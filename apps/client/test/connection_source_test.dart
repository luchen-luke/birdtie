import 'dart:convert';

import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_share_pending_store_test.dart';
import 'share_entity_to_chat_test.dart' show shareReceipt, shareJSON;
import 'connection_request_review_entry_test.dart' show reviewRequest, reviewJSON, reviewID;
import 'connection_request_review_controller_test.dart' show decisionWire;

void main() {
  test('原Request列表保留SCREEN及有效期；原决策实际小DTO', () async {
    final api = ConnectionSource(authorizationHeader: () => 'Bearer fixed-fixture',
      apiBaseUrl: 'https://original.fixture', client: MockClient((r) async {
        expect(r.headers['Authorization'], 'Bearer fixed-fixture');
        if (r.method == 'GET') return reviewJSON([reviewRequest()]);
        expect(r.url.path, '/v1/me/connection-requests/$reviewID/decision');
        return reviewJSON(decisionWire(reviewRequest(), 'accept'));
      }));
    final request = (await api.reviewRequests()).single;
    expect(request.pendingReview, true); expect(request.expiresAt, isNotNull);
    final receipt = await api.decideReviewed(request, 'accept');
    expect(receipt.id, reviewID); expect(receipt.state, 'accepted');
    expect(receipt.conversationID, ''); api.dispose();
  });
  for (final invalid in ['id', 'direction', 'scope', 'state', 'otherAccountId', 'policyDisposition', 'screeningStatus', 'expiresAt', 'createdAt', 'ownerId', 'confirmed']) {
    test('人工审阅拒绝非法列表字段 $invalid', () async {
      final row = reviewRequest()..[invalid] = 'invalid';
      final api = ConnectionSource(authorizationHeader: () => 'Bearer fixed-fixture',
        apiBaseUrl: 'https://original.fixture', client: MockClient((_) async => reviewJSON([row])));
      await expectLater(api.reviewRequests(), throwsFormatException); api.dispose();
    });
  }
  for (final invalid in [
    'operation',
    'conversation',
    'sender',
    'id',
    'speaker',
    'body',
    'kind',
    'target',
    'empty-title',
    'private-field',
    'removed-with-id',
    'removed-with-title',
    'bad-date',
    'missing-date',
    'offset-date',
  ]) {
    test('原操作回执拒绝错绑或泄漏：$invalid', () async {
      final receipt = shareReceipt(shareOperation);
      final message = receipt['message'] as Map<String, dynamic>;
      final entity = message['entity'] as Map<String, dynamic>;
      switch (invalid) {
        case 'operation':
          receipt['operationId'] = shareMessage;
        case 'conversation':
          message['conversationId'] = shareTarget;
        case 'sender':
          message['senderAccountId'] = sharePeer;
        case 'id':
          message['id'] = 'fake';
        case 'speaker':
          message['speakerKind'] = 'agent';
        case 'body':
          message['body'] = '客户端替换正文';
        case 'kind':
          entity['type'] = 'memory';
        case 'target':
          entity['id'] = sharePeer;
        case 'empty-title':
          entity['title'] = '';
        case 'private-field':
          entity['evidence'] = '私密资料';
        case 'removed-with-id':
          entity.remove('title');
          entity['available'] = false;
        case 'removed-with-title':
          entity.remove('id');
          entity['available'] = false;
        case 'bad-date':
          message['createdAt'] = '2026-02-31T09:00:00Z';
        case 'missing-date':
          message.remove('createdAt');
        case 'offset-date':
          message['createdAt'] = '2026-10-03T09:00:00+08:00';
      }
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer A',
        apiBaseUrl: 'https://api.example',
        client: MockClient((_) async => shareJSON(receipt)),
      );
      await expectLater(
        source.recoverEntityShare(pendingShare, shareOwner),
        throwsFormatException,
      );
      source.dispose();
    });
  }
  const personId = '11111111-1111-4111-8111-111111111111';
  test('好友申请显式声明 friend 且不提交城市；旧私信申请保留城市', () async {
    final bodies = <Map<String, dynamic>>[];
    final api = ConnectionSource(
      authorizationHeader: () => 'Bearer test-session',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        expect(request.url.path, '/v1/me/connection-requests');
        expect(request.headers['Authorization'], 'Bearer test-session');
        bodies.add(jsonDecode(request.body) as Map<String, dynamic>);
        return http.Response(jsonEncode({'data': {}}), 201);
      }),
    );
    await api.requestFriend(personId, '一起打球吗？');
    await api.request(personId, 'aberdeen-gb', '想聊聊活动');
    expect(bodies[0], {
      'recipientAccountId': personId,
      'scope': 'friend',
      'note': '一起打球吗？',
    });
    expect(bodies[0].containsKey('cityId'), isFalse);
    expect(bodies[1]['cityId'], 'aberdeen-gb');
    expect(bodies[1].containsKey('scope'), isFalse);
    api.dispose();
  });

  test('好友列表与申请范围来自鉴权接口', () async {
    final api = ConnectionSource(
      authorizationHeader: () => 'Bearer test-session',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        if (request.url.path == '/v1/me/ties') {
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': [
                  {
                    'id': 'tie-1',
                    'otherAccountId': personId,
                    'otherName': '小林',
                  },
                ],
              }),
            ),
            200,
          );
        }
        return http.Response(
          jsonEncode({
            'data': [
              {
                'id': 'request-1',
                'scope': 'friend',
                'direction': 'incoming',
                'state': 'pending',
              },
            ],
          }),
          200,
        );
      }),
    );
    expect((await api.ties()).single.otherName, '小林');
    expect((await api.requests()).single.scope, 'friend');
    api.dispose();
  });

  test('移除好友与屏蔽分别调用本人鉴权接口', () async {
    final paths = <String>[];
    final api = ConnectionSource(
      authorizationHeader: () => 'Bearer test-session',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        expect(request.headers['Authorization'], 'Bearer test-session');
        paths.add('${request.method} ${request.url.path}');
        if (request.method == 'POST') {
          expect(jsonDecode(request.body), {'accountId': personId});
        }
        return http.Response('', 204);
      }),
    );
    await api.removeTie('22222222-2222-4222-8222-222222222222');
    await api.blockAccount(personId);
    expect(paths, [
      'DELETE /v1/me/ties/22222222-2222-4222-8222-222222222222',
      'POST /v1/me/blocks',
    ]);
    api.dispose();
  });

  test('私信列表显示服务端未读数，已读标记携带实际消息 ID', () async {
    const conversationId = '33333333-3333-4333-8333-333333333333';
    const messageId = '44444444-4444-4444-8444-444444444444';
    final api = ConnectionSource(
      authorizationHeader: () => 'Bearer test-session',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        expect(request.headers['Authorization'], 'Bearer test-session');
        if (request.method == 'GET') {
          expect(request.url.path, '/v1/me/conversations');
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': [
                  {
                    'id': conversationId,
                    'otherAccountId': personId,
                    'otherName': '小林',
                    'unreadCount': 2,
                  },
                ],
              }),
            ),
            200,
          );
        }
        expect(request.url.path, '/v1/me/conversations/$conversationId/read');
        expect(jsonDecode(request.body), {'throughMessageId': messageId});
        return http.Response(jsonEncode({'data': {}}), 200);
      }),
    );
    expect((await api.conversations()).single.unreadCount, 2);
    await api.markRead(conversationId, messageId);
    api.dispose();
  });

  test('好友主动发起私信，消息来自服务端；发送失败不显示虚构消息', () async {
    const tieId = '55555555-5555-4555-8555-555555555555';
    const conversationId = '66666666-6666-4666-8666-666666666666';
    var offline = false;
    final messages = <Map<String, dynamic>>[];
    final api = ConnectionSource(
      authorizationHeader: () => 'Bearer test-session',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        expect(request.headers['Authorization'], 'Bearer test-session');
        if (request.url.path == '/v1/me/ties/$tieId/conversation') {
          expect(request.method, 'POST');
          return http.Response.bytes(
            utf8.encode(
              jsonEncode({
                'data': {
                  'id': conversationId,
                  'otherAccountId': personId,
                  'otherName': '小林',
                },
              }),
            ),
            200,
          );
        }
        if (request.method == 'POST') {
          expect(
            request.url.path,
            '/v1/me/conversations/$conversationId/messages',
          );
          if (offline) return http.Response('', 503);
          messages.add({
            'id': '77777777-7777-4777-8777-777777777777',
            'senderAccountId': personId,
            'body': (jsonDecode(request.body) as Map<String, dynamic>)['body'],
            'createdAt': '2026-10-01T10:00:00Z',
          });
          return http.Response.bytes(
            utf8.encode(jsonEncode({'data': messages.last})),
            201,
          );
        }
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': messages})),
          200,
        );
      }),
    );
    final chat = await api.startFriendChat(tieId);
    expect(chat.id, conversationId);
    expect(await api.messages(conversationId), isEmpty);
    await api.send(conversationId, '周末见');
    expect((await api.messages(conversationId)).single.body, '周末见');
    offline = true;
    await expectLater(api.send(conversationId, '失败消息'), throwsStateError);
    expect((await api.messages(conversationId)).length, 1);
    api.dispose();
  });

  test('结构化卡片发送稳定实体 ID，撤回后不展示旧标题和 ID', () async {
    const conversationId = '66666666-6666-4666-8666-666666666666';
    const placeId = '88888888-8888-4888-8888-888888888888';
    final api = ConnectionSource(
      authorizationHeader: () => 'Bearer test-session',
      apiBaseUrl: 'https://birdtie.example',
      client: MockClient((request) async {
        expect(request.headers['Authorization'], 'Bearer test-session');
        if (request.method == 'POST') {
          expect(jsonDecode(request.body), {
            'entity': {'type': 'place', 'id': placeId},
          });
          return http.Response('{}', 201);
        }
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': [
                {
                  'id': '77777777-7777-4777-8777-777777777777',
                  'senderAccountId': personId,
                  'body': '分享了一张卡片',
                  'createdAt': '2026-10-01T10:00:00Z',
                  'entity': {'type': 'place', 'available': false},
                },
              ],
            }),
          ),
          200,
        );
      }),
    );
    await api.sendEntity(conversationId, 'place', placeId);
    final message = (await api.messages(conversationId)).single;
    expect(message.entity?.available, false);
    expect(message.entity?.id, isNull);
    expect(message.entity?.title, isNull);
    expect(message.entity?.typeLabel, '地点');
    api.dispose();
  });
}
