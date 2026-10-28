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

    def test_rehearsal_capture_executes_reviewed_transitive_bytes_and_checks_intent(self):
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
        self.assertEqual(result.returncode,0,result.stderr.decode());self.assertEqual(result.stdout,b'NEW\n')
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
