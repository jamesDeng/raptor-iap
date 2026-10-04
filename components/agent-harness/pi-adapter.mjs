import fs from 'node:fs';import path from 'node:path';import {randomUUID,randomBytes} from 'node:crypto';import {createRequire} from 'node:module';import {pathToFileURL} from 'node:url';
const require=createRequire(import.meta.url);
async function modules(){const entry=new URL(import.meta.resolve('@earendil-works/pi-coding-agent'));return {sdk:await import(entry.href),auth:await import(new URL('./core/auth-storage.js',entry).href)};}
export async function loadPiRuntime({stateRoot}){
 process.env.PI_CODING_AGENT_DIR=stateRoot;process.env.PI_OFFLINE='1';process.env.PI_TELEMETRY='0';delete process.env.OPENAI_API_KEY;
 fs.mkdirSync(stateRoot,{recursive:true,mode:0o700});fs.chmodSync(stateRoot,0o700);fs.mkdirSync(path.join(stateRoot,'sessions'),{recursive:true,mode:0o700});
 const file=path.join(stateRoot,'auth.json');if(!fs.existsSync(file)||fs.lstatSync(file).isSymbolicLink())throw Error('NeedsSignIn');fs.chmodSync(file,0o600);
 const host=path.join(stateRoot,'host-id');if(!fs.existsSync(host))fs.writeFileSync(host,randomUUID(),{mode:0o600,flag:'wx'});
 const {sdk,auth}=await modules(),credentials=auth.AuthStorage.create(file),record=await credentials.read('openai');if(record?.type!=='oauth')throw Error('NeedsSignIn');
 const runtime=await sdk.ModelRuntime.create({credentials,modelsPath:null,modelsStorePath:path.join(stateRoot,'catalog.json'),allowModelNetwork:false});return {credentials,runtime};
}
export async function refreshCredentials({credentials,runtime,forceExpiry}){
 const before=await credentials.read('openai');if(forceExpiry)await credentials.modify('openai',async c=>({...c,expires:0}));
 try{await runtime.getAuth('openai');}catch{throw Error('NeedsSignIn');}
 const after=await credentials.read('openai');if(!after||after.expires<=Date.now())throw Error('NeedsSignIn');
 return {refresh_succeeded:true,refresh_token_changed:before.refresh!==after.refresh};
}
export function collectProbe(marker,maxTurns,abort){
 const value={passed:false,tool_succeeded:false,answer_matches:false,usage:[]};let turns=0,error;
 return {observe(e){
 if(e.type==='turn_start'&&++turns>maxTurns){error='TurnLimit';abort();}
 if(e.type==='tool_execution_end'&&e.toolName==='read')value.tool_succeeded||=!e.isError&&!!e.result?.content?.some(c=>c.type==='text'&&c.text.includes(marker));
 if(e.type==='message_end'&&e.message.role==='assistant'){
  value.answer_matches=e.message.content.filter(c=>c.type==='text').map(c=>c.text).join('').trim()===marker;
  if(['error','aborted'].includes(e.message.stopReason))error||='ModelFailed';
  const u=e.message.usage;if(u)value.usage.push({input:u.input,output:u.output,total_tokens:u.totalTokens});
 }},timeout(){error='Timeout';abort();},result(){return {...value,passed:!!(value.tool_succeeded&&value.answer_matches&&!error),...(error?{error}:{})};}};
}
export async function runReadProbe({stateRoot,runtime,limits}){
 const {sdk}=await modules(),marker='RAPTOR_'+randomBytes(12).toString('hex'),fixture=path.join(stateRoot,'read-fixture.txt');fs.writeFileSync(fixture,marker+'\n',{mode:0o600});
 const settingsManager=sdk.SettingsManager.inMemory({cacheWarming:'off',compaction:{enabled:false},retry:{enabled:false,provider:{maxRetries:0}}});
 const resourceLoader=new sdk.DefaultResourceLoader({cwd:stateRoot,agentDir:stateRoot,settingsManager,noExtensions:true,noSkills:true,noPromptTemplates:true,noThemes:true,noContextFiles:true});await resourceLoader.reload();
 const stream=runtime.streamSimple.bind(runtime);runtime.streamSimple=(m,c,o)=>stream(m,c,{...o,maxTokens:limits.max_output_tokens,maxRetries:0});
 let session,timer,observation;
 try{
  ({session}=await sdk.createAgentSession({cwd:stateRoot,agentDir:stateRoot,modelRuntime:runtime,model:runtime.getModel('openai','gpt-5.6-luna'),thinkingLevel:'off',tools:['read'],resourceLoader,settingsManager,sessionManager:sdk.SessionManager.create(stateRoot,path.join(stateRoot,'sessions'))}));
  observation=collectProbe(marker,limits.max_turns,()=>void session.abort());session.subscribe(e=>observation.observe(e));timer=setTimeout(()=>observation.timeout(),limits.model_seconds*1000);
  try{await session.prompt(`Use the read tool to read ${fixture}. Reply with only its exact contents. Do not read any other file.`);}catch{return {passed:false,tool_succeeded:false,answer_matches:false,usage:[],error:'ModelFailed'};}
  return observation.result();
 }finally{clearTimeout(timer);session?.dispose();runtime.streamSimple=stream;}
}
