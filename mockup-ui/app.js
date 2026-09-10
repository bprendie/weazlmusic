import {tracks,stations,state,escapeHTML as esc} from './data.js';
import {renderMain,renderQueue,renderPlayer} from './views.js';
const $=s=>document.querySelector(s);
let noticeTimer;
const history=[];
function toast(message){clearTimeout(noticeTimer);$('#toast').textContent=message;$('#toast').hidden=false;noticeTimer=setTimeout(()=>{$('#toast').hidden=true;},3000);}
function resetPosition(){$('#seek').value=0;$('#elapsed').textContent='0:00';}
function play(track){if(!track)return;history.push(state.current);state.current=track;state.playing=true;resetPosition();renderPlayer();toast('Playback preview only — no audio connected.');}
function navigate(view){state.view=view;state.query='';$('#search').value='';renderMain();}
function next(){if(state.queue.length){play(state.queue.shift());renderQueue();renderPlayer();}}
function previous(){const t=history.pop();if(t){state.current=t;resetPosition();renderPlayer();}}
function toggle(){state.playing=!state.playing;renderPlayer();toast(state.playing?'Playback preview only — no audio connected.':'Preview paused.');}
function modal(html){$('#modal-content').innerHTML=html;$('#modal').showModal();}
function signIn(){modal(`<img class="auth-brand" src="weazlhead.png" alt="Weazl"><span class="eyebrow purple">WEAZL / LOCAL ACCESS</span><h2>Your node. Your tunes.</h2><p>A local account, then straight to your music.<br>This screen previews the sign-in flow only.</p><form id="sign-in" class="auth-form"><label>Username<input name="username" autocomplete="off" value="${esc(state.user)}" required maxlength="40"></label><label>Password<input name="password" type="password" placeholder="Preview only — leave blank" autocomplete="off"></label><button class="primary">Enter preview →</button></form><p>No authentication is performed. Do not enter real credentials. Nothing is saved or sent.</p>`);}
function help(){modal('<span class="eyebrow purple">KEEP YOUR HANDS ON THE KEYS</span><h2>The short route.</h2>'+[['Search','/'],['Home / Albums / Favorites / Playlists / Radio','1–5'],['Play / pause preview','Space'],['Next / previous track','N / P'],['Close dialog / clear search','Esc'],['This cheat sheet','?']].map(([a,b])=>`<div class="shortcut"><span>${a}</span><kbd>${b}</kbd></div>`).join('')+'<button class="secondary" id="mobile-account" style="margin-top:24px">Preview local sign-in ↗</button>');}
document.addEventListener('click',e=>{
 const b=e.target.closest('button');if(!b)return;
 if(b.dataset.view)navigate(b.dataset.view);
 if(b.dataset.album!==undefined){state.album=Number(b.dataset.album);navigate('album');}
 if(b.dataset.track)play([...tracks,...stations].find(t=>t.id===b.dataset.track));
 if(b.dataset.add){state.queue.push([...tracks,...stations].find(t=>t.id===b.dataset.add));renderQueue();renderPlayer();toast('Added to your queue.');}
 if(b.dataset.remove!==undefined){state.queue.splice(Number(b.dataset.remove),1);renderQueue();renderPlayer();}
 if(b.dataset.queuePlay!==undefined){const [t]=state.queue.splice(Number(b.dataset.queuePlay),1);play(t);renderQueue();}
 if(b.dataset.tab){state.radioTab=b.dataset.tab;renderMain();}
 if(b.dataset.preset){const s=stations.find(t=>t.id===b.dataset.preset);if(!s.preset&&stations.filter(t=>t.preset).length>=8){toast('Eight slots full. Remove a preset first.');return;}s.preset=!s.preset;renderMain();}
 if(b.dataset.playAlbum!==undefined){const list=tracks.filter(t=>t.art===Number(b.dataset.playAlbum));state.queue=list.slice(1);play(list[0]);renderQueue();}
 if(b.dataset.action==='resume'){state.playing=true;renderPlayer();toast('Playback preview only — no audio connected.');}
 if(b.classList.contains('dialog-close'))$('#modal').close();
 if(b.id==='mobile-account'){$('#modal').close();signIn();}
});
$('.brand').addEventListener('click',e=>{e.preventDefault();navigate('home');});
$('#search').addEventListener('input',e=>{state.query=e.target.value.trim();renderMain();});
$('#play').onclick=toggle;$('#next').onclick=next;$('#previous').onclick=previous;
$('#clear-queue').onclick=()=>{state.queue=[];renderQueue();renderPlayer();};
$('#favorite').onclick=()=>{const id=state.current.id;state.favorites.has(id)?state.favorites.delete(id):state.favorites.add(id);renderPlayer();if(state.view==='favorites')renderMain();};
$('#shuffle').onclick=()=>{for(let i=state.queue.length-1;i>0;i--){const j=Math.floor(Math.random()*(i+1));[state.queue[i],state.queue[j]]=[state.queue[j],state.queue[i]];}renderQueue();toast('Queue shuffled.');};
$('#seek').oninput=e=>{const [m,s]=state.current.duration.split(':').map(Number);const sec=Math.round((m*60+s)*Number(e.target.value)/100);$('#elapsed').textContent=`${Math.floor(sec/60)}:${String(sec%60).padStart(2,'0')}`;};
$('#account').onclick=signIn;$('#help').onclick=help;
document.addEventListener('submit',e=>{
 if(e.target.id==='sign-in'){e.preventDefault();const form=new FormData(e.target);state.user=String(form.get('username')).trim()||'bob';$('#username').innerHTML=`${esc(state.user)}<small>Local account · preview</small>`;$('.avatar').textContent=state.user[0].toUpperCase();e.target.reset();$('#modal-content').replaceChildren();$('#modal').close();toast('Account UI preview — no authenticated session created.');}
 if(e.target.id==='station-form'){e.preventDefault();const url=new URL(new FormData(e.target).get('url'));if(!['http:','https:'].includes(url.protocol)){toast('Use an http or https stream URL.');return;}if(stations.some(s=>s.url===url.href)){toast('That station is already saved.');return;}stations.push({id:`custom-${stations.length}`,title:url.hostname,artist:'My station',genre:'Custom stream · preview',url:url.href,duration:'LIVE',art:1,preset:false});state.radioTab='My stations';renderMain();toast('Station added for this session. No stream opened.');}
});
document.addEventListener('keydown',e=>{
 if(e.ctrlKey||e.metaKey||e.altKey)return;
 if($('#modal').open)return;
 if(e.key==='Escape'){state.query='';$('#search').value='';$('#search').blur();renderMain();return;}
 if(e.target.matches('input,textarea,select')||e.target.isContentEditable)return;
 if(e.key==='/'){e.preventDefault();$('#search').focus();}
 if(e.code==='Space'&&!e.target.closest('button,a')){e.preventDefault();toggle();}
 if(e.key==='n')next();if(e.key==='p')previous();if(e.key==='?')help();
 if(/^[1-5]$/.test(e.key))navigate(['home','albums','favorites','playlists','radio'][Number(e.key)-1]);
});
$('#modal').addEventListener('close',()=>{$('#modal-content').replaceChildren();});
renderMain();renderQueue();renderPlayer();
