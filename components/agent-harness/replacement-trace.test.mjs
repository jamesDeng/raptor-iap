import test from 'node:test';import assert from 'node:assert/strict';
import {ReplacementTrace} from './replacement-trace.mjs';
const binding={requestId:'request',definitionSha256:'a'.repeat(64),skillsCommit:'b'.repeat(40),model:'gpt-5.6-luna'};
const scope={groupId:'group',serverGroupId:'backend',targetDbCode:'db',application:{uid:'app-uid'}};
const manager=()=>({entries:[],getEntries(){return this.entries;},appendCustomEntry(customType,data){this.entries.push({type:'custom',customType,data:structuredClone(data)});}});
const fleet=ids=>({groupId:'group',observedAt:new Date().toISOString(),instances:ids.map(instanceId=>({instanceId,lifecycleState:'Protected',healthStatus:'Healthy',protectedFromScaleIn:true}))});
test('trace survives a verified session reopen and refuses submitted action replay',()=>{
 const session=manager();let trace=new ReplacementTrace(session,binding,scope);trace.observe('fleet',fleet(['old-a','old-b']));
 const args={actionId:'expand',desiredCapacity:4};trace.beforeMutation('db_proxy_scale',args);trace.mutation('db_proxy_scale',args,{previousDesiredCapacity:2,desiredCapacity:4,outcome:'unknown'});
 trace=new ReplacementTrace(session,binding,scope);assert.deepEqual(trace.baseline().instanceIds,['old-a','old-b']);assert.throws(()=>trace.beforeMutation('db_proxy_scale',args),/MutationAlreadySubmitted/);
 assert.throws(()=>new ReplacementTrace(session,{...binding,definitionSha256:'c'.repeat(64)},scope),/InvalidEvidence/);
});
test('a post-expansion fleet cannot become the original two-node baseline',()=>{
 const trace=new ReplacementTrace(manager(),binding,scope);trace.observe('fleet',fleet(['a','b','c','d']));assert.equal(trace.baseline(),undefined);assert.throws(()=>trace.beforeMutation('db_proxy_scale',{actionId:'scale',desiredCapacity:3}),/MissingEvidence/);
});

test('reopened trace rejects malformed or orphaned retained entries',()=>{
 for(const row of [
  {kind:'intent',tool:'db_proxy_scale',args:null,key:'x'},
  {kind:'result',tool:'db_proxy_scale',key:'a'.repeat(64),state:{}},
  {kind:'baseline',instanceIds:['old','old'],observedAt:new Date().toISOString()},
 ]){
  const session=manager();const trace=new ReplacementTrace(session,binding,scope);
  session.appendCustomEntry('raptor-replacement-evidence-v1',{version:1,identity:trace.identity,...row});
  assert.throws(()=>new ReplacementTrace(session,binding,scope),/InvalidEvidence/);
 }
});

test('a lost write response reconciles only an exact submitted receipt from the approved request grant',()=>{
 const bound={...scope,envCode:'dev',proxyCode:'proxy',resolutionDigest:'scope-digest'};
 const trace=new ReplacementTrace(manager(),{...binding,approvalProtocol:'request-grants-v1'},bound);
 trace.observe('fleet',fleet(['old-a','old-b']));
 const args={requestId:'request',envCode:'dev',proxyCode:'proxy',actionId:'expand',desiredCapacity:4};
 trace.beforeMutation('db_proxy_scale',args);
 const row={requestId:'request',envCode:'dev',scopeDigest:'scope-digest',grantId:'approved',actionId:'expand',state:'submitted',
  arguments:args,providerRequestId:'provider:asa-one',capability:{capability:'db-proxy.scale-out',target:{kind:'db-proxy',code:'proxy'}}};
 for(const bad of [{...row,state:'unknown'},{...row,grantId:'foreign'},{...row,arguments:{...args,desiredCapacity:2}}]){
  assert.equal(trace.reconcileSubmitted([bad],'approved'),undefined);
  assert.equal(trace.unresolved(),true);
 }
 assert.equal(trace.reconcileSubmitted([row],'approved')?.receipt.scalingActivityId,'asa-one');
 assert.equal(trace.unresolved(),false);
 assert.throws(()=>trace.beforeMutation('db_proxy_scale',args),/MutationAlreadySubmitted/);
});
