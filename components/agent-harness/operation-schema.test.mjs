import test from 'node:test';
import assert from 'node:assert/strict';
import {validateOperationToolSchema,operationToolSchema} from './operation-profile.mjs';
test('supported command schema changes refuse before runtime exposure',()=>{
 const schema=operationToolSchema('infra','db_proxy_scale');
 assert.doesNotThrow(()=>validateOperationToolSchema('infra','db_proxy_scale',schema));
 for(const bad of [undefined,{...schema,required:['envCode']},{...schema,additionalProperties:true},{...schema,properties:{...schema.properties,desiredCapacity:{type:'string'}}},{...schema,properties:{...schema.properties,extra:{type:'string'}}}])assert.throws(()=>validateOperationToolSchema('infra','db_proxy_scale',bad),/UnexpectedToolSchema/);
});
