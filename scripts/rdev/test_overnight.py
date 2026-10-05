import copy
import unittest
from overnight import scope_from_state, build_actions, execute_actions

TAGS = {'Project': 'raptor-iap', 'Environment': 'rdev.ali', 'Owner': 'rdev-foundation'}
def state():
    return {'version':4,'resources':[
        {'module':'module.foundation','mode':'data','type':'alicloud_account','name':'current','instances':[{'attributes':{'id':'1234567890123456'}}]},
        *[{'module':'module.foundation','mode':'managed','type':t,'name':n,'instances':[{'attributes':{'id':i,'tags':TAGS}}]} for t,n,i in [('alicloud_vpc','env','vpc-test'),('alicloud_db_instance','platform','pgm-test')]]]}
def inventory():
    return {'account':'1234567890123456','vpc':'vpc-test','database':{'id':'pgm-test','status':'Running'},'nodes':[{'id':'i-test','status':'Running','stopped_mode':None}],'safe_to_stop_nodes':True}
class Overnight(unittest.TestCase):
    def test_state_requires_owned_exact_scope(self):
        self.assertEqual(scope_from_state(state())['database'],'pgm-test')
        for change in ('wrong_tag','missing_account','duplicate_database'):
            s=state()
            if change=='wrong_tag':s['resources'][2]['instances'][0]['attributes']['tags']={'Environment':'prod'}
            if change=='missing_account':s['resources'].pop(0)
            if change=='duplicate_database':s['resources'].append(copy.deepcopy(s['resources'][2]))
            with self.subTest(change=change),self.assertRaises(ValueError):scope_from_state(s)
    def test_sleep_preserves_disks_and_never_deletes(self):
        a=build_actions(inventory(),'sleep')
        self.assertEqual([x['api'] for x in a],['StopInstance','StopDBInstance'])
        self.assertEqual(a[0]['parameters']['StoppedMode'],'StopCharging')
        self.assertEqual(a[0]['parameters']['ForceStop'],'false')
    def test_wake_database_before_worker(self):
        i=inventory();i['database']['status']='Stopped';i['nodes'][0].update(status='Stopped',stopped_mode='StopCharging')
        self.assertEqual([x['api'] for x in build_actions(i,'wake')],['StartDBInstance','StartInstance'])
    def test_sleep_rejects_replacement_and_standard_stop(self):
        i=inventory();i['safe_to_stop_nodes']=False
        with self.assertRaises(ValueError):build_actions(i,'sleep')
        i=inventory();i['nodes'][0].update(status='Stopped',stopped_mode='KeepCharging')
        with self.assertRaises(ValueError):build_actions(i,'sleep')
    def test_stable_states_idempotent_and_busy_states_stop(self):
        i=inventory();self.assertEqual(build_actions(i,'wake'),[])
        i['database']['status']='Stopping'
        with self.assertRaises(ValueError):build_actions(i,'sleep')
    def test_mutation_once_then_poll_no_retry(self):
        calls=[]; reads=iter([{'status':'Stopping'},{'status':'Stopped','stopped_mode':'StopCharging'}])
        execute_actions(build_actions(inventory(),'sleep')[:1],lambda a:calls.append(a),lambda a:next(reads),sleep=lambda _:None)
        self.assertEqual(len(calls),1)
    def test_standard_stop_is_not_success(self):
        with self.assertRaises(ValueError):execute_actions(build_actions(inventory(),'sleep')[:1],lambda a:None,lambda a:{'status':'Stopped','stopped_mode':'KeepCharging'},sleep=lambda _:None)
    def test_mutation_error_stops_before_next_action(self):
        calls=[]
        def mutate(a):calls.append(a);raise ValueError('API failure')
        with self.assertRaises(ValueError):execute_actions(build_actions(inventory(),'sleep'),mutate,lambda a:{},sleep=lambda _:None)
        self.assertEqual(len(calls),1)

class Discovery(unittest.TestCase):
    def test_real_response_shapes_and_foreign_resources(self):
        from overnight import collect
        class API:
            def __init__(self): self.foreign=False;self.worker=False;self.account='1234567890123456'
            def call(self, service, action, params=None):
                if service=='sts':return {'AccountId':self.account}
                if action=='DescribeDBInstanceAttribute':return {'Items':{'DBInstanceAttribute':[{'DBInstanceId':'pgm-test','RegionId':'ap-southeast-1','VpcId':'vpc-test','Engine':'PostgreSQL','PayType':'Postpaid','DBInstanceType':'Primary','DBInstanceStorageType':'general_essd','ReadOnlyDBInstanceIds':{'ReadOnlyDBInstanceId':[]},'DBInstanceStatus':'Running'}]}}
                if action=='DescribeTags':return {'Items':{'TagInfos':[{'TagKey':k,'TagValue':v,'DBInstanceIds':{'DBInstanceIds':(['pgm-foreign'] if self.foreign else ['pgm-test'])}} for k,v in TAGS.items()]}}
                if action=='DescribeInstances':return {'TotalCount':1 if self.worker else 0,'Instances':{'Instance':[{}] if self.worker else []}}
                raise AssertionError('Unexpected API')
        api=API();scope=scope_from_state(state())
        self.assertEqual(collect(api,scope)['database']['status'],'Running')
        api.foreign=True
        with self.assertRaises(ValueError):collect(api,scope)
        api.foreign=False;api.worker=True
        with self.assertRaises(ValueError):collect(api,scope)
        api.worker=False;api.account='9999999999999999'
        with self.assertRaises(ValueError):collect(api,scope)
    def test_cli_errors_do_not_expose_raw_output(self):
        from overnight import CLI
        from unittest.mock import patch
        import subprocess
        with patch('overnight.subprocess.run',return_value=subprocess.CompletedProcess([],1,'','{"error_code":"AccessDenied","message":"secret-token-value"}')):
            with self.assertRaisesRegex(ValueError,'AccessDenied') as e:CLI('test').call('rds','StopDBInstance')
            self.assertNotIn('secret-token-value',str(e.exception))

class ReviewRegressions(unittest.TestCase):
    def test_modern_independent_controls_not_legacy_master(self):
        from overnight import automation_safe
        pool={'auto_scaling':{'enable':False},'management':{'auto_repair':False,'auto_vul_fix':False}}
        cluster={'operation_policy':{'cluster_auto_upgrade':{'enabled':False}}}
        self.assertTrue(automation_safe(pool,cluster))
        for feature in ('auto_repair','auto_vul_fix'):
            p=copy.deepcopy(pool);p['management'].update(enable=False);p['management'][feature]=True
            with self.subTest(feature=feature):self.assertFalse(automation_safe(p,cluster))
        self.assertFalse(automation_safe(pool,{}))
        self.assertFalse(automation_safe({'auto_scaling':{'enable':False},'management':{'enable':False}},cluster))
        c=copy.deepcopy(cluster);c['operation_policy']['cluster_auto_upgrade']['enabled']=True
        self.assertFalse(automation_safe(pool,c))
    def test_cli_explicitly_disables_profile_retries(self):
        from overnight import CLI
        from unittest.mock import patch
        import subprocess
        with patch('overnight.subprocess.run',return_value=subprocess.CompletedProcess([],0,'{}','')) as run:
            CLI('test').call('rds','StopDBInstance',{'DBInstanceId':'pgm-test'})
            args=run.call_args.args[0]
            self.assertIn('--retry-count',args)
            self.assertEqual(args[args.index('--retry-count')+1],'0')
    def test_poll_emits_intermediate_status(self):
        import io
        from contextlib import redirect_stdout
        statuses=iter([{'status':'Stopping'},{'status':'Stopped','stopped_mode':'StopCharging'}]);out=io.StringIO()
        with redirect_stdout(out):execute_actions(build_actions(inventory(),'sleep')[:1],lambda _:None,lambda _:next(statuses),sleep=lambda _:None)
        self.assertIn('Stopping',out.getvalue())
