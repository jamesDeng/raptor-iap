import {createHash} from 'node:crypto';
import {ReplacementEvidenceRequired} from './replacement-evidence.mjs';
// Only trusted observation callbacks populate current evidence. Protected session
// permits retain ordering, never current health, across approval/resource waits.
const WAIT='raptor-replacement-withdrawal-wait-v1';
const DRAIN='raptor-pgcat-drain-v1';
const PROBE_RELEASE='raptor-pgcat-probe-release-v1';
const TRAFFIC='raptor-pgcat-traffic-v1';
const FINAL_TRAFFIC='raptor-pgcat-final-traffic-v1';
const digest=v=>createHash('sha256').update(canonical(v)).digest('hex');
const TYPE='raptor-replacement-sequence-v2',TRACE='raptor-replacement-evidence-v1';
const missing=()=>Error('MissingEvidence'),invalid=()=>Error('InvalidEvidence');
const canonical=v=>JSON.stringify(v,(_k,x)=>x&&typeof x==='object'&&!Array.isArray(x)?Object.fromEntries(Object.keys(x).sort().map(k=>[k,x[k]])):x);
const same=(a,b)=>canonical(a)===canonical(b);
const observed=(row,after=0)=>row&&typeof row.observedAt==='string'&&Number.isFinite(Date.parse(row.observedAt))&&Date.parse(row.observedAt)>=after;
const exact=(rows,ids)=>Array.isArray(rows)&&rows.length===ids.length&&new Set(rows.map(n=>n?.instanceId)).size===ids.length&&rows.every(n=>ids.includes(n?.instanceId));
const toolID=value=>typeof value==='string'&&/^[-A-Za-z0-9_.]{1,128}$/.test(value);
const subset=(ids,allowed)=>Array.isArray(ids)&&ids.length>0&&ids.length<=2&&new Set(ids).size===ids.length&&ids.every(id=>allowed.includes(id));
const healthy=n=>n.healthStatus==='Healthy'&&['InService','Protected'].includes(n.lifecycleState);
const protectedNode=n=>healthy(n)&&n.protectedFromScaleIn===true&&n.lifecycleState==='Protected';
const registered=n=>n?.registered===true&&n.healthy===true&&n.status==='Available';
const backendMatches=(rows,receiving,drained)=>exact(rows,[...receiving,...drained])&&rows.every(n=>
 receiving.includes(n.instanceId)?registered(n):n.registered===false&&n.status==='Available');
const mcpChecks=['raptor.request_get','raptor.environment_get','raptor.object_get','raptor.deployments_list','infra.deployments_list','infra.deployment_status_get'];
const withdrawalChecks=['raptor.deployments_list','infra.deployments_list','infra.deployment_status_get'];
const missingReads=checks=>checks.filter(([valid])=>!valid).map(([,read])=>read);
function metricsValid(m,ids,budgeted=[],after=0){
 if(!observed(m,after)||!Array.isArray(m.pendingInstanceIds)||m.pendingInstanceIds.length||!exact(m.nodes,ids))return false;
 const sources=[Date.parse(m.sourceAt),Date.parse(m.latestSourceAt)],responses=[Date.parse(m.responseAt),Date.parse(m.latestResponseAt)];
 if(sources.some(t=>!Number.isFinite(t)||t<after)||responses.some(t=>!Number.isFinite(t)||t<after)||sources[0]>sources[1]||responses[0]>responses[1])return false;
 let retiringTotal=0;
 for(const n of m.nodes){
  if(!Number.isSafeInteger(n.connectedClients)||n.connectedClients<0||!Array.isArray(n.pools)||!n.pools.length||n.pools.length>100)return false;
  let total=0;const seen=new Set();
  for(const p of n.pools){const key=JSON.stringify([p.pool,p.user]);if(p.instanceId!==n.instanceId||typeof p.pool!=='string'||!p.pool||typeof p.user!=='string'||!p.user||seen.has(key)||!['active','idle','waiting'].every(k=>Number.isSafeInteger(p[k])&&p[k]>=0))return false;seen.add(key);total+=p.active+p.idle+p.waiting;}
  if(!Number.isSafeInteger(total)||total!==n.connectedClients)return false;
  if(budgeted.includes(n.instanceId))retiringTotal+=total;
 }
 return Number.isSafeInteger(retiringTotal)&&retiringTotal===0;
}
export class ReplacementSequence {
 constructor(manager,trace,readiness,profile,decisionContext){
  if(profile!=='resource-v1')throw Error('InvalidSkillsRelease');
  this.manager=manager;this.trace=trace;this.readiness=readiness;this.current={};this.probes=new Map();this.checks=new Map();
  const entries=manager.getEntries(),saved=entries.filter(e=>e.type==='custom'&&e.customType===TYPE);
  this.permits=[];
  if(trace.versioned){
   this.trafficProof=entries.findLast(e=>e.type==='custom'&&e.customType===TRAFFIC)?.data;
   this.finalTraffic=entries.findLast(e=>e.type==='custom'&&e.customType===FINAL_TRAFFIC)?.data;
  }
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
  this.loadWithdrawalWait(entries,decisionContext);
  this.loadDrainProof(entries);
  const releases=entries.filter(e=>e.type==='custom'&&e.customType===PROBE_RELEASE);
  if(releases.length){
   if(releases.length!==1||entries.indexOf(releases[0])<=entries.findIndex(e=>e.type==='custom'&&e.customType===DRAIN))throw invalid();
   this.probeRelease=this.validateProbeRelease(releases[0].data);
  }
 }
 loadWithdrawalWait(entries,decision){
  const saved=entries.filter(e=>e.type==='custom'&&e.customType===WAIT);
  if(saved.length>2)throw invalid();
  if(!saved.length){if(this.intents('restart').length)throw invalid();return;}
  const requested=saved[0].data,at=entries.indexOf(saved[0]),wait=requested?.wait;
  if(!requested||Object.keys(requested).sort().join(',')!=='cohort,evidence,identity,kind,observedAt,version,wait,withdrawalKeys'||![1,2].includes(requested.version)||requested.kind!=='requested'||!same(requested.identity,this.trace.identity)||!same(requested.cohort,this.cohort())||Buffer.byteLength(canonical(requested))>32768)throw invalid();
  const withdrawals=this.intents('withdraw');
  if(this.trace.versioned&&!this.submittedWithdrawal())throw invalid();
  if(this.idsFor('withdraw').length!==2||!same(requested.withdrawalKeys,withdrawals.map(i=>i.key))||!wait||Object.keys(wait).sort().join(',')!=='actionId,approvalId,bindingDigest,definitionSha256,kind,requestId,startedAt,wakeAfterSeconds'||wait.kind!=='resource'||wait.approvalId!==''||!/^capacity-[a-f0-9-]{36}$/.test(wait.actionId)||wait.requestId!==this.trace.identity.requestId||wait.definitionSha256!==this.trace.identity.definitionSha256||wait.bindingDigest!==wait.definitionSha256||wait.wakeAfterSeconds!==60||!Number.isFinite(Date.parse(wait.startedAt)))throw invalid();
  const begun=Date.parse(wait.startedAt),e=requested.evidence,c=requested.cohort;
  if(!Number.isFinite(Date.parse(requested.observedAt))||Date.parse(requested.observedAt)<begun)throw invalid();
  const resultAts=withdrawals.map(i=>entries.findIndex(e=>e.type==='custom'&&e.customType===TRACE&&e.data.kind==='result'&&e.data.key===i.key));
  if(resultAts.some(i=>i<0||i>=at))throw invalid();
  const lastResult=Math.max(...resultAts.map(i=>Date.parse(entries[i].data.observedAt)));
  if(!e||Object.keys(e).sort().join(',')!=='backend,capacity,fleet'||Object.values(e).some(r=>!r||!Number.isFinite(Date.parse(r.observedAt))||Date.parse(r.observedAt)<lastResult||Date.parse(r.observedAt)>begun)||Object.keys(e.capacity).sort().join(',')!=='desiredCapacity,groupId,maxSize,minSize,observedAt'||Object.keys(e.fleet).sort().join(',')!=='groupId,instances,observedAt'||Object.keys(e.backend).sort().join(',')!=='nodes,observedAt,serverGroupId'||e.capacity.groupId!==this.trace.identity.scope.groupId||e.capacity.desiredCapacity!==4||e.capacity.minSize!==2||e.capacity.maxSize!==4||e.fleet.groupId!==this.trace.identity.scope.groupId||!exact(e.fleet.instances,[...c.oldInstanceIds,...c.newInstanceIds])||!e.fleet.instances.every(n=>Object.keys(n).sort().join(',')==='healthStatus,instanceId,lifecycleState,protectedFromScaleIn'&&protectedNode(n))||e.backend.serverGroupId!==this.trace.identity.scope.serverGroupId||!backendMatches(e.backend.nodes,c.newInstanceIds,requested.version===2?c.oldInstanceIds:[])||!e.backend.nodes.every(n=>Object.keys(n).sort().join(',')==='healthy,instanceId,registered,status'))throw invalid();
  this.withdrawalRequested=requested;
  if(saved.length===2){
   const completed=saved[1].data;
   if(!completed||Object.keys(completed).sort().join(',')!=='identity,kind,requestedSha256,resumedAt,version,waitActionId'||completed.version!==1||completed.kind!=='completed'||!same(completed.identity,this.trace.identity)||completed.waitActionId!==wait.actionId||completed.requestedSha256!==digest(requested)||!Number.isFinite(Date.parse(completed.resumedAt))||Date.parse(completed.resumedAt)<begun+60000||Date.parse(completed.resumedAt)>Date.now())throw invalid();
   this.withdrawalCompleted=completed;
  }else if(decision?.decision==='resume'&&decision.approvalId===''&&decision.actionId===wait.actionId&&decision.bindingDigest===wait.bindingDigest&&Date.now()>=begun+60000){
   const completed={version:1,kind:'completed',identity:structuredClone(this.trace.identity),waitActionId:wait.actionId,requestedSha256:digest(requested),resumedAt:new Date().toISOString()};
   this.manager.appendCustomEntry(WAIT,completed);this.withdrawalCompleted=completed;
  }else throw missing();
  const restart=this.permits.find(p=>p.stage==='restart');
  if(restart){const restartAt=entries.findIndex(e=>e.type==='custom'&&e.customType===TYPE&&e.data.key===restart.key);if(saved.length!==2||entries.indexOf(saved[1])>=restartAt||Date.parse(restart.observedAt)<Date.parse(this.withdrawalCompleted.resumedAt))throw invalid();}
 }
 needsWithdrawalWait(){return !this.withdrawalRequested&&!this.intents('restart').length&&this.idsFor('withdraw').length===2;}
 withdrawalWaitReads(){
  if(!this.needsWithdrawalWait())return [];
  const {oldInstanceIds:old,newInstanceIds:added}=this.cohort();
  const withdrawals=this.intents('withdraw');
  const results=withdrawals.map(i=>this.trace.rows.find(r=>r.kind==='result'&&r.key===i.key));
  if(this.trace.unresolved()||results.some(r=>!r))return ['request_grant_actions_list'];
  if(this.trace.versioned&&!this.submittedWithdrawal())throw invalid();
  const lastResult=Math.max(...results.map(r=>Date.parse(r.observedAt)));
  return missingReads([
   [observed(this.current.fleet,lastResult)&&this.fleet([...old,...added])&&this.current.fleet.instances.every(protectedNode),'cloud_read fleet'],
   [observed(this.current.backend,lastResult)&&this.backend(added,old),'cloud_read backend'],
   [observed(this.current.capacity,lastResult)&&this.capacity(4),'cloud_read capacity'],
  ]);
 }
 recordResourceWait(wait){
  if(wait?.kind!=='resource'||!this.needsWithdrawalWait()||this.withdrawalWaitReads().length)return;
  const pick=(value,keys)=>Object.fromEntries(keys.map(k=>[k,value[k]]));
  const f=this.current.fleet,b=this.current.backend,c=this.current.capacity;
  const evidence={fleet:{...pick(f,['observedAt','groupId']),instances:f.instances.map(n=>pick(n,['instanceId','lifecycleState','healthStatus','protectedFromScaleIn']))},backend:{...pick(b,['observedAt','serverGroupId']),nodes:b.nodes.map(n=>pick(n,['instanceId','status','registered','healthy']))},capacity:pick(c,['observedAt','groupId','desiredCapacity','minSize','maxSize'])};
  const row={version:2,kind:'requested',identity:structuredClone(this.trace.identity),cohort:this.cohort(),withdrawalKeys:this.intents('withdraw').map(i=>i.key),evidence,wait:structuredClone(wait),observedAt:new Date().toISOString()};
  if(Buffer.byteLength(canonical(row))>32768)throw invalid();
  this.manager.appendCustomEntry(WAIT,row);this.withdrawalRequested=row;
 }
 withdrawalReads(){
  const at=Date.parse(this.withdrawalCompleted?.resumedAt);
  if(!Number.isFinite(at))return ['cloud_read fleet, backend and capacity after drain, then resource_wait'];
  const {oldInstanceIds,newInstanceIds}=this.cohort();
  return missingReads([
   [metricsValid(this.current.metrics,[...oldInstanceIds,...newInstanceIds],[],at),'metrics_read pgcatClients'],
   ...['fleet','backend','capacity'].map(k=>[observed(this.current[k],at),'cloud_read '+k]),
   [observed(this.current.deployment,at),'deployment_read'],
   ...withdrawalChecks.map(k=>[observed({observedAt:this.checks.get(k)},at),k]),
  ]);
 }
 intents(stage){return this.permits.filter(p=>p.stage===stage).map(p=>this.trace.rows.find(r=>r.kind==='intent'&&r.key===p.key));}
 cohort(){const oldInstanceIds=this.trace.baseline()?.instanceIds;const ids=this.readiness.witness?.evidence.fleet.instances.map(n=>n.instanceId);if(!oldInstanceIds||!ids)throw missing();const newInstanceIds=ids.filter(id=>!oldInstanceIds.includes(id)).sort();if(newInstanceIds.length!==2)throw missing();return {oldInstanceIds:[...oldInstanceIds].sort(),newInstanceIds};}
 stage(tool,args){
  const expanded=this.permits.some(p=>p.stage==='expand'),restarted=this.permits.some(p=>p.stage==='restart'),shrunk=this.permits.some(p=>p.stage==='shrink');
  const old=this.trace.baseline()?.instanceIds??[],added=this.readiness.ready()?this.cohort().newInstanceIds:[];
  if(shrunk)throw Error('MutationAlreadySubmitted');
  if(tool==='db_proxy_scale'){
   if(args.desiredCapacity===4&&!expanded)return 'expand';
   if(args.desiredCapacity===2&&expanded&&restarted&&(this.trace.versioned||this.idsFor('unprotectOld').length===2))return 'shrink';
   throw missing();
  }
  if(tool==='db_proxy_node_protection_set'){
   const stage=args.protected===true?(!expanded?'protectOld':'protectNew'):args.protected===false?'unprotectOld':null;
   const allowed=stage==='protectNew'?added:old;
   if(!stage||(this.trace.versioned&&(stage!=='protectNew'||args.nodeIds.length!==2))||!subset(args.nodeIds,allowed)||(stage==='protectNew'&&(restarted||this.idsFor('withdraw').length))||(stage==='unprotectOld'&&!restarted)||args.nodeIds.some(id=>this.idsFor(stage).includes(id)))throw missing();
   return stage;
  }
  if(tool==='db_proxy_nodes_deregister'&&expanded&&!restarted&&(!this.trace.versioned||args.nodeIds.length===2)&&subset(args.nodeIds,old)&&args.nodeIds.every(id=>!this.idsFor('withdraw').includes(id)))return 'withdraw';
  if(tool==='deployment_restart'&&expanded&&!restarted&&this.idsFor('withdraw').length===2)return 'restart';
  throw missing();
 }
 continuityReads(args){
  if(!['verify-drain','release-probes'].includes(args?.phase))return [];
  if(!this.withdrawalCompleted)return ['cloud_read fleet, backend and capacity after drain, then resource_wait'];
  if(args.phase==='release-probes'&&!this.savedDrainProof(args))return ['pgcat_connection_continuity verify-drain'];
  return [];
 }
 savedDrainProof(args){
  if(this.drainProof&&(this.drainProof.observationId!==args.observationId||this.drainProof.drainActionId!==args.drainActionId))throw invalid();
  return this.drainProof;
 }
 submittedWithdrawal(){
  const withdrawals=this.intents('withdraw');
  if(withdrawals.length!==1)return undefined;
  const receipt=this.trace.rows.find(row=>row.kind==='result'&&row.key===withdrawals[0].key);
  return receipt?.state.outcome==='submitted'&&toolID(receipt.state.actionId)?receipt:undefined;
 }
 validateDrainProof(proof){
  const receipt=this.submittedWithdrawal();
  if(!this.trace.versioned||!this.withdrawalCompleted||!receipt||!proof||proof.drainActionId!==receipt.state.actionId||
   Object.keys(proof).sort().join(',')!=='drainActionId,drainObservedAt,freshConnections,observationId,observedAt,requestId,scopeDigest,sockets'||
   proof.requestId!==this.trace.identity.requestId||proof.scopeDigest!==this.trace.identity.scope.resolutionDigest||
   !toolID(proof.observationId)||!observed(proof)||!observed({observedAt:proof.drainObservedAt},Date.parse(this.withdrawalCompleted.resumedAt)))throw invalid();
  const {oldInstanceIds:old,newInstanceIds:added}=this.cohort(),drainAt=Date.parse(proof.drainObservedAt),observedAt=Date.parse(proof.observedAt);
  if(!same(Object.keys(proof.sockets??{}).sort(),old)||!same(Object.keys(proof.freshConnections??{}).sort(),added))throw invalid();
  const sockets=Object.values(proof.sockets),counts=Object.values(proof.freshConnections);
  if(sockets.some(s=>!toolID(s?.id)||Object.keys(s).sort().join(',')!=='id,lastSqlAt,openedAt,rounds'||
    !Number.isFinite(Date.parse(s.openedAt))||Date.parse(s.openedAt)>=drainAt||
    !Number.isFinite(Date.parse(s.lastSqlAt))||Date.parse(s.lastSqlAt)<drainAt||Date.parse(s.lastSqlAt)>observedAt||!Number.isSafeInteger(s.rounds)||s.rounds<2)||
   new Set(sockets.map(s=>s.id)).size!==old.length||counts.some(n=>!Number.isSafeInteger(n)||n<1)||counts.reduce((a,b)=>a+b,0)!==24)throw invalid();
 }
 loadDrainProof(entries){
  const saved=entries.filter(e=>e.type==='custom'&&e.customType===DRAIN);
  if(!saved.length)return;
  const entry=saved[0],row=entry.data,index=entries.indexOf(entry);
  const completed=entries.findIndex(e=>e.type==='custom'&&e.customType===WAIT&&e.data.kind==='completed');
  const restart=this.permits.find(p=>p.stage==='restart');
  if(saved.length!==1||!row||row.version!==1||Object.keys(row).sort().join(',')!=='identity,proof,version,waitSha256'||
   Buffer.byteLength(canonical(row))>8192||!same(row.identity,this.trace.identity)||
   completed<0||completed>=index||row.waitSha256!==digest(this.withdrawalCompleted)||
   (restart&&index>=entries.findIndex(e=>e.type==='custom'&&e.customType===TYPE&&e.data.key===restart.key)))throw invalid();
  this.validateDrainProof(row.proof);
  this.drainProof=structuredClone(row.proof);
 }
 validateProbeRelease(receipt){
  const proof=this.drainProof;
  if(!proof||!receipt||['requestId','scopeDigest','observationId','drainActionId'].some(k=>receipt[k]!==proof[k])||
   !observed({observedAt:receipt.releasedAt},Date.parse(proof.observedAt))||
   !observed(receipt,Date.parse(receipt.releasedAt)))throw invalid();
  const {requestId,scopeDigest,observationId,drainActionId,observedAt,releasedAt}=receipt;
  return {requestId,scopeDigest,observationId,drainActionId,observedAt,releasedAt};
 }
 observeContinuity(args,result){
  if(!this.trace.versioned||!result||result.readiness==='traffic-warming')return;
  if(args.phase==='hold'){
   const ids=this.trace.baseline()?.instanceIds;
   if(!ids||result.requestId!==this.trace.identity.requestId||result.scopeDigest!==this.trace.identity.scope.resolutionDigest||
    !observed(result)||!same(Object.keys(result.sockets??{}).sort(),[...ids].sort()))throw invalid();
   const prior=this.current.hold;
   if(prior&&(prior.observationId!==result.observationId||ids.some(id=>prior.sockets[id].id!==result.sockets[id].id)))throw invalid();
   this.current.hold=result;
  }else if(args.phase==='verify-drain'){
   this.validateDrainProof(result);
   if(result.observationId!==args.observationId||result.drainActionId!==args.drainActionId||this.drainProof)throw invalid();
   const row={version:1,identity:this.trace.identity,waitSha256:digest(this.withdrawalCompleted),proof:result};
   if(Buffer.byteLength(canonical(row))>8192)throw invalid();
   this.manager.appendCustomEntry(DRAIN,row);
   this.drainProof=structuredClone(result);
  }else if(args.phase==='release-probes'){
   this.savedDrainProof(args);
   const receipt=this.validateProbeRelease(result);
   if(this.probeRelease){
    if(this.probeRelease.releasedAt!==receipt.releasedAt)throw invalid();
    return;
   }
   this.manager.appendCustomEntry(PROBE_RELEASE,receipt);
   this.probeRelease=receipt;
  }else if(['verify-traffic','verify-final-traffic'].includes(args.phase)){
   const r=this.restart(),scope=this.trace.identity.scope;
   if(!r||!result.noErrors||!observed(result,r.at)||result.generation!==r.generation||result.restartActionId!==r.actionId||
    result.applicationUid!==scope.application.uid||!same(Object.keys(result.nodes??{}).sort(),this.cohort().newInstanceIds)||
    Object.values(result.nodes).some(node=>node.queries<=0||node.transactions<=0))throw invalid();
   if(args.phase==='verify-final-traffic'){
    if(!this.intents('shrink').length)throw invalid();
    this.finalTraffic=structuredClone(result);
    this.manager.appendCustomEntry(FINAL_TRAFFIC,this.finalTraffic);
   }else{
    this.trafficProof=structuredClone(result);
    this.manager.appendCustomEntry(TRAFFIC,this.trafficProof);
   }
  }
 }
 idsFor(stage){return this.intents(stage).flatMap(i=>i.args.nodeIds??[]);}
 observe(kind,data,args){
  if(kind==='mcp'){for(const row of data??[])if(mcpChecks.includes(row.server+'.'+row.tool))this.checks.set(row.server+'.'+row.tool,row.observedAt);return;}
  if(kind==='sql'){
   if(typeof args?.instanceId!=='string')return;
   this.probes.delete(args.instanceId);
   if(data?.instanceId===args.instanceId)this.probes.set(args.instanceId,{...data,observedAt:Object.hasOwn(data,'observedAt')?data.observedAt:new Date().toISOString()});
  }else if(['fleet','backend','capacity','metrics','deployment','trafficFailures','connections','expansion','retirement'].includes(kind))this.current[kind]=structuredClone(data);
  this.trace.observe(kind,data);this.readiness.observe(kind,data,args);
 }
 capacity(desired){const c=this.current.capacity;return observed(c)&&c.groupId===this.trace.identity.scope.groupId&&c.desiredCapacity===desired&&c.minSize===2&&c.maxSize===4;}
 fleet(ids){const f=this.current.fleet;return observed(f)&&f.groupId===this.trace.identity.scope.groupId&&exact(f.instances,ids)&&f.instances.every(healthy);}
 backend(ids,drained=[]){
  const b=this.current.backend;
  return observed(b)&&b.serverGroupId===this.trace.identity.scope.serverGroupId&&backendMatches(b.nodes,ids,drained);
 }
 app(minGeneration=1,after=0){const a=this.current.deployment,s=this.trace.identity.scope.application;return observed(a,after)&&['clusterId','namespace','name','uid'].every(k=>a[k]===s[k])&&['replicas','readyReplicas','updatedReplicas','availableReplicas'].every(k=>a[k]===2)&&Number.isSafeInteger(a.generation)&&a.generation>=minGeneration&&Number.isSafeInteger(a.observedGeneration)&&a.observedGeneration===a.generation;}
 traffic(kind){const row=this.current[kind];if(!observed(row)||!Array.isArray(row.samples)||!row.samples.length)return false;const seen=new Set();for(const s of row.samples){const key=s.instance+'|'+(s.outcome??'');if(typeof s.instance!=='string'||!s.instance||seen.has(key)||!Number.isFinite(s.timestamp)||!Number.isFinite(s.value)||(kind==='connections'?s.value<=0:s.value!==0))return false;seen.add(key);}const ids=[...new Set(row.samples.map(s=>s.instance))];return ids.length===2&&(kind==='connections'?row.samples.length===2:row.samples.length===6&&ids.every(id=>['failure','timeout','ambiguous'].every(outcome=>seen.has(id+'|'+outcome))));}
 probesReady(ids,after=0){return ids.every(id=>{const p=this.probes.get(id);return observed(p,after)&&p.connected===true&&!Object.hasOwn(p,'readiness');});}
 holdReady(){
  const hold=this.current.hold;
  return observed(hold)&&Object.values(hold.sockets).every(socket=>observed({observedAt:socket.lastSqlAt}));
 }
 initialReads(){
  const ids=this.trace.baseline()?.instanceIds;
  return missingReads([
   [ids&&this.fleet(ids),'cloud_read fleet'],[ids&&this.backend(ids),'cloud_read backend'],[this.capacity(2),'cloud_read capacity'],
   [this.app(),'deployment_read'],[ids&&this.probesReady(ids),'db_connection_probe for the two original nodes'],
   [this.traffic('trafficFailures'),'metrics_read trafficFailures'],[this.traffic('connections'),'metrics_read connections'],
   [ids&&metricsValid(this.current.metrics,ids),'metrics_read pgcatClients'],
   ...mcpChecks.map(k=>[observed({observedAt:this.checks.get(k)}),k]),
  ]);
 }
 initial(){return this.initialReads().length===0;}
 requireReads(reads){
  if(reads.length)throw this.trace.versioned?new ReplacementEvidenceRequired(reads):missing();
 }
 requireReadyFour(){
  if(!this.readiness.blocked())return;
  const ids=this.current.fleet?.instances?.map(n=>n.instanceId)??[];
  const original=this.trace.baseline()?.instanceIds??[];
  const added=ids.filter(id=>!original.includes(id));
  const reads=missingReads([
   [ids.length===4&&original.every(id=>ids.includes(id))&&this.fleet(ids),'cloud_read fleet'],
   [ids.length===4&&this.backend(ids),'cloud_read backend'],[this.capacity(4),'cloud_read capacity'],
   [added.length===2&&this.probesReady(added),'db_connection_probe for the two replacement nodes'],
   [ids.length===4&&metricsValid(this.current.metrics,ids),'metrics_read pgcatClients'],
  ]);
  this.requireReads(reads.length?reads:['cloud_read fleet, backend and capacity, db_connection_probe and metrics_read pgcatClients']);
 }
 canWrite(){return this.permits.some(p=>p.stage==='expand')?this.readiness.ready():this.initial();}
 retained(){const {oldInstanceIds:old,newInstanceIds:added}=this.cohort();return this.capacity(4)&&this.fleet([...old,...added])&&this.current.fleet.instances.filter(n=>added.includes(n.instanceId)).every(protectedNode);}
 restart(){const intent=this.intents('restart')[0],result=this.trace.rows.find(r=>r.kind==='result'&&r.key===intent?.key);return result&&result.state.accepted===true&&Number.isSafeInteger(result.state.generation)&&result.state.generation>0?{generation:result.state.generation,at:Date.parse(result.observedAt),...(result.state.actionId?{actionId:result.state.actionId}:{})}:null;}
 drainReads(){
  const r=this.restart();
  if(!r)return ['deployment_read after the approved restart'];
  const {oldInstanceIds:old,newInstanceIds:added}=this.cohort();
  const metrics=this.current.metrics;
  return missingReads([
   [this.capacity(4),'cloud_read capacity'],[this.fleet([...old,...added])&&this.current.fleet.instances.filter(n=>added.includes(n.instanceId)).every(protectedNode),'cloud_read fleet'],
   [this.backend(added,old)&&observed(this.current.backend,r.at),'cloud_read backend'],[this.app(r.generation,r.at),'deployment_read'],
   [metricsValid(metrics,[...old,...added],old,r.at)&&metrics.nodes.filter(n=>added.includes(n.instanceId)).every(n=>n.pools.some(p=>p.pool==='test'&&p.user==='poc_app'&&(p.active>0||p.idle>0))),'metrics_read pgcatClients'],
  ]);
 }
 drained(){return this.drainReads().length===0;}
 beforeMutation(tool,args){
  if(tool==='approval_request'){
   if(args.parameters?.desiredCapacity!==2||!this.readiness.ready()||!this.drained()||this.idsFor('unprotectOld').length!==2||this.permits.some(p=>p.stage==='shrink')||this.current.fleet.instances.filter(n=>this.cohort().oldInstanceIds.includes(n.instanceId)).some(n=>n.protectedFromScaleIn!==false||n.lifecycleState!=='InService'))throw missing();
   return;
  }
  if(!['db_proxy_scale','db_proxy_node_protection_set','db_proxy_nodes_deregister','deployment_restart'].includes(tool))return;
  const key=this.trace.key(tool,args);if(this.trace.rows.some(r=>r.kind==='intent'&&(r.key===key||(tool==='db_proxy_scale'&&r.tool===tool&&r.args.actionId===args.actionId))))throw Error('MutationAlreadySubmitted');
  if(this.trace.unresolved())throw missing();
  const s=this.trace.identity.scope;
  if(args.requestId!==this.trace.identity.requestId||args.envCode!==s.envCode)throw missing();
  if(tool==='deployment_restart'){if(args.appCode!==s.application.appCode||typeof args.workloadKey!=='string'||!args.workloadKey)throw missing();}
  else if(args.proxyCode!==s.proxyCode)throw missing();
  if(this.trace.versioned&&tool==='db_proxy_node_protection_set'&&args.protected===true)this.requireReadyFour();
  const stage=this.stage(tool,args);
  if(['protectOld','expand'].includes(stage)){
   this.requireReads([...this.initialReads(),...missingReads([
    [stage!=='expand'||this.current.fleet?.instances?.every(protectedNode),'cloud_read fleet with both originals protected'],
    [stage!=='expand'||!this.trace.versioned||this.holdReady(),'pgcat_connection_continuity hold'],
   ])]);
  }else{
   if(this.trace.versioned)this.requireReadyFour();
   if(!this.readiness.ready())throw missing();
   const {oldInstanceIds:old,newInstanceIds:added}=this.cohort();
   const reads=missingReads([
    [this.capacity(4),'cloud_read capacity'],
    [this.fleet([...old,...added])&&(stage==='protectNew'||this.current.fleet.instances.filter(n=>added.includes(n.instanceId)).every(protectedNode)),'cloud_read fleet'],
    [!['protectNew','withdraw','restart'].includes(stage)||metricsValid(this.current.metrics,[...old,...added]),'metrics_read pgcatClients'],
   ]);
   if(this.trace.versioned&&['protectNew','withdraw'].includes(stage)){
    const after=Date.parse(this.readiness.expansion().result.observedAt);
    if(!this.probesReady(added,after))reads.push('db_connection_probe for the two replacement nodes');
   }
   if(stage==='protectNew'){
    // The new pair may be unprotected before this specific protection operation.
    if(!this.backend([...old,...added]))reads.push('cloud_read backend');
   }else if(stage==='withdraw'){
    const withdrawn=this.idsFor('withdraw');if(!this.backend([...old,...added].filter(id=>!withdrawn.includes(id)),withdrawn))reads.push('cloud_read backend');
   }else if(stage==='restart'){
    reads.push(...this.withdrawalReads(),...missingReads([[this.backend(added,old),'cloud_read backend'],[this.app(),'deployment_read'],
     [!this.trace.versioned||Boolean(this.drainProof),'pgcat_connection_continuity verify-drain']]));
   }else{
    reads.push(...this.drainReads());
    if(stage==='shrink'&&this.trace.versioned&&!observed(this.trafficProof,this.restart()?.at))reads.push('pgcat_connection_continuity verify-traffic');
    if(stage==='shrink'&&!this.trace.versioned&&this.current.fleet?.instances?.filter(n=>old.includes(n.instanceId)).some(n=>n.protectedFromScaleIn!==false||n.lifecycleState!=='InService'))throw missing();
   }
   this.requireReads(reads);
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
  if(tool==='db_proxy_nodes_deregister')for(const kind of this.trace.versioned?['fleet','backend','capacity']:['backend'])delete this.current[kind];
  if(tool==='deployment_restart')for(const kind of ['deployment','metrics','trafficFailures','connections'])delete this.current[kind];
 }
 retirementReceipt(){
  if(!this.trace.versioned||!this.trafficProof?.noErrors||this.trace.unresolved())return;
  return this.trace.scaleActions().find(a=>a.previousDesiredCapacity===4&&a.desiredCapacity===2&&a.outcome==='submitted'&&toolID(a.actionId)&&toolID(a.scalingActivityId));
 }
 finalTrafficReads(){
  const shrink=this.retirementReceipt();
  if(!shrink)return ['the submitted scale-in receipt'];
  const {oldInstanceIds,newInstanceIds}=this.cohort(),restart=this.restart(),retirement=this.current.retirement;
  return missingReads([
   [observed(retirement)&&retirement.status==='Successful'&&retirement.actionId===shrink.actionId&&
    retirement.scalingActivityId===shrink.scalingActivityId&&Array.isArray(retirement.destroyedIds)&&
    same([...retirement.destroyedIds].sort(),oldInstanceIds),'cloud_read retirement'],
   [this.capacity(2),'cloud_read capacity'],
   [this.fleet(newInstanceIds)&&this.current.fleet.instances.every(protectedNode),'cloud_read fleet'],
   [this.backend(newInstanceIds),'cloud_read backend'],
   [metricsValid(this.current.metrics,newInstanceIds),'metrics_read pgcatClients'],
   [restart&&this.app(restart.generation,restart.at)&&this.current.deployment.generation===restart.generation,'deployment_read'],
  ]);
 }
 // Guide a stopped model from the recorded phase. Tools still enforce all
 // guards and the model still chooses and executes the next tool round.
 continuationStep(grantVerified){
  if(!this.trace.versioned||!this.drainProof)return;
  if(!grantVerified)return {stage:'grant',instruction:'Read request_get first, then request_grant_get for the saved grant in its own tool round.'};
  if(this.trace.unresolved())return {stage:'reconcile',instruction:'Read request_grant_actions_list to reconcile the outstanding action. Never replay it.'};
  const {observationId,drainActionId}=this.drainProof;
  const restart=this.restart();
  if(!restart){
   if(!this.probeRelease)return {stage:'release',instruction:'Use the verified saved drain proof. Call pgcat_connection_continuity alone with '+JSON.stringify({phase:'release-probes',observationId,drainActionId})+'. Keep the existing expansion and drain.'};
   return {stage:'restart',instruction:'Finish the bound Deployment discovery and status reads, deployment_read, fleet, backend, capacity and pgcatClients. Then use deployment_restart once with the discovered workloadKey.'};
  }
  const trafficArgs=phase=>JSON.stringify({phase,observationId,restartActionId:restart.actionId});
  if(this.intents('shrink').length){
   if(this.finalTraffic){
    try{this.completionEvidence();return;}catch(e){if(e.message!=='MissingEvidence')throw e;}
   }
   return {stage:'final-traffic',instruction:'The scale-in receipt is saved. Read cloud_read retirement first. After it succeeds, read cloud_read fleet, backend and capacity, metrics_read pgcatClients, and deployment_read. Then call pgcat_connection_continuity with '+trafficArgs('verify-final-traffic')+'. Use resource_wait while retirement or traffic is settling.'};
  }
  if(!this.trafficProof)return {stage:'traffic',instruction:'Check the restarted Deployment and wait for 2/2, then call pgcat_connection_continuity with '+trafficArgs('verify-traffic')+'. If metrics or traffic are warming, use resource_wait and recheck.'};
  return {stage:'retire',instruction:'Confirm zero old clients in pgcatClients and refresh the required fleet, backend, capacity and Deployment reads, then call the approved scale to 2 once.'};
 }
 completionEvidence(){
  const cohort=this.cohort(),r=this.restart();
  if(this.trace.unresolved()||this.intents('shrink').length!==1||!r||!this.capacity(2)||!this.fleet(cohort.newInstanceIds)||!this.current.fleet.instances.every(protectedNode)||!this.backend(cohort.newInstanceIds)||!this.app(r.generation,r.at))throw missing();
  if(this.trace.versioned){
   if(this.finalTrafficReads().length||!this.finalTraffic?.noErrors||
    !observed(this.finalTraffic,Math.max(...['retirement','capacity','fleet','backend','metrics','deployment'].map(k=>Date.parse(this.current[k].observedAt)))))throw missing();
   return {...cohort,restartGeneration:r.generation,traffic:this.trafficProof,finalTraffic:this.finalTraffic,retirement:this.current.retirement};
  }
  return {...cohort,restartGeneration:r.generation};
 }
}
