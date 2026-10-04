import test from 'node:test';
import assert from 'node:assert/strict';

const task={task_id:'synthetic-task',question:'<script>private question</script>',model:'gpt-5.6-luna',snapshot:{name:'<img src=x>',version:1,provenance:'owner-entered'},snapshot_sha256:'b'.repeat(64),blocked:false,attempts:[{attempt_id:'attempt-one',state:'completed',stage:'finished',answer:'<img src=x onerror=alert(1)>',outcomes:{answer:'completed',checkpoint:'saved',cleanup:'confirmed',overall:'completed',error:null},events:[{stage:'starting',timestamp:'synthetic time'}],evidence:{actual_model:'gpt-5.6-luna',context_sha256:'b'.repeat(64),tool_succeeded:true,usage:[{input:3,output:2,total_tokens:5}]}}]};

test('presentation separates model answer from checkpoint and cleanup success',async()=>{
 const {taskPresentation}=await import('../../components/raptor/web/app.js');
 assert.equal(taskPresentation(task).state,'completed');
 assert.equal(taskPresentation(task).actualModel,'gpt-5.6-luna');
 for(const kind of ['checkpoint','cleanup']){
  const changed=structuredClone(task);changed.attempts[0].outcomes[kind]='failed';changed.attempts[0].outcomes.overall='failed';changed.attempts[0].state='failed';
  const view=taskPresentation(changed);assert.equal(view.state,'failed');assert.equal(view.answer,task.attempts[0].answer);assert.equal(view.outcomes[kind],'failed');
 }
 const queued=structuredClone(task);queued.attempts[0]={attempt_id:'new',state:'queued',stage:'queued',answer:null,outcomes:null,events:[],evidence:null};
 assert.equal(taskPresentation(queued).state,'queued');assert.equal(taskPresentation(queued).outcomes.answer,'unknown');assert.equal(taskPresentation(queued).actualModel,'unknown');
 const blocked=structuredClone(queued);blocked.blocked=true;blocked.attempts[0].state='blocked';
 assert.equal(taskPresentation(blocked).blocked,true);
});

class Element{
 constructor(tag,doc){this.tag=tag;this.ownerDocument=doc;this.children=[];this._text='';}
 set textContent(value){this._text=String(value);this.children=[];}
 get textContent(){return this._text+this.children.map(c=>c.textContent).join('');}
 set innerHTML(_){throw Error('unsafe HTML rendering');}
 setAttribute(_,value){assert.ok(!String(value).includes('<'));}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this.children=nodes;this._text='';}
}

test('answers, questions and attempt history are literal text',async()=>{
 const {renderTask}=await import('../../components/raptor/web/app.js');
 const doc={createElement:tag=>new Element(tag,doc)};
 const container=new Element('section',doc),data=structuredClone(task);
 data.attempts.unshift({...data.attempts[0],attempt_id:'old',state:'failed',answer:'previous <script>answer</script>'});
 renderTask(container,data);
 for(const marker of [task.question,task.attempts[0].answer,'previous <script>answer</script>','owner-entered','synthetic time'])assert.ok(container.textContent.includes(marker),marker);
});

test('transport retry keeps submission identity and a new question gets a new identity',async()=>{
 const {createQuestionSubmission}=await import('../../components/raptor/web/app.js');
 const one=createQuestionSubmission({app_id:'app',question:'one',model:'gpt-5.6-luna'});
 const received=[];let failed=false;
 const api=async(_path,{body})=>{received.push(structuredClone(body));if(!failed){failed=true;throw Error('NetworkUnavailable');}return {task_id:'saved'};};
 await assert.rejects(()=>one.submit(api));await one.submit(api);
 assert.equal(received[0].idempotency_key,received[1].idempotency_key);
 const two=createQuestionSubmission({app_id:'app',question:'two',model:'gpt-5.6-luna'});
 await two.submit(api);assert.notEqual(received[1].idempotency_key,received[2].idempotency_key);
});

test('API wrapper obtains request token, sends JSON and exposes only fixed errors',async()=>{
 const {apiRequest}=await import('../../components/raptor/web/app.js');const original=global.fetch;const calls=[];
 try{
  global.fetch=async(url,options={})=>{calls.push({url,options});return {ok:true,json:async()=>({data:url==='/api/session'?{token:'synthetic-request-token'}:{saved:true}})};};
  assert.deepEqual(await apiRequest('/api/apps',{method:'POST',body:{name:'synthetic'}}),{saved:true});
  assert.equal(calls[0].url,'/api/session');assert.equal(calls[1].options.headers['X-Raptor-Token'],'synthetic-request-token');assert.equal(calls[1].options.headers['Content-Type'],'application/json');
  global.fetch=async()=>({ok:false,json:async()=>({error:'synthetic-secret-provider-error'})});
  await assert.rejects(()=>apiRequest('/api/apps'),/RequestFailed/);
 }finally{global.fetch=original;}
});

 test('application name stays distinct from dependency names in the same form',async()=>{
 const {applicationFields}=await import('../../components/raptor/web/app.js');
 const values={name:'demo-db-client',owner:'owner',environment:'poc',region:'ap-southeast-1',ack_cluster_id:'',namespace:'',deployment:''};
 const form={elements:{name:{value:''}},querySelector:selector=>({value:values[selector.match(/name="([^"]+)"/)[1]]})};
 assert.deepEqual(applicationFields(form),{...values,ack_cluster_id:null,namespace:null,deployment:null});
 });

test('late submission and task-action responses cannot change a newly selected app',async()=>{
 for(const action of ['submit','retry','recover','cancel','restore']){
  const originals={document:global.document,localStorage:global.localStorage,fetch:global.fetch,setTimeout:global.setTimeout};
  class BrowserElement extends Element{
   constructor(tag,doc){super(tag,doc);this.listeners={};this.hidden=false;this.value='';this.classList={toggle(){}};}
   addEventListener(event,fn){this.listeners[event]=fn;}
   reset(){}
  }
  const doc={createElement:tag=>new BrowserElement(tag,doc),getElementById:id=>nodes[id]};
  const nodes=Object.fromEntries(['notice','apps','tasks','app-editor','question-section','app-detail','task-panel','task-view','cancel-task','retry-task','recover-task','new-app','cancel-edit','add-dependency','app-form','question','question-count','question-form','ask','model'].map(id=>[id,new BrowserElement('div',doc)]));
  nodes['task-panel'].hidden=true;nodes.question.value='question A';nodes.model.value='gpt-5.6-luna';
  const apps=['A','B'].map(name=>({app_id:name,name,environment:'poc',region:'SG',owner:'owner',version:1,dependencies:[]}));
  const taskA={...structuredClone(task),task_id:'task-A',app_id:'A'};
  let selection=action==='restore'?JSON.stringify({app_id:'B',task_id:'task-A'}):null,release;
  global.document=doc;global.localStorage={getItem:()=>selection,setItem:(_,value)=>{selection=value;}};
  global.setTimeout=()=>0;
  global.fetch=async(url,options={})=>{
   let data;
   if(options.method==='POST')return new Promise(resolve=>{release=()=>resolve({ok:true,json:async()=>({data:taskA})});});
   if(url==='/api/session')data={token:'synthetic'};
   else if(url==='/api/apps')data=apps;
   else if(url==='/api/tasks/task-A')data=taskA;
   else if(url==='/api/tasks?app_id=A')data=[{task_id:'task-A',state:'completed',submitted_at:'2026-01-01'}];
   else data=[];
   return {ok:true,json:async()=>({data})};
  };
  try{
   await import('../../components/raptor/web/app.js?navigation='+action);
   await new Promise(resolve=>setImmediate(resolve));
   if(action!=='restore'){
    await nodes.apps.children.find(e=>e.textContent.startsWith('A ·')).listeners.click();
    if(action!=='submit')await nodes.tasks.children[0].listeners.click();
    const pending=action==='submit'?nodes['question-form'].listeners.submit({preventDefault(){}}):nodes[action+'-task'].listeners.click();
    await new Promise(resolve=>setImmediate(resolve));
    assert.equal(typeof release,'function');
    await nodes.apps.children.find(e=>e.textContent.startsWith('B ·')).listeners.click();
    release();await pending;
   }
   await new Promise(resolve=>setImmediate(resolve));
   assert.equal(nodes['task-panel'].hidden,true,action);
   assert.equal(JSON.parse(selection).app_id,'B');assert.equal(JSON.parse(selection).task_id,null,action);
  }finally{for(const [key,value] of Object.entries(originals)){if(value===undefined)delete global[key];else global[key]=value;}}
 }
});
