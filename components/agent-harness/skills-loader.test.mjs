import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';import {execFileSync} from 'node:child_process';
import {DefaultResourceLoader,SettingsManager} from '@earendil-works/pi-coding-agent';
import {prepareSkills} from './skills-loader.mjs';
import {createOperationResourceLoader} from './live-operation.mjs';
test('skills load from verified immutable Git content rather than working tree',async()=>{
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'pi-skills-'))),repo=path.join(root,'repo');fs.mkdirSync(repo);
 const git=(...args)=>execFileSync('git',['-C',repo,...args],{encoding:'utf8',stdio:['ignore','pipe','pipe']}).trim();
 try{git('init');git('config','user.name','Fixture');git('config','user.email','fixture@example.invalid');const folder=path.join(repo,'agent-skills','pgcat-replacement');fs.mkdirSync(folder,{recursive:true});const file=path.join(folder,'SKILL.md');fs.writeFileSync(file,'---\nname: pgcat-replacement\ndescription: Replace scoped PgCat nodes safely.\n---\nPINNED CONTENT\n');git('add','.');git('commit','-m','fixture');const sha=git('rev-parse','HEAD');git('tag','skills-v1.0.0');fs.writeFileSync(file,'WORKING TREE IS NOT AUTHORITY');
 const prepared=await prepareSkills({repository:repo,tag:'skills-v1.0.0',commitSHA:sha,destination:path.join(root,'staged')});
 assert.equal(prepared.commitSHA,sha);assert.match(fs.readFileSync(path.join(prepared.path,'pgcat-replacement','SKILL.md'),'utf8'),/PINNED CONTENT/);
 const state=path.join(root,'state');fs.mkdirSync(state);const loader=new DefaultResourceLoader({cwd:state,agentDir:state,settingsManager:SettingsManager.inMemory(),noExtensions:true,noPromptTemplates:true,noThemes:true,noContextFiles:true,additionalSkillPaths:[prepared.path]});await loader.reload();assert.ok(loader.getSkills().skills.some(s=>s.name==='pgcat-replacement'));
 const binding={operation:'db-proxy.replace-nodes',skillsCommit:sha};
 const selected=await createOperationResourceLoader({sdk:{DefaultResourceLoader,SettingsManager},stateRoot:state,binding,skills:prepared});assert.deepEqual(selected.getSkills().skills.map(s=>s.name),['pgcat-replacement']);
 await assert.rejects(createOperationResourceLoader({sdk:{DefaultResourceLoader,SettingsManager},stateRoot:state,binding:{...binding,skillsCommit:'f'.repeat(40)},skills:prepared}),/InvalidSkillsRelease/);
 await assert.rejects(prepareSkills({repository:repo,tag:'skills-v1.0.0',commitSHA:'a'.repeat(40),destination:path.join(root,'wrong')}),/InvalidSkillsRelease/);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
