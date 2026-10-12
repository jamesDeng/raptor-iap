import test from 'node:test';
import assert from 'node:assert/strict';
import {validateRepositoryJob} from './live-repository.mjs';
import {operationCapabilities,checkOperationSelectors} from './operation-profile.mjs';

const binding={requestId:'11111111-1111-4111-8111-111111111111',attemptId:'22222222-2222-4222-8222-222222222222',operation:'repository.change',objectKind:'application',envCode:'rdev.ali',skillsCommit:'a'.repeat(40)};
const row={purpose:'terraform',url:'https://github.com/example/infra.git',baseBranch:'main',baseCommit:'b'.repeat(40),path:'/workspace/repositories/repo-0'};

test('repository runner accepts only a bounded trusted clone map',()=>{
 assert.equal(validateRepositoryJob({binding,question:'Update the Terraform module'},[row]).manifest.length,1);
 assert.throws(()=>validateRepositoryJob({binding:{...binding,operation:'application.question'},question:'change'},[row]),/InvalidJob/);
 assert.throws(()=>validateRepositoryJob({binding,question:'change'},[{...row,url:'https://github.com/example/infra.git?token=secret'}]),/InvalidJob/);
 assert.throws(()=>validateRepositoryJob({binding,question:'change'},[{...row,path:'/workspace/../tmp'}]),/InvalidJob/);
});

test('repository tool profile permits only request-bound attachment and catalog reads',()=>{
 const profile=operationCapabilities(binding);
 assert.deepEqual(profile.infra,[]);
 assert.ok(profile.raptor.includes('pull_request_attach'));
 const args={requestId:binding.requestId,attemptId:binding.attemptId,repository:'owner/repo',number:7,url:'https://github.com/owner/repo/pull/7',headSha:'c'.repeat(40),baseCommit:'b'.repeat(40),baseBranch:'main',workingBranch:'raptor/change'};
 assert.doesNotThrow(()=>checkOperationSelectors('raptor','pull_request_attach',args,binding));
 assert.throws(()=>checkOperationSelectors('raptor','pull_request_attach',{...args,attemptId:'other'},binding),/ScopeMismatch/);
 assert.throws(()=>checkOperationSelectors('infra','deployment_restart',{},binding),/ScopeMismatch/);
});
