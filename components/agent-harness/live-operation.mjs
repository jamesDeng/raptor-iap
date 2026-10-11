import fs from 'node:fs';import path from 'node:path';
// Only a previously verified release may contribute instructions. Authentication
// state is not a skill directory, and ambient/user skill discovery stays disabled.
export async function createOperationResourceLoader({sdk,stateRoot,binding,skills,settingsManager,extensionFactories=[],systemPrompt=''}){
 if(binding?.operation!=='db-proxy.replace-nodes'||!skills||skills.commitSHA!==binding.skillsCommit||!/^[a-f0-9]{40}$/.test(skills.commitSHA)||!/^skills-v[0-9]+\.[0-9]+\.[0-9]+$/.test(skills.tag)||typeof skills.path!=='string')throw Error('InvalidSkillsRelease');
 const root=path.resolve(stateRoot),skillRoot=path.resolve(skills.path),relative=path.relative(root,skillRoot);
 if(!relative||(!relative.startsWith('..'+path.sep)&&relative!=='..'&&!path.isAbsolute(relative)))throw Error('InvalidSkillsRelease');
 const selected=path.join(skillRoot,'pgcat-replacement');
 for(let cursor=selected;;cursor=path.dirname(cursor)){if(fs.existsSync(cursor)&&fs.lstatSync(cursor).isSymbolicLink())throw Error('InvalidSkillsRelease');if(path.dirname(cursor)===cursor)break;}
 if(!fs.existsSync(path.join(selected,'SKILL.md')))throw Error('InvalidSkillsRelease');
 const loader=new sdk.DefaultResourceLoader({cwd:root,agentDir:root,settingsManager:settingsManager??sdk.SettingsManager.inMemory({compaction:{enabled:false}}),noExtensions:true,noSkills:true,noPromptTemplates:true,noThemes:true,noContextFiles:true,additionalSkillPaths:[selected],extensionFactories,systemPromptOverride:()=>systemPrompt});
 await loader.reload();if(loader.getSkills().skills.length!==1||loader.getSkills().skills[0].name!=='pgcat-replacement')throw Error('InvalidSkillsRelease');return loader;
}
