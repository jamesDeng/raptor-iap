import {fileURLToPath} from 'node:url';
import {ModelRuntime} from '@earendil-works/pi-coding-agent';

export async function runLogin(runtime,emit,signal) {
 try {
  const credential=await runtime.login('openai-codex','oauth',{
   signal,
   prompt:async prompt=>{if(prompt.type!=='select')throw Error('UnexpectedPrompt');return 'device_code';},
   notify:event=>{
    if(event.type==='device_code')emit({type:'challenge',verificationUrl:event.verificationUri,userCode:event.userCode,expiresAt:new Date(Date.now()+(event.expiresInSeconds??900)*1000).toISOString()});
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
  await runLogin(runtime,value=>process.stdout.write(JSON.stringify(value)+'\n'));
 }
}
