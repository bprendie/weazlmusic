// Real AAC + real browser decoder; API/latency are isolated local fixtures.
const assert=require('node:assert/strict'),fs=require('node:fs'),http=require('node:http'),path=require('node:path');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(__dirname,'..'),sample=fs.readFileSync(path.join(root,'fixtures/v1/media/aac-lc-160k-stereo.m4a'));
const step=5016,segments=Array.from({length:240},(_,i)=>({assetId:'s'+i,startMs:i*step,durationMs:step,mediaStartMs:0,byteLength:sample.length}));
const tracks=[{stationId:'a',name:'Buffered fixture',segments},{stationId:'b',name:'Second station',segments:segments.map((s,i)=>({...s,startMs:s.startMs+(i>=2?2000:0)}))}];
let leases=[],fail=false,delay=0;
const server=http.createServer(async(req,res)=>{
 const url=new URL(req.url,'http://localhost');
 if(url.pathname.endsWith('/manifest'))return json(res,{data:{sessionId:'buffer-fixture',revision:'1',name:'Buffer fixture',durationMs:step*240,tracks}});
 if(url.pathname==='/api/v1/media/leases'){
  let body='';for await(const c of req)body+=c;const id=JSON.parse(body).resourceId;leases.push(id);await new Promise(r=>setTimeout(r,delay));
  if(fail){res.statusCode=503;return json(res,{error:{message:'Injected network outage'}});}return json(res,{data:{url:'/media/'+id}});
 }
 if(url.pathname.startsWith('/media/')){res.setHeader('Content-Type','audio/mp4');res.setHeader('Accept-Ranges','bytes');const m=/bytes=(\d+)-(\d*)/.exec(req.headers.range||'');let bytes=sample;if(m){const start=+m[1],end=m[2]?Math.min(+m[2],sample.length-1):sample.length-1;res.statusCode=206;res.setHeader('Content-Range',`bytes ${start}-${end}/${sample.length}`);bytes=sample.subarray(start,end+1);}res.setHeader('Content-Length',bytes.length);return res.end(bytes);}
 const name=url.pathname==='/'?'index.html':path.basename(url.pathname);
 try{let bytes=fs.readFileSync(path.join(root,'web',name));if(name==='index.html')bytes=Buffer.from(bytes.toString().replace('<script type="module" src="app.js"></script>',''));res.setHeader('Content-Type',name.endsWith('.js')?'text/javascript':name.endsWith('.html')?'text/html':'application/octet-stream');res.end(bytes);}catch{res.statusCode=404;res.end();}
});
function json(res,value){res.setHeader('Content-Type','application/json');res.end(JSON.stringify(value));}
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await chromium.launch({headless:true,executablePath:process.env.BROWSER_BIN||'/usr/bin/chromium',args:['--no-sandbox','--autoplay-policy=no-user-gesture-required']});
 try{
 const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto('http://127.0.0.1:'+server.address().port);
 await page.evaluate(async()=>{document.querySelector('.app').hidden=false;document.querySelector('#login-screen').hidden=true;document.querySelector('#content').innerHTML='<div id="flight-playback"></div>';window.flight=await import('/flight-playback.js');window.audio=(await import('/player.js')).audio;window.state=(await import('/state.js')).state;window.events=[];for(const event of ['emptied','seeking','waiting','pause','ended'])audio.addEventListener(event,()=>events.push(event));await flight.openFlightPlayback('buffer-fixture');});
 await page.waitForFunction(()=>state.flightPlayback.buffer.ahead(0)>=299000);
 assert.equal(await page.locator('#flight-buffer').inputValue(),'5');assert.equal(leases.length,60,'five minutes fetched at startup');
 await page.evaluate(()=>{audio.currentTime=55;});await page.waitForTimeout(650);assert.equal(leases.length,60,'do not refill while more than four minutes remain');
 await page.evaluate(()=>{audio.currentTime=65;});await page.waitForFunction(()=>state.flightPlayback.buffer.ahead(audio.currentTime*1000)>=299000);assert.ok(leases.length>=72&&leases.length<=74,'refill to five minutes after crossing four-minute low water');
 // A network outage cannot interrupt already buffered segment transitions.
 fail=true;await page.evaluate(async()=>{audio.currentTime=68.9;await flight.toggleFlight();events.length=0;});await page.waitForTimeout(3200);
 assert.ok(await page.evaluate(()=>audio.currentTime)>71.5);assert.deepEqual(await page.evaluate(()=>events),[],'segment boundary must not reload, seek, pause or wait');
 await page.evaluate(()=>flight.toggleFlight());const paused=await page.evaluate(()=>state.flightPlayback.position());await page.waitForTimeout(400);assert.ok(Math.abs(await page.evaluate(()=>state.flightPlayback.position())-paused)<20,'pause freezes the audio clock');
 fail=false;
 // Smaller preference is persisted and an uncached seek rebuilds a bounded new buffer.
 await page.locator('#flight-buffer').selectOption('1');await page.evaluate(()=>flight.seekFlight(400000));await page.waitForFunction(()=>state.flightPlayback.buffer.ahead(400000)>59000);
 assert.equal(await page.evaluate(()=>localStorage.getItem('flight-buffer-minutes')),'1');assert.ok(await page.evaluate(()=>audio.buffered.end(audio.buffered.length-1))<466);
 // Exhaust a buffer under network failure: time must stop, then resume without skipping.
 fail=true;await page.evaluate(async()=>{audio.currentTime=audio.buffered.end(audio.buffered.length-1)-0.6;await flight.toggleFlight();});await page.waitForTimeout(2000);
 const stalled=await page.evaluate(()=>state.flightPlayback.position());await page.waitForTimeout(1300);assert.ok(Math.abs(await page.evaluate(()=>state.flightPlayback.position())-stalled)<80,'network stall cannot advance the session clock');
 fail=false;await page.waitForFunction(previous=>state.flightPlayback.position()>previous+500,stalled,{timeout:9000});assert.ok(await page.evaluate(()=>state.flightPlayback.position())<stalled+5000,'resume does not jump over network downtime');
 // Seek and station switch preserve position; pending downloads are aborted on stop.
 await page.evaluate(()=>flight.toggleFlight());await page.evaluate(()=>flight.seekFlight(400000));assert.ok(Math.abs(await page.evaluate(()=>state.flightPlayback.position())-400000)<50);
 await page.locator('[data-flight-station="b"]').click();assert.ok(Math.abs(await page.evaluate(()=>state.flightPlayback.position())-400000)<100);assert.equal(await page.locator('#now-title').innerText(),'Second station');
 // Real capture gaps keep their original timeline and resume at the next segment.
 await page.evaluate(()=>flight.seekFlight(9400));await page.evaluate(()=>flight.toggleFlight());await page.waitForFunction(()=>state.flightPlayback.gap===true,{},{timeout:3000});await page.waitForFunction(()=>state.flightPlayback.gap===false&&state.flightPlayback.position()>12032,{},{timeout:5000});
 delay=400;await page.evaluate(()=>{flight.seekFlight(600000);});await page.waitForTimeout(50);await page.evaluate(()=>flight.stopFlightPlayback());await page.waitForTimeout(600);assert.equal(await page.evaluate(()=>state.flightPlayback),null);assert.equal(await page.evaluate(()=>audio.getAttribute('src')),null);
 // Compatibility path still prefetches larger buffers without requiring MSE.
 delay=0;const legacy=await browser.newPage();await legacy.addInitScript(()=>{window.MediaSource=undefined;window.ManagedMediaSource=undefined;});await legacy.goto('http://127.0.0.1:'+server.address().port);
 await legacy.evaluate(async()=>{window.flight=await import('/flight-playback.js');window.state=(await import('/state.js')).state;window.audio=(await import('/player.js')).audio;await flight.openFlightPlayback('buffer-fixture');});await legacy.waitForFunction(()=>state.flightPlayback.buffer.cache.size>=60);
 await legacy.evaluate(async()=>{await flight.seekFlight(4300);await flight.toggleFlight();});await legacy.waitForTimeout(2000);assert.ok(await legacy.evaluate(()=>state.flightPlayback.position())>5700);await legacy.close();
 assert.deepEqual(errors,[]);console.log('PASS: 5-minute buffer, 4-minute refill threshold, continuous AAC across boundaries during outage, bounded memory window, frozen clock on starvation, resume, pause, seek, station switch, cancellation.');
 }finally{await browser.close();server.closeAllConnections();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);server.closeAllConnections();server.close();process.exitCode=1;});
