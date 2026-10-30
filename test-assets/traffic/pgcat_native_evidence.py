"""Opt-in native rolling evidence. Consistency witnesses, never authorization."""
import re
from segmented_capture import (SOURCE_KEYS, FLAGS, require, canonical, decode_private,
    integer, hash_value, validate_ref)
from capture_window import COUNTERS, seconds, clean_client_pods, observed_sql_failure

MODE = 'full-replacement-direct-v2'
CONTEXT_KEYS = {'schemaVersion','mode','executionKind','runId','feedSession',
    'providerRunContextSha256','bundleSha256','sourceHashes','baselineSha256','planSha256',
    'initialRoster','deployment','collectorIdentity'}
OWNER_KEYS = {'deploymentUid','replicaSetUid','templateHash','imageDigest','terminating'}
IMMUTABLE_OWNER_KEYS = OWNER_KEYS - {'terminating'}
IDENTITY_KEYS = {'name','uid','containerId','processId'}
STARTING_KEYS = {'name','uid','deploymentUid','replicaSetUid','templateHash','containerId'}
SNAPSHOT_KEYS = {'schemaVersion','cohort','successorReplicaSetUid','successorTemplateHash','observedGeneration','flags','startingPods'}


def detached(value):return decode_private(canonical(value))
def token(value):return type(value) is str and re.fullmatch(r'[A-Za-z0-9_.:/-]{1,256}',value) is not None


def validate_deployment(value):
    require(type(value) is dict and set(value)=={'name','namespace','uid','generation','initialReplicaSetUid','initialTemplateHash','imageDigest'})
    require(integer(value['generation'],1) and value['generation']<2**53)
    require(token(value['initialReplicaSetUid']) and token(value['initialTemplateHash']))
    require(type(value['imageDigest']) is str and re.fullmatch('sha256:[0-9a-f]{64}',value['imageDigest']))
    require(value['name']=='rdev-test-client-client' and value['namespace']=='raptor-test' and token(value['uid']))
    return detached(value)


def validate_context(value):
    from segmented_capture import validate_context as legacy_context
    require(type(value) is dict and set(value)==CONTEXT_KEYS)
    require(type(value['schemaVersion']) is int and value['schemaVersion']==2 and value['mode']==MODE)
    require(value['executionKind'] in ('native-replacement','disabled-proof') and hash_value(value['planSha256']))
    deployment=validate_deployment(value['deployment'])
    hashes=value['sourceHashes']
    require(type(hashes) is dict and set(hashes)==SOURCE_KEYS|{'nativeEvidence'} and hash_value(hashes['nativeEvidence']))
    legacy={key:value[key] for key in ('runId','feedSession','providerRunContextSha256','bundleSha256','baselineSha256','collectorIdentity')}
    legacy.update(schemaVersion=1, originalRoster=value['initialRoster'],
        deployment={key:deployment[key] for key in ('name','namespace','uid','generation')},
        sourceHashes={key:hashes[key] for key in SOURCE_KEYS})
    legacy_context(legacy)
    return detached(value)


def pod_identity(pod):
    sample=pod['snapshot']['sample']
    identity={key:pod[key] for key in ('name','uid','containerId')}
    identity['processId']=sample['processId']
    require(all(token(value) for value in identity.values()))
    return identity


def validate_owner(owner,context):
    require(type(owner) is dict and set(owner)==OWNER_KEYS)
    require(all(token(owner[key]) for key in IMMUTABLE_OWNER_KEYS) and type(owner['terminating']) is bool)
    require(owner['deploymentUid']==context['deployment']['uid'] and owner['imageDigest']==context['deployment']['imageDigest'])
    return owner


class NativeCohort:
    """Bound four immutable process identities; commit refs only after durable reread."""
    def __init__(self,context):
        self.context=validate_context(context)
        self.rows={};self.samples={};self.finals={};self.last_seen=set();self.starting={}
        self.flags={key:False for key in FLAGS}
        self.successor=None;self.generation=None;self.observed_generation=None;self.first_at=None

    def observe(self,frame):
        flags=self.flags
        if self.first_at is None:self.first_at=seconds(frame['at'])
        authenticated=[];seen=set();active=set()
        initial={row['uid']:row for row in self.context['initialRoster']}
        try:
            require(frame['inventory']['feedSession']==self.context['feedSession'] and frame['inventory']['mode']==MODE)
            require(type(frame['expectedReplicas']) is int and frame['expectedReplicas']==2)
            dep=frame['deployment'];generation=dep['generation'];base=self.context['deployment']['generation']
            require(type(dep) is dict and all(integer(dep[key]) for key in ('generation','observedGeneration','desired','ready')))
            if dep['uid']!=self.context['deployment']['uid'] or generation not in (base,base+1) or dep['desired']!=2 or (self.generation is not None and generation<self.generation):
                flags['identityChanged']=True
            else:self.generation=generation
            require(dep['observedGeneration']<=generation and dep['ready']<=4)
            require(type(frame['pods']) is list and 1<=len(frame['pods'])<=4)
        except (KeyError,TypeError,ValueError,OverflowError):flags['invalidEvidence']=True
        startup_seen=set()
        try:
            pending=frame.get('startingPods',[]);require(type(pending) is list)
            for startup in pending:
                validate_starting(startup,self.context);uid=startup['uid']
                require(uid not in initial and uid not in self.rows and uid not in startup_seen)
                require(self.generation==self.context['deployment']['generation']+1)
                pair=(startup['replicaSetUid'],startup['templateHash'])
                require(pair[0]!=self.context['deployment']['initialReplicaSetUid'] and pair[1]!=self.context['deployment']['initialTemplateHash'] and self.successor in (None,pair))
                old=self.starting.get(uid)
                if old is not None:
                    require(all(startup[key]==old[key] for key in STARTING_KEYS-{'containerId'}))
                    require(old['containerId'] is None or old['containerId']==startup['containerId'])
                require(len(set(initial)|set(self.rows)|set(self.starting)|{uid})<=4)
                self.successor=pair;self.starting[uid]=detached(startup);startup_seen.add(uid)
        except (KeyError,TypeError,ValueError):flags['invalidEvidence']=True
        converted=set()
        for pod in frame.get('pods',[]) if type(frame.get('pods')) is list else []:
            try:
                identity=pod_identity(pod);uid=identity['uid'];owner=validate_owner(pod['ownership'],self.context)
                require(uid not in seen and uid not in startup_seen);seen.add(uid)
                startup=self.starting.get(uid)
                if startup is not None:
                    require(startup['name']==identity['name'] and (startup['containerId'] is None or startup['containerId']==identity['containerId']))
                    require(all(startup[key]==owner[key] for key in ('deploymentUid','replicaSetUid','templateHash')))
                    converted.add(uid)
                require(len(set(initial)|set(self.rows)|set(self.starting)|{uid})<=4)
                if uid in initial:
                    require(identity==initial[uid])
                    require((owner['replicaSetUid'],owner['templateHash'])==(self.context['deployment']['initialReplicaSetUid'],self.context['deployment']['initialTemplateHash']))
                else:
                    require(self.generation==self.context['deployment']['generation']+1)
                    pair=(owner['replicaSetUid'],owner['templateHash'])
                    require(pair[0]!=self.context['deployment']['initialReplicaSetUid'] and pair[1]!=self.context['deployment']['initialTemplateHash'])
                    require(self.successor in (None,pair))
                    require(sum(key not in initial for key in self.rows)<2 or uid in self.rows)
                    self.successor=pair
                row=self.rows.get(uid)
                if row is None:
                    require(len(self.rows)<4 and all(all(identity[key]!=r['identity'][key] for key in IDENTITY_KEYS) for r in self.rows.values()))
                    row=dict(identity=identity,ownership=detached(owner),firstFrame=None,finalFrame=None);self.rows[uid]=row
                require(row['identity']==identity and all(row['ownership'][key]==owner[key] for key in IMMUTABLE_OWNER_KEYS))
                row['ownership']['terminating']=owner['terminating']
                authenticated.append(pod)
            except (KeyError,TypeError,ValueError,OverflowError):flags['identityChanged']=True;continue
            try:
                snap=pod['snapshot'];sample=snap['sample']
                require(type(pod['ready']) is bool and type(snap['final']) is bool)
                _,complete=clean_client_pods([pod]);require(complete)
                require(all(integer(sample[key]) for key in COUNTERS+('sequence',)))
                require(sample['Attempts']==sum(sample[key] for key in ('Success','Failure','Timeout','Ambiguous')) and sample['Scheduled']>=sample['Attempts']+sample['Skipped'])
                require(integer(snap['connected']) and -1<=seconds(frame['at'])-seconds(sample['at'])<=15)
                if sample['Skipped']:flags['skipped']=True
                old=self.samples.get(uid)
                if old is not None:require(all(sample[key]>=old[key] for key in COUNTERS+('sequence',)))
                if uid in self.finals:
                    require(snap['final'] and snap['connected']==0 and all(sample[key]==self.finals[uid][key] for key in COUNTERS+('sequence',)))
                oldseq=old['sequence'] if old is not None else sample['sequence'] if uid in initial else 0
                events=[event for event in snap['events'] if event['sequence']>oldseq]
                require(sample['sequence']-oldseq<=8192 and [event['sequence'] for event in events]==list(range(oldseq+1,sample['sequence']+1)))
                require(all(event['processId']==identity['processId'] and self.first_at<=seconds(event['at'])<=seconds(frame['at'])+1 for event in events))
                if snap['final']:
                    require(snap['connected']==0 and sample['Scheduled']==sample['Attempts']+sample['Skipped'])
                    self.finals.setdefault(uid,dict(sample))
                self.samples[uid]=dict(sample)
                if not snap['final']:active.add(uid)
            except (KeyError,TypeError,ValueError,OverflowError):flags['invalidEvidence']=True
        if frame.get('phase','measurement')=='measurement' and len(active)<2:
            flags['invalidEvidence']=True
        for uid in set(self.starting)-startup_seen-converted:flags['invalidEvidence']=True
        # Conversion becomes resolved only after its first frame is durable.
        if not self.last_seen and not set(initial).issubset(seen):flags['identityChanged']=True
        for uid in self.last_seen-seen:
            if self.rows[uid]['finalFrame'] is None:flags['invalidEvidence']=True
        self.last_seen=set(self.rows)&seen
        if observed_sql_failure(dict(frame,pods=authenticated),self.first_at):flags['sqlFailure']=True

    def committed(self,frame,ref):
        validate_ref(ref);require(ref['at']==frame['at'])
        dep=frame.get('deployment',{});generation=dep.get('generation');base=self.context['deployment']['generation']
        if type(generation) is int and generation in (base,base+1) and dep.get('uid')==self.context['deployment']['uid']:
            if self.observed_generation is not None and generation<self.observed_generation:self.flags['identityChanged']=True
            else:self.observed_generation=generation
        for pod in frame['pods']:
            try:
                identity=pod_identity(pod);row=self.rows[identity['uid']]
                require(row['identity']==identity and all(row['ownership'][key]==pod['ownership'][key] for key in IMMUTABLE_OWNER_KEYS))
                if row['firstFrame'] is None:row['firstFrame']=detached(ref)
                self.starting.pop(identity['uid'],None)
                sample=pod['snapshot']['sample']
                if pod['snapshot']['final'] is True and identity['uid'] in self.finals:
                    require(type(pod['snapshot']['connected']) is int and pod['snapshot']['connected']==0)
                    require(all(sample[key]==self.finals[identity['uid']][key] for key in COUNTERS+('sequence',)))
                    if row['finalFrame'] is None:row['finalFrame']=detached(ref)
            except (KeyError,TypeError,ValueError):self.flags['invalidEvidence']=True

    def snapshot(self):
        return detached(dict(schemaVersion=1,cohort=[self.rows[uid] for uid in sorted(self.rows)],
            successorReplicaSetUid=self.successor[0] if self.successor else None,
            successorTemplateHash=self.successor[1] if self.successor else None,
            observedGeneration=self.observed_generation,flags=self.flags,startingPods=[self.starting[uid] for uid in sorted(self.starting)]))


def validate_starting(value,context):
    require(type(value) is dict and set(value)==STARTING_KEYS)
    require(all(token(value[key]) for key in STARTING_KEYS-{'containerId'}) and (value['containerId'] is None or token(value['containerId'])))
    require(value['deploymentUid']==context['deployment']['uid'])


def validate_snapshot(value,context):
    require(type(value) is dict and set(value)==SNAPSHOT_KEYS and type(value['schemaVersion']) is int and value['schemaVersion']==1)
    require(type(value['flags']) is dict and set(value['flags'])==set(FLAGS) and all(type(v) is bool for v in value['flags'].values()))
    rows=value['cohort'];require(type(rows) is list and len(rows)<=4)
    require(value['observedGeneration'] is None or (type(value['observedGeneration']) is int and value['observedGeneration'] in (context['deployment']['generation'],context['deployment']['generation']+1)))
    for key in ('successorReplicaSetUid','successorTemplateHash'):require(value[key] is None or token(value[key]))
    require((value['successorReplicaSetUid'] is None)==(value['successorTemplateHash'] is None))
    starting=value['startingPods'];require(type(starting) is list and len(starting)<=2)
    for row in starting:validate_starting(row,context)
    require(len({row['uid'] for row in starting})==len(starting) and starting==sorted(starting,key=lambda row:row['uid']))
    require(len({row['uid'] for row in starting}|{row['identity']['uid'] for row in rows}|{row['uid'] for row in context['initialRoster']})<=4)
    seen=set()
    for row in rows:
        require(type(row) is dict and set(row)=={'identity','ownership','firstFrame','finalFrame'})
        identity=row['identity'];require(type(identity) is dict and set(identity)==IDENTITY_KEYS and all(token(v) for v in identity.values()))
        require(identity['uid'] not in seen);seen.add(identity['uid'])
        validate_owner(row['ownership'],context)
        for key in ('firstFrame','finalFrame'):
            if row[key] is not None:validate_ref(row[key])
        if row['finalFrame'] is not None:
            require(row['firstFrame'] is not None and seconds(row['firstFrame']['at'])<=seconds(row['finalFrame']['at']))
    require(rows==sorted(rows,key=lambda row:row['identity']['uid']))
    return detached(value)


def validate_operation_binding(value,context):
    import uuid
    context=validate_context(context)
    common={'schemaVersion','kind','planSha256','restartGeneration','ingressSha256'}
    native={'requestId','definitionSha256','skillsCommit','model','resultSha256','eventsSha256'}
    kind=context['executionKind']
    require(type(value) is dict and set(value)==common|(native if kind=='native-replacement' else {'restartReceiptSha256'}))
    require(type(value['schemaVersion']) is int and value['schemaVersion']==1 and value['kind']==kind and value['planSha256']==context['planSha256'])
    require(type(value['restartGeneration']) is int and value['restartGeneration']==context['deployment']['generation']+1)
    for key in ('planSha256','ingressSha256')+ (('definitionSha256','resultSha256','eventsSha256') if kind=='native-replacement' else ('restartReceiptSha256',)):
        require(hash_value(value[key]))
    if kind=='native-replacement':
        require(type(value['requestId']) is str and str(uuid.UUID(value['requestId']))==value['requestId'])
        require(type(value['skillsCommit']) is str and re.fullmatch('[0-9a-f]{40}',value['skillsCommit']))
        require(type(value['model']) is str and 1<=len(value['model'])<=256 and value['model'].strip()==value['model'] and all(32<=ord(c)<=126 for c in value['model']))
    return detached(value)


def _binding_bytes(fd,context,operation):
    import os
    from segmented_capture import read_private
    binding_raw=read_private(fd,'progress-binding.json');binding=decode_private(binding_raw)
    info=os.fstat(fd)
    require(binding==dict(context,captureDirectoryIdentity=dict(device=info.st_dev,inode=info.st_ino)))
    require(type(binding.get('schemaVersion')) is int)
    operation_raw=read_private(fd,'native-operation-binding.json')
    require(validate_operation_binding(decode_private(operation_raw),context)==operation)
    return binding_raw,operation_raw


def _replay(fd,context,latest):
    """Replay bounded actual frame bytes up to the sealed prefix, never its growing tail."""
    import os
    from segmented_capture import read_frame, private_info, sha, FRAME_LIMIT
    validate_ref(latest);cohort=NativeCohort(context);last=None;count=0;max_gap=0
    for number in range(1,int(latest['basename'][8:12])+1):
        name='segment-%04d.jsonl'%number
        item=os.open(name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=fd)
        try:
            info=os.fstat(item);private_info(info)
            end=latest['offset']+latest['length'] if name==latest['basename'] else info.st_size
            require(0<end<=info.st_size)
            with os.fdopen(os.dup(item),'rb') as stream:
                offset=0
                while offset<end:
                    raw=stream.readline(FRAME_LIMIT+1);require(0<len(raw)<=FRAME_LIMIT and raw.endswith(b'\n') and offset+len(raw)<=end)
                    value=decode_private(raw)
                    ref=dict(basename=name,device=info.st_dev,inode=info.st_ino,offset=offset,length=len(raw),sha256=sha(raw),at=value['at'])
                    value=read_frame(fd,ref)
                    if last is not None:
                        gap=seconds(value['at'])-seconds(last['at']);require(0<gap<=3);max_gap=max(max_gap,gap)
                    cohort.observe(value);cohort.committed(value,ref)
                    last=ref;offset+=len(raw);count+=1;require(count<=5406)
                require(offset==end)
        finally:os.close(item)
    require(last==latest)
    return cohort.snapshot(),count,max_gap


def _terminal_roster(frame,context,operation,snapshot):
    from finish_clients import validate_roster
    dep=frame['deployment'];generation=operation['restartGeneration']
    require(dep==dict(uid=context['deployment']['uid'],generation=generation,observedGeneration=generation,desired=2,ready=2))
    require(type(frame['expectedReplicas']) is int and frame['expectedReplicas']==2 and type(frame['pods']) is list and len(frame['pods'])==2)
    roster=[]
    initials={row['uid'] for row in context['initialRoster']}
    for pod in frame['pods']:
        identity=pod_identity(pod);owner=validate_owner(pod['ownership'],context)
        require(identity['uid'] not in initials and pod['ready'] is True and owner['terminating'] is False)
        require(owner['replicaSetUid']==snapshot['successorReplicaSetUid'] and owner['templateHash']==snapshot['successorTemplateHash'])
        require(pod['snapshot']['final'] is False)
        roster.append(identity)
    return validate_roster(sorted(roster,key=lambda row:row['uid']))


def _verify_refs(fd,context,snapshot,previous,latest):
    from segmented_capture import read_frame
    require(len(snapshot['cohort'])==4 and not snapshot['startingPods'] and not any(snapshot['flags'].values()))
    require(snapshot['observedGeneration']==context['deployment']['generation']+1)
    initial={r['uid']:r for r in context['initialRoster']};finals=[]
    for row in snapshot['cohort']:
        identity=row['identity'];require(row['firstFrame'] is not None)
        first=read_frame(fd,row['firstFrame'])
        require(any(pod_identity(pod)==identity for pod in first['pods']))
        if row['finalFrame'] is not None:
            final=read_frame(fd,row['finalFrame']);pod=next(p for p in final['pods'] if pod_identity(p)==identity)
            snap=pod['snapshot'];sample=snap['sample']
            require(snap['final'] is True and type(snap['connected']) is int and snap['connected']==0 and sample['Scheduled']==sample['Attempts']+sample['Skipped'] and sample['Skipped']==0)
        if identity['uid'] in initial:
            require(identity==initial[identity['uid']] and row['finalFrame'] is not None)
            require(seconds(row['finalFrame']['at'])<seconds(previous['at']))
            finals.append(dict(uid=identity['uid'],processId=identity['processId'],frame=row['finalFrame']))
    require(len(finals)==2)
    before=read_frame(fd,previous);after=read_frame(fd,latest)
    require(0<seconds(after['at'])-seconds(before['at'])<=3)
    if previous['basename']==latest['basename']:require(previous['offset']+previous['length']==latest['offset'])
    else:
        require(int(latest['basename'][8:12])==int(previous['basename'][8:12])+1 and latest['offset']==0)
        read_frame(fd,previous,tail=True)
    return before,after,finals


def seal_terminal_clients(capture_dir,context,operation_binding,live_read,*,now):
    import os
    from segmented_capture import open_directory, read_progress, check_directory, sha, publish_private
    from worker_capture import native_owner_evidence
    from finish_clients import validate_roster
    context=validate_context(context);operation=validate_operation_binding(operation_binding,context)
    progress,_,_=read_progress(capture_dir,context)
    require(not any(progress['flags'].values()) and progress['maxGapSeconds']<=3)
    require(progress['previous'] is not None and progress['latest'] is not None)
    fd=open_directory(capture_dir)
    try:
        binding_raw,operation_raw=_binding_bytes(fd,context,operation)
        snapshot,count,gap=_replay(fd,context,progress['latest'])
        require(snapshot==progress['nativeCohort'] and count==progress['committedFrames'])
        previous,latest,finals=_verify_refs(fd,context,snapshot,progress['previous'],progress['latest'])
        roster=_terminal_roster(previous,context,operation,snapshot)
        require(roster==_terminal_roster(latest,context,operation,snapshot))
        current=seconds(now())
        require(all(0<=current-seconds(frame['at'])<=3 for frame in (previous,latest)))
        live=live_read();require(type(live) is dict and set(live)=={'deployment','replicaSets','pods','roster'})
        owners=native_owner_evidence(live['deployment'],live['replicaSets'],live['pods'],context)
        require(validate_roster(live['roster'])==roster and set(owners)=={r['uid'] for r in roster})
        dep=live['deployment'];require(type(dep['metadata']['generation']) is int and type(dep['status']['observedGeneration']) is int and dep['metadata']['generation']==dep['status']['observedGeneration']==operation['restartGeneration'])
        require(type(dep['spec']['replicas']) is int and type(dep['status']['readyReplicas']) is int and dep['spec']['replicas']==dep['status']['readyReplicas']==2)
        for pod in live['pods']:
            uid=pod['metadata']['uid'];row=next(r for r in roster if r['uid']==uid)
            require(pod['metadata']['name']==row['name'] and pod['status']['containerStatuses'][0]['containerID']==row['containerId'])
            ready=[r for r in pod['status'].get('conditions',[]) if r.get('type')=='Ready']
            require(len(ready)==1 and ready[0].get('status')=='True')
            retained=next(p for p in latest['pods'] if p['uid']==uid)
            require(owners[uid]==retained['ownership'])
        # Recheck after independent live I/O; a changed tail requires a new seal attempt.
        fresh,_,_=read_progress(capture_dir,context);require(fresh==progress)
        require(_binding_bytes(fd,context,operation)==(binding_raw,operation_raw))
        current=seconds(now());require(all(0<=current-seconds(f['at'])<=3 for f in (previous,latest)))
        check_directory(fd,capture_dir)
        value=dict(schemaVersion=1,mode=MODE,executionKind=context['executionKind'],runId=context['runId'],planSha256=context['planSha256'],
            progressBindingSha256=sha(binding_raw),operationBindingSha256=sha(operation_raw),generation=operation['restartGeneration'],roster=roster,
            frames=dict(previous=progress['previous'],latest=progress['latest']),predecessorFinals=finals,cohort=snapshot['cohort'])
        raw=publish_private(fd,'terminal-clients.json',value);check_directory(fd,capture_dir)
        return dict(terminalSha256=sha(raw),roster=roster)
    finally:os.close(fd)


def read_terminal_clients(capture_dir,context,operation_binding):
    import os
    from segmented_capture import open_directory, read_private, check_directory, sha
    context=validate_context(context);operation=validate_operation_binding(operation_binding,context)
    fd=open_directory(capture_dir)
    try:
        binding_raw,operation_raw=_binding_bytes(fd,context,operation)
        raw=read_private(fd,'terminal-clients.json');value=decode_private(raw)
        require(type(value) is dict and set(value)=={'schemaVersion','mode','executionKind','runId','planSha256','progressBindingSha256','operationBindingSha256','generation','roster','frames','predecessorFinals','cohort'})
        require(raw==canonical(value) and type(value['schemaVersion']) is int and value['schemaVersion']==1)
        require(value['mode']==MODE and value['executionKind']==context['executionKind'] and value['runId']==context['runId'] and value['planSha256']==context['planSha256'])
        require(value['progressBindingSha256']==sha(binding_raw) and value['operationBindingSha256']==sha(operation_raw))
        require(type(value['generation']) is int and value['generation']==operation['restartGeneration'])
        refs=value['frames'];require(type(refs) is dict and set(refs)=={'previous','latest'})
        snapshot,_,_=_replay(fd,context,refs['latest'])
        require(value['cohort']==snapshot['cohort'])
        previous,latest,finals=_verify_refs(fd,context,snapshot,refs['previous'],refs['latest'])
        roster=_terminal_roster(previous,context,operation,snapshot)
        require(roster==value['roster']==_terminal_roster(latest,context,operation,snapshot) and finals==value['predecessorFinals'])
        require(_binding_bytes(fd,context,operation)==(binding_raw,operation_raw) and read_private(fd,'terminal-clients.json')==raw)
        check_directory(fd,capture_dir)
        return dict(terminalSha256=sha(raw),roster=roster)
    finally:os.close(fd)
