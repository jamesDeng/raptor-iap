"""Retained, conservative evidence assessment. No provider mutations or credentials."""
import datetime, json, os, math, re, uuid
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
INVENTORY_FIELDS=('observedAt','desired','members','healthyRegistered')
TRANSITION_FIELDS=('schemaVersion','feedSession','sequence')+INVENTORY_FIELDS+('memberStates','backendStates')
MEMBER_FIELDS=('id','lifecycle','health','addressKnown')
BACKEND_FIELDS=('id','status','weight','listenerHealth')
PENDING=('Pending','Pending:Wait')
LIFECYCLES=PENDING+('InService','Protected','Standby','Stopped','Removing','Removing:Wait')

def clean_inventory(source):
    """Retain only the provider contract, including an explicit unavailable v2 frame."""
    unavailable=dict(schemaVersion=2,feedSession=None,sequence=None,observedAt=0,
                     desired=0,members=[],healthyRegistered=[],memberStates=[],backendStates=[])
    try:
        if 'schemaVersion' not in source:
            return {k:source[k] for k in INVENTORY_FIELDS}
        if type(source['schemaVersion']) is not int or source['schemaVersion']!=2:raise ValueError('schema')
        inv={k:source[k] for k in TRANSITION_FIELDS}
        if 'mode' in source:
            if source['mode'] not in ('full-replacement','disabled-rehearsal'):raise ValueError('mode')
            inv['mode']=source['mode']
        if not isinstance(inv['feedSession'],str) or str(uuid.UUID(inv['feedSession']))!=inv['feedSession']:raise ValueError('session')
        if type(inv['sequence']) is not int or inv['sequence']<1:raise ValueError('sequence')
        seconds(inv['observedAt'])
        if type(inv['desired']) is not int or not 2<=inv['desired']<=4:raise ValueError('desired')
        for key in ('members','healthyRegistered'):
            values=inv[key]
            if not isinstance(values,list) or len(values)>4 or any(not isinstance(n,str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}',n) for n in values):raise ValueError('ids')
            if values!=sorted(set(values)):raise ValueError('ids')
        if not 2<=len(inv['members'])<=4:raise ValueError('member count')
        for key,fields in (('memberStates',MEMBER_FIELDS),('backendStates',BACKEND_FIELDS)):
            if not isinstance(inv[key],list) or len(inv[key])>4:raise ValueError('states')
            inv[key]=[{k:row[k] for k in fields} for row in inv[key]]
            ids=[r['id'] for r in inv[key]]
            if any(not isinstance(n,str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}',n) for n in ids) or ids!=sorted(set(ids)):raise ValueError('state ids')
        if [r['id'] for r in inv['memberStates']]!=inv['members']:raise ValueError('member states')
        for row in inv['memberStates']:
            if row['lifecycle'] not in LIFECYCLES or row['health'] not in ('Healthy','Unhealthy') or type(row['addressKnown']) is not bool:raise ValueError('member state')
        for row in inv['backendStates']:
            if (row['status'] not in ('Adding','Available','Configuring','Removing') or
                type(row['weight']) is not int or not 0<=row['weight']<=100 or
                row['listenerHealth'] not in ('Normal','Initial','Unhealthy','Unavailable')):raise ValueError('backend state')
        return inv
    except (KeyError,TypeError,ValueError,AttributeError,OverflowError):
        # Never downgrade a malformed new snapshot to the historical contract.
        return unavailable


class InventoryMode:
    """Bind a rehearsal to its initial two IDs; legacy streams keep full rules."""
    def __init__(self):
        self.mode=None;self.originals=set();self.added=set();self.phase=0;self.departed=False

    def observe(self,inv):
        mode=inv.get('mode','full-replacement')
        if mode not in ('full-replacement','disabled-rehearsal') or self.mode not in (None,mode):
            raise ValueError('changed inventory mode')
        first=self.mode is None
        self.mode=mode
        if mode=='full-replacement':return
        members=set(inv['members']);desired=inv['desired']
        if first:
            if desired!=2 or len(members)!=2:raise ValueError('rehearsal baseline')
            self.originals=members.copy()
        if type(desired) is not int or desired not in (2,3) or not self.originals.issubset(members):
            raise ValueError('rehearsal scope')
        added=self.added | (members-self.originals)
        if len(added)>1:raise ValueError('rehearsal extra identity')
        if desired==3:
            if self.phase==2:raise ValueError('rehearsal repeated expansion')
            self.phase=1
        elif self.phase==1:
            if not self.added:raise ValueError('rehearsal skipped expansion')
            self.phase=2
        if (added and self.phase==0 or
            self.phase==1 and self.added-members or
            self.departed and added & members):raise ValueError('rehearsal membership order')
        if added-self.added and self.phase!=1:raise ValueError('rehearsal late identity')
        self.added=added
        if self.phase==2 and not (added & members):self.departed=True

    def complete(self,inv):
        return (self.mode!='disabled-rehearsal' or
                self.phase==2 and self.departed and inv['desired']==2 and
                set(inv['members'])==self.originals and
                {r['id'] for r in inv['backendStates']}==self.originals and
                set(inv['healthyRegistered'])==self.originals)


class TransitionEvidence:
    """History belongs to the full stream, never to an individual segment."""
    def __init__(self):
        self.previous=None;self.seen=set();self.address_required=set();self.ever_serving=set();self.obligations={};self.departures=0;self.mode=InventoryMode()

    def assess(self,inv,met,now,issue):
        if inv['sequence'] is None:
            issue('incomplete provider inventory');return
        try:self.mode.observe(inv)
        except ValueError:issue('unexpected rehearsal mode or membership')
        previous=self.previous
        if previous is not None and inv!=previous:
            if (inv['feedSession']!=previous['feedSession'] or inv['sequence']!=previous['sequence']+1 or
                seconds(inv['observedAt'])<=seconds(previous['observedAt'])):
                issue('provider feed discontinuity')
            expected=((3,2),) if self.mode.mode=='disabled-rehearsal' else ((4,3),(3,2))
            if self.departures<len(expected) and (previous['desired'],inv['desired'])==expected[self.departures]:self.departures+=1
        self.previous=inv
        members=set(inv['members']);backends={r['id'] for r in inv['backendStates']}
        self.seen.update(members)
        if len(self.seen)>4 or not backends.issubset(self.seen) or len(self.seen-members)>self.departures:
            issue('unexpected provider membership')
        if not -1<=now-seconds(inv['observedAt'])<=30:issue('stale provider inventory')
        healthy={r['id'] for r in inv['backendStates'] if r['status']=='Available' and r['weight']>0 and r['listenerHealth']=='Normal'} & members
        if set(inv['healthyRegistered'])!=healthy:issue('inconsistent healthy backends')
        if not healthy:issue('no healthy registered backends')
        nonpending=set()
        for row in inv['memberStates']:
            node=row['id']
            if row['lifecycle'] not in PENDING:nonpending.add(node)
            if not row['addressKnown'] and (node in self.address_required or node in backends or node in nonpending):issue('missing required member address')
            if row['addressKnown'] or node in backends or node in nonpending:self.address_required.add(node)
        self.ever_serving.update(nonpending|backends)
        # Only a subsequent fresh publication can close a prior zero observation.
        for node,zero_at in list(self.obligations.items()):
            if node not in backends and zero_at is not None and seconds(inv['observedAt'])>zero_at and -1<=now-seconds(inv['observedAt'])<=30:
                del self.obligations[node]
        for node in backends:self.obligations.setdefault(node,None)
        nodes=met['nodes'];allowed=self.seen
        fresh=(-1<=now-seconds(met['observedAt'])<=3 and -1<=now-seconds(met['sourceAt'])<=45)
        valid=met['available'] is True and isinstance(nodes,list) and len(nodes)==len(set(nodes)) and set(nodes).issubset(allowed)
        if not valid:issue('missing metrics coverage')
        if not fresh:issue('stale metrics')
        triplets={};node_valid={n:True for n in nodes};nonzero=set()
        for row in met['raw']:
            node=row['node'];key=(node,row['pool'],row['user']);kind=row['kind'];value=row['value']
            ok=(node in nodes and node in allowed and bool(row['pool']) and bool(row['user']) and kind in ('active','idle','waiting'))
            if not ok:issue('invalid raw coverage')
            numeric=type(value) in (int,float) and math.isfinite(value) and value>=0 and int(value)==value and value<=2**53
            if not numeric:issue('invalid raw observation')
            row_fresh=-1<=now-seconds(row['sourceAt'])<=45
            if not row_fresh:issue('stale raw observation')
            bucket=triplets.setdefault(key,set())
            if kind in bucket:issue('duplicate raw series');ok=False
            bucket.add(kind)
            node_valid[node]=node_valid.get(node,False) and ok and numeric and row_fresh
            if numeric and value>0:nonzero.add(node)
        for key,kinds in triplets.items():
            if kinds!={'active','idle','waiting'}:node_valid[key[0]]=False;issue('missing expected raw triplets')
        for node in nodes:
            if (node,'test','poc_app') not in triplets:node_valid[node]=False;issue('missing expected raw triplets')
        # A later nonzero sample restores even a previously retired obligation.
        for node in nonzero:
            if node in allowed:self.obligations[node]=None
        required=set(self.obligations)|nonpending|(members & self.ever_serving)
        if not required.issubset(nodes) or any(not node_valid.get(n,False) for n in required):issue('missing metrics coverage')
        for node in self.obligations:
            if node in nonzero:self.obligations[node]=None
            elif valid and fresh and node_valid.get(node,False):
                # Do not move an existing proof forward on repeated zero samples.
                if self.obligations[node] is None:self.obligations[node]=max(now,seconds(met['observedAt']))


def clean_client_pods(rows):
    """Sanitize partial v2 clients independently; never copy an opaque value."""
    complete=[True]
    def pick(value,predicate):
        try:
            if predicate(value):return value
        except (TypeError,ValueError,OverflowError):pass
        complete[0]=False;return None
    def identity(value):return isinstance(value,str) and re.fullmatch(r'[A-Za-z0-9_.:/-]{1,256}',value) is not None
    def integer(value):return type(value) is int
    def timestamp(value):
        try:seconds(value);return True
        except (TypeError,ValueError,OverflowError):return False
    def mapping(value):
        if isinstance(value,dict):return value
        complete[0]=False;return {}
    def sequence(value):
        if isinstance(value,list):return value
        complete[0]=False;return []
    pods=[]
    for item in sequence(rows):
        p=mapping(item);snap=mapping(p.get('snapshot'));sample=mapping(snap.get('sample'))
        cleaned={'processId':pick(sample.get('processId'),identity),'at':pick(sample.get('at'),timestamp)}
        for key in COUNTERS+('sequence',):cleaned[key]=pick(sample.get(key),integer)
        events=[]
        for item in sequence(snap.get('events')):
            event=mapping(item)
            row={'processId':pick(event.get('processId'),identity),'sequence':pick(event.get('sequence'),integer),
                 'at':pick(event.get('at'),timestamp),
                 'kind':pick(event.get('kind'),lambda v:v in ('scheduled','operation','skipped','connected','disconnected','connection_failure'))}
            if 'operationId' in event or row['kind'] in ('scheduled','operation','skipped'):
                row['operationId']=pick(event.get('operationId'),identity)
            if 'outcome' in event or row['kind']=='operation':
                row['outcome']=pick(event.get('outcome'),lambda v:isinstance(v,str) and v.lower() in ('success','failure','timeout','ambiguous'))
            if 'durationSeconds' in event:
                row['durationSeconds']=pick(event['durationSeconds'],lambda v:type(v) in (int,float) and math.isfinite(v) and v>=0)
            events.append(row)
        pods.append({'uid':pick(p.get('uid'),identity),'snapshot':{'sample':cleaned,'events':events,
                     'connected':pick(snap.get('connected'),integer),'truncated':pick(snap.get('truncated'),lambda v:type(v) is bool),
                     'final':pick(snap.get('final',False),lambda v:type(v) is bool)}})
    return pods,complete[0]


def client_identity(pod):
    sample=pod['snapshot']['sample'];uid=pod['uid'];pid=sample['processId']
    return (uid,pid) if isinstance(uid,str) and uid and isinstance(pid,str) and pid else None


def clean(frame):
    # Only schema-defined non-secret observations are persisted.
    inventory=clean_inventory(frame.get('inventory',{'schemaVersion':2}))
    complete=True
    if inventory.get('schemaVersion')==2:
        pods,complete=clean_client_pods(frame.get('pods'))
    else:
        pods=[]
        for p in frame['pods']:
            s=p['snapshot'];a=s['sample']
            pods.append({'uid':p['uid'],'snapshot':{'sample':{k:a[k] for k in ('processId','at','sequence')+COUNTERS},'events':[{k:e[k] for k in EVENT_FIELDS if k in e} for e in s['events']], 'connected':s['connected'],'truncated':s['truncated'],'final':s.get('final',False)}})
    try:
        metrics=dict({k:frame['metrics'][k] for k in ('observedAt','sourceAt','nodes','available')},raw=[{k:r[k] for k in ('node','pool','user','kind','value','sourceAt')} for r in frame['metrics']['raw']])
    except (KeyError,TypeError):
        if inventory.get('schemaVersion')!=2:raise
        metrics=dict(observedAt=0,sourceAt=0,nodes=[],available=False,raw=[])
    return {'at':frame['at'],'phase':frame.get('phase','measurement'),'expectedReplicas':frame.get('expectedReplicas',len(pods)) if complete else 0,'pods':pods,'inventory':inventory,'metrics':metrics}
class CaptureWindow:
    def __init__(self,path):
        self._sequences={};self._started=False
        self.path=Path(path);self.path.parent.mkdir(parents=True,exist_ok=True)
    def append(self,frame):
        frame=clean(frame)
        version=frame['inventory'].get('schemaVersion',1)
        sequences={}
        for p in frame['pods']:
            snap=p['snapshot'];sample=snap['sample'];key=client_identity(p)
            if version==2 and (key is None or type(sample['sequence']) is not int or sample['sequence']<0):continue
            if version==1:key=(p['uid'],sample['processId'])
            last=self._sequences.get(key,sample['sequence'] if not self._started else 0)
            if not self._started and version==2:
                # Preserve invalid event markers and explicit in-window outcomes.
                def retain_event(event):
                    try:return seconds(event['at'])>=seconds(frame['at'])
                    except (TypeError,ValueError,OverflowError):return True
                snap['events']=[e for e in snap['events'] if retain_event(e)]
            elif version==2:
                snap['events']=[e for e in snap['events'] if type(e['sequence']) is not int or e['sequence']>last]
            else:snap['events']=[e for e in snap['events'] if e['sequence']>last]
            sequences[key]=sample['sequence']
        raw=json.dumps(frame,separators=(',',':'),allow_nan=False)+'\n'
        if len(raw)>2*1024*1024:raise ValueError('frame bound exceeded')
        fd=os.open(str(self.path),os.O_WRONLY|os.O_CREAT|os.O_APPEND,0o600)
        with os.fdopen(fd,'a') as f:f.write(raw);f.flush();os.fsync(f.fileno())
        self._started=True
        self._sequences.update(sequences)
        return frame
    def read(self):
        with self.path.open() as f:
            for line in f:
                if len(line)>2*1024*1024:raise ValueError('frame bound exceeded')
                yield json.loads(line)
def observed_sql_failure(frame,start):
    """Recognize explicit v2 SQL evidence independently of coverage validation."""
    try:
        if frame['inventory'].get('schemaVersion')!=2:return False
        pods=frame['pods']
        if not isinstance(pods,list):return False
    except (KeyError,TypeError,AttributeError):return False
    for pod in pods:
        try:
            snap=pod['snapshot'];sample=snap['sample'];pid=sample['processId']
            if not isinstance(pod['uid'],str) or not pod['uid'] or not isinstance(pid,str) or not pid:continue
            if any(type(sample.get(k)) is int and sample[k]>0 for k in ('Failure','Timeout','Ambiguous')):return True
            now=seconds(frame['at'])
            if start is None or not isinstance(snap['events'],list):continue
        except (KeyError,TypeError,ValueError,OverflowError):continue
        for event in snap['events']:
            try:
                if (event['processId']==pid and event['kind']=='operation' and
                    type(event['sequence']) is int and event['sequence']>0 and
                    isinstance(event.get('operationId'),str) and event['operationId'] and
                    start<=seconds(event['at'])<=now+1 and
                    isinstance(event.get('outcome'),str) and event['outcome'].lower() in ('failure','timeout','ambiguous')):
                    return True
            except (KeyError,TypeError,ValueError,OverflowError):continue
    return False


def assess(frames,cancelled=False,completed=False,max_gap=3):
    reasons=[];failure=False;success=0;processes={};seen_pods={};last_poll=None;first_poll=None;process_pods={};settling_start=None
    transitions=TransitionEvidence();schema=None
    def issue(reason):
        if reason not in reasons:reasons.append(reason)
    def observations():
        nonlocal failure
        start=None
        for frame in frames:
            if start is None:
                try:start=seconds(frame['at'])
                except (KeyError,TypeError,ValueError,OverflowError):pass
            failure=observed_sql_failure(frame,start) or failure
            yield frame
    stream=observations()
    try:
        for frame in stream:
            frame=clean(frame);now=seconds(frame['at'])
            if first_poll is None:first_poll=now
            poll_limit=min(max_gap,3) if frame['inventory'].get('schemaVersion')==2 else max_gap
            if last_poll is not None and (now<=last_poll or now-last_poll>poll_limit):issue('missed or unordered poll')
            previous=last_poll;last_poll=now
            phase=frame['phase']
            if phase=='settling':
                if settling_start is None:settling_start=now
                if now-settling_start>5:issue('settling period exceeded')
            elif phase!='measurement' or settling_start is not None:issue('invalid measurement phase')
            inv,met=frame['inventory'],frame['metrics'];members=inv['members']
            version=inv.get('schemaVersion',1)
            if schema is None:schema=version
            elif version!=schema:issue('changed inventory schema')
            if version==2:
                # Provider uncertainty must not suppress observed client failures.
                try:transitions.assess(inv,met,now,issue)
                except (KeyError,TypeError,ValueError,OverflowError):issue('malformed provider or metrics evidence')
            else:
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
                if version==2:
                    failure=failure or any(s[k]>0 for k in ('Failure','Timeout','Ambiguous'))
                    if s['Skipped']>0:issue('scheduled work skipped')
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
                    if new_process and previous is not None and seconds(e['at'])<first_poll:issue('uncovered new process history')
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
        if schema==2 and not transitions.mode.complete(transitions.previous):issue('incomplete rehearsal transition')
        for s in processes.values():
            if s['pending'] or s['baselinePending'] or s['last']['Scheduled']!=s['last']['Attempts']+s['last']['Skipped']:issue('operations in flight at boundary')
        if first_poll is None or first_poll==last_poll or success==0:issue('no covered successful traffic')
    except (KeyError,TypeError,ValueError,OverflowError):issue('malformed retained evidence')
    # Detailed assessment may stop on corruption; consume the remaining bounded
    # stream so safely observable v2 failures, including a process's first sample,
    # cannot be hidden. Historical v1 failure-baseline semantics stay unchanged.
    try:
        for _ in stream:pass
    except (KeyError,TypeError,ValueError,OverflowError):issue('malformed retained evidence')
    if cancelled or not completed:issue('capture cancelled or missing completion marker')
    return {'status':'FAIL' if failure else 'INCONCLUSIVE' if reasons else 'PASS','success':success,'reasons':reasons}
def retain(path,completed=False,cancelled=False):
    window=CaptureWindow(path);result=assess(window.read(),completed=completed,cancelled=cancelled)
    dest=Path(str(path)+'.summary.json');dest.write_text(json.dumps(result,indent=2)+'\n');os.chmod(dest,0o600)
    return result
