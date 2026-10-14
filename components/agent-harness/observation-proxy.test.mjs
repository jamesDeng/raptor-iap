import test from 'node:test';import assert from 'node:assert/strict';import {createObservationTools} from './observation-tools.mjs';
const binding={requestId:'request',attemptId:'attempt',definitionSha256:'hash',envCode:'rdev.ali',clusterId:'cluster',operation:'db-proxy.replace-nodes'};
const scope={envCode:'rdev.ali',proxyCode:'proxy',targetDbCode:'db',groupId:'group',serverGroupId:'backend',application:{clusterId:'cluster',namespace:'raptor-test',name:'client',uid:'uid'}};
const config=()=>({version:1,transport:'gateway',url:'https://api.rdev.raptor-iap.top/v1/gateway-observations',token:'a'.repeat(64),binding,scope,expiresAt:new Date(Date.now()+120000).toISOString()});
test('remote probes preserve names and binding without provider credentials',async()=>{
 let calls=[];const c=config();const tools=createObservationTools({binding,config:c,replacementScope:scope,fetch:async(url,options)=>{calls.push({url,options});return new Response(JSON.stringify({data:{replicas:2}}));}});
 assert.deepEqual(await tools.deploymentRead({}),{replicas:2});const call=calls[0];assert.equal(call.url,c.url);assert.equal(call.options.redirect,'error');assert.equal(call.options.headers.Authorization,'Bearer '+c.token);assert.deepEqual(JSON.parse(call.options.body),{requestId:'request',attemptId:'attempt',definitionSha256:'hash',tool:'deployment_read',arguments:{}});
 await assert.rejects(tools.cloudRead({kind:'write'}),/InvalidObservation/);await assert.rejects(tools.deploymentRead({url:'http://evil'}),/InvalidObservation/);assert.equal(calls.length,1);
 c.expiresAt=new Date(0).toISOString();await assert.rejects(tools.deploymentRead({}),/InvalidObservation/);assert.equal(calls.length,1);
});
test('remote configuration rejects foreign binding scope endpoints and private credentials',()=>{
 for(const change of [{binding:{...binding,attemptId:'other'}},{scope:{...scope,groupId:'foreign'}},{url:'http://api.rdev.raptor-iap.top/v1/gateway-observations'},{url:'https://evil.example/v1/gateway-observations'},{private:{password:'secret'}}])assert.throws(()=>createObservationTools({binding,config:{...config(),...change},replacementScope:scope}),/InvalidObservation/);
});
test('remote errors and oversized output are sanitized',async()=>{
 for(const response of [new Response('secret failure',{status:503}),new Response('a'.repeat(262145)),new Response(JSON.stringify({data:{token:'a'.repeat(64)}}))]){
 const tools=createObservationTools({binding,config:config(),replacementScope:scope,fetch:async()=>response});await assert.rejects(tools.deploymentRead({}),e=>e.message==='InvalidObservation');}
});
test('remote observation calls are serialized for the single-flight Gateway proxy',async()=>{
 let active=0,peak=0;const seen=[];
 const tools=createObservationTools({binding,config:config(),replacementScope:scope,fetch:async(_url,options)=>{
  active++;peak=Math.max(peak,active);const tool=JSON.parse(options.body).tool;seen.push(tool);
  await new Promise(resolve=>setTimeout(resolve,5));active--;
  return active>0?new Response('busy',{status:429}):new Response(JSON.stringify({data:{tool}}));
 }});
 const result=await Promise.all([tools.cloudRead({kind:'fleet'}),tools.deploymentRead({}),tools.metricsRead({kind:'connections'})]);
 assert.equal(peak,1);assert.deepEqual(seen,['cloud_read','deployment_read','metrics_read']);assert.deepEqual(result.map(x=>x.tool),seen);
});
