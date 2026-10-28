const { skipTutorial } = require('./returning-user.cjs');
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const net = require('node:net');
const { spawn, execFileSync } = require('node:child_process');
const { once } = require('node:events');
const { chromium, webkit } = require('playwright');
const binary = process.env.WEFTO_BINARY || path.resolve(__dirname, '../../..', 'dist/wefto');

test(`quantized model advice and conservative resource messages (${process.env.BROWSER || 'chromium'})`, { timeout: 180000 }, async t => {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), 'api-router-local-model-ui-'));
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
  const configFile = path.join(layout.config, 'config.json');
  const config = JSON.parse(await fs.readFile(configFile, 'utf8')); config.listen.address = new URL(base).host;
  await fs.writeFile(configFile, JSON.stringify(config), { mode: 0o600 });
  service = spawn(binary, ['serve'], { env, stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true }); exited = once(service, 'exit');
  let output = ''; service.stdout.on('data', chunk => { output += chunk; }); service.stderr.on('data', chunk => { output += chunk; });
  let token;
  for (let i = 0; i < 200; i++) {
    if (service.exitCode !== null) assert.fail(output);
    try { token = (await fs.readFile(path.join(layout.state, 'router.lock', 'management-token'), 'utf8')).trim(); break; } catch {}
    await new Promise(resolve => setTimeout(resolve, 25));
  }
  assert.ok(token);
  const status = async () => (await (await fetch(base + '/api/local-model', { headers: { Authorization: 'Bearer ' + token } })).json()).status;
  assert.equal((await fetch(base + '/api/local-model')).status, 401);
  assert.equal((await status()).intent.enabled, false);
  browser = await (process.env.BROWSER === 'webkit' ? webkit : chromium).launch();
  const errors = [];
  const id4 = 'spark-x2.6-4B', id8 = 'spark-x2.6-4B-Q8_0';
  const name4 = 'Spark-X2.6-4B-Q4_K_M', name8 = 'Spark-X2.6-4B-Q8_0';
  const baseReport = () => ({
    device: { availableMemoryBytes: { value: 12 * 2 ** 30 } },
    recommendation: { tier: '4B', maxTier: '4B' }, detection: { state: 'passed', reasons: [] },
    advice: { policy: 'spark26-64k-v2', state: 'recommended', modelID: id8, modelName: name8, quantization: 'Q8_0', reason: 'resources_suitable', assessments: [
      { id: id4, name: name4, quantization: 'Q4_K_M', automatic: 'passed', manual: 'passed', issues: [], disks: [{ path: layout.models, volume: { value: 'same' }, available: { value: 100 * 2 ** 30 }, required: 10 * 2 ** 30 }] },
      { id: id8, name: name8, quantization: 'Q8_0', automatic: 'passed', manual: 'passed', issues: [], disks: [{ path: layout.models, volume: { value: 'same' }, available: { value: 100 * 2 ** 30 }, required: 12 * 2 ** 30 }] },
    ] },
  });
  let firstPage = true;
  for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) {
    for (const width of [1600, 375]) {
      const page = await browser.newPage({ locale: 'en-US', viewport: { width, height: 950 } });
      page.on('pageerror', error => errors.push(error.message));
      const startup = [];
      page.on('response', response => { if (new URL(response.url()).origin === base && response.status() >= 400) startup.push({ url: new URL(response.url()).pathname, status: response.status() }); });
      page.on('requestfailed', request => startup.push({ url: new URL(request.url()).pathname, failed: request.failure()?.errorText }));
      page.on('console', message => { if (message.type() === 'error') startup.push({ console: message.text() }); });
      const actions = [];
      let report = baseReport();
      page.on('request', request => { if (request.method() === 'POST' && new URL(request.url()).pathname.startsWith('/api/local-model/')) actions.push(new URL(request.url()).pathname); });
      await page.route('**/api/local-model/detect', async route => report === null ? route.abort('failed') : route.fulfill({ json: { schemaVersion: 1, device: report } }));
      await skipTutorial(page);
      await page.goto(`${base}/?locale=${language}&view=local-model`);
      try { await page.waitForFunction(name => document.querySelector('#localDeviceMessage')?.textContent.includes(name), name8, { timeout: 10000 }); } catch (error) { t.diagnostic(JSON.stringify({ language, width, errors, startup, refreshError: await page.evaluate(async () => { try { await refreshService(); return null } catch (e) { return { message: e.message, stack: e.stack } } }), health: await fetch(base + '/healthz').then(r => r.status).catch(e => e.message), state: await page.evaluate(() => ({ message: document.querySelector('#localDeviceMessage')?.textContent, error: document.querySelector('#localModelError')?.textContent, content: document.querySelector('#localModelContent')?.textContent, service: document.querySelector('#service')?.textContent })) })); throw error; }
      assert.deepEqual(await page.locator('#localModelChoices button').allTextContents(), [name4, name8]);
      assert.equal(await page.locator('#localModelChoices button').nth(firstPage ? 1 : 0).getAttribute('aria-pressed'), 'true', 'Q8 is the initial default; a saved explicit Q4 choice is preserved');
      assert.ok(actions.every(action => action === '/api/local-model/detect'), 'detecting and choosing a default does not persist intent or download');
      // Explicit user choice must survive later high-spec Q8 recommendations.
      await page.locator('#localModelChoices button').first().click();
      const check = async () => {
        await page.locator('#localDeviceGuide [data-action=detect]').click();
        await page.waitForFunction(() => !document.querySelector('#localDeviceGuide [data-action=detect]').disabled);
      };
      await check();
      assert.equal(await page.locator('#localModelChoices button').first().getAttribute('aria-pressed'), 'true');
      report.advice.state = 'not_recommended'; report.advice.modelID = ''; report.advice.modelName = '';
      report.advice.reason = 'memory_below_16_gib';
      report.advice.assessments.forEach(item => { item.automatic = 'failed'; item.manual = 'failed'; });
      report.advice.assessments[0].issues = [{ code: 'memory_below_16_gib', available: 12 * 2 ** 30, required: 16 * 2 ** 30 }];
      await check();
      const declined = await page.evaluate(() => t('Local models are not recommended for this computer. You can use cloud models without downloading one.'));
      assert.equal(await page.locator('#localDeviceMessage').textContent(), declined);
      const memoryReason = await page.evaluate(() => t('Installed memory: {available} GiB; automatic recommendations need {required} GiB.', { available: '12.0', required: '16.0' }));
      assert.ok((await page.locator('#localDeviceGuide').textContent()).includes(memoryReason), 'the hardware decline explains the measured total memory');
      assert.equal(await page.locator('#localModelContent [data-action=install]').isDisabled(), true);
      report.advice.state = 'deferred'; report.advice.reason = 'available_memory_insufficient';
      report.advice.assessments[0].issues = [{ code: 'available_memory_insufficient', available: 6 * 2 ** 30, required: 8 * 2 ** 30 }];
      await check();
      const deferred = await page.evaluate(() => t('Local download is not recommended right now. Check the resource details and detect again.'));
      assert.equal(await page.locator('#localDeviceMessage').textContent(), deferred);
      const guide = await page.locator('#localDeviceGuide').textContent();
      assert.ok(guide.includes('6.0') && guide.includes('8.0'), 'temporary pressure shows the actual resource shortfall');
      report.advice.state = 'not_recommended'; report.advice.reason = 'cpu_threads_below_8';
      report.advice.assessments[0].manual = 'passed'; report.advice.assessments[0].issues = [{ code: 'cpu_threads_below_8' }];
      await check();
      assert.equal(await page.locator('#localModelContent [data-action=install]').isEnabled(), true, 'safe manual use is separate from automatic recommendation');
      report.advice.state = 'deferred'; report.advice.reason = 'available_memory_unknown';
      report.advice.assessments[0].manual = 'unknown'; report.advice.assessments[0].issues = [{ code: 'available_memory_unknown' }];
      await check();
      const unknown = await page.evaluate(() => t('Some device or disk information is unavailable. You can still download and try loading a model.'));
      assert.equal(await page.locator('#localDeviceMessage').textContent(), unknown);
      const unavailableMemory = await page.evaluate(() => t('Available memory could not be read. You can still download and try loading a model.'));
      assert.ok((await page.locator('#localDeviceGuide').textContent()).includes(unavailableMemory), 'missing available memory is identified separately from disk space');
      assert.equal(await page.locator('#localModelContent [data-action=install]').isEnabled(), true, 'unknown resources allow explicit installation');
      report.advice.assessments[0].manual = 'failed'; report.advice.assessments[0].issues.push({ code: 'disk_insufficient', available: 0, required: 10 * 2 ** 30 });
      await check();
      assert.equal(await page.locator('#localModelContent [data-action=install]').isDisabled(), true, 'known disk shortage still blocks when memory is unknown');
      report = baseReport();
      report.device.system = { value: { os: 'darwin', architecture: { value: 'arm64' } } };
      report.device.availableMemoryBytes.value = 4.7 * 2 ** 30;
      report.advice.state = 'deferred'; report.advice.modelID = ''; report.advice.modelName = '';
      report.advice.reason = 'available_memory_below_recommended';
      const macQ4 = report.advice.assessments[0];
      macQ4.download = 'passed'; macQ4.automatic = 'failed';
      macQ4.issues = [{ code: 'available_memory_below_recommended', available: 4.7 * 2 ** 30, required: 8 * 2 ** 30 }];
      await check();
      const macTrial = await page.evaluate(() => t('Q4 can be downloaded and tried manually on this Mac. Available memory is below the automatic recommendation.'));
      assert.equal(await page.locator('#localDeviceMessage').textContent(), macTrial);
      const macGuide = await page.locator('#localDeviceGuide').textContent();
      assert.ok(macGuide.includes('4.7') && macGuide.includes('8.0'), 'Mac trial shows headroom and recommendation separately');
      assert.equal(await page.locator('#localModelContent [data-action=install]').isEnabled(), true, '4.7 GiB Mac can download Q4');
      // Lower headroom still permits downloading without enabling inference.
      macQ4.manual = 'failed';
      macQ4.issues = [{ code: 'available_memory_insufficient', available: 3 * 2 ** 30, required: 4 * 2 ** 30 }];
      await check();
      const downloadOnly = await page.evaluate(() => t('You can download this model now. Check the resource details before loading it.'));
      assert.equal(await page.locator('#localDeviceMessage').textContent(), downloadOnly);
      assert.equal(await page.locator('#localModelContent [data-action=install]').isEnabled(), true);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, `${language}/${width}: Mac details fit the viewport`);
      const beforeDownload = actions.length;
      const downloads = [];
      await page.route('**/api/local-model/install', async route => {
        downloads.push(route.request().postDataJSON());
        await route.fulfill({ json: { schemaVersion: 1, status: await status() } });
      });
      await page.locator('#localModelContent [data-action=install]').click();
      await page.waitForFunction(() => !document.querySelector('#localModelContent [data-action=install]').disabled);
      assert.deepEqual(actions.splice(beforeDownload), ['/api/local-model/install'], 'download-only path never enables or loads a model');
      assert.deepEqual(downloads, [{ model: id4 }]);
      macQ4.download = 'failed'; macQ4.issues.push({ code: 'disk_insufficient', available: 0, required: 10 * 2 ** 30 });
      await check();
      assert.equal(await page.locator('#localModelContent [data-action=install]').isDisabled(), true, 'Mac downloads still require disk space');
      report = null;
      await check();
      const failed = await page.evaluate(() => t('Device detection failed. You can still download and try loading a model.'));
      assert.equal(await page.locator('#localDeviceMessage').textContent(), failed);
      assert.equal(await page.locator('#localModelContent [data-action=install]').isEnabled(), true, 'failed detection allows explicit installation and backend rechecks resources');
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, `${language}/${width}: resource details fit the viewport`);
      assert.ok(actions.every(action => ['/api/local-model/detect', '/api/local-model/select'].includes(action)), 'advice never installs, enables or unloads a model');
      const current = await status();
      assert.equal(current.intent.enabled, false); assert.equal(current.runtime, 'unloaded');
      await page.evaluate(() => window.routerLocalDisconnected());
      await page.unrouteAll({ behavior: 'wait' }); await page.close(); firstPage = false;
    }
  }
  // A fresh low-headroom Mac has no automatic recommendation or saved choice.
  // Offer Q4 as a page choice without saving intent or starting a download.
  for (const width of [1600, 375]) {
    const page = await browser.newPage({ viewport: { width, height: 950 } });
    page.on('pageerror', error => errors.push(error.message));
    const actions = [];
    page.on('request', request => { if (request.method() === 'POST' && new URL(request.url()).pathname.startsWith('/api/local-model/')) actions.push(new URL(request.url()).pathname); });
    const report = baseReport();
    report.device.system = { value: { os: 'darwin', architecture: { value: 'arm64' } } };
    report.device.availableMemoryBytes.value = 4.7 * 2 ** 30;
    report.advice.state = 'deferred'; report.advice.modelID = ''; report.advice.modelName = '';
    report.advice.reason = 'available_memory_below_recommended';
    report.advice.assessments[0].download = 'passed'; report.advice.assessments[0].automatic = 'failed';
    report.advice.assessments[0].issues = [{ code: 'available_memory_below_recommended', available: 4.7 * 2 ** 30, required: 8 * 2 ** 30 }];
    await page.route('**/api/local-model/detect', route => route.fulfill({ json: { schemaVersion: 1, device: report } }));
    await page.route('**/api/local-model', async route => {
      const response = await route.fetch();
      const value = await response.json();
      value.status.intent = { ...value.status.intent, selected: '', enabled: false };
      value.status.installation = 'not_installed';
      await route.fulfill({ json: value });
    });
    await skipTutorial(page);
    await page.goto(`${base}/?locale=zh&view=local-model`);
    await page.waitForFunction(() => document.querySelector('#localDeviceMessage')?.textContent.includes('Q4'));
    assert.equal(await page.locator('#localModelChoices button').first().getAttribute('aria-pressed'), 'true');
    assert.equal(await page.locator('#localModelContent [data-action=install]').isEnabled(), true);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false);
    assert.deepEqual(actions, ['/api/local-model/detect'], 'fresh Mac Q4 default is only a local page choice');
    await page.evaluate(() => window.routerLocalDisconnected());
    await page.unrouteAll({ behavior: 'wait' }); await page.close();
  }
  assert.deepEqual(errors, []);
});
