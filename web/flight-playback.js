import {state,$,esc,toast,duration} from './state.js';
import {audio,stop} from './player.js';
import {renderPlayer} from './render.js';
import {flightAPI,flightIdentity} from './flight-api.js';
import {FlightBuffer,bufferChoices,bufferMinutes,saveBufferMinutes} from './flight-buffer.js';
let playback,timer,version=0;
function position(){
 const p=playback;if(!p)return 0;
 const value=p.loading||p.waitingFor?p.offset:p.gap?p.offset+(p.paused?0:performance.now()-p.anchor):p.buffer.position();
 return Math.max(0,Math.min(p.manifest.durationMs,value));
}
function remember(){if(playback)localStorage.setItem('flight:'+flightIdentity(),JSON.stringify({sessionId:playback.manifest.sessionId,revision:playback.manifest.revision,positionMs:position(),selectedStationId:playback.station,paused:playback.paused}));}
export function stopFlightPlayback(id){
 if(!playback||id&&playback.manifest.sessionId!==id)return;
 remember();clearTimeout(timer);version++;playback.buffer?.close();playback=null;state.flightPlayback=null;state.playing=false;
 audio.pause();audio.removeAttribute('src');audio.load();$('#flight-playback')?.replaceChildren();renderPlayer();
}
export async function openFlightPlayback(id){
 stopFlightPlayback();stop();const request=++version,manifest=(await flightAPI('flight-recorder/sessions/'+id+'/manifest')).data;if(request!==version)return;let saved;
 try{saved=JSON.parse(localStorage.getItem('flight:'+flightIdentity()));}catch{}
 playback={manifest,offset:saved?.sessionId===id&&saved.revision===manifest.revision?saved.positionMs:0,station:saved?.sessionId===id?saved.selectedStationId:manifest.tracks[0].stationId,paused:true,anchor:performance.now(),loading:true,minutes:bufferMinutes(),position};
 playback.offset=Math.max(0,Math.min(manifest.durationMs,Number(playback.offset)||0));
 if(!manifest.tracks.some(s=>s.stationId===playback.station))playback.station=manifest.tracks[0].stationId;
 audio.onloadedmetadata=null;state.flightPlayback=playback;render();await selectAudio();
}
export function refreshFlightPlayback(){render();}
function render(){
 renderPlayer();const host=$('#flight-playback');if(!host||!playback)return;
 host.innerHTML=`<h2>${esc(playback.manifest.name)}</h2><div class="hero-actions">${playback.manifest.tracks.map(t=>`<button class="${t.stationId===playback.station?'primary':'secondary'}" data-flight-station="${esc(t.stationId)}">${esc(t.name)}</button>`).join('')}<button class="secondary" id="flight-toggle" aria-label="${playback.paused?'Play recording':'Pause recording'}">${playback.paused?'Play':'Pause'}</button><label>Buffer ahead <select id="flight-buffer">${bufferChoices.map(n=>`<option value="${n}" ${n===playback.minutes?'selected':''}>${n} minute${n===1?'':'s'}</option>`).join('')}</select></label></div><label>Session position<input id="flight-seek" type="range" min="0" max="${playback.manifest.durationMs}" value="${position()}" step="1000"></label><p id="flight-position"></p><p id="flight-gap" aria-live="polite"></p>`;
 host.querySelectorAll('[data-flight-station]').forEach(b=>b.onclick=async()=>{playback.offset=position();playback.loading=true;playback.station=b.dataset.flightStation;render();await selectAudio();remember();});
 $('#flight-buffer').onchange=event=>{playback.minutes=Number(event.target.value);saveBufferMinutes(playback.minutes);if(playback.buffer){playback.buffer.minutes=playback.minutes;playback.buffer.fill(position());}};
 $('#flight-toggle').onclick=toggleFlight;$('#flight-seek').oninput=event=>seekFlight(Number(event.target.value));update();
}
export async function seekFlight(offset){
 const p=playback;if(!p)return;offset=Math.max(0,Math.min(p.manifest.durationMs,offset));
 if(!p.loading&&p.buffer?.canSeek(offset)){
  const buffer=p.buffer;p.offset=offset;p.loading=true;p.gap=false;p.waitingFor=null;
  try{await buffer.activate(buffer.track.segments.find(s=>offset>=s.startMs&&offset<s.startMs+s.durationMs),offset);
   if(playback!==p||p.buffer!==buffer)return;p.loading=false;buffer.fill(offset);if(!p.paused)await playAudio(p);render();schedule();remember();return;
  }catch(e){if(playback!==p||p.buffer!==buffer||e.name==='AbortError')return;}
 }
 p.offset=offset;p.loading=true;await selectAudio();remember();
}
export async function toggleFlight(){
 const p=playback;if(!p)return;
 if(!p.paused){p.offset=position();p.paused=true;audio.pause();}
 else {p.paused=false;p.anchor=performance.now();if(p.offset>=p.manifest.durationMs){await seekFlight(0);}else if(p.loading)await selectAudio();else if(!p.gap)await playAudio(p);}
 render();schedule();remember();
}
async function playAudio(p){try{await audio.play();}catch(e){if(playback===p&&e.name!=='AbortError'){p.paused=true;toast('Recording playback could not start.');render();}}}
async function selectAudio(){
 const p=playback;if(!p)return;const request=++version;p.loading=true;p.gap=false;p.waitingFor=null;p.error='';p.buffer?.close();audio.pause();
 const track=p.manifest.tracks.find(s=>s.stationId===p.station);
 p.buffer=new FlightBuffer(audio,track,p.manifest.durationMs,p.minutes,e=>{if(playback===p){p.error=e?.message||'';update();}});
 try{
  const seg=await p.buffer.open(p.offset);if(request!==version||playback!==p)return;
  p.gap=!seg||seg.startMs>p.offset+2;p.anchor=performance.now();p.loading=false;
  if(!p.paused&&!p.gap)await playAudio(p);
 }catch(e){if(request===version&&e.name!=='AbortError'){p.paused=true;p.error=e.message;toast(e.message);}}
 if(request===version){render();schedule();}
}
async function leaveGapOrAdvance(p,next){
 const buffer=p.buffer;p.offset=next.startMs;p.loading=true;p.gap=false;p.waitingFor=null;
 try{
  if(!buffer.has(next))await buffer.fill(next.startMs);if(playback!==p||p.buffer!==buffer)return;
  if(!buffer.has(next)){p.waitingFor=next;p.loading=false;return;}
  await buffer.activate(next,next.startMs);if(playback!==p||p.buffer!==buffer)return;
  p.loading=false;if(!p.paused)await playAudio(p);
 }catch(e){if(playback===p&&e.name!=='AbortError'){p.error=e.message;p.paused=true;}}
}
async function tick(){
 const p=playback;if(!p)return;
 if(!p.loading&&!p.paused){
  const t=position(),segments=p.buffer.track.segments;
  if(p.gap||p.waitingFor){const next=p.waitingFor||segments.find(s=>s.startMs>=p.offset-2);if(next&&t>=next.startMs)await leaveGapOrAdvance(p,next);}
  else if(!audio.seeking&&(audio.readyState<3||audio.ended)){
   // MSE can stop its clock slightly before a gap while queued audio drains.
   const previous=segments.find(s=>Math.abs(s.startMs+s.durationMs-t)<250);
   if(previous){const end=previous.startMs+previous.durationMs,next=segments.find(s=>s.startMs>=end-2&&s!==previous);
    if(!next||next.startMs>end+2){p.offset=t;p.anchor=performance.now();p.gap=true;}
    else if(!p.buffer.continuous&&audio.ended)await leaveGapOrAdvance(p,next);
   }
  }
 }
 if(playback!==p)return;
 if(!p.paused&&position()>=p.manifest.durationMs){p.offset=p.manifest.durationMs;p.loading=true;p.paused=true;audio.pause();render();remember();}
 p.buffer?.fill(position());update();schedule();
}
function update(){
 const p=playback;if(!p)return;const t=position(),track=p.manifest.tracks.find(s=>s.stationId===p.station);
 const label=$('#flight-position');if(label)label.textContent=`${duration(t/1000)} / ${duration(p.manifest.durationMs/1000)} · ${track.name} · ${duration((p.buffer?.ahead(t)||0)/1000)} buffered`;
 if($('#flight-seek'))$('#flight-seek').value=t;
 if($('#flight-gap'))$('#flight-gap').textContent=p.error|| (p.gap?'No recording at this time':p.loading||p.waitingFor||(!p.paused&&audio.readyState<3)?'Buffering…':'');
 $('#elapsed').textContent=duration(t/1000);$('#seek').value=t/p.manifest.durationMs*100;
 if(p.inGap!==p.gap){p.inGap=p.gap;renderPlayer();}
}
function schedule(){clearTimeout(timer);if(playback)timer=setTimeout(()=>tick().catch(e=>{if(playback){playback.error=e.message;update();schedule();}}),250);}
export function flightPlaying(){return !!playback;}
document.addEventListener('session-expired',()=>stopFlightPlayback());
