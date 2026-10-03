"""Boundary tests: malformed inputs, crash safety and lifecycle side effects."""
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from tools.agenticfs_bootstrap.config import load_config, read_inventory, save_inventory


def fixture():
    return {
        "account_id": "1234567890123456", "region": "ap-southeast-1",
        "zone": "ap-southeast-1a", "team_id": "team-fixture", "run_id": "offline-test",
        "runtime_uid": 1000, "runtime_gid": 1000,
        "terraform_outputs": {
            "vpc_id": {"value": "vpc-fixture"},
            "vswitch_id": {"value": "vsw-fixture"},
            "security_group_id": {"value": "sg-fixture"},
            "execution_role_arn": {"value": "acs:ram::1234567890123456:role/raptor-storage"},
        },
    }


class ConfigTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def write_config(self, data):
        path = self.root / "input.json"
        path.write_text(json.dumps(data))
        return path

    def test_config_rejects_wrong_region_and_missing_outputs(self):
        for field in ("region", "zone", "terraform_outputs", "runtime_uid"):
            data = fixture()
            data[field] = {"region": "cn-hangzhou", "zone": "ap-southeast-1b",
                           "terraform_outputs": {}, "runtime_uid": True}[field]
            with self.subTest(field=field), self.assertRaises(ValueError):
                load_config(self.write_config(data))
        config = load_config(self.write_config(fixture()))
        self.assertEqual(config.vpc_id, "vpc-fixture")
        self.assertEqual(config.run_id, "offline-test")

    def test_inventory_rejects_unknown_secret_fields(self):
        path = self.root / "state" / "inventory.json"
        for field in ("access_token", "refresh_token", "api_key"):
            with self.subTest(field=field), self.assertRaises(ValueError):
                save_inventory(path, {"schema_version": 1, field: "synthetic"})
        path.parent.mkdir(exist_ok=True)
        path.write_text('{"schema_version":1,"api_key":"synthetic"}')
        with self.assertRaises(ValueError):
            read_inventory(path)

    def test_inventory_replace_preserves_previous_record_on_failure(self):
        path = self.root / "state" / "inventory.json"
        before = {"schema_version": 1, "filesystem_id": "fs-before"}
        save_inventory(path, before)
        with patch("os.replace", side_effect=OSError("injected")):
            with self.assertRaises(OSError):
                save_inventory(path, {"schema_version": 1, "filesystem_id": "fs-after"})
        self.assertEqual(read_inventory(path), before)
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(path.parent.stat().st_mode & 0o777, 0o700)
        self.assertEqual(list(path.parent.iterdir()), [path])


if __name__ == "__main__":
    unittest.main()
