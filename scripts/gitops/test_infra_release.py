import hashlib
import json
import pathlib
import tempfile
import unittest

import yaml

import infra_release


class InfraRelease(unittest.TestCase):
    def test_pin_is_exact(self):
        good = {"sourceCommit": "a" * 40, "previousChecksum": "17069153212829515407"}
        self.assertEqual(infra_release.validate_pin(good), good)
        for bad in (dict(good, function="other"), dict(good, sourceCommit="main"), dict(good, previousChecksum="0")):
            with self.assertRaises(ValueError):
                infra_release.validate_pin(bad)

    def test_cloud_release_runs_after_the_reviewed_apply(self):
        workflow = infra_release.ROOT / ".github/workflows/terraform-infra-api-gitops.yml"
        jobs = yaml.load(workflow.read_text(), Loader=yaml.BaseLoader)["jobs"]
        self.assertNotIn("id-token", jobs["check"].get("permissions", {}))
        apply = jobs["apply"]
        self.assertEqual(apply["environment"], "rdev.ali-apply")
        self.assertIn("refs/heads/main", apply["if"])
        commands = [step.get("run", "") for step in apply["steps"]]
        self.assertLess(next(i for i, cmd in enumerate(commands) if "runner.py apply" in cmd),
                        next(i for i, cmd in enumerate(commands) if "infra_release.py deploy" in cmd))
        self.assertTrue(any('gh run download "$RUN_ID" --name reviewed-infra-api-release' in cmd for cmd in commands))

    def test_release_artifact_binds_both_files_and_the_reviewed_commit(self):
        with tempfile.TemporaryDirectory() as folder:
            root = pathlib.Path(folder)
            pin = {"sourceCommit": "a" * 40, "previousChecksum": "17069153212829515407"}
            payload = {"function.zip": b"reviewed package", "fc-release": b"reviewed release program"}
            for name, contents in payload.items():
                (root / name).write_bytes(contents)
            digests = {name: hashlib.sha256(contents).hexdigest() for name, contents in payload.items()}
            receipt = {"controlCommit": "b" * 40, "pin": pin, "sha256": digests}
            (root / "receipt.json").write_text(json.dumps(receipt))
            self.assertEqual(infra_release.verify_bundle(root, pin, "b" * 40), digests)
            with self.assertRaises(ValueError):
                infra_release.verify_bundle(root, pin, "c" * 40)
            (root / "fc-release").write_bytes(b"different program")
            with self.assertRaises(ValueError):
                infra_release.verify_bundle(root, pin, "b" * 40)


if __name__ == "__main__":
    unittest.main()
