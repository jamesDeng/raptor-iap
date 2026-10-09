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
