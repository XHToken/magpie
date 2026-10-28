const { skipTutorial } = require('./returning-user.cjs');
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

const binary = process.env.WEFTO_BINARY || process.env.API_ROUTER_BINARY || path.resolve(__dirname, '../../..', 'dist/wefto');
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ['chromium', 'webkit'])) {
  test(`model catalogue provenance (${engine})`, {timeout: 180000}, async t => {
    const home = await fs.mkdtemp(path.join(os.tmpdir(), 'api-router-catalogue-'));
    let service, exited, browser, cachedFails = false, generationCalls = 0;
    const upstream = http.createServer((req, res) => {
      if (req.method !== 'GET') generationCalls++;
      res.setHeader('Content-Type', 'application/json');
      if (req.url.startsWith('/preset/')) return res.writeHead(404).end('{"error":"private-upstream-body"}');
      if (req.url.startsWith('/cached/') && cachedFails) return res.writeHead(401).end('{"error":"private-upstream-body"}');
      res.end('{"data":[{"id":"listed-model"}]}');
    });
    t.after(async () => {
      if (browser) await browser.close();
      if (service) { if (service.exitCode === null) service.kill('SIGTERM'); await exited; }
      await new Promise(resolve => upstream.close(resolve));
      await fs.rm(home, {recursive: true, force: true});
    });
    upstream.listen(0, '127.0.0.1'); await once(upstream, 'listening');
    const env = {...process.env, HOME: home, USERPROFILE: home};
    for (const key of ['XDG_CONFIG_HOME','XDG_DATA_HOME','XDG_CACHE_HOME','XDG_STATE_HOME','APPDATA','LOCALAPPDATA']) env[key] = path.join(home,key);
    const layout = JSON.parse(execFileSync(binary,['dirs'],{env,encoding:'utf8'}));
    execFileSync(binary,['config','init'],{env});
    const reservation = net.createServer(); reservation.listen(0,'127.0.0.1'); await once(reservation,'listening');
    const address = `127.0.0.1:${reservation.address().port}`;
    await new Promise(resolve=>reservation.close(resolve));
    const configFile = path.join(layout.config,'config.json');
    const config = JSON.parse(await fs.readFile(configFile,'utf8')); config.listen.address=address;
    await fs.writeFile(configFile,JSON.stringify(config));
    service=spawn(binary,['serve'],{env,stdio:['ignore','pipe','pipe']});exited=once(service,'exit');
    service.stdout.resume();service.stderr.resume();
    let token;
    for(let i=0;i<200;i++){
      assert.equal(service.exitCode,null);
      try{token=(await fs.readFile(path.join(layout.state,'router.lock','management-token'),'utf8')).trim();break;}catch{}
      await new Promise(resolve=>setTimeout(resolve,25));
    }
    assert.ok(token);
    browser=await(engine==='webkit'?webkit:chromium).launch();
    const page=await browser.newPage();
    const api=async(method,url,body)=>{
      const response=await page.request.fetch(`http://${address}/api/cloud-providers${url}`,{method,headers:{Authorization:'Bearer '+token},...(body?{data:body}:{})});
      assert.equal(response.status(),200,await response.text());return response.json();
    };
    const ids={};
    for(const kind of ['healthy','cached','preset']){
      const result=await api('POST','',{name:kind,preset:kind==='preset'?'qwen-cn':'custom',protocol:'chat',baseURL:`http://127.0.0.1:${upstream.address().port}/${kind}/v1`,apiKey:'private-fixture-key',proxyMode:'direct'});
      ids[kind]=result.provider.id;
      await api('POST','/models',{id:ids[kind],persist:true,fallback:true});
    }
    cachedFails=true;
    const failed=await api('POST','/models',{id:ids.cached,persist:true,fallback:true});
    assert.equal(failed.source,'discovered');assert.equal(failed.fallback,true);assert.equal(failed.discoveryError,'model_list_auth_failed');
    const listed=await api('GET','');
    assert.equal(listed.providers.find(p=>p.id===ids.cached).modelListError,'model_list_auth_failed');
    const labels={healthy:'Discovered model list',cached:'Cached models; refresh failed',preset:'Preset models; availability is not verified'};
    const errors=[];page.on('pageerror',e=>errors.push(e.message));
    for(const language of ['zh','en','zh-TW','ja','de'])for(const width of [375,1440]){
      await page.setViewportSize({width,height:950});await skipTutorial(page);
      await page.goto(`http://${address}/?locale=${language}&view=agents`);
      await page.waitForFunction(()=>!!managementToken&&!!serviceInfo&&document.querySelectorAll('#agents .row.agent.conn').length===2);
      await page.locator('#agents [data-app="loomy"] .field.ag-menu').click();
      const pop=page.locator('.am-pop');await pop.waitFor({state:'visible'});
      for(const [kind,key] of Object.entries(labels)){
        const note=pop.locator(`[data-section="provider-${ids[kind]}"] .am-source`);
        await note.waitFor({state:'visible'});
        assert.equal(await note.textContent(),await page.evaluate(key=>t(key),key));
        if(language!=='en')assert.notEqual(await note.textContent(),key,'source note must be translated');
      }
      assert.equal(await pop.evaluate(n=>n.scrollWidth<=n.clientWidth+1),true,'source notes overflow picker');
      assert.equal((await page.locator('body').innerText()).includes('private-upstream-body'),false);
      if(process.env.ARTIFACT_DIR&&['zh','en'].includes(language)){
        await fs.mkdir(process.env.ARTIFACT_DIR,{recursive:true});
        await page.waitForFunction(()=>document.querySelector('.am-pop')?.getAnimations().every(a=>a.playState!=='running'));
        await page.screenshot({path:path.join(process.env.ARTIFACT_DIR,`catalogue-${engine}-${language}-${width}.png`)});
      }
      await page.keyboard.press('Escape');
    }
    // Saving a provider after a rejected refresh must not claim discovery succeeded.
    await page.goto(`http://${address}/?locale=en&view=providers`);
    await page.locator(`#providers [data-provider="${ids.cached}"] button`).click();
    await page.locator('#providerSave').click();
    await page.waitForFunction(()=>document.querySelector('#providerModal').hidden);
    await page.waitForFunction(()=>document.querySelector('#providerStatus').textContent.includes('Model discovery failed'));
    assert.equal(await page.locator('#providerStatus').textContent(),'Provider saved. Model discovery failed; using cached models.');
    assert.deepEqual(errors,[]);assert.equal(generationCalls,0);
  });
}
