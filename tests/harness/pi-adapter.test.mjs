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
