"""Offline collector lifecycle and exact reviewed-source execution tests."""
import copy
import hashlib
import io
import importlib
import importlib.util
import json
import os
from pathlib import Path
import py_compile
import shlex
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
import uuid
from unittest.mock import patch
from contextlib import redirect_stdout

from pgcat_lane_actions import ActionCatalog
from pgcat_lane_protocol import Refused, validate_result
import test_pgcat_rehearsal_worker as rehearsal_tests

try:
    import pgcat_collector_control as control
except ImportError:
    control = None


IDENTITY = dict(pid=1234, bootId='boot-test', startTicks=456, commandSha256='a'*64)


class CollectorTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(control, 'finite collector control is required')
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(os.path.realpath(self.tmp.name)); self.root.chmod(0o700)
        self.run = self.root/'run'; self.run.mkdir(mode=0o700)
        self.contract = self.root/'contract'; self.contract.mkdir(mode=0o700)
        (self.run/'disabled-boundary').mkdir(mode=0o700)
        self.kubeconfig = self.root/'kubeconfig'
        self.kubeconfig.write_text('fixture'); self.kubeconfig.chmod(0o600)
        self.run_id = str(uuid.uuid4())
        self.worker = self.run/'pgcat_collector_control.py'
        here = Path(__file__).resolve().parent
        for name in control.SOURCES.values():
            shutil.copyfile(str(here/name), str(self.run/name)); (self.run/name).chmod(0o600)
        self.hashes = self.rehash()
        self.calls = []

    def rehash(self):
        return {k:hashlib.sha256((self.run/v).read_bytes()).hexdigest()
                for k,v in control.SOURCES.items()}

    def cleanup_fixture(self,status='PASS',complete=True):
        job=self.run/'collector-job';job.mkdir(mode=0o700)
        verdict=dict(status=status,category='lane-timing')
        if complete:verdict.update(runId=self.run_id,clientIdentitySha256='a'*64)
        fd=control.directory(job)
        try:control.write(fd,'lane-verdict.json',verdict)
        finally:os.close(fd)
        result=dict(status=status,category='lane-timing',
            verdictSha256=hashlib.sha256((job/'lane-verdict.json').read_bytes()).hexdigest())
        return job,result

    def test_authenticated_fail_survives_unlink_failure(self):
        from unittest.mock import patch
        job,result=self.cleanup_fixture('FAIL')
        with patch.object(control.os,'unlink',side_effect=OSError('fixture')):
            response=control.close_verdict(self.contract,self.run_id,str(self.kubeconfig),result)
        self.assertEqual(response['status'],'FAIL')
        self.assertEqual(response['verdictSha256'],result['verdictSha256'])
        self.assertIsNone(response['workerCredentialCleanupSha256'])
        self.assertTrue(self.kubeconfig.exists())
        validate_result(dict(state='complete',category='ok',payload=response))

    def test_authenticated_fail_survives_cleanup_claim_fsync_failure(self):
        from unittest.mock import patch
        job,result=self.cleanup_fixture('FAIL')
        with patch.object(control.os,'fsync',side_effect=OSError('fixture')):
            response=control.close_verdict(self.contract,self.run_id,str(self.kubeconfig),result)
        self.assertEqual(response['status'],'FAIL')
        self.assertEqual(response['verdictSha256'],result['verdictSha256'])
        self.assertIsNone(response['workerCredentialCleanupSha256'])
        self.assertTrue(self.kubeconfig.exists())
        with self.assertRaises((OSError,ValueError)):
            control.close_verdict(self.contract,self.run_id,str(self.kubeconfig),result)

    def test_authenticated_fail_survives_missing_client_identity(self):
        job,result=self.cleanup_fixture('FAIL',complete=False)
        response=control.close_verdict(self.contract,self.run_id,str(self.kubeconfig),result)
        self.assertEqual(response['status'],'FAIL')
        self.assertEqual(response['verdictSha256'],result['verdictSha256'])
        self.assertIsNone(response['clientIdentitySha256'])
        self.assertFalse(self.kubeconfig.exists())
        validate_result(dict(state='complete',category='ok',payload=response))

    def test_worker_cleanup_is_exact_one_use_and_keeps_evidence(self):
        job,result=self.cleanup_fixture('FAIL')
        response=control.close_verdict(self.contract,self.run_id,str(self.kubeconfig),result)
        self.assertEqual(response['status'],'FAIL');self.assertFalse(self.kubeconfig.exists())
        self.assertTrue((job/'lane-verdict.json').exists())
        self.assertEqual(response['workerCredentialCleanupSha256'],
            hashlib.sha256((job/'credential-cleanup.json').read_bytes()).hexdigest())
        with self.assertRaises((ValueError,OSError)):
            control.close_verdict(self.contract,self.run_id,str(self.kubeconfig),result)

    def test_incomplete_worker_verdict_still_removes_bound_credential_but_refuses_pass(self):
        job,result=self.cleanup_fixture('INCONCLUSIVE',complete=False)
        response=control.close_verdict(self.contract,self.run_id,str(self.kubeconfig),result)
        self.assertEqual(response['status'],'INCONCLUSIVE');self.assertIsNone(response['clientIdentitySha256'])
        self.assertFalse(self.kubeconfig.exists());self.assertTrue((job/'credential-cleanup.json').exists())

    def test_worker_cleanup_never_follows_symlink_or_unrelated_path(self):
        job,result=self.cleanup_fixture()
        other=self.root/'other';other.write_text('fixture');other.chmod(0o600)
        with self.assertRaises(ValueError):control.close_verdict(self.contract,self.run_id,str(other),result)
        self.assertTrue(other.exists())
        self.kubeconfig.unlink();self.kubeconfig.symlink_to(other)
        with self.assertRaises(ValueError):control.close_verdict(self.contract,self.run_id,str(self.kubeconfig),result)
        self.assertTrue(self.kubeconfig.is_symlink());self.assertTrue(other.exists())
        self.assertFalse((job/'credential-cleanup.json').exists())

    def test_worker_cleanup_failure_never_emits_cleanup_receipt(self):
        from unittest.mock import patch
        job,result=self.cleanup_fixture()
        with patch.object(control.os,'unlink',side_effect=OSError('fixture')):
            with self.assertRaises(OSError):control.close_verdict(self.contract,self.run_id,str(self.kubeconfig),result)
        self.assertTrue(self.kubeconfig.exists());self.assertFalse((job/'credential-cleanup.json').exists())

    def launch(self, spawn=None, *, profile='no-scale'):
        def fake(*args, **kwargs):
            self.calls.append((args, kwargs))
            return object()
        return control.launch(self.contract, self.run_id, self.kubeconfig,
            self.hashes, worker_path=self.worker, spawn=spawn or fake,
            **({} if profile=='no-scale' else {'profile':profile}))

    def child(self, code=0, timeout=False, completion=True, completed=True, cancelled=False,
              *, profile='no-scale', manifest=None):
        case = self
        class FakeChild:
            pid = 1234
            waits = 0
            kills = 0
            timeouts = None
            def wait(self, timeout=None):
                self.waits += 1
                if self.timeouts is None:self.timeouts=[]
                self.timeouts.append(timeout)
                if timeout is not None:
                    case.assertLessEqual(timeout, 80 if profile=='no-scale' else 5415)
                    if timeout_flag and (timeout_flag != "once" or self.waits == 1):
                        raise subprocess.TimeoutExpired('fixture', timeout)
                return code
            def kill(self):
                self.kills += 1
                case.killed = True
        timeout_flag = timeout
        def spawn(args, **kwargs):
            self.calls.append((args, kwargs))
            tail = (['--output',str(self.run/'collector-capture'),
                '--inventory',str(self.run/'provider-inventory.json'),'--seconds','65']
                if profile=='no-scale' else ['--output',str(self.run/'rehearsal-capture'),
                '--inventory',str(self.run/'provider-inventory.json'),'--seconds','5400',
                '--boundary-dir',str(self.run/'disabled-boundary')])
            self.assertEqual(args[-len(tail):], tail)
            if profile=='no-scale':self.assertNotIn('--boundary-dir', args)
            self.assertEqual(kwargs['env']['KUBECONFIG'], str(self.kubeconfig))
            capture = self.run/('collector-capture' if profile=='no-scale' else 'rehearsal-capture')
            capture.mkdir(mode=0o700)
            if completion:
                path = capture/'completion.json'
                value = dict(version=1, completed=completed, cancelled=cancelled,
                    maxTotalSeconds=65 if profile=='no-scale' else 5400,
                    segments=[{'path':'segment-0001.jsonl'}])
                if profile=='disabled-rehearsal':
                    value.update(maxSegmentSeconds=1800,errors=[],missingPollCount=0,
                        clientFinalMarkers=[dict(uid='pod',processId='process',final=True)])
                if manifest is not None:value.update(manifest)
                path.write_text(json.dumps(value));path.chmod(0o600)
            # Status before the owning wait completes must expose the same identity.
            self.spawned = FakeChild()
            return self.spawned
        return control.run_child(self.contract,self.run_id,self.kubeconfig,self.hashes,
            worker_path=self.worker,spawn=spawn,probe=lambda pid: dict(IDENTITY),
            **({} if profile=='no-scale' else {'profile':profile}))

    def status(self, alive=False, now=None, *, profile='no-scale'):
        return control.status(self.contract,self.run_id,self.hashes,
            probe=lambda pid: dict(IDENTITY) if alive else None, now=now,
            **({} if profile=='no-scale' else {'profile':profile}))

    def observe(self):
        self.assertEqual(self.status(alive=True), {'status':'ALIVE','identity':IDENTITY})

    def test_rehearsal_launch_requires_existing_empty_private_boundary(self):
        for kind in ('missing','nonempty','public','symlink'):
            with self.subTest(kind=kind):
                self.setUp();boundary=self.run/'disabled-boundary'
                if kind=='missing':boundary.rmdir()
                if kind=='nonempty':(boundary/'request.json').write_text('stale')
                if kind=='public':boundary.chmod(0o755)
                if kind=='symlink':
                    boundary.rmdir();target=self.root/'elsewhere';target.mkdir(mode=0o700)
                    boundary.symlink_to(target,target_is_directory=True)
                with self.assertRaises((ValueError,OSError)):self.launch(profile='disabled-rehearsal')
                self.assertFalse(self.calls)
                self.assertFalse((self.run/'collector-job'/'intent.json').exists())
                # The no-scale component has no boundary precondition.
                self.assertEqual(self.launch(),{'status':'LAUNCHED'})

    def test_rehearsal_capture_argv_uses_bound_paths_and5400(self):
        self.assertEqual(control.capture_arguments(self.run,profile='disabled-rehearsal'),
            ['--output',str(self.run/'rehearsal-capture'),'--inventory',
             str(self.run/'provider-inventory.json'),'--seconds','5400',
             '--boundary-dir',str(self.run/'disabled-boundary')])
        self.launch(profile='disabled-rehearsal')
        self.assertEqual(self.child(profile='disabled-rehearsal'),0)
        self.assertEqual(self.calls[-1][0][7],'_rehearsal-capture')

    def test_no_scale_profile_remains65_without_boundary(self):
        self.assertEqual(control.capture_arguments(self.run,profile='no-scale'),
            ['--output',str(self.run/'collector-capture'),'--inventory',
             str(self.run/'provider-inventory.json'),'--seconds','65'])
        self.launch();self.assertEqual(self.child(),0)
        self.assertEqual(self.spawned.timeouts,[80])
        intent=json.loads((self.run/'collector-job'/'intent.json').read_text())
        self.assertEqual(set(intent),{'runId','startedAt'})
        self.assertEqual(self.status(alive=True,now=lambda:intent['startedAt']+90)['status'],'ALIVE')
        self.assertEqual(self.status(alive=True,now=lambda:intent['startedAt']+91),{'status':'INCONCLUSIVE'})

    def test_rehearsal_intent_binds_profile_across_child_and_status(self):
        self.launch(profile='disabled-rehearsal')
        intent=json.loads((self.run/'collector-job'/'intent.json').read_text())
        self.assertEqual(intent,dict(runId=self.run_id,startedAt=intent['startedAt'],
                                    profile='disabled-rehearsal'))
        self.assertEqual(self.calls[0][0][0][7],'_rehearsal-run')
        self.assertEqual(self.child(profile='disabled-rehearsal'),0)
        self.assertEqual(self.status(alive=True,profile='disabled-rehearsal')['status'],'ALIVE')
        self.assertEqual(self.status(profile='disabled-rehearsal')['status'],'EXITED')
        path=self.run/'collector-job'/'intent.json'
        path.write_text(json.dumps(dict(intent,profile='no-scale')))
        self.assertEqual(self.status(alive=True,profile='disabled-rehearsal'),{'status':'INCONCLUSIVE'})
        with self.assertRaises(ValueError):control.completion(self.run,profile='disabled-rehearsal')

    def test_cross_profile_status_and_launch_refused(self):
        for launched,wrong in [('no-scale','disabled-rehearsal'),('disabled-rehearsal','no-scale')]:
            with self.subTest(launched=launched):
                self.setUp();self.launch(profile=launched)
                self.assertEqual(self.child(profile=wrong),1);self.assertEqual(len(self.calls),1)
                with self.assertRaises(ValueError):self.launch(profile=wrong)
                self.assertEqual(self.child(profile=launched),0)
                self.assertEqual(self.status(alive=True,profile=wrong),{'status':'INCONCLUSIVE'})
                self.assertEqual(self.status(alive=True,profile=launched)['status'],'ALIVE')
                with self.assertRaises(ValueError):control.completion(self.run,profile=wrong)
                self.assertEqual(self.status(profile=wrong),{'status':'INCONCLUSIVE'})
        for invalid in ('rehearsal','5400','',None,True,{},[]):
            with self.subTest(invalid=invalid):
                with self.assertRaises(ValueError):control.capture_arguments(self.run,profile=invalid)

    def test_rehearsal_guardian_and_status_use_long_budgets(self):
        self.launch(profile='disabled-rehearsal');self.assertEqual(self.child(profile='disabled-rehearsal'),0)
        self.assertEqual(self.spawned.timeouts,[5415])
        start=json.loads((self.run/'collector-job'/'intent.json').read_text())['startedAt']
        self.assertEqual(self.status(alive=True,now=lambda:start+5430,profile='disabled-rehearsal')['status'],'ALIVE')
        self.assertEqual(self.status(now=lambda:start+5430,profile='disabled-rehearsal')['status'],'EXITED')
        for delta in (-1,5431):
            self.assertEqual(self.status(alive=True,now=lambda:start+delta,profile='disabled-rehearsal'),{'status':'INCONCLUSIVE'})
        self.setUp();self.launch(profile='disabled-rehearsal')
        self.assertEqual(self.child(code=-9,timeout='once',profile='disabled-rehearsal'),1)
        self.assertEqual(self.spawned.timeouts,[5415,5]);self.assertEqual(self.spawned.kills,1)
        self.assertEqual(self.status(alive=True,profile='disabled-rehearsal')['status'],'ALIVE')
        self.assertEqual(self.status(profile='disabled-rehearsal'),{'status':'INCONCLUSIVE'})

    def test_rehearsal_missing_final_or_expired_duration_is_not_completed(self):
        cases=[dict(clientFinalMarkers=[]),dict(clientFinalMarkers=[dict(uid='p',processId='c',final=False)]),
               dict(errors=[{'reason':'client boundary unavailable'}],missingPollCount=1),
               dict(maxTotalSeconds=65),dict(maxSegmentSeconds=1801),dict(maxSegmentSeconds=True),
               dict(segments=[]),dict(segments=[{}]*5407),dict(completed=False),dict(cancelled=True)]
        for value in cases:
            with self.subTest(value=str(value)[:100]):
                self.setUp();self.launch(profile='disabled-rehearsal')
                self.assertEqual(self.child(profile='disabled-rehearsal',manifest=value),1)
                self.assertEqual(self.status(alive=True,profile='disabled-rehearsal')['status'],'ALIVE')
                self.assertEqual(self.status(profile='disabled-rehearsal'),{'status':'INCONCLUSIVE'})

    def run_long_capture(self,kind):
        # Real worker/segmentation, fixed 5400 argv, only remote reads and clock faked.
        import worker_capture
        from test_capture_window import frame
        from finish_clients import atomic_json
        boundary=self.run/'disabled-boundary'
        from test_finish_clients import ROSTER
        roster=ROSTER
        clock=[0]
        def capture(*args):
            t=clock[0];row=frame(t,min(t,2),final=t>=2 and kind!='missing-final')
            pod=row['pods'][0];row['expectedReplicas']=2;row['pods']=[]
            for identity in roster:
                item=copy.deepcopy(pod);item.update({k:v for k,v in identity.items() if k!='processId'})
                item['snapshot']['sample']['processId']=identity['processId']
                for event in item['snapshot']['events']:event['processId']=identity['processId']
                row['pods'].append(item)
            if t==1 and kind!='cap':atomic_json(boundary/'request.json',dict(requestId='one',roster=roster))
            if t==2 and kind!='cap':
                atomic_json(boundary/'receipt.json',dict(requestId='one',status='ACKNOWLEDGED',
                    clients={r['uid']+'/'+r['processId']:{'status':'acknowledged'} for r in roster}))
            return row
        def sleep(_):clock[0]=5400 if kind=='cap' else clock[0]+1
        with patch.object(worker_capture,'capture_frame',side_effect=capture), \
             patch.object(worker_capture.time,'monotonic',side_effect=lambda:clock[0]), \
             patch.object(worker_capture.time,'sleep',side_effect=sleep):
            worker_capture.run_capture(self.run/'rehearsal-capture',self.run/'provider-inventory.json',5400,boundary)
        return clock[0]

    def test_real_rehearsal_boundary_completes_and_cap_or_missing_finals_refuse(self):
        for kind in ('good','missing-final','cap'):
            with self.subTest(kind=kind):
                self.setUp();self.launch(profile='disabled-rehearsal');clock=self.run_long_capture(kind)
                if kind=='good':
                    self.assertEqual(clock,2)
                    self.assertEqual(len(control.completion(self.run,profile='disabled-rehearsal')),64)
                else:
                    with self.assertRaises(ValueError):control.completion(self.run,profile='disabled-rehearsal')

    def test_rehearsal_capture_refuses_missing_original_provenance_and_checks_intent(self):
        self.launch(profile='disabled-rehearsal')
        self._stale(self.run/'capture_window.py','VALUE="OLD"\n','VALUE="NEW"\n')
        capture=self.run/'worker_capture.py'
        capture.write_text('import capture_window\ndef main():\n print(capture_window.VALUE)\n')
        capture.chmod(0o600);self.hashes=self.rehash()
        args=control.entry_command(self.worker,self.hashes['collectorControl'],
            ['_rehearsal-capture',str(self.contract),self.run_id,str(self.kubeconfig),json.dumps(self.hashes),
             '--output',str(self.run/'rehearsal-capture'),'--inventory',
             str(self.run/'provider-inventory.json'),'--seconds','5400',
             '--boundary-dir',str(self.run/'disabled-boundary')])
        result=subprocess.run(args,cwd=str(self.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5,
            env=dict(os.environ,PYTHONPYCACHEPREFIX=str(self.root/'pycache')))
        self.assertNotEqual(result.returncode,0);self.assertEqual(json.loads(result.stdout),{'status':'INCONCLUSIVE'})
        args[7]='_capture'
        result=subprocess.run(args,cwd=str(self.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertNotEqual(result.returncode,0);self.assertEqual(json.loads(result.stdout),{'status':'INCONCLUSIVE'})
        args[7]='_rehearsal-capture';args[-3]='65'
        result=subprocess.run(args,cwd=str(self.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertNotEqual(result.returncode,0)

    def test_rehearsal_private_launch_and_status_cli_bind_profile(self):
        previous_mask=os.umask(0o077);os.umask(previous_mask)
        self.addCleanup(os.umask,previous_mask)
        args=[str(self.worker),'rehearsal-launch',str(self.contract),self.run_id,
              str(self.kubeconfig),json.dumps(self.hashes)]
        output=io.StringIO()
        with patch.object(control,'__file__',str(self.worker)),patch.object(sys,'argv',args), \
             patch.object(control.subprocess,'Popen',return_value=object()),redirect_stdout(output):
            self.assertEqual(control.main(),0)
        self.assertEqual(json.loads(output.getvalue()),{'status':'LAUNCHED'})
        self.assertEqual(self.child(profile='disabled-rehearsal'),0)
        args[1]='rehearsal-status';output=io.StringIO()
        with patch.object(sys,'argv',args),patch.object(control,'process_identity',return_value=dict(IDENTITY)), \
             redirect_stdout(output):
            self.assertEqual(control.main(),0)
        self.assertEqual(json.loads(output.getvalue()),dict(status='ALIVE',identity=IDENTITY))
        args[1]='status';output=io.StringIO()
        with patch.object(sys,'argv',args),redirect_stdout(output):self.assertEqual(control.main(),1)
        self.assertEqual(json.loads(output.getvalue()),{'status':'INCONCLUSIVE'})

    def test_launch_is_one_use_detached_even_after_unknown_spawn(self):
        self.assertEqual(self.launch(), {'status':'LAUNCHED'})
        _, kw = self.calls[0]
        self.assertTrue(kw['start_new_session']); self.assertTrue(kw['close_fds'])
        with self.assertRaises(ValueError): self.launch()
        self.assertEqual(len(self.calls), 1)

    def test_unknown_spawn_consumes_intent(self):
        def fail(*args, **kwargs): raise OSError('unknown')
        with self.assertRaises(ValueError): self.launch(fail)
        with self.assertRaises(ValueError): self.launch()
        self.assertEqual(self.status(), {'status':'INCONCLUSIVE'})

    def test_exact_identity_alive_then_sealed_completed_exit(self):
        self.launch(); self.assertEqual(self.child(), 0)
        self.observe()
        result = self.status()
        self.assertEqual(result, dict(status='EXITED',identity=IDENTITY,
            exit=dict(category='completed',exitCode=0,completionSha256=hashlib.sha256(
                (self.run/'collector-capture'/'completion.json').read_bytes()).hexdigest())))
        self.assertEqual(self.status(), result)

    def test_exit_without_previous_alive_is_inconclusive(self):
        self.launch(); self.child()
        self.assertEqual(self.status(), {'status':'INCONCLUSIVE'})

    def test_missing_nonzero_timeout_or_invalid_completion_never_exits_completed(self):
        cases = [dict(completion=False),dict(code=1),dict(timeout=True),
                 dict(completed=False),dict(cancelled=True)]
        for case in cases:
            with self.subTest(case=case):
                # Fresh private run is required for each one-use attempt.
                self.setUp(); self.launch(); self.child(**case); self.observe()
                self.assertEqual(self.status(), {'status':'INCONCLUSIVE'})
        self.assertTrue(self.killed)

    def test_timeout_once_kills_reaps_and_seals_timeout_without_completed_status(self):
        self.launch();self.assertEqual(self.child(code=-9,timeout='once'),1);self.observe()
        self.assertEqual(self.spawned.kills,1);self.assertEqual(self.spawned.waits,2)
        sealed=json.loads((self.run/'collector-job'/'exit.json').read_text())
        self.assertEqual(sealed,dict(identity=IDENTITY,exitCode=-9,category='timeout',completionSha256=None))
        self.assertEqual(self.status(),{'status':'INCONCLUSIVE'})

    def test_reused_pid_or_changed_completion_is_inconclusive(self):
        self.launch(); self.child(); self.observe()
        self.assertEqual(control.status(self.contract,self.run_id,self.hashes,
            probe=lambda pid: dict(IDENTITY,startTicks=999)), {'status':'INCONCLUSIVE'})
        path=self.run/'collector-capture'/'completion.json'
        path.write_text(path.read_text()+' ')
        self.assertEqual(self.status(), {'status':'INCONCLUSIVE'})

    def test_deadline_cannot_report_alive(self):
        self.launch(); self.child()
        self.assertEqual(self.status(alive=True,now=lambda:time.time()+100),
                         {'status':'INCONCLUSIVE'})

    def test_second_child_claim_cannot_launch_another_capture(self):
        self.launch(); self.child(); count=len(self.calls)
        self.assertEqual(self.child(),1)
        self.assertEqual(len(self.calls),count)

    def test_changed_source_is_refused_before_launch(self):
        (self.run/'capture_window.py').write_text('raise SystemExit(9)\n')
        with self.assertRaises(ValueError): self.launch()
        self.assertFalse(self.calls)

    def _stale(self,path,old,new):
        self.assertEqual(len(old),len(new))
        stamp=int(time.time())-100
        path.write_text(old);path.chmod(0o600);os.utime(path,(stamp,stamp))
        previous=sys.pycache_prefix
        try:
            sys.pycache_prefix=str(self.root/'pycache')
            py_compile.compile(str(path),cfile=importlib.util.cache_from_source(str(path),optimization=''),
                               doraise=True,optimize=0)
        finally:
            sys.pycache_prefix=previous
        path.write_text(new);path.chmod(0o600);os.utime(path,(stamp,stamp))

    def test_capture_executes_reviewed_transitive_bytes_ignoring_matching_pyc(self):
        self.launch()
        self._stale(self.run/'capture_window.py','VALUE="OLD"\n','VALUE="NEW"\n')
        capture=self.run/'worker_capture.py'
        capture.write_text('import capture_window\ndef main():\n print(capture_window.VALUE)\n')
        capture.chmod(0o600); self.hashes=self.rehash()
        cached=subprocess.run([sys.executable,'-c','import capture_window; print(capture_window.VALUE)'],
            cwd=str(self.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5,
            env=dict(os.environ,PYTHONPYCACHEPREFIX=str(self.root/'pycache')))
        self.assertEqual(cached.returncode,0,cached.stderr.decode())
        self.assertEqual(cached.stdout,b'OLD\n')
        args=control.entry_command(self.worker,self.hashes['collectorControl'],
            ['_capture',str(self.contract),self.run_id,str(self.kubeconfig),json.dumps(self.hashes),
             '--output',str(self.run/'collector-capture'),'--inventory',
             str(self.run/'provider-inventory.json'),'--seconds','65'])
        result=subprocess.run(args,cwd=str(self.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5,
            env=dict(os.environ,PYTHONPYCACHEPREFIX=str(self.root/'pycache')))
        self.assertEqual(result.returncode,0,result.stderr.decode())
        self.assertEqual(result.stdout,b'NEW\n')

    def test_entry_executes_reviewed_collector_bytes_ignoring_matching_pyc(self):
        self._stale(self.worker,'print("OLD")\n','print("NEW")\n')
        digest=hashlib.sha256(self.worker.read_bytes()).hexdigest()
        result=subprocess.run(control.entry_command(self.worker,digest,[]),
            stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5,
            env=dict(os.environ,PYTHONPYCACHEPREFIX=str(self.root/'pycache')))
        self.assertEqual(result.returncode,0,result.stderr.decode())
        self.assertEqual(result.stdout,b'NEW\n')

    def test_entry_cannot_import_unreviewed_local_shadow_before_source_checks(self):
        marker=self.root/'unreviewed-import'
        shadow=self.run/'hashlib.py'
        shadow.write_text('from pathlib import Path\nPath('+repr(str(marker))+').touch()\n')
        self.worker.write_text('print("reviewed")\n')
        digest=hashlib.sha256(self.worker.read_bytes()).hexdigest()
        result=subprocess.run(control.entry_command(self.worker,digest,[]),cwd=str(self.run),
            stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertFalse(marker.exists())
        self.assertEqual(result.returncode,0,result.stderr.decode())
        self.assertEqual(result.stdout,b'reviewed\n')

    def test_entry_rejects_changed_collector_before_any_code_executes(self):
        marker=self.root/'executed'
        self.worker.write_text('from pathlib import Path\nPath('+repr(str(marker))+').touch()\n')
        result=subprocess.run(control.entry_command(self.worker,self.hashes['collectorControl'],[]),
            stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertNotEqual(result.returncode,0);self.assertFalse(marker.exists())
        self.assertNotIn(str(marker).encode(),result.stdout+result.stderr)

    def test_capture_refuses_arbitrary_arguments(self):
        args=control.entry_command(self.worker,self.hashes['collectorControl'],
            ['_capture',str(self.contract),self.run_id,str(self.kubeconfig),json.dumps(self.hashes),
             '--output','/unbound','--inventory','/unbound','--seconds','65'])
        result=subprocess.run(args,cwd=str(self.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(json.loads(result.stdout),{'status':'INCONCLUSIVE'})


class CollectorParserTests(unittest.TestCase):
    def test_accepts_only_exact_successful_sealed_exit_and_sanitized_identity(self):
        value=dict(status='EXITED',identity=IDENTITY,
                   exit=dict(category='completed',exitCode=0,completionSha256='b'*64))
        self.assertEqual(ActionCatalog.parse('collector-status',json.dumps(value).encode(),{}),value)
        for exit_value in ({'category':'completed','exitCode':1,'completionSha256':'b'*64},
                           {'category':'timeout','exitCode':0,'completionSha256':'b'*64},
                           {'category':'completed','exitCode':0,'completionSha256':'invalid'}):
            with self.assertRaises(Refused):
                ActionCatalog.parse('collector-status',json.dumps(dict(value,exit=exit_value)).encode(),{})
        with self.assertRaises(Refused):
            validate_result(dict(state='complete',category='ok',payload=dict(value,exit=dict(value['exit'],privateAddress='hidden'))))

    def test_fixed_source_checked_action_uses_only_bound_paths(self):
        fixture=rehearsal_tests.DetachedWorkerTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        bindings=fixture._bindings()
        bindings['sourceHashes']['collectorControl']='c'*64
        for action in ('collector-launch','collector-status'):
            argv,_,timeout=ActionCatalog.command(action,bindings,fixture.run_id)
            words=shlex.split(argv[-1])
            self.assertEqual(words[0:4],['python3','-I','-S','-c'])
            self.assertEqual(words[5:8],[str(fixture.run/'pgcat_collector_control.py'),'c'*64,action.split('-')[1]])
            self.assertEqual(words[8:11],[str(fixture.contract),fixture.run_id,str(fixture.kubeconfig)])
            self.assertEqual(json.loads(words[11])['collectorControl'],'c'*64)
            self.assertEqual(timeout,5)

class CollectorWitnessContext(unittest.TestCase):
    def setUp(self):
        from test_pgcat_rehearsal_contract import RehearsalContractTests
        self.fixture=RehearsalContractTests();self.fixture.setUp();self.addCleanup(self.fixture.doCleanups)
        self.fixture._baseline();f=self.fixture
        for name in control.SOURCES.values():
            shutil.copyfile(str(Path(__file__).resolve().parent/name),str(f.run/name));(f.run/name).chmod(0o600)
        self.hashes={key:hashlib.sha256((f.run/name).read_bytes()).hexdigest() for key,name in control.SOURCES.items()}
        self.run,self.fd,self.verified=control.context(f.contract,f.run_id,self.hashes);self.addCleanup(os.close,self.fd)
    def derive(self):
        f=self.fixture
        return control.capture_context(f.contract,f.run_id,self.hashes,self.verified,self.fd)
    def identity(self,value=None):control.write(self.fd,'identity.json',dict(IDENTITY) if value is None else value)
    def test_exact_original_provenance_and_actual_child_identity(self):
        self.identity();f=self.fixture
        with patch.object(control,'process_identity',return_value=dict(IDENTITY)) as probe:
            value=self.derive()
        self.assertEqual(probe.call_args_list[0].args,(os.getpid(),))
        self.assertEqual(value['collectorIdentity'],IDENTITY)
        self.assertEqual(value['baselineSha256'],hashlib.sha256((f.contract/'baseline.json').read_bytes()).hexdigest())
        self.assertEqual(value['providerRunContextSha256'],hashlib.sha256(f.raw).hexdigest())
        self.assertEqual(value['sourceHashes'],self.hashes);self.assertEqual(value['bundleSha256'],'b'*64)
        self.assertEqual(value['originalRoster'],json.loads((f.contract/'baseline.json').read_text())['roster'])
    def test_verified_disabled_entry_injects_context_without_cli_capability(self):
        f=self.fixture
        # Offline subprocess gets a deterministic Linux identity probe; every
        # transitive module still executes its verified source bytes.
        path=f.run/'pgcat_collector_control.py';raw=path.read_text()
        raw=raw.replace("if __name__=='__main__':", "def process_identity(pid):return "+repr(IDENTITY)+"\n\nif __name__=='__main__':")
        path.write_text(raw)
        capture=f.run/'worker_capture.py';capture.write_text(capture.read_text()+
            "\ndef main(*,progress_context=None):\n print(json.dumps(progress_context,sort_keys=True))\n")
        hashes={key:hashlib.sha256((f.run/name).read_bytes()).hexdigest() for key,name in control.SOURCES.items()}
        control.write(self.fd,'identity.json',IDENTITY)
        control.write(self.fd,'intent.json',dict(runId=f.run_id,startedAt=int(time.time()),profile='disabled-rehearsal'))
        args=control.entry_command(path,hashes['collectorControl'],['_rehearsal-capture',str(f.contract),f.run_id,str(f.kubeconfig),json.dumps(hashes)]+control.capture_arguments(f.run,profile='disabled-rehearsal'))
        result=subprocess.run(args,cwd=str(f.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertEqual(result.returncode,0,result.stderr.decode()+result.stdout.decode())
        value=json.loads(result.stdout);self.assertEqual(value['runId'],f.run_id);self.assertEqual(value['collectorIdentity'],IDENTITY)
        self.assertEqual(value['sourceHashes'],hashes)

    def test_guardian_identity_race_wait_is_bounded_and_never_adopted(self):
        clock=[0];slept=[]
        def sleep(delay):
            slept.append(delay);clock[0]+=delay
            if clock[0]>=.03 and not (self.run/'collector-job'/'identity.json').exists():self.identity()
        with patch.object(control,'process_identity',return_value=dict(IDENTITY)),patch.object(control.time,'monotonic',side_effect=lambda:clock[0]),patch.object(control.time,'sleep',side_effect=sleep):
            self.assertEqual(self.derive()['collectorIdentity'],IDENTITY)
        self.assertTrue(slept);self.assertLessEqual(clock[0],1)
        (self.run/'collector-job'/'identity.json').unlink();self.identity(dict(IDENTITY,pid=9999))
        with patch.object(control,'process_identity',return_value=dict(IDENTITY)),self.assertRaises(ValueError):self.derive()
    def test_missing_identity_timeout_and_mutated_source_or_seal_refuse(self):
        clock=[0]
        with patch.object(control,'process_identity',return_value=dict(IDENTITY)),patch.object(control.time,'monotonic',side_effect=lambda:clock[0]), \
             patch.object(control.time,'sleep',side_effect=lambda delay:clock.__setitem__(0,clock[0]+delay)),self.assertRaises(ValueError):self.derive()
        self.assertLessEqual(clock[0],1.01)
        self.identity()
        for kind in ('source','seal','path','target','feed','run'):
            with self.subTest(kind=kind):
                f=self.fixture;path=f.contract/'baseline.json';saved=path.read_bytes();seal=Path(str(path)+'.sha256');saved_seal=seal.read_bytes();v=json.loads(saved)
                if kind=='source':self.verified['worker_capture']+=b'\n'
                if kind=='seal':path.write_bytes(saved+b' ')
                if kind=='path':v['paths']['captureDir']=str(f.run/'wrong')
                if kind=='target':v['targetSha256']='f'*64
                if kind=='feed':v['receipt']['feedSession']=str(uuid.uuid4())
                if kind=='run':v['providerRunContextSha256']='f'*64
                if kind not in ('source','seal'):
                    path.write_text(json.dumps(v));seal.write_text(json.dumps(dict(sha256=hashlib.sha256(path.read_bytes()).hexdigest())))
                with patch.object(control,'process_identity',return_value=dict(IDENTITY)),self.assertRaises(ValueError):self.derive()
                path.write_bytes(saved);seal.write_bytes(saved_seal)
                if kind=='source':self.verified['worker_capture']=self.verified['worker_capture'][:-1]


# These fixtures replace cluster reads only. Both guardian and collector are
# actual disposable Popen children; the production inherited FDs and publication
# code run unchanged. Evidence semantics are covered by the controller kernels.
def startup_test_identity(pid):
    return dict(pid=pid,bootId='offline-test',startTicks=pid,commandSha256='a'*64)


def startup_guardian_fixture(run,prepared_sha,payload,mode):
    import pgcat_native_controller as native
    run=Path(run);payload=json.loads(payload);plan=control.decode((run/'native-plan.json').read_bytes())
    if mode=='ack-closed':os.close(payload['ackFD'])
    if mode=='missing-context':(run/'native-context.json').unlink()
    if mode=='diagnostic-refused':(run/'collector-job'/'native-guardian-failure.json').symlink_to(run/'fixture-ready.json')
    if mode=='diagnostic-existing':(run/'collector-job'/'native-guardian-failure.json').write_bytes(b'SENTINEL-original')
    def proof(folder,**kwargs):
        delay=1.15 if mode=='delayed' else .03
        time.sleep(delay)
        if mode in ('proof-failure','diagnostic-refused','diagnostic-existing','diagnostic-write-error','diagnostic-fsync-error'):raise ValueError('SENTINEL-secret-url-key-path')
        if mode=='proof-timeout':
            time.sleep(max(0,kwargs['deadline']-time.monotonic())+.02)
        if mode=='collector-exit':time.sleep(.15)
        ready=control.decode((run/'fixture-ready.json').read_bytes())
        ready.update(collectorIdentity=control.decode((run/'collector-job'/'identity.json').read_bytes()),guardianIdentity=startup_test_identity(os.getpid()))
        native._write_bytes(run/'native-ready.json',json.dumps(ready,sort_keys=True,separators=(',',':')).encode('ascii'))
        return ready
    def command(*args):return [sys.executable,'-c','import time;time.sleep(%s)'%('.06' if mode=='collector-exit' else '30')]
    with patch('pgcat_lane_protocol._release_admission'),patch.object(control,'context',side_effect=lambda *a,**k:(run,control.directory(run/'collector-job'),{})),patch.object(control,'process_identity',side_effect=startup_test_identity),patch.object(control,'native_entry_command',side_effect=command),patch.object(native,'seal_ready',side_effect=proof),patch.object(control,'completion',return_value='a'*64):
        # This ceiling is only for the local disposable process test. Production
        # profile_values and the admitted 75..900 capture limit stay unchanged.
        original=control.profile_values
        def bounded(profile,cap=5400):
            values=original(profile,cap)
            return (values[0],values[1],1.7,values[3]) if profile==control.NATIVE_MODE else values
        from segmented_capture import read_progress
        def progress(*args):
            if mode=='waiting-failure':raise RuntimeError('SENTINEL-secret-url-key-path')
            return read_progress(*args)
        original_write=control.write
        def write_after_cleanup(fd,name,value):
            if name=='native-guardian-failure.json':
                identity=control.decode((run/'collector-job'/'identity.json').read_bytes())
                try:os.kill(identity['pid'],0)
                except ProcessLookupError:pass
                else:raise AssertionError('diagnostic before child cleanup')
                if mode=='diagnostic-write-error':raise OSError('SENTINEL-writer-refused')
                if mode=='diagnostic-fsync-error':
                    with patch.object(control.os,'fsync',side_effect=OSError('SENTINEL-fsync-refused')):
                        return original_write(fd,name,value)
            return original_write(fd,name,value)
        with patch.object(control,'profile_values',side_effect=bounded),patch('segmented_capture.read_progress',side_effect=progress),patch.object(control,'write',side_effect=write_after_cleanup):
            return control.run_child(run.parent/'contract',plan['runId'],run.parent/'kubeconfig',plan['sourceHashes'],profile=control.NATIVE_MODE,prepared_sha=prepared_sha,startup=payload)


class NativeOwnedStartupTests(unittest.TestCase):
    def fixture(self,mode='delayed'):
        import pgcat_native_controller as native
        from test_pgcat_native_controller import Fixture
        self.assertTrue(hasattr(control,'receive_startup_ack'),'private owned startup channel missing')
        temporary=tempfile.TemporaryDirectory();self.addCleanup(temporary.cleanup)
        root=Path(temporary.name).resolve();root.chmod(0o700);f=Fixture(root,'disabled-proof')
        (root/'contract').mkdir(mode=0o700,exist_ok=True);(f.run/'native-boundary').mkdir(mode=0o700,exist_ok=True)
        ready=f.run/'native-ready.json';ready.rename(f.run/'fixture-ready.json')
        # The fixture proof's context/pair prerequisite is supplied below; the
        # actual production seal_ready tests replay the complete real capture.
        self.children=[]
        def reap():
            for child in self.children:
                if child.poll() is None:child.kill()
                child.wait(timeout=2)
        self.addCleanup(reap)
        here=Path(__file__).parent.resolve()
        def spawn(args,**kwargs):
            prepared_sha,payload=args[-2:]
            body="import sys;sys.path.insert(0,%r);from test_pgcat_collector_control import startup_guardian_fixture;sys.exit(startup_guardian_fixture(*sys.argv[1:]))"%str(here)
            child=subprocess.Popen([sys.executable,'-c',body,str(f.run),prepared_sha,payload,mode],**kwargs);self.children.append(child);return child
        return f,spawn

    def launch(self,f,spawn):
        import pgcat_native_controller as native
        with patch('pgcat_lane_protocol._release_admission'),patch.object(control,'context',side_effect=lambda *a,**k:(f.run,control.directory(f.run/'collector-job') if (f.run/'collector-job').exists() else self.job(f.run),{})),patch.object(control,'process_identity',side_effect=startup_test_identity),patch.object(control.time,'time',return_value=f.now):
            return control.launch(f.run.parent/'contract',f.plan['runId'],f.run.parent/'kubeconfig',f.plan['sourceHashes'],profile=control.NATIVE_MODE,prepared=f.prepared,spawn=spawn)

    def job(self,run):
        (run/'collector-job').mkdir(mode=0o700);return control.directory(run/'collector-job')

    def test_owned_ack_returns_before_delayed_proof_and_capture_end_does_not_renew(self):
        f,spawn=self.fixture();before=time.monotonic();launched=self.launch(f,spawn);acked=time.monotonic()
        self.assertEqual(launched['status'],'LAUNCHED');self.assertNotIn('ready',launched)
        self.assertLess(acked-before,1.);self.assertFalse((f.run/'native-ready.json').exists())
        self.assertEqual(launched['guardianIdentity']['pid'],self.children[0].pid)
        self.assertFalse(launched['resultKey'] in '\0'.join(self.children[0].args))
        self.assertEqual(launched['collectorIdentity'],control.decode((f.run/'collector-job'/'identity.json').read_bytes()))
        self.assertGreater(launched['deadlineMonotonic'],acked)
        self.assertGreaterEqual(launched['deadlineMonotonic'],before+4.);self.assertLessEqual(launched['deadlineMonotonic'],acked+4.)
        self.children[0].wait(timeout=3)
        elapsed=time.monotonic()-before
        self.assertLess(elapsed,2.6) # 1.15s proof + original 1.7s cap, not 2.85s
        result=control.decode(Path(launched['resultPath']).read_bytes())
        import pgcat_native_controller as native
        native.verify_startup_result(result,launched,f.plan,f.prepared)

    def test_guardian_proof_failure_timeout_or_collector_exit_stops_actual_child(self):
        for mode in ('proof-failure','proof-timeout','collector-exit','missing-context'):
            with self.subTest(mode=mode):
                f,spawn=self.fixture(mode);launched=self.launch(f,spawn)
                self.assertEqual(self.children[0].wait(timeout=5),1)
                self.assertEqual(Path(launched['resultPath']).read_bytes(),b'')
                # Reaping is performed by run_child, not a test sidecar adopter.
                pid=launched['collectorIdentity']['pid']
                with self.assertRaises(ProcessLookupError):os.kill(pid,0)

    def test_startup_ack_eof_partial_and_crash_refuse_without_adoption(self):
        for body in ('pass','import os;os.write(int(__import__("sys").argv[1]),b"{")','raise RuntimeError()'):
            f,unused=self.fixture()
            def spawn(args,**kwargs):
                payload=json.loads(args[-1]);child=subprocess.Popen([sys.executable,'-c',body,str(payload['ackFD'])],**kwargs);self.children.append(child);return child
            with self.subTest(body=body),self.assertRaises(ValueError):self.launch(f,spawn)
            self.assertFalse((f.run/'collector-job'/'guardian.json').exists())

    def test_guardian_losing_original_ack_pipe_stops_its_retained_collector(self):
        f,spawn=self.fixture('ack-closed')
        with self.assertRaises(ValueError):self.launch(f,spawn)
        self.children[0].wait(timeout=2)
        identity=control.decode((f.run/'collector-job'/'identity.json').read_bytes())
        with self.assertRaises(ProcessLookupError):os.kill(identity['pid'],0)
        self.assertEqual((f.run/'collector-job'/'startup-result.json').read_bytes(),b'')
        self.assertFalse((f.run/'collector-job'/'guardian.json').exists())


class GuardianFailureDiagnosticsTests(unittest.TestCase):
    fixture=NativeOwnedStartupTests.fixture
    launch=NativeOwnedStartupTests.launch
    job=NativeOwnedStartupTests.job
    def test_actual_failure_phase_clocks_precleanup_exit_and_sanitization(self):
        for mode,phase,category in [('waiting-failure','waiting-frames','runtime-error'),('proof-failure','seal-ready','value-error'),('collector-exit','seal-ready','value-error'),('proof-timeout','seal-ready','value-error')]:
            with self.subTest(mode=mode):
                f,spawn=self.fixture(mode);launched=self.launch(f,spawn)
                self.assertEqual(self.children[0].wait(timeout=6),1)
                path=f.run/'collector-job'/'native-guardian-failure.json';self.assertTrue(path.exists(),'guardian failure record missing');raw=path.read_bytes();record=json.loads(raw)
                self.assertNotIn(b'SENTINEL',raw);self.assertLessEqual(len(raw),4096)
                self.assertEqual(path.stat().st_mode & 0o777,0o600)
                self.assertEqual(record['phase'],phase);self.assertEqual(record['category'],category)
                self.assertEqual(record['location'],'collectorControl.guardian_startup')
                self.assertTrue(record['sourceLocations'])
                for location in record['sourceLocations']:
                    self.assertEqual(set(location),{'module','line'});self.assertIn(location['module'],('collectorControl','nativeController'))
                    self.assertIs(type(location['line']),int);self.assertGreater(location['line'],0)
                self.assertEqual(record['action'],'native-collector-launch');self.assertEqual(record['runId'],f.plan['runId'])
                self.assertEqual(record['childReturnCode'],0 if mode=='collector-exit' else None)
                self.assertEqual(record['deadlineMonotonic'],launched['deadlineMonotonic'])
                self.assertAlmostEqual(record['elapsedSeconds'],record['failedMonotonic']-(record['deadlineMonotonic']-4.))
                self.assertEqual(record['deadlineExceeded'],mode=='proof-timeout')
                self.assertEqual(set(record),{'schemaVersion','runId','action','phase','location','category','childReturnCode','deadlineMonotonic','failedMonotonic','elapsedSeconds','deadlineExceeded','sourceLocations'})
                self.assertEqual(Path(launched['resultPath']).read_bytes(),b'')
                with self.assertRaises(ProcessLookupError):os.kill(launched['collectorIdentity']['pid'],0)

    def test_diagnostic_symlink_refusal_keeps_guardian_exit_and_cleanup(self):
        f,spawn=self.fixture('diagnostic-refused');before=(f.run/'fixture-ready.json').read_bytes();launched=self.launch(f,spawn)
        self.assertEqual(self.children[0].wait(timeout=3),1)
        self.assertTrue((f.run/'collector-job'/'native-guardian-failure.json').is_symlink())
        self.assertEqual((f.run/'fixture-ready.json').read_bytes(),before)
        self.assertEqual(Path(launched['resultPath']).read_bytes(),b'')
        with self.assertRaises(ProcessLookupError):os.kill(launched['collectorIdentity']['pid'],0)

    def test_existing_and_writer_errors_keep_original_exit_and_reap_child(self):
        for mode in ('diagnostic-existing','diagnostic-write-error','diagnostic-fsync-error'):
            with self.subTest(mode=mode):
                f,spawn=self.fixture(mode);launched=self.launch(f,spawn)
                self.assertEqual(self.children[0].wait(timeout=3),1)
                path=f.run/'collector-job'/'native-guardian-failure.json'
                if mode=='diagnostic-existing':self.assertEqual(path.read_bytes(),b'SENTINEL-original')
                if mode=='diagnostic-write-error':self.assertFalse(path.exists())
                if mode=='diagnostic-fsync-error':
                    self.assertNotIn(b'SENTINEL',path.read_bytes());self.assertEqual(path.stat().st_mode & 0o777,0o600)
                self.assertEqual(Path(launched['resultPath']).read_bytes(),b'')
                with self.assertRaises(ProcessLookupError):os.kill(launched['collectorIdentity']['pid'],0)

    def test_success_has_no_diagnostic_record(self):
        f,spawn=self.fixture('delayed');launched=self.launch(f,spawn);self.children[0].wait(timeout=3)
        self.assertFalse((f.run/'collector-job'/'native-guardian-failure.json').exists())
        self.assertNotEqual(Path(launched['resultPath']).read_bytes(),b'')
