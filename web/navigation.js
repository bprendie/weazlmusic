import {state, $, esc, track} from './state.js';
import {api, library} from './api.js';
import {renderMain, renderPlayer} from './render.js';
let requestID = 0, controller;
export function cancelNavigation() { requestID++; controller?.abort(); }
export async function refreshPlaylists() {
  const data = await library('getPlaylists');
  state.playlists = data.playlists?.playlist || [];

}
export async function navigate(view = state.view, query = '', more = false) {
  controller?.abort(); controller = new AbortController();
  const id = ++requestID, signal = controller.signal;
  state.view = view; state.query = query; state.loading = true; state.error = '';
  if (!query) $('#search').value = '';
  renderMain();
  try {
    let apply = () => {};
    if (query) {
      const data = await library('search3',{query,songCount:100,albumCount:0,artistCount:0},signal);
      apply = () => {state.tracks = (data.searchResult3?.song || []).map(track);};
    } else if (view === 'home' || view === 'albums') {
      const offset = more ? state.offset + 40 : 0;
      const [data, stars] = await Promise.all([
        library('getAlbumList2',{type:'newest',size:view === 'home' ? 16 : 40,offset},signal),
        view === 'home' ? library('getStarred2',{},signal) : Promise.resolve(null),
      ]);
      apply = () => {
        const rows = data.albumList2?.album || [];
        state.albums = more ? [...state.albums,...rows] : rows;
        state.more = rows.length === 40; state.offset = offset;
        if (stars) {state.tracks = (stars.starred2?.song || []).slice(0,6).map(track);state.favorites = new Set((stars.starred2?.song || []).map(t => t.id));}
      };
    } else if (view === 'album') {
      const data = await library('getAlbum',{id:state.album.id},signal);
      apply = () => {state.album = data.album;state.tracks = (data.album?.song || []).map(track);};
    } else if (view === 'favorites') {
      const data = await library('getStarred2',{},signal);
      apply = () => {state.tracks = (data.starred2?.song || []).map(track);state.favorites = new Set(state.tracks.map(t => t.id));};
    } else if (view === 'playlists') {
      await refreshPlaylists();
    } else if (view === 'playlist') {
      const data = await library('getPlaylist',{id:state.playlist.id},signal);
      apply = () => {state.playlist = data.playlist;state.tracks = (data.playlist?.entry || []).map(track);};
    } else if (view === 'radio' && ['SomaFM','Icecast'].includes(state.radioTab)) {
      const data = await api(`radio/directory?${new URLSearchParams({source:state.radioTab.toLowerCase(),q:state.directoryQuery || ''})}`,'GET',undefined,signal);
      apply = () => {state.directory = data;};
    }
    if (id !== requestID) return;
    apply();
  } catch (error) { if (id !== requestID || error.name === 'AbortError') return; state.error = error.message; }
  if (id !== requestID) return;
  state.loading = false; renderMain(); renderPlayer();
}
