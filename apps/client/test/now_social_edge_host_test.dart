import 'package:birdtie_client/src/workspace/friend_image.dart';
import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/birdtie_login_sheet.dart';
import 'package:birdtie_client/src/auth/dev_phone_login_sheet.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/config/birdtie_environment.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/friend_conversation_thread.dart';
import 'package:birdtie_client/src/workspace/friend_audio.dart';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:birdtie_client/src/workspace/now_social_edge.dart';
import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:birdtie_client/src/workspace/now_public_place_panel.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/now_glass_surface.dart';
import 'package:birdtie_client/src/workspace/top_controls.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/sidebar.dart';
import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// Production MapWorkspace with a synthetic HTTP boundary and no native map.
// These tests verify flow and ownership; they are not two-account/device E2E.
import 'now_scope_recovery_test.dart'
    show NowFixture, nowField, nowSend, nowTap;
import 'friend_audio_test.dart' show audioFixture;
import 'private_moment_media_controller_test.dart'
    show syntheticPrivatePNG, TestMomentImagePicker;

const _ownerA = '11111111-1111-4111-8111-111111111111';
const _ownerB = '77777777-7777-4777-8777-777777777777';
const _friend = '22222222-2222-4222-8222-222222222222';
const _tie = '33333333-3333-4333-8333-333333333333';
const _chat = '44444444-4444-4444-8444-444444444444';
const _otherChat = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';
const _otherFriend = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';
const _follow = '55555555-5555-4555-8555-555555555555';
const _organization = '66666666-6666-4666-8666-666666666666';
const _moment = '88888888-8888-4888-8888-888888888888';
const _place = '99999999-9999-4999-8999-999999999999';
const _tokenA = 'Bearer synthetic-now-host-a';
const _tokenB = 'Bearer synthetic-now-host-b';
const _defaultBase = 'http://now-host-zone.test';
const _bounds = MapBounds(west: -2.2, south: 57.1, east: -2, north: 57.3);
const _edgePaths = {
  '/v1/me/ties/page',
  '/v1/me/conversations/page',
  '/v1/me/follows/page',
};

http.Response _response(Object? data, {int status = 200}) => http.Response(
  jsonEncode({'data': data}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

/// Synthetic transport only. Records the synchronous production send boundary.
/// All UUIDs, labels, tokens and deadlines in this file are synthetic fixtures.
class _HostWire extends http.BaseClient {
  _HostWire(this.label);
  final String label;
  String? otherOwnerLabel;
  bool includeSecondConversation = false;
  bool publicMoment = false;
  bool includeHistory = false;
  int unreadMessages = 3;
  int unreadNotifications = 0;
  http.Response Function(http.BaseRequest)? notificationPage;
  Completer<http.Response>? pendingNotificationPage;
  final dispatch = <Map<String, String>>[];
  final observedAt = DateTime.now().toUtc();
  Completer<http.Response>? pendingTies;
  Completer<http.Response>? pendingConversations;
  Completer<http.Response>? pendingMessages;
  Completer<http.Response>? pendingPublicMap;
  Completer<http.Response>? pendingChat;
  int conversationStatus = 200;
  int get conversationGets =>
      dispatch.where((r) => r['path'] == '/v1/me/conversations/page').length;
  int get messageGets => dispatch
      .where((r) => r['path'] == '/v1/me/conversations/$_chat/messages/page')
      .length;
  int get chatOpens => dispatch
      .where(
        (r) =>
            r['method'] == 'POST' &&
            r['path'] == '/v1/me/ties/$_tie/conversation',
      )
      .length;

  http.Response conversations({String? accountLabel}) => _response({
    'items': [
      {
        'id': _chat,
        'otherAccountId': _friend,
        'otherName': '${accountLabel ?? label}合成私信对象',
        'unreadCount': unreadMessages,
        'lastMessagePreview': '合成最近消息预览',
        'createdAt': observedAt.toIso8601String(),
      },
      if (includeSecondConversation)
        {
          'id': _otherChat,
          'otherAccountId': _otherFriend,
          'otherName': '${accountLabel ?? label}合成另一私信对象',
          'unreadCount': 0,
          'lastMessagePreview': '合成另一段最近消息',
          'createdAt': observedAt.toIso8601String(),
        },
    ],
    'nextCursor': null,
    'unreadCount': unreadMessages,
  }, status: conversationStatus);
  http.Response conversation() => _response({
    'id': _chat,
    'otherAccountId': _friend,
    'otherName': '$label合成好友',
    'unreadCount': 0,
    'createdAt': observedAt.toIso8601String(),
  });
  http.Response recentConversation({String? accountLabel}) => _response({
    'id': _chat,
    'otherAccountId': _friend,
    'otherName': '${accountLabel ?? label}合成私信对象',
    'unreadCount': unreadMessages,
    'createdAt': observedAt.toIso8601String(),
    'lastMessagePreview': '合成最近消息预览',
  });
  http.Response messages({
    String conversationID = _chat,
    bool withItem = false,
  }) => _response({
    'items': [
      if (withItem)
        {
          'id': 'cccccccc-cccc-4ccc-8ccc-cccccccccccc',
          'conversationId': conversationID,
          'senderAccountId': _friend,
          'speakerKind': 'human',
          'body': '合成真人历史消息',
          'createdAt': observedAt.toIso8601String(),
        },
    ],
    'nextCursor': null,
  });
  Completer<http.Response>? pendingOrganizations;
  int closes = 0;

  int get edgeGets =>
      dispatch.where((r) => _edgePaths.contains(r['path'])).length;
  int get authorGets => dispatch.where((r) => r['author']!.isNotEmpty).length;
  int get organizationGets =>
      dispatch.where((r) => r['path'] == '/v1/me/organizations').length;

  http.Response ties({String? accountLabel}) => _response([
    {
      'id': _tie,
      'otherAccountId': _friend,
      'otherName': '${accountLabel ?? label}合成好友',
    },
  ]);

  Map<String, dynamic> _map(String? author, bool private) => {
    'schemaVersion': author == null
        ? 'typed-map-layers-v1'
        : 'typed-map-layers-v2',
    'cityId': 'alpha',
    'scope': author != null
        ? 'PUBLIC_AUTHOR'
        : private
        ? 'SELF_PRIVATE'
        : 'PUBLIC',
    if (author != null) 'authorId': author,
    'observedAt': observedAt.toIso8601String(),
    'validUntil': observedAt.add(const Duration(minutes: 2)).toIso8601String(),
    'truncated': false,
    'items': private
        ? <Object>[]
        : [
            {
              'kind': author == null && !publicMoment ? 'PLACE' : 'MOMENT',
              'id': author == null && !publicMoment ? _place : _moment,
              'title': author == null ? '合成公共地点' : '$label合成作者公开地点动态',
              'entityRef': {
                'type': author == null && !publicMoment ? 'PLACE' : 'MOMENT',
                'id': author == null && !publicMoment ? _place : _moment,
              },
              'detailRef': {
                'type': author == null && !publicMoment ? 'PLACE' : 'MOMENT',
                'id': author == null && !publicMoment ? _place : _moment,
              },
              'anchor': {
                'placeId': _place,
                'label': '合成公开场地',
                'coordinateSystem': 'wgs84',
                'precision': 'point',
                'latitude': 57.2,
                'longitude': -2.1,
              },
              'sourceVersion': 'synthetic-now-host-author-version-1',
            },
          ],
  };

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    final author = request.url.queryParameters['authorId'];
    final accountLabel = request.headers['Authorization'] == _tokenB
        ? otherOwnerLabel ?? label
        : label;
    dispatch.add({
      'method': request.method,
      'host': request.url.host,
      'path': request.url.path,
      'authorization': request.headers['Authorization'] ?? '',
      'author': author ?? '',
      'view': request.url.queryParameters['view'] ?? '',
      'limit': request.url.queryParameters['limit'] ?? '',
    });
    // Only an explicit friend-row tap may open the existing authoritative chat.
    // Sending, applying and all unexpected mutations fail visibly. Only the
    // dedicated history fixture allows its verified visible message read ACK.
    if (includeHistory &&
        request.method == 'POST' &&
        request.url.path == '/v1/me/conversations/$_chat/read') {
      expect(request, isA<http.Request>());
      expect(jsonDecode((request as http.Request).body), {
        'throughMessageId': 'cccccccc-cccc-4ccc-8ccc-cccccccccccc',
      });
      unreadMessages = 0;
      return Future.value(
        http.StreamedResponse(
          Stream.value(_response(null).bodyBytes),
          200,
          request: request,
        ),
      );
    }
    if (request.method == 'POST' &&
        request.url.path == '/v1/me/ties/$_tie/conversation') {
      return (pendingChat?.future ?? Future.value(conversation())).then(
        (r) => http.StreamedResponse(
          Stream.value(r.bodyBytes),
          r.statusCode,
          headers: r.headers,
          request: request,
        ),
      );
    }
    if (request.method != 'GET') {
      throw StateError('unexpected synthetic host mutation: ${request.method}');
    }
    final path = request.url.path;
    final Future<http.Response> response;
    if (path == '/v1/me/ties/page') {
      response =
          pendingTies?.future ?? Future.value(ties(accountLabel: accountLabel));
    } else if (path == '/v1/me/conversations/page') {
      response =
          pendingConversations?.future ??
          Future.value(conversations(accountLabel: accountLabel));
    } else if (path == '/v1/me/conversations/$_chat/messages/page') {
      response =
          pendingMessages?.future ??
          Future.value(messages(withItem: includeHistory));
    } else if (path == '/v1/me/conversations/$_chat') {
      response = Future.value(recentConversation(accountLabel: accountLabel));
    } else if (path == '/v1/me/conversations/$_otherChat/messages/page') {
      response = Future.value(messages(conversationID: _otherChat));
    } else if (path == '/v1/me/follows/page') {
      response = Future.value(
        _response([
          {
            'id': _follow,
            'target': {'targetType': 'PERSON', 'targetId': _friend},
            'label': '$accountLabel合成关注',
            'createdAt': observedAt.toIso8601String(),
          },
        ]),
      );
    } else if (path == '/v1/me/inbox/page') {
      if (pendingNotificationPage != null) {
        response = pendingNotificationPage!.future;
      } else if (notificationPage != null) {
        response = Future.value(notificationPage!(request));
      } else {
        expect(request.url.queryParameters, {
          'view': 'notifications',
          'limit': '3',
        });
        response = Future.value(
          _response({
            'items': <Object>[],
            'nextCursor': null,
            'unreadCount': unreadNotifications,
          }),
        );
      }
    } else if (path == '/v1/me/organizations') {
      response = pendingOrganizations?.future ?? Future.value(_response([]));
    } else if (path.endsWith('/map-layers') ||
        path.endsWith('/map-opportunities')) {
      response = author == null && path.endsWith('/map-layers')
          ? pendingPublicMap?.future ??
                Future.value(_response(_map(null, false)))
          : Future.value(
              _response(_map(author, path.endsWith('/map-opportunities'))),
            );
    } else if (path.contains('/places/') || path.contains('/moments/')) {
      // Detail loading is outside this proposal; failure is not an empty read.
      response = Future.value(_response(null, status: 404));
    } else {
      response = Future.value(_response([]));
    }
    final pageResponse = response.then((r) {
      if (const {
            '/v1/me/ties/page',
            '/v1/me/follows/page',
            '/v1/me/connection-requests/page',
          }.contains(path) &&
          r.statusCode == 200) {
        final value = jsonDecode(utf8.decode(r.bodyBytes))['data'];
        if (value is List) {
          return _response({
            'items': [
              for (final row in value)
                {
                  ...row as Map<String, dynamic>,
                  if (path == '/v1/me/ties/page')
                    'createdAt': '2026-10-10T11:00:00Z',
                },
            ],
            'nextCursor': null,
          });
        }
      }
      return r;
    });
    return pageResponse.then(
      (r) => http.StreamedResponse(
        Stream.value(r.bodyBytes),
        r.statusCode,
        headers: r.headers,
        request: request,
      ),
    );
  }

  @override
  void close() => closes++;
}

/// Each default Client() created by the production host owns this wrapper.
/// Production disposal cannot close the independently borrowed fixture wire.
class _DefaultClient extends http.BaseClient {
  _DefaultClient(this.wire);
  final _HostWire wire;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) =>
      wire.send(request);
  @override
  void close() {}
}

/// Actual auth state and rejection, backed only by synthetic HTTP and memory.
/// Rejection is invoked by another feature, before Now receives its notification.
class _SharedAuthFixture {
  _SharedAuthFixture({bool initiallySignedIn = true}) {
    if (initiallySignedIn) {
      vault.session = const StoredSession(
        token: 'synthetic-now-host-a',
        method: 'dev_phone',
      );
    }
    auth = BirdtieAuthController(
      sessionVault: vault,
      apiBaseUrl: 'https://shared-auth.fixture',
      client: MockClient((request) async {
        if (request.url.path == '/v1/auth/dev-phone/status') {
          return _response({'enabled': true});
        }
        if (request.url.path == '/v1/auth/dev-phone/verify') {
          return _response({'accessToken': nextToken});
        }
        if (request.url.path == '/v1/auth/dev-phone/code') {
          return _response({'accepted': true});
        }
        if (request.url.path == '/v1/me') return _response({'id': nextOwner});
        if (request.url.path.endsWith('/profile')) {
          return _response({'displayName': '合成登录用户'});
        }
        if (request.url.path == '/v1/session/logout') {
          return http.Response('', 204);
        }
        return http.Response('{}', 404);
      }),
    );
  }
  final vault = MemorySessionVault();
  late final BirdtieAuthController auth;
  String nextToken = 'synthetic-now-host-a', nextOwner = _ownerA;
  Future<void> initialize(WidgetTester t) async {
    await t.runAsync(auth.initialize);
    expect(auth.accountID, _ownerA);
  }

  Future<void> login(WidgetTester t, {String owner = _ownerA}) async {
    nextOwner = owner;
    nextToken = owner == _ownerA
        ? 'synthetic-now-host-a-restored'
        : 'synthetic-now-host-b';
    final accepted = await auth.verifyDevPhoneCode('13800138000', '123456');
    expect(accepted, true);
    await t.pumpAndSettle();
    if (find.byType(BirdtieLoginSheet).evaluate().isNotEmpty) {
      // The synthetic controller completion replaces the real login method's
      // completion callback. Dismiss its chooser to inspect the retained Now.
      await nowTap(
        t,
        find.descendant(
          of: find.byType(BirdtieLoginSheet),
          matching: find.text('返回'),
        ),
      );
    }
  }
}

class _Host {
  _Host(
    this.f,
    this.wire, {
    String? fixedTransportToken,
    BirdtieAuthController? auth,
    this.imagePicker,
    this.friendImageEnabled = false,
  }) : auth = auth ?? f.auth {
    connections = ConnectionSource(
      client: wire,
      apiBaseUrl: 'http://now-host-edge.test',
      authorizationHeader: fixedTransportToken == null
          ? () => this.auth.authorizationHeader
          : () => fixedTransportToken,
    );
    provider = ValueNotifier(connections);
  }
  final NowFixture f;
  final _HostWire wire;
  final BirdtieAuthController auth;
  final MomentImagePicker? imagePicker;
  final bool friendImageEnabled;
  final imagePending = MemoryFriendImagePendingStore();
  late final ConnectionSource connections;
  late final ValueNotifier<ConnectionSource> provider;
  final presentation = ValueNotifier<(double, Brightness)>((
    1,
    Brightness.light,
  ));
  final audioPending = MemoryFriendAudioPendingStore();
  final messagePending = MemoryHumanMessagePendingStore();
  final extraSources = <ConnectionSource>[];
  final extraWires = <_HostWire>[];
  bool highContrast = false,
      reducedMotion = false,
      accessibleNavigation = false;
  AgentWorkspaceController workspace(WidgetTester t) => f.workspace(t);
  MapCanvas canvas(WidgetTester t) => t.widget(find.byType(MapCanvas));
  PublicCityMapView map(WidgetTester t) =>
      t.widget(find.byType(PublicCityMapView));

  Future<void> mount(WidgetTester t) async {
    await t.pumpWidget(
      ValueListenableBuilder<(double, Brightness)>(
        valueListenable: presentation,
        builder: (_, appearance, _) => MaterialApp(
          theme: ThemeData(brightness: appearance.$2),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context).copyWith(
              textScaler: TextScaler.linear(appearance.$1),
              highContrast: highContrast,
              disableAnimations: reducedMotion,
              accessibleNavigation: accessibleNavigation,
            ),
            child: child!,
          ),
          home: ValueListenableBuilder<ConnectionSource>(
            valueListenable: provider,
            builder: (_, current, _) => MapWorkspace(
              key: const ValueKey('real-now-social-host'),
              city: f.city,
              auth: auth,
              moments: f.moments,
              agentTaskSource: f.source,
              connectionSource: current,
              friendAudioPendingStore: audioPending,
              humanMessagePendingStore: messagePending,
              taskImagePicker: imagePicker,
              friendImageEnabled: friendImageEnabled,
              friendImagePicker: imagePicker,
              friendImagePendingStore: imagePending,
              seedClient: wire,
              seedApiBaseUrl: 'http://now-host-map.test',
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
  }

  Future<void> primeMap(WidgetTester t) async {
    final actual = canvas(t);
    actual.mapState.initializeViewport(_bounds);
    await actual.layers!.load('alpha', _bounds);
    await t.pumpAndSettle();
    expect(actual.layers!.publicView, isNotNull);
  }

  Future<void> openEdge(WidgetTester t) => nowTap(t, find.byTooltip('联系'));
  Future<void> closeEdge(WidgetTester t) => nowTap(
    t,
    find.byKey(const Key('now-human-close')).evaluate().isNotEmpty
        ? find.byKey(const Key('now-human-close'))
        : find.byTooltip('收起联系'),
  );

  VoidCallback friendAction(WidgetTester t) => t
      .widget<InkWell>(
        find.descendant(
          of: find.byKey(const ValueKey('edge-friend-$_friend')),
          matching: find.byType(InkWell),
        ),
      )
      .onTap!;

  Future<OrganizationWorkspaceController> organizations(WidgetTester t) async {
    await nowTap(t, find.byTooltip('打开侧边栏'));
    final actual = t.widget<Sidebar>(find.byType(Sidebar)).organizations;
    t.state<ScaffoldState>(find.byType(Scaffold).first).closeDrawer();
    await t.pumpAndSettle();
    return actual;
  }

  void replaceProvider() {
    final other = _HostWire('乙');
    final replacement = ConnectionSource(
      client: other,
      apiBaseUrl: 'http://now-host-replacement.test',
      authorizationHeader: () => f.auth.authorizationHeader,
    );
    extraWires.add(other);
    extraSources.add(replacement);
    provider.value = replacement;
  }

  Future<void> close(WidgetTester t) async {
    await f.unmount(t);
    connections.dispose();
    for (final source in extraSources) {
      source.dispose();
    }
    expect(wire.closes, 0, reason: 'MapWorkspace borrows the supplied client');
    for (final other in extraWires) {
      expect(other.closes, 0);
      other.close();
    }
    provider.dispose();
    presentation.dispose();
    f.dispose();
    wire.close();
  }
}

Future<void> _withHost(
  WidgetTester t,
  Future<void> Function(_Host host) body, {
  bool mismatched = false,
  BirdtieAuthController? auth,
  MomentImagePicker? imagePicker,
  bool friendImageEnabled = false,
}) async {
  expect(
    BirdtieEnvironment.apiBaseUrl,
    _defaultBase,
    reason:
        'The real host organization controllers use build-time public base; this suite requires its synthetic URL dart-define.',
  );
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
  FlutterSecureStorage.setMockInitialValues({});
  addTearDown(() => FlutterSecureStorage.setMockInitialValues({}));
  final wire = _HostWire('甲');
  await http.runWithClient(() async {
    final f = NowFixture();
    f.auth.owner = mismatched ? _ownerB : _ownerA;
    f.auth.token = mismatched ? _tokenB : _tokenA;
    final host = _Host(
      f,
      wire,
      fixedTransportToken: mismatched ? _tokenA : null,
      auth: auth,
      imagePicker: imagePicker,
      friendImageEnabled: friendImageEnabled,
    );
    try {
      await host.mount(t);
      await body(host);
      expect(t.takeException(), isNull);
    } finally {
      await host.close(t);
    }
  }, () => _DefaultClient(wire));
}

void _sameMapAndInput(WidgetTester t, Object element, Object state) {
  final map = find.byType(PublicCityMapView);
  expect(map, findsOneWidget);
  expect(map.evaluate().single, same(element));
  expect(t.state(map), same(state));
  expect(find.byType(AgentComposer), findsOneWidget);
  expect(find.byType(TextField), findsOneWidget);
  expect(nowField(), findsOneWidget);
}

Future<void> _section(WidgetTester t, NowSocialSection section) async {
  if (section == NowSocialSection.notifications) {
    await nowTap(t, find.byKey(const Key('now-notifications-button')));
    return;
  }
  if (find.byKey(const Key('edge-back-messages')).evaluate().isNotEmpty) {
    await nowTap(t, find.byKey(const Key('edge-back-messages')));
  }
  if (section == NowSocialSection.messages) {
    if (find.byKey(const Key('edge-back-recent')).evaluate().isNotEmpty) {
      await nowTap(t, find.byKey(const Key('edge-back-recent')));
    } else if (find
        .byKey(const Key('edge-recent-avatars'))
        .evaluate()
        .isEmpty) {
      await nowTap(t, find.byKey(const Key('now-social-button')));
    }
    return;
  }
  if (find.byKey(const Key('edge-filter-friends')).evaluate().isNotEmpty)
    return;
  if (find.byKey(const Key('edge-all-conversations')).evaluate().isEmpty &&
      find.byKey(const ValueKey('edge-tab-contacts')).evaluate().isEmpty) {
    await nowTap(t, find.byKey(const Key('now-social-button')));
  }
  if (find.byKey(const Key('edge-all-conversations')).evaluate().isNotEmpty) {
    await nowTap(t, find.byKey(const Key('edge-all-conversations')));
  }
  await nowTap(t, find.byKey(const ValueKey('edge-tab-contacts')));
}

Finder _chatRow() => find.byKey(const ValueKey('edge-conversation-$_chat'));
VoidCallback _chatAction(WidgetTester t) {
  final widget = t.widget(_chatRow());
  return widget is InkWell ? widget.onTap! : (widget as ListTile).onTap!;
}

bool _hasMessageWrite(_HostWire wire) => wire.dispatch.any(
  (r) => r['method'] != 'GET' && r['path']!.contains('/messages'),
);

void main() {
  testWidgets(
    'matched image gate local selection keeps original map and sole composer; closed gate has no entry',
    (t) async {
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      final picker = TestMomentImagePicker(
        (await t.runAsync(syntheticPrivatePNG))!,
      );
      await _withHost(
        t,
        (h) async {
          final map = find.byType(PublicCityMapView),
              element = map.evaluate().single,
              state = t.state(map);
          await h.openEdge(t);
          await nowTap(t, _chatRow());
          final composer = t.state<AgentComposerState>(
            find.byType(AgentComposer),
          );
          final beforeSelection = h.wire.dispatch.length;
          await t.runAsync(composer.selectFriendImage);
          await t.pumpAndSettle();
          expect(picker.calls, 1);
          expect(
            find.byKey(const Key('now-composer-image-thumbnail')),
            findsOneWidget,
          );
          _sameMapAndInput(t, element, state);
          expect(
            h.wire.dispatch.where(
              (r) => r['path']!.contains('image-message-operations'),
            ),
            isEmpty,
          );
          expect(
            h.wire.dispatch
                .skip(beforeSelection)
                .where((r) => r['path']!.contains('/agent')),
            isEmpty,
            reason:
                'Selecting a local friend image must not invoke Agent; the signed-in host already read agent-seed before this gesture.',
          );
          expect(_hasMessageWrite(h.wire), false);
          expect(h.f.source.queries, isEmpty);
          await nowTap(t, find.byTooltip('移除本机图片'));
          expect(
            find.byKey(const Key('now-composer-image-thumbnail')),
            findsNothing,
          );
          _sameMapAndInput(t, element, state);
        },
        imagePicker: picker,
        friendImageEnabled: true,
      );
      final closedPicker = TestMomentImagePicker(
        (await t.runAsync(syntheticPrivatePNG))!,
      );
      await _withHost(t, (h) async {
        await h.openEdge(t);
        await nowTap(t, _chatRow());
        await t
            .state<AgentComposerState>(find.byType(AgentComposer))
            .selectFriendImage();
        await t.pumpAndSettle();
        expect(closedPicker.calls, 0);
        expect(
          find.byKey(const Key('now-composer-image-thumbnail')),
          findsNothing,
        );
        expect(
          h.wire.dispatch.where(
            (r) => r['path']!.contains('image-message-operations'),
          ),
          isEmpty,
        );
        expect(find.byType(TextField), findsOneWidget);
      }, imagePicker: closedPicker);
    },
  );

  _registerNotificationContentGeometryTests();
  testWidgets('真实宿主社交统一入口语义、主题刷新与地图实例连续', (t) async {
    final handle = t.ensureSemantics();
    try {
      await _withHost(t, (h) async {
        final map = find.byType(PublicCityMapView);
        final originalElement = map.evaluate().single,
            originalState = t.state(map);
        Element parentOfHeader() {
          Element? parent;
          find.byType(TopControls).evaluate().single.visitAncestorElements((e) {
            parent = e;
            return false;
          });
          return parent!;
        }

        final parent = parentOfHeader();
        final closed = t.semantics
            .simulatedAccessibilityTraversal()
            .where((node) => node.label == '联系')
            .single;
        t.binding.pipelineOwner.semanticsOwner!.performAction(
          closed.id,
          SemanticsAction.tap,
        );
        await t.pumpAndSettle();
        final opened = t.semantics
            .simulatedAccessibilityTraversal()
            .where((node) => node.label.startsWith('收起联系'))
            .single;
        expect(find.text('联系'), findsNothing, reason: '删除首层冗余标题，保留工具栏语义');
        expect(h.wire.edgeGets, 1);
        for (final value in [true, false, true]) {
          h.accessibleNavigation = value;
          h.presentation.value = (
            1.0,
            value ? Brightness.dark : Brightness.light,
          );
          await t.pumpAndSettle();
          expect(parentOfHeader(), same(parent));
          final current = t.semantics
              .simulatedAccessibilityTraversal()
              .where((node) => node.label.startsWith('收起联系'))
              .single;
          expect(current.id, opened.id);
          expect(
            current.getSemanticsData().hasAction(SemanticsAction.tap),
            true,
          );
          _sameMapAndInput(t, originalElement, originalState);
          expect(h.wire.edgeGets, 1);
        }
        t.binding.pipelineOwner.semanticsOwner!.performAction(
          opened.id,
          SemanticsAction.tap,
        );
        await t.pumpAndSettle();
        expect(find.byType(NowSocialEdge), findsNothing);
        _sameMapAndInput(t, originalElement, originalState);
      });
    } finally {
      handle.dispose();
    }
  });

  for (final signal in [
    'highContrast',
    'reducedMotion',
    'accessibleNavigation',
  ]) {
    testWidgets('真实宿主深色辅助状态保持局部玻璃及实际降级 $signal', (t) async {
      await _withHost(t, (h) async {
        h.highContrast = signal == 'highContrast';
        h.reducedMotion = signal == 'reducedMotion';
        h.accessibleNavigation = signal == 'accessibleNavigation';
        h.presentation.value = (1.3, Brightness.dark);
        await t.pumpAndSettle();
        await h.openEdge(t);
        final edge = find.byKey(const Key('now-social-local-surface'));
        expect(t.widget(edge), isA<NowGlassSurface>());
        final opaque = signal != 'accessibleNavigation';
        final background = find
            .descendant(
              of: edge,
              matching: find.byWidgetPredicate(
                (w) => w is Material && w.color != null,
              ),
            )
            .first;
        expect(background, findsOneWidget);
        final social = t.getRect(find.byKey(const Key('now-social-edge')));
        expect(t.getRect(background), social);
        expect(
          t.widget<Material>(background).color!.a,
          closeTo(opaque ? 1 : NowUiTokens.glassOpacity, .001),
        );
        expect(
          find.descendant(of: edge, matching: find.byType(BackdropFilter)),
          opaque ? findsNothing : findsOneWidget,
        );
        expect(find.byKey(const Key('now-social-edge-fade')), findsNothing);
        expect(social.width, closeTo(NowUiTokens.socialRailWidth * 1.3, .01));
        expect(
          social.height,
          lessThanOrEqualTo(NowUiTokens.socialPanelMaxHeight),
        );
        expect(h.wire.edgeGets, 1);
        expect(find.byType(PublicCityMapView), findsOneWidget);
      });
    });
  }

  for (final extent in [AgentSheetExtent.expanded, AgentSheetExtent.medium]) {
    testWidgets('真实宿主 $extent 原任务及 Agent 草稿在社交浏览开关后保留', (t) async {
      await _withHost(t, (h) async {
        await nowSend(t, '合成已完成任务：保留这轮回答');
        final ws = h.workspace(t);
        ws.setSheetExtent(extent);
        await t.pumpAndSettle();
        final task = ws.task,
            epoch = ws.taskEpoch,
            history = List.of(ws.conversation);
        final map = find.byType(PublicCityMapView);
        final element = map.evaluate().single, state = t.state(map);
        await t.enterText(nowField(), '未发送 Agent 中文 draft');
        await t.pumpAndSettle();
        await h.openEdge(t);
        final rect = t.getRect(find.byKey(const Key('now-social-edge')));
        final toolbar = t.getRect(find.byKey(const Key('now-social-button')));
        expect(rect.top - toolbar.bottom, inInclusiveRange(0, 8));
        expect(rect.width, NowUiTokens.socialRailWidth);
        expect(
          rect.height,
          lessThanOrEqualTo(NowUiTokens.socialPanelMaxHeight),
        );
        expect(find.byKey(const Key('edge-message-requests')), findsNothing);
        expect(find.byKey(const ValueKey('edge-tab-contacts')), findsNothing);
        await nowTap(t, find.byKey(const Key('edge-all-conversations')));
        expect(
          t.getSize(find.byKey(const Key('now-social-edge'))).width,
          NowUiTokens.socialPanelWidth,
        );
        final requests = find.byKey(const Key('edge-message-requests'));
        expect(t.getSize(requests).height, greaterThanOrEqualTo(48));
        expect(requests.hitTestable(), findsOneWidget);
        expect(find.byKey(const Key('edge-management')), findsNothing);
        expect(find.byType(Drawer), findsNothing);
        expect(find.byType(BottomSheet), findsNothing);
        _sameMapAndInput(t, element, state);
        expect(ws.task, same(task));
        expect(ws.taskEpoch, epoch);
        expect(ws.conversation, orderedEquals(history));
        expect(ws.sheetExtent, extent);
        expect(
          t.widget<TextField>(nowField()).controller!.text,
          '未发送 Agent 中文 draft',
        );
        expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
        expect(h.wire.edgeGets, 1);
        await h.closeEdge(t);
        _sameMapAndInput(t, element, state);
        expect(ws.task, same(task));
        expect(ws.sheetExtent, extent);
        expect(
          t.widget<TextField>(nowField()).controller!.text,
          '未发送 Agent 中文 draft',
        );
      });
    });
  }

  testWidgets('真实宿主访客右侧只有一次登录且公共地图可见', (t) async {
    await _withHost(t, (h) async {
      h.f.auth.changeIdentity(null, nextOwner: _ownerA);
      await t.pumpAndSettle();
      final before = h.wire.edgeGets;
      await h.openEdge(t);
      expect(find.text('登录，与朋友保持联系'), findsOneWidget);
      expect(find.byKey(const Key('edge-sign-in')), findsOneWidget);
      expect(find.text('读取失败'), findsNothing);
      expect(find.text('重试'), findsNothing);
      expect(find.byKey(const ValueKey('edge-tab-contacts')), findsNothing);
      expect(h.wire.edgeGets, before);
      expect(find.byType(PublicCityMapView), findsOneWidget);
      expect(
        t.getSize(find.byKey(const Key('now-social-edge'))).height,
        lessThan(180),
      );
    });
  });

  testWidgets('真实访客从通知完成登录后回到原通知且不自动提交', (t) async {
    final shared = _SharedAuthFixture(initiallySignedIn: false);
    try {
      await t.runAsync(shared.auth.initialize);
      expect(shared.auth.signedIn, false);
      expect(shared.auth.devPhoneAvailable, true);
      await _withHost(t, (h) async {
        final map = find.byType(PublicCityMapView);
        final originalElement = map.evaluate().single;
        final originalState = t.state(map);
        final before = h.wire.dispatch.length;
        await _section(t, NowSocialSection.notifications);
        expect(
          t.widget<NowSocialEdge>(find.byType(NowSocialEdge)).initialSection,
          NowSocialSection.notifications,
        );
        expect(find.text('登录，与朋友保持联系'), findsOneWidget);
        expect(find.byKey(const Key('edge-sign-in')), findsOneWidget);
        expect(h.wire.edgeGets, 0);
        expect(
          h.wire.dispatch
              .skip(before)
              .where((r) => r['path']!.startsWith('/v1/me/inbox')),
          isEmpty,
        );
        _sameMapAndInput(t, originalElement, originalState);

        await nowTap(t, find.byKey(const Key('edge-sign-in')));
        expect(find.byType(BirdtieLoginSheet), findsOneWidget);
        await nowTap(t, find.text('手机号测试登录'));
        final login = find.byType(DevPhoneLoginSheet);
        expect(login, findsOneWidget);
        await t.enterText(
          find.descendant(of: login, matching: find.byType(TextField)).first,
          '13800138000',
        );
        t.testTextInput.hide();
        await t.pumpAndSettle();
        await nowTap(t, find.text('获取测试验证码'));
        await nowTap(
          t,
          find.descendant(of: login, matching: find.text('登录 Birdtie')),
        );
        expect(shared.auth.signedIn, true);
        expect(shared.auth.accountID, _ownerA);
        expect(find.byType(BirdtieLoginSheet), findsNothing);
        expect(find.byType(DevPhoneLoginSheet), findsNothing);
        _sameMapAndInput(t, originalElement, originalState);
        expect(
          t.widget<NowSocialEdge>(find.byType(NowSocialEdge)).initialSection,
          NowSocialSection.notifications,
        );
        expect(find.text('通知'), findsOneWidget);
        expect(find.text('暂无通知'), findsOneWidget);
        expect(_chatRow(), findsNothing);
        expect(h.wire.conversationGets, 0);
        expect(
          h.wire.dispatch.where((r) => r['path'] == '/v1/me/inbox/page'),
          hasLength(1),
        );
        expect(h.wire.dispatch.where((r) => r['method'] != 'GET'), isEmpty);
        expect(h.f.source.queries, isEmpty);
      }, auth: shared.auth);
    } finally {
      shared.auth.dispose();
    }
  });

  testWidgets('真实宿主不匹配 owner/token 的借入 transport 没有私人 GET', (t) async {
    await _withHost(t, (h) async {
      await h.openEdge(t);
      expect(find.textContaining('当前连接已变化'), findsOneWidget);
      expect(_chatRow(), findsNothing);
      expect(h.wire.edgeGets, 0);
      expect(h.wire.authorGets, 0);
    }, mismatched: true);
  });

  testWidgets('真实宿主消息联系人通知替换同一区域且按需请求', (t) async {
    await _withHost(t, (h) async {
      await h.openEdge(t);
      expect(h.wire.edgeGets, 1);
      expect(_chatRow(), findsOneWidget);
      await _section(t, NowSocialSection.contacts);
      expect(h.wire.edgeGets, 2);
      expect(_chatRow(), findsNothing);
      expect(
        find.byKey(const ValueKey('edge-friend-$_friend')),
        findsOneWidget,
      );
      await nowTap(t, find.byKey(const ValueKey('edge-filter-follows')));
      expect(h.wire.edgeGets, 3);
      expect(find.byKey(const ValueKey('edge-friend-$_friend')), findsNothing);
      expect(
        find.byKey(const ValueKey('edge-follow-PERSON-$_friend')),
        findsOneWidget,
      );
      await _section(t, NowSocialSection.notifications);
      expect(
        find.byKey(const ValueKey('edge-follow-PERSON-$_friend')),
        findsNothing,
      );
      expect(find.text('通知'), findsOneWidget);
      expect(h.wire.edgeGets, 3);
      expect(
        h.wire.dispatch.where((r) => r['path'] == '/v1/me/inbox/page'),
        hasLength(1),
      );
      expect(find.byType(SingleChildScrollView), findsNothing);
      expect(find.byType(TextField), findsOneWidget);
      await _section(t, NowSocialSection.messages);
      expect(h.wire.conversationGets, 2);
      expect(_chatRow(), findsOneWidget);
    });
  });

  for (final kind in ['PLACE', 'MOMENT']) {
    for (final section in [
      NowSocialSection.messages,
      NowSocialSection.notifications,
    ]) {
      testWidgets('排队公开预览$kind不覆盖较新${section.name}入口', (t) async {
        await _withHost(t, (h) async {
          h.wire.publicMoment = kind == 'MOMENT';
          await h.primeMap(t);
          final originalElement = find
              .byType(PublicCityMapView)
              .evaluate()
              .single;
          final originalState = t.state(find.byType(PublicCityMapView));
          const draft = '合成 Agent 草稿保留';
          await t.enterText(nowField(), draft);
          await t.pumpAndSettle();
          final workspace = h.workspace(t);
          final selected = kind == 'PLACE'
              ? 'place:$_place'
              : 'moment:$_moment';
          expect(h.canvas(t).layers!.find(selected)?.kind, kind);

          // The real host listener queues its public preview postframe. Do not
          // pump between selecting the map item and the newer explicit tap.
          workspace.selectEntity(selected, preserveViewport: true);
          expect(find.byType(NowPlacePreview), findsNothing);
          expect(find.byType(NowPublicPlacePanel), findsNothing);
          await t.tap(
            find.byKey(
              Key(
                section == NowSocialSection.notifications
                    ? 'now-notifications-button'
                    : 'now-social-button',
              ),
            ),
          );
          await t.pumpAndSettle();
          expect(
            find.byType(NowSocialEdge),
            findsOneWidget,
            reason:
                'Older queued preview must not replace the newer social tap; '
                'public preview count=${find.byType(NowPlacePreview).evaluate().length}',
          );
          expect(find.byType(NowPlacePreview), findsNothing);
          expect(find.byType(NowPublicPlacePanel), findsNothing);
          expect(
            t.widget<NowSocialEdge>(find.byType(NowSocialEdge)).initialSection,
            section,
          );
          expect(workspace.selectedEntityId, selected);
          expect(t.widget<TextField>(nowField()).controller!.text, draft);
          expect(
            t.widget<AgentComposer>(find.byType(AgentComposer)).humanMode,
            false,
          );
          expect(_hasMessageWrite(h.wire), false);
          expect(h.f.source.queries, isEmpty);
          _sameMapAndInput(t, originalElement, originalState);

          // A later explicit map pick is still allowed to open that same item.
          final currentMap = h.map(t);
          currentMap.onEntitySelected!.call(
            currentMap.entities.singleWhere((entity) => entity.id == selected),
          );
          await t.pumpAndSettle();
          expect(
            find.byType(NowPlacePreview),
            kind == 'PLACE' ? findsOneWidget : findsNothing,
          );
          expect(
            find.byType(NowPublicPlacePanel),
            kind == 'MOMENT' ? findsOneWidget : findsNothing,
          );
          if (kind == 'MOMENT') {
            expect(
              t
                  .widget<PublicMomentPage>(find.byType(PublicMomentPage))
                  .momentID,
              _moment,
            );
          }
          expect(find.byType(NowSocialEdge), findsNothing);
          expect(workspace.selectedEntityId, selected);
          expect(t.widget<TextField>(nowField()).controller!.text, draft);
          _sameMapAndInput(t, originalElement, originalState);
        });
      });
    }
  }

  for (final kind in ['PLACE', 'MOMENT']) {
    for (final section in [
      NowSocialSection.messages,
      NowSocialSection.notifications,
    ]) {
      testWidgets('晚到相同地图读取$kind不覆盖已打开${section.name}入口', (t) async {
        await _withHost(t, (h) async {
          h.wire.publicMoment = kind == 'MOMENT';
          await h.primeMap(t);
          final originalElement = find
              .byType(PublicCityMapView)
              .evaluate()
              .single;
          final originalState = t.state(find.byType(PublicCityMapView));
          final workspace = h.workspace(t);
          final selected = kind == 'PLACE'
              ? 'place:$_place'
              : 'moment:$_moment';
          workspace.selectEntity(selected, preserveViewport: true);
          await t.pumpAndSettle();
          expect(
            find.byType(NowPlacePreview),
            kind == 'PLACE' ? findsOneWidget : findsNothing,
          );
          expect(
            find.byType(NowPublicPlacePanel),
            kind == 'MOMENT' ? findsOneWidget : findsNothing,
          );
          if (kind == 'MOMENT') {
            expect(
              t
                  .widget<PublicMomentPage>(find.byType(PublicMomentPage))
                  .momentID,
              _moment,
            );
          }

          final layers = h.canvas(t).layers!;
          final previousPublicRead = layers.publicView;
          final late = Completer<http.Response>();
          h.wire.pendingPublicMap = late;
          final refresh = layers.load('alpha', _bounds);
          await t.pump();
          await t.tap(
            find.byKey(
              Key(
                section == NowSocialSection.notifications
                    ? 'now-notifications-button'
                    : 'now-social-button',
              ),
            ),
          );
          await t.pump();
          await t.pump();
          expect(find.byType(NowSocialEdge), findsOneWidget);
          expect(find.byType(NowPlacePreview), findsNothing);
          expect(find.byType(NowPublicPlacePanel), findsNothing);
          expect(workspace.selectedEntityId, selected);

          // Complete the actual map source after the newer explicit social tap.
          // Also deliver a real workspace history notification, with the map
          // selection unchanged. No host-private state or callbacks are used.
          late.complete(_response(h.wire._map(null, false)));
          await refresh;
          await workspace.loadRecent();
          await t.pumpAndSettle();
          expect(layers.publicView, isNot(same(previousPublicRead)));
          expect(find.byType(NowSocialEdge), findsOneWidget);
          expect(find.byType(NowPlacePreview), findsNothing);
          expect(find.byType(NowPublicPlacePanel), findsNothing);
          expect(workspace.selectedEntityId, selected);
          expect(
            t.widget<NowSocialEdge>(find.byType(NowSocialEdge)).initialSection,
            section,
          );
          _sameMapAndInput(t, originalElement, originalState);

          final currentMap = h.map(t);
          currentMap.onEntitySelected!.call(
            currentMap.entities.singleWhere((entity) => entity.id == selected),
          );
          await t.pumpAndSettle();
          expect(
            find.byType(NowPlacePreview),
            kind == 'PLACE' ? findsOneWidget : findsNothing,
          );
          expect(
            find.byType(NowPublicPlacePanel),
            kind == 'MOMENT' ? findsOneWidget : findsNothing,
          );
          if (kind == 'MOMENT') {
            expect(
              t
                  .widget<PublicMomentPage>(find.byType(PublicMomentPage))
                  .momentID,
              _moment,
            );
          }
          expect(find.byType(NowSocialEdge), findsNothing);
          expect(workspace.selectedEntityId, selected);
          expect(_hasMessageWrite(h.wire), false);
          expect(h.f.source.queries, isEmpty);
          _sameMapAndInput(t, originalElement, originalState);
        });
      });
    }
  }

  for (final kind in ['PLACE', 'MOMENT']) {
    testWidgets('录音中的父地图指针与$kind选择不得隐藏当前真人对话', (t) async {
      const channel = MethodChannel('birdtie/friend_audio');
      final audioCalls = <String>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            audioCalls.add(call.method);
            return null;
          });
      addTearDown(
        () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(channel, null),
      );
      await _withHost(t, (h) async {
        h.wire.publicMoment = kind == 'MOMENT';
        await h.primeMap(t);
        final originalElement = find
            .byType(PublicCityMapView)
            .evaluate()
            .single;
        final originalState = t.state(find.byType(PublicCityMapView));
        await t.enterText(nowField(), '原 Agent 草稿保留');
        await t.pumpAndSettle();
        await h.openEdge(t);
        await nowTap(t, _chatRow());
        await t.enterText(nowField(), '此朋友的未发送文字');
        await t.pumpAndSettle();
        final before = t.widget<AgentComposer>(find.byType(AgentComposer));
        final friend = before.friendConversation!;
        expect(before.humanMode, true);
        expect(friend.canRecord, true);
        final recipient = before.recipientName;
        await t.tap(find.byKey(const Key('now-composer-microphone')));
        await t.pump();
        await t.pump();
        expect(friend.recording, true);
        expect(audioCalls.where((method) => method == 'start'), hasLength(1));

        // Deliver a real pointer through the map's parent Listener, outside
        // the social region. Its recording guard must refuse closing first.
        await t.tapAt(
          Offset(
            24,
            t.getRect(find.byKey(const Key('now-primary-surface'))).top - 4,
          ),
        );
        await t.pump();
        expect(find.text('请先停止或取消录音，录音和草稿会保留。'), findsOneWidget);
        expect(friend.recording, true);
        expect(find.byType(FriendConversationThread), findsOneWidget);

        // A native map may deliver its visible feature selection after that
        // pointer. Use the current public DTO and actual MapCanvas callback.
        final currentMap = h.map(t);
        final selected = kind == 'PLACE' ? 'place:$_place' : 'moment:$_moment';
        currentMap.onEntitySelected!.call(
          currentMap.entities.singleWhere((entity) => entity.id == selected),
        );
        await t.pump();
        await t.pump();
        final after = t.widget<AgentComposer>(find.byType(AgentComposer));
        expect(friend.recording, true);
        expect(after.humanMode, true);
        expect(after.friendConversation, same(friend));
        expect(after.recipientName, recipient);
        expect(t.widget<TextField>(nowField()).controller!.text, '此朋友的未发送文字');
        expect(_hasMessageWrite(h.wire), false);
        expect(h.f.source.queries, isEmpty);
        _sameMapAndInput(t, originalElement, originalState);
        expect(
          find.byType(NowPlacePreview),
          findsNothing,
          reason:
              'Parent map close was refused while recording; visible thread='
              '${find.byType(FriendConversationThread).evaluate().length}, '
              'social=${find.byType(NowSocialEdge).evaluate().length}, '
              'recording=${friend.recording}, recipientRetained='
              '${after.recipientName == recipient}',
        );
        expect(find.byType(FriendConversationThread), findsOneWidget);
        expect(
          find.byKey(const Key('now-human-conversation-panel')),
          findsOneWidget,
        );
      });
    });
  }

  testWidgets('真实分页历史仅向当前真人会话确认已读，通知单独读取', (t) async {
    // The widget binding resets lifecycle to null between individual tests.
    // Model an actual foreground view; production must keep this read guard.
    t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await _withHost(t, (h) async {
      h.wire.includeHistory = true;
      await h.openEdge(t);
      final initial = t.widget<TopControls>(find.byType(TopControls));
      expect(initial.hasUnreadMessages, true);
      expect(initial.hasUnreadNotifications, false);
      expect(
        h.wire.dispatch.where((r) => r['path'] == '/v1/me/inbox/page'),
        isEmpty,
      );
      await nowTap(t, _chatRow());
      expect(find.text('合成真人历史消息'), findsOneWidget);
      final thread = t.widget<FriendConversationThread>(
        find.byType(FriendConversationThread),
      );
      expect(thread.controller.historyAvailable, true);
      expect(thread.controller.isVisible?.call(), true);
      expect(thread.controller.conversation.id, _chat);
      expect(thread.controller.messages.single.senderAccountId, _friend);
      expect(thread.controller.messages.single.createdAt, h.wire.observedAt);
      expect(thread.controller.error, isNull);
      expect(
        h.wire.dispatch.where(
          (r) =>
              r['method'] == 'POST' &&
              r['path'] == '/v1/me/conversations/$_chat/read',
        ),
        hasLength(1),
      );
      expect(_hasMessageWrite(h.wire), false);
      await _section(t, NowSocialSection.notifications);
      expect(find.text('合成真人历史消息'), findsNothing);
      expect(
        h.wire.dispatch.where((r) => r['path'] == '/v1/me/inbox/page'),
        hasLength(1),
      );
      expect(
        h.wire.dispatch.where(
          (r) => r['method'] != 'GET' && r['path']!.startsWith('/v1/me/inbox'),
        ),
        isEmpty,
      );
      expect(h.wire.messageGets, 1);
      await _section(t, NowSocialSection.messages);
      final refreshed = t.widget<TopControls>(find.byType(TopControls));
      expect(refreshed.hasUnreadMessages, false);
      expect(refreshed.hasUnreadNotifications, false);
      expect(h.wire.conversationGets, 2);
    });
  });

  testWidgets('真实宿主 provider 替换只重读当前联系人，迟到 A 不替换 B', (t) async {
    await _withHost(t, (h) async {
      await h.openEdge(t);
      final late = Completer<http.Response>();
      h.wire.pendingTies = late;
      await _section(t, NowSocialSection.contacts);
      expect(h.wire.edgeGets, 2);
      h.replaceProvider();
      await t.pumpAndSettle();
      final b = h.extraWires.single;
      expect(b.edgeGets, 1);
      expect(find.text('乙合成好友'), findsOneWidget);
      final action = h.friendAction(t);
      late.complete(h.wire.ties());
      await t.pumpAndSettle();
      expect(find.text('甲合成好友'), findsNothing);
      expect(find.text('乙合成好友'), findsOneWidget);
      expect(b.edgeGets, 1);
      action();
      await t.pumpAndSettle();
      expect(b.chatOpens, 1);
      expect(h.wire.chatOpens, 0);
      expect(find.byType(FriendConversationThread), findsOneWidget);
      expect(_hasMessageWrite(b), false);
    });
  });

  testWidgets('真实宿主 provider A-B-A 不复活旧消息 row callback', (t) async {
    await _withHost(t, (h) async {
      await h.openEdge(t);
      final old = _chatAction(t), oldCount = h.wire.conversationGets;
      final map = find.byType(PublicCityMapView);
      final element = map.evaluate().single, state = t.state(map);
      h.replaceProvider();
      await t.pumpAndSettle();
      final b = h.extraWires.single, sourceB = h.provider.value;
      expect(find.text('乙合成私信对象'), findsOneWidget);
      final fromB = _chatAction(t);
      h.provider.value = h.connections;
      await t.pumpAndSettle();
      h.provider.value = sourceB;
      await t.pumpAndSettle();
      final beforeA = h.wire.dispatch.length, beforeB = b.dispatch.length;
      old();
      fromB();
      await t.pumpAndSettle();
      expect(h.wire.dispatch.length, beforeA);
      expect(b.dispatch.length, beforeB);
      expect(h.wire.conversationGets, oldCount + 1);
      expect(b.conversationGets, 2);
      expect(find.byType(FriendConversationThread), findsNothing);
      _sameMapAndInput(t, element, state);
    });
  });

  testWidgets('真实宿主组织加载与字体主题变化保留当前联系人并无重复 GET', (t) async {
    await _withHost(t, (h) async {
      final organizations = await h.organizations(t);
      await h.openEdge(t);
      await _section(t, NowSocialSection.contacts);
      expect(h.wire.edgeGets, 2);
      final late = Completer<http.Response>();
      h.wire.pendingOrganizations = late;
      final before = h.wire.organizationGets;
      final loading = organizations.load();
      await t.pump();
      expect(h.wire.organizationGets, before + 1);
      expect(h.wire.edgeGets, 2);
      late.complete(_response([]));
      await loading;
      h.f.auth.updateProfileDisplayName('合成新显示名');
      h.presentation.value = (2, Brightness.dark);
      t.view.physicalSize = const Size(320, 844);
      await t.pumpAndSettle();
      expect(h.wire.edgeGets, 2);
      expect(find.text('联系人'), findsOneWidget);
      expect(t.getSize(find.byKey(const Key('now-social-edge'))).width, 272);
      expect(find.byType(TextField), findsOneWidget);
    });
  });

  for (final kind in ['organization', 'account']) {
    testWidgets('真实宿主 $kind A-B-A 永久退休旧好友操作', (t) async {
      await _withHost(t, (h) async {
        final organizations = await h.organizations(t);
        await h.openEdge(t);
        await _section(t, NowSocialSection.contacts);
        final old = h.friendAction(t);
        if (kind == 'organization') {
          organizations.select(
            const OrganizationWorkspace(
              id: _organization,
              name: '合成组织',
              role: 'owner',
              organizationType: 'community',
            ),
          );
          organizations.select(null);
        } else {
          h.f.auth.changeIdentity(_tokenB, nextOwner: _ownerB);
          h.f.auth.changeIdentity(_tokenA, nextOwner: _ownerA);
        }
        await t.pumpAndSettle();
        final before = h.wire.dispatch.length;
        old();
        await t.pumpAndSettle();
        expect(h.wire.dispatch.length, before);
        expect(h.wire.chatOpens, 0);
        expect(find.byType(FriendConversationThread), findsNothing);
      });
    });
  }

  testWidgets('真实宿主 pending 好友会话 A-B-A 后不能采用迟到旧回执', (t) async {
    await _withHost(t, (h) async {
      await h.openEdge(t);
      await _section(t, NowSocialSection.contacts);
      final late = Completer<http.Response>();
      h.wire.pendingChat = late;
      await nowTap(t, find.byKey(const ValueKey('edge-friend-$_friend')));
      expect(h.wire.chatOpens, 1);
      h.f.auth.changeIdentity(_tokenB, nextOwner: _ownerB);
      h.f.auth.changeIdentity(_tokenA, nextOwner: _ownerA);
      await t.pumpAndSettle();
      late.complete(h.wire.conversation());
      await t.pumpAndSettle();
      expect(find.byType(FriendConversationThread), findsNothing);
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).humanMode,
        false,
      );
      expect(_hasMessageWrite(h.wire), false);
    });
  });

  testWidgets('真实宿主消息使用 Now 唯一输入并按收件人保留草稿', (t) async {
    await _withHost(t, (h) async {
      final map = find.byType(PublicCityMapView);
      final element = map.evaluate().single, state = t.state(map);
      await t.enterText(nowField(), '原 Agent 草稿');
      await t.pumpAndSettle();
      await h.openEdge(t);
      await nowTap(t, _chatRow());
      expect(find.byType(FriendConversationThread), findsOneWidget);
      expect(find.byType(TextField), findsOneWidget);
      final composer = t.widget<AgentComposer>(find.byType(AgentComposer));
      expect(composer.humanMode, true);
      expect(composer.recipientName, '甲合成私信对象');
      expect(composer.friendConversation!.conversation.id, _chat);
      expect(h.wire.messageGets, 1);
      expect(_hasMessageWrite(h.wire), false);
      expect(t.widget<TextField>(nowField()).controller!.text, '');
      await t.enterText(nowField(), '朋友未发送草稿');
      await t.pumpAndSettle();
      await _section(t, NowSocialSection.contacts);
      expect(find.byType(FriendConversationThread), findsNothing);
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).humanMode,
        false,
      );
      expect(t.widget<TextField>(nowField()).controller!.text, '原 Agent 草稿');
      await _section(t, NowSocialSection.messages);
      await nowTap(t, _chatRow());
      expect(t.widget<TextField>(nowField()).controller!.text, '朋友未发送草稿');
      await h.closeEdge(t);
      expect(t.widget<TextField>(nowField()).controller!.text, '原 Agent 草稿');
      expect(_hasMessageWrite(h.wire), false);
      _sameMapAndInput(t, element, state);
    });
  });

  for (final owner in [_ownerA, _ownerB]) {
    testWidgets(
      '真实宿主联系人关注跨键盘卸载及登录恢复 ${owner == _ownerA ? '同账号保留' : '另一账号重置'}',
      (t) async {
        final session = _SharedAuthFixture();
        addTearDown(t.view.resetViewInsets);
        try {
          await session.initialize(t);
          await _withHost(t, (h) async {
            h.wire.otherOwnerLabel = '乙';
            final map = find.byType(PublicCityMapView);
            final element = map.evaluate().single, state = t.state(map);
            await h.openEdge(t);
            await _section(t, NowSocialSection.contacts);
            await nowTap(t, find.byKey(const ValueKey('edge-filter-follows')));
            expect(find.text('甲合成关注'), findsOneWidget);
            final beforeKeyboard = t.state(find.byType(NowSocialEdge));
            await t.enterText(nowField(), '本人的未发送 Agent 草稿');
            t.view.viewInsets = const FakeViewPadding(bottom: 680);
            await t.pumpAndSettle();
            expect(find.byType(NowSocialEdge), findsNothing);
            expect(beforeKeyboard.mounted, false);
            expect(find.byType(TopControls), findsNothing);
            t
                .state<AgentComposerState>(find.byType(AgentComposer))
                .pauseEditing();
            t.view.viewInsets = const FakeViewPadding();
            await t.pumpAndSettle();
            expect(
              t.state(find.byType(NowSocialEdge)),
              isNot(same(beforeKeyboard)),
            );
            expect(find.text('联系人'), findsOneWidget);
            expect(find.text('甲合成关注'), findsOneWidget);
            expect(
              h.wire.dispatch.lastWhere(
                (r) => _edgePaths.contains(r['path']),
              )['path'],
              '/v1/me/follows/page',
            );
            final oldFollow = t
                .widget<ListTile>(
                  find.byKey(const ValueKey('edge-follow-PERSON-$_friend')),
                )
                .onTap!;
            await t.enterText(nowField(), '本人的未发送 Agent 草稿');
            t.view.viewInsets = const FakeViewPadding(bottom: 680);
            await t.pumpAndSettle();
            expect(find.byType(NowSocialEdge), findsNothing);
            final readsBeforeExpiry = h.wire.edgeGets;
            expect(await session.auth.invalidateRejectedSession(_tokenA), true);
            await t.pumpAndSettle();
            expect(find.byType(BirdtieLoginSheet), findsOneWidget);
            expect(find.byType(NowSocialEdge), findsNothing);
            expect(find.text('甲合成关注'), findsNothing);
            expect(h.wire.edgeGets, readsBeforeExpiry);
            session.nextOwner = owner;
            session.nextToken = owner == _ownerA
                ? 'synthetic-now-host-a-restored'
                : 'synthetic-now-host-b';
            await nowTap(t, find.text('手机号测试登录'));
            expect(find.byType(DevPhoneLoginSheet), findsOneWidget);
            final phone = find.descendant(
              of: find.byType(DevPhoneLoginSheet),
              matching: find.byType(TextField),
            );
            await t.ensureVisible(phone);
            await t.enterText(phone, '13800138000');
            // The extreme short editor above proves chrome was unmounted.
            // Return to a normal phone IME height for its submit controls.
            t.view.viewInsets = const FakeViewPadding(bottom: 300);
            await t.pumpAndSettle();
            await nowTap(t, find.text('获取测试验证码'));
            expect(h.wire.edgeGets, readsBeforeExpiry);
            expect(find.byType(NowSocialEdge), findsNothing);
            await nowTap(t, find.text('登录 Birdtie'));
            expect(session.auth.accountID, owner);
            expect(find.byType(DevPhoneLoginSheet), findsNothing);
            expect(find.byType(BirdtieLoginSheet), findsNothing);
            t.view.viewInsets = const FakeViewPadding();
            await t.pumpAndSettle();
            expect(find.byType(NowSocialEdge), findsOneWidget);
            final beforeStaleCallback = h.wire.dispatch.length;
            oldFollow();
            await t.pumpAndSettle();
            expect(h.wire.dispatch.length, beforeStaleCallback);
            if (owner == _ownerA) {
              expect(find.text('联系人'), findsOneWidget);
              expect(find.text('甲合成关注'), findsOneWidget);
              expect(
                t.widget<TextField>(nowField()).controller!.text,
                '本人的未发送 Agent 草稿',
              );
              expect(
                h.wire.dispatch.lastWhere(
                  (r) => _edgePaths.contains(r['path']),
                )['path'],
                '/v1/me/follows/page',
              );
            } else {
              expect(find.text('联系'), findsNothing);
              expect(find.text('乙合成私信对象'), findsOneWidget);
              expect(find.text('甲合成私信对象'), findsNothing);
              expect(find.text('甲合成关注'), findsNothing);
              expect(t.widget<TextField>(nowField()).controller!.text, isEmpty);
              await _section(t, NowSocialSection.contacts);
              expect(find.text('乙合成好友'), findsOneWidget);
              expect(find.text('甲合成好友'), findsNothing);
              expect(find.text('乙合成关注'), findsNothing);
              expect(
                h.wire.dispatch.lastWhere(
                  (r) => _edgePaths.contains(r['path']),
                )['path'],
                '/v1/me/ties/page',
              );
            }
            expect(h.f.source.queries, isEmpty);
            expect(h.wire.dispatch.where((r) => r['method'] != 'GET'), isEmpty);
            _sameMapAndInput(t, element, state);
          }, auth: session.auth);
        } finally {
          session.auth.dispose();
        }
      },
    );
  }

  for (final recovery in [
    'same-owner',
    'logout',
    'logout-expired',
    'other-owner',
  ]) {
    testWidgets('共享身份失效恢复 $recovery 保留或清除对应草稿且零自动发送', (t) async {
      final session = _SharedAuthFixture();
      try {
        await session.initialize(t);
        final png = await t.runAsync(syntheticPrivatePNG);
        await _withHost(
          t,
          (h) async {
            final map = find.byType(PublicCityMapView);
            final element = map.evaluate().single, state = t.state(map);
            await t.enterText(nowField(), '原 Agent 的未发送文字');
            await t.pumpAndSettle();
            final image = t
                .widget<AgentComposer>(find.byType(AgentComposer))
                .imageDraft!;
            await t.runAsync(image.select);
            image.setReviewed(true);
            await t.pumpAndSettle();
            expect(image.localBytes, isNotNull);
            final originalImage = image.localBytes!.toList();
            final imageApproval = image.snapshot()!;
            await h.openEdge(t);
            await nowTap(t, _chatRow());
            await t.enterText(nowField(), '给此人的未发送文字');
            final oldConversation = t
                .widget<AgentComposer>(find.byType(AgentComposer))
                .friendConversation!;
            final audio = audioFixture();
            expect(
              oldConversation.restoreAudioDraft(
                audio,
                owner: _ownerA,
                conversationID: _chat,
              ),
              true,
            );
            await t.pumpAndSettle();
            expect(
              image.localBytes,
              originalImage,
              reason: '切到朋友时 Agent 本地材料仍属于原草稿',
            );
            final beforeQueries = h.f.source.queries.length;
            if (recovery == 'logout') {
              await session.auth.signOut();
            } else {
              // An independent private feature already invalidated the shared
              // auth controller. Now's own 401 handler is never called here.
              expect(
                await session.auth.invalidateRejectedSession(_tokenA),
                true,
              );
              expect(session.auth.rejectedSessionOwnerID, _ownerA);
              if (recovery == 'logout-expired') {
                await session.auth.signOut();
                expect(session.auth.rejectedSessionOwnerID, isNull);
              }
            }
            await t.pumpAndSettle();
            expect(oldConversation.current, false);
            expect(oldConversation.audioDraft, isNull);
            expect(imageApproval.isCurrent, false);
            expect(find.byType(FriendConversationThread), findsNothing);
            expect(find.text('甲合成私信对象'), findsNothing);
            expect(t.widget<TextField>(nowField()).controller!.text, isEmpty);
            expect(_hasMessageWrite(h.wire), false);
            final owner = recovery == 'other-owner' ? _ownerB : _ownerA;
            await session.login(t, owner: owner);
            expect(session.auth.accountID, owner);
            if (recovery == 'same-owner') {
              final composer = t.widget<AgentComposer>(
                find.byType(AgentComposer),
              );
              expect(composer.humanMode, true);
              expect(composer.recipientName, '甲合成私信对象');
              expect(
                t.widget<TextField>(nowField()).controller!.text,
                '给此人的未发送文字',
              );
              expect(
                composer.friendConversation!.audioDraft!.bytes,
                audio.bytes,
              );
              expect(
                composer.friendConversation!.audioDraft!.metadata.same(
                  audio.metadata,
                ),
                true,
              );
              await _section(t, NowSocialSection.contacts);
              expect(
                t.widget<TextField>(nowField()).controller!.text,
                '原 Agent 的未发送文字',
              );
              final restored = t
                  .widget<AgentComposer>(find.byType(AgentComposer))
                  .imageDraft!;
              expect(restored.localBytes, originalImage);
              expect(restored.reviewed, false);
              expect(restored.canUse, false);
            } else {
              final composer = t.widget<AgentComposer>(
                find.byType(AgentComposer),
              );
              expect(composer.humanMode, false);
              expect(t.widget<TextField>(nowField()).controller!.text, isEmpty);
              expect(composer.imageDraft?.localBytes, isNull);
              // Ordinary logout does not open a modal which dismisses focus.
              // Leave the synthetic active editor before browsing its list.
              t
                  .state<AgentComposerState>(find.byType(AgentComposer))
                  .pauseEditing();
              await t.pumpAndSettle();
              if (find.byTooltip('联系').evaluate().isNotEmpty) {
                await h.openEdge(t);
              }
              expect(_chatRow(), findsOneWidget);
              await nowTap(t, _chatRow());
              final fresh = t
                  .widget<AgentComposer>(find.byType(AgentComposer))
                  .friendConversation!;
              expect(t.widget<TextField>(nowField()).controller!.text, isEmpty);
              expect(fresh.audioDraft, isNull);
            }
            expect(h.f.source.queries.length, beforeQueries);
            expect(h.wire.dispatch.where((r) => r['method'] != 'GET'), isEmpty);
            _sameMapAndInput(t, element, state);
          },
          auth: session.auth,
          imagePicker: TestMomentImagePicker(png!),
        );
      } finally {
        session.auth.dispose();
      }
    });
  }

  testWidgets('真实宿主聚焦 Now 草稿隐藏联系后首次点击恢复并单次打开原线程', (t) async {
    addTearDown(t.view.resetViewInsets);
    await _withHost(t, (h) async {
      final map = find.byType(PublicCityMapView);
      final element = map.evaluate().single, state = t.state(map);
      final workspace = h.workspace(t);
      const draft = '未发送的 Now 草稿 · original draft';
      await h.openEdge(t);
      expect(t.widget<NowSocialEdge>(find.byType(NowSocialEdge)).open, true);
      expect(_chatRow().hitTestable(), findsOneWidget);
      expect(find.byType(FriendConversationThread), findsNothing);
      expect(h.wire.messageGets, 0);

      // Focus alone hides the already expanded selector even with no IME
      // inset. Its first toolbar tap must show it, rather than close the
      // logical expanded state after pausing the Now editor.
      await nowTap(t, nowField());
      await t.enterText(nowField(), draft);
      t.view.viewInsets = const FakeViewPadding();
      await t.pumpAndSettle();
      expect(workspace.inputFocused, true);
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, true);
      expect(find.byType(NowSocialEdge), findsNothing);
      expect(_chatRow(), findsNothing);
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      _sameMapAndInput(t, element, state);

      await nowTap(t, find.byKey(const Key('now-social-button')));
      expect(find.byType(NowSocialEdge), findsOneWidget);
      expect(_chatRow().hitTestable(), findsOneWidget);
      expect(workspace.inputFocused, false);
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      expect(h.wire.messageGets, 0);

      await nowTap(t, _chatRow());
      final panel = find.byKey(const Key('now-human-conversation-panel'));
      expect(panel, findsOneWidget);
      expect(t.getSize(panel).width, greaterThan(320));
      expect(find.byType(FriendConversationThread), findsOneWidget);
      expect(
        find.descendant(
          of: find.byType(NowSocialEdge),
          matching: find.byType(FriendConversationThread),
        ),
        findsNothing,
      );
      final composer = t.widget<AgentComposer>(find.byType(AgentComposer));
      expect(composer.humanMode, true);
      expect(composer.recipientName, '甲合成私信对象');
      expect(composer.friendConversation!.conversation.id, _chat);
      expect(composer.friendConversation!.conversation.otherAccountId, _friend);
      expect(h.wire.messageGets, 1, reason: '一次原联系人点击只开启一次线程读取');
      expect(h.wire.chatOpens, 0, reason: '已有会话无需另建好友会话');
      expect(_hasMessageWrite(h.wire), false);
      _sameMapAndInput(t, element, state);

      await nowTap(t, find.byKey(const Key('edge-back-messages')));
      expect(find.byType(FriendConversationThread), findsNothing);
      expect(_chatRow().hitTestable(), findsOneWidget);
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).humanMode,
        false,
      );
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      await nowTap(t, find.byKey(const Key('now-social-button')));
      expect(find.byType(NowSocialEdge), findsNothing);
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      expect(h.wire.messageGets, 1);
      expect(_hasMessageWrite(h.wire), false);
      expect(h.f.source.queries, isEmpty);
      _sameMapAndInput(t, element, state);
    });
  });

  testWidgets('真实宿主好友 row 在当前空间打开会话并切换为消息', (t) async {
    await _withHost(t, (h) async {
      await h.openEdge(t);
      await _section(t, NowSocialSection.contacts);
      await nowTap(t, find.byKey(const ValueKey('edge-friend-$_friend')));
      expect(h.wire.chatOpens, 1);
      expect(
        find.byKey(const Key('now-human-conversation-panel')),
        findsOneWidget,
      );
      expect(
        t.getSize(find.byKey(const Key('now-human-conversation-panel'))).width,
        greaterThan(320),
        reason: '真人线程承接到底部宽阅读区',
      );
      expect(
        find.descendant(
          of: find.byType(NowSocialEdge),
          matching: find.byType(FriendConversationThread),
        ),
        findsNothing,
      );
      expect(find.byType(FriendConversationThread), findsOneWidget);
      expect(find.byType(TextField), findsOneWidget);
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).recipientName,
        '甲合成好友',
      );
      expect(h.wire.messageGets, 1);
      expect(_hasMessageWrite(h.wire), false);
      await nowTap(t, find.byKey(const Key('edge-back-messages')));
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).humanMode,
        false,
      );
      expect(find.byType(FriendConversationThread), findsNothing);
    });
  });

  testWidgets('真实宿主联系人打开对话后返回近期消息跨键盘卸载仍保持消息', (t) async {
    addTearDown(t.view.resetViewInsets);
    await _withHost(t, (h) async {
      await h.openEdge(t);
      await _section(t, NowSocialSection.contacts);
      await nowTap(t, find.byKey(const ValueKey('edge-friend-$_friend')));
      expect(
        find.byKey(const Key('now-human-conversation-panel')),
        findsOneWidget,
      );
      expect(find.byType(FriendConversationThread), findsOneWidget);
      await t.enterText(nowField(), '此人的未发送草稿');
      await nowTap(t, find.byKey(const Key('edge-back-messages')));
      expect(find.byType(FriendConversationThread), findsNothing);
      expect(find.text('联系'), findsNothing);
      final beforeKeyboard = t.state(find.byType(NowSocialEdge));
      await t.enterText(nowField(), 'Agent 的另一份未发送草稿');
      t.view.viewInsets = const FakeViewPadding(bottom: 680);
      await t.pumpAndSettle();
      expect(find.byType(NowSocialEdge), findsNothing);
      expect(beforeKeyboard.mounted, false);
      t.state<AgentComposerState>(find.byType(AgentComposer)).pauseEditing();
      t.view.viewInsets = const FakeViewPadding();
      await t.pumpAndSettle();
      expect(t.state(find.byType(NowSocialEdge)), isNot(same(beforeKeyboard)));
      expect(find.text('联系'), findsNothing);
      expect(_chatRow(), findsOneWidget);
      expect(find.byKey(const ValueKey('edge-filter-friends')), findsNothing);
      expect(
        h.wire.dispatch.lastWhere(
          (r) => _edgePaths.contains(r['path']),
        )['path'],
        '/v1/me/conversations/page',
      );
      expect(
        t.widget<TextField>(nowField()).controller!.text,
        'Agent 的另一份未发送草稿',
      );
      await nowTap(t, _chatRow());
      expect(t.widget<TextField>(nowField()).controller!.text, '此人的未发送草稿');
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).recipientName,
        '甲合成私信对象',
      );
      expect(_hasMessageWrite(h.wire), false);
      expect(h.f.source.queries, isEmpty);
    });
  });

  for (final kind in ['recent-message', 'friend']) {
    testWidgets('真实宿主 $kind 关闭社交后迟到成功不得重新打开或改变收件人', (t) async {
      await _withHost(t, (h) async {
        await t.enterText(nowField(), '保持 Agent 原草稿');
        await h.openEdge(t);
        final late = Completer<http.Response>();
        if (kind == 'friend') {
          await _section(t, NowSocialSection.contacts);
          h.wire.pendingChat = late;
          await nowTap(t, find.byKey(const ValueKey('edge-friend-$_friend')));
          expect(h.wire.chatOpens, 1);
        } else {
          h.wire.pendingMessages = late;
          await t.tap(_chatRow());
          await t.pump();
          await t.pump();
        }
        expect(
          find.byType(FriendConversationThread),
          kind == 'friend' ? findsNothing : findsOneWidget,
        );
        await h.closeEdge(t);
        expect(find.byType(NowSocialEdge), findsNothing);
        late.complete(
          kind == 'friend'
              ? h.wire.conversation()
              : h.wire.messages(withItem: true),
        );
        await t.pumpAndSettle();
        expect(find.byType(NowSocialEdge), findsNothing);
        expect(find.byType(FriendConversationThread), findsNothing);
        final composer = t.widget<AgentComposer>(find.byType(AgentComposer));
        expect(composer.humanMode, false);
        expect(composer.recipientName, isNull);
        expect(
          t.widget<TextField>(nowField()).controller!.text,
          '保持 Agent 原草稿',
        );
        expect(h.wire.messageGets, kind == 'friend' ? 0 : 1);
        expect(find.text('合成真人历史消息'), findsNothing);
        expect(_hasMessageWrite(h.wire), false);
        expect(h.f.source.queries, isEmpty);
      });
    });
  }

  testWidgets('真实宿主先选 A 再选 B 时迟到 A 不得覆写 B 收件人与草稿', (t) async {
    await _withHost(t, (h) async {
      h.wire.includeSecondConversation = true;
      await h.openEdge(t);
      final lateA = Completer<http.Response>();
      h.wire.pendingMessages = lateA;
      await t.tap(_chatRow());
      await t.pump();
      await t.pump();
      expect(find.byType(FriendConversationThread), findsOneWidget);
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).recipientName,
        '甲合成私信对象',
      );
      await nowTap(t, find.byKey(const Key('edge-back-messages')));
      h.wire.pendingMessages = null;
      await nowTap(
        t,
        find.byKey(const ValueKey('edge-conversation-$_otherChat')),
      );
      final selectedB = t.widget<AgentComposer>(find.byType(AgentComposer));
      expect(selectedB.friendConversation!.conversation.id, _otherChat);
      expect(selectedB.recipientName, '甲合成另一私信对象');
      await t.enterText(nowField(), '只属于 B 的未发送文字');
      await t.pumpAndSettle();
      lateA.complete(h.wire.messages(withItem: true));
      await t.pumpAndSettle();
      final afterA = t.widget<AgentComposer>(find.byType(AgentComposer));
      expect(afterA.friendConversation, same(selectedB.friendConversation));
      expect(afterA.friendConversation!.conversation.id, _otherChat);
      expect(afterA.recipientName, '甲合成另一私信对象');
      expect(t.widget<TextField>(nowField()).controller!.text, '只属于 B 的未发送文字');
      expect(h.wire.messageGets, 1);
      expect(find.text('合成真人历史消息'), findsNothing);
      expect(
        h.wire.dispatch.where(
          (r) => r['path'] == '/v1/me/conversations/$_otherChat/messages/page',
        ),
        hasLength(1),
      );
      expect(_hasMessageWrite(h.wire), false);
      expect(h.f.source.queries, isEmpty);
    });
  });

  testWidgets('真实宿主待开好友会话切关注筛选后晚回仍保持关注与 Agent 草稿', (t) async {
    await _withHost(t, (h) async {
      await t.enterText(nowField(), 'Agent 原未发送文字');
      await h.openEdge(t);
      await _section(t, NowSocialSection.contacts);
      final late = Completer<http.Response>();
      h.wire.pendingChat = late;
      await nowTap(t, find.byKey(const ValueKey('edge-friend-$_friend')));
      expect(h.wire.chatOpens, 1);
      await nowTap(t, find.byKey(const ValueKey('edge-filter-follows')));
      expect(find.text('联系人'), findsOneWidget);
      expect(find.text('甲合成关注'), findsOneWidget);
      final readsBeforeLate = h.wire.edgeGets;
      late.complete(h.wire.conversation());
      await t.pumpAndSettle();
      expect(find.text('联系人'), findsOneWidget);
      expect(find.text('甲合成关注'), findsOneWidget);
      expect(find.byType(FriendConversationThread), findsNothing);
      final composer = t.widget<AgentComposer>(find.byType(AgentComposer));
      expect(composer.humanMode, false);
      expect(composer.recipientName, isNull);
      expect(t.widget<TextField>(nowField()).controller!.text, 'Agent 原未发送文字');
      expect(h.wire.edgeGets, readsBeforeLate);
      expect(h.wire.messageGets, 0);
      expect(_hasMessageWrite(h.wire), false);
      expect(h.f.source.queries, isEmpty);
    });
  });

  testWidgets('真实宿主 provider ABA 退休打开线程和旧发送读取回调', (t) async {
    await _withHost(t, (h) async {
      await h.openEdge(t);
      h.replaceProvider();
      await t.pumpAndSettle();
      final b = h.extraWires.single, sourceB = h.provider.value;
      await nowTap(t, _chatRow());
      final old = t
          .widget<AgentComposer>(find.byType(AgentComposer))
          .friendConversation!;
      await t.enterText(nowField(), '未发送朋友草稿');
      await t.pumpAndSettle();
      h.provider.value = h.connections;
      await t.pumpAndSettle();
      h.provider.value = sourceB;
      await t.pumpAndSettle();
      expect(old.current, false);
      expect(find.byType(FriendConversationThread), findsNothing);
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).humanMode,
        false,
      );
      final beforeA = h.wire.dispatch.length, beforeB = b.dispatch.length;
      await old.load();
      expect(await old.sendText('旧草稿不可发送'), false);
      await t.pumpAndSettle();
      expect(h.wire.dispatch.length, beforeA);
      expect(b.dispatch.length, beforeB);
      expect(_hasMessageWrite(b), false);
    });
  });

  testWidgets('真实宿主消息请求可达且没有第二编辑器和收件箱页面', (t) async {
    await _withHost(t, (h) async {
      await h.openEdge(t);
      expect(find.byKey(const Key('edge-message-requests')), findsNothing);
      await nowTap(t, find.byKey(const Key('edge-all-conversations')));
      await nowTap(t, find.byKey(const Key('edge-message-requests')));
      expect(_chatRow(), findsNothing);
      expect(find.byKey(const Key('edge-back-messages')), findsOneWidget);
      expect(
        h.wire.dispatch.where(
          (r) => r['path'] == '/v1/me/connection-requests/page',
        ),
        hasLength(1),
      );
      expect(find.byType(TextField), findsOneWidget);
      expect(find.text('收件箱'), findsNothing);
      await nowTap(t, find.byKey(const Key('edge-back-messages')));
      expect(_chatRow(), findsOneWidget);
      expect(_hasMessageWrite(h.wire), false);
    });
  });

  testWidgets('真实宿主私人读取异常与单项重试保持地图及 Agent 草稿', (t) async {
    await _withHost(t, (h) async {
      h.wire.conversationStatus = 503;
      await t.enterText(nowField(), '网络异常期间 Agent 草稿');
      await t.pumpAndSettle();
      await h.openEdge(t);
      expect(find.text('服务暂不可用，请稍后重试。'), findsOneWidget);
      expect(find.text('还没有消息'), findsNothing);
      expect(find.text('登录'), findsNothing);
      h.wire.conversationStatus = 200;
      await nowTap(t, find.byKey(const Key('edge-retry')));
      expect(h.wire.edgeGets, 2);
      expect(_chatRow(), findsOneWidget);
      expect(
        t.widget<TextField>(nowField()).controller!.text,
        '网络异常期间 Agent 草稿',
      );
      expect(find.byType(PublicCityMapView), findsOneWidget);
    });
  });

  // AUDIO-CANCEL-REPLAY-CANDIDATES01: append-only public-seam regressions.
  for (final kind in ['PLACE', 'MOMENT']) {
    testWidgets('取消录音后普通刷新不得重放被阻挡的$kind预览且新地图选择仍可达', (t) async {
      const channel = MethodChannel('birdtie/friend_audio');
      final audioCalls = <String>[];
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            audioCalls.add(call.method);
            return null;
          });
      addTearDown(
        () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(channel, null),
      );
      await _withHost(t, (h) async {
        h.wire.publicMoment = kind == 'MOMENT';
        await h.primeMap(t);
        final originalElement = find
            .byType(PublicCityMapView)
            .evaluate()
            .single;
        final originalState = t.state(find.byType(PublicCityMapView));
        await t.enterText(nowField(), '取消录音后仍保留 Agent 草稿');
        await t.pumpAndSettle();
        await h.openEdge(t);
        await nowTap(t, _chatRow());
        await t.enterText(nowField(), '取消录音后仍属于此朋友的文字');
        await t.pumpAndSettle();
        final before = t.widget<AgentComposer>(find.byType(AgentComposer));
        final friend = before.friendConversation!;
        final recipient = before.recipientName;
        expect(before.humanMode, true);
        expect(friend.canRecord, true);
        await t.tap(find.byKey(const Key('now-composer-microphone')));
        await t.pump();
        await t.pump();
        expect(friend.recording, true);
        expect(audioCalls.where((method) => method == 'start'), hasLength(1));

        // First map intent is refused during active recording. Deliver both
        // the actual parent pointer and its current visible native DTO.
        await t.tapAt(
          Offset(
            24,
            t.getRect(find.byKey(const Key('now-primary-surface'))).top - 4,
          ),
        );
        await t.pump();
        expect(find.text('请先停止或取消录音，录音和草稿会保留。'), findsOneWidget);
        final selected = kind == 'PLACE' ? 'place:$_place' : 'moment:$_moment';
        final blockedMap = h.map(t);
        blockedMap.onEntitySelected!.call(
          blockedMap.entities.singleWhere((entity) => entity.id == selected),
        );
        await t.pump();
        await t.pump();
        expect(friend.recording, true);
        expect(h.workspace(t).selectedEntityId, selected);
        expect(find.byType(NowPlacePreview), findsNothing);
        expect(find.byType(NowPublicPlacePanel), findsNothing);
        expect(find.byType(FriendConversationThread), findsOneWidget);

        // Cancel through the existing public Friend controller, observing its
        // native MethodChannel acknowledgement. This is not a map pick or send.
        friend.cancelAudio();
        await t.pump();
        await t.pump();
        expect(audioCalls.where((method) => method == 'cancel'), hasLength(1));
        expect(audioCalls.where((method) => method == 'stop'), isEmpty);
        expect(friend.audioActive, false);
        expect(friend.audioDraft, isNull);
        expect(find.byType(FriendConversationThread), findsOneWidget);

        // Ordinary public history refresh notifies the same workspace without
        // changing its selected entity. It must not replay the refused intent.
        await h.workspace(t).loadRecent();
        await t.pumpAndSettle();
        final afterRefresh = t.widget<AgentComposer>(
          find.byType(AgentComposer),
        );
        expect(h.workspace(t).selectedEntityId, selected);
        expect(afterRefresh.humanMode, true);
        expect(afterRefresh.friendConversation, same(friend));
        expect(afterRefresh.recipientName, recipient);
        expect(
          t.widget<TextField>(nowField()).controller!.text,
          '取消录音后仍属于此朋友的文字',
        );
        expect(find.byType(NowPlacePreview), findsNothing);
        expect(find.byType(NowPublicPlacePanel), findsNothing);
        expect(
          find.byKey(const Key('now-human-conversation-panel')),
          findsOneWidget,
        );
        expect(find.byType(FriendConversationThread), findsOneWidget);
        expect(_hasMessageWrite(h.wire), false);
        expect(h.f.source.queries, isEmpty);
        _sameMapAndInput(t, originalElement, originalState);

        // A later new map pointer may successfully leave the conversation;
        // its explicit DTO pick must still open this same selected entity.
        await t.tapAt(const Offset(24, 300));
        await t.pump();
        final freshMap = h.map(t);
        freshMap.onEntitySelected!.call(
          freshMap.entities.singleWhere((entity) => entity.id == selected),
        );
        await t.pumpAndSettle();
        final afterPick = t.widget<AgentComposer>(find.byType(AgentComposer));
        expect(
          find.byType(NowPlacePreview),
          kind == 'PLACE' ? findsOneWidget : findsNothing,
        );
        expect(
          find.byType(NowPublicPlacePanel),
          kind == 'MOMENT' ? findsOneWidget : findsNothing,
        );
        if (kind == 'MOMENT') {
          expect(
            t.widget<PublicMomentPage>(find.byType(PublicMomentPage)).momentID,
            _moment,
          );
        }
        expect(find.byType(NowSocialEdge), findsNothing);
        expect(find.byType(FriendConversationThread), findsNothing);
        expect(afterPick.humanMode, false);
        expect(afterPick.recipientName, isNull);
        expect(
          t.widget<TextField>(nowField()).controller!.text,
          '取消录音后仍保留 Agent 草稿',
        );
        expect(h.workspace(t).selectedEntityId, selected);
        expect(_hasMessageWrite(h.wire), false);
        expect(h.f.source.queries, isEmpty);
        _sameMapAndInput(t, originalElement, originalState);
      });
    });
  }
}

// NOTIFICATION-CONTENT-GEOMETRY01: real MapWorkspace -> InboxPanel ->
// RemoteInboxSource pagination, using synthetic HTTP only. No replacement
// Text body or manually sized notification fixture bypasses the production UI.
Map<String, Object?> _geometryNotification(int index) => {
  'id': 'dddddddd-dddd-4ddd-8ddd-${index.toString().padLeft(12, '0')}',
  'category': 'updates',
  'title': '几何通知$index',
  'detail': '本人合成事项',
  'resourceType': 'community',
  'resourceId': _friend,
  'createdAt': '2026-10-12T00:00:00Z',
};

http.Response _geometryNotificationPage(
  http.BaseRequest request, {
  required int previewCount,
  bool hasMore = false,
}) {
  expect(request.method, 'GET');
  expect(request.url.path, '/v1/me/inbox/page');
  final query = request.url.queryParameters;
  expect(query['view'], 'notifications');
  final limit = query['limit'], cursor = query['cursor'];
  if (limit == '3') {
    expect(query, {'view': 'notifications', 'limit': '3'});
    return _response({
      'items': [
        for (var i = 1; i <= previewCount; i++) _geometryNotification(i),
      ],
      'nextCursor': hasMore ? 'YWJj' : null,
      'unreadCount': hasMore ? 23 : previewCount,
    });
  }
  expect(limit, '20');
  if (cursor == null) {
    expect(query, {'view': 'notifications', 'limit': '20'});
    return _response({
      'items': [for (var i = 1; i <= 20; i++) _geometryNotification(i)],
      'nextCursor': 'ZGVm',
      'unreadCount': 23,
    });
  }
  expect(query, {'view': 'notifications', 'limit': '20', 'cursor': 'ZGVm'});
  return _response({
    'items': [for (var i = 21; i <= 23; i++) _geometryNotification(i)],
    'nextCursor': null,
    'unreadCount': 23,
  });
}

Finder _geometryInbox() => find.byType(InboxPanel);
Finder _geometryNotificationScroll() =>
    find.descendant(of: _geometryInbox(), matching: find.byType(Scrollable));

void _expectNoNotificationGeometryWrites(_Host host) {
  expect(
    host.wire.dispatch.where((request) => request['method'] != 'GET'),
    isEmpty,
    reason:
        'Browsing, refreshing and paging notifications cannot ack or submit.',
  );
  expect(host.f.source.queries, isEmpty);
}

void _registerNotificationContentGeometryTests() {
  for (final scale in [1.0, 2.0]) {
    testWidgets(
      'notification geometry real host named pending preview stays compact at $scale text',
      (t) async {
        final semantics = t.ensureSemantics();
        try {
          await _withHost(t, (h) async {
            h.presentation.value = (scale, Brightness.light);
            await t.pumpAndSettle();
            final map = find.byType(PublicCityMapView);
            final element = map.evaluate().single, state = t.state(map);
            final pending = Completer<http.Response>();
            h.wire.pendingNotificationPage = pending;
            await t.tap(find.byKey(const Key('now-notifications-button')));
            await t.pump();
            await t.pump(const Duration(milliseconds: 200));
            expect(_geometryInbox(), findsOneWidget);
            expect(
              find.byWidgetPredicate(
                (widget) =>
                    widget is CircularProgressIndicator &&
                    widget.semanticsLabel == '正在加载通知',
              ),
              findsOneWidget,
            );
            expect(
              t.semantics.simulatedAccessibilityTraversal().any(
                (node) => node.label.contains('正在加载通知'),
              ),
              true,
            );
            final waiting = t.getRect(find.byKey(const Key('now-social-edge')));
            expect(
              waiting.height,
              lessThan(NowUiTokens.socialPanelMaxHeight - 80),
            );
            expect(find.text('暂无通知'), findsNothing);
            expect(
              find.byKey(const Key('notifications-show-all')),
              findsNothing,
            );
            pending.complete(
              _response({
                'items': <Object>[],
                'nextCursor': null,
                'unreadCount': 0,
              }),
            );
            await t.pumpAndSettle();
            expect(find.text('暂无通知').hitTestable(), findsOneWidget);
            expect(find.byType(CircularProgressIndicator), findsNothing);
            final result = t.getRect(find.byKey(const Key('now-social-edge')));
            expect(result.topLeft, waiting.topLeft);
            expect(result.width, waiting.width);
            expect(result.height, lessThan(NowUiTokens.socialPanelMaxHeight));
            _sameMapAndInput(t, element, state);
            _expectNoNotificationGeometryWrites(h);
            expect(
              h.wire.dispatch.where((r) => r['path'] == '/v1/me/inbox/page'),
              hasLength(1),
            );
          });
        } finally {
          semantics.dispose();
        }
      },
    );
  }
  testWidgets(
    'notification geometry real host empty and three-item preview grow with content',
    (t) async {
      await _withHost(t, (h) async {
        var count = 0;
        h.wire.notificationPage = (request) =>
            _geometryNotificationPage(request, previewCount: count);
        final map = find.byType(PublicCityMapView);
        final element = map.evaluate().single, state = t.state(map);
        await t.enterText(nowField(), '通知浏览保留 Agent 草稿');
        await t.pumpAndSettle();
        await _section(t, NowSocialSection.notifications);
        expect(_geometryInbox(), findsOneWidget);
        expect(find.text('暂无通知'), findsOneWidget);
        final empty = t.getRect(find.byKey(const Key('now-social-edge')));
        final refresh = find.descendant(
          of: _geometryInbox(),
          matching: find.widgetWithText(TextButton, '刷新'),
        );
        expect(refresh.hitTestable(), findsOneWidget);
        expect(find.byKey(const Key('notifications-show-all')), findsNothing);

        count = 3;
        await nowTap(t, refresh);
        final three = t.getRect(find.byKey(const Key('now-social-edge')));
        expect(find.text('暂无通知'), findsNothing);
        for (var i = 1; i <= 3; i++) {
          expect(find.text('几何通知$i'), findsOneWidget);
        }
        expect(find.text('几何通知4'), findsNothing);
        expect(
          h.wire.dispatch.where((r) => r['path'] == '/v1/me/inbox/page'),
          hasLength(2),
        );
        expect(
          empty.height,
          lessThan(NowUiTokens.socialPanelMaxHeight - 80),
          reason:
              'A verified empty notification preview must not fill its cap.',
        );
        expect(
          three.height,
          greaterThan(empty.height + 48),
          reason:
              'Three real preview rows add readable space to the same panel.',
        );
        expect(
          three.height,
          lessThanOrEqualTo(NowUiTokens.socialPanelMaxHeight),
        );
        expect(three.top, empty.top);
        expect(three.width, empty.width);
        _sameMapAndInput(t, element, state);
        expect(
          t.widget<TextField>(nowField()).controller!.text,
          '通知浏览保留 Agent 草稿',
        );
        _expectNoNotificationGeometryWrites(h);
      });
    },
  );

  testWidgets(
    'notification geometry real host all list caps and scrolls internally across pages',
    (t) async {
      await _withHost(t, (h) async {
        h.wire.notificationPage = (request) =>
            _geometryNotificationPage(request, previewCount: 3, hasMore: true);
        final map = find.byType(PublicCityMapView);
        final element = map.evaluate().single, state = t.state(map);
        await _section(t, NowSocialSection.notifications);
        expect(find.text('几何通知4'), findsNothing);
        final all = find.byKey(const Key('notifications-show-all'));
        await t.scrollUntilVisible(
          all,
          120,
          scrollable: _geometryNotificationScroll(),
        );
        await nowTap(t, all);
        final capped = t.getRect(find.byKey(const Key('now-social-edge')));
        expect(capped.height, closeTo(NowUiTokens.socialPanelMaxHeight, .01));
        expect(
          t
              .state<ScrollableState>(_geometryNotificationScroll())
              .position
              .maxScrollExtent,
          greaterThan(0),
        );
        final more = find.byKey(const Key('notifications-load-more'));
        await t.scrollUntilVisible(
          more,
          180,
          scrollable: _geometryNotificationScroll(),
          maxScrolls: 30,
        );
        expect(more.hitTestable(), findsOneWidget);
        expect(find.text('几何通知20').hitTestable(), findsOneWidget);
        expect(t.getRect(find.byKey(const Key('now-social-edge'))), capped);
        await nowTap(t, more);
        await t.scrollUntilVisible(
          find.text('几何通知23'),
          180,
          scrollable: _geometryNotificationScroll(),
          maxScrolls: 30,
        );
        expect(find.text('几何通知23').hitTestable(), findsOneWidget);
        expect(find.byKey(const Key('notifications-load-more')), findsNothing);
        expect(t.getRect(find.byKey(const Key('now-social-edge'))), capped);
        final requests = h.wire.dispatch
            .where((r) => r['path'] == '/v1/me/inbox/page')
            .toList();
        expect(requests.map((r) => r['limit']), ['3', '20', '20']);
        _sameMapAndInput(t, element, state);
        _expectNoNotificationGeometryWrites(h);
      });
    },
  );

  testWidgets(
    'notification geometry real host two-times text keeps compact empty and preview-all reachable',
    (t) async {
      final semantics = t.ensureSemantics();
      try {
        await _withHost(t, (h) async {
          h.presentation.value = (2, Brightness.light);
          await t.pumpAndSettle();
          var count = 0;
          h.wire.notificationPage = (request) => _geometryNotificationPage(
            request,
            previewCount: count,
            hasMore: count > 0,
          );
          final map = find.byType(PublicCityMapView);
          final element = map.evaluate().single, state = t.state(map);
          await _section(t, NowSocialSection.notifications);
          expect(find.text('暂无通知').hitTestable(), findsOneWidget);
          final empty = t.getSize(find.byKey(const Key('now-social-edge')));
          final refresh = find.descendant(
            of: _geometryInbox(),
            matching: find.widgetWithText(TextButton, '刷新'),
          );
          expect(refresh.hitTestable(), findsOneWidget);
          expect(t.getSize(refresh).height, greaterThanOrEqualTo(48));
          count = 3;
          await nowTap(t, refresh);
          final preview = t.getRect(find.byKey(const Key('now-social-edge')));
          final all = find.byKey(const Key('notifications-show-all'));
          await t.scrollUntilVisible(
            all,
            120,
            scrollable: _geometryNotificationScroll(),
          );
          expect(all.hitTestable(), findsOneWidget);
          expect(t.getSize(all).height, greaterThanOrEqualTo(48));
          expect(
            t.semantics.simulatedAccessibilityTraversal().any(
              (node) => node.label.contains('查看全部通知'),
            ),
            true,
          );
          await nowTap(t, all);
          final more = find.byKey(const Key('notifications-load-more'));
          await t.scrollUntilVisible(
            more,
            180,
            scrollable: _geometryNotificationScroll(),
            maxScrolls: 40,
          );
          expect(more.hitTestable(), findsOneWidget);
          expect(t.getSize(more).height, greaterThanOrEqualTo(48));
          expect(
            empty.height,
            lessThan(NowUiTokens.socialPanelMaxHeight - 40),
            reason:
                'Large text still leaves unused map space for an empty list.',
          );
          expect(preview.height, greaterThan(empty.height));
          expect(
            preview.height,
            lessThanOrEqualTo(NowUiTokens.socialPanelMaxHeight),
          );
          expect(
            t.getSize(find.byKey(const Key('now-social-edge'))).height,
            lessThanOrEqualTo(NowUiTokens.socialPanelMaxHeight),
          );
          _sameMapAndInput(t, element, state);
          _expectNoNotificationGeometryWrites(h);
        });
      } finally {
        semantics.dispose();
      }
    },
  );
}
