import test from 'node:test';import assert from 'node:assert/strict';
import {readPgcatClients} from './pgcat-metrics.mjs';
const scope={envCode:'rdev.ali',proxyCode:'proxy',targetDbCode:'db'};
const rows=query=>['node-a','node-b'].map(ecs_instance_id=>({metric:{job:'pgcat',env:'rdev.ali',db_proxy_code:'proxy',target_db_code:'db',ecs_instance_id,pool:'test',user:'poc_app'},value:[Date.now()/1000,query.startsWith('timestamp(')?String(Date.now()/1000):query.startsWith('up{')?'1':ecs_instance_id==='node-a'?'0':'2']}));
test('complete raw active idle waiting samples distinguish zero-client drained node',async()=>{
 const out=await readPgcatClients({scope,nodeIds:['node-a','node-b'],pool:'test',user:'poc_app',query:async q=>rows(q)});
 assert.equal(out.nodes[0].connectedClients,0);assert.equal(out.nodes[1].connectedClients,6);
});
test('missing pool metrics stale source foreign node duplicate or down scrape refuse zero',async()=>{
 for(const failure of ['missing','stale','foreign','duplicate','down']){
  const query=async q=>{const result=rows(q);if(failure==='missing'&&q.startsWith('pgcat_pools_cl_idle'))result.pop();if(failure==='stale'&&q.startsWith('timestamp('))result[0].value[1]='0';if(failure==='foreign')result[0].metric.ecs_instance_id='foreign';if(failure==='duplicate')result.push(result[0]);if(failure==='down'&&q.startsWith('up{'))result[0].value[1]='0';return result;};
  await assert.rejects(()=>readPgcatClients({scope,nodeIds:['node-a','node-b'],pool:'test',user:'poc_app',query}),/InvalidObservation/);
 }
});
