const categories=new Set(['local-selector','local-precondition','transport-error','remote-rpc-error','remote-tool-error','local-evidence','remote-ScopeMismatch','remote-ProviderUnavailable','remote-InvalidInput','remote-IdentityChanged','remote-NotConfigured','remote-Unauthenticated']);

// Only a known tool identity and a fixed category cross the public progress boundary.
// Inspect just the SDK validation prefix; never copy its argument-bearing result text.
export function toolFailureDiagnostic(name,profile,category,result){
 const match=/^mcp__(raptor|infra)__([a-z][a-z0-9_]*)$/.exec(name);
 if(!match||!profile[match[1]]?.includes(match[2]))return undefined;
 const sdkValidation=result?.content?.some(block=>block?.type==='text'&&typeof block.text==='string'&&block.text.startsWith(`Validation failed for tool "${name}":\n`));
 return {server:match[1],tool:match[2],category:category&&(categories.has(category)||/^http-[45][0-9]{2}$/.test(category))?category:sdkValidation?'sdk-arguments':'sdk-error'};
}

// Never publish provider error text, headers, URLs, or credential material.
export function modelFailureDiagnostic(message,model){
 if(message?.role!=='assistant')return undefined;
 if(message.model!==model.id||message.provider!==model.provider)return {category:'response-identity',failureCode:'ModelFailed'};
 if(!['error','aborted'].includes(message.stopReason))return undefined;
 if(message.stopReason==='aborted')return {category:'aborted',failureCode:'ModelFailed'};
 const text=typeof message.errorMessage==='string'?message.errorMessage.slice(0,16384):'';
 if(/invalid_grant|invalid token|\b401\b|unauthorized|unauthenticated|(?:token|authentication|credential).{0,80}(?:expired|invalidated|revoked)|(?:expired|invalid|revoked).{0,40}(?:token|credential)|sign[ -]?in again/i.test(text))
  return {category:'authentication',failureCode:'NeedsSignIn'};
 if(/\b429\b|usage.limit|rate.limit/i.test(text))return {category:'usage-limit',failureCode:'ModelFailed'};
 if(/\b403\b|forbidden/i.test(text))return {category:'access-denied',failureCode:'ModelFailed'};
 if(/timed?\s*out|deadline|ETIMEDOUT/i.test(text))return {category:'timeout',failureCode:'ModelFailed'};
 if(/fetch failed|ECONNRESET|ECONNREFUSED|ENOTFOUND|WebSocket stream closed/i.test(text))return {category:'transport',failureCode:'ModelFailed'};
 if(/\b50[0234]\b|server_error/i.test(text))return {category:'provider-server',failureCode:'ModelFailed'};
 return {category:'provider-error',failureCode:'ModelFailed'};
}
