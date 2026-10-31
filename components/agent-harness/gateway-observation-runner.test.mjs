import {withResourceProof} from './replacement-sequence-fixtures.mjs';
import test from 'node:test';
import assert from 'node:assert/strict';
import {runGatewayObservation} from './gateway-observation-runner.mjs';
const credential='raptor_at_'+'a'.repeat(43);
const url='https://infra.fixture/mcp';
const binding={requestId:'request',attemptId:'attempt',definitionSha256:'hash',envCode:'rdev.ali',clusterId:'cluster',operation:'db-proxy.replace-nodes',executionMode:'replacement',objectKind:'db-proxy',objectCode:'proxy'};
const scope=withResourceProof({envCode:'rdev.ali',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',targetDbCode:'db',application:{envCode:'rdev.ali',appCode:'app',clusterId:'cluster',namespace:'raptor-test',name:'client',uid:'uid'}});
const config={version:1,requestId:'request',definitionSha256:'hash',envCode:'rdev.ali',accountId:'1360282071200743',region:'ap-southeast-1',groupId:'group',serverGroupId:'backend',dbInstanceId:'db',listenerId:'listener',application:{clusterId:'cluster',namespace:'raptor-test',name:'client',uid:'uid'},clientTargets:['10.70.1.20:9090'],prometheusURL:'http://10.70.1.10:9090',expiresAt:new Date(Date.now()+120000).toISOString(),private:{kubeconfig:'/tmp/raptor-private/kubeconfig',sql:{password:'fixture-password',database:'test',user:'fixtureuser',sslmode:'disable'}}};
function input(tool='cloud_read',args={kind:'fleet'}){return {binding,scope,config,kubeconfig:{private:'controller'},infra:{url,credential},deadlineAt:new Date(Date.now()+19000).toISOString(),tool,arguments:args};}
function state(kind,{pending=false,registered=false}={}){
 const observedAt=new Date().toISOString();
 if(kind==='fleet')return {groupId:'group',observedAt,instances:[{instanceId:'i-one',protectedFromScaleIn:false,lifecycleState:'InService',healthStatus:'Healthy'},...(pending?[{instanceId:'i-pending',protectedFromScaleIn:false,lifecycleState:'Pending:Wait',healthStatus:'Healthy'}]:[])]};
 if(kind==='capacity')return {groupId:'group',desiredCapacity:2,minSize:2,maxSize:4,observedAt};
 return {serverGroupId:'backend',listenerId:'listener',observedAt,nodes:[{instanceId:'i-one',privateIp:'10.70.1.30',status:'Available',registered:true,healthy:true},...(pending&&registered?[{instanceId:'i-pending',privateIp:'10.70.1.31',status:'Adding',registered:true,healthy:false}]:[])]};
}
function rows(series){const now=Date.now()/1000;return [{metric:{job:'pgcat',env:'rdev.ali',db_proxy_code:'proxy',target_db_code:'db',ecs_instance_id:'i-one',pool:'test',user:'fixtureuser'},value:[now,series.endsWith('Timestamp')?String(now):series==='up'?'1':series==='active'?'1':'0']}];}
function fixture({options={},damage=x=>x,respond}={}){
 const calls=[];let initialized=0;
 const fetch=async(target,init)=>{
  assert.equal(String(target),url,'unexpected direct HTTP fallback');assert.equal(init.redirect,'error');assert.equal(new Headers(init.headers).get('X-Infra-Authorization'),'Basic '+Buffer.from('agent:'+credential).toString('base64'));
  assert.equal(init.method,'POST');const rpc=JSON.parse(init.body);calls.push(rpc);
  if(respond){const response=await respond(rpc,init);if(response)return response;}
  if(rpc.method==='notifications/initialized')return new Response(null,{status:202});
  let result;
  if(rpc.method==='initialize'){initialized++;result={protocolVersion:'2025-06-18',capabilities:{tools:{}},serverInfo:{name:'infra-api',version:'0.1.0'}};}
  else {assert.equal(rpc.method,'tools/call');const a=rpc.params.arguments;assert.deepEqual(Object.fromEntries(Object.entries(a).filter(([k])=>!['kind','series'].includes(k))),{requestId:'request',envCode:'rdev.ali',proxyCode:'proxy'});
   const selected=rpc.params.name==='proxy_observation_get'?state(a.kind,options):{series:a.series,observedAt:new Date().toISOString(),rows:rows(a.series)};
   const data={envCode:'rdev.ali',proxyCode:'proxy',observedAt:selected.observedAt,pgproxyWorkload:{type:'ess',code:'proxy',workloadKey:'opaque-do-not-decode',state:'Running',targetDbCode:'db',[a.kind??'metrics']:selected}};
   const envelope={data:damage(data,rpc)};result={structuredContent:envelope,content:[{type:'text',text:JSON.stringify(envelope)}]};}
  return new Response(JSON.stringify({jsonrpc:'2.0',id:rpc.id,result}),{headers:{'Content-Type':'application/json'}});
 };
 return {fetch,calls,get initialized(){return initialized;}};
}
const noProbe=()=>{throw Error('unexpected direct cloud probe');};
test('Gateway uses fixed MCP state tools and preserves provider timestamps',async()=>{
 for(const kind of ['fleet','backend','capacity']){const f=fixture();const out=await runGatewayObservation(input('cloud_read',{kind}),{fetch:f.fetch,probe:noProbe});assert.ok(Date.parse(out.observedAt)>0);assert.equal(f.initialized,1);assert.deepEqual(f.calls.filter(r=>r.method==='tools/call').map(r=>r.params.name),['proxy_observation_get']);if(kind==='capacity')assert.equal(out.minSize,2);if(kind==='backend')assert.equal(out.nodes[0].privateIp,'10.70.1.30');}
});
test('PgCat metrics uses one MCP session for nested state and all eight fixed series',async()=>{
 const f=fixture({options:{pending:true}});const out=await runGatewayObservation(input('metrics_read',{kind:'pgcatClients'}),{fetch:f.fetch,probe:noProbe});assert.equal(f.initialized,1);assert.deepEqual(out.pendingInstanceIds,['i-pending']);assert.equal(out.nodes[0].connectedClients,1);assert.deepEqual(out.nodes[0].pools,[{instanceId:'i-one',pool:'test',user:'fixtureuser',active:1,idle:0,waiting:0}]);
 assert.deepEqual(f.calls.filter(r=>r.params?.name==='proxy_pgcat_metrics_get').map(r=>r.params.arguments.series),['up','upTimestamp','active','activeTimestamp','idle','idleTimestamp','waiting','waitingTimestamp']);for(const k of ['observedAt','responseAt','sourceAt','latestResponseAt','latestSourceAt'])assert.ok(Date.parse(out[k])>0);
});
test('registered Pending members still require metrics',async()=>{const f=fixture({options:{pending:true,registered:true}});await assert.rejects(runGatewayObservation(input('metrics_read',{kind:'pgcatClients'}),{fetch:f.fetch,probe:noProbe}),/InvalidObservation/);});
test('SQL probes use nested MCP membership/address reads and pending skips SQL',async()=>{
 for(const instanceId of ['i-one','i-pending','i-foreign']){const f=fixture({options:{pending:true}});const probes=[];const work=()=>runGatewayObservation(input('db_connection_probe',{instanceId}),{fetch:f.fetch,probe:async p=>{probes.push(p);assert.equal(p.command,'psql');assert.equal(p.env.PGHOST,'10.70.1.30');assert.ok(p.args.includes('SELECT 1'));return {connected:true};}});
  if(instanceId==='i-foreign')await assert.rejects(work(),/InvalidObservation/);else{const out=await work();assert.equal(out.connected,instanceId==='i-one');assert.equal(probes.length,instanceId==='i-one'?1:0);}}
});
test('deployment keeps its original sanitized read without cloud calls',async()=>{
 const f=fixture();const out=await runGatewayObservation(input('deployment_read',{}),{fetch:f.fetch,probe:async p=>{assert.equal(p.command,'kubectl');return {metadata:{namespace:'raptor-test',name:'client',uid:'uid',generation:7},spec:{replicas:2,password:'fixture-password'},status:{readyReplicas:2,observedGeneration:6}};}});assert.equal(out.readyReplicas,2);assert.equal(out.generation,7);assert.equal(JSON.stringify(out).includes('fixture-password'),false);assert.equal(f.calls.length,0);
});
for(const mode of ['auth','transport','redirect','isError','malformed','oversized','conflict','wrong-series','secret','wrong-pin','stale-state'])test(`MCP ${mode} fails closed without fallback`,async()=>{
 const f=fixture({damage:(data,rpc)=>{if(mode==='wrong-series'&&rpc.params.name==='proxy_pgcat_metrics_get')return {...data,pgproxyWorkload:{...data.pgproxyWorkload,metrics:{...data.pgproxyWorkload.metrics,series:'foreign'}}};if(mode==='wrong-pin')return {...data,pgproxyWorkload:{...data.pgproxyWorkload,fleet:{...data.pgproxyWorkload.fleet,groupId:'foreign'}}};if(mode==='stale-state')return {...data,observedAt:new Date(0).toISOString()};return data;},respond:async rpc=>{
  if(rpc.method!=='tools/call')return;
  if(mode==='auth')return new Response('secret-error',{status:401});if(mode==='transport')throw Error('network');if(mode==='redirect')return new Response(null,{status:302,headers:{Location:'https://evil.fixture'}});
  if(mode==='malformed')return new Response('{',{headers:{'Content-Type':'application/json'}});if(mode==='oversized')return new Response(' '.repeat(300000),{headers:{'Content-Type':'application/json'}});
  if(mode==='isError')return Response.json({jsonrpc:'2.0',id:rpc.id,result:{isError:true,content:[{type:'text',text:'ProviderUnavailable'}]}});
  if(mode==='conflict')return Response.json({jsonrpc:'2.0',id:rpc.id,result:{structuredContent:{data:state('fleet')},content:[{type:'text',text:JSON.stringify({data:{groupId:'foreign'}})}]}});
  if(mode==='secret')return Response.json({jsonrpc:'2.0',id:rpc.id,result:{structuredContent:{data:{...state('fleet'),leak:credential}}}});
 }});
 await assert.rejects(runGatewayObservation(input(mode==='wrong-series'?'metrics_read':'cloud_read',{kind:mode==='wrong-series'?'pgcatClients':'fleet'}),{fetch:f.fetch,probe:noProbe}),e=>e.message==='InvalidObservation');
});
for(const field of ['response','source'])test(`MCP metrics preserves ${field} freshness refusal`,async()=>{const f=fixture({damage:(data,rpc)=>rpc.params.name==='proxy_pgcat_metrics_get'?{...data,pgproxyWorkload:{...data.pgproxyWorkload,metrics:{...data.pgproxyWorkload.metrics,rows:data.pgproxyWorkload.metrics.rows.map(r=>({...r,value:field==='response'?[0,r.value[1]]:[r.value[0],rpc.params.arguments.series.endsWith('Timestamp')?'0':r.value[1]]}))}}}:data});await assert.rejects(runGatewayObservation(input('metrics_read',{kind:'pgcatClients'}),{fetch:f.fetch,probe:noProbe}),/InvalidObservation/);});
test('missing private MCP access and invalid deadline refuse before any read',async()=>{
 for(const change of [{infra:undefined},{infra:{url:'http://evil.fixture',credential}},{infra:{url,credential:''}},{deadlineAt:new Date(0).toISOString()},{deadlineAt:new Date(Date.now()+60000).toISOString()},{scope:{...scope,proxyCode:'foreign'}}])await assert.rejects(runGatewayObservation({...input(),...change},{fetch:noProbe,probe:noProbe}),/InvalidObservation/);
});
for(const phase of ['initialize','body','later-series'])test(`whole observation deadline aborts ${phase}`,async()=>{
 let aborted=false;const wait=signal=>new Promise((_,reject)=>{const stop=()=>{aborted=true;reject(Error('aborted'));};if(signal.aborted)stop();else signal.addEventListener('abort',stop,{once:true});});
 const f=fixture({respond:async(rpc,init)=>{if(phase==='initialize'&&rpc.method==='initialize')return wait(init.signal);if(phase==='later-series'&&rpc.params?.arguments?.series==='idle')return wait(init.signal);if(phase==='body'&&rpc.method==='initialize')return new Response(new ReadableStream({start(c){init.signal.addEventListener('abort',()=>{aborted=true;c.error(Error('aborted'));},{once:true});}}),{headers:{'Content-Type':'application/json'}});}});
 const started=Date.now();const keepAlive=setTimeout(()=>{},2000);try{await assert.rejects(runGatewayObservation({...input('metrics_read',{kind:'pgcatClients'}),deadlineAt:new Date(Date.now()+100).toISOString()},{fetch:f.fetch,probe:noProbe}),/InvalidObservation/);}finally{clearTimeout(keepAlive);}assert.ok(Date.now()-started<1500);assert.equal(aborted,true);
});
test('whole deadline bounds a late probe result even when the adapter ignores cancellation',async()=>{
 const work=runGatewayObservation({...input('deployment_read',{}),deadlineAt:new Date(Date.now()+50).toISOString()},{probe:async()=>{await new Promise(r=>setTimeout(r,250));return {metadata:{namespace:'raptor-test',name:'client',uid:'uid',generation:1},spec:{replicas:1},status:{observedGeneration:1}};},fetch:noProbe});
 const late=new Promise(resolve=>setTimeout(()=>resolve('late-result'),150));await assert.rejects(Promise.race([work,late]),/InvalidObservation/);
});

for(const mode of ['wrong-type','foreign-root','foreign-code','foreign-db','empty-key','extra-selection','timestamp-mismatch','flat'])test(`resource envelope ${mode} rejected`,async()=>{
 const f=fixture({damage:data=>{const w=data.pgproxyWorkload;if(mode==='flat')return w.fleet;if(mode==='wrong-type')w.type='kubernetes';if(mode==='foreign-root')data.envCode='prod';if(mode==='foreign-code')w.code='other';if(mode==='foreign-db')w.targetDbCode='other';if(mode==='empty-key')w.workloadKey='';if(mode==='extra-selection')w.capacity=state('capacity');if(mode==='timestamp-mismatch')data.observedAt=new Date(0).toISOString();return data;}});
 await assert.rejects(runGatewayObservation(input(),{fetch:f.fetch,probe:noProbe}),/InvalidObservation/);
});
test('resource boundary preserves raw samples and source observation time without normalization',async()=>{
 const {resourceObservation}=await import('./gateway-observation-runner.mjs');const at='2026-10-01T00:00:00.123Z',samples=[{metric:{pool:'db',user:'client'},value:[17.25,'0004']},{metric:{pool:'db',user:'client'},value:[17.25,'NaN']}];
 const metrics={series:'active',observedAt:at,rows:samples};const data={envCode:scope.envCode,proxyCode:scope.proxyCode,observedAt:at,pgproxyWorkload:{type:'ess',code:scope.proxyCode,workloadKey:'opaque',state:'Running',targetDbCode:scope.targetDbCode,metrics}};
 const out=resourceObservation(data,scope,'metrics');assert.strictEqual(out,metrics);assert.strictEqual(out.rows,samples);assert.equal(out.observedAt,at);assert.deepEqual(out.rows.map(r=>r.value),[[17.25,'0004'],[17.25,'NaN']]);
});
