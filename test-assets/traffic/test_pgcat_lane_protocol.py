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

    def test_deadline_clock_is_shared_with_detached_process(self):
        import subprocess
        import sys
        import pgcat_lane_protocol as protocol
        before = protocol.monotonic()
        child = float(subprocess.check_output([sys.executable, '-c',
            'from pgcat_lane_protocol import monotonic; print(monotonic())']))
        after = protocol.monotonic()
        self.assertLessEqual(before, child)
        self.assertLessEqual(child, after)

    def finish_receipts(self):
        feed = dict(pid=123, exitCode=0, category='ok', feedSession=self.session, sequence=1)
        assessment = dict(status='INCONCLUSIVE', mode='no-scale', feedSession=self.session,
                          endSequence=1, evidenceSha256='e'*64)
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

    def test_rehearsal_scaleout_requires_fixed_group(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            journal = Journal(root)
            wrong_target = dict(self.target, scalingGroupId='wrong-group')
            journal.create_run(self.run, 'disabled-rehearsal', wrong_target, {'bundleSha256':'a'*64})
            request = self.request(action='scaleout-two-to-three', target=wrong_target)
            publish_request(root, request)
            with self.assertRaises(Refused):
                journal.admit(request)

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
