import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:birdtie_client/src/workspace/private_place_memory_page.dart';
import 'private_place_memory_api_test.dart';

Future<void> placePageScroll(WidgetTester t, String text) async {
  await t.scrollUntilVisible(
    find.text(text),
    180,
    scrollable: find.byType(Scrollable).first,
  );
  await t.pumpAndSettle();
}

void main() {
  testWidgets(
    'Chinese current own sources and explicit confirmation cancel saves nothing',
    (t) async {
      final identity = ChangeNotifier();
      addTearDown(identity.dispose);
      var puts = 0;
      final client = placeFixtureClient((r) async {
        if (r.method == 'GET') return placeResponse(placeReadFixture());
        puts++;
        return placeResponse(placeReceiptFixture(r));
      });
      await t.pumpWidget(
        MaterialApp(
          home: PrivatePlaceMemoryPage(
            placeID: placeTarget,
            placeName: '合成测试地点',
            authorizationHeader: () => 'Bearer local',
            accountID: () => placeOwner,
            identityChanges: identity,
            client: client,
            apiBaseUrl: 'http://127.0.0.1',
            pendingStore: MemoryPlacePendingStore(),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('我的地点记录'), findsOneWidget);
      expect(find.text('目前没有可见的本人地点记录。这不表示从未到访。'), findsOneWidget);
      await placePageScroll(t, '检查并保存本人声明');
      await t.tap(find.text('检查并保存本人声明'));
      await t.pumpAndSettle();
      expect(find.text('检查本人声明'), findsOneWidget);
      expect(find.textContaining('以本人账号保存，目标：'), findsOneWidget);
      expect(find.textContaining('不会公开为到访'), findsOneWidget);
      await t.tap(find.text('返回修改'));
      await t.pumpAndSettle();
      expect(puts, 0);
    },
  );
  testWidgets('explicit self visit and Agent marking exact confirmation', (
    t,
  ) async {
    final identity = ChangeNotifier();
    addTearDown(identity.dispose);
    var puts = 0;
    String? kind, visibility;
    final client = placeFixtureClient((r) async {
      if (r.method == 'GET') return placeResponse(placeReadFixture());
      puts++;
      final receipt = placeReceiptFixture(r);
      kind = receipt['kind'];
      visibility = receipt['visibility'];
      return placeResponse(receipt);
    });
    await t.pumpWidget(
      MaterialApp(
        home: PrivatePlaceMemoryPage(
          placeID: placeTarget,
          placeName: '合成测试地点',
          authorizationHeader: () => 'Bearer local',
          accountID: () => placeOwner,
          identityChanges: identity,
          client: client,
          apiBaseUrl: 'http://127.0.0.1',
          pendingStore: MemoryPlacePendingStore(),
        ),
      ),
    );
    await t.pumpAndSettle();
    await placePageScroll(t, '自述到访（未核验）');
    await t.tap(find.text('自述到访（未核验）'));
    await placePageScroll(t, 'Agent 范围标记');
    await t.tap(find.text('Agent 范围标记'));
    await placePageScroll(t, '检查并保存本人声明');
    await t.tap(find.text('检查并保存本人声明'));
    await t.pumpAndSettle();
    expect(find.text('本人自述到访过这个地点（未经核验）'), findsOneWidget);
    await t.tap(find.text('确认保存本人声明'));
    await t.pumpAndSettle();
    expect(puts, 1);
    expect(kind, 'VISITED');
    expect(visibility, 'AGENT_ONLY');
    expect(t.takeException(), isNull);
  });
  testWidgets('identity ABA destroys old confirmation and no old submission', (
    t,
  ) async {
    final identity = ChangeNotifier();
    addTearDown(identity.dispose);
    String token = 'Bearer A';
    var puts = 0;
    final client = placeFixtureClient((r) async {
      if (r.method == 'GET') return placeResponse(placeReadFixture());
      puts++;
      return placeResponse(placeReceiptFixture(r));
    });
    await t.pumpWidget(
      MaterialApp(
        home: PrivatePlaceMemoryPage(
          placeID: placeTarget,
          placeName: '合成测试地点',
          authorizationHeader: () => token,
          accountID: () => placeOwner,
          identityChanges: identity,
          client: client,
          apiBaseUrl: 'http://127.0.0.1',
          pendingStore: MemoryPlacePendingStore(),
        ),
      ),
    );
    await t.pumpAndSettle();
    await placePageScroll(t, '检查并保存本人声明');
    await t.tap(find.text('检查并保存本人声明'));
    await t.pumpAndSettle();
    token = 'Bearer B';
    identity.notifyListeners();
    token = 'Bearer A';
    identity.notifyListeners();
    await t.pumpAndSettle();
    expect(find.text('确认保存本人声明'), findsNothing);
    expect(puts, 0);
    expect(t.takeException(), isNull);
  });
  testWidgets(
    'unknown DELETE survives page close and never claims success from absence',
    (t) async {
      final identity = ChangeNotifier();
      addTearDown(identity.dispose);
      final store = MemoryPlacePendingStore();
      var deletes = 0;
      final client = placeFixtureClient((r) async {
        if (r.method == 'GET') {
          return placeResponse(
            placeReadFixture(
              signals: deletes == 0
                  ? [placeSignalFixture(kind: 'VISITED')]
                  : [],
            ),
          );
        }
        deletes++;
        throw StateError('lost committed response');
      });
      Widget page() => MaterialApp(
        home: PrivatePlaceMemoryPage(
          placeID: placeTarget,
          placeName: '合成测试地点',
          authorizationHeader: () => 'Bearer local',
          accountID: () => placeOwner,
          identityChanges: identity,
          client: client,
          apiBaseUrl: 'http://127.0.0.1',
          pendingStore: store,
        ),
      );
      await t.pumpWidget(page());
      await t.pumpAndSettle();
      await placePageScroll(t, '撤回这条声明');
      await t.tap(find.text('撤回这条声明'));
      await t.pumpAndSettle();
      await t.tap(find.text('确认撤回声明'));
      await t.pumpAndSettle();
      expect(store.items, isNotEmpty);
      await t.pumpWidget(const SizedBox());
      await t.pump();
      await t.pumpWidget(page());
      await t.pumpAndSettle();
      expect(find.textContaining('提交结果待核实'), findsOneWidget);
      expect(find.textContaining('已收到权威撤回回执'), findsNothing);
      expect(deletes, 1);
    },
  );
  testWidgets('organization cannot expose source or declaration controls', (
    t,
  ) async {
    final identity = ChangeNotifier();
    addTearDown(identity.dispose);
    var requests = 0;
    await t.pumpWidget(
      MaterialApp(
        home: PrivatePlaceMemoryPage(
          placeID: placeTarget,
          placeName: '合成测试地点',
          authorizationHeader: () => 'Bearer local',
          accountID: () => placeOwner,
          organizationWorkspaceID: () => 'organization',
          identityChanges: identity,
          client: placeFixtureClient((r) async {
            requests++;
            return placeResponse(
              placeReadFixture(signals: [placeSignalFixture()]),
            );
          }),
          apiBaseUrl: 'http://127.0.0.1',
          pendingStore: MemoryPlacePendingStore(),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(requests, 0);
    expect(find.text('检查并保存本人声明'), findsNothing);
    expect(find.textContaining('组织工作台不能查看'), findsOneWidget);
  });
  testWidgets(
    '320 wide, large type, keyboard inset confirmation remains scrollable',
    (t) async {
      await t.binding.setSurfaceSize(const Size(320, 640));
      addTearDown(() => t.binding.setSurfaceSize(null));
      final identity = ChangeNotifier();
      addTearDown(identity.dispose);
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context).copyWith(
              textScaler: const TextScaler.linear(2),
              viewInsets: const EdgeInsets.only(bottom: 160),
            ),
            child: child!,
          ),
          home: PrivatePlaceMemoryPage(
            placeID: placeTarget,
            placeName: '一个很长的合成地点名称需要换行而不能裁切',
            authorizationHeader: () => 'Bearer local',
            accountID: () => placeOwner,
            identityChanges: identity,
            client: placeFixtureClient(
              (r) async => placeResponse(placeReadFixture()),
            ),
            apiBaseUrl: 'http://127.0.0.1',
            pendingStore: MemoryPlacePendingStore(),
          ),
        ),
      );
      await t.pumpAndSettle();
      await placePageScroll(t, '检查并保存本人声明');
      await t.tap(find.text('检查并保存本人声明'));
      await t.pumpAndSettle();
      expect(find.text('确认保存本人声明'), findsOneWidget);
      expect(find.byType(SingleChildScrollView), findsWidgets);
      expect(t.takeException(), isNull);
      await t.tap(find.text('返回修改'));
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    'late source read after page disposal does not notify dead widget',
    (t) async {
      final identity = ChangeNotifier();
      addTearDown(identity.dispose);
      final wait = Completer<http.Response>();
      await t.pumpWidget(
        MaterialApp(
          home: PrivatePlaceMemoryPage(
            placeID: placeTarget,
            placeName: '合成测试地点',
            authorizationHeader: () => 'Bearer local',
            accountID: () => placeOwner,
            identityChanges: identity,
            client: placeFixtureClient((r) => wait.future),
            apiBaseUrl: 'http://127.0.0.1',
            pendingStore: MemoryPlacePendingStore(),
          ),
        ),
      );
      await t.pump();
      await t.pumpWidget(const SizedBox());
      wait.complete(placeResponse(placeReadFixture()));
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets('selected control semantics remain available', (t) async {
    final semantics = t.ensureSemantics();
    final identity = ChangeNotifier();
    addTearDown(identity.dispose);
    await t.pumpWidget(
      MaterialApp(
        home: PrivatePlaceMemoryPage(
          placeID: placeTarget,
          placeName: '合成测试地点',
          authorizationHeader: () => 'Bearer local',
          accountID: () => placeOwner,
          identityChanges: identity,
          client: placeFixtureClient(
            (r) async => placeResponse(placeReadFixture()),
          ),
          apiBaseUrl: 'http://127.0.0.1',
          pendingStore: MemoryPlacePendingStore(),
        ),
      ),
    );
    await t.pumpAndSettle();
    await placePageScroll(t, '我喜欢这里');
    expect(
      t.getSemantics(find.widgetWithText(ChoiceChip, '我喜欢这里')),
      matchesSemantics(
        label: '我喜欢这里',
        isSelected: true,
        hasSelectedState: true,
        hasEnabledState: true,
        hasFocusAction: true,
        isEnabled: true,
        isButton: true,
        hasTapAction: true,
        isFocusable: true,
      ),
    );
    expect(t.takeException(), isNull);
    semantics.dispose();
  });

  testWidgets(
    'hidden Place removes cached name and retains expired own withdrawal control',
    (t) async {
      final identity = ChangeNotifier();
      addTearDown(identity.dispose);
      var deletes = 0;
      await t.pumpWidget(
        MaterialApp(
          home: PrivatePlaceMemoryPage(
            placeID: placeTarget,
            placeName: '不应泄漏的旧公开名称',
            authorizationHeader: () => 'Bearer local',
            accountID: () => placeOwner,
            identityChanges: identity,
            apiBaseUrl: 'http://127.0.0.1',
            pendingStore: MemoryPlacePendingStore(),
            client: placeFixtureClient((r) async {
              if (r.method == 'GET') {
                if (r.url.path.endsWith('/declarations')) {
                  return placeResponse(
                    placeControlsFixture(
                      declarations: [
                        placeDeclarationFixture(
                          status: 'EXPIRED',
                          until: DateTime.now().toUtc().subtract(
                            const Duration(days: 1),
                          ),
                        ),
                      ],
                    ),
                  );
                }
                return http.Response('{}', 404);
              }
              deletes++;
              return placeResponse(placeReceiptFixture(r));
            }),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('不应泄漏的旧公开名称'), findsNothing);
      expect(find.text('检查并保存本人声明'), findsNothing);
      await placePageScroll(t, '撤回这条声明');
      expect(find.text('已过期，当前不作为地点信号'), findsOneWidget);
      await t.tap(find.text('撤回这条声明'));
      await t.pumpAndSettle();
      expect(find.text('所选地点的本人声明'), findsOneWidget);
      await t.tap(find.text('确认撤回声明'));
      await t.pumpAndSettle();
      expect(deletes, 1);
      expect(t.takeException(), isNull);
    },
  );
}
