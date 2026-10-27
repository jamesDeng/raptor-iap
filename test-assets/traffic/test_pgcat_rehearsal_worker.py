"""Offline one-use detached worker launch and marker tests."""
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest
import uuid

from pgcat_lane_actions import ActionCatalog
from pgcat_lane_protocol import Refused


SOURCES = {'rehearsalWorker': 'pgcat_rehearsal_worker.py',
           'rehearsalContract': 'pgcat_rehearsal_contract.py',
           'pendingProofWorker': 'pgcat_pending_proof_worker.py',
           'captureWindow': 'capture_window.py',
           'finishClients': 'finish_clients.py',
           'workerCapture': 'worker_capture.py',
           'segmentedCapture': 'segmented_capture.py'}


class DetachedWorkerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(os.path.realpath(self.tmp.name))
        self.root.chmod(0o700)
        self.run = self.root / 'run'
        self.contract = self.root / 'contract'
        self.run.mkdir(mode=0o700)
        self.contract.mkdir(mode=0o700)
        self.kubeconfig = self.root / 'kubeconfig'
        self.kubeconfig.write_text('fake private credential')
        self.kubeconfig.chmod(0o600)
        self.run_id = str(uuid.uuid4())
        self.feed_session = str(uuid.uuid4())
        self.target = {'accountId': '1360282071200743', 'regionId': 'ap-southeast-1',
                       'workerId': 'i-t4nj1bitmcz7cuoch82h',
                       'scalingGroupId': 'asg-t4necb4dfmu6o3z2rn0g',
                       'nlbServerGroupId': 'sgp-xs5s42tihtl6ja2a67',
                       'originalInstanceIds': ['original-a', 'original-b']}
        here = Path(__file__).resolve().parent
        self.worker = self.run / 'pgcat_rehearsal_worker.py'
        source = here / self.worker.name
        if source.exists():
            shutil.copyfile(str(source), str(self.worker))
        else:
            self.worker.write_text('')
        self.worker.chmod(0o600)
        for name in set(SOURCES.values()) - {self.worker.name, 'pgcat_rehearsal_contract.py'}:
            file = self.run / name
            file.write_text('pass\n')
            file.chmod(0o600)
        fake_contract = self.run / 'pgcat_rehearsal_contract.py'
        pre_scale_value = {'providerRunContextSha256': 'a' * 64,
            'targetSha256': hashlib.sha256(json.dumps(self.target, sort_keys=True,
                separators=(',', ':')).encode('ascii')).hexdigest(),
            'firstAt': 1.0, 'latestAt': 2.0, 'sequence': 7,
            'feedSession': self.feed_session,
            'firstFrameSha256': 'b' * 64, 'latestFrameSha256': 'c' * 64}
        fake_contract.write_text('import hashlib,json,time\nfrom pathlib import Path\n'
            'def done(contract,stage,value=None):\n'
            ' time.sleep(.1)\n'
            ' p=Path(contract)/(stage+".json")\n'
            ' raw=(json.dumps(value if value is not None else {},sort_keys=True)+"\\n").encode("ascii")\n'
            ' p.write_bytes(raw);p.chmod(0o600)\n'
            ' s=Path(str(p)+".sha256")\n'
            ' s.write_text(json.dumps({"sha256":hashlib.sha256(raw).hexdigest()}));s.chmod(0o600)\n'
            'def baseline(contract,*args): done(contract,"baseline")\n'
            'def pre_scale(contract,*args): done(contract,"pre-scale",'+repr(pre_scale_value)+')\n'
            'def pending_ready(contract,*args): done(contract,"pending-ready")\n')
        fake_contract.chmod(0o600)
        self.hashes = {key: hashlib.sha256((self.run / name).read_bytes()).hexdigest()
                       for key, name in SOURCES.items()}

    def test_pre_scale_real_worker_output_is_accepted_by_action_parser(self):
        arguments = [sys.executable, str(self.worker), 'start', 'pre-scale',
            str(self.contract), self.run_id, str(self.kubeconfig),
            json.dumps(self.hashes, sort_keys=True, separators=(',', ':')),
            json.dumps(self.target, sort_keys=True, separators=(',', ':'))]
        started = subprocess.run(arguments, cwd=str(self.run),
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
        self.assertEqual(started.returncode, 0)
        self.assertEqual(json.loads(started.stdout),
                         {'status': 'STARTED', 'phase': 'pre-scale'})
        arguments[2] = 'status'
        deadline = time.monotonic() + 3
        while True:
            completed = subprocess.run(arguments, cwd=str(self.run),
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
            if completed.returncode == 0 and b'PREPARED' in completed.stdout:
                break
            self.assertLess(time.monotonic(), deadline)
            time.sleep(.02)
        parsed = ActionCatalog.parse('pre-scale-status', completed.stdout,
            {'feedSession': self.feed_session},
            {'feedSession': self.feed_session, 'sequence': 8})
        self.assertEqual(parsed, {'status': 'PREPARED', 'phase': 'pre-scale',
                                  'feedSession': self.feed_session, 'sequence': 7})
        self.assertNotIn(b'10.0.', completed.stdout)

    def test_pending_start_returns_quickly_then_only_exact_markers_prepare(self):
        import pgcat_rehearsal_worker as worker
        children = []
        def retain(*args, **kwargs):
            child = subprocess.Popen(*args, **kwargs)
            children.append(child)
            return child
        started = time.monotonic()
        self.assertEqual(worker.start(self.contract, self.run_id, 'pending-ready',
            self.kubeconfig, self.hashes, self.target, worker_path=self.worker, spawn=retain), 'STARTED')
        self.assertLess(time.monotonic() - started, 5)
        until = time.monotonic() + 3
        state = 'RUNNING'
        while time.monotonic() < until:
            state = worker.status(self.contract, self.run_id, 'pending-ready', self.hashes, self.target)
            if state == 'PREPARED':
                break
            time.sleep(.02)
        self.assertEqual(state, 'PREPARED')
        self.assertEqual(children[0].wait(timeout=3), 0)
        wrong = dict(self.target, originalInstanceIds=['other-a', 'other-b'])
        self.assertEqual(worker.status(self.contract, self.run_id, 'pending-ready',
                                      self.hashes, wrong), 'INCONCLUSIVE')
        with self.assertRaises(ValueError):
            worker.start(self.contract, self.run_id, 'pending-ready',
                self.kubeconfig, self.hashes, self.target, worker_path=self.worker)

    def test_unknown_spawn_outcome_consumes_one_use_intent(self):
        import pgcat_rehearsal_worker as worker
        def uncertain(*args, **kwargs):
            raise OSError('unknown kernel result')
        with self.assertRaises(ValueError):
            worker.start(self.contract, self.run_id, 'pending-ready',
                self.kubeconfig, self.hashes, self.target, worker_path=self.worker, spawn=uncertain)
        self.assertEqual(worker.status(self.contract, self.run_id, 'pending-ready',
                                      self.hashes, self.target, now=lambda: time.time() + 90), 'INCONCLUSIVE')
        with self.assertRaises(ValueError):
            worker.start(self.contract, self.run_id, 'pending-ready',
                self.kubeconfig, self.hashes, self.target, worker_path=self.worker)

    def test_status_rejects_orphan_or_tampered_completion(self):
        import pgcat_rehearsal_worker as worker
        def uncertain(*args, **kwargs):
            raise OSError('unknown kernel result')
        with self.assertRaises(ValueError):
            worker.start(self.contract, self.run_id, 'baseline',
                self.kubeconfig, self.hashes, self.target, worker_path=self.worker, spawn=uncertain)
        self.assertEqual(worker.status(self.contract, self.run_id, 'baseline', self.hashes, self.target),
                         'RUNNING')
        jobs = self.run / 'phase-jobs'
        intent = json.loads((jobs / 'baseline.intent.json').read_text())
        result = jobs / 'baseline.result.json'
        result.write_text(json.dumps({'version': 1, 'runId': self.run_id,
            'stage': 'baseline', 'nonce': str(uuid.uuid4()), 'status': 'PREPARED'}))
        result.chmod(0o600)
        exit_file = jobs / 'baseline.exit.json'
        exit_file.write_text(json.dumps({'version': 1, 'nonce': intent['nonce'], 'code': 0}))
        exit_file.chmod(0o600)
        self.assertEqual(worker.status(self.contract, self.run_id, 'baseline', self.hashes, self.target),
                         'INCONCLUSIVE')
        result.write_text(json.dumps({'version': 1, 'runId': self.run_id,
            'stage': 'baseline', 'nonce': intent['nonce'], 'status': 'PREPARED'}))
        claim = jobs / 'baseline.claim.json'
        claim.write_text(json.dumps({'version': 1, 'nonce': intent['nonce']}))
        claim.chmod(0o600)
        self.assertEqual(worker.status(self.contract, self.run_id, 'baseline', self.hashes, self.target),
                         'INCONCLUSIVE')

    def test_result_without_exit_or_sealed_stage_never_prepares(self):
        import pgcat_rehearsal_worker as worker
        with self.assertRaises(ValueError):
            worker.start(self.contract, self.run_id, 'baseline', self.kubeconfig,
                         self.hashes, self.target, worker_path=self.worker,
                         spawn=lambda *args, **kwargs: (_ for _ in ()).throw(OSError()))
        jobs = self.run / 'phase-jobs'
        intent = json.loads((jobs / 'baseline.intent.json').read_text())
        for suffix, value in [('claim', {'version': 1, 'nonce': intent['nonce']}),
                              ('result', {'version': 1, 'runId': self.run_id,
                                          'stage': 'baseline', 'nonce': intent['nonce'],
                                          'status': 'PREPARED'})]:
            path = jobs / ('baseline.' + suffix + '.json')
            path.write_text(json.dumps(value)); path.chmod(0o600)
        stage = self.contract / 'baseline.json'
        raw = b'{}\n'
        stage.write_bytes(raw); stage.chmod(0o600)
        seal = self.contract / 'baseline.json.sha256'
        seal.write_text(json.dumps({'sha256': hashlib.sha256(raw).hexdigest()}))
        seal.chmod(0o600)
        self.assertEqual(worker.status(self.contract, self.run_id, 'baseline', self.hashes, self.target),
                         'INCONCLUSIVE')
        exit_file = jobs / 'baseline.exit.json'
        exit_file.write_text(json.dumps({'version': 1, 'nonce': intent['nonce'], 'code': 0}))
        exit_file.chmod(0o600)
        stage.unlink(); seal.unlink()
        self.assertEqual(worker.status(self.contract, self.run_id, 'baseline', self.hashes, self.target),
                         'INCONCLUSIVE')

    def test_launcher_signal_after_intent_preserves_one_use_identity(self):
        import pgcat_rehearsal_worker as worker
        here = Path(__file__).resolve().parent
        script = ('import json,os,signal,sys\n'
                  'import pgcat_rehearsal_worker as worker\n'
                  'worker.start(sys.argv[1],sys.argv[2],sys.argv[3],sys.argv[4],'
                  'json.loads(sys.argv[5]),json.loads(sys.argv[6]),worker_path=sys.argv[7],'
                  'spawn=lambda *a,**k: os.kill(os.getpid(),int(sys.argv[8])))\n')
        for stage, signum in [('baseline', signal.SIGTERM),
                              ('pre-scale', signal.SIGKILL)]:
            with self.subTest(stage=stage):
                env = dict(os.environ, PYTHONPATH=str(here), PYTHONDONTWRITEBYTECODE='1')
                child = subprocess.run([sys.executable, '-c', script, str(self.contract),
                    self.run_id, stage, str(self.kubeconfig), json.dumps(self.hashes),
                    json.dumps(self.target), str(self.worker), str(int(signum))], cwd=str(self.root), env=env,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
                self.assertEqual(child.returncode, -signum,
                                 child.stdout.decode('utf-8') + child.stderr.decode('utf-8'))
                self.assertTrue((self.run / 'phase-jobs' /
                                 (stage + '.intent.json')).is_file())
                self.assertEqual(worker.status(self.contract, self.run_id, stage,
                    self.hashes, self.target, now=lambda: time.time() + 90), 'INCONCLUSIVE')
                with self.assertRaises(ValueError):
                    worker.start(self.contract, self.run_id, stage,
                        self.kubeconfig, self.hashes, self.target, worker_path=self.worker)

    def _bindings(self):
        cli = self.root / 'fake-cli'
        cli.write_text('#!/bin/sh\nexit 0\n')
        cli.chmod(0o700)
        here = Path(__file__).resolve().parent
        source_names = {'laneWrapper': 'pgcat_workbench_lane.py',
                        'providerFeed': 'provider_feed.py',
                        'laneProtocol': 'pgcat_lane_protocol.py',
                        'laneSupervisor': 'pgcat_lane_supervisor.py',
                        'laneActions': 'pgcat_lane_actions.py'}
        hashes = {key: hashlib.sha256((here / name).read_bytes()).hexdigest()
                  for key, name in source_names.items()}
        hashes.update(self.hashes)
        hashes['preDrainProofWorker'] = hashlib.sha256((here/'pgcat_pre_drain_proof_worker.py').read_bytes()).hexdigest()
        return {'workbenchCli': str(cli), 'aliyunCli': str(cli),
                'laneWrapper': str(here / 'pgcat_workbench_lane.py'),
                'providerFeedPath': str(here / 'provider_feed.py'),
                'receiptPath': str(self.root / 'receipt.json'),
                'laneLockPath': str(self.root / 'lane.lock'),
                'laneIntentDir': str(self.root / 'intents'),
                'phaseWorker': str(self.worker), 'pendingWorker': str(self.worker),
                'pendingProofWorker': str(self.run / 'pgcat_pending_proof_worker.py'),
                'preDrainProofWorker': str(self.run / 'pgcat_pre_drain_proof_worker.py'),
                'remoteRun': str(self.run), 'contractDir': str(self.contract),
                'workerKubeconfig': str(self.kubeconfig),
                'scaleoutIntentPath': str(self.root / 'scaleout.intent'),
                'nlbIntentPath': str(self.root / 'nlb-deregister.intent'),
                'preScaleProofPath': str(self.root / 'pre-scale-proof.json'),
                'sourceHashes': hashes,
                'target': {'accountId': '1360282071200743', 'regionId': 'ap-southeast-1',
                           'workerId': 'i-t4nj1bitmcz7cuoch82h',
                           'scalingGroupId': 'asg-t4necb4dfmu6o3z2rn0g',
                           'nlbServerGroupId': 'sgp-xs5s42tihtl6ja2a67',
                           'originalInstanceIds': ['original-a', 'original-b']}}

    def test_action_catalog_uses_only_fixed_bound_worker_paths_and_hashes(self):
        bindings = self._bindings()
        for action, verb, stage in [('baseline-start', 'start', 'baseline'),
                                    ('pre-scale-status', 'status', 'pre-scale'),
                                    ('pending-start', 'start', 'pending-ready')]:
            with self.assertRaises(Refused):
                ActionCatalog.command(action, bindings)
            argv, _, timeout = ActionCatalog.command(action, bindings, self.run_id)
            command = shlex.split(argv[-1])
            self.assertEqual(command[:6], ['python3', str(self.worker), verb, stage,
                                           str(self.contract), self.run_id])
            self.assertEqual(command[6], str(self.kubeconfig))
            self.assertEqual(json.loads(command[7]), self.hashes)
            self.assertEqual(json.loads(command[8]), self.target)
            self.assertEqual(timeout, 5.0)
        with self.assertRaises(Refused):
            ActionCatalog.command('zero-gate-start', bindings, self.run_id)
        with self.assertRaises(Refused):
            ActionCatalog.command('reconcile-close', bindings, self.run_id)
        bindings['target']['originalInstanceIds'].reverse()
        with self.assertRaises(Refused):
            ActionCatalog.command('baseline-start', bindings, self.run_id)


if __name__ == '__main__':
    unittest.main()
