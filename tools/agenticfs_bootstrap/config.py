"""Strict nonsecret inputs and crash-safe local resource inventory."""
from dataclasses import asdict, dataclass
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile


@dataclass(frozen=True)
class BootstrapConfig:
    account_id: str
    region: str
    zone: str
    team_id: str
    run_id: str
    runtime_uid: int
    runtime_gid: int
    vpc_id: str
    vswitch_id: str
    security_group_id: str
    execution_role_arn: str

    @property
    def fingerprint(self):
        return hashlib.sha256(json.dumps(asdict(self), sort_keys=True).encode()).hexdigest()


INVENTORY_FIELDS = {
    "schema_version", "fingerprint", "run_id", "phase", "client_tokens",
    "filesystem_id", "agentic_space_id", "access_point_id", "access_point_domain",
    "volume_id", "volume_name", "pending", "account_id", "region", "team_id",
    "owner_nonce",
}
Inventory = dict


def load_config(path: Path) -> BootstrapConfig:
    try:
        raw = json.loads(path.read_text())
        fields = set(BootstrapConfig.__dataclass_fields__)
        network = {"vpc_id", "vswitch_id", "security_group_id", "execution_role_arn"}
        if set(raw) != (fields - network) | {"terraform_outputs"}:
            raise ValueError()
        outputs = raw.pop("terraform_outputs")
        raw.update({key: outputs[key]["value"] for key in network})
        cfg = BootstrapConfig(**raw)
        if cfg.region != "ap-southeast-1" or cfg.zone != "ap-southeast-1a":
            raise ValueError()
        for key in fields - {"runtime_uid", "runtime_gid"}:
            value = getattr(cfg, key)
            if not isinstance(value, str) or not re.fullmatch(r"[a-zA-Z0-9:_./-]+", value):
                raise ValueError()
        if not re.fullmatch(r"[0-9]{12,20}", cfg.account_id):
            raise ValueError()
        if not re.fullmatch(r"[a-z][a-z0-9-]{0,31}", cfg.run_id):
            raise ValueError()
        if not cfg.execution_role_arn.startswith("acs:ram::" + cfg.account_id + ":role/"):
            raise ValueError()
        for key in ("runtime_uid", "runtime_gid"):
            value = getattr(cfg, key)
            if type(value) is not int or not 0 <= value <= 2**31 - 1:
                raise ValueError()
        for key, prefix in (("vpc_id", "vpc-"), ("vswitch_id", "vsw-"), ("security_group_id", "sg-")):
            if not getattr(cfg, key).startswith(prefix):
                raise ValueError()
        return cfg
    except (KeyError, TypeError, ValueError, AttributeError) as exc:
        raise ValueError("InvalidConfig") from None


def validate_inventory(value):
    if not isinstance(value, dict) or set(value) - INVENTORY_FIELDS or value.get("schema_version") != 1:
        raise ValueError("InvalidInventory")
    for key, field in value.items():
        if key == "schema_version":
            continue
        if key == "client_tokens":
            if not isinstance(field, dict) or set(field) - {"filesystem", "space"}:
                raise ValueError("InvalidInventory")
            values = field.values()
        else:
            values = [field]
        if any(not isinstance(item, str) or not re.fullmatch(r"[a-zA-Z0-9:_./-]{0,256}", item) for item in values):
            raise ValueError("InvalidInventory")
    return value


def read_inventory(path: Path) -> Inventory:
    if not path.exists():
        return {"schema_version": 1}
    if path.is_symlink():
        raise ValueError("UnsafeInventoryPath")
    try:
        return validate_inventory(json.loads(path.read_text()))
    except (ValueError, TypeError):
        raise ValueError("InvalidInventory") from None


def save_inventory(path: Path, inventory: Inventory) -> None:
    validate_inventory(inventory)
    if path.is_symlink() or path.parent.is_symlink():
        raise ValueError("UnsafeInventoryPath")
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(path.parent, 0o700)
    fd, name = tempfile.mkstemp(prefix=".inventory-", dir=str(path.parent))
    try:
        with os.fdopen(fd, "w") as stream:
            os.fchmod(stream.fileno(), 0o600)
            json.dump(inventory, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(name, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(name):
            os.unlink(name)
