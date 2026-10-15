import ast,base64,hashlib,importlib.util,json,shlex,subprocess,tarfile
from pathlib import Path
ROOT=Path(r'D:\Project\birdtie')
spec=importlib.util.spec_from_file_location('gateway',ROOT/'scripts/deploy_birdtie_public_entry.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
base=ROOT/'docs/testing/evidence/social-remote-migration-20261015'
old=json.loads((base/'final-web09/receipt.json').read_text());new=json.loads((base/'final-web10/receipt.json').read_text())
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
assert new['source_stable'] and new['effective_exit_code']==0 and new['api_target']=='https://47.110.154.133'
assert all(sha(ROOT/p)==h for p,h in new['source_after'].items())
normalize=lambda x:{n.replace(chr(92),'/'):v for n,v in x['artifacts'].items()}
oldman,newman=normalize(old),normalize(new)
assert set(oldman)==set(newman)
changed=[n for n in newman if oldman[n]!=newman[n]]
assert changed and len(changed)<12
out=base/'public-client10';out.mkdir(parents=True,exist_ok=False)
work=ROOT/'work/social-remote-migration-20261015/public-client10';work.mkdir(parents=True,exist_ok=False)
with tarfile.open(work/'delta.tar','w') as t:
 for n in sorted(changed):
  path=Path(new['artifact_path'])/n;assert sha(path)==newman[n]['sha256'];t.add(path,arcname=n,recursive=False)
raw=(work/'delta.tar').read_bytes()
canonical=lambda v:hashlib.sha256(json.dumps(v,sort_keys=True,separators=(',',':')).encode()).hexdigest()
payload={'oldManifestSHA':canonical(oldman),'newManifest':newman,'newManifestSHA':canonical(newman),'changed':changed,'deltaSHA':hashlib.sha256(raw).hexdigest()}
remote=r'''
import hashlib,json,os,shutil,subprocess,sys,tarfile,time,urllib.request
from pathlib import Path
p=json.loads(sys.stdin.buffer.readline());raw=sys.stdin.buffer.read()
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def canonical(v):return hashlib.sha256(json.dumps(v,sort_keys=True,separators=(',',':')).encode()).hexdigest()
def run(args):
 r=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 if r.returncode:raise RuntimeError('CLIENT_UPDATE_STEP_FAILED')
 return r.stdout
def public_ready():
 try:
  with urllib.request.urlopen('https://47.110.154.133/readyz',timeout=4) as r:return r.status==200 and json.load(r)['status']=='ready'
 except Exception:return False
assert public_ready()
state=json.loads(Path('/etc/birdtie/deployment.json').read_text());assert state['release']=='/opt/birdtie/releases/social167-20261015-build14'
old=json.loads(run(['docker','inspect','birdtie-caddy']))[0]
assert old['State']['Running'] and old['HostConfig']['NetworkMode']=='host' and old['HostConfig']['RestartPolicy']['Name']=='unless-stopped'
assert not json.loads(run(['docker','inspect','civu-caddy']))[0]['State']['Running']
assert 'birdtie-caddy-client09' not in run(['docker','ps','-a','--format','{{.Names}}']).decode().splitlines()
oldrelease=Path('/opt/birdtie/web/releases/public-20261015-final-web09')
current=Path('/opt/birdtie/web/current');assert current.resolve()==oldrelease
oldman={n.replace(chr(92),'/'):v for n,v in json.loads((oldrelease/'birdtie-web-manifest.json').read_text()).items()}
assert canonical(oldman)==p['oldManifestSHA'] and canonical(p['newManifest'])==p['newManifestSHA']
assert set(oldman)==set(p['newManifest']) and sorted(p['changed'])==sorted(n for n in oldman if oldman[n]!=p['newManifest'][n])
assert all(digest(oldrelease/n)==v['sha256'] for n,v in oldman.items())
assert hashlib.sha256(raw).hexdigest()==p['deltaSHA']
release=Path('/opt/birdtie/web/releases/public-20261015-final-web10');assert not release.exists()
shutil.copytree(oldrelease,release)
archive=release/'client-delta.tar';archive.write_bytes(raw)
with tarfile.open(archive) as t:
 assert sorted(m.name for m in t.getmembers())==sorted(p['changed'])
 for member in t.getmembers():
  assert member.isfile() and not member.name.startswith('/') and '..' not in Path(member.name).parts
  data=t.extractfile(member).read();proof=p['newManifest'][member.name]
  assert len(data)==proof['bytes'] and hashlib.sha256(data).hexdigest()==proof['sha256']
  (release/member.name).write_bytes(data)
archive.unlink()
(release/'birdtie-web-manifest.json').write_text(json.dumps(p['newManifest'],indent=2)+'\n')
assert all(digest(release/n)==v['sha256'] for n,v in p['newManifest'].items())
cfg=Path('/etc/birdtie/Caddyfile');cfg_hash=digest(cfg)
assert 'default_sni 47.110.154.133' in cfg.read_text() and 'profile shortlived' in cfg.read_text()
switched=False
try:
 run(['docker','stop','birdtie-caddy']);run(['docker','rename','birdtie-caddy','birdtie-caddy-client09']);switched=True
 replacement=current.with_name('current.client10');assert not replacement.exists();replacement.symlink_to(release,target_is_directory=True);os.replace(replacement,current)
 run(['docker','create','--name','birdtie-caddy','--network','host','--restart','unless-stopped','-v','/etc/birdtie/Caddyfile:/etc/caddy/Caddyfile:ro','-v',str(current)+':/srv/birdtie-web:ro','-v','/srv/birdtie-data/caddy/config:/config','-v','/srv/birdtie-data/caddy/data:/data',old['Image'],'caddy','run','--config','/etc/caddy/Caddyfile','--adapter','caddyfile'])
 run(['docker','start','birdtie-caddy'])
 for _ in range(30):
  if public_ready():break
  time.sleep(.5)
 assert public_ready() and digest(cfg)==cfg_hash
 with urllib.request.urlopen('https://47.110.154.133/main.dart.js',timeout=20) as r:assert r.status==200 and hashlib.sha256(r.read()).hexdigest()==p['newManifest']['main.dart.js']['sha256']
 print(json.dumps({'status':'CLIENT10_PUBLIC_READY','origin':'https://47.110.154.133','webRelease':str(release),'newManifestSHA':p['newManifestSHA'],'changedFiles':p['changed'],'unchangedAssetsReused':len(oldman)-len(p['changed']),'apiRestarted':False,'databaseMigrationsApplied':0,'previousGatewayPreserved':'birdtie-caddy-client09','currentGateway':'birdtie-caddy','tlsDefaultValidation':True,'configUnchanged':True,'oldCivuAssetsDeleted':False}))
except Exception:
 if switched:
  subprocess.run(['docker','stop','birdtie-caddy'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  rollback=current.with_name('current.rollback10');assert not rollback.exists();rollback.symlink_to(oldrelease,target_is_directory=True);os.replace(rollback,current)
  run(['docker','start','birdtie-caddy-client09'])
 print(json.dumps({'status':'FAILED_PREVIOUS_BIRDTIE_GATEWAY_RESTORED','oldCivuAssetsDeleted':False}))
 raise SystemExit(1)
'''
ast.parse(remote);(out/'controlled-client-update.py').write_text(Path(__file__).read_text())
bootstrap="import base64;exec(base64.b64decode('"+base64.b64encode(remote.encode()).decode()+"'))"
r=subprocess.run(m.SSH+['sudo -n python3 -c '+shlex.quote(bootstrap)],input=json.dumps(payload).encode()+b'\n'+raw,stdout=subprocess.PIPE,stderr=subprocess.PIPE,creationflags=subprocess.CREATE_NO_WINDOW)
(out/'stdout.json').write_bytes(r.stdout);(out/'stderr.log').write_bytes(r.stderr)
proof={'exitCode':r.returncode,'deltaBytes':len(raw),'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'result':json.loads(r.stdout) if r.stdout else None}
(out/'receipt.json').write_text(json.dumps(proof,indent=2)+'\n');print(json.dumps(proof));raise SystemExit(r.returncode)
