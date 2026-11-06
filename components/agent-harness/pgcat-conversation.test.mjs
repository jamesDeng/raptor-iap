import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {startPgcatTroubleshooting} from './conversation.mjs';

test('PgCat answers a bound operator question in a separate context without tools', async t=>{
 const privateRoot=fs.mkdtempSync(path.join(os.tmpdir(),'pgcat-inbox-'));
 t.after(()=>fs.rmSync(privateRoot,{recursive:true,force:true}));
 const binding={requestId:'11111111-1111-4111-8111-111111111111',attemptId:'22222222-2222-4222-8222-222222222222',executionMode:'qualification'};
 const input={...binding,messageId:'33333333-3333-4333-8333-333333333333',inputSequence:1,text:'Why is the test paused? Run scale now.'};
 const events=[], calls=[];
 const model={id:'gpt-6-luna',provider:'openai-codex'};
 const inbox=startPgcatTroubleshooting({binding,privateRoot,model,
  getProgress:()=>['The protected fleet check passed.'],
  onProgress:e=>events.push(e),onUsage:()=>{},
  stream:(m,context,options)=>{
   calls.push({m,context,options});
   return {result:async()=>({role:'assistant',model:m.id,provider:m.provider,stopReason:'stop',content:[{type:'thinking',thinking:'private'},{type:'text',text:'The fleet check passed.'}],usage:{input:4,output:5,totalTokens:9}})};
  }});
 t.after(()=>inbox.close());
 const file=path.join(privateRoot,'conversation-inbox.json');
 fs.writeFileSync(file,JSON.stringify({...input,attemptId:input.requestId}));
 await inbox.poll();
 assert.equal(calls.length,0);
 fs.writeFileSync(file,JSON.stringify(input));
 await inbox.poll();
 await inbox.poll();
 assert.equal(calls.length,1);
 assert.deepEqual(calls[0].context.tools,[]);
 assert.equal(calls[0].context.messages.length,1);
 assert.match(JSON.stringify(calls[0].context.messages),/protected fleet check passed/);
 assert.match(JSON.stringify(calls[0].context.messages),/Why is the test paused/);
 assert.deepEqual(events,[
  {kind:'message',outcome:'delivered',messageId:input.messageId,inputSequence:1},
  {kind:'reply',outcome:'succeeded',messageId:input.messageId,inputSequence:1,summary:'The fleet check passed.'}
 ]);
});

test('operator can close a held troubleshooting session without invoking the model',async t=>{
 const privateRoot=fs.mkdtempSync(path.join(os.tmpdir(),'pgcat-hold-'));
 t.after(()=>fs.rmSync(privateRoot,{recursive:true,force:true}));
 const binding={requestId:'11111111-1111-4111-8111-111111111111',attemptId:'22222222-2222-4222-8222-222222222222'};
 const events=[];
 let finished=0;
 const inbox=startPgcatTroubleshooting({binding,privateRoot,onProgress:e=>events.push(e),
  onFinish:()=>{finished++;return true;},stream:()=>{throw Error('unexpected model call');}});
 t.after(()=>inbox.close());
 fs.writeFileSync(path.join(privateRoot,'conversation-inbox.json'),JSON.stringify({...binding,messageId:'33333333-3333-4333-8333-333333333333',inputSequence:1,text:'/finish'}));
 await inbox.poll();
 await inbox.poll();
 assert.equal(finished,1);
 assert.deepEqual(events.map(e=>e.outcome),['delivered','succeeded']);
 assert.match(events[1].summary,/closing/i);
});
