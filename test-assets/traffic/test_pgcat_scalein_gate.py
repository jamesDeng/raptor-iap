"""Offline admission regressions for one exact PgCat desired-two mutation."""
import hashlib
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch
import uuid

import pgcat_lane_protocol as protocol
from pgcat_lane_protocol import (Journal, Refused, encode, expected_roster,
                                 monotonic, publish_request, roster_sha)


class ScaleinJournalTests(unittest.TestCase):
    def setUp(self):
        from unittest.mock import patch
        admission=patch('pgcat_lane_protocol._release_admission');admission.start();self.addCleanup(admission.stop)
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.run = str(uuid.uuid4()); self.session = str(uuid.uuid4())
        self.supervisor = str(uuid.uuid4())
        self.originals = ['i-original-a', 'i-original-b']
        self.candidate = 'i-new-c'
        self.target = dict(accountId='1360282071200743', regionId='ap-southeast-1',
            workerId='i-t4nj1bitmcz7cuoch82h',
            scalingGroupId='asg-t4necb4dfmu6o3z2rn0g',
            nlbServerGroupId='sgp-xs5s42tihtl6ja2a67', originalInstanceIds=self.originals)
        self.journal = Journal(self.root)
        self.journal.create_run(self.run, 'disabled-rehearsal', self.target,
            dict(bundleSha256='a'*64, zeroProofWorker='d'*64))
        self.pending = dict(runId=self.run, feedSession=self.session, newId=self.candidate,
            originalInstanceIds=self.originals, clientRosterSha256='b'*64,
            sequence=3, observedAt=time.time()-2, resourceVersion='11',
            evidenceSha256='c'*64, sourceSha256='e'*64)
        self.pre = dict(runId=self.run, feedSession=self.session, newId=self.candidate,
            originalInstanceIds=self.originals, clientRosterSha256='b'*64,
            pendingBindingSha256=hashlib.sha256(encode(self.pending)).hexdigest(),
            sequence=4, observedAt=time.time()-1, resourceVersion='12',
            evidenceSha256='f'*64, sourceSha256='e'*64)
        with self.journal._connection() as db:
            db.execute('INSERT INTO pending_binding VALUES (1,?)',(encode(self.pending).decode(),))
            db.execute('INSERT INTO pre_drain_binding VALUES (1,?,?)',
                       (encode(self.pre).decode(),time.time()))
        self.absent = roster_sha(expected_roster(self.originals,self.candidate,registered=False))

    def request(self, action, sequence, *, roster=None):
        req = dict(requestId=str(uuid.uuid4()),runId=self.run,supervisorId=self.supervisor,
            action=action,feedSession=self.session,minimumSequence=sequence,
            receiptIdentity='e'*64,target=self.target,bundleSha256='a'*64,
            nonce=str(uuid.uuid4()),deadlineMonotonic=monotonic()+20.)
        publish_request(self.root,req)
        receipt = dict(feedSession=self.session,sequence=sequence,desired=3,
            observedAt=time.time(),rosterSha256=self.absent if roster is None else roster)
        return req,receipt

    def acknowledge_nlb(self):
        registered=roster_sha(expected_roster(self.originals,self.candidate,registered=True))
        req,receipt=self.request('deregister-new-node',5,roster=registered)
        self.journal.admit(req,receipt)
        self.journal.seal(req['requestId'],dict(state='complete',category='ok',
            payload={'requestId':'nlb-ack','jobId':'nlb-job'}))

    def test_scalein_needs_sealed_zero_then_clear_activity_then_new_publication(self):
        req,receipt = self.request('scalein-three-to-two',7)
        with self.assertRaises(Refused): self.journal.admit(req,receipt)
        self.acknowledge_nlb()
        time.sleep(.01)
        zero_req,zero_receipt = self.request('zero-proof',6)
        self.journal.admit(zero_req,zero_receipt)
        zero = dict(runId=self.run,feedSession=self.session,newId=self.candidate,
            originalInstanceIds=self.originals,
            preDrainBindingSha256=hashlib.sha256(encode(self.pre)).hexdigest(),
            sequence=6,observedAt=zero_receipt['observedAt'],
            clientRosterSha256='b'*64,evidenceSha256='c'*64,sourceSha256='d'*64)
        self.journal.seal(zero_req['requestId'],dict(state='complete',category='ok',payload=zero),
                          zero_source_sha='d'*64)
        req,receipt = self.request('scalein-three-to-two',7)
        with self.assertRaises(Refused): self.journal.admit(req,receipt)
        time.sleep(.01)
        act_req,act_receipt = self.request('scaling-activity-check',7)
        self.journal.admit(act_req,act_receipt)
        self.journal.seal(act_req['requestId'],dict(state='complete',category='ok',
            payload={'status':'CLEAR','requestId':act_req['requestId']}))
        time.sleep(.01)
        req,receipt = self.request('scalein-three-to-two',7)
        self.journal.admit(req,receipt)
        again,later = self.request('scalein-three-to-two',8)
        with self.assertRaises(Refused): self.journal.admit(again,later)

    def test_nonzero_roster_or_changed_candidate_refuses_before_scalein(self):
        self.acknowledge_nlb()
        zero_req,receipt = self.request('zero-proof',6,
            roster=roster_sha(expected_roster(self.originals,self.candidate,registered=True)))
        with self.assertRaises(Refused): self.journal.admit(zero_req,receipt)

    def test_zero_action_requires_completed_bound_nlb_removal(self):
        req,receipt=self.request('zero-proof',6)
        with self.assertRaises(Refused): self.journal.admit(req,receipt)

    def ready_activity(self):
        self.acknowledge_nlb()
        time.sleep(.01)
        zero_req,zero_receipt=self.request('zero-proof',6)
        self.journal.admit(zero_req,zero_receipt)
        zero=dict(runId=self.run,feedSession=self.session,newId=self.candidate,
            originalInstanceIds=self.originals,
            preDrainBindingSha256=hashlib.sha256(encode(self.pre)).hexdigest(),
            sequence=6,observedAt=zero_receipt['observedAt'],
            clientRosterSha256='b'*64,evidenceSha256='c'*64,sourceSha256='d'*64)
        self.journal.seal(zero_req['requestId'],dict(state='complete',category='ok',payload=zero),
                          zero_source_sha='d'*64)
        time.sleep(.01)
        activity,receipt=self.request('scaling-activity-check',7)
        self.journal.admit(activity,receipt)
        self.journal.seal(activity['requestId'],dict(state='complete',category='ok',
            payload={'status':'CLEAR','requestId':activity['requestId']}))
        return activity,receipt

    def test_scalein_rejects_new_provider_receipt_or_changed_identity_after_activity(self):
        activity,receipt=self.ready_activity()
        for change in (dict(minimumSequence=8),dict(receiptIdentity='f'*64)):
            with self.subTest(change=change):
                req,now=self.request('scalein-three-to-two',7)
                req.update(change)
                # The published bytes must match the tested request.
                req['requestId']=str(uuid.uuid4()); req['nonce']=str(uuid.uuid4())
                publish_request(self.root,req)
                now['sequence']=req['minimumSequence']
                with self.assertRaises(Refused): self.journal.admit(req,now)

    def test_scalein_rejects_activity_older_than_freshness_window(self):
        self.ready_activity()
        req,receipt=self.request('scalein-three-to-two',7)
        future=time.time()+4
        with patch.object(protocol.time,'time',return_value=future):
            with self.assertRaises(Refused): self.journal.admit(req,receipt)

    def test_cancelled_run_cannot_use_prior_clear_activity(self):
        self.ready_activity()
        self.journal.cancel('cancelled')
        req,receipt=self.request('scalein-three-to-two',7)
        with self.assertRaises(Refused): self.journal.admit(req,receipt)


if __name__ == '__main__': unittest.main()
