import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/community_avatar.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/now_social_edge.dart';
import 'package:birdtie_client/src/workspace/now_social_edge_source.dart';
import 'package:birdtie_client/src/workspace/person_avatar.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'person_avatar_fixture.dart' show avatarJPEG, avatarMetadata;

const _owner = 'fa000000-0000-4000-8000-000000000001';
const _community = 'fa000000-0000-4000-8000-000000000002';
const _organization = 'fa000000-0000-4000-8000-000000000003';
const _business = 'fa000000-0000-4000-8000-000000000004';
const _person = 'fa000000-0000-4000-8000-000000000005';
const _follow = 'fa000000-0000-4000-8000-000000000006';
const _followOrg = 'fa000000-0000-4000-8000-000000000007';
const _followBusiness = 'fa000000-0000-4000-8000-000000000008';
const _followPerson = 'fa000000-0000-4000-8000-000000000009';

Map<String, Object?> _media({int version = 1}) => {
  'communityId': _community,
  'version': version,
  'thumbnailPath': '/v1/communities/$_community/avatar/$version/content',
  'width': 128,
  'height': 128,
  'byteCount': avatarJPEG().length,
  'sha256': sha256.convert(avatarJPEG()).toString(),
};
Map<String, Object?> _row({
  String type = 'COMMUNITY',
  String id = _community,
  String followID = _follow,
  String label = '隔离合成社群',
  Object? media,
  Object? personAvatar,
}) => {
  'id': followID,
  'target': {'targetType': type, 'targetId': id},
  'label': label,
  'createdAt': '2026-10-14T00:00:00Z',
  if (media != null) 'communityAvatar': media,
  if (personAvatar != null) 'avatar': personAvatar,
};
http.Response _wire(Object? value, {int status = 200}) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': value})),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
http.Response _page(List<Map<String, Object?>> rows, {String? next}) =>
    _wire({'items': rows, 'nextCursor': next});

class _TrackedClient extends MockClient {
  _TrackedClient(super.fn);
  bool closed = false;
  @override
  void close() {
    closed = true;
    super.close();
  }
}

class _FollowModel extends ChangeNotifier {
  String? token = 'Bearer follow-A', owner = _owner, workspace;
  int epoch = 0;
  bool open = true;
  late NowSocialEdgeSource source;
  void changed() {
    epoch++;
    notifyListeners();
  }
}

class _Fixture {
  final model = _FollowModel();
  late _TrackedClient client;
  late ConnectionSource connections;
  final requests = <http.Request>[];
  final targets = <NowFollowEntry>[];
  final persons = <String>[];
  List<Map<String, Object?>> rows = [_row()];
  Completer<http.Response>? contentGate, pageGate, moreGate;
  int contentStatus = 200;
  bool badPixels = false;
  String? next;
  _Fixture() {
    client = _TrackedClient((r) async {
      requests.add(r);
      // This callback also runs while a tester pump is in flight.
      expectSync(r.method, 'GET');
      expectSync(r.bodyBytes, isEmpty);
      expectSync(r.url.host, 'follow.fixture');
      if (r.url.path.contains('/avatar/')) {
        if (contentGate != null) return contentGate!.future;
        return http.Response.bytes(
          badPixels ? [1, 2, 3] : avatarJPEG(),
          contentStatus,
        );
      }
      if (r.url.path == '/v1/me/follows/page') {
        if (r.url.queryParameters['cursor'] != null && moreGate != null) {
          return moreGate!.future;
        }
        if (pageGate != null) return pageGate!.future;
        return _page(rows, next: next);
      }
      if (r.url.path == '/v1/me/ties/page') return _page([]);
      if (r.url.path == '/v1/me/conversations/page') {
        return _wire({'items': [], 'nextCursor': null, 'unreadCount': 0});
      }
      if (r.url.path == '/borrowed-check') return _wire(null);
      throw StateError('Unexpected controlled request ${r.url}');
    });
    connections = ConnectionSource(
      authorizationHeader: () => model.token,
      client: client,
      apiBaseUrl: 'https://follow.fixture',
      identityChanges: model,
    );
    model.source = NowSocialEdgeSource(
      connections,
      identityEpoch: () => model.epoch,
    );
  }
  Widget host({bool callback = true}) => MaterialApp(
    home: Scaffold(
      body: AnimatedBuilder(
        animation: model,
        builder: (_, __) => Stack(
          children: [
            Positioned(
              top: 0,
              bottom: 0,
              right: 0,
              child: NowSocialEdge(
                key: const Key('follow-test-edge'),
                open: model.open,
                source: model.source,
                identityChanges: model,
                current: () => true,
                accountID: () => model.owner,
                workspaceID: () => model.workspace,
                initialSection: NowSocialSection.contacts,
                initialFollowedContacts: true,
                onOpenAuthor: persons.add,
                onOpenFollowTarget: callback ? targets.add : null,
                onOpenConversation: (_) {},
                onOpenManagement: () {},
                onSignIn: () {},
                onUsePersonal: () {},
              ),
            ),
          ],
        ),
      ),
    ),
  );
  Future<void> mount(WidgetTester t, {bool callback = true}) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    addTearDown(() {
      connections.dispose();
      model.dispose();
      client.close();
    });
    await t.pumpWidget(host(callback: callback));
    await _settle(t);
  }

  Finder tile(String type, String id) =>
      find.byKey(ValueKey('edge-follow-$type-$id'));
  VoidCallback tapCallback(WidgetTester t) =>
      t.widget<ListTile>(tile('COMMUNITY', _community)).onTap!;
}

Future<void> _settle(WidgetTester t) async {
  await t.pump();
  await t.runAsync(
    () => Future<void>.delayed(const Duration(milliseconds: 40)),
  );
  await t.pumpAndSettle();
}

Future<void> _decodeVisibleAvatar(WidgetTester t) async {
  await _settle(t);
  expect(find.byType(CommunityAvatar), findsOneWidget);
  // After a list refresh this frame has only mounted the controlled avatar.
  // Drain its subsequent content stream before asking the mounted Image to
  // decode. The initial mount already had this stage; replacements do not.
  await t.runAsync(
    () => Future<void>.delayed(const Duration(milliseconds: 40)),
  );
  await t.pump();
  // The Image is mounted by the final fake-time frame above. Its engine codec
  // needs a real event-loop turn after mounting; pumpAndSettle alone cannot
  // finish that work. Decode the actual mounted provider, without fake pixels.
  final image = t.widget<Image>(find.byType(Image));
  final context = t.element(find.byType(Image));
  await t.runAsync(
    () =>
        precacheImage(image.image, context).timeout(const Duration(seconds: 2)),
  );
  await t.pumpAndSettle();
}

void main() {
  test(
    'followTarget metadata optional compatibility and original community pairing',
    () async {
      final client = _TrackedClient((r) async {
        final rows = [
          _row(
            media: _media(),
            personAvatar: avatarMetadata(owner: _person),
          ),
          _row(
            type: 'PERSON',
            id: _person,
            followID: _followPerson,
            label: '隔离人物',
            personAvatar: avatarMetadata(owner: _person),
          ),
        ];
        return r.url.path.endsWith('/page') ? _page(rows) : _wire(rows);
      });
      final connection = ConnectionSource(
        authorizationHeader: () => 'Bearer A',
        client: client,
        apiBaseUrl: 'https://follow.fixture',
      );
      final source = NowSocialEdgeSource(connection);
      for (final rows in [
        (await source.followsPage()).items,
        await source.follows(),
      ]) {
        expect(rows.first.communityAvatar!.communityID, _community);
        expect(rows.first.avatar, isNull);
        expect(rows.last.communityAvatar, isNull);
        expect(rows.last.avatar!.accountID, _person);
        expect(rows.first.targetID, _community);
      }
      expect(client.closed, isFalse);
      connection.dispose();
      expect(client.closed, isFalse);
      client.close();
    },
  );
  test(
    'followTarget strict media rejects type target version path digest and bounds on both ports',
    () async {
      for (final malformed in [
        {..._media(), 'communityId': _person},
        {
          ..._media(),
          'thumbnailPath': '/v1/accounts/$_community/avatar/1/content',
        },
        {..._media(), 'thumbnailPath': 'https://private.invalid/photo.jpg'},
        {..._media(), 'version': 0},
        {..._media(), 'version': 9007199254740992},
        {..._media(), 'byteCount': 0},
        {..._media(), 'byteCount': 9999999},
        {..._media(), 'width': 256},
        {..._media(), 'height': 64},
        {..._media(), 'sha256': 'invalid'},
        {..._media(), 'extra': 'unapproved'},
      ]) {
        final client = MockClient(
          (r) async => r.url.path.endsWith('/page')
              ? _page([_row(media: malformed)])
              : _wire([_row(media: malformed)]),
        );
        final connection = ConnectionSource(
          authorizationHeader: () => 'Bearer A',
          client: client,
          apiBaseUrl: 'https://follow.fixture',
        );
        final source = NowSocialEdgeSource(connection);
        await expectLater(source.followsPage(), throwsFormatException);
        await expectLater(source.follows(), throwsFormatException);
        connection.dispose();
        client.close();
      }
      for (final type in ['PERSON', 'ORGANIZATION', 'BUSINESS']) {
        final client = MockClient(
          (r) async => _page([_row(type: type, media: _media())]),
        );
        final connection = ConnectionSource(
          authorizationHeader: () => 'Bearer A',
          client: client,
          apiBaseUrl: 'https://follow.fixture',
        );
        await expectLater(
          NowSocialEdgeSource(connection).followsPage(),
          throwsFormatException,
        );
        connection.dispose();
        client.close();
      }
    },
  );
  test(
    'followTarget read actor ABA rejects late same-token authorized page',
    () async {
      final gate = Completer<http.Response>();
      var epoch = 0;
      final client = MockClient((_) => gate.future);
      final connection = ConnectionSource(
        authorizationHeader: () => 'Bearer same',
        client: client,
        apiBaseUrl: 'https://follow.fixture',
      );
      final source = NowSocialEdgeSource(
        connection,
        identityEpoch: () => epoch,
      );
      final read = source.followsPage();
      epoch++;
      epoch++;
      gate.complete(_page([_row(media: _media())]));
      await expectLater(read, throwsStateError);
      connection.dispose();
      client.close();
    },
  );
  testWidgets(
    'followTarget three canonical non-person callbacks preserve IDs and Person path',
    (t) async {
      final f = _Fixture()
        ..rows = [
          _row(),
          _row(
            type: 'ORGANIZATION',
            id: _organization,
            followID: _followOrg,
            label: '隔离组织',
          ),
          _row(
            type: 'BUSINESS',
            id: _business,
            followID: _followBusiness,
            label: '隔离商家',
          ),
          _row(
            type: 'PERSON',
            id: _person,
            followID: _followPerson,
            label: '隔离人物',
          ),
        ];
      await f.mount(t);
      for (final entry in [
        ('COMMUNITY', _community),
        ('ORGANIZATION', _organization),
        ('BUSINESS', _business),
        ('PERSON', _person),
      ]) {
        await t.ensureVisible(f.tile(entry.$1, entry.$2));
        await t.tap(f.tile(entry.$1, entry.$2));
        await t.pump();
      }
      expect(f.targets.map((e) => (e.type, e.targetID)).toList(), [
        ('COMMUNITY', _community),
        ('ORGANIZATION', _organization),
        ('BUSINESS', _business),
      ]);
      expect(f.targets.map((e) => e.id).toList(), [
        _follow,
        _followOrg,
        _followBusiness,
      ]);
      expect(f.persons, [_person]);
      expect(f.requests.where((r) => r.url.path.contains('/avatar/')), isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      expect(f.client.closed, isFalse);
      await f.client.get(Uri.parse('https://follow.fixture/borrowed-check'));
    },
  );
  testWidgets('followTarget no callback remains honest non-button entity row', (
    t,
  ) async {
    final f = _Fixture();
    await f.mount(t, callback: false);
    expect(t.widget<ListTile>(f.tile('COMMUNITY', _community)).onTap, isNull);
    expect(find.byIcon(Icons.groups_outlined), findsOneWidget);
    expect(find.byType(PersonAvatar), findsNothing);
    expect(f.targets, isEmpty);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets(
    'followTarget actual controlled Image version delete and hidden source retire',
    (t) async {
      final f = _Fixture()..rows = [_row(media: _media())];
      await f.mount(t);
      await _decodeVisibleAvatar(t);
      expect(find.byType(CommunityAvatar), findsOneWidget);
      expect(find.byType(Image), findsOneWidget);
      expect(t.widget<RawImage>(find.byType(RawImage)).image, isNotNull);
      expect(find.byType(PersonAvatar), findsNothing);
      final oldTap = f.tapCallback(t);
      f.rows = [_row(media: _media(version: 2))];
      f.model.changed();
      await _decodeVisibleAvatar(t);
      oldTap();
      expect(f.targets, isEmpty);
      expect(
        t
            .widget<CommunityAvatar>(find.byType(CommunityAvatar))
            .metadata!
            .version,
        2,
      );
      expect(find.byType(Image), findsOneWidget);
      expect(t.widget<RawImage>(find.byType(RawImage)).image, isNotNull);
      expect(
        f.requests.where((r) => r.url.path.endsWith('/avatar/1/content')),
        hasLength(1),
      );
      expect(
        f.requests.where((r) => r.url.path.endsWith('/avatar/2/content')),
        hasLength(1),
      );
      final versionTwoTap = f.tapCallback(t);
      f.rows = [_row()];
      f.model.changed();
      await _settle(t);
      versionTwoTap();
      expect(f.targets, isEmpty);
      expect(find.byType(Image), findsNothing);
      expect(find.byIcon(Icons.groups_outlined), findsOneWidget);
      f.rows = [];
      f.model.changed();
      await _settle(t);
      expect(f.tile('COMMUNITY', _community), findsNothing);
      expect(f.targets, isEmpty);
      expect(
        f.requests.every(
          (r) =>
              r.url.path.startsWith('/v1/me/') ||
              r.url.path.startsWith('/v1/communities/'),
        ),
        isTrue,
      );
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
  );
  for (final axis in ['actor', 'source', 'page']) {
    testWidgets(
      'followTarget $axis ABA retires late pixels and original callback',
      (t) async {
        final gate = Completer<http.Response>();
        final f = _Fixture()
          ..rows = [_row(media: _media())]
          ..contentGate = gate;
        await f.mount(t);
        expect(
          f.requests.any((r) => r.url.path.endsWith('/avatar/1/content')),
          isTrue,
        );
        final oldTap = f.tapCallback(t);
        ConnectionSource? other;
        _TrackedClient? otherClient;
        if (axis == 'actor') {
          f.model.token = 'Bearer B';
          f.model.owner = _person;
          f.rows = [];
          f.model.changed();
          await _settle(t);
          f.model.token = 'Bearer follow-A';
          f.model.owner = _owner;
          f.rows = [_row()];
          f.model.changed();
          await _settle(t);
        } else if (axis == 'source') {
          final original = f.model.source;
          otherClient = _TrackedClient((_) async => _page([]));
          other = ConnectionSource(
            authorizationHeader: () => f.model.token,
            client: otherClient,
            apiBaseUrl: 'https://other.fixture',
          );
          f.model.source = NowSocialEdgeSource(
            other,
            identityEpoch: () => f.model.epoch,
          );
          f.model.changed();
          await _settle(t);
          f.rows = [_row()];
          f.model.source = original;
          f.model.changed();
          await _settle(t);
        } else {
          await t.tap(find.byKey(const ValueKey('edge-filter-friends')));
          await _settle(t);
          f.rows = [_row()];
          await t.tap(find.byKey(const ValueKey('edge-filter-follows')));
          await _settle(t);
        }
        gate.complete(http.Response.bytes(avatarJPEG(), 200));
        await _settle(t);
        oldTap();
        expect(f.targets, isEmpty);
        expect(find.byType(Image), findsNothing);
        expect(find.byType(PersonAvatar), findsNothing);
        if (axis == 'actor') {
          // Owner changes intentionally return to recent contacts. Returning
          // A does not resurrect A's old followed-list selection or its rows.
          expect(f.tile('COMMUNITY', _community), findsNothing);
          expect(f.targets, isEmpty);
        } else {
          await t.tap(f.tile('COMMUNITY', _community));
          expect(f.targets.single.targetID, _community);
        }
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        other?.dispose();
        otherClient?.close();
      },
    );
  }
  testWidgets(
    'followTarget valid append preserves prior Image and row callback',
    (t) async {
      final more = Completer<http.Response>();
      final f = _Fixture()
        ..rows = [_row(media: _media())]
        ..next = 'valid_next_page'
        ..moreGate = more;
      await f.mount(t);
      final initialTap = f.tapCallback(t);
      expect(find.byType(Image), findsOneWidget);
      final button = find.byKey(const ValueKey('edge-more-follows'));
      await t.scrollUntilVisible(
        button,
        40,
        scrollable: find.descendant(
          of: find.byKey(const ValueKey('edge-list-contacts-follows')),
          matching: find.byType(Scrollable),
        ),
      );
      await t.ensureVisible(button);
      await t.pumpAndSettle();
      expect(button.hitTestable(), findsOneWidget);
      await t.tap(button);
      await t.pump();
      expect(
        f.requests.where(
          (r) => r.url.queryParameters['cursor'] == 'valid_next_page',
        ),
        hasLength(1),
      );
      expect(find.byType(Image), findsOneWidget);
      more.complete(
        _page([
          _row(
            type: 'ORGANIZATION',
            id: _organization,
            followID: _followOrg,
            label: '续页合成组织',
          ),
        ]),
      );
      await _settle(t);
      expect(find.byType(Image), findsOneWidget);
      expect(
        f.requests.where((r) => r.url.path.contains('/avatar/')),
        hasLength(1),
      );
      initialTap();
      expect(f.targets.single.targetID, _community);
      expect(f.tile('ORGANIZATION', _organization), findsOneWidget);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
  );
  for (final denial in ['forbidden', 'pixels']) {
    testWidgets(
      'followTarget $denial thumbnail falls back without unauthorized pixels',
      (t) async {
        final f = _Fixture()..rows = [_row(media: _media())];
        f.contentStatus = denial == 'forbidden' ? 403 : 200;
        f.badPixels = denial == 'pixels';
        await f.mount(t);
        expect(find.byType(Image), findsNothing);
        expect(find.byType(CommunityAvatar), findsOneWidget);
        expect(find.byType(PersonAvatar), findsNothing);
        expect(
          f.requests.where((r) => r.url.path.contains('/avatar/')),
          hasLength(1),
        );
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
      },
    );
  }
}
