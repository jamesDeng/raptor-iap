import test from 'node:test';import assert from 'node:assert/strict';
import {ApprovalPause} from './approval-pause.mjs';
const binding={requestId:'request',definitionSha256:'a'.repeat(64)};
const args={requestId:'request',actionId:'scale-three',envCode:'rdev.ali',interface:'ess.scale-in',target:{proxyCode:'proxy',groupId:'group'},parameters:{desiredCapacity:3}};
const approval={requestId:'request',approvalId:'approval',actionId:'scale-three',binding:args,state:'pending'};
test('approval pause waits for completed tool round and binds exact action',()=>{
 const pause=new ApprovalPause(binding);pause.requested(args,approval);assert.equal(pause.wait(),undefined);pause.paused({requestId:'request',reason:'waiting_approval'});assert.equal(pause.wait(),undefined);const wait=pause.endRound();assert.equal(wait.kind,'approval');assert.equal(wait.approvalId,'approval');assert.equal(wait.actionId,'scale-three');assert.match(wait.bindingDigest,/^[a-f0-9]{64}$/);assert.equal(pause.endRound(),wait);
 assert.throws(()=>pause.requested({...args,parameters:{desiredCapacity:2}},approval),/InvalidApprovalPause/);
});
test('foreign or altered approval cannot become a wait',()=>{
 for(const invalid of [{...approval,requestId:'other'},{...approval,actionId:'other'},{...approval,binding:{...args,parameters:{desiredCapacity:2}}}])assert.throws(()=>new ApprovalPause(binding).requested(args,invalid),/InvalidApprovalPause/);
 const pause=new ApprovalPause(binding);assert.throws(()=>pause.paused({requestId:'request',reason:'waiting_approval'}),/InvalidApprovalPause/);
});
test('approved receipt is still a pause and never a scale-in execution instruction',()=>{
 const pause=new ApprovalPause(binding);pause.requested(args,{...approval,state:'approved'});pause.paused({requestId:'request',reason:'waiting_approval'});assert.equal(pause.endRound().approvalId,'approval');
});
test('pending approval blocks mutations before another tool in the same round',()=>{
 const pause=new ApprovalPause(binding);pause.requested(args,approval);
 assert.throws(()=>pause.beforeTool('infra','db_proxy_scale'),/ApprovalWaiting/);
 assert.doesNotThrow(()=>pause.beforeTool('raptor','request_pause'));
 pause.paused({requestId:'request',reason:'waiting_approval'});pause.endRound();
 assert.throws(()=>pause.beforeTool('raptor','request_get'),/ApprovalWaiting/);
});

test('resource wait ends a completed round without claiming human approval',()=>{
 const pause=new ApprovalPause(binding);pause.resourceWait({seconds:60});assert.equal(pause.wait(),undefined);assert.throws(()=>pause.beforeTool('infra','db_proxy_scale'),/ApprovalWaiting/);
 const wait=pause.endRound();assert.equal(wait.kind,'resource');assert.equal(wait.approvalId,'');assert.equal(wait.wakeAfterSeconds,60);assert.equal(wait.bindingDigest,binding.definitionSha256);
 assert.throws(()=>new ApprovalPause(binding).resourceWait({seconds:3600}),/InvalidApprovalPause/);
 const approved=new ApprovalPause(binding);approved.requested(args,approval);assert.throws(()=>approved.resourceWait({seconds:60}),/InvalidApprovalPause/);
});
