import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {pinnedReplacementInstructions} from './pinned-replacement-skill.mjs';

function fixture(){
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'pinned-replacement-')));
 const folder=path.join(root,'pgcat-replacement');
 fs.mkdirSync(path.join(folder,'references'),{recursive:true});
 fs.writeFileSync(path.join(folder,'SKILL.md'),'Protect originals; expand 2 to 4 first.\n');
 fs.writeFileSync(path.join(folder,'references','contracts.md'),'Use exact action IDs.\n');
 fs.writeFileSync(path.join(folder,'references','recovery.md'),'Reconcile unknown outcomes.\n');
 return {root,folder,skills:{tag:'skills-v0.4.0',commitSHA:'a'.repeat(40),path:root}};
}

test('includes exactly the staged replacement skill and both references',()=>{
 const x=fixture();try{
  const result=pinnedReplacementInstructions(x.skills);
  assert.match(result,/Protect originals; expand 2 to 4 first/);
  assert.match(result,/Use exact action IDs/);
  assert.match(result,/Reconcile unknown outcomes/);
  assert.match(result,/skills-v0\.4\.0/);
  assert.match(result,/a{40}/);
 }finally{fs.rmSync(x.root,{recursive:true,force:true});}
});

test('refuses a missing or symlinked pinned reference',()=>{
 const x=fixture();try{
  const file=path.join(x.folder,'references','recovery.md');
  fs.rmSync(file);assert.throws(()=>pinnedReplacementInstructions(x.skills),/InvalidSkillsRelease/);
  fs.symlinkSync(path.join(x.folder,'SKILL.md'),file);
  assert.throws(()=>pinnedReplacementInstructions(x.skills),/InvalidSkillsRelease/);
 }finally{fs.rmSync(x.root,{recursive:true,force:true});}
});

test('refuses oversized pinned instruction bytes',()=>{
 const x=fixture();try{
  fs.writeFileSync(path.join(x.folder,'references','recovery.md'),'x'.repeat(20000));
  assert.throws(()=>pinnedReplacementInstructions(x.skills),/InvalidSkillsRelease/);
 }finally{fs.rmSync(x.root,{recursive:true,force:true});}
});
