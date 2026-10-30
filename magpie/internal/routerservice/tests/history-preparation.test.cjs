const {skipTutorial}=require('./returning-user.cjs');
const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs/promises'),os=require('node:os'),path=require('node:path'),net=require('node:net');
const {spawn,execFileSync}=require('node:child_process');
const {once}=require('node:events');
const {chromium,webkit}=require('playwright');
const binary=process.env.WEFTO_BINARY || path.resolve(__dirname,'../../../dist/wefto.exe');

for(const engine of (process.env.BROWSER?[process.env.BROWSER]:['chromium','webkit'])) {
 test(`explicit history preparation UI (${engine})`,{timeout:180000},async t=>{
  const taskHome=await fs.mkdtemp(path.join(os.tmpdir(),'wefto-history-ui-'));
  let service,exited,browser;
  t.after(async()=>{if(browser)await browser.close();if(service){if(service.exitCode===null)service.kill('SIGTERM');await exited;}await fs.rm(taskHome,{recursive:true,force:true});});
  const env={...process.env,HOME:taskHome,USERPROFILE:taskHome};
  for(const key of ['XDG_CONFIG_HOME','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_STATE_HOME','APPDATA','LOCALAPPDATA'])env[key]=path.join(taskHome,key);
  const layout=JSON.parse(execFileSync(binary,['dirs'],{env,encoding:'utf8'}));execFileSync(binary,['config','init'],{env});
  const reservation=net.createServer();reservation.listen(0,'127.0.0.1');await once(reservation,'listening');
  const base=`http://127.0.0.1:${reservation.address().port}`;await new Promise(resolve=>reservation.close(resolve));
  const configFile=path.join(layout.config,'config.json'),config=JSON.parse(await fs.readFile(configFile,'utf8'));config.listen.address=new URL(base).host;await fs.writeFile(configFile,JSON.stringify(config));
  service=spawn(binary,['serve','--no-experimental-redaction'],{env,stdio:['ignore','pipe','pipe'],windowsHide:true});exited=once(service,'exit');
  let output='';service.stdout.on('data',b=>output+=b);service.stderr.on('data',b=>output+=b);
  for(let i=0;i<200;i++){if(service.exitCode!==null)assert.fail(output);try{await fs.access(path.join(layout.state,'router.lock','management-token'));break;}catch{}await new Promise(resolve=>setTimeout(resolve,25));}
  browser=await(engine==='webkit'?webkit:chromium).launch();
  for(const language of ['zh','en','zh-TW','ja','de'])for(const width of [375,1600]){
   const page=await browser.newPage({viewport:{width,height:950}}),errors=[],actions=[];
   page.on('pageerror',e=>errors.push(e.message));
   // Explicit UI projection: backend authorization, inference and persistence
   // are exercised separately through the real Go HTTP stack.
   await page.route('**/api/redaction/history',async route=>{
    const input=route.request().postDataJSON();actions.push(input);
    await route.fulfill({json:{schemaVersion:1,id:'ui-fixture',cloudAttempts:0,progress:{totalMessages:1,completedMessages:input.action==='step'?1:0,totalCharacters:14,totalChunks:1,completedChunks:input.action==='step'?1:0,done:input.action==='step'}}});
   });
   await skipTutorial(page);await page.goto(`${base}/?locale=${language}&view=usage&tab=redaction`);
   await page.waitForFunction(()=>!!managementToken && !!serviceInfo);
   const panel=page.locator('#historyPreparation');await panel.locator('summary').click();
   if(language!=='en')assert.notEqual(await panel.locator('summary').innerText(),'Local history preparation');
   const source={model:'local-history-preparation',messages:[{role:'user',content:'Contact Elodie'}]};
   await panel.locator('[name=body]').fill('Contact Elodie');await panel.locator('[name=appId]').fill('codex');
   await panel.locator('button').nth(0).click();await page.waitForFunction(()=>document.querySelectorAll('#historyPreparation button')[1].disabled===false);
   assert.equal(actions.length,1);assert.deepEqual(actions[0].selected,[0]);assert.deepEqual(actions[0].body,source);
   await panel.locator('button').nth(1).click();await page.waitForFunction(()=>document.querySelector('#historyPreparation [role=status]').textContent.includes('1 / 1'));
   assert.equal(actions.length,2);assert.equal(actions[1].action,'step');assert.equal(await panel.locator('button').nth(1).isDisabled(),true);
   await panel.locator('button').nth(2).click();await page.waitForFunction(()=>document.querySelectorAll('#historyPreparation button')[2].disabled===true);
   assert.equal(actions[2].action,'cancel');assert.deepEqual(errors,[]);
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
   await page.close();
  }
 });
}
