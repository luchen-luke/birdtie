import 'dart:convert';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_action_contract_test.dart' show actionWire;

void main() {
  testWidgets('同key个人详情transport重绑使旧好友批准永久失效', (tester) async {
    const id = '22e9cd18-babb-4359-9d1e-53593bedff47';
    final binding = ValueNotifier(false);
    var writes = 0;
    http.Client source() => MockClient((r) async {
      if (r.method == 'POST') {
        writes++;
        return http.Response('{}', 201);
      }
      if (r.url.path.contains('/entity-actions/')) {
        final data = actionWire(ref: const EntityActionRef('person', id));
        for (final dynamic a in data['actions'] as List) {
          a['state'] = a['kind'] == 'CONNECT' ? 'AVAILABLE' : 'UNAVAILABLE';
        }
        return http.Response(
          jsonEncode({'data': data}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      return http.Response(
        jsonEncode({
          'data': {
            'accountId': id,
            'displayName': '合成伙伴',
            'visibility': 'public',
          },
        }),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    final first = source(), second = source();
    String? authorization() => 'Bearer original';
    await tester.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<bool>(
          valueListenable: binding,
          builder: (_, changed, _) => PublicPersonPage(
            key: const ValueKey('same-person'),
            accountID: id,
            authorizationHeader: authorization,
            client: changed ? second : first,
            apiBaseUrl: changed ? 'http://new.test' : 'http://old.test',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('申请连接'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), '旧来源明确留言');
    await tester.pump();
    await tester.tap(find.text('检查申请'));
    await tester.pumpAndSettle();
    expect(find.text('发送好友申请'), findsOneWidget);
    binding.value = true;
    await tester.pumpAndSettle();
    binding.value = false;
    await tester.pumpAndSettle();
    await tester.tap(find.text('发送好友申请'));
    await tester.pumpAndSettle();
    expect(
      writes,
      0,
      reason:
          'old proposal must not send old token through newly rebound transport or revive after ABA',
    );
    await tester.pumpWidget(const SizedBox());
    binding.dispose();
  });
  testWidgets('同key个人详情同值getter与监听替换A-B-A旧批准必须退休', (tester) async {
    const id = '22e9cd18-babb-4359-9d1e-53593bedff47';
    final binding = ValueNotifier(false);
    var writes = 0;
    http.Client source() => MockClient((r) async {
      if (r.method == 'POST') {
        writes++;
        return http.Response('{}', 201);
      }
      if (r.url.path.contains('/entity-actions/')) {
        final data = actionWire(ref: const EntityActionRef('person', id));
        for (final dynamic a in data['actions'] as List) {
          a['state'] = a['kind'] == 'CONNECT' ? 'AVAILABLE' : 'UNAVAILABLE';
        }
        return http.Response(
          jsonEncode({'data': data}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }
      return http.Response(
        jsonEncode({
          'data': {
            'accountId': id,
            'displayName': '合成伙伴',
            'visibility': 'public',
          },
        }),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    final first = source();
    final changesA = ValueNotifier(0), changesB = ValueNotifier(0);
    String? authorizationA() => 'Bearer original';
    String? authorizationB() => 'Bearer original';
    String? workspaceA() => null;
    String? workspaceB() => null;
    await tester.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<bool>(
          valueListenable: binding,
          builder: (_, changed, _) => PublicPersonPage(
            key: const ValueKey('same-person'),
            accountID: id,
            authorizationHeader: changed ? authorizationB : authorizationA,
            workspaceID: changed ? workspaceB : workspaceA,
            identityChanges: changed ? changesB : changesA,
            client: first,
            apiBaseUrl: 'http://same.test',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('申请连接'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), '旧来源明确留言');
    await tester.pump();
    await tester.tap(find.text('检查申请'));
    await tester.pumpAndSettle();
    expect(find.text('发送好友申请'), findsOneWidget);
    binding.value = true;
    await tester.pumpAndSettle();
    binding.value = false;
    await tester.pumpAndSettle();
    await tester.tap(find.text('发送好友申请'));
    await tester.pumpAndSettle();
    expect(
      writes,
      0,
      reason:
          'old proposal must not send old token through newly rebound transport or revive after ABA',
    );
    await tester.pumpWidget(const SizedBox());
    binding.dispose();
    changesA.dispose();
    changesB.dispose();
    first.close();
  });
  for (final decision in ['cancel', 'confirm', 'identityABA']) {
    testWidgets('本人连接具体合同 $decision 复用原好友申请不自动发消息', (tester) async {
      const id = '22e9cd18-babb-4359-9d1e-53593bedff47';
      final identity = ValueNotifier<String?>('Bearer a');
      var writes = 0;
      final client = MockClient((r) async {
        if (r.url.path.contains('/entity-actions/')) {
          final data = actionWire(ref: const EntityActionRef('person', id));
          for (final dynamic a in data['actions'] as List) {
            a['state'] = a['kind'] == 'CONNECT' ? 'AVAILABLE' : 'UNAVAILABLE';
          }
          return http.Response(
            jsonEncode({'data': data}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        if (r.method == 'POST') {
          writes++;
          expect(r.url.path, '/v1/me/connection-requests');
          expect(jsonDecode(r.body), {
            'recipientAccountId': id,
            'scope': 'friend',
            'note': '希望一起打球',
          });
          return http.Response('{}', 201);
        }
        if (r.url.path.endsWith('/profile')) {
          return http.Response(
            jsonEncode({
              'data': {
                'accountId': id,
                'displayName': '合成伙伴',
                'visibility': 'public',
              },
            }),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        return http.Response('{}', 404);
      });
      await tester.pumpWidget(
        MaterialApp(
          home: PublicPersonPage(
            accountID: id,
            authorizationHeader: () => identity.value,
            identityChanges: identity,
            client: client,
            apiBaseUrl: 'http://api.test',
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('申请连接'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), '希望一起打球');
      await tester.pump();
      await tester.tap(find.text('检查申请'));
      await tester.pumpAndSettle();
      expect(find.text('发送好友申请'), findsOneWidget);
      if (decision == 'identityABA') {
        identity.value = 'Bearer b';
        identity.value = 'Bearer a';
        await tester.pumpAndSettle();
      }
      await tester.tap(find.text(decision == 'confirm' ? '发送好友申请' : '取消'));
      await tester.pumpAndSettle();
      expect(writes, decision == 'confirm' ? 1 : 0);
      await tester.pumpWidget(const SizedBox());
      identity.dispose();
    });
  }
}
