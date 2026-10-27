from pgcat_lane_protocol import monotonic
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import unittest
from unittest.mock import patch

import pgcat_lane_client as client_module
from pgcat_lane_client import LaneClient
from pgcat_lane_protocol import Refused, publish_request
from pgcat_lane_supervisor import read_json, save_once
import test_pgcat_lane_supervisor as supervisor_tests


class ClientTests(unittest.TestCase):
    private_json = supervisor_tests.SupervisorTests.private_json
    receipt = supervisor_tests.SupervisorTests.receipt

    def setUp(self):
        supervisor_tests.SupervisorTests.setUp(self)
        self.owner.close()
        (self.global_dir/'active-run').unlink()  # Only this test's isolated fake owner.
        real_spawn = subprocess.Popen
        global_dir = str(self.global_dir)
        def spawn(argv, **kwargs):
            bootstrap = ('import runpy,sys; from pathlib import Path; sys.path.insert(0,str(Path(sys.argv[1]).parent)); '
                'import pgcat_lane_supervisor as s; s.GLOBAL_ROOT=Path('+repr(global_dir)+'); '
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

    def receipts(self, status='INCONCLUSIVE'):
        feed = subprocess.Popen([sys.executable, '-c', 'pass'])
        feed.wait()
        exit_receipt = dict(pid=feed.pid, exitCode=0, category='ok', feedSession=self.session, sequence=1)
        assessment = dict(status=status, mode='disabled-rehearsal', feedSession=self.session,
                          endSequence=1, evidenceSha256='e'*64)
        return feed, exit_receipt, assessment

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

    def test_global_record_blocks_a_new_run_after_finish(self):
        self.client.finish(*self.receipts())
        self.client.process.wait(timeout=3)
        # Use a direct supervisor construction so the test's private global root applies.
        from pgcat_lane_protocol import Journal
        from pgcat_lane_supervisor import LaneSupervisor
        root = self.base/'second'; root.mkdir(mode=0o700)
        journal = Journal(root)
        import uuid
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


if __name__ == '__main__': unittest.main()
