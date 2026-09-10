import {state, $, esc, radioTrack, toast, track} from './state.js';
import {api, library, saveState, cancelSave, flushState} from './api.js';
import {renderMain, renderPlayer, renderQueue} from './render.js';
import {navigate, refreshPlaylists, cancelNavigation} from './navigation.js';
import {audio, play, toggle, next, previous, playList, enqueue, stop, resetPlayer} from './player.js';
import {account, help, closeModal, playlistDialog, confirmDelete} from './dialogs.js';
import {bindAuth, openConnection} from './auth.js';
import {openLLMSettings} from './llm-settings.js';
import {buildMood, cancelMood} from './mood.js';
let searchTimer;
function signedOut() {
 cancelMood(true);
  cancelNavigation(); cancelSave(); clearTimeout(searchTimer); resetPlayer();
  state.user = '';state.connection = null;state.queue = [];state.stations = [];state.tracks = [];state.albums = [];state.playlists = [];state.directory = [];state.favorites.clear();
  state.album = null;state.playlist = null;state.view = 'home';state.query = '';state.radioTab = 'Presets';
  $('.app').hidden = true;$('#login-screen').hidden = false;$('#toast').hidden = true;closeModal();
  $('#content').replaceChildren();$('#queue').replaceChildren();
}
async function action(button) {
  const d = button.dataset;
  if (d.shelf) { const shelf = $('#fresh-shelf');shelf?.scrollBy({left:Number(d.shelf) * shelf.clientWidth * 0.8,behavior:'auto'}); }
  if (d.view) {clearTimeout(searchTimer);await navigate(d.view);}
  if (d.album !== undefined) {state.album = state.albums[Number(d.album)];await navigate('album');}
  if (d.playlist !== undefined) {state.playlist = state.playlists[Number(d.playlist)];await navigate('playlist');}
  if (d.play !== undefined) await play(state.tracks[Number(d.play)]);
  if (d.enqueue !== undefined) enqueue(state.tracks[Number(d.enqueue)]);
  if (d.remove !== undefined) {state.queue.splice(Number(d.remove),1);renderQueue();renderPlayer();saveState();}
  if (d.move !== undefined) {const i = Number(d.move);[state.queue[i-1],state.queue[i]] = [state.queue[i],state.queue[i-1]];renderQueue();saveState();}
  if (d.queuePlay !== undefined) {const [t] = state.queue.splice(Number(d.queuePlay),1);await play(t);renderQueue();saveState();}
  if (d.tab) {state.radioTab = d.tab;await navigate('radio');}
  if (d.radio !== undefined) await play(radioTrack(state.visibleStations[Number(d.radio)]));
  if (d.preset !== undefined) {
    const entry = state.visibleStations[Number(d.preset)];let saved = state.stations.find(s => s.url === entry.url);
    if (!saved?.preset && state.stations.filter(s => s.preset).length >= 8) {toast('Eight presets full. Remove a preset first.');return;}
    if (!saved) {saved = {...entry,preset:false};state.stations.push(saved);}
    saved.preset = !saved.preset;renderMain();saveState();
  }
  if (d.stationDelete !== undefined) {const url = state.visibleStations[Number(d.stationDelete)].url;state.stations = state.stations.filter(s => s.url !== url);renderMain();saveState();}
  if (d.playlistRemove !== undefined) {await api('playlists','POST',{id:state.playlist.id,remove:[Number(d.playlistRemove)]});await navigate('playlist');await refreshPlaylists();}
  switch (d.action) {
    case 'save-queue': playlistDialog('queue');break;
    case 'clear-queue': state.queue = [];renderQueue();renderPlayer();saveState();break;
    case 'resume': {
      const out = await library('getRandomSongs',{size:31});
      const unique = [...new Map((out.randomSongs?.song || []).map(t => [t.id,track(t)])).values()];
      if (!unique.length) {toast('No playable tracks returned by your library.');break;}
      if (state.mood) state.mood.followQueue = false;
      playList(unique);
      if (unique.length < 31) toast(`Started a random track; ${unique.length-1} tracks available for the queue.`);
      break;
    }
    case 'retry': await navigate(state.view,state.query);break;
    case 'more': await navigate('albums','',true);break;
    case 'play-list': playList([...state.tracks]);break;
    case 'new-playlist': playlistDialog('new');break;
    case 'rename-playlist': playlistDialog('rename');break;
    case 'delete-playlist': confirmDelete();break;
    case 'append-queue': {
      const ids = state.queue.filter(t => !t.url).map(t => t.id);
      if (!ids.length) {toast('Add some library tracks to your queue first.');break;}
      await api('playlists','POST',{id:state.playlist.id,songIds:ids});await navigate('playlist');await refreshPlaylists();toast('Queue added to your Navidrome playlist.');break;
    }
  }
  if (button.classList.contains('dialog-close')) closeModal();
  if (button.id === 'mobile-account') {closeModal();account();}
  if (button.id === 'configure-llm') {await openLLMSettings();}
  if (button.id === 'configure-connection') {closeModal();await openConnection();}
  if (button.id === 'logout') {await flushState();await api('logout','POST',{});signedOut();}
}
document.addEventListener('click',event => {
  const button = event.target.closest('button');if (!button || button.disabled || button.type === 'submit' && button.closest('form')) return;
  if (!Object.keys(button.dataset).length && !['logout','mobile-account','configure-connection','configure-llm'].includes(button.id) && !button.classList.contains('dialog-close')) return;
  button.disabled = true;action(button).catch(error => toast(error.message)).finally(() => {button.disabled = false;});
});
$('.brand').addEventListener('click',event => {event.preventDefault();navigate('home');});
$('#search').addEventListener('input',event => {clearTimeout(searchTimer);const query = event.target.value.trim();searchTimer = setTimeout(() => navigate(state.view,query),180);});
$('#play').onclick = toggle;$('#next').onclick = next;$('#previous').onclick = previous;
$('#stop').onclick = () => {stop();renderPlayer();};
$('#save-queue').onclick = () => playlistDialog('queue');
$('#clear-queue').onclick = () => {state.queue = [];renderQueue();renderPlayer();saveState();};
$('#shuffle').onclick = () => {for (let i=state.queue.length-1;i>0;i--) {const j=Math.floor(Math.random()*(i+1));[state.queue[i],state.queue[j]]=[state.queue[j],state.queue[i]];}renderQueue();saveState();};
$('#favorite').onclick = async () => {
  const t = state.current;if (!t || t.url) return;
  const star = !state.favorites.has(t.id);$('#favorite').disabled = true;
  try {await api('favorite','POST',{id:t.id,star});star ? state.favorites.add(t.id) : state.favorites.delete(t.id);renderPlayer();if (['favorites','home'].includes(state.view)) await navigate();}
  catch (error) {toast(error.message);renderPlayer();}
};
$('#build-mood').onclick = () => buildMood().catch(error => toast(error.message));
$('#account').onclick = account;$('#help').onclick = help;
document.addEventListener('submit',event => {
  if (event.target.id === 'directory-search') {event.preventDefault();state.directoryQuery = new FormData(event.target).get('q');navigate('radio');}
  if (event.target.id === 'station-form') {
    event.preventDefault();const data = new FormData(event.target);const url = new URL(data.get('url'));
    if (!['http:','https:'].includes(url.protocol) || url.username || url.password) {toast('Use an http or https URL without embedded credentials.');return;}
    if (state.stations.some(s => s.url === url.href)) {toast('This station is already saved.');return;}
    state.stations.push({name:data.get('name'),url:url.href,preset:false});state.radioTab = 'My stations';renderMain();saveState();
  }
});
document.addEventListener('keydown',event => {
  if (!state.user || event.ctrlKey || event.metaKey || event.altKey || $('#modal').open) return;
  if (event.key === 'Escape') {clearTimeout(searchTimer);$('#search').blur();navigate(state.view);return;}
  if (event.target.matches('input,textarea,select') || event.target.isContentEditable) return;
  if (event.key === '/') {event.preventDefault();$('#search').focus();}
  if (event.code === 'Space' && !event.target.closest('button,a')) {event.preventDefault();toggle();}
  if (event.key.toLowerCase() === 'm') buildMood().catch(error => toast(error.message));
  if (event.key === 'n') next();if (event.key === 'p') previous();if (event.key === '?') help();
  if (/^[1-6]$/.test(event.key)) navigate(['home','albums','favorites','playlists','radio','queue'][Number(event.key)-1]);
});
$('#modal').addEventListener('close',() => {if (!$('#modal').open) $('#modal-content').replaceChildren();});
document.addEventListener('error',event => {if (event.target.matches?.('img.album-art,img.mini-cover')) {event.target.src = '/weazlhead.png';event.target.classList.add('art-2');}},true);
bindAuth(signedOut);
