import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/business_public_permission_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'supplier_profile_api_test.dart' show supplierID, supplierResponse;

Map<String, dynamic> permission({int version = 0, String? snapshot}) => {
  'permission': {
    'version': version,
    'profileVersion': version == 0 ? 0 : 2,
    'state': version == 0 ? 'unpublished' : 'active',
    'validUntil': version == 0 ? null : '2027-01-01T10:00:00Z',
  },
  'disclosureStatus': version == 0 ? 'unpublished' : 'active',
  'preview': {
    'profileVersion': 2,
    'name': '合成商家',
    'description': '准备公开的介绍',
    'officialLinks': ['https://example.invalid/official'],
    'reviewedAt': '2026-10-03T10:00:00Z',
    'validUntil': '2027-01-01T10:00:00Z',
    'sourceSnapshot': snapshot ?? List.filled(64, 'a').join(),
  },
};
Widget permissionPage(MockClient client, ValueNotifier<String> identity) =>
    MaterialApp(
      home: BusinessPublicPermissionPage(
        businessID: supplierID,
        businessName: '合成商家',
        authorizationHeader: () => identity.value,
        workspaceID: () => null,
        identityChanges: identity,
        apiBaseUrl: 'http://127.0.0.1:3697',
        client: client,
      ),
    );
Future<void> check(WidgetTester t) async {
  await t.ensureVisible(find.byType(CheckboxListTile));
  await t.tap(find.byType(CheckboxListTile));
  await t.pump();
}

Future<void> publish(WidgetTester t) async {
  await t.ensureVisible(find.text('确认公开这些资料'));
  await t.tap(find.text('确认公开这些资料'));
  await t.pumpAndSettle();
}

void main() {
  testWidgets(
    'unchecked precise public disclosure has no PUT; confirm submits one exact version',
    (t) async {
      var puts = 0;
      Map<String, dynamic>? sent;
      final identity = ValueNotifier('Bearer A');
      final client = MockClient((r) async {
        if (r.method == 'PUT') {
          puts++;
          sent = jsonDecode(r.body) as Map<String, dynamic>;
          return supplierResponse({'version': 1});
        }
        return supplierResponse(permission(version: puts));
      });
      await t.pumpWidget(permissionPage(client, identity));
      await t.pumpAndSettle();
      await publish(t);
      expect(puts, 0);
      await check(t);
      await publish(t);
      expect(puts, 1);
      expect(sent?['expectedVersion'], 0);
      expect(sent?['expectedProfileVersion'], 2);
      expect(sent?['action'], 'publish');
      expect(sent?['sourceSnapshot'], List.filled(64, 'a').join());
      expect(find.text('公开状态：当前公开'), findsOneWidget);
      expect(
        (t.widget(find.byType(CheckboxListTile)) as CheckboxListTile).value,
        isFalse,
      );
      await t.pumpWidget(const SizedBox());
      identity.dispose();
    },
  );
  testWidgets('source change resets approval and makes no write', (t) async {
    var gets = 0, puts = 0;
    final identity = ValueNotifier('Bearer A');
    final client = MockClient((r) async {
      if (r.method == 'PUT') puts++;
      return supplierResponse(
        permission(snapshot: ++gets > 1 ? List.filled(64, 'b').join() : null),
      );
    });
    await t.pumpWidget(permissionPage(client, identity));
    await t.pumpAndSettle();
    await check(t);
    await publish(t);
    expect(puts, 0);
    expect(find.textContaining('内容、权限或有效期已变化'), findsOneWidget);
    expect(
      (t.widget(find.byType(CheckboxListTile)) as CheckboxListTile).value,
      isFalse,
    );
    await t.pumpWidget(const SizedBox());
    identity.dispose();
  });
  testWidgets('unknown write outcome reads current state without resending', (
    t,
  ) async {
    var puts = 0, gets = 0;
    final identity = ValueNotifier('Bearer A');
    final client = MockClient((r) async {
      if (r.method == 'PUT') {
        puts++;
        throw StateError('transport lost');
      }
      gets++;
      return supplierResponse(permission(version: puts));
    });
    await t.pumpWidget(permissionPage(client, identity));
    await t.pumpAndSettle();
    await check(t);
    await publish(t);
    expect(puts, 1);
    expect(gets, 3);
    expect(find.text('提交结果待核实，不会自动重发。'), findsOneWidget);
    expect(find.text('公开状态：当前公开'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
  });
  testWidgets('account ABA invalidates the open withdrawal approval', (
    t,
  ) async {
    var puts = 0;
    final identity = ValueNotifier('Bearer A');
    final client = MockClient((r) async {
      if (r.method == 'PUT') puts++;
      return supplierResponse(permission(version: 1));
    });
    await t.pumpWidget(permissionPage(client, identity));
    await t.pumpAndSettle();
    await t.ensureVisible(find.text('撤回公开资料'));
    await t.tap(find.text('撤回公开资料'));
    await t.pump(const Duration(milliseconds: 400));
    expect(find.text('确认撤回'), findsOneWidget);
    identity.value = 'Bearer B';
    identity.value = 'Bearer A';
    await t.pump();
    expect(find.text('确认撤回'), findsNothing);
    expect(find.text('身份已变化，请取消并重新检查'), findsOneWidget);
    await t.tap(find.text('取消'));
    await t.pumpAndSettle();
    expect(puts, 0);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
  });
  testWidgets('delayed owner preview cannot populate new account', (t) async {
    final identity = ValueNotifier('Bearer A');
    final gate = Completer<http.Response>();
    final client = MockClient((_) => gate.future);
    await t.pumpWidget(permissionPage(client, identity));
    await t.pump();
    identity.value = 'Bearer B';
    gate.complete(supplierResponse(permission()));
    await t.pumpAndSettle();
    expect(find.text('准备公开的介绍'), findsNothing);
    expect(find.byType(CheckboxListTile), findsNothing);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
  });
}
