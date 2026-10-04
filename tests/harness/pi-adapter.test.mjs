import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';
import {loadPiRuntime,refreshCredentials,collectProbe} from '../../components/agent-harness/pi-adapter.mjs';
test('real Pi store preserves full OAuth record and stable local host identity',async()=>{
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'pi-local-')));try{
 const record={type:'oauth',access:'synthetic-access',refresh:'synthetic-refresh',expires:Date.now()+3600000,clientId:'issued-preserved',opaque:{x:1}};fs.writeFileSync(path.join(root,'auth.json'),JSON.stringify({openai:record}),{mode:0o600});
 const first=await loadPiRuntime({stateRoot:root});assert.deepEqual(await first.credentials.read('openai'),record);const host=fs.readFileSync(path.join(root,'host-id'),'utf8');await loadPiRuntime({stateRoot:root});assert.equal(fs.readFileSync(path.join(root,'host-id'),'utf8'),host);assert.equal(process.env.OPENAI_API_KEY,undefined);
 const refreshed=await refreshCredentials({credentials:first.credentials,runtime:{getAuth:async()=>{await first.credentials.modify('openai',async c=>({...c,access:'replacement',refresh:'replacement-refresh',expires:Date.now()+3600000}));}},forceExpiry:true});assert.equal(refreshed.refresh_succeeded,true);assert.equal(refreshed.refresh_token_changed,true);assert.equal((await first.credentials.read('openai')).clientId,'issued-preserved');
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('probe observation enforces turn limit and rejects error result',async()=>{
 let aborts=0;const p=collectProbe('marker',3,()=>aborts++);p.observe({type:'turn_start'});p.observe({type:'turn_start'});p.observe({type:'turn_start'});p.observe({type:'turn_start'});assert.equal(aborts,1);assert.equal(p.result().passed,false);
 const good=collectProbe('marker',3,()=>{});good.observe({type:'tool_execution_end',toolName:'read',isError:false,result:{content:[{type:'text',text:'marker'}]}});good.observe({type:'message_end',message:{role:'assistant',content:[{type:'text',text:'marker'}],stopReason:'stop',usage:{input:5,output:2,totalTokens:7}}});assert.equal(good.result().passed,true);
 good.observe({type:'message_end',message:{role:'assistant',content:[],stopReason:'error'}});assert.equal(good.result().passed,false);assert.equal(JSON.stringify(good.result()).includes('synthetic-secret'),false);
});
test('real SDK sessions enforce fresh context, token limits and bounded timeout without a provider call',async()=>{
 const {runReadProbe}=await import('../../components/agent-harness/pi-adapter.mjs');
 const entry=new URL(import.meta.resolve('../../components/agent-harness/node_modules/@earendil-works/pi-coding-agent/dist/index.js'));
 const {createAssistantMessageEventStream}=await import(new URL('../node_modules/@earendil-works/pi-ai/dist/utils/event-stream.js',entry));
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'pi-session-')));
 const originalTimer=global.setTimeout;let deadlineSeen=false;
 try{
  fs.writeFileSync(path.join(root,'auth.json'),JSON.stringify({openai:{type:'oauth',access:'synthetic-access',refresh:'synthetic-refresh',expires:Date.now()+3600000}}));
  const {runtime}=await loadPiRuntime({stateRoot:root});let mode='read',calls=0;const firstContexts=[];
  runtime.streamSimple=(model,context,options)=>{
   assert.equal(options.maxTokens,1024);assert.equal(options.maxRetries,0);
   const stream=createAssistantMessageEventStream();const fixture=context.messages.find(m=>m.role==='user').content.find(c=>c.type==='text').text.match(/Use the read tool to read (.+)\. Reply/)[1];
   const finish=(content,stopReason)=>{const message={role:'assistant',content,api:model.api,provider:model.provider,model:model.id,usage:{input:1,output:1,cacheRead:0,cacheWrite:0,totalTokens:2,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}},stopReason,timestamp:Date.now()};stream.push({type:stopReason==='aborted'?'error':'done',reason:stopReason,...(stopReason==='aborted'?{error:message}:{message})});};
   if(options.signal.aborted){finish([],'aborted');return stream;}
   calls++;
   if(mode==='hang'){options.signal.addEventListener('abort',()=>finish([],'aborted'),{once:true});return stream;}
   const hasTool=context.messages.some(m=>m.role==='toolResult');
   if(!hasTool)firstContexts.push(context.messages.filter(m=>m.role!=="system").map(m=>m.role));
   finish(mode==='repeat'||!hasTool?[{type:'toolCall',id:'read-'+calls,name:'read',arguments:{path:fixture}}]:[{type:'text',text:fs.readFileSync(fixture,'utf8').trim()}],mode==='repeat'||!hasTool?'toolUse':'stop');return stream;
  };
  const limits={model_seconds:90,max_turns:3,max_output_tokens:1024};
  for(let n=0;n<2;n++){calls=0;const r=await runReadProbe({stateRoot:root,runtime,limits});assert.equal(r.passed,true,JSON.stringify({r,calls}));assert.equal(calls,2);}
  assert.deepEqual(firstContexts,[['user'],['user']]);assert.equal(fs.readdirSync(path.join(root,'sessions')).filter(n=>n.endsWith('.jsonl')).length,2);
  mode='repeat';calls=0;const bounded=await runReadProbe({stateRoot:root,runtime,limits});assert.equal(bounded.passed,false);assert.equal(bounded.error,'TurnLimit');assert.ok(calls<=3);
  mode='hang';global.setTimeout=(callback,ms,...args)=>{if(ms===90000){deadlineSeen=true;return originalTimer(callback,5,...args);}return originalTimer(callback,ms,...args);};
  const timed=await runReadProbe({stateRoot:root,runtime,limits});assert.equal(deadlineSeen,true);assert.equal(timed.passed,false);assert.equal(timed.error,'Timeout');
 }finally{global.setTimeout=originalTimer;fs.rmSync(root,{recursive:true,force:true});}
});
