export const OBSERVATION_TOOLS=['cloud_read','deployment_read','metrics_read','db_connection_probe'];
export const PG_CAT_CONTINUITY='pgcat_connection_continuity';
export function operationObservationTools(binding){
 return binding?.approvalProtocol==='request-grants-v1'?[...OBSERVATION_TOOLS,PG_CAT_CONTINUITY]:OBSERVATION_TOOLS;
}
export function validContinuityArgs(args){
 if(!args||typeof args!=='object'||Array.isArray(args))return false;
 const keys=Object.keys(args).sort().join(',');
 if(args.phase==='hold')return keys==='phase';
 const validId=value=>typeof value==='string'&&/^[-A-Za-z0-9_.]{1,128}$/.test(value);
 return args.phase==='verify-drain'&&keys==='drainActionId,observationId,phase'&&validId(args.observationId)&&validId(args.drainActionId);
}
export function observationArgsCategory(name,args){
 if(name===PG_CAT_CONTINUITY)return validContinuityArgs(args)?'args-valid':'args-invalid';
 if(!OBSERVATION_TOOLS.includes(name))return undefined;
 if(!args||typeof args!=='object'||Array.isArray(args))return 'args-invalid';
 const keys=Object.keys(args);
 const exact=key=>keys.length===1&&keys[0]===key&&typeof args[key]==='string';
 const valid=name==='cloud_read'?exact('kind')&&['fleet','backend','capacity'].includes(args.kind):name==='metrics_read'?exact('kind')&&['trafficFailures','connections','pgcatClients'].includes(args.kind):name==='deployment_read'?keys.length===0:exact('instanceId')&&/^[-A-Za-z0-9_.]{1,128}$/.test(args.instanceId);
 return valid?'args-valid':'args-invalid';
}
export function observationExtension(observer){return pi=>{
 const definitions=[['cloud_read','Read the request-bound ESS fleet.',{kind:{type:'string',enum:['fleet','backend','capacity']}},['kind'],observer.cloudRead],['deployment_read','Read status of the request-bound test client Deployment.',{},[],observer.deploymentRead],['metrics_read','Read fresh scoped traffic or per-node PgCat client metrics from the trusted private endpoint.',{kind:{type:'string',enum:['trafficFailures','connections','pgcatClients']}},['kind'],observer.metricsRead],['db_connection_probe','Run fixed read-only SELECT 1 against a trusted PgCat node.',{instanceId:{type:'string'}},['instanceId'],observer.dbConnectionProbe]];
 if(observer.pgcatConnectionContinuity)definitions.push([PG_CAT_CONTINUITY,
  'Gateway holds one physical NLB TCP connection to each original node or verifies those sockets still execute SQL after drain and all fresh connections reach the replacement pair.',
  {phase:{type:'string',enum:['hold','verify-drain']},observationId:{type:'string'},drainActionId:{type:'string'}},
  ['phase'],observer.pgcatConnectionContinuity]);
 for(const [name,description,properties,required,read] of definitions)pi.registerTool({name,label:name,description,parameters:{type:'object',properties,required,additionalProperties:false},execute:async(_id,args)=>{const data=await read(args);return {content:[{type:'text',text:JSON.stringify(data)}],details:{}};}});
};}
