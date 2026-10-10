import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {SessionManager} from '@earendil-works/pi-coding-agent';
import {openOperationSession,operationSessionReference} from './operation-session.mjs';
const binding={requestId:'request-a',definitionSha256:'a'.repeat(64),skillsCommit:'b'.repeat(40),model:'gpt-5.6-luna'};
test('pinned Pi session reopens only matching request history',()=>{
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'pi-session-')));
 try {
  const first=openOperationSession({sdk:{SessionManager},stateRoot:root,binding});
  assert.equal(first.getEntries().length,0);
  first.appendMessage({role:'user',content:'request-a-only',timestamp:Date.now()});
  first.appendMessage({role:'toolResult',toolCallId:'read-1',toolName:'mcp__infra__deployment_status_get',content:[{type:'text',text:'scoped observation'}],isError:false,timestamp:Date.now()});
  const ref=operationSessionReference(first,binding,root);
  const resumed=openOperationSession({sdk:{SessionManager},stateRoot:root,binding,sessionReference:ref});
  assert.equal(resumed.getSessionId(),first.getSessionId());
  assert.ok(resumed.getEntries().some(e=>e.type==='message'&&e.message.role==='toolResult'&&e.message.toolCallId==='read-1'));
  assert.ok(resumed.getEntries().some(e=>e.type==='message'&&e.message.content==='request-a-only'));
  for(const changed of [{...binding,requestId:'request-b'},{...binding,definitionSha256:'c'.repeat(64)},{...binding,skillsCommit:'d'.repeat(40)}])assert.throws(()=>openOperationSession({sdk:{SessionManager},stateRoot:root,binding:changed,sessionReference:ref}),/InvalidSessionReference/);
  const second=openOperationSession({sdk:{SessionManager},stateRoot:root,binding:{...binding,requestId:'request-b'}});
  assert.notEqual(second.getSessionId(),first.getSessionId());assert.equal(second.getEntries().length,0);
  assert.throws(()=>openOperationSession({sdk:{SessionManager},stateRoot:root,binding,sessionReference:{...ref,file:'../auth.json'}}),/InvalidSessionReference/);
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('tampered session content refuses resume',()=>{
 const root=fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(),'pi-session-')));
 try{const manager=openOperationSession({sdk:{SessionManager},stateRoot:root,binding});manager.appendMessage({role:'user',content:'hello',timestamp:Date.now()});const ref=operationSessionReference(manager,binding,root);fs.appendFileSync(manager.getSessionFile(),'{}\n');assert.throws(()=>openOperationSession({sdk:{SessionManager},stateRoot:root,binding,sessionReference:ref}),/InvalidSessionReference/);}finally{fs.rmSync(root,{recursive:true,force:true});}
});
