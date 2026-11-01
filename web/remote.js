import {state,$,toast,duration} from './state.js';
import {uuid} from './device.js';
import {audio,stop,play} from './player.js';
import {renderPlayer,renderQueue} from './render.js';
import {cancelSave} from './api.js';
let pendingTransfer=null;
let feed=null, user='', players=[], busy=false, queueTimer, sliderTimer, localSnapshot=null;
const select=document.createElement('select');select.id='play-on';select.setAttribute('aria-label','Play on');
const label=document.createElement('label');label.textContent='Play on ';label.append(select);
const status=document.createElement('span');status.id='remote-status';status.setAttribute('role','status');
label.className='remote-output';label.append(status);$('.topbar').append(label);
async function request(path,method='GET',body,headers={}){
 const response=await fetch('/api/v1/remote/'+path,{method,headers:{'Content-Type':'application/json',...(method==='GET'?{}:{'X-Weazl-Request':'1','Idempotency-Key':uuid()}),...headers},body:body===undefined?undefined:JSON.stringify(body)});
 const json=await response.json();if(!response.ok)throw new Error(json.error?.message||'Remote control unavailable.');return json.data;
}
function entry(t){return {entryId:t._entryId||uuid(),trackId:t.id,title:t.title,artist:t.artist,album:t.album,duration:t.duration||0,coverArt:t.coverArt||''};}
function libraryPlayback(t,queue){const first=entry(t);return {kind:'library',queue:[first,...queue.map(entry)],currentEntryId:first.entryId,positionMs:Math.round((t.position||0)*1000),playWhenReady:state.playing};}
function playback(t,queue){
 if(!t)throw new Error('Choose a track or station first.');
 if(t.url){const station=state.stations.find(s=>s.url===t.url&&s.id);if(!station)throw new Error('Save this station before transferring.');return {kind:'radioLive',stationId:station.id,queue:[],positionMs:0,playWhenReady:state.playing,heldLibrary:state.resume?libraryPlayback(state.resume,queue):undefined};}
 return libraryPlayback(t,queue);
}
function mirror(p){
 state.remote=p;select.disabled=busy;const s=p.state,b=s.playback;
 if(b?.kind==='library'){const current=b.queue.find(e=>e.entryId===b.currentEntryId);const convert=e=>({id:e.trackId,title:e.title||e.trackId,artist:e.artist||'',album:e.album||'',duration:e.duration||0,coverArt:e.coverArt||'',_entryId:e.entryId});state.current=current?convert(current):null;state.queue=b.queue.slice(b.queue.indexOf(current)+1).map(convert);}
 else if(b?.kind==='radioLive'){const station=state.stations.find(s=>s.id===b.stationId);state.current={id:b.stationId,url:station?.url||'remote-radio',title:station?.name||'Live radio',duration:0};if(b.heldLibrary){const held=b.heldLibrary,at=held.queue.findIndex(e=>e.entryId===held.currentEntryId),convert=e=>({id:e.trackId,title:e.title||e.trackId,artist:e.artist,album:e.album,duration:e.duration,coverArt:e.coverArt,_entryId:e.entryId});state.resume={...convert(held.queue[at]),position:held.positionMs/1000};state.queue=held.queue.slice(at+1).map(convert);}else{state.resume=null;state.queue=[];}}
 state.radioMetadata=s.radioMetadata||null;state.playing=s.transport==='playing';renderPlayer();renderQueue();
 $('#elapsed').textContent=duration(s.positionMs/1000);$('#volume').value=s.volume*100;
 for(const [id,command] of [['play',state.playing?'pause':'play'],['stop','stop'],['next','next'],['previous','previous'],['seek','seek'],['volume','setVolume'],['shuffle','replaceQueue']]){const el=$('#'+id);el.disabled=busy||!p.online||!p.commands.includes(command)||(command==='seek'&&s.playback?.kind!=='library')||(['play','pause','next','previous'].includes(command)&&!s.playback);}
 status.textContent=p.online?`${p.name} · ${s.transport}${busy?' · Pending…':''}`:`${p.name} is unreachable; audio may continue.`;
}
function options(){const current=state.remote?.id||'';select.replaceChildren(new Option('This browser',''));for(const p of players){const option=new Option(p.name+(p.online?'':' (unreachable)'),p.id);option.disabled=!p.online;select.append(option);}select.value=current;select.title=players.length?'Choose playback output':'No receivers connected. Sign in on a receiver and connect it.';if(!players.length&&!state.remote)status.textContent='No receivers connected. Sign in on a receiver to add it.';}
async function command(p,type,extra={}){
 const c=await request('players/'+p.id+'/commands','POST',{type,...extra},{'If-Match':`"v${p.revision}"`});if(type==='load'&&pendingTransfer?.playerId===p.id)pendingTransfer.commandId=c.id;
 for(let i=0;i<70;i++){
  const result=await request('players/'+p.id+'/commands/'+c.id);
  if(result.status==='applied')return await request('players/'+p.id);
  if(result.status==='rejected')throw new Error('Receiver rejected this control. Refresh and try again.');
  if(result.status==='outcome_unknown')throw new Error('Playback status is uncertain. Reconnect the receiver to reconcile before trying again.');
  await new Promise(resolve=>setTimeout(resolve,250));
 }
 throw new Error('Playback status is uncertain. The receiver may be playing.');
}
export function remoteControl(type,extra={}){
 if(!state.remote)return;
 if(['seek','setVolume'].includes(type)){clearTimeout(sliderTimer);const apply=()=>{if(!state.remote)return;if(busy){sliderTimer=setTimeout(apply,100);return;}perform(type,extra);};sliderTimer=setTimeout(apply,150);return;}
 return perform(type,extra);
}
async function perform(type,extra,expected=null){if(busy){toast('Wait for the pending receiver command.');return;}busy=true;mirror(state.remote);try{const target=state.remote.id,account=state.user;const p=expected||state.remote;const applied=await command(p,type,extra);if(state.user===account&&state.remote?.id===target)mirror(applied);}catch(e){toast(e.message);}finally{busy=false;if(state.remote)mirror(state.remote);}}
export function remoteLoad(t,queue){try{const b=playback(t,queue);b.playWhenReady=true;return perform('load',{playback:b});}catch(e){toast(e.message);}}
select.onchange=async()=>{
 const target=select.value;
 if(busy){options();return;}
 if(state.flightPlayback||state.mood?.running){toast('Finish recordings or Mood before transferring playback.');options();return;}
 if(state.remote&&target){toast('Return to This browser before choosing another receiver.');options();return;}
 busy=true;select.disabled=true;state.remotePending=true;const version=state.transferVersion||0;
 try{
  if(target){
   const p=await request('players/'+target);const b=playback(state.current,state.queue);if(b.kind==='library')b.positionMs=Math.round(audio.currentTime*1000);
   localSnapshot=structuredClone({current:state.current,queue:state.queue,resume:state.resume});if(b.kind==='library')localSnapshot.current.position=b.positionMs/1000;state.localOutputSaved=localSnapshot;
   pendingTransfer={playerId:p.id,version,playback:b};const applied=await command(p,'load',{playback:b});pendingTransfer=null;
   if(version!==(state.transferVersion||0)){toast('Earlier transfer started on the receiver. Use Play on to control or stop it.');return;}
   cancelSave();stop();mirror(applied);
  }else if(state.remote){
   const p=await request('players/'+state.remote.id);const snapshot=structuredClone(p.state);
   try{await command(p,'stop');}catch(e){if(!window.confirm(e.message+' The receiver may still be playing. Start in this browser anyway?'))throw e;}
   state.remote=null;state.localOutputSaved=null;for(const id of ['volume','stop'])$('#'+id).disabled=false;const b=snapshot.playback;
   if(b?.kind==='library'){const current=b.queue.find(e=>e.entryId===b.currentEntryId);const convert=e=>({id:e.trackId,title:e.title||e.trackId,artist:e.artist,album:e.album,duration:e.duration,coverArt:e.coverArt});state.queue=b.queue.slice(b.queue.indexOf(current)+1).map(convert);if(current){await play({...convert(current),position:snapshot.positionMs/1000});if(snapshot.transport!=='playing')audio.pause();}}
   else {if(b?.heldLibrary){const h=b.heldLibrary,i=h.queue.findIndex(e=>e.entryId===h.currentEntryId),convert=e=>({id:e.trackId,title:e.title||e.trackId,artist:e.artist,album:e.album,duration:e.duration,coverArt:e.coverArt});state.resume={...convert(h.queue[i]),position:h.positionMs/1000};state.queue=h.queue.slice(i+1).map(convert);}else{state.queue=[];state.resume=null;}const station=state.stations.find(s=>s.id===b?.stationId);if(station){await play({id:station.id,url:station.url,title:station.name});if(snapshot.transport!=='playing')audio.pause();}}
   status.textContent='';renderPlayer();renderQueue();
  }
 }catch(e){toast(e.message);}finally{busy=false;select.disabled=false;state.remotePending=false;options();}
};
document.addEventListener('remote-queue-save',()=>{
 const observed=state.remote?.state.playback;if(!observed)return;const held=observed.kind==='library'?observed:observed.heldLibrary;const index=held?.queue.findIndex(e=>e.entryId===held.currentEntryId)??-1;if(JSON.stringify(state.queue.map(e=>e._entryId||'new:'+e.id))===JSON.stringify((held?.queue.slice(index+1)||[]).map(e=>e.entryId)))return;
 const current=structuredClone(state.current),queue=structuredClone(state.queue),expected=structuredClone(state.remote),target=state.remote?.id;
 clearTimeout(queueTimer);
 const submit=()=>{
  if(!state.remote||state.remote.id!==target)return;
  if(busy){queueTimer=setTimeout(submit,100);return;}
  try{const b=playback(current,queue);if(b.kind==='library'){const old=state.remote.state.playback;const i=old.queue.findIndex(e=>e.entryId===old.currentEntryId);b.queue=[...old.queue.slice(0,i),...b.queue];}else if(!b.heldLibrary&&queue.length){b.heldLibrary=libraryPlayback(queue[0],queue.slice(1));}else if(b.heldLibrary&&held){b.heldLibrary.queue=[...held.queue.slice(0,index),...b.heldLibrary.queue];}b.positionMs=state.remote.state.positionMs;perform('replaceQueue',{playback:b},expected);}catch(e){toast(e.message);}
 };
 queueTimer=setTimeout(submit,200);
});
setInterval(()=>{
 if(state.user!==user){feed?.close();feed=null;user=state.user;players=[];state.remote=null;clearTimeout(queueTimer);clearTimeout(sliderTimer);options();if(user){const connectedUser=user;feed=new EventSource('/api/v1/remote/events');feed.addEventListener('snapshot',event=>{if(state.user!==connectedUser)return;players=JSON.parse(event.data);options();if(pendingTransfer?.commandId&&!busy){const p=players.find(p=>p.id===pendingTransfer.playerId);if(p?.state.lastAppliedCommandId===pendingTransfer.commandId&&p.state.transport!=='buffering'){if(pendingTransfer.version===(state.transferVersion||0)){cancelSave();stop();mirror(p);}else toast('Earlier transfer is playing on '+p.name+'.');pendingTransfer=null;}}if(state.remote){const p=players.find(p=>p.id===state.remote.id);if(p)mirror(p);else{status.textContent='Receiver unavailable. Return to This browser explicitly.';}}});feed.onerror=()=>{status.textContent='Receiver connection interrupted; audio may continue.';};}}
},500);
options();
