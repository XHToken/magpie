const { skipTutorial } = require('./returning-user.cjs');
// Real built product + temporary HOME + local upstream. Local statistics
// presentation and error states use explicitly marked management projections;
// the persistence scenarios create their records through real model HTTP.
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
  test(`persisted product request observations (${engine})`, { timeout: 180000 }, async t => {
    const home = await fs.mkdtemp(path.join(os.tmpdir(), 'wefto-observation-ui-'));
    let service, exited, browser, upstream, releaseHeld;
    t.after(async () => {
      releaseHeld?.();
      if (browser) await browser.close();
      if (service) { if (service.exitCode === null) service.kill('SIGTERM'); await exited; }
      if (upstream) await new Promise(resolve => upstream.close(resolve));
      await fs.rm(home, { recursive: true, force: true });
    });
    const env = { ...process.env, HOME: home, USERPROFILE: home };
    for (const key of ['XDG_CONFIG_HOME', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME', 'APPDATA', 'LOCALAPPDATA']) env[key] = path.join(home, key);
    const layout = JSON.parse(execFileSync(binary, ['dirs'], { env, encoding: 'utf8' }));
    execFileSync(binary, ['config', 'init'], { env });
    const reservation = net.createServer(); reservation.listen(0, '127.0.0.1'); await once(reservation, 'listening');
    const base = `http://127.0.0.1:${reservation.address().port}`; await new Promise(resolve => reservation.close(resolve));
    const configFile = path.join(layout.config, 'config.json'), config = JSON.parse(await fs.readFile(configFile, 'utf8'));
    config.listen.address = new URL(base).host; await fs.writeFile(configFile, JSON.stringify(config), { mode: 0o600 });
    service = spawn(binary, ['serve'], { env, stdio: ['ignore', 'pipe', 'pipe'] }); exited = once(service, 'exit');
    let output = ''; service.stdout.on('data', chunk => { output += chunk; }); service.stderr.on('data', chunk => { output += chunk; });
    let token;
    for (let i = 0; i < 200; i++) {
      if (service.exitCode !== null) assert.fail(`service exited: ${output}`);
      try { token = (await fs.readFile(path.join(layout.state, 'router.lock', 'management-token'), 'utf8')).trim(); break; } catch {}
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    assert.ok(token, 'service started and wrote its isolated management token');
    const api = async (route, body) => {
      const response = await fetch(base + route, { method: body ? 'POST' : 'GET', headers: { Authorization: `Bearer ${token}`, ...(body ? { 'Content-Type': 'application/json' } : {}) }, ...(body ? { body: JSON.stringify(body) } : {}) });
      assert.equal(response.status, 200, `${route}: ${await response.clone().text()}`); return response.json();
    };
    assert.equal((await fetch(base + '/api/requests')).status, 401);
    assert.deepEqual((await api('/api/requests')).requests, []);
    browser = await (engine === 'webkit' ? webkit : chromium).launch({ ...(engine === 'chromium' && process.env.CHROMIUM_EXECUTABLE ? { executablePath: process.env.CHROMIUM_EXECUTABLE } : {}) });
    async function authenticate(page, language = 'en', width = 1600) {
      await page.setViewportSize({ width, height: 950 }); await skipTutorial(page); await page.goto(`${base}/?locale=${language}&view=usage&tab=requests`);
      await page.waitForFunction(() => document.querySelector('#service').classList.contains('ok'));
      await page.waitForFunction(() => !!managementToken && !!serviceInfo);
      await page.waitForFunction(() => !!managementToken && !!serviceInfo && !document.querySelector('#requestRecords').textContent.includes(t('Loading requests…')));
    }
    async function detailValue(page, key) {
      const title = await page.evaluate(key => t(key), key);
      return page.locator('#requestRecords .request-detail > .request-fields > dt').and(page.getByText(title, { exact: true })).evaluate(node => node.nextElementSibling.textContent);
    }
    const emptyPage = await browser.newPage({ locale: 'en-US' }); await authenticate(emptyPage);
    assert.equal(await emptyPage.locator('#requestRecords').textContent(), 'No requests yet');
    if (process.env.ARTIFACT_DIR) { await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true }); await emptyPage.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `statistics-empty-${engine}.png`) }); }
    await emptyPage.close();
    let mode = 'known', upstreamCalls = 0, markHeldStarted;
    const heldStarted = new Promise(resolve => { markHeldStarted = resolve; });
    const secret = 'fake-private-prompt-and-error-never-in-ledger';
    const model = 'local-test-model-' + 'very-long-model-name-'.repeat(9);
    upstream = http.createServer(async (req, res) => {
      upstreamCalls++; assert.equal(req.url, '/v1/chat/completions'); assert.equal(req.headers.authorization, 'Bearer fake-observation-upstream-key');
      let body = ''; for await (const chunk of req) body += chunk; assert.equal(JSON.parse(body).model, model);
      if (mode === 'held') { await new Promise(resolve => { releaseHeld = resolve; markHeldStarted(); }); }
      res.setHeader('Content-Type', 'application/json');
      if (mode === 'failed') { res.statusCode = 503; res.setHeader('Retry-After', '0'); res.end(JSON.stringify({ error: { message: secret } })); return; }
      const usage = mode === 'unknown' ? undefined : mode === 'zero' ? { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 } : mode === 'partial' ? { prompt_tokens: 2 } : { prompt_tokens: 4, completion_tokens: 3, total_tokens: 7 };
      res.end(JSON.stringify({ id: 'chatcmpl-test', object: 'chat.completion', model, choices: [{ index: 0, message: { role: 'assistant', content: secret }, finish_reason: 'stop' }], ...(usage ? { usage } : {}) }));
    });
    upstream.listen(0, '127.0.0.1'); await once(upstream, 'listening');
    const provider = (await api('/api/cloud-providers', { name: 'Local observation fixture', baseURL: `http://127.0.0.1:${upstream.address().port}/v1`, protocol: 'chat', model, apiKey: 'fake-observation-upstream-key', proxyMode: 'direct' })).provider;
    assert.ok(provider.id);
    await api('/api/apps', { id: 'codex', name: 'Codex snapshot', cloud: { providerId: provider.id, model }, credentialMode: 'stored' });
    const ids = {};
    async function generate(kind) {
      mode = kind;
      const response = await fetch(base + '/app/codex/v1/chat/completions', { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer fake-client-placeholder' }, body: JSON.stringify({ model, messages: [{ role: 'user', content: secret }] }) });
      const responseBody = await response.text(); assert.equal(response.status, kind === 'failed' ? 502 : 200, `${kind}: ${responseBody}`); const id = response.headers.get('x-request-id'); assert.ok(id); ids[kind] = id; return id;
    }
    for (const kind of ['known', 'unknown', 'zero', 'partial', 'failed']) await generate(kind);
    // Enough real external requests to exercise a second bounded product page.
    for (let i = 0; i < 47; i++) await generate('known');
    assert.equal(upstreamCalls, 53);
    const first = await api('/api/requests?limit=50'); assert.equal(first.requests.length, 50); assert.ok(first.nextCursor);
    const second = await api('/api/requests?limit=50&before=' + encodeURIComponent(first.nextCursor)); assert.equal(second.requests.length, 2);
    const directCloudDetail = await api('/api/requests/' + ids.failed);
    const failed = directCloudDetail.request;
    assert.equal(failed.status, 'failed'); assert.equal(failed.attempts, 2); assert.equal(failed.destination, 'cloud'); assert.equal(failed.usage, null);
    assert.equal(failed.decision, 'cloud'); assert.equal((directCloudDetail.calls || []).length, 0, 'the real direct-cloud request has no observed judgment call');
    assert.equal(JSON.stringify(first).includes(secret), false); assert.equal(JSON.stringify(failed).includes(secret), false);
    const errors = [];
    for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) {
      for (const width of [1600, 375]) {
        const page = await browser.newPage({ locale: 'en-US' }); page.on('pageerror', error => errors.push(error.message));
        const external = []; page.on('request', req => { if (new URL(req.url()).origin !== base) external.push(req.url()); });
        await authenticate(page, language, width);
        await page.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 50);
        assert.ok((await page.locator('#requestRecords').textContent()).includes(model));
        assert.ok((await page.locator(`#requestRecords [data-request="${ids.zero}"]`).textContent()).includes(await page.evaluate(() => t('Total tokens') + ': 0')));
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${language} ${width}: viewport overflow`);
        await page.locator('#requestRecords .request-pages button').click();
        await page.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 2);
        assert.ok((await page.locator(`#requestRecords [data-request="${ids.unknown}"]`).textContent()).includes(await page.evaluate(() => t('Unknown'))));
        await page.locator('#requestRecords .request-pages button').click();
        await page.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 50);
        // Status chips: all, processing, succeeded, failed.
        await page.locator('#requestStatuses button').nth(3).click();
        await page.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 1);
        await page.locator('#requestRecords tbody button').click();
        await page.waitForFunction(() => !!document.querySelector('#requestRecords [data-request-detail]'));
        assert.equal(await page.locator('#requestRecords [data-request-detail]').getAttribute('data-request-detail'), ids.failed);
        assert.equal(await page.locator('#requestRecords .request-attempt').count(), 2);
        assert.equal(await detailValue(page, 'Judgment status'), await page.evaluate(() => t('Not observed')));
        assert.equal(await detailValue(page, 'Cloud routing reason'), await page.evaluate(() => t('Unknown')), 'a direct-cloud decision without a judgment observation cannot be attributed to the local judge');
        assert.equal(await page.locator('#requestRecords .request-detail').getByText(await page.evaluate(() => t('Judgment selected cloud')), { exact: true }).count(), 0);
        if (process.env.ARTIFACT_DIR && ['en', 'zh'].includes(language)) { await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true }); await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `observation-details-${engine}-${language}-${width}.png`) }); }
        assert.equal((await page.locator('#requestRecords').textContent()).includes(secret), false);
        assert.equal((await page.locator('#requestRecords').textContent()).includes(await page.evaluate(() => t('Unknown'))), true);
        assert.equal(await page.locator('#requestScope').textContent(), await page.evaluate(() => t('Final local and cloud destinations. One business request is counted once; retries and internal calls do not add requests.') + ' ' + t('Actual reported usage. Missing fields remain unknown; explicit zero stays zero.')));
        const tab = name => page.locator(width >= 1024 ? `.nav-s[data-of=usageTab-${name}]` : `#usageTab-${name}`);
        await tab('usage').click();
        await page.waitForFunction(() => document.querySelectorAll('#requestSummary .kpi').length === 7);
        assert.equal(await page.locator('#requestSummary .kpi b').first().textContent(), '52');
        if (process.env.ARTIFACT_DIR && ['en', 'zh'].includes(language)) await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `observation-summary-${engine}-${language}-${width}.png`) });
        assert.equal((await page.locator('#requestSummary').textContent()).includes(await page.evaluate(() => t('Unknown requests'))), true);
        assert.equal((await page.locator('#requestSummary').textContent()).includes(await page.evaluate(() => t('Generation requests only. Totals cover retained records, not all-time usage. Auxiliary operations are excluded.'))), true);
        assert.equal((await page.locator('#requestSummary .request-destinations').textContent()).includes(await page.evaluate(() => t('Final local and cloud destinations. One business request is counted once; retries and internal calls do not add requests.'))), true);
        assert.equal(await page.locator('#requestSummary .request-destinations [data-destination]').count(), 4);
        await page.locator('#nav [data-view=routing]').click();
        assert.equal(await page.locator('#view-routing').isVisible(), true);
        assert.equal(await page.locator('#routingGroups').count(), 1);
        assert.equal(await page.locator('#routingRecords, #routingScope, #routingSummary, #routingDestinations').count(), 0);
        await page.locator('#nav [data-view=usage]').click();
        assert.equal(await page.locator('#contextPane, #usageTab-context').count(), 0); assert.deepEqual(external, []);
        if (process.env.ARTIFACT_DIR && (language === 'en' || language === 'zh')) { await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true }); await tab('requests').click(); await page.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 1); await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `observations-${engine}-${language}-${width}.png`) }); }
        await page.close();
      }
    }
    assert.deepEqual(errors, []);
    // Explicit management projections cover statuses that the local upstream
    // cannot produce. Filter the response by the query actually sent by the UI.
    const filterStatuses = ['processing', 'succeeded', 'failed', 'canceled', 'timed_out', 'interrupted'];
    const filterRecords = filterStatuses.flatMap((status, index) => ['generate', 'count_tokens'].map((operation, op) => ({
      ...failed, requestId: (index * 2 + op + 1).toString(16).padStart(32, '0'), status, operation,
      appId: op ? 'loomy' : 'codex', appName: op ? 'Loomy' : 'Codex', destination: op ? 'cloud' : 'local'
    })));
    for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) for (const width of [1600, 375]) {
      const filterPage = await browser.newPage({ locale: 'en-US' });
      filterPage.on('pageerror', error => errors.push(error.message));
      const queries = [];
      await filterPage.route('**/api/requests?*', route => {
        const query = new URL(route.request().url()).searchParams;
        queries.push(Object.fromEntries(query));
        const matches = filterRecords.filter(record => ['status', 'operation', 'appId'].every(key => !query.has(key) || record[key] === query.get(key)));
        const offset = query.has('before') ? 6 : 0;
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
          schemaVersion: 2, ledger: { available: true }, requests: matches.slice(offset, offset + 6), nextCursor: matches.length > offset + 6 ? 'older-filter-records' : ''
        }) });
      });
      await authenticate(filterPage, language, width);
      async function choose(selector, index, expected) {
        const count = queries.length;
        await filterPage.locator(`${selector} button`).nth(index).click();
        await filterPage.waitForFunction(() => !document.querySelector('#requestApply').disabled);
        assert.equal(queries.length, count + 1, `${language} ${width}: clicking ${selector}[${index}] queries immediately`);
        const rows = await filterPage.locator('#requestRecords tbody tr').evaluateAll(nodes => nodes.map(node => node.dataset.request));
        assert.deepEqual(rows, expected.map(record => record.requestId));
        assert.equal(await filterPage.locator(`${selector} button[aria-pressed=true]`).count(), 1);
        assert.equal(await filterPage.locator(`${selector} button`).nth(index).getAttribute('aria-pressed'), 'true');
        assert.equal(queries.at(-1).before, undefined, 'a changed filter starts at the latest page');
      }
      await filterPage.locator('#requestRecords .request-pages button').click();
      await filterPage.waitForFunction(() => !document.querySelector('#requestApply').disabled);
      assert.equal(queries.at(-1).before, 'older-filter-records');
      for (const [index, status] of filterStatuses.entries()) {
        await choose('#requestStatuses', index + 1, filterRecords.filter(record => record.status === status));
        assert.equal(queries.at(-1).status, status);
      }
      const interrupted = filterRecords.filter(record => record.status === 'interrupted');
      await choose('#requestOperations', 2, interrupted.filter(record => record.operation === 'count_tokens'));
      assert.equal(queries.at(-1).operation, 'count_tokens');
      await choose('#requestOperations', 1, interrupted.filter(record => record.operation === 'generate'));
      await choose('#requestOperations', 0, interrupted);
      await filterPage.locator('#requestApp').fill('loomy');
      await filterPage.locator('#requestApply').click();
      await filterPage.waitForFunction(() => !document.querySelector('#requestApply').disabled);
      await choose('#requestOperations', 1, []);
      assert.equal(await filterPage.locator('#requestRecords').textContent(), await filterPage.evaluate(() => t('No matching requests')));
      await choose('#requestOperations', 2, interrupted.filter(record => record.appId === 'loomy'));
      assert.equal(queries.at(-1).appId, 'loomy');
      await filterPage.locator('#requestApp').fill('');
      await choose('#requestStatuses', 0, filterRecords.filter(record => record.operation === 'count_tokens'));
      assert.equal(queries.at(-1).status, undefined);
      await choose('#requestOperations', 0, filterRecords.slice(0, 6));
      assert.equal(queries.at(-1).operation, undefined);
      await filterPage.close();
    }
    assert.deepEqual(errors, []);
    const heldResponse = generate('held'); let heldTimer;
    const processingPage = await browser.newPage({ locale: 'en-US' });
    try {
      await Promise.race([heldStarted, new Promise((_, reject) => { heldTimer = setTimeout(() => reject(new Error('held upstream was not reached')), 10000); })]);
      clearTimeout(heldTimer); await authenticate(processingPage);
      await processingPage.locator('#requestStatuses button').nth(1).click();
      await processingPage.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 1);
      assert.equal(await processingPage.locator('#requestRecords tbody td').nth(5).textContent(), 'Processing');
      await processingPage.locator('#requestRecords tbody button').click();
      await processingPage.waitForFunction(() => !!document.querySelector('#requestRecords [data-request-detail]'));
      assert.equal(await processingPage.locator('#requestRecords .request-detail > .request-fields > dt').and(processingPage.getByText('Duration (ms)', { exact: true })).evaluate(node => node.nextElementSibling.textContent), 'Processing');
    } finally {
      clearTimeout(heldTimer); releaseHeld?.(); await heldResponse; await processingPage.close();
    }
    assert.equal((await api('/api/requests/' + ids.held)).request.status, 'succeeded');
    const page = await browser.newPage({ locale: 'en-US' }); await authenticate(page);
    await page.locator('#requestApp').fill('missing-app'); await page.locator('#requestApply').click();
    await page.waitForFunction(() => document.querySelector('#requestRecords').textContent === 'No matching requests');
    await page.locator('#requestApp').fill('codex'); await page.locator('#requestApply').click();
    await page.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 50);
    const recovered = { ...failed, status: 'interrupted', executionKnown: false, destination: '', attempts: 0, outputCommitted: false, durationMs: 999, safeErrorCode: 'request_interrupted', failureStage: 'interrupted' };
    await page.route('**/api/requests?*', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schemaVersion: 1, ledger: { available: true }, requests: [recovered], nextCursor: '' }) }));
    await page.route('**/api/requests/' + recovered.requestId, route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schemaVersion: 1, request: recovered }) }));
    await page.locator('#usageReload').click(); await page.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 1);
    for (const column of [5, 6, 7]) assert.equal(await page.locator('#requestRecords tbody td').nth(column).textContent(), 'Unknown');
    await page.locator('#requestRecords tbody button').click(); await page.waitForFunction(() => !!document.querySelector('#requestRecords [data-request-detail]'));
    for (const name of ['Duration (ms)', 'Request end', 'Output committed']) assert.equal(await page.locator('#requestRecords .request-detail > .request-fields > dt').and(page.getByText(name, { exact: true })).evaluate(node => node.nextElementSibling.textContent), 'Unknown');
    await page.unroute('**/api/requests?*'); await page.unroute('**/api/requests/' + recovered.requestId);
    const count = (knownTokens, knownRequests, unknownRequests) => ({ knownTokens, knownRequests, unknownRequests });
    const known = count(0, 1, 0), unknown = count(null, 0, 1);
    const local = { ...failed, requestId: 'a'.repeat(32), status: 'succeeded', destination: 'local', attempts: 0, attemptDetails: [], localModel: 'spark-x2.6-4B', safeErrorCode: '', failureStage: '', usage: { inputTokens: 0, outputTokens: 7, totalTokens: null }, referencePrice: { currency: 'USD', unit: 'million-tokens', input: 1.25, output: 2.5, version: 'price-v1', source: 'Profile reference' } };
    const localUnknown = { ...local, requestId: 'b'.repeat(32), localModel: 'spark-x2.6-4B-Q8_0', usage: { inputTokens: 0, outputTokens: null, totalTokens: null } };
    const localZero = { ...local, requestId: 'c'.repeat(32), usage: { inputTokens: 0, outputTokens: 0, totalTokens: 0 }, referencePrice: { ...local.referencePrice, currency: 'EUR' } };
    const counts = { processing: 0, succeeded: 0, failed: 0, canceled: 0, timedOut: 0, interrupted: 0 };
    const timing = { knownMs: 0, knownCalls: 1, unknownCalls: 0 }, unknownTiming = { knownMs: null, knownCalls: 0, unknownCalls: 1 };
    const calls = { total: 1, inferenceStarted: 1, notStarted: 0, results: { succeeded: 1 }, inputTokens: known, outputTokens: known, totalTokens: unknown, timings: { queueLoadMs: timing, queueMs: unknownTiming, loadMs: unknownTiming, requestHeadersMs: unknownTiming, consumeMs: unknownTiming, inferenceMs: timing, totalMs: timing }, businessUsageCalls: 0, overheadUsageCalls: 1, overheadInputTokens: known, overheadOutputTokens: known, overheadTotalTokens: unknown };
    const projection = { total: 4, ...counts, succeeded: 3, failed: 1, inputTokens: count(0, 3, 1), outputTokens: count(7, 2, 2), totalTokens: count(0, 1, 3), oldestStartedAt: failed.startedAt, newestStartedAt: failed.startedAt,
      destinations: [{ ...counts, destination: 'local', total: 3, share: .75, succeeded: 3, inputTokens: count(0, 3, 0), outputTokens: count(7, 2, 1), totalTokens: count(0, 1, 2) }, { ...counts, destination: 'cloud', total: 1, share: .25, failed: 1, inputTokens: unknown, outputTokens: unknown, totalTokens: unknown }, { ...counts, destination: 'none', total: 0, share: 0, inputTokens: count(null, 0, 0), outputTokens: count(null, 0, 0), totalTokens: count(null, 0, 0) }],
      savings: { eligibleRequests: 3, inputTokens: count(0, 3, 0), outputTokens: count(7, 2, 1), avoidedTokens: count(7, 2, 1), unknownPriceRequests: 0, money: [{ currency: 'USD', unit: 'million-tokens', knownAmount: '0.0000175', knownRequests: 1, unknownRequests: 1 }, { currency: 'EUR', unit: 'million-tokens', knownAmount: '0', knownRequests: 1, unknownRequests: 0 }] },
      internalCalls: [{ ...calls, purpose: 'judgment' }, { ...calls, purpose: 'execution', businessUsageCalls: 1, overheadUsageCalls: 0, overheadInputTokens: count(null, 0, 0), overheadOutputTokens: count(null, 0, 0), overheadTotalTokens: count(null, 0, 0) }, { ...calls, purpose: 'redaction', inferenceStarted: 0, notStarted: 1, results: { rejected: 1 }, inputTokens: count(null, 0, 0), outputTokens: count(null, 0, 0), totalTokens: count(null, 0, 0) }], checkCalls: [{ ...calls, purpose: 'judgment', total: 2, inferenceStarted: 2, overheadUsageCalls: 2 }] };
    const callDetail = { purpose: 'execution', modelId: local.localModel, fingerprint: 'safe-model-fingerprint', materialVersion: 'execution-v1', mode: 'formal', result: 'succeeded', inferenceStarted: true, startedAt: local.startedAt, endedAt: local.endedAt, safeErrorCode: '', usage: local.usage, timings: { queueLoadMs: 0, queueMs: null, loadMs: null, inferenceMs: 0, totalMs: 0 } };
    const settings = { contextTokens: 65536, judgmentQueueSeconds: 15, judgmentInferenceSeconds: 30, executionQueueSeconds: 30, executionSeconds: 300, loadSeconds: 90, requestSeconds: 600, cloudReserveSeconds: 60 };
    const fallback = { ...failed, redactionPolicy: { required: true, profileRequired: true, sourceGroupIds: ['privacy'], sourceGroupCount: 1, modelId: 'spark-x2.6-4B-Q8_0', actualModelId: 'spark-x2.6-4B-Q8_0', engineVersion: 'privacy-user-input-v1', scopeVersion: 'user-input-v1', scanDisposition: 'known_values_only', newUserCharacters: 0, newUserChunks: 0, reusedUserFragments: 2, localInferenceCalls: 0, state: 'prepared' }, decision: 'cloud', capabilityState: 'judgment_inference_timeout', judgmentMs: 30000 };
    const failedJudgment = { ...callDetail, purpose: 'judgment', result: 'failed', safeErrorCode: 'local_judgment_inference_timeout', settings, settingsFingerprint: 'a'.repeat(64), contextUsage: { inputTokens: 128, outputTokens: 32, contextTokens: 65536 }, httpStatus: 400 };
    const blocked = { ...failed, requestId: 'd'.repeat(32), destination: 'none', model: '', localModel: '', attempts: 0, safeErrorCode: 'redaction_unavailable', localModels: [{ purpose: 'redaction', modelId: 'spark-x2.6-4B' }] };
    const pending = { ...blocked, requestId: 'e'.repeat(32), status: 'processing', localModels: [{ purpose: 'judgment', modelId: 'spark-x2.6-4B-Q8_0' }] };
    const unobserved = { ...blocked, requestId: 'f'.repeat(32), localModels: [], redactionPolicy: { modelId: 'configured-but-not-executed', required: true, state: 'blocked' } };
    for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) for (const width of [1600, 375]) {
      const fixturePage = await browser.newPage({ locale: 'en-US' }); fixturePage.on('pageerror', error => errors.push(error.message));
      let cloudDetail = fallback, cloudCalls = [failedJudgment];
      await fixturePage.route('**/api/requests?*', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schemaVersion: 2, ledger: { available: true, maxRecords: 10000, maxAgeSeconds: 2592000, observationFailures: 2 }, requests: [local, localUnknown, localZero, failed], nextCursor: '' }) }));
      await fixturePage.route('**/api/requests/summary', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schemaVersion: 2, ledger: { available: true, maxRecords: 10000, maxAgeSeconds: 2592000, observationFailures: 2 }, summary: projection }) }));
      await fixturePage.route('**/api/requests/' + local.requestId, route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schemaVersion: 2, request: local, calls: [callDetail] }) }));
      await fixturePage.route('**/api/requests/' + failed.requestId, route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schemaVersion: 2, request: cloudDetail, calls: cloudCalls }) }));
      await authenticate(fixturePage, language, width);
      const localRow = fixturePage.locator(`#requestRecords [data-request="${local.requestId}"]`);
      await localRow.waitFor(); assert.equal(await localRow.locator('td').nth(6).textContent(), await fixturePage.evaluate(() => t('Local')));
      assert.ok((await fixturePage.locator('#requestRecords .request-observation-warning').textContent()).endsWith(': 2'));
      assert.equal(await localRow.locator('td').nth(1).textContent(), local.localModel);
      assert.equal(await fixturePage.locator(`#requestRecords [data-request="${localUnknown.requestId}"] td`).nth(1).textContent(), localUnknown.localModel);
      assert.ok((await localRow.textContent()).includes(await fixturePage.evaluate(() => t('Input tokens') + ': 0')));
      assert.ok((await localRow.textContent()).includes(await fixturePage.evaluate(() => t('Total tokens') + ': ' + t('Unknown'))));
      await localRow.locator('button').click(); await fixturePage.locator('#requestRecords .request-price').waitFor();
      assert.ok((await fixturePage.locator('#requestRecords .request-price').textContent()).includes('price-v1'));
      assert.ok((await fixturePage.locator('#requestRecords .request-detail > .request-fields').textContent()).includes(model));
      assert.ok((await fixturePage.locator('#requestRecords .request-detail > .request-fields').textContent()).includes(local.localModel));
      assert.ok((await fixturePage.locator('#requestRecords .request-call-details').textContent()).includes('execution-v1'));
      await fixturePage.locator(`#requestRecords [data-request="${failed.requestId}"] button`).click();
      await fixturePage.waitForFunction(() => document.querySelector('#requestRecords .request-detail > .request-fields')?.textContent.includes(t('Judgment inference timed out')));
      const fallbackDetails = await fixturePage.locator('#requestRecords .request-detail > .request-fields').textContent();
      assert.equal(await detailValue(fixturePage, 'User input processing'), await fixturePage.evaluate(() => t('Known user values only')));
      assert.ok(fallbackDetails.includes(await fixturePage.evaluate(() => t('Application requirement'))), 'application privacy source is visible');
      assert.ok(fallbackDetails.includes(await fixturePage.evaluate(() => t('Actual redaction model') + 'spark-x2.6-4B-Q8_0')), 'actual privacy binding is visible');
      assert.ok(fallbackDetails.includes(await fixturePage.evaluate(() => t('Judgment status') + t('Failed'))), 'local judgment failure remains visible with final cloud destination');
      assert.ok(fallbackDetails.includes(await fixturePage.evaluate(() => t('Execution status') + t('Not observed'))), 'no local execution is not misrepresented as success');
      const internalDetails = await fixturePage.locator('#requestRecords .request-call-details').textContent();
      assert.ok(internalDetails.includes(await fixturePage.evaluate(() => t('Output token reserve') + '32')));
      assert.ok(internalDetails.includes(await fixturePage.evaluate(() => (65536).toLocaleString(intlLang()))));
      assert.ok(internalDetails.includes('a'.repeat(64)) && internalDetails.includes('400'), 'safe settings identity and HTTP status are visible');
      cloudDetail = { ...fallback, capabilityState: '', judgmentMs: 100 };
      cloudCalls = [{ ...callDetail, purpose: 'judgment' }];
      await fixturePage.locator(`#requestRecords [data-request="${failed.requestId}"] button`).click();
      await fixturePage.waitForFunction(() => document.querySelector('#requestRecords .request-detail > .request-fields')?.textContent.includes(t('Judgment selected cloud')));
      assert.equal(await detailValue(fixturePage, 'Judgment status'), await fixturePage.evaluate(() => t('Succeeded')));
      assert.equal(await detailValue(fixturePage, 'Redaction status'), await fixturePage.evaluate(() => t('Not observed')));
      assert.equal(await detailValue(fixturePage, 'Redaction call duration (ms)'), await fixturePage.evaluate(() => t('Not observed')));
      assert.equal(await detailValue(fixturePage, 'Cloud routing reason'), await fixturePage.evaluate(() => t('Judgment selected cloud')), 'a successful observed judgment can explain the cloud routing decision');
      assert.equal(await fixturePage.locator('#requestDestinations button').count(), 4);
      await fixturePage.locator('#requestDestinations button').nth(1).click(); assert.equal(await fixturePage.locator('#requestRecords tbody tr').count(), 3);
      await fixturePage.locator('#requestDestinations button').nth(2).click(); assert.equal(await fixturePage.locator('#requestRecords tbody tr').count(), 1);
      await fixturePage.locator('#requestDestinations button').nth(0).click(); assert.equal(await fixturePage.locator('#requestRecords tbody tr').count(), 4);
      await fixturePage.route('**/api/requests?*', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schemaVersion: 1, ledger: { available: true }, requests: [blocked, pending, unobserved, { ...local, localModels: [{ purpose: 'execution', modelId: local.localModel }] }, { ...failed, localModels: blocked.localModels }], nextCursor: '' }) }));
      await fixturePage.locator('#usageReload').click();
      await fixturePage.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 5);
      const modelCell = id => fixturePage.locator(`#requestRecords [data-request="${id}"] td`).nth(1);
      assert.equal(await modelCell(blocked.requestId).textContent(), await fixturePage.evaluate(() => 'spark-x2.6-4B (' + t('Redaction') + ')'));
      assert.equal(await modelCell(pending.requestId).textContent(), await fixturePage.evaluate(() => 'spark-x2.6-4B-Q8_0 (' + t('Judgment') + ')'));
      assert.equal(await modelCell(unobserved.requestId).textContent(), await fixturePage.evaluate(() => t('Unknown')));
      assert.equal(await modelCell(local.requestId).textContent(), local.localModel, 'final local execution is not duplicated');
      assert.equal(await modelCell(failed.requestId).textContent(), model + ' · ' + await fixturePage.evaluate(() => 'spark-x2.6-4B (' + t('Redaction') + ')'), 'cloud generation and local redaction keep their own identities');
      const overviewTab = fixturePage.locator(width >= 1024 ? '.nav-s[data-of=usageTab-usage]' : '#usageTab-usage'); await overviewTab.click();
      await fixturePage.locator('#requestSummary [data-currency=USD]').waitFor();
      assert.equal(await fixturePage.locator('#requestSummary .kpi b').first().textContent(), '4');
      assert.ok((await fixturePage.locator('#requestSummary .request-observation-warning').textContent()).endsWith(': 2'));
      assert.equal(await fixturePage.locator('#requestSummary [data-destination=local] h3').textContent(), await fixturePage.evaluate(() => t('Local')));
      assert.ok((await fixturePage.locator('#requestSummary [data-currency=USD]').textContent()).includes('0.0000175'));
      assert.ok((await fixturePage.locator('#requestSummary [data-currency=EUR]').textContent()).startsWith('EUR: 0'));
      assert.equal(await fixturePage.locator('#requestSummary [data-currency]').count(), 2);
      const executionFields = fixturePage.locator('#requestSummary .request-internal [data-purpose=execution] .request-fields').first();
      const includedLabel = await fixturePage.evaluate(() => t('Business usage already counted'));
      assert.equal(await executionFields.locator('dt').and(executionFields.getByText(includedLabel, { exact: true })).evaluate(node => node.nextElementSibling.textContent), '1');
      const avgInferenceLabel = await fixturePage.evaluate(() => t('Average inference (ms)'));
      assert.equal(await executionFields.locator('dt').and(executionFields.getByText(avgInferenceLabel, { exact: true })).evaluate(node => node.nextElementSibling.textContent), '0');
      const redactionFields = fixturePage.locator('#requestSummary .request-internal [data-purpose=redaction] .request-fields').first();
      const notStartedLabel = await fixturePage.evaluate(() => t('Inference not started'));
      assert.equal(await redactionFields.locator('dt').and(redactionFields.getByText(notStartedLabel, { exact: true })).evaluate(node => node.nextElementSibling.textContent), '1');
      assert.equal(await fixturePage.locator('#requestSummary .request-diagnostics [data-purpose=judgment]').count(), 1);
      assert.equal(await fixturePage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${language} ${width}: statistics overflow`);
      if (process.env.ARTIFACT_DIR) {
        await fixturePage.setViewportSize({ width, height: 2600 });
        for (const [name, selector] of [['local', '[data-destination=local]'], ['cloud', '[data-destination=cloud]'], ['savings', '.request-savings'], ['judgment', '.request-internal [data-purpose=judgment]'], ['execution', '.request-internal [data-purpose=execution]']]) await fixturePage.locator(`#requestSummary ${selector}`).screenshot({ path: path.join(process.env.ARTIFACT_DIR, `statistics-projection-${name}-${engine}-${language}-${width}.png`) });
      }
      // Historical projections may carry a stale process error beside a final
      // success. The summary follows the terminal outcome; attempts retain it.
      const failoverAttempts = [
        { number: 1, startedAt: failed.startedAt, httpStatus: 200, errorCode: 'cloud_response_error', retried: true },
        { number: 2, startedAt: failed.startedAt, httpStatus: 200, errorCode: '', outputCommitted: true }
      ];
      const failoverSuccess = { ...failed, requestId: '1'.repeat(32), status: 'succeeded', capabilityState: '', safeErrorCode: 'cloud_response_error', failureStage: 'cloud', attempts: 2, attemptDetails: failoverAttempts };
      const failoverFailure = { ...failed, requestId: '2'.repeat(32), capabilityState: 'judgment_failed', safeErrorCode: 'cloud_auth_error', attempts: 2, attemptDetails: [failoverAttempts[0], { ...failoverAttempts[1], errorCode: 'cloud_auth_error', httpStatus: 401 }] };
      await fixturePage.route('**/api/requests?*', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schemaVersion: 2, ledger: { available: true }, requests: [failoverSuccess, failoverFailure], nextCursor: '' }) }));
      for (const request of [failoverSuccess, failoverFailure]) await fixturePage.route('**/api/requests/' + request.requestId, route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ schemaVersion: 2, request }) }));
      await fixturePage.locator(width >= 1024 ? '.nav-s[data-of=usageTab-requests]' : '#usageTab-requests').click();
      await fixturePage.locator('#usageReload').click();
      await fixturePage.locator(`#requestRecords [data-request="${failoverSuccess.requestId}"]`).waitFor();
      const reasonCell = id => fixturePage.locator(`#requestRecords [data-request="${id}"] td`).nth(10);
      assert.equal(await reasonCell(failoverSuccess.requestId).textContent(), '—', 'final success has no failure reason');
      assert.equal(await reasonCell(failoverFailure.requestId).textContent(), 'cloud_auth_error', 'final failure takes precedence over an earlier local routing reason');
      await fixturePage.locator(`#requestRecords [data-request="${failoverSuccess.requestId}"] button`).click();
      await fixturePage.locator('#requestRecords .request-detail').waitFor();
      assert.equal(await detailValue(fixturePage, 'Error code'), '—');
      assert.equal(await detailValue(fixturePage, 'Failure stage'), '—');
      assert.ok((await fixturePage.locator('#requestRecords .request-attempt').first().textContent()).includes('cloud_response_error'), 'per-attempt diagnostics remain visible');
      await fixturePage.close();
    }
    assert.deepEqual(errors, []);
    for (const [code, message] of [[503, 'Request ledger unavailable'], [404, 'Request observation is not connected yet']]) {
      await page.route('**/api/requests?*', route => route.fulfill({ status: code, contentType: 'application/json', body: '{}' }));
      await page.locator('#usageReload').click(); await page.waitForFunction(text => document.querySelector('#requestRecords').textContent === text, message); await page.unroute('**/api/requests?*');
      if (process.env.ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `statistics-error-${code}-${engine}.png`) });
    }
    await page.route('**/api/requests?*', route => route.fulfill({ status: 401, contentType: 'application/json', body: '{}' }));
    await page.locator('#usageReload').click(); await page.waitForFunction(() => document.querySelector('#requestRecords').textContent === 'Management token required');
    assert.equal(await page.locator('#requestRecords tbody tr').count(), 0); await page.unroute('**/api/requests?*');
    await page.locator('#usageReload').click();
    await page.waitForFunction(() => document.querySelectorAll('#requestRecords tbody tr').length === 50);
    service.kill('SIGTERM'); await exited;
    await page.locator('#usageReload').click(); await page.waitForFunction(() => document.querySelector('#requestRecords').textContent === 'Connection unavailable');
    assert.equal(await page.locator('#requestRecords tbody tr').count(), 0);
    assert.equal((await page.evaluate(() => JSON.stringify(localStorage))).includes(token), false);
  });
}
