import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';
import {createCheckpoint,restoreCheckpoint} from '../../components/agent-harness/checkpoint.mjs';
test('immutable checkpoint roundtrip and collision preserve original bytes',async()=>{
 const tmp=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'checkpoint-')));try{
 const root=path.join(tmp,'live'),mount=path.join(tmp,'mount');fs.mkdirSync(root);fs.mkdirSync(mount);fs.mkdirSync(path.join(root,'sessions'));fs.writeFileSync(path.join(root,'auth.json'),JSON.stringify({openai:{type:'oauth',access:'synthetic',refresh:'synthetic',expires:1,clientId:'preserve'}}));
 const d=await createCheckpoint({stateRoot:root,mountRoot:mount,generation:'11111111-1111-4111-8111-111111111111'});const before=fs.readFileSync(path.join(mount,'lifecycle/11111111-1111-4111-8111-111111111111.tgz'));
 await assert.rejects(createCheckpoint({stateRoot:root,mountRoot:mount,generation:'11111111-1111-4111-8111-111111111111'}));assert.deepEqual(fs.readFileSync(path.join(mount,'lifecycle/11111111-1111-4111-8111-111111111111.tgz')),before);
 await restoreCheckpoint({reference:d,stateRoot:path.join(tmp,'restored'),mountRoot:mount});assert.equal(JSON.parse(fs.readFileSync(path.join(tmp,'restored/auth.json'))).openai.clientId,'preserve');
 fs.writeFileSync(path.join(mount,'lifecycle/11111111-1111-4111-8111-111111111111.tgz'),'corrupt');await assert.rejects(restoreCheckpoint({reference:d,stateRoot:path.join(tmp,'bad'),mountRoot:mount}));assert.equal(fs.existsSync(path.join(tmp,'bad')),false);
 }finally{fs.rmSync(tmp,{recursive:true,force:true});}
});
test('failed OSS readback returns no checkpoint descriptor',async()=>{
 const tmp=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'readback-'))),originalRead=fs.readFileSync;
 try{
  const root=path.join(tmp,'live'),mount=path.join(tmp,'mount');fs.mkdirSync(root);fs.mkdirSync(mount);fs.mkdirSync(path.join(root,'sessions'));
  fs.writeFileSync(path.join(root,'auth.json'),JSON.stringify({openai:{type:'oauth',access:'synthetic',refresh:'synthetic',expires:1}}));
  fs.readFileSync=(p,...args)=>String(p).startsWith(path.join(mount,'lifecycle'))&&String(p).endsWith('.tgz')?Buffer.from('corrupt-readback'):originalRead(p,...args);
  await assert.rejects(createCheckpoint({stateRoot:root,mountRoot:mount,generation:'22222222-2222-4222-8222-222222222222'}),/CheckpointReadbackFailed/);
 }finally{fs.readFileSync=originalRead;fs.rmSync(tmp,{recursive:true,force:true});}
});
