import {createDirectProgressController} from './direct-progress.js';
import {updateWorklog,resetWorklog} from './worklog.js';
export function validateParameters(schema, values) {
  const errors=[];
  for (const name of schema.required ?? []) if (!(name in values) || values[name]==='') errors.push(`${name} is required`);
  for (const [name,value] of Object.entries(values)) {
    const field=schema.properties?.[name];
    if (!field) { errors.push(`${name} is not supported`); continue; }
    if (field.type==='integer' && (!Number.isSafeInteger(value) || value<(field.minimum ?? -Infinity))) errors.push(`${name} must be an integer at least ${field.minimum ?? 0}`);
    if (field.type==='string' && (typeof value!=='string' || Array.from(value).length<(field.minLength ?? 0) || (field.maxLength!==undefined && Array.from(value).length>field.maxLength) || (field.maxBytes!==undefined && new TextEncoder().encode(value).length>field.maxBytes) || (field['x-multiline'] && !value.trim()))) errors.push(`${name} must be text`);
    if (field.enum && !field.enum.includes(value)) errors.push(`${name} must use a listed value`);
  }
  return errors;
}
export function groupEnvironments(envs) { const groups=new Map(); for(const env of envs){if(!groups.has(env.groupCode))groups.set(env.groupCode,[]);groups.get(env.groupCode).push(env);}return groups; }
export function discoveryView(deployments,error){return error?{state:'unavailable',message:'Deployment discovery is unavailable'}:{state:deployments.length?'available':'empty',deployments};}
export function createProgressController({read,schedule=setTimeout,cancel=clearTimeout,onChange=()=>{}}){
  const state={requestId:null,events:[],cursor:0,unavailable:false};let timer,epoch=0;
  async function poll(id,version){try{const result=await read(id,state.cursor);if(version!==epoch||id!==state.requestId)return;const known=new Set(state.events.map(e=>e.sequence));for(const event of result.events??[]){if(!known.has(event.sequence)){state.events.push(event);known.add(event.sequence);state.cursor=Math.max(state.cursor,event.sequence);}}state.unavailable=!!result.syncUnavailable;}catch{if(version!==epoch)return;state.unavailable=true;}if(version!==epoch)return;onChange(state);timer=schedule(()=>poll(id,version),2000);}
  return {state,async select(id){cancel(timer);epoch++;state.requestId=id;state.events=[];state.cursor=0;return poll(id,epoch);},close(){cancel(timer);epoch++;state.requestId=null;}};
}
let pageEpoch=0;
export function setPageContext(document,page,kind,tab){
 pageEpoch++;
 document.getElementById('message').textContent='';
 document.getElementById('restart-basket-panel').hidden=!(page==='object-detail'&&kind==='application'&&tab==='deployments');
}
export function setSessionView(document,signedIn){
 for(const id of ['platform-sidebar','workspace','workspace-skip'])document.getElementById(id).hidden=!signedIn;
 document.getElementById('login-page').hidden=signedIn;
 document.getElementById(signedIn?'workspace-feedback':'login-feedback').append(document.getElementById('message'));
}
let csrf='';
export async function api(path,{method='GET',body,headers={}}={}){const epoch=pageEpoch;try{const response=await fetch('/api/v1'+path,{method,headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,...headers},body:body===undefined?undefined:JSON.stringify(body),credentials:'same-origin'});const result=await response.json();if(!response.ok)throw new Error(result.error?.code??'Unavailable');return result.data;}catch(error){error.pageEpoch=epoch;throw error;}}
function node(tag,text){const el=document.createElement(tag);if(text!==undefined)el.textContent=String(text);return el;}
function button(text,fn){const el=node('button',text);el.type='button';el.addEventListener('click',()=>Promise.resolve(fn()).catch(showError));return el;}
export function showError(error){if(error.pageEpoch!==undefined&&error.pageEpoch!==pageEpoch)return;document.getElementById('message').textContent=error.message??'Unavailable';}
export function renderOperationForm(schema){const host=document.getElementById('operation-fields');host.replaceChildren();for(const [name,field]of Object.entries(schema.properties??{})){const label=node('label',`${name}${schema.required?.includes(name)?' *':''}`);let input;if(field.enum){input=node('select');for(const value of field.enum){const option=node('option',value);option.value=value;input.append(option);}}else if(field['x-multiline']){input=node('textarea');input.rows=5;}else{input=node('input');input.type=field.type==='integer'?'number':'text';if(field.minimum!==undefined)input.min=field.minimum;if(field.type==='integer')input.step='1';}input.name=name;input.required=schema.required?.includes(name)??false;if(field.default!==undefined)input.value=field.default;label.append(input);host.append(label);}}
export function renderProgress(events){if(updateWorklog(document.getElementById('t3-worklog'),{events}))return;const host=document.getElementById('progress');host.replaceChildren();for(const event of events){const row=node('div');row.className='event';row.append(node('strong',`${event.evidenceMode??'unverified'} · ${event.kind}`),node('p',event.summary));if(event.occurredAt){const time=node('time',new Date(event.occurredAt).toLocaleString());time.dateTime=event.occurredAt;row.append(time);}if(event.details&&Object.keys(event.details).length){const detail=node('details');detail.append(node('summary','Tool / event details'),node('pre',JSON.stringify(event.details,null,2)));row.append(detail);}host.append(row);}}
export function renderExecutionResult(value){
 updateWorklog(document.getElementById('t3-worklog'),{snapshot:value});
 const $=id=>document.getElementById(id), execution=value.execution??{}, definition=value.request?.definition??{}, result=execution.result;
 $('answer-panel').hidden=!result?.answer;$('answer').textContent=result?.answer??'';
 const counts=Array.isArray(result?.usage)&&result.usage.length?result.usage.reduce((a,u)=>({input:a.input+u.input,output:a.output+u.output,total:a.total+u.totalTokens}),{input:0,output:0,total:0}):null;
 const cleanup=typeof execution.cleanupStatus==='object'&&execution.cleanupStatus!==null?(execution.cleanupStatus.sandboxAbsent===true&&execution.cleanupStatus.keyAbsent===true&&execution.cleanupStatus.accessRevoked===true?'confirmed':'pending — sandbox '+(execution.cleanupStatus.sandboxAbsent?'absent':'unconfirmed')+', key '+(execution.cleanupStatus.keyAbsent?'absent':'unconfirmed')+', Raptor access '+(execution.cleanupStatus.accessRevoked?'revoked':'unconfirmed')):(execution.cleanupStatus??execution.cleanup??'not recorded');
 $('answer-identity').textContent=`${value.executionAvailable===false?'Last saved · ':''}Requested model: ${definition.model??'not recorded'} · Actual model: ${result?.actualModel??'not recorded'} · Selected skills: ${definition.skills?.tag??'not recorded'} (${definition.skills?.commitSha??'not recorded'}) · Applied skills: ${execution.appliedSkills?.tag??'not recorded'} (${execution.appliedSkills?.commitSha??'not recorded'})${value.lastSuccessfulSyncAt?' · Last sync: '+value.lastSuccessfulSyncAt:''}${counts?' · Usage: '+counts.input+' input, '+counts.output+' output, '+counts.total+' total':''}`;
 $('lifecycle').textContent=`Stage: ${execution.stage??'not recorded'} · Checkpoint: ${execution.checkpointStatus??'not recorded'} · Cleanup: ${cleanup}${execution.failureCode?' · Failure: '+execution.failureCode:''}${value.cancellationPending?' · Cancellation requested; waiting for cleanup':''}`;
 $('answer-evidence').replaceChildren();for(const evidence of result?.evidence??[]){$('answer-evidence').append(node('p',`${evidence.evidenceMode??'unverified'} · ${evidence.server}/${evidence.tool} · ${evidence.observedAt??'timestamp not recorded'}`),node('pre',JSON.stringify({identity:evidence.identity,state:evidence.state},null,2)));}
 const question=definition.operations?.some(op=>op.name==='application.question');
 const restricted=question;
 if($('instructions'))$('instructions').hidden=!!restricted;
 for(const control of document.querySelectorAll?.('[data-action]')??[])control.hidden=restricted&&control.dataset.action!=='cancel';
 if($('skills-change'))$('skills-change').hidden=restricted;
}
export function catalogObjects(objects,kind){return objects.filter(object=>object.kind===kind);}
export function requestTabState(value,selected){
 const approvals=!!value.approvals?.length;
 const result=!!value.execution?.result?.answer||!!value.targets?.length;
 return {selected:selected==='approvals'&&!approvals?'overview':selected|| (result?'result':'overview'),approvals};
}
export function operationChoices(schemas,kind){return Object.keys(schemas).filter(name=>schemas[name]['x-object-kind']===kind);}
export function pageRoute(hash){
 const params=new URLSearchParams(hash.replace(/^#/,''));
 const page=['new-request','request','object','catalog'].find(key=>params.has(key))||'catalog';
 return {page,id:params.get(page)||'application',environment:params.get('env')||'',tab:['overview','deployments','dependencies','related'].includes(params.get('tab'))?params.get('tab'):'overview',mode:params.get('mode')==='restart'?'restart':'agent'};
}
function metadata(host,values){host.replaceChildren();for(const [label,value] of values){const group=node('div');group.append(node('dt',label),node('dd',value||'Not recorded'));host.append(group);}}
export function updateNavigation(panel,tab,kind='application'){
 for(const [id,active] of [['catalog-nav',(panel==='catalog-panel'||panel==='object-detail')&&kind==='application'],['database-nav',(panel==='catalog-panel'||panel==='object-detail')&&kind==='database'],['proxy-nav',(panel==='catalog-panel'||panel==='object-detail')&&kind==='db-proxy'],['requests-nav',panel==='requests-panel'||panel==='request-panel'||panel==='new-request-panel']]){
  const control=document.getElementById(id);
  if(active)control.setAttribute('aria-current','page');else control.removeAttribute('aria-current');
 }
 for(const control of document.querySelectorAll('[data-tab]')){
  if(control.dataset.tab===tab)control.setAttribute('aria-current','page');else control.removeAttribute('aria-current');
 }
}
async function start(){
 const $=id=>document.getElementById(id);let objects=[],envs=[],releases=[],schemas={},selectedObject=null,requestId=null,currentTab='overview',catalogKind='application',requestTab=null,requestView=null,basket=[],viewEpoch=0,requestEpoch=0,pendingSubmission=null,requestResource=null,requestOrigin=null,requestTargets=[],activeRoute='';
 const progress=createDirectProgressController({read:(id,cursor)=>api(`/requests/${encodeURIComponent(id)}/events?after=${cursor}`),ticket:id=>api(`/requests/${encodeURIComponent(id)}/progress-ticket`,{method:'POST',body:{}}),onChange:state=>{updateWorklog($('t3-worklog'),{unavailable:state.unavailable});renderProgress(state.events);$('progress-availability').textContent=state.unavailable?'Live sync unavailable; showing saved history':state.mode==='polling'?'Realtime not configured; checking progress every two seconds':state.mode==='complete'?'Execution finished; showing saved history':state.mode==='live'?'Connected directly to Gateway; live progress':'Connecting to Gateway';if(state.execution&&requestView){requestView={...requestView,execution:state.execution,executionAvailable:true};renderExecutionResult(requestView);$('request-state').textContent=`${state.execution.status} · ${state.execution.runtimeMode??'unverified'} execution`;renderRequestTabs();}if(state.mode==='complete')refreshRequest(state.requestId).catch(showError);else if(state.mode==='polling')refreshRequest(state.requestId).catch(showError);}});
 function panel(name){setPageContext(document,name,catalogKind,currentTab);resetWorklog($('t3-worklog'));setSessionView(document,name!=='login-panel');updateNavigation(name,currentTab,catalogKind);for(const id of ['catalog-panel','object-detail','requests-panel','request-panel','new-request-panel','login-panel'])$(id).hidden=id!==name;}
 function skillOptions(select){select.replaceChildren();for(const v of releases){const option=node('option',v.tag);option.value=v.tag;select.append(option);}}
 function pickedSkills(select){return releases.find(v=>v.tag===select.value)??{tag:'',commitSha:''};}
 function fillEnvironments(){const select=$('environment');select.replaceChildren();for(const [name,values]of groupEnvironments(envs)){const group=node('optgroup');group.label=name;for(const env of values){const option=node('option',`${env.code} · ${env.stage}`);option.value=env.code;group.append(option);}select.append(group);}}
 async function refreshCatalog(){[objects,envs,schemas]=await Promise.all([api('/objects'),api('/environments'),api('/operation-schemas')]);try{releases=await api('/skills/releases');}catch{releases=[];}fillEnvironments();for(const id of ['request-skills','change-skills'])skillOptions($(id));renderCatalog();}
 function kindLabel(){return {application:'Application',database:'Database','db-proxy':'Database proxy'}[catalogKind];}
 function renderCatalog(){
  $('catalog-heading').textContent=kindLabel()+' list';$('catalog-eyebrow').textContent=catalogKind==='application'?'APPLICATION':'COMPONENT';
  $('create-object').elements.kind.value=catalogKind;$('create-name-label').firstChild.textContent=kindLabel()+' name';$('create-object-submit').textContent='Create '+kindLabel().toLowerCase();
  $('objects').replaceChildren();const list=catalogObjects(objects,catalogKind);
  if(!list.length){const empty=node('p','No '+kindLabel().toLowerCase()+' resources yet. Create one to get started.');empty.className='empty-state';$('objects').append(empty);}
  for(const object of list){const item=button('',()=>openObject(object));item.append(node('strong',object.name),node('span',object.description||'No description yet'),node('small',object.code));$('objects').append(item);}
 }
 function openCatalog(kind=catalogKind,replace=false){progress.close();requestId=null;requestEpoch++;viewEpoch++;selectedObject=null;catalogKind=kind;setRoute({catalog:kind},replace);currentTab='overview';renderCatalog();panel('catalog-panel');}
 function setRoute(values,replace=false){activeRoute=new URLSearchParams(values).toString();if(location.hash.slice(1)!==activeRoute){if(replace)history.replaceState(null,'','#'+activeRoute);else location.hash=activeRoute;}}
 async function openObject(object,tab='overview',replace=false){progress.close();requestId=null;requestEpoch++;viewEpoch++;selectedObject=object;catalogKind=object.kind;currentTab=tab;setRoute({object:object.id,tab,env:$('environment').value},replace);panel('object-detail');$('object-heading').textContent=object.name;$('object-kind').textContent=kindLabel();$('object-code').textContent=object.code;$('back-to-list').textContent='← '+kindLabel()+' list';$('new-request').disabled=!operationChoices(schemas,object.kind).length;await showDetail();}
 function openNewRequest(object,mode='agent',replace=false){
  if(!object)throw new Error('Choose a catalog resource first');
  requestOrigin={object,tab:currentTab,environment:$('environment').value};requestResource=object;requestTargets=mode==='restart'?structuredClone(basket):[];
  progress.close();requestId=null;requestEpoch++;viewEpoch++;selectedObject=object;catalogKind=object.kind;
  setRoute({'new-request':object.id,env:$('environment').value,mode},replace);panel('new-request-panel');$('operation-form').reset();
  metadata($('new-request-context'),[['Resource',object.name],['Type',kindLabel()],['Code',object.code]]);
  $('new-agent-request').hidden=mode!=='agent';$('new-restart-request').hidden=mode!=='restart';
  $('new-request-environment').replaceChildren(...[...$('environment').children].map(group=>group.cloneNode(true)));$('new-request-environment').value=$('environment').value;
  $('operation').replaceChildren();for(const name of operationChoices(schemas,object.kind)){const option=node('option',name);option.value=name;$('operation').append(option);}
  $('operation-form').hidden=!$('operation').value;$('model-field').hidden=$('operation').value!=='application.question';if($('operation').value)renderOperationForm(schemas[$('operation').value]);
  $('restart-request-targets').replaceChildren();for(const target of requestTargets){const card=node('section');const fields=node('dl');fields.className='metadata-grid';metadata(fields,[['Application code',target.appCode],['Environment',target.envCode],['Deployment',target.namespace+'/'+target.name],['Cluster',target.clusterId],['UID',target.uid]]);card.append(fields);$('restart-request-targets').append(card);}
  if(mode==='restart'&&!requestTargets.length)$('restart-request-targets').append(node('p','No deployments selected. Cancel and choose deployments before creating a restart request.'));
  $('create-restart-request').disabled=!requestTargets.length;$('create-agent-request').disabled=false;$('cancel-new-request').disabled=false;$('new-request-panel').querySelector('h2').focus();
 }
 async function restoreRoute(){
  const route=pageRoute(location.hash);if(route.environment){if(!envs.some(env=>env.code===route.environment)){openCatalog('application',true);throw new Error('The selected environment is unavailable');}$('environment').value=route.environment;}
  if(route.page==='request'){await loadRequest(route.id,true);return;}
  if(route.page==='object'||route.page==='new-request'){
   const object=objects.find(object=>object.id===route.id);if(!object){openCatalog('application',true);throw new Error('The selected resource is unavailable');}
   if(route.page==='object')await openObject(object,route.tab,true);else openNewRequest(object,route.mode,true);return;
  }
  if(route.id==='requests')await requestList(true);else openCatalog(['application','database','db-proxy'].includes(route.id)?route.id:'application',true);
 }
 async function showDetail(){const object=selectedObject;if(!object)return;setPageContext(document,'object-detail',object.kind,currentTab);const env=$('environment').value;const token=++viewEpoch;const host=$('detail');host.replaceChildren();
  if(currentTab==='overview'){
   const layout=node('div');layout.className='overview-layout';const about=node('section'),context=node('section');
   about.append(node('h3','About'),node('p',object.description||'No description has been added.'));
   const fields=node('dl');fields.className='metadata-grid';metadata(fields,[['Name',object.name],['Resource type',kindLabel()],['Code',object.code]]);about.append(fields);
   const edit=node('details');edit.append(node('summary','Edit catalog metadata'));const form=node('form');form.className='metadata-form';
   const name=node('input');name.value=object.name;name.required=true;const nameLabel=node('label','Name');nameLabel.append(name);
   const description=node('textarea');description.value=object.description??'';description.rows=4;const descriptionLabel=node('label','Description');descriptionLabel.append(description);
   form.append(nameLabel,descriptionLabel,node('button','Save catalog metadata'));form.addEventListener('submit',async e=>{e.preventDefault();try{selectedObject=await api(`/objects/${object.id}`,{method:'PATCH',body:{name:name.value,description:description.value}});await refreshCatalog();await openObject(selectedObject);}catch(err){showError(err);}});edit.append(form);about.append(edit);
   context.append(node('h3','Selected environment'));const envFields=node('dl');envFields.className='metadata-grid';const environment=envs.find(e=>e.code===env);metadata(envFields,[['Environment',env],['Group',environment?.groupCode],['Stage',environment?.stage]]);context.append(envFields,node('p','Deployment state is available in the Deployments view.'));
   const links=node('div');links.className='overview-links';for(const [tab,title] of [['deployments','View deployments'],['dependencies','View dependencies'],['related','View requests']])links.append(button(title,()=>{currentTab=tab;setRoute({object:selectedObject.id,tab,env:$('environment').value});updateNavigation('object-detail',tab,catalogKind);return showDetail();}));context.append(links);layout.append(about,context);host.append(layout);return;
  }
  if(currentTab==='related'){const requests=await api('/requests');if(token!==viewEpoch)return;for(const r of requests)if(r.definition.object?.code===object.code||r.definition.targets?.some(t=>t.appCode===object.code))host.append(button(`${r.id} · ${r.status}`,()=>loadRequest(r.id)));return;}
  let deployments;try{deployments=await api(`/objects/${object.id}/deployments?env=${encodeURIComponent(env)}`);}catch(err){if(token===viewEpoch)host.append(node('p','Discovery unavailable. This does not mean there are no deployments.'));return;}if(token!==viewEpoch)return;if(!deployments.length){host.append(node('p',`No discovered deployment in ${env}`));return;}
  if(currentTab==='dependencies'){for(const d of deployments)host.append(node('p',d['target-db-code']?`DB proxy target database: ${d['target-db-code']} in ${d.envCode}`:'No dependency association returned by Infra API.'));return;}
  for(const d of deployments){const card=node('section');card.append(node('h3',d.name),node('span',`${d.evidenceMode??'unavailable'} · ${d.state}`));const fields=node('dl');fields.className='metadata-grid';metadata(fields,[['Cluster',d.clusterId],['Namespace',d.namespace],['Environment',d.envCode],['Workload',d.kind],['Deployment UID',d.uid]]);card.append(fields);const raw=node('details');raw.append(node('summary','Discovery details'),node('pre',JSON.stringify(d,null,2)));card.append(raw);if(object.kind==='application')card.append(button('Add deployment to restart batch',()=>{const target={appCode:object.code,envCode:env,clusterId:d.clusterId,namespace:d.namespace,name:d.name,uid:d.uid};if(!basket.some(t=>JSON.stringify(t)===JSON.stringify(target)))basket.push(target);renderBasket();}));host.append(card);}
 }
 function renderBasket(){$('basket').replaceChildren();for(const t of basket)$('basket').append(node('p',`${t.appCode}, ${t.envCode}, ${t.namespace}/${t.name}`));}
 async function submit(body){
  const signature=JSON.stringify(body);
  if(pendingSubmission?.promise)return pendingSubmission.promise;
  if(!pendingSubmission || pendingSubmission.signature!==signature)pendingSubmission={signature,key:crypto.randomUUID(),promise:null};
  const pending=pendingSubmission,navigation=viewEpoch;
  pending.promise=(async()=>{const result=await api('/requests',{method:'POST',body,headers:{'Idempotency-Key':pending.key}});pendingSubmission=null;if(navigation===viewEpoch)await loadRequest(result.id);})();
  try{return await pending.promise;}finally{pending.promise=null;}
 }
 async function loadRequest(id,replace=false){viewEpoch++;setRoute({request:id},replace);requestTab=null;requestView=null;const version=++requestEpoch;progress.close();requestId=id;panel('request-panel');await refreshRequest(id);if(version!==requestEpoch || requestId!==id)return;await progress.select(id);}
 async function refreshRequest(id){if(!id)return;const token=id;const value=await api(`/requests/${encodeURIComponent(id)}`);if(token!==requestId)return;const request=value.request??value;$('request-heading').textContent=`Request ${request.id}`;$('request-definition').textContent=JSON.stringify(request.definition,null,2);renderRequestInformation(request);$('request-state').textContent=value.executionAvailable===false?`${request.status} (last saved); Execution sync unavailable`:`${request.status} · ${value.execution?.runtimeMode??'unverified'} execution`;renderExecutionResult(value);renderTargetResults(value.targets??[]);$('approvals').replaceChildren();for(const approval of value.approvals??[]){const box=node('section');box.append(node('h3',`Approval ${approval.state}`),node('pre',JSON.stringify(approval.binding,null,2)));if(approval.state==='pending'){box.append(button('Approve',()=>decision(id,approval.approvalId,{decision:'approve'})));for(const next of ['block','cancel','continue'])box.append(button(`Deny and ${next}`,()=>decision(id,approval.approvalId,{decision:'deny',next,guidance:$('instructions').value})));}$('approvals').append(box);}requestView=value;renderRequestTabs();if(value.execution?.recoveryNeeded)$('request-state').append(node('p','Recovery required: previous runtime ownership is unresolved. No automatic replay.'));if(value.execution?.appliedSkills)$('request-state').append(node('p',`Applied skills: ${value.execution.appliedSkills.tag??'not started'}`));}
 function renderRequestInformation(request){
  const d=request.definition??{},targets=d.targets??[],codes=[...new Set(targets.map(t=>t.appCode))],environments=[...new Set(targets.map(t=>t.envCode))];
  const object=objects.find(o=>o.code===d.object?.code);
  metadata($('request-basics'),[['Resource',object?.name||d.object?.code||codes.join(', ')],['Environment',d.envCode||environments.join(', ')],['Operation',d.operation||d.operations?.map(o=>o.name).join(', ')],['Type',d.type],['Submitted',request.createdAt?new Date(request.createdAt).toLocaleString():'Not recorded']]);
  metadata($('request-config'),[['Requested model',d.model],['Skills release',d.skills?.tag],['Skills commit',d.skills?.commitSha]]);
  $('request-input').replaceChildren();for(const op of d.operations??[]){$('request-input').append(node('h3',op.name));for(const [key,value] of Object.entries(op.parameters??{})){const label=node('h4',key),text=node('p',typeof value==='object'?JSON.stringify(value):value);text.className='submitted-value';$('request-input').append(label,text);}}
  if(d.operation)$('request-input').append(node('p',d.operation+' · '+targets.length+' targets'));
 }
 function renderTargetResults(targets){
  const host=$('target-results');host.replaceChildren();if(!targets.length)return;
  host.append(node('h3','Target outcomes'));const wrap=node('div');wrap.className='table-scroll';const table=node('table');table.append(node('caption','Restart results by deployment'));const head=node('thead'),headRow=node('tr');for(const title of ['Application','Environment','Deployment','State','Evidence','Details']){const cell=node('th',title);cell.scope='col';headRow.append(cell);}head.append(headRow);table.append(head);const body=node('tbody');
  for(const item of targets){const row=node('tr');for(const value of [item.target.appCode,item.target.envCode,item.target.namespace+'/'+item.target.name,item.state,item.details?.evidenceMode??'unverified'])row.append(node('td',value));const cell=node('td'),details=node('details');details.append(node('summary','Inspect'),node('pre',JSON.stringify(item.details??{},null,2)));cell.append(details);row.append(cell);body.append(row);}table.append(body);wrap.append(table);host.append(wrap);
 }
 function renderRequestTabs(){
  const state=requestTabState(requestView??{},requestTab),activeTab=state.selected;
  for(const control of document.querySelectorAll('[data-request-tab]')){control.hidden=control.dataset.requestTab==='approvals'&&!state.approvals;if(control.dataset.requestTab===activeTab)control.setAttribute('aria-current','page');else control.removeAttribute('aria-current');}
  for(const view of document.querySelectorAll('[data-request-view]'))view.hidden=view.dataset.requestView!==activeTab;
  $('result-empty').hidden=!!requestView?.execution?.result?.answer||!!requestView?.targets?.length;
 }
 async function decision(id,approvalId,body){await api(`/requests/${id}/approvals/${approvalId}/decision`,{method:'POST',body});await refreshRequest(id);}
 async function requestList(replace=false){progress.close();requestId=null;requestEpoch++;viewEpoch++;setRoute({catalog:'requests'},replace);panel('requests-panel');const list=await api('/requests');$('request-list').replaceChildren();for(const r of list)$('request-list').append(button(`${r.id} · ${r.definition.type} · ${r.status}`,()=>loadRequest(r.id)));}
 $('login').addEventListener('submit',async e=>{e.preventDefault();try{const data=await api('/login',{method:'POST',body:Object.fromEntries(new FormData(e.target))});csrf=data.csrf;$('message').textContent='';e.target.reset?.();$('identity').textContent=data.user.username;await refreshCatalog();await restoreRoute();}catch(err){showError(err);}});
 $('logout').onclick=async()=>{viewEpoch++;try{await api('/logout',{method:'POST'});progress.close();csrf='';panel('login-panel');}catch(e){showError(e);}};
 $('catalog-nav').onclick=()=>openCatalog('application');$('database-nav').onclick=()=>openCatalog('database');$('proxy-nav').onclick=()=>openCatalog('db-proxy');$('back-to-list').onclick=()=>openCatalog();$('back-to-requests').onclick=()=>requestList().catch(showError);for(const control of document.querySelectorAll('[data-request-tab]'))control.onclick=()=>{requestTab=control.dataset.requestTab;renderRequestTabs();};$('requests-nav').onclick=()=>requestList().catch(showError);$('environment').onchange=()=>{if(!$('new-request-panel').hidden){$('new-request-environment').value=$('environment').value;setRoute({'new-request':requestResource.id,env:$('environment').value,mode:$('new-agent-request').hidden?'restart':'agent'});}else if(!$('object-detail').hidden){setRoute({object:selectedObject.id,tab:currentTab,env:$('environment').value});showDetail().catch(showError);}};
 for(const b of document.querySelectorAll('[data-tab]'))b.onclick=()=>{currentTab=b.dataset.tab;setRoute({object:selectedObject.id,tab:currentTab,env:$('environment').value});updateNavigation('object-detail',currentTab,catalogKind);showDetail().catch(showError);};$('operation').onchange=()=>{renderOperationForm(schemas[$('operation').value]);$('model-field').hidden=$('operation').value!=='application.question';};
 $('new-request').onclick=()=>openNewRequest(selectedObject);
 $('cancel-new-request').onclick=()=>{if(!requestOrigin)return;const origin=requestOrigin;$('environment').value=origin.environment;openObject(origin.object,origin.tab).catch(showError);};
 $('new-request-environment').onchange=()=>{$('environment').value=$('new-request-environment').value;setRoute({'new-request':requestResource.id,env:$('environment').value,mode:'agent'});};
 window.addEventListener('hashchange',()=>{if(csrf&&location.hash.slice(1)!==activeRoute)restoreRoute().catch(showError);});
 $('create-object').onsubmit=async e=>{e.preventDefault();try{const object=await api('/objects',{method:'POST',body:Object.fromEntries(new FormData(e.target))});await refreshCatalog();await openObject(object);}catch(err){showError(err);}};
 $('operation-form').onsubmit=async e=>{e.preventDefault();try{const name=$('operation').value,schema=schemas[name],parameters={};for(const input of $('operation-fields').querySelectorAll('input,select,textarea')){if(input.value==='')continue;parameters[input.name]=schema.properties[input.name].type==='integer'?Number(input.value):input.value;}const errors=validateParameters(schema,parameters);if(errors.length)throw new Error(errors.join('; '));$('create-agent-request').disabled=true;$('cancel-new-request').disabled=true;await submit({type:'agent',object:{kind:requestResource.kind,code:requestResource.code},envCode:$('new-request-environment').value,operations:[{name,parameters}],skills:pickedSkills($('request-skills')),...(name==='application.question'?{model:$('request-model').value}:{})});}catch(err){showError(err);}finally{$('create-agent-request').disabled=false;$('cancel-new-request').disabled=false;}};
 $('restart-submit').onclick=()=>{if(!basket.length){showError(new Error('Choose deployments first'));return;}openNewRequest(selectedObject,'restart');};$('create-restart-request').onclick=async()=>{if(!requestTargets.length)return;try{$('create-restart-request').disabled=true;$('cancel-new-request').disabled=true;await submit({type:'direct',operation:'application.restart',targets:structuredClone(requestTargets)});}catch(error){showError(error);}finally{$('create-restart-request').disabled=false;$('cancel-new-request').disabled=false;}};$('restart-clear').onclick=()=>{basket=[];renderBasket();};
 for(const b of document.querySelectorAll('[data-action]'))b.onclick=async()=>{try{await api(`/requests/${requestId}/actions`,{method:'POST',body:{action:b.dataset.action,instructions:$('instructions').value}});await refreshRequest(requestId);}catch(err){showError(err);}};
 $('skills-change').onsubmit=async e=>{e.preventDefault();try{await api(`/requests/${requestId}/skills`,{method:'POST',body:{...pickedSkills($('change-skills')),strategy:e.target.elements.strategy.value}});await refreshRequest(requestId);}catch(err){showError(err);}};
 window.addEventListener('pagehide',()=>progress.close());
 try{const session=await api('/session');csrf=session.csrf;$('identity').textContent=session.user.username;await refreshCatalog();try{await restoreRoute();}catch(error){showError(error);}}catch{panel('login-panel');}
}
if(typeof document!=='undefined')start().catch(showError);
