#!/usr/bin/env python3
"""One-use detached PgCat baseline, pre-scale and Pending worker."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
import uuid


SOURCES = {
    'rehearsalWorker': 'pgcat_rehearsal_worker.py',
    'rehearsalContract': 'pgcat_rehearsal_contract.py',
    'pendingProofWorker': 'pgcat_pending_proof_worker.py',
    'captureWindow': 'capture_window.py',
    'finishClients': 'finish_clients.py',
    'workerCapture': 'worker_capture.py',
    'segmentedCapture': 'segmented_capture.py',
}
STAGES = ('baseline', 'pre-scale', 'pending-ready')
TARGET_KEYS = {'accountId', 'regionId', 'workerId', 'scalingGroupId',
               'nlbServerGroupId', 'originalInstanceIds'}


def _check(value):
    if not value:
        raise ValueError('worker')


def _unique(pairs):
    result = {}
    for key, value in pairs:
        _check(key not in result)
        result[key] = value
    return result


def _decode(raw):
    def invalid(_):
        raise ValueError('worker')
    return json.loads(raw.decode('ascii'), object_pairs_hook=_unique,
                      parse_constant=invalid)


def _directory(path):
    parts = Path(path).parts
    _check(parts and parts[0] == '/' and '..' not in parts)
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for part in parts[1:]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                            dir_fd=fd)
            os.close(fd)
            fd = child
        info = os.fstat(fd)
        _check(stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700
               and info.st_uid == os.geteuid())
        return fd
    except Exception:
        os.close(fd)
        raise


def _read_at(fd, name, limit=2048):
    _check(type(name) is str and '/' not in name and name not in ('.', '..'))
    item = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=fd)
    try:
        before = os.fstat(item)
        _check(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid()
               and stat.S_IMODE(before.st_mode) == 0o600 and before.st_nlink == 1
               and before.st_size <= limit)
        raw = os.read(item, limit + 1)
        after = os.fstat(item)
        current = os.stat(name, dir_fd=fd, follow_symlinks=False)
        def identity(info):
            return (info.st_dev, info.st_ino, info.st_size,
                    info.st_mtime_ns, info.st_ctime_ns)
        _check(len(raw) == before.st_size and len(raw) <= limit and
               identity(before) == identity(after) == identity(current))
        return raw
    finally:
        os.close(item)


def _write_at(fd, name, value):
    raw = (json.dumps(value, sort_keys=True, separators=(',', ':'),
                      allow_nan=False) + '\n').encode('ascii')
    item = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                   0o600, dir_fd=fd)
    try:
        os.fchmod(item, 0o600)
        while raw:
            raw = raw[os.write(item, raw):]
        os.fsync(item)
    finally:
        os.close(item)
    os.fsync(fd)


def _uuid(value):
    _check(type(value) is str and str(uuid.UUID(value)) == value)


def _target(value):
    _check(type(value) is dict and set(value) == TARGET_KEYS)
    _check(all(type(value[key]) is str and
               re.fullmatch(r'[A-Za-z0-9_-]{1,256}', value[key]) is not None
               for key in TARGET_KEYS - {'originalInstanceIds'}))
    ids = value['originalInstanceIds']
    _check(type(ids) is list and len(ids) == 2 and ids == sorted(set(ids)) and
           all(type(node) is str and
               re.fullmatch(r'[A-Za-z0-9_-]{1,256}', node) is not None
               for node in ids))
    raw = json.dumps(value, sort_keys=True, separators=(',', ':'),
                     allow_nan=False).encode('ascii')
    return hashlib.sha256(raw).hexdigest()


def _paths(contract, worker_path):
    contract = Path(contract)
    worker_path = Path(worker_path)
    _check(contract.is_absolute() and contract.name == 'contract' and
           worker_path == contract.parent / 'run' / 'pgcat_rehearsal_worker.py')
    run = contract.parent / 'run'
    for directory in (run, contract):
        fd = _directory(directory)
        os.close(fd)
    return run


def _verify_sources(run, expected):
    _check(type(expected) is dict and set(expected) == set(SOURCES))
    fd = _directory(run)
    try:
        for key, name in SOURCES.items():
            digest = expected[key]
            _check(type(digest) is str and re.fullmatch(r'[0-9a-f]{64}', digest) is not None)
            raw = _read_at(fd, name, 256 * 1024)
            _check(hashlib.sha256(raw).hexdigest() == digest)
    finally:
        os.close(fd)


def _jobs(run):
    fd = _directory(run)
    try:
        try:
            os.mkdir('phase-jobs', 0o700, dir_fd=fd)
            os.fsync(fd)
        except FileExistsError:
            pass
    finally:
        os.close(fd)
    return _directory(run / 'phase-jobs')


def _intent(fd, stage, run_id):
    value = _decode(_read_at(fd, stage + '.intent.json'))
    _check(type(value) is dict and set(value) ==
           {'version', 'runId', 'stage', 'nonce', 'startedAt', 'targetSha256'} and
           type(value['version']) is int and value['version'] == 1 and
           value['runId'] == run_id and value['stage'] == stage and
           type(value['startedAt']) is int)
    _uuid(value['nonce'])
    _check(type(value['targetSha256']) is str and
           re.fullmatch(r'[0-9a-f]{64}', value['targetSha256']) is not None)
    return value


def _sealed_stage(contract, stage):
    fd = _directory(contract)
    try:
        raw = _read_at(fd, stage + '.json', 65536)
        seal = _decode(_read_at(fd, stage + '.json.sha256', 256))
        _check(seal == {'sha256': hashlib.sha256(raw).hexdigest()})
        value = _decode(raw)
        _check(type(value) is dict)
        return value
    finally:
        os.close(fd)


def start(contract, run_id, stage, kubeconfig, expected, target, *, worker_path=None,
          spawn=None):
    """Publish one intent before detached spawn; an unknown spawn never retries."""
    try:
        _check(stage in STAGES)
        _uuid(run_id)
        target_sha = _target(target)
        worker_path = Path(__file__).resolve() if worker_path is None else Path(worker_path)
        run = _paths(contract, worker_path)
        _verify_sources(run, expected)
        kubeconfig = Path(kubeconfig)
        _check(kubeconfig.is_absolute() and '..' not in kubeconfig.parts)
        parent = _directory(kubeconfig.parent)
        try:
            _read_at(parent, kubeconfig.name, 65536)
        finally:
            os.close(parent)
        jobs = _jobs(run)
        try:
            _check(not any(name.startswith(stage + '.') for name in os.listdir(jobs)))
            nonce = str(uuid.uuid4())
            _write_at(jobs, stage + '.intent.json', {'version': 1, 'runId': run_id,
                'stage': stage, 'nonce': nonce, 'startedAt': int(time.time()),
                'targetSha256': target_sha})
        finally:
            os.close(jobs)
        launcher = subprocess.Popen if spawn is None else spawn
        env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
               'HOME': '/root', 'LANG': 'C.UTF-8'}
        arguments = [sys.executable, str(worker_path), '_run', stage, str(contract),
                     run_id, str(kubeconfig), json.dumps(expected, sort_keys=True,
                         separators=(',', ':')),
                     json.dumps(target, sort_keys=True, separators=(',', ':')), nonce]
        try:
            launcher(arguments, cwd=str(run), stdin=subprocess.DEVNULL,
                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                     env=env, start_new_session=True, close_fds=True)
        except BaseException:
            raise ValueError('worker') from None
        return 'STARTED'
    except (OSError, ValueError, TypeError, KeyError, IndexError,
            OverflowError, UnicodeError, AttributeError):
        raise ValueError('worker') from None


def run_child(contract, run_id, stage, kubeconfig, expected, target, nonce,
              *, worker_path=None):
    result = {'version': 1, 'runId': run_id, 'stage': stage,
              'nonce': nonce, 'status': 'INCONCLUSIVE'}
    jobs = None
    try:
        _check(stage in STAGES)
        _uuid(run_id)
        _uuid(nonce)
        target_sha = _target(target)
        worker_path = Path(__file__).resolve() if worker_path is None else Path(worker_path)
        run = _paths(contract, worker_path)
        _verify_sources(run, expected)
        jobs = _jobs(run)
        intent = _intent(jobs, stage, run_id)
        _check(intent['nonce'] == nonce and intent['targetSha256'] == target_sha)
        _write_at(jobs, stage + '.claim.json', {'version': 1, 'nonce': nonce})
        import pgcat_rehearsal_contract as rehearsal
        if stage == 'baseline':
            rehearsal.baseline(contract, run_id, kubeconfig, target)
        elif stage == 'pre-scale':
            rehearsal.pre_scale(contract, run_id, target)
        else:
            rehearsal.pending_ready(contract, run_id, target)
        result['status'] = 'PREPARED'
    except BaseException:
        pass
    try:
        if jobs is not None:
            _write_at(jobs, stage + '.result.json', result)
            _write_at(jobs, stage + '.exit.json',
                      {'version': 1, 'nonce': nonce,
                       'code': 0 if result['status'] == 'PREPARED' else 1})
    except BaseException:
        return 1
    finally:
        if jobs is not None:
            os.close(jobs)
    return 0 if result['status'] == 'PREPARED' else 1


def status(contract, run_id, stage, expected, target, *, now=None):
    """Read only nonce-bound completion markers; a PID is never adopted."""
    try:
        _check(stage in STAGES)
        _uuid(run_id)
        target_sha = _target(target)
        run = _paths(contract, Path(contract).parent / 'run' /
                     'pgcat_rehearsal_worker.py')
        _verify_sources(run, expected)
        jobs = _jobs(run)
        try:
            intent = _intent(jobs, stage, run_id)
            _check(intent['targetSha256'] == target_sha)
            names = set(os.listdir(jobs))
            result_name = stage + '.result.json'
            exit_name = stage + '.exit.json'
            if result_name in names or exit_name in names:
                if (result_name not in names or exit_name not in names or
                        stage + '.claim.json' not in names):
                    return 'INCONCLUSIVE'
                claim = _decode(_read_at(jobs, stage + '.claim.json'))
                result = _decode(_read_at(jobs, result_name))
                finished = _decode(_read_at(jobs, exit_name))
                if (claim != {'version': 1, 'nonce': intent['nonce']} or
                    result != {'version': 1, 'runId': run_id, 'stage': stage,
                               'nonce': intent['nonce'], 'status': 'PREPARED'} or
                    finished != {'version': 1, 'nonce': intent['nonce'], 'code': 0}):
                    return 'INCONCLUSIVE'
                _sealed_stage(contract, stage)
                return 'PREPARED'
            clock = time.time if now is None else now
            return 'RUNNING' if 0 <= clock() - intent['startedAt'] <= 40 else 'INCONCLUSIVE'
        finally:
            os.close(jobs)
    except (OSError, ValueError, TypeError, KeyError, IndexError,
            OverflowError, UnicodeError, AttributeError):
        return 'INCONCLUSIVE'


def main():
    os.umask(0o077)
    try:
        args = sys.argv[1:]
        if len(args) not in (7, 8) or args[0] not in ('start', 'status', '_run'):
            raise ValueError('worker')
        verb, stage, contract, run_id, kubeconfig, hashes, target_raw = args[:7]
        expected = _decode(hashes.encode('ascii'))
        target = _decode(target_raw.encode('ascii'))
        if verb == '_run':
            if len(args) != 8:
                raise ValueError('worker')
            return run_child(contract, run_id, stage, kubeconfig, expected, target, args[7])
        if len(args) != 7:
            raise ValueError('worker')
        state = (start(contract, run_id, stage, kubeconfig, expected, target) if verb == 'start'
                 else status(contract, run_id, stage, expected, target))
        if stage == 'pending-ready':
            print('PENDING_DETACHED:' + state)
        else:
            output = {'status': state, 'phase': stage}
            if stage == 'pre-scale' and state == 'PREPARED':
                sealed = _sealed_stage(contract, stage)
                _check(set(sealed) == {'providerRunContextSha256', 'targetSha256',
                    'firstAt', 'latestAt', 'sequence', 'feedSession',
                    'firstFrameSha256', 'latestFrameSha256'} and
                    sealed['targetSha256'] == _target(target) and
                    type(sealed['sequence']) is int and sealed['sequence'] > 0)
                _uuid(sealed['feedSession'])
                output.update(feedSession=sealed['feedSession'],
                              sequence=sealed['sequence'])
            print(json.dumps(output, sort_keys=True))
        return 0 if state in ('STARTED', 'RUNNING', 'PREPARED') else 1
    except BaseException:
        if len(sys.argv) > 2 and sys.argv[2] == 'pending-ready':
            print('PENDING_DETACHED:INCONCLUSIVE')
        else:
            print(json.dumps({'status': 'INCONCLUSIVE', 'phase':
                              sys.argv[2] if len(sys.argv) > 2 and
                              sys.argv[2] in STAGES else 'unknown'}, sort_keys=True))
        return 1


if __name__ == '__main__':
    sys.exit(main())
