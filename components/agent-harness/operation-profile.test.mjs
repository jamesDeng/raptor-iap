import test from 'node:test';
import assert from 'node:assert/strict';
import {operationCapabilities,checkOperationSelectors} from './operation-profile.mjs';

const binding={requestId:'request',envCode:'rdev.ali',objectKind:'db-proxy',objectCode:'proxy',operation:'db-proxy.replace-nodes'};
const scope={envCode:'rdev.ali',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',targetDbCode:'db',application:{envCode:'rdev.ali',appCode:'client',clusterId:'cluster',namespace:'raptor-test',name:'traffic',uid:'uid'}};
test('replacement capability profile requires complete matching dependency scope',()=>{
 const cap=operationCapabilities(binding,scope);
 assert.ok(cap.raptor.includes('approval_request'));
 assert.ok(cap.infra.includes('db_proxy_scale'));
 assert.ok(cap.infra.includes('deployment_restart'));
 for(const wrong of [undefined,{...scope,proxyCode:'other'},{...scope,envCode:'prod'},{...scope,application:undefined},{...scope,groupId:''},{...scope,application:{...scope.application,envCode:'prod'}}])assert.throws(()=>operationCapabilities(binding,wrong),/InvalidReplacementScope/);
});
test('replacement selectors bind proxy commands and dependent deployment',()=>{
 const args={requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',actionId:'action',desiredCapacity:3};
 assert.doesNotThrow(()=>checkOperationSelectors('infra','db_proxy_scale',args,binding,scope));
 for(const key of ['requestId','envCode','proxyCode'])assert.throws(()=>checkOperationSelectors('infra','db_proxy_scale',{...args,[key]:'foreign'},binding,scope),/ScopeMismatch/);
 const restart={requestId:'request',envCode:'rdev.ali',appCode:'client',workloadKey:'key'};const discovered=new Map([['key',{appCode:'client'}]]);
 assert.doesNotThrow(()=>checkOperationSelectors('infra','deployment_restart',restart,binding,scope,discovered));
 assert.throws(()=>checkOperationSelectors('infra','deployment_restart',{...restart,appCode:'foreign'},binding,scope),/ScopeMismatch/);
 assert.throws(()=>checkOperationSelectors('infra','deployment_restart',{...restart,uid:'replacement'},binding,scope),/ScopeMismatch/);
 assert.throws(()=>checkOperationSelectors('infra','db_proxy_scale',{...args,unexpected:true},binding,scope),/ScopeMismatch/);
});
test('approval selector verifies full exact target and parameters',()=>{
 const args={requestId:'request',actionId:'action',interface:'db-proxy.scale-in',envCode:'rdev.ali',target:{proxyCode:'proxy'},parameters:{desiredCapacity:3}};
 assert.doesNotThrow(()=>checkOperationSelectors('raptor','approval_request',args,binding,scope));
 for(const wrong of [{...args,target:{proxyCode:'other',groupId:'group'}},{...args,parameters:{desiredCapacity:3,hidden:true}},{...args,interface:'ecs.delete'}])assert.throws(()=>checkOperationSelectors('raptor','approval_request',wrong,binding,scope),/ScopeMismatch/);
});
test('question profile has no mutation or approval capability',()=>{
 const b={...binding,operation:'application.question',objectKind:'application'};
 const cap=operationCapabilities(b);
 assert.equal(cap.raptor.length,3);
 assert.equal(cap.infra.length,2);
 assert.throws(()=>checkOperationSelectors('infra','db_proxy_scale',{},b,scope),/ScopeMismatch/);
});
test('node command batches obey the Infra API maximum of 50 nodes',()=>{
 const b={requestId:'request',operation:'db-proxy.replace-nodes',objectKind:'db-proxy',envCode:'dev',objectCode:'proxy'};
 const s={envCode:'dev',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',targetDbCode:'db',application:{envCode:'dev',appCode:'client',clusterId:'cluster',namespace:'ns',name:'app',uid:'uid'}};
 assert.throws(()=>checkOperationSelectors('infra','db_proxy_nodes_deregister',{requestId:b.requestId,envCode:'dev',proxyCode:'proxy',nodeIds:Array.from({length:51},(_,i)=>'i-'+i)},b,s),/ScopeMismatch/);
});

test('resource-only approvals and commands reject old provider selectors',()=>{
 const scale={requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',actionId:'action',desiredCapacity:3};
 assert.doesNotThrow(()=>checkOperationSelectors('infra','db_proxy_scale',scale,binding,scope));
 assert.throws(()=>checkOperationSelectors('infra','db_proxy_scale',{...scale,groupId:'group'},binding,scope),/ScopeMismatch/);
 const approval={requestId:'request',actionId:'action',interface:'db-proxy.scale-in',envCode:'rdev.ali',target:{proxyCode:'proxy'},parameters:{desiredCapacity:3}};
 assert.doesNotThrow(()=>checkOperationSelectors('raptor','approval_request',approval,binding,scope));
 assert.throws(()=>checkOperationSelectors('raptor','approval_request',{...approval,target:{...approval.target,groupId:'group'}},binding,scope),/ScopeMismatch/);
 for(const tool of ['db_proxy_node_protection_set','db_proxy_nodes_deregister'])assert.doesNotThrow(()=>checkOperationSelectors('infra',tool,{requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',nodeIds:['i-one'],...(tool==='db_proxy_node_protection_set'?{protected:false}:{})},binding,scope));
 assert.ok(!operationCapabilities(binding,scope).infra.includes('cloud_identity_get'));
 assert.doesNotThrow(()=>checkOperationSelectors('raptor','deployments_list',{requestId:'request',envCode:'rdev.ali',kind:'application',code:'client'},binding,scope));
 assert.throws(()=>checkOperationSelectors('raptor','deployments_list',{requestId:'request',envCode:'rdev.ali',kind:'application',code:'foreign'},binding,scope),/ScopeMismatch/);
});
test('resource transport refuses missing or retargeted canonical private proof',async()=>{
 const {validateResourceScope}=await import('./operation-profile.mjs');assert.equal(typeof validateResourceScope,'function');
 const s={envCode:'rdev.ali',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',targetDbCode:'db',accountId:'123',region:'ap-southeast-1',listenerId:'listener',port:6432,databaseResourceId:'rds',resolutionDigest:'bd2faeeca7e56c5b518557cf77c39236d8cb3d1fb09c1e185e8e65900f9804d1',application:{envCode:'rdev.ali',appCode:'client',clusterId:'cluster',namespace:'test',name:'client',uid:'uid'}};
 assert.doesNotThrow(()=>validateResourceScope({...binding,executionMode:'replacement',clusterId:'cluster'},s));
 for(const key of ['accountId','region','listenerId','port','databaseResourceId','resolutionDigest']){const copy={...s};delete copy[key];assert.throws(()=>validateResourceScope({...binding,executionMode:'replacement'},copy),/InvalidEvidence/);}
 assert.throws(()=>validateResourceScope({...binding,executionMode:'replacement'},{...s,listenerId:'retargeted'}),/InvalidEvidence/);
});
