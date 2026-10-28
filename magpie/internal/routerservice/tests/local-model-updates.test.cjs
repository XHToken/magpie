'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const net = require('node:net');
const { spawn, execFileSync } = require('node:child_process');
const { once } = require('node:events');
const { chromium, webkit } = require('playwright');
const { skipTutorial } = require('./returning-user.cjs');
const binary = process.env.WEFTO_BINARY || path.resolve(__dirname, '../../..', 'dist/wefto');

test(`local model push updates (${process.env.BROWSER || 'chromium'})`, { timeout: 120000 }, async t => {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), 'api-router-push-ui-'));
  let service, exited, browser;
  t.after(async () => {
    if (browser) await browser.close();
    if (service) { if (service.exitCode === null) service.kill('SIGTERM'); await exited; }
    await fs.rm(home, { recursive: true, force: true });
  });
  const env = { ...process.env, HOME: home, USERPROFILE: home };
  for (const key of ['XDG_CONFIG_HOME', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME', 'APPDATA', 'LOCALAPPDATA']) env[key] = path.join(home, key);
  const layout = JSON.parse(execFileSync(binary, ['dirs'], { env, encoding: 'utf8' }));
  execFileSync(binary, ['config', 'init'], { env });
  const reservation = net.createServer(); reservation.listen(0, '127.0.0.1'); await once(reservation, 'listening');
  const base = `http://127.0.0.1:${reservation.address().port}`; await new Promise(resolve => reservation.close(resolve));
  const configFile = path.join(layout.config, 'config.json'), config = JSON.parse(await fs.readFile(configFile, 'utf8'));
  config.listen.address = new URL(base).host;
  await fs.writeFile(configFile, JSON.stringify(config));
  service = spawn(binary, ['serve'], { env, stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true }); exited = once(service, 'exit');
  let output = '', token;
  service.stdout.on('data', chunk => { output += chunk; }); service.stderr.on('data', chunk => { output += chunk; });
  for (let i = 0; i < 200; i++) {
    if (service.exitCode !== null) assert.fail(output);
    try { token = (await fs.readFile(path.join(layout.state, 'router.lock', 'management-token'), 'utf8')).trim(); break; } catch {}
    await new Promise(resolve => setTimeout(resolve, 25));
  }
  assert.ok(token);
  const original = await (await fetch(base + '/api/local-model', { headers: { Authorization: 'Bearer ' + token } })).json();
  browser = await (process.env.BROWSER === 'webkit' ? webkit : chromium).launch({ headless: true });
  for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) {
    const page = await browser.newPage({ locale: 'en-US' });
    const errors = []; page.on('pageerror', error => errors.push(error.message));
    // Keep transport delivery under test control while exercising the real
    // browser ReadableStream/decoder, abort handling and the production page.
    await page.addInitScript(() => {
      const originalFetch = window.fetch;
      let receiver, releaseReceiver, hidden = false;
      Object.defineProperty(document, 'hidden', { get: () => hidden, configurable: true });
      window.__visibility = value => { hidden = value; document.dispatchEvent(new Event('visibilitychange')); };
      window.__streams = { opened: 0, active: 0, reads: 0, paths: [] };
      window.fetch = async (url, options) => {
        if (url === '/api/local-model') window.__streams.reads++;
        if (url !== '/api/local-model/events') return originalFetch(url, options);
        window.__streams.opened++; window.__streams.active++;
        window.__streams.paths.push({ url, auth: new Headers(options.headers).get('Authorization') });
        let active = true;
        const release = () => { if (active) { active = false; window.__streams.active--; } };
        const body = new ReadableStream({
          start(controller) {
            receiver = controller; releaseReceiver = release;
            options.signal.addEventListener('abort', () => { release(); controller.error(new DOMException('Aborted', 'AbortError')); }, { once: true });
          },
          cancel: release
        });
        return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } });
      };
      window.__push = status => {
        const bytes = new TextEncoder().encode(`: keep-alive\n\ndata: ${JSON.stringify({ schemaVersion: 1, status })}\n\n`);
        // Deliver a split frame, including a split UTF-8 character.
        const split = bytes.findIndex(byte => byte >= 0xe0) + 1;
        receiver.enqueue(bytes.slice(0, split)); receiver.enqueue(bytes.slice(split));
      };
      window.__end = () => { releaseReceiver(); receiver.close(); };
    });
    let snapshot = structuredClone(original), reads = 0, privacyReads = 0;
    snapshot.status = { ...snapshot.status, installation: 'downloading', runtime: 'unloaded', dependencies: { state: 'ready' }, intent: { ...snapshot.status.intent, enabled: true, selected: 'spark-x2.6-4B' }, progress: { downloaded: 10 * 1048576, total: 100 * 1048576, artifact: '妯″瀷' } };
    await page.route('**/api/local-model', route => { reads++; return route.fulfill({ json: snapshot }); });
    page.on('request', request => { if (new URL(request.url()).pathname === '/api/redaction') privacyReads++; });
    await skipTutorial(page); await page.goto(`${base}/?locale=${language}&view=local-model`);
    await page.waitForFunction(() => document.querySelector('#localDownloadProgress progress')?.value === 10);
    await page.evaluate(() => window.routerLocalOpen());
    await page.waitForFunction(() => window.__streams.active === 1);
    // Complete the real management handshake before virtual time drives the
    // download/backoff assertions; its network timeout is not a simulated event.
    await page.clock.install();
    const initialReads = reads, initialPrivacyReads = privacyReads;
    await page.clock.runFor(15000);
    assert.equal(reads, initialReads, 'a healthy download stream never polls full model state');
    assert.equal(privacyReads, initialPrivacyReads, 'download waiting never polls redaction');
    assert.equal(await page.evaluate(() => window.__streams.active), 1, 'one stream carries the active download');
    snapshot.status.progress.downloaded = 50 * 1048576;
    await page.evaluate(status => window.__push(status), snapshot.status);
    await page.waitForFunction(() => document.querySelector('#localDownloadProgress progress')?.value === 50);
    assert.equal(privacyReads, initialPrivacyReads, 'byte progress does not refresh privacy state');

    await page.evaluate(() => window.__visibility(true));
    await page.waitForFunction(() => window.__streams.active === 0);
    await page.clock.runFor(60000);
    assert.equal(reads, initialReads, 'hidden pages do not observe or reconnect');
    await page.evaluate(() => window.__visibility(false));
    await page.waitForFunction(() => window.__streams.active === 1);
    assert.equal(reads, initialReads + 1, 'returning to the page re-reads the latest snapshot');

    await page.evaluate(() => show('providers'));
    await page.waitForFunction(() => window.__streams.active === 0);
    const departedReads = reads;
    await page.clock.runFor(60000);
    assert.equal(reads, departedReads, 'leaving the model page stops observation');
    await page.evaluate(() => show('local-model'));
    await page.waitForFunction(() => window.__streams.active === 1);

    // Freeze playback before testing the retry boundary: real time spent in
    // browser assertions must not count toward the one-second backoff.
    await page.clock.pauseAt(await page.evaluate(() => Date.now() + 60000));
    const opened = await page.evaluate(() => window.__streams.opened);
    const retryReads = await page.evaluate(() => window.__streams.reads);
    await page.evaluate(() => window.__end());
    await page.waitForFunction(() => document.querySelector('#localModelError').textContent === t('Connection unavailable'));
    await page.clock.runFor(999);
    assert.equal(await page.evaluate(() => window.__streams.opened), opened, 'disconnect recovery backs off');
    assert.equal(await page.evaluate(() => window.__streams.reads), retryReads, 'the retry does not start before the backoff');
    await page.clock.runFor(1);
    assert.equal(await page.evaluate(() => window.__streams.reads), retryReads + 1, 'the retry starts at the backoff boundary');
    // The REST refresh and stream reopen finish asynchronously. Resume frames
    // after checking the exact boundary so WebKit's RAF-based wait can observe
    // their completion even when the response arrives after runFor returns.
    await page.clock.resume();
    await page.waitForFunction(count => window.__streams.opened === count + 1, opened);
    await page.waitForFunction(() => window.__streams.active === 1);

    const beforeCompletion = reads;
    snapshot.status.installation = 'installed'; snapshot.status.progress = {};
    snapshot.models = [{ id: 'spark-x2.6-4B', installed: true }];
    await page.evaluate(status => window.__push(status), snapshot.status);
    await page.waitForFunction(() => !!document.querySelector('#localInstalledModels [data-model="spark-x2.6-4B"]'));
    await page.waitForFunction(() => window.__streams.active === 0);
    assert.equal(reads, beforeCompletion + 1, 'completion refreshes installed candidates exactly once');
    const completedReads = reads, completedPrivacy = privacyReads;
    await page.clock.runFor(60000);
    assert.equal(reads, completedReads, 'completed installations do not poll');
    assert.equal(privacyReads, completedPrivacy, 'completed installations do not poll privacy state');
    assert.ok((await page.evaluate(() => window.__streams.paths)).every(item => item.url === '/api/local-model/events' && item.auth?.startsWith('Bearer ')), 'stream authentication stays out of URLs');
    assert.deepEqual(errors, []);
    await page.close();
  }
});
