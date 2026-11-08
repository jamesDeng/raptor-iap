import hashlib
import json
import pathlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

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

    def test_provenance_requires_an_exact_merged_main_pr(self):
        commit = "a" * 40
        pull = {
            "merge_commit_sha": commit,
            "merged_at": "2026-11-08T06:09:03Z",
            "base": {"ref": "main", "repo": {"full_name": infra_release.REPO}},
        }
        self.assertTrue(infra_release.reviewed_merge(commit, [pull]))
        self.assertFalse(infra_release.reviewed_merge("b" * 40, [pull]))
        self.assertFalse(infra_release.reviewed_merge(commit, [dict(pull, merged_at=None)]))
        self.assertFalse(infra_release.reviewed_merge(commit, [dict(pull, base=dict(pull["base"], ref="other"))]))

    def test_superseded_release_is_rejected_before_review_lookup(self):
        commit, latest = "a" * 40, "b" * 40
        changed = subprocess.CalledProcessError(1, "git diff --quiet")
        with patch.object(infra_release, "git", side_effect=["", latest, "", changed]) as git:
            with self.assertRaisesRegex(ValueError, "no longer matches current main"):
                infra_release.verify_review({"sourceCommit": commit}, commit)
        git.assert_called_with("diff", "--quiet", commit, latest, "--", *infra_release.CONTROL_FILES)


if __name__ == "__main__":
    unittest.main()
