import {isDeepStrictEqual} from 'node:util';
import {ApprovalPause} from './approval-pause.mjs';

const checkpointType='raptor-request-grant-v1';
const rejected=()=>Error('InvalidApprovalPause');

export class RequestGrantRuntime {
 constructor(manager,binding,scope,decisionContext){
  this.manager=manager;
  this.binding=binding;
  this.scope=scope;
  const entries=manager.getEntries().filter(entry=>entry.type==='custom'&&entry.customType===checkpointType);
  const saved=entries.at(-1)?.data;
  if(saved&&(saved.requestId!==binding.requestId||saved.definitionSha256!==binding.definitionSha256||
   saved.kind!=='grant'||saved.actionId!==saved.approvalId))throw rejected();
  if(decisionContext?.kind==='grant'){
   if(!saved||decisionContext.decision!=='resume'||
    ['approvalId','actionId','bindingDigest'].some(key=>decisionContext[key]!==saved[key]))throw rejected();
  }
  this.expected=saved?{...saved,decision:'resume'}:undefined;
 }

 readContext(data,infraPolicies){
  if(data?.scopeDigest!==this.scope.resolutionDigest||
   data.definitionSha256!==this.binding.definitionSha256||
   typeof data.operationPrompt!=='string'||!data.operationPrompt||
   !data.approvalPolicies||typeof data.approvalPolicies!=='object')throw rejected();
  for(const [tool,policy] of Object.entries(infraPolicies)){
   if(policy.variants.some(variant=>variant.requiresHumanApproval)&&!isDeepStrictEqual(data.approvalPolicies[tool],policy))throw rejected();
  }
  const required=new Map();
  for(const [tool,policy] of Object.entries(data.approvalPolicies)){
   if(!isDeepStrictEqual(policy,infraPolicies[tool]))throw rejected();
   for(const variant of policy.variants){
    if(variant.requiresHumanApproval){
     if(required.has(variant.capability))throw rejected();
     required.set(variant.capability,variant.targetKind);
    }
   }
  }
  if(!required.size)throw rejected();
  this.required=required;
 }

 beforeProposal(args){
  if(this.expected||!this.required||args.capabilities.length!==this.required.size)throw rejected();
  const outstanding=new Map(this.required);
  for(const capability of args.capabilities){
   if(outstanding.get(capability.capability)!==capability.target.kind)throw rejected();
   outstanding.delete(capability.capability);
  }
  if(outstanding.size)throw rejected();
 }

 beforeRead(args){
  if(!this.expected||args.grantId!==this.expected.approvalId)throw rejected();
 }

 readReceipt(receipt){
  if(!this.required)throw rejected();
  this.approved=ApprovalPause.verifyGrantResume(this.binding,this.expected,this.scope.resolutionDigest,receipt);
 }

 recordWait(wait){
  if(wait.kind==='grant')this.manager.appendCustomEntry(checkpointType,wait);
 }

 resumeInstruction(){
  return this.expected
   ? ' After request_get, call request_grant_get with grantId '+this.expected.approvalId+' in its own read round. Recheck the phase evidence before the next mutation.'
   : '';
 }
}
