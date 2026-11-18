import json
import unittest
from iskra.bot import Bot, event_key
from iskra.storage import Store
from iskra.runtime import enqueue, process, deliver, ApiError


class ScenarioTests(unittest.TestCase):
    def setUp(self):
        self.s = Store(':memory:')
        self.bot = Bot(self.s,[999])
    def tearDown(self):
        self.s.db.close()
    def user(self, uid, gender='f', city='Самара', age=25):
        u = self.s.get(uid)
        u['profile'] = dict(name='Тест <b>', age=age, gender=gender, looking='any', city=city,city_key=city.casefold(),bio='Привет всем!', photo='test-token', complete=True,min_age=18,max_age=99)
        u['active'] = 1
        self.s.save(u)
        return u
    def callback(self,uid,action):
        u = self.s.get(uid)
        self.bot.callback(u,action)
        if not u.get('deleted'):
            self.s.save(u)
        return u
    def text(self,uid,text,attachments=()):
        u = self.s.get(uid)
        self.bot.text(u,text,attachments)
        self.s.save(u)
        return u
    def messages(self,uid):
        return [json.loads(r[0]) for r in self.s.db.execute('SELECT body FROM outbox WHERE uid=?',(uid,))]
    def test_registration_validation_photo_publish(self):
        self.callback(1,'create')
        self.text(1,'Никита')
        self.text(1,'17')
        self.assertEqual(self.s.get(1)['state']['step'],'age')
        self.text(1,'23')
        self.callback(1,'gender:m'); self.callback(1,'looking:f')
        self.text(1,'Самара'); self.text(1,'Люблю музыку и путешествия')
        self.text(1,'фото')
        self.assertEqual(self.s.get(1)['state']['step'],'photo')
        self.text(1,'',[{'type':'image','payload':{'token':'photo-1'}}])
        self.assertFalse(self.s.get(1)['active'])
        self.callback(1,'publish')
        self.assertTrue(self.s.get(1)['active'])
        self.assertEqual(self.s.get(1)['profile']['photo'],'photo-1')
    def test_mutual_preferences_city_age(self):
        a = self.user(1,'m'); b = self.user(2)
        self.assertTrue(self.bot.allowed(a,b))
        b['profile']['looking']='f'
        self.assertFalse(self.bot.allowed(a,b))
        b['profile']['looking']='any'; b['profile']['city_key']='москва'
        self.assertFalse(self.bot.allowed(a,b))
        a['profile']['all_cities']=True; b['profile']['all_cities']=True
        self.assertTrue(self.bot.allowed(a,b))
        a['profile']['max_age']=24
        self.assertFalse(self.bot.allowed(a,b))
    def test_like_note_match_and_relay(self):
        self.user(1,'m'); self.user(2)
        self.callback(1,'browse'); self.callback(1,'note:2')
        self.text(1,'Привет!')
        self.assertFalse(self.s.matched(1,2))
        self.callback(2,'likes')
        self.assertIn('Привет!',self.messages(2)[-1]['text'])
        self.callback(2,'like:1')
        self.assertTrue(self.s.matched(1,2))
        self.assertEqual(self.s.db.execute('SELECT count(*) FROM matches').fetchone()[0],1)
        self.callback(1,'chat:2'); self.text(1,'<script>привет</script>')
        self.assertIn('&lt;script&gt;',self.messages(2)[-1]['text'])
    def test_stale_button_cannot_like_anyone(self):
        self.user(1); self.user(2)
        self.callback(1,'like:2')
        self.assertEqual(self.s.db.execute('SELECT count(*) FROM reactions').fetchone()[0],0)
    def test_no_chat_before_match(self):
        self.user(1); self.user(2)
        self.callback(1,'chat:2')
        self.assertNotIn('chat',self.s.get(1)['state'])
    def test_report_blocks_feed_and_chat(self):
        a = self.user(1); b = self.user(2)
        self.s.db.execute('INSERT INTO matches(a,b) VALUES(1,2)')
        self.callback(1,'report:2'); self.text(1,'Спам в сообщениях')
        self.assertTrue(self.s.blocked(1,2))
        self.assertFalse(self.bot.allowed(a,b))
        self.callback(2,'chat:1')
        self.assertNotIn('chat',self.s.get(2)['state'])
        self.assertIn('Жалоба',self.messages(999)[-1]['text'])
    def test_delete_cascade_and_recreate(self):
        self.user(1); self.user(2)
        self.s.db.execute("INSERT INTO reactions(actor,target,kind) VALUES(1,2,'like')")
        self.s.db.execute('INSERT INTO matches(a,b) VALUES(1,2)')
        self.callback(1,'delete'); self.callback(1,'delete_confirm')
        self.assertEqual(self.s.db.execute('SELECT count(*) FROM users WHERE id=1').fetchone()[0],0)
        self.assertEqual(self.s.db.execute('SELECT count(*) FROM matches').fetchone()[0],0)
        self.assertEqual(self.s.db.execute('SELECT count(*) FROM reactions').fetchone()[0],0)
        self.assertEqual(self.s.get(1)['profile'],{})
    def test_admin_authorization(self):
        self.user(1)
        self.text(1,'/admin ban 1')
        self.assertFalse(self.s.get(1)['banned'])
        self.text(999,'/admin ban 1')
        self.assertTrue(self.s.get(1)['banned'])
    def test_dedup_durable_event_and_redaction(self):
        event = {'update_type':'bot_started','user':{'user_id':1},'timestamp':10}
        with self.s.db:
            enqueue(self.s,event); enqueue(self.s,event)
        process(self.s,self.bot)
        self.assertEqual(self.s.db.execute('SELECT count(*) FROM inbox').fetchone()[0],1)
        self.assertEqual(self.s.db.execute('SELECT payload FROM inbox').fetchone()[0],'{}')
        count = len(self.messages(1))
        process(self.s,self.bot)
        self.assertEqual(len(self.messages(1)),count)
    def test_event_transaction_rolls_back_on_failure(self):
        event = {'update_type':'bot_started','user':{'user_id':1},'timestamp':10}
        with self.s.db: enqueue(self.s,event)
        class FailingBot:
            def handle(inner,event):
                self.s.get(1); self.bot.say(1,'test')
                raise RuntimeError('test failure')
        with self.assertLogs('iskra',level='ERROR'):
            process(self.s,FailingBot())
        self.assertEqual(len(self.messages(1)),0)
        self.assertEqual(self.s.db.execute('SELECT done FROM inbox').fetchone()[0],0)
    def test_outbox_retry_then_delivery(self):
        self.bot.say(1,'Hello')
        self.s.db.commit()
        class FakeApi:
            fail = True
            def call(inner,*args,**kwargs):
                if inner.fail: raise ApiError(429)
                return {'message':{}}
        api = FakeApi()
        with self.assertLogs('iskra',level='WARNING'):
            deliver(self.s,api,{})
        self.assertEqual(self.s.db.execute('SELECT attempts FROM outbox').fetchone()[0],1)
        api.fail=False
        self.s.db.execute('UPDATE outbox SET next_try=0'); self.s.db.commit()
        deliver(self.s,api,{})
        self.assertEqual(self.s.db.execute('SELECT count(*) FROM outbox').fetchone()[0],0)

if __name__ == '__main__':
    unittest.main()
