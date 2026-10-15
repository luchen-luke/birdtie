import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:birdtie_client/src/content/private_moment_video_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const owner = '15900000-0000-4000-8000-000000000001',
    mid = '15900000-0000-4000-8000-000000000002',
    vid = '15900000-0000-4000-8000-000000000003';
Map<String, Object> video() => {
  'schemaVersion': 'private-moment-video-v1',
  'origin': 'LEGACY_PRIVATE_IMPORT',
  'id': vid,
  'momentId': mid,
  'ownerAccountId': owner,
  'status': 'ready_private',
  'revision': 1,
  'position': 0,
  'mimeType': 'video/mp4',
  'byteSize': 3,
  'sha256': sha256.convert([1, 2, 3]).toString(),
  'durationMs': 104824,
  'width': 720,
  'height': 1280,
  'antivirusScan': 'NOT_RUN',
  'privacyRisk': 'UNKNOWN',
};
void main() {
  test(
    'Full-length private video read verifies bytes and rejects late Session response',
    () async {
      var token = 'Bearer original';
      final changes = ValueNotifier(0);
      Completer<http.Response>? pending;
      final client = MockClient((r) async {
        expect(r.headers['Authorization'], token);
        if (r.url.path.endsWith('/content')) {
          pending = Completer();
          return pending!.future;
        }
        return http.Response(
          jsonEncode({
            'data': {
              'items': [video()],
            },
          }),
          200,
        );
      });
      final c = PrivateMomentVideoController(
        momentID: mid,
        authorizationHeader: () => token,
        ownerID: () => owner,
        sourceCurrent: () => true,
        apiBaseUrl: 'https://synthetic.invalid',
        identityChanges: changes,
        client: client,
      );
      await c.load();
      expect(c.items.single.durationMS, 104824);
      final read = c.readContent(c.items.single);
      await Future<void>.delayed(Duration.zero);
      token = 'Bearer another';
      changes.value++;
      pending!.complete(
        http.Response.bytes(
          [1, 2, 3],
          200,
          headers: {'content-type': 'video/mp4'},
        ),
      );
      expect(await read, isNull);
      expect(c.items, isEmpty);
      expect(c.current, isFalse);
      c.dispose();
      client.close();
      changes.dispose();
    },
  );
  test(
    'Unknown video delete blocks repeat and recovers by reading current list',
    () async {
      var deleted = false, deletes = 0;
      final changes = ValueNotifier(0);
      final client = MockClient((r) async {
        if (r.method == 'DELETE') {
          deletes++;
          deleted = true;
          throw const FormatException('synthetic connection loss');
        }
        return http.Response(
          jsonEncode({
            'data': {
              'items': deleted ? [] : [video()],
            },
          }),
          200,
        );
      });
      final c = PrivateMomentVideoController(
        momentID: mid,
        authorizationHeader: () => 'Bearer test',
        ownerID: () => owner,
        sourceCurrent: () => true,
        apiBaseUrl: 'https://synthetic.invalid',
        identityChanges: changes,
        client: client,
      );
      await c.load();
      final v = c.items.single;
      await c.remove(v);
      await c.remove(v);
      expect(c.uncertain, isTrue);
      expect(deletes, 1);
      await c.load();
      expect(c.uncertain, isFalse);
      expect(c.items, isEmpty);
      expect(deletes, 1);
      c.dispose();
      client.close();
      changes.dispose();
    },
  );
}
