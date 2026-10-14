const fs=require('fs');
const {chromium}=require('C:/Users/chens/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
(async()=>{
 const config=JSON.parse(fs.readFileSync('D:/Project/birdtie/apps/client/.env.maps.mobile.local.json','utf8'));
 const token=config.BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN;if(!token||!token.startsWith('pk.'))throw new Error('Public map configuration unavailable');
 const resp=await fetch('https://api.mapbox.com/mapbox-gl-js/v3.15.0/mapbox-gl.js');if(!resp.ok)throw new Error('Map renderer unavailable: '+resp.status);const script=await resp.text();
 const browser=await chromium.launch({headless:true,executablePath:'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',args:['--use-gl=angle','--use-angle=swiftshader-webgl','--enable-webgl']});
 try{
  const page=await browser.newPage({viewport:{width:390,height:849},deviceScaleFactor:2});
  await page.setContent('<div id="map" style="position:absolute;inset:0"></div><style>html,body{margin:0}.mapboxgl-canvas{position:absolute;inset:0}.mapboxgl-control-container{display:none}</style>');
  await page.addScriptTag({content:script});
  await page.evaluate(t=>{mapboxgl.accessToken=t;window.mapErrors=[];window.mapReady=false;window.baseMap=new mapboxgl.Map({container:'map',style:'mapbox://styles/lookluo/cmth7kwad001p01ssc9kw7kco',center:[-2.089,57.155],zoom:12.65,bearing:0,pitch:0,interactive:false,preserveDrawingBuffer:true,attributionControl:false,fadeDuration:0});baseMap.on('error',e=>{mapErrors.push(String(e.error?.message||'Map resource error').replace(/access_token=[^&\s]+/g,'access_token=REDACTED'));});baseMap.on('idle',()=>window.mapReady=true);},token);
  await page.waitForFunction(()=>window.mapReady,{timeout:45000});
  const stats=await page.evaluate(()=>({loaded:baseMap.loaded(),layers:baseMap.getStyle().layers.length,errors:mapErrors.map(x=>x.slice(0,120))}));
  await page.locator('#map').screenshot({path:'C:/Users/chens/.codex/visualizations/2026/10/12/01a12f37-35ca-74a2-bba1-8e9d69b940e3/now-basemap-separate.png'});console.log(JSON.stringify(stats));
 }finally{await browser.close();}
})().catch(e=>{console.error(String(e.message).replace(/access_token=[^&\s]+/g,'access_token=REDACTED'));process.exitCode=1;});
