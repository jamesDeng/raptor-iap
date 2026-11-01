"""AX-only Terraform plan/apply; never publish raw state, values or provider logs."""

import base64
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

ROOT = Path(__file__).resolve().parents[2]
STACK = ROOT / "infra-terraform/environments/rdev.ali/ecs-ax"
INSTANCE = "i-t4nj9k37b82g5a64x424"
ADDRESS = "module.node.alicloud_instance.node"
SIZES = {"ecs.g7.xlarge", "ecs.u2i-c1m8.large"}
BUCKET = "raptor-iap-tfstate-sg-200743"
LOCK_ENDPOINT = "https://raptor-tf-lock.ap-southeast-1.ots.aliyuncs.com"
BINDING = b"raptor-ecs-ax-v1:"


def require_sha(value):
    if not re.fullmatch(r"[0-9a-f]{40}", value):
        raise ValueError("Invalid source revision")
    return value


def check_inputs(value):
    if not isinstance(value, dict) or set(value) != {"ssh_public_key", "admin_cidr"}:
        raise ValueError("Invalid AX input fields")
    if not isinstance(value["ssh_public_key"], str) or not re.fullmatch(
        r"ssh-(?:ed25519|rsa) [A-Za-z0-9+/=]{40,1000}(?: [A-Za-z0-9_.@-]{1,100})?",
        value["ssh_public_key"],
    ):
        raise ValueError("Invalid AX public key")
    network = ipaddress.ip_network(value["admin_cidr"], strict=True)
    if network.version != 4 or network.prefixlen != 32:
        raise ValueError("AX administrator must be one IPv4 address")


def check_plan(plan):
    if plan.get("complete") is not True or plan.get("errored") or plan.get("deferred_changes"):
        raise ValueError("Incomplete AX plan")
    changes = [
        row for row in plan.get("resource_changes", [])
        if row.get("mode") == "managed" and row.get("change", {}).get("actions") != ["no-op"]
    ]
    if len(changes) > 1:
        raise ValueError("AX plan changes more than one resource")
    if not changes:
        return False
    row = changes[0]
    action = row.get("change", {})
    if row.get("address") != ADDRESS or action.get("actions") != ["update"]:
        raise ValueError("AX plan must update only the existing instance")
    before, after = action.get("before") or {}, action.get("after") or {}
    if before.get("id") != INSTANCE or after.get("id") != INSTANCE:
        raise ValueError("AX instance identity changed")
    if {before.get("instance_type"), after.get("instance_type")} != SIZES:
        raise ValueError("Unexpected AX instance type transition")
    for field in (
        "image_id", "instance_name", "host_name", "vswitch_id", "system_disk_size",
        "system_disk_category", "system_disk_encrypted", "deletion_protection",
        "instance_charge_type", "internet_charge_type", "internet_max_bandwidth_out",
        "key_name", "security_groups", "tags",
    ):
        if before.get(field) != after.get(field):
            raise ValueError("AX plan changes an unrelated instance field")
    if after.get("system_disk_size") != 120 or after.get("deletion_protection") is not True:
        raise ValueError("AX disk or deletion protection changed")
    return True


def comment(sha, changed):
    require_sha(sha)
    action = "Update existing AX instance size in place" if changed else "No resource changes"
    return (
        "<!-- raptor-ecs-ax-terraform-plan -->\n"
        f"Terraform AX plan for `{sha}`\n\n"
        "| Resource | Action |\n|---|---|\n"
        f"| `{ADDRESS}` | {action} |\n\n"
        "Values, credentials, raw plan, state and provider logs are withheld. "
        "A size change requires a reviewed maintenance window and a fresh account cost check.\n"
    )


def run(args, cwd):
    result = subprocess.run(args, cwd=cwd, capture_output=True)
    if result.returncode:
        raise RuntimeError(f"Private command failed: {args[0]} {args[1]}")
    return result.stdout


def key_bytes():
    key = base64.b64decode(os.environ["TF_PLAN_KEY"], validate=True)
    if len(key) != 32:
        raise ValueError("Invalid plan key")
    return key


def seal(plan, key, sha):
    nonce = os.urandom(12)
    return nonce + AESGCM(key).encrypt(nonce, plan, BINDING + require_sha(sha).encode())


def unseal(blob, key, sha):
    if len(blob) < 28:
        raise ValueError("Invalid encrypted plan")
    return AESGCM(key).decrypt(blob[:12], blob[12:], BINDING + require_sha(sha).encode())


def main():
    mode = sys.argv[1]
    if mode not in {"plan", "apply"}:
        raise ValueError("Invalid mode")
    sha = require_sha(os.environ["TF_SOURCE_SHA"])
    if run(["git", "rev-parse", "HEAD"], ROOT).decode().strip() != sha:
        raise ValueError("Checkout does not match source revision")
    if mode == "apply" and (
        os.environ.get("GITHUB_REF") != "refs/heads/main"
        or os.environ.get("TF_EXPECTED_SHA") != sha
    ):
        raise ValueError("Apply requires the reviewed main revision")
    inputs = json.loads(os.environ["TF_STACK_INPUTS"])
    check_inputs(inputs)
    variable = STACK / "gitops.auto.tfvars.json"
    created = False
    try:
        descriptor = os.open(variable, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        created = True
        with os.fdopen(descriptor, "w") as output:
            json.dump(inputs, output)
        run([
            "terraform", "init", "-input=false", "-lockfile=readonly",
            f"-backend-config=bucket={BUCKET}",
            "-backend-config=region=ap-southeast-1",
            "-backend-config=prefix=ecs-ax.ali",
            "-backend-config=key=terraform.tfstate",
            f"-backend-config=tablestore_endpoint={LOCK_ENDPOINT}",
            "-backend-config=tablestore_table=terraform_lock",
        ], STACK)
        run(["terraform", "validate"], STACK)
        with tempfile.TemporaryDirectory() as directory:
            plan_path = Path(directory) / "private.tfplan"
            if mode == "plan":
                run([
                    "terraform", "plan", "-input=false", "-lock-timeout=5m",
                    f"-var-file={variable}", f"-out={plan_path}",
                ], STACK)
                changed = check_plan(json.loads(run(["terraform", "show", "-json", str(plan_path)], STACK)))
                Path(os.environ["TF_SUMMARY_PATH"]).write_text(comment(sha, changed))
                if os.environ.get("TF_ENCRYPTED_PLAN_PATH"):
                    Path(os.environ["TF_ENCRYPTED_PLAN_PATH"]).write_bytes(
                        seal(plan_path.read_bytes(), key_bytes(), sha)
                    )
                if os.environ.get("GITHUB_OUTPUT"):
                    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
                        output.write(f"changed={'true' if changed else 'false'}\n")
            else:
                blob = Path(os.environ["TF_ENCRYPTED_PLAN_PATH"]).read_bytes()
                plan_path.write_bytes(unseal(blob, key_bytes(), sha))
                plan_path.chmod(0o600)
                check_plan(json.loads(run(["terraform", "show", "-json", str(plan_path)], STACK)))
                run(["terraform", "apply", "-input=false", str(plan_path)], STACK)
                print("Reviewed AX plan applied.")
    finally:
        if created:
            variable.unlink(missing_ok=True)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print("AX Terraform execution failed; private output withheld.", file=sys.stderr)
        sys.exit(1)
