import {loadPiRuntime} from './pi-adapter.mjs';import {runLiveAppQuestion} from './live-question.mjs';import {createCheckpoint} from './checkpoint.mjs';
export async function runLiveJob(job,dependencies={}){
 const deps={loadPiRuntime,runLiveAppQuestion,createCheckpoint,...dependencies};let settled={passed:false,error:'RunnerFailed'};
 try{const {runtime}=await deps.loadPiRuntime({stateRoot:job.state_root});settled=await deps.runLiveAppQuestion({stateRoot:job.state_root,privateRoot:job.private_root,runtime,limits:job.limits,request:job.request,mcpConfig:job.mcp,onProgress:dependencies.onProgress});}
 catch(e){return {phase:'live-inference',passed:false,error:e.message==='NeedsSignIn'?'NeedsSignIn':'RunnerFailed'};}
 try{const checkpoint=await deps.createCheckpoint({stateRoot:job.state_root,mountRoot:job.mount_root,generation:job.generation,prefix:job.prefix});return {phase:'live-inference',...settled,checkpoint};}
 catch{return {phase:'live-inference',...settled,passed:false,error:settled.error??'CheckpointFailed',checkpointError:'CheckpointFailed'};}
}
