import test from 'node:test';
import assert from 'node:assert/strict';
import * as app from './app.js';

function element(tag){return {tag,children:[],dataset:{},textContent:'',append(...children){this.children.push(...children)},replaceChildren(...children){this.children=children}}}
function descendants(root,tag){return root.children.flatMap(child=>[...(child.tag===tag?[child]:[]),...descendants(child,tag)]);}
const attached={repository:'jamesdeng/raptor-iap',number:190,url:'https://github.com/jamesdeng/raptor-iap/pull/190',headSha:'aecfe72bcbcca4ed5b9ef893ee5872da174880b4',state:'success',checkedAt:'2026-10-14T08:00:00Z'};

test('pull request tab renders the attached PR link and Actions state',()=>{
 const previous=globalThis.document;globalThis.document={createElement:element};
 try{
  assert.equal(typeof app.renderPullRequests,'function');
  const host=element('div');app.renderPullRequests(host,{status:'ready',items:[attached]});
  const links=descendants(host,'a');assert.equal(links.length,1);
  assert.equal(links[0].href,'https://github.com/jamesdeng/raptor-iap/pull/190');
  assert.equal(links[0].textContent,'jamesdeng/raptor-iap #190');
  assert.match(descendants(host,'p').map(item=>item.textContent).join(' '),/Actions: Passed/);
  assert.equal(links[0].rel,'noopener noreferrer');
 }finally{globalThis.document=previous;}
});

test('pull request tab distinguishes no attachment from an unavailable read',()=>{
 const previous=globalThis.document;globalThis.document={createElement:element};
 try{
  assert.equal(typeof app.renderPullRequests,'function');
  const host=element('div');app.renderPullRequests(host,{status:'ready',items:[]});
  assert.match(descendants(host,'p')[0].textContent,/No pull requests attached/);
  app.renderPullRequests(host,{status:'error',items:[]});
  assert.match(descendants(host,'p')[0].textContent,/Could not load pull requests/);
 }finally{globalThis.document=previous;}
});

test('only repository change requests can select the PR tab',()=>{
 const change={request:{definition:{operations:[{name:'repository.change'}]}}};
 assert.equal(app.requestTabState(change,'pull-requests').selected,'pull-requests');
 assert.equal(app.requestTabState({},'pull-requests').selected,'overview');
});

test('an older PR read cannot replace the current request and errors remain visible',async()=>{
 const pending=new Map(),states=[];
 assert.equal(typeof app.createPullRequestController,'function');
 const controller=app.createPullRequestController({read:id=>new Promise((resolve,reject)=>pending.set(id,{resolve,reject})),onChange:(id,view)=>states.push([id,view.status,view.items])});
 const old=controller.load('old');const current=controller.load('current');
 pending.get('old').resolve([attached]);await old;
 assert.deepEqual(states.map(item=>item.slice(0,2)),[['old','loading'],['current','loading']]);
 pending.get('current').resolve([attached]);await current;
 assert.deepEqual(states.at(-1),['current','ready',[attached]]);
 const failure=controller.load('failed');pending.get('failed').reject(Error('unavailable'));await failure;
 assert.deepEqual(states.at(-1),['failed','error',[]]);
});
