import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _id = '16500000-0000-4000-8000-000000000001';
http.Response _reply(Map<String, Object?> data, {int status = 200}) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);

void main() {
  test(
    'untitled text and media moments use read-only share descriptions',
    () async {
      var payload = <String, Object?>{'id': _id, 'title': '', 'body': ' 正文内容 '};
      var reads = 0;
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer synthetic',
        apiBaseUrl: 'https://unit.invalid',
        client: MockClient((request) async {
          expect(request.method, 'GET');
          expect(request.url.path, '/v1/moments/$_id');
          reads++;
          return _reply(payload);
        }),
      );
      addTearDown(source.dispose);
      expect(await source.shareTargetTitle('moment', _id), '正文内容');
      expect(payload['title'], '');
      payload = {'id': _id, 'title': '', 'body': ''};
      expect(await source.shareTargetTitle('moment', _id), '分享动态');
      payload = {'id': _id, 'title': '', 'body': List.filled(121, '🌍').join()};
      expect(
        await source.shareTargetTitle('moment', _id),
        List.filled(120, '🌍').join(),
      );
      payload = {'id': _id, 'title': ' 原标题 ', 'body': '正文'};
      expect(await source.shareTargetTitle('moment', _id), '原标题');
      expect(reads, 4);
    },
  );

  test(
    'share descriptions keep current denial and non-moment contracts',
    () async {
      var status = 403;
      var payload = <String, Object?>{'id': _id, 'title': '', 'body': '禁止复用'};
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer synthetic',
        apiBaseUrl: 'https://unit.invalid',
        client: MockClient((_) async => _reply(payload, status: status)),
      );
      addTearDown(source.dispose);
      await expectLater(
        source.shareTargetTitle('moment', _id),
        throwsStateError,
      );
      status = 200;
      await expectLater(
        source.shareTargetTitle('activity', _id),
        throwsFormatException,
      );
      payload = {'id': _id, 'title': '', 'body': 1};
      await expectLater(
        source.shareTargetTitle('moment', _id),
        throwsFormatException,
      );
      payload = {
        'id': '16500000-0000-4000-8000-000000000002',
        'title': '',
        'body': '',
      };
      await expectLater(
        source.shareTargetTitle('moment', _id),
        throwsFormatException,
      );
    },
  );

  test(
    'late share description retires after token, identity epoch or disposal',
    () async {
      for (final reason in ['token', 'epoch', 'dispose']) {
        var token = 'Bearer first';
        final changes = ValueNotifier(0);
        final started = Completer<void>();
        final reply = Completer<http.Response>();
        final source = ConnectionSource(
          authorizationHeader: () => token,
          identityChanges: changes,
          apiBaseUrl: 'https://unit.invalid',
          client: MockClient((_) {
            started.complete();
            return reply.future;
          }),
        );
        final future = source.shareTargetTitle('moment', _id);
        await started.future;
        if (reason == 'token') token = 'Bearer second';
        if (reason == 'epoch') changes.value++;
        if (reason == 'dispose') source.dispose();
        final rejected = expectLater(future, throwsStateError);
        reply.complete(_reply({'id': _id, 'title': '原账号标题', 'body': ''}));
        await rejected;
        source.dispose();
        changes.dispose();
      }
    },
  );
}
