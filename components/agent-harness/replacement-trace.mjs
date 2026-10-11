import {createHash} from 'node:crypto';
const TYPE='raptor-replacement-evidence-v1';
const invalid=()=>new Error('InvalidEvidence');
const canonical=value=>JSON.stringify(value,(_key,v)=>v&&typeof v==='object'&&!Array.isArray(v)?Object.fromEntries(Object.keys(v).sort().map(k=>[k,v[k]])):v);
const digest=value=>createHash('sha256').update(canonical(value)).digest('hex');
const mutations=new Set(['db_proxy_scale','db_proxy_node_protection_set','db_proxy_nodes_deregister','deployment_restart']);
const fresh=stamp=>Number.isFinite(Date.parse(stamp))&&Math.abs(Date.now()-Date.parse(stamp))<=30000;
export class ReplacementTrace {
 constructor(manager,binding,scope){
  this.manager=manager;this.identity={requestId:binding.requestId,definitionSha256:binding.definitionSha256,skillsCommit:binding.skillsCommit,model:binding.model,scope};
  this.rows=manager.getEntries().filter(e=>e.type==='custom'&&e.customType===TYPE).map(e=>e.data);
  if(this.rows.length>200||this.rows.some(r=>!r||typeof r!=='object'||r.version!==1||canonical(r.identity)!==canonical(this.identity)||!['baseline','intent','result'].includes(r.kind)||Buffer.byteLength(canonical(r))>8192))throw invalid();
  if(this.rows.filter(r=>r.kind==='baseline').length>1)throw invalid();
  const intents=new Map(),results=new Set();let baseline=false;
  for(const row of this.rows){
   if(typeof row.observedAt!=='string'||!Number.isFinite(Date.parse(row.observedAt)))throw invalid();
   if(row.kind==='baseline'){
    if(baseline||intents.size||!Array.isArray(row.instanceIds)||row.instanceIds.length!==2||row.instanceIds.some(id=>typeof id!=='string'||!id)||new Set(row.instanceIds).size!==2)throw invalid();
    baseline=true;continue;
   }
   if(!baseline||!mutations.has(row.tool)||typeof row.key!=='string'||!/^[a-f0-9]{64}$/.test(row.key))throw invalid();
   if(row.kind==='intent'){
    if(!row.args||typeof row.args!=='object'||Array.isArray(row.args)||this.key(row.tool,row.args)!==row.key||intents.has(row.key))throw invalid();
    if(row.tool==='db_proxy_scale'&&(typeof row.args.actionId!=='string'||!row.args.actionId||[...intents.values()].some(v=>v.tool===row.tool&&v.args.actionId===row.args.actionId)))throw invalid();
    intents.set(row.key,row);
   }else{
    if(!intents.has(row.key)||intents.get(row.key).tool!==row.tool||results.has(row.key)||!row.state||typeof row.state!=='object'||Array.isArray(row.state))throw invalid();
    results.add(row.key);
   }
  }

 }
 append(row){if(this.rows.length>=200)throw invalid();const data={version:1,identity:this.identity,...row};this.manager.appendCustomEntry(TYPE,data);this.rows.push(data);}
 baseline(){return this.rows.find(r=>r.kind==='baseline');}
 observe(kind,data){
  if(kind!=='fleet'||this.baseline()||this.rows.some(r=>r.kind==='intent'))return;
  if(data.groupId!==this.identity.scope.groupId||!fresh(data.observedAt)||!Array.isArray(data.instances)||data.instances.length!==2||new Set(data.instances.map(n=>n.instanceId)).size!==2||data.instances.some(n=>!n.instanceId||!['InService','Protected'].includes(n.lifecycleState)||n.healthStatus!=='Healthy'))return;
  this.append({kind:'baseline',instanceIds:data.instances.map(n=>n.instanceId).sort(),observedAt:data.observedAt});
 }
 key(tool,args){return digest({tool,args:{...args,...(args.instanceIds?{instanceIds:[...args.instanceIds].sort()}:{})}});}
 beforeMutation(tool,args){
  if(!mutations.has(tool))return;
  if(!this.baseline())throw new Error('MissingEvidence');
  const key=this.key(tool,args);
  if(this.rows.some(r=>r.kind==='intent'&&(r.key===key||(tool==='db_proxy_scale'&&r.tool===tool&&r.args.actionId===args.actionId))))throw new Error('MutationAlreadySubmitted');
  this.append({kind:'intent',key,tool,args:structuredClone(args),observedAt:new Date().toISOString()});
 }
 mutation(tool,args,data){
  if(!mutations.has(tool))return;
  const key=this.key(tool,args),intent=this.rows.find(r=>r.kind==='intent'&&r.key===key);
  if(!intent||this.rows.some(r=>r.kind==='result'&&r.key===key))throw invalid();
  const state=tool==='db_proxy_scale'?{previousDesiredCapacity:data.previousDesiredCapacity,desiredCapacity:data.desiredCapacity,outcome:data.outcome}:tool==='deployment_restart'?{accepted:data.accepted,generation:data.generation}:{instanceIds:data.instanceIds,outcome:data.outcome};
  this.append({kind:'result',key,tool,state,observedAt:new Date().toISOString()});
 }
 scaleActions(){return this.rows.filter(r=>r.kind==='intent'&&r.tool==='db_proxy_scale').map(intent=>({actionId:intent.args.actionId,...this.rows.find(r=>r.kind==='result'&&r.key===intent.key)?.state}));}
 unresolved(){return this.rows.some(r=>r.kind==='intent'&&!this.rows.some(v=>v.kind==='result'&&v.key===r.key));}
}
