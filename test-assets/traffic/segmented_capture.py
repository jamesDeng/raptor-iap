"""Private immutable segments assessed as one continuous evidence stream."""
import hashlib
import json
import os
import math
import re
import stat
import time
import uuid
from pathlib import Path
from capture_window import CaptureWindow, assess, seconds, clean, COUNTERS, observed_sql_failure, client_identity


def digest(path):
    hasher=hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda:stream.read(128*1024),b''):hasher.update(chunk)
    return hasher.hexdigest()


def private_json(path, value):
    fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as stream:
        json.dump(value,stream,allow_nan=False);stream.write('\n');stream.flush();os.fsync(stream.fileno())


# These private witnesses are bounded diagnostics, never a traffic verdict.
FRAME_LIMIT=2*1024*1024
RECORD_LIMIT=65536
FLAGS=('pollError','identityChanged','sqlFailure','skipped','invalidEvidence')
SOURCE_KEYS={'collectorControl','laneTimingAssessor','rehearsalWorker','rehearsalContract',
    'pendingProofWorker','preDrainProofWorker','zeroProofWorker','captureWindow',
    'finishClients','workerCapture','segmentedCapture'}
CONTEXT_KEYS={'schemaVersion','runId','feedSession','providerRunContextSha256','bundleSha256',
    'sourceHashes','baselineSha256','originalRoster','deployment','collectorIdentity'}


def require(value):
    if not value:raise ValueError('invalid collector witness')


def canonical(value):
    return json.dumps(value,sort_keys=True,separators=(',',':'),allow_nan=False).encode('ascii')


def sha(raw):return hashlib.sha256(raw).hexdigest()


def decode_private(raw):
    def unique(pairs):
        result={}
        for key,value in pairs:
            require(key not in result);result[key]=value
        return result
    def invalid(value):raise ValueError('invalid collector witness')
    return json.loads(raw.decode('utf-8'),object_pairs_hook=unique,parse_constant=invalid)


def number(value):return type(value) in (int,float) and math.isfinite(value)
def integer(value,minimum=0):return type(value) is int and value>=minimum
def hash_value(value):return type(value) is str and re.fullmatch('[0-9a-f]{64}',value) is not None


def validate_context(value):
    require(type(value) is dict and set(value)==CONTEXT_KEYS and type(value['schemaVersion']) is int and value['schemaVersion']==1)
    for key in ('runId','feedSession'):require(type(value[key]) is str and str(uuid.UUID(value[key]))==value[key])
    for key in ('providerRunContextSha256','bundleSha256','baselineSha256'):require(hash_value(value[key]))
    hashes=value['sourceHashes'];require(type(hashes) is dict and set(hashes)==SOURCE_KEYS and all(hash_value(v) for v in hashes.values()))
    roster=value['originalRoster'];fields={'name','uid','containerId','processId'}
    require(type(roster) is list and len(roster)==2)
    require(all(type(row) is dict and set(row)==fields and all(type(row[k]) is str and re.fullmatch(r'[A-Za-z0-9_.:/-]{1,256}',row[k]) for k in fields) for row in roster))
    require(all(len({row[k] for row in roster})==2 for k in fields) and roster==sorted(roster,key=lambda r:r['uid']))
    deployment=value['deployment'];require(type(deployment) is dict and set(deployment)=={'name','namespace','uid','generation'})
    require(deployment['name']=='rdev-test-client-client' and deployment['namespace']=='raptor-test' and type(deployment['uid']) is str and re.fullmatch(r'[A-Za-z0-9_.:/-]{1,256}',deployment['uid']) and integer(deployment['generation'],1))
    identity=value['collectorIdentity'];require(type(identity) is dict and set(identity)=={'pid','bootId','startTicks','commandSha256'})
    require(integer(identity['pid'],1) and integer(identity['startTicks']) and type(identity['bootId']) is str and re.fullmatch(r'[A-Za-z0-9_-]{1,256}',identity['bootId']) and hash_value(identity['commandSha256']))
    return decode_private(canonical(value))


def file_identity(info):
    return (info.st_dev,info.st_ino,info.st_size,info.st_mtime_ns,info.st_ctime_ns)


def private_info(info):
    require(stat.S_ISREG(info.st_mode) and info.st_uid==os.geteuid() and stat.S_IMODE(info.st_mode)==0o600 and info.st_nlink==1)


def open_directory(path):
    path=Path(os.path.abspath(str(path)));require('..' not in path.parts)
    fd=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:
        for part in path.parts[1:]:
            item=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=fd);os.close(fd);fd=item
        info=os.fstat(fd)
        require(info.st_uid==os.geteuid() and stat.S_IMODE(info.st_mode)==0o700)
        return fd
    except BaseException:os.close(fd);raise


def check_directory(fd,path):
    current=open_directory(path)
    try:require(file_identity(os.fstat(fd))[:2]==file_identity(os.fstat(current))[:2])
    finally:os.close(current)


def read_private(fd,name,limit=RECORD_LIMIT):
    item=os.open(name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=fd)
    try:
        before=os.fstat(item);private_info(before);require(before.st_size<=limit)
        raw=os.pread(item,limit+1,0)
        require(len(raw)==before.st_size and file_identity(before)==file_identity(os.fstat(item))==file_identity(os.stat(name,dir_fd=fd,follow_symlinks=False)))
        return raw
    finally:os.close(item)


def publish_private(fd,name,value,replace=False):
    raw=canonical(value);require(len(raw)<=RECORD_LIMIT)
    temporary='.'+name+'.'+uuid.uuid4().hex
    item=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600,dir_fd=fd)
    try:
        os.fchmod(item,0o600);remaining=raw
        while remaining:
            count=os.write(item,remaining);require(count>0);remaining=remaining[count:]
        os.fsync(item)
    finally:os.close(item)
    try:
        if replace:
            private_info(os.stat(name,dir_fd=fd,follow_symlinks=False))
            os.rename(temporary,name,src_dir_fd=fd,dst_dir_fd=fd)
        else:
            os.link(temporary,name,src_dir_fd=fd,dst_dir_fd=fd,follow_symlinks=False)
            os.unlink(temporary,dir_fd=fd)
        os.fsync(fd)
    finally:
        try:os.unlink(temporary,dir_fd=fd)
        except FileNotFoundError:pass
    return raw


def validate_ref(ref):
    require(type(ref) is dict and set(ref)=={'basename','device','inode','offset','length','sha256','at'})
    require(type(ref['basename']) is str and re.fullmatch(r'segment-[0-9]{4}\.jsonl',ref['basename']) and 1<=int(ref['basename'][8:12])<=5406)
    require(integer(ref['device']) and integer(ref['inode'],1) and integer(ref['offset']) and integer(ref['length'],1) and ref['length']<=FRAME_LIMIT and hash_value(ref['sha256']))
    require(type(ref['at']) is not bool);seconds(ref['at'])


def read_frame(fd,ref,tail=False):
    validate_ref(ref);item=os.open(ref['basename'],os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=fd)
    try:
        before=os.fstat(item);private_info(before)
        end=ref['offset']+ref['length']
        require((before.st_dev,before.st_ino)==(ref['device'],ref['inode']) and before.st_size>=end and (not tail or before.st_size==end))
        raw=os.pread(item,ref['length'],ref['offset'])
        require(len(raw)==ref['length'] and raw.endswith(b'\n') and raw.count(b'\n')==1 and sha(raw)==ref['sha256'])
        value=decode_private(raw);require(type(value) is dict and value.get('at')==ref['at'])
        require(file_identity(before)==file_identity(os.fstat(item))==file_identity(os.stat(ref['basename'],dir_fd=fd,follow_symlinks=False)))
        return value
    finally:os.close(item)


def read_progress(directory,expected_context):
    expected=validate_context(expected_context);fd=open_directory(directory)
    try:
        binding_raw=read_private(fd,'progress-binding.json');binding=decode_private(binding_raw)
        identity=os.fstat(fd)
        require(type(binding) is dict and set(binding)==CONTEXT_KEYS|{'captureDirectoryIdentity'})
        validate_context({key:binding[key] for key in CONTEXT_KEYS})
        capture_identity=binding['captureDirectoryIdentity']
        require(type(capture_identity) is dict and set(capture_identity)=={'device','inode'} and integer(capture_identity['device']) and integer(capture_identity['inode'],1))
        require(binding==dict(expected,captureDirectoryIdentity=dict(device=identity.st_dev,inode=identity.st_ino)))
        progress=decode_private(read_private(fd,'progress.json'))
        keys={'schemaVersion','bindingSha256','generation','attempts','committedFrames','firstAt','lastAttemptAt','lastCommitMonotonic','maxGapSeconds','flags','previous','latest'}
        require(type(progress) is dict and set(progress)==keys and type(progress['schemaVersion']) is int and progress['schemaVersion']==1 and progress['bindingSha256']==sha(binding_raw))
        require(integer(progress['generation'],1) and integer(progress['attempts']) and integer(progress['committedFrames']) and progress['committedFrames']<=progress['attempts'] and progress['generation']>=progress['attempts']+1)
        require(type(progress['flags']) is dict and set(progress['flags'])==set(FLAGS) and all(type(v) is bool for v in progress['flags'].values()))
        require(number(progress['maxGapSeconds']) and progress['maxGapSeconds']>=0)
        if progress['lastAttemptAt'] is not None:require(type(progress['lastAttemptAt']) is not bool);seconds(progress['lastAttemptAt'])
        require((progress['attempts']==0)==(progress['lastAttemptAt'] is None))
        count=progress['committedFrames']
        if count==0:
            require(all(progress[k] is None for k in ('firstAt','lastCommitMonotonic','previous','latest')))
        else:
            require(number(progress['lastCommitMonotonic']) and progress['lastCommitMonotonic']>=0 and progress['latest'] is not None)
            require(type(progress['firstAt']) is not bool);seconds(progress['firstAt'])
            require((count==1)==(progress['previous'] is None))
        previous=read_frame(fd,progress['previous']) if progress['previous'] is not None else None
        latest=read_frame(fd,progress['latest'],True) if progress['latest'] is not None else None
        if latest is not None:
            require(seconds(progress['firstAt'])<=seconds(latest['at']))
            if count==1:require(progress['firstAt']==latest['at'])
        if previous is not None:
            require(seconds(progress['firstAt'])<=seconds(previous['at'])<seconds(latest['at']))
            before=progress['previous'];after=progress['latest']
            if before['basename']==after['basename']:require(before['offset']+before['length']==after['offset'])
            else:require(int(after['basename'][8:12])==int(before['basename'][8:12])+1 and after['offset']==0)
        check_directory(fd,directory)
        return progress,previous,latest
    finally:os.close(fd)


class SegmentedCapture:
    def __init__(self,directory: Path,max_segment_seconds=1800,max_total_seconds=5400,*,progress_context=None):
        if not 1<=max_segment_seconds<=1800 or not 1<=max_total_seconds<=5400:raise ValueError('invalid capture bounds')
        self.progress_context=validate_context(progress_context) if progress_context is not None else None
        self.progress_fd=None;self.final_refs={};self.final_samples={}
        self.directory=Path(directory);self.directory.mkdir(mode=0o700,parents=False,exist_ok=False)
        self.segment_seconds=max_segment_seconds;self.total_seconds=max_total_seconds
        self.window=CaptureWindow(self.directory/'segment-0001.jsonl')
        self.segments=[];self.current=None;self.first=None;self.last=None;self.finals={};self.finished=False
        if self.progress_context is not None:
            self.progress_fd=open_directory(self.directory);info=os.fstat(self.progress_fd)
            binding=dict(self.progress_context,captureDirectoryIdentity=dict(device=info.st_dev,inode=info.st_ino))
            self.binding_raw=publish_private(self.progress_fd,'progress-binding.json',binding)
            self.binding_identity=file_identity(os.stat('progress-binding.json',dir_fd=self.progress_fd,follow_symlinks=False))
            self.progress=dict(schemaVersion=1,bindingSha256=sha(self.binding_raw),generation=0,attempts=0,committedFrames=0,
                firstAt=None,lastAttemptAt=None,lastCommitMonotonic=None,maxGapSeconds=0,
                flags={key:False for key in FLAGS},previous=None,latest=None)
            self._publish()
    def __del__(self):
        if getattr(self,'progress_fd',None) is not None:os.close(self.progress_fd);self.progress_fd=None
    def _publish(self):
        check_directory(self.progress_fd,self.directory)
        require(file_identity(os.stat('progress-binding.json',dir_fd=self.progress_fd,follow_symlinks=False))==self.binding_identity)
        require(read_private(self.progress_fd,'progress-binding.json')==self.binding_raw)
        self.progress['generation']+=1
        publish_private(self.progress_fd,'progress.json',self.progress,replace=self.progress['generation']>1)
        check_directory(self.progress_fd,self.directory)
    def note_poll_error(self,at,attempt=True):
        if self.progress_context is None:return
        if attempt:self.progress['attempts']+=1;self.progress['lastAttemptAt']=at
        self.progress['flags']['pollError']=True;self._publish()
    def _observe(self,frame):
        p=self.progress;p['attempts']+=1;p['lastAttemptAt']=frame['at'];flags=p['flags'];context=self.progress_context
        if frame.get('expectedReplicas')!=2:flags['pollError']=True
        if type(frame.get('inventory')) is not dict or frame['inventory'].get('feedSession')!=context['feedSession']:flags['invalidEvidence']=True
        authenticated=[]
        try:
            rows=frame['pods'];require(type(rows) is list and len(rows)==2)
            expected={r['uid']:r for r in context['originalRoster']};seen=set()
            for pod in rows:
                require(type(pod) is dict)
                snap=pod['snapshot'];require(type(snap) is dict)
                sample=snap['sample'];require(type(sample) is dict)
                require(all(type(pod.get(k)) is str and pod[k] for k in ('name','uid','containerId')) and type(sample.get('processId')) is str and sample['processId'])
                identity=dict(name=pod['name'],uid=pod['uid'],containerId=pod['containerId'],processId=sample['processId'])
                if pod['uid'] in seen or identity!=expected.get(pod['uid']):flags['identityChanged']=True;continue
                seen.add(pod['uid']);authenticated.append(pod)
                require(type(pod.get('ready')) is bool and type(snap.get('final')) is bool)
                if frame.get('phase','measurement')!='settling':require(pod['ready'] and not snap['final'])
                if snap['final']:require(type(snap.get('connected')) is int and snap['connected']==0)
                if integer(sample.get('Skipped'),1):flags['skipped']=True
                require(all(integer(sample.get(k)) for k in COUNTERS+('sequence',)) and type(snap.get('events')) is list)
                require(sample['Attempts']==sum(sample[k] for k in ('Success','Failure','Timeout','Ambiguous')) and sample['Scheduled']>=sample['Attempts']+sample['Skipped'])
                if sample['Skipped']>0:flags['skipped']=True
                sanitized,complete=__import__('capture_window').clean_client_pods([pod]);require(complete)
            deployment=frame['deployment'];require(type(deployment) is dict and all(deployment.get(k) is not None for k in ('uid','generation','observedGeneration','desired','ready')))
            require(type(deployment['uid']) is str and all(integer(deployment[k]) for k in ('generation','observedGeneration','desired','ready')))
            if deployment['uid']!=context['deployment']['uid'] or deployment['generation']!=context['deployment']['generation'] or deployment['desired']!=2:flags['identityChanged']=True
            require(deployment['observedGeneration']==deployment['generation'] and frame['expectedReplicas']==2)
            if frame.get('phase','measurement')!='settling':require(deployment['ready']==2)
        except (KeyError,TypeError,ValueError,IndexError,OverflowError):flags['invalidEvidence']=True
        # Authenticate each available original independently, even if a peer is malformed.
        authenticated=[]
        for pod in frame.get('pods',[]) if type(frame.get('pods')) is list else []:
            try:
                identity=dict(name=pod['name'],uid=pod['uid'],containerId=pod['containerId'],processId=pod['snapshot']['sample']['processId'])
                if identity in context['originalRoster']:
                    authenticated.append(pod)
                    if integer(pod['snapshot']['sample'].get('Skipped'),1):flags['skipped']=True
            except (KeyError,TypeError):pass
        source=dict(frame,pods=authenticated)
        if observed_sql_failure(source,seconds(p['firstAt']) if p['firstAt'] is not None else seconds(frame['at'])):flags['sqlFailure']=True
    def _tail_before(self):
        check_directory(self.progress_fd,self.directory)
        require(file_identity(os.stat('progress-binding.json',dir_fd=self.progress_fd,follow_symlinks=False))==self.binding_identity)
        try:
            info=os.stat(self.window.path.name,dir_fd=self.progress_fd,follow_symlinks=False);private_info(info)
            latest=self.progress['latest']
            require(latest is not None and latest['basename']==self.window.path.name and
                (info.st_dev,info.st_ino,info.st_size)==(latest['device'],latest['inode'],latest['offset']+latest['length']))
            return info
        except FileNotFoundError:
            require(self.progress['latest'] is None or self.progress['latest']['basename']!=self.window.path.name)
            return None
    def _committed(self,before):
        fd=self.progress_fd;check_directory(fd,self.directory)
        info=os.stat(self.window.path.name,dir_fd=fd,follow_symlinks=False);private_info(info)
        require(before is None or (before.st_dev,before.st_ino)==(info.st_dev,info.st_ino))
        offset=before.st_size if before is not None else 0;length=info.st_size-offset
        require(1<=length<=FRAME_LIMIT)
        item=os.open(self.window.path.name,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=fd)
        try:
            require(file_identity(os.fstat(item))==file_identity(info));raw=os.pread(item,length,offset)
            require(len(raw)==length and raw.endswith(b'\n') and raw.count(b'\n')==1)
            actual=decode_private(raw);require(type(actual) is dict)
            ref=dict(basename=self.window.path.name,device=info.st_dev,inode=info.st_ino,offset=offset,length=length,sha256=sha(raw),at=actual['at'])
            validate_ref(ref)
            require(file_identity(info)==file_identity(os.fstat(item))==file_identity(os.stat(self.window.path.name,dir_fd=fd,follow_symlinks=False)))
        finally:os.close(item)
        p=self.progress
        if p['firstAt'] is None:p['firstAt']=actual['at']
        if p['latest'] is not None:
            gap=seconds(actual['at'])-seconds(p['latest']['at'])
            if gap<=0:p['flags']['invalidEvidence']=True
            p['maxGapSeconds']=max(p['maxGapSeconds'],gap)
        p['previous']=p['latest'];p['latest']=ref;p['committedFrames']+=1;p['lastCommitMonotonic']=time.monotonic()
        for pod in actual['pods']:
            try:
                key=client_identity(pod);snap=pod['snapshot'];sample=snap['sample']
                original=next((row for row in self.progress_context['originalRoster'] if (row['uid'],row['processId'])==key and row['name']==pod.get('name') and row['containerId']==pod.get('containerId')),None)
                if original is not None and snap.get('final') is True and key not in self.final_refs:
                    require(snap['connected']==0 and type(snap['connected']) is int and sample['Scheduled']==sample['Attempts']+sample['Skipped'] and sample['Skipped']==0)
                    self.final_refs[key]=ref;self.final_samples[key]=dict(sample)
                elif key in self.final_samples and sample!=self.final_samples[key]:p['flags']['invalidEvidence']=True
            except (KeyError,TypeError,ValueError):p['flags']['invalidEvidence']=True
        self._publish()
        return actual
    def _close(self):
        if self.current:
            self.current['sha256']=digest(self.window.path) if self.progress_context is None else None;self.segments.append(self.current);self.current=None
    def append(self,frame: dict):
        if self.progress_context is None:return self._append(frame)
        try:
            self._observe(frame)
            return self._append(frame)
        except Exception:
            self.progress['flags']['invalidEvidence']=True;self._publish();raise
    def _append(self,frame):
        if self.finished:raise ValueError('capture already finished')
        now=seconds(frame['at'])
        if self.first is None:self.first=now
        if now-self.first>self.total_seconds+(5 if frame.get('phase')=='settling' else 0):raise ValueError('total capture bound exceeded')
        if self.last is not None and now<=self.last:raise ValueError('unordered capture poll')
        if self.current and now-seconds(self.current['firstAt'])>=self.segment_seconds:self._close()
        if self.current is None:
            name='segment-%04d.jsonl'%(len(self.segments)+1);self.window.path=self.directory/name
            self.current={'path':name,'basename':name,'firstAt':frame['at'],'lastAt':frame['at'],'frames':0}
        before=self._tail_before() if self.progress_context is not None else None
        frame=self.window.append(frame)
        if self.progress_context is not None:frame=self._committed(before)
        self.current['lastAt']=frame['at'];self.current['frames']+=1;self.last=now
        for p in frame['pods']:
            s=p['snapshot'];key=client_identity(p)
            if frame['inventory'].get('schemaVersion')==2:
                if key is None:continue
            else:key=(p['uid'],s['sample']['processId'])
            self.finals[key]=s.get('final',False)
    def finish(self,*,completed: bool,cancelled: bool,errors: list):
        if self.finished:raise ValueError('capture already finished')
        self._close();self.finished=True
        if self.progress_context is not None:
            for segment in self.segments:segment['sha256']=digest(self.directory/segment['basename'])
        private_json(self.directory/'completion.json',{'version':1,'completed':completed,'cancelled':cancelled,'pollSeconds':1,'maxSegmentSeconds':self.segment_seconds,'maxTotalSeconds':self.total_seconds,'segments':self.segments,'missingPollCount':len(errors),'errors':errors,'clientFinalMarkers':[{'uid':k[0],'processId':k[1],'final':v} for k,v in sorted(self.finals.items())]})
        result=assess_manifest(self.directory);private_json(self.directory/'summary.json',result);return result


def assess_manifest(directory: Path):
    directory=Path(directory);issues=[];finals={};failure_counters={};observed_failure=False
    invalid=(OSError,ValueError,KeyError,TypeError,IndexError,OverflowError)
    def issue(reason):
        if reason not in issues:issues.append(reason)
    try:
        with (directory/'completion.json').open() as stream:
            raw=stream.read(2*1024*1024+1)
        if len(raw)>2*1024*1024:raise ValueError('manifest bound exceeded')
        m=json.loads(raw);segments=m['segments']
        if m['version']!=1 or m['pollSeconds']!=1 or not 1<=m['maxSegmentSeconds']<=1800 or not 1<=m['maxTotalSeconds']<=5400 or not isinstance(segments,list) or not 1<=len(segments)<=5406:raise ValueError('invalid manifest bounds')
        completed=m['completed'] is True;cancelled=m['cancelled'] is not False
        if type(m['missingPollCount']) is not int or m['missingPollCount']!=len(m['errors']) or m['missingPollCount']:issue('unavailable capture poll')
    except invalid:
        return {'status':'INCONCLUSIVE','success':0,'reasons':['missing or invalid completion/segment evidence']}

    def frames():
        nonlocal observed_failure
        previous=None;window_first=None;seen=set()
        for i,s in enumerate(segments,1):
            name='segment-%04d.jsonl'%i
            # Validate each segment independently: a missing interval must not
            # hide an observed SQL failure in a later authenticated segment.
            try:
                if s['basename']!=name or s['path']!=name:raise ValueError('unordered segment')
                seen.add(name);p=directory/name
                if p.is_symlink() or digest(p)!=s['sha256']:raise ValueError('segment hash mismatch')
                first,last=seconds(s['firstAt']),seconds(s['lastAt'])
                if last<first or last-first>=m['maxSegmentSeconds'] or (previous is not None and first<=previous):issue('segment overlap or bound exceeded')
                previous=last
                count=0;actual_first=None;actual_last=None
                with p.open() as stream:
                    while True:
                        line=stream.readline(2*1024*1024+1)
                        if not line:break
                        if len(line)>2*1024*1024:raise ValueError('frame bound exceeded')
                        try:
                            source=json.loads(line)
                            if observed_sql_failure(source,window_first if window_first is not None else first):observed_failure=True
                            f=clean(source);now=seconds(f['at'])
                        except invalid:
                            issue('malformed retained evidence');continue
                        count+=1
                        if actual_first is None:actual_first=f['at']
                        actual_last=f['at']
                        if window_first is None:window_first=now
                        limit=m['maxTotalSeconds']+(5 if f['phase']=='settling' else 0)
                        if now-window_first>limit:issue('total capture bound exceeded')
                        for pod in f['pods']:
                            snap=pod['snapshot'];sample=snap['sample'];key=client_identity(pod)
                            if f['inventory'].get('schemaVersion')==2:
                                if key is None:
                                    issue('malformed retained evidence');continue
                            else:key=(pod['uid'],sample['processId'])
                            finals[key]=snap.get('final',False)
                            # Independent numeric deltas remain observable even if
                            # the full assessor stops at malformed earlier evidence.
                            fields=COUNTERS+('sequence',)
                            valid=all(type(sample[k]) is int and sample[k]>=0 for k in fields)
                            if valid:
                                valid=(sample['Attempts']==sum(sample[k] for k in ('Success','Failure','Timeout','Ambiguous')) and sample['Scheduled']>=sample['Attempts']+sample['Skipped'])
                            if valid:
                                counts=tuple(sample[k] for k in fields)
                                old=failure_counters.get(key)
                                # A reset or corrupt lower observation must never
                                # replace the last trustworthy cumulative baseline.
                                if old is not None and any(v<before for v,before in zip(counts,old)):
                                    issue('counter reset')
                                else:
                                    if old is not None and any(sample[k]>old[fields.index(k)] for k in ('Failure','Timeout','Ambiguous')):observed_failure=True
                                    failure_counters[key]=counts
                            else:issue('malformed retained evidence')
                        yield f
                if not count or count!=s['frames'] or actual_first!=s['firstAt'] or actual_last!=s['lastAt']:issue('segment boundaries mismatch')
            except invalid:
                issue('missing or invalid completion/segment evidence')
                continue
        if set(p.name for p in directory.glob('segment-*.jsonl'))!=seen:issue('unlisted segment')

    stream=frames()
    result=assess(stream,completed=completed,cancelled=cancelled)
    # assess deliberately aborts on semantic corruption. Finish the same bounded
    # iterator so later authenticated observations can still establish FAIL.
    for _ in stream:pass
    if observed_failure:result['status']='FAIL'
    expected=[{'uid':k[0],'processId':k[1],'final':v} for k,v in sorted(finals.items())]
    if not finals or not all(finals.values()) or m.get('clientFinalMarkers')!=expected:issue('missing client final marker')
    for reason in issues:
        if reason not in result['reasons']:result['reasons'].append(reason)
    if issues and result['status']!='FAIL':result['status']='INCONCLUSIVE'
    return result
