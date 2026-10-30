"""Offline process-level contract tests; never invoke a real Workbench CLI."""
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
import uuid

WRAPPER = Path(__file__).with_name('pgcat_workbench_lane.py')
SESSION = '12345678-1234-4234-8234-123456789abc'
ARGV = ['exec', '-P', 'infra-ops-poc', '-r', 'ap-southeast-1',
        '-i', 'i-t4nj1bitmcz7cuoch82h', '-c', 'synthetic-private-command']


class LaneTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(WRAPPER.exists(), 'shared Workbench wrapper is absent')
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.cli = self.root / 'fake-cli'
        self.cli.write_text('#!' + sys.executable + '\n' + '''
import json, os, sys, time
from pathlib import Path
p = Path(os.environ['FAKE_EVENTS'])
with p.open('a') as stream:
    stream.write(json.dumps({'event':'start','pid':os.getpid(),'at':time.clock_gettime(time.CLOCK_MONOTONIC),'argv':sys.argv[1:],'deadline':os.environ.get('PGCAT_LANE_DEADLINE_MONOTONIC')})+'\\n')
print('ALLOWLISTED', flush=True)
time.sleep(float(os.environ.get('FAKE_SLEEP','0')))
with p.open('a') as stream:
    stream.write(json.dumps({'event':'end','pid':os.getpid(),'at':time.clock_gettime(time.CLOCK_MONOTONIC)})+'\\n')
''')
        self.cli.chmod(0o700)
        self.events = self.root / 'events'
        self.receipt = self.root / 'receipt.json'
        self.intents = self.root / 'intents'
        self.intents.mkdir(mode=0o700)
        self.processes = []
        self.addCleanup(self.stop_processes)

    def stop_processes(self):
        for process in self.processes:
            if process.poll() is None:
                process.kill()
            process.communicate()

    def environment(self, role='provider', **changes):
        env = dict(os.environ, PGCAT_LANE_ROLE=role,
                   PGCAT_LANE_LOCK=str(self.root / 'lane.lock'),
                   PGCAT_LANE_REAL_CLI=str(self.cli),
                   PGCAT_LANE_DEADLINE_MONOTONIC=str(time.clock_gettime(time.CLOCK_MONOTONIC)+3),
                   PGCAT_LANE_RECEIPT=str(self.receipt), PGCAT_LANE_SESSION=SESSION,
                   PGCAT_LANE_MIN_SEQUENCE='1', PGCAT_LANE_INTENT_DIR=str(self.intents),
                   FAKE_EVENTS=str(self.events))
        env.update(changes)
        return env

    def write_receipt(self, **changes):
        value = dict(feedSession=SESSION, sequence=1, mode='disabled-rehearsal',
                     desired=2, observedAt=time.time(),rosterSha256='a'*64)
        value.update(changes)
        temporary = self.root / ('new-' + str(uuid.uuid4()))
        temporary.write_text(json.dumps(value))
        temporary.chmod(0o600)
        temporary.replace(self.receipt)
        return value

    def run_lane(self, env=None, argv=None, timeout=3):
        return subprocess.run([sys.executable, str(WRAPPER)] + (ARGV if argv is None else argv),
                              env=env or self.environment(), stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, timeout=timeout)

    def launch(self, env):
        process = subprocess.Popen([sys.executable, str(WRAPPER)] + ARGV, env=env,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.processes.append(process)
        return process

    def rows(self):
        return [json.loads(line) for line in self.events.read_text().splitlines()] if self.events.exists() else []

    def wait_starts(self, count):
        until = time.clock_gettime(time.CLOCK_MONOTONIC)+2
        while time.clock_gettime(time.CLOCK_MONOTONIC) < until:
            starts = [row for row in self.rows() if row['event'] == 'start']
            if len(starts) >= count:
                return starts
            time.sleep(.01)
        self.fail('fake CLI did not start')

    def refused(self, result, category=None):
        self.assertEqual(result.returncode, 75, result.stderr)
        self.assertEqual(result.stdout, b'')
        self.assertTrue(result.stderr.startswith(b'PGCAT_LANE_REFUSED:'))
        self.assertNotIn(b'synthetic-private-command', result.stderr)
        self.assertNotIn(bytes(str(self.root), 'utf8'), result.stderr)
        if category:
            self.assertEqual(result.stderr, ('PGCAT_LANE_REFUSED:'+category+'\n').encode())

    def test_fast_call_preserves_exact_argv_and_returns_output(self):
        result = self.run_lane()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, b'ALLOWLISTED\n')
        self.assertEqual(self.rows()[0]['argv'], ARGV)
        self.assertEqual((self.root/'lane.lock').stat().st_mode & 0o777, 0o600)

    def test_two_cli_processes_exclude_each_other_with_stable_lock_inode(self):
        first = self.launch(self.environment(FAKE_SLEEP='.25'))
        self.wait_starts(1)
        inode = (self.root/'lane.lock').stat().st_ino
        second = self.launch(self.environment())
        self.assertEqual(first.communicate(timeout=2)[1], b'')
        self.assertEqual(second.communicate(timeout=2)[1], b'')
        rows = self.rows()
        self.assertEqual([row['event'] for row in rows], ['start','end','start','end'])
        self.assertGreaterEqual(rows[2]['at'], rows[1]['at'])
        self.assertEqual((self.root/'lane.lock').stat().st_ino, inode)

    def test_provider_wait_expires_at_original_absolute_deadline(self):
        first = self.launch(self.environment(FAKE_SLEEP='.5'))
        self.wait_starts(1)
        started = time.clock_gettime(time.CLOCK_MONOTONIC)
        result = self.run_lane(self.environment(PGCAT_LANE_DEADLINE_MONOTONIC=str(started+.12)))
        self.refused(result, 'deadline')
        self.assertLess(time.clock_gettime(time.CLOCK_MONOTONIC)-started, .4)
        self.assertIsNone(first.poll())
        self.assertEqual(len(self.rows()), 1)

    def test_provider_startup_command_supplies_deadline(self):
        from provider_feed import Operator
        from types import SimpleNamespace
        from unittest.mock import patch
        op = Operator(SimpleNamespace())
        environment = self.environment()
        environment.pop('PGCAT_LANE_DEADLINE_MONOTONIC')
        started = time.clock_gettime(time.CLOCK_MONOTONIC)
        with patch.dict(os.environ, environment, clear=True):
            result = op.command([sys.executable, str(WRAPPER)] + ARGV)
        self.assertEqual(result, 'ALLOWLISTED\n')
        deadline = float(self.rows()[0]['deadline'])
        self.assertGreaterEqual(deadline, started + 9.5)
        self.assertLessEqual(deadline, started + 10.1)
        self.assertIsNone(op.deadline, 'startup must not create a publication tick')

    def test_provider_command_passes_the_same_absolute_deadline_to_subprocess(self):
        from provider_feed import Deadline, Operator
        from types import SimpleNamespace
        op = Operator(SimpleNamespace())
        op.deadline = Deadline(time.monotonic()+2)
        result = op.command([sys.executable, '-c',
                             "import os; print(os.environ.get('PGCAT_LANE_DEADLINE_MONOTONIC','missing'))"])
        remaining = float(result)-time.clock_gettime(time.CLOCK_MONOTONIC)
        self.assertGreater(remaining, 1.5)
        self.assertLessEqual(remaining, 2)

    def test_external_refuses_stale_mtime_observation_session_or_sequence(self):
        for change in ({'observedAt':time.time()-7.1}, {'observedAt':time.time()+1},
                       {'feedSession':str(uuid.uuid4())}, {'sequence':0},
                       {'sequence':True}, {'observedAt':float('nan')}, {'desired':4},
                       {'mode':'full-replacement'}, {'rosterSha256':'A'*64},
                       {'rosterSha256':'0'*63}):
            with self.subTest(change=change):
                self.write_receipt(**change)
                self.refused(self.run_lane(self.environment('external')), 'receipt')
        self.write_receipt()
        os.utime(self.receipt, (time.time()-1.1, time.time()-1.1))
        self.refused(self.run_lane(self.environment('external')), 'receipt')
        self.write_receipt(sequence=1)
        self.refused(self.run_lane(self.environment('external', PGCAT_LANE_MIN_SEQUENCE='2')), 'receipt')
        self.assertEqual(self.rows(), [])

    def test_receipt_replaced_while_waiting_for_lock_refuses_launch(self):
        self.write_receipt()
        with (self.root/'lane.lock').open('w') as lock:
            os.chmod(lock.name, 0o600)
            fcntl.flock(lock, fcntl.LOCK_EX)
            waiting = self.launch(self.environment('external'))
            time.sleep(.15)
            self.write_receipt()
            fcntl.flock(lock, fcntl.LOCK_UN)
        out, err = waiting.communicate(timeout=2)
        self.refused(subprocess.CompletedProcess([], waiting.returncode, out, err), 'receipt')
        self.assertEqual(self.rows(), [])

    def test_receipt_ages_while_waiting_for_lock_refuses_launch(self):
        self.write_receipt()
        with (self.root/'lane.lock').open('w') as lock:
            os.chmod(lock.name, 0o600)
            fcntl.flock(lock, fcntl.LOCK_EX)
            waiting = self.launch(self.environment('external'))
            out, err = waiting.communicate(timeout=2)
        self.refused(subprocess.CompletedProcess([], waiting.returncode, out, err), 'receipt')
        self.assertEqual(self.rows(), [])

    def test_one_publication_sequence_cannot_launch_twice(self):
        self.write_receipt(sequence=9)
        env = self.environment('external', PGCAT_LANE_MIN_SEQUENCE='9')
        self.assertEqual(self.run_lane(env).returncode, 0)
        # Even a fresh copy of the same publication cannot reuse its sequence.
        self.write_receipt(sequence=9)
        self.refused(self.run_lane(env), 'intent')
        self.assertEqual(len([row for row in self.rows() if row['event']=='start']), 1)
        intents = list(self.intents.iterdir())
        self.assertEqual(len(intents), 1)
        self.assertEqual(intents[0].stat().st_mode & 0o777, 0o600)
        self.write_receipt(sequence=10)
        self.assertEqual(self.run_lane(env).returncode, 0)

    def test_timeout_kills_exact_cli_pid_and_consumes_intent_before_lock_releases(self):
        self.write_receipt()
        env = self.environment('external', FAKE_SLEEP='3')
        process = self.launch(env)
        cli_pid = self.wait_starts(1)[0]['pid']
        self.assertEqual(cli_pid, process.pid, 'wrapper must exec CLI in the same PID')
        self.refused(self.run_lane(self.environment(PGCAT_LANE_DEADLINE_MONOTONIC=str(time.clock_gettime(time.CLOCK_MONOTONIC)+.1))), 'deadline')
        self.assertIsNone(process.poll(), 'lock waiter must not kill the owning CLI')
        with self.assertRaises(subprocess.TimeoutExpired):
            process.communicate(timeout=.05)
        process.kill()
        process.communicate(timeout=1)
        with self.assertRaises(ProcessLookupError):
            os.kill(cli_pid, 0)
        self.assertEqual(self.run_lane().returncode, 0)
        self.write_receipt()
        self.refused(self.run_lane(self.environment('external')), 'intent')

    def test_subprocess_run_timeout_has_no_wrapper_child_left_holding_lock(self):
        from unittest.mock import patch
        self.write_receipt()
        original=subprocess.Popen.communicate
        def after_exec(process,*args,**kwargs):
            if kwargs.get('timeout')==.7:self.wait_starts(1)
            return original(process,*args,**kwargs)
        # Start the short communication timeout after the actual exec handshake;
        # subprocess.run must still kill/wait the same PID and release its lock.
        with patch.object(subprocess.Popen,'communicate',after_exec):
            with self.assertRaises(subprocess.TimeoutExpired):
                self.run_lane(self.environment('external', FAKE_SLEEP='3'), timeout=.7)
        cli_pid = self.rows()[0]['pid']
        with self.assertRaises(ProcessLookupError):
            os.kill(cli_pid, 0)
        self.assertEqual(self.run_lane().returncode, 0)
        self.write_receipt()
        self.refused(self.run_lane(self.environment('external')), 'intent')

    def test_wrong_target_or_duplicate_options_refuse_without_exposing_command(self):
        for argv in (ARGV[:], ARGV+['-P','infra-ops-poc'], ['daemon','start']):
            if argv == ARGV:
                argv[4] = 'wrong-region'
            with self.subTest(argv=argv):
                self.refused(self.run_lane(argv=argv), 'configuration')
        self.assertEqual(self.rows(), [])

    def test_unsafe_lock_receipt_and_intent_paths_fail_closed(self):
        lock = self.root/'lane.lock'
        target = self.root/'target'
        target.write_text('preserve')
        lock.symlink_to(target)
        self.refused(self.run_lane(), 'configuration')
        lock.unlink()
        self.write_receipt()
        self.receipt.chmod(0o644)
        self.refused(self.run_lane(self.environment('external')), 'receipt')
        self.receipt.chmod(0o600)
        self.intents.chmod(0o755)
        self.refused(self.run_lane(self.environment('external')), 'configuration')
        self.assertEqual(target.read_text(), 'preserve')
        self.assertEqual(self.rows(), [])


if __name__ == '__main__':
    unittest.main()


class PostStopWrapperTests(unittest.TestCase):
    def setUp(self):
        import test_pgcat_lane_protocol as fixtures
        from pgcat_lane_actions import ActionCatalog
        from pgcat_lane_protocol import encode
        f=fixtures.PostStopJournalTests();f.setUp();self.addCleanup(f.doCleanups);self.f=f
        self.global_dir=f.root.parent/'global';self.global_dir.mkdir(mode=0o700)
        self.marker=f.root.parent/'cli-pid';self.sleep=f.root.parent/'cli-sleep'
        self.sleep.write_text('0')
        cli=Path(f.f.outer['laneBindings']['workbenchCli'])
        cli.write_text('#!'+sys.executable+'\nimport os,time\nfrom pathlib import Path\nPath('+repr(str(self.marker))+').write_text(str(os.getpid()))\ntime.sleep(float(Path('+repr(str(self.sleep))+').read_text()))\n')
        cli.chmod(0o700)
        from pgcat_lane_supervisor import save_once
        save_once(self.global_dir,'active-run',dict(runId=f.f.run,supervisorId=f.supervisor,laneRoot=str(f.root)))
        f.admit()
        b=f.f.outer['laneBindings'];self.argv=['exec','-P','infra-ops-poc','-r','ap-southeast-1','-i',WORKER_ID,'-c',
            ActionCatalog.post_stop_worker_command(b,f.f.run,f.journal.post_stop_context(f.request))]
        self.env=dict(os.environ,PGCAT_LANE_ROLE='post-stop',PGCAT_LANE_ROOT=str(f.root),
            PGCAT_LANE_REQUEST_ID=f.request['requestId'],PGCAT_LANE_REAL_CLI=str(cli),
            PGCAT_LANE_LOCK=b['laneLockPath'])
        self.driver=('import sys;from pathlib import Path;import pgcat_lane_protocol as p;'
            'p._release_admission=lambda *a:None;import pgcat_source_bootstrap as b;b.require_verified=lambda *a:None;'
            'b.current_hashes=lambda:'+repr(b['sourceHashes'])+';'
            'import pgcat_lane_supervisor as s;s.GLOBAL_ROOT=Path(sys.argv[1]);'
            'import pgcat_workbench_lane as w;sys.argv=sys.argv[2:];sys.exit(w.main())')
        self.env['PYTHONPATH']=str(Path(__file__).parent)

    def launch(self,argv=None,env=None):
        process=subprocess.Popen([sys.executable,'-c',self.driver,str(self.global_dir),'wrapper']+(argv or self.argv),
            env=env or self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.addCleanup(lambda:self.clean(process));return process

    def clean(self,process):
        if process.poll() is None:process.kill()
        process.communicate()

    def test_fixed_exec_preserves_same_pid_and_inherited_flock(self):
        self.sleep.write_text('1')
        process=self.launch();until=time.time()+2
        while not self.marker.exists() and time.time()<until:time.sleep(.01)
        self.assertTrue(self.marker.exists(),process.communicate() if process.poll() is not None else 'no exec')
        self.assertEqual(int(self.marker.read_text()),process.pid)
        fd=os.open(self.env['PGCAT_LANE_LOCK'],os.O_RDWR)
        try:
            with self.assertRaises(BlockingIOError):fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
        finally:os.close(fd)
        self.assertEqual(process.wait(timeout=2),0)
        from pgcat_lane_protocol import Refused
        with self.assertRaises(Refused):self.f.journal.post_stop_dispatch(self.f.request['requestId'])

    def test_forged_argv_cli_lock_and_global_owner_refuse_without_claim(self):
        for key,value in [('PGCAT_LANE_REAL_CLI',sys.executable),('PGCAT_LANE_LOCK',str(self.f.root.parent/'other.lock')),
                          ('PGCAT_LANE_ROOT',str(self.f.root.parent))]:
            with self.subTest(key=key):
                process=self.launch(env=dict(self.env,**{key:value}));out,err=process.communicate(timeout=2)
                self.assertNotEqual(process.returncode,0);self.assertFalse(self.marker.exists())
        process=self.launch(argv=self.argv[:-1]+['echo forged']);process.communicate(timeout=2)
        self.assertNotEqual(process.returncode,0)
        self.assertFalse(self.marker.exists())
        self.assertEqual(self.f.journal.post_stop_dispatch(self.f.request['requestId'])['request'],self.f.request)

    def test_timeout_kills_exact_exec_and_claim_cannot_replay(self):
        from pgcat_lane_protocol import Refused
        self.sleep.write_text('3')
        self.driver='import time;time.sleep(.7);'+self.driver
        process=self.launch()
        # Start the short execution timeout only after actual exec has claimed admission.
        # The production request still has its unchanged original five-second deadline.
        until=self.f.request['deadlineMonotonic']
        from pgcat_lane_protocol import monotonic
        while not self.marker.exists() and process.poll() is None and monotonic() < until:time.sleep(.01)
        self.assertTrue(self.marker.exists())
        with self.assertRaises(subprocess.TimeoutExpired):process.communicate(timeout=.5)
        process.kill();process.communicate()
        self.assertEqual(int(self.marker.read_text()),process.pid)
        with self.assertRaises(Refused):self.f.journal.post_stop_dispatch(self.f.request['requestId'])

    def test_role_environment_without_verified_loading_refuses(self):
        process=subprocess.run([sys.executable,str(WRAPPER)]+self.argv,env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.assertNotEqual(process.returncode,0);self.assertFalse(self.marker.exists())

    def test_verified_loader_snapshot_must_match_the_original_admitted_sources(self):
        b=self.f.f.outer['laneBindings']
        self.driver=self.driver.replace('b.current_hashes=lambda:'+repr(b['sourceHashes'])+';',
            'b.current_hashes=lambda:{};')
        process=self.launch();process.communicate(timeout=2)
        self.assertNotEqual(process.returncode,0);self.assertFalse(self.marker.exists())


WORKER_ID = 'i-t4nj1bitmcz7cuoch82h'

class NativeReceiptTests(unittest.TestCase):
    setUp=LaneTests.setUp
    stop_processes=LaneTests.stop_processes
    environment=LaneTests.environment
    write_receipt=LaneTests.write_receipt
    run_lane=LaneTests.run_lane
    refused=LaneTests.refused
    rows=LaneTests.rows
    def test_explicit_native_receipt_profile_preserves_legacy_default(self):
        import inspect,pgcat_workbench_lane as lane
        self.assertIn('expected_mode',inspect.signature(lane.read_receipt).parameters)
        self.write_receipt(mode='full-replacement-direct-v2',desired=4)
        with self.assertRaises(lane.Refused):lane.read_receipt(str(self.receipt),SESSION,1)
        _,value,_=lane.read_receipt(str(self.receipt),SESSION,1,expected_mode='full-replacement-direct-v2')
        self.assertEqual(value['desired'],4)
        for mode in ('full-replacement','direct-v2','',None):
            with self.assertRaises(lane.Refused):lane.read_receipt(str(self.receipt),SESSION,1,expected_mode=mode)
        self.write_receipt()
        lane.read_receipt(str(self.receipt),SESSION,1)
        with self.assertRaises(lane.Refused):lane.read_receipt(str(self.receipt),SESSION,1,expected_mode='full-replacement-direct-v2')
        self.write_receipt(mode='full-replacement-direct-v2',desired=4)
        self.refused(self.run_lane(self.environment('external',PGCAT_LANE_MODE='full-replacement-direct-v2')),'receipt')
        self.assertEqual(self.rows(),[])
