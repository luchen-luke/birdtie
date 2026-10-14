import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/birdtie_login_sheet.dart';
import 'package:birdtie_client/src/auth/dev_phone_login_sheet.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/activity_participations.dart';
import 'package:birdtie_client/src/workspace/activity_plans.dart';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

http.Response _reply(Object data, [int status = 200]) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

class _AuthFixture {
  _AuthFixture({
    bool saved = false,
    this.waitingMe,
    this.waitingVerify,
    this.session = 'unit-session',
    this.owner = 'unit-owner',
  }) {
    vault = MemorySessionVault();
    if (saved)
      vault.session = StoredSession(
        token: session,
        method: 'dev_phone',
        origin: 'https://unit.example',
      );
    client = MockClient((request) async {
      calls.add('${request.method} ${request.url.path}');
      if (request.url.path == '/v1/accounts/$owner/profile') {
        return _reply({'displayName': '本人'});
      }
      switch (request.url.path) {
        case '/v1/auth/dev-phone/status':
          return _reply({'enabled': true});
        case '/v1/auth/dev-phone/code':
          return _reply({'expiresInSeconds': 300});
        case '/v1/auth/dev-phone/verify':
          if (waitingVerify != null) return waitingVerify!.future;
          return _reply({'accessToken': session});
        case '/v1/me':
          if (waitingMe != null) return waitingMe!.future;
          return _reply({'id': owner});
        case '/v1/session/logout':
          return http.Response('', 204);
        default:
          return http.Response('{}', 404);
      }
    });
    auth = BirdtieAuthController(
      client: client,
      apiBaseUrl: 'https://unit.example',
      sessionVault: vault,
    );
  }
  final Completer<http.Response>? waitingMe;
  final Completer<http.Response>? waitingVerify;
  final String session, owner;
  final calls = <String>[];
  late final MemorySessionVault vault;
  late final MockClient client;
  late final BirdtieAuthController auth;
}

void main() {
  test(
    'restoration is visible, credential stays private, initialize reentry shares one operation',
    () async {
      final fixture = _AuthFixture(
        saved: true,
        waitingMe: Completer<http.Response>(),
      );
      Future<void>? reentered;
      fixture.auth.addListener(() {
        if (fixture.auth.sessionRestoring)
          reentered ??= fixture.auth.initialize();
      });
      final initializing = fixture.auth.initialize();
      await Future<void>.delayed(Duration.zero);
      expect(identical(initializing, reentered), isTrue);
      expect(fixture.auth.sessionRestoring, isTrue);
      expect(fixture.auth.authorizationHeader, isNull);
      expect(fixture.auth.accountID, isNull);
      expect(await fixture.auth.requestDevPhoneCode('13800138000'), isFalse);
      expect(
        await fixture.auth.verifyDevPhoneCode('13800138000', '123456'),
        isFalse,
      );
      fixture.waitingMe!.complete(_reply({'id': 'unit-owner'}));
      await initializing;
      expect(fixture.auth.sessionRestoring, isFalse);
      expect(fixture.auth.authorizationHeader, 'Bearer unit-session');
      expect(fixture.calls.where((call) => call == 'GET /v1/me'), hasLength(1));
      expect(fixture.calls.where((call) => call.startsWith('POST')), isEmpty);
      expect(identical(initializing, fixture.auth.initialize()), isTrue);
      fixture.auth.dispose();
    },
  );

  test(
    'retired login before dispatch never sends an authentication request',
    () async {
      final fixture = _AuthFixture();
      await fixture.auth.initialize();
      var current = true;
      void retireOnBusy() {
        if (fixture.auth.busy) current = false;
      }

      fixture.auth.addListener(retireOnBusy);
      expect(
        await fixture.auth.requestDevPhoneCode(
          '13800138000',
          isCurrent: () => current,
        ),
        isFalse,
      );
      expect(fixture.calls.where((call) => call.startsWith('POST')), isEmpty);
      fixture.auth.removeListener(retireOnBusy);
      expect(
        await fixture.auth.verifyDevPhoneCode(
          '13800138000',
          '123456',
          isCurrent: () => false,
        ),
        isFalse,
      );
      expect(fixture.auth.signedIn, isFalse);
      fixture.auth.dispose();
    },
  );

  test(
    'late issued login after entry retirement is revoked without accepting or persisting',
    () async {
      final fixture = _AuthFixture(waitingVerify: Completer<http.Response>());
      await fixture.auth.initialize();
      var current = true;
      final verifying = fixture.auth.verifyDevPhoneCode(
        '13800138000',
        '123456',
        isCurrent: () => current,
      );
      await Future<void>.delayed(Duration.zero);
      current = false;
      fixture.waitingVerify!.complete(_reply({'accessToken': 'unit-session'}));
      expect(await verifying, isFalse);
      expect(fixture.auth.signedIn, isFalse);
      expect(await fixture.vault.read(), isNull);
      expect(fixture.calls.where((call) => call == 'GET /v1/me'), isEmpty);
      expect(
        fixture.calls.where((call) => call == 'POST /v1/session/logout'),
        hasLength(1),
      );
      fixture.auth.dispose();
    },
  );

  testWidgets(
    'restoring Inbox reads no private data and loads once after native auth succeeds',
    (tester) async {
      final fixture = _AuthFixture(
        saved: true,
        waitingMe: Completer<http.Response>(),
      );
      final initializing = fixture.auth.initialize();
      var reads = 0, posts = 0;
      final source = RemoteInboxSource(
        authorizationHeader: () => fixture.auth.authorizationHeader,
        apiBaseUrl: 'https://unit.example',
        client: MockClient((request) async {
          if (request.method != 'GET') posts++;
          if (request.url.path == '/v1/me/inbox') reads++;
          return _reply([]);
        }),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: InboxPanel(auth: fixture.auth, source: source),
          ),
        ),
      );
      await tester.pump();
      expect(find.text('正在恢复账户'), findsOneWidget);
      expect(find.text('登录'), findsNothing);
      expect(reads, 0);
      fixture.waitingMe!.complete(_reply({'id': 'unit-owner'}));
      await initializing;
      await tester.pumpAndSettle();
      expect(reads, 1);
      expect(posts, 0);
      expect(find.text('暂无通知'), findsOneWidget);
      fixture.auth.updateProfileDisplayName('新昵称');
      await tester.pumpAndSettle();
      expect(reads, 1);
      await tester.pumpWidget(const SizedBox());
      source.dispose();
      fixture.auth.dispose();
    },
  );

  testWidgets(
    'cancel and repeated close callback keep Inbox and send no requests',
    (tester) async {
      final fixture = _AuthFixture();
      await fixture.auth.initialize();
      var privateReads = 0;
      final source = RemoteInboxSource(
        authorizationHeader: () => fixture.auth.authorizationHeader,
        apiBaseUrl: 'https://unit.example',
        client: MockClient((request) async {
          privateReads++;
          return _reply([]);
        }),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () => showModalBottomSheet<void>(
                  context: context,
                  isScrollControlled: true,
                  builder: (_) => SizedBox(
                    height: 380,
                    child: InboxPanel(auth: fixture.auth, source: source),
                  ),
                ),
                child: const Text('打开收件箱'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('打开收件箱'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('登录'));
      await tester.pumpAndSettle();
      expect(find.byType(BirdtieLoginSheet), findsOneWidget);
      final close = tester
          .widget<TextButton>(find.widgetWithText(TextButton, '返回'))
          .onPressed!;
      close();
      close();
      await tester.pumpAndSettle();
      expect(find.byType(InboxPanel), findsOneWidget);
      expect(privateReads, 0);
      expect(fixture.calls.where((call) => call.startsWith('POST')), isEmpty);
      await tester.pumpWidget(const SizedBox());
      source.dispose();
      fixture.auth.dispose();
    },
  );

  testWidgets(
    'guest Inbox at short screen and 200% exposes a real 48dp action',
    (tester) async {
      tester.view.physicalSize = const Size(320, 240);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final fixture = _AuthFixture();
      await fixture.auth.initialize();
      var logins = 0, reads = 0;
      final source = RemoteInboxSource(
        authorizationHeader: () => fixture.auth.authorizationHeader,
        apiBaseUrl: 'https://unit.example',
        client: MockClient((request) async {
          reads++;
          return _reply([]);
        }),
      );
      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: Scaffold(
            body: InboxPanel(
              auth: fixture.auth,
              source: source,
              onSignIn: () => logins++,
            ),
          ),
        ),
      );
      await tester.ensureVisible(find.widgetWithText(FilledButton, '登录'));
      await tester.pump();
      expect(
        tester.getSize(find.widgetWithText(FilledButton, '登录')).height,
        greaterThanOrEqualTo(48),
      );
      await tester.tap(find.text('登录'));
      await tester.pump();
      expect(logins, 1);
      expect(reads, 0);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      source.dispose();
      fixture.auth.dispose();
    },
  );

  testWidgets(
    'same-key auth replacement retires chooser without launching old dev login',
    (tester) async {
      final a = _AuthFixture(), b = _AuthFixture();
      await a.auth.initialize();
      await b.auth.initialize();
      final sourceA = RemoteInboxSource(
        authorizationHeader: () => a.auth.authorizationHeader,
        apiBaseUrl: 'https://unit.example',
        client: MockClient((_) async => _reply([])),
      );
      final sourceB = RemoteInboxSource(
        authorizationHeader: () => b.auth.authorizationHeader,
        apiBaseUrl: 'https://unit.example',
        client: MockClient((_) async => _reply([])),
      );
      final active = ValueNotifier<bool>(false);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ValueListenableBuilder<bool>(
              valueListenable: active,
              builder: (_, useB, __) => InboxPanel(
                key: const ValueKey('same-inbox'),
                auth: useB ? b.auth : a.auth,
                source: useB ? sourceB : sourceA,
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('登录'));
      await tester.pumpAndSettle();
      active.value = true;
      await tester.pumpAndSettle();
      expect(find.text('账户或入口已变化，请返回后重新打开。'), findsOneWidget);
      expect(find.text('手机号测试登录'), findsNothing);
      await tester.tap(find.text('返回'));
      await tester.pumpAndSettle();
      expect(find.byType(DevPhoneLoginSheet), findsNothing);
      expect(a.calls.where((call) => call.startsWith('POST')), isEmpty);
      await tester.pumpWidget(const SizedBox());
      active.dispose();
      sourceA.dispose();
      sourceB.dispose();
      a.auth.dispose();
      b.auth.dispose();
    },
  );

  testWidgets(
    'same-key MyActivities replacement rejects sources from the previous account',
    (tester) async {
      final a = _AuthFixture(saved: true),
          b = _AuthFixture(
            saved: true,
            session: 'unit-session-b',
            owner: 'unit-owner-b',
          );
      await a.auth.initialize();
      await b.auth.initialize();
      final reads = <String>[];
      final client = MockClient((request) async {
        reads.add('${request.method} ${request.url.path}');
        return _reply([]);
      });
      final plans = ActivityPlansController(
        authorizationHeader: () => a.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      final attendance = ActivityParticipationController(
        authorizationHeader: () => a.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      final saved = SavedController(
        authorizationHeader: () => a.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      final city = PublicCityController(
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      final organizations = OrganizationWorkspaceController(
        authorizationHeader: () => a.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      final active = ValueNotifier<bool>(false);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ValueListenableBuilder<bool>(
              valueListenable: active,
              builder: (_, useB, __) => MyActivitiesPage(
                key: const ValueKey('same-my-activities'),
                plans: plans,
                participations: attendance,
                saved: saved,
                auth: useB ? b.auth : a.auth,
                city: city,
                organizations: organizations,
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(reads, isNotEmpty);
      final count = reads.length;
      active.value = true;
      await tester.pumpAndSettle();
      expect(find.text('账户来源已变化'), findsOneWidget);
      expect(find.text('发起活动'), findsNothing);
      expect(reads.length, count);
      b.auth.updateProfileDisplayName('B 新昵称');
      await tester.pumpAndSettle();
      expect(reads.length, count);
      expect(reads.every((path) => path.startsWith('GET ')), isTrue);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      active.dispose();
      plans.dispose();
      attendance.dispose();
      saved.dispose();
      city.dispose();
      organizations.dispose();
      a.auth.dispose();
      b.auth.dispose();
      client.close();
    },
  );

  testWidgets(
    'MyActivities real login reads saved and phase-only notifications do not reload',
    (tester) async {
      final fixture = _AuthFixture();
      await fixture.auth.initialize();
      final reads = <String>[];
      final client = MockClient((request) async {
        reads.add('${request.method} ${request.url.path}');
        return _reply([]);
      });
      final plans = ActivityPlansController(
        authorizationHeader: () => fixture.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      final attendance = ActivityParticipationController(
        authorizationHeader: () => fixture.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      final saved = SavedController(
        authorizationHeader: () => fixture.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      final city = PublicCityController(
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      final organizations = OrganizationWorkspaceController(
        authorizationHeader: () => fixture.auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'https://unit.example',
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MyActivitiesPage(
              plans: plans,
              participations: attendance,
              saved: saved,
              auth: fixture.auth,
              city: city,
              organizations: organizations,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(reads, isEmpty);
      expect(find.text('登录后查看我的活动'), findsOneWidget);
      expect(
        await fixture.auth.verifyDevPhoneCode('13800138000', '123456'),
        isTrue,
      );
      await tester.pumpAndSettle();
      expect(reads.where((path) => path == 'GET /v1/me/saved'), hasLength(1));
      expect(
        reads.where((path) => path == 'GET /v1/me/activity-plans'),
        hasLength(1),
      );
      expect(
        reads.where((path) => path == 'GET /v1/me/participations'),
        hasLength(1),
      );
      expect(
        reads.where((path) => path == 'GET /v1/me/activities'),
        hasLength(1),
      );
      final count = reads.length;
      fixture.auth.updateProfileDisplayName('改名');
      await tester.pumpAndSettle();
      expect(reads.length, count);
      expect(reads.every((path) => path.startsWith('GET ')), isTrue);
      expect(find.text('登录'), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      plans.dispose();
      attendance.dispose();
      saved.dispose();
      city.dispose();
      organizations.dispose();
      fixture.auth.dispose();
      client.close();
    },
  );
}
