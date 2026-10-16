from pathlib import Path
import ast,hashlib,importlib.util,json,subprocess,sys
w=Path(__file__).parent;r=Path(r'D:/Project/birdtie')
spec=importlib.util.spec_from_file_location('base',w/'verify_reference167.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);ssh,prefix=m.build_parts();prefix=prefix.replace("target['schema']==167","target['schema']==172")
helpers=(w/'verify_map170.remote.py').read_text().split("stage='PREFLIGHT'")[0]+(w/'cm_refinement_live_helpers.py').read_text()
body=r'''
owner=None;token=None;hashes=None;last_paths=[];stage='PREFLIGHT28'
result.update(status='FAIL',schema=172,apiBuild=28,contentWrites=0,sourceWrites=0,databaseDirectWrites=0)
try:
 owner,source_owner,phone,hashes=claimed_owner_phone();deployment=json.loads(pathlib.Path('/etc/birdtie/deployment.json').read_bytes())
 require(deployment['oid']==54087 and deployment['release']=='/opt/birdtie/releases/social172-20261015-build28','EXACT_API28_RELEASE')
 require(hashlib.sha256((pathlib.Path(deployment['release'])/'bin/birdtie-api').read_bytes()).hexdigest()=='9c4919d1f98916319c054e9b94a2fd6b4c93f161ff64a160ee1f1d609c9ffc43','EXACT_API28_BINARY')
 source=json.loads(SOURCE.read_bytes())['tables'];users={u['id']:u for u in source['users']};notes={n['id']:n for n in source['notes']}
 facts=query("SELECT jsonb_agg(row_to_json(t))FROM(SELECT DISTINCT ON(a.source_user_id)a.source_user_id,a.owner_account_id,a.source_media_id,a.thumbnail_sha256,octet_length(a.thumbnail)avatar_bytes,f.moment_id,f.source_note_id FROM legacy_moment_author_snapshots a JOIN legacy_moment_author_facts f USING(run_id,source_user_id)WHERE a.run_id='"+RUN+"'ORDER BY a.source_user_id,f.moment_id)t")
 require(len(facts)==9 and sum(f['source_media_id']is not None for f in facts)==6,'COMPLETE_ORIGINAL_AUTHOR_PROJECTION')
 stage='CURRENT_OWNER_LOGIN';token=login(phone);del phone;require(data('GET','/v1/me',token)['id']==owner,'EXACT_OWNER_BOUNDARY')
 stage='ORIGINAL_ATTRIBUTION_AND_AVATARS';checked=0;avatars=0;totalbytes=0;foreign=0;self_current=0;dates=0
 def date(v):return datetime.fromisoformat(v.replace('Z','+00:00'))
 for f in facts:
  path='/v1/me/map-content/moments/'+f['moment_id'];detail=data('GET',path,token);note=notes[f['source_note_id']];user=users[f['source_user_id']]
  require(note['author_id']==user['id'] and detail['id']==f['moment_id'] and detail['author']['accountId']==f['owner_account_id'],'EXACT_ORIGINAL_AUTHOR_BINDING')
  if detail['visibilitySource']=='CIVU_PUBLIC_MOMENT':
   foreign+=1;require(detail['author']['displayName']==user['display_name'],'ORIGINAL_PUBLIC_AUTHOR_NAME')
   av=detail['author']['avatar']
   if f['source_media_id']is None:require(av is None,'UNPRESERVED_AVATAR_NOT_INVENTED')
   else:
    require(av is not None and av['accountId']==f['owner_account_id']and av['version']==user['version']and av['width']==128 and av['height']==128 and av['byteCount']==f['avatar_bytes']and av['sha256']==f['thumbnail_sha256'],'EXACT_AUTHOR_AVATAR_METADATA')
    u=urllib.parse.urlsplit(av['thumbnailPath']);query_args=urllib.parse.parse_qs(u.query)
    require(not u.scheme and not u.netloc and not u.fragment and u.path=='/v1/me/map-content/moments/'+detail['id']+'/author-avatar/'+f['source_media_id']+'/content'and set(query_args)=={'sourceVersion'}and len(query_args['sourceVersion'])==1 and re.fullmatch('[0-9a-f]{64}',query_args['sourceVersion'][0]),'EXACT_MOMENT_AVATAR_PATH')
    totalbytes+=content_read({'contentPath':av['thumbnailPath'],'mimeType':'image/jpeg','byteCount':av['byteCount'],'sha256':av['sha256']},token);avatars+=1;last_paths.append(av['thumbnailPath'])
    require(call('GET',av['thumbnailPath'].replace(query_args['sourceVersion'][0],'0'*64),token)[0]in(403,404,409),'WRONG_AVATAR_VERSION_DENIED')
  else:
   require(detail['visibilitySource']=='SELF_PRIVATE'and f['owner_account_id']==owner,'CURRENT_SELF_AUTHORITY_ONLY');self_current+=1
  if note.get('created_at'):
   require(detail['createdAtSource']=='CIVU_ORIGINAL'and date(detail['createdAt'])==date(note['created_at']),'ORIGINAL_TIME_NOT_IMPORT_TIME');dates+=1
  else:require(detail['createdAtSource']=='UNKNOWN','UNKNOWN_ORIGINAL_TIME')
  require(detail['publishedAt']is None,'NO_INVENTED_PUBLICATION_TIME');checked+=1
 require(avatars==6 and foreign>0,'ALL_SIX_AVATARS_READ')
 stage='CURRENT_VIEWPORT_SMOKE';measurements=[]
 for label,w,s,e,n in [('wide',-3,56,-1,58),('phone',-2.15,57.12,-2.06,57.19),('adjacent',-2.16,57.12,-2.07,57.19)]:
  map_path='/v1/me/map-content?'+urllib.parse.urlencode({'cityId':'aberdeen-gb','west':w,'south':s,'east':e,'north':n,'zoom':12});view,measurement=measured_get(map_path,token);require(len(view['pins'])==7 and all(p['kind']!='CLUSTER'for p in view['pins']),'REAL_SEPARATE_CONTENT_POINTS');measurements.append({'scenario':label,'runs':[measurement]})
 last_paths.extend([path,map_path]);require(all(call('GET',p)[0]in(401,403)for p in last_paths),'ANONYMOUS_ATTRIBUTION_DENIED')
 require((hashlib.sha256(SOURCE.read_bytes()).hexdigest(),hashlib.sha256(MANIFEST.read_bytes()).hexdigest())==hashes,'ORIGINAL_SOURCE_UNCHANGED')
 result.update(status='PASS',originalAuthorsChecked=checked,originalPublicAuthors=foreign,currentSelfAuthors=self_current,originalTimesChecked=dates,originalAvatarImagesChecked=avatars,avatarBytesVerified=totalbytes,originalNameMatchesBackup=True,originalTimeMatchesBackup=True,unboundAvatarNotInvented=True,wrongAvatarSourceRejected=True,anonymousDenied=True,measurements=measurements,sourceInputsUnchanged=True)
except Exception as e:result.update(status='FAIL',stage=stage,errorType=type(e).__name__,errorCode=str(e)if isinstance(e,RuntimeError)else'CONTROLLED_AUTHOR_ACCEPTANCE_FAILED')
finally:
 closed=True
 for current_token in tokens:
  ok=False
  for attempt in range(3):
   try:ok=call('POST','/v1/session/logout',current_token)[0]in(200,204)
   except Exception:ok=False
   if ok:break
   time.sleep(1)
  closed=closed and ok
 denied=False
 if closed and tokens and last_paths:
  try:denied=all(call('GET',p,tokens[-1])[0]in(401,403)for p in last_paths)
  except Exception:denied=False
 result.update(allCreatedSessionsLoggedOut=closed,loggedOutReadsDenied=denied)
 if not closed or not denied:result['status']='FAIL'
print(json.dumps(result));raise SystemExit(0 if result['status']=='PASS'else 1)
'''
remote=prefix+'\n'+helpers+'\n'+body;ast.parse(remote)
if '--execute'not in sys.argv:print(json.dumps({'status':'PLAN','apiBuild':28,'schema':172,'writes':'LOGIN_LOGOUT_ONLY','readOnlyAuthorChecks':9,'avatarReads':6}));raise SystemExit(0)
out=r/'docs/testing/evidence/content-map-refinement-20261016/backend-build28/live01';out.mkdir(exist_ok=False)
v=subprocess.run(ssh,input=remote.encode(),capture_output=True,timeout=300,creationflags=subprocess.CREATE_NO_WINDOW);receipt={'exitCode':v.returncode,'stderrPresent':bool(v.stderr),'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'status':'FAIL'}
try:
 value=json.loads(v.stdout);assert m.aggregate_safe(value);receipt.update(result=value,status='PASS'if v.returncode==0 and value['status']=='PASS'else'FAIL')
except Exception as e:receipt.update(errorType=type(e).__name__)
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');(out/'actual-runner.py').write_text(Path(__file__).read_text());(out/'remote.py').write_text(remote);print(json.dumps(receipt));raise SystemExit(0 if receipt['status']=='PASS'else 1)
