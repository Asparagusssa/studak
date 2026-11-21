import json
import sqlite3
from pathlib import Path

SCHEMA = '''
PRAGMA foreign_keys=ON;
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS users (
 id INTEGER PRIMARY KEY, profile TEXT NOT NULL DEFAULT '{}',
 state TEXT NOT NULL DEFAULT '{}', active INTEGER NOT NULL DEFAULT 0,
 banned INTEGER NOT NULL DEFAULT 0, created TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS reactions (
 actor INTEGER REFERENCES users(id) ON DELETE CASCADE,
 target INTEGER REFERENCES users(id) ON DELETE CASCADE,
 kind TEXT NOT NULL, note TEXT DEFAULT '', created TEXT DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(actor,target)
);
CREATE TABLE IF NOT EXISTS matches (
 a INTEGER REFERENCES users(id) ON DELETE CASCADE,
 b INTEGER REFERENCES users(id) ON DELETE CASCADE,
 created TEXT DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY(a,b)
);
CREATE TABLE IF NOT EXISTS blocks (
 actor INTEGER REFERENCES users(id) ON DELETE CASCADE,
 target INTEGER REFERENCES users(id) ON DELETE CASCADE, PRIMARY KEY(actor,target)
);
CREATE TABLE IF NOT EXISTS reports (
 id INTEGER PRIMARY KEY, actor INTEGER REFERENCES users(id) ON DELETE CASCADE,
 target INTEGER REFERENCES users(id) ON DELETE CASCADE, reason TEXT,
 resolved INTEGER DEFAULT 0, created TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS inbox (
 key TEXT PRIMARY KEY, payload TEXT NOT NULL, done INTEGER DEFAULT 0,
 attempts INTEGER DEFAULT 0, next_try REAL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS outbox (
 id INTEGER PRIMARY KEY, uid INTEGER NOT NULL, body TEXT NOT NULL,
 attempts INTEGER DEFAULT 0, next_try REAL DEFAULT 0, dead INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
'''

class Store:
    def __init__(self, path):
        if path != ':memory:':
            Path(path).parent.mkdir(parents=True, exist_ok=True)
        self.db = sqlite3.connect(path)
        self.db.row_factory = sqlite3.Row
        self.db.executescript(SCHEMA)

    def get(self, uid):
        row = self.db.execute('SELECT * FROM users WHERE id=?', (uid,)).fetchone()
        if row is None:
            self.db.execute('INSERT INTO users(id) VALUES(?)', (uid,))
            row = self.db.execute('SELECT * FROM users WHERE id=?', (uid,)).fetchone()
        return dict(row, profile=json.loads(row['profile']), state=json.loads(row['state']))

    def save(self, user):
        self.db.execute('UPDATE users SET profile=?,state=?,active=?,banned=? WHERE id=?',
                        (json.dumps(user['profile'], ensure_ascii=False), json.dumps(user['state']),
                         user['active'], user['banned'], user['id']))

    def send(self, uid, body):
        self.db.execute('INSERT INTO outbox(uid,body) VALUES(?,?)',
                        (uid, json.dumps(body, ensure_ascii=False)))

    def blocked(self, a, b):
        return self.db.execute('SELECT 1 FROM blocks WHERE (actor=? AND target=?) OR (actor=? AND target=?)',
                               (a,b,b,a)).fetchone() is not None

    def matched(self, a, b):
        return self.db.execute('SELECT 1 FROM matches WHERE a=? AND b=?', tuple(sorted((a,b)))).fetchone() is not None
