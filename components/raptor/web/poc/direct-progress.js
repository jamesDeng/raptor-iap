// A ticket is used only in the first WebSocket frame, never in a URL or persistent storage.
export function createDirectProgressController({read,ticket,Socket=WebSocket,schedule=setTimeout,cancel=clearTimeout,onChange=()=>{}}){
 const state={requestId:null,events:[],cursor:0,unavailable:false,mode:'connecting',execution:null};
 let epoch=0,socket,timer,retries=0,completed=false,streamCursor=0,namespace='gateway';
 const active=(id,v)=>state.requestId===id&&epoch===v;
 const publish=()=>onChange(state);
 function append(events){const known=new Set(state.events.map(e=>e.sequence));for(const event of events){if(event.requestId&&event.requestId!==state.requestId)continue;if(!Number.isSafeInteger(event.sequence)||event.sequence<=0||known.has(event.sequence))continue;known.add(event.sequence);state.events.push(event);state.cursor=Math.max(state.cursor,event.sequence)}state.events.sort((a,b)=>a.sequence-b.sequence)}
 async function fallback(id,v){if(!active(id,v))return;try{const result=await read(id,state.cursor);if(!active(id,v))return;append(result.events??[]);state.unavailable=!!result.syncUnavailable}catch{if(!active(id,v))return;state.unavailable=true}state.mode='polling';publish();timer=schedule(()=>fallback(id,v),2000)}
 async function connect(id,v){
  if(!active(id,v)||completed)return;
  state.mode='connecting';publish();
  try{
   let grant=await ticket(id);if(!active(id,v))return;
   const u=new URL(grant.url);if(u.username||u.password||u.search||u.hash||!(u.protocol==='wss:'||(u.protocol==='ws:'&&['localhost','127.0.0.1','[::1]'].includes(u.hostname))))throw new Error('InvalidProgressEndpoint');
   const current=new Socket(grant.url);socket=current;let authenticated=false;
   const watchdog=schedule(()=>{if(active(id,v)&&!authenticated)current.close()},10000);
   current.onopen=()=>{if(!active(id,v)){current.close();return}current.send(JSON.stringify({type:'subscribe',requestId:id,token:grant.token,afterSequence:streamCursor}));grant=null};
   current.onmessage=message=>{if(!active(id,v)||current!==socket)return;try{const frame=JSON.parse(message.data);if(!['event','snapshot','complete'].includes(frame.type))return;if(namespace==='raptor'){state.events=[];state.cursor=0;namespace='gateway'}authenticated=true;cancel(watchdog);retries=0;state.unavailable=false;state.mode='live';if(frame.type==='event'){append([frame.event]);streamCursor=state.cursor;}if(frame.type==='snapshot')state.execution=frame.execution;if(frame.type==='complete'){completed=true;state.mode='complete';current.close()}publish()}catch{current.close()}};
   current.onclose=()=>{grant=null;cancel(watchdog);if(!active(id,v)||current!==socket||completed)return;state.unavailable=true;state.mode='reconnecting';publish();timer=schedule(()=>connect(id,v),Math.min(1000*2**retries++,15000))};
   current.onerror=()=>current.close();
  }catch(error){if(!active(id,v))return;if(error.message==='ProgressNotConfigured'){state.events=[];state.cursor=0;streamCursor=0;namespace='raptor';return fallback(id,v)}if(!state.events.length){try{const saved=await read(id,0);if(!active(id,v))return;namespace='raptor';append(saved.events??[])}catch{}}state.unavailable=true;state.mode='reconnecting';publish();timer=schedule(()=>connect(id,v),Math.min(1000*2**retries++,15000))}
 }
 function close(){epoch++;cancel(timer);if(socket){socket.onclose=null;socket.close();socket=null}state.requestId=null}
 return {state,close,async select(id){close();const v=epoch;state.requestId=id;state.events=[];state.cursor=0;state.execution=null;state.unavailable=false;completed=false;retries=0;streamCursor=0;namespace='gateway';await connect(id,v)}};
}
