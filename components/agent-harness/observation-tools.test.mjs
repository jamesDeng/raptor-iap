import test from 'node:test';import assert from 'node:assert/strict';
import {createObservationTools} from './observation-tools.mjs';
const binding={requestId:'request',definitionSha256:'a'.repeat(64),envCode:'rdev.ali',clusterId:'cluster'};
const config=()=>({version:1,requestId:binding.requestId,definitionSha256:binding.definitionSha256,envCode:'rdev.ali',accountId:'1360282071200743',region:'ap-southeast-1',groupId:'group',serverGroupId:'backend',dbInstanceId:'db',application:{clusterId:'cluster',namespace:'raptor-test',name:'client',uid:'uid'},clientTargets:['10.70.1.20:9090'],prometheusURL:'http://10.70.1.10:9090',expiresAt:new Date(Date.now()+600000).toISOString(),private:{aliyunEnv:{ALIBABA_CLOUD_ACCESS_KEY_ID:'fixture-key',ALIBABA_CLOUD_ACCESS_KEY_SECRET:'fixture-secret',ALIBABA_CLOUD_SECURITY_TOKEN:'fixture-token'},kubeconfig:'/tmp/private/kubeconfig',sql:{user:'fixtureuser',password:'fixture-password',database:'test',sslmode:'disable'}}});
const clientRead=async(_command,args)=>{const kind=args[args.indexOf('get')+1];const metadata={namespace:'raptor-test',name:'client',uid:'uid'};if(kind==='deployment')return {metadata,spec:{replicas:1,selector:{matchLabels:{app:'client'}}}};if(kind==='replicasets')return {items:[{metadata:{namespace:'raptor-test',name:'client-rs',uid:'rs',ownerReferences:[{kind:'Deployment',name:'client',uid:'uid',controller:true}]}}]};return {items:[{metadata:{namespace:'raptor-test',name:'client-pod',uid:'pod',labels:{app:'client'},ownerReferences:[{kind:'ReplicaSet',name:'client-rs',uid:'rs',controller:true}]},status:{phase:'Running',podIP:'10.70.1.20',conditions:[{type:'Ready',status:'True'}]}}]};};
test('observer commands are fixed reads and results exclude raw workload credentials',async()=>{
 const calls=[];const tools=createObservationTools({binding,config:config(),exec:async(command,args)=>{calls.push([command,args]);if(command==='aliyun'&&args.includes('GetCallerIdentity'))return {AccountId:'1360282071200743',Arn:'acs:ram::1360282071200743:assumed-role/raptor-rdev-agent-observer/session'};if(command==='aliyun')return {ScalingInstances:[{InstanceId:'i-one',ScalingGroupId:'group',ProtectedFromScaleIn:true,LifecycleState:'InService',HealthStatus:'Healthy'}]};return {metadata:{uid:'uid',namespace:'raptor-test',name:'client'},spec:{replicas:2,template:{spec:{containers:[{env:[{value:'fixture-password'}]}]}}},status:{readyReplicas:2}};}});
 const fleet=await tools.cloudRead({kind:'fleet'});assert.equal(fleet.instances[0].instanceId,'i-one');const deployment=await tools.deploymentRead({});assert.equal(deployment.readyReplicas,2);assert.equal(JSON.stringify(deployment).includes('fixture-password'),false);
 assert.ok(calls.every(([command,args])=>!args.includes('delete')&&!args.includes('patch')&&!args.includes('secrets')));await assert.rejects(tools.cloudRead({kind:'DeleteInstances'}),/InvalidObservation/);
});
test('wrong identity expired credentials and unsafe endpoints fail closed',async()=>{
 for(const change of ['request','cluster','expiry','endpoint']){const c=config();if(change==='request')c.requestId='other';if(change==='cluster')c.application.clusterId='other';if(change==='expiry')c.expiresAt=new Date(0).toISOString();if(change==='endpoint')c.prometheusURL='http://evil.example';assert.throws(()=>createObservationTools({binding,config:c}),/InvalidObservation/);}
 const tools=createObservationTools({binding,config:config(),exec:async()=>({metadata:{uid:'foreign',namespace:'raptor-test',name:'client'}})});await assert.rejects(tools.deploymentRead({}),/InvalidObservation/);
});
test('metrics refuse stale or down client scrapes before reading counters',async()=>{
 const tools=createObservationTools({binding,config:config(),exec:clientRead,fetch:async()=>({ok:true,json:async()=>({status:'success',data:{resultType:'vector',result:[{metric:{job:'test-client',instance:'10.70.1.20:9090'},value:[Date.now()/1000,'0']}]}})})});
 await assert.rejects(tools.metricsRead({kind:'connections'}),/InvalidObservation/);
});
test('metrics queries are fixed and reject stale samples',async()=>{
 let url;const tools=createObservationTools({binding,config:config(),exec:clientRead,fetch:async value=>{url=new URL(value);const q=url.searchParams.get('query'),now=Date.now()/1000;const rows=q.startsWith('up{')?[{metric:{job:'test-client',instance:'10.70.1.20:9090'},value:[now,'1']}]:['failure','timeout','ambiguous'].map(outcome=>({metric:{job:'test-client',instance:'10.70.1.20:9090',outcome},value:[now,q.startsWith('timestamp(')?String(now):'0']}));return {ok:true,json:async()=>({status:'success',data:{resultType:'vector',result:rows}})};}});
 const result=await tools.metricsRead({kind:'trafficFailures'});assert.equal(result.samples.length,3);assert.equal(result.samples[0].value,0);assert.equal(url.hostname,'10.70.1.10');assert.match(url.searchParams.get('query'),/failure/);await assert.rejects(tools.metricsRead({kind:'arbitrary',query:'up'}),/InvalidObservation/);
 const stale=createObservationTools({binding,config:config(),exec:clientRead,fetch:async()=>({ok:true,json:async()=>({status:'success',data:{resultType:'vector',result:[{metric:{},value:[0,'0']}]}})})});await assert.rejects(stale.metricsRead({kind:'trafficFailures'}),/InvalidObservation/);
});
test('credentials expire between calls and truncated fleets never look complete',async()=>{
 const c=config();const tools=createObservationTools({binding,config:c,exec:async(_command,args)=>args.includes('GetCallerIdentity')?{AccountId:'1360282071200743',Arn:'acs:ram::1360282071200743:assumed-role/raptor-rdev-agent-observer/session'}:{TotalCount:51,ScalingInstances:[]}});await assert.rejects(tools.cloudRead({kind:'fleet'}),/InvalidObservation/);c.expiresAt=new Date(0).toISOString();await assert.rejects(tools.deploymentRead({}),/InvalidObservation/);
});
test('SQL probe uses fixed read-only query and never places password in argv',async()=>{
 const c=config();c.listenerId='listener';let call;const tools=createObservationTools({binding,config:c,exec:async(command,args,env)=>{if(command==='psql'){call={command,args,env};return {connected:true};}return observerCloudFixture(c,args);}});
 assert.deepEqual(await tools.dbConnectionProbe({instanceId:'i-one'}),{instanceId:'i-one',connected:true});assert.equal(call.command,'psql');assert.ok(call.args.includes('SELECT 1'));assert.equal(call.env.PGPASSWORD,'fixture-password');assert.equal(call.args.join(' ').includes('fixture-password'),false);assert.match(call.env.PGOPTIONS,/default_transaction_read_only=on/);await assert.rejects(tools.dbConnectionProbe({instanceId:'other'}),/InvalidObservation/);
});
test('oversized Prometheus responses fail without exporting data',async()=>{
 const tools=createObservationTools({binding,config:config(),exec:clientRead,fetch:async()=>new Response(JSON.stringify({status:'success',padding:' '.repeat(300000),data:{resultType:'vector',result:[{metric:{job:'test-client',instance:'10.70.1.20:9090'},value:[Date.now()/1000,'0']}]}}),{headers:{'Content-Type':'application/json'}})});await assert.rejects(tools.metricsRead({kind:'connections'}),/InvalidObservation/);
});
test('observers match the controller-resolved fleet and application scope',()=>{
 const replacementScope={envCode:'rdev.ali',groupId:'group',serverGroupId:'backend',application:{clusterId:'cluster',namespace:'raptor-test',name:'client',uid:'uid'}};const c=config();c.groupId='foreign';assert.throws(()=>createObservationTools({binding,config:c,replacementScope}),/InvalidObservation/);
});

test('cloud credential identity must be the dedicated observer role',async()=>{
 const tools=createObservationTools({binding,config:config(),exec:async(_command,args)=>args.includes('GetCallerIdentity')?{AccountId:'1360282071200743',Arn:'acs:ram::1360282071200743:assumed-role/administrator/session'}:{ScalingInstances:[{InstanceId:'i-one',ScalingGroupId:'group'}]}});await assert.rejects(tools.cloudRead({kind:'fleet'}),/InvalidObservation/);
});
test('backend health requires complete exact listener and backend membership',async()=>{
 const c=config();c.listenerId='listener';const tools=createObservationTools({binding,config:c,exec:async(_command,args)=>{
 if(args.includes('GetCallerIdentity'))return {AccountId:c.accountId,Arn:`acs:ram::${c.accountId}:assumed-role/raptor-rdev-agent-observer/session`};
 if(args.includes('DescribeScalingInstances'))return {TotalCount:1,ScalingInstances:[{InstanceId:'i-one',ScalingGroupId:'group',ProtectedFromScaleIn:true,LifecycleState:'InService',HealthStatus:'Healthy'}]};
 if(args.includes('ListServerGroupServers'))return {TotalCount:1,Servers:[{ServerId:'i-one',ServerIp:'10.70.1.30',ServerGroupId:'backend',ServerType:'Ecs',Port:6432,Status:'Available',Weight:100}]};
 if(args.includes('GetListenerAttribute'))return {ListenerId:'listener',ServerGroupId:'backend',ListenerPort:6432,ListenerStatus:'Running'};
 return {ListenerHealthStatus:[{ListenerId:'listener',ListenerPort:6432,ServerGroupInfos:[{ServerGroupId:'backend',HeathCheckEnabled:true,NonNormalServers:[]}]}]};
 }});const result=await tools.cloudRead({kind:'backend'});assert.equal(result.nodes[0].healthy,true);assert.equal(result.nodes[0].instanceId,'i-one');
});

test('fleet refuses missing health, lifecycle, duplicate identities and inconsistent totals',async()=>{
 const row={InstanceId:'i-one',ScalingGroupId:'group',ProtectedFromScaleIn:true,LifecycleState:'InService',HealthStatus:'Healthy'};
 for(const result of [{ScalingInstances:[{...row,HealthStatus:undefined}]},{ScalingInstances:[{...row,LifecycleState:undefined}]},{ScalingInstances:[row,row]},{TotalCount:0,ScalingInstances:[row]}]){
 const tools=createObservationTools({binding,config:config(),exec:async(_command,args)=>args.includes('GetCallerIdentity')?{AccountId:'1360282071200743',Arn:'acs:ram::1360282071200743:assumed-role/raptor-rdev-agent-observer/session'}:result});
 await assert.rejects(tools.cloudRead({kind:'fleet'}),/InvalidObservation/);
 }
});

test('SQL probe discovers current private address from matching ESS and NLB membership',async()=>{
 const c=config();c.listenerId='listener';c.nodeHosts={'i-one':'10.70.1.99'};let sqlHost;
 const tools=createObservationTools({binding,config:c,exec:async(command,args,env)=>{
 if(command==='psql'){sqlHost=env.PGHOST;return {connected:true};}
 if(args.includes('GetCallerIdentity'))return {AccountId:c.accountId,Arn:`acs:ram::${c.accountId}:assumed-role/raptor-rdev-agent-observer/session`};
 if(args.includes('DescribeScalingInstances'))return {TotalCount:1,ScalingInstances:[{InstanceId:'i-one',ScalingGroupId:'group',ProtectedFromScaleIn:true,LifecycleState:'InService',HealthStatus:'Healthy'}]};
 if(args.includes('ListServerGroupServers'))return {TotalCount:1,Servers:[{ServerId:'i-one',ServerIp:'10.70.1.30',ServerGroupId:'backend',ServerType:'Ecs',Port:6432,Status:'Available',Weight:100}]};
 if(args.includes('GetListenerAttribute'))return {ListenerId:'listener',ServerGroupId:'backend',ListenerPort:6432,ListenerStatus:'Running'};
 return {ListenerHealthStatus:[{ListenerId:'listener',ListenerPort:6432,ServerGroupInfos:[{ServerGroupId:'backend',HeathCheckEnabled:true,NonNormalServers:[]}]}]};
 }});
 await tools.dbConnectionProbe({instanceId:'i-one'});assert.equal(sqlHost,'10.70.1.30');
 await assert.rejects(tools.dbConnectionProbe({instanceId:'foreign'}),/InvalidObservation/);
});

function observerCloudFixture(c,args){
 if(args.includes('GetCallerIdentity'))return {AccountId:c.accountId,Arn:`acs:ram::${c.accountId}:assumed-role/raptor-rdev-agent-observer/session`};
 if(args.includes('DescribeScalingInstances'))return {TotalCount:1,ScalingInstances:[{InstanceId:'i-one',ScalingGroupId:'group',ProtectedFromScaleIn:true,LifecycleState:'InService',HealthStatus:'Healthy'}]};
 if(args.includes('ListServerGroupServers'))return {TotalCount:1,Servers:[{ServerId:'i-one',ServerIp:'10.70.1.30',ServerGroupId:'backend',ServerType:'Ecs',Port:6432,Status:'Available',Weight:100}]};
 if(args.includes('GetListenerAttribute'))return {ListenerId:c.listenerId,ServerGroupId:'backend',ListenerPort:6432,ListenerStatus:'Running'};
 return {ListenerHealthStatus:[{ListenerId:c.listenerId,ListenerPort:6432,ServerGroupInfos:[{ServerGroupId:'backend',HeathCheckEnabled:true,NonNormalServers:[]}]}]};
}

function transitionFixture(c,{status='Available',pending=false,registered=true,foreign=false}={}){
 const fleet=[{InstanceId:'i-one',ScalingGroupId:'group',LifecycleState:'InService',HealthStatus:'Healthy'},...(pending?[{InstanceId:'i-pending',ScalingGroupId:'group',LifecycleState:'Pending:Wait',HealthStatus:'Healthy'}]:[])];
 return args=>{
  if(args.includes('GetCallerIdentity'))return {AccountId:c.accountId,Arn:`acs:ram::${c.accountId}:assumed-role/raptor-rdev-agent-observer/session`};
  if(args.includes('DescribeScalingInstances'))return {TotalCount:fleet.length,ScalingInstances:fleet};
  if(args.includes('ListServerGroupServers')){const servers=[{ServerId:foreign?'i-foreign':'i-one',ServerIp:'10.70.1.30',ServerGroupId:'backend',ServerType:'Ecs',Port:6432,Status:status,Weight:100}];if(pending&&registered)servers.push({ServerId:'i-pending',ServerIp:'10.70.1.31',ServerGroupId:'backend',ServerType:'Ecs',Port:6432,Status:'Adding',Weight:100});return {TotalCount:servers.length,Servers:servers};}
  if(args.includes('GetListenerAttribute'))return {ListenerId:c.listenerId,ServerGroupId:'backend',ListenerPort:6432,ListenerStatus:'Running'};
  return {ListenerHealthStatus:[{ListenerId:c.listenerId,ListenerPort:6432,ServerGroupInfos:[{ServerGroupId:'backend',HeathCheckEnabled:true,NonNormalServers:[]}]}]};
 };
}
function pgcatFetch(ids){return async url=>{const q=new URL(url).searchParams.get('query'),now=Date.now()/1000;const result=ids.map(ecs_instance_id=>({metric:{job:'pgcat',env:'rdev.ali',db_proxy_code:'proxy',target_db_code:'db',ecs_instance_id,pool:'test',user:'fixtureuser'},value:[now,q.startsWith('timestamp(')?String(now):q.startsWith('up{')?'1':'0']}));return {ok:true,json:async()=>({status:'success',data:{resultType:'vector',result}})};};}
const metricScope={envCode:'rdev.ali',proxyCode:'proxy',targetDbCode:'db',groupId:'group',serverGroupId:'backend',application:{clusterId:'cluster',namespace:'raptor-test',name:'client',uid:'uid'}};

test('documented NLB transitions remain visible but unhealthy',async()=>{
 for(const status of ['Adding','Configuring','Removing']){const c=config();c.listenerId='listener';const tools=createObservationTools({binding,config:c,exec:async(_command,args)=>transitionFixture(c,{status})(args)});const node=(await tools.cloudRead({kind:'backend'})).nodes[0];assert.equal(node.status,status);assert.equal(node.healthy,false);}
});
test('unknown NLB status and foreign backend still fail closed',async()=>{
 for(const options of [{status:'Surprise'},{foreign:true}]){const c=config();c.listenerId='listener';const tools=createObservationTools({binding,config:c,exec:async(_command,args)=>transitionFixture(c,options)(args)});await assert.rejects(tools.cloudRead({kind:'backend'}),/InvalidObservation/);}
});
test('unregistered Pending member can lack metrics and is returned explicitly',async()=>{
 const c=config();c.listenerId='listener';const tools=createObservationTools({binding,config:c,replacementScope:metricScope,exec:async(_command,args)=>transitionFixture(c,{pending:true,registered:false})(args),fetch:pgcatFetch(['i-one'])});const out=await tools.metricsRead({kind:'pgcatClients'});assert.deepEqual(out.nodes.map(n=>n.instanceId),['i-one']);assert.deepEqual(out.pendingInstanceIds,['i-pending']);
});
test('partial early scrape from unregistered Pending member does not invalidate current backend metrics',async()=>{
 const c=config();c.listenerId='listener';const base=pgcatFetch(['i-one']);const fetch=async url=>{const response=await base(url),data=await response.json(),q=new URL(url).searchParams.get('query');if(q.startsWith('up{')||q.startsWith('timestamp(up'))data.data.result.push({metric:{...data.data.result[0].metric,ecs_instance_id:'i-pending'},value:[Date.now()/1000,q.startsWith('timestamp(')?String(Date.now()/1000):'1']});return {ok:true,json:async()=>data};};
 const tools=createObservationTools({binding,config:c,replacementScope:metricScope,exec:async(_command,args)=>transitionFixture(c,{pending:true,registered:false})(args),fetch});const out=await tools.metricsRead({kind:'pgcatClients'});assert.deepEqual(out.pendingInstanceIds,['i-pending']);assert.deepEqual(out.nodes.map(n=>n.instanceId),['i-one']);
});
const pendingSampleFailures=[
  ['up-stale','up{',row=>{row.value[0]=0;}],
  ['up-malformed','up{',row=>{row.value[1]='bad';}],
  ['pool-stale','pgcat_pools_cl_active',row=>{row.value[0]=0;}],
  ['pool-malformed','pgcat_pools_cl_active',row=>{delete row.metric.pool;}],
  ['source-stale','timestamp(pgcat_pools_cl_active',row=>{row.value[1]='0';}],
  ['source-malformed','timestamp(up',row=>{row.value[1]='bad';}],
 ];
for(const [name,target,damage] of pendingSampleFailures){
 test(`present ${name} Pending sample fails closed before optional coverage is omitted`,async()=>{
  const c=config();c.listenerId='listener';const base=pgcatFetch(['i-one']);
  const fetch=async url=>{const response=await base(url),data=await response.json(),q=new URL(url).searchParams.get('query');if(q.startsWith(target)){const row={metric:{...data.data.result[0].metric,ecs_instance_id:'i-pending'},value:[...data.data.result[0].value]};damage(row);data.data.result.push(row);}return {ok:true,json:async()=>data};};
  const tools=createObservationTools({binding,config:c,replacementScope:metricScope,exec:async(_command,args)=>transitionFixture(c,{pending:true,registered:false})(args),fetch});
  await assert.rejects(tools.metricsRead({kind:'pgcatClients'}),/InvalidObservation/);
 });
}
test('registered Pending member and InService member cannot lack metrics',async()=>{
 for(const options of [{pending:true,registered:true},{pending:false,registered:true}]){const c=config();c.listenerId='listener';const fixture=transitionFixture(c,options);const exec=async(_command,args)=>{const value=fixture(args);if(!options.pending&&args.includes('DescribeScalingInstances'))return {TotalCount:2,ScalingInstances:[...value.ScalingInstances,{InstanceId:'i-two',ScalingGroupId:'group',LifecycleState:'InService',HealthStatus:'Healthy'}]};return value;};const tools=createObservationTools({binding,config:c,replacementScope:metricScope,exec,fetch:pgcatFetch(['i-one'])});await assert.rejects(tools.metricsRead({kind:'pgcatClients'}),/InvalidObservation/);}
});
test('backend appearing between fleet reads cannot be omitted from required metrics',async()=>{
 const c=config();c.listenerId='listener';let fleetReads=0;const fixture=transitionFixture(c,{pending:true,registered:true});const exec=async(_command,args)=>{const value=fixture(args);if(args.includes('DescribeScalingInstances')&&++fleetReads===1)return {TotalCount:1,ScalingInstances:value.ScalingInstances.slice(0,1)};return value;};
 const tools=createObservationTools({binding,config:c,replacementScope:metricScope,exec,fetch:pgcatFetch(['i-one'])});await assert.rejects(tools.metricsRead({kind:'pgcatClients'}),/InvalidObservation/);
});
test('known unready SQL target skips credentials, healthy SQL failure stays invalid',async()=>{
 const c=config();c.listenerId='listener';let sqlCalls=0;const tools=createObservationTools({binding,config:c,exec:async(command,args)=>{if(command==='psql'){sqlCalls++;throw new Error('SQL failed');}return transitionFixture(c,{pending:true,registered:false})(args);}});assert.deepEqual(await tools.dbConnectionProbe({instanceId:'i-pending'}),{instanceId:'i-pending',connected:false,readiness:'pending'});assert.equal(sqlCalls,0);await assert.rejects(tools.dbConnectionProbe({instanceId:'i-one'}),/InvalidObservation/);assert.equal(sqlCalls,1);await assert.rejects(tools.dbConnectionProbe({instanceId:'i-foreign'}),/InvalidObservation/);
});

test('fleet reads provider protection from LifecycleState, never a fabricated response field',async()=>{
 const tools=createObservationTools({binding,config:config(),exec:async(_command,args)=>args.includes('GetCallerIdentity')?{AccountId:'1360282071200743',Arn:'acs:ram::1360282071200743:assumed-role/raptor-rdev-agent-observer/session'}:{TotalCount:2,ScalingInstances:[{InstanceId:'i-one',ScalingGroupId:'group',LifecycleState:'Protected',HealthStatus:'Healthy'},{InstanceId:'i-two',ScalingGroupId:'group',LifecycleState:'InService',HealthStatus:'Healthy'}]}});
 const fleet=await tools.cloudRead({kind:'fleet'});assert.equal(fleet.instances[0].protectedFromScaleIn,true);assert.equal(fleet.instances[1].protectedFromScaleIn,false);
});

test('capacity reads exact provider desired count rather than inferring it from fleet size',async()=>{
 let command;
 const tools=createObservationTools({binding,config:config(),exec:async(_c,args)=>{if(args.includes('GetCallerIdentity'))return {AccountId:'1360282071200743',Arn:'acs:ram::1360282071200743:assumed-role/raptor-rdev-agent-observer/session'};command=args;return {TotalCount:1,ScalingGroups:{ScalingGroup:[{ScalingGroupId:'group',RegionId:'ap-southeast-1',DesiredCapacity:4,MinSize:2,MaxSize:4}]}};}});
 assert.equal((await tools.cloudRead({kind:'capacity'})).desiredCapacity,4);assert.ok(command.includes('DescribeScalingGroups'));assert.ok(command.includes('["group"]'));
});

test('traffic counters require every outcome and fresh source samples for the current client',async()=>{
 for(const defect of ['missing','duplicate','source-stale']) {
  const tools=createObservationTools({binding,config:config(),exec:clientRead,fetch:async value=>{
   const q=new URL(value).searchParams.get('query'),now=Date.now()/1000;
   const metric=outcome=>({job:'test-client',instance:'10.70.1.20:9090',outcome});
   let rows=q.startsWith('up{')?[{metric:{job:'test-client',instance:'10.70.1.20:9090'},value:[now,'1']}]:['failure','timeout','ambiguous'].map(o=>({metric:metric(o),value:[now,q.startsWith('timestamp(')?String(defect==='source-stale'?now-60:now):'0']}));
   if(!q.startsWith('up{')&&defect==='missing')rows=rows.slice(0,1);
   if(!q.startsWith('up{')&&defect==='duplicate')rows[2]=rows[0];
   return {ok:true,json:async()=>({status:'success',data:{resultType:'vector',result:rows}})};
  }});
  await assert.rejects(tools.metricsRead({kind:'trafficFailures'}),/InvalidObservation/,defect);
 }
});

test('NLB backend supports documented listener ID with its 6432 port suffix',async()=>{
 const c=config();c.listenerId='lsn-7kuvcqkaz5o3z2w376@6432';const tools=createObservationTools({binding,config:c,exec:async(_command,args)=>observerCloudFixture(c,args)});
 const backend=await tools.cloudRead({kind:'backend'});assert.equal(backend.listenerId,c.listenerId);assert.equal(backend.nodes.length,1);
 for(const invalidId of ['lsn-test@6443','lsn-test@6432/evil','lsn-test@6432?secret']){c.listenerId=invalidId;await assert.rejects(tools.cloudRead({kind:'backend'}),/InvalidObservation/);}
});
