import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {scope,old,added,restarted,observe,observations,mutate} from './replacement-sequence-fixtures.mjs';
import {qualifyReplacementConvergence} from './replacement-completion.mjs';

test('Gateway fixture is produced by the native trace readiness sequence and completion path',async t=>{
 t.mock.timers.enable({apis:['Date'],now:Date.parse('2026-10-29T00:00:00Z')});
 const x=restarted();
 mutate(x,'db_proxy_node_protection_set',{instanceIds:old,protected:false},{instanceIds:old,outcome:'submitted'});
 observe(x,observations([...old,...added],{withdrawn:old,unprotected:old,zero:true,generation:6}));
 mutate(x,'db_proxy_scale',{actionId:'shrink',desiredCapacity:2},{previousDesiredCapacity:4,desiredCapacity:2,outcome:'unknown'});
 const final=observations(added,{zero:true,generation:6});
 const observer={cloudRead:async({kind})=>{x.gate.observe(kind,final[kind]);return final[kind];},deploymentRead:async()=>{x.gate.observe('deployment',final.deployment);return final.deployment;},dbConnectionProbe:async({instanceId})=>({instanceId,connected:true})};
 const replacement=await qualifyReplacementConvergence(x.trace,observer,scope,x.gate);
 const result={requestId:'request',attemptId:'attempt',selectedModel:'gpt-5.6-luna',actualModel:'gpt-5.6-luna',answer:'Replacement converged; retained traffic and approval assessment is pending.',replacement,evidence:[],usage:[],generatedAt:new Date().toISOString()};
 const file=new URL('../agent-gateway/internal/execution/testdata/replacement-direct-v2.json',import.meta.url);
 if(process.env.UPDATE_DIRECT_FIXTURE==='1')fs.writeFileSync(file,JSON.stringify(result,null,2)+'\n');
 assert.deepEqual(result,JSON.parse(fs.readFileSync(file,'utf8')));
 assert.equal(replacement.version,2);assert.equal(replacement.restartGeneration,6);
});
