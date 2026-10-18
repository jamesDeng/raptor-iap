import test from 'node:test';
import assert from 'node:assert/strict';
const load=()=>import('../src/model.mjs').catch(e=>e.code==='ERR_MODULE_NOT_FOUND'?{}:Promise.reject(e));
// Catches cross-request display, misleading completion, and fabricated elapsed time.
test('progress belongs to the selected request and stays in recorded order',async()=>{
 const {projectWorklog}=await load();assert.equal(typeof projectWorklog,'function');
 const result=projectWorklog({request:{id:'one'},execution:{}},[
  {requestId:'two',sequence:1,summary:'wrong request'},
  {requestId:'one',sequence:3,summary:'third'},
  {requestId:'one',sequence:2,summary:'second'},
  {requestId:'one',sequence:2,summary:'second'},
 ]);
 assert.deepEqual(result.events.map(e=>e.summary),['second','third']);
});
test('answer does not imply completed execution and failed sync stays visible',async()=>{
 const {projectWorklog}=await load();assert.equal(typeof projectWorklog,'function');
 const result=projectWorklog({request:{id:'one'},executionAvailable:false,execution:{status:'running',result:{answer:'<img src=x onerror=alert(1)>'},checkpointStatus:'pending',cleanupStatus:{sandboxAbsent:false,keyAbsent:false,accessRevoked:false}}},[]);
 assert.equal(result.status,'running');assert.equal(result.answer,'<img src=x onerror=alert(1)>');assert.equal(result.cleanup,'pending');assert.equal(result.stale,true);
});
test('elapsed time uses recorded timestamps only',async()=>{
 const {projectWorklog}=await load();assert.equal(typeof projectWorklog,'function');
 assert.equal(projectWorklog({},[{sequence:1,occurredAt:'2026-10-09T00:00:00Z'},{sequence:2,occurredAt:'2026-10-09T00:00:31Z'}]).durationSeconds,31);
 assert.equal(projectWorklog({},[{sequence:1},{sequence:2,occurredAt:'bad'}]).durationSeconds,null);
});
test('observations during an unambiguous tool call stay inside its collapsed activity',async()=>{
 const {projectTranscript}=await load();
 const base={requestId:'one',attemptId:'a',evidenceMode:'live'};
 const model=projectTranscript({request:{id:'one'},execution:{status:'running'}},[
  {...base,sequence:1,kind:'tool_start',details:{tool:'db_connection_probe'}},
  {...base,sequence:2,kind:'progress',summary:'Observation db_connection_probe args-valid'},
  {...base,sequence:3,kind:'tool_result',details:{tool:'db_connection_probe',status:'succeeded'}},
 ]);
 const part=model.messages[0].content[0];
 assert.equal(part.name,'tool-activity');
 assert.deepEqual(model.records[part.data.items[0].part.toolCallId].events.map(e=>e.sequence),[1,2,3]);
 assert.equal(model.messages[0].content.length,1);
});
test('ambiguous adjacent tool events share one activity without invented pairing',async()=>{
 const {projectTranscript}=await load();
 const base={requestId:'one',attemptId:'a',evidenceMode:'live'};
 const model=projectTranscript({request:{id:'one'},execution:{status:'completed'}},[
  {...base,sequence:1,kind:'tool_start',details:{tool:'probe'}},
  {...base,sequence:2,kind:'tool_start',details:{tool:'probe'}},
  {...base,sequence:3,kind:'progress',summary:'Observation probe args-valid'},
  {...base,sequence:4,kind:'tool_result',summary:'tool_result: succeeded',details:{tool:'probe',status:'succeeded'}},
 ]);
 const parts=model.messages[0].content;
 assert.equal(parts.length,1);assert.equal(parts[0].name,'tool-activity');
 assert.deepEqual(parts[0].data.items.map(item=>item.type),['call','call','event','event']);
 assert.deepEqual(Object.values(model.records).map(record=>record.events.length),[1,1]);
});
test('narration record details join the following tool activity',async()=>{
 const {projectTranscript}=await load();
 const base={requestId:'one',attemptId:'a',evidenceMode:'live'};
 const model=projectTranscript({request:{id:'one'},execution:{status:'completed'}},[
  {...base,sequence:1,kind:'progress',summary:'Checking the workload',details:{runtimeSequence:3,status:'running'}},
  {...base,sequence:2,kind:'tool_start',details:{tool:'deployment_status_get'}},
  {...base,sequence:3,kind:'tool_result',details:{tool:'deployment_status_get',status:'succeeded'}},
 ]);
 const parts=model.messages[0].content;
 assert.deepEqual(parts.map(part=>part.type==='data'?part.name:part.type),['text','tool-activity']);
 assert.deepEqual(parts[1].data.items.map(item=>item.type),['record','call']);
 assert.equal(parts[1].data.items[0].event.details.runtimeSequence,3);
});
test('earlier status details join tool activity across intervening narration',async()=>{
 const {projectTranscript}=await load();
 const base={requestId:'one',attemptId:'a',evidenceMode:'live'};
 const model=projectTranscript({request:{id:'one'},execution:{status:'completed'}},[
  {...base,sequence:1,kind:'status',summary:'status: started',details:{status:'started'}},
  {...base,sequence:2,kind:'progress',summary:'I will inspect the workload',details:{runtimeSequence:3,status:'running'}},
  {...base,sequence:3,kind:'tool_start',summary:'tool_start: started',details:{tool:'deployment_status_get'}},
  {...base,sequence:4,kind:'tool_result',summary:'tool_result: succeeded',details:{tool:'deployment_status_get',status:'succeeded'}},
 ]);
 const parts=model.messages[0].content;
 assert.deepEqual(parts.map(part=>part.type==='data'?part.name:part.type),['text','text','tool-activity']);
 assert.deepEqual(parts[2].data.items.map(item=>item.type),['record','record','call']);
 assert.deepEqual(parts[2].data.items.slice(0,2).map(item=>item.event.sequence),[1,2]);
});
