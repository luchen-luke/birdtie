import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'package:birdtie_client/src/workspace/saved_items.dart';

void main() {
  test('Saved loads, adds and removes an owner bookmark', () async {
    var present = false;
    String? authorization = 'Bearer test-session';
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], authorization);
      if (request.method == 'GET') {
        return http.Response(
          jsonEncode({
            'data': present
                ? [
                    {
                      'id': 'saved-1',
                      'kind': 'place',
                      'targetId': 'place-1',
                      'title': 'Town House',
                      'summary': 'Public place',
                      'cityId': 'aberdeen-gb',
                      'available': true,
                    },
                  ]
                : [],
          }),
          200,
        );
      }
      if (request.method == 'POST') {
        expect(jsonDecode(request.body), {
          'kind': 'place',
          'targetId': 'place-1',
        });
        present = true;
        return http.Response('{"data":{"id":"saved-1"}}', 201);
      }
      if (request.method == 'DELETE') {
        expect(request.url.path, '/v1/me/saved/saved-1');
        present = false;
        return http.Response('', 204);
      }
      return http.Response('', 404);
    });
    final saved = SavedController(
      authorizationHeader: () => authorization,
      client: client,
      apiBaseUrl: 'http://localhost:8080',
    );
    await saved.load();
    expect(saved.items, isEmpty);
    await saved.toggle('place', 'place-1');
    expect(saved.contains('place', 'place-1'), isTrue);
    await saved.toggle('place', 'place-1');
    expect(saved.items, isEmpty);
    authorization = null;
    saved.clear();
    expect(saved.items, isEmpty);
    saved.dispose();
  });
}
