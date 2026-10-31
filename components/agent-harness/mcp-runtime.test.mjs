import {operationToolSchema} from './operation-profile.mjs';
import test from 'node:test';import assert from 'node:assert/strict';
const binding={requestId:'11111111-1111-4111-8111-111111111111',attemptId:'22222222-2222-4222-8222-222222222222',operation:'application.question',objectKind:'application',objectCode:'app',envCode:'rdev.ali',skillsCommit:'0123456789012345678901234567890123456789',model:'gpt-5.6-luna'};
test('qualification MCP tool calls finish serially and stop after a failed read',async()=>{
 const {createGuardedFetch,createToolCallGate,serializeMcpToolCalls}=await import('./mcp-runtime.mjs');
 let failed=false,calls=0,releaseFirst;
 const gate=createToolCallGate(()=>failed);
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,fetch:async()=>{calls++;if(calls===1)await new Promise(resolve=>releaseFirst=resolve);return new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{isError:true,content:[{type:'text',text:'ScopeMismatch'}]}}),{headers:{'Content-Type':'application/json'}});},onDiagnostic:()=>{failed=true;}});
 const serial=serializeMcpToolCalls(guarded,gate);
 const init={method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'deployments_list',arguments:{envCode:binding.envCode,kind:'application',code:'app'}}})};
 const first=serial('https://infra.example/mcp',init),second=serial('https://infra.example/mcp',init);
 await Promise.resolve();assert.equal(calls,1);releaseFirst();
 assert.equal((await first).status,200);await assert.rejects(second,/PriorToolFailed/);assert.equal(calls,1);
});
test('a mismatched MCP response id stops the queued call before another service request',async()=>{
 const {createGuardedFetch,createToolCallGate,serializeMcpToolCalls}=await import('./mcp-runtime.mjs');let calls=0;
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,fetch:async()=>{calls++;return new Response(JSON.stringify({jsonrpc:'2.0',id:999,result:{structuredContent:{data:{envCode:binding.envCode,accountMatches:true,evidenceMode:'live',observedAt:new Date().toISOString()}}}}),{headers:{'Content-Type':'application/json'}});}});
 const serial=serializeMcpToolCalls(guarded,createToolCallGate());
 const init={method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'deployments_list',arguments:{envCode:binding.envCode,kind:'application',code:'app'}}})};
 const first=serial('https://infra.example/mcp',init),second=serial('https://infra.example/mcp',init);
 await assert.rejects(first,/InvalidMcpResponse/);await assert.rejects(second,/PriorToolFailed/);assert.equal(calls,1);
});
test('repository change connects only its Raptor MCP audience',async()=>{const {createScopedMcpRuntime,mcpServerNames}=await import('./mcp-runtime.mjs');assert.deepEqual(mcpServerNames({...binding,operation:'repository.change'}),['raptor']);assert.deepEqual(mcpServerNames(binding),['raptor','infra']);let config;const sdk={createMcpExtension:({loadConfig})=>{config=loadConfig();return {}}};await createScopedMcpRuntime({sdk,stateRoot:'/tmp/state',privateRoot:'/tmp/private',servers:{raptor:{url:'https://raptor.example/mcp',headers:{}},infra:{url:'https://infra.example/mcp',headers:{}}},binding:{...binding,operation:'repository.change'}});assert.deepEqual(config.servers.map(s=>s.name),['raptor']);});
test('scoped MCP transport blocks redirects, forbidden methods and selectors before network',async()=>{const {createGuardedFetch}=await import('./mcp-runtime.mjs');let calls=0;const guarded=createGuardedFetch({server:'raptor',url:'https://raptor.example/mcp',binding,fetch:async(url,init)=>{calls++;assert.equal(init.redirect,'error');return new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{content:[{type:'text',text:JSON.stringify({data:{requestId:binding.requestId,definition:{object:{kind:'application',code:'app'},envCode:'rdev.ali'}}})}]}}),{headers:{'Content-Type':'application/json'}})}});const invoke=(method,params)=>guarded('https://raptor.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method,params})});for(const [method,params]of [['tools/call',{name:'request_get',arguments:{requestId:'other'}}],['tools/call',{name:'pull_request_attach',arguments:{}}],['resources/read',{uri:'file:///auth.json'}]])await assert.rejects(()=>invoke(method,params));await assert.rejects(()=>guarded('https://elsewhere.example/mcp',{method:'POST'}));assert.equal(calls,0);await invoke('tools/call',{name:'request_get',arguments:{requestId:binding.requestId}});assert.equal(calls,1)});
test('MCP catalogs must contain exactly first-run reads and auth cannot redirect',async()=>{const {createGuardedFetch}=await import('./mcp-runtime.mjs');for(const tools of [[{name:'request_get'}],[{name:'request_get'},{name:'request_get'},{name:'environment_get'},{name:'object_get'}]]){const guarded=createGuardedFetch({server:'raptor',url:'https://raptor.example/mcp',binding,fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{tools}}),{headers:{'Content-Type':'application/json'}})});await assert.rejects(()=>guarded('https://raptor.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})}));}const guarded=createGuardedFetch({server:'raptor',url:'https://raptor.example/mcp',binding,fetch:async()=>new Response('',{status:302,headers:{Location:'https://elsewhere.example/'}})});await assert.rejects(()=>guarded('https://raptor.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'initialize'})}));});
test('MCP responses cannot archive attempt tokens or unbounded bodies',async()=>{const {createGuardedFetch}=await import('./mcp-runtime.mjs');for(const text of ['x'.repeat(65537),JSON.stringify({jsonrpc:'2.0',id:1,result:{note:'private-attempt-token'}})]){const guarded=createGuardedFetch({server:'raptor',url:'https://raptor.example/mcp',binding,knownSecrets:['private-attempt-token'],fetch:async()=>new Response(text,{headers:{'Content-Type':'application/json'}})});await assert.rejects(()=>guarded('https://raptor.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'initialize'})}));}});
test('status response matches the specifically requested discovered deployment',async()=>{const {createGuardedFetch}=await import('./mcp-runtime.mjs');const discovered=new Map([['one',{appCode:'app',clusterId:'cluster',namespace:'ns',name:'one',uid:'a'}],['two',{appCode:'app',clusterId:'cluster',namespace:'ns',name:'two',uid:'b'}]]);const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,discovered,fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{structuredContent:{data:{envCode:binding.envCode,objectCode:binding.objectCode,clusterId:'cluster',namespace:'ns',name:'two',uid:'b',evidenceMode:'live',observedAt:new Date().toISOString(),generation:1,observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1}},content:[]}}),{headers:{'Content-Type':'application/json'}})});await assert.rejects(()=>guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'deployment_status_get',arguments:{envCode:binding.envCode,appCode:binding.objectCode,workloadKey:'one'}}})}),/IdentityChanged/)});

test('MCP diagnostics classify failed read boundaries without exporting remote text',async()=>{
 const {createGuardedFetch}=await import('./mcp-runtime.mjs');const diagnostics=[];
 const rpc=args=>({method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'deployments_list',arguments:{kind:'application',code:'app',...args}}})});
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,onDiagnostic:x=>diagnostics.push(x),fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{isError:true,content:[{type:'text',text:'ScopeMismatch private-token'}]}}),{headers:{'Content-Type':'application/json'}})});
 await assert.rejects(()=>guarded('https://infra.example/mcp',rpc({envCode:'other'})),/ScopeMismatch/);
 const response=await guarded('https://infra.example/mcp',rpc({envCode:binding.envCode}));assert.equal((await response.json()).result.content[0].text,'McpToolFailed');
 const denied=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,onDiagnostic:x=>diagnostics.push(x),fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{isError:true,content:[{type:'text',text:'ScopeMismatch'}]}}),{headers:{'Content-Type':'application/json'}})});
 await denied('https://infra.example/mcp',rpc({envCode:binding.envCode}));
 const malformed=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,onDiagnostic:x=>diagnostics.push(x),fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{structuredContent:{data:{resource:{envCode:'other',kind:'application',code:'app'},applicationWorkloads:[]}}}}),{headers:{'Content-Type':'application/json'}})});
 await assert.rejects(()=>malformed('https://infra.example/mcp',rpc({envCode:binding.envCode})),/IdentityChanged/);
 const unreachable=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,onDiagnostic:x=>diagnostics.push(x),fetch:async()=>{throw Error('private network detail')}});
 await assert.rejects(()=>unreachable('https://infra.example/mcp',rpc({envCode:binding.envCode})),/private network detail/);
 assert.deepEqual(diagnostics,[{server:'infra',tool:'deployments_list',category:'local-selector'},{server:'infra',tool:'deployments_list',category:'remote-tool-error'},{server:'infra',tool:'deployments_list',category:'remote-ScopeMismatch'},{server:'infra',tool:'deployments_list',category:'local-evidence'},{server:'infra',tool:'deployments_list',category:'transport-error'}]);
});

test('read-only runtime filters the known restart tool and blocks its use', async()=>{const {createGuardedFetch}=await import('./mcp-runtime.mjs');let calls=0;const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,fetch:async()=>{calls++;return new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{tools:['cloud_identity_get','deployments_list','deployment_status_get','deployment_restart'].map(name=>({name,...(['deployments_list','deployment_status_get'].includes(name)?{inputSchema:operationToolSchema('infra',name)}:{})}))}}),{headers:{'Content-Type':'application/json'}})}});const r=await guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})});assert.equal((await r.json()).result.tools.length,2);await assert.rejects(()=>guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:2,method:'tools/call',params:{name:'deployment_restart',arguments:{}}})}),/ScopeMismatch/);assert.equal(calls,1);});


test('catalog filters unrelated tools while retaining only question capabilities',async()=>{
 const {createGuardedFetch}=await import('./mcp-runtime.mjs');
 const names=['cloud_identity_get','deployments_list','deployment_status_get','db_proxy_scale','future_admin_tool'];
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{tools:names.map(name=>({name,...(['deployments_list','deployment_status_get'].includes(name)?{inputSchema:operationToolSchema('infra',name)}:{})}))}}),{headers:{'Content-Type':'application/json'}})});
 const response=await guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})});
 assert.deepEqual((await response.json()).result.tools.map(t=>t.name),['deployments_list','deployment_status_get']);
});

test('qualification tool catalog exposes exact trusted read selectors to the model',async()=>{
 const {createGuardedFetch}=await import('./mcp-runtime.mjs');
 const {operationCapabilities,operationToolSchema}=await import('./operation-profile.mjs');
 const b={...binding,operation:'db-proxy.replace-nodes',objectKind:'db-proxy',objectCode:'DP00001',executionMode:'qualification'};
 const scope={envCode:'rdev.ali',proxyCode:'DP00001',groupId:'asg-bound',serverGroupId:'sgp-bound',targetDbCode:'DB00001',application:{envCode:'rdev.ali',appCode:'APP00001',clusterId:'cluster-bound',namespace:'pgcat',name:'client',uid:'uid-bound'}};
 const tools=operationCapabilities(b,scope).infra.map(name=>({name,inputSchema:operationToolSchema('infra',name)}));
 const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding:b,replacementScope:scope,fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{tools}}),{headers:{'Content-Type':'application/json'}})});
 const response=await guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})});
 const visible=Object.fromEntries((await response.json()).result.tools.map(t=>[t.name,t.inputSchema.properties]));
 assert.equal(visible.cloud_identity_get,undefined);
 assert.deepEqual(visible.deployments_list.kind.enum,['db-proxy']);
 assert.deepEqual(visible.deployments_list.code.enum,['DP00001']);
 assert.deepEqual(visible.deployment_status_get.appCode.enum,['APP00001']);
 assert.equal(visible.deployment_status_get.uid,undefined);assert.equal(visible.deployment_status_get.workloadKey.type,'string');
 assert.equal(tools[0].inputSchema.properties.code.enum,undefined);
});

test('qualification approval catalog exposes the exact harmless request-bound action',async()=>{
 const {createGuardedFetch}=await import('./mcp-runtime.mjs');
 const {operationCapabilities,operationToolSchema}=await import('./operation-profile.mjs');
 const b={...binding,operation:'db-proxy.replace-nodes',objectKind:'db-proxy',objectCode:'DP00001',executionMode:'qualification'};
 const scope={envCode:'rdev.ali',proxyCode:'DP00001',groupId:'asg-bound',serverGroupId:'sgp-bound',targetDbCode:'DB00001',application:{envCode:'rdev.ali',appCode:'APP00001',clusterId:'cluster-bound',namespace:'pgcat',name:'client',uid:'uid-bound'}};
 const tools=operationCapabilities(b,scope).raptor.map(name=>({name,inputSchema:operationToolSchema('raptor',name)}));
 const guarded=createGuardedFetch({server:'raptor',url:'https://raptor.example/mcp',binding:b,replacementScope:scope,fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{tools}}),{headers:{'Content-Type':'application/json'}})});
 const response=await guarded('https://raptor.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})});
 const visible=(await response.json()).result.tools.find(t=>t.name==='approval_request').inputSchema;
 assert.deepEqual(visible.properties.requestId.enum,[b.requestId]);
 assert.deepEqual(visible.properties.envCode.enum,['rdev.ali']);
 assert.deepEqual(visible.properties.interface.enum,['runtime.qualification-resume']);
 assert.deepEqual(visible.properties.target,{type:'object',properties:{proxyCode:{type:'string',enum:['DP00001']}},required:['proxyCode'],additionalProperties:false});
 assert.deepEqual(visible.properties.parameters,{type:'object',properties:{phase:{type:'string',enum:['resume']}},required:['phase'],additionalProperties:false});
 assert.deepEqual(visible.properties.actionId,{type:'string'});
 assert.equal(tools.find(t=>t.name==='approval_request').inputSchema.properties.interface.enum,undefined);
});

test('qualification pause catalog admits only the exact human-approval wait through Pi validation',async()=>{
 const {createGuardedFetch}=await import('./mcp-runtime.mjs');
 const {operationCapabilities,operationToolSchema}=await import('./operation-profile.mjs');
 const entry=new URL(import.meta.resolve('@earendil-works/pi-coding-agent'));
 const {validateToolArguments}=await import('@earendil-works/pi-ai/utils/validation');
 const b={...binding,operation:'db-proxy.replace-nodes',objectKind:'db-proxy',objectCode:'DP00001',executionMode:'qualification'};
 const scope={envCode:'rdev.ali',proxyCode:'DP00001',groupId:'asg-bound',serverGroupId:'sgp-bound',targetDbCode:'DB00001',application:{envCode:'rdev.ali',appCode:'APP00001',clusterId:'cluster-bound',namespace:'pgcat',name:'client',uid:'uid-bound'}};
 const tools=operationCapabilities(b,scope).raptor.map(name=>({name,inputSchema:operationToolSchema('raptor',name)}));
 const guarded=createGuardedFetch({server:'raptor',url:'https://raptor.example/mcp',binding:b,replacementScope:scope,fetch:async()=>new Response(JSON.stringify({jsonrpc:'2.0',id:1,result:{tools}}),{headers:{'Content-Type':'application/json'}})});
 const response=await guarded('https://raptor.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})});
 const visible=(await response.json()).result.tools.find(t=>t.name==='request_pause').inputSchema;
 assert.deepEqual(visible.properties.requestId.enum,[b.requestId]);
 assert.deepEqual(visible.properties.reason.enum,['waiting_approval']);
 assert.equal(visible.additionalProperties,false);
 const tool={name:'mcp__raptor__request_pause',parameters:visible};
 const call=args=>validateToolArguments(tool,{name:tool.name,arguments:args});
 assert.deepEqual(call({requestId:b.requestId,reason:'waiting_approval'}),{requestId:b.requestId,reason:'waiting_approval'});
 for(const args of [{requestId:b.requestId,reason:'waiting_review'},{requestId:'foreign',reason:'waiting_approval'},{requestId:b.requestId,reason:'waiting_approval',approvalId:'foreign'}])assert.throws(()=>call(args),/Validation failed/);
 assert.equal(tools.find(t=>t.name==='request_pause').inputSchema.properties.reason.enum,undefined);
});

test('qualification pause selector rejects a foreign or malformed wait before reaching Raptor',async()=>{
 const {createGuardedFetch}=await import('./mcp-runtime.mjs');
 const b={...binding,operation:'db-proxy.replace-nodes',objectKind:'db-proxy',objectCode:'DP00001',executionMode:'qualification'};
 const scope={envCode:'rdev.ali',proxyCode:'DP00001',groupId:'asg-bound',serverGroupId:'sgp-bound',targetDbCode:'DB00001',application:{envCode:'rdev.ali',appCode:'APP00001',clusterId:'cluster-bound',namespace:'pgcat',name:'client',uid:'uid-bound'}};
 let remoteCalls=0;
 const diagnostics=[];
 const guarded=createGuardedFetch({server:'raptor',url:'https://raptor.example/mcp',binding:b,replacementScope:scope,onDiagnostic:d=>diagnostics.push(d),fetch:async()=>{remoteCalls++;throw Error('unexpected remote call');}});
 for(const args of [{requestId:b.requestId,reason:'waiting_review'},{requestId:'foreign',reason:'waiting_approval'},{requestId:b.requestId,reason:'waiting_approval',approvalId:'foreign'}])await assert.rejects(()=>guarded('https://raptor.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'request_pause',arguments:args}})}),/ScopeMismatch/);
 assert.equal(remoteCalls,0);
 assert.deepEqual(diagnostics.map(d=>d.category),['local-selector','local-selector','local-selector']);
});

test('logical application discovery retains opaque workload keys and refuses foreign status selection',async()=>{
 const {createGuardedFetch}=await import('./mcp-runtime.mjs');const discovered=new Map(),evidence=[];
 const w={type:'kubernetes',workloadKey:'opaque:/not-a-provider-id',clusterId:'cluster',namespace:'ns',name:'app',uid:'uid',state:'Running'};
 let damage='';const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,discovered,onEvidence:rows=>evidence.push(...rows),fetch:async(_url,init)=>{
  const rpc=JSON.parse(init.body),status={envCode:binding.envCode,objectCode:'app',clusterId:'cluster',namespace:'ns',name:'app',uid:'uid',evidenceMode:'live',observedAt:new Date(Date.now()-1000).toISOString(),generation:2,observedGeneration:2,replicas:2,updatedReplicas:2,readyReplicas:2};
  const data=rpc.params.name==='deployments_list'?{resource:{envCode:binding.envCode,kind:'application',code:'app'},applicationWorkloads:[w]}:{resource:{envCode:binding.envCode,kind:'application',code:'app'},applicationWorkload:{...w,...(damage==='key'?{workloadKey:'foreign'}:{})},status};
  const result={structuredContent:{data},content:[{type:'text',text:JSON.stringify({data:damage==='conflict'?{}:data})}]};return Response.json({jsonrpc:'2.0',id:1,result});
 }});
 const call=(name,args)=>guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name,arguments:args}})});
 await call('deployments_list',{envCode:binding.envCode,kind:'application',code:'app'});assert.equal(discovered.has(w.workloadKey),true);assert.equal(evidence[0].evidenceMode,'discovery');
 await call('deployment_status_get',{envCode:binding.envCode,appCode:'app',workloadKey:w.workloadKey});assert.equal(evidence.at(-1).identity.uid,'uid');assert.ok(Date.parse(evidence.at(-1).observedAt)<Date.now()-500);
 damage='key';await assert.rejects(()=>call('deployment_status_get',{envCode:binding.envCode,appCode:'app',workloadKey:w.workloadKey}),/IdentityChanged/);
 damage='conflict';await assert.rejects(()=>call('deployments_list',{envCode:binding.envCode,kind:'application',code:'app'}),/InvalidEvidence/);
});

test('question catalog rejects legacy provider selectors and malformed current schemas before exposure',async()=>{
 const {createGuardedFetch}=await import('./mcp-runtime.mjs');const {operationToolSchema}=await import('./operation-profile.mjs');
 const legacy={type:'object',properties:Object.fromEntries(['envCode','appCode','clusterId','namespace','name','uid'].map(key=>[key,{type:'string'}])),required:['envCode','appCode','clusterId','namespace','name','uid'],additionalProperties:false};
 const current=operationToolSchema('infra','deployment_status_get');
 for(const inputSchema of [legacy,undefined,{...current,required:['envCode','appCode','workloadKey']}]){
  let exposed=0;const tools=[{name:'deployments_list',inputSchema:operationToolSchema('infra','deployments_list')},{name:'deployment_status_get',inputSchema}];
  const guarded=createGuardedFetch({server:'infra',url:'https://infra.example/mcp',binding,onCatalog:()=>exposed++,fetch:async()=>Response.json({jsonrpc:'2.0',id:1,result:{tools}})});
  await assert.rejects(()=>guarded('https://infra.example/mcp',{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})}),/UnexpectedToolSchema/);assert.equal(exposed,0);
 }
});
test('question and repository catalogs retain legitimate current schemas including optional workload key',async()=>{
 const {createGuardedFetch}=await import('./mcp-runtime.mjs');const {operationToolSchema,operationCapabilities}=await import('./operation-profile.mjs');
 for(const b of [binding,{...binding,operation:'repository.change'}])for(const [server,names] of Object.entries(operationCapabilities(b))){
  if(!names.length)continue;const tools=names.map(name=>({name,inputSchema:operationToolSchema(server,name)}));
  const guarded=createGuardedFetch({server,url:`https://${server}.example/mcp`,binding:b,fetch:async()=>Response.json({jsonrpc:'2.0',id:1,result:{tools}})});
  const response=await guarded(`https://${server}.example/mcp`,{method:'POST',body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})});const visible=(await response.json()).result.tools;
  assert.deepEqual(visible,tools);const status=visible.find(tool=>tool.name==='deployment_status_get');if(status){assert.deepEqual(status.inputSchema.required,['envCode','appCode']);assert.equal(status.inputSchema.properties.workloadKey.type,'string');}
 }
});
