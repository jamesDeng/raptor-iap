import test from 'node:test';
import assert from 'node:assert/strict';
import * as app from './app.js';

test('catalog lists isolate applications, databases and proxies',()=>{
 const objects=[{id:'app',kind:'application'},{id:'db',kind:'database'},{id:'proxy',kind:'db-proxy'}];
 assert.equal(typeof app.catalogObjects,'function');
 assert.deepEqual(app.catalogObjects(objects,'application').map(o=>o.id),['app']);
 assert.deepEqual(app.catalogObjects(objects,'database').map(o=>o.id),['db']);
 assert.deepEqual(app.catalogObjects(objects,'db-proxy').map(o=>o.id),['proxy']);
});

test('request defaults to results only when available and preserves manual tabs during polling',()=>{
 assert.equal(typeof app.requestTabState,'function');
 assert.deepEqual(app.requestTabState({}),{selected:'overview',approvals:false});
 const value={execution:{result:{answer:'Answer'}},approvals:[{state:'pending'}]};
 assert.deepEqual(app.requestTabState(value),{selected:'result',approvals:true});
 assert.equal(app.requestTabState(value,'progress').selected,'progress');
 assert.equal(app.requestTabState({targets:[{state:'completed'}]}).selected,'result');
 assert.equal(app.requestTabState({},'approvals').selected,'overview');
 assert.equal(app.requestTabState({},'result').selected,'result');
});

// Execute the page renderer with a minimal DOM, as in the page-loader contract tests.
// This catches accidentally persisting an automatic selection as a user preference.
import {readFileSync} from 'node:fs';
test('a delayed answer activates Result unless the user explicitly selected another tab',()=>{
 const source=readFileSync(new URL('./app.js',import.meta.url),'utf8');
 const body=source.match(/function renderRequestTabs\(\)\{([\s\S]*?)\n \}\n async function decision/)[1];
 const controls=['overview','result','progress','approvals'].map(tab=>({dataset:{requestTab:tab},attributes:{},setAttribute(k,v){this.attributes[k]=v},removeAttribute(k){delete this.attributes[k]}}));
 const views=controls.map(control=>({dataset:{requestView:control.dataset.requestTab}}));
 const doc={querySelectorAll:selector=>selector==='[data-request-tab]'?controls:views};
 const empty={};
 const controller=new Function('requestTabState','document','$',`let requestView={},requestTab=null;function renderRequestTabs(){${body}};return {render(value,manual){requestView=value;if(manual)requestTab=manual;renderRequestTabs();}}`)(app.requestTabState,doc,()=>empty);
 controller.render({});assert.equal(controls[0].attributes['aria-current'],'page');
 controller.render({execution:{result:{answer:'ready'}}});assert.equal(controls[1].attributes['aria-current'],'page');assert.equal(views[1].hidden,false);
 controller.render({},'progress');controller.render({execution:{result:{answer:'ready'}}});assert.equal(controls[2].attributes['aria-current'],'page');assert.equal(views[2].hidden,false);
});
