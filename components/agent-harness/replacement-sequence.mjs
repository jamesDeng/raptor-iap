// Only trusted observation callbacks populate current evidence. Protected session
// permits retain ordering, never current health, across approval/resource waits.
const TYPE='raptor-replacement-sequence-v2',TRACE='raptor-replacement-evidence-v1';
const missing=()=>Error('MissingEvidence'),invalid=()=>Error('InvalidEvidence');
const canonical=v=>JSON.stringify(v,(_k,x)=>x&&typeof x==='object'&&!Array.isArray(x)?Object.fromEntries(Object.keys(x).sort().map(k=>[k,x[k]])):x);
const same=(a,b)=>canonical(a)===canonical(b);
const fresh=(row,after=0)=>row&&typeof row.observedAt==='string'&&Number.isFinite(Date.parse(row.observedAt))&&Math.abs(Date.now()-Date.parse(row.observedAt))<=30000&&Date.parse(row.observedAt)>=after;
const exact=(rows,ids)=>Array.isArray(rows)&&rows.length===ids.length&&new Set(rows.map(n=>n?.instanceId)).size===ids.length&&rows.every(n=>ids.includes(n?.instanceId));
const subset=(ids,allowed)=>Array.isArray(ids)&&ids.length>0&&ids.length<=2&&new Set(ids).size===ids.length&&ids.every(id=>allowed.includes(id));
const healthy=n=>n.healthStatus==='Healthy'&&['InService','Protected'].includes(n.lifecycleState);
const protectedNode=n=>healthy(n)&&n.protectedFromScaleIn===true&&n.lifecycleState==='Protected';
const registered=n=>n.registered===true&&n.healthy===true&&n.status==='Available';
const mcpChecks=['raptor.request_get','raptor.environment_get','raptor.object_get','raptor.deployments_list','infra.deployments_list','infra.deployment_status_get'];
function metricsValid(m,ids,budgeted=[],after=0){
 if(!fresh(m,after)||!Array.isArray(m.pendingInstanceIds)||m.pendingInstanceIds.length||!exact(m.nodes,ids))return false;
 const completed=Date.parse(m.observedAt),sources=[Date.parse(m.sourceAt),Date.parse(m.latestSourceAt)],responses=[Date.parse(m.responseAt),Date.parse(m.latestResponseAt)];
 if(sources.some(t=>!Number.isFinite(t)||Math.abs(Date.now()-t)>15000||Math.abs(completed-t)>15000||t<after)||responses.some(t=>!Number.isFinite(t)||Math.abs(Date.now()-t)>30000||Math.abs(completed-t)>30000||t<after)||sources[0]>sources[1]||responses[0]>responses[1])return false;
 let retiringTotal=0;
 for(const n of m.nodes){
  if(!Number.isSafeInteger(n.connectedClients)||n.connectedClients<0||!Array.isArray(n.pools)||!n.pools.length||n.pools.length>100)return false;
  let total=0;const seen=new Set();
  for(const p of n.pools){const key=JSON.stringify([p.pool,p.user]);if(p.instanceId!==n.instanceId||typeof p.pool!=='string'||!p.pool||typeof p.user!=='string'||!p.user||seen.has(key)||!['active','idle','waiting'].every(k=>Number.isSafeInteger(p[k])&&p[k]>=0))return false;seen.add(key);total+=p.active+p.idle+p.waiting;}
  if(!Number.isSafeInteger(total)||total!==n.connectedClients)return false;
  if(budgeted.includes(n.instanceId))retiringTotal+=total;
 }
 return Number.isSafeInteger(retiringTotal)&&retiringTotal<=1;
}
export class ReplacementSequence {
 constructor(manager,trace,readiness,profile){
  if(profile!=='resource-v1')throw Error('InvalidSkillsRelease');
  this.manager=manager;this.trace=trace;this.readiness=readiness;this.current={};this.probes=new Map();this.checks=new Map();
  const entries=manager.getEntries(),saved=entries.filter(e=>e.type==='custom'&&e.customType===TYPE);
  this.permits=[];
  if(saved.length>16)throw invalid();
  const intents=trace.rows.filter(r=>r.kind==='intent');
  if(saved.length!==intents.length)throw invalid();
  const baselineAt=entries.findIndex(e=>e.type==='custom'&&e.customType===TRACE&&e.data.kind==='baseline'&&same(e.data,this.trace.baseline()));
  for(let i=0;i<saved.length;i++){
   const row=saved[i].data,intent=intents[i];
   if(!row||row.version!==2||Object.keys(row).sort().join(',')!=='identity,key,observedAt,stage,version'||!same(row.identity,trace.identity)||row.key!==intent.key||!Number.isFinite(Date.parse(row.observedAt))||Date.parse(row.observedAt)>Date.parse(intent.observedAt))throw invalid();
   const at=entries.indexOf(saved[i]),intentAt=entries.findIndex(e=>e.type==='custom'&&e.customType===TRACE&&e.data.kind==='intent'&&e.data.key===intent.key);
   if(baselineAt<0||baselineAt>=at||at>=intentAt||(i&&at<=entries.findIndex(e=>e.type==='custom'&&e.customType===TRACE&&e.data.kind==='intent'&&e.data.key===intents[i-1].key)))throw invalid();
   if(i){const priorResultAt=entries.findIndex(e=>e.type==='custom'&&e.customType===TRACE&&e.data.kind==='result'&&e.data.key===intents[i-1].key);if(priorResultAt<0||priorResultAt>=at)throw invalid();}
   let stage;try{stage=this.stage(intent.tool,intent.args);}catch{throw invalid();}
   if(row.stage!==stage)throw invalid();
   if(!['protectOld','expand'].includes(stage)){
    const readyAt=entries.findIndex(e=>e.type==='custom'&&e.customType==='raptor-replacement-readiness-v1');
    if(readyAt<0||readyAt>=at)throw invalid();
   }
   this.permits.push(row);
   const result=trace.rows.find(r=>r.kind==='result'&&r.key===intent.key);
   if(result)this.validateReceipt(intent.tool,intent.args,result.state);
  }
 }
 intents(stage){return this.permits.filter(p=>p.stage===stage).map(p=>this.trace.rows.find(r=>r.kind==='intent'&&r.key===p.key));}
 cohort(){const oldInstanceIds=this.trace.baseline()?.instanceIds;const ids=this.readiness.witness?.evidence.fleet.instances.map(n=>n.instanceId);if(!oldInstanceIds||!ids)throw missing();const newInstanceIds=ids.filter(id=>!oldInstanceIds.includes(id)).sort();if(newInstanceIds.length!==2)throw missing();return {oldInstanceIds:[...oldInstanceIds].sort(),newInstanceIds};}
 stage(tool,args){
  const expanded=this.permits.some(p=>p.stage==='expand'),restarted=this.permits.some(p=>p.stage==='restart'),shrunk=this.permits.some(p=>p.stage==='shrink');
  const old=this.trace.baseline()?.instanceIds??[],added=this.readiness.ready()?this.cohort().newInstanceIds:[];
  if(shrunk)throw Error('MutationAlreadySubmitted');
  if(tool==='db_proxy_scale'){
   if(args.desiredCapacity===4&&!expanded)return 'expand';
   if(args.desiredCapacity===2&&expanded&&restarted&&this.idsFor('unprotectOld').length===2)return 'shrink';
   throw missing();
  }
  if(tool==='db_proxy_node_protection_set'){
   const stage=args.protected===true?(!expanded?'protectOld':'protectNew'):args.protected===false?'unprotectOld':null;
   const allowed=stage==='protectNew'?added:old;
   if(!stage||!subset(args.nodeIds,allowed)||(stage==='protectNew'&&(restarted||this.idsFor('withdraw').length))||(stage==='unprotectOld'&&!restarted)||args.nodeIds.some(id=>this.idsFor(stage).includes(id)))throw missing();
   return stage;
  }
  if(tool==='db_proxy_nodes_deregister'&&expanded&&!restarted&&subset(args.nodeIds,old)&&args.nodeIds.every(id=>!this.idsFor('withdraw').includes(id)))return 'withdraw';
  if(tool==='deployment_restart'&&expanded&&!restarted&&this.idsFor('withdraw').length===2)return 'restart';
  throw missing();
 }
 idsFor(stage){return this.intents(stage).flatMap(i=>i.args.nodeIds??[]);}
 observe(kind,data,args){
  if(kind==='mcp'){for(const row of data??[])if(mcpChecks.includes(row.server+'.'+row.tool))this.checks.set(row.server+'.'+row.tool,row.observedAt);return;}
  if(kind==='sql'){
   if(typeof args?.instanceId!=='string')return;
   this.probes.delete(args.instanceId);
   if(data?.instanceId===args.instanceId)this.probes.set(args.instanceId,{...data,observedAt:Object.hasOwn(data,'observedAt')?data.observedAt:new Date().toISOString()});
  }else if(['fleet','backend','capacity','metrics','deployment','trafficFailures','connections'].includes(kind))this.current[kind]=structuredClone(data);
  this.trace.observe(kind,data);this.readiness.observe(kind,data,args);
 }
 capacity(desired){const c=this.current.capacity;return fresh(c)&&c.groupId===this.trace.identity.scope.groupId&&c.desiredCapacity===desired&&c.minSize===2&&c.maxSize===4;}
 fleet(ids){const f=this.current.fleet;return fresh(f)&&f.groupId===this.trace.identity.scope.groupId&&exact(f.instances,ids)&&f.instances.every(healthy);}
 backend(ids,absent=[]){const b=this.current.backend;return fresh(b)&&b.serverGroupId===this.trace.identity.scope.serverGroupId&&exact(b.nodes,ids)&&b.nodes.every(registered)&&b.nodes.every(n=>!absent.includes(n.instanceId));}
 app(minGeneration=1,after=0){const a=this.current.deployment,s=this.trace.identity.scope.application;return fresh(a,after)&&['clusterId','namespace','name','uid'].every(k=>a[k]===s[k])&&['replicas','readyReplicas','updatedReplicas','availableReplicas'].every(k=>a[k]===2)&&Number.isSafeInteger(a.generation)&&a.generation>=minGeneration&&Number.isSafeInteger(a.observedGeneration)&&a.observedGeneration===a.generation;}
 traffic(kind){const row=this.current[kind];if(!fresh(row)||!Array.isArray(row.samples)||!row.samples.length)return false;const seen=new Set();for(const s of row.samples){const key=s.instance+'|'+(s.outcome??'');if(typeof s.instance!=='string'||!s.instance||seen.has(key)||!Number.isFinite(s.timestamp)||Math.abs(Date.now()/1000-s.timestamp)>30||!Number.isFinite(s.value)||(kind==='connections'?s.value<=0:s.value!==0))return false;seen.add(key);}const ids=[...new Set(row.samples.map(s=>s.instance))];return ids.length===2&&(kind==='connections'?row.samples.length===2:row.samples.length===6&&ids.every(id=>['failure','timeout','ambiguous'].every(outcome=>seen.has(id+'|'+outcome))));}
 probesReady(ids){return ids.every(id=>{const p=this.probes.get(id);return fresh(p)&&p.connected===true&&!Object.hasOwn(p,'readiness');});}
 initial(){const ids=this.trace.baseline()?.instanceIds;if(!ids)return false;return this.capacity(2)&&this.fleet(ids)&&this.backend(ids)&&metricsValid(this.current.metrics,ids)&&this.app()&&this.traffic('trafficFailures')&&this.traffic('connections')&&mcpChecks.every(k=>fresh({observedAt:this.checks.get(k)}))&&this.probesReady(ids);}
 canWrite(){return this.permits.some(p=>p.stage==='expand')?this.readiness.ready():this.initial();}
 retained(requireProtected=true){const {oldInstanceIds:old,newInstanceIds:added}=this.cohort();return this.capacity(4)&&this.fleet([...old,...added])&&(!requireProtected||this.current.fleet.instances.filter(n=>added.includes(n.instanceId)).every(protectedNode));}
 restart(){const intent=this.intents('restart')[0],result=this.trace.rows.find(r=>r.kind==='result'&&r.key===intent?.key);return result&&result.state.accepted===true&&Number.isSafeInteger(result.state.generation)&&result.state.generation>0?{generation:result.state.generation,at:Date.parse(result.observedAt)}:null;}
 drained(){const r=this.restart();if(!r)return false;const {oldInstanceIds:old,newInstanceIds:added}=this.cohort();return this.retained()&&this.probesReady(added)&&this.backend(added,old)&&fresh(this.current.backend,r.at)&&this.app(r.generation,r.at)&&metricsValid(this.current.metrics,[...old,...added],old,r.at);}
 beforeMutation(tool,args){
  if(tool==='approval_request'){
   if(args.parameters?.desiredCapacity!==2||!this.readiness.ready()||!this.drained()||this.idsFor('unprotectOld').length!==2||this.permits.some(p=>p.stage==='shrink')||this.current.fleet.instances.filter(n=>this.cohort().oldInstanceIds.includes(n.instanceId)).some(n=>n.protectedFromScaleIn!==false||n.lifecycleState!=='InService'))throw missing();
   return;
  }
  if(!['db_proxy_scale','db_proxy_node_protection_set','db_proxy_nodes_deregister','deployment_restart'].includes(tool))return;
  const key=this.trace.key(tool,args);if(this.trace.rows.some(r=>r.kind==='intent'&&(r.key===key||(tool==='db_proxy_scale'&&r.tool===tool&&r.args.actionId===args.actionId))))throw Error('MutationAlreadySubmitted');
  if(this.trace.unresolved())throw missing();
  const stage=this.stage(tool,args),s=this.trace.identity.scope;
  if(args.requestId!==this.trace.identity.requestId||args.envCode!==s.envCode)throw missing();
  if(tool==='deployment_restart'){if(args.appCode!==s.application.appCode||typeof args.workloadKey!=='string'||!args.workloadKey)throw missing();}
  else if(args.proxyCode!==s.proxyCode)throw missing();
  if(['protectOld','expand'].includes(stage)){
   if(!this.initial()||(stage==='expand'&&!this.current.fleet.instances.every(protectedNode)))throw missing();
  }else{
   if(!this.readiness.ready()||!this.retained(stage!=='protectNew'))throw missing();
   const {oldInstanceIds:old,newInstanceIds:added}=this.cohort();
   if(['protectNew','withdraw','restart'].includes(stage)&&(!metricsValid(this.current.metrics,[...old,...added])||!this.probesReady(added)))throw missing();
   if(stage==='protectNew'){
    // The new pair may be unprotected before this specific protection operation.
    if(!this.backend([...old,...added]))throw missing();
   }else if(stage==='withdraw'){
    const withdrawn=this.idsFor('withdraw');if(!this.backend([...old,...added].filter(id=>!withdrawn.includes(id))))throw missing();
   }else if(stage==='restart'){
    if(!this.backend(added,old)||!this.app())throw missing();
   }else if(!this.drained()||(stage==='shrink'&&this.current.fleet.instances.filter(n=>old.includes(n.instanceId)).some(n=>n.protectedFromScaleIn!==false||n.lifecycleState!=='InService')))throw missing();
  }
  const row={version:2,identity:structuredClone(this.trace.identity),key,stage,observedAt:new Date().toISOString()};this.manager.appendCustomEntry(TYPE,row);this.permits.push(row);
 }
 validateReceipt(tool,args,data){
  if(tool==='deployment_restart'){if(data.accepted!==true||!Number.isSafeInteger(data.generation)||data.generation<1)throw invalid();}
  else if(tool==='db_proxy_scale'){if(data.desiredCapacity!==args.desiredCapacity||data.previousDesiredCapacity!==(args.desiredCapacity===4?2:4)||!['submitted','unknown'].includes(data.outcome))throw invalid();}
  else if(!subset(data.instanceIds,args.nodeIds)||data.instanceIds.length!==args.nodeIds.length||!['submitted','unknown'].includes(data.outcome))throw invalid();
 }
 onMutationResult(tool,args,receipt){
  if(!['db_proxy_scale','db_proxy_node_protection_set','db_proxy_nodes_deregister','deployment_restart'].includes(tool))return;
  this.validateReceipt(tool,args,receipt);
  if(tool==='db_proxy_scale'){this.current={};this.probes.clear();}
  if(tool==='db_proxy_node_protection_set')delete this.current.fleet;
  if(tool==='db_proxy_nodes_deregister')delete this.current.backend;
  if(tool==='deployment_restart')for(const kind of ['deployment','metrics','trafficFailures','connections'])delete this.current[kind];
 }
 completionEvidence(){
  const cohort=this.cohort(),r=this.restart();
  if(this.trace.unresolved()||this.intents('shrink').length!==1||!r||!this.capacity(2)||!this.fleet(cohort.newInstanceIds)||!this.current.fleet.instances.every(protectedNode)||!this.backend(cohort.newInstanceIds)||!this.app(r.generation,r.at))throw missing();
  return {...cohort,restartGeneration:r.generation};
 }
}
