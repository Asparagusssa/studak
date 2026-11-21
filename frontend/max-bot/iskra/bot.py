import hashlib
import html
import json
import re
import time


def button(text, payload):
    return {'type': 'callback', 'text': text, 'payload': payload}


def keyboard(rows):
    return {'type': 'inline_keyboard', 'payload': {'buttons': [[button(*b) for b in row] for row in rows]}}


MENU = [[('✨ Смотреть анкеты', 'browse')], [('💌 Кто меня лайкнул', 'likes'), ('💞 Мои симпатии', 'matches')],
        [('🪪 Моя анкета', 'profile'), ('⚙️ Настройки', 'settings')]]
GENDERS = [[('Я парень', 'gender:m'), ('Я девушка', 'gender:f')]]
LOOKING = [[('Парней', 'looking:m'), ('Девушек', 'looking:f'), ('Всех', 'looking:any')]]
STEPS = ['name', 'age', 'gender', 'looking', 'city', 'bio', 'photo', 'confirm']
PROMPTS = {
 'name': ('Как тебя зовут?\nНапиши имя, которое увидят другие.', None),
 'age': ('Сколько тебе лет?\nЗдесь знакомятся люди от 18 до 99 лет.', None),
 'gender': ('Расскажи немного о себе', GENDERS),
 'looking': ('С кем хочешь познакомиться?', LOOKING),
 'city': ('В каком городе ты живёшь?\nНапример: Самара. Мы начнём с людей рядом.', None),
 'bio': ('Что делает тебя тобой?\nНапиши о себе и интересах, до 600 символов.', None),
 'photo': ('Добавь искру ✨\nОтправь своё фото одним изображением. Оно появится в анкете.', None),
}

class Bot:
    def __init__(self, store, admins=()):
        self.s = store
        self.admins = set(admins)

    def say(self, uid, text, rows=None, photo=None):
        body = {'text': text, 'format': 'html', 'attachments': []}
        if photo:
            body['attachments'].append({'type': 'image', 'payload': {'token': photo}})
        if rows:
            body['attachments'].append(keyboard(rows))
        self.s.send(uid, body)

    def menu(self, u):
        self.say(u['id'], '<b>Искра ✦ знакомства рядом</b>\n\nОдин лайк может стать началом большой истории.\nВыбирай, что хочется сейчас.', MENU)

    def start(self, u):
        if u['profile'].get('complete'):
            self.menu(u)
        else:
            self.say(u['id'], '<b>Привет, это Искра ✨</b>\n\nЛюди рядом. Общие интересы. Та самая симпатия.\nСоздай анкету за пару минут и начни знакомиться.\n\n<b>18+</b> · уважение · никакого спама\nАнкета и фото будут видны участникам. Переписка проходит через бота; контакты можно отправить самостоятельно.\n\nНажимая «Начать», ты подтверждаешь совершеннолетие и согласие сохранить анкету для знакомств. Удалить данные можно в настройках.', [[('✨ Начать', 'create')], [('📖 Правила и данные', 'rules')]])

    def prompt(self, u):
        step = u['state']['step']
        if step == 'confirm':
            self.card(u['id'], dict(u, profile=u['state']['draft']),
                      [[('🚀 Всё отлично, опубликовать', 'publish')], [('✏️ Заполнить заново', 'create'), ('🏠 Отмена', 'menu')]])
        else:
            text, rows = PROMPTS[step]
            index = STEPS.index(step) + 1
            self.say(u['id'], '<b>Твоя анкета · %s/8</b>\n%s\n\n%s' % (index, '●' * index + '○' * (8-index), text),
                     (rows or []) + [[('🏠 Отмена', 'menu')]])

    def card(self, uid, other, rows, note=''):
        p = other['profile']
        caption = '<b>%s, %s</b>\n📍 %s\n\n%s' % (html.escape(p.get('name','')), p.get('age',''), html.escape(p.get('city','')), html.escape(p.get('bio','')))
        if note:
            caption += '\n\n💌 <i>%s</i>' % html.escape(note)
        self.say(uid, caption, rows, p.get('photo'))

    def allowed(self, u, other):
        p, q = u['profile'], other['profile']
        return (u['id'] != other['id'] and other['active'] and not other['banned'] and q.get('complete')
                and not self.s.blocked(u['id'], other['id'])
                and (p.get('looking') == 'any' or p.get('looking') == q.get('gender'))
                and (q.get('looking') == 'any' or q.get('looking') == p.get('gender'))
                and p.get('min_age',18) <= q['age'] <= p.get('max_age',99)
                and q.get('min_age',18) <= p['age'] <= q.get('max_age',99)
                and (p.get('all_cities') or p['city_key'] == q['city_key'])
                and (q.get('all_cities') or p['city_key'] == q['city_key']))

    def browse(self, u, incoming=False):
        if not u['active']:
            self.say(u['id'], 'Твоя анкета на паузе. Включи её, чтобы знакомиться.', [[('▶️ Включить анкету', 'toggle')]])
            return
        ids = self.s.db.execute('SELECT id FROM users WHERE active=1 AND banned=0 ORDER BY created DESC,id').fetchall()
        for row in ids:
            v = self.s.get(row['id'])
            if not self.allowed(u,v):
                continue
            reaction = self.s.db.execute('SELECT kind FROM reactions WHERE actor=? AND target=?', (u['id'],v['id'])).fetchone()
            if reaction or self.s.matched(u['id'], v['id']):
                continue
            like = self.s.db.execute("SELECT note FROM reactions WHERE actor=? AND target=? AND kind='like'", (v['id'],u['id'])).fetchone()
            if incoming and not like:
                continue
            u['state'] = {'candidate': v['id'], 'incoming': incoming}
            self.card(u['id'], v, [[('❤️ Нравится', 'like:%s'%v['id']), ('👋 Пропустить', 'skip:%s'%v['id'])],
                       [('💬 Лайк с сообщением', 'note:%s'%v['id'])], [('🚩 Пожаловаться', 'report:%s'%v['id']), ('🏠 Меню', 'menu')]],
                      like['note'] if incoming and like else '')
            return
        u['state'] = {}
        self.say(u['id'], '💌 Пока нет новых лайков.' if incoming else '🌙 На сегодня анкеты закончились.\nПопробуй расширить возраст или искать по всем городам. Возвращайся — здесь появятся новые люди.',
                 [[('🔄 Проверить снова', 'likes' if incoming else 'browse')], [('⚙️ Настройки', 'settings'), ('🏠 Меню', 'menu')]])

    def react(self, u, target, kind, note=''):
        v = self.s.get(target)
        if u['state'].get('candidate') != target or not self.allowed(u, v) or not u['active']:
            self.say(u['id'], 'Эта карточка уже неактуальна. Открой новые анкеты.', MENU)
            return
        existing = self.s.db.execute('SELECT 1 FROM reactions WHERE actor=? AND target=?', (u['id'],target)).fetchone()
        if existing:
            self.browse(u, u['state'].get('incoming',False))
            return
        self.s.db.execute('INSERT INTO reactions(actor,target,kind,note) VALUES(?,?,?,?)', (u['id'],target,kind,note))
        reverse = self.s.db.execute("SELECT 1 FROM reactions WHERE actor=? AND target=? AND kind='like'", (target,u['id'])).fetchone()
        if kind == 'like' and reverse:
            a,b = sorted((u['id'],target))
            self.s.db.execute('INSERT OR IGNORE INTO matches(a,b) VALUES(?,?)', (a,b))
            for who, other in [(u['id'],v),(target,u)]:
                self.say(who, '<b>Это взаимно! 💞</b>\n%s тоже хочет познакомиться. Самое время сказать «привет».\nТвои контакты не раскрываются автоматически.' % html.escape(other['profile']['name']),
                         [[('💬 Написать', 'chat:%s'%other['id'])], [('💞 Мои симпатии', 'matches'), ('🏠 Меню', 'menu')]])
        elif kind == 'like':
            self.say(target, '💌 Ты кому-то понравился!\nЗагляни в лайки — вдруг это взаимно?', [[('✨ Посмотреть', 'likes')]])
        self.browse(u, u['state'].get('incoming',False))

    def list_matches(self, u):
        rows = self.s.db.execute('SELECT a,b FROM matches WHERE a=? OR b=? ORDER BY created DESC', (u['id'],u['id'])).fetchall()
        buttons = []
        for row in rows:
            target = row['b'] if row['a'] == u['id'] else row['a']
            v = self.s.get(target)
            if not v['banned'] and not self.s.blocked(u['id'],target):
                buttons.append([(v['profile'].get('name','Симпатия') + ' 💬', 'chat:%s'%target)])
            if len(buttons) == 20:
                break
        self.say(u['id'], '<b>Мои симпатии 💞</b>\nВыбери, кому написать.' if buttons else '💞 Здесь появятся взаимные симпатии.', buttons + [[('🏠 Меню','menu')]])

    def settings(self, u):
        p = u['profile']
        self.say(u['id'], '<b>Твои настройки ⚙️</b>\nВозраст: %s–%s\nПоиск: %s\nАнкета: %s' % (p.get('min_age',18), p.get('max_age',99), 'все города' if p.get('all_cities') else html.escape(p.get('city','')), 'видна' if u['active'] else 'на паузе'),
                 [[('🎂 Возраст поиска', 'ages'), ('📍 Сменить географию', 'cities')], [('🔎 Кого искать', 'preference')],
                  [('⏸ Пауза / ▶️ Продолжить','toggle')], [('🗑 Удалить все мои данные', 'delete')], [('🏠 Меню', 'menu')]])

    def callback(self, u, action):
        uid = u['id']
        if action == 'rules':
            self.say(uid, '<b>Знакомимся с уважением</b>\nТолько 18+. Используй свои фотографии. Нельзя выдавать себя за другого, спамить, оскорблять, публиковать чужие данные или запрещённый контент.\n\n<b>Что хранится</b>\nИмя, возраст, город, описание, токен фото, лайки, блокировки и жалобы. Сообщения временно хранятся в очереди доставки; после отправки удаляются. Жалобы видят администраторы.\nУдаление анкеты стирает связанные данные. Уже доставленные сообщения остаются в MAX.\n\nДля защиты от повторов сохраняются идентификаторы последних 10 000 обработанных событий. Модерация выполняется администратором.', [[('🏠 Назад','menu')]])
            return
        if action == 'menu':
            u['state'] = {}
            self.start(u)
            return
        if action == 'create':
            u['state'] = {'step':'name','draft':{}}
            self.prompt(u)
            return
        if action.startswith(('gender:', 'looking:')) and u['state'].get('step'):
            field, value = action.split(':')
            if field != u['state']['step'] or value not in (['m','f'] if field == 'gender' else ['m','f','any']):
                return
            u['state']['draft'][field] = value
            u['state']['step'] = STEPS[STEPS.index(field)+1]
            self.prompt(u)
            return
        if action == 'publish' and u['state'].get('step') == 'confirm':
            old = u['profile']
            u['profile'] = dict(u['state']['draft'], complete=True, min_age=old.get('min_age',18), max_age=old.get('max_age',99), all_cities=old.get('all_cities',False))
            u['active'] = 1
            u['state'] = {}
            self.say(uid, '<b>Ты в Искре ✨</b>\nАнкета опубликована. Давай найдём человека, с которым захочется задержаться.', MENU)
            return
        if not u['profile'].get('complete'):
            self.start(u)
            return
        if action == 'browse':
            self.browse(u)
        elif action == 'likes':
            self.browse(u, True)
        elif action == 'matches':
            u['state'] = {}
            self.list_matches(u)
        elif action == 'profile':
            u['state'] = {}
            self.card(uid,u,[[('✏️ Изменить анкету','create')], [('⚙️ Настройки','settings'), ('🏠 Меню','menu')]])
        elif action == 'settings':
            u['state'] = {}
            self.settings(u)
        elif action == 'toggle':
            u['active'] = 1 - u['active']
            self.settings(u)
        elif action == 'cities':
            u['profile']['all_cities'] = not u['profile'].get('all_cities',False)
            self.settings(u)
        elif action == 'ages':
            u['state'] = {'input':'ages'}
            self.say(uid, '🎂 Напиши возрастной диапазон: например, 20-30.\nДопустимо от 18 до 99.', [[('🏠 Отмена','menu')]])
        elif action == 'preference':
            u['state'] = {'input':'preference'}
            self.say(uid, 'Кого будем искать?', [[('Парней','pref:m'), ('Девушек','pref:f'), ('Всех','pref:any')]])
        elif action.startswith('pref:') and u['state'].get('input') == 'preference':
            value = action.split(':')[1]
            if value in ('m','f','any'):
                u['profile']['looking'] = value
                u['state'] = {}
                self.settings(u)
        elif action == 'delete':
            self.say(uid, 'Удалить анкету, лайки, симпатии и связанные данные? Это действие нельзя отменить.', [[('Да, удалить','delete_confirm')], [('Оставить','menu')]])
            u['state'] = {'input':'delete'}
        elif action == 'delete_confirm' and u['state'].get('input') == 'delete':
            self.s.db.execute('DELETE FROM users WHERE id=?',(uid,))
            self.s.db.execute('DELETE FROM outbox WHERE uid=?',(uid,))
            # Remove pending/raw events containing this user and any not-yet-delivered messages.
            for row in self.s.db.execute('SELECT key,payload FROM inbox').fetchall():
                ev = json.loads(row['payload'])
                actor = ev.get('user') or ev.get('callback',{}).get('user') or ev.get('message',{}).get('sender') or {}
                if actor.get('user_id') == uid:
                    self.s.db.execute('DELETE FROM inbox WHERE key=?',(row['key'],))
            self.say(uid, 'Анкета и связанные данные удалены. Если захочешь вернуться — /start.')
            u['deleted'] = True
        elif ':' in action:
            verb, raw = action.split(':',1)
            if not raw.isdigit():
                return
            target = int(raw)
            v = self.s.get(target)
            if target == uid or v['banned'] or not v['profile'].get('complete'):
                return
            if verb in ('like','skip'):
                self.react(u,target,'like' if verb == 'like' else 'skip')
            elif verb == 'note' and u['state'].get('candidate') == target:
                u['state']['input'] = 'note'
                self.say(uid, '💌 Напиши первое сообщение — до 400 символов.\nОтправлю его вместе с твоим лайком.', [[('🏠 Отмена','menu')]])
            elif verb in ('report','block') and (u['state'].get('candidate') == target or self.s.matched(uid,target)):
                u['state'] = {'input':'report','target':target}
                self.say(uid, '🚩 Что случилось?\nНапиши причину жалобы (до 600 символов). Пользователь будет скрыт от тебя.', [[('🏠 Отмена','menu')]])
            elif verb == 'chat' and self.s.matched(uid,target) and not self.s.blocked(uid,target):
                u['state'] = {'chat':target}
                self.say(uid, '💬 <b>Диалог с %s</b>\nОтправь текст — я передам его. Для выхода нажми «Меню» или /menu.' % html.escape(v['profile']['name']), [[('🚩 Жалоба / блокировка','report:%s'%target)], [('🏠 Меню','menu')]])

    def text(self, u, text, attachments):
        uid = u['id']
        if text.startswith('/admin'):
            if uid not in self.admins:
                self.say(uid, 'Нет доступа к модерации.')
                return
            parts = text.split()
            if len(parts) == 3 and parts[1] in ('ban','unban') and parts[2].isdigit():
                target = int(parts[2])
                self.s.db.execute('UPDATE users SET banned=? WHERE id=?',(int(parts[1]=='ban'),target))
                self.s.db.execute('DELETE FROM outbox WHERE uid=?',(target,))
                self.s.db.execute('UPDATE reports SET resolved=1 WHERE target=?',(target,))
                self.say(uid, 'Модерация: действие выполнено.')
            else:
                stats = self.s.db.execute('SELECT count(*) n,sum(active) a FROM users').fetchone()
                reports = self.s.db.execute('SELECT id,actor,target,reason FROM reports WHERE resolved=0 ORDER BY id DESC LIMIT 10').fetchall()
                self.say(uid, '<b>Модерация</b>\nАнкет: %s · активных: %s\n/admin ban ID · /admin unban ID\n\n%s' % (stats['n'],stats['a'] or 0, '\n'.join('#%s · %s → %s: %s'%(r['id'],r['actor'],r['target'],html.escape(r['reason'])) for r in reports) or 'Новых жалоб нет.'))
            return
        if text in ('/start','/menu','/cancel','/help','/id'):
            if text == '/id':
                self.say(uid, 'Твой ID: <code>%s</code>'%uid)
            elif text == '/help':
                self.say(uid, '/start — начать\n/menu — меню и выход из диалога\n/cancel — отменить ввод\n/id — твой ID\n/privacy — правила и данные\n/delete — удалить данные', MENU)
            else:
                u['state'] = {}
                self.start(u)
            return
        if text == '/privacy':
            self.callback(u,'rules')
            return
        if text == '/delete':
            self.callback(u,'delete')
            return
        state = u['state']
        if state.get('step'):
            step, draft = state['step'], state['draft']
            if step == 'name':
                if not 2 <= len(text) <= 40:
                    self.say(uid,'Имя должно быть от 2 до 40 символов.'); return
                draft['name'] = text
            elif step == 'age':
                if not text.isdigit() or not 18 <= int(text) <= 99:
                    self.say(uid,'Укажи возраст от 18 до 99 лет.'); return
                draft['age'] = int(text)
            elif step == 'city':
                if not 2 <= len(text) <= 60:
                    self.say(uid,'Напиши название города: от 2 до 60 символов.'); return
                draft['city'] = text
                draft['city_key'] = ' '.join(text.casefold().replace('ё','е').split())
            elif step == 'bio':
                if not 5 <= len(text) <= 600:
                    self.say(uid,'Напиши от 5 до 600 символов о себе.'); return
                draft['bio'] = text
            elif step == 'photo':
                photos = [a for a in attachments if a.get('type') == 'image' and a.get('payload',{}).get('token')]
                if not photos:
                    self.say(uid,'Пришли фото как изображение, не как файл.'); return
                draft['photo'] = photos[0]['payload']['token']
            else:
                self.prompt(u); return
            state['step'] = STEPS[STEPS.index(step)+1]
            self.prompt(u)
        elif state.get('input') == 'ages':
            m = re.fullmatch(r'(\d{2})\s*[-– ]\s*(\d{2})',text)
            if not m or not 18 <= int(m[1]) <= int(m[2]) <= 99:
                self.say(uid,'Напиши диапазон в формате 20-30, от 18 до 99.'); return
            u['profile'].update(min_age=int(m[1]),max_age=int(m[2]))
            u['state'] = {}
            self.settings(u)
        elif state.get('input') == 'note':
            if not 1 <= len(text) <= 400:
                self.say(uid,'Сообщение должно быть от 1 до 400 символов.'); return
            self.react(u,state['candidate'],'like',text)
        elif state.get('input') == 'report':
            if not 3 <= len(text) <= 600:
                self.say(uid,'Напиши причину: от 3 до 600 символов.'); return
            target = state['target']
            self.s.db.execute('INSERT OR IGNORE INTO blocks(actor,target) VALUES(?,?)',(uid,target))
            self.s.db.execute('INSERT INTO reports(actor,target,reason) VALUES(?,?,?)',(uid,target,text))
            for admin in self.admins:
                self.say(admin,'🚩 Жалоба от %s на %s\n%s\n/admin ban %s'%(uid,target,html.escape(text),target))
            u['state'] = {}
            self.say(uid,'Жалоба сохранена для модерации. Вы больше не видите друг друга и не можете переписываться.',MENU)
        elif state.get('chat'):
            target = state['chat']
            v = self.s.get(target)
            if not self.s.matched(uid,target) or self.s.blocked(uid,target) or v['banned']:
                u['state'] = {}
                self.say(uid,'Диалог недоступен.',MENU); return
            if not 1 <= len(text) <= 1500:
                self.say(uid,'Отправь текст длиной до 1500 символов.'); return
            self.say(target,'💬 <b>%s</b>\n\n%s'%(html.escape(u['profile']['name']),html.escape(text)), [[('↩️ Ответить','chat:%s'%uid),('🚩 Жалоба','report:%s'%uid)]])
            self.say(uid,'✓ Сообщение в очереди доставки.',[[('🏠 Меню','menu')]])
        else:
            self.start(u)

    def handle(self, event):
        kind = event.get('update_type')
        if kind == 'message_callback':
            actor = event.get('callback',{}).get('user',{})
        elif kind == 'message_created':
            message = event.get('message',{})
            if message.get('recipient',{}).get('chat_type') != 'dialog':
                return
            actor = message.get('sender') or {}
        elif kind == 'bot_started':
            actor = event.get('user',{})
        else:
            return
        if not actor.get('user_id') or actor.get('is_bot'):
            return
        uid = int(actor['user_id'])
        u = self.s.get(uid)
        if u['banned']:
            self.say(uid,'Анкета заблокирована модератором.'); return
        now = time.time()
        if kind == 'bot_started':
            self.start(u)
        elif kind == 'message_callback':
            self.callback(u,event['callback'].get('payload',''))
        else:
            body = event['message'].get('body',{})
            self.text(u,(body.get('text') or '').strip(),body.get('attachments') or [])
        if not u.get('deleted'):
            u['state']['_last'] = now
            self.s.save(u)


def event_key(event):
    callback = event.get('callback') or {}
    mid = (event.get('message') or {}).get('body',{}).get('mid')
    if callback.get('callback_id'):
        return 'callback:' + callback['callback_id']
    if event.get('update_type') == 'message_created' and mid:
        return 'message:' + mid
    return hashlib.sha256(json.dumps(event,sort_keys=True).encode()).hexdigest()
