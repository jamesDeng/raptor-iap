import test from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';
test('progress is sequenced and rejects unknown or sensitive payloads; terminal is atomic',async()=>{const {ProgressWriter,writeTerminal}=await import('./progress.mjs');const root=fs.mkdtempSync(path.join(os.tmpdir(),'live-progress-'));try{const file=path.join(root,'progress.jsonl');const writer=new ProgressWriter(file);writer.append({kind:'status',outcome:'started'});writer.append({kind:'tool_start',tool:'mcp__infra__deployments_list',outcome:'started'});assert.throws(()=>writer.append({kind:'tool_start',tool:'bash',outcome:'started'}));assert.throws(()=>writer.append({kind:'progress',outcome:'running',secret:'sensitive'}));const rows=fs.readFileSync(file,'utf8').trim().split('\n').map(JSON.parse);assert.deepEqual(rows.map(x=>x.runtimeSequence),[1,2]);assert.ok(rows.every(x=>x.occurredAt));writeTerminal(path.join(root,'result.json'),{passed:false,error:'NeedsSignIn'});assert.deepEqual(JSON.parse(fs.readFileSync(path.join(root,'result.json'),'utf8')),{passed:false,error:'NeedsSignIn'});assert.equal(fs.existsSync(path.join(root,'result.json.tmp')),false)}finally{fs.rmSync(root,{recursive:true,force:true})}});
test('public progress text is bounded and excluded from tool/status envelopes',async()=>{
 const {ProgressWriter}=await import('./progress.mjs');const root=fs.mkdtempSync(path.join(os.tmpdir(),'public-progress-'));
 try{const writer=new ProgressWriter(path.join(root,'events'));const row=writer.append({kind:'progress',outcome:'running',summary:'正在检查部署状态。'});assert.equal(row.summary,'正在检查部署状态。');
 for(const event of [{kind:'progress',outcome:'running',summary:' '},{kind:'progress',outcome:'running',summary:'字'.repeat(700)},{kind:'status',outcome:'started',summary:'bad'},{kind:'progress',outcome:'running',summary:42}])assert.throws(()=>writer.append(event));
 }finally{fs.rmSync(root,{recursive:true,force:true})}
});
test('only public pre-tool assistant text becomes progress; reasoning and final answers are excluded',async()=>{
 const mod=await import('./progress.mjs');assert.equal(typeof mod.publicProgressFromMessage,'function');
 assert.equal(mod.publicProgressFromMessage({role:'assistant',stopReason:'toolUse',content:[{type:'thinking',thinking:'private reasoning'},{type:'text',text:'I will check the deployment.'},{type:'toolCall',name:'tool'}]}),'I will check the deployment.');
 for(const stopReason of ['stop','error','aborted'])assert.equal(mod.publicProgressFromMessage({role:'assistant',stopReason,content:[{type:'text',text:'Final answer'}]}),'');
 assert.equal(mod.publicProgressFromMessage({role:'assistant',stopReason:'toolUse',content:[{type:'text',text:'字'.repeat(1000)}]}),'');
 const boundary='x'.repeat(490)+'fixture-sensitive-value';
 assert.equal(mod.publicProgressFromMessage({role:'assistant',stopReason:'toolUse',content:[{type:'text',text:boundary}]}),boundary);
});
test('replacement progress catalog is explicit and does not broaden default question tools',async()=>{
 const {ProgressWriter}=await import('./progress.mjs');const root=fs.mkdtempSync(path.join(os.tmpdir(),'operation-progress-'));
 try{const event={kind:'tool_start',tool:'mcp__raptor__approval_request',outcome:'started'};const question=new ProgressWriter(path.join(root,'question'));assert.throws(()=>question.append(event));const replacement=new ProgressWriter(path.join(root,'replacement'),['mcp__raptor__approval_request']);replacement.append(event);assert.throws(()=>replacement.append({...event,tool:'bash'}));}finally{fs.rmSync(root,{recursive:true,force:true})}
});
