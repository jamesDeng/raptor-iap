"""Native retained-window assessment. Pure readers cannot grant runtime admission."""
import os
from pathlib import Path
import time
from segmented_capture import canonical,sha,decode_private,open_directory,read_progress,read_frame
from pgcat_native_ingress import require,exact,_identity,_Chain,validate_withdrawal_witness,validate_retirement_witness,instant,_result,_approval,validate_causal_read


def _original(run,artifact):
    from pgcat_native_controller import file_identity
    exact(artifact,{'basename','identity'});file_identity(artifact['identity'])
    require(type(artifact['basename']) is str and Path(artifact['basename']).name==artifact['basename'])
    raw,identity=_identity(run/artifact['basename'],32*1024*1024);require(identity==artifact['identity'])
    return decode_private(raw),sha(raw)


def _operation_assessment(chain,operation,ingresses):
    from pgcat_native_closure import _coverage_prefix
    from capture_window import seconds
    progress,_,_=read_progress(chain.run/'native-capture',chain.context)
    pair,_=_coverage_prefix(chain,progress)
    records=[_original(chain.run,a) for a in ingresses]
    final=[r for r,d in records if d==operation['ingressSha256']];require(len(final)==1);final=final[0]
    if chain.plan['executionKind']=='disabled-proof':
        causal=validate_causal_read(final,chain);require(causal['status']=='VERIFIED')
        _causal_coverage(chain,progress,[causal]);return
    require(all(r['context']==final['context'] for r,d in records))
    execution=final['execution'];result=_result(execution['result'],chain.plan,operation['requestId'],execution['attemptId'])
    require(sha(canonical(result))==operation['resultSha256'] and pair==sorted(result['replacement']['newInstanceIds']))
    events=final['events'];require(sha(canonical(events))==operation['eventsSha256'] and all(e['evidenceMode']=='live' for e in events))
    starts=[e for e in events if e['kind']=='tool_start'];restarts=[e for e in starts if e['details'].get('tool')=='mcp__infra__deployment_restart']
    scales=[e for e in starts if e['details'].get('tool')=='mcp__infra__db_proxy_scale'];require(len(restarts)==1 and len(scales)==2)
    restart=restarts[0];require(scales[0]['sequence']<restart['sequence']<scales[1]['sequence'])
    witnesses=[]
    for record,digest in records:
        if record['withdrawalWitness'] is None:continue
        witness,derived=validate_withdrawal_witness(record,chain)
        prefix=[e for e in events if e['sequence']<=witness['eventCursor']]
        require(prefix==record['events'] and derived==pair)
        require(restart['sequence']>witness['eventCursor'] and restart['attemptId']!=witness['attemptId'])
        witnesses.append(witness)
    require(witnesses)
    retiring=[]
    for record,digest in records:
        if record['retirementWitness'] is None:continue
        witness,derived=validate_retirement_witness(record,chain)
        require([e for e in events if e['sequence']<=witness['eventCursor']]==record['events'] and derived==pair)
        require(scales[1]['sequence']>witness['eventCursor'] and scales[1]['attemptId']!=witness['attemptId'] and witness['actionId']==result['replacement']['actions'][1]['actionId'])
        require(any(a['approvalId']==witness['approvalId'] and a['actionId']==witness['actionId'] and a['state']=='approved' for a in final['approvals']))
        retiring.append(validate_causal_read(record,chain))
    require(retiring)
    require(any(_approval(dict(a,guidance=None),a['approvalId'],chain.plan,operation['requestId'],result)==a for a in final['approvals']))
    _causal_coverage(chain,progress,retiring)


def _causal_coverage(chain,progress,receipts):
    """Every later discovered pool/user must also exist in the original read."""
    from pgcat_native_evidence import _replay
    groups={(r['node'],r['pool'],r['user']) for r in chain.baseline['metricGroups']}
    originals=set(chain.plan['target']['originalInstanceIds'])
    def visit(frame,ref):
        groups.update((r['node'],r['pool'],r['user']) for r in frame['metrics']['raw'] if r['node'] in originals)
    fd=open_directory(chain.run/'native-capture')
    try:_replay(fd,chain.context,progress['latest'],visit=visit)
    finally:os.close(fd)
    require(any(groups<={(r['node'],r['pool'],r['user']) for r in receipt['evidence']['samples']['metricGroups']} for receipt in receipts))


def assess_native(run_dir):
    """Read all completed segments first; late evidence cannot erase SQL FAIL."""
    from segmented_capture import assess_manifest
    run=Path(run_dir);ordinary=assess_manifest(run/'native-capture');reasons=list(ordinary['reasons'])
    status='INCONCLUSIVE';operation_status='INCONCLUSIVE';collector=False;guardian=False
    fields=dict(operationBindingSha256=None,terminalSha256=None,stopSha256=None,finishSha256=None,manifestSha256=None)
    plan=decode_private(_identity(run/'native-plan.json')[0])
    try:
        from pgcat_native_closure import read_support
        from pgcat_native_evidence import read_terminal_clients
        from pgcat_collector_control import process_identity,completion
        chain=_Chain(run)
        anchors=decode_private(_identity(run/'native-assessment-input.json')[0]);exact(anchors,{'operation','supports','ingresses'})
        operation,fields['operationBindingSha256']=_original(run,anchors['operation'])
        supports={k:read_support(run,k,plan=plan,original=a) for k,a in anchors['supports'].items()}
        require({'native-finish','native-terminal-seal'}<=set(supports))
        stop=decode_private(_identity(run/'native-feed-stop.json')[0]);fields['stopSha256']=sha(canonical(stop))
        fields['finishSha256']=sha(canonical(supports['native-finish']))
        terminal=read_terminal_clients(run/'native-capture',chain.context,operation);fields['terminalSha256']=terminal['terminalSha256']
        fields['manifestSha256']=completion(run,profile=plan['mode'])
        _operation_assessment(chain,operation,anchors['ingresses'])
        if plan['executionKind']=='disabled-proof':ordinary=assess_manifest(run/'native-capture',require_transition=False)
        job=run/'collector-job';exited=decode_private(_identity(job/'exit.json')[0])
        require(exited==dict(identity=chain.ready['collectorIdentity'],exitCode=0,category='completed',completionSha256=fields['manifestSha256']))
        # Separate independent process probes, never the guardian-written exit alone.
        collector=process_identity(chain.ready['collectorIdentity']['pid']) is None
        guardian=process_identity(chain.ready['guardianIdentity']['pid']) is None
        require(collector and guardian)
        operation_status='PASS';status=ordinary['status'];reasons=list(ordinary['reasons'])
    except ClockAgreementUnavailable:
        reasons.append('unverified request/capture/source clock agreement')
    except (OSError,ValueError,KeyError,TypeError,IndexError,OverflowError):
        reasons.append('incomplete native closure evidence')
    if ordinary['status']=='FAIL':status='FAIL'
    return dict(schemaVersion=1,runId=plan['runId'],executionKind=plan['executionKind'],planSha256=sha(canonical(plan)),status=status,
        ordinaryStatus=ordinary['status'],operationStatus=operation_status,reasons=sorted(set(reasons)),collectorAbsent=collector,guardianAbsent=guardian,**fields)


def complete_restored_history(snapshot):
    """A new process must expose all scheduled/terminal events from sequence one."""
    from capture_window import COUNTERS,seconds
    sample=snapshot['sample'];events=snapshot['events'];sequence=sample['sequence']
    require(type(sequence) is int and 0<sequence<=8192 and [e['sequence'] for e in events]==list(range(1,sequence+1)))
    counts={k:0 for k in COUNTERS};pending=set();seen=set()
    for e in events:
        require(e['processId']==sample['processId'] and seconds(e['at'])<=seconds(sample['at'])+1)
        kind=e['kind'];op=e.get('operationId')
        if kind=='scheduled':
            require(op and op not in seen);seen.add(op);pending.add(op);counts['Scheduled']+=1
        elif kind in ('operation','skipped'):
            require(op in pending);pending.remove(op)
            if kind=='skipped':counts['Skipped']+=1
            else:
                counts['Attempts']+=1;out=e.get('outcome','').capitalize();require(out in ('Success','Failure','Timeout','Ambiguous'));counts[out]+=1
        else:require(kind in ('connected','disconnected','connection_failure'))
    require(all(type(sample[k]) is int and sample[k]==counts[k] for k in COUNTERS))
    return True


class ClockAgreementUnavailable(ValueError):pass

def _timing_agreement(chain):
    # C4/root must establish supported actual timing agreement. Pod placement
    # alone does not establish AX/Kata runtime event vs Prom/worker clock identity.
    # There is deliberately no caller boolean, admission option or invented proof.
    raise ClockAgreementUnavailable('native timing agreement unavailable')
