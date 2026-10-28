"""Offline tests for the pending-proof result and one-use journal binding."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import time
import unittest
import uuid

from pgcat_lane_actions import ActionCatalog, SOURCE_NAMES
from pgcat_lane_protocol import ACTIONS, Journal, Refused, encode, monotonic, publish_request


class PendingBindingTests(unittest.TestCase):
    def setUp(self):
        from unittest.mock import patch
        admission=patch('pgcat_lane_protocol._release_admission');admission.start();self.addCleanup(admission.stop)
        self.temp=tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)
        self.run=str(uuid.uuid4()); self.session=str(uuid.uuid4()); self.supervisor=str(uuid.uuid4())
        self.originals=['i-original-a','i-original-b']; self.source='d'*64
        self.target=dict(accountId='1360282071200743',regionId='ap-southeast-1',
            workerId='i-worker',scalingGroupId='asg-t4necb4dfmu6o3z2rn0g',
            nlbServerGroupId='sgp-exact',originalInstanceIds=self.originals)
        self.journal=Journal(self.root)
        self.journal.create_run(self.run,'disabled-rehearsal',self.target,
            dict(bundleSha256='a'*64,pendingProofWorker=self.source))
        self.proof=dict(runId=self.run,feedSession=self.session,newId='i-new-c',
            originalInstanceIds=self.originals,sequence=4,observedAt=time.time(),
            resourceVersion='11',clientRosterSha256='b'*64,evidenceSha256='c'*64,
            sourceSha256=self.source)

    def request(self, observed_at=None, **changes):
        req=dict(requestId=str(uuid.uuid4()),runId=self.run,supervisorId=self.supervisor,
            action='pending-proof',feedSession=self.session,minimumSequence=4,
            receiptIdentity='e'*64,target=self.target,bundleSha256='a'*64,
            nonce=str(uuid.uuid4()),deadlineMonotonic=monotonic()+5.)
        req.update(changes); publish_request(self.root,req)
        receipt=(dict(feedSession=self.session,sequence=req['minimumSequence'],
                      observedAt=time.time()+1 if observed_at is None else observed_at)
                 if req['action'] == 'pending-proof' else None)
        self.journal.admit(req,receipt)
        return req

    def prepared(self):
        req=self.request(action='pending-status',minimumSequence=3)
        self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload={'status':'PREPARED'}))
        return self.journal.record_prepared(req['requestId'])

    def test_prepared_marker_requires_later_provider_observation(self):
        status=self.request(action='pending-status',minimumSequence=3)
        self.journal.seal(status['requestId'],dict(state='complete',category='ok',
            payload={'status':'PREPARED'}))
        with self.assertRaises(Refused): self.request(observed_at=time.time()+1)
        marker=self.journal.record_prepared(status['requestId'])
        with self.assertRaises(Refused): self.request(observed_at=marker['sealedAt'])
        self.request(observed_at=marker['sealedAt']+.001)

    def test_no_scale_admits_only_baseline_and_collector(self):
        allowed={'baseline-start','baseline-status','collector-launch','collector-status'}
        for action in sorted(ACTIONS):
            with self.subTest(action=action):
                root=self.root/action; root.mkdir(mode=0o700)
                journal=Journal(root)
                journal.create_run(str(uuid.uuid4()),'no-scale',self.target,
                                   dict(bundleSha256='a'*64,pendingProofWorker=self.source))
                snapshot=journal.run_snapshot()
                req=dict(requestId=str(uuid.uuid4()),runId=snapshot['runId'],
                    supervisorId=str(uuid.uuid4()),action=action,
                    feedSession=self.session,minimumSequence=1,receiptIdentity='e'*64,
                    target=self.target,bundleSha256='a'*64,nonce=str(uuid.uuid4()),
                    deadlineMonotonic=monotonic()+5.)
                publish_request(root,req)
                if action in allowed: journal.admit(req)
                else:
                    with self.assertRaises(Refused): journal.admit(req)

    def test_pending_proof_needs_completed_prepared_status(self):
        with self.assertRaises(Refused): self.request()
        status=self.request(action='pending-status',minimumSequence=3)
        with self.assertRaises(Refused): self.request()
        self.journal.seal(status['requestId'],dict(state='complete',category='ok',payload={'status':'RUNNING'}))
        with self.assertRaises(Refused): self.request()

    def test_pending_proof_rejects_non_exact_prepared_record(self):
        status=self.request(action='pending-status',minimumSequence=3)
        self.journal.seal(status['requestId'],dict(state='complete',category='ok',
            payload={'status':'PREPARED','stage':'extra'}))
        with self.assertRaises(Refused): self.request()

    def parse(self, proof=None, request=None, receipt='successful'):
        request=request or dict(runId=self.run,feedSession=self.session,
                                minimumSequence=4,target=self.target)
        if receipt == 'successful':
            receipt=dict(feedSession=self.session,sequence=4,observedAt=time.time()+1)
        return ActionCatalog.parse('pending-proof',encode(proof or self.proof),request,receipt,
                                   source_sha=self.source)

    def test_pending_proof_requires_acknowledged_publication(self):
        with self.assertRaises(Refused): self.parse(receipt=None)

    def test_exact_address_free_summary_binds_once(self):
        self.prepared()
        req=self.request(); parsed=self.parse(request=req)
        self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=parsed),
                          pending_source_sha=self.source)
        self.assertEqual(self.journal.pending_binding(),parsed)
        self.assertNotIn('10.0.0.',json.dumps(self.journal.pending_binding()))
        with self.assertRaises(Refused):
            self.request()

    def test_pre_drain_summary_binds_exact_pending_singleton(self):
        self.prepared()
        pending_req=self.request()
        self.journal.seal(pending_req['requestId'],dict(state='complete',category='ok',payload=self.proof),
                          pending_source_sha=self.source)
        with self.journal._connection() as db:
            hashes=json.loads(db.execute('SELECT hashes FROM run').fetchone()['hashes'])
            hashes['preDrainProofWorker']='f'*64
            db.execute('UPDATE run SET hashes=?',(encode(hashes).decode(),))
        summary=dict(self.proof,sequence=5,observedAt=time.time(),
            pendingBindingSha256=hashlib.sha256(encode(self.proof)).hexdigest(),
            evidenceSha256='1'*64,sourceSha256='f'*64)
        req=dict(pending_req,requestId=str(uuid.uuid4()),nonce=str(uuid.uuid4()),
            action='pre-drain-proof',minimumSequence=5)
        publish_request(self.root,req)
        receipt=dict(feedSession=self.session,sequence=5,desired=3,observedAt=time.time())
        self.journal.admit(req,receipt)
        self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=summary),
                          pre_drain_source_sha='f'*64)
        self.assertEqual(self.journal.pre_drain_binding()['newId'],self.proof['newId'])
        self.assertNotIn('address',json.dumps(self.journal.pre_drain_binding()).lower())
        with self.assertRaises(Refused):
            other=dict(req,requestId=str(uuid.uuid4()),nonce=str(uuid.uuid4()))
            publish_request(self.root,other);self.journal.admit(other,receipt)

    def test_original_changed_session_source_or_extra_field_refused(self):
        for changed in (dict(newId=self.originals[0]),
                        dict(feedSession=str(uuid.uuid4())),
                        dict(sourceSha256='f'*64),
                        dict(address='10.0.0.3')):
            with self.subTest(changed=changed):
                with self.assertRaises(Refused): self.parse(dict(self.proof,**changed))

    def test_stale_or_incomplete_proof_refused(self):
        for changed in (dict(observedAt=time.time()-60),dict(sequence=5),
                        dict(evidenceSha256=''),dict(originalInstanceIds=['i-original-a'])):
            with self.subTest(changed=changed):
                with self.assertRaises(Refused): self.parse(dict(self.proof,**changed))

    def test_recent_proof_one_publication_before_call_is_valid(self):
        self.prepared()
        earlier=dict(self.proof,sequence=3,observedAt=time.time()-1)
        self.assertEqual(self.parse(earlier),earlier)
        req=self.request()
        self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=earlier),
                          pending_source_sha=self.source)
        self.assertEqual(self.journal.pending_binding()['sequence'],3)

    def test_proof_must_precede_consumed_publication(self):
        receipt=dict(feedSession=self.session,sequence=4,observedAt=time.time()-2)
        with self.assertRaises(Refused): self.parse(receipt=receipt)

    def test_no_scale_result_never_binds(self):
        self.prepared()
        req=self.request()
        with self.journal._transaction() as db: db.execute("UPDATE run SET mode='no-scale'")
        with self.assertRaises(Refused):
            self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=self.proof),
                              pending_source_sha=self.source)
        with self.assertRaises(Refused): self.journal.pending_binding()

    def test_uncertain_result_never_binds(self):
        self.prepared()
        req=self.request()
        self.journal.seal(req['requestId'],dict(state='uncertain',category='child',payload={}))
        with self.assertRaises(Refused): self.journal.pending_binding()

    def test_command_accepts_only_validated_run_uuid(self):
        cli=self.root/'cli'; cli.write_text('offline'); cli.chmod(0o700)
        bindings=dict(workbenchCli=str(cli),aliyunCli=str(cli),laneWrapper='/fixed/wrapper',
            providerFeedPath='/fixed/feed',receiptPath='/fixed/receipt',
            laneLockPath='/fixed/lock',laneIntentDir='/fixed/intents',
            phaseWorker='/fixed/run/pgcat_rehearsal_worker.py',
            pendingWorker='/fixed/run/pgcat_rehearsal_worker.py',
            pendingProofWorker='/fixed/run/pgcat_pending_proof_worker.py',
            preDrainProofWorker='/fixed/run/pgcat_pre_drain_proof_worker.py',
            zeroProofWorker='/fixed/run/pgcat_zero_proof_worker.py',
            workerKubeconfig='/fixed/kubeconfig',remoteRun='/fixed/run',
            contractDir='/fixed/contract',scaleoutIntentPath='/fixed/scaleout',
            nlbIntentPath='/fixed/nlb',
            scaleinIntentPath='/fixed/scalein',
            preScaleProofPath='/fixed/pre-scale',sourceHashes={k:'d'*64 for k in SOURCE_NAMES},
            target=dict(self.target,workerId='i-t4nj1bitmcz7cuoch82h'))
        argv,env,budget=ActionCatalog.command('pending-proof',bindings,self.run)
        self.assertEqual(budget,5.)
        self.assertIn(' pending-proof /fixed/contract '+self.run,argv[-1])
        self.assertIn('python3 -I -S -c',argv[-1])
        self.assertNotIn('10.0.0.',repr(argv))
        self.assertNotIn('AWS_SECRET_ACCESS_KEY',env)
        with self.assertRaises(Refused):
            ActionCatalog.command('pending-proof',bindings,self.run+'; touch /tmp/bad')
        argv,_,budget=ActionCatalog.command('pre-drain-proof',bindings,self.run,self.proof)
        self.assertEqual(budget,5.)
        self.assertIn(' pre-drain-proof /fixed/contract '+self.run,argv[-1])
        self.assertIn('i-new-c',argv[-1]);self.assertNotIn('10.0.0.',argv[-1])
        pre=dict(self.proof,sequence=5,observedAt=time.time(),
            pendingBindingSha256=hashlib.sha256(encode(self.proof)).hexdigest())
        request=dict(runId=self.run,feedSession=self.session,minimumSequence=5,target=self.target)
        receipt=dict(feedSession=self.session,sequence=5,observedAt=time.time()+1)
        self.assertEqual(ActionCatalog.parse('pre-drain-proof',encode(pre),request,receipt,
            source_sha=self.source),pre)


if __name__ == '__main__': unittest.main()
