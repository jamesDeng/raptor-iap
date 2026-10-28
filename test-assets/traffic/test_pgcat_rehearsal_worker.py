"""Offline one-use detached worker launch and marker tests."""
import hashlib
import importlib.util
import json
import os
import py_compile
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
           'preDrainProofWorker': 'pgcat_pre_drain_proof_worker.py',
           'zeroProofWorker': 'pgcat_zero_proof_worker.py',
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
                        'laneActions': 'pgcat_lane_actions.py',
                        'sourceBootstrap':'pgcat_source_bootstrap.py','controller':'pgcat_rehearsal_controller.py',
                        'client':'pgcat_lane_client.py','runner':'pgcat_lane_runner.py',
                        'collectorControl': 'pgcat_collector_control.py',
                        'laneTimingAssessor': 'pgcat_lane_timing_assessor.py'}
        hashes = {key: hashlib.sha256((here / name).read_bytes()).hexdigest()
                  for key, name in source_names.items()}
        hashes.update(self.hashes)
        return {'workbenchCli': str(cli), 'aliyunCli': str(cli),
                'laneWrapper': str(here / 'pgcat_workbench_lane.py'),
                'providerFeedPath': str(here / 'provider_feed.py'),
                'receiptPath': str(self.root / 'receipt.json'),
                'laneLockPath': str(self.root / 'lane.lock'),
                'laneIntentDir': str(self.root / 'intents'),
                'phaseWorker': str(self.worker), 'pendingWorker': str(self.worker),
                'pendingProofWorker': str(self.run / 'pgcat_pending_proof_worker.py'),
                'preDrainProofWorker': str(self.run / 'pgcat_pre_drain_proof_worker.py'),
                'zeroProofWorker': str(self.run / 'pgcat_zero_proof_worker.py'),
                'remoteRun': str(self.run), 'contractDir': str(self.contract),
                'workerKubeconfig': str(self.kubeconfig),
                'scaleoutIntentPath': str(self.root / 'scaleout.intent'),
                'nlbIntentPath': str(self.root / 'nlb-deregister.intent'),
                'scaleinIntentPath': str(self.root / 'scalein.intent'),
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
            raw = shlex.split(argv[-1])
            self.assertEqual(raw[:4],['python3','-I','-S','-c'])
            self.assertEqual(raw[6],self.hashes['rehearsalWorker'])
            command=[raw[0],raw[5]]+raw[7:]
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

    def test_zero_proof_uses_exact_bound_worker_and_pre_drain_identity(self):
        bindings = self._bindings()
        bindings['zeroProofWorker'] = str(self.run / 'pgcat_zero_proof_worker.py')
        bindings['scaleinIntentPath'] = str(self.root / 'scalein.intent')
        bindings['sourceHashes']['zeroProofWorker'] = self.hashes['zeroProofWorker']
        pre = {'runId': self.run_id, 'feedSession': self.feed_session,
               'newId': 'candidate', 'originalInstanceIds': self.target['originalInstanceIds']}
        argv, _, timeout = ActionCatalog.command('zero-proof', bindings, self.run_id, pre)
        raw = shlex.split(argv[-1])
        self.assertEqual(raw[:4],['python3','-I','-S','-c'])
        command=[raw[0],raw[5]]+raw[7:]
        self.assertEqual(command[:5], ['python3', str(self.worker), 'zero-proof',
                                       str(self.contract), self.run_id])
        self.assertEqual(command[5], str(self.kubeconfig))
        self.assertEqual(json.loads(command[6]), self.hashes)
        self.assertEqual(json.loads(command[7]), self.target)
        self.assertEqual(json.loads(command[8]), pre)
        self.assertEqual(timeout, 5.0)
        sealed = dict(pre, sealedAt=time.time())
        argv, _, _ = ActionCatalog.command('zero-proof', bindings, self.run_id, sealed)
        self.assertEqual(json.loads(shlex.split(argv[-1])[13]), pre)

    def test_zero_worker_executes_only_after_transitive_source_hash_check(self):
        marker=self.root/'zero-executed'
        zero=self.run/'pgcat_zero_proof_worker.py'
        zero.write_text('from pathlib import Path\n'
            'def produce(contract,run,binding):\n'
            ' Path('+repr(str(marker))+').write_text("yes")\n'
            ' return {"runId":run,"newId":binding["newId"]}\n')
        zero.chmod(0o600)
        self.hashes['zeroProofWorker']=hashlib.sha256(zero.read_bytes()).hexdigest()
        pre={'runId':self.run_id,'feedSession':self.feed_session,'newId':'candidate',
             'originalInstanceIds':self.target['originalInstanceIds']}
        args=[sys.executable,str(self.worker),'zero-proof',str(self.contract),self.run_id,
              str(self.kubeconfig),json.dumps(self.hashes),json.dumps(self.target),json.dumps(pre)]
        valid=subprocess.run(args,cwd=str(self.run),stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE,timeout=5)
        self.assertEqual(valid.returncode,0,valid.stdout.decode())
        self.assertEqual(json.loads(valid.stdout),{'runId':self.run_id,'newId':'candidate'})
        self.assertTrue(marker.exists());marker.unlink()
        zero.write_text(zero.read_text()+'# changed\n')
        rejected=subprocess.run(args,cwd=str(self.run),stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE,timeout=5)
        self.assertNotEqual(rejected.returncode,0)
        self.assertFalse(marker.exists())

    def _stale_cache(self, path, old, fresh):
        self.assertEqual(len(old.encode()), len(fresh.encode()))
        stamp = int(time.time()) - 100
        path.write_text(old); path.chmod(0o600)
        os.utime(path, (stamp, stamp))
        cache_root=self.root/'pycache'
        previous=sys.pycache_prefix
        try:
            sys.pycache_prefix=str(cache_root)
            py_compile.compile(str(path),
                cfile=importlib.util.cache_from_source(str(path)),doraise=True)
        finally:
            sys.pycache_prefix=previous
        path.write_text(fresh); path.chmod(0o600)
        os.utime(path, (stamp, stamp))

    def _zero_cli(self):
        pre={'runId':self.run_id,'feedSession':self.feed_session,'newId':'candidate',
             'originalInstanceIds':self.target['originalInstanceIds']}
        return [sys.executable,str(self.worker),'zero-proof',str(self.contract),self.run_id,
                str(self.kubeconfig),json.dumps(self.hashes),json.dumps(self.target),json.dumps(pre)]

    def test_zero_worker_ignores_matching_timestamp_size_stale_direct_bytecode(self):
        zero=self.run/'pgcat_zero_proof_worker.py'
        old='def produce(contract,run,binding):\n return {"observed":"OLD"}\n'
        fresh='def produce(contract,run,binding):\n return {"observed":"NEW"}\n'
        self._stale_cache(zero,old,fresh)
        self.hashes['zeroProofWorker']=hashlib.sha256(zero.read_bytes()).hexdigest()
        result=subprocess.run(self._zero_cli(),cwd=str(self.run),stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE,timeout=5,
                              env=dict(os.environ,PYTHONPYCACHEPREFIX=str(self.root/'pycache')))
        self.assertEqual(result.returncode,0,result.stdout.decode())
        self.assertEqual(json.loads(result.stdout),{'observed':'NEW'})

    def test_zero_worker_ignores_matching_timestamp_size_stale_transitive_bytecode(self):
        transitive=self.run/'capture_window.py'
        self._stale_cache(transitive,'MARKER="OLD"\n','MARKER="NEW"\n')
        self.hashes['captureWindow']=hashlib.sha256(transitive.read_bytes()).hexdigest()
        zero=self.run/'pgcat_zero_proof_worker.py'
        zero.write_text('import capture_window\n'
                        'def produce(contract,run,binding):\n'
                        ' return {"observed":capture_window.MARKER}\n')
        zero.chmod(0o600)
        self.hashes['zeroProofWorker']=hashlib.sha256(zero.read_bytes()).hexdigest()
        result=subprocess.run(self._zero_cli(),cwd=str(self.run),stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE,timeout=5,
                              env=dict(os.environ,PYTHONPYCACHEPREFIX=str(self.root/'pycache')))
        self.assertEqual(result.returncode,0,result.stdout.decode())
        self.assertEqual(json.loads(result.stdout),{'observed':'NEW'})

    def test_zero_worker_verified_modules_have_exact_source_file_locations(self):
        pre=self.run/'pgcat_pre_drain_proof_worker.py'
        pre.write_text('def location():\n return __file__\n'); pre.chmod(0o600)
        self.hashes['preDrainProofWorker']=hashlib.sha256(pre.read_bytes()).hexdigest()
        zero=self.run/'pgcat_zero_proof_worker.py'
        zero.write_text('import pgcat_pre_drain_proof_worker as pre\n'
                        'def produce(contract,run,binding):\n'
                        ' return {"direct":__file__,"transitive":pre.location()}\n')
        zero.chmod(0o600)
        self.hashes['zeroProofWorker']=hashlib.sha256(zero.read_bytes()).hexdigest()
        result=subprocess.run(self._zero_cli(),cwd=str(self.run),stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE,timeout=5)
        self.assertEqual(result.returncode,0,result.stdout.decode())
        self.assertEqual(json.loads(result.stdout),
                         {'direct':str(zero),'transitive':str(pre)})

    def test_activity_and_scalein_use_only_fixed_ess_arguments(self):
        bindings = self._bindings()
        bindings['zeroProofWorker'] = str(self.run / 'pgcat_zero_proof_worker.py')
        bindings['scaleinIntentPath'] = str(self.root / 'scalein.intent')
        bindings['sourceHashes']['zeroProofWorker'] = self.hashes['zeroProofWorker']
        activity, _, budget = ActionCatalog.command('scaling-activity-check', bindings, self.run_id)
        self.assertEqual(activity[1:6], ['ess', 'DescribeScalingActivities', '--ScalingGroupId',
                                        self.target['scalingGroupId'], '--StatusCode'])
        self.assertEqual(activity[6:11], ['InProgress', '--PageNumber', '1', '--PageSize', '50'])
        self.assertEqual(budget, 5.0)
        scale, _, budget = ActionCatalog.command('scalein-three-to-two', bindings, self.run_id)
        self.assertEqual(scale[1:6], ['ess', 'ModifyScalingGroup', '--ScalingGroupId',
                                     self.target['scalingGroupId'], '--DesiredCapacity'])
        self.assertEqual(scale[6], '2')
        self.assertEqual(budget, 20.0)

    def test_activity_parser_requires_clear_exact_page(self):
        request = {'target': self.target, 'requestId': self.run_id}
        clean = b'{"PageNumber":1,"TotalCount":0,"ScalingActivities":{"ScalingActivity":[]}}'
        self.assertEqual(ActionCatalog.parse('scaling-activity-check', clean, request),
                         {'status':'CLEAR','requestId':self.run_id})
        for raw in (b'{"PageNumber":1,"TotalCount":1,"ScalingActivities":{"ScalingActivity":[]}}',
                    b'{"PageNumber":2,"TotalCount":0,"ScalingActivities":{"ScalingActivity":[]}}',
                    b'{"PageNumber":1,"TotalCount":0,"ScalingActivities":{"ScalingActivity":[{}]}}'):
            with self.assertRaises(Refused):
                ActionCatalog.parse('scaling-activity-check', raw, request)

    def test_scalein_ack_requires_single_request_id(self):
        request = {'target': self.target}
        self.assertEqual(ActionCatalog.parse('scalein-three-to-two', b'{"RequestId":"req-ok"}', request),
                         {'requestId':'req-ok'})
        with self.assertRaises(Refused):
            ActionCatalog.parse('scalein-three-to-two', b'{"RequestId":"req-ok","extra":1}', request)

    def test_zero_proof_parser_binds_candidate_session_and_reviewed_source(self):
        pre = dict(runId=self.run_id, feedSession=self.feed_session, newId='candidate',
                   originalInstanceIds=self.target['originalInstanceIds'],
                   clientRosterSha256='b'*64)
        value = dict(runId=self.run_id, feedSession=self.feed_session, newId='candidate',
            originalInstanceIds=self.target['originalInstanceIds'],
            preDrainBindingSha256=hashlib.sha256(json.dumps(pre, sort_keys=True,
                separators=(',',':')).encode()).hexdigest(),
            sequence=7, observedAt=time.time(), clientRosterSha256='b'*64,
            evidenceSha256='c'*64, sourceSha256='d'*64)
        request = dict(runId=self.run_id, feedSession=self.feed_session,
                       target=self.target, minimumSequence=7)
        receipt = dict(feedSession=self.feed_session, sequence=7, observedAt=value['observedAt'])
        self.assertEqual(ActionCatalog.parse('zero-proof', json.dumps(value).encode(),
                         request, receipt, source_sha='d'*64, pending_binding=pre), value)
        with self.assertRaises(Refused):
            ActionCatalog.parse('zero-proof', json.dumps(dict(value,newId='other')).encode(),
                                request, receipt, source_sha='d'*64, pending_binding=pre)
        with self.assertRaises(Refused):
            ActionCatalog.parse('zero-proof', json.dumps(value).encode(), request,
                                receipt, source_sha='e'*64, pending_binding=pre)




class BaselineSnapshotTests(unittest.TestCase):
    setUp = DetachedWorkerTests.setUp
    def test_actual_start_child_baseline_executes_direct_and_transitive_reviewed_bytes(self):
        from test_pgcat_source_bootstrap import stale_cache
        import pgcat_rehearsal_worker as worker
        contract=self.run/'pgcat_rehearsal_contract.py'
        source=contract.read_text().replace('def baseline(contract,*args): done(contract,"baseline")',
            'def baseline(contract,*args):\n import capture_window\n done(contract,"baseline",{"direct":"NEW","transitive":capture_window.PROBE})')
        stale_cache(contract,source.replace('"direct":"NEW"','"direct":"OLD"'),source)
        stale_cache(self.run/'capture_window.py','PROBE="OLD"\n','PROBE="NEW"\n')
        self.hashes={k:hashlib.sha256((self.run/name).read_bytes()).hexdigest() for k,name in SOURCES.items()}
        launched=[]
        worker.start(self.contract,self.run_id,'baseline',str(self.kubeconfig),self.hashes,self.target,
                     worker_path=self.worker,spawn=lambda args,**kw:launched.append((args,kw)))
        args,kw=launched[0];kw.update(stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        # Explicit private cache prefix also exercises Apple's site default safely.
        args[1:1]=['-X','pycache_prefix='+str(self.run/'fixture-pycache')]
        child=subprocess.run(args,timeout=5,**kw)
        self.assertEqual(child.returncode,0,child.stderr)
        value=json.loads((self.contract/'baseline.json').read_text())
        self.assertEqual(value,dict(direct='NEW',transitive='NEW'))

    def test_pending_and_pre_drain_dispatch_execute_reviewed_direct_and_transitive_bytes(self):
        from test_pgcat_source_bootstrap import stale_cache
        from pgcat_rehearsal_worker import BOOTSTRAP
        for verb,name in [('pending-proof','pgcat_pending_proof_worker.py'),('pre-drain-proof','pgcat_pre_drain_proof_worker.py')]:
            with self.subTest(verb=verb):
                source='import capture_window\ndef produce(*args):return {"direct":"NEW","transitive":capture_window.PROBE}\n'
                stale_cache(self.run/name,source.replace('"direct":"NEW"','"direct":"OLD"'),source)
                stale_cache(self.run/'capture_window.py','PROBE="OLD"\n','PROBE="NEW"\n')
                hashes={k:hashlib.sha256((self.run/n).read_bytes()).hexdigest() for k,n in SOURCES.items()}
                bound=dict(runId=self.run_id,originalInstanceIds=self.target['originalInstanceIds'],newId='candidate')
                argv=[sys.executable,'-I','-S','-c',BOOTSTRAP,str(self.worker),hashes['rehearsalWorker'],verb,
                      str(self.contract),self.run_id,str(self.kubeconfig),json.dumps(hashes),json.dumps(self.target),json.dumps(bound)]
                got=subprocess.run(argv,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
                self.assertEqual(got.returncode,0,got.stderr)
                self.assertEqual(json.loads(got.stdout),dict(direct='NEW',transitive='NEW'))


class ReconcileDispatchTests(unittest.TestCase):
    setUp = DetachedWorkerTests.setUp

    def _dispatch_fixture(self, extra_source=''):
        from pgcat_rehearsal_worker import BOOTSTRAP
        path = self.contract / 'baseline.json'
        raw = json.dumps({'paths': {'kubeconfig': str(self.kubeconfig)}}).encode('ascii')
        path.write_bytes(raw); path.chmod(0o600)
        seal = self.contract / 'baseline.json.sha256'
        seal.write_text(json.dumps({'sha256': hashlib.sha256(raw).hexdigest()})); seal.chmod(0o600)
        source = ('import capture_window\nfrom pathlib import Path\n'
            'def reconcile_close(contract, run_id, target, pending, context):\n'
            ' assert capture_window.PROBE == "NEW"\n'
            ' assert pending == {"sealed": "pending"} and context == {"asserted": "ack"}\n'
            ' Path(contract,"called").write_text("yes")\n' + extra_source +
            ' return '+repr(dict(feedSession=self.feed_session, sequence=20, resourceVersion='12'))+'\n')
        from test_pgcat_source_bootstrap import stale_cache
        stale_cache(self.run / 'pgcat_rehearsal_contract.py', source.replace('== "NEW"', '== "OLD"'), source)
        stale_cache(self.run / 'capture_window.py', 'PROBE="OLD"\n', 'PROBE="NEW"\n')
        self.hashes = {key: hashlib.sha256((self.run / name).read_bytes()).hexdigest()
                       for key, name in SOURCES.items()}
        self.argv = [sys.executable, '-I', '-S', '-c', BOOTSTRAP, str(self.worker),
            self.hashes['rehearsalWorker'], 'reconcile-close', str(self.contract), self.run_id,
            str(self.kubeconfig), json.dumps(self.hashes), json.dumps(self.target),
            json.dumps({'sealed': 'pending'}), json.dumps({'asserted': 'ack'})]

    def _dispatch(self):
        return subprocess.run(self.argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)

    def test_reconcile_fixed_private_verb_executes_verified_bytes_and_two_safe_rows(self):
        self._dispatch_fixture()
        result = self._dispatch()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([json.loads(row) for row in result.stdout.splitlines()], [
            dict(status='PREPARED', stage='reconcile', category='rehearsal-only'),
            dict(feedSession=self.feed_session, sequence=20, resourceVersion='12')])
        self.assertEqual(result.stderr, b'')
        self.assertNotIn(b'private', result.stdout)
        self.assertTrue((self.contract / 'called').exists())

    def test_reconcile_dispatch_rejects_unknown_argument_stale_source_and_kubeconfig_swap(self):
        for case in ('extra', 'missing', 'source', 'kubeconfig'):
            with self.subTest(case=case):
                self.setUp(); self._dispatch_fixture()
                if case == 'extra': self.argv.append('extra')
                elif case == 'missing': self.argv.pop()
                elif case == 'source':
                    with (self.run / 'capture_window.py').open('a') as stream: stream.write('# stale\n')
                else: self.argv[10] = str(self.root / 'other-kubeconfig')
                result = self._dispatch()
                self.assertEqual(result.returncode, 1)
                self.assertEqual(json.loads(result.stdout), dict(status='INCONCLUSIVE', phase='unknown'))
                self.assertEqual(result.stderr, b'')
                self.assertFalse((self.contract / 'called').exists())

    def test_reconcile_dispatch_rechecks_sources_after_function_before_success(self):
        self._dispatch_fixture(' Path(__file__).with_name("capture_window.py").write_text("changed")\n')
        result = self._dispatch()
        self.assertEqual(result.returncode, 1)
        self.assertEqual(json.loads(result.stdout), dict(status='INCONCLUSIVE', phase='unknown'))
        self.assertNotIn(b'PREPARED', result.stdout)
        self.assertEqual(result.stderr, b'')


if __name__ == '__main__':
    unittest.main()
