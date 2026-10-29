const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const crypto = require('node:crypto');
module.exports = async function quickActions(page, context) {
  const menu = page.getByRole('menu',{name:'Quick actions'});
  const search = async query => {
    await page.locator('#search').fill(query);
    await page.getByRole('heading',{name:`Results for “${query}”`}).waitFor();
    await page.locator('.track-row').first().waitFor();
  };
  assert.equal(await page.locator('.album-tile').count(),3,'artist search includes albums');
  await page.locator('.track-row').first().click({button:'right'});
  await menu.getByRole('menuitem',{name:'Play album',exact:true}).click();
  await page.waitForFunction(() => document.querySelector('#now-title').textContent === 'Track 7' && document.querySelector('#queue-count').textContent === '02');
  assert.deepEqual(await page.locator('#queue .queue-item strong').allTextContents(),['Track 8','Track 9'],'whole selected album in album order');
  assert.equal(await page.locator('.track-row').count(),9,'play album keeps search results');
  await page.locator('#play').click();

  // Keyboard path, dismissal/focus, viewport positioning, and touch button.
  const trigger = page.locator('.track-row [data-quick-open]').first();
  await trigger.focus();await page.keyboard.press('Shift+F10');
  await menu.waitFor();await page.keyboard.press('End');
  assert.equal(await page.evaluate(() => document.activeElement.textContent),'Download original');
  await page.keyboard.press('Home');await page.keyboard.press('ArrowDown');
  assert.equal(await page.evaluate(() => document.activeElement.textContent),'Play next');
  await page.keyboard.press('Escape');
  assert.equal(await trigger.evaluate(e => e === document.activeElement),true);
  assert.equal(await page.locator('#search').inputValue(),'Radiohead','menu Escape must not clear search');
  await page.locator('.track-row').first().evaluate(e => e.dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,clientX:1439,clientY:999})));
  const box = await menu.boundingBox();assert.ok(box.x>=0&&box.y>=0&&box.x+box.width<=1440&&box.y+box.height<=1000,JSON.stringify(box));
  await page.locator('#search').click();await menu.waitFor({state:'detached'});

  await trigger.click();
  await page.screenshot({path:'/tmp/weazltunes-quick-actions-desktop.png'});
  const dlEvent = page.waitForEvent('download');
  await menu.getByRole('menuitem',{name:'Download original',exact:true}).click();
  const dl = await dlEvent;
  assert.equal(dl.suggestedFilename(),'Radiohead - Track 7.wav');
  assert.equal(await dl.failure(),null);
  const data = await fs.readFile(await dl.path());assert.equal(data.subarray(0,4).toString(),'RIFF');
  const direct = await context.request.get(new URL('/api/media/download?id=2-0',page.url()).href);
  assert.equal(crypto.createHash('sha256').update(data).digest('hex'),crypto.createHash('sha256').update(await direct.body()).digest('hex'));

  await page.locator('.album-tile [data-quick-open]').first().click();
  const zipEvent = page.waitForEvent('download');
  await menu.getByRole('menuitem',{name:'Download album ZIP'}).click();
  const zip = await zipEvent;assert.equal(zip.suggestedFilename(),'In Rainbows.zip');assert.equal(await zip.failure(),null);
  const zipData = await fs.readFile(await zip.path());assert.equal(zipData.readUInt32LE(),0x04034b50);assert.ok(zipData.includes(Buffer.from('001 - Radiohead - Track 7.wav')));

  await page.locator('#queue .queue-item').first().click({button:'right'});
  await menu.getByRole('menuitem',{name:'Play next',exact:true}).click();
  assert.equal(await page.locator('#queue .queue-item').count(),2,'moving queued track must not duplicate it');
  await page.locator('#queue .queue-item').last().click({button:'right'});
  await menu.getByRole('menuitem',{name:'Remove from queue',exact:true}).click();
  assert.equal(await page.locator('#queue .queue-item').count(),1);
  // Queue snapshots saved by older releases resolve album IDs via getSong.
  await page.evaluate(async () => {const {state} = await import('/state.js');delete state.queue[0].albumId;});
  await page.locator('#queue .queue-item').first().click({button:'right'});
  const resolvedAlbum = page.waitForRequest(r => r.url().includes('/api/library/getSong?'));
  await menu.getByRole('menuitem',{name:'Play album',exact:true}).click();await resolvedAlbum;
  await page.waitForFunction(() => document.querySelector('#queue-count').textContent === '02' && document.querySelector('#play').getAttribute('aria-label') === 'Pause');
  await page.locator('#play').click();
  await page.locator('#now-actions').click();
  await menu.getByRole('menuitem',{name:'Open album',exact:true}).click();
  await page.getByRole('heading',{name:'In Rainbows',exact:true}).waitFor();
  assert.equal(await page.locator('.track-row').count(),3);
  await page.locator('[data-download-collection=album]').waitFor();

  await page.locator('nav [data-view=playlists]').click();
  await page.locator('.playlist-tile').filter({hasText:'Renamed browser mixtape'}).getByRole('button',{name:/Actions for/}).click();
  await menu.getByRole('menuitem',{name:'Rename playlist',exact:true}).waitFor();
  await menu.getByRole('menuitem',{name:'Delete playlist',exact:true}).click();
  await page.getByRole('heading',{name:'Delete “Renamed browser mixtape”?',exact:true}).waitFor();
  await page.getByRole('button',{name:'Keep it',exact:true}).click();
  await page.route('**/api/library/getPlaylists?*',route => route.fulfill({json:{playlists:{playlist:[{id:'shared',name:'Shared collection',owner:'someone-else',songCount:1}]}}}));
  await page.locator('nav [data-view=playlists]').click();
  await page.locator('.playlist-tile [data-quick-open]').click();
  assert.equal(await menu.getByRole('menuitem',{name:'Delete playlist',exact:true}).count(),0);
  assert.equal(await menu.getByRole('menuitem',{name:'Rename playlist',exact:true}).count(),0);
  await page.keyboard.press('Escape');await page.unroute('**/api/library/getPlaylists?*');
  await page.locator('nav [data-view=playlists]').click();

  await page.locator('nav [data-view=radio]').click();
  await page.locator('.station').first().click({button:'right'});
  await menu.getByRole('menuitem',{name:'Tune in',exact:true}).waitFor();
  assert.equal(await menu.getByRole('menuitem',{name:/Download/}).count(),0,'live stations have no file download');
  await page.keyboard.press('Escape');
  // Recorder menus delegate to the existing versioned/confirmed controls.
  const instant = new Date().toISOString();
  await page.route('**/api/v1/flight-recorder/sessions',route => route.fulfill({json:{data:[{id:'finished-fixture',name:'Finished fixture',state:'ready',version:1,manifestRevision:1,startsAt:instant,stationCount:1,capturedBytes:0,stations:[]},{id:'active-fixture',name:'Active fixture',state:'recording',version:1,startsAt:instant,stationCount:1,capturedBytes:0,stations:[]}],nextCursor:null}}));
  await page.route('**/api/v1/flight-recorder/schedules',route => route.fulfill({json:{data:[{id:'schedule-fixture',name:'Schedule fixture',state:'active',version:1,nextStartsAt:instant,nextEndsAt:instant,timeZone:'UTC',recurrence:{kind:'once'},stationSnapshots:[]}],nextCursor:null}}));
  await page.route('**/api/v1/flight-recorder/storage',route => route.fulfill({json:{data:{usedBytes:0,accountAvailableBytes:1000000,reserveBytes:0,retention:{enabled:false}}}}));
  await page.locator('nav [data-view=flight-recorder]').click();
  await page.locator('[data-flight-session=finished-fixture] [data-quick-open]').click();
  await menu.getByRole('menuitem',{name:'Listen',exact:true}).waitFor();
  page.once('dialog',dialog => dialog.dismiss());
  await menu.getByRole('menuitem',{name:'Delete server recording',exact:true}).click();
  await page.locator('[data-flight-session=finished-fixture]').waitFor();
  await page.locator('[data-flight-session=active-fixture] [data-quick-open]').click();
  assert.deepEqual(await menu.getByRole('menuitem').allTextContents(),['Stop recording']);
  await page.keyboard.press('Escape');
  await page.locator('.flight-list article').filter({hasText:'Schedule fixture'}).locator('[data-quick-open]').click();
  assert.deepEqual(await menu.getByRole('menuitem').allTextContents(),['Edit','Cancel future recordings']);
  await page.keyboard.press('Escape');
  await search('Radiohead');
  await page.unroute('**/api/v1/flight-recorder/sessions');await page.unroute('**/api/v1/flight-recorder/schedules');await page.unroute('**/api/v1/flight-recorder/storage');
  await page.setViewportSize({width:390,height:844});
  await page.locator('.track-row [data-quick-open]').first().click();
  await menu.getByRole('menuitem',{name:'Play album',exact:true}).waitFor();
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth>innerWidth),false);
  await page.screenshot({path:'/tmp/weazltunes-quick-actions-mobile.png',fullPage:false});
  assert.equal(await menu.isVisible(),true,'menu stays open after focus/automatic scrolling');
  await page.keyboard.press('Escape');await page.setViewportSize({width:1440,height:1000});
  // Album-only match remains useful with zero matching tracks.
  await page.route('**/api/library/search3?*',route => route.fulfill({json:{searchResult3:{album:[{id:'2',name:'In Rainbows',artist:'Radiohead'}],song:[]}}}));
  await page.locator('#search').fill('Radiohead album');
  await page.getByRole('heading',{name:'Results for “Radiohead album”'}).waitFor();
  assert.equal(await page.locator('.album-tile').count(),1);assert.equal(await page.locator('.track-row').count(),0);
  await page.unroute('**/api/library/search3?*');
  await search('Radiohead');
  console.log('PASS: album search, whole-album playback, context/touch/keyboard menus, focus/viewport/dismissal, queue occurrence actions, original/ZIP browser downloads, playlist ownership actions, live radio gating.');
};
