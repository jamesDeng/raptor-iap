import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {loadPiRuntime} from './pi-adapter.mjs';
import {operationCapabilities,operationToolSchema} from './operation-profile.mjs';
import {runLiveReplacement} from './live-replacement.mjs';

async function runScenario(failureKind,baselineCase,expansionCase){
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'replacement-read-queue-')));
 try{
  const state=path.join(root,'state'),skills=path.join(root,'skills');
  fs.mkdirSync(state);
  fs.mkdirSync(path.join(skills,'pgcat-replacement','references'),{recursive:true});
  fs.writeFileSync(path.join(skills,'pgcat-replacement','SKILL.md'),'---\nname: pgcat-replacement\ndescription: Safe replacement.\n---\nProtect and expand.\n');
  fs.writeFileSync(path.join(skills,'pgcat-replacement','references','contracts.md'),'Exact contracts.\n');
  fs.writeFileSync(path.join(skills,'pgcat-replacement','references','recovery.md'),'Reconcile uncertainty.\n');
  fs.writeFileSync(path.join(state,'auth.json'),JSON.stringify({openai:{type:'oauth',access:'synthetic',refresh:'synthetic',expires:Date.now()+999999}}));
  const b={requestId:'11111111-1111-4111-8111-111111111111',attemptId:'22222222-2222-4222-8222-222222222222',definitionSha256:'a'.repeat(64),skillsCommit:'b'.repeat(40),operation:'db-proxy.replace-nodes',executionMode:'replacement',objectKind:'db-proxy',objectCode:'proxy',envCode:'dev',clusterId:'cluster',model:'gpt-5.6-luna'};
  const scope={envCode:'dev',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',targetDbCode:'db',application:{envCode:'dev',appCode:'client',clusterId:'cluster',namespace:'test',name:'traffic',uid:'uid'}};
  const {createAssistantMessageEventStream}=await import('@earendil-works/pi-ai/utils/event-stream');
 const {getCurrentTools}=await import('@earendil-works/pi-ai');
  const {runtime}=await loadPiRuntime({stateRoot:state});runtime.getAuth=async()=>({type:'oauth',access:'fixture'});
 let round=0,active=0,maximum=0,serviceCalls=0,observerCalls=0;const visibleTools=[],writes=[];
 const target={requestId:b.requestId,envCode:b.envCode,proxyCode:scope.proxyCode,groupId:scope.groupId};
 const expand={name:'mcp__infra__db_proxy_scale',arguments:{...target,actionId:'expand',desiredCapacity:4}};
 const postWrites={
 restart:{name:'mcp__infra__deployment_restart',arguments:{requestId:b.requestId,envCode:b.envCode,...scope.application}},
 protection:{name:'mcp__infra__db_proxy_node_protection_set',arguments:{...target,instanceIds:['new-a','new-b'],protected:true}},
 deregister:{name:'mcp__infra__db_proxy_nodes_deregister',arguments:{...target,serverGroupId:scope.serverGroupId,instanceIds:['i-one']}},
 scale:{name:'mcp__infra__db_proxy_scale',arguments:{...target,actionId:'shrink',desiredCapacity:3}},
 approval:{name:'mcp__raptor__approval_request',arguments:{requestId:b.requestId,envCode:b.envCode,actionId:'shrink',interface:'ess.scale-in',target:{proxyCode:scope.proxyCode,groupId:scope.groupId},parameters:{desiredCapacity:3}}}
 };
 runtime.streamSimple=(model,context)=>{
   round++;
  visibleTools.push(getCurrentTools(context.messages).map(tool=>tool.name));
  const readyCase=['ready','ready-unknown','ready-batch','ready-scale-batch'].includes(expansionCase);
  const readinessCalls=[...['fleet','backend','capacity'].map(kind=>({name:'cloud_read',arguments:{kind}})),{name:'metrics_read',arguments:{kind:'pgcatClients'}},...['new-a','new-b'].map(instanceId=>({name:'db_connection_probe',arguments:{instanceId}}))];
  const calls=expansionCase?(round===1?[{name:'cloud_read',arguments:{kind:'fleet'}}]:round===2?[expand,...(expansionCase==='ready-scale-batch'?[...readinessCalls,postWrites.protection]:postWrites[expansionCase]?[postWrites[expansionCase]]:[])]:readyCase&&round===3?[...readinessCalls,...(expansionCase==='ready-batch'?[postWrites.protection]:[])]:readyCase&&round===4?[postWrites.protection]:[{name:'resource_wait',arguments:{seconds:60}}]):baselineCase?(round===1?[{name:'cloud_read',arguments:{kind:'fleet'}}]:[{name:'resource_wait',arguments:{seconds:60}}]):round===1?[
    {name:'mcp__raptor__request_get',arguments:{requestId:b.requestId}},
    {name:'cloud_read',arguments:{kind:'backend'}},
    {name:'metrics_read',arguments:{kind:'trafficFailures'}},
    {name:'mcp__infra__cloud_identity_get',arguments:{envCode:b.envCode}}
   ]:[{name:'resource_wait',arguments:{seconds:60}}];
   const stream=createAssistantMessageEventStream();
   const message={role:'assistant',content:calls.map((call,i)=>({type:'toolCall',id:'call-'+round+'-'+i,...call})),api:model.api,provider:model.provider,model:model.id,usage:{input:3,output:2,totalTokens:5,cacheRead:0,cacheWrite:0,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}},stopReason:'toolUse',timestamp:Date.now()};
   stream.push({type:'done',reason:'toolUse',message});return stream;
  };
  const fetcher=async(url,init)=>{
   const rpc=JSON.parse(init.body),server=String(url).includes('raptor')?'raptor':'infra';
   if(rpc.method==='notifications/initialized')return new Response(null,{status:202});
   let result;
   if(rpc.method==='initialize')result={protocolVersion:'2025-06-18',capabilities:{tools:{}},serverInfo:{name:server,version:'1'}};
   else if(rpc.method==='tools/list')result={tools:operationCapabilities(b,scope)[server].map(name=>({name,description:'Scoped operation',inputSchema:operationToolSchema(server,name)}))};
   else if(rpc.method==='tools/call'){
    serviceCalls++;
    if(['db_proxy_scale','db_proxy_node_protection_set','db_proxy_nodes_deregister','deployment_restart','approval_request'].includes(rpc.params.name))writes.push(rpc.params.name);
    active++;maximum=Math.max(maximum,active);
    await new Promise(resolve=>setTimeout(resolve,30));
    active--;
    if(failureKind==='http'&&serviceCalls===1)return new Response('Unavailable',{status:503});
    if(failureKind==='malformed'&&serviceCalls===1)return new Response('{bad json',{headers:{'Content-Type':'application/json'}});
    const args=rpc.params.arguments;
    const data=rpc.params.name==='deployment_restart'?{evidenceMode:'live',accepted:true,generation:2,target:args}:rpc.params.name==='db_proxy_scale'?{evidenceMode:'live',groupId:scope.groupId,previousDesiredCapacity:args.desiredCapacity===4?2:4,desiredCapacity:args.desiredCapacity,outcome:['unknown','ready-unknown'].includes(expansionCase)?'unknown':'accepted'}:rpc.params.name==='db_proxy_node_protection_set'||rpc.params.name==='db_proxy_nodes_deregister'?{evidenceMode:'live',instanceIds:args.instanceIds,outcome:'submitted'}:rpc.params.name==='approval_request'?{requestId:b.requestId,approvalId:'approval',actionId:args.actionId,binding:args,state:'pending'}:server==='raptor'?{requestId:b.requestId,definition:{envCode:b.envCode,object:{kind:b.objectKind,code:b.objectCode},operations:[{name:b.operation,parameters:{mode:'replacement'}}]}}:{envCode:b.envCode,accountMatches:true,evidenceMode:'live',observedAt:new Date().toISOString()};
    result={structuredContent:{data},content:[{type:'text',text:JSON.stringify({data})}]};
   }else throw Error('UnexpectedRPC');
   return new Response(JSON.stringify({jsonrpc:'2.0',id:rpc.id,result}),{headers:{'Content-Type':'application/json'}});
  };
  const read=async args=>{observerCalls++;active++;maximum=Math.max(maximum,active);await new Promise(resolve=>setTimeout(resolve,30));active--;if(failureKind==='observer'&&observerCalls===1)throw Error('FixtureObserverUnavailable');if(expansionCase&&(round>=3||(round===2&&expansionCase==='ready-scale-batch'))){
   const ids=['i-one','i-two','new-a','new-b'],observedAt=new Date().toISOString();
   if(args.kind==='fleet')return {groupId:scope.groupId,observedAt,instances:ids.map(instanceId=>({instanceId,lifecycleState:'Protected',healthStatus:'Healthy'}))};
   if(args.kind==='backend')return {serverGroupId:scope.serverGroupId,observedAt,nodes:ids.map(instanceId=>({instanceId,status:'Available',healthy:true,registered:true,privateIp:'10.0.0.1'}))};
   if(args.kind==='capacity')return {groupId:scope.groupId,observedAt,desiredCapacity:4,minSize:2,maxSize:4};
   if(args.kind==='pgcatClients')return {observedAt,pendingInstanceIds:[],nodes:ids.map(instanceId=>({instanceId,connectedClients:3,pools:[{instanceId,pool:'db',user:'client',active:1,idle:2,waiting:0}]}))};
   if(args.instanceId)return {instanceId:args.instanceId,connected:true};
  }if(args?.kind==='fleet')return {groupId:scope.groupId,observedAt:new Date().toISOString(),instances:(baselineCase==='valid'?['i-one','i-two']:['i-one']).map(instanceId=>({instanceId,lifecycleState:'Protected',healthStatus:'Healthy'}))};return {observedAt:new Date().toISOString()};};
  const observer={cloudRead:read,metricsRead:read,deploymentRead:read,dbConnectionProbe:read};
  const options={stateRoot:state,privateRoot:path.join(root,'private'),runtime,binding:b,replacementScope:scope,skills:{tag:'skills-v1.0.0',commitSHA:b.skillsCommit,path:skills},limits:{model_seconds:90,max_turns:10,max_output_tokens:1024},mcpConfig:{raptor:{url:'https://raptor.fixture/mcp',headers:{Authorization:'Bearer synthetic'}},infra:{url:'https://infra.fixture/mcp',headers:{'X-Infra-Authorization':'Bearer synthetic'}}},fetch:fetcher,observer};
  const out=await runLiveReplacement(options);
  if(postWrites[expansionCase]||['ready-batch','ready-scale-batch'].includes(expansionCase)){assert.equal(out.error,'ToolFailed',JSON.stringify(out));assert.deepEqual(writes,['db_proxy_scale'],'early post-expansion writes must never reach MCP');}
  else if(failureKind){assert.equal(out.error,'ToolFailed',JSON.stringify(out));if(failureKind==='observer'){assert.equal(observerCalls,1,'an observation failure must stop queued observations');assert.ok(serviceCalls<=1,'an observation failure must stop queued service calls');}else assert.equal(serviceCalls,1,'a failed MCP read must stop queued service calls');}
  else assert.equal(out.paused?.kind,'resource',JSON.stringify(out));
  assert.equal(maximum,1,'first-turn reads must never overlap at their service boundary');
  assert.ok(visibleTools[0].includes('mcp__raptor__request_get'),'initial reads must remain available');
  assert.ok(!visibleTools[0].includes('mcp__infra__db_proxy_node_protection_set'),'a fresh session must not advertise protection before a fleet baseline');
  assert.ok(!visibleTools[0].includes('mcp__raptor__approval_request'),'a fresh session must not advertise approval writes before a fleet baseline');
  if(expansionCase&&!postWrites[expansionCase]&&expansionCase!=='ready-scale-batch'){for(const name of ['mcp__infra__db_proxy_scale','mcp__infra__db_proxy_node_protection_set','mcp__infra__db_proxy_nodes_deregister','mcp__infra__deployment_restart','mcp__raptor__approval_request'])assert.ok(!visibleTools[2].includes(name),'pending readiness must hide '+name);assert.ok(visibleTools[2].includes('resource_wait'));}
  if(['ready','ready-unknown'].includes(expansionCase)){
   assert.ok(visibleTools[3].includes('mcp__infra__db_proxy_node_protection_set'));
   assert.deepEqual(writes,['db_proxy_scale','db_proxy_node_protection_set']);
   const saved=fs.readFileSync(path.join(state,'sessions',out.sessionReference.file),'utf8').trim().split('\n').map(line=>JSON.parse(line));
   const witness=saved.find(e=>e.customType==='raptor-replacement-readiness-v1');assert.ok(witness);assert.ok(!JSON.stringify(witness).includes('10.0.0.1'));
   const resumed=await runLiveReplacement({...options,sessionReference:out.sessionReference,decisionContext:{decision:'resume'}});
   assert.equal(resumed.paused?.kind,'resource',JSON.stringify(resumed));assert.ok(visibleTools.at(-1).includes('mcp__raptor__approval_request'));
   assert.deepEqual(writes,['db_proxy_scale','db_proxy_node_protection_set'],'resume must not replay expansion');
  }
  if(['accepted','unknown'].includes(expansionCase)){
   const resumed=await runLiveReplacement({...options,sessionReference:out.sessionReference,decisionContext:{decision:'resume'}});
   assert.equal(resumed.paused?.kind,'resource',JSON.stringify(resumed));assert.ok(!visibleTools.at(-1).includes('mcp__raptor__approval_request'));
  }
  if(baselineCase)assert.equal(visibleTools[1].includes('mcp__infra__db_proxy_node_protection_set'),baselineCase==='valid','only a persisted valid two-node baseline unlocks replacement writes on the next turn');
 }finally{fs.rmSync(root,{recursive:true,force:true});}
}

test('replacement queues simultaneous first reads across MCP and observation tools',()=>runScenario());
test('replacement stops queued reads after an HTTP failure',()=>runScenario('http'));
test('replacement stops queued reads after a malformed MCP response',()=>runScenario('malformed'));
test('replacement stops queued reads after an observation failure',()=>runScenario('observer'));
test('replacement exposes mutations on the next turn after a valid fleet baseline',()=>runScenario(undefined,'valid'));
test('replacement keeps mutations unavailable after an invalid fleet baseline',()=>runScenario(undefined,'invalid'));

for(const name of ['protection','deregister','scale','restart','approval'])test('replacement blocks same-round post-expansion '+name+' before MCP',()=>runScenario(undefined,'valid',name));
for(const name of ['accepted','unknown'])test('replacement hides writes after '+name+' expansion',()=>runScenario(undefined,'valid',name));

for(const name of ['ready','ready-unknown'])test('replacement unlocks fresh exact readiness on the next turn and retains it on resume: '+name,()=>runScenario(undefined,'valid',name));
test('replacement cannot use a newly collected witness for a write in the same read round',()=>runScenario(undefined,'valid','ready-batch'));

test('replacement cannot expand, gather readiness and write in the same formerly unlocked round',()=>runScenario(undefined,'valid','ready-scale-batch'));
