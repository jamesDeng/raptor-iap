import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';import {createHash} from 'node:crypto';
import {stageSkillsBundle} from './skills-bundle.mjs';
test('private verified bundle stages bounded immutable docs outside session state',async()=>{
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'skills-bundle-')));try{
 const data=Buffer.from('---\nname: pgcat-replacement\ndescription: safe\n---\nPinned skill.');const sha=createHash('sha256').update(data).digest('hex');const b={skillsCommit:'a'.repeat(40)};
 const bundle={version:1,tag:'skills-v1.0.0',commitSHA:b.skillsCommit,files:[{path:'pgcat-replacement/SKILL.md',sha256:sha,data:data.toString('base64')}]};
 const file=path.join(root,'bundle.json');fs.writeFileSync(file,JSON.stringify(bundle),{mode:0o600});const staged=stageSkillsBundle({file,destination:path.join(root,'skills'),binding:b});assert.equal(staged.commitSHA,b.skillsCommit);assert.equal(fs.readFileSync(path.join(staged.path,'pgcat-replacement/SKILL.md'),'utf8'),data.toString());
 for(const change of ['sha','path','commit','duplicate']){const bad=structuredClone(bundle);if(change==='sha')bad.files[0].sha256='b'.repeat(64);if(change==='path')bad.files[0].path='../auth.json';if(change==='commit')bad.commitSHA='b'.repeat(40);if(change==='duplicate')bad.files.push(bad.files[0]);fs.writeFileSync(file,JSON.stringify(bad));assert.throws(()=>stageSkillsBundle({file,destination:path.join(root,change),binding:b}),/InvalidSkillsRelease/);assert.equal(fs.existsSync(path.join(root,change)),false);}
 }finally{fs.rmSync(root,{recursive:true,force:true})}
});
