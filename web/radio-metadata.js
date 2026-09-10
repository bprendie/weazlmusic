import {state} from './state.js';
import {renderPlayer} from './render.js';
let source = null;
export function closeRadioMetadata() {
  source?.close();source = null;state.radioMetadata = null;
}
export function watchRadioMetadata(station) {
  closeRadioMetadata();
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  const playback = Array.from(bytes,b => b.toString(16).padStart(2,'0')).join('');
  const events = new EventSource('/api/radio/events?' + new URLSearchParams({playback}));
  source = events;
  events.onmessage = event => {
    if (source !== events || state.current !== station) return;
    let meta;try {meta = JSON.parse(event.data);} catch {return;}
    if (meta.ended) {events.close();return;}
    state.radioMetadata = {title:meta.title || '',artist:meta.artist || ''};
    renderPlayer();
  };
  // EventSource may reconnect while playing; stop/pause closes it explicitly.
  return playback;
}
