import {state, $, esc, toast} from './state.js';
import {api} from './api.js';
import {modal, closeModal} from './dialogs.js';

export async function openAdminSettings() {
  const cfg = await api('admin/config');
  const llm = cfg.llm || {provider:'off',url:'',model:'',hasKey:false};
  modal(`<span class="eyebrow purple">ADMIN / INSTALLATION</span><h2>Set the node.</h2><p>Configure the shared Navidrome backend and curator. Users will use their own Navidrome credentials when the identity phase is enabled.</p><form id="admin-form" class="auth-form"><label>Navidrome server<input name="navidromeURL" type="url" required value="${esc(cfg.navidromeURL)}" placeholder="https://music.example.com"></label><label>Curator provider<select name="provider"><option value="off" ${llm.provider === 'off' ? 'selected' : ''}>Off</option><option value="ollama" ${llm.provider === 'ollama' ? 'selected' : ''}>Ollama</option><option value="vllm" ${llm.provider === 'vllm' ? 'selected' : ''}>vLLM</option></select></label><label>Curator endpoint<input name="llmURL" type="url" value="${esc(llm.url)}" placeholder="http://ollama:11434"></label><label>Model<input name="model" value="${esc(llm.model)}" maxlength="200" placeholder="llama3.2"></label><label>API key (optional)<input name="apiKey" type="password" autocomplete="new-password" placeholder="${llm.hasKey ? 'Saved key — leave blank to keep' : 'Only if required'}"></label><button class="primary">Save installation settings</button><p id="admin-error" role="alert"></p></form>`);
  $('#admin-form').onsubmit = async event => {
    event.preventDefault();const button = event.target.querySelector('button');button.disabled = true;
    try {
      const data = new FormData(event.target);
      await api('admin/config','PUT',{navidromeURL:data.get('navidromeURL'),provider:data.get('provider'),llmURL:data.get('llmURL'),model:data.get('model'),apiKey:data.get('apiKey') || null});
      closeModal();toast('Installation settings saved.');
    } catch (error) {$('#admin-error').textContent = error.message;button.disabled = false;}
  };
}
