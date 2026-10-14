import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _author = '33333333-3333-4333-8333-333333333333';
const _other = '44444444-4444-4444-8444-444444444444';

http.Response _profile(String id, {String visibility = 'public'}) =>
    http.Response(
      jsonEncode({
        'data': {
          'accountId': id,
          'displayName': '合成公开作者',
          'visibility': visibility,
        },
      }),
      200,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );

void main() {
  testWidgets('个人公开来源提供48dp只读当前范围入口，embedded无第二Scaffold', (t) async {
    var opens = 0;
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return _profile(_author);
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PublicPersonPage(
            accountID: _author,
            embedded: true,
            authorizationHeader: () => null,
            client: client,
            apiBaseUrl: 'http://unit.test',
            onOpenPublicMoments: () async {
              opens++;
            },
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    final button = find.widgetWithText(TextButton, '当前范围的公开动态');
    expect(t.getSize(button).height, greaterThanOrEqualTo(48));
    expect(find.byType(Scaffold), findsOneWidget);
    await t.tap(button);
    await t.pumpAndSettle();
    expect(opens, 1);
    expect(calls.every((r) => r.method == 'GET'), isTrue);
    await t.pumpWidget(const SizedBox());
    client.close();
  });
  for (final mode in [
    'no-callback',
    'private',
    'org',
    'read-error',
    'wrong-target',
  ]) {
    testWidgets('$mode 不显示当前范围公开动态入口', (t) async {
      final client = MockClient(
        (r) async => mode == 'read-error'
            ? http.Response('{}', 404)
            : _profile(
                mode == 'wrong-target' ? _other : _author,
                visibility: mode == 'private' ? 'private' : 'public',
              ),
      );
      await t.pumpWidget(
        MaterialApp(
          home: PublicPersonPage(
            accountID: _author,
            authorizationHeader: () => 'Bearer unit',
            workspaceID: () => mode == 'org' ? 'unit-org' : null,
            client: client,
            apiBaseUrl: 'http://unit.test',
            onOpenPublicMoments: mode == 'no-callback' ? null : () async {},
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('当前范围的公开动态'), findsNothing);
      await t.pumpWidget(const SizedBox());
      client.close();
    });
  }
  for (final mode in [
    'identity-ABA',
    'target-ABA',
    'callback-ABA',
    'transport-ABA',
  ]) {
    testWidgets('$mode 保存的旧入口不能回生', (t) async {
      final identity = ValueNotifier<String?>('Bearer unit-a');
      final binding = ValueNotifier(false);
      var opens = 0;
      final first = MockClient(
        (r) async => _profile(r.url.path.contains(_other) ? _other : _author),
      );
      final second = MockClient(
        (r) async => _profile(r.url.path.contains(_other) ? _other : _author),
      );
      Future<void> openA() async {
        opens++;
      }

      Future<void> openB() async {
        opens++;
      }

      String? authorization() => identity.value;
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<bool>(
            valueListenable: binding,
            builder: (_, changed, _) => PublicPersonPage(
              key: const Key('same-author'),
              accountID: mode == 'target-ABA' && changed ? _other : _author,
              authorizationHeader: authorization,
              identityChanges: identity,
              client: mode == 'transport-ABA' && changed ? second : first,
              apiBaseUrl: 'http://unit.test',
              onOpenPublicMoments: mode == 'callback-ABA' && changed
                  ? openB
                  : openA,
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      final saved = t
          .widget<TextButton>(find.widgetWithText(TextButton, '当前范围的公开动态'))
          .onPressed!;
      if (mode == 'identity-ABA') {
        identity.value = 'Bearer unit-b';
        identity.value = 'Bearer unit-a';
        await t.pumpAndSettle();
      } else {
        binding.value = true;
        await t.pumpAndSettle();
        binding.value = false;
        await t.pumpAndSettle();
      }
      saved();
      await t.pumpAndSettle();
      expect(opens, 0);
      await t.pumpWidget(const SizedBox());
      identity.dispose();
      binding.dispose();
      first.close();
      second.close();
    });
  }
  testWidgets('迟到profile在target改变后不产生旧作者入口', (t) async {
    final pending = Completer<http.Response>();
    final target = ValueNotifier(_author);
    final client = MockClient(
      (r) async => r.url.path.contains(_author)
          ? pending.future
          : http.Response('{}', 404),
    );
    await t.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<String>(
          valueListenable: target,
          builder: (_, id, _) => PublicPersonPage(
            key: const Key('same-author'),
            accountID: id,
            authorizationHeader: () => null,
            client: client,
            apiBaseUrl: 'http://unit.test',
            onOpenPublicMoments: () async {},
          ),
        ),
      ),
    );
    await t.pump();
    target.value = _other;
    await t.pump();
    pending.complete(_profile(_author));
    await t.pumpAndSettle();
    expect(find.text('合成公开作者'), findsNothing);
    expect(find.text('当前范围的公开动态'), findsNothing);
    await t.pumpWidget(const SizedBox());
    target.dispose();
    client.close();
  });
}
