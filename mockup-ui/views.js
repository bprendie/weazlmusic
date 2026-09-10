import {albums,tracks,stations,state,escapeHTML as esc} from './data.js';
const cover = (a, cls='cover') => `<div class="${cls} art-${a.art}"><span class="cover-label">${esc(a.label || 'W/')}</span><span class="cover-title">${esc(a.title.toUpperCase())}</span></div>`;
const albumCard = (a,i) => `<button class="album-card" data-album="${i}" aria-label="Open ${esc(a.title)}">${cover(a)}<strong>${esc(a.title)}</strong><small>${esc(a.artist)} · ${a.year}</small></button>`;
const head = (label,title,description) => `<div class="page-head"><span class="eyebrow">${label}</span><h1>${title}</h1><p>${description}</p></div>`;
export function trackList(items){
  if(!items.length) return '<p class="empty">Nothing here yet. Pick a track and make it yours.</p>';
  return `<div class="recent">${items.map(t=>`<div class="track-row"><button data-track="${t.id}" aria-label="Preview ${esc(t.title)}"><div class="mini-cover art-${t.art}">▶</div><span><strong>${esc(t.title)}</strong><small>${esc(t.artist)} · ${esc(t.album || t.genre)}</small></span></button><span class="length">${t.duration}</span><button class="icon-button" data-add="${t.id}" aria-label="Queue ${esc(t.title)}">+</button></div>`).join('')}</div>`;
}
function home(){
 return `<section class="hero"><div class="hero-copy"><p class="eyebrow">GOOD SIGNAL. ZERO SLUDGE.</p><h1>Stay in the flow.</h1><p>Your collection. The underground airwaves.<br>All dialed in. All yours.</p><div class="hero-actions"><button class="primary" data-action="resume">▶ &nbsp; Pick up the needle</button><button class="secondary" data-view="radio">Find a frequency ↗</button></div></div><img src="weazlhead.png" alt="Pixel-art Weazl mascot" width="255" height="251"></section><section><div class="section-title"><h2>Fresh in the crates</h2><button class="text-button" data-view="albums">All albums ↗</button></div><div class="album-grid">${albums.map(albumCard).join('')}</div></section><section><div class="section-title"><h2>Back in rotation</h2><span class="eyebrow">SAMPLE LISTENING HISTORY</span></div>${trackList([tracks[0],tracks[6],tracks[9]])}</section>`;
}
function radio(){
 const list=stations.filter(s=>state.radioTab==='Presets'?s.preset:state.radioTab==='SomaFM'?s.artist==='SomaFM':state.radioTab==='Icecast'?false:s.artist==='My station');
 return head('WEAZLTUNES / RADIO','The underground is still on air.','Eight preset slots. A whole weird web beyond them.')+`<div class="tabs" aria-label="Radio source">${['Presets','SomaFM','Icecast','My stations'].map(t=>`<button data-tab="${t}" class="${t===state.radioTab?'active':''}" aria-pressed="${t===state.radioTab}">${t}</button>`).join('')}</div><div class="section-title"><span class="eyebrow">${state.radioTab==='Presets'?`${stations.filter(s=>s.preset).length} / 8 PRESETS · FROM WEAZLTUNES`:'SAMPLE DIRECTORY · NOT CONNECTED'}</span><span class="eyebrow">LIVE RADIO / PREVIEW</span></div><div class="station-grid">${list.map((s,i)=>`<article class="station"><div class="station-top"><span>${String(i+1).padStart(2,'0')} / ${esc(s.artist.toUpperCase())}</span><span>◎</span></div><h3>${esc(s.title)}</h3><p>${esc(s.genre)}</p><div class="station-bottom"><button data-track="${s.id}">▶ &nbsp; Tune in</button><button data-preset="${s.id}" aria-label="${s.preset?'Remove':'Add'} ${esc(s.title)} ${s.preset?'from':'to'} presets">${s.preset?'★ Preset':'☆ Preset'}</button></div></article>`).join('') || (state.radioTab==='Icecast'?'<p class="empty">Icecast directory is not connected in this mockup.<br>Your saved stations are ready in Presets.</p>':'<p class="empty">Your own frequencies go here. Add a stream below.</p>')}</div><form id="station-form" class="inline-form"><input type="url" name="url" required placeholder="Paste a stream URL…" aria-label="Stream URL"><button class="secondary">+ Add station</button></form><p class="eyebrow">URLS STAY IN THIS PREVIEW SESSION. NO STREAM IS OPENED.</p>`;
}
export function renderMain(){
 let html;
 if(state.query){
  const q=state.query.toLowerCase();
  const found=[...tracks,...stations].filter(t=>`${t.title} ${t.artist} ${t.album || ''} ${t.genre || ''}`.toLowerCase().includes(q));
  html=head('LOCAL SEARCH',`Results for “${esc(state.query)}”`,`${found.length} matches in the sample collection.`)+trackList(found);
 }else switch(state.view){
 case 'home': html=home();break;
 case 'radio': html=radio();break;
 case 'albums': html=head('THE COLLECTION','Dig into your crates.','Recently added · 4 sample albums')+`<div class="album-grid">${albums.map(albumCard).join('')}</div>`;break;
 case 'album': {const a=albums[state.album];html=head('THE COLLECTION / ALBUM',esc(a.title),`${esc(a.artist)} · ${a.year} · 3 preview tracks`)+`<button class="primary" data-play-album="${state.album}">▶ Play album</button><div style="margin-top:24px">${trackList(tracks.filter(t=>t.art===state.album))}</div>`;break;}
 case 'favorites':html=head('THE COLLECTION','Only the keepers.', 'Your favorite tracks. Stored in memory for this preview.')+trackList(tracks.filter(t=>state.favorites.has(t.id)));break;
 case 'playlists':html=head('YOUR MIXTAPES','Handpicked. Zero filler.','Sample playlists from your collection.')+`<div class="station-grid"><button class="station" data-view="mixtape"><span class="eyebrow">03 TRACKS / MIXTAPE</span><h3>After hours ↗</h3><p>A little space for the late shift.</p></button><button class="station" data-view="grind"><span class="eyebrow">04 TRACKS / MIXTAPE</span><h3>Zero-tax grindage ↗</h3><p>Keep the momentum. Protect the flow.</p></button></div>`;break;
 default:html=head('YOUR MIXTAPES',state.view==='grind'?'Zero-tax grindage':'After hours','A handpicked sample mix. No AI provider connected.')+trackList(state.view==='grind'?[tracks[0],tracks[3],tracks[6],tracks[9]]:[tracks[2],tracks[5],tracks[8]]);
 }
 document.querySelector('#content').innerHTML=html;
 document.querySelector('#breadcrumb').textContent=state.query?'SEARCH':state.view.toUpperCase();
 document.querySelectorAll('nav [data-view]').forEach(b=>{const active=b.dataset.view===state.view;b.classList.toggle('active',active);if(active)b.setAttribute('aria-current','page');else b.removeAttribute('aria-current');});
}
export function renderQueue(){
 document.querySelector('#queue-count').textContent=String(state.queue.length).padStart(2,'0');
 document.querySelector('#queue').innerHTML=state.queue.map((t,i)=>`<div class="queue-item"><button data-queue-play="${i}"><span class="queue-number">${String(i+1).padStart(2,'0')}</span><span><strong>${esc(t.title)}</strong><small>${esc(t.artist)}</small></span></button><button class="icon-button" data-remove="${i}" aria-label="Remove ${esc(t.title)} from queue">×</button></div>`).join('') || '<p class="empty">Room for the next good thing.<br>Add a track with +.</p>';
}
export function renderPlayer(){
 const t=state.current;document.querySelector('#now-title').textContent=t.title;
 document.querySelector('#now-artist').textContent=`${t.artist} · ${t.album || 'Live radio'}`;
 document.querySelector('#now-cover').className=`mini-cover art-${t.art}`;
 document.querySelector('#play').textContent=state.playing?'Ⅱ':'▶';
 document.querySelector('#play').setAttribute('aria-label',state.playing?'Pause preview':'Play preview');
 document.querySelector('#duration').textContent=t.duration;
 document.querySelector('#source').textContent=t.duration==='LIVE'?'RADIO':'LIBRARY';
 document.querySelector('#seek').disabled=t.duration==='LIVE';
 document.querySelector('#favorite').textContent=state.favorites.has(t.id)?'♥':'♡';
 document.querySelector('#favorite').setAttribute('aria-pressed',String(state.favorites.has(t.id)));
 document.querySelector('#favorite').disabled=t.duration==='LIVE';
 document.querySelector('#next').disabled=!state.queue.length;
}
