// Capability selection is a client-side boundary; provider/server authorization
// remains mandatory. Scope is resolved by trusted preflight, never model input.
const readRaptor=['request_get','environment_get','object_get'];
const readInfra=['cloud_identity_get','deployments_list','deployment_status_get'];
const schemas={
 raptor:{request_get:{requestId:'string'},environment_get:{requestId:'string',envCode:'string'},object_get:{requestId:'string',kind:'string',code:'string'},approval_request:{requestId:'string',actionId:'string',interface:'string',envCode:'string',target:'object',parameters:'object'},approval_get:{requestId:'string',approvalId:'string'},request_pause:{requestId:'string',reason:'string'}},
 infra:{cloud_identity_get:{envCode:'string'},deployments_list:{envCode:'string',kind:'string',code:'string'},deployment_status_get:{envCode:'string',appCode:'string',clusterId:'string',namespace:'string',name:'string',uid:'string'},deployment_restart:{requestId:'string',envCode:'string',appCode:'string',clusterId:'string',namespace:'string',name:'string',uid:'string'},db_proxy_scale:{requestId:'string',envCode:'string',proxyCode:'string',groupId:'string',actionId:'string',desiredCapacity:'integer'},db_proxy_node_protection_set:{requestId:'string',envCode:'string',proxyCode:'string',groupId:'string',instanceIds:'array',protected:'boolean'},db_proxy_nodes_deregister:{requestId:'string',envCode:'string',proxyCode:'string',groupId:'string',serverGroupId:'string',instanceIds:'array'}}
};
export function operationToolSchema(server,tool){
 const fields=schemas[server]?.[tool];if(!fields)throw Error('UnexpectedToolSchema');
 return {type:'object',properties:Object.fromEntries(Object.entries(fields).map(([name,type])=>[name,{type,...(type==='array'?{items:{type:'string'}}:{})}])),required:Object.keys(fields),additionalProperties:false};
}
export function validateOperationToolSchema(server,tool,schema){
 const fields=schemas[server]?.[tool],keys=Object.keys(fields??{}).sort().join(',');
 if(!fields||schema?.type!=='object'||schema.additionalProperties!==false||!Array.isArray(schema.required)||[...schema.required].sort().join(',')!==keys||Object.keys(schema.properties??{}).sort().join(',')!==keys||Object.entries(fields).some(([k,type])=>schema.properties[k]?.type!==type||(type==='array'&&schema.properties[k].items?.type!=='string')))throw Error('UnexpectedToolSchema');
}
const text=v=>typeof v==='string'&&v.length>0&&v.length<=256&&v.trim()===v;
function replacementScope(b,s){
 if(!s||s.envCode!==b.envCode||s.proxyCode!==b.objectCode||!['envCode','proxyCode','groupId','serverGroupId','targetDbCode'].every(k=>text(s[k]))||!s.application||s.application.envCode!==b.envCode||(b.clusterId&&s.application.clusterId!==b.clusterId)||!['appCode','clusterId','namespace','name','uid'].every(k=>text(s.application[k])))throw Error('InvalidReplacementScope');
 return s;
}
export function operationCapabilities(b,scope){
 if(b?.operation==='application.question'&&b.objectKind==='application')return {raptor:[...readRaptor],infra:[...readInfra]};
 if(b?.operation==='db-proxy.replace-nodes'&&b.objectKind==='db-proxy'){
  replacementScope(b,scope);
  return {raptor:[...readRaptor,'approval_request','approval_get','request_pause'],infra:[...readInfra,'db_proxy_scale','db_proxy_node_protection_set','db_proxy_nodes_deregister','deployment_restart']};
 }
 throw Error('UnsupportedOperation');
}
function exact(args,required,extra=[]){
 if(!args||typeof args!=='object'||Array.isArray(args)||Object.keys(args).sort().join(',')!==[...Object.keys(required),...extra].sort().join(',')||Object.entries(required).some(([k,v])=>args[k]!==v))throw Error('ScopeMismatch');
}
const capacity=v=>Number.isSafeInteger(v)&&v>=0;
function nodes(v){if(!Array.isArray(v)||!v.length||v.length>50||v.some(x=>!text(x))||new Set(v).size!==v.length)throw Error('ScopeMismatch');}
export function checkOperationSelectors(server,tool,args,b,scope){
 const profile=operationCapabilities(b,scope);
 if(!profile[server]?.includes(tool))throw Error('ScopeMismatch');
 if(server==='raptor'){
  switch(tool){
  case 'request_get':exact(args,{requestId:b.requestId});break;
  case 'environment_get':exact(args,{requestId:b.requestId,envCode:b.envCode});break;
  case 'object_get':exact(args,{requestId:b.requestId,kind:b.objectKind,code:b.objectCode});break;
  case 'approval_get':exact(args,{requestId:b.requestId},['approvalId']);if(!text(args.approvalId))throw Error('ScopeMismatch');break;
  case 'request_pause':exact(args,{requestId:b.requestId},['reason']);if(!['waiting_approval','waiting_review'].includes(args.reason))throw Error('ScopeMismatch');break;
  case 'approval_request':
   exact(args,{requestId:b.requestId,envCode:b.envCode,interface:'ess.scale-in'},['actionId','target','parameters']);
   exact(args.target,{proxyCode:scope.proxyCode,groupId:scope.groupId});
   exact(args.parameters,{},['desiredCapacity']);
   if(!text(args.actionId)||!capacity(args.parameters.desiredCapacity))throw Error('ScopeMismatch');break;
  }
  return;
 }
 const application=b.operation==='application.question'?{appCode:b.objectCode,clusterId:b.clusterId}:scope.application;
 switch(tool){
 case 'cloud_identity_get':exact(args,{envCode:b.envCode});break;
 case 'deployments_list':exact(args,{envCode:b.envCode,kind:b.objectKind,code:b.objectCode});break;
 case 'deployment_status_get':
  if(b.operation==='application.question'){
   exact(args,{envCode:b.envCode,appCode:b.objectCode,clusterId:b.clusterId},['namespace','name','uid']);
   if(!['namespace','name','uid'].every(k=>text(args[k])))throw Error('ScopeMismatch');
  }else exact(args,{envCode:b.envCode,...application});break;
 case 'deployment_restart':exact(args,{requestId:b.requestId,envCode:b.envCode,...application});break;
 default:{
  const target={requestId:b.requestId,envCode:b.envCode,proxyCode:scope.proxyCode,groupId:scope.groupId};
  if(tool==='db_proxy_scale'){
   exact(args,target,['actionId','desiredCapacity']);if(!text(args.actionId)||!capacity(args.desiredCapacity))throw Error('ScopeMismatch');
  }else if(tool==='db_proxy_node_protection_set'){
   exact(args,target,['instanceIds','protected']);nodes(args.instanceIds);if(typeof args.protected!=='boolean')throw Error('ScopeMismatch');
  }else if(tool==='db_proxy_nodes_deregister'){
   exact(args,{...target,serverGroupId:scope.serverGroupId},['instanceIds']);nodes(args.instanceIds);
  }else throw Error('ScopeMismatch');
 }
 }
}
