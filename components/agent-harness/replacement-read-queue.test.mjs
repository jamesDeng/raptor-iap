import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {loadPiRuntime} from './pi-adapter.mjs';
import {operationCapabilities,operationToolSchema} from './operation-profile.mjs';
import {runLiveReplacement} from './live-replacement.mjs';

async function runScenario(failFirst=false){
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
  const {runtime}=await loadPiRuntime({stateRoot:state});runtime.getAuth=async()=>({type:'oauth',access:'fixture'});
  let round=0,active=0,maximum=0,serviceCalls=0;
  runtime.streamSimple=(model)=>{
   round++;
   const calls=round===1?[
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
    active++;maximum=Math.max(maximum,active);
    await new Promise(resolve=>setTimeout(resolve,30));
    active--;
    if(failFirst&&serviceCalls===1)return new Response('Unavailable',{status:503});
    const data=server==='raptor'?{requestId:b.requestId,definition:{envCode:b.envCode,object:{kind:b.objectKind,code:b.objectCode},operations:[{name:b.operation,parameters:{mode:'replacement'}}]}}:{envCode:b.envCode,accountMatches:true,evidenceMode:'live',observedAt:new Date().toISOString()};
    result={structuredContent:{data},content:[{type:'text',text:JSON.stringify({data})}]};
   }else throw Error('UnexpectedRPC');
   return new Response(JSON.stringify({jsonrpc:'2.0',id:rpc.id,result}),{headers:{'Content-Type':'application/json'}});
  };
  const read=async()=>{active++;maximum=Math.max(maximum,active);await new Promise(resolve=>setTimeout(resolve,30));active--;return {observedAt:new Date().toISOString()};};
  const observer={cloudRead:read,metricsRead:read,deploymentRead:read,dbConnectionProbe:read};
  const out=await runLiveReplacement({stateRoot:state,privateRoot:path.join(root,'private'),runtime,binding:b,replacementScope:scope,skills:{tag:'skills-v1.0.0',commitSHA:b.skillsCommit,path:skills},limits:{model_seconds:90,max_turns:10,max_output_tokens:1024},mcpConfig:{raptor:{url:'https://raptor.fixture/mcp',headers:{Authorization:'Bearer synthetic'}},infra:{url:'https://infra.fixture/mcp',headers:{'X-Infra-Authorization':'Bearer synthetic'}}},fetch:fetcher,observer});
  if(failFirst){assert.equal(out.error,'ToolFailed',JSON.stringify(out));assert.equal(serviceCalls,1,'a failed read must stop queued service calls');}
  else assert.equal(out.paused?.kind,'resource',JSON.stringify(out));
  assert.equal(maximum,1,'first-turn reads must never overlap at their service boundary');
 }finally{fs.rmSync(root,{recursive:true,force:true});}
}

test('replacement queues simultaneous first reads across MCP and observation tools',()=>runScenario());
test('replacement stops queued reads after the first service failure',()=>runScenario(true));
