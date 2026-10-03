"""Non-mutating by default. Live apply requires a separate reviewed gate record."""
import argparse
from datetime import datetime, timezone
import json
import math
from pathlib import Path

from .api import StorageAPI, StorageError
from .config import load_config, read_inventory
from .lifecycle import bind_inventory, build_plan, cleanup_empty_storage, cleanup_plan, ensure_storage, verify_owned


def check_apply_gate(path, config):
    if path is None:
        raise StorageError("MissingProvisioningEvidence")
    gate = json.loads(path.read_text())
    now = datetime.now(timezone.utc)
    try:
        age = (now - datetime.fromisoformat(gate["checked_at"])).total_seconds()
        values = [gate[k] for k in ("hourly_price_rmb_per_gib", "storage_budget_rmb", "remaining_budget_rmb", "max_hours")]
        if any(type(v) not in (int, float) or not math.isfinite(v) or v <= 0 for v in values):
            raise ValueError()
        price, budget, remaining, hours = values
        source = gate["price_source"]
        if (not 0 <= age <= 86400 or gate["configuration_fingerprint"] != config.fingerprint or
                gate["network_reviewed"] is not True or
                not source.startswith(("https://www.aliyun.com/", "https://help.aliyun.com/")) or
                not 10 * price * hours <= budget <= remaining <= 1000 or hours > 24):
            raise ValueError()
    except (KeyError, TypeError, ValueError, AttributeError):
        raise StorageError("InvalidProvisioningEvidence") from None


def summary(state):
    return {key: state[key] for key in ("phase", "filesystem_id", "agentic_space_id", "access_point_id", "volume_id") if key in state}


def main(argv=None):
    parser = argparse.ArgumentParser(description="Plan, inspect and bootstrap dedicated AgenticFS storage.")
    parser.add_argument("command", nargs="?", default="plan", choices=("plan", "inspect", "apply", "cleanup-plan", "cleanup"))
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--inventory", type=Path, default=Path(".raptor-local/agenticfs/inventory.json"))
    parser.add_argument("--gate", type=Path, help="Reviewed nonsecret price/budget/network evidence for apply")
    parser.add_argument("--allow-empty-delete", action="store_true")
    args = parser.parse_args(argv)
    try:
        cfg = load_config(args.config)
        if args.command == "plan":
            result = build_plan(cfg, read_inventory(args.inventory))
        else:
            if args.command == "apply":
                check_apply_gate(args.gate, cfg)
            api = StorageAPI.from_credentials(cfg)
            if args.command == "apply":
                result = summary(ensure_storage(api, cfg, args.inventory))
            elif args.command == "cleanup":
                result = summary(cleanup_empty_storage(api, args.inventory, [], args.allow_empty_delete))
            else:
                state = read_inventory(args.inventory)
                if args.command == "cleanup-plan":
                    result = cleanup_plan(api, state, [])
                else:
                    bind_inventory(api, cfg, state)
                    api.assert_identity()
                    verify_owned(api, state)
                    result = summary(state)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (StorageError, ValueError, OSError, ImportError) as error:
        public = str(error) if isinstance(error, StorageError) else "LocalValidationOrDependencyError"
        print(json.dumps({"error": public}))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
