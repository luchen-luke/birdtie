"""One opt-in synthetic HTTPS chain; no imported identities or private source data."""
import argparse,copy,hashlib,json,uuid
from datetime import datetime,timezone
from pathlib import Path
from urllib.request import Request,urlopen
from urllib.error import HTTPError

R=Path(r'D:/Project/birdtie');W=Path(__file__).resolve().parent
BASE='https://47.110.154.133'
def uid():return str(uuid.uuid4())
def now():return datetime.now(timezone.utc).isoformat()
def run(name):
    proof=json.loads((R/'docs/testing/evidence/social-remote-migration-20261015/upgrade168-01/receipt.json').read_bytes())
    assert proof['exit_code']==0 and proof['result']['migrations']==168
    out=W/name;out.mkdir(exist_ok=False)
    events=[];tokens=[];created=set();operations=[];actor=None
    result={'status':'FAIL','started':now(),'origin':BASE,'scope':'SYNTHETIC_OWNED_HTTP_CHAIN','civuIdentitiesUsed':0,'sourceWrites':0,'deviceEvidence':False}
    def call(method,path,token=None,value=None,extra=None,expected=(200,201,204)):
        headers={'Content-Type':'application/json'}
        if token:headers['Authorization']='Bearer '+token
        headers.update(extra or {})
        try:
            with urlopen(Request(BASE+path,data=None if value is None else json.dumps(value).encode(),headers=headers,method=method),timeout=25) as response:
                status=response.status;data={} if status==204 else json.load(response)
        except HTTPError as error:
            status=error.code
            try:data=json.load(error)
            except Exception:data={}
        if status not in expected:
            code=data.get('error',{}).get('code','unknown')
            raise RuntimeError('HTTP_'+str(status)+'_'+code)
        return data
    def save_state():
        # Only synthetic IDs and explicitly written test inputs; no token or approval.
        (out/'operations.json').write_text(json.dumps(operations,indent=2)+'\n',encoding='utf-8')
    def login(phone):
        call('POST','/v1/auth/dev-phone/code',value={'phone':phone})
        token=call('POST','/v1/auth/dev-phone/verify',value={'phone':phone,'code':'123456'})['data']['accessToken'];tokens.append(token);return token
    def action(token,family,value):
        prefix='/v1/me/'+family
        preview=call('POST',prefix+'/previews',token,value)['data']
        operations.append({'family':family,'input':copy.deepcopy(value),'status':'PREVIEWED'});save_state()
        current=operations[-1]
        submitted=dict(copy.deepcopy(value),sourceVersion=preview['sourceVersion'],approval=preview['approval'])
        current['status']='UNKNOWN';save_state()
        try:receipt=call('PUT',prefix+'/operations/'+value['operationId'],token,submitted)['data']
        except Exception:
            # Query the original fact after an uncertain response; never make a new ID.
            receipt=call('GET',prefix+'/operations/'+value['operationId'],token)['data']
        assert receipt['status']=='SUCCEEDED'
        assert call('GET',prefix+'/operations/'+value['operationId'],token)['data']==receipt
        assert call('PUT',prefix+'/operations/'+value['operationId'],token,submitted)['data']==receipt
        current.update(status='SUCCEEDED',receipt=receipt);save_state()
        if family=='moment-threads':created.update(m['momentId']for m in receipt['members'])
        elif family=='moment-controls':created.add(receipt['momentId'])
        return receipt
    def material(text,city=None):
        return {'cityId':'','placeId':'','title':'','body':text,'occurredAt':None,'timePrecision':'unknown','locationPrecision':'none','locations':[]if city is None else[{'id':uid(),'kind':'CITY','cityId':city,'placeId':'','precision':'city'}]}
    def item(text,city=None,mid='',revision=0):
        return {'momentId':mid,'expectedRevision':revision,'restore':False,'input':material(text,city),'media':None if mid else[]}
    def edit(items,thread='',version=0,base='',removed=None):
        return {'operationId':uid(),'action':'EDIT','threadId':thread,'baseMomentId':base,'localScope':uid(),'audience':'PUBLIC','expectedVersion':version,'items':items,'removedItems':removed or[]}
    def detail(mid,token=None,expected=(200,)):
        return call('GET','/v1/moments/'+mid,token,extra={'X-Birdtie-Public-Moment-Context':'city-v1','X-Birdtie-Public-Moment-Media':'photo-v1','X-Birdtie-Map-Locations':'locations-v2'},expected=expected)
    try:
        actor=login('+447700900001');other=login('+447700900002')
        cities=call('GET','/v1/cities')['data']
        # Existing catalog metadata only; deterministic selection with no legacy content read.
        ids=[c['id']for c in cities]
        assert 'aberdeen-gb' in ids and len(ids)>1
        first='aberdeen-gb';second=next(v for v in ids if v!=first)
        base_input=material('QA-unified168-original-'+name)
        base=action(actor,'moment-controls',{'operationId':uid(),'action':'CREATE','expectedRevision':0,'input':base_input})
        base=action(actor,'moment-controls',{'operationId':uid(),'action':'PUBLISH','momentId':base['momentId'],'expectedRevision':base['appliedRevision'],'input':base_input})
        mid=base['momentId'];original=detail(mid)['data'];assert original['title']==''and original['body']==base_input['body']
        events.append('STANDALONE_PURE_TEXT_WITHOUT_TITLE_OR_LOCATION_PUBLISHED')
        interaction=call('GET','/v1/moments/'+mid+'/interactions',actor)['data']
        like=action(actor,'moment-interactions',{'operationId':uid(),'action':'LIKE','momentId':mid,'expectedMomentRevision':original['revision'],'expectedLikeVersion':interaction['likeVersion'],'commentId':'','body':''})
        interaction=call('GET','/v1/moments/'+mid+'/interactions',actor)['data']
        comment=action(actor,'moment-interactions',{'operationId':uid(),'action':'COMMENT','momentId':mid,'expectedMomentRevision':original['revision'],'expectedLikeVersion':interaction['likeVersion'],'commentId':'','body':'QA-unified168-self-comment'})
        entries=[item('QA-unified168-edited-'+name,first,mid,base['appliedRevision']),item('QA-unified168-added-'+name,second),item('QA-unified168-third-'+name)]
        creation=edit(entries,base=mid)
        bad=copy.deepcopy(creation);bad['operationId']=uid()
        call('POST','/v1/me/moment-threads/previews',other,bad,expected=(403,404,409))
        call('POST','/v1/me/moment-threads/previews',None,bad,expected=(401,))
        receipt=action(actor,'moment-threads',creation)
        assert receipt['schemaVersion']=='moment-thread-operation-v2'and receipt['rootMomentId']==mid and receipt['members'][0]['momentId']==mid
        thread=receipt['threadId'];members=receipt['members']
        view=call('GET','/v1/moment-threads/'+thread)['data'];assert len(view['members'])==3
        for i,m in enumerate(members):entries[i]['momentId']=m['momentId'];entries[i]['expectedRevision']=m['appliedRevision'];entries[i]['media']=None
        interactions=call('GET','/v1/moments/'+mid+'/interactions?commentId='+comment['commentId'],actor)['data']
        assert any(c['id']==comment['commentId'] for c in interactions['comments'])
        assert call('GET','/v1/me/moment-interactions/operations/'+like['operationId'],actor)['data']==like
        events.append('STABLE_ORIGINAL_ID_COMMENT_LIKE_AND_LINK_WITH_ATOMIC_CROSS_CITY_APPEND')
        reordered=[entries[2],entries[0],entries[1]]
        newer=action(actor,'moment-threads',edit(reordered,thread,receipt['appliedVersion']))
        assert newer['rootMomentId']==mid and newer['members'][0]['momentId']==members[2]['momentId']
        view=call('GET','/v1/moment-threads/'+thread)['data'];assert [m['momentId']for m in view['members']]==[m['momentId']for m in newer['members']]
        for i,m in enumerate(newer['members']):reordered[i]['expectedRevision']=m['appliedRevision']
        survivors=[reordered[0],reordered[2]]
        removed=[{'momentId':mid,'expectedRevision':reordered[1]['expectedRevision']}]
        newer=action(actor,'moment-threads',edit(survivors,thread,newer['appliedVersion'],removed=removed))
        assert newer['rootMomentId']==mid and len(newer['removedMembers'])==1
        detail(mid,expected=(404,))
        call('GET','/v1/moments/'+mid+'/thread',expected=(404,))
        for i,m in enumerate(newer['members']):survivors[i]['expectedRevision']=m['appliedRevision']
        one=action(actor,'moment-threads',edit([survivors[0]],thread,newer['appliedVersion'],removed=[{'momentId':survivors[1]['momentId'],'expectedRevision':survivors[1]['expectedRevision']}]))
        view=call('GET','/v1/moment-threads/'+thread)['data'];assert len(view['members'])==1 and view['rootMomentId'] is None
        detail(one['members'][0]['momentId'])
        events.append('PUBLISHED_REORDER_SINGLE_DELETE_HIDDEN_ROOT_AND_ONE_REMAINING_CURRENT_READ')
        assert call('GET','/v1/me/moment-threads/operations/'+receipt['operationId'],actor)['data']==receipt
        withdrawn=action(actor,'moment-threads',{'operationId':uid(),'action':'WITHDRAW','threadId':thread,'localScope':'','audience':'PUBLIC','expectedVersion':one['appliedVersion'],'items':[]})
        assert len(withdrawn['members'])==1
        call('GET','/v1/moment-threads/'+thread,expected=(404,))
        events.append('ONE_MEMBER_WHOLE_WITHDRAW_AND_OLD_IMMUTABLE_OPERATION_RECOVERY')
        result['status']='PASS'
    except Exception as error:
        result.update(failureType=type(error).__name__,failure=str(error) if isinstance(error,RuntimeError) else 'COMPOSITE_ASSERTION_FAILED')
    finally:
        cleanup=[]
        if actor:
            # Retire only exact IDs created by this bounded synthetic run.
            for mid in created:
                try:
                    current=call('GET','/v1/me/moments/'+mid,actor)['data']
                    if current['status']=='published':
                        action(actor,'moment-controls',{'operationId':uid(),'action':'WITHDRAW','momentId':mid,'expectedRevision':current['revision']})
                    cleanup.append('RETIRED_OR_PRIVATE')
                except Exception:cleanup.append('RECHECK_REQUIRED')
        result['cleanup']=cleanup
        result['sessionsRevoked']=[]
        for token in tokens:
            try:call('POST','/v1/session/logout',token);result['sessionsRevoked'].append(True)
            except Exception:result['sessionsRevoked'].append(False)
        result.update(completed=now(),events=events,newMediaUpload='NOT_RUN_IN_THIS_HTTP_CHAIN',mapPrivacy='NOT_RUN_IN_THIS_HTTP_CHAIN',helperSHA256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest())
        (out/'receipt.json').write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
        print(json.dumps(result))
    return 0 if result['status']=='PASS'and all(result['sessionsRevoked'])and 'RECHECK_REQUIRED'not in cleanup else 1

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--execute',action='store_true');p.add_argument('--name',default='unified168-http01');args=p.parse_args()
    if not args.execute:print('PLAN: synthetic HTTPS compound acceptance after schema168 deployment. No requests made.')
    else:raise SystemExit(run(args.name))
