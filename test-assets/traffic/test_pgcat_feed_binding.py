"""Real local pipe/exec boundary with a harmless fake reviewed feed."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import unittest
from unittest.mock import patch
import uuid

from pgcat_lane_client import LaneClient
from pgcat_lane_protocol import Journal, Refused, digest, encode
import test_pgcat_rehearsal_controller as controller_tests


class FeedBindingTests(unittest.TestCase):
    def setUp(self):
        fixture=controller_tests.ControllerTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        patch.stopall()
        self.base=fixture.base;self.manifest=fixture.manifest
        self.marker=self.base/'provider-executed'
        feed=self.base/'reviewed-feed.py'
        feed.write_text('from pathlib import Path\nPath('+repr(str(self.marker))+').touch()\n')
        b=fixture.binding['laneBindings'];b['providerFeedPath']=str(feed)
        b['sourceHashes']['providerFeed']=hashlib.sha256(feed.read_bytes()).hexdigest()
        fixture.write_binding();self.outer=fixture.binding
        self.root=Path(fixture.binding['laneRoot']);self.root.parent.mkdir(mode=0o700);self.root.mkdir(mode=0o700)
        c=LaneClient();c.root=self.root;c.mode='no-scale';c.run_id=str(uuid.uuid4());c.bindings=b
        c.bundle=digest(b['sourceHashes']);c.journal=Journal(self.root)
        c.journal.create_run(c.run_id,'no-scale',b['target'],dict(b['sourceHashes'],bundleSha256=c.bundle))
        c._active=lambda:None
        self.client=c;self.addCleanup(self.reap)
        self.argv=[sys.executable,'-u',str(feed),'--mode','disabled-rehearsal','--interval','15','--max-seconds','180',
            '--workbench',b['laneWrapper'],'--worker-inventory',fixture.binding['workerInventory'],
            '--kubeconfig',fixture.binding['workerKubeconfig'],'--status-file',b['receiptPath'],
            '--run-context',str(self.root/'run-context.json'),'--contract-dir',b['contractDir'],
            '--no-scale-target',encode(b['target']).decode('ascii')]

    def reap(self):
        process=getattr(self.client,'feed_process',None)
        if process is not None:process.wait(timeout=7)

    def test_provider_exec_waits_for_durable_binding_and_exit_is_one_use(self):
        self.assertTrue(hasattr(self.client,'start_feed'),'missing startup barrier')
        original=self.client.journal.bind_feed
        def bind(value):
            time.sleep(.08)
            self.assertFalse(self.marker.exists())
            self.assertIsNone(self.client.feed_process.poll())
            original(value)
        with patch.object(self.client.journal,'bind_feed',side_effect=bind):
            child=self.client.start_feed(self.argv,{'PATH':os.defpath},self.manifest)
        child.wait(timeout=3);self.assertTrue(self.marker.exists())
        self.client.confirm_feed_exit(child)
        with self.client.journal._connection() as db:
            bound=json.loads(db.execute('SELECT proof FROM feed_binding').fetchone()['proof'])
            exited=json.loads(db.execute('SELECT proof FROM feed_exit_confirmation').fetchone()['proof'])
        self.assertEqual(bound['bindingSha256'],hashlib.sha256(self.manifest.read_bytes()).hexdigest())
        self.assertEqual(bound['argvSha256'],digest(self.argv));self.assertEqual(bound['pid'],child.pid)
        self.assertEqual(exited,dict(runId=self.client.run_id,feedId=bound['feedId'],pid=child.pid,exitCode=0))
        with self.assertRaises(Refused):self.client.confirm_feed_exit(child)
        with self.assertRaises(Refused):self.client.start_feed(self.argv,{'PATH':os.defpath},self.manifest)

    def test_binding_error_closes_gate_without_provider_execution(self):
        self.assertTrue(hasattr(self.client,'start_feed'),'missing startup barrier')
        with patch.object(self.client.journal,'bind_feed',side_effect=Refused('storage')):
            with self.assertRaises(Refused):self.client.start_feed(self.argv,{'PATH':os.defpath},self.manifest)
        self.client.feed_process.wait(timeout=3)
        self.assertFalse(self.marker.exists())

    def test_gate_eof_without_release_never_executes_provider(self):
        self.assertTrue(hasattr(self.client,'start_feed'),'missing startup barrier')
        real_write=os.write
        def drop_release(fd,data):
            if data==b'1':return 0
            return real_write(fd,data)
        with patch('pgcat_lane_client.os.write',side_effect=drop_release):
            with self.assertRaises(Refused):self.client.start_feed(self.argv,{'PATH':os.defpath},self.manifest)
        self.client.feed_process.wait(timeout=3)
        self.assertFalse(self.marker.exists())

    def test_live_or_unrelated_handle_cannot_seal_exit(self):
        self.assertTrue(hasattr(self.client,'start_feed'),'missing startup barrier')
        original=self.client.journal.bind_feed
        def bind(value):
            original(value)
            with self.assertRaises(Refused):self.client.confirm_feed_exit(self.client.feed_process)
        with patch.object(self.client.journal,'bind_feed',side_effect=bind):
            child=self.client.start_feed(self.argv,{'PATH':os.defpath},self.manifest)
        child.wait(timeout=3)
        other=subprocess.Popen([sys.executable,'-c','pass']);other.wait(timeout=3)
        with self.assertRaises(Refused):self.client.confirm_feed_exit(other)
        with self.client.journal._connection() as db:
            self.assertIsNone(db.execute('SELECT 1 FROM feed_exit_confirmation').fetchone())

    def test_gate_timeout_never_executes_provider(self):
        original=self.client.journal.bind_feed
        def delayed(value):
            original(value)
            self.client.feed_process.wait(timeout=7)
        with patch.object(self.client.journal,'bind_feed',side_effect=delayed):
            with self.assertRaises(Refused):self.client.start_feed(self.argv,{'PATH':os.defpath},self.manifest)
        self.assertEqual(self.client.feed_process.poll(),125)
        self.assertFalse(self.marker.exists())

    def test_changed_command_or_source_refuses_before_spawn(self):
        self.assertTrue(hasattr(self.client,'start_feed'),'missing startup barrier')
        with self.assertRaises(Refused):self.client.start_feed(self.argv+['--arbitrary'],{'PATH':os.defpath},self.manifest)
        Path(self.argv[2]).write_text('raise SystemExit(0)')
        with self.assertRaises(Refused):self.client.start_feed(self.argv,{'PATH':os.defpath},self.manifest)
        self.assertFalse(self.marker.exists())
