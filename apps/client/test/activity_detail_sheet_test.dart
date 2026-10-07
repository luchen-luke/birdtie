import 'dart:convert';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/activity_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_action_contract_test.dart' show actionWire;

void main() {
  testWidgets(
    'activity changed public point cannot navigate cached coordinates',
    (tester) async {
      const id = 'b1700000-0000-4000-8000-000000000007';
      var changed = false;
      final opened = <Uri>[];
      final start = DateTime.now().toUtc().add(const Duration(days: 1));
      Map<String, dynamic> detail() => {
        'id': id,
        'title': '合成活动',
        'hostLabel': '合成组织',
        'startsAt': start.toIso8601String(),
        'endsAt': start.add(const Duration(hours: 1)).toIso8601String(),
        'timeZone': 'UTC',
        'status': 'upcoming',
        'source': {'label': '合成资料'},
        'location': {
          'coordinateSystem': 'wgs84',
          'precision': 'point',
          'latitude': changed ? 58.1 : 57.1,
          'longitude': -2.1,
        },
      };
      final client = MockClient((r) async {
        if (r.url.path.contains('/entity-actions/')) {
          final data = actionWire(ref: const EntityActionRef('activity', id));
          for (final dynamic a in data['actions'] as List) {
            a['state'] = a['kind'] == 'NAVIGATE' ? 'AVAILABLE' : 'UNAVAILABLE';
          }
          return http.Response(
            jsonEncode({'data': data}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        return http.Response(
          jsonEncode({'data': detail()}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ActivityDetailSheet(
              activity: PublicActivity.fromJson(detail()),
              authorizationHeader: () => null,
              apiBaseUrl: 'http://api.test',
              client: client,
              openExternal: (url) async {
                opened.add(url);
                return true;
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      changed = true;
      await tester.ensureVisible(find.text('导航'));
      await tester.tap(find.text('导航'));
      await tester.pumpAndSettle();
      if (find.text('继续').evaluate().isNotEmpty) {
        await tester.tap(find.text('继续'));
        await tester.pumpAndSettle();
      }
      expect(
        opened,
        isEmpty,
        reason: 'new source version must not reuse old public point',
      );
      expect(find.textContaining('导航地点已变化'), findsOneWidget);
    },
  );
  testWidgets('changed fee after visible free detail cannot submit stale JOIN', (
    tester,
  ) async {
    const id = 'b1700000-0000-4000-8000-000000000006';
    var paid = false, writes = 0;
    final start = DateTime.now().toUtc().add(const Duration(days: 1));
    Map<String, dynamic> detail() => {
      'id': id,
      'title': '合成活动',
      'hostLabel': '合成组织',
      'placeName': '',
      'summary': '',
      'startsAt': start.toIso8601String(),
      'endsAt': start.add(const Duration(hours: 1)).toIso8601String(),
      'timeZone': 'UTC',
      'status': 'upcoming',
      'source': {'label': '合成资料'},
      'priceMinor': paid ? 5000 : 0,
      'currency': 'GBP',
    };
    final client = MockClient((r) async {
      Object? data;
      if (r.url.path.contains('/entity-actions/')) {
        data = actionWire(ref: const EntityActionRef('activity', id));
      } else if (r.method == 'POST') {
        writes++;
        data = {'status': 'going'};
      } else if (r.url.path.endsWith('/participations/me')) {
        data = null;
      } else {
        data = detail();
      }
      return http.Response(
        jsonEncode({'data': data}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ActivityDetailSheet(
            activity: PublicActivity.fromJson(detail()),
            authorizationHeader: () => 'Bearer owner',
            apiBaseUrl: 'http://api.test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    paid = true;
    await tester.tap(find.text('报名参加'));
    await tester.pumpAndSettle();
    final continueButton = find.text('继续');
    if (continueButton.evaluate().isNotEmpty) {
      await tester.tap(continueButton);
      await tester.pumpAndSettle();
    }
    expect(
      writes,
      0,
      reason:
          'current native version alone does not mean the new fee was reviewed',
    );
    expect(find.textContaining('活动条件已变化'), findsOneWidget);
  });
  for (final location in [
    ('in_person', 'tbd', '地点：待定'),
    ('online', 'not_applicable', '形式：线上活动'),
    ('hybrid', 'tbd', '形式：线上＋线下活动'),
  ]) {
    testWidgets('activity detail shows ${location.$3}', (tester) async {
      final payload = <String, dynamic>{
        'id': 'b1700000-0000-4000-8000-000000000009',
        'hostLabel': '测试主办方',
        'title': '测试活动',
        'summary': '',
        'startsAt': '2026-10-03T10:00:00Z',
        'endsAt': '2026-10-03T12:00:00Z',
        'timeZone': 'Europe/London',
        'schedule': '周六 11:00',
        'status': 'upcoming',
        'modality': location.$1,
        'physicalPlaceStatus': location.$2,
        'source': {'label': '测试'},
      };
      final activity = PublicActivity.fromJson(payload);
      final client = MockClient(
        (_) async => http.Response(
          jsonEncode({'data': payload}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ActivityDetailSheet(
              activity: activity,
              authorizationHeader: () => null,
              apiBaseUrl: 'http://api.test',
              client: client,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text(location.$3), findsOneWidget);
      expect(find.text('导航'), findsNothing);
    });
  }
  for (final organizerType in [
    'PERSON',
    'COMMUNITY',
    'ORGANIZATION',
    'BUSINESS',
  ]) {
    testWidgets('activity detail opens $organizerType organizer', (
      tester,
    ) async {
      final start = DateTime.utc(2026, 10, 3, 10);
      final organizer = PublicActivityOrganizer(
        type: organizerType,
        id: 'organizer-id',
        name: '测试主办方',
      );
      final activity = PublicActivity(
        id: 'b1700000-0000-4000-8000-000000000009',
        organizer: organizer,
        hostLabel: organizer.name,
        placeName: '体育馆',
        title: '测试活动',
        summary: '',
        startsAt: start,
        endsAt: start.add(const Duration(hours: 2)),
        timeZone: 'Europe/London',
        schedule: '周六 11:00',
        status: 'upcoming',
        source: const PublicSource(
          label: '测试',
          maintainer: '',
          freshness: 'current',
          updatedAt: null,
        ),
        location: null,
      );
      final opened = <PublicActivityOrganizer>[];
      final client = MockClient((request) async {
        return http.Response(
          jsonEncode({
            'data': {
              'id': activity.id,
              'organizer': {
                'type': organizerType,
                'id': organizer.id,
                'name': organizer.name,
              },
              'title': activity.title,
              'hostLabel': activity.hostLabel,
              'startsAt': activity.startsAt.toIso8601String(),
              'endsAt': activity.endsAt.toIso8601String(),
              'timeZone': activity.timeZone,
              'schedule': activity.schedule,
              'status': 'upcoming',
              'source': {'label': '测试'},
            },
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ActivityDetailSheet(
              activity: activity,
              authorizationHeader: () => null,
              apiBaseUrl: 'http://api.test',
              client: client,
              onOpenOrganizer: opened.add,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('查看主办方：测试主办方'));
      expect(opened.single.type, organizerType);
      expect(opened.single.id, organizer.id);
    });
  }
  testWidgets('活动同key授权getter监听同值重绑ABA不能复用旧报名批准', (tester) async {
    const id = 'b1700000-0000-4000-8000-000000000006';
    var writes = 0;
    final rebound = ValueNotifier(false),
        changesA = ValueNotifier(0),
        changesB = ValueNotifier(0);
    String? tokenA() => 'Bearer owner';
    String? tokenB() => 'Bearer owner';
    String? workspaceA() => null;
    String? workspaceB() => null;
    final start = DateTime.now().toUtc().add(const Duration(days: 1));
    Map<String, dynamic> detail() => {
      'id': id,
      'title': '合成活动',
      'hostLabel': '合成组织',
      'placeName': '',
      'summary': '',
      'startsAt': start.toIso8601String(),
      'endsAt': start.add(const Duration(hours: 1)).toIso8601String(),
      'timeZone': 'UTC',
      'status': 'upcoming',
      'source': {'label': '合成资料'},
      'priceMinor': 0,
      'currency': 'GBP',
    };
    final client = MockClient((r) async {
      Object? data;
      if (r.url.path.contains('/entity-actions/')) {
        data = actionWire(ref: const EntityActionRef('activity', id));
      } else if (r.method == 'POST') {
        writes++;
        data = {'status': 'going'};
      } else if (r.url.path.endsWith('/participations/me')) {
        data = null;
      } else {
        data = detail();
      }
      return http.Response(
        jsonEncode({'data': data}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ValueListenableBuilder<bool>(
            valueListenable: rebound,
            builder: (_, changed, _) => ActivityDetailSheet(
              key: const ValueKey('same-activity'),
              activity: PublicActivity.fromJson(detail()),
              authorizationHeader: changed ? tokenB : tokenA,
              workspaceID: changed ? workspaceB : workspaceA,
              identityChanges: changed ? changesB : changesA,
              apiBaseUrl: 'http://api.test',
              client: client,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('报名参加'));
    await tester.pumpAndSettle();
    expect(find.text('确认报名'), findsOneWidget);
    expect(writes, 0);
    rebound.value = true;
    await tester.pumpAndSettle();
    rebound.value = false;
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认报名'));
    await tester.pumpAndSettle();
    expect(writes, 0, reason: '具体批准必须在同值回调监听A-B-A后永久失效');
    expect(find.text('确认报名'), findsNothing);
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox());
    rebound.dispose();
    changesA.dispose();
    changesB.dispose();
    client.close();
  });

  testWidgets(
    'cancelled activity has no new join action and failed loading can retry',
    (tester) async {
      final start = DateTime.utc(2026, 10, 3, 10);
      final activity = PublicActivity(
        id: 'b1700000-0000-4000-8000-000000000006',
        hostLabel: '学生社团',
        placeName: '体育馆',
        title: '已取消的活动',
        summary: '',
        startsAt: start,
        endsAt: start.add(const Duration(hours: 2)),
        timeZone: 'Europe/London',
        schedule: '10月3日（周六）11:00',
        status: 'cancelled',
        source: const PublicSource(
          label: '社团',
          maintainer: '',
          freshness: 'current',
          updatedAt: null,
        ),
        location: null,
      );
      var failed = true;
      final client = MockClient((request) async {
        if (failed) return http.Response('{"error":{"code":"temporary"}}', 503);
        if (request.url.path.endsWith('/participations/me')) {
          return http.Response('{"data":null}', 200);
        }
        return http.Response(
          jsonEncode({
            'data': {
              'id': activity.id,
              'title': activity.title,
              'hostLabel': activity.hostLabel,
              'startsAt': activity.startsAt.toIso8601String(),
              'endsAt': activity.endsAt.toIso8601String(),
              'timeZone': activity.timeZone,
              'schedule': activity.schedule,
              'status': 'cancelled',
              'source': {'label': '社团'},
            },
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ActivityDetailSheet(
              activity: activity,
              authorizationHeader: () => 'Bearer test',
              apiBaseUrl: 'http://api.test',
              client: client,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('活动状态加载失败，请重试。'), findsOneWidget);
      failed = false;
      await tester.tap(find.text('重试'));
      await tester.pumpAndSettle();
      expect(find.text('活动已取消'), findsOneWidget);
      expect(find.text('报名参加'), findsNothing);
      expect(find.text('导航'), findsNothing);
    },
  );

  testWidgets(
    'full activity shows decision details and real share/navigation actions',
    (tester) async {
      final start = DateTime.utc(2026, 10, 3, 10);
      final activity = PublicActivity(
        id: 'b1700000-0000-4000-8000-000000000005',
        hostLabel: '学生社团',
        placeName: '体育馆',
        title: '周末羽毛球',
        summary: '一起打球',
        startsAt: start,
        endsAt: start.add(const Duration(hours: 2)),
        timeZone: 'Europe/London',
        schedule: '10月3日（周六）11:00',
        status: 'upcoming',
        source: const PublicSource(
          label: '社团',
          reference: 'https://example.org/event',
          maintainer: '',
          freshness: 'current',
          updatedAt: null,
        ),
        location: const PublicPlaceLocation(
          coordinateSystem: 'wgs84',
          precision: 'point',
          latitude: 57.15,
          longitude: -2.1,
        ),
      );
      final urls = <Uri>[];
      final shares = <String>[];
      final client = MockClient((request) async {
        if (request.url.path.contains('/entity-actions/')) {
          final data = actionWire(ref: EntityActionRef('activity', activity.id))
            ..['title'] = activity.title;
          for (final dynamic a in data['actions'] as List) {
            if (a['kind'] == 'NAVIGATE') a['state'] = 'AVAILABLE';
            if (a['kind'] == 'SHARE') {
              a['state'] = 'AVAILABLE';
              a['operation'] = 'EXPORT_PUBLIC';
              a['allowedOperations'] = ['EXPORT_PUBLIC'];
              a['label'] = '系统公开分享';
            }
          }
          return http.Response(
            jsonEncode({'data': data}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        if (request.url.path.endsWith('/participations/me')) {
          return http.Response('{"data":null}', 200);
        }
        return http.Response(
          jsonEncode({
            'data': {
              'id': activity.id,
              'title': activity.title,
              'hostLabel': activity.hostLabel,
              'placeName': activity.placeName,
              'summary': activity.summary,
              'description': '欢迎所有水平的同学参加。',
              'startsAt': activity.startsAt.toIso8601String(),
              'endsAt': activity.endsAt.toIso8601String(),
              'timeZone': activity.timeZone,
              'schedule': activity.schedule,
              'endSchedule': '10月3日（周六）13:00',
              'status': 'upcoming',
              'capacity': 1,
              'participantCount': 1,
              'priceMinor': 500,
              'currency': 'GBP',
              'eligibility': '学生优先',
              'officialUrl': 'https://example.org',
              'location': {
                'coordinateSystem': 'wgs84',
                'precision': 'point',
                'latitude': 57.15,
                'longitude': -2.1,
              },
              'source': {
                'label': '社团',
                'reference': 'https://example.org/event',
              },
            },
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ActivityDetailSheet(
              activity: activity,
              authorizationHeader: () => 'Bearer test',
              apiBaseUrl: 'http://api.test',
              client: client,
              shareText: (text) async {
                shares.add(text);
              },
              openExternal: (uri) async {
                urls.add(uri);
                return true;
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('费用：GBP 5.00'), findsOneWidget);
      expect(find.text('参加条件：学生优先'), findsOneWidget);
      expect(find.text('名额：1 / 1 · 已满'), findsOneWidget);
      expect(find.text('欢迎所有水平的同学参加。'), findsOneWidget);
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
        isNull,
      );
      await tester.ensureVisible(find.text('主办方提供的链接'));
      await tester.tap(find.text('主办方提供的链接'));
      await tester.pump();
      expect(urls.single.host, 'example.org');
      await tester.ensureVisible(find.text('分享'));
      await tester.tap(find.text('分享'));
      await tester.pumpAndSettle();
      expect(shares, isEmpty);
      await tester.tap(find.text('打开系统分享'));
      await tester.pumpAndSettle();
      expect(shares.single, contains('周末羽毛球'));
      await tester.ensureVisible(find.text('导航'));
      await tester.tap(find.text('导航'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('打开地图'));
      await tester.pumpAndSettle();
      expect(urls.last.queryParameters['query'], '57.15,-2.1');
    },
  );

  testWidgets(
    'activity detail reads server RSVP and supports cancel and rejoin',
    (tester) async {
      var status = 'going';
      final start = DateTime.utc(2026, 10, 3, 10);
      final activity = PublicActivity(
        id: 'b1700000-0000-4000-8000-000000000005',
        hostLabel: '学生社团',
        placeName: '体育馆',
        title: '周末羽毛球',
        summary: '一起打球',
        startsAt: start,
        endsAt: start.add(const Duration(hours: 2)),
        timeZone: 'Europe/London',
        schedule: '周六 11:00',
        status: 'upcoming',
        source: const PublicSource(
          label: '社团',
          maintainer: '',
          freshness: 'current',
          updatedAt: null,
        ),
        location: null,
      );
      final client = MockClient((request) async {
        expect(
          request.headers.entries.any(
            (entry) =>
                entry.key.toLowerCase() == 'authorization' &&
                entry.value == 'Bearer test',
          ),
          isTrue,
        );
        if (request.url.path.contains('/entity-actions/')) {
          final payload = actionWire(
            ref: EntityActionRef('activity', activity.id),
          );
          final actions = payload['actions'] as List;
          for (final dynamic action in actions) {
            if (action['kind'] == 'JOIN') {
              action['operation'] = status == 'going' ? 'CANCEL_RSVP' : 'JOIN';
              action['label'] = status == 'going' ? '取消报名' : '报名参加';
            }
          }
          return http.Response(
            jsonEncode({'data': payload}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        if (request.url.path.endsWith('/participations/me')) {
          if (request.method == 'DELETE') status = 'cancelled';
          return http.Response(
            jsonEncode({
              'data': {'status': status},
            }),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        if (request.url.path.endsWith('/participations')) {
          status = 'going';
          return http.Response(
            jsonEncode({
              'data': {'status': status},
            }),
            201,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        return http.Response(
          jsonEncode({
            'data': {
              'id': activity.id,
              'hostLabel': activity.hostLabel,
              'placeName': activity.placeName,
              'title': activity.title,
              'summary': activity.summary,
              'startsAt': activity.startsAt.toIso8601String(),
              'endsAt': activity.endsAt.toIso8601String(),
              'timeZone': activity.timeZone,
              'schedule': activity.schedule,
              'status': activity.status,
              'source': {'label': '社团'},
            },
          }),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ActivityDetailSheet(
              activity: activity,
              authorizationHeader: () => 'Bearer test',
              apiBaseUrl: 'http://api.test',
              client: client,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('你已报名'), findsOneWidget);
      await tester.tap(find.text('取消报名'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认取消报名'));
      await tester.pumpAndSettle();
      expect(find.text('报名参加'), findsOneWidget);
      await tester.tap(find.text('报名参加'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认报名'));
      await tester.pumpAndSettle();
      expect(find.text('你已报名'), findsOneWidget);
    },
  );
}
