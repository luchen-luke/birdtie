import sys,os,json,subprocess,re
from pathlib import Path
from urllib.parse import quote
ROOT=Path(r'D:\Project\birdtie');sys.path.insert(0,str(ROOT/'work/now-v4-v41-integration-20261010/continuation20'))
from common21 import api_frame,read,save,sha,now,WORK,EV
name,pattern,*options=sys.argv[1:]
assert options in ([],['--batch=b'],['--batch=c'])
batch=options[0].split('=')[1] if options else 'a'
assert re.fullmatch(r'\^Test[A-Za-z0-9_()|]+\$',pattern)
out=ROOT/'docs/testing/evidence/dgs-20261016/social-reuse'/name
assert not out.exists();out.mkdir(parents=True)
expected=read(EV/'continuation19/database/identity01.json')['actual']
assert expected['name']==read(WORK/'active-database.json')['database']
assert re.fullmatch(r'birdtie_phone_owned_[0-9a-f]{12}',expected['name'])
sql="SELECT json_build_object('name',datname,'oid',oid,'owner',pg_get_userbyid(datdba),'marker',shobj_description(oid,'pg_database'))::text FROM pg_database WHERE datname='"+expected['name']+"';"
def identity():
 r=subprocess.run(['docker','exec','-i','api-db-1','psql','-X','-qAt','-U','birdtie','-d','postgres'],input=sql.encode(),capture_output=True,check=True,creationflags=subprocess.CREATE_NO_WINDOW)
 return json.loads(r.stdout)
assert identity()==expected
container=json.loads(subprocess.run(['docker','inspect','api-db-1'],capture_output=True,check=True,creationflags=subprocess.CREATE_NO_WINDOW).stdout)[0]
values=dict(v.split('=',1) for v in container['Config']['Env'] if '=' in v)
env=os.environ.copy()
for key in list(env):
 if key.startswith(('BIRDTIE_MODEL_','BIRDTIE_VISUAL_','BIRDTIE_VIDEO_','BIRDTIE_OIDC_','BIRDTIE_AGENT_VISUAL_','BIRDTIE_SYSTEM_PUSH_')) or key in ('BIRDTIE_TENCENT_TOKENHUB_API_KEY','OPENAI_API_KEY'):env.pop(key,None)
env.pop('BIRDTIE_LEGACY_IMPORT_NATIVE_TEST',None)
if pattern == '^TestLegacySocialHistoryNativePrivateOwnerInertInboxAndRepeat$':env['BIRDTIE_LEGACY_IMPORT_NATIVE_TEST']='1'
env.update(BIRDTIE_DATABASE_URL='postgresql://'+quote(values['POSTGRES_USER'],safe='')+':'+quote(values['POSTGRES_PASSWORD'],safe='')+'@127.0.0.1:55432/'+expected['name']+'?sslmode=disable',BIRDTIE_DISPOSABLE_DB='1',BIRDTIE_AGENT_ACTIVITY_JOIN_ENABLED='false')
from urllib.parse import urlparse
assert urlparse(env['BIRDTIE_DATABASE_URL']).hostname in ('127.0.0.1','localhost','::1')
before=api_frame();started=now();command=[r'C:\Program Files\Go\bin\go.exe','test','-mod=readonly','./internal/postgres','-count=1','-json','-run',pattern]
with(out/'stdout.jsonl').open('xb')as stdout,(out/'stderr.log').open('xb')as stderr:
 r=subprocess.run(command,cwd=ROOT/'apps/api',env=env,stdout=stdout,stderr=stderr,creationflags=subprocess.CREATE_NO_WINDOW)
after=api_frame();events=[]
for line in (out/'stdout.jsonl').read_text(encoding='utf-8').splitlines():
 try:events.append(json.loads(line))
 except ValueError:pass
counts={k:sum(e.get('Action')==k and 'Test'in e for e in events) for k in ('pass','fail','skip')}
top=[e['Test'] for e in events if e.get('Action')=='pass' and e.get('Test') and '/'not in e['Test']]
save(out/'receipt.json',dict(startedAtUTC=started,completedAtUTC=now(),command=command,cwd=str(ROOT/'apps/api'),exitCode=r.returncode,events=counts,topLevelPass=top,sourceBefore=before,sourceAfter=after,sourceStable=before==after,parentIdentityBefore=expected,parentIdentityAfter=identity(),testDatabaseMode='Selected tests own and remove independently marked migrated children; parent QA never migrated by tests',credentials='Docker inputs in memory, not emitted',stdoutSHA256=sha(out/'stdout.jsonl'),stderrSHA256=sha(out/'stderr.log')))
print(name,'exit',r.returncode,counts,'top',len(top),'sourceStable',before==after)
if r.returncode or counts['skip']:
 for e in events:
  output=e.get('Output','')
  if any(x in output for x in ['FAIL','error','Error','SKIP','panic']):print(output.strip())
sys.exit(r.returncode or (0 if before==after and counts['skip']==0 and top else 2))

