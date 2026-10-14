import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import prepare_civu_collections as preparation

OWNER='a1100000-0000-4000-8000-000000000001'
ACCOUNT='a2200000-0000-4000-8000-000000000001'
SOURCE='b1100000-0000-4000-8000-000000000001'
PLACE1='c1100000-0000-4000-8000-000000000001'
PLACE2='c1100000-0000-4000-8000-000000000002'
TARGET1='c2200000-0000-4000-8000-000000000001'
TARGET2='c2200000-0000-4000-8000-000000000002'

def selected():
    return {'schemaVersion':preparation.SCHEMA,'sourceInstanceTag':'synthetic-fixture',
      'sourceBackupSha256':'1'*64,'sourceSchemaSha256':'2'*64,
      'accountMappings':{OWNER:ACCOUNT},
      'placeMappings':{PLACE1:{'id':TARGET1,'cityId':'test-city'},PLACE2:{'id':TARGET2,'cityId':'test-city'}},
      'collections':[{'kind':'theme_map','id':SOURCE,'ownerId':OWNER,'revision':3,
        'status':'active','visibility':'public','cityId':'test-city','title':'合成主题地图',
        'summary':'合成待确认文字','entries':[{'placeId':PLACE2,'position':9},{'placeId':PLACE1,'position':2}]}]}

class PreparationTests(unittest.TestCase):
    def test_current_native_inputs_order_private_and_no_legacy_rights(self):
        data=selected(); original=copy.deepcopy(data)
        result=preparation.prepare(data); self.assertEqual(data,original)
        row=result['plans'][0]; self.assertEqual(row['targetCollectionId'],None)
        self.assertEqual(row['publicationState'],'private/draft')
        self.assertEqual(row['state'],'NEEDS_OWNER_AND_CURRENT_SOURCE_CONFIRMATION')
        self.assertEqual(row['candidateInput']['entries'],[
          {'placeId':TARGET1,'note':'','locationVisibility':'hidden'},
          {'placeId':TARGET2,'note':'','locationVisibility':'hidden'}])
        self.assertEqual(set(row['candidateInput']),{'operationId','kind','cityId','title','summary','entries'})
        self.assertEqual(result['networkRequests'],0); self.assertEqual(result['databaseChanges'],0)
        self.assertEqual(result['ownerRights'],'NOT_RUN'); self.assertEqual(result['sourcePreservation'],'NOT_RUN')

    def test_stable_original_proposal_content_change_detected_not_new_operation(self):
        data=selected(); first=preparation.prepare(data)
        self.assertEqual(preparation.canonical(first),preparation.canonical(preparation.prepare(data)))
        data['collections'][0]['title']='本人修改的合成标题'; data['collections'][0]['revision']+=1
        next=preparation.prepare(data)
        self.assertEqual(first['plans'][0]['candidateInput']['operationId'],next['plans'][0]['candidateInput']['operationId'])
        self.assertNotEqual(first['plans'][0]['candidateSha256'],next['plans'][0]['candidateSha256'])
        self.assertNotEqual(first['plans'][0]['sourceSha256'],next['plans'][0]['sourceSha256'])
        data['sourceInstanceTag']='other-synthetic-source'
        self.assertNotEqual(first['plans'][0]['candidateInput']['operationId'],preparation.prepare(data)['plans'][0]['candidateInput']['operationId'])

    def test_unmapped_owner_place_city_inactive_are_deferred_not_fake_ready(self):
        for mutation,reason in [(lambda d:d['accountMappings'].clear(),'OWNER_MAPPING_MISSING'),
          (lambda d:d['placeMappings'].pop(PLACE1),'PLACE_MAPPING_MISSING'),
          (lambda d:d['placeMappings'][PLACE1].update(cityId='different-city'),'CITY_MAPPING_MISMATCH'),
          (lambda d:d['collections'][0].update(status='removed'),'SOURCE_NOT_ACTIVE')]:
            with self.subTest(reason=reason):
                data=selected(); mutation(data); result=preparation.prepare(data)
                self.assertEqual(result['plans'],[]); self.assertIn(reason,result['deferred'][0]['reasons'])

    def test_source_duplicates_and_target_merge_need_review(self):
        data=selected(); data['collections'].append(copy.deepcopy(data['collections'][0]))
        with self.assertRaisesRegex(preparation.InvalidSelection,'DUPLICATE_SOURCE'):preparation.prepare(data)
        data=selected(); data['collections'][0]['entries'][1]['position']=9
        with self.assertRaisesRegex(preparation.InvalidSelection,'DUPLICATE_ENTRY'):preparation.prepare(data)
        data=selected(); data['placeMappings'][PLACE2]['id']=TARGET1
        result=preparation.prepare(data); self.assertEqual(result['plans'],[])
        self.assertIn('PLACE_MERGE_REQUIRES_REVIEW',result['deferred'][0]['reasons'])

    def test_closed_inputs_reject_sessions_legacy_media_author_notes_and_invalid_unicode(self):
        for key in ('authorization','password_hash','sessions','approval','cover_url','creator_note'):
            data=selected(); data['collections'][0][key]='UNAUTHORIZED_FIELD'
            with self.subTest(key=key), self.assertRaisesRegex(preparation.InvalidSelection,'CLOSED_FIELDS'):
                preparation.prepare(data)
        data=selected(); data['collections'][0]['title']='\ud800'
        with self.assertRaisesRegex(preparation.InvalidSelection,'INVALID_TEXT'):preparation.prepare(data)
        data=selected(); data['collections'][0]['entries'][0]['position']=True
        with self.assertRaisesRegex(preparation.InvalidSelection,'ENTRY_ORDER'):preparation.prepare(data)

    def test_actual_cli_writes_new_local_plan_once_preserves_input_and_existing_output(self):
        with tempfile.TemporaryDirectory(prefix='birdtie-civu-preparation-') as directory:
            source=Path(directory)/'selected.json'; out=Path(directory)/'prepared.json'
            source.write_bytes(preparation.canonical(selected())); old_source=hashlib.sha256(source.read_bytes()).hexdigest()
            cmd=[sys.executable,str(Path(preparation.__file__)),'--selected',str(source),'--output',str(out)]
            first=subprocess.run(cmd,capture_output=True); self.assertEqual(first.returncode,0,first.stderr)
            old_output=out.read_bytes(); self.assertIn(b'PREPARED_ONLY',first.stdout)
            again=subprocess.run(cmd,capture_output=True); self.assertEqual(again.returncode,2)
            self.assertEqual(out.read_bytes(),old_output); self.assertEqual(hashlib.sha256(source.read_bytes()).hexdigest(),old_source)
            self.assertNotIn(OWNER.encode(),first.stdout); self.assertNotIn(SOURCE.encode(),first.stdout)
            apply=subprocess.run([*cmd,'--apply'],capture_output=True); self.assertEqual(apply.returncode,2)
            self.assertEqual(out.read_bytes(),old_output)

    def test_actual_cli_duplicate_json_key_rejected_before_output_and_no_raw_text_logged(self):
        with tempfile.TemporaryDirectory(prefix='birdtie-civu-preparation-') as directory:
            source=Path(directory)/'selected.json'; out=Path(directory)/'prepared.json'
            source.write_bytes(b'{"private":"SECRET_VALUE","private":"SECRET_VALUE"}')
            cmd=[sys.executable,str(Path(preparation.__file__)),'--selected',str(source),'--output',str(out)]
            result=subprocess.run(cmd,capture_output=True); self.assertEqual(result.returncode,2)
            self.assertIn(b'DUPLICATE_KEY',result.stdout); self.assertNotIn(b'SECRET_VALUE',result.stdout+result.stderr)
            self.assertFalse(out.exists())

if __name__=='__main__': unittest.main()
