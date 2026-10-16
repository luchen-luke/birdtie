from pathlib import Path
import importlib.util,json,hashlib,subprocess
w=Path(__file__).parent;r=Path(r'D:\Project\birdtie')
spec=importlib.util.spec_from_file_location('v',w/'verify_reference167.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
transport,_=m.build_parts()
expected=json.loads((r/'docs/testing/evidence/social-remote-migration-20261015/build26/receipt.json').read_text())['manifest']['bin/birdtie-api']['sha256']
remote=r'''
import pathlib,json,subprocess,hashlib,urllib.request
settings=json.loads(pathlib.Path('/etc/birdtie/deployment.json').read_text())
assert settings['release']=='/opt/birdtie/releases/social171-20261015-build26' and settings['oid']==54087 and settings['database']=='birdtie_native_hz_20261015' and settings['owner']=='birdtie_native_hz'
def sql(q):
 p=subprocess.run(['sudo','-n','-u','postgres','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-d',settings['database']],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 assert p.returncode==0
 return json.loads(p.stdout)
identity=sql("SELECT jsonb_build_object('oid',(SELECT oid FROM pg_database WHERE datname=current_database()),'owner',(SELECT pg_get_userbyid(datdba)FROM pg_database WHERE datname=current_database()),'schema',(SELECT max(version)FROM birdtie_schema_migrations),'migrations',(SELECT count(*)FROM birdtie_schema_migrations),'contract',birdtie_content_map_contract(),'markerMatches',(SELECT shobj_description(oid,'pg_database')FROM pg_database WHERE datname=current_database())="+"'"+settings['marker']+"')")
assert identity=={'oid':54087,'owner':'birdtie_native_hz','schema':171,'migrations':171,'contract':'map-content-v2','markerMatches':True}
api=pathlib.Path(settings['release'])/'bin/birdtie-api';digest=hashlib.sha256(api.read_bytes()).hexdigest()
assert digest==EXPECTED
service=subprocess.check_output(['systemctl','is-active','birdtie-api.service']).decode().strip();assert service=='active'
with urllib.request.urlopen('http://127.0.0.1:18114/readyz',timeout=5)as response: assert response.status==200 and json.load(response)['status']=='ready'
print(json.dumps({'status':'PASS','readOnly':True,'release':settings['release'],'apiSHA256':digest,'databaseOID':54087,'schema':171,'migrations':171,'serviceActive':True,'ready':True}))
'''
remote='EXPECTED='+repr(expected)+'\n'+remote
out=r/'docs/testing/evidence/content-map-refinement-20261016/backend-build27';out.mkdir(parents=True,exist_ok=True)
p=subprocess.run(transport,input=remote.encode(),capture_output=True,timeout=75,creationflags=subprocess.CREATE_NO_WINDOW)
receipt={'exitCode':p.returncode,'stderrPresent':bool(p.stderr),'status':'FAIL'}
if p.returncode==0:receipt.update(result=json.loads(p.stdout),status='PASS')
(out/'preflight.stderr.log').write_bytes(p.stderr)
if p.stderr: print(p.stderr.decode(errors='replace')[-1200:])
(out/'preflight.json').write_text(json.dumps(receipt,indent=2)+'\n');(out/'preflight-runner.py').write_text(Path(__file__).read_text())
print(json.dumps(receipt));raise SystemExit(p.returncode)
