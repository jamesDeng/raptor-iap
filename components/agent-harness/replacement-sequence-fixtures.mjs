import assert from 'node:assert/strict';
import {ReplacementTrace} from './replacement-trace.mjs';
import {ReplacementReadiness} from './replacement-readiness.mjs';
import {ReplacementSequence} from './replacement-sequence.mjs';
const binding={requestId:'request',definitionSha256:'a'.repeat(64),skillsCommit:'b'.repeat(40),model:'gpt-5.6-luna'};
const scope={envCode:'rdev.ali',proxyCode:'proxy',groupId:'group',serverGroupId:'backend',targetDbCode:'db',application:{envCode:'rdev.ali',appCode:'client',clusterId:'cluster',namespace:'test',name:'client',uid:'uid'}};
const old=['old-a','old-b'],added=['new-a','new-b'];
const stamp=()=>new Date().toISOString();
function setup(session={entries:[],getEntries(){return this.entries;},appendCustomEntry(customType,data){this.entries.push({type:'custom',customType,data:structuredClone(data)});}}){
 const trace=new ReplacementTrace(session,binding,scope),readiness=new ReplacementReadiness(session,trace);
 return {session,trace,readiness,gate:new ReplacementSequence(session,trace,readiness,'direct-v2')};
}
function observations(ids=old,{withdrawn=[],unprotected=[],zero=false,generation=5}={}){
 const at=stamp();return {
 fleet:{groupId:'group',observedAt:at,instances:ids.map(instanceId=>({instanceId,protectedFromScaleIn:!unprotected.includes(instanceId),lifecycleState:unprotected.includes(instanceId)?'InService':'Protected',healthStatus:'Healthy'}))},
 backend:{serverGroupId:'backend',observedAt:at,nodes:ids.filter(id=>!withdrawn.includes(id)).map(instanceId=>({instanceId,registered:true,healthy:true,status:'Available'}))},
 capacity:{groupId:'group',desiredCapacity:ids.length,minSize:2,maxSize:4,observedAt:at},
 metrics:{observedAt:at,sourceAt:at,responseAt:at,latestSourceAt:at,latestResponseAt:at,pendingInstanceIds:[],nodes:ids.map(instanceId=>({instanceId,connectedClients:zero?0:3,pools:[{instanceId,pool:'db',user:'client',active:zero?0:1,idle:zero?0:2,waiting:0}]}))},
 deployment:{...scope.application,generation,observedGeneration:generation,replicas:2,readyReplicas:2,updatedReplicas:2,availableReplicas:2,observedAt:at},
 trafficFailures:{observedAt:at,samples:['a','b'].flatMap(instance=>['failure','timeout','ambiguous'].map(outcome=>({instance,outcome,value:0,timestamp:Date.now()/1000})))},
 connections:{observedAt:at,samples:['a','b'].map(instance=>({instance,value:10,timestamp:Date.now()/1000}))},
 };
}
function observe(x,e=observations()){
 for(const [kind,data] of Object.entries(e))x.gate.observe(kind,data);
 for(const instanceId of e.fleet.instances.map(n=>n.instanceId))x.gate.observe('sql',{instanceId,connected:true},{instanceId});
 x.gate.observe('mcp',['raptor.request_get','raptor.environment_get','raptor.object_get','infra.cloud_identity_get','infra.deployments_list','infra.deployment_status_get'].map(name=>({server:name.split('.')[0],tool:name.split('.')[1],observedAt:stamp()})));
}
const argsFor=(tool,extra={})=>({...tool==='deployment_restart'?{requestId:'request',...scope.application}:{requestId:'request',envCode:'rdev.ali',proxyCode:'proxy',groupId:'group'},...extra});
function mutate(x,tool,extra,receipt){const args=argsFor(tool,extra);x.gate.beforeMutation(tool,args);x.trace.beforeMutation(tool,args);x.trace.mutation(tool,args,receipt);x.gate.onMutationResult(tool,args,receipt);}
function expanded(){const x=setup();observe(x);mutate(x,'db_proxy_scale',{actionId:'expand',desiredCapacity:4},{previousDesiredCapacity:2,desiredCapacity:4,outcome:'submitted'});observe(x,observations([...old,...added]));assert.equal(x.readiness.ready(),true);return x;}
function withdrawn(){const x=expanded();mutate(x,'db_proxy_nodes_deregister',{serverGroupId:'backend',instanceIds:old},{instanceIds:old,outcome:'submitted'});observe(x,observations([...old,...added],{withdrawn:old}));return x;}
function restarted(){const x=withdrawn();mutate(x,'deployment_restart',{}, {accepted:true,generation:6});observe(x,observations([...old,...added],{withdrawn:old,zero:true,generation:6}));return x;}
const protect=(ids,value=true)=>argsFor('db_proxy_node_protection_set',{instanceIds:ids,protected:value});

export {binding,scope,old,added,setup,observations,observe,argsFor,mutate,expanded,withdrawn,restarted,protect};
