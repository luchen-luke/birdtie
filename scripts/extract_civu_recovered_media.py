"""Explicit protected recovery extraction; parent authenticates the whole GCM stream."""
import hashlib,json,re,sys,tarfile
from pathlib import Path

def main():
 source,matched,out=map(Path,sys.argv[1:]);raw=source.read_bytes();m=json.loads(matched.read_bytes())
 assert hashlib.sha256(raw).hexdigest()==m['sourceFileSha256']
 t=json.loads(raw)['tables'];notes={x['id']:x for x in t['notes']if x['deleted_at']is None};assets={x['id']:x for x in t['media_assets']}
 selectors={x['sourceMediaId']:x for x in m['selectors']if x['role']=='original'};wanted={}
 for rel in t['note_media']:
  if rel['note_id']not in notes:continue
  a,v=assets[rel['media_id']],selectors[rel['media_id']]
  if v['matched']:continue
  assert a['owner_id']==notes[rel['note_id']]['author_id']==v['sourceOwnerId'] and a['status']==v['sourceStatus']=='ready' and a['kind']=='image' and a['storage_key']==v['sourceStorageKey']
  reasons=set(v['reasons']);assert len(reasons)==len(v['reasons'])
  jpeg=v['observedMIME']=='image/jpeg' and not v['sourceMIME'] and not a['content_type'] and v['sourceBytes']==a['size_bytes'] and a['size_bytes']in (None,0) and reasons=={'NOT_SCANNED','RIGHTS_NOT_REVIEWED','SOURCE_MIME_UNKNOWN','SOURCE_SIZE_UNKNOWN'}
  heic=v['observedMIME']=='video/mp4' and v['sourceMIME']==a['content_type']=='image/heic' and v['sourceBytes']==a['size_bytes']==v['bytes'] and reasons=={'NOT_SCANNED','RIGHTS_NOT_REVIEWED','MIME_MISMATCH'}
  assert jpeg or heic
  assert v['archiveMember'] and v['archiveMember']not in wanted and 0<v['bytes']<=16*1024**2 and re.fullmatch('[a-f0-9]{64}',v['sha256'])
  wanted[v['archiveMember']]=(v,'jpeg'if jpeg else'heic')
 assert len(wanted)==31
 class Stream:
  def __init__(self):self.sha=hashlib.sha256();self.size=0
  def read(self,n=-1):
   b=sys.stdin.buffer.read(min(n if n>=0 else 1024**2,1024**2));self.sha.update(b);self.size+=len(b)
   assert self.size<=4*1024**3
   return b
 stream=Stream();found=set();seen=set();count=0
 with tarfile.open(fileobj=stream,mode='r|')as archive:
  for member in archive:
   count+=1;assert count<=30000
   name=member.name.rstrip('/')if member.isdir()else member.name
   assert name and not name.startswith('/')and not any(p in ('','.','..')for p in name.split('/'))and '\\'not in name and ':'not in name and name not in seen
   seen.add(name)
   if member.isdir():continue
   assert member.isfile()and not member.issym()and not member.islnk()and not member.sparse
   if name not in wanted:continue
   v,kind=wanted[name];assert member.size==v['bytes']
   raw=archive.extractfile(member).read(v['bytes']+1)
   assert len(raw)==v['bytes']and hashlib.sha256(raw).hexdigest()==v['sha256']
   assert raw.startswith(b'\xff\xd8\xff')if kind=='jpeg'else raw[4:12]==b'ftypheic'
   with(out/(v['sha256']+'.blob')).open('xb')as f:f.write(raw)
   found.add(name)
 while stream.read(1024**2):pass
 assert found==set(wanted)and stream.sha.hexdigest()==m['archivePlaintextSHA256']and stream.size==m['archiveBytes']
 print(json.dumps({'extractedFiles':len(found),'archiveBytes':stream.size,'archivePlaintextSHA256':stream.sha.hexdigest()}))

if __name__=='__main__':
 try:main()
 except Exception:
  print(json.dumps({'status':'REJECTED','reason':'PROTECTED_RECOVERY_EXTRACTION_FAILED','privateValuesOmitted':True}));sys.exit(2)
