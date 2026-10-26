#!/usr/bin/env python3
"""Local disabled-rehearsal Workbench lane; no cloud calls until final exec.

The caller MUST use subprocess.run(timeout=remaining_lease) (at most five
seconds and before observedAt + 12 for external calls). That timeout includes
lock wait and kills/waits this exact PID. This wrapper never forks the CLI:
its inheritable lock FD survives exec, so a waiting caller cannot release an
active CLI's lock. A killed CLI may leave an uncertain remote session; its
consumed intent is never removed and only a newer publication permits a call.
"""
import fcntl
import json
import math
import os
import stat
import sys
import time
import uuid

PROFILE = 'infra-ops-poc'
REGION = 'ap-southeast-1'
WORKER = 'i-t4nj1bitmcz7cuoch82h'
RECEIPT_FIELDS = {'feedSession', 'sequence', 'mode', 'desired', 'observedAt'}


class Refused(Exception):
    def __init__(self, category):
        self.category = category


def require(condition, category='configuration'):
    if not condition:
        raise Refused(category)


def private_directory(path):
    require(os.path.isabs(path))
    value = os.lstat(path)
    require(stat.S_ISDIR(value.st_mode) and stat.S_IMODE(value.st_mode) == 0o700
            and value.st_uid == os.getuid())


def private_file(value, category):
    require(stat.S_ISREG(value.st_mode) and stat.S_IMODE(value.st_mode) == 0o600
            and value.st_uid == os.getuid() and value.st_nlink == 1, category)


def identity(value):
    return (value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns,
            value.st_ctime_ns)


def finite_number(value):
    return type(value) in (int, float) and math.isfinite(value)


def read_receipt(path, session, minimum):
    """Read without following a symlink and bind both inode and content."""
    try:
        private_directory(os.path.dirname(path))
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            before = os.fstat(fd)
            private_file(before, 'receipt')
            require(before.st_size <= 4096, 'receipt')
            raw = os.read(fd, 4097)
            after = os.fstat(fd)
            current = os.lstat(path)
            require(identity(before) == identity(after) == identity(current), 'receipt')
            value = json.loads(raw.decode('utf-8'))
        finally:
            os.close(fd)
        require(type(value) is dict and set(value) == RECEIPT_FIELDS, 'receipt')
        require(value['feedSession'] == session, 'receipt')
        require(value['mode'] == 'disabled-rehearsal', 'receipt')
        require(type(value['desired']) is int and value['desired'] in (2, 3), 'receipt')
        require(type(value['sequence']) is int and value['sequence'] >= minimum, 'receipt')
        now = time.time()
        require(finite_number(value['observedAt'])
                and 0 <= now - value['observedAt'] < 7
                and 0 <= now - after.st_mtime < 1, 'receipt')
        return (identity(after), raw), value, min(after.st_mtime+1, value['observedAt']+7)
    except Refused:
        raise Refused('receipt') from None
    except Exception:
        raise Refused('receipt') from None


def consume_intent(directory, session, sequence):
    """Persist a one-use reservation before exec; uncertainty never rolls it back."""
    private_directory(directory)
    folder = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        try:
            fd = os.open(session+'.'+str(sequence)+'.intent',
                         os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                         0o600, dir_fd=folder)
        except FileExistsError:
            raise Refused('intent') from None
        try:
            private_file(os.fstat(fd), 'intent')
            os.write(fd, b'consumed\n')
            os.fsync(fd)
        finally:
            os.close(fd)
        os.fsync(folder)
    finally:
        os.close(folder)


def execute(argv, env):
    require(len(argv) == 9 and argv[:8] ==
            ['exec', '-P', PROFILE, '-r', REGION, '-i', WORKER, '-c']
            and bool(argv[8]))
    role = env.get('PGCAT_LANE_ROLE')
    require(role in ('provider', 'external'))
    real_cli = env['PGCAT_LANE_REAL_CLI']
    require(os.path.isabs(real_cli) and os.path.isfile(real_cli)
            and os.access(real_cli, os.X_OK))
    lock_path = env['PGCAT_LANE_LOCK']
    require(os.path.isabs(lock_path))
    private_directory(os.path.dirname(lock_path))
    if role == 'provider':
        deadline = float(env['PGCAT_LANE_DEADLINE_MONOTONIC'])
        require(math.isfinite(deadline))
    else:
        session = env['PGCAT_LANE_SESSION']
        require(str(uuid.UUID(session)) == session)
        minimum = int(env['PGCAT_LANE_MIN_SEQUENCE'])
        require(minimum >= 1)
        receipt_path = env['PGCAT_LANE_RECEIPT']
        intent_dir = env['PGCAT_LANE_INTENT_DIR']
        require(os.path.isabs(receipt_path))
        private_directory(intent_dir)
        original, value, fresh_until = read_receipt(receipt_path, session, minimum)
        deadline = time.clock_gettime(time.CLOCK_MONOTONIC) + fresh_until-time.time()
    # Never unlink or replace this lock: every process must use the same inode.
    fd = os.open(lock_path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        private_file(os.fstat(fd), 'configuration')
        while True:
            remaining = deadline-time.clock_gettime(time.CLOCK_MONOTONIC)
            require(remaining > 0, 'deadline' if role == 'provider' else 'receipt')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                time.sleep(min(.01, remaining))
        opened = os.fstat(fd)
        current = os.lstat(lock_path)
        private_file(current, 'configuration')
        require((opened.st_dev, opened.st_ino) == (current.st_dev, current.st_ino))
        require(time.clock_gettime(time.CLOCK_MONOTONIC) < deadline, 'deadline' if role == 'provider' else 'receipt')
        if role == 'external':
            latest, value, _ = read_receipt(receipt_path, session, minimum)
            require(latest == original, 'receipt')
            consume_intent(intent_dir, session, value['sequence'])
            latest, _, _ = read_receipt(receipt_path, session, minimum)
            require(latest == original, 'receipt')
        # No context manager, subprocess, helper child or early unlock after exec.
        os.set_inheritable(fd, True)
        try:
            os.execv(real_cli, [real_cli] + argv)
        except OSError:
            raise Refused('launch') from None
    finally:
        os.close(fd)


def main():
    try:
        execute(sys.argv[1:], os.environ)
    except Refused as error:
        category = error.category
    except Exception:
        category = 'configuration'
    sys.stderr.write('PGCAT_LANE_REFUSED:'+category+'\n')
    return 75


if __name__ == '__main__':
    sys.exit(main())
