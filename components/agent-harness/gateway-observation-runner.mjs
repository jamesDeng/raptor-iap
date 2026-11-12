import {validateResourceScope} from './operation-profile.mjs';
import {createObservationTools} from './observation-tools.mjs';
import {McpClient,StreamableHttpTransport} from '@earendil-works/pi-mcp';
import {isDeepStrictEqual} from 'node:util';
import {execFile} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {performance} from 'node:perf_hooks';
const methods={cloud_read:'cloudRead',deployment_read:'deploymentRead',metrics_read:'metricsRead',db_connection_probe:'dbConnectionProbe',pgcat_client_pods:'pgcatClientPods'};
const invalid=()=>Error('InvalidObservation');
const exact=(v,keys)=>v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===keys.slice().sort().join(',');
const metricSeries=['up','upTimestamp','active','activeTimestamp','idle','idleTimestamp','waiting','waitingTimestamp'];


// The only resource-to-private-observation boundary. Never infer provider IDs
// from opaque workloadKey, stamp source observations or normalize metric rows.
export function resourceObservation(data,scope,kind){
 const w=data?.pgproxyWorkload;
 if(!['fleet','backend','capacity','metrics','expansion','retirement'].includes(kind)||!exact(data,['envCode','proxyCode','observedAt','pgproxyWorkload'])||data.envCode!==scope.envCode||data.proxyCode!==scope.proxyCode||!exact(w,['type','code','workloadKey','state','targetDbCode',kind])||w.type!=='ess'||w.code!==scope.proxyCode||w.targetDbCode!==scope.targetDbCode||typeof w.workloadKey!=='string'||!w.workloadKey||w.workloadKey.length>256||typeof w.state!=='string'||!Number.isFinite(Date.parse(data.observedAt))||w[kind]?.observedAt!==data.observedAt)throw invalid();
 return w[kind];
}

const upstreamIdPattern=/^(?:[a-f0-9]{32}|[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}|1-[a-f0-9]{8}-(?:[a-f0-9]{24}|[a-f0-9]{8}-[a-f0-9]{12}))$/i;

export async function runGatewayObservation(input,{probe=invokeProbe,fetch=globalThis.fetch,reportFailure=()=>{}}={}){
 const traceFleet=input?.tool==='cloud_read'&&input.arguments?.kind==='fleet';
 const observationId=traceFleet?randomUUID():'';
 const started=performance.now();
 let client,timer,controller;
 let stage='input_validation',category='invalid_input',upstreamId='',httpStatus=0,infraFailure;
 try{
  const deadline=Date.parse(input?.deadlineAt),remaining=()=>deadline-Date.now();
  const infra=input?.infra,scope=input?.scope,binding=input?.binding;
  const endpoint=new URL(infra?.url);
  validateResourceScope(binding,scope);
  if(!input||!Object.hasOwn(methods,input.tool)||input.config?.transport||!Number.isFinite(deadline)||remaining()<=0||remaining()>20000||endpoint.protocol!=='https:'||!endpoint.host||endpoint.username||endpoint.password||endpoint.search||endpoint.hash||!endpoint.pathname.endsWith('/mcp')||typeof infra.credential!=='string'||!/^raptor_at_[A-Za-z0-9_-]{43}$/.test(infra.credential)||!scope||scope.envCode!==binding?.envCode||scope.proxyCode!==binding?.objectCode||scope.application?.clusterId!==binding?.clusterId)throw invalid();
  controller=new AbortController();timer=setTimeout(()=>controller.abort(),remaining());
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
   let headers=init.headers;
   if(traceFleet){
    headers=new Headers(headers);
    headers.set('X-Raptor-Observation-Id',observationId);
   }
   category='upstream_transport';
   upstreamId='';
   httpStatus=0;
   const response=await within(fetch(target,{...init,headers,redirect:'error',signal}));
   if(traceFleet){
    const ids=[response.headers.get('X-Ca-Request-Id'),response.headers.get('X-Fc-Request-Id')];
    upstreamId=ids.find(id=>id&&upstreamIdPattern.test(id))??'';
    httpStatus=response.status;
   }
   if(!response.ok||response.redirected){category='upstream_http';throw invalid();}
   category='upstream_response';
   const parts=[];let size=0;const reader=response.body?.getReader();
   if(reader){try{for(;;){check();const {done,value}=await within(reader.read());if(done)break;size+=value.byteLength;if(size>262144)throw invalid();parts.push(Buffer.from(value));}}catch(e){void reader.cancel().catch(()=>{});throw e;}finally{reader.releaseLock();}}
   check();const raw=Buffer.concat(parts).toString('utf8');safe(raw);
   return new Response(raw||null,{status:response.status,headers:response.headers});
  };
  const transport=new StreamableHttpTransport({url:endpoint,headers:{'X-Infra-Authorization':authorization},fetch:guardedFetch,openGetStream:false,maxMessageBytes:262144,reconnect:{maxRetries:0}});
  client=new McpClient({name:'raptor-gateway-observer',version:'1.0.0',requestTimeoutMs:remaining()});
  let connected=false;
  const selectors={requestId:binding.requestId,envCode:scope.envCode,proxyCode:scope.proxyCode};
  const call=async(name,selection)=>{
   check();
   if(!connected){stage='mcp_connect';await client.connect(transport);connected=true;}
   stage='mcp_call';
   check();const result=await client.callTool(name,{...selectors,...selection},{signal:controller.signal,timeoutMs:remaining()});check();
   if(result.isError){
    category='upstream_tool_error';
    if(traceFleet){
     const failure=result._meta?.['raptor/fleetReadFailure'];
     if(exact(failure,['stage','category','elapsedMs','upstreamId','httpStatus'])){
      // Go revalidates all fields before emitting a log. Send only bounded
      // metadata, even when an upstream implementation sends an invalid type.
      const label=value=>typeof value==='string'&&value.length<=48?value:'';
      infraFailure={stage:label(failure.stage),category:label(failure.category),
       elapsedMs:Number.isInteger(failure.elapsedMs)?failure.elapsedMs:-1,
       upstreamId:typeof failure.upstreamId==='string'&&upstreamIdPattern.test(failure.upstreamId)?failure.upstreamId:'',
       httpStatus:Number.isInteger(failure.httpStatus)?failure.httpStatus:0};
     }
    }
    throw invalid();
   }
   stage='observation_validation';
   category='invalid_observation';
   safe(JSON.stringify(result));
   let envelope=result.structuredContent;const text=result.content?.filter(part=>part.type==='text')??[];
   if(text.length>1)throw invalid();if(text.length){const parsed=JSON.parse(text[0].text);if(envelope&&!isDeepStrictEqual(envelope,parsed))throw invalid();envelope=parsed;}
   if(!envelope||Object.keys(envelope).join(',')!=='data'||!envelope.data||typeof envelope.data!=='object')throw invalid();
   return envelope.data;
  };
  // The complete bundle is private to this invocation, including its original
  // fleet/backend times. Existing projections and raw validators remain authoritative.
  const clients=input.tool==='metrics_read'&&input.arguments?.kind==='pgcatClients';
  let bundle;
  const clientsBundle=()=>bundle??=(async()=>{
   const data=resourceObservation(await call('proxy_pgcat_metrics_get',{series:'clients'}),scope,'metrics');
   if(!exact(data,['series','observedAt','fleet','backend','samples'])||data.series!=='clients'||!exact(data.samples,metricSeries)||metricSeries.some(series=>!Array.isArray(data.samples[series])))throw invalid();
   return data;
  })();
  const tools=createObservationTools({binding,config:input.config,replacementScope:scope,fetch,
   readCloud:async({kind})=>clients?(await clientsBundle())[kind]:resourceObservation(await call('proxy_observation_get',{kind}),scope,kind),
   readPgcatSeries:async series=>{
    if(!clients||!metricSeries.includes(series))throw invalid();
    return (await clientsBundle()).samples[series];
   },
   exec:(command,args,env)=>{check();if(command==='aliyun')throw invalid();return probe({command,args,env,kubeconfig:input.kubeconfig,config:input.config});}});
  const result=await within(tools[methods[input.tool]](input.arguments));check();safe(JSON.stringify(result));return result;
 }catch{
  if(traceFleet)reportFailure({stage,category:controller?.signal.aborted?'deadline':category,
   upstreamId,httpStatus,observationId,infraFailure,elapsedMs:Math.floor(performance.now()-started)});
  throw invalid();
 }finally{if(client)await client.close().catch(()=>{});clearTimeout(timer);}
}
function invokeProbe(input){return new Promise((resolve,reject)=>{
 const child=execFile('/usr/local/bin/gateway-observer-probe',[],{env:{PATH:'/usr/local/bin:/usr/bin:/bin'},timeout:15000,maxBuffer:262144},(error,stdout)=>{if(error)return reject(invalid());try{resolve(JSON.parse(stdout));}catch{reject(invalid());}});child.stdin.on('error',()=>{});child.stdin.end(JSON.stringify(input));
});}
if(process.argv[1]?.endsWith('/gateway-observation-runner.mjs')){
 let reportedFailure=false;
 try{
  let input='';
  for await(const chunk of process.stdin){
   input+=chunk;
   if(Buffer.byteLength(input)>131072)throw invalid();
  }
  const result=await runGatewayObservation(JSON.parse(input),{reportFailure:diagnostic=>{
   process.stderr.write(JSON.stringify(diagnostic));
   reportedFailure=true;
  }});
  const raw=JSON.stringify(result);
  if(Buffer.byteLength(raw)>262144)throw invalid();
  process.stdout.write(raw);
 }catch{
  if(!reportedFailure)process.stderr.write('InvalidObservation');
  process.exitCode=1;
 }
}
