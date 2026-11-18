import argparse
import hmac
import json
import logging
import os
import re
import ssl
import time
import threading
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from .bot import Bot, event_key
from .storage import Store

LOG = logging.getLogger('iskra')
TYPES = ['bot_started','message_created','message_callback']


def load_env():
    path = Path('.env')
    if path.exists():
        for line in path.read_text().splitlines():
            line = line.strip()
            if line and not line.startswith('#') and '=' in line:
                key,value = line.split('=',1)
                os.environ.setdefault(key.strip(),value.strip().strip('\"').strip("'"))


class ApiError(Exception):
    def __init__(self, status, code='unknown'):
        self.status = status
        super().__init__('MAX API status %s (%s)'%(status,code))


class Api:
    def __init__(self):
        self.token = os.environ.get('MAX_BOT_TOKEN','')
        if not self.token:
            raise SystemExit('Добавь MAX_BOT_TOKEN в .env. Без ключа: python3 -m iskra demo')
        self.url = os.environ.get('MAX_API_URL','https://platform-api2.max.ru').rstrip('/')
        if not self.url.startswith('https://'):
            raise SystemExit('MAX_API_URL должен использовать HTTPS')
        self.ssl = ssl.create_default_context(cafile=os.environ.get('SSL_CERT_FILE') or None)

    def call(self, method, path, body=None, **params):
        query = urllib.parse.urlencode({k:v for k,v in params.items() if v is not None})
        request = urllib.request.Request(self.url + path + ('?'+query if query else ''),
            data=json.dumps(body).encode() if body is not None else None, method=method,
            headers={'Authorization':self.token,'Content-Type':'application/json'})
        try:
            with urllib.request.urlopen(request,context=self.ssl,timeout=45) as response:
                result = json.load(response)
        except urllib.error.HTTPError as exc:
            # Never log headers, credentials, API response text or user messages.
            raise ApiError(exc.code) from None
        if result.get('success') is False:
            raise ApiError(400, result.get('code','rejected'))
        return result


def enqueue(store, event):
    store.db.execute('INSERT OR IGNORE INTO inbox(key,payload) VALUES(?,?)',
                     (event_key(event),json.dumps(event)))


def process(store, bot, api=None):
    rows = store.db.execute('SELECT * FROM inbox WHERE done=0 AND next_try<=? ORDER BY rowid LIMIT 30',(time.time(),)).fetchall()
    for row in rows:
        try:
            with store.db:
                event = json.loads(row['payload'])
                bot.handle(event)
                store.db.execute('UPDATE inbox SET done=1 WHERE key=?',(row['key'],))
            if api and event.get('callback',{}).get('callback_id'):
                try:
                    api.call('POST','/answers',{'notification':'✓'},callback_id=event['callback']['callback_id'])
                except Exception:
                    LOG.warning('Callback acknowledgement failed')
        except Exception:
            LOG.exception('Event processing failed; content omitted')
            with store.db:
                store.db.execute('UPDATE inbox SET attempts=attempts+1,next_try=? WHERE key=?',
                                  (time.time()+min(300,2**min(row['attempts']+1,8)),row['key']))
    with store.db:
        # Completed events retain identifiers for deduplication but no raw personal data.
        store.db.execute("UPDATE inbox SET payload='{}' WHERE done=1 AND payload!='{}'")
        store.db.execute('DELETE FROM inbox WHERE done=1 AND rowid NOT IN (SELECT rowid FROM inbox ORDER BY rowid DESC LIMIT 10000)')


def deliver(store, api, last_sent):
    now = time.time()
    rows = store.db.execute('SELECT * FROM outbox WHERE dead=0 AND next_try<=? ORDER BY id LIMIT 100',(now,)).fetchall()
    seen = set()
    for row in rows:
        uid = row['uid']
        if uid in seen:
            continue
        seen.add(uid)
        # Preserve per-recipient order, including messages awaiting retry.
        head = store.db.execute('SELECT min(id) FROM outbox WHERE uid=? AND dead=0',(uid,)).fetchone()[0]
        if head != row['id'] or time.time()-last_sent.get(uid,0) < 0.55:
            continue
        try:
            api.call('POST','/messages',json.loads(row['body']),user_id=uid)
        except Exception as exc:
            status = exc.status if isinstance(exc,ApiError) else 0
            dead = status in (401,403,404) or row['attempts'] >= 11
            LOG.warning('Delivery failed status=%s dead=%s',status,dead)
            with store.db:
                store.db.execute('UPDATE outbox SET attempts=attempts+1,next_try=?,dead=? WHERE id=?',
                                  (time.time()+min(3600,2**min(row['attempts']+1,11)),int(dead),row['id']))
        else:
            with store.db:
                store.db.execute('DELETE FROM outbox WHERE id=?',(row['id'],))
        last_sent[uid] = time.time()


def polling(store, bot, api):
    if api.call('GET','/subscriptions').get('subscriptions'):
        raise SystemExit('У бота настроен webhook. Используй webhook-режим либо сначала удали подписку через API.')
    LOG.info('Polling enabled for local testing')
    last_sent = {}
    while True:
        try:
            marker = store.db.execute("SELECT value FROM meta WHERE key='marker'").fetchone()
            result = api.call('GET','/updates',marker=marker[0] if marker else None,
                              timeout=1,types=','.join(TYPES))
            with store.db:
                for event in result.get('updates',[]):
                    enqueue(store,event)
                if result.get('marker') is not None:
                    store.db.execute("INSERT OR REPLACE INTO meta(key,value) VALUES('marker',?)",(str(result['marker']),))
            process(store,bot,api)
            deliver(store,api,last_sent)
        except ApiError as exc:
            if exc.status == 401:
                raise SystemExit('MAX отклонил токен (401). Проверь .env.')
            LOG.warning('Polling API error status=%s',exc.status)
            time.sleep(3)
        except (OSError,ValueError):
            LOG.warning('Polling connection failed')
            time.sleep(3)


def webhook(store, bot, api):
    secret = os.environ.get('WEBHOOK_SECRET','')
    if not re.fullmatch(r'[A-Za-z0-9_-]{16,256}',secret):
        raise SystemExit('Задай WEBHOOK_SECRET: 16–256 букв, цифр, _ или -')
    class Handler(BaseHTTPRequestHandler):
        def setup(self):
            super().setup()
            self.connection.settimeout(10)
        def log_message(self, *args):
            pass
        def do_GET(self):
            if self.path != '/health':
                self.send_error(404); return
            self.send_response(200)
            self.send_header('Content-Type','application/json')
            self.end_headers()
            self.wfile.write(b'{"status":"ok"}')
        def do_POST(self):
            if self.path != '/webhook':
                self.send_error(404); return
            if not hmac.compare_digest(self.headers.get('X-Max-Bot-Api-Secret',''),secret):
                self.send_error(403); return
            try:
                length = int(self.headers.get('Content-Length','0'))
                if not 0 < length <= 1048576:
                    self.send_error(413); return
                event = json.loads(self.rfile.read(length))
                if not isinstance(event,dict):
                    self.send_error(400); return
                receiver = Store(os.environ.get('DATABASE_PATH','data/iskra.sqlite3'))
                try:
                    with receiver.db:
                        enqueue(receiver,event)
                finally:
                    receiver.db.close()
            except (ValueError,TypeError):
                self.send_error(400); return
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b'OK')
    server = ThreadingHTTPServer((os.environ.get('HOST','127.0.0.1'),int(os.environ.get('PORT','8080'))),Handler)
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    last_sent = {}
    LOG.info('Webhook listening on %s:%s',*server.server_address)
    try:
        while True:
            process(store,bot,api)
            deliver(store,api,last_sent)
            time.sleep(0.2)
    finally:
        server.shutdown()
        server.server_close()


def demo():
    from .demo import run
    run()


def main():
    load_env()
    logging.basicConfig(level=logging.INFO,format='%(asctime)s %(levelname)s %(message)s')
    parser = argparse.ArgumentParser(description='Искра — бот знакомств для MAX')
    parser.add_argument('mode',choices=['demo','poll','webhook','subscribe','check'])
    args = parser.parse_args()
    if args.mode == 'demo':
        demo(); return
    api = Api()
    if args.mode == 'check':
        me = api.call('GET','/me')
        print('Подключено:',me.get('name'), 'ID:',me.get('user_id')); return
    if args.mode == 'subscribe':
        url, secret = os.environ.get('WEBHOOK_URL',''),os.environ.get('WEBHOOK_SECRET','')
        if not url.startswith('https://') or not re.fullmatch(r'[A-Za-z0-9_-]{16,256}',secret):
            raise SystemExit('Задай HTTPS WEBHOOK_URL и WEBHOOK_SECRET в .env')
        api.call('POST','/subscriptions',{'url':url,'secret':secret,'update_types':TYPES})
        print('Webhook зарегистрирован.'); return
    store = Store(os.environ.get('DATABASE_PATH','data/iskra.sqlite3'))
    admins = [int(x.strip()) for x in os.environ.get('ADMIN_IDS','').split(',') if x.strip()]
    bot = Bot(store,admins)
    try:
        (polling if args.mode == 'poll' else webhook)(store,bot,api)
    except KeyboardInterrupt:
        print('\nБот остановлен.')
    finally:
        store.db.close()
