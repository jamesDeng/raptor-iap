import importlib.util,pathlib,unittest
spec=importlib.util.spec_from_file_location('inventory',pathlib.Path(__file__).with_name('provider_inventory.py'))
inventory=importlib.util.module_from_spec(spec);spec.loader.exec_module(inventory)
class InventoryTests(unittest.TestCase):
 def page(self,n):
  return {'PageNumber':n,'TotalCount':2,'ScalingInstances':{'ScalingInstance':[{'InstanceId':'i-'+str(n),'ScalingGroupId':'asg','LifecycleState':'Protected' if n==1 else 'InService','HealthStatus':'Healthy'}]}}
 def test_complete_protected_membership(self):
  rows=inventory.collect_members(self.page,'asg');self.assertEqual([r['InstanceId'] for r in rows],['i-1','i-2'])
 def test_incomplete_foreign_and_transitional_refuse(self):
  for kind in ['duplicate','foreign','pending','missing','changing total','empty page']:
   def fetch(n):
    b=self.page(n);r=b['ScalingInstances']['ScalingInstance'][0]
    if kind=='duplicate':r['InstanceId']='i-same'
    if kind=='foreign':r['ScalingGroupId']='other'
    if kind=='pending':r['LifecycleState']='Pending'
    if kind=='missing':del r['HealthStatus']
    if kind=='changing total' and n==2:b['TotalCount']=3
    if kind=='empty page':b['ScalingInstances']['ScalingInstance']=[]
    return b
   with self.subTest(kind=kind),self.assertRaises(ValueError):inventory.collect_members(fetch,'asg')
if __name__=='__main__':unittest.main()
