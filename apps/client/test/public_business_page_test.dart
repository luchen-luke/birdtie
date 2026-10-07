import 'dart:async';
import 'package:birdtie_client/src/workspace/public_business_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'supplier_profile_api_test.dart'
    show supplierID, publicSupplier, supplierResponse;

void main() {
  testWidgets(
    'Chinese public facts and refreshed link require current approved source',
    (t) async {
      var reads = 0, opens = 0;
      final client = MockClient((_) async {
        reads++;
        return supplierResponse(publicSupplier());
      });
      await t.pumpWidget(
        MaterialApp(
          home: PublicBusinessPage(
            businessID: supplierID,
            authorizationHeader: () => null,
            onOpenActivity: (_) {},
            apiBaseUrl: 'http://127.0.0.1:3697',
            client: client,
            openExternal: (_) async {
              opens++;
              return true;
            },
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('经营权已核验'), findsOneWidget);
      expect(find.textContaining('交易安全'), findsOneWidget);
      await t.tap(find.text('https://example.invalid/official'));
      await t.pumpAndSettle();
      expect(reads, 2);
      expect(opens, 1);
      expect(t.takeException(), isNull);
    },
  );
  testWidgets('withdrawn source never opens the old official link', (t) async {
    var reads = 0, opens = 0;
    final client = MockClient(
      (_) async => supplierResponse(publicSupplier(published: ++reads == 1)),
    );
    await t.pumpWidget(
      MaterialApp(
        home: PublicBusinessPage(
          businessID: supplierID,
          authorizationHeader: () => null,
          onOpenActivity: (_) {},
          apiBaseUrl: 'http://127.0.0.1:3697',
          client: client,
          openExternal: (_) async {
            opens++;
            return true;
          },
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('https://example.invalid/official'));
    await t.pumpAndSettle();
    expect(opens, 0);
    expect(find.text('商家尚无当前有效且获准公开的介绍与链接。'), findsOneWidget);
    expect(find.text('https://example.invalid/official'), findsNothing);
  });
  testWidgets('identity ABA discards an old response even when token returns', (
    t,
  ) async {
    final identity = ValueNotifier('Bearer A');
    final old = Completer<http.Response>();
    var reads = 0;
    final client = MockClient((_) async {
      if (++reads == 1) return old.future;
      return supplierResponse(publicSupplier()..['name'] = '当前合成商家');
    });
    await t.pumpWidget(
      MaterialApp(
        home: PublicBusinessPage(
          businessID: supplierID,
          authorizationHeader: () => identity.value,
          identityChanges: identity,
          onOpenActivity: (_) {},
          apiBaseUrl: 'http://127.0.0.1:3697',
          client: client,
        ),
      ),
    );
    identity.value = 'Bearer B';
    identity.value = 'Bearer A';
    await t.pumpAndSettle();
    old.complete(supplierResponse(publicSupplier()..['name'] = '旧身份商家'));
    await t.pumpAndSettle();
    expect(find.text('旧身份商家'), findsNothing);
    expect(find.text('当前合成商家'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
  });
  testWidgets(
    '360px and large text render without overflow and launcher errors recover',
    (t) async {
      t.view.physicalSize = const Size(360, 800);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final client = MockClient(
        (_) async => supplierResponse(publicSupplier()),
      );
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: PublicBusinessPage(
            businessID: supplierID,
            authorizationHeader: () => null,
            onOpenActivity: (_) {},
            apiBaseUrl: 'http://127.0.0.1:3697',
            client: client,
            openExternal: (_) async => throw StateError('platform error'),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
      await t.ensureVisible(find.text('https://example.invalid/official'));
      await t.pumpAndSettle();
      await t.tap(find.text('https://example.invalid/official'));
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
      expect(find.text('重新读取商家资料'), findsOneWidget);
    },
  );
}
