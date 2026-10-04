import test from 'node:test';
import assert from 'node:assert/strict';
import {validateParameters,groupEnvironments,createProgressController,discoveryView} from '../../components/raptor/web/poc/app.js';
test('required fields and scalar values are validated',()=>{const schema={required:['size'],properties:{size:{type:'integer',minimum:1}}};assert.equal(validateParameters(schema,{}).length,1);assert.equal(validateParameters(schema,{size:1}).length,0);assert.ok(validateParameters(schema,{size:'1'}).length);assert.ok(validateParameters(schema,{size:0}).length);});
test('environment selector groups globally unique environments',()=>{const groups=groupEnvironments([{code:'adev',groupCode:'biz-a'},{code:'bdev',groupCode:'biz-b'}]);assert.equal(groups.get('biz-a')[0].code,'adev');assert.equal(groups.size,2);});
test('saved history and two-second polling survive page refresh',async()=>{let delay;const p=createProgressController({read:async()=>({events:[{sequence:1,summary:'saved'}]}),schedule:(fn,ms)=>{delay=ms;return 1},cancel:()=>{}});await p.select('request');assert.equal(p.state.events[0].summary,'saved');assert.equal(delay,2000);p.close();});
test('stale response cannot replace selected request progress',async()=>{let resolve;const p=createProgressController({read:id=>id==='old'?new Promise(r=>resolve=r):Promise.resolve({events:[{sequence:1,summary:'new'}]}),schedule:()=>1,cancel:()=>{}});const old=p.select('old');await p.select('new');resolve({events:[{sequence:5,summary:'old'}]});await old;assert.equal(p.state.requestId,'new');assert.equal(p.state.events[0].summary,'new');p.close();});
test('discovery failure is different from an empty deployment list',()=>{assert.equal(discoveryView(null,new Error('Unavailable')).state,'unavailable');assert.equal(discoveryView([],null).state,'empty');});

// Exercise the page loader itself, including its ordering around the controller.
import {readFileSync} from 'node:fs';
const pageSource=readFileSync(new URL('../../components/raptor/web/poc/app.js',import.meta.url),'utf8');
test('page navigation ignores a request load that finishes after a newer selection',async()=>{
 const source=pageSource.match(/async function loadRequest\(id\)\{([\s\S]*?)\n async function refreshRequest/)[1];
 let resolveOld,selectedProgress;
 const progress={close(){selectedProgress=null},async select(id){selectedProgress=id}};
 const refresh=id=>id==='old'?new Promise(resolve=>resolveOld=resolve):Promise.resolve();
 const load=new Function('location','panel','refreshRequest','progress',`let requestId=null,requestEpoch=0; return async function loadRequest(id){${source}`)({},()=>{},refresh,progress);
 const old=load('old');await load('new');resolveOld();await old;assert.equal(selectedProgress,'new');
});
test('same pending browser submission retains identity after lost acknowledgement',async()=>{
 const source=pageSource.match(/async function submit\(body\)\{([\s\S]*?)\n async function loadRequest/)[1];
 const calls=[];let resolveFirst;const api=async(path,opts)=>{calls.push(opts.headers['Idempotency-Key']);if(calls.length===1)await new Promise(resolve=>resolveFirst=resolve);if(calls.length===1)throw new Error('lost response');return{id:'same-request'}};
 const submit=new Function('api','loadRequest','crypto',`let pendingSubmission=null; return async function submit(body){${source}`)(api,async()=>{},crypto);
 const first=submit({type:'agent'});const duplicate=submit({type:'agent'});resolveFirst();await Promise.allSettled([first,duplicate]);
 await submit({type:'agent'});assert.equal(calls.length,2);assert.equal(calls[0],calls[1]);
});
