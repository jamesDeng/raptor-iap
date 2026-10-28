"""Isolated execution contract: real source snapshots, harmless child processes."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import py_compile
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


class SourceBoundaryTests(unittest.TestCase):
    def test_normally_imported_local_runtime_is_refused(self):
        import pgcat_rehearsal_controller as controller
        import pgcat_lane_client as client
        import pgcat_lane_supervisor as supervisor
        import provider_feed
        for module in (controller,client,supervisor,provider_feed):
            self.assertTrue(hasattr(module,"_require_verified"))
            with self.subTest(module=module.__name__),self.assertRaises((ValueError,controller.Refused)):
                module._require_verified()

    def test_paid_public_start_is_structurally_refused_before_preflight(self):
        import pgcat_rehearsal_controller as controller
        c=object.__new__(controller.Controller)
        with patch.object(c,'_paid_evidence',side_effect=AssertionError('preflight called')):
            with self.assertRaises(controller.Refused):c.start_rehearsal('/unused')

    def test_paid_direct_client_and_supervisor_refuse_before_runtime_work(self):
        import pgcat_lane_client as client
        import pgcat_lane_supervisor as supervisor
        from test_pgcat_rehearsal_controller import ControllerTests
        real_start=client.LaneClient.start
        fixture=ControllerTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        root=fixture.base/'lane'
        with patch.object(client,'_require_verified'),patch.object(client.subprocess,'Popen',side_effect=AssertionError('spawned')):
            with self.assertRaises(client.Refused):real_start(fixture.binding['laneBindings'],'disabled-rehearsal',root)
        self.assertFalse(root.exists())
        journal=unittest.mock.Mock();journal.run_snapshot.return_value={'mode':'disabled-rehearsal'}
        with patch.object(supervisor,'_require_verified'):
            with self.assertRaises(supervisor.Refused):supervisor.LaneSupervisor(root,{},journal)
        journal._connection.assert_not_called()

    def test_paid_durable_admission_refuses_preexisting_paid_run(self):
        import pgcat_lane_protocol as protocol
        from test_pgcat_lane_protocol import ProtocolTest
        fixture=ProtocolTest();fixture.setUp()
        try:
            with fixture.journal._transaction() as db:
                db.execute("UPDATE run SET mode='disabled-rehearsal'")
            request=fixture.request(action='baseline-status')
            protocol.publish_request(fixture.root,request)
            with self.assertRaises(protocol.Refused):fixture.journal.admit(request)
        finally:fixture.doCleanups()

    def test_paid_feed_context_refused_without_provider_operations(self):
        import provider_feed
        import pgcat_lane_supervisor as supervisor
        from pgcat_lane_protocol import Refused
        from test_provider_feed import NoScaleFeedTests
        fixture=NoScaleFeedTests()
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);root.chmod(0o700)
            active,lane,target=fixture.sealed_context(root,'disabled-rehearsal')
            with patch.object(supervisor,'GLOBAL_ROOT',active):
                with self.assertRaises(Refused):
                    provider_feed.validate_run_context(lane,lane/'run-context.json',no_scale=False)


def stale_cache(path, old, new):
    """Build an actually valid timestamp/size cache, then restore reviewed bytes."""
    import time
    if len(old)!=len(new):raise AssertionError('same size required')
    stamp=int(time.time())-100
    path.write_text(old);os.utime(str(path),(stamp,stamp))
    previous=sys.pycache_prefix
    try:
        sys.pycache_prefix=None
        py_compile.compile(str(path),doraise=True,optimize=0)
        sys.pycache_prefix=str(path.parent/'fixture-pycache')
        py_compile.compile(str(path),doraise=True,optimize=0)
    finally:sys.pycache_prefix=previous
    path.write_text(new);os.utime(str(path),(stamp,stamp));path.chmod(0o600)


class SnapshotProcessTests(unittest.TestCase):
    def setUp(self):
        import pgcat_source_bootstrap as bootstrap
        self.bootstrap=bootstrap
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(os.path.realpath(self.tmp.name));self.root.chmod(0o700)
        here=Path(__file__).parent
        for filename in bootstrap.SOURCES.values():
            shutil.copyfile(str(here/filename),str(self.root/filename))

    def hashes(self):
        return {key:hashlib.sha256((self.root/name).read_bytes()).hexdigest()
                for key,name in self.bootstrap.SOURCES.items()}

    def run_entry(self,entry='runner',prefix=None):
        hashes=self.hashes()
        code=self.bootstrap.ROOT
        if prefix:code=code.replace('sys.exit(m.main())',prefix+'\nsys.exit(m.main())')
        return subprocess.run([sys.executable,'-I','-S','-c',code,
            str(self.root/'pgcat_source_bootstrap.py'),hashes['sourceBootstrap'],entry,json.dumps(hashes)],
            cwd=str(self.root),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)

    def test_runner_and_local_guards_ignore_direct_and_transitive_stale_cache(self):
        runner=self.root/'pgcat_lane_runner.py'
        new=('import pgcat_lane_protocol as p\nimport pgcat_rehearsal_controller as c\n'
             'import pgcat_lane_client as l\nimport pgcat_lane_supervisor as s\nimport provider_feed as f\n'
             'def main():\n'
             ' for module in (c,l,s,f): module._require_verified()\n'
             ' print("NEW", "reconcile-close" in p.NO_SCALE_ACTIONS, c.__file__)\n'
             ' return 0\n')
        stale_cache(runner,new.replace('"NEW"','"OLD"'),new)
        protocol=self.root/'pgcat_lane_protocol.py';source=protocol.read_text()
        # Only the no-scale declaration is changed in the accepted stale cache.
        old=source.replace("NO_SCALE_ACTIONS = frozenset(('baseline-start','baseline-status',",
                           "NO_SCALE_ACTIONS = frozenset(('baseline-start','reconcile-close',")
        stale_cache(protocol,old,source)
        normal=subprocess.run([sys.executable,'-I','-S','-c',
            'import sys;sys.path.insert(0,sys.argv[1]);import pgcat_lane_protocol as p;print("reconcile-close" in p.NO_SCALE_ACTIONS)',str(self.root)],
            stdout=subprocess.PIPE,check=True)
        self.assertEqual(normal.stdout.strip(),b'True')
        got=self.run_entry()
        self.assertEqual(got.returncode,0,got.stderr)
        self.assertIn(b'NEW False',got.stdout)
        self.assertIn(str(self.root/'pgcat_rehearsal_controller.py').encode(),got.stdout)

    def test_feed_entry_ignores_direct_and_transitive_stale_cache(self):
        path=self.root/'provider_feed.py'
        new='import pgcat_lane_protocol as p\ndef main():\n print("NEW", "reconcile-close" in p.NO_SCALE_ACTIONS)\n'
        stale_cache(path,new.replace('NEW','OLD'),new)
        protocol=self.root/'pgcat_lane_protocol.py';source=protocol.read_text()
        stale_cache(protocol,source.replace("NO_SCALE_ACTIONS = frozenset(('baseline-start','baseline-status',",
                "NO_SCALE_ACTIONS = frozenset(('baseline-start','reconcile-close',"),source)
        got=self.run_entry('providerFeed');self.assertEqual(got.returncode,0,got.stderr)
        self.assertEqual(got.stdout.strip(),b'NEW False')

    def test_unlisted_and_preloaded_application_modules_are_refused(self):
        (self.root/'pgcat_lane_runner.py').write_text('import undeclared\ndef main():return 0\n')
        (self.root/'undeclared.py').write_text('raise RuntimeError("must not execute")\n')
        got=self.run_entry();self.assertNotEqual(got.returncode,0)
        self.assertNotIn(b'must not execute',got.stderr)
        for name in ('pgcat_lane_protocol','undeclared'):
            prefix=('x=types.ModuleType('+repr(name)+');x.__file__='+repr(str(self.root/(name+'.py')))+
                    ';sys.modules[x.__name__]=x')
            got=self.run_entry(prefix=prefix);self.assertNotEqual(got.returncode,0)

    def test_changed_source_refuses_before_runner_execution(self):
        hashes=self.hashes();marker=self.root/'marker'
        (self.root/'pgcat_lane_runner.py').write_text('from pathlib import Path\nPath('+repr(str(marker))+').touch()\n')
        got=subprocess.run([sys.executable,'-I','-S','-c',self.bootstrap.ROOT,
            str(self.root/'pgcat_source_bootstrap.py'),hashes['sourceBootstrap'],'runner',json.dumps(hashes)],
            stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5)
        self.assertNotEqual(got.returncode,0);self.assertFalse(marker.exists())

    def test_actual_gated_feed_releases_into_verified_source_snapshot(self):
        marker=self.root/'feed-marker';lane_root=self.root/'lane';lane_root.mkdir(mode=0o700)
        feed=self.root/'provider_feed.py'
        source='from pathlib import Path\nimport capture_window\ndef main():\n Path('+repr(str(marker))+').write_text("NEW"+capture_window.PROBE)\n'
        stale_cache(feed,source.replace('"NEW"','"OLD"'),source)
        capture=self.root/'capture_window.py';source=capture.read_text()+'\nPROBE="NEW"\n'
        stale_cache(capture,source.replace('PROBE="NEW"','PROBE="OLD"'),source)
        runner=self.root/'pgcat_lane_runner.py'
        runner.write_text('''import json,sys,uuid
from pathlib import Path
from pgcat_lane_client import LaneClient
from pgcat_lane_protocol import Journal,encode,digest

def main():
 root=Path(__file__).parent;outer=json.loads((root/'outer.json').read_text());b=outer['laneBindings']
 c=LaneClient();c.root=Path(outer['laneRoot']);c.bindings=b;c.mode='no-scale';c.run_id=str(uuid.uuid4());c.bundle=digest(b['sourceHashes']);c._active=lambda:None
 c.journal=Journal(c.root);c.journal.create_run(c.run_id,'no-scale',b['target'],dict(b['sourceHashes'],bundleSha256=c.bundle))
 original=c.journal.bind_feed
 def bind(value):
  if (root/'feed-marker').exists():raise RuntimeError('feed escaped gate')
  original(value)
 c.journal.bind_feed=bind
 argv=[sys.executable,'-u',b['providerFeedPath'],'--mode','disabled-rehearsal','--interval','15','--max-seconds','180','--workbench',b['laneWrapper'],'--worker-inventory',outer['workerInventory'],'--kubeconfig',outer['workerKubeconfig'],'--status-file',b['receiptPath'],'--run-context',str(c.root/'run-context.json'),'--contract-dir',b['contractDir'],'--no-scale-target',encode(b['target']).decode('ascii')]
 child=c.start_feed(argv,{},root/'outer.json')
 if c.feed_binding['argvSha256']!=digest(child.args[6:]):raise RuntimeError('wrong bound argv')
 code=child.wait(timeout=5);c.confirm_feed_exit(child)
 return code
''')
        hashes=self.hashes()
        target=dict(accountId='account',regionId='region',workerId='worker',scalingGroupId='group',nlbServerGroupId='nlb',originalInstanceIds=['a','b'])
        b=dict(sourceHashes=hashes,target=target,providerFeedPath=str(feed),laneWrapper=str(self.root/'pgcat_workbench_lane.py'),receiptPath=str(self.root/'receipt.json'),contractDir=str(self.root/'contract'))
        outer=dict(laneBindings=b,laneRoot=str(lane_root),workerInventory='/unused/inventory',workerKubeconfig='/unused/kubeconfig',sourceHashes={k:hashes[k] for k in ('client','controller','runner')})
        file=self.root/'outer.json';file.write_text(json.dumps(outer));file.chmod(0o600)
        got=self.run_entry();self.assertEqual(got.returncode,0,got.stderr)
        self.assertEqual(marker.read_text(),'NEWNEW')

    def test_verified_wrapper_preserves_exec_pid_lock_and_absolute_deadline(self):
        import fcntl
        import time
        from test_pgcat_workbench_lane import LaneTests,ARGV
        fixture=LaneTests();fixture.setUp();self.addCleanup(fixture.doCleanups)
        hashes=self.hashes()
        argv=[sys.executable,'-I','-S','-c',self.bootstrap.ROOT,
            str(self.root/'pgcat_source_bootstrap.py'),hashes['sourceBootstrap'],'laneWrapper',json.dumps(hashes)]+ARGV
        owner=subprocess.Popen(argv,env=fixture.environment(FAKE_SLEEP='3'),stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        fixture.processes.append(owner)
        started=fixture.wait_starts(1)[0];self.assertEqual(started['pid'],owner.pid)
        with (fixture.root/'lane.lock').open('r') as lock:
            with self.assertRaises(BlockingIOError):fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        deadline=time.clock_gettime(time.CLOCK_MONOTONIC)+.15
        waiter=subprocess.run(argv,env=fixture.environment(PGCAT_LANE_DEADLINE_MONOTONIC=str(deadline)),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=2)
        self.assertEqual(waiter.stderr,b'PGCAT_LANE_REFUSED:deadline\n')
        self.assertLess(time.clock_gettime(time.CLOCK_MONOTONIC)-deadline,.3)
        self.assertIsNone(owner.poll());owner.kill();owner.communicate(timeout=2)
        self.assertEqual(subprocess.run(argv,env=fixture.environment(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=3).returncode,0)
        count=len(fixture.rows())
        wrapper=self.root/'pgcat_workbench_lane.py';wrapper.write_bytes(wrapper.read_bytes()+b'\n# changed\n')
        refused=subprocess.run(argv,env=fixture.environment(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=3)
        self.assertNotEqual(refused.returncode,0);self.assertEqual(len(fixture.rows()),count)

if __name__ == '__main__':
    unittest.main()
