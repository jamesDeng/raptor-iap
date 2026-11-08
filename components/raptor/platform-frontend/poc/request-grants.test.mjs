import test from 'node:test';
import assert from 'node:assert/strict';
import {renderRequestGrants,requestTabState} from './app.js';

test('approval tab lists every granted capability and its exact bound object',()=>{
 const grants=[{id:'grant',state:'pending',envCode:'rdev.ali',capabilities:[
  {capability:'db-proxy.scale-in',target:{kind:'db-proxy',code:'DP00001'},constraints:{desiredCapacity:2}},
  {capability:'application.restart',target:{kind:'application',code:'A00003'},constraints:{}},
 ]}];
 assert.deepEqual(requestTabState({grants},'approvals'),{selected:'approvals',approvals:true});
 const previous=globalThis.document;
 const make=(tag)=>({tag,children:[],append(...children){this.children.push(...children);},addEventListener(_name,handler){this.click=handler;}});
 globalThis.document={createElement:make};
 try{
  const host=make('div'),decisions=[];
  renderRequestGrants(host,grants,(id,decision)=>decisions.push({id,decision}));
  const flat=node=>[node,...node.children.flatMap(flat)];
  const nodes=flat(host);
  for(const value of ['db-proxy.scale-in','DP00001','desiredCapacity','application.restart','A00003']){
   assert.ok(nodes.some(node=>node.textContent?.includes(value)),value);
  }
  nodes.find(node=>node.textContent==='Approve bundle').click();
  assert.deepEqual(decisions,[{id:'grant',decision:'approve'}]);
 }finally{globalThis.document=previous;}
});
