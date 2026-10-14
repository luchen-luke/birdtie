import 'dart:async';
import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/content/legacy_personal_archive_api.dart';
import 'package:birdtie_client/src/content/legacy_archive_api.dart';

const archiveID = '14900000-0000-4000-8000-000000000001';
const ownerID = '14900000-0000-4000-8000-000000000010';
Map<String, dynamic> summary({bool deleted = false}) => {
  'id': archiveID,
  'kind': 'journey',
  'sourceKey': archiveID,
  'sourceRevision': 1,
  'sourceSha256': 'a' * 64,
  'deleted': deleted,
  'sourceDeletedAt': deleted ? '2026-10-14T00:00:00Z' : null,
  'title': deleted ? '' : '隔离测试旧行程',
  'importedAt': '2026-10-15T00:00:00Z',
};
void main() {
  test(
    'private archive deletion and media boundary remain separate from current content',
    () {
      expect(
        LegacyPersonalDetail.decode({
          ...summary(deleted: true),
          'payload': null,
        }).payload,
        isNull,
      );
      expect(
        () => LegacyPersonalDetail.decode({
          ...summary(deleted: true),
          'payload': {'body': 'should not revive'},
        }),
        throwsFormatException,
      );
      final payload = {
        'metadata': {},
        'stops': [],
        'arrangements': [],
        'notes': [],
        'members': [],
        'media': [
          {'state': 'READY_PUBLIC'},
        ],
        'publications': [],
        'publicationPlaces': [],
      };
      expect(
        () => LegacyPersonalDetail.decode({...summary(), 'payload': payload}),
        throwsFormatException,
      );
    },
  );
  test(
    'page reads original same-origin identity; late response is never released',
    () async {
      String owner = ownerID;
      final response = Completer<http.StreamedResponse>();
      final client = MockClient((request) async {
        expect(request.url.path, '/v1/me/migration-archive-records');
        expect(request.headers['Authorization'], 'Bearer fixture');
        await response.future;
        return http.Response(
          jsonEncode({
            'data': {
              'items': [summary()],
            },
          }),
          200,
        );
      });
      final api = LegacyPersonalArchiveAPI(
        authorizationHeader: () => 'Bearer fixture',
        ownerID: () => owner,
        current: () => true,
        baseUrl: 'https://fixture',
        client: client,
      );
      final value = api.page('journey');
      await Future<void>.delayed(Duration.zero);
      owner = archiveID;
      response.complete(http.StreamedResponse(const Stream.empty(), 200));
      await expectLater(value, throwsA(isA<LegacyArchiveSourceChanged>()));
      api.dispose();
      client.close();
    },
  );
}
