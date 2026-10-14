const {chromium}=require('C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
const fs=require('fs'),path=require('path');
(async()=>{
 const out='D:/Project/birdtie/docs/ux/designs/2026-10-12-now-composer/review-03';
 const browser=await chromium.launch({headless:true,executablePath:'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'});
 const page=await browser.newPage({viewport:{width:392,height:1120},deviceScaleFactor:1});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('http://127.0.0.1:8763/',{waitUntil:'networkidle'});const f=page.frameLocator('iframe');await f.locator('.bt-chrome svg.lucide').first().waitFor();
 await f.locator('.bt-contact-rail [data-action=contacts]').click();await f.locator('.bt-recent-grid [data-person=lin]').click();
 for(let i=0;i<7;i++){await f.locator('#bt-input').fill('第'+(i+1)+'段演示消息：我们先在城市中心见面，再一起慢慢逛。具体出发时间确认以后，我会把计划整理给你。');await f.locator('[data-action=send]').click();}
 await f.locator('.bt-sheet-title [data-action=expand]').click();await page.waitForTimeout(60);
 const before=await f.locator('.bt-result-content').evaluate(el=>{el.scrollTop=112;return {top:el.scrollTop,overflow:el.scrollHeight>el.clientHeight};});
 await f.locator('[data-action=chat-contacts]').click();await f.locator('.bt-contact-row[data-person=che]').click();await f.locator('[data-action=chat-contacts]').click();await f.locator('.bt-contact-row[data-person=lin]').click();await page.waitForTimeout(80);
 const after=await f.locator('.bt-result-content').evaluate(el=>({top:el.scrollTop,expanded:el.closest('.bt-sheet').classList.contains('expanded')}));
 await f.locator('#birdtie-now-preview').screenshot({path:path.join(out,'chat-reading-restored.png')});
 const check={name:'长私人会话切联系人再返回，恢复展开状态和阅读位置',passed:before.overflow&&before.top>0&&Math.abs(before.top-after.top)<2&&after.expanded,details:{before,after}};
 await f.locator('#bt-input').fill('新的演示消息，发送后应显示在最新位置。');await f.locator('[data-action=send]').click();await page.waitForTimeout(80);const bottom=await f.locator('.bt-result-content').evaluate(el=>({top:el.scrollTop,max:el.scrollHeight-el.clientHeight}));const sendCheck={name:'恢复阅读位置后发送消息，滚到最新内容且不被旧回调改回',passed:Math.abs(bottom.top-bottom.max)<2,details:bottom};
 const report=JSON.parse(fs.readFileSync(path.join(out,'verification.json'),'utf8'));report.errors.push(...errors);report.checks=report.checks.filter(x=>x.name!==check.name&&x.name!==sendCheck.name);report.checks.push(check,sendCheck);report.passed=!report.errors.length&&report.checks.every(x=>x.passed);report.finalLocalFix='阅读位置补齐仅重测受影响的长私人会话流程';fs.writeFileSync(path.join(out,'verification.json'),JSON.stringify(report,null,2));console.log(JSON.stringify({check,sendCheck,errors,passed:report.passed},null,2));await browser.close();if(!check.passed||!sendCheck.passed)process.exitCode=1;
})();
