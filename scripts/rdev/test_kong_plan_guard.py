import copy,importlib.util,pathlib,unittest
P=pathlib.Path(__file__).with_name('kong_plan_guard.py')
OWN={'account_id':'1360282071200743','region':'ap-southeast-1','vpc_id':'vpc-t4n4fi6r1a7bi6n93ftq3','vswitch_id':'vsw-t4nop9qf6v46gw2sa8l7d'}
COST={'verified':True,'currency':'CNY','remaining_budget':900,'estimated_72h_total':30,'includes_instance_ip_lcu_traffic':True}
def plan():
 return {'resource_changes':[{'address':'module.kong_ingress[0].alicloud_slb_load_balancer.proxy','type':'alicloud_slb_load_balancer','change':{'actions':['create'],'after':{'address_type':'internet','vswitch_id':None,'instance_charge_type':'PayByCLCU','internet_charge_type':'paybytraffic','master_zone_id':'ap-southeast-1a','slave_zone_id':'ap-southeast-1b','tags':{'Project':'raptor-iap','Environment':'rdev.ali','Owner':'kong-ingress'}}}}]}
class Guard(unittest.TestCase):
 def guard(self,p=None,o=None,c=None):
  self.assertTrue(P.exists(),'Kong plan guard missing')
  s=importlib.util.spec_from_file_location('kong_guard',P);m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
  return m.validate_plan(p or plan(),o or OWN,c or COST)
 def test_exact_creation_allowed(self):self.assertEqual(self.guard()['creates'],1)
 def test_public_clb_cannot_claim_subnet_binding(self):
  p=plan();p['resource_changes'][0]['change']['after']['vswitch_id']=OWN['vswitch_id']
  with self.assertRaises(ValueError):self.guard(p)
 def test_existing_update_rejected(self):
  p=plan();p['resource_changes'][0]['change']['actions']=['update']
  with self.assertRaises(ValueError):self.guard(p)
 def test_foreign_target_rejected(self):
  for field in ['account_id','region','vpc_id','vswitch_id']:
   with self.subTest(field=field):
    o=dict(OWN);o[field]='foreign'
    with self.assertRaises(ValueError):self.guard(o=o)
 def test_uncosted_or_overbudget_rejected(self):
  for override in [{'verified':False},{'includes_instance_ip_lcu_traffic':False},{'estimated_72h_total':1001}]:
   with self.assertRaises(ValueError):self.guard(c=dict(COST,**override))
 def test_extra_resource_or_foreign_dns_rejected(self):
  for extra in [copy.deepcopy(plan()['resource_changes'][0]),{'address':'module.foundation.any','change':{'actions':['delete']}}]:
   p=plan();p['resource_changes'].append(extra)
   with self.assertRaises(ValueError):self.guard(p)
 def test_dns_requires_verified_owned_lb_destination(self):
  row={'address':'module.kong_ingress[0].alicloud_alidns_record.public["raptor.rdev"]','type':'alicloud_alidns_record','change':{'actions':['create'],'after':{'domain_name':'raptor-iap.top','rr':'raptor.rdev','type':'A','value':'203.0.113.42'}}}
  with self.assertRaises(ValueError):self.guard({'resource_changes':[row]})
 def test_verified_staged_dns_publication(self):
  lb=plan()['resource_changes'][0];lb['change']['actions']=['no-op'];lb['change']['after'].update(id='lb-verified',address='203.0.113.7')
  dns={'address':'module.kong_ingress[0].alicloud_alidns_record.public["raptor.rdev"]','type':'alicloud_alidns_record','change':{'actions':['create'],'after':{'domain_name':'raptor-iap.top','rr':'raptor.rdev','type':'A','value':'203.0.113.7'}}}
  o=dict(OWN,load_balancer_id='lb-verified',public_address='203.0.113.7')
  self.assertEqual(self.guard({'resource_changes':[lb,dns]},o=o)['creates'],1)
  dns['change']['after']['value']='203.0.113.42'
  with self.assertRaises(ValueError):self.guard({'resource_changes':[lb,dns]},o=o)
if __name__=='__main__':unittest.main()
