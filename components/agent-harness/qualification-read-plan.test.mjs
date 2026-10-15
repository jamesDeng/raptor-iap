import test from 'node:test';
import assert from 'node:assert/strict';
import {checkOperationSelectors} from './operation-profile.mjs';
import {qualificationReadBatches} from './qualification-read-plan.mjs';

test('qualification first reads use trusted proxy and application selectors in dependency order',()=>{
 const binding={requestId:'request-bound',envCode:'rdev.ali',objectKind:'db-proxy',objectCode:'DP00001',operation:'db-proxy.replace-nodes',executionMode:'qualification'};
 const scope={envCode:'rdev.ali',proxyCode:'DP00001',groupId:'asg-bound',serverGroupId:'sgp-bound',targetDbCode:'DB00001',application:{envCode:'rdev.ali',appCode:'APP00001',clusterId:'cluster-bound',namespace:'pgcat',name:'client',uid:'uid-bound'}};
 const batches=qualificationReadBatches(binding,scope);
 assert.deepEqual(batches.map(batch=>batch.map(call=>call.tool)),[
  ['mcp__raptor__request_get','mcp__raptor__environment_get','mcp__raptor__object_get'],
  ['mcp__infra__cloud_identity_get','mcp__infra__deployments_list'],
  ['mcp__infra__deployment_status_get'],
  ['cloud_read']
 ]);
 assert.deepEqual(batches[1][1].arguments,{envCode:'rdev.ali',kind:'db-proxy',code:'DP00001'});
 assert.deepEqual(batches[2][0].arguments,{envCode:'rdev.ali',appCode:'APP00001',clusterId:'cluster-bound',namespace:'pgcat',name:'client',uid:'uid-bound'});
 assert.deepEqual(batches[3][0].arguments,{kind:'fleet'});
 for(const call of batches.flat())if(call.tool.startsWith('mcp__')){
  const [,server,tool]=call.tool.split('__');
  assert.doesNotThrow(()=>checkOperationSelectors(server,tool,call.arguments,binding,scope));
 }
});
