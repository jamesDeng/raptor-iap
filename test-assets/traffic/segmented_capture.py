"""Private immutable segments assessed as one continuous evidence stream."""
import hashlib
import json
import os
from pathlib import Path
from capture_window import CaptureWindow, assess, seconds


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


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
    directory=Path(directory);issues=[];frames=[]
    try:
        m=json.loads((directory/'completion.json').read_text());segments=m['segments'];previous=None;seen=set()
        if m['version']!=1 or m['pollSeconds']!=1 or not 1<=m['maxSegmentSeconds']<=1800 or not 1<=m['maxTotalSeconds']<=5400 or not segments:raise ValueError('invalid manifest bounds')
        for i,s in enumerate(segments,1):
            name='segment-%04d.jsonl'%i
            if s['basename']!=name or s['path']!=name or name in seen:raise ValueError('missing or unordered segment')
            seen.add(name);p=directory/name
            if p.is_symlink() or digest(p)!=s['sha256']:raise ValueError('segment hash mismatch')
            rows=list(CaptureWindow(p).read())
            if not rows or len(rows)!=s['frames'] or rows[0]['at']!=s['firstAt'] or rows[-1]['at']!=s['lastAt']:raise ValueError('segment boundaries mismatch')
            first,last=seconds(s['firstAt']),seconds(s['lastAt'])
            if last<first or last-first>=m['maxSegmentSeconds'] or (previous is not None and first<=previous):raise ValueError('segment overlap or bound exceeded')
            previous=last;frames.extend(rows)
        if set(p.name for p in directory.glob('segment-*.jsonl'))!=seen:raise ValueError('unlisted segment')
        if seconds(frames[-1]['at'])-seconds(frames[0]['at'])>m['maxTotalSeconds']+5:raise ValueError('total bound exceeded')
        finals={}
        for f in frames:
            if f.get('phase','measurement')=='measurement' and seconds(f['at'])-seconds(frames[0]['at'])>m['maxTotalSeconds']:raise ValueError('measurement bound exceeded')
            for p in f['pods']:finals[(p['uid'],p['snapshot']['sample']['processId'])]=p['snapshot'].get('final',False)
        expected=[{'uid':k[0],'processId':k[1],'final':v} for k,v in sorted(finals.items())]
        if not finals or not all(finals.values()) or m['clientFinalMarkers']!=expected:issues.append('missing client final marker')
        if type(m['missingPollCount']) is not int or m['missingPollCount']!=len(m['errors']) or m['missingPollCount']:issues.append('unavailable capture poll')
        completed=m['completed'] is True;cancelled=m['cancelled'] is not False
    except (OSError,ValueError,KeyError,TypeError,IndexError):
        issues.append('missing or invalid completion/segment evidence');completed=False;cancelled=False
    result=assess(frames,completed=completed,cancelled=cancelled)
    for issue in issues:
        if issue not in result['reasons']:result['reasons'].append(issue)
    if issues and result['status']!='FAIL':result['status']='INCONCLUSIVE'
    return result
