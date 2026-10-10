import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';
import {readObservationConfig} from './observation-config.mjs';
test('private observer configuration rejects permissions links and path escape',()=>{const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'observer-config-')));try{const file=path.join(root,'observation.json');fs.writeFileSync(file,'{"version":1}',{mode:0o600});assert.equal(readObservationConfig(file,root).version,1);fs.chmodSync(file,0o644);assert.throws(()=>readObservationConfig(file,root),/InvalidObservation/);fs.chmodSync(file,0o600);const link=path.join(root,'link');fs.symlinkSync(file,link);assert.throws(()=>readObservationConfig(link,root),/InvalidObservation/);assert.throws(()=>readObservationConfig(file,root+'/other'),/InvalidObservation/);}finally{fs.rmSync(root,{recursive:true,force:true});}});

test('observer secrets are removed before checkpoint while session state remains',async()=>{
 const {removeObservationCredentials}=await import('./observation-config.mjs');
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'observer-cleanup-')));
 try{for(const name of ['observation.json','kubeconfig','attempt-job.json'])fs.writeFileSync(path.join(root,name),'private',{mode:0o600});fs.writeFileSync(path.join(root,'skills-bundle.json'),'public');
 removeObservationCredentials(root);
 for(const name of ['observation.json','kubeconfig','attempt-job.json'])assert.equal(fs.existsSync(path.join(root,name)),false);
 assert.equal(fs.existsSync(path.join(root,'skills-bundle.json')),true);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
