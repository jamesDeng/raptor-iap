import test from 'node:test';
import assert from 'node:assert/strict';
import * as app from './app.js';
test('new request URL restores the chosen resource and environment, never submitted parameters',()=>{
 assert.equal(typeof app.pageRoute,'function');
 assert.deepEqual(app.pageRoute('#new-request=app%201&env=rdev.ali'),{page:'new-request',id:'app 1',environment:'rdev.ali',tab:'overview',mode:'agent'});
 assert.equal(app.pageRoute('#request=run-1').page,'request');
 assert.equal(app.pageRoute('#object=app-1&tab=deployments').tab,'deployments');
 assert.equal(app.pageRoute('').page,'catalog');
});
test('request operation choices are restricted to the selected resource kind',()=>{
 assert.equal(typeof app.operationChoices,'function');
 const schemas={'application.question':{'x-object-kind':'application'},'database.create':{'x-object-kind':'database'},'proxy.create':{'x-object-kind':'db-proxy'}};
 assert.deepEqual(app.operationChoices(schemas,'application'),['application.question']);
 assert.deepEqual(app.operationChoices(schemas,'database'),['database.create']);
 assert.deepEqual(app.operationChoices(schemas,'other'),[]);
});

import {readFileSync} from 'node:fs';
const source=readFileSync(new URL('./app.js',import.meta.url),'utf8');
test('signing in resumes the requested page instead of forcing the catalog',async()=>{
 const body=source.match(/\$\('login'\)\.addEventListener\('submit',async e=>\{([\s\S]*?)\n \$\('logout'\)/)[1].replace(/\}\);$/,'');
 let destination='login';
 const handler=new Function('api','$','refreshCatalog','restoreRoute','openCatalog','FormData','showError',`let csrf='';return async e=>{${body}}`)(async()=>({csrf:'synthetic',user:{username:'fixture'}}),()=>({}),async()=>{},async()=>{destination='new-request'},()=>{destination='catalog'},function(){return []},e=>{throw e});
 await handler({preventDefault(){},target:{}});assert.equal(destination,'new-request');
});
test('a submitted request does not redirect after the user navigates away',async()=>{
 const body=source.match(/async function submit\(body\)\{([\s\S]*?)\n async function loadRequest/)[1];
 let resolve,loaded=null;
 const controller=new Function('api','loadRequest','crypto',`let pendingSubmission=null,viewEpoch=0;async function submit(body){${body};return {submit,navigate(){viewEpoch++}}`)(()=>new Promise(r=>resolve=r),id=>{loaded=id},crypto);
 const submission=controller.submit({type:'agent'});controller.navigate();resolve({id:'created-request'});await submission;assert.equal(loaded,null);
});

test('restoring omitted route defaults replaces the current history entry',async()=>{
 const writer=source.match(/ function setRoute\(values,replace=false\)\{[^\n]+/)[0];
 const restore=source.match(/ async function restoreRoute\(\)\{[\s\S]*?\n \}\n async function showDetail/)[0].replace(/\n async function showDetail$/,'');
 const entries=['catalog=application','new-request=app-1&env=rdev.ali'];
 const location={get hash(){return '#'+entries.at(-1)},set hash(value){entries.push(value.replace(/^#/,''))}};
 const history={replaceState(_state,_title,url){entries[entries.length-1]=url.replace(/^#/,'')}};
 const environment={value:'rdev.ali'};
 const restorePage=new Function('pageRoute','location','history','objects','envs','$',`let activeRoute='';${writer}${restore}
 function openNewRequest(object,mode,replace){setRoute({'new-request':object.id,env:environment.value,mode},replace)}
 const environment=$('environment');return restoreRoute;`)(app.pageRoute,location,history,[{id:'app-1'}],[{code:'rdev.ali'}],()=>environment);
 await restorePage();assert.deepEqual(entries,['catalog=application','new-request=app-1&env=rdev.ali&mode=agent']);
});

test('opening another request invalidates the pending creation redirect',async()=>{
 const submitBody=source.match(/async function submit\(body\)\{([\s\S]*?)\n async function loadRequest/)[1];
 const loadBody=source.match(/async function loadRequest\(id,replace=false\)\{([\s\S]*?)\n async function refreshRequest/)[1];
 let resolve;
 const controller=new Function('api','crypto','setRoute','panel','refreshRequest','progress',`let pendingSubmission=null,viewEpoch=0,requestEpoch=0,requestId=null,requestTab=null,requestView=null;async function submit(body){${submitBody}async function loadRequest(id,replace=false){${loadBody}return {submit,loadRequest,selected:()=>requestId};`)(()=>new Promise(r=>resolve=r),crypto,()=>{},()=>{},async()=>{}, {close(){},async select(){}});
 const pending=controller.submit({type:'agent'});await controller.loadRequest('other-request');resolve({id:'created-request'});await pending;assert.equal(controller.selected(),'other-request');
});
test('logging out invalidates the pending creation redirect',async()=>{
 const submitBody=source.match(/async function submit\(body\)\{([\s\S]*?)\n async function loadRequest/)[1];
 const logoutBody=source.match(/\$\('logout'\)\.onclick=async\(\)=>\{([\s\S]*?)\n \$\('catalog-nav'\)/)[1].replace(/\};$/,'');
 let resolve,destination='new-request';
 const controller=new Function('api','crypto','loadRequest','panel','progress','showError',`let csrf='fixture',pendingSubmission=null,viewEpoch=0;async function submit(body){${submitBody}async function logout(){${logoutBody}}return {submit,logout};`)(path=>path==='/requests'?new Promise(r=>resolve=r):Promise.resolve(),crypto,()=>{destination='created-request'},name=>{destination=name},{close(){}},e=>{throw e});
 const pending=controller.submit({type:'agent'});await controller.logout();resolve({id:'created-request'});await pending;assert.equal(destination,'login-panel');
});
