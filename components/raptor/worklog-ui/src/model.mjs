// Map public Raptor projections only. Never derive completion from an answer.
export function projectWorklog(snapshot={},events=[]) {
 const request=snapshot.request??{},execution=snapshot.execution??{};
 const seen=new Set();
 const selected=events.filter(e=>!request.id||!e.requestId||e.requestId===request.id)
  .slice().sort((a,b)=>a.sequence-b.sequence).filter(e=>{if(seen.has(e.sequence))return false;seen.add(e.sequence);return true;});
 const times=selected.map(e=>Date.parse(e.occurredAt)).filter(Number.isFinite);
 const cleanup=execution.cleanupStatus;
 return {requestId:request.id??'',events:selected,status:execution.status??request.status??'unknown',
  answer:execution.result?.answer??'',checkpoint:execution.checkpointStatus??'not recorded',
  cleanup:typeof cleanup==='object'&&cleanup!==null?(cleanup.sandboxAbsent===true&&cleanup.keyAbsent===true&&cleanup.accessRevoked===true?'confirmed':'pending'):(cleanup??execution.cleanup??'not recorded'),
  recoveryNeeded:execution.recoveryNeeded===true,stale:snapshot.executionAvailable===false,
  durationSeconds:times.length>=2?Math.floor((Math.max(...times)-Math.min(...times))/1000):null};
}

// Gateway currently identifies tools by name, not call ID. Pair only an unambiguous
// start/result in the same attempt; otherwise preserve the result as narration.
export function projectTranscript(snapshot={}, events=[]) {
 const model=projectWorklog(snapshot,events);
 const terminal=['completed','succeeded','failed','cancelled'].includes(model.status);
 const waiting=['paused','waiting_approval','approval_wait','waiting_pr','waiting_review'].includes(model.status);
 const running=!terminal&&!waiting&&['running','queued','starting','resuming'].includes(model.status);
 const complete=terminal&&model.cleanup==='confirmed'&&!model.recoveryNeeded;
 const messages=[],records={};let segment,pending=new Map();
 const narration=event=>[{type:'text',text:event.summary||event.kind||'Recorded event'},{type:'data',name:'progress-event',data:event}];
 for(const event of model.events){
  const attempt=event.attemptId??'unrecorded';
  if(!segment||segment.attempt!==attempt){
   segment={id:`${model.requestId}:${attempt}:${event.sequence}`,role:'assistant',content:[],attempt};messages.push(segment);pending=new Map();
  }
  const name=event.details?.tool;
  if(event.kind==='tool_start'&&typeof name==='string'&&name){
   const id=`${model.requestId}:${attempt}:${event.sequence}`;
   const part={type:'tool-call',toolCallId:id,toolName:name,args:{},argsText:''};
   segment.content.push(part);records[id]={events:[event],status:'started'};
   const candidates=pending.get(name)??[];candidates.push(part);pending.set(name,candidates);
  }else if(event.kind==='tool_result'&&pending.get(name)?.length===1){
   const part=pending.get(name)[0],status=event.details?.status??'unknown';
   part.result={status};part.isError=['failed','cancelled'].includes(status);
   records[part.toolCallId].events.push(event);records[part.toolCallId].status=status;pending.delete(name);
  }else{segment.content.push(...narration(event));}
 }
 for(const [index,message] of messages.entries()){
  const active=index===messages.length-1&&(!snapshot.execution?.attemptId||message.attempt===snapshot.execution.attemptId);
  for(const part of message.content){if(part.type==='tool-call')records[part.toolCallId].active=active&&running;}
  message.status=active&&running?{type:'running'}:active&&waiting?{type:'requires-action',reason:'interrupt'}:{type:'complete',reason:'unknown'};
  delete message.attempt;
 }
 return {...model,messages,records,running,waiting,complete,terminal};
}
