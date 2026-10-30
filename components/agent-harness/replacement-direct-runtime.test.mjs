import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {loadPiRuntime} from './pi-adapter.mjs';
import {operationCapabilities,operationToolSchema} from './operation-profile.mjs';
import {runLiveReplacement} from './live-replacement.mjs';
import {scope,old,added,observations} from './replacement-sequence-fixtures.mjs';

test('real pinned Pi executes direct sequence through surge wait and exact approval resume',async()=>{
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'direct-native-')));
 try{
  const state=path.join(root,'state'),skillsPath=path.join(root,'skills');fs.mkdirSync(state);fs.mkdirSync(skillsPath);
  fs.cpSync(new URL('../../agent-skills/pgcat-replacement',import.meta.url),path.join(skillsPath,'pgcat-replacement'),{recursive:true});
  fs.writeFileSync(path.join(state,'auth.json'),JSON.stringify({openai:{type:'oauth',access:'synthetic',refresh:'synthetic',expires:Date.now()+999999}}));
  const b={requestId:'11111111-1111-4111-8111-111111111111',attemptId:'22222222-2222-4222-8222-222222222222',definitionSha256:'a'.repeat(64),skillsCommit:'b'.repeat(40),operation:'db-proxy.replace-nodes',executionMode:'replacement',objectKind:'db-proxy',objectCode:'proxy',envCode:scope.envCode,clusterId:'cluster',model:'gpt-5.6-luna'};
  const {createAssistantMessageEventStream}=await import('@earendil-works/pi-ai/utils/event-stream');
  const {runtime}=await loadPiRuntime({stateRoot:state});runtime.getAuth=async()=>({type:'oauth',access:'fixture'});
  let desired=2,expanded=false,newProtected=false,withdrawn=false,restarted=false,oldUnprotected=false,surge=false,round=0,phase=0;
  const writes=[],trafficDuringSurge=[];
  const call=(name,args)=>({name,arguments:args}),target={requestId:b.requestId,envCode:b.envCode,proxyCode:'proxy',groupId:'group'};
  const cloud=kind=>call('cloud_read',{kind}),metric=kind=>call('metrics_read',{kind}),deployment=call('deployment_read',{});
  const control=[call('mcp__raptor__request_get',{requestId:b.requestId}),call('mcp__raptor__environment_get',{requestId:b.requestId,envCode:b.envCode}),call('mcp__raptor__object_get',{requestId:b.requestId,kind:'db-proxy',code:'proxy'}),call('mcp__infra__cloud_identity_get',{envCode:b.envCode}),call('mcp__infra__deployments_list',{envCode:b.envCode,kind:'db-proxy',code:'proxy'}),call('mcp__infra__deployment_status_get',scope.application)];
  const reads=ids=>[cloud('fleet'),cloud('backend'),cloud('capacity'),deployment,metric('pgcatClients'),...ids.map(instanceId=>call('db_connection_probe',{instanceId}))];
  const approval={requestId:b.requestId,actionId:'shrink',interface:'ess.scale-in',envCode:b.envCode,target:{proxyCode:'proxy',groupId:'group'},parameters:{desiredCapacity:2}};
  const scripts=[
   [[...control,...reads(old),metric('trafficFailures'),metric('connections')],
    [call('mcp__infra__db_proxy_scale',{...target,actionId:'expand',desiredCapacity:4})],
    reads(added),
    [call('mcp__infra__db_proxy_node_protection_set',{...target,instanceIds:added,protected:true})],
    [cloud('fleet')],
    [call('mcp__infra__db_proxy_nodes_deregister',{...target,serverGroupId:'backend',instanceIds:old})],
    reads(added),
    [call('mcp__infra__deployment_restart',{requestId:b.requestId,...scope.application})],
    [deployment,call('resource_wait',{seconds:60})]],
   [reads(added),
    [call('mcp__infra__db_proxy_node_protection_set',{...target,instanceIds:old,protected:false})],
    reads(added),
    [call('mcp__raptor__approval_request',approval)],
    [call('mcp__raptor__request_pause',{requestId:b.requestId,reason:'waiting_approval'})]],
   [[call('mcp__raptor__approval_get',{requestId:b.requestId,approvalId:'33333333-3333-4333-8333-333333333333'}),...reads(added)],
    [call('mcp__infra__db_proxy_scale',{...target,actionId:'shrink',desiredCapacity:2})],[]]
  ];
  runtime.streamSimple=(model)=>{
   const calls=scripts[phase][round++];assert.ok(calls,'unexpected model round');
   const message={role:'assistant',content:calls.length?calls.map((c,i)=>({type:'toolCall',id:`call-${phase}-${round}-${i}`,...c})):[{type:'text',text:'Replacement convergence awaits fixed evidence checks.'}],api:model.api,provider:model.provider,model:model.id,usage:{input:3,output:2,totalTokens:5,cacheRead:0,cacheWrite:0,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}},stopReason:calls.length?'toolUse':'stop',timestamp:Date.now()};
   const stream=createAssistantMessageEventStream();stream.push({type:'done',reason:message.stopReason,message});return stream;
  };
  const current=()=>observations(expanded?(desired===2?added:[...old,...added]):old,{withdrawn:withdrawn?old:[],unprotected:[...(!newProtected?added:[]),...(oldUnprotected?old:[])],zero:restarted,generation:restarted?6:5});
  const observer={cloudRead:async({kind})=>current()[kind],deploymentRead:async()=>{const app=current().deployment;return surge?{...app,observedGeneration:5,readyReplicas:3,updatedReplicas:1}:app;},metricsRead:async({kind})=>{if(surge&&kind!=='pgcatClients'){trafficDuringSurge.push(kind);throw Error('InvalidObservation');}return current()[kind==='pgcatClients'?'metrics':kind];},dbConnectionProbe:async({instanceId})=>({instanceId,connected:!(withdrawn&&old.includes(instanceId))})};
  const fetcher=async(url,init)=>{
   const rpc=JSON.parse(init.body),server=String(url).includes('raptor')?'raptor':'infra';
   if(rpc.method==='notifications/initialized')return new Response(null,{status:202});let result;
   if(rpc.method==='initialize')result={protocolVersion:'2025-06-18',capabilities:{tools:{}},serverInfo:{name:server,version:'1'}};
   else if(rpc.method==='tools/list')result={tools:operationCapabilities(b,scope)[server].map(name=>({name,description:'Scoped operation',inputSchema:operationToolSchema(server,name)}))};
   else if(rpc.method==='tools/call'){
    const name=rpc.params.name,args=rpc.params.arguments,at=new Date().toISOString();let data;
    if(name==='request_get')data={requestId:b.requestId,definition:{envCode:b.envCode,object:{kind:'db-proxy',code:'proxy'},operations:[{name:b.operation,parameters:{mode:'replacement'}}]}};
    else if(name==='cloud_identity_get')data={envCode:b.envCode,accountMatches:true,evidenceMode:'live',observedAt:at};
    else if(name==='deployments_list')data=[{resourceId:'group',kind:'db-proxy',objectCode:'proxy',envCode:b.envCode,'target-db-code':'db',evidenceMode:'live',observedAt:at}];
    else if(name==='deployment_status_get')data={...current().deployment,evidenceMode:'live'};
    else if(name==='db_proxy_scale'){writes.push([name,args.desiredCapacity]);data={evidenceMode:'live',groupId:'group',previousDesiredCapacity:desired,desiredCapacity:args.desiredCapacity,outcome:args.desiredCapacity===2?'unknown':'submitted'};desired=args.desiredCapacity;expanded=true;}
    else if(name==='db_proxy_node_protection_set'){writes.push([name,args.instanceIds,args.protected]);if(args.protected)newProtected=true;else oldUnprotected=true;data={evidenceMode:'live',instanceIds:args.instanceIds,outcome:'submitted'};}
    else if(name==='db_proxy_nodes_deregister'){writes.push([name,args.instanceIds]);withdrawn=true;data={evidenceMode:'live',instanceIds:args.instanceIds,outcome:'unknown'};}
    else if(name==='deployment_restart'){writes.push([name]);restarted=true;surge=true;data={evidenceMode:'live',accepted:true,generation:6,target:args};}
    else if(name==='approval_request'||name==='approval_get'){writes.push([name]);data={requestId:b.requestId,approvalId:'33333333-3333-4333-8333-333333333333',actionId:'shrink',binding:approval,state:name==='approval_get'?'approved':'pending'};}
    else data={accepted:true};
    result={structuredContent:{data},content:[{type:'text',text:JSON.stringify({data})}]};
   }else throw Error('UnexpectedRPC');
   return new Response(JSON.stringify({jsonrpc:'2.0',id:rpc.id,result}),{headers:{'Content-Type':'application/json'}});
  };
  const options={stateRoot:state,privateRoot:path.join(root,'private'),runtime,binding:b,replacementScope:scope,observer,skills:{tag:'skills-v0.5.0',commitSHA:b.skillsCommit,path:skillsPath},limits:{model_seconds:90,max_turns:10,max_output_tokens:1024},mcpConfig:{raptor:{url:'https://raptor.fixture/mcp',headers:{Authorization:'Bearer synthetic'}},infra:{url:'https://infra.fixture/mcp',headers:{'X-Infra-Authorization':'Bearer synthetic'}}},fetch:fetcher};
  const first=await runLiveReplacement(options);assert.equal(first.paused?.kind,'resource',JSON.stringify(first));assert.equal(round,9);assert.deepEqual(trafficDuringSurge,[]);
  phase=1;round=0;surge=false;
  const second=await runLiveReplacement({...options,binding:{...b,attemptId:'44444444-4444-4444-8444-444444444444'},sessionReference:first.sessionReference,decisionContext:{decision:'resume'}});
  assert.equal(second.paused?.kind,'approval',JSON.stringify(second));assert.equal(second.paused.actionId,'shrink');assert.equal(round,5);
  phase=2;round=0;
  const third=await runLiveReplacement({...options,binding:{...b,attemptId:'55555555-5555-4555-8555-555555555555'},sessionReference:second.sessionReference,decisionContext:{decision:'resume',approvalId:second.paused.approvalId,actionId:'shrink'}});
  assert.equal(third.passed,true,JSON.stringify(third));assert.equal(third.result.replacement.version,2);assert.equal(third.result.replacement.restartGeneration,6);assert.deepEqual(third.result.replacement.newInstanceIds,added);assert.equal(third.result.replacement.acceptance,'pending');
  assert.deepEqual(writes.filter(w=>w[0]==='db_proxy_scale'),[['db_proxy_scale',4],['db_proxy_scale',2]]);
  assert.equal(writes.filter(w=>w[0]==='deployment_restart').length,1);assert.equal(writes.filter(w=>w[0]==='approval_request').length,1);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
