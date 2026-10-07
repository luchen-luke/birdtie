import 'dart:async';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'entity_action_contract_test.dart' show actionWire;
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'package:birdtie_client/src/workspace/saved_items.dart';

void main() {
  test('收藏明确当前条件传入原writer，私人不可见收藏仍可清理', () async {
    const id = '22e9cd18-babb-4359-9d1e-53593bedff47';
    final wire = actionWire(ref: const EntityActionRef('place', id));
    wire['actions'][3]['state'] = 'UNAVAILABLE';
    final view = EntityActionView.decode(
      wire,
      const EntityActionRef('place', id),
    );
    var present = false;
    final seen = <http.Request>[];
    final client = MockClient((r) async {
      seen.add(r);
      if (r.method == 'GET') {
        return http.Response(
          jsonEncode({
            'data': present
                ? [
                    {
                      'id': 'original-save',
                      'kind': 'place',
                      'targetId': id,
                      'title': '',
                      'summary': '',
                      'cityId': '',
                      'available': false,
                    },
                  ]
                : [],
          }),
          200,
        );
      }
      if (r.method == 'POST') {
        present = true;
        return http.Response('{"data":{"id":"original-save"}}', 201);
      }
      present = false;
      return http.Response('', 204);
    });
    final controller = SavedController(
      authorizationHeader: () => 'Bearer a',
      client: client,
      apiBaseUrl: 'https://api.test',
    );
    await controller.toggle(
      'place',
      id,
      approved: view.action(EntityActionKind.save).reviewed(view),
    );
    final post = seen.singleWhere((r) => r.method == 'POST');
    expect(post.headers['X-Birdtie-Action-Version'], view.sourceVersion);
    expect(
      post.headers['X-Birdtie-Action-Until'],
      view.validUntil.toIso8601String(),
    );
    expect(post.headers['X-Birdtie-Action-Operation'], 'SAVE');
    // Ownership-only unavailable-item cleanup intentionally preserves legacy path.
    await controller.toggle('place', id);
    final deleted = seen.singleWhere((r) => r.method == 'DELETE');
    expect(deleted.url.path, '/v1/me/saved/original-save');
    expect(deleted.headers.containsKey('X-Birdtie-Action-Version'), isFalse);
    controller.dispose();
    client.close();
  });
  for (final mutation in [false, true]) {
    test('收藏${mutation ? '写' : '读'}迟到在本人A-B-A清空后不复活或重发，借用client不关闭', () async {
      final pending = Completer<http.Response>();
      var token = 'Bearer A', calls = 0, closes = 0;
      final client = _ClosingClient((r) async {
        calls++;
        return pending.future;
      }, () => closes++);
      final controller = SavedController(
        authorizationHeader: () => token,
        client: client,
        apiBaseUrl: 'https://api.test',
      );
      final result = mutation
          ? controller.toggle('place', 'target')
          : controller.load();
      await Future<void>.delayed(Duration.zero);
      token = 'Bearer B';
      controller.clear();
      token = 'Bearer A';
      controller.clear();
      pending.complete(
        mutation
            ? http.Response('{"data":{"id":"old"}}', 201)
            : http.Response(
                '{"data":[{"id":"old","kind":"place","targetId":"target","title":"private","summary":"old","available":true}]}',
                200,
              ),
      );
      await result;
      expect(calls, 1);
      expect(controller.items, isEmpty);
      expect(controller.busy, isEmpty);
      controller.dispose();
      expect(closes, 0);
      client.close();
      expect(closes, 1);
    });
  }
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

class _ClosingClient extends MockClient {
  _ClosingClient(super.fn, this.closed);
  final void Function() closed;
  @override
  void close() {
    closed();
    super.close();
  }
}
