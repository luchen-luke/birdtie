import sys,json,subprocess,hashlib,re,time,queue,threading,argparse,datetime
from pathlib import Path
from urllib.parse import urlparse,parse_qs
W=Path(__file__).resolve().parent
sys.path.insert(0,str(W/'cm-web-deps'));sys.path.insert(0,str(W))
from playwright.sync_api import sync_playwright
sys.stdout.reconfigure(encoding='utf-8');sys.stdin.reconfigure(encoding='utf-8')
OUT=W.parent/'outputs';OUT.mkdir(exist_ok=True)
parser=argparse.ArgumentParser(description='Bounded Web34 reader/map acceptance; no content mutations')
parser.add_argument('--manifest-sha256',required=True)
parser.add_argument('--attempt',default='attempt01')
parser.add_argument('--api-sha256',required=True)
args=parser.parse_args()
if not re.fullmatch('[0-9a-f]{64}',args.api_sha256) or not re.fullmatch('[0-9a-f]{64}',args.manifest_sha256) or not re.fullmatch('[a-z0-9-]+',args.attempt):raise SystemExit('INVALID_ARGUMENTS')
EVID=Path(r'D:/Project/birdtie/docs/testing/evidence/content-map-refinement-20261016/city-viewports/web34-acceptance')/args.attempt
EVID.mkdir(parents=True,exist_ok=False)
ORIGIN='https://47.110.154.133'
private={'phone':None,'owner':None,'token':None};pages=[];metrics={'mapQueries':0,'memberQueries':0,'placeReads':0,'momentReads':0,'coverReads':0,'denials':0,'contentWrites':0,'blockedWrites':0,'failedRequests':0,'browserPageErrors':0};checks=[];events=[];details=[];assets={};media_counts={};marks={};screenshots=[]
metrics.update(markerReads=0,avatarReads=0,detailMediaReads=0,descriptorReads=0,auditMetadataReads=0)
targets={};playbacks={}
def safe(v):
 if isinstance(v,str):
  for p in private.values():
   if isinstance(p,str) and p:v=v.replace(p,'[PRIVATE]')
  v=re.sub(r'(?:Bearer\s+)[A-Za-z0-9._-]+','[PRIVATE]',v)
  return re.sub(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}','[ID]',v)
 if isinstance(v,list):return [safe(i) for i in v]
 if isinstance(v,dict):return {k:safe(i) for k,i in v.items()}
 return v
def emit(v):print(json.dumps(safe(v),ensure_ascii=False),flush=True)
def load_phone():
 from verify_reference167 import build_parts
 transport,code=build_parts()
 code=code.replace("target['schema']==167","target['schema']==173").replace('CURRENT167_TARGET_GUARD','CURRENT173_TARGET_GUARD')
 code+='\ndeployment=json.loads(pathlib.Path(\'/etc/birdtie/deployment.json\').read_bytes())\nrequire(hashlib.sha256((pathlib.Path(deployment[\'release\'])/\'bin/birdtie-api\').read_bytes()).hexdigest()=='+repr(args.api_sha256)+',\'EXACT_API30_BINARY\')\n'
 code+='\nowner,source_owner,phone,hashes=claimed_owner_phone()\nprint(json.dumps({"phone":phone,"owner":owner}))\n'
 p=subprocess.run(transport,input=code.encode(),capture_output=True,timeout=45,creationflags=subprocess.CREATE_NO_WINDOW)
 if p.returncode:raise RuntimeError('PINNED_OWNER_LOOKUP_FAILED')
 row=json.loads(p.stdout);private.update(phone=row['phone'],owner=row['owner'])
 if not re.fullmatch(r'\+[1-9][0-9]{7,14}',private['phone']):raise RuntimeError('PHONE_SHAPE_FAILED')
 row.clear()
def state():
 labels=page.locator('button,input,textarea,[role="button"],[role="textbox"],[role="menuitem"]').evaluate_all("els=>els.filter(e=>e.getBoundingClientRect().width&&e.getBoundingClientRect().height&&e.getBoundingClientRect().y<900).map(e=>({tag:e.tagName,role:e.getAttribute('role'),label:e.getAttribute('aria-label'),text:(e.innerText||'').slice(0,160),placeholder:e.getAttribute('placeholder'),type:e.getAttribute('type'),rect:{x:e.getBoundingClientRect().x,y:e.getBoundingClientRect().y,w:e.getBoundingClientRect().width,h:e.getBoundingClientRect().height}})).slice(0,120)")
 summary=[e for e in events if e.get('kind')=='viewport-response'][-4:]
 for row in pages[-4:]:
  body=row['data']; pins=body.get('items',body.get('pins',[]));summary.append({'members':row['members'],'zoom':body.get('zoom'),'kinds':{k:sum(i.get('kind')==k for i in pins) for k in ['PLACE_CONTENT','MOMENT_ANCHOR','CLUSTER']},'pinCount':len(pins),'totalContentCount':body.get('totalContentCount'),'totalAnchorCount':body.get('totalAnchorCount')})
 return {'urlPath':urlparse(page.url).path,'title':page.title(),'labels':labels,'mapContainers':page.locator('[id^="birdtie-mapbox-"]').count(),'metrics':metrics,'responses':summary}
def screenshot(name):
 if not re.fullmatch(r'cm-web34-[a-z0-9-]+',name):raise RuntimeError('SCREENSHOT_LABEL')
 if private['phone'] and private['phone'] in page.locator('body').inner_text():raise RuntimeError('SCREENSHOT_PRIVATE_TEXT')
 for v in page.locator('input,textarea').evaluate_all('es=>es.map(e=>e.value)'):
  if private['phone'] and private['phone'] in v:raise RuntimeError('SCREENSHOT_PRIVATE_INPUT')
 path=OUT/(name+'.png');page.screenshot(path=str(path),full_page=True);screenshots.append({'path':str(path),'sha256':hashlib.sha256(path.read_bytes()).hexdigest()});return str(path)
def response(r):
 try:
  u=urlparse(r.url)
  if u.netloc!='47.110.154.133':return
  path=u.path;query=parse_qs(u.query)
  if path.endswith('/display-descriptor'):
   metrics['descriptorReads']+=1;return
  if r.status in [401,403]:metrics['denials']+=1
  if path=='/v1/auth/dev-phone/verify' and r.status==200:
   private['token']=r.json().get('data',{}).get('accessToken')
  if '/v1/me/map-content' in path:
   if path.endswith('/content'):
    metrics['coverReads']+=1
    kind='avatarReads' if '/author-avatar/' in path else 'markerReads' if query.get('variant')==['marker'] else 'detailMediaReads'
    metrics[kind]+=1
    key=path+'?'+u.query;media_counts[key]=media_counts.get(key,0)+1
    if r.status==200:
     raw=r.body();assets[key]={'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw),'mime':r.headers.get('content-type',''),'kind':kind}
    return
   if '/places/' in path:metrics['placeReads']+=1
   elif '/moments/' in path:metrics['momentReads']+=1
   else:
    member='clusterId' in query;metrics['memberQueries' if member else 'mapQueries']+=1
    events.append({'kind':'viewport-response','status':r.status,'zoom':float(query.get('zoom',[0])[0]),'members':member})
   if r.status==200:
    body=r.json().get('data',{})
    if path=='/v1/me/map-content':
     pages.append({'members':'clusterId' in query,'data':body});del pages[:-40]
    elif '/moments/' in path:
     details.append(body);del details[:-40]
    elif '/places/' in path:
     details.extend(body.get('items',[]));del details[:-40]
   events.append({'kind':'map-content','status':r.status,'pathClass':'detail' if '/places/' in path or '/moments/' in path else 'viewport'})
 except Exception:events.append({'kind':'response-inspection-failed'})

def detail_proof():
 if not details:raise RuntimeError('NO_OBSERVED_DETAIL')
 detail=details[-1];author=detail.get('author') or {};name=author.get('displayName','')
 avatar=author.get('avatar');time_text=None
 stamp=detail.get('publishedAt') or (detail.get('createdAt') if detail.get('createdAtSource') in ['NATIVE','CIVU_ORIGINAL'] else None)
 if stamp:
  dt=datetime.datetime.fromisoformat(stamp.replace('Z','+00:00')).astimezone(datetime.timezone(datetime.timedelta(hours=8)))
  time_text=dt.strftime('%Y.%m.%d  %H:%M')
 body=page.locator('body').inner_text()
 result={'expectedAuthorPresent':bool(name),'authorVisible':bool(name and name in body),'timestampSource':detail.get('createdAtSource'),'originalTimeVisible':bool(time_text and time_text in body),'publishedAtPresent':bool(detail.get('publishedAt')),'avatarMetadataPresent':bool(avatar),'avatarSemanticVisible':page.get_by_label('作者头像',exact=True).count()>0,'exactSelectedEntry':detail.get('id')==private.get('targetMoment'),'sameOriginalMapDOM':page.evaluate("window.__cmOriginalMapElement===document.querySelector('[id^=birdtie-mapbox-]')"),'mediaCount':len(detail.get('media',[]))}
 if avatar:
  record=assets.get(avatar['thumbnailPath'])
  result['controlledAvatarReceived']=bool(record and record['sha256']==avatar['sha256'] and record['bytes']==avatar['byteCount'] and record['mime'].startswith('image/jpeg'))
 else:result['controlledAvatarReceived']=False
 return result

def guard(route):
 req=route.request;u=urlparse(req.url)
 if u.netloc=='47.110.154.133' and req.method not in ['GET','HEAD','OPTIONS'] and u.path not in ['/v1/auth/dev-phone/code','/v1/auth/dev-phone/verify','/v1/session/logout']:
  metrics['blockedWrites']+=1;route.abort();return
 route.continue_()
with sync_playwright() as p:
 browser=p.chromium.launch(executable_path=r'C:/Program Files/Google/Chrome/Application/chrome.exe',headless=True,args=['--enable-unsafe-swiftshader'])
 context=browser.new_context(viewport={'width':1280,'height':900},device_scale_factor=1,timezone_id='Asia/Shanghai')
 context.route('**/*',guard)
 page=context.new_page();page.on('requestfinished',lambda req: response(req.response()) if req.response() is not None else None)
 page.on('requestfailed',lambda r:metrics.__setitem__('failedRequests',metrics['failedRequests']+1))
 page.on('pageerror',lambda r:metrics.__setitem__('browserPageErrors',metrics['browserPageErrors']+1))
 try:
  manifest_response=context.request.get(ORIGIN+'/birdtie-web-manifest.json')
  manifest_bytes=manifest_response.body()
  manifest={k.replace(chr(92),'/'):v for k,v in json.loads(manifest_bytes).items()}
  canonical=hashlib.sha256(json.dumps(manifest,sort_keys=True,separators=(',',':')).encode()).hexdigest()
  if manifest_response.status!=200 or canonical!=args.manifest_sha256:raise RuntimeError('EXPECTED_WEB34_MANIFEST_MISMATCH')
  for asset in ['main.dart.js','index.html']:
   r=context.request.get(ORIGIN+'/'+asset)
   if r.status!=200 or hashlib.sha256(r.body()).hexdigest()!=manifest[asset]['sha256']:raise RuntimeError('EXPECTED_WEB34_ASSET_MISMATCH')
  checks.append({'check':'EXPECTED_WEB34_MANIFEST','passed':True})
  page.goto(ORIGIN,wait_until='networkidle',timeout=45000)
  page.wait_for_timeout(1000)
  if page.locator('flt-semantics-placeholder').count():page.locator('flt-semantics-placeholder').evaluate('(e)=>e.click()')
  page.wait_for_timeout(500)
  page.evaluate("window.__cmOriginalMapElement=document.querySelector('[id^=birdtie-mapbox-]')")
  emit({'status':'READY','state':state()})
  commands=queue.Queue()
  def read_commands():
   for line in sys.stdin:commands.put(line)
   commands.put('{"op":"done"}')
  threading.Thread(target=read_commands,daemon=True).start()
  while True:
   try:line=commands.get_nowait()
   except queue.Empty:
    page.wait_for_timeout(50);continue
   try:
    c=json.loads(line);op=c['op']
    if op=='state':page.wait_for_timeout(c.get('wait',0));emit(state())
    elif op=='shot':emit({'screenshot':screenshot(c['name'])})
    elif op=='click':
     if 'label' in c:page.get_by_role(c.get('role','button'),name=c['label'],exact=True).click(timeout=5000)
     elif 'text' in c:page.get_by_text(c['text'],exact=True).click(timeout=5000)
     else:page.mouse.click(c['x'],c['y'])
     page.wait_for_timeout(c.get('wait',1000));emit(state())
    elif op=='code':
     page.get_by_role('textbox',name='验证码',exact=True).fill('123456');emit({'fixedCodeFilled':True})
    elif op=='keyboard':
     if c.get('label'):page.get_by_role(c.get('role','button'),name=c['label'],exact=True).focus(timeout=5000)
     page.keyboard.press(c.get('key','Enter'));page.wait_for_timeout(c.get('wait',500));emit(state())
    elif op=='phone':
     load_phone();page.get_by_role('textbox',name='手机号',exact=True).fill(private['phone']);emit({'phoneFilled':True,'ownerGuard':'MATCHED_PREVIOUSLY_CLAIMED_OWNER'})
    elif op=='owner':
     if not private['token']:raise RuntimeError('NO_CAPTURED_SESSION_TOKEN')
     r=context.request.get(ORIGIN+'/v1/me',headers={'Authorization':'Bearer '+private['token']});d=r.json().get('data',{})
     valid=r.status==200 and d.get('id')==private['owner'];checks.append({'check':'PINNED_OWNER','passed':valid});emit({'ownerVerified':valid,'status':r.status})
    elif op=='wheel-check':
     before=metrics['mapQueries'];previous=next((i['data'].get('zoom') for i in reversed(pages) if not i['members']),None)
     map_node=page.locator('[id^="birdtie-mapbox-"]').element_handle()
     page.mouse.move(700,450)
     for _ in range(3):page.mouse.wheel(0,-100);page.wait_for_timeout(80)
     page.wait_for_timeout(1500)
     latest=next((i['data'].get('zoom') for i in reversed(pages) if not i['members']),None)
     result={'queriesBefore':before,'queriesAfter':metrics['mapQueries'],'delta':metrics['mapQueries']-before,'zoomBefore':previous,'zoomAfter':latest,'mapElementRetained':map_node.evaluate('e=>e===document.querySelector(\'[id^="birdtie-mapbox-"]\')')}
     checks.append({'check':'REAL_WHEEL_BURST_NEW_ZOOM_ONE_QUERY','passed':result['delta']==1 and latest is not None and previous is not None and latest>previous and result['mapElementRetained'],'metrics':result});emit(result)
    elif op=='wheel':page.mouse.move(c.get('x',800),c.get('y',300));page.mouse.wheel(0,c['dy']);page.wait_for_timeout(c.get('wait',1500));emit(state())
    elif op=='drag':page.mouse.move(c['x1'],c['y1']);page.mouse.down();page.mouse.move(c['x2'],c['y2'],steps=20);page.mouse.up();page.wait_for_timeout(c.get('wait',1500));emit(state())
    elif op=='marker':
     row=next(i for i in reversed(pages) if not i['members']);ps=row['data'].get('items',row['data'].get('pins',[]));candidates=[i for i in ps if i.get('kind')==c['kind']]
     pin=candidates[c.get('index',0)]
     coords=pin.get('coordinate') or pin.get('coordinates')
     emit({'markerShape':list(pin),'coordinateShape':list(coords) if isinstance(coords,dict) else type(coords).__name__})
    elif op=='point-click':
     pins=[];seen=set()
     for row in reversed(pages):
      if row['members']:continue
      for pin in row['data'].get('items',row['data'].get('pins',[])):
       marker=pin.get('markerId')
       if marker in seen or pin.get('kind')!=c.get('kind','MOMENT_ANCHOR'):continue
       seen.add(marker)
       loc=page.locator('[flt-semantics-identifier="birdtie-map-point:owned-map:'+marker+'"]')
       if loc.count() and loc.is_visible():pins.append((pin,loc))
     pin,loc=pins[c.get('index',0)]
     private['targetMoment']=pin.get('entryId') or pin.get('entityId')
     before=metrics['momentReads'];loc.click(timeout=5000);page.wait_for_timeout(c.get('wait',1500))
     emit({'semanticPointClick':True,'momentReadDelta':metrics['momentReads']-before,'state':state()})
    elif op=='city-targets':
     if not private['token']:raise RuntimeError('NO_CAPTURED_SESSION_TOKEN')
     headers={'Authorization':'Bearer '+private['token']}
     def audited(path):
      r=context.request.get(ORIGIN+path,headers=headers);metrics['auditMetadataReads']+=1
      if r.status!=200:raise RuntimeError('AUTHORIZED_TARGET_METADATA_UNAVAILABLE')
      return r.json()['data']
     catalog=audited('/v1/cities');cities={x['name']:x for x in catalog if x['name']in['上海市','汕头市']}
     if len(cities)!=2 or any(x.get('map',{}).get('provider')!='mapbox' for x in cities.values()):raise RuntimeError('REVIEWED_CITY_CONFIG_NOT_LIVE')
     from urllib.parse import urlencode,urlsplit,urlunsplit,parse_qsl
     pins=[];path='/v1/me/map-content?west=-180&south=-85&east=180&north=85&zoom=0';count=0
     while path:
      d=audited(path);pins+=d['pins'];count+=1
      if count>4:raise RuntimeError('TARGET_METADATA_PAGE_LIMIT')
      cursor=d.get('nextCursor')
      if cursor:
       u=urlsplit(d['viewportPath']);q=dict(parse_qsl(u.query));q['cursor']=cursor;path=urlunsplit(('', '',u.path,urlencode(q),''))
      else:path=None
     for name,city in cities.items():
      seen=set();reads=0
      for pin in pins:
       if pin.get('cityId')!=city['id']:continue
       for selector in pin.get('moments',[]):
        mid=selector['id']
        if mid in seen:continue
        seen.add(mid);reads+=1
        if reads>30:raise RuntimeError('CITY_TARGET_METADATA_LIMIT')
        d=audited('/v1/me/map-content/moments/'+mid)
        if any(m.get('kind')=='VIDEO' for m in d.get('media',[])):
         targets[name]={'city':city,'markerId':pin['markerId'],'momentId':mid,'title':d.get('title',''),'body':d.get('body','')};break
       if name in targets:break
     emit({'authorizedTargets':{name:{'existingVideo':True,'cityMapboxConfigured':True}for name in targets},'auditMetadataReads':metrics['auditMetadataReads'],'contentWrites':0})
    elif op=='video-target':
     target=targets[c['city']];private['targetMoment']=target['momentId']
     loc=page.locator('[flt-semantics-identifier="birdtie-map-point:owned-map:'+target['markerId']+'"]')
     box=loc.bounding_box()if loc.count()else None
     visible=bool(box and 0<=box['x']<=1232 and 0<=box['y']<=852 and loc.is_visible())
     if visible:loc.click(timeout=5000);page.wait_for_timeout(1200)
     emit({'city':c['city'],'targetMarkerVisible':visible,'clicked':visible,'state':state()})
    elif op=='target-row':
     target=targets[c['city']]
     # Use only text actually observed in the authorized current UI.
     label=target['title'] or target['body']
     matches=page.get_by_text(label,exact=True)
     if matches.count()!=1:raise RuntimeError('TARGET_ROW_NOT_UNIQUE_IN_OBSERVED_UI')
     matches.click(timeout=5000);page.wait_for_timeout(1200);emit(state())
    elif op=='playback-sample':
     values=page.locator('video').evaluate_all('es=>es.map(e=>({paused:e.paused,currentTime:e.currentTime,duration:Number.isFinite(e.duration)?e.duration:null,readyState:e.readyState,error:e.error?e.error.code:null,width:e.videoWidth,height:e.videoHeight}))')
     if len(values)!=1:raise RuntimeError('ONE_ACTUAL_PLAYER_REQUIRED')
     playbacks.setdefault(c['city'],[]).append(values[0]);emit({'city':c['city'],'playback':values[0]})
    elif op=='city-result':
     name=c['city'];samples=playbacks.get(name,[])
     advanced=len(samples)>=2 and samples[-1]['currentTime']>samples[0]['currentTime']+.2
     valid=advanced and all(x['error']is None and x['readyState']>=2 and x['width']>0 and x['height']>0 for x in samples)
     exact=any(d.get('id')==targets[name]['momentId']for d in details)
     same=page.evaluate("window.__cmOriginalMapElement===document.querySelector('[id^=birdtie-mapbox-]')")
     released=page.locator('video').count()==0
     proof={'city':name,'playbackAdvanced':advanced,'playerHealthy':valid,'exactAuthorizedEntryReadByUI':exact,'returnedSameMapDOM':same,'playerReleased':released}
     checks.append({'check':'CITY_VIDEO_'+name,'passed':bool(valid and exact and same and released),'proof':proof});emit(proof)
    elif op=='video-proof':
     emit({'videos':page.locator('video').evaluate_all('es=>es.map(e=>({paused:e.paused,currentTime:e.currentTime,duration:Number.isFinite(e.duration)?e.duration:null,readyState:e.readyState,networkState:e.networkState,error:e.error?e.error.code:null,width:e.videoWidth,height:e.videoHeight}))'),'sameOriginalMapDOM':page.evaluate("window.__cmOriginalMapElement===document.querySelector('[id^=birdtie-mapbox-]')")})
    elif op=='detail-proof':emit(detail_proof())
    elif op=='display-proof':
     if not details:raise RuntimeError('NO_OBSERVED_DETAIL')
     d=details[-1];media=d.get('media',[])
     images=[m for m in media if m.get('kind')=='IMAGE']
     proof=[]
     for m in images:
      path=m.get('contentPath','');record=assets.get(path);q=parse_qs(urlparse(path).query)
      proof.append({'displayVariant':q.get('variant')==['display'],'maxDimension':max(m.get('width',0),m.get('height',0)),'byteCount':m.get('byteCount'),'received':record is not None,'digestMatches':bool(record and record['sha256']==m.get('sha256')),'bytesMatch':bool(record and record['bytes']==m.get('byteCount')),'mimeJPEG':bool(record and record['mime'].startswith('image/jpeg'))})
     result={'images':proof,'author':detail_proof()};checks.append({'check':'DISPLAY_VARIANT_NETWORK_METADATA','passed':bool(proof) and all(i['displayVariant'] and i['maxDimension']<=1280 and i['byteCount']<=262144 and i['received'] and i['digestMatches'] and i['bytesMatch'] and i['mimeJPEG'] for i in proof),'proof':result});emit(result)
    elif op=='mark':marks[c['name']]={'metrics':dict(metrics),'media':dict(media_counts)};emit({'marked':c['name'],'metrics':metrics})
    elif op=='since':
     old=marks[c['name']];duplicate=sum(v-old['media'].get(k,0) for k,v in media_counts.items() if k in old['media'])
     emit({'since':c['name'],'delta':{k:metrics[k]-old['metrics'].get(k,0) for k in metrics},'repeatKnownMediaRequests':duplicate,'sameOriginalMapDOM':page.evaluate("window.__cmOriginalMapElement===document.querySelector('[id^=birdtie-mapbox-]')")})
    elif op=='eval':emit({'result':page.evaluate(c['js'])})
    elif op=='check':checks.append({'check':c['name'],'passed':c['passed'],'notes':c.get('notes','')});emit({'checkRecorded':c['name']})
    elif op=='done':break
    else:raise RuntimeError('UNKNOWN_OP')
   except Exception as e:emit({'status':'COMMAND_ERROR','type':type(e).__name__,'message':str(e)[:300] if not private['phone'] else 'CONTROLLED_COMMAND_FAILURE'})
 finally:
  logout='NOT_RUN_NO_SESSION'
  if private['token']:
   try:
    r=context.request.post(ORIGIN+'/v1/session/logout',headers={'Authorization':'Bearer '+private['token']});logout='PASS' if r.status in [200,204] else 'FAIL'
   except Exception:logout='FAIL'
  for city_name in ['上海市','汕头市']:
   if not any(x['check']=='CITY_VIDEO_'+city_name for x in checks):checks.append({'check':'CITY_VIDEO_'+city_name,'passed':False,'status':'NOT_RUN_OR_INCOMPLETE'})
  result={'status':('PASS' if checks and all(i['passed'] for i in checks) else 'FAIL'),'headless':True,'browserVersion':browser.version,'nonPersistentContext':True,'privateValuesPersisted':False,'contentWrites':metrics['contentWrites'],'checks':checks,'metrics':metrics,'events':events,'logout':logout,'release':{'web':34,'api':30,'schema':173,'manifestSHA256':args.manifest_sha256,'apiSHA256':args.api_sha256},'screenshots':screenshots,'playbacks':playbacks,'scope':'Two reviewed city entries and one existing authorized video per city; no Aberdeen repeat; old city-entry failure retained','notRun':['Screen recording','Publishing/editing/deleting content','Full release regression']}
  context.close();browser.close();private.clear();pages.clear();details.clear();assets.clear();media_counts.clear();targets.clear()
  (EVID/'receipt.json').write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8');emit(result)
