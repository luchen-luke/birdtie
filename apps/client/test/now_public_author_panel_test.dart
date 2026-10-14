import 'dart:async';

import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:birdtie_client/src/workspace/public_author_moments.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'map_layers_api_test.dart' show mapTestBounds, mapTestItem;
import 'place_detail_sheet_test.dart'
    show workspacePlaceID, workspaceMomentID, workspacePlaceData, jsonReply;
import 'place_history_controller_test.dart' show historyTestData;
import 'public_moment_author_v2_test.dart'
    show publicMomentV2Data, publicAuthorTestID;

String _moment(int n) =>
    'f0000000-0000-4000-8000-${n.toRadixString(16).padLeft(12, '0')}';

class _AuthorFixture {
  final auth = SeedTestAuth()..token = null;
  final source = ValueNotifier(true), show = ValueNotifier(true);
  final selection = ValueNotifier<String?>(null);
  final requests = <http.Request>[];
  final mapModes = <bool>[], selected = <TypedMapItem>[];
  int count = 22, authorReads = 0, leaves = 0, closes = 0, momentReads = 0;
  String? error;
  bool changedVersion = false, truncated = false;
  bool reuseEntryID = false;
  int leaseMs = 120000;
  Completer<http.Response>? pendingAuthor;
  late final client = MockClient(reply);
  late final layers = MapLayersController(
    api: MapLayersApi(client: client, apiBaseUrl: 'http://unit.test'),
    authorizationHeader: () => auth.authorizationHeader,
    organizationWorkspaceID: () => null,
  );
  late final changes = Listenable.merge([auth, source]);
  late final panel = NowPublicPlacePanel(
    item: TypedMapItem(
      kind: 'MOMENT',
      id: workspaceMomentID,
      title: '入口动态',
      detailType: 'MOMENT',
      detailID: workspaceMomentID,
      placeID: workspacePlaceID,
      anchorLabel: '入口地点',
      latitude: 57.1,
      longitude: -2.1,
      sourceVersion: 'entry-v1',
    ),
    auth: auth,
    identityChanges: changes,
    workspaceID: () => null,
    current: () => source.value && auth.authorizationHeader == null,
    revalidate: () async => source.value && auth.authorizationHeader == null,
    onClose: () {
      closes++;
      show.value = false;
    },
    onOpenActivity: (_) {},
    client: client,
    apiBaseUrl: 'http://unit.test',
    authorLayers: layers,
    onOpenAuthorScope: (id) async =>
        source.value &&
        await layers.loadAuthor('owned-city', mapTestBounds, id),
    onLeaveAuthorScope: () {
      leaves++;
      layers.leaveAuthor();
    },
    onAuthorMapVisible: mapModes.add,
    onSelectAuthorMoment: (item) {
      selected.add(item);
      selection.value = item.mapID;
    },
    authorSelectionChanges: selection,
    selectedAuthorMomentID: () => selection.value,
  );

  Map<String, dynamic> wire({String? author}) {
    final now = DateTime.now().toUtc();
    return {
      'schemaVersion': author == null
          ? 'typed-map-layers-v1'
          : 'typed-map-layers-v2',
      'cityId': 'owned-city',
      'scope': author == null ? 'PUBLIC' : 'PUBLIC_AUTHOR',
      if (author != null) 'authorId': author,
      'observedAt': now.toIso8601String(),
      'validUntil': now.add(Duration(milliseconds: leaseMs)).toIso8601String(),
      'truncated': author != null && truncated,
      'items': author == null
          ? [mapTestItem('PLACE'), mapTestItem('MOMENT')]
          : [
              for (var n = 1; n <= count; n++)
                {
                  ...mapTestItem('MOMENT'),
                  'id': reuseEntryID && n == 1 ? workspaceMomentID : _moment(n),
                  'title': '作者记录$n',
                  'entityRef': {
                    'type': 'MOMENT',
                    'id': reuseEntryID && n == 1
                        ? workspaceMomentID
                        : _moment(n),
                  },
                  'detailRef': {
                    'type': 'MOMENT',
                    'id': reuseEntryID && n == 1
                        ? workspaceMomentID
                        : _moment(n),
                  },
                  'sourceVersion': changedVersion ? 'source-v2' : 'source-v1',
                },
            ],
    };
  }

  Future<http.Response> reply(http.Request r) async {
    requests.add(r);
    if (r.method != 'GET') throw StateError('No writes authorized');
    if (r.url.path.endsWith('/map-layers')) {
      final id = r.url.queryParameters['authorId'];
      if (id != null) {
        authorReads++;
        if (pendingAuthor case final p?) {
          pendingAuthor = null;
          return p.future;
        }
        if (error == '404') return http.Response('{}', 404);
        if (error == 'schema') {
          final bad = wire(author: id)..['scope'] = 'PUBLIC';
          return jsonReply({'data': bad});
        }
      }
      return jsonReply({'data': wire(author: id)});
    }
    if (r.url.path == '/v1/places/$workspacePlaceID')
      return jsonReply({'data': workspacePlaceData()});
    if (r.url.path == '/v1/places/$workspacePlaceID/social-history')
      return jsonReply({'data': historyTestData()});
    if (r.url.path == '/v1/places/$workspacePlaceID/activities' ||
        r.url.path == '/v1/me/moments')
      return jsonReply({'data': []});
    if (r.url.path.startsWith('/v1/moments/')) {
      momentReads++;
      final id = r.url.path.split('/').last;
      return jsonReply({
        'data': publicMomentV2Data()
          ..['id'] = id
          ..['title'] = '读取动态$id'
          ..['body'] = List.filled(30, '合成正文，用于恢复阅读位置。').join('\n'),
      });
    }
    if (r.url.path == '/v1/accounts/$publicAuthorTestID/profile')
      return jsonReply({
        'data': {
          'accountId': publicAuthorTestID,
          'displayName': '当前公开作者',
          'visibility': 'public',
        },
      });
    return http.Response('{}', 404);
  }

  Future<void> mount(WidgetTester t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    await layers.load('owned-city', mapTestBounds);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Stack(
            children: [
              const Positioned.fill(
                child: SizedBox(key: Key('one-original-map')),
              ),
              ValueListenableBuilder<bool>(
                valueListenable: show,
                builder: (_, shown, _) => shown ? panel : const SizedBox(),
              ),
            ],
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  Future<void> author(WidgetTester t) async {
    final open = find.text('查看公开动态');
    await t.ensureVisible(open);
    await t.tap(open);
    await t.pumpAndSettle();
    await t.tap(find.text('公开作者名字'));
    await t.pumpAndSettle();
    expect(find.byType(PublicPersonPage), findsOneWidget);
  }

  Future<void> authorList(WidgetTester t) async {
    await author(t);
    await t.tap(find.text('当前范围的公开动态'));
    await t.pumpAndSettle();
  }

  void readOnly() {
    expect(requests.every((r) => r.method == 'GET'), isTrue);
    expect(requests.any((r) => r.url.path.contains('agent-tasks')), isFalse);
  }

  bool _disposed = false;
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    layers.dispose();
    client.close();
    auth.dispose();
    source.dispose();
    show.dispose();
    selection.dispose();
  }
}

Finder _authorScroll({bool offstage = false}) => find
    .descendant(
      of: find.byType(PublicAuthorMoments, skipOffstage: !offstage),
      matching: find.byType(Scrollable, skipOffstage: !offstage),
    )
    .first;

void main() {
  for (final mode in ['target', 'scope', 'lease', 'kind']) {
    testWidgets('独立作者body $mode 误绑定即使外部current为true也拒绝旧集合', (t) async {
      final f = _AuthorFixture();
      addTearDown(f.dispose);
      try {
        await f.mount(t);
        await f.layers.loadAuthor(
          'owned-city',
          mapTestBounds,
          publicAuthorTestID,
        );
        final old = f.layers.authorView!;
        f.layers.authorView = TypedMapView(
          cityID: old.cityID,
          scope: mode == 'scope' ? 'PUBLIC' : old.scope,
          authorID: old.authorID,
          observedAt: old.observedAt,
          validUntil: mode == 'lease'
              ? DateTime.now().toUtc().subtract(const Duration(seconds: 1))
              : old.validUntil,
          truncated: false,
          items: mode == 'kind' ? f.layers.baseItems : old.items,
        );
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: PublicAuthorMoments(
                layers: f.layers,
                authorID: mode == 'target'
                    ? '44444444-4444-4444-8444-444444444444'
                    : publicAuthorTestID,
                authorScopeCurrent: () => true,
                onOpenMoment: f.selected.add,
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(find.text('作者记录1'), findsNothing);
        expect(find.byType(ListTile), findsNothing);
        expect(f.selected, isEmpty);
        f.readOnly();
      } finally {
        await t.pumpWidget(const SizedBox());
        f.dispose();
      }
    });
  }
  for (final mode in ['expiry', 'version', '404']) {
    testWidgets('相同Moment ID的作者来源再次进入后 $mode 仍遮掉旧正文', (t) async {
      final f = _AuthorFixture()..reuseEntryID = true;
      addTearDown(f.dispose);
      try {
        await f.mount(t);
        await f.authorList(t);
        await t.tap(
          find.byKey(const ValueKey('author-moment:$workspaceMomentID')),
        );
        await t.pumpAndSettle();
        expect(find.text('读取动态$workspaceMomentID'), findsOneWidget);
        if (mode == 'expiry') {
          f.layers.authorView = null;
          f.layers.authorError = '作者范围的来源已到期，请重新读取';
          f.layers.notifyListeners();
        } else {
          if (mode == 'version')
            f.changedVersion = true;
          else
            f.error = '404';
          await f.layers.loadAuthor(
            'owned-city',
            mapTestBounds,
            publicAuthorTestID,
          );
        }
        await t.pumpAndSettle();
        expect(find.text('读取动态$workspaceMomentID'), findsNothing);
        expect(find.text('该动态或作者范围已变化，请返回当前列表重新核实。'), findsOneWidget);
        f.readOnly();
      } finally {
        await t.pumpWidget(const SizedBox());
        f.dispose();
      }
    });
  }
  for (final mode in ['epoch', 'target', 'actor']) {
    testWidgets('作者范围绑定 $mode ABA 不复活旧列表或保存的tile回调', (t) async {
      final f = _AuthorFixture();
      addTearDown(f.dispose);
      await f.mount(t);
      await f.authorList(t);
      final saved = t
          .widget<ListTile>(find.byKey(ValueKey('author-moment:${_moment(1)}')))
          .onTap!;
      if (mode == 'epoch') {
        f.layers.retire();
        await f.layers.load('owned-city', mapTestBounds);
        await f.layers.loadAuthor(
          'owned-city',
          mapTestBounds,
          publicAuthorTestID,
        );
      } else if (mode == 'target') {
        await f.layers.loadAuthor(
          'owned-city',
          mapTestBounds,
          '44444444-4444-4444-8444-444444444444',
        );
        await f.layers.loadAuthor(
          'owned-city',
          mapTestBounds,
          publicAuthorTestID,
        );
      } else {
        f.auth.changeIdentity('Bearer unit-b');
        f.auth.changeIdentity(null);
      }
      await t.pumpAndSettle();
      saved();
      await t.pumpAndSettle();
      expect(find.text('作者记录1'), findsNothing);
      expect(f.selected, isEmpty);
      expect(find.text('读取动态${_moment(1)}'), findsNothing);
      f.readOnly();
      await t.pumpWidget(const SizedBox());
      f.dispose();
    });
  }
  testWidgets('刷新后的旧tile与未知地图选择均不触发新详情，关闭仍恢复基础范围', (t) async {
    final f = _AuthorFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.authorList(t);
    final saved = t
        .widget<ListTile>(find.byKey(ValueKey('author-moment:${_moment(1)}')))
        .onTap!;
    await f.layers.loadAuthor('owned-city', mapTestBounds, publicAuthorTestID);
    await t.pumpAndSettle();
    final reads = f.authorReads;
    saved();
    await t.pumpAndSettle();
    expect(f.authorReads, reads);
    expect(f.selected, isEmpty);
    await t.tap(find.text('查看作者地图'));
    await t.pumpAndSettle();
    f.selection.value = 'moment:99999999-9999-4999-8999-999999999999';
    await t.pumpAndSettle();
    expect(f.selected, isEmpty);
    expect(find.byType(PublicMomentPage), findsNothing);
    await t.tap(find.byTooltip('关闭内容返回地图'));
    await t.pumpAndSettle();
    expect(f.layers.authorID, isNull);
    f.readOnly();
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('作者地图到期恢复可见失败说明，保留单面板不补城市内容', (t) async {
    final f = _AuthorFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.authorList(t);
    await t.tap(find.text('查看作者地图'));
    await t.pumpAndSettle();
    f.layers.authorView = null;
    f.layers.authorError = '作者范围的来源已到期，请重新读取';
    f.layers.notifyListeners();
    await t.pumpAndSettle();
    expect(f.mapModes.last, false);
    expect(find.text('作者范围的来源已到期，请重新读取'), findsOneWidget);
    expect(find.text('作者记录1'), findsNothing);
    expect(f.layers.items, isEmpty);
    expect(f.layers.baseItems, isNotEmpty);
    f.readOnly();
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('TA列表包含原进入动态时仍为独立读帧，不重复key或折返旧缓存', (t) async {
    final f = _AuthorFixture()..reuseEntryID = true;
    addTearDown(f.dispose);
    await f.mount(t);
    await f.authorList(t);
    final reads = f.momentReads;
    await t.tap(find.byKey(const ValueKey('author-moment:$workspaceMomentID')));
    await t.pumpAndSettle();
    expect(t.takeException(), isNull);
    expect(f.momentReads, reads + 1);
    expect(f.layers.authorID, publicAuthorTestID);
    expect(find.text('读取动态$workspaceMomentID'), findsOneWidget);
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(find.byType(PublicAuthorMoments), findsOneWidget);
    f.readOnly();
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('同一面板TA当前原typed集合/实体/地图ID一致，深入返回保留列表滚动', (t) async {
    final f = _AuthorFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    final panelState = t.state(find.byType(NowPublicPlacePanel));
    await f.authorList(t);
    expect(find.byType(Scaffold), findsOneWidget);
    expect(t.state(find.byType(NowPublicPlacePanel)), same(panelState));
    expect(f.layers.items, same(f.layers.authorView!.items));
    expect(f.layers.items.length, 22);
    final first = t
        .widget<ListTile>(find.byKey(ValueKey('author-moment:${_moment(1)}')))
        .onTap!;
    await t.drag(_authorScroll(), const Offset(0, -430));
    await t.pumpAndSettle();
    final state = t.state<ScrollableState>(_authorScroll());
    final offset = state.position.pixels;
    expect(offset, greaterThan(0));
    first();
    await t.pumpAndSettle();
    expect(f.authorReads, 2);
    expect(f.selected.single.id, _moment(1));
    expect(find.byType(PublicMomentPage), findsOneWidget);
    expect(find.text('读取动态${_moment(1)}'), findsOneWidget);
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(f.authorReads, 3);
    expect(find.byType(PublicAuthorMoments), findsOneWidget);
    expect(
      t.state<ScrollableState>(_authorScroll()).position.pixels,
      closeTo(offset, .1),
    );
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(f.layers.authorID, isNull);
    expect(f.leaves, 1);
    expect(f.mapModes.last, false);
    expect(find.byType(PublicPersonPage), findsOneWidget);
    expect(f.layers.items.length, 2);
    f.readOnly();
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('真空集合不填城市结果，截断说明仅已加载范围', (t) async {
    final f = _AuthorFixture()
      ..count = 0
      ..truncated = true;
    addTearDown(f.dispose);
    await f.mount(t);
    await f.authorList(t);
    expect(f.layers.baseItems, isNotEmpty);
    expect(f.layers.items, isEmpty);
    expect(find.text('当前范围暂无该作者可见的公开动态。'), findsOneWidget);
    expect(find.text('仅展示当前范围已加载的公开动态；可返回地图缩小范围。'), findsOneWidget);
    f.readOnly();
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  for (final mode in ['404', 'schema']) {
    testWidgets('$mode 不接旧城市列表，保留作者页真实失败说明', (t) async {
      final f = _AuthorFixture()..error = mode;
      addTearDown(f.dispose);
      await f.mount(t);
      await f.authorList(t);
      expect(find.byType(PublicAuthorMoments), findsNothing);
      expect(find.textContaining('作者当前范围的公开动态暂不可用'), findsOneWidget);
      expect(f.layers.authorID, isNull);
      expect(f.leaves, 1);
      expect(find.byType(PublicPersonPage), findsOneWidget);
      expect(
        find.text('当前范围的公开动态'),
        findsNothing,
        reason:
            'failed source must be reopened through a fresh parent/profile read',
      );
      f.readOnly();
      await t.pumpWidget(const SizedBox());
      f.dispose();
    });
  }
  for (final mode in ['missing', 'version']) {
    testWidgets('fresh $mode 拒绝旧候选详情，不标成功', (t) async {
      final f = _AuthorFixture();
      addTearDown(f.dispose);
      await f.mount(t);
      await f.authorList(t);
      if (mode == 'missing')
        f.count = 0;
      else
        f.changedVersion = true;
      await t.tap(find.byKey(ValueKey('author-moment:${_moment(1)}')));
      await t.pumpAndSettle();
      expect(f.selected, isEmpty);
      expect(find.byType(PublicAuthorMoments), findsOneWidget);
      expect(find.byType(PublicMomentPage), findsNothing);
      expect(find.text('这条动态已变化或不可访问，请从当前列表重新选择。'), findsOneWidget);
      f.readOnly();
      await t.pumpWidget(const SizedBox());
      f.dispose();
    });
  }
  testWidgets('作者地图切换保持列表Element/滚动，原workspace选择双向深入且不重复', (t) async {
    final f = _AuthorFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.authorList(t);
    await t.drag(_authorScroll(), const Offset(0, -430));
    await t.pumpAndSettle();
    final state = t.state<ScrollableState>(_authorScroll());
    final offset = state.position.pixels;
    await t.tap(find.text('查看作者地图'));
    await t.pumpAndSettle();
    expect(f.mapModes.last, true);
    expect(find.byType(PublicAuthorMoments), findsNothing);
    expect(
      t.state<ScrollableState>(_authorScroll(offstage: true)),
      same(state),
    );
    expect(state.position.pixels, closeTo(offset, .1));
    await t.tap(find.text('展开作者动态'));
    await t.pumpAndSettle();
    expect(f.mapModes.last, false);
    expect(t.state<ScrollableState>(_authorScroll()), same(state));
    await t.tap(find.text('查看作者地图'));
    await t.pumpAndSettle();
    f.selection.value = 'moment:${_moment(2)}';
    await t.pumpAndSettle();
    expect(find.text('读取动态${_moment(2)}'), findsOneWidget);
    expect(f.selected.length, 1);
    expect(f.selected.single.mapID, f.selection.value);
    expect(f.mapModes.last, false);
    f.readOnly();
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('scope到期退役TA正文和深层动态，不能回退城市或ABA回生', (t) async {
    final f = _AuthorFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.authorList(t);
    await t.tap(find.byKey(ValueKey('author-moment:${_moment(1)}')));
    await t.pumpAndSettle();
    f.layers.authorView = null;
    f.layers.authorError = '作者范围的来源已到期，请重新读取';
    f.layers.notifyListeners();
    await t.pumpAndSettle();
    expect(find.text('读取动态${_moment(1)}'), findsNothing);
    expect(find.textContaining('该动态或作者范围已变化'), findsOneWidget);
    await f.layers.loadAuthor('owned-city', mapTestBounds, publicAuthorTestID);
    await t.pumpAndSettle();
    expect(find.text('读取动态${_moment(1)}'), findsNothing);
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(find.textContaining('作者范围的来源已变化'), findsOneWidget);
    await t.tap(find.byTooltip('返回上一层'));
    await t.pumpAndSettle();
    expect(f.layers.authorID, isNull);
    f.readOnly();
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('author读取迟到source ABA不创建frame或恢复授权', (t) async {
    final f = _AuthorFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.author(t);
    final pending = Completer<http.Response>();
    f.pendingAuthor = pending;
    await t.tap(find.text('当前范围的公开动态'));
    await t.pump();
    await t.pump();
    f.source.value = false;
    f.source.value = true;
    await t.pump();
    pending.complete(jsonReply({'data': f.wire(author: publicAuthorTestID)}));
    await t.pumpAndSettle();
    expect(find.byType(PublicAuthorMoments), findsNothing);
    expect(find.textContaining('身份、地点或公开来源已变化'), findsOneWidget);
    f.readOnly();
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
  testWidgets('关闭作者范围先leave及mapfalse，0新Task写入', (t) async {
    final f = _AuthorFixture();
    addTearDown(f.dispose);
    await f.mount(t);
    await f.authorList(t);
    await t.tap(find.text('查看作者地图'));
    await t.pumpAndSettle();
    await t.tap(find.byTooltip('关闭内容返回地图'));
    await t.pumpAndSettle();
    expect(f.closes, 1);
    expect(f.leaves, 1);
    expect(f.layers.authorID, isNull);
    expect(f.mapModes.last, false);
    expect(find.byKey(const Key('one-original-map')), findsOneWidget);
    f.readOnly();
    await t.pumpWidget(const SizedBox());
    f.dispose();
  });
}
