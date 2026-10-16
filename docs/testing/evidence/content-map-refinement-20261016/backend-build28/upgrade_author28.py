from pathlib import Path
import importlib.util,json,ast,sys
w=Path(__file__).parent;r=Path(r'D:\Project\birdtie')
spec=importlib.util.spec_from_file_location('prior',w/'upgrade_native171_24.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
prior,s=m.program()
s=s.replace('prior_count == 170 and target == 171','prior_count == 171 and target == 172')
s=s.replace("'/opt/birdtie/releases/social170-20261015-build23'", "'/opt/birdtie/releases/social171-20261015-build27'")
s=s.replace("out=ROOT/'docs/testing/evidence/social-remote-migration-20261015'/a.evidence_name", "out=ROOT/'docs/testing/evidence/content-map-refinement-20261016/backend-build28'/a.evidence_name")
s=s.replace("(ROOT/'docs/testing/evidence/social-remote-migration-20261015'/build/'receipt.json')", "(ROOT/'docs/testing/evidence/content-map-refinement-20261016/backend-build28/build/receipt.json')")
s=s.replace("payload={'mode':a.mode,'reuseStaged':a.reuse_staged}", "payload={'mode':a.mode,'reuseStaged':a.reuse_staged,'expectedPriorAPI':'c95adfbe22b10df95f4e934d042c438f3977e710afd6260b30b5f0c0c37d8ed5'}")
s=s.replace("target=payload['targetMigrations'];prior_count=payload['expectedMigrations']", "assert digest(Path(settings['release'])/'bin/birdtie-api')==payload['expectedPriorAPI']\n target=payload['targetMigrations'];prior_count=payload['expectedMigrations']")
s=s.replace('assert not existing or existing==[desired]', 'assert existing==[desired]')
s=s.replace('target+(11 if target>=171','target+(12 if target>=172 else 11 if target>=171')
s=s.replace('legacy-map-anchor-restore)', 'legacy-map-anchor-restore|legacy-moment-author-restore)')
s=s.replace(" migration_names[171]='migrations/171_content_map_anchors.sql'", " migration_names[171]='migrations/171_content_map_anchors.sql'\n migration_names[172]='migrations/172_legacy_moment_authors.sql'")
s=s.replace('range(149,172)','range(149,173)').replace('range(148,172)','range(148,173)')
s=s.replace(" assert sql(DB,\"SELECT birdtie_content_map_contract();\").decode().strip()=='map-content-v2'", " assert sql(DB,\"SELECT birdtie_content_map_contract();\").decode().strip()=='map-content-v2'\n assert sql(DB,\"SELECT birdtie_legacy_moment_author_contract();\").decode().strip()=='legacy-moment-author-v1'")
a=s.index(" previous=settings['release'];settings['release']=str(release)")
b=s.index("elif mode=='personal-apply':",a)
s=s[:a]+''' previous=settings['release'];state_before=STATE.read_bytes();unit_before=unit.read_bytes()
 def atomic_bytes(path,value,mode):
  temporary=path.with_name(path.name+'.build28.tmp')
  with temporary.open('xb')as f:f.write(value);f.flush();os.fsync(f.fileno())
  os.chmod(temporary,mode);os.replace(temporary,path)
 def ready_current():
  for _ in range(30):
   try:
    with urllib.request.urlopen('http://127.0.0.1:18114/readyz',timeout=2)as response:
     if response.status==200 and json.load(response)['status']=='ready':return True
   except Exception:pass
   time.sleep(.5)
  return False
 settings['release']=str(release)
 try:
  atomic_bytes(STATE,(json.dumps(settings,indent=2)+'\\n').encode(),0o600)
  replacement=text.replace('ExecStart='+old,'ExecStart='+str(release)+'/bin/birdtie-api').replace('WorkingDirectory='+previous,'WorkingDirectory='+str(release))
  atomic_bytes(unit,replacement.encode(),0o644)
  run(['systemctl','daemon-reload']);run(['systemctl','restart','birdtie-api.service'])
  assert ready_current(),'NEW_RELEASE_NOT_READY'
 except Exception:
  atomic_bytes(STATE,state_before,0o600);atomic_bytes(unit,unit_before,0o644)
  run(['systemctl','daemon-reload']);run(['systemctl','restart','birdtie-api.service'])
  assert ready_current(),'ROLLBACK_NOT_READY'
  raise RuntimeError('BUILD28_FAILED_PREVIOUS_RELEASE_RESTORED')
 print(json.dumps({'stage':'UPGRADED_READY','migrations':target,'appliedMigrations':target-prior_count,'originalMigrationsUnchanged':prior_count,'release':str(release),'previousRelease':previous,'priorAPISHA256':payload['expectedPriorAPI'],'apiSHA256':digest(release/'bin/birdtie-api'),'publicRoutingChanged':False,'oldAssetsDeleted':False,'atomicFileReplacement':True,'rollbackOnReadinessFailure':True}))
''' + s[b:]
ast.parse(s)
for node in ast.parse(s).body:
 if isinstance(node,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='REMOTE' for t in node.targets):ast.parse(ast.literal_eval(node.value))
if '--execute' not in sys.argv:print(json.dumps({'status':'PLAN','expectedAPI':27,'targetAPI':28,'schema':172,'newMigrations':1,'atomicReplace':True,'rollbackOnReadinessFailure':True}));raise SystemExit(0)
ns={'__name__':'refinement_deploy27','__file__':str(prior)};exec(compile(s,str(prior),'exec'),ns);ns['SSH'].insert(1,'-C')
sys.argv=[str(prior),'upgrade','--evidence-name','deploy01','--build-name','build28','--expected-migrations','171','--target-migrations','172']
raise SystemExit(ns['main']())
