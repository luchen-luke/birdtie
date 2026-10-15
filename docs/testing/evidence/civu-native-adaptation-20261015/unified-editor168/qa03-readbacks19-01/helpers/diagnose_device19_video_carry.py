"""Exact synthetic QA03/video metadata only; no media output or database write."""
import hashlib, importlib.util, json, subprocess
from pathlib import Path
WORK=Path(__file__).resolve().parent
ROOT=Path(r'D:\Project\birdtie')
REMOTE=r'''
import hashlib,json,os,subprocess
from pathlib import Path
s=json.loads(Path('/etc/birdtie/deployment.json').read_bytes())
assert s['database']=='birdtie_native_hz_20261015' and s['oid']==54087 and s['owner']=='birdtie_native_hz'
env=os.environ.copy();env.update(PGHOST='127.0.0.1',PGPORT='5432',PGDATABASE=s['database'],PGUSER=s['owner'],PGPASSWORD=s['password'])
phone=hashlib.sha256(b'birdtie-dev-phone:+447700900003').hexdigest()
query="""BEGIN READ ONLY;SET LOCAL statement_timeout='5000ms';
WITH actor AS(SELECT a.id FROM account_auth_identities ai JOIN accounts a ON a.id=ai.account_id WHERE ai.issuer='urn:birdtie:local-dev-phone' AND ai.subject='%s' AND a.account_type='person' AND a.status='active'),
selected AS(SELECT m.id,m.revision,m.status,m.author_account_id FROM moments m JOIN actor a ON a.id=m.author_account_id JOIN moment_thread_members tm ON tm.moment_id=m.id WHERE m.id='4ef5b105-9152-4a8c-98e0-b445b1153ef8' AND tm.thread_id='8ca660be-f15f-4bad-acc9-87d9a1f25036'),
attachment AS(SELECT a.* FROM selected m JOIN public_moment_media_sets s ON s.moment_id=m.id AND s.owner_account_id=m.author_account_id AND s.status='READY' JOIN public_moment_media_attachments a ON a.moment_id=s.moment_id AND a.set_version=s.version WHERE a.metadata->>'kind'='VIDEO'),
original_fact AS(SELECT o.receipt FROM moment_media_prepare_operations o JOIN actor p ON p.id=o.actor_account_id JOIN attachment a ON o.receipt->'material'->>'mediaId'=a.media_id::text WHERE o.receipt->>'status'='SUCCEEDED'),
frame AS(SELECT clock_timestamp() now,'2026-10-15T17:16:37.739Z'::timestamptz tap)
SELECT jsonb_build_object(
'databaseGuard',current_database()='birdtie_native_hz_20261015' AND(SELECT oid=54087 AND pg_get_userbyid(datdba)='birdtie_native_hz' FROM pg_database WHERE datname=current_database()),
'schema',(SELECT max(version) FROM birdtie_schema_migrations),'actorCount',(SELECT count(*) FROM actor),
'actorNotLegacy',NOT EXISTS(SELECT 1 FROM actor a JOIN legacy_entity_mappings l ON l.entity_kind='user' AND l.target_id=a.id),
'exactMomentCount',(SELECT count(*) FROM selected),'momentRevision',(SELECT revision FROM selected),'momentStatus',(SELECT status FROM selected),
'currentVideoAttachmentCount',(SELECT count(*) FROM attachment),'currentPublicVideoBytesPresent',EXISTS(SELECT 1 FROM public_moment_media_video_payloads v JOIN attachment a ON a.moment_id=v.moment_id AND a.set_version=v.set_version AND a.position=v.position WHERE v.content IS NOT NULL),
'currentPublicPosterAndThumbnailPresent',EXISTS(SELECT 1 FROM attachment WHERE display IS NOT NULL AND thumbnail IS NOT NULL),
'sourceStageExists',EXISTS(SELECT 1 FROM attachment a JOIN moment_media_materials m ON m.id=a.media_id),
'sourceCanonicalStageExists',EXISTS(SELECT 1 FROM attachment a JOIN moment_media_video_materials v ON v.media_id=a.media_id),
'sourceStagePreparedAt',(SELECT m.prepared_at FROM attachment a JOIN moment_media_materials m ON m.id=a.media_id),
'sourceStageRetainUntil',(SELECT m.retain_until FROM attachment a JOIN moment_media_materials m ON m.id=a.media_id),
'sourceStageExpiredAtConfirm',coalesce((SELECT m.retain_until<=f.tap FROM attachment a JOIN moment_media_materials m ON m.id=a.media_id CROSS JOIN frame f),false),
'sourceStageExpiredNow',coalesce((SELECT m.retain_until<=f.now FROM attachment a JOIN moment_media_materials m ON m.id=a.media_id CROSS JOIN frame f),false),
'sourceStageGuardQualifiesAtConfirm',EXISTS(SELECT 1 FROM attachment a JOIN moment_media_materials m ON m.id=a.media_id JOIN moment_media_video_materials v ON v.media_id=m.id JOIN selected p ON p.id=a.moment_id CROSS JOIN frame f WHERE m.owner_account_id=p.author_account_id AND m.version=a.media_version AND m.retain_until>f.tap),
'immutablePrepareReceiptCount',(SELECT count(*) FROM original_fact),
'immutablePreparePreparedAt',(SELECT receipt->'material'->>'preparedAt' FROM original_fact),
'immutablePrepareRetainUntil',(SELECT receipt->'material'->>'retainUntil' FROM original_fact),
'deployedTriggerHasExactCanonicalError',strpos(pg_get_functiondef('birdtie_guard_public_video_payload()'::regprocedure),'exact approved canonical video copy required')>0,
'deployedTriggerRequiresUnexpiredStage',strpos(pg_get_functiondef('birdtie_guard_public_video_payload()'::regprocedure),'m.retain_until>clock_timestamp()')>0,
'confirmTapUTC',(SELECT tap FROM frame),'observedAtUTC',(SELECT now FROM frame));ROLLBACK;"""%phone
p=subprocess.run(['psql','-X','-qAt','-v','ON_ERROR_STOP=1'],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env,timeout=12)
if p.returncode:raise RuntimeError('READ_ONLY_SQL_METADATA_FAILED')
v=json.loads(p.stdout)
assert v['databaseGuard'] and v['schema']==168 and v['actorCount']==v['exactMomentCount']==v['currentVideoAttachmentCount']==1 and v['actorNotLegacy']
print(json.dumps({'status':'READ_ONLY_EXACT_VIDEO_METADATA','observed':v,'databaseWrites':0,'sourceWrites':0,'mediaBytesOutput':0,'otherOwnersSelected':0}))
'''
if __name__=='__main__':
 spec=importlib.util.spec_from_file_location('deployment',ROOT/'scripts/deploy_birdtie_hangzhou.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
 out=WORK/'device19-video-carry-diagnostic01';out.mkdir(exist_ok=False)
 p=subprocess.run(m.SSH+['sudo -n python3 -'],input=REMOTE.encode(),capture_output=True,timeout=45,creationflags=subprocess.CREATE_NO_WINDOW)
 v={'exitCode':p.returncode,'remoteScriptSHA256':hashlib.sha256(REMOTE.encode()).hexdigest(),'helperSHA256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'stderrPresent':bool(p.stderr),'status':'FAIL'}
 if p.returncode==0:v.update(status='READ_ONLY_COMPLETE',result=json.loads(p.stdout))
 (out/'receipt.json').write_text(json.dumps(v,indent=2)+'\n',encoding='utf-8');print(json.dumps(v))
