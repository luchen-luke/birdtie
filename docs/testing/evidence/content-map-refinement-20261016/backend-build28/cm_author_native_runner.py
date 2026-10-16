from pathlib import Path
import sys
R=Path(r'D:\Project\birdtie')
s=(R/'work/remaining-execution-20261014/run_native.py').read_text()
needle="out=ROOT/'docs/testing/evidence/remaining-execution-20261014'/('batch-'+batch)/name"
assert needle in s;s=s.replace(needle,"out=ROOT/'docs/testing/evidence/content-map-refinement-20261016/backend-build28'/name")
needle="BIRDTIE_DISPOSABLE_DB='1',BIRDTIE_AGENT_ACTIVITY_JOIN_ENABLED='false'"
assert needle in s;s=s.replace(needle,"BIRDTIE_DISPOSABLE_DB='1',BIRDTIE_LEGACY_IMPORT_NATIVE_TEST='1',BIRDTIE_AGENT_ACTIVITY_JOIN_ENABLED='false'")
exec(compile(s,str(__file__),'exec'))
