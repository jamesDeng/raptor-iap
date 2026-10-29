"""Offline guards for the worker's Python 3.6 syntax and stdlib boundaries.

These checks run on the development interpreter; they do not replace an actual
Python 3.6 runtime qualification.
"""
import __future__
import ast
import importlib.util
import json
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import finish_clients
import provider_feed
import worker_capture


class WorkerPython36Tests(unittest.TestCase):
    def test_worker_annotations_evaluate_without_postponement(self):
        class Python36Type(type):
            def __or__(cls, other):
                raise TypeError('Python 3.6 types do not support union operators')

        class Python36Path(metaclass=Python36Type):
            pass

        path = Path(worker_capture.__file__)
        tree = ast.parse(path.read_text(), filename=str(path))
        namespace = dict(vars(worker_capture), Path=Python36Path)
        for node in ast.walk(tree):
            if isinstance(node, ast.FunctionDef):
                annotations = [arg.annotation for arg in node.args.args + node.args.kwonlyargs]
                for annotation in annotations + [node.returns]:
                    if annotation is not None:
                        eval(compile(ast.Expression(annotation), str(path), 'eval'), namespace)

    def test_worker_modules_compile_for_python36_and_import(self):
        for name in ('capture_window', 'segmented_capture', 'worker_capture', 'finish_clients', 'pgcat_native_evidence', 'pgcat_native_ingress'):
            with self.subTest(module=name):
                path = Path(__file__).with_name(name + '.py')
                tree = ast.parse(path.read_text(), filename=str(path), feature_version=(3, 6))
                # ast.parse alone accepts future imports unknown to Python 3.6.
                for node in ast.walk(tree):
                    if isinstance(node, ast.ImportFrom) and node.module == '__future__':
                        for alias in node.names:
                            release = getattr(__future__, alias.name).getOptionalRelease()
                            self.assertLessEqual(release[:2], (3, 6), alias.name)
                spec = importlib.util.spec_from_file_location('compat_' + name, path)
                spec.loader.exec_module(importlib.util.module_from_spec(spec))

    def test_atomic_boundary_write_uses_python36_unlink_and_never_replaces(self):
        unlink = Path.unlink

        def unlink_python36(path):
            return unlink(path)

        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'request.json'
            with patch.object(Path, 'unlink', unlink_python36):
                finish_clients.atomic_json(path, {'requestId': 'first'})
                with self.assertRaises(FileExistsError):
                    finish_clients.atomic_json(path, {'requestId': 'second'})
            self.assertEqual(json.loads(path.read_text()), {'requestId': 'first'})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(list(Path(directory).iterdir()), [path])

    def test_atomic_cleanup_tolerates_only_missing_temporary(self):
        unlink = Path.unlink
        link = finish_clients.os.link

        def link_then_remove_temporary(source, destination):
            link(source, destination)
            unlink(source)

        def unlink_python36(path):
            return unlink(path)

        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'ack.json'
            with patch.object(Path, 'unlink', unlink_python36), patch.object(finish_clients.os, 'link', link_then_remove_temporary):
                finish_clients.atomic_json(path, {'requestId': 'first'})
            self.assertEqual(json.loads(path.read_text()), {'requestId': 'first'})
            with patch.object(Path, 'unlink', side_effect=PermissionError('cleanup refused')):
                with self.assertRaises(PermissionError):
                    finish_clients.atomic_json(Path(directory) / 'receipt.json', {})

    def test_remote_targets_patch_and_verify_with_python36_subprocess_signature(self):
        body = {'metadata': {'name': 'pgcat-provider-inventory', 'namespace': 'raptor-test', 'resourceVersion': '42'}, 'data': {'targets.json': '[]'}}
        calls = []

        # Only kubectl is external. The actual remote script parses and builds
        # its read and resourceVersion-bound patch under this 3.6 API boundary.
        def run_python36(argv, *, check, stdout, stderr, universal_newlines, timeout):
            self.assertTrue(check and universal_newlines)
            self.assertEqual((stdout, stderr), (subprocess.PIPE, subprocess.PIPE))
            self.assertGreater(timeout, 0)
            self.assertLessEqual(timeout, 8)
            calls.append(argv)
            if argv[5] == 'patch':
                body['data'].update(json.loads(argv[-1])['data'])
            return SimpleNamespace(stdout=json.dumps(body) if argv[5] == 'get' else '')

        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'offline-config'
            path.write_text('offline fixture')
            path.chmod(0o600)
            targets = [{'targets': ['10.0.0.1:9930'], 'labels': {'ecs_instance_id': 'i-fixture'}}]
            with patch.object(provider_feed.subprocess, 'run', run_python36), patch.object(sys, 'argv', ['remote', str(path), json.dumps(targets)]):
                exec(provider_feed._TARGET_SCRIPT, {'_remote_deadline': time.monotonic() + 10})
            self.assertEqual([argv[5] for argv in calls], ['get', 'patch', 'get'])
            self.assertEqual(json.loads(calls[1][-1]), {'metadata': {'resourceVersion': '42'}, 'data': {'targets.json': json.dumps(targets)}})

    def test_scaling_group_query_emits_indexed_exact_filter(self):
        operator = provider_feed.Operator(SimpleNamespace())
        group = {'ScalingGroupId': 'asg-t4necb4dfmu6o3z2rn0g', 'VpcId': provider_feed.VPC_ID, 'DesiredCapacity': 2}
        body = {'PageNumber': 1, 'TotalCount': 1, 'ScalingGroups': {'ScalingGroup': [group]}}
        calls = []

        def command(argv):
            calls.append(argv)
            return json.dumps(body)

        operator.command = command
        self.assertEqual(operator.fetch_group(), group)
        self.assertEqual(calls, [['aliyun', 'ess', 'DescribeScalingGroups', '--profile', 'infra-ops-poc', '--region', 'ap-southeast-1', '--retry-count', '2', '--ScalingGroupId.1', 'asg-t4necb4dfmu6o3z2rn0g', '--PageNumber', '1', '--PageSize', '1']])
        for invalid in (dict(body, TotalCount=2), dict(body, PageNumber=2), dict(body, ScalingGroups={'ScalingGroup': [group, group]})):
            operator.command = lambda argv: json.dumps(invalid)
            with self.assertRaises(ValueError):
                operator.fetch_group()


if __name__ == '__main__':
    unittest.main()
