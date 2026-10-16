from pathlib import Path
import importlib.util,json,ast,sys
w=Path(__file__).parent;r=Path(r'D:\Project\birdtie')
spec=importlib.util.spec_from_file_location('prior',w/'upgrade_native171_24.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
prior,s=m.program()
s=s.replace('prior_count == 170 and target == 171','prior_count == 171 and target == 171')
s=s.replace("'/opt/birdtie/releases/social170-20261015-build23'", "'/opt/birdtie/releases/social171-20261015-build26'")
s=s.replace("out=ROOT/'docs/testing/evidence/social-remote-migration-20261015'/a.evidence_name", "out=ROOT/'docs/testing/evidence/content-map-refinement-20261016/backend-build27'/a.evidence_name")
s=s.replace("(ROOT/'docs/testing/evidence/social-remote-migration-20261015'/build/'receipt.json')", "(ROOT/'docs/testing/evidence/content-map-refinement-20261016/backend-build27/build/receipt.json')")
s=s.replace("payload={'mode':a.mode,'reuseStaged':a.reuse_staged}", "payload={'mode':a.mode,'reuseStaged':a.reuse_staged,'expectedPriorAPI':'d77531962e0a89fa4f136849d5e883342d8d8f66caf37df5079871b722e42afe'}")
s=s.replace("target=payload['targetMigrations'];prior_count=payload['expectedMigrations']", "assert digest(Path(settings['release'])/'bin/birdtie-api')==payload['expectedPriorAPI']\n target=payload['targetMigrations'];prior_count=payload['expectedMigrations']")
s=s.replace('assert not existing or existing==[desired]', 'assert existing==[desired]')
a=s.index(" previous=settings['release'];settings['release']=str(release)")
b=s.index("elif mode=='personal-apply':",a)
s=s[:a]+''' previous=settings['release'];state_before=STATE.read_bytes();unit_before=unit.read_bytes()
 def atomic_bytes(path,value,mode):
  temporary=path.with_name(path.name+'.build27.tmp')
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
  raise RuntimeError('BUILD27_FAILED_PREVIOUS_RELEASE_RESTORED')
 print(json.dumps({'stage':'REFRESHED_READY','migrations':target,'appliedMigrations':0,'originalMigrationsUnchanged':prior_count,'release':str(release),'previousRelease':previous,'priorAPISHA256':payload['expectedPriorAPI'],'apiSHA256':digest(release/'bin/birdtie-api'),'publicRoutingChanged':False,'oldAssetsDeleted':False,'atomicFileReplacement':True,'rollbackOnReadinessFailure':True}))
''' + s[b:]
ast.parse(s)
for node in ast.parse(s).body:
 if isinstance(node,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='REMOTE' for t in node.targets):ast.parse(ast.literal_eval(node.value))
if '--execute' not in sys.argv:print(json.dumps({'status':'PLAN','expectedAPI':26,'targetAPI':27,'schema':171,'newMigrations':0,'atomicReplace':True,'rollbackOnReadinessFailure':True}));raise SystemExit(0)
ns={'__name__':'refinement_deploy27','__file__':str(prior)};exec(compile(s,str(prior),'exec'),ns);ns['SSH'].insert(1,'-C')
sys.argv=[str(prior),'upgrade','--evidence-name','deploy01','--build-name','build27','--expected-migrations','171','--target-migrations','171']
raise SystemExit(ns['main']())
