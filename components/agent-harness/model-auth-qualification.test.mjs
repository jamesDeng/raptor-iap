import test from 'node:test';
import assert from 'node:assert/strict';
import {ModelRuntime} from '@earendil-works/pi-coding-agent';
const {AuthStorage}=await import(new URL('./core/auth-storage.js',import.meta.resolve('@earendil-works/pi-coding-agent')));

const json = (value, status=200) => new Response(JSON.stringify(value), {status, headers:{'Content-Type':'application/json'}});
const token = `a.${Buffer.from(JSON.stringify({'https://api.openai.com/auth':{chatgpt_account_id:'synthetic-account'}})).toString('base64')}.c`;

async function fixture({cancel=false, expire=false}={}) {
 const originalFetch=globalThis.fetch, originalNow=Date.now;
 const controller=new AbortController(), events=[];
 let polls=0, clock=originalNow();
 globalThis.fetch=async (url) => {
  const path=new URL(url).pathname;
  if(path.endsWith('/deviceauth/usercode')) return json({device_auth_id:'fixture-device',user_code:'ABCD-EFGH',interval:0});
  if(path.endsWith('/deviceauth/token')) {
   polls++;
   if(expire&&polls===1) clock+=16*60*1000;
   if(polls===1) return json({error:'deviceauth_authorization_pending'},403);
   return json({authorization_code:'fixture-code',code_verifier:'fixture-verifier'});
  }
  if(path.endsWith('/oauth/token')) return json({access_token:token,refresh_token:'synthetic-refresh',expires_in:3600});
  throw new Error(`Unexpected URL: ${path}`);
 };
 if(expire) Date.now=()=>clock;
 try {
  const credentials=AuthStorage.inMemory();
  const runtime=await ModelRuntime.create({credentials,modelsPath:null,refreshOnCreate:false});
  const login=runtime.login('openai-codex','oauth',{signal:controller.signal,prompt:async prompt=>{
   assert.equal(prompt.type,'select');return 'device_code';
  },notify:event=>{events.push(event);if(cancel&&event.type==='device_code') controller.abort();}});
  if(cancel) await assert.rejects(login,error=>error.name==='AbortError'||/cancel/i.test(error.message));
  else if(expire) await assert.rejects(login,/timed out/i);
  else {const credential=await login;assert.equal(credential.type,'oauth');assert.equal(credential.accountId,'synthetic-account');}
  assert.deepEqual(events.filter(event=>event.type==='device_code').map(event=>[event.userCode,event.verificationUri]),[['ABCD-EFGH','https://auth.openai.com/codex/device']]);
  if(cancel||expire) assert.equal(await credentials.read('openai-codex'),undefined);
  else assert.equal((await credentials.read('openai-codex')).refresh,'synthetic-refresh');
 } finally {globalThis.fetch=originalFetch;Date.now=originalNow;}
}

test('public Pi login supports headless device code and polling',()=>fixture());
test('public Pi login cancels without storing a credential',()=>fixture({cancel:true}));
test('public Pi login expires without storing a credential',()=>fixture({expire:true}));
