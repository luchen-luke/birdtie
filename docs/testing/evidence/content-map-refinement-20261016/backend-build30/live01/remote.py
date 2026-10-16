
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
 require(target['guard']and target['schema']==173 and target['contract']=='legacy-reference-media-v1'and target['runCommitted'],'CURRENT167_TARGET_GUARD')
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

EXPECTED_RELEASE='/opt/birdtie/releases/social173-20261016-build30'
EXPECTED_BINARY_SHA='4cc70262b66cffa0061a79fa91fdc7f622de23aedd5552a713b5e1070a7b9900'
# Runtime selectors remain only in memory. Only aggregate acceptance output leaves SSH.
def measured_json(path,token):
 parsed=urllib.parse.urlsplit(path)
 require(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.path.startswith('/v1/me/map-content/'),'EXACT_METADATA_ORIGIN')
 started=time.monotonic()
 with opener.open(urllib.request.Request(BASE+path,headers={'Authorization':'Bearer '+token}),timeout=30)as response:
  raw=response.read(4*1024**2+1)
  require(response.status==200 and len(raw)<=4*1024**2 and 'no-store'in response.headers.get('Cache-Control',''),'METADATA_STATUS_BOUND')
  value=json.loads(raw)['data']
 return value,{'elapsedMs':round((time.monotonic()-started)*1000,3),'httpResponseBytes':len(raw)}
def jpeg_dimensions(raw):
 require(raw[:2]==b'\xff\xd8','JPEG_SOI')
 pos=2
 while pos<len(raw):
  require(raw[pos]==255,'JPEG_MARKER')
  while pos<len(raw)and raw[pos]==255:pos+=1
  require(pos<len(raw),'JPEG_TRUNCATED');marker=raw[pos];pos+=1
  if marker in(0xd8,0xd9)or 0xd0<=marker<=0xd7:continue
  require(pos+2<=len(raw),'JPEG_SEGMENT');size=int.from_bytes(raw[pos:pos+2],'big')
  require(size>=2 and pos+size<=len(raw),'JPEG_SEGMENT_BOUND')
  if marker in(0xc0,0xc1,0xc2):
   require(size>=8,'JPEG_SOF');return int.from_bytes(raw[pos+5:pos+7],'big'),int.from_bytes(raw[pos+3:pos+5],'big')
  pos+=size
 raise RuntimeError('JPEG_DIMENSIONS_MISSING')
def display_read(item,token):
 started=time.monotonic()
 with opener.open(urllib.request.Request(BASE+item['contentPath'],headers={'Authorization':'Bearer '+token}),timeout=30)as response:
  raw=response.read(262145)
  require(response.status==200 and response.headers.get('Content-Type')=='image/jpeg'and response.headers.get('Content-Length')==str(item['byteCount']),'DISPLAY_HEADERS')
  require('no-store'in response.headers.get('Cache-Control','')and response.headers.get('X-Content-Type-Options')=='nosniff','DISPLAY_PRIVATE_HEADERS')
 require(len(raw)==item['byteCount']and len(raw)<=262144 and hashlib.sha256(raw).hexdigest()==item['sha256'],'DISPLAY_ACTUAL_HASH_BYTES')
 require(jpeg_dimensions(raw)==(item['width'],item['height']),'DISPLAY_ACTUAL_DIMENSIONS')
 return {'elapsedMs':round((time.monotonic()-started)*1000,3),'bytes':len(raw),'width':item['width'],'height':item['height'],'hashVerified':True,'actualJPEGDimensionsVerified':True}
def timestamp(s):return datetime.fromisoformat(s.replace('Z','+00:00'))
def current_lease(value):
 now=datetime.now(timezone.utc)
 require(timestamp(value['observedAt'])<=now<timestamp(value['validUntil']),'CURRENT_RESPONSE_LEASE')
def original_facts(mid,iid):
 require(re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}',mid)and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}',iid),'LOCAL_SELECTOR_UUIDS')
 return query("SELECT jsonb_build_object('revision',revision,'sha',derivative_sha256,'actualSHA',encode(sha256(derivative),'hex'),'bytes',octet_length(derivative))FROM private_moment_images WHERE id='"+iid+"'AND moment_id='"+mid+"'AND status='ready_private'")
owner=None;token=None;hashes=None;last_paths=[];login_attempted=False;stage='PREFLIGHT30'
result.update(status='FAIL',schema=173,apiBuild=30,contentWrites=0,sourceWrites=0,databaseDirectWrites=0,otherOriginalOwnerLogins=0,privateValuesPersisted=False)
try:
 deployment=json.loads(pathlib.Path('/etc/birdtie/deployment.json').read_bytes())
 require(deployment['oid']==54087 and deployment['release']==EXPECTED_RELEASE,'EXACT_API30_RELEASE')
 require(hashlib.sha256((pathlib.Path(EXPECTED_RELEASE)/'bin/birdtie-api').read_bytes()).hexdigest()==EXPECTED_BINARY_SHA,'EXACT_API30_BINARY')
 pid=int(__import__('subprocess').check_output(['systemctl','show','-p','MainPID','--value','birdtie-api.service']).decode().strip())
 process_exe=pathlib.Path('/proc/'+str(pid)+'/exe')
 require(pid>0 and process_exe.resolve()==pathlib.Path(EXPECTED_RELEASE)/'bin/birdtie-api'and hashlib.sha256(process_exe.read_bytes()).hexdigest()==EXPECTED_BINARY_SHA,'EXACT_RUNNING_API30_PROCESS')
 owner,source_owner,phone,hashes=claimed_owner_phone()
 # Same previously accepted phone image, bounded selector uniqueness query only.
 candidates=query("SELECT coalesce(jsonb_agg(row_to_json(t)),'[]')FROM(SELECT i.id,i.moment_id FROM private_moment_images i WHERE octet_length(i.derivative)=1519753 AND i.status='ready_private'AND EXISTS(SELECT 1 FROM(SELECT run_id,source_note_id,owner_account_id,moment_id,image_id FROM legacy_private_media_import_items UNION ALL SELECT run_id,source_note_id,owner_account_id,moment_id,image_id FROM legacy_private_image_recovery_items)imp JOIN legacy_native_adaptation_entities n ON n.run_id=imp.run_id AND n.kind='moment'AND n.source_key=imp.source_note_id::text AND n.target_id=imp.moment_id::text AND n.owner_account_id=imp.owner_account_id WHERE imp.run_id='"+RUN+"'AND imp.image_id=i.id AND imp.moment_id=i.moment_id AND imp.owner_account_id=i.owner_account_id)ORDER BY i.id LIMIT 2)t")
 require(len(candidates)==1,'EXACT_PREVIOUS_PHONE_IMAGE')
 mid=candidates[0]['moment_id'];iid=candidates[0]['id'];original=original_facts(mid,iid)
 require(original and original['bytes']==1519753 and original['sha']==original['actualSHA'],'ORIGINAL_STORED_HASH_MATCHES_BYTES')
 stage='CURRENT_OWNER_LOGIN';login_attempted=True;token=login(phone);del phone
 require(data('GET','/v1/me',token)['id']==owner,'EXACT_OWNER_BOUNDARY')
 stage='LAZY_DETAIL';detail_path='/v1/me/map-content/moments/'+mid+'?mediaMode=lazy'
 detail,detail_metrics=measured_json(detail_path,token);current_lease(detail)
 require(detail['viewerAccountId']==owner and detail['id']==mid and detail['schemaVersion']=='map-content-moment-v1','BOUND_LAZY_DETAIL')
 images=[x for x in detail['media']if x['id']==iid];require(len(images)==1,'EXACT_SELECTED_IMAGE')
 image=images[0]
 require(image['kind']=='IMAGE'and image['byteCount']==original['bytes']and image['sha256']==original['sha']and image['revision']==original['revision']and image['width']==5760 and image['height']==7680,'TRUE_ORIGINAL_LAZY_METADATA')
 expected_descriptor='/v1/me/map-content/moments/'+mid+'/media/'+iid+'/display-descriptor?sourceVersion='+image['sourceVersion']
 require(image.get('displayDescriptorPath')==expected_descriptor and image['contentPath']=='/v1/me/map-content/moments/'+mid+'/media/'+iid+'/content?sourceVersion='+image['sourceVersion'],'EXACT_LAZY_SELECTORS')
 require(detail.get('author')is not None and detail['createdAtSource']=='CIVU_ORIGINAL','AUTHOR_ORIGINAL_TIME_RETAINED')
 stage='ONE_DISPLAY_DESCRIPTOR';descriptor,descriptor_metrics=measured_json(expected_descriptor,token);current_lease(descriptor)
 require(set(descriptor)=={'schemaVersion','viewerAccountId','momentId','originalSourceVersion','observedAt','validUntil','media'}and descriptor['schemaVersion']=='map-content-media-display-v1','CLOSED_DESCRIPTOR_SCHEMA')
 require(descriptor['viewerAccountId']==owner and descriptor['momentId']==mid and descriptor['originalSourceVersion']==image['sourceVersion'],'EXACT_DESCRIPTOR_OWNER_SOURCE')
 derived=descriptor['media'];derived_source=hashlib.sha256(json.dumps([image['sourceVersion'],'display1280-v1'],separators=(',',':')).encode()).hexdigest()
 require(derived['id']==iid and derived['revision']==image['revision']and derived['sourceVersion']==derived_source and not derived.get('displayDescriptorPath'),'BOUND_DERIVED_MEDIA')
 require(derived['contentPath']=='/v1/me/map-content/moments/'+mid+'/media/'+iid+'/content?sourceVersion='+derived_source+'&variant=display','EXACT_DISPLAY_PATH')
 require(derived['kind']=='IMAGE'and derived['mimeType']=='image/jpeg'and 0<derived['byteCount']<=262144 and 0<derived['width']<=1280 and 0<derived['height']<=1280 and derived['width']*7680==derived['height']*5760,'BOUNDED_WHOLE_FRAME_DISPLAY')
 require(min(timestamp(detail['validUntil']),timestamp(descriptor['validUntil']))>datetime.now(timezone.utc),'DETAIL_DESCRIPTOR_LEASE_INTERSECTION')
 stage='ONE_DISPLAY_BYTES';binary_metrics=display_read(derived,token)
 last_paths=[detail_path,expected_descriptor,derived['contentPath']]
 stage='NEGATIVE_CURRENT_AUTHORITY'
 require(call('GET',expected_descriptor.replace(image['sourceVersion'],'0'*64),token)[0]in(403,404,409),'WRONG_ORIGINAL_SOURCE_DENIED')
 require(call('GET',derived['contentPath'].replace(derived_source,'0'*64),token)[0]in(403,404,409),'WRONG_DISPLAY_SOURCE_DENIED')
 require(call('GET',expected_descriptor.replace('/moments/'+mid+'/', '/moments/00000000-0000-0000-0000-000000000001/'),token)[0]in(403,404,409),'WRONG_MOMENT_BINDING_DENIED')
 require(all(call('GET',path)[0]in(401,403)for path in last_paths),'ANONYMOUS_LAZY_DESCRIPTOR_BYTES_DENIED')
 stage='ORIGINAL_UNCHANGED'
 require(original_facts(mid,iid)==original,'ORIGINAL_IMAGE_BYTES_AND_REVISION_UNCHANGED')
 require((hashlib.sha256(SOURCE.read_bytes()).hexdigest(),hashlib.sha256(MANIFEST.read_bytes()).hexdigest())==hashes,'ORIGINAL_BACKUPS_UNCHANGED')
 result.update(status='PASS',apiReleaseAndBinaryPinned=True,runningProcessPinned=True,selectedLegacyImageProvenanceBound=True,selectedImages=1,lazyDetail=detail_metrics,descriptor=descriptor_metrics,display=binary_metrics,originalBytes=original['bytes'],originalWidth=5760,originalHeight=7680,authorOriginalTimeRetained=True,originalImageBytesAndRevisionUnchanged=True,originalBackupsUnchanged=True,wrongOriginalAndDisplaySourceDenied=True,wrongMomentBindingDenied=True,viewerMatchesOnlyAuthorizedOwner=True,anonymousAllThreeDenied=True,realOtherOwnerLogin='NOT_RUN',serverCacheState='UNCONTROLLED_EXISTING_CACHE',endToEndPhone='NOT_RUN',fullSize200Media='NOT_RUN')
except Exception as e:
 result.update(status='FAIL',stage=stage,errorType=type(e).__name__,errorCode=str(e)if isinstance(e,RuntimeError)else'CONTROLLED_LAZY_ACCEPTANCE_FAILED')
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
 cleanup='ALL_RETURNED_SESSIONS_REVOKED'if closed and tokens else'NO_LOGIN_ATTEMPT'
 if login_attempted and not tokens:closed=False;cleanup='UNKNOWN_LOGIN_RESPONSE_UNAVAILABLE'
 if tokens and not closed:cleanup='LOGOUT_FAILED'
 result.update(allCreatedSessionsLoggedOut=closed,loggedOutAllThreeDenied=denied,sessionCleanup=cleanup)
 if not closed or(tokens and not denied):result['status']='FAIL'
print(json.dumps(result));raise SystemExit(0 if result['status']=='PASS'else 1)
