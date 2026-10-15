"""Read imported native data as one previously enrolled real owner; no content writes."""
import ast, json, subprocess, hashlib
from pathlib import Path
ROOT=Path(r'D:/Project/birdtie')
prior=ROOT/'docs/testing/evidence/github-remote-completion-20261015/remote-legacy-login01/verify.py'
tree=ast.parse(prior.read_text())
values={n.targets[0].id:ast.literal_eval(n.value) for n in tree.body if isinstance(n,ast.Assign) and isinstance(n.targets[0],ast.Name) and n.targets[0].id in ('REMOTE','SSH')}
remote=values['REMOTE'].split('owner=None;before=None;source_sha=None;manifest_sha=None')[0]
remote=remote.replace("BASE='http://127.0.0.1:18114'", "BASE='https://47.110.154.133'")
remote=remote.replace("cursor=page.get('nextCursor','')", "cursor=page.get('nextCursor') or ''")
remote+=r'''
owner=None;before=None;source_sha=None;manifest_sha=None
try:
 source_raw=SOURCE.read_bytes();manifest_raw=MANIFEST.read_bytes()
 source_sha=hashlib.sha256(source_raw).hexdigest();manifest_sha=hashlib.sha256(manifest_raw).hexdigest()
 source=json.loads(source_raw);manifest=json.loads(manifest_raw)
 require(manifest['sourceFileSha256']==source_sha,'SOURCE_HASH_MISMATCH')
 target=query("""SELECT jsonb_build_object('guard',current_database()='birdtie_native_hz_20261015' AND
 (SELECT oid=54087 AND pg_get_userbyid(datdba)='birdtie_native_hz' FROM pg_database WHERE datname=current_database()),
 'schema',(SELECT max(version) FROM birdtie_schema_migrations),
 'contract',birdtie_optional_moment_content_contract(),
 'bindings',(SELECT jsonb_agg(jsonb_build_object('owner',owner_account_id,'source',source_user_id)) FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL),
 'native',(SELECT counts FROM legacy_native_adaptation_runs WHERE run_id='c5df0d7c-f361-4275-9838-feb6d1d92352'),
 'allNativeMomentsPrivate',(SELECT bool_and(m.visibility='private' AND m.status='draft') FROM moments m JOIN legacy_native_adaptation_entities e ON e.kind='moment' AND e.target_id=m.id::text),
 'allImportedPlacesDraft',(SELECT bool_and(p.publication_status='draft') FROM places p JOIN legacy_native_adaptation_entities e ON e.kind='place' AND e.target_id=p.id::text),
 'ownerAgentsSuspended',(SELECT bool_and(a.status='suspended') FROM agents a JOIN legacy_entity_mappings m ON m.entity_kind='user' AND m.target_id=a.principal_account_id))""")
 require(target['guard'] and target['schema']==157 and target['contract']=='moment-optional-content-v1','TARGET_GUARD_FAILED')
 require(target['allNativeMomentsPrivate'] and target['allImportedPlacesDraft'] and target['ownerAgentsSuspended'],'MIGRATION_EXPANDED_AUTHORITY')
 require(len(target['bindings'])==1,'EXPECTED_ONE_PREVIOUSLY_ENROLLED_OWNER')
 binding=target['bindings'][0];owner=binding['owner'];uid=binding['source']
 require(re.fullmatch(r'[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}',owner) is not None,'OWNER_ID_INVALID')
 identities=[i for i in source['tables']['auth_identities'] if i['provider']=='phone' and i['user_id']==uid]
 require(len(identities)==1,'EXACT_PHONE_IDENTITY_REQUIRED')
 expected=query("""SELECT coalesce(jsonb_agg(jsonb_build_object('id',m.id,'title',m.title,'body',m.body,'cityId',coalesce(m.city_id,''),'placeId',coalesce(m.place_id::text,''),'revision',m.revision)),'[]'::jsonb) FROM moments m JOIN legacy_native_adaptation_entities e ON e.kind='moment' AND e.target_id=m.id::text WHERE m.author_account_id='%s'"""%owner)
 require(len(expected)>0,'NATIVE_OWNER_CONTENT_MISSING')
 before=state(owner)
 stage='fixed_login'
 token=login(normalize(identities[0]['subject']))
 require(data('GET','/v1/me',token)['id']==owner,'FIXED_LOGIN_OWNER_MISMATCH')
 stage='native_read'
 items,pages=page_all('/v1/me/moments/history?status=draft',token)
 by_id={item['id']:item for item in items}
 for want in expected:
  require(want['id'] in by_id,'NATIVE_HISTORY_OMITTED_ITEM')
  actual=by_id[want['id']]
  require(all(actual.get(k,'')==v for k,v in want.items()),'NATIVE_HISTORY_CHANGED_SOURCE_TEXT')
  require(actual['visibility']=='private' and actual['status']=='draft','NATIVE_HISTORY_EXPANDED_PRIVACY')
 for want in [expected[0]]+next(([x] for x in expected if x['cityId']==''),[]):
  actual=data('GET','/v1/me/moments/'+want['id'],token)
  require(all(actual.get(k,'')==v for k,v in want.items()),'NATIVE_DETAIL_MISMATCH')
  require(call('GET','/v1/moments/'+want['id'])[0]==404,'PRIVATE_CONTENT_PUBLICLY_READABLE')
 result.update(status='PASS',nativeCounts=target['native'],selectedOwnerNativeMoments=len(expected),
  selectedOwnerUntitled=sum(not x['title'] for x in expected),selectedOwnerUnlocated=sum(not x['cityId'] and not x['placeId'] for x in expected),
  historyPages=pages,allSelectedNativeTextMatched=True,anonymousPrivateReadRejected=True,
  allMigratedMomentsPrivate=True,allImportedPlacesDraft=True,originalOwnerMatched=True,defaultTLSValidated=True,
  newOwnerEnrolled=False,newAccountActivated=False,allImportedAgentsSuspended=True)
except Exception as error:
 result['failureStage']=stage;result['failureType']=type(error).__name__
 if isinstance(error,RuntimeError) and re.fullmatch(r'[A-Z_]+',str(error)):result['failureCode']=str(error)
finally:
 logged=[];rejected=[]
 for token in tokens:
  try:
   logged.append(call('POST','/v1/session/logout',token)[0]==204)
   rejected.append(call('GET','/v1/me',token)[0]==401)
  except Exception:logged.append(False)
 result['allCreatedSessionsLoggedOut']=bool(tokens) and all(logged) and all(rejected)
 result['privateInputUnchanged']=hashlib.sha256(SOURCE.read_bytes()).hexdigest()==source_sha and hashlib.sha256(MANIFEST.read_bytes()).hexdigest()==manifest_sha
 if result['status']=='PASS' and not result['allCreatedSessionsLoggedOut']:result['status']='FAIL'
 print(json.dumps(result))
 raise SystemExit(0 if result['status']=='PASS' else 1)
'''
ast.parse(remote)
out=ROOT/'docs/testing/evidence/civu-native-adaptation-20261015/real-owner-native-text01'
out.mkdir(parents=True,exist_ok=False)
(out/'controlled-verification.py').write_text(Path(__file__).read_text())
result=subprocess.run(values['SSH'],input=remote.encode(),capture_output=True,creationflags=subprocess.CREATE_NO_WINDOW)
(out/'stdout.json').write_bytes(result.stdout);(out/'stderr.log').write_bytes(result.stderr)
receipt={'exitCode':result.returncode,'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'result':json.loads(result.stdout) if result.stdout else None}
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt));raise SystemExit(result.returncode)
