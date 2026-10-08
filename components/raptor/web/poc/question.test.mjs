import test from 'node:test';
import assert from 'node:assert/strict';
import {validateParameters,renderOperationForm,renderProgress} from './app.js';
const schema={required:['question'],properties:{question:{type:'string',minLength:1,maxLength:2000,maxBytes:8192,'x-multiline':true}}};
test('question uses Unicode code points and rejects whitespace/oversize',()=>{assert.deepEqual(validateParameters(schema,{question:'😀'.repeat(2000)}),[]);for(const question of ['  ','a'.repeat(2001)])assert.ok(validateParameters(schema,{question}).length);});
function element(tag){return {tag,children:[],textContent:'',append(...children){this.children.push(...children)},replaceChildren(...children){this.children=children},addEventListener(){}}}
function dom(){const hosts=new Map();global.document={createElement:element,getElementById(id){if(!hosts.has(id))hosts.set(id,element('div'));return hosts.get(id)}};return hosts}
test('question renders as a multiline field',()=>{const hosts=dom();try{renderOperationForm(schema);assert.equal(hosts.get('operation-fields').children[0].children[0].tag,'textarea')}finally{delete global.document}});
test('missing evidence is unverified rather than simulated',()=>{const hosts=dom();try{renderProgress([{kind:'progress',summary:'unknown'}]);assert.match(hosts.get('progress').children[0].children[0].textContent,/unverified/)}finally{delete global.document}});

test('answer stays plain text and cleanup is independent',async()=>{const hosts=dom();try{const app=await import('./app.js');assert.equal(typeof app.renderExecutionResult,'function');app.renderExecutionResult({request:{definition:{model:'gpt-5.6-luna',operations:[{name:'application.question'}],skills:{tag:'selected'}}},executionAvailable:false,lastSuccessfulSyncAt:'2026-10-07T00:00:00Z',execution:{runtimeMode:'live',result:{answer:'<script>not executed</script>',actualModel:'gpt-5.6-luna'},checkpointStatus:'failed',cleanupStatus:'unknown'}});assert.equal(hosts.get('answer').textContent,'<script>not executed</script>');assert.match(hosts.get('lifecycle').textContent,/cleanup: unknown/i);assert.match(hosts.get('answer-identity').textContent,/Last saved/)}finally{delete global.document}});

test('structured cleanup and usage are readable on the request page',async()=>{const hosts=dom();try{const app=await import('./app.js');app.renderExecutionResult({execution:{stage:'finished',cleanupStatus:{sandboxAbsent:true,keyAbsent:true,accessRevoked:true},result:{answer:'healthy',usage:[{input:100,output:20,totalTokens:120},{input:50,output:10,totalTokens:60}]}}});assert.match(hosts.get('lifecycle').textContent,/cleanup: confirmed/i);assert.match(hosts.get('answer-identity').textContent,/150 input.*30 output.*180 total/i);assert.ok(!hosts.get('lifecycle').textContent.includes('[object Object]'));}finally{delete global.document}});

test('navigation follows the visible panel and resource tab',async()=>{
 const hosts=dom(),tabs=['overview','deployments'].map(tab=>({dataset:{tab},attributes:{},setAttribute(k,v){this.attributes[k]=v},removeAttribute(k){delete this.attributes[k]}}));
 document.querySelectorAll=()=>tabs;
 for(const id of ['catalog-nav','database-nav','proxy-nav','requests-nav'])Object.assign(document.getElementById(id),{attributes:{},setAttribute(k,v){this.attributes[k]=v},removeAttribute(k){delete this.attributes[k]}});
 try{
  const app=await import('./app.js');
  assert.equal(typeof app.updateNavigation,'function');
  app.updateNavigation('catalog-panel','deployments');
  assert.equal(hosts.get('catalog-nav').attributes['aria-current'],'page');
  assert.equal(tabs[1].attributes['aria-current'],'page');
  assert.equal(tabs[0].attributes['aria-current'],undefined);
  app.updateNavigation('request-panel','overview');
  assert.equal(hosts.get('requests-nav').attributes['aria-current'],'page');
  assert.equal(hosts.get('catalog-nav').attributes['aria-current'],undefined);
  app.updateNavigation('login-panel','overview');
  assert.equal(hosts.get('requests-nav').attributes['aria-current'],undefined);
 }finally{delete global.document}
});

test('progress shows the recorded event timestamp without inventing a missing one',()=>{
 const hosts=dom();try{
  renderProgress([{kind:'progress',summary:'saved',occurredAt:'2026-10-08T01:00:00Z'}]);
  const time=hosts.get('progress').children[0].children.find(e=>e.tag==='time');
  assert.ok(time);assert.equal(time.dateTime,'2026-10-08T01:00:00Z');
  renderProgress([{kind:'progress',summary:'saved'}]);assert.equal(hosts.get('progress').children[0].children.some(e=>e.tag==='time'),false);
 }finally{delete global.document}
});
