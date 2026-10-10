import {readPgcatClients} from './pgcat-metrics.mjs';
import {discoverClientTargets} from './client-targets.mjs';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {isIP} from 'node:net';
const run=promisify(execFile);
const invalid=()=>new Error('InvalidObservation');
async function boundedJSON(response){
 if(!response.body?.getReader){const data=await response.json();if(Buffer.byteLength(JSON.stringify(data))>262144)throw invalid();return data;}
 const reader=response.body.getReader(),parts=[];let size=0;try{for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>262144){await reader.cancel();throw invalid();}parts.push(Buffer.from(value));}return JSON.parse(Buffer.concat(parts).toString('utf8'));}finally{reader.releaseLock();}
}
const id=value=>typeof value==='string'&&/^[A-Za-z0-9_.-]{1,128}$/.test(value);
function privateEndpoint(value){try{const u=new URL(value);const h=u.hostname;return ['http:','https:'].includes(u.protocol)&&!u.username&&!u.password&&!u.search&&!u.hash&&isIP(h)===4&&(h.startsWith('10.')||h.startsWith('192.168.')||/^172\.(1[6-9]|2\d|3[01])\./.test(h));}catch{return false;}}
export function createObservationTools({binding,config,replacementScope,fetch=globalThis.fetch,exec=async(command,args,env)=>{const {stdout}=await run(command,args,{env:{PATH:process.env.PATH,...env},timeout:15000,maxBuffer:262144});return command==='psql'?{connected:stdout.trim()==='1'}:JSON.parse(stdout);}}){
 if(!config||config.version!==1||config.requestId!==binding.requestId||config.definitionSha256!==binding.definitionSha256||config.envCode!==binding.envCode||config.application?.clusterId!==binding.clusterId||(!Array.isArray(config.clientTargets)||!config.clientTargets.length||config.clientTargets.length>10||config.clientTargets.some(t=>!/^10\.[0-9.]+:9090$/.test(t)))||!privateEndpoint(config.prometheusURL)||!Number.isFinite(Date.parse(config.expiresAt))||Date.parse(config.expiresAt)<=Date.now()||!['accountId','region','groupId','serverGroupId','dbInstanceId'].every(k=>id(config[k]))||!['namespace','name','uid'].every(k=>id(config.application[k])))throw invalid();
 if(replacementScope&&(['envCode','groupId','serverGroupId'].some(k=>config[k]!==replacementScope[k])||['clusterId','namespace','name','uid'].some(k=>config.application[k]!==replacementScope.application?.[k])))throw invalid();
 const fresh=()=>{if(Date.parse(config.expiresAt)<=Date.now())throw invalid();};
 const execute=async(command,args,env)=>{fresh();try{return await exec(command,args,env);}catch{throw invalid();}};
 const tools = {
  async dbConnectionProbe(input){
   if(!input||Object.keys(input).join(',')!=='instanceId'||!id(input.instanceId))throw invalid();
   const backend=await tools.cloudRead({kind:'backend'});
   const node=backend.nodes.find(n=>n.instanceId===input.instanceId&&n.healthy);
   const host=node?.privateIp,sql=config.private?.sql;
   if(!host||!privateEndpoint('http://'+host)||!sql||!id(sql.user)||!id(sql.database)||!['disable','verify-full'].includes(sql.sslmode))throw invalid();
   const result=await execute('psql',['--no-psqlrc','--no-password','--tuples-only','--no-align','--command','SELECT 1'],{PGHOST:host,PGPORT:'6432',PGUSER:sql.user,PGPASSWORD:sql.password,PGDATABASE:sql.database,PGSSLMODE:sql.sslmode,PGCONNECT_TIMEOUT:'5',PGOPTIONS:'-c default_transaction_read_only=on -c statement_timeout=5000'});
   if(result?.connected!==true)throw invalid();return {instanceId:input.instanceId,connected:true};
  },
  async metricsRead(input){
   fresh();if(!input||Object.keys(input).some(k=>k!=="kind"))throw invalid();
   if(input.kind==='pgcatClients'){
    const fleet=await tools.cloudRead({kind:'fleet'});
    return readPgcatClients({scope:replacementScope,nodeIds:fleet.instances.map(n=>n.instanceId),pool:config.private?.sql?.database,user:config.private?.sql?.user,query:async promql=>{
     const url=new URL('/api/v1/query',config.prometheusURL);url.searchParams.set('query',promql);
     try{const response=await fetch(url,{redirect:'error',signal:AbortSignal.timeout(10000)});if(!response.ok)throw invalid();const data=await boundedJSON(response);if(data.status!=='success'||data.data?.resultType!=='vector')throw invalid();return data.data.result;}catch{throw invalid();}
    }});
   }
   const targets=await discoverClientTargets(config.application,async(kind,selector)=>execute('kubectl',['--kubeconfig',config.private?.kubeconfig,'--namespace',config.application.namespace,'get',kind,...(kind==='deployment'?[config.application.name]:['--selector',selector]),'--output','json'],{}));
   const selector=`job="test-client"`;
   const queries={trafficFailures:`infra_test_operations_total{${selector},outcome=~"failure|timeout|ambiguous"}`,connections:`infra_test_connected_sessions{${selector}}`};
   if(!Object.hasOwn(queries,input.kind))throw invalid();
   const scrapeURL=new URL('/api/v1/query',config.prometheusURL);scrapeURL.searchParams.set('query',`up{${selector}} * (time() - timestamp(up{${selector}}) <= bool 15)`);
   try{const response=await fetch(scrapeURL,{redirect:'error',signal:AbortSignal.timeout(10000)});if(!response.ok)throw invalid();const data=await boundedJSON(response),rows=data.data?.result;
    if(data.status!=='success'||data.data?.resultType!=='vector'||!Array.isArray(rows)||rows.length!==targets.length)throw invalid();
    const seen=new Set();for(const row of rows){const instance=row.metric?.instance;if(row.metric?.job!=='test-client'||!targets.includes(instance)||seen.has(instance)||Number(row.value?.[1])!==1||!Number.isFinite(Number(row.value?.[0]))||Math.abs(Date.now()/1000-Number(row.value[0]))>30)throw invalid();seen.add(instance);}
   }catch{throw invalid();}
   const query=async expression=>{
    try{const url=new URL('/api/v1/query',config.prometheusURL);url.searchParams.set('query',expression);const response=await fetch(url,{redirect:'error',signal:AbortSignal.timeout(10000)});if(!response.ok)throw invalid();const data=await boundedJSON(response);if(data.status!=='success'||data.data?.resultType!=='vector')throw invalid();return data.data.result;}catch{throw invalid();}
   };
   const rows=await query(queries[input.kind]),sources=await query('timestamp('+queries[input.kind]+')'),now=Date.now()/1000;
   const outcomes=input.kind==='trafficFailures'?['failure','timeout','ambiguous']:[''];
   const expected=new Set(targets.flatMap(target=>outcomes.map(outcome=>target+'|'+outcome)));
   const validate=(samples,source=false)=>{
    if(!Array.isArray(samples)||samples.length!==expected.size)throw invalid();
    const seen=new Set();return samples.map(row=>{
     const instance=row.metric?.instance,outcome=row.metric?.outcome??'',key=instance+'|'+outcome,timestamp=Number(row.value?.[0]),value=Number(row.value?.[1]);
     if(row.metric?.job!=='test-client'||!expected.has(key)||seen.has(key)||!Number.isFinite(timestamp)||!Number.isFinite(value)||value<0||Math.abs(now-timestamp)>30||(source&&(now-value>15||value-now>2)))throw invalid();
     seen.add(key);return {timestamp,value,instance,...(outcome?{outcome}:{})};
    });
   };
   validate(sources,true);return {observedAt:new Date().toISOString(),samples:validate(rows)};
  },
  async cloudRead({kind}){
   if(!['fleet','backend','capacity'].includes(kind))throw invalid();
   const identity=await execute('aliyun',['sts','GetCallerIdentity','--RegionId',config.region],config.private?.aliyunEnv);
   if(identity.AccountId!==config.accountId||typeof identity.Arn!=='string'||!identity.Arn.startsWith(`acs:ram::${config.accountId}:assumed-role/raptor-rdev-agent-observer/`))throw invalid();
   if(kind==='capacity'){
    const data=await execute('aliyun',['ess','DescribeScalingGroups','--RegionId',config.region,'--ScalingGroupIds',JSON.stringify([config.groupId])],config.private?.aliyunEnv),groups=data.ScalingGroups?.ScalingGroup??data.ScalingGroups;
    if(data.TotalCount!==1||!Array.isArray(groups)||groups.length!==1)throw invalid();const group=groups[0];
    if(group.ScalingGroupId!==config.groupId||group.RegionId!==config.region||!['DesiredCapacity','MinSize','MaxSize'].every(k=>Number.isSafeInteger(group[k])&&group[k]>=0)||group.MinSize!==2||group.MaxSize!==4||group.DesiredCapacity<2||group.DesiredCapacity>4)throw invalid();
    return {groupId:config.groupId,desiredCapacity:group.DesiredCapacity,minSize:group.MinSize,maxSize:group.MaxSize,observedAt:new Date().toISOString()};
   }
   if(kind==='backend'){
    if(!id(config.listenerId))throw invalid();
    const fleet=await tools.cloudRead({kind:'fleet'}),members=new Set(fleet.instances.map(node=>node.instanceId)),eligible=new Set(fleet.instances.filter(node=>['InService','Protected'].includes(node.lifecycleState)&&node.healthStatus==='Healthy').map(node=>node.instanceId));
    const read=(action,field,value)=>execute('aliyun',['nlb',action,'--RegionId',config.region,'--'+field,value],config.private?.aliyunEnv);
    const servers=await read('ListServerGroupServers','ServerGroupId',config.serverGroupId);
    if(!Array.isArray(servers.Servers)||servers.Servers.length>50||servers.TotalCount!==servers.Servers.length||servers.NextToken)throw invalid();
    const listener=await read('GetListenerAttribute','ListenerId',config.listenerId);
    if(listener.ListenerId!==config.listenerId||listener.ServerGroupId!==config.serverGroupId||listener.ListenerPort!==6432||listener.ListenerStatus!=='Running')throw invalid();
    const health=await read('GetListenerHealthStatus','ListenerId',config.listenerId),listeners=health.ListenerHealthStatus;
    if(health.NextToken||!Array.isArray(listeners)||listeners.length!==1||listeners[0].ListenerId!==config.listenerId||listeners[0].ListenerPort!==6432||listeners[0].ServerGroupInfos?.length!==1)throw invalid();
    const group=listeners[0].ServerGroupInfos[0];if(group.ServerGroupId!==config.serverGroupId||group.HeathCheckEnabled!==true||!Array.isArray(group.NonNormalServers)||group.NonNormalServers.length>50)throw invalid();
    const seen=new Set(),bad=new Set(group.NonNormalServers.map(node=>{if(!id(node.ServerId)||node.Port!==6432||!['Initial','Unhealthy','Unavailable'].includes(node.Status))throw invalid();return node.ServerId;}));
    if(bad.size!==group.NonNormalServers.length)throw invalid();
    const nodes=servers.Servers.map(node=>{if(!id(node.ServerId)||!members.has(node.ServerId)||!privateEndpoint('http://'+node.ServerIp)||seen.has(node.ServerId)||node.ServerGroupId!==config.serverGroupId||node.ServerType!=='Ecs'||node.Port!==6432||node.Status!=='Available'||!Number.isInteger(node.Weight)||node.Weight<0||node.Weight>100)throw invalid();seen.add(node.ServerId);return {instanceId:node.ServerId,privateIp:node.ServerIp,registered:node.Weight>0,healthy:node.Weight>0&&!bad.has(node.ServerId)&&eligible.has(node.ServerId)};});
    if([...bad].some(value=>!seen.has(value)))throw invalid();return {serverGroupId:config.serverGroupId,listenerId:config.listenerId,observedAt:new Date().toISOString(),nodes};
   }
   const result=await execute('aliyun',['ess','DescribeScalingInstances','--RegionId',config.region,'--ScalingGroupId',config.groupId,'--PageSize','50'],config.private?.aliyunEnv);
   const rows=result.ScalingInstances?.ScalingInstance??result.ScalingInstances;
   if(!Array.isArray(rows)||rows.length>50||(result.TotalCount!==undefined&&(!Number.isInteger(result.TotalCount)||result.TotalCount!==rows.length)))throw invalid();
   const fleetSeen=new Set();
   return {groupId:config.groupId,observedAt:new Date().toISOString(),instances:rows.map(row=>{if(!id(row.InstanceId)||fleetSeen.has(row.InstanceId)||row.ScalingGroupId!==config.groupId||!['InService','Protected','Pending','Pending:Wait','Standby','Stopped','Removing','Removing:Wait'].includes(row.LifecycleState)||!['Healthy','Unhealthy'].includes(row.HealthStatus))throw invalid();fleetSeen.add(row.InstanceId);return {instanceId:row.InstanceId,protectedFromScaleIn:row.LifecycleState==='Protected',lifecycleState:row.LifecycleState,healthStatus:row.HealthStatus};})};
  },
  async deploymentRead(input){
   if(!input||Object.keys(input).length)throw invalid();const app=config.application;
   const result=await execute('kubectl',['--kubeconfig',config.private?.kubeconfig,'--namespace',app.namespace,'get','deployment',app.name,'--output','json'],{});
   if(result.metadata?.uid!==app.uid||result.metadata?.namespace!==app.namespace||result.metadata?.name!==app.name)throw invalid();
   return {clusterId:app.clusterId,namespace:app.namespace,name:app.name,uid:app.uid,observedAt:new Date().toISOString(),replicas:result.spec?.replicas??0,readyReplicas:result.status?.readyReplicas??0,updatedReplicas:result.status?.updatedReplicas??0,availableReplicas:result.status?.availableReplicas??0};
  }
 };
 return tools;
}
