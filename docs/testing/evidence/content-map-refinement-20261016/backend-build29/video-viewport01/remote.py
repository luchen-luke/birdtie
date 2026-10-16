
import hashlib, json, os, pathlib, re, subprocess, urllib.error, urllib.parse, urllib.request

BASE='https://47.110.154.133'
DB='birdtie_native_hz_20261015'
RUN='c5df0d7c-f361-4275-9838-feb6d1d92352'
SYSTEM='hangzhou-civu-20261014'
SOURCE=pathlib.Path('/srv/birdtie-data/migration/source.json')
MANIFEST=pathlib.Path('/srv/birdtie-data/migration/manifest.json')
tokens=[]
result={'status':'FAIL','devOnly':True,'productionIdentityVerified':False,
        'productionPhoneVerification':False,'fixedCodeUsed':True,
        'sourceWrites':0,'databaseDirectWrites':0,'remoteConfigurationChanged':False,
        'agentActivationRequested':False,'oldSessionAccepted':False,
        'messagesSent':0,'privateValuesPersisted':False,'allCreatedSessionsLoggedOut':False}
stage='preflight'

def require(value, code):
 if not value: raise RuntimeError(code)

def query(sql):
 # The fixed owned target, a fresh read-only transaction, and stdin SQL avoid
 # arbitrary database selection or credentials in process arguments.
 p=subprocess.run(['runuser','-u','postgres','--','psql','-X','-A','-t','-q',
                   '-v','ON_ERROR_STOP=1','-d',DB],
                  input='BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\n'+sql+';\nROLLBACK;\n',
                  capture_output=True,text=True,timeout=12)
 require(p.returncode==0,'READ_ONLY_DATABASE_QUERY_FAILED')
 try: return json.loads(p.stdout.strip())
 except Exception: raise RuntimeError('READ_ONLY_DATABASE_RESULT_INVALID') from None

def call(method,path,token=None,value=None):
 headers={}
 if token: headers['Authorization']='Bearer '+token
 raw=None
 if value is not None:
  headers['Content-Type']='application/json';raw=json.dumps(value).encode()
 try:
  with urllib.request.urlopen(urllib.request.Request(BASE+path,data=raw,headers=headers,method=method),timeout=12) as response:
   data=response.read(10*1024*1024+1)
   require(len(data)<=10*1024*1024,'API_RESPONSE_TOO_LARGE')
   return response.status,None if not data else json.loads(data)
 except urllib.error.HTTPError as error:
  # Never echo a request URL, request body, API response, or private exception.
  return error.code,None

def data(method,path,token=None,value=None):
 code,body=call(method,path,token,value)
 require(code==200 and isinstance(body,dict) and 'data' in body,'API_EXPECTED_200')
 return body['data']

def login(phone):
 data('POST','/v1/auth/dev-phone/code',value={'phone':phone})
 response=data('POST','/v1/auth/dev-phone/verify',value={'phone':phone,'code':'123456'})
 token=response.get('accessToken')
 require(isinstance(token,str) and token,'CURRENT_TOKEN_MISSING')
 tokens.append(token)
 return token

def normalize(raw):
 require(isinstance(raw,str),'SOURCE_PHONE_NOT_STRING')
 phone=raw.strip().replace(' ','').replace('-','')
 if re.fullmatch(r'1[0-9]{10}',phone):phone='+86'+phone
 elif re.fullmatch(r'86[0-9]{11}',phone):phone='+'+phone
 require(re.fullmatch(r'\+[1-9][0-9]{7,14}',phone) is not None,'SOURCE_PHONE_NOT_CANONICAL')
 return phone

def page_all(path,token):
 items=[];cursor='';seen=set();pages=0
 while True:
  current=path+('&' if '?' in path else '?')+'cursor='+urllib.parse.quote(cursor,safe='') if cursor else path
  page=data('GET',current,token)
  require(isinstance(page.get('items'),list) and len(page['items'])<=20,'ARCHIVE_PAGE_INVALID')
  for item in page['items']:
   require(item['id'] not in seen,'ARCHIVE_DUPLICATE_ID');seen.add(item['id']);items.append(item)
  pages+=1;cursor=page.get('nextCursor','')
  require(pages<=20 and isinstance(cursor,str),'ARCHIVE_PAGING_UNBOUNDED')
  if not cursor:return items,pages

def state(owner):
 require(re.fullmatch(r'[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}',owner) is not None,'OWNER_NOT_UUID')
 return query("""SELECT jsonb_build_object(
 'enrolledOwners',(SELECT count(first_enrolled_at) FROM legacy_dev_phone_bindings),
 'bindingOwners',(SELECT count(*) FROM legacy_dev_phone_bindings),
 'importedActiveAccounts',(SELECT count(*) FROM accounts a JOIN legacy_entity_mappings m ON m.entity_kind='user' AND m.target_id=a.id WHERE a.status='active'),
 'importedActiveAgents',(SELECT count(*) FROM agents a JOIN legacy_entity_mappings m ON m.entity_kind='user' AND m.target_id=a.principal_account_id WHERE a.status<>'suspended'),
 'selectedAccountActive',(SELECT status='active' FROM accounts WHERE id='%s'),
 'selectedProfilePrivate',(SELECT visibility='private' FROM user_profiles WHERE account_id='%s'),
 'selectedAgents',(SELECT count(*) FROM agents WHERE principal_account_id='%s'),
 'selectedAgentsPaused',(SELECT count(*)>0 AND bool_and(status='suspended') FROM agents WHERE principal_account_id='%s'),
 'allMappingsUnclaimed',(SELECT bool_and(claim_status='UNCLAIMED') FROM legacy_entity_mappings),
 'selectedSessions',(SELECT count(*) FROM sessions WHERE account_id='%s'),
 'selectedLiveSessions',(SELECT count(*) FROM sessions WHERE account_id='%s' AND revoked_at IS NULL),
 'selectedDevSessions',(SELECT count(*) FROM sessions WHERE account_id='%s' AND authentication_method='dev_phone'),
 'firstFixedLoginAudits',(SELECT count(*) FROM audit_events WHERE purpose='FIRST_FIXED_TEST_LOGIN'),
 'selectedEnrolled',(SELECT first_enrolled_at IS NOT NULL FROM legacy_dev_phone_bindings WHERE owner_account_id='%s'))""" % ((owner,)*8))


EXPECTED_OWNER_SHA='3c2f9b23184f3dfb1b42e1dd0ac3cae7d86c55bc50c6a82d3c4b4a8c7528fc4a'

def claimed_owner_phone():
 require(SOURCE.stat().st_mode&0o777==0o600 and MANIFEST.stat().st_mode&0o777==0o600,'PRIVATE_SOURCE_MODE')
 source_raw=SOURCE.read_bytes();manifest_raw=MANIFEST.read_bytes()
 hashes=(hashlib.sha256(source_raw).hexdigest(),hashlib.sha256(manifest_raw).hexdigest())
 source=json.loads(source_raw);manifest=json.loads(manifest_raw)
 require(manifest['sourceFileSha256']==hashes[0]and source['source']['snapshotSha256']==manifest['source']['snapshotSha256'],'SOURCE_MANIFEST_BINDING')
 target=query("""SELECT jsonb_build_object('guard',current_database()='birdtie_native_hz_20261015'AND(SELECT oid=54087 AND pg_get_userbyid(datdba)='birdtie_native_hz'FROM pg_database WHERE datname=current_database()),
 'schema',(SELECT max(version)FROM birdtie_schema_migrations),'contract',birdtie_legacy_reference_media_contract(),
 'bindings',(SELECT coalesce(jsonb_agg(jsonb_build_object('owner',b.owner_account_id,'source',b.source_user_id,'phoneDigest',encode(b.phone_digest,'hex'),'sourceSHA',b.source_file_sha256,'manifestSHA',b.manifest_file_sha256,'snapshotSHA',b.snapshot_sha256,
 'mappingMatches',m.target_id=b.owner_account_id AND m.owner_account_id=b.owner_account_id AND m.run_id=b.run_id AND m.source_sha256=b.source_owner_sha256 AND m.source_version=b.source_owner_version AND m.source_status='active' AND m.claim_status='UNCLAIMED')),'[]')
 FROM legacy_dev_phone_bindings b JOIN legacy_entity_mappings m ON m.entity_kind='user'AND m.source_system=b.source_system AND m.source_id=b.source_user_id WHERE b.first_enrolled_at IS NOT NULL),
 'enrolledEmails',(SELECT count(*)FROM legacy_dev_email_bindings WHERE first_enrolled_at IS NOT NULL),
 'runCommitted',EXISTS(SELECT 1 FROM legacy_reference_media_runs WHERE run_id='%s'AND source_file_sha256='%s'AND manifest_file_sha256='%s'))"""%(RUN,hashes[0],hashes[1]))
 require(target['guard']and target['schema']==172 and target['contract']=='legacy-reference-media-v1'and target['runCommitted'],'CURRENT167_TARGET_GUARD')
 require(len(target['bindings'])==1 and target['enrolledEmails']==0,'EXACT_PREVIOUSLY_CLAIMED_PHONE_OWNER_ONLY')
 b=target['bindings'][0];owner=b['owner'];source_owner=b['source']
 require(re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}',owner)and hashlib.sha256(owner.encode()).hexdigest()==EXPECTED_OWNER_SHA,'FIRST_CLAIMED_OWNER_PIN')
 require(b['mappingMatches']is True and b['sourceSHA']==hashes[0]and b['manifestSHA']==hashes[1]and b['snapshotSHA']==source['source']['snapshotSha256'],'CURRENT_ORIGINAL_OWNER_BINDING')
 identities=[i for i in source['tables']['auth_identities']if i['provider']=='phone'and i['user_id']==source_owner]
 require(len(identities)==1,'EXACT_ORIGINAL_OWNER_PHONE');phone=normalize(identities[0]['subject'])
 require(hashlib.sha256(('birdtie-dev-phone:'+phone).encode()).hexdigest()==b['phoneDigest'],'CANONICAL_PHONE_DIGEST')
 return owner,source_owner,phone,hashes

from datetime import datetime,timezone
import time
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,req,fp,code,msg,headers,newurl):return None
opener=urllib.request.build_opener(NoRedirect())
def call(method,path,token=None,value=None):
 require(BASE=='https://47.110.154.133' and path.startswith('/v1/') and not path.startswith('//'),'EXACT_HTTPS_ORIGIN')
 require(method=='GET' or(method=='POST' and path in('/v1/auth/dev-phone/code','/v1/auth/dev-phone/verify','/v1/session/logout')),'READ_ONLY_API_SCOPE')
 h={};raw=None
 if token:h['Authorization']='Bearer '+token
 if value is not None:h['Content-Type']='application/json';raw=json.dumps(value).encode()
 try:
  with opener.open(urllib.request.Request(BASE+path,data=raw,headers=h,method=method),timeout=30)as response:
   body=response.read(4*1024**2+1);require(len(body)<=4*1024**2,'RESPONSE_BOUND');return response.status,None if not body else json.loads(body)
 except urllib.error.HTTPError as e:return e.code,None
def content_read(item,token):
 path=item['contentPath'];parsed=urllib.parse.urlsplit(path)
 require(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.path.startswith('/v1/me/map-content/'),'EXACT_CONTENT_ORIGIN')
 require(isinstance(item['byteCount'],int)and 0<item['byteCount']<=64*1024**2,'MEDIA_BOUND')
 total=0;sha=hashlib.sha256()
 with opener.open(urllib.request.Request(BASE+path,headers={'Authorization':'Bearer '+token}),timeout=30)as response:
  require(response.status==200 and response.headers.get('Content-Type')==item['mimeType'] and response.headers.get('Content-Length')==str(item['byteCount']),'MEDIA_HEADERS')
  require('no-store'in response.headers.get('Cache-Control','')and response.headers.get('X-Content-Type-Options')=='nosniff','MEDIA_CACHE_BOUNDARY')
  while True:
   chunk=response.read(65536)
   if not chunk:break
   total+=len(chunk);require(total<=item['byteCount'],'MEDIA_LENGTH');sha.update(chunk)
 require(total==item['byteCount']and sha.hexdigest()==item['sha256'],'MEDIA_INTEGRITY')
 return total
def measured_get(path,token):
 parsed=urllib.parse.urlsplit(path)
 require(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.path=='/v1/me/map-content','EXACT_MAP_PATH')
 started=time.monotonic()
 with opener.open(urllib.request.Request(BASE+path,headers={'Authorization':'Bearer '+token}),timeout=30)as response:
  raw=response.read(4*1024**2+1);require(response.status==200 and len(raw)<=4*1024**2,'BOUNDED_MAP_RESPONSE')
  require('no-store'in response.headers.get('Cache-Control',''),'MAP_NO_STORE')
  payload=json.loads(raw);view=payload['data']
 elapsed=round((time.monotonic()-started)*1000,3)
 require(view['schemaVersion']=='map-content-v2'and view['scope']=='MAP_CONTENT'and view['viewerAccountId']==owner and view['filterScope']=='DEFAULT_CONTENT','CURRENT_CONTENT_MAP')
 pins=view['pins'];require(isinstance(pins,list)and len(pins)<=40 and not view['truncated'],'MARKER_BUDGET')
 require(len({p['markerId']for p in pins})==len(pins),'UNIQUE_MARKERS')
 require(view['totalAnchorCount']>=len(pins)and view['totalContentCount']>=0,'COMPLETE_TOTALS')
 require(all(p['momentCount']>0 and p['kind']in('PLACE_CONTENT','MOMENT_ANCHOR','CLUSTER')for p in pins),'NO_EMPTY_POI')
 covers=[p['cover']for p in pins if p['cover']is not None];require(len(covers)<=12,'COVER_BUDGET')
 for cover in covers:
  url=urllib.parse.urlsplit(cover['contentPath']);args=urllib.parse.parse_qs(url.query)
  require(not url.scheme and not url.netloc and url.path.startswith('/v1/me/map-content/')and args.get('variant')==['marker'],'MARKER_VARIANT')
  require(cover['mimeType']=='image/jpeg'and 0<cover['width']<=160 and 0<cover['height']<=160 and 0<cover['byteCount']<=262144,'REAL_SMALL_THUMBNAIL')
 measurement={'elapsedMs':elapsed,'responseJSONBytes':len(json.dumps(view,ensure_ascii=False,separators=(',',':')).encode()),'httpResponseBytes':len(raw),'markers':len(pins),'withContent':sum(p['momentCount']>0 for p in pins),'totalContentCount':view['totalContentCount'],'totalAnchorCount':view['totalAnchorCount'],'covers':len(covers),'advertisedCoverBytes':sum(c['byteCount']for c in covers),'kindCounts':{k:sum(p['kind']==k for p in pins)for k in('PLACE_CONTENT','MOMENT_ANCHOR','CLUSTER')}}
 return view,measurement
def page_path(path,cursor=None,selected=None):
 u=urllib.parse.urlsplit(path);require(not u.scheme and not u.netloc and not u.fragment and u.path=='/v1/me/map-content','EXACT_MEMBERS_ORIGIN')
 args=urllib.parse.parse_qs(u.query);require(all(len(v)==1 for v in args.values())and set(args)<=set(('cityId','west','south','east','north','zoom','filterScope','clusterId','cursor','sourceVersion','selectedMarkerId')),'CLOSED_MEMBERS_QUERY')
 if cursor:args['cursor']=[cursor]
 if selected:args['selectedMarkerId']=[selected]
 return u.path+'?'+urllib.parse.urlencode({k:v[0]for k,v in args.items()})


owner=None;token=None;hashes=None;last_paths=[];stage='PREFLIGHT29';private_locator=None
result.update(status='FAIL',schema=172,apiBuild=29,contentWrites=0,sourceWrites=0,databaseDirectWrites=0)
try:
 owner,source_owner,phone,hashes=claimed_owner_phone();deployment=json.loads(pathlib.Path('/etc/birdtie/deployment.json').read_bytes())
 require(deployment['oid']==54087 and deployment['release']=='/opt/birdtie/releases/social172-20261015-build29','EXACT_API29_RELEASE')
 require(hashlib.sha256((pathlib.Path(deployment['release'])/'bin/birdtie-api').read_bytes()).hexdigest()=='73a97552316ae642f4b97aea37c04266c3fd0959eeb0d346dd03bc6e54702818','EXACT_API29_BINARY')
 originals=query("SELECT coalesce(jsonb_agg(row_to_json(t)),'[]')FROM(SELECT id,moment_id,derivative_sha256,octet_length(derivative)bytes FROM private_moment_images WHERE octet_length(derivative)=1519753 AND status='ready_private')t")
 require(len(originals)>0,'KNOWN_PHONE_IMAGE_PRESENT')
 stage='CURRENT_OWNER_LOGIN';token=login(phone);del phone;require(data('GET','/v1/me',token)['id']==owner,'EXACT_OWNER_BOUNDARY')
 stage='PUBLIC_CITY_CATALOG';catalog=data('GET','/v1/cities',token);require(isinstance(catalog,list),'CITY_CATALOG_LIST');cities={c['id']:c for c in catalog}
 stage='VISIBLE_MAP_VIDEO_INVENTORY';pins=[];cursor=None;source=None;pages=0
 world_path='/v1/me/map-content?'+urllib.parse.urlencode({'west':-180,'south':-85,'east':180,'north':85,'zoom':0});next_path=world_path
 while True:
  view,metrics=measured_get(next_path,token);pages+=1
  require(pages<=20,'BOUNDED_WORLD_PAGES')
  if source is None:source=view['sourceVersion'];total=view['totalPointCount']
  require(view['sourceVersion']==source,'WORLD_SOURCE_STABLE');pins.extend(view['pins'])
  if not view.get('nextCursor'):break
  next_path=page_path(view['viewportPath'],view['nextCursor'])
 require(len(pins)==total and len({p['markerId']for p in pins})==total,'COMPLETE_WORLD_POINT_INVENTORY')
 require(all(p['kind']!='CLUSTER'and not p['momentsTruncated']for p in pins),'COMPLETE_LEAF_SELECTOR_INVENTORY')
 accessible={m['id']for p in pins for m in p['moments']}
 candidates=query("SELECT coalesce(jsonb_agg(DISTINCT moment_id),'[]')FROM(SELECT moment_id FROM private_moment_videos WHERE status='ready_private'AND retain_until>clock_timestamp()UNION SELECT moment_id FROM public_moment_media_attachments WHERE metadata->>'kind'='VIDEO'AND retention_until>clock_timestamp())v")

 reached=[];videos=0;checks=[];video_cities=set()
 def valid_viewport(city):
  v=city.get('map')if city else None
  return isinstance(v,dict)and v.get('provider')in('mapbox','amap')and all(isinstance(v.get(k),(int,float))for k in('latitude','longitude','defaultZoom'))and -90<=v['latitude']<=90 and -180<=v['longitude']<=180 and 0<=v['defaultZoom']<=24
 for mid in candidates:
  if mid not in accessible:continue
  path='/v1/me/map-content/moments/'+mid;detail=data('GET',path,token);items=[x for x in detail['media']if x['kind']=='VIDEO']
  if not items:continue
  reached.append(mid);videos+=len(items);target_pins=[p for p in pins if any(m['id']==mid for m in p['moments'])];matched=[(pin,cities.get(pin['cityId']))for pin in target_pins];video_cities.update(pin['cityId']for pin in target_pins)
  configured=[(pin,city)for pin,city in matched if valid_viewport(city)]
  checks.append({'targetIndex':len(reached),'mediaCount':len(items),'mapPointCount':len(target_pins),'cityCatalogMatches':sum(city is not None for pin,city in matched),'hasViewport':bool(configured),'configuredPoints':len(configured),'providers':sorted({city['map']['provider']for pin,city in configured})})
  last_paths.append(path)
  if configured and private_locator is None:
   pin,city=configured[0];private_locator={'detailPath':path,'markerId':pin['markerId'],'kind':pin['kind'],'cityId':pin['cityId'],'cityName':city['name'],'cityMap':city['map'],'anchor':pin['anchor'],'videoMediaId':items[0]['id']}
 require(len(reached)==4 and videos==4,'SAME_FOUR_CURRENT_MAP_VIDEOS')
 require((hashlib.sha256(SOURCE.read_bytes()).hexdigest(),hashlib.sha256(MANIFEST.read_bytes()).hexdigest())==hashes,'SOURCE_UNCHANGED')
 result.update(status='PASS',originalSourcesUnchanged=True,worldPages=pages,worldTruePoints=total,worldUniqueContent=len(accessible),accessibleVideoMoments=len(reached),accessibleVideos=videos,distinctVideoCities=len(video_cities),targets=checks,videoTargetsWithValidViewport=sum(x['hasViewport']for x in checks),videoPlayback='NOT_RUN',uiVideoEntry='READY_FOR_CHECK'if private_locator is not None else'BLOCKED_NO_CONFIGURED_CITY_VIEWPORT',privateLocatorSaved=private_locator is not None,privateValuesPersisted=private_locator is not None)
except Exception as e:result.update(status='FAIL',stage=stage,errorType=type(e).__name__,errorCode=str(e)if isinstance(e,RuntimeError)else'CONTROLLED_DISPLAY_ACCEPTANCE_FAILED')
finally:
 closed=True
 for current_token in tokens:
  ok=False
  for attempt in range(3):
   try:ok=call('POST','/v1/session/logout',current_token)[0]in(200,204)
   except Exception:pass
   if ok:break
   time.sleep(1)
  closed=closed and ok
 denied=False
 if closed and tokens and last_paths:
  try:denied=all(call('GET',p,tokens[-1])[0]in(401,403)for p in last_paths)
  except Exception:pass
 result.update(allCreatedSessionsLoggedOut=closed,loggedOutReadsDenied=denied)
 if not closed or not denied:result['status']='FAIL'
if private_locator is not None:result['_privateLocator']=private_locator
print(json.dumps(result));raise SystemExit(0 if result['status']=='PASS'else 1)
