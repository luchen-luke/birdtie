import ast,datetime,hashlib,importlib.util,json,pathlib,subprocess
W=pathlib.Path(__file__).parent
E=pathlib.Path('D:/Project/birdtie/docs/testing/evidence')
N=E/'now-civu-map-20261016'
S=E/'social-remote-migration-20261015'
OUT=W/'map-live170-02-session-audit01'
OUT.mkdir(exist_ok=False)

def stamp(v):return datetime.datetime.fromtimestamp(v,datetime.timezone.utc).isoformat()
def boundaries(path):
 stat=path.stat();r=path/'receipt.json'
 return {'directoryCreatedUTC':stamp(stat.st_birthtime if hasattr(stat,'st_birthtime')else stat.st_ctime),'receiptWrittenUTC':stamp(r.stat().st_mtime)}

b={name:boundaries(path)for name,path in [
 ('mapLive01',N/'map-live170-01'),('mapLive02',N/'map-live170-02'),('mapLive03',N/'map-live170-03'),
 ('api22Upgrade170_02',S/'upgrade170-02'),('web21GatewaySwitch',S/'public-client21')]}
window=b['mapLive02']
spec=importlib.util.spec_from_file_location('prior',W/'verify_reference167.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
transport,prefix=m.build_parts()
expected=ast.literal_eval(next(n.value for n in ast.parse(prefix).body if isinstance(n,ast.Assign)and getattr(n.targets[0],'id','')=='EXPECTED_OWNER_SHA'))
remote='import json,hashlib,subprocess\nDB="birdtie_native_hz_20261015"\nEXPECTED='+repr(expected)+'\nWINDOW='+repr(window)+'\n'+r'''
def query(sql):
 p=subprocess.run(['runuser','-u','postgres','--','psql','-X','-A','-t','-q','-v','ON_ERROR_STOP=1','-d',DB],input="BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;SET LOCAL statement_timeout='5000ms';"+sql+";ROLLBACK;",capture_output=True,text=True,timeout=8)
 assert p.returncode==0,'CONTROLLED_READONLY_QUERY_FAILED'
 return json.loads(p.stdout.strip())
g=query("SELECT jsonb_build_object('valid',current_database()='birdtie_native_hz_20261015'AND(SELECT oid=54087 AND pg_get_userbyid(datdba)='birdtie_native_hz'FROM pg_database WHERE datname=current_database())AND(SELECT max(version)=170 FROM birdtie_schema_migrations),'owners',(SELECT jsonb_agg(owner_account_id)FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL))")
assert g['valid']and len(g['owners'])==1,'EXACT_TARGET_OWNER_REQUIRED'
owner=g['owners'][0];assert hashlib.sha256(owner.encode()).hexdigest()==EXPECTED,'ORIGINAL_CLAIMED_OWNER_CHANGED'
start=WINDOW['directoryCreatedUTC'];end=WINDOW['receiptWrittenUTC']
q="""WITH cl AS(SELECT clock_timestamp() at),selected AS(
 SELECT s.created_at,s.revoked_at,s.expires_at,s.idle_expires_at,s.authentication_method,s.authorization_generation,
 s.revoked_at IS NULL AND s.expires_at>cl.at AND s.idle_expires_at>cl.at current_valid
 FROM sessions s CROSS JOIN cl WHERE s.account_id='%s'::uuid AND s.created_at>='%s'::timestamptz AND s.created_at<='%s'::timestamptz
)SELECT jsonb_build_object('sessionsCreatedInWindow',count(*),'notRevokedCount',count(*)FILTER(WHERE revoked_at IS NULL),'currentlyValidCount',count(*)FILTER(WHERE current_valid),
 'rows',coalesce(jsonb_agg(jsonb_build_object('createdAt',created_at,'revoked',revoked_at IS NOT NULL,'currentlyValid',current_valid,'expiresAt',expires_at,'idleExpiresAt',idle_expires_at,'authenticationMethod',authentication_method,'authorizationGeneration',authorization_generation)ORDER BY created_at),'[]'))FROM selected"""%(owner,start,end)
v=query(q)
print(json.dumps({'status':'PASS','databaseGuard':True,'sameOriginalClaimedOwnerGuard':True,'schema':170,'window':WINDOW,'result':v,'readOnly':True,'databaseWrites':0,'httpCalls':0,'newLogins':0,'sessionIDsOrOwnerIDsPersisted':False,'tokensRead':0,'phoneSessionsChanged':0}))
'''
ast.parse(remote)
r=subprocess.run(transport,input=remote.encode(),capture_output=True,timeout=25,creationflags=subprocess.CREATE_NO_WINDOW)
receipt={'status':'FAIL','exitCode':r.returncode,'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'stderrPresent':bool(r.stderr),'localEvidenceBoundaries':b}
if r.stdout:
 v=json.loads(r.stdout)
 assert not __import__('re').search('[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}',json.dumps(v)),'PRIVATE_IDENTIFIERS_REJECTED'
 receipt.update(status=v['status'],audit=v)
(OUT/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
print(json.dumps(receipt))
raise SystemExit(r.returncode)
