import 'dart:convert';
import 'package:birdtie_client/src/content/collection_publication_controller.dart';
import 'package:birdtie_client/src/content/collection_publication_page.dart';
import 'package:birdtie_client/src/content/personal_collection_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'personal_collection_controller_test.dart'
    show
        collectionID,
        collectionReceipt,
        collectionResponse,
        collectionController;
import 'collection_publication_controller_test.dart'
    show operationRecord, publicRecord;

void main() {
  testWidgets(
    'C3 borrowed private source explains restriction and cannot enable author grant',
    (t) async {
      var writes = 0;
      final client = MockClient((r) async {
        if (r.method != 'GET') writes++;
        return collectionResponse({
          ...collectionReceipt(),
          'privatePlaceCopyRestricted': true,
        });
      });
      final source = collectionController(client);
      await source.openID(collectionID);
      final c = CollectionPublicationController(
        source: source,
        authorizationHeader: source.authorizationHeader,
        ownerID: source.ownerID,
        client: client,
        apiBaseUrl: source.apiBaseUrl,
      );
      await t.pumpWidget(
        MaterialApp(
          home: CollectionPublicationPage(controller: c, cityName: '合成城市'),
        ),
      );
      await t.pumpAndSettle();
      expect(find.textContaining('原复制许可只允许私人保存'), findsOneWidget);
      final permission = find.byKey(
        const Key('publication-allow-private-place-copy'),
      );
      await t.ensureVisible(permission);
      expect(t.widget<SwitchListTile>(permission).onChanged, isNull);
      expect(c.canPublish, isFalse);
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      source.dispose();
      client.close();
    },
  );
  testWidgets(
    'C3 author private-place-copy permission starts off and never implies publication',
    (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      var writes = 0;
      final client = MockClient((r) async {
        if (r.method != 'GET') writes++;
        return collectionResponse(collectionReceipt());
      });
      final source = collectionController(client);
      await source.open(PersonalCollection.fromJson(collectionReceipt()));
      final c = CollectionPublicationController(
        source: source,
        authorizationHeader: source.authorizationHeader,
        ownerID: source.ownerID,
        client: client,
        apiBaseUrl: source.apiBaseUrl,
      );
      await t.pumpWidget(
        MaterialApp(
          home: CollectionPublicationPage(controller: c, cityName: '合成城市'),
        ),
      );
      await t.pumpAndSettle();
      final permission = find.byKey(
        const Key('publication-allow-private-place-copy'),
      );
      await t.ensureVisible(permission);
      expect(t.widget<SwitchListTile>(permission).value, isFalse);
      expect(writes, 0);
      await t.tap(permission);
      await t.pumpAndSettle();
      expect(c.allowPrivatePlaceCopy, isTrue);
      expect(writes, 0);
      expect(find.textContaining('不复制你的名称、说明、备注或坐标'), findsOneWidget);
      source.holdPublication({
        'id': collectionID,
        'kind': 'journey',
        'operationId': collectionID,
        'revision': 1,
        'action': 'PUBLISH',
      });
      await t.pumpAndSettle();
      expect(t.widget<SwitchListTile>(permission).onChanged, isNull);
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      source.dispose();
      client.close();
    },
  );
  testWidgets('结束原操作先明确检查；返回和对话框期间换操作都不提交', (t) async {
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      return collectionResponse(collectionReceipt());
    });
    final source = collectionController(client);
    await source.open(PersonalCollection.fromJson(collectionReceipt()));
    final pending = {
      'id': collectionID,
      'kind': 'journey',
      'operationId': collectionID,
      'revision': 1,
      'action': 'PUBLISH',
    };
    source.holdPublication(pending);
    final c = CollectionPublicationController(
      source: source,
      authorizationHeader: source.authorizationHeader,
      ownerID: source.ownerID,
      client: client,
      apiBaseUrl: source.apiBaseUrl,
    );
    await t.pumpWidget(
      MaterialApp(
        home: CollectionPublicationPage(controller: c, cityName: '阿伯丁'),
      ),
    );
    await t.pumpAndSettle();
    final button = find.widgetWithText(OutlinedButton, '结束并核实原操作');
    await t.ensureVisible(button);
    await t.tap(button);
    await t.pumpAndSettle();
    expect(find.textContaining('不会重新发布、撤回或删除内容'), findsOneWidget);
    await t.tap(find.widgetWithText(TextButton, '保留待核实状态'));
    await t.pumpAndSettle();
    expect(writes, 0);
    expect(c.uncertain, true);
    await t.tap(button);
    await t.pumpAndSettle();
    source.holdPublication({
      ...pending,
      'operationId': '33333333-3333-4333-8333-333333333333',
    });
    await t.pumpAndSettle();
    expect(find.text('账号或原操作已变化，请返回重新核实。'), findsOneWidget);
    expect(
      t
          .widget<FilledButton>(find.widgetWithText(FilledButton, '结束并核实'))
          .onPressed,
      isNull,
    );
    await t.tap(find.widgetWithText(FilledButton, '结束并核实'));
    await t.pumpAndSettle();
    expect(writes, 0);
    expect(
      source.publicationPending!['operationId'],
      '33333333-3333-4333-8333-333333333333',
    );
    await t.pumpWidget(const SizedBox());
    c.dispose();
    source.dispose();
    client.close();
  });
  testWidgets('公开预览显示完整公开文字和位置范围，大字移动端只明确点击发布', (tester) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    var writes = 0;
    late PersonalCollectionController source;
    final j = collectionReceipt()..['summary'] = '会公开的完整说明';
    (j['entries'] as List)[0]['note'] = '会公开的地点备注';
    final client = MockClient((r) async {
      if (r.method == 'GET') return collectionResponse(j);
      writes++;
      final body = jsonDecode(r.body) as Map<String, dynamic>;
      expect(body['entries'][0]['locationVisibility'], 'hidden');
      return collectionResponse(operationRecord(source.publicationPending!));
    });
    source = collectionController(client);
    await source.open(PersonalCollection.fromJson(j));
    final c = CollectionPublicationController(
      source: source,
      authorizationHeader: source.authorizationHeader,
      ownerID: source.ownerID,
      client: client,
      apiBaseUrl: source.apiBaseUrl,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(1.6)),
          child: CollectionPublicationPage(controller: c, cityName: '阿伯丁'),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(writes, 0);
    expect(find.text('会公开的完整说明'), findsOneWidget);
    expect(find.text('会公开的地点备注'), findsOneWidget);
    await tester.ensureVisible(find.text('将以上内容公开发布'));
    await tester.tap(find.text('将以上内容公开发布'));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(find.text('撤回公开，保留本人草稿'), findsOneWidget);
    expect(tester.takeException(), null);
    await tester.pumpWidget(const SizedBox());
    c.dispose();
    source.dispose();
    client.close();
  });
  testWidgets('分享阅读可重读失效状态，不保留原公开内容', (tester) async {
    var visible = true;
    final client = MockClient(
      (r) async => visible
          ? collectionResponse(publicRecord())
          : collectionResponse({}, code: 404),
    );
    final c = PublicCollectionController(
      reference: const CollectionShareReference('journey', collectionID),
      authorizationHeader: () => null,
      client: client,
      apiBaseUrl: 'https://synthetic.invalid',
    );
    await tester.pumpWidget(
      MaterialApp(home: PublicCollectionPage(controller: c)),
    );
    await tester.pumpAndSettle();
    expect(find.text('周末路线'), findsOneWidget);
    visible = false;
    await tester.tap(find.text('重新读取'));
    await tester.pumpAndSettle();
    expect(find.text('周末路线'), findsNothing);
    expect(find.textContaining('当前分享不可查看'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    c.dispose();
    client.close();
  });
}
