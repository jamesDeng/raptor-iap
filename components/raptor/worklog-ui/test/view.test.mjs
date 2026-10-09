import test from 'node:test';
import assert from 'node:assert/strict';
import {JSDOM} from 'jsdom';
const wait=()=>new Promise(resolve=>setTimeout(resolve,60));
// Catches accidental composer/actions, collapsed whole transcript, lost expansion and XSS.
test('readonly transcript interleaves narration and tool groups, retaining expansion on live updates',async()=>{
 const dom=new JSDOM('<div id="root"></div>',{url:'http://localhost/',pretendToBeVisual:true});
 Object.assign(globalThis,{window:dom.window,document:dom.window.document,HTMLElement:dom.window.HTMLElement,MutationObserver:dom.window.MutationObserver,requestAnimationFrame:dom.window.requestAnimationFrame.bind(dom.window),cancelAnimationFrame:dom.window.cancelAnimationFrame.bind(dom.window),ResizeObserver:class{observe(){} unobserve(){} disconnect(){}}});
 dom.window.HTMLElement.prototype.scrollTo=function(options){this.scrollTop=options.top??this.scrollTop;};
 const ui=await import('../../web/poc/worklog.js');
 const host=document.getElementById('root');
 const snapshot={request:{id:'one'},execution:{status:'running',checkpointStatus:'pending',cleanupStatus:'pending'}};
 const base={requestId:'one',attemptId:'a',evidenceMode:'live',occurredAt:'2026-10-09T00:00:00Z'};
 const events=[{...base,sequence:1,kind:'progress',summary:'<script>Inspect request</script>'},{...base,sequence:2,kind:'tool_start',summary:'tool_start: started',details:{tool:'deployment_status_get',status:'started'}}];
 try{
  ui.updateWorklog(host,{snapshot,events});await wait();
  assert.equal(host.querySelector('input,textarea,form,[contenteditable="true"]'),null);
  assert.match(host.querySelector('.progress-narration')?.textContent??'',/Inspect request/);
  assert.equal(host.querySelector('script,img'),null);
  const group=host.querySelector('details.progress-activity');assert.ok(group);group.querySelector('summary').click();
  const tool=host.querySelector('details.progress-tool');assert.ok(tool);tool.querySelector('summary').click();await wait();
  assert.match(tool.textContent,/Running/);assert.match(tool.textContent,/live/);
  ui.updateWorklog(host,{snapshot,events:[...events,{...base,sequence:3,kind:'tool_result',summary:'tool_result: failed',details:{tool:'deployment_status_get',status:'failed',result:'<img src=x onerror=alert(1)>'}},{...base,sequence:4,kind:'approval_wait',summary:'Waiting for permission approval'}]});await wait();
  assert.equal(host.querySelector('details.progress-activity').open,true);assert.equal(host.querySelector('details.progress-tool').open,true);
  assert.match(host.querySelector('.progress-tool').textContent,/Failed/);assert.ok(!host.querySelector('.progress-tool summary').textContent.includes('✓'));
  assert.match(host.textContent,/Waiting for permission approval/);assert.equal(host.querySelector('script,img'),null);
  ui.updateWorklog(host,{snapshot:{...snapshot,executionAvailable:false},unavailable:true});await wait();assert.match(host.textContent,/saved history/);
  ui.updateWorklog(host,{snapshot:{request:{id:'two'},execution:{}},events:[],unavailable:false});await wait();
  assert.ok(!host.textContent.includes('Inspect request'));assert.ok(!host.querySelector('.progress-tool'));
 }finally{ui.resetWorklog(host);dom.window.close();delete globalThis.window;delete globalThis.document;delete globalThis.HTMLElement;delete globalThis.MutationObserver;delete globalThis.ResizeObserver;delete globalThis.requestAnimationFrame;delete globalThis.cancelAnimationFrame;}
});
