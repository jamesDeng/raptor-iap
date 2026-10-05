"""Initial provisioning gate; one saved-plan apply, no automatic retries."""
from datetime import datetime, timezone
import math
import re
from preflight import validate


def validate_release(evidence, source_sha, expected_sha, now=None):
    now = now or datetime.now(timezone.utc)
    try:
        if not isinstance(evidence, dict) or validate(evidence):
            raise ValueError()
        if not re.fullmatch(r'[0-9a-f]{40}', source_sha) or source_sha != expected_sha:
            raise ValueError()
        checked = datetime.fromisoformat(evidence['checked_at'])
        age = (now - checked).total_seconds()
        if checked.tzinfo is None or not 0 <= age <= 21600:
            raise ValueError()
        for key in ('available_balance_cny', 'reported_pretax_cny', 'uncertainty_reserve_cny'):
            value = evidence[key]
            if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value < 0:
                raise ValueError()
        if evidence['inventory_complete'] is not True:
            if evidence['bounded_cost_owner_approved'] is not True or evidence['uncertainty_reserve_cny'] < 2:
                raise ValueError()
        total = evidence['estimated_total_cny']
        if total < 144.76 + evidence['uncertainty_reserve_cny'] or evidence['available_balance_cny'] < total:
            raise ValueError()
        if evidence['whole_poc_budget_cny'] != 1000 or evidence['reported_pretax_cny'] + total >= 1000:
            raise ValueError()
    except (KeyError, TypeError, ValueError):
        raise ValueError('Initial apply requires current, approved budget evidence and exact reviewed source') from None


def apply_saved_plan(run, evidence, source_sha, expected_sha, now=None):
    validate_release(evidence, source_sha, expected_sha, now)
    run(['apply', '-input=false', '-lock-timeout=60s', 'foundation.tfplan'], timeout=2400)


if __name__ == '__main__':
    import subprocess
    from live_plan import main
    try:
        raise SystemExit(main(provision=True))
    except (ValueError, OSError, subprocess.SubprocessError):
        print('Initial provisioning stopped. State may contain partial resources; reconcile before any retry. Raw diagnostics withheld.')
        raise SystemExit(1)
