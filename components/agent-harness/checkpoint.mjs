import fs from 'node:fs';import path from 'node:path';import {createHash,randomUUID} from 'node:crypto';import {execFileSync} from 'node:child_process';import {fileURLToPath} from 'node:url';
const helper=fileURLToPath(new URL('./archive.py',import.meta.url)),limit=16*1024*1024;
const hash=b=>createHash('sha256').update(b).digest('hex');
function safe(p){let v=path.resolve(p);for(let q=v;;q=path.dirname(q)){if(fs.existsSync(q)&&fs.lstatSync(q).isSymbolicLink())throw Error('UnsafeCheckpointPath');if(path.dirname(q)===q)break;}return v;}
function relative(key,prefix){if(typeof key!=='string'||!key.startsWith(prefix+'/')||key.split('/').some(s=>!s||s==='.'||s==='..'))throw Error('InvalidCheckpoint');return key.slice(prefix.length+1);}
function archive(action,root,file,sha){try{return JSON.parse(execFileSync('python3',[helper,action,root,file,...(sha?['--sha256',sha]:[])],{encoding:'utf8',stdio:['ignore','pipe','pipe'],maxBuffer:4096}));}catch{throw Error('CheckpointArchiveFailed');}}
export async function createCheckpoint({stateRoot,mountRoot,generation,prefix='auth'}){
 if(!/^[a-f0-9-]{36}$/.test(generation))throw Error('InvalidGeneration');
 const mount=safe(mountRoot),folder=safe(path.join(mount,'lifecycle'));fs.mkdirSync(folder,{recursive:true});
 const tmp=safe(path.join(path.dirname(stateRoot),'checkpoint-'+randomUUID()+'.tgz'));
 try{
 const meta=archive('pack',stateRoot,tmp),bytes=fs.readFileSync(tmp),dest=safe(path.join(folder,generation+'.tgz')),checksum=dest.slice(0,-4)+'.sha256';
 fs.writeFileSync(dest,bytes,{flag:'wx'});fs.writeFileSync(checksum,meta.sha256,{flag:'wx'});
 if(hash(fs.readFileSync(dest))!==meta.sha256||fs.readFileSync(checksum,'utf8')!==meta.sha256)throw Error('CheckpointReadbackFailed');
 return {archive_key:prefix+'/lifecycle/'+generation+'.tgz',checksum_key:prefix+'/lifecycle/'+generation+'.sha256',...meta,pi_version:'0.99.2'};
 }finally{if(fs.existsSync(tmp))fs.unlinkSync(tmp);}
}
export async function restoreCheckpoint({reference,stateRoot,mountRoot,prefix='auth',bootstrapOnly=false}){
 const file=safe(path.join(mountRoot,relative(reference.archive_key,prefix))),checksum=safe(path.join(mountRoot,relative(reference.checksum_key,prefix)));
 if(reference.checksum_key!==reference.archive_key.slice(0,-4)+'.sha256'||reference.pi_version!=='0.99.2'||!Number.isInteger(reference.bytes)||reference.bytes<1||reference.bytes>limit||!/^[a-f0-9]{64}$/.test(reference.sha256))throw Error('InvalidCheckpoint');
 if(fs.statSync(file).size!==reference.bytes||fs.readFileSync(checksum,'utf8').trim()!==reference.sha256)throw Error('CheckpointMismatch');
 archive('restore',safe(stateRoot),file,reference.sha256);
 if(bootstrapOnly){const sessions=safe(path.join(stateRoot,'sessions'));fs.rmSync(sessions,{recursive:true});fs.mkdirSync(sessions,{mode:0o700});}
}
