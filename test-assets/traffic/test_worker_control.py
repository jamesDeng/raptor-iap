"""One-use worker control beside the live provider feed."""
import hashlib
import fcntl
import json
import os
import tempfile
import unittest
from unittest import mock
from pathlib import Path

from worker_control import WorkerControl, main


RUN_ID = '4b762f6f71cf4fd6a30b29363a78dce9'
SCOPE = {
    'cluster': 'c92787e953503492ea141a744c81498f1',
    'group': 'asg-t4necb4dfmu6o3z2rn0g',
    'proxy': 'DP00001',
    'region': 'ap-southeast-1',
}


class WorkerControlTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.run_dir = Path(self.temp.name) / 'run'
        self.run_dir.mkdir(mode=0o700)
        (self.run_dir / 'commands').mkdir(mode=0o700)

    def trigger(self, action, **more):
        body = dict(runId=RUN_ID, action=action, scope=SCOPE)
        body.update(more)
        raw = json.dumps(body, separators=(',', ':'), sort_keys=True).encode() + b'\n'
        path = self.run_dir / 'commands' / (action + '.json')
        path.write_bytes(raw)
        os.chmod(str(path), 0o600)
        return hashlib.sha256(raw).hexdigest()

    def test_probe_is_claimed_once_and_writes_private_receipt(self):
        digest = self.trigger('probe')
        control = WorkerControl(self.run_dir, RUN_ID, 'probe', digest)

        result = control.run_once()

        self.assertEqual(result['action'], 'probe')
        self.assertEqual(result['status'], 'ACKNOWLEDGED')
        self.assertFalse((self.run_dir / 'commands' / 'probe.json').exists())
        self.assertTrue((self.run_dir / 'commands' / 'probe.claimed').exists())
        receipt = self.run_dir / 'control' / 'probe.status.json'
        self.assertEqual(json.loads(receipt.read_text())['status'], 'ACKNOWLEDGED')
        self.assertEqual(receipt.stat().st_mode & 0o777, 0o600)
        self.assertIsNone(control.run_once())

    def test_changed_trigger_digest_is_rejected_without_action(self):
        self.trigger('probe')
        result = WorkerControl(self.run_dir, RUN_ID, 'probe', '0' * 64).run_once()
        self.assertEqual(result['status'], 'REJECTED')
        self.assertEqual(result['stage'], 'trigger')
        self.assertEqual(json.loads((self.run_dir / 'control' / 'probe.status.json').read_text())['status'], 'REJECTED')

    def test_exact_digest_does_not_allow_wrong_scope(self):
        digest = self.trigger('probe', scope=dict(SCOPE, group='another-group'))
        result = WorkerControl(self.run_dir, RUN_ID, 'probe', digest).run_once()
        self.assertEqual(result['status'], 'REJECTED')
        self.assertEqual(result['stage'], 'trigger')

    def test_symlink_trigger_is_rejected(self):
        outside = Path(self.temp.name) / 'outside.json'
        outside.write_text('{}')
        (self.run_dir / 'commands' / 'probe.json').symlink_to(outside)
        result = WorkerControl(self.run_dir, RUN_ID, 'probe', hashlib.sha256(b'{}').hexdigest()).run_once()
        self.assertEqual(result['status'], 'REJECTED')
        self.assertEqual(outside.read_text(), '{}')

    def test_nonprivate_trigger_is_rejected(self):
        digest = self.trigger('probe')
        os.chmod(str(self.run_dir / 'commands' / 'probe.json'), 0o666)
        result = WorkerControl(self.run_dir, RUN_ID, 'probe', digest).run_once()
        self.assertEqual(result, {'action': 'probe', 'status': 'REJECTED', 'stage': 'trigger'})

    def test_workbench_upload_mode_is_accepted_inside_private_directory(self):
        digest = self.trigger('probe')
        os.chmod(str(self.run_dir / 'commands' / 'probe.json'), 0o644)
        result = WorkerControl(self.run_dir, RUN_ID, 'probe', digest).run_once()
        self.assertEqual(result, {'action': 'probe', 'status': 'ACKNOWLEDGED'})

    def test_reuploaded_trigger_cannot_repeat_an_action(self):
        digest = self.trigger('probe')
        WorkerControl(self.run_dir, RUN_ID, 'probe', digest).run_once()
        self.trigger('probe')
        with self.assertRaises(ValueError):
            WorkerControl(self.run_dir, RUN_ID, 'probe', digest).run_once()
        self.assertEqual(json.loads((self.run_dir / 'control' / 'probe.status.json').read_text())['status'], 'ACKNOWLEDGED')

    def credential(self):
        path = self.run_dir / 'kubeconfig'
        path.write_text('test-only')
        os.chmod(str(path), 0o600)
        return path

    def test_enable_saves_set_receipt_then_rolls_out_and_verifies(self):
        digest = self.trigger('enable')
        credential = self.credential()
        calls = []

        def run(argv, timeout, env):
            calls.append((argv, timeout, env['KUBECONFIG']))
            if argv[0] == str(self.run_dir / 'gateway-live-gate') and 'rollout' not in argv and 'verify' not in argv:
                return b'{"resourceVersion":"801","changedAfter":"2026-10-19T03:00:00Z","replacementEnabled":true}\n'
            if 'rollout' in argv:
                return b'rollout=requested\n'
            if argv[0] == 'kubectl':
                return b'deployment successfully rolled out\n'
            return b'{"replacementEnabled":true,"generation":41,"ready":true,"podUID":"uid-new"}\n'

        result = WorkerControl(self.run_dir, RUN_ID, 'enable', digest, kubeconfig=credential, run_command=run).run_once()
        self.assertEqual(result['status'], 'VERIFIED')
        self.assertEqual(result['resourceVersion'], '801')
        self.assertEqual(result['podUID'], 'uid-new')
        self.assertEqual(json.loads((self.run_dir / 'control' / 'enable.set.json').read_text())['resourceVersion'], '801')
        self.assertEqual([argv[0] for argv, _, _ in calls], [str(self.run_dir / 'gateway-live-gate')] * 2 + ['kubectl', str(self.run_dir / 'gateway-live-gate')])
        self.assertIn('--expected', calls[0][0])
        self.assertIn('false', calls[0][0])
        self.assertIn('true', calls[0][0])
        self.assertTrue(all(path == str(credential) for _, _, path in calls))

    def test_rollout_failure_keeps_set_receipt_and_does_not_retry(self):
        digest = self.trigger('enable')
        credential = self.credential()
        calls = []

        def run(argv, timeout, env):
            calls.append(argv)
            if len(calls) == 1:
                return b'{"resourceVersion":"802","changedAfter":"2026-10-19T03:00:00Z","replacementEnabled":true}\n'
            raise RuntimeError('private subprocess error must not leak')

        result = WorkerControl(self.run_dir, RUN_ID, 'enable', digest, kubeconfig=credential, run_command=run).run_once()
        self.assertEqual(result, {'action': 'enable', 'status': 'INCONCLUSIVE', 'stage': 'rollout'})
        self.assertEqual(json.loads((self.run_dir / 'control' / 'enable.set.json').read_text())['resourceVersion'], '802')
        self.assertEqual(len(calls), 2)
        self.assertNotIn('private', (self.run_dir / 'control' / 'enable.status.json').read_text())

    def test_finish_uses_fixed_boundary_and_reports_helper_acknowledgement(self):
        digest = self.trigger('finish')
        credential = self.credential()
        calls = []

        def run(argv, timeout, env):
            calls.append(argv)
            return b'{"requestId":"boundary-1","status":"ACKNOWLEDGED","clients":{"uid-a/process-a":{"status":"acknowledged"},"uid-b/process-b":{"status":"acknowledged"}}}\n'

        result = WorkerControl(self.run_dir, RUN_ID, 'finish', digest, kubeconfig=credential, run_command=run).run_once()
        self.assertEqual(result['status'], 'ACKNOWLEDGED')
        self.assertEqual(result['boundaryRequestId'], 'boundary-1')
        self.assertEqual(calls, [['python3', str(self.run_dir / 'finish_clients.py'), '--boundary-dir', str(self.run_dir / 'full-boundary'), '--expected-replicas', '2']])

    def test_disable_requires_true_to_false_and_verified_new_pod(self):
        digest = self.trigger('disable')
        calls = []

        def run(argv, timeout, env):
            calls.append(argv)
            if len(calls) == 1:
                return b'{"resourceVersion":"803","changedAfter":"2026-10-19T03:00:00Z","replacementEnabled":false}\n'
            if len(calls) == 2:
                return b'rollout=requested\n'
            if len(calls) == 3:
                return b'deployment successfully rolled out\n'
            return b'{"replacementEnabled":false,"generation":43,"ready":true,"podUID":"uid-restored"}\n'

        result = WorkerControl(self.run_dir, RUN_ID, 'disable', digest,
                               kubeconfig=self.credential(), run_command=run).run_once()
        self.assertEqual(result['status'], 'VERIFIED')
        self.assertEqual(result['replacementEnabled'], False)
        self.assertEqual(calls[0][-4:], ['--expected', 'true', '--set', 'false'])

    def test_finish_uncertain_response_never_retries_or_leaks_details(self):
        digest = self.trigger('finish')
        calls = []

        def run(argv, timeout, env):
            calls.append(argv)
            return b'{"requestId":"boundary-1","status":"INCONCLUSIVE","clients":{"a":{"status":"unknown"}}}\n'

        result = WorkerControl(self.run_dir, RUN_ID, 'finish', digest,
                               kubeconfig=self.credential(), run_command=run).run_once()
        self.assertEqual(result, {'action': 'finish', 'status': 'INCONCLUSIVE', 'stage': 'finish'})
        self.assertEqual(len(calls), 1)

    def test_no_credential_means_no_gate_call(self):
        digest = self.trigger('enable')
        calls = []
        result = WorkerControl(self.run_dir, RUN_ID, 'enable', digest,
                               run_command=lambda *args: calls.append(args)).run_once()
        self.assertEqual(result, {'action': 'enable', 'status': 'INCONCLUSIVE', 'stage': 'set'})
        self.assertEqual(calls, [])

    def test_concurrent_control_is_refused_before_any_mutation(self):
        digest = self.trigger('enable')
        control_dir = self.run_dir / 'control'
        control_dir.mkdir(mode=0o700)
        lock = control_dir / 'action.lock'
        fd = os.open(str(lock), os.O_CREAT | os.O_RDWR, 0o600)
        self.addCleanup(os.close, fd)
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        calls = []

        result = WorkerControl(self.run_dir, RUN_ID, 'enable', digest,
                               kubeconfig=self.credential(),
                               run_command=lambda *args: calls.append(args)).run_once()

        self.assertEqual(result, {'action': 'enable', 'status': 'INCONCLUSIVE', 'stage': 'lock'})
        self.assertEqual(calls, [])

    def test_watcher_outlives_maximum_capture_but_remains_bounded(self):
        argv = ['worker_control.py', '--run-dir', str(self.run_dir), '--run-id', RUN_ID,
                '--action', 'probe', '--trigger-sha256', '0' * 64,
                '--max-seconds', '7200']
        with mock.patch('sys.argv', argv), mock.patch('worker_control.WorkerControl') as control:
            control.return_value.run_once.return_value = {'status': 'ACKNOWLEDGED'}
            self.assertEqual(main(), 0)
        argv[-1] = '7201'
        with mock.patch('sys.argv', argv), self.assertRaises(SystemExit):
            main()

    def test_waiting_watcher_publishes_private_heartbeat(self):
        argv = ['worker_control.py', '--run-dir', str(self.run_dir), '--run-id', RUN_ID,
                '--action', 'probe', '--trigger-sha256', '0' * 64,
                '--max-seconds', '30']
        with mock.patch('sys.argv', argv), mock.patch('worker_control.WorkerControl') as control:
            control.return_value.run_once.side_effect = [None, {'status': 'ACKNOWLEDGED'}]
            with mock.patch('worker_control.time.sleep'):
                self.assertEqual(main(), 0)
        heartbeat = self.run_dir / 'control' / 'probe.heartbeat.json'
        self.assertEqual(heartbeat.stat().st_mode & 0o777, 0o600)
        body = json.loads(heartbeat.read_text())
        self.assertEqual(body['runId'], RUN_ID)
        self.assertEqual(body['action'], 'probe')
        self.assertIsInstance(body['observedAt'], (int, float))


if __name__ == '__main__':
    unittest.main()
