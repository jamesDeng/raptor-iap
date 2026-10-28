"""Authenticated offline lane timing fixtures; no provider or cluster calls."""
import copy
import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from capture_window import assess
from segmented_capture import SegmentedCapture
from test_capture_window import transition_frame
from test_pgcat_collector_control import IDENTITY
try:
    from pgcat_lane_timing_assessor import assess_lane_timing
except ImportError:
    assess_lane_timing = None
SESSION='00000000-0000-4000-8000-000000000001'
RUN='00000000-0000-4000-8000-000000000002'
TARGET=dict(accountId='account',regionId='region',workerId='worker',scalingGroupId='group',
            nlbServerGroupId='nlb',originalInstanceIds=['a','b'])


def lane_frames():
    frames=[]
    for t in range(66):
        f=transition_frame(t)
        f['inventory'].update(mode='disabled-rehearsal',sequence=t//15+1,observedAt=(t//15)*15)
        f['deployment']=dict(uid='deployment',generation=1,observedGeneration=1,desired=2,ready=2)
        f['metrics']['upSourceAt']={'a':t,'b':t}
        p=f['pods'][0];p.update(name='client-a',containerId='containerd://a',ready=True)
        other=copy.deepcopy(p);other.update(uid='pod-b',name='client-b',containerId='containerd://b')
        other['snapshot']['sample']['processId']='process-b'
        for event in other['snapshot']['events']:event['processId']='process-b'
        f['pods'].append(other);f['expectedReplicas']=2
        frames.append(f)
    return frames


class TimingTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(os.path.realpath(self.tmp.name));self.root.chmod(0o700)

    def capture(self,frames=None):
        path=self.root/('capture-'+str(len(list(self.root.iterdir()))))
        writer=SegmentedCapture(path,max_segment_seconds=20,max_total_seconds=65)
        for f in frames if frames is not None else lane_frames():writer.append(f)
        writer.finish(completed=True,cancelled=False,errors=[])
        return path

    def result(self,path):
        self.assertIsNotNone(assess_lane_timing,'lane timing assessor is required')
        return assess_lane_timing(path,IDENTITY,TARGET,
            dict(runId=RUN,feedSession=SESSION,completionSha256=hashlib.sha256((path/'completion.json').read_bytes()).hexdigest()),
            {'captureWindow':'a'*64,'laneTimingAssessor':'b'*64})

    def test_complete_65_second_capture_passes_without_transition_or_final_markers(self):
        got=self.result(self.capture());self.assertEqual(got['status'],'PASS',got)
        self.assertEqual(got['category'],'lane-timing')
        self.assertEqual(got['runId'],RUN);self.assertEqual(got['feedSession'],SESSION)
        self.assertEqual(got['collectorIdentity'],IDENTITY)
        self.assertNotIn('containerd://',json.dumps(got))

    def test_inclusive_sixty_second_three_second_gap_and_fortyfive_second_metric_limits(self):
        frames=lane_frames()[:61];del frames[30:32]
        for f in frames:
            stamp=f['at']-45;f['metrics']['sourceAt']=stamp
            f['metrics']['upSourceAt']={'a':stamp,'b':stamp}
            for row in f['metrics']['raw']:row['sourceAt']=stamp
        got=self.result(self.capture(frames));self.assertEqual(got['status'],'PASS',got)

    def test_transition_opt_out_defaults_to_required(self):
        frames=lane_frames()
        self.assertIn('incomplete rehearsal transition',assess(frames,completed=True)['reasons'])
        self.assertEqual(assess(frames,completed=True,require_transition=False)['status'],'PASS')

    def test_rules_refuse_each_incomplete_interval(self):
        cases=['short','gap','pod','container','process','ready','deployment','generation','observed generation',
               'replicas','ess','nlb','health','weight','feed','sequence','provider age','missing up','stale up',
               'stale raw','missing raw','skipped','stalled']
        for case in cases:
            with self.subTest(case=case):
                frames=lane_frames();f=frames[35]
                if case=='short':frames=frames[:60]
                elif case=='gap':del frames[30:33]
                elif case in ('pod','container','process','ready'):
                    if case=='process':f['pods'][0]['snapshot']['sample']['processId']='restart'
                    else:f['pods'][0][{'pod':'uid','container':'containerId','ready':'ready'}[case]]=False if case=='ready' else 'changed'
                elif case in ('deployment','generation','observed generation','replicas'):
                    f['deployment'][{'deployment':'uid','generation':'generation','observed generation':'observedGeneration','replicas':'ready'}[case]]='changed' if case=='deployment' else 3
                elif case=='ess':f['inventory']['memberStates'][0]['lifecycle']='InService'
                elif case=='nlb':f['inventory']['backendStates'].pop()
                elif case=='health':f['inventory']['memberStates'][0]['health']='Unhealthy'
                elif case=='weight':f['inventory']['backendStates'][0]['weight']=99
                elif case=='feed':f['inventory']['feedSession']=RUN
                elif case=='sequence':f['inventory']['sequence']+=1
                elif case=='provider age':f['inventory']['observedAt']=5
                elif case=='missing up':del f['metrics']['upSourceAt']['a']
                elif case=='stale up':f['metrics']['upSourceAt']['a']=-11
                elif case=='stale raw':f['metrics']['raw'][0]['sourceAt']=-11
                elif case=='missing raw':f['metrics']['raw'].pop()
                elif case=='skipped':f['pods'][0]['snapshot']['sample']['Skipped']=1
                elif case=='stalled':
                    for row in frames[30:]:row['pods']=copy.deepcopy(frames[29]['pods'])
                self.assertEqual(self.result(self.capture(frames))['status'],'INCONCLUSIVE')

    def test_missing_segment_does_not_hide_later_authenticated_sql_failure(self):
        frames=lane_frames();frames[50]['pods'][0]['snapshot']['sample']['Failure']=1
        path=self.capture(frames);(path/'segment-0001.jsonl').unlink()
        self.assertEqual(self.result(path)['status'],'FAIL')

    def test_authenticated_failure_survives_malformed_segment_metadata(self):
        frames=lane_frames();frames[50]['pods'][0]['snapshot']['sample']['Failure']=1
        path=self.capture(frames);completion=path/'completion.json';value=json.loads(completion.read_text())
        value['segments'][2]['firstAt']='invalid';completion.write_text(json.dumps(value))
        self.assertEqual(self.result(path)['status'],'FAIL')

    def test_connection_failure_has_fail_precedence_and_disconnect_is_inconclusive(self):
        for kind,want in [('connection_failure','FAIL'),('disconnected','INCONCLUSIVE')]:
            with self.subTest(kind=kind):
                frames=lane_frames();snapshot=frames[35]['pods'][0]['snapshot']
                for f in frames[35:]:
                    f['pods'][0]['snapshot']['sample']['sequence']+=1
                    for event in f['pods'][0]['snapshot']['events']:
                        if event['sequence']>=71:event['sequence']+=1
                snapshot['events'].append(dict(processId='process',sequence=71,at=35,kind=kind))
                if kind=='disconnected':
                    self.assertEqual(assess(frames,completed=True,require_transition=False)['status'],'PASS')
                path=self.capture(frames)
                if kind=='connection_failure':(path/'segment-0001.jsonl').unlink()
                self.assertEqual(self.result(path)['status'],want)

    def test_malformed_earlier_connectivity_event_cannot_hide_later_authenticated_failure(self):
        self.assertEqual(self.result(self.capture())['status'],'PASS')
        for later_failure,want in [(False,'INCONCLUSIVE'),(True,'FAIL')]:
            with self.subTest(later_failure=later_failure):
                frames=lane_frames();snapshot=frames[35]['pods'][0]['snapshot']
                snapshot['events'].append(dict(processId='process',sequence=71,at=None,kind='disconnected'))
                snapshot['sample']['sequence']=71
                if later_failure:
                    snapshot['events'].append(dict(processId='process',sequence=72,at=35,kind='connection_failure'))
                    snapshot['sample']['sequence']=72
                self.assertEqual(self.result(self.capture(frames))['status'],want)

    def test_changed_completion_and_unauthenticated_failure_cannot_pass_or_fail(self):
        path=self.capture();context=dict(runId=RUN,feedSession=SESSION,completionSha256='f'*64)
        self.assertIsNotNone(assess_lane_timing)
        self.assertEqual(assess_lane_timing(path,IDENTITY,TARGET,context,{'a':'a'*64})['status'],'INCONCLUSIVE')
        segment=path/'segment-0001.jsonl';segment.write_text(segment.read_text()+' ')
        self.assertEqual(self.result(path)['status'],'INCONCLUSIVE')

    def test_symlink_segment_and_wrong_completion_bounds_are_refused(self):
        path=self.capture();seg=path/'segment-0001.jsonl';moved=self.root/'moved';seg.rename(moved);seg.symlink_to(moved)
        self.assertEqual(self.result(path)['status'],'INCONCLUSIVE')
        path=self.capture();m=path/'completion.json';value=json.loads(m.read_text());value['maxTotalSeconds']=60;m.write_text(json.dumps(value))
        self.assertEqual(self.result(path)['status'],'INCONCLUSIVE')

from pgcat_lane_actions import ActionCatalog
from pgcat_lane_protocol import Refused, publish_request
import test_pgcat_lane_protocol as protocol_tests
import test_pgcat_rehearsal_worker as rehearsal_tests
import pgcat_collector_control as control


class VerdictBindingTests(unittest.TestCase):
    request=protocol_tests.ProtocolTest.request
    admit=protocol_tests.ProtocolTest.admit
    def setUp(self):
        protocol_tests.ProtocolTest.setUp(self)
        # Fresh run includes the supervisor's exact reviewed assessor source.
        with self.journal._transaction() as db:
            db.execute('UPDATE run SET hashes=?',(json.dumps(dict(bundleSha256='a'*64,laneTimingAssessor='c'*64)),))

    def sealed(self,payload,sequence):
        req=self.request(action='collector-status',minimumSequence=sequence)
        self.admit(req)
        self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=payload))
        return req

    def exited(self):
        self.sealed(dict(status='ALIVE',identity=IDENTITY),1)
        return self.sealed(dict(status='EXITED',identity=IDENTITY,
            exit=dict(category='completed',exitCode=0,completionSha256='d'*64)),2)

    def test_verdict_requires_exact_previously_observed_collector_exit(self):
        req=self.request(action='lane-verdict',minimumSequence=3)
        publish_request(self.root,req)
        with self.assertRaises(Refused):self.journal.admit(req)
        self.exited();self.journal.admit(req)
        bound=self.journal.collector_exit()
        self.assertEqual(bound['payload']['identity'],IDENTITY)
        self.assertEqual(bound['payload']['exit']['completionSha256'],'d'*64)
        payload=dict(status='PASS',category='lane-timing',verdictSha256='e'*64,clientIdentitySha256='a'*64,workerCredentialCleanupSha256='b'*64)
        result=dict(state='complete',category='ok',payload=payload)
        with self.assertRaises(Refused):self.journal.seal(req['requestId'],result)
        with self.assertRaises(Refused):self.journal.seal(req['requestId'],result,
            lane_verdict_source_sha='f'*64,lane_verdict_result=payload)
        with self.assertRaises(Refused):self.journal.seal(req['requestId'],result,
            lane_verdict_source_sha='c'*64,lane_verdict_result=dict(payload,verdictSha256='f'*64))
        self.journal.seal(req['requestId'],result,lane_verdict_source_sha='c'*64,lane_verdict_result=payload)
        verdict=self.journal.lane_verdict()
        self.assertEqual(verdict['payload'],payload)
        self.assertEqual(verdict['collectorExit'],bound)
        later=self.request(action='lane-verdict',minimumSequence=4);publish_request(self.root,later)
        with self.assertRaises(Refused):self.journal.admit(later)

    def test_changed_completion_after_journaled_exit_is_refused(self):
        self.exited()
        req=self.request(action='collector-status',minimumSequence=3);self.admit(req)
        payload=dict(status='EXITED',identity=IDENTITY,
                     exit=dict(category='completed',exitCode=0,completionSha256='f'*64))
        with self.assertRaises(Refused):
            self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=payload))

    def test_changed_collector_identity_cannot_replace_journaled_original(self):
        self.exited();req=self.request(action='collector-status',minimumSequence=3);self.admit(req)
        payload=dict(status='EXITED',identity=dict(IDENTITY,startTicks=999),
                     exit=dict(category='completed',exitCode=0,completionSha256='d'*64))
        with self.assertRaises(Refused):
            self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=payload))

    def test_exit_without_alive_or_with_changed_identity_cannot_be_journaled(self):
        payload=dict(status='EXITED',identity=IDENTITY,exit=dict(category='completed',exitCode=0,completionSha256='d'*64))
        req=self.request(action='collector-status');self.admit(req)
        with self.assertRaises(Refused):self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=payload))


class VerdictActionTests(unittest.TestCase):
    def test_fixed_action_binds_only_supervisor_context_and_rejects_caller_text(self):
        fixture=rehearsal_tests.DetachedWorkerTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        bindings=fixture._bindings();bindings['sourceHashes']['laneTimingAssessor']='e'*64
        context=dict(runId=fixture.run_id,feedSession=SESSION,collectorIdentity=IDENTITY,completionSha256='d'*64)
        argv,env,budget=ActionCatalog.command('lane-verdict',bindings,fixture.run_id,run_context=context)
        import shlex
        words=shlex.split(argv[-1]);self.assertEqual(words[:4],['python3','-I','-S','-c'])
        self.assertEqual(words[7],'verdict');self.assertEqual(json.loads(words[-1]),context)
        good=dict(status='PASS',category='lane-timing',verdictSha256='e'*64,clientIdentitySha256='a'*64,workerCredentialCleanupSha256='b'*64)
        self.assertEqual(ActionCatalog.parse('lane-verdict',json.dumps(good).encode(),{}),good)
        for value in (dict(good,privateAddress='hidden'),dict(good,verdictSha256='invalid'),dict(good,category='ok'),
                      dict(good,clientIdentitySha256=None),dict(good,workerCredentialCleanupSha256=None)):
            with self.assertRaises(Refused):ActionCatalog.parse('lane-verdict',json.dumps(value).encode(),{})

    def test_worker_refuses_boolean_exit_code_before_sealing_verdict(self):
        from pgcat_lane_timing_assessor import seal_verdict
        case=TimingTests();case.setUp();self.addCleanup(case.doCleanups)
        run=case.root/'run';run.mkdir(mode=0o700);job=run/'collector-job';job.mkdir(mode=0o700)
        fd=control.directory(job)
        try:
            control.write(fd,'identity.json',IDENTITY);control.write(fd,'observed.json',IDENTITY)
            control.write(fd,'exit.json',dict(identity=IDENTITY,exitCode=False,category='completed',completionSha256='d'*64))
        finally:os.close(fd)
        context=dict(runId=RUN,feedSession=SESSION,collectorIdentity=IDENTITY,completionSha256='d'*64)
        with self.assertRaises(ValueError):seal_verdict(run,RUN,TARGET,context,{'laneTimingAssessor':'a'*64})
        self.assertFalse((job/'lane-verdict.json').exists())

    def test_worker_verdict_is_one_use_and_matches_sealed_capture_identity(self):
        from pgcat_lane_timing_assessor import seal_verdict
        case=TimingTests();case.setUp();self.addCleanup(case.doCleanups)
        run=case.root/'run';run.mkdir(mode=0o700);job=run/'collector-job';job.mkdir(mode=0o700)
        capture=case.capture();capture.rename(run/'collector-capture');capture=run/'collector-capture'
        digest=hashlib.sha256((capture/'completion.json').read_bytes()).hexdigest()
        fd=control.directory(job)
        try:
            control.write(fd,'identity.json',IDENTITY);control.write(fd,'observed.json',IDENTITY)
            control.write(fd,'exit.json',dict(identity=IDENTITY,exitCode=0,category='completed',completionSha256=digest))
        finally:os.close(fd)
        context=dict(runId=RUN,feedSession=SESSION,collectorIdentity=IDENTITY,completionSha256=digest)
        result=seal_verdict(run,RUN,TARGET,context,{'laneTimingAssessor':'a'*64})
        self.assertEqual(result['status'],'PASS')
        self.assertEqual(result['verdictSha256'],hashlib.sha256((job/'lane-verdict.json').read_bytes()).hexdigest())
        with self.assertRaises((ValueError,OSError)):seal_verdict(run,RUN,TARGET,context,{'laneTimingAssessor':'a'*64})

class VerdictExecutionTests(unittest.TestCase):
    def fixture(self):
        from test_pgcat_collector_control import CollectorTests
        case=CollectorTests();case.setUp();self.addCleanup(case.doCleanups)
        return case

    def test_verdict_executes_reviewed_assessor_source_even_with_matching_stale_pyc(self):
        import subprocess
        import sys
        case=self.fixture();path=case.run/'pgcat_lane_timing_assessor.py'
        case._stale(path,'def seal_verdict(*args): open("reviewed-marker","w").write("OLD"); return {"status":"OLD"}\n',
                         'def seal_verdict(*args): open("reviewed-marker","w").write("NEW"); return {"status":"NEW"}\n')
        hashes=case.rehash()
        args=control.entry_command(case.worker,hashes['collectorControl'],['verdict',str(case.contract),
            case.run_id,str(case.kubeconfig),json.dumps(hashes),'{}','{}'])
        result=subprocess.run(args,cwd=str(case.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,
            env=dict(os.environ,PYTHONPYCACHEPREFIX=str(case.root/'pycache')),timeout=5)
        self.assertNotEqual(result.returncode,0)
        self.assertEqual((case.run/'reviewed-marker').read_text(),'NEW')
        self.assertTrue(case.kubeconfig.exists())
        path.write_text('raise SystemExit(9)\n')
        result=subprocess.run(args,cwd=str(case.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(json.loads(result.stdout),{'status':'INCONCLUSIVE'})

    def test_fixed_entry_sql_fail_survives_cleanup_error_or_incomplete_identity(self):
        import subprocess
        from pgcat_lane_actions import ActionCatalog
        from pgcat_lane_protocol import validate_result
        for fault in ('unlink','fsync','identity'):
            with self.subTest(fault=fault):
                case=self.fixture();capture_case=TimingTests();capture_case.setUp();self.addCleanup(capture_case.doCleanups)
                frames=lane_frames();frames[50]['pods'][0]['snapshot']['sample']['Failure']=1
                if fault=='identity':
                    extras=[]
                    for n in range(1,7):
                        extra=copy.deepcopy(frames[50]);extra['at']=50+n/10.;extras.append(extra)
                    frames[51:51]=extras
                capture=capture_case.capture(frames);capture.rename(case.run/'collector-capture')
                completion=hashlib.sha256((case.run/'collector-capture'/'completion.json').read_bytes()).hexdigest()
                job=case.run/'collector-job';job.mkdir(mode=0o700);fd=control.directory(job)
                try:
                    control.write(fd,'identity.json',IDENTITY);control.write(fd,'observed.json',IDENTITY)
                    control.write(fd,'exit.json',dict(identity=IDENTITY,exitCode=0,category='completed',completionSha256=completion))
                finally:os.close(fd)
                context=dict(runId=case.run_id,feedSession=SESSION,collectorIdentity=IDENTITY,completionSha256=completion)
                args=control.entry_command(case.worker,case.hashes['collectorControl'],['verdict',str(case.contract),
                    case.run_id,str(case.kubeconfig),json.dumps(case.hashes),json.dumps(TARGET),json.dumps(context)])
                # Inject only the OS failure at the local subprocess boundary;
                # the fixed entry still verifies and executes all staged bytes.
                if fault=='unlink':
                    args[4]='import os\n_real_unlink=os.unlink\ndef _unlink(p,*a,**k):\n if p=="kubeconfig": raise OSError("fixture")\n return _real_unlink(p,*a,**k)\nos.unlink=_unlink\n'+args[4]
                if fault=='fsync':
                    args[4]='import os\n_real_fsync=os.fsync\ndef _fsync(fd):\n if not os.path.exists('+repr(str(case.kubeconfig))+'): raise OSError("fixture")\n return _real_fsync(fd)\nos.fsync=_fsync\n'+args[4]
                result=subprocess.run(args,cwd=str(case.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
                self.assertEqual(result.returncode,0,result.stdout)
                response=json.loads(result.stdout);self.assertEqual(response['status'],'FAIL')
                self.assertEqual(response['verdictSha256'],hashlib.sha256((job/'lane-verdict.json').read_bytes()).hexdigest())
                if fault=='identity':self.assertIsNone(response['clientIdentitySha256'])
                else:
                    self.assertIsNone(response['workerCredentialCleanupSha256'])
                    self.assertFalse((job/'credential-cleanup.json').exists())
                validate_result(dict(state='complete',category='ok',payload=response))
                self.assertEqual(ActionCatalog.parse('lane-verdict',result.stdout,{}),response)
                self.assertEqual(case.kubeconfig.exists(),fault=='unlink')

    def test_real_fixed_entry_seals_bound_capture_once_without_arbitrary_paths(self):
        import subprocess
        case=self.fixture();capture_case=TimingTests();capture_case.setUp();self.addCleanup(capture_case.doCleanups)
        capture=capture_case.capture();capture.rename(case.run/'collector-capture')
        capture=case.run/'collector-capture';completion=hashlib.sha256((capture/'completion.json').read_bytes()).hexdigest()
        job=case.run/'collector-job';job.mkdir(mode=0o700);fd=control.directory(job)
        try:
            control.write(fd,'identity.json',IDENTITY);control.write(fd,'observed.json',IDENTITY)
            control.write(fd,'exit.json',dict(identity=IDENTITY,exitCode=0,category='completed',completionSha256=completion))
        finally:os.close(fd)
        context=dict(runId=case.run_id,feedSession=SESSION,collectorIdentity=IDENTITY,completionSha256=completion)
        args=control.entry_command(case.worker,case.hashes['collectorControl'],['verdict',str(case.contract),
            case.run_id,str(case.kubeconfig),json.dumps(case.hashes),json.dumps(TARGET),json.dumps(context)])
        result=subprocess.run(args,cwd=str(case.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertEqual(result.returncode,0,result.stderr.decode())
        response=json.loads(result.stdout);self.assertEqual(response['status'],'PASS',result.stdout)
        self.assertEqual(set(response),{'status','category','verdictSha256','clientIdentitySha256','workerCredentialCleanupSha256'})
        self.assertFalse(case.kubeconfig.exists())
        cleanup=json.loads((job/'credential-cleanup.json').read_text())
        self.assertEqual(cleanup,dict(runId=case.run_id,verdictSha256=response['verdictSha256'],workerKubeconfigAbsent=True))
        self.assertEqual(response['workerCredentialCleanupSha256'],hashlib.sha256((job/'credential-cleanup.json').read_bytes()).hexdigest())
        self.assertEqual(response['clientIdentitySha256'],json.loads((job/'lane-verdict.json').read_text())['clientIdentitySha256'])
        self.assertEqual(response['verdictSha256'],hashlib.sha256((job/'lane-verdict.json').read_bytes()).hexdigest())
        result=subprocess.run(args,cwd=str(case.run),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(json.loads(result.stdout),{'status':'INCONCLUSIVE'})


class SupervisorVerdictTests(unittest.TestCase):
    def test_fixed_supervisor_action_uses_journal_exit_and_journals_exact_worker_hash(self):
        self.fixed_payload(dict(status='PASS',category='lane-timing',verdictSha256='e'*64,
            clientIdentitySha256='a'*64,workerCredentialCleanupSha256='b'*64))

    def test_supervisor_journals_fail_with_null_identity_and_cleanup(self):
        self.fixed_payload(dict(status='FAIL',category='lane-timing',verdictSha256='e'*64,
            clientIdentitySha256=None,workerCredentialCleanupSha256=None))

    def test_supervisor_journals_inconclusive_with_null_identity_and_cleanup(self):
        self.fixed_payload(dict(status='INCONCLUSIVE',category='lane-timing',verdictSha256='e'*64,
            clientIdentitySha256=None,workerCredentialCleanupSha256=None))

    def fixed_payload(self,payload):
        import shlex
        import sys
        import test_pgcat_lane_supervisor as supervisor_tests
        fixture=supervisor_tests.SupervisorTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        with fixture.journal._transaction() as db:db.execute("UPDATE run SET mode='no-scale'")
        for sequence,collector_payload in [(1,dict(status='ALIVE',identity=IDENTITY)),
                (2,dict(status='EXITED',identity=IDENTITY,exit=dict(category='completed',exitCode=0,completionSha256='d'*64)))]:
            fixture.receipt(sequence);req=fixture.request(action='collector-status')
            fixture.journal.admit(req)
            fixture.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=collector_payload))
        marker=fixture.base/'remote-command'
        fixture.cli.write_text('#!'+sys.executable+'\nimport sys\nfrom pathlib import Path\n'
            +'Path('+repr(str(marker))+').write_text(sys.argv[-1])\nprint('+repr(json.dumps(payload))+')\n')
        fixture.receipt(3);result=fixture.run_request(action='lane-verdict')
        self.assertEqual(result['result'],dict(state='complete',category='ok',payload=payload))
        words=shlex.split(marker.read_text());context=json.loads(words[-1])
        self.assertEqual(context,dict(runId=fixture.run,feedSession=fixture.session,
            collectorIdentity=IDENTITY,completionSha256='d'*64))
        bound=fixture.journal.lane_verdict()
        self.assertEqual(bound['payload'],payload)
        self.assertEqual(bound['sourceSha256'],fixture.hashes['laneTimingAssessor'])


if __name__=='__main__':
    unittest.main()
