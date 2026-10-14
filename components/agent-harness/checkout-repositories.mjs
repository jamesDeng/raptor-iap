import fs from 'node:fs';
import path from 'node:path';
import {spawn} from 'node:child_process';
import {pathToFileURL} from 'node:url';

const urlPattern=/^https:\/\/github\.com\/[A-Za-z0-9][A-Za-z0-9-]*\/[A-Za-z0-9][A-Za-z0-9_.-]*(?:\.git)?$/;
const branchPattern=/^[A-Za-z0-9][A-Za-z0-9._/-]*$/;
const shaPattern=/^[a-f0-9]{40}$/;

export async function checkoutRepositories(rows,{git=runGit,workspace='/workspace'}={}){
 if(!Array.isArray(rows)||rows.length<1||rows.length>32)throw Error('InvalidRepositories');
 if(!path.isAbsolute(workspace)||fs.realpathSync(workspace)!==workspace)throw Error('InvalidRepositories');
 const roots=new Map();
 for(const row of rows){
  const root=typeof row?.path==='string'?row.path.match(new RegExp('^'+workspace.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')+'/repositories/repo-[0-9]+'))?.[0]:undefined;
  if(!root||!urlPattern.test(row.url)||!shaPattern.test(row.baseCommit)||!branchPattern.test(row.baseBranch)||row.baseBranch.includes('..')||row.baseBranch.includes('//')||row.baseBranch.endsWith('/')||typeof row.directory!=='string'||path.posix.isAbsolute(row.directory)||row.directory.split('/').includes('..'))throw Error('InvalidRepositories');
  const selected=row.directory==='.'?root:path.posix.join(root,row.directory);
  if(selected!==row.path||!selected.startsWith(root))throw Error('InvalidRepositories');
  const value={url:row.url,branch:row.baseBranch,sha:row.baseCommit};
  const prior=roots.get(root);
  if(prior&&JSON.stringify(prior)!==JSON.stringify(value))throw Error('InvalidRepositories');
  roots.set(root,value);
 }
 const parent=path.join(workspace,'repositories');
 fs.mkdirSync(parent,{recursive:true,mode:0o700});
 if(fs.realpathSync(parent)!==parent)throw Error('InvalidRepositories');
 const entries=[...roots];let next=0,failure;
 await Promise.all(Array.from({length:Math.min(8,entries.length)},async()=>{
  while(!failure){
   const index=next++;
   if(index>=entries.length)return;
   const [root,{url,branch,sha}]=entries[index];
   try{
    fs.rmSync(root,{recursive:true,force:true});
    fs.mkdirSync(root,{mode:0o700});
    await git(root,['init','-q']);
    await git(root,['remote','add','origin',url]);
    await git(root,['fetch','--depth=1','origin','refs/heads/'+branch]);
    if((await git(root,['rev-parse','FETCH_HEAD'])).trim()!==sha)throw Error('RepositoryMoved');
    await git(root,['checkout','--detach','FETCH_HEAD']);
    await git(root,['config','--local','user.name','Raptor Sandbox Agent']);
    await git(root,['config','--local','user.email','raptor-sandbox@localhost']);
   }catch(error){failure??={error,url};}
  }
 }));
 if(failure)throw Object.assign(failure.error,{repositoryURL:failure.url});
}

async function runGit(root,args){
 return new Promise((resolve,reject)=>{
  const child=spawn('git',['-C',root,...args],{stdio:['ignore','pipe','ignore'],timeout:60000,env:{...process.env,GIT_TERMINAL_PROMPT:'0',GIT_CONFIG_GLOBAL:'/dev/null',GIT_CONFIG_NOSYSTEM:'1'}});
  let output='';
  child.stdout.on('data',chunk=>{
   output+=chunk.toString('utf8');
   if(output.length>4096){child.kill();reject(Error('GitOutputExceeded'));}
  });
  child.on('error',reject);
  child.on('close',(code,signal)=>code===0?resolve(output):reject(Error(signal?'GitTimeout':'GitFailed')));
 });
}

if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href){
 const [metadataPath,failurePath]=process.argv.slice(2);
 let current='';
 try{
  if(!path.isAbsolute(metadataPath)||!path.isAbsolute(failurePath))throw Error('InvalidRepositories');
  const rows=JSON.parse(fs.readFileSync(metadataPath,'utf8'));
  await checkoutRepositories(rows);
  fs.rmSync(failurePath,{force:true});
 }catch(error){
  current=urlPattern.test(error.repositoryURL)?error.repositoryURL:'';
  try{fs.writeFileSync(failurePath,JSON.stringify({url:current}),{mode:0o600});}catch{}
  process.exitCode=1;
 }
}
