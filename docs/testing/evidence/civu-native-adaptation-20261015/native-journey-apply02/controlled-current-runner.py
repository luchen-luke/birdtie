import base64,hashlib,importlib.util,json,re,shlex,subprocess,sys
from pathlib import Path
R=Path(r'D:\Project\birdtie')
spec=importlib.util.spec_from_file_location('deploy',R/'scripts/deploy_birdtie_hangzhou.py');d=importlib.util.module_from_spec(spec);spec.loader.exec_module(d)
mode,name=sys.argv[1:];assert mode in ('apply','inspect') and re.fullmatch('[a-z0-9-]+',name)
out=R/'docs/testing/evidence/civu-native-adaptation-20261015'/name;out.mkdir(exist_ok=False)
remote=r'''
import hashlib,json,os,re,subprocess,sys
from pathlib import Path
p=json.loads(sys.stdin.buffer.readline());stage='preflight'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def run(args,env):
 r=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env)
 if r.returncode:
  msg=r.stderr.decode(errors='replace').strip()
  raise RuntimeError(msg if re.fullmatch('legacy_[a-z_]+',msg) else 'CONTROLLED_COMMAND_FAILED')
 return json.loads(r.stdout)
try:
 settings=json.loads(Path('/etc/birdtie/deployment.json').read_bytes())
 assert settings['database']=='birdtie_native_hz_20261015' and settings['oid']==54087 and settings['release']=='/opt/birdtie/releases/social159-20261015-build09'
 source=Path('/srv/birdtie-data/migration/source.json');manifest=Path('/srv/birdtie-data/migration/manifest.json')
 before=(sha(source),sha(manifest));env={k:v for k,v in os.environ.items()if not k.startswith(('PG','BIRDTIE_'))}
 env['BIRDTIE_DATABASE_URL']='postgresql://birdtie_native_hz:'+settings['password']+'@127.0.0.1:5432/'+settings['database']+'?sslmode=disable'
 env['BIRDTIE_REPOSITORY_ROOT']='/opt/birdtie'
 binary=settings['release']+'/bin/birdtie-legacy-native-import'
 target=[binary,'--journey-details','--database',settings['database'],'--database-oid',str(settings['oid']),'--database-owner','birdtie_native_hz','--database-marker',settings['marker'],'--source-database-oid',str(settings['sourceOID'])]
 runid='c5df0d7c-f361-4275-9838-feb6d1d92352';selected=['--run-id',runid,'--source',str(source),'--manifest',str(manifest)]
 if p['mode']=='inspect':
  stage='inspect';original=run(target+['--inspect-run',runid],env)
 else:
  stage='validate';prepared=run([binary,'--journey-details']+selected+['--validate'],env)
  assert prepared['counts']=={'journeys':22,'entries':110,'stops':24,'arrangements':85,'notes':1,'tombstones':12,'unmappedPlaces':0,'unmappedCities':0}
  stage='apply';result=run(target+selected+['--apply'],env)
  stage='inspect';original=run(target+['--inspect-run',runid],env)
  assert result==original and original['status']=='COMMITTED' and original['counts']==prepared['counts']
  stage='repeat';assert run(target+selected+['--apply'],env)==original
 assert before==(sha(source),sha(manifest))
 print(json.dumps({'status':'NATIVE_JOURNEYS_COMMITTED','result':original,'sourceWrites':0,'originalInputUnchanged':True,'originalFactInspected':True,'repeatChecked':p['mode']=='apply'}))
except Exception as e:
 print(json.dumps({'status':'UNKNOWN_REQUIRES_INSPECTION'if stage=='apply'else'FAILED','stage':stage,'errorType':type(e).__name__,'controlledCode':str(e)if isinstance(e,RuntimeError)and re.fullmatch('legacy_[a-z_]+|CONTROLLED_COMMAND_FAILED',str(e))else None,'privateValuesOmitted':True}));sys.exit(1)
'''
bootstrap="import base64;exec(base64.b64decode('"+base64.b64encode(remote.encode()).decode()+"'))"
r=subprocess.run(d.SSH+['sudo -n python3 -c '+shlex.quote(bootstrap)],input=json.dumps({'mode':mode}).encode()+b'\n',capture_output=True,creationflags=subprocess.CREATE_NO_WINDOW)
(out/'stdout.json').write_bytes(r.stdout);(out/'stderr.log').write_bytes(r.stderr)
receipt={'exitCode':r.returncode,'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'result':json.loads(r.stdout)}
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt));sys.exit(r.returncode)
