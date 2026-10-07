import fs from 'node:fs';import path from 'node:path';import {pathToFileURL} from 'node:url';
import {loadPiRuntime,refreshCredentials,runReadProbe} from './pi-adapter.mjs';import {createCheckpoint,restoreCheckpoint} from './checkpoint.mjs';
import {runAppQuestion} from './app-question.mjs';
import {runLiveJob} from './live-runner.mjs';
import {ProgressWriter,writeTerminal} from './progress.mjs';
import {validateApplicationRequest} from './application-context.mjs';
export async function runJob(job,dependencies={}){
 if(job.phase==='live-inference')return runLiveJob(job,dependencies);
 const deps={loadPiRuntime,refreshCredentials,runReadProbe,runAppQuestion,createCheckpoint,restoreCheckpoint,...dependencies};const result={phase:job.phase,passed:false};
 try{
  if(job.phase==='inference'&&job.request!==undefined)validateApplicationRequest(job.request);
  if(job.phase==='restore'){await deps.restoreCheckpoint({reference:job.reference,stateRoot:job.state_root,mountRoot:job.mount_root,prefix:job.prefix});await deps.loadPiRuntime({stateRoot:job.state_root});return {...result,passed:true};}
  const {credentials,runtime}=await deps.loadPiRuntime({stateRoot:job.state_root});
  if(job.phase==='refresh'){Object.assign(result,await deps.refreshCredentials({credentials,runtime,forceExpiry:true}));result.passed=true;}
  else if(job.phase==='inference'){Object.assign(result,await (job.request?deps.runAppQuestion({stateRoot:job.state_root,runtime,limits:job.limits,request:job.request}):deps.runReadProbe({stateRoot:job.state_root,runtime,limits:job.limits})));}
  else throw Error('InvalidJob');
 }catch(e){result.error=e.message==='NeedsSignIn'?'NeedsSignIn':'RunnerFailed';}
 if(['refresh','inference'].includes(job.phase)){
  try{result.checkpoint=await deps.createCheckpoint({stateRoot:job.state_root,mountRoot:job.mount_root,generation:job.generation,prefix:job.prefix});}
  catch{result.passed=false;result.error='CheckpointFailed';}
 }
 return result;
}
async function main(){
 const input=process.argv[2],output=process.argv[3];let result,timer;const controller=new AbortController();
 try{const job=JSON.parse(fs.readFileSync(input,'utf8'));const writer=job.phase==='live-inference'?new ProgressWriter(output.replace(/-result\.json$/,'-progress.jsonl')):null;if(writer){const cancel=output.replace(/-result\.json$/,'-cancel');const check=()=>{if(fs.existsSync(cancel))controller.abort();};check();timer=setInterval(check,200);}result=await runJob(job,writer?{onProgress:event=>writer.append(event),signal:controller.signal}:{});}catch{result={passed:false,error:'InvalidJob'};}
 clearInterval(timer);writeTerminal(output,result);process.exitCode=result.passed?0:1;
}
if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href)await main();
