import {state} from './state.js';
import {uuid} from './device.js';
export async function flightAPI(path, method='GET', body, version, signal) {
  const headers={};
  if(method!=='GET'){headers['X-Weazl-Request']='1';headers['Content-Type']='application/json';headers['Idempotency-Key']=uuid();}
  if(version!==undefined)headers['If-Match']=`"v${version}"`;
  const response=await fetch('/api/v1/'+path,{method,headers,signal,body:body===undefined?undefined:JSON.stringify(body)});
  const value=response.status===204?null:await response.json();
  if(!response.ok){if(response.status===401)document.dispatchEvent(new Event('session-expired'));throw new Error(value?.error?.message||'Request failed.');}
  return value;
}
export async function listFlights(path){let rows=[],cursor='';do{const out=await flightAPI(path+(cursor?'?cursor='+encodeURIComponent(cursor):''));rows.push(...out.data);cursor=out.nextCursor;}while(cursor);return rows;}
export const flightIdentity=()=>[state.user,state.connection?.url||'',state.connection?.username||''].join(':');
