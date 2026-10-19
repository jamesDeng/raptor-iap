"""One-use worker-local controls for the bounded PgCat live test."""
import hashlib
import json
import os
import stat
import subprocess
import time
import argparse
import fcntl
import uuid
from pathlib import Path

SCOPE = {
    'cluster': 'c92787e953503492ea141a744c81498f1',
    'group': 'asg-t4necb4dfmu6o3z2rn0g',
    'proxy': 'DP00001',
    'region': 'ap-southeast-1',
}


def private_directory(path):
    if path.is_symlink() or not path.is_dir() or stat.S_IMODE(path.stat().st_mode) != 0o700:
        raise ValueError('private worker directory required')


def unique_pairs(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError('duplicate trigger key')
        value[key] = item
    return value


def write_receipt(path, value):
    fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, allow_nan=False, separators=(',', ':'))
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(str(path.parent), os.O_RDONLY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def write_heartbeat(run_dir, run_id, action):
    directory = run_dir / 'control'
    directory.mkdir(mode=0o700, exist_ok=True)
    private_directory(directory)
    destination = directory / (action + '.heartbeat.json')
    temporary = directory / ('.' + action + '.heartbeat.' + uuid.uuid4().hex)
    try:
        write_receipt(temporary, {'runId': run_id, 'action': action, 'observedAt': time.time()})
        os.replace(str(temporary), str(destination))
        fd = os.open(str(directory), os.O_RDONLY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    finally:
        if temporary.exists():
            temporary.unlink()


def command(argv, timeout, env):
    result = subprocess.run(argv, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                            timeout=timeout, env=env, check=True)
    if len(result.stdout) > 8192:
        raise ValueError('control response too large')
    return result.stdout


def checked_json(raw):
    if len(raw) > 8192:
        raise ValueError('control response too large')
    value = json.loads(raw.decode('utf-8'), object_pairs_hook=unique_pairs)
    if not isinstance(value, dict):
        raise ValueError('control response must be an object')
    return value


class WorkerControl:
    def __init__(self, run_dir, run_id, action, expected_digest, kubeconfig=None, run_command=command):
        self.run_dir = Path(run_dir)
        self.run_id = run_id
        self.action = action
        self.expected_digest = expected_digest
        self.kubeconfig = Path(kubeconfig) if kubeconfig else None
        self.run_command = run_command

    def _environment(self):
        path = self.kubeconfig
        if path is None or path.is_symlink() or not path.is_file() or stat.S_IMODE(path.stat().st_mode) != 0o600:
            raise ValueError('private worker credential required')
        env = os.environ.copy()
        env['KUBECONFIG'] = str(path)
        return env

    def _gate(self):
        gate = str(self.run_dir / 'gateway-live-gate')
        want = self.action == 'enable'
        expected = 'false' if want else 'true'
        target = 'true' if want else 'false'
        stage = 'set'
        try:
            env = self._environment()
            set_result = checked_json(self.run_command(
                [gate, '--namespace', 'raptor-system', '--secret', 'gateway-live-config',
                 '--expected', expected, '--set', target], 90, env))
            rv = set_result.get('resourceVersion')
            changed = set_result.get('changedAfter')
            if (not isinstance(rv, str) or not rv.isdigit() or not isinstance(changed, str)
                    or not changed.endswith('Z') or set_result.get('replacementEnabled') is not want):
                raise ValueError('invalid guarded set receipt')
            write_receipt(self.run_dir / 'control' / (self.action + '.set.json'),
                          {'resourceVersion': rv, 'changedAfter': changed, 'replacementEnabled': want})
            stage = 'rollout'
            common = ['--expected', target, '--resource-version', rv, '--changed-after', changed]
            rollout = self.run_command([gate, 'rollout'] + common, 120, env)
            if rollout.strip() != b'rollout=requested':
                raise ValueError('rollout request uncertain')
            self.run_command(['kubectl', '-n', 'raptor-system', 'rollout', 'status',
                              'deployment/agent-gateway', '--timeout=120s'], 135, env)
            stage = 'verify'
            verification = checked_json(self.run_command([gate, 'verify'] + common, 90, env))
            generation = verification.get('generation')
            pod_uid = verification.get('podUID')
            if (verification.get('replacementEnabled') is not want or verification.get('ready') is not True
                    or not isinstance(generation, int) or generation <= 0
                    or not isinstance(pod_uid, str) or not pod_uid):
                raise ValueError('guard verification failed')
            return {'action': self.action, 'status': 'VERIFIED', 'resourceVersion': rv,
                    'podUID': pod_uid, 'generation': generation, 'replacementEnabled': want}
        except Exception:
            return {'action': self.action, 'status': 'INCONCLUSIVE', 'stage': stage}

    def _finish(self):
        try:
            env = self._environment()
            result = checked_json(self.run_command(
                ['python3', str(self.run_dir / 'finish_clients.py'), '--boundary-dir',
                 str(self.run_dir / 'full-boundary'), '--expected-replicas', '2'], 30, env))
            clients = result.get('clients')
            boundary_id = result.get('requestId')
            if (result.get('status') != 'ACKNOWLEDGED' or not isinstance(clients, dict)
                    or len(clients) != 2 or not all(isinstance(row, dict) and row.get('status') == 'acknowledged'
                                                    for row in clients.values())
                    or not isinstance(boundary_id, str) or not boundary_id):
                raise ValueError('client finish uncertain')
            return {'action': 'finish', 'status': 'ACKNOWLEDGED', 'boundaryRequestId': boundary_id}
        except Exception:
            return {'action': 'finish', 'status': 'INCONCLUSIVE', 'stage': 'finish'}

    def _controlled_action(self):
        lock_path = self.run_dir / 'control' / 'action.lock'
        try:
            fd = os.open(str(lock_path), os.O_CREAT | os.O_RDWR | getattr(os, 'O_NOFOLLOW', 0), 0o600)
            if not stat.S_ISREG(os.fstat(fd).st_mode) or stat.S_IMODE(os.fstat(fd).st_mode) != 0o600:
                os.close(fd)
                raise ValueError('private control lock required')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except Exception:
                os.close(fd)
                raise
        except Exception:
            return {'action': self.action, 'status': 'INCONCLUSIVE', 'stage': 'lock'}
        try:
            if self.action in ('enable', 'disable'):
                return self._gate()
            return self._finish()
        finally:
            fcntl.flock(fd, fcntl.LOCK_UN)
            os.close(fd)

    def run_once(self):
        if self.action not in ('probe', 'enable', 'disable', 'finish'):
            raise ValueError('unsupported control action')
        if (not isinstance(self.expected_digest, str) or len(self.expected_digest) != 64
                or any(char not in '0123456789abcdef' for char in self.expected_digest)):
            raise ValueError('invalid trigger digest')
        commands = self.run_dir / 'commands'
        trigger = commands / (self.action + '.json')
        claimed = commands / (self.action + '.claimed')
        private_directory(self.run_dir)
        private_directory(commands)
        if claimed.exists():
            if trigger.exists() or trigger.is_symlink():
                raise ValueError('action already claimed')
            return None
        if not trigger.exists() and not trigger.is_symlink():
            return None
        fd = os.open(str(claimed), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        os.close(fd)
        try:
            flags = os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0)
            fd = os.open(str(trigger), flags)
            try:
                info = os.fstat(fd)
                if (not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) not in (0o600, 0o644)
                        or info.st_uid != os.geteuid() or info.st_nlink != 1 or info.st_size > 4096):
                    raise ValueError('invalid trigger file')
                raw = os.read(fd, 4097)
            finally:
                os.close(fd)
            if len(raw) > 4096 or hashlib.sha256(raw).hexdigest() != self.expected_digest:
                raise ValueError('trigger checksum mismatch')
            body = json.loads(raw.decode('utf-8'), object_pairs_hook=unique_pairs)
            if not isinstance(body, dict) or set(body) != {'runId', 'action', 'scope'}:
                raise ValueError('trigger schema mismatch')
            if body['runId'] != self.run_id or body['action'] != self.action or body['scope'] != SCOPE:
                raise ValueError('trigger target mismatch')
            directory = self.run_dir / 'control'
            directory.mkdir(mode=0o700, exist_ok=True)
            private_directory(directory)
            if self.action in ('enable', 'disable', 'finish'):
                result = self._controlled_action()
            else:
                result = {'action': self.action, 'status': 'ACKNOWLEDGED'}
        except Exception:
            result = {'action': self.action, 'status': 'REJECTED', 'stage': 'trigger'}
        directory = self.run_dir / 'control'
        directory.mkdir(mode=0o700, exist_ok=True)
        private_directory(directory)
        receipt = directory / (self.action + '.status.json')
        write_receipt(receipt, result)
        trigger.unlink()
        return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--run-dir', required=True)
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--action', choices=('probe', 'enable', 'disable', 'finish'), required=True)
    parser.add_argument('--trigger-sha256', required=True)
    parser.add_argument('--kubeconfig')
    parser.add_argument('--max-seconds', type=int, default=7200)
    args = parser.parse_args()
    if args.max_seconds < 1 or args.max_seconds > 7200:
        raise SystemExit('invalid control wait bound')
    control = WorkerControl(args.run_dir, args.run_id, args.action, args.trigger_sha256,
                            kubeconfig=args.kubeconfig)
    deadline = time.monotonic() + args.max_seconds
    next_heartbeat = 0
    while time.monotonic() < deadline:
        now = time.monotonic()
        if now >= next_heartbeat:
            write_heartbeat(Path(args.run_dir), args.run_id, args.action)
            next_heartbeat = now + 5
        result = control.run_once()
        if result is not None:
            return 0 if result['status'] in ('ACKNOWLEDGED', 'VERIFIED') else 1
        time.sleep(0.2)
    return 2


if __name__ == '__main__':
    raise SystemExit(main())
