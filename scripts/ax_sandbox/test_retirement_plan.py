import unittest
from retirement_plan import changes

def plan(address, ident=None, actions=None):
    return {'resource_changes': [{'mode': 'managed', 'address': address,
            'change': {'actions': actions or ['delete'], 'before': {'id': ident}}}]}

class RetirementBoundaryTests(unittest.TestCase):
    def test_dedicated_cluster_can_be_retired(self):
        rows = changes(plan('module.sandbox.alicloud_cs_managed_kubernetes.cluster', 'c7de05cd5a9b84512925b1fc7a24e1bc9'))
        self.assertEqual(len(rows), 1)

    def test_shared_vpc_cannot_be_deleted_even_under_sandbox_prefix(self):
        with self.assertRaises(ValueError):
            changes(plan('module.sandbox.alicloud_vpc.shared', 'vpc-t4n4fi6r1a7bi6n93ftq3'))

    def test_current_ax_node_cannot_be_deleted(self):
        with self.assertRaises(ValueError):
            changes(plan('module.ax.alicloud_instance.node', 'i-t4nj9k37b82g5a64x424'))

    def test_another_cluster_cannot_be_deleted_at_same_address(self):
        with self.assertRaises(ValueError):
            changes(plan('module.sandbox.alicloud_cs_managed_kubernetes.cluster', 'c92787e953503492ea141a744c81498f1'))

    def test_cluster_replacement_is_not_retirement(self):
        with self.assertRaises(ValueError):
            changes(plan('module.sandbox.alicloud_cs_managed_kubernetes.cluster', 'c7de05cd5a9b84512925b1fc7a24e1bc9', ['delete', 'create']))

    def test_fresh_cluster_creation_is_rejected(self):
        with self.assertRaises(ValueError):
            changes(plan('module.sandbox.alicloud_cs_managed_kubernetes.cluster', 'c7de05cd5a9b84512925b1fc7a24e1bc9', ['create']))
