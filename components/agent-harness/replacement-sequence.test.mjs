import test from 'node:test';
import assert from 'node:assert/strict';
import {ReplacementTrace} from './replacement-trace.mjs';
import {ReplacementReadiness} from './replacement-readiness.mjs';
const module=await import('./replacement-sequence.mjs').catch(()=>({}));
const {ReplacementSequence}=module;
import {binding,scope,old,added,setup,observations,observe,argsFor,mutate,expanded,withdrawn,restarted,protect} from './replacement-sequence-fixtures.mjs';
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
test('restart requires both old withdrawals and observed absence with protected healthy new pair',()=>{
 const x=expanded();assert.throws(()=>x.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 mutate(x,'db_proxy_nodes_deregister',{serverGroupId:'backend',instanceIds:['old-a']},{instanceIds:['old-a'],outcome:'unknown'});
 observe(x,observations([...old,...added],{withdrawn:['old-a']}));assert.throws(()=>x.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_nodes_deregister',argsFor('db_proxy_nodes_deregister',{serverGroupId:'backend',instanceIds:old})),/MutationAlreadySubmitted|MissingEvidence/);
 mutate(x,'db_proxy_nodes_deregister',{serverGroupId:'backend',instanceIds:['old-b']},{instanceIds:['old-b'],outcome:'submitted'});
 observe(x,observations([...old,...added],{withdrawn:old,unprotected:['new-a']}));assert.throws(()=>x.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')),/MissingEvidence/);
 observe(x,observations([...old,...added],{withdrawn:old}));assert.doesNotThrow(()=>x.gate.beforeMutation('deployment_restart',argsFor('deployment_restart')));
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
])test('unprotect and scale reject '+name,()=>{
 const x=restarted(),e=observations([...old,...added],{withdrawn:old,zero:true,generation:6});change(e);observe(x,e);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_node_protection_set',protect(old,false)),/MissingEvidence/);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_scale',argsFor('db_proxy_scale',{actionId:'shrink',desiredCapacity:2})),/MissingEvidence/);
});
for(const stage of ['unprotect','approval','reduction'])for(const [name,counts,allowed] of [
 ['zero total',[[0,0,0],[0,0,0]],true],
 ['0+1 active',[[0,0,0],[1,0,0]],true],
 ['1+0 idle',[[0,1,0],[0,0,0]],true],
 ['0+1 waiting',[[0,0,0],[0,0,1]],true],
 ['1+1 active',[[1,0,0],[1,0,0]],false],
 ['idle plus waiting',[[0,1,0],[0,0,1]],false],
])test(`${stage} enforces combined old-node budget: ${name}`,()=>{
 const x=restarted();
 if(stage!=='unprotect')mutate(x,'db_proxy_node_protection_set',{instanceIds:old,protected:false},{instanceIds:old,outcome:'submitted'});
 const e=observations([...old,...added],{withdrawn:old,unprotected:stage==='unprotect'?[]:old,zero:true,generation:6});
 for(const [i,[active,idle,waiting]] of counts.entries()){
  Object.assign(e.metrics.nodes[i].pools[0],{active,idle,waiting});e.metrics.nodes[i].connectedClients=active+idle+waiting;
 }
 // Retained protected-node clients are outside the retiring-node budget.
 for(const n of e.metrics.nodes.filter(n=>added.includes(n.instanceId))){n.pools[0].active=3;n.connectedClients=3;}
 observe(x,e);
 const tool=stage==='unprotect'?'db_proxy_node_protection_set':stage==='approval'?'approval_request':'db_proxy_scale';
 const args=stage==='unprotect'?protect(old,false):stage==='approval'?{requestId:'request',actionId:'shrink',interface:'ess.scale-in',envCode:'rdev.ali',target:{proxyCode:'proxy',groupId:'group'},parameters:{desiredCapacity:2}}:argsFor('db_proxy_scale',{actionId:'shrink',desiredCapacity:2});
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
 const x=restarted();mutate(x,'db_proxy_node_protection_set',{instanceIds:old,protected:false},{instanceIds:old,outcome:'submitted'});
 const y=setup(x.session),scale=argsFor('db_proxy_scale',{actionId:'shrink',desiredCapacity:2});
 assert.throws(()=>y.gate.beforeMutation('db_proxy_scale',scale),/MissingEvidence/);
 const e=observations([...old,...added],{withdrawn:old,unprotected:old,zero:true,generation:6});
 e.metrics.nodes[1].pools[0].waiting=1;e.metrics.nodes[1].connectedClients=1;
 observe(y,e);
 mutate(y,'db_proxy_scale',{actionId:'shrink',desiredCapacity:2},{previousDesiredCapacity:4,desiredCapacity:2,outcome:'unknown'});
 assert.throws(()=>y.gate.beforeMutation('db_proxy_scale',{...scale,actionId:'renamed'}),/MutationAlreadySubmitted|MissingEvidence/);
 observe(y,observations(added,{zero:true,generation:6}));
 assert.deepEqual(y.gate.completionEvidence(),{oldInstanceIds:old,newInstanceIds:added,restartGeneration:6});
});
test('unresolved restart never replays or permits scale, even when app looks ready',()=>{
 const x=withdrawn(),args=argsFor('deployment_restart');x.gate.beforeMutation('deployment_restart',args);x.trace.beforeMutation('deployment_restart',args);
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
 for(const desiredCapacity of [2,3])assert.throws(()=>x.gate.beforeMutation('approval_request',{requestId:'request',actionId:'shrink',interface:'ess.scale-in',envCode:'rdev.ali',target:{proxyCode:'proxy',groupId:'group'},parameters:{desiredCapacity}}),/MissingEvidence/);
});
test('initial SQL and control reads cannot be replaced by only healthy cloud observations',()=>{
 const x=setup();for(const [kind,data] of Object.entries(observations()))x.gate.observe(kind,data);
 assert.throws(()=>x.gate.beforeMutation('db_proxy_scale',argsFor('db_proxy_scale',{actionId:'expand',desiredCapacity:4})),/MissingEvidence/);
});

test('historical readyFour cannot substitute for fresh pool and new SQL readiness on resume',()=>{
 for(const omitted of ['metrics','new SQL']){
  const x=expanded(),y=setup(x.session),e=observations([...old,...added]);
  for(const [kind,data] of Object.entries(e))if(kind!==omitted)y.gate.observe(kind,data);
  if(omitted!=='new SQL')for(const instanceId of added)y.gate.observe('sql',{instanceId,connected:true},{instanceId});
  assert.throws(()=>y.gate.beforeMutation('db_proxy_nodes_deregister',argsFor('db_proxy_nodes_deregister',{serverGroupId:'backend',instanceIds:old})),/MissingEvidence/);
 }
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

for(const stage of ['unprotect','approval','reduction'])for(const instanceId of added)for(const fault of ['omitted','failed','expired'])test(`resumed ${stage} rejects ${fault} SQL probe for ${instanceId}`,t=>{
 t.mock.timers.enable({apis:['Date'],now:Date.now()});
 const x=restarted();
 if(stage!=='unprotect')mutate(x,'db_proxy_node_protection_set',{instanceIds:old,protected:false},{instanceIds:old,outcome:'submitted'});
 // Model a resource wait (unprotect/approval) or approval wait (reduction).
 t.mock.timers.tick(60000);
 const y=setup(x.session);
 const probe=id=>y.gate.observe('sql',{instanceId:id,connected:true},{instanceId:id});
 if(fault==='expired'){
  probe(instanceId);t.mock.timers.tick(31001);
 }else if(fault==='failed')y.gate.observe('sql',{instanceId,connected:false,readiness:'pending'},{instanceId});
 for(const id of added)if(id!==instanceId)probe(id);
 const e=observations([...old,...added],{withdrawn:old,unprotected:stage==='unprotect'?[]:old,zero:true,generation:6});
 for(const [kind,data] of Object.entries(e))y.gate.observe(kind,data);
 const tool=stage==='unprotect'?'db_proxy_node_protection_set':stage==='approval'?'approval_request':'db_proxy_scale';
 const args=stage==='unprotect'?protect(old,false):stage==='approval'?{requestId:'request',actionId:'shrink',interface:'ess.scale-in',envCode:'rdev.ali',target:{proxyCode:'proxy',groupId:'group'},parameters:{desiredCapacity:2}}:argsFor('db_proxy_scale',{actionId:'shrink',desiredCapacity:2});
 assert.throws(()=>y.gate.beforeMutation(tool,args),/MissingEvidence/);
 probe(instanceId);
 assert.doesNotThrow(()=>y.gate.beforeMutation(tool,args),'both fresh successful probes restore eligibility');
});

test('approval resume cannot reduce with an empty SQL probe map despite fresh zero and rollout evidence',()=>{
 const x=restarted();mutate(x,'db_proxy_node_protection_set',{instanceIds:old,protected:false},{instanceIds:old,outcome:'submitted'});
 const y=setup(x.session);
 for(const [kind,data] of Object.entries(observations([...old,...added],{withdrawn:old,unprotected:old,zero:true,generation:6})))y.gate.observe(kind,data);
 assert.equal(y.gate.probes.size,0);
 assert.throws(()=>y.gate.beforeMutation('db_proxy_scale',argsFor('db_proxy_scale',{actionId:'shrink',desiredCapacity:2})),/MissingEvidence/);
});

for(const first of ['protectOld','expand'])test(`resume rejects first ${first} permit before its baseline`,()=>{
 const x=setup();observe(x);
 if(first==='protectOld')mutate(x,'db_proxy_node_protection_set',{instanceIds:old,protected:true},{instanceIds:old,outcome:'submitted'});
 else mutate(x,'db_proxy_scale',{actionId:'expand',desiredCapacity:4},{previousDesiredCapacity:2,desiredCapacity:4,outcome:'submitted'});
 assert.doesNotThrow(()=>setup(x.session),'the unmodified baseline-first history must reopen');
 const entries=x.session.entries,index=entries.findIndex(e=>e.customType==='raptor-replacement-sequence-v2');
 const [permit]=entries.splice(index,1);entries.unshift(permit);
 assert.throws(()=>setup(x.session),/InvalidEvidence/);
});
