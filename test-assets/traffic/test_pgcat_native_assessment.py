"""Offline C3 closure: real private files, no credentials or live services."""
import copy
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import pgcat_native_ingress as ingress
import pgcat_native_closure as closure
from segmented_capture import canonical,sha
from test_pgcat_native_ingress import plan


def resource_scope(p,**updates):
    s=copy.deepcopy(p['controllerScope'])
    s.update(accountId=p['target']['accountId'],region=p['target']['regionId'],listenerId='listener',port=6432,databaseResourceId='database')
    s.update(updates)
    keys=('envCode','accountId','region','clusterId','proxyCode','groupId','serverGroupId','listenerId','port','targetDbCode','databaseResourceId','application')
    from collections import OrderedDict
    import json
    identity=OrderedDict((k,s['application']['clusterId'] if k=='clusterId' else s[k]) for k in keys)
    identity['application']=OrderedDict((k,s['application'][k]) for k in ('envCode','appCode','clusterId','namespace','name','uid'))
    s['resolutionDigest']=sha(json.dumps(identity,separators=(',',':')).encode())
    return s


class ResourceTests(unittest.TestCase):
    def test_listener_key_preserves_bare_and_exact_composite_identity(self):
        for listener in ('listener','lsn-fixture123@6432','lsn-a@6432','lsn-'+'a'*64+'@6432'):
            with self.subTest(listener=listener):
                p=plan();s=resource_scope(p,listenerId=listener)
                result=ingress.resource_scope(s,p)
                self.assertEqual(result,s)
                self.assertEqual(result['listenerId'],listener)
                s['listenerId']='changed'
                self.assertEqual(result['listenerId'],listener)
                p['controllerScope']=result
                self.assertEqual(ingress.validate_plan(p)['controllerScope'],result)

    def test_listener_key_rejects_malformed_composites_with_valid_digest(self):
        invalid=('lsn-fixture123@5432','lsn-fixture123@6433','lsn-fixture123@@6432',
                 'lsn-@6432','lsn-fixture!123@6432','lsn-fixture123@6432\n',
                 'lsn-'+'a'*65+'@6432','lsn-Fixture123@6432','listener@6432',
                 '',None,6432,True)
        for listener in invalid:
            with self.subTest(listener=listener):
                p=plan();s=resource_scope(p,listenerId=listener)
                with self.assertRaises(ValueError):ingress.resource_scope(s,p)
        for port in (5432,6433,True):
            with self.subTest(port=port):
                p=plan();s=resource_scope(p,listenerId='lsn-fixture123@6432',port=port)
                with self.assertRaises(ValueError):ingress.resource_scope(s,p)

    def test_listener_key_does_not_relax_other_tokens_digest_or_scope(self):
        p=plan()
        for key in ('envCode','proxyCode','groupId','serverGroupId','targetDbCode',
                    'accountId','region','databaseResourceId'):
            with self.subTest(key=key):
                s=resource_scope(p,**{key:'fixture@6432'})
                self.assertFalse(ingress.token(s[key]))
                with self.assertRaises(ValueError):ingress.resource_scope(s,p)
        for key in p['controllerScope']['application']:
            with self.subTest(application=key):
                app=copy.deepcopy(p['controllerScope']['application']);app[key]='fixture@6432'
                s=resource_scope(p,application=app)
                with self.assertRaises(ValueError):ingress.resource_scope(s,p)
        s=resource_scope(p,listenerId='lsn-fixture123@6432');s['resolutionDigest']='0'*64
        with self.assertRaises(ValueError):ingress.resource_scope(s,p)
        for updates in (dict(groupId='wrong'),dict(application=dict(p['controllerScope']['application'],uid='wrong'))):
            with self.subTest(updates=updates):
                s=resource_scope(p,listenerId='lsn-fixture123@6432',**updates)
                with self.assertRaises(ValueError):ingress.resource_scope(s,p)

    def test_complete_scope_is_validated_without_dropping_proof(self):
        p=plan();s=resource_scope(p)
        self.assertTrue(callable(getattr(ingress,'resource_scope',None)))
        self.assertEqual(ingress.resource_scope(s,p),s)
        for key in ('accountId','listenerId','databaseResourceId','resolutionDigest','region','port'):
            bad=copy.deepcopy(s);bad[key]=1 if key=='port' else 'changed'
            with self.subTest(key=key),self.assertRaises(ValueError):ingress.resource_scope(bad,p)
        with self.assertRaises(ValueError):ingress.resource_scope(dict(s,extra='ignored'),p)

    def test_safe_released_wait_requires_actual_three_cleanup_booleans(self):
        self.assertTrue(callable(getattr(ingress,'released_wait',None)))
        raw=dict(requestId='request',attemptId='attempt',status='waiting_resource',stage='waiting_resource',runtimeMode='live',recoveryNeeded=False,
                 cleanup='pending',cleanupStatus=dict(sandboxAbsent=True,keyAbsent=True,accessRevoked=True))
        self.assertEqual(ingress.released_wait(raw)['cleanupStatus'],raw['cleanupStatus'])
        for key in raw['cleanupStatus']:
            bad=copy.deepcopy(raw);bad['cleanupStatus'][key]=1
            with self.assertRaises(ValueError):ingress.released_wait(bad)


class CredentialTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name).resolve();self.root.chmod(0o700)
        self.path=self.root/'kubeconfig';self.path.write_bytes(b'offline-owned-material');self.path.chmod(0o600)
        from pgcat_native_controller import credential_identity
        self.identity=credential_identity(self.path)
    def test_exact_material_deleted_once_and_absence_independently_proved(self):
        self.assertTrue(callable(getattr(closure,'cleanup_bound_credential',None)))
        result=closure.cleanup_bound_credential(self.path,self.identity)
        self.assertEqual(result['state'],'deleted');self.assertFalse(self.path.exists())
        self.assertTrue((self.root/'kubeconfig.cleanup.claim').exists())
        with self.assertRaises(ValueError):closure.cleanup_bound_credential(self.path,self.identity)
    def test_inode_rewrite_symlink_and_foreign_paths_refuse(self):
        self.assertTrue(callable(getattr(closure,'cleanup_bound_credential',None)))
        self.path.write_bytes(b'foreign')
        with self.assertRaises(ValueError):closure.cleanup_bound_credential(self.path,self.identity)
        self.assertTrue(self.path.exists())
    def test_assessment_interface_exists(self):
        import importlib.util
        self.assertIsNotNone(importlib.util.find_spec('pgcat_native_assessment'))


class DiagnosticTests(unittest.TestCase):
    def test_source_bound_false_idle_kernel_exists(self):
        self.assertTrue(callable(getattr(closure,'gateway_false_idle',None)))



class HoldTests(unittest.TestCase):
    def test_restore_hold_uses_actual_worker_monotonic_and_boot(self):
        self.assertTrue(callable(getattr(closure,'hold_ready',None)))
        hold=dict(schemaVersion=1,runId='run',bootId='boot',acknowledgedMonotonic=30.,finalDurableMonotonic=31.,notBeforeMonotonic=151.)
        self.assertFalse(closure.hold_ready(hold,'run','boot',150.999))
        self.assertTrue(closure.hold_ready(hold,'run','boot',151.))
        with self.assertRaises(ValueError):closure.hold_ready(hold,'run','other',500.)
        bad=dict(hold,notBeforeMonotonic=150.)
        with self.assertRaises(ValueError):closure.hold_ready(bad,'run','boot',500.)


class AbortTests(unittest.TestCase):
    def test_abort_kernel_exists_and_is_fixed_controller_only(self):
        self.assertTrue(callable(getattr(closure,'abort_owned',None)))
        with self.assertRaises(ValueError):closure.abort_owned(Path('/tmp/run'),adapters=object(),now=lambda:0)


class CausalWitnessTests(unittest.TestCase):
    def setUp(self):
        from test_pgcat_native_ingress import Fixture,START,timestamp,REQUEST
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name).resolve();self.root.chmod(0o700);self.f=Fixture(self.root)
        value=copy.deepcopy(self.f.frame)
        value['inventory'].update(desired=4,members=['a','b','c','d'],healthyRegistered=['c','d'],
            memberStates=[dict(id=n,lifecycle='Protected',health='Healthy',addressKnown=True) for n in ('a','b','c','d')],
            backendStates=[dict(id=n,status='Available',weight=100,listenerHealth='Normal') for n in ('c','d')])
        self.f.append(2,value)
        self.f.execution.update(status='waiting_resource',stage='waiting_resource',cleanupStatus=dict(sandboxAbsent=True,keyAbsent=True,accessRevoked=True))
        self.f.events=[]
        tools=[('mcp__infra__db_proxy_nodes_deregister','tool_start','started'),('mcp__infra__db_proxy_nodes_deregister','tool_result','succeeded'),('resource_wait','tool_start','started'),('resource_wait','tool_result','succeeded')]
        for n,(tool,kind,status) in enumerate(tools,1):
            self.f.events.append(dict(eventId='e'+str(n),requestId=REQUEST,attemptId='attempt-1',sequence=n,occurredAt=timestamp(START+1),kind=kind,summary='safe',details=dict(tool=tool,status=status,runtimeSequence=n),evidenceMode='live'))
        self.addCleanup(ingress._close_origins)
    def test_actual_read_between_gets_seals_typed_same_wait_proof(self):
        import segmented_capture
        calls=[];http=self.f.http;read=segmented_capture.read_frame
        def get(*args):
            if args[1]=='/v1/requests/'+self.f.execution['requestId']:calls.append('get')
            return http(*args)
        def frame(*args,**kwargs):calls.append('frame');return read(*args,**kwargs)
        self.f.http=get
        with patch.object(segmented_capture,'read_frame',side_effect=frame):record=ingress.seal_ingress(self.f.run,self.f.read())['record']
        witness,pair=ingress.validate_withdrawal_witness(record,ingress._Chain(self.f.run))
        self.assertEqual(pair,['c','d']);self.assertEqual(witness['before'],witness['after'])
        gets=[i for i,c in enumerate(calls) if c=='get'];self.assertTrue(any(c=='frame' for c in calls[gets[0]+1:gets[1]]))
    def test_resume_race_and_false_cleanup_refuse(self):
        http=self.f.http;count=[0]
        def get(*args):
            if args[1]=='/v1/requests/'+self.f.execution['requestId']:
                count[0]+=1
                if count[0]>=2:self.f.execution['status']='queued'
            return http(*args)
        self.f.http=get
        with self.assertRaises(ValueError):self.f.read()
    def test_prior_restart_any_attempt_refuses(self):
        event=self.f.events[0];event['details']['tool']='mcp__infra__deployment_restart';event['attemptId']='older'
        with self.assertRaises(ValueError):self.f.read()
    def test_changed_ended_attempt_event_prefix_refuses(self):
        http=self.f.http;count=[0]
        def get(*args):
            if args[1]=='/v1/requests/'+self.f.execution['requestId']:
                count[0]+=1
                if count[0]==2:
                    extra=copy.deepcopy(self.f.events[-1]);extra.update(eventId='extra',sequence=5);extra['details']['runtimeSequence']=5
                    self.f.events.append(extra)
            return http(*args)
        self.f.http=get
        with self.assertRaises(ValueError):self.f.read()


def native_complete_fixture(test,guest_skew=0):
    """Real frames, two issued causal wait artifacts and full final boundary."""
    from test_pgcat_causal_integration import bind_fixture_policy,causal_fixture
    bind_fixture_policy(test)
    from test_pgcat_native_ingress import Fixture,START,timestamp,REQUEST,APPROVAL,write,file_record
    from test_pgcat_native_evidence import rolling,kubernetes
    from test_pgcat_native_final import FinalActionTests
    import datetime
    tmp=tempfile.TemporaryDirectory();test.addCleanup(tmp.cleanup)
    root=Path(tmp.name).resolve();root.chmod(0o700);f=Fixture(root);original=f.append
    def append(n,value=None):
        value=copy.deepcopy(value or f.frame);met=copy.deepcopy(f.frame['metrics'])
        ids=['a','b','c','d'] if n<5 else ['c','d'];met['nodes']=ids
        met['raw']=[dict(node=node,pool='test',user='poc_app',kind=k,value=int(node=='a' and k=='idle'),sourceAt=START+n) for node in ids for k in ('active','idle','waiting')]
        value['metrics']=met
        if n in (2,3,4):value['inventory'].update(desired=4,members=['a','b','c','d'],healthyRegistered=['c','d'],memberStates=[dict(id=node,lifecycle='Protected',health='Healthy',addressKnown=True) for node in ('a','b','c','d')],backendStates=[dict(id=node,status='Available',weight=100,listenerHealth='Normal') for node in ('c','d')])
        original(n,value)
    f.append=append;append(2)
    def event(n,tool,kind,status,at,attempt='attempt-1'):
        return dict(eventId='e'+str(n),requestId=REQUEST,attemptId=attempt,sequence=n,occurredAt=datetime.datetime.fromtimestamp(START+at+guest_skew,datetime.timezone.utc).isoformat().replace('+00:00','Z'),kind=kind,summary='safe',details=dict(tool=tool,status=status,runtimeSequence=n),evidenceMode='live')
    f.events=[event(1,'mcp__infra__db_proxy_scale','tool_start','started',1.5),event(2,'mcp__infra__db_proxy_nodes_deregister','tool_start','started',1.6),event(3,'mcp__infra__db_proxy_nodes_deregister','tool_result','succeeded',1.7),event(4,'resource_wait','tool_start','started',1.8),event(5,'resource_wait','tool_result','succeeded',1.9)]
    f.execution.update(status='waiting_resource',stage='waiting_resource',cleanupStatus=dict(sandboxAbsent=True,keyAbsent=True,accessRevoked=True))
    ingress.seal_ingress(f.run,f.read())
    for n in (3,4):append(n,rolling(n-1))
    f.live=kubernetes(3)
    f.events += [event(6,'mcp__infra__deployment_restart','tool_start','started',2.5,'attempt-2'),event(7,'mcp__raptor__approval_request','tool_start','started',3.8,'attempt-2'),event(8,'mcp__raptor__approval_request','tool_result','succeeded',3.9,'attempt-2')]
    f.execution.update(status='waiting_approval',stage='waiting_approval',attemptId='attempt-2')
    http=f.http
    approval=dict(approvalId=APPROVAL,requestId=REQUEST,actionId='shrink',state='pending',guidance=None,binding=dict(requestId=REQUEST,actionId='shrink',interface='db-proxy.scale-in',envCode='rdev.ali',target=dict(proxyCode='DP00001'),parameters=dict(desiredCapacity=2)))
    f.http=lambda origin,path,auth:canonical(dict(data=approval)) if path=='/v1/approvals/'+APPROVAL else http(origin,path,auth)
    f.read=lambda:ingress.read_ingress(f.run,dict(requestId=REQUEST,approvalIds=[APPROVAL]),gate=f,kube_read=f.kube,http_get=f.http,now=lambda:f.now)
    with causal_fixture(f):ingress.seal_ingress(f.run,f.read())
    value=rolling(4);value['inventory'].update(desired=2,members=['c','d'],healthyRegistered=['c','d'],memberStates=[dict(id=node,lifecycle='Protected',health='Healthy',addressKnown=True) for node in ('c','d')],backendStates=[dict(id=node,status='Available',weight=100,listenerHealth='Normal') for node in ('c','d')]);append(5,value)
    replacement=dict(version=2,converged=True,acceptance='pending',scope=copy.deepcopy(f.plan['controllerScope']),oldInstanceIds=['a','b'],newInstanceIds=['c','d'],desiredCapacity=2,restartGeneration=2,actions=[dict(actionId='expand',previousDesiredCapacity=2,desiredCapacity=4,outcome='submitted'),dict(actionId='shrink',previousDesiredCapacity=4,desiredCapacity=2,outcome='unknown')],observedAt=timestamp(f.now+guest_skew))
    f.events += [event(9,'mcp__infra__db_proxy_scale','tool_start','started',4.5,'attempt-3')];approval['state']='approved'
    f.execution.update(status='completed',stage='finished',attemptId='attempt-3',cleanup='confirmed',result=dict(requestId=REQUEST,attemptId='attempt-3',selectedModel='gpt-6-luna',actualModel='gpt-6-luna',selectedProvider='codex',actualProvider='openai-codex',answer='Replacement converged; retained traffic and approval assessment is pending.',evidence=[],usage=[],generatedAt=timestamp(f.now+guest_skew),replacement=replacement))
    helper=FinalActionTests();test.addCleanup(helper.doCleanups);helper.fixture=lambda:f;bound=helper.bound;captured=[]
    def keep():
        actual=ingress.seal_operation_binding
        with patch.object(ingress,'seal_operation_binding',side_effect=lambda run,path:actual(run,run/'native-ingress-0003.json')):result=bound()
        captured.append(result);return result
    helper.bound=keep;helper.test_finish_matches_real_boundary_and_all_four_finals_once()
    f,a=captured[0];f.writer.finish(completed=True,cancelled=False,errors=[])
    supports=dict(a.supports);supports['native-finish']=dict(basename='native-finish.json',identity=file_record(f.run/'native-finish.json'))
    records=[dict(basename=p.name,identity=file_record(p)) for p in sorted(f.run.glob('native-ingress-*.json'))]
    write(f.run/'native-assessment-input.json',dict(operation=a.operation,supports=supports,ingresses=records))
    job=f.run/'collector-job';job.mkdir(mode=0o700);write(job/'intent.json',dict(runId=f.plan['runId'],startedAt=START,profile=f.plan['mode'],captureMaxSeconds=5400,prepared=a.prepared))
    from pgcat_collector_control import completion
    digest=completion(f.run,profile=f.plan['mode']);write(job/'exit.json',dict(identity=f.context['collectorIdentity'],exitCode=0,category='completed',completionSha256=digest))
    ingress._close_origins()
    return f,a


class AssessmentTests(unittest.TestCase):
    def test_full_native_and_guardian_absence_are_independent(self):
        from pgcat_native_assessment import assess_native
        f,a=native_complete_fixture(self)
        with patch('pgcat_collector_control.process_identity',return_value=None):result=assess_native(f.run)
        self.assertEqual(result['status'],'PASS',result)
        with patch('pgcat_collector_control.process_identity',side_effect=lambda pid:f.guardian if pid==f.guardian['pid'] else None):bad=assess_native(f.run)
        self.assertEqual(bad['status'],'INCONCLUSIVE');self.assertFalse(bad['guardianAbsent'])
    def test_missing_approval_or_causal_witness_never_upgrades(self):
        from pgcat_native_assessment import assess_native
        f,a=native_complete_fixture(self)
        path=f.run/'native-ingress-0001.json';path.unlink()
        with patch('pgcat_collector_control.process_identity',return_value=None):result=assess_native(f.run)
        self.assertEqual(result['status'],'INCONCLUSIVE')


def diagnostic_complete_fixture(test):
    from test_pgcat_causal_integration import bind_fixture_policy
    bind_fixture_policy(test)
    from test_pgcat_native_ingress import Fixture,DiagnosticTests as DisabledRestartTests,START,write,file_record
    from test_pgcat_native_evidence import rolling,kubernetes
    from test_pgcat_native_final import FinalActionTests
    tmp=tempfile.TemporaryDirectory();test.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
    f=Fixture(root,'disabled-proof');restart=DisabledRestartTests();restart.f=f;restart.restart()
    for n in range(2,6):
        value=rolling(n-1);value['inventory']=copy.deepcopy(f.frame['inventory']);f.append(n,value)
    f.live=kubernetes(4)
    helper=FinalActionTests();test.addCleanup(helper.doCleanups);helper.fixture=lambda:f
    bound=helper.bound;captured=[]
    def keep():
        result=bound();captured.append(result);return result
    helper.bound=keep;helper.test_finish_matches_real_boundary_and_all_four_finals_once()
    f,a=captured[0];f.writer.finish(completed=True,cancelled=False,errors=[])
    supports=dict(a.supports);supports['native-finish']=dict(basename='native-finish.json',identity=file_record(f.run/'native-finish.json'))
    records=[dict(basename=p.name,identity=file_record(p)) for p in sorted(f.run.glob('native-ingress-*.json'))]
    write(f.run/'native-assessment-input.json',dict(operation=a.operation,supports=supports,ingresses=records))
    job=f.run/'collector-job';job.mkdir(mode=0o700);write(job/'intent.json',dict(runId=f.plan['runId'],startedAt=START,profile=f.plan['mode'],captureMaxSeconds=5400,prepared=a.prepared))
    from pgcat_collector_control import completion
    digest=completion(f.run,profile=f.plan['mode']);write(job/'exit.json',dict(identity=f.context['collectorIdentity'],exitCode=0,category='completed',completionSha256=digest))
    ingress._close_origins()
    return f,a


class NativeBoundaryProducerReaderTests(unittest.TestCase):
    def test_actual_collector_v2_boundary_reaches_reader_for_both_execution_kinds(self):
        import json
        for kind,fixture in [('native-replacement',native_complete_fixture),('disabled-proof',diagnostic_complete_fixture)]:
            with self.subTest(kind=kind):
                f,a=fixture(self)
                start=json.loads((f.run/'native-boundary'/'boundary-start.json').read_text())
                completed=json.loads((f.run/'native-boundary'/'boundary-completed.json').read_text())
                self.assertEqual(start['schemaVersion'],2);self.assertEqual(start['executionKind'],kind)
                self.assertNotIn('originalRosterSha256',start)
                self.assertEqual(start['terminalSha256'],completed['terminalSha256'])
                self.assertTrue((f.run/'native-finish.json').exists())
                from pgcat_collector_control import native_boundary_completion
                from pgcat_native_evidence import read_terminal_clients
                from segmented_capture import read_progress
                from test_pgcat_native_ingress import write
                import time
                operation=json.loads((f.run/'native-operation-binding.json').read_text())
                terminal=read_terminal_clients(f.run/'native-capture',f.context,operation)
                progress,_,_=read_progress(f.run/'native-capture',f.context)
                inventory=json.loads((f.run/'provider-inventory.json').read_text())
                def consume():return native_boundary_completion(f.run,f.context,operation,terminal,inventory,progress,started=0,deadline=time.monotonic()+5)
                consume()
                for case in ('legacy-v1','wrong-kind','wrong-terminal','hybrid'):
                    bad=copy.deepcopy(start)
                    if case=='legacy-v1':
                        bad['schemaVersion']=1;del bad['terminalSha256'];del bad['executionKind'];bad['originalRosterSha256']=sha(canonical(terminal['roster']))
                    if case=='wrong-kind':bad['executionKind']='disabled-proof' if kind=='native-replacement' else 'native-replacement'
                    if case=='wrong-terminal':bad['terminalSha256']='0'*64
                    if case=='hybrid':bad['originalRosterSha256']=sha(canonical(terminal['roster']))
                    write(f.run/'native-boundary'/'boundary-start.json',bad)
                    write(f.run/'native-boundary'/'boundary-completed.json',dict(completed,startSha256=sha(canonical(bad))))
                    with self.subTest(case=case),self.assertRaises(ValueError):consume()
                write(f.run/'native-boundary'/'boundary-start.json',start);write(f.run/'native-boundary'/'boundary-completed.json',completed)


class DiagnosticAssessmentTests(unittest.TestCase):
    def test_constant_two_actual_restart_proof_can_assess_diagnostic_only(self):
        from pgcat_native_assessment import assess_native
        f,a=diagnostic_complete_fixture(self)
        with patch('pgcat_collector_control.process_identity',return_value=None):result=assess_native(f.run)
        self.assertEqual(result['status'],'PASS',result);self.assertEqual(result['executionKind'],'disabled-proof')


def restored_observations(f,n=2,final=False):
    from test_pgcat_native_evidence import kubernetes
    from test_capture_window import frame
    from test_pgcat_native_ingress import START
    live=kubernetes(4);live['deployment']['status'].update(replicas=2,updatedReplicas=2,availableReplicas=2);rows=[]
    for i,p in enumerate(live['pods']):
        row=frame(START+129,n)['pods'][0];row.update(uid=p['metadata']['uid'],name=p['metadata']['name'],containerId='restored-container-'+str(i),ready=True)
        row['snapshot']['sample']['processId']='restored-process-'+str(i);row['snapshot']['final']=final
        for e in row['snapshot']['events']:e['processId']=row['snapshot']['sample']['processId'];e['at']+=START+125
        rows.append(row);p['status']['containerStatuses'][0]['containerID']=row['containerId']
    live['roster']=[dict(name=r['name'],uid=r['uid'],containerId=r['containerId'],processId=r['snapshot']['sample']['processId']) for r in rows]
    return live,rows


class RestorationHistoryTests(unittest.TestCase):
    def test_new_advancing_process_requires_complete_actual_history(self):
        f,a=diagnostic_complete_fixture(self);live,rows=restored_observations(f)
        def read(p,*args,**kwargs):return copy.deepcopy(next(r for r in rows if r['uid']==p['metadata']['uid']))
        with patch.object(a.reads,'live_clients',return_value=live),patch('worker_capture.read_pod',side_effect=read):
            self.assertEqual(closure._restoration_frame(a,lambda:__import__('test_pgcat_native_ingress').START+129)['roster'],live['roster'])
            rows[0]['snapshot']['events']=[]
            with self.assertRaises(ValueError):closure._restoration_frame(a,lambda:__import__('test_pgcat_native_ingress').START+129)
    def test_ready_but_final_is_not_restored_and_changed_owner_refuses(self):
        f,a=diagnostic_complete_fixture(self);live,rows=restored_observations(f,final=True)
        def read(p,*args,**kwargs):return copy.deepcopy(next(r for r in rows if r['uid']==p['metadata']['uid']))
        with patch.object(a.reads,'live_clients',return_value=live),patch('worker_capture.read_pod',side_effect=read):
            with self.assertRaises(ValueError):closure._restoration_frame(a,lambda:__import__('test_pgcat_native_ingress').START+129)


class FailurePrecedenceTests(unittest.TestCase):
    def test_all_four_sql_failures_survive_missing_late_segment(self):
        import json
        from segmented_capture import SegmentedCapture
        from pgcat_native_assessment import assess_native
        for index in range(4):
            f,a=native_complete_fixture(self)
            frames=[json.loads(line) for p in sorted((f.run/'native-capture').glob('segment-*.jsonl')) for line in p.read_text().splitlines()]
            ids=sorted({p['uid'] for frame in frames for p in frame['pods']});target=ids[index]
            old=f.run/'native-capture';old.rename(f.run/'source-capture')
            writer=SegmentedCapture(old,max_segment_seconds=2)
            marked=False
            for frame in frames:
                for pod in frame['pods']:
                    sample=pod['snapshot']['sample']
                    if pod['uid']==target and sample['Attempts']:
                        sample['Failure']=1;sample['Success']-=1
                        if not marked:
                            ops=[e for e in pod['snapshot']['events'] if e['kind']=='operation']
                            if ops:ops[0]['outcome']='failure';marked=True
                writer.append(frame)
            writer.finish(completed=True,cancelled=False,errors=[])
            last=sorted(old.glob('segment-*.jsonl'))[-1];last.unlink()
            with self.subTest(cohort=target):self.assertEqual(assess_native(f.run)['status'],'FAIL')


class AbortPathsTests(unittest.TestCase):
    def test_partial_phases_attempt_both_original_cleanups_and_keep_unknown(self):
        import subprocess,sys,uuid
        import pgcat_native_controller as controller
        for phase in ('before-feed','after-feed','after-restart','cleanup-fault'):
            tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
            mac=root/'mac';worker=root/'worker';mac.mkdir(mode=0o700);worker.mkdir(mode=0o700)
            for parent in (mac,worker):(parent/'kubeconfig').write_bytes(b'only-offline-material');(parent/'kubeconfig').chmod(0o600)
            owner=object.__new__(controller.NativeController);owner.run_id=str(uuid.uuid4());owner.prepared=None;owner.phase=phase;owner.client=None
            owner.mac_credential=controller.credential_identity(mac/'kubeconfig');owner.worker_credential=controller.credential_identity(worker/'kubeconfig')
            owner.bindings=dict(captureRoot=str(root),workerKubeconfig=str(worker/'kubeconfig'),laneBindings=dict(executionKind='native-replacement'))
            owner.worker_processes={};owner.collector_attempted=phase=='after-restart';owner.feed=None
            if phase in ('after-feed','after-restart'):
                owner.feed=subprocess.Popen([sys.executable,'-c','import time;time.sleep(20)'])
                self.addCleanup(lambda child=owner.feed:child.kill() if child.poll() is None else None)
                owner.feed_identity=dict(runId=owner.run_id,feedId=str(uuid.uuid4()),pid=owner.feed.pid)
            controller._native_owners[owner]=dict(pid=os.getpid(),bindings=canonical(owner.bindings),runId=owner.run_id,mac=copy.deepcopy(owner.mac_credential),worker=copy.deepcopy(owner.worker_credential),prepared=None,feed=owner.feed,feedIdentity=getattr(owner,'feed_identity',None),client=None,workerProcesses={})
            calls=[]
            def remote(verb,payload):
                calls.append(verb)
                if verb=='ordinary-status':return dict(status='FAIL' if phase=='after-restart' else 'INCONCLUSIVE')
                if verb=='credential-cleanup':
                    if phase=='cleanup-fault':raise ValueError('actual offline transport fault')
                    return closure.cleanup_bound_credential(worker/'kubeconfig',payload)
                if verb=='credential-absent':return dict(absent=not (worker/'kubeconfig').exists())
                raise ValueError('unavailable')
            with patch.object(controller,'_require_verified'),patch.object(owner,'_owned_worker',side_effect=remote):result=closure.abort_owned(root,adapters=owner,now=lambda:0)
            self.assertFalse((mac/'kubeconfig').exists());self.assertEqual(calls.count('credential-cleanup'),1)
            self.assertEqual(result['status'],'FAIL' if phase=='after-restart' else 'INCONCLUSIVE')
            self.assertEqual(result['reservationState'],'unresolved');self.assertFalse((root/'native-close.json').exists())
            if phase=='after-restart':
                rows=[r for r in result['ownedProcesses'] if r['role']=='collector'];self.assertEqual(rows,[dict(role='collector',identity=None,state='unknown')])
                self.assertTrue((root/'native-abort-faults.json').exists())
            if owner.feed is not None:self.assertIsNotNone(owner.feed.poll())


class CredentialSubstitutionTests(unittest.TestCase):
    def test_replaced_inode_symlink_hardlink_and_missing_unproven_refuse(self):
        from pgcat_native_controller import credential_identity
        for mode in ('inode','symlink','hardlink','missing'):
            tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
            path=root/'kubeconfig';path.write_bytes(b'owned');path.chmod(0o600);original=credential_identity(path)
            if mode=='inode':path.rename(root/'old');path.write_bytes(b'owned');path.chmod(0o600)
            if mode=='symlink':path.rename(root/'old');path.symlink_to(root/'old')
            if mode=='hardlink':os.link(path,root/'other')
            if mode=='missing':path.unlink()
            with self.subTest(mode=mode),self.assertRaises(ValueError):closure.cleanup_bound_credential(path,original)


def restoration_provider_record(f,a,clock):
    from segmented_capture import sha,canonical
    reconcile=__import__('json').loads((f.run/'native-reconcile.json').read_text())
    finished=__import__('json').loads((f.run/'native-finish.json').read_text())
    inventory=dict(copy.deepcopy(f.baseline['fleet']),schemaVersion=2,observedAt=f.now,privateAddresses={r['id']:r['address'] for r in f.baseline['originals']})
    return dict(schemaVersion=1,runId=f.plan['runId'],executionKind=f.plan['executionKind'],planSha256=sha(canonical(f.plan)),scope=f.plan['target'],pair=reconcile['outputs']['pair'],
        stopSha256=sha(canonical(a.stopped['support'])),reconcileSha256=sha(canonical(reconcile)),finishSha256=sha(canonical(finished)),feedBindingSha256=sha(canonical(a.stopped['feedBinding'])),
        observedAt=f.now,completedAt=f.now,startedMonotonic=clock,completedMonotonic=clock,holdNotBeforeMonotonic=clock-.1,
        capacity=dict(desiredCapacity=2,minSize=2,maxSize=4),inventory=inventory)


class RestoreKernelTests(unittest.TestCase):
    def test_actual_diagnostic_false_idle_and_posthold_two_advancing_reads(self,provider_case=None):
        import json
        from test_pgcat_native_ingress import file_record,START
        f,a=diagnostic_complete_fixture(self)
        inputs=json.loads((f.run/'native-assessment-input.json').read_text());(f.run/'native-assessment-input.json').unlink();a.ingresses=inputs['ingresses'];a.supports=inputs['supports'];a.action_id='dddddddd-dddd-4ddd-8ddd-dddddddddddd'
        with patch.object(closure,'_source',return_value={}),patch('pgcat_collector_control.process_identity',return_value=None):
            closure.verdict(f.run,adapters=a,now=lambda:f.now)
            a.supports['native-verdict']=closure.support_artifact(f.run,'native-verdict',adapters=a)
        g=object.__new__(closure.FixedGatewayAdapters);g.run=f.run;g.run_id=f.plan['runId'];g.plan=a.plan;g.prepared=a.prepared;g.credential=a.credential;g.reads=a.reads;g.action_id='eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee';g.gate=f.run/'gateway-live-gate'
        a.action_id=g.action_id;a.gateway=g
        hold=json.loads((f.run/'native-finish-hold.json').read_text());clock=hold['notBeforeMonotonic']+.1;a.deadline=clock+4;a.reads.deadline=a.deadline
        f.now=START+129;live,rows=restored_observations(f)
        cm=dict(metadata=copy.deepcopy(f.baseline['configMap']),data={'targets.json':closure._targets({'a':'10.1.0.1','b':'10.1.0.2'}).decode()})
        meta={k:f.runtime[k] for k in ('secretUid','secretResourceVersion','deploymentUid','podUid','generation','imageDigest')};meta.update(deploymentResourceVersion='1',replacementEnabled=False)
        calls=[];reads=[0]
        def command(args,*rest):
            calls.append(args)
            if args[0]==str(g.gate):self.assertEqual(args[1:],['inspect','--expected','false']);return canonical(f.runtime['verification'])
            self.assertEqual(args[-1],'--idle-check');return b'idle=true\n'
        def pod(p,*args,**kwargs):
            n=2+reads[0]//2;reads[0]+=1;_,current=restored_observations(f,n=n)
            return copy.deepcopy(next(r for r in current if r['uid']==p['metadata']['uid']))
        def perform(provider_stage=None):
            from test_pgcat_native_ingress import write
            if provider_stage is None and provider_case!='missing':
                record=restoration_provider_record(f,a,clock)
                if provider_case=='protection':record['inventory']['memberStates'][0]['lifecycle']='InService'
                if provider_case=='health':record['inventory']['memberStates'][0]['health']='Unhealthy'
                if provider_case=='backend':record['inventory']['healthyRegistered'].pop()
                if provider_case=='membership':record['inventory']['members'][0]='foreign'
                if provider_case in ('desiredCapacity','minSize','maxSize'):record['capacity'][provider_case]=3
                if provider_case=='address':record['inventory']['privateAddresses']['a']='10.9.9.9'
                if provider_case=='scope':record['scope']=dict(record['scope'],accountId='foreign')
                if provider_case=='future':record['completedAt']+=1
                if provider_case=='stale':
                    record['observedAt']-=31;record['completedAt']-=31;record['inventory']['observedAt']-=31
                write(f.run/'native-restoration-provider.json',record)
                a.restoration_provider=dict(basename='native-restoration-provider.json',identity=file_record(f.run/'native-restoration-provider.json'))
                if provider_case=='substituted':
                    path=f.run/'native-restoration-provider.json';path.rename(f.run/'old-provider.json');write(path,record)
            with patch.object(closure,'_source',return_value={}),patch.object(closure,'_worker_boot',return_value='boot'),patch.object(closure.time,'monotonic',return_value=clock),patch.object(closure.time,'time',return_value=f.now),patch.object(g,'_metadata',return_value=meta),patch.object(g,'_command',side_effect=command),patch.object(g,'_check'),patch.object(a.reads,'read',return_value=cm),patch.object(a.reads,'live_clients',return_value=live),patch('worker_capture.read_pod',side_effect=pod):
                if provider_stage is not None:a.restoration_provider=provider_stage()['workerArtifact']
                result=closure.restore(f.run,adapters=a,now=lambda:f.now)
            self.assertEqual(result['outputs']['roster'],live['roster']);self.assertEqual(reads[0],4)
            self.assertTrue((f.run/'native-restoration-frames.json').exists());self.assertTrue(all('set' not in c and 'rollout' not in c for c in calls))
            return result
        if provider_case=='defer':return perform
        perform()


class FreshRestorationProviderTests(unittest.TestCase):
    def test_drift_stale_foreign_or_substituted_provider_cannot_restore(self):
        for case in ('protection','health','backend','membership','desiredCapacity','minSize','maxSize','address','scope','stale','future','substituted'):
            helper=RestoreKernelTests();self.addCleanup(helper.doCleanups)
            with self.subTest(case=case),self.assertRaises(ValueError):helper.test_actual_diagnostic_false_idle_and_posthold_two_advancing_reads(provider_case=case)

    def test_missing_posthold_provider_read_cannot_restore(self):
        helper=RestoreKernelTests();self.addCleanup(helper.doCleanups)
        with self.assertRaises(ValueError):helper.test_actual_diagnostic_false_idle_and_posthold_two_advancing_reads(provider_case='missing')


class RetirementWitnessInterfaceTests(unittest.TestCase):
    def test_retirement_witness_validator_exists(self):
        self.assertTrue(callable(getattr(ingress,'validate_retirement_witness',None)))


class RetirementWitnessTests(unittest.TestCase):
    def test_bound_pending_approval_frame_and_strict_safe_projection(self):
        import json
        f,a=native_complete_fixture(self);record=json.loads((f.run/'native-ingress-0002.json').read_text());chain=ingress._Chain(f.run)
        witness,pair=ingress.validate_retirement_witness(record,chain)
        self.assertEqual(pair,['c','d']);self.assertEqual(witness['actionId'],'shrink');self.assertEqual(record['approvals'][0]['state'],'pending')
        for case in ('action','cleanup','early-frame','denied','second-scale'):
            bad=copy.deepcopy(record)
            if case=='action':bad['retirementWitness']['actionId']='foreign'
            if case=='cleanup':bad['retirementWitness']['after']['cleanupStatus']['keyAbsent']=False
            if case=='early-frame':bad['retirementWitness']['frame']=json.loads((f.run/'native-ingress-0001.json').read_text())['withdrawalWitness']['frame']
            if case=='denied':bad['approvals'][0]['state']='denied'
            if case=='second-scale':
                extra=copy.deepcopy(bad['events'][0]);extra.update(eventId='extra',sequence=9);bad['events'].append(extra);bad['retirementWitness'].update(eventCursor=9,eventsSha256=sha(canonical(bad['events'])))
            with self.subTest(case=case),self.assertRaises(ValueError):ingress.validate_retirement_witness(bad,chain)




class GlobalAuditTests(unittest.TestCase):
    def test_only_finished_absent_owned_supervisor_releases_exact_reservation(self):
        import subprocess,sys,uuid
        from types import SimpleNamespace
        import pgcat_lane_supervisor as supervisor
        from pgcat_native_controller import NativeController
        from test_pgcat_native_ingress import write
        for foreign in (True,False):
            tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
            global_root=root/'global';global_root.mkdir(mode=0o700);lock=global_root/'owner.lock';lock.write_bytes(b'');lock.chmod(0o600)
            child=subprocess.Popen([sys.executable,'-c','pass']);child.wait(timeout=3)
            owner=object.__new__(NativeController);owner.bindings=dict(captureRoot=str(root));owner.prepared=dict(plan=plan())
            sid=str(uuid.uuid4());lane=root/'lane';lane.mkdir(mode=0o700)
            marker=dict(runId='foreign' if foreign else owner.prepared['plan']['runId'],supervisorId=sid,laneRoot=str(lane));write(global_root/'active-run',marker)
            exited=dict(runId=owner.prepared['plan']['runId'],feedId=str(uuid.uuid4()),pid=child.pid,exitCode=0)
            owner.client=SimpleNamespace(process=child,supervisor_id=sid,root=lane,journal=SimpleNamespace(run_snapshot=lambda:dict(runState='finished'),native_stop=lambda:dict(feedExit=exited)))
            with patch.object(owner,'_require_owner'),patch.object(supervisor,'GLOBAL_ROOT',global_root):
                if foreign:
                    with self.assertRaises(ValueError):closure.audit_global_completion(owner)
                    self.assertTrue((global_root/'active-run').exists())
                else:
                    record,identity=closure.audit_global_completion(owner)
                    self.assertTrue(record['reservationAbsent']);self.assertTrue(record['supervisorAbsent']);self.assertFalse((global_root/'active-run').exists())


class AbortOriginTests(unittest.TestCase):
    def test_shaped_or_fork_inherited_controller_cannot_authorize_cleanup(self):
        import pgcat_native_controller as controller
        owner=object.__new__(controller.NativeController)
        with patch.object(controller,'_require_verified'):
            with self.assertRaises(ValueError):owner._require_abort_owner()
            controller._native_owners[owner]=dict(pid=os.getpid()+1)
            with self.assertRaises(ValueError):owner._require_abort_owner()

    def test_worker_substitution_refuses_without_adopting_current_material(self):
        import pgcat_native_controller as controller
        owner=object.__new__(controller.NativeController);owner.worker_credential=dict(path='/foreign/kubeconfig')
        actual=dict(path='/owned/kubeconfig')
        controller._native_owners[owner]=dict(pid=os.getpid(),bindings=canonical(dict(workerKubeconfig=actual['path'])),runId='run',mac={},worker=actual,prepared=None,feed=None,feedIdentity=None,client=None,workerProcesses={})
        with patch.object(controller,'_require_verified'),patch.object(owner,'_owned_worker') as transport:
            with self.assertRaises(ValueError):owner._cleanup_worker()
            transport.assert_not_called()


class AbortReviewTests(unittest.TestCase):
    def owner(self,root,*,client=None,prepared=None):
        import uuid
        import pgcat_native_controller as controller
        paths=[]
        for name in ('abort-mac','abort-worker'):
            path=root/name;path.mkdir(mode=0o700);path=path/'kubeconfig'
            path.write_bytes(b'offline-owned-material');path.chmod(0o600);paths.append(path)
        owner=object.__new__(controller.NativeController)
        owner.run_id=str(uuid.uuid4()) if prepared is None else prepared['plan']['runId']
        owner.phase='partial-start';owner.collector_attempted=False;owner.prepared=prepared
        owner.mac_credential=controller.credential_identity(paths[0]);owner.worker_credential=controller.credential_identity(paths[1])
        owner.bindings=dict(captureRoot=str(root),workerKubeconfig=str(paths[1]),laneRoot=str(root/'lane'),laneBindings=dict(executionKind='native-replacement'))
        owner.client=client;owner.feed=None;owner.worker_processes={}
        controller._native_owners[owner]=dict(pid=os.getpid(),bindings=canonical(owner.bindings),runId=owner.run_id,
            mac=copy.deepcopy(owner.mac_credential),worker=copy.deepcopy(owner.worker_credential),prepared=None if prepared is None else canonical(prepared),
            feed=None,feedIdentity=None,client=client,workerProcesses={})
        return owner,paths

    def remote(self,paths,calls,*,recovered=None):
        def perform(verb,payload):
            calls.append((verb,payload))
            if verb=='ordinary-status':raise TimeoutError('offline worker timeout')
            if verb=='process-identities':return recovered
            if verb=='process-stop':return dict(state='stopped')
            if verb=='credential-cleanup':return closure.cleanup_bound_credential(paths[1],payload)
            if verb=='credential-absent':return dict(absent=not paths[1].exists())
            raise ValueError('unavailable')
        return perform

    def test_unbound_foreign_sidecar_never_grants_stop_authority(self):
        import pgcat_native_controller as controller
        from pgcat_collector_control import valid_identity
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
        owner,paths=self.owner(root,prepared=dict(plan=dict(runId='owned-run'),planFile={},baselineFile={},gatewayRuntimeFile={},workerCredential={}))
        owner.collector_attempted=True
        foreign=dict(pid=12345,bootId='foreign-boot',startTicks=456,commandSha256='a'*64);valid_identity(foreign)
        calls=[]
        with patch.object(controller,'_require_verified'),patch.object(owner,'_owned_worker',side_effect=self.remote(paths,calls,recovered=dict(guardian=foreign))):
            result=closure.abort_owned(root,adapters=owner,now=lambda:0)
        self.assertFalse(any(verb=='process-stop' for verb,_ in calls))
        self.assertEqual([r for r in result['ownedProcesses'] if r['role']=='guardian'],[dict(role='guardian',identity=None,state='unknown')])
        self.assertEqual(controller._native_owners[owner]['workerProcesses'],{})

    def test_retained_authenticated_fail_survives_worker_timeout_and_both_cleanup(self):
        from test_pgcat_native_final import JournalClosureTests
        from test_pgcat_native_ingress import file_record,write
        from pgcat_lane_client import LaneClient
        import pgcat_native_controller as controller
        root,f,j,_,_=JournalClosureTests.journal(self)
        support=dict(schemaVersion=1,action='native-verdict',runId=f.plan['runId'],executionKind=f.plan['executionKind'],planSha256=sha(canonical(f.plan)),
            requestId='33333333-3333-4333-8333-333333333333',inputs=dict(stopSha256='a'*64,finishSha256='b'*64),outputs=dict(assessmentSha256='c'*64,status='FAIL'))
        write(f.run/'native-verdict.json',support)
        proof=dict(action='native-verdict',requestId=support['requestId'],outputs=dict(support=support,artifact=dict(basename='native-verdict.json',identity=file_record(f.run/'native-verdict.json'))))
        with j._transaction() as db:db.execute('INSERT INTO native_support VALUES (?,?)',(support['requestId'],canonical(proof).decode()))
        client=LaneClient();client.journal=j;client.root=j.root;client.liveness_fd=None;client.process=None
        owner,paths=self.owner(root,client=client,prepared=dict(f.prepared,plan=f.plan));calls=[]
        with patch.object(controller,'_require_verified'),patch.object(owner,'_owned_worker',side_effect=self.remote(paths,calls)):
            result=closure.abort_owned(root,adapters=owner,now=lambda:0)
        self.assertEqual(result['status'],'FAIL')
        self.assertEqual([verb for verb,_ in calls].count('credential-cleanup'),1)
        self.assertTrue(all(not path.exists() for path in paths))

    def test_unanchored_fail_and_changed_original_artifact_cannot_supply_verdict(self):
        from test_pgcat_native_final import JournalClosureTests
        from test_pgcat_native_ingress import file_record,write
        from pgcat_lane_client import LaneClient
        import pgcat_native_controller as controller
        import sqlite3
        for case in ('unanchored','changed-artifact','foreign-original-plan','journal-read-failure'):
            with self.subTest(case=case):
                root,f,j,_,_=JournalClosureTests.journal(self)
                support=dict(schemaVersion=1,action='native-verdict',runId=f.plan['runId'],executionKind=f.plan['executionKind'],planSha256=sha(canonical(f.plan)),
                    requestId='33333333-3333-4333-8333-333333333333',inputs=dict(stopSha256='a'*64,finishSha256='b'*64),outputs=dict(assessmentSha256='c'*64,status='FAIL'))
                write(f.run/'native-verdict.json',support);write(root/'native-assessment.json',dict(status='FAIL'))
                artifact=dict(basename='native-verdict.json',identity=file_record(f.run/'native-verdict.json'))
                if case=='changed-artifact':artifact['identity']['sha256']='d'*64
                if case!='unanchored':
                    proof=dict(action='native-verdict',requestId=support['requestId'],outputs=dict(support=support,artifact=artifact))
                    with j._transaction() as db:db.execute('INSERT INTO native_support VALUES (?,?)',(support['requestId'],canonical(proof).decode()))
                original_plan=copy.deepcopy(f.plan)
                if case=='foreign-original-plan':original_plan['runId']='44444444-4444-4444-8444-444444444444'
                client=LaneClient();client.journal=j;client.root=j.root;client.liveness_fd=None;client.process=None
                owner,paths=self.owner(root,client=client,prepared=dict(f.prepared,plan=original_plan));calls=[]
                read=sqlite3.DatabaseError('offline journal read failure') if case=='journal-read-failure' else j._native_record_in_tx
                with patch.object(controller,'_require_verified'),patch.object(j,'_native_record_in_tx',side_effect=read),patch.object(owner,'_owned_worker',side_effect=self.remote(paths,calls)):
                    result=closure.abort_owned(root,adapters=owner,now=lambda:0)
                self.assertEqual(result['status'],'INCONCLUSIVE');self.assertTrue(all(not path.exists() for path in paths))

    def test_partial_feed_bind_failure_reaps_actual_child_with_unknown_identity(self):
        from test_pgcat_feed_binding import FeedBindingTests
        from pgcat_lane_protocol import Refused
        import pgcat_native_controller as controller
        fixture=FeedBindingTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        client=fixture.client;client.liveness_fd=None;client.process=None
        owner,paths=self.owner(fixture.base,client=client);calls=[]
        with patch.object(client.journal,'bind_feed',side_effect=Refused('storage')),self.assertRaises(Refused):
            client.start_feed(fixture.argv,{'PATH':os.defpath},fixture.manifest)
        child=client.feed_process
        self.assertIsNone(child.returncode)
        import pgcat_lane_client as lane_client
        from types import SimpleNamespace
        import subprocess,sys
        origin=lane_client._feed_children[client]
        lane_client._feed_children[client]=(os.getpid()+1,child)
        with self.assertRaises(Refused):client._abort_feed_child()
        lane_client._feed_children[client]=origin
        imitation=SimpleNamespace(feed_process=child)
        with self.assertRaises(Refused):lane_client.LaneClient._abort_feed_child(imitation)
        foreign=subprocess.Popen([sys.executable,'-c','import time;time.sleep(20)'])
        def reap_foreign():
            if foreign.poll() is None:foreign.kill()
            foreign.wait(timeout=2);client.feed_process=child
        self.addCleanup(reap_foreign)
        client.feed_process=foreign
        self.assertIs(client._abort_feed_child(),child)
        with patch.object(controller,'_require_verified'),patch.object(owner,'_owned_worker',side_effect=self.remote(paths,calls)):
            result=closure.abort_owned(fixture.base,adapters=owner,now=lambda:0)
        self.assertIsNotNone(child.returncode)
        self.assertIsNone(foreign.poll())
        self.assertEqual([r for r in result['ownedProcesses'] if r['role']=='feed'],[dict(role='feed',identity=None,state='unknown')])
        self.assertFalse(fixture.marker.exists());self.assertTrue(all(not path.exists() for path in paths))

    def test_feed_origin_precedes_first_post_spawn_close_failure(self):
        from test_pgcat_feed_binding import FeedBindingTests
        import pgcat_lane_client as lane_client
        import pgcat_native_controller as controller
        fixture=FeedBindingTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        client=fixture.client;client.liveness_fd=None;client.process=None
        owner,paths=self.owner(fixture.base,client=client);calls=[];issued=[];failed=[]
        real_spawn=lane_client.subprocess.Popen;real_close=os.close
        def spawn(*args,**kwargs):
            child=real_spawn(*args,**kwargs);issued.append(child);return child
        def close(fd):
            if issued and not failed:
                self.assertIs(lane_client._feed_children[client][1],issued[0]);failed.append(True)
                raise OSError('offline failure immediately after spawn')
            return real_close(fd)
        with patch.object(lane_client.subprocess,'Popen',side_effect=spawn),patch.object(lane_client.os,'close',side_effect=close),self.assertRaises(OSError):
            client.start_feed(fixture.argv,{'PATH':os.defpath},fixture.manifest)
        self.assertTrue(failed)
        with patch.object(controller,'_require_verified'),patch.object(owner,'_owned_worker',side_effect=self.remote(paths,calls)):
            result=closure.abort_owned(fixture.base,adapters=owner,now=lambda:0)
        self.assertIsNotNone(issued[0].returncode)
        self.assertEqual([r for r in result['ownedProcesses'] if r['role']=='feed'],[dict(role='feed',identity=None,state='unknown')])
        self.assertTrue(all(not path.exists() for path in paths));self.assertFalse(fixture.marker.exists())

    def test_already_bound_original_worker_tuple_is_still_stopped(self):
        import pgcat_native_controller as controller
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
        owner,paths=self.owner(root);owner.collector_attempted=True
        identity=dict(pid=12345,bootId='original-boot',startTicks=456,commandSha256='a'*64)
        controller._native_owners[owner]['workerProcesses']=dict(guardian=identity);calls=[]
        with patch.object(controller,'_require_verified'),patch.object(owner,'_owned_worker',side_effect=self.remote(paths,calls)):
            result=closure.abort_owned(root,adapters=owner,now=lambda:0)
        self.assertEqual([payload for verb,payload in calls if verb=='process-stop'],[identity])
        self.assertEqual([r for r in result['ownedProcesses'] if r['role']=='guardian'],[dict(role='guardian',identity=identity,state='stopped')])


if __name__=='__main__':unittest.main()
