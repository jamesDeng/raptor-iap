from pgcat_lane_protocol import monotonic
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import unittest
import uuid
from unittest.mock import patch

import pgcat_lane_client as client_module
from pgcat_lane_client import LaneClient
from pgcat_lane_protocol import Refused, encode, publish_request
from pgcat_lane_supervisor import read_json, save_once
import test_pgcat_lane_supervisor as supervisor_tests


class ClientTests(unittest.TestCase):
    private_json = supervisor_tests.SupervisorTests.private_json
    receipt = supervisor_tests.SupervisorTests.receipt

    def setUp(self):
        guard=patch.object(client_module,'_require_verified');guard.start();self.addCleanup(guard.stop)
        command=patch('pgcat_source_bootstrap.command',side_effect=lambda hashes,entry,args:
            [sys.executable,str(Path(__file__).with_name({'runner':'pgcat_lane_runner.py','laneWrapper':'pgcat_workbench_lane.py'}[entry]))]+args)
        command.start();self.addCleanup(command.stop)
        supervisor_tests.SupervisorTests.setUp(self)
        self.owner.close()
        (self.global_dir/'active-run').unlink()  # Only this test's isolated fake owner.
        real_spawn = subprocess.Popen
        global_dir = str(self.global_dir)
        def spawn(argv, **kwargs):
            bootstrap = ('import runpy,sys; from pathlib import Path; sys.path.insert(0,str(Path(sys.argv[1]).parent)); '
                'import pgcat_lane_protocol as p; p._release_admission=lambda *a: None; '
                'import pgcat_lane_supervisor as s; s._require_verified=lambda: None; s.GLOBAL_ROOT=Path('+repr(global_dir)+'); '
                'p=sys.argv[1]; sys.argv=sys.argv[1:]; runpy.run_path(p,run_name="__main__")')
            return real_spawn([sys.executable, '-c', bootstrap] + argv[1:], **kwargs)
        with patch.object(client_module.subprocess, 'Popen', spawn):
            self.client = LaneClient.start(self.bindings, 'disabled-rehearsal', self.base/'client')
        self.addCleanup(self.cleanup_client)

    def cleanup_client(self):
        self.client.close()
        try: self.client.process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            self.client.process.kill(); self.client.process.wait()

    def call(self):
        return self.client.call('baseline-status', read_json(self.root/'receipt.json'), monotonic()+4.)

    def seed_prepared_status(self):
        request=dict(requestId=str(uuid.uuid4()),runId=self.client.run_id,
            supervisorId=self.client.supervisor_id,action='pending-status',
            feedSession=self.session,minimumSequence=1,receiptIdentity='e'*64,
            target=self.target,bundleSha256=self.client.bundle,
            nonce=str(uuid.uuid4()),deadlineMonotonic=monotonic()+5.)
        result=dict(state='complete',category='ok',payload={'status':'PREPARED'})
        with self.client.journal._transaction() as db:
            db.execute('UPDATE run SET supervisor=?,session=?',
                       (self.client.supervisor_id,self.session))
            db.execute('INSERT INTO requests VALUES (?,?,?,?,?,?)',
                       (request['requestId'],self.client.run_id,request['nonce'],
                        encode(request).decode(),'complete',encode(result).decode()))
        return request['requestId']

    def test_direct_pending_proof_requires_publication_after_prepared_marker(self):
        self.receipt(1,desired=3)
        prepared_id=self.seed_prepared_status()
        # Sequence 2 exists, but its observation predates the durable marker.
        self.receipt(2,desired=3)
        later_time=time.time()+1
        with patch('pgcat_lane_protocol.time.time',return_value=later_time):
            marker=self.client.journal.record_prepared(prepared_id)
        self.assertLess(read_json(self.root/'receipt.json')['observedAt'],marker['sealedAt'])
        with self.assertRaises(Refused):
            self.client.call('pending-proof',read_json(self.root/'receipt.json'),monotonic()+4.)
        with self.client.journal._connection() as db:
            admitted=db.execute("SELECT count(*) FROM requests WHERE json_extract(request,'$.action')='pending-proof'").fetchone()[0]
        self.assertEqual(admitted,0)

    def test_direct_pending_proof_accepts_later_publication(self):
        self.receipt(1,desired=3)
        prepared_id=self.seed_prepared_status()
        with patch('pgcat_lane_protocol.time.time',return_value=time.time()-1):
            self.client.journal.record_prepared(prepared_id)
        self.receipt(2,desired=3)
        receipt=read_json(self.root/'receipt.json')
        proof=dict(runId=self.client.run_id,feedSession=self.session,newId='i-new-c',
            originalInstanceIds=self.target['originalInstanceIds'],sequence=1,
            observedAt=receipt['observedAt']-1,resourceVersion='11',
            clientRosterSha256='b'*64,evidenceSha256='c'*64,
            sourceSha256=self.hashes['pendingProofWorker'])
        self.cli.write_text('#!'+sys.executable+'\nprint('+repr(json.dumps(proof))+')\n')
        self.cli.chmod(0o700)
        result=self.client.call('pending-proof',receipt,monotonic()+4.)
        self.assertEqual(result['payload'],proof)
        self.assertEqual(self.client.journal.pending_binding(),proof)

    def test_pending_prepared_is_not_reported_before_second_marker_commit(self):
        request_id=self.seed_prepared_status()
        result=dict(state='complete',category='ok',payload={'status':'PREPARED'})
        self.assertFalse(self.client._prepared_result_ready('pending-status',request_id,result))
        self.client.journal.record_prepared(request_id)
        self.assertTrue(self.client._prepared_result_ready('pending-status',request_id,result))

    def receipts(self, status='INCONCLUSIVE'):
        feed = subprocess.Popen([sys.executable, '-c', 'pass'])
        feed.wait()
        exit_receipt = dict(pid=feed.pid, exitCode=0, category='ok', feedSession=self.session, sequence=1)
        assessment = dict(status=status, mode='disabled-rehearsal', feedSession=self.session,
                          endSequence=1, evidenceSha256='e'*64)
        return feed, exit_receipt, assessment

    def test_cancel_storage_failure_still_closes_pipe(self):
        with patch.object(self.client.journal, 'cancel', side_effect=Refused('storage')):
            with self.assertRaises(Refused):
                self.client.call('baseline-status', {}, monotonic()+4.)
        self.assertIsNone(self.client.liveness_fd)
        self.client.process.wait(timeout=3)

    def test_finish_cancel_storage_failure_still_closes_pipe(self):
        receipts = self.receipts()
        self.client.process.kill(); self.client.process.wait()
        with patch.object(self.client.journal, 'cancel', side_effect=Refused('storage')):
            with self.assertRaises(Refused): self.client.finish(*receipts)
        self.assertIsNone(self.client.liveness_fd)

    def test_finish_publish_storage_failure_still_closes_pipe(self):
        receipts = self.receipts()
        with patch.object(client_module, 'publish_finish', side_effect=Refused('storage')), \
                patch.object(self.client.journal, 'cancel', side_effect=Refused('storage')):
            with self.assertRaises(Refused): self.client.finish(*receipts)
        self.assertIsNone(self.client.liveness_fd)
        self.client.process.wait(timeout=3)

    def test_startup_receipt_is_not_visible_before_write(self):
        self.client.finish(*self.receipts()); self.client.process.wait(timeout=3)
        (self.global_dir/'active-run').unlink()  # Exact fake run, isolated test only.
        real_spawn = subprocess.Popen
        children = []
        def spawn(argv, **kwargs):
            bootstrap = ("import runpy,sys,time,os; from pathlib import Path; "
                "sys.path.insert(0,str(Path(sys.argv[1]).parent)); "
                "import pgcat_lane_protocol as p; p._release_admission=lambda *a:None; "
                "import pgcat_lane_supervisor as s; s._require_verified=lambda:None; s.GLOBAL_ROOT=Path("+repr(str(self.global_dir))+"); "
                "original=s.write_all; "
                "s.write_all=lambda fd,raw:(time.sleep(.15),original(fd,raw))[1] "
                "if b'supervisorId' in raw else original(fd,raw); "
                "p=sys.argv[1]; sys.argv=sys.argv[1:]; runpy.run_path(p,run_name='__main__')")
            child = real_spawn([sys.executable,'-c',bootstrap]+argv[1:], **kwargs)
            children.append(child)
            return child
        try:
            with patch.object(client_module.subprocess, 'Popen', spawn):
                new_client = LaneClient.start(self.bindings, 'disabled-rehearsal', self.base/'slow-start')
            self.assertEqual(new_client.journal.run_snapshot()['runState'], 'active')
            new_client.close()
        finally:
            for child in children:
                try: child.wait(timeout=3)
                except subprocess.TimeoutExpired: child.kill(); child.wait()

    def test_start_failure_cancel_storage_failure_closes_pipe(self):
        readers = []
        real_pipe = os.pipe
        def pipe():
            reader, writer = real_pipe()
            readers.append(os.dup(reader))
            return reader, writer
        try:
            with patch.object(client_module.os, 'pipe', pipe), \
                    patch.object(client_module.subprocess, 'Popen', side_effect=OSError()), \
                    patch.object(client_module.Journal, 'cancel', side_effect=Refused('storage')):
                with self.assertRaises(Refused):
                    LaneClient.start(self.bindings, 'disabled-rehearsal', self.base/'failed-start')
            import select
            self.assertTrue(select.select(readers, [], [], 0)[0])
            self.assertEqual(os.read(readers[0], 1), b'')
        finally:
            for fd in readers: os.close(fd)

    def test_private_startup_and_exact_committed_result(self):
        self.assertFalse(os.get_inheritable(self.client.liveness_fd))
        self.assertEqual(os.getsid(self.client.process.pid), self.client.process.pid)
        self.assertEqual((self.client.root/'bindings.json').stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.client.root.stat().st_mode & 0o777, 0o700)
        result = self.call()
        self.assertEqual(result, {'state':'complete','category':'ok','payload':{'status':'PREPARED','phase':'baseline'}})
        with self.client.journal._connection() as db:
            row = db.execute('SELECT request_id FROM requests').fetchone()
        snap = self.client.journal.snapshot(row['request_id'])
        self.assertEqual(snap['runId'], self.client.run_id)
        self.assertEqual(snap['request']['supervisorId'], self.client.supervisor_id)
        with self.assertRaises(Refused): publish_request(self.client.root, snap['request'])

    def test_in_progress_marker_is_not_a_published_request(self):
        real_link = os.link
        def slow_link(source, destination, **kwargs):
            if destination.endswith('.ready'):
                time.sleep(.08)  # Let the real runner scan the staged marker.
            return real_link(source, destination, **kwargs)
        with patch('pgcat_lane_protocol.os.link', slow_link):
            result = self.call()
        self.assertEqual(result['state'], 'complete')

    def test_published_marker_two_link_interval_does_not_cancel(self):
        real_link = os.link
        def linked_pause(source, destination, **kwargs):
            result = real_link(source, destination, **kwargs)
            if destination.endswith('.ready'): time.sleep(.08)
            return result
        with patch('pgcat_lane_protocol.os.link', linked_pause):
            result = self.call()
        self.assertEqual(result['state'], 'complete')
        with patch('pgcat_lane_protocol.os.link', linked_pause):
            result = self.client.finish(*self.receipts())
        self.assertEqual(result['runState'], 'finished')

    def test_disappearance_before_finish_marks_uncertainty(self):
        receipts = self.receipts()
        self.client.process.kill(); self.client.process.wait()
        with self.assertRaises(Refused): self.client.finish(*receipts)
        self.assertEqual(self.client.journal.run_snapshot()['runState'], 'uncertain')

    def test_disappearance_refuses_and_retains_global_record(self):
        self.client.process.kill(); self.client.process.wait()
        with self.assertRaises(Refused): self.call()
        self.assertEqual(self.client.journal.run_snapshot()['runState'], 'uncertain')
        self.assertTrue((self.global_dir/'active-run').exists())

    def test_cancel_after_complete_refuses_next_call_and_finish(self):
        self.call()
        self.client.journal.cancel('cancelled')
        with self.assertRaises(Refused): self.call()
        with self.assertRaises(Refused): self.client.finish(*self.receipts())
        self.assertEqual(self.client.journal.run_snapshot()['runState'], 'cancelled')

    def test_cancellation_during_complete_observation_refuses_result(self):
        original = self.client.journal.snapshot
        def snapshot(request_id):
            result = original(request_id)
            if result['state'] == 'complete': self.client.journal.cancel('cancelled')
            return result
        with patch.object(self.client.journal, 'snapshot', snapshot):
            with self.assertRaises(Refused): self.call()
        self.assertEqual(self.client.journal.run_snapshot()['runState'], 'cancelled')

    def test_finish_reconciles_stale_active_snapshot_after_durable_exit(self):
        receipts=self.receipts('FAIL')
        original=self.client.journal.run_snapshot
        stale=[None]
        publish=client_module.publish_finish
        def publish_then_mark(*args):
            stale[0]=original();publish(*args)
        def delayed_snapshot():
            state=stale[0] if stale[0] is not None else original()
            if stale[0] is not None:
                stale[0]=None
                self.assertEqual(state['runState'],'active')
                self.client.process.wait(timeout=3)
                self.assertEqual(original()['runState'],'finished')
            return state
        with patch.object(client_module,'publish_finish',publish_then_mark), \
                patch.object(self.client.journal,'run_snapshot',delayed_snapshot):
            result=self.client.finish(*receipts)
        self.assertEqual(result['assessment'],receipts[2])
        self.assertEqual(result['assessment']['status'],'FAIL')

    def test_finish_reconciliation_rejects_changed_sealed_receipt(self):
        receipts=self.receipts()
        original=self.client.journal.run_snapshot
        stale=[None]
        publish=client_module.publish_finish
        def publish_then_mark(*args):stale[0]=original();publish(*args)
        def changed_snapshot():
            state=stale[0] if stale[0] is not None else original()
            if stale[0] is not None:
                stale[0]=None
                self.assertEqual(state['runState'],'active')
                self.client.process.wait(timeout=3)
                path=self.client.root/'assessment.json'
                value=json.loads(path.read_text());value['status']='FAIL'
                path.write_text(json.dumps(value))
            return state
        with patch.object(client_module,'publish_finish',publish_then_mark), \
                patch.object(self.client.journal,'run_snapshot',changed_snapshot):
            with self.assertRaises(Refused):self.client.finish(*receipts)
        self.assertIsNone(self.client.liveness_fd)

    def test_finish_stale_snapshot_exit_without_seal_refused(self):
        receipts=self.receipts()
        original=self.client.journal.run_snapshot
        published=[False]
        def unsealed_publish(*args):published[0]=True
        def delayed_snapshot():
            state=original()
            if published[0] and state['runState']=='active':
                self.client.process.kill();self.client.process.wait(timeout=3)
            return state
        with patch.object(client_module,'publish_finish',unsealed_publish), \
                patch.object(self.client.journal,'run_snapshot',delayed_snapshot):
            with self.assertRaises(Refused):self.client.finish(*receipts)
        self.assertNotEqual(original()['runState'],'finished')

    def test_global_record_blocks_a_new_run_after_finish(self):
        self.client.finish(*self.receipts())
        self.client.process.wait(timeout=3)
        # Use a direct supervisor construction so the test's private global root applies.
        from pgcat_lane_protocol import Journal
        from pgcat_lane_supervisor import LaneSupervisor
        root = self.base/'second'; root.mkdir(mode=0o700)
        journal = Journal(root)
        journal.create_run(str(uuid.uuid4()), 'disabled-rehearsal', self.target,
                           dict(self.hashes, bundleSha256=self.bundle))
        with self.assertRaises(Refused): LaneSupervisor(root, self.bindings, journal)

    def test_finish_is_one_use_and_does_not_infer_pass(self):
        self.call()
        result = self.client.finish(*self.receipts('FAIL'))
        self.assertEqual(result['runState'], 'finished')
        self.assertEqual(result['assessment']['status'], 'FAIL')
        self.assertEqual(self.client.process.wait(timeout=3), 0)
        for name in ('feed-exit.json','assessment.json','finish.ready'):
            self.assertEqual((self.client.root/name).stat().st_mode & 0o777, 0o600)
        self.assertTrue((self.global_dir/'active-run').exists())
        with self.assertRaises(Refused): self.client.finish(*self.receipts())
        with self.assertRaises(Refused): self.call()

    def test_eof_without_finish_is_uncertain(self):
        self.client.close(); self.client.process.wait(timeout=3)
        self.assertEqual(self.client.journal.run_snapshot()['runState'], 'uncertain')
        self.assertTrue((self.global_dir/'active-run').exists())

    def test_malformed_finish_marker_is_uncertain(self):
        save_once(self.client.root, 'finish.ready', {'runId':self.client.run_id})
        self.client.process.wait(timeout=3)
        self.assertEqual(self.client.journal.run_snapshot()['runState'], 'uncertain')

    def test_live_feed_and_mismatched_exit_are_rejected(self):
        feed = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(5)'])
        try:
            _, receipt, assessment = self.receipts()
            receipt['pid'] = feed.pid
            with self.assertRaises(Refused): self.client.finish(feed, receipt, assessment)
        finally: feed.kill(); feed.wait()
        receipt['exitCode'] = 0
        with self.assertRaises(Refused): self.client.finish(feed, receipt, assessment)
        self.assertFalse((self.client.root/'finish.ready').exists())

    def test_assessment_schema_and_identity_rejected(self):
        feed, receipt, assessment = self.receipts()
        for changes in ({'status':'finished'}, {'mode':'no-scale'}, {'endSequence':2}, {'extra':True}):
            with self.subTest(changes=changes), self.assertRaises(Refused):
                self.client.finish(feed, receipt, dict(assessment, **changes))
        self.assertFalse((self.client.root/'finish.ready').exists())

    def test_reused_sequence_cannot_dispatch_twice(self):
        self.call()
        with self.assertRaises(Refused): self.call()
        self.assertNotEqual(self.client.journal.run_snapshot()['runState'], 'finished')




class PaidFeedBarrierTests(unittest.TestCase):
    def setUp(self):
        from test_pgcat_feed_binding import FeedBindingTests
        fixture=FeedBindingTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        self.fixture=fixture;self.client=fixture.client;self.client.mode='disabled-rehearsal'
        self.argv=fixture.argv[:-2];self.argv[self.argv.index('--max-seconds')+1]='75';fixture.argv=self.argv
        from pgcat_lane_protocol import encode
        from pgcat_lane_supervisor import save_once
        save_once(self.client.root,'bindings.json',self.client.bindings)
        with self.client.journal._transaction() as db:db.execute("UPDATE run SET mode='disabled-rehearsal'")
        self.wall=patch('pgcat_lane_client.time.time',return_value=1793188800.) # 2026-10-28 20:00 Shanghai
        self.wall.start();self.addCleanup(self.wall.stop)
        admission=patch('pgcat_lane_protocol._release_admission');admission.start();self.addCleanup(admission.stop)

    def test_paid_actual_child_executes_only_after_original_raw_manifest_is_bound(self):
        from pgcat_lane_protocol import digest
        original=self.client.journal.bind_feed
        raw=self.fixture.manifest.read_bytes()
        def bind(value,**kwargs):
            self.assertFalse(self.fixture.marker.exists());self.assertIsNone(self.client.feed_process.poll())
            self.assertEqual(kwargs['manifest_raw'],raw)
            original(value,**kwargs)
        with patch.object(self.client.journal,'bind_feed',side_effect=bind):
            child=self.client.start_feed(self.argv,{'PATH':os.defpath},self.fixture.manifest)
        child.wait(timeout=3);self.assertTrue(self.fixture.marker.exists())
        self.client.journal.cancel('incomplete') # Cleanup exit is still evidence after uncertainty.
        self.client.confirm_feed_exit(child)
        with self.client.journal._connection() as db:
            self.assertEqual(bytes(db.execute('SELECT raw FROM feed_manifest').fetchone()['raw']),raw)
        with self.assertRaises(Refused):self.client.confirm_feed_exit(child)
        other=subprocess.Popen([sys.executable,'-c','pass']);other.wait(timeout=3)
        with self.assertRaises(Refused):self.client.confirm_feed_exit(other)

    def test_paid_persistence_failure_or_eof_never_executes_provider(self):
        for kind in ('persist','eof'):
            with self.subTest(kind=kind):
                self.client.feed_process=None
                if kind=='persist':
                    context=patch.object(self.client.journal,'bind_feed',side_effect=Refused('storage'))
                else:
                    real=os.write
                    context=patch('pgcat_lane_client.os.write',side_effect=lambda fd,data:0 if data==b'1' else real(fd,data))
                with context,self.assertRaises(Refused):
                    self.client.start_feed(self.argv,{'PATH':os.defpath},self.fixture.manifest)
                self.assertIsNotNone(self.client.feed_process)
                self.client.feed_process.wait(timeout=3)
                self.assertFalse(self.fixture.marker.exists())

    def test_paid_gate_timeout_and_live_handle_refuse_without_provider_execution(self):
        original=self.client.journal.bind_feed
        def delayed(value,**kwargs):
            original(value,**kwargs)
            with self.assertRaises(Refused):self.client.confirm_feed_exit(self.client.feed_process)
            self.client.feed_process.wait(timeout=7)
        with patch.object(self.client.journal,'bind_feed',side_effect=delayed),self.assertRaises(Refused):
            self.client.start_feed(self.argv,{'PATH':os.defpath},self.fixture.manifest)
        self.assertEqual(self.client.feed_process.poll(),125)
        self.assertFalse(self.fixture.marker.exists())
        self.client.confirm_feed_exit(self.client.feed_process)

    def test_paid_cutoff_is_rechecked_after_source_validation_before_actual_spawn(self):
        from datetime import datetime,timezone,timedelta
        late=datetime(2026,10,28,21,30,tzinfo=timezone(timedelta(hours=8))).timestamp()
        # Change the active clock through its patched callable, after cap parsing.
        with patch('pgcat_lane_client.time.time',side_effect=lambda:getattr(self,'now',1793188800.)), \
                patch('pgcat_lane_client._require_verified',side_effect=lambda:setattr(self,'now',late)):
            with self.assertRaises(Refused):self.client.start_feed(self.argv,{'PATH':os.defpath},self.fixture.manifest)
        self.assertIsNone(getattr(self.client,'feed_process',None));self.assertFalse(self.fixture.marker.exists())

    def test_paid_exit_confirmation_rejects_changed_bound_source_or_independent_binding(self):
        child=self.client.start_feed(self.argv,{'PATH':os.defpath},self.fixture.manifest);child.wait(timeout=3)
        old=self.client.bindings['sourceHashes']['providerFeed']
        self.client.bindings['sourceHashes']['providerFeed']='0'*64
        with self.assertRaises(Refused):self.client.confirm_feed_exit(child)
        self.client.bindings['sourceHashes']['providerFeed']=old
        self.client.feed_binding['argvSha256']='0'*64
        with self.assertRaises(Refused):self.client.confirm_feed_exit(child)

    def test_paid_largest_cap_is_allowed_only_with_remaining_window(self):
        self.argv[self.argv.index('--max-seconds')+1]='5400'
        child=self.client.start_feed(self.argv,{'PATH':os.defpath},self.fixture.manifest);child.wait(timeout=3)
        self.assertTrue(self.fixture.marker.exists());self.client.confirm_feed_exit(child)

    def test_paid_exact_argv_cap_cutoff_and_guard_refuse_before_spawn(self):
        from datetime import datetime,timezone,timedelta
        good=1793188800.
        for cap,hour,minute in (('74',20,0),('5401',20,0),('075',20,0),('75.0',20,0),
                                ('75',21,30),('5400',21,29)):
            with self.subTest(cap=cap,hour=hour,minute=minute):
                now=datetime(2026,10,28,hour,minute,tzinfo=timezone(timedelta(hours=8))).timestamp()
                argv=list(self.argv);argv[argv.index('--max-seconds')+1]=cap
                with patch('pgcat_lane_client.time.time',return_value=now),self.assertRaises(Refused):
                    self.client.start_feed(argv,{'PATH':os.defpath},self.fixture.manifest)
                self.assertIsNone(getattr(self.client,'feed_process',None))
        for suffix in (['--no-scale-target','{}'],['--arbitrary']):
            with self.assertRaises(Refused):self.client.start_feed(self.argv+suffix,{'PATH':os.defpath},self.fixture.manifest)
        with patch('pgcat_lane_protocol._release_admission',side_effect=Refused()),self.assertRaises(Refused):
            self.client.start_feed(self.argv,{'PATH':os.defpath},self.fixture.manifest)
        self.assertIsNone(getattr(self.client,'feed_process',None))


if __name__ == '__main__': unittest.main()


class PostStopClientTests(unittest.TestCase):
    def setUp(self):
        import test_pgcat_lane_protocol as fixtures
        from unittest.mock import Mock
        from pgcat_lane_protocol import digest
        f=fixtures.PostStopJournalTests();f.setUp();self.addCleanup(f.doCleanups);self.f=f
        c=LaneClient();self.client=c;c.root=f.root;c.bindings=f.f.outer['laneBindings'];c.mode='disabled-rehearsal'
        c.run_id=f.f.run;c.supervisor_id=f.supervisor;c.bundle=digest(c.bindings['sourceHashes']);c.journal=f.journal
        c.process=Mock();c.process.poll.return_value=None
        reader,c.liveness_fd=os.pipe();os.close(reader);self.addCleanup(c.close)

    def finish_published(self,root,request):
        publish_request(root,request);self.f.request=request
        self.f.journal.admit(request);self.f.journal.post_stop_dispatch(request['requestId'])
        result=self.f.result();self.f.journal.seal(request['requestId'],result,post_stop_source_sha=result['payload']['sourceSha256'])
        return request['requestId']

    def test_noarg_client_uses_frozen_stop_and_returns_sealed_result(self):
        with patch.object(client_module,'publish_request',side_effect=self.finish_published), \
             patch.object(client_module.lane,'read_receipt',side_effect=AssertionError('live receipt')):
            result=self.client.post_stop_call()
        self.assertEqual(result,self.f.result());self.assertIsNotNone(self.client.liveness_fd)
        with self.assertRaises(Refused):self.client.post_stop_call()
        self.assertIsNotNone(self.client.liveness_fd)
        with self.assertRaises(Refused):self.client.call('baseline-status',self.f.f.receipt,monotonic()+5.)
        self.assertIsNotNone(self.client.liveness_fd)

    def test_pre_call_final_age_and_late_return_refuse(self):
        with patch.object(client_module.time,'time',return_value=self.f.f.receipt['observedAt']+24):
            with self.assertRaises(Refused):self.client.post_stop_call()
        self.assertIsNone(self.client.liveness_fd)

    def test_uncertain_first_attempt_closes_and_never_retries(self):
        def timeout(root,request):
            publish_request(root,request);self.f.journal.admit(request)
            self.f.journal.post_stop_dispatch(request['requestId']);self.f.journal.cancel('timeout')
        with patch.object(client_module,'publish_request',side_effect=timeout):
            with self.assertRaises(Refused):self.client.post_stop_call()
        self.assertIsNone(self.client.liveness_fd)
        with self.f.journal._connection() as db:
            self.assertEqual(db.execute('SELECT dispatch_claimed FROM post_stop_admission').fetchone()[0],1)
