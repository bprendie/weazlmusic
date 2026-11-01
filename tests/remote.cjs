const assert=require('node:assert/strict');
const {chromium}=require('playwright');
const {spawn}=require('node:child_process');
const fs=require('node:fs');const os=require('node:os');const path=require('node:path');
(async()=>{
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'weazl-remote-'));
 const fixture=spawn('go',['run','./cmd/weazlfixture'],{env:{...process.env,LISTEN_ADDR:'127.0.0.1:4005',FIXTURE_NAV_LISTEN:'127.0.0.1:4537',DATA_DIR:path.join(dir,'data')},detached:true,stdio:['ignore','pipe','pipe']});
 let receiver,browser;let output='';fixture.stdout.on('data',b=>output+=b);fixture.stderr.on('data',b=>output+=b);
 try{
  for(let i=0;i<150;i++){try{if((await fetch('http://127.0.0.1:4005/api/v1/info')).ok&&output.includes('Fixture ready'))break;}catch{}await new Promise(r=>setTimeout(r,200));}
  assert(output.includes('Fixture ready'),'isolated fixture failed to start');
  receiver=spawn('python3',['scripts/remote-receiver.py','--server','http://127.0.0.1:4005','--state-file',path.join(dir,'receiver.json')],{stdio:['ignore','pipe','pipe']});let receiverLog='';receiver.stderr.on('data',b=>receiverLog+=b);
  browser=await chromium.launch({headless:true,executablePath:process.env.BROWSER_BIN||'/usr/bin/chromium',args:['--no-sandbox']});
  const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto('http://127.0.0.1:4005');
  await page.locator('#login-form [name=username]').fill('alice');await page.locator('#login-form [name=password]').fill('test-password');await page.locator('#login-form button').click();
  await page.waitForFunction(()=>document.querySelector('#play-on')?.options.length>1);
  await page.evaluate(async()=>{const {playList,audio}=await import('/player.js');window.remoteState=(await import('/state.js')).state;window.remoteAudio=audio;playList([{id:'0-0',title:'First',duration:180},{id:'0-0',title:'Repeated',duration:180},{id:'0-1',title:'Next',duration:180}]);});
  await page.waitForFunction(()=>!window.remoteAudio.paused);
  const value=await page.locator('#play-on option').nth(1).getAttribute('value');await page.locator('#play-on').selectOption(value);
  await page.waitForFunction(()=>!!window.remoteState.remote&&!window.remoteAudio.getAttribute('src'),null,{timeout:25000});
  assert.equal(await page.evaluate(async()=>{const {state}=await import('/state.js');return state.remote.state.playback.queue.length;}),3);
  await page.locator('#play').click();await page.waitForFunction(()=>!window.remoteState.playing);
  await page.locator('#next').click();await page.waitForFunction(()=>window.remoteState.remote.state.playback.currentEntryId===window.remoteState.remote.state.playback.queue[1].entryId);
  await page.evaluate(async()=>{(await import('/player.js')).enqueue({id:'0-2',title:'Enqueued',duration:180});});
  await page.waitForFunction(()=>window.remoteState.remote.state.playback.queue.length===4);
  await page.locator('#volume').fill('42');await page.locator('#volume').dispatchEvent('input');
  await page.waitForFunction(()=>window.remoteState.remote.state.volume===.42);
  await page.setViewportSize({width:390,height:844});assert(await page.locator('#play-on').isVisible());
  await page.locator('#play-on').selectOption('');await page.waitForFunction(()=>!window.remoteState.remote&&!window.remoteState.remotePending).catch(async e=>{console.error('Return recovery failed',await page.evaluate(()=>({remote:window.remoteState.remote,pending:window.remoteState.remotePending,toast:document.querySelector('#toast').textContent,audio:{src:window.remoteAudio.src,paused:window.remoteAudio.paused,ready:window.remoteAudio.readyState}})),receiverLog);throw e;});
  await page.evaluate(async()=>{const s=window.remoteState.stations[0];await (await import('/player.js')).play({id:s.id,url:s.url,title:s.name});});
  await page.locator('#play-on').selectOption(value);
  await page.waitForFunction(()=>window.remoteState.remote?.state.playback.kind==='radioLive');
  const heldLength=await page.evaluate(()=>window.remoteState.remote.state.playback.heldLibrary.queue.length);
  await page.evaluate(async()=>{(await import('/player.js')).enqueue({id:'0-3',title:'Held queue append',duration:180});});
  await page.waitForFunction(length=>window.remoteState.remote.state.playback.heldLibrary.queue.length===length+1,heldLength);
  assert.equal(await page.evaluate(()=>window.remoteState.remote.state.playback.kind),'radioLive');
  await page.locator('#play-on').selectOption('');await page.waitForFunction(()=>!window.remoteState.remote&&!window.remoteState.remotePending);
  const restart=async flag=>{
   const old=(await (await page.request.get('http://127.0.0.1:4005/api/v1/remote/players/'+value)).json()).data.connectionId;
   const exiting=new Promise(resolve=>receiver.once('exit',resolve));receiver.kill();await exiting;
   receiver=spawn('python3',['scripts/remote-receiver.py','--server','http://127.0.0.1:4005','--state-file',path.join(dir,'receiver.json'),flag],{stdio:['ignore','pipe','pipe']});receiver.stderr.on('data',b=>receiverLog+=b);
   let ready=false;for(let i=0;i<80;i++){const p=(await (await page.request.get('http://127.0.0.1:4005/api/v1/remote/players/'+value)).json()).data;if(p.online&&p.connectionId!==old){ready=true;break;}await new Promise(resolve=>setTimeout(resolve,100));}assert(ready,'restarted receiver did not become available');await page.waitForFunction(id=>!Array.from(document.querySelector('#play-on').options).find(o=>o.value===id)?.disabled,value);
  };
  await restart('--reject');
  await page.evaluate(async()=>{await (await import('/player.js')).play({id:'0-0',title:'Local survivor',duration:180});});
  await page.locator('#play-on').selectOption(value);
  await page.waitForFunction(()=>!window.remoteState.remotePending);
  assert.equal(await page.evaluate(()=>!!window.remoteState.remote),false);
  assert.equal(await page.evaluate(()=>window.remoteAudio.paused),false);
  await restart('--drop-ack');
  await page.locator('#play-on').selectOption(value);
  await page.waitForFunction(()=>!!window.remoteState.remote&&!window.remoteAudio.getAttribute('src'),null,{timeout:20000}).catch(async e=>{console.error('Lost acknowledgement recovery failed',await page.evaluate(()=>({remote:window.remoteState.remote,pending:window.remoteState.remotePending,toast:document.querySelector('#toast').textContent})),(await (await page.request.get('http://127.0.0.1:4005/api/v1/remote/players/'+value)).json()).data,receiverLog);throw e;});
  await page.locator('#play-on').selectOption('');await page.waitForFunction(()=>!window.remoteState.remote&&!window.remoteState.remotePending);
  assert.deepEqual(errors,[]);assert(!receiverLog.includes('Traceback'),receiverLog);
  console.log('PASS: real protocol simulator/media leases, duplicate queue, browser transfer/pause/next/enqueue/volume/radio/return/rejection/lost-ack, mobile output selector.');
 }finally{await browser?.close();receiver?.kill();try{process.kill(-fixture.pid,'SIGTERM');}catch{}await new Promise(r=>setTimeout(r,300));fs.rmSync(dir,{recursive:true,force:true});}
})().catch(e=>{console.error(e);process.exitCode=1});
