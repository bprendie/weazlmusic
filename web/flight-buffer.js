import {flightAPI} from './flight-api.js';

const MIME='audio/mp4; codecs="mp4a.40.2"';
export const bufferChoices=[1,3,5,10];
export function bufferMinutes(){try{const n=Number(localStorage.getItem('flight-buffer-minutes'));return bufferChoices.includes(n)?n:5;}catch{return 5;}}
export function saveBufferMinutes(n){try{localStorage.setItem('flight-buffer-minutes',String(n));}catch{}}
function waitEvent(target,event,signal,action,errorTarget=target){
 return new Promise((resolve,reject)=>{
  let timeout;const clean=()=>{clearTimeout(timeout);target.removeEventListener(event,ok);errorTarget.removeEventListener('error',bad);signal.removeEventListener('abort',abort);};
  const ok=()=>{clean();resolve();},bad=()=>{clean();reject(new Error('Recording media could not be loaded.'));},abort=()=>{clean();reject(new DOMException('Cancelled','AbortError'));};
  if(signal.aborted)return abort();target.addEventListener(event,ok,{once:true});errorTarget.addEventListener('error',bad,{once:true});signal.addEventListener('abort',abort,{once:true});
  timeout=setTimeout(bad,30000);try{action?.();}catch(e){clean();reject(e);}
 });
}
async function fragment(data){
 const {createFile}=await import('./mp4box.js');const file=createFile();let init,codec,error;const chunks=[];
 file.onError=e=>{error=new Error(String(e));};
 file.onReady=info=>{
  const track=info.audioTracks[0];if(!track||info.tracks.length!==1){error=new Error('Recording must contain one audio track.');return;}
  codec=track.codec;file.setSegmentOptions(track.id,null,{nbSamples:100000});init=file.initializeSegmentation().buffer;file.start();
 };
 file.onSegment=(_id,_user,buffer)=>chunks.push(buffer);
 data.fileStart=0;file.appendBuffer(data);file.flush();
 if(error)throw error;if(!init||!chunks.length||codec!=='mp4a.40.2')throw new Error('Invalid AAC recording segment.');
 return {init,chunks};
}

// Keep compressed AAC ahead of the playhead. MSE retains one decoder across files.
export class FlightBuffer {
 constructor(audio,track,duration,minutes,onError){
  this.audio=audio;this.track=track;this.duration=duration;this.minutes=minutes;this.onError=onError;
  this.abort=new AbortController();this.cache=new Map();this.appended=new Set();this.urls=[];this.target=0;this.closed=false;
  this.Source=[globalThis.MediaSource,globalThis.ManagedMediaSource].find(source=>source?.isTypeSupported?.(MIME));
  this.continuous=!!this.Source;
 }
 async open(position){
  this.target=position;const signal=this.abort.signal;
  if(this.continuous){
   this.media=new this.Source();const url=URL.createObjectURL(this.media);this.urls.push(url);this.remotePlayback=this.audio.disableRemotePlayback;this.audio.disableRemotePlayback=true;
   await waitEvent(this.media,'sourceopen',signal,()=>{this.audio.src=url;this.audio.load();},this.audio);
   this.sb=this.media.addSourceBuffer(MIME);this.media.duration=this.duration/1000;
  }
  const seg=this.track.segments.find(s=>s.startMs+s.durationMs>position);
  if(seg){await this.add(seg);await this.activate(seg,Math.max(position,seg.startMs));}
  this.opened=true;this.fill(position);return seg;
 }
 async bytes(seg){
  const signal=this.abort.signal;
  const lease=(await flightAPI('media/leases','POST',{kind:'recordingSegment',resourceId:seg.assetId},undefined,signal)).data;
  const response=await fetch(lease.url,{signal});if(!response.ok)throw new Error('Recording download failed.');
  const data=await response.arrayBuffer();if(data.byteLength!==seg.byteLength)throw new Error('Recording download was incomplete.');return data;
 }
 async add(seg){
  if(this.closed)throw new DOMException('Cancelled','AbortError');
  if(this.has(seg))return;
  const data=await this.bytes(seg);if(this.closed)return;if(this.failed){this.failed=false;this.onError(null);}
  if(!this.continuous){const url=URL.createObjectURL(new Blob([data],{type:'audio/mp4'}));this.cache.set(seg.assetId,url);return;}
  const {init,chunks}=await fragment(data);if(this.closed)return;
  if(!this.initialized){await this.append(init);this.initialized=true;}
  this.sb.timestampOffset=(seg.startMs-seg.mediaStartMs)/1000;
  for(const chunk of chunks)await this.append(chunk);
  this.appended.add(seg.assetId);this.media.duration=this.duration/1000;
 }
 append(data){return waitEvent(this.sb,'updateend',this.abort.signal,()=>this.sb.appendBuffer(data));}
 async activate(seg,position){
  if(!this.continuous){
   const url=this.cache.get(seg.assetId);if(!url)throw new Error('Recording is not buffered yet.');
   await waitEvent(this.audio,'loadedmetadata',this.abort.signal,()=>{this.audio.src=url;this.audio.load();});
  }
  this.active=seg;this.audio.currentTime=this.continuous?position/1000:(seg.mediaStartMs+position-seg.startMs)/1000;
 }
 canSeek(position){
  const seg=this.track.segments.find(s=>position>=s.startMs&&position<s.startMs+s.durationMs);if(!seg)return false;
  if(!this.continuous)return this.cache.has(seg.assetId);
  const ranges=this.audio.buffered;for(let i=0;i<ranges.length;i++)if(ranges.start(i)*1000<=position&&ranges.end(i)*1000>position)return true;return false;
 }
 position(){return this.continuous?this.audio.currentTime*1000:(this.active?.startMs||0)+this.audio.currentTime*1000-(this.active?.mediaStartMs||0);}
 has(seg){
  if(!this.continuous)return this.cache.has(seg.assetId);
  if(!this.appended.has(seg.assetId)||!this.sb)return false;
  const start=Math.max(seg.startMs,this.target),end=Math.min(seg.startMs+seg.durationMs,this.duration),ranges=this.sb.buffered;
  for(let i=0;i<ranges.length;i++)if(ranges.start(i)*1000<=start+2&&ranges.end(i)*1000>=end-2)return true;return false;
 }
 fill(position){
  this.target=position;if(this.busy||this.closed||!this.opened)return this.busy;
  if(Date.now()<(this.retryAt||0)||this.ahead(position)>=this.minutes*60000*0.8)return;
  this.fillUntil=position+this.minutes*60000;
  this.busy=this.pump().catch(e=>{if(!this.closed&&e.name!=='AbortError'){this.failed=true;this.retryAt=Date.now()+3000;this.onError(e);}}).finally(()=>{this.busy=null;});return this.busy;
 }
 async pump(){
  for(;;){
   if(this.closed)return;
   const floor=Math.max(0,this.target-60000),ceiling=this.fillUntil;
   if(this.continuous&&this.sb.buffered.length&&this.sb.buffered.start(0)<floor/1000-30){
    await waitEvent(this.sb,'updateend',this.abort.signal,()=>this.sb.remove(0,floor/1000));
   }
   for(const [id,url] of this.cache){const s=this.track.segments.find(s=>s.assetId===id);if(s.startMs+s.durationMs<floor||s.startMs>ceiling){URL.revokeObjectURL(url);this.cache.delete(id);}}
   const next=this.track.segments.find(s=>s.startMs+s.durationMs>this.target&&s.startMs<ceiling&&!this.has(s));
   if(!next)return;await this.add(next);
  }
 }
 ahead(position){
  if(this.continuous){const ranges=this.audio.buffered;for(let i=0;i<ranges.length;i++)if(ranges.start(i)*1000<=position+50&&ranges.end(i)*1000>=position)return Math.max(0,ranges.end(i)*1000-position);return 0;}
  let end=position;for(const s of this.track.segments)if(this.has(s)&&s.startMs<=end+2&&s.startMs+s.durationMs>end)end=s.startMs+s.durationMs;return end-position;
 }
 close(){if(this.remotePlayback!==undefined)this.audio.disableRemotePlayback=this.remotePlayback;this.closed=true;this.abort.abort();for(const url of [...this.urls,...this.cache.values()])URL.revokeObjectURL(url);this.cache.clear();}
}
