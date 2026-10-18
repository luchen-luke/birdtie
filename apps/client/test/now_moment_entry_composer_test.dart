import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/moment_interaction.dart';
import 'package:birdtie_client/src/workspace/moment_thread_controller.dart';
import 'package:birdtie_client/src/workspace/moment_thread_panel.dart';
import 'package:birdtie_client/src/workspace/now_moment_editor_host.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'map_layers_api_test.dart' show mapTestBounds;
import 'now_public_moment_composer_test.dart' show page;
import 'now_scope_recovery_test.dart'
    show NowFixtureCity, NowFixtureSource, nowField;
import 'public_moment_api_test.dart' show publicMomentV3Data;

const _owner = '84000000-0000-4000-8000-000000000001';
const _other = '84000000-0000-4000-8000-000000000002';
const _moment = '84000000-0000-4000-8000-000000000003';
const _origin = 'https://now-editor-reader.test';
const _original = '原已发布的编辑正文';
const _editing = '编辑器尚未公开的新正文';
const _publicBody = '服务器当前公开正文';
const _commentDraft = '只属于这条动态的评论草稿';
const _mapPlaces = [
  '84000000-0000-4000-8000-000000000010',
  '84000000-0000-4000-8000-000000000011',
];
const _mapMoments = [
  '84000000-0000-4000-8000-000000000012',
  '84000000-0000-4000-8000-000000000013',
];
const _mapTitle = '地图B公开动态';

http.Response _json(Object value, {int status = 200}) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': value})),
  status,
  headers: {
    'content-type': 'application/json; charset=utf-8',
    'cache-control': 'no-store',
  },
);

// Real Now host and its existing editor capability, over synthetic read-only
// HTTP. No production authentication, upload, publication or media fixture.
class _EntryFixture {
  _EntryFixture({
    this.readLease = const Duration(seconds: 50),
    this.withMap = false,
  });

  final Duration readLease;
  final bool withMap;
  final auth = SeedTestAuth()
    ..token = 'Bearer synthetic-editor-reader-A'
    ..owner = _owner;
  final city = NowFixtureCity();
  late final source = NowFixtureSource(city);
  final calls = <http.Request>[];
  Completer<http.Response>? interactionRead;
  late final client = MockClient((request) async {
    calls.add(request);
    if (request.method != 'GET') {
      throw StateError('This reader fixture must never write HTTP');
    }
    final path = request.url.path;
    if (withMap) {
      final response = mapReply(request);
      if (response != null) return response;
    }
    if (path == '/v1/me/moments/$_moment/thread') {
      // A published standalone Moment legitimately uses the existing fallback.
      return _json({}, status: 404);
    }
    if (path == '/v1/me/moments/$_moment') {
      return _json({
        'id': _moment,
        'authorAccountId': _owner,
        'cityId': '',
        'placeId': '',
        'title': '原已发布标题',
        'body': _original,
        'timePrecision': 'unknown',
        'locationPrecision': 'none',
        'status': 'published',
        'visibility': 'public',
        'revision': 2,
      });
    }
    if (path == '/v1/me/moments/$_moment/locations' ||
        path == '/v1/moments/$_moment/locations') {
      final now = DateTime.now().toUtc();
      return _json({
        'schemaVersion': 'moment-locations-v2',
        'momentId': _moment,
        'momentRevision': 2,
        'items': <Object>[],
        'sourceVersion': 'a' * 64,
        'observedAt': now.toIso8601String(),
        'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
      });
    }
    if (path == '/v1/moments/$_moment') {
      return _json({
        ...publicMomentV3Data(photo: false, lease: readLease),
        'id': _moment,
        'body': _publicBody,
      });
    }
    if (path == '/v1/moments/$_moment/interactions') {
      return interactionRead?.future ?? _json(interactions());
    }
    if (path == '/v1/moments/$_moment/thread') {
      return _json({}, status: 404);
    }
    // Independent existing catalog and social readers may request empty data.
    return _json(<Object>[]);
  });
  late final moments = PrivateMomentController(
    client: client,
    apiBaseUrl: _origin,
    authorizationHeader: () => auth.authorizationHeader,
    ownerID: () => auth.accountID,
    identityChanges: auth,
    pendingStore: MemoryMomentControlPendingStore(),
  );

  Map<String, dynamic> interactions() => {...page(), 'momentId': _moment};

  http.Response? mapReply(http.Request request) {
    final path = request.url.path;
    final now = DateTime.now().toUtc();
    final lease = {
      'observedAt': now.toIso8601String(),
      'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
    };
    const version =
        'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
    Map<String, Object?> detail(int n) => {
      'schemaVersion': 'map-content-moment-v1',
      'viewerAccountId': _owner,
      'id': _mapMoments[n],
      'revision': 2,
      'visibilitySource': 'BIRDTIE_PUBLIC_MOMENT',
      'canEdit': false,
      'status': 'published',
      'visibility': 'public',
      'title': n == 0 ? _mapTitle : '另一地图公开动态',
      'body': '地图当前公开正文$n',
      'createdAt': '2026-01-01T00:00:00Z',
      'locations': [
        {'placeId': _mapPlaces[n], 'name': '地图地点$n', 'anchor': null},
      ],
      'sourceVersion': version,
      ...lease,
      'media': [],
    };
    if (path == '/v1/me/map-content') {
      return _json({
        'schemaVersion': 'map-content-v2',
        'scope': 'MAP_CONTENT',
        'viewerAccountId': _owner,
        'cityId': 'alpha',
        'bounds': {
          for (final key in ['west', 'south', 'east', 'north'])
            key: double.parse(request.url.queryParameters[key]!),
        },
        'zoom': double.parse(request.url.queryParameters['zoom'] ?? '12'),
        'sourceVersion': version,
        'truncated': false,
        'filterScope': 'DEFAULT_CONTENT',
        'totalContentCount': 2,
        'totalAnchorCount': 2,
        'nextCursor': null,
        ...lease,
        'pins': [
          for (var n = 0; n < 2; n++)
            {
              'markerId': 'place:${_mapPlaces[n]}',
              'kind': 'PLACE_CONTENT',
              'entityId': _mapPlaces[n],
              'anchorCount': 1,
              'placeId': _mapPlaces[n],
              'name': '地图地点$n',
              'categoryCode': 'museum',
              'cityId': null,
              'anchor': {
                'coordinateSystem': 'wgs84',
                'precision': 'point',
                'latitude': 57.2,
                'longitude': -2.1,
              },
              'visibilitySources': ['BIRDTIE_PUBLIC_MOMENT'],
              'sourceVersion': version,
              'momentCount': 1,
              'moments': [
                {
                  'id': _mapMoments[n],
                  'revision': 2,
                  'visibilitySource': 'BIRDTIE_PUBLIC_MOMENT',
                  'detailPath': '/v1/me/map-content/moments/${_mapMoments[n]}',
                },
              ],
              'momentsTruncated': false,
              'cover': null,
            },
        ],
      });
    }
    if (path == '/v1/cities/alpha/map-layers') {
      return _json({
        'schemaVersion': 'typed-map-layers-v1',
        'scope': 'PUBLIC',
        'cityId': 'alpha',
        'items': [],
        'truncated': false,
        ...lease,
      });
    }
    for (var n = 0; n < 2; n++) {
      if (path == '/v1/me/map-content/places/${_mapPlaces[n]}/moments') {
        return _json({
          'schemaVersion': 'map-content-place-moments-v1',
          'viewerAccountId': _owner,
          'placeId': _mapPlaces[n],
          'sourceVersion': version,
          'nextCursor': null,
          ...lease,
          'items': [detail(n)],
        });
      }
      if (path == '/v1/me/map-content/moments/${_mapMoments[n]}') {
        return _json(detail(n));
      }
      if (path == '/v1/moments/${_mapMoments[n]}/interactions') {
        return _json({...page(), 'momentId': _mapMoments[n]});
      }
    }
    if (path.startsWith('/v1/me/map-content/') && path.endsWith('/thread')) {
      return http.Response(
        '404 page not found\n',
        404,
        headers: {'content-type': 'text/plain; charset=utf-8'},
      );
    }
    return null;
  }

  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    if (withMap) {
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    }
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await t.pumpWidget(
      NowMomentEditorScope(
        child: MaterialApp(
          home: MapWorkspace(
            auth: auth,
            city: city,
            moments: moments,
            agentTaskSource: source,
            seedClient: client,
            seedApiBaseUrl: _origin,
          ),
        ),
      ),
    );
    await _settle(t);
  }

  Future<MomentThreadController> openPublishedEditor(WidgetTester t) async {
    final capability = NowMomentEditorScope.maybeOf(
      t.element(find.byType(AgentComposer)),
    )!;
    expect(
      await capability.open(
        NowMomentEditorRequest(
          momentID: _moment,
          ownerID: _owner,
          authorizationHeader: auth.authorizationHeader!,
          apiBaseUrl: _origin,
          sourceCurrent: () => true,
        ),
      ),
      isTrue,
    );
    await _settle(t);
    final controller = t
        .widget<MomentThreadPanel>(find.byType(MomentThreadPanel))
        .controller;
    expect(controller.entries.single.momentID, _moment);
    expect(controller.entries.single.originalStatus, 'published');
    await t.enterText(nowField(), _editing);
    await _settle(t);
    expect(controller.activeText, _editing);
    expect(await controller.flushDraft(), isTrue);
    return controller;
  }

  Future<void> readPublished(WidgetTester t) async {
    await _tap(t, find.text('查看当前内容'));
    await _settle(t);
    expect(find.byKey(const Key('now-return-moment-editor')), findsOneWidget);
    expect(find.byType(PublicMomentPage), findsOneWidget);
    expect(find.byType(MomentThreadPanel), findsNothing);
    expect(find.byType(TextField), findsOneWidget);
    expect(find.text(_publicBody), findsOneWidget);
  }

  MomentInteractionController interaction(WidgetTester t) => t
      .widget<MomentInteractionPanel>(find.byType(MomentInteractionPanel))
      .controller!;

  Future<void> close(WidgetTester t) async {
    final pending = interactionRead;
    if (pending != null && !pending.isCompleted) {
      pending.complete(_json(interactions()));
    }
    await t.pumpWidget(const SizedBox());
    await _settle(t);
    moments.dispose();
    city.dispose();
    auth.dispose();
    client.close();
  }

  void expectNoWrites() =>
      expect(calls.where((r) => r.method != 'GET'), isEmpty);
}

Future<void> _settle(WidgetTester t) async {
  // Bounded frames also work while a deliberately held GET shows a spinner.
  for (var i = 0; i < 20; i++) {
    await t.pump(const Duration(milliseconds: 20));
  }
}

Future<void> _tap(WidgetTester t, Finder target) async {
  await t.ensureVisible(target);
  await t.pump();
  expect(target.hitTestable(), findsOneWidget);
  await t.tap(target);
  await _settle(t);
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  testWidgets(
    'actual published editor reads with one composer and restores separate edit and comment drafts',
    (t) async {
      final f = _EntryFixture();
      addTearDown(() => f.close(t));
      await f.mount(t);
      final map = t.element(find.byType(MapCanvas));
      final editor = await f.openPublishedEditor(t);
      await f.readPublished(t);
      expect(f.interaction(t).momentID, _moment);
      expect(f.interaction(t).canComment, isTrue);
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).inputReadOnly,
        isFalse,
      );
      await t.enterText(nowField(), _commentDraft);
      await _settle(t);
      expect(editor.activeText, _editing);
      expect(editor.entries.single.text, _editing);
      expect(editor.current, isTrue);
      await _tap(t, find.byKey(const Key('now-return-moment-editor')));
      await _settle(t);
      expect(find.byType(PublicMomentPage), findsNothing);
      expect(
        t.widget<MomentThreadPanel>(find.byType(MomentThreadPanel)).controller,
        same(editor),
      );
      expect(t.widget<TextField>(nowField()).controller!.text, _editing);
      expect(t.element(find.byType(MapCanvas)), same(map));
      await f.readPublished(t);
      expect(t.widget<TextField>(nowField()).controller!.text, _commentDraft);
      expect(editor.activeText, _editing);
      FocusManager.instance.primaryFocus?.unfocus();
      t.testTextInput.hide();
      await _settle(t);
      await t.binding.handlePopRoute();
      await _settle(t);
      expect(find.byType(PublicMomentPage), findsNothing);
      expect(
        t.widget<MomentThreadPanel>(find.byType(MomentThreadPanel)).controller,
        same(editor),
      );
      expect(t.widget<TextField>(nowField()).controller!.text, _editing);
      expect(t.element(find.byType(MapCanvas)), same(map));
      f.expectNoWrites();
      expect(t.takeException(), isNull);
    },
  );

  testWidgets(
    'return from actual editor reader rejects a late interaction response',
    (t) async {
      final f = _EntryFixture();
      addTearDown(() => f.close(t));
      await f.mount(t);
      final editor = await f.openPublishedEditor(t);
      f.interactionRead = Completer<http.Response>();
      await f.readPublished(t);
      final retired = f.interaction(t);
      expect(retired.canComment, isFalse);
      await _tap(t, find.byKey(const Key('now-return-moment-editor')));
      f.interactionRead!.complete(_json(f.interactions()));
      await _settle(t);
      expect(retired.current(), isFalse);
      expect(find.byType(PublicMomentPage), findsNothing);
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).contentLabel,
        isNull,
      );
      expect(t.widget<TextField>(nowField()).controller!.text, _editing);
      expect(editor.activeText, _editing);
      f.expectNoWrites();
      expect(t.takeException(), isNull);
    },
  );

  for (final overlap in [false, true]) {
    testWidgets(
      'switching editor reader to ${overlap ? 'overlap group' : 'map pin'} retires A late replies and preserves its draft',
      (t) async {
        final f = _EntryFixture(withMap: true);
        addTearDown(() => f.close(t));
        await f.mount(t);
        MapCanvas canvas() => t.widget<MapCanvas>(find.byType(MapCanvas));
        await canvas().layers!.load('alpha', mapTestBounds);
        canvas().mapState.initializeViewport(mapTestBounds);
        canvas().mapContent!.sync(cityID: 'alpha', bounds: mapTestBounds);
        for (var n = 0; n < 30 && !canvas().mapContent!.current; n++) {
          await t.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)),
          );
          await t.pump();
        }
        expect(
          canvas().mapContent!.current,
          isTrue,
          reason: canvas().mapContent!.error,
        );
        final mapElement = t.element(find.byType(PublicCityMapView));
        final editor = await f.openPublishedEditor(t);
        f.interactionRead = Completer<http.Response>();
        await f.readPublished(t);
        final pageA = t.widget<PublicMomentPage>(find.byType(PublicMomentPage));
        final interactionA = f.interaction(t);
        final owned = canvas().mapContent!;
        if (overlap) {
          canvas().onContentOverlap!(owned.entities.toList());
          await _settle(t);
          expect(
            pageA.sourceCurrent!(),
            isFalse,
            reason:
                'Opening a group must retire the editor reader source immediately.',
          );
          await _tap(t, find.text('地图地点0'));
        } else {
          t
              .widget<PublicCityMapView>(find.byType(PublicCityMapView))
              .onEntitySelected!(owned.entities.first);
          await _settle(t);
          expect(
            pageA.sourceCurrent!(),
            isFalse,
            reason:
                'Opening a map reader must retire the editor reader source immediately.',
          );
          await _tap(t, find.text('查看此地点的全部1条动态'));
        }
        for (
          var n = 0;
          n < 40 &&
              t
                      .widget<AgentComposer>(find.byType(AgentComposer))
                      .contentLabel
                      ?.contains(_mapTitle) !=
                  true;
          n++
        ) {
          await t.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)),
          );
          await t.pump();
        }
        await _settle(t);
        expect(find.byType(NowMapContentPanel), findsOneWidget);
        expect(
          t.widget<AgentComposer>(find.byType(AgentComposer)).contentLabel,
          contains(_mapTitle),
        );
        await t.enterText(nowField(), 'B的独立评论草稿');
        f.interactionRead!.complete(_json(f.interactions()));
        await _settle(t);
        // Also model an already scheduled source callback after the response.
        pageA.onCommentTarget!(interactionA, _moment, '迟到的A', null);
        await _settle(t);
        expect(
          t.widget<AgentComposer>(find.byType(AgentComposer)).contentLabel,
          contains(_mapTitle),
          reason: 'Only the current B source may own Now.',
        );
        expect(t.widget<TextField>(nowField()).controller!.text, 'B的独立评论草稿');
        expect(pageA.sourceCurrent!(), isFalse);
        expect(interactionA.current(), isFalse);
        expect(find.byKey(const Key('now-return-moment-editor')), findsNothing);
        expect(editor.activeText, _editing);
        expect(editor.entries.single.text, _editing);
        expect(t.element(find.byType(PublicCityMapView)), same(mapElement));
        FocusManager.instance.primaryFocus?.unfocus();
        t.testTextInput.hide();
        await _settle(t);
        if (overlap) {
          await _tap(t, find.byTooltip('返回内容列表'));
        }
        await _tap(t, find.byTooltip('返回地图'));
        await _settle(t);
        expect(find.byType(NowMapContentPanel), findsNothing);
        expect(
          t
              .widget<MomentThreadPanel>(find.byType(MomentThreadPanel))
              .controller,
          same(editor),
        );
        expect(t.widget<TextField>(nowField()).controller!.text, _editing);
        expect(t.element(find.byType(PublicCityMapView)), same(mapElement));
        f.expectNoWrites();
        expect(t.takeException(), isNull);
      },
    );
  }

  for (final boundary in ['public lease expiry', 'owner ABA']) {
    testWidgets('actual editor reader $boundary cannot publish', (t) async {
      final f = _EntryFixture(
        readLease: boundary == 'public lease expiry'
            ? const Duration(seconds: 2)
            : const Duration(seconds: 50),
      );
      addTearDown(() => f.close(t));
      await f.mount(t);
      final editor = await f.openPublishedEditor(t);
      await f.readPublished(t);
      final original = f.interaction(t);
      expect(original.canComment, isTrue);
      await t.enterText(nowField(), _commentDraft);
      if (boundary == 'public lease expiry') {
        await t.pump(const Duration(seconds: 3));
      } else {
        f.auth.changeIdentity('Bearer synthetic-reader-B', nextOwner: _other);
        await _settle(t);
        f.auth.changeIdentity(
          'Bearer synthetic-reader-A-again',
          nextOwner: _owner,
        );
      }
      await _settle(t);
      expect(original.current(), isFalse);
      expect(original.canComment, isFalse);
      expect(
        await original.publish(
          t.element(find.byType(AgentComposer)),
          body: _commentDraft,
        ),
        isNull,
      );
      expect(find.text(_publicBody), findsNothing);
      if (boundary == 'public lease expiry') {
        expect(editor.activeText, _editing);
        expect(t.widget<TextField>(nowField()).controller!.text, _commentDraft);
        expect(
          t.widget<AgentComposer>(find.byType(AgentComposer)).inputReadOnly,
          isTrue,
        );
      }
      f.expectNoWrites();
      expect(t.takeException(), isNull);
    });
  }
}
