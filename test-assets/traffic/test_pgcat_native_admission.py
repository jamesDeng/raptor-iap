"""Release manifest and entry admission; synthetic pins, no external I/O."""
import copy
import hashlib
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import pgcat_native_closure as closure
import pgcat_native_controller as controller
from segmented_capture import canonical
from test_pgcat_native_ingress import plan,write


def assets_for(p):
    scope=p.get('controllerScope',plan()['controllerScope']);target=p['target']
    return dict(schemaVersion=1,sourceBundleSha256=hashlib.sha256(canonical(p['sourceHashes'])).hexdigest(),infraApiRevision='b'*40,
        gatewayImage='ghcr.io/jamesdeng/raptor-iap-gateway@sha256:'+'e'*64,axImage='ax@sha256:'+'1'*64,
        clientImage='client@'+p['application']['imageDigest'],gateBinarySha256='f'*64,
        skillsTag='skills-v0.5.0',skillsCommit='a'*40,
        scope=dict(accountId=target['accountId'],regionId=target['regionId'],envCode=scope['envCode'],proxyCode=scope['proxyCode'],
            scalingGroupId=target['scalingGroupId'],nlbServerGroupId=target['nlbServerGroupId'],targetDbCode=scope['targetDbCode'],clusterId=scope['application']['clusterId']),
        application={k:p['application'][k] for k in ('namespace','name','uid','imageDigest')})


class AdmissionTests(unittest.TestCase):
    def test_full_stable_manifest_and_fresh_baseline_projection(self):
        p=plan();assets=assets_for(p)
        self.assertEqual(closure.validate_runtime_assets(assets,p),assets)
        for key in ('generation','initialReplicaSetUid','initialTemplateHash'):
            other=copy.deepcopy(p);other['application'][key]=2 if key=='generation' else 'fresh'
            self.assertEqual(closure.validate_runtime_assets(assets,other),assets)
        for key in assets:
            bad=copy.deepcopy(assets);del bad[key]
            with self.subTest(key=key),self.assertRaises(ValueError):closure.validate_runtime_assets(bad,p)
        for key in ('uid','imageDigest'):
            bad=copy.deepcopy(p);bad['application'][key]='foreign'
            with self.assertRaises(ValueError):closure.validate_runtime_assets(assets,bad)
        for key in assets['scope']:
            bad=copy.deepcopy(assets);bad['scope'][key]='foreign'
            with self.subTest(key=key),self.assertRaises(ValueError):closure.validate_runtime_assets(bad,p)

    def test_original_file_identity_and_canonical_bytes_are_retained(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder).resolve();root.chmod(0o700);p=plan('disabled-proof');assets=assets_for(p)
            file=root/'native-runtime-assets.json';write(file,assets)
            value,identity=closure.read_runtime_assets(root,p)
            self.assertEqual(value,assets)
            file.unlink();write(file,assets)
            with self.assertRaises(ValueError):closure.read_runtime_assets(root,p,original=identity)

    def test_native_blocker_precedes_any_effect_even_with_valid_assets(self):
        with patch('subprocess.Popen',side_effect=AssertionError('spawn')),patch('subprocess.run',side_effect=AssertionError('command')):
            with self.assertRaisesRegex(ValueError,'ClockAgreementUnavailable'):closure.runtime_admission(plan())
        self.assertIsNone(closure.runtime_admission(plan('disabled-proof')))

    def test_missing_or_summary_only_qualification_cannot_qualify(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder).resolve();root.chmod(0o700);p=plan('disabled-proof');assets=assets_for(p)
            write(root/'native-qualification.json',dict(schemaVersion=1,proofRunId=p['runId'],closeSha256='a'*64,
                sourceBundleSha256=assets['sourceBundleSha256'],runtimeAssetsSha256=hashlib.sha256(canonical(assets)).hexdigest(),qualifiedAt=1))
            with self.assertRaises((ValueError,OSError)):closure.validate_native_qualification(root,assets)

    def test_cleanup_proof_reads_original_absence_without_reopening_credential(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder).resolve();root.chmod(0o700);file=root/'kubeconfig';file.write_bytes(b'offline');file.chmod(0o600)
            identity=controller.credential_identity(file);receipt=closure.cleanup_bound_credential(file,identity)
            with patch.object(controller,'credential_identity',side_effect=AssertionError('deleted credential reopened')):
                self.assertEqual(closure.verify_credential_absence(identity),receipt['receiptSha256'])
            file.write_bytes(b'replacement');file.chmod(0o600)
            with self.assertRaises(ValueError):closure.verify_credential_absence(identity)

    def test_retained_journal_reader_never_creates_or_mutates_a_database(self):
        from pgcat_lane_protocol import Journal,Refused
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder).resolve();root.chmod(0o700)
            with self.assertRaises((OSError,Refused)):Journal.read_only(root)
            self.assertFalse((root/'journal.sqlite3').exists())
            journal=Journal(root);reader=Journal.read_only(root)
            with self.assertRaises(Refused),reader._connection() as db:db.execute('DELETE FROM run')

    def test_actual_isolated_source_entry_admits_only_manifest_bound_diagnostic(self):
        import subprocess,sys,json
        import pgcat_source_bootstrap as bootstrap
        from test_pgcat_native_controller import Fixture
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder).resolve();root.chmod(0o700);f=Fixture(root,'disabled-proof')
            source=bootstrap.ROOT.replace("sys.argv=[p]+sys.argv[3:]\nsys.exit(m.main())\n", """
m.install(__import__('json').loads(sys.argv[3]))
from pgcat_lane_protocol import _release_admission,Refused,ACTIONS,NATIVE_ACTIONS,NATIVE_RESERVED
from pgcat_native_closure import release_assets
from pgcat_lane_actions import ActionCatalog
from pgcat_native_controller import main
import subprocess
plan=__import__('json').loads(sys.argv[4])
_release_admission(plan['mode'],'native-baseline',plan=plan)
for action in ACTIONS|{'native-gateway-enable','native-gateway-disable'}:
 try:_release_admission(plan['mode'],action,plan=plan)
 except Refused:pass
 else:raise AssertionError('opened action')
for action in NATIVE_ACTIONS|NATIVE_RESERVED:
 try:_release_admission(plan['mode'],action,kind='native-replacement')
 except Refused:pass
 else:raise AssertionError('opened native')
 try:ActionCatalog.command(action,dict(mode=plan['mode'],executionKind='native-replacement'))
 except Refused:pass
 else:raise AssertionError('native catalog bypass')
for name in ('run','Popen'):
 setattr(subprocess,name,lambda *a,**kw:(_ for _ in ()).throw(AssertionError('effect')))
sys.argv=['native','run-native','/missing','/missing','/missing']
assert main()==1
print('DIAGNOSTIC-ONLY')
""")
            result=subprocess.run([sys.executable,'-I','-S','-c',source,str(f.run/'pgcat_source_bootstrap.py'),f.plan['sourceHashes']['sourceBootstrap'],json.dumps(f.plan['sourceHashes']),json.dumps(f.plan)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
            self.assertEqual(result.returncode,0,result.stderr.decode());self.assertEqual(result.stdout,b'DIAGNOSTIC-ONLY\n')

    def test_worker_qualification_replays_actual_complete_diagnostic_after_cleanup(self):
        import pgcat_source_bootstrap as bootstrap
        import test_pgcat_native_assessment as assessment_tests
        from test_pgcat_native_controller import Fixture
        from test_pgcat_native_ingress import file_record
        original=assessment_tests.diagnostic_complete_fixture;kept=[]
        def keep(test):
            result=original(test);kept.append(result);return result
        helper=assessment_tests.RestoreKernelTests();self.addCleanup(helper.doCleanups)
        with patch('test_pgcat_native_ingress.Fixture',side_effect=Fixture),patch.object(assessment_tests,'diagnostic_complete_fixture',side_effect=keep):
            helper.test_actual_diagnostic_false_idle_and_posthold_two_advancing_reads()
        f,a=kept[0]
        supports={k:dict(basename=k+'.json',identity=file_record(f.run/(k+'.json'))) for k in ('native-terminal-seal','native-reconcile','native-finish','native-verdict','native-restore')}
        cleaned=closure.cleanup_bound_credential(a.credential['path'],a.credential)
        payload=dict(plan=f.plan,prepared=a.prepared,supports=supports,workerCleanupSha256=cleaned['receiptSha256'],stopped=a.stopped,stopArtifact=dict(basename='native-feed-stop.json',identity=a.stop_mirror),holdArtifact=a.hold_artifact,operation=a.operation,ingresses=a.ingresses)
        with patch.object(bootstrap,'require_verified'),patch.object(bootstrap,'current_hashes',return_value=f.plan['sourceHashes']),patch('pgcat_collector_control.process_identity',return_value=None):
            result=closure.diagnostic_worker_proof(f.run,payload)
            self.assertEqual(result['assessmentSha256'],__import__('json').loads((f.run/'native-verdict.json').read_text())['outputs']['assessmentSha256'])
            self.assertFalse(Path(a.credential['path']).exists())
            for key in supports:
                bad=copy.deepcopy(payload);bad['supports'][key]['identity']['inode']+=1
                with self.subTest(key=key),self.assertRaises(ValueError):closure.diagnostic_worker_proof(f.run,bad)
            for key in ('stopArtifact','holdArtifact','operation'):
                bad=copy.deepcopy(payload);bad[key]['identity']['inode']+=1
                with self.subTest(key=key),self.assertRaises(ValueError):closure.diagnostic_worker_proof(f.run,bad)
            bad=copy.deepcopy(payload);bad['stopped']['feedBinding']['pid']+=1
            with self.assertRaises(ValueError):closure.diagnostic_worker_proof(f.run,bad)
            raw=(f.run/'native-restoration-frames.json').read_bytes();(f.run/'native-restoration-frames.json').write_bytes(raw+b' ')
            with self.assertRaises(ValueError):closure.diagnostic_worker_proof(f.run,payload)

    def test_poll_skips_actual_wrapper_consumed_tick_without_poisoning_journal(self):
        import os,json,subprocess,sys,time
        from test_pgcat_native_final import JournalClosureTests
        from pgcat_lane_client import LaneClient
        import pgcat_lane_protocol as protocol
        import pgcat_workbench_lane as wrapper
        from pgcat_lane_supervisor import LaneSupervisor
        helper=JournalClosureTests();self.addCleanup(helper.doCleanups)
        root,f,j,_,_=helper.journal('disabled-proof');bindings=json.loads((j.root/'bindings.json').read_text())
        intents=Path(bindings['laneIntentDir']);intents.mkdir(mode=0o700)
        child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(10)'])
        def reap():
            if child.poll() is None:child.kill()
            child.wait(timeout=2)
        self.addCleanup(reap)
        reader,writer=os.pipe();self.addCleanup(os.close,reader);self.addCleanup(os.close,writer)
        client=object.__new__(LaneClient);client.liveness_fd=writer;client.process=child;client.feed_process=child
        client.journal=j;client.root=j.root;client.run_id=f.plan['runId'];client.mode=f.plan['mode'];client.bindings=bindings
        client.bundle=hashlib.sha256(canonical(f.plan['sourceHashes'])).hexdigest();client.supervisor_id='88888888-8888-4888-8888-888888888888'
        owner=object.__new__(controller.NativeController);owner.client=client;owner.feed=child
        owner.bindings=dict(laneBindings=bindings);owner.prepared=dict(plan=f.plan)
        receipt=dict(mode=f.plan['mode'],feedSession=f.context['feedSession'],sequence=6,observedAt=time.time(),desired=2,
            rosterSha256=protocol.roster_sha(protocol.original_roster(['a','b'])))
        write(Path(bindings['receiptPath']),receipt);owner.receipt=receipt
        # The wrapper reserves a tick before execution. Ordinary effect admission
        # must still reject reuse, even though a polling miss is expected.
        wrapper.consume_intent(str(intents),receipt['feedSession'],receipt['sequence'])
        with self.assertRaises(wrapper.Refused):wrapper.consume_intent(str(intents),receipt['feedSession'],receipt['sequence'])
        supervisor=object.__new__(LaneSupervisor);supervisor.bindings=bindings
        with self.assertRaises(protocol.Refused):supervisor._unused(dict(feedSession=receipt['feedSession']),receipt)
        raw=Path(bindings['receiptPath']).read_bytes();stamp=Path(bindings['receiptPath']).stat().st_mtime_ns
        # Real LaneClient._active/call and Journal; only outer source-owner
        # issuance is supplied by this local fixture.
        with patch.object(controller,'_closed'),patch.object(owner,'_require_owner'):
            self.assertIsNone(owner.poll_ingress())
            self.assertIsNone(owner.poll_ingress())
            self.assertEqual(j.run_snapshot()['runState'],'active')
            with j._connection() as db:self.assertEqual(db.execute('SELECT count(*) FROM requests').fetchone()[0],0)
            self.assertEqual(Path(bindings['receiptPath']).read_bytes(),raw)
            self.assertEqual(Path(bindings['receiptPath']).stat().st_mtime_ns,stamp)
            consumed=intents/(receipt['feedSession']+'.6.intent');consumed.write_bytes(b'foreign')
            with self.assertRaises(ValueError):owner.poll_ingress()

    def test_cleanup_complete_audit_and_qualification_keep_the_full_original_chain(self):
        import os,json,sys,subprocess,threading,time,shlex,uuid
        from types import SimpleNamespace
        import pgcat_source_bootstrap as bootstrap
        import pgcat_lane_protocol as protocol
        import pgcat_lane_supervisor as supervisor
        import test_pgcat_native_ingress as ingress_tests
        import test_pgcat_native_assessment as assessment_tests
        from test_pgcat_native_controller import Fixture
        from pgcat_lane_client import LaneClient
        from pgcat_lane_actions import BINDING_KEYS
        from test_pgcat_native_ingress import file_record
        feed=subprocess.Popen([sys.executable,'-c','pass']);feed.wait(timeout=2)
        captured=[];original_fixture=assessment_tests.diagnostic_complete_fixture;original_write=ingress_tests.write
        outer_holder=[]
        def bind_original_feed(path,value):
            if path.name=='native-feed-binding.json':
                run=path.parent;root=run.parent;capture=root/'mac-capture';capture.mkdir(mode=0o700)
                p=json.loads((run/'native-plan.json').read_text());cli=root/'offline-cli';cli.write_bytes(b'exit 1');cli.chmod(0o700)
                b={k:str(capture/k) for k in BINDING_KEYS-{'sourceHashes','target'}}
                b.update(mode=p['mode'],executionKind=p['executionKind'],workbenchCli=str(cli),aliyunCli=str(cli),remoteRun=str(run),contractDir=str(root/'contract'),workerKubeconfig=str(root/'kubeconfig'),sourceHashes=p['sourceHashes'],target=p['target'])
                for key,name in [('phaseWorker','pgcat_rehearsal_worker.py'),('pendingWorker','pgcat_rehearsal_worker.py'),('pendingProofWorker','pgcat_pending_proof_worker.py'),('preDrainProofWorker','pgcat_pre_drain_proof_worker.py'),('zeroProofWorker','pgcat_zero_proof_worker.py')]:b[key]=str(run/name)
                b.update(receiptPath=str(capture/'status.json'),laneLockPath=str(capture/'lane.lock'),laneIntentDir=str(capture/'intents'))
                outer=dict(laneBindings=b,laneRoot=str(capture/'lane'),captureRoot=str(capture),workerInventory=str(run/'provider-inventory.json'),workerKubeconfig=str(root/'kubeconfig'),sourceHashes={k:p['sourceHashes'][k] for k in ('nativeController','client','runner')},localKubeconfig=str(root/'mac'/'kubeconfig'))
                value.update(pid=feed.pid,bindingSha256=protocol.digest(outer));outer_holder.append(outer)
            original_write(path,value)
        def keep(test):
            result=original_fixture(test);captured.append(result);return result
        helper=assessment_tests.RestoreKernelTests();self.addCleanup(helper.doCleanups)
        with patch('test_pgcat_native_ingress.Fixture',side_effect=Fixture),patch.object(ingress_tests,'write',side_effect=bind_original_feed),patch.object(assessment_tests,'diagnostic_complete_fixture',side_effect=keep):
            helper.test_actual_diagnostic_false_idle_and_posthold_two_advancing_reads()
        f,a=captured[0];outer=outer_holder[0];root=Path(outer['captureRoot']);b=outer['laneBindings'];lane=Path(outer['laneRoot']);lane.mkdir(mode=0o700)
        j=protocol.Journal(lane);write(lane/'bindings.json',b);write(lane/'native-plan.json',f.plan)
        with j._transaction() as db:
            db.execute('INSERT INTO run VALUES (1,?,?,?,?,?,?,?)',(f.plan['runId'],f.plan['mode'],canonical(f.plan['target']).decode(),canonical(dict(f.plan['sourceHashes'],bundleSha256=protocol.digest(f.plan['sourceHashes']))).decode(),'active',None,None))
            db.execute('INSERT INTO native_plan VALUES (1,?,?)',(canonical(f.plan),canonical(file_record(lane/'native-plan.json')).decode()))
            db.execute('INSERT INTO native_prepared VALUES (1,?)',(canonical(a.prepared).decode(),))
        with patch.object(protocol,'_release_admission'):j.bind_feed(f.feed,manifest_raw=canonical(outer))
        with j._transaction() as db:
            for name in ('native-terminal-seal','native-reconcile','native-finish','native-verdict','native-restore'):
                support=json.loads((f.run/(name+'.json')).read_text());output=dict(support=support,artifact=dict(basename=name+'.json',identity=file_record(f.run/(name+'.json'))))
                if name=='native-finish':output.update(stopArtifact=dict(basename='native-feed-stop.json',identity=a.stop_mirror),holdArtifact=a.hold_artifact)
                proof=dict(action=name,requestId=support['requestId'],outputs=output)
                db.execute('INSERT INTO native_support VALUES (?,?)',(str(uuid.uuid4()),canonical(proof).decode()))
            for artifact in a.ingresses:
                proof=dict(action='native-ingress',requestId=str(uuid.uuid4()),outputs=dict(status='BOUND',ingressFile=artifact,operationFile=a.operation))
                db.execute('INSERT INTO native_support VALUES (?,?)',(proof['requestId'],canonical(proof).decode()))
        # Reconstruct the original stop publication from the retained worker
        # fixture, then let the actual Journal authenticate it and the real PID.
        stopped=a.stopped;claim={k:v for k,v in stopped['support'].items() if k!='outputs'}
        write(root/'native-feed-stop.intent',claim)
        with j._transaction() as db:db.execute('INSERT INTO native_stop_claim VALUES (1,?)',(canonical(dict(record=claim,identity=file_record(root/'native-feed-stop.intent'),deadlineMonotonic=protocol.monotonic()+5.)).decode(),))
        for name,value in [('native-stop.intent',stopped['intent']),('native-stop.ack',stopped['ack']),('status.json',stopped['receipt'])]:write(root/name,value)
        exit_marker=f.root/'supervisor-exit'
        child=subprocess.Popen([sys.executable,'-c','import pathlib,sys,time; p=pathlib.Path(sys.argv[1]);\nwhile not p.exists():time.sleep(.01)',str(exit_marker)])
        def reap():
            if child.poll() is None:child.kill()
            child.wait(timeout=2)
        self.addCleanup(reap)
        reader,writer=os.pipe();self.addCleanup(os.close,reader)
        client=LaneClient();client.liveness_fd=writer;client.process=child;client.feed_process=feed;client.feed_binding=f.feed
        client.journal=j;client.root=lane;client.run_id=f.plan['runId'];client.mode=f.plan['mode'];client.bindings=b;client.bundle=protocol.digest(f.plan['sourceHashes']);client.supervisor_id='66666666-6666-4666-8666-666666666666'
        self.addCleanup(client.close)
        client.confirm_feed_exit(feed)
        with patch.object(protocol,'_release_admission'),patch.object(protocol.time,'time',return_value=stopped['receipt']['observedAt']):self.assertEqual(j.record_native_stop(),stopped)
        mac=Path(outer['localKubeconfig']);mac.parent.mkdir(mode=0o700);mac.write_bytes(b'offline-mac');mac.chmod(0o600)
        assets=assets_for(f.plan);write(root/'native-runtime-assets.json',assets)
        prepared=dict(a.prepared,plan=f.plan,baseline=f.baseline,macCredential=controller.credential_identity(mac),macRuntimeAssetsFile=file_record(root/'native-runtime-assets.json'))
        write(root/'native-prepared.json',prepared);write(root/'native-plan.json',f.plan)
        binding_path=f.root/'outer-binding.json';write(binding_path,outer)
        owner=object.__new__(controller.NativeController);owner.owner_pid=os.getpid();owner.bindings=outer;owner.binding_path=binding_path;owner.binding_sha=protocol.digest(outer);owner.run_id=f.plan['runId']
        owner.prepared=prepared;owner.prepared_identity=file_record(root/'native-prepared.json');owner.local_plan_identity=file_record(root/'native-plan.json')
        owner.runtime_assets=assets;owner.runtime_assets_identity=file_record(f.run/'native-runtime-assets.json');owner.client=client;owner.feed=feed;owner.mac_credential=prepared['macCredential'];owner.worker_credential=a.credential
        controller._native_owners[owner]=dict(pid=os.getpid(),bindings=canonical(outer),runId=owner.run_id,mac=copy.deepcopy(owner.mac_credential),worker=copy.deepcopy(owner.worker_credential),prepared=canonical(prepared),feed=feed,feedIdentity=f.feed,feedAttempted=True,client=client,workerProcesses={})
        global_root=f.root/'global';global_root.mkdir(mode=0o700);(global_root/'owner.lock').write_bytes(b'');(global_root/'owner.lock').chmod(0o600)
        write(global_root/'active-run',dict(runId=owner.run_id,supervisorId=client.supervisor_id,laneRoot=str(lane)))
        def worker(verb,payload):
            if verb=='credential-cleanup':return closure.cleanup_bound_credential(a.credential['path'],payload)
            if verb=='credential-absent':return dict(absent=not Path(a.credential['path']).exists())
            raise AssertionError('unexpected worker effect')
        def transport(argv,**kw):
            self.assertEqual(kw['timeout'],4);parts=shlex.split(argv[-1]);self.assertEqual(parts[-3:-1],['diagnostic-proof',str(f.run)])
            result=controller._owned_worker_control('diagnostic-proof',f.run,json.loads(parts[-1]))
            return SimpleNamespace(returncode=0,stdout=canonical(result))
        errors=[]
        def finish_journal():
            try:
                until=time.monotonic()+4
                while not (lane/'finish.ready').exists():
                    if time.monotonic()>until:raise TimeoutError('finish publication')
                    time.sleep(.005)
                j.finish();exit_marker.write_bytes(b'exit')
            except BaseException as error:errors.append(error)
        with patch.object(controller,'_require_verified'),patch.object(bootstrap,'require_verified'),patch.object(bootstrap,'current_hashes',return_value=f.plan['sourceHashes']),patch.object(protocol,'_release_admission'),patch.object(closure,'release_assets',return_value=(assets,owner.runtime_assets_identity)),patch.object(closure,'__file__',str(f.run/'pgcat_native_closure.py')),patch.object(controller,'__file__',str(f.run/'pgcat_native_controller.py')),patch.object(owner,'_owned_worker',side_effect=worker),patch.object(supervisor,'GLOBAL_ROOT',global_root),patch('pgcat_collector_control.process_identity',return_value=None):
            owner.cleanup();self.assertFalse(mac.exists());self.assertFalse(Path(a.credential['path']).exists())
            # No post-cleanup path may reopen either original credential.
            with patch.object(controller,'credential_identity',side_effect=AssertionError('credential reopened')):
                thread=threading.Thread(target=finish_journal);thread.start()
                try:closed=owner.complete()
                finally:thread.join(timeout=5)
                self.assertEqual(errors,[]);self.assertTrue(closed['closed']);self.assertEqual(closed['status'],'PASS')
                with patch.object(closure.subprocess,'run',side_effect=transport):
                    qualified=closure.seal_native_qualification(owner)
                    self.assertEqual(closure.validate_native_qualification(root,assets),qualified)
                    for changed in ('scope','application','sourceBundleSha256','clientImage'):
                        bad=copy.deepcopy(assets)
                        if changed=='scope':bad[changed]['proxyCode']='foreign'
                        elif changed=='application':bad[changed]['uid']='foreign'
                        else:bad[changed]='foreign'
                        with self.subTest(changed=changed),self.assertRaises(ValueError):closure.validate_native_qualification(root,bad)
                    write(root/'native-abort.json',dict(status='INCONCLUSIVE'))
                    with self.assertRaises(ValueError):closure.validate_native_qualification(root,assets)
                self.assertIs(owner.feed,feed);self.assertEqual(feed.poll(),0);self.assertEqual(child.poll(),0)
                self.assertFalse((global_root/'active-run').exists())

    def test_original_create_handoff_is_one_use_and_not_a_final_binding(self):
        from test_pgcat_native_final import JournalClosureTests
        from test_pgcat_native_ingress import REQUEST
        import pgcat_lane_protocol as protocol
        helper=JournalClosureTests();self.addCleanup(helper.doCleanups)
        root,f,j,_,_=helper.journal()
        handoff=dict(schemaVersion=1,runId=f.plan['runId'],planSha256=hashlib.sha256(canonical(f.plan)).hexdigest(),requestId=REQUEST)
        write(root/'native-created-request.json',handoff)
        with patch.object(protocol,'_release_admission'):
            self.assertEqual(j.claim_native_request(),REQUEST)
            self.assertEqual(j.native_request()['handoff'],handoff)
            self.assertIsNone(j.native_request()['context'])
            context={k:v for k,v in f.api_context.items() if k!='environment'}
            output=dict(requestContext=context,ingressFile=dict(basename='native-ingress-0001.json',identity=f.prepared['baselineFile']))
            with j._transaction() as db:j._associate_native_context_in_tx(db,f.plan,output)
            self.assertEqual(j.native_request()['context'],context)
            with j._transaction() as db:j._associate_native_context_in_tx(db,f.plan,output)
            bad=copy.deepcopy(output);bad['requestContext']['definitionSha256']='0'*64
            with self.assertRaises(protocol.Refused),j._transaction() as db:j._associate_native_context_in_tx(db,f.plan,bad)
            with self.assertRaises(protocol.Refused):j.claim_native_request()
            path=root/'native-created-request.json';path.unlink();write(path,handoff)
            with self.assertRaises((ValueError,protocol.Refused)):j.native_request()
