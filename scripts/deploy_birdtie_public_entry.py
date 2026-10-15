"""Switch the authorized existing HTTPS IP entry to native Birdtie.

Preserves old container, Caddyfile, TLS state, database and API for rollback.
The public fixed-code channel is an explicitly configured development test.
"""
import base64
import hashlib
import json
import re
import shlex
import subprocess
import sys
import tarfile
from pathlib import Path

ROOT = Path(r'D:\Project\birdtie')
ORIGIN = 'https://47.110.154.133'
SSH = [r'C:\Windows\System32\OpenSSH\ssh.exe', '-i',
       r'C:\Users\chens\.ssh\civu-prod-hz-admin.pem', '-o',
       'UserKnownHostsFile=C:/Users/chens/.ssh/known_hosts_civu', '-o',
       'StrictHostKeyChecking=yes', '-o', 'BatchMode=yes', '-o',
       'ConnectTimeout=15', 'ecs-user@47.110.154.133']

REMOTE = r'''
import hashlib,json,os,re,shutil,subprocess,sys,tarfile,time,urllib.request
from pathlib import Path
p=json.loads(sys.stdin.buffer.readline());raw=sys.stdin.buffer.read()
if p.get('stagedArchive'):
 assert p['stagedArchive']=='/opt/birdtie/web/releases/public-20261015-final-web01/incoming.tar' and not raw
 staged=Path(p['stagedArchive']);assert staged.is_file() and not staged.is_symlink()
 raw=staged.read_bytes()
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def run(argv):
 r=subprocess.run(argv,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 if r.returncode:raise RuntimeError('PUBLIC_ENTRY_STEP_FAILED:'+argv[0])
 return r.stdout
def ready(url):
 try:
  with urllib.request.urlopen(url,timeout=5) as r:return r.status==200 and json.load(r)['status']=='ready'
 except Exception:return False
def wait(url):
 for _ in range(30):
  if ready(url):return True
  time.sleep(.5)
 return False
state=json.loads(Path('/etc/birdtie/deployment.json').read_text())
assert state['database']=='birdtie_native_hz_20261015' and state['release']=='/opt/birdtie/releases/social155-20261015-build04'
assert digest(Path(state['release'])/'bin/birdtie-api')==p['apiSHA256']
assert run(['systemctl','show','birdtie-api.service','--property=MainPID','--value']).strip()!=b'0'
assert ready('http://127.0.0.1:18114/readyz')
old=json.loads(run(['docker','inspect','civu-caddy']))[0]
assert old['State']['Running'] and old['HostConfig']['NetworkMode']=='host'
assert old['Config']['Image']=='caddy:2.10-alpine'
assert any(m['Source']=='/opt/civu/Caddyfile' and not m['RW'] for m in old['Mounts'])
oldhash='d7e38d5451313782d46ed0f1555ca2f2043b382ea29d0c7135c0fe142073c671'
assert digest(Path('/opt/civu/Caddyfile'))==oldhash
assert '127.0.0.1:8080' in Path('/opt/civu/Caddyfile').read_text()
assert 'birdtie-caddy' not in run(['docker','ps','-a','--format','{{.Names}}']).decode().splitlines()
assert hashlib.sha256(raw).hexdigest()==p['archiveSHA256']
release=Path('/opt/birdtie/web/releases')/p['release'];assert not release.exists()
release.mkdir(parents=True,mode=0o755)
archive=release/'incoming.tar';archive.write_bytes(raw)
with tarfile.open(archive) as t:
 manifest={name.replace(chr(92),'/'):proof for name,proof in json.load(t.extractfile('birdtie-web-manifest.json')).items()}
 assert sorted(m.name for m in t.getmembers())==sorted(list(manifest)+['birdtie-web-manifest.json'])
 assert 'index.html' in manifest and 'main.dart.js' in manifest
 for m in t.getmembers():
  assert m.isfile() and not m.name.startswith('/') and '..' not in Path(m.name).parts
  assert not any(s in m.name for s in ('.env','api.env','deployment.json'))
  content=t.extractfile(m).read();out=release/m.name;out.parent.mkdir(parents=True,exist_ok=True);out.write_bytes(content)
  os.chmod(out,0o644)
  if m.name in manifest:assert len(content)==manifest[m.name]['bytes'] and hashlib.sha256(content).hexdigest()==manifest[m.name]['sha256']
archive.unlink()
config=Path('/etc/birdtie/Caddyfile');assert not config.exists()
original_config=Path('/opt/civu/Caddyfile').read_text()
email_lines=re.findall(r'^\s*(email\s+[^\s#]+)\s*$',original_config,re.M)
assert len(email_lines)==1 and re.search(r'^\s*default_sni\s+47[.]110[.]154[.]133\s*$',original_config,re.M)
cfg='\n{\n '+email_lines[0]+'\n default_sni 47.110.154.133\n}\n'+"""
https://47.110.154.133 {
 tls {
  issuer acme {
   dir https://acme-v02.api.letsencrypt.org/directory
   profile shortlived
  }
 }
 @api path /v1 /v1/* /healthz /readyz
 handle @api {
  reverse_proxy 127.0.0.1:18114
 }
 @oldadmin path /admin /admin/*
 handle @oldadmin {
  redir / 308
 }
 handle {
  root * /srv/birdtie-web
  try_files {path} /index.html
  file_server
 }
}
"""
config.write_text(cfg);os.chmod(config,0o644)
data=Path('/srv/birdtie-data/caddy');assert not data.exists();data.mkdir(mode=0o700)
(data/'config').mkdir(mode=0o700)
shutil.copytree('/srv/civu-data/caddy/data',data/'data')
current=Path('/opt/birdtie/web/current');assert not current.exists();current.symlink_to(release,target_is_directory=True)
image=old['Image']
run(['docker','run','--rm','--network','none','-v',str(config)+':/etc/caddy/Caddyfile:ro',image,'caddy','validate','--config','/etc/caddy/Caddyfile','--adapter','caddyfile'])
env=Path('/etc/birdtie/api.env');before=env.read_text();backup=env.with_name('api.env.before-public-entry');assert not backup.exists()
values=dict(line.split('=',1) for line in before.splitlines() if line and not line.startswith('#'))
assert values['BIRDTIE_DEV_PHONE_AUTH']=='true' and values['BIRDTIE_API_ADDR']=='127.0.0.1:18114'
assert values['BIRDTIE_ALLOWED_ORIGINS']=='http://localhost:7357'
assert 'BIRDTIE_DEV_PHONE_PUBLIC_TEST_ORIGIN' not in values
backup.write_text(before);os.chmod(backup,0o600)
after=before.replace('BIRDTIE_ALLOWED_ORIGINS=http://localhost:7357','BIRDTIE_ALLOWED_ORIGINS=http://localhost:7357,https://47.110.154.133')+'BIRDTIE_DEV_PHONE_PUBLIC_TEST_ORIGIN=https://47.110.154.133\n'
switched=False
try:
 env.write_text(after);os.chmod(env,0o600)
 run(['systemctl','restart','birdtie-api.service'])
 assert wait('http://127.0.0.1:18114/readyz'),'EXPLICIT_PUBLIC_TEST_API_FAILED'
 run(['docker','create','--name','birdtie-caddy','--network','host','--restart','unless-stopped','-v',str(config)+':/etc/caddy/Caddyfile:ro','-v',str(current)+':/srv/birdtie-web:ro','-v',str(data/'config')+':/config','-v',str(data/'data')+':/data',image,'caddy','run','--config','/etc/caddy/Caddyfile','--adapter','caddyfile'])
 run(['docker','stop','civu-caddy']);switched=True
 run(['docker','start','birdtie-caddy'])
 assert wait('https://47.110.154.133/readyz'),'PUBLIC_BIRDTIE_READINESS_FAILED'
 with urllib.request.urlopen('https://47.110.154.133/v1/auth/dev-phone/status',timeout=5) as r:assert r.status==200 and json.load(r)['data']['enabled']
 with urllib.request.urlopen('https://47.110.154.133/',timeout=5) as r:assert r.status==200 and b'flutter_bootstrap.js' in r.read()
 assert digest(Path('/opt/civu/Caddyfile'))==oldhash
 run(['systemctl','enable','birdtie-api.service'])
 print(json.dumps({'status':'PUBLIC_BIRDTIE_READY','origin':'https://47.110.154.133','webRelease':str(release),'webManifestSHA256':digest(release/'birdtie-web-manifest.json'),'caddyImage':image,'authentication':'EXPLICIT_FIXED_CODE_PUBLIC_TEST','fixedCode':'123456','apiLoopback':True,'databaseLoopback':True,'oldCaddyStopped':True,'oldCaddyfileUnchanged':True,'oldAPIStopped':False,'oldAssetsDeleted':False,'tlsDefaultValidation':True,'persistentConfig':str(config)}))
except Exception:
 if switched:
  subprocess.run(['docker','stop','birdtie-caddy'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  run(['docker','start','civu-caddy'])
 env.write_text(before);os.chmod(env,0o600);run(['systemctl','restart','birdtie-api.service'])
 raise RuntimeError('PUBLIC_ENTRY_FAILED_OLD_ROUTE_RESTORED') from None
'''


def main():
    name, evidence = sys.argv[1:3]
    assert all(re.fullmatch('[a-z0-9-]+', value) for value in (name, evidence))
    proof = json.loads((ROOT / 'docs/testing/evidence/social-remote-migration-20261015' / name / 'receipt.json').read_text())
    assert proof['effective_exit_code'] == 0 and proof['source_stable']
    assert proof['api_target'] == ORIGIN
    source = Path(proof['artifact_path'])
    manifest = {name.replace('\\', '/'): item for name, item in proof['artifacts'].items()}
    for relative, item in manifest.items():
        path = source / relative
        assert hashlib.sha256(path.read_bytes()).hexdigest() == item['sha256']
    assert all(hashlib.sha256((ROOT / p).read_bytes()).hexdigest() == h for p, h in proof['source_after'].items() if not p.startswith('apps/api/'))
    api = json.loads((ROOT / 'docs/testing/evidence/social-remote-migration-20261015/build04/receipt.json').read_text())
    assert api['source_stable'] and api['target_migrations'] == 155
    assert all(hashlib.sha256((ROOT / p).read_bytes()).hexdigest() == h for p, h in api['source_after'].items())
    out = ROOT / 'docs/testing/evidence/social-remote-migration-20261015' / evidence
    out.mkdir(parents=True, exist_ok=False)
    artifact = ROOT / 'work/social-remote-migration-20261015' / evidence
    artifact.mkdir(parents=True, exist_ok=False)
    (artifact / 'birdtie-web-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    archive = artifact / 'web.tar'
    with tarfile.open(archive, 'w') as tar:
        for relative in sorted(manifest):
            tar.add(source / relative, arcname=relative, recursive=False)
        tar.add(artifact / 'birdtie-web-manifest.json', arcname='birdtie-web-manifest.json')
    raw = archive.read_bytes()
    payload = {'release': 'public-20261015-' + name + '-' + evidence, 'archiveSHA256': hashlib.sha256(raw).hexdigest(), 'apiSHA256': api['manifest']['bin/birdtie-api']['sha256']}
    if len(sys.argv) == 4:
        assert sys.argv[3] == '--reuse-staged-first'
        first = json.loads((ROOT / 'docs/testing/evidence/social-remote-migration-20261015/public-entry01/receipt.json').read_text())
        assert first['exitCode'] == 1
        payload['stagedArchive'] = '/opt/birdtie/web/releases/public-20261015-final-web01/incoming.tar'
        payload['archiveSHA256'] = first['archiveSHA256']
        raw = b''
    bootstrap = "import base64;exec(base64.b64decode('" + base64.b64encode(REMOTE.encode()).decode() + "'))"
    result = subprocess.run(SSH + ['sudo -n python3 -c ' + shlex.quote(bootstrap)], input=json.dumps(payload).encode() + b'\n' + raw, stdout=subprocess.PIPE, stderr=subprocess.PIPE, creationflags=subprocess.CREATE_NO_WINDOW)
    (out / 'stdout.json').write_bytes(result.stdout)
    (out / 'stderr.log').write_bytes(result.stderr)
    receipt = {'exitCode': result.returncode, 'origin': ORIGIN, 'remoteScriptSHA256': hashlib.sha256(REMOTE.encode()).hexdigest(), 'archiveSHA256': payload['archiveSHA256'], 'oldAssetsDeleted': False}
    if result.returncode == 0:
        receipt['result'] = json.loads(result.stdout)
    (out / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps(receipt))
    return result.returncode


if __name__ == '__main__':
    sys.exit(main())
