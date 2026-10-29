import test from 'node:test';import assert from 'node:assert/strict';import {runGatewayObservation} from './gateway-observation-runner.mjs';
const binding={requestId:'request',definitionSha256:'hash',envCode:'rdev.ali',clusterId:'cluster'};
const config={version:1,requestId:'request',definitionSha256:'hash',envCode:'rdev.ali',accountId:'1360282071200743',region:'ap-southeast-1',groupId:'group',serverGroupId:'backend',dbInstanceId:'db',application:{clusterId:'cluster',namespace:'raptor-test',name:'client',uid:'uid'},clientTargets:['10.70.1.20:9090'],prometheusURL:'http://10.70.1.10:9090',expiresAt:new Date(Date.now()+120000).toISOString(),private:{kubeconfig:'/tmp/raptor-private/kubeconfig',sql:{password:'private'}}};
test('Gateway uses original validation and returns a sanitized deployment projection',async()=>{
 const input={binding,config,kubeconfig:{private:'controller'},tool:'deployment_read',arguments:{}};let received;
 const result=await runGatewayObservation(input,{probe:async probe=>{received=probe;return {metadata:{namespace:'raptor-test',name:'client',uid:'uid',generation:7},spec:{replicas:2,password:'private'},status:{readyReplicas:2,observedGeneration:6}};}});
 assert.equal(result.readyReplicas,2);assert.equal(result.generation,7);assert.equal(result.observedGeneration,6);assert.equal(JSON.stringify(result).includes('private'),false);assert.equal(received.command,'kubectl');assert.deepEqual(received.kubeconfig,input.kubeconfig);
 await assert.rejects(runGatewayObservation({...input,arguments:{name:'other'}},{probe:()=>{throw Error('called');}}),/InvalidObservation/);
 await assert.rejects(runGatewayObservation({...input,tool:'delete'},{probe:()=>{throw Error('called');}}),/InvalidObservation/);
});
