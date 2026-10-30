import {createObservationTools} from './observation-tools.mjs';
import {McpClient,StreamableHttpTransport} from '@earendil-works/pi-mcp';
import {isDeepStrictEqual} from 'node:util';
import {execFile} from 'node:child_process';
const methods={cloud_read:'cloudRead',deployment_read:'deploymentRead',metrics_read:'metricsRead',db_connection_probe:'dbConnectionProbe'};
const invalid=()=>Error('InvalidObservation');
const metricSeries=['up','upTimestamp','active','activeTimestamp','idle','idleTimestamp','waiting','waitingTimestamp'];

export async function runGatewayObservation(input,{probe=invokeProbe,fetch=globalThis.fetch}={}){
 let client,timer;
 try{
  const deadline=Date.parse(input?.deadlineAt),remaining=()=>deadline-Date.now();
  const infra=input?.infra,scope=input?.scope,binding=input?.binding;
  const endpoint=new URL(infra?.url);
  if(!input||!Object.hasOwn(methods,input.tool)||input.config?.transport||!Number.isFinite(deadline)||remaining()<=0||remaining()>20000||endpoint.protocol!=='https:'||!endpoint.host||endpoint.username||endpoint.password||endpoint.search||endpoint.hash||!endpoint.pathname.endsWith('/mcp')||typeof infra.credential!=='string'||!/^raptor_at_[A-Za-z0-9_-]{43}$/.test(infra.credential)||!scope||scope.envCode!==binding?.envCode||scope.proxyCode!==binding?.objectCode||scope.application?.clusterId!==binding?.clusterId)throw invalid();
  const controller=new AbortController();timer=setTimeout(()=>controller.abort(),remaining());
  const check=()=>{if(controller.signal.aborted||remaining()<=0)throw invalid();};
  const within=promise=>new Promise((resolve,reject)=>{
   const stop=()=>reject(invalid());if(controller.signal.aborted){stop();return;}
   controller.signal.addEventListener('abort',stop,{once:true});
   Promise.resolve(promise).then(resolve,reject).finally(()=>controller.signal.removeEventListener('abort',stop));
  });
  const plain='agent:'+infra.credential,encoded=Buffer.from(plain).toString('base64'),authorization='Basic '+encoded;
  const secrets=[infra.credential,plain,encoded,authorization];
  const safe=value=>{if(secrets.some(secret=>value.includes(secret)))throw invalid();};
  // Bound JSON as well as SSE before the SDK consumes it, including error bodies.
  const guardedFetch=async(target,init={})=>{
   check();if(String(target)!==endpoint.href||init.method!=='POST')throw invalid();
   const signal=init.signal?AbortSignal.any([controller.signal,init.signal]):controller.signal;
   const response=await within(fetch(target,{...init,redirect:'error',signal}));
   if(!response.ok||response.redirected)throw invalid();
   const parts=[];let size=0;const reader=response.body?.getReader();
   if(reader){try{for(;;){check();const {done,value}=await within(reader.read());if(done)break;size+=value.byteLength;if(size>262144)throw invalid();parts.push(Buffer.from(value));}}catch(e){void reader.cancel().catch(()=>{});throw e;}finally{reader.releaseLock();}}
   check();const raw=Buffer.concat(parts).toString('utf8');safe(raw);
   return new Response(raw||null,{status:response.status,headers:response.headers});
  };
  const transport=new StreamableHttpTransport({url:endpoint,headers:{'X-Infra-Authorization':authorization},fetch:guardedFetch,openGetStream:false,maxMessageBytes:262144,reconnect:{maxRetries:0}});
  client=new McpClient({name:'raptor-gateway-observer',version:'1.0.0',requestTimeoutMs:remaining()});
  let connected=false;
  const selectors={requestId:binding.requestId,envCode:scope.envCode,proxyCode:scope.proxyCode,groupId:scope.groupId,serverGroupId:scope.serverGroupId};
  const call=async(name,selection)=>{
   check();if(!connected){await client.connect(transport);connected=true;}
   check();const result=await client.callTool(name,{...selectors,...selection},{signal:controller.signal,timeoutMs:remaining()});check();
   if(result.isError)throw invalid();safe(JSON.stringify(result));
   let envelope=result.structuredContent;const text=result.content?.filter(part=>part.type==='text')??[];
   if(text.length>1)throw invalid();if(text.length){const parsed=JSON.parse(text[0].text);if(envelope&&!isDeepStrictEqual(envelope,parsed))throw invalid();envelope=parsed;}
   if(!envelope||Object.keys(envelope).join(',')!=='data'||!envelope.data||typeof envelope.data!=='object')throw invalid();
   return envelope.data;
  };
  const tools=createObservationTools({binding,config:input.config,replacementScope:scope,fetch,
   readCloud:async({kind})=>call('proxy_observation_get',{kind}),
   readPgcatSeries:async series=>{if(!metricSeries.includes(series))throw invalid();const data=await call('proxy_pgcat_metrics_get',{series});if(data.series!==series||Object.keys(data).sort().join(',')!=='observedAt,rows,series'||!Number.isFinite(Date.parse(data.observedAt))||!Array.isArray(data.rows))throw invalid();return data.rows;},
   exec:(command,args,env)=>{check();if(command==='aliyun')throw invalid();return probe({command,args,env,kubeconfig:input.kubeconfig,config:input.config});}});
  const result=await within(tools[methods[input.tool]](input.arguments));check();safe(JSON.stringify(result));return result;
 }catch{throw invalid();}finally{if(client)await client.close().catch(()=>{});clearTimeout(timer);}
}
function invokeProbe(input){return new Promise((resolve,reject)=>{
 const child=execFile('/usr/local/bin/gateway-observer-probe',[],{env:{PATH:'/usr/local/bin:/usr/bin:/bin'},timeout:15000,maxBuffer:262144},(error,stdout)=>{if(error)return reject(invalid());try{resolve(JSON.parse(stdout));}catch{reject(invalid());}});child.stdin.on('error',()=>{});child.stdin.end(JSON.stringify(input));
});}
if(process.argv[1]?.endsWith('/gateway-observation-runner.mjs')){
 try{let input='';for await(const chunk of process.stdin){input+=chunk;if(Buffer.byteLength(input)>131072)throw invalid();}const result=await runGatewayObservation(JSON.parse(input));const raw=JSON.stringify(result);if(Buffer.byteLength(raw)>262144)throw invalid();process.stdout.write(raw);}catch{process.stderr.write('InvalidObservation');process.exitCode=1;}
}
