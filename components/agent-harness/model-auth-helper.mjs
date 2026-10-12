import {fileURLToPath} from 'node:url';
import {ModelRuntime} from '@earendil-works/pi-coding-agent';
import {createInterface} from 'node:readline';

export async function runLogin(runtime,emit,signal,{method='device_code',readRedirect}={}) {
 try {
  const credential=await runtime.login('openai-codex','oauth',{
   signal,
   prompt:async prompt=>{
    if(prompt.type==='select')return method;
    if(method==='browser'&&prompt.type==='manual_code'&&readRedirect)return readRedirect();
    throw Error('UnexpectedPrompt');
   },
   notify:event=>{
    if(event.type==='device_code')emit({type:'challenge',verificationUrl:event.verificationUri,userCode:event.userCode,expiresAt:new Date(Date.now()+(event.expiresInSeconds??900)*1000).toISOString()});
    if(method==='browser'&&event.type==='auth_url')emit({type:'browser_challenge',authorizationUrl:event.url,expiresAt:new Date(Date.now()+15*60*1000).toISOString()});
   },
  });
  if(credential?.type!=='oauth'||!credential.access||!credential.refresh||!Number.isFinite(credential.expires))throw Error('InvalidCredential');
  emit({type:'credential',credential});
 } catch {emit({type:'failure',code:'ProviderAuthorizationFailed'});}
}

if(process.argv[1]===fileURLToPath(import.meta.url)) {
 const {AuthStorage}=await import(new URL('./core/auth-storage.js',import.meta.resolve('@earendil-works/pi-coding-agent')));
 const runtime=await ModelRuntime.create({credentials:AuthStorage.inMemory(),modelsPath:null,refreshOnCreate:false});
 if(process.argv[2]==='--models') {
  const models=runtime.getModels('openai-codex').map(model=>({modelId:model.id,displayName:model.name||model.id}));
  process.stdout.write(JSON.stringify({models})+'\n');
 } else {
  const browser=process.argv[2]==='--browser';
  const input=browser?createInterface({input:process.stdin,terminal:false}):null;
  let readRedirect;
  if(input)readRedirect=()=>new Promise((resolve,reject)=>{
   input.once('line',line=>{input.close();resolve(line)});
   input.once('close',()=>reject(Error('RedirectUnavailable')));
  });
  await runLogin(runtime,value=>process.stdout.write(JSON.stringify(value)+'\n'),undefined,{method:browser?'browser':'device_code',readRedirect});
 }
}
