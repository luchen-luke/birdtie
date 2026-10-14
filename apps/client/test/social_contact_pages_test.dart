import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_controller.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_pending_store.dart';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/now_social_edge.dart';
import 'package:birdtie_client/src/workspace/now_social_edge_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'connection_request_review_entry_test.dart'
    show reviewAuth, reviewAuthResponse, reviewOwner;

String _id(int n) => '77777777-7777-4777-8777-${n.toString().padLeft(12, '0')}';
Map<String, dynamic> _tie(int n) => {
  'id': _id(n),
  'otherAccountId': _id(1000 + n),
  'otherName': '好友$n',
  'createdAt': '2026-10-13T00:00:00Z',
};
Map<String, dynamic> _follow(int n) => {
  'id': _id(n),
  'target': {'targetType': 'PERSON', 'targetId': _id(1000 + n)},
  'label': '关注$n',
  'createdAt': '2026-10-13T00:00:00Z',
};
Map<String, dynamic> _request(int n, {String scope = 'friend'}) => {
  'id': _id(n),
  'direction': 'incoming',
  'otherAccountId': _id(1000 + n),
  'otherName': '申请$n',
  'cityId': 'test',
  'note': '受控申请',
  'scope': scope,
  'state': 'pending',
  'createdAt': '2026-10-13T00:00:00Z',
  'expiresAt': '2099-10-13T00:00:00Z',
};
http.Response _wire(dynamic data, {int status = 200}) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);
http.Response _page(List<Map<String, dynamic>> rows, {String? next}) =>
    _wire({'items': rows, 'nextCursor': next});

Widget _edge(
  NowSocialEdgeSource source,
  ValueNotifier<int> changes,
  String? Function() owner, {
  bool followed = false,
}) => MaterialApp(
  home: Scaffold(
    body: SizedBox(
      width: 320,
      height: 600,
      child: NowSocialEdge(
        key: const ValueKey('contact-page-edge'),
        open: true,
        source: source,
        identityChanges: changes,
        current: () => true,
        accountID: owner,
        workspaceID: () => null,
        initialSection: NowSocialSection.contacts,
        initialFollowedContacts: followed,
        onOpenAuthor: (_) {},
        onOpenConversation: (_) {},
        onOpenManagement: () {},
        onSignIn: () {},
        onUsePersonal: () {},
      ),
    ),
  ),
);

Future<void> _tapMore(
  WidgetTester t,
  String key, {
  bool settleAfter = true,
}) async {
  final more = find.byKey(ValueKey(key));
  await t.scrollUntilVisible(
    more,
    220,
    scrollable: find.byType(Scrollable).last,
  );
  // scrollUntilVisible ends with ensureVisible's jump; lay out its new offset
  // before inspecting the button or dispatching a real pointer event.
  await t.pumpAndSettle();
  expect(t.getSize(more).height, greaterThanOrEqualTo(48));
  expect(more.hitTestable(), findsOneWidget);
  await t.tap(more.hitTestable());
  if (settleAfter) {
    await t.pumpAndSettle();
  } else {
    await t.pump();
  }
}

void main() {
  testWidgets(
    'expired contact snapshot restarts first page only after explicit retry',
    (t) async {
      final changes = ValueNotifier(0), calls = <http.Request>[];
      var firsts = 0;
      final client = MockClient((r) async {
        calls.add(r);
        if (r.url.queryParameters['cursor'] != null)
          return _wire({}, status: 409);
        firsts++;
        return _page([_tie(firsts)], next: firsts == 1 ? 'snapshot_old' : null);
      });
      final wire = ConnectionSource(
        authorizationHeader: () => 'Bearer a',
        client: client,
        apiBaseUrl: 'https://pages.fixture',
      );
      addTearDown(changes.dispose);
      addTearDown(wire.dispose);
      addTearDown(client.close);
      await t.pumpWidget(
        _edge(NowSocialEdgeSource(wire), changes, () => reviewOwner),
      );
      await t.pumpAndSettle();
      await _tapMore(t, 'edge-more-friends');
      expect(find.text('好友1'), findsOneWidget);
      expect(calls, hasLength(2));
      expect(find.text('列表已更新，重新加载'), findsOneWidget);
      expect(calls.last.url.queryParameters, {
        'limit': '20',
        'cursor': 'snapshot_old',
      });
      await _tapMore(t, 'edge-more-friends');
      expect(calls, hasLength(3));
      expect(calls.last.url.queryParameters, {'limit': '20'});
      expect(find.text('好友2'), findsOneWidget);
      expect(find.text('好友1'), findsNothing);
      await t.pumpWidget(const SizedBox());
    },
  );

  for (final change in ['identity', 'source']) {
    testWidgets(
      'late contact continuation retires after $change and new cursor starts empty',
      (t) async {
        final changes = ValueNotifier(0),
            gate = Completer<http.Response>(),
            calls = <http.Request>[];
        String? token = 'Bearer a';
        var fresh = false;
        final client = MockClient((r) async {
          calls.add(r);
          if (r.url.queryParameters['cursor'] != null) return gate.future;
          return _page([_tie(fresh ? 2 : 1)], next: fresh ? null : 'old_more');
        });
        final wire = ConnectionSource(
          authorizationHeader: () => token,
          client: client,
          apiBaseUrl: 'https://pages.fixture',
        );
        var source = NowSocialEdgeSource(
          wire,
          identityEpoch: () => changes.value,
        );
        addTearDown(changes.dispose);
        addTearDown(wire.dispose);
        addTearDown(client.close);
        await t.pumpWidget(_edge(source, changes, () => reviewOwner));
        await t.pumpAndSettle();
        expect(calls, hasLength(1));
        await _tapMore(t, 'edge-more-friends', settleAfter: false);
        expect(calls, hasLength(2));
        expect(calls.last.url.queryParameters, {
          'limit': '20',
          'cursor': 'old_more',
        });
        fresh = true;
        if (change == 'identity') {
          token = null;
          changes.value++;
          await t.pumpAndSettle();
          expect(find.text('好友1'), findsNothing);
          token = 'Bearer same-owner-new';
          changes.value++;
        } else {
          source = NowSocialEdgeSource(wire);
          await t.pumpWidget(_edge(source, changes, () => reviewOwner));
        }
        await t.pumpAndSettle();
        expect(calls, hasLength(3));
        gate.complete(_page([_tie(99)], next: 'retired_more'));
        await t.pumpAndSettle();
        expect(calls, hasLength(3));
        expect(find.text('好友2'), findsOneWidget);
        expect(find.text('好友99'), findsNothing);
        expect(find.byKey(const Key('edge-more-friends')), findsNothing);
        expect(calls.last.url.queryParameters, {'limit': '20'});
        await t.pumpWidget(const SizedBox());
      },
    );
  }

  test(
    'three real social page routes use limit20 and opaque cursors, no legacy fallback',
    () async {
      final calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        return _page([
          switch (r.url.path) {
            '/v1/me/ties/page' => _tie(1),
            '/v1/me/follows/page' => _follow(1),
            '/v1/me/connection-requests/page' => _request(1),
            _ => throw StateError('unexpected old list'),
          },
        ], next: r.url.queryParameters['cursor'] == null ? 'opaque_1' : null);
      });
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer a',
        client: client,
        apiBaseUrl: 'https://pages.fixture',
      );
      final edge = NowSocialEdgeSource(source);
      addTearDown(source.dispose);
      addTearDown(client.close);
      expect((await edge.friendsPage()).items.single.otherName, '好友1');
      expect((await edge.followsPage()).items.single.isPerson, isTrue);
      expect((await edge.requestsPage()).items.single.scope, 'friend');
      await edge.friendsPage(cursor: 'opaque_1');
      await source.tiesPage(cursor: 'opaque_1');
      await source.requestsPage(cursor: 'opaque_1');
      expect(
        calls.take(3).map((r) => r.url.queryParameters),
        everyElement({'limit': '20'}),
      );
      expect(
        calls.skip(3).map((r) => r.url.queryParameters),
        everyElement({'limit': '20', 'cursor': 'opaque_1'}),
      );
      expect(calls.map((r) => r.method), everyElement('GET'));
    },
  );

  test(
    'closed page rejects missing keys, hasMore, oversized/duplicate rows and stuck cursors',
    () {
      final valid = {
        'items': [_tie(1)],
        'nextCursor': null,
      };
      for (final bad in [
        {
          'items': [_tie(1)],
        },
        {'nextCursor': null},
        {...valid, 'hasMore': false},
        {...valid, 'nextCursor': ''},
        {...valid, 'nextCursor': 'same'},
        {'items': [], 'nextCursor': 'another'},
        {
          'items': [_tie(1), _tie(1)],
          'nextCursor': null,
        },
        {
          'items': [for (var i = 0; i < 21; i++) _tie(i)],
          'nextCursor': null,
        },
      ]) {
        expect(
          () => SocialListPage<FriendTie>.fromJson(
            bad,
            decode: socialPageFriend,
            key: (v) => v.otherAccountId,
            previousCursor: 'same',
          ),
          throwsFormatException,
        );
      }
    },
  );

  test(
    'old-owner page including same-token ABA is retired; borrowed client stays usable',
    () async {
      final changes = ValueNotifier(0), gate = Completer<http.Response>();
      var reads = 0;
      final client = MockClient(
        (_) async => ++reads == 1 ? gate.future : _page([_tie(2)]),
      );
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer same',
        client: client,
        apiBaseUrl: 'https://pages.fixture',
        identityChanges: changes,
      );
      addTearDown(source.dispose);
      addTearDown(client.close);
      addTearDown(changes.dispose);
      final old = source.tiesPage();
      final rejected = expectLater(old, throwsA(isA<http.ClientException>()));
      await Future<void>.delayed(Duration.zero);
      changes.value = 1;
      changes.value = 2;
      gate.complete(_page([_tie(1)], next: 'old'));
      await rejected;
      expect((await source.tiesPage()).items.single.otherName, '好友2');
    },
  );

  for (final followed in [false, true]) {
    testWidgets(
      '${followed ? 'follow' : 'friend'} pages preserve rows on more failure and deduplicate retry',
      (t) async {
        final changes = ValueNotifier(0), calls = <http.Request>[];
        var moreReads = 0;
        final client = MockClient((r) async {
          calls.add(r);
          final make = followed ? _follow : _tie;
          if (r.url.queryParameters['cursor'] == null)
            return _page([
              for (var i = 1; i <= 20; i++) make(i),
            ], next: 'next_20');
          if (++moreReads == 1) return _wire({}, status: 503);
          return _page([make(20), make(21)]);
        });
        final wire = ConnectionSource(
          authorizationHeader: () => 'Bearer a',
          client: client,
          apiBaseUrl: 'https://pages.fixture',
        );
        final source = NowSocialEdgeSource(wire);
        addTearDown(changes.dispose);
        addTearDown(wire.dispose);
        addTearDown(client.close);
        await t.pumpWidget(
          _edge(source, changes, () => reviewOwner, followed: followed),
        );
        await t.pumpAndSettle();
        expect(calls, hasLength(1));
        expect(calls.single.url.queryParameters, {'limit': '20'});
        final kind = followed ? 'follows' : 'friends', key = 'edge-more-$kind';
        await _tapMore(t, key);
        expect(calls, hasLength(2));
        expect(moreReads, 1);
        expect(find.text('重试加载更多'), findsOneWidget);
        final list = t.widget<ListView>(
          find.byKey(ValueKey('edge-list-contacts-$kind')),
        );
        expect(
          (list.childrenDelegate as SliverChildListDelegate).children,
          hasLength(21),
        );
        await _tapMore(t, key);
        expect(calls, hasLength(3));
        expect(moreReads, 2);
        final after = t.widget<ListView>(
          find.byKey(ValueKey('edge-list-contacts-$kind')),
        );
        expect(
          (after.childrenDelegate as SliverChildListDelegate).children,
          hasLength(21),
        );
        expect(find.byKey(ValueKey(key)), findsNothing);
        expect(
          calls.map((r) => r.url.path),
          everyElement('/v1/me/${followed ? 'follows' : 'ties'}/page'),
        );
        expect(
          calls.skip(1).map((r) => r.url.queryParameters['cursor']),
          everyElement('next_20'),
        );
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets(
    'switching filter retires old continuation and resets its cursor',
    (t) async {
      final changes = ValueNotifier(0),
          gate = Completer<http.Response>(),
          calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        if (r.url.path.endsWith('/follows/page')) return _page([_follow(9)]);
        if (r.url.queryParameters['cursor'] != null) return gate.future;
        return _page([_tie(1)], next: 'friends_more');
      });
      final wire = ConnectionSource(
        authorizationHeader: () => 'Bearer a',
        client: client,
        apiBaseUrl: 'https://pages.fixture',
      );
      final source = NowSocialEdgeSource(wire);
      addTearDown(changes.dispose);
      addTearDown(wire.dispose);
      addTearDown(client.close);
      await t.pumpWidget(_edge(source, changes, () => reviewOwner));
      await t.pumpAndSettle();
      expect(calls, hasLength(1));
      await _tapMore(t, 'edge-more-friends', settleAfter: false);
      expect(calls, hasLength(2));
      expect(calls.last.url.queryParameters, {
        'limit': '20',
        'cursor': 'friends_more',
      });
      final follows = find
          .byKey(const Key('edge-filter-follows'))
          .hitTestable();
      expect(follows, findsOneWidget);
      await t.tap(follows);
      await t.pumpAndSettle();
      expect(calls, hasLength(3));
      expect(calls.last.url.path, '/v1/me/follows/page');
      expect(calls.last.url.queryParameters, {'limit': '20'});
      gate.complete(_page([_tie(99)], next: 'stale'));
      await t.pumpAndSettle();
      expect(calls, hasLength(3));
      expect(find.text('关注9'), findsOneWidget);
      expect(find.text('好友99'), findsNothing);
      final friends = find
          .byKey(const Key('edge-filter-friends'))
          .hitTestable();
      expect(friends, findsOneWidget);
      await t.tap(friends);
      await t.pumpAndSettle();
      expect(calls, hasLength(4));
      expect(calls.last.url.path, '/v1/me/ties/page');
      expect(calls.last.url.queryParameters, {'limit': '20'});
      expect(find.text('好友1'), findsOneWidget);
      expect(find.text('好友99'), findsNothing);
      await t.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'request scope with no matching first page still reaches later matching records',
    (t) async {
      final calls = <http.Request>[];
      var moreReads = 0;
      final client = MockClient((r) async {
        if (r.url.path != '/v1/me/connection-requests/page')
          return reviewAuthResponse(r);
        calls.add(r);
        if (r.url.queryParameters['cursor'] == null)
          return _page([_request(1, scope: 'conversation')], next: 'mixed');
        if (++moreReads == 1) return _wire({}, status: 503);
        return _page([_request(101)]);
      });
      final auth = await reviewAuth(client), changes = ValueNotifier(0);
      final wire = ConnectionSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://pages.fixture',
      );
      final source = NowSocialEdgeSource(wire);
      addTearDown(auth.dispose);
      addTearDown(changes.dispose);
      addTearDown(wire.dispose);
      addTearDown(client.close);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SocialRequestsPanel(
              auth: auth,
              source: source,
              scope: 'friend',
              identityChanges: changes,
              current: () => true,
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('暂无好友申请'), findsNothing);
      expect(calls, hasLength(1));
      await _tapMore(t, 'social-requests-more');
      expect(calls, hasLength(2));
      expect(moreReads, 1);
      expect(find.text('重试加载更多'), findsOneWidget);
      await _tapMore(t, 'social-requests-more');
      expect(calls, hasLength(3));
      expect(moreReads, 2);
      expect(find.text('申请101'), findsOneWidget);
      expect(find.text('申请1'), findsNothing);
      expect(find.byKey(const Key('social-requests-more')), findsNothing);
      expect(
        calls.skip(1).map((r) => r.url.queryParameters['cursor']),
        everyElement('mixed'),
      );
      await t.pumpWidget(const SizedBox());
    },
  );

  test(
    'review beyond first100 uses exact request twice and a changed response cannot write',
    () async {
      final id = _id(101), calls = <String>[];
      var reads = 0, posts = 0;
      final client = MockClient((r) async {
        calls.add(r.url.path);
        if (r.method != 'GET') {
          posts++;
          return _wire({}, status: 500);
        }
        expect(r.url.path, '/v1/me/connection-requests/$id');
        return _wire({..._request(101), if (++reads > 1) 'state': 'withdrawn'});
      });
      final wire = ConnectionSource(
        authorizationHeader: () => 'Bearer a',
        client: client,
        apiBaseUrl: 'https://pages.fixture',
      );
      final controller = ConnectionRequestReviewController(
        requestID: id,
        source: wire,
        accountID: () => reviewOwner,
        authorizationHeader: () => 'Bearer a',
        pendingStore: MemoryConnectionReviewPendingStore(),
        now: () => DateTime.utc(2026, 10, 14),
      );
      addTearDown(controller.dispose);
      addTearDown(wire.dispose);
      addTearDown(client.close);
      await controller.load();
      expect(controller.request!.id, id);
      final approval = controller.preview('accept');
      expect(approval, isNotNull);
      await controller.submit(approval!);
      expect(posts, 0);
      expect(reads, 2);
      expect(controller.message, contains('申请已变化'));
      expect(calls, everyElement('/v1/me/connection-requests/$id'));
    },
  );
}
