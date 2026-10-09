import {useState} from 'react';
import {createRoot, type Root} from 'react-dom/client';
import {WorkLogBlock,WorkLogButton,WorkLogList,WorkLogDetails} from './vendor/WorkLog';
import {projectWorklog} from './model.mjs';

type Event = {requestId?:string;attemptId?:string;sequence:number;kind?:string;summary?:string;occurredAt?:string;evidenceMode?:string;details?:unknown};
type View = {snapshot?:any;events?:Event[];unavailable?:boolean};
const roots=new WeakMap<HTMLElement,{root:Root;view:View}>();
function EventRow({event}:{event:Event}) {
 const [open,setOpen]=useState(false);
 const hasDetail=event.details&&Object.keys(event.details).length>0;
 const time=event.occurredAt&&Number.isFinite(Date.parse(event.occurredAt))?new Date(event.occurredAt).toLocaleString():null;
 return <WorkLogBlock layout="group-content">
  <WorkLogButton wrapLabel icon={<span className="worklog-icon">{event.kind==='tool_result'?'◇':event.kind==='tool_start'?'◉':'·'}</span>}
   label={event.summary||event.kind||'Recorded event'} trailing={<span className="worklog-time">{time?<time dateTime={event.occurredAt}>{time}</time>:'Time not recorded'}{hasDetail?' '+(open?'⌄':'›'):''}</span>}
   data-event-sequence={event.sequence} aria-expanded={!!hasDetail&&open} aria-label={`${event.summary||event.kind||'Recorded event'} details`}
   onClick={()=>hasDetail&&setOpen(!open)} disabled={!hasDetail}/>
  <div className="worklog-metadata ms-7"><span>{event.evidenceMode??'unverified'} · {event.kind??'event'}</span></div>
  {hasDetail&&open?<WorkLogDetails><pre className="worklog-details">{JSON.stringify(event.details,null,2)}</pre></WorkLogDetails>:null}
 </WorkLogBlock>;
}
function Viewer({view}:{view:View}) {
 const model=projectWorklog(view.snapshot,view.events);
 const [open,setOpen]=useState(false);
 return <div className="t3-worklog">
  {(view.unavailable||model.stale)&&<p className="worklog-notice" role="status">Live sync unavailable — showing saved history.</p>}
  {model.recoveryNeeded&&<p className="worklog-notice">Recovery required. Execution ownership remains unresolved.</p>}
  <WorkLogBlock>
   <WorkLogButton label="Handling progress" icon={<span className="worklog-chevron">{open?'⌄':'›'}</span>}
    trailing={<span className="worklog-state">{model.events.length} events{model.durationSeconds!==null?` · recorded span ${model.durationSeconds}s`:''}</span>}
    aria-expanded={open} aria-controls="worklog-events" onClick={()=>setOpen(!open)}/>
   <div className="worklog-metadata ms-7"><span>Execution: {model.status}</span><span>Checkpoint: {model.checkpoint}</span><span>Cleanup: {model.cleanup}</span></div>
  </WorkLogBlock>
  <div id="worklog-events" hidden={!open}><WorkLogList>
   {model.events.map(event=><EventRow key={`${event.attemptId??''}:${event.sequence}`} event={event}/>)}
   {!model.events.length&&<p className="worklog-metadata">No progress has been recorded yet.</p>}
  </WorkLogList></div>
  {model.answer&&<div className="worklog-result"><h3>Result</h3><div className="worklog-answer">{model.answer}</div>
   <p className="worklog-metadata">An available result does not confirm checkpoint or cleanup completion. Evidence is shown in the Result tab.</p></div>}
 </div>;
}
export function updateWorklog(host:HTMLElement|null,patch:View) {
 if(typeof HTMLElement==='undefined'||!(host instanceof HTMLElement))return false;
 let entry=roots.get(host);
 if(!entry){entry={root:createRoot(host),view:{events:[],snapshot:{}}};roots.set(host,entry);}
 entry.view={...entry.view,...patch};
 const id=entry.view.snapshot?.request?.id??'';
 entry.root.render(<Viewer key={id} view={entry.view}/>);
 return true;
}
export function resetWorklog(host:HTMLElement|null) {
 if(!host)return;
 const entry=roots.get(host);if(entry){entry.root.unmount();roots.delete(host);}
}
