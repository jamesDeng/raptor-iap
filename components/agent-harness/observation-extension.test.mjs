import test from 'node:test';import assert from 'node:assert/strict';
import {observationExtension,observationArgsCategory,OBSERVATION_TOOLS} from './observation-extension.mjs';
test('fixed observer extension registers only bounded read tools',async()=>{
 const registered=[];observationExtension({cloudRead:async args=>({kind:args.kind}),deploymentRead:async()=>({readyReplicas:2}),metricsRead:async()=>({samples:[]})})({registerTool:tool=>registered.push(tool)});
 assert.deepEqual(registered.map(t=>t.name),OBSERVATION_TOOLS);const out=await registered[0].execute('call',{kind:'fleet'});assert.deepEqual(JSON.parse(out.content[0].text),{kind:'fleet'});assert.equal(registered[0].parameters.additionalProperties,false);
});
test('observation argument diagnostics report shape only',()=>{
 assert.equal(observationArgsCategory('cloud_read',{kind:'fleet'}),'args-valid');
 assert.equal(observationArgsCategory('cloud_read',{kind:'private-token'}),'args-invalid');
 assert.equal(observationArgsCategory('deployment_read',{}),'args-valid');
 assert.equal(observationArgsCategory('metrics_read',{kind:'connections',secret:'private-token'}),'args-invalid');
 assert.equal(observationArgsCategory('db_connection_probe',{instanceId:'i-123'}),'args-valid');
 assert.equal(observationArgsCategory('db_connection_probe',{instanceId:'private-token!'}),'args-invalid');
 assert.equal(observationArgsCategory('unlisted',{}),undefined);
});
