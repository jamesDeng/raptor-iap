#!/usr/bin/env python3
"""Bounded ordinary app stdout retention; never a traffic acceptance verdict.

Linux, Python 3.6 standard library only. Access is supplied through kubectl's existing
environment, never copied into this utility's manifest or diagnostics.
"""
import argparse
import datetime
import errno
import json
import os
import re
import selectors
import shutil
import signal
import stat
import subprocess
import sys
import time


DEPLOYMENT_UID = '17b7b33c-fd64-4d07-863e-f65b6563e504'
NAMESPACE = 'raptor-test'
DEPLOYMENT = 'rdev-test-client-client'
CONTAINER = 'client'  # helm-chart/infra-test-client/templates/deployment.yaml
MAX_BYTES = 256 * 1024 * 1024
MIN_FREE_BYTES = 512 * 1024 * 1024
NOFOLLOW = os.O_NOFOLLOW


class CaptureError(Exception):
    """Only fixed, credential-free category strings cross this boundary."""


def utc_now():
    return datetime.datetime.utcnow().isoformat(timespec='microseconds') + 'Z'


def private_directory(path):
    """Walk with directory descriptors so symlink ancestors cannot redirect IO."""
    path = os.path.abspath(path)
    parts = path.split(os.sep)[1:]
    descriptor = os.open(os.sep, os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | NOFOLLOW, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        os.mkdir(parts[-1], mode=0o700, dir_fd=descriptor)
        child = os.open(parts[-1], os.O_RDONLY | os.O_DIRECTORY | NOFOLLOW, dir_fd=descriptor)
        os.fchmod(child, 0o700)
        return child
    finally:
        os.close(descriptor)


def terminate(process, hard=False):
    # Each child has its own session, including any kubectl auth-plugin children.
    if process.returncode is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGKILL if hard else signal.SIGTERM)
    except ProcessLookupError:
        pass


def child_exited(process):
    # WNOWAIT observes exit without releasing the leader's PID/PGID for reuse.
    return os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT) is not None


def release_process(process):
    if process.returncode is None:
        # The unreaped leader still pins this group ID, even after natural exit.
        # Clean up any pipe-closing descendants before reaping that leader.
        terminate(process, hard=True)
        process.wait()
    return process.returncode


def metadata(command):
    """Bound API and process time independently; discard all raw stderr."""
    process = subprocess.Popen(command, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                               start_new_session=True)
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    result = bytearray()
    deadline = time.monotonic() + 5
    try:
        while selector.get_map():
            if time.monotonic() >= deadline:
                raise CaptureError('metadata_timeout')
            for key, _ in selector.select(min(0.05, max(0, deadline - time.monotonic()))):
                chunk = os.read(key.fd, 65536)
                if not chunk:
                    selector.unregister(key.fileobj)
                else:
                    result.extend(chunk)
                    if len(result) > 2 * 1024 * 1024:
                        raise CaptureError('metadata_limit')
        while not child_exited(process):
            if time.monotonic() >= deadline:
                raise CaptureError('metadata_timeout')
            time.sleep(0.01)
        if release_process(process) != 0:
            raise CaptureError('metadata_failed')
        return json.loads(result.decode('utf-8'))
    except (ValueError, subprocess.TimeoutExpired):
        raise CaptureError('metadata_failed')
    finally:
        release_process(process)
        process.stdout.close()
        selector.close()


def safe_identifier(value):
    if not isinstance(value, str) or not re.match(r'\A[A-Za-z0-9_.:/-]{1,256}\Z', value):
        raise CaptureError('metadata_identity')
    return value


def controlled_by(item, kind, name, uid):
    references = item['metadata'].get('ownerReferences', [])
    controllers = [r for r in references if r.get('controller') is True]
    return len(controllers) == 1 and all([
        controllers[0].get('kind') == kind, controllers[0].get('name') == name,
        controllers[0].get('uid') == uid])


class Recorder:
    def __init__(self, args, directory):
        self.args = args
        self.directory = directory
        self.selector = selectors.DefaultSelector()
        self.streams = []
        self.current = set()
        self.initial = set()
        self.ready = False
        self.stopped = False
        self.failed = False
        self.total = 0
        self.serial = 0
        self.manifest_inode = None
        self.document = {'schema': 1, 'status': 'STARTING', 'startedAt': utc_now(),
                         'deploymentUid': DEPLOYMENT_UID, 'namespace': NAMESPACE,
                         'deployment': DEPLOYMENT, 'container': CONTAINER,
                         'recorderPid': os.getpid(), 'maxBytes': args.max_bytes,
                         'emergencyStatusBytes': 1024 * 1024,
                         'maxSeconds': args.seconds, 'bytes': 0, 'problems': [],
                         'coverage': 'ordinary_streams_only; rotation, discovery races and unavailable history may leave gaps',
                         'streams': []}

    def problem(self, category, fatal=True):
        if category not in self.document['problems']:
            self.document['problems'].append(category)
        if fatal:
            self.failed = True

    def save(self):
        self.document['bytes'] = self.total
        self.serial += 1
        name = '.manifest-{}'.format(self.serial)
        descriptor = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | NOFOLLOW,
                             0o600, dir_fd=self.directory)
        try:
            with os.fdopen(descriptor, 'w') as target:
                json.dump(self.document, target, sort_keys=True)
                target.write('\n')
                target.flush()
                os.fsync(target.fileno())
            if self.manifest_inode is not None:
                current = os.stat('manifest.json', dir_fd=self.directory, follow_symlinks=False)
                if current.st_ino != self.manifest_inode or not stat.S_ISREG(current.st_mode):
                    raise CaptureError('unsafe_manifest')
            else:
                # No earlier file, even a dangling symlink, may be overwritten.
                try:
                    os.stat('manifest.json', dir_fd=self.directory, follow_symlinks=False)
                except FileNotFoundError:
                    pass
                else:
                    raise CaptureError('unsafe_manifest')
            os.replace(name, 'manifest.json', src_dir_fd=self.directory, dst_dir_fd=self.directory)
            self.manifest_inode = os.stat('manifest.json', dir_fd=self.directory,
                                          follow_symlinks=False).st_ino
            os.fsync(self.directory)
        finally:
            try:
                os.unlink(name, dir_fd=self.directory)
            except FileNotFoundError:
                pass

    def get(self, resource, name=None):
        command = [self.args.kubectl, '--namespace', NAMESPACE, '--request-timeout=5s',
                   'get', resource]
        if name:
            command.append(name)
        return metadata(command + ['-o', 'json'])

    def identity(self, pod, replica_sets):
        meta = pod['metadata']
        if meta.get('namespace') != NAMESPACE:
            return None
        owners = [r for r in meta.get('ownerReferences', []) if r.get('controller') is True]
        if len(owners) != 1:
            return None
        rs = replica_sets.get(owners[0].get('uid'))
        if rs is None or not controlled_by(pod, 'ReplicaSet', rs['name'], rs['uid']):
            return None
        if CONTAINER not in [c['name'] for c in pod['spec']['containers']]:
            raise CaptureError('container_missing')
        statuses = [c for c in pod.get('status', {}).get('containerStatuses', [])
                    if c['name'] == CONTAINER]
        if len(statuses) != 1 or 'running' not in statuses[0].get('state', {}):
            return None
        status = statuses[0]
        return {'podName': safe_identifier(meta['name']), 'podUid': safe_identifier(meta['uid']),
                'replicaSetUid': safe_identifier(rs['uid']),
                'containerId': safe_identifier(status['containerID']),
                'restartCount': int(status.get('restartCount', 0)),
                'containerReady': status.get('ready') is True}

    def discover(self, first=False):
        deployment = self.get('deployment', DEPLOYMENT)
        meta = deployment['metadata']
        if (meta['uid'], meta['name'], meta.get('namespace')) != (DEPLOYMENT_UID, DEPLOYMENT, NAMESPACE):
            raise CaptureError('deployment_binding')
        replica_sets = {}
        for rs in self.get('replicasets')['items']:
            rm = rs['metadata']
            if rm.get('namespace') == NAMESPACE and controlled_by(rs, 'Deployment', DEPLOYMENT, DEPLOYMENT_UID):
                replica_sets[safe_identifier(rm['uid'])] = rm
        found = []
        for pod in self.get('pods')['items']:
            item = self.identity(pod, replica_sets)
            if item:
                found.append(item)
        keys = {(p['podUid'], p['containerId']) for p in found}
        if len(keys) != len(found):
            raise CaptureError('duplicate_container')
        if first:
            if (len(found) != 2 or not all(p['containerReady'] for p in found) or
                    deployment['spec']['replicas'] != 2 or deployment.get('status', {}).get('readyReplicas') != 2):
                raise CaptureError('initial_two_ready_required')
            self.initial = keys
        elif not self.ready and keys != self.initial:
            raise CaptureError('initial_roster_changed')
        self.current = keys
        known = {s['key'] for s in self.streams}
        for item in found:
            key = (item['podUid'], item['containerId'])
            if key in known:
                continue
            # Check the named Pod again directly before logs; kubectl's name-based
            # log API cannot make this check atomic with stream establishment.
            if self.identity(self.get('pod', item['podName']), replica_sets) != item:
                raise CaptureError('pod_changed_before_stream')
            if any(s['record']['podUid'] == item['podUid'] for s in self.streams) or item['restartCount']:
                self.problem('container_restart', fatal=False)
            self.start_stream(item, key, first)
        for stream in self.streams:
            if stream['key'] not in self.current and not stream['stopping']:
                # A removed/terminated Pod can still have final output buffered
                # upstream. Allow natural EOF before terminating its log stream.
                stream['stopping'] = True
                stream['retiredAt'] = time.monotonic()
                stream['record']['exitCategory'] = 'container_ended'

    def start_stream(self, item, key, initial):
        if len(self.streams) >= 1024:
            raise CaptureError('stream_count_limit')
        filename = 'stream-{:04d}.log'.format(len(self.streams) + 1)
        descriptor = os.open(filename, os.O_WRONLY | os.O_CREAT | os.O_EXCL | NOFOLLOW,
                             0o600, dir_fd=self.directory)
        command = [self.args.kubectl, '--namespace', NAMESPACE, '--request-timeout=0',
                   'logs', item['podName'], '--container', CONTAINER, '--follow',
                   '--timestamps', '--tail=-1']
        if initial:
            command.append('--since-time=' + self.document['startedAt'])
        record = dict(item, file=filename, bytes=0, startedAt=utc_now(), endedAt=None,
                      exitCode=None, exitCategory=None, history='recording_start' if initial else 'available_start')
        self.document['streams'].append(record)
        try:
            process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, start_new_session=True)
        except OSError:
            os.close(descriptor)
            record['exitCategory'] = 'stream_spawn_failed'
            raise CaptureError('stream_spawn_failed')
        record['pid'] = process.pid
        stream = {'key': key, 'record': record, 'process': process, 'fd': descriptor,
                  'stopping': False, 'stopAt': None, 'owned': True}
        self.streams.append(stream)
        self.selector.register(process.stdout, selectors.EVENT_READ, (stream, True))
        self.selector.register(process.stderr, selectors.EVENT_READ, (stream, False))

    def stop_stream(self, stream, category):
        if not stream['owned'] or child_exited(stream['process']):
            return
        stream['stopping'] = True
        stream['stopAt'] = time.monotonic()
        stream['record']['exitCategory'] = category
        terminate(stream['process'])

    def pump(self, timeout=0.05):
        for key, _ in self.selector.select(timeout):
            stream, stdout = key.data
            chunk = os.read(key.fd, 65536)
            if not chunk:
                self.selector.unregister(key.fileobj)
                key.fileobj.close()
                continue
            if stdout:
                remaining = self.args.max_bytes - self.total
                if len(chunk) > remaining:
                    self.problem('byte_limit')
                chunk = chunk[:remaining]
                while chunk:
                    try:
                        count = os.write(stream['fd'], chunk)
                        if count <= 0:
                            raise OSError(errno.EIO, 'short write')
                    except OSError as error:
                        self.problem('disk_full' if error.errno == errno.ENOSPC else 'log_write_failed')
                        break
                    self.total += count
                    stream['record']['bytes'] += count
                    chunk = chunk[count:]
        pending_exits = []
        for stream in self.streams:
            if not stream['owned']:
                continue
            process = stream['process']
            code = None
            if child_exited(process):
                code = release_process(process)
                stream['owned'] = False
                stream['record']['exitCode'] = code
                stream['record']['endedAt'] = utc_now()
                if not stream['stopping']:
                    pending_exits.append(stream)
                elif code not in (0, -signal.SIGTERM) and not (
                        code == 1 and stream['record']['exitCategory'] == 'operator_stop'):
                    self.problem('stream_stop_failed')
            if stream['stopping'] and code is None:
                if stream['stopAt'] is None:
                    if time.monotonic() - stream['retiredAt'] >= 2:
                        self.problem('retired_stream_interrupted', fatal=False)
                        self.stop_stream(stream, 'retired_stream_interrupted')
                elif time.monotonic() - stream['stopAt'] >= 2:
                    self.problem('stream_killed')
                    terminate(process, hard=True)
        if pending_exits:
            # A normal container termination can close logs before the next
            # periodic inventory. Refresh ownership/lifecycle before classifying.
            if not self.stopped and not self.failed:
                self.discover()
            for stream in pending_exits:
                if not stream['stopping'] or stream['record']['exitCode'] != 0:
                    stream['record']['exitCategory'] = 'unexpected_stream_exit'
                    self.problem('unexpected_stream_exit')

    def stop_requested(self):
        try:
            info = os.stat('STOP', dir_fd=self.directory, follow_symlinks=False)
        except FileNotFoundError:
            return self.stopped
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid():
            raise CaptureError('unsafe_stop_file')
        return True

    def finish(self):
        for stream in self.streams:
            if stream['owned'] and (not stream['stopping'] or stream['stopAt'] is None):
                if stream['stopping']:
                    self.problem('retired_stream_interrupted', fatal=False)
                self.stop_stream(stream, 'operator_stop' if self.stopped else 'recorder_stop')
        # Read throughout graceful termination and then through EOF, rather than
        # polling file sizes or dropping already buffered stdout on STOP.
        deadline = time.monotonic() + 5
        while self.selector.get_map() and time.monotonic() < deadline:
            self.pump()
        if self.selector.get_map():
            self.problem('stream_drain_timeout')
        for stream in self.streams:
            process = stream['process']
            if stream['owned']:
                stream['record']['exitCode'] = release_process(process)
                stream['owned'] = False
                stream['record']['endedAt'] = utc_now()
            for pipe in (process.stdout, process.stderr):
                if not pipe.closed:
                    self.selector.unregister(pipe)
                    pipe.close()
            try:
                os.fsync(stream['fd'])
            except OSError:
                self.problem('log_flush_failed')
            os.close(stream['fd'])
        self.selector.close()
        if not self.ready:
            self.problem('initial_logs_missing', fatal=False)
        self.document['endedAt'] = utc_now()
        self.document['status'] = ('FAILED' if self.failed else
                                   'UNRELIABLE' if self.document['problems'] else 'COMPLETED')
        try:
            os.unlink('status-reserve', dir_fd=self.directory)
        except FileNotFoundError:
            pass
        self.save()

    def run(self):
        started = time.monotonic()
        self.save()
        try:
            if shutil.disk_usage(self.args.output_dir).free < MIN_FREE_BYTES:
                raise CaptureError('initial_disk_space')
            # Leave room to persist the failure manifest even if another process
            # fills the worker disk during recording. This is fixed metadata
            # overhead, separate from the stricter cumulative app-log byte cap.
            reserve = os.open('status-reserve', os.O_WRONLY | os.O_CREAT | os.O_EXCL | NOFOLLOW,
                              0o600, dir_fd=self.directory)
            with os.fdopen(reserve, 'wb') as target:
                target.write(b'\0' * self.document['emergencyStatusBytes'])
                target.flush()
                os.fsync(target.fileno())
            self.discover(first=True)
            next_discovery = 0
            next_status = 0
            while not self.failed:
                if self.stop_requested():
                    self.stopped = True
                    break
                if time.monotonic() - started >= self.args.seconds:
                    raise CaptureError('duration_limit')
                if time.monotonic() >= next_discovery:
                    self.discover()
                    next_discovery = time.monotonic() + 0.5
                self.pump()
                if not self.failed and not self.ready and all(s['record']['bytes'] > 0 for s in self.streams):
                    self.ready = True
                    self.document['status'] = 'READY'
                    self.document['readyAt'] = utc_now()
                if time.monotonic() >= next_status:
                    self.save()
                    next_status = time.monotonic() + 0.5
        except CaptureError as error:
            self.problem(str(error))
        except (AttributeError, IndexError, KeyError, TypeError, ValueError):
            self.problem('metadata_invalid')
        except OSError as error:
            self.problem('disk_full' if error.errno == errno.ENOSPC else 'io_failed')
        finally:
            self.finish()
        return 0 if self.document['status'] == 'COMPLETED' else 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--namespace', required=True, choices=[NAMESPACE])
    parser.add_argument('--deployment', required=True, choices=[DEPLOYMENT])
    parser.add_argument('--deployment-uid', required=True, choices=[DEPLOYMENT_UID])
    parser.add_argument('--output-dir', required=True, help='new unique directory; every ancestor must be a real directory')
    parser.add_argument('--kubectl', default='kubectl')
    parser.add_argument('--seconds', type=float, default=7200, help='duration ceiling, maximum 7200 seconds')
    parser.add_argument('--max-bytes', type=int, default=MAX_BYTES, help='log byte ceiling, at most 268435456')
    args = parser.parse_args(argv)
    if not 0 < args.seconds <= 7200 or not 0 < args.max_bytes <= MAX_BYTES:
        parser.error('limits must be positive and within their documented ceilings')
    if not sys.platform.startswith('linux') or not hasattr(os, 'waitid'):
        sys.stderr.write('app_log_retention: requires Linux with os.waitid(WNOWAIT)\n')
        return 1
    directory = None
    previous = {}
    try:
        directory = private_directory(args.output_dir)
        recorder = Recorder(args, directory)
        def stop(signum, frame):
            recorder.stopped = True
        for sig in (signal.SIGTERM, signal.SIGINT):
            previous[sig] = signal.signal(sig, stop)
        return recorder.run()
    except (OSError, CaptureError):
        # Deliberately omit raw errors, command arguments and access paths.
        sys.stderr.write('app_log_retention: private capture IO failed; inspect storage and retained status\n')
        return 1
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)
        if directory is not None:
            os.close(directory)


if __name__ == '__main__':
    sys.exit(main())
