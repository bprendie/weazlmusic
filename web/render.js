import {state, $, esc, duration} from './state.js';
export const head = (label, title, description) => `<div class="page-head"><span class="eyebrow">${esc(label)}</span><h1>${esc(title)}</h1><p>${esc(description)}</p></div>`;
export function cover(item, small = false) {
  return item.coverArt ? `<img class="${small ? 'mini-cover' : 'cover album-art'}" width="400" height="400" src="/api/media/cover?id=${encodeURIComponent(item.coverArt)}" alt="" loading="lazy">` : `<div class="${small ? 'mini-cover' : 'cover'} art-2"><span class="cover-label">WEAZLTUNES</span>${small ? '' : `<span class="cover-title">${esc(item.title || item.name)}</span>`}</div>`;
}
export function trackList(items, editable = false) {
  if (!items.length) return '<p class="empty">Nothing here yet. Pick a track and make it yours.</p>';
  return `<div class="recent">${items.map((t,i) => `<div class="track-row"><button data-play="${i}" aria-label="Play ${esc(t.title)}">${cover(t,true)}<span><strong>${esc(t.title)}</strong><small>${esc(t.artist)} · ${esc(t.album)}</small></span></button><span class="length">${duration(t.duration)}</span><button class="icon-button" data-enqueue="${i}" aria-label="Queue ${esc(t.title)}">+</button>${editable ? `<button class="icon-button" data-playlist-remove="${i}" aria-label="Remove ${esc(t.title)} from playlist">×</button>` : ''}</div>`).join('')}</div>`;
}
function albums(shelf = false) {
  return `<div class="album-grid ${shelf ? 'album-shelf' : ''}" ${shelf ? 'id="fresh-shelf" tabindex="0" aria-label="Recently added albums"' : ''}>${state.albums.map((a,i) => `<button class="album-card" data-album="${i}" aria-label="Open ${esc(a.name)}">${cover(a)}<strong>${esc(a.name)}</strong><small>${esc(a.artist)} · ${esc(a.year || '')}</small></button>`).join('')}</div>`;
}
function home() {
  return `<section class="hero"><div class="hero-copy"><p class="eyebrow">GOOD SIGNAL. ZERO SLUDGE.</p><h1>Stay in the flow.</h1><p>Your collection. The underground airwaves.<br>All dialed in. All yours.</p><div class="hero-actions"><button class="primary" data-action="resume">▶ &nbsp; Pick up the needle</button><button class="secondary" data-view="radio">Find a frequency ↗</button></div></div><img src="weazlhead.png" alt="Weazl" width="255" height="251"></section><section><div class="section-title"><h2>Fresh in the crates</h2><div class="shelf-actions"><button class="icon-button" data-shelf="-1" aria-label="Previous recent albums">←</button><button class="icon-button" data-shelf="1" aria-label="Next recent albums">→</button><button class="text-button" data-view="albums">All albums ↗</button></div></div>${albums(true)}</section><section><div class="section-title"><h2>Your favorites</h2><button class="text-button" data-view="favorites">All favorites ↗</button></div>${trackList(state.tracks)}</section>`;
}
function radio() {
  const list = state.radioTab === 'Presets' ? state.stations.filter(s => s.preset) : state.radioTab === 'My stations' ? state.stations : state.directory;
  state.visibleStations = list;
  return head('WEAZLTUNES / RADIO', 'The underground is still on air.', 'Eight preset slots. A whole weird web beyond them.') +
    `<div class="tabs">${['Presets','SomaFM','Icecast','My stations'].map(tab => `<button data-tab="${tab}" class="${state.radioTab === tab ? 'active' : ''}">${tab}</button>`).join('')}</div>` +
    (state.radioTab === 'Icecast' ? '<form id="directory-search" class="inline-form"><input name="q" placeholder="Genre or station name" aria-label="Search Icecast"><button class="secondary">Search directory</button></form>' : '') +
    `<div class="section-title"><p class="eyebrow">${state.stations.filter(s => s.preset).length} / 8 PRESETS</p><span class="eyebrow">LIVE RADIO</span></div><div class="station-grid">${list.map((s,i) => {
      const saved = state.stations.find(item => item.url === s.url);
      return `<article class="station"><div class="station-top"><span>${String(i+1).padStart(2,'0')} / FREQUENCY</span><span>◎</span></div><h3>${esc(s.name)}</h3><p>${esc(new URL(s.url).hostname)}</p><div class="station-bottom"><button data-radio="${i}">▶ Tune in</button><button data-preset="${i}">${saved?.preset ? '★ Preset' : '☆ Preset'}</button>${state.radioTab === 'My stations' ? `<button data-station-delete="${i}" aria-label="Remove ${esc(s.name)}">×</button>` : ''}</div></article>`;
    }).join('') || '<p class="empty">No stations here. Add a stream or search the directory.</p>'}</div><form id="station-form" class="inline-form"><input name="name" placeholder="Station name" required maxlength="200" aria-label="Station name"><input type="url" name="url" required placeholder="Paste a stream URL…" aria-label="Stream URL"><button class="secondary">+ Add station</button></form>`;
}
export function renderMain() {
  let html = '';
  if (state.loading) html = '<p class="empty" role="status">Loading your collection…</p>';
  else if (state.error) html = `<p class="empty" role="alert">${esc(state.error)}</p><button class="secondary" data-action="retry">Try again</button>`;
  else if (state.query) html = head('LIBRARY SEARCH', `Results for “${state.query}”`, 'Up to 100 tracks from Navidrome.') + trackList(state.tracks);
  else switch (state.view) {
    case 'home': html = home(); break;
    case 'albums': html = head('THE COLLECTION','Dig into your crates.','Recently added to your Navidrome library.') + albums() + (state.more ? '<button class="secondary" data-action="more">Load more albums</button>' : ''); break;
    case 'album': html = head('THE COLLECTION / ALBUM',state.album?.name || '',state.album?.artist || '') + '<button class="primary" data-action="play-list">▶ Play album</button>' + trackList(state.tracks); break;
    case 'favorites': html = head('THE COLLECTION','Only the keepers.','Your favorites, saved on Navidrome.') + trackList(state.tracks); break;
    case 'playlists': html = head('YOUR PLAYLISTS','Handpicked. Zero filler.','These playlists live in your Navidrome account.') + '<button class="primary" data-action="new-playlist">+ New playlist</button><div class="station-grid playlist-grid">' + state.playlists.map((p,i) => `<button class="station" data-playlist="${i}"><span class="eyebrow">${p.songCount || 0} TRACKS / ${esc(p.owner)}</span><h3>${esc(p.name)} ↗</h3></button>`).join('') + '</div>'; break;
    case 'playlist': {
      const p = state.playlist, own = p?.owner === state.connection?.username;
      html = head('NAVIDROME PLAYLIST',p?.name || '',`${state.tracks.length} tracks · ${p?.owner || ''}`) + `<div class="hero-actions"><button class="primary" data-action="play-list">▶ Play playlist</button>${own ? '<button class="secondary" data-action="rename-playlist">Rename</button><button class="secondary" data-action="append-queue">Add queue</button><button class="secondary" data-action="delete-playlist">Delete</button>' : ''}</div>` + trackList(state.tracks,own); break;
    }
    case 'queue': html = head('YOUR PLAY QUEUE','The queue is yours.',`${state.queue.length} tracks up next.`) + '<div class="hero-actions"><button class="primary" data-action="save-queue">Save to playlist</button><button class="secondary" data-action="clear-queue">Clear queue</button></div>' + queueRows(); break;
    case 'radio': html = radio(); break;
  }
  $('#content').innerHTML = html;
  $('#radio-preset-count').textContent = `${state.stations.filter(s => s.preset).length} presets`;
  $('#breadcrumb').textContent = state.query ? 'SEARCH' : state.view.toUpperCase();
  document.querySelectorAll('nav [data-view]').forEach(b => {b.classList.toggle('active',b.dataset.view === state.view);b.setAttribute('aria-current', b.dataset.view === state.view ? 'page' : 'false');});
}
function queueRows() {
  return state.queue.map((t,i) => `<div class="queue-item"><button data-queue-play="${i}"><span class="queue-number">${String(i+1).padStart(2,'0')}</span><span><strong>${esc(t.title)}</strong><small>${esc(t.artist)}</small></span></button><button class="icon-button" data-move="${i}" aria-label="Move ${esc(t.title)} up" ${i === 0 ? 'disabled' : ''}>↑</button><button class="icon-button" data-remove="${i}" aria-label="Remove ${esc(t.title)} from queue">×</button></div>`).join('') || '<p class="empty">Room for the next good thing.<br>Add a track with +.</p>';
}
export function renderQueue() {
  $('#queue-count').textContent = String(state.queue.length).padStart(2,'0');
  $('#queue').innerHTML = queueRows();
  if (state.view === 'queue') renderMain();
}
export function renderPlayer() {
  const t = state.current;
  $('#build-mood').disabled = !t || !!t.url || !!state.mood?.running;
  const meta = t?.url ? state.radioMetadata : null;
  const title = meta?.title || t?.title || 'Ready when you are.';
  const subtitle = t?.url ? (meta?.title ? [meta.artist,t.title].filter(Boolean).join(' · ') : 'Live radio') : t ? [t.artist,t.album].filter(Boolean).join(' · ') : 'Pick a track or a frequency.';
  $('#now-title').textContent = title;$('#now-title').title = title;
  $('#now-artist').textContent = subtitle;$('#now-artist').title = subtitle;
  if ('mediaSession' in navigator && 'MediaMetadata' in window) navigator.mediaSession.metadata = t ? new MediaMetadata({title,artist:meta?.artist || (t.url ? t.title : t.artist),album:t.url ? t.title : t.album}) : null;
  $('#now-cover').innerHTML = t ? cover(t,true) : '<img src="weazlhead.png" alt="" class="mini-cover">';
  $('#play').textContent = state.playing ? 'Ⅱ' : '▶';
  $('#play').setAttribute('aria-label',state.playing ? 'Pause' : 'Play');
  $('#duration').textContent = t ? duration(t.duration) : '0:00';
  $('#source').textContent = t?.url ? 'RADIO' : 'LIBRARY';
  $('#seek').disabled = !t || !!t.url;
  $('#favorite').textContent = t && state.favorites.has(t.id) ? '♥' : '♡';
  $('#favorite').disabled = !t || !!t.url;
  $('#favorite').setAttribute('aria-pressed',String(!!t && state.favorites.has(t.id)));
  $('#next').disabled = !state.queue.length;
}
