"""Bounded original-owner session metadata; SELECT-only, no identities or tokens."""
from pathlib import Path
import ast, importlib.util, subprocess, json, hashlib, sys, re
R=Path(r'D:\Project\birdtie')
name=sys.argv[1];assert re.fullmatch('[a-z0-9-]+',name)
O=R/'docs/testing/evidence/session-recovery-20261016/android41-device'/(name+'.json')
assert not O.exists()
spec=importlib.util.spec_from_file_location('transport',R/'scripts/deploy_birdtie_hangzhou.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
tree=ast.parse(Path('work/dgs_post175_readonly01.py').read_text(encoding='utf-8'))
tail=next(ast.literal_eval(n.value)for n in tree.body if isinstance(n,ast.AugAssign)and isinstance(n.target,ast.Name)and n.target.id=='source'and isinstance(n.value,ast.Constant))
guard=tail.split("backup=Path('/srv/birdtie-data/migration/source.json')")[0]
guard=guard.replace('social175-20261016-build31','social176-20261016-build32').replace('3359b4baf56aadad3dff66c25e9d9cceaab4a5ef16fc78273a78881942216b4c','7543929ca5569e2501ef04bb00ffe7fdf44c9e57ce5a94478d612d3370e2d9d7').replace('==175','==176').replace('175_GUARD','176_GUARD')
source="__name__='_audit_scope'\n"+(R/'scripts/audit_birdtie_synthetic_data.py').read_text(encoding='utf-8').replace('from __future__ import annotations','')+guard
source+=r'''
data=query("""WITH claimed AS (SELECT owner_account_id FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL), family AS (SELECT f.* FROM persistent_session_families f JOIN claimed c ON c.owner_account_id=f.account_id ORDER BY f.created_at DESC LIMIT 1), latest AS (SELECT s.* FROM sessions s JOIN claimed c ON c.owner_account_id=s.account_id ORDER BY s.created_at DESC LIMIT 1) SELECT jsonb_build_object(
 'observedAt',clock_timestamp(),'claimedOwners',(SELECT count(*)FROM claimed),
 'latestSession',(SELECT jsonb_build_object('createdAt',created_at,'absoluteExpiry',expires_at,'idleExpiry',idle_expires_at,'revoked',revoked_at IS NOT NULL,'absoluteExpired',expires_at<=clock_timestamp(),'idleExpired',idle_expires_at<=clock_timestamp(),'method',authentication_method,'accountActive',(SELECT status='active'FROM accounts WHERE id=account_id))FROM latest),
 'familyCount',(SELECT count(*)FROM persistent_session_families),
 'latestFamily',(SELECT jsonb_build_object('createdAt',created_at,'expiresAt',expires_at,'revoked',revoked_at IS NOT NULL,'expired',expires_at<=clock_timestamp(),'tokens',(SELECT count(*)FROM persistent_session_tokens t WHERE t.family_id=f.id),'consumedTokens',(SELECT count(*)FROM persistent_session_tokens t WHERE t.family_id=f.id AND t.consumed_at IS NOT NULL),'activeAccess',(SELECT count(*)FROM persistent_session_access a JOIN sessions s ON s.id=a.session_id WHERE a.family_id=f.id AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()AND s.idle_expires_at>clock_timestamp()),'revokedAccess',(SELECT count(*)FROM persistent_session_access a JOIN sessions s ON s.id=a.session_id WHERE a.family_id=f.id AND s.revoked_at IS NOT NULL),'accessSessions',(SELECT count(*)FROM persistent_session_access a WHERE a.family_id=f.id))FROM family f),
 'moments',(SELECT count(*)FROM moments),'civuMoments',(SELECT count(*)FROM moments m WHERE EXISTS(SELECT 1 FROM legacy_native_adaptation_entities n WHERE n.kind='moment'AND n.target_id=m.id::text)),
 'messages',(SELECT count(*)FROM conversation_messages),'readOnly',current_setting('transaction_read_only'))""")
require(data['claimedOwners']==1 and data['readOnly']=='on','EXACT_OWNER_READONLY_GUARD')
print(json.dumps({'status':'READ_ONLY_OBSERVATION','apiRelease':EXPECTED_RELEASE,'apiSHA256':EXPECTED_BINARY,'metadata':data,'productionWrites':0,'apiLogins':0,'privateTokensOrBodiesReturned':False}))
'''
ast.parse(source)
p=subprocess.run(m.SSH+['sudo','-n','python3','-'],input=source.encode(),capture_output=True,timeout=50)
assert p.returncode==0,'READONLY_FAILED_NO_RAW_OUTPUT'
d=json.loads(p.stdout);d['probeSHA256']=hashlib.sha256(source.encode()).hexdigest();O.parent.mkdir(parents=True,exist_ok=True);O.write_text(json.dumps(d,indent=2)+'\n');print(json.dumps(d))
