from pathlib import Path
import sys,subprocess,json,hashlib,os,datetime
R=Path(r'D:/Project/birdtie');root=R/'docs/testing/evidence/content-map-lazy-display-20261016'
label=sys.argv[1];out=root/label;out.mkdir(exist_ok=False)
paths=['apps/client/lib/src/workspace/owned_map_api.dart','apps/client/lib/src/workspace/owned_map_controller.dart','apps/client/lib/src/workspace/now_map_content_panel.dart','apps/client/test/content_map_lazy_display_test.dart']
def frame():return {p:hashlib.sha256((R/p).read_bytes()).hexdigest()for p in paths}
before=frame();commands=[
 ['D:/DevTools/flutter/bin/cache/dart-sdk/bin/dart.exe','analyze','--format','machine',* [p.removeprefix('apps/client/')for p in paths]],
 ['D:/DevTools/flutter/bin/flutter.bat','test','test/content_map_lazy_display_test.dart','test/content_map_reader_test.dart','test/content_map_panel_test.dart','--reporter','expanded']]
jobs=[]
for name,cmd in zip(['analyze','test'],commands):
 with(out/(name+'.log')).open('xb')as log:p=subprocess.run(cmd,cwd=R/'apps/client',stdout=log,stderr=subprocess.STDOUT,creationflags=subprocess.CREATE_NO_WINDOW)
 jobs.append({'kind':name,'command':cmd,'exitCode':p.returncode,'logSHA256':hashlib.sha256((out/(name+'.log')).read_bytes()).hexdigest()})
 print(name,p.returncode)
 print((out/(name+'.log')).read_text(encoding='utf-8',errors='replace')[-6000:])
after=frame();receipt={'createdAtUTC':datetime.datetime.now(datetime.timezone.utc).isoformat(),'jobs':jobs,'sourceBefore':before,'sourceAfter':after,'sourceStable':before==after,'productionWrites':0}
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');(out/'actual-runner.py').write_text(Path(__file__).read_text())
raise SystemExit(0 if before==after and all(j['exitCode']==0 for j in jobs)else 1)
