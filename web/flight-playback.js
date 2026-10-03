import {state,$,esc,toast,duration} from './state.js';
import {audio,stop} from './player.js';
import {renderPlayer} from './render.js';
import {flightAPI,flightIdentity} from './flight-api.js';
let playback,timer,version=0;
const position=()=>playback?Math.min(playback.manifest.durationMs,playback.offset+(playback.paused?0:performance.now()-playback.anchor)):0;
function remember(){if(playback)localStorage.setItem('flight:'+flightIdentity(),JSON.stringify({sessionId:playback.manifest.sessionId,revision:playback.manifest.revision,positionMs:position(),selectedStationId:playback.station,paused:playback.paused}));}
export function stopFlightPlayback(id){
 if(!playback||id&&playback.manifest.sessionId!==id)return;
 remember();clearTimeout(timer);version++;playback=null;state.flightPlayback=null;state.playing=false;
 audio.pause();audio.removeAttribute('src');audio.load();$('#flight-playback')?.replaceChildren();renderPlayer();
}
export async function openFlightPlayback(id){
 stopFlightPlayback();stop();const manifest=(await flightAPI('flight-recorder/sessions/'+id+'/manifest')).data;let saved;
 try{saved=JSON.parse(localStorage.getItem('flight:'+flightIdentity()));}catch{}
 playback={manifest,offset:saved?.sessionId===id&&saved.revision===manifest.revision?saved.positionMs:0,station:saved?.sessionId===id?saved.selectedStationId:manifest.tracks[0].stationId,paused:true,anchor:performance.now(),asset:null};
 if(!manifest.tracks.some(s=>s.stationId===playback.station))playback.station=manifest.tracks[0].stationId;
 audio.onloadedmetadata=null;state.flightPlayback=playback;render();await selectAudio();
}
export function refreshFlightPlayback(){render();}
function render(){
 renderPlayer();const host=$('#flight-playback');if(!host||!playback)return;
 host.innerHTML=`<h2>${esc(playback.manifest.name)}</h2><div class="hero-actions">${playback.manifest.tracks.map(t=>`<button class="${t.stationId===playback.station?'primary':'secondary'}" data-flight-station="${esc(t.stationId)}">${esc(t.name)}</button>`).join('')}<button class="secondary" id="flight-toggle" aria-label="${playback.paused?'Play recording':'Pause recording'}">${playback.paused?'Play':'Pause'}</button></div><label>Session position<input id="flight-seek" type="range" min="0" max="${playback.manifest.durationMs}" value="${position()}" step="1000"></label><p id="flight-position"></p><p id="flight-gap" aria-live="polite"></p>`;
 host.querySelectorAll('[data-flight-station]').forEach(b=>b.onclick=async()=>{playback.station=b.dataset.flightStation;playback.asset=null;render();await selectAudio();remember();});
 $('#flight-toggle').onclick=toggleFlight;$('#flight-seek').oninput=event=>seekFlight(Number(event.target.value));update();
}
export async function seekFlight(offset){
 if(!playback)return;playback.offset=Math.max(0,Math.min(playback.manifest.durationMs,offset));playback.anchor=performance.now();playback.asset=null;await selectAudio();remember();
}
export async function toggleFlight(){
 if(!playback)return;playback.offset=position();playback.anchor=performance.now();
 if(playback.paused&&playback.offset>=playback.manifest.durationMs)playback.offset=0;
 playback.paused=!playback.paused;render();
 if(playback.paused){audio.pause();clearTimeout(timer);}else await selectAudio();remember();
}
async function selectAudio(){
 if(!playback)return;const request=++version,t=position(),track=playback.manifest.tracks.find(s=>s.stationId===playback.station);const segment=track.segments.find(s=>t>=s.startMs&&t<s.startMs+s.durationMs);audio.pause();
 if(!segment){audio.removeAttribute('src');audio.load();playback.asset=null;update();schedule();return;}
 if(playback.asset!==segment.assetId){
  const lease=(await flightAPI('media/leases','POST',{kind:'recordingSegment',resourceId:segment.assetId})).data;if(request!==version||!playback)return;
  playback.asset=segment.assetId;audio.src=lease.url;
  await new Promise(resolve=>{const ready=()=>{for(const event of ['loadedmetadata','error','abort'])audio.removeEventListener(event,ready);resolve();};for(const event of ['loadedmetadata','error','abort'])audio.addEventListener(event,ready,{once:true});audio.load();});if(request!==version||!playback)return;
 }
 const current=position();audio.currentTime=Math.max(0,(segment.mediaStartMs+current-segment.startMs)/1000);
 if(!playback.paused){try{await audio.play();}catch{toast('Recording playback could not start.');}}update();schedule();
}
function update(){
 if(!playback)return;const t=position();const track=playback.manifest.tracks.find(s=>s.stationId===playback.station);const seg=track.segments.find(s=>t>=s.startMs&&t<s.startMs+s.durationMs);
 const label=$('#flight-position');if(label)label.textContent=`${duration(t/1000)} / ${duration(playback.manifest.durationMs/1000)} · ${track.name}`;
 if($('#flight-seek'))$('#flight-seek').value=t;if($('#flight-gap'))$('#flight-gap').textContent=seg?'':'No recording at this time';
 $('#elapsed').textContent=duration(t/1000);$('#seek').value=t/playback.manifest.durationMs*100;
 const gap=!seg;if(playback.inGap!==gap){playback.inGap=gap;renderPlayer();}
 if(t>=playback.manifest.durationMs&&!playback.paused){playback.offset=t;playback.paused=true;audio.pause();clearTimeout(timer);render();remember();}
}
function schedule(){clearTimeout(timer);if(!playback||playback.paused)return;timer=setTimeout(async()=>{if(!playback)return;update();const t=position(),track=playback.manifest.tracks.find(s=>s.stationId===playback.station),segment=track.segments.find(s=>t>=s.startMs&&t<s.startMs+s.durationMs);if((segment?.assetId||null)!==playback.asset)await selectAudio();else schedule();},250);}
export function flightPlaying(){return !!playback;}
document.addEventListener('session-expired',()=>stopFlightPlayback());
