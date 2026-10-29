import {state, $, track, radioTrack, toast} from './state.js';
import {api, library, saveState} from './api.js';
import {renderMain, renderPlayer, renderQueue} from './render.js';
import {navigate} from './navigation.js';
import {play, playList, enqueue, toggle} from './player.js';
import {addToPlaylistDialog, playlistDialog, confirmDelete} from './dialogs.js';

let menu, opener, controller;
export function closeQuickActions(restore = false) {
  menu?.remove();menu = null;
  opener?.setAttribute('aria-expanded','false');
  if (restore && opener?.isConnected) opener.focus();
  opener = null;
}
export function cancelQuickActions() {controller?.abort();closeQuickActions();}
function download(kind, id) {
  const a = document.createElement('a');
  a.href = kind === 'track' ? `/api/media/download?${new URLSearchParams({id})}` : `/api/download/${kind}?${new URLSearchParams({id})}`;
  a.download = '';document.body.append(a);a.click();a.remove();
  toast('Download requested. Check your browser’s downloads.');
}
async function albumFor(t, signal) {
  if (t.albumId) return t.albumId;
  const data = await library('getSong',{id:t.id},signal);
  if (data.song?.albumId == null || data.song.albumId === '') throw new Error('No album is available for this track.');
  return String(data.song.albumId);
}
async function collection(kind, id, signal) {
  const data = await library(kind === 'album' ? 'getAlbum' : 'getPlaylist',{id},signal);
  const item = data[kind];
  if (!item?.id) throw new Error('This collection is unavailable.');
  return {item, tracks:(item.song || item.entry || []).map(track)};
}
function queueTracks(tracks, next = false) {
  if (!tracks.length) throw new Error('This collection has no playable tracks.');
  state.queue = next ? [...tracks,...state.queue] : [...state.queue,...tracks];
  renderQueue();renderPlayer();saveState();toast(next ? 'Playing next.' : 'Added to your queue.');
}
function itemsFor(target) {
  const kind = target.dataset.quickKind, index = Number(target.dataset.quickIndex);
  const entries = [], add = (label, run, danger = false) => entries.push({label,run,danger});
  if (['track','queue','current'].includes(kind)) {
    const t = kind === 'track' ? state.tracks[index] : kind === 'queue' ? state.queue[index] : state.current;
    if (!t || kind === 'current' && state.flightPlayback) return [];
    const queued = kind === 'queue';
    const remove = () => {if (state.queue[index] !== t) throw new Error('The queue changed. Open the menu again.');state.queue.splice(index,1);};
    add(kind === 'current' ? 'Play / pause' : 'Play now',() => {if (kind === 'current') return toggle();if (queued) remove();return play(t).then(() => {renderQueue();saveState();});});
    if (kind !== 'current') add('Play next',() => {if (queued) remove();queueTracks([t],true);});
    if (!queued) add('Add to queue',() => enqueue(t));
    if (!t.url) {
      add('Play album',async signal => {const id = await albumFor(t,signal);const out = await collection('album',id,signal);signal.throwIfAborted();playList(out.tracks);});
      add('Open album',async signal => {const id = await albumFor(t,signal);signal.throwIfAborted();state.album = {id};await navigate('album');});
      add('Add to playlist',() => addToPlaylistDialog([t],'this track'));
      add(state.favorites.has(t.id) ? 'Remove from favorites' : 'Add to favorites',async signal => {
        const star = !state.favorites.has(t.id);await api('favorite','POST',{id:t.id,star},signal);signal.throwIfAborted();
        star ? state.favorites.add(t.id) : state.favorites.delete(t.id);renderPlayer();
        if (['home','favorites'].includes(state.view)) await navigate(state.view,state.query);
      });
      add('Download original',() => download('track',t.id));
      if (kind === 'track' && state.view === 'playlist' && !state.query && state.playlist?.owner === state.connection?.username) {
        const playlist = state.playlist;
        add('Remove from playlist',async signal => {await api('playlists','POST',{id:playlist.id,remove:[index]},signal);signal.throwIfAborted();await navigate('playlist');},true);
      }
    }
    if (queued) add('Remove from queue',() => {remove();renderQueue();renderPlayer();saveState();},true);
  } else if (kind === 'album' || kind === 'playlist') {
    const item = kind === 'album' ? state.albums[index] : state.playlists[index];if (!item) return [];
    add(`Open ${kind}`,() => {state[kind] = item;return navigate(kind);});
    const withTracks = fn => async signal => {const out = await collection(kind,item.id,signal);signal.throwIfAborted();return fn(out.tracks);};
    add(`Play ${kind}`,withTracks(tracks => playList(tracks)));
    add('Play next',withTracks(tracks => queueTracks(tracks,true)));
    add('Add to queue',withTracks(tracks => queueTracks(tracks)));
    add('Add to playlist',withTracks(tracks => addToPlaylistDialog(tracks,`this ${kind}`)));
    add(`Download ${kind} ZIP`,() => download(kind,item.id));
    if (kind === 'playlist' && item.owner === state.connection?.username) {
      add('Rename playlist',() => {state.playlist = item;playlistDialog('rename');});
      add('Delete playlist',() => {state.playlist = item;confirmDelete();},true);
    }
  } else if (kind === 'station') {
    const station = state.visibleStations[index];if (!station) return [];
    add('Tune in',() => play(radioTrack(station)));
    const saved = state.stations.find(s => s.url === station.url);
    add(saved?.preset ? 'Remove preset' : 'Save preset',() => {
      if (!saved?.preset && state.stations.filter(s => s.preset).length >= 8) throw new Error('Eight presets full. Remove a preset first.');
      if (saved) saved.preset = !saved.preset;else state.stations.push({...station,preset:true});
      renderMain();saveState();
    });
    if (!saved) add('Save station',() => {state.stations.push({...station,preset:false});renderMain();saveState();});
    if (saved) add('Remove saved station',() => {state.stations = state.stations.filter(s => s.url !== station.url);renderMain();saveState();},true);
  } else if (kind === 'existing') {
    target.querySelectorAll('button[data-flight]:not(:disabled)').forEach(button => add(button.textContent.trim(),() => {if (button.isConnected) button.click();},['delete','stop','cancel'].includes(button.dataset.flight)));
  }
  return entries;
}
function show(target, x, y, focusTarget) {
  if (!state.user) return;
  const entries = itemsFor(target);if (!entries.length) return;
  closeQuickActions();opener = focusTarget || target.querySelector('button');opener?.setAttribute('aria-expanded','true');
  menu = document.createElement('div');menu.id = 'quick-actions';menu.className = 'quick-menu';menu.role = 'menu';menu.setAttribute('aria-label','Quick actions');
  entries.forEach(({label,run,danger}) => {
    const button = document.createElement('button');button.type = 'button';button.role = 'menuitem';button.tabIndex = -1;button.textContent = label;
    if (danger) button.className = 'danger';
    button.onclick = event => {
      event.stopPropagation();closeQuickActions(true);controller?.abort();controller = new AbortController();const signal = controller.signal;
      Promise.resolve().then(() => {signal.throwIfAborted();return run(signal);}).catch(error => {if (error.name !== 'AbortError') toast(error.message);});
    };
    menu.append(button);
  });
  document.body.append(menu);
  const rect = menu.getBoundingClientRect();
  menu.style.left = `${Math.max(8,Math.min(x,innerWidth-rect.width-8))}px`;
  menu.style.top = `${Math.max(8,Math.min(y,innerHeight-rect.height-8))}px`;
  menu.firstElementChild.focus();
}
export function bindQuickActions() {
  document.addEventListener('quick-actions-close',() => closeQuickActions());
  document.addEventListener('contextmenu',event => {
    const target = event.target.closest('[data-quick-kind]');if (!target || event.target.closest('input,textarea,a')) return;
    if (!itemsFor(target).length) return;
    event.preventDefault();show(target,event.clientX,event.clientY,event.target.closest('button'));
  });
  document.addEventListener('click',event => {
    const button = event.target.closest('[data-quick-open]');if (button) {
      const rect = button.getBoundingClientRect();show(button.closest('[data-quick-kind]'),rect.left,rect.bottom,button);
    }
    const collectionButton = event.target.closest('[data-download-collection]');
    if (collectionButton) download(collectionButton.dataset.downloadCollection,state[collectionButton.dataset.downloadCollection].id);
  });
  document.addEventListener('pointerdown',event => {if (menu && !menu.contains(event.target) && !event.target.closest('[data-quick-open]')) closeQuickActions();});
  document.addEventListener('keydown',event => {
    if (menu) {
      event.stopImmediatePropagation();
      const buttons = [...menu.querySelectorAll('button')], i = buttons.indexOf(document.activeElement);
      if (event.key === 'Escape' || event.key === 'Tab') {if(event.key === 'Escape')event.preventDefault();closeQuickActions(true);}
      if (['ArrowDown','ArrowUp','Home','End'].includes(event.key)) {event.preventDefault();buttons[event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length-1 : (i+(event.key === 'ArrowDown' ? 1 : buttons.length-1))%buttons.length].focus();}
      return;
    }
    if (event.key === 'ContextMenu' || event.shiftKey && event.key === 'F10') {
      const target = event.target.closest('[data-quick-kind]');if (!target || !itemsFor(target).length) return;
      event.preventDefault();event.stopImmediatePropagation();const rect = event.target.getBoundingClientRect();show(target,rect.left,rect.bottom,event.target);
    }
  },true);
  window.addEventListener('resize',() => closeQuickActions());
  // Browser focus/scroll-into-view can dispatch a deferred scroll after the
  // opening click. Dismiss on user scroll intent instead, so touch menus stay open.
  for (const type of ['wheel','touchmove']) document.addEventListener(type,event => {if (menu && !menu.contains(event.target)) closeQuickActions();},{passive:true,capture:true});
}
