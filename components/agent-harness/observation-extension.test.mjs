import test from 'node:test';import assert from 'node:assert/strict';
import {observationExtension,OBSERVATION_TOOLS} from './observation-extension.mjs';
test('fixed observer extension registers only bounded read tools',async()=>{
 const registered=[];observationExtension({cloudRead:async args=>({kind:args.kind}),deploymentRead:async()=>({readyReplicas:2}),metricsRead:async()=>({samples:[]})})({registerTool:tool=>registered.push(tool)});
 assert.deepEqual(registered.map(t=>t.name),OBSERVATION_TOOLS);const out=await registered[0].execute('call',{kind:'fleet'});assert.deepEqual(JSON.parse(out.content[0].text),{kind:'fleet'});assert.equal(registered[0].parameters.additionalProperties,false);
});
