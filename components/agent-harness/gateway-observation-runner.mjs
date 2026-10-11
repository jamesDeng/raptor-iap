import {createObservationTools} from './observation-tools.mjs';
import {execFile} from 'node:child_process';
const methods={cloud_read:'cloudRead',deployment_read:'deploymentRead',metrics_read:'metricsRead',db_connection_probe:'dbConnectionProbe'};
export async function runGatewayObservation(input,{probe=invokeProbe,fetch=globalThis.fetch}={}){
 if(!input||!Object.hasOwn(methods,input.tool)||input.config?.transport)throw Error('InvalidObservation');
 const tools=createObservationTools({binding:input.binding,config:input.config,replacementScope:input.scope,fetch,exec:(command,args,env)=>probe({command,args,env,kubeconfig:input.kubeconfig,config:input.config})});
 return tools[methods[input.tool]](input.arguments);
}
function invokeProbe(input){return new Promise((resolve,reject)=>{
 const child=execFile('/usr/local/bin/gateway-observer-probe',[],{env:{PATH:'/usr/local/bin:/usr/bin:/bin'},timeout:15000,maxBuffer:262144},(error,stdout)=>{if(error)return reject(Error('InvalidObservation'));try{resolve(JSON.parse(stdout));}catch{reject(Error('InvalidObservation'));}});child.stdin.on('error',()=>{});child.stdin.end(JSON.stringify(input));
});}
if(process.argv[1]?.endsWith('/gateway-observation-runner.mjs')){
 try{let input='';for await(const chunk of process.stdin){input+=chunk;if(Buffer.byteLength(input)>131072)throw Error();}const result=await runGatewayObservation(JSON.parse(input));const raw=JSON.stringify(result);if(Buffer.byteLength(raw)>262144)throw Error();process.stdout.write(raw);}catch{process.stderr.write('InvalidObservation');process.exitCode=1;}
}
