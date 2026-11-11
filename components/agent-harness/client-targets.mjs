import {isIP} from 'node:net';
const invalid=()=>new Error('InvalidObservation');
const owns=(metadata,kind,name,uid)=>metadata?.ownerReferences?.some(o=>o.controller===true&&o.kind===kind&&o.name===name&&o.uid===uid);
const privateIP=ip=>isIP(ip)===4&&(ip.startsWith('10.')||ip.startsWith('192.168.')||/^172\.(1[6-9]|2\d|3[01])\./.test(ip));
export async function discoverClientPods(app,read,retiringPodUids=[]){
 const deployment=await read('deployment');
 if(deployment.metadata?.uid!==app.uid||deployment.metadata?.name!==app.name||deployment.metadata?.namespace!==app.namespace||deployment.metadata.deletionTimestamp)throw invalid();
 const labels=deployment.spec?.selector?.matchLabels;
 if(!labels||!Object.keys(labels).length||Object.keys(labels).length>10||deployment.spec.selector.matchExpressions?.length||Object.entries(labels).some(([k,v])=>!/^[-A-Za-z0-9_./]+$/.test(k)||!/^[-A-Za-z0-9_.]+$/.test(v)))throw invalid();
 const replicas=deployment.spec.replicas;if(!Number.isInteger(replicas)||replicas<1||replicas>10)throw invalid();
 const selector=Object.entries(labels).map(([k,v])=>k+'='+v).sort().join(',');
 const sets=await read('replicasets',selector),pods=await read('pods',selector);
 const retiring=new Set(retiringPodUids);
 if(!Array.isArray(sets.items)||sets.items.length>20||!Array.isArray(pods.items)||pods.items.length>replicas+retiring.size)throw invalid();
 const owners=new Map();for(const rs of sets.items){if(rs.metadata?.namespace===app.namespace&&owns(rs.metadata,'Deployment',app.name,app.uid)){if(owners.has(rs.metadata.name)||!rs.metadata.uid)throw invalid();owners.set(rs.metadata.name,rs.metadata.uid);}}
 const addresses=new Set(),uids=new Set(),selected=[];
 for(const pod of pods.items){const m=pod.metadata,s=pod.status,owner=m?.ownerReferences?.find(o=>o.controller===true&&o.kind==='ReplicaSet');
  if(m?.namespace!==app.namespace||!m.uid||uids.has(m.uid)||!owner?.uid||owners.get(owner.name)!==owner.uid||Object.entries(labels).some(([k,v])=>m.labels?.[k]!==v))throw invalid();
  uids.add(m.uid);
  if(retiring.has(m.uid)&&m.deletionTimestamp)continue;
  if(retiring.has(m.uid)||m.deletionTimestamp||s?.phase!=='Running'||!s.conditions?.some(c=>c.type==='Ready'&&c.status==='True')||!privateIP(s.podIP)||addresses.has(s.podIP))throw invalid();
  addresses.add(s.podIP);selected.push({name:m.name,uid:m.uid,ip:s.podIP});
 }
 if(selected.length!==replicas)throw invalid();
 return selected.sort((a,b)=>a.ip.localeCompare(b.ip));
}

export async function discoverClientTargets(app,read){
 return (await discoverClientPods(app,read)).map(pod=>pod.ip+':9090');
}
