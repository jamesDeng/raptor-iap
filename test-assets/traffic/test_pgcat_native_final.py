"""Native closure uses actual committed segments and private provider sidecars."""
import copy
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import pgcat_native_closure as closure
import pgcat_native_evidence as evidence
import pgcat_native_ingress as ingress
from segmented_capture import canonical, sha, read_progress, open_directory
from test_pgcat_native_ingress import Fixture, complete_native, write, START


class CoverageTests(unittest.TestCase):
    def fixture(self, mutate=None, kind='native-replacement'):
        from test_pgcat_causal_integration import bind_fixture_policy
        bind_fixture_policy(self)
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup)
        root=Path(tmp.name).resolve();root.chmod(0o700)
        f=Fixture(root,kind)
        if kind=='native-replacement':
            original=f.append
            def append(n,frame=None):
                value=copy.deepcopy(frame or f.frame)
                # Keep all old exporter series measurable through reduction;
                # shared original total is one, never one per original.
                if n>=2:
                    value['metrics']=copy.deepcopy(f.frame['metrics'])
                    for row in value['metrics']['raw']:row['value']=int(row['node']=='a' and row['kind']=='idle')
                    if n<5:
                        value['metrics']['nodes']=['a','b','c','d']
                        for node in ('c','d'):
                            for metric in ('active','idle','waiting'):
                                value['metrics']['raw'].append(dict(node=node,pool='test',user='poc_app',kind=metric,value=0,sourceAt=START+n))
                    if n>=5:
                        value['metrics']['nodes']=['c','d']
                        value['metrics']['raw']=[]
                        for node in ('c','d'):
                            for kind in ('active','idle','waiting'):
                                value['metrics']['raw'].append(dict(node=node,pool='test',user='poc_app',kind=kind,value=0,sourceAt=START+n))
                if mutate:mutate(n,value)
                original(n,value)
            f.append=append
            complete_native(f)
        return f

    def coverage(self,f):
        chain=ingress._Chain(f.run)
        progress,_,_=read_progress(f.run/'native-capture',f.context)
        self.assertTrue(callable(getattr(closure,'_coverage_prefix',None)), 'committed coverage closure is missing')
        return closure._coverage_prefix(chain,progress)

    def test_complete_shared_budget_then_later_absence_retains_every_ref(self):
        f=self.fixture()
        pair,refs=self.coverage(f)
        self.assertEqual(pair,['c','d'])
        self.assertEqual(len(refs),6)
        self.assertEqual([r['at'] for r in refs],[START+n for n in range(6)])
        self.assertEqual((f.run/'native-capture'/'segment-0001.jsonl').stat().st_nlink,1)

    def test_incomplete_history_or_missing_preabsence_budget_refuses(self):
        cases=('new-pool','missing-kind','two-total','absent-as-zero','no-measured-budget','substituted')
        for case in cases:
            def mutate(n,value):
                rows=value['metrics']['raw']
                if case=='new-pool' and n==2:
                    rows.extend(dict(node='a',pool='hidden',user='other',kind=k,value=0,sourceAt=START+n) for k in ('active','idle','waiting'))
                if case=='missing-kind' and n>=2:rows[:]=[r for r in rows if not (r['node']=='b' and r['kind']=='waiting')]
                if case=='two-total' and n>=2:
                    for r in rows:r['value']=int(r['kind']=='idle')
                if case=='absent-as-zero' and n>=2:
                    value['metrics']['nodes']=['a'];rows[:]=[r for r in rows if r['node']=='a']
                if case=='no-measured-budget' and n>=2:
                    for r in rows:r['value']=5
                if case=='substituted' and n==5:
                    value['inventory']['members']=['c','x']
            f=self.fixture(mutate)
            with self.subTest(case=case),self.assertRaises(ValueError):self.coverage(f)

    def test_diagnostic_constant_two_needs_no_native_result_or_retirement(self):
        f=self.fixture(kind='disabled-proof');pair,refs=self.coverage(f)
        self.assertEqual(pair,['a','b']);self.assertEqual(len(refs),2)
        self.assertFalse((f.run/'native-operation-binding.json').exists())

    def test_replay_visitor_cannot_change_authenticated_cohort_or_refs(self):
        from test_pgcat_native_evidence import NativeTerminal
        t=NativeTerminal();t.setUp();self.addCleanup(t.doCleanups);writer=t.writer()
        progress,_,_=read_progress(t.directory,t.context);fd=open_directory(t.directory)
        try:
            expected=evidence._replay(fd,t.context,progress['latest'])
            def mutate(value,ref):value['pods'].clear();ref['offset']=999
            actual=evidence._replay(fd,t.context,progress['latest'],visit=mutate)
            self.assertEqual(actual,expected)
        finally:os.close(fd)

    def test_current_gate_refuses_stale_tail_while_historical_terminal_reads(self):
        from test_pgcat_native_evidence import NativeTerminal
        t=NativeTerminal();t.setUp();self.addCleanup(t.doCleanups)
        writer=t.writer();sealed=t.seal()
        self.assertEqual(evidence.read_terminal_clients(t.directory,t.context,t.operation),sealed)
        self.assertTrue(callable(getattr(closure,'_current_terminal',None)), 'independent current terminal gate missing')
        with self.assertRaises(ValueError):closure._current_terminal(t.directory,t.context,t.operation,sealed,lambda: __import__('test_pgcat_native_evidence').kubernetes(),now=lambda:8)


class NativeStopSchemaTests(unittest.TestCase):
    def test_later_exact_pair_and_sequence_are_required(self):
        import pgcat_lane_protocol as protocol
        from pgcat_lane_protocol import roster_sha, original_roster
        plan=dict(runId='11111111-1111-4111-8111-111111111111',executionKind='native-replacement')
        reconcile=dict(schemaVersion=1,action='native-reconcile',runId=plan['runId'],executionKind=plan['executionKind'],planSha256=sha(canonical(plan)),requestId='22222222-2222-4222-8222-222222222222',inputs={},outputs=dict(pair=['c','d'],feedSession='33333333-3333-4333-8333-333333333333',sequence=5,observedAt=100))
        intent=dict(schemaVersion=1,runId=plan['runId'],executionKind=plan['executionKind'],planSha256=sha(canonical(plan)),feedSession=reconcile['outputs']['feedSession'],minimumSequence=6,reconcileSha256=sha(canonical(reconcile)))
        receipt=dict(mode='full-replacement-direct-v2',feedSession=intent['feedSession'],sequence=6,observedAt=101,desired=2,rosterSha256=roster_sha(original_roster(['c','d'])))
        self.assertTrue(callable(getattr(protocol,'native_stop_ack',None)), 'native exact final publication validation missing')
        ack=protocol.native_stop_ack(intent,receipt,reconcile,plan,now=102)
        self.assertEqual(ack,dict(intent,intentSha256=sha(canonical(intent)),finalReceiptSha256=sha(canonical(receipt)),sequence=6))
        for changes in (dict(sequence=5),dict(sequence=8),dict(observedAt=100),dict(observedAt=70),dict(rosterSha256='0'*64),dict(feedSession=plan['runId'])):
            with self.subTest(changes=changes),self.assertRaises(Exception):protocol.native_stop_ack(intent,dict(receipt,**changes),reconcile,plan,now=102)


class FinalActionTests(unittest.TestCase):
    fixture=CoverageTests.fixture
    def bound(self):
        import pgcat_native_controller as controller
        from test_pgcat_native_ingress import file_record
        from test_pgcat_native_evidence import rolling,kubernetes
        f=self.fixture()
        value=rolling(4);value['inventory']=copy.deepcopy(__import__('json').loads((f.run/'provider-inventory.json').read_text()))
        f.append(6,value);f.live=kubernetes(4)
        sealed=ingress.seal_ingress(f.run,f.read())
        ingress.seal_operation_binding(f.run,f.run/'native-ingress-0001.json')
        controller.mirror_operation_binding(f.run)
        credential=f.root/'kubeconfig';credential.write_bytes(b'offline-only');credential.chmod(0o600)
        from test_pgcat_native_admission import assets_for
        write(f.run/'native-runtime-assets.json',assets_for(f.plan))
        prepared={k:file_record(f.run/name) for k,name in [('runtimeAssetsFile','native-runtime-assets.json'),('planFile','native-plan.json'),('baselineFile','native-baseline.json'),('gatewayRuntimeFile','native-gateway-runtime.json')]}
        prepared['workerCredential']=controller.credential_identity(credential)
        self.assertTrue(callable(getattr(closure,'FixedClosureAdapters',None)), 'fixed closure adapter missing')
        with patch.object(closure,'_source',return_value={}):
            reads=controller.FixedWorkerReads(f.plan,credential,credential=prepared['workerCredential'])
            adapter=closure.FixedClosureAdapters(f.run,reads,prepared=prepared,action_id='99999999-9999-4999-8999-999999999999',
                operation=dict(basename='native-operation-binding.json',identity=file_record(f.run/'native-operation-binding.json')),supports={})
        return f,adapter

    def test_seal_then_reconcile_cas_records_exact_anchors_and_replay_refuses(self):
        f,a=self.bound()
        from test_pgcat_native_ingress import file_record
        with patch.object(closure,'_source',return_value={}),patch.object(a.reads,'live_clients',side_effect=f.live_clients):
            terminal=closure.seal_terminal(f.run,adapters=a,now=lambda:f.now)
            artifact=closure.support_artifact(f.run,'native-terminal-seal',adapters=a)
            self.assertEqual(artifact['identity']['sha256'],sha(canonical(terminal)))
            with self.assertRaises((ValueError,OSError)):closure.seal_terminal(f.run,adapters=a,now=lambda:f.now)
            a.supports['native-terminal-seal']=artifact
            a.action_id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
            old=closure._targets(dict(a='10.1.0.1',b='10.1.0.2',c='10.1.0.1',d='10.1.0.2'))
            # Sidecars preserve the fixture's addresses by enumeration.
            cm=dict(metadata=copy.deepcopy(f.baseline['configMap']),data={'targets.json':old.decode()})
            def patch_targets(before,new):
                self.assertEqual(before,cm)
                cm['data']['targets.json']=new.decode();cm['metadata']['resourceVersion']='2'
                return copy.deepcopy(cm)
            with patch.object(a.reads,'read',side_effect=lambda *args:copy.deepcopy(cm)),patch.object(a,'_patch_targets',side_effect=patch_targets):
                record=closure.reconcile(f.run,adapters=a,now=lambda:f.now)
            self.assertEqual(record['outputs']['pair'],['c','d'])
            self.assertEqual(record['outputs']['previousResourceVersion'],'1')
            self.assertEqual(record['outputs']['resourceVersion'],'2')
            coverage=__import__('json').loads((f.run/'native-coverage.json').read_text())
            self.assertEqual(len(coverage['frames']),7)
            self.assertEqual(coverage['oldIds'],['a','b'])
            self.assertEqual(record['inputs']['terminalSha256'],terminal['outputs']['terminalSha256'])

    def test_finish_matches_real_boundary_and_all_four_finals_once(self):
        import time
        from pgcat_lane_protocol import original_roster,roster_sha,native_stop_ack
        from pgcat_native_controller import _write_bytes
        from test_pgcat_native_ingress import file_record
        from test_pgcat_native_evidence import rolling
        f,a=self.bound()
        with patch.object(closure,'_source',return_value={}),patch.object(a.reads,'live_clients',side_effect=f.live_clients):
            terminal=closure.seal_terminal(f.run,adapters=a,now=lambda:f.now)
            a.supports['native-terminal-seal']=closure.support_artifact(f.run,'native-terminal-seal',adapters=a)
            addresses={r['id']:r['address'] for r in f.baseline['originals']};addresses.update(a._provider(lambda:f.now)[0]['privateAddresses'])
            cm=dict(metadata=copy.deepcopy(f.baseline['configMap']),data={'targets.json':closure._targets(addresses).decode()})
            def patch_targets(before,new):
                cm['data']['targets.json']=new.decode();cm['metadata']['resourceVersion']='2';return copy.deepcopy(cm)
            with patch.object(a.reads,'read',side_effect=lambda *args:copy.deepcopy(cm)),patch.object(a,'_patch_targets',side_effect=patch_targets):
                reconcile=closure.reconcile(f.run,adapters=a,now=lambda:f.now)
            a.supports['native-reconcile']=closure.support_artifact(f.run,'native-reconcile',adapters=a)
            value=rolling(5);value['inventory']=copy.deepcopy(__import__('json').loads((f.run/'provider-inventory.json').read_text()));f.append(7,value)
            inventory=__import__('json').loads((f.run/'provider-inventory.json').read_text())
            receipt=dict(mode=f.plan['mode'],feedSession=f.context['feedSession'],sequence=8,observedAt=f.now,desired=2,rosterSha256=roster_sha(inventory))
            intent=dict(schemaVersion=1,runId=f.plan['runId'],executionKind=f.plan['executionKind'],planSha256=sha(canonical(f.plan)),feedSession=f.context['feedSession'],minimumSequence=8,reconcileSha256=sha(canonical(reconcile)))
            ack=native_stop_ack(intent,receipt,reconcile,f.plan,now=f.now)
            exited=dict(runId=f.plan['runId'],feedId=f.feed['feedId'],pid=f.feed['pid'],exitCode=0)
            stop=dict(schemaVersion=1,action='native-feed-stop',runId=f.plan['runId'],executionKind=f.plan['executionKind'],planSha256=sha(canonical(f.plan)),requestId='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',inputs=dict(reconcileSha256=sha(canonical(reconcile))),outputs=dict(intentSha256=sha(canonical(intent)),ackSha256=sha(canonical(ack)),finalReceiptSha256=sha(canonical(receipt)),feedExit=exited))
            a.stopped=dict(support=stop,intent=intent,ack=ack,receipt=receipt,feedBinding=f.feed,feedExit=exited)
            a.stop_mirror=_write_bytes(f.run/'native-feed-stop.json',canonical(stop))
            a.action_id='cccccccc-cccc-4ccc-8ccc-cccccccccccc'
            boundary=f.run/'native-boundary';boundary.mkdir(mode=0o700)
            source_before=(f.run/'native-capture'/'segment-0001.jsonl').read_bytes()
            def finish(directory,*,expected_roster,deadline,kubeconfig):
                self.assertEqual(expected_roster,[dict(r['identity']) for r in read_progress(f.run/'native-capture',f.context)[0]['nativeCohort']['cohort'] if r['identity']['uid'].startswith('new-')])
                self.assertGreater(deadline,time.monotonic());self.assertEqual(kubeconfig,a.credential['path'])
                request=dict(requestId='d'*32,roster=expected_roster)
                actual=dict(requestId=request['requestId'],status='ACKNOWLEDGED',clients={r['uid']+'/'+r['processId']:{'status':'acknowledged'} for r in expected_roster})
                value=rolling(6);value['at']=START+8;value['phase']='settling';value['inventory']=inventory
                value['metrics']=copy.deepcopy(read_progress(f.run/'native-capture',f.context)[2]['metrics']);value['metrics'].update(observedAt=START+8,sourceAt=START+8)
                for row in value['metrics']['raw']:row['sourceAt']=START+8
                for pod in value['pods']:
                    pod['snapshot']['final']=True;pod['snapshot']['connected']=0;pod['snapshot']['sample']['at']=START+8
                    for event in pod['snapshot']['events']:event['at']+=START
                # Use the real collector producer for BOTH boundary records.
                # Its already-running writer owns the preceding rolling frames.
                # Defer only outer collector finalization to the caller fixture.
                import worker_capture
                def capture(*args,**kwargs):
                    write(boundary/'request.json',request);write(boundary/'receipt.json',actual)
                    return copy.deepcopy(value)
                with patch.object(worker_capture,'SegmentedCapture',return_value=f.writer),patch.object(worker_capture,'capture_frame',side_effect=capture),patch.object(f.writer,'finish',side_effect=lambda **kw:kw):
                    finished=worker_capture.run_capture(f.run/'native-capture',f.run/'provider-inventory.json',5400,boundary,progress_context=f.context)
                self.assertEqual(finished,dict(completed=True,cancelled=False,errors=[]))
                start=__import__('json').loads((boundary/'boundary-start.json').read_text())
                self.assertEqual(set(start),{'schemaVersion','bindingSha256','requestSha256','requestId','terminalSha256','executionKind','startedMonotonic','deadlineMonotonic','firstSettlingFrame'})
                self.assertEqual(start['schemaVersion'],2);self.assertEqual(start['executionKind'],f.plan['executionKind'])
                return actual
            with patch.object(a.reads,'read',side_effect=lambda *args:copy.deepcopy(cm)),patch('finish_clients.finish',side_effect=finish) as mutation,patch.object(closure,'_worker_boot',return_value='boot'):
                result=closure.finish_terminal(f.run,adapters=a,now=lambda:f.now)
                self.assertEqual(result['outputs']['boundaryRequestId'],'d'*32);self.assertEqual(mutation.call_count,1)
                with self.assertRaises((ValueError,OSError)):closure.finish_terminal(f.run,adapters=a,now=lambda:f.now)
                self.assertEqual(mutation.call_count,1)
            self.assertTrue((f.run/'native-capture'/'segment-0001.jsonl').read_bytes().startswith(source_before))

    @unittest.skipUnless(hasattr(os,'fork'),'requires real process fork')
    def test_fork_cannot_issue_action_from_inherited_closure_context(self):
        import select,json
        f,a=self.bound()
        with patch.object(closure,'_source',return_value={}),patch.object(a.reads,'live_clients',side_effect=f.live_clients):
            reader,writer=os.pipe();child=os.fork()
            if child==0:
                os.close(reader);result={}
                try:
                    for name,operation in [('context',a._operation),('seal',lambda:closure.seal_terminal(f.run,adapters=a,now=lambda:f.now))]:
                        try:operation()
                        except ValueError:result[name]='refused'
                        else:result[name]='inherited'
                    os.write(writer,json.dumps(result).encode());os.close(writer);os._exit(0)
                except BaseException:os.close(writer);os._exit(2)
            os.close(writer)
            try:
                ready,_,_=select.select([reader],[],[],5)
                if not ready:
                    os.kill(child,9);os.waitpid(child,0);self.fail('forked closure child exceeded budget')
                result=json.loads(os.read(reader,4096));_,status=os.waitpid(child,0)
            finally:os.close(reader)
            self.assertEqual(status,0);self.assertEqual(result,dict(context='refused',seal='refused'))
            self.assertFalse((f.run/'native-terminal-seal.intent').exists())
            record=closure.seal_terminal(f.run,adapters=a,now=lambda:f.now)
            self.assertEqual(closure.support_artifact(f.run,'native-terminal-seal',adapters=a)['identity']['sha256'],sha(canonical(record)))

    def test_publication_change_during_readback_cannot_adopt_a_later_sequence(self):
        from test_pgcat_native_evidence import rolling
        f,a=self.bound()
        with patch.object(closure,'_source',return_value={}),patch.object(a.reads,'live_clients',side_effect=f.live_clients):
            closure.seal_terminal(f.run,adapters=a,now=lambda:f.now)
            a.supports['native-terminal-seal']=closure.support_artifact(f.run,'native-terminal-seal',adapters=a)
            addresses={r['id']:r['address'] for r in f.baseline['originals']};addresses.update(a._provider(lambda:f.now)[0]['privateAddresses'])
            cm=dict(metadata=copy.deepcopy(f.baseline['configMap']),data={'targets.json':closure._targets(addresses).decode()})
            reads=[]
            def read(*args):
                reads.append(args)
                if len(reads)==2:
                    value=rolling(5);value['inventory']=copy.deepcopy(__import__('json').loads((f.run/'provider-inventory.json').read_text()));f.append(7,value)
                return copy.deepcopy(cm)
            def patch_targets(before,new):cm['data']['targets.json']=new.decode();cm['metadata']['resourceVersion']='2';return copy.deepcopy(cm)
            with patch.object(a.reads,'read',side_effect=read),patch.object(a,'_patch_targets',side_effect=patch_targets):
                with self.assertRaises(ValueError):closure.reconcile(f.run,adapters=a,now=lambda:f.now)
            self.assertFalse((f.run/'native-reconcile.json').exists())
            self.assertTrue((f.run/'native-reconcile.intent').exists())

    def test_foreign_uid_and_changed_targets_refuse_before_patch(self):
        for case in ('uid','targets'):
            f,a=self.bound()
            with patch.object(closure,'_source',return_value={}),patch.object(a.reads,'live_clients',side_effect=f.live_clients):
                closure.seal_terminal(f.run,adapters=a,now=lambda:f.now)
                a.supports['native-terminal-seal']=closure.support_artifact(f.run,'native-terminal-seal',adapters=a)
                cm=copy.deepcopy(f.objects[('ConfigMap','raptor-test','pgcat-provider-inventory')])
                if case=='uid':cm['metadata']['uid']='foreign'
                with patch.object(a.reads,'read',return_value=cm),patch.object(a,'_patch_targets') as mutation:
                    with self.assertRaises(ValueError):closure.reconcile(f.run,adapters=a,now=lambda:f.now)
                    mutation.assert_not_called()


class OwnedStopTests(unittest.TestCase):
    def test_only_same_retained_child_can_prove_exit(self):
        import subprocess,sys
        import pgcat_native_controller as controller
        from types import SimpleNamespace
        child=subprocess.Popen([sys.executable,'-c','pass']);self.addCleanup(lambda:child.poll() is None and child.kill())
        child.wait(timeout=3)
        owner=SimpleNamespace(feed=child)
        self.assertTrue(callable(getattr(controller,'_owned_feed_exit',None)), 'owned Popen exit gate missing')
        self.assertEqual(controller._owned_feed_exit(owner,child,deadline=__import__('pgcat_lane_protocol').monotonic()+1),0)
        other=SimpleNamespace(pid=child.pid,poll=lambda:0,wait=lambda **kw:0)
        with self.assertRaises(ValueError):controller._owned_feed_exit(owner,other,deadline=__import__('pgcat_lane_protocol').monotonic()+1)
        with self.assertRaises(ValueError):controller._owned_feed_exit(owner,child,deadline=__import__('pgcat_lane_protocol').monotonic()-1)

    def test_controller_stop_waits_for_same_real_child_after_ack_delay(self):
        import subprocess,sys,time,threading
        from types import SimpleNamespace
        from pgcat_lane_supervisor import save_once
        import pgcat_native_controller as controller
        import pgcat_lane_protocol as protocol
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
        child=subprocess.Popen([sys.executable,'-c',"import time,pathlib,sys; p=pathlib.Path(sys.argv[1]);\nwhile not p.exists(): time.sleep(.01)",str(root/'exit')])
        def reap():
            if child.poll() is None:child.kill()
            child.wait(timeout=3)
        self.addCleanup(reap)
        plan=dict(runId='11111111-1111-4111-8111-111111111111',executionKind='native-replacement')
        reconcile=dict(action='native-reconcile',runId=plan['runId'],executionKind=plan['executionKind'],planSha256=sha(canonical(plan)),outputs=dict(pair=['c','d'],feedSession='33333333-3333-4333-8333-333333333333',sequence=5,observedAt=time.time()-2))
        receipt=dict(mode=controller.MODE,feedSession=reconcile['outputs']['feedSession'],sequence=6,observedAt=time.time()-1,desired=2,rosterSha256=protocol.roster_sha(protocol.original_roster(['c','d'])))
        write(root/'status.json',receipt);claims=[];exits=[]
        owner=object.__new__(controller.NativeController);owner.feed=child;owner.bindings=dict(captureRoot=str(root),laneBindings=dict(receiptPath=str(root/'status.json')))
        journal=SimpleNamespace(native_plan=lambda:plan,native_reconcile=lambda:reconcile,claim_native_stop=lambda deadline:claims.append(deadline),record_native_stop=lambda:dict(feedExit=exits[0]))
        def confirm(actual):
            self.assertIs(actual,child);self.assertEqual(actual.poll(),0)
            exits.append(dict(runId=plan['runId'],feedId='f'*36,pid=child.pid,exitCode=actual.poll()))
        owner.client=SimpleNamespace(feed_process=child,journal=journal,_active=lambda:None,confirm_feed_exit=confirm,close=lambda:None)
        def producer():
            while not (root/'native-stop.intent').exists():time.sleep(.01)
            intent=__import__('json').loads((root/'native-stop.intent').read_text())
            time.sleep(3.1)
            save_once(root,'native-stop.ack',protocol.native_stop_ack(intent,receipt,reconcile,plan,now=time.time()),atomic=True)
            (root/'exit').touch()
        thread=threading.Thread(target=producer,daemon=True);thread.start()
        with patch.object(controller,'_closed'),patch.object(controller,'_require_verified'):
            result=owner.stop_feed()
        thread.join(timeout=1);self.assertFalse(thread.is_alive())
        self.assertEqual(len(claims),1);self.assertEqual(result['feedExit']['pid'],child.pid);self.assertEqual(child.returncode,0)

    def test_client_liveness_after_stop_needs_exact_owned_exit_binding(self):
        import subprocess,sys
        from types import SimpleNamespace
        from pgcat_lane_client import LaneClient
        from pgcat_lane_protocol import digest,Refused
        from test_pgcat_native_ingress import plan
        p=plan();child=subprocess.Popen([sys.executable,'-c','pass']);child.wait(timeout=3)
        binding=dict(runId=p['runId'],feedId='55555555-5555-4555-8555-555555555555',pid=child.pid,bindingSha256='a'*64,argvSha256='b'*64)
        stop=dict(feedBinding=binding,feedExit=dict(runId=p['runId'],feedId=binding['feedId'],pid=child.pid,exitCode=0))
        client=LaneClient();client.liveness_fd=1;client.process=SimpleNamespace(poll=lambda:None);client.run_id=p['runId'];client.mode=p['mode'];client.bindings=dict(mode=p['mode'],executionKind=p['executionKind'],target=p['target'],sourceHashes=p['sourceHashes']);client.bundle=digest(p['sourceHashes']);client.feed_process=child;client.feed_binding=binding
        client.journal=SimpleNamespace(run_snapshot=lambda:dict(runState='active',runId=p['runId'],mode=p['mode'],target=p['target'],hashes=dict(p['sourceHashes'],bundleSha256=client.bundle)),native_plan=lambda:p,native_stop=lambda:copy.deepcopy(stop))
        try:client._active()
        except Refused:self.fail('durably proven owned feed exit must allow the single native finish path')
        stop['feedExit']['pid']+=1
        with self.assertRaises(Refused):client._active()

    def test_current_gate_checks_exact_successor_and_predecessor_finals(self):
        from test_pgcat_native_evidence import NativeTerminal,kubernetes
        for change in ('valid','successor','owner','startup','predecessor'):
            t=NativeTerminal();t.setUp();self.addCleanup(t.doCleanups);writer=t.writer();sealed=t.seal()
            live=kubernetes()
            if change=='successor':live['roster'][0]['processId']='other'
            if change=='owner':live['replicaSets'][1]['metadata']['ownerReferences'][0]['uid']='other'
            if change in ('startup','predecessor'):
                value=__import__('test_pgcat_native_evidence').rolling(5)
                if change=='startup':value['startingPods']=[dict(name='third',uid='third',deploymentUid='deployment',replicaSetUid='rs-new',templateHash='hash-new',containerId=None)]
                else:value['pods'][0]['snapshot']['sample']['processId']='reused'
                writer.append(value)
            if change=='valid':closure._current_terminal(t.directory,t.context,t.operation,sealed,lambda:live,now=lambda:4)
            else:
                with self.subTest(change=change),self.assertRaises(ValueError):closure._current_terminal(t.directory,t.context,t.operation,sealed,lambda:live,now=lambda:5)

    def test_fixed_finish_api_exists_without_opening_runtime(self):
        import pgcat_native_controller as controller
        self.assertTrue(callable(getattr(closure,'finish_terminal',None)), 'exact terminal finish kernel missing')
        owner=object.__new__(controller.NativeController)
        from pgcat_lane_protocol import Refused
        with self.assertRaises(Refused):owner.stop_feed()
        with self.assertRaises(Refused):owner.finish_terminal()


class JournalClosureTests(unittest.TestCase):
    def journal(self,kind='native-replacement'):
        import time
        import pgcat_lane_protocol as protocol
        from test_pgcat_native_controller import Fixture as StartupFixture
        from test_pgcat_native_ingress import file_record
        from pgcat_lane_actions import BINDING_KEYS
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
        f=StartupFixture(root,kind);lane=root/'lane';lane.mkdir(mode=0o700);j=protocol.Journal(lane)
        cli=root/'offline-cli';cli.write_bytes(b'exit 1');cli.chmod(0o700)
        b={k:str(root/k) for k in BINDING_KEYS-{'sourceHashes','target'}}
        b.update(mode=f.plan['mode'],executionKind=f.plan['executionKind'],workbenchCli=str(cli),aliyunCli=str(cli),remoteRun=str(f.run),contractDir=str(root/'contract'),workerKubeconfig=str(root/'kubeconfig'),sourceHashes=f.plan['sourceHashes'],target=f.plan['target'])
        for key,name in [('phaseWorker','pgcat_rehearsal_worker.py'),('pendingWorker','pgcat_rehearsal_worker.py'),('pendingProofWorker','pgcat_pending_proof_worker.py'),('preDrainProofWorker','pgcat_pre_drain_proof_worker.py'),('zeroProofWorker','pgcat_zero_proof_worker.py')]:b[key]=str(f.run/name)
        b['receiptPath']=str(root/'status.json');b['laneLockPath']=str(root/'lane.lock');b['laneIntentDir']=str(root/'intents')
        outer=dict(laneBindings=b,laneRoot=str(lane),captureRoot=str(root),workerInventory=str(f.run/'provider-inventory.json'),workerKubeconfig=str(root/'kubeconfig'),sourceHashes={k:f.plan['sourceHashes'][k] for k in ('nativeController','client','runner')},localKubeconfig=str(root/'mac-kubeconfig'))
        write(lane/'bindings.json',b);write(lane/'native-plan.json',f.plan)
        raw=canonical(outer);binding=dict(f.feed,bindingSha256=sha(raw))
        with j._transaction() as db:
            db.execute('INSERT INTO run VALUES (1,?,?,?,?,?,?,?)',(f.plan['runId'],f.plan['mode'],canonical(f.plan['target']).decode(),canonical(dict(f.plan['sourceHashes'],bundleSha256=sha(canonical(f.plan['sourceHashes'])))).decode(),'active',None,None))
            db.execute('INSERT INTO native_plan VALUES (1,?,?)',(canonical(f.plan),canonical(file_record(lane/'native-plan.json')).decode()))
            db.execute('INSERT INTO native_prepared VALUES (1,?)',(canonical(f.prepared).decode(),))
        reconcile=dict(schemaVersion=1,action='native-reconcile',runId=f.plan['runId'],executionKind=f.plan['executionKind'],planSha256=sha(canonical(f.plan)),requestId='22222222-2222-4222-8222-222222222222',inputs=dict(operationBindingSha256='a'*64,terminalSha256='b'*64,coverageSha256='c'*64),outputs=dict(pair=['c','d'],configMapUid='cm-uid',previousResourceVersion='1',resourceVersion='2',targetsSha256='e'*64,feedSession=f.context['feedSession'],sequence=5,observedAt=time.time()-2))
        write(f.run/'native-reconcile.json',reconcile)
        artifact=dict(basename='native-reconcile.json',identity=file_record(f.run/'native-reconcile.json'))
        with j._transaction() as db:db.execute('INSERT INTO native_support VALUES (?,?)',(reconcile['requestId'],canonical(dict(action='native-reconcile',requestId=reconcile['requestId'],outputs=dict(support=reconcile,artifact=artifact))).decode()))
        with patch.object(protocol,'_release_admission'):j.bind_feed(binding,manifest_raw=raw)
        return root,f,j,reconcile,binding

    def test_stop_claim_and_original_private_files_bind_once(self):
        import time
        import pgcat_lane_protocol as protocol
        for case in ('valid','timeout','missing-exit','changed-ack'):
            root,f,j,reconcile,binding=self.journal()
            with patch.object(protocol,'_release_admission'):
                claim=j.claim_native_stop(float(protocol.monotonic()+5))
                with self.assertRaises(protocol.Refused):j.claim_native_stop(float(protocol.monotonic()+5))
                intent=dict(schemaVersion=1,runId=f.plan['runId'],executionKind=f.plan['executionKind'],planSha256=sha(canonical(f.plan)),feedSession=f.context['feedSession'],minimumSequence=6,reconcileSha256=sha(canonical(reconcile)))
                receipt=dict(mode=f.plan['mode'],feedSession=f.context['feedSession'],sequence=6,observedAt=time.time()-1,desired=2,rosterSha256=protocol.roster_sha(protocol.original_roster(['c','d'])))
                ack=protocol.native_stop_ack(intent,receipt,reconcile,f.plan,now=time.time())
                write(root/'native-stop.intent',intent);write(root/'native-stop.ack',ack);write(root/'status.json',receipt)
                if case!='missing-exit':j.confirm_feed_exit(dict(runId=f.plan['runId'],feedId=binding['feedId'],pid=binding['pid'],exitCode=0))
                if case=='changed-ack':write(root/'native-stop.ack',dict(ack,sequence=7))
                if case=='timeout':
                    with j._transaction() as db:
                        old=__import__('json').loads(db.execute('SELECT proof FROM native_stop_claim').fetchone()['proof']);old['deadlineMonotonic']=0;db.execute('UPDATE native_stop_claim SET proof=?',(canonical(old).decode(),))
                if case=='valid':
                    result=j.record_native_stop();self.assertEqual(result,j.native_stop());
                    with patch.object(protocol.time,'time',return_value=receipt['observedAt']+120):
                        self.assertEqual(j.native_stop(),result)
                        identity,frozen=j.native_stopped_receipt();self.assertEqual(frozen,receipt);self.assertEqual(identity[1],canonical(receipt))
                        with self.assertRaises(protocol.Refused):j.native_stop(require_fresh=True)
                    self.assertEqual(result['support']['requestId'],claim['requestId'])
                    with self.assertRaises(protocol.Refused):j.record_native_stop()
                    saved=(root/'native-stop.ack').read_bytes();(root/'native-stop.ack').rename(root/'old-ack');(root/'native-stop.ack').write_bytes(saved);(root/'native-stop.ack').chmod(0o600)
                    with self.assertRaises(protocol.Refused):j.native_stop()
                else:
                    with self.subTest(case=case),self.assertRaises(protocol.Refused):j.record_native_stop()
                    self.assertFalse((root/'native-feed-stop.json').exists())
                self.assertTrue((root/'native-feed-stop.intent').exists())

    def test_native_dependency_admission_is_exact_and_one_use(self):
        import pgcat_lane_protocol as protocol
        import pgcat_native_controller as controller
        from test_pgcat_native_controller import Fixture as StartupFixture
        from test_pgcat_native_ingress import file_record
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
        f=StartupFixture(root);lane=root/'lane';lane.mkdir(mode=0o700);j=protocol.Journal(lane)
        raw=(f.run/'native-plan.json').read_bytes();(lane/'native-plan.json').write_bytes(raw);(lane/'native-plan.json').chmod(0o600)
        with j._transaction() as db:
            db.execute('INSERT INTO run VALUES (1,?,?,?,?,?,?,?)',(f.plan['runId'],controller.MODE,canonical(f.plan['target']).decode(),canonical(dict(f.plan['sourceHashes'],bundleSha256=sha(canonical(f.plan['sourceHashes'])))).decode(),'active',None,None))
            db.execute('INSERT INTO native_plan VALUES (1,?,?)',(raw,canonical(file_record(lane/'native-plan.json')).decode()))
            db.execute('INSERT INTO native_prepared VALUES (1,?)',(canonical(f.prepared).decode(),))
            db.execute('INSERT INTO feed_binding VALUES (1,?)',(canonical(f.feed).decode(),))
            request=dict(requestId=f.admitted['localRequestId'],action='native-terminal-seal',deadlineMonotonic=123.)
            write(lane/('native-selector-'+request['requestId']+'.json'),{})
            # No completed operation binding: cannot adopt a current path.
            with self.assertRaises(protocol.Refused):j._native_admit_in_tx(db,request)
        self.assertIn('native-terminal-seal',protocol.NATIVE_ACTIONS)
        self.assertTrue(callable(getattr(j,'record_native_stop',None)), 'durable native stop binding missing')


class ProviderNativeStopTests(unittest.TestCase):
    def test_actual_successful_receipt_is_required_and_tick_budget_never_resets(self):
        import provider_feed as feed
        import time
        from types import SimpleNamespace
        from pgcat_lane_protocol import original_roster,roster_sha,native_stop_ack
        for case in ('valid','wrong-session','unknown-last','stale','wrong-ack'):
            tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
            plan=dict(runId='11111111-1111-4111-8111-111111111111',executionKind='native-replacement')
            session='33333333-3333-4333-8333-333333333333';now=time.time()
            reconcile=dict(schemaVersion=1,action='native-reconcile',runId=plan['runId'],executionKind=plan['executionKind'],planSha256=sha(canonical(plan)),outputs=dict(pair=['c','d'],feedSession=session,sequence=5,observedAt=now-2))
            receipt=dict(mode='full-replacement-direct-v2',feedSession=session,sequence=6,observedAt=now-1,desired=2,rosterSha256=roster_sha(original_roster(['c','d'])))
            intent=dict(schemaVersion=1,runId=plan['runId'],executionKind=plan['executionKind'],planSha256=sha(canonical(plan)),feedSession=session,minimumSequence=6,reconcileSha256=sha(canonical(reconcile)))
            op=object.__new__(feed.Operator);op.mode='full-replacement-direct-v2';op.run_context=dict(runId=plan['runId']);op.status_file=root/'status.json';op.args=SimpleNamespace(run_context=str(root/'lane'/'run-context.json'))
            op.feedSession=session;op.sequence=6;op.stop_ack=None;op.stopped=False;op.deadline=object();deadline=op.deadline
            op.last_successful_receipt=copy.deepcopy(receipt)
            if case=='wrong-session':intent['feedSession']=plan['runId']
            if case=='unknown-last':op.last_successful_receipt=None
            if case=='stale':receipt['observedAt']=now-31;op.last_successful_receipt=copy.deepcopy(receipt)
            if case=='wrong-ack':write(root/'native-stop.ack',dict(foreign=True))
            write(root/'native-stop.intent',intent);write(op.status_file,receipt)
            journal=SimpleNamespace(native_plan=lambda:plan,native_reconcile=lambda:reconcile,run_snapshot=lambda:dict(runId=plan['runId']),native_feed_owner=lambda:dict(pid=os.getpid()))
            with patch.object(feed,'Journal',return_value=journal),patch.object(feed,'validate_run_context',return_value=op.run_context):
                if case=='valid':
                    self.assertTrue(op.stop_native());self.assertEqual(__import__('json').loads((root/'native-stop.ack').read_text()),native_stop_ack(intent,receipt,reconcile,plan,now=time.time()))
                else:
                    with self.subTest(case=case),self.assertRaises(feed.FeedStop):op.stop_native()
            self.assertIs(op.deadline,deadline)
            self.assertEqual(op.status_file.read_bytes(),canonical(receipt))

    def test_native_success_receipt_bytes_are_canonical(self):
        import provider_feed as feed
        from types import SimpleNamespace
        from pgcat_lane_protocol import original_roster
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup);root=Path(tmp.name).resolve();root.chmod(0o700)
        op=feed.Operator(SimpleNamespace(mode='full-replacement-direct-v2',status_file=str(root/'status.json')))
        op.atomic_worker_write=lambda *a,**kw:None;op.update_targets=lambda *a,**kw:None
        value=dict(schemaVersion=2,mode=op.mode,feedSession=op.feedSession,sequence=1,observedAt=100,desired=2,privateAddresses={'c':'10.1.0.3','d':'10.1.0.4'},**original_roster(['c','d']))
        op.write(value)
        self.assertEqual(op.status_file.read_bytes(),canonical(op.last_successful_receipt))


if __name__=='__main__':unittest.main()
