const ranks={sending:0,send_failed:0,accepted:1,queued:2,delivered:3,answered:4,interrupted:4,rejected:4};
export function mergeConversationMessages({requestId,originalQuestion,acceptedMessages=[],events=[],localMessages=[]}){
 const rows=new Map();const put=(v)=>{if(!v.messageId)return;const old=rows.get(v.messageId);if(!old||(ranks[v.status]??0)>=(ranks[old.status]??0))rows.set(v.messageId,{...old,...v})};
 if(originalQuestion)put({messageId:`original:${requestId}`,role:'user',text:originalQuestion,status:'original',original:true});
 for(const v of localMessages)put({...v,role:'user'});for(const v of acceptedMessages)put({...v,role:'user'});
 for(const e of events){if(e.requestId&&e.requestId!==requestId||e.kind!=='message')continue;put({messageId:e.details?.messageId,role:e.details?.role,text:e.summary,status:e.details?.status,reason:e.details?.reason,inputSequence:e.details?.inputSequence,event:e});}
 return [...rows.values()];
}
export function createMessageSender({requestId,submit,onChange=()=>{}}){
 const rows=new Map(),active=new Map();let closed=false;const publish=()=>{if(!closed)onChange([...rows.values()])};
 function dispatch(v){if(closed)return Promise.reject(Error('ConversationClosed'));if(active.has(v.messageId))return active.get(v.messageId);v.status='sending';delete v.reason;publish();const p=Promise.resolve().then(()=>submit(requestId,{messageId:v.messageId,text:v.text})).then(receipt=>{if(!closed){Object.assign(v,receipt);publish()}return receipt}).catch(e=>{if(!closed){v.status='send_failed';v.reason=e.message;publish()}throw e}).finally(()=>active.delete(v.messageId));active.set(v.messageId,p);return p;}
 return {get messages(){return [...rows.values()]},send(text){if(closed||!text?.trim()||[...text].length>2000||new TextEncoder().encode(text).length>8192)return Promise.reject(Error('InvalidMessage'));const v={messageId:crypto.randomUUID(),text,status:'sending'};rows.set(v.messageId,v);return dispatch(v)},retry(id){const v=rows.get(id);if(!v||v.status!=='send_failed')return Promise.reject(Error('InvalidRetry'));return dispatch(v)},close(){closed=true;rows.clear()}};
}
