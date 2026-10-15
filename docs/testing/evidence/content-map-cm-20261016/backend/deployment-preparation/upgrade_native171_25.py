"""Refresh exact schema171/build24 to schema171/build25; no migration and no execution by default."""
import argparse,importlib.util,json,sys
from pathlib import Path

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--execute',action='store_true');p.add_argument('--name',default='upgrade171-02');p.add_argument('--reuse-staged',action='store_true');a=p.parse_args()
 previous=Path(__file__).with_name('upgrade_native171_24.py');spec=importlib.util.spec_from_file_location('previous171',previous);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
 prior,s=m.program();assert 'prior_count == 170 and target == 171'in s
 s=s.replace('prior_count == 170 and target == 171','prior_count == 171 and target == 171')
 assert s.count("'/opt/birdtie/releases/social170-20261015-build23'")==1
 s=s.replace("'/opt/birdtie/releases/social170-20261015-build23'","'/opt/birdtie/releases/social171-20261015-build24'")
 if not a.execute:
  print(json.dumps({'status':'PLAN_NOT_EXECUTED','expectedDatabaseOID':54087,'expectedSchema':171,'targetSchema':171,'expectedAPI':24,'targetAPI':25,'buildName':'build25','newMigrations':0,'remoteWrites':0}));return 0
 ns={'__name__':'cm_deployment25','__file__':str(prior)};exec(compile(s,str(prior),'exec'),ns);ns['SSH'].insert(1,'-C')
 sys.argv=[str(prior),'upgrade','--evidence-name',a.name,'--build-name','build25','--expected-migrations','171','--target-migrations','171']+(['--reuse-staged']if a.reuse_staged else [])
 return ns['main']()
if __name__=='__main__':raise SystemExit(main())
