import 'package:birdtie_client/src/content/moment_publication_controller.dart';
import 'package:birdtie_client/src/content/moment_publication_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'moment_publication_controller_test.dart'
    show
        publicationTestPreview,
        publicationTestReply,
        publicationTestMoment,
        publicationTestPlace;

void main() {
  testWidgets('公开确认展示内容和后果，取消不写入，大字小屏无溢出', (t) async {
    await t.binding.setSurfaceSize(const Size(360, 640));
    addTearDown(() => t.binding.setSurfaceSize(null));
    var writes = 0;
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer self',
      client: MockClient((r) async {
        if (r.method != 'GET') writes++;
        return publicationTestReply(publicationTestPreview());
      }),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await t.pumpWidget(
      MaterialApp(
        builder: (ctx, child) => MediaQuery(
          data: MediaQuery.of(
            ctx,
          ).copyWith(textScaler: const TextScaler.linear(1.6)),
          child: child!,
        ),
        home: Scaffold(body: MomentPublicationSheet(controller: c)),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('想公开的记录'), findsOneWidget);
    expect(find.textContaining('任何人都可能看到'), findsOneWidget);
    expect(
      (t.widget<FilledButton>(find.byType(FilledButton))).onPressed,
      isNull,
    );
    await t.ensureVisible(find.text('取消'));
    await t.tap(find.text('取消'));
    await t.pumpAndSettle();
    expect(writes, 0);
    expect(t.takeException(), isNull);
  });
  testWidgets('重新加载具体版本清除勾选批准', (t) async {
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer self',
      client: MockClient(
        (_) async => publicationTestReply(publicationTestPreview()),
      ),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(body: MomentPublicationSheet(controller: c)),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.byType(CheckboxListTile));
    await t.pump();
    expect(
      (t.widget<FilledButton>(find.byType(FilledButton))).onPressed,
      isNotNull,
    );
    await c.load();
    await t.pumpAndSettle();
    expect(
      (t.widget<FilledButton>(find.byType(FilledButton))).onPressed,
      isNull,
    );
  });
}
