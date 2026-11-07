import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {loadPiRuntime} from './pi-adapter.mjs';
import {operationCapabilities,operationToolSchema} from './operation-profile.mjs';
import {runLiveReplacement} from './live-replacement.mjs';
import {scope,old,added,observations} from './replacement-sequence-fixtures.mjs';

test('real pinned Pi settles ten-turn expansion and approval with durable withdrawal and rollout waits',async(t)=>{
 t.mock.timers.enable({apis:['Date'],now:Date.now()});
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'direct-native-')));
 try{
  const state=path.join(root,'state'),skillsPath=path.join(root,'skills');fs.mkdirSync(state);fs.mkdirSync(skillsPath);
  fs.mkdirSync(path.join(skillsPath,'pgcat-replacement','references'),{recursive:true});for(const [name,text] of [['SKILL.md','---\nname: pgcat-replacement\ndescription: Synthetic resource contract fixture.\n---\nreplacement-contract: resource-v1\n'],['references/contracts.md','Use logical codes and opaque workloadKey only.'],['references/recovery.md','Preserve baseline; never replay submitted operations.']])fs.writeFileSync(path.join(skillsPath,'pgcat-replacement',name),text);
  fs.writeFileSync(path.join(state,'auth.json'),JSON.stringify({'openai-codex':{type:'oauth',access:'synthetic',refresh:'synthetic',expires:Date.now()+999999}}));
  const b={requestId:'11111111-1111-4111-8111-111111111111',attemptId:'22222222-2222-4222-8222-222222222222',definitionSha256:'a'.repeat(64),skillsCommit:'b'.repeat(40),operation:'db-proxy.replace-nodes',executionMode:'replacement',objectKind:'db-proxy',objectCode:'proxy',envCode:scope.envCode,clusterId:'cluster',model:'gpt-6-luna',providerId:'codex',connectionVersion:3};
  const {createAssistantMessageEventStream}=await import('@earendil-works/pi-ai/utils/event-stream');
  const {runtime}=await loadPiRuntime({stateRoot:state,provider:'openai-codex'});runtime.getAuth=async()=>({type:'oauth',access:'fixture'});
  let desired=2,expanded=false,newProtected=false,withdrawn=false,restarted=false,oldUnprotected=false,surge=false,round=0,phase=0;
  const writes=[],trafficDuringSurge=[],progress=[];
  const call=(name,args)=>({name,arguments:args}),target={requestId:b.requestId,envCode:b.envCode,proxyCode:'proxy'};
  const cloud=kind=>call('cloud_read',{kind}),metric=kind=>call('metrics_read',{kind}),deployment=call('deployment_read',{});
  const control=[call('mcp__raptor__request_get',{requestId:b.requestId}),call('mcp__raptor__environment_get',{requestId:b.requestId,envCode:b.envCode}),call('mcp__raptor__object_get',{requestId:b.requestId,kind:'db-proxy',code:'proxy'}),call('mcp__raptor__deployments_list',{requestId:b.requestId,envCode:b.envCode,kind:'application',code:'client'}),call('mcp__infra__deployments_list',{envCode:b.envCode,kind:'db-proxy',code:'proxy'}),call('mcp__infra__deployment_status_get',{envCode:b.envCode,appCode:'client',workloadKey:'opaque-app'})];
  const reads=ids=>[cloud('fleet'),cloud('backend'),cloud('capacity'),deployment,metric('pgcatClients'),...ids.map(instanceId=>call('db_connection_probe',{instanceId}))];
  const approval={requestId:b.requestId,actionId:'shrink',interface:'db-proxy.scale-in',envCode:b.envCode,target:{proxyCode:'proxy'},parameters:{desiredCapacity:2}};
  const scripts=[
   [[control[0]],[control[1]],[control[2]],[control[3],control[4]],[control[5]],
    [cloud('fleet')],[cloud('backend'),cloud('capacity')],
    [deployment,metric('pgcatClients'),metric('trafficFailures'),metric('connections')],
    old.map(instanceId=>call('db_connection_probe',{instanceId})),
    [call('mcp__infra__db_proxy_scale',{...target,actionId:'expand',desiredCapacity:4})]],
   [reads(added)],
   [reads(added)],
   [reads(added),
    [call('mcp__infra__db_proxy_node_protection_set',{...target,nodeIds:added,protected:true})],
    [cloud('fleet')],
    [call('mcp__infra__db_proxy_nodes_deregister',{...target,nodeIds:old})],
    reads(added),[call('resource_wait',{seconds:60})]],
   [[control[3],control[4]],[control[5]],reads(added),
    [call('mcp__infra__deployment_restart',{requestId:b.requestId,envCode:b.envCode,appCode:'client',workloadKey:'opaque-app'})],
    [deployment,call('resource_wait',{seconds:60})]],
   [[control[3],control[4]],[control[5]],[cloud('fleet')],[cloud('backend'),cloud('capacity')],
    [deployment,metric('pgcatClients')],added.map(instanceId=>call('db_connection_probe',{instanceId})),
    [call('mcp__infra__db_proxy_node_protection_set',{...target,nodeIds:old,protected:false})],
    [cloud('fleet')],reads(added),[call('mcp__raptor__approval_request',approval)]],
   [[call('mcp__raptor__approval_get',{requestId:b.requestId,approvalId:'33333333-3333-4333-8333-333333333333'}),...reads(added)],
    [call('mcp__infra__db_proxy_scale',{...target,actionId:'shrink',desiredCapacity:2})],[]]
  ];
  runtime.streamSimple=(model)=>{
   const calls=scripts[phase][round++];assert.ok(calls,'unexpected model round');
   const message={role:'assistant',content:calls.length?calls.map((c,i)=>({type:'toolCall',id:`call-${phase}-${round}-${i}`,...c})):[{type:'text',text:'Replacement convergence awaits fixed evidence checks.'}],api:model.api,provider:model.provider,model:model.id,usage:{input:3,output:2,totalTokens:5,cacheRead:0,cacheWrite:0,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}},stopReason:calls.length?'toolUse':'stop',timestamp:Date.now()};
   const stream=createAssistantMessageEventStream();stream.push({type:'done',reason:message.stopReason,message});return stream;
  };
  const current=()=>observations(expanded?(desired===2?added:[...old,...added]):old,{withdrawn:withdrawn?old:[],unprotected:[...(!newProtected?added:[]),...(oldUnprotected?old:[])],zero:restarted,generation:restarted?6:5});
  const observer={cloudRead:async({kind})=>current()[kind],deploymentRead:async()=>{const app=current().deployment;return surge?{...app,observedGeneration:5,readyReplicas:3,updatedReplicas:1}:app;},metricsRead:async({kind})=>{
   if(surge&&kind!=='pgcatClients'){trafficDuringSurge.push(kind);throw Error('InvalidObservation');}
   if(kind==='pgcatClients'&&[1,2].includes(phase))return {readiness:'metrics-warming',instanceIds:[...old,...added],missingInstanceIds:phase===1?added:added.slice(1),observedAt:new Date().toISOString()};
   return current()[kind==='pgcatClients'?'metrics':kind];
  },dbConnectionProbe:async({instanceId})=>({instanceId,connected:!(withdrawn&&old.includes(instanceId))})};
  const fetcher=async(url,init)=>{
   const rpc=JSON.parse(init.body),server=String(url).includes('raptor')?'raptor':'infra';
   if(rpc.method==='notifications/initialized')return new Response(null,{status:202});let result;
   if(rpc.method==='initialize')result={protocolVersion:'2025-06-18',capabilities:{tools:{}},serverInfo:{name:server,version:'1'}};
   else if(rpc.method==='tools/list')result={tools:operationCapabilities(b,scope)[server].map(name=>({name,description:'Scoped operation',inputSchema:operationToolSchema(server,name)}))};
   else if(rpc.method==='tools/call'){
    const name=rpc.params.name,args=rpc.params.arguments,at=new Date().toISOString();let data;
    if(name==='request_get')data={requestId:b.requestId,relatedResources:[{kind:'application',code:scope.application.appCode},{kind:'database',code:scope.targetDbCode}],definition:{envCode:b.envCode,object:{kind:'db-proxy',code:'proxy'},operations:[{name:b.operation,parameters:{mode:'replacement'}}]}};

    else if(name==='deployments_list')data=server==='raptor'?{resource:{kind:'application',code:'client'},envCode:b.envCode,deployments:[{workloadKey:'opaque-app',resourceId:'uid',kind:'application',objectCode:'client',envCode:b.envCode,...scope.application,state:'Running',evidenceMode:'live'}]}:{resource:{envCode:b.envCode,kind:'db-proxy',code:'proxy'},pgproxyWorkload:{type:'ess',code:'proxy',workloadKey:'opaque-proxy',state:'Running',targetDbCode:'db'}};
    else if(name==='deployment_status_get')data={resource:{envCode:b.envCode,kind:'application',code:'client'},applicationWorkload:{type:'kubernetes',workloadKey:'opaque-app',...scope.application,state:'Running'},status:{...current().deployment,objectCode:'client',evidenceMode:'live'}};
    else if(name==='db_proxy_scale'){writes.push([name,args.desiredCapacity]);data={evidenceMode:'live',groupId:'group',previousDesiredCapacity:desired,desiredCapacity:args.desiredCapacity,outcome:args.desiredCapacity===2?'unknown':'submitted'};desired=args.desiredCapacity;expanded=true;}
    else if(name==='db_proxy_node_protection_set'){writes.push([name,args.nodeIds,args.protected]);if(args.protected)newProtected=true;else oldUnprotected=true;data={evidenceMode:'live',instanceIds:args.nodeIds,outcome:'submitted'};}
    else if(name==='db_proxy_nodes_deregister'){writes.push([name,args.nodeIds]);withdrawn=true;data={evidenceMode:'live',instanceIds:args.nodeIds,outcome:'unknown'};}
    else if(name==='deployment_restart'){writes.push([name]);restarted=true;surge=true;data={evidenceMode:'live',accepted:true,generation:6,target:{requestId:b.requestId,...scope.application}};}
    else if(name==='approval_request'||name==='approval_get'){writes.push([name]);data={requestId:b.requestId,approvalId:'33333333-3333-4333-8333-333333333333',actionId:'shrink',binding:approval,state:name==='approval_get'?'approved':'pending'};}
    else data={accepted:true};
    result={structuredContent:{data},content:[{type:'text',text:JSON.stringify({data})}]};
   }else throw Error('UnexpectedRPC');
   return new Response(JSON.stringify({jsonrpc:'2.0',id:rpc.id,result}),{headers:{'Content-Type':'application/json'}});
  };
  const options={stateRoot:state,privateRoot:path.join(root,'private'),runtime,binding:b,replacementScope:scope,observer,skills:{tag:'skills-v99.0.0',commitSHA:b.skillsCommit,path:skillsPath},limits:{model_seconds:90,max_turns:10,max_output_tokens:1024},mcpConfig:{raptor:{url:'https://raptor.fixture/mcp',headers:{Authorization:'Bearer synthetic'}},infra:{url:'https://infra.fixture/mcp',headers:{'X-Infra-Authorization':'Bearer synthetic'}}},fetch:fetcher,onProgress:event=>progress.push(event)};
  const operationStream=runtime.streamSimple;
  const authState=path.join(root,'auth-error-state');
  fs.mkdirSync(authState);
  fs.copyFileSync(path.join(state,'auth.json'),path.join(authState,'auth.json'));
  runtime.streamSimple=model=>{
   const message={role:'assistant',content:[],api:model.api,provider:model.provider,model:model.id,
    usage:{input:0,output:0,totalTokens:0},stopReason:'error',errorMessage:'Your access token has expired. synthetic-private-token',timestamp:Date.now()};
   const stream=createAssistantMessageEventStream();stream.push({type:'error',reason:'error',error:message});return stream;
  };
  const authFailure=await runLiveReplacement({...options,stateRoot:authState,privateRoot:path.join(root,'auth-error-private')});
  assert.equal(authFailure.error,'NeedsSignIn');
  assert.ok(progress.some(e=>e.summary==='Model authentication'));
  assert.ok(progress.some(e=>e.summary?.includes('Contact the platform administrator')));
  assert.ok(!JSON.stringify(progress).includes('synthetic-private-token'));
  assert.deepEqual(writes,[]);
  progress.length=0;
  runtime.streamSimple=operationStream;
  const debugState=path.join(root,'debug-state'),debugPrivate=path.join(root,'debug-private');
  fs.mkdirSync(debugState);
  fs.copyFileSync(path.join(state,'auth.json'),path.join(debugState,'auth.json'));
  const scaleArgs=scripts[0].at(-1)[0].arguments;
  const actionID=scaleArgs.actionId;
  scaleArgs.actionId=' ';
  let held=false;
  const debug=await runLiveReplacement({...options,stateRoot:debugState,privateRoot:debugPrivate,
   signal:AbortSignal.timeout(3000),conversation:{enabled:true},onProgress:event=>{
    progress.push(event);
    if(!event.summary?.startsWith('Expansion was rejected locally'))return;
    held=true;
    fs.writeFileSync(path.join(debugPrivate,'conversation-inbox.json'),JSON.stringify({
     requestId:b.requestId,attemptId:b.attemptId,messageId:'77777777-7777-4777-8777-777777777777',inputSequence:1,text:'/finish'
    }));
   }});
  assert.equal(debug.error,'ToolFailed');
  assert.equal(held,true);
  assert.ok(progress.some(e=>e.kind==='reply'&&e.summary.startsWith('Closing the troubleshooting session')));
  assert.deepEqual(writes,[]);
  progress.length=0;
  scaleArgs.actionId=actionID;
  let previous;
  const counts=[10,1,1,6,5,10,3];
  for(phase=0;phase<scripts.length;phase++){
   round=0;if(phase===5)surge=false;
   const continuation=previous?.paused?{decision:'resume',approvalId:previous.paused.approvalId,actionId:previous.paused.actionId,bindingDigest:previous.paused.bindingDigest}:undefined;
   if(previous?.paused)t.mock.timers.tick(60000);
   const out=await runLiveReplacement({...options,binding:{...b,attemptId:`${phase+2}2222222-2222-4222-8222-222222222222`},sessionReference:previous?.sessionReference,decisionContext:continuation});
   assert.equal(round,counts[phase],JSON.stringify(out));assert.equal(out.usage.length,counts[phase]);
   if(phase<6){assert.equal(out.paused?.kind,phase===5?'approval':'resource',JSON.stringify(out));assert.ok(out.sessionReference);}
   else{assert.equal(out.passed,true,JSON.stringify(out));assert.equal(out.result.replacement.version,2);assert.equal(out.result.replacement.restartGeneration,6);assert.deepEqual(out.result.replacement.newInstanceIds,added);assert.equal(out.result.replacement.acceptance,'pending');}
   if([1,2].includes(phase))assert.deepEqual(writes,[['db_proxy_scale',4]]);
   previous=out;
  }
  assert.deepEqual(trafficDuringSurge,[]);
  const entries=fs.readFileSync(path.join(state,'sessions',previous.sessionReference.file),'utf8').trim().split('\n').map(JSON.parse);
  assert.deepEqual(entries.filter(e=>e.customType==='raptor-replacement-withdrawal-wait-v1').map(e=>e.data.kind),['requested','completed']);
  assert.equal(progress.filter(e=>e.kind==='tool_start'&&e.tool==='resource_wait').length,5);
  assert.equal(progress.filter(e=>e.kind==='tool_result'&&e.tool==='resource_wait'&&e.outcome==='succeeded').length,5);
  assert.equal(progress.filter(e=>e.kind==='tool_start'&&e.tool==='mcp__raptor__request_pause').length,1);
  assert.ok(!entries.some(e=>e.message?.role==='assistant'&&e.message.stopReason==='aborted'));
  assert.deepEqual(writes.filter(w=>w[0]==='db_proxy_scale'),[['db_proxy_scale',4],['db_proxy_scale',2]]);
  assert.equal(writes.filter(w=>w[0]==='deployment_restart').length,1);assert.equal(writes.filter(w=>w[0]==='approval_request').length,1);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
