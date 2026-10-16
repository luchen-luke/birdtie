"""Enable compression on the existing Birdtie gateway without changing routes.

The prior configuration remains on the server. Validation/reload use stdin because
an atomic host-file replacement does not refresh a running file bind mount.
Only sanitized headers, sizes, hashes and timings enter local evidence.
"""
import argparse
import base64
import hashlib
import json
import re
import shlex
import subprocess
from pathlib import Path

from deploy_birdtie_public_entry import ORIGIN, ROOT, SSH


REMOTE = r'''
import gzip,hashlib,json,os,re,socket,subprocess,sys,time,urllib.error,urllib.request,uuid
from pathlib import Path
p=json.loads(sys.stdin.read())
config=Path('/etc/birdtie/Caddyfile')
old_civu=Path('/opt/civu/Caddyfile')
def sha(raw):return hashlib.sha256(raw).hexdigest()
def run(args,raw=None):
 r=subprocess.run(args,input=raw,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if r.returncode:raise RuntimeError('COMMAND_FAILED:'+args[0]+':'+str(r.returncode))
 return r.stdout
def caddy(action,raw):
 return run(['docker','exec','-i','birdtie-caddy','caddy',action,'--config','-','--adapter','caddyfile'],raw)
def request(path,encoding='identity',auth=False,base='https://47.110.154.133'):
 headers={'Accept-Encoding':encoding}
 if auth:headers['Authorization']='Bearer synthetic-compression-fixture-not-a-session'
 start=time.monotonic()
 try:response=urllib.request.urlopen(urllib.request.Request(base+path,headers=headers),timeout=20)
 except urllib.error.HTTPError as error:response=error
 with response:
  raw=response.read(32*1024*1024+1)
  assert len(raw)<=32*1024*1024,'RESPONSE_LIMIT'
  content_encoding=response.headers.get('Content-Encoding','')
  decoded=gzip.decompress(raw) if content_encoding=='gzip' else raw
  return {'status':response.status,'requestAcceptEncoding':encoding,'contentEncoding':content_encoding,
   'contentType':response.headers.get('Content-Type',''),'cacheControl':response.headers.get('Cache-Control',''),
   'vary':response.headers.get('Vary',''),'wireBytes':len(raw),'decodedBytes':len(decoded),
   'decodedSHA256':sha(decoded),'elapsedMs':round((time.monotonic()-start)*1000,2)},decoded
def ready():
 r,raw=request('/readyz','gzip')
 assert r['status']==200 and json.loads(raw)['status']=='ready','GATEWAY_NOT_READY'
 return r
def atomic_write(path,raw):
 temp=path.with_name('.'+path.name+'.compression-'+uuid.uuid4().hex)
 stat=path.stat()
 with temp.open('xb') as out:
  os.fchmod(out.fileno(),stat.st_mode & 0o777)
  os.fchown(out.fileno(),stat.st_uid,stat.st_gid)
  out.write(raw);out.flush();os.fsync(out.fileno())
 os.replace(temp,path)
 fd=os.open(str(path.parent),os.O_DIRECTORY)
 try:os.fsync(fd)
 finally:os.close(fd)
def synthetic(image):
 # Same installed Caddy image, isolated loopback port, no production routes or
 # sessions. The temporary container has no host files, TLS or database mounts.
 sock=socket.socket();sock.bind(('127.0.0.1',0));port=sock.getsockname()[1];sock.close()
 name='birdtie-compression-proof-'+uuid.uuid4().hex[:12]
 payload=json.dumps({'fixture':'x'*4096},separators=(',',':'))
 text="""{\n admin off\n auto_https off\n}\nhttp://127.0.0.1:%d {\n encode zstd gzip\n header Cache-Control "private, no-store"\n handle /small {\n  header Content-Type application/json\n  respond "{}"\n }\n handle /image {\n  header Content-Type image/jpeg\n  respond %s\n }\n handle {\n  header Content-Type application/json\n  respond %s\n }\n}\n"""%(port,json.dumps('fixture-media-'*400),json.dumps(payload))
 proc=subprocess.Popen(['docker','run','--rm','-i','--name',name,'--network','host',image,
  'caddy','run','--config','-','--adapter','caddyfile'],stdin=subprocess.PIPE,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  proc.stdin.write(text.encode());proc.stdin.close()
  base='http://127.0.0.1:'+str(port)
  for _ in range(30):
   try:
    identity,_=request('/large',auth=True,base=base);break
   except (OSError,urllib.error.URLError):time.sleep(.1)
  else:raise RuntimeError('SYNTHETIC_START_FAILED')
  compressed,_=request('/large','gzip',auth=True,base=base)
  small,_=request('/small','gzip',auth=True,base=base)
  media,_=request('/image','gzip',auth=True,base=base)
  assert identity['decodedSHA256']==compressed['decodedSHA256']
  assert compressed['contentEncoding']=='gzip' and compressed['wireBytes']<identity['wireBytes']
  assert compressed['cacheControl']==identity['cacheControl']=='private, no-store'
  assert small['contentEncoding']==media['contentEncoding']==''
  return {'status':'PASS','authentication':'SYNTHETIC_HEADER_ONLY_NO_REAL_SESSION',
   'identity':identity,'gzip':compressed,'smallJSON':small,'alreadyCompressedMediaType':media}
 finally:
  subprocess.run(['docker','rm','--force',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=15)
  proc.wait(timeout=15)

assert config.is_file() and not config.is_symlink(),'CONFIG_NOT_REGULAR'
before=config.read_bytes();text=before.decode()
assert sha(before)==p['expectedSHA256'],'CONFIG_CHANGED'
inspection=json.loads(run(['docker','inspect','birdtie-caddy']))[0]
assert inspection['State']['Running'] and inspection['HostConfig']['NetworkMode']=='host'
assert any(m['Source']==str(config) and m['Destination']=='/etc/caddy/Caddyfile' and not m['RW'] for m in inspection['Mounts'])
assert text.count('https://47.110.154.133 {')==1
assert text.count('reverse_proxy 127.0.0.1:18114')==1
assert not re.search(r'^\s*encode\b',text,re.M),'ENCODE_ALREADY_CONFIGURED'
after=text.replace('https://47.110.154.133 {','https://47.110.154.133 {\n encode zstd gzip',1).encode()
preserved={'civuCaddyfileSHA256':sha(old_civu.read_bytes()),
 'webRelease':str(Path('/opt/birdtie/web/current').resolve()),
 'apiEnvironmentSHA256':sha(Path('/etc/birdtie/api.env').read_bytes())}
proof={'status':'INSPECTED','beforeConfigSHA256':sha(before),'afterConfigSHA256':sha(after),
 'installedVersion':run(['docker','exec','birdtie-caddy','caddy','version']).decode().strip(),
 'origin':'https://47.110.154.133','tlsDefaultValidation':True,'before':{},'after':{},
 'configurationChange':'Add one site-level encode zstd gzip directive; no routes/TLS/cache headers altered',
 'preserved':preserved,'rollbackAttempted':False}
try:
 proof['before']['ready']=ready()
 for name,path,auth in [('cities','/v1/cities',False),('web','/',False),('privateRejection','/v1/me/map-content?west=120&east=121&south=30&north=31&zoom=12',True)]:
  proof['before'][name],_=request(path,'gzip',auth)
 caddy('validate',after);proof['installedBinaryValidation']='PASS'
 if not p['apply']:
  print(json.dumps(proof));sys.exit(0)
 proof['synthetic']=synthetic(inspection['Image'])
 backup=config.with_name('Caddyfile.before-compression-'+time.strftime('%Y%m%dT%H%M%SZ',time.gmtime())+'-'+sha(before)[:12])
 with backup.open('xb') as out:
  os.fchmod(out.fileno(),0o600);out.write(before);out.flush();os.fsync(out.fileno())
 proof['backupPath']=str(backup);proof['backupSHA256']=sha(backup.read_bytes())
 assert config.read_bytes()==before,'CONFIG_CHANGED_BEFORE_WRITE'
 try:
  atomic_write(config,after)
  caddy('reload',after)
  proof['after']['ready']=ready()
  for name,path,auth in [('cities','/v1/cities',False),('web','/',False),('privateRejection','/v1/me/map-content?west=120&east=121&south=30&north=31&zoom=12',True)]:
   proof['after'][name],_=request(path,'gzip',auth)
   previous=proof['before'][name];current=proof['after'][name]
   assert current['status']==previous['status'],name+'_STATUS_CHANGED'
   if name!='privateRejection':
    assert current['decodedSHA256']==previous['decodedSHA256'],name+'_CONTENT_CHANGED'
   assert current['cacheControl']==previous['cacheControl'],name+'_CACHE_HEADERS_CHANGED'
  for name in ('cities','web'):
   item=proof['after'][name]
   assert item['contentEncoding']=='gzip' and item['wireBytes']<item['decodedBytes'],name+'_NOT_COMPRESSED'
   assert 'accept-encoding' in item['vary'].lower(),name+'_VARY_MISSING'
  zstd,_=request('/v1/cities','zstd')
  assert zstd['status']==200 and zstd['contentEncoding']=='zstd','ZSTD_NOT_NEGOTIATED'
  proof['after']['citiesZstd']={k:v for k,v in zstd.items() if k not in ('decodedSHA256','decodedBytes')}
  assert config.read_bytes()==after
  assert sha(old_civu.read_bytes())==preserved['civuCaddyfileSHA256']
  assert sha(Path('/etc/birdtie/api.env').read_bytes())==preserved['apiEnvironmentSHA256']
  assert str(Path('/opt/birdtie/web/current').resolve())==preserved['webRelease']
  proof['status']='PASS';proof['reload']='PASS';proof['noServiceRestart']=True
 except BaseException:
  proof['rollbackAttempted']=True
  atomic_write(config,before);caddy('reload',before);ready()
  proof['rollback']='PASS';raise
except Exception as error:
 proof['status']='FAIL';proof['errorClass']=type(error).__name__
 # Only our bounded labels are emitted; never dump config, raw bodies or tokens.
 if isinstance(error,AssertionError) and re.fullmatch('[A-Za-z0-9_]+',str(error)):
  proof['errorCode']=str(error)
 print(json.dumps(proof));sys.exit(1)
print(json.dumps(proof))
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--expected-sha256', required=True)
    parser.add_argument('--evidence', required=True)
    args = parser.parse_args()
    assert re.fullmatch('[0-9a-f]{64}', args.expected_sha256)
    assert re.fullmatch('[a-z0-9-]+', args.evidence)
    out = ROOT / 'docs/testing/evidence/content-map-refinement-20261016/gateway-compression' / args.evidence
    out.mkdir(parents=True, exist_ok=False)
    payload = {'apply': args.apply, 'expectedSHA256': args.expected_sha256}
    bootstrap = "import base64;exec(base64.b64decode('" + base64.b64encode(REMOTE.encode()).decode() + "'))"
    result = subprocess.run(
        SSH + ['sudo -n python3 -c ' + shlex.quote(bootstrap)],
        input=json.dumps(payload).encode(), stdout=subprocess.PIPE,
        stderr=subprocess.PIPE, creationflags=subprocess.CREATE_NO_WINDOW,
        timeout=240,
    )
    receipt = {'exitCode': result.returncode, 'origin': ORIGIN,
               'remoteScriptSHA256': hashlib.sha256(REMOTE.encode()).hexdigest(),
               'scriptSHA256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
               'apply': args.apply}
    try:
        receipt['result'] = json.loads(result.stdout)
    except (ValueError, UnicodeDecodeError):
        receipt['status'] = 'FAILED_WITHOUT_RECEIPT'
    # Do not persist stderr, which may include configuration fragments on errors.
    receipt['stderrBytes'] = len(result.stderr)
    (out / 'receipt.json').write_text(json.dumps(receipt, indent=2) + '\n', encoding='utf-8')
    print(json.dumps(receipt))
    return result.returncode


if __name__ == '__main__':
    raise SystemExit(main())
