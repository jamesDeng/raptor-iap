import fs from 'node:fs';import path from 'node:path';import {execFileSync} from 'node:child_process';
function safe(file){const resolved=path.resolve(file);for(let p=resolved;;p=path.dirname(p)){if(fs.existsSync(p)&&fs.lstatSync(p).isSymbolicLink())throw Error('InvalidSkillsRelease');if(path.dirname(p)===p)break;}return resolved;}
export async function prepareSkills({repository,tag,commitSHA,destination}){
 if(!/^skills-v[0-9]+\.[0-9]+\.[0-9]+$/.test(tag)||!/^[a-f0-9]{40}$/.test(commitSHA))throw Error('InvalidSkillsRelease');
 const repo=safe(repository),dest=safe(destination);
 const git=(...args)=>execFileSync('git',['-C',repo,...args],{stdio:['ignore','pipe','pipe'],maxBuffer:2*1024*1024});
 try{
  if(git('rev-parse','--verify','refs/tags/'+tag+'^{commit}').toString().trim()!==commitSHA)throw Error('InvalidSkillsRelease');
  const entries=git('ls-tree','-rz','--full-tree',commitSHA,'--','agent-skills').toString().split('\0').filter(Boolean);
  if(!entries.length||entries.length>256||fs.existsSync(dest))throw Error('InvalidSkillsRelease');
  const files=[];let total=0;
  for(const entry of entries){const match=/^100644 blob ([a-f0-9]{40})\t(agent-skills\/.+)$/.exec(entry);if(!match)throw Error('InvalidSkillsRelease');const relative=match[2].slice('agent-skills/'.length);if(relative.split('/').some(p=>!p||p==='.'||p==='..')||!/^[-a-zA-Z0-9_./]+$/.test(relative)||!relative.match(/\.(md|json|txt)$/))throw Error('InvalidSkillsRelease');const bytes=git('cat-file','blob',match[1]);total+=bytes.length;if(total>1024*1024)throw Error('InvalidSkillsRelease');files.push([relative,bytes]);}
  if(!files.some(([name])=>name.endsWith('/SKILL.md')))throw Error('InvalidSkillsRelease');
  fs.mkdirSync(dest,{recursive:true,mode:0o700});
  try{for(const [relative,bytes] of files){const file=path.join(dest,relative);fs.mkdirSync(path.dirname(file),{recursive:true,mode:0o700});fs.writeFileSync(file,bytes,{flag:'wx',mode:0o600});}}catch{fs.rmSync(dest,{recursive:true,force:true});throw Error('InvalidSkillsRelease');}
  return {tag,commitSHA,path:dest};
 }catch{throw Error('InvalidSkillsRelease');}
}
