"""Atomic local queue: ownership never expires and claimed work never requeues."""
from contextlib import contextmanager
from dataclasses import asdict
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import sqlite3
import uuid
from .models import (Blocked, Conflict, NotFound, ValidationError, TaskBinding, OutcomeReport,
                     STAGES, canonical, digest, identifier, validate_app, validate_snapshot, validate_submission)
from tools.sandbox_lifecycle.state import protect_dir, safe_path


def now():
    return datetime.now(timezone.utc).isoformat()


class Store:
    def __init__(self, root: Path):
        self.root = protect_dir(root)
        self.path = safe_path(self.root / 'tasks.sqlite3')
        for suffix in ('', '-wal', '-shm', '-journal'):
            safe_path(Path(str(self.path) + suffix))
        fd = os.open(self.path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        os.fchmod(fd, 0o600)
        os.close(fd)
        with self.connection() as db:
            version = db.execute('PRAGMA user_version').fetchone()[0]
            if version not in (0, 1):
                raise ValueError('StoreUnavailable')
            if version == 1:
                return
            db.executescript('''
                CREATE TABLE IF NOT EXISTS apps(id TEXT PRIMARY KEY, value TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS tasks(id TEXT PRIMARY KEY, app_id TEXT NOT NULL REFERENCES apps(id),
                    request_key TEXT UNIQUE NOT NULL, request_hash TEXT NOT NULL, value TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS attempts(sequence INTEGER PRIMARY KEY AUTOINCREMENT,
                    id TEXT UNIQUE NOT NULL, task_id TEXT NOT NULL REFERENCES tasks(id), state TEXT NOT NULL,
                    retry_key TEXT UNIQUE, value TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,
                    attempt_id TEXT NOT NULL REFERENCES attempts(id), stage TEXT NOT NULL, timestamp TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS dispatch_gate(id INTEGER PRIMARY KEY CHECK(id=1), value TEXT);
                INSERT OR IGNORE INTO dispatch_gate(id,value) VALUES(1,NULL);
                CREATE TABLE IF NOT EXISTS recovery_requests(key TEXT PRIMARY KEY, binding TEXT NOT NULL,
                    state TEXT NOT NULL, sequence INTEGER NOT NULL);
                PRAGMA user_version=1;
            ''')

    @contextmanager
    def connection(self):
        db = None
        try:
            for suffix in ('', '-wal', '-shm', '-journal'):
                p = safe_path(Path(str(self.path) + suffix))
                if p.exists():
                    os.chmod(p, 0o600)
            db = sqlite3.connect(self.path, timeout=5)
            db.row_factory = sqlite3.Row
            db.execute('PRAGMA foreign_keys=ON')
            db.execute('PRAGMA journal_mode=WAL')
            db.execute('PRAGMA synchronous=FULL')
            db.execute('BEGIN IMMEDIATE')
            yield db
            db.commit()
        except sqlite3.Error:
            if db:
                db.rollback()
            raise ValueError('StoreUnavailable') from None
        finally:
            if db:
                db.close()
            for suffix in ('', '-wal', '-shm', '-journal'):
                p = safe_path(Path(str(self.path) + suffix))
                if p.exists():
                    os.chmod(p, 0o600)

    def _get(self, db, table, key):
        identifier(key)
        row = db.execute(f'SELECT value FROM {table} WHERE id=?', (key,)).fetchone()
        if row is None:
            raise NotFound()
        return json.loads(row['value'])

    def create_app(self, value):
        record = dict(validate_app(value), app_id=str(uuid.uuid4()), version=1,
                      updated_at=now(), provenance='owner-entered')
        validate_snapshot(record)
        with self.connection() as db:
            db.execute('INSERT INTO apps VALUES(?,?)', (record['app_id'], canonical(record).decode()))
        return record

    def update_app(self, app_id, expected_version, value):
        if type(expected_version) is not int or expected_version < 1:
            raise ValidationError()
        value = validate_app(value)
        with self.connection() as db:
            old = self._get(db, 'apps', app_id)
            if old['version'] != expected_version:
                raise Conflict()
            record = dict(value, app_id=app_id, version=expected_version + 1,
                          updated_at=now(), provenance='owner-entered')
            validate_snapshot(record)
            db.execute('UPDATE apps SET value=? WHERE id=?', (canonical(record).decode(), app_id))
        return record

    def list_apps(self):
        with self.connection() as db:
            return [json.loads(row[0]) for row in db.execute('SELECT value FROM apps ORDER BY rowid')]

    def get_app(self, app_id):
        with self.connection() as db:
            return self._get(db, 'apps', app_id)

    def _attempt(self, db, task_id, retry_key=None):
        record = dict(task_id=task_id, attempt_id=str(uuid.uuid4()), state='queued',
                      stage='queued', submitted_at=now(), outcomes=None, answer=None)
        db.execute('INSERT INTO attempts(id,task_id,state,retry_key,value) VALUES(?,?,?,?,?)',
                   (record['attempt_id'], task_id, 'queued', retry_key, canonical(record).decode()))
        self._event(db, record['attempt_id'], 'queued')
        return record

    def submit(self, value):
        value = validate_submission(value)
        request_hash = digest({k: v for k, v in value.items() if k != 'idempotency_key'})
        with self.connection() as db:
            old = db.execute('SELECT id,request_hash FROM tasks WHERE request_key=?', (value['idempotency_key'],)).fetchone()
            if old:
                if old['request_hash'] != request_hash:
                    raise Conflict()
                return self._task(db, old['id'])
            snapshot = validate_snapshot(self._get(db, 'apps', value['app_id']))
            record = dict(task_id=str(uuid.uuid4()), app_id=value['app_id'], question=value['question'],
                          model=value['model'], snapshot=snapshot, snapshot_sha256=digest(snapshot), submitted_at=now())
            db.execute('INSERT INTO tasks VALUES(?,?,?,?,?)', (record['task_id'], record['app_id'],
                       value['idempotency_key'], request_hash, canonical(record).decode()))
            self._attempt(db, record['task_id'])
            return self._task(db, record['task_id'])

    def _task(self, db, task_id):
        task = self._get(db, 'tasks', task_id)
        attempts = [json.loads(r['value']) for r in db.execute('SELECT value FROM attempts WHERE task_id=? ORDER BY sequence', (task_id,))]
        for attempt in attempts:
            attempt['events'] = [dict(r) for r in db.execute('SELECT stage,timestamp FROM events WHERE attempt_id=? ORDER BY sequence', (attempt['attempt_id'],))]
        task['attempts'] = attempts
        task['blocked'] = db.execute('SELECT value FROM dispatch_gate WHERE id=1').fetchone()[0] is not None
        return task

    def get_task(self, task_id):
        with self.connection() as db:
            return self._task(db, task_id)

    def list_tasks(self, app_id=None):
        with self.connection() as db:
            if app_id:
                self._get(db, 'apps', app_id)
            rows = db.execute('SELECT id FROM tasks WHERE (? IS NULL OR app_id=?) ORDER BY rowid', (app_id, app_id)).fetchall()
            summaries = []
            for row in rows:
                task = self._task(db, row['id'])
                summaries.append({k: v for k, v in task.items() if k not in ('question', 'snapshot', 'attempts')}
                                 | {'state': task['attempts'][-1]['state'], 'stage': task['attempts'][-1]['stage']})
            return summaries

    def _save_attempt(self, db, record):
        db.execute('UPDATE attempts SET state=?,value=? WHERE id=?',
                   (record['state'], canonical(record).decode(), record['attempt_id']))

    def _event(self, db, attempt_id, stage):
        if stage not in STAGES:
            raise ValidationError()
        db.execute('INSERT INTO events(attempt_id,stage,timestamp) VALUES(?,?,?)', (attempt_id, stage, now()))

    def _bound(self, db, binding):
        task = self._get(db, 'tasks', binding.task_id)
        attempt = self._get(db, 'attempts', binding.attempt_id)
        if attempt['task_id'] != binding.task_id or task['snapshot_sha256'] != binding.snapshot_sha256:
            raise Conflict()
        return attempt

    def claim_next(self):
        with self.connection() as db:
            if db.execute('SELECT value FROM dispatch_gate WHERE id=1').fetchone()[0] is not None:
                return None
            if db.execute("SELECT 1 FROM attempts WHERE state IN ('claimed','blocked') LIMIT 1").fetchone():
                return None
            row = db.execute("SELECT value FROM attempts WHERE state='queued' ORDER BY sequence LIMIT 1").fetchone()
            if not row:
                return None
            record = dict(json.loads(row[0]), state='claimed', stage='starting')
            self._save_attempt(db, record)
            self._event(db, record['attempt_id'], 'starting')
            return record

    def cancel(self, task_id, attempt_id):
        with self.connection() as db:
            self._get(db, 'tasks', task_id)
            attempt = self._get(db, 'attempts', attempt_id)
            if attempt['task_id'] != task_id or attempt['state'] != 'queued':
                raise Conflict()
            attempt.update(state='cancelled', stage='finished')
            self._save_attempt(db, attempt)
            self._event(db, attempt_id, 'finished')

    def retry(self, task_id, idempotency_key):
        identifier(idempotency_key)
        with self.connection() as db:
            self._get(db, 'tasks', task_id)
            old = db.execute('SELECT task_id,value FROM attempts WHERE retry_key=?', (idempotency_key,)).fetchone()
            if old:
                if old['task_id'] != task_id:
                    raise Conflict()
                return json.loads(old['value'])
            if db.execute('SELECT value FROM dispatch_gate WHERE id=1').fetchone()[0] is not None:
                raise Blocked()
            prior = json.loads(db.execute('SELECT value FROM attempts WHERE task_id=? ORDER BY sequence DESC LIMIT 1', (task_id,)).fetchone()[0])
            if prior['state'] != 'failed' or not prior['outcomes'] or prior['outcomes']['cleanup'] != 'confirmed':
                raise Conflict()
            return self._attempt(db, task_id, idempotency_key)

    def append_event(self, binding, stage):
        with self.connection() as db:
            attempt = self._bound(db, binding)
            if attempt['state'] != 'claimed':
                raise Conflict()
            self._event(db, binding.attempt_id, stage)
            attempt['stage'] = stage
            self._save_attempt(db, attempt)

    def block(self, binding, error):
        OutcomeReport('unknown', 'unknown', 'unknown', 'blocked', error)
        with self.connection() as db:
            if binding:
                attempt = self._bound(db, binding)
                attempt['state'] = 'blocked'
                self._save_attempt(db, attempt)
            gate = dict(binding=asdict(binding) if binding else None, error=error)
            db.execute('UPDATE dispatch_gate SET value=? WHERE id=1', (canonical(gate).decode(),))

    def finish(self, binding, report, answer):
        if not isinstance(report, OutcomeReport):
            raise ValidationError()
        if answer is not None and (type(answer) is not str or not answer.strip() or len(answer.encode('utf-8')) > 16384):
            raise ValidationError()
        if report.answer == 'completed' and answer is None:
            raise ValidationError()
        with self.connection() as db:
            attempt = self._bound(db, binding)
            if attempt['state'] not in ('claimed', 'blocked', 'failed', 'completed'):
                raise Conflict()
            attempt.update(state=report.overall, stage='finished', outcomes=asdict(report), answer=answer)
            self._save_attempt(db, attempt)
            self._event(db, binding.attempt_id, 'finished')
            if report.overall == 'blocked':
                gate = dict(binding=asdict(binding), error=report.error)
                db.execute('UPDATE dispatch_gate SET value=? WHERE id=1', (canonical(gate).decode(),))

    def unfinished(self):
        with self.connection() as db:
            return [json.loads(r[0]) for r in db.execute("SELECT value FROM attempts WHERE state IN ('claimed','blocked') ORDER BY sequence")]

    def request_recovery(self, binding, idempotency_key):
        identifier(idempotency_key)
        encoded = canonical(asdict(binding)).decode()
        with self.connection() as db:
            self._bound(db, binding)
            old = db.execute('SELECT binding FROM recovery_requests WHERE key=?', (idempotency_key,)).fetchone()
            if old:
                if old[0] != encoded:
                    raise Conflict()
                return
            gate = db.execute('SELECT value FROM dispatch_gate WHERE id=1').fetchone()[0]
            if gate is None or json.loads(gate)['binding'] != asdict(binding):
                raise Conflict()
            sequence = db.execute('SELECT COALESCE(MAX(sequence),0)+1 FROM recovery_requests').fetchone()[0]
            db.execute('INSERT INTO recovery_requests VALUES(?,?,?,?)', (idempotency_key, encoded, 'queued', sequence))

    def claim_recovery(self):
        with self.connection() as db:
            row = db.execute("SELECT key,binding FROM recovery_requests WHERE state IN ('queued','claimed') ORDER BY sequence LIMIT 1").fetchone()
            if not row:
                return None
            db.execute("UPDATE recovery_requests SET state='claimed' WHERE key=?", (row['key'],))
            return TaskBinding(**json.loads(row['binding']))

    def clear_gate(self, binding):
        self.finish_recovery(binding, cleared=True)

    def gate(self):
        with self.connection() as db:
            value = db.execute('SELECT value FROM dispatch_gate WHERE id=1').fetchone()[0]
            return json.loads(value) if value else None

    def finish_recovery(self, binding, *, cleared):
        with self.connection() as db:
            if binding:
                self._bound(db, binding)
            gate = db.execute('SELECT value FROM dispatch_gate WHERE id=1').fetchone()[0]
            expected = asdict(binding) if binding else None
            if gate is not None and json.loads(gate)['binding'] != expected:
                raise Conflict()
            if cleared:
                db.execute('UPDATE dispatch_gate SET value=NULL WHERE id=1')
            if binding:
                db.execute("UPDATE recovery_requests SET state='finished' WHERE binding=?", (canonical(expected).decode(),))
