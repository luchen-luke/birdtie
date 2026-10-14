import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/person_avatar_editor.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'person_avatar_fixture.dart';

Widget avatarEditor(
  http.Client client,
  ValueNotifier<int> changes,
  MemoryPersonAvatarPendingStore pending, {
  bool Function()? current,
}) => MaterialApp(
  home: Scaffold(
    body: SingleChildScrollView(
      child: PersonAvatarEditor(
        ownerID: avatarOwner,
        baseUrl: 'http://local.test',
        authorizationHeader: () => 'Bearer A',
        identityChanges: changes,
        current: current ?? () => true,
        client: client,
        pendingStore: pending,
        picker: () async => avatarJPEG(),
      ),
    ),
  ),
);
Future<void> chooseAvatar(WidgetTester t) async {
  await t.runAsync(() async {
    await t.tap(find.byKey(const Key('person-avatar-select')));
    await Future<void>.delayed(const Duration(milliseconds: 120));
  });
  await t.pumpAndSettle();
}

Future<void> settleAvatar(WidgetTester t) async {
  // Streamed HTTP and codec completion need a real event-loop turn before
  // waiting for the indeterminate UI loading animation to stop.
  await t.runAsync(() async {
    await Future<void>.delayed(const Duration(milliseconds: 80));
  });
  await t.pumpAndSettle();
}

void main() {
  test('pending recovery remains scoped to environment and person', () async {
    final store = MemoryPersonAvatarPendingStore();
    final pending = PendingPersonAvatar(avatarOperation, 3, 'DELETE');
    await store.write('A', avatarOwner, pending);
    expect(await store.read('B', avatarOwner), isNull);
    expect(await store.read('A', avatarPeer), isNull);
    expect(PendingPersonAvatar.decode(jsonEncode(pending.toJson())).version, 3);
  });
  testWidgets(
    'selection stays local until save then sends cropped current version',
    (t) async {
      var writes = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/content'))
          return http.Response.bytes(avatarJPEG(), 200);
        if (r.method == 'GET')
          return avatarWire({'version': 0, 'avatar': null});
        writes++;
        final body = jsonDecode(r.body) as Map<String, dynamic>;
        expect(body['expectedVersion'], 0);
        expect(body['contentType'], 'image/png');
        return avatarWire(avatarResult(body['operationId'] as String));
      });
      addTearDown(client.close);
      final changes = ValueNotifier(0);
      addTearDown(changes.dispose);
      final pending = MemoryPersonAvatarPendingStore();
      await t.pumpWidget(avatarEditor(client, changes, pending));
      await settleAvatar(t);
      await chooseAvatar(t);
      expect(
        find.byKey(const Key('person-avatar-local-preview')),
        findsOneWidget,
      );
      expect(writes, 0);
      await t.tap(find.byKey(const Key('person-avatar-save')));
      await settleAvatar(t);
      expect(writes, 1);
      expect(find.text('头像已保存。'), findsOneWidget);
      expect(await pending.read('http://local.test', avatarOwner), isNull);
    },
  );
  testWidgets('unknown mutation reconciles a receipt without resending', (
    t,
  ) async {
    var writes = 0;
    String? op;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/content'))
        return http.Response.bytes(avatarJPEG(), 200);
      if (r.url.path.contains('/operations/'))
        return avatarWire(avatarResult(op!));
      if (r.method == 'GET') return avatarWire({'version': 0, 'avatar': null});
      writes++;
      op =
          (jsonDecode(r.body) as Map<String, dynamic>)['operationId'] as String;
      return avatarWire(null, status: 503);
    });
    addTearDown(client.close);
    final changes = ValueNotifier(0);
    addTearDown(changes.dispose);
    final pending = MemoryPersonAvatarPendingStore();
    await t.pumpWidget(avatarEditor(client, changes, pending));
    await settleAvatar(t);
    await chooseAvatar(t);
    await t.tap(find.byKey(const Key('person-avatar-save')));
    await settleAvatar(t);
    expect(find.byKey(const Key('person-avatar-reconcile')), findsOneWidget);
    expect(await pending.read('http://local.test', avatarOwner), isNotNull);
    await t.tap(find.byKey(const Key('person-avatar-reconcile')));
    await settleAvatar(t);
    expect(writes, 1);
    expect(await pending.read('http://local.test', avatarOwner), isNull);
  });
  testWidgets('deletion confirms the shown version and retires old picture', (
    t,
  ) async {
    var deletes = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/content'))
        return http.Response.bytes(avatarJPEG(), 200);
      if (r.method == 'GET')
        return avatarWire({'version': 1, 'avatar': avatarMetadata()});
      deletes++;
      expect(r.method, 'DELETE');
      final body = jsonDecode(r.body) as Map<String, dynamic>;
      expect(body['expectedVersion'], 1);
      return avatarWire(
        avatarResult(
          body['operationId'] as String,
          version: 2,
          action: 'DELETE',
          present: false,
        ),
      );
    });
    addTearDown(client.close);
    final changes = ValueNotifier(0);
    addTearDown(changes.dispose);
    await t.pumpWidget(
      avatarEditor(client, changes, MemoryPersonAvatarPendingStore()),
    );
    await settleAvatar(t);
    await t.tap(find.byKey(const Key('person-avatar-delete')));
    await settleAvatar(t);
    expect(deletes, 0);
    await t.tap(find.widgetWithText(FilledButton, '删除头像'));
    await settleAvatar(t);
    expect(deletes, 1);
    expect(find.text('头像已删除。'), findsOneWidget);
  });
  testWidgets('workspace change removes a local selected picture', (t) async {
    var current = true;
    final client = MockClient(
      (r) async => avatarWire({'version': 0, 'avatar': null}),
    );
    addTearDown(client.close);
    final changes = ValueNotifier(0);
    addTearDown(changes.dispose);
    await t.pumpWidget(
      avatarEditor(
        client,
        changes,
        MemoryPersonAvatarPendingStore(),
        current: () => current,
      ),
    );
    await settleAvatar(t);
    await chooseAvatar(t);
    expect(
      find.byKey(const Key('person-avatar-local-preview')),
      findsOneWidget,
    );
    current = false;
    changes.value++;
    await t.pumpAndSettle();
    expect(find.byKey(const Key('person-avatar-local-preview')), findsNothing);
    expect(find.byKey(const Key('person-avatar-save')), findsNothing);
  });
}
