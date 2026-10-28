import importlib.util
import pathlib
import tempfile
import unittest
import hashlib
import json
import os
import uuid
from datetime import datetime, timezone

spec = importlib.util.spec_from_file_location('feed', pathlib.Path(__file__).with_name('provider_feed.py'))
feed = importlib.util.module_from_spec(spec)
if pathlib.Path(spec.origin).exists():
    spec.loader.exec_module(feed)

class FeedTests(unittest.TestCase):
    def setUp(self):
        from unittest.mock import patch
        guard=patch.object(feed,'_require_verified');guard.start();self.addCleanup(guard.stop)
        wrapper=patch.object(feed,'_wrapper_command',side_effect=lambda path,args:[path]+args)
        wrapper.start();self.addCleanup(wrapper.stop)

    def test_private_publication_is_immutable_and_precedes_inventory(self):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(os.path.realpath(directory)); root.chmod(0o700)
            contract=root/'contract'; contract.mkdir(mode=0o700)
            run=root/'run'; run.mkdir(mode=0o700)
            inventory=run/'provider-inventory.json'
            context=dict(schemaVersion=1,runId=str(uuid.uuid4()),supervisorId=str(uuid.uuid4()),
                targetSha256='a'*64,bundleSha256='b'*64,nonce=str(uuid.uuid4()))
            session=str(uuid.uuid4())
            raw=json.dumps(dict(schemaVersion=2,feedSession=session,sequence=1,
                observedAt=123.0,members=['i-one']),separators=(',',':')).encode('ascii')
            feed.write_private_publication(str(contract),context,session,1,123.0,
                {'i-one':'10.0.0.1','i-two':'10.0.0.2'},str(inventory),raw)
            private=contract/'provider-source'
            self.assertEqual(private.stat().st_mode & 0o777,0o700)
            sidecar=private/'000001.json'
            self.assertEqual(sidecar.stat().st_mode & 0o777,0o600)
            self.assertEqual(json.loads(sidecar.read_text()),dict(schemaVersion=1,runId=context['runId'],
                supervisorId=context['supervisorId'],contextNonce=context['nonce'],
                targetSha256=context['targetSha256'],bundleSha256=context['bundleSha256'],
                feedSession=session,sequence=1,observedAt=123.0,
                inventorySha256=hashlib.sha256(raw).hexdigest(),
                inventorySemanticSha256=hashlib.sha256(json.dumps(json.loads(raw),
                    sort_keys=True,separators=(',',':'),allow_nan=False).encode('ascii')).hexdigest(),
                privateAddresses={'i-one':'10.0.0.1','i-two':'10.0.0.2'}))
            self.assertEqual(inventory.read_bytes(),raw)
            inventory.unlink()
            with self.assertRaises(ValueError):
                feed.write_private_publication(str(contract),context,session,1,123.0,
                    {'i-one':'10.0.0.1','i-two':'10.0.0.2'},str(inventory),raw)
            self.assertFalse(inventory.exists())

    def test_historical_cleaned_frame_matches_semantic_hash_and_tamper_differs(self):
        from capture_window import clean_inventory
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(os.path.realpath(directory));root.chmod(0o700)
            contract=root/'contract';contract.mkdir(mode=0o700)
            run=root/'run';run.mkdir(mode=0o700)
            inventory_path=run/'provider-inventory.json'
            context=dict(schemaVersion=1,runId=str(uuid.uuid4()),supervisorId=str(uuid.uuid4()),
                targetSha256='a'*64,bundleSha256='b'*64,nonce=str(uuid.uuid4()))
            session=str(uuid.uuid4())
            observed=self.take(self.fixture())
            observed.update(feedSession=session,sequence=1,mode='disabled-rehearsal')
            inventory={key:observed[key] for key in feed.FIELDS}
            inventory['mode']=observed['mode']
            raw=json.dumps(inventory,indent=2).encode('ascii')
            feed.write_private_publication(str(contract),context,session,1,
                observed['observedAt'],observed['privateAddresses'],str(inventory_path),raw)
            sidecar=json.loads((contract/'provider-source'/'000001.json').read_text())
            selected_frame=clean_inventory(json.loads(raw))
            semantic=lambda value:hashlib.sha256(json.dumps(value,sort_keys=True,
                separators=(',',':'),allow_nan=False).encode('ascii')).hexdigest()
            self.assertEqual(sidecar['inventorySemanticSha256'],semantic(selected_frame))
            self.assertEqual(sidecar['inventorySha256'],hashlib.sha256(raw).hexdigest())
            self.assertNotEqual(sidecar['inventorySemanticSha256'],sidecar['inventorySha256'])
            altered=dict(selected_frame,desired=3)
            self.assertNotEqual(sidecar['inventorySemanticSha256'],semantic(altered))

    def test_bound_feed_single_remote_write_keeps_unacknowledged_worker_files(self):
        from types import SimpleNamespace
        from unittest.mock import patch
        import subprocess
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(os.path.realpath(directory)); root.chmod(0o700)
            contract=root/'contract'; contract.mkdir(mode=0o700)
            run=root/'run'; run.mkdir(mode=0o700)
            inventory=run/'provider-inventory.json'; receipt=root/'status.json'
            context=dict(schemaVersion=1,runId=str(uuid.uuid4()),supervisorId=str(uuid.uuid4()),
                targetSha256='a'*64,bundleSha256='b'*64,nonce=str(uuid.uuid4()))
            args=SimpleNamespace(mode='disabled-rehearsal',status_file=str(receipt),
                run_context=str(root/'run-context.json'),contract_dir=str(contract),
                worker_inventory=str(inventory))
            validation=patch.object(feed,'validate_run_context',return_value=context)
            validation.start();self.addCleanup(validation.stop)
            op=feed.Operator(args)
            calls=[]
            def remote(script,*arguments):
                calls.append((script,arguments))
                return subprocess.run(['python3','-c',script,*arguments],check=True,
                    capture_output=True,text=True).stdout
            op.remote=remote; op.update_targets=lambda targets:None
            with patch.object(feed,'write_status_receipt',side_effect=OSError('local failure')):
                with self.assertRaises(feed.FeedStop) as error:
                    op.write(self.take(self.fixture()))
            self.assertEqual(error.exception.stage,'status-write')
            self.assertEqual(len(calls),1)
            import ast
            ast.parse(calls[0][0],feature_version=(3,6))
            self.assertTrue(inventory.exists())
            self.assertTrue((contract/'provider-source'/'000001.json').exists())
            self.assertFalse(receipt.exists())
            with self.assertRaises(feed.FeedStop):op.write(self.take(self.fixture()))
            self.assertEqual(len(calls),1)

    def test_inactive_supervisor_before_publication_prevents_worker_call(self):
        from types import SimpleNamespace
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(os.path.realpath(directory));root.chmod(0o700)
            context=dict(schemaVersion=1,runId=str(uuid.uuid4()),supervisorId=str(uuid.uuid4()),
                targetSha256='a'*64,bundleSha256='b'*64,nonce=str(uuid.uuid4()))
            args=SimpleNamespace(mode='disabled-rehearsal',status_file=str(root/'status.json'),
                run_context=str(root/'run-context.json'),contract_dir=str(root/'contract'),
                worker_inventory=str(root/'run'/'provider-inventory.json'))
            with patch.object(feed,'validate_run_context',side_effect=[context,ValueError('inactive')]):
                op=feed.Operator(args)
                calls=[];op.remote=lambda *parts:calls.append(parts)
                op.update_targets=lambda *parts:calls.append(parts)
                with self.assertRaises(feed.FeedStop):op.write(self.take(self.fixture()))
            self.assertEqual(calls,[])
            self.assertFalse((root/'status.json').exists())

    def test_partial_prior_inventory_refuses_next_sidecar(self):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(os.path.realpath(directory)); root.chmod(0o700)
            contract=root/'contract';contract.mkdir(mode=0o700)
            run=root/'run'; run.mkdir(mode=0o700)
            inventory=run/'provider-inventory.json'; session=str(uuid.uuid4())
            context=dict(schemaVersion=1,runId=str(uuid.uuid4()),supervisorId=str(uuid.uuid4()),
                targetSha256='a'*64,bundleSha256='b'*64,nonce=str(uuid.uuid4()))
            def raw(sequence):
                return json.dumps(dict(schemaVersion=2,feedSession=session,sequence=sequence,
                    observedAt=float(sequence)),separators=(',',':')).encode('ascii')
            addresses={'i-one':'10.0.0.1','i-two':'10.0.0.2'}
            feed.write_private_publication(str(contract),context,session,1,1.0,
                addresses,str(inventory),raw(1))
            inventory.unlink()
            with self.assertRaises(ValueError):
                feed.write_private_publication(str(contract),context,session,2,2.0,
                    addresses,str(inventory),raw(2))
            self.assertFalse((contract/'provider-source'/'000002.json').exists())

    def test_private_publication_rejects_symlinked_parent(self):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(os.path.realpath(directory));root.chmod(0o700)
            actual=root/'actual';actual.mkdir(mode=0o700)
            (actual/'contract').mkdir(mode=0o700)
            (actual/'run').mkdir(mode=0o700)
            alias=root/'alias';alias.symlink_to(actual,target_is_directory=True)
            context=dict(schemaVersion=1,runId=str(uuid.uuid4()),supervisorId=str(uuid.uuid4()),
                targetSha256='a'*64,bundleSha256='b'*64,nonce=str(uuid.uuid4()))
            session=str(uuid.uuid4())
            raw=json.dumps(dict(feedSession=session,sequence=1,observedAt=1.0)).encode('ascii')
            with self.assertRaises(ValueError):
                feed.write_private_publication(str(alias/'contract'),context,session,1,1.0,
                    {'a':'10.0.0.1','b':'10.0.0.2'},str(alias/'run'/'provider-inventory.json'),raw)
            self.assertFalse((actual/'contract'/'provider-source').exists())

    def fixture(self, count=2):
        ids = ['i-'+str(n) for n in range(count)]
        pages = [{'TotalCount':count,'PageNumber':n+1,'ScalingInstances':{'ScalingInstance':[{'InstanceId':node,'ScalingGroupId':'asg','LifecycleState':'Protected','HealthStatus':'Healthy'}]}} for n,node in enumerate(ids)]
        group = {'ScalingGroupId':'asg','VpcId':feed.VPC_ID,'DesiredCapacity':count}
        ecs = {node:{'InstanceId':node,'VpcAttributes':{'VpcId':feed.VPC_ID,'PrivateIpAddress':{'IpAddress':['10.0.0.'+str(n+1)]}}} for n,node in enumerate(ids)}
        nlb = {'Servers':[{'ServerId':node,'ServerGroupId':'sgp','ServerType':'Ecs','Port':6432,'Status':'Available','Weight':100} for node in ids], 'Listener':{'ListenerId':'lsn','ServerGroupId':'sgp','ListenerPort':6432,'ListenerStatus':'Running'}, 'Health':{'ListenerHealthStatus':[{'ListenerId':'lsn','ListenerPort':6432,'ServerGroupInfos':[{'ServerGroupId':'sgp','HeathCheckEnabled':True,'NonNormalServers':[]}]}]}}
        return pages,group,ecs,nlb
    def take(self, f, **kwargs):
        pages,group,ecs,nlb=f
        return feed.snapshot(lambda p:pages[p-1],lambda:group,lambda ids:[ecs[n] for n in ids if n in ecs],lambda:nlb,group_id='asg',server_group_id='sgp',now=datetime(2026,10,16,tzinfo=timezone.utc),**kwargs)
    def receipt_operator(self, path, mode='disabled-rehearsal'):
        from types import SimpleNamespace
        op=feed.Operator(SimpleNamespace(status_file=str(path),mode=mode))
        writes=[]
        op.atomic_worker_write=lambda value:writes.append(value)
        op.update_targets=lambda value:None
        return op,writes

    def test_status_receipt_after_worker_success_is_private_and_address_free(self):
        import json
        for mode in ('full-replacement','disabled-rehearsal'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                path=pathlib.Path(directory)/'status.json'
                op,writes=self.receipt_operator(path,mode)
                self.assertFalse(path.exists())
                def worker(value):
                    if writes:
                        self.assertEqual(json.loads(path.read_text())['sequence'],len(writes))
                    else:
                        self.assertFalse(path.exists())
                    writes.append(value)
                op.atomic_worker_write=worker
                for sequence in (1,2):
                    value=self.take(self.fixture())
                    value['credential']='synthetic-secret-must-not-escape'
                    op.write(value)
                    self.assertEqual(len(writes),sequence)
                    self.assertTrue(path.exists(), 'successful worker publication needs a receipt')
                    self.assertEqual(json.loads(path.read_text()),{
                        'feedSession':op.feedSession,'sequence':sequence,'mode':mode,
                        'desired':2,'observedAt':1792108800.0,
                        'rosterSha256':feed.roster_sha(value)})
                    self.assertEqual(path.stat().st_mode & 0o777,0o600)
                    for private in (b'10.0.0.',b'synthetic-secret',b'privateAddresses',b'i-0'):
                        self.assertNotIn(private,path.read_bytes())
                self.assertEqual(path.parent.stat().st_mode & 0o777,0o700)
                self.assertEqual(list(path.parent.iterdir()),[path])

    def test_status_path_requires_absent_absolute_path_and_existing_private_directory(self):
        from types import SimpleNamespace
        with tempfile.TemporaryDirectory() as directory:
            parent=pathlib.Path(directory)
            existing=parent/'existing';existing.write_text('keep')
            dangling=parent/'dangling';dangling.symlink_to(parent/'absent')
            public=parent/'public';public.mkdir(mode=0o755)
            paths=('relative.json',existing,dangling,parent,parent/'missing'/'status',public/'status')
            for path in paths:
                with self.subTest(path=str(path)),self.assertRaises(ValueError):
                    feed.Operator(SimpleNamespace(status_file=str(path)))
            self.assertEqual(existing.read_text(),'keep')

    def test_failed_worker_publication_never_creates_or_advances_receipt(self):
        for prior in (False,True):
            with self.subTest(prior=prior),tempfile.TemporaryDirectory() as directory:
                path=pathlib.Path(directory)/'status.json'
                op,writes=self.receipt_operator(path)
                if prior:
                    op.write(self.take(self.fixture()))
                    self.assertTrue(path.exists(), 'successful worker publication needs a receipt')
                before=path.read_bytes() if prior else None
                attempts=[]
                def uncertain(value):
                    attempts.append(value)
                    raise RuntimeError('sensitive uncertain worker write')
                op.atomic_worker_write=uncertain
                with self.assertRaises(feed.FeedStop):op.write(self.take(self.fixture()))
                self.assertEqual(path.read_bytes() if path.exists() else None,before)
                with self.assertRaises(feed.FeedStop):op.write(self.take(self.fixture()))
                self.assertEqual(len(attempts),1)

    def test_local_receipt_failure_stops_without_advancing_or_retrying_worker(self):
        from unittest.mock import patch
        for prior in (False,True):
            with self.subTest(prior=prior),tempfile.TemporaryDirectory() as directory:
                path=pathlib.Path(directory)/'status.json'
                op,writes=self.receipt_operator(path)
                if prior:
                    op.write(self.take(self.fixture()))
                    self.assertTrue(path.exists(), 'successful worker publication needs a receipt')
                before=path.read_bytes() if prior else None
                with patch.object(feed.os,'replace',side_effect=OSError('private failure')):
                    with self.assertRaises(feed.FeedStop) as caught:
                        op.write(self.take(self.fixture()))
                self.assertEqual(caught.exception.stage,'status-write')
                self.assertEqual(path.read_bytes() if path.exists() else None,before)
                self.assertEqual(list(path.parent.iterdir()),[path] if prior else [])
                with self.assertRaises(feed.FeedStop):op.write(self.take(self.fixture()))
                self.assertEqual(len(writes),2 if prior else 1)

    def test_status_receipt_cannot_cross_absolute_publication_deadline(self):
        from unittest.mock import patch
        for delay in ('worker','fsync'):
            with self.subTest(delay=delay),tempfile.TemporaryDirectory() as directory:
                path=pathlib.Path(directory)/'status.json'
                op,writes=self.receipt_operator(path)
                clock=[0.0]
                def worker(value):
                    writes.append(value)
                    if delay=='worker':clock[0]=15.0
                def fsync(fd):clock[0]=15.0
                op.atomic_worker_write=worker
                with patch.object(feed.os,'fsync',side_effect=fsync):
                    with self.assertRaises(feed.FeedStop):
                        op.write(self.take(self.fixture()),deadline=feed.Deadline(15,monotonic=lambda:clock[0]))
                self.assertEqual(len(writes),1)
                self.assertFalse(path.exists())
                self.assertEqual(list(path.parent.iterdir()),[])

    def test_status_receipt_rejects_changed_session_before_worker_write(self):
        with tempfile.TemporaryDirectory() as directory:
            path=pathlib.Path(directory)/'status.json'
            op,writes=self.receipt_operator(path)
            op.write(self.take(self.fixture()))
            self.assertTrue(path.exists(), 'successful worker publication needs a receipt')
            before=path.read_bytes()
            value=dict(self.take(self.fixture()),feedSession='12345678-1234-4234-8234-123456789abc')
            with self.assertRaises(feed.FeedStop):op.write(value)
            self.assertEqual(path.read_bytes(),before)
            self.assertEqual(len(writes),1)

    def test_new_feed_cannot_reuse_previous_session_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            path=pathlib.Path(directory)/'status.json'
            op,writes=self.receipt_operator(path)
            op.write(self.take(self.fixture()))
            self.assertTrue(path.exists(), 'successful worker publication needs a receipt')
            before=path.read_bytes()
            with self.assertRaises(ValueError):self.receipt_operator(path)
            self.assertEqual(path.read_bytes(),before)

    def test_cli_status_file_publishes_receipt(self):
        import sys
        import json
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            path=pathlib.Path(directory)/'status.json'
            argv=['provider_feed.py','--status-file',str(path),'--workbench','offline',
                  '--worker-inventory','/private/run/provider-inventory.json','--kubeconfig','/private/kubeconfig',
                  '--run-context',str(path.parent/'run-context.json'),'--contract-dir','/private/contract']
            def once(read,write,**kwargs):write(self.take(self.fixture()))
            with patch.object(sys,'argv',argv),patch.object(feed.Operator,'existing_targets',return_value=()), \
                    patch.object(feed.Operator,'update_targets'),patch.object(feed.Operator,'atomic_worker_write'), \
                    patch.object(feed,'run_loop',side_effect=once), \
                    patch.object(feed,'validate_run_context',return_value={}):
                feed.main()
            self.assertEqual(json.loads(path.read_text())['sequence'],1)
            self.assertEqual(json.loads(path.read_text())['rosterSha256'],
                feed.roster_sha(self.take(self.fixture())))

    def test_complete_two_three_four(self):
        for count in (2,3,4):
            s=self.take(self.fixture(count));self.assertEqual(len(s['members']),count);self.assertEqual(s['members'],s['healthyRegistered'])
    def test_fail_closed_invalid_inventory(self):
        for kind in ('count','page','foreign','duplicate','unknown-lifecycle','unknown-health','vpc','address','backend','health','listener'):
            f=self.fixture();pages,g,e,n=f
            if kind=='count':pages[1]['TotalCount']=3
            if kind=='page':pages[1]['PageNumber']=1
            if kind=='foreign':pages[0]['ScalingInstances']['ScalingInstance'][0]['ScalingGroupId']='other'
            if kind=='duplicate':pages[1]['ScalingInstances']['ScalingInstance'][0]['InstanceId']='i-0'
            if kind=='unknown-lifecycle':pages[0]['ScalingInstances']['ScalingInstance'][0]['LifecycleState']='Unknown'
            if kind=='unknown-health':pages[0]['ScalingInstances']['ScalingInstance'][0]['HealthStatus']='Unknown'
            if kind=='vpc':e['i-0']['VpcAttributes']['VpcId']='other'
            if kind=='address':e['i-0']['VpcAttributes']['PrivateIpAddress']['IpAddress']=[]
            if kind=='backend':n['Servers'][0]['ServerGroupId']='other'
            if kind=='health':n['Health']['ListenerHealthStatus'][0]['ServerGroupInfos'][0]['NonNormalServers']=[{'ServerId':'foreign','Port':6432,'Status':'Unhealthy'}]
            if kind=='listener':n['Listener']['ServerGroupId']='other'
            with self.subTest(kind=kind),self.assertRaises(ValueError):self.take(f)
    def test_unhealthy_backend_excluded(self):
        f=self.fixture();f[3]['Health']['ListenerHealthStatus'][0]['ServerGroupInfos'][0]['NonNormalServers']=[{'ServerId':'i-0','Port':6432,'Status':'Unhealthy'}]
        self.assertEqual(self.take(f)['healthyRegistered'],['i-1'])

    def test_schema_two_snapshot_has_complete_sorted_states(self):
        result=self.take(self.fixture())
        self.assertEqual(result, {
            'schemaVersion':2, 'observedAt':1792108800.0, 'desired':2,
            'members':['i-0','i-1'],
            'memberStates':[{'id':'i-0','lifecycle':'Protected','health':'Healthy','addressKnown':True},
                            {'id':'i-1','lifecycle':'Protected','health':'Healthy','addressKnown':True}],
            'backendStates':[{'id':'i-0','status':'Available','weight':100,'listenerHealth':'Normal'},
                             {'id':'i-1','status':'Available','weight':100,'listenerHealth':'Normal'}],
            'healthyRegistered':['i-0','i-1'],
            'privateAddresses':{'i-0':'10.0.0.1','i-1':'10.0.0.2'}})

    def test_new_pending_member_without_ecs_or_backend_is_explicit(self):
        for lifecycle in ('Pending','Pending:Wait'):
            fixture=self.fixture(3)
            fixture[0][2]['ScalingInstances']['ScalingInstance'][0]['LifecycleState']=lifecycle
            fixture[3]['Servers'].pop()
            for missing in ('instance','address'):
                with self.subTest(lifecycle=lifecycle,missing=missing):
                    if missing=='instance':fixture[2].pop('i-2')
                    else:fixture[2]['i-2']={'InstanceId':'i-2','VpcAttributes':{'VpcId':feed.VPC_ID,'PrivateIpAddress':{'IpAddress':[]}}}
                    result=self.take(fixture,known_ids=('i-0','i-1'))
                    self.assertEqual(result['memberStates'][-1],{'id':'i-2','lifecycle':lifecycle,'health':'Healthy','addressKnown':False})
                    self.assertEqual(result['healthyRegistered'],['i-0','i-1'])
                    self.assertNotIn('i-2',result['privateAddresses'])

    def test_pending_private_target_is_published_before_nlb_registration(self):
        for lifecycle in ('Pending', 'Pending:Wait'):
            with self.subTest(lifecycle=lifecycle):
                fixture=self.fixture(3)
                fixture[0][2]['ScalingInstances']['ScalingInstance'][0]['LifecycleState']=lifecycle
                fixture[3]['Servers'].pop()
                events=[]
                snapshot=dict(self.take(fixture,known_ids=('i-0','i-1')),
                              feedSession='12345678-1234-4234-8234-123456789abc',sequence=1)
                self.assertEqual([row['id'] for row in snapshot['backendStates']],['i-0','i-1'])
                self.assertEqual(snapshot['privateAddresses']['i-2'],'10.0.0.3')
                feed.publish(snapshot,(),lambda value:events.append(('inventory',value)),
                             lambda targets:events.append(('targets',targets)))
                self.assertEqual([event[0] for event in events],['targets','inventory'])
                self.assertIn(('i-2','10.0.0.3'),events[0][1])
                self.assertEqual(events[1][1]['healthyRegistered'],['i-0','i-1'])

    def test_every_documented_transition_state_is_preserved(self):
        for lifecycle in ('InService','Pending','Pending:Wait','Protected','Standby','Stopped','Removing','Removing:Wait'):
            for health in ('Healthy','Unhealthy'):
                fixture=self.fixture()
                fixture[0][0]['ScalingInstances']['ScalingInstance'][0].update(LifecycleState=lifecycle,HealthStatus=health)
                result=self.take(fixture)
                self.assertEqual(result['memberStates'][0],{'id':'i-0','lifecycle':lifecycle,'health':health,'addressKnown':True})
        for status in ('Adding','Configuring','Removing'):
            fixture=self.fixture();fixture[3]['Servers'][0]['Status']=status
            result=self.take(fixture)
            self.assertEqual(result['backendStates'][0],{'id':'i-0','status':status,'weight':100,'listenerHealth':'Normal'})
            self.assertEqual(result['healthyRegistered'],['i-1'])
        for health in ('Initial','Unhealthy','Unavailable'):
            fixture=self.fixture();fixture[3]['Health']['ListenerHealthStatus'][0]['ServerGroupInfos'][0]['NonNormalServers']=[{'ServerId':'i-0','Port':6432,'Status':health}]
            self.assertEqual(self.take(fixture)['backendStates'][0]['listenerHealth'],health)
        fixture=self.fixture();fixture[3]['Servers'][0]['Weight']=0
        self.assertEqual(self.take(fixture)['healthyRegistered'],['i-1'])

    def test_departed_backend_needs_exact_prior_ess_identity_and_decrease(self):
        fixture=self.fixture(4)
        fixture[0].pop();fixture[1]['DesiredCapacity']=3
        for page in fixture[0]:page['TotalCount']=3
        for known,allowance,valid in ((('i-0','i-1','i-2','i-3'),1,True),
                                      (('i-0','i-1','i-2','i-3'),0,False),
                                      (('i-0','i-1','i-2'),1,False)):
            with self.subTest(known=known,allowance=allowance):
                if valid:
                    result=self.take(fixture,known_ids=known,allowed_departures=allowance)
                    self.assertEqual([row['id'] for row in result['backendStates']],['i-0','i-1','i-2','i-3'])
                    self.assertEqual(result['healthyRegistered'],['i-0','i-1','i-2'])
                else:
                    with self.assertRaises(ValueError):self.take(fixture,known_ids=known,allowed_departures=allowance)
        fixture[0].pop();fixture[1]['DesiredCapacity']=2
        for page in fixture[0]:page['TotalCount']=2
        with self.assertRaises(ValueError):self.take(fixture,known_ids=('i-0','i-1','i-2','i-3'),allowed_departures=1)
        self.assertEqual(len(self.take(fixture,known_ids=('i-0','i-1','i-2','i-3'),allowed_departures=2)['backendStates']),4)

    def test_backend_and_previously_known_pending_member_require_ecs_address(self):
        for status in ('Available','Adding','Configuring','Removing'):
            fixture=self.fixture();fixture[3]['Servers'][0]['Status']=status;fixture[2].pop('i-0')
            with self.subTest(status=status),self.assertRaises(ValueError):self.take(fixture)
        fixture=self.fixture(3);fixture[3]['Servers'].pop();fixture[2].pop('i-2')
        fixture[0][2]['ScalingInstances']['ScalingInstance'][0]['LifecycleState']='Pending'
        with self.assertRaises(ValueError):self.take(fixture,known_ids=('i-2',),required_address_ids=('i-2',))
        fixture[0][2]['ScalingInstances']['ScalingInstance'][0]['LifecycleState']='Protected'
        with self.assertRaises(ValueError):self.take(fixture)

    def test_transition_pagination_is_complete_and_bounded(self):
        for kind in ('count','page','duplicate','foreign','truncated','overflow'):
            pages=self.fixture()[0]
            if kind=='count':pages[1]['TotalCount']=3
            if kind=='page':pages[1]['PageNumber']=1
            if kind=='duplicate':pages[1]['ScalingInstances']['ScalingInstance'][0]['InstanceId']='i-0'
            if kind=='foreign':pages[1]['ScalingInstances']['ScalingInstance'][0]['ScalingGroupId']='other'
            if kind=='truncated':pages[1]['ScalingInstances']['ScalingInstance']=[]
            if kind=='overflow':pages[0]['TotalCount']=5
            with self.subTest(kind=kind),self.assertRaises(ValueError):feed.collect_transition_members(lambda p:pages[p-1],'asg')

    def test_retained_targets_survive_backend_absence_until_explicit_cleanup(self):
        fixture=self.fixture();writes=[];updates=[]
        value=dict(self.take(fixture),feedSession='12345678-1234-4234-8234-123456789abc',sequence=1)
        old=(('i-old','10.0.0.9'),)
        retained=feed.publish(value,(),writes.append,updates.append,retain_targets=old)
        self.assertEqual(retained,(('i-0','10.0.0.1'),('i-1','10.0.0.2'),('i-old','10.0.0.9')))
        final=feed.publish(value,retained,writes.append,updates.append)
        self.assertEqual(final,(('i-0','10.0.0.1'),('i-1','10.0.0.2')))
        self.assertEqual(set(writes[0]),{'schemaVersion','feedSession','sequence','observedAt','desired','members','memberStates','backendStates','healthyRegistered'})
        self.assertNotIn('10.0.0.',__import__('json').dumps(writes))
        with self.assertRaises(ValueError):feed.publish(value,retained,writes.append,updates.append,retain_targets=(('i-0','10.0.0.9'),))

    def operator_fixture(self):
        from types import SimpleNamespace
        op=feed.Operator(SimpleNamespace());fixture=self.fixture(4)
        fixture[1]['ScalingGroupId']=feed.GROUP_ID
        for page in fixture[0]:page['ScalingInstances']['ScalingInstance'][0]['ScalingGroupId']=feed.GROUP_ID
        for row in fixture[3]['Servers']:row['ServerGroupId']=feed.SERVER_GROUP_ID
        fixture[3]['Listener']['ServerGroupId']=feed.SERVER_GROUP_ID
        fixture[3]['Health']['ListenerHealthStatus'][0]['ServerGroupInfos'][0]['ServerGroupId']=feed.SERVER_GROUP_ID
        op.fetch_ess=lambda page:fixture[0][page-1]
        op.fetch_group=lambda:fixture[1]
        op.fetch_ecs=lambda ids:[fixture[2][node] for node in ids if node in fixture[2]]
        op.fetch_nlb=lambda:fixture[3]
        return op,fixture

    def test_operator_session_sequence_and_delayed_departures(self):
        import uuid
        op,fixture=self.operator_fixture();writes=[];updates=[]
        op.atomic_worker_write=writes.append;op.update_targets=updates.append
        op.write(op.read())
        fixture[1]['DesiredCapacity']=3
        op.write(op.read())  # Desired decrease precedes the eventual ESS departure.
        fixture[0].pop()
        for page in fixture[0]:page['TotalCount']=3
        op.write(op.read());op.write(op.read())
        fixture[3]['Servers'].pop();fixture[2].pop('i-3')
        op.write(op.read())
        fixture[1]['DesiredCapacity']=2
        op.write(op.read())
        fixture[0].pop()
        for page in fixture[0]:page['TotalCount']=2
        op.write(op.read())
        self.assertEqual([row['sequence'] for row in writes],[1,2,3,4,5,6,7])
        sessions={row['feedSession'] for row in writes}
        self.assertEqual(len(sessions),1)
        self.assertEqual(str(uuid.UUID(next(iter(sessions)))),next(iter(sessions)))
        self.assertNotEqual(feed.Operator(op.args).feedSession,next(iter(sessions)))
        self.assertEqual(len(op.previous),4)
        self.assertEqual(op.known_ids,{'i-0','i-1','i-2','i-3'})
        self.assertEqual(op.allowed_departures,2)
        # A failed publication cannot advance the retained session or history.
        def fail(value):raise RuntimeError('private failure')
        op.atomic_worker_write=fail
        with self.assertRaises(ValueError):op.write(op.read())
        self.assertEqual(op.sequence,7)

    def test_addressless_pending_survives_fresh_ticks_but_known_address_cannot_disappear(self):
        op,fixture=self.operator_fixture();writes=[]
        op.atomic_worker_write=writes.append;op.update_targets=lambda value:None
        fixture[3]['Servers'].pop()
        fixture[0][3]['ScalingInstances']['ScalingInstance'][0]['LifecycleState']='Pending'
        fixture[2].pop('i-3')
        op.write(op.read());op.write(op.read())
        self.assertFalse(writes[-1]['memberStates'][-1]['addressKnown'])
        fixture[2]['i-3']={'InstanceId':'i-3','VpcAttributes':{'VpcId':feed.VPC_ID,'PrivateIpAddress':{'IpAddress':['10.0.0.4']}}}
        op.write(op.read())
        fixture[2].pop('i-3')
        with self.assertRaises(ValueError):op.read()
        self.assertEqual(op.sequence,3)

    def test_operator_rejects_unexpected_departure_without_decrease(self):
        op,fixture=self.operator_fixture();op.atomic_worker_write=lambda value:None;op.update_targets=lambda value:None
        op.write(op.read());fixture[0].pop()
        for page in fixture[0]:page['TotalCount']=3
        with self.assertRaises(ValueError):op.read()

    def test_ecs_adapter_accepts_complete_partial_result_for_pending_identity(self):
        from types import SimpleNamespace
        op=feed.Operator(SimpleNamespace())
        op.aliyun=lambda *args,**kwargs:{'TotalCount':1,'PageNumber':1,'Instances':{'Instance':[{'InstanceId':'i-0'}]}}
        self.assertEqual(op.fetch_ecs(['i-0','i-pending']),[{'InstanceId':'i-0'}])
        op.aliyun=lambda *args,**kwargs:{'TotalCount':2,'PageNumber':1,'Instances':{'Instance':[{'InstanceId':'i-0'}]}}
        with self.assertRaises(ValueError):op.fetch_ecs(['i-0','i-pending'])

    def test_changed_targets_require_exact_readback_before_publication(self):
        import json,subprocess
        from types import SimpleNamespace
        for changed in (False,True):
            with self.subTest(changed=changed),tempfile.TemporaryDirectory() as directory:
                kube=pathlib.Path(directory)/'config';kube.write_text('private');kube.chmod(0o600)
                targets=[{'targets':['10.0.0.'+str(n+1)+':9930'],'labels':{'env':'rdev.ali','db_proxy_code':'DP00001','target_db_code':'D00002','ecs_instance_id':'i-'+str(n)}} for n in range(2)]
                body={'metadata':{'name':'pgcat-provider-inventory','namespace':'raptor-test','resourceVersion':'17'},'data':{'targets.json':'[]'}}
                # Exercise the actual embedded remote script with a fake kubectl boundary.
                preamble=('import subprocess,json,time\n_calls=[]\n'+
                    'def fixture_run(argv,**kwargs):\n'+
                    ' _calls.append(argv)\n'+
                    ' if argv[5]=="patch":\n'+
                    '  patch=json.loads(argv[-1]); assert patch["metadata"]["resourceVersion"]=="17"\n'+
                    '  return type("Result",(),{"stdout":""})()\n'+
                    ' body='+repr(body)+'\n'+
                    ' if len(_calls)>2: body["data"]["targets.json"]='+repr(json.dumps([] if changed else targets))+'\n'+
                    ' return type("Result",(),{"stdout":json.dumps(body)})()\n'+
                    'subprocess.run=fixture_run\n_remote_deadline=time.monotonic()+15\n')
                op=feed.Operator(SimpleNamespace(kubeconfig=str(kube)));writes=[]
                op.atomic_worker_write=writes.append
                op.remote=lambda script,*args:subprocess.run(['python3','-c',preamble+script,*args],capture_output=True,text=True,check=True).stdout
                value=self.take(self.fixture())
                if changed:
                    with self.assertRaises(ValueError):op.write(value)
                else:
                    op.write(value)
                    self.assertEqual(len(writes),1)
                if changed:
                    self.assertEqual(writes,[])
                    self.assertEqual(op.sequence,0)
                    self.assertEqual(op.known_ids,set())

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

    def test_unknown_ess_state_has_distinct_sanitized_category(self):
        fixture=self.fixture()
        fixture[0][0]['ScalingInstances']['ScalingInstance'][0]['LifecycleState']='Unknown'
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
        with tempfile.TemporaryDirectory() as directory:
            argv=['provider_feed.py','--interval','16','--workbench','/private/workbench',
                  '--worker-inventory','/private/run/provider-inventory.json','--kubeconfig','/private/kubeconfig',
                  '--run-context','/private/lane/run-context.json','--contract-dir','/private/contract',
                  '--status-file',str(pathlib.Path(directory)/'status.json')]
            with patch.object(sys,'argv',argv), patch.object(feed.Operator,'existing_targets',return_value=()), \
                    patch.object(feed,'validate_run_context',return_value={}), \
                    contextlib.redirect_stderr(output), self.assertRaises(SystemExit):
                feed.main()
        self.assertIn('stopped at loop',output.getvalue())
        self.assertNotIn('targets-read',output.getvalue())

    def test_publish_allowlist_and_changed_targets(self):
        writes=[];updates=[];s=dict(self.take(self.fixture()),feedSession='12345678-1234-4234-8234-123456789abc',sequence=1)
        previous=feed.publish(s,(),writes.append,updates.append)
        feed.publish(s,previous,writes.append,updates.append)
        self.assertEqual(len(updates),1)
        self.assertEqual(set(writes[0]),{'schemaVersion','feedSession','sequence','observedAt','desired','members','memberStates','backendStates','healthyRegistered'})
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

class NoScaleFeedTests(unittest.TestCase):
    def setUp(self):
        from unittest.mock import patch
        admission=patch('pgcat_lane_protocol._release_admission');admission.start();self.addCleanup(admission.stop)
        guard=patch.object(feed,'_require_verified');guard.start();self.addCleanup(guard.stop)
        wrapper=patch.object(feed,'_wrapper_command',side_effect=lambda path,args:[path]+args)
        wrapper.start();self.addCleanup(wrapper.stop)

    fixture = FeedTests.fixture
    take = FeedTests.take
    def operator(self, root, **changes):
        from types import SimpleNamespace
        from unittest.mock import patch
        from pgcat_lane_protocol import encode
        target=dict(accountId='account',regionId=feed.REGION,workerId=feed.WORKER_ID,
            scalingGroupId=feed.GROUP_ID,nlbServerGroupId=feed.SERVER_GROUP_ID,
            originalInstanceIds=['i-0','i-1'])
        context=dict(runId=str(uuid.uuid4()),targetSha256=hashlib.sha256(encode(target)).hexdigest())
        args=dict(mode='disabled-rehearsal',status_file=str(root/'status.json'),
            run_context=str(root/'lane'/'run-context.json'),contract_dir='/fresh/contract',
            worker_inventory='/fresh/run/provider-inventory.json',no_scale_target=json.dumps(target),
            interval=15,max_seconds=180)
        args.update(changes)
        with patch.object(feed,'validate_run_context',return_value=context):
            op=feed.Operator(SimpleNamespace(**args))
        op.verify_run_context=lambda:None
        op.atomic_worker_write=lambda *a:None
        op.update_targets=lambda *a:None
        op.previous=tuple(sorted(self.take(self.fixture())['privateAddresses'].items()))
        return op,context

    def intent(self, op, context, **changes):
        from pgcat_lane_supervisor import save_once
        value=dict(schemaVersion=1,runId=context['runId'],feedSession=op.feedSession,
            minimumSequence=op.sequence,verdictSha256='b'*64)
        value.update(changes)
        save_once(op.status_file.parent,'no-scale-stop.intent',value,atomic=True)
        return value

    def sealed_context(self, root, mode):
        from pgcat_lane_protocol import Journal,encode
        from pgcat_lane_supervisor import save_once
        lane=root/'lane';lane.mkdir(mode=0o700)
        active=root/'global';active.mkdir(mode=0o700)
        target=dict(accountId='account',regionId=feed.REGION,workerId=feed.WORKER_ID,
            scalingGroupId=feed.GROUP_ID,nlbServerGroupId=feed.SERVER_GROUP_ID,
            originalInstanceIds=['i-0','i-1'])
        hashes={'providerFeed':'a'*64};bundle=hashlib.sha256(encode(hashes)).hexdigest()
        run_id=str(uuid.uuid4());supervisor_id=str(uuid.uuid4());nonce=str(uuid.uuid4())
        from unittest.mock import patch
        with patch('pgcat_lane_protocol._release_admission'):
            Journal(lane).create_run(run_id,mode,target,dict(hashes,bundleSha256=bundle))
        context=dict(schemaVersion=1,runId=run_id,supervisorId=supervisor_id,nonce=nonce,
            targetSha256=hashlib.sha256(encode(target)).hexdigest(),bundleSha256=bundle)
        save_once(lane,'run-context.json',context)
        save_once(lane,'supervisor-startup.json',dict(runId=run_id,supervisorId=supervisor_id,contextNonce=nonce))
        save_once(active,'active-run',dict(runId=run_id,supervisorId=supervisor_id,laneRoot=str(lane)))
        return active,lane,target

    def test_direct_no_scale_feed_requires_target_before_any_provider_operation(self):
        import contextlib,io,sys
        from unittest.mock import patch
        import pgcat_lane_supervisor as supervisor
        for mode,include_target,refuse in (('no-scale',False,True),
                ('no-scale',True,False),('disabled-rehearsal',False,False),
                ('disabled-rehearsal',True,True)):
            with self.subTest(mode=mode,include_target=include_target),tempfile.TemporaryDirectory() as d:
                root=pathlib.Path(d);root.chmod(0o700)
                active,lane,target=self.sealed_context(root,mode)
                argv=['provider_feed.py','--mode','disabled-rehearsal','--interval','15','--max-seconds','180',
                    '--workbench','offline-workbench','--worker-inventory','/fresh/run/provider-inventory.json',
                    '--run-context',str(lane/'run-context.json'),'--contract-dir','/fresh/contract',
                    '--status-file',str(root/'status.json'),'--kubeconfig','/fresh/kubeconfig']
                if include_target:argv+=['--no-scale-target',json.dumps(target)]
                with patch.object(sys,'argv',argv),patch.object(supervisor,'GLOBAL_ROOT',active), \
                        patch.object(feed.Operator,'existing_targets',return_value=()) as existing, \
                        patch.object(feed.Operator,'read') as read,patch.object(feed.Operator,'update_targets') as update, \
                        patch.object(feed,'run_loop') as loop,contextlib.redirect_stderr(io.StringIO()):
                    if refuse:
                        with self.assertRaises(SystemExit) as stopped:feed.main()
                        self.assertEqual(stopped.exception.code,1)
                        existing.assert_not_called();read.assert_not_called();update.assert_not_called();loop.assert_not_called()
                    else:
                        existing.return_value=(('i-0','10.0.0.1'),('i-1','10.0.0.2'))
                        feed.main();existing.assert_called_once();loop.assert_called_once()

    def test_stop_is_terminal_and_no_scale_targets_can_never_be_patched(self):
        with tempfile.TemporaryDirectory() as d:
            op,context=self.operator(pathlib.Path(d));updates=[]
            op.update_targets=lambda *args:updates.append(args)
            op.write(self.take(self.fixture()))
            self.assertEqual(updates,[])
            self.intent(op,context);self.assertTrue(op.stop_no_scale())
            with self.assertRaises(ValueError):op.write(self.take(self.fixture()))
        with tempfile.TemporaryDirectory() as d:
            op,_=self.operator(pathlib.Path(d));updates=[]
            op.update_targets=lambda *args:updates.append(args)
            value=self.take(self.fixture());value['privateAddresses']['i-0']='10.0.1.1'
            with self.assertRaises(ValueError):op.write(value)
            self.assertEqual(updates,[])

    def test_targets_configmap_address_drift_is_refused_before_mutation(self):
        for mutate in ('initial-id','initial-address','later-address'):
            with self.subTest(mutate=mutate),tempfile.TemporaryDirectory() as d:
                op,_=self.operator(pathlib.Path(d));writes=[];updates=[]
                op.atomic_worker_write=lambda *args:writes.append(args)
                op.update_targets=lambda *args:updates.append(args)
                if mutate=='later-address':op.write(self.take(self.fixture()))
                before=len(writes)
                if mutate=='initial-id':op.previous=(('another','10.0.0.1'),('i-1','10.0.0.2'))
                if mutate=='initial-address':op.previous=(('i-0','10.0.1.1'),('i-1','10.0.0.2'))
                value=self.take(self.fixture())
                if mutate=='later-address':value['privateAddresses']['i-0']='10.0.1.1'
                with self.assertRaises(ValueError):op.write(value)
                self.assertEqual(len(writes),before);self.assertEqual(updates,[])

    def test_well_formed_different_target_hash_is_rejected(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as d:
            with patch.object(feed,'validate_run_context',return_value=dict(runId=str(uuid.uuid4()),targetSha256='0'*64)):
                # operator() has its own context patch, so build first and reuse its validated arguments.
                op,_=self.operator(pathlib.Path(d))
                with self.assertRaises(ValueError):feed.Operator(op.args)

    def test_target_hash_and_fixed_window_refuse_before_provider_read(self):
        for change in (dict(no_scale_target='{}'),dict(interval=14),dict(max_seconds=181),
                       dict(mode='full-replacement')):
            with self.subTest(change=change),tempfile.TemporaryDirectory() as d:
                with self.assertRaises(ValueError):
                    self.operator(pathlib.Path(d),**change)

    def test_successor_after_worker_credential_deletion_uses_only_inventory(self):
        with tempfile.TemporaryDirectory() as d:
            root=pathlib.Path(d);op,_=self.operator(root,kubeconfig=str(root/'kubeconfig'))
            self.assertFalse(pathlib.Path(op.args.kubeconfig).exists())
            # Exercise the production publication command builder. Its remote
            # stdlib writer gets inventory/context arguments, never kubeconfig.
            op.args.worker_inventory=str(root/'provider-inventory.json')
            calls=[]
            op.atomic_worker_write=feed.Operator.atomic_worker_write.__get__(op)
            op.remote=lambda script,*args:calls.append((script,args))
            op.update_targets=lambda *args:self.fail('no-scale target update')
            value=self.take(self.fixture());op.write(value)
            self.assertEqual(op.sequence,1);self.assertEqual(len(calls),1)
            self.assertNotIn(op.args.kubeconfig,calls[0][1])
            self.assertNotIn('kubectl',calls[0][0])
            value['privateAddresses']['i-0']='10.0.0.99'
            with self.assertRaises(ValueError):op.write(value)
            self.assertEqual(len(calls),1)

    def test_exact_two_every_publication(self):
        for mutation in ('desired','third','protection','health','weight','backend','roster'):
            with self.subTest(mutation=mutation),tempfile.TemporaryDirectory() as d:
                op,_=self.operator(pathlib.Path(d)); writes=[]
                op.atomic_worker_write=lambda *args:writes.append(args)
                value=self.take(self.fixture())
                if mutation=='desired':value['desired']=3
                elif mutation=='third':value=self.take(self.fixture(3))
                elif mutation=='protection':value['memberStates'][0]['lifecycle']='InService'
                elif mutation=='health':value['memberStates'][0]['health']='Unhealthy'
                elif mutation=='weight':value['backendStates'][0]['weight']=99
                elif mutation=='backend':value['backendStates'][0]['status']='Removing'
                else:value['members']=['different','i-1']
                with self.assertRaises(ValueError):op.write(value)
                self.assertEqual(writes,[])
                self.assertFalse(op.status_file.exists())

    def test_changed_fleet_read_stops_before_publication(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as d:
            op,_=self.operator(pathlib.Path(d));op.fetch_group=lambda:dict(DesiredCapacity=3)
            with patch.object(feed,'snapshot',return_value=self.take(self.fixture(3))):
                with self.assertRaises(ValueError):op.read()
            self.assertFalse(op.status_file.exists())

    def test_no_scale_cannot_reuse_stop_control_files(self):
        for name in ('no-scale-stop.intent','no-scale-stop.ack'):
            with self.subTest(name=name),tempfile.TemporaryDirectory() as d:
                root=pathlib.Path(d);(root/name).write_text('{}')
                with self.assertRaises(ValueError):self.operator(root)

    def test_stop_before_read_and_in_tick_publish_at_most_once(self):
        for in_tick in (False,True):
            with self.subTest(in_tick=in_tick),tempfile.TemporaryDirectory() as d:
                op,context=self.operator(pathlib.Path(d)); op.write(self.take(self.fixture()))
                calls=[]; clock=[0.]
                if not in_tick:self.intent(op,context)
                def read(**kwargs):
                    calls.append('read'); self.intent(op,context)
                    return self.take(self.fixture())
                feed.run_loop(read,op.write,interval=15,max_seconds=180,
                    monotonic=lambda:clock[0],sleep=lambda n:clock.__setitem__(0,clock[0]+n),
                    stop=op.stop_no_scale)
                self.assertEqual(len(calls),int(in_tick))
                self.assertEqual(op.sequence,1+int(in_tick))
                ack=json.loads((op.status_file.parent/'no-scale-stop.ack').read_text())
                self.assertEqual(ack['sequence'],op.sequence)
                self.assertTrue(op.stop_no_scale())
                changed=json.loads((op.status_file.parent/'no-scale-stop.intent').read_text())
                changed['verdictSha256']='c'*64
                feed.atomic_write(op.status_file.parent/'no-scale-stop.intent',changed)
                with self.assertRaises(ValueError):op.stop_no_scale()

    def test_wrong_or_unsafe_intent_cannot_acknowledge(self):
        for mutation in ('session','run','future','mode','symlink','extra'):
            with self.subTest(mutation=mutation),tempfile.TemporaryDirectory() as d:
                op,context=self.operator(pathlib.Path(d)); op.write(self.take(self.fixture()))
                changes={}
                if mutation=='session':changes['feedSession']=str(uuid.uuid4())
                if mutation=='run':changes['runId']=str(uuid.uuid4())
                if mutation=='future':changes['minimumSequence']=op.sequence+1
                if mutation=='extra':changes['command']='arbitrary'
                self.intent(op,context,**changes)
                path=op.status_file.parent/'no-scale-stop.intent'
                if mutation=='mode':path.chmod(0o644)
                if mutation=='symlink':path.rename(path.with_name('other'));path.symlink_to(path.with_name('other'))
                with self.assertRaises(ValueError):op.stop_no_scale()
                self.assertFalse((path.parent/'no-scale-stop.ack').exists())

    def test_stop_during_sleep_does_not_reuse_expired_publication_deadline(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as d:
            op,context=self.operator(pathlib.Path(d));clock=[0.];ticks=[]
            op.verify_run_context=feed.Operator.verify_run_context.__get__(op)
            def read(**kwargs):ticks.append(clock[0]);return self.take(self.fixture())
            def sleep(seconds):
                self.intent(op,context)
                clock[0]+=seconds
            with patch.object(feed,'validate_run_context',return_value=context):
                feed.run_loop(read,op.write,interval=15,max_seconds=180,
                    monotonic=lambda:clock[0],sleep=sleep,stop=op.stop_no_scale)
            self.assertEqual(ticks,[0.])
            self.assertTrue((op.status_file.parent/'no-scale-stop.ack').exists())

    def test_stop_in_final_sleep_acknowledges_without_another_provider_read(self):
        with tempfile.TemporaryDirectory() as d:
            op,context=self.operator(pathlib.Path(d));clock=[0.];ticks=[]
            def read(**kwargs):
                ticks.append(clock[0]);value=self.take(self.fixture());value['observedAt']=clock[0]
                return value
            def sleep(seconds):
                if clock[0] == 165.:
                    self.intent(op,context)
                clock[0]+=seconds
            feed.run_loop(read,op.write,interval=15,max_seconds=180,
                monotonic=lambda:clock[0],sleep=sleep,stop=op.stop_no_scale)
            self.assertEqual(clock[0],180.)
            self.assertEqual(ticks,list(range(0,180,15)))
            ack=json.loads((op.status_file.parent/'no-scale-stop.ack').read_text())
            final=json.loads(op.status_file.read_text())
            self.assertEqual(ack['sequence'],12)
            self.assertEqual(final['observedAt'],165.)
            self.assertEqual(final['sequence'],12)

    def test_natural_cap_has_no_ack_and_preserves_15_second_ticks(self):
        with tempfile.TemporaryDirectory() as d:
            op,_=self.operator(pathlib.Path(d));clock=[0.];ticks=[]
            def read(**kwargs):ticks.append(clock[0]);return self.take(self.fixture())
            feed.run_loop(read,op.write,interval=15,max_seconds=180,
                monotonic=lambda:clock[0],sleep=lambda n:clock.__setitem__(0,clock[0]+n),
                stop=op.stop_no_scale)
            self.assertEqual(ticks,list(range(0,180,15)))
            self.assertFalse((op.status_file.parent/'no-scale-stop.ack').exists())

if __name__=='__main__':unittest.main()
