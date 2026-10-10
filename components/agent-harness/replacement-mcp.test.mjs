import test from 'node:test';
import assert from 'node:assert/strict';
import {createGuardedFetch} from './mcp-runtime.mjs';
import {operationCapabilities,operationToolSchema} from './operation-profile.mjs';
const b={requestId:'request',envCode:'rdev.ali',objectKind:'db-proxy',objectCode:'proxy',operation:'db-proxy.replace-nodes',clusterId:'cluster'};
const scope={envCode:'rdev.ali',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',targetDbCode:'db',application:{envCode:'rdev.ali',appCode:'client',clusterId:'cluster',namespace:'test',name:'traffic',uid:'uid'}};
test('replacement MCP exposes required commands while filtering unrelated tools',async()=>{
 const names=[...operationCapabilities(b,scope).infra,'unrelated_tool'];
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding:b,replacementScope:scope,fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{tools:names.map(name=>({name,...(name==='unrelated_tool'?{}:{inputSchema:operationToolSchema('infra',name)})}))}}),{headers:{'Content-Type':'application/json'}})});
 const response=await guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})});
 assert.deepEqual((await response.json()).result.tools.map(x=>x.name),names.slice(0,-1));
});
test('replacement MCP accepts proxy identity and preserves unknown acknowledgement',async()=>{
 const rows=[];let calls=0;
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding:b,replacementScope:scope,onEvidence:x=>rows.push(...x),fetch:async()=>{calls++;return new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{structuredContent:{data:{outcome:'unknown',groupId:'group',previousDesiredCapacity:4,desiredCapacity:3,evidenceMode:'live'}},content:[]}}),{headers:{'Content-Type':'application/json'}})}});
 const args={requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',groupId:'group',actionId:'action',desiredCapacity:3};
 const call=v=>guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'db_proxy_scale',arguments:v}})});
 await assert.rejects(()=>call({...args,groupId:'foreign'}),/ScopeMismatch/);assert.equal(calls,0);
 await call(args);assert.equal(calls,1);assert.equal(rows[0].state.outcome,'unknown');
});
test('proxy discovery binds resource ID and target database without Kubernetes UID fiction',async()=>{
 const rows=[];
 const data=[{resourceId:'group',kind:'db-proxy',envCode:'rdev.ali',objectCode:'proxy',name:'proxy',state:'active','target-db-code':'db',evidenceMode:'live',observedAt:new Date().toISOString()}];
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding:b,replacementScope:scope,onEvidence:x=>rows.push(...x),fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{structuredContent:{data},content:[]}}),{headers:{'Content-Type':'application/json'}})});
 const call=()=>guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'deployments_list',arguments:{envCode:'rdev.ali',kind:'db-proxy',code:'proxy'}}})});
 await call();assert.equal(rows[0].identity.resourceId,'group');assert.equal(rows[0].identity.uid,undefined);
 data[0]['target-db-code']='foreign';await assert.rejects(call,/IdentityChanged/);
});
test('pause lifecycle hooks guard before transport and receive validated result',async()=>{
 let calls=0,seen;
 const data={requestId:'request',approvalId:'approval',state:'pending'};
 const guarded=createGuardedFetch({server:'raptor',url:'https://raptor.example/mcp',binding:b,replacementScope:scope,beforeTool:(_server,tool)=>{if(tool==='object_get')throw Error('ApprovalWaiting');},onToolResult:(server,tool,args,result)=>{seen={server,tool,args,result};},fetch:async()=>{calls++;return new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{structuredContent:{data},content:[]}}),{headers:{'Content-Type':'application/json'}})}});
 const call=(name,args)=>guarded('https://raptor.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name,arguments:args}})});
 await assert.rejects(()=>call('object_get',{requestId:'request',kind:'db-proxy',code:'proxy'}),/ApprovalWaiting/);assert.equal(calls,0);
 await call('approval_get',{requestId:'request',approvalId:'approval'});assert.equal(seen.tool,'approval_get');assert.equal(seen.result.approvalId,'approval');
});

test('invalid selectors never enter the retained mutation journal',async()=>{
 let hooks=0;
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding:b,replacementScope:scope,beforeTool:()=>hooks++,fetch:async()=>{throw Error('must not transport');}});
 await assert.rejects(()=>guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'db_proxy_scale',arguments:{requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',groupId:'foreign',actionId:'expand',desiredCapacity:4}}})}),/ScopeMismatch/);
 assert.equal(hooks,0);
});
