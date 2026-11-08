"""Build and verify the reviewed rdev Infra API function package."""
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
REPO = "jamesDeng/raptor-iap"
PIN = "infra-terraform/environments/rdev-infra-api/function-release.json"
CONTROL_FILES = (
    PIN,
    ".github/workflows/terraform-infra-api-gitops.yml",
    "components/infra-api/release/infra_release.py",
    "components/infra-api/cmd/fc-release/main.go",
    "components/infra-api/cmd/fc-release/sdk.go",
    "components/infra-api/go.mod",
    "components/infra-api/go.sum",
)
FILES = ("function.zip", "fc-release")


def validate_pin(pin):
    if (not isinstance(pin, dict) or set(pin) != {"sourceCommit", "previousChecksum"}
            or not isinstance(pin["sourceCommit"], str) or not re.fullmatch(r"[0-9a-f]{40}", pin["sourceCommit"])
            or not isinstance(pin["previousChecksum"], str)
            or not re.fullmatch(r"[1-9][0-9]{0,19}", pin["previousChecksum"])
            or int(pin["previousChecksum"]) >= 2 ** 64):
        raise ValueError("Invalid function release pin")
    return pin


def sha256(path):
    if not path.is_file() or path.is_symlink():
        raise ValueError("Invalid release artifact")
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify_bundle(directory, pin, control_commit):
    receipt_path = directory / "receipt.json"
    if receipt_path.is_symlink() or receipt_path.stat().st_size > 4096:
        raise ValueError("Invalid release receipt")
    receipt = json.loads(receipt_path.read_text())
    digests = {name: sha256(directory / name) for name in FILES}
    if receipt != {"controlCommit": control_commit, "pin": pin, "sha256": digests}:
        raise ValueError("Release receipt does not match this reviewed commit and bundle")
    return digests


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def reviewed_merge(commit, pulls):
    return any(pull["merge_commit_sha"] == commit and pull["merged_at"]
               and pull["base"]["ref"] == "main" and pull["base"]["repo"]["full_name"] == REPO
               for pull in pulls)


def verify_review(pin, control_commit):
    git("fetch", "--no-tags", f"https://github.com/{REPO}.git", "refs/heads/main")
    latest = git("rev-parse", "FETCH_HEAD")
    try:
        git("merge-base", "--is-ancestor", control_commit, latest)
        git("diff", "--quiet", control_commit, latest, "--", *CONTROL_FILES)
    except subprocess.CalledProcessError as exc:
        raise ValueError("Infra API release no longer matches current main") from exc
    merges = {control_commit, pin["sourceCommit"]}
    for path in CONTROL_FILES:
        merges.add(git("log", "--first-parent", "-1", "--format=%H", control_commit, "--", path))
    for commit in sorted(merges):
        pulls = subprocess.check_output(["gh", "api", f"repos/{REPO}/commits/{commit}/pulls"], cwd=ROOT, text=True)
        if not reviewed_merge(commit, json.loads(pulls)):
            raise ValueError("Infra API release provenance requires an exact merged main PR")


def build(directory, pin, control_commit):
    source_commit = pin["sourceCommit"]
    subprocess.run(["git", "merge-base", "--is-ancestor", source_commit, "origin/main"], cwd=ROOT, check=True)
    directory.mkdir(parents=True)
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        archive = root / "source.tar"
        with archive.open("wb") as stream:
            subprocess.run(["git", "archive", source_commit, "components/infra-api"], cwd=ROOT, stdout=stream, check=True)
        subprocess.run(["tar", "-xf", str(archive), "-C", str(root)], check=True)
        env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="amd64", GOFLAGS="")
        server = root / "server"
        subprocess.run(["go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", str(server), "./cmd/server"],
                       cwd=root / "components/infra-api", env=env, check=True)
        entry = zipfile.ZipInfo("server")
        entry.create_system = 3
        entry.external_attr = 0o100755 << 16
        entry.compress_type = zipfile.ZIP_DEFLATED
        with zipfile.ZipFile(directory / "function.zip", "w") as package:
            with server.open("rb") as src, package.open(entry, "w") as dest:
                shutil.copyfileobj(src, dest)
        subprocess.run(["go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", str(directory / "fc-release"), "./cmd/fc-release"],
                       cwd=ROOT / "components/infra-api", env=env, check=True)
    digests = {name: sha256(directory / name) for name in FILES}
    (directory / "receipt.json").write_text(json.dumps({"controlCommit": control_commit, "pin": pin, "sha256": digests}))
    print(f"Infra API package from {source_commit}, SHA-256 {digests['function.zip']}")


def main():
    mode, path = sys.argv[1:]
    directory = Path(path).resolve()
    pin = validate_pin(json.loads((ROOT / PIN).read_text()))
    control_commit = git("rev-parse", "HEAD")
    if mode == "build":
        build(directory, pin, control_commit)
    elif mode == "deploy":
        if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("GITHUB_REF") != "refs/heads/main":
            raise ValueError("Function release requires protected main CI")
        verify_review(pin, control_commit)
        digests = verify_bundle(directory, pin, control_commit)
        executable = directory / "fc-release"
        executable.chmod(0o700)
        subprocess.run([str(executable), str(directory / "function.zip"), digests["function.zip"], pin["previousChecksum"]], check=True)
    else:
        raise ValueError("Invalid release mode")


if __name__ == "__main__":
    try:
        main()
    except ValueError as error:
        sys.exit(str(error))
    except (OSError, subprocess.CalledProcessError):
        sys.exit("Infra API release step failed.")
