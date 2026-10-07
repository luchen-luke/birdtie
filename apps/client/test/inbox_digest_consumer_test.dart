import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'inbox_panel_test.dart' show signedInAuth;

// Schema fixtures derived from the native Item DTO and 099 delivery contract.
// These are not captured native HTTP responses or production notifications.
Map<String, dynamic> digestRow(int index, String kind) => {
  'id': '11000000-0000-4000-8000-${index.toString().padLeft(12, '0')}',
  'category': kind == 'community_message' ? 'messages' : 'updates',
  'title': switch (kind) {
    'community_message' => '社区有新消息',
    'business_claim_review' => '商家经营权审核状态已更新',
    _ => '与你的明确意图匹配',
  },
  'detail': '请打开当前来源查看，通知不表示已报名或经营背书。',
  'resourceType': kind,
  'resourceId': '22000000-0000-4000-8000-${index.toString().padLeft(12, '0')}',
  'createdAt': '2026-10-07T10:00:00Z',
  'semanticCategory': switch (kind) {
    'community_message' => 'COMMUNITY',
    'business_claim_review' => 'BUSINESS',
    _ => 'ACTIVITY',
  },
  'notificationRoute': 'DIGEST',
  'notificationPriority': 10,
  if (kind == 'community_message')
    'targetCommunityId': '33000000-0000-4000-8000-000000000001',
  if (kind == 'business_claim_review')
    'targetBusinessId': '44000000-0000-4000-8000-000000000001',
  if (kind == 'opportunity_available')
    'targetActivityId': '55000000-0000-4000-8000-000000000001',
};

http.Response fixtureResponse(Object data) => http.Response(
  jsonEncode({'data': data}), 200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  test('已投递DIGEST的原Inbox schema fixture保留三类原目标', () async {
    final rows = [digestRow(1, 'community_message'),
      digestRow(2, 'opportunity_available'), digestRow(3, 'business_claim_review')];
    final client = MockClient((r) async {
      expect(r.method, 'GET');
      expect(r.url.path, '/v1/me/inbox');
      return fixtureResponse(rows);
    });
    final source = RemoteInboxSource(authorizationHeader: () => 'Bearer schema-fixture',
      client: client, apiBaseUrl: 'https://schema.fixture');
    addTearDown(source.dispose);
    final items = await source.load();
    expect(items.map((i) => i.id), rows.map((r) => r['id']));
    expect(items[0].targetCommunityID, rows[0]['targetCommunityId']);
    expect(items[1].targetActivityID, rows[1]['targetActivityId']);
    expect(items[2].targetBusinessID, rows[2]['targetBusinessId']);
    expect(items[2].resourceID, isNot(items[2].targetBusinessID));
    expect(items.every((i) => i.notificationRoute == 'DIGEST'), isTrue);
    expect(items.map((i) => i.semanticCategory), ['COMMUNITY', 'ACTIVITY', 'BUSINESS']);
    expect(items.every((i) => i.notificationPriority == 10), isTrue);
  });

  test('DIGEST严格路由元数据和原目标边界，旧通知仍兼容', () {
    final row = digestRow(2, 'opportunity_available');
    for (final change in <Map<String, dynamic>>[
      {'notificationRoute': 'SILENT'}, {'notificationRoute': 'BLOCK'},
      {'notificationRoute': 'UNKNOWN'}, {'notificationRoute': 10},
      {'semanticCategory': null}, {'semanticCategory': 'UNKNOWN'},
      {'notificationPriority': null}, {'notificationPriority': 50},
      {'notificationPriority': '10'}, {'notificationPriority': 10.0},
      {'targetActivityId': 'invented'}, {'id': 'invented'},
      {'resourceType': 'business_update'},
    ]) {
      expect(() => InboxItem.fromJson({...row, ...change}), throwsFormatException);
    }
    final business = digestRow(3, 'business_claim_review');
    expect(() => InboxItem.fromJson({...business, 'targetTaskId': row['targetActivityId']}),
      throwsFormatException);
    for (final route in ['NORMAL', 'IMMEDIATE', null]) {
      final legacy = InboxItem.fromJson({...row, 'notificationRoute': route,
        'semanticCategory': null, 'notificationPriority': null});
      expect(legacy.targetActivityID, row['targetActivityId']);
      expect(legacy.notificationRoute, route);
    }
  });

  test('DIGEST读取保留原身份迟到拒绝及Read回执同ID检查', () async {
    var header = 'Bearer first';
    final waiting = Completer<http.Response>();
    final row = digestRow(2, 'opportunity_available');
    final source = RemoteInboxSource(authorizationHeader: () => header,
      apiBaseUrl: 'https://schema.fixture', client: MockClient((r) async {
        if (r.method == 'GET') return waiting.future;
        return fixtureResponse({...row, 'id': digestRow(4, 'opportunity_available')['id'],
          'readAt': '2026-10-07T10:01:00Z'});
      }));
    addTearDown(source.dispose);
    final load = source.load();
    final rejected = expectLater(load, throwsStateError);
    header = 'Bearer second';
    waiting.complete(fixtureResponse([row]));
    await rejected;
    await expectLater(source.markRead(row['id'] as String), throwsFormatException);
  });

  testWidgets('原右Inbox定时汇总只分组本次条目，逐条核验后打开原活动', (t) async {
    await t.binding.setSurfaceSize(const Size(400, 1100));
    addTearDown(() => t.binding.setSurfaceSize(null));
    final auth = await signedInAuth();
    final row = digestRow(2, 'opportunity_available');
    final normal = {...digestRow(4, 'opportunity_available'), 'title': '普通动态',
      'notificationRoute': 'NORMAL', 'notificationPriority': 50};
    final immediate = {...digestRow(5, 'opportunity_available'), 'title': '立即动态',
      'notificationRoute': 'IMMEDIATE', 'notificationPriority': 100};
    final calls = <String>[], opened = <String>[];
    final source = RemoteInboxSource(authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://schema.fixture', client: MockClient((r) async {
        calls.add('${r.method} ${r.url.path}');
        if (r.url.path == '/v1/me/inbox') return fixtureResponse([row, normal, immediate]);
        if (r.url.path.endsWith('/read')) {
          expect(r.url.path, '/v1/me/inbox/${row['id']}/read');
          return fixtureResponse({...row, 'readAt': '2026-10-07T10:01:00Z'});
        }
        expect(r.method, 'GET');
        return fixtureResponse([]);
      }));
    await t.pumpWidget(MaterialApp(home: Scaffold(body: InboxPanel(auth: auth,
      source: source, onOpenActivity: opened.add))));
    await t.pumpAndSettle();
    expect(find.text('定时汇总'), findsOneWidget);
    expect(find.text('以下是本次读取的已投递条目，打开时会核对当前内容。'), findsOneWidget);
    expect(find.text(row['title'] as String), findsOneWidget);
    expect(find.text('普通动态'), findsOneWidget);
    expect(find.text('立即动态'), findsOneWidget);
    expect(calls.where((c) => c.startsWith('POST')), isEmpty);
    expect(opened, isEmpty);
    await t.ensureVisible(find.text(row['title'] as String));
    await t.tap(find.text(row['title'] as String).hitTestable());
    await t.pumpAndSettle();
    expect(opened, [row['targetActivityId']]);
    expect(calls.where((c) => c.startsWith('POST')).toList(),
      ['POST /v1/me/inbox/${row['id']}/read']);
    expect(find.text(row['title'] as String), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    source.dispose(); auth.dispose();
  });

  testWidgets('DIGEST当前拒绝不打开，320宽大字号仍可读取和操作', (t) async {
    await t.binding.setSurfaceSize(const Size(320, 1050));
    addTearDown(() => t.binding.setSurfaceSize(null));
    final auth = await signedInAuth(), row = digestRow(2, 'opportunity_available');
    var posts = 0;
    final opened = <String>[];
    final source = RemoteInboxSource(authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://schema.fixture', client: MockClient((r) async {
        if (r.url.path == '/v1/me/inbox') return fixtureResponse([row]);
        if (r.url.path.endsWith('/read')) { posts++; return http.Response('{}', 404); }
        expect(r.method, 'GET'); return fixtureResponse([]);
      }));
    await t.pumpWidget(MaterialApp(builder: (context, child) => MediaQuery(
      data: MediaQuery.of(context).copyWith(textScaler: const TextScaler.linear(2)),
      child: child!), home: Scaffold(body: InboxPanel(auth: auth,
        source: source, onOpenActivity: opened.add))));
    await t.pumpAndSettle();
    await t.ensureVisible(find.text(row['title'] as String));
    await t.pumpAndSettle();
    final tile = find.ancestor(of: find.text(row['title'] as String), matching: find.byType(ListTile));
    expect(t.getSize(tile).height, greaterThanOrEqualTo(48));
    expect(find.text(row['title'] as String).hitTestable(), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.tap(find.text(row['title'] as String).hitTestable());
    await t.pumpAndSettle();
    expect(posts, 1); expect(opened, isEmpty);
    expect(find.text('当前通知暂不可读取，请刷新后重试。'), findsOneWidget);
    expect(find.text(row['title'] as String), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox()); source.dispose(); auth.dispose();
  });

  testWidgets('DIGEST同Inbox来源替换及组织ABA后旧已发Read不跳转', (t) async {
    await t.binding.setSurfaceSize(const Size(400, 1000));
    addTearDown(() => t.binding.setSurfaceSize(null));
    final auth = await signedInAuth(), workspace = ValueNotifier<String?>(null);
    final row = digestRow(2, 'opportunity_available'), waiting = Completer<http.Response>();
    var postsA = 0, postsB = 0;
    final opened = <String>[];
    RemoteInboxSource make(bool old) => RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader, apiBaseUrl: 'https://schema.fixture',
      client: MockClient((r) async {
        if (r.url.path == '/v1/me/inbox') return fixtureResponse(old ? [row] : []);
        if (r.url.path.endsWith('/read')) {
          if (old) { postsA++; return waiting.future; }
          postsB++; return http.Response('{}', 404);
        }
        expect(r.method, 'GET'); return fixtureResponse([]);
      }));
    final sourceA = make(true), sourceB = make(false);
    Widget view(RemoteInboxSource s) => MaterialApp(home: Scaffold(body: InboxPanel(
      key: const ValueKey('same-inbox'), auth: auth, source: s,
      workspaceChanges: workspace, organizationWorkspaceID: () => workspace.value,
      onOpenActivity: opened.add)));
    await t.pumpWidget(view(sourceA)); await t.pumpAndSettle();
    await t.ensureVisible(find.text(row['title'] as String));
    await t.tap(find.text(row['title'] as String).hitTestable()); await t.pump();
    expect(postsA, 1);
    workspace.value = 'organization'; workspace.value = null;
    await t.pumpWidget(view(sourceB)); await t.pumpAndSettle();
    waiting.complete(fixtureResponse({...row, 'readAt': '2026-10-07T10:01:00Z'}));
    await t.pumpAndSettle();
    expect(opened, isEmpty); expect(postsB, 0);
    expect(find.text(row['title'] as String), findsNothing);
    expect(find.text('定时汇总'), findsNothing);
    await t.pumpWidget(const SizedBox());
    sourceA.dispose(); sourceB.dispose(); auth.dispose(); workspace.dispose();
  });
}
