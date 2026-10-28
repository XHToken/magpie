const { skipTutorial } = require('./returning-user.cjs');
const { workBuddyDisplayName } = require('./workbuddy-display.cjs');
// End-to-end Playwright suite for Stage 2 T2.2: Agent page refactor
// (Loomy / WorkBuddy one-click connect switch + routing group / model multi-select picker UI).
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const net = require('node:net');
const http = require('node:http');
const { spawn, execFileSync } = require('node:child_process');
const { once } = require('node:events');
const { chromium, webkit } = require('playwright');

const binary = process.env.WEFTO_BINARY || path.resolve(__dirname, '../../..', 'dist/wefto');

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ['chromium', 'webkit'])) {
  test(`agents page multi-select picker and one-click host switch (${engine})`, { timeout: 180000 }, async (t) => {
    const home = await fs.mkdtemp(path.join(os.tmpdir(), 'wefto-agents-'));
    let service, exited, browser;
    t.after(async () => {
      if (browser) await browser.close();
      if (service) {
        if (service.exitCode === null) service.kill('SIGTERM');
        await exited;
      }
      await fs.rm(home, { recursive: true, force: true });
    });

    const env = { ...process.env, HOME: home, USERPROFILE: home };
    for (const key of ['XDG_CONFIG_HOME', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME', 'APPDATA', 'LOCALAPPDATA']) {
      env[key] = path.join(home, key);
    }
    // Exercise Loomy's native default discovery, including macOS's actual
    // ~/.config/loomy-opencode layout, rather than hiding it behind an override.
    env.PUBLIC = path.join(home, 'Public');
    delete env.LOOMY_CONFIG_DIR;
    delete env.LOOMY_OPENCODE_DIR;
    const loomyRoot = process.platform === 'win32'
      ? path.join(env.PUBLIC, 'Loomy', 'fixture-identity', 'opencode')
      : path.join(home, '.config', 'loomy-opencode');
    const workBuddyRoot = path.join(home, 'workbuddy-config');
    await fs.mkdir(loomyRoot, { recursive: true });
    await fs.mkdir(workBuddyRoot, { recursive: true });
    const initialLoomyJson = JSON.stringify({ model: 'anthropic/claude-original', provider: { existing: { name: 'Existing' } } }, null, 2);
    const initialWbSettings = JSON.stringify({ model: 'original-wb-model', theme: 'dark' }, null, 2);
    await fs.writeFile(path.join(loomyRoot, 'opencode.json'), initialLoomyJson, 'utf8');
    await fs.writeFile(path.join(workBuddyRoot, 'settings.json'), initialWbSettings, 'utf8');
    env.WORKBUDDY_CONFIG_DIR = workBuddyRoot;

    const layout = JSON.parse(execFileSync(binary, ['dirs'], { env, encoding: 'utf8' }));
    execFileSync(binary, ['config', 'init'], { env });

    let generationRequests = 0;
    const upstream = http.createServer((req, res) => {
      if (req.url === '/v1/chat/completions') {
        generationRequests++;
        res.writeHead(500).end('unexpected generation probe');
        return;
      }
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ data: [{ id: 'gpt-4o' }, { id: 'gpt-4.1-mini' }] }));
    });
    upstream.listen(0, '127.0.0.1');
    await once(upstream, 'listening');
    t.after(() => new Promise(resolve => upstream.close(resolve)));
    const upstreamBase = `http://127.0.0.1:${upstream.address().port}/v1`;

    const reservation = net.createServer();
    reservation.listen(0, '127.0.0.1');
    await once(reservation, 'listening');
    const address = `127.0.0.1:${reservation.address().port}`;
    await new Promise(resolve => reservation.close(resolve));

    const configFile = path.join(layout.config, 'config.json');
    const config = JSON.parse(await fs.readFile(configFile, 'utf8'));
    config.listen.address = address;
    config.providers = {
      openai: { id: 'openai', revision: 1, name: 'OpenAI', preset: 'openai', protocol: 'chat', baseURL: upstreamBase, proxyMode: 'direct', credentialRef: 'cred_openai', discoveredModels: ['gpt-4o', 'gpt-4.1-mini'] }
    };
    config.localModels = [{ id: 'spark-x2.6-4b', installed: true, enabled: true, status: 'ready' }];
    config.groups = [{ id: 'coding-hybrid', name: '端云编程主力组', routing: 'order', members: ['local/spark-x2.6-4b', 'openai/gpt-4o'] }];
    await fs.writeFile(configFile, JSON.stringify(config), { mode: 0o600 });
    await fs.writeFile(path.join(layout.config, 'credentials.json'), JSON.stringify({
      schemaVersion: 1,
      providers: { cred_openai: { providerId: 'openai', apiKey: 'sk-test-agents', headers: {}, extraBody: {} } }
    }), { mode: 0o600 });

    service = spawn(binary, ['serve'], { env, stdio: ['ignore', 'pipe', 'pipe'] });
    exited = once(service, 'exit');
    let output = '';
    service.stdout.on('data', chunk => { output += chunk; });
    service.stderr.on('data', chunk => { output += chunk; });

    const tokenFile = path.join(layout.state, 'router.lock', 'management-token');
    let token;
    for (let i = 0; i < 200; i++) {
      if (service.exitCode !== null) assert.fail(`service exited: ${output}`);
      try { token = (await fs.readFile(tokenFile, 'utf8')).trim(); break; } catch {}
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    assert.ok(token, `no management token: ${output}`);

    browser = await (engine === 'webkit' ? webkit : chromium).launch({
      ...(engine === 'chromium' && process.env.CHROMIUM_EXECUTABLE ? { executablePath: process.env.CHROMIUM_EXECUTABLE } : {})
    });
    const page = await browser.newPage({ locale: 'zh-CN', viewport: { width: 1440, height: 900 } });
    const errors = [];
    page.on('pageerror', err => errors.push(err.message));

    await skipTutorial(page);
    await page.goto(`http://${address}/?locale=zh&view=agents`);
    await page.waitForFunction(() => !!managementToken && !!serviceInfo && document.querySelectorAll('#agents .row.agent.conn').length === 2);
    await page.evaluate(() => window.routerAppsOpen());

    // 1. Page purification assertions
    assert.equal(await page.locator('#agents .row.agent.conn').count(), 2);
    assert.deepEqual(await page.locator('#agents .row.agent.conn').evaluateAll(nodes => nodes.map(n => n.dataset.app)), ['loomy', 'workbuddy']);
    assert.equal(await page.locator('#agents [data-app="codex"], #agents [data-app="claude-code"], #agents [data-app="astudio"], #agents [data-app="dsh"], #addApp, #profileModal').count(), 0);
    for (const id of ['loomy', 'workbuddy']) {
      const row = page.locator(`#agents [data-app="${id}"]`);
      await row.locator('.ag-link').click();
      assert.equal(await row.locator('.ag-exp').isVisible(), true);
      assert.equal(await row.locator('.ag-pick').isVisible(), true);
      await row.locator('.ag-link').click();
      assert.equal(await row.locator('.ag-exp').count(), 0);
    }

    // 2. Multi-select popover & Routing Groups top section assertions on Loomy
    const loomyMenuBtn = page.locator('#agents [data-app="loomy"] .field.ag-menu');
    assert.equal(await loomyMenuBtn.evaluate(el => el.classList.contains('empty')), true);
    await loomyMenuBtn.click();
    const pop = page.locator('.pop.am-pop');
    await assert.doesNotReject(() => pop.waitFor({ state: 'visible' }));

    const sections = await pop.locator('.am-g').evaluateAll(nodes => nodes.map(n => n.dataset.section));
    assert.equal(sections[0], 'routing-groups', 'Routing groups section must be at the top');
    assert.ok(sections.includes('provider-openai'), 'Enabled cloud provider section must be present');
    assert.ok(sections.includes('local-models'), 'Local models section must be present');

    // Select 3 items: group/coding-hybrid, openai/gpt-4o, local/spark-x2.6-4b
    await pop.locator('.am-mr[data-model="group/coding-hybrid"]').click();
    await pop.locator('.am-mr[data-model="openai/gpt-4o"]').click();
    const offlineSave = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/apps');
    await pop.locator('.am-mr[data-model="local/spark-x2.6-4b"]').click();
    assert.equal((await offlineSave).status(), 200);

    // Button summary shows first 2 short names + "+1"
    assert.equal(await loomyMenuBtn.locator('.v').textContent(), 'coding-hybrid, gpt-4o +1');
    assert.equal(await loomyMenuBtn.evaluate(el => el.classList.contains('empty')), false);

    // Search filter & Show all / Hide all
    await pop.locator('.am-search input').fill('gpt-4.1');
    assert.equal(await pop.locator('.am-mr').count(), 1);
    assert.equal(await pop.locator('.am-mr').getAttribute('data-model'), 'openai/gpt-4.1-mini');
    await pop.locator('.am-clear').click();
    await pop.locator('[data-action="hide-all"]').click();
    assert.equal(await pop.locator('.am-g.fold').count(), sections.length);
    await pop.locator('[data-action="show-all"]').click();
    assert.equal(await pop.locator('.am-g.fold').count(), 0);

    // Close popover by clicking outside
    await page.locator('#view-agents .row-head .label').click();
    assert.equal(await page.locator('.pop.am-pop').count(), 0);

    // 3. Loomy one-click switch ON (off -> on) & OFF (on -> off)
    const loomySwitch = page.locator('#agents [data-app="loomy"] .lib-switch.ag-conn');
    assert.equal(await loomySwitch.getAttribute('aria-checked'), 'false');
    const loomyConnectResp = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/hosts/auto');
    await loomySwitch.click();
    const loomyConnected = await loomyConnectResp;
    assert.equal(loomyConnected.status(), 200, await loomyConnected.text());
    await page.waitForFunction(() => document.querySelector('#agents [data-app="loomy"] .lib-switch.ag-conn').getAttribute('aria-checked') === 'true');
    assert.equal(generationRequests, 0, 'connecting Loomy must not trigger upstream generation probe');

    const loomyCfg = JSON.parse(await fs.readFile(path.join(loomyRoot, 'opencode.json'), 'utf8'));
    assert.equal(loomyCfg.model, 'wefto/group/coding-hybrid');
    assert.deepEqual(Object.keys(loomyCfg.provider.wefto.models).sort(), ['group/coding-hybrid', 'local/spark-x2.6-4b', 'openai/gpt-4o']);
    assert.equal(loomyCfg.provider.wefto.models['group/coding-hybrid'].name, '端云编程主力组');
    assert.equal(loomyCfg.provider.wefto.models['openai/gpt-4o'].name, 'gpt-4o · OpenAI');
    assert.equal(loomyCfg.provider.wefto.models['local/spark-x2.6-4b'].name, 'Spark-X2.6-4B-Q4_K_M');

    // A failed clear must retain the connected selection and configuration.
    await page.route('**/api/hosts/auto', async route => {
      if (route.request().method() === 'POST') await route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: { code: 'host_operation_failed', message: 'fixture save failure' } }) });
      else await route.continue();
    });
    await loomyMenuBtn.click();
    const failedClear = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/hosts/auto');
    await page.locator('.am-foot button').filter({ hasText: '清空已选' }).click();
    assert.equal((await failedClear).status(), 409);
    await page.waitForFunction(() => document.querySelector('#agents [data-app="loomy"] .field.ag-menu').textContent.includes('coding-hybrid'));
    assert.equal(await loomySwitch.getAttribute('aria-checked'), 'true');
    assert.deepEqual(JSON.parse(await fs.readFile(path.join(loomyRoot, 'opencode.json'), 'utf8')), loomyCfg);
    await page.keyboard.press('Escape');
    await page.unroute('**/api/hosts/auto');

    // Turn OFF Loomy switch -> restores original opencode.json & preserves selected models in UI
    const loomyDisconnectResp = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/hosts/auto');
    await loomySwitch.click();
    assert.equal((await loomyDisconnectResp).status(), 200);
    await page.waitForFunction(() => document.querySelector('#agents [data-app="loomy"] .lib-switch.ag-conn').getAttribute('aria-checked') === 'false');
    const restoredLoomyCfg = JSON.parse(await fs.readFile(path.join(loomyRoot, 'opencode.json'), 'utf8'));
    assert.equal(restoredLoomyCfg.model, 'anthropic/claude-original');
    assert.equal(Object.hasOwn(restoredLoomyCfg.provider || {}, 'wefto'), false);
    assert.equal(await loomyMenuBtn.locator('.v').textContent(), 'coding-hybrid, gpt-4o +1');

    // 4. WorkBuddy empty-model guard, multi-select reorder, one-click switch ON & OFF
    const wbSwitch = page.locator('#agents [data-app="workbuddy"] .lib-switch.ag-conn');
    const wbMenuBtn = page.locator('#agents [data-app="workbuddy"] .field.ag-menu');
    // Turning ON with 0 models selected keeps switch OFF and opens the model picker automatically
    await wbSwitch.click();
    assert.equal(await wbSwitch.getAttribute('aria-checked'), 'false');
    await assert.doesNotReject(() => page.locator('.pop.am-pop[data-app="workbuddy"]').waitFor({ state: 'visible' }));
    assert.match(await page.locator('#appStatus').textContent(), /选择至少一个路由组或模型|Select at least one/);

    // Select group/coding-hybrid and openai/gpt-4o, then close popover and turn switch ON
    await page.locator('.pop.am-pop .am-mr[data-model="group/coding-hybrid"]').click();
    await page.locator('.pop.am-pop .am-mr[data-model="openai/gpt-4o"]').click();
    await page.locator('#view-agents .row-head .label').click();

    const wbConnectResp = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/hosts/auto');
    await wbSwitch.click();
    assert.equal((await wbConnectResp).status(), 200);
    await page.waitForFunction(() => document.querySelector('#agents [data-app="workbuddy"] .lib-switch.ag-conn').getAttribute('aria-checked') === 'true');
    assert.equal(generationRequests, 0);

    let wbModels = JSON.parse(await fs.readFile(path.join(workBuddyRoot, 'models.json'), 'utf8'));
    let wbSettings = JSON.parse(await fs.readFile(path.join(workBuddyRoot, 'settings.json'), 'utf8'));
    assert.deepEqual(wbModels.models.map(m => m.id), ['端云编程主力组', 'gpt-4o · OpenAI']);
    assert.ok(wbModels.models.every(m => m.id === m.name), 'WorkBuddy must not append routing IDs to labels');
    assert.deepEqual(wbModels.models.map(workBuddyDisplayName), ['端云编程主力组', 'gpt-4o · OpenAI']);
    assert.equal(wbSettings.model, 'custom-local:端云编程主力组');

    // Reorder while connected: move openai/gpt-4o up to index 0 (Primary)
    await wbMenuBtn.click();
    const reorderResp = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/hosts/auto');
    await page.locator('.pop.am-pop .am-mr[data-model="openai/gpt-4o"] [data-move="up"]').click();
    assert.equal((await reorderResp).status(), 200);
    wbSettings = JSON.parse(await fs.readFile(path.join(workBuddyRoot, 'settings.json'), 'utf8'));
    assert.equal(wbSettings.model, 'custom-local:gpt-4o · OpenAI');
    await page.locator('#view-agents .row-head .label').click();

    // Disconnect WorkBuddy -> restores original settings.json and preserves selected models
    const wbDisconnectResp = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/hosts/auto');
    await wbSwitch.click();
    assert.equal((await wbDisconnectResp).status(), 200);
    await page.waitForFunction(() => document.querySelector('#agents [data-app="workbuddy"] .lib-switch.ag-conn').getAttribute('aria-checked') === 'false');
    wbSettings = JSON.parse(await fs.readFile(path.join(workBuddyRoot, 'settings.json'), 'utf8'));
    assert.equal(wbSettings.model, 'original-wb-model');
    assert.equal(await wbMenuBtn.locator('.v').textContent(), 'gpt-4o, coding-hybrid');

    // 5. 409 host_config_unavailable keeps switch OFF and shows friendly prompt in #appStatus
    await page.route('**/api/hosts/auto', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: { code: 'host_config_unavailable', message: 'host configuration directory was not found' } }) });
      } else {
        await route.continue();
      }
    });
    await wbSwitch.click();
    await page.waitForFunction(() => /未检测到宿主配置目录|Host configuration directory was not found/.test(document.querySelector('#appStatus').textContent));
    assert.equal(await wbSwitch.getAttribute('aria-checked'), 'false');
    await page.unroute('**/api/hosts/auto');

    // Reproduce Loomy's stale model menu after its 端云协同 Profile was cleared.
    await loomySwitch.click();
    await page.waitForFunction(() => document.querySelector('#agents [data-app="loomy"] .lib-switch.ag-conn').getAttribute('aria-checked') === 'true');
    const authHeaders = { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' };
    const appList = await (await fetch(`http://${address}/api/apps`, { headers: authHeaders })).json();
    const savedLoomy = appList.apps.find(a => a.id === 'loomy');
    const cleared = await fetch(`http://${address}/api/apps`, { method: 'POST', headers: authHeaders, body: JSON.stringify({ id: 'loomy', revision: savedLoomy.revision, enabled: false, models: [], cloud: { providerId: '', model: '' } }) });
    assert.equal(cleared.status, 200);
    const orphanedConfig = await fs.readFile(path.join(loomyRoot, 'opencode.json'), 'utf8');
    for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) {
      await page.goto(`http://${address}/?locale=${language}&view=agents`);
      await page.waitForFunction(() => !!managementToken && !!serviceInfo);
      await page.evaluate(() => window.routerAppsOpen());
      assert.equal(await page.locator('#agents [data-app="loomy"] .lib-switch.ag-conn').getAttribute('aria-checked'), 'false');
      assert.equal(await page.locator('#agents [data-app="loomy"] .router-state').textContent(), await page.evaluate(() => t('Reconnect required')));
      assert.equal(await page.locator('#agents [data-app="loomy"] .field.ag-menu').getAttribute('title'), await page.evaluate(() => t('Choose models')));
      assert.equal(await fs.readFile(path.join(loomyRoot, 'opencode.json'), 'utf8'), orphanedConfig, 'inspection must not rewrite the host');
    }

    // Long provider IDs and wrapped model names must fit their own rows.
    const providerId = 'p_061adbf19e0d938b7bdec787c296c9a2';
    const modelNames = ['spark-x2.5-4B', 'spark-x2.5-1.7b', 'deepseek-reasoner-with-a-long-model-name'];
    await page.route('**/api/cloud-providers', route => route.fulfill({ json: {
      providers: [{ id: providerId, name: '讯飞星辰 MaaS', enabled: true, models: modelNames }], localModels: [],
    } }));
    await page.route('**/api/apps', async route => {
      assert.equal(route.request().method(), 'GET', 'layout inspection must not save selections');
      const response = await route.fetch(), data = await response.json();
      for (const app of data.apps) app.models = modelNames.map(name => `${providerId}/${name}`);
      await route.fulfill({ response, json: data });
    });
    for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) {
      for (const width of [1440, 375]) {
        await page.setViewportSize({ width, height: width === 375 ? 667 : 900 });
        await page.goto(`http://${address}/?locale=${language}&view=agents`);
        await page.waitForFunction(() => !!managementToken && !!serviceInfo);
        await page.evaluate(() => window.routerAppsOpen());
        for (const host of ['loomy', 'workbuddy']) {
          await page.locator(`#agents [data-app="${host}"] .field.ag-menu`).click();
          await page.locator(`.am-mr[data-model="${providerId}/${modelNames[0]}"] .am-order`).waitFor();
          await page.locator('.am-pop').evaluate(pop => Promise.all(pop.getAnimations().map(animation => animation.finished)));
          const issues = await page.locator('.am-pop').evaluate(pop => {
            const issues = [], bounds = pop.getBoundingClientRect();
            if (bounds.left < 0 || bounds.right > innerWidth + 1 || bounds.bottom > innerHeight + 1) issues.push('popover outside viewport');
            for (const row of pop.querySelectorAll('.am-mr')) {
              const box = row.getBoundingClientRect(), name = row.querySelector('.am-mn');
              const range = document.createRange(); range.selectNodeContents(name);
              for (const text of range.getClientRects()) {
                if (text.top < box.top - 1 || text.bottom > box.bottom + 1) issues.push(`model text outside row: ${row.dataset.model}`);
              }
              const children = [...row.children].map(child => child.getBoundingClientRect());
              for (let i = 1; i < children.length; i++) {
                if (children[i].left < children[i - 1].right - 1 || children[i].right > box.right + 1) issues.push(`overlapping columns: ${row.dataset.model}`);
              }
            }
            return issues;
          });
          assert.deepEqual(issues, [], `${language}/${width}/${host}: model rows stay aligned`);
          if (process.env.ARTIFACT_DIR && language === 'zh' && host === 'loomy') {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `agent-model-layout-${width}.png`), fullPage: true });
          }
          await page.locator('.am-pop [data-action="hide-all"]').click();
          assert.equal(await page.locator('.am-pop .am-mr:visible').count(), 0, 'folded sections hide their rows');
          const folded = await page.locator('.am-pop .am-gh').evaluateAll(heads => heads.map(head => {
            const box = head.getBoundingClientRect(); return { left: box.left, right: box.right, top: box.top, bottom: box.bottom };
          }));
          for (let i = 1; i < folded.length; i++) {
            assert.ok(Math.abs(folded[i].left - folded[0].left) < 1 && Math.abs(folded[i].right - folded[0].right) < 1, 'folded headers keep the same columns');
            assert.ok(folded[i].top >= folded[i - 1].bottom, 'folded headers remain in separate rows');
          }
          await page.keyboard.press('Escape');
        }
      }
    }
    assert.deepEqual(errors, []);
    await page.close();
  });
}
