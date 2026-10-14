import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {checkoutRepositories} from './checkout-repositories.mjs';

test('checkout happens once per repository after the workspace exists and verifies the pinned commit',async()=>{
 const workspace=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'raptor-checkout-')));
 try{
  const root=path.join(workspace,'repositories','repo-0');
  const row={url:'https://github.com/example/infra.git',baseBranch:'main',baseCommit:'a'.repeat(40),directory:'.',path:root};
  const calls=[];
  await checkoutRepositories([row,{...row,directory:'kubernetes',path:path.join(root,'kubernetes')}],{workspace,git:async(cwd,args)=>{
   assert.equal(cwd,root);assert.equal(fs.existsSync(root),true);
   calls.push(args);
   return args[0]==='rev-parse'?'a'.repeat(40)+'\n':'';
  }});
  assert.deepEqual(calls.map(args=>args[0]),['init','remote','fetch','rev-parse','checkout','config','config']);
  assert.deepEqual(calls[2],['fetch','--depth=1','origin','refs/heads/main']);
  assert.deepEqual(calls[5],['config','--local','user.name','Raptor Sandbox Agent']);
  assert.deepEqual(calls[6],['config','--local','user.email','raptor-sandbox@localhost']);
 }finally{fs.rmSync(workspace,{recursive:true,force:true});}
});

test('checkout refuses a moved branch and malformed or conflicting paths',async()=>{
 const workspace=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'raptor-checkout-')));
 try{
  const root=path.join(workspace,'repositories','repo-0');
  const row={url:'https://github.com/example/infra.git',baseBranch:'main',baseCommit:'a'.repeat(40),directory:'.',path:root};
  await assert.rejects(checkoutRepositories([row],{workspace,git:async()=> 'b'.repeat(40)}),/RepositoryMoved/);
  await assert.rejects(checkoutRepositories([{...row,directory:'../repo-0',path:root}],{workspace,git:async()=>''}),/InvalidRepositories/);
  await assert.rejects(checkoutRepositories([row,{...row,url:'https://github.com/example/other.git'}],{workspace,git:async()=>''}),/InvalidRepositories/);
 }finally{fs.rmSync(workspace,{recursive:true,force:true});}
});

test('checkout bounds concurrent fetches for the supported 32 repository rows',async()=>{
 const workspace=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'raptor-checkout-')));
 try{
  const rows=Array.from({length:32},(_,i)=>({url:`https://github.com/example/repo-${i}.git`,baseBranch:'main',baseCommit:'a'.repeat(40),directory:'.',path:path.join(workspace,'repositories',`repo-${i}`)}));
  let active=0,maximum=0,fetches=0;
  await checkoutRepositories(rows,{workspace,git:async(_cwd,args)=>{
   if(args[0]==='fetch'){active++;maximum=Math.max(maximum,active);fetches++;await new Promise(resolve=>setTimeout(resolve,2));active--;}
   return args[0]==='rev-parse'?'a'.repeat(40):'';
  }});
  assert.equal(fetches,32);assert.equal(maximum,8);
 }finally{fs.rmSync(workspace,{recursive:true,force:true});}
});
