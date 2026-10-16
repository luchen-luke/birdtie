from pathlib import Path
import base64,hashlib,importlib.util,json,shlex,subprocess,sys
w=Path(__file__).parent;r=Path(r'D:/Project/birdtie')
execute='--execute'in sys.argv
if not execute:print(json.dumps({'status':'PLAN','schema':172,'apiBuild':28,'sourceWrites':0,'expected':{'moments':83,'authors':9,'avatars':6}}));raise SystemExit(0)
spec=importlib.util.spec_from_file_location('transfer_guard',w/'cm_transfer_author_avatars.py');g=importlib.util.module_from_spec(spec);spec.loader.exec_module(g)
_,_,_,_,payload=g.preflight();payload.pop('tarBytes');payload.update(manifestSHA256=g.file_sha(g.PRIVATE/'civu-private-extract-20261015-01/manifest.json'))
remote=r'''
import sys,json,os,re,stat,hashlib,subprocess
from pathlib import Path
p=json.loads(sys.stdin.buffer.read());stage='PREFLIGHT';applied=False;committed=False
folder=Path('/srv/birdtie-data/migration/private-author-avatars-20261016-01');source=Path('/srv/birdtie-data/migration/source.json');manifest=source.with_name('manifest.json')
def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def need(v,code):
 if not v:raise RuntimeError(code)
def cli(args):
 x=subprocess.run(args,env=env,capture_output=True,timeout=150)
 if x.returncode:
  error=x.stderr.decode(errors='replace').strip();raise RuntimeError(error if re.fullmatch(r'legacy_[a-z_]+',error)else'CONTROLLED_AUTHOR_RESTORE_FAILED')
 return json.loads(x.stdout)
def query(q):
 x=subprocess.run(['psql','-X','-qAt','-v','ON_ERROR_STOP=1'],env=sqlenv,input=q.encode(),capture_output=True,timeout=30);need(x.returncode==0,'READONLY_RECONCILIATION_FAILED');return json.loads(x.stdout)
def state():return query("SELECT jsonb_build_object('moments',(SELECT md5(string_agg(row_to_json(m)::text,''ORDER BY m.id))FROM moments m),'profiles',(SELECT md5(string_agg(row_to_json(p)::text,''ORDER BY p.account_id))FROM user_profiles p),'places',(SELECT md5(string_agg(row_to_json(p)::text,''ORDER BY p.id))FROM places p),'originalRuns',(SELECT count(*)FROM legacy_import_runs),'anchorFacts',(SELECT count(*)FROM legacy_map_anchor_facts))")
try:
 settings=json.loads(Path('/etc/birdtie/deployment.json').read_bytes());need(settings['database']=='birdtie_native_hz_20261015'and settings['oid']==54087 and settings['owner']=='birdtie_native_hz'and settings['release']=='/opt/birdtie/releases/social172-20261015-build28','EXACT_RELEASE_REQUIRED')
 for path in [folder,folder/'transfer-complete.json',source,manifest,*[folder/n for n in p['files']]]:
  s=os.lstat(path);need(s.st_uid==0 and not stat.S_ISLNK(s.st_mode)and stat.S_IMODE(s.st_mode)==(0o700 if path==folder else 0o600),'PROTECTED_FILES_REQUIRED')
 marker=json.loads((folder/'transfer-complete.json').read_bytes());need(marker['protocol']=='birdtie-author-avatar-transfer-v1'and marker['filesVerified']and marker['sourceInputsUnchanged'],'TRANSFER_ACCEPTANCE_REQUIRED')
 need(all(marker[k]==v for k,v in p.items()if k!='manifestSHA256'),'EXACT_TRANSFER_BINDING');need({v.name for v in folder.iterdir()}==set(p['files'])|{'transfer-complete.json'},'EXACT_FILE_SET')
 for name,value in p['files'].items():need((folder/name).stat().st_size==value['bytes']and sha(folder/name)==value['sha256'],'EXACT_AVATAR_INPUT')
 need(sha(source)==p['sourceFileSHA256']and sha(manifest)==p['manifestSHA256'],'ORIGINAL_SOURCE_CHANGED')
 release=Path(settings['release']);binary=release/'bin/birdtie-legacy-moment-author-restore';release_manifest=json.loads((release/'manifest.json').read_bytes());need(sha(binary)==release_manifest['bin/birdtie-legacy-moment-author-restore']['sha256'],'BUILT_IMPORTER_REQUIRED')
 env={k:v for k,v in os.environ.items()if not k.startswith(('PG','BIRDTIE_'))};env.update(BIRDTIE_DATABASE_URL='postgresql://'+settings['owner']+':'+settings['password']+'@127.0.0.1:5432/'+settings['database']+'?sslmode=disable',BIRDTIE_REPOSITORY_ROOT='/opt/birdtie',GOMEMLIMIT='512MiB')
 sqlenv=dict(env,PGHOST='127.0.0.1',PGPORT='5432',PGDATABASE=settings['database'],PGUSER=settings['owner'],PGPASSWORD=settings['password'])
 need(query("SELECT jsonb_build_object('oid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),'schema',(SELECT max(version)FROM birdtie_schema_migrations),'contract',birdtie_legacy_moment_author_contract())")=={'oid':54087,'schema':172,'contract':'legacy-moment-author-v1'},'EXACT172_TARGET_REQUIRED')
 before=state();runid='c5df0d7c-f361-4275-9838-feb6d1d92352';base=[str(binary)];target=['--database',settings['database'],'--database-oid',str(settings['oid']),'--database-owner',settings['owner'],'--database-marker',settings['marker'],'--source-database-oid',str(settings['sourceOID'])]
 inputs=['--run-id',runid,'--source',str(source),'--manifest',str(manifest),'--matched',str(folder/'matched.private.json'),'--seal',str(folder/'authenticated-seal.json'),'--seal-sha256',p['matcherSealSHA256'],'--blob-root',str(folder)]
 expected={'moments':83,'authors':9,'avatars':6}
 stage='PREPARE';prepared=cli(base+inputs+['--validate']);need(prepared['status']=='PREPARED_NOT_IMPORTED'and prepared['counts']==expected,'PREPARE_COUNTS')
 stage='APPLY';applied=True;result=cli(base+inputs+target+['--apply']);need(result['status']=='COMMITTED'and result['counts']==expected,'COMMIT_COUNTS');committed=True
 stage='INSPECT';need(cli(base+target+['--inspect-run',runid])==result,'POSTCOMMIT_INSPECTION')
 stage='IDEMPOTENT_REPEAT';need(cli(base+inputs+target+['--apply'])==result,'IDEMPOTENT_REPLAY')
 stage='RECONCILIATION';need(state()==before,'ORIGINAL_NATIVE_CONTENT_CHANGED');need(sha(source)==p['sourceFileSHA256']and sha(manifest)==p['manifestSHA256'],'SOURCE_CHANGED')
 aggregate=query("SELECT jsonb_build_object('moments',(SELECT count(*)FROM legacy_moment_author_facts),'authors',count(*),'avatars',count(*)FILTER(WHERE thumbnail IS NOT NULL),'avatarBytes',sum(octet_length(thumbnail)),'validBytes',bool_and(thumbnail IS NULL OR encode(sha256(thumbnail),'hex')=thumbnail_sha256))FROM legacy_moment_author_snapshots")
 need(aggregate=={'moments':83,'authors':9,'avatars':6,'avatarBytes':32295,'validBytes':True},'CURRENT_NATIVE_AUTHOR_COUNTS')
 print(json.dumps({'status':'COMMITTED_INSPECTED_REPLAYED','schema':172,'apiBuild':28,'counts':result['counts'],'deferred':result['deferred'],'actual':aggregate,'nativeMomentsPlacesProfilesUnchanged':True,'sourceInputsUnchanged':True,'sourceWrites':0,'originalRunReused':True,'sessionsCreated':0,'oldAvatarURLsFetched':0}))
except Exception as e:
 print(json.dumps({'status':'COMMITTED_REQUIRES_INSPECTION'if committed else'UNKNOWN_REQUIRES_INSPECTION'if applied else'FAILED_NOT_APPLIED','stage':stage,'errorType':type(e).__name__,'code':str(e)if isinstance(e,RuntimeError)else'CONTROLLED_AUTHOR_APPLY_FAILED','privateValuesOmitted':True}));raise SystemExit(1)
'''
spec=importlib.util.spec_from_file_location('deployment',r/'scripts/deploy_birdtie_hangzhou.py');d=importlib.util.module_from_spec(spec);spec.loader.exec_module(d)
out=r/'docs/testing/evidence/content-map-refinement-20261016/backend-build28/apply01';out.mkdir(exist_ok=False)
bootstrap="import base64;exec(base64.b64decode('"+base64.b64encode(remote.encode()).decode()+"'))"
v=subprocess.run(d.SSH+['sudo -n python3 -c '+shlex.quote(bootstrap)],input=json.dumps(payload).encode(),capture_output=True,timeout=360,creationflags=subprocess.CREATE_NO_WINDOW)
receipt={'exitCode':v.returncode,'remoteProgramSHA256':hashlib.sha256(remote.encode()).hexdigest(),'result':json.loads(v.stdout)if v.stdout else None,'stderrPresent':bool(v.stderr)}
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');(out/'actual-runner.py').write_text(Path(__file__).read_text());(out/'remote.py').write_text(remote);print(json.dumps(receipt));raise SystemExit(v.returncode)
