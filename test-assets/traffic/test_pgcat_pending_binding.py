"""Offline tests for the pending-proof result and one-use journal binding."""
import copy
import json
from pathlib import Path
import tempfile
import time
import unittest
import uuid

from pgcat_lane_actions import ActionCatalog, SOURCE_NAMES
from pgcat_lane_protocol import Journal, Refused, encode, monotonic, publish_request


class PendingBindingTests(unittest.TestCase):
    def setUp(self):
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

    def request(self, **changes):
        req=dict(requestId=str(uuid.uuid4()),runId=self.run,supervisorId=self.supervisor,
            action='pending-proof',feedSession=self.session,minimumSequence=4,
            receiptIdentity='e'*64,target=self.target,bundleSha256='a'*64,
            nonce=str(uuid.uuid4()),deadlineMonotonic=monotonic()+5.)
        req.update(changes); publish_request(self.root,req); self.journal.admit(req)
        return req

    def parse(self, proof=None, request=None, receipt=None):
        request=request or dict(runId=self.run,feedSession=self.session,
                                minimumSequence=4,target=self.target)
        return ActionCatalog.parse('pending-proof',encode(proof or self.proof),request,receipt,
                                   source_sha=self.source)

    def test_exact_address_free_summary_binds_once(self):
        req=self.request(); parsed=self.parse(request=req)
        self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=parsed),
                          pending_source_sha=self.source)
        self.assertEqual(self.journal.pending_binding(),parsed)
        self.assertNotIn('10.',json.dumps(self.journal.pending_binding()))
        with self.assertRaises(Refused):
            self.request()

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
        req=self.request()
        with self.journal._transaction() as db: db.execute("UPDATE run SET mode='no-scale'")
        with self.assertRaises(Refused):
            self.journal.seal(req['requestId'],dict(state='complete',category='ok',payload=self.proof),
                              pending_source_sha=self.source)
        with self.assertRaises(Refused): self.journal.pending_binding()

    def test_uncertain_result_never_binds(self):
        req=self.request()
        self.journal.seal(req['requestId'],dict(state='uncertain',category='child',payload={}))
        with self.assertRaises(Refused): self.journal.pending_binding()

    def test_command_accepts_only_validated_run_uuid(self):
        cli=self.root/'cli'; cli.write_text('offline'); cli.chmod(0o700)
        bindings=dict(workbenchCli=str(cli),aliyunCli=str(cli),laneWrapper='/fixed/wrapper',
            providerFeedPath='/fixed/feed',receiptPath='/fixed/receipt',
            laneLockPath='/fixed/lock',laneIntentDir='/fixed/intents',
            phaseWorker='/fixed/phase',pendingWorker='/fixed/pending',
            pendingProofWorker='/fixed/pending-proof',remoteRun='/fixed/run',
            contractDir='/fixed/contract',scaleoutIntentPath='/fixed/scaleout',
            preScaleProofPath='/fixed/pre-scale',sourceHashes={k:'d'*64 for k in SOURCE_NAMES},
            target=dict(self.target,workerId='i-t4nj1bitmcz7cuoch82h'))
        argv,env,budget=ActionCatalog.command('pending-proof',bindings,self.run)
        self.assertEqual(budget,5.)
        self.assertIn('/fixed/pending-proof /fixed/contract '+self.run,argv[-1])
        self.assertNotIn('10.0.0.',repr(argv))
        self.assertNotIn('AWS_SECRET_ACCESS_KEY',env)
        with self.assertRaises(Refused):
            ActionCatalog.command('pending-proof',bindings,self.run+'; touch /tmp/bad')


if __name__ == '__main__': unittest.main()
