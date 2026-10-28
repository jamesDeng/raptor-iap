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
        self.write_receipt()
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
