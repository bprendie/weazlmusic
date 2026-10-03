import {state,$,esc,toast} from './state.js';
import {flightAPI,listFlights,flightIdentity} from './flight-api.js';
import {openFlightPlayback,stopFlightPlayback,refreshFlightPlayback} from './flight-playback.js';
let timer,generation=0,delay=5000,deleting=false,selectionIdentity='';
const selected=new Set();
const deletable=j=>!['scheduled','recording','finalizing'].includes(j.state);
function updateSelection(){
 const count=selected.size,available=state.flight?.sessions.filter(deletable)||[];
 const button=$('#flight-delete-selected');if(button){button.disabled=count===0||deleting;button.textContent=`Delete selected (${count})`;}
 const all=$('#flight-select-all');if(all){all.checked=available.length>0&&count===available.length;all.indeterminate=count>0&&count<available.length;}
}
export function closeFlightRecorder(){clearTimeout(timer);generation++;}
const bytes=n=>`${(Number(n)/(1024**3)).toFixed(2)} GB`;
const instant=t=>new Date(t).toLocaleString();
export async function renderFlightRecorder(){
 if(deleting)return;
 closeFlightRecorder();const gen=generation;
 try{
 const [sessions,schedules,storage,stations]=await Promise.all([listFlights('flight-recorder/sessions'),listFlights('flight-recorder/schedules'),flightAPI('flight-recorder/storage'),flightAPI('radio/stations')]);
 if(gen!==generation||state.view!=='flight-recorder')return;
 state.flight={sessions,schedules,storage:storage.data,stations:stations.data};delay=5000;
 if(selectionIdentity!==flightIdentity()){selected.clear();selectionIdentity=flightIdentity();}
 const removable=new Set(sessions.filter(deletable).map(j=>j.id));for(const id of selected)if(!removable.has(id))selected.delete(id);
 $('#content').innerHTML=`<p class="eyebrow purple">INTERNET RADIO</p><h1>Flight Recorder</h1><p>Record your presets here. Download recordings in Subweazl for offline listening.</p><div class="hero-actions"><button class="primary" data-flight="new">+ Schedule recording</button><button class="secondary" data-flight="now">Record now</button></div><p>${bytes(storage.data.usedBytes)} stored · ${bytes(storage.data.accountAvailableBytes)} available · ${bytes(storage.data.reserveBytes)} disk reserve. Up to six streams at once. ${storage.data.retention.enabled?`Server copies kept ${storage.data.retention.days} days.`:'Server copies kept until deleted.'}</p><h2>Upcoming recordings</h2><div class="flight-list">${schedules.filter(s=>s.state==='active').map(s=>`<article><strong>${esc(s.name)}</strong><p>${instant(s.nextStartsAt)} → ${instant(s.nextEndsAt)} · ${esc(s.timeZone)} · ${esc(s.recurrence.kind)}</p><p>${s.stationSnapshots.map(x=>esc(x.name)).join(' · ')}</p>${(s.timeCorrections||[]).map(x=>`<p>${esc(x)}</p>`).join('')}${s.error?`<p role="status">${esc(s.error)}</p>`:''}<button class="secondary" data-flight="edit" data-id="${esc(s.id)}">Edit</button> <button class="secondary" data-flight="cancel" data-id="${esc(s.id)}">Cancel future recordings</button></article>`).join('')||'<p>No upcoming recordings.</p>'}</div><h2>Saved recordings</h2><p>Select the recordings you want to delete from the server.</p>${sessions.some(deletable)?'<div class="flight-selection"><label><input type="checkbox" id="flight-select-all"> Select all finished recordings</label><button class="secondary" id="flight-delete-selected" data-flight="delete-selected" disabled>Delete selected (0)</button></div>':''}<div class="flight-list">${sessions.slice().sort((a,b)=>b.startsAt.localeCompare(a.startsAt)).map(j=>`<article data-flight-session="${esc(j.id)}"><div class="flight-recording-title">${deletable(j)?`<input type="checkbox" data-flight-select="${esc(j.id)}" aria-label="Select ${esc(j.name)}" ${selected.has(j.id)?'checked':''}>`:''}<strong>${esc(j.name)}</strong> <span class="tag">${esc(j.state)}</span></div><p>${instant(j.startsAt)} · ${j.stationCount} presets · ${bytes(j.capturedBytes)}</p><p>${j.stations.map(s=>`${esc(s.station.name)}: ${esc(s.state)}${s.error?' · '+esc(s.error):''}`).join('<br>')}</p>${['scheduled','recording','finalizing'].includes(j.state)?`<button class="secondary" data-flight="stop" data-id="${esc(j.id)}">Stop recording</button>`:`${j.manifestRevision?`<button class="primary" data-flight="play" data-id="${esc(j.id)}">Listen</button> `:''}<button class="secondary" data-flight="delete" data-id="${esc(j.id)}">Delete server recording</button>`}</article>`).join('')||'<p>No recorded sessions yet.</p>'}</div><div id="flight-playback"></div>`;
 updateSelection();refreshFlightPlayback();
 timer=setTimeout(()=>{if(state.view==='flight-recorder'&&!$('#flight-form'))renderFlightRecorder();},delay);
 }catch(error){if(gen===generation){toast(error.message);delay=Math.min(delay*2,60000);timer=setTimeout(renderFlightRecorder,delay);}}
}
function localInput(t){const date=new Date(t);return new Date(date.getTime()-date.getTimezoneOffset()*60000).toISOString().slice(0,16);}
function form(schedule,now){
 clearTimeout(timer);const favorites=state.flight.stations.filter(s=>s.preset);const ids=schedule?.stationIds||favorites.slice(0,6).map(s=>s.id);const initial=new Date(Date.now()+60000);const start=schedule?new Date(schedule.nextStartsAt):initial;const end=schedule?new Date(schedule.nextEndsAt):new Date(start.getTime()+4*3600000);const zone=Intl.DateTimeFormat().resolvedOptions().timeZone;
 $('#content').innerHTML=`<p class="eyebrow purple">FLIGHT RECORDER</p><h1>${schedule?'Edit recording':now?'Record now':'Schedule recording'}</h1><p>Up to six favorite presets. ${bytes(state.flight.storage.accountAvailableBytes)} available.</p><form id="flight-form" class="auth-form"><label>Name<input name="name" value="${esc(schedule?.name||'Flight recording')}" required maxlength="200"></label><fieldset><legend>Favorite presets <span id="flight-count"></span></legend>${favorites.map(s=>`<label class="flight-choice"><input type="checkbox" name="station" value="${esc(s.id)}" ${ids.includes(s.id)?'checked':''}> ${esc(s.name)}</label>`).join('')||'<p>Save a favorite preset under Radio first.</p>'}</fieldset>${now?'<label>Duration (hours)<input name="duration" type="number" value="4" min="0.01" max="12" step="0.01" required></label>':`<label>Start (${esc(zone)})<input type="datetime-local" name="start" value="${localInput(start)}" required></label><label>End time (${esc(zone)})<input type="time" name="end" value="${localInput(end).slice(11)}" required></label><label>Repeat<select name="repeat"><option value="once">Once</option><option value="daily">Daily</option><option value="weekly">Selected weekdays</option></select></label><fieldset><legend>Weekdays</legend>${['Mon','Tue','Wed','Thu','Fri','Sat','Sun'].map((x,i)=>`<label class="flight-choice"><input type="checkbox" name="day" value="${i+1}" ${(schedule?.recurrence.weekdays||[1,2,3,4,5]).includes(i+1)?'checked':''}> ${x}</label>`).join('')}</fieldset><label>Recurrence time zone<input name="zone" value="${esc(schedule?.timeZone||zone)}" required></label><p id="flight-window" aria-live="polite"></p>`}<p id="flight-estimate" aria-live="polite"></p><p id="flight-error" role="alert"></p><div class="hero-actions"><button class="primary">${now?'Start recording':'Save schedule'}</button><button type="button" class="secondary" data-flight="back">Back</button></div></form>`;
 const f=$('#flight-form');if(schedule)f.elements.repeat.value=schedule.recurrence.kind;
 function window(){const s=new Date(f.elements.start.value);const e=new Date(f.elements.start.value.slice(0,10)+'T'+f.elements.end.value);if(e<s)e.setDate(e.getDate()+1);if(+s===+e)throw new Error('Start and end must differ.');return [s,e];}
 function update(){const chosen=[...f.querySelectorAll('[name=station]:checked')].length;$('#flight-count').textContent=`${chosen}/6`;f.querySelectorAll('[name=station]').forEach(x=>x.disabled=!x.checked&&chosen>=6);let seconds=Number(f.elements.duration?.value||4)*3600;
 if(!now){try{const [s,e]=window();seconds=(e-s)/1000;$('#flight-window').textContent=`${instant(s)} → ${instant(e)} (${seconds/3600} hours). Exact UTC window: ${s.toISOString()} → ${e.toISOString()}. Overnight end times use the next day.`;}catch{$('#flight-window').textContent='Choose a valid start and end.';}}
 $('#flight-estimate').textContent=`Estimated storage with headroom: ${bytes(seconds*40000*chosen)}. Server checks capacity and disk space before saving.`;}
 f.oninput=update;update();f.onsubmit=async event=>{event.preventDefault();const button=f.querySelector('button');button.disabled=true;try{const data=new FormData(f);const selected=data.getAll('station');if(!selected.length||selected.length>6)throw new Error('Choose one to six presets.');if(now){await flightAPI('flight-recorder/sessions','POST',{name:data.get('name'),stationIds:selected,durationMs:Math.round(Number(data.get('duration'))*3600000)});}else{const [s,e]=window();const kind=data.get('repeat');const rule={kind};if(kind!=='once'){rule.localStart=data.get('start').slice(11);rule.localEnd=data.get('end');rule.weekdays=data.getAll('day').map(Number);rule.endDate=null;}const body={name:data.get('name'),stationIds:selected,startsAt:s.toISOString(),endsAt:e.toISOString(),timeZone:kind==='once'?zone:data.get('zone'),recurrence:rule};await flightAPI('flight-recorder/schedules'+(schedule?'/'+schedule.id:''),schedule?'PATCH':'POST',body,schedule?.version);}toast(now?'Recording reserved. The server will capture it independently.':'Schedule saved.');await renderFlightRecorder();}catch(error){$('#flight-error').textContent=error.message;}finally{button.disabled=false;}};
}
document.addEventListener('click',async event=>{const b=event.target.closest('[data-flight]');if(!b)return;const action=b.dataset.flight,id=b.dataset.id;try{
 if(action==='new'||action==='now'){form(null,action==='now');return;}if(action==='edit'){form(state.flight.schedules.find(s=>s.id===id),false);return;}if(action==='back'){await renderFlightRecorder();return;}if(action==='play'){await openFlightPlayback(id);return;}
 if(action==='delete'||action==='delete-selected'){await deleteRecordings(action==='delete'?[id]:[...selected]);return;}
 if(action==='cancel'){const s=state.flight.schedules.find(s=>s.id===id);await flightAPI('flight-recorder/schedules/'+id,'DELETE',undefined,s.version);}if(action==='stop')await flightAPI('flight-recorder/sessions/'+id+'/stop','POST',{});
 await renderFlightRecorder();
 }catch(error){toast(error.message);}});

document.addEventListener('change',event=>{
 const input=event.target;if(input.id==='flight-select-all'){
  for(const j of state.flight.sessions.filter(deletable)){if(input.checked)selected.add(j.id);else selected.delete(j.id);}
  document.querySelectorAll('[data-flight-select]').forEach(box=>box.checked=selected.has(box.dataset.flightSelect));
 }else if(input.matches('[data-flight-select]')){if(input.checked)selected.add(input.dataset.flightSelect);else selected.delete(input.dataset.flightSelect);}else return;
 updateSelection();
});
async function deleteRecordings(ids){
 if(deleting)return;const jobs=state.flight.sessions.filter(j=>ids.includes(j.id)&&deletable(j));if(!jobs.length)return;
 if(!confirm(`Delete ${jobs.length===1?'“'+jobs[0].name+'”':jobs.length+' selected recordings'} from the server? Phone copies are managed separately.`))return;
 deleting=true;closeFlightRecorder();updateSelection();const failures=[];let removed=0;
 try{for(const j of jobs){try{await flightAPI('flight-recorder/sessions/'+j.id,'DELETE',undefined,j.version);stopFlightPlayback(j.id);selected.delete(j.id);removed++;}catch(error){failures.push(`${j.name}: ${error.message}`);}}}
 finally{deleting=false;await renderFlightRecorder();}
 toast(failures.length?`${removed} deleted. ${failures.join(' · ')}`:`${removed} recording${removed===1?'':'s'} deleted from the server.`);
}
