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
const models = [
  { id: 'spark-x2.6-4B', name: 'Spark-X2.6-4B-Q4_K_M' },
  { id: 'spark-x2.6-4B-Q8_0', name: 'Spark-X2.6-4B-Q8_0' },
  { id: 'spark-x2.6-1.7B', name: 'Spark-X2.6-1.7B-Q4_K_M' },
  { id: 'spark-x2.6-1.7B-Q8_0', name: 'Spark-X2.6-1.7B-Q8_0' },
].map(model => ({ ...model, selector: 'local/' + model.id.toLowerCase() }));
const small = models.slice(2);

test(`four-model display and selection across pages (${process.env.BROWSER || 'chromium'})`, { timeout: 300000 }, async t => {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), 'spark-model-surfaces-'));
  const env = { ...process.env, HOME: home, USERPROFILE: home };
  for (const key of ['XDG_CONFIG_HOME', 'XDG_DATA_HOME', 'XDG_CACHE_HOME', 'XDG_STATE_HOME', 'APPDATA', 'LOCALAPPDATA']) env[key] = path.join(home, key);
  const loomy = path.join(home, '.config', 'loomy-opencode'), workbuddy = path.join(home, 'workbuddy');
  env.LOOMY_CONFIG_DIR = loomy; env.LOOMY_OPENCODE_DIR = loomy; env.WORKBUDDY_CONFIG_DIR = workbuddy;
  await fs.mkdir(loomy, { recursive: true }); await fs.mkdir(workbuddy, { recursive: true });
  await fs.writeFile(path.join(loomy, 'opencode.json'), JSON.stringify({ model: 'original/model', provider: {} }));
  await fs.writeFile(path.join(workbuddy, 'models.json'), JSON.stringify({ models: [] }));
  await fs.writeFile(path.join(workbuddy, 'settings.json'), JSON.stringify({ model: 'original-model' }));
  const layout = JSON.parse(execFileSync(binary, ['dirs'], { env, encoding: 'utf8' }));
  execFileSync(binary, ['config', 'init'], { env });
  const reservation = net.createServer(); reservation.listen(0, '127.0.0.1'); await once(reservation, 'listening');
  const base = `http://127.0.0.1:${reservation.address().port}`; await new Promise(resolve => reservation.close(resolve));
  const configFile = path.join(layout.config, 'config.json'), config = JSON.parse(await fs.readFile(configFile, 'utf8'));
  config.listen.address = new URL(base).host;
  // Synthetic installation metadata enables real Profile/group/host saves;
  // runtime projections below never launch or download a real model.
  config.localModels = models.map(model => ({ id: model.id.toLowerCase(), installed: true, enabled: false, status: 'installed' }));
  await fs.writeFile(configFile, JSON.stringify(config));
  const service = spawn(binary, ['serve'], { env, stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true }), exited = once(service, 'exit');
  service.stdout.resume(); service.stderr.resume();
  let browser, token;
  t.after(async () => {
    if (browser) await browser.close();
    if (service.exitCode === null) service.kill('SIGTERM');
    await exited;
    await fs.rm(home, { recursive: true, force: true });
  });
  for (let i = 0; i < 200; i++) {
    assert.equal(service.exitCode, null);
    try { token = (await fs.readFile(path.join(layout.state, 'router.lock', 'management-token'), 'utf8')).trim(); break; } catch {}
    await new Promise(resolve => setTimeout(resolve, 25));
  }
  assert.ok(token);
  const headers = { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' };
  const api = async url => (await fetch(base + url, { headers })).json();
  const original = await api('/api/local-model');
  assert.deepEqual(original.catalog.models.map(model => model.name), models.map(model => model.name));
  browser = await (process.env.BROWSER === 'webkit' ? webkit : chromium).launch();
  const errors = [];
  const pageFor = async (language, width) => {
    const page = await browser.newPage({ locale: 'en-US', viewport: { width, height: 1000 } });
    page.on('pageerror', error => errors.push(error.message));
    await skipTutorial(page);
    return page;
  };
  const fit = async (page, label) => {
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), true, label);
    const overflow = await page.locator('.am-pop .am-mr:visible').evaluateAll(rows => rows.flatMap(row => {
      const box = row.getBoundingClientRect(), name = row.querySelector('.am-mn'), range = document.createRange();
      range.selectNodeContents(name);
      return [...range.getClientRects()].filter(rect => rect.left < box.left - 1 || rect.right > box.right + 1 || rect.top < box.top - 1 || rect.bottom > box.bottom + 1).map(() => row.dataset.model);
    }));
    assert.deepEqual(overflow, [], label + ': names stay inside their picker rows');
    const clipped = await page.locator('#localProviders .local-provider .name, #localInstalledModels .local-provider .name').evaluateAll(nodes => nodes.filter(node => node.clientWidth && node.scrollWidth > node.clientWidth + 1).map(node => node.textContent));
    assert.deepEqual(clipped, [], label + ': quantization labels remain readable');
  };
  const capture = async (page, name) => {
    if (!process.env.ARTIFACT_DIR) return;
    await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
    await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, name + '.png'), fullPage: true });
  };
  const go = async (page, language, view) => {
    await page.goto(`${base}/?locale=${language}&view=${view}`);
    await page.waitForFunction(() => !!managementToken && !!serviceInfo && healthy);
  };
  const close = async page => { await page.evaluate(() => window.routerLocalDisconnected()); await page.unrouteAll({ behavior: 'wait' }); await page.close(); };

  await t.test('provider shortcuts preview the exact model and keep the running model', async () => {
    const page = await pageFor('zh', 375), mutations = [];
    try {
      await page.addInitScript(() => {
        const originalFetch = window.fetch;
        window.fetch = (url, options) => url !== '/api/local-model/events' ? originalFetch(url, options) : Promise.resolve(new Response(new ReadableStream({
          start(controller) { options.signal.addEventListener('abort', () => controller.error(new DOMException('Aborted', 'AbortError')), { once: true }); }
        }), { headers: { 'Content-Type': 'text/event-stream' } }));
      });
      await page.route('**/api/local-model', route => route.fulfill({ json: { ...original,
        models: models.map(model => ({ id: model.id, installed: true })),
        status: { ...original.status, installation: 'installed', runtime: 'idle', healthy: true, pid: 123,
          dependencies: { state: 'ready' }, intent: { selected: models[0].id, enabled: true } },
      } }));
      page.on('request', request => { if (request.method() === 'POST' && new URL(request.url()).pathname.startsWith('/api/local-model/')) mutations.push(new URL(request.url()).pathname); });
      for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) for (const width of [1440, 375, 320]) {
        await page.setViewportSize({ width, height: 1000 });
        await go(page, language, 'providers');
        for (const model of small) {
          const row = page.locator(`#localProviders [data-local-model="${model.id.toLowerCase()}"]`);
          await row.waitFor(); assert.equal(await row.locator('.name').textContent(), model.name);
          await row.locator('[data-action="open-local-model"]').click();
          await page.locator('#localModelChoices button').filter({ hasText: model.name }).waitFor();
          assert.equal(await page.locator('#localModelChoices button').filter({ hasText: model.name }).getAttribute('aria-pressed'), 'true', 'shortcut must focus its own model');
          assert.deepEqual(await page.locator('#localInstalledModels .name').allTextContents(), models.map(model => model.name));
          assert.equal(await page.locator(`#localInstalledModels [data-model="${models[0].id}"]`).getAttribute('aria-checked'), 'true');
          await fit(page, `${language}/${width}: model page fits`);
          if (language === 'zh') {
            await capture(page, `models-${model.id}-${width}`);
            if (process.env.ARTIFACT_DIR) await page.locator('#localInstalledModels').screenshot({ path: path.join(process.env.ARTIFACT_DIR, `installed-${model.id}-${width}.png`) });
          }
          await page.locator('#localModelChoices button').first().click();
          await page.evaluate(() => window.routerLocalOpen());
          assert.equal(await page.locator('#localModelChoices button').first().getAttribute('aria-pressed'), 'true', 'refresh keeps an explicit page choice');
          await page.locator('#nav [data-view="providers"]').click();
        }
      }
      assert.deepEqual(mutations, [], 'previewing a model must not change runtime intent');
    } finally { await close(page); }
  });

  await t.test('installed 1.7B switches select and load the exact model', async () => {
    const page = await pageFor('zh', 375), mutations = [];
    let state;
    try {
      await page.addInitScript(() => {
        const originalFetch = window.fetch;
        window.fetch = (url, options) => url !== '/api/local-model/events' ? originalFetch(url, options) : Promise.resolve(new Response(new ReadableStream({
          start(controller) { options.signal.addEventListener('abort', () => controller.error(new DOMException('Aborted', 'AbortError')), { once: true }); }
        }), { headers: { 'Content-Type': 'text/event-stream' } }));
      });
      await page.route('**/api/local-model', route => route.fulfill({ json: { ...original, status: state, models: models.map(model => ({ id: model.id, installed: true })) } }));
      await page.route('**/api/local-model/**', route => {
        const action = new URL(route.request().url()).pathname.slice('/api/local-model/'.length), body = route.request().postDataJSON();
        mutations.push({ action, body });
        if (action === 'select') state.intent.selected = body.model;
        if (action === 'enable') state.intent.enabled = true;
        if (action === 'disable') { state.intent.enabled = false; state.runtime = 'unloaded'; state.healthy = false; state.pid = 0; }
        if (action === 'load') { state.runtime = 'idle'; state.healthy = true; state.pid = 123; }
        return route.fulfill({ json: { schemaVersion: 1, status: state } });
      });
      for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) for (const width of [1440, 375]) {
        state = { ...original.status, installation: 'installed', runtime: 'idle', healthy: true, pid: 123, dependencies: { state: 'ready' }, intent: { selected: models[0].id, enabled: true } };
        await page.setViewportSize({ width, height: 1000 });
        await go(page, language, 'local-model');
        for (const model of small) {
          mutations.length = 0;
          await page.locator(`#localInstalledModels [data-model="${model.id}"]`).click();
          await page.waitForFunction(id => {
            const button = document.querySelector(`#localInstalledModels [data-model="${id}"]`);
            return button?.getAttribute('aria-checked') === 'true' && !button.disabled;
          }, model.id);
          assert.deepEqual(mutations.filter(item => item.action === 'select').map(item => item.body), [{ model: model.id }]);
          assert.deepEqual(mutations.map(item => item.action), ['disable', 'select', 'enable', 'load', 'check']);
          assert.equal(await page.locator('#localInstalledModels [aria-checked="true"]').count(), 1);
          assert.equal(await page.locator(`#localInstalledModels [data-model="${model.id}"]`).getAttribute('aria-label'), model.name);
        }
        await fit(page, `${language}/${width}: enabled model fits`);
      }
    } finally { await close(page); }
  });

  await t.test('uninstalled and unknown models stay out of all available-model lists', async () => {
    const page = await pageFor('en', 375);
    try {
      for (const installed of [false, undefined, true]) {
        await page.route('**/api/cloud-providers', route => route.fulfill({ json: { providers: [], localModels: models.map(model => ({ id: model.id.toLowerCase(), selector: model.selector, name: model.name, installed, enabled: false, status: 'ready' })) } }));
        await go(page, 'en', 'providers');
        await page.evaluate(() => window.routerProvidersOpen());
        assert.equal(await page.locator('#localProviders [data-local-model]').count(), installed === true ? 4 : 0, 'installation, rather than a stale status label, controls visibility');
        if (installed === true) assert.deepEqual(await page.locator('#localProviders .router-state').allTextContents(), Array(4).fill(await page.evaluate(() => t('Installed'))), 'disabled models are installed, not running');
        await go(page, 'en', 'agents'); await page.evaluate(() => window.routerAppsOpen());
        await page.locator('#agents [data-app="loomy"] .field.ag-menu').click();
        assert.equal(await page.locator('.am-mr[data-model^="local/"]').count(), installed === true ? 4 : 0);
        await page.keyboard.press('Escape');
        await go(page, 'en', 'routing');
        await page.waitForFunction(() => window.__weftoRoutingGroups?.state.localModels.length === 4);
        await page.locator('#newRoutingGroupBtn').click(); await page.locator('#rgAddModelBtn').click();
        assert.equal(await page.locator('#rgPickerList [data-picker-ref^="local/"]').count(), installed === true ? 4 : 0);
        await page.locator('#rgCancelBtn').click(); await page.unroute('**/api/cloud-providers');
      }
      for (const active of small) {
        await page.route('**/api/cloud-providers', route => route.fulfill({ json: { providers: [], localModels: models.map(model => ({ id: model.id.toLowerCase(), selector: model.selector, name: model.name, installed: true, selected: model.id === active.id, enabled: model.id === active.id, status: model.id === active.id ? 'idle' : 'unloaded' })) } }));
        await go(page, 'en', 'providers'); await page.evaluate(() => window.routerProvidersOpen());
        for (const model of models) assert.equal(await page.locator(`#localProviders [data-local-model="${model.id.toLowerCase()}"] .router-state`).textContent(), await page.evaluate(key => t(key), model.id === active.id ? 'Ready' : 'Installed'));
        await page.unroute('**/api/cloud-providers');
      }
    } finally { await close(page); }
  });

  await t.test('both small models can be selected, saved, searched and restored across applications and groups', async () => {
    const page = await pageFor('zh', 1440);
    try {
      for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) for (const width of [1440, 375, 320]) {
        await page.setViewportSize({ width, height: 1000 });
        await go(page, language, 'agents'); await page.evaluate(() => window.routerAppsOpen());
        for (const host of ['loomy', 'workbuddy']) {
          const row = page.locator(`#agents [data-app="${host}"]`);
          await row.locator('.field.ag-menu').click();
          const savedApp = (await api('/api/apps')).apps.find(app => app.id === host);
          if (savedApp.models?.length) {
            const clear = page.waitForResponse(response => new URL(response.url()).pathname === '/api/apps' && response.request().method() === 'POST');
            await page.locator('.am-reset').click(); assert.equal((await clear).status(), 200);
          }
          await page.locator('.am-search input').fill('1.7B');
          assert.deepEqual(await page.locator('.am-mr[data-model^="local/"] .am-mn').allTextContents(), small.map(model => model.name));
          for (const model of small) {
            const saved = page.waitForResponse(response => new URL(response.url()).pathname === '/api/apps' && response.request().method() === 'POST' && response.request().postDataJSON().models.includes(model.selector));
            await page.locator(`.am-mr[data-model="${model.selector}"]`).click(); assert.equal((await saved).status(), 200);
          }
          await fit(page, `${language}/${width}/${host}: picker fits`);
          if (language === 'zh') await capture(page, `picker-${host}-${width}`);
          await page.keyboard.press('Escape');
          assert.equal(await row.locator('.field.ag-menu .v').textContent(), small.map(model => model.name).join(', '));
          assert.equal(await row.locator('.field.ag-menu').getAttribute('title'), small.map(model => model.name).join('\n'));
          assert.ok((await row.locator('.who').textContent()).includes(small[0].name), 'application status names the quantization');
          await row.locator('.ag-link').click();
          assert.equal(await row.locator('.field.ag-pick .v').textContent(), small[0].name);
          assert.equal(await row.locator('.field.ag-pick').getAttribute('title'), small[0].name);
          await row.locator('.field.ag-pick').click();
          const reordered = page.waitForResponse(response => new URL(response.url()).pathname === '/api/apps' && response.request().method() === 'POST' && response.request().postDataJSON().models[0] === small[1].selector);
          await page.locator(`.am-mr[data-model="${small[1].selector}"] [data-move="up"]`).click();
          assert.equal((await reordered).status(), 200);
          await page.keyboard.press('Escape');
          assert.equal(await row.locator('.field.ag-pick .v').textContent(), small[1].name);
          assert.equal(await row.locator('.field.ag-pick').getAttribute('title'), small[1].name);
          await row.locator('.ag-link').click();
        }
        await page.reload(); await page.waitForFunction(() => !!serviceInfo && !!managementToken); await page.evaluate(() => window.routerAppsOpen());
        for (const host of ['loomy', 'workbuddy']) assert.equal(await page.locator(`#agents [data-app="${host}"] .field.ag-menu .v`).textContent(), [...small].reverse().map(model => model.name).join(', '));
        await go(page, language, 'routing');
        const header = await page.evaluate(() => ({ buttonBottom: document.querySelector('#newRoutingGroupBtn').getBoundingClientRect().bottom, introTop: document.querySelector('#routingIntroNote').getBoundingClientRect().top }));
        if (header.buttonBottom > header.introTop + 1) await capture(page, `routing-header-${language}-${width}`);
        assert.ok(header.buttonBottom <= header.introTop + 1, `${language}/${width}: routing actions overlap the introduction: ${JSON.stringify(header)}`);
        await page.locator('#newRoutingGroupBtn').click(); await page.locator('#rgName').fill(`Spark17 ${language} ${width}`);
        await page.locator('#rgAddModelBtn').click(); await page.locator('#rgPickerSearch').fill('1.7B');
        assert.deepEqual(await page.locator('#rgPickerList [data-picker-ref^="local/"] .v').allTextContents(), small.map(model => model.name));
        for (const model of small) await page.locator(`[data-picker-ref="${model.selector}"]`).click();
        await page.locator('#rgAddModelBtn').click();
        const saved = page.waitForResponse(response => new URL(response.url()).pathname === '/api/routing-groups' && response.request().method() === 'POST');
        await page.locator('#rgSaveBtn').click();
        const response = await saved; assert.equal(response.status(), 200); const draft = response.request().postDataJSON();
        assert.deepEqual(draft.members, small.map(model => model.selector));
        await page.locator('#routingGroupModal').waitFor({ state: 'hidden' });
        const card = page.locator(`.rt-group-card[data-group-id="${draft.id}"]`);
        for (const model of small) assert.ok((await card.textContent()).includes(model.name));
        await fit(page, `${language}/${width}: routing card fits`);
        if (language === 'zh') await capture(page, `routing-${width}`);
        await page.reload(); await page.locator(`[data-rg-edit="${draft.id}"]`).click();
        for (const model of small) assert.ok((await page.locator('#routingGroupModal').textContent()).includes(model.name));
        await page.locator('#rgCancelBtn').click();
      }
      for (const host of ['loomy', 'workbuddy']) {
        const response = await fetch(base + '/api/hosts/auto', { method: 'POST', headers, body: JSON.stringify({ host, enabled: true, models: [...small].reverse().map(model => model.selector) }) });
        assert.equal(response.status, 200, await response.text());
      }
      const loomyConfig = JSON.parse(await fs.readFile(path.join(loomy, 'opencode.json'), 'utf8'));
      for (const model of small) assert.equal(loomyConfig.provider.wefto.models[model.selector].name, model.name);
      assert.equal(loomyConfig.model, 'wefto/' + small[1].selector);
      const workbuddyModels = JSON.parse(await fs.readFile(path.join(workbuddy, 'models.json'), 'utf8')).models;
      for (const model of small) assert.equal(workbuddyModels.find(entry => entry.id === model.name)?.name, model.name);
      assert.equal(JSON.parse(await fs.readFile(path.join(workbuddy, 'settings.json'), 'utf8')).model, 'custom-local:' + small[1].name);
      const apps = (await api('/api/apps')).apps;
      assert.deepEqual(apps.find(app => app.id === 'workbuddy').modelAliases, Object.fromEntries(small.map(model => [model.name, model.selector])));
    } finally { await close(page); }
  });

  await t.test('an invalid system locale cannot disconnect the model console', async () => {
    for (const systemLocale of ['en-US@posix', 'en-INVALID-!', 'en-GB']) {
      const page = await pageFor('en', 375);
      try {
        await page.addInitScript(language => Object.defineProperty(navigator, 'language', { get: () => language }), systemLocale);
        await page.goto(base + '/?locale=en&view=providers');
        await page.waitForFunction(() => !!managementToken && !!serviceInfo && healthy, null, { timeout: 5000 });
        assert.notEqual(await page.locator('#started').textContent(), '—');
        assert.equal(await page.evaluate(() => routerIntlLang()), systemLocale === 'en-US@posix' ? 'en-US' : systemLocale === 'en-GB' ? 'en-GB' : 'en');
        await fit(page, systemLocale);
      } finally { await close(page); }
    }
  });
  assert.deepEqual(errors, []);
});
