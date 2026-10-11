// A command acknowledgement is evidence of submission, never fleet health.
export function replacementEvidence(server,tool,data,b,scope,args){
 const now=()=>new Date().toISOString();
 const record=(state,identity={},observedAt=now(),mode=server==='infra'?'live':'catalog')=>[{server,tool,observedAt,evidenceMode:mode,identity:{requestId:b.requestId,envCode:b.envCode,objectCode:b.objectCode,...identity},state}];
 const stamp=v=>{if(v?.evidenceMode!=='live'||typeof v.observedAt!=='string'||!Number.isFinite(Date.parse(v.observedAt)))throw Error('InvalidEvidence');return v.observedAt;};
 if(server==='raptor'){
  if(tool==='request_get'){
   const d=data?.definition;
   if(data?.requestId!==b.requestId||d?.envCode!==b.envCode||d?.object?.kind!==b.objectKind||d?.object?.code!==b.objectCode||d?.operations?.length!==1||d.operations[0].name!==b.operation)throw Error('ScopeMismatch');
  }
  if(tool==='approval_request'||tool==='approval_get'){
   if(data?.requestId!==b.requestId||typeof data.approvalId!=='string'||!data.approvalId||(tool==='approval_get'&&data.approvalId!==args.approvalId))throw Error('IdentityChanged');
   return record({approvalId:data.approvalId,state:String(data.state??'unknown').slice(0,64)});
  }
  return record({available:true});
 }
 if(tool==='cloud_identity_get'){
  if(data?.envCode!==b.envCode||data.accountMatches!==true)throw Error('ScopeMismatch');
  return record({accountMatches:true},{},stamp(data));
 }
 if(tool==='deployments_list'){
  if(!Array.isArray(data)||data.length!==1)throw Error('InvalidEvidence');
  const v=data[0];
  if(v.resourceId!==scope.groupId||v.kind!=='db-proxy'||v.objectCode!==b.objectCode||v.envCode!==b.envCode||v['target-db-code']!==scope.targetDbCode)throw Error('IdentityChanged');
  return record({state:String(v.state??'unknown').slice(0,256),targetDbCode:v['target-db-code']},{resourceId:v.resourceId,kind:v.kind},stamp(v));
 }
 if(tool==='deployment_status_get'){
  const a=scope.application;
  if(data?.envCode!==b.envCode||(data.objectCode??data.appCode)!==a.appCode||['clusterId','namespace','name','uid'].some(k=>data[k]!==a[k]))throw Error('IdentityChanged');
  const state={};for(const k of ['generation','observedGeneration','replicas','updatedReplicas','readyReplicas']){if(!Number.isSafeInteger(data[k])||data[k]<0)throw Error('InvalidEvidence');state[k]=data[k];}
  return record(state,{objectCode:a.appCode,clusterId:a.clusterId,namespace:a.namespace,name:a.name,uid:a.uid},stamp(data));
 }
 if(data?.evidenceMode!=='live')throw Error('InvalidEvidence');
 if(tool==='db_proxy_scale'){
  if(data.groupId!==scope.groupId||data.desiredCapacity!==args.desiredCapacity||!Number.isSafeInteger(data.previousDesiredCapacity)||data.previousDesiredCapacity<0||!['submitted','unknown'].includes(data.outcome))throw Error('InvalidEvidence');
  return record({outcome:data.outcome,desiredCapacity:data.desiredCapacity,previousDesiredCapacity:data.previousDesiredCapacity},{resourceId:scope.groupId,kind:'db-proxy'});
 }
 if(tool==='db_proxy_node_protection_set'||tool==='db_proxy_nodes_deregister'){
  if(!['submitted','unknown'].includes(data.outcome)||!Array.isArray(data.instanceIds)||data.instanceIds.length!==args.instanceIds.length||[...data.instanceIds].sort().join(',')!==[...args.instanceIds].sort().join(','))throw Error('InvalidEvidence');
  return record({outcome:data.outcome,instanceIds:[...data.instanceIds]},{resourceId:scope.groupId,kind:'db-proxy'});
 }
 if(tool==='deployment_restart'){
  if(data.accepted!==true||!Number.isSafeInteger(data.generation)||data.generation<0||!data.target||Object.keys(args).some(k=>data.target[k]!==args[k]))throw Error('InvalidEvidence');
  return record({accepted:true,generation:data.generation},{objectCode:scope.application.appCode,clusterId:scope.application.clusterId,namespace:scope.application.namespace,name:scope.application.name,uid:scope.application.uid});
 }
 throw Error('InvalidEvidence');
}
