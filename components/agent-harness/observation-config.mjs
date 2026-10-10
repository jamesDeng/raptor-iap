import fs from 'node:fs';import path from 'node:path';
export function readObservationConfig(file,privateRoot){try{
 if(!path.isAbsolute(file)||path.dirname(file)!==path.resolve(privateRoot)||path.basename(file)!=='observation.json')throw Error();
 for(let cursor=file;;cursor=path.dirname(cursor)){if(fs.lstatSync(cursor).isSymbolicLink())throw Error();if(cursor===path.dirname(cursor))break;}
 const fd=fs.openSync(file,fs.constants.O_RDONLY|fs.constants.O_NOFOLLOW);try{const stat=fs.fstatSync(fd);if(!stat.isFile()||(stat.mode&0o077)!==0||stat.size>65536)throw Error();return JSON.parse(fs.readFileSync(fd,'utf8'));}finally{fs.closeSync(fd);}
 }catch{throw Error('InvalidObservation');}}
export function removeObservationCredentials(privateRoot){try{
 if(typeof privateRoot!=='string'||!path.isAbsolute(privateRoot)||path.resolve(privateRoot)===path.parse(privateRoot).root)throw Error();
 for(let cursor=privateRoot;;cursor=path.dirname(cursor)){if(fs.lstatSync(cursor).isSymbolicLink())throw Error();if(cursor===path.dirname(cursor))break;}
 for(const name of ['observation.json','kubeconfig','attempt-job.json']){const file=path.join(privateRoot,name);let stat;try{stat=fs.lstatSync(file);}catch(e){if(e.code==='ENOENT')continue;throw e;}if(!stat.isFile()||stat.isSymbolicLink())throw Error();fs.unlinkSync(file);}
 }catch{throw Error('ObservationCleanupFailed');}}
