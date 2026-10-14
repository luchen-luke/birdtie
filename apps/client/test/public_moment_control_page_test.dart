import 'dart:convert';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/moment_control_pending_store.dart';
import 'package:birdtie_client/src/workspace/public_moment_control_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'moment_control_test.dart';
import 'moment_control_controller_test.dart' show mcJSON, mcRecord;

class PublicControlFixture {
  final identity = ValueNotifier<(String, String)>((
    'Bearer synthetic',
    mcOwner,
  ));
  Map<String, Object?> record = {
    ...mcRecord(revision: 2),
    'visibility': 'public',
    'status': 'published',
    'placeId': mcTarget,
    'locationPrecision': 'place',
  };
  final inputs = <String, MomentControlInput>{};
  int puts = 0;
  late final client = MockClient((r) async {
    if (r.method == 'GET' && r.url.path == '/v1/me/moments/$mcTarget')
      return mcJSON(record);
    if (r.method == 'GET' && r.url.path == '/v1/me/moments')
      return mcJSON(record['status'] == 'withdrawn' ? [] : [record]);
    if (r.url.path == '/v1/me/moment-controls/previews') {
      final body = jsonDecode(r.body) as Map<String, dynamic>;
      final input = MomentControlInput(
        operationID: body['operationId'],
        action: body['action'],
        momentID: body['momentId'],
        expectedRevision: body['expectedRevision'],
        input: body['input'] == null
            ? null
            : Map<String, Object?>.from(body['input']),
      );
      inputs[input.operationID] = input;
      return mcJSON(mcPreview(input));
    }
    if (r.method == 'PUT' &&
        r.url.path.startsWith('/v1/me/moment-controls/operations/')) {
      puts++;
      final input = inputs[r.url.path.split('/').last]!;
      record = {
        ...record,
        if (input.input != null) ...input.input!,
        'revision': input.expectedRevision + 1,
        if (input.action == 'WITHDRAW') 'status': 'withdrawn',
      };
      return mcJSON(mcReceipt(input));
    }
    throw StateError('unexpected ${r.method} ${r.url.path}');
  });
  late final controller = PrivateMomentController(
    authorizationHeader: () => identity.value.$1,
    ownerID: () => identity.value.$2,
    identityChanges: identity,
    client: client,
    apiBaseUrl: 'https://isolated.invalid',
    pendingStore: MemoryMomentControlPendingStore(),
  );
  Future<void> open(WidgetTester t) async {
    await t.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => Navigator.of(context).push<String>(
                MaterialPageRoute(
                  builder: (_) => PublicMomentControlPage(
                    moments: controller,
                    momentID: mcTarget,
                    identityChanges: identity,
                  ),
                ),
              ),
              child: const Text('打开本人动态'),
            ),
          ),
        ),
      ),
    );
    await t.tap(find.text('打开本人动态'));
    await advance(t);
  }

  void dispose() {
    controller.dispose();
    identity.dispose();
    client.close();
  }
}

Future<void> advance(WidgetTester t) async {
  await t.runAsync(() async {
    await Future<void>.delayed(const Duration(milliseconds: 10));
  });
  await t.pump();
  await t.pump(const Duration(milliseconds: 350));
  await t.pump();
}

void main() {
  testWidgets('公开修改保持原ID并明确图片后果，编辑退休旧确认', (t) async {
    final f = PublicControlFixture();
    addTearDown(f.dispose);
    await f.open(t);
    await t.enterText(find.byType(TextField).last, '本人新的公开正文');
    await t.tap(find.text('查看本次修改'));
    await advance(t);
    expect(
      f.controller.operationPreview,
      isNotNull,
      reason: f.controller.operationError,
    );
    FocusManager.instance.primaryFocus?.unfocus();
    await advance(t);
    await t.scrollUntilVisible(find.text('确认保存公开修改'), 180, scrollable: find.byType(Scrollable).first);
    expect(find.textContaining('保存本次修改会撤回图片'), findsOneWidget);
    final stale = t
        .widget<FilledButton>(find.widgetWithText(FilledButton, '确认保存公开修改'))
        .onPressed!;
    await t.enterText(find.byType(TextField).last, '修改后最终正文');
    stale();
    await advance(t);
    expect(f.puts, 0);
    await t.tap(find.text('查看本次修改'));
    await advance(t);
    await t.ensureVisible(find.text('确认保存公开修改'));
    await t.tap(find.text('确认保存公开修改'));
    await advance(t);
    expect(f.puts, 1);
    expect(f.record['id'], mcTarget);
    expect(f.record['revision'], 3);
    expect(f.record['body'], '修改后最终正文');
  });
  testWidgets('删除由明确确认提交，换账号后旧确认不能生效', (t) async {
    final f = PublicControlFixture();
    addTearDown(f.dispose);
    await f.open(t);
    await t.ensureVisible(find.text('删除动态'));
    await t.tap(find.text('删除动态'));
    await advance(t);
    expect(
      f.controller.operationPreview,
      isNotNull,
      reason: f.controller.operationError,
    );
    await t.scrollUntilVisible(find.text('确认删除动态'), 180, scrollable: find.byType(Scrollable).first);
    expect(f.puts, 0);
    final stale = t
        .widget<FilledButton>(find.widgetWithText(FilledButton, '确认删除动态'))
        .onPressed!;
    f.identity.value = ('Bearer other', '44444444-4444-4444-8444-444444444444');
    await advance(t);
    stale();
    await advance(t);
    expect(f.puts, 0);
    expect(find.text('本人私人原文'), findsNothing);
  });
}
