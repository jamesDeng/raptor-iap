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

if __name__=='__main__':unittest.main()
