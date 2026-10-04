const ERROR_CODES=new Set(['InvalidInput','RequestTooLarge','NotFound','ConflictOrBlocked','LocalAccessDenied','LocalPersistenceFailed']);
let requestToken;
export async function apiRequest(path,{method='GET',body}={}){
 try{
  if(method!=='GET'&&!requestToken){const r=await fetch('/api/session',{cache:'no-store'});if(!r.ok)throw Error('RequestFailed');requestToken=(await r.json()).data.token;}
  const headers=method==='GET'?{}:{'Content-Type':'application/json','X-Raptor-Token':requestToken};
  const response=await fetch(path,{method,headers,cache:'no-store',...(body===undefined?{}:{body:JSON.stringify(body)})});
  const result=await response.json();
  if(!response.ok)throw Error(ERROR_CODES.has(result.error)?result.error:'RequestFailed');
  return result.data;
 }catch(error){throw Error(ERROR_CODES.has(error.message)?error.message:'RequestFailed');}
}

export function createQuestionSubmission(value){
 const payload=structuredClone({...value,idempotency_key:crypto.randomUUID()});
 return {submit:(api=apiRequest)=>api('/api/tasks',{method:'POST',body:structuredClone(payload)})};
}

export function taskPresentation(task){
 const latest=task.attempts.at(-1),evidence=latest.evidence||{};
 return {state:latest.state,stage:latest.stage,blocked:task.blocked,question:task.question,
  answer:latest.answer,outcomes:latest.outcomes||{answer:'unknown',checkpoint:'unknown',cleanup:'unknown'},
  actualModel:evidence.actual_model||'unknown',contextHash:evidence.context_sha256||'unknown',
  contextTool:evidence.tool_succeeded===true,usage:evidence.usage||[],attempts:task.attempts};
}

function element(doc,tag,text,className){const e=doc.createElement(tag);if(text!==undefined)e.textContent=text;if(className)e.className=className;return e;}
export function renderTask(container,task){
 const doc=container.ownerDocument,v=taskPresentation(task);container.replaceChildren();
 container.append(element(doc,'p',`${v.state} · ${v.stage}`,'eyebrow'),element(doc,'h2','Application question'),element(doc,'p',v.question,'question-text'));
 if(v.blocked)container.append(element(doc,'p','Dispatch is blocked. An interrupted run needs recovery before another question can start.','muted'));
 const outcomes=element(doc,'div',undefined,'outcomes');
 for(const name of ['answer','checkpoint','cleanup'])outcomes.append(element(doc,'span',`${name}: ${v.outcomes[name]}`,'outcome'));
 container.append(outcomes,element(doc,'p',`Saved context v${task.snapshot.version} · ${task.snapshot.provenance}`,'muted'));
 if(v.answer)container.append(element(doc,'div',v.answer,'answer'));
 else container.append(element(doc,'p',v.state==='queued'?'Waiting for the gateway worker.':'No completed answer is available yet.','muted'));
 const evidence=element(doc,'details');evidence.append(element(doc,'summary','Run evidence'));
 evidence.append(element(doc,'p',`Requested model: ${task.model}\nActual model: ${v.actualModel}\nContext tool: ${v.contextTool?'confirmed':'not confirmed'}\nContext hash: ${v.contextHash}\nUsage: ${JSON.stringify(v.usage)}`,'context-json'));
 evidence.append(element(doc,'pre',JSON.stringify(task.snapshot,null,2),'context-json'));container.append(evidence);
 for(let i=0;i<v.attempts.length;i++){
  const a=v.attempts[i],details=element(doc,'details');details.append(element(doc,'summary',`Attempt ${i+1} · ${a.state}`));
  if(a.outcomes)details.append(element(doc,'p',`Answer ${a.outcomes.answer} · checkpoint ${a.outcomes.checkpoint} · cleanup ${a.outcomes.cleanup}${a.outcomes.error?' · '+a.outcomes.error:''}`,'muted'));
  if(a.answer&&i<v.attempts.length-1)details.append(element(doc,'div',a.answer,'answer'));
  for(const event of a.events||[])details.append(element(doc,'p',`${event.timestamp} · ${event.stage}`,'event'));
  container.append(details);
 }
}

export function applicationFields(form){
 return Object.fromEntries(['name','owner','environment','region','ack_cluster_id','namespace','deployment'].map(name=>[name,form.querySelector(`[name="${name}"]`).value.trim()||null]));
}

function start(){
 const $=id=>document.getElementById(id);let apps=[],selectedApp=null,selectedTask=null,editing=null,poll=null,epoch=0,pending=null,pendingInput=null;
 const notice=error=>{const messages={InvalidInput:'Check the fields and limits. Keep application context nonsecret.',RequestTooLarge:'The request is too large.',ConflictOrBlocked:'This action conflicts with current state. Reload the record or recover the blocked run.',NotFound:'That record is no longer available.',LocalAccessDenied:'Open this app at its exact local address and reload.',LocalPersistenceFailed:'Local task storage is unavailable. No new work should start.',RequestFailed:'The request did not complete. You can retry the same submission.'};$('notice').textContent=messages[error.message]||messages.RequestFailed;$('notice').hidden=false;};
 const safely=fn=>async event=>{event?.preventDefault();try{await fn(event);$('notice').hidden=true;}catch(error){notice(error);}};
 function saveSelection(){localStorage.setItem('raptor-selection',JSON.stringify({app_id:selectedApp?.app_id||null,task_id:selectedTask?.task_id||null}));}
 function dependency(value={name:'',type:'postgres',lifecycle:'planned',notes:''}){
  if($('dependencies').children.length>=10)return;
  const row=element(document,'div',undefined,'dependency'),fields=element(document,'div',undefined,'fields');
  for(const [name,label,choices] of [['name','Name'],['type','Type',['postgres','pgcat','other']],['lifecycle','Lifecycle',['planned','present','unknown']],['notes','Nonsecret notes']]){
   const l=element(document,'label',label),input=element(document,choices?'select':'input');input.name=name;
   if(choices)for(const choice of choices){const option=element(document,'option',choice);option.value=choice;input.append(option);}
   else{input.maxLength=name==='notes'?1000:128;input.required=name==='name';}
   input.value=value[name]||'';l.append(input);fields.append(l);
  }
  const remove=element(document,'button','Remove','small');remove.type='button';remove.addEventListener('click',()=>row.remove());row.append(fields,remove);$('dependencies').append(row);
 }
 function edit(app=null){editing=app;$('app-editor').hidden=false;$('editor-title').textContent=app?'Edit application':'Register application';$('dependencies').replaceChildren();$('app-form').reset();for(const name of ['name','owner','environment','region','ack_cluster_id','namespace','deployment'])if(app)$('app-form').querySelector(`[name="${name}"]`).value=app[name]||'';for(const dep of app?.dependencies||[])dependency(dep);$('app-form').querySelector('[name="name"]').focus();}
 async function refreshApps(){apps=await apiRequest('/api/apps');$('apps').replaceChildren();for(const app of apps){const button=element(document,'button',`${app.name} · ${app.environment}`);button.classList.toggle('selected',selectedApp?.app_id===app.app_id);button.addEventListener('click',safely(()=>selectApp(app)));$('apps').append(button);}}
 async function refreshTasks(){if(!selectedApp)return;const id=selectedApp.app_id,tasks=await apiRequest('/api/tasks?app_id='+encodeURIComponent(id));if(selectedApp?.app_id!==id)return;$('tasks').replaceChildren();if(!tasks.length)$('tasks').append(element(document,'p','No questions yet.','muted'));for(const task of tasks.reverse()){const button=element(document,'button',`${task.state} · ${new Date(task.submitted_at).toLocaleString()}`,'task-item');button.addEventListener('click',safely(()=>showTask(task.task_id)));$('tasks').append(button);}}
 async function selectApp(app){epoch++;clearTimeout(poll);selectedApp=app;selectedTask=null;pending=null;pendingInput=null;$('task-panel').hidden=true;$('app-editor').hidden=true;$('question-section').hidden=false;$('app-detail').replaceChildren();const title=element(document,'div',undefined,'section-heading'),editButton=element(document,'button','Edit context','small');editButton.addEventListener('click',()=>edit(selectedApp));title.append(element(document,'h1',app.name),editButton);$('app-detail').append(title,element(document,'p',`${app.environment} · ${app.region} · owner ${app.owner}`,'muted'),element(document,'p',`Context v${app.version} · owner-entered declarations`,'muted'));for(const name of ['ack_cluster_id','namespace','deployment'])$('app-detail').append(element(document,'p',`${name.replaceAll('_',' ')}: ${app[name]||'unknown'}`));for(const dep of app.dependencies)$('app-detail').append(element(document,'p',`${dep.name} · ${dep.type} · ${dep.lifecycle}${dep.notes?' — '+dep.notes:''}`));saveSelection();await refreshApps();await refreshTasks();}
 async function showTask(id){clearTimeout(poll);const current=++epoch,task=await apiRequest('/api/tasks/'+encodeURIComponent(id));if(current!==epoch)return;selectedTask=task;saveSelection();$('task-panel').hidden=false;renderTask($('task-view'),task);const a=task.attempts.at(-1);$('cancel-task').hidden=a.state!=='queued';$('retry-task').hidden=task.blocked||a.state!=='failed'||a.outcomes?.cleanup!=='confirmed';$('recover-task').hidden=!task.blocked||!['blocked','failed','completed','claimed'].includes(a.state);if(['queued','claimed','blocked'].includes(a.state)||task.blocked)poll=setTimeout(()=>showTask(id).then(refreshTasks).catch(notice),2000);}
 $('new-app').addEventListener('click',()=>edit());$('cancel-edit').addEventListener('click',()=>{$('app-editor').hidden=true;});$('add-dependency').addEventListener('click',()=>dependency());
 $('app-form').addEventListener('submit',safely(async()=>{const app=applicationFields($('app-form'));app.dependencies=[...$('dependencies').children].map(row=>Object.fromEntries(['name','type','lifecycle','notes'].map(name=>[name,row.querySelector(`[name="${name}"]`).value])));const saved=await apiRequest(editing?'/api/apps/'+editing.app_id:'/api/apps',{method:editing?'PUT':'POST',body:editing?{expected_version:editing.version,app}:app});await selectApp(saved);}));
 $('question').addEventListener('input',()=>{$('question-count').textContent=`${[...$('question').value].length} / 2,000 characters`;});
 $('question-form').addEventListener('submit',safely(async()=>{if(!selectedApp)return;const value={app_id:selectedApp.app_id,question:$('question').value,model:$('model').value},key=JSON.stringify(value);if(pendingInput!==key){pending=createQuestionSubmission(value);pendingInput=key;}$('ask').disabled=true;try{const task=await pending.submit();pending=null;pendingInput=null;$('question-form').reset();$('question-count').textContent='0 / 2,000 characters';await refreshTasks();await showTask(task.task_id);}finally{$('ask').disabled=false;}}));
 for(const [button,action] of [['cancel-task','cancel'],['retry-task','retry'],['recover-task','recover']])$(button).addEventListener('click',safely(async()=>{const task=selectedTask;if(!task)return;const a=task.attempts.at(-1),body=action==='cancel'?{attempt_id:a.attempt_id}:action==='retry'?{idempotency_key:crypto.randomUUID()}:{attempt_id:a.attempt_id,idempotency_key:crypto.randomUUID()};$(button).disabled=true;try{await apiRequest('/api/tasks/'+task.task_id+'/'+action,{method:'POST',body});await showTask(task.task_id);await refreshTasks();}finally{$(button).disabled=false;}}));
 (async()=>{await refreshApps();let saved;try{saved=JSON.parse(localStorage.getItem('raptor-selection'));}catch{}const app=apps.find(a=>a.app_id===saved?.app_id);if(app){await selectApp(app);if(saved?.task_id)await showTask(saved.task_id);}})().catch(notice);
}
if(typeof document!=='undefined')start();
