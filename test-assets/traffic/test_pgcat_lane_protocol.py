from pgcat_lane_protocol import monotonic
import copy
import json
from pathlib import Path
import tempfile
import time
import unittest
import uuid

from pgcat_lane_protocol import Journal, Refused, publish_request, publish_finish


class ProtocolTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.target = dict(accountId='account', regionId='region', workerId='worker',
                           scalingGroupId='asg-t4necb4dfmu6o3z2rn0g', nlbServerGroupId='nlb',
                           originalInstanceIds=['instance-a', 'instance-b'])
        self.run = str(uuid.uuid4())
        self.session = str(uuid.uuid4())
        self.supervisor = str(uuid.uuid4())
        self.journal = Journal(self.root)
        self.journal.create_run(self.run, 'no-scale', self.target, {'bundleSha256': 'a'*64})

    def request(self, **changes):
        value = dict(requestId=str(uuid.uuid4()), runId=self.run,
                     supervisorId=self.supervisor, action='baseline-status',
                     feedSession=self.session, minimumSequence=1,
                     receiptIdentity='b'*64, target=copy.deepcopy(self.target),
                     bundleSha256='a'*64, nonce=str(uuid.uuid4()),
                     deadlineMonotonic=monotonic()+5.0)
        value.update(changes)
        return value

    def admit(self, request):
        self.assertEqual(publish_request(self.root, request), request['requestId'])
        self.journal.admit(request)

    def test_no_scale_stop_binds_only_run_session_sequence_verdict(self):
        from pgcat_lane_protocol import validate_no_scale_stop,no_scale_stop_ack,encode
        import hashlib
        intent=dict(schemaVersion=1,runId=self.run,feedSession=self.session,
            minimumSequence=3,verdictSha256='a'*64)
        validate_no_scale_stop(intent)
        final=dict(feedSession=self.session,sequence=4,observedAt=123.)
        ack=no_scale_stop_ack(intent,final)
        self.assertEqual(ack['intentSha256'],hashlib.sha256(encode(intent)).hexdigest())
        self.assertEqual(ack['finalReceiptSha256'],hashlib.sha256(encode(final)).hexdigest())
        for changes in (dict(schemaVersion=True),dict(minimumSequence=True),dict(minimumSequence=0),
                        dict(verdictSha256='wrong'),dict(command='arbitrary'),dict(runId='wrong')):
            with self.subTest(changes=changes),self.assertRaises(Refused):
                validate_no_scale_stop(dict(intent,**changes))
        for changes in (dict(sequence=2),dict(sequence=5),dict(sequence=True),dict(feedSession=str(uuid.uuid4()))):
            with self.subTest(changes=changes),self.assertRaises(Refused):
                no_scale_stop_ack(intent,dict(final,**changes))

    def test_deadline_clock_is_shared_with_detached_process(self):
        import subprocess
        import sys
        import pgcat_lane_protocol as protocol
        before = protocol.monotonic()
        child = float(subprocess.check_output([sys.executable, '-c',
            'from pgcat_lane_protocol import monotonic; print(monotonic())'],
            cwd=Path(__file__).resolve().parent))
        after = protocol.monotonic()
        self.assertLessEqual(before, child)
        self.assertLessEqual(child, after)

    def finish_receipts(self):
        from pgcat_lane_protocol import digest,no_scale_assessment,encode
        from pgcat_lane_supervisor import save_once
        feed = dict(pid=123, exitCode=0, category='ok', feedSession=self.session, sequence=1)
        hashes=dict(lane={},local=dict(controller='a'*64,client='b'*64,runner='c'*64))
        with self.journal._transaction() as db:
            db.execute('UPDATE run SET hashes=?',(encode(dict(bundleSha256=digest({}))).decode(),))
        record=dict(schemaVersion=1,day='2026-10-27',runId=self.run,feedSession=self.session,
            targetSha256=digest(self.target),sourceSha256=digest(hashes),sourceHashes=hashes,
            clientIdentitySha256=None,bindingSha256='d'*64,workerVerdict=None,stop=None,localCredentialCleanup=False)
        save_once(self.root,'no-scale-proof.json',record)
        assessment=no_scale_assessment(record,self.journal.run_snapshot(),None,feed)
        binding=dict(runId=self.run,feedId=str(uuid.uuid4()),pid=123,bindingSha256='d'*64,argvSha256='a'*64)
        self.journal.bind_feed(binding)
        self.journal.confirm_feed_exit(dict(runId=self.run,feedId=binding['feedId'],pid=123,exitCode=0))
        publish_finish(self.root, self.run, feed, assessment)

    def test_finish_is_monotonic_with_valid_marker(self):
        self.finish_receipts()
        self.journal.cancel('incomplete')
        with self.assertRaises(Refused): self.journal.finish()
        self.assertEqual(self.journal.run_snapshot()['runState'], 'uncertain')

    def test_finish_rejects_unsealed_request_with_valid_marker(self):
        self.admit(self.request())
        self.finish_receipts()
        with self.assertRaises(Refused): self.journal.finish()
        self.assertEqual(self.journal.run_snapshot()['runState'], 'active')

    def test_finish_rejects_unpublished_staging_file(self):
        folder = self.root/'requests'; folder.mkdir(mode=0o700)
        (folder/'partial.request.json').write_text('{}')
        self.finish_receipts()
        with self.assertRaises(Refused): self.journal.finish()

    def test_finish_rejects_receipt_digest_change(self):
        self.finish_receipts()
        (self.root/'assessment.json').write_text('{}')
        with self.assertRaises(Refused): self.journal.finish()

    def test_finished_remains_closed_and_preserves_assessment(self):
        request = self.request(); self.admit(request)
        self.journal.seal(request['requestId'], dict(state='complete',category='ok',payload={}))
        self.finish_receipts()
        result = self.journal.finish()
        self.assertEqual(result['runState'], 'finished')
        self.assertEqual(result['assessment']['status'], 'INCONCLUSIVE')
        self.journal.cancel('cancelled')
        self.assertEqual(self.journal.run_snapshot()['runState'], 'finished')
        with self.assertRaises(Refused): self.journal.finish()
        with self.assertRaises(Refused): self.admit(self.request())

    def test_private_publication_and_database(self):
        request = self.request()
        self.admit(request)
        for path, mode in [(self.root/'requests', 0o700),
                           (self.root/'requests'/(request['requestId']+'.request.json'), 0o600),
                           (self.root/'requests'/(request['requestId']+'.ready'), 0o600),
                           (self.root/'journal.sqlite3', 0o600)]:
            self.assertEqual(path.stat().st_mode & 0o777, mode)
        self.assertEqual(self.journal.snapshot(request['requestId'])['state'], 'dispatching')

    def test_unpublished_request_never_admits(self):
        request = self.request()
        folder = self.root/'requests'
        folder.mkdir(mode=0o700, exist_ok=True)
        path = folder/(request['requestId']+'.request.json')
        path.write_text(json.dumps(request))
        path.chmod(0o600)
        with self.assertRaises(Refused):
            self.journal.admit(request)
        with self.assertRaises(Refused):
            self.journal.snapshot(request['requestId'])

    def test_duplicate_id_and_one_terminal_result(self):
        request = self.request()
        self.admit(request)
        with self.assertRaises(Refused):
            publish_request(self.root, request)
        with self.assertRaises(Refused):
            self.journal.admit(request)
        self.journal.seal(request['requestId'], {'state':'complete', 'category':'ok', 'payload':{'status':'done'}})
        with self.assertRaises(Refused):
            self.journal.seal(request['requestId'], {'state':'uncertain', 'category':'timeout', 'payload':{}})
        self.assertEqual(self.journal.snapshot(request['requestId'])['state'], 'complete')

    def test_run_bindings_are_immutable(self):
        for mode, target, hashes in [('disabled-rehearsal', self.target, {'bundleSha256':'a'*64}),
                                     ('no-scale', dict(self.target, workerId='different'), {'bundleSha256':'a'*64}),
                                     ('no-scale', self.target, {'bundleSha256':'c'*64})]:
            with self.assertRaises(Refused):
                self.journal.create_run(self.run, mode, target, hashes)
        for changes in [dict(target=dict(self.target, workerId='different')), dict(bundleSha256='c'*64),
                        dict(action='scaleout-two-to-three')]:
            request = self.request(**changes)
            publish_request(self.root, request)
            with self.assertRaises(Refused):
                self.journal.admit(request)

    def test_cancel_is_monotonic_and_blocks_success(self):
        request = self.request()
        self.admit(request)
        self.journal.cancel('cancelled')
        self.journal.cancel('timeout')
        with self.assertRaises(Refused):
            self.journal.seal(request['requestId'], {'state':'complete','category':'ok','payload':{}})
        self.journal.seal(request['requestId'], {'state':'uncertain','category':'cancelled','payload':{}})
        self.assertEqual(self.journal.snapshot(request['requestId'])['runState'], 'cancelled')
        with self.assertRaises(Refused):
            self.journal.create_run(self.run, 'no-scale', self.target, {'bundleSha256':'a'*64})
        later = self.request()
        publish_request(self.root, later)
        with self.assertRaises(Refused):
            self.journal.admit(later)

    def test_invalid_records_refused_without_leaking_fields(self):
        for changes in [dict(extra='secret'), dict(deadlineMonotonic=float('nan')),
                        dict(minimumSequence=True), dict(action=[]), dict(requestId='../escape')]:
            with self.assertRaises(Refused):
                publish_request(self.root, self.request(**changes))
        request = self.request()
        self.admit(request)
        for result in [dict(state='complete',category='ok',payload={'stderr':'secret'}),
                       dict(state='complete',category='ok',payload={'status':'192.0.2.1'}),
                       dict(state='complete',category='remote',payload={}),
                       dict(state='uncertain',category=[],payload={}),
                       dict(state='uncertain',category='remote',payload={'identity':{'pid':1}})]:
            with self.assertRaises(Refused):
                self.journal.seal(request['requestId'], result)

    def test_symlinks_modes_and_modified_publication_refused(self):
        request = self.request()
        publish_request(self.root, request)
        path = self.root/'requests'/(request['requestId']+'.request.json')
        path.chmod(0o644)
        with self.assertRaises(Refused):
            self.journal.admit(request)
        path.chmod(0o600)
        path.write_text(json.dumps(self.request(requestId=request['requestId'])))
        with self.assertRaises(Refused):
            self.journal.admit(request)
        path.unlink()
        path.symlink_to(self.root/'journal.sqlite3')
        with self.assertRaises(Refused):
            self.journal.admit(request)

    def collector_journal(self, mode):
        from unittest.mock import patch
        admission=patch('pgcat_lane_protocol._release_admission')
        admission.start();self.addCleanup(admission.stop)
        root=self.root/str(uuid.uuid4());root.mkdir(mode=0o700)
        journal=Journal(root)
        journal.create_run(self.run,mode,self.target,{'bundleSha256':'a'*64})
        return journal

    def collector_request(self, journal, action, sequence=1):
        request=self.request(action=action,minimumSequence=sequence)
        publish_request(journal.root,request)
        journal.admit(request)
        return request

    def collector_status(self, journal, action, payload, sequence):
        request=self.collector_request(journal,action,sequence)
        journal.seal(request['requestId'],dict(state='complete',category='ok',payload=payload))
        return request

    def test_collector_pair_requires_matching_journal_mode(self):
        for mode,allowed,denied in (
                ('no-scale',('collector-launch','collector-status'),
                    ('rehearsal-collector-launch','rehearsal-collector-status')),
                ('disabled-rehearsal',('rehearsal-collector-launch','rehearsal-collector-status'),
                    ('collector-launch','collector-status'))):
            for action in allowed:
                with self.subTest(mode=mode,action=action):
                    journal=self.collector_journal(mode)
                    self.collector_request(journal,action)
            for action in denied:
                with self.subTest(mode=mode,action=action),self.assertRaises(Refused):
                    self.collector_request(self.collector_journal(mode),action)

    def test_rehearsal_collector_launch_is_semantically_once(self):
        journal=self.collector_journal('disabled-rehearsal')
        request=self.collector_request(journal,'rehearsal-collector-launch')
        journal.seal(request['requestId'],dict(state='complete',category='ok',payload=dict(status='LAUNCHED')))
        with self.assertRaises(Refused):
            self.collector_request(journal,'rehearsal-collector-launch',2)
        with journal._connection() as db:
            self.assertEqual(db.execute('SELECT COUNT(*) FROM requests').fetchone()[0],1)

    def test_collector_exit_cannot_cross_profiles(self):
        identity=dict(pid=9,bootId='boot',startTicks=1,commandSha256='a'*64)
        for mode,action,other in (('no-scale','collector-status','disabled-rehearsal'),
                ('disabled-rehearsal','rehearsal-collector-status','no-scale')):
            with self.subTest(mode=mode):
                journal=self.collector_journal(mode)
                self.collector_status(journal,action,dict(status='ALIVE',identity=identity),1)
                exited=dict(status='EXITED',identity=identity,
                    exit=dict(category='completed',exitCode=0,completionSha256='c'*64))
                self.collector_status(journal,action,exited,2)
                self.assertEqual(journal.collector_exit(profile=mode)['payload'],exited)
                with self.assertRaises(Refused):journal.collector_exit(profile=other)
                with self.assertRaises(Refused):journal.collector_exit(profile='unknown')
                if mode=='no-scale':
                    self.assertEqual(journal.collector_exit()['payload'],exited)
                    self.collector_request(journal,'lane-verdict',3)
                else:
                    with self.assertRaises(Refused):journal.collector_exit()
                    with self.assertRaises(Refused):self.collector_request(journal,'lane-verdict',3)

    def test_rehearsal_status_requires_alive_and_unchanged_identity(self):
        identity=dict(pid=9,bootId='boot',startTicks=1,commandSha256='a'*64)
        journal=self.collector_journal('disabled-rehearsal')
        exited=dict(status='EXITED',identity=identity,
            exit=dict(category='completed',exitCode=0,completionSha256='c'*64))
        request=self.collector_request(journal,'rehearsal-collector-status')
        with self.assertRaises(Refused):
            journal.seal(request['requestId'],dict(state='complete',category='ok',payload=exited))
        journal.seal(request['requestId'],dict(state='complete',category='ok',
            payload=dict(status='ALIVE',identity=identity)))
        request=self.collector_request(journal,'rehearsal-collector-status',2)
        for payload in (dict(status='LAUNCHED'),dict(exited,identity=dict(identity,pid=10))):
            with self.subTest(payload=payload),self.assertRaises(Refused):
                journal.seal(request['requestId'],dict(state='complete',category='ok',payload=payload))
        journal.seal(request['requestId'],dict(state='complete',category='ok',payload=exited))
        request=self.collector_request(journal,'rehearsal-collector-status',3)
        with self.assertRaises(Refused):
            journal.seal(request['requestId'],dict(state='complete',category='ok',
                payload=dict(status='ALIVE',identity=identity)))

    def test_rehearsal_scaleout_requires_fixed_group(self):
        from unittest.mock import patch
        admission=patch("pgcat_lane_protocol._release_admission");admission.start();self.addCleanup(admission.stop)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            journal = Journal(root)
            wrong_target = dict(self.target, scalingGroupId='wrong-group')
            journal.create_run(self.run, 'disabled-rehearsal', wrong_target, {'bundleSha256':'a'*64})
            request = self.request(action='scaleout-two-to-three', target=wrong_target)
            publish_request(root, request)
            with self.assertRaises(Refused):
                journal.admit(request)

    def reconciliation_journal(self):
        from pgcat_lane_protocol import encode, original_roster, roster_sha
        journal=self.collector_journal('disabled-rehearsal')
        pending=dict(runId=self.run,feedSession=self.session,newId='candidate',
            originalInstanceIds=self.target['originalInstanceIds'],sequence=3,
            observedAt=time.time()-120,resourceVersion='11',clientRosterSha256='c'*64,
            evidenceSha256='d'*64,sourceSha256='e'*64)
        scalein=self.request(action='scalein-three-to-two',minimumSequence=8)
        with journal._transaction() as db:
            db.execute('UPDATE run SET session=?, hashes=?',(self.session,
                encode(dict(bundleSha256='a'*64,rehearsalContract='f'*64,pendingProofWorker='e'*64)).decode()))
            db.execute('INSERT INTO pending_binding VALUES (1,?)',(encode(pending).decode(),))
            db.execute('INSERT INTO requests VALUES (?,?,?,?,?,NULL)',
                (scalein['requestId'],self.run,scalein['nonce'],encode(scalein).decode(),'dispatching'))
        journal.seal(scalein['requestId'],dict(state='complete',category='ok',payload=dict(requestId='provider-ack')))
        receipt=dict(feedSession=self.session,sequence=9,mode='disabled-rehearsal',desired=2,
            observedAt=time.time()+.001,rosterSha256=roster_sha(original_roster(self.target['originalInstanceIds'])))
        return journal,pending,scalein,receipt

    def test_scalein_terminal_seals_actual_ack_and_reconcile_ignores_old_provenance_age(self):
        from unittest.mock import patch
        journal,pending,scalein,receipt=self.reconciliation_journal()
        ack=journal.scalein_ack()
        self.assertEqual(ack['requestId'],scalein['requestId'])
        self.assertEqual(ack['providerRequestId'],'provider-ack')
        self.assertEqual(ack['sequence'],8)
        with patch('pgcat_lane_protocol.time.time',return_value=receipt['observedAt']+60):
            # Old ACK/Pending remain provenance while the current publication is fresh.
            receipt['observedAt']+=60
            context=journal.reconcile_context(receipt)
        self.assertEqual(set(context),{'feedSession','sequence','observedAt','scaleinRequestId',
            'scaleinAcknowledgedAt','pendingBindingSha256'})
        self.assertEqual(context['scaleinRequestId'],scalein['requestId'])
        self.assertEqual(context['scaleinAcknowledgedAt'],ack['sealedAt'])
        ack['requestId']='changed'
        self.assertEqual(journal.scalein_ack()['requestId'],scalein['requestId'])

    def test_reconcile_context_refuses_missing_ack_and_bad_current_evidence(self):
        from unittest.mock import patch
        journal,pending,scalein,receipt=self.reconciliation_journal()
        now=receipt['observedAt']
        for changes in (dict(sequence=8),dict(sequence=True),dict(desired=3),dict(desired=True),
                dict(mode='no-scale'),dict(feedSession=str(uuid.uuid4())),dict(observedAt=now-3.5),
                dict(observedAt=now+1),dict(observedAt=float('nan')),dict(rosterSha256='0'*64)):
            with self.subTest(changes=changes),patch('pgcat_lane_protocol.time.time',return_value=now),self.assertRaises(Refused):
                journal.reconcile_context(dict(receipt,**changes))
        with journal._transaction() as db:db.execute('DELETE FROM scalein_ack')
        with self.assertRaises(Refused):journal.reconcile_context(receipt)

    def test_reconcile_context_rejects_wrong_run_session_and_pending_scope(self):
        from pgcat_lane_protocol import encode
        from unittest.mock import patch
        for changes in (dict(runId=str(uuid.uuid4())),dict(feedSession=str(uuid.uuid4())),
                        dict(originalInstanceIds=['different','original']),dict(sequence=True),
                        dict(sourceSha256='0'*64)):
            journal,pending,scalein,receipt=self.reconciliation_journal()
            with journal._transaction() as db:
                db.execute('UPDATE pending_binding SET proof=?',(encode(dict(pending,**changes)).decode(),))
            with patch('pgcat_lane_protocol.time.time',return_value=receipt['observedAt']),self.assertRaises(Refused):
                journal.reconcile_context(receipt)

    def test_reconcile_admit_rechecks_receipt_and_semantic_claim_is_atomic(self):
        from unittest.mock import patch
        journal,_,_,receipt=self.reconciliation_journal()
        request=self.request(action='reconcile-close',minimumSequence=9)
        publish_request(journal.root,request)
        with patch('pgcat_lane_protocol.time.time',return_value=receipt['observedAt']):
            journal.reconcile_context(receipt)  # This getter does not reserve permission.
            with self.assertRaises(Refused):journal.admit(request,dict(receipt,sequence=10))
            journal.admit(request,receipt)
        journal.seal(request['requestId'],dict(state='complete',category='ok',
            payload=dict(feedSession=self.session,sequence=9,resourceVersion='42')),reconcile_source_sha='f'*64)
        replay=self.request(action='reconcile-close',minimumSequence=10)
        publish_request(journal.root,replay)
        with patch('pgcat_lane_protocol.time.time',return_value=receipt['observedAt']),self.assertRaises(Refused):
            journal.admit(replay,dict(receipt,sequence=10))
        with journal._connection() as db:
            self.assertEqual(db.execute("SELECT count(*) FROM requests WHERE json_extract(request,'$.action')='reconcile-close'").fetchone()[0],1)

    def test_reconcile_seal_uses_admitted_context_and_requires_bound_source(self):
        from unittest.mock import patch
        journal,_,scalein,receipt=self.reconciliation_journal()
        req=self.request(action='reconcile-close',minimumSequence=9)
        publish_request(journal.root,req)
        with patch('pgcat_lane_protocol.time.time',return_value=receipt['observedAt']):journal.admit(req,receipt)
        payload=dict(feedSession=self.session,sequence=9,resourceVersion='42')
        complete=lambda value:dict(state='complete',category='ok',payload=value)
        for source,value in ((None,payload),('0'*64,payload),('f'*64,dict(payload,sequence=10)),
                ('f'*64,dict(payload,feedSession=str(uuid.uuid4()))),('f'*64,dict(payload,resourceVersion='042'))):
            with self.subTest(source=source,value=value),self.assertRaises(Refused):
                journal.seal(req['requestId'],complete(value),reconcile_source_sha=source)
        # The feed may publish a newer receipt before the original response seals.
        with patch('pgcat_lane_protocol.time.time',return_value=receipt['observedAt']+1):
            journal.reconcile_context(dict(receipt,sequence=10,observedAt=receipt['observedAt']+1))
            journal.seal(req['requestId'],complete(payload),reconcile_source_sha='f'*64)
        proof=journal.reconcile_binding()
        self.assertEqual(proof['sequence'],9)
        self.assertEqual(proof['requestId'],req['requestId'])
        self.assertEqual(proof['scaleinRequestId'],scalein['requestId'])
        self.assertEqual(proof['sourceSha256'],'f'*64)
        self.assertEqual(proof['sealedAt'],receipt['observedAt']+1)
        with self.assertRaises(Refused):journal.seal(req['requestId'],complete(payload),reconcile_source_sha='f'*64)

    def test_reconcile_source_for_other_action_and_no_scale_are_refused(self):
        req=self.request();self.admit(req)
        with self.assertRaises(Refused):
            self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload={}),reconcile_source_sha='f'*64)
        req=self.request(action='reconcile-close');publish_request(self.root,req)
        with self.assertRaises(Refused):self.journal.admit(req,{})

    def test_reconcile_refuses_ack_from_wrong_run_session_or_unconfirmed_request(self):
        from pgcat_lane_protocol import encode
        from unittest.mock import patch
        for changes in (dict(runId=str(uuid.uuid4())),dict(feedSession=str(uuid.uuid4())),
                dict(requestId=str(uuid.uuid4())),dict(sequence=7),dict(sealedAt=float('inf'))):
            journal,_,_,receipt=self.reconciliation_journal()
            ack=journal.scalein_ack()
            with journal._transaction() as db:
                # Raw JSON permits an intentionally malformed non-finite fixture.
                db.execute('UPDATE scalein_ack SET proof=?',(json.dumps(dict(ack,**changes)),))
            with patch('pgcat_lane_protocol.time.time',return_value=receipt['observedAt']),self.assertRaises(Refused):
                journal.reconcile_context(receipt)

    def test_reconcile_refuses_missing_changed_admission_or_cancelled_run(self):
        from pgcat_lane_protocol import encode
        from unittest.mock import patch
        for kind in ('missing','identity','context','source','cancelled'):
            journal,_,_,receipt=self.reconciliation_journal()
            req=self.request(action='reconcile-close',minimumSequence=9)
            publish_request(journal.root,req)
            with patch('pgcat_lane_protocol.time.time',return_value=receipt['observedAt']):journal.admit(req,receipt)
            with journal._transaction() as db:
                if kind=='missing':db.execute('DELETE FROM reconcile_admission')
                elif kind=='source':db.execute('UPDATE run SET hashes=?',(encode(dict(bundleSha256='a'*64,rehearsalContract='0'*64)).decode(),))
                elif kind!='cancelled':
                    proof=json.loads(db.execute('SELECT proof FROM reconcile_admission').fetchone()[0])
                    if kind=='identity':proof['receiptIdentity']='0'*64
                    if kind=='context':proof['context']['observedAt']+=1
                    db.execute('UPDATE reconcile_admission SET proof=?',(encode(proof).decode(),))
            if kind=='cancelled':journal.cancel('cancelled')
            result=dict(state='complete',category='ok',payload=dict(feedSession=self.session,sequence=9,resourceVersion='42'))
            with self.subTest(kind=kind),self.assertRaises(Refused):
                journal.seal(req['requestId'],result,reconcile_source_sha='f'*64)
            with self.assertRaises(Refused):journal.reconcile_binding()
            journal.seal(req['requestId'],dict(state='uncertain',category='source',payload={}))
            self.assertEqual(journal.snapshot(req['requestId'])['state'],'uncertain')

    def test_invalid_mode_and_nonprivate_root_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            journal = Journal(root)
            with self.assertRaises(Refused):
                journal.create_run(self.run, 'rehearsal', self.target, {'bundleSha256':'a'*64})
            root.chmod(0o755)
            with self.assertRaises(Refused):
                publish_request(root, self.request())

    def test_outstanding_and_reused_nonce_refused(self):
        request = self.request()
        self.admit(request)
        later = self.request(nonce=request['nonce'])
        publish_request(self.root, later)
        with self.assertRaises(Refused):
            self.journal.admit(later)
        self.journal.seal(request['requestId'], dict(state='complete',category='ok',payload={}))
        with self.assertRaises(Refused):
            self.journal.admit(later)


if __name__ == '__main__':
    unittest.main()


class PaidStopJournalTests(unittest.TestCase):
    def setUp(self):
        from unittest.mock import patch
        from test_pgcat_rehearsal_controller import ControllerTests
        from pgcat_lane_protocol import encode,digest
        from pgcat_lane_supervisor import save_once
        fixture=ControllerTests();fixture.setUp();self.addCleanup(fixture.doCleanups);patch.stopall()
        admission=patch('pgcat_lane_protocol._release_admission');admission.start();self.addCleanup(admission.stop)
        self.fixture=fixture;self.outer=fixture.binding;self.root=Path(self.outer['laneRoot'])
        self.root.parent.mkdir(mode=0o700);self.root.mkdir(mode=0o700)
        self.journal=Journal(self.root);self.run=str(uuid.uuid4());self.session=str(uuid.uuid4())
        b=self.outer['laneBindings']
        with patch('pgcat_lane_protocol._release_admission'):
            self.journal.create_run(self.run,'disabled-rehearsal',b['target'],dict(b['sourceHashes'],bundleSha256=digest(b['sourceHashes'])))
        save_once(self.root,'bindings.json',b)
        self.raw=fixture.manifest.read_bytes()
        import hashlib
        self.binding=dict(runId=self.run,feedId=str(uuid.uuid4()),pid=123,
            bindingSha256=hashlib.sha256(self.raw).hexdigest(),argvSha256='a'*64)
        self.proof=dict(runId=self.run,feedSession=self.session,sequence=9,resourceVersion='42',
            requestId=str(uuid.uuid4()),scaleinRequestId='scalein-ack',pendingBindingSha256='e'*64,
            sourceSha256=b['sourceHashes']['rehearsalContract'],sealedAt=time.time()-2)
        with self.journal._transaction() as db:
            db.execute('UPDATE run SET session=?',(self.session,))
            db.execute('INSERT INTO reconcile_binding VALUES (1,?)',(encode(self.proof).decode(),))
        self.receipt=dict(feedSession=self.session,sequence=10,desired=2,mode='disabled-rehearsal',
            observedAt=time.time()-1,rosterSha256=__import__('pgcat_lane_protocol').roster_sha(
                __import__('pgcat_lane_protocol').original_roster(b['target']['originalInstanceIds'])))

    def bind(self):
        self.journal.bind_feed(self.binding,manifest_raw=self.raw)
        self.journal.confirm_feed_exit(dict(runId=self.run,feedId=self.binding['feedId'],pid=123,exitCode=0))

    def controls(self,**changes):
        from pgcat_lane_protocol import digest,rehearsal_stop_ack
        from pgcat_lane_supervisor import save_once
        self.intent=dict(schemaVersion=1,runId=self.run,feedSession=self.session,minimumSequence=10,
                         reconcileBindingSha256=digest(self.proof))
        self.receipt.update(changes)
        save_once(self.root.parent,'rehearsal-stop.intent',self.intent,atomic=True)
        save_once(self.root.parent,'rehearsal-stop.ack',rehearsal_stop_ack(self.intent,self.receipt),atomic=True)
        save_once(self.root.parent,'status.json',self.receipt)

    def test_paid_bind_requires_original_bytes_and_is_atomic(self):
        from pgcat_lane_protocol import encode
        for raw in (None,encode(self.outer),self.raw+b' ',b'{}'):
            with self.subTest(raw=raw),self.assertRaises(Refused):
                self.journal.bind_feed(self.binding,manifest_raw=raw)
        with self.journal._connection() as db:self.assertIsNone(db.execute('SELECT 1 FROM feed_binding').fetchone())
        self.bind()
        with self.journal._connection() as db:self.assertEqual(bytes(db.execute('SELECT raw FROM feed_manifest').fetchone()['raw']),self.raw)
        with self.assertRaises(Refused):self.journal.bind_feed(self.binding,manifest_raw=self.raw)

    def test_record_derives_one_stop_with_exact_private_files_and_exit(self):
        self.bind();self.controls(sequence=11)
        proof=self.journal.record_rehearsal_stop()
        self.assertEqual(proof,self.journal.rehearsal_stop())
        self.assertEqual(proof['receipt'],self.receipt)
        self.assertEqual(proof['feedExit'],dict(pid=123,exitCode=0,category='ok',feedSession=self.session,sequence=11))
        self.assertEqual(proof['reconcileBinding'],self.proof)
        self.assertEqual(proof['feedBinding'],self.binding)
        self.assertEqual(self.journal.run_snapshot()['runState'],'active')
        with self.assertRaises(Refused):self.journal.record_rehearsal_stop()
        with self.assertRaises(TypeError):self.journal.record_rehearsal_stop({'exitCode':0})

    def test_direct_record_refuses_missing_independent_artifacts(self):
        self.bind();self.controls()
        for table in ('feed_binding','feed_exit_confirmation','feed_manifest','reconcile_binding'):
            with self.subTest(table=table):
                with self.journal._transaction() as db:
                    row=dict(db.execute('SELECT * FROM '+table).fetchone());db.execute('DELETE FROM '+table)
                with self.assertRaises(Refused):self.journal.record_rehearsal_stop()
                with self.journal._transaction() as db:
                    db.execute('INSERT INTO '+table+' VALUES ('+','.join('?' for _ in row)+')',tuple(row.values()))
        for name in ('rehearsal-stop.intent','rehearsal-stop.ack','status.json','lane/bindings.json'):
            path=self.root.parent/name;raw=path.read_bytes();path.unlink()
            with self.subTest(name=name),self.assertRaises(Refused):self.journal.record_rehearsal_stop()
            path.write_bytes(raw);path.chmod(0o600)

    def test_record_refuses_changed_private_evidence_and_wrong_reconciliation(self):
        from pgcat_lane_protocol import encode
        self.bind();self.controls()
        for name in ('rehearsal-stop.intent','rehearsal-stop.ack','status.json','lane/bindings.json'):
            path=self.root.parent/name;raw=path.read_bytes();value=json.loads(raw)
            if name.endswith('intent'):value['reconcileBindingSha256']='0'*64
            elif name.endswith('ack'):value['finalReceiptSha256']='0'*64
            elif name=='status.json':value['observedAt']=time.time()-30
            else:value['receiptPath']=str(self.root.parent/'unrelated.json')
            path.write_bytes(encode(value))
            with self.subTest(name=name),self.assertRaises(Refused):self.journal.record_rehearsal_stop()
            path.write_bytes(raw)
        for changes in (dict(sourceSha256='0'*64),dict(runId=str(uuid.uuid4())),dict(feedSession=str(uuid.uuid4())),
                        dict(sealedAt=time.time()),dict(sequence=10),dict(extra=True)):
            with self.journal._transaction() as db:db.execute('UPDATE reconcile_binding SET proof=?',(encode(dict(self.proof,**changes)).decode(),))
            with self.subTest(changes=changes),self.assertRaises(Refused):self.journal.record_rehearsal_stop()
        with self.journal._transaction() as db:
            db.execute('UPDATE reconcile_binding SET proof=?',(encode(self.proof).decode(),))
            db.execute('UPDATE feed_manifest SET raw=?',(self.raw+b' ',))
        with self.assertRaises(Refused):self.journal.record_rehearsal_stop()

    def test_stop_intent_schema_and_ack_bind_actual_sequence(self):
        from pgcat_lane_protocol import validate_rehearsal_stop,rehearsal_stop_ack,digest
        intent=dict(schemaVersion=1,runId=self.run,feedSession=self.session,minimumSequence=10,reconcileBindingSha256=digest(self.proof))
        validate_rehearsal_stop(intent)
        for change in (dict(schemaVersion=True),dict(minimumSequence=True),dict(minimumSequence=0),
                       dict(reconcileBindingSha256='bad'),dict(extra='bad')):
            with self.subTest(change=change),self.assertRaises(Refused):validate_rehearsal_stop(dict(intent,**change))
        for change in (dict(sequence=9),dict(sequence=12),dict(sequence=True),dict(feedSession=str(uuid.uuid4()))):
            with self.subTest(change=change),self.assertRaises(Refused):rehearsal_stop_ack(intent,dict(self.receipt,**change))

    def test_record_refuses_nonzero_exit_cancelled_run_and_noncanonical_or_redirected_controls(self):
        self.bind();self.controls()
        from pgcat_lane_protocol import encode
        with self.journal._transaction() as db:
            row=db.execute('SELECT proof FROM feed_exit_confirmation').fetchone()
            original=row['proof'];value=json.loads(original);value['exitCode']=1
            db.execute('UPDATE feed_exit_confirmation SET proof=?',(encode(value).decode(),))
        with self.assertRaises(Refused):self.journal.record_rehearsal_stop()
        with self.journal._transaction() as db:db.execute('UPDATE feed_exit_confirmation SET proof=?',(original,))
        for name in ('rehearsal-stop.intent','rehearsal-stop.ack','status.json'):
            path=self.root.parent/name;raw=path.read_bytes()
            if name != 'status.json':
                path.write_bytes(raw+b' ')
                with self.subTest(name=name),self.assertRaises(Refused):self.journal.record_rehearsal_stop()
                path.write_bytes(raw)
            path.rename(path.with_name('unrelated'));path.symlink_to(path.with_name('unrelated'))
            with self.subTest(symlink=name),self.assertRaises(Refused):self.journal.record_rehearsal_stop()
            path.unlink();path.with_name('unrelated').rename(path)
        self.journal.cancel('incomplete')
        with self.assertRaises(Refused):self.journal.record_rehearsal_stop()

    def test_bind_rejects_root_inner_or_manifest_mismatch_before_atomic_storage(self):
        from pgcat_lane_protocol import encode
        import hashlib
        for key,value in (('laneRoot',str(self.root.parent/'other')),('captureRoot',str(self.root.parent.parent)),
                          ('workerInventory','/other/run/provider-inventory.json'),('sourceHashes',dict(controller='0'*64,client='0'*64,runner='0'*64))):
            raw=encode(dict(self.outer,**{key:value}));binding=dict(self.binding,bindingSha256=hashlib.sha256(raw).hexdigest())
            with self.subTest(key=key),self.assertRaises(Refused):self.journal.bind_feed(binding,manifest_raw=raw)
        path=self.root/'bindings.json';raw=path.read_bytes();path.write_bytes(encode(dict(self.outer['laneBindings'],receiptPath=str(self.root.parent/'other'))))
        with self.assertRaises(Refused):self.journal.bind_feed(self.binding,manifest_raw=self.raw)
        path.write_bytes(raw)
        with self.journal._connection() as db:
            self.assertIsNone(db.execute('SELECT 1 FROM feed_binding').fetchone())
            self.assertIsNone(db.execute('SELECT 1 FROM feed_manifest').fetchone())

    def test_record_refuses_unknown_fields_in_independent_feed_binding(self):
        from pgcat_lane_protocol import encode
        self.bind();self.controls()
        with self.journal._transaction() as db:
            db.execute('UPDATE feed_binding SET proof=?',(encode(dict(self.binding,extra=True)).decode(),))
        with self.assertRaises(Refused):self.journal.record_rehearsal_stop()

    def test_record_refuses_unknown_fields_in_independent_exit_confirmation(self):
        from pgcat_lane_protocol import encode
        self.bind();self.controls()
        with self.journal._transaction() as db:
            row=db.execute('SELECT proof FROM feed_exit_confirmation').fetchone()
            db.execute('UPDATE feed_exit_confirmation SET proof=?',(encode(dict(json.loads(row['proof']),extra=True)).decode(),))
        with self.assertRaises(Refused):self.journal.record_rehearsal_stop()


if __name__ == '__main__': unittest.main()
