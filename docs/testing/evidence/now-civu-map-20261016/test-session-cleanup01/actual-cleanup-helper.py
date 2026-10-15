import argparse,ast,hashlib,importlib.util,json,pathlib,shutil,subprocess
W=pathlib.Path(__file__).parent
N=pathlib.Path('D:/Project/birdtie/docs/testing/evidence/now-civu-map-20261016')
args=argparse.ArgumentParser();args.add_argument('--execute',action='store_true');option=args.parse_args()
if not option.execute:
 print(json.dumps({'status':'PLAN_NOT_EXECUTED','maximumSessionsRevoked':1,'strictSameOwnerExactCreationExpiryMethodGenerationGuard':True}));raise SystemExit(0)
OUT=N/'test-session-cleanup01';OUT.mkdir(exist_ok=False)
spec=importlib.util.spec_from_file_location('prior',W/'verify_reference167.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
transport,prefix=m.build_parts()
expected=ast.literal_eval(next(n.value for n in ast.parse(prefix).body if isinstance(n,ast.Assign)and getattr(n.targets[0],'id','')=='EXPECTED_OWNER_SHA'))
assert len(expected)==64
remote='import json,subprocess\nDB="birdtie_native_hz_20261015"\nEXPECTED='+repr(expected)+'\n'+r'''
def run(sql):
 p=subprocess.run(['runuser','-u','postgres','--','psql','-X','-A','-t','-q','-v','ON_ERROR_STOP=1','-d',DB],input=sql,capture_output=True,text=True,timeout=12)
 if p.returncode:raise RuntimeError('EXACT_SESSION_CLEANUP_TRANSACTION_REJECTED')
 return json.loads(p.stdout.strip())
sql=r"""BEGIN;
SET LOCAL statement_timeout='5000ms';SET LOCAL lock_timeout='2000ms';
DO $guard$
DECLARE owners uuid[];owner uuid;chosen uuid;affected integer;before_state text;after_state text;generation bigint;
BEGIN
 IF current_database()<>'birdtie_native_hz_20261015' OR NOT EXISTS(SELECT 1 FROM pg_database WHERE datname=current_database() AND oid=54087 AND pg_get_userbyid(datdba)='birdtie_native_hz')
 OR(SELECT max(version)FROM birdtie_schema_migrations)<>170 THEN RAISE EXCEPTION 'exact current target required';END IF;
 SELECT array_agg(owner_account_id)INTO owners FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL;
 IF coalesce(array_length(owners,1),0)<>1 THEN RAISE EXCEPTION 'unique first claimed owner required';END IF;
 owner:=owners[1];
 IF encode(sha256(convert_to(owner::text,'UTF8')),'hex')<>'@EXPECTED@' THEN RAISE EXCEPTION 'original first claimed owner pin required';END IF;
 SELECT id INTO STRICT chosen FROM sessions WHERE account_id=owner
 AND created_at='2026-10-15T19:30:22.128489Z'::timestamptz
 AND expires_at='2026-10-16T03:30:22.128489Z'::timestamptz
 AND authentication_method='dev_phone'AND authorization_generation=1 AND revoked_at IS NULL FOR UPDATE;
 SELECT encode(sha256(convert_to(coalesce(jsonb_agg(jsonb_build_array(id,created_at,expires_at,revoked_at,authorization_generation)ORDER BY id),'[]')::text,'UTF8')),'hex')
 INTO before_state FROM sessions WHERE account_id=owner AND id<>chosen;
 UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=chosen AND account_id=owner AND revoked_at IS NULL AND authorization_generation=1;
 GET DIAGNOSTICS affected=ROW_COUNT;
 IF affected<>1 THEN RAISE EXCEPTION 'exact one test session required';END IF;
 SELECT authorization_generation INTO generation FROM sessions WHERE id=chosen AND revoked_at IS NOT NULL;
 IF generation IS DISTINCT FROM 2::bigint THEN RAISE EXCEPTION 'native generation trigger required';END IF;
 SELECT encode(sha256(convert_to(coalesce(jsonb_agg(jsonb_build_array(id,created_at,expires_at,revoked_at,authorization_generation)ORDER BY id),'[]')::text,'UTF8')),'hex')
 INTO after_state FROM sessions WHERE account_id=owner AND id<>chosen;
 IF before_state IS DISTINCT FROM after_state THEN RAISE EXCEPTION 'other session terminal states changed';END IF;
 PERFORM set_config('birdtie.test_session_cleanup_receipt',jsonb_build_object('databaseGuard',true,'firstClaimedOwnerPinMatched',true,'strictUniqueTarget',true,'updatedSessions',affected,'targetRevoked',true,'generationBefore',1,'generationAfter',generation,'otherOwnerSessionTerminalStatesUnchanged',true,'createdAtUTC','2026-10-15T19:30:22.128489Z','expiresAtUTC','2026-10-16T03:30:22.128489Z')::text,true);
END
$guard$;
SELECT current_setting('birdtie.test_session_cleanup_receipt')::jsonb;
COMMIT;
""".replace('@EXPECTED@',EXPECTED)
result=run(sql)
result['transactionCommitted']=True
check=run("""BEGIN READ ONLY;SET LOCAL statement_timeout='5000ms';
SELECT jsonb_build_object('matchingSessionCount',count(*),'revokedCount',count(*)FILTER(WHERE s.revoked_at IS NOT NULL),'generation2Count',count(*)FILTER(WHERE s.authorization_generation=2))
FROM sessions s WHERE s.account_id=(SELECT owner_account_id FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL)
AND s.created_at='2026-10-15T19:30:22.128489Z'::timestamptz AND s.expires_at='2026-10-16T03:30:22.128489Z'::timestamptz AND s.authentication_method='dev_phone';ROLLBACK;""")
assert check=={'matchingSessionCount':1,'revokedCount':1,'generation2Count':1},'COMMITTED_REVOKE_READBACK_UNVERIFIED'
print(json.dumps({'status':'PASS','cleanup':result,'readback':check,'maximumSessionWrites':1,'newLogins':0,'httpCalls':0,'phoneSessionWrites':0,'ownerOrSessionIDsPersisted':False,'tokensReadOrWritten':0}))
'''
ast.parse(remote)
receipt={'status':'UNKNOWN_NOT_RETRIED','remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest()}
try:
 p=subprocess.run(transport,input=remote.encode(),capture_output=True,timeout=30,creationflags=subprocess.CREATE_NO_WINDOW)
 receipt.update(exitCode=p.returncode,stderrPresent=bool(p.stderr))
 if p.stdout:
  value=json.loads(p.stdout)
  assert not __import__('re').search('[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}',json.dumps(value)),'PRIVATE_IDENTIFIER_REJECTED'
  receipt.update(status=value['status'],result=value)
except Exception as e:receipt['controlledErrorType']=type(e).__name__
(OUT/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
shutil.copyfile(W/'map-live170-02-session-audit01/receipt.json',OUT/'audit-before.json')
shutil.copyfile(W/'audit_live170_02_session.py',OUT/'actual-audit-helper.py')
shutil.copyfile(pathlib.Path(__file__),OUT/'actual-cleanup-helper.py')
files={p.name:{'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'bytes':p.stat().st_size}for p in OUT.iterdir()if p.is_file()}
(OUT/'index.json').write_text(json.dumps({'safeAggregateOnly':True,'files':files},indent=2)+'\n',encoding='utf-8')
print(json.dumps(receipt))
raise SystemExit(0 if receipt['status']=='PASS'else 1)
