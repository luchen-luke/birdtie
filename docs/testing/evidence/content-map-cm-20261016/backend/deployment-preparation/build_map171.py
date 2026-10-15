"""Build API24/schema171 locally; default prints a plan and does not build."""
import argparse, ast, json, sys
from pathlib import Path

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--execute',action='store_true');p.add_argument('--name',default='build24');a=p.parse_args()
 assert a.name.replace('-','').isalnum()
 prior=Path(__file__).with_name('build_map20.py');s=prior.read_text()
 assert "target in (165,166,167,168,169,170)" in s
 s=s.replace("target in (165,166,167,168,169,170)","target == 171")
 assert "'legacy-history-import','legacy-map-restore']" in s
 s=s.replace("'legacy-history-import','legacy-map-restore']","'legacy-history-import','legacy-map-restore','legacy-map-anchor-restore']")
 s=s.replace("'binaries':10","'binaries':11")
 ast.parse(s)
 if not a.execute:
  print(json.dumps({'status':'PLAN_NOT_EXECUTED','apiBuild':24,'schema':171,'buildName':a.name,'binaryCount':11,'artifact':'D:/Project/birdtie/work/social-remote-migration-20261015/'+a.name+'/release.tar','remoteWrites':0}));return 0
 sys.argv=[str(prior),a.name,'171'];exec(compile(s,str(prior),'exec'),{'__name__':'__main__','__file__':str(prior)})
 return 0
if __name__=='__main__':raise SystemExit(main())
