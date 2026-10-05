from datetime import datetime, timedelta, timezone
import unittest
from unittest.mock import Mock
from initial_apply import validate_release, apply_saved_plan

NOW = datetime(2026, 10, 5, 14, 0, tzinfo=timezone.utc)
SHA = 'a' * 40


def evidence():
    return {'checked_at': NOW.isoformat(), 'region': 'ap-southeast-1', 'worker_cores': 4, 'worker_memory_gb': 8, 'worker_count': 1, 'hours': 72, 'worker_available': True, 'prices_complete': True, 'source_integrated': True, 'credentials_valid': True, 'estimated_total_cny': 146.76, 'budget_cny': 150, 'available_balance_cny': 985.98, 'reported_pretax_cny': 14.02, 'whole_poc_budget_cny': 1000, 'bounded_cost_owner_approved': True, 'inventory_complete': False, 'uncertainty_reserve_cny': 2}


class ReleaseGate(unittest.TestCase):
    def test_reviewed_bounded_forecast_can_proceed(self):
        validate_release(evidence(), SHA, SHA, NOW)

    def test_wrong_source_or_unapproved_uncertainty_stops(self):
        with self.assertRaises(ValueError): validate_release(evidence(), SHA, 'b' * 40, NOW)
        e = evidence(); e['bounded_cost_owner_approved'] = False
        with self.assertRaises(ValueError): validate_release(e, SHA, SHA, NOW)

    def test_stale_or_future_evidence_stops(self):
        for hours in [-7, 1]:
            e = evidence(); e['checked_at'] = (NOW + timedelta(hours=hours)).isoformat()
            with self.subTest(hours=hours), self.assertRaises(ValueError): validate_release(e, SHA, SHA, NOW)

    def test_insufficient_funds_and_whole_poc_overrun_stop(self):
        for field, value in [('available_balance_cny', 100), ('reported_pretax_cny', 900), ('estimated_total_cny', 151), ('uncertainty_reserve_cny', 0), ('prices_complete', False)]:
            e = evidence(); e[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError): validate_release(e, SHA, SHA, NOW)

    def test_invalid_gate_never_calls_apply(self):
        run = Mock()
        e = evidence(); e['worker_available'] = False
        with self.assertRaises(ValueError): apply_saved_plan(run, e, SHA, SHA, NOW)
        run.assert_not_called()

    def test_apply_uses_saved_plan_without_retry(self):
        run = Mock(return_value='')
        apply_saved_plan(run, evidence(), SHA, SHA, NOW)
        run.assert_called_once_with(['apply', '-input=false', '-lock-timeout=60s', 'foundation.tfplan'], timeout=2400)
        run = Mock(side_effect=ValueError('provider failed'))
        with self.assertRaises(ValueError): apply_saved_plan(run, evidence(), SHA, SHA, NOW)
        self.assertEqual(run.call_count, 1)


if __name__ == '__main__': unittest.main()
