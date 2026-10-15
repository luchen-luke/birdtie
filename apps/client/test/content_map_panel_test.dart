import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'content_map_controller_test.dart' as fixtures;

class _LeaseFixture extends fixtures.Fixture {
  bool rejectMembers = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    if (rejectMembers && request.url.queryParameters.containsKey('clusterId')) {
      requests.add(request);
      return Future.value(
        http.StreamedResponse(
          Stream.value(<int>[]),
          503,
          headers: {
            'cache-control': 'no-store',
            'content-type': 'application/json',
          },
        ),
      );
    }
    return super.send(request);
  }
}

class _WindowFixture extends fixtures.Fixture {
  @override
  Map<String, Object?> page(Uri uri) {
    final value = super.page(uri);
    final query = uri.queryParameters;
    value['totalContentCount'] = 400;
    value['totalAnchorCount'] = 400;
    if (query['clusterId'] == null) {
      final parent = (value['pins'] as List).single as Map<String, Object?>;
      parent['momentCount'] = 400;
      parent['anchorCount'] = 400;
      parent['momentsTruncated'] = true;
    } else {
      final start = int.parse(query['cursor'] ?? '0');
      final bounds = MapBounds(
        west: double.parse(query['west']!),
        south: double.parse(query['south']!),
        east: double.parse(query['east']!),
        north: double.parse(query['north']!),
      );
      value['pins'] = List.generate(40, (i) => pin(start + i + 1, bounds));
      value['nextCursor'] = start + 40 < 400 ? '${start + 40}' : null;
    }
    return value;
  }
}

void main() {
  testWidgets(
    'same leaf restores after viewport and selected budget changes; permission retirement cannot revive it',
    (t) async {
      final f = fixtures.Fixture()..now = DateTime.now().toUtc();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: f,
        apiBaseUrl: () => fixtures.base,
        ownerID: () => f.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => f.organization,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => f.now,
      );
      c.updateQuery('', fixtures.bounds);
      await fixtures.drain(t, () => c.current);
      final pin = c.items.single, permission = c.permissionEpoch;
      bool current() =>
          c.permissionEpoch == permission &&
          c.find(pin.mapID)?.sourceVersion == pin.sourceVersion;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NowMapContentPanel(
              controller: c,
              pin: pin,
              current: current,
              changes: c,
              onClose: () {},
              onExpand: () {},
              onEdit: (_, __) {},
            ),
          ),
        ),
      );
      await fixtures.drain(t, () => find.text('合成正文').evaluate().isNotEmpty);
      c.updateQuery(
        '',
        const MapBounds(west: -2.29, south: 57, east: -1.89, north: 57.4),
        selectedMarkerID: pin.canonicalMarkerID,
      );
      await t.pump();
      await fixtures.drain(
        t,
        () => c.current && find.text('合成正文').evaluate().isNotEmpty,
      );
      expect(c.permissionEpoch, permission);
      expect(
        f.requests
            .where((r) => r.url.path.endsWith('/moments/${fixtures.id(1)}'))
            .length,
        2,
      );
      identity.value++;
      await t.pump();
      await fixtures.drain(t, () => c.current);
      expect(find.text('合成正文'), findsNothing);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      identity.dispose();
      await t.pump();
    },
  );

  testWidgets(
    'cluster continuation renews the bounded current window after thirty seconds instead of returning to page one',
    (t) async {
      final f = _WindowFixture()
        ..clusters = true
        ..now = DateTime.now().toUtc();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: f,
        apiBaseUrl: () => fixtures.base,
        ownerID: () => f.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => f.organization,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => f.now,
      );
      c.updateQuery('', fixtures.bounds);
      await fixtures.drain(t, () => c.current);
      final parent = c.items.single;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NowMapContentGroupPanel(
              controller: c,
              cluster: parent,
              current: () => c.sourceCurrent(parent),
              changes: c,
              onClose: () {},
              onEdit: (_, __) {},
            ),
          ),
        ),
      );
      await fixtures.drain(t, () => find.text('合成动态 1').evaluate().isNotEmpty);
      for (var page = 1; page < 9; page++) {
        await t.scrollUntilVisible(
          find.text('加载更多位置'),
          500,
          scrollable: find.byType(Scrollable).first,
        );
        await t.tap(find.text('加载更多位置'));
        await t.pump();
        await fixtures.drain(
          t,
          () =>
              f.requests
                      .where(
                        (r) => r.url.queryParameters.containsKey('clusterId'),
                      )
                      .length >=
                  page + 1 &&
              find.byType(LinearProgressIndicator).evaluate().isEmpty,
        );
      }
      final before = f.requests.length;
      f.now = f.now.add(const Duration(seconds: 15));
      await t.pump(const Duration(seconds: 15));
      await fixtures.drain(t, () => c.current && !c.loading);
      f.now = f.now.add(const Duration(seconds: 16));
      await t.pump(const Duration(seconds: 16));
      await fixtures.drain(
        t,
        () =>
            f.requests
                    .skip(before)
                    .where(
                      (r) => r.url.queryParameters.containsKey('clusterId'),
                    )
                    .length >=
                8 &&
            find.byType(LinearProgressIndicator).evaluate().isEmpty,
      );
      final continued = f.requests
          .skip(before)
          .where((r) => r.url.queryParameters.containsKey('clusterId'))
          .toList();
      expect(continued.first.url.queryParameters['cursor'], '40');
      expect(continued.last.url.queryParameters['cursor'], '320');
      expect(
        continued.every((r) => r.url.queryParameters['cursor'] != null),
        true,
      );
      await t.scrollUntilVisible(
        find.text('加载更多位置'),
        500,
        scrollable: find.byType(Scrollable).first,
      );
      await t.tap(find.text('加载更多位置'));
      await fixtures.drain(
        t,
        () =>
            f.requests.last.url.queryParameters['cursor'] == '360' &&
            find.byType(LinearProgressIndicator).evaluate().isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      c.dispose();
      identity.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'actual cluster panel pages, opens exact anchor detail, returns to loaded group and retires permission',
    (t) async {
      final f = fixtures.Fixture()
        ..clusters = true
        ..now = DateTime.now().toUtc();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: f,
        apiBaseUrl: () => fixtures.base,
        ownerID: () => f.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => f.organization,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => f.now,
      );
      c.updateQuery('', fixtures.bounds);
      await fixtures.drain(t, () => c.current);
      final parent = c.items.single;
      var closed = false;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NowMapContentGroupPanel(
              controller: c,
              cluster: parent,
              current: () => c.sourceCurrent(parent),
              changes: c,
              onClose: () => closed = true,
              onEdit: (_, __) {},
            ),
          ),
        ),
      );
      await fixtures.drain(t, () => find.text('合成动态 1').evaluate().isNotEmpty);
      await t.tap(find.text('加载更多位置'));
      await fixtures.drain(t, () => find.text('合成动态 2').evaluate().isNotEmpty);
      final groupReads = f.requests
          .where((r) => r.url.queryParameters.containsKey('clusterId'))
          .length;
      expect(groupReads, 2);
      await t.tap(find.text('合成动态 2'));
      await fixtures.drain(t, () => find.text('合成正文').evaluate().isNotEmpty);
      expect(
        f.requests.last.url.path,
        '/v1/me/map-content/moments/${fixtures.id(2)}',
      );
      expect(f.requests.any((r) => r.url.path.contains('/places/')), false);
      expect(find.byType(NowMapContentPanel), findsOneWidget);
      await t.tap(find.byTooltip('关闭内容预览'));
      await t.pump();
      expect(find.text('合成动态 1'), findsOneWidget);
      expect(find.text('合成动态 2'), findsOneWidget);
      expect(
        f.requests
            .where((r) => r.url.queryParameters.containsKey('clusterId'))
            .length,
        groupReads,
      );
      expect(closed, false);
      f.organization = fixtures.id(9999);
      identity.value++;
      await t.pump();
      expect(find.text('合成动态 1'), findsNothing);
      expect(find.text('合成动态 2'), findsNothing);
      expect(find.text('当前内容已不可读，请返回地图重新选择。'), findsOneWidget);
      await t.tap(find.byTooltip('返回地图'));
      expect(closed, true);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      identity.dispose();
      await t.pump();
    },
  );
  testWidgets(
    'parent renewal cannot extend child grant; expiry clears names during failure and retry restores loaded exact selection',
    (t) async {
      final f = _LeaseFixture()
        ..clusters = true
        ..now = DateTime.now().toUtc();
      final identity = ValueNotifier(0);
      final c = OwnedMapController(
        client: f,
        apiBaseUrl: () => fixtures.base,
        ownerID: () => f.owner,
        authorizationHeader: () => 'Bearer fixture',
        organizationWorkspaceID: () => f.organization,
        identityChanges: identity,
        refreshAllowed: () => true,
        now: () => f.now,
      );
      c.updateQuery('', fixtures.bounds);
      await fixtures.drain(t, () => c.current);
      final parent = c.items.single;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NowMapContentGroupPanel(
              controller: c,
              cluster: parent,
              current: () => c.sourceCurrent(parent),
              changes: c,
              onClose: () {},
              onEdit: (_, __) {},
            ),
          ),
        ),
      );
      await fixtures.drain(t, () => find.text('合成动态 1').evaluate().isNotEmpty);
      await t.tap(find.text('加载更多位置'));
      await fixtures.drain(t, () => find.text('合成动态 2').evaluate().isNotEmpty);
      await t.tap(find.text('合成动态 2'));
      await fixtures.drain(t, () => find.text('合成正文').evaluate().isNotEmpty);
      f.now = f.now.add(const Duration(seconds: 15));
      await t.pump(const Duration(seconds: 15));
      await fixtures.drain(t, () => !c.loading && c.current);
      expect(c.sourceCurrent(parent), true);
      f.rejectMembers = true;
      f.now = f.now.add(const Duration(seconds: 16));
      await t.pump(const Duration(seconds: 16));
      await fixtures.drain(
        t,
        () => find.text('内容列表暂不可读，请重试。').evaluate().isNotEmpty && !c.loading,
      );
      expect(
        c.current,
        true,
        reason: 'The parent has renewed while child grant expired.',
      );
      expect(find.text('合成动态 1'), findsNothing);
      expect(find.text('合成动态 2'), findsNothing);
      expect(find.text('合成正文'), findsNothing);
      f.rejectMembers = false;
      await t.tap(find.text('重试'));
      await fixtures.drain(t, () => find.text('合成正文').evaluate().isNotEmpty);
      expect(
        f.requests.last.url.path,
        '/v1/me/map-content/moments/${fixtures.id(2)}',
      );
      await t.tap(find.byTooltip('关闭内容预览'));
      await t.pump();
      expect(find.text('合成动态 1'), findsOneWidget);
      expect(find.text('合成动态 2'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      identity.dispose();
      await t.pump();
    },
  );
}
