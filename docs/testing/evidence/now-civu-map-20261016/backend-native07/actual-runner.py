import os,sys,json,re,subprocess,hashlib
from pathlib import Path
from datetime import datetime,timezone
from urllib.parse import quote
ROOT=Path(r'D:\Project\birdtie')
BASE=Path(__file__).parent
OUT=BASE/sys.argv[1]
OUT.mkdir(parents=True,exist_ok=False)
assert not (OUT/'stdout.jsonl').exists()
sys.path.insert(0,str(ROOT/'work/now-v4-v41-integration-20261010/continuation20'))
from common21 import api_frame,read,WORK,EV
expected=read(EV/'continuation19/database/identity01.json')['actual']
assert expected['name']==read(WORK/'active-database.json')['database']
assert re.fullmatch(r'birdtie_phone_owned_[0-9a-f]{12}',expected['name'])
sql="SELECT json_build_object('name',datname,'oid',oid,'owner',pg_get_userbyid(datdba),'marker',shobj_description(oid,'pg_database'))::text FROM pg_database WHERE datname='"+expected['name']+"';"
def run(command,**kwargs):
 return subprocess.run(command,creationflags=subprocess.CREATE_NO_WINDOW,**kwargs)
def identity():
 p=run(['docker','exec','-i','api-db-1','psql','-X','-qAt','-U','birdtie','-d','postgres'],input=sql.encode(),capture_output=True,check=True)
 return json.loads(p.stdout)
assert identity()==expected
c=json.loads(run(['docker','inspect','api-db-1'],capture_output=True,check=True).stdout)[0]
v=dict(x.split('=',1)for x in c['Config']['Env'] if '='in x)
env=os.environ.copy()
for key in list(env):
 if key.startswith(('BIRDTIE_','PG')):env.pop(key)
env.update(BIRDTIE_DATABASE_URL='postgresql://'+quote(v['POSTGRES_USER'],safe='')+':'+quote(v['POSTGRES_PASSWORD'],safe='')+'@127.0.0.1:55432/'+expected['name']+'?sslmode=disable',BIRDTIE_DISPOSABLE_DB='1',BIRDTIE_LEGACY_IMPORT_NATIVE_TEST='1')
codec=Path(r'C:\Users\chens\AppData\Local\Microsoft\WinGet\Packages\Gyan.FFmpeg_Microsoft.Winget.Source_8wekyb3d8bbwe\ffmpeg-9.0.1-full_build\bin')
assert (codec/'ffmpeg.exe').is_file() and (codec/'ffprobe.exe').is_file()
env.update(BIRDTIE_PUBLIC_VIDEO_FFMPEG=str(codec/'ffmpeg.exe'),BIRDTIE_PUBLIC_VIDEO_FFPROBE=str(codec/'ffprobe.exe'))
SNAP=OUT/"api"
import shutil
files=subprocess.check_output(["git","ls-files","-c","-o","--exclude-standard","--","apps/api"],cwd=ROOT,text=True).splitlines()
for rel in files:
 src=ROOT/rel
 if not src.is_file(): continue
 target=SNAP/Path(rel).relative_to("apps/api")
 target.parent.mkdir(parents=True,exist_ok=True)
 shutil.copy2(src,target)
SCOPES=[p.relative_to(ROOT).as_posix() for p in [*(ROOT/'apps/api/internal/legacymap').glob('*.go'),*(ROOT/'apps/api/cmd/legacy-map-restore').glob('*.go'),*(ROOT/'apps/api/internal/content').glob('map_content*.go'),*(ROOT/'apps/api/internal/httpapi').glob('map_content*.go'),*(ROOT/'apps/api/internal/postgres').glob('map_content*.go'),ROOT/'apps/api/internal/postgres/legacy_map_restore.go',ROOT/'apps/api/migrations/170_legacy_map_visibility.sql',ROOT/'apps/api/migrations/170_legacy_map_visibility.down.sql']]
def assigned_frame():return {p:hashlib.sha256((ROOT/p).read_bytes()).hexdigest()for p in SCOPES}
def api_frame():return {p.relative_to(SNAP).as_posix():hashlib.sha256(p.read_bytes()).hexdigest()for p in SNAP.rglob('*')if p.is_file()}
assigned_before=assigned_frame()
before=api_frame()
cmd=[r'C:\Program Files\Go\bin\go.exe','test','-mod=readonly','./internal/postgres','-count=1','-json','-run','^TestMapContent170NativeOriginalPublicAndOwnPrivateCurrent$']
with (OUT/'stdout.jsonl').open('xb')as stdout,(OUT/'stderr.log').open('xb')as stderr:
 p=run(cmd,cwd=SNAP,env=env,stdout=stdout,stderr=stderr)
events=[]
for line in (OUT/'stdout.jsonl').read_text().splitlines():
 try:events.append(json.loads(line))
 except ValueError:pass
counts={k:sum(e.get('Action')==k and 'Test'in e for e in events) for k in ('pass','fail','skip')}
after=api_frame()
receipt={'at':datetime.now(timezone.utc).isoformat(),'command':cmd,'cwd':str(SNAP),'exitCode':p.returncode,'tests':counts,'sourceStable':before==after,'sourceBefore':before,'sourceAfter':after,'parentIdentityStable':identity()==expected,'testDatabase':'Separate random marked disposable child; parent and source are not migrated','assignedParentSourceStable':assigned_before==assigned_frame(),'sourceFrameScope':'Frozen complete API snapshot; assigned170 parent source hash checked separately'}
(OUT/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
print(json.dumps({k:v for k,v in receipt.items() if k not in ('sourceBefore','sourceAfter')}))
if p.returncode:
 for e in events:
  if 'Output'in e and any(k in e['Output']for k in ('FAIL','integration_test.go','migration failed','ERROR','controlled')):print(e['Output'].strip())
 print((OUT/'stderr.log').read_text()[:1800])
sys.exit(p.returncode or (0 if counts['pass']==1 and not counts['skip'] else 2))

