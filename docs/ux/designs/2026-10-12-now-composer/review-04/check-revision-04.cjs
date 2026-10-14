const {chromium}=require('C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
const fs=require('fs'),path=require('path');
(async()=>{
 const out='D:/Project/birdtie/docs/ux/designs/2026-10-12-now-composer/review-04';fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({headless:true,executablePath:'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'});
 const page=await browser.newPage({viewport:{width:900,height:1120},deviceScaleFactor:1});const errors=[],checks=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('http://127.0.0.1:8763/',{waitUntil:'networkidle'});const f=page.frameLocator('iframe');await f.locator('.bt-entity-marker').first().waitFor();
 const shot=async(n)=>{await f.locator('.bt-chrome svg.lucide').first().waitFor();await page.waitForTimeout(80);await f.locator('#birdtie-now-preview').screenshot({path:path.join(out,n+'.png')});};
 const map=()=>f.locator('[data-scene=map]').click(),dense=()=>f.locator('[data-scene=dense]').click(),close=()=>f.locator('.bt-sheet-title [data-action=close-result]').click();
 await shot('desktop-all-points');const originalIds=await f.locator('.bt-entity-marker').evaluateAll(es=>es.map(e=>e.dataset.object).sort());
 checks.push({name:'默认每对象一个标记，3地点和6动态，不存在聚合入口',passed:originalIds.length===9&&await f.locator('.bt-entity-marker[data-kind=place]').count()===3&&await f.locator('.bt-entity-marker[data-kind=moment]').count()===6&&await f.locator('[data-action=cluster]').count()===0});
 checks.push({name:'纯底图加载，署名保留，空态为浮起输入条',passed:await f.locator('.bt-current-map').evaluate(e=>e.complete&&e.naturalWidth>0)&&await f.locator('.bt-map-logo').isVisible()&&await f.locator('.bt-map-attribution').isVisible()&&await f.locator('.bt-sheet').evaluate(e=>getComputedStyle(e).backgroundColor==='rgba(0, 0, 0, 0)')});
 await dense();await shot('desktop-pale-points');const denseIds=await f.locator('.bt-entity-marker').evaluateAll(es=>es.map(e=>e.dataset.object).sort());
 checks.push({name:'淡点模式保留同一对象集合，不合并/隐藏，不需放大散开',passed:JSON.stringify(originalIds)===JSON.stringify(denseIds)&&await f.locator('.bt-marker-symbol').evaluateAll(es=>es.every(e=>Math.abs(e.getBoundingClientRect().width-9)<1))});
 checks.push({name:'地点圆形、动态小方形，9px淡点保留44px独立操作区',passed:await f.locator('.bt-entity-marker').evaluateAll(es=>es.every(e=>e.getBoundingClientRect().width>=44&&e.getBoundingClientRect().height>=44))});
 await f.locator('[data-object=moment-college-mia]').click();await shot('direct-moment');
 checks.push({name:'点击动态直接打开对应作者/内容，不先打开地点汇总',passed:(await f.locator('.bt-result-content').innerText()).includes('Mia')&&(await f.locator('.bt-result-content').innerText()).includes('建筑和树')&&(await f.locator('#bt-task-title').innerText()).includes('动态详情')&&await f.locator('.bt-modal').isHidden()});await close();
 await f.locator('[data-object=museum]').click();await shot('direct-place');
 checks.push({name:'点击地点直接显示该地点，不是记录或成员选择器',passed:(await f.locator('.bt-result-content').innerText()).includes('Shiprow')&&(await f.locator('#bt-task-title').innerText()).includes('地点详情')});await close();
 await map();await f.locator('#bt-input').fill('地图切点后保留这段草稿');await f.locator('.bt-keyboard-bar [data-action=hide-keyboard]').click();await f.locator('[data-object=moment-gardens-che]').click();await close();
 checks.push({name:'查看和关闭地点内容保留唯一输入框草稿',passed:await f.locator('#bt-input').inputValue()==='地图切点后保留这段草稿'&&await f.locator('#bt-input').count()===1});
 await map();await f.locator('[data-action=sidebar]').click();await shot('desktop-sidebar');checks.push({name:'左侧导航继续为侧边栏',passed:await f.locator('.bt-drawer').isVisible()&&await f.locator('.bt-modal').isHidden()});await f.locator('[data-action=close-drawer]').click();
 await f.locator('.bt-contact-rail [data-action=contacts]').click();await shot('desktop-contacts');await f.locator('.bt-recent-grid [data-person=lin]').click();checks.push({name:'右侧联系人继续进入独立聊天',passed:(await f.locator('#bt-task-title').innerText()).includes('小林')});await close();await map();
 await f.locator('[data-action=voice]').click();await shot('desktop-inline-voice');checks.push({name:'录音继续位于原输入框内，无额外弹窗',passed:await f.locator('.bt-recording').isVisible()&&await f.locator('.bt-modal').isHidden()});await f.locator('[data-action=cancel-voice]').click();
 for(const width of [392,352]){await page.setViewportSize({width,height:1120});await map();await shot(width===392?'mobile-all-points':'narrow-all-points');await dense();await shot(width===392?'mobile-pale-points':'narrow-pale-points');
 checks.push({name:(width-32)+'px 独立标记操作区不重叠且不越界',passed:await f.locator('.bt-entity-marker').evaluateAll(es=>{const p=es[0].closest('.bt-phone').getBoundingClientRect(),boxes=es.map(e=>e.getBoundingClientRect());return boxes.every(b=>b.left>=p.left&&b.right<=p.right&&b.top>=p.top&&b.bottom<=p.bottom)&&boxes.every((b,i)=>boxes.slice(i+1).every(c=>b.right<=c.left||c.right<=b.left||b.bottom<=c.top||c.bottom<=b.top));})});}
 await page.setViewportSize({width:392,height:1120});await f.locator('[data-object=moment-college-lin]').click();await shot('mobile-selected-pale');await close();await page.emulateMedia({colorScheme:'dark'});await dense();await shot('dark-pale-points');await f.locator('[data-object=college]').click();await shot('dark-place');
 const report={scope:'第四轮逐点展示设计稿；纯底图为一次静态渲染，全部动态演示，非原生产品验收',errors,checks,passed:!errors.length&&checks.every(x=>x.passed)};fs.writeFileSync(path.join(out,'verification.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));await browser.close();if(!report.passed)process.exitCode=1;
})();
