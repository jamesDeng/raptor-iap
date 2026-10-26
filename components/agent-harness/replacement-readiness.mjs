// A submission receipt is not readiness. This witness is written only from
// request-bound observation tools; the verified Pi session protects it on resume.
const TYPE='raptor-replacement-readiness-v1';
const TRACE_TYPE='raptor-replacement-evidence-v1';
const invalid=()=>new Error('InvalidEvidence');
const canonical=value=>JSON.stringify(value,(_key,v)=>v&&typeof v==='object'&&!Array.isArray(v)?Object.fromEntries(Object.keys(v).sort().map(k=>[k,v[k]])):v);
const id=value=>typeof value==='string'&&/^[A-Za-z0-9_.-]{1,128}$/.test(value);
const timestamp=value=>typeof value==='string'?Date.parse(value):NaN;
const exactIds=(nodes,ids)=>Array.isArray(nodes)&&nodes.length===ids.length&&new Set(nodes.map(n=>n?.instanceId)).size===ids.length&&nodes.every(n=>ids.includes(n?.instanceId));
const pick=(value,keys)=>Object.fromEntries(keys.filter(k=>Object.hasOwn(value??{},k)).map(k=>[k,value[k]]));
function sanitize(kind,data){
 if(!data||typeof data!=='object'||Array.isArray(data))return null;
 const out=pick(data,['observedAt']);
 if(kind==='fleet')return {...out,...pick(data,['groupId']),instances:Array.isArray(data.instances)?data.instances.map(n=>pick(n,['instanceId','lifecycleState','healthStatus'])):null};
 if(kind==='backend')return {...out,...pick(data,['serverGroupId']),nodes:Array.isArray(data.nodes)?data.nodes.map(n=>pick(n,['instanceId','status','registered','healthy'])):null};
 if(kind==='capacity')return {...out,...pick(data,['groupId','desiredCapacity','minSize','maxSize'])};
 if(kind==='metrics')return {...out,...pick(data,['pendingInstanceIds','sourceAt','responseAt','latestSourceAt','latestResponseAt']),nodes:Array.isArray(data.nodes)?data.nodes.map(n=>({...pick(n,['instanceId','connectedClients']),pools:Array.isArray(n?.pools)?n.pools.map(p=>pick(p,['instanceId','pool','user','active','idle','waiting'])):null})):null};
 if(kind==='sql')return {...out,...pick(data,['instanceId','connected','readiness'])};
}
function validEvidence(e,trace,expansion,at){
 if(!e||!Number.isFinite(at)||!expansion?.result)return false;
 const {fleet,backend,capacity,metrics,probes}=e,scope=trace.identity.scope,original=trace.baseline()?.instanceIds;
 const state=expansion.result.state;
 if(state.previousDesiredCapacity!==2||state.desiredCapacity!==4||!['submitted','unknown'].includes(state.outcome)||!original||original.length!==2)return false;
 const nodes=fleet?.instances,ids=Array.isArray(nodes)?nodes.map(n=>n?.instanceId):[];
 if(fleet?.groupId!==scope.groupId||!Array.isArray(nodes)||nodes.length!==4||new Set(ids).size!==4||ids.some(v=>!id(v))||original.some(v=>!ids.includes(v))||nodes.some(n=>!['InService','Protected'].includes(n.lifecycleState)||n.healthStatus!=='Healthy'))return false;
 const added=ids.filter(v=>!original.includes(v));
 if(added.length!==2||backend?.serverGroupId!==scope.serverGroupId||!exactIds(backend.nodes,ids)||backend.nodes.some(n=>n.registered!==true||n.healthy!==true||n.status!=='Available'))return false;
 if(capacity?.groupId!==scope.groupId||capacity.desiredCapacity!==4||capacity.minSize!==2||capacity.maxSize!==4)return false;
 if(!Array.isArray(metrics?.pendingInstanceIds)||metrics.pendingInstanceIds.length||!exactIds(metrics.nodes,ids))return false;
 for(const node of metrics.nodes){
  if(!Number.isSafeInteger(node.connectedClients)||node.connectedClients<0||!Array.isArray(node.pools)||!node.pools.length||node.pools.length>100)return false;
  const seen=new Set();let total=0;
  for(const pool of node.pools){
   if(pool.instanceId!==node.instanceId||typeof pool.pool!=='string'||!pool.pool||typeof pool.user!=='string'||!pool.user||!['active','idle','waiting'].every(k=>Number.isSafeInteger(pool[k])&&pool[k]>=0))return false;
   const key=JSON.stringify([pool.pool,pool.user]);if(seen.has(key))return false;seen.add(key);total+=pool.active+pool.idle+pool.waiting;
  }
  if(!Number.isSafeInteger(total)||node.connectedClients!==total)return false;
 }
 if(!exactIds(probes,added)||probes.some(p=>p.connected!==true||Object.hasOwn(p,'readiness')))return false;
 const times=[fleet,backend,capacity,metrics,...probes].map(r=>timestamp(r.observedAt));
 const sources=[timestamp(metrics.sourceAt),timestamp(metrics.latestSourceAt)],responses=[timestamp(metrics.responseAt),timestamp(metrics.latestResponseAt)],completed=timestamp(metrics.observedAt);
 if(sources.some(t=>!Number.isFinite(t)||Math.abs(completed-t)>15000)||responses.some(t=>!Number.isFinite(t)||Math.abs(completed-t)>30000)||sources[0]>sources[1]||responses[0]>responses[1])return false;
 times.push(...sources,...responses);
 const submittedAt=timestamp(expansion.intent.observedAt);
 return times.every(t=>Number.isFinite(t)&&Math.abs(at-t)<=30000&&t>=submittedAt)&&Math.max(...times)-Math.min(...times)<=30000;
}
export class ReplacementReadiness {
 constructor(manager,trace){
  this.manager=manager;this.trace=trace;this.observations={};this.probes=new Map();
  const entries=manager.getEntries(),rows=entries.filter(e=>e.type==='custom'&&e.customType===TYPE);
  if(rows.length>1)throw invalid();
  if(rows.length){
   const row=rows[0].data,expansion=this.expansion();
   if(!row||row.version!==1||row.kind!=='readyFour'||Object.keys(row).sort().join(',')!=='evidence,expansionKey,identity,kind,observedAt,version'||canonical(row.identity)!==canonical(trace.identity)||row.expansionKey!==expansion?.intent.key||Buffer.byteLength(canonical(row))>32768||!validEvidence(row.evidence,trace,expansion,timestamp(row.observedAt)))throw invalid();
   const resultIndex=entries.findIndex(e=>e.type==='custom'&&e.customType===TRACE_TYPE&&e.data.kind==='result'&&e.data.key===row.expansionKey);
   if(resultIndex<0||resultIndex>=entries.indexOf(rows[0]))throw invalid();
   // Require the same allowlisted shape as a newly created witness.
   const clean={...Object.fromEntries(['fleet','backend','capacity','metrics'].map(k=>[k,sanitize(k,row.evidence[k])])),probes:row.evidence.probes.map(p=>sanitize('sql',p))};
   if(canonical(clean)!==canonical(row.evidence))throw invalid();
   this.witness=row;
  }
 }
 expansion(){
  const intents=this.trace.rows.filter(r=>r.kind==='intent'&&r.tool==='db_proxy_scale'&&r.args.desiredCapacity===4);
  if(intents.length!==1)return undefined;
  return {intent:intents[0],result:this.trace.rows.find(r=>r.kind==='result'&&r.key===intents[0].key)};
 }
 // This is a historical stage milestone, not a substitute for Infra's fresh
 // per-mutation gates. It survives approval and 60-second resource waits.
 ready(){return Boolean(this.witness&&this.witness.expansionKey===this.expansion()?.intent.key);}
 blocked(){return this.trace.rows.some(r=>r.kind==='intent'&&r.tool==='db_proxy_scale'&&r.args.desiredCapacity===4)&&!this.ready();}
 observe(kind,data,args){
  if(this.ready()||!this.expansion()?.result||!['fleet','backend','capacity','metrics','sql'].includes(kind))return;
  const clean=sanitize(kind,data);
  if(kind==='sql'){
   // SQL tools perform read-only SELECT 1 and currently return no timestamp.
   // Bind receipt time to the exact requested instance; never accept another ID.
   if(!id(args?.instanceId))return;
   this.probes.delete(args.instanceId);
   if(clean?.instanceId===args.instanceId)this.probes.set(args.instanceId,{...clean,observedAt:Object.hasOwn(clean,'observedAt')?clean.observedAt:new Date().toISOString()});
  }else this.observations[kind]=clean;
  const original=this.trace.baseline()?.instanceIds??[],fleetIds=this.observations.fleet?.instances?.map(n=>n.instanceId)??[];
  const added=fleetIds.filter(instanceId=>!original.includes(instanceId));
  const evidence={...this.observations,probes:[...this.probes.values()].filter(p=>added.includes(p.instanceId))},expansion=this.expansion(),observedAt=new Date().toISOString();
  if(!validEvidence(evidence,this.trace,expansion,Date.parse(observedAt)))return;
  const row={version:1,identity:structuredClone(this.trace.identity),kind:'readyFour',expansionKey:expansion.intent.key,evidence:structuredClone(evidence),observedAt};
  if(Buffer.byteLength(canonical(row))>32768)throw invalid();
  this.manager.appendCustomEntry(TYPE,row);this.witness=row;
 }
}
