// Generate the ignored ten-minute fixture with TestRecorderTenMinuteCompactionAtomicRetryAndPackets first.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=path.resolve(__dirname,'..'),dir=process.env.RECORDER_COMPACT_ARTIFACT_DIR||path.join(root,'test-results/ten-minute');
const manifest=JSON.parse(fs.readFileSync(path.join(dir,'manifest.json'),'utf8'));manifest.tracks.push({...manifest.tracks[0],stationId:'second',name:'Second station'});
const leases=[];
const server=http.createServer(async(req,res)=>{
 const url=new URL(req.url,'http://localhost');
 if(url.pathname.endsWith('/manifest')){res.setHeader('Content-Type','application/json');return res.end(JSON.stringify({data:manifest}));}
 if(url.pathname==='/api/v1/media/leases'){
  let body='';for await(const c of req)body+=c;const id=JSON.parse(body).resourceId;leases.push(id);await new Promise(r=>setTimeout(r,150));res.setHeader('Content-Type','application/json');return res.end(JSON.stringify({data:{url:'/media/'+id+'.m4a'}}));
 }
 let name=url.pathname==='/'?'index.html':path.basename(url.pathname);
 try{
  let bytes=fs.readFileSync(path.join(url.pathname.startsWith('/media/')?dir:path.join(root,'web'),name));
  if(name==='index.html')bytes=Buffer.from(bytes.toString().replace('<script type="module" src="app.js"></script>',''));
  res.setHeader('Content-Security-Policy',"default-src 'self'; script-src 'self'; media-src 'self' blob:; img-src 'self' data:; style-src 'self' 'unsafe-inline'");
  res.setHeader('Content-Type',name.endsWith('.js')?'text/javascript':name.endsWith('.m4a')?'audio/mp4':name.endsWith('.html')?'text/html':name.endsWith('.css')?'text/css':name.endsWith('.png')?'image/png':'application/octet-stream');
  if(name.endsWith('.m4a')){res.setHeader('Accept-Ranges','bytes');const m=/bytes=(\d+)-(\d*)/.exec(req.headers.range||'');if(m){const start=+m[1],end=m[2]?Math.min(+m[2],bytes.length-1):bytes.length-1;res.statusCode=206;res.setHeader('Content-Range',`bytes ${start}-${end}/${bytes.length}`);bytes=bytes.subarray(start,end+1);}}
  res.setHeader('Content-Length',bytes.length);res.end(bytes);
 }catch{res.statusCode=404;res.end();}
});
(async()=>{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const browser=await chromium.launch({headless:true,executablePath:process.env.BROWSER_BIN||'/usr/bin/chromium',args:['--no-sandbox','--autoplay-policy=no-user-gesture-required']});
 try{
 const page=await browser.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto('http://127.0.0.1:'+server.address().port);
 await page.evaluate(async()=>{document.querySelector('.app').hidden=false;document.querySelector('#login-screen').hidden=true;document.querySelector('#content').innerHTML='<div id="flight-playback"></div>';window.flight=await import('/flight-playback.js');window.audio=(await import('/player.js')).audio;window.state=(await import('/state.js')).state;await flight.openFlightPlayback('compact-job');window.events=[];for(const e of ['pause','waiting','seeking','emptied'])audio.addEventListener(e,()=>events.push(e));});
 await page.waitForFunction(()=>!state.flightPlayback.loading&&audio.readyState>=3);assert.equal(leases.length,1,'initial buffer obtains one ten-minute asset');
 for(const position of [28000,300000,120000]){await page.evaluate(ms=>flight.seekFlight(ms),position);await page.waitForTimeout(50);assert.ok(Math.abs(await page.evaluate(()=>state.flightPlayback.position())-position)<50);}
 assert.equal(leases.length,1,'seeking within the cached ten-minute asset must not re-download it');
 await page.evaluate(async()=>{await flight.seekFlight(29000);await flight.toggleFlight();events.length=0;});await page.waitForTimeout(2200);assert.ok(await page.evaluate(()=>state.flightPlayback.position())>30900);assert.deepEqual(await page.evaluate(()=>events),[],'original thirty-second seam is continuous');
 await page.evaluate(()=>flight.toggleFlight());const paused=await page.evaluate(()=>state.flightPlayback.position());await page.waitForTimeout(300);assert.ok(Math.abs(await page.evaluate(()=>state.flightPlayback.position())-paused)<30);
 await page.locator('[data-flight-station="second"]').click();assert.ok(Math.abs(await page.evaluate(()=>state.flightPlayback.position())-paused)<70,'station switch retains position');assert.equal(await page.locator('#now-title').innerText(),'Second station');
 const boundary=manifest.tracks[0].segments[1].startMs;
 await page.evaluate(ms=>flight.seekFlight(ms),boundary-1300);await page.waitForFunction(()=>state.flightPlayback.buffer.ahead(state.flightPlayback.position())>2000);await page.evaluate(async()=>{await flight.toggleFlight();events.length=0;});await page.waitForTimeout(2600);assert.ok(await page.evaluate(()=>state.flightPlayback.position())>boundary+800);assert.deepEqual(await page.evaluate(()=>events),[],'ten-minute boundary stays continuous');
 await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:path.join(dir,'browser-mobile.png'),fullPage:true});
 await page.evaluate(()=>flight.stopFlightPlayback());assert.equal(await page.evaluate(()=>audio.getAttribute('src')),null);assert.deepEqual(errors,[]);
 console.log('PASS: actual ten-minute M4A, cached seek without re-download, old thirty-second seam, ten-minute boundary, pause, station sync, mobile and stop.');
 }finally{await browser.close();server.closeAllConnections();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);server.closeAllConnections();server.close();process.exitCode=1;});
