import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';
import {runJob} from '../../components/agent-harness/runner.mjs';
test('runner settles inference before checkpoint and keeps model failure separate',async()=>{
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'runner-')));try{
 const calls=[];const job={phase:'inference',state_root:root,mount_root:root,prefix:'auth',generation:'11111111-1111-4111-8111-111111111111',limits:{model_seconds:90,max_turns:3,max_output_tokens:1024}};
 const result=await runJob(job,{loadPiRuntime:async()=>({runtime:{}}),runReadProbe:async()=>{calls.push('settled');return {passed:false,tool_succeeded:false,answer_matches:false,usage:[],error:'ModelFailed'};},createCheckpoint:async()=>{calls.push('saved');return {archive_key:'safe-reference'};}});assert.deepEqual(calls,['settled','saved']);assert.equal(result.passed,false);assert.equal(result.checkpoint.archive_key,'safe-reference');assert.equal(JSON.stringify(result).includes('synthetic-secret'),false);
 const failure=await runJob(job,{loadPiRuntime:async()=>{throw Error('synthetic-secret-do-not-print');}});assert.equal(failure.passed,false);assert.equal(JSON.stringify(failure).includes('synthetic-secret'),false);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('restore failure blocks Pi and emits only fixed safe error',async()=>{
 let loaded=false;const result=await runJob({phase:'restore',reference:{},state_root:'/none'},{restoreCheckpoint:async()=>{throw Error('synthetic-token');},loadPiRuntime:async()=>{loaded=true;}});assert.equal(loaded,false);assert.equal(result.passed,false);assert.equal(JSON.stringify(result).includes('synthetic-token'),false);
});
