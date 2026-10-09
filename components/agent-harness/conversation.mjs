import fs from 'node:fs';import path from 'node:path';
const UUID=/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
export function openConversationSession(sdk,conversation,stateRoot,requestId){
 const dir=path.join(stateRoot,'sessions'),metaFile=path.join(dir,`request-${requestId}.json`);fs.mkdirSync(dir,{recursive:true,mode:0o700});
 if(conversation?.sessionId){
  if(conversation.requestId!==requestId||!UUID.test(conversation.sessionId)||!conversation.sessionFile||path.basename(conversation.sessionFile)!==conversation.sessionFile||!conversation.sessionFile.endsWith('.jsonl'))throw Error('InvalidJob');
  const file=path.join(dir,conversation.sessionFile);if(fs.lstatSync(file).isSymbolicLink())throw Error('InvalidJob');const meta=JSON.parse(fs.readFileSync(metaFile,'utf8'));
  if(meta.requestId!==requestId||meta.sessionId!==conversation.sessionId||meta.sessionFile!==conversation.sessionFile)throw Error('InvalidJob');
  const manager=sdk.SessionManager.open(file,dir,stateRoot);if(manager.getSessionId()!==conversation.sessionId)throw Error('InvalidJob');return {manager,meta,save:()=>fs.writeFileSync(metaFile,JSON.stringify(meta),{mode:0o600})};
 }
 const manager=sdk.SessionManager.create(stateRoot,dir),meta={requestId,sessionId:manager.getSessionId(),sessionFile:path.basename(manager.getSessionFile()),lastInputSequence:0};
 return {manager,meta,save:()=>fs.writeFileSync(metaFile,JSON.stringify(meta),{mode:0o600})};
}
export function createConversationInbox({privateRoot,binding,session,onProgress,signal,startSequence=0,onDelivered=()=>{}}){
 let closed=false,busy=false,last=startSequence;const seen=new Map(),pending=[];
 const unsubscribe=session.subscribe(event=>{if(event.type!=='message_start'||event.message.role!=='user')return;const text=event.message.content.filter(x=>x.type==='text').map(x=>x.text).join('');const first=pending[0];if(!first||first.text!==text)return;pending.shift();last=first.inputSequence;onDelivered(first);onProgress({kind:'message',outcome:'delivered',messageId:first.messageId,inputSequence:first.inputSequence});});
 function valid(v){return v&&v.requestId===binding.requestId&&v.attemptId===binding.attemptId&&UUID.test(v.messageId)&&Number.isSafeInteger(v.inputSequence)&&v.inputSequence>0&&typeof v.text==='string'&&v.text.trim()&&[...v.text].length<=2000&&Buffer.byteLength(v.text)<=8192&&!v.text.includes('\0');}
 async function accept(v){if(!valid(v))throw Error('InvalidProgress');const prior=seen.get(v.messageId);if(prior){if(prior!==JSON.stringify(v))throw Error('InvalidProgress');return};if(v.inputSequence<=last)return;if(pending.length)throw Error('InvalidProgress');seen.set(v.messageId,JSON.stringify(v));pending.push(v);try{await session.prompt(v.text,{expandPromptTemplates:false,streamingBehavior:'steer'})}catch(e){pending.splice(pending.indexOf(v),1);throw e}}
 async function poll(){if(closed||busy||signal?.aborted)return;busy=true;try{let raw;try{raw=fs.readFileSync(path.join(privateRoot,'conversation-inbox.json'));}catch(e){if(e.code==='ENOENT')return;throw e};if(raw.length>16384)throw Error('InvalidProgress');let v;try{v=JSON.parse(raw.toString('utf8'))}catch{return};await accept(v)}finally{busy=false}}
 return {poll,accept,get lastSequence(){return last},get pending(){return pending.length>0},close(){closed=true;unsubscribe?.()}};
}
