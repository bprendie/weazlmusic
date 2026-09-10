import {state, $, esc, toast} from './state.js';
import {api} from './api.js';
import {modal, closeModal} from './dialogs.js';
export async function openLLMSettings() {
  const c = await api('llm');
  closeModal();
  modal(`<span class="eyebrow purple">YOUR ACCOUNT / DJ-WEAZL</span><h2>Set the curator.</h2><p>Use your own Ollama or vLLM endpoint. The model picks from tracks in your connected Navidrome library.</p><form id="llm-form" class="auth-form"><label>Provider<select name="provider">${[['off','Off'],['ollama','Ollama'],['vllm','vLLM']].map(([id,name]) => `<option value="${id}" ${c.provider === id ? 'selected' : ''}>${name}</option>`).join('')}</select></label><label>Endpoint<input name="url" type="url" value="${esc(c.url)}" placeholder="http://ollama:11434" maxlength="2048"></label><label>API key (optional)<input name="apiKey" type="password" autocomplete="new-password" placeholder="${c.hasKey ? 'Saved key — leave blank to keep' : 'Only if your endpoint requires one'}"></label>${c.hasKey ? '<label class="check-label"><input type="checkbox" name="clearKey"> Remove saved API key</label>' : ''}<button type="button" id="load-models" class="secondary">Load models</button><label>Model<input name="model" value="${esc(c.model)}" list="llm-models" autocomplete="off" maxlength="200"><datalist id="llm-models"></datalist></label><button class="primary">Save curator settings</button><p id="llm-error" role="alert"></p></form>`);
  const settings = () => {
    const form = new FormData($('#llm-form'));
    const data = {provider:form.get('provider'),url:form.get('url'),model:form.get('model')};
    if (form.get('apiKey')) data.apiKey = form.get('apiKey');
    if (form.get('clearKey')) data.apiKey = '';
    return data;
  };
  $('#load-models').onclick = async event => {
    event.target.disabled = true;$('#llm-error').textContent = '';
    try {
      const result = await api('llm/models','POST',settings());
      if (!$('#llm-models')) return;
      $('#llm-models').innerHTML = result.models.map(m => `<option value="${esc(m)}"></option>`).join('');
      if (result.models.length && !$('#llm-form [name=model]').value) $('#llm-form [name=model]').value = result.models[0];
      $('#llm-error').textContent = `${result.models.length} models available.`;
    } catch (error) {if ($('#llm-error')) $('#llm-error').textContent = error.message;}
    finally {event.target.disabled = false;}
  };
  $('#llm-form').onsubmit = async event => {
    event.preventDefault();const button = event.target.querySelector('button.primary');button.disabled = true;
    try {await api('llm','PUT',settings());closeModal();toast('Curator settings saved for your account.');}
    catch (error) {if ($('#llm-error')) $('#llm-error').textContent = error.message;}
    finally {button.disabled = false;}
  };
}
