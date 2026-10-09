import unittest
from runner import select_stack,validate_inputs,seal,unseal,summary
class InfraStackTests(unittest.TestCase):
 def test_preserves_existing_state_address(self):
  stack=select_stack('infra-api');self.assertEqual(stack['prefix'],'rdev.ali');self.assertEqual(stack['key'],'infra-api.tfstate');self.assertEqual(stack['root'],'infra-terraform/environments/rdev-infra-api')
 def test_rejects_arbitrary_root_or_secret_inputs(self):
  for name in ('../../etc','other'):
   with self.assertRaises(ValueError):select_stack(name)
  values={'trigger_url':'https://fixture.ap-southeast-1.fcapp.run','gateway_instance_id':'api-shared-vpc-fixture'}
  validate_inputs(values,'infra-api')
  for extra in ('password','account_password','root'):
   with self.assertRaises(ValueError):validate_inputs(dict(values,**{extra:'private'}),'infra-api')
 def test_encrypted_plan_cannot_cross_stack(self):
  blob=seal(b'private plan',b'k'*32,'a'*40,'infra-api')
  self.assertEqual(unseal(blob,b'k'*32,'a'*40,'infra-api'),b'private plan')
  with self.assertRaises(Exception):unseal(blob,b'k'*32,'a'*40,'test-foundation')
 def test_infra_comment_has_actions_without_values(self):
  plan={'complete':True,'resource_changes':[{'mode':'managed','address':'module.infra_api_read.alicloud_ram_policy.read','change':{'actions':['update'],'after':{'secret':'PRIVATE'}}}]}
  text=summary(plan,'a'*40,'infra-api');self.assertIn('Infra API',text);self.assertIn('update',text);self.assertNotIn('PRIVATE',text)
if __name__=='__main__':unittest.main()
