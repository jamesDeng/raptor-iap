import importlib.util,pathlib,unittest

class PlanGuardTests(unittest.TestCase):
 def call(self, actions, tags=None, vpc_owner='rdev-foundation', subnet_refs=None, worker_unknown=None):
  source=pathlib.Path(__file__).with_name('plan_guard.py')
  self.assertTrue(source.exists(), 'plan guard missing')
  spec=importlib.util.spec_from_file_location('guard',source);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
  changes=[]
  for address,action in actions.items():
   after={'tags': tags or {'Project':'raptor-iap','Environment':'ax-sandbox.ali','Owner':'ax-sandbox'}}
   if address.endswith('cluster'): after.update(version='1.36.2-aliyun.1',new_nat_gateway=False,slb_internet_enabled=False,skip_set_certificate_authority=True,pod_cidr='10.74.0.0/16',service_cidr='10.75.0.0/16',cluster_spec='ack.standard')
   if address.endswith('workers'): after.update(vpc_id='vpc-owned',cidr_block='10.70.3.0/24',zone_id='ap-southeast-1a')
   changes.append({'mode':'managed','address':address,'change':{'actions':action,'after':after,'after_unknown':worker_unknown or {}}})
  configuration={'root_module':{'module_calls':{'sandbox':{'module':{'resources':[{'address':'alicloud_cs_managed_kubernetes.cluster','expressions':{'vswitch_ids':{'references':subnet_refs if subnet_refs is not None else ['alicloud_vswitch.workers.id','alicloud_vswitch.workers']}}}]}}}}}
  return m.inspect_plan({'errored':False,'resource_changes':changes,'configuration':configuration}, {'account_match':True,'vpc_id':'vpc-owned','vpc_owner':vpc_owner,'subnet_free':True})
 def test_minimal_creates_pass(self):
  self.assertTrue(self.call({'module.sandbox.alicloud_cs_managed_kubernetes.cluster':['create'],'module.sandbox.alicloud_vswitch.workers':['create'],'module.sandbox.terraform_data.account_guard':['create']})['passed'])
 def test_existing_platform_update_rejected(self):
  self.assertFalse(self.call({'module.foundation.alicloud_cs_managed_kubernetes.cluster':['update']})['passed'])
 def test_delete_rejected(self):
  self.assertFalse(self.call({'module.sandbox.alicloud_cs_managed_kubernetes.cluster':['delete','create']})['passed'])
 def test_wrong_ownership_rejected(self):
  self.assertFalse(self.call({'module.sandbox.alicloud_vswitch.workers':['create']},vpc_owner='other')['passed'])
 def test_empty_plan_rejected(self):
  self.assertFalse(self.call({})['passed'])

 def test_platform_subnet_reference_rejected(self):
  actions={'module.sandbox.alicloud_cs_managed_kubernetes.cluster':['create'],'module.sandbox.alicloud_vswitch.workers':['create'],'module.sandbox.terraform_data.account_guard':['create']}
  self.assertFalse(self.call(actions,subnet_refs=['var.platform_subnet'])['passed'])
 def test_unknown_worker_capacity_rejected(self):
  actions={'module.sandbox.alicloud_cs_managed_kubernetes.cluster':['create'],'module.sandbox.alicloud_vswitch.workers':['create'],'module.sandbox.terraform_data.account_guard':['create']}
  for key in ['worker_number','worker_numbers']:
   with self.subTest(key=key): self.assertFalse(self.call(actions,worker_unknown={key:True})['passed'])
