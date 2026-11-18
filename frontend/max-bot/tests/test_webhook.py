import json
import multiprocessing
import os
import socket
import tempfile
import time
import unittest
import urllib.error
import urllib.request

from iskra.bot import Bot
from iskra.runtime import webhook
from iskra.storage import Store


def run_server(path,port):
    os.environ.update(DATABASE_PATH=path,PORT=str(port),HOST='127.0.0.1',WEBHOOK_SECRET='test_secret_123456789')
    class FakeApi:
        def call(self,*args,**kwargs):
            return {'success':True}
    store=Store(path)
    webhook(store,Bot(store),FakeApi())


class WebhookTests(unittest.TestCase):
    def test_secret_validation_and_persisted_event(self):
        with tempfile.TemporaryDirectory() as folder:
            path=os.path.join(folder,'test.sqlite3')
            sock=socket.socket(); sock.bind(('127.0.0.1',0)); port=sock.getsockname()[1]; sock.close()
            proc=multiprocessing.Process(target=run_server,args=(path,port))
            proc.start()
            try:
                url='http://127.0.0.1:%s'%port
                for _ in range(100):
                    try:
                        with urllib.request.urlopen(url+'/health',timeout=1) as response:
                            self.assertEqual(json.load(response)['status'],'ok')
                        break
                    except OSError:
                        time.sleep(0.05)
                else:
                    self.fail('Webhook did not start')
                event=json.dumps({'update_type':'bot_started','user':{'user_id':42},'timestamp':123}).encode()
                request=urllib.request.Request(url+'/webhook',data=event,headers={'Content-Type':'application/json'})
                with self.assertRaises(urllib.error.HTTPError) as error:
                    urllib.request.urlopen(request,timeout=2)
                self.assertEqual(error.exception.code,403)
                request.add_header('X-Max-Bot-Api-Secret','test_secret_123456789')
                with urllib.request.urlopen(request,timeout=2) as response:
                    self.assertEqual(response.status,200)
                with urllib.request.urlopen(request,timeout=2) as response:
                    self.assertEqual(response.status,200)
                for _ in range(50):
                    store=Store(path)
                    rows=store.db.execute('SELECT done FROM inbox').fetchall()
                    store.db.close()
                    if rows and rows[0]['done']:
                        break
                    time.sleep(0.05)
                self.assertEqual(len(rows),1)
                self.assertEqual(rows[0]['done'],1)
            finally:
                proc.terminate(); proc.join(timeout=5)
