import {state, $, esc, toast} from './state.js';
import {api} from './api.js';
import {navigate, refreshPlaylists} from './navigation.js';
export function modal(html) { $('#modal-content').innerHTML = html; $('#modal').showModal(); }
export function closeModal() { $('#modal').close(); }
export function account() {
  modal(`<img class="auth-brand" src="weazlhead.png" alt="Weazl"><span class="eyebrow purple">YOUR LOCAL NODE</span><h2>${esc(state.user)}</h2><p>Web-app account</p><p>Navidrome: ${esc(state.connection?.url || "Not configured")}<br>User: ${esc(state.connection?.username || "—")}</p><div class="dialog-actions"><button class="secondary" id="configure-connection">Configure Navidrome</button><button class="secondary" id="configure-llm">LLM curator</button></div><button class="primary" id="logout">Sign out</button>`);
}
export function help() {
  modal('<span class="eyebrow purple">KEEP YOUR HANDS ON THE KEYS</span><h2>The short route.</h2>' + [['Search','/'],['Home / Albums / Favorites / Playlists / Radio','1–5'],['Play / pause','Space'],['Next / previous','N / P'],['Queue','6'],['Build Mood','M'],['Close / clear search','Esc']].map(([a,b]) => `<div class="shortcut"><span>${a}</span><kbd>${b}</kbd></div>`).join('') + '<button class="secondary" id="mobile-account">Your account ↗</button>');
}
export function playlistDialog(mode) {
  const rename = mode === 'rename';
  const items = mode === 'queue' ? [state.current?.url ? state.resume : state.current,...state.queue].filter(Boolean) : [];
  modal(`<span class="eyebrow purple">SAVE TO NAVIDROME</span><h2>${rename ? 'Rename playlist' : 'Make a playlist.'}</h2><p>${rename ? 'Updates this playlist in your account.' : `${items.length} tracks · owned by ${esc(state.connection?.username)}`}</p><form id="playlist-form" class="auth-form"><label>Playlist name<input name="name" required maxlength="200" value="${rename ? esc(state.playlist.name) : ''}"></label><button class="primary">Save to Navidrome →</button><p id="form-error" role="alert"></p></form>`);
  $('#playlist-form').onsubmit = async event => {
    event.preventDefault(); const button = event.target.querySelector('button'); button.disabled = true;
    try {
      const name = new FormData(event.target).get('name');
      const out = await api('playlists','POST',rename ? {id:state.playlist.id,name} : {name,songIds:items.filter(t => !t.url).map(t => t.id)});
      closeModal(); await refreshPlaylists();
      if (rename) await navigate('playlist');
      else if (out.playlist?.id) {state.playlist = out.playlist;await navigate('playlist');}
      else await navigate('playlists');
      toast('Saved to your Navidrome account.');
    } catch (error) { const target = $('#form-error'); if (target) target.textContent = error.message; }
    finally {button.disabled = false;}
  };
}
export function addToPlaylistDialog(items, label = 'tracks') {
  const tracks = [...new Map(items.filter(t => t && !t.url).map(t => [t.id,t])).values()];
  if (!tracks.length) {toast('Pick library tracks first.');return;}
  const owned = state.playlists.filter(p => p.owner === state.connection?.username);
  modal(`<span class="eyebrow purple">EDIT NAVIDROME PLAYLISTS</span><h2>Add ${esc(label)}.</h2><p>${tracks.length} tracks ready for ${esc(state.connection?.username)}.</p><form id="playlist-add-form" class="auth-form"><label>Destination<select name="target"><option value="new">New playlist</option>${owned.map(p => `<option value="${esc(p.id)}">${esc(p.name)} · ${p.songCount || 0} tracks</option>`).join('')}</select></label><label id="new-playlist-name">Playlist name<input name="name" required maxlength="200"></label><button class="primary">Save tracks →</button><p id="form-error" role="alert"></p></form>`);
  const form = $('#playlist-add-form'), name = $('#new-playlist-name'), select = form.elements.namedItem('target'), input = form.elements.namedItem('name');
  const sync = () => {name.hidden = select.value !== 'new';input.required = select.value === 'new';};
  select.onchange = sync;sync();
  form.onsubmit = async event => {
    event.preventDefault();const button = form.querySelector('button');button.disabled = true;
    try {
      const data = new FormData(form), ids = tracks.map(t => t.id);
      const body = data.get('target') === 'new' ? {name:data.get('name'),songIds:ids} : {id:data.get('target'),songIds:ids};
      const out = await api('playlists','POST',body);
      closeModal();await refreshPlaylists();
      const id = body.id || out.playlist?.id;
      if (id) {state.playlist = state.playlists.find(p => p.id === id) || out.playlist;await navigate('playlist');}
      else await navigate('playlists');
      toast(body.id ? 'Tracks added to your playlist.' : 'Playlist created in Navidrome.');
    } catch (error) {const target = $('#form-error');if (target) target.textContent = error.message;}
    finally {button.disabled = false;}
  };
}
export function confirmDelete() {
  const target = {...state.playlist};
  modal(`<span class="eyebrow purple">NAVIDROME PLAYLIST</span><h2>Delete “${esc(target.name)}”?</h2><p>This deletes the playlist from your Navidrome account. Your music files and current queue remain.</p><div class="dialog-actions"><button class="primary" id="confirm-delete">Delete playlist</button><button class="secondary dialog-close">Keep it</button></div><p id="form-error" role="alert"></p>`);
  $('#confirm-delete').onclick = async event => {
    event.target.disabled = true;
    try {await api('playlist-delete','POST',{id:target.id});closeModal();await refreshPlaylists();await navigate('playlists');toast('Playlist deleted from Navidrome.');}
    catch (error) {if ($('#form-error')) $('#form-error').textContent = error.message;event.target.disabled = false;}
  };
}
