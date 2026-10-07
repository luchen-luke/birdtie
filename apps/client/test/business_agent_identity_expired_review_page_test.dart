import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/business_agent_identity_page.dart';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'business_agent_identity_api_test.dart'
    show identityBusiness, identityPerson, identityWire, identityReply;

class ExpiryIdentityClient extends http.BaseClient {
  final requests = <http.BaseRequest>[];
  DateTime? firstDeadline;
  int identityReads = 0;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    // Capture synchronous send entry, not a later MockClient callback.
    requests.add(request);
    final at = DateTime.now().toUtc();
    final read = request.method == 'GET';
    if (read) identityReads++;
    final wire = identityWire(
      exists: !read,
      version: identityReads == 1 ? 2 : 3,
      now: at,
    );
    if (read && identityReads == 1) {
      firstDeadline = at.add(const Duration(seconds: 3));
      wire['validUntil'] = firstDeadline!.toIso8601String();
    }
    final response = identityReply(wire);
    return http.StreamedResponse(
      Stream.value(response.bodyBytes),
      response.statusCode,
    );
  }
}

void main() {
  testWidgets('确认时读取快照到期明确未提交，新读取新检查才建立暂停身份', (t) async {
    final client = ExpiryIdentityClient();
    final changes = ChangeNotifier();
    final source = Object();
    final api = BusinessApi(
      authorizationHeader: () => 'Bearer local-expiry-contract',
      apiBaseUrl: 'https://business.test',
      client: client,
    );
    addTearDown(() {
      api.dispose();
      client.close();
      changes.dispose();
    });
    await t.pumpWidget(
      MaterialApp(
        home: BusinessAgentIdentityPage(
          api: api,
          businessID: identityBusiness,
          accountID: () => identityPerson,
          authorizationHeader: () => 'Bearer local-expiry-contract',
          workspaceID: () => null,
          currentBusinessID: () => identityBusiness,
          bindingCurrent: () => true,
          sourceFrame: () => source,
          identityChanges: changes,
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('本地合成商家'), findsOneWidget);
    expect(client.requests.map((r) => r.method), ['GET']);
    final check = find.widgetWithText(FilledButton, '检查并建立身份');
    await t.ensureVisible(check);
    expect(check.hitTestable(), findsOneWidget);
    expect(t.getSize(check).height, greaterThanOrEqualTo(48));
    await t.tap(check);
    await t.pumpAndSettle();
    expect(find.text('确认建立商家智能体身份'), findsOneWidget);
    final confirm = find.widgetWithText(FilledButton, '确认建立');
    expect(confirm.hitTestable(), findsOneWidget);
    // Default production controller and Page use actual DateTime.now; pumping
    // FakeAsync time alone would not reproduce an expired read snapshot.
    final deadline = client.firstDeadline!;
    await t.runAsync(() async {
      final remaining = deadline.difference(DateTime.now().toUtc());
      if (!remaining.isNegative) {
        await Future<void>.delayed(
          remaining + const Duration(milliseconds: 40),
        );
      }
    });
    expect(DateTime.now().toUtc().isBefore(deadline), false);
    await t.tap(confirm);
    await t.pumpAndSettle();
    expect(client.requests.where((r) => r.method == 'POST'), isEmpty);
    expect(find.text('确认建立商家智能体身份'), findsNothing);
    expect(
      find.text('本次未提交，读取快照已到期，请重新读取后检查。'),
      findsOneWidget,
    );
    expect(find.textContaining('经营权失效'), findsNothing);
    expect(find.text('尚未建立身份'), findsOneWidget);
    expect(check, findsNothing);

    final reload = find.widgetWithText(OutlinedButton, '重新读取当前身份');
    await t.ensureVisible(reload);
    expect(reload.hitTestable(), findsOneWidget);
    await t.tap(reload);
    await t.pumpAndSettle();
    expect(client.identityReads, 2);
    expect(
      find.text('本次未提交，读取快照已到期，请重新读取后检查。'),
      findsNothing,
    );
    await t.ensureVisible(check);
    expect(check.hitTestable(), findsOneWidget);
    await t.tap(check);
    await t.pumpAndSettle();
    expect(confirm.hitTestable(), findsOneWidget);
    await t.tap(confirm);
    await t.pumpAndSettle();
    final posts = client.requests.where((r) => r.method == 'POST').toList();
    expect(posts, hasLength(1));
    expect(posts.single.url.path,
        '/v1/me/businesses/$identityBusiness/agent-identity');
    expect(
      jsonDecode((posts.single as http.Request).body),
      {'expectedClaimVersion': 3},
    );
    expect(client.requests.map((r) => r.method), ['GET', 'GET', 'POST']);
    expect(find.text('身份已建立 · 暂停中'), findsOneWidget);
    expect(
      find.text('身份已建立并保持暂停；未启用模型、资料读取或工具。'),
      findsOneWidget,
    );
    await t.pumpWidget(const SizedBox());
  });
}
