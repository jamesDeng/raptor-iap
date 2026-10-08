"""Local scenario oracle, NEVER a provider adapter or authoritative approval gate.

Consumes synthetic fixture state. It makes no network calls or state changes.
Its provisional field names do not define the deployed Infra API contract.
"""
import math

def evaluate(state, action):
    reasons=[]
    def deny(reason): reasons.append(reason)
    if state.get('evidenceMode')!='simulated': deny('Synthetic rehearsal state required')
    for a,b in [('envCode','envCode'),('proxyCode','proxyCode'),('targetDbCode','target-db-code')]:
        if not action.get(a) or action.get(a)!=state.get(b): deny('Target identity mismatch')
    if state.get('operationStatus') not in ('new','succeeded'): deny('Reconcile unknown/pending/partial operation before mutation')
    if state.get('topologyChanged') or state.get('restartStatus','succeeded')!='succeeded': deny('Refresh topology and restart evidence')
    nodes=state.get('nodes',[])
    ids=[n.get('id') for n in nodes]
    if not ids or any(not n for n in ids) or len(set(ids))!=len(ids): deny('Invalid node inventory')
    kind=action.get('type')
    if kind in ('deregister','protection'):
        selected=action.get('nodeIds',[])
        if not selected or len(set(selected))!=len(selected) or any(n not in ids for n in selected): deny('Node membership mismatch')
        if kind=='deregister':
            desired=state.get('desiredCapacity',0)
            healthy=sum(bool(n.get('healthy') and n.get('registered') and n['id'] not in selected) for n in nodes)
            if not isinstance(desired,int) or isinstance(desired,bool) or desired<=0 or healthy<=desired/2: deny('Healthy registered remainder must be strictly greater than desired capacity / 2')
    elif kind=='scale':
        target=action.get('desiredCapacity')
        desired=state.get('desiredCapacity')
        if not isinstance(target,int) or isinstance(target,bool) or not isinstance(desired,int) or not 2<=target<=4: deny('Capacity outside bounded fixture range')
        elif target<desired:
            approval=state.get('approval',{})
            if not approval.get('granted') or approval.get('consumed'): deny('Fresh approval required')
            for key in ('requestId','attemptId','operationId','envCode','proxyCode'):
                if not state.get(key) or approval.get(key)!=state.get(key): deny('Approval identity mismatch')
            if approval.get('desiredCapacity')!=target: deny('Approval parameter mismatch')
            candidates=[n for n in nodes if not n.get('protected')]
            if len(candidates)<desired-target: deny('Insufficient unprotected capacity')
            for n in candidates:
                value=n.get('connectedClients'); observed=n.get('observedAt'); now=state.get('now')
                if not n.get('up') or not isinstance(value,(int,float)) or isinstance(value,bool) or not math.isfinite(value) or value!=0: deny('All unprotected nodes require valid zero connected-client metrics')
                if not isinstance(observed,(int,float)) or not isinstance(now,(int,float)) or not math.isfinite(observed) or not math.isfinite(now) or not 0<=now-observed<=15: deny('Missing/stale/future observation')
                # Deliberately no registration/routing-exclusion requirement: the owner accepted this race.
    else: deny('Unsupported rehearsal action')
    return {'status':'denied' if reasons else 'eligible','evidenceMode':'simulated','reasons':reasons,'executed':False}
