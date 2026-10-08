import test from 'node:test';
import assert from 'node:assert/strict';
import * as app from './app.js';
const target=mode=>({details:{evidenceMode:mode}});
const direct=targets=>({request:{status:'completed',definition:{type:'direct',operation:'application.restart'}},targets});
test('direct restart header reflects actual target evidence, including mixed and missing evidence',()=>{
 assert.equal(typeof app.requestStateText,'function');
 assert.equal(app.requestStateText(direct([target('live'),target('live')])),'completed · live execution');
 assert.equal(app.requestStateText(direct([target('simulated')])),'completed · simulated execution');
 assert.equal(app.requestStateText(direct([target('live'),target('simulated')])),'completed · mixed evidence');
 assert.equal(app.requestStateText(direct([target('live'),{}])),'completed · unverified execution');
 assert.equal(app.requestStateText(direct([])),'completed · unverified execution');
});
test('agent runtime and unavailable synchronization retain their existing meaning',()=>{
 assert.equal(typeof app.requestStateText,'function');
 assert.equal(app.requestStateText({request:{status:'running',definition:{type:'agent'}},execution:{runtimeMode:'live'}}),'running · live execution');
 assert.equal(app.requestStateText({...direct([target('live')]),executionAvailable:false}),'completed (last saved); Execution sync unavailable');
});
