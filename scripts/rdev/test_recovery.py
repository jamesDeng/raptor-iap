from pathlib import Path
import json
import signal
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives import serialization
from recovery import durable_workdir, decrypt_bundle, TerraformRunner

SHA = 'a' * 40


class RecoveryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.private_key = rsa.generate_private_key(public_exponent=65537, key_size=3072)

    def test_failed_apply_retains_and_encrypts_local_recovery_state(self):
        with tempfile.TemporaryDirectory() as name:
            base = Path(name); public = base / 'public.pem'
            public.write_bytes(self.private_key.public_key().public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo))
            with self.assertRaises(ValueError):
                with durable_workdir(public, base / 'encrypted', SHA, parent=base) as work:
                    work = Path(work)
                    (work / 'errored.tfstate').write_text('{"resource":"created-but-not-persisted"}')
                    (work / 'apply.stderr.log').write_text('private-provider-diagnostic')
                    raise ValueError('remote persistence failed')
            self.assertTrue((work / 'errored.tfstate').exists())
            artifact = base / 'encrypted/recovery.json'
            self.assertNotIn('created-but-not-persisted', artifact.read_text())
            recovered = base / 'recovered'
            decrypt_bundle(artifact, self.private_key, recovered)
            self.assertEqual((recovered / 'errored.tfstate').read_text(), '{"resource":"created-but-not-persisted"}')
            self.assertEqual((recovered / 'apply.stderr.log').read_text(), 'private-provider-diagnostic')
            envelope = json.loads(artifact.read_text()); envelope['source_sha'] = 'b' * 40; artifact.write_text(json.dumps(envelope))
            with self.assertRaises(Exception): decrypt_bundle(artifact, self.private_key, base / 'tampered')

    def test_real_main_failure_keeps_recovery_state(self):
        from live_plan import main
        from test_live_plan import fixture
        with tempfile.TemporaryDirectory() as name:
            base = Path(name); public = base / 'public.pem'
            public.write_bytes(self.private_key.public_key().public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo))
            captured = []
            def simulate(directory, args):
                if args[0] == 'show': return json.dumps(fixture())
                if args[0] == 'apply':
                    state = Path(directory) / 'errored.tfstate'; state.write_text('local-recovery')
                    captured.append(state)
                    raise ValueError('remote persistence failed')
                return ''
            def old_run(command, **kwargs):
                directory = command[1].split('=', 1)[1]
                try: return subprocess.CompletedProcess(command, 0, simulate(directory, command[2:]), '')
                except ValueError: return subprocess.CompletedProcess(command, 1, '', 'private-error')
            def new_run(runner, args, **kwargs): return simulate(runner.directory, args)
            env = {'GITHUB_REF': 'refs/heads/main', 'GITHUB_SHA': SHA, 'RDEV_EXPECTED_SOURCE_SHA': SHA,
                   'RDEV_APPLY_ROLE_ARN': 'acs:ram::123456:role/raptor-iap-rdev-apply',
                   'RDEV_STATE_BUCKET': 'raptor-iap-tfstate-sg-200743',
                   'RDEV_LOCK_ENDPOINT': 'https://raptor-tf-lock.ap-southeast-1.ots.aliyuncs.com',
                   'RUNNER_TEMP': str(base), 'RDEV_RECOVERY_PUBLIC_KEY': str(public)}
            with patch.dict('os.environ', env), patch('initial_apply.validate_release'), patch('live_plan.subprocess.run', side_effect=old_run), patch('recovery.TerraformRunner.run', new_run):
                with self.assertRaises(ValueError): main(provision=True)
            self.assertEqual(len(captured), 1)
            self.assertTrue(captured[0].exists(), 'main deleted the only local recovery state')
            self.assertTrue((base / 'rdev-recovery/recovery.json').exists())

    def test_timeout_interrupts_once_and_does_not_kill_or_retry(self):
        with tempfile.TemporaryDirectory() as name:
            process = Mock(); process.wait.side_effect = [subprocess.TimeoutExpired('terraform', 2400), 0]
            factory = Mock(return_value=process)
            runner = TerraformRunner(Path(name), popen=factory, clock=lambda: 0)
            with self.assertRaises(ValueError): runner.run(['apply', 'foundation.tfplan'], timeout=2400)
            process.send_signal.assert_called_once_with(signal.SIGINT)
            process.kill.assert_not_called()
            self.assertEqual(factory.call_count, 1)
            self.assertEqual(process.wait.call_args_list[-1].kwargs['timeout'], 120)

    def test_slow_preparation_blocks_apply_before_launch(self):
        with tempfile.TemporaryDirectory() as name:
            factory = Mock()
            clock = Mock(side_effect=[0, 1600])
            runner = TerraformRunner(Path(name), popen=factory, clock=clock)
            with self.assertRaises(ValueError): runner.run(['apply', 'foundation.tfplan'], timeout=2400)
            factory.assert_not_called()

    def test_successful_command_reads_stdout_without_printing_private_stderr(self):
        with tempfile.TemporaryDirectory() as name:
            runner = TerraformRunner(Path(name))
            result = runner.run(['version'], timeout=10, executable=['python3', '-c', 'import sys;print("safe-output");print("private-error",file=sys.stderr)'])
            self.assertEqual(result.strip(), 'safe-output')
            self.assertIn('private-error', (Path(name) / 'version.stderr.log').read_text())


if __name__ == '__main__': unittest.main()
