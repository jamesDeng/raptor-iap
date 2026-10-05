"""Fail-closed offline check of verified deployment evidence; creates no resources."""
import argparse
import json
import math
from pathlib import Path


def validate(evidence):
    errors = []
    if evidence.get("region") != "ap-southeast-1":
        errors.append("Region must be Singapore")
    for name, minimum in [("worker_cores", 4), ("worker_memory_gb", 8)]:
        value = evidence.get(name)
        if not isinstance(value, (int, float)) or isinstance(value, bool) or not math.isfinite(value) or value < minimum:
            errors.append(name + " is below the ACK minimum")
    if evidence.get("worker_count") != 1 or evidence.get("hours") != 72:
        errors.append("Scope must be one worker for 72 hours")
    for name in ["worker_available", "prices_complete", "source_integrated", "credentials_valid"]:
        if evidence.get(name) is not True:
            errors.append(name + " is not verified")
    cost = evidence.get("estimated_total_cny")
    if evidence.get("budget_cny") != 150 or not isinstance(cost, (int, float)) or isinstance(cost, bool) or not math.isfinite(cost) or cost < 0 or cost > 150:
        errors.append("Complete forecast must fit RMB150")
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("evidence", type=Path)
    args = parser.parse_args()
    try:
        evidence = json.loads(args.evidence.read_text())
        if not isinstance(evidence, dict):
            raise ValueError()
        errors = validate(evidence)
    except (OSError, ValueError):
        errors = ["Evidence is missing or invalid"]
    print(json.dumps({"passed": not errors, "errors": errors}))
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
