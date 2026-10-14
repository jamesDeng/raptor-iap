import test from 'node:test';
import assert from 'node:assert/strict';
import {toolFailureDiagnostic} from './tool-failure-diagnostic.mjs';

test('failed tool endings produce bounded categories without copying result content',()=>{
 const secret='fixture-private-token';
 const profile={infra:['cloud_identity_get']};
 assert.deepEqual(toolFailureDiagnostic('mcp__infra__cloud_identity_get',profile,undefined,{content:[{type:'text',text:secret}]}),{server:'infra',tool:'cloud_identity_get',category:'sdk-error'});
 assert.deepEqual(toolFailureDiagnostic('mcp__infra__cloud_identity_get',profile,'remote-ScopeMismatch',{content:[{type:'text',text:secret}]}),{server:'infra',tool:'cloud_identity_get',category:'remote-ScopeMismatch'});
 assert.equal(toolFailureDiagnostic('mcp__infra__unlisted',profile,undefined,{content:[{type:'text',text:secret}]}),undefined);
 assert.equal(toolFailureDiagnostic('cloud_read',profile,undefined,{content:[{type:'text',text:secret}]}),undefined);
});
