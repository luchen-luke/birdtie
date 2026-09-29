import 'dart:convert';

import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('Inbox uses the owner Bearer session and marks an item read', () async {
    final item = {
      'id': '11111111-1111-1111-1111-111111111111',
      'category': 'updates',
      'title': 'Group reviewed',
      'detail': 'Your group is now visible.',
      'resourceType': 'community',
      'resourceId': '22222222-2222-2222-2222-222222222222',
      'createdAt': '2026-09-30T08:00:00Z',
    };
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer test');
      if (request.method == 'GET') {
        expect(request.url.path, '/v1/me/inbox');
        return http.Response(
          jsonEncode({
            'data': [item],
          }),
          200,
        );
      }
      expect(request.method, 'POST');
      expect(
        request.url.path,
        '/v1/me/inbox/11111111-1111-1111-1111-111111111111/read',
      );
      return http.Response(
        jsonEncode({
          'data': {...item, 'readAt': '2026-09-30T08:10:00Z'},
        }),
        200,
      );
    });
    final source = RemoteInboxSource(
      authorizationHeader: () => 'Bearer test',
      client: client,
      apiBaseUrl: 'https://birdtie.example',
    );
    final items = await source.load();
    expect(items.single.title, 'Group reviewed');
    expect(items.single.readAt, isNull);
    final read = await source.markRead(items.single.id);
    expect(read.readAt, isNotNull);
    source.dispose();
  });
}
