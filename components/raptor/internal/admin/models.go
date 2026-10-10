package admin

const modelScript = `<script>
let modelPoll;
function modelNotice(message){document.getElementById('model-status').textContent=message}
async function refreshModelProviders(){
 const root=document.getElementById('model-admin');if(!root)return;
 root.replaceChildren();const section=document.createElement('section'),title=document.createElement('h2');title.textContent='AI model providers';section.append(title);root.append(section);
 let response;try{response=await api('admin/model-providers')}catch{const p=document.createElement('p');p.textContent='Model provider setup is not enabled on this deployment.';section.append(p);return}
 const provider=response.providers.find(p=>p.providerId==='codex')||{providerId:'codex',status:'disconnected',models:[],enabled:[],policyVersion:0};
 const status=document.createElement('p');status.id='model-status';status.textContent='OpenAI Codex · '+provider.status+(provider.accountLabel?' · '+provider.accountLabel:'');section.append(status);
 const connect=document.createElement('button');connect.textContent='Connect with device code';connect.onclick=async()=>{try{const challenge=await api('admin/model-providers/codex/connect','POST',{});showDeviceChallenge(section,challenge)}catch(e){modelNotice(e.message)}};section.append(connect);
 const disconnect=document.createElement('button');disconnect.textContent='Disconnect';disconnect.disabled=provider.status!=='connected';disconnect.onclick=async()=>{try{await api('admin/model-providers/codex/disconnect','POST',{});await refreshModelProviders()}catch(e){modelNotice(e.message)}};section.append(disconnect);
 const discover=document.createElement('button');discover.textContent='Refresh available models';discover.disabled=provider.status!=='connected';discover.onclick=async()=>{try{await api('admin/model-providers/codex/discover','POST',{});await refreshModelProviders()}catch(e){modelNotice(e.message)}};section.append(discover);
 if(provider.models?.length){const form=document.createElement('form'),heading=document.createElement('h3');heading.textContent='Models';form.append(heading);const defaultSelect=document.createElement('select');defaultSelect.name='defaultModelId';const empty=document.createElement('option');empty.value='';empty.textContent='No default';defaultSelect.append(empty);
 for(const m of provider.models){const row=document.createElement('label'),box=document.createElement('input');box.type='checkbox';box.value=m.modelId;box.checked=provider.enabled?.includes(m.modelId);box.disabled=!m.available;row.append(box,document.createTextNode(' '+m.displayName+(m.available?'':' (unavailable)')));form.append(row,document.createElement('br'));if(m.available){const option=document.createElement('option');option.value=m.modelId;option.textContent=m.displayName;defaultSelect.append(option)}}
 defaultSelect.value=provider.defaultModelId||'';form.append(document.createTextNode('Default '),defaultSelect);const save=document.createElement('button');save.textContent='Save enabled models';form.append(save);form.onsubmit=async e=>{e.preventDefault();const enabled=[...form.querySelectorAll('input:checked')].map(x=>x.value);try{await api('admin/model-providers/codex/models','PUT',{expectedVersion:provider.policyVersion,enabled,defaultModelId:defaultSelect.value});await refreshModelProviders()}catch(error){modelNotice(error.message);await refreshModelProviders()}};section.append(form)}
}
function showDeviceChallenge(section,challenge){
 clearInterval(modelPoll);const panel=document.createElement('div'),link=document.createElement('a'),code=document.createElement('strong'),expiry=document.createElement('p'),cancel=document.createElement('button');
 link.href=challenge.verificationUrl;link.target='_blank';link.rel='noopener noreferrer';link.textContent='Open verification page';code.textContent=' Code: '+challenge.userCode;expiry.textContent='Expires: '+new Date(challenge.expiresAt).toLocaleString();cancel.textContent='Cancel';panel.append(link,code,expiry,cancel);section.append(panel);
 cancel.onclick=async()=>{clearInterval(modelPoll);try{await api('admin/model-providers/codex/connect/'+encodeURIComponent(challenge.sessionId),'DELETE');panel.remove();modelNotice('Sign-in cancelled')}catch(e){modelNotice(e.message)}};
 const poll=async()=>{try{const state=await api('admin/model-providers/codex/connect/'+encodeURIComponent(challenge.sessionId));if(state.status==='pending')return;clearInterval(modelPoll);panel.remove();if(state.status==='connected'){await refreshModelProviders()}else modelNotice('Sign-in '+state.status)}catch(e){clearInterval(modelPoll);modelNotice(e.message)}};
 modelPoll=setInterval(poll,2000);
}
</script>`
