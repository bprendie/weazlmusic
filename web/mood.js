import {state, $, track, toast} from './state.js';
import {api, saveState} from './api.js';
import {renderMain, renderPlayer, renderQueue} from './render.js';
import {cancelNavigation, refreshPlaylists} from './navigation.js';
import {openLLMSettings} from './llm-settings.js';
let active = null;
function progress() {
  const m = state.mood;
  $('#mood-progress').textContent = m ? `Mood ${m.count}/20${m.running ? ' · curating' : ''}` : '';
  $('#cancel-mood').hidden = !m?.running;
  renderPlayer();
}
export function cancelMood(clear = false) {
  active?.abort();active = null;
  if (state.mood) state.mood.running = false;
  if (clear) state.mood = null;
  progress();
}
function receive(event, m) {
  if (state.mood !== m) return;
  if (event.error) throw new Error(event.error);
  if (event.type === 'started') {
    m.playlist = event.playlist;m.count = 1;m.tracks = [track(event.track)];
    cancelNavigation();state.loading = false;state.error = '';state.query = '';$('#search').value = '';
    state.view = 'playlist';state.playlist = {...event.playlist};state.tracks = [...m.tracks];
    state.playlists = [...state.playlists.filter(p => p.id !== event.playlist.id),event.playlist];
    renderMain();
  }
  if (event.type === 'track') {
    const t = track(event.track);m.tracks.push(t);m.count = event.count;
    if (m.followQueue && state.current?.id !== t.id && !state.queue.some(row => row.id === t.id)) {
      const moodIDs = new Set(m.tracks.map(row => row.id));
      const firstOther = state.queue.findIndex(row => !moodIDs.has(row.id));
      state.queue.splice(firstOther < 0 ? state.queue.length : firstOther,0,t);
      renderQueue();saveState();
    }
    if (state.view === 'playlist' && state.playlist?.id === m.playlist?.id) {
      if (!state.tracks.some(row => row.id === t.id)) state.tracks.push(t);
      state.playlist.songCount = m.count;renderMain();
    }
  }
  if (event.type === 'done') {
    m.done = true;
    if (m.followQueue) {const moodIDs = new Set(m.tracks.map(t => t.id));state.queue = state.queue.filter(t => !m.safety.has(t.id) || moodIDs.has(t.id));renderQueue();saveState();}
  }
  progress();
}
export async function buildMood() {
  if (state.mood?.running) return;
  const seed = state.current;
  if (!seed || seed.url) {toast('Play a library track to seed Mood.');return;}
  const settings = await api('llm');
  if (settings.provider === 'off') {await openLLMSettings();toast('Choose an Ollama or vLLM model, then build Mood.');return;}
  const controller = new AbortController();active = controller;
  const m = {running:true,count:0,tracks:[],safety:new Set(state.queue.map(t => t.id)),followQueue:true,done:false};state.mood = m;progress();
  try {
    const response = await fetch('/api/mood',{method:'POST',headers:{'Content-Type':'application/json','X-Weazl-Request':'1'},body:JSON.stringify({seedId:seed.id}),signal:controller.signal});
    if (!response.ok) {const error = await response.json();if (response.status === 401) document.dispatchEvent(new Event('session-expired'));throw new Error(error.error || 'Mood could not start.');}
    const reader = response.body.getReader();const decoder = new TextDecoder();let pending = '';
    while (true) {
      const {value,done} = await reader.read();pending += decoder.decode(value,{stream:!done});
      let newline;
      while ((newline = pending.indexOf('\n')) !== -1) {const line = pending.slice(0,newline);pending = pending.slice(newline+1);if (line.trim()) receive(JSON.parse(line),m);}
      if (done) break;
    }
    if (!m.done) throw new Error(`Mood stopped at ${m.count}/20. Accepted tracks remain in Navidrome.`);
    toast('Mood is ready: 20 tracks in your Navidrome playlist.');
  } catch (error) {if (error.name !== 'AbortError' && state.mood === m) toast(error.message);}
  finally {
    controller.abort();if (active === controller) active = null;
    m.running = false;if (state.mood === m) {progress();if (state.user) await refreshPlaylists().catch(() => {});}
  }
}
$('#cancel-mood').onclick = () => {cancelMood();toast('Mood stopped. Tracks already added remain in the playlist.');};
