"""Boundary tests: malformed inputs, crash safety and lifecycle side effects."""
import json
import contextlib
import io
import os
from pathlib import Path
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from tools.agenticfs_bootstrap.config import load_config, read_inventory, save_inventory
from tools.agenticfs_bootstrap.api import StorageAPI, StorageError
from tools.agenticfs_bootstrap.lifecycle import ensure_storage, cleanup_empty_storage
from tools.agenticfs_bootstrap.__main__ import main


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


class FileCase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def write_config(self, data):
        path = self.root / "input.json"
        path.write_text(json.dumps(data))
        return path


class ConfigTests(FileCase):

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


class CloudNotFound(Exception):
    def __init__(self, code):
        self.code = code


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
                if "space" not in self.data:
                    raise CloudNotFound("InvalidAgenticSpace.NotFound")
                result = {"AgenticSpace": self.data["space"]}
            elif name == "describe_agentic_spaces":
                result = {"AgenticSpaces": {"AgenticSpace": [self.data["space"]] if "space" in self.data else []}}
            elif name == "create_access_point":
                self.data["ap"] = dict(request, Tags=request["Tag"], VSwitchId=request["VswId"], AccessPointId="ap-test", DomainName="ap-test.fs-test-w.ap-southeast-1.nas.aliyuncs.com", Status="active")
                result = {"AccessPoint": {"AccessPointId": "ap-test", "AccessPointDomain": self.data["ap"]["DomainName"]}}
            elif name == "describe_access_point":
                if "ap" not in self.data:
                    raise CloudNotFound("InvalidAccessPoint.NotFound")
                result = {"AccessPoint": self.data["ap"]}
            elif name == "describe_access_points":
                result = {"AccessPoints": [self.data["ap"]] if "ap" in self.data else []}
            elif name == "create_volume":
                self.data["volume"] = dict(request["body"], volumeID="vol-test", status="active")
                result = {"volume": self.data["volume"]}
            elif name == "get_volume":
                if "volume" not in self.data:
                    raise CloudNotFound("VolumeNotFound")
                result = {"volume": self.data["volume"]}
            elif name == "list_volumes":
                result = {"volumes": [self.data["volume"]] if "volume" in self.data else []}
            elif name.startswith("delete_"):
                key = {"delete_volume": "volume", "delete_access_point": "ap",
                       "delete_agentic_space": "space", "delete_file_system": "fs"}[name]
                self.data.pop(key, None)
                result = {}
            else:
                raise AssertionError(name)
            return SimpleNamespace(body=SimpleNamespace(to_map=lambda: result))
        return call


class APICase(FileCase):
    def setUp(self):
        super().setUp()
        self.cfg = load_config(self.write_config(fixture()))
        self.transport = FakeTransport()
        self.api = StorageAPI(self.cfg, self.transport, self.transport, self.transport)
        self.path = self.root / "state" / "inventory.json"


class LifecycleTests(APICase):

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


class CleanupTests(APICase):
    def prepare(self):
        ensure_storage(self.api, self.cfg, self.path)
        self.api.consumer_checker = lambda: []
        self.transport.calls.clear()

    def test_default_command_does_not_mutate(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output), patch.object(StorageAPI, "from_credentials") as construct:
            code = main(["--config", str(self.root / "input.json"), "--inventory", str(self.path)])
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(output.getvalue())["mode"], "plan")
        construct.assert_not_called()
        self.assertFalse(self.path.exists())

    def test_apply_without_price_evidence_stops(self):
        with contextlib.redirect_stdout(io.StringIO()), patch.object(StorageAPI, "from_credentials") as construct:
            code = main(["apply", "--config", str(self.root / "input.json"), "--inventory", str(self.path)])
        self.assertEqual(code, 2)
        construct.assert_not_called()
        self.assertFalse(self.path.exists())

    def test_cleanup_refuses_active_consumer_or_nonempty_space(self):
        self.prepare()
        self.api.consumer_checker = lambda: ["sbx-active"]
        with self.assertRaises(StorageError):
            cleanup_empty_storage(self.api, self.path, [], True)
        self.assertFalse(any(x[0].startswith("delete_") for x in self.transport.calls))
        self.api.consumer_checker = lambda: []
        self.transport.data["space"]["FileCountUsage"] = 1
        with self.assertRaises(StorageError):
            cleanup_empty_storage(self.api, self.path, [], True)
        self.assertFalse(any(x[0].startswith("delete_") for x in self.transport.calls))

    def test_child_delete_failure_preserves_parent(self):
        self.prepare()
        self.transport.fail = "delete_volume"
        with self.assertRaises(StorageError):
            cleanup_empty_storage(self.api, self.path, [], True)
        self.assertEqual([x[0] for x in self.transport.calls if x[0].startswith("delete_")], ["delete_volume"])
        self.assertEqual(read_inventory(self.path)["filesystem_id"], "fs-test")

    def test_cleanup_rejects_foreign_inventory(self):
        self.prepare()
        state = read_inventory(self.path)
        state["account_id"] = "9876543210987654"
        save_inventory(self.path, state)
        with self.assertRaises(StorageError):
            cleanup_empty_storage(self.api, self.path, [], True)
        self.assertFalse(any(x[0].startswith("delete_") for x in self.transport.calls))

    def test_empty_cleanup_deletes_in_reverse_dependency_order(self):
        self.prepare()
        state = cleanup_empty_storage(self.api, self.path, [], True)
        self.assertEqual([x[0] for x in self.transport.calls if x[0].startswith("delete_")],
                         ["delete_volume", "delete_access_point", "delete_agentic_space", "delete_file_system"])
        self.assertEqual(state["phase"], "deleted")
        self.assertNotIn("filesystem_id", read_inventory(self.path))

    def test_unknown_occupancy_blocks_cleanup(self):
        self.prepare()
        self.transport.data["space"].pop("SpaceUsage")
        with self.assertRaises(StorageError):
            cleanup_empty_storage(self.api, self.path, [], True)
        self.assertFalse(any(x[0].startswith("delete_") for x in self.transport.calls))

    def test_failed_cleanup_can_resume_without_deleting_parent_early(self):
        self.prepare()
        self.transport.fail = "delete_access_point"
        with self.assertRaises(StorageError):
            cleanup_empty_storage(self.api, self.path, [], True)
        self.assertNotIn("volume_id", read_inventory(self.path))
        self.assertIn("access_point_id", read_inventory(self.path))
        self.transport.fail = None
        result = cleanup_empty_storage(self.api, self.path, [], True)
        self.assertEqual(result["phase"], "deleted")

    def test_valid_apply_gate_allows_sdk_setup(self):
        from datetime import datetime, timezone
        from tools.agenticfs_bootstrap.__main__ import check_apply_gate
        gate = self.root / "gate.json"
        gate.write_text(json.dumps({
            "checked_at": datetime.now(timezone.utc).isoformat(),
            "configuration_fingerprint": self.cfg.fingerprint, "network_reviewed": True,
            "hourly_price_rmb_per_gib": 0.01, "storage_budget_rmb": 5,
            "remaining_budget_rmb": 100, "max_hours": 24,
            "price_source": "https://www.aliyun.com/price"}))
        check_apply_gate(gate, self.cfg)
        with patch.object(StorageAPI, "from_credentials", return_value=self.api), contextlib.redirect_stdout(io.StringIO()):
            code = main(["apply", "--config", str(self.root / "input.json"),
                         "--inventory", str(self.path), "--gate", str(gate)])
        self.assertEqual(code, 0)
        self.assertEqual(read_inventory(self.path)["phase"], "ready")


if __name__ == "__main__":
    unittest.main()
