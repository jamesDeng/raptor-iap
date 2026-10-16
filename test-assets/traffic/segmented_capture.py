"""Private immutable segments assessed as one continuous evidence stream."""
import hashlib
import json
import os
from pathlib import Path
from capture_window import CaptureWindow, assess, seconds, clean


def digest(path):
    hasher=hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda:stream.read(128*1024),b''):hasher.update(chunk)
    return hasher.hexdigest()


def private_json(path, value):
    fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as stream:
        json.dump(value,stream,allow_nan=False);stream.write('\n');stream.flush();os.fsync(stream.fileno())


class SegmentedCapture:
    def __init__(self,directory: Path,max_segment_seconds=1800,max_total_seconds=5400):
        if not 1<=max_segment_seconds<=1800 or not 1<=max_total_seconds<=5400:raise ValueError('invalid capture bounds')
        self.directory=Path(directory);self.directory.mkdir(mode=0o700,parents=False,exist_ok=False)
        self.segment_seconds=max_segment_seconds;self.total_seconds=max_total_seconds
        self.window=CaptureWindow(self.directory/'segment-0001.jsonl')
        self.segments=[];self.current=None;self.first=None;self.last=None;self.finals={};self.finished=False
    def _close(self):
        if self.current:
            self.current['sha256']=digest(self.window.path);self.segments.append(self.current);self.current=None
    def append(self,frame: dict):
        if self.finished:raise ValueError('capture already finished')
        now=seconds(frame['at'])
        if self.first is None:self.first=now
        if now-self.first>self.total_seconds+(5 if frame.get('phase')=='settling' else 0):raise ValueError('total capture bound exceeded')
        if self.last is not None and now<=self.last:raise ValueError('unordered capture poll')
        if self.current and now-seconds(self.current['firstAt'])>=self.segment_seconds:self._close()
        if self.current is None:
            name='segment-%04d.jsonl'%(len(self.segments)+1);self.window.path=self.directory/name
            self.current={'path':name,'basename':name,'firstAt':frame['at'],'lastAt':frame['at'],'frames':0}
        self.window.append(frame)
        self.current['lastAt']=frame['at'];self.current['frames']+=1;self.last=now
        for p in frame['pods']:
            s=p['snapshot'];self.finals[(p['uid'],s['sample']['processId'])]=s.get('final',False)
    def finish(self,*,completed: bool,cancelled: bool,errors: list):
        if self.finished:raise ValueError('capture already finished')
        self._close();self.finished=True
        private_json(self.directory/'completion.json',{'version':1,'completed':completed,'cancelled':cancelled,'pollSeconds':1,'maxSegmentSeconds':self.segment_seconds,'maxTotalSeconds':self.total_seconds,'segments':self.segments,'missingPollCount':len(errors),'errors':errors,'clientFinalMarkers':[{'uid':k[0],'processId':k[1],'final':v} for k,v in sorted(self.finals.items())]})
        result=assess_manifest(self.directory);private_json(self.directory/'summary.json',result);return result


def assess_manifest(directory: Path):
    directory=Path(directory);issues=[];finals={}
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
                            f=clean(json.loads(line));now=seconds(f['at'])
                        except invalid:
                            issue('malformed retained evidence');continue
                        count+=1
                        if actual_first is None:actual_first=f['at']
                        actual_last=f['at']
                        if window_first is None:window_first=now
                        limit=m['maxTotalSeconds']+(5 if f['phase']=='settling' else 0)
                        if now-window_first>limit:issue('total capture bound exceeded')
                        for pod in f['pods']:
                            snap=pod['snapshot'];finals[(pod['uid'],snap['sample']['processId'])]=snap.get('final',False)
                        yield f
                if not count or count!=s['frames'] or actual_first!=s['firstAt'] or actual_last!=s['lastAt']:issue('segment boundaries mismatch')
            except invalid:
                issue('missing or invalid completion/segment evidence')
                continue
        if set(p.name for p in directory.glob('segment-*.jsonl'))!=seen:issue('unlisted segment')

    result=assess(frames(),completed=completed,cancelled=cancelled)
    expected=[{'uid':k[0],'processId':k[1],'final':v} for k,v in sorted(finals.items())]
    if not finals or not all(finals.values()) or m.get('clientFinalMarkers')!=expected:issue('missing client final marker')
    for reason in issues:
        if reason not in result['reasons']:result['reasons'].append(reason)
    if issues and result['status']!='FAIL':result['status']='INCONCLUSIVE'
    return result
