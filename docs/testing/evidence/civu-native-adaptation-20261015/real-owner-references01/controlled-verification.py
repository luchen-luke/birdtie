import ast,hashlib,json,subprocess
from pathlib import Path
R=Path(r'D:\Project\birdtie');tree=ast.parse((R/'docs/testing/evidence/github-remote-completion-20261015/remote-legacy-login01/verify.py').read_text())
v={n.targets[0].id:ast.literal_eval(n.value)for n in tree.body if isinstance(n,ast.Assign)and isinstance(n.targets[0],ast.Name)and n.targets[0].id in('REMOTE','SSH')}
remote=v['REMOTE'].split('owner=None;before=None;source_sha=None;manifest_sha=None')[0].replace("BASE='http://127.0.0.1:18114'","BASE='https://47.110.154.133'").replace("cursor=page.get('nextCursor','')","cursor=page.get('nextCursor') or ''")
remote+=r'''
try:
 source_raw=SOURCE.read_bytes();manifest_raw=MANIFEST.read_bytes()
 before=(hashlib.sha256(source_raw).hexdigest(),hashlib.sha256(manifest_raw).hexdigest())
 bindings=query("SELECT jsonb_agg(jsonb_build_object('owner',owner_account_id,'source',source_user_id)) FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL")
 require(len(bindings)==1,'EXISTING_OWNER_REQUIRED')
 binding=bindings[0];owner=binding['owner'];source=json.loads(source_raw)
 require(re.fullmatch(r'[0-9a-f-]{36}',owner) is not None,'OWNER_INVALID')
 identities=[i for i in source['tables']['auth_identities']if i['provider']=='phone'and i['user_id']==binding['source']]
 require(len(identities)==1,'SOURCE_IDENTITY_UNRESOLVED');token=login(normalize(identities[0]['subject']))
 require(data('GET','/v1/me',token)['id']==owner,'OWNER_CHANGED')
 expected=query("SELECT coalesce(jsonb_agg(jsonb_build_object('id',s.id,'place',p.id,'name',p.name,'summary',p.summary,'city',p.city_id)),'[]'::jsonb) FROM saved_items s JOIN legacy_native_adaptation_entities x ON x.kind='saved_place' AND x.target_id=s.id::text AND x.owner_account_id=s.owner_account_id JOIN places p ON p.id=s.place_id WHERE s.owner_account_id='%s'"%owner)
 require(len(expected)>0,'OWNER_NATIVE_SAVED_MISSING')
 stage='saved_page'
 items=[];cursor='';pages=0
 while True:
  page=data('GET','/v1/me/saved/page'+('?cursor='+urllib.parse.quote(cursor,safe='')if cursor else ''),token)
  items+=page['items'];pages+=1;cursor=page.get('nextCursor')or''
  require(pages<=30,'PAGES_UNBOUNDED')
  if not cursor:break
 actual={i['id']:i for i in items}
 for row in expected:
  item=actual.get(row['id']);require(item is not None,'NATIVE_SAVED_ROW_MISSING')
  require(item['title']==row['name'] and item['summary']==row['summary']and item['cityId']==row['city']and item['available']is False,'PRIVATE_SAVED_LABEL_MISMATCH')
  ref=item.get('legacyReference');require(ref and ref['kind']=='saved_place'and ref['recordId']==row['id']and ref['placeId']==row['place'],'SAVED_REFERENCE_BINDING_MISMATCH')
  require(not any(item.get(k)is not None for k in ['latitude','longitude','sourceExpiresAt','placePhoto','cover']),'PRIVATE_REFERENCE_BECAME_PUBLIC_MAP')
 selected=expected[0];stage='private_place_detail'
 detailpath='/v1/me/place-references/'+selected['place']+'?kind=saved_place&recordId='+selected['id']
 detail=data('GET',detailpath,token)
 require(detail['name']==selected['name']and detail['reference']['recordId']==selected['id'],'PRIVATE_DETAIL_MISMATCH')
 require(call('GET',detailpath)[0]==401 and call('GET','/v1/places/'+selected['place'])[0]==404,'PRIVATE_PLACE_PUBLICLY_READABLE')
 searched=data('GET','/v1/me/saved/page?q='+urllib.parse.quote(selected['name'][:30],safe=''),token)
 require(any(x['id']==selected['id']for x in searched['items']),'PRIVATE_SAVED_SEARCH_OMITTED')
 stage='theme_maps'
 themes,_=page_all('/v1/me/personal-collections?kind=theme_map',token)
 native_themes=query("SELECT coalesce(jsonb_agg(jsonb_build_object('id',m.id,'title',m.title,'summary',m.summary)),'[]'::jsonb) FROM theme_maps m JOIN legacy_native_adaptation_entities x ON x.kind='theme_map' AND x.target_id=m.id::text AND x.owner_account_id=m.owner_account_id WHERE m.owner_account_id='%s'"%owner)
 byid={i['id']:i for i in themes};entries=0
 for row in native_themes:
  actual=byid[row['id']];require(actual['title']==row['title']and actual['summary']==row['summary'],'THEME_OWNER_CONTENT_CHANGED')
  for item in actual['entries']:
   ref=item.get('legacyReference');require(not item['available']and ref and ref['recordId']==row['id']and ref['placeId']==item['placeId']and item['placeName'],'THEME_PRIVATE_REFERENCE_MISSING');entries+=1
   require(not any(item.get(k)is not None for k in ['latitude','longitude','sourceExpiresAt']),'THEME_PRIVATE_MAP_AUTHORITY_INVENTED')
  if actual['entries']:
   ref=actual['entries'][0]['legacyReference'];detail=data('GET','/v1/me/place-references/'+ref['placeId']+'?kind=theme_map&recordId='+row['id'],token)
   require(detail['reference']['recordId']==row['id'],'THEME_DETAIL_REFERENCE_CHANGED')
 require(before==(hashlib.sha256(SOURCE.read_bytes()).hexdigest(),hashlib.sha256(MANIFEST.read_bytes()).hexdigest()),'PRIVATE_INPUT_CHANGED')
 result.update(status='PASS',selectedOwnerSaved=len(expected),savedPages=pages,selectedOwnerThemeMaps=len(native_themes),themePrivateReferences=entries,privateLabelsAndDetailsReadable=True,privateSavedSearchMatched=True,publicPlaceReadRejected=True,anonymousReadRejected=True,newOwnerEnrolled=False,originalInputUnchanged=True,defaultTLSValidated=True)
except Exception as e:
 result.update(failureStage=stage,failureType=type(e).__name__)
 if isinstance(e,RuntimeError)and re.fullmatch('[A-Z_]+',str(e)):result['failureCode']=str(e)
finally:
 checks=[]
 for token in tokens:
  try:checks.append(call('POST','/v1/session/logout',token)[0]==204 and call('GET','/v1/me',token)[0]==401)
  except Exception:checks.append(False)
 result['allCreatedSessionsLoggedOut']=bool(tokens)and all(checks)
 if result['status']=='PASS'and not result['allCreatedSessionsLoggedOut']:result['status']='FAIL'
 print(json.dumps(result));raise SystemExit(0 if result['status']=='PASS'else 1)
'''
ast.parse(remote);out=R/'docs/testing/evidence/civu-native-adaptation-20261015/real-owner-references01';out.mkdir(exist_ok=False)
(out/'controlled-verification.py').write_text(Path(__file__).read_text())
r=subprocess.run(v['SSH'],input=remote.encode(),capture_output=True,creationflags=subprocess.CREATE_NO_WINDOW)
(out/'stdout.json').write_bytes(r.stdout);(out/'stderr.log').write_bytes(r.stderr)
receipt={'exitCode':r.returncode,'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'result':json.loads(r.stdout)if r.stdout else None}
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt));raise SystemExit(r.returncode)
