import json
import unittest
from state_access import validate_state, validate_plan, validate_write_plan


class StateGuardTest(unittest.TestCase):
    def test_existing_managed_resources_stop_probe(self):
        with self.assertRaises(ValueError):
            validate_state(json.dumps({'version': 4, 'resources': [{'mode': 'managed', 'type': 'alicloud_vpc'}]}))

    def test_invalid_state_stops_probe(self):
        for raw in ['not-json', '{}', '{"version":4,"resources":null}', '{"version":4,"resources":[{}]}']:
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                validate_state(raw)

    def test_empty_or_data_only_state_can_be_checked(self):
        for resources in [[], [{'mode': 'data', 'type': 'alicloud_account'}]]:
            validate_state(json.dumps({'version': 4, 'resources': resources}))


class PlanGuardTest(unittest.TestCase):
    def test_state_populated_between_pull_and_plan_is_rejected(self):
        for key in ['prior_state', 'planned_values', 'resource_drift', 'resource_changes']:
            with self.subTest(key=key), self.assertRaises(ValueError):
                validate_plan({key: {'child_modules': [{'resources': [{'mode': 'managed'}]}]}})

    def test_empty_or_data_only_plan_is_accepted(self):
        validate_plan({'planned_values': {'root_module': {'resources': [{'mode': 'data'}]}}})

    def test_incomplete_plan_is_rejected(self):
        for plan in [{'errored': True}, {'deferred_changes': [{}]}, []]:
            with self.subTest(plan=plan), self.assertRaises(ValueError):
                validate_plan(plan)


class WriteGuardTest(unittest.TestCase):
    def test_noop_or_wrong_marker_is_rejected(self):
        for change in [{}, {'actions': ['no-op'], 'after': 'new'}, {'actions': ['update'], 'after': 'wrong'}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate_write_plan({'output_changes': {'state_probe_nonce': change}}, 'new')

    def test_new_marker_requires_persistence(self):
        for action in ['create', 'update']:
            validate_write_plan({'output_changes': {'state_probe_nonce': {'actions': [action], 'after': 'new'}}}, 'new')


if __name__ == '__main__':
    unittest.main()
