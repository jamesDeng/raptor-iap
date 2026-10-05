import copy
import unittest
from live_plan import validate_plan


def fixture():
    values = {
        'alicloud_vpc.env': {'cidr_block': '10.70.0.0/16'},
        'alicloud_vswitch.workers': {'zone_id': 'ap-southeast-1a', 'cidr_block': '10.70.1.0/24'},
        'alicloud_vswitch.database': {'zone_id': 'ap-southeast-1a', 'cidr_block': '10.70.2.0/24'},
        'alicloud_nat_gateway.outbound': {'nat_type': 'Enhanced', 'payment_type': 'PayAsYouGo', 'internet_charge_type': 'PayByLcu', 'deletion_protection': True},
        'alicloud_eip_address.outbound': {'payment_type': 'PayAsYouGo', 'internet_charge_type': 'PayByTraffic', 'bandwidth': '5'},
        'alicloud_eip_association.outbound': {'instance_type': 'Nat'},
        'alicloud_snat_entry.workers': {},
        'alicloud_cs_managed_kubernetes.cluster': {'cluster_spec': 'ack.standard', 'profile': 'Default', 'version': '1.35.7-aliyun.1', 'new_nat_gateway': False, 'slb_internet_enabled': False, 'deletion_protection': True, 'skip_set_certificate_authority': True, 'pod_cidr': '10.72.0.0/16', 'service_cidr': '10.73.0.0/16', 'addons': [{'name': 'flannel', 'disabled': False, 'config': None, 'version': None}]},
        'alicloud_cs_kubernetes_node_pool.platform': {'instance_types': ['ecs.e-c1m2.xlarge'], 'desired_size': '1', 'instance_charge_type': 'PostPaid', 'system_disk_category': 'cloud_essd', 'system_disk_size': 40, 'internet_max_bandwidth_out': 0, 'spot_strategy': 'NoSpot'},
        'alicloud_db_instance.platform': {'engine': 'PostgreSQL', 'engine_version': '14.0', 'category': 'Basic', 'instance_type': 'pg.n2e.1c.1m', 'instance_storage': 10, 'db_instance_storage_type': 'general_essd', 'instance_charge_type': 'Postpaid', 'zone_id': 'ap-southeast-1a', 'security_ips': ['10.70.1.0/24', '10.72.0.0/16'], 'storage_auto_scale': 'Disable', 'deletion_protection': True},
        'terraform_data.account_guard': {},
        'terraform_data.service_role_guard': {},
    }
    plan = {'errored': False, 'complete': True, 'variables': {'account_id': {'value': '123456'}, 'kubernetes_version': {'value': '1.35.7-aliyun.1'}}, 'resource_changes': [
        {'address': 'module.foundation.' + address, 'mode': 'managed', 'type': address.split('.')[0], 'change': {'actions': ['create'], 'after': after}}
        for address, after in values.items()
    ]}

    refs = {
        'alicloud_vswitch.workers': {'vpc_id': 'alicloud_vpc.env.id'},
        'alicloud_vswitch.database': {'vpc_id': 'alicloud_vpc.env.id'},
        'alicloud_nat_gateway.outbound': {'vpc_id': 'alicloud_vpc.env.id', 'vswitch_id': 'alicloud_vswitch.workers.id'},
        'alicloud_cs_managed_kubernetes.cluster': {'vswitch_ids': 'alicloud_vswitch.workers.id'},
        'alicloud_cs_kubernetes_node_pool.platform': {'cluster_id': 'alicloud_cs_managed_kubernetes.cluster.id', 'vswitch_ids': 'alicloud_vswitch.workers.id'},
        'alicloud_db_instance.platform': {'vpc_id': 'alicloud_vpc.env.id', 'vswitch_id': 'alicloud_vswitch.database.id'},
        'alicloud_eip_association.outbound': {'allocation_id': 'alicloud_eip_address.outbound.id', 'instance_id': 'alicloud_nat_gateway.outbound.id'},
        'alicloud_snat_entry.workers': {'snat_table_id': 'alicloud_nat_gateway.outbound.snat_table_ids', 'source_vswitch_id': 'alicloud_vswitch.workers.id', 'snat_ip': 'alicloud_eip_address.outbound.ip_address'},
    }
    plan['configuration'] = {'provider_config': {'alicloud': {'full_name': 'registry.terraform.io/aliyun/alicloud', 'version_constraint': '1.293.0', 'expressions': {'region': {'constant_value': 'ap-southeast-1'}}}}, 'root_module': {'module_calls': {'foundation': {'source': '../../../terraform-module/rdev-foundation', 'module': {'resources': [
        {'address': address, 'mode': 'managed', 'provider_config_key': 'alicloud' if not address.startswith('terraform_data') else 'module.foundation:terraform', 'expressions': {field: {'references': [ref, ref.rsplit('.', 1)[0]]} for field, ref in refs.get(address, {}).items()}} for address in values
    ]}}}}}
    for resource in plan['resource_changes']:
        address = resource['address'][len('module.foundation.'):]
        resource['change']['after_unknown'] = {key: True for key in refs.get(address, {})}
    return plan


class InitialPlanGuard(unittest.TestCase):
    def test_expected_initial_plan(self):
        summary = validate_plan(fixture(), '123456')
        self.assertEqual(summary['cloud_creates'], 10)
        self.assertEqual(summary['local_guards'], 2)

    def test_update_delete_and_replacement_stop(self):
        for actions in [['update'], ['delete'], ['delete', 'create'], ['no-op']]:
            plan = fixture()
            plan['resource_changes'][0]['change']['actions'] = actions
            with self.subTest(actions=actions), self.assertRaises(ValueError):
                validate_plan(plan, '123456')

    def test_incomplete_or_drifted_plan_stops(self):
        for key, value in [('errored', True), ('complete', False), ('deferred_changes', [{}]), ('resource_drift', [{}])]:
            plan = fixture(); plan[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                validate_plan(plan, '123456')

    def test_extra_or_missing_resource_stops(self):
        plan = fixture(); plan['resource_changes'].pop()
        with self.assertRaises(ValueError): validate_plan(plan, '123456')
        plan = fixture(); extra = copy.deepcopy(plan['resource_changes'][0]); extra['address'] += '_extra'; plan['resource_changes'].append(extra)
        with self.assertRaises(ValueError): validate_plan(plan, '123456')

    def test_different_account_stops(self):
        with self.assertRaises(ValueError): validate_plan(fixture(), '654321')

    def test_unreviewed_capacity_or_public_database_stops(self):
        for suffix, field, value in [('alicloud_cs_kubernetes_node_pool.platform', 'desired_size', '2'), ('alicloud_cs_managed_kubernetes.cluster', 'slb_internet_enabled', True), ('alicloud_db_instance.platform', 'instance_storage', 100), ('alicloud_db_instance.platform', 'security_ips', ['0.0.0.0/0'])]:
            plan = fixture()
            change = next(r for r in plan['resource_changes'] if r['address'].endswith(suffix))
            change['change']['after'][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError): validate_plan(plan, '123456')

    def test_malformed_entries_stop(self):
        for entry in [{'mode': 'unexpected'}, None]:
            plan = fixture(); plan['resource_changes'].append(entry)
            with self.subTest(entry=entry), self.assertRaises(ValueError): validate_plan(plan, '123456')
        plan = fixture(); plan['resource_changes'][-1]['change']['after'] = None
        with self.assertRaises(ValueError): validate_plan(plan, '123456')

    def test_changed_network_or_reused_existing_parent_stops(self):
        for field, value in [('pod_cidr', '10.99.0.0/16'), ('service_cidr', '10.98.0.0/16'), ('addons', [{'name': 'terway', 'disabled': False}])]:
            plan = fixture(); next(r for r in plan['resource_changes'] if r['type'] == 'alicloud_cs_managed_kubernetes')['change']['after'][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError): validate_plan(plan, '123456')
        plan = fixture(); db = next(r for r in plan['resource_changes'] if r['type'] == 'alicloud_db_instance')
        db['change']['after']['vpc_id'] = 'existing-vpc'; db['change']['after_unknown']['vpc_id'] = False
        with self.assertRaises(ValueError): validate_plan(plan, '123456')

    def test_configuration_reference_or_region_change_stops(self):
        plan = fixture(); plan['configuration']['provider_config']['alicloud']['expressions']['region']['constant_value'] = 'cn-hangzhou'
        with self.assertRaises(ValueError): validate_plan(plan, '123456')
        plan = fixture(); resources = plan['configuration']['root_module']['module_calls']['foundation']['module']['resources']
        db = next(r for r in resources if r['address'] == 'alicloud_db_instance.platform')
        db['expressions']['vswitch_id'] = {'constant_value': 'existing-subnet'}
        with self.assertRaises(ValueError): validate_plan(plan, '123456')

    def test_malformed_json_shape_stops(self):
        for plan in [None, [], {}, {'resource_changes': None}]:
            with self.subTest(plan=plan), self.assertRaises(ValueError): validate_plan(plan, '123456')


if __name__ == '__main__': unittest.main()
