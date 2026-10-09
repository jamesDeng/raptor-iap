import unittest
from runner import summary, seal, unseal, require_protection

class Contracts(unittest.TestCase):
    def test_comment_never_contains_values(self):
        p={'complete':True,'resource_changes':[{'mode':'managed','address':'module.database[0].alicloud_db_instance.test','change':{'actions':['update'],'after':{'password':'SECRET','id':'PRIVATE'}}}]}
        s=summary(p,'a'*40)
        self.assertIn('update',s)
        self.assertNotIn('SECRET',s); self.assertNotIn('PRIVATE',s)
    def test_reject_injected_address(self):
        with self.assertRaises(ValueError): summary({'complete':True,'resource_changes':[{'mode':'managed','address':'x\n@everyone','change':{'actions':['create']}}]},'a'*40)
    def test_encrypted_plan_bound_to_commit(self):
        key=b'k'*32; blob=seal(b'private plan',key,'a'*40)
        self.assertEqual(unseal(blob,key,'a'*40),b'private plan')
        with self.assertRaises(Exception):unseal(blob,key,'b'*40)
        with self.assertRaises(Exception):unseal(blob[:-1]+bytes([blob[-1]^1]),key,'a'*40)
    def test_reject_incomplete_plan(self):
        with self.assertRaises(ValueError):summary({'complete':False,'resource_changes':[]},'a'*40)
    def test_redact_string_resource_key(self):
        p={'complete':True,'resource_changes':[{'mode':'managed','address':'module.x["PRIVATE"].thing.y','change':{'actions':['update']}}]}
        self.assertNotIn('PRIVATE',summary(p,'a'*40))
    def test_missing_reviewer_blocks_cloud_execution(self):
        with self.assertRaises(ValueError):require_protection({'protection_rules':[]})
        require_protection({'protection_rules':[{'type':'required_reviewers','prevent_self_review':True,'reviewers':[{'id':1}]}]})
    def test_reject_bad_key(self):
        with self.assertRaises(ValueError):seal(b'plan',b'k','a'*40)


class ProxyInputContracts(unittest.TestCase):
    def inputs(self):return dict(account_id='123',vpc_id='vpc-test',db_vswitch_id='vsw-test',config_bucket='bucket-test',db_code='db-test')
    def test_existing_foundation_input_stays_valid(self):
        from runner import validate_inputs
        validate_inputs(self.inputs())
    def test_proxy_requires_real_binding(self):
        from runner import validate_inputs
        x=self.inputs();x['proxy_enabled']=True
        with self.assertRaises(ValueError):validate_inputs(x)
    def test_accept_bound_proxy_without_secret_values(self):
        from runner import validate_inputs
        x=self.inputs();x.update(proxy_enabled=True,proxy_code='proxy-test',proxy_worker_vswitch_id='vsw-worker',proxy_secret_version='version.abc_123--')
        validate_inputs(x)
    def test_reject_secret_or_reference_injection(self):
        from runner import validate_inputs
        x=self.inputs();x['password']='SECRET'
        with self.assertRaises(ValueError):validate_inputs(x)
        x=self.inputs();x.update(proxy_enabled=True,proxy_code='proxy-test',proxy_worker_vswitch_id='vsw-worker',proxy_secret_version='version&extra=true')
        with self.assertRaises(ValueError):validate_inputs(x)

class AdoptionSummaryContracts(unittest.TestCase):
    def test_comment_shows_import_without_identifier(self):
        p={'complete':True,'resource_changes':[{'mode':'managed','address':'module.proxy[0].module.config_iam.alicloud_ram_role.config','change':{'actions':['no-op'],'importing':{'id':'PRIVATE_ROLE'}}}]}
        s=summary(p,'a'*40)
        self.assertIn('import',s);self.assertNotIn('PRIVATE_ROLE',s)

class NetworkSummaryContracts(unittest.TestCase):
    def test_private_cidr_key_is_redacted(self):
        p={'complete':True,'resource_changes':[{'mode':'managed','address':'module.proxy[0].alicloud_security_group_rule.sql["10.70.1.0/24"]','change':{'actions':['create']}}]}
        s=summary(p,'a'*40)
        self.assertIn('redacted-key',s);self.assertNotIn('10.70.1.0',s)

if __name__=='__main__':unittest.main()
