import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/activity_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/activity_participation_operation.dart';
import 'package:birdtie_client/src/workspace/activity_participation_pending_store.dart';
import 'package:birdtie_client/src/workspace/chat_entity_router.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'chat_entity_router_test.dart' show ChatTestAuth, chatActivity;
import 'entity_action_contract_test.dart' show actionWire;
import 'entity_share_pending_store_test.dart' show shareOwner, shareTarget;

const _api = 'https://rsvp-stable-binding.test';

http.Response _json(Object? data, [int status = 200]) => http.Response(
  jsonEncode({'data': data}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

class _RsvpFixture {
  _RsvpFixture() {
    client = MockClient(reply);
    replacement = MockClient(reply);
  }
  final auth = ChatTestAuth();
  final presentation = ValueNotifier<int>(0);
  final bindingChanges = ValueNotifier<int>(0);
  late final MockClient client, replacement;
  late http.Client transport = client;
  final navigator = GlobalKey<NavigatorState>();
  String? currentWorkspaceID() => workspace;
  late final String? Function() workspaceID = currentWorkspaceID;
  final store = SecureParticipationPendingStore();
  final requests = <http.Request>[];
  String? workspace;
  String base = _api;
  ThemeMode theme = ThemeMode.light;
  double scale = 1, inset = 0;
  bool responseLost = false, allowVerify = false, joined = false;
  PendingParticipationOperation? original;

  Iterable<http.Request> get posts => requests.where((r) => r.method == 'POST');
  Iterable<http.Request> get operationGets => requests.where(
    (r) =>
        r.method == 'GET' && r.url.path.contains('/participation-operations/'),
  );
  Future<http.Response> reply(http.Request r) async {
    requests.add(r);
    if (r.url.path.contains('/entity-actions/')) {
      final wire = actionWire(
        ref: const EntityActionRef('activity', shareTarget),
      );
      for (final dynamic action in wire['actions'] as List) {
        action['state'] = action['kind'] == 'JOIN'
            ? 'AVAILABLE'
            : 'UNAVAILABLE';
        if (action['kind'] == 'JOIN') action['label'] = '报名参加';
      }
      return _json(wire);
    }
    if (r.url.path == '/v1/activities/$shareTarget/participation-operations' &&
        r.method == 'POST') {
      final pending = (await store.read(_api, shareOwner)).single;
      original = pending;
      expect(jsonDecode(r.body), {
        'operationId': pending.operationID,
        'operation': 'JOIN',
      });
      expect(r.headers['Authorization'], 'Bearer A');
      expect(r.headers['X-Birdtie-Action-Version'], 'a' * 64);
      expect(r.headers['X-Birdtie-Action-Operation'], 'JOIN');
      expect(pending.activityID, shareTarget);
      joined = true;
      if (responseLost) {
        throw TimeoutException('synthetic committed response lost');
      }
      return _json(receipt(pending), 201);
    }
    if (r.url.path.contains('/participation-operations/')) {
      expect(r.method, 'GET');
      expect(
        r.url.path,
        '/v1/activities/$shareTarget/participation-operations/${original!.operationID}',
      );
      return allowVerify ? _json(receipt(original!)) : _json(null, 404);
    }
    if (r.url.path.endsWith('/participations/me')) {
      return _json(joined ? {'status': 'going'} : null);
    }
    if (r.url.path == '/v1/activities/$shareTarget') {
      return _json(chatActivity());
    }
    return _json(null, 503);
  }

  Map<String, dynamic> receipt(PendingParticipationOperation p) => {
    'schemaVersion': participationOperationSchema,
    'ownerId': shareOwner,
    'activityId': shareTarget,
    'operationId': p.operationID,
    'operation': p.operation,
    'requestDigest': p.requestDigest,
    'effectKey': participationEffectKey(shareOwner, p.operationID),
    'toolVersion': participationToolVersion,
    'recordedAt': '2026-10-10T08:00:00.123456Z',
    'status': 'COMMITTED',
    'effectId': 'a1000000-0000-4000-8000-000000000004',
    'state': 'going',
    'effectAt': '2026-10-10T08:00:00.123Z',
  };

  Widget app() => ValueListenableBuilder<int>(
    valueListenable: presentation,
    builder: (_, _, _) => MaterialApp(
      navigatorKey: navigator,
      theme: ThemeData.light(),
      darkTheme: ThemeData.dark(),
      themeMode: theme,
      builder: (context, child) => MediaQuery(
        data: MediaQuery.of(context).copyWith(
          textScaler: TextScaler.linear(scale),
          viewInsets: EdgeInsets.only(bottom: inset),
        ),
        child: child!,
      ),
      home: Scaffold(
        body: Builder(
          builder: (context) => Center(
            child: FilledButton(
              onPressed: () {
                unawaited(
                  Navigator.of(context).push<void>(
                    MaterialPageRoute(
                      builder: (_) => ValueListenableBuilder<int>(
                        valueListenable: bindingChanges,
                        builder: (_, _, _) => ChatEntityDetail(
                          key: const ValueKey('stable-chat-activity'),
                          type: 'activity',
                          id: shareTarget,
                          auth: auth,
                          workspaceChanges: bindingChanges,
                          workspaceID: workspaceID,
                          client: transport,
                          apiBaseUrl: base,
                        ),
                      ),
                    ),
                  ),
                );
              },
              child: const Text('打开原活动详情'),
            ),
          ),
        ),
      ),
    ),
  );
  Future<void> mountAndOpen(WidgetTester t) async {
    await t.pumpWidget(app());
    await t.pumpAndSettle();
    await t.tap(find.text('打开原活动详情'));
    await t.pumpAndSettle();
    expect(find.byType(ActivityDetailSheet), findsOneWidget);
    expect(
      t
          .widget<ActivityDetailSheet>(find.byType(ActivityDetailSheet))
          .accountID
          ?.call(),
      shareOwner,
    );
  }

  Future<void> propose(WidgetTester t) async {
    await t.ensureVisible(find.text('报名参加'));
    await t.tap(find.text('报名参加'));
    await t.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(find.text('确认报名'), findsOneWidget);
    expect(posts, isEmpty);
  }

  Future<void> unmount(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
  }

  void dispose() {
    presentation.dispose();
    bindingChanges.dispose();
    auth.dispose();
    client.close();
    replacement.close();
  }
}

Future<void> _fixture(
  WidgetTester t,
  Future<void> Function(_RsvpFixture) work,
) async {
  FlutterSecureStorage.setMockInitialValues({});
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  final f = _RsvpFixture();
  try {
    await work(f);
  } finally {
    await f.unmount(t);
    f.dispose();
  }
}

void main() {
  testWidgets('原 ChatEntityDetail 报名批准与恢复的八场景真实Navigator检查', (t) async {
    // The production secure journal owns one serial Future tail. Keep these
    // related scenarios in one FakeAsync zone instead of leaving a pending
    // plugin-initialization Future across separate Flutter test zones.
    Future<void> scenario(
      String description,
      Future<void> Function(WidgetTester) body,
    ) async {
      debugPrint('SCENARIO START: $description');
      await body(t);
      debugPrint('SCENARIO PASS: $description');
    }

    for (final change in ['dialog', 'theme', 'mediaquery', 'focus-keyboard']) {
      await scenario('原 ChatEntityDetail $change 重建后原确认只提交一次并保留 owner/journal', (
        t,
      ) async {
        await _fixture(t, (f) async {
          await f.mountAndOpen(t);
          final before = t.widget<ActivityDetailSheet>(
            find.byType(ActivityDetailSheet),
          );
          await f.propose(t);
          if (change == 'theme') f.theme = ThemeMode.dark;
          if (change == 'mediaquery') f.scale = 1.3;
          if (change == 'focus-keyboard') {
            FocusManager.instance.primaryFocus?.unfocus();
            f.inset = 100;
          }
          f.presentation.value++;
          await t.pumpAndSettle();
          await t.ensureVisible(find.text('确认报名'));
          await t.tap(find.text('确认报名'));
          await t.pumpAndSettle();
          expect(
            f.posts,
            hasLength(1),
            reason:
                'same live approval must reach the original human writer exactly once',
          );
          expect(find.text('取消报名'), findsOneWidget);
          final after = t.widget<ActivityDetailSheet>(
            find.byType(ActivityDetailSheet),
          );
          expect(identical(before.accountID, after.accountID), true);
          expect(
            identical(before.authorizationHeader, after.authorizationHeader),
            true,
          );
          expect(after.accountID?.call(), shareOwner);
          expect(after.apiBaseUrl, _api);
          expect(await f.store.read(_api, shareOwner), isEmpty);
          expect(t.takeException(), isNull);
        });
      });
    }

    await scenario('原 ChatEntityDetail UNKNOWN 重建与重启均仅 sameID GET核实，零重复POST', (
      t,
    ) async {
      await _fixture(t, (f) async {
        f.responseLost = true;
        await f.mountAndOpen(t);
        await f.propose(t);
        await t.tap(find.text('确认报名'));
        await t.pumpAndSettle();
        expect(f.posts, hasLength(1));
        final pending = (await f.store.read(_api, shareOwner)).single;
        expect(pending.sameBinding(f.original!), true);
        expect(find.text('核对原操作'), findsOneWidget);
        f.theme = ThemeMode.dark;
        f.scale = 1.3;
        f.presentation.value++;
        await t.pumpAndSettle();
        expect(
          (await f.store.read(_api, shareOwner)).single.sameBinding(pending),
          true,
        );
        await t.ensureVisible(find.text('核对原操作'));
        await t.tap(find.text('核对原操作'));
        await t.pumpAndSettle();
        expect(f.operationGets, hasLength(1));
        expect(f.posts, hasLength(1));
        await f.unmount(t);
        f.allowVerify = true;
        await f.mountAndOpen(t);
        expect(f.operationGets, hasLength(2));
        expect(f.posts, hasLength(1));
        expect(
          f.operationGets.every(
            (r) => r.url.path.endsWith('/${pending.operationID}'),
          ),
          true,
        );
        expect(await f.store.read(_api, shareOwner), isEmpty);
        expect(find.text('取消报名'), findsOneWidget);
        expect(t.takeException(), isNull);
      });
    });

    for (final change in ['account', 'workspace', 'transport']) {
      await scenario('原 ChatEntityDetail $change A→B→A 后旧确认0POST', (t) async {
        await _fixture(t, (f) async {
          await f.mountAndOpen(t);
          await f.propose(t);
          if (change == 'account') {
            f.auth.use('Bearer B');
            f.auth.use('Bearer A');
          } else if (change == 'workspace') {
            f.workspace = 'a1000000-0000-4000-8000-000000000006';
            f.bindingChanges.value++;
            f.workspace = null;
            f.bindingChanges.value++;
          } else {
            f.transport = f.replacement;
            f.base = 'https://rsvp-source-b.test';
            f.bindingChanges.value++;
            await t.pump();
            f.transport = f.client;
            f.base = _api;
            f.bindingChanges.value++;
          }
          await t.pumpAndSettle();
          final confirm = find.text('确认报名');
          if (confirm.evaluate().isNotEmpty) {
            await t.tap(confirm);
            await t.pumpAndSettle();
          }
          expect(f.posts, isEmpty);
          expect(await f.store.read(_api, shareOwner), isEmpty);
          expect(find.text('取消报名'), findsNothing);
          expect(t.takeException(), isNull);
        });
      });
    }
  });
}
