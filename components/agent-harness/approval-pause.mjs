import {createHash,randomUUID} from 'node:crypto';
function canonical(value){
 if(value===null||typeof value!=='object')return JSON.stringify(value);
 if(Array.isArray(value))return '['+value.map(canonical).join(',')+']';
 return '{'+Object.keys(value).sort().map(k=>JSON.stringify(k)+':'+canonical(value[k])).join(',')+'}';
}
export class ApprovalPause {
 constructor(binding){if(!binding?.requestId||!/^[a-f0-9]{64}$/.test(binding.definitionSha256))throw Error('InvalidApprovalPause');this.binding={...binding};}
 requested(args,approval){
  if(args?.requestId!==this.binding.requestId||args.interface!=='ess.scale-in'||typeof args.actionId!=='string'||!args.actionId||approval?.requestId!==this.binding.requestId||typeof approval.approvalId!=='string'||!approval.approvalId||approval.actionId!==args.actionId||!['pending','approved','denied'].includes(approval.state)||canonical(approval.binding)!==canonical(args))throw Error('InvalidApprovalPause');
  const digest=createHash('sha256').update(canonical(args)).digest('hex');
  if(this.candidate&&(this.candidate.approvalId!==approval.approvalId||this.candidate.bindingDigest!==digest))throw Error('InvalidApprovalPause');
  this.candidate??={kind:'approval',approvalId:approval.approvalId,actionId:args.actionId,bindingDigest:digest,requestId:this.binding.requestId,definitionSha256:this.binding.definitionSha256,startedAt:new Date().toISOString()};
 }
 resourceWait(args){if(this.candidate||!args||Object.keys(args).join(',')!=='seconds'||args.seconds!==60)throw Error('InvalidApprovalPause');this.candidate={kind:'resource',approvalId:'',actionId:'capacity-'+randomUUID(),bindingDigest:this.binding.definitionSha256,requestId:this.binding.requestId,definitionSha256:this.binding.definitionSha256,startedAt:new Date().toISOString(),wakeAfterSeconds:60};this.pending=true;}
 paused(args){if(!this.candidate||args?.requestId!==this.binding.requestId||args.reason!=='waiting_approval')throw Error('InvalidApprovalPause');this.pending=true;}
 endRound(){if(this.pending)this.committed??=Object.freeze({...this.candidate});return this.committed;}
 beforeTool(server,tool){if(this.committed||(this.candidate&&server==='infra'&&['db_proxy_scale','db_proxy_node_protection_set','db_proxy_nodes_deregister','deployment_restart'].includes(tool)))throw Error('ApprovalWaiting');}
 wait(){return this.committed;}
}

export function resourceWaitExtension(pause){return pi=>pi.registerTool({name:'resource_wait',label:'Wait for resource readiness',description:'Checkpoint and wait 60 seconds before a fresh readiness check. This does not approve scale-in or any other gated interface. At most ten resource waits are allowed per request.',parameters:{type:'object',properties:{seconds:{type:'integer',enum:[60]}},required:['seconds'],additionalProperties:false},execute:async(_id,args)=>{pause.resourceWait(args);return {content:[{type:'text',text:'Readiness wait requested; stop at the completed tool round.'}],details:{}};}});}
