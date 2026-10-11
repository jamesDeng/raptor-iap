import test from 'node:test';import assert from 'node:assert/strict';
test('live runner settles/disposes inference before checkpoint and retains answer if checkpoint fails',async()=>{const {runLiveJob}=await import('./live-runner.mjs');const calls=[];const result={answer:'Point-in-time answer'};const deps={loadPiRuntime:async()=>({runtime:{}}),runLiveAppQuestion:async()=>{calls.push('settled');return {passed:true,result}},createCheckpoint:async()=>{calls.push('checkpoint');throw Error('private error')}};const out=await runLiveJob({phase:'live-inference',generation:'generation',state_root:'/tmp/state',private_root:'/tmp/private',mount_root:'/mnt/oss',prefix:'auth',request:{},limits:{},mcp:{}},deps);assert.deepEqual(calls,['settled','checkpoint']);assert.equal(out.passed,false);assert.deepEqual(out.result,result);assert.equal(out.error,'CheckpointFailed')});
test('sign-in failure remains explicit without alternate billing or checkpoint claims',async()=>{const {runLiveJob}=await import('./live-runner.mjs');let model=false;const out=await runLiveJob({phase:'live-inference'}, {loadPiRuntime:async()=>{throw Error('NeedsSignIn')},runLiveAppQuestion:async()=>{model=true},createCheckpoint:async()=>{throw Error('not restored')}});assert.equal(model,false);assert.equal(out.error,'NeedsSignIn');assert.equal(out.checkpoint,undefined)});
test('live runner passes cancellation signal through to the settled model call',async()=>{const {runLiveJob}=await import('./live-runner.mjs');const controller=new AbortController();controller.abort();let received;const out=await runLiveJob({phase:'live-inference'}, {signal:controller.signal,loadPiRuntime:async()=>({runtime:{}}),runLiveAppQuestion:async args=>{received=args.signal;return {passed:false,error:'Cancelled'}},createCheckpoint:async()=>({})});assert.equal(received,controller.signal);assert.equal(out.error,'Cancelled')});
test('replacement dispatch passes pinned skills session and verified continuation, never question adapter',async()=>{
 const {runLiveJob}=await import('./live-runner.mjs');let received;let questions=0;
 const binding={operation:'db-proxy.replace-nodes'};const job={request:{binding},prepared_skills:{tag:'skills-v1.0.0',commitSHA:'b'.repeat(40),path:'/tmp/skills'},replacement_scope:{proxyCode:'proxy'},session_reference:{sessionId:'saved'},decision_context:{approvalId:'approved',decision:'resume'}};
 const out=await runLiveJob(job,{loadPiRuntime:async()=>({runtime:{}}),runLiveAppQuestion:async()=>{questions++;},runLiveReplacement:async args=>{received=args;return {passed:false,paused:{approvalId:'next'}}},createCheckpoint:async()=>({archive_key:'checkpoint'})});
 assert.equal(questions,0);assert.equal(received.binding,binding);assert.equal(received.skills,job.prepared_skills);assert.equal(received.sessionReference,job.session_reference);assert.equal(received.decisionContext,job.decision_context);assert.equal(out.paused.approvalId,'next');assert.equal(out.checkpoint.archive_key,'checkpoint');
});
test('checkpoint failure cannot leave a resumable pause',async()=>{
 const {runLiveJob}=await import('./live-runner.mjs');const out=await runLiveJob({request:{binding:{operation:'db-proxy.replace-nodes'}}},{loadPiRuntime:async()=>({runtime:{}}),runLiveReplacement:async()=>({passed:false,paused:{approvalId:'approval'}}),createCheckpoint:async()=>{throw Error('failed')}});
 assert.equal(out.paused,undefined);assert.equal(out.error,'CheckpointFailed');
});
test('checkpointed approval pause exits cleanly without claiming completion',async()=>{
 const {terminalExitCode}=await import('./runner.mjs');assert.equal(typeof terminalExitCode,'function');
 assert.equal(terminalExitCode({passed:false,paused:{kind:'approval'},checkpoint:{archive_key:'saved'}}),0);
 assert.equal(terminalExitCode({passed:false,paused:{kind:'resource'},checkpoint:{archive_key:'saved'}}),0);
 for(const out of [{passed:false,error:'Timeout'},{passed:false,paused:{kind:'approval'}},{passed:false,paused:{kind:'approval'},checkpoint:{},checkpointError:'CheckpointFailed'}])assert.equal(terminalExitCode(out),1);
});
test('observer path escape is rejected before loading model runtime',async()=>{
 const {runLiveJob}=await import('./live-runner.mjs');let called=false;const result=await runLiveJob({observation_config_path:'/tmp/outside.json',private_root:'/tmp/private',request:{binding:{operation:'db-proxy.replace-nodes'}}},{loadPiRuntime:async()=>{called=true;throw Error('Unexpected');}});assert.equal(called,false);assert.equal(result.passed,false);
});
