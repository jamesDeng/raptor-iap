import test from 'node:test';import assert from 'node:assert/strict';
import {createDirectProgressController} from './direct-progress.js';
class Socket{static all=[];constructor(url){this.url=url;Socket.all.push(this)}send(s){this.sent=JSON.parse(s)}close(){this.closed=true}message(v){this.onmessage?.({data:JSON.stringify(v)})}}
test('direct stream uses ticket, deduplicates replay, renews with cursor and ignores old request',async()=>{
 const jobs=[];const tickets=[];const controller=createDirectProgressController({read:async()=>({events:[{sequence:400}]}),ticket:async id=>{tickets.push(id);return {token:'opaque',url:'ws://localhost:8874/v1/progress'}},Socket,schedule:fn=>{jobs.push(fn);return jobs.length},cancel:()=>{},onChange:()=>{}});
 await controller.select('A');let socket=Socket.all.at(-1);socket.onopen();assert.deepEqual(socket.sent,{type:'subscribe',requestId:'A',token:'opaque',afterSequence:0});
 socket.message({type:'event',event:{requestId:'A',sequence:1}});socket.message({type:'event',event:{requestId:'A',sequence:1}});socket.message({type:'event',event:{requestId:'A',sequence:2}});assert.equal(controller.state.events.length,2);
 socket.onclose();await jobs.pop()();let second=Socket.all.at(-1);second.onopen();assert.equal(second.sent.afterSequence,2);assert.equal(tickets.length,2);
 await controller.select('B');socket.message({type:'event',event:{requestId:'A',sequence:99}});assert.equal(controller.state.requestId,'B');assert.equal(controller.state.cursor,0);
 controller.close();assert.equal(Socket.all.at(-1).closed,true);
});
test('explicitly disabled realtime uses HTTP polling; complete stops reconnect',async()=>{
 const jobs=[];let reads=0;let sockets=Socket.all.length;
 const controller=createDirectProgressController({read:async()=>{reads++;return {events:[{sequence:400}]}},ticket:async()=>{throw new Error('ProgressNotConfigured')},Socket,schedule:fn=>{jobs.push(fn);return jobs.length},cancel:()=>{},onChange:()=>{}});
 await controller.select('direct');assert.equal(controller.state.mode,'polling');assert.equal(controller.state.cursor,400);assert.equal(Socket.all.length,sockets);await jobs.pop()();assert.equal(reads,2);controller.close();
 const live=createDirectProgressController({read:async()=>{throw new Error('HTTP history must not seed Gateway cursor')},ticket:async()=>({token:'opaque',url:'ws://localhost:8874/v1/progress'}),Socket,schedule:fn=>{jobs.push(fn);return jobs.length},cancel:()=>{},onChange:()=>{}});
 await live.select('agent');const socket=Socket.all.at(-1);socket.onopen();socket.message({type:'complete'});assert.equal(live.state.mode,'complete');assert.equal(socket.closed,true);live.close();
});
test('outage shows saved history without seeding Gateway cursor; disabled renewal resets HTTP cursor',async()=>{
 const jobs=[];const cursors=[];let issuance=0;
 const c=createDirectProgressController({read:async(id,after)=>{cursors.push(after);return {events:[{sequence:400}],syncUnavailable:true}},ticket:async()=>{issuance++;if(issuance===1)throw new Error('Unavailable');if(issuance===3)throw new Error('ProgressNotConfigured');return {token:'opaque',url:'ws://localhost:8874/v1/progress'}},Socket,schedule:fn=>{jobs.push(fn);return jobs.length},cancel:()=>{},onChange:()=>{}});
 await c.select('A');assert.equal(c.state.events[0].sequence,400);await jobs.pop()();const s=Socket.all.at(-1);s.onopen();assert.equal(s.sent.afterSequence,0);s.message({type:'event',event:{requestId:'A',sequence:1}});assert.deepEqual(c.state.events.map(v=>v.sequence),[1]);s.onclose();await jobs.pop()();assert.equal(cursors.at(-1),0);assert.equal(c.state.mode,'polling');assert.deepEqual(c.state.events.map(v=>v.sequence),[400]);c.close();
});
test('completion socket renews after accepted input with Gateway cursor',async()=>{const c=createDirectProgressController({read:async()=>({events:[]}),ticket:async()=>({token:'opaque',url:'ws://localhost:8874/v1/progress'}),Socket,schedule:()=>0,cancel:()=>{}});await c.select('r');const s=Socket.all.at(-1);s.onopen();s.message({type:'event',event:{requestId:'r',sequence:12}});s.message({type:'complete'});assert.equal(typeof c.renew,'function');await c.renew('r');const next=Socket.all.at(-1);next.onopen();assert.equal(next.sent.afterSequence,12);assert.equal(c.state.events.length,1);await c.renew('other');assert.equal(c.state.requestId,'r');c.close()});
