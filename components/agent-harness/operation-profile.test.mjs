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
 const args={requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',groupId:'group',actionId:'action',desiredCapacity:3};
 assert.doesNotThrow(()=>checkOperationSelectors('infra','db_proxy_scale',args,binding,scope));
 for(const key of ['requestId','envCode','proxyCode','groupId'])assert.throws(()=>checkOperationSelectors('infra','db_proxy_scale',{...args,[key]:'foreign'},binding,scope),/ScopeMismatch/);
 const restart={requestId:'request',envCode:'rdev.ali',...scope.application};
 assert.doesNotThrow(()=>checkOperationSelectors('infra','deployment_restart',restart,binding,scope));
 assert.throws(()=>checkOperationSelectors('infra','deployment_restart',{...restart,appCode:'foreign'},binding,scope),/ScopeMismatch/);
 assert.throws(()=>checkOperationSelectors('infra','deployment_restart',{...restart,uid:'replacement'},binding,scope),/ScopeMismatch/);
 assert.throws(()=>checkOperationSelectors('infra','db_proxy_scale',{...args,unexpected:true},binding,scope),/ScopeMismatch/);
});
test('approval selector verifies full exact target and parameters',()=>{
 const args={requestId:'request',actionId:'action',interface:'ess.scale-in',envCode:'rdev.ali',target:{proxyCode:'proxy',groupId:'group'},parameters:{desiredCapacity:3}};
 assert.doesNotThrow(()=>checkOperationSelectors('raptor','approval_request',args,binding,scope));
 for(const wrong of [{...args,target:{proxyCode:'other',groupId:'group'}},{...args,parameters:{desiredCapacity:3,hidden:true}},{...args,interface:'ecs.delete'}])assert.throws(()=>checkOperationSelectors('raptor','approval_request',wrong,binding,scope),/ScopeMismatch/);
});
test('question profile has no mutation or approval capability',()=>{
 const b={...binding,operation:'application.question',objectKind:'application'};
 const cap=operationCapabilities(b);
 assert.equal(cap.raptor.length,3);
 assert.equal(cap.infra.length,3);
 assert.throws(()=>checkOperationSelectors('infra','db_proxy_scale',{},b,scope),/ScopeMismatch/);
});
test('node command batches obey the Infra API maximum of 50 nodes',()=>{
 const b={requestId:'request',operation:'db-proxy.replace-nodes',objectKind:'db-proxy',envCode:'dev',objectCode:'proxy'};
 const s={envCode:'dev',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',targetDbCode:'db',application:{envCode:'dev',appCode:'client',clusterId:'cluster',namespace:'ns',name:'app',uid:'uid'}};
 assert.throws(()=>checkOperationSelectors('infra','db_proxy_nodes_deregister',{requestId:b.requestId,envCode:'dev',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',instanceIds:Array.from({length:51},(_,i)=>'i-'+i)},b,s),/ScopeMismatch/);
});
