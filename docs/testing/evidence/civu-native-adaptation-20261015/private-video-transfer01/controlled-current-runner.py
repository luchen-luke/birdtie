import base64,hashlib,importlib.util,json,shlex,subprocess,sys,tarfile
from pathlib import Path
ROOT=Path(r'D:\Project\birdtie')
PRIVATE=Path(r'C:\Users\chens\Documents\Codex\2026-10-14\birdtie-hangzhou')
BLOBS=PRIVATE/'civu-private-videos-20261015-01';MATCH=PRIVATE/'civu-private-media-match-20261015-01'
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
sealSHA='6aa8fdb2326ae66b743355959fef5584b31ce4472371bebb961dbf8abc28f169'
assert sha(MATCH/'authenticated-seal.json')==sealSHA
assert json.loads((BLOBS/'extraction-authenticated.json').read_bytes())['aesGcmAuthenticated']
spec=importlib.util.spec_from_file_location('deploy',ROOT/'scripts/deploy_birdtie_hangzhou.py');d=importlib.util.module_from_spec(spec);spec.loader.exec_module(d)
mode,name=sys.argv[1:];assert mode in ('transfer','apply','inspect')
out=ROOT/'docs/testing/evidence/civu-native-adaptation-20261015'/name;out.mkdir(exist_ok=False)
remote=r'''
import hashlib,json,os,re,subprocess,sys,tarfile
from pathlib import Path
p=json.loads(sys.stdin.buffer.readline());folder=Path('/srv/birdtie-data/migration/private-video-20261015-01');stage='preflight'
def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def run(args,**kwargs):
 r=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,**kwargs)
 if r.returncode:
  diagnostic=r.stderr.decode(errors='replace').strip()
  raise RuntimeError(diagnostic if re.fullmatch(r'legacy_[a-z_]+',diagnostic) else 'CONTROLLED_COMMAND_FAILED')
 return r.stdout
try:
 if p['mode']=='transfer':
  assert not folder.exists();folder.mkdir(mode=0o700);seen=set();total=0
  with tarfile.open(fileobj=sys.stdin.buffer,mode='r|') as archive:
   for m in archive:
    assert m.isfile() and not m.issym() and not m.islnk() and not m.sparse
    assert m.name not in seen and m.name in p['files'] and '/'not in m.name and '\\'not in m.name
    assert re.fullmatch(r'[0-9a-f]{64}\.blob|matched\.private\.json|authenticated-seal\.json',m.name)
    proof=p['files'][m.name];assert 0<m.size==proof['bytes']<=64*1024**2
    raw=archive.extractfile(m).read();assert len(raw)==m.size and hashlib.sha256(raw).hexdigest()==proof['sha256']
    target=folder/m.name
    with target.open('xb')as f:f.write(raw)
    os.chmod(target,0o600);seen.add(m.name);total+=len(raw)
  assert seen==set(p['files']) and len(seen)==7
  (folder/'transfer-complete.json').write_text(json.dumps({'files':p['files'],'sourceAuthenticated':True}));os.chmod(folder/'transfer-complete.json',0o600)
  print(json.dumps({'status':'PRIVATE_VIDEO_TRANSFER_COMPLETE','files':len(seen),'bytes':total,'sourceWrites':0,'databaseWrites':0}))
 else:
  marker=json.loads((folder/'transfer-complete.json').read_bytes());assert marker['sourceAuthenticated']
  assert all(sha(folder/n)==v['sha256'] for n,v in marker['files'].items())
  assert sha(folder/'authenticated-seal.json')==p['sealSHA']
  settings=json.loads(Path('/etc/birdtie/deployment.json').read_bytes())
  assert settings['database']=='birdtie_native_hz_20261015' and settings['oid']==54087 and settings['release']=='/opt/birdtie/releases/social159-20261015-build10'
  env={k:v for k,v in os.environ.items()if not k.startswith(('PG','BIRDTIE_'))}
  env['GOMEMLIMIT']='512MiB';env['BIRDTIE_DATABASE_URL']='postgresql://birdtie_native_hz:'+settings['password']+'@127.0.0.1:5432/'+settings['database']+'?sslmode=disable';env['BIRDTIE_REPOSITORY_ROOT']='/opt/birdtie'
  binary=settings['release']+'/bin/birdtie-legacy-video-import'
  target=[binary,'--database',settings['database'],'--database-oid',str(settings['oid']),'--database-owner','birdtie_native_hz','--database-marker',settings['marker'],'--source-database-oid',str(settings['sourceOID'])]
  runid='c5df0d7c-f361-4275-9838-feb6d1d92352'
  selected=['--run-id',runid,'--source','/srv/birdtie-data/migration/source.json','--manifest','/srv/birdtie-data/migration/manifest.json','--matched',str(folder/'matched.private.json'),'--authenticated-seal',str(folder/'authenticated-seal.json'),'--authenticated-seal-sha256',p['sealSHA']]
  stage='inspect' if p['mode']=='inspect' else 'validate'
  if p['mode']=='inspect':original=json.loads(run(target+['--inspect-run',runid],env=env))
  else:
   prepared=json.loads(run([binary]+selected+['--validate'],env=env));assert prepared['counts']['videos']==5
   stage='verify_remote_codec'
   verification=[json.loads(line) for line in run([binary]+selected+['--blob-directory',str(folder),'--ffmpeg','/usr/bin/ffmpeg','--ffprobe','/usr/bin/ffprobe','--prlimit','/usr/bin/prlimit','--codec-temp-root','/tmp','--verify-blobs'],env=env).splitlines() if line.strip()]
   verified=verification[-1];assert verified['status']=='VERIFIED_NOT_IMPORTED' and verified['total']==verified['passed']==5 and verified['failed']==0 and verified['databaseWrites']==0
   stage='apply'
   result=json.loads(run(target+selected+['--blob-directory',str(folder),'--ffmpeg','/usr/bin/ffmpeg','--ffprobe','/usr/bin/ffprobe','--prlimit','/usr/bin/prlimit','--codec-temp-root','/tmp','--apply'],env=env))
   stage='inspect'
   original=json.loads(run(target+['--inspect-run',runid],env=env));assert result==original and original['status']=='COMMITTED' and original['counts']==prepared['counts']
   stage='repeat'
   repeated=json.loads(run(target+selected+['--blob-directory',str(folder),'--ffmpeg','/usr/bin/ffmpeg','--ffprobe','/usr/bin/ffprobe','--prlimit','/usr/bin/prlimit','--codec-temp-root','/tmp','--apply'],env=env));assert repeated==original
  print(json.dumps({'status':'PRIVATE_VIDEO_COMMITTED','result':original,'sourceWrites':0,'originalFactInspected':True,'repeatChecked':p['mode']=='apply','remoteCodecVerification':verified if p['mode']=='apply' else None}))
except Exception as e:
 print(json.dumps({'status':'UNKNOWN_REQUIRES_INSPECTION' if stage=='apply' else 'FAILED','stage':stage,'errorType':type(e).__name__,'controlledCode':str(e) if isinstance(e,RuntimeError) and re.fullmatch(r'legacy_[a-z_]+|CONTROLLED_COMMAND_FAILED',str(e)) else None,'privateValuesOmitted':True}));sys.exit(1)
'''
files={p.name:p for p in BLOBS.glob('*.blob')};files.update({n:MATCH/n for n in ['matched.private.json','authenticated-seal.json']});assert len(files)==7
payload={'mode':mode,'sealSHA':sealSHA,'files':{n:{'sha256':sha(p),'bytes':p.stat().st_size}for n,p in files.items()}}
bootstrap="import base64;exec(base64.b64decode('"+base64.b64encode(remote.encode()).decode()+"'))"
with (out/'stdout.json').open('xb')as stdout,(out/'stderr.log').open('xb')as stderr:
 proc=subprocess.Popen(d.SSH+['sudo -n python3 -c '+shlex.quote(bootstrap)],stdin=subprocess.PIPE,stdout=stdout,stderr=stderr,creationflags=subprocess.CREATE_NO_WINDOW)
 proc.stdin.write(json.dumps(payload).encode()+b'\n')
 if mode=='transfer':
  with tarfile.open(fileobj=proc.stdin,mode='w|')as tar:
   for name,path in sorted(files.items()):tar.add(path,arcname=name,recursive=False)
 proc.stdin.close();code=proc.wait()
result=json.loads((out/'stdout.json').read_bytes())
receipt={'mode':mode,'exitCode':code,'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'result':result}
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt));sys.exit(code)
