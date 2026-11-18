import html
import re
from .bot import Bot
from .storage import Store


def run():
    s = Store(':memory:')
    b = Bot(s, [1])
    for uid,name,age,gender,bio in [(101,'Аня',24,'f','Люблю кофе, горы и разговоры до рассвета ☕'),(102,'Маша',26,'f','Ищу компанию на выставку и прогулку по набережной 🎨'),(103,'Артём',25,'m','Гитара, походы и спонтанные поездки 🎸')]:
        u = s.get(uid)
        u['profile'] = dict(name=name,age=age,gender=gender,looking='any',city='Самара',city_key='самара',bio=bio,complete=True,min_age=18,max_age=99)
        u['active'] = 1
        s.save(u)
    s.db.commit()
    uid = 1
    print('ИСКРА ✦ Демо без ключа. Данные только в памяти.\nТекст — отправить сообщение. Число — нажать кнопку.\n/photo — тестовое фото, /user ID — сменить пользователя, /quit — выход.\nТестовые анкеты: 101, 102, 103. Город: Самара.\n')
    choices = []
    def show():
        nonlocal choices
        for row in s.db.execute('SELECT * FROM outbox ORDER BY id').fetchall():
            body = __import__('json').loads(row['body'])
            print('\n[%s] %s' % (row['uid'],html.unescape(re.sub('<[^>]+>','',body['text']))))
            if row['uid'] == uid:
                choices = []
            for attachment in body['attachments']:
                if attachment['type'] == 'image':
                    print('🖼 [Фото анкеты]')
                if attachment['type'] == 'inline_keyboard':
                    for buttons in attachment['payload']['buttons']:
                        for item in buttons:
                            if row['uid'] == uid:
                                choices.append(item['payload'])
                                print('  %s. %s'%(len(choices),item['text']))
            s.db.execute('DELETE FROM outbox WHERE id=?',(row['id'],))
        s.db.commit()
    with s.db:
        u = s.get(uid); b.start(u); s.save(u)
    show()
    while True:
        try:
            text = input('\nТы [%s] › '%uid).strip()
        except (EOFError,KeyboardInterrupt):
            break
        if text == '/quit':
            break
        if text.startswith('/user '):
            try:
                uid = int(text.split()[1])
            except (ValueError,IndexError):
                continue
            with s.db:
                u = s.get(uid); b.start(u); s.save(u)
            show(); continue
        with s.db:
            u = s.get(uid)
            if text.isdigit() and 1 <= int(text) <= len(choices):
                b.callback(u,choices[int(text)-1])
            else:
                b.text(u,text if text != '/photo' else '',[{'type':'image','payload':{'token':'demo-photo'}}] if text == '/photo' else [])
            if not u.get('deleted'):
                s.save(u)
        show()
