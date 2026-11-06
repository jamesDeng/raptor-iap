import test from 'node:test';
import assert from 'node:assert/strict';
import {createConversationRefresh} from './app.js';

test('connected progress refreshes conversation readiness without reconnecting',async()=>{
 let context={authenticated:true,id:'A',mode:'live',status:'running'},canSend=false;
 const reads=[],renewals=[];
 const tick=createConversationRefresh({current:()=>context,refresh:async id=>{reads.push(id);canSend=true},renew:async id=>renewals.push(id)});
 await tick();assert.equal(canSend,true);assert.deepEqual(reads,['A']);assert.deepEqual(renewals,[]);
 context={...context,mode:'complete'};await tick();assert.deepEqual(renewals,['A']);
});
test('slow refresh does not overlap or renew a newly selected request',async()=>{
 let context={authenticated:true,id:'A',mode:'complete',status:'running'},resolve;const reads=[],renewals=[];
 const tick=createConversationRefresh({current:()=>context,refresh:id=>{reads.push(id);return new Promise(r=>resolve=r)},renew:async id=>renewals.push(id)});
 const first=tick();await tick();assert.deepEqual(reads,['A']);context={...context,id:'B'};resolve();await first;assert.deepEqual(renewals,[]);
});
test('signed-out and unselected views do not refresh, and failure permits retry',async()=>{
 let context={authenticated:false,id:'A',mode:'live',status:'running'},calls=0;const errors=[];
 const tick=createConversationRefresh({current:()=>context,refresh:async()=>{calls++;throw Error('Unavailable')},renew:async()=>{},onError:e=>errors.push(e.message)});
 await tick();context={...context,authenticated:true,id:null};await tick();assert.equal(calls,0);
 context={...context,id:'A'};await tick();await tick();assert.equal(calls,2);assert.deepEqual(errors,['Unavailable','Unavailable']);
});
