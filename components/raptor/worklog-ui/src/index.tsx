import {createContext,useContext} from 'react';
import {createRoot,type Root} from 'react-dom/client';
import {AssistantRuntimeProvider,useExternalStoreRuntime,ThreadPrimitive,MessagePrimitive,groupPartByType} from '@assistant-ui/react';
import {projectTranscript} from './model.mjs';
type View={snapshot?:any;events?:any[];unavailable?:boolean};
const roots=new WeakMap<HTMLElement,{root:Root;view:View}>();
const ModelContext=createContext<any>(null);
const groupBy=groupPartByType({'tool-call':['group-tools']});
const convertMessage=(message:any)=>message;
const rejectSend=async()=>{throw new Error('ReadOnlyProgress')};
function Tool({part}:{part:any}){
 const model=useContext(ModelContext),record=model.records[part.toolCallId];
 const status=record?.status??'unknown',pending=part.result===undefined;
 const label=status==='succeeded'?'Succeeded':status==='failed'?'Failed':status==='cancelled'?'Cancelled':pending?(record?.active?'Running':'Result not recorded'):status==='pending'?'Pending':'Outcome not confirmed';
 const icon=status==='succeeded'?'✓':status==='failed'?'!':status==='cancelled'?'×':pending&&record?.active?'◌':'◇';
 return <details className="progress-tool"><summary><span aria-hidden="true">{icon}</span><span>{part.toolName}</span><span className="progress-tag">{label}</span></summary><div className="progress-tool-detail">{record?.events.map((event:any)=><div key={event.sequence}><p>{event.summary}</p><div className="progress-metadata">{event.evidenceMode??'unverified'} · {event.kind}{event.occurredAt&&<> · <time dateTime={event.occurredAt}>{event.occurredAt}</time></>}</div>{event.details&&<pre>{JSON.stringify(event.details,null,2)}</pre>}</div>)}</div></details>;
}
function AssistantMessage(){const model=useContext(ModelContext);return <MessagePrimitive.Root className="progress-message"><MessagePrimitive.GroupedParts groupBy={groupBy} indicator="never">{({part,children}:any)=>{
 if(part.type==='group-tools')return <details className="progress-activity"><summary><span aria-hidden="true">⚒</span>Tool activity · {part.indices.length}<span className="progress-tag">{part.status.type==='running'&&model.running?'Running':'View details'}</span></summary><div className="progress-activity-content">{children}</div></details>;
 if(part.type==='text')return <p className="progress-narration">{part.text}</p>;
 if(part.type==='data'&&part.name==='progress-event'){const event=part.data;return <div className="progress-event-record"><div className="progress-metadata">{event.evidenceMode??'unverified'} · {event.kind??'event'}{event.occurredAt&&<> · <time dateTime={event.occurredAt}>{event.occurredAt}</time></>}</div>{event.details&&Object.keys(event.details).length>0&&<details className="progress-record-details"><summary>Recorded event details</summary><pre>{JSON.stringify(event.details,null,2)}</pre></details>}</div>;}

 if(part.type==='tool-call')return <Tool part={part}/>;
 return null;
 }}</MessagePrimitive.GroupedParts></MessagePrimitive.Root>}
function Viewer({view}:{view:View}){
 const model=projectTranscript(view.snapshot,view.events);
 const runtime=useExternalStoreRuntime({messages:model.messages,convertMessage,isRunning:model.running,onNew:rejectSend,isDisabled:true});
 return <div className="assistant-progress">
 {(view.unavailable||model.stale)&&<p className="progress-notice" role="status">Live sync unavailable — showing saved history.</p>}
 {model.recoveryNeeded&&<p className="progress-notice">Recovery required. Execution ownership remains unresolved.</p>}
 <div className="progress-status"><span>Execution: {model.status}</span><span>{model.durationSeconds!==null?`Recorded activity span: ${model.durationSeconds}s`:''}</span></div>
 <div className="progress-metadata progress-lifecycle">Checkpoint: {model.checkpoint} · Cleanup: {model.cleanup}</div>
 <ModelContext.Provider value={model}><AssistantRuntimeProvider runtime={runtime}><ThreadPrimitive.Root><ThreadPrimitive.Viewport className="progress-timeline"><ThreadPrimitive.Messages components={{AssistantMessage}}/></ThreadPrimitive.Viewport></ThreadPrimitive.Root></AssistantRuntimeProvider></ModelContext.Provider>
 {!model.events.length&&<p className="progress-metadata">No progress has been recorded yet.</p>}
 {model.answer&&<div className="progress-result"><h3>Result</h3><p>{model.answer}</p><div className="progress-metadata">An available result does not confirm checkpoint or cleanup completion. Evidence is shown in the Result tab.</div></div>}
 </div>;
}
// Keep the mount API stable for existing Gateway and saved-history controllers.
export function updateWorklog(host:HTMLElement|null,patch:View){
 if(typeof HTMLElement==='undefined'||!(host instanceof HTMLElement))return false;
 let entry=roots.get(host);if(!entry){entry={root:createRoot(host),view:{events:[],snapshot:{}}};roots.set(host,entry);}
 entry.view={...entry.view,...patch};entry.root.render(<Viewer key={entry.view.snapshot?.request?.id??''} view={entry.view}/>);return true;
}
export function resetWorklog(host:HTMLElement|null){if(!host)return;const entry=roots.get(host);if(entry){entry.root.unmount();roots.delete(host);}}
