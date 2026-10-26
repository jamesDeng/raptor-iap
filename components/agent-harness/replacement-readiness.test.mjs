import test from 'node:test';
import assert from 'node:assert/strict';
import {ReplacementTrace} from './replacement-trace.mjs';
import {ReplacementReadiness} from './replacement-readiness.mjs';
const binding={requestId:'request',definitionSha256:'a'.repeat(64),skillsCommit:'b'.repeat(40),model:'gpt-6-luna'};
const scope={envCode:'dev',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',targetDbCode:'db',application:{uid:'app-uid'}};
const ids=['old-a','old-b','new-a','new-b'];
const manager=()=>({entries:[],getEntries(){return this.entries;},appendCustomEntry(customType,data){this.entries.push({type:'custom',customType,data:structuredClone(data)});}});
function evidence(stamp=new Date().toISOString()){
 return {
  fleet:{groupId:'group',observedAt:stamp,instances:ids.map(instanceId=>({instanceId,lifecycleState:'Protected',healthStatus:'Healthy'}))},
  backend:{serverGroupId:'backend',observedAt:stamp,nodes:ids.map(instanceId=>({instanceId,status:'Available',registered:true,healthy:true}))},
  capacity:{groupId:'group',observedAt:stamp,desiredCapacity:4,minSize:2,maxSize:4},
  metrics:{observedAt:stamp,responseAt:stamp,sourceAt:stamp,latestResponseAt:stamp,latestSourceAt:stamp,pendingInstanceIds:[],nodes:ids.map(instanceId=>({instanceId,connectedClients:3,pools:[{instanceId,pool:'db',user:'client',active:1,idle:2,waiting:0}]}))},
  probes:ids.slice(2).map(instanceId=>({instanceId,connected:true}))
 };
}
function setup(outcome='submitted'){
 const session=manager(),trace=new ReplacementTrace(session,binding,scope);
 trace.observe('fleet',{...evidence().fleet,instances:evidence().fleet.instances.slice(0,2)});
 const args={actionId:'expand',desiredCapacity:4};
 trace.beforeMutation('db_proxy_scale',args);
 if(outcome)trace.mutation('db_proxy_scale',args,{previousDesiredCapacity:2,desiredCapacity:4,outcome});
 return {session,trace,args,gate:new ReplacementReadiness(session,trace)};
}
function observe(gate,e=evidence()){
 for(const kind of ['fleet','backend','capacity','metrics'])gate.observe(kind,e[kind]);
 for(const probe of e.probes)gate.observe('sql',probe,{instanceId:probe.instanceId});
}
test('four-node readiness requires all observations and persists across a later reopen',()=>{
 const {session,trace,gate}=setup(),e=evidence();
 assert.equal(gate.blocked(),true);
 for(const kind of ['fleet','backend','capacity','metrics'])gate.observe(kind,e[kind]);
 gate.observe('sql',e.probes[0],{instanceId:'new-a'});assert.equal(gate.ready(),false);
 gate.observe('sql',e.probes[1],{instanceId:'new-b'});assert.equal(gate.ready(),true);
 assert.equal(gate.blocked(),false);
 const reopened=new ReplacementReadiness(session,new ReplacementTrace(session,binding,scope));
 assert.equal(reopened.ready(),true);assert.equal(session.entries.filter(e=>e.customType==='raptor-replacement-readiness-v1').length,1);
 observe(reopened);assert.equal(session.entries.filter(e=>e.customType==='raptor-replacement-readiness-v1').length,1);
 assert.throws(()=>trace.beforeMutation('db_proxy_scale',{actionId:'expand',desiredCapacity:4}),/MutationAlreadySubmitted/);
});
test('submitted or unknown scale locks immediately, including an unresolved submission',()=>{
 for(const outcome of ['submitted','unknown',null]){const {gate}=setup(outcome);assert.equal(gate.blocked(),true);}
 const {session,trace}=setup('unknown');const gate=new ReplacementReadiness(session,trace);observe(gate);assert.equal(gate.ready(),true);
 assert.throws(()=>trace.beforeMutation('db_proxy_scale',{actionId:'expand',desiredCapacity:4}),/MutationAlreadySubmitted/);
});
for(const [name,change] of [
 ['pending metric IDs',e=>e.metrics.pendingInstanceIds=['new-a']],
 ['pending fleet node',e=>e.fleet.instances[2].lifecycleState='Pending:Wait'],
 ['unhealthy fleet node',e=>e.fleet.instances[2].healthStatus='Unhealthy'],
 ['adding backend',e=>{e.backend.nodes[2].healthy=false;e.backend.nodes[2].status='Adding';}],
 ['unregistered backend',e=>e.backend.nodes[2].registered=false],
 ['foreign backend',e=>e.backend.nodes[2].instanceId='foreign'],
 ['duplicate backend',e=>e.backend.nodes[2].instanceId='old-a'],
 ['missing original',e=>e.fleet.instances[0].instanceId='foreign'],
 ['wrong fleet scope',e=>e.fleet.groupId='foreign'],
 ['wrong backend target',e=>e.backend.serverGroupId='foreign'],
 ['wrong capacity scope',e=>e.capacity.groupId='foreign'],
 ['wrong desired capacity',e=>e.capacity.desiredCapacity=3],
 ['missing metric node',e=>e.metrics.nodes.pop()],
 ['foreign metric node',e=>e.metrics.nodes[2].instanceId='foreign'],
 ['missing raw metric',e=>delete e.metrics.nodes[2].pools[0].waiting],
 ['negative raw metric',e=>e.metrics.nodes[2].pools[0].active=-1],
 ['mismatched pool instance',e=>e.metrics.nodes[2].pools[0].instanceId='foreign'],
 ['duplicate pool',e=>e.metrics.nodes[2].pools.push({...e.metrics.nodes[2].pools[0]})],
 ['inconsistent total',e=>e.metrics.nodes[2].connectedClients=999],
 ['pending SQL',e=>{e.probes[1].connected=false;e.probes[1].readiness='pending';}],
 ['pending successful SQL',e=>e.probes[1].readiness='pending'],
 ['old-node SQL',e=>e.probes[1].instanceId='old-a'],
 ['foreign SQL',e=>e.probes[1].instanceId='foreign'],
 ['stale cloud',e=>e.backend.observedAt=new Date(Date.now()-31000).toISOString()],
 ['missing metric source age',e=>delete e.metrics.sourceAt],
 ['stale metric source age',e=>e.metrics.sourceAt=new Date(Date.now()-31000).toISOString()],
 ['stale metric response age',e=>e.metrics.responseAt=new Date(Date.now()-31000).toISOString()],
 ['stale metrics',e=>e.metrics.observedAt=new Date(Date.now()-31000).toISOString()],
 ['malformed SQL timestamp',e=>e.probes[1].observedAt=null],
 ['stale SQL',e=>e.probes[1].observedAt=new Date(Date.now()-31000).toISOString()],
 ['unaligned times',e=>{e.fleet.observedAt=new Date(Date.now()-20000).toISOString();e.backend.observedAt=new Date(Date.now()+20000).toISOString();}],
])test('readiness rejects '+name,()=>{const {gate}=setup(),e=evidence();change(e);observe(gate,e);assert.equal(gate.ready(),false);assert.equal(gate.blocked(),true);});
test('SQL result must match requested instance and repeated probes do not replace the second node',()=>{
 const {gate}=setup(),e=evidence();for(const kind of ['fleet','backend','capacity','metrics'])gate.observe(kind,e[kind]);
 gate.observe('sql',e.probes[0],{instanceId:'new-a'});gate.observe('sql',e.probes[1],{instanceId:'new-a'});assert.equal(gate.ready(),false);
});
test('observations before expansion and pending replacements cannot reuse earlier evidence',()=>{
 const session=manager(),trace=new ReplacementTrace(session,binding,scope),gate=new ReplacementReadiness(session,trace);observe(gate);assert.equal(gate.ready(),false);
 trace.observe('fleet',{...evidence().fleet,instances:evidence().fleet.instances.slice(0,2)});assert.equal(gate.blocked(),false);
 trace.beforeMutation('db_proxy_scale',{actionId:'expand',desiredCapacity:4});assert.equal(gate.ready(),false);assert.equal(gate.blocked(),true);
});
test('reopen rejects duplicate, malformed, foreign, stale-at-creation and orphaned witnesses',()=>{
 for(const corrupt of [
  (s,r)=>s.entries.push(structuredClone(r)),
  (_s,r)=>r.data.identity.requestId='foreign',
  (_s,r)=>r.data.identity.skillsCommit='c'.repeat(40),
  (_s,r)=>r.data.identity.model='other',
  (_s,r)=>r.data.identity.scope.targetDbCode='foreign',
  (_s,r)=>r.data.expansionKey='a'.repeat(64),
  (_s,r)=>r.data.evidence.metrics.nodes.pop(),
  (_s,r)=>r.data.evidence.metrics.sourceAt=new Date(Date.parse(r.data.observedAt)-31000).toISOString(),
  (_s,r)=>r.data.evidence.metrics.responseAt=new Date(Date.parse(r.data.observedAt)-31000).toISOString(),
  (_s,r)=>r.data.evidence.fleet.instances={},
  (_s,r)=>r.data.identity.definitionSha256='c'.repeat(64),
  (_s,r)=>r.data.evidence.fleet.observedAt=new Date(Date.parse(r.data.observedAt)-31000).toISOString(),
  (_s,r)=>r.data.kind='other',
  (_s,r)=>r.data.evidence=null,
  (s)=>s.entries=s.entries.filter(e=>e.data.kind!=='intent'&&e.data.kind!=='result'),
 ]){
  const {session,gate}=setup();observe(gate);const row=session.entries.find(e=>e.customType==='raptor-replacement-readiness-v1');corrupt(session,row);
  assert.throws(()=>new ReplacementReadiness(session,new ReplacementTrace(session,binding,scope)),/InvalidEvidence/);
 }
});
test('persisted witness remains valid after resource or approval waiting expires its observation window',t=>{
 t.mock.timers.enable({apis:['Date'],now:Date.now()});
 const {session,gate}=setup();observe(gate);assert.equal(gate.ready(),true);
 t.mock.timers.tick(60000);
 assert.equal(new ReplacementReadiness(session,new ReplacementTrace(session,binding,scope)).ready(),true);
});
test('unknown scale with ambiguous state or no acknowledgement cannot establish readiness',()=>{
 for(const outcome of ['unknown',null]){
  const {gate}=setup(outcome),e=evidence();e.fleet.instances.pop();observe(gate,e);assert.equal(gate.ready(),false);assert.equal(gate.blocked(),true);
 }
 const {gate}=setup(null);observe(gate);assert.equal(gate.ready(),false);assert.equal(gate.blocked(),true);
});
test('readiness witness retains sanitized observation fields only',()=>{
 const {session,gate}=setup(),e=evidence();e.backend.nodes[2].privateIp='10.0.0.1';e.metrics.secret='do-not-store';e.probes[0].password='do-not-store';observe(gate,e);
 assert.equal(gate.ready(),true);
 const row=session.entries.find(e=>e.customType==='raptor-replacement-readiness-v1');
 assert.ok(!JSON.stringify(row).includes('10.0.0.1'));assert.ok(!JSON.stringify(row).includes('do-not-store'));
});

test('successful original-node probe does not displace the two exact new-node probes',()=>{
 const {session,gate}=setup();
 gate.observe('sql',{instanceId:'old-a',connected:true},{instanceId:'old-a'});
 observe(gate);assert.equal(gate.ready(),true);
 const row=session.entries.find(e=>e.customType==='raptor-replacement-readiness-v1');
 assert.deepEqual(row.data.evidence.probes.map(p=>p.instanceId).sort(),['new-a','new-b']);
});
test('source evidence ages at witness creation even when metric completion remains fresh',t=>{
 t.mock.timers.enable({apis:['Date'],now:Date.now()});
 const {gate}=setup(),e=evidence();
 t.mock.timers.tick(14000);e.metrics.observedAt=new Date().toISOString();
 t.mock.timers.tick(17000);
 for(const kind of ['fleet','backend','capacity'])e[kind].observedAt=new Date().toISOString();
 observe(gate,e);assert.equal(gate.ready(),false);assert.equal(gate.blocked(),true);
});

test('witness alignment includes the newest metric timestamps, not only oldest ages',t=>{
 t.mock.timers.enable({apis:['Date'],now:Date.now()});
 const {gate}=setup();t.mock.timers.tick(20000);const e=evidence();
 e.fleet.observedAt=new Date(Date.now()-20000).toISOString();
 e.metrics.latestResponseAt=new Date(Date.now()+15000).toISOString();
 e.metrics.latestSourceAt=e.metrics.latestResponseAt;
 observe(gate,e);assert.equal(gate.ready(),false);assert.equal(gate.blocked(),true);
});
