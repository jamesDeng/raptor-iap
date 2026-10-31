// A command acknowledgement is evidence of submission, never fleet health.
export function replacementEvidence(server,tool,data,b,scope,args,discovered=new Map()){
 const now=()=>new Date().toISOString();
 const record=(state,identity={},observedAt=now(),mode=server==='infra'?'live':'catalog')=>[{server,tool,observedAt,evidenceMode:mode,identity:{requestId:b.requestId,envCode:b.envCode,objectCode:b.objectCode,...identity},state}];
 if(tool==='deployments_list'||tool==='deployment_status_get')return workloadEvidence(server,tool,data,b,scope,args,discovered);
 if(server==='raptor'){
  if(tool==='request_get'){
   const d=data?.definition;
   if(data?.requestId!==b.requestId||d?.envCode!==b.envCode||d?.object?.kind!==b.objectKind||d?.object?.code!==b.objectCode||d?.operations?.length!==1||d.operations[0].name!==b.operation||(d.operations[0].parameters?.mode??'')!==(b.executionMode??''))throw Error('ScopeMismatch');
   const refs=data.relatedResources;const expected=new Set(['application:'+scope.application.appCode,'database:'+scope.targetDbCode]);
   if(!Array.isArray(refs)||refs.length!==2||refs.some(ref=>!ref||Object.keys(ref).sort().join(',')!=='code,kind'||!expected.delete(ref.kind+':'+ref.code))||expected.size)throw Error('IdentityChanged');
  }
  if(tool==='approval_request'||tool==='approval_get'){
   if(data?.requestId!==b.requestId||typeof data.approvalId!=='string'||!data.approvalId||(tool==='approval_get'&&data.approvalId!==args.approvalId))throw Error('IdentityChanged');
   return record({approvalId:data.approvalId,state:String(data.state??'unknown').slice(0,64)});
  }
  return record({available:true});
 }
 if(data?.evidenceMode!=='live')throw Error('InvalidEvidence');
 if(tool==='db_proxy_scale'){
  if(data.groupId!==scope.groupId||data.desiredCapacity!==args.desiredCapacity||!Number.isSafeInteger(data.previousDesiredCapacity)||data.previousDesiredCapacity<0||!['submitted','unknown'].includes(data.outcome))throw Error('InvalidEvidence');
  return record({outcome:data.outcome,desiredCapacity:data.desiredCapacity,previousDesiredCapacity:data.previousDesiredCapacity},{resourceId:scope.groupId,kind:'db-proxy'});
 }
 if(tool==='db_proxy_node_protection_set'||tool==='db_proxy_nodes_deregister'){
  if(!['submitted','unknown'].includes(data.outcome)||!Array.isArray(data.instanceIds)||data.instanceIds.length!==args.nodeIds.length||[...data.instanceIds].sort().join(',')!==[...args.nodeIds].sort().join(','))throw Error('InvalidEvidence');
  return record({outcome:data.outcome,instanceIds:[...data.instanceIds]},{resourceId:scope.groupId,kind:'db-proxy'});
 }
 if(tool==='deployment_restart'){
  if(data.accepted!==true||!Number.isSafeInteger(data.generation)||data.generation<0||!data.target||data.target.requestId!==args.requestId||['envCode','appCode','clusterId','namespace','name','uid'].some(k=>data.target[k]!==scope.application[k]))throw Error('InvalidEvidence');
  return record({accepted:true,generation:data.generation},{objectCode:scope.application.appCode,clusterId:scope.application.clusterId,namespace:scope.application.namespace,name:scope.application.name,uid:scope.application.uid});
 }
 throw Error('InvalidEvidence');
}

// Discovery read time records transport completion, never metric freshness.
// workloadKey stays opaque; private pins validate the actual returned identity.
export function workloadEvidence(server,tool,data,b,scope,args,discovered){
 const invalid=()=>Error('InvalidEvidence'),changed=()=>Error('IdentityChanged');
 const text=v=>typeof v==='string'&&v.length>0&&v.length<=256;
 const resource=(v,withEnv=true)=>v&&v.kind===args.kind&&v.code===args.code&&(!withEnv||v.envCode===b.envCode);
 const record=(identity,state,observedAt=new Date().toISOString(),evidenceMode='discovery')=>({server,tool,observedAt,evidenceMode,identity:{requestId:b.requestId,envCode:b.envCode,objectCode:args.code??args.appCode,...identity},state});
 const app=(w,code)=>{
  if(!w||!text(w.workloadKey)||['clusterId','namespace','name','uid'].some(k=>!text(w[k]))||(b.clusterId&&w.clusterId!==b.clusterId))throw changed();
  const id={appCode:code,clusterId:w.clusterId,namespace:w.namespace,name:w.name,uid:w.uid};
  if(scope&&(code!==scope.application.appCode||['clusterId','namespace','name','uid'].some(k=>id[k]!==scope.application[k])))throw changed();
  const old=discovered.get(w.workloadKey);if(old&&JSON.stringify(old)!==JSON.stringify(id))throw changed();
  return id;
 };
 if(tool==='deployments_list'){
  let rows;
  if(server==='raptor'){
   if(!resource(data?.resource,false)||data.envCode!==b.envCode||!Array.isArray(data.deployments)||Object.keys(data).sort().join(',')!=='deployments,envCode,resource')throw changed();
   rows=data.deployments.map(v=>{if(v.envCode!==b.envCode||v.kind!==args.kind||v.objectCode!==args.code)throw changed();return v;});
  }else{
   if(!resource(data?.resource))throw changed();
   const selected=args.kind==='application'?'applicationWorkloads':args.kind==='database'?'databaseWorkloads':'pgproxyWorkload';
   if(Object.keys(data).sort().join(',')!==['resource',selected].sort().join(','))throw invalid();
   rows=selected==='pgproxyWorkload'?[data[selected]]:data[selected];
  }
  if(!Array.isArray(rows)||rows.length>64||(scope&&rows.length!==1))throw invalid();
  return rows.map(w=>{
   if(args.kind==='application'){
    if(server==='infra'&&w.type!=='kubernetes')throw changed();const id=app(w,args.code);discovered.set(w.workloadKey,id);
    return record({...id,workloadKey:w.workloadKey},{state:String(w.state??'unknown').slice(0,256)});
   }
   if(args.kind==='db-proxy'){
    if(!scope||(server==='infra'?(w.type!=='ess'||w.code!==args.code||w.targetDbCode!==scope.targetDbCode):(w.resourceId!==''||w['target-db-code']!==scope.targetDbCode))||!text(w.workloadKey))throw changed();
    return record({kind:args.kind,workloadKey:w.workloadKey},{state:String(w.state??'unknown').slice(0,256),targetDbCode:scope.targetDbCode});
   }
   if(args.kind!=='database'||!scope||(server==='infra'?(w.type!=='rds'||w.code!==args.code):false)||w.resourceId!==scope.databaseResourceId)throw changed();
   return record({kind:args.kind,resourceId:w.resourceId},{state:String(w.state??'unknown').slice(0,256)});
  });
 }
 if(tool!=='deployment_status_get'||server!=='infra'||data?.resource?.envCode!==b.envCode||data.resource.kind!=='application'||data.resource.code!==args.appCode||Object.keys(data).sort().join(',')!=='applicationWorkload,resource,status')throw changed();
 const w=data.applicationWorkload,status=data.status,id=app(w,args.appCode),known=discovered.get(w.workloadKey);
 if(w.type!=='kubernetes'||!known||JSON.stringify(known)!==JSON.stringify(id)||(args.workloadKey!==undefined&&args.workloadKey!==w.workloadKey)||status?.envCode!==b.envCode||(status.objectCode??status.appCode)!==args.appCode||['clusterId','namespace','name','uid'].some(k=>status[k]!==id[k])||status.evidenceMode!=='live'||!Number.isFinite(Date.parse(status.observedAt)))throw changed();
 const state={};for(const key of ['generation','observedGeneration','replicas','updatedReplicas','readyReplicas']){if(!Number.isSafeInteger(status[key])||status[key]<0)throw invalid();state[key]=status[key];}
 return [record({...id,workloadKey:w.workloadKey},state,status.observedAt,'live')];
}
