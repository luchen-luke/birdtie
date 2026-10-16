from pathlib import Path
import ast,importlib.util,subprocess,json,hashlib,datetime
R=Path(r'D:\Project\birdtie');O=Path('work/empty40-readonly.json')
assert not O.exists()
spec=importlib.util.spec_from_file_location('transport',R/'scripts/deploy_birdtie_hangzhou.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
tree=ast.parse(Path('work/dgs_post175_readonly01.py').read_text(encoding='utf-8'))
tail=next(ast.literal_eval(n.value)for n in tree.body if isinstance(n,ast.AugAssign)and isinstance(n.target,ast.Name)and n.target.id=='source'and isinstance(n.value,ast.Constant))
guard=tail.split("backup=Path('/srv/birdtie-data/migration/source.json')")[0]
source="__name__='_audit_scope'\n"+(R/'scripts/audit_birdtie_synthetic_data.py').read_text(encoding='utf-8').replace('from __future__ import annotations','')+guard
source+=r'''
data=query("""WITH claimed AS (SELECT owner_account_id FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL), latest AS (SELECT s.* FROM sessions s JOIN claimed c ON c.owner_account_id=s.account_id ORDER BY s.created_at DESC LIMIT 1) SELECT jsonb_build_object('nowUTC',clock_timestamp(),'claimedOwners',(SELECT count(*)FROM claimed),'latestSession',(SELECT jsonb_build_object('createdAt',created_at,'absoluteExpiry',expires_at,'idleExpiry',idle_expires_at,'revoked',revoked_at IS NOT NULL,'absoluteExpired',expires_at<=clock_timestamp(),'idleExpired',idle_expires_at<=clock_timestamp(),'method',authentication_method,'accountActive',(SELECT status='active'FROM accounts WHERE id=account_id))FROM latest),'moments',(SELECT count(*)FROM moments),'civuMoments',(SELECT count(*)FROM moments m WHERE EXISTS(SELECT 1 FROM legacy_native_adaptation_entities n WHERE n.kind='moment'AND n.target_id=m.id::text)),'readOnly',current_setting('transaction_read_only'))""")
require(data['claimedOwners']==1 and data['readOnly']=='on','EXACT_OWNER_READONLY_GUARD')
print(json.dumps({'status':'READ_ONLY_OBSERVATION','metadata':data,'productionWrites':0,'apiLogins':0,'privateTokensOrBodiesReturned':False}))
'''
ast.parse(source)
p=subprocess.run(m.SSH+['sudo','-n','python3','-'],input=source.encode(),capture_output=True,timeout=50)
assert p.returncode==0,'READONLY_FAILED_NO_RAW_OUTPUT'
d=json.loads(p.stdout);d['probeSHA256']=hashlib.sha256(source.encode()).hexdigest();O.write_text(json.dumps(d,indent=2)+'\n');print(json.dumps(d))
