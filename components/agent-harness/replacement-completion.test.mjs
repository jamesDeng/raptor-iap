import test from 'node:test';import assert from 'node:assert/strict';
import {qualifyReplacementConvergence} from './replacement-completion.mjs';
const scope={groupId:'group',serverGroupId:'backend',application:{clusterId:'cluster',namespace:'test',name:'client',uid:'uid'}};
const now=()=>new Date().toISOString();
const trace={baseline:()=>({instanceIds:['old-a','old-b']}),unresolved:()=>false,scaleActions:()=>[{actionId:'expand',previousDesiredCapacity:2,desiredCapacity:4,outcome:'submitted'},{actionId:'shrink-a',previousDesiredCapacity:4,desiredCapacity:3,outcome:'submitted'},{actionId:'shrink-b',previousDesiredCapacity:3,desiredCapacity:2,outcome:'unknown'}]};
const observer=()=>({cloudRead:async({kind})=>kind==='capacity'?{groupId:'group',desiredCapacity:2,minSize:2,maxSize:4,observedAt:now()}:kind==='fleet'?{groupId:'group',observedAt:now(),instances:['new-a','new-b'].map(instanceId=>({instanceId,protectedFromScaleIn:true,lifecycleState:'Protected',healthStatus:'Healthy'}))}:{serverGroupId:'backend',observedAt:now(),nodes:['new-a','new-b'].map(instanceId=>({instanceId,registered:true,healthy:true}))},dbConnectionProbe:async({instanceId})=>({instanceId,connected:true}),deploymentRead:async()=>({...scope.application,replicas:2,readyReplicas:2,updatedReplicas:2,availableReplicas:2,observedAt:now()})});
test('fixed observer qualifies convergence without certifying retained traffic acceptance',async()=>{
 const result=await qualifyReplacementConvergence(trace,observer(),scope);
 assert.equal(result.converged,true);assert.equal(result.acceptance,'pending');assert.deepEqual(result.oldInstanceIds,['old-a','old-b']);assert.deepEqual(result.newInstanceIds,['new-a','new-b']);assert.equal(result.desiredCapacity,2);
});
test('missing stages unresolved submissions old nodes and stale health cannot qualify',async()=>{
 for(const t of [{...trace,unresolved:()=>true},{...trace,scaleActions:()=>trace.scaleActions().slice(1)}])await assert.rejects(()=>qualifyReplacementConvergence(t,observer(),scope),/MissingEvidence/);
 const o=observer(),read=o.cloudRead;o.cloudRead=async args=>{const d=await read(args);if(args.kind==='fleet')d.instances[0].instanceId='old-a';return d;};await assert.rejects(()=>qualifyReplacementConvergence(trace,o,scope),/MissingEvidence/);
 const stale=observer(),fresh=stale.cloudRead;stale.cloudRead=async args=>({...await fresh(args),observedAt:'2020-01-01T00:00:00Z'});await assert.rejects(()=>qualifyReplacementConvergence(trace,stale,scope),/MissingEvidence/);
});

test('direct convergence emits only two stages and retains exact readyFour cohort and restart generation',async()=>{
 const direct={...trace,scaleActions:()=>[trace.scaleActions()[0],{actionId:'shrink',previousDesiredCapacity:4,desiredCapacity:2,outcome:'unknown'}]};
 const sequence={cohort:()=>({oldInstanceIds:['old-a','old-b'],newInstanceIds:['new-a','new-b']}),completionEvidence:()=>({oldInstanceIds:['old-a','old-b'],newInstanceIds:['new-a','new-b'],restartGeneration:6})};
 const o=observer();o.deploymentRead=async()=>({...scope.application,replicas:2,readyReplicas:2,updatedReplicas:2,availableReplicas:2,generation:6,observedGeneration:6,observedAt:now()});
 const result=await qualifyReplacementConvergence(direct,o,scope,sequence);
 assert.equal(result.version,2);assert.equal(result.restartGeneration,6);assert.equal(result.actions.length,2);
 const wrong={...sequence,cohort:()=>({oldInstanceIds:['old-a','old-b'],newInstanceIds:['other-a','other-b']})};
 await assert.rejects(()=>qualifyReplacementConvergence(direct,o,scope,wrong),/MissingEvidence/);
 o.deploymentRead=async()=>({...scope.application,replicas:2,readyReplicas:2,updatedReplicas:2,availableReplicas:2,generation:6,observedGeneration:5,observedAt:now()});
 await assert.rejects(()=>qualifyReplacementConvergence(direct,o,scope,sequence),/MissingEvidence/);
 await assert.rejects(()=>qualifyReplacementConvergence(trace,observer(),scope,sequence),/MissingEvidence/);
});
