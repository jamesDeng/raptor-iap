const categories=new Set(['local-selector','transport-error','remote-rpc-error','remote-tool-error','local-evidence','remote-ScopeMismatch','remote-ProviderUnavailable','remote-InvalidInput','remote-IdentityChanged','remote-NotConfigured','remote-Unauthenticated']);

// Only a known tool identity and a fixed category cross the public progress boundary.
// The SDK result can contain credentials or provider output and is never inspected.
export function toolFailureDiagnostic(name,profile,category,_result){
 const match=/^mcp__(raptor|infra)__([a-z][a-z0-9_]*)$/.exec(name);
 if(!match||!profile[match[1]]?.includes(match[2]))return undefined;
 return {server:match[1],tool:match[2],category:category&&(categories.has(category)||/^http-[45][0-9]{2}$/.test(category))?category:'sdk-error'};
}
