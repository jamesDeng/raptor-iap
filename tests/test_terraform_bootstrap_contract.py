"""Actual local Terraform output JSON must load through the bootstrap contract."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from tools.agenticfs_bootstrap.config import load_config

ROOT = Path(__file__).resolve().parents[1]

class TerraformContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.tmp.cleanup)
        cls.directory = Path(cls.tmp.name)
        shutil.copytree(ROOT / "tests/fixtures/terraform-bootstrap-contract", cls.directory, dirs_exist_ok=True)
        binary = os.environ.get("TERRAFORM_BIN", str(ROOT / ".raptor-local/bin/terraform"))
        for command in (["init", "-backend=false", "-input=false"], ["apply", "-auto-approve", "-input=false"], ["output", "-json"]):
            result = subprocess.run([binary, *command], cwd=cls.directory, capture_output=True, text=True, timeout=60)
            if result.returncode:
                raise AssertionError("Local-only Terraform fixture failed: " + command[0])
            if command[0] == "output":
                cls.outputs = json.loads(result.stdout)

    def config(self, outputs):
        path = self.directory / "bootstrap-config.json"
        path.write_text(json.dumps({
            "account_id": "1234567890123456", "region": "ap-southeast-1", "zone": "ap-southeast-1a",
            "team_id": "team-fixture", "run_id": "contract-test", "runtime_uid": 1000, "runtime_gid": 1000,
            "terraform_outputs": outputs,
        }))
        return path

    def test_terraform_outputs_load_into_bootstrap(self):
        cfg = load_config(self.config(self.outputs))
        self.assertEqual(cfg.vpc_id, "vpc-fixture")
        self.assertEqual(cfg.vswitch_id, "vsw-fixture")
        self.assertEqual(cfg.security_group_id, "sg-fixture")
        self.assertEqual(cfg.execution_role_arn, "acs:ram::1234567890123456:role/raptor-storage")

    def test_missing_named_output_is_rejected(self):
        outputs = dict(self.outputs)
        outputs.pop("vswitch_id")
        with self.assertRaisesRegex(ValueError, "InvalidConfig"):
            load_config(self.config(outputs))
