package admin

const environmentPanel = `<h2>Environment groups</h2><form id="group-form"><input name="code" placeholder="Group code" required><input name="name" placeholder="Group name" required><button>Create group</button></form><h2>Environments</h2><form id="env-form"><input name="code" placeholder="Globally unique environment code" required><input name="groupCode" placeholder="Group code" required><input name="stage" placeholder="Stage" required><textarea name="config" rows="7" cols="65" required placeholder='{"cloud":"aliyun","region":"ap-southeast-1","accountId":"...","ackClusterId":"...","terraformRepo":"...","terraformPath":"infra/terraform","kubernetesRepo":"...","kubernetesPath":"infra/kubernetes"}'></textarea><label><input name="edit" type="checkbox">Edit existing environment</label><button>Save environment</button></form><pre id="environment-list"></pre>`
const environmentScript = `<script>
async function refreshEnvironments(){const [groups,envs]=await Promise.all([api('environment-groups'),api('environments')]);document.getElementById('environment-list').textContent=JSON.stringify({groups,environments:envs},null,2)}
document.getElementById('group-form').onsubmit=safely(async e=>{await api('environment-groups','POST',Object.fromEntries(new FormData(e.target)));e.target.reset();await refreshEnvironments()});
document.getElementById('env-form').onsubmit=safely(async e=>{const f=new FormData(e.target);const v={code:f.get('code'),groupCode:f.get('groupCode'),stage:f.get('stage'),config:JSON.parse(f.get('config'))};await api('environments'+(f.has('edit')?'/'+encodeURIComponent(v.code):''),f.has('edit')?'PATCH':'POST',v);await refreshEnvironments()});
refreshEnvironments().catch(()=>{});
</script>`
