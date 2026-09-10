export const state = {
  user: '', connection: null, view: 'home', query: '', albums: [], playlists: [], tracks: [],
  stations: [], directory: [], radioTab: 'Presets', queue: [], current: null,
  mood: null, radioMetadata: null, favorites: new Set(), playing: false, album: null, playlist: null,
  offset: 0, more: false, loading: false, error: '', resume: null,
};
export const $ = selector => document.querySelector(selector);
export const esc = value => String(value ?? '').replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
export const duration = value => Number.isFinite(value) ? `${Math.floor(value / 60)}:${String(Math.floor(value % 60)).padStart(2, '0')}` : 'LIVE';
export function track(raw) {
  return {id: String(raw.id), title: raw.title || raw.name || 'Untitled', artist: raw.artist || 'Unknown artist',
    album: raw.album || '', coverArt: raw.coverArt || '', duration: Number(raw.duration) || 0, starred: !!raw.starred};
}
export const radioTrack = station => ({id: station.url, title: station.name, artist: 'Live radio', url: station.url, duration: null});
let notice;
export function toast(message) {
  clearTimeout(notice); $('#toast').textContent = message; $('#toast').hidden = false;
  notice = setTimeout(() => { $('#toast').hidden = true; }, 4500);
}
