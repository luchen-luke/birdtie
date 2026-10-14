"""Controlled native Birdtie deployment on the explicitly authorized HZ host.

Never points a Birdtie writer at Civu, copies its schema, accepts old sessions,
deletes old assets, or changes public routing. Private inputs remain outside the
repository and are streamed into a permission-restricted remote directory.
"""
import argparse,hashlib,json,os,re,subprocess,sys,uuid
from pathlib import Path

ROOT=Path(r'D:\Project\birdtie')
HOST='ecs-user@47.110.154.133'
SSH=[r'C:\Windows\System32\OpenSSH\ssh.exe','-i',r'C:\Users\chens\.ssh\civu-prod-hz-admin.pem','-o','UserKnownHostsFile=C:/Users/chens/.ssh/known_hosts_civu','-o','StrictHostKeyChecking=yes','-o','BatchMode=yes','-o','ConnectTimeout=15',HOST]
PRIVATE=Path(r'C:\Users\chens\Documents\Codex\2026-10-14\birdtie-hangzhou\civu-private-extract-20261015-01')
RUN='c5df0d7c-f361-4275-9838-feb6d1d92352'
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()

REMOTE=r'''
import sys,os,json,re,hashlib,secrets,subprocess,tarfile,urllib.request,time
from pathlib import Path
payload=json.loads(sys.stdin.buffer.readline())
ROOT=Path('/opt/birdtie');STATE=Path('/etc/birdtie/deployment.json');ENV=Path('/etc/birdtie/api.env')
PRIVATE=Path('/srv/birdtie-data/migration');DB='birdtie_native_hz_20261015';ROLE='birdtie_native_hz'
def run(command,raw=None,env=None):
 p=subprocess.run(command,input=raw,stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env)
 if p.returncode:raise RuntimeError('remote_step_failed:'+command[0])
 return p.stdout
def sql(db,q,admin=False):
 if admin:return run(['sudo','-n','-u','postgres','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-d',db],q.encode())
 e=os.environ.copy();e.update(PGHOST='127.0.0.1',PGPORT='5432',PGDATABASE=db,PGUSER=ROLE,PGPASSWORD=settings['password'])
 return run(['psql','-X','-qAt','-v','ON_ERROR_STOP=1'],q.encode(),e)
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def write(p,value,mode=0o600):
 p.parent.mkdir(parents=True,exist_ok=True);os.chmod(p.parent,0o700)
 with p.open('x',encoding='utf-8')as f:f.write(value)
 os.chmod(p,mode)
mode=payload['mode']
if mode=='prepare':
 assert not STATE.exists() and not ROOT.exists(),'unknown prior deployment'
 assert not sql('postgres',"SELECT 1 FROM pg_database WHERE datname='"+DB+"';",True).strip(),'unknown existing target'
 assert not sql('postgres',"SELECT 1 FROM pg_roles WHERE rolname='"+ROLE+"';",True).strip(),'unknown existing role'
 source_oid=int(sql('postgres',"SELECT oid FROM pg_database WHERE datname='civu';",True))
 data=sys.stdin.buffer.read();assert hashlib.sha256(data).hexdigest()==payload['releaseSHA256']
 ROOT.mkdir(mode=0o755);release=ROOT/'releases'/payload['release'];release.mkdir(parents=True,mode=0o755)
 archive=ROOT/'release-incoming.tar';archive.write_bytes(data)
 with tarfile.open(archive)as t:
  manifest=json.load(t.extractfile('manifest.json'))
  assert len(manifest)==150 and all(re.fullmatch(r'(bin/birdtie-(api|legacy-import)|migrations/[0-9]{3}_[a-z0-9_]+\.sql)',n)for n in manifest)
  assert sorted(m.name for m in t.getmembers())==sorted(list(manifest)+['manifest.json'])
  for m in t.getmembers():
   assert m.isfile() and not m.name.startswith('/') and '..'not in Path(m.name).parts
   raw=t.extractfile(m).read();p=release/m.name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(raw)
   if m.name in manifest:assert len(raw)==manifest[m.name]['bytes'] and hashlib.sha256(raw).hexdigest()==manifest[m.name]['sha256']
 for p in (release/'bin').iterdir():os.chmod(p,0o755)
 password=secrets.token_hex(32);marker='birdtie-owned-legacy-import-2026-10-15:'+secrets.token_hex(16)
 sql('postgres',"CREATE ROLE "+ROLE+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT PASSWORD '"+password+"';",True)
 sql('postgres','CREATE DATABASE '+DB+' OWNER '+ROLE+' TEMPLATE template0;',True)
 sql('postgres',"COMMENT ON DATABASE "+DB+" IS '"+marker+"';",True)
 oid=int(sql('postgres',"SELECT oid FROM pg_database WHERE datname='"+DB+"';",True));assert oid!=source_oid
 settings={'database':DB,'oid':oid,'owner':ROLE,'marker':marker,'sourceOID':source_oid,'password':password,'release':str(release)}
 write(STATE,json.dumps(settings,indent=2)+'\n')
 sql(DB,'CREATE TABLE birdtie_schema_migrations(version integer PRIMARY KEY,filename text UNIQUE NOT NULL,sha256 char(64) NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp());')
 for name in sorted(n for n in manifest if n.startswith('migrations/')):
  number=int(Path(name).name.split('_')[0]);body=(release/name).read_text(encoding='utf-8')
  body=re.sub(r'(?im)^\s*(BEGIN|COMMIT);\s*$','',body)
  sql(DB,"BEGIN;SET LOCAL lock_timeout='20s';SET LOCAL statement_timeout='120s';\n"+body+"\nINSERT INTO birdtie_schema_migrations(version,filename,sha256)VALUES("+str(number)+",'"+Path(name).name+"','"+manifest[name]['sha256']+"');COMMIT;\n")
 assert int(sql(DB,'SELECT count(*) FROM birdtie_schema_migrations;'))==148
 PRIVATE.mkdir(parents=True,mode=0o700);os.chmod(PRIVATE,0o700)
 # Remote API stays loopback. Fixed development auth is reachable only through
 # an explicit loopback SSH channel, retaining the repository's existing gate.
 dsn='postgresql://'+ROLE+':'+password+'@127.0.0.1:5432/'+DB+'?sslmode=disable&pool_max_conns=4'
 text='\n'.join(['BIRDTIE_DATABASE_URL='+dsn,'BIRDTIE_NATIVE_DATABASE_NAME='+DB,
  'BIRDTIE_API_ADDR=127.0.0.1:18114','BIRDTIE_ALLOWED_ORIGINS=http://localhost:7357',
  'BIRDTIE_DEV_PHONE_AUTH=true','BIRDTIE_BACKGROUND_MAINTENANCE=false',
  'BIRDTIE_OPEN_PLACES_ENABLED=0','BIRDTIE_SYSTEM_PUSH_ENABLED=false',
  'BIRDTIE_VIDEO_ENABLED=0','BIRDTIE_AGENT_ACTIVITY_JOIN_ENABLED=false'])+'\n'
 write(ENV,text)
 print(json.dumps({'stage':'PREPARED','database':DB,'databaseOID':oid,'owner':ROLE,'sourceDatabaseOID':source_oid,'migrations':148,'release':str(release),'publicRoutingChanged':False}))
elif mode=='private-input':
 settings=json.loads(STATE.read_text());assert settings['database']==DB
 name=payload['name'];assert name in('source.json','manifest.json')
 raw=sys.stdin.buffer.read();assert hashlib.sha256(raw).hexdigest()==payload['sha256']
 p=PRIVATE/name;assert not p.exists();p.write_bytes(raw);os.chmod(p,0o600)
 print(json.dumps({'stage':'PRIVATE_INPUT_STAGED','bytes':len(raw),'repositoryDataCopied':False}))
elif mode=='apply':
 settings=json.loads(STATE.read_text());assert settings['database']==DB
 src=PRIVATE/'source.json';man=PRIVATE/'manifest.json'
 assert digest(src)==payload['sourceSHA256'] and digest(man)==payload['manifestSHA256']
 e={k:v for k,v in os.environ.items()if not k.startswith(('PG','BIRDTIE_'))}
 e['BIRDTIE_DATABASE_URL']='postgresql://'+ROLE+':'+settings['password']+'@127.0.0.1:5432/'+DB+'?sslmode=disable'
 e['BIRDTIE_REPOSITORY_ROOT']='/opt/birdtie'
 base=[settings['release']+'/bin/birdtie-legacy-import','--database',DB,'--database-oid',str(settings['oid']),'--database-owner',ROLE,'--database-marker',settings['marker'],'--source-database-oid',str(settings['sourceOID'])]
 summary=json.loads(run(base+['--apply','--run-id',payload['runID'],'--source',str(src),'--manifest',str(man)],env=e))
 original=json.loads(run(base+['--inspect-run',payload['runID']],env=e))
 assert summary==original and summary['status']=='COMMITTED' and not summary['activated']
 assert digest(src)==payload['sourceSHA256'] and digest(man)==payload['manifestSHA256']
 counts=json.loads(sql(DB,"SELECT json_build_object('mappedOwners',(SELECT count(*) FROM legacy_entity_mappings WHERE entity_kind='user'),'notes',(SELECT count(*) FROM legacy_note_archives),'liveNotes',(SELECT count(*) FROM legacy_note_archives WHERE NOT deleted),'tombstones',(SELECT count(*) FROM legacy_note_archives WHERE deleted),'activeImportedAccounts',(SELECT count(*) FROM accounts WHERE status='active'),'sessions',(SELECT count(*) FROM sessions));"))
 assert counts=={'mappedOwners':20,'notes':103,'liveNotes':83,'tombstones':20,'activeImportedAccounts':0,'sessions':0}
 print(json.dumps({'stage':'COMMITTED','summary':summary,'counts':counts,'sourceWrites':0,'identityActivated':False,'oldSessionsCopied':False}))
elif mode=='start':
 settings=json.loads(STATE.read_text());assert settings['database']==DB
 unit=Path('/etc/systemd/system/birdtie-api.service');assert not unit.exists()
 text='[Unit]\nDescription=Birdtie controlled native API\nAfter=postgresql@16-main.service network.target\n[Service]\nType=simple\nUser=ecs-user\nWorkingDirectory='+settings['release']+'\nEnvironmentFile=/etc/birdtie/api.env\nExecStart='+settings['release']+'/bin/birdtie-api\nRestart=on-failure\nRestartSec=5\nNoNewPrivileges=true\nPrivateTmp=true\nProtectSystem=strict\nProtectHome=true\nMemoryMax=256M\nLimitNOFILE=65536\n[Install]\nWantedBy=multi-user.target\n'
 # Only root reads the protected EnvironmentFile; application runs unprivileged.
 unit.write_text(text);os.chmod(unit,0o644)
 run(['systemctl','daemon-reload']);run(['systemctl','start','birdtie-api.service'])
 ready=False
 for _ in range(30):
  try:
   with urllib.request.urlopen('http://127.0.0.1:18114/readyz',timeout=2)as response:ready=response.status==200 and json.load(response)['status']=='ready'
  except Exception:pass
  if ready:break
  time.sleep(.5)
 assert ready,'new API readiness failed; old public service remains available'
 print(json.dumps({'stage':'READY','nativeAPI':'127.0.0.1:18114','database':DB,'service':'birdtie-api.service','devPhone':'LOOPBACK_ONLY','publicRoutingChanged':False,'oldServiceStopped':False,'backgroundMaintenance':False}))
elif mode=='upgrade':
 settings=json.loads(STATE.read_text());assert settings['database']==DB
 current=json.loads(sql('postgres',"SELECT json_build_object('oid',oid,'owner',pg_get_userbyid(datdba),'marker',shobj_description(oid,'pg_database')) FROM pg_database WHERE datname='"+DB+"';",True))
 assert current=={'oid':settings['oid'],'owner':ROLE,'marker':settings['marker']}
 data=sys.stdin.buffer.read();assert hashlib.sha256(data).hexdigest()==payload['releaseSHA256']
 release=ROOT/'releases'/payload['release'];assert not release.exists();release.mkdir(mode=0o755)
 archive=release/'release.tar';archive.write_bytes(data)
 with tarfile.open(archive)as t:
  manifest=json.load(t.extractfile('manifest.json'));assert len(manifest)==152
  assert sorted(m.name for m in t.getmembers())==sorted(list(manifest)+['manifest.json'])
  for m in t.getmembers():
   assert m.isfile() and re.fullmatch(r'(manifest\.json|bin/birdtie-(api|legacy-import|legacy-personal-import)|migrations/[0-9]{3}_[a-z0-9_]+\.sql)',m.name)
   raw=t.extractfile(m).read();p=release/m.name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(raw)
   if m.name in manifest:assert len(raw)==manifest[m.name]['bytes'] and hashlib.sha256(raw).hexdigest()==manifest[m.name]['sha256']
 prior=sql(DB,'SELECT filename,sha256 FROM birdtie_schema_migrations ORDER BY version;').decode().strip().splitlines()
 assert len(prior)==148
 for row in prior:
  filename,digest1=row.split('|');assert manifest['migrations/'+filename]['sha256']==digest1
 for p in (release/'bin').iterdir():os.chmod(p,0o755)
 name='migrations/149_legacy_personal_archives.sql';body=(release/name).read_text();body=re.sub(r'(?im)^\s*(BEGIN|COMMIT);\s*$','',body)
 sql(DB,"BEGIN;SET LOCAL lock_timeout='20s';SET LOCAL statement_timeout='120s';\n"+body+"\nINSERT INTO birdtie_schema_migrations(version,filename,sha256)VALUES(149,'149_legacy_personal_archives.sql','"+manifest[name]['sha256']+"');COMMIT;\n")
 unit=Path('/etc/systemd/system/birdtie-api.service');text=unit.read_text();old=settings['release']+'/bin/birdtie-api'
 assert text.count('ExecStart='+old)==1 and 'User=ecs-user' in text
 previous=settings['release'];settings['release']=str(release)
 STATE.write_text(json.dumps(settings,indent=2)+'\n');os.chmod(STATE,0o600)
 unit.write_text(text.replace('ExecStart='+old,'ExecStart='+str(release)+'/bin/birdtie-api').replace('WorkingDirectory='+previous,'WorkingDirectory='+str(release)))
 run(['systemctl','daemon-reload']);run(['systemctl','restart','birdtie-api.service'])
 ready=False
 for _ in range(30):
  try:
   with urllib.request.urlopen('http://127.0.0.1:18114/readyz',timeout=2)as response:ready=response.status==200 and json.load(response)['status']=='ready'
  except Exception:pass
  if ready:break
  time.sleep(.5)
 assert ready
 print(json.dumps({'stage':'UPGRADED_READY','migrations':149,'original148Unchanged':True,'release':str(release),'previousRelease':previous,'publicRoutingChanged':False,'oldAssetsDeleted':False}))
elif mode=='personal-apply':
 settings=json.loads(STATE.read_text());assert settings['database']==DB
 src=PRIVATE/'source.json';man=PRIVATE/'manifest.json'
 assert digest(src)==payload['sourceSHA256'] and digest(man)==payload['manifestSHA256']
 e={k:v for k,v in os.environ.items()if not k.startswith(('PG','BIRDTIE_'))}
 e['BIRDTIE_DATABASE_URL']='postgresql://'+ROLE+':'+settings['password']+'@127.0.0.1:5432/'+DB+'?sslmode=disable';e['BIRDTIE_REPOSITORY_ROOT']='/opt/birdtie'
 base=[settings['release']+'/bin/birdtie-legacy-personal-import','--database',DB,'--database-oid',str(settings['oid']),'--database-owner',ROLE,'--database-marker',settings['marker'],'--source-database-oid',str(settings['sourceOID'])]
 summary=json.loads(run(base+['--apply','--run-id',payload['runID'],'--source',str(src),'--manifest',str(man)],env=e))
 original=json.loads(run(base+['--inspect-run',payload['runID']],env=e));assert summary==original and summary['status']=='COMMITTED' and not summary['activated']
 assert summary['counts']=={'journeys':34,'journeyTombstones':12,'themeMaps':3,'themeMapTombstones':0,'savedPlaces':189}
 assert digest(src)==payload['sourceSHA256'] and digest(man)==payload['manifestSHA256']
 print(json.dumps({'stage':'PERSONAL_ARCHIVES_COMMITTED','summary':summary,'sourceWrites':0,'currentSavedItemsCreated':False,'currentJourneysPublished':False,'mediaImported':False}))
else:raise RuntimeError('unknown stage')
'''

def main():
 p=argparse.ArgumentParser();p.add_argument('mode',choices=['prepare','private-input','apply','start','upgrade','personal-apply']);p.add_argument('--name',choices=['source.json','manifest.json']);p.add_argument('--evidence-name',required=True);a=p.parse_args()
 assert re.fullmatch(r'[a-z0-9-]+',a.evidence_name)
 out=ROOT/'docs/testing/evidence/social-remote-migration-20261015'/a.evidence_name;out.mkdir(parents=True,exist_ok=False)
 payload={'mode':a.mode};raw=b''
 if a.mode in ('prepare','upgrade'):
  build='build01' if a.mode=='prepare' else 'build02'
  artifact=ROOT/'work/social-remote-migration-20261015'/build/'release.tar';proof=json.loads((ROOT/'docs/testing/evidence/social-remote-migration-20261015'/build/'receipt.json').read_text());assert sha(artifact)==proof['artifact_sha256']
  assert all(sha(ROOT/p)==h for p,h in proof['source_after'].items() if not p.endswith('_test.go'))
  raw=artifact.read_bytes();payload.update(release='social148-20261015-01' if a.mode=='prepare' else 'social149-20261015-02',releaseSHA256=sha(artifact))
 elif a.mode=='private-input':
  assert a.name;src=PRIVATE/a.name;raw=src.read_bytes();payload.update(name=a.name,sha256=sha(src))
 elif a.mode in ('apply','personal-apply'):payload.update(runID=RUN,sourceSHA256=sha(PRIVATE/'source.json'),manifestSHA256=sha(PRIVATE/'manifest.json'))
 encoded=REMOTE.encode();bootstrap="import base64;exec(base64.b64decode('"+__import__('base64').b64encode(encoded).decode()+"'))"
 command=SSH+['sudo -n python3 -c '+__import__('shlex').quote(bootstrap)]
 result=subprocess.run(command,input=json.dumps(payload).encode()+b'\n'+raw,stdout=subprocess.PIPE,stderr=subprocess.PIPE,creationflags=subprocess.CREATE_NO_WINDOW)
 (out/'stdout.json').write_bytes(result.stdout)
 # Remote errors must be fixed codes, not raw PG SQL or private inputs.
 (out/'stderr.log').write_bytes(result.stderr)
 receipt={'mode':a.mode,'exit_code':result.returncode,'host':HOST,'strict_host_key_checking':True,'remoteScriptSHA256':hashlib.sha256(encoded).hexdigest(),'stdoutSHA256':sha(out/'stdout.json'),'stderrSHA256':sha(out/'stderr.log'),'public_routing_changed':False}
 if result.returncode==0:receipt['result']=json.loads(result.stdout)
 (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
 print(json.dumps({'mode':a.mode,'exit_code':result.returncode,'result':receipt.get('result')}))
 if result.returncode:print('Remote stage failed; preserved evidence; no blind retry or public cutover')
 return result.returncode
if __name__=='__main__':sys.exit(main())
