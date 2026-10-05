package admin

import "errors"

func fmtURL() error { return errors.New("invalid backend URL") }

const page = `<!doctype html><html><head><meta charset="utf-8"><title>Raptor Admin</title><style>body{font:15px system-ui;background:#f4f6f8;max-width:1000px;margin:40px auto}section{background:white;padding:24px;margin:20px 0;border:1px solid #ddd}input,select,button{padding:9px;margin:5px}pre{white-space:pre-wrap}#notice{color:#a21}</style></head><body><h1>Raptor Admin</h1><section><form id="login"><input name="username" placeholder="Username" required autocomplete="username"><input name="password" type="password" placeholder="Password" required autocomplete="current-password"><button>Sign in</button></form><button id="logout">Sign out</button></section><p id="notice"></p><section id="management" hidden><h2>Users</h2><form id="create"><input name="username" required placeholder="Username"><input name="password" type="password" minlength="12" required placeholder="New password"><select name="role"><option value="user">User</option><option value="admin">Admin</option></select><button>Create</button></form><ul id="users"></ul><div id="environment-admin"></div></section><script>
let csrf;async function api(path,method='GET',body){const r=await fetch('/api/v1/'+path,{method,headers:{'Content-Type':'application/json',...(csrf?{'X-CSRF-Token':csrf}:{})},...(body?{body:JSON.stringify(body)}:{})});const v=await r.json();if(!r.ok)throw Error(v.error?.code||'Unavailable');return v.data}
async function refresh(){const s=await api('session');csrf=s.csrf;if(s.user.role!=='admin')throw Error('Administrator access required');document.getElementById('management').hidden=false;const users=await api('users');const ul=document.getElementById('users');ul.replaceChildren();for(const u of users){const li=document.createElement('li');li.textContent=u.username+' · '+u.role;ul.append(li)}}
function safely(f){return async e=>{e.preventDefault();try{document.getElementById('notice').textContent='';await f(e)}catch(x){document.getElementById('notice').textContent=x.message}}}
document.getElementById('login').onsubmit=safely(async e=>{const d=Object.fromEntries(new FormData(e.target));const s=await api('login','POST',d);csrf=s.csrf;e.target.reset();await refresh()});
document.getElementById('create').onsubmit=safely(async e=>{await api('users','POST',Object.fromEntries(new FormData(e.target)));e.target.reset();await refresh()});
document.getElementById('logout').onclick=safely(async()=>{await api('logout','POST',{});document.getElementById('management').hidden=true;csrf=undefined});refresh().catch(()=>{});
</script></body></html>`
