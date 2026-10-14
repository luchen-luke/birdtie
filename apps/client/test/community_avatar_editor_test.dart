import 'dart:convert';
import 'package:birdtie_client/src/workspace/community_avatar.dart';
import 'package:birdtie_client/src/workspace/community_avatar_editor.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'person_avatar_fixture.dart';
import 'community_avatar_test.dart';

Widget editor(
  http.Client client,
  ValueNotifier<String> changes,
  MemoryCommunityAvatarPendingStore store,
) => MaterialApp(
  home: Scaffold(
    body: SingleChildScrollView(
      child: CommunityAvatarEditor(
        communityID: avatarOwner,
        communityName: '合成视觉社群',
        ownerID: avatarPeer,
        api: CommunityAvatarAPI(
          client: client,
          baseUrl: 'https://fixture.test',
          authorizationHeader: () => changes.value,
        ),
        identityChanges: changes,
        current: () => true,
        initialImage: avatarJPEG(),
        pendingStore: store,
      ),
    ),
  ),
);

void main() {
  test(
    'durable selector binds origin/person/community and prevents unknown overwrite',
    () async {
      final store = MemoryCommunityAvatarPendingStore();
      const p = CommunityAvatarPending(avatarOperation, 2, 'SAVE');
      await store.write('A', avatarOwner, avatarPeer, p);
      expect(await store.read('B', avatarOwner, avatarPeer), isNull);
      expect(await store.read('A', avatarPeer, avatarOwner), isNull);
      await expectLater(
        store.write(
          'A',
          avatarOwner,
          avatarPeer,
          const CommunityAvatarPending(
            avatarOperation,
            2,
            'SAVE',
            stage: 'PREPARED',
          ),
        ),
        throwsFormatException,
      );
      expect(CommunityAvatarPending.fromJson(p.toJson()).same(p), isTrue);
    },
  );
  for (final mode in [
    'cancel',
    'identityABA',
    'success',
    'unknown',
    'rejected',
  ]) {
    testWidgets(
      'community avatar frozen approval and original operation $mode',
      (t) async {
        var writes = 0, previews = 0, receipts = 0;
        String? op;
        final changes = ValueNotifier('Bearer A');
        addTearDown(changes.dispose);
        final store = MemoryCommunityAvatarPendingStore();
        final client = MockClient((r) async {
          if (r.url.path.endsWith('/content'))
            return http.Response.bytes(avatarJPEG(), 200);
          if (r.url.path.contains('/operations/')) {
            receipts++;
            expect(r.url.path.endsWith(op!), isTrue);
            return receipts == 1
                ? avatarWire(null, status: 404)
                : avatarWire(
                    communityAvatarResult(op!, current: 2, present: false),
                  );
          }
          if (r.method == 'GET')
            return avatarWire({'version': 0, 'avatar': null});
          final body = jsonDecode(r.body) as Map<String, dynamic>;
          if (r.method == 'POST') {
            previews++;
            op = body['operationId'];
            expect(body['expectedVersion'], 0);
            return avatarWire(communityAvatarApproval(body));
          }
          writes++;
          expect(body['operationId'], op);
          expect(body['action'], 'SAVE');
          expect(body['approval'], 'ca1.1.2.${'a' * 64}');
          return mode == 'unknown'
              ? avatarWire(null, status: 503)
              : mode == 'rejected'
              ? avatarWire(null, status: 409)
              : avatarWire(communityAvatarResult(op!));
        });
        addTearDown(client.close);
        await t.pumpWidget(editor(client, changes, store));
        await settleCommunityAvatar(t);
        await t.tap(find.byKey(const Key('community-avatar-save')));
        await settleCommunityAvatar(t);
        expect(previews, 1);
        expect(writes, 0);
        expect(find.text('保存社群头像？'), findsOneWidget);
        expect(find.text('头像随社群可见范围展示。'), findsOneWidget);
        if (mode == 'cancel') {
          await t.tap(find.text('取消'));
          await settleCommunityAvatar(t);
          expect(writes, 0);
          return;
        }
        if (mode == 'identityABA') {
          changes.value = 'Bearer B';
          await t.pump();
          changes.value = 'Bearer A';
          await t.pump();
          expect(
            t
                .widget<FilledButton>(find.widgetWithText(FilledButton, '确认保存'))
                .onPressed,
            isNull,
          );
          expect(writes, 0);
          return;
        }
        await t.tap(find.text('确认保存'));
        await settleCommunityAvatar(t);
        expect(writes, 1);
        if (mode == 'unknown') {
          expect(
            await store.read('https://fixture.test', avatarPeer, avatarOwner),
            isNotNull,
          );
          await t.tap(find.byKey(const Key('community-avatar-recover')));
          await settleCommunityAvatar(t);
          expect(find.text('尚未找到原操作结果，请稍后再核实。'), findsOneWidget);
          expect(writes, 1);
          await t.tap(find.byKey(const Key('community-avatar-recover')));
          await settleCommunityAvatar(t);
          expect(receipts, 2);
          expect(writes, 1);
          expect(find.text('原头像操作已完成。'), findsOneWidget);
          expect(
            await store.read('https://fixture.test', avatarPeer, avatarOwner),
            isNull,
          );
        } else if (mode == 'rejected') {
          expect(
            await store.read('https://fixture.test', avatarPeer, avatarOwner),
            isNull,
          );
          expect(find.text('重新读取'), findsOneWidget);
          expect(writes, 1);
        } else {
          expect(find.text('社群头像已保存。'), findsOneWidget);
          expect(
            await store.read('https://fixture.test', avatarPeer, avatarOwner),
            isNull,
          );
        }
        expect(t.takeException(), isNull);
      },
    );
  }
  testWidgets('delete confirms current version and sends no pixels', (t) async {
    var writes = 0;
    final changes = ValueNotifier('Bearer A');
    addTearDown(changes.dispose);
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/content'))
        return http.Response.bytes(avatarJPEG(), 200);
      if (r.method == 'GET')
        return avatarWire({'version': 1, 'avatar': communityAvatarMetadata()});
      final body = jsonDecode(r.body) as Map<String, dynamic>;
      expect(body['action'], 'DELETE');
      expect(body['expectedVersion'], 1);
      expect(body.containsKey('contentType'), isFalse);
      expect(body.containsKey('contentBase64'), isFalse);
      if (r.method == 'POST') return avatarWire(communityAvatarApproval(body));
      writes++;
      return avatarWire(
        communityAvatarResult(
          body['operationId'],
          applied: 2,
          current: 2,
          action: 'DELETE',
          present: false,
        ),
      );
    });
    addTearDown(client.close);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CommunityAvatarEditor(
            communityID: avatarOwner,
            communityName: '合成视觉社群',
            ownerID: avatarPeer,
            api: CommunityAvatarAPI(
              client: client,
              baseUrl: 'https://fixture.test',
              authorizationHeader: () => changes.value,
            ),
            identityChanges: changes,
            current: () => true,
            pendingStore: MemoryCommunityAvatarPendingStore(),
          ),
        ),
      ),
    );
    await settleCommunityAvatar(t);
    await t.tap(find.text('删除头像'));
    await settleCommunityAvatar(t);
    expect(writes, 0);
    await t.tap(find.text('确认删除'));
    await settleCommunityAvatar(t);
    expect(writes, 1);
    expect(find.text('头像已删除。'), findsOneWidget);
    expect(find.byType(Image), findsNothing);
  });
}
