import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/activity_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'entity_action_contract_test.dart' show actionWire;

void main() {
  testWidgets('详情按权威取消收藏执行，原书签不在已加载页也能核实原ID', (tester) async {
    const id = 'b1700000-0000-4000-8000-000000000007';
    const bookmark = '41000000-0000-0000-0000-000000000001';
    final start = DateTime.now().toUtc().add(const Duration(days: 1));
    final detail = <String, dynamic>{
      'id': id,
      'title': '合成旧收藏活动',
      'hostLabel': '合成主办方',
      'startsAt': start.toIso8601String(),
      'endsAt': start.add(const Duration(hours: 1)).toIso8601String(),
      'timeZone': 'UTC',
      'status': 'upcoming',
      'source': {'label': '合成测试'},
    };
    final identity = ValueNotifier('Bearer synthetic-a');
    final writes = <http.Request>[];
    var targetReads = 0;
    http.Response reply(Object? data, [int code = 200]) => http.Response(
      jsonEncode({'data': data}),
      code,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
    final client = MockClient((request) async {
      if (request.method == 'DELETE') {
        writes.add(request);
        expect(request.url.path, '/v1/me/saved/$bookmark');
        return reply(null, 204);
      }
      if (request.method != 'GET') {
        writes.add(request);
        return reply(null, 500);
      }
      if (request.url.path.contains('/entity-actions/')) {
        final data = actionWire(ref: const EntityActionRef('activity', id));
        final action = (data['actions'] as List)
            .cast<Map<String, dynamic>>()
            .singleWhere((a) => a['kind'] == 'SAVE');
        action['operation'] = 'UNSAVE';
        action['label'] = '取消收藏';
        return reply(data);
      }
      if (request.url.path == '/v1/me/saved/target/activity/$id') {
        targetReads++;
        return reply({
          'id': bookmark,
          'kind': 'activity',
          'targetId': id,
          'title': '合成旧收藏活动',
          'summary': '',
          'cityId': 'aberdeen-gb',
          'available': true,
          'note': '',
          'noteRevision': 0,
        });
      }
      if (request.url.path == '/v1/me/saved') return reply([]);
      if (request.url.path.endsWith('/participations/me')) return reply(null);
      return reply(detail);
    });
    final saved = SavedController(
      authorizationHeader: () => identity.value,
      client: client,
      apiBaseUrl: 'http://synthetic-api.test',
    );
    expect(saved.contains('activity', id), false);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ActivityDetailSheet(
            activity: PublicActivity.fromJson(detail),
            authorizationHeader: () => identity.value,
            identityChanges: identity,
            apiBaseUrl: 'http://synthetic-api.test',
            client: client,
            saved: saved,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('收藏'));
    await tester.tap(find.text('收藏'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认取消收藏'));
    await tester.pumpAndSettle();
    expect(targetReads, 1);
    expect(writes, hasLength(1));
    expect(writes.single.method, 'DELETE');
    expect(writes.single.headers['X-Birdtie-Action-Version'], 'a' * 64);
    expect(find.text('收藏更新失败，请重试。'), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
    saved.dispose();
    identity.dispose();
    client.close();
  });
  for (final switchIdentity in [true, false]) {
    testWidgets(
      switchIdentity
          ? 'old activity save failure cannot replace the new identity state'
          : 'current activity save failure keeps its feedback and releases busy',
      (tester) async {
        const id = 'b1700000-0000-4000-8000-000000000007';
        final start = DateTime.now().toUtc().add(const Duration(days: 1));
        final detail = <String, dynamic>{
          'id': id,
          'title': '合成活动，仅供单元测试',
          'hostLabel': '合成主办方',
          'startsAt': start.toIso8601String(),
          'endsAt': start.add(const Duration(hours: 1)).toIso8601String(),
          'timeZone': 'UTC',
          'status': 'upcoming',
          'source': {'label': '合成资料'},
        };
        final identity = ValueNotifier('Bearer synthetic-a');
        String? authorization() => identity.value;
        final waiting = Completer<http.Response>();
        final writes = <http.Request>[];
        http.Response response(Object? data, [int code = 200]) => http.Response(
          jsonEncode({'data': data}),
          code,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
        final client = MockClient((request) async {
          if (request.method == 'POST') {
            expect(request.url.path, '/v1/me/saved');
            writes.add(request);
            return waiting.future;
          }
          expect(request.method, 'GET');
          if (request.url.path.contains('/entity-actions/')) {
            return response(
              actionWire(ref: const EntityActionRef('activity', id)),
            );
          }
          if (request.url.path.endsWith('/participations/me')) {
            return response(null);
          }
          if (request.url.path == '/v1/me/saved') return response([]);
          expect(request.url.path, '/v1/activities/$id');
          return response(detail);
        });
        final saved = SavedController(
          authorizationHeader: authorization,
          client: client,
          apiBaseUrl: 'http://synthetic-api.test',
        );
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: ActivityDetailSheet(
                activity: PublicActivity.fromJson(detail),
                authorizationHeader: authorization,
                identityChanges: identity,
                apiBaseUrl: 'http://synthetic-api.test',
                client: client,
                saved: saved,
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        await tester.ensureVisible(find.text('收藏'));
        await tester.tap(find.text('收藏'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('确认收藏'));
        await tester.pumpAndSettle();
        expect(writes, hasLength(1));
        expect(writes.single.headers['Authorization'], 'Bearer synthetic-a');
        expect(writes.single.headers['X-Birdtie-Action-Version'], 'a' * 64);
        if (switchIdentity) {
          saved.clear();
          identity.value = 'Bearer synthetic-b';
          await tester.pumpAndSettle();
          expect(find.text('收藏更新失败，请重试。'), findsNothing);
        }
        waiting.complete(response(null, 503));
        await tester.pumpAndSettle();
        expect(
          find.text('收藏更新失败，请重试。'),
          switchIdentity ? findsNothing : findsOneWidget,
        );
        expect(find.text('正在更新…'), findsNothing);
        expect(writes, hasLength(1), reason: 'a lost result is not a new save');
        expect(saved.items, isEmpty);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox.shrink());
        saved.dispose();
        identity.dispose();
        client.close();
      },
    );
  }
}
