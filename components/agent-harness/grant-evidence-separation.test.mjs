import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {loadPiRuntime} from './pi-adapter.mjs';
import {runLiveReplacement} from './live-replacement.mjs';
import {operationCapabilities,operationToolSchema} from './operation-profile.mjs';
import {binding,scope,old,observations} from './replacement-sequence-fixtures.mjs';

test('grant proposal needs no live evidence and an approved write can refresh before any dispatch',async t=>{
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'grant-evidence-')));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const stateRoot=path.join(root,'state');
 fs.mkdirSync(stateRoot);
 fs.writeFileSync(path.join(stateRoot,'auth.json'),JSON.stringify({openai:{type:'oauth',access:'synthetic',refresh:'synthetic',expires:Date.now()+999999}}));
 const b={...binding,attemptId:'22222222-2222-4222-8222-222222222222',operation:'db-proxy.replace-nodes',
  objectKind:'db-proxy',objectCode:scope.proxyCode,envCode:scope.envCode,clusterId:scope.application.clusterId,
  executionMode:'replacement',approvalProtocol:'request-grants-v1'};
 const {requestId,envCode}=b;
 const {proxyCode,application}=scope;
 const capability={capability:'db-proxy.scale-out',target:{kind:'db-proxy',code:proxyCode},constraints:{desiredCapacity:4}};
 const policies={db_proxy_scale:{version:1,variants:[{capability:capability.capability,targetKind:'db-proxy',
  condition:'desired-greater-than-current',requiresHumanApproval:true}]}};
 const grantID='33333333-3333-4333-8333-333333333333';
 const proposal={requestId,envCode,scopeDigest:scope.resolutionDigest,capabilities:[capability]};
 const grant={...proposal,id:grantID,bundleDigest:'c'.repeat(64),version:1,state:'pending'};
 const modelCall=(name,args)=>({type:'toolCall',name,arguments:args});
 const requestRead=modelCall('mcp__raptor__request_get',{requestId});
 const discover=modelCall('mcp__raptor__deployments_list',{requestId,envCode,kind:'application',code:application.appCode});
 const expand=modelCall('mcp__infra__db_proxy_scale',{requestId,envCode,proxyCode,actionId:'expand',desiredCapacity:4});
 const release=modelCall('pgcat_connection_continuity',{phase:'release-probes',observationId:'held-originals',drainActionId:'drain'});
 const {createAssistantMessageEventStream}=await import('@earendil-works/pi-ai/utils/event-stream');
 const {getCurrentTools}=await import('@earendil-works/pi-ai');
 const {runtime}=await loadPiRuntime({stateRoot});
 runtime.getAuth=async()=>({type:'oauth',access:'fixture'});
 const progress=[],writes=[];
 let phase='proposal',round=0,observationsMade=0,canariesReleased=0;
 const visible=[];
 let feedback;
 runtime.streamSimple=(model,context)=>{
  round++;
  visible.push(getCurrentTools(context.messages).map(tool=>tool.name));
  if(phase==='resume'&&round===4)feedback=context.messages.findLast(m=>m.role==='toolResult'&&m.toolName===expand.name);
  const rounds=phase==='proposal'?[
   [requestRead],[modelCall('mcp__raptor__request_grant_request',proposal)]
  ]:phase==='resume'?[
   [requestRead],[modelCall('mcp__raptor__request_grant_get',{requestId,grantId:grantID})],[expand],
   [discover,modelCall('cloud_read',{kind:'fleet'})],
   [modelCall('pgcat_connection_continuity',{phase:'hold'})],
   [requestRead,discover,modelCall('mcp__raptor__environment_get',{requestId,envCode}),
    modelCall('mcp__raptor__object_get',{requestId,kind:'db-proxy',code:proxyCode}),
    modelCall('mcp__infra__deployments_list',{envCode,kind:'db-proxy',code:proxyCode}),
    modelCall('mcp__infra__deployment_status_get',{envCode,appCode:application.appCode,workloadKey:'opaque-app'}),
    modelCall('deployment_read',{}),...['fleet','backend','capacity'].map(kind=>modelCall('cloud_read',{kind})),
    ...old.map(instanceId=>modelCall('db_connection_probe',{instanceId})),
    ...['trafficFailures','connections','pgcatClients'].map(kind=>modelCall('metrics_read',{kind})),expand],
   [expand]
  ]:[
   [requestRead],[modelCall('mcp__raptor__request_grant_get',{requestId,grantId:grantID})],[requestRead,expand],
   [release,requestRead],
  ];
  const calls=rounds[round-1]??[];
  const stopReason=calls.length?'toolUse':'stop';
  const stream=createAssistantMessageEventStream();
  stream.push({type:'done',reason:stopReason,message:{role:'assistant',api:model.api,provider:model.provider,model:model.id,
   content:calls.map((call,i)=>({...call,id:phase+'-'+round+'-'+i})),stopReason,timestamp:Date.now(),
   usage:{input:3,output:2,totalTokens:5,cacheRead:0,cacheWrite:0,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}}}});
  return stream;
 };
 const fetch=async(url,init)=>{
  const rpc=JSON.parse(init.body),server=String(url).includes('raptor.fixture')?'raptor':'infra';
  if(rpc.method==='notifications/initialized')return new Response(null,{status:202});
  let result;
  if(rpc.method==='initialize')result={protocolVersion:'2025-06-18',capabilities:{tools:{}},serverInfo:{name:server,version:'1'}};
  else if(rpc.method==='tools/list')result={tools:operationCapabilities(b,scope)[server].map(name=>({name,description:name,
   inputSchema:operationToolSchema(server,name),...(server==='infra'&&policies[name]?{_meta:{'io.raptor-iap/approval-policy':policies[name]}}:{})}))};
  else if(rpc.method==='tools/call'){
   const {name,arguments:args}=rpc.params;
   const resource={envCode,kind:'application',code:application.appCode};
   const workload={workloadKey:'opaque-app',...application};
   const time=new Date().toISOString();
   const data={
    request_get:{requestId,scopeDigest:scope.resolutionDigest,definitionSha256:b.definitionSha256,operationPrompt:'Follow the bound skill',approvalPolicies:policies,
     relatedResources:[{kind:'application',code:application.appCode},{kind:'database',code:scope.targetDbCode}],
     definition:{object:{kind:b.objectKind,code:proxyCode},envCode,operations:[{name:b.operation,parameters:{mode:b.executionMode,approvalProtocol:b.approvalProtocol}}]}},
    request_grant_request:grant,request_grant_get:{...grant,state:'approved'},environment_get:{},object_get:{},
    deployments_list:server==='raptor'?{resource:{kind:'application',code:application.appCode},envCode,
     deployments:[{...workload,kind:'application',objectCode:application.appCode}]}:{resource:{envCode,kind:'db-proxy',code:proxyCode},
     pgproxyWorkload:{type:'ess',code:proxyCode,workloadKey:'opaque-proxy',targetDbCode:scope.targetDbCode}},
    deployment_status_get:{resource,applicationWorkload:{...workload,type:'kubernetes'},status:{...observations().deployment,evidenceMode:'live',observedAt:time}},
    db_proxy_scale:{actionId:args.actionId,scalingActivityId:'asa-test',evidenceMode:'live',groupId:scope.groupId,previousDesiredCapacity:2,desiredCapacity:4,outcome:'submitted'}
   }[name];
   assert.ok(data,`unexpected remote ${name}`);
   if(name==='db_proxy_scale')writes.push({args,grant:new Headers(init.headers).get('X-Raptor-Request-Grant'),round});
   result={structuredContent:{data},content:[{type:'text',text:JSON.stringify({data})}]};
  }else assert.fail(`unexpected RPC ${rpc.method}`);
  return Response.json({jsonrpc:'2.0',id:rpc.id,result});
 };
 const observed=work=>async args=>{observationsMade++;return work(args);};
 const observer={cloudRead:observed(args=>observations()[args.kind]),deploymentRead:observed(()=>observations().deployment),
  metricsRead:observed(args=>observations()[args.kind==='pgcatClients'?'metrics':args.kind]),
  dbConnectionProbe:observed(args=>({...args,connected:true})),pgcatConnectionContinuity:observed(args=>{
   if(args.phase==='release-probes')canariesReleased++;
   const at=new Date().toISOString();
   return {requestId,scopeDigest:scope.resolutionDigest,observationId:'held-originals',observedAt:at,
    sockets:Object.fromEntries(old.map(id=>[id,{id,lastSqlAt:at}]))};
  })};
 const options={stateRoot,privateRoot:path.join(root,'private'),runtime,binding:b,replacementScope:scope,observer,fetch,
  skills:{tag:'skills-v1.0.0',commitSHA:b.skillsCommit,path:fs.realpathSync(new URL('../../agent-skills',import.meta.url))},
  limits:{max_turns:10,max_output_tokens:1024},onProgress:e=>progress.push(e),
  mcpConfig:{raptor:{url:'https://raptor.fixture/mcp',headers:{Authorization:'Bearer fixture'}},infra:{url:'https://infra.fixture/mcp',headers:{'X-Infra-Authorization':'Bearer fixture'}}}};
 const first=await runLiveReplacement(options);
 assert.equal(first.paused?.kind,'grant',JSON.stringify(first));
 assert.equal(observationsMade,0);
 assert.deepEqual(writes,[]);
 assert.ok(!visible[0].includes('mcp__raptor__request_grant_request'));
 assert.ok(visible[1].includes('mcp__raptor__request_grant_request'));
 assert.ok(!visible[1].includes(expand.name));
 phase='resume';round=0;visible.length=0;
 const resumed=await runLiveReplacement({...options,privateRoot:path.join(root,'resume'),
  binding:{...b,attemptId:'44444444-4444-4444-8444-444444444444'},sessionReference:first.sessionReference,
  decisionContext:{...first.paused,decision:'resume'}});
 assert.ok(!visible[1].includes(expand.name),'the saved bundle alone is insufficient');
 assert.ok(visible[2].includes(expand.name),'an approved command can return its missing evidence');
 assert.equal(feedback?.isError,true);
 assert.match(feedback?.content[0].text,/environment_get/);
 assert.match(feedback?.content[0].text,/object_get/);
 assert.match(feedback?.content[0].text,/pgcat_connection_continuity/);
 assert.equal(progress.filter(e=>e.tool===expand.name&&e.outcome==='failed').length,2);
 assert.ok(progress.some(e=>e.summary?.includes('separate tool round')));
 assert.equal(resumed.paused?.kind,'resource',JSON.stringify(resumed));
 assert.deepEqual(writes,[{args:expand.arguments,grant:grantID,round:7}]);
 phase='after-expansion';round=0;
 const replay=await runLiveReplacement({...options,privateRoot:path.join(root,'after-expansion'),
  binding:{...b,attemptId:'55555555-5555-4555-8555-555555555555'},sessionReference:resumed.sessionReference,
  decisionContext:{...resumed.paused,decision:'resume'}});
 assert.equal(round,5,'a blocked replay must not use the earlier receipt to pause again');
 assert.equal(replay.paused,undefined);
 assert.equal(writes.length,1);
 assert.equal(canariesReleased,0,'a mixed round must not close the old canaries');
});
