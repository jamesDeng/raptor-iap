import test from 'node:test';
import assert from 'node:assert/strict';
import {toolFailureDiagnostic} from './tool-failure-diagnostic.mjs';

test('failed tool endings produce bounded categories without copying result content',()=>{
 const secret='fixture-private-token';
 const profile={infra:['cloud_identity_get']};
 assert.deepEqual(toolFailureDiagnostic('mcp__infra__cloud_identity_get',profile,undefined,{content:[{type:'text',text:secret}]}),{server:'infra',tool:'cloud_identity_get',category:'sdk-error'});
 assert.deepEqual(toolFailureDiagnostic('mcp__infra__cloud_identity_get',profile,'remote-ScopeMismatch',{content:[{type:'text',text:secret}]}),{server:'infra',tool:'cloud_identity_get',category:'remote-ScopeMismatch'});
 assert.deepEqual(toolFailureDiagnostic('mcp__infra__cloud_identity_get',profile,undefined,{content:[{type:'text',text:'Validation failed for tool "mcp__infra__cloud_identity_get":\nReceived arguments: '+secret}]}),{server:'infra',tool:'cloud_identity_get',category:'sdk-arguments'});
 assert.deepEqual(toolFailureDiagnostic('mcp__infra__cloud_identity_get',profile,undefined,{content:[{type:'text',text:'Validation failed for tool "mcp__raptor__approval_request": '+secret}]}),{server:'infra',tool:'cloud_identity_get',category:'sdk-error'});
 assert.equal(toolFailureDiagnostic('mcp__infra__unlisted',profile,undefined,{content:[{type:'text',text:secret}]}),undefined);
 assert.equal(toolFailureDiagnostic('cloud_read',profile,undefined,{content:[{type:'text',text:secret}]}),undefined);
});

test('model errors expose only fixed categories and identify sign-in failures',async()=>{
 const {modelFailureDiagnostic}=await import('./tool-failure-diagnostic.mjs');
 const model={id:'gpt-6-luna',provider:'openai-codex'};
 const base={role:'assistant',model:model.id,provider:model.provider,stopReason:'error'};
 for(const errorMessage of ['Your access token has expired. private-token','invalid_grant private-token','HTTP 401 private-token','Your authentication token has been invalidated. Please sign in again.']){
  assert.deepEqual(modelFailureDiagnostic({...base,errorMessage},model),{category:'authentication',failureCode:'NeedsSignIn'});
 }
 assert.deepEqual(modelFailureDiagnostic({...base,errorMessage:'HTTP 429 private-token'},model),{category:'usage-limit',failureCode:'ModelFailed'});
 assert.deepEqual(modelFailureDiagnostic({...base,errorMessage:'Something private'},model),{category:'provider-error',failureCode:'ModelFailed'});
 assert.deepEqual(modelFailureDiagnostic({...base,errorMessage:'TurnLimit'},model),{category:'turn-limit',failureCode:'TurnLimit'});
 assert.deepEqual(modelFailureDiagnostic({...base,model:'foreign',errorMessage:'expired token'},model),{category:'response-identity',failureCode:'ModelFailed'});
 assert.equal(modelFailureDiagnostic({...base,stopReason:'stop'},model),undefined);
});
