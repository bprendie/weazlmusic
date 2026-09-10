import {state, toast} from './state.js';
export async function api(path, method = 'GET', data, signal) {
  const response = await fetch('/api/' + path, {
    method, signal, headers: method === 'GET' ? {} : {'Content-Type':'application/json', 'X-Weazl-Request':'1'},
    body: data === undefined ? undefined : JSON.stringify(data),
  });
  const body = await response.json();
  if (!response.ok) {
    if (response.status === 401 && path !== 'login') document.dispatchEvent(new Event('session-expired'));
    throw new Error(body.error || `Request failed (${response.status})`);
  }
  return body;
}
export const library = (method, params = {}, signal) => api(`library/${method}?${new URLSearchParams(params)}`, 'GET', undefined, signal);
let saveTimer, saveChain = Promise.resolve(), generation = 0;
export function cancelSave() { clearTimeout(saveTimer); generation++; }
export function flushState() {
  clearTimeout(saveTimer);
  const gen = generation;
  if (!state.user) return Promise.resolve();
  const snapshot = structuredClone({stations: state.stations, queue: state.queue, current: state.current?.url ? state.resume : state.current});
  const write = saveChain.then(() => {
    if (gen !== generation) return;
    return api('state', 'PUT', snapshot);
  });
  saveChain = write.catch(() => {});
  return write;
}
export function saveState() {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => flushState().catch(error => toast('Could not save your queue/settings: ' + error.message)), 350);
}
