import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import * as app from './app.js';
const source=readFileSync(new URL('./app.js',import.meta.url),'utf8');
test('database navigation clears restart errors and hides restart UI',()=>{
 const nodes=Object.fromEntries(['message','restart-basket-panel'].map(id=>[id,{hidden:false,textContent:'Choose deployments first'}]));
 const doc={getElementById:id=>nodes[id]};
 app.setPageContext(doc,'catalog-panel','database','overview');
 assert.equal(nodes.message.textContent,'');assert.equal(nodes['restart-basket-panel'].hidden,true);
 app.setPageContext(doc,'object-detail','database','deployments');assert.equal(nodes['restart-basket-panel'].hidden,true);
 app.setPageContext(doc,'object-detail','db-proxy','deployments');assert.equal(nodes['restart-basket-panel'].hidden,true);
 app.setPageContext(doc,'object-detail','application','overview');assert.equal(nodes['restart-basket-panel'].hidden,true);
 app.setPageContext(doc,'object-detail','application','deployments');assert.equal(nodes['restart-basket-panel'].hidden,false);
 for(const page of ['request-panel','new-request-panel']){app.setPageContext(doc,page,'application','overview');assert.equal(nodes['restart-basket-panel'].hidden,true);}
});
test('page context is refreshed both on page navigation and detail-tab changes',()=>{
 assert.match(source,/function panel\(name\)\{setPageContext\(document,name,catalogKind,currentTab\)/);
 assert.match(source,/async function showDetail\(\)\{[^\n]*setPageContext\(document,'object-detail',object.kind,currentTab\)/);
});
test('a delayed operation failure cannot put its error on a different page',async()=>{
 const previousFetch=globalThis.fetch,previousDocument=globalThis.document;
 const nodes=Object.fromEntries(['message','restart-basket-panel'].map(id=>[id,{hidden:false,textContent:''}]));
 globalThis.document={getElementById:id=>nodes[id]};let release;
 globalThis.fetch=()=>new Promise(resolve=>release=resolve);
 try{
  app.setPageContext(document,'new-request-panel','application','overview');
  const pending=app.api('/requests',{method:'POST',body:{}}).catch(error=>app.showError(error));
  app.setPageContext(document,'catalog-panel','database','overview');
  release({ok:false,json:async()=>({error:{code:'old-request-failure'}})});await pending;
  assert.equal(nodes.message.textContent,'');
  const current=app.api('/objects').catch(error=>app.showError(error));
  release({ok:false,json:async()=>({error:{code:'current-page-failure'}})});await current;
  assert.equal(nodes.message.textContent,'current-page-failure');
 }finally{globalThis.fetch=previousFetch;globalThis.document=previousDocument;}
});
