package admin

const githubPATPanel = `<section class="section"><h2>GitHub personal access token</h2><p class="section-desc">Used by repository change attempts to fetch public repositories, push a branch, and open a pull request. The agent in its sandbox can read this token.</p><p id="github-pat-status" role="status">Checking…</p><form id="github-pat-form"><label class="field">Personal access token<input id="github-pat-input" type="password" required autocomplete="off" spellcheck="false"></label><div class="actions"><button class="button" type="submit">Save or replace</button><button class="button secondary" id="github-pat-delete" type="button">Delete token</button></div></form></section>`

const githubPATScript = `<script>
async function refreshGitHubPAT(){const s=await api('admin/github-pat');document.getElementById('github-pat-status').textContent=s.configured?'Configured for '+s.owner+'; verified '+new Date(s.verifiedAt).toLocaleString():'No token configured';document.getElementById('github-pat-delete').disabled=!s.configured}
document.getElementById('github-pat-form').addEventListener('submit',async e=>{e.preventDefault();const input=document.getElementById('github-pat-input');try{await api('admin/github-pat','PUT',{token:input.value});input.value='';await refreshGitHubPAT();document.getElementById("notice").textContent='Token saved'}catch(err){input.value='';document.getElementById("notice").textContent=err.message}});
document.getElementById('github-pat-delete').addEventListener('click',async()=>{try{await api('admin/github-pat','DELETE');await refreshGitHubPAT();document.getElementById("notice").textContent='Token deleted'}catch(err){document.getElementById("notice").textContent=err.message}});
</script>`
