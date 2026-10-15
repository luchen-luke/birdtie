"""Bounded read-only Birdtie API/gateway error classification for one QA window."""
import hashlib, importlib.util, json, re, subprocess
from pathlib import Path
WORK = Path(__file__).resolve().parent
ROOT = Path(r'D:\Project\birdtie')
OUT = WORK / 'device19-thread-window-diagnostic01'
REMOTE = r'''
import collections,json,re,subprocess
since='2026-10-15 17:16:25 UTC';until='2026-10-15 17:17:10 UTC'
def read(args):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20)
 if p.returncode: return [],{'exitCode':p.returncode,'stderrPresent':bool(p.stderr)}
 if len(p.stdout)>4*1024*1024:raise RuntimeError('BOUNDED_LOG_EXCEEDED')
 return p.stdout.decode('utf-8',errors='replace').splitlines(),{'exitCode':0,'stderrPresent':bool(p.stderr)}
lines,journal=read(['journalctl','-u','birdtie-api.service','--since',since,'--until',until,'--no-pager','-o','json','-n','400'])
counts=collections.Counter();states=collections.Counter();constraints=set();frames=set();direct=0
for line in lines:
 try:message=json.loads(line).get('MESSAGE','')
 except Exception:continue
 if not isinstance(message,str):continue
 low=message.lower()
 for name,marker in [('panic','panic:'),('serverError','server error:'),('deadline','deadline exceeded'),('memory','out of memory'),('connectionReset','connection reset'),('sqlError','sqlstate')]:
  if marker in low:counts[name]+=1
 if 'moment_thread' in low or 'momentthread' in low or '8ca660be-f15f-4bad-acc9-87d9a1f25036' in low:direct+=1
 for state in re.findall(r'SQLSTATE\s+([A-Z0-9]{5})',message):states[state]+=1
 for constraint in re.findall(r'(?:constraint|relation) "((?:moment_|public_moment_|birdtie_)[a-z0-9_]{1,90})"',message):constraints.add(constraint)
 for frame in re.findall(r'(?:github\.com/birdtie/birdtie/apps/api/)?(?:internal/)?(?:postgres|httpapi)/[a-z_]+\.go:[0-9]+',message):frames.add(frame)
gateway_lines,gateway=read(['docker','logs','--since','2026-10-15T17:16:25Z','--until','2026-10-15T17:17:10Z','--tail','400','birdtie-caddy'])
events=[]
for line in gateway_lines:
 try:v=json.loads(line)
 except Exception:continue
 request=v.get('request') or {};uri=request.get('uri') or '';method=request.get('method');status=v.get('status')
 if not isinstance(uri,str):continue
 path=uri.split('?',1)[0]
 if re.fullmatch('/v1/me/moment-threads/(?:previews|operations/[0-9a-f-]{36}(?:/stop)?)',path):
  events.append({'timestamp':v.get('ts'),'method':method,'status':status,'route':'THREAD_PREVIEW'if path.endswith('/previews')else'THREAD_OPERATION_STOP'if path.endswith('/stop')else'THREAD_OPERATION'})
print(json.dumps({'status':'READ_ONLY_WINDOW_CLASSIFIED','windowSinceUTC':'2026-10-15T17:16:25Z','windowUntilUTC':'2026-10-15T17:17:10Z','journal':journal,'apiMessagesInWindow':len(lines),'apiErrorCounts':dict(counts),'sqlStates':dict(states),'canonicalConstraintNames':sorted(constraints),'canonicalGoFrames':sorted(frames),'threadDirectMentionMessages':direct,'gateway':gateway,'gatewayThreadCandidateEvents':events,'gatewayEventAccountAttribution':'NOT_AVAILABLE_AUTH_HEADERS_NOT_READ','rawMessagesSaved':False,'credentialHeadersRead':False,'databaseAccess':False,'serviceMutations':0}))
'''
if __name__ == '__main__':
    spec=importlib.util.spec_from_file_location('deployment',ROOT/'scripts/deploy_birdtie_hangzhou.py')
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    OUT.mkdir(exist_ok=False)
    run=subprocess.run(module.SSH+['sudo -n python3 -'],input=REMOTE.encode(),capture_output=True,timeout=60,creationflags=subprocess.CREATE_NO_WINDOW)
    proof={'exitCode':run.returncode,'remoteScriptSHA256':hashlib.sha256(REMOTE.encode()).hexdigest(),'helperSHA256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),'stderrPresent':bool(run.stderr),'status':'FAIL'}
    if run.returncode==0:
        proof.update(status='READ_ONLY_COMPLETE',result=json.loads(run.stdout))
    (OUT/'receipt.json').write_text(json.dumps(proof,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(proof))
