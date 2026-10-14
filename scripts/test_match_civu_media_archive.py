import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

from match_civu_media_archive import InvalidArchive, load_private_source, match_archive

ID = '11111111-1111-4111-8111-111111111111'
OWNER = '22222222-2222-4222-8222-222222222222'
PNG = b'\x89PNG\r\n\x1a\n' + b'synthetic-not-a-decoded-photo'


def asset(**updates):
    return dict(id=ID, owner_id=OWNER, storage_key='private/a.png',
                content_type='image/png', size_bytes=len(PNG), status='ready', **updates)


def tar(entries):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w') as target:
        for name, payload, kind in entries:
            info = tarfile.TarInfo(name)
            info.type = kind
            info.size = len(payload) if kind == tarfile.REGTYPE else 0
            if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE):
                info.linkname = 'outside'
            target.addfile(info, io.BytesIO(payload) if info.size else None)
    return stream.getvalue()


class MatcherTests(unittest.TestCase):
    def match(self, rows, entries):
        raw = tar(entries)
        return match_archive(rows, io.BytesIO(raw), key_prefix='media'), raw

    def test_exact_original_and_motion_integrity_is_not_rights_or_scan(self):
        row = asset(motion_storage_key='private/m.mov', motion_content_type='video/quicktime', motion_size_bytes=12)
        result, raw = self.match([row], [('media/private/a.png', PNG, tarfile.REGTYPE),
                                       ('media/private/m.mov', b'\0\0\0\x0cftypqt  ', tarfile.REGTYPE)])
        self.assertEqual(result['summary']['matched'], 2)
        self.assertEqual(result['archivePlaintextSHA256'], hashlib.sha256(raw).hexdigest())
        self.assertEqual(result['streamAuthentication'], 'NOT_VERIFIED')
        self.assertEqual(result['publicationRights'], 'NOT_AUTHORIZED')
        self.assertEqual(result['scanner'], 'NOT_RUN')
        self.assertEqual(result['extractedFiles'], 0)
        self.assertIn('NOT_SCANNED', result['selectors'][0]['reasons'])

    def test_missing_or_ambiguous_keys_never_guess_uuid_filenames(self):
        a = asset()
        b = dict(a, id='33333333-3333-4333-8333-333333333333')
        result, _ = self.match([a, b], [('media/private/a.png', PNG, tarfile.REGTYPE)])
        self.assertEqual(result['summary']['matched'], 0)
        self.assertEqual(result['summary']['reasons']['DUPLICATE_STORAGE_KEY'], 2)
        result, _ = self.match([a], [('media/'+ID+'.png', PNG, tarfile.REGTYPE)])
        self.assertIn('ARCHIVE_OBJECT_MISSING', result['selectors'][0]['reasons'])

    def test_size_mime_pending_and_owner_conditions_remain_separate(self):
        row = dict(asset(), owner_id=None, status='pending', size_bytes=1, content_type='image/jpeg')
        result, _ = self.match([row], [('media/private/a.png', PNG, tarfile.REGTYPE)])
        self.assertFalse(result['selectors'][0]['matched'])
        self.assertTrue({'OWNER_MISSING', 'SOURCE_NOT_READY', 'SIZE_MISMATCH', 'MIME_MISMATCH'} <= set(result['selectors'][0]['reasons']))

    def test_entire_archive_rejects_link_duplicate_and_traversal_even_unselected(self):
        for entries in [ [('media/x', b'', tarfile.SYMTYPE)],
                         [('media/x', b'', tarfile.LNKTYPE)],
                         [('../secret', PNG, tarfile.REGTYPE)],
                         [('media/x', PNG, tarfile.REGTYPE)] * 2 ]:
            with self.subTest(entries=entries[0][0]), self.assertRaises(InvalidArchive):
                self.match([], entries)

    def test_unsafe_source_key_is_deferred_without_reading_external_path(self):
        for key in ('', '../a', '/a', 'https://x/a', 'a\\b', 'a//b'):
            row = dict(asset(), storage_key=key)
            result, _ = self.match([row], [])
            self.assertIn('KEY_MISSING_OR_UNSAFE', result['selectors'][0]['reasons'])

    def test_member_and_stream_budgets_fail_closed(self):
        raw = tar([('media/a', PNG, tarfile.REGTYPE)])
        for options in ({'max_members': 0}, {'max_bytes': 10}, {'max_object_bytes': 1}):
            with self.subTest(options=options), self.assertRaises(InvalidArchive):
                match_archive([], io.BytesIO(raw), key_prefix='media', **options)

    def test_private_source_hash_and_snapshot_are_required_before_media_read(self):
        from extract_civu_hangzhou import TABLE_SPECS, preserve
        tables = {name: [] for name in TABLE_SPECS}
        tables['media_assets'] = [asset()]
        source, manifest, _ = preserve({'observedAt': 'synthetic', 'transactionSnapshot': 'synthetic'}, tables)
        with tempfile.TemporaryDirectory() as directory:
            p = Path(directory) / 'source.json'
            m = Path(directory) / 'manifest.json'
            p.write_bytes(source)
            m.write_bytes(manifest)
            parsed, _ = load_private_source(p)
            self.assertEqual(parsed['tables']['media_assets'][0]['id'], ID)
            p.write_bytes(source + b' ')
            with self.assertRaisesRegex(InvalidArchive, 'SOURCE_RECEIPT_MISMATCH'):
                load_private_source(p)
            # Even matching file SHA cannot substitute a changed snapshot body.
            altered = json.loads(source)
            altered['tables']['media_assets'][0]['owner_id'] = None
            raw = json.dumps(altered).encode()
            p.write_bytes(raw)
            receipt = json.loads(manifest)
            receipt['sourceFileSha256'] = hashlib.sha256(raw).hexdigest()
            m.write_text(json.dumps(receipt))
            with self.assertRaisesRegex(InvalidArchive, 'SOURCE_SNAPSHOT_MISMATCH'):
                load_private_source(p)


if __name__ == '__main__':
    unittest.main()
