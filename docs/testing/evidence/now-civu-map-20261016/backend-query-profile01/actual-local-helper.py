import ast, hashlib, importlib.util, json, pathlib, re, subprocess, sys

W=pathlib.Path(__file__).parent
R=pathlib.Path('D:/Project/birdtie/apps/api/internal/postgres')
OUT=W/'map170-query-profile01'
OUT.mkdir(exist_ok=False)

def source(name):return (R/name).read_text(encoding='utf-8')
def function(filename,name):
 s=source(filename);a=s.index('func '+name+'(');b=s.index('\n}',a)+2
 return s[a:b]
def constant(filename,name):
 return re.search(r'const '+name+r' = `([\s\S]*?)`',source(filename)).group(0)

functions=[function('map_content.go',n)for n in ['mapContentPlaceSQL','mapContentCurrentMomentPlacesSQL','ownMapPlaceRelationsSQL','mapContentBorrowedStampSQL']]
functions += [function('place_retained_visibility.go',n)for n in ['currentRetainedPlaceVisibilitySQL','currentRetainedPlaceDeadlineSQL']]
functions += [function('public_moments.go','retainedPlaceReadBindingSQL')]
functions += [function('collection_copy.go',n)for n in ['collectionCopyAuthoritySQL','collectionCopyBorrowedSQL','collectionCopyBorrowedDeadlineSQL']]
functions += [function('collections.go','collectionTables').replace('content.ErrInvalid','errors.New("invalid")')]
constants=[constant('map_content.go',n)for n in ['mapContentMomentsSQL','mapContentMediaFactsSQL']]
constants += [constant('map_public_author.go','mapPublicAuthorSafetyGenerationSQL')]
go='package main\nimport("strings";"encoding/json";"os";"errors")\n'+'\n'.join(constants+functions)+'''\nfunc main(){_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"PLACES":mapContentPlaceSQL(),"MOMENTS":mapContentMomentsSQL,"LINKS":mapContentCurrentMomentPlacesSQL(),"OWN_RELATIONS":ownMapPlaceRelationsSQL(),"MEDIA_FACTS":mapContentMediaFactsSQL})}\n'''
(OUT/'query_source.go').write_text(go,encoding='utf-8')
v=subprocess.run([r'C:\Program Files\Go\bin\go.exe','run',str(OUT/'query_source.go')],capture_output=True,text=True,timeout=30,creationflags=subprocess.CREATE_NO_WINDOW)
assert v.returncode==0,'SQL_GENERATOR_FAILED'
queries=json.loads(v.stdout)
queries['REFERENCES']=re.search(r'rows, e := f.tx.Query\(ctx, `(WITH bounded[\s\S]*?)`, missing, f.a.ID\)',source('map_content_media.go')).group(1)
(OUT/'sql.json').write_text(json.dumps(queries,indent=2)+'\n',encoding='utf-8')
spec=importlib.util.spec_from_file_location('old',W/'verify_reference167.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
transport,prefix=m.build_parts()
expected=ast.literal_eval(next(n.value for n in ast.parse(prefix).body if isinstance(n,ast.Assign)and getattr(n.targets[0],'id','')=='EXPECTED_OWNER_SHA'))
remote='import json,subprocess,hashlib,re,time\nDB="birdtie_native_hz_20261015"\nQUERIES='+repr(queries)+'\nOWNER_SHA='+repr(expected)+'\n'+r'''
def sql(statement,jit=False):
 start=time.monotonic()
 setting="SET LOCAL jit=off;"if jit else""
 p=subprocess.run(['runuser','-u','postgres','--','psql','-X','-A','-t','-q','-v','ON_ERROR_STOP=1','-d',DB],input="BEGIN READ ONLY;SET LOCAL statement_timeout='9000ms';SET LOCAL lock_timeout='500ms';"+setting+statement+";ROLLBACK;",capture_output=True,text=True,timeout=12)
 if p.returncode:
  return None,{'error':'STATEMENT_TIMEOUT'if'canceling statement due to statement timeout'in p.stderr else'LOCK_TIMEOUT'if'lock timeout'in p.stderr else'CONTROLLED_SQL_FAILURE','wallMs':round((time.monotonic()-start)*1000)}
 return json.loads(p.stdout.strip()),{'wallMs':round((time.monotonic()-start)*1000)}
guard,e=sql("SELECT jsonb_build_object('valid',current_database()='birdtie_native_hz_20261015'AND(SELECT oid=54087 AND pg_get_userbyid(datdba)='birdtie_native_hz'FROM pg_database WHERE datname=current_database())AND(SELECT max(version)=170 FROM birdtie_schema_migrations),'owners',(SELECT jsonb_agg(owner_account_id)FROM legacy_dev_phone_bindings WHERE first_enrolled_at IS NOT NULL))")
assert guard and guard['valid']and len(guard['owners'])==1,'EXACT_TARGET_REJECTED'
owner=guard['owners'][0];assert hashlib.sha256(owner.encode()).hexdigest()==OWNER_SHA,'EXACT_EXISTING_OWNER_REJECTED'
assert re.fullmatch('[a-f0-9-]{36}',owner)
lit="'"+owner+"'"
mquery=QUERIES['MOMENTS'].replace('$1',lit)
mids,e=sql("SELECT coalesce(jsonb_agg(id),'[]') FROM("+mquery+")selected")
assert mids is not None,'VISIBLE_ID_SELECTION_FAILED'
places,e=sql("SELECT coalesce(jsonb_agg(place_id),'[]') FROM legacy_map_place_facts WHERE longitude BETWEEN -3 AND -1 AND latitude BETWEEN 56 AND 58")
assert places is not None and len(places)<=500,'BOUNDED_PLACES_REQUIRED'
def array(ids):
 assert all(re.fullmatch('[a-f0-9-]{36}',i)for i in ids)
 return"ARRAY["+','.join("'"+i+"'::uuid"for i in ids)+"]::uuid[]"
def summarize(plan):
 top=plan[0];nodes=[]
 def visit(n):
  keys=['Node Type','Relation Name','Index Name','Actual Total Time','Actual Rows','Actual Loops','Shared Hit Blocks','Shared Read Blocks','Temp Read Blocks','Temp Written Blocks']
  nodes.append({k:n[k]for k in keys if k in n})
  for c in n.get('Plans',[]):visit(c)
 visit(top['Plan'])
 return{'planningMs':top.get('Planning Time'),'executionMs':top.get('Execution Time'),'jit':top.get('JIT',{}),'slowNodes':sorted(nodes,key=lambda n:n.get('Actual Total Time',0)*n.get('Actual Loops',1),reverse=True)[:12]}
results=[]
for stage in ['PLACES','MOMENTS','LINKS','OWN_RELATIONS','MEDIA_FACTS','REFERENCES']:
 statement=QUERIES[stage]
 if stage=='MOMENTS':statement=mquery
 elif stage in('LINKS','MEDIA_FACTS'):statement=statement.replace('$1',array(mids)).replace('$2',lit)
 elif stage=='REFERENCES':statement=statement.replace('$1',array(places)).replace('$2',lit)
 else:statement=statement.replace('$1',lit)
 plan,meta=sql('EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) '+statement)
 results.append({'stage':stage,'mode':'DEFAULT',**meta,**(summarize(plan)if plan else{})})
 if not plan or plan[0].get('Execution Time',0)>300 or plan[0].get('JIT',{}).get('Timing',{}).get('Total',0)>100:
  plan,meta=sql('EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) '+statement,True)
  results.append({'stage':stage,'mode':'JIT_OFF',**meta,**(summarize(plan)if plan else{})})
print(json.dumps({'status':'PASS','targetGuard':True,'schema':170,'visibleMomentIDsCount':len(mids),'viewportPlaceIDsCount':len(places),'sourceReads':0,'databaseWrites':0,'httpCalls':0,'logins':0,'queries':results}))
'''
ast.parse(remote)
(OUT/'remote.py').write_text(remote,encoding='utf-8')
v=subprocess.run(transport,input=remote.encode(),capture_output=True,timeout=150,creationflags=subprocess.CREATE_NO_WINDOW)
receipt={'exitCode':v.returncode,'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'stderrPresent':bool(v.stderr),'status':'FAIL'}
if v.stdout:
 result=json.loads(v.stdout)
 assert all(not re.search('[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}',str(value))for value in result.values()),'PRIVATE_IDENTIFIER_REJECTED'
 receipt.update(status=result['status'],result=result)
(OUT/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
print(json.dumps(receipt))
raise SystemExit(v.returncode)
