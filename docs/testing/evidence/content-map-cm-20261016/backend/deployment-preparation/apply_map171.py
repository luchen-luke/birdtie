"""Exact protected-source 16-anchor restore. Default plan; --execute explicitly required."""
import argparse,ast,json,sys
from pathlib import Path

def program():
 prior=Path(__file__).with_name('apply_map170.py');s=prior.read_text()
 assert "social170-20261015-build23" in s
 s=s.replace('social170-20261015-build23','social171-20261015-build24').replace('birdtie-legacy-map-restore','birdtie-legacy-map-anchor-restore').replace("'schema':170","'schema':171")
 s=s.replace("out=r/'docs/testing/evidence/now-civu-map-20261016'/name", "out=r/'docs/testing/evidence/content-map-cm-20261016/backend'/name")
 needle="runid='c5df0d7c-f361-4275-9838-feb6d1d92352';base=[str(binary)]"
 assert needle in s
 replacement="""assert all((folder/n).stat().st_mode&0o777==0o600 for n in ['source.json','manifest.json'])
 sqlenv=dict(env,PGHOST='127.0.0.1',PGPORT='5432',PGDATABASE=s['database'],PGUSER=s['owner'],PGPASSWORD=s['password'])
 def guard():
  q=\"SELECT json_build_object('oid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),'schema',(SELECT max(version)FROM birdtie_schema_migrations),'contract',birdtie_content_map_contract(),'moments',(SELECT count(*)FROM moments),'places',(SELECT count(*)FROM places))::text;\"
  response=subprocess.run(['psql','-X','-qAt','-v','ON_ERROR_STOP=1'],input=q.encode(),env=sqlenv,capture_output=True,timeout=30)
  assert response.returncode==0
  return json.loads(response.stdout)
 before=guard();assert before['oid']==54087 and before['schema']==171 and before['contract']=='map-content-v2'
 runid='c5df0d7c-f361-4275-9838-feb6d1d92352';base=[str(binary)]"""
 s=s.replace(needle,replacement)
 needle="stage='PREPARE';prepared=run(base+source+['--validate'],env)"
 assert needle in s;s=s.replace(needle,needle+"\n  assert prepared['status']=='PREPARED_NOT_IMPORTED' and prepared['counts']=={'anchors':16,'mediaAnchors':15,'noteAnchors':1}")
 needle="assert sha(folder/'source.json')==p['sourceSHA'] and sha(folder/'manifest.json')==p['manifestSHA']\n print(json.dumps("
 assert needle in s;s=s.replace(needle,"assert sha(folder/'source.json')==p['sourceSHA'] and sha(folder/'manifest.json')==p['manifestSHA']\n assert guard()==before\n print(json.dumps(")
 ast.parse(s)
 tree=ast.parse(s)
 for node in tree.body:
  if isinstance(node,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='remote' for t in node.targets):ast.parse(ast.literal_eval(node.value))
 return prior,s
def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('mode',choices=['prepare','apply','inspect']);p.add_argument('--execute',action='store_true');p.add_argument('--name',default='anchor171-01');a=p.parse_args()
 prior,s=program()
 if not a.execute:
  print(json.dumps({'status':'PLAN_NOT_EXECUTED','mode':a.mode,'expectedDatabaseOID':54087,'schema':171,'release':'social171-20261015-build24','expectedAnchors':16,'expectedMediaAnchors':15,'expectedNoteAnchors':1,'remoteWrites':0,'sourceWrites':0}));return 0
 sys.argv=[str(prior),a.mode,a.name];exec(compile(s,str(prior),'exec'),{'__name__':'__main__','__file__':str(prior)})
 return 0
if __name__=='__main__':raise SystemExit(main())
