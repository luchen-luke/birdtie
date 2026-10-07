import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/now_context_query_api.dart';
import 'package:birdtie_client/src/workspace/now_context_query_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'now_context_query_api_test.dart' show onlineContextID;

http.Response _jsonResponse(String body, int status) => http.Response(
  body,
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
void main() {
  test(
    'only native declared nodes can be selected and retirement clears old choice',
    () async {
      final c = NowContextQueryController(
        api: NowContextQueryApi(
          authorizationHeader: () => 'Bearer own',
          client: MockClient(
            (_) async => _jsonResponse(
              jsonEncode({
                'data': [
                  {'id': onlineContextID, 'label': '阅读', 'type': 'ONLINE'},
                ],
              }),
              200,
            ),
          ),
          apiBaseUrl: 'http://localhost',
        ),
      );
      await c.load();
      expect(c.contexts.length, 1);
      c.select(c.contexts.single);
      expect(c.selected!.id, onlineContextID);
      expect(
        () => c.select(const NowOnlineContext(id: 'foreign', label: '推断')),
        throwsFormatException,
      );
      c.retire();
      expect(c.selected, isNull);
      expect(c.contexts, isEmpty);
      c.dispose();
    },
  );
  test('late declared list after retire cannot restore A choice', () async {
    final reply = Completer<http.Response>();
    final c = NowContextQueryController(
      api: NowContextQueryApi(
        authorizationHeader: () => 'Bearer own',
        client: MockClient((_) => reply.future),
        apiBaseUrl: 'http://localhost',
      ),
    );
    final pending = c.load();
    c.retire();
    reply.complete(
      _jsonResponse(
        jsonEncode({
          'data': [
            {'id': onlineContextID, 'label': '阅读', 'type': 'ONLINE'},
          ],
        }),
        200,
      ),
    );
    await pending;
    expect(c.contexts, isEmpty);
    expect(c.selected, isNull);
    c.dispose();
  });
  test(
    'permission withdrawal clears context and gives Chinese recoverable error',
    () async {
      final c = NowContextQueryController(
        api: NowContextQueryApi(
          authorizationHeader: () => 'Bearer own',
          client: MockClient((_) async => _jsonResponse('{}', 403)),
          apiBaseUrl: 'http://localhost',
        ),
      );
      await c.load();
      expect(c.error, contains('无法读取'));
      expect(c.contexts, isEmpty);
      c.dispose();
    },
  );
}
