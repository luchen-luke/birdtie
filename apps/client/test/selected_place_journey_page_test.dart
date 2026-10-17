import 'dart:async';
import 'package:birdtie_client/src/content/selected_place_journey.dart';
import 'package:birdtie_client/src/content/collection_control_pending_store.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:birdtie_client/src/workspace/selected_place_followup.dart';
import 'package:birdtie_client/src/workspace/agent_reply_membership.dart';
import 'package:http/testing.dart';
import 'package:birdtie_client/src/content/personal_collection_page.dart';
import 'package:birdtie_client/src/content/selected_place_journey_page.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart'
    show MapWorkspace;
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'selected_place_journey_test.dart';

void viewport(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Future<void> openJourney(WidgetTester t, JourneyHarness h) async {
  await t.pumpWidget(
    MaterialApp(
      home: Builder(
        builder: (context) => Scaffold(
          body: TextButton(
            onPressed: () => Navigator.of(context).push<void>(
              MaterialPageRoute(
                builder: (_) => SelectedPlaceJourneyPage(
                  controller: h.controller,
                  cityName: '阿伯丁',
                ),
              ),
            ),
            child: const Text('同一对话原草稿'),
          ),
        ),
      ),
    ),
  );
  await t.tap(find.text('同一对话原草稿'));
  await t.pumpAndSettle();
}

Future<void> saveVisible(WidgetTester t) async {
  final save = find.byKey(const Key('collection-save'));
  await t.ensureVisible(save);
  await t.pumpAndSettle();
  await t.tap(save);
  await journeyPagePump(t);
}

Future<void> journeyPagePump(WidgetTester t) async {
  // Existing control ports consume a real streamed response, and the preview
  // has a live expiry. Let I/O finish in bounded rounds instead of waiting for
  // every scheduled frame to stop.
  for (var i = 0; i < 3; i++) {
    await t.pump(const Duration(milliseconds: 200));
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 30)),
    );
  }
  await t.pump();
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  testWidgets(
    'real owned-map suspension and fake-clock reply expiry do not cancel independently reread public Journey, explicit permission retirement does',
    (t) async {
      final h = JourneyHarness();
      addTearDown(h.dispose);
      final identity = ValueNotifier(0);
      addTearDown(identity.dispose);
      var visible = true;
      DateTime now() => t.binding.clock.now().toUtc();
      final privateClient = MockClient((request) async {
        expect(request.url.path, '/v1/me/map-content');
        final at = now();
        return journeyResponse({
          'schemaVersion': 'map-content-v2',
          'scope': 'MAP_CONTENT',
          'viewerAccountId': journeyOwner,
          'cityId': journeyCity,
          'zoom': double.parse(request.url.queryParameters['zoom']!),
          'filterScope': 'DEFAULT_CONTENT',
          'totalContentCount': 1,
          'totalAnchorCount': 1,
          'nextCursor': null,
          'truncated': false,
          'bounds': {
            for (final k in ['west', 'south', 'east', 'north'])
              k: double.parse(request.url.queryParameters[k]!),
          },
          'sourceVersion': 'a' * 64,
          'observedAt': at.toIso8601String(),
          'validUntil': at.add(const Duration(seconds: 30)).toIso8601String(),
          'pins': [
            {
              'kind': 'PLACE_CONTENT',
              'markerId': 'place:$journeyPlace',
              'entityId': journeyPlace,
              'anchorCount': 1,
              'placeId': journeyPlace,
              'name': '仅旧私人地图读许可',
              'categoryCode': 'culture.museum',
              'cityId': journeyCity,
              'anchor': {
                'coordinateSystem': 'wgs84',
                'precision': 'point',
                'latitude': 57.2,
                'longitude': -2.1,
              },
              'visibilitySources': ['SELF_PRIVATE'],
              'sourceVersion': 'a' * 64,
              'momentCount': 1,
              'momentsTruncated': false,
              'cover': null,
              'moments': [
                {
                  'id': journeyEntry,
                  'revision': 1,
                  'visibilitySource': 'SELF_PRIVATE',
                  'detailPath': '/v1/me/map-content/moments/$journeyEntry',
                },
              ],
            },
          ],
        });
      });
      addTearDown(privateClient.close);
      final owned = OwnedMapController(
        client: privateClient,
        apiBaseUrl: () => 'https://synthetic.invalid',
        ownerID: () => journeyOwner,
        authorizationHeader: () => h.token,
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => visible,
        now: now,
      );
      addTearDown(owned.dispose);
      await t.runAsync(() async {
        // Start and consume the actual streamed map read in the same I/O
        // zone; awaiting a fake-zone flight from runAsync deadlocks the test.
        owned.updateQuery(
          journeyCity,
          const MapBounds(west: -2.3, south: 57, east: -1.9, north: 57.4),
          selectedMarkerID: 'place:$journeyPlace',
        );
        await owned.refresh();
      });
      expect(
        owned.error,
        isNull,
        reason: 'The synthetic map envelope must pass the real strict decoder.',
      );
      expect(
        owned.items,
        hasLength(1),
        reason:
            'requests=${owned.requests}, now=${now()}, current=${owned.current}',
      );
      final pin = owned.items.single, permission = owned.permissionEpoch;
      final reply = AgentReplyProjectionLifetime(
        validUntil: now().add(const Duration(seconds: 30)),
        current: () => true,
        now: now,
      );
      final selection = SelectedPlaceFollowup(
        placeID: journeyPlace,
        cityID: journeyCity,
        taskID: 'same-native-task',
        current: () => owned.sourceCurrent(pin) && reply.current,
        contextCurrent: () =>
            owned.permissionEpoch == permission && h.selected && h.taskCurrent,
      );
      final c = SelectedPlaceJourneyController(
        selection: selection,
        day: 2,
        ownerID: () => journeyOwner,
        authorizationHeader: () => h.token,
        sourceCurrent: () => h.taskCurrent,
        identityChanges: Listenable.merge([owned, h.changes]),
        client: h.client,
        apiBaseUrl: 'https://synthetic.invalid',
        controlPendingStore: MemoryCollectionControlPendingStore(),
        publicationRecoveryReader: (_) async => null,
      );
      addTearDown(c.dispose);
      await c.load();
      expect(c.placeName, '合成当前地点');
      visible = false;
      owned.updateRefreshVisibility();
      await t.pump(const Duration(seconds: 31));
      expect(owned.sourceCurrent(pin), isFalse);
      expect(reply.current, isFalse);
      expect(selection.current, isFalse);
      expect(selection.contextCurrent, isTrue);
      expect(c.current, isTrue);
      expect(await c.choose(), isTrue);
      expect(await c.collections.save(), isTrue);
      await t.pump();
      await t.pump();
      expect(c.receipt?.revision, 1);
      expect(h.creates, 1);
      owned.retire();
      expect(selection.contextCurrent, isFalse);
      expect(c.current, isFalse);
      expect(c.receipt, isNull);
      expect(await c.collections.save(), isFalse);
      expect(h.creates, 1);
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    'current Now host compiles and target cancellation returns original conversation without domain writes',
    (t) async {
      expect(MapWorkspace, isNotNull);
      viewport(t);
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await openJourney(t, h);
      expect(find.text('合成当前地点'), findsOneWidget);
      expect(find.text('合成本人旅程'), findsOneWidget);
      await t.tap(find.text('取消，返回对话'));
      await t.pumpAndSettle();
      expect(find.text('同一对话原草稿'), findsOneWidget);
      expect(h.creates + h.puts + h.previews, 0);
      expect(t.takeException(), isNull);
    },
  );

  testWidgets(
    'normal390 selected place enters original editor and real save receives exact readback',
    (t) async {
      viewport(t);
      t.view.padding = const FakeViewPadding(top: 24, bottom: 24);
      t.view.viewPadding = const FakeViewPadding(top: 24, bottom: 24);
      addTearDown(t.view.resetPadding);
      addTearDown(t.view.resetViewPadding);
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await openJourney(t, h);
      await t.tap(find.byKey(const Key('selected-place-new-journey')));
      await t.pumpAndSettle();
      expect(find.byType(PersonalCollectionPage), findsOneWidget);
      final header = find.byKey(
        const Key('selected-place-journey-editor-header'),
      );
      final headerRect = t.getRect(header);
      expect(headerRect.left, 0);
      expect(headerRect.top, 0);
      expect(headerRect.width, 390);
      expect(t.getTopLeft(find.text('合成当前地点 · 第2天，检查后保存')).dy, 24 + 8);
      final editorMedia = MediaQuery.of(
        t.element(find.byType(PersonalCollectionPage)),
      );
      expect(editorMedia.padding.top, 0);
      expect(editorMedia.viewPadding.top, 0);
      expect(editorMedia.padding.bottom, 24);
      final editorBar = t.getRect(
        find.descendant(
          of: find.byType(PersonalCollectionPage),
          matching: find.byType(AppBar),
        ),
      );
      expect(editorBar.top, headerRect.bottom);
      expect(editorBar.height, kToolbarHeight);
      expect(h.creates, 0);
      await saveVisible(t);
      expect(h.creates, 1);
      expect(h.controller.receipt?.revision, 1);
      expect(find.textContaining('已核实保存到'), findsOneWidget);
      expect(t.takeException(), isNull);
    },
  );

  testWidgets(
    'existing target keeps explicit update preview cancel then confirmed save',
    (t) async {
      viewport(t);
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await openJourney(t, h);
      await t.tap(find.text('合成本人旅程'));
      await t.pumpAndSettle();
      await saveVisible(t);
      expect(find.text('确认保存修改'), findsOneWidget);
      expect(h.puts, 0);
      await t.tap(find.text('取消'));
      await journeyPagePump(t);
      expect(h.puts, 0);
      expect(h.controller.receipt, isNull);
      await saveVisible(t);
      await t.tap(find.byKey(const Key('collection-control-confirm')));
      await journeyPagePump(t);
      expect(h.puts, 1);
      expect(h.controller.receipt?.revision, 5);
      expect(find.textContaining('已核实保存到'), findsOneWidget);
      expect(t.takeException(), isNull);
    },
  );

  testWidgets(
    'identity retirement removes draft actions and never sends old selection',
    (t) async {
      viewport(t);
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await openJourney(t, h);
      h.owner = '18000000-0000-4000-8000-000000000099';
      h.changes.value++;
      await t.pumpAndSettle();
      expect(find.byKey(const Key('selected-place-new-journey')), findsNothing);
      expect(find.textContaining('已变化'), findsOneWidget);
      expect(h.creates + h.puts, 0);
      expect(t.takeException(), isNull);
    },
  );
  testWidgets(
    'ordinary back preserves an uncertain original create for recovery without replay',
    (t) async {
      viewport(t);
      final h = JourneyHarness();
      addTearDown(h.dispose);
      await openJourney(t, h);
      await t.tap(find.byKey(const Key('selected-place-new-journey')));
      await t.pumpAndSettle();
      h.createUnknown = true;
      await saveVisible(t);
      expect(h.controller.collections.uncertain, isTrue);
      await t.pageBack();
      await t.pump(const Duration(milliseconds: 300));
      expect(find.byType(PersonalCollectionPage), findsOneWidget);
      expect(h.creates, 1);
      expect(h.controller.collections.retainedLocalDraft!.unknown, isTrue);
      expect(t.takeException(), isNull);
    },
  );
}
