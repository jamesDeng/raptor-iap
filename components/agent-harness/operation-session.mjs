import fs from 'node:fs';
import path from 'node:path';
import {createHash} from 'node:crypto';
const fingerprint=data=>createHash('sha256').update(data).digest('hex');
const keys=['requestId','definitionSha256','skillsCommit','model'];
function identity(binding){
 if(!binding||typeof binding.requestId!=='string'||!binding.requestId||!/^[a-f0-9]{64}$/.test(binding.definitionSha256)||!/^[a-f0-9]{40}$/.test(binding.skillsCommit)||binding.model!=='gpt-5.6-luna')throw Error('InvalidSessionReference');
 return Object.fromEntries(keys.map(k=>[k,binding[k]]));
}
function safe(file){
 const resolved=path.resolve(file);
 for(let cursor=resolved;;cursor=path.dirname(cursor)){
  if(fs.existsSync(cursor)&&fs.lstatSync(cursor).isSymbolicLink())throw Error('InvalidSessionReference');
  if(path.dirname(cursor)===cursor)break;
 }
 return resolved;
}
function sessionPath(stateRoot,file){
 const root=safe(path.join(stateRoot,'sessions'));
 if(typeof file!=='string'||path.basename(file)!==file||!file.endsWith('.jsonl'))throw Error('InvalidSessionReference');
 return safe(path.join(root,file));
}
export function operationSessionReference(manager,binding,stateRoot){
 const expected=identity(binding),file=manager.getSessionFile();
 if(!file||safe(file)!==sessionPath(stateRoot,path.basename(file)))throw Error('InvalidSessionReference');
 const bytes=fs.readFileSync(file);if(bytes.length>16*1024*1024)throw Error('InvalidSessionReference');
 return {version:1,...expected,sessionId:manager.getSessionId(),file:path.basename(file),sha256:fingerprint(bytes)};
}
export function openOperationSession({sdk,stateRoot,binding,sessionReference}){
 const expected=identity(binding),dir=safe(path.join(stateRoot,'sessions'));
 fs.mkdirSync(dir,{recursive:true,mode:0o700});
 if(!sessionReference)return sdk.SessionManager.create(stateRoot,dir);
 const ref=sessionReference;
 if(Object.keys(ref).sort().join(',')!==['version',...keys,'sessionId','file','sha256'].sort().join(',')||ref.version!==1||keys.some(k=>ref[k]!==expected[k])||typeof ref.sessionId!=='string'||!ref.sessionId||!/^[a-f0-9]{64}$/.test(ref.sha256))throw Error('InvalidSessionReference');
 const file=sessionPath(stateRoot,ref.file);
 if(!fs.existsSync(file)||!fs.lstatSync(file).isFile()||fs.statSync(file).size>16*1024*1024||fingerprint(fs.readFileSync(file))!==ref.sha256)throw Error('InvalidSessionReference');
 const manager=sdk.SessionManager.open(file,dir,stateRoot);
 if(manager.getSessionId()!==ref.sessionId)throw Error('InvalidSessionReference');
 return manager;
}
