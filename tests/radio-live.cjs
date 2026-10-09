// Opt-in check against an isolated local app configured for the chosen station.
const assert = require('node:assert/strict');
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
(async () => {
 const base = process.env.RADIO_TEST_APP || 'http://127.0.0.1:4012';
 assert.equal(new URL(base).hostname, '127.0.0.1', 'Use an isolated local app');
 const stream = process.env.WEAZL_TEST_RADIO_URL;
 assert.ok(stream, 'Set WEAZL_TEST_RADIO_URL explicitly');
 const browser = await chromium.launch({headless:true, executablePath:process.env.BROWSER_BIN || '/usr/bin/chromium', args:['--no-sandbox','--autoplay-policy=no-user-gesture-required']});
 try {
  const page = await browser.newPage();
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  await page.goto(base);
  await page.locator('#login-form input[name=username]').fill('weazladmin');
  await page.locator('#login-form input[name=password]').fill('admin');
  await page.locator('#login-form button').click();
  await page.locator('#login-screen').waitFor({state:'hidden'});
  await page.evaluate(async url => {window.player = await import('/player.js');await player.play({id:'live-radio-check',name:'Live radio check',title:'Live radio check',url});}, stream);
  await page.waitForFunction(() => player.audio.currentTime > 3 && !player.audio.paused && !player.audio.error, {timeout:20000});
  const result = await page.evaluate(() => ({seconds:player.audio.currentTime,readyState:player.audio.readyState}));
  await page.evaluate(() => player.stop());
  assert.deepEqual(errors, []);
  console.log('PASS: live radio decoded through authenticated web relay', result);
 } finally {await browser.close();}
})().catch(e => {console.error(e);process.exitCode=1;});
