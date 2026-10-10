import test from 'node:test';
import assert from 'node:assert/strict';
import {runLogin} from './model-auth-helper.mjs';

test('Codex helper selects device code and emits only a challenge then credential',async()=>{
 const lines=[];
 const runtime={async login(provider,type,interaction){
  assert.equal(provider,'openai-codex');assert.equal(type,'oauth');
  assert.equal(await interaction.prompt({type:'select'}),'device_code');
  interaction.notify({type:'device_code',userCode:'ABCD-EFGH',verificationUri:'https://auth.openai.com/codex/device',expiresInSeconds:900});
  return {type:'oauth',access:'synthetic-access',refresh:'synthetic-refresh',expires:1234};
 }};
 await runLogin(runtime,value=>lines.push(value));
 assert.deepEqual(lines.map(value=>value.type),['challenge','credential']);
 assert.equal(lines[0].userCode,'ABCD-EFGH');
 assert.equal(lines[0].credential,undefined);
 assert.equal(lines[1].credential.refresh,'synthetic-refresh');
});

test('Codex helper suppresses provider error bodies',async()=>{
 const lines=[];
 await runLogin({login:async()=>{throw Error('private provider body')}},value=>lines.push(value));
 assert.deepEqual(lines,[{type:'failure',code:'ProviderAuthorizationFailed'}]);
});
