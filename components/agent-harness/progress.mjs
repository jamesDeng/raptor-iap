import fs from 'node:fs';
export const TOOL_NAMES=['mcp__raptor__request_get','mcp__raptor__environment_get','mcp__raptor__object_get','mcp__infra__cloud_identity_get','mcp__infra__deployments_list','mcp__infra__deployment_status_get'];
export class ProgressWriter{
 constructor(file){this.file=file;this.sequence=0;fs.writeFileSync(file,'',{flag:'wx',mode:0o600});}
 append(event){if(!event||Object.keys(event).some(k=>!['kind','tool','outcome','summary','messageId','inputSequence','turnId','sessionId','sessionFile'].includes(k))||!['status','progress','tool_start','tool_result','checkpoint','cleanup','message','turn','session','settling'].includes(event.kind)||!['started','running','succeeded','failed','cancelled','pending','confirmed','delivered'].includes(event.outcome)||(['tool_start','tool_result'].includes(event.kind)?!TOOL_NAMES.includes(event.tool):event.tool!==undefined))throw Error('InvalidProgress');if(event.summary!==undefined&&(event.kind!=='progress'||event.outcome!=='running'||typeof event.summary!=='string'||!event.summary.trim()||Buffer.byteLength(event.summary)>2048))throw Error('InvalidProgress');if(['message','turn','session','settling'].includes(event.kind)){if(event.kind==='message'&&(!/^[a-f0-9-]{36}$/.test(event.messageId??'')||!Number.isSafeInteger(event.inputSequence)||event.inputSequence<1||event.outcome!=='delivered'))throw Error('InvalidProgress');if(event.kind!=='message'&&event.outcome!=='confirmed')throw Error('InvalidProgress');}else if(['messageId','inputSequence','turnId','sessionId','sessionFile'].some(k=>event[k]!==undefined))throw Error('InvalidProgress');const row={...event,runtimeSequence:this.sequence+1,occurredAt:new Date().toISOString()},line=JSON.stringify(row)+'\n';if(fs.statSync(this.file).size+Buffer.byteLength(line)>65536)throw Error('ProgressLimit');fs.appendFileSync(this.file,line,{mode:0o600});this.sequence++;return row;}
}
export function writeTerminal(file,value){const raw=JSON.stringify(value);if(Buffer.byteLength(raw)>65536)throw Error('InvalidResult');const tmp=file+'.tmp';fs.writeFileSync(tmp,raw,{flag:'wx',mode:0o600});fs.renameSync(tmp,file);}

// Completed public text before a tool round; never export thinking blocks or the final answer.
export function publicProgressFromMessage(message){
 if(message?.role!=='assistant'||message.stopReason!=='toolUse')return '';
 const text=message.content.filter(part=>part.type==='text'&&typeof part.text==='string').map(part=>part.text).join('').trim();
 // Preserve complete text for Gateway secret matching; never truncate a credential.
 return Buffer.byteLength(text)>2048?'':text;
}
