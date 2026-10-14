import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/community_avatar.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'person_avatar_fixture.dart';

Map<String, Object?> communityAvatarMetadata({
  String id = avatarOwner,
  int version = 1,
}) => {
  'communityId': id,
  'version': version,
  'thumbnailPath': '/v1/communities/$id/avatar/$version/content',
  'width': 128,
  'height': 128,
  'byteCount': avatarJPEG().length,
  'sha256': sha256.convert(avatarJPEG()).toString(),
};
Map<String, Object?> communityAvatarResult(
  String op, {
  int applied = 1,
  int current = 1,
  String action = 'SAVE',
  bool present = true,
}) => {
  'communityId': avatarOwner,
  'operationId': op,
  'action': action,
  'appliedVersion': applied,
  'current': {
    'version': current,
    'avatar': present ? communityAvatarMetadata(version: current) : null,
  },
};
Map<String, Object?> communityAvatarApproval(Map<String, dynamic> body) => {
  'communityId': avatarOwner,
  'operationId': body['operationId'],
  'expectedVersion': body['expectedVersion'],
  'action': body['action'],
  'inputSha256': body['action'] == 'SAVE'
      ? sha256.convert(avatarJPEG()).toString()
      : '',
  'token': 'ca1.1.2.${'a' * 64}',
  'expiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(seconds: 25))
      .toIso8601String(),
};
Future<void> settleCommunityAvatar(WidgetTester t) async {
  // Mount a newly opened nested sheet before its streamed HTTP/codec work
  // gets a real event-loop turn. Fake-time animation pumping cannot drain it.
  await t.pump();
  await t.runAsync(() async {
    await Future<void>.delayed(const Duration(milliseconds: 100));
  });
  await t.pumpAndSettle();
}

void main() {
  test(
    'community metadata rejects account paths, stale versions and external URLs',
    () {
      final raw = communityAvatarMetadata();
      expect(
        CommunityAvatarMetadata.fromJson(raw, avatarOwner).communityID,
        avatarOwner,
      );
      for (final bad in [
        {'thumbnailPath': 'https://unapproved.test/private.jpg'},
        {'thumbnailPath': '/v1/accounts/$avatarOwner/avatar/1/content'},
        {'communityId': avatarPeer},
        {'version': 9007199254740992},
        {'width': 256},
      ]) {
        expect(
          () => CommunityAvatarMetadata.fromJson({...raw, ...bad}, avatarOwner),
          throwsFormatException,
        );
      }
      expect(
        CommunityAvatarState.fromJson({
          'version': 0,
          'avatar': null,
        }, avatarOwner).version,
        0,
      );
      expect(
        CommunityAvatarState.fromJson({
          'version': 2,
          'avatar': null,
        }, avatarOwner).avatar,
        isNull,
      );
    },
  );
  test(
    'operation receipt keeps original applied version apart from later current state',
    () {
      const pending = CommunityAvatarPending(avatarOperation, 0, 'SAVE');
      final result = CommunityAvatarResult.fromJson(
        communityAvatarResult(avatarOperation, current: 3, present: false),
        avatarOwner,
        pending,
      );
      expect(result.appliedVersion, 1);
      expect(result.current.version, 3);
      expect(
        () => CommunityAvatarResult.fromJson(
          communityAvatarResult(avatarOperation, applied: 2),
          avatarOwner,
          pending,
        ),
        throwsFormatException,
      );
    },
  );
  test(
    'avatar HTTP uses controlled same-origin content and verifies pixels digest',
    () async {
      var calls = 0;
      final client = MockClient((r) async {
        calls++;
        expect(r.url.host, 'fixture.test');
        expect(r.url.path, '/v1/communities/$avatarOwner/avatar/1/content');
        expect(r.headers['Authorization'], 'Bearer A');
        return http.Response.bytes(avatarJPEG(), 200);
      });
      addTearDown(client.close);
      final api = CommunityAvatarAPI(
        client: client,
        baseUrl: 'https://fixture.test',
        authorizationHeader: () => 'Bearer A',
      );
      final m = CommunityAvatarMetadata.fromJson(
        communityAvatarMetadata(),
        avatarOwner,
      );
      expect(await api.content(m), avatarJPEG());
      final bad = CommunityAvatarMetadata(
        communityID: avatarOwner,
        version: 1,
        path: 'https://foreign.test/private',
        byteCount: m.byteCount,
        digest: m.digest,
      );
      await expectLater(api.content(bad), throwsFormatException);
      expect(calls, 1);
    },
  );
  test(
    'preview and one PUT carry same frozen operation, body and explicit approval',
    () async {
      const p = CommunityAvatarPending(avatarOperation, 0, 'SAVE');
      Map<String, dynamic>? preview;
      var writes = 0;
      final client = MockClient((r) async {
        final body = jsonDecode(r.body) as Map<String, dynamic>;
        if (r.method == 'POST') {
          preview = body;
          return avatarWire(communityAvatarApproval(body));
        }
        writes++;
        expect(r.method, 'PUT');
        expect({...body}..remove('approval'), preview);
        expect(body['approval'], 'ca1.1.2.${'a' * 64}');
        return avatarWire(communityAvatarResult(avatarOperation));
      });
      addTearDown(client.close);
      final api = CommunityAvatarAPI(
        client: client,
        baseUrl: 'https://fixture.test',
        authorizationHeader: () => 'Bearer A',
      );
      final approved = await api.preview(avatarOwner, p, avatarJPEG());
      expect(writes, 0);
      expect(
        (await api.change(
          avatarOwner,
          p,
          avatarJPEG(),
          approved,
        )).appliedVersion,
        1,
      );
      expect(writes, 1);
    },
  );
  testWidgets(
    'current authorized thumbnail renders then is retired on workspace change',
    (t) async {
      var current = true;
      final changes = ValueNotifier(0);
      addTearDown(changes.dispose);
      final client = MockClient(
        (r) async => http.Response.bytes(avatarJPEG(), 200),
      );
      addTearDown(client.close);
      await t.pumpWidget(
        MaterialApp(
          home: CommunityAvatar(
            metadata: CommunityAvatarMetadata.fromJson(
              communityAvatarMetadata(),
              avatarOwner,
            ),
            fallbackLabel: '合成社群',
            api: CommunityAvatarAPI(
              client: client,
              baseUrl: 'https://fixture.test',
              authorizationHeader: () => 'Bearer A',
            ),
            identityChanges: changes,
            current: () => current,
          ),
        ),
      );
      await settleCommunityAvatar(t);
      expect(find.byType(Image), findsOneWidget);
      current = false;
      changes.value++;
      await t.pump();
      expect(find.byType(Image), findsNothing);
      expect(find.text('合'), findsOneWidget);
    },
  );
  testWidgets('late thumbnail after identity ABA does not reuse old pixels', (
    t,
  ) async {
    final late = Completer<http.Response>();
    var calls = 0;
    final changes = ValueNotifier('Bearer A');
    addTearDown(changes.dispose);
    final client = MockClient((r) async {
      calls++;
      if (calls == 1) return late.future;
      return avatarWire(null, status: 403);
    });
    addTearDown(client.close);
    await t.pumpWidget(
      MaterialApp(
        home: CommunityAvatar(
          metadata: CommunityAvatarMetadata.fromJson(
            communityAvatarMetadata(),
            avatarOwner,
          ),
          fallbackLabel: '合成社群',
          api: CommunityAvatarAPI(
            client: client,
            baseUrl: 'https://fixture.test',
            authorizationHeader: () => changes.value,
          ),
          identityChanges: changes,
          current: () => true,
        ),
      ),
    );
    await t.pump();
    changes.value = 'Bearer B';
    changes.value = 'Bearer A';
    late.complete(http.Response.bytes(avatarJPEG(), 200));
    await settleCommunityAvatar(t);
    expect(find.byType(Image), findsNothing);
  });
}
