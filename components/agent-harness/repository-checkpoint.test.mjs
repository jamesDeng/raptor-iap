import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {createCheckpoint} from './checkpoint.mjs';

test('checkpoint archives state while excluding private Git credentials and repository worktrees',async()=>{
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'raptor-repository-checkpoint-')));
 try{
  const state=path.join(root,'workspace','raptor-state'),privateRoot=path.join(root,'private'),repository=path.join(root,'workspace','repositories','repo-0'),mount=path.join(root,'mount');
  fs.mkdirSync(path.join(state,'sessions'),{recursive:true});fs.mkdirSync(privateRoot,{recursive:true});fs.mkdirSync(repository,{recursive:true});
  const token='ghp_SyntheticCredentialMustNeverBeArchived';
  fs.writeFileSync(path.join(state,'auth.json'),JSON.stringify({openai:{type:'oauth',access:'synthetic-access',refresh:'synthetic-refresh',expires:9999999999999}}));
  fs.writeFileSync(path.join(state,'sessions','session.jsonl'),'safe session');
  fs.writeFileSync(path.join(privateRoot,'github-pat'),token);
  fs.writeFileSync(path.join(repository,'source.txt'),'repository worktree');
  const checkpoint=await createCheckpoint({stateRoot:state,mountRoot:mount,generation:'22222222-2222-4222-8222-222222222222',prefix:'auth'});
  const archive=path.join(mount,'lifecycle','22222222-2222-4222-8222-222222222222.tgz');
  const names=execFileSync('tar',['-tzf',archive],{encoding:'utf8'});
  assert.ok(checkpoint.bytes>0);
  assert.match(names,/sessions\/session\.jsonl/);
  assert.doesNotMatch(names,/github-pat|repositories|source\.txt/);
  const content=execFileSync('tar',['-xOzf',archive,'sessions/session.jsonl'],{encoding:'utf8'});
  assert.equal(content,'safe session');
  assert.doesNotMatch(content,new RegExp(token));
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
