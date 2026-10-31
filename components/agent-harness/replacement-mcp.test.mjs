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
 const args={requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',actionId:'action',desiredCapacity:3};
 const call=v=>guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'db_proxy_scale',arguments:v}})});
 await assert.rejects(()=>call({...args,groupId:'foreign'}),/ScopeMismatch/);assert.equal(calls,0);
 await call(args);assert.equal(calls,1);assert.equal(rows[0].state.outcome,'unknown');
});
test('provider accepted receipts become submitted evidence and retained action results',async()=>{
 for(const [tool,args,data] of [
  ['db_proxy_node_protection_set',{requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',nodeIds:['old-a','old-b'],protected:true},{outcome:'accepted',instanceIds:['old-a','old-b'],evidenceMode:'live'}],
  ['db_proxy_scale',{requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',actionId:'expand',desiredCapacity:4},{outcome:'accepted',groupId:'group',previousDesiredCapacity:2,desiredCapacity:4,evidenceMode:'live'}],
  ['db_proxy_nodes_deregister',{requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',nodeIds:['old-a']},{outcome:'accepted',instanceIds:['old-a'],evidenceMode:'live'}]
 ]){
  const rows=[];let retained;
  const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding:b,replacementScope:scope,onEvidence:x=>rows.push(...x),onToolResult:(_server,_tool,_args,result)=>{retained=result;},fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{structuredContent:{data},content:[]}}),{headers:{'Content-Type':'application/json'}})});
  await guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:tool,arguments:args}})});
  assert.equal(rows[0].state.outcome,'submitted',tool);
  assert.equal(retained.outcome,'submitted',tool);
 }
});
test('proxy discovery uses typed opaque workload identity without fabricated freshness',async()=>{
 const rows=[];
 const data={resource:{envCode:'rdev.ali',kind:'db-proxy',code:'proxy'},pgproxyWorkload:{type:'ess',code:'proxy',workloadKey:'opaque-is-not-group',state:'active',targetDbCode:'db'}};
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding:b,replacementScope:scope,onEvidence:x=>rows.push(...x),fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{structuredContent:{data},content:[]}}),{headers:{'Content-Type':'application/json'}})});
 const call=()=>guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'deployments_list',arguments:{envCode:'rdev.ali',kind:'db-proxy',code:'proxy'}}})});
 await call();assert.equal(rows[0].identity.resourceId,undefined);assert.equal(rows[0].identity.uid,undefined);assert.equal(rows[0].evidenceMode,'discovery');assert.equal(rows[0].sourceObservedAt,undefined);
 data.pgproxyWorkload.targetDbCode='foreign';await assert.rejects(call,/IdentityChanged/);
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
test('replacement approval schema binds only the logical proxy and exact reduction interface',async()=>{
 const {validateToolArguments}=await import('@earendil-works/pi-ai/utils/validation');const tools=operationCapabilities(b,scope).raptor.map(name=>({name,inputSchema:operationToolSchema('raptor',name)}));
 const guarded=createGuardedFetch({server:'raptor',url:'https://raptor.example/mcp',binding:b,replacementScope:scope,fetch:async()=>Response.json({jsonrpc:'2.0',id:1,result:{tools}})});const response=await guarded('https://raptor.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})});const schema=(await response.json()).result.tools.find(t=>t.name==='approval_request').inputSchema;
 const tool={name:'mcp__raptor__approval_request',parameters:schema};const args={requestId:b.requestId,envCode:b.envCode,actionId:'shrink',interface:'db-proxy.scale-in',target:{proxyCode:'proxy'},parameters:{desiredCapacity:2}};
 assert.deepEqual(validateToolArguments(tool,{name:tool.name,arguments:args}),args);
 for(const wrong of [{...args,target:{proxyCode:'proxy',groupId:'group'}},{...args,interface:'ess.scale-in'},{...args,target:{proxyCode:'foreign'}}])assert.throws(()=>validateToolArguments(tool,{name:tool.name,arguments:wrong}),/Validation failed/);
});
test('restart receipt must bind the exact request and private application identity',async()=>{
 const {replacementEvidence}=await import('./replacement-evidence.mjs');const args={requestId:b.requestId,envCode:b.envCode,appCode:'client',workloadKey:'opaque-key'};const data={evidenceMode:'live',accepted:true,generation:2,target:{requestId:b.requestId,...scope.application}};
 assert.doesNotThrow(()=>replacementEvidence('infra','deployment_restart',data,b,scope,args));
 assert.throws(()=>replacementEvidence('infra','deployment_restart',{...data,target:{...data.target,requestId:'foreign'}},b,scope,args),/InvalidEvidence/);
});
