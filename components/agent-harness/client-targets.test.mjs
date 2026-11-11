import test from 'node:test';import assert from 'node:assert/strict';
import {discoverClientPods,discoverClientTargets} from './client-targets.mjs';
const app={namespace:'test',name:'traffic',uid:'deployment-uid'};
const deployment={metadata:{...app},spec:{replicas:1,selector:{matchLabels:{app:'traffic'}}}};
const rs={metadata:{name:'traffic-new',uid:'rs-uid',namespace:'test',ownerReferences:[{kind:'Deployment',name:'traffic',uid:app.uid,controller:true}]}};
const pod={metadata:{name:'traffic-new-pod',uid:'pod-uid',namespace:'test',labels:{app:'traffic'},ownerReferences:[{kind:'ReplicaSet',name:rs.metadata.name,uid:rs.metadata.uid,controller:true}]},status:{phase:'Running',podIP:'10.1.2.3',conditions:[{type:'Ready',status:'True'}]}};
const reader=(mutate=()=>{})=>async(kind)=>{const row=structuredClone(kind==='deployment'?deployment:kind==='replicasets'?{items:[rs]}:{items:[pod]});mutate(kind,row);return row;};
test('client targets follow current Deployment-owned Ready Pods',async()=>{assert.deepEqual(await discoverClientTargets(app,reader()),['10.1.2.3:9090']);});
test('foreign owners, missing replicas and unready clients cannot certify traffic',async()=>{
 for(const scenario of ['foreign','missing','unready','deleting','public','duplicate']) await assert.rejects(discoverClientTargets(app,reader((kind,row)=>{if(kind!=='pods')return;if(scenario==='foreign')row.items[0].metadata.ownerReferences[0].uid='other';if(scenario==='missing')row.items=[];if(scenario==='unready')row.items[0].status.conditions=[];if(scenario==='deleting')row.items[0].metadata.deletionTimestamp='now';if(scenario==='public')row.items[0].status.podIP='8.8.8.8';if(scenario==='duplicate')row.items.push(structuredClone(row.items[0]));})),/InvalidObservation/);
});
test('traffic verification excludes only the tracked terminating pods',async()=>{
 const withOldPod=mutate=>reader((kind,row)=>{
  if(kind!=='pods')return;
  const old=structuredClone(pod);
  old.metadata.name='traffic-old-pod';old.metadata.uid='old-uid';old.metadata.deletionTimestamp='now';
  mutate?.(old);row.items.push(old);
 });
 assert.deepEqual(await discoverClientPods(app,withOldPod(),['old-uid']),[{name:pod.metadata.name,uid:pod.metadata.uid,ip:pod.status.podIP}]);
 await assert.rejects(discoverClientTargets(app,withOldPod()),/InvalidObservation/);
 await assert.rejects(discoverClientPods(app,withOldPod(),['different-uid']),/InvalidObservation/);
 await assert.rejects(discoverClientPods(app,withOldPod(old=>delete old.metadata.deletionTimestamp),['old-uid']),/InvalidObservation/);
 await assert.rejects(discoverClientPods(app,withOldPod(old=>old.metadata.ownerReferences[0].uid='foreign'),['old-uid']),/InvalidObservation/);
 await assert.rejects(discoverClientPods(app,withOldPod(old=>{
  const owner=old.metadata.ownerReferences[0];owner.name='unknown';delete owner.uid;
 }),['old-uid']),/InvalidObservation/);
});
