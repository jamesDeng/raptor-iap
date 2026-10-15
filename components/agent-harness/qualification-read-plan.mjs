export function qualificationReadBatches(binding,scope,continuation){
 const call=(tool,args)=>({tool,arguments:args});
 const reads=[
  [call('mcp__raptor__request_get',{requestId:binding.requestId}),call('mcp__raptor__environment_get',{requestId:binding.requestId,envCode:binding.envCode}),call('mcp__raptor__object_get',{requestId:binding.requestId,kind:binding.objectKind,code:binding.objectCode})],
  [call('mcp__infra__cloud_identity_get',{envCode:binding.envCode}),call('mcp__infra__deployments_list',{envCode:binding.envCode,kind:binding.objectKind,code:binding.objectCode})],
  [call('mcp__infra__deployment_status_get',{envCode:binding.envCode,...scope.application})],
  [call('cloud_read',{kind:'fleet'})]
 ];
 return continuation?.decision==='resume'&&continuation.approvalId?[[call('mcp__raptor__approval_get',{requestId:binding.requestId,approvalId:continuation.approvalId})],...reads]:reads;
}
