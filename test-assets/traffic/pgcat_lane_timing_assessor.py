"""Bounded, address-free assessment of an authenticated finite no-scale capture."""
import hashlib
import json
import os
import re
import uuid
from pathlib import Path
from capture_window import assess, clean, observed_sql_failure, seconds
from pgcat_collector_control import check, decode, directory, read, valid_identity, write


def canonical(value):
    return json.dumps(value,sort_keys=True,separators=(',',':'),allow_nan=False).encode('ascii')


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def assess_lane_timing(capture_dir,collector_identity,target,run_context,source_hashes):
    issues=[];frames=[];segment_digests=[];failure=False;fd=None;window_start=None
    def issue(reason):
        if reason not in issues:issues.append(reason)
    result=dict(status='INCONCLUSIVE',category='lane-timing',reasons=issues)
    invalid=(OSError,ValueError,TypeError,KeyError,IndexError,OverflowError,UnicodeError,AttributeError)
    try:
        valid_identity(collector_identity)
        check(type(run_context) is dict and set(run_context)=={'runId','feedSession','completionSha256'})
        for key in ('runId','feedSession'):check(str(uuid.UUID(run_context[key]))==run_context[key])
        check(re.fullmatch('[0-9a-f]{64}',run_context['completionSha256']) is not None)
        check(type(source_hashes) is dict and source_hashes and all(type(k) is str and
              re.fullmatch('[A-Za-z0-9_-]{1,64}',k) is not None and type(v) is str and
              re.fullmatch('[0-9a-f]{64}',v) is not None for k,v in source_hashes.items()))
        originals=target['originalInstanceIds']
        check(type(originals) is list and len(originals)==2 and originals==sorted(set(originals)))
        fd=directory(capture_dir);raw=read(fd,'completion.json',2*1024*1024)
        check(hashlib.sha256(raw).hexdigest()==run_context['completionSha256'])
        manifest=decode(raw);segments=manifest['segments']
        check(type(segments) is list and 1<=len(segments)<=71)
        if not (type(manifest.get('version')) is int and manifest['version']==1 and
                manifest.get('completed') is True and manifest.get('cancelled') is False and
                type(manifest.get('maxTotalSeconds')) is int and manifest['maxTotalSeconds']==65 and
                type(manifest.get('pollSeconds')) is int and manifest['pollSeconds']==1 and
                type(manifest.get('maxSegmentSeconds')) is int and 1<=manifest['maxSegmentSeconds']<=1800 and
                type(manifest.get('missingPollCount')) is int and manifest['missingPollCount']==0 and
                manifest.get('errors')==[]):issue('incomplete finite capture')
        for index,segment in enumerate(segments,1):
            try:
                name='segment-%04d.jsonl'%index
                check(segment['path']==segment['basename']==name)
                body=read(fd,name,16*1024*1024)
                check(hashlib.sha256(body).hexdigest()==segment['sha256'])
                segment_digests.append(dict(path=name,sha256=segment['sha256']))
                lines=body.splitlines()
                if not 1<=len(lines)<=71:issue('invalid segment frame count')
                actual=[]
                for line in lines:
                    try:
                        check(len(line)<=2*1024*1024)
                        source=decode(line)
                        if window_start is None:
                            try:window_start=seconds(source['at'])
                            except invalid:pass
                        failure=observed_sql_failure(source,window_start) or failure
                        # Scan authenticated connectivity events before stricter
                        # metadata checks so an incomplete interval cannot hide failure.
                        for pod in source.get('pods',[]):
                            try:
                                snapshot=pod['snapshot'];pid=snapshot['sample']['processId']
                                check(type(pod['uid']) is str and pod['uid'] and type(pid) is str and pid)
                                for event in snapshot['events']:
                                    try:
                                        if (event.get('processId')==pid and type(event.get('sequence')) is int and
                                            event['sequence']>0 and window_start is not None and
                                            window_start<=seconds(event['at'])<=seconds(source['at'])+1):
                                            if event.get('kind')=='connection_failure':failure=True
                                            elif event.get('kind') in ('connected','disconnected') and seconds(event['at'])>window_start:
                                                issue('unexplained connection turnover')
                                    except invalid:issue('malformed connectivity event')
                            except invalid:issue('malformed connectivity interval')
                        frame=clean(source);actual.append(frame['at']);frames.append(frame)
                    except invalid:issue('malformed retained interval')
                if (type(segment['frames']) is not int or len(actual)!=segment['frames'] or not actual or
                    seconds(actual[0])!=seconds(segment['firstAt']) or
                    seconds(actual[-1])!=seconds(segment['lastAt']) or
                    seconds(actual[-1])-seconds(actual[0])>=manifest['maxSegmentSeconds']):
                    issue('incomplete segment interval')
            except invalid:issue('unauthenticated or missing segment')
        check(len(frames)<=71)
        shared=assess(frames,completed=manifest.get('completed') is True,
                      cancelled=manifest.get('cancelled') is not False,require_transition=False)
        failure=failure or shared['status']=='FAIL'
        for reason in shared['reasons']:issue(reason)
        measurement=[f for f in frames if f['phase']=='measurement']
        tail=[f for f in frames if f['phase']=='settling']
        if (not measurement or frames != measurement+tail or
            not 60<=seconds(measurement[-1]['at'])-seconds(measurement[0]['at'])<=65):
            issue('insufficient authenticated timing coverage')
        if tail and (not measurement or
                     seconds(tail[-1]['at'])-seconds(tail[0]['at'])>5 or
                     seconds(tail[-1]['at'])-seconds(measurement[0]['at'])>70):
            issue('invalid terminal settling tail')
        roster=None;deployment=None;previous_inventory=None;first_success={};last_success={}
        for f in frames:
            try:
                now=seconds(f['at']);inv=f['inventory'];met=f['metrics'];pods=f['pods'];dep=f['deployment']
                check(f['phase'] in ('measurement','settling') and f['expectedReplicas']==2)
                check(inv['schemaVersion']==2 and inv['mode']=='disabled-rehearsal' and
                      inv['feedSession']==run_context['feedSession'] and
                      type(inv['sequence']) is int and inv['sequence']>0 and
                      -1<=now-seconds(inv['observedAt'])<30)
                check(inv['desired']==2 and inv['members']==originals and inv['healthyRegistered']==originals)
                check(inv['memberStates']==[dict(id=n,lifecycle='Protected',health='Healthy',addressKnown=True) for n in originals])
                check(inv['backendStates']==[dict(id=n,status='Available',weight=100,listenerHealth='Normal') for n in originals])
                if previous_inventory is not None:
                    if inv['sequence']==previous_inventory['sequence']:check(inv==previous_inventory)
                    else:check(inv['sequence']==previous_inventory['sequence']+1 and
                               seconds(inv['observedAt'])>seconds(previous_inventory['observedAt']))
                previous_inventory=inv
                check(dep['uid'] and type(dep['generation']) is int and dep['generation']>0 and
                      dep['generation']==dep['observedGeneration'] and dep['desired']==dep['ready']==2)
                if deployment is None:deployment=dep
                check(dep==deployment and len(pods)==2 and all(p['ready'] is True for p in pods))
                current=sorted((p['uid'],p['name'],p['containerId'],p['snapshot']['sample']['processId']) for p in pods)
                check(all(all(type(v) is str and v for v in row) for row in current) and
                      all(len(set(row[i] for row in current))==2 for i in range(4)))
                if roster is None:roster=current
                check(current==roster)
                for p in pods:
                    check(p['snapshot']['final'] is False)
                    sample=p['snapshot']['sample'];key=p['uid']
                    first_success.setdefault(key,sample['Success']);last_success[key]=sample['Success']
                check(met['available'] is True and sorted(met['nodes'])==originals and
                      set(met['upSourceAt'])==set(originals) and
                      all(-1<=now-seconds(met['upSourceAt'][n])<=45 for n in originals))
            except invalid:issue('incomplete or changed lane observation')
        if len(first_success)!=2 or any(last_success[n]<=first_success[n] for n in first_success):
            issue('client SQL did not advance')
        result.update(runId=run_context['runId'],feedSession=run_context['feedSession'],
            collectorIdentity=collector_identity,completionSha256=run_context['completionSha256'],
            segments=segment_digests,targetSha256=digest(target),sourceSha256=digest(source_hashes),
            clientIdentitySha256=digest(dict(roster=roster,deployment=deployment)))
        result['status']='FAIL' if failure else 'INCONCLUSIVE' if issues else 'PASS'
    except invalid:
        issue('unavailable authenticated capture binding')
        result['status']='FAIL' if failure else 'INCONCLUSIVE'
    finally:
        if fd is not None:os.close(fd)
    return result


def seal_verdict(run,run_id,target,run_context,source_hashes):
    """One-use private worker result, tied to the supervisor's prior exact exit."""
    fd=directory(Path(run)/'collector-job')
    try:
        check(run_context['runId']==run_id)
        identity=decode(read(fd,'identity.json'));valid_identity(identity)
        check(decode(read(fd,'observed.json'))==identity)
        sealed_exit=decode(read(fd,'exit.json'))
        check(type(sealed_exit) is dict and type(sealed_exit.get('exitCode')) is int and
              sealed_exit==dict(identity=identity,exitCode=0,category='completed',
              completionSha256=run_context['completionSha256']))
        check(identity==run_context['collectorIdentity'])
        write(fd,'lane-verdict-claim.json',dict(runId=run_id,feedSession=run_context['feedSession']))
        context={k:v for k,v in run_context.items() if k!='collectorIdentity'}
        verdict=assess_lane_timing(Path(run)/'collector-capture',identity,target,context,source_hashes)
        write(fd,'lane-verdict.json',verdict)
        return dict(status=verdict['status'],category='lane-timing',verdictSha256=digest(verdict))
    finally:os.close(fd)
