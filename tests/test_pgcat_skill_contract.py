"""Executable local rehearsal fixtures; not LLM compliance or deployed API gates."""
import copy, json, pathlib, unittest
from tools.test_assets.rehearsal import evaluate
ROOT=pathlib.Path(__file__).resolve().parents[1]
class Rehearsal(unittest.TestCase):
 def state(self):
  s=json.loads((ROOT/'tests/fixtures/pgcat-replacement/inventory.json').read_text());s.update(requestId='request-fixture',attemptId='attempt-fixture',operationId='operation-fixture',now=100,operationStatus='new',approval={'granted':True,'consumed':False,'requestId':'request-fixture','attemptId':'attempt-fixture','operationId':'operation-fixture','envCode':'fixture.env','proxyCode':'proxy-fixture','desiredCapacity':3});s['nodes'][0]['protected']=False
  for n in s['nodes']:n.update(connectedClients=0,observedAt=95,up=True)
  return s
 def action(self):return {'type':'scale','envCode':'fixture.env','proxyCode':'proxy-fixture','targetDbCode':'db-fixture','desiredCapacity':3}
 def test_one_old_node_reduction_and_accepted_routing_race(self):
  s=self.state();self.assertEqual(evaluate(s,self.action())['status'],'eligible');s['nodes'][0]['registered']=True;self.assertEqual(evaluate(s,self.action())['status'],'eligible');s['nodes'][0]['registered']=False;self.assertEqual(evaluate(s,self.action())['status'],'eligible')
 def test_idle_client_is_not_zero(self):
  s=self.state();s['nodes'][0]['connectedClients']=1;self.assertEqual(evaluate(s,self.action())['status'],'denied')
 def test_missing_stale_unknown_and_denied_approval(self):
  for mutate in [lambda s:s['nodes'][0].pop('connectedClients'),lambda s:s['nodes'][0].update(observedAt=84),lambda s:s.update(operationStatus='unknown'),lambda s:s['approval'].update(granted=False),lambda s:s['approval'].update(consumed=True),lambda s:s['nodes'][0].update(up=False),lambda s:s['nodes'][0].update(connectedClients=float('nan'))]:
   s=self.state();mutate(s);self.assertEqual(evaluate(s,self.action())['status'],'denied')
 def test_target_parameter_request_attempt_binding(self):
  for key,val in [('envCode','wrong'),('proxyCode','wrong'),('desiredCapacity',2),('requestId','wrong'),('attemptId','wrong'),('operationId','wrong')]:
   s=self.state();s['approval'][key]=val;self.assertEqual(evaluate(s,self.action())['status'],'denied')
  a=self.action();a['targetDbCode']='wrong';self.assertEqual(evaluate(self.state(),a)['status'],'denied')
 def test_majority_equality_refused(self):
  s=self.state();a=self.action();a.update(type='deregister',nodeIds=['old-a','old-b']);self.assertEqual(evaluate(s,a)['status'],'denied');a['nodeIds']=['old-a'];self.assertEqual(evaluate(s,a)['status'],'eligible');s['nodes'][2]['healthy']=False;self.assertEqual(evaluate(s,a)['status'],'denied')
 def test_foreign_duplicate_and_partial_nodes_refused(self):
  for ids in [['foreign'],['old-a','old-a']]:
   a=self.action();a.update(type='protection',nodeIds=ids);self.assertEqual(evaluate(self.state(),a)['status'],'denied')
  s=self.state();s['operationStatus']='partial';self.assertEqual(evaluate(s,self.action())['status'],'denied')
 def test_all_unprotected_nodes_must_have_metrics(self):
  s=self.state();s['nodes'][1]['protected']=False;s['nodes'][1].pop('connectedClients');self.assertEqual(evaluate(s,self.action())['status'],'denied')
 def test_resume_requires_fresh_topology_and_restart_success(self):
  s=self.state();s['restartStatus']='failed';self.assertEqual(evaluate(s,self.action())['status'],'denied');s=self.state();s['topologyChanged']=True;self.assertEqual(evaluate(s,self.action())['status'],'denied')
