"""Exercise the real recorder against isolated fake kubectl processes."""
import ast
import contextlib
import importlib.util
import json
import io
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch


HERE = Path(__file__).parent
HELPER = HERE / 'app_log_retention.py'
UID = '17b7b33c-fd64-4d07-863e-f65b6563e504'


def owner(kind, name, uid):
    return {'kind': kind, 'name': name, 'uid': uid, 'controller': True}


def pod(name, uid, container_id, rs='rs-1'):
    return {'metadata': {'name': name, 'namespace': 'raptor-test', 'uid': uid,
                         'ownerReferences': [owner('ReplicaSet', rs, rs)]},
            'spec': {'containers': [{'name': 'client'}]},
            'status': {'phase': 'Running', 'containerStatuses': [
                {'name': 'client', 'containerID': container_id, 'ready': True,
                 'restartCount': 0, 'state': {'running': {'startedAt': '2026-11-04T00:00:00Z'}}}]}}


FAKE = r'''#!/usr/bin/env python3
import json, os, signal, sys, time
from pathlib import Path
args = sys.argv[1:]
root = Path(os.environ['FAKE_ROOT'])
with (root / 'calls').open('a') as f:
    f.write(json.dumps(args) + '\n')
state = json.loads((root / 'state').read_text())
if 'get' in args:
    if state.get('metadata_error'):
        sys.stderr.write('secret-token-must-not-escape')
        sys.exit(1)
    what = args[args.index('get') + 1]
    if what == 'deployment':
        value = state['deployment']
    elif what == 'replicasets':
        value = {'items': state['replicasets']}
    elif what == 'pods':
        value = {'items': state['pods']}
    elif what == 'pod':
        name = args[args.index('get') + 2]
        value = next(p for p in state['pods'] if p['metadata']['name'] == name)
    else:
        sys.exit(8)
    print(json.dumps(value))
    sys.exit(0)
assert '--request-timeout=0' in args
assert '--follow' in args and '--timestamps' in args and '--tail=-1' in args
name = args[args.index('logs') + 1]
p = next(p for p in state['pods'] if p['metadata']['name'] == name)
identity = p['status']['containerStatuses'][0]['containerID']
with (root / 'pids').open('a') as f:
    f.write(str(os.getpid()) + '\n')
def stop(signum, frame):
    os.write(1, ('2026-11-04T00:00:01Z terminal-' + identity + '\n').encode())
    sys.exit(0)
signal.signal(signal.SIGTERM, stop)
if state.get('descendant'):
    child = os.fork()
    if child == 0:
        signal.signal(signal.SIGTERM, signal.SIG_DFL)
        null = os.open(os.devnull, os.O_WRONLY)
        os.dup2(null, 1)
        os.dup2(null, 2)
        os.close(null)
        time.sleep(120)
        os._exit(0)
    with (root / 'descendants').open('a') as f:
        f.write(name + ' ' + str(child) + '\n')
if not state.get('quiet'):
    os.write(1, ('2026-11-04T00:00:00Z output-' + identity + '\n').encode())
if state.get('exit_stream'):
    sys.stderr.write('secret-token-must-not-escape')
    sys.exit(state['exit_stream'])
while True:
    live = json.loads((root / 'state').read_text())
    if name in live.get('end_streams', []):
        sys.exit(0)
    current = [p for p in live['pods'] if p['metadata']['name'] == name]
    if not live.get('hold_retired') and (not current or
            'running' not in current[0]['status']['containerStatuses'][0]['state']):
        time.sleep(live.get('final_delay', 0))
        os.write(1, ('2026-11-04T00:00:01Z natural-final-' + identity + '\n').encode())
        sys.exit(0)
    if state.get('flood'):
        os.write(1, b'x' * 8192)
    time.sleep(0.01)
'''


class AppLogRetentionTests(unittest.TestCase):
    def setUp(self):
        # Missing implementation is an assertion failure during the initial RED run.
        self.assertTrue(HELPER.is_file(), 'ordinary app-log recorder is not implemented')
        if (not sys.platform.startswith('linux') or not hasattr(os, 'waitid')) and self._testMethodName not in (
                'test_python36_syntax_and_cli_help', 'test_unsupported_platform_refuses_capture_clearly'):
            self.skipTest('runtime capture requires Linux os.waitid(WNOWAIT); verified in Linux CI/worker')
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.fake = self.root / 'kubectl'
        self.fake.write_text(FAKE)
        self.fake.chmod(0o700)
        self.output = self.root / 'capture'
        self.state = {
            'deployment': {'metadata': {'name': 'rdev-test-client-client',
                                       'namespace': 'raptor-test', 'uid': UID},
                           'spec': {'replicas': 2}, 'status': {'readyReplicas': 2}},
            'replicasets': [{'metadata': {'name': 'rs-1', 'namespace': 'raptor-test',
                                         'uid': 'rs-1', 'ownerReferences': [
                                             owner('Deployment', 'rdev-test-client-client', UID)]}}],
            'pods': [pod('old-a', 'pod-a', 'containerd://a'),
                     pod('old-b', 'pod-b', 'containerd://b')]}
        self.write_state()
        self.process = None

    def write_state(self):
        pending = self.root / 'state-new'
        pending.write_text(json.dumps(self.state))
        pending.replace(self.root / 'state')

    def launch(self, *extra):
        command = [sys.executable, str(HELPER), '--kubectl', str(self.fake),
                   '--namespace', 'raptor-test', '--deployment', 'rdev-test-client-client',
                   '--deployment-uid', UID, '--output-dir', str(self.output)]
        self.process = subprocess.Popen(command + list(extra), env=dict(os.environ, FAKE_ROOT=str(self.root)),
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.addCleanup(self.reap)
        return self.process

    def reap(self):
        if self.process is not None:
            if self.process.poll() is None:
                self.process.terminate()
            # Python3.6 communicate(timeout=...) tries to register already closed
            # pipes on a repeated call. Keep only open pipes eligible for drain.
            for name in ('stdout', 'stderr'):
                stream = getattr(self.process, name)
                if stream is not None and stream.closed:
                    setattr(self.process, name, None)
            if self.process.stdout is None and self.process.stderr is None:
                self.process.wait(timeout=8)
            else:
                self.process.communicate(timeout=8)

    def manifest(self):
        return json.loads((self.output / 'manifest.json').read_text())

    def await_condition(self, predicate):
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            try:
                if predicate():
                    return
            except (OSError, ValueError, KeyError):
                pass
            if self.process.poll() is not None:
                break
            time.sleep(0.03)
        self.fail('recorder did not reach expected observable state; manifest=' +
                  ((self.output / 'manifest.json').read_text() if (self.output / 'manifest.json').exists() else 'absent'))

    def stop(self, sig=False):
        if sig:
            self.process.send_signal(signal.SIGTERM)
        else:
            (self.output / 'STOP').touch(mode=0o600)
        self.process.communicate(timeout=8)
        for value in (self.root / 'pids').read_text().splitlines():
            with self.assertRaises(ProcessLookupError):
                os.kill(int(value), 0)
        return self.manifest()

    def test_owned_rollout_keeps_old_logs_and_discovers_surge_without_labels(self):
        self.state['replicasets'].append({'metadata': {
            'name': 'foreign', 'namespace': 'raptor-test', 'uid': 'foreign',
            'ownerReferences': [owner('Deployment', 'other', 'wrong')]}})
        self.state['pods'].append(pod('foreign', 'foreign', 'containerd://foreign', 'foreign'))
        self.state['pods'].append(pod('label-only', 'label-only', 'containerd://label', 'unknown'))
        self.write_state()
        self.launch()
        self.await_condition(lambda: self.manifest()['status'] == 'READY')
        self.state['pods'].append(pod('new-c', 'pod-c', 'containerd://c'))
        self.write_state()
        self.await_condition(lambda: len(self.manifest()['streams']) == 3 and
                             all(s['bytes'] > 0 for s in self.manifest()['streams']))
        self.state['pods'] = [self.state['pods'][2], self.state['pods'][3], self.state['pods'][4]]
        self.write_state()
        self.await_condition(lambda: all(s['exitCode'] is not None for s in self.manifest()['streams'][:2]))
        result = self.stop()
        self.assertEqual(result['status'], 'COMPLETED')
        self.assertEqual({s['podUid'] for s in result['streams']}, {'pod-a', 'pod-b', 'pod-c'})
        self.assertIn('output-containerd://a', (self.output / result['streams'][0]['file']).read_text())
        calls = [json.loads(line) for line in (self.root / 'calls').read_text().splitlines()]
        logs = [c for c in calls if 'logs' in c]
        self.assertEqual(len(logs), 3)
        self.assertTrue(any(a.startswith('--since-time=') for a in logs[0]))
        self.assertFalse(any(a.startswith('--since-time=') for a in logs[2]))
        self.assertEqual(self.output.stat().st_mode & 0o777, 0o700)
        for file in self.output.iterdir():
            self.assertEqual(file.stat().st_mode & 0o777, 0o600)

    def test_same_pod_restart_creates_distinct_container_stream(self):
        self.launch()
        self.await_condition(lambda: self.manifest()['status'] == 'READY')
        self.state['pods'][0]['status']['containerStatuses'][0]['containerID'] = 'containerd://a2'
        self.state['pods'][0]['status']['containerStatuses'][0]['restartCount'] = 1
        self.write_state()
        self.await_condition(lambda: len(self.manifest()['streams']) == 3)
        result = self.stop(sig=True)
        self.assertEqual({s['containerId'] for s in result['streams']},
                         {'containerd://a', 'containerd://b', 'containerd://a2'})
        self.assertEqual(len({s['file'] for s in result['streams']}), 3)
        self.assertEqual(result['status'], 'UNRELIABLE')
        self.assertIn('container_restart', result['problems'])

    def test_stop_drains_terminal_bytes_and_reaps_streams(self):
        self.launch()
        self.await_condition(lambda: self.manifest()['status'] == 'READY')
        result = self.stop(sig=True)
        self.assertEqual(self.process.returncode, 0)
        self.assertEqual(result['status'], 'COMPLETED')
        for stream in result['streams']:
            self.assertIn('terminal-', (self.output / stream['file']).read_text())

    def test_final_stop_never_signals_reused_retired_group_and_drains_active_stream(self):
        spec = importlib.util.spec_from_file_location('app_log_retention', HELPER)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        directory = module.private_directory(str(self.output))
        self.addCleanup(os.close, directory)
        recorder = module.Recorder(SimpleNamespace(kubectl=str(self.fake),
                                                  output_dir=str(self.output),
                                                  max_bytes=268435456, seconds=7200), directory)
        with patch.dict(os.environ, FAKE_ROOT=str(self.root)):
            recorder.discover(first=True)
            deadline = time.monotonic() + 5
            while not all(s['record']['bytes'] for s in recorder.streams):
                self.assertLess(time.monotonic(), deadline)
                recorder.pump()
            recorder.ready = True
            self.state['pods'].pop(0)
            self.write_state()
            recorder.discover()
            old, active = recorder.streams
            deadline = time.monotonic() + 5
            while old['record']['exitCode'] is None:
                self.assertLess(time.monotonic(), deadline)
                recorder.pump()
            old_pid = old['process'].pid
            with self.assertRaises(ProcessLookupError):
                os.kill(old_pid, 0)
            original_category = old['record']['exitCategory']
            real_killpg = module.os.killpg
            reused_signals = []
            def simulate_reuse(group, sig):
                if group == old_pid:
                    # The numeric group now belongs to an unrelated process.
                    reused_signals.append(sig)
                    return
                return real_killpg(group, sig)
            recorder.stopped = True
            with patch.object(module.os, 'killpg', side_effect=simulate_reuse):
                recorder.finish()
        self.assertEqual(reused_signals, [], 'final STOP signaled released process-group ID')
        self.assertEqual(old['record']['exitCategory'], original_category)
        self.assertIn('terminal-', (self.output / active['record']['file']).read_text())
        self.assertEqual(recorder.document['status'], 'COMPLETED')
        with self.assertRaises(ProcessLookupError):
            os.kill(active['process'].pid, 0)

    def test_natural_stream_retirement_cleans_descendants_before_releasing_leader(self):
        self.state['descendant'] = True
        self.write_state()
        self.launch()
        self.await_condition(lambda: self.manifest()['status'] == 'READY' and
                             len((self.root / 'descendants').read_text().splitlines()) == 2)
        self.state['pods'].pop(0)
        self.write_state()
        self.await_condition(lambda: self.manifest()['streams'][0]['exitCode'] is not None)
        old_child = next(int(line.split()[1]) for line in
                         (self.root / 'descendants').read_text().splitlines()
                         if line.startswith('old-a '))
        # The retired stream's child has closed its inherited pipes but remains
        # in the owned process group. Merely reaping the leader would leak it.
        gone = False
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            try:
                os.kill(old_child, 0)
            except ProcessLookupError:
                gone = True
                break
            time.sleep(0.03)
        result = self.stop()
        self.assertTrue(gone, 'retired kubectl descendant survived ownership release')
        self.assertEqual(result['streams'][0]['exitCategory'], 'container_ended')

    def test_ready_requires_output_from_both_current_containers(self):
        self.state['quiet'] = True
        self.write_state()
        self.launch()
        self.await_condition(lambda: len(self.manifest()['streams']) == 2)
        time.sleep(0.3)
        self.assertEqual(self.manifest()['status'], 'STARTING')
        result = self.stop()
        self.assertNotEqual(result['status'], 'COMPLETED')
        self.assertIn('initial_logs_missing', result['problems'])

    def test_stream_exit_is_sticky_failure_and_errors_are_sanitized(self):
        self.state['exit_stream'] = 9
        self.write_state()
        self.launch()
        self.process.communicate(timeout=8)
        result = self.manifest()
        self.assertEqual(result['status'], 'FAILED')
        self.assertIn('unexpected_stream_exit', result['problems'])
        self.assertNotIn('secret-token', json.dumps(result))
        self.assertNotEqual(self.process.returncode, 0)

    def test_metadata_failure_after_ready_is_not_erased(self):
        self.launch()
        self.await_condition(lambda: self.manifest()['status'] == 'READY')
        self.state['metadata_error'] = True
        self.write_state()
        self.process.communicate(timeout=8)
        self.assertEqual(self.manifest()['status'], 'FAILED')
        self.assertIn('metadata_failed', self.manifest()['problems'])
        self.assertNotIn('secret-token', json.dumps(self.manifest()))

    def test_clean_stream_exit_after_pod_termination_is_expected(self):
        self.launch()
        self.await_condition(lambda: self.manifest()['status'] == 'READY')
        self.state['pods'][0]['status']['containerStatuses'][0]['state'] = {
            'terminated': {'exitCode': 0}}
        self.state['end_streams'] = ['old-a']
        self.write_state()
        self.await_condition(lambda: self.manifest()['streams'][0]['exitCode'] is not None)
        self.assertEqual(self.stop()['status'], 'COMPLETED')

    def test_retiring_container_stream_reaches_natural_eof_with_final_output(self):
        self.state['final_delay'] = 1
        self.write_state()
        self.launch()
        self.await_condition(lambda: self.manifest()['status'] == 'READY')
        self.state['pods'][0]['status']['containerStatuses'][0]['state'] = {
            'terminated': {'exitCode': 0}}
        self.write_state()
        self.await_condition(lambda: self.manifest()['streams'][0]['exitCode'] is not None)
        result = self.stop()
        self.assertIn('natural-final-containerd://a',
                      (self.output / result['streams'][0]['file']).read_text())
        self.assertEqual(result['status'], 'COMPLETED')

    def test_retirement_stream_that_never_ends_reports_interruption_gap(self):
        self.state['hold_retired'] = True
        self.write_state()
        self.launch()
        self.await_condition(lambda: self.manifest()['status'] == 'READY')
        self.state['pods'].pop(0)
        self.write_state()
        self.await_condition(lambda: self.manifest()['streams'][0]['exitCode'] is not None)
        result = self.stop()
        self.assertEqual(result['status'], 'UNRELIABLE')
        self.assertIn('retired_stream_interrupted', result['problems'])

    def test_clean_exit_while_container_still_running_is_a_failure(self):
        self.state['end_streams'] = ['old-a']
        self.write_state()
        self.launch()
        self.process.communicate(timeout=8)
        self.assertEqual(self.manifest()['status'], 'FAILED')
        self.assertIn('unexpected_stream_exit', self.manifest()['problems'])

    def test_disk_full_during_log_write_retains_failure_status(self):
        spec = importlib.util.spec_from_file_location('app_log_retention', HELPER)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        import errno
        with patch.dict(os.environ, FAKE_ROOT=str(self.root)), patch.object(
                module.os, 'write', side_effect=OSError(errno.ENOSPC, 'private error')):
            result = module.main(['--kubectl', str(self.fake), '--namespace', 'raptor-test',
                                  '--deployment', 'rdev-test-client-client', '--deployment-uid', UID,
                                  '--output-dir', str(self.output)])
        self.assertNotEqual(result, 0)
        self.assertEqual(self.manifest()['status'], 'FAILED')
        self.assertIn('disk_full', self.manifest()['problems'])

    def test_manifest_disk_full_releases_emergency_space_for_failure_status(self):
        spec = importlib.util.spec_from_file_location('app_log_retention', HELPER)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        import errno
        real_replace = module.os.replace
        def full_until_reserve_released(source, destination, **kwargs):
            incoming = json.loads((self.output / source).read_text())
            if incoming['status'] in ('READY', 'FAILED') and (self.output / 'status-reserve').exists():
                raise OSError(errno.ENOSPC, 'private error')
            if incoming['status'] == 'READY':
                raise OSError(errno.ENOSPC, 'private error')
            return real_replace(source, destination, **kwargs)
        with patch.dict(os.environ, FAKE_ROOT=str(self.root)), patch.object(
                module.os, 'replace', side_effect=full_until_reserve_released):
            result = module.main(['--kubectl', str(self.fake), '--namespace', 'raptor-test',
                                  '--deployment', 'rdev-test-client-client', '--deployment-uid', UID,
                                  '--output-dir', str(self.output)])
        self.assertNotEqual(result, 0)
        self.assertEqual(self.manifest()['status'], 'FAILED')
        self.assertIn('disk_full', self.manifest()['problems'])
        self.assertGreater(self.manifest().get('emergencyStatusBytes', 0), 0)
        self.assertFalse((self.output / 'status-reserve').exists())

    def test_read_time_cap_never_writes_over_limit(self):
        self.state['flood'] = True
        self.write_state()
        self.launch('--max-bytes', '256')
        self.process.communicate(timeout=8)
        result = self.manifest()
        self.assertEqual(result['status'], 'FAILED')
        self.assertIn('byte_limit', result['problems'])
        self.assertLessEqual(sum(p.stat().st_size for p in self.output.glob('*.log')), 256)

    def test_deployment_uid_mismatch_refuses_all_streams(self):
        self.state['deployment']['metadata']['uid'] = 'wrong'
        self.write_state()
        self.launch()
        self.process.communicate(timeout=8)
        self.assertEqual(self.manifest()['status'], 'FAILED')
        self.assertEqual(self.manifest()['streams'], [])
        self.assertFalse((self.root / 'pids').exists())

    def test_existing_directory_and_symlink_ancestor_are_refused(self):
        self.output.mkdir()
        marker = self.output / 'keep'
        marker.write_text('untouched')
        self.launch()
        self.process.communicate(timeout=8)
        self.assertNotEqual(self.process.returncode, 0)
        self.assertEqual(marker.read_text(), 'untouched')
        self.assertFalse((self.output / 'manifest.json').exists())
        link = self.root / 'link'
        link.symlink_to(self.root, target_is_directory=True)
        self.output = link / 'symlink-capture'
        self.launch()
        self.process.communicate(timeout=8)
        self.assertNotEqual(self.process.returncode, 0)
        self.assertFalse(self.output.exists())

    def test_symlink_stop_is_a_failure(self):
        self.launch()
        self.await_condition(lambda: self.manifest()['status'] == 'READY')
        (self.output / 'STOP').symlink_to(self.root / 'absent')
        self.process.communicate(timeout=8)
        self.assertEqual(self.manifest()['status'], 'FAILED')
        self.assertIn('unsafe_stop_file', self.manifest()['problems'])

    def test_duration_limit_stops_all_streams(self):
        self.launch('--seconds', '0.5')
        self.process.communicate(timeout=8)
        self.assertNotEqual(self.process.returncode, 0)
        self.assertIn('duration_limit', self.manifest()['problems'])
        for value in (self.root / 'pids').read_text().splitlines():
            with self.assertRaises(ProcessLookupError):
                os.kill(int(value), 0)

    def test_low_disk_refuses_before_metadata_or_stream_spawn(self):
        spec = importlib.util.spec_from_file_location('app_log_retention', HELPER)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        arguments = ['--kubectl', str(self.fake), '--namespace', 'raptor-test',
                     '--deployment', 'rdev-test-client-client', '--deployment-uid', UID,
                     '--output-dir', str(self.output)]
        with patch.object(module.shutil, 'disk_usage', return_value=type('Disk', (), {'free': 1})()):
            self.assertNotEqual(module.main(arguments), 0)
        self.assertIn('initial_disk_space', self.manifest()['problems'])
        self.assertFalse((self.root / 'calls').exists())

    def test_python36_syntax_and_cli_help(self):
        if sys.version_info >= (3, 8):
            ast.parse(HELPER.read_text(), feature_version=(3, 6))
        else:
            ast.parse(HELPER.read_text())
        result = subprocess.run([sys.executable, str(HELPER), '--help'], stdout=subprocess.PIPE)
        self.assertEqual(result.returncode, 0)
        self.assertIn(b'--deployment-uid', result.stdout)

    def test_unsupported_platform_refuses_capture_clearly(self):
        spec = importlib.util.spec_from_file_location('app_log_retention', HELPER)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        output = io.StringIO()
        with patch.object(module.sys, 'platform', 'unsupported'), contextlib.redirect_stderr(output):
            result = module.main(['--namespace', 'raptor-test', '--deployment', 'rdev-test-client-client',
                                  '--deployment-uid', UID, '--output-dir', str(self.output)])
        self.assertNotEqual(result, 0)
        self.assertIn('requires Linux with os.waitid(WNOWAIT)', output.getvalue())
        self.assertFalse(self.output.exists())


if __name__ == '__main__':
    unittest.main()
