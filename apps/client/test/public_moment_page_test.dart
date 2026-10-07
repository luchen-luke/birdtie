import 'dart:async';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'public_moment_api_test.dart';
import 'entity_share_pending_store_test.dart' show shareTarget;

void main() {
  for (final mode in ['account', 'workspace']) {
    testWidgets('公开动态$mode ABA丢弃旧响应', (t) async {
      final identity = ValueNotifier<String?>('Bearer A'),
          workspace = ValueNotifier<String?>(null);
      final old = Completer<http.Response>();
      var reads = 0;
      final client = MockClient(
        (r) async => ++reads == 1
            ? old.future
            : momentReply(publicMomentData(title: '当前动态')),
      );
      await t.pumpWidget(
        MaterialApp(
          home: PublicMomentPage(
            momentID: shareTarget,
            authorizationHeader: () => identity.value,
            workspaceID: () => workspace.value,
            identityChanges: Listenable.merge([identity, workspace]),
            client: client,
            apiBaseUrl: 'https://api.example',
          ),
        ),
      );
      if (mode == 'account') {
        identity.value = 'Bearer B';
        identity.value = 'Bearer A';
      } else {
        workspace.value = 'organization';
        workspace.value = null;
      }
      await t.pumpAndSettle();
      old.complete(momentReply(publicMomentData(title: '旧身份动态')));
      await t.pumpAndSettle();
      expect(find.text('旧身份动态'), findsNothing);
      expect(find.text('当前动态'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      identity.dispose();
      workspace.dispose();
    });
  }
  testWidgets('公开动态中文、小屏大字、稳定地点入口及撤回重试', (t) async {
    var reads = 0;
    String? opened;
    final client = MockClient(
      (_) async => ++reads == 1
          ? http.Response('{}', 404)
          : momentReply(publicMomentData()),
    );
    await t.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(360, 800),
            textScaler: TextScaler.linear(2),
          ),
          child: PublicMomentPage(
            momentID: shareTarget,
            authorizationHeader: () => null,
            apiBaseUrl: 'https://api.example',
            client: client,
            onOpenPlace: (id) => opened = id,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('重新核实'));
    await t.pumpAndSettle();
    expect(find.textContaining('不是到访'), findsNothing);
    expect(find.textContaining('不代表已核验'), findsOneWidget);
    await t.ensureVisible(find.text('查看地点'));
    await t.tap(find.text('查看地点'));
    expect(opened, isNotNull);
    expect(t.takeException(), isNull);
  });
}
