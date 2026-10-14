export const OBSERVATION_TOOLS=['cloud_read','deployment_read','metrics_read','db_connection_probe'];
export function observationArgsCategory(name,args){
 if(!OBSERVATION_TOOLS.includes(name))return undefined;
 if(!args||typeof args!=='object'||Array.isArray(args))return 'args-invalid';
 const keys=Object.keys(args);
 const exact=key=>keys.length===1&&keys[0]===key&&typeof args[key]==='string';
 const valid=name==='cloud_read'?exact('kind')&&['fleet','backend','capacity'].includes(args.kind):name==='metrics_read'?exact('kind')&&['trafficFailures','connections','pgcatClients'].includes(args.kind):name==='deployment_read'?keys.length===0:exact('instanceId')&&/^[-A-Za-z0-9_.]{1,128}$/.test(args.instanceId);
 return valid?'args-valid':'args-invalid';
}
export function observationExtension(observer){return pi=>{
 const definitions=[['cloud_read','Read the request-bound ESS fleet.',{kind:{type:'string',enum:['fleet','backend','capacity']}},['kind'],observer.cloudRead],['deployment_read','Read status of the request-bound test client Deployment.',{},[],observer.deploymentRead],['metrics_read','Read fresh scoped traffic or per-node PgCat client metrics from the trusted private endpoint.',{kind:{type:'string',enum:['trafficFailures','connections','pgcatClients']}},['kind'],observer.metricsRead],['db_connection_probe','Run fixed read-only SELECT 1 against a trusted PgCat node.',{instanceId:{type:'string'}},['instanceId'],observer.dbConnectionProbe]];
 for(const [name,description,properties,required,read] of definitions)pi.registerTool({name,label:name,description,parameters:{type:'object',properties,required,additionalProperties:false},execute:async(_id,args)=>{const data=await read(args);return {content:[{type:'text',text:JSON.stringify(data)}],details:{}};}});
};}
