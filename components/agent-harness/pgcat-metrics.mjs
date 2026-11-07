// Use raw pool triplets, never an aggregation that hides a missing node/pool.
const invalid=()=>new Error('InvalidObservation');
export async function readPgcatClients({scope,nodeIds,pool,user,query,allowIncomplete=false}){
 if(!scope||!['envCode','proxyCode','targetDbCode'].every(k=>typeof scope[k]==='string'&&/^[A-Za-z0-9_.-]{1,128}$/.test(scope[k]))||!Array.isArray(nodeIds)||!nodeIds.length||nodeIds.length>4||new Set(nodeIds).size!==nodeIds.length||!pool||!user)throw invalid();
 const selector='{job="pgcat",env='+JSON.stringify(scope.envCode)+',db_proxy_code='+JSON.stringify(scope.proxyCode)+',target_db_code='+JSON.stringify(scope.targetDbCode)+'}';
 const responseTimes=[],sourceTimes=[];
 const missing=new Set();
 const markMissing=instanceId=>{if(!allowIncomplete)throw invalid();missing.add(instanceId);};
 const validRows=rows=>{
  if(!Array.isArray(rows)||!rows.length||rows.length>100)throw invalid();
  for(const r of rows){const m=r.metric,t=Number(r.value?.[0]);if(!m||m.job!=='pgcat'||m.env!==scope.envCode||m.db_proxy_code!==scope.proxyCode||m.target_db_code!==scope.targetDbCode||!nodeIds.includes(m.ecs_instance_id)||!Number.isFinite(t)||Math.abs(Date.now()/1000-t)>30)throw invalid();responseTimes.push(t);}
  return rows;
 };
 const up=validRows(await query('up'+selector)),stamps=validRows(await query('timestamp(up'+selector+')'));
 const scrape=new Map();for(const r of stamps){const id=r.metric.ecs_instance_id,value=Number(r.value?.[1]);if(scrape.has(id)||!Number.isFinite(value)||Math.abs(Date.now()/1000-value)>15)throw invalid();scrape.set(id,value);sourceTimes.push(value);}
 const seen=new Set();for(const r of up){const id=r.metric.ecs_instance_id;if(seen.has(id)||Number(r.value?.[1])!==1)throw invalid();seen.add(id);}
 for(const id of nodeIds)if(!seen.has(id)||!scrape.has(id))markMissing(id);
 const key=r=>JSON.stringify([r.metric.ecs_instance_id,r.metric.pool,r.metric.user]);
 const triplets=new Map();
 for(const kind of ['active','idle','waiting']){
  const name='pgcat_pools_cl_'+kind+selector,values=validRows(await query(name)),times=validRows(await query('timestamp('+name+')')),sources=new Map();
  for(const r of times){const k=key(r),v=Number(r.value?.[1]);if(!r.metric.pool||!r.metric.user||sources.has(k)||!Number.isFinite(v)||Math.abs(Date.now()/1000-v)>15)throw invalid();sources.set(k,r.metric.ecs_instance_id);sourceTimes.push(v);}
  const keys=new Set();for(const r of values){const k=key(r),v=Number(r.value?.[1]);if(!r.metric.pool||!r.metric.user||keys.has(k)||!Number.isSafeInteger(v)||v<0)throw invalid();if(!sources.has(k))markMissing(r.metric.ecs_instance_id);keys.add(k);const row=triplets.get(k)??{instanceId:r.metric.ecs_instance_id,pool:r.metric.pool,user:r.metric.user};row[kind]=v;triplets.set(k,row);}
  for(const [k,instanceId] of sources)if(!keys.has(k))markMissing(instanceId);
 }
 const all=[...triplets.values()];
 for(const row of all)if(!['active','idle','waiting'].every(k=>Object.hasOwn(row,k)))markMissing(row.instanceId);
 const instanceIds=nodeIds.slice().sort();
 const nodes=[];
 for(const instanceId of instanceIds){
  const pools=all.filter(r=>r.instanceId===instanceId);
  if(!pools.some(r=>r.pool===pool&&r.user===user))markMissing(instanceId);
  if(missing.has(instanceId))continue;
  const connectedClients=pools.reduce((total,r)=>total+r.active+r.idle+r.waiting,0);
  if(!Number.isSafeInteger(connectedClients))throw invalid();
  nodes.push({instanceId,connectedClients,pools});
 }
 // Sequential queries must remain fresh as a collection, not just on arrival.
 const completed=Date.now()/1000;
 if(responseTimes.some(t=>Math.abs(completed-t)>30)||sourceTimes.some(t=>Math.abs(completed-t)>15))throw invalid();
 if(missing.size)return {readiness:'metrics-warming',instanceIds,missingInstanceIds:[...missing].sort(),observedAt:new Date(completed*1000).toISOString()};
 return {observedAt:new Date(completed*1000).toISOString(),responseAt:new Date(Math.min(...responseTimes)*1000).toISOString(),sourceAt:new Date(Math.min(...sourceTimes)*1000).toISOString(),latestResponseAt:new Date(Math.max(...responseTimes)*1000).toISOString(),latestSourceAt:new Date(Math.max(...sourceTimes)*1000).toISOString(),nodes};
}
