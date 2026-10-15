"""Prepare exact native170/API23 to native171/API24 upgrade; no execution by default."""
import argparse,ast,json,sys
from pathlib import Path

def replace_once(s,old,new):
 assert s.count(old)==1,old
 return s.replace(old,new,1)
def program():
 p=Path('D:/Project/birdtie/scripts/deploy_birdtie_hangzhou.py');s=p.read_text()
 s=replace_once(s,'148 <= prior_count <= target <= 170','prior_count == 170 and target == 171')
 s=replace_once(s,"settings=json.loads(STATE.read_text());assert settings['database']==DB\n target=", "settings=json.loads(STATE.read_text());assert settings['database']==DB and settings['oid']==54087 and settings['owner']==ROLE and settings['release']=='/opt/birdtie/releases/social170-20261015-build23'\n target=")
 s=s.replace('target+(10 if target>=170','target+(11 if target>=171 else 10 if target>=170')
 s=replace_once(s,'legacy-history-import|legacy-map-restore)', 'legacy-history-import|legacy-map-restore|legacy-map-anchor-restore)')
 s=replace_once(s," migration_names[170]='migrations/170_legacy_map_visibility.sql'", " migration_names[170]='migrations/170_legacy_map_visibility.sql'\n migration_names[171]='migrations/171_content_map_anchors.sql'")
 s=s.replace('range(149,171)','range(149,172)').replace('range(148,171)','range(148,172)')
 # Pin startup to the exact post-migration content contract before switching.
 s=replace_once(s,' unit=Path(\'/etc/systemd/system/birdtie-api.service\');text=unit.read_text();old='," assert sql(DB,\"SELECT birdtie_content_map_contract();\").decode().strip()=='map-content-v2'\n unit=Path('/etc/systemd/system/birdtie-api.service');text=unit.read_text();old=")
 tree=ast.parse(s)
 for node in tree.body:
  if isinstance(node,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='REMOTE' for t in node.targets):ast.parse(ast.literal_eval(node.value))
 return p,s
def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--execute',action='store_true');p.add_argument('--name',default='upgrade171-01');p.add_argument('--build-name',default='build24');p.add_argument('--reuse-staged',action='store_true');a=p.parse_args()
 prior,s=program()
 if not a.execute:
  print(json.dumps({'status':'PLAN_NOT_EXECUTED','expectedDatabaseOID':54087,'expectedSchema':170,'targetSchema':171,'expectedAPI':23,'targetAPI':24,'newBinary':'birdtie-legacy-map-anchor-restore','buildName':a.build_name,'remoteWrites':0}));return 0
 ns={'__name__':'cm_deployment','__file__':str(prior)};exec(compile(s,str(prior),'exec'),ns);ns['SSH'].insert(1,'-C')
 sys.argv=[str(prior),'upgrade','--evidence-name',a.name,'--build-name',a.build_name,'--expected-migrations','170','--target-migrations','171']+(['--reuse-staged']if a.reuse_staged else [])
 return ns['main']()
if __name__=='__main__':raise SystemExit(main())
