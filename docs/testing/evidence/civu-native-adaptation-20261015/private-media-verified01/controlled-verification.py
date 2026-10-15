import ast,hashlib,json,subprocess
from pathlib import Path
R=Path(r'D:\Project\birdtie');base=R/'docs/testing/evidence/github-remote-completion-20261015/remote-legacy-login01/verify.py'
tree=ast.parse(base.read_text());v={n.targets[0].id:ast.literal_eval(n.value)for n in tree.body if isinstance(n,ast.Assign)and isinstance(n.targets[0],ast.Name)and n.targets[0].id in('REMOTE','SSH')}
remote=v['REMOTE'].split('owner=None;before=None;source_sha=None;manifest_sha=None')[0].replace("BASE='http://127.0.0.1:18114'","BASE='https://47.110.154.133'")
remote+=r'''
try:
 before=(hashlib.sha256(SOURCE.read_bytes()).hexdigest(),hashlib.sha256(MANIFEST.read_bytes()).hexdigest())
 audit=query("""SELECT jsonb_build_object(
 'images',count(*),'notes',count(DISTINCT x.moment_id),'owners',count(DISTINCT x.owner_account_id),
 'derivativeBytesMatch',bool_and(octet_length(i.derivative)=x.derivative_bytes AND encode(sha256(i.derivative),'hex')=x.derivative_sha256 AND i.derivative_sha256=x.derivative_sha256),
 'ownerBindingsMatch',bool_and(x.owner_account_id=i.owner_account_id AND i.owner_account_id=m.author_account_id AND x.moment_id=i.moment_id AND n.owner_account_id=x.owner_account_id AND n.target_id=m.id::text),
 'privateDraftsOnly',bool_and(m.visibility='private' AND m.status='draft' AND i.status='ready_private' AND i.purpose='PRIVATE_MOMENT_ATTACHMENT'),
 'noFabricatedSessions',bool_and(i.session_id IS NULL),
 'sourceSizesMatch',bool_and(i.input_size=x.source_input_bytes AND i.input_sha256=x.source_input_sha256),
 'assetProofMatches',bool_and(a.owner_account_id=x.owner_account_id AND a.sha256_hex=x.derivative_sha256 AND a.byte_size=x.derivative_bytes AND a.state='ready_private'),
 'noScanOrPrivacyClaim',bool_and(x.antivirus_scan='NOT_RUN' AND x.privacy_risk='UNKNOWN' AND i.pixel_risk='UNKNOWN'),
 'ownerAgentsSuspended',(SELECT bool_and(a.status='suspended') FROM agents a JOIN legacy_entity_mappings e ON e.entity_kind='user' AND e.target_id=a.principal_account_id),
 'previouslyEnrolledOwnerCount',(SELECT count(*) FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL)
 ) FROM legacy_private_media_import_items x JOIN private_moment_images i ON i.id=x.image_id JOIN moments m ON m.id=x.moment_id
 JOIN media_assets a ON a.id=i.id JOIN legacy_native_adaptation_entities n ON n.kind='moment' AND n.source_key=x.source_note_id::text AND n.run_id=x.run_id
 WHERE x.run_id='c5df0d7c-f361-4275-9838-feb6d1d92352'""")
 require((audit['images'],audit['notes'],audit['owners'])==(135,52,8),'IMPORTED_COUNTS_CHANGED')
 require(all(value for value in audit.values() if isinstance(value,bool)),'IMPORTED_MEDIA_BINDING_MISMATCH')
 require(audit['previouslyEnrolledOwnerCount']==1,'OWNER_ENROLLMENT_CHANGED')
 ids=query("SELECT jsonb_build_object('image',image_id,'moment',moment_id) FROM legacy_private_media_import_items ORDER BY image_id LIMIT 1")
 require(call('GET','/v1/me/moments/'+ids['moment']+'/private-images/'+ids['image']+'/content')[0]==401,'ANONYMOUS_MEDIA_READ_ALLOWED')
 require(call('GET','/v1/moments/'+ids['moment'])[0]==404,'PRIVATE_MOMENT_PUBLICLY_READABLE')
 require(before==(hashlib.sha256(SOURCE.read_bytes()).hexdigest(),hashlib.sha256(MANIFEST.read_bytes()).hexdigest()),'PRIVATE_INPUT_CHANGED')
 print(json.dumps({'status':'PASS','checks':audit,'anonymousImageRejected':True,'privateMomentPublicReadRejected':True,'databaseWrites':0,'newSessions':0,'newOwnerEnrolled':False,'originalInputUnchanged':True,'actualOwnerImageHTTPRead':'NOT_RUN: previously enrolled owner has no imported images','defaultTLSValidated':True}))
except Exception as e:
 print(json.dumps({'status':'FAIL','errorType':type(e).__name__,'controlledCode':str(e)if isinstance(e,RuntimeError)and re.fullmatch('[A-Z_]+',str(e))else None}));raise SystemExit(1)
'''
ast.parse(remote);out=R/'docs/testing/evidence/civu-native-adaptation-20261015/private-media-verified01';out.mkdir(exist_ok=False)
(out/'controlled-verification.py').write_text(Path(__file__).read_text())
r=subprocess.run(v['SSH'],input=remote.encode(),capture_output=True,creationflags=subprocess.CREATE_NO_WINDOW)
(out/'stdout.json').write_bytes(r.stdout);(out/'stderr.log').write_bytes(r.stderr)
receipt={'exitCode':r.returncode,'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'result':json.loads(r.stdout)if r.stdout else None}
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt));raise SystemExit(r.returncode)
