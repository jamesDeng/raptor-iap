import unittest

from runner import ADDRESS, INSTANCE, check_inputs, check_plan, comment, seal, unseal


def plan(before_type="ecs.g7.xlarge", after_type="ecs.u2i-c1m8.large", actions=None):
    original = {
        "id": INSTANCE, "instance_type": before_type, "system_disk_size": 120,
        "system_disk_encrypted": True, "deletion_protection": True,
    }
    changed = dict(original, instance_type=after_type)
    return {"complete": True, "resource_changes": [{
        "mode": "managed", "address": ADDRESS,
        "change": {"actions": actions or ["update"], "before": original, "after": changed},
    }]}


class AXRunnerContracts(unittest.TestCase):
    def test_only_exact_existing_instance_update_is_accepted(self):
        self.assertTrue(check_plan(plan()))
        self.assertTrue(check_plan(plan("ecs.u2i-c1m8.large", "ecs.g7.xlarge")))
        self.assertFalse(check_plan({"complete": True, "resource_changes": []}))
        for changed in (
            plan(actions=["delete", "create"]),
            plan("ecs.g7.xlarge", "ecs.e-c1m4.xlarge"),
        ):
            with self.assertRaises(ValueError):
                check_plan(changed)

    def test_reject_unrelated_resource_or_field_change(self):
        value = plan()
        value["resource_changes"][0]["change"]["after"]["system_disk_size"] = 80
        with self.assertRaises(ValueError):
            check_plan(value)
        value = plan()
        value["resource_changes"].append({
            "mode": "managed", "address": "module.node.alicloud_security_group.node",
            "change": {"actions": ["delete"]},
        })
        with self.assertRaises(ValueError):
            check_plan(value)
        value = plan()
        value["resource_changes"][0]["change"]["before"]["id"] = "foreign"
        with self.assertRaises(ValueError):
            check_plan(value)

    def test_comment_never_contains_plan_values(self):
        body = comment("a" * 40, True)
        self.assertIn("in place", body)
        self.assertNotIn(INSTANCE, body)
        self.assertNotIn("ecs.g7.xlarge", body)

    def test_plan_cipher_is_bound_to_revision(self):
        key = b"k" * 32
        encrypted = seal(b"private plan", key, "a" * 40)
        self.assertEqual(unseal(encrypted, key, "a" * 40), b"private plan")
        with self.assertRaises(Exception):
            unseal(encrypted, key, "b" * 40)

    def test_exact_inputs(self):
        inputs = {"ssh_public_key": "ssh-ed25519 " + "A" * 44, "admin_cidr": "192.0.2.1/32"}
        check_inputs(inputs)
        with self.assertRaises(ValueError):
            check_inputs(dict(inputs, password="secret"))
        with self.assertRaises(ValueError):
            check_inputs(dict(inputs, admin_cidr="0.0.0.0/0"))


if __name__ == "__main__":
    unittest.main()
