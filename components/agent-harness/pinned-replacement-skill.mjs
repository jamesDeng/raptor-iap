import fs from 'node:fs';
import path from 'node:path';

const FILES=[
 'SKILL.md',
 'references/contracts.md',
 'references/recovery.md',
];
const MAX_BYTES=16384;

export function pinnedReplacementInstructions(skills){
 try{
  if(!skills||typeof skills.path!=='string'||!/^skills-v[0-9]+\.[0-9]+\.[0-9]+$/.test(skills.tag)||!/^[a-f0-9]{40}$/.test(skills.commitSHA))throw Error();
  const root=path.resolve(skills.path);
  const selected=path.join(root,'pgcat-replacement');
  for(let cursor=selected;;cursor=path.dirname(cursor)){
   if(fs.lstatSync(cursor).isSymbolicLink())throw Error();
   if(path.dirname(cursor)===cursor)break;
  }
  let total=0;
  const decoder=new TextDecoder('utf-8',{fatal:true});
  const sections=FILES.map(name=>{
   const file=path.join(selected,name);
   if(path.dirname(file)!==selected&&fs.lstatSync(path.dirname(file)).isSymbolicLink())throw Error();
   const stat=fs.lstatSync(file);
   if(!stat.isFile()||stat.isSymbolicLink()||stat.size<1||stat.size>MAX_BYTES)throw Error();
   total+=stat.size;
   if(total>MAX_BYTES)throw Error();
   const content=decoder.decode(fs.readFileSync(file));
   if(!content.trim()||content.includes('\0'))throw Error();
   return `--- ${name} ---\n${content}`;
  });
  return `\nVerified pinned PgCat replacement instructions (${skills.tag}, commit ${skills.commitSHA}). Follow these instructions for this request; their references are included below.\n${sections.join('\n')}`;
 }catch{throw Error('InvalidSkillsRelease');}
}

export function pinnedReplacementProfile(skills){
 const instructions=pinnedReplacementInstructions(skills);
 const markers=instructions.split('\n').filter(line=>line.startsWith('replacement-contract:'));
 if(markers.length!==1||markers[0]!=='replacement-contract: direct-v2')throw Error('InvalidSkillsRelease');
 return 'direct-v2';
}
