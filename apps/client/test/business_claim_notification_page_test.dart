import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:birdtie_client/src/workspace/business_claim_notification_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const owner = 'be000000-0000-4000-8000-000000000099';
const business = 'be000000-0000-4000-8000-000000000001';

http.Response reply({
  String id = business,
  String role = 'owner',
  bool manage = true,
}) => http.Response(
  jsonEncode({
    'data': {
      'business': {
        'id': id,
        'name': '本地合成商家审核目标',
        'claimStatus': 'rejected',
        'role': role,
      },
      'canManage': manage,
      'canManageMembers': manage,
      'canReview': false,
      'membershipVersion': 1,
      'reviewPermissions': <String>[],
      'claim': {'version': 2, 'state': 'rejected'},
      'profile': null,
      'venues': <dynamic>[],
      'members': <dynamic>[],
    },
  }),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  testWidgets('original business current Console read, no management write', (
    tester,
  ) async {
    final requests = <http.Request>[];
    String? token() => 'Bearer local-A';
    bool current() => true;
    final api = BusinessApi(
      apiBaseUrl: 'http://127.0.0.1:1',
      authorizationHeader: token,
      client: MockClient((r) async {
        requests.add(r);
        return reply();
      }),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: BusinessClaimNotificationPage(
          businessID: business,
          ownerID: owner,
          authorizationHeader: token,
          isCurrent: current,
          api: api,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('本地合成商家审核目标'), findsOneWidget);
    expect(find.text('当前经营权状态：未通过'), findsOneWidget);
    expect(find.text('审核资料版本：2'), findsOneWidget);
    await tester.tap(find.text('重新读取当前状态'));
    await tester.pumpAndSettle();
    expect(requests.length, 2);
    expect(
      requests.every(
        (r) =>
            r.method == 'GET' &&
            r.url.path == '/v1/me/businesses/$business/console',
      ),
      isTrue,
    );
  });

  for (final state in ['removed', 'reviewer', 'other_business']) {
    testWidgets('current authority denies $state without copied claim', (
      tester,
    ) async {
      String? token() => 'Bearer local-A';
      bool current() => true;
      final api = BusinessApi(
        apiBaseUrl: 'http://127.0.0.1:1',
        authorizationHeader: token,
        client: MockClient(
          (_) async => reply(
            id: state == 'other_business'
                ? 'be000000-0000-4000-8000-000000000002'
                : business,
            role: state == 'reviewer' ? 'reviewer' : 'owner',
            manage: state != 'removed',
          ),
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: BusinessClaimNotificationPage(
            businessID: business,
            ownerID: owner,
            authorizationHeader: token,
            isCurrent: current,
            api: api,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('本地合成商家审核目标'), findsNothing);
      expect(
        find.text(
          state == 'other_business'
              ? '商家工作台暂不可用，请稍后重试。'
              : '当前账号没有管理或审核这项资料的权限。',
        ),
        findsOneWidget,
      );
    });
  }

  testWidgets('same key replacement and ABA permanently retire original read', (
    tester,
  ) async {
    final pending = Completer<http.Response>();
    var calls = 0;
    String? token() => 'Bearer local-A';
    bool current() => true;
    final old = BusinessApi(
      apiBaseUrl: 'http://127.0.0.1:1',
      authorizationHeader: token,
      client: MockClient((_) {
        calls++;
        return pending.future;
      }),
    );
    final next = BusinessApi(
      apiBaseUrl: 'http://127.0.0.1:2',
      authorizationHeader: token,
      client: MockClient((_) async {
        calls++;
        return reply();
      }),
    );
    Widget page(BusinessApi api) => MaterialApp(
      home: BusinessClaimNotificationPage(
        key: const ValueKey('claim'),
        businessID: business,
        ownerID: owner,
        authorizationHeader: token,
        isCurrent: current,
        api: api,
      ),
    );
    await tester.pumpWidget(page(old));
    await tester.pump();
    await tester.pumpWidget(page(next));
    await tester.pumpWidget(page(old));
    pending.complete(reply());
    await tester.pumpAndSettle();
    expect(calls, 1);
    expect(find.text('本地合成商家审核目标'), findsNothing);
    expect(find.text('账号、会话或来源已变化，请返回收件箱重新打开。'), findsOneWidget);
  });

  testWidgets('credential ABA drops late old response without implicit retry', (
    tester,
  ) async {
    final pending = Completer<http.Response>();
    var calls = 0;
    String? bearer = 'Bearer local-A';
    String? token() => bearer;
    bool current() => true;
    final api = BusinessApi(
      apiBaseUrl: 'http://127.0.0.1:1',
      authorizationHeader: token,
      client: MockClient((_) {
        calls++;
        return pending.future;
      }),
    );
    Widget page() => MaterialApp(
      home: BusinessClaimNotificationPage(
        businessID: business,
        ownerID: owner,
        authorizationHeader: token,
        isCurrent: current,
        api: api,
      ),
    );
    await tester.pumpWidget(page());
    await tester.pump();
    bearer = 'Bearer local-B';
    await tester.pumpWidget(page());
    bearer = 'Bearer local-A';
    await tester.pumpWidget(page());
    pending.complete(reply());
    await tester.pumpAndSettle();
    expect(calls, 1);
    expect(find.text('本地合成商家审核目标'), findsNothing);
  });

  testWidgets('320 width text scale3 keeps read action reachable', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    String? token() => 'Bearer local-A';
    bool current() => true;
    final api = BusinessApi(
      apiBaseUrl: 'http://127.0.0.1:1',
      authorizationHeader: token,
      client: MockClient((_) async => reply()),
    );
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(3)),
          child: child!,
        ),
        home: BusinessClaimNotificationPage(
          businessID: business,
          ownerID: owner,
          authorizationHeader: token,
          isCurrent: current,
          api: api,
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('重新读取当前状态'), 200);
    await tester.tap(find.text('重新读取当前状态'));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });
}
