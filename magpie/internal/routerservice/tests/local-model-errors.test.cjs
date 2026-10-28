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
const binary = process.env.WEFTO_BINARY || path.resolve(__dirname, '../../../dist/wefto');

test(`local model failures and response checks explain outcomes and recovery (${process.env.BROWSER || 'chromium'})`, { timeout: 180000 }, async t => {
  const home = await fs.mkdtemp(path.join(os.tmpdir(), 'api-router-install-errors-'));
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
  await fs.writeFile(configFile, JSON.stringify(config));
  service = spawn(binary, ['serve'], { env, stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true }); exited = once(service, 'exit');
  let output = '', token;
  service.stdout.on('data', chunk => { output += chunk; }); service.stderr.on('data', chunk => { output += chunk; });
  for (let i = 0; i < 200; i++) {
    if (service.exitCode !== null) assert.fail(output);
    try { token = (await fs.readFile(path.join(layout.state, 'router.lock/management-token'), 'utf8')).trim(); break; } catch {}
    await new Promise(resolve => setTimeout(resolve, 25));
  }
  assert.ok(token);
  const original = await (await fetch(base + '/api/local-model', { headers: { Authorization: 'Bearer ' + token } })).json();
  browser = await (process.env.BROWSER === 'webkit' ? webkit : chromium).launch();
  for (const language of ['zh', 'en', 'zh-TW', 'ja', 'de']) {
    for (const width of [1600, 375]) {
      const page = await browser.newPage({ locale: 'en-US', viewport: { width, height: 950 } });
      const errors = [], actions = [];
      page.on('pageerror', error => errors.push(error.message));
      page.on('request', request => {
        const pathname = new URL(request.url()).pathname;
        if (request.method() === 'POST' && /^\/api\/local-model\/(install|load|enable|select|disable)$/.test(pathname)) actions.push(pathname);
      });
      await page.route('**/api/local-model/detect', route => route.fulfill({ json: { schemaVersion: 1, device: { device: {}, recommendation: { tier: '4B' }, detection: { state: 'passed', reasons: [] } } } }));
      await page.addInitScript(() => {
        const originalFetch = window.fetch;
        window.fetch = (url, options) => url !== '/api/local-model/events' ? originalFetch(url, options) : Promise.resolve(new Response(new ReadableStream({
          start(controller) {
            window.__pushInstallStatus = status => controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ schemaVersion: 1, status })}\n\n`));
            options.signal.addEventListener('abort', () => { delete window.__pushInstallStatus; controller.error(new DOMException('Aborted', 'AbortError')); }, { once: true });
          }
        }), { headers: { 'Content-Type': 'text/event-stream' } }));
      });
      const snapshot = structuredClone(original);
      const failed = (stage, reason, extra = {}) => ({
        ...original.status, installation: 'failed', installingModel: 'spark-x2.6-4B', runtime: 'unloaded', pid: 0,
        intent: { ...original.status.intent, selected: 'spark-x2.6-4B', enabled: false }, dependencies: { state: 'ready' },
        error: `local_model_install_${stage}_${reason}`,
        progress: { artifact: 'Spark-X2.5-4B-template-0bcb35678590218655dff3765b9e61c83b35e9c4', stage, reason, downloaded: 4648, total: 4648, step: 4, totalSteps: 4, host: 'hf-mirror.com', ...extra },
      });
      snapshot.status = failed('template', 'integrity');
      await page.route('**/api/local-model', route => route.fulfill({ json: snapshot }));
      await skipTutorial(page); await page.goto(`${base}/?locale=${language}&view=local-model`);
      await page.locator('.local-status-error').waitFor();
      const message = async () => {
        const text = await page.locator('.local-status-error').textContent();
        assert.notEqual(text, await page.evaluate(() => t('Local model operation failed. Refresh to inspect status.')), 'backend install errors must explain the actual failure');
        assert.ok(!/\{\w+\}/.test(text), 'all translated placeholders are filled');
        return text;
      };
      assert.match(await message(), /SHA256/, 'the reported template integrity error names verification');
      const templateMessage = await message();
      const reuseKey = 'Verified model weights will be reused; only the chat template needs to be downloaded again.';
      assert.ok(templateMessage.includes(await page.evaluate(key => t(key), reuseKey)));
      if (language !== 'en') assert.notEqual(await page.evaluate(key => t(key), reuseKey), reuseKey, 'retry guidance is translated');
      assert.deepEqual(actions, [], 'reading an installation failure never starts a download');
      for (const stage of ['runtime', 'model', 'template', 'verify']) {
        const reasonMessages = new Set();
        for (const reason of ['timeout', 'interrupted', 'http_status', 'integrity', 'failed', 'busy']) {
          snapshot.status = failed(stage, reason, { step: 2, totalSteps: 3, host: 'modelscope.cn' });
          await page.evaluate(() => window.routerLocalOpen());
          const text = await message();
          assert.ok(text.includes('[2/3]'), 'errors use actual platform steps');
          assert.ok(text.includes('modelscope.cn'), 'errors use the source that actually failed');
          if (reason === 'integrity') assert.match(text, /SHA256/);
          reasonMessages.add(text);
        }
        assert.equal(reasonMessages.size, 6, 'different failure causes have different retry guidance');
      }
      // Older status payloads remain readable, including the old weights name.
      for (const code of ['local_model_template_corrupt', 'local_model_weights_timeout']) {
        snapshot.status = { ...failed('template', 'integrity'), error: code, progress: {} };
        await page.evaluate(() => window.routerLocalOpen()); await message();
      }
      snapshot.status = failed('template', 'integrity', { stage: 'runtime', step: 2, host: 'github.com' });
      await page.evaluate(() => window.routerLocalOpen());
      assert.ok((await message()).includes('[4/4]'), 'stale progress from another stage does not override the failed stage');
      assert.ok(!(await message()).includes('github.com'), 'stale progress does not misidentify the failed source');
      // A direct failed operation often has no progress; derive the stage
      // from its code and keep the retry guidance in the action error too.
      snapshot.status = { ...failed('template', 'integrity'), error: null, progress: {} };
      snapshot.status.intent.enabled = true;
      await page.route('**/api/local-model/install', route => route.fulfill({ status: 409, json: { error: { code: 'local_model_install_template_integrity' } } }));
      await page.evaluate(() => window.routerLocalOpen());
      await page.locator('#localModelContent [data-action=install]').click();
      await page.waitForFunction(() => document.querySelector('#localModelError').textContent.includes('SHA256'));
      assert.ok(!(await page.locator('#localModelError').textContent()).includes('hf-mirror.com'), 'an operation reply without progress does not invent a failed source');
      assert.equal(actions.filter(action => action === '/api/local-model/install').length, 1);
      // Response checks report progress, the actual outcome and a useful next
      // step immediately below the actions, including HTTP-success/slow results.
      snapshot.status = { ...original.status, installation: 'installed', runtime: 'idle', healthy: true, pid: 4242,
        intent: { ...original.status.intent, selected: 'spark-x2.6-4B', enabled: true }, dependencies: { state: 'ready' },
        performance: { state: 'unknown' }, error: null };
      await page.evaluate(() => window.routerLocalOpen());
      let finishCheck, checkStarted;
      await page.route('**/api/local-model/check', async route => {
        await new Promise(resolve => { finishCheck = resolve; checkStarted(); });
        await route.fulfill({ json: { schemaVersion: 1, status: snapshot.status } });
      });
      const checkButton = page.locator('#localModelContent [data-action=experience]');
      const feedback = page.locator('#localModelFeedback');
      const beginCheck = async () => {
        const arrived = new Promise(resolve => { checkStarted = resolve; });
        await checkButton.click();
        await arrived;
        await page.waitForFunction(() => document.querySelector('#localModelFeedback').dataset.state === 'checking');
      };
      await beginCheck();
      assert.equal(await checkButton.isDisabled(), true, 'a pending check cannot be submitted twice');
      snapshot.status.performance = { state: 'slow', tokensPerSecond: 2.4, firstTokenMs: 18100 };
      finishCheck();
      await page.waitForFunction(() => document.querySelector('#localModelFeedback').dataset.state === 'warning');
      assert.equal(await page.locator('#localModelFeedbackTitle').textContent(), await page.evaluate(() => t('Local responses are slow')));
      assert.match(await feedback.textContent(), /2\.4.*18\.1/);
      assert.equal(await page.locator('#localModelFeedbackNext').isVisible(), true);
      assert.equal(await feedback.getAttribute('aria-live'), 'polite');
      assert.equal(await page.evaluate(() => {
        const panel = document.querySelector('#localModelFeedback');
        return !!(panel.compareDocumentPosition(document.querySelector('#localModelLifecycle')) & Node.DOCUMENT_POSITION_FOLLOWING);
      }), true, 'the result precedes the low-priority lifecycle note');
      assert.ok(!(await feedback.textContent()).match(/\{\w+\}/), 'result placeholders are translated and filled');
      if (language !== 'en') assert.notEqual(await page.locator('#localModelFeedbackTitle').textContent(), 'Local responses are slow');
      for (const state of ['unknown', 'acceptable']) {
        await beginCheck();
        snapshot.status.performance = { state, ...(state === 'acceptable' ? { tokensPerSecond: 12, firstTokenMs: 800 } : {}) };
        finishCheck();
        await page.waitForFunction(expected => document.querySelector('#localModelFeedback').dataset.state === expected, state === 'acceptable' ? 'success' : 'warning');
        assert.equal(await page.locator('#localModelFeedbackMetrics').isVisible(), state === 'acceptable', 'unknown samples never reuse old measurements');
      }
      await page.unroute('**/api/local-model/check');
      await page.route('**/api/local-model/check', route => route.fulfill({ status: 409, json: { error: { code: 'capability_acceptance_suites_not_provided' } } }));
      await checkButton.click();
      await page.waitForFunction(() => document.querySelector('#localModelFeedback').dataset.state === 'error');
      assert.equal(await page.locator('#localModelFeedbackTitle').textContent(), await page.evaluate(() => t('Local response check failed')));
      assert.equal(await page.locator('#localModelError').textContent(), await page.evaluate(() => t('Capability acceptance suites have not been provided.')));
      assert.equal(await page.locator('#localModelFeedbackNext').isVisible(), true, 'failed checks explain what to do next');
      if (process.env.LOCAL_FEEDBACK_SCREENSHOTS && language === 'zh') await feedback.screenshot({ path: path.join(process.env.LOCAL_FEEDBACK_SCREENSHOTS, `local-feedback-${width}-failure.png`) });
      for (const [code, key] of [
        ['local_runtime_timed_out', 'The local operation timed out. Check the runtime state, close resource-heavy apps and retry.'],
        ['local_model_queue_full', 'The local model is busy or its queue is full. Wait for current requests to finish, then retry.'],
        ['local_model_process_fault', 'The local model process is unhealthy. Check process health above; if it remains faulty, turn the model off and on, then retry.'],
      ]) {
        await page.unroute('**/api/local-model/check');
        await page.route('**/api/local-model/check', route => route.fulfill({ status: 409, json: { error: { code } } }));
        await checkButton.click();
        await page.waitForFunction(expected => document.querySelector('#localModelError').textContent === t(expected), key);
        assert.equal(await feedback.getAttribute('data-state'), 'error');
        assert.equal(await page.locator('#localModelFeedbackMetrics').isVisible(), false, 'a failed check does not present an earlier success measurement');
      }
      await page.unroute('**/api/local-model/check');
      await page.route('**/api/local-model/check', route => route.fulfill({ json: { schemaVersion: 1, status: snapshot.status } }));
      await checkButton.click();
      await page.waitForFunction(() => document.querySelector('#localModelFeedback').dataset.state === 'success');
      assert.ok(!(await feedback.textContent()).includes(await page.evaluate(() => t('Local response check failed'))), 'retry replaces the old failure');
      if (process.env.LOCAL_FEEDBACK_SCREENSHOTS && language === 'zh') {
        snapshot.status.performance = { state: 'slow', tokensPerSecond: 2.4, firstTokenMs: 18100 };
        await page.evaluate(() => window.routerLocalOpen());
        await feedback.scrollIntoViewIfNeeded();
        await page.screenshot({ path: path.join(process.env.LOCAL_FEEDBACK_SCREENSHOTS, `local-feedback-${width}.png`) });
        await page.emulateMedia({ colorScheme: 'dark' });
        await page.screenshot({ path: path.join(process.env.LOCAL_FEEDBACK_SCREENSHOTS, `local-feedback-${width}-dark.png`) });
      }
      // A stream failure follows the same rendering path as the initial REST
      // snapshot; its completion refresh still observes the failed state.
      snapshot.status = { ...failed('template', 'integrity'), installation: 'downloading', error: null };
      await page.evaluate(() => window.routerLocalOpen());
      await page.waitForFunction(() => typeof window.__pushInstallStatus === 'function');
      snapshot.status = failed('template', 'integrity');
      await page.evaluate(status => window.__pushInstallStatus(status), snapshot.status);
      await page.locator('.local-status-error').waitFor();
      assert.equal(await message(), templateMessage);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false);
      assert.deepEqual(errors, []);
      await page.close();
    }
  }
});
