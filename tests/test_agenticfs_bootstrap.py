"""Boundary tests: malformed inputs, crash safety and lifecycle side effects."""
import json
import os
from pathlib import Path
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from tools.agenticfs_bootstrap.config import load_config, read_inventory, save_inventory
from tools.agenticfs_bootstrap.api import StorageAPI, StorageError
from tools.agenticfs_bootstrap.lifecycle import ensure_storage


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


class FakeTransport:
    """Only the external SDK call is replaced; real request models serialize."""
    def __init__(self):
        self.calls = []
        self.data = {}
        self.fail = None

    def __getattr__(self, name):
        def call(*args):
            request = args[-1].to_map() if args else {}
            self.calls.append((name, request))
            if self.fail == name:
                raise RuntimeError("synthetic-secret-do-not-print")
            if name == "get_caller_identity":
                result = {"AccountId": "1234567890123456"}
            elif name == "create_file_system":
                self.data["fs"] = dict(request, Tags={"Tag": request["Tag"]}, FileSystemId="fs-test", Status="Running", RegionId="ap-southeast-1")
                result = {"FileSystemId": "fs-test"}
            elif name == "describe_file_systems":
                result = {"TotalCount": 1 if "fs" in self.data else 0,
                          "FileSystems": {"FileSystem": [self.data["fs"]] if "fs" in self.data else []}}
            elif name == "create_agentic_space":
                self.data["space"] = dict(request, AgenticSpaceId="space-test", Status="Running", SpaceUsage=0, FileCountUsage=0)
                result = {"AgenticSpaceId": "space-test"}
            elif name == "get_agentic_space":
                result = {"AgenticSpace": self.data["space"]}
            elif name == "describe_agentic_spaces":
                result = {"AgenticSpaces": {"AgenticSpace": [self.data["space"]] if "space" in self.data else []}}
            elif name == "create_access_point":
                self.data["ap"] = dict(request, Tags=request["Tag"], VSwitchId=request["VswId"], AccessPointId="ap-test", DomainName="ap-test.fs-test-w.ap-southeast-1.nas.aliyuncs.com", Status="active")
                result = {"AccessPoint": {"AccessPointId": "ap-test", "AccessPointDomain": self.data["ap"]["DomainName"]}}
            elif name == "describe_access_point":
                result = {"AccessPoint": self.data["ap"]}
            elif name == "describe_access_points":
                result = {"AccessPoints": [self.data["ap"]] if "ap" in self.data else []}
            elif name == "create_volume":
                self.data["volume"] = dict(request["body"], volumeID="vol-test", status="active")
                result = {"volume": self.data["volume"]}
            elif name == "get_volume":
                result = {"volume": self.data["volume"]}
            elif name == "list_volumes":
                result = {"volumes": [self.data["volume"]] if "volume" in self.data else []}
            else:
                raise AssertionError(name)
            return SimpleNamespace(body=SimpleNamespace(to_map=lambda: result))
        return call


class LifecycleTests(ConfigTests):
    def setUp(self):
        super().setUp()
        self.cfg = load_config(self.write_config(fixture()))
        self.transport = FakeTransport()
        self.api = StorageAPI(self.cfg, self.transport, self.transport, self.transport)
        self.path = self.root / "state" / "inventory.json"

    def test_setup_request_fields_and_order(self):
        state = ensure_storage(self.api, self.cfg, self.path)
        creates = [(name, req) for name, req in self.transport.calls if name.startswith("create_")]
        self.assertEqual([x[0] for x in creates], ["create_file_system", "create_agentic_space", "create_access_point", "create_volume"])
        self.assertEqual(creates[0][1]["StorageType"], "Agentic")
        self.assertEqual(creates[0][1]["ProtocolType"], "NFS")
        self.assertEqual(creates[1][1]["Quota"], {"SizeLimit": 10737418240, "FileCountLimit": 10000})
        self.assertEqual(creates[1][1]["Azone"], "ap-southeast-1a")
        ap = creates[2][1]
        self.assertEqual(ap["AgenticSpaceId"], "space-test")
        self.assertIs(ap["EnabledRam"], True)
        self.assertFalse({"AccessGroup", "RootDirectory", "PosixUserId"} & set(ap))
        self.assertEqual(creates[3][1]["body"]["agenticFSVolumeConfig"]["serverAddr"], "ap-test.fs-test-w.ap-southeast-1.nas.aliyuncs.com:/")
        self.assertEqual(state["volume_id"], "vol-test")

    def test_rerun_reuses_recorded_ids(self):
        ensure_storage(self.api, self.cfg, self.path)
        self.transport.calls.clear()
        ensure_storage(self.api, self.cfg, self.path)
        self.assertFalse(any(name.startswith("create_") for name, _ in self.transport.calls))

    def test_uncertain_create_stops_before_duplicate(self):
        self.transport.fail = "create_access_point"
        with self.assertRaises(StorageError):
            ensure_storage(self.api, self.cfg, self.path)
        self.assertEqual(read_inventory(self.path)["pending"], "access_point")
        self.transport.fail = None
        self.transport.calls.clear()
        with self.assertRaisesRegex(StorageError, "UncertainCreate"):
            ensure_storage(self.api, self.cfg, self.path)
        self.assertFalse(any(name.startswith("create_") for name, _ in self.transport.calls))

    def test_wrong_team_volume_is_rejected(self):
        ensure_storage(self.api, self.cfg, self.path)
        self.transport.data["volume"]["teamID"] = "other-team"
        self.transport.calls.clear()
        with self.assertRaises(StorageError):
            ensure_storage(self.api, self.cfg, self.path)
        self.assertFalse(any(name.startswith("create_") for name, _ in self.transport.calls))

    def test_sdk_error_is_redacted(self):
        self.transport.fail = "create_file_system"
        with self.assertRaises(StorageError) as caught:
            ensure_storage(self.api, self.cfg, self.path)
        self.assertNotIn("synthetic-secret", str(caught.exception))
