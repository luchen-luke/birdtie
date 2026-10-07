import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/place_history_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const historyTestPlace = '22222222-2222-4222-8222-222222222222';
Map<String, dynamic> historyTestData() {
  final now = DateTime.now().toUtc();
  return {
    'schemaVersion': 'place-social-history-v1',
    'placeId': historyTestPlace,
    'cityId': 'test',
    'windowDays': 30,
    'windowStart': now.subtract(const Duration(days: 30)).toIso8601String(),
    'checkedAt': now.toIso8601String(),
    'recentMomentCount': 1,
    'recentMoments': [
      {
        'id': '11111111-1111-4111-8111-111111111111',
        'title': '公开分享',
        'excerpt': '公开的节选',
        'revision': 2,
        'publishedAt': now.subtract(const Duration(days: 1)).toIso8601String(),
      },
    ],
    'activityPatterns': [
      {'category': 'badminton', 'dayKind': 'WEEKEND', 'arrangements': 2},
    ],
    'suitability': null,
  };
}

http.Response historyTestReply(Map<String, dynamic> d) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': d})), 200);
void main() {
  test('匿名公开摘要不发送私人来源，安排不解读到场', () async {
    final c = PlaceHistoryController(
      placeID: historyTestPlace,
      authorizationHeader: () => null,
      client: MockClient((r) async {
        expect(r.headers.containsKey('Authorization'), false);
        expect(r.url.query, isEmpty);
        expect(r.url.path.endsWith('/social-history'), true);
        return historyTestReply(historyTestData());
      }),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await c.refresh();
    expect(c.summary!.count, 1);
    expect(c.summary!.patterns.single.arrangements, 2);
    expect(c.summary!.facts, isNull);
  });
  test('无效已提供会话不退回匿名重试', () async {
    var requests = 0;
    final c = PlaceHistoryController(
      placeID: historyTestPlace,
      authorizationHeader: () => 'Bearer expired',
      client: MockClient((r) async {
        requests++;
        expect(r.headers['Authorization'], 'Bearer expired');
        return http.Response('{}', 401);
      }),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await c.refresh();
    expect(c.summary, isNull);
    expect(requests, 1);
  });
  test('主体改变的迟到摘要无法显示', () async {
    var token = 'Bearer one';
    final done = Completer<http.Response>();
    final c = PlaceHistoryController(
      placeID: historyTestPlace,
      authorizationHeader: () => token,
      client: MockClient((_) async => done.future),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    final request = c.refresh();
    token = 'Bearer two';
    expect(c.summary, isNull);
    done.complete(historyTestReply(historyTestData()));
    await request;
    expect(c.summary, isNull);
  });
  test('公开结构拒绝私密字段、错误来源、未来分享和重复ID', () {
    for (final mode in [
      'private',
      'target',
      'future',
      'duplicate',
      'count',
      'days',
    ]) {
      final d = historyTestData();
      switch (mode) {
        case 'private':
          d['privateMemories'] = ['secret'];
        case 'target':
          d['placeId'] = 'wrong';
        case 'future':
          (d['recentMoments'] as List).first['publishedAt'] = DateTime.now()
              .toUtc()
              .add(const Duration(days: 1))
              .toIso8601String();
        case 'duplicate':
          d['recentMoments'] = List<Map<String, dynamic>>.from(
            d['recentMoments'],
          );
          (d['recentMoments'] as List).add(
            Map<String, dynamic>.from((d['recentMoments'] as List).first),
          );
          d['recentMomentCount'] = 2;
        case 'count':
          d['recentMomentCount'] = 0;
        case 'days':
          d['windowDays'] = 365;
      }
      expect(
        () => PlaceHistorySummary.fromJson(d, historyTestPlace),
        throwsA(anything),
        reason: mode,
      );
    }
  });
  test('审核资料拒绝重复声明、全未知空资料和带片段来源', () {
    Map<String, dynamic> data() {
      final d = historyTestData();
      final now = DateTime.parse(d['checkedAt']);
      d['suitability'] = {
        'schemaVersion': 'place-semantic-v1',
        'placeId': historyTestPlace,
        'cityId': d['cityId'],
        'version': 1,
        'checkedAt': d['checkedAt'],
        'facts': {
          'vibe': ['quiet'],
          'good_for': null,
          'price': null,
          'accessibility': null,
          'group_size': null,
          'reservation': null,
          'suitability': null,
        },
        'source': {
          'label': '审核来源',
          'url': 'https://example.test/place',
          'observedAt': now.subtract(const Duration(days: 2)).toIso8601String(),
          'reviewedAt': now.subtract(const Duration(days: 1)).toIso8601String(),
          'expiresAt': now.add(const Duration(days: 1)).toIso8601String(),
        },
        'confidence': {
          'kind': 'EDITOR_ASSESSMENT_UNCALIBRATED',
          'level': 'MEDIUM',
        },
      };
      return d;
    }

    expect(
      PlaceHistorySummary.fromJson(data(), historyTestPlace).facts!['vibe'],
      ['quiet'],
    );
    for (final mode in ['duplicate', 'empty', 'fragment']) {
      final d = data(), p = d['suitability'] as Map<String, dynamic>;
      if (mode == 'duplicate') p['facts']['vibe'] = ['quiet', 'quiet'];
      if (mode == 'empty') p['facts']['vibe'] = <String>[];
      if (mode == 'fragment') {
        p['source']['url'] = 'https://example.test/place#private';
      }
      expect(
        () => PlaceHistorySummary.fromJson(d, historyTestPlace),
        throwsA(anything),
        reason: mode,
      );
    }
  });
}
