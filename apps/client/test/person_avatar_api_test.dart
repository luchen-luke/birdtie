import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/person_avatar.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/now_social_edge_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'person_avatar_fixture.dart';

void main() {
  test('metadata binds owner version internal path and thumbnail bound', () {
    final valid = avatarMetadata();
    expect(PersonAvatarMetadata.fromJson(valid, avatarOwner).version, 1);
    for (final bad in [
      {...valid, 'accountId': avatarPeer},
      {...valid, 'thumbnailPath': 'https://external.invalid/picture'},
      {...valid, 'thumbnailPath': '/v1/accounts/$avatarOwner/avatar/2/content'},
      {...valid, 'byteCount': personAvatarMaxThumbnailBytes + 1},
      {...valid, 'width': 4096},
    ]) {
      expect(
        () => PersonAvatarMetadata.fromJson(bad, avatarOwner),
        throwsFormatException,
      );
    }
  });
  test(
    'save delete and receipt carry a specific version and operation',
    () async {
      final methods = <String>[];
      final client = MockClient((r) async {
        methods.add(r.method);
        if (r.method == 'GET')
          return avatarWire(
            avatarResult(
              avatarOperation,
              version: 2,
              action: 'DELETE',
              present: false,
            ),
          );
        final body = jsonDecode(r.body) as Map<String, dynamic>;
        expect(body['operationId'], avatarOperation);
        expect(body['expectedVersion'], r.method == 'PUT' ? 0 : 1);
        if (r.method == 'PUT') {
          expect(body['contentType'], 'image/jpeg');
          expect(base64Decode(body['contentBase64'] as String), avatarJPEG());
        }
        return avatarWire(
          avatarResult(
            avatarOperation,
            version: r.method == 'PUT' ? 1 : 2,
            action: r.method == 'PUT' ? 'SAVE' : 'DELETE',
            present: r.method == 'PUT',
          ),
        );
      });
      addTearDown(client.close);
      final api = PersonAvatarAPI(
        client: client,
        baseUrl: 'http://local.test',
        authorizationHeader: () => 'Bearer A',
      );
      expect(
        (await api.change(
          avatarOwner,
          avatarOperation,
          0,
          bytes: avatarJPEG(),
        )).current.avatar?.version,
        1,
      );
      expect(
        (await api.change(avatarOwner, avatarOperation, 1)).current.avatar,
        isNull,
      );
      expect(
        (await api.operation(
          avatarOwner,
          avatarOperation,
          action: 'DELETE',
          expectedVersion: 1,
        )).appliedVersion,
        2,
      );
      expect(methods, ['PUT', 'DELETE', 'GET']);
    },
  );
  test(
    'content verifies bytes hash dimensions and late authorization',
    () async {
      final client = MockClient(
        (r) async => http.Response.bytes(avatarJPEG(), 200),
      );
      addTearDown(client.close);
      final api = PersonAvatarAPI(
        client: client,
        baseUrl: 'http://local.test',
        authorizationHeader: () => 'Bearer A',
      );
      expect(
        await api.content(
          PersonAvatarMetadata.fromJson(avatarMetadata(), avatarOwner),
        ),
        avatarJPEG(),
      );
      final bad = PersonAvatarMetadata.fromJson({
        ...avatarMetadata(),
        'sha256': 'a' * 64,
      }, avatarOwner);
      await expectLater(api.content(bad), throwsFormatException);
      var token = 'Bearer A';
      final pending = Completer<http.Response>();
      final late = MockClient((r) => pending.future);
      addTearDown(late.close);
      final future = PersonAvatarAPI(
        client: late,
        baseUrl: 'http://local.test',
        authorizationHeader: () => token,
      ).own(avatarOwner);
      final checked = expectLater(future, throwsStateError);
      await Future<void>.delayed(Duration.zero);
      token = 'Bearer B';
      pending.complete(avatarWire({'version': 0, 'avatar': null}));
      await checked;
    },
  );
  test('social DTOs carry only an avatar of the referenced person', () async {
    final metadata = avatarMetadata(owner: avatarPeer);
    final row = {
      'id': avatarOperation,
      'otherAccountId': avatarPeer,
      'otherName': '好友',
      'createdAt': '2026-10-13T00:00:00Z',
      'avatar': metadata,
    };
    expect(socialPageFriend(row).avatar?.accountID, avatarPeer);
    expect(
      HumanConversation.fromJson({...row, 'unreadCount': 0}).avatar?.version,
      1,
    );
    expect(
      () => FriendTie.fromJson({...row, 'avatar': avatarMetadata()}),
      throwsFormatException,
    );
    final client = MockClient(
      (r) async => avatarWire({
        'items': [
          {
            'id': avatarOperation,
            'target': {'targetType': 'PERSON', 'targetId': avatarPeer},
            'label': '好友',
            'createdAt': '2026-10-13T00:00:00Z',
            'avatar': metadata,
          },
        ],
        'nextCursor': null,
      }),
    );
    addTearDown(client.close);
    final source = ConnectionSource(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'http://local.test',
    );
    addTearDown(source.dispose);
    expect(
      (await NowSocialEdgeSource(
        source,
      ).followsPage()).items.single.avatar?.accountID,
      avatarPeer,
    );
  });
}
