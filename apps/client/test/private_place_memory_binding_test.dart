import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/private_place_memory_api.dart';
import 'package:birdtie_client/src/workspace/private_place_memory_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'private_place_memory_api_test.dart';
import 'private_place_memory_page_test.dart' show placePageScroll;

const retiredPlace = '地点页面连接已变更，请返回后重新打开。';
const retiredPrivatePlace = '地点记录连接已变更，请返回后重新打开。';

class PlaceOwnedClient implements HttpClient {
  int closes = 0;
  @override
  void close({bool force = false}) {
    closes++;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class PlaceBindingChanges extends ChangeNotifier {
  bool get listening => hasListeners;
  void change() => notifyListeners();
}

class PlaceBindingClient extends MockClient {
  PlaceBindingClient(super.handler);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

http.Response placePublicResponse(Object value) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': value})), 200);
http.Response placeDetailReply(http.Request r) {
  if (r.url.path.endsWith('/activities') || r.url.path == '/v1/me/moments') {
    return placePublicResponse([]);
  }
  if (r.url.path.endsWith('/venue') || r.url.path.endsWith('/social-history')) {
    return http.Response('{}', 404);
  }
  return placePublicResponse({
    'id': placeTarget,
    'name': '本地合成地点',
    'source': {'label': '合成来源'},
    'location': {'precision': 'none'},
  });
}

Widget bindingPrivatePage(
  http.Client client,
  ChangeNotifier changes,
  MemoryPlacePendingStore store,
  String? Function() token, {
  String base = 'http://old-fixture',
  Key key = const ValueKey('same-private'),
}) => MaterialApp(
  home: PrivatePlaceMemoryPage(
    key: key,
    placeID: placeTarget,
    placeName: '本地合成地点',
    authorizationHeader: token,
    accountID: bindingOwner,
    identityChanges: changes,
    client: client,
    apiBaseUrl: base,
    pendingStore: store,
  ),
);
String? bindingOwner() => placeOwner;
String? bindingToken() => 'Bearer local';

void main() {
  testWidgets(
    '320 wide font2 retired private route has reachable semantic return without clearing recovery',
    (t) async {
      t.view.physicalSize = const Size(320, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final semantics = t.ensureSemantics();
      final changes = ChangeNotifier(),
          base = ValueNotifier('http://old-fixture'),
          store = MemoryPlacePendingStore();
      var writes = 0;
      final c = PlaceBindingClient((r) async {
        if (r.method != 'GET') writes++;
        return placeResponse(
          r.url.path.endsWith('/declarations')
              ? placeControlsFixture()
              : placeReadFixture(),
        );
      });
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) => ValueListenableBuilder<String>(
                      valueListenable: base,
                      builder: (context, value, child) =>
                          PrivatePlaceMemoryPage(
                            key: const ValueKey('mobile'),
                            placeID: placeTarget,
                            placeName: '本地合成地点',
                            authorizationHeader: bindingToken,
                            accountID: bindingOwner,
                            identityChanges: changes,
                            client: c,
                            apiBaseUrl: value,
                            pendingStore: store,
                          ),
                    ),
                  ),
                ),
                child: const Text('原地点入口'),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('原地点入口'));
      await t.pumpAndSettle();
      base.value = 'http://new-fixture';
      await t.pumpAndSettle();
      await t.scrollUntilVisible(find.text('返回'), 180);
      await t.ensureVisible(find.text('返回'));
      await t.pumpAndSettle();
      final button = find.ancestor(
        of: find.text('返回'),
        matching: find.byType(FilledButton),
      );
      expect(t.getSize(button).height, greaterThanOrEqualTo(48));
      expect(t.getSize(button).width, greaterThanOrEqualTo(48));
      expect(t.getSemantics(button).label, contains('返回'));
      expect(t.takeException(), isNull);
      await t.tap(button);
      await t.pumpAndSettle();
      expect(find.text('原地点入口'), findsOneWidget);
      expect(writes, 0);
      expect(store.items, isEmpty);
      await t.pumpWidget(const SizedBox());
      semantics.dispose();
      c.close();
      changes.dispose();
      base.dispose();
    },
  );
  testWidgets(
    'owned Place transport closes exactly once after replacement by borrowed client',
    (t) async {
      final owned = PlaceOwnedClient();
      final borrowed = PlaceBindingClient((r) async => placeDetailReply(r));
      String? anonymous() => null;
      Widget page(http.Client? client) => MaterialApp(
        home: Scaffold(
          body: PlaceDetailSheet(
            key: const ValueKey('owned'),
            placeID: placeTarget,
            authorizationHeader: anonymous,
            client: client,
            apiBaseUrl: 'http://fixture',
            onOpenActivity: (_) {},
          ),
        ),
      );
      await HttpOverrides.runZoned(() async {
        await t.pumpWidget(page(null));
        await t.pumpAndSettle();
        await t.pumpWidget(page(borrowed));
        await t.pumpAndSettle();
        await t.pumpWidget(const SizedBox());
        expect(owned.closes, 1);
        expect(borrowed.closes, 0);
        expect(t.takeException(), isNull);
      }, createHttpClient: (_) => owned);
      borrowed.close();
    },
  );

  testWidgets(
    'normal Me entry and explicit original Agent CAS save remain available',
    (t) async {
      final changes = ChangeNotifier(), store = MemoryPlacePendingStore();
      var puts = 0, meReads = 0;
      final c = PlaceBindingClient((r) async {
        expect(r.headers['authorization'], 'Bearer local');
        expect(r.url.host, 'old-fixture');
        if (r.url.path == '/v1/me') {
          meReads++;
          return placePublicResponse({
            'id': placeOwner,
            'accountType': 'person',
          });
        }
        if (r.method == 'PUT') {
          puts++;
          final body = jsonDecode(r.body) as Map;
          expect(body['agentId'], placeAgent);
          expect(body['expectedVersion'], 0);
          return placeResponse(placeReceiptFixture(r));
        }
        if (r.url.path.contains('/v1/me/places/')) {
          return placeResponse(
            r.url.path.endsWith('/declarations')
                ? placeControlsFixture()
                : placeReadFixture(),
          );
        }
        return placeDetailReply(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PlaceDetailSheet(
              placeID: placeTarget,
              authorizationHeader: bindingToken,
              workspaceChanges: changes,
              client: c,
              apiBaseUrl: 'http://old-fixture',
              privatePlacePendingStore: store,
              onOpenActivity: (_) {},
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.ensureVisible(find.text('我的地点记录'));
      await t.pumpAndSettle();
      await t.tap(find.text('我的地点记录'));
      await t.pumpAndSettle();
      expect(find.byType(PrivatePlaceMemoryPage), findsOneWidget);
      expect(meReads, 1);
      await placePageScroll(t, '检查并保存本人声明');
      await t.tap(find.text('检查并保存本人声明'));
      await t.pumpAndSettle();
      expect(find.text('检查本人声明'), findsOneWidget);
      expect(puts, 0);
      await t.tap(find.text('确认保存本人声明'));
      await t.pumpAndSettle();
      expect(puts, 1);
      expect(store.items, isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      expect(c.closes, 0);
      c.close();
      changes.dispose();
    },
  );

  testWidgets('open nested approval base A B A is retired and cannot send', (
    t,
  ) async {
    final changes = ChangeNotifier(),
        base = ValueNotifier('http://old-fixture'),
        store = MemoryPlacePendingStore();
    final requests = <http.Request>[];
    final c = PlaceBindingClient((r) async {
      requests.add(r);
      if (r.url.path == '/v1/me') {
        return placePublicResponse({'id': placeOwner, 'accountType': 'person'});
      }
      if (r.url.path.contains('/v1/me/places/')) {
        return placeResponse(
          r.url.path.endsWith('/declarations')
              ? placeControlsFixture()
              : placeReadFixture(),
        );
      }
      return placeDetailReply(r);
    });
    await t.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<String>(
          valueListenable: base,
          builder: (context, value, child) => Scaffold(
            body: PlaceDetailSheet(
              key: const ValueKey('nested'),
              placeID: placeTarget,
              authorizationHeader: bindingToken,
              workspaceChanges: changes,
              client: c,
              apiBaseUrl: value,
              privatePlacePendingStore: store,
              onOpenActivity: (_) {},
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.ensureVisible(find.text('我的地点记录'));
    await t.pumpAndSettle();
    await t.tap(find.text('我的地点记录'));
    await t.pumpAndSettle();
    await placePageScroll(t, '检查并保存本人声明');
    await t.tap(find.text('检查并保存本人声明'));
    await t.pumpAndSettle();
    final count = requests.length;
    base.value = 'http://new-fixture';
    await t.pumpAndSettle();
    base.value = 'http://old-fixture';
    await t.pumpAndSettle();
    expect(find.text('确认保存本人声明'), findsNothing);
    expect(find.byType(PrivatePlaceMemoryPage), findsNothing);
    expect(requests.length, count);
    expect(requests.every((r) => r.method == 'GET'), isTrue);
    expect(store.items, isEmpty);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    c.close();
    changes.dispose();
    base.dispose();
  });

  for (final changed in [
    'base',
    'client',
    'listener',
    'pendingStore',
    'ownerGetter',
    'workspaceGetter',
  ]) {
    testWidgets(
      'private $changed A B A retires without loading or clearing pending',
      (t) async {
        final changes = PlaceBindingChanges(),
            nextChanges = PlaceBindingChanges(),
            store = MemoryPlacePendingStore(),
            nextStore = MemoryPlacePendingStore();
        var reads = 0, nextReads = 0;
        final c = PlaceBindingClient((r) async {
          reads++;
          return placeResponse(
            r.url.path.endsWith('/declarations')
                ? placeControlsFixture()
                : placeReadFixture(),
          );
        });
        final next = PlaceBindingClient((r) async {
          nextReads++;
          return placeResponse(placeReadFixture());
        });
        String? nextOwner() => placeOwner;
        String? workspace() => null;
        String? nextWorkspace() => null;
        Widget page(bool other) => MaterialApp(
          home: PrivatePlaceMemoryPage(
            key: const ValueKey('permanent'),
            placeID: placeTarget,
            placeName: '合成地点',
            authorizationHeader: bindingToken,
            accountID: other && changed == 'ownerGetter'
                ? nextOwner
                : bindingOwner,
            organizationWorkspaceID: other && changed == 'workspaceGetter'
                ? nextWorkspace
                : workspace,
            identityChanges: other && changed == 'listener'
                ? nextChanges
                : changes,
            client: other && changed == 'client' ? next : c,
            apiBaseUrl: other && changed == 'base'
                ? 'http://new-fixture'
                : 'http://old-fixture',
            pendingStore: other && changed == 'pendingStore'
                ? nextStore
                : store,
          ),
        );
        await t.pumpWidget(page(false));
        await t.pumpAndSettle();
        expect(reads, 2);
        expect(changes.listening, isTrue);
        await t.pumpWidget(page(true));
        await t.pumpAndSettle();
        await t.pumpWidget(page(false));
        await t.pumpAndSettle();
        expect(find.text(retiredPrivatePlace), findsOneWidget);
        expect(changes.listening, isFalse);
        expect(nextChanges.listening, isFalse);
        changes.change();
        nextChanges.change();
        await t.pumpAndSettle();
        expect(reads, 2);
        expect(nextReads, 0);
        expect(c.closes, 0);
        expect(next.closes, 0);
        expect(store.items, isEmpty);
        expect(nextStore.items, isEmpty);
        await t.pumpWidget(const SizedBox());
        c.close();
        next.close();
        changes.dispose();
        nextChanges.dispose();
      },
    );
  }

  for (final phase in ['GET', 'PUT', 'DELETE', 'unknownPUT', 'unknownDELETE']) {
    testWidgets(
      'late $phase on retired source cannot restore success or lose original recovery key',
      (t) async {
        final changes = ChangeNotifier(), store = MemoryPlacePendingStore();
        final pending = Completer<http.Response>();
        var writes = 0;
        http.Request? operation;
        final c = PlaceBindingClient((r) async {
          if (r.method == 'GET') {
            if (phase == 'GET') return pending.future;
            return placeResponse(
              r.url.path.endsWith('/declarations')
                  ? placeControlsFixture(
                      declarations: phase.contains('DELETE')
                          ? [placeDeclarationFixture()]
                          : [],
                    )
                  : placeReadFixture(
                      signals: phase.contains('DELETE')
                          ? [placeSignalFixture()]
                          : [],
                    ),
            );
          }
          writes++;
          operation = r;
          return pending.future;
        });
        await t.pumpWidget(bindingPrivatePage(c, changes, store, bindingToken));
        await t.pump();
        if (phase != 'GET') {
          await t.pumpAndSettle();
          final deleting = phase.contains('DELETE');
          await placePageScroll(t, deleting ? '撤回这条声明' : '检查并保存本人声明');
          await t.tap(find.text(deleting ? '撤回这条声明' : '检查并保存本人声明'));
          await t.pumpAndSettle();
          await t.tap(find.text(deleting ? '确认撤回声明' : '确认保存本人声明'));
          await t.pump();
          expect(writes, 1);
          expect(store.items.length, 1);
        }
        final oldRecovery = Map.of(store.items);
        await t.pumpWidget(
          bindingPrivatePage(
            c,
            changes,
            store,
            bindingToken,
            base: 'http://new-fixture',
          ),
        );
        await t.pumpAndSettle();
        if (phase.startsWith('unknown')) {
          pending.completeError(StateError('lost native receipt'));
        } else {
          pending.complete(
            phase == 'GET'
                ? placeResponse(placeControlsFixture())
                : placeResponse(placeReceiptFixture(operation!)),
          );
        }
        await t.pumpAndSettle();
        expect(find.text(retiredPrivatePlace), findsOneWidget);
        expect(writes, phase == 'GET' ? 0 : 1);
        expect(store.items, oldRecovery);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        expect(c.closes, 0);
        c.close();
        changes.dispose();
      },
    );
  }

  testWidgets(
    'stop old recovery confirmation never deletes source A or B pending on rebind',
    (t) async {
      final changes = ChangeNotifier(), store = MemoryPlacePendingStore();
      final old = PendingPlaceDeclaration(
        memoryID: placeMemory,
        agentID: placeAgent,
        expectedVersion: 0,
        sessionFingerprint: placeSessionFingerprint('Bearer local'),
        deleting: false,
        draft: PlaceDeclarationDraft(
          kind: 'LIKED',
          visibility: 'PRIVATE',
          validUntil: DateTime.now().toUtc().add(const Duration(days: 7)),
        ),
      );
      final other = PendingPlaceDeclaration(
        memoryID: '27000000-0000-4000-8000-000000000005',
        agentID: placeAgent,
        expectedVersion: 0,
        sessionFingerprint: placeSessionFingerprint('Bearer local'),
        deleting: false,
        draft: old.draft,
      );
      await store.write('http://old-fixture', placeOwner, placeTarget, old);
      await store.write('http://new-fixture', placeOwner, placeTarget, other);
      final snapshot = Map.of(store.items);
      var writes = 0;
      final c = PlaceBindingClient((r) async {
        if (r.method != 'GET') writes++;
        return placeResponse(
          r.url.path.endsWith('/declarations')
              ? placeControlsFixture()
              : placeReadFixture(),
        );
      });
      await t.pumpWidget(bindingPrivatePage(c, changes, store, bindingToken));
      await t.pumpAndSettle();
      await placePageScroll(t, '停止本机核实（结果仍未知）');
      await t.tap(find.text('停止本机核实（结果仍未知）'));
      await t.pumpAndSettle();
      expect(find.text('停止本机核实？'), findsOneWidget);
      await t.pumpWidget(
        bindingPrivatePage(
          c,
          changes,
          store,
          bindingToken,
          base: 'http://new-fixture',
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('停止本机核实'), findsNothing);
      expect(store.items, snapshot);
      expect(writes, 0);
      // An explicitly new State recovers only its original environment/owner/place key.
      await t.pumpWidget(
        bindingPrivatePage(
          c,
          changes,
          store,
          bindingToken,
          base: 'http://old-fixture',
          key: const ValueKey('fresh'),
        ),
      );
      await t.pumpAndSettle();
      expect(find.textContaining('提交结果待核实'), findsOneWidget);
      expect(store.items, snapshot);
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      c.close();
      changes.dispose();
    },
  );
  testWidgets(
    'late Me from old endpoint cannot open a private route on new endpoint',
    (t) async {
      final changes = ChangeNotifier(),
          base = ValueNotifier('http://old-fixture');
      final pending = Completer<http.Response>();
      final requests = <http.Request>[];
      final c = PlaceBindingClient((r) async {
        requests.add(r);
        if (r.url.path == '/v1/me') return pending.future;
        if (r.url.path.contains('/v1/me/places/')) {
          return placeResponse(
            r.url.path.endsWith('/declarations')
                ? placeControlsFixture()
                : placeReadFixture(),
          );
        }
        return placeDetailReply(r);
      });
      final store = MemoryPlacePendingStore();
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<String>(
            valueListenable: base,
            builder: (context, value, child) => Scaffold(
              body: PlaceDetailSheet(
                key: const ValueKey('same-place'),
                placeID: placeTarget,
                authorizationHeader: bindingToken,
                workspaceChanges: changes,
                client: c,
                apiBaseUrl: value,
                privatePlacePendingStore: store,
                onOpenActivity: (_) {},
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.ensureVisible(find.text('我的地点记录'));
      await t.pumpAndSettle();
      await t.tap(find.text('我的地点记录'));
      await t.pump();
      expect(requests.where((r) => r.url.path == '/v1/me').length, 1);
      base.value = 'http://new-fixture';
      await t.pump();
      pending.complete(
        placePublicResponse({'id': placeOwner, 'accountType': 'person'}),
      );
      await t.pumpAndSettle();
      expect(
        requests.where((r) => r.url.path.contains('/v1/me/places/')),
        isEmpty,
      );
      expect(find.byType(PrivatePlaceMemoryPage), findsNothing);
      expect(find.text(retiredPlace), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      expect(c.closes, 0);
      c.close();
      changes.dispose();
      base.dispose();
    },
  );

  testWidgets(
    'borrowed Place transport must not close after widget client becomes null',
    (t) async {
      final c = PlaceBindingClient((r) async => placeDetailReply(r));
      Widget page(http.Client? client) => MaterialApp(
        home: Scaffold(
          body: PlaceDetailSheet(
            key: const ValueKey('place'),
            placeID: placeTarget,
            authorizationHeader: () => null,
            client: client,
            apiBaseUrl: 'http://fixture',
            onOpenActivity: (_) {},
          ),
        ),
      );
      await t.pumpWidget(page(c));
      await t.pumpAndSettle();
      await t.pumpWidget(page(null));
      await t.pumpAndSettle();
      await t.pumpWidget(const SizedBox());
      expect(c.closes, 0);
      c.close();
    },
  );

  testWidgets(
    'same-key credential getter replacement destroys concrete private approval',
    (t) async {
      final changes = ChangeNotifier(), store = MemoryPlacePendingStore();
      var writes = 0;
      final c = placeFixtureClient((r) async {
        if (r.method != 'GET') writes++;
        return placeResponse(placeReadFixture());
      });
      String? nextToken() => 'Bearer local';
      await t.pumpWidget(bindingPrivatePage(c, changes, store, bindingToken));
      await t.pumpAndSettle();
      await placePageScroll(t, '检查并保存本人声明');
      await t.tap(find.text('检查并保存本人声明'));
      await t.pumpAndSettle();
      expect(find.text('检查本人声明'), findsOneWidget);
      await t.pumpWidget(bindingPrivatePage(c, changes, store, nextToken));
      await t.pumpAndSettle();
      expect(find.text('确认保存本人声明'), findsNothing);
      expect(find.text(retiredPrivatePlace), findsOneWidget);
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      c.close();
      changes.dispose();
    },
  );
}
