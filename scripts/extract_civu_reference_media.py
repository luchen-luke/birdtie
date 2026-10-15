"""Exact offline purpose extraction; the caller authenticates the complete GCM stream.

Arguments: protected source.json matched.private.json new-private-output-directory.
Only selected JPEG/PNG originals are copied; no old URL is fetched or published.
"""
import hashlib,json,re,sys,tarfile
from pathlib import Path

def main():
 source,matched,out=map(Path,sys.argv[1:]);raw=source.read_bytes();m=json.loads(matched.read_bytes())
 assert hashlib.sha256(raw).hexdigest()==m['sourceFileSha256']
 t=json.loads(raw)['tables'];assets={a['id']:a for a in t['media_assets']};notes={n['id']:n for n in t['notes']};journeys={j['id']:j for j in t['journeys']}
 assert out.is_absolute()and out.is_dir()and not out.is_symlink()and not any(out.iterdir())
 live={r['media_id']for r in t['note_media']if notes[r['note_id']]['deleted_at']is None}
 selected={r['media_id']for r in t['place_images']if r['status']!='removed'}
 selected|={r['media_id']for r in t['journey_media']if journeys[r['journey_id']]['deleted_at']is None and not r['hidden']}
 urls={}
 for a in assets.values():
  if a['public_url']:urls.setdefault(a['public_url'],set()).add(a['id'])
 for n in notes.values():
  if n['deleted_at']is None:
   matches=urls.get(n['cover_url'],set());assert len(matches)<=1;selected|=matches
 selected-=live
 artwork={g['artwork_media_id']for g in t['geo_areas']if g['artwork_media_id']}
 assert not(selected&artwork);selected|=artwork
 selectors={r['sourceMediaId']:r for r in m['selectors']if r['role']=='original'};wanted={}
 for aid in selected:
  a,s=assets[aid],selectors[aid]
  assert a['status']==s['sourceStatus']=='ready'and a['kind']=='image'and a['owner_id']==(s['sourceOwnerId']or None)
  assert a['storage_key']==s['sourceStorageKey']and s['sourceMIME']==a['content_type']==s['observedMIME']and s['observedMIME']in('image/jpeg','image/png')
  assert s['archiveMember']and s['archiveMember']not in wanted and 0<s['bytes']<=16*1024**2 and re.fullmatch('[a-f0-9]{64}',s['sha256'])
  if not s['matched']:
   assert set(s['reasons'])=={'NOT_SCANNED','RIGHTS_NOT_REVIEWED','SOURCE_SIZE_UNKNOWN'}and len(s['reasons'])==3 and a['size_bytes']in(None,0)and s['sourceBytes']in(None,0)
  else:assert s['sourceBytes']==a['size_bytes']==s['bytes']
  wanted[s['archiveMember']]=s
 class Stream:
  def __init__(self):self.sha=hashlib.sha256();self.size=0
  def read(self,n=-1):
   data=sys.stdin.buffer.read(min(n if n>=0 else 1024**2,1024**2));self.sha.update(data);self.size+=len(data);assert self.size<=4*1024**3;return data
 stream=Stream();found=set();seen=set();blobs=set();members=0
 with tarfile.open(fileobj=stream,mode='r|')as archive:
  for item in archive:
   members+=1;assert members<=30000
   name=item.name.rstrip('/')if item.isdir()else item.name
   assert name and not name.startswith('/')and not any(v in('','.','..')for v in name.split('/'))and '\\'not in name and ':'not in name and name not in seen
   seen.add(name)
   if item.isdir():continue
   assert item.isfile()and not item.issym()and not item.islnk()and not item.sparse
   if name not in wanted:continue
   s=wanted[name];assert item.size==s['bytes'];data=archive.extractfile(item).read(s['bytes']+1)
   assert len(data)==s['bytes']and hashlib.sha256(data).hexdigest()==s['sha256']
   assert data.startswith(b'\xff\xd8\xff')if s['observedMIME']=='image/jpeg'else data.startswith(b'\x89PNG\r\n\x1a\n')
   target=out/(s['sha256']+'.blob')
   if s['sha256']not in blobs:
    with target.open('xb')as f:f.write(data)
    target.chmod(0o600);blobs.add(s['sha256'])
   found.add(name)
 while stream.read(1024**2):pass
 assert found==set(wanted)and stream.sha.hexdigest()==m['archivePlaintextSHA256']and stream.size==m['archiveBytes']
 print(json.dumps({'status':'PURPOSE_ORIGINALS_EXTRACTED_NOT_IMPORTED','selectedAssets':len(selected),'privateImages':len(selected-artwork),'systemArtworkArchives':len(artwork),'blobFiles':len(blobs),'archiveBytes':stream.size,'archivePlaintextSHA256':stream.sha.hexdigest(),'sourceWrites':0,'databaseWrites':0,'mediaPublication':False}))

if __name__=='__main__':
 try:main()
 except Exception:
  print(json.dumps({'status':'REJECTED','reason':'PROTECTED_REFERENCE_EXTRACTION_FAILED','privateValuesOmitted':True}));sys.exit(2)
