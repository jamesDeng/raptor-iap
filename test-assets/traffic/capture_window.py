"""Retained, conservative evidence assessment. No provider mutations or credentials."""
import datetime, json, os, math, re
from pathlib import Path

COUNTERS=('Scheduled','Attempts','Success','Failure','Timeout','Ambiguous','Skipped')
EVENT_FIELDS=('processId','sequence','at','kind','outcome','operationId','durationSeconds')
def seconds(value):
    if isinstance(value,(int,float)) and not isinstance(value,bool):
        if not math.isfinite(value):raise ValueError("nonfinite timestamp")
        return float(value)
    v=re.sub(r'(\.\d{6})\d+',r'\1',value).replace('Z','+0000')
    v=re.sub(r'([+-]\d{2}):(\d{2})$',r'\1\2',v)
    d=None
    for pattern in ('%Y-%m-%dT%H:%M:%S.%f%z','%Y-%m-%dT%H:%M:%S%z'):
        try:d=datetime.datetime.strptime(v,pattern);break
        except ValueError:pass
    if d is None:raise ValueError('invalid timestamp')
    if d.tzinfo is None:raise ValueError('UTC offset required')
    return d.timestamp()
def clean(frame):
    # Only schema-defined non-secret observations are persisted.
    pods=[]
    for p in frame['pods']:
        s=p['snapshot'];a=s['sample']
        pods.append({'uid':p['uid'],'snapshot':{'sample':{k:a[k] for k in ('processId','at','sequence')+COUNTERS},'events':[{k:e[k] for k in EVENT_FIELDS if k in e} for e in s['events']], 'connected':s['connected'],'truncated':s['truncated'],'final':s.get('final',False)}})
    return {'at':frame['at'],'phase':frame.get('phase','measurement'),'expectedReplicas':frame.get('expectedReplicas',len(pods)),'pods':pods,'inventory':{k:frame['inventory'][k] for k in ('observedAt','desired','members','healthyRegistered')},'metrics':dict({k:frame['metrics'][k] for k in ('observedAt','sourceAt','nodes','available')},raw=[{k:r[k] for k in ('node','pool','user','kind','value','sourceAt')} for r in frame['metrics']['raw']])}
class CaptureWindow:
    def __init__(self,path):
        self._sequences={};self._started=False
        self.path=Path(path);self.path.parent.mkdir(parents=True,exist_ok=True)
    def append(self,frame):
        frame=clean(frame)
        for p in frame['pods']:
            s=p['snapshot'];key=(p['uid'],s['sample']['processId']);last=self._sequences.get(key,s['sample']['sequence'] if not self._started else 0)
            s['events']=[e for e in s['events'] if e['sequence']>last]
        raw=json.dumps(frame,separators=(',',':'),allow_nan=False)+'\n'
        if len(raw)>2*1024*1024:raise ValueError('frame bound exceeded')
        fd=os.open(str(self.path),os.O_WRONLY|os.O_CREAT|os.O_APPEND,0o600)
        with os.fdopen(fd,'a') as f:f.write(raw);f.flush();os.fsync(f.fileno())
        self._started=True
        for p in frame['pods']:self._sequences[(p['uid'],p['snapshot']['sample']['processId'])]=p['snapshot']['sample']['sequence']
    def read(self):
        with self.path.open() as f:
            for line in f:
                if len(line)>2*1024*1024:raise ValueError('frame bound exceeded')
                yield json.loads(line)
def assess(frames,cancelled=False,completed=False,max_gap=3):
    reasons=[];failure=False;success=0;processes={};seen_pods={};last_poll=None;first_poll=None;process_pods={};settling_start=None
    def issue(reason):
        if reason not in reasons:reasons.append(reason)
    try:
        for frame in frames:
            frame=clean(frame);now=seconds(frame['at'])
            if first_poll is None:first_poll=now
            if last_poll is not None and (now<=last_poll or now-last_poll>max_gap):issue('missed or unordered poll')
            previous=last_poll;last_poll=now
            phase=frame['phase']
            if phase=='settling':
                if settling_start is None:settling_start=now
                if now-settling_start>5:issue('settling period exceeded')
            elif phase!='measurement' or settling_start is not None:issue('invalid measurement phase')
            inv,met=frame['inventory'],frame['metrics'];members=inv['members']
            if not members or len(set(members))!=len(members) or not set(inv['healthyRegistered']).issubset(members):issue('incomplete provider inventory')
            if type(inv['desired'])!=int or inv['desired']<1 or not -1<=now-seconds(inv['observedAt'])<=30:issue('stale provider inventory')
            if not met['available'] or len(set(met['nodes']))!=len(met['nodes']) or set(met['nodes'])!=set(members):issue('missing metrics coverage')
            if not -1<=now-seconds(met['observedAt'])<=3 or not -1<=now-seconds(met['sourceAt'])<=45:issue('stale metrics')
            triplets={}
            for row in met['raw']:
                key=(row['node'],row['pool'],row['user']);k=row['kind'];v=row['value']
                if row['node'] not in members or not row['pool'] or not row['user'] or k not in ('active','idle','waiting'):issue('invalid raw coverage')
                if type(v) not in (int,float) or not math.isfinite(v) or v<0 or int(v)!=v or v>2**53:issue('invalid raw observation')
                if not -1<=now-seconds(row['sourceAt'])<=45:issue('stale raw observation')
                bucket=triplets.setdefault(key,set())
                if k in bucket:issue('duplicate raw series')
                bucket.add(k)
            if any(v!={'active','idle','waiting'} for v in triplets.values()) or any((n,'test','poc_app') not in triplets for n in members):issue('missing expected raw triplets')
            roster=set();active=0
            for pod in frame['pods']:
                uid=pod['uid'];snap=pod['snapshot'];s=snap['sample'];pid=s['processId'];seq=s['sequence'];key=(uid,pid)
                if not uid or not pid or uid in roster:issue('invalid or duplicate pod identity')
                roster.add(uid)
                if not snap['final']:active+=1
                if pid in process_pods and process_pods[pid]!=uid:issue('duplicate process across pods')
                process_pods[pid]=uid
                if any(type(s[k])!=int or s[k]<0 for k in COUNTERS+('sequence',)):raise ValueError('invalid counters')
                if type(snap['connected'])!=int or snap['connected']<0 or not -1<=now-seconds(s['at'])<=15:issue('invalid or stale client snapshot')
                if s['Attempts']!=sum(s[k] for k in ('Success','Failure','Timeout','Ambiguous')) or s['Scheduled']<s['Attempts']+s['Skipped']:issue('inconsistent counters')
                if uid in seen_pods and seen_pods[uid]!=pid:
                    old=processes[(uid,seen_pods[uid])]
                    if not old['final']:issue('old process lost without final counters')
                new_process=key not in processes
                if new_process:
                    if previous is None:base=dict(s);start_seq=seq
                    else:
                        # A new process must expose its entire event history, starting at 1.
                        base={k:0 for k in COUNTERS};start_seq=0
                    processes[key]={'last':base,'seq':start_seq,'final':False,'pending':{},'baselinePending':base['Scheduled']-base['Attempts']-base['Skipped'],'lastAt':now,'progressAt':now}
                state=processes[key];old=state['last'];oldseq=state['seq']
                if state['final'] and any(s[k]!=old[k] for k in COUNTERS+('sequence',)):issue('final process changed')
                if seq<oldseq or any(s[k]<old[k] for k in COUNTERS):issue('counter reset')
                delta={k:s[k]-old[k] for k in COUNTERS}
                failure=failure or any(delta[k]>0 for k in ('Failure','Timeout','Ambiguous'))
                if delta['Skipped']>0:issue('scheduled work skipped')
                success+=max(0,delta['Success'])
                if delta['Scheduled']>0:state['progressAt']=now
                if phase=='measurement' and not snap['final'] and now-state['progressAt']>3:issue('stalled scheduling')
                events=[e for e in snap['events'] if oldseq<e['sequence']<=seq]
                if seq-oldseq>8192 or [e['sequence'] for e in events]!=list(range(oldseq+1,seq+1)):issue('event ring lost or duplicate events')
                ec={k:0 for k in COUNTERS}
                for e in events:
                    if e['processId']!=pid or seconds(e['at'])>now+1:issue('invalid event identity/time')
                    if new_process and previous is not None and seconds(e['at'])<previous:issue('uncovered new process history')
                    op=e.get('operationId');kind=e['kind']
                    if kind=='scheduled':
                        ec['Scheduled']+=1
                        if not op or op in state['pending']:issue('duplicate or absent scheduled operation')
                        state['pending'][op]=True
                    elif kind in ('operation','skipped'):
                        if op in state['pending']:del state['pending'][op]
                        elif state['baselinePending']>0:state['baselinePending']-=1
                        else:issue('terminal operation without schedule')
                        if kind=='skipped':ec['Skipped']+=1
                        else:
                            ec['Attempts']+=1;out=e.get('outcome','').capitalize()
                            if out in ('Success','Failure','Timeout','Ambiguous'):ec[out]+=1
                            else:issue('unknown terminal outcome')
                    elif kind not in ('connected','disconnected','connection_failure'):issue('unknown event kind')
                if any(ec[k]!=delta[k] for k in COUNTERS):issue('event/counter mismatch')
                if snap['final'] and (s['Scheduled']!=s['Attempts']+s['Skipped'] or snap['connected']!=0):issue('final process not settled')
                state.update(last=dict(s),seq=seq,final=snap['final'],lastAt=now);seen_pods[uid]=pid
            if phase=='measurement' and active<frame['expectedReplicas']:issue('incomplete active traffic roster')
            if not roster or type(frame['expectedReplicas'])!=int or frame['expectedReplicas']<1 or len(roster)<frame['expectedReplicas']:issue('incomplete client roster')
            for (uid,pid),s in processes.items():
                if (uid not in roster or seen_pods.get(uid)!=pid) and not s['final']:issue('old process lost without final counters')
        for s in processes.values():
            if s['pending'] or s['baselinePending'] or s['last']['Scheduled']!=s['last']['Attempts']+s['last']['Skipped']:issue('operations in flight at boundary')
        if first_poll is None or first_poll==last_poll or success==0:issue('no covered successful traffic')
    except (KeyError,TypeError,ValueError,OverflowError):issue('malformed retained evidence')
    if cancelled or not completed:issue('capture cancelled or missing completion marker')
    return {'status':'FAIL' if failure else 'INCONCLUSIVE' if reasons else 'PASS','success':success,'reasons':reasons}
def retain(path,completed=False,cancelled=False):
    window=CaptureWindow(path);result=assess(window.read(),completed=completed,cancelled=cancelled)
    dest=Path(str(path)+'.summary.json');dest.write_text(json.dumps(result,indent=2)+'\n');os.chmod(dest,0o600)
    return result
