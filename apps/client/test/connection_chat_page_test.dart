import 'friend_image_test.dart' show imageFixtureBytes, imageMetadata;
import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'friend_audio_test.dart' show audioFixture;

const pageOwner = '11111111-1111-4111-8111-111111111111';
const pagePeer = '22222222-2222-4222-8222-222222222222';
const pageConversation = '33333333-3333-4333-8333-333333333333';

String pageMessageID(int number) =>
    '55555555-5555-4555-8555-${number.toString().padLeft(12, '0')}';

Map<String, Object> pageMessage(int number) => {
  'id': pageMessageID(number),
  'conversationId': pageConversation,
  'senderAccountId': pagePeer,
  'speakerKind': 'human',
  'body': '合成分页消息 $number',
  'createdAt': DateTime.utc(
    2026,
    10,
    12,
  ).add(Duration(seconds: number)).toIso8601String(),
};

http.Response chatPageWire(
  List<Object> items, {
  String? nextCursor,
  int? unreadCount,
}) => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': {
        'items': items,
        'nextCursor': nextCursor,
        if (unreadCount != null) 'unreadCount': unreadCount,
      },
    }),
  ),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

Map<String, Object> _conversation() => {
  'id': pageConversation,
  'otherAccountId': pagePeer,
  'otherName': '合成联系人',
  'createdAt': '2026-10-12T00:00:00Z',
  'unreadCount': 3,
  'lastMessagePreview': '一行合成预览',
};

void main() {
  test(
    'current conversation page accepts real message time but never invents one',
    () async {
      final client = MockClient(
        (_) async => chatPageWire([
          {..._conversation(), 'lastMessageAt': '2026-10-13T00:12:34.123456Z'},
        ], unreadCount: 3),
      );
      final source = ConnectionSource(
        client: client,
        apiBaseUrl: 'https://unit.example',
        authorizationHeader: () => 'Bearer synthetic',
      );
      addTearDown(source.dispose);
      addTearDown(client.close);
      final page = await source.conversationsPage();
      expect(
        page.items.single.lastMessageAt,
        DateTime.utc(2026, 10, 13, 0, 12, 34, 123, 456),
      );
      final empty = HumanConversationListPage.fromJson({
        'items': [_conversation()],
        'unreadCount': 3,
        'nextCursor': null,
      });
      expect(empty.items.single.lastMessageAt, isNull);
      for (final malformed in ['2026-02-31T00:00:00Z', 'tomorrow', 42, {}]) {
        expect(
          () => HumanConversationListPage.fromJson({
            'items': [
              {..._conversation(), 'lastMessageAt': malformed},
            ],
            'unreadCount': 3,
            'nextCursor': null,
          }),
          throwsFormatException,
        );
      }
      expect(
        () => HumanConversationListPage.fromJson({
          'items': [
            {..._conversation(), 'unknownTime': '2026-10-13T00:00:00Z'},
          ],
          'unreadCount': 3,
          'nextCursor': null,
        }),
        throwsFormatException,
      );
    },
  );

  test(
    'page accepts strict image metadata and rejects image plus audio/entity',
    () async {
      final metadata = imageMetadata(await imageFixtureBytes()).toJson();
      final value = {...pageMessage(1), 'image': metadata};
      final client = MockClient((_) async => chatPageWire([value]));
      final source = ConnectionSource(
        client: client,
        apiBaseUrl: 'https://unit.example',
        authorizationHeader: () => 'Bearer synthetic',
      );
      addTearDown(source.dispose);
      addTearDown(client.close);
      expect(
        (await source.messagesPage(pageConversation)).items.single.image,
        isNotNull,
      );
      for (final extra in [
        {'audio': audioFixture().metadata.toJson()},
        {
          'entity': {'type': 'place', 'available': false},
        },
      ]) {
        expect(
          () => HumanMessage.fromJson({...value, ...extra}),
          throwsFormatException,
        );
      }
      expect(
        () => HumanMessage.fromJson({
          ...value,
          'image': {...metadata, 'url': 'outside'},
        }),
        throwsFormatException,
      );
    },
  );

  test(
    'recent page uses its real endpoint and all-visible unread total',
    () async {
      final calls = <http.Request>[];
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer synthetic-a',
        apiBaseUrl: 'https://unit.example',
        client: MockClient((r) async {
          calls.add(r);
          return chatPageWire(
            [_conversation()],
            nextCursor: r.url.queryParameters['cursor'] == null
                ? 'next_page'
                : null,
            unreadCount: 207,
          );
        }),
      );
      addTearDown(source.dispose);
      final page = await source.conversationsPage();
      expect(calls.single.method, 'GET');
      expect(calls.single.url.path, '/v1/me/conversations/page');
      expect(calls.single.url.queryParameters, {'limit': '20'});
      expect(page.unreadCount, 207);
      expect(page.items.single.unreadCount, 3);
      expect(page.nextCursor, 'next_page');
      await source.conversationsPage(cursor: page.nextCursor, limit: 4);
      expect(calls.last.url.queryParameters, {
        'limit': '4',
        'cursor': 'next_page',
      });
    },
  );

  test(
    'history page preserves server chronological order and does not ACK',
    () async {
      final calls = <http.Request>[];
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer synthetic-a',
        apiBaseUrl: 'https://unit.example',
        client: MockClient((r) async {
          calls.add(r);
          return chatPageWire([
            pageMessage(4),
            pageMessage(5),
          ], nextCursor: 'older_3');
        }),
      );
      addTearDown(source.dispose);
      final page = await source.messagesPage(pageConversation);
      expect(calls.single.method, 'GET');
      expect(
        calls.single.url.path,
        '/v1/me/conversations/$pageConversation/messages/page',
      );
      expect(page.items.map((m) => m.id), [pageMessageID(4), pageMessageID(5)]);
      expect(page.nextCursor, 'older_3');
      expect(calls, hasLength(1));
    },
  );

  for (final status in [401, 403, 404, 503]) {
    test(
      'HTTP $status keeps typed access/service failure without list fallback',
      () async {
        var calls = 0;
        final source = ConnectionSource(
          authorizationHeader: () => 'Bearer synthetic-a',
          client: MockClient((r) async {
            calls++;
            return http.Response('{}', status);
          }),
        );
        addTearDown(source.dispose);
        await expectLater(
          source.messagesPage(pageConversation),
          throwsA(
            isA<ConnectionTransportFailure>().having(
              (e) => e.status,
              'status',
              status,
            ),
          ),
        );
        expect(calls, 1);
      },
    );
  }

  test(
    'logout, another account and ABA invalidate late successful private pages',
    () async {
      for (final replacement in ['logout', 'other', 'ABA', 'dispose']) {
        var token = 'Bearer synthetic-a';
        final changes = ValueNotifier(0), delayed = Completer<http.Response>();
        final source = ConnectionSource(
          authorizationHeader: () => token,
          identityChanges: changes,
          client: MockClient((_) => delayed.future),
        );
        final loading = source.messagesPage(pageConversation);
        await Future<void>.delayed(Duration.zero);
        if (replacement == 'dispose') {
          source.dispose();
        } else {
          token = replacement == 'logout' ? '' : 'Bearer synthetic-b';
          changes.value++;
          if (replacement == 'ABA') {
            token = 'Bearer synthetic-a';
            changes.value++;
          }
        }
        delayed.complete(chatPageWire([pageMessage(1)]));
        await expectLater(loading, throwsA(isA<http.ClientException>()));
        source.dispose();
        changes.dispose();
      }
    },
  );

  for (final mutation in [
    'oversize',
    'duplicate',
    'reversed',
    'foreign',
    'unknown',
    'speaker',
    'timestamp',
    'cursor-loop',
    'empty-with-cursor',
    'cursor-url',
  ]) {
    test(
      'rejects malformed page $mutation instead of mixing private rows',
      () async {
        final rows = <Object>[pageMessage(1), pageMessage(2)];
        String? next;
        switch (mutation) {
          case 'oversize':
            rows.addAll([pageMessage(3)]);
          case 'duplicate':
            rows[1] = pageMessage(1);
          case 'reversed':
            rows.setAll(0, [pageMessage(2), pageMessage(1)]);
          case 'foreign':
            (rows.first as Map)['conversationId'] = pageOwner;
          case 'unknown':
            (rows.first as Map)['accessToken'] = 'synthetic-bogus';
          case 'speaker':
            (rows.first as Map)['speakerKind'] = 'agent';
          case 'timestamp':
            (rows.first as Map)['createdAt'] = '2026-02-30T00:00:00Z';
          case 'cursor-loop':
            next = 'same_cursor';
          case 'empty-with-cursor':
            rows.clear();
            next = 'older';
          case 'cursor-url':
            next = 'https://outside.example/path';
        }
        final source = ConnectionSource(
          authorizationHeader: () => 'Bearer synthetic-a',
          client: MockClient((_) async => chatPageWire(rows, nextCursor: next)),
        );
        addTearDown(source.dispose);
        await expectLater(
          source.messagesPage(
            pageConversation,
            limit: 2,
            cursor: mutation == 'cursor-loop' ? 'same_cursor' : null,
          ),
          throwsFormatException,
        );
      },
    );
  }

  test(
    'page factory rejects guessed unread, unknown fields and duplicate conversations',
    () {
      for (final value in [
        {
          'items': [_conversation()],
          'nextCursor': null,
          'unreadCount': true,
        },
        {
          'items': [_conversation()],
          'nextCursor': null,
          'unreadCount': -1,
        },
        {
          'items': [_conversation()],
          'nextCursor': null,
          'unreadCount': 0,
          'owner': pageOwner,
        },
        {
          'items': [_conversation(), _conversation()],
          'nextCursor': null,
          'unreadCount': 0,
        },
      ]) {
        expect(
          () => HumanConversationListPage.fromJson(value),
          throwsFormatException,
        );
      }
    },
  );

  test('invalid input and guest never request a private page', () async {
    var calls = 0;
    String? token = 'Bearer synthetic-a';
    final source = ConnectionSource(
      authorizationHeader: () => token,
      client: MockClient((_) async {
        calls++;
        return chatPageWire([]);
      }),
    );
    addTearDown(source.dispose);
    for (final limit in [0, -1, 51]) {
      await expectLater(
        source.messagesPage(pageConversation, limit: limit),
        throwsFormatException,
      );
    }
    await expectLater(source.messagesPage('not-a-uuid'), throwsFormatException);
    await expectLater(
      source.messagesPage('00000000-0000-0000-0000-000000000000'),
      throwsFormatException,
    );
    await expectLater(
      source.messagesPage(pageConversation, cursor: 'https://outside.example'),
      throwsFormatException,
    );
    token = null;
    await expectLater(source.messagesPage(pageConversation), throwsStateError);
    expect(calls, 0);
  });

  test(
    'unknown withdrawn entity stays unavailable without leaking its old ID or title',
    () async {
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer synthetic-a',
        client: MockClient(
          (r) async => chatPageWire([
            pageMessage(1)
              ..['entity'] = {'type': 'future_entity', 'available': false},
          ]),
        ),
      );
      addTearDown(source.dispose);
      final message = (await source.messagesPage(
        pageConversation,
      )).items.single;
      expect(message.entity!.available, isFalse);
      expect(message.entity!.id, isNull);
      expect(message.entity!.title, isNull);
    },
  );

  test(
    'retains real audio metadata in history without requesting private bytes',
    () async {
      final requests = <http.Request>[];
      final metadata = audioFixture().metadata;
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer synthetic-a',
        client: MockClient((r) async {
          requests.add(r);
          return chatPageWire([pageMessage(1)..['audio'] = metadata.toJson()]);
        }),
      );
      addTearDown(source.dispose);
      final message = (await source.messagesPage(
        pageConversation,
      )).items.single;
      expect(message.audio!.same(metadata), isTrue);
      expect(requests, hasLength(1));
      expect(requests.single.url.path.endsWith('/messages/page'), isTrue);
    },
  );

  test(
    'unavailable entity cannot smuggle a private title into a successful page',
    () async {
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer synthetic-a',
        client: MockClient(
          (r) async => chatPageWire([
            pageMessage(1)
              ..['entity'] = {
                'type': 'moment',
                'available': false,
                'id': pageOwner,
                'title': '合成撤权标题',
              },
          ]),
        ),
      );
      addTearDown(source.dispose);
      await expectLater(
        source.messagesPage(pageConversation),
        throwsFormatException,
      );
    },
  );

  test('original list endpoint and DTO stay compatible', () async {
    final calls = <String>[];
    final source = ConnectionSource(
      authorizationHeader: () => 'Bearer synthetic-a',
      client: MockClient((r) async {
        calls.add(r.url.path);
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': [_conversation()],
            }),
          ),
          200,
        );
      }),
    );
    addTearDown(source.dispose);
    final original = await source.conversations();
    expect(original.single.id, pageConversation);
    expect(calls, ['/v1/me/conversations']);
  });

  test(
    'direct conversation GET reaches old conversations without bounded-list lookup',
    () async {
      final requests = <http.Request>[];
      final source = ConnectionSource(
        authorizationHeader: () => 'Bearer synthetic-a',
        apiBaseUrl: 'https://unit.example',
        client: MockClient((r) async {
          requests.add(r);
          return http.Response.bytes(
            utf8.encode(jsonEncode({'data': _conversation()})),
            200,
          );
        }),
      );
      addTearDown(source.dispose);
      final item = await source.conversation(pageConversation);
      expect(item.id, pageConversation);
      expect(requests.single.method, 'GET');
      expect(
        requests.single.url.path,
        '/v1/me/conversations/$pageConversation',
      );
      expect(requests.single.url.hasQuery, isFalse);
    },
  );

  test(
    'direct conversation GET rejects a different object and revoked access',
    () async {
      for (final status in [200, 403, 404]) {
        final source = ConnectionSource(
          authorizationHeader: () => 'Bearer synthetic-a',
          client: MockClient(
            (r) async => http.Response.bytes(
              utf8.encode(
                jsonEncode({'data': _conversation()..['id'] = pageOwner}),
              ),
              status,
            ),
          ),
        );
        addTearDown(source.dispose);
        await expectLater(
          source.conversation(pageConversation),
          status == 200
              ? throwsFormatException
              : throwsA(isA<ConnectionTransportFailure>()),
        );
      }
    },
  );
}
