"""Private Mac-local publication and durable admission. Never starts a process."""
import hashlib
import json
import math
import os
from pathlib import Path
import re
import sqlite3
import stat
import time
import uuid
from contextlib import contextmanager

MAX_REQUEST = 16384
ACTIONS = frozenset(('baseline-start', 'baseline-status', 'pre-scale-start',
    'pre-scale-status', 'zero-gate-start', 'zero-gate-status', 'pending-start',
    'pending-status', 'collector-launch', 'collector-status', 'reconcile-close',
    'scaleout-two-to-three'))
REQUEST_KEYS = frozenset(('requestId', 'runId', 'supervisorId', 'action',
    'feedSession', 'minimumSequence', 'receiptIdentity', 'target', 'bundleSha256',
    'nonce', 'deadlineMonotonic'))
TARGET_KEYS = frozenset(('accountId', 'regionId', 'workerId', 'scalingGroupId',
    'nlbServerGroupId', 'originalInstanceIds'))
CATEGORIES = frozenset(('cancelled', 'timeout', 'receipt', 'target', 'source',
    'child', 'storage', 'remote', 'incomplete', 'parse', 'configuration'))
TOKEN = re.compile(r'[A-Za-z0-9_-]{1,256}\Z')
SHA = re.compile(r'[0-9a-f]{64}\Z')


class Refused(Exception):
    """Address-free refusal category, safe to persist or display."""
    def __init__(self, category='configuration'):
        self.category = category
        super().__init__(category)


def require(value, category='configuration'):
    if not value:
        raise Refused(category)


def encode(value):
    try:
        return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode('ascii')
    except (ValueError, TypeError, UnicodeError):
        raise Refused() from None


def token(value):
    return type(value) is str and TOKEN.fullmatch(value) is not None


def sha(value):
    return type(value) is str and SHA.fullmatch(value) is not None


def identifier(value):
    try:
        require(type(value) is str and str(uuid.UUID(value)) == value)
    except (ValueError, AttributeError):
        raise Refused() from None


def target_valid(value):
    require(type(value) is dict and set(value) == TARGET_KEYS)
    require(all(token(value[key]) for key in TARGET_KEYS - {'originalInstanceIds'}))
    ids = value['originalInstanceIds']
    require(type(ids) is list and len(ids) == 2 and all(token(x) for x in ids) and ids[0] != ids[1])


def validate_request(value):
    require(type(value) is dict and set(value) == REQUEST_KEYS)
    require(len(encode(value)) <= MAX_REQUEST)
    for key in ('requestId', 'runId', 'supervisorId', 'feedSession', 'nonce'):
        identifier(value[key])
    require(type(value['action']) is str and value['action'] in ACTIONS)
    require(type(value['minimumSequence']) is int and value['minimumSequence'] > 0)
    require(sha(value['receiptIdentity']) and sha(value['bundleSha256']))
    require(type(value['deadlineMonotonic']) is float and math.isfinite(value['deadlineMonotonic']))
    target_valid(value['target'])


def private(path, directory=False):
    info = os.lstat(path)
    require(info.st_uid == os.getuid())
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode))
    require(stat.S_IMODE(info.st_mode) == (0o700 if directory else 0o600))
    if not directory:
        require(info.st_nlink == 1)


def write_all(fd, raw):
    while raw:
        count = os.write(fd, raw)
        require(count > 0, 'storage')
        raw = raw[count:]


def read_private(folder, name, maximum):
    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=folder)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_uid == os.getuid()
                and stat.S_IMODE(before.st_mode) == 0o600 and before.st_nlink == 1)
        require(before.st_size <= maximum)
        raw = b''
        while len(raw) <= maximum:
            part = os.read(fd, maximum + 1 - len(raw))
            if not part:
                break
            raw += part
        after = os.fstat(fd)
        current = os.stat(name, dir_fd=folder, follow_symlinks=False)
        identity = lambda s: (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns, s.st_ctime_ns)
        require(identity(before) == identity(after) == identity(current) and len(raw) <= maximum)
        return raw
    finally:
        os.close(fd)


def request_folder(root):
    private(root, True)
    path = Path(root)/'requests'
    try:
        path.mkdir(mode=0o700)
    except FileExistsError:
        pass
    private(path, True)
    return os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)


def publish_request(root: Path, request: dict) -> str:
    """Publish once; a ready marker is the sole visibility boundary.

    Failed publication intentionally leaves its one-use staging file behind.
    Caller creates the absolute monotonic deadline before calling this function.
    """
    validate_request(request)
    raw = encode(request)
    request_id = request['requestId']
    folder = request_folder(root)
    temporary = '.' + request_id + '.' + str(uuid.uuid4()) + '.ready'
    try:
        fd = os.open(request_id+'.request.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=folder)
        try:
            os.fchmod(fd, 0o600)
            write_all(fd, raw)
            os.fsync(fd)
        finally:
            os.close(fd)
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=folder)
        try:
            os.fchmod(fd, 0o600)
            write_all(fd, hashlib.sha256(raw).hexdigest().encode('ascii'))
            os.fsync(fd)
        finally:
            os.close(fd)
        # link is an atomic no-replace publication; remove its private staging name.
        os.link(temporary, request_id+'.ready', src_dir_fd=folder, dst_dir_fd=folder, follow_symlinks=False)
        os.unlink(temporary, dir_fd=folder)
        os.fsync(folder)
        return request_id
    except OSError:
        raise Refused('storage') from None
    finally:
        try:
            os.unlink(temporary, dir_fd=folder)
        except FileNotFoundError:
            pass
        os.close(folder)


def read_request(root, request_id):
    identifier(request_id)
    folder = request_folder(root)
    try:
        marker = read_private(folder, request_id+'.ready', 64)
        raw = read_private(folder, request_id+'.request.json', MAX_REQUEST)
        require(marker == hashlib.sha256(raw).hexdigest().encode('ascii'))
        value = json.loads(raw.decode('ascii'))
        validate_request(value)
        require(value['requestId'] == request_id)
        return value
    except (OSError, ValueError, UnicodeError):
        raise Refused('configuration') from None
    finally:
        os.close(folder)


def validate_result(result):
    require(type(result) is dict and set(result) == {'state', 'category', 'payload'})
    require(type(result['state']) is str and type(result['category']) is str)
    require((result['state'] == 'complete' and result['category'] == 'ok') or
            (result['state'] == 'uncertain' and result['category'] in CATEGORIES))
    payload = result['payload']
    require(type(payload) is dict and len(encode(payload)) <= 4096)
    require(set(payload) <= {'status','stage','phase','feedSession','sequence','resourceVersion','requestId','identity'})
    for key, value in payload.items():
        if key == 'identity':
            require(type(value) is dict and set(value) == {'pid','bootId','startTicks','commandSha256'})
            require(type(value['pid']) is int and value['pid'] > 0)
            require(type(value['startTicks']) is int and value['startTicks'] >= 0)
            require(token(value['bootId']) and sha(value['commandSha256']))
        elif key == 'sequence':
            require(type(value) is int and value > 0)
        else:
            require(token(value))


class Journal:
    def __init__(self, root: Path):
        self.root = Path(root)
        private(self.root, True)
        self.path = self.root/'journal.sqlite3'
        try:
            fd = os.open(self.path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        except FileExistsError:
            private(self.path)
        else:
            os.fchmod(fd, 0o600)
            os.fsync(fd)
            os.close(fd)
        with self._connection() as db:
            db.executescript('''CREATE TABLE IF NOT EXISTS run (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1), run_id TEXT NOT NULL,
                mode TEXT NOT NULL, target TEXT NOT NULL, hashes TEXT NOT NULL,
                state TEXT NOT NULL CHECK(state IN ('active','cancelled','uncertain')),
                supervisor TEXT, session TEXT);
                CREATE TABLE IF NOT EXISTS requests (
                request_id TEXT PRIMARY KEY, run_id TEXT NOT NULL, nonce TEXT UNIQUE NOT NULL,
                request TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('dispatching','complete','uncertain')),
                result TEXT);''')
        folder = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(folder)
        finally:
            os.close(folder)

    @contextmanager
    def _connection(self):
        private(self.path)
        db = sqlite3.connect(str(self.path), isolation_level=None, timeout=1)
        db.row_factory = sqlite3.Row
        try:
            db.execute('PRAGMA journal_mode=DELETE')
            db.execute('PRAGMA synchronous=FULL')
            yield db
        except sqlite3.Error:
            raise Refused('storage') from None
        finally:
            db.close()

    @contextmanager
    def _transaction(self):
        with self._connection() as db:
            db.execute('BEGIN IMMEDIATE')
            try:
                yield db
                db.execute('COMMIT')
            except BaseException:
                if db.in_transaction:
                    db.execute('ROLLBACK')
                raise

    def create_run(self, run_id: str, mode: str, target: dict, hashes: dict) -> None:
        identifier(run_id)
        require(mode in ('no-scale', 'disabled-rehearsal'))
        target_valid(target)
        require(type(hashes) is dict and 'bundleSha256' in hashes and len(hashes) <= 64)
        require(all(token(k) and sha(v) for k, v in hashes.items()))
        with self._transaction() as db:
            require(db.execute('SELECT 1 FROM run').fetchone() is None)
            db.execute('INSERT INTO run VALUES (1,?,?,?,?,?,?,?)',
                       (run_id, mode, encode(target).decode(), encode(hashes).decode(), 'active', None, None))

    def admit(self, request: dict) -> None:
        validate_request(request)
        published = read_request(self.root, request['requestId'])
        require(encode(request) == encode(published))
        with self._transaction() as db:
            run = db.execute('SELECT * FROM run').fetchone()
            require(run is not None and run['state'] == 'active')
            require(request['runId'] == run['run_id'])
            require(request['target'] == json.loads(run['target']), 'target')
            require(request['bundleSha256'] == json.loads(run['hashes'])['bundleSha256'], 'source')
            require(request['action'] != 'scaleout-two-to-three' or
                    (run['mode'] == 'disabled-rehearsal' and
                     request['target']['scalingGroupId'] == 'asg-t4necb4dfmu6o3z2rn0g'))
            require(request['deadlineMonotonic'] > time.monotonic(), 'timeout')
            require(run['supervisor'] in (None, request['supervisorId']))
            require(run['session'] in (None, request['feedSession']))
            require(db.execute("SELECT 1 FROM requests WHERE state='dispatching'").fetchone() is None)
            require(db.execute('SELECT 1 FROM requests WHERE request_id=? OR nonce=?',
                               (request['requestId'],request['nonce'])).fetchone() is None)
            db.execute('UPDATE run SET supervisor=?, session=?', (request['supervisorId'],request['feedSession']))
            db.execute('INSERT INTO requests VALUES (?,?,?,?,?,NULL)',
                       (request['requestId'], request['runId'], request['nonce'], encode(request).decode(), 'dispatching'))

    def seal(self, request_id: str, result: dict) -> None:
        identifier(request_id)
        validate_result(result)
        with self._transaction() as db:
            row = db.execute('SELECT state FROM requests WHERE request_id=?', (request_id,)).fetchone()
            require(row is not None and row['state'] == 'dispatching')
            run = db.execute('SELECT state FROM run').fetchone()
            require(result['state'] != 'complete' or run['state'] == 'active')
            db.execute('UPDATE requests SET state=?, result=? WHERE request_id=?',
                       (result['state'], encode(result).decode(), request_id))
            if result['state'] == 'uncertain':
                db.execute("UPDATE run SET state='uncertain' WHERE state='active'")

    def cancel(self, category: str) -> None:
        require(category in CATEGORIES)
        with self._transaction() as db:
            require(db.execute('SELECT 1 FROM run').fetchone() is not None)
            db.execute("UPDATE run SET state=? WHERE state='active'",
                       ('cancelled' if category == 'cancelled' else 'uncertain',))

    def snapshot(self, request_id: str) -> dict:
        identifier(request_id)
        with self._transaction() as db:
            row = db.execute('SELECT * FROM requests WHERE request_id=?', (request_id,)).fetchone()
            require(row is not None)
            run = db.execute('SELECT * FROM run').fetchone()
            return dict(requestId=request_id, runId=row['run_id'], state=row['state'],
                        runState=run['state'], mode=run['mode'], target=json.loads(run['target']),
                        hashes=json.loads(run['hashes']), request=json.loads(row['request']),
                        result=json.loads(row['result']) if row['result'] else None)
