"""Serialized SDK-owned resource lifecycle; no automatic rollback destroys data."""
from contextlib import contextmanager
import fcntl
import os
import uuid

from .api import StorageError
from .config import read_inventory, save_inventory


@contextmanager
def inventory_lock(path):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    lock_path = path.with_suffix(".lock")
    fd = os.open(lock_path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise StorageError("BootstrapAlreadyRunning") from None
        yield
    finally:
        os.close(fd)


def bind_inventory(api, config, state):
    if state.get("fingerprint") != config.fingerprint:
        raise StorageError("InventoryConfigMismatch")
    if (state.get("account_id"), state.get("region"), state.get("team_id")) != (
            config.account_id, config.region, config.team_id):
        raise StorageError("InventoryOwnerMismatch")
    nonce = state.get("owner_nonce")
    if not nonce or len(nonce) != 32 or any(c not in "0123456789abcdef" for c in nonce):
        raise StorageError("InvalidOwnershipNonce")
    api.owner_nonce = nonce
    api.filesystem_id = state.get("filesystem_id")


def verify_owned(api, state):
    """Verify the chain before reuse or deletion; fail closed on incomplete data."""
    if state.get("filesystem_id"):
        fs = api.wait_ready(lambda: api.get_filesystem(state["filesystem_id"]), "Running")
        if not api.owns_filesystem(fs) or fs.get("RegionId") != api.cfg.region:
            raise StorageError("ResourceOwnershipMismatch")
    if state.get("agentic_space_id"):
        space = api.wait_ready(lambda: api.get_space(state["filesystem_id"], state["agentic_space_id"]), "Running")
        if (space.get("AgenticSpaceId") != state["agentic_space_id"] or
                space.get("FileSystemId") != state["filesystem_id"] or
                space.get("Description") != "raptor-" + api.owner_nonce or
                space.get("Azone") != api.cfg.zone or
                space.get("FileSystemPath", "").rstrip("/") != "/raptor-" + api.cfg.run_id):
            raise StorageError("ResourceOwnershipMismatch")
    if state.get("access_point_id"):
        ap = api.wait_ready(lambda: api.get_access_point(state["access_point_id"]), "active")
        if (ap.get("AgenticSpaceId") != state["agentic_space_id"] or
                ap.get("FileSystemId") != state["filesystem_id"] or
                ap.get("AccessPointId") != state["access_point_id"] or
                ap.get("EnabledRam") is not True or not api.has_owner_tag(ap) or
                ap.get("VpcId") != api.cfg.vpc_id):
            raise StorageError("ResourceOwnershipMismatch")
        if api.valid_domain(ap.get("DomainName")) != state["access_point_domain"]:
            raise StorageError("ResourceOwnershipMismatch")
    if state.get("volume_id"):
        volume = api.get_volume(state["volume_id"])
        cfg = volume.get("agenticFSVolumeConfig", {})
        if (volume.get("teamID") != api.cfg.team_id or volume.get("volumeID") != state["volume_id"]
                or volume.get("volumeName") != state["volume_name"] or
                cfg != {"serverAddr": state["access_point_domain"] + ":/",
                        "userID": api.cfg.runtime_uid, "groupID": api.cfg.runtime_gid}):
            raise StorageError("ResourceOwnershipMismatch")


def reconcile_pending(api, config, inventory):
    stage = inventory.get("pending")
    if not stage:
        return inventory
    if stage not in ("filesystem", "space", "access_point", "volume"):
        raise StorageError("InvalidPendingStage")
    matches = api.find_pending(stage, inventory)
    if len(matches) != 1:
        raise StorageError("UncertainCreate")
    row = matches[0]
    key, remote = {"filesystem": ("filesystem_id", "FileSystemId"),
                   "space": ("agentic_space_id", "AgenticSpaceId"),
                   "access_point": ("access_point_id", "AccessPointId"),
                   "volume": ("volume_id", "volumeID")}[stage]
    inventory[key] = api._id(row.get(remote))
    if stage == "access_point":
        inventory["access_point_domain"] = api.valid_domain(row.get("DomainName"))
    inventory.pop("pending", None)
    return inventory


def ensure_storage(api, config, inventory_path):
    with inventory_lock(inventory_path):
        api.assert_identity()
        state = read_inventory(inventory_path)
        if "fingerprint" not in state:
            if len(state) != 1:
                raise StorageError("UnownedInventory")
            nonce = uuid.uuid4().hex
            state.update(fingerprint=config.fingerprint, run_id=config.run_id,
                         account_id=config.account_id, region=config.region, team_id=config.team_id,
                         owner_nonce=nonce, phase="planned", client_tokens={
                             "filesystem": uuid.uuid4().hex, "space": uuid.uuid4().hex},
                         volume_name="raptor-" + config.run_id + "-" + nonce[:12])
            save_inventory(inventory_path, state)
        bind_inventory(api, config, state)
        verify_owned(api, state)
        if state.get("pending"):
            reconcile_pending(api, config, state)
            save_inventory(inventory_path, state)
            verify_owned(api, state)
        steps = [
            ("filesystem", "filesystem_id", lambda: api.create_filesystem(state["client_tokens"]["filesystem"])),
            ("space", "agentic_space_id", lambda: api.create_space(state["filesystem_id"], state["client_tokens"]["space"])),
            ("access_point", "access_point_id", lambda: api.create_access_point(state["filesystem_id"], state["agentic_space_id"])),
            ("volume", "volume_id", lambda: api.create_volume(state["volume_name"], state["access_point_domain"] + ":/", config.runtime_uid, config.runtime_gid)),
        ]
        for stage, key, create in steps:
            if key in state:
                continue
            state["pending"] = stage
            save_inventory(inventory_path, state)
            result = create()
            if stage == "access_point":
                state[key], state["access_point_domain"] = result
            else:
                state[key] = result
            state.pop("pending")
            state["phase"] = stage
            save_inventory(inventory_path, state)
            verify_owned(api, state)
        state["phase"] = "ready"
        save_inventory(inventory_path, state)
        return state
