import {state, $, esc, toast} from './state.js';
import {api, flushState, cancelSave} from './api.js';
import {renderPlayer, renderQueue} from './render.js';
import {navigate, refreshPlaylists, cancelNavigation} from './navigation.js';
import {resetPlayer} from './player.js';
import {cancelMood} from './mood.js';
import {modal, closeModal} from './dialogs.js';
import {openAdminSettings} from './admin-settings.js';
let signOut;
async function loadAccount(user) {
  state.user = user.username;state.admin = !!user.admin;state.connection = user.connection;
  $('#username').innerHTML = `${esc(state.user)}<small>Web-app account</small>`;
  $('.avatar').textContent = state.user[0].toUpperCase();
  const saved = await api('state');
  state.stations = saved.stations || [];state.queue = saved.queue || [];state.current = saved.current || null;
  $('#login-screen').hidden = true;$('.app').hidden = false;
  renderQueue();renderPlayer();
  if (!user.connection) {
    if (state.admin) {
      const config = await api('admin/config');
      if (!config.navidromeURL) {state.view = 'home';await openAdminSettings();}
      else {state.view = 'radio';state.radioTab = 'Presets';await navigate('radio');}
    }
    else {state.view = 'radio';state.radioTab = 'Presets';await navigate('radio');connectionForm(false);}
  } else {
    $('#connection-status').textContent = `NAVIDROME / ${user.connection.username}`;
    await Promise.all([navigate('home'),refreshPlaylists().catch(error => toast(error.message))]);
  }
}
export async function openConnection() {
  if (state.user) await flushState();
  connectionForm(!!state.connection);
}
function connectionForm(existing) {
  modal(`<img class="auth-brand" src="weazlhead.png" alt="Weazl"><span class="eyebrow purple">${existing ? 'YOUR MUSIC CONNECTION' : 'STEP 2 / CONNECT YOUR MUSIC'}</span><h2>Where are your tunes?</h2><p>Your web-app account is <strong>${esc(state.user)}</strong>. Choose your Navidrome server, then enter the credentials for that server.</p><form id="connection-form" class="auth-form"><label>Navidrome server<input type="url" name="url" required placeholder="https://music.example.com" value="${esc(state.connection?.url || '')}"></label><label>Navidrome username<input name="username" required autocomplete="off" value="${esc(state.connection?.username || '')}"></label><label>Navidrome password<input name="password" type="password" required autocomplete="off"></label><button class="primary">Test & save connection →</button><p id="connection-error" role="alert"></p></form><p>Your playlists will belong to this Navidrome user. You can change the connection from your account menu.</p>`);
  $('#connection-form').onsubmit = async event => {
    event.preventDefault();const button = event.target.querySelector('button');button.disabled = true;
    const form = new FormData(event.target);
    try {
      await flushState();cancelSave();cancelNavigation();
      const identity = await api('connection','PUT',{url:form.get('url'),username:form.get('username'),password:form.get('password')});
      event.target.reset();closeModal();cancelMood(true);resetPlayer();
      state.tracks = [];state.albums = [];state.playlists = [];state.favorites.clear();
      
      await loadAccount(identity);toast('Navidrome connected. Your web-app login stays separate.');
    } catch (error) {if ($('#connection-error')) $('#connection-error').textContent = error.message;else toast(error.message);}
    finally {button.disabled = false;}
  };
}
export function bindAuth(onSignOut) {
  signOut = onSignOut;
  document.addEventListener('session-expired',signOut);
  $('#login-form').onsubmit = async event => {
    event.preventDefault();const button = event.target.querySelector('button');button.disabled = true;$('#login-error').textContent = '';
    const data = new FormData(event.target);
    try {
      const user = await api('login','POST',{username:data.get('username'),password:data.get('password')});
      event.target.reset();await loadAccount(user);
    } catch (error) {$('#login-error').textContent = error.message;}
    finally {button.disabled = false;}
  };
  api('me').then(loadAccount).catch(error => {signOut();if (!error.message.includes('Sign in') && !error.message.includes('Session expired')) $('#login-error').textContent = error.message;});
}
