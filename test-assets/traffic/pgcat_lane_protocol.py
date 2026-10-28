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
    'pending-status', 'pending-proof', 'collector-launch', 'collector-status', 'lane-verdict', 'reconcile-close',
    'scaleout-two-to-three', 'pre-drain-proof', 'deregister-new-node',
    'zero-proof', 'scaling-activity-check', 'scalein-three-to-two'))
NO_SCALE_ACTIONS = frozenset(('baseline-start','baseline-status',
    'collector-launch','collector-status','lane-verdict'))
REQUEST_KEYS = frozenset(('requestId', 'runId', 'supervisorId', 'action',
    'feedSession', 'minimumSequence', 'receiptIdentity', 'target', 'bundleSha256',
    'nonce', 'deadlineMonotonic'))
TARGET_KEYS = frozenset(('accountId', 'regionId', 'workerId', 'scalingGroupId',
    'nlbServerGroupId', 'originalInstanceIds'))
CATEGORIES = frozenset(('cancelled', 'timeout', 'receipt', 'target', 'source',
    'child', 'storage', 'remote', 'incomplete', 'parse', 'configuration'))
TOKEN = re.compile(r'[A-Za-z0-9_-]{1,256}\Z')
SHA = re.compile(r'[0-9a-f]{64}\Z')
ROSTER_KEYS = frozenset(('members','healthyRegistered','memberStates','backendStates'))


def monotonic():
    """Host-wide deadline clock, including macOS Python 3.9.

    Callers MUST create deadlineMonotonic values with this clock. Python 3.9's
    macOS time.monotonic() uses a separate origin in each process.
    """
    return time.clock_gettime(time.CLOCK_MONOTONIC)


class Refused(Exception):
    """Address-free refusal category, safe to persist or display."""
    def __init__(self, category='configuration'):
        self.category = category
        super().__init__(category)


def require(value, category='configuration'):
    if not value:
        raise Refused(category)


def _release_admission(mode, action=None):
    """This release has only the completed no-scale runtime capability."""
    require(mode == 'no-scale', 'configuration')
    require(action is None or action in NO_SCALE_ACTIONS, 'configuration')


def encode(value):
    try:
        return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode('ascii')
    except (ValueError, TypeError, UnicodeError):
        raise Refused() from None


def roster_sha(value):
    """Bind a receipt to one provider publication without retaining addresses."""
    require(type(value) is dict and ROSTER_KEYS <= set(value), 'receipt')
    return hashlib.sha256(encode({key:value[key] for key in ROSTER_KEYS})).hexdigest()


def expected_roster(originals, candidate, *, registered):
    require(type(originals) is list and len(originals) == 2 and
            all(token(node) for node in originals) and originals[0] != originals[1], 'target')
    require(token(candidate) and candidate not in originals, 'target')
    members=sorted(originals+[candidate])
    healthy=members if registered else sorted(originals)
    return dict(members=members,healthyRegistered=healthy,
        memberStates=[dict(id=node,lifecycle='InService' if node == candidate else 'Protected',
            health='Healthy',addressKnown=True)
            for node in members],
        backendStates=[dict(id=node,status='Available',weight=100,listenerHealth='Normal')
            for node in healthy])


def removing_roster_shas(originals,candidate):
    """Only the candidate may transition to NLB Removing after its ACK."""
    base=expected_roster(originals,candidate,registered=True)
    base['healthyRegistered']=sorted(originals)
    row=next(item for item in base['backendStates'] if item['id'] == candidate)
    digests=set()
    for weight in (0,100):
        for health in ('Normal','Initial','Unhealthy','Unavailable'):
            row.update(status='Removing',weight=weight,listenerHealth=health)
            digests.add(roster_sha(base))
    return frozenset(digests)


def original_roster(originals):
    require(type(originals) is list and len(originals) == 2 and
            originals == sorted(originals) and all(token(node) for node in originals) and
            originals[0] != originals[1], 'target')
    return dict(members=originals,healthyRegistered=originals,
        memberStates=[dict(id=node,lifecycle='Protected',health='Healthy',addressKnown=True)
                      for node in originals],
        backendStates=[dict(id=node,status='Available',weight=100,listenerHealth='Normal')
                       for node in originals])


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


def read_private(folder, name, maximum, publication_marker=False):
    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=folder)
    try:
        before = os.fstat(fd)
        def valid(info):
            return (stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid()
                    and stat.S_IMODE(info.st_mode) == 0o600
                    and info.st_nlink in ((1, 2) if publication_marker else (1,)))
        require(valid(before))
        require(before.st_size <= maximum)
        raw = b''
        while len(raw) <= maximum:
            part = os.read(fd, maximum + 1 - len(raw))
            if not part:
                break
            raw += part
        after = os.fstat(fd)
        current = os.stat(name, dir_fd=folder, follow_symlinks=False)
        require(valid(after) and valid(current))
        # Atomic link publication briefly has two names; removing the staging
        # name changes ctime without changing the complete fsynced marker bytes.
        def identity(info):
            stable = (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns)
            return stable if publication_marker else stable + (info.st_ctime_ns,)
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
    temporary = '.' + request_id + '.' + str(uuid.uuid4()) + '.staging'
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
        # Persist the observed no-replace link even if its publisher is paused.
        os.fsync(folder)
        marker = read_private(folder, request_id+'.ready', 64, publication_marker=True)
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
    require(set(payload) <= {'status','stage','phase','feedSession','sequence','resourceVersion','requestId','jobId','identity',
                            'runId','newId','originalInstanceIds','observedAt','clientRosterSha256',
                            'evidenceSha256','sourceSha256','pendingBindingSha256',
                            'preDrainBindingSha256','exit','category','verdictSha256','clientIdentitySha256','workerCredentialCleanupSha256'})
    for key, value in payload.items():
        if key == 'exit':
            require(type(value) is dict and set(value)=={'category','exitCode','completionSha256'})
            require(value['category']=='completed' and type(value['exitCode']) is int and
                    value['exitCode']==0 and sha(value['completionSha256']))
        elif key == 'identity':
            require(type(value) is dict and set(value) == {'pid','bootId','startTicks','commandSha256'})
            require(type(value['pid']) is int and value['pid'] > 0)
            require(type(value['startTicks']) is int and value['startTicks'] >= 0)
            require(token(value['bootId']) and sha(value['commandSha256']))
        elif key == 'sequence':
            require(type(value) is int and value > 0)
        elif key == 'originalInstanceIds':
            require(type(value) is list and len(value) == 2 and all(token(x) for x in value)
                    and value[0] != value[1])
        elif key == 'observedAt':
            require(type(value) in (int,float) and math.isfinite(value))
        elif key=='category':
            require(value=='lane-timing')
        elif key in ('clientIdentitySha256','workerCredentialCleanupSha256'):
            require(sha(value) or (value is None and payload.get('category')=='lane-timing' and
                    payload.get('status') in ('FAIL','INCONCLUSIVE')))
        elif key=='verdictSha256':
            require(sha(value))
        elif key in ('clientRosterSha256','evidenceSha256','sourceSha256','pendingBindingSha256'):
            require(sha(value))
        else:
            require(token(value))


NO_SCALE_STOP_INTENT = 'no-scale-stop.intent'
NO_SCALE_STOP_ACK = 'no-scale-stop.ack'


def validate_no_scale_stop(value):
    """One address-free stop request; paths and commands are never inputs."""
    require(type(value) is dict and set(value) == {'schemaVersion','runId','feedSession',
            'minimumSequence','verdictSha256'}, 'receipt')
    require(type(value['schemaVersion']) is int and value['schemaVersion'] == 1, 'receipt')
    identifier(value['runId']); identifier(value['feedSession'])
    require(type(value['minimumSequence']) is int and value['minimumSequence'] > 0 and
            sha(value['verdictSha256']), 'receipt')


def no_scale_stop_ack(intent, receipt):
    validate_no_scale_stop(intent)
    require(type(receipt) is dict and receipt.get('feedSession') == intent['feedSession'] and
            type(receipt.get('sequence')) is int and
            intent['minimumSequence'] <= receipt['sequence'] <= intent['minimumSequence']+1, 'receipt')
    return dict(schemaVersion=1,runId=intent['runId'],feedSession=intent['feedSession'],
        minimumSequence=intent['minimumSequence'],verdictSha256=intent['verdictSha256'],
        intentSha256=hashlib.sha256(encode(intent)).hexdigest(),
        finalReceiptSha256=hashlib.sha256(encode(receipt)).hexdigest(),sequence=receipt['sequence'])


NO_SCALE_PROOF = 'no-scale-proof.json'


def digest(value):
    return hashlib.sha256(encode(value)).hexdigest()


def private_json(path):
    path=Path(path);private(path.parent,True)
    fd=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:
        raw=read_private(fd,path.name,MAX_REQUEST)
        def unique(rows):
            value={}
            for key,item in rows:
                require(key not in value,'parse');value[key]=item
            return value
        return json.loads(raw.decode('ascii'),object_pairs_hook=unique),raw
    finally:os.close(fd)


def no_scale_assessment(record, run, worker, feed_exit, now=None):
    """Recompute the only admissible final status from sealed inputs."""
    require(type(record) is dict and set(record)=={'schemaVersion','day','runId','feedSession',
        'targetSha256','sourceSha256','sourceHashes','clientIdentitySha256','bindingSha256',
        'workerVerdict','stop','localCredentialCleanup'},'source')
    require(type(record['schemaVersion']) is int and record['schemaVersion']==1,'source')
    require(record['runId']==run['runId'] and record['feedSession']==feed_exit['feedSession'] and
            record['targetSha256']==digest(run['target']) and sha(record['bindingSha256']),'source')
    hashes=record['sourceHashes']
    require(type(hashes) is dict and set(hashes)=={'lane','local'} and
            dict(hashes['lane'],bundleSha256=digest(hashes['lane']))==run['hashes'] and
            record['sourceSha256']==digest(hashes),'source')
    require(type(hashes['local']) is dict and set(hashes['local'])=={'controller','client','runner'} and
            all(sha(v) for v in hashes['local'].values()),'source')
    require(encode(record['workerVerdict'])==encode(worker),'source')
    status='INCONCLUSIVE'
    if worker is not None:
        require(type(worker['sequence']) is int and worker['sequence']>0 and
                worker['runId']==run['runId'] and worker['feedSession']==feed_exit['feedSession'] and
                worker['sourceSha256']==hashes['lane']['laneTimingAssessor'],'source')
        payload=worker['payload'];validate_result(dict(state='complete',category='ok',payload=payload))
        require(set(payload)=={'status','category','verdictSha256','clientIdentitySha256','workerCredentialCleanupSha256'} and
                payload['category']=='lane-timing' and payload['status'] in ('PASS','FAIL','INCONCLUSIVE'),'source')
        require(record['clientIdentitySha256']==payload['clientIdentitySha256'],'source')
        collector=worker['collectorExit']
        require(collector['runId']==run['runId'] and collector['feedSession']==feed_exit['feedSession'] and
                type(collector['sequence']) is int and collector['sequence']<worker['sequence'],'source')
        out=collector['payload'];validate_result(dict(state='complete',category='ok',payload=out))
        require(out['status']=='EXITED' and out['exit']['category']=='completed' and
                type(out['exit']['exitCode']) is int and out['exit']['exitCode']==0,'child')
        status=payload['status']
    else:require(record['clientIdentitySha256'] is None,'source')
    stop=record['stop'];complete=False
    if stop is not None:
        require(worker is not None and type(stop) is dict and set(stop)=={'intent','ack','receipt','feedExit'},'receipt')
        intent=stop['intent'];receipt=stop['receipt'];validate_no_scale_stop(intent)
        require(intent['runId']==run['runId'] and intent['feedSession']==feed_exit['feedSession'] and
                intent['verdictSha256']==worker['payload']['verdictSha256'] and
                worker['sequence']<intent['minimumSequence'],'receipt')
        require(encode(stop['ack'])==encode(no_scale_stop_ack(intent,receipt)) and
                encode(stop['feedExit'])==encode(feed_exit),'receipt')
        require(set(receipt)=={'feedSession','sequence','desired','mode','observedAt','rosterSha256'} and
                receipt['mode']=='disabled-rehearsal' and type(receipt['desired']) is int and receipt['desired']==2 and
                receipt['rosterSha256']==roster_sha(original_roster(run['target']['originalInstanceIds'])) and
                receipt['sequence']==feed_exit['sequence'],'receipt')
        require(type(receipt['observedAt']) in (int,float) and math.isfinite(receipt['observedAt']),'receipt')
        if now is not None:require(0<=now-receipt['observedAt']<30,'receipt')
        complete=type(feed_exit['exitCode']) is int and feed_exit['exitCode']==0 and feed_exit['category']=='ok'
    require(type(record['localCredentialCleanup']) is bool,'source')
    if status!='FAIL' and not (complete and record['localCredentialCleanup']):status='INCONCLUSIVE'
    assessment=dict(status=status,mode='no-scale',feedSession=feed_exit['feedSession'],
                    endSequence=feed_exit['sequence'],evidenceSha256=digest(record))
    validate_finish_receipts(feed_exit,assessment,'no-scale')
    return assessment


def validate_finish_receipts(feed_exit, assessment, mode):
    require(type(feed_exit) is dict and set(feed_exit) ==
            {'pid','exitCode','category','feedSession','sequence'})
    require(type(feed_exit['pid']) is int and feed_exit['pid'] > 0)
    require(type(feed_exit['exitCode']) is int)
    require(type(feed_exit['category']) is str and feed_exit['category'] in CATEGORIES | {'ok'})
    identifier(feed_exit['feedSession'])
    require(type(feed_exit['sequence']) is int and feed_exit['sequence'] > 0)
    require(type(assessment) is dict and set(assessment) ==
            {'status','mode','feedSession','endSequence','evidenceSha256'})
    require(type(assessment['status']) is str and assessment['status'] in ('PASS','FAIL','INCONCLUSIVE'))
    require(assessment['mode'] == mode and assessment['feedSession'] == feed_exit['feedSession'])
    require(type(assessment['endSequence']) is int and assessment['endSequence'] == feed_exit['sequence'])
    require(sha(assessment['evidenceSha256']))


def publish_finish(root, run_id, feed_exit, assessment):
    """Publish immutable receipts followed by one atomic no-replace marker."""
    identifier(run_id)
    private(root, True)
    folder = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    temporary = '.' + str(uuid.uuid4()) + '.finish'
    try:
        marker = {'runId':run_id}
        for name, key, value in [('feed-exit.json','feedExitSha256',feed_exit),
                                 ('assessment.json','assessmentSha256',assessment)]:
            raw = encode(value)
            require(len(raw) <= MAX_REQUEST)
            fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=folder)
            try:
                os.fchmod(fd, 0o600); write_all(fd, raw); os.fsync(fd)
            finally: os.close(fd)
            marker[key] = hashlib.sha256(raw).hexdigest()
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=folder)
        try:
            os.fchmod(fd, 0o600); write_all(fd, encode(marker)); os.fsync(fd)
        finally: os.close(fd)
        os.link(temporary, 'finish.ready', src_dir_fd=folder, dst_dir_fd=folder, follow_symlinks=False)
        os.unlink(temporary, dir_fd=folder)
        os.fsync(folder)
    except OSError:
        raise Refused('storage') from None
    finally:
        try: os.unlink(temporary, dir_fd=folder)
        except FileNotFoundError: pass
        os.close(folder)


def read_finish(root):
    private(root, True)
    folder = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(folder)
        marker = json.loads(read_private(folder, 'finish.ready', MAX_REQUEST, publication_marker=True))
        require(type(marker) is dict and set(marker) == {'runId','feedExitSha256','assessmentSha256'})
        identifier(marker['runId'])
        values = []
        for name, key in [('feed-exit.json','feedExitSha256'), ('assessment.json','assessmentSha256')]:
            raw = read_private(folder, name, MAX_REQUEST)
            require(sha(marker[key]) and hashlib.sha256(raw).hexdigest() == marker[key])
            values.append(json.loads(raw))
        return marker['runId'], values[0], values[1]
    except (OSError, ValueError, UnicodeError):
        raise Refused('configuration') from None
    finally: os.close(folder)


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
                state TEXT NOT NULL CHECK(state IN ('active','finished','cancelled','uncertain')),
                supervisor TEXT, session TEXT);
                CREATE TABLE IF NOT EXISTS requests (
                request_id TEXT PRIMARY KEY, run_id TEXT NOT NULL, nonce TEXT UNIQUE NOT NULL,
                request TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('dispatching','complete','uncertain')),
                result TEXT);
                CREATE TABLE IF NOT EXISTS finish_receipts (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1),
                feed_exit TEXT NOT NULL, assessment TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS feed_binding (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1), proof TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS feed_exit_confirmation (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1), proof TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS lane_verdict (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1), proof TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS pending_binding (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1),
                proof TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS pre_drain_binding (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1),
                proof TEXT NOT NULL, sealed_at REAL NOT NULL);
                CREATE TABLE IF NOT EXISTS zero_binding (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1),
                proof TEXT NOT NULL, sealed_at REAL NOT NULL);
                CREATE TABLE IF NOT EXISTS activity_clear (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1),
                request_id TEXT NOT NULL, sealed_at REAL NOT NULL);
                CREATE TABLE IF NOT EXISTS nlb_ack (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1),
                request_id TEXT NOT NULL, sealed_at REAL NOT NULL);
                CREATE TABLE IF NOT EXISTS mutation_keys (
                run_id TEXT NOT NULL, action TEXT NOT NULL,
                PRIMARY KEY (run_id,action));
                CREATE TABLE IF NOT EXISTS prepared_marker (
                singleton INTEGER PRIMARY KEY CHECK(singleton=1),
                request_id TEXT NOT NULL, run_id TEXT NOT NULL,
                feed_session TEXT NOT NULL, sequence INTEGER NOT NULL,
                receipt_identity TEXT NOT NULL, sealed_at REAL NOT NULL);''')
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
    def _transaction(self, before_commit=None):
        with self._connection() as db:
            db.execute('BEGIN IMMEDIATE')
            try:
                yield db
                if before_commit is not None: before_commit()
                db.execute('COMMIT')
            except BaseException:
                if db.in_transaction:
                    db.execute('ROLLBACK')
                raise

    def create_run(self, run_id: str, mode: str, target: dict, hashes: dict) -> None:
        _release_admission(mode)
        identifier(run_id)
        require(mode in ('no-scale', 'disabled-rehearsal'))
        target_valid(target)
        require(type(hashes) is dict and 'bundleSha256' in hashes and len(hashes) <= 64)
        require(all(token(k) and sha(v) for k, v in hashes.items()))
        with self._transaction() as db:
            require(db.execute('SELECT 1 FROM run').fetchone() is None)
            db.execute('INSERT INTO run VALUES (1,?,?,?,?,?,?,?)',
                       (run_id, mode, encode(target).decode(), encode(hashes).decode(), 'active', None, None))

    def admit(self, request: dict, receipt=None) -> None:
        validate_request(request)
        published = read_request(self.root, request['requestId'])
        require(encode(request) == encode(published))
        with self._transaction() as db:
            run = db.execute('SELECT * FROM run').fetchone()
            require(run is not None and run['state'] == 'active')
            _release_admission(run['mode'], request['action'])
            require(request['runId'] == run['run_id'])
            require(request['target'] == json.loads(run['target']), 'target')
            require(request['bundleSha256'] == json.loads(run['hashes'])['bundleSha256'], 'source')
            if run['mode'] == 'no-scale':
                require(request['action'] in NO_SCALE_ACTIONS, 'configuration')
            if request['action'] == 'scaleout-two-to-three':
                require(run['mode'] == 'disabled-rehearsal' and
                        request['target']['scalingGroupId'] == 'asg-t4necb4dfmu6o3z2rn0g')
            require(request['deadlineMonotonic'] > monotonic(), 'timeout')
            require(run['supervisor'] in (None, request['supervisorId']))
            require(run['session'] in (None, request['feedSession']))
            require(db.execute("SELECT 1 FROM requests WHERE state='dispatching'").fetchone() is None)
            if request['action']=='lane-verdict':
                require(run['mode']=='no-scale','configuration')
                prior=self._collector_exit_in_tx(db)
                require(prior['runId']==request['runId'] and prior['feedSession']==request['feedSession'] and
                        prior['sequence']<request['minimumSequence'],'incomplete')
                require(db.execute("SELECT 1 FROM requests WHERE json_extract(request,'$.action')='lane-verdict'").fetchone() is None,'incomplete')
            if request['action'] == 'pending-proof':
                require(run['mode'] == 'disabled-rehearsal')
                require(db.execute('SELECT 1 FROM pending_binding').fetchone() is None, 'incomplete')
                require(db.execute("SELECT 1 FROM requests WHERE json_extract(request,'$.action')='pending-proof'").fetchone() is None,
                        'incomplete')
                prior = None
                for row in db.execute("SELECT request,result FROM requests WHERE run_id=? AND state='complete' ORDER BY rowid DESC",
                                      (request['runId'],)):
                    candidate = json.loads(row['request'])
                    if candidate['action'] == 'pending-status':
                        prior = (candidate,json.loads(row['result']))
                        break
                require(prior is not None, 'incomplete')
                marker, outcome = prior
                require(marker['feedSession'] == request['feedSession'] and
                        marker['minimumSequence'] < request['minimumSequence'] and
                        outcome == {'state':'complete','category':'ok','payload':{'status':'PREPARED'}},
                        'incomplete')
                gate = db.execute('SELECT * FROM prepared_marker').fetchone()
                require(gate is not None and gate['request_id'] == marker['requestId'] and
                        gate['run_id'] == request['runId'] and
                        gate['feed_session'] == request['feedSession'] and
                        gate['sequence'] == marker['minimumSequence'] and
                        gate['receipt_identity'] == marker['receiptIdentity'], 'incomplete')
                require(type(receipt) is dict and
                        receipt.get('feedSession') == request['feedSession'] and
                        type(receipt.get('sequence')) is int and
                        receipt['sequence'] == request['minimumSequence'] and
                        type(receipt.get('observedAt')) in (int,float) and
                        math.isfinite(receipt['observedAt']) and
                        receipt['observedAt'] > gate['sealed_at'], 'receipt')
            if request['action'] in ('pre-drain-proof','deregister-new-node',
                                     'zero-proof','scaling-activity-check','scalein-three-to-two'):
                require(run['mode'] == 'disabled-rehearsal' and receipt is not None and
                        receipt.get('feedSession') == request['feedSession'] and
                        receipt.get('sequence') == request['minimumSequence'] and
                        receipt.get('desired') == 3 and
                        type(receipt.get('observedAt')) in (int,float) and
                        0 <= time.time()-receipt['observedAt'] < 3.5, 'receipt')
                pending=db.execute('SELECT proof FROM pending_binding').fetchone()
                require(pending is not None, 'incomplete')
                proof=json.loads(pending['proof'])
                require(proof['runId'] == request['runId'] and
                        proof['feedSession'] == request['feedSession'] and
                        proof['originalInstanceIds'] == request['target']['originalInstanceIds'], 'target')
                if request['action'] == 'pre-drain-proof':
                    require(db.execute('SELECT 1 FROM pre_drain_binding').fetchone() is None and
                            db.execute("SELECT 1 FROM requests WHERE json_extract(request,'$.action')='pre-drain-proof'").fetchone() is None,
                            'incomplete')
                elif request['action'] == 'deregister-new-node':
                    pre=db.execute('SELECT * FROM pre_drain_binding').fetchone()
                    require(pre is not None and 0 <= time.time()-pre['sealed_at'] < 30, 'incomplete')
                    summary=json.loads(pre['proof'])
                    require(summary['newId'] == proof['newId'] and
                            summary['feedSession'] == request['feedSession'] and
                            summary['sequence'] <= receipt['sequence'] and
                            0 <= time.time()-summary['observedAt'] < 30 and
                            summary['pendingBindingSha256'] == hashlib.sha256(encode(proof)).hexdigest(), 'target')
                    require(sha(receipt.get('rosterSha256')) and
                            receipt['rosterSha256'] == roster_sha(expected_roster(
                                request['target']['originalInstanceIds'],proof['newId'],registered=True)), 'receipt')
                    require(db.execute('SELECT 1 FROM mutation_keys WHERE run_id=? AND action=?',
                            (request['runId'],request['action'])).fetchone() is None,'incomplete')
                else:
                    pre=db.execute('SELECT * FROM pre_drain_binding').fetchone()
                    require(pre is not None, 'incomplete')
                    summary=json.loads(pre['proof'])
                    require(summary['newId'] == proof['newId'] and
                            summary['feedSession'] == request['feedSession'] and
                            summary['pendingBindingSha256'] == hashlib.sha256(encode(proof)).hexdigest(),
                            'target')
                    require(receipt['rosterSha256'] == roster_sha(expected_roster(
                        request['target']['originalInstanceIds'],proof['newId'],registered=False)),
                        'receipt')
                    if request['action'] == 'zero-proof':
                        ack=db.execute('SELECT request_id,sealed_at FROM nlb_ack').fetchone()
                        require(ack is not None and receipt['observedAt'] > ack['sealed_at'],
                                'incomplete')
                        sent=db.execute('SELECT request,result FROM requests WHERE request_id=?',
                                        (ack['request_id'],)).fetchone()
                        require(sent is not None and json.loads(sent['result'])['state'] == 'complete' and
                                json.loads(sent['request'])['action'] == 'deregister-new-node' and
                                json.loads(sent['request'])['runId'] == request['runId'] and
                                json.loads(sent['request'])['feedSession'] == request['feedSession'] and
                                json.loads(sent['request'])['minimumSequence'] < receipt['sequence'],
                                'incomplete')
                        require(db.execute('SELECT 1 FROM zero_binding').fetchone() is None and
                            db.execute("SELECT 1 FROM requests WHERE json_extract(request,'$.action')='zero-proof'").fetchone() is None and
                            receipt['sequence'] > summary['sequence'],'incomplete')
                    else:
                        zero=db.execute('SELECT * FROM zero_binding').fetchone()
                        require(zero is not None and 0 <= time.time()-zero['sealed_at'] < 30,
                                'incomplete')
                        zero_proof=json.loads(zero['proof'])
                        require(zero_proof['newId'] == proof['newId'] and
                            zero_proof['feedSession'] == request['feedSession'] and
                            zero_proof['preDrainBindingSha256'] == hashlib.sha256(
                                encode(summary)).hexdigest() and
                            receipt['sequence'] > zero_proof['sequence'] and
                            receipt['observedAt'] > zero['sealed_at'],'receipt')
                        activity=db.execute('SELECT * FROM activity_clear').fetchone()
                        if request['action'] == 'scaling-activity-check':
                            require(activity is None and db.execute("SELECT 1 FROM requests WHERE json_extract(request,'$.action')='scaling-activity-check'").fetchone() is None,
                                    'incomplete')
                        else:
                            require(activity is not None and
                                0 <= time.time()-activity['sealed_at'] < 3.5,'incomplete')
                            checked=db.execute('SELECT request,result FROM requests WHERE request_id=?',
                                               (activity['request_id'],)).fetchone()
                            require(checked is not None and json.loads(checked['result']) ==
                                {'state':'complete','category':'ok','payload':
                                 {'status':'CLEAR','requestId':activity['request_id']}},'incomplete')
                            check_request=json.loads(checked['request'])
                            require(check_request['runId'] == request['runId'] and
                                    check_request['feedSession'] == request['feedSession'] and
                                    check_request['minimumSequence'] == request['minimumSequence'] and
                                    check_request['receiptIdentity'] == request['receiptIdentity'],
                                    'receipt')
                            require(db.execute('SELECT 1 FROM mutation_keys WHERE run_id=? AND action=?',
                                    (request['runId'],request['action'])).fetchone() is None,'incomplete')
            require(db.execute('SELECT 1 FROM requests WHERE request_id=? OR nonce=?',
                               (request['requestId'],request['nonce'])).fetchone() is None)
            db.execute('UPDATE run SET supervisor=?, session=?', (request['supervisorId'],request['feedSession']))
            db.execute('INSERT INTO requests VALUES (?,?,?,?,?,NULL)',
                       (request['requestId'], request['runId'], request['nonce'], encode(request).decode(), 'dispatching'))
            if request['action'] in ('deregister-new-node','scalein-three-to-two'):
                db.execute('INSERT INTO mutation_keys VALUES (?,?)',(request['runId'],request['action']))

    def seal(self, request_id: str, result: dict, pending_source_sha=None,
             pre_drain_source_sha=None, zero_source_sha=None,
             lane_verdict_source_sha=None, lane_verdict_result=None) -> None:
        identifier(request_id)
        validate_result(result)
        with self._transaction() as db:
            row = db.execute('SELECT state,request FROM requests WHERE request_id=?', (request_id,)).fetchone()
            require(row is not None and row['state'] == 'dispatching')
            run = db.execute('SELECT state FROM run').fetchone()
            require(result['state'] != 'complete' or run['state'] == 'active')
            request = json.loads(row['request'])
            require(request['action']=='lane-verdict' or
                    lane_verdict_source_sha is None and lane_verdict_result is None,'source')
            if result['state']=='complete' and request['action']=='lane-verdict':
                require(pending_source_sha is None and pre_drain_source_sha is None and zero_source_sha is None,'source')
                payload=result['payload']
                require(type(payload) is dict and set(payload)=={'status','category','verdictSha256','clientIdentitySha256','workerCredentialCleanupSha256'} and
                        payload['status'] in ('PASS','FAIL','INCONCLUSIVE') and payload['category']=='lane-timing' and
                        payload==lane_verdict_result,'source')
                current=db.execute('SELECT * FROM run').fetchone()
                require(current['mode']=='no-scale' and sha(lane_verdict_source_sha) and
                        json.loads(current['hashes']).get('laneTimingAssessor')==lane_verdict_source_sha,'source')
                prior=self._collector_exit_in_tx(db)
                require(prior['runId']==request['runId'] and prior['feedSession']==request['feedSession'] and
                        prior['sequence']<request['minimumSequence'],'incomplete')
                proof=dict(payload=payload,requestId=request_id,runId=request['runId'],feedSession=request['feedSession'],
                    sequence=request['minimumSequence'],sourceSha256=lane_verdict_source_sha,collectorExit=prior)
                require(db.execute('SELECT 1 FROM lane_verdict').fetchone() is None,'incomplete')
                db.execute('INSERT INTO lane_verdict VALUES (1,?)',(encode(proof).decode(),))
            elif result['state']=='complete' and request['action']=='collector-status':
                require(pending_source_sha is None and pre_drain_source_sha is None and
                        zero_source_sha is None,'source')
                payload=result['payload'];status=payload.get('status')
                require(status in ('ALIVE','EXITED') and set(payload)==
                        ({'status','identity'} if status=='ALIVE' else {'status','identity','exit'}),'source')
                alive=False
                for previous in db.execute("SELECT request,result FROM requests WHERE state='complete' ORDER BY rowid"):
                    req=json.loads(previous['request']);out=json.loads(previous['result'])['payload']
                    if req['action']=='collector-status':
                        require(out['identity']==payload['identity'] and req['feedSession']==request['feedSession'],'source')
                        if out['status']=='ALIVE':alive=True
                        if out['status']=='EXITED':require(payload==out,'source')
                if status=='EXITED':require(alive,'incomplete')
            elif result['state'] == 'complete' and request['action'] == 'pending-proof':
                self._bind_pending_in_tx(db, request, result['payload'], pending_source_sha)
            elif result['state'] == 'complete' and request['action'] == 'pre-drain-proof':
                require(pending_source_sha is None and zero_source_sha is None, 'source')
                self._bind_pre_drain_in_tx(db,request,result['payload'],pre_drain_source_sha)
            elif result['state'] == 'complete' and request['action'] == 'zero-proof':
                require(pending_source_sha is None and pre_drain_source_sha is None, 'source')
                self._bind_zero_in_tx(db,request,result['payload'],zero_source_sha)
            elif result['state'] == 'complete' and request['action'] == 'scaling-activity-check':
                require(result['payload'] == {'status':'CLEAR','requestId':request_id} and
                        pending_source_sha is None and pre_drain_source_sha is None and
                        zero_source_sha is None,'source')
                require(db.execute('SELECT 1 FROM activity_clear').fetchone() is None,'incomplete')
                db.execute('INSERT INTO activity_clear VALUES (1,?,?)',(request_id,time.time()))
            elif result['state'] == 'complete' and request['action'] == 'deregister-new-node':
                require(result['payload'].keys() == {'requestId','jobId'} and
                        all(token(value) for value in result['payload'].values()) and
                        pending_source_sha is None and pre_drain_source_sha is None and
                        zero_source_sha is None,'source')
                require(db.execute('SELECT 1 FROM nlb_ack').fetchone() is None,'incomplete')
                db.execute('INSERT INTO nlb_ack VALUES (1,?,?)',(request_id,time.time()))
            else:
                require(pending_source_sha is None and pre_drain_source_sha is None and
                        zero_source_sha is None, 'source')
            db.execute('UPDATE requests SET state=?, result=? WHERE request_id=?',
                       (result['state'], encode(result).decode(), request_id))
            if result['state'] == 'uncertain':
                db.execute("UPDATE run SET state='uncertain' WHERE state='active'")

    def _collector_exit_in_tx(self,db):
        for row in db.execute("SELECT request,result FROM requests WHERE state='complete' ORDER BY rowid DESC"):
            req=json.loads(row['request']);out=json.loads(row['result'])['payload']
            if req['action']=='collector-status' and out.get('status')=='EXITED':
                return dict(runId=req['runId'],feedSession=req['feedSession'],sequence=req['minimumSequence'],
                            requestId=req['requestId'],payload=out)
        raise Refused('incomplete')

    def collector_exit(self):
        with self._connection() as db:return self._collector_exit_in_tx(db)

    def lane_verdict(self):
        with self._connection() as db:
            row=db.execute('SELECT proof FROM lane_verdict').fetchone()
            require(row is not None,'incomplete')
            return json.loads(row['proof'])

    def record_prepared(self, request_id: str) -> dict:
        """Commit a second marker strictly after the PREPARED result commit."""
        identifier(request_id)
        with self._transaction() as db:
            row = db.execute('SELECT * FROM requests WHERE request_id=?', (request_id,)).fetchone()
            run = db.execute('SELECT * FROM run').fetchone()
            require(row is not None and run is not None and run['state'] == 'active' and
                    row['state'] == 'complete' and row['run_id'] == run['run_id'], 'incomplete')
            request = json.loads(row['request']); result = json.loads(row['result'])
            require(request['action'] == 'pending-status' and
                    result == {'state':'complete','category':'ok','payload':{'status':'PREPARED'}} and
                    run['session'] == request['feedSession'], 'incomplete')
            require(db.execute('SELECT 1 FROM prepared_marker').fetchone() is None, 'incomplete')
            sealed_at = time.time()
            db.execute('INSERT INTO prepared_marker VALUES (1,?,?,?,?,?,?)',
                       (request_id, run['run_id'], request['feedSession'],
                        request['minimumSequence'], request['receiptIdentity'], sealed_at))
        return dict(requestId=request_id,feedSession=request['feedSession'],
                    sequence=request['minimumSequence'],sealedAt=sealed_at)

    def cancel(self, category: str) -> None:
        require(category in CATEGORIES)
        with self._transaction() as db:
            require(db.execute('SELECT 1 FROM run').fetchone() is not None)
            db.execute("UPDATE run SET state=? WHERE state='active'",
                       ('cancelled' if category == 'cancelled' else 'uncertain',))

    def _bind_pending_in_tx(self, db, request: dict, proof: dict, source_sha: str) -> None:
        """Bind one output from the exact reviewed Pending proof worker.

        Only the supervisor calls this after its fixed Workbench command has
        completed. Remote source equality is a separate staging review gate.
        """
        require(type(proof) is dict and set(proof) == {'runId','feedSession','newId',
            'originalInstanceIds','sequence','observedAt','resourceVersion',
            'clientRosterSha256','evidenceSha256','sourceSha256'}, 'source')
        identifier(proof['runId']); identifier(proof['feedSession'])
        require(token(proof['newId']) and type(proof['sequence']) is int and
                proof['sequence'] > 0 and sha(proof['clientRosterSha256']) and
                sha(proof['evidenceSha256']) and token(proof['resourceVersion']) and
                sha(source_sha) and proof['sourceSha256'] == source_sha, 'source')
        require(type(proof['observedAt']) in (int,float) and
                math.isfinite(proof['observedAt']) and
                0 <= time.time()-proof['observedAt'] < 30, 'source')
        run = db.execute('SELECT * FROM run').fetchone()
        require(run is not None and run['state'] == 'active' and
                run['mode'] == 'disabled-rehearsal', 'incomplete')
        require(json.loads(run['hashes']).get('pendingProofWorker') == source_sha, 'source')
        originals = json.loads(run['target'])['originalInstanceIds']
        require(proof['runId'] == run['run_id'] == request['runId'] and
                proof['feedSession'] == request['feedSession'] and
                proof['originalInstanceIds'] == originals and
                proof['newId'] not in originals and
                0 < proof['sequence'] <= request['minimumSequence'] and
                run['session'] in (None,proof['feedSession']), 'target')
        require(db.execute('SELECT 1 FROM pending_binding').fetchone() is None, 'incomplete')
        db.execute('INSERT INTO pending_binding VALUES (1,?)', (encode(proof).decode(),))
        db.execute('UPDATE run SET session=?', (proof['feedSession'],))

    def pending_binding(self) -> dict:
        with self._connection() as db:
            row = db.execute('SELECT proof FROM pending_binding').fetchone()
            require(row is not None, 'incomplete')
            return json.loads(row['proof'])

    def _bind_pre_drain_in_tx(self,db,request,proof,source_sha):
        require(type(proof) is dict and set(proof) == {'runId','feedSession','newId',
            'originalInstanceIds','pendingBindingSha256','sequence','observedAt',
            'resourceVersion','clientRosterSha256','evidenceSha256','sourceSha256'}, 'source')
        require(sha(source_sha) and proof['sourceSha256'] == source_sha and
                sha(proof['pendingBindingSha256']) and sha(proof['clientRosterSha256']) and
                sha(proof['evidenceSha256']) and token(proof['resourceVersion']) and
                type(proof['sequence']) is int and 0 < proof['sequence'] <= request['minimumSequence'] and
                type(proof['observedAt']) in (int,float) and
                0 <= time.time()-proof['observedAt'] < 30,'source')
        run=db.execute('SELECT * FROM run').fetchone()
        pending=db.execute('SELECT proof FROM pending_binding').fetchone()
        require(run is not None and pending is not None and
                json.loads(run['hashes']).get('preDrainProofWorker') == source_sha,'source')
        bound=json.loads(pending['proof'])
        require(proof['runId'] == run['run_id'] == request['runId'] and
                proof['feedSession'] == request['feedSession'] == bound['feedSession'] and
                proof['newId'] == bound['newId'] and
                proof['originalInstanceIds'] == bound['originalInstanceIds'] and
                proof['clientRosterSha256'] == bound['clientRosterSha256'] and
                proof['pendingBindingSha256'] == hashlib.sha256(encode(bound)).hexdigest() and
                proof['sequence'] > bound['sequence'],'target')
        require(db.execute('SELECT 1 FROM pre_drain_binding').fetchone() is None,'incomplete')
        db.execute('INSERT INTO pre_drain_binding VALUES (1,?,?)',
                   (encode(proof).decode(),time.time()))

    def pre_drain_binding(self):
        with self._connection() as db:
            row=db.execute('SELECT proof,sealed_at FROM pre_drain_binding').fetchone()
            require(row is not None,'incomplete')
            return dict(json.loads(row['proof']),sealedAt=row['sealed_at'])

    def _bind_zero_in_tx(self,db,request,proof,source_sha):
        require(type(proof) is dict and set(proof) == {'runId','feedSession','newId',
            'originalInstanceIds','preDrainBindingSha256','sequence','observedAt',
            'clientRosterSha256','evidenceSha256','sourceSha256'},'source')
        require(sha(source_sha) and proof['sourceSha256'] == source_sha and
                all(sha(proof[key]) for key in ('preDrainBindingSha256',
                    'clientRosterSha256','evidenceSha256')) and
                type(proof['sequence']) is int and 0 < proof['sequence'] <= request['minimumSequence'] and
                type(proof['observedAt']) in (int,float) and
                0 <= time.time()-proof['observedAt'] < 30,'source')
        run=db.execute('SELECT * FROM run').fetchone()
        pre=db.execute('SELECT proof FROM pre_drain_binding').fetchone()
        require(run is not None and pre is not None and run['mode'] == 'disabled-rehearsal' and
                json.loads(run['hashes']).get('zeroProofWorker') == source_sha,'source')
        bound=json.loads(pre['proof'])
        require(proof['runId'] == run['run_id'] == request['runId'] and
                proof['feedSession'] == request['feedSession'] == bound['feedSession'] and
                proof['newId'] == bound['newId'] and
                proof['originalInstanceIds'] == bound['originalInstanceIds'] and
                proof['clientRosterSha256'] == bound['clientRosterSha256'] and
                proof['preDrainBindingSha256'] == hashlib.sha256(encode(bound)).hexdigest() and
                proof['sequence'] > bound['sequence'],'target')
        require(db.execute('SELECT 1 FROM zero_binding').fetchone() is None,'incomplete')
        db.execute('INSERT INTO zero_binding VALUES (1,?,?)',
                   (encode(proof).decode(),time.time()))

    def zero_binding(self):
        with self._connection() as db:
            row=db.execute('SELECT proof,sealed_at FROM zero_binding').fetchone()
            require(row is not None,'incomplete')
            return dict(json.loads(row['proof']),sealedAt=row['sealed_at'])

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


    def run_snapshot(self):
        with self._transaction() as db:
            run = db.execute('SELECT * FROM run').fetchone()
            require(run is not None)
            finish = db.execute('SELECT * FROM finish_receipts').fetchone()
            return dict(runId=run['run_id'], runState=run['state'], mode=run['mode'],
                        target=json.loads(run['target']), hashes=json.loads(run['hashes']),
                        assessment=json.loads(finish['assessment']) if finish else None,
                        feedExit=json.loads(finish['feed_exit']) if finish else None)

    def bind_feed(self, binding):
        require(type(binding) is dict and set(binding)=={'runId','feedId','pid','bindingSha256','argvSha256'},'child')
        identifier(binding['runId']);identifier(binding['feedId'])
        require(type(binding['pid']) is int and binding['pid']>0 and
                sha(binding['bindingSha256']) and sha(binding['argvSha256']),'child')
        with self._transaction() as db:
            run=db.execute('SELECT * FROM run').fetchone()
            require(run is not None and run['mode']=='no-scale' and run['state']=='active' and
                    run['run_id']==binding['runId'],'child')
            require(db.execute('SELECT 1 FROM feed_binding').fetchone() is None,'child')
            db.execute('INSERT INTO feed_binding VALUES (1,?)',(encode(binding).decode(),))

    def confirm_feed_exit(self, confirmation):
        require(type(confirmation) is dict and set(confirmation)=={'runId','feedId','pid','exitCode'},'child')
        require(type(confirmation['pid']) is int and type(confirmation['exitCode']) is int,'child')
        with self._transaction() as db:
            row=db.execute('SELECT proof FROM feed_binding').fetchone()
            require(row is not None,'child');binding=json.loads(row['proof'])
            require(all(confirmation[k]==binding[k] for k in ('runId','feedId','pid')),'child')
            require(db.execute('SELECT 1 FROM feed_exit_confirmation').fetchone() is None,'child')
            db.execute('INSERT INTO feed_exit_confirmation VALUES (1,?)',(encode(confirmation).decode(),))

    def _verify_feed_closure(self, db, record, feed_exit):
        bound=db.execute('SELECT proof FROM feed_binding').fetchone()
        exited=db.execute('SELECT proof FROM feed_exit_confirmation').fetchone()
        require(bound is not None and exited is not None,'child')
        bound=json.loads(bound['proof']);exited=json.loads(exited['proof'])
        require(bound['runId']==record['runId'] and bound['bindingSha256']==record['bindingSha256'],'source')
        require(all(bound[k]==exited[k] for k in ('runId','feedId','pid')) and
                type(feed_exit['pid']) is int and type(exited['exitCode']) is int and
                bound['pid']==feed_exit['pid'] and exited['exitCode']==feed_exit['exitCode'],'child')

    def verify_feed_closure(self, record, feed_exit):
        with self._connection() as db:self._verify_feed_closure(db,record,feed_exit)

    def finish(self, before_commit=None):
        run_id, feed_exit, assessment = read_finish(self.root)
        with self._transaction(before_commit=before_commit) as db:
            run = db.execute('SELECT * FROM run').fetchone()
            require(run is not None and run['state'] == 'active' and run['run_id'] == run_id)
            validate_finish_receipts(feed_exit, assessment, run['mode'])
            require(run['session'] in (None, feed_exit['feedSession']))
            if run['mode']=='no-scale':
                record,raw=private_json(self.root/NO_SCALE_PROOF)
                self._verify_feed_closure(db,record,feed_exit)
                worker_row=db.execute('SELECT proof FROM lane_verdict').fetchone()
                worker=json.loads(worker_row['proof']) if worker_row else None
                bound=dict(runId=run['run_id'],target=json.loads(run['target']),hashes=json.loads(run['hashes']))
                derived=no_scale_assessment(record,bound,worker,feed_exit,now=time.time())
                require(raw==encode(record) and encode(assessment)==encode(derived),'source')

            rows = db.execute('SELECT * FROM requests').fetchall()
            require(all(row['state'] == 'complete' for row in rows), 'incomplete')
            require(all(json.loads(row['request'])['minimumSequence'] <= feed_exit['sequence'] for row in rows))
            # Also reject staged/unadmitted requests, including incomplete publications.
            folder = self.root/'requests'
            if folder.exists():
                private(folder, True)
                expected = {row['request_id'] + suffix for row in rows
                            for suffix in ('.request.json','.ready')}
                require({entry.name for entry in folder.iterdir()} == expected, 'incomplete')
                for row in rows:
                    require(read_request(self.root, row['request_id']) == json.loads(row['request']))
            db.execute('INSERT INTO finish_receipts VALUES (1,?,?)',
                       (encode(feed_exit).decode(), encode(assessment).decode()))
            db.execute("UPDATE run SET state='finished', session=?", (feed_exit['feedSession'],))
        return self.run_snapshot()
