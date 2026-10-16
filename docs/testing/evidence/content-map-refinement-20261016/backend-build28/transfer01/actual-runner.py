"""Root-run-only transfer of the exact six authenticated author-avatar originals.

Default PLAN does not read private inputs or connect. --execute stages only a
new root-only remote directory. It never runs an importer or changes services.
"""
import argparse
import base64
import hashlib
import importlib.util
import io
import json
import os
import re
import shlex
import subprocess
import sys
import tarfile
import threading
from pathlib import Path

sys.dont_write_bytecode = True

HERE = Path(__file__).resolve().parent
ROOT = Path(r'D:\Project\birdtie')
PRIVATE = Path(r'C:\Users\chens\Documents\Codex\2026-10-14\birdtie-hangzhou')
BLOBS = PRIVATE / 'civu-private-author-avatars-20261016-01'
MATCHED = PRIVATE / 'civu-private-media-match-20261015-01'
EXTRACT_HELPER = HERE / 'cm_extract_author_avatars.py'
EXTRACT_HELPER_SHA = '759d02ad5d71f1f90a32d9b90ed9e32f1f301424cafe154fe33bb89055a409e3'
SEAL_SHA = '6aa8fdb2326ae66b743355959fef5584b31ce4472371bebb961dbf8abc28f169'
REMOTE_FOLDER = '/srv/birdtie-data/migration/private-author-avatars-20261016-01'
PROTOCOL = 'birdtie-author-avatar-transfer-v1'

REMOTE = r'''
import hashlib,io,json,os,re,stat,sys,tarfile
from pathlib import Path
FOLDER=Path('/srv/birdtie-data/migration/private-author-avatars-20261016-01')
PROTOCOL='birdtie-author-avatar-transfer-v1'
stage='PREFLIGHT';created=False;seen=set();total=0
def require(v):
 if not v:raise ValueError('CONTROLLED_GATE_REJECTED')
def sha(p):
 h=hashlib.sha256()
 with p.open('rb')as f:
  while chunk:=f.read(65536):h.update(chunk)
 return h.hexdigest()
class Limited:
 def __init__(self,source,n):self.source=source;self.left=n
 def read(self,n=-1):
  if self.left==0:return b''
  if n<0 or n>self.left:n=self.left
  raw=self.source.read(n);require(raw);self.left-=len(raw);return raw
try:
 line=sys.stdin.buffer.readline(1024*1024+1);require(len(line)<=1024*1024 and line.endswith(b'\n'));p=json.loads(line)
 keys={'protocol','files','selectedAssets','privateImages','systemArtworkArchives','sourceAuthenticated','sourceFileSHA256','matcherFileSHA256','matcherSealSHA256','selectionProjectionSHA256','authenticationProofSHA256','extractionReceiptSHA256','transferManifestSHA256','archivePlaintextSHA256','archiveBytes','tarBytes'}
 require(set(p)==keys and p['protocol']==PROTOCOL and p['sourceAuthenticated']is True and p['selectedAssets']==6 and p['privateImages']==6 and p['systemArtworkArchives']==0)
 hashes=['sourceFileSHA256','matcherFileSHA256','matcherSealSHA256','selectionProjectionSHA256','authenticationProofSHA256','extractionReceiptSHA256','transferManifestSHA256','archivePlaintextSHA256']
 require(all(isinstance(p[k],str)and re.fullmatch('[a-f0-9]{64}',p[k])for k in hashes))
 require(p['matcherSealSHA256']=='6aa8fdb2326ae66b743355959fef5584b31ce4472371bebb961dbf8abc28f169')
 require(type(p['archiveBytes'])is int and p['archiveBytes']>0 and type(p['tarBytes'])is int)
 files=p['files'];require(isinstance(files,dict)and len(files)==8)
 blobs=0;expected_bytes=0;raw_tar=1024
 for name,v in files.items():
  require(isinstance(name,str)and re.fullmatch(r'[0-9a-f]{64}\.blob|matched\.private\.json|authenticated-seal\.json',name))
  require(isinstance(v,dict)and set(v)=={'sha256','bytes'}and type(v['bytes'])is int and 0<v['bytes']<=16*1024**2 and re.fullmatch('[a-f0-9]{64}',v['sha256']))
  if name.endswith('.blob'):require(name==v['sha256']+'.blob');blobs+=1
  expected_bytes+=v['bytes'];raw_tar+=512+((v['bytes']+511)//512)*512
 require(blobs==6 and expected_bytes<=128*1024**2 and files['matched.private.json']['sha256']==p['matcherFileSHA256']and files['authenticated-seal.json']['sha256']==p['matcherSealSHA256'])
 require(p['tarBytes']==((raw_tar+10239)//10240)*10240)
 payload_hash=hashlib.sha256(json.dumps(p,sort_keys=True,separators=(',',':')).encode()).hexdigest()
 require(os.geteuid()==0)
 for path in [Path('/srv'),Path('/srv/birdtie-data'),FOLDER.parent]:
  s=os.lstat(path);require(stat.S_ISDIR(s.st_mode)and not stat.S_ISLNK(s.st_mode)and s.st_uid==0 and not(s.st_mode&0o022))
 require(not os.path.lexists(FOLDER));FOLDER.mkdir(mode=0o700);created=True;os.chmod(FOLDER,0o700)
 stage='EXACT_FILES_TRANSFER';limited=Limited(sys.stdin.buffer,p['tarBytes'])
 with tarfile.open(fileobj=limited,mode='r|')as archive:
  for m in archive:
   require(m.isfile()and not(m.issym()or m.islnk()or m.sparse)and not m.pax_headers and m.name in files and m.name not in seen)
   proof=files[m.name];require(m.size==proof['bytes'])
   source=archive.extractfile(m);require(source is not None);h=hashlib.sha256();count=0
   fd=os.open(FOLDER/m.name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
   with os.fdopen(fd,'wb')as target:
    while chunk:=source.read(65536):
     count+=len(chunk);require(count<=proof['bytes']);h.update(chunk);target.write(chunk)
    target.flush();os.fsync(target.fileno())
   require(count==proof['bytes']and h.hexdigest()==proof['sha256']);os.chmod(FOLDER/m.name,0o600);seen.add(m.name);total+=count
 while limited.left:limited.read(min(65536,limited.left))
 require(seen==set(files)and total==expected_bytes)
 stage='LOCAL_SOURCE_FRAME_ACK';footer_line=sys.stdin.buffer.readline(4097);require(footer_line.endswith(b'\n')and len(footer_line)<=4096)
 footer=json.loads(footer_line);require(footer=={'transportComplete':True,'payloadSHA256':payload_hash,'sourceInputsUnchanged':True})
 require(sys.stdin.buffer.read(1)==b'')
 stage='FINAL_FILE_RECHECK'
 require({v.name for v in FOLDER.iterdir()}==set(files))
 for name,v in files.items():
  target=FOLDER/name;s=os.lstat(target);require(stat.S_ISREG(s.st_mode)and not stat.S_ISLNK(s.st_mode)and s.st_uid==0 and stat.S_IMODE(s.st_mode)==0o600 and s.st_size==v['bytes']and sha(target)==v['sha256'])
 marker={k:v for k,v in p.items()if k!='tarBytes'};marker['filesVerified']=True;marker['sourceInputsUnchanged']=True
 with (FOLDER/'transfer-complete.json').open('x',encoding='utf-8')as f:f.write(json.dumps(marker,sort_keys=True,indent=2)+'\n');f.flush();os.fsync(f.fileno())
 os.chmod(FOLDER/'transfer-complete.json',0o600)
 print(json.dumps({'status':'PRIVATE_AUTHOR_AVATAR_TRANSFER_COMPLETE','selectedAssets':6,'privateImages':6,'systemArtworkArchives':0,'blobFiles':6,'files':8,'bytes':total,'directoryMode':'0700','fileMode':'0600','sourceAuthenticated':True,'filesVerified':True,'sourceInputsUnchanged':True,'sourceWrites':0,'databaseWrites':0,'mediaPublication':False,'privateValuesOmitted':True}),flush=True)
except Exception as e:
 print(json.dumps({'status':'TRANSFER_REJECTED_UNACCEPTED_DIRECTORY_PRESERVED'if created else'TRANSFER_REJECTED_BEFORE_CREATION','stage':stage,'errorType':type(e).__name__,'receivedFiles':len(seen),'receivedBytes':total,'sourceWrites':0,'databaseWrites':0,'mediaPublication':False,'privateValuesOmitted':True}),flush=True);raise SystemExit(2)
'''


class Guard(Exception):
    pass


def require(value, code):
    if not value:
        raise Guard(code)


def file_sha(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        while chunk := stream.read(1024 * 1024):
            h.update(chunk)
    return h.hexdigest()


def json_bytes(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def write_json(path, value):
    with path.open('x', encoding='utf-8') as stream:
        stream.write(json.dumps(value, indent=2) + '\n')


def import_file(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def protected_directory_gate():
    # Read-only counterpart of the extractor's pure.NET DACL creation gate.
    script = r'''
$ErrorActionPreference='Stop';$ProgressPreference='SilentlyContinue'
[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false)
try{
 $p='__PATH__';$sid=[System.Security.Principal.WindowsIdentity]::GetCurrent().User
 $a=[IO.Directory]::GetAccessControl($p);$rules=$a.GetAccessRules($true,$true,[System.Security.Principal.SecurityIdentifier])
 if(!$a.AreAccessRulesProtected -or $rules.Count -ne 1 -or $a.GetOwner([System.Security.Principal.SecurityIdentifier]).Value -ne $sid.Value){throw 'ACL'}
 $r=$rules[0];$flags=[System.Security.AccessControl.InheritanceFlags]::ContainerInherit -bor [System.Security.AccessControl.InheritanceFlags]::ObjectInherit
 if($r.IdentityReference.Value -ne $sid.Value -or $r.IsInherited -or $r.AccessControlType -ne [System.Security.AccessControl.AccessControlType]::Allow -or ($r.FileSystemRights -band [System.Security.AccessControl.FileSystemRights]::FullControl) -ne [System.Security.AccessControl.FileSystemRights]::FullControl -or ($r.InheritanceFlags -band $flags) -ne $flags){throw 'ACL'}
 [Console]::Out.Write('{"status":"PRIVATE_SOURCE_DACL_ACCEPTED"}')
}catch{[Console]::Out.Write('{"status":"PRIVATE_SOURCE_DACL_REJECTED"}');exit 2}
'''.replace('__PATH__', str(BLOBS).replace("'", "''"))
    encoded = base64.b64encode(script.encode('utf-16-le')).decode()
    result = subprocess.run([r'C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe', '-NoProfile', '-NonInteractive', '-EncodedCommand', encoded], capture_output=True, timeout=30, creationflags=subprocess.CREATE_NO_WINDOW)
    require(result.returncode == 0 and not result.stderr and json.loads(result.stdout.decode('utf-8-sig')) == {'status': 'PRIVATE_SOURCE_DACL_ACCEPTED'}, 'PRIVATE_SOURCE_DACL_REJECTED')


def preflight():
    require(os.name == 'nt' and file_sha(EXTRACT_HELPER) == EXTRACT_HELPER_SHA, 'EXTRACT_HELPER_FRAME_CHANGED')
    extractor = import_file('author_avatar_extractor_contract', EXTRACT_HELPER)
    helper = extractor.guard
    helper.path_guard(BLOBS, False)
    protected_directory_gate()
    metadata = extractor.select_metadata()
    transfer_raw = helper.read_private(BLOBS / 'transfer.private.json', 1024 ** 2)
    receipt_raw = helper.read_private(BLOBS / 'extraction-receipt.json', 1024 ** 2)
    proof_raw = helper.read_private(BLOBS / 'extraction-authenticated.json', 1024 ** 2)
    transfer, receipt, proof = map(json.loads, [transfer_raw, receipt_raw, proof_raw])
    require(transfer['protocol'] == 'birdtie-author-avatar-extraction-transfer-v1'
            and transfer['files'] == metadata['files']
            and transfer['inputHashes'] == metadata['inputHashes'], 'EXACT_SELECTION_MANIFEST_CHANGED')
    require(receipt['status'] == 'AUTHENTICATED_AUTHOR_AVATAR_EXTRACTION_COMPLETE'
            and receipt['accepted'] is True and receipt['helperSHA256'] == EXTRACT_HELPER_SHA
            and receipt['extractorSHA256'] == file_sha(extractor.EXTRACTOR)
            and receipt['transferManifestSHA256'] == hashlib.sha256(transfer_raw).hexdigest(), 'EXTRACTION_RECEIPT_BINDING')
    require(all(receipt[k] is True for k in ['wholeGCMAuthenticated', 'wholePlaintextHashAndLengthMatched',
            'exactObjectHashesMatched', 'currentUserOnlyDirectory', 'sourceInputsUnchanged']), 'EXTRACTION_ACCEPTANCE_REQUIRED')
    require(receipt['sourceWrites'] == receipt['databaseWrites'] == receipt['urlFetches'] == 0
            and receipt['mediaPublication'] is False and receipt['pixelDecode'] == 'NOT_RUN', 'EXTRACTION_PURPOSE_GUARD')
    require(proof['status'] == 'AUTHENTICATED_AUTHOR_AVATAR_ORIGINALS' and proof['accepted'] is True
            and proof['aesGcmAuthenticated'] is True and proof['ciphertextSHA256'] == helper.CIPHER_SHA
            and proof['plaintextSHA256'] == metadata['archivePlaintextSHA256']
            and proof['archiveBytes'] == metadata['archiveBytes'], 'WHOLE_ARCHIVE_AUTHENTICATION_REQUIRED')
    require(proof['sourceWrites'] == proof['databaseWrites'] == 0
            and proof['publicationRights'] == 'NOT_GRANTED' and proof['pixelDecode'] == 'NOT_RUN', 'AUTHENTICATION_PURPOSE_GUARD')
    require(transfer['selectedAssets'] == receipt['selectedAssets'] == proof['selectedAssets'] == 6
            and proof['privateImages'] == 6 and proof['systemArtworkArchives'] == 0, 'EXACT_AVATAR_COUNTS_REQUIRED')
    require(receipt['blobFiles'] == proof['extractedFiles'] == len(metadata['files']) == 6
            and receipt['selectedOriginalBytes'] == sum(v['bytes'] for v in metadata['files'].values()) == 14140782,
            'EXACT_AVATAR_BLOB_COUNTS_REQUIRED')
    expected = set(metadata['files']) | {'transfer.private.json', 'extraction-authenticated.json', 'extraction-receipt.json'}
    require({p.name for p in BLOBS.iterdir()} == expected, 'UNEXPECTED_OR_UNACCEPTED_OUTPUT')
    for key in ['selectionProjectionSHA256', 'archivePlaintextSHA256', 'archiveBytes']:
        require(transfer[key] == metadata[key], 'SELECTION_SOURCE_BINDING')
    require(metadata['inputHashes']['seal'] == SEAL_SHA
            and transfer['authenticationProofSHA256'] == hashlib.sha256(proof_raw).hexdigest(), 'MATCHER_AND_PROOF_BINDING')
    local = {name: BLOBS / name for name in transfer['files']}
    local.update({name: MATCHED / name for name in ['matched.private.json', 'authenticated-seal.json']})
    files = dict(transfer['files'])
    for name in ['matched.private.json', 'authenticated-seal.json']:
        helper.path_guard(local[name])
        files[name] = {'bytes': local[name].stat().st_size, 'sha256': file_sha(local[name])}
    require(files['matched.private.json']['sha256'] == metadata['inputHashes']['matched']
            and files['authenticated-seal.json']['sha256'] == SEAL_SHA, 'ORIGINAL_MATCHER_FRAME_CHANGED')
    frames = {}
    for name, path in local.items():
        helper.path_guard(path)
        info = path.stat()
        require(info.st_size == files[name]['bytes'] and file_sha(path) == files[name]['sha256'], 'SELECTED_INPUT_HASH_SIZE_CHANGED')
        frames[name] = (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns)
    payload = {key: transfer[key] for key in ['selectedAssets', 'selectionProjectionSHA256',
               'authenticationProofSHA256', 'archivePlaintextSHA256', 'archiveBytes']}
    payload.update(privateImages=6, systemArtworkArchives=0,
                   sourceFileSHA256=metadata['inputHashes']['source'], matcherFileSHA256=metadata['inputHashes']['matched'],
                   matcherSealSHA256=SEAL_SHA)
    raw_tar = 1024 + sum(512 + ((v['bytes'] + 511) // 512) * 512 for v in files.values())
    payload.update(protocol=PROTOCOL, files=files, sourceAuthenticated=True,
                   extractionReceiptSHA256=hashlib.sha256(receipt_raw).hexdigest(),
                   transferManifestSHA256=hashlib.sha256(transfer_raw).hexdigest(),
                   tarBytes=((raw_tar + 10239) // 10240) * 10240)
    paths = list(metadata['inputPaths'].values()) + [Path(__file__), EXTRACT_HELPER, extractor.EXTRACTOR,
            extractor.HELPER, BLOBS / 'transfer.private.json', BLOBS / 'extraction-receipt.json', BLOBS / 'extraction-authenticated.json']
    proof_frames = {path: file_sha(path) for path in paths}
    return helper, local, frames, proof_frames, payload


class VerifiedReader:
    def __init__(self, source):
        self.source = source
        self.hash = hashlib.sha256()
        self.count = 0

    def read(self, n):
        chunk = self.source.read(n)
        self.hash.update(chunk)
        self.count += len(chunk)
        return chunk


def drain(stream, keep):
    result = {'bytes': 0, 'data': bytearray(), 'overflow': False}
    def run():
        while chunk := stream.read(4096):
            result['bytes'] += len(chunk)
            if keep and len(result['data']) + len(chunk) <= keep:
                result['data'].extend(chunk)
            elif keep:
                result['overflow'] = True
        stream.close()
    thread = threading.Thread(target=run, daemon=True)
    thread.start()
    return result, thread


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute', action='store_true')
    parser.add_argument('--evidence-name', default='transfer01')
    args = parser.parse_args()
    require(re.fullmatch(r'transfer[a-zA-Z0-9_-]{1,40}', args.evidence_name), 'INVALID_EVIDENCE_LABEL')
    if not args.execute:
        print(json.dumps({'status': 'PLAN_ONLY', 'execution': 'NOT_RUN', 'selectedAssets': 6, 'privateImages': 6, 'systemArtworkArchives': 0, 'blobFiles': 6, 'metadataFiles': 2, 'totalFiles': 8, 'transport': 'EXACT_FILES_TAR_OVER_EXISTING_STRICT_SSH', 'remoteDestination': 'NEW_ROOT_ONLY_PRIVATE_AUTHOR_AVATAR_DIRECTORY', 'originalInputsChanged': False, 'sourceWrites': 0, 'databaseWrites': 0, 'remoteRequests': 0, 'mediaPublication': False, 'remoteScriptSHA256': hashlib.sha256(REMOTE.encode()).hexdigest()}))
        return 0
    out = ROOT / 'docs/testing/evidence/content-map-refinement-20261016/backend-build28' / args.evidence_name
    require(not out.exists(), 'EVIDENCE_ALREADY_EXISTS')
    out.mkdir()
    stage, proc, timer = 'LOCAL_AUTHENTICATED_PREFLIGHT', None, None
    result = None
    try:
        helper, local, frames, proof_frames, payload = preflight()
        deploy = import_file('reference167_strict_deploy', ROOT / 'scripts/deploy_birdtie_hangzhou.py')
        bootstrap = "import base64;exec(base64.b64decode('" + base64.b64encode(REMOTE.encode()).decode() + "'))"
        command = deploy.SSH + ['sudo -n python3 -c ' + shlex.quote(bootstrap)]
        stage = 'REMOTE_NEW_DIRECTORY_TRANSFER'
        proc = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, creationflags=subprocess.CREATE_NO_WINDOW)
        stdout, stdout_thread = drain(proc.stdout, 65536)
        stderr, stderr_thread = drain(proc.stderr, 0)
        timer = threading.Timer(600, proc.kill)
        timer.daemon = True
        timer.start()
        proc.stdin.write(json_bytes(payload) + b'\n')
        with tarfile.open(fileobj=proc.stdin, mode='w|', format=tarfile.USTAR_FORMAT) as archive:
            for name, path in sorted(local.items()):
                helper.path_guard(path)
                with path.open('rb') as source:
                    current = os.fstat(source.fileno())
                    frame = (current.st_dev, current.st_ino, current.st_size, current.st_mtime_ns)
                    require(frame == frames[name], 'LOCAL_FILE_FRAME_CHANGED')
                    info = tarfile.TarInfo(name)
                    info.size, info.mode = current.st_size, 0o600
                    reader = VerifiedReader(source)
                    archive.addfile(info, reader)
                    after = os.fstat(source.fileno())
                    require((after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns) == frame and reader.count == payload['files'][name]['bytes'] and reader.hash.hexdigest() == payload['files'][name]['sha256'], 'LOCAL_TRANSFER_FRAME_CHANGED')
        # Remote emits no accepted marker without this post-stream proof ACK.
        stage = 'LOCAL_SOURCE_FINAL_RECHECK'
        require(all(file_sha(path) == expected for path, expected in proof_frames.items()), 'ORIGINAL_SOURCE_FRAME_CHANGED')
        for name, path in local.items():
            current = path.stat()
            require((current.st_dev, current.st_ino, current.st_size, current.st_mtime_ns) == frames[name] and file_sha(path) == payload['files'][name]['sha256'], 'LOCAL_FILE_FRAME_CHANGED')
        proc.stdin.write(json_bytes({'transportComplete': True, 'payloadSHA256': hashlib.sha256(json_bytes(payload)).hexdigest(), 'sourceInputsUnchanged': True}) + b'\n')
        proc.stdin.close()
        stage = 'REMOTE_FINAL_RECEIPT'
        code = proc.wait(timeout=60)
        stdout_thread.join(timeout=5)
        stderr_thread.join(timeout=5)
        require(not stdout_thread.is_alive() and not stderr_thread.is_alive() and not stdout['overflow'], 'REMOTE_OUTPUT_BOUND_REJECTED')
        rows = [json.loads(line) for line in bytes(stdout['data']).splitlines() if line.strip()]
        require(len(rows) == 1, 'REMOTE_RECEIPT_SHAPE_REJECTED')
        result = rows[0]
        write_json(out / 'stdout.json', result)
        write_json(out / 'stderr-summary.json', {'bytes': stderr['bytes'], 'contentPersisted': False})
        require(code == 0 and stderr['bytes'] == 0 and result['status'] == 'PRIVATE_AUTHOR_AVATAR_TRANSFER_COMPLETE' and result['files'] == 8 and result['blobFiles'] == 6 and result['bytes'] == sum(v['bytes'] for v in payload['files'].values()) and result['filesVerified'] is True and result['sourceAuthenticated'] is True and result['sourceInputsUnchanged'] is True, 'REMOTE_TRANSFER_NOT_ACCEPTED')
        receipt = {'status': 'PRIVATE_AUTHOR_AVATAR_TRANSFER_ACCEPTED', 'exitCode': code, 'helperSHA256': file_sha(Path(__file__)), 'remoteScriptSHA256': hashlib.sha256(REMOTE.encode()).hexdigest(), 'result': result, 'sourceWrites': 0, 'databaseWrites': 0, 'importExecuted': False, 'privateValuesOmitted': True}
        write_json(out / 'receipt.json', receipt)
        print(json.dumps(receipt))
        return 0
    except Exception as error:
        failure = {'status': 'UNKNOWN_REQUIRES_REMOTE_DIRECTORY_INSPECTION' if proc else 'PREFLIGHT_REJECTED_NO_REMOTE_REQUEST', 'stage': stage, 'code': str(error) if isinstance(error, Guard) else 'CONTROLLED_TRANSFER_FAILURE', 'errorType': type(error).__name__, 'remoteReceipt': result, 'sourceWrites': 0, 'databaseWrites': 0, 'importExecuted': False, 'privateValuesOmitted': True, 'automaticRetry': False}
        write_json(out / 'receipt.json', failure)
        print(json.dumps(failure))
        return 2
    finally:
        if timer:
            timer.cancel()
        if proc and proc.poll() is None:
            if proc.stdin:
                try:
                    proc.stdin.close()
                except Exception:
                    pass
            proc.kill()
            proc.wait(timeout=15)


if __name__ == '__main__':
    raise SystemExit(main())
