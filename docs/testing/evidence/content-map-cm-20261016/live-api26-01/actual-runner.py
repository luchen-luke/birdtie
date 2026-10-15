"""Read-only API26/schema171 acceptance; default PLAN never connects."""
import argparse,ast,hashlib,importlib.util,json,re,statistics,subprocess
from datetime import datetime,timezone
from pathlib import Path

W=Path(__file__).parent;R=Path('D:/Project/birdtie')
BODY=r'''
owner=None;token=None;hashes=None;last_paths=[];stage='PREFLIGHT171'
result.update(status='FAIL',schema=171,apiBuild=26,contentWrites=0,sourceWrites=0,databaseDirectWrites=0)
def measured_get(path,token):
 parsed=urllib.parse.urlsplit(path)
 require(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.path=='/v1/me/map-content','EXACT_MAP_PATH')
 started=time.monotonic()
 with opener.open(urllib.request.Request(BASE+path,headers={'Authorization':'Bearer '+token}),timeout=30)as response:
  raw=response.read(4*1024**2+1);require(response.status==200 and len(raw)<=4*1024**2,'BOUNDED_MAP_RESPONSE')
  require('no-store'in response.headers.get('Cache-Control',''),'MAP_NO_STORE')
  payload=json.loads(raw);view=payload['data']
 elapsed=round((time.monotonic()-started)*1000,3)
 require(view['schemaVersion']=='map-content-v2'and view['scope']=='MAP_CONTENT'and view['viewerAccountId']==owner and view['filterScope']=='DEFAULT_CONTENT','CURRENT_CONTENT_MAP')
 pins=view['pins'];require(isinstance(pins,list)and len(pins)<=40 and not view['truncated'],'MARKER_BUDGET')
 require(len({p['markerId']for p in pins})==len(pins),'UNIQUE_MARKERS')
 require(view['totalAnchorCount']>=len(pins)and view['totalContentCount']>=0,'COMPLETE_TOTALS')
 require(all(p['momentCount']>0 and p['kind']in('PLACE_CONTENT','MOMENT_ANCHOR','CLUSTER')for p in pins),'NO_EMPTY_POI')
 covers=[p['cover']for p in pins if p['cover']is not None];require(len(covers)<=12,'COVER_BUDGET')
 for cover in covers:
  url=urllib.parse.urlsplit(cover['contentPath']);args=urllib.parse.parse_qs(url.query)
  require(not url.scheme and not url.netloc and url.path.startswith('/v1/me/map-content/')and args.get('variant')==['marker'],'MARKER_VARIANT')
  require(cover['mimeType']=='image/jpeg'and 0<cover['width']<=160 and 0<cover['height']<=160 and 0<cover['byteCount']<=262144,'REAL_SMALL_THUMBNAIL')
 measurement={'elapsedMs':elapsed,'responseJSONBytes':len(json.dumps(view,ensure_ascii=False,separators=(',',':')).encode()),'httpResponseBytes':len(raw),'markers':len(pins),'withContent':sum(p['momentCount']>0 for p in pins),'totalContentCount':view['totalContentCount'],'totalAnchorCount':view['totalAnchorCount'],'covers':len(covers),'advertisedCoverBytes':sum(c['byteCount']for c in covers),'kindCounts':{k:sum(p['kind']==k for p in pins)for k in('PLACE_CONTENT','MOMENT_ANCHOR','CLUSTER')}}
 return view,measurement
def page_path(path,cursor=None,selected=None):
 u=urllib.parse.urlsplit(path);require(not u.scheme and not u.netloc and not u.fragment and u.path=='/v1/me/map-content','EXACT_MEMBERS_ORIGIN')
 args=urllib.parse.parse_qs(u.query);require(all(len(v)==1 for v in args.values())and set(args)<=set(('cityId','west','south','east','north','zoom','filterScope','clusterId','cursor','sourceVersion','selectedMarkerId')),'CLOSED_MEMBERS_QUERY')
 if cursor:args['cursor']=[cursor]
 if selected:args['selectedMarkerId']=[selected]
 return u.path+'?'+urllib.parse.urlencode({k:v[0]for k,v in args.items()})
try:
 owner,source_owner,phone,hashes=claimed_owner_phone()
 deployment=json.loads(pathlib.Path('/etc/birdtie/deployment.json').read_bytes())
 require(deployment['oid']==54087 and deployment['release']=='/opt/birdtie/releases/social171-20261015-build26','EXACT_API26_RELEASE')
 require(query("SELECT to_jsonb(birdtie_content_map_contract()='map-content-v2')"),'CONTENT_MAP_V2_CONTRACT')
 ledger=query("SELECT coalesce(jsonb_agg(jsonb_build_object('anchor',id,'moment',moment_id,'media',media_id,'precision',precision)),'[]')FROM legacy_map_anchor_facts WHERE run_id='"+RUN+"'")
 require(len(ledger)==16 and sum(x['media']is not None for x in ledger)==15,'EXACT_SIXTEEN_MIGRATED_ANCHORS')
 targets={x['anchor']:x for x in ledger}
 stage='CURRENT_OWNER_LOGIN';token=login(phone);del phone
 require(data('GET','/v1/me',token)['id']==owner,'EXACT_OWNER_BOUNDARY')
 stage='MATCHED_VIEWPORT_MEASUREMENTS'
 measurements=[]
 for label,w,s,e,n in [('wide',-3,56,-1,58),('phone',-2.15,57.12,-2.06,57.19),('adjacent',-2.16,57.12,-2.07,57.19)]:
  path='/v1/me/map-content?'+urllib.parse.urlencode({'cityId':'aberdeen-gb','west':w,'south':s,'east':e,'north':n,'zoom':12})
  runs=[]
  for attempt in range(3):
   view,measurement=measured_get(path,token);require(view['zoom']==12,'EXACT_COMPARISON_ZOOM');runs.append(measurement)
  measurements.append({'scenario':label,'runs':runs})
 result['measurements']=measurements
 stage='WORLD_CLUSTER_AND_ANCHOR'
 world_path='/v1/me/map-content?'+urllib.parse.urlencode({'west':-180,'south':-85,'east':180,'north':85,'zoom':0})
 world,world_measurement=measured_get(world_path,token)
 require(world['totalContentCount']>0 and world['totalAnchorCount']>=16,'WORLD_TOTALS')
 observed=set();selected=None;selected_path=None;member_pages=0
 clusters=[p for p in world['pins']if p['kind']=='CLUSTER'];require(clusters,'WORLD_CLUSTER_REQUIRED')
 for cluster in clusters:
  members=page_path(cluster['membersPath']);args=urllib.parse.parse_qs(urllib.parse.urlsplit(members).query)
  require(args.get('clusterId')==[cluster['markerId']]and args.get('sourceVersion')==[world['sourceVersion']],'MEMBERS_SAME_SOURCE')
  cursor=None;seen=set()
  for attempt in range(12):
   current=page_path(members,cursor);page,metrics=measured_get(current,token);member_pages+=1
   require(page['sourceVersion']==world['sourceVersion']and page['totalContentCount']==world['totalContentCount']and page['totalAnchorCount']==world['totalAnchorCount'],'MEMBER_TOTALS_NOT_CLUSTER_COUNTS')
   require(all(p['kind']!='CLUSTER'for p in page['pins']),'MEMBERS_ARE_LEAVES')
   for pin in page['pins']:
    require(pin['markerId']not in seen,'NO_DUPLICATE_MEMBERS');seen.add(pin['markerId'])
    if pin['kind']=='MOMENT_ANCHOR'and pin.get('anchorId')in targets:
     observed.add(pin['anchorId']);target=targets[pin['anchorId']]
     require(pin['placeId']==''and pin['momentId']==target['moment']and pin['entryId']==target['moment']and pin['entityId']==target['moment'],'CORRECT_MIGRATED_ENTRY')
     if selected is None and target['media']is not None:selected=pin;selected_path=current
   if selected is not None:break
   cursor=page.get('nextCursor')
   if not cursor:break
  if selected is not None:break
 require(selected is not None and member_pages>0,'MIGRATED_MEDIA_ANCHOR_ACCESSIBLE')
 if selected['cover']is None:
  page,_=measured_get(page_path(selected_path,selected=selected['markerId']),token)
  selected=next(p for p in page['pins']if p['markerId']==selected['markerId'])
 stage='EXACT_ANCHOR_ENTRY_AND_MARKER'
 target=targets[selected['anchorId']];cover=selected['cover']
 require(cover is not None and cover['id']==target['media'],'ANCHOR_SPECIFIC_MEDIA')
 detail_path='/v1/me/map-content/moments/'+selected['momentId'];detail=data('GET',detail_path,token)
 require(detail['id']==selected['entryId']and detail['viewerAccountId']==owner and any(m['id']==cover['id']for m in detail['media']),'CORRECT_ENTRY_AND_MEDIA')
 marker_bytes=content_read(cover,token)
 last_paths=[world_path,detail_path,cover['contentPath']]
 require(all(call('GET',path)[0]in(401,403)for path in last_paths),'ANONYMOUS_MAP_ENTRY_MARKER_DENIED')
 result.update(world=world_measurement,worldClusterMarkers=len(clusters),clusterMemberPagesRead=member_pages,migratedAnchorsObserved=len(observed),migratedMediaAnchorsVerified=1,exactEntryVerified=True,anchorSpecificMediaVerified=True,markerMediaSHA256Verified=True,markerBytesVerified=marker_bytes,anonymousMapEntryMarkerDenied=True)
 require((hashlib.sha256(SOURCE.read_bytes()).hexdigest(),hashlib.sha256(MANIFEST.read_bytes()).hexdigest())==hashes,'PROTECTED_SOURCE_UNCHANGED')
 result.update(status='PASS',protectedSourceUnchanged=True)
except Exception as e:
 result.update(status='FAIL',stage=stage,errorType=type(e).__name__,errorCode=str(e)if isinstance(e,RuntimeError)else'CONTROLLED_CONTENT_MAP_ACCEPTANCE_FAILED')
finally:
 closed=True
 for current_token in tokens:
  ok=False
  for attempt in range(3):
   try:ok=call('POST','/v1/session/logout',current_token)[0]in(200,204)
   except Exception:ok=False
   if ok:break
   time.sleep(1)
  closed=closed and ok
 result['allCreatedSessionsLoggedOut']=closed
 denied=False
 if closed and tokens and last_paths:
  try:denied=all(call('GET',path,tokens[-1])[0]in(401,403)for path in last_paths)
  except Exception:denied=False
 result['loggedOutMapEntryMarkerDenied']=denied
 if not closed or not denied:result['status']='FAIL'
 print(json.dumps(result));raise SystemExit(0 if result['status']=='PASS'else 1)
'''

def build():
 spec=importlib.util.spec_from_file_location('prior',W/'verify_reference167.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
 transport,prefix=m.build_parts();assert "target['schema']==167"in prefix
 prefix=prefix.replace("target['schema']==167","target['schema']==171")
 helpers=(W/'verify_map170.remote.py').read_text().split("stage='PREFLIGHT'")[0]
 remote=prefix+'\n'+helpers+'\n'+BODY;ast.parse(remote)
 return m,transport,remote
def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--execute',action='store_true');parser.add_argument('--name',default='live-api26-01');args=parser.parse_args()
 require_name=re.fullmatch('live-api26-[a-z0-9-]{1,40}',args.name);assert require_name
 m,transport,remote=build();digest=hashlib.sha256(remote.encode()).hexdigest()
 if not args.execute:
  print(json.dumps({'status':'PLAN_NOT_EXECUTED','schema':171,'apiBuild':26,'sameViewports':3,'readsEach':3,'zoom':12,'worldClusterPagination':True,'exactMigratedEntryAndMarker':True,'anonymousAndLogoutChecks':True,'contentWrites':0,'databaseDirectWrites':0,'sourceWrites':0,'remoteScriptSHA256':digest}));return 0
 out=R/'docs/testing/evidence/content-map-cm-20261016'/args.name;out.mkdir(parents=True,exist_ok=False)
 receipt={'status':'FAIL','startedAtUTC':datetime.now(timezone.utc).isoformat(),'remoteScriptSHA256':digest,'privateDataLogged':False}
 try:
  run=subprocess.run(transport,input=remote.encode(),capture_output=True,timeout=300,creationflags=subprocess.CREATE_NO_WINDOW)
  receipt.update(exitCode=run.returncode,stderrPresent=bool(run.stderr))
  value=json.loads(run.stdout);assert m.aggregate_safe(value)
  receipt.update(result=value,status='PASS'if run.returncode==0 and value.get('status')=='PASS'else'FAIL')
 except Exception as e:receipt.update(failureType=type(e).__name__,failureCode='CONTROLLED_READER_EXECUTION_FAILED')
 receipt['completedAtUTC']=datetime.now(timezone.utc).isoformat();(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
 (out/'actual-runner.py').write_text(Path(__file__).read_text())
 if receipt['status']=='PASS':
  baseline=R/'docs/testing/evidence/content-map-cm-20261016/baseline-api23-02/receipt.json'
  old={v['scenario']:v['runs']for v in json.loads(baseline.read_bytes())['result']['measurements']}
  comparisons=[]
  for measurement in receipt['result']['measurements']:
   prior=old[measurement['scenario']];current=measurement['runs']
   comparisons.append({'scenario':measurement['scenario'],'beforeMedianMs':statistics.median(x['elapsedMs']for x in prior),'afterMedianMs':statistics.median(x['elapsedMs']for x in current),'beforeMedianJSONBytes':statistics.median(x['responseJSONBytes']for x in prior),'afterMedianJSONBytes':statistics.median(x['responseJSONBytes']for x in current),'beforeMarkers':prior[0]['markers'],'afterMarkers':current[0]['markers'],'afterTotalContentCount':current[0]['totalContentCount'],'afterTotalAnchorCount':current[0]['totalAnchorCount'],'beforeAdvertisedCoverBytes':prior[0]['advertisedCoverBytes'],'afterAdvertisedCoverBytes':current[0]['advertisedCoverBytes']})
  (out/'comparison.json').write_text(json.dumps({'status':'MEASURED_SINGLE_SESSION_COMPARISON','beforeAPI':23,'afterAPI':26,'schemaBefore':170,'schemaAfter':171,'viewportsMatch':True,'runsPerViewport':3,'metrics':comparisons,'qualification':'Same HTTPS origin and3viewport bounds; API23 had no zoom parameter, API26 requests zoom12. Counts distinguish marker clusters from full content/anchor totals. AdvertisedCoverBytes measures declared payload budget, not all covers downloaded. This is API evidence, not device FPS/memory evidence.'},indent=2)+'\n')
 print(json.dumps(receipt));return 0 if receipt['status']=='PASS'else 1
if __name__=='__main__':raise SystemExit(main())
