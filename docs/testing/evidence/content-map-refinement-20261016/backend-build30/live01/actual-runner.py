"""API30 single legacy image acceptance. PLAN does not connect; execute only after root release gate."""
from pathlib import Path
import argparse, ast, hashlib, importlib.util, json, re, subprocess

W = Path(__file__).resolve().parent
R = Path(r'D:/Project/birdtie')

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--execute', action='store_true')
    p.add_argument('--expected-release')
    p.add_argument('--expected-sha')
    p.add_argument('--name', default='live01')
    args = p.parse_args()
    if not re.fullmatch(r'live[0-9]{2}', args.name):
        raise SystemExit('INVALID_LOCAL_LABEL')
    spec = importlib.util.spec_from_file_location('base', W/'verify_reference167.py')
    m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
    ssh, prefix = m.build_parts()
    prefix = prefix.replace("target['schema']==167", "target['schema']==173")
    helpers = (W/'verify_map170.remote.py').read_text().split("stage='PREFLIGHT'")[0]
    body = (W/'verify_lazy30.remote.py').read_text()
    remote = prefix + '\n' + helpers + '\nEXPECTED_RELEASE=' + repr(args.expected_release) + '\nEXPECTED_BINARY_SHA=' + repr(args.expected_sha) + '\n' + body
    ast.parse(remote)
    if not args.execute:
        print(json.dumps({'status':'PLAN_NOT_EXECUTED','apiBuild':30,'schema':173,'imageTargets':1,'expectedReleaseAndSHARequired':True,'writes':'LOGIN_LOGOUT_ONLY','privateLocatorPersisted':False,'worldInventory':False,'detailInventory':False}))
        return 0
    if not args.expected_release or not re.fullmatch(r'/opt/birdtie/releases/social173-[0-9]{8}-build30', args.expected_release) or not args.expected_sha or not re.fullmatch(r'[a-f0-9]{64}', args.expected_sha):
        raise SystemExit('EXACT_API30_RELEASE_AND_SHA_REQUIRED')
    out = R/'docs/testing/evidence/content-map-refinement-20261016/backend-build30'/args.name
    out.mkdir(exist_ok=False)
    receipt = {'status':'FAIL','privateValuesPersisted':False,'sessionCleanup':'UNKNOWN_UNTIL_REMOTE_RECEIPT','remoteScriptSHA256':hashlib.sha256(remote.encode()).hexdigest()}
    try:
        run = subprocess.run(ssh,input=remote.encode(),capture_output=True,timeout=660,creationflags=subprocess.CREATE_NO_WINDOW)
        receipt.update(exitCode=run.returncode,stderrPresent=bool(run.stderr))
        value = json.loads(run.stdout)
        if not m.aggregate_safe(value):
            raise ValueError('NON_AGGREGATE_RESPONSE_REJECTED')
        receipt.update(result=value,sessionCleanup=value.get('sessionCleanup','UNKNOWN'),status='PASS' if run.returncode==0 and value.get('status')=='PASS' else 'FAIL')
    except Exception as e:
        receipt.update(failureType=type(e).__name__,failureCode='CONTROLLED_LAZY_ACCEPTANCE_FAILED')
    (out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n',encoding='utf-8')
    (out/'actual-runner.py').write_text(Path(__file__).read_text(),encoding='utf-8')
    (out/'remote.py').write_text(remote,encoding='utf-8')
    print(json.dumps(receipt))
    return 0 if receipt['status']=='PASS' else 1

if __name__=='__main__':
    raise SystemExit(main())
