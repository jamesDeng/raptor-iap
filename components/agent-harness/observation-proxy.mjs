import {isDeepStrictEqual} from 'node:util';
const invalid=()=>new Error('InvalidObservation');
const tools={cloudRead:['cloud_read','kind',['fleet','backend','capacity']],deploymentRead:['deployment_read'],metricsRead:['metrics_read','kind',['trafficFailures','connections','pgcatClients']],dbConnectionProbe:['db_connection_probe','instanceId']};
export function createRemoteObservationTools({binding,config,replacementScope,fetch=globalThis.fetch}){
 const keys=['version','transport','url','token','binding','scope','expiresAt'];
 if(!config||Object.keys(config).some(k=>!keys.includes(k))||config.version!==1||config.transport!=='gateway'||config.url!=='https://api.rdev.raptor-iap.top/v1/gateway-observations'||!/^\w{64}$/.test(config.token)||!isDeepStrictEqual(config.binding,binding)||!isDeepStrictEqual(config.scope,replacementScope)||!Number.isFinite(Date.parse(config.expiresAt))||Date.parse(config.expiresAt)<=Date.now())throw invalid();
 return Object.fromEntries(Object.entries(tools).map(([name,[tool,key,kinds]])=>[name,async args=>{
  if(Date.parse(config.expiresAt)<=Date.now()||!args||typeof args!=='object'||Array.isArray(args)||Object.keys(args).length!==(key?1:0)||(key&&(!Object.hasOwn(args,key)||typeof args[key]!=='string'||(kinds?!kinds.includes(args[key]):!/^[-A-Za-z0-9_.]{1,128}$/.test(args[key])))))throw invalid();
  try{
   const response=await fetch(config.url,{method:'POST',redirect:'error',headers:{Authorization:'Bearer '+config.token,'Content-Type':'application/json'},body:JSON.stringify({requestId:binding.requestId,attemptId:binding.attemptId,definitionSha256:binding.definitionSha256,tool,arguments:args}),signal:AbortSignal.timeout(25000)});
   if(!response.ok||!response.body?.getReader)throw invalid();const reader=response.body.getReader();let size=0;const parts=[];
   try{for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>262200){await reader.cancel();throw invalid();}parts.push(Buffer.from(value));}}finally{reader.releaseLock();}
   const raw=Buffer.concat(parts).toString('utf8');if(raw.includes(config.token))throw invalid();const result=JSON.parse(raw);if(!result||Object.keys(result).join(',')!=='data'||result.data===null||typeof result.data!=='object'||Buffer.byteLength(JSON.stringify(result.data))>262144)throw invalid();return result.data;
  }catch{throw invalid();}
 }]));
}
