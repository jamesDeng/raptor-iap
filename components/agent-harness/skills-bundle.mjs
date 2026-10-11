import fs from 'node:fs';import path from 'node:path';import {createHash} from 'node:crypto';
function safe(file){const resolved=path.resolve(file);for(let p=resolved;;p=path.dirname(p)){if(fs.existsSync(p)&&fs.lstatSync(p).isSymbolicLink())throw Error('InvalidSkillsRelease');if(path.dirname(p)===p)break;}return resolved;}
export function stageSkillsBundle({file,destination,binding}){
 try{
 const source=safe(file),dest=safe(destination),info=fs.lstatSync(source);if(!info.isFile()||info.mode&0o077||info.size>2*1024*1024||fs.existsSync(dest))throw Error();
 const bundle=JSON.parse(fs.readFileSync(source,'utf8'));if(bundle.version!==1||!/^skills-v[0-9]+\.[0-9]+\.[0-9]+$/.test(bundle.tag)||!/^([a-f0-9]{40})$/.test(bundle.commitSHA)||bundle.commitSHA!==binding.skillsCommit||!Array.isArray(bundle.files)||!bundle.files.length||bundle.files.length>256)throw Error();
 const files=[],seen=new Set();let total=0;
 for(const entry of bundle.files){const name=entry.path;if(typeof name!=='string'||!/^[-a-zA-Z0-9_./]+\.(md|json|txt)$/.test(name)||name.split('/').some(p=>!p||p==='.'||p==='..')||seen.has(name)||typeof entry.data!=='string'||!/^([a-f0-9]{64})$/.test(entry.sha256))throw Error();seen.add(name);const data=Buffer.from(entry.data,'base64');if(data.toString('base64')!==entry.data||createHash('sha256').update(data).digest('hex')!==entry.sha256)throw Error();total+=data.length;if(total>1024*1024)throw Error();files.push([name,data]);}
 if(!seen.has('pgcat-replacement/SKILL.md'))throw Error();
 fs.mkdirSync(dest,{recursive:true,mode:0o700});try{for(const [name,data]of files){const out=path.join(dest,name);fs.mkdirSync(path.dirname(out),{recursive:true,mode:0o700});fs.writeFileSync(out,data,{flag:'wx',mode:0o600});}}catch{fs.rmSync(dest,{recursive:true,force:true});throw Error();}
 return {tag:bundle.tag,commitSHA:bundle.commitSHA,path:dest};
 }catch{throw Error('InvalidSkillsRelease');}
}
