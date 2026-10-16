from pathlib import Path
import importlib.util,json,hashlib,ast,subprocess,argparse,statistics
from datetime import datetime,timezone
w=Path(__file__).parent;r=Path(r'D:\Project\birdtie')
p=argparse.ArgumentParser();p.add_argument('--execute',action='store_true');p.add_argument('--name',default='verify01');a=p.parse_args();assert a.name.replace('-','').isalnum()
spec=importlib.util.spec_from_file_location('prior',w/'verify_reference167.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
transport,prefix=m.build_parts();assert "target['schema']==167"in prefix;prefix=prefix.replace("target['schema']==167","target['schema']==171")
helpers=(w/'verify_map170.remote.py').read_text().split("stage='PREFLIGHT'")[0]
remote=prefix+'\n'+helpers+'\n'+(w/'cm_refinement_live_helpers.py').read_text()+'\n'+(w/'cm_refinement27.remote.py').read_text(encoding='utf-8-sig')
ast.parse(remote)
if not a.execute:print(json.dumps({'status':'PLAN','apiBuild':27,'schema':171,'writes':'LOGIN_LOGOUT_ONLY','remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest()}));raise SystemExit(0)
out=r/'docs/testing/evidence/content-map-refinement-20261016/live-api27'/a.name;out.mkdir(parents=True,exist_ok=False)
receipt={'status':'FAIL','at':datetime.now(timezone.utc).isoformat(),'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'privateValuesLogged':False}
try:
 run=subprocess.run(transport,input=remote.encode(),capture_output=True,timeout=300,creationflags=subprocess.CREATE_NO_WINDOW)
 receipt.update(exitCode=run.returncode,stderrPresent=bool(run.stderr))
 value=json.loads(run.stdout);assert m.aggregate_safe(value)
 receipt.update(result=value,status='PASS'if run.returncode==0 and value.get('status')=='PASS'else'FAIL')
except Exception as e:receipt.update(errorType=type(e).__name__,errorCode='CONTROLLED_REMOTE_READER_FAILURE')
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');(out/'actual-runner.py').write_text(Path(__file__).read_text(encoding='utf-8-sig'));(out/'remote.py').write_text(remote)
if receipt['status']=='PASS':
 old=json.loads((r/'docs/testing/evidence/content-map-cm-20261016/live-api26-01/receipt.json').read_text())['result']['measurements'];old={v['scenario']:v['runs']for v in old}
 metrics=[]
 for v in receipt['result']['measurements']:
  before,after=old[v['scenario']],v['runs']
  metrics.append({'scenario':v['scenario'],'beforeAPI':26,'afterAPI':27,'beforeMedianMs':statistics.median(x['elapsedMs']for x in before),'afterMedianMs':statistics.median(x['elapsedMs']for x in after),'beforeJSONBytes':statistics.median(x['responseJSONBytes']for x in before),'afterJSONBytes':statistics.median(x['responseJSONBytes']for x in after),'beforeMarkers':before[0]['markers'],'afterMarkers':after[0]['markers'],'afterContentCount':after[0]['totalContentCount'],'afterAnchorCount':after[0]['totalAnchorCount'],'afterPointCount':after[0]['totalPointCount'],'afterCovers':after[0]['covers'],'afterAdvertisedCoverBytes':after[0]['advertisedCoverBytes']})
 (out/'comparison.json').write_text(json.dumps({'status':'SAME_BOUNDS_ZOOM12_THREE_READS','metrics':metrics,'qualification':'Single-session API timings, not device FPS. API26 merged distinct points; API27 returns actual individual points and bounded cover metadata. Extra reads verify low/high zoom and tight near viewport.'},indent=2)+'\n')
print(json.dumps(receipt));raise SystemExit(0 if receipt['status']=='PASS'else 1)
