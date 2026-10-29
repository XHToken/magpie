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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ['chromium', 'webkit'])) {
  test(`local-model page omits redaction diagnostics and preserves intent (${engine})`, { timeout: 180000 }, async t => {
    const home = await fs.mkdtemp(path.join(os.tmpdir(), 'wefto-redaction-ui-'));
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
    // Saved intent must survive visiting the simplified local-model page.
    config.appRevision = 2;
    for (const [index, id] of ['codex', 'dsh'].entries()) config.apps[id] = { schemaVersion: 1, id, revision: index + 1, name: id, cloud: { providerId: '', model: '' }, credentialMode: 'stored', localRuntime: 'shared', redactionEnabled: true, referencePrice: null };
    await fs.writeFile(configFile, JSON.stringify(config), { mode: 0o600 });
    service = spawn(binary, ['serve', '--no-experimental-redaction'], { env, stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true }); exited = once(service, 'exit');
    let output = ''; service.stdout.on('data', chunk => { output += chunk; }); service.stderr.on('data', chunk => { output += chunk; });
    let token;
    for (let i = 0; i < 200; i++) {
      if (service.exitCode !== null) assert.fail(output);
      try { token = (await fs.readFile(path.join(layout.state, 'router.lock', 'management-token'), 'utf8')).trim(); break; } catch {}
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    assert.ok(token);
    const state = async () => (await (await fetch(base + '/api/redaction', { headers: { Authorization: 'Bearer ' + token } })).json());
    assert.equal((await fetch(base + '/api/redaction')).status, 401);
    const initial = await state();
    assert.equal(initial.capability, 'unknown'); assert.equal(initial.productionIntegrated, true); assert.equal(initial.modelReady, false); assert.equal(initial.recognitionReady, false); assert.equal(initial.statsAvailable, true);
    assert.equal(initial.intents.filter(p => p.enabled).length, 2);
    assert.ok(initial.intents.every(p => p.active === false));
    assert.equal((await fetch(base + '/api/redaction', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: JSON.stringify({ edits: [{ id: 'loomy', revision: 0, enabled: true }] }) })).status, 409);
    browser = await (engine === 'webkit' ? webkit : chromium).launch();
    const errors = [];
    for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) {
      for (const width of [1600, 375]) {
        const page = await browser.newPage({ locale: 'en-US', viewport: { width, height: 950 } });
        page.on('pageerror', error => errors.push(error.message));
        const external = [], privacyRequests = [];
        page.on('request', req => {
          const url = new URL(req.url());
          if (url.origin !== base) external.push(req.url());
          if (url.pathname === '/api/redaction' || url.pathname === '/redaction.js') privacyRequests.push(url.pathname);
        });
        await skipTutorial(page); await page.goto(`${base}/?locale=${language}&view=local-model`);
        await page.waitForFunction(() => !!managementToken && !!serviceInfo && document.querySelector('#localModelContent').children.length > 0);
        assert.equal(await page.locator('#redactionPanel, #redactionSetup, #redactionStatus, #redactionDisableAll').count(), 0, 'the diagnostic module is removed');
        assert.equal(await page.locator('script[src="/redaction.js"]').count(), 0);
        await page.locator('#sync').click();
        await page.waitForFunction(() => !busy);
        await page.locator('#nav [data-view=agents]').click();
        await page.waitForFunction(() => !document.querySelector('#view-agents').hidden);
        await page.locator('#agents [data-app="loomy"] .ag-link').click();
        const details = page.locator('#agents [data-app="loomy"] .ag-exp');
        await details.waitFor({ state: 'visible' });
        const titles = { zh: '用户输入脱敏', en: 'User input redaction', 'zh-TW': '使用者輸入去識別化', ja: 'ユーザー入力の匿名化', de: 'Anonymisierung der Benutzereingabe' };
        assert.equal(await details.getByRole('switch', { name: titles[language], exact: true }).count(), 0, 'redaction is configured in routing groups');
        assert.equal(await details.getByText(titles[language], { exact: true }).count(), 0, 'application details omit the redaction setting');
        if (process.env.ARTIFACT_DIR && ['zh','en'].includes(language)) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `application-details-${engine}-${language}-${width}.png`), fullPage: true });
        }

        await page.locator('#nav [data-view=local-model]').click();
        await page.waitForFunction(() => !document.querySelector('#view-local-model').hidden && document.querySelector('#localModelContent').children.length > 0);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, `${language} ${width}: overflow`);
        assert.equal((await page.content()).includes(token), false);
        assert.equal(await page.evaluate(token => JSON.stringify({ ...localStorage, ...sessionStorage }).includes(token), token), false);
        assert.deepEqual(privacyRequests, [], 'navigation and refresh do not fetch the removed module');
        assert.deepEqual(external, []);
        if (process.env.ARTIFACT_DIR && ['zh', 'en'].includes(language)) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `local-model-${engine}-${language}-${width}.png`), fullPage: true });
        }
        await page.close();
      }
    }
    assert.deepEqual((await state()).intents, initial.intents, 'viewing the page preserves saved intent and effective state');
    const persisted = JSON.parse(await fs.readFile(configFile, 'utf8'));
    assert.equal(persisted.apps.codex.redactionEnabled, true); assert.equal(persisted.apps.dsh.redactionEnabled, true);
    assert.deepEqual(errors, []);
    assert.equal(output.includes(token), false);
  });
}
