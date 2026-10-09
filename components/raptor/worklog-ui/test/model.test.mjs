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
