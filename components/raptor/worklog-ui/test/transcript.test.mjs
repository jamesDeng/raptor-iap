import test from 'node:test';
import assert from 'node:assert/strict';
import * as model from '../src/model.mjs';
const snapshot={request:{id:'one'},execution:{status:'running'}};
const event=(sequence,kind,summary,details={},attemptId='a')=>({requestId:'one',attemptId,sequence,kind,summary,details,evidenceMode:'live'});
// Catches start/result shown twice, wrong tool pairing, retry cross-pairing and invented success.
test('transcript pairs sequential tool calls without moving intervening narration',()=>{
 assert.equal(typeof model.projectTranscript,'function');
 const result=model.projectTranscript(snapshot,[event(1,'progress','Inspect deployment'),event(2,'tool_start','start',{tool:'status_get',status:'started'}),event(3,'progress','Waiting on infra'),event(4,'tool_result','failed',{tool:'status_get',status:'failed'}),event(5,'tool_start','retry',{tool:'status_get',status:'started'})]);
 const content=result.messages[0].content;
 assert.deepEqual(content.map(p=>p.type),['text','data','tool-call','text','data','tool-call']);
 assert.equal(content[2].isError,true);assert.equal(content[2].result.status,'failed');
 assert.equal(content[5].result,undefined);assert.notEqual(content[2].toolCallId,content[5].toolCallId);
 assert.match(content[3].text,/Waiting on infra/);
 const updated=model.projectTranscript(snapshot,[event(2,'tool_start','start',{tool:'status_get'}),event(4,'tool_result','done',{tool:'status_get',status:'succeeded'})]);
 assert.equal(updated.messages[0].content[0].toolCallId,content[2].toolCallId);
});
test('different attempts, unknown results and request boundaries retain provenance',()=>{
 assert.equal(typeof model.projectTranscript,'function');
 const result=model.projectTranscript(snapshot,[event(1,'tool_start','first',{tool:'status_get'}),event(2,'tool_result','orphan',{tool:'status_get',status:'cancelled'},'b'),{...event(3,'progress','other'),requestId:'other'},event(4,'approval_wait','Wait for reviewer',{},'b')]);
 assert.equal(result.messages.length,2);
 assert.equal(result.messages[0].content[0].result,undefined);
 assert.match(result.messages[1].content[0].text,/orphan/);
 assert.match(result.messages[1].content[2].text,/Wait for reviewer/);
 assert.ok(!JSON.stringify(result.messages).includes('other'));
});
test('only an explicitly recorded success receives success classification',()=>{
 assert.equal(typeof model.projectTranscript,'function');
 for(const status of ['cancelled','failed','pending','unknown',undefined]){
  const result=model.projectTranscript(snapshot,[event(1,'tool_start','start',{tool:'tool'}),event(2,'tool_result','end',{tool:'tool',status})]);
  const part=result.messages[0].content[0];assert.equal(part.result.status,status??'unknown');
  assert.equal(part.isError,status==='failed'||status==='cancelled');
 }
 const result=model.projectTranscript({request:{id:'one'},execution:{status:'running',result:{answer:'Available'},checkpointStatus:'pending',cleanupStatus:'pending'}},[]);
 assert.equal(result.running,true);assert.equal(result.complete,false);
});

test('a previous attempt with no result never appears to be running in the new attempt',()=>{
 const result=model.projectTranscript({...snapshot,execution:{status:'running',attemptId:'b'}},[event(1,'tool_start','old',{tool:'tool'}),event(2,'progress','resumed',{},'b')]);
 assert.equal(Object.values(result.records)[0].active,false);
});
test('Gateway review pause is not marked running and completed lifecycle is recognized',()=>{
 const paused=model.projectTranscript({request:{id:'one'},execution:{status:'waiting_review'}},[]);
 assert.equal(paused.waiting,true);assert.equal(paused.running,false);
 const completed=model.projectTranscript({request:{id:'one'},execution:{status:'completed',cleanupStatus:'confirmed'}},[]);
 assert.equal(completed.complete,true);
});

test('orphan results preserve their public details and provenance',()=>{
 const result=model.projectTranscript(snapshot,[event(1,'tool_result','result without start',{tool:'status_get',status:'unknown',result:'recorded observation'})]);
 assert.equal(result.messages[0].content[1].type,'data');
 assert.equal(result.messages[0].content[1].data.details.result,'recorded observation');
 assert.equal(result.messages[0].content[1].data.evidenceMode,'live');
});
