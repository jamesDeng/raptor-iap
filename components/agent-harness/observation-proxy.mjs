import {isDeepStrictEqual} from 'node:util';
import {createReadConcurrency} from './mcp-runtime.mjs';
import {PG_CAT_CONTINUITY,validContinuityArgs} from './observation-extension.mjs';
const invalid=()=>new Error('InvalidObservation');
const tools={cloudRead:['cloud_read','kind',['fleet','backend','capacity']],deploymentRead:['deployment_read'],metricsRead:['metrics_read','kind',['trafficFailures','connections','pgcatClients']],dbConnectionProbe:['db_connection_probe','instanceId']};
export function createRemoteObservationTools({binding,config,replacementScope,fetch=globalThis.fetch,onDiagnostic=()=>{}}){
 const keys=['version','transport','url','token','binding','scope','expiresAt'];
 if(!config||Object.keys(config).some(k=>!keys.includes(k))||config.version!==1||config.transport!=='gateway'||config.url!=='https://api.rdev.raptor-iap.top/v1/gateway-observations'||!/^\w{64}$/.test(config.token)||!isDeepStrictEqual(config.binding,binding)||!isDeepStrictEqual(config.scope,replacementScope)||!Number.isFinite(Date.parse(config.expiresAt))||Date.parse(config.expiresAt)<=Date.now())throw invalid();
 const scheduleRead=createReadConcurrency(4);
 const selected={...tools};
 if(binding.approvalProtocol==='request-grants-v1'){
  selected.cloudRead=['cloud_read','kind',['fleet','backend','capacity','retirement']];
  selected.pgcatConnectionContinuity=[PG_CAT_CONTINUITY];
 }
 return Object.fromEntries(Object.entries(selected).map(([name,[tool,key,kinds]])=>[name,args=>{
  return scheduleRead(async()=>{
  const matchesSelector=()=>args&&typeof args==='object'&&!Array.isArray(args)&&Object.keys(args).length===(key?1:0)&&
   (!key||(Object.hasOwn(args,key)&&typeof args[key]==='string'&&(kinds?kinds.includes(args[key]):/^[-A-Za-z0-9_.]{1,128}$/.test(args[key]))));
  const valid=tool===PG_CAT_CONTINUITY?validContinuityArgs(args):matchesSelector();
  if(Date.parse(config.expiresAt)<=Date.now()||!valid){onDiagnostic({tool,category:'local-input'});throw invalid();}
  let category='transport-error';
  try{
   const response=await fetch(config.url,{method:'POST',redirect:'error',headers:{Authorization:'Bearer '+config.token,'Content-Type':'application/json'},body:JSON.stringify({requestId:binding.requestId,attemptId:binding.attemptId,definitionSha256:binding.definitionSha256,tool,arguments:args}),signal:AbortSignal.timeout(25000)});
   category=response.ok?'invalid-response':Number.isInteger(response.status)&&response.status>=400&&response.status<=599?'http-'+response.status:'http-error';
   if(!response.ok||!response.body?.getReader)throw invalid();const reader=response.body.getReader();let size=0;const parts=[];
   try{for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>262200){await reader.cancel();throw invalid();}parts.push(Buffer.from(value));}}finally{reader.releaseLock();}
   const raw=Buffer.concat(parts).toString('utf8');if(raw.includes(config.token))throw invalid();const result=JSON.parse(raw);if(!result||Object.keys(result).join(',')!=='data'||result.data===null||typeof result.data!=='object'||Buffer.byteLength(JSON.stringify(result.data))>262144)throw invalid();return result.data;
  }catch{onDiagnostic({tool,category});throw invalid();}
  });
 }]));
}
