"""Native Gateway owned boundaries; disposable files/processes, no cloud calls."""
import copy
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import pgcat_native_controller as native
import pgcat_native_closure as closure
import pgcat_lane_protocol as protocol
import pgcat_source_bootstrap as bootstrap
from segmented_capture import canonical,sha
from test_pgcat_native_ingress import REQUEST,write


def job_budget(f):
    from test_pgcat_native_ingress import file_record
    start=int(f.now);cap=native.capture_budget(f.plan,start)
    folder=f.run/'collector-job';folder.mkdir(mode=0o700,exist_ok=True)
    write(folder/'intent.json',dict(runId=f.plan['runId'],startedAt=start,profile=f.plan['mode'],captureMaxSeconds=cap,prepared=f.prepared))
    return dict(captureStartedAt=start,captureUntil=start+cap,collectorIntentFile=file_record(folder/'intent.json'))


def owned_fixture(test,capture_max=None):
    """Real private Journal/lock and harmless owned children; external I/O stays absent."""
    import subprocess,sys,fcntl
    from test_pgcat_native_final import JournalClosureTests
    from test_pgcat_native_ingress import file_record
    from pgcat_lane_client import LaneClient
    from test_pgcat_native_admission import assets_for
    helper=JournalClosureTests();test.addCleanup(helper.doCleanups)
    import test_pgcat_native_ingress as fixtures
    original_plan=fixtures.plan
    def bounded_plan(kind='native-replacement'):
        value=original_plan(kind)
        if capture_max is not None:value['deadline']['captureMaxSeconds']=capture_max
        return value
    with patch.object(fixtures,'plan',side_effect=bounded_plan):root,f,j,_,binding=helper.journal()
    with j._connection() as db:outer=json.loads(bytes(db.execute('SELECT raw FROM feed_manifest').fetchone()['raw']))
    children=[subprocess.Popen([sys.executable,'-c','import time;time.sleep(90)']) for _ in range(2)]
    def reap():
        for child in children:
            if child.poll() is None:child.kill()
            child.wait(timeout=2)
    test.addCleanup(reap);feed,supervisor=children
    binding=dict(binding,pid=feed.pid)
    with j._transaction() as db:db.execute('UPDATE feed_binding SET proof=?',(canonical(binding).decode(),))
    mac=Path(outer['localKubeconfig']);mac.write_bytes(b'fixture-only');mac.chmod(0o600)
    assets=assets_for(f.plan);write(root/'native-runtime-assets.json',assets)
    prepared=dict(f.prepared,plan=f.plan,baseline=f.baseline,macCredential=native.credential_identity(mac),macRuntimeAssetsFile=file_record(root/'native-runtime-assets.json'))
    write(root/'native-prepared.json',prepared);write(root/'native-plan.json',f.plan);write(root/'outer.json',outer)
    client=LaneClient();client.process=supervisor;client.feed_process=feed;client.feed_binding=binding;client.journal=j;client.root=j.root
    client.run_id=f.plan['runId'];client.mode=f.plan['mode'];client.bindings=outer['laneBindings'];client.bundle=sha(canonical(f.plan['sourceHashes']));client.supervisor_id=REQUEST
    reader,writer=os.pipe();client.liveness_fd=writer;test.addCleanup(os.close,reader);test.addCleanup(client.close)
    owner=object.__new__(native.NativeController);owner.owner_pid=os.getpid();owner.bindings=outer;owner.binding_path=root/'outer.json';owner.binding_sha=sha(canonical(outer));owner.run_id=f.plan['runId']
    owner.prepared=prepared;owner.prepared_identity=file_record(root/'native-prepared.json');owner.local_plan_identity=file_record(root/'native-plan.json')
    owner.runtime_assets=assets;owner.runtime_assets_identity=file_record(f.run/'native-runtime-assets.json');owner.client=client;owner.feed=feed
    owner.mac_credential=prepared['macCredential'];owner.worker_credential=f.prepared['workerCredential']
    global_root=root/'global';global_root.mkdir(mode=0o700);lock=global_root/'owner.lock'
    fd=os.open(str(lock),os.O_RDWR|os.O_CREAT|os.O_EXCL,0o600);fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB);test.addCleanup(os.close,fd)
    write(global_root/'active-run',dict(runId=owner.run_id,supervisorId=REQUEST,laneRoot=str(j.root)))
    info=os.fstat(fd)
    native._native_owners[owner]=dict(pid=os.getpid(),bindings=canonical(outer),runId=owner.run_id,mac=owner.mac_credential,worker=owner.worker_credential,
        prepared=canonical(prepared),supervisor=supervisor,feed=feed,feedIdentity=binding,feedAttempted=True,client=client,workerProcesses={},humanPause='operator-confirmed-external-pause',globalOwnerLock=(info.st_dev,info.st_ino))
    budget=job_budget(f);owner.capture_until=budget['captureUntil'];native._native_owners[owner]['captureBudget']={k:budget[k] for k in ('captureStartedAt','captureUntil')}
    patches=[patch.object(native.time,'time',return_value=f.now),patch.object(bootstrap,'require_verified'),patch.object(bootstrap,'current_hashes',return_value=f.plan['sourceHashes']),
        patch.object(closure,'release_assets',return_value=(assets,owner.runtime_assets_identity)),patch('pgcat_lane_supervisor.GLOBAL_ROOT',global_root),patch.object(protocol,'_release_admission')]
    for context in patches:context.start();test.addCleanup(context.stop)
    with j._transaction() as db:
        db.execute('INSERT INTO native_support VALUES (?,?)',('ready',canonical(dict(action='native-collector-launch',outputs=dict(ready=f.ready,readyFile=file_record(f.run/'native-ready.json'),collectorIntentFile=budget['collectorIntentFile']))).decode()))
    return owner,f,j,global_root


class CreateReservationTests(unittest.TestCase):
    def fixture(self,state='RESERVED',enabled=True):
        from test_pgcat_native_final import JournalClosureTests
        helper=JournalClosureTests();self.addCleanup(helper.doCleanups)
        root,f,j,_,_=helper.journal()
        self.assertTrue(hasattr(j,'begin_native_create'),'owned Create reservation missing')
        with j._transaction() as db:
            db.execute('INSERT INTO native_create VALUES (1,?,?)',(state,canonical(dict(runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),reservationId=REQUEST)).decode()))
            if enabled:
                db.execute('INSERT INTO native_gateway VALUES (?,?,?,?,?)',('native-gateway-enable',REQUEST,'VERIFIED','{}','{}'))
        return root,f,j

    def test_begin_is_durable_before_ui_and_never_reconsumed(self):
        root,f,j=self.fixture()
        with patch.object(protocol,'_release_admission'):
            result=j.begin_native_create()
            self.assertEqual(result['state'],'DISPATCHING')
            with self.assertRaises(protocol.Refused):j.begin_native_create()
        with j._connection() as db:self.assertEqual(db.execute('SELECT state FROM native_create').fetchone()['state'],'DISPATCHING')
        self.assertFalse((root/'native-created-request.json').exists())

    def test_create_before_verified_enable_or_after_abort_is_refused(self):
        for state,enabled in [('RESERVED',False),('ABORTING',True),('DISPATCHING',True)]:
            root,f,j=self.fixture(state,enabled)
            with patch.object(protocol,'_release_admission'),self.assertRaises(protocol.Refused):j.begin_native_create()

    def test_handoff_requires_dispatching_and_binds_real_id_once(self):
        for state in ('RESERVED','DISPATCHING','ABORTING'):
            root,f,j=self.fixture(state)
            write(root/'native-created-request.json',dict(schemaVersion=1,runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),requestId=REQUEST))
            with patch.object(protocol,'_release_admission'):
                if state=='DISPATCHING':
                    self.assertEqual(j.claim_native_request(),REQUEST)
                    with self.assertRaises(protocol.Refused):j.claim_native_request()
                else:
                    with self.assertRaises(protocol.Refused):j.claim_native_request()


class NativeRestoreAdmissionTests(unittest.TestCase):
    def test_native_restore_preserves_lane_request_and_real_handoff(self):
        import time,uuid
        from test_pgcat_native_final import JournalClosureTests
        from test_pgcat_native_ingress import file_record,timestamp
        from pgcat_lane_supervisor import receipt_identity
        helper=JournalClosureTests();self.addCleanup(helper.doCleanups);root,f,j,reconcile,binding=helper.journal()
        def artifact(name,value):
            write(f.run/name,value);return dict(basename=name,identity=file_record(f.run/name))
        def put(action,support,**extra):
            output=dict(support=support,artifact=artifact(action+'.json',support),**extra)
            with j._transaction() as db:db.execute('INSERT INTO native_support VALUES (?,?)',(str(uuid.uuid4()),canonical(dict(action=action,requestId=support['requestId'],outputs=output)).decode()))
            return output['artifact']
        qualification=dict(proof='fixture');runtime=dict(f.runtime,kind='gateway-current-runtime')
        enabled=dict(schemaVersion=1,action='native-gateway-enable',runId=f.plan['runId'],executionKind='native-replacement',planSha256=sha(canonical(f.plan)),requestId=REQUEST,
            inputs=dict(readySha256=sha(canonical(f.ready)),qualificationSha256=sha(canonical(qualification))),outputs=dict(status='VERIFIED',observedAt=f.now,
            setReceipt=dict(resourceVersion='1',changedAfter=timestamp(f.now),replacementEnabled=True),runtime=dict(runtime,verification=dict(runtime['verification'],replacementEnabled=True),provenance=dict(kind='guarded-verify',resourceVersion='1',changedAfter=timestamp(f.now)))))
        put('native-gateway-enable',enabled)
        with j._transaction() as db:
            db.execute('INSERT INTO native_gateway VALUES (?,?,?,?,?)',('native-gateway-enable',REQUEST,'VERIFIED',canonical(dict(qualification=dict(qualification=qualification))).decode(),'{}'))
            db.execute('INSERT INTO native_create VALUES (1,?,?)',('DISPATCHING','{}'))
        write(root/'native-created-request.json',dict(schemaVersion=1,runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),requestId=REQUEST))
        ingress=artifact('native-ingress-0001.json',dict(fixture=True));operation=artifact('native-operation-binding.json',dict(fixture=True))
        import pgcat_native_ingress as ingress_reader
        self.addCleanup(ingress_reader._close_origins)
        context=f.read()['context']
        self.assertGreaterEqual(len(f.secret_reads),2)
        with patch.object(protocol,'_release_admission'):
            self.assertEqual(j.claim_native_request(),REQUEST)
            with j._transaction() as db:
                j._associate_native_context_in_tx(db,f.plan,dict(requestContext=context,ingressFile=ingress))
                db.execute('INSERT INTO native_support VALUES (?,?)',(str(uuid.uuid4()),canonical(dict(action='native-ingress',outputs=dict(status='BOUND',ingressFile=ingress,operationFile=operation))).decode()))
            j.claim_native_stop(float(protocol.monotonic()+5))
            intent=dict(schemaVersion=1,runId=f.plan['runId'],executionKind=f.plan['executionKind'],planSha256=sha(canonical(f.plan)),feedSession=f.context['feedSession'],minimumSequence=6,reconcileSha256=sha(canonical(reconcile)))
            receipt=dict(mode=f.plan['mode'],feedSession=f.context['feedSession'],sequence=6,observedAt=time.time()-1,desired=2,rosterSha256=protocol.roster_sha(protocol.original_roster(['c','d'])))
            for name,value in [('native-stop.intent',intent),('native-stop.ack',protocol.native_stop_ack(intent,receipt,reconcile,f.plan,now=time.time())),('status.json',receipt)]:write(root/name,value)
            j.confirm_feed_exit(dict(runId=f.plan['runId'],feedId=binding['feedId'],pid=binding['pid'],exitCode=0));stopped=j.record_native_stop()
        stop=artifact('native-feed-stop.json',stopped['support']);hold=artifact('native-finish-hold.json',dict(fixture=True))
        for action in ('native-terminal-seal','native-finish','native-verdict'):
            put(action,dict(requestId=str(uuid.uuid4())),**(dict(stopArtifact=stop,holdArtifact=hold) if action=='native-finish' else {}))
        disabled=dict(enabled,action='native-gateway-disable',requestId=str(uuid.uuid4()),inputs=dict(raptorRequestId=REQUEST),outputs=dict(status='VERIFIED',observedAt=f.now,setReceipt=None,runtime=runtime,
            settlement=dict(kind='bound-request',raptorRequestId=REQUEST,attemptId='attempt-1',status='completed',cleanup='confirmed',recoveryNeeded=False,controlState='')))
        closure.validate_support(disabled,f.plan,'native-gateway-disable');disabled_artifact=put('native-gateway-disable',disabled)
        provider=artifact('native-restoration-provider.json',dict(fixture=True))
        identity,_=j.native_stopped_receipt();local_id=str(uuid.uuid4());request=dict(requestId=local_id,action='native-restore',deadlineMonotonic=protocol.monotonic()+5.,feedSession=receipt['feedSession'],minimumSequence=6,receiptIdentity=receipt_identity(identity))
        write(j.root/('native-selector-'+local_id+'.json'),{})
        # The independent provider reader is covered elsewhere. Do not bypass the native
        # request/disable-support/stopped-receipt/envelope/admission branch under test.
        with patch.object(protocol,'_release_admission'),patch.object(j,'_native_restoration_provider_in_tx',return_value=dict(workerArtifact=provider)):
            with j._transaction() as db:j._native_admit_in_tx(db,request)
        with j._connection() as db:proof=json.loads(db.execute('SELECT proof FROM native_admission WHERE request_id=?',(local_id,)).fetchone()['proof'])
        self.assertEqual(proof['envelope']['localRequestId'],local_id);self.assertNotEqual(local_id,REQUEST)
        self.assertEqual(proof['envelope']['raptorRequestId'],REQUEST);self.assertEqual(proof['envelope']['supports']['native-gateway-disable'],disabled_artifact)
        self.assertEqual(proof['envelope']['stopped'],stopped)


class SettlementTests(unittest.TestCase):
    def test_precreate_is_typed_and_cannot_impersonate_terminal_request(self):
        self.assertTrue(hasattr(closure,'validate_settlement'),'typed pre-Create settlement missing')
        record=dict(kind='pre-create-reserved',runId=REQUEST,planSha256='a'*64,reservationSha256='b'*64,ownerSha256='c'*64,state='ABORTING')
        self.assertEqual(closure.validate_settlement(record,request_id=None),record)
        for change in (dict(state='DISPATCHING'),dict(raptorRequestId=REQUEST),dict(kind='bound-request')):
            with self.assertRaises(ValueError):closure.validate_settlement(dict(record,**change),request_id=None)
        with self.assertRaises(ValueError):closure.validate_settlement(record,request_id=REQUEST)

    def test_bound_requires_real_terminal_cleanup_and_identity(self):
        self.assertTrue(hasattr(closure,'validate_settlement'),'typed settlement missing')
        record=dict(kind='bound-request',raptorRequestId=REQUEST,attemptId='attempt',status='completed',cleanup='confirmed',recoveryNeeded=False,controlState='')
        self.assertEqual(closure.validate_settlement(record,request_id=REQUEST),record)
        for change in (dict(status='paused'),dict(cleanup='unknown'),dict(recoveryNeeded=True),dict(controlState='cancel_requested'),dict(raptorRequestId='foreign')):
            with self.assertRaises(ValueError):closure.validate_settlement(dict(record,**change),request_id=REQUEST)


class PublicOwnerTests(unittest.TestCase):
    def test_live_unrelated_supervisor_handle_cannot_replace_original(self):
        import subprocess,sys
        owner,f,j,global_root=owned_fixture(self)
        original=owner.client.process
        foreign=subprocess.Popen([sys.executable,'-c','import time;time.sleep(90)'])
        try:
            self.assertNotEqual(original.pid,foreign.pid)
            owner.client.process=foreign
            with self.assertRaises(ValueError):owner._gateway_owner(require_feed=True)
        finally:
            owner.client.process=original;foreign.kill();foreign.wait(timeout=2)

    def test_enable_budget_requires_original_capture_and_minimum_remaining(self):
        owner,f,j,global_root=owned_fixture(self)
        self.assertTrue(hasattr(owner,'_gateway_budget'),'original capture budget missing')
        from pgcat_native_ingress import instant
        start=f.now;owner.capture_until=start+native.capture_budget(f.plan,start)
        native._native_owners[owner]['captureBudget']=dict(captureStartedAt=start,captureUntil=owner.capture_until)
        with patch.object(native.time,'time',return_value=start):
            self.assertEqual(owner._gateway_budget(enable=True)['captureUntil'],owner.capture_until)
        for now in (owner.capture_until-406.,owner.capture_until+1.,instant(f.plan['deadline']['mutationCutoff'])-406.):
            with patch.object(native.time,'time',return_value=now),patch.object(owner,'_owned_worker',side_effect=AssertionError('launch')):
                with self.assertRaises(ValueError):owner.enable_gateway()
            with j._connection() as db:self.assertEqual(db.execute('SELECT count(*) FROM native_gateway').fetchone()[0],0)
        owner.capture_until+=1
        with patch.object(native.time,'time',return_value=start),self.assertRaises(ValueError):owner._gateway_budget(enable=True)
        short,f,j,_=owned_fixture(self,capture_max=75)
        with patch.object(short,'_owned_worker',side_effect=AssertionError('short capture launch')):
            with self.assertRaises(ValueError):short.enable_gateway()
        with j._connection() as db:self.assertEqual(db.execute('SELECT count(*) FROM native_gateway').fetchone()[0],0)

    def test_original_budget_is_claimed_and_expiry_during_qualification_rolls_back(self):
        for expires in (False,True):
            owner,f,j,_=owned_fixture(self);clock=[f.now]
            def qualification():
                if expires:clock[0]=owner.capture_until-406.
                return dict(qualification=dict(fixture='actual diagnostic reader covered separately'))
            with patch.object(native.time,'time',side_effect=lambda:clock[0]),patch.object(owner,'_gateway_qualification',side_effect=qualification):
                if expires:
                    with self.assertRaises((ValueError,protocol.Refused)):j.claim_native_gateway('native-gateway-enable',owner)
                    with j._connection() as db:
                        self.assertEqual(db.execute('SELECT count(*) FROM native_create').fetchone()[0],0)
                        self.assertEqual(db.execute('SELECT count(*) FROM native_gateway').fetchone()[0],0)
                else:
                    claim=j.claim_native_gateway('native-gateway-enable',owner)
                    self.assertEqual(claim['budget'],owner._gateway_budget(enable=True))
                    self.assertEqual(j.native_gateway('native-gateway-enable')['claim'],claim)

    def test_actual_owned_handles_and_original_global_lock_are_required(self):
        owner,f,j,global_root=owned_fixture(self)
        evidence=owner._gateway_owner(require_feed=True)
        self.assertEqual(evidence['pid'],os.getpid());self.assertEqual(evidence['humanPause'],'operator-confirmed-external-pause')
        (global_root/'owner.lock').rename(global_root/'held.lock')
        (global_root/'owner.lock').write_bytes(b'');(global_root/'owner.lock').chmod(0o600)
        with self.assertRaises(ValueError):owner._gateway_owner(require_feed=True)

    def test_preflight_booleans_and_copied_or_forked_owner_do_not_issue_qualification(self):
        owner,f,j,global_root=owned_fixture(self)
        row=native._native_owners[owner];row['producersQuiesced']=True
        with self.assertRaises(ValueError):owner._gateway_qualification()
        with self.assertRaises(ValueError):copy.copy(owner)._gateway_owner(require_feed=True)
        if hasattr(os,'fork'):
            read,write_fd=os.pipe();child=os.fork()
            if child==0:
                os.close(read)
                try:owner._gateway_owner(require_feed=True)
                except ValueError:os.write(write_fd,b'refused')
                finally:os.close(write_fd);os._exit(0)
            os.close(write_fd)
            try:self.assertEqual(os.read(read,32),b'refused');os.waitpid(child,0)
            finally:os.close(read)

    def test_reserved_abort_claim_is_typed_and_dispatching_lost_id_stays_unknown(self):
        for state in ('RESERVED','DISPATCHING'):
            owner,f,j,global_root=owned_fixture(self)
            native._native_owners[owner]['aborting']=True
            reservation=dict(runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),reservationId=REQUEST)
            with j._transaction() as db:db.execute('INSERT INTO native_create VALUES (1,?,?)',(state,canonical(reservation).decode()))
            if state=='DISPATCHING':
                with self.assertRaises(protocol.Refused):j.claim_native_gateway('native-gateway-disable',owner)
                self.assertFalse((f.run/'native-gateway-disable-job').exists())
            else:
                claim=j.claim_native_gateway('native-gateway-disable',owner)
                self.assertEqual(claim['settlement']['kind'],'pre-create-reserved')
                self.assertNotIn('raptorRequestId',claim['settlement'])
                with j._connection() as db:self.assertEqual(db.execute('SELECT state FROM native_create').fetchone()['state'],'ABORTING')
                with self.assertRaises(protocol.Refused):j.begin_native_create()
                with self.assertRaises(protocol.Refused):j.claim_native_gateway('native-gateway-disable',owner)

    def test_native_observer_envelope_needs_original_qualified_enable_anchor(self):
        from test_pgcat_native_controller import Fixture
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder).resolve();root.chmod(0o700);f=Fixture(root)
            envelope=dict(schemaVersion=1,action='native-ingress',localRequestId=REQUEST,runId=f.plan['runId'],executionKind='native-replacement',
                planSha256=sha(canonical(f.plan)),baselineSha256=f.plan['baselineSha256'],feedBinding=f.feed,prepared=f.prepared,priorIngress=None,
                deadlineMonotonic=1.,readySha256=f.admitted['readySha256'],feedBindingSha256=f.ready['feedBindingSha256'])
            with self.assertRaises(ValueError):native.validate_action(f.plan,envelope,dict(requestId=REQUEST,approvalIds=[]))

    def test_create_after_mutation_cutoff_never_contacts_worker(self):
        owner,f,j,global_root=owned_fixture(self)
        from pgcat_native_ingress import instant
        with j._transaction() as db:db.execute('INSERT INTO native_gateway VALUES (?,?,?,?,?)',('native-gateway-enable',REQUEST,'VERIFIED','{}','{}'))
        with patch.object(native.time,'time',return_value=instant(f.plan['deadline']['mutationCutoff'])),patch.object(owner,'_owned_worker',side_effect=AssertionError('worker contacted after cutoff')) as transport:
            with self.assertRaises(ValueError):owner.begin_create()
            transport.assert_not_called()

    def test_native_abort_before_client_still_attempts_both_credentials(self):
        from test_pgcat_native_assessment import AbortReviewTests
        helper=AbortReviewTests()
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder).resolve();root.chmod(0o700)
            owner,paths=helper.owner(root,prepared=dict(plan=dict(runId=REQUEST,executionKind='native-replacement')))
            calls=[]
            with patch.object(native,'_require_verified'),patch.object(owner,'_owned_worker',side_effect=helper.remote(paths,calls)):
                result=closure.abort_owned(root,adapters=owner,now=lambda:0)
            self.assertEqual(result['gatewayState'],'unknown')
            self.assertEqual([verb for verb,_ in calls].count('credential-cleanup'),1)
            self.assertTrue(all(not path.exists() for path in paths))

    def test_unowned_controller_cannot_launch_poll_or_begin_create(self):
        c=object.__new__(native.NativeController)
        for name,args in [('bind_qualification',('/missing',)),('enable_gateway',()),('disable_gateway',()),('poll_gateway',()),('begin_create',())]:
            self.assertTrue(hasattr(c,name),name+' missing')
            with patch.object(bootstrap,'require_verified'),patch('subprocess.run',side_effect=AssertionError('external I/O')),patch('subprocess.Popen',side_effect=AssertionError('spawn')):
                with self.assertRaises(ValueError):getattr(c,name)(*args)


class NativeJobTests(unittest.TestCase):
    def test_actual_precreate_job_seals_and_authenticates_false_without_request_reads(self):
        import worker_control,pgcat_collector_control as collector,time,hmac,hashlib,base64
        from test_pgcat_native_closure import GatewayClosureTests
        helper=GatewayClosureTests();helper.setUp();self.addCleanup(helper.doCleanups)
        f=helper.f;run=f.run
        owner=dict(pid=os.getpid(),runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),feedBindingSha256='a'*64,supervisorId=REQUEST,humanPause='operator-confirmed-external-pause')
        reservation=dict(runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),reservationId=REQUEST)
        settlement=dict(kind='pre-create-reserved',runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),reservationSha256=sha(canonical(reservation)),ownerSha256=sha(canonical(owner)),state='ABORTING')
        claim=dict(schemaVersion=1,action='native-gateway-disable',requestId=REQUEST,runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),prepared=f.prepared,
            readyFile=__import__('test_pgcat_native_ingress').file_record(run/'native-ready.json'),qualification=None,settlement=settlement,owner=owner,reservation=reservation,budget=job_budget(f))
        folder=run/'native-gateway-disable-job';folder.mkdir(mode=0o700)
        intent=dict(claim=claim,deadlineMonotonic=time.monotonic()+151.)
        intent_file=native._write_bytes(folder/'intent.json',canonical(intent))
        fd=os.open(str(folder/'result.json'),os.O_RDWR|os.O_CREAT|os.O_EXCL,0o600)
        slot=worker_control._slot_identity(os.fstat(fd));kr,kw=os.pipe();key=os.urandom(32);os.write(kw,key);os.close(kw)
        proc=dict(pid=os.getpid(),bootId='test-boot',startTicks=1,commandSha256='d'*64)
        original=native._write_bytes
        def publication(path,raw):
            identity=original(path,raw)
            if path==folder/'ready.json':
                record=dict(claimSha256=sha(canonical(claim)),intentFile=intent_file,readyFile=identity,process=proc,resultSlot=slot,deadlineMonotonic=intent['deadlineMonotonic'])
                launch=dict(record=record,file=original(folder/'started.json',canonical(record)))
                original(folder/'trigger.json',canonical(launch))
            return identity
        helper.enabled=True
        helper.objects[('Secret','raptor-system','gateway-live-config')]['data']['config.json']=base64.b64encode(b'{"replacementEnabled":true}').decode()
        with patch.object(worker_control.time,'time',return_value=f.now),patch.object(worker_control,'__file__',str(run/'worker_control.py')),patch.object(collector,'process_identity',return_value=proc),patch.object(native,'_write_bytes',side_effect=publication),patch.object(native.FixedHTTP,'__call__',side_effect=AssertionError('fake request read')):
            result=worker_control.native_job_run(run,dict(action='native-gateway-disable',intentSha256=intent_file['sha256'],resultFD=fd,resultKeyFD=kr))
        signed=json.loads((folder/'result.json').read_bytes())
        actual=worker_control.verify_job_result(signed,key.hex())
        self.assertEqual(result,dict(status='VERIFIED'))
        self.assertEqual(actual['support']['outputs']['settlement'],settlement)
        self.assertFalse(actual['support']['outputs']['runtime']['verification']['replacementEnabled'])
        self.assertEqual(actual['artifact']['identity']['sha256'],sha(canonical(actual['support'])))
        self.assertNotIn(key.hex(),''.join(p.read_text() for p in folder.iterdir()))
        self.assertNotIn('raptorRequestId',actual['support']['inputs'])
        self.assertEqual(sum(a[0]==str(helper.gate) and a[1]=='set' for a in helper.calls),1)

    def test_status_dictionary_cannot_authenticate_as_the_actual_child_result(self):
        import worker_control,hmac,hashlib
        self.assertTrue(hasattr(worker_control,'verify_job_result'),'same-process child result authentication missing')
        key=os.urandom(32)
        value=dict(claimSha256='a'*64,process=dict(pid=1,bootId='boot',startTicks=3,commandSha256='b'*64),status='UNKNOWN',support=None,artifact=None)
        signed=dict(value,authenticator=hmac.new(key,canonical(value),hashlib.sha256).hexdigest())
        self.assertEqual(worker_control.verify_job_result(signed,key.hex()),value)
        for bad in (dict(signed,status='VERIFIED'),dict(signed,authenticator='0'*64),dict(signed,extra=True)):
            with self.assertRaises(ValueError):worker_control.verify_job_result(bad,key.hex())
        with self.assertRaises(ValueError):worker_control.verify_job_result(signed,os.urandom(32).hex())

    def claim_fixture(self):
        from test_pgcat_native_controller import Fixture
        from test_pgcat_native_ingress import file_record
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup)
        root=Path(tmp.name).resolve();root.chmod(0o700);f=Fixture(root)
        claim=dict(schemaVersion=1,action='native-gateway-enable',requestId=REQUEST,runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),
            prepared=f.prepared,readyFile=file_record(f.run/'native-ready.json'),qualification={},settlement=None,
            owner=dict(pid=os.getpid(),runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),feedBindingSha256='a'*64,supervisorId=REQUEST,humanPause='operator-confirmed-external-pause'),
            reservation=dict(runId=f.plan['runId'],planSha256=sha(canonical(f.plan)),reservationId=REQUEST),budget=job_budget(f))
        return f,claim

    def test_worker_budget_originals_refuse_expiry_forgery_and_inode_swap_before_launch(self):
        import worker_control
        for case in ('expired','near-cutoff','forged-until','replaced-intent'):
            f,claim=self.claim_fixture();now=f.now
            if case=='expired':now=claim['budget']['captureUntil']+1
            if case=='near-cutoff':now=__import__('pgcat_native_ingress').instant(f.plan['deadline']['mutationCutoff'])-406
            if case=='forged-until':claim['budget']['captureUntil']+=151
            if case=='replaced-intent':
                path=f.run/'collector-job'/'intent.json';raw=path.read_bytes();path.rename(path.with_suffix('.old'));path.write_bytes(raw);path.chmod(0o600)
            with patch.object(worker_control.time,'time',return_value=now),patch.object(worker_control,'__file__',str(f.run/'worker_control.py')),patch.object(bootstrap,'require_verified'),patch.object(bootstrap,'current_hashes',return_value=f.plan['sourceHashes']),patch('subprocess.Popen',side_effect=AssertionError('spawn')):
                with self.subTest(case=case),self.assertRaises(ValueError):worker_control.native_job_control('launch',f.run,dict(claim=claim))
            self.assertFalse((f.run/'native-gateway-enable-job').exists())
            self.assertFalse((f.run/'native-gateway-enable.set.json').exists())

    def test_worker_disable_deadline_is_clamped_to_original_remaining_capture(self):
        import worker_control,time
        f,claim=self.claim_fixture();claim.update(action='native-gateway-disable',qualification=None)
        claim['settlement']=dict(kind='pre-create-reserved',runId=claim['runId'],planSha256=claim['planSha256'],reservationSha256=sha(canonical(claim['reservation'])),ownerSha256=sha(canonical(claim['owner'])),state='ABORTING')
        now=claim['budget']['captureUntil']-10.;before=time.monotonic()
        with patch.object(worker_control.time,'time',return_value=now),patch.object(worker_control,'__file__',str(f.run/'worker_control.py')),patch.object(bootstrap,'require_verified'),patch.object(bootstrap,'current_hashes',return_value=f.plan['sourceHashes']),patch('subprocess.Popen',side_effect=ValueError('fixture launch refused')):
            with self.assertRaises(ValueError):worker_control.native_job_control('launch',f.run,dict(claim=claim))
        intent=json.loads((f.run/'native-gateway-disable-job'/'intent.json').read_text())
        self.assertGreater(intent['deadlineMonotonic'],before)
        self.assertLessEqual(intent['deadlineMonotonic'],time.monotonic()+10.)
        self.assertEqual(intent['claim']['budget'],claim['budget'])

    def test_detached_launch_then_exact_trigger_and_read_only_poll_preserve_original_identity(self):
        import worker_control,pgcat_collector_control as collector,sys,time,signal
        f,claim=self.claim_fixture();commands=[]
        code="""import os,sys,json,time,hashlib
from pathlib import Path
folder=Path(sys.argv[1]);claim_sha=sys.argv[2]
proc=dict(pid=os.getpid(),bootId='fixture-boot',startTicks=1,commandSha256=hashlib.sha256(b'\\0'.join(os.fsencode(v) for v in sys.orig_argv)+b'\\0').hexdigest())
raw=json.dumps(dict(claimSha256=claim_sha,process=proc),sort_keys=True,separators=(',',':')).encode()
fd=os.open(str(folder/'ready.json'),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600);os.write(fd,raw);os.fsync(fd);os.close(fd)
time.sleep(20)
"""
        def command(hashes,entry,args):
            self.assertEqual(entry,'nativeController');self.assertEqual(args[:2],['owned-control','gateway-job-run'])
            result=[sys.executable,'-c',code,str(f.run/'native-gateway-enable-job'),sha(canonical(claim))];commands.append(result);return result
        def probe(pid):
            try:os.kill(pid,0)
            except ProcessLookupError:return None
            return dict(pid=pid,bootId='fixture-boot',startTicks=1,commandSha256=sha(b'\0'.join(os.fsencode(v) for v in commands[0])+b'\0'))
        with patch.object(worker_control.time,'time',return_value=f.now),patch.object(worker_control,'__file__',str(f.run/'worker_control.py')),patch.object(bootstrap,'require_verified'),patch.object(bootstrap,'current_hashes',return_value=f.plan['sourceHashes']),patch.object(bootstrap,'command',side_effect=command),patch.object(collector,'process_identity',side_effect=probe):
            started=__import__('time').monotonic();launch=worker_control.native_job_control('launch',f.run,dict(claim=claim))
            self.assertLess(time.monotonic()-started,4)
            self.assertEqual(len(launch.pop('resultKey')),64)
            pid=launch['record']['process']['pid']
            try:
                payload=dict(claim=claim,launch=launch)
                self.assertFalse((f.run/'commands'/'enable.json').exists())
                self.assertEqual(worker_control.native_job_control('status',f.run,payload),dict(status='PENDING'))
                self.assertEqual(worker_control.native_job_control('trigger',f.run,payload)['status'],'TRIGGERED')
                # A detached rollout outlives the ordinary 5s action without a held transport.
                with patch.object(worker_control.time,'monotonic',return_value=time.monotonic()+6.):
                    self.assertEqual(worker_control.native_job_control('status',f.run,payload),dict(status='PENDING'))
                with self.assertRaises((ValueError,FileExistsError)):worker_control.native_job_control('trigger',f.run,payload)
                bad=copy.deepcopy(payload);bad['launch']['record']['process']['pid']+=1
                with self.assertRaises(ValueError):worker_control.native_job_control('status',f.run,bad)
                # A status-shaped JSON at an unrelated/replaced inode is not an issued result.
                output=f.run/'native-gateway-enable-job'/'result.json';output.rename(output.with_suffix('.old'))
                write(output,dict(status='VERIFIED'))
                with self.assertRaises(ValueError):worker_control.native_job_control('status',f.run,payload)
                self.assertEqual(len(commands),1)
            finally:
                os.kill(pid,signal.SIGTERM)
                try:os.waitpid(pid,0)
                except ChildProcessError:pass

    def test_native_helper_is_verified_before_an_isolated_entry_can_import_it(self):
        self.assertEqual(bootstrap.NATIVE_SOURCES.get('workerControl'),'worker_control.py')
        self.assertNotIn('workerControl',bootstrap.SOURCES)
        from test_pgcat_native_controller import Fixture
        import subprocess,sys
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder).resolve();root.chmod(0o700);f=Fixture(root)
            code=bootstrap.ROOT.replace('sys.argv=[p]+sys.argv[3:]\nsys.exit(m.main())\n',"m.install(__import__('json').loads(sys.argv[3]))\nimport worker_control\nprint(worker_control.__verified_source_sha256__)\n")
            cmd=[sys.executable,'-I','-S','-c',code,str(f.run/'pgcat_source_bootstrap.py'),f.plan['sourceHashes']['sourceBootstrap'],json.dumps(f.plan['sourceHashes'])]
            result=subprocess.run(cmd,capture_output=True,timeout=4)
            self.assertEqual(result.returncode,0,result.stderr.decode());self.assertEqual(result.stdout.strip().decode(),f.plan['sourceHashes']['workerControl'])
            (f.run/'worker_control.py').write_text('raise RuntimeError("unreviewed")')
            self.assertNotEqual(subprocess.run(cmd,capture_output=True,timeout=4).returncode,0)

    def test_native_transition_reuses_helper_and_preserves_actual_set_original(self):
        import worker_control
        self.assertTrue(hasattr(worker_control,'native_transition'),'native helper bridge missing')
        from test_pgcat_native_closure import GatewayClosureTests
        helper=GatewayClosureTests();helper.setUp();self.addCleanup(helper.doCleanups)
        adapter=helper.adapter();adapter.deadline=__import__('time').monotonic()+4
        receipt,runtime=worker_control.native_transition(adapter,True,lambda:helper.f.now)
        self.assertEqual(receipt['resourceVersion'],'2')
        self.assertTrue(runtime['verification']['replacementEnabled'])
        self.assertTrue((helper.f.run/'control'/'enable.set.json').exists())
        self.assertEqual([a[1] for a in helper.calls if a[0]==str(helper.gate)],['set','rollout','verify'])

    def test_missing_original_launch_cannot_status_trigger_or_spawn(self):
        import worker_control
        self.assertTrue(hasattr(worker_control,'native_job_control'),'native detached bridge missing')
        with tempfile.TemporaryDirectory() as folder:
            run=Path(folder).resolve();run.chmod(0o700)
            with patch.object(bootstrap,'require_verified'),patch('subprocess.Popen',side_effect=AssertionError('spawn')):
                for verb in ('trigger','status'):
                    with self.assertRaises((ValueError,OSError,KeyError)):worker_control.native_job_control(verb,run,dict(action='native-gateway-enable',pid=os.getpid()))


if __name__=='__main__':unittest.main()
