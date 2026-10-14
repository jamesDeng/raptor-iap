import test from 'node:test';import assert from 'node:assert/strict';import {QualificationTrace} from './qualification-trace.mjs';
const b={requestId:'r',definitionSha256:'a'.repeat(64),skillsCommit:'b'.repeat(40),model:'gpt-5.6-luna',executionMode:'qualification'};const scope={envCode:'dev',proxyCode:'p',groupId:'g',serverGroupId:'s',targetDbCode:'d',application:{envCode:'dev',appCode:'a',clusterId:'c',namespace:'n',name:'x',uid:'u'}};
const resume={approvalId:'approval',actionId:'action',bindingDigest:'c'.repeat(64),decision:'resume'};
const manager=()=>({rows:[],getEntries(){return this.rows},appendCustomEntry(customType,data){this.rows.push({type:'custom',customType,data})}});
function reads(t){let now=new Date().toISOString();for(const [s,tools] of Object.entries({raptor:['request_get','environment_get','object_get'],infra:['cloud_identity_get','deployments_list','deployment_status_get']}))for(const name of tools)t.mcp([{server:s,tool:name,observedAt:now}]);t.cloud('fleet',{observedAt:now,groupId:'g',instances:['i1','i2'].map(instanceId=>({instanceId,protectedFromScaleIn:true,healthStatus:'Healthy',lifecycleState:'Protected'}))});t.cloud('backend',{observedAt:now,serverGroupId:'s',nodes:['i1','i2'].map(instanceId=>({instanceId,healthy:true,registered:true}))});t.cloud('capacity',{observedAt:now,groupId:'g',desiredCapacity:2});t.observation('deployment.status',{...scope.application,replicas:2,readyReplicas:2,updatedReplicas:2,availableReplicas:2,observedAt:now});for(const k of ['trafficFailures','connections','pgcatClients'])t.observation('metrics.'+k,{observedAt:now});for(const instanceId of ['i1','i2'])t.sql({instanceId,connected:true})}
test('qualification requires all fresh reads and a persisted unchanged baseline plus verified continuation',()=>{const m=manager();const first=new QualificationTrace(m,b,scope);reads(first);assert.throws(()=>first.complete(),/MissingEvidence/);const resumed=new QualificationTrace(m,b,scope,resume);assert.throws(()=>resumed.complete(),/MissingEvidence/);reads(resumed);const out=resumed.complete();assert.equal(out.passed,true);assert.equal(out.readOnly,true);assert.equal(out.checks.length,13);assert.equal(out.approvalId,'approval');const wrong=new QualificationTrace(m,b,scope,resume);assert.throws(()=>wrong.cloud('fleet',{observedAt:new Date().toISOString(),groupId:'g',instances:['other','i2'].map(instanceId=>({instanceId,protectedFromScaleIn:true,healthStatus:'Healthy',lifecycleState:'Protected'}))}),/InvalidEvidence/)});
test('stale observation and denial cannot qualify',()=>{const m=manager();const first=new QualificationTrace(m,b,scope);reads(first);const t=new QualificationTrace(m,b,scope,{...resume,decision:'continue'});assert.throws(()=>t.observation('deployment.status',{observedAt:'2000-01-01T00:00:00Z'}),/InvalidEvidence/);reads(t);assert.throws(()=>t.complete(),/MissingEvidence/)});
test('resumed qualification cannot invent a baseline after approval',()=>{assert.throws(()=>new QualificationTrace(manager(),b,scope,resume),/InvalidEvidence/)});
test('qualification approval requires the complete first read segment',()=>{
 const m=manager(),t=new QualificationTrace(m,b,scope);
 assert.throws(()=>t.requireApprovalReady(),/MissingEvidence/);
 reads(t);
 assert.equal(m.rows.filter(row=>row.customType==='raptor-qualification-baseline-v1').length,1);
 assert.doesNotThrow(()=>t.requireApprovalReady());
 t.checks.delete('metrics.pgcatClients');
 assert.throws(()=>t.requireApprovalReady(),/MissingEvidence/);
});
test('qualification rejects foreign or incomplete client rollout observations',()=>{
 const t=new QualificationTrace(manager(),b,scope),now=new Date().toISOString();
 const ready={...scope.application,objectCode:scope.application.appCode,generation:2,observedGeneration:2,replicas:2,updatedReplicas:2,readyReplicas:2,availableReplicas:2,observedAt:now};
 for(const patch of [{uid:'foreign'},{namespace:'foreign'},{objectCode:'foreign'},{envCode:'foreign'},{readyReplicas:0},{updatedReplicas:1},{availableReplicas:1},{observedGeneration:1}])assert.throws(()=>t.observation('deployment.status',{...ready,...patch}),/InvalidEvidence/);
 t.observation('deployment.status',ready);
});
