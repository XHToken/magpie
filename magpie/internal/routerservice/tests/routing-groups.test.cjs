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
test(`routing groups list, editor and persistence (${engine})`, { timeout: 120000 }, async (t) => {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), 'wefto-rg-ui-'));
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
  env.WORKBUDDY_CONFIG_DIR = path.join(home, 'workbuddy');

  const layout = JSON.parse(execFileSync(binary, ['dirs'], { env, encoding: 'utf8' }));
  execFileSync(binary, ['config', 'init'], { env });

  const reservation = net.createServer();
  reservation.listen(0, '127.0.0.1');
  await once(reservation, 'listening');
  const address = `127.0.0.1:${reservation.address().port}`;
  await new Promise(resolve => reservation.close(resolve));

  const configFile = path.join(layout.config, 'config.json');
  const config = JSON.parse(await fs.readFile(configFile, 'utf8'));
  config.listen.address = address;
  config.providers = {
    openai: {
      id: 'openai',
      revision: 1,
      name: 'OpenAI',
      preset: 'openai',
      protocol: 'chat',
      baseURL: 'https://api.openai.com/v1',
      proxyMode: 'direct',
      credentialRef: 'cred_openai',
      discoveredModels: ['gpt-4o', 'deepseek-chat'],
    },
    deepseek: {
      id: 'deepseek',
      revision: 1,
      name: 'DeepSeek',
      preset: 'deepseek',
      protocol: 'chat',
      baseURL: 'https://api.deepseek.com/v1',
      proxyMode: 'direct',
      credentialRef: 'cred_deepseek',
      discoveredModels: ['deepseek-chat'],
    },
  };
  config.localModels = [{ id: 'spark-x2.6-4b', installed: true, enabled: true, status: 'ready' }];
  config.groups = [
    { id: 'private-child', name: 'Private Child', localRedaction: true, members: ['local/spark-x2.6-4b', 'openai/gpt-4o'] },
    { id: 'private-parent', name: 'Private Parent', members: ['group/private-child'], localRedaction: false },
    { id: 'missing-redactor', name: 'Missing Redactor', members: ['openai/gpt-4o'], localRedaction: true },
    {
      id: 'coding',
      name: 'Coding Group',
      routing: 'order',
      members: ['local/spark-x2.6-4b', 'openai/gpt-4o'],
    },
  ];
  await fs.writeFile(configFile, JSON.stringify(config, null, 2), { mode: 0o600 });

  const credFile = path.join(layout.config, 'credentials.json');
  const creds = {
    schemaVersion: 1,
    providers: {
      cred_openai: { providerId: 'openai', apiKey: 'sk-test-openai', headers: {}, extraBody: {} },
      cred_deepseek: { providerId: 'deepseek', apiKey: 'sk-test-deepseek', headers: {}, extraBody: {} },
    },
  };
  await fs.writeFile(credFile, JSON.stringify(creds, null, 2), { mode: 0o600 });

  service = spawn(binary, ['serve'], { env, stdio: ['ignore', 'pipe', 'pipe'] });
  exited = once(service, 'exit');
  let output = '';
  service.stdout.on('data', chunk => { output += chunk; });
  service.stderr.on('data', chunk => { output += chunk; });

  const tokenFile = path.join(layout.state, 'router.lock', 'management-token');
  for (let i = 0; i < 200; i++) {
    if (service.exitCode !== null) assert.fail(`service exited early: ${output}`);
    try {
      if ((await fs.readFile(tokenFile, 'utf8')).trim()) break;
    } catch {}
    await new Promise(resolve => setTimeout(resolve, 25));
  }

  browser = await (engine === 'webkit' ? webkit : chromium).launch({
    ...(process.env.CHROMIUM_EXECUTABLE ? { executablePath: process.env.CHROMIUM_EXECUTABLE } : {}),
  });
  const page = await browser.newPage({ locale: 'zh-CN' });
  const pageErrors = [];
  page.on('pageerror', err => pageErrors.push(err.message));

  const apiCalls = [];
  page.on('request', req => {
    const url = new URL(req.url());
    if (url.pathname.startsWith('/api/routing-groups')) {
      let body = null;
      try { body = req.postDataJSON(); } catch {}
      apiCalls.push({ method: req.method(), path: url.pathname + url.search, body });
    }
  });

  await skipTutorial(page);
  await page.goto(`http://${address}/?locale=zh&view=routing`);
  await page.waitForFunction(() => document.querySelector('#service')?.classList.contains('ok'));

  // 1. 双分区渲染测试：自定义路由组 + 同名模型自动路由组
  const codingCard = page.locator('#customRoutingGroupsList .rt-group-card[data-group-id="coding"]');
  await codingCard.waitFor({ state: 'visible' });
  assert.equal(await codingCard.locator('[data-copy-call="group/coding"]').textContent(), 'group/coding');
  assert.equal(await codingCard.locator('.rt-strategy-tag').isVisible(), true);
  assert.equal(await codingCard.locator('.rt-member-chip.is-local[data-member="local/spark-x2.6-4b"]').isVisible(), true);
  assert.equal(await codingCard.locator('[data-member="local/spark-x2.6-4b"] > span').first().textContent(), 'Spark-X2.6-4B-Q4_K_M');
  assert.equal(await codingCard.locator('.rt-member-chip[data-member="openai/gpt-4o"]').isVisible(), true);

  const autoCard = page.locator('#autoRoutingGroupsList .rt-group-card[data-group-id="auto-deepseek-chat"]');
  // The parent's own switch is off, but its effective privacy requirement is inherited.
  for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) {
    await page.goto(`http://${address}/?locale=${language}&view=routing`);
    const note = page.locator('[data-group-privacy="private-parent"]');
    await note.waitFor({ state: 'visible' });
    const expected = await page.evaluate(() => window.__weftoRoutingGroups.tr('Inherited redaction: all cloud candidates require redaction.'));
    assert.equal(await note.textContent(), expected);
    if (language !== 'en') assert.notEqual(expected, 'Inherited redaction: all cloud candidates require redaction.');
  }
  await page.goto(`http://${address}/?locale=zh&view=routing`);
  await page.locator('[data-rg-edit="private-parent"]').click();
  await page.waitForFunction(() => document.querySelector('#rgPrivacySummary')?.textContent.includes('已继承'));
  assert.equal(await page.locator('[data-rg-local-redaction="0"]').evaluate(node => node.classList.contains('on')), true);
  await page.locator('#rgSaveBtn').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'hidden' });
  await page.locator('[data-rg-edit="missing-redactor"]').click();
  await page.waitForFunction(() => document.querySelector('#rgPrivacySummary')?.textContent.includes('缺少'));
  assert.equal(await page.locator('[data-rg-local-redaction="1"]').evaluate(node => node.classList.contains('on')), true);
  await page.locator('#rgSaveBtn').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'hidden' });
  assert.match(await page.locator('[data-group-privacy="missing-redactor"]').textContent(), /缺少/);
  await autoCard.waitFor({ state: 'visible' });
  assert.equal(await autoCard.locator('.rt-group-title').textContent(), 'deepseek-chat');
  assert.equal(await autoCard.locator('[data-copy-call="group/auto-deepseek-chat"]').textContent(), 'group/auto-deepseek-chat');
  assert.equal(await autoCard.locator('.rt-member-chip').count(), 2);
  assert.equal(await autoCard.locator('[data-member="openai/deepseek-chat"]').isVisible(), true);
  assert.equal(await autoCard.locator('[data-member="deepseek/deepseek-chat"]').isVisible(), true);

  // 2. 新建端云混合路由组 + 智能默认策略 + 本地模型脱敏选项测试
  await page.locator('#newRoutingGroupBtn').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#rgPatternsList, #rgPatternNewInput, #rgAddPatternBtn, .rt-pats').count(), 0);
  assert.equal(await page.locator('#routingGroupModal [data-rg-strategy="smart_hybrid"]').evaluate(node => node.classList.contains('on')), true);
  assert.equal(await page.locator('#rgLocalRedactionLabel').isHidden(), true);
  assert.equal(await page.locator('#rgLocalRedactionSection').isHidden(), true);

  await page.locator('#rgName').fill('Hybrid Coding');
  assert.equal(await page.locator('#rgCallBadge').textContent(), 'group/hybrid-coding');

  await page.locator('#rgAddModelBtn').click();
  await page.locator('#rgModelPicker').waitFor({ state: 'visible' });
  const localCand = page.locator('#rgPickerList [data-picker-ref="local/spark-x2.6-4b"]');
  const cloudCand = page.locator('#rgPickerList [data-picker-ref="openai/gpt-4o"]');
  await localCand.waitFor({ state: 'visible' });
  assert.equal(await localCand.locator('.v').textContent(), 'Spark-X2.6-4B-Q4_K_M');
  await cloudCand.waitFor({ state: 'visible' });
  assert.deepEqual(await page.locator('#rgPickerList [data-picker-ref^="local/"]').evaluateAll(rows => rows.map(row => row.dataset.pickerRef)), ['local/spark-x2.6-4b'], 'the group picker only offers the installed Q4 model');

  await cloudCand.click();
  assert.equal(await page.locator('#rgLocalRedactionLabel').isHidden(), true);
  await localCand.click();
  await page.locator('#rgAddModelBtn').click();
  await page.locator('#rgModelPicker').waitFor({ state: 'hidden' });

  // Adding a local/* model reveals the "本地模型脱敏" row with "开" and "关" options
  await page.locator('#rgLocalRedactionLabel').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#rgLocalRedactionLabel').textContent(), '用户输入脱敏');
  const redactOnBtn = page.locator('#rgLocalRedactionSection [data-rg-local-redaction="1"]');
  const redactOffBtn = page.locator('#rgLocalRedactionSection [data-rg-local-redaction="0"]');
  assert.equal(await redactOnBtn.textContent(), '开');
  assert.equal(await redactOffBtn.textContent(), '关');
  assert.equal(await redactOffBtn.evaluate(node => node.classList.contains('on')), true);
  await redactOnBtn.click();
  assert.equal(await redactOnBtn.evaluate(node => node.classList.contains('on')), true);

  // Smart hybrid is the explicit default for a new mixed local/cloud group.
  assert.equal(await page.locator('#routingGroupModal [data-rg-strategy="smart_hybrid"]').evaluate(node => node.classList.contains('on')), true);

  apiCalls.length = 0;
  await page.locator('#rgSaveBtn').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'hidden' });

  const createCall = apiCalls.find(c => c.method === 'POST' && c.path === '/api/routing-groups');
  assert.ok(createCall, 'expected POST /api/routing-groups');
  assert.equal(createCall.body.id, 'hybrid-coding');
  assert.equal(createCall.body.routing, 'smart_hybrid');
  assert.equal(createCall.body.localRedaction, true);
  assert.deepEqual(createCall.body.members, ['openai/gpt-4o', 'local/spark-x2.6-4b']);
  assert.deepEqual(createCall.body.match, []);
  assert.deepEqual(createCall.body.patterns, []);

  const hybridCard = page.locator('#customRoutingGroupsList .rt-group-card[data-group-id="hybrid-coding"]');
  await hybridCard.waitFor({ state: 'visible' });
  assert.equal(await hybridCard.locator('.rt-strategy-tag').isVisible(), true);

  // 3. 同名自动路由组一键固定（Pin）测试
  apiCalls.length = 0;
  await autoCard.locator('[data-rg-pin="auto-deepseek-chat"]').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'visible' });
  await page.locator('#rgSaveBtn').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'hidden' });
  const pinnedCard = page.locator('#customRoutingGroupsList .rt-group-card[data-group-id="deepseek-chat"]');
  await pinnedCard.waitFor({ state: 'visible' });
  const pinCall = apiCalls.find(c => c.method === 'POST' && c.path === '/api/routing-groups');
  assert.ok(pinCall, 'expected POST /api/routing-groups when pinning auto group');
  assert.equal(pinCall.body.id, 'deepseek-chat');
  assert.equal(await pinnedCard.locator('.rt-member-chip').count(), 2);

  // 4. 一键启停与手动切换成员测试
  apiCalls.length = 0;
  await hybridCard.locator('[data-rg-toggle="hybrid-coding"]').click();
  await page.waitForFunction(() => document.querySelector('#customRoutingGroupsList .rt-group-card[data-group-id="hybrid-coding"]')?.classList.contains('is-disabled'));
  const switchOffCall = apiCalls.find(c => c.method === 'POST' && c.path === '/api/routing-groups/switch');
  assert.ok(switchOffCall, 'expected POST /api/routing-groups/switch');
  assert.deepEqual(switchOffCall.body, { id: 'hybrid-coding', enabled: false });

  await hybridCard.locator('[data-rg-toggle="hybrid-coding"]').click();
  await page.waitForFunction(() => !document.querySelector('#customRoutingGroupsList .rt-group-card[data-group-id="hybrid-coding"]')?.classList.contains('is-disabled'));

  // 设为 manual 策略后在列表中点击切换 active 成员（同时验证编辑态下本地模型脱敏保持“开”）
  await hybridCard.locator('[data-rg-edit="hybrid-coding"]').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#rgLocalRedactionSection [data-rg-local-redaction="1"]').evaluate(node => node.classList.contains('on')), true);
  await page.locator('#routingGroupModal [data-rg-strategy="manual"]').click();
  await page.locator('#rgSaveBtn').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'hidden' });

  apiCalls.length = 0;
  await hybridCard.locator('[data-member="openai/gpt-4o"]').click();
  await page.waitForFunction(() => document.querySelector('#customRoutingGroupsList .rt-group-card[data-group-id="hybrid-coding"] [data-member="openai/gpt-4o"]')?.classList.contains('is-active'));
  const manualSwitchCall = apiCalls.find(c => c.method === 'POST' && c.path === '/api/routing-groups');
  assert.ok(manualSwitchCall, 'expected POST /api/routing-groups when switching active member');
  assert.equal(manualSwitchCall.body.active, 'openai/gpt-4o');

  // 5. Editor timing, affinity, context and matching options persist exactly.
  const existingPatterns = ['gpt-4*', 'claude-*'];
  await page.evaluate(async ({ id, patterns }) => {
    const rg = window.__weftoRoutingGroups;
    const group = rg.state.groups.find(g => g.id === id);
    await rg.apiRequest('/api/routing-groups', { method: 'POST', body: { ...group, match: patterns, patterns } });
    await rg.loadRoutingGroups(true);
  }, { id: 'hybrid-coding', patterns: existingPatterns });
  await hybridCard.locator('[data-rg-edit="hybrid-coding"]').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#rgPatternsList, #rgPatternNewInput, #rgAddPatternBtn, .rt-pats').count(), 0);
  await page.locator('#rgFirstTokenInput').fill('15');
  await page.locator('#rgPatienceInput').fill('2');
  await page.locator('[data-rg-aff="session"]').click();
  await page.locator('#rgStickinessInput').fill('10');
  await page.locator('#rgContextInput').fill('32k');

  apiCalls.length = 0;
  await page.locator('#rgSaveBtn').click();
  await page.locator('#routingGroupModal').waitFor({ state: 'hidden' });
  const advSaveCall = apiCalls.find(c => c.method === 'POST' && c.path === '/api/routing-groups');
  assert.ok(advSaveCall);
  assert.equal(advSaveCall.body.firstToken, 15);
  assert.equal(advSaveCall.body.patience, 120);
  assert.equal(advSaveCall.body.affinity, 'session');
  assert.equal(advSaveCall.body.stickiness, 10);
  assert.equal(advSaveCall.body.context, 32000);
  assert.deepEqual(advSaveCall.body.match, existingPatterns);
  assert.deepEqual(advSaveCall.body.patterns, existingPatterns);

  apiCalls.length = 0;
  page.once('dialog', dialog => dialog.accept());
  await hybridCard.locator('[data-rg-delete="hybrid-coding"]').click();
  await page.waitForFunction(() => !document.querySelector('#customRoutingGroupsList .rt-group-card[data-group-id="hybrid-coding"]'));
  const deleteCall = apiCalls.find(c => c.method === 'DELETE' && c.path === '/api/routing-groups?id=hybrid-coding');
  assert.ok(deleteCall, 'expected DELETE /api/routing-groups?id=hybrid-coding');

  // All routing surfaces use catalogue names, including missing installations.
  const localNames = ['Spark-X2.6-4B-Q4_K_M', 'Spark-X2.6-4B-Q8_0'];
  const localRefs = ['local/spark-x2.6-4b', 'local/spark-x2.6-4b-q8_0'];
  await page.route('**/api/cloud-providers', route => route.fulfill({ json: {
    providers: [], localModels: localRefs.map((selector, i) => ({ id: selector.slice(6), selector, name: localNames[i], installed: true })),
  } }));
  for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) {
    for (const width of [1280, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(`http://${address}/?locale=${language}&view=routing`);
      await page.waitForFunction(() => window.__weftoRoutingGroups?.state.localModels.length === 2);
      assert.deepEqual(await page.evaluate(refs => refs.map(ref => window.__weftoRoutingGroups.describeMember(ref).modelName), localRefs), localNames);
      assert.equal(await page.evaluate(() => window.__weftoRoutingGroups.describeMember('local/unknown-model').modelName), 'unknown-model');
      await page.locator('#newRoutingGroupBtn').click();
      await page.locator('#rgAddModelBtn').click();
      for (let i = 0; i < localRefs.length; i++) {
        assert.equal(await page.locator(`[data-picker-ref="${localRefs[i]}"] .v`).textContent(), localNames[i]);
      }
      await page.locator('#rgPickerSearch').fill('Q4_K_M');
      assert.deepEqual(await page.locator('#rgPickerList [data-picker-ref]').evaluateAll(rows => rows.map(row => row.dataset.pickerRef)), [localRefs[0]]);
      await page.locator(`[data-picker-ref="${localRefs[0]}"]`).click();
      await page.locator('#rgAddModelBtn').click();
      assert.ok((await page.locator('#routingGroupModal').textContent()).includes(localNames[0]));
      await page.evaluate(refs => {
        const rg = window.__weftoRoutingGroups;
        rg.state.groups.push({ id: 'label-rule', name: 'Label rule', members: [refs[0]], rules: [{ when: 'tools', use: refs[1] }] });
        rg.renderGroups();
      }, localRefs);
      await page.locator('#rgCancelBtn').click();
      await page.locator('[data-rg-edit="label-rule"]').click();
      for (let i = 0; i < localRefs.length; i++) {
        assert.ok((await page.locator(`.rt-rule-use option[value="${localRefs[i]}"]`).textContent()).startsWith(localNames[i]), 'rule targets use names even outside group members');
      }
      await page.waitForFunction(() => {
        const rg = window.__weftoRoutingGroups, note = document.querySelector('#rgPrivacySummary');
        return note && note.textContent !== rg.tr('Checking redaction policy');
      });
      await page.locator('#rgCancelBtn').click();
    }
  }
  await page.unroute('**/api/cloud-providers');

  // 6. 模型服务商页「本地端侧模型」图标渲染验证
  await page.goto(`http://${address}/?locale=zh&view=providers`);
  const localProviderIcon = page.locator('#localProviders .row.local-provider .ic img').first();
  await localProviderIcon.waitFor({ state: 'visible' });
  assert.equal(await localProviderIcon.getAttribute('src'), '/icons/spark-color.svg');
  await page.waitForFunction(() => {
    const img = document.querySelector('#localProviders .row.local-provider .ic img');
    return Boolean(img && img.complete && img.naturalWidth > 0);
  });

  assert.deepEqual(pageErrors, []);
});

}
