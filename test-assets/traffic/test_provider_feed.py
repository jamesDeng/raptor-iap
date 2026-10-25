import importlib.util
import pathlib
import tempfile
import unittest
from datetime import datetime, timezone

spec = importlib.util.spec_from_file_location('feed', pathlib.Path(__file__).with_name('provider_feed.py'))
feed = importlib.util.module_from_spec(spec)
if pathlib.Path(spec.origin).exists():
    spec.loader.exec_module(feed)

class FeedTests(unittest.TestCase):
    def fixture(self, count=2):
        ids = ['i-'+str(n) for n in range(count)]
        pages = [{'TotalCount':count,'PageNumber':n+1,'ScalingInstances':{'ScalingInstance':[{'InstanceId':node,'ScalingGroupId':'asg','LifecycleState':'Protected','HealthStatus':'Healthy'}]}} for n,node in enumerate(ids)]
        group = {'ScalingGroupId':'asg','VpcId':feed.VPC_ID,'DesiredCapacity':count}
        ecs = {node:{'InstanceId':node,'VpcAttributes':{'VpcId':feed.VPC_ID,'PrivateIpAddress':{'IpAddress':['10.0.0.'+str(n+1)]}}} for n,node in enumerate(ids)}
        nlb = {'Servers':[{'ServerId':node,'ServerGroupId':'sgp','ServerType':'Ecs','Port':6432,'Status':'Available','Weight':100} for node in ids], 'Listener':{'ListenerId':'lsn','ServerGroupId':'sgp','ListenerPort':6432,'ListenerStatus':'Running'}, 'Health':{'ListenerHealthStatus':[{'ListenerId':'lsn','ListenerPort':6432,'ServerGroupInfos':[{'ServerGroupId':'sgp','HeathCheckEnabled':True,'NonNormalServers':[]}]}]}}
        return pages,group,ecs,nlb
    def take(self, f):
        pages,group,ecs,nlb=f
        return feed.snapshot(lambda p:pages[p-1],lambda:group,lambda ids:[ecs[n] for n in ids],lambda:nlb,group_id='asg',server_group_id='sgp',now=datetime(2026,10,16,tzinfo=timezone.utc))
    def test_complete_two_three_four(self):
        for count in (2,3,4):
            s=self.take(self.fixture(count));self.assertEqual(len(s['members']),count);self.assertEqual(s['members'],s['healthyRegistered'])
    def test_fail_closed_invalid_inventory(self):
        for kind in ('count','page','foreign','duplicate','pending','unhealthy','vpc','address','backend','health','listener'):
            f=self.fixture();pages,g,e,n=f
            if kind=='count':pages[1]['TotalCount']=3
            if kind=='page':pages[1]['PageNumber']=1
            if kind=='foreign':pages[0]['ScalingInstances']['ScalingInstance'][0]['ScalingGroupId']='other'
            if kind=='duplicate':pages[1]['ScalingInstances']['ScalingInstance'][0]['InstanceId']='i-0'
            if kind=='pending':pages[0]['ScalingInstances']['ScalingInstance'][0]['LifecycleState']='Pending'
            if kind=='unhealthy':pages[0]['ScalingInstances']['ScalingInstance'][0]['HealthStatus']='Unhealthy'
            if kind=='vpc':e['i-0']['VpcAttributes']['VpcId']='other'
            if kind=='address':e['i-0']['VpcAttributes']['PrivateIpAddress']['IpAddress']=[]
            if kind=='backend':n['Servers'][0]['ServerGroupId']='other'
            if kind=='health':n['Health']['ListenerHealthStatus'][0]['ServerGroupInfos'][0]['NonNormalServers']=[{'ServerId':'foreign','Port':6432,'Status':'Unhealthy'}]
            if kind=='listener':n['Listener']['ServerGroupId']='other'
            with self.subTest(kind=kind),self.assertRaises(ValueError):self.take(f)
    def test_unhealthy_backend_excluded(self):
        f=self.fixture();f[3]['Health']['ListenerHealthStatus'][0]['ServerGroupInfos'][0]['NonNormalServers']=[{'ServerId':'i-0','Port':6432,'Status':'Unhealthy'}]
        self.assertEqual(self.take(f)['healthyRegistered'],['i-1'])

    def test_group_read_failure_reports_only_fixed_stage(self):
        from types import SimpleNamespace
        op=feed.Operator(SimpleNamespace())
        def fail(*args, **kwargs):
            raise RuntimeError('private provider response must stay private')
        op.aliyun=fail
        with self.assertRaises(ValueError) as caught:
            op.read()
        self.assertEqual(getattr(caught.exception,'stage',None),'ess-group')
        self.assertEqual(str(caught.exception),'Provider feed stopped at ess-group; evidence is no longer refreshed')
        self.assertNotIn('private provider',str(caught.exception))

    def test_nlb_read_failure_reports_exact_fixed_stage(self):
        from types import SimpleNamespace
        pages,group,ecs,_=self.fixture()
        group['ScalingGroupId']=feed.GROUP_ID
        for page in pages:
            page['ScalingInstances']['ScalingInstance'][0]['ScalingGroupId']=feed.GROUP_ID
        op=feed.Operator(SimpleNamespace(listener_id=feed.LISTENER_ID))
        op.fetch_group=lambda:group
        op.fetch_ess=lambda page:pages[page-1]
        op.fetch_ecs=lambda ids:[ecs[node] for node in ids]
        def fail(*args, **kwargs):
            raise RuntimeError('private NLB response must stay private')
        op.aliyun=fail
        with self.assertRaises(ValueError) as caught:
            op.read()
        self.assertEqual(getattr(caught.exception,'stage',None),'nlb-servers')
        self.assertNotIn('private NLB',str(caught.exception))

    def test_target_publication_failure_reports_only_fixed_stage(self):
        from types import SimpleNamespace
        op=feed.Operator(SimpleNamespace(kubeconfig='/private/kubeconfig',worker_inventory='/private/inventory'))
        def fail(*args, **kwargs):
            raise RuntimeError('private Workbench output must stay private')
        op.remote=fail
        with self.assertRaises(ValueError) as caught:
            op.write(self.take(self.fixture()))
        self.assertEqual(getattr(caught.exception,'stage',None),'targets-update')
        self.assertNotIn('private Workbench',str(caught.exception))

    def test_transitional_ess_member_has_distinct_sanitized_category(self):
        fixture=self.fixture()
        fixture[0][0]['ScalingInstances']['ScalingInstance'][0]['LifecycleState']='Pending'
        with self.assertRaises(ValueError) as caught:
            self.take(fixture)
        self.assertEqual(getattr(caught.exception,'stage',None),'ess-transition')

    def test_transitional_nlb_backend_has_distinct_sanitized_category(self):
        fixture=self.fixture()
        fixture[3]['Servers'][0]['Status']='Unavailable'
        with self.assertRaises(ValueError) as caught:
            self.take(fixture)
        self.assertEqual(getattr(caught.exception,'stage',None),'nlb-backend-state')

    def test_invalid_loop_limit_is_not_reported_as_target_read(self):
        import contextlib
        import io
        import sys
        from unittest.mock import patch
        output=io.StringIO()
        argv=['provider_feed.py','--interval','16','--workbench','/private/workbench',
              '--worker-inventory','/private/inventory','--kubeconfig','/private/kubeconfig']
        with patch.object(sys,'argv',argv), patch.object(feed.Operator,'existing_targets',return_value=()), \
                contextlib.redirect_stderr(output), self.assertRaises(SystemExit):
            feed.main()
        self.assertIn('stopped at loop',output.getvalue())
        self.assertNotIn('targets-read',output.getvalue())

    def test_publish_allowlist_and_changed_targets(self):
        writes=[];updates=[];s=self.take(self.fixture())
        previous=feed.publish(s,(),writes.append,updates.append)
        feed.publish(s,previous,writes.append,updates.append)
        self.assertEqual(len(updates),1)
        self.assertEqual(set(writes[0]),{'observedAt','desired','members','healthyRegistered'})
    def test_nlb_pagination_rejects_changed_count_duplicate_and_cursor(self):
        rows=self.fixture()[3]['Servers']
        for kind in ('valid','count','duplicate','cursor','truncated'):
            calls=[]
            def fetch(token):
                calls.append(token);second=bool(token)
                return {'TotalCount':3 if kind=='count' and second else 2,'Servers':[rows[0] if kind=='duplicate' or not second else rows[1]],'NextToken':('same' if kind=='cursor' else 'next') if not second or kind=='cursor' else ('more' if kind=='truncated' else '')}
            if kind=='valid':self.assertEqual(len(feed.collect_backends(fetch,'sgp')),2)
            else:
                with self.subTest(kind=kind),self.assertRaises(ValueError):feed.collect_backends(fetch,'sgp')
    def test_existing_targets_are_reused_without_update(self):
        from types import SimpleNamespace
        op=feed.Operator(SimpleNamespace(kubeconfig="/private/path"))
        op.remote=lambda *args: __import__('json').dumps([{'targets':['10.0.0.1:9930'],'labels':{'env':'rdev.ali','db_proxy_code':'DP00001','target_db_code':'D00002','ecs_instance_id':'i-0'}}])
        self.assertEqual(op.existing_targets(),(('i-0','10.0.0.1'),))

    def test_publish_failure_never_writes_inventory(self):
        writes=[]
        def fail(value): raise RuntimeError('failure')
        with self.assertRaises(RuntimeError):feed.publish(self.take(self.fixture()),(),writes.append,fail)
        self.assertEqual(writes,[])

    def test_nlb_adapter_reads_pages_listener_and_health(self):
        from types import SimpleNamespace
        op=feed.Operator(SimpleNamespace(listener_id='lsn'))
        rows=self.fixture()[3]['Servers'];calls=[]
        def api(service, operation, **kwargs):
            calls.append((operation,kwargs))
            if operation=='ListServerGroupServers':
                second=bool(kwargs['NextToken'])
                row=dict(rows[int(second)],ServerGroupId=feed.SERVER_GROUP_ID)
                return {'TotalCount':2,'Servers':[row],'NextToken':'' if second else 'next'}
            if operation=='GetListenerAttribute':return {'ListenerId':'lsn'}
            return {'ListenerHealthStatus':[]}
        op.aliyun=api
        self.assertEqual(len(op.fetch_nlb()['Servers']),2)
        self.assertEqual([x[0] for x in calls],['ListServerGroupServers','ListServerGroupServers','GetListenerAttribute','GetListenerHealthStatus'])

    def test_provider_reads_use_bounded_cli_retry(self):
        from types import SimpleNamespace
        op=feed.Operator(SimpleNamespace())
        commands=[]
        def command(argv):
            commands.append(argv)
            return '{}'
        op.command=command
        for service,operation in (('ess','DescribeScalingGroups'),('ess','DescribeScalingInstances'),
                                  ('ecs','DescribeInstances'),('nlb','ListServerGroupServers'),
                                  ('nlb','GetListenerAttribute'),('nlb','GetListenerHealthStatus')):
            with self.subTest(operation=operation):
                op.aliyun(service,operation)
        self.assertEqual(len(commands),6)
        for argv in commands:
            self.assertEqual(argv.count('--retry-count'),1)
            self.assertEqual(argv[argv.index('--retry-count')+1],'2')
        op.aliyun('ecs','DeleteInstance')
        self.assertNotIn('--retry-count',commands[-1])

    def test_workbench_and_configmap_commands_have_no_cli_retry(self):
        from types import SimpleNamespace
        op=feed.Operator(SimpleNamespace(workbench='offline-workbench',kubeconfig='/private/kubeconfig'))
        commands=[]
        op.command=lambda argv:commands.append(argv) or ''
        op.workbench('print("probe")')
        op.workbench(feed._TARGET_SCRIPT,'/private/kubeconfig','[]')
        self.assertEqual(len(commands),2)
        for argv in commands:
            self.assertNotIn('--retry-count',argv)
            self.assertNotIn('--retry-count',argv[-1])

    def test_read_reaching_deadline_after_cli_call_cannot_publish(self):
        from types import SimpleNamespace
        from unittest.mock import patch
        clock=[0.0];writes=[];commands=[]
        op=feed.Operator(SimpleNamespace())
        def cli(argv,**kwargs):
            commands.append(argv)
            clock[0]=15.0
            return SimpleNamespace(stdout='{}')
        with patch.object(feed.subprocess,'run',side_effect=cli):
            with self.assertRaises(ValueError):
                feed.run_loop(op.read,lambda value,**kwargs:writes.append(value),
                              interval=15,max_seconds=15,monotonic=lambda:clock[0])
        self.assertEqual(len(commands),1)
        self.assertEqual(writes,[])

    def test_remote_write_runs_the_tested_atomic_implementation(self):
        from types import SimpleNamespace
        import subprocess
        with tempfile.TemporaryDirectory() as d:
            p=pathlib.Path(d)/'unique.json'
            op=feed.Operator(SimpleNamespace(worker_inventory=str(p)))
            op.remote=lambda script,*args:subprocess.run(['python3','-c',script,*args],check=True,capture_output=True,text=True).stdout
            op.atomic_worker_write({'members':['i-one']})
            self.assertEqual(__import__('json').loads(p.read_text()),{'members':['i-one']})
            self.assertEqual(p.stat().st_mode & 0o777,0o600)

    def test_successful_cadence_stops_at_runtime_boundary(self):
        clock=[0.0];reads=[];writes=[]
        def read(*,deadline):
            reads.append(clock[0]);clock[0]+=2;return {}
        def write(value,*,deadline):
            writes.append(clock[0]);clock[0]+=1
        feed.run_loop(read,write,interval=15,max_seconds=30,monotonic=lambda:clock[0],sleep=lambda n:clock.__setitem__(0,clock[0]+n))
        self.assertEqual(reads,[0,15])
        self.assertEqual(writes,[2,17])
        self.assertEqual(clock[0],30)

    def test_final_runtime_boundary_prevents_publication(self):
        clock=[0.0];writes=[];reads=[]
        def read(*,deadline):reads.append(clock[0]);clock[0]=5;return {}
        with self.assertRaises(ValueError):
            feed.run_loop(read,lambda value,**kw:writes.append(value),max_seconds=5,monotonic=lambda:clock[0])
        self.assertEqual(reads,[0]);self.assertEqual(writes,[])

    def test_slow_read_cannot_publish(self):
        clock=[0.0];reads=[];writes=[]
        def read(*,deadline):
            reads.append(clock[0]);clock[0]=16;return {}
        with self.assertRaises(ValueError):
            feed.run_loop(read,lambda value,**kw:writes.append(value),monotonic=lambda:clock[0])
        self.assertEqual(reads,[0]);self.assertEqual(writes,[])

    def test_slow_targets_cannot_publish_worker_inventory(self):
        from types import SimpleNamespace
        clock=[0.0];writes=[];updates=[]
        op=feed.Operator(SimpleNamespace())
        def update(targets):
            updates.append(targets);clock[0]=24
        op.update_targets=update;op.atomic_worker_write=writes.append
        deadline=feed.Deadline(15,monotonic=lambda:clock[0])
        with self.assertRaises(ValueError):op.write(self.take(self.fixture()),deadline=deadline)
        self.assertEqual(len(updates),1);self.assertEqual(writes,[])

    def test_atomic_replace_refuses_expired_deadline(self):
        with tempfile.TemporaryDirectory() as d:
            p=pathlib.Path(d)/'inventory.json';p.write_text('old')
            with self.assertRaises(ValueError):feed.atomic_write(p,{},deadline=10,clock=lambda:10)
            self.assertEqual(p.read_text(),'old')
            self.assertEqual([x.name for x in pathlib.Path(d).iterdir()],['inventory.json'])

    def test_fsync_crossing_deadline_preserves_previous_worker_file(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as d:
            p=pathlib.Path(d)/'inventory.json';p.write_text('old');clock=[0]
            def fsync(fd):clock[0]=15
            with patch.object(feed.os,'fsync',side_effect=fsync):
                with self.assertRaises(ValueError):feed.atomic_write(p,{},deadline=15,clock=lambda:clock[0])
            self.assertEqual(p.read_text(),'old')

    def remote_fixture(self, *, probe_delay=0.2, command_delay=0, worker_offset=-30, reboot=False):
        from types import SimpleNamespace
        import json,shlex,subprocess
        op=feed.Operator(SimpleNamespace(workbench='offline-fixture'))
        clock=[100.0];calls=[];boot='12345678-1234-1234-1234-123456789abc'
        op.deadline=feed.Deadline(115,monotonic=lambda:clock[0])
        def command(argv):
            calls.append(argv)
            if len(calls)==1:
                clock[0]+=probe_delay
                return json.dumps({'monotonic':1000.0,'boot':boot})
            clock[0]+=command_delay
            worker_mono=1000.0+probe_delay+command_delay
            command_parts=shlex.split(argv[-1]);script=command_parts[2]
            actual_boot='different-boot' if reboot else boot
            preamble=("import time,builtins,io\ntime.monotonic=lambda:"+repr(worker_mono)+
                      "\ntime.time=lambda:"+repr(clock[0]+worker_offset)+
                      "\n_original_open=builtins.open\nbuiltins.open=lambda path,*a,**k:io.StringIO("+repr(actual_boot)+") if str(path)=='/proc/sys/kernel/random/boot_id' else _original_open(path,*a,**k)\n")
            return subprocess.run(['python3','-c',preamble+script,*command_parts[3:]],check=True,capture_output=True,text=True).stdout
        op.command=command
        return op,calls

    def test_worker_wall_clock_behind_does_not_extend_deadline(self):
        op,calls=self.remote_fixture(worker_offset=-30)
        self.assertEqual(op.remote("print('published')").strip(),'published')
        self.assertEqual(len(calls),2)

    def test_delayed_remote_command_refuses_even_with_worker_clock_behind(self):
        import subprocess
        with tempfile.TemporaryDirectory() as d:
            p=pathlib.Path(d)/'inventory.json';p.write_text('old')
            op,calls=self.remote_fixture(command_delay=20,worker_offset=-30)
            op.args.worker_inventory=str(p)
            with self.assertRaises(subprocess.CalledProcessError):op.atomic_worker_write({'members':[]})
            self.assertEqual(p.read_text(),'old')
            self.assertEqual(len(calls),2)

    def test_unbounded_clock_probe_latency_refuses_publication(self):
        op,calls=self.remote_fixture(probe_delay=3)
        with self.assertRaises(ValueError):op.remote("print('published')")
        self.assertEqual(len(calls),1)

    def test_worker_reboot_invalidates_clock_probe(self):
        import subprocess
        op,calls=self.remote_fixture(reboot=True)
        with self.assertRaises(subprocess.CalledProcessError):op.remote("print('published')")

    def test_subprocess_timeout_uses_remaining_budget(self):
        from types import SimpleNamespace
        from unittest.mock import patch
        op=feed.Operator(SimpleNamespace());op.deadline=feed.Deadline(15,monotonic=lambda:12)
        with patch.object(feed.subprocess,'run',return_value=SimpleNamespace(stdout='ok')) as run:
            self.assertEqual(op.command(['offline-fixture']),'ok')
        self.assertEqual(run.call_args.kwargs['timeout'],3)

    def test_atomic_file_private(self):
        with tempfile.TemporaryDirectory() as d:
            p=pathlib.Path(d)/'inventory.json';feed.atomic_write(p,{'members':[]})
            self.assertEqual(p.stat().st_mode & 0o777,0o600)
    def test_loop_stops_on_failure_and_bounds(self):
        calls=[]
        def fail(*,deadline):raise RuntimeError('sensitive')
        with self.assertRaises(ValueError):feed.run_loop(fail,lambda x:calls.append(x),interval=15,max_seconds=5400)
        self.assertEqual(calls,[])
        with self.assertRaises(ValueError):feed.run_loop(lambda:{},lambda x:None,interval=16,max_seconds=5401)

if __name__=='__main__':unittest.main()
