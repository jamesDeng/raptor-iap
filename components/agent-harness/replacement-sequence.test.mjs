import test from 'node:test';
import assert from 'node:assert/strict';
import {ReplacementTrace} from './replacement-trace.mjs';
import {ReplacementReadiness} from './replacement-readiness.mjs';
const module=await import('./replacement-sequence.mjs').catch(()=>({}));
const {ReplacementSequence}=module;
import {binding,scope,old,added,setup,observations,observe,argsFor,mutate,expanded,withdrawn,waited,requestWithdrawalWait,restarted,protect} from './replacement-sequence-fixtures.mjs';
test('fleet alone cannot unlock any initial write; complete native reads do',()=>{
 const x=setup();x.gate.observe('fleet',observations().fleet);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_node_protection_set',protect(old)),/MissingEvidence/);
 observe(x);assert.doesNotThrow(()=>x.gate.beforeMutation('db_proxy_node_protection_set',protect(old)));
});
for(const omitted of ['backend','capacity','metrics','deployment','trafficFailures','connections'])test('initial writes require '+omitted,()=>{
 const x=setup(),e=observations();delete e[omitted];observe(x,e);assert.throws(()=>x.gate.beforeMutation('db_proxy_scale',argsFor('db_proxy_scale',{actionId:'expand',desiredCapacity:4})),/MissingEvidence/);
});
test('profile absent or incompatible cannot execute even with complete observations',()=>{
 const x=setup();for(const profile of [undefined,'staged-v1','skills-v0.5.0'])assert.throws(()=>new ReplacementSequence(x.session,x.trace,x.readiness,profile),/InvalidSkillsRelease/);
});
test('direct sequence prohibits desired three, second expansion and foreign protection',()=>{
 const x=expanded();for(const args of [{actionId:'three',desiredCapacity:3},{actionId:'again',desiredCapacity:4}])assert.throws(()=>x.gate.beforeMutation('db_proxy_scale',argsFor('db_proxy_scale',args)),/MissingEvidence|MutationAlreadySubmitted/);
 for(const ids of [['new-a','foreign'],['new-a','new-a'],['old-a','new-b']])assert.throws(()=>x.gate.beforeMutation('db_proxy_node_protection_set',protect(ids)),/MissingEvidence/);
 assert.deepEqual(x.gate.cohort(),{oldInstanceIds:old,newInstanceIds:added});
});
test('restart requires both old withdrawals, observed absence, and the completed wait',t=>{
 t.mock.timers.enable({apis:['Date'],now:Date.now()});
 const x=expanded();assert.throws(()=>x.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 mutate(x,'db_proxy_nodes_deregister',{nodeIds:['old-a']},{instanceIds:['old-a'],outcome:'unknown'});
 observe(x,observations([...old,...added],{withdrawn:['old-a']}));assert.throws(()=>x.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_nodes_deregister',argsFor('db_proxy_nodes_deregister',{nodeIds:old})),/MutationAlreadySubmitted|MissingEvidence/);
 mutate(x,'db_proxy_nodes_deregister',{nodeIds:['old-b']},{instanceIds:['old-b'],outcome:'submitted'});
 observe(x,observations([...old,...added],{withdrawn:old,unprotected:['new-a']}));assert.throws(()=>x.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 observe(x,observations([...old,...added],{withdrawn:old}));assert.throws(()=>x.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 const decision=requestWithdrawalWait(x);assert.throws(()=>setup(x.session,decision),/MissingEvidence/);
 t.mock.timers.tick(60000);const y=setup(x.session,decision);
 assert.throws(()=>y.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 observe(y,observations([...old,...added],{withdrawn:old}));assert.doesNotThrow(()=>y.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')));
});
test('weight-zero old rows satisfy the completed drain without hiding unrelated backends',t=>{
 t.mock.timers.enable({apis:['Date'],now:Date.now()});
 const x=expanded();
 const observeDrained=(run,drained)=>{
  const e=observations([...old,...added],{withdrawn:drained});
  observe(run,e);
  return e;
 };
 mutate(x,'db_proxy_nodes_deregister',{nodeIds:['old-a']},{instanceIds:['old-a'],outcome:'submitted'});
 observeDrained(x,['old-a']);
 mutate(x,'db_proxy_nodes_deregister',{nodeIds:['old-b']},{instanceIds:['old-b'],outcome:'submitted'});
 observeDrained(x,old);
 const decision=requestWithdrawalWait(x);
 t.mock.timers.tick(60000);
 const y=setup(x.session,decision);
 const e=observeDrained(y,old);
 e.backend.nodes.push({instanceId:'foreign',registered:false,healthy:false,status:'Available'});
 observe(y,e);
 assert.throws(()=>y.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 const absent=observations([...old,...added],{withdrawn:old});
 absent.backend.nodes=absent.backend.nodes.filter(n=>n.registered);
 observe(y,absent);
 assert.throws(()=>y.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 observeDrained(y,old);
 assert.doesNotThrow(()=>y.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')));
});
for(const [name,change] of [
 ['over budget',e=>{e.metrics.nodes[0].pools[0].idle=2;e.metrics.nodes[0].connectedClients=2;}],
 ['missing triplet',e=>delete e.metrics.nodes[0].pools[0].waiting],
 ['missing old metrics',e=>e.metrics.nodes.shift()],
 ['duplicate pool',e=>e.metrics.nodes[0].pools.push({...e.metrics.nodes[0].pools[0]})],
 ['stale source',e=>e.metrics.sourceAt=new Date(Date.now()-16000).toISOString()],
 ['wrong UID',e=>e.deployment.uid='foreign'],
 ['older generation',e=>e.deployment.observedGeneration=5],
 ['unready rollout',e=>e.deployment.updatedReplicas=1],
 ['old still registered',e=>e.backend.nodes.push({instanceId:'old-a',registered:true,healthy:true,status:'Available'})],
 ['replacement cohort changed',e=>e.fleet.instances[2].instanceId='other-new'],
 ['replacement only has another pool client',e=>{const n=e.metrics.nodes.find(n=>n.instanceId===added[0]);n.pools[0].user='other-client';}],
 ['replacement has no active or idle client',e=>{const n=e.metrics.nodes.find(n=>n.instanceId===added[0]);Object.assign(n.pools[0],{active:0,idle:0,waiting:1});n.connectedClients=1;}],
])test('unprotect and scale reject '+name,()=>{
 const x=restarted(),e=observations([...old,...added],{withdrawn:old,zero:true,generation:6});change(e);observe(x,e);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_node_protection_set',protect(old,false)),/MissingEvidence/);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_scale',argsFor('db_proxy_scale',{actionId:'shrink',desiredCapacity:2})),/MissingEvidence/);
});
for(const stage of ['unprotect','approval','reduction'])for(const [name,counts,allowed] of [
 ['zero total',[[0,0,0],[0,0,0]],true],
 ['0+1 active',[[0,0,0],[1,0,0]],false],
 ['1+0 idle',[[0,1,0],[0,0,0]],false],
 ['0+1 waiting',[[0,0,0],[0,0,1]],false],
 ['1+1 active',[[1,0,0],[1,0,0]],false],
 ['idle plus waiting',[[0,1,0],[0,0,1]],false],
])test(`${stage} enforces combined old-node budget: ${name}`,()=>{
 const x=restarted();
 if(stage!=='unprotect')mutate(x,'db_proxy_node_protection_set',{nodeIds:old,protected:false},{instanceIds:old,outcome:'submitted'});
 const e=observations([...old,...added],{withdrawn:old,unprotected:stage==='unprotect'?[]:old,zero:true,generation:6});
 for(const [i,[active,idle,waiting]] of counts.entries()){
  Object.assign(e.metrics.nodes[i].pools[0],{active,idle,waiting});e.metrics.nodes[i].connectedClients=active+idle+waiting;
 }
 // Retained protected-node clients are outside the retiring-node budget.
 for(const n of e.metrics.nodes.filter(n=>added.includes(n.instanceId))){n.pools[0].active=3;n.pools[0].idle=0;n.connectedClients=3;}
 observe(x,e);
 const tool=stage==='unprotect'?'db_proxy_node_protection_set':stage==='approval'?'approval_request':'db_proxy_scale';
 const args=stage==='unprotect'?protect(old,false):stage==='approval'?{requestId:'request',actionId:'shrink',interface:'db-proxy.scale-in',envCode:'rdev.ali',target:{proxyCode:'proxy'},parameters:{desiredCapacity:2}}:argsFor('db_proxy_scale',{actionId:'shrink',desiredCapacity:2});
 if(allowed)assert.doesNotThrow(()=>x.gate.beforeMutation(tool,args));
 else assert.throws(()=>x.gate.beforeMutation(tool,args),/MissingEvidence/);
});
for(const [name,change] of [
 ['extra pool/user exceeds combined budget',e=>{const n=e.metrics.nodes[1];n.pools[0].active=1;n.pools.push({instanceId:n.instanceId,pool:'other-db',user:'other-client',active:0,idle:0,waiting:1});n.connectedClients=2;}],
 ['missing pool',e=>e.metrics.nodes[1].pools=[]],
 ['missing idle triplet value',e=>delete e.metrics.nodes[1].pools[0].idle],
 ['missing active triplet value',e=>delete e.metrics.nodes[1].pools[0].active],
 ['malformed count',e=>e.metrics.nodes[1].pools[0].waiting=0.5],
 ['node total mismatch',e=>e.metrics.nodes[1].connectedClients=1],
 ['pending node metrics',e=>e.metrics.pendingInstanceIds=[old[1]]],
 ['stale response',e=>e.metrics.responseAt=new Date(Date.now()-31000).toISOString()],
 ['source before restart',(e,x)=>e.metrics.sourceAt=new Date(x.gate.restart().at-1).toISOString()],
 ['response before restart',(e,x)=>e.metrics.responseAt=new Date(x.gate.restart().at-1).toISOString()],
])test(`direct budget refuses ${name}`,()=>{
 const x=restarted(),e=observations([...old,...added],{withdrawn:old,zero:true,generation:6});change(e,x);observe(x,e);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_node_protection_set',protect(old,false)),/MissingEvidence/);
});
test('zero before restart is not rollout evidence and unprotecting replacements is forbidden',()=>{
 const x=withdrawn();observe(x,observations([...old,...added],{withdrawn:old,zero:true}));assert.throws(()=>x.gate.beforeMutation('db_proxy_node_protection_set',protect(old,false)),/MissingEvidence/);
 const y=restarted();assert.throws(()=>y.gate.beforeMutation('db_proxy_node_protection_set',protect(added,false)),/MissingEvidence/);
});
test('resume retains ordering and generation but requires fresh current reads; one exact reduction only',()=>{
 const x=restarted();mutate(x,'db_proxy_node_protection_set',{nodeIds:old,protected:false},{instanceIds:old,outcome:'submitted'});
 const y=setup(x.session),scale=argsFor('db_proxy_scale',{actionId:'shrink',desiredCapacity:2});
 assert.throws(()=>y.gate.beforeMutation('db_proxy_scale',scale),/MissingEvidence/);
 const e=observations([...old,...added],{withdrawn:old,unprotected:old,zero:true,generation:6});
 observe(y,e);
 mutate(y,'db_proxy_scale',{actionId:'shrink',desiredCapacity:2},{previousDesiredCapacity:4,desiredCapacity:2,outcome:'unknown'});
 assert.throws(()=>y.gate.beforeMutation('db_proxy_scale',{...scale,actionId:'renamed'}),/MutationAlreadySubmitted|MissingEvidence/);
 observe(y,observations(added,{zero:true,generation:6}));
 assert.deepEqual(y.gate.completionEvidence(),{oldInstanceIds:old,newInstanceIds:added,restartGeneration:6});
});
test('unresolved restart never replays or permits scale, even when app looks ready',()=>{
 const x=waited(),args=argsFor('deployment_restart');x.gate.beforeMutation('deployment_restart',args);x.trace.beforeMutation('deployment_restart',args);
 const y=setup(x.session);observe(y,observations([...old,...added],{withdrawn:old,zero:true,generation:6}));
 assert.throws(()=>y.gate.beforeMutation('deployment_restart',args),/MutationAlreadySubmitted|MissingEvidence/);
 assert.throws(()=>y.gate.beforeMutation('db_proxy_node_protection_set',protect(old,false)),/MissingEvidence/);
});
test('resume rejects altered or orphaned ordering entries',()=>{
 for(const change of [s=>s.entries.find(e=>e.customType==='raptor-replacement-sequence-v2').data.identity.requestId='foreign',s=>s.entries=s.entries.filter(e=>e.customType!=='raptor-replacement-sequence-v2'),s=>s.entries.push(structuredClone(s.entries.find(e=>e.customType==='raptor-replacement-sequence-v2')))]){
 const x=restarted();change(x.session);assert.throws(()=>setup(x.session),/InvalidEvidence/);
 }
});

test('readyFour allows protection of the as-yet unprotected exact new pair',()=>{
 const x=expanded();observe(x,observations([...old,...added],{unprotected:added}));
 assert.doesNotThrow(()=>x.gate.beforeMutation('db_proxy_node_protection_set',protect(added)));
});
test('early and desired-three approvals fail before creating a pending approval',()=>{
 const x=expanded();
 for(const desiredCapacity of [2,3])assert.throws(()=>x.gate.beforeMutation('approval_request',{requestId:'request',actionId:'shrink',interface:'db-proxy.scale-in',envCode:'rdev.ali',target:{proxyCode:'proxy'},parameters:{desiredCapacity}}),/MissingEvidence/);
});
test('initial SQL and control reads cannot be replaced by only healthy cloud observations',()=>{
 const x=setup();for(const [kind,data] of Object.entries(observations()))x.gate.observe(kind,data);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_scale',argsFor('db_proxy_scale',{actionId:'expand',desiredCapacity:4})),/MissingEvidence/);
});

test('a resumed grant requires request, environment and object reads even when valid reads are older',()=>{
 const {session}=setup();
 const grantBinding={...binding,approvalProtocol:'request-grants-v1'};
 const initialTrace=new ReplacementTrace(session,grantBinding,scope);
 initialTrace.observe('fleet',observations().fleet);
 const trace=new ReplacementTrace(session,grantBinding,scope);
 const gate=new ReplacementSequence(session,trace,new ReplacementReadiness(session,trace),'resource-v1');
 for(const [kind,data] of Object.entries(observations()))gate.observe(kind,data);
 for(const instanceId of old)gate.observe('sql',{instanceId,connected:true},{instanceId});
 const at=new Date().toISOString();
 gate.observe('mcp',[
  {server:'raptor',tool:'request_get',observedAt:at},
  {server:'raptor',tool:'deployments_list',observedAt:at},
  {server:'infra',tool:'deployments_list',observedAt:at},
  {server:'infra',tool:'deployment_status_get',observedAt:at},
 ]);
 gate.observeContinuity({phase:'hold'},{
  requestId:binding.requestId,scopeDigest:scope.resolutionDigest,observationId:'held-originals',
  observedAt:at,sockets:Object.fromEntries(old.map(id=>[id,{id,lastSqlAt:at}]))
 });
 const args=argsFor('db_proxy_scale',{actionId:'expand',desiredCapacity:4});
 assert.throws(()=>gate.beforeMutation('db_proxy_scale',args),/MissingEvidence/);
 observe({gate});
 gate.observe('mcp',[{server:'raptor',tool:'request_get',observedAt:new Date(Date.now()-43000).toISOString()}]);
 assert.doesNotThrow(()=>gate.beforeMutation('db_proxy_scale',args));
});

test('granted protection and drain name expired replacement SQL after readiness was recorded',()=>{
 const {session}=setup();
 const trace=new ReplacementTrace(session,{...binding,approvalProtocol:'request-grants-v1'},scope);
 const readiness=new ReplacementReadiness(session,trace);
 const gate=new ReplacementSequence(session,trace,readiness,'resource-v1');
 const x={gate,trace};
 observe(x);
 const at=new Date().toISOString();
 gate.observeContinuity({phase:'hold'},{requestId:binding.requestId,scopeDigest:scope.resolutionDigest,observationId:'hold',
  observedAt:at,sockets:Object.fromEntries(old.map(id=>[id,{id,lastSqlAt:at}]))});
 mutate(x,'db_proxy_scale',{actionId:'expand',desiredCapacity:4},{actionId:'expand',scalingActivityId:'asa-test',previousDesiredCapacity:2,desiredCapacity:4,outcome:'submitted'});
 const refresh=()=>observe(x,observations([...old,...added]));
 const expire=()=>gate.observe('sql',{instanceId:added[0],connected:true,observedAt:new Date(Date.now()-43000).toISOString()},{instanceId:added[0]});
 refresh();
 assert.equal(readiness.ready(),true);
 expire();
 const protection={nodeIds:added,protected:true};
 const requestsSQL=e=>e.reads?.includes('db_connection_probe for the two replacement nodes')===true;
 assert.throws(()=>gate.beforeMutation('db_proxy_node_protection_set',argsFor('db_proxy_node_protection_set',protection)),requestsSQL);
 refresh();
 mutate(x,'db_proxy_node_protection_set',protection,{actionId:'protect',instanceIds:added,outcome:'submitted'});
 refresh();
 expire();
 assert.throws(()=>gate.beforeMutation('db_proxy_nodes_deregister',argsFor('db_proxy_nodes_deregister',{nodeIds:old})),requestsSQL);
});

test('historical readyFour cannot substitute for fresh complete metrics on resume',()=>{
 const x=expanded(),y=setup(x.session),e=observations([...old,...added]);
 for(const [kind,data] of Object.entries(e))if(kind!=='metrics')y.gate.observe(kind,data);
 assert.throws(()=>y.gate.beforeMutation('db_proxy_nodes_deregister',argsFor('db_proxy_nodes_deregister',{nodeIds:old})),/MissingEvidence/);
});
test('each protected sequence permit must follow all prior receipts on reopen',()=>{
 const x=restarted(),entries=x.session.entries,result=entries.find(e=>e.customType==='raptor-replacement-evidence-v1'&&e.data.kind==='result'&&e.data.tool==='db_proxy_nodes_deregister');
 entries.splice(entries.indexOf(result),1);entries.push(result);
 assert.throws(()=>setup(x.session),/InvalidEvidence/);
});

test('malformed SQL timestamp and impossible observedGeneration cannot qualify initial writes',()=>{
 for(const kind of ['sql','deployment']){
  const x=setup();observe(x);
  if(kind==='sql')x.gate.observe('sql',{instanceId:old[0],connected:true,observedAt:null},{instanceId:old[0]});
  else x.gate.observe('deployment',{...observations().deployment,observedGeneration:6});
  assert.throws(()=>x.gate.beforeMutation('db_proxy_scale',argsFor('db_proxy_scale',{actionId:'expand',desiredCapacity:4})),/MissingEvidence/);
 }
});

for(const stage of ['unprotect','approval','reduction'])for(const instanceId of added)for(const fault of ['missing series'])test(`resumed ${stage} rejects ${fault} on ${instanceId}`,()=>{
 const x=restarted();
 if(stage!=='unprotect')mutate(x,'db_proxy_node_protection_set',{nodeIds:old,protected:false},{instanceIds:old,outcome:'submitted'});
 const y=setup(x.session),e=observations([...old,...added],{withdrawn:old,unprotected:stage==='unprotect'?[]:old,zero:true,generation:6});
 const node=e.metrics.nodes.find(n=>n.instanceId===instanceId);
 if(fault==='missing series')e.metrics.nodes=e.metrics.nodes.filter(n=>n!==node);
 else {node.pools[0].active=0;node.pools[0].idle=0;node.pools[0].waiting=3;node.connectedClients=3;}
 for(const [kind,data] of Object.entries(e))y.gate.observe(kind,data);
 const tool=stage==='unprotect'?'db_proxy_node_protection_set':stage==='approval'?'approval_request':'db_proxy_scale';
 const args=stage==='unprotect'?protect(old,false):stage==='approval'?{requestId:'request',actionId:'shrink',interface:'db-proxy.scale-in',envCode:'rdev.ali',target:{proxyCode:'proxy'},parameters:{desiredCapacity:2}}:argsFor('db_proxy_scale',{actionId:'shrink',desiredCapacity:2});
 assert.throws(()=>y.gate.beforeMutation(tool,args),/MissingEvidence/);
});

for(const first of ['protectOld','expand'])test(`resume rejects first ${first} permit before its baseline`,()=>{
 const x=setup();observe(x);
 if(first==='protectOld')mutate(x,'db_proxy_node_protection_set',{nodeIds:old,protected:true},{instanceIds:old,outcome:'submitted'});
 else mutate(x,'db_proxy_scale',{actionId:'expand',desiredCapacity:4},{previousDesiredCapacity:2,desiredCapacity:4,outcome:'submitted'});
 assert.doesNotThrow(()=>setup(x.session),'the unmodified baseline-first history must reopen');
 const entries=x.session.entries,index=entries.findIndex(e=>e.customType==='raptor-replacement-sequence-v2');
 const [permit]=entries.splice(index,1);entries.unshift(permit);
 assert.throws(()=>setup(x.session),/InvalidEvidence/);
});

for(const [label,change] of [
 ['identity',r=>r.identity.model='other-model'],['cohort',r=>r.cohort.newInstanceIds[0]='other'],
 ['duration',r=>r.wait.wakeAfterSeconds=59],['binding',r=>r.wait.bindingDigest='f'.repeat(64)],
 ['registered original',r=>r.evidence.backend.nodes.find(n=>n.instanceId==='old-a').registered=true],
 ['unprotected replacement',r=>r.evidence.fleet.instances[2].protectedFromScaleIn=false],
 ['stale prewait read',r=>r.evidence.capacity.observedAt=new Date(Date.parse(r.wait.startedAt)-30001).toISOString()],
 ['unknown field',r=>r.approved=true],
])test('withdrawal wait rejects altered '+label,()=>{
 const x=waited();change(x.session.entries.find(e=>e.customType==='raptor-replacement-withdrawal-wait-v1').data);
 assert.throws(()=>setup(x.session),/InvalidEvidence/);
});
for(const decisionChange of [d=>d.actionId+='other',d=>d.approvalId='human-approval',d=>d.bindingDigest='f'.repeat(64),d=>delete d.decision])test('withdrawal continuation rejects wrong decision binding',t=>{
 t.mock.timers.enable({apis:['Date'],now:Date.now()});const x=withdrawn(),decision=requestWithdrawalWait(x);t.mock.timers.tick(60000);decisionChange(decision);
 assert.throws(()=>setup(x.session,decision),/MissingEvidence/);
});
test('withdrawal continuation rejects 59999ms and duplicate or reordered milestones',t=>{
 t.mock.timers.enable({apis:['Date'],now:Date.now()});const x=withdrawn(),decision=requestWithdrawalWait(x);t.mock.timers.tick(59999);assert.throws(()=>setup(x.session,decision),/MissingEvidence/);
 t.mock.timers.tick(1);const y=setup(x.session,decision);assert.ok(y.gate.withdrawalCompleted);
 const entry=x.session.entries.find(e=>e.customType==='raptor-replacement-withdrawal-wait-v1');x.session.entries.push(structuredClone(entry));assert.throws(()=>setup(x.session),/InvalidEvidence/);
});
for(const omitted of ['fleet','backend','capacity','metrics','deployment','discovery'])test('restart after withdrawal wait requires fresh '+omitted,()=>{
 const x=waited(),y=setup(x.session),e=observations([...old,...added],{withdrawn:old});
 for(const [kind,data] of Object.entries(e))if(kind!==omitted)y.gate.observe(kind,data);
 if(omitted!=='discovery')y.gate.observe('mcp',['raptor.deployments_list','infra.deployments_list','infra.deployment_status_get'].map(name=>({server:name.split('.')[0],tool:name.split('.')[1],observedAt:new Date().toISOString()})));
 assert.throws(()=>y.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
});

function grantedWithdrawal(session=setup().session,decision){
 const trace=new ReplacementTrace(session,{...binding,approvalProtocol:'request-grants-v1'},scope);
 const gate=new ReplacementSequence(session,trace,new ReplacementReadiness(session,trace),'resource-v1',decision);
 const run={session,trace,gate};
 if(trace.baseline())return run;
 observe(run);
 const at=new Date().toISOString();
 gate.observeContinuity({phase:'hold'},{requestId:binding.requestId,scopeDigest:scope.resolutionDigest,observationId:'hold',
  observedAt:at,sockets:Object.fromEntries(old.map(id=>[id,{id,lastSqlAt:at}]))});
 mutate(run,'db_proxy_scale',{actionId:'expand',desiredCapacity:4},{actionId:'expand',scalingActivityId:'asa-test',previousDesiredCapacity:2,desiredCapacity:4,outcome:'submitted'});
 observe(run,observations([...old,...added]));
 mutate(run,'db_proxy_nodes_deregister',{nodeIds:old},{actionId:'drain',instanceIds:old,outcome:'submitted'});
 return run;
}
const verifyDrain={phase:'verify-drain',observationId:'hold',drainActionId:'drain'};
const releaseDrain={...verifyDrain,phase:'release-probes'};
function completedGrantDrain(t){
 t.mock.timers.enable({apis:['Date'],now:Date.now()});
 const initial=grantedWithdrawal();
 observe(initial,observations([...old,...added],{withdrawn:old}));
 const decision=requestWithdrawalWait(initial);
 t.mock.timers.tick(60000);
 return grantedWithdrawal(initial.session,decision);
}
function drainProof(){
 const at=new Date().toISOString();
 return {requestId:binding.requestId,scopeDigest:scope.resolutionDigest,observationId:'hold',drainActionId:'drain',
  observedAt:at,drainObservedAt:at,sockets:Object.fromEntries(old.map(id=>[id,{id,openedAt:new Date(Date.now()-61000).toISOString(),lastSqlAt:at,rounds:2}])),
  freshConnections:Object.fromEntries(added.map(id=>[id,12]))};
}
test('only observed post-drain routing qualifies a resource wait and socket verification',()=>{
 const {gate}=grantedWithdrawal();
 assert.throws(()=>gate.observeContinuity(verifyDrain,drainProof()),/InvalidEvidence/);
 assert.deepEqual(gate.withdrawalWaitReads(),['cloud_read fleet','cloud_read backend','cloud_read capacity']);
 assert.ok(gate.continuityReads(verifyDrain).includes('cloud_read fleet, backend and capacity after drain, then resource_wait'));
 assert.ok(gate.continuityReads(releaseDrain).length);
 observe({gate},observations([...old,...added],{withdrawn:old}));
 assert.deepEqual(gate.withdrawalWaitReads(),[]);
 assert.ok(gate.continuityReads(verifyDrain).length);
 const receipt=gate.trace.rows.find(row=>row.kind==='result'&&row.tool==='db_proxy_nodes_deregister');
 receipt.state.outcome='unknown';
 assert.throws(()=>gate.withdrawalWaitReads(),/InvalidEvidence/);
});
test('checkpoint reuses completed drain proof for the same request after a later wait',t=>{
 const run=completedGrantDrain(t);
 const proof=drainProof();
 run.gate.observeContinuity(verifyDrain,proof);
 const resumed=grantedWithdrawal(run.session);
 assert.throws(()=>resumed.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 observe(resumed,observations([...old,...added],{withdrawn:old}));
 assert.doesNotThrow(()=>resumed.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')));
 assert.deepEqual(run.gate.continuityReads(verifyDrain),[]);
 assert.deepEqual(run.gate.continuityReads(releaseDrain),[]);
 assert.deepEqual(resumed.gate.savedDrainProof(verifyDrain),proof);
 assert.throws(()=>resumed.gate.savedDrainProof({...verifyDrain,observationId:'foreign'}),/InvalidEvidence/);
});
test('a completed drain provides the next unfinished step after a model stop and a later wait',t=>{
 const run=completedGrantDrain(t),proof=drainProof();
 assert.equal(run.gate.continuationStep(true),undefined);
 run.gate.observeContinuity(verifyDrain,proof);
 const {gate}=grantedWithdrawal(run.session);
 assert.match(gate.continuationStep(false).instruction,/request_get.*request_grant_get/);
 assert.equal(gate.continuationStep(true).stage,'release');
 assert.match(gate.continuationStep(true).instruction,/"phase":"release-probes","observationId":"hold","drainActionId":"drain"/);
 const {freshConnections,drainObservedAt,...hold}=proof;
 const release={...hold,releasedAt:proof.observedAt};
 assert.throws(()=>gate.observeContinuity(releaseDrain,{...release,observationId:'foreign'}),/InvalidEvidence/);
 gate.observeContinuity(releaseDrain,release);
 gate.observeContinuity(releaseDrain,release);
 const resumed=grantedWithdrawal(run.session).gate;
 assert.equal(resumed.continuationStep(true).stage,'restart');
 resumed.trace.unresolved=()=>true;
 assert.match(resumed.continuationStep(true).instruction,/request_grant_actions_list/);
 resumed.trace.unresolved=()=>false;
 resumed.restart=()=>({generation:6,actionId:'restart-accepted'});
 assert.match(resumed.continuationStep(true).instruction,/verify-traffic.*restart-accepted/);
 resumed.trafficProof={noErrors:true};
 assert.match(resumed.continuationStep(true).instruction,/zero.*scale/i);
 const savedIntents=resumed.intents.bind(resumed);
 resumed.intents=stage=>stage==='shrink'?[{}]:savedIntents(stage);
 assert.match(resumed.continuationStep(true).instruction,/verify-final-traffic/);
 resumed.finalTraffic={noErrors:true};
 resumed.completionEvidence=()=>({});
 assert.equal(resumed.continuationStep(true),undefined);
});
test('only a submitted scale-in with accepted traffic unlocks the retirement read phase',()=>{
 const {gate}=grantedWithdrawal();
 const receipt={actionId:'shrink',previousDesiredCapacity:4,desiredCapacity:2,outcome:'submitted',scalingActivityId:'asa-example'};
 gate.trace.scaleActions=()=>[receipt];
 assert.equal(gate.retirementReceipt(),undefined);
 gate.trafficProof={noErrors:true};
 assert.equal(gate.retirementReceipt(),receipt);
 gate.trace.scaleActions=()=>[{...receipt,outcome:'unknown'}];
 assert.equal(gate.retirementReceipt(),undefined);
 gate.trace.scaleActions=()=>[receipt];
 gate.trace.unresolved=()=>true;
 assert.equal(gate.retirementReceipt(),undefined);
});
test('drain proof rejects the wrong route count and a checkpoint entry ahead of its completed wait',t=>{
 const run=completedGrantDrain(t);
 const proof=drainProof();
 const tooEarly=new Date(Date.now()-1).toISOString();
 assert.throws(()=>run.gate.observeContinuity(verifyDrain,{...proof,drainObservedAt:tooEarly}),/InvalidEvidence/);
 assert.throws(()=>run.gate.observeContinuity(verifyDrain,{...proof,freshConnections:{[added[0]]:24,[added[1]]:0}}),/InvalidEvidence/);
 run.gate.observeContinuity(verifyDrain,proof);
 const entries=run.session.entries;
 entries.unshift(entries.pop());
 assert.throws(()=>grantedWithdrawal(run.session),/InvalidEvidence/);
});
