import test from 'node:test';
import assert from 'node:assert/strict';
import {RequestGrantRuntime} from './request-grant-runtime.mjs';

const binding={requestId:'request',definitionSha256:'a'.repeat(64)};
const scope={resolutionDigest:'b'.repeat(64)};
const capability={capability:'db-proxy.scale-in',target:{kind:'db-proxy',code:'DP00001'},constraints:{desiredCapacity:2}};
const policies={db_proxy_scale:{version:1,variants:[{capability:capability.capability,targetKind:capability.target.kind,condition:'desired-less-than-current',requiresHumanApproval:true}]}};
const context={scopeDigest:scope.resolutionDigest,definitionSha256:binding.definitionSha256,operationPrompt:'Use the pinned skill',approvalPolicies:policies};
const id='33333333-3333-4333-8333-333333333333';
const wait={kind:'grant',approvalId:id,actionId:id,bindingDigest:'c'.repeat(64),...binding};
const receipt={requestId:binding.requestId,scopeDigest:scope.resolutionDigest,id,bundleDigest:wait.bindingDigest,state:'approved',version:1,capabilities:[capability]};
function journal(){
 const entries=[];
 return {getEntries:()=>entries,appendCustomEntry:(customType,data)=>entries.push({type:'custom',customType,data})};
}

test('the normal approval proposal requires the complete matching policy from both MCP servers',()=>{
 const runtime=new RequestGrantRuntime(journal(),binding,scope);
 assert.throws(()=>runtime.readContext(context,{}),/InvalidApprovalPause/);
 runtime.readContext(context,policies);
 assert.throws(()=>runtime.beforeProposal({capabilities:[]}),/InvalidApprovalPause/);
 assert.throws(()=>runtime.beforeProposal({capabilities:[{...capability,target:{kind:'application',code:'A00003'}}]}),/InvalidApprovalPause/);
 assert.doesNotThrow(()=>runtime.beforeProposal({capabilities:[capability]}));
});

test('a checkpoint alone authorizes no mutation after an approval or resource wait',()=>{
 const manager=journal();
 new RequestGrantRuntime(manager,binding,scope).recordWait(wait);
 for(const continuation of [{...wait,decision:'resume'},{kind:'resource',decision:'resume'}]){
  const runtime=new RequestGrantRuntime(manager,binding,scope,continuation);
  assert.equal(runtime.approved,undefined);
  assert.throws(()=>runtime.readReceipt(receipt),/InvalidApprovalPause/);
  runtime.readContext(context,policies);
  assert.throws(()=>runtime.beforeRead({grantId:'another-grant'}),/InvalidApprovalPause/);
  assert.throws(()=>runtime.readReceipt({...receipt,state:'denied'}),/InvalidApprovalPause/);
  assert.doesNotThrow(()=>runtime.beforeRead({grantId:id}));
  runtime.readReceipt(receipt);
  assert.equal(runtime.approved.id,id);
 }
 assert.throws(()=>new RequestGrantRuntime(manager,binding,scope,{...wait,decision:'resume',bindingDigest:'d'.repeat(64)}),/InvalidApprovalPause/);
});
