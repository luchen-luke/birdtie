import ast,hashlib,json,subprocess,sys,re
from pathlib import Path
R=Path(r'D:\Project\birdtie');tree=ast.parse((R/'docs/testing/evidence/github-remote-completion-20261015/remote-legacy-login01/verify.py').read_text())
v={n.targets[0].id:ast.literal_eval(n.value)for n in tree.body if isinstance(n,ast.Assign)and isinstance(n.targets[0],ast.Name)and n.targets[0].id in('REMOTE','SSH')}
remote=v['REMOTE'].split('owner=None;before=None;source_sha=None;manifest_sha=None')[0].replace("BASE='http://127.0.0.1:18114'","BASE='https://47.110.154.133'").replace("cursor=page.get('nextCursor','')","cursor=page.get('nextCursor') or ''")
remote+=(Path(__file__).with_name('verify_journey_video159.remote.py')).read_text()
ast.parse(remote);name=sys.argv[1] if len(sys.argv)>1 else 'real-owner-journeys-videos01';assert re.fullmatch('[a-z0-9-]+',name);out=R/'docs/testing/evidence/civu-native-adaptation-20261015'/name;out.mkdir(exist_ok=False)
(out/'controlled-verification.py').write_text(Path(__file__).read_text())
(out/'controlled-verification.remote.py').write_text(Path(__file__).with_name('verify_journey_video159.remote.py').read_text())
r=subprocess.run(v['SSH'],input=remote.encode(),capture_output=True,creationflags=subprocess.CREATE_NO_WINDOW)
(out/'stdout.json').write_bytes(r.stdout);(out/'stderr.log').write_bytes(r.stderr)
receipt={'exitCode':r.returncode,'remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest(),'result':json.loads(r.stdout)if r.stdout else None}
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt));raise SystemExit(r.returncode)
