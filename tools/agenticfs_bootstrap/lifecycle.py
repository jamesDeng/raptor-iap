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
        if state.get("phase") in ("deleting", "deleted"):
            raise StorageError("InventoryBeingDeleted")
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


def build_plan(config, inventory):
    if "fingerprint" in inventory and inventory["fingerprint"] != config.fingerprint:
        raise StorageError("InventoryConfigMismatch")
    return {"mode": "plan", "region": config.region, "configuration_fingerprint": config.fingerprint,
            "sdk_owned": ["AgenticFS", "AgenticSpace", "AccessPoint", "SandboxVolume"],
            "terraform_owned": ["VPC", "vSwitch", "security_group", "execution_role"],
            "quota_bytes": 10737418240, "quota_file_count": 10000,
            "retention": "retain_until_explicit_empty_cleanup", "cloud_verified": False}


def cleanup_plan(api, inventory, active_sandbox_ids):
    bind_inventory(api, api.cfg, inventory)
    api.assert_identity()
    consumers = api.active_consumers()
    if active_sandbox_ids or consumers:
        raise StorageError("ActiveConsumers")
    if inventory.get("pending"):
        raise StorageError("UncertainOperation")
    verify_owned(api, inventory)
    api.assert_no_foreign_children(inventory)
    if inventory.get("agentic_space_id") and not api.space_is_empty(inventory["filesystem_id"], inventory["agentic_space_id"]):
        raise StorageError("StoredDataPresent")
    return {"mode": "cleanup-plan", "order": [key for key in (
        "volume_id", "access_point_id", "agentic_space_id", "filesystem_id") if inventory.get(key)],
        "data_deletion": False}


def cleanup_empty_storage(api, inventory_path, active_sandbox_ids, allow_empty_delete):
    if not allow_empty_delete:
        raise StorageError("CleanupNotSelected")
    with inventory_lock(inventory_path):
        state = read_inventory(inventory_path)
        if state.get("pending", "").startswith("delete-"):
            bind_inventory(api, api.cfg, state)
            api.assert_identity()
            if active_sandbox_ids or api.active_consumers():
                raise StorageError("ActiveConsumers")
            key = state["pending"][7:]
            getters = {"volume_id": lambda: api.get_volume(state[key]),
                       "access_point_id": lambda: api.get_access_point(state[key]),
                       "agentic_space_id": lambda: api.get_space(state["filesystem_id"], state[key]),
                       "filesystem_id": lambda: api.get_filesystem(state[key])}
            if state.get("phase") != "deleting" or key not in getters or not state.get(key):
                raise StorageError("InvalidPendingStage")
            recovered = dict(state)
            try:
                getters[key]()
            except StorageError as error:
                if str(error) != "ResourceNotFound":
                    raise
                recovered.pop(key)
            recovered.pop("pending")
            cleanup_plan(api, recovered, active_sandbox_ids)
            save_inventory(inventory_path, recovered)
            state = recovered
        cleanup_plan(api, state, active_sandbox_ids)
        steps = [
            ("volume_id", lambda id: api.delete_volume(id), lambda id: api.get_volume(id)),
            ("access_point_id", lambda id: api.delete_access_point(id), lambda id: api.get_access_point(id)),
            ("agentic_space_id", lambda id: api.delete_space(state["filesystem_id"], id), lambda id: api.get_space(state["filesystem_id"], id)),
            ("filesystem_id", lambda id: api.delete_filesystem(id), lambda id: api.get_filesystem(id)),
        ]
        for key, delete, get in steps:
            if not state.get(key):
                continue
            if api.active_consumers():
                raise StorageError("ActiveConsumers")
            api.assert_no_foreign_children(state)
            if state.get("agentic_space_id") and not api.space_is_empty(state["filesystem_id"], state["agentic_space_id"]):
                raise StorageError("StoredDataPresent")
            state["phase"], state["pending"] = "deleting", "delete-" + key
            save_inventory(inventory_path, state)
            id = state[key]
            delete(id)
            api.wait_deleted(lambda: get(id))
            state.pop(key)
            state.pop("pending")
            save_inventory(inventory_path, state)
        state["phase"] = "deleted"
        save_inventory(inventory_path, state)
        return state
