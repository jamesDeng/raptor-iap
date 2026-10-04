import {createHash} from 'node:crypto';

export const MODEL='gpt-5.6-luna';
export function canonical(value){
 return JSON.stringify(value,(_,v)=>v&&typeof v==='object'&&!Array.isArray(v)?Object.fromEntries(Object.keys(v).sort().map(k=>[k,v[k]])):v);
}
export function snapshotHash(value){return createHash('sha256').update(canonical(value),'utf8').digest('hex');}
function fail(){throw Error('InvalidJob');}
function exact(value,fields){if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).sort().join('|')!==[...fields].sort().join('|'))fail();}
function text(value,max,nullable=false,blank=false){
 if(value===null&&nullable)return;
 if(typeof value!=='string'||[...value].length>max||value.includes('\0')||(!blank&&!value.trim())||/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value))fail();
}
function uuid(value){if(typeof value!=='string'||! /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(value))fail();}

export function validateApplicationRequest(value){
 exact(value,['kind','question','snapshot','binding','model']);
 if(value.kind!=='app-question'||value.model!==MODEL)fail();
 text(value.question,2000);if(Buffer.byteLength(value.question,'utf8')>8192)fail();
 const s=value.snapshot,b=value.binding;
 exact(s,['app_id','name','owner','environment','region','ack_cluster_id','namespace','deployment','dependencies','version','updated_at','provenance']);
 uuid(s.app_id);if(!Number.isSafeInteger(s.version)||s.version<1||s.provenance!=='owner-entered')fail();
 for(const name of ['name','owner','environment','region'])text(s[name],128);
 for(const name of ['ack_cluster_id','namespace','deployment'])text(s[name],128,true);
 text(s.updated_at,64);
 if(!Array.isArray(s.dependencies)||s.dependencies.length>10)fail();
 for(const dep of s.dependencies){exact(dep,['name','type','lifecycle','notes']);text(dep.name,128);text(dep.notes,1000,false,true);if(!['postgres','pgcat','other'].includes(dep.type)||!['planned','present','unknown'].includes(dep.lifecycle))fail();}
 if(Buffer.byteLength(canonical(s),'utf8')>32768)fail();
 exact(b,['task_id','attempt_id','snapshot_sha256']);uuid(b.task_id);uuid(b.attempt_id);
 if(b.snapshot_sha256!==snapshotHash(s))fail();
 return structuredClone(value);
}

export function createApplicationContextTool({snapshot,binding,onRead=()=>{}}){
 const saved=structuredClone(snapshot),identity=structuredClone(binding);
 if(snapshotHash(saved)!==identity.snapshot_sha256)fail();
 return {
  name:'get_application_context',label:'Registered application context',
  description:'Return the immutable owner-entered app snapshot for this task. Planned/unknown labels are declarations, not live inspection. Accepts no selectors, commands, paths or URLs.',
  parameters:{type:'object',properties:{},additionalProperties:false},
  async execute(_callId,args){
   exact(args,[]);
   const details={context_sha256:identity.snapshot_sha256};
   const text=canonical({snapshot:saved,binding:identity,provenance:'owner-entered'});
   onRead(details);
   return {content:[{type:'text',text}],details};
  }
 };
}
