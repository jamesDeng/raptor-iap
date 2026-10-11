// This establishes operational convergence only. Full retained-window traffic
// and approval/gate assessment is a separate acceptance boundary.
const missing=()=>new Error('MissingEvidence');
const fresh=row=>row&&typeof row.observedAt==='string'&&Number.isFinite(Date.parse(row.observedAt))&&Math.abs(Date.now()-Date.parse(row.observedAt))<=30000;
export async function qualifyReplacementConvergence(trace,observer,scope){
 const original=trace.baseline()?.instanceIds,actions=trace.scaleActions();
 if(!observer||!Array.isArray(original)||original.length!==2||new Set(original).size!==2||trace.unresolved()||actions.length!==3||new Set(actions.map(a=>a.actionId)).size!==3)throw missing();
 const stages=[[2,4],[4,3],[3,2]];
 if(actions.some((a,i)=>!a.actionId||a.previousDesiredCapacity!==stages[i][0]||a.desiredCapacity!==stages[i][1]||!['submitted','unknown'].includes(a.outcome)))throw missing();
 const capacity=await observer.cloudRead({kind:'capacity'});
 if(!fresh(capacity)||capacity.groupId!==scope.groupId||capacity.desiredCapacity!==2||capacity.minSize!==2||capacity.maxSize!==4)throw missing();
 const fleet=await observer.cloudRead({kind:'fleet'}),nodes=fleet?.instances;
 if(!fresh(fleet)||fleet.groupId!==scope.groupId||!Array.isArray(nodes)||nodes.length!==2||new Set(nodes.map(n=>n.instanceId)).size!==2||nodes.some(n=>!n.instanceId||original.includes(n.instanceId)||n.protectedFromScaleIn!==true||n.lifecycleState!=='Protected'||n.healthStatus!=='Healthy'))throw missing();
 const ids=nodes.map(n=>n.instanceId).sort(),backend=await observer.cloudRead({kind:'backend'});
 if(!fresh(backend)||backend.serverGroupId!==scope.serverGroupId||!Array.isArray(backend.nodes)||backend.nodes.length!==2||new Set(backend.nodes.map(n=>n.instanceId)).size!==2||backend.nodes.some(n=>!ids.includes(n.instanceId)||n.registered!==true||n.healthy!==true))throw missing();
 for(const instanceId of ids){const probe=await observer.dbConnectionProbe({instanceId});if(probe.instanceId!==instanceId||probe.connected!==true)throw missing();}
 const app=await observer.deploymentRead({});
 if(!fresh(app)||['clusterId','namespace','name','uid'].some(k=>app[k]!==scope.application[k])||app.replicas!==2||['readyReplicas','updatedReplicas','availableReplicas'].some(k=>app[k]!==2))throw missing();
 // Recheck capacity after sequential reads; no successful mutation ack substitutes
 // for this observation. The collector independently assesses the whole window.
 const final=await observer.cloudRead({kind:'capacity'});
 if(!fresh(final)||final.groupId!==scope.groupId||final.desiredCapacity!==2||final.minSize!==2||final.maxSize!==4)throw missing();
 return {version:1,converged:true,acceptance:'pending',scope:structuredClone(scope),oldInstanceIds:[...original].sort(),newInstanceIds:ids,desiredCapacity:2,actions,observedAt:new Date().toISOString()};
}
