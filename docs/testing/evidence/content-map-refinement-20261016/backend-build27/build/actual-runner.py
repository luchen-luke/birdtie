from pathlib import Path
import sys,ast
w=Path(__file__).parent
p=w/'build_map20.py';s=p.read_text()
s=s.replace('target in (165,166,167,168,169,170)','target == 171')
s=s.replace("OUT=ROOT/'docs/testing/evidence/social-remote-migration-20261015'/name", "OUT=ROOT/'docs/testing/evidence/content-map-refinement-20261016/backend-build27/build'")
s=s.replace("'legacy-history-import','legacy-map-restore']", "'legacy-history-import','legacy-map-restore','legacy-map-anchor-restore']")
s=s.replace("'binaries':10", "'binaries':11")
ast.parse(s)
sys.argv=[str(p),'build27','171'];exec(compile(s,str(p),'exec'),{'__name__':'__main__','__file__':str(p)})
