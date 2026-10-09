import test from 'node:test';
import assert from 'node:assert/strict';
import {JSDOM} from 'jsdom';
const wait=()=>new Promise(resolve=>setTimeout(resolve,40));
test('actual T3 rows expand, stay expanded on refresh, and render untrusted text literally',async()=>{
 const dom=new JSDOM('<div id="root"></div>',{url:'http://localhost/'});
 Object.assign(globalThis,{window:dom.window,document:dom.window.document,HTMLElement:dom.window.HTMLElement});
 const ui=await import('../../web/poc/worklog.js').catch(e=>e.code==='ERR_MODULE_NOT_FOUND'?{}:Promise.reject(e));
 assert.equal(typeof ui.updateWorklog,'function');
 const host=document.getElementById('root');
 const snapshot={request:{id:'one'},execution:{status:'running',result:{answer:'<img src=x onerror=alert(1)>'},cleanupStatus:'pending'}};
 const event={requestId:'one',sequence:1,kind:'tool_result',summary:'<script>evil()</script>',details:{tool:'deployment_status_get',result:'1/1 ready'},occurredAt:'2026-10-09T00:00:00Z'};
 ui.updateWorklog(host,{snapshot,events:[event]});await wait();
 assert.equal(host.querySelector('input,textarea,form'),null);
 const header=host.querySelector('button[aria-controls="worklog-events"]');assert.ok(header);header.click();await wait();
 const row=host.querySelector('button[data-event-sequence="1"]');assert.ok(row);assert.equal(row.querySelector('time').getAttribute('datetime'),'2026-10-09T00:00:00Z');row.click();await wait();
 assert.match(host.textContent,/1\/1 ready/);assert.equal(host.querySelector('script,img'),null);
 ui.updateWorklog(host,{snapshot:{...snapshot,executionAvailable:false},events:[event]});await wait();
 assert.equal(host.querySelector('button[data-event-sequence="1"]').getAttribute('aria-expanded'),'true');
 assert.match(host.textContent,/saved|unavailable/i);assert.match(host.textContent,/pending/i);
 for(const status of ['failed','cancelled']){
  ui.updateWorklog(host,{events:[{...event,details:{status}}]});await wait();
  assert.equal(host.querySelector('.worklog-icon').textContent,'◇');
  assert.ok(!host.querySelector('.worklog-icon').textContent.includes('✓'));
 }
 ui.resetWorklog(host);ui.updateWorklog(host,{snapshot:{request:{id:'two'},execution:{}},events:[]});await wait();
 assert.ok(!host.textContent.includes('1/1 ready'));assert.ok(!host.textContent.includes('evil'));
 ui.resetWorklog(host);dom.window.close();delete globalThis.window;delete globalThis.document;delete globalThis.HTMLElement;
});
