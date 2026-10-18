import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/moment_interaction.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'moment_interaction_test.dart' show MemoryPendingStore;

const _owner = '11111111-1111-4111-8111-111111111111';
const _moment = '22222222-2222-4222-8222-222222222222';
const _parent = '44444444-4444-4444-8444-444444444444';
const _child = '55555555-5555-4555-8555-555555555555';
Map<String, dynamic> _comment() => {
  'id': _parent,
  'body': '原评论正文',
  'createdAt': DateTime.now().toUtc().toIso8601String(),
  'authorId': null,
  'authorPublicProfile': null,
  'canDelete': false,
  'version': 1,
  'canReply': true,
  'replyTo': null,
};
Map<String, dynamic> _page() {
  final now = DateTime.now().toUtc();
  return {
    'schemaVersion': 'moment-interaction-page-v2',
    'momentId': _moment,
    'momentRevision': 2,
    'canComment': true,
    'likeCount': 0,
    'commentCount': 1,
    'liked': false,
    'likeVersion': 0,
    'comments': [_comment()],
    'nextCursor': null,
    'observedAt': now.toIso8601String(),
    'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
    'sourceVersion': 'a' * 64,
  };
}

http.Response _response(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'Content-Type': 'application/json'},
);
void main() {
  test(
    'reply DTO preserves version and rejects unavailable parent content',
    () {
      final c = MomentComment.decode({
        ..._comment(),
        'id': _child,
        'replyTo': {
          'commentId': _parent,
          'version': 1,
          'status': 'UNAVAILABLE',
        },
      });
      expect(c.version, 1);
      expect(c.replyTo!.available, false);
      expect(c.replyTo!.body, isNull);
      expect(
        () => MomentComment.decode({
          ..._comment(),
          'replyTo': {
            'commentId': _parent,
            'version': 1,
            'status': 'UNAVAILABLE',
            'body': 'forbidden',
          },
        }),
        throwsFormatException,
      );
      final input = {
        'operationId': _child,
        'action': 'COMMENT',
        'momentId': _moment,
        'expectedMomentRevision': 2,
        'expectedLikeVersion': 0,
        'commentId': '',
        'body': 'reply',
      };
      final top = socialOperationDigest('MOMENT_INTERACTION', input);
      final reply = {
        ...input,
        'parentCommentId': _parent,
        'expectedParentCommentVersion': 1,
      };
      expect(socialOperationDigest('MOMENT_INTERACTION', reply), isNot(top));
      expect(
        socialOperationDigest('MOMENT_INTERACTION', {
          ...reply,
          'expectedParentCommentVersion': 2,
        }),
        isNot(socialOperationDigest('MOMENT_INTERACTION', reply)),
      );
      final legacy = {..._page(), 'schemaVersion': 'moment-interaction-page-v1'}
        ..remove('canComment');
      expect(MomentInteractionPage.decode(legacy, _moment).canComment, false);
    },
  );
  testWidgets(
    'external composer list has no input and reply UNKNOWN recovers only original operation',
    (t) async {
      final store = MemoryPendingStore();
      Map<String, dynamic>? posted;
      var puts = 0, gets = 0;
      MomentComment? target;
      BuildContext? context;
      final client = MockClient((r) async {
        if (r.method == 'GET' && r.url.path.endsWith('/interactions'))
          return _response(_page());
        if (r.method == 'POST' && r.url.path.endsWith('/previews')) {
          posted = jsonDecode(r.body) as Map<String, dynamic>;
          final now = DateTime.now().toUtc();
          return _response({
            'operationId': posted!['operationId'],
            'action': 'COMMENT',
            'payloadDigest': socialOperationDigest(
              'MOMENT_INTERACTION',
              posted!,
            ),
            'observedAt': now.toIso8601String(),
            'expiresAt': now.add(const Duration(seconds: 30)).toIso8601String(),
            'approval': 'synthetic-approval',
            'input': posted,
            'sourceVersion': 'a' * 64,
          });
        }
        if (r.method == 'PUT') {
          puts++;
          return http.Response('{}', 500);
        }
        if (r.method == 'GET' && r.url.path.contains('/operations/')) {
          gets++;
          return _response({
            'operationId': posted!['operationId'],
            'status': 'SUCCEEDED',
            'action': 'COMMENT',
            'momentId': _moment,
            'commentId': _child,
            'appliedVersion': 1,
            'payloadDigest': socialOperationDigest(
              'MOMENT_INTERACTION',
              posted!,
            ),
            'committedAt': DateTime.now().toUtc().toIso8601String(),
          });
        }
        return http.Response('{}', 404);
      });
      final c = MomentInteractionController(
        momentID: _moment,
        authorizationHeader: () => 'Bearer synthetic',
        ownerID: () => _owner,
        apiBaseUrl: 'https://reply.test',
        client: client,
        current: () => true,
        pendingStore: store,
      );
      try {
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: Builder(
                builder: (b) {
                  context = b;
                  return MomentInteractionPanel(
                    momentID: _moment,
                    authorizationHeader: () => 'Bearer synthetic',
                    ownerID: () => _owner,
                    apiBaseUrl: 'https://reply.test',
                    client: client,
                    current: () => true,
                    controller: c,
                    showComposer: false,
                    onReply: (v) => target = v,
                  );
                },
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(c.canComment, true);
        expect(find.byType(TextField), findsNothing);
        await t.tap(find.text('回复'));
        await t.pumpAndSettle();
        expect(target?.id, _parent);
        final done = c.publish(context!, body: '明确发布此回复', replyTo: target);
        await t.pumpAndSettle();
        expect(find.text('发布回复'), findsOneWidget);
        await t.tap(find.widgetWithText(FilledButton, '确认'));
        await t.pumpAndSettle();
        await done;
        expect(posted!['parentCommentId'], _parent);
        expect(posted!['expectedParentCommentVersion'], 1);
        expect(puts, 1);
        expect(c.operations.pending?.stage, 'UNKNOWN');
        expect(store.value, isNotNull);
        final recovered = await c.operations.recover();
        await t.pumpAndSettle();
        expect(recovered?.commentID, _child);
        expect(puts, 1);
        expect(gets, 1);
        expect(store.value, isNull);
        expect(find.byType(TextField), findsNothing);
      } finally {
        await t.pumpWidget(const SizedBox());
        c.dispose();
        client.close();
      }
    },
  );
  testWidgets(
    'deleted reply target cannot silently become a top level comment',
    (t) async {
      var reads = 0, writes = 0;
      final client = MockClient((r) async {
        if (r.method == 'GET') {
          reads++;
          return _response(
            reads == 1
                ? _page()
                : {..._page(), 'comments': <Object>[], 'commentCount': 0},
          );
        }
        writes++;
        return http.Response('{}', 500);
      });
      final c = MomentInteractionController(
        momentID: _moment,
        authorizationHeader: () => 'Bearer synthetic',
        ownerID: () => _owner,
        apiBaseUrl: 'https://reply.test',
        client: client,
        current: () => true,
        pendingStore: MemoryPendingStore(),
      );
      try {
        await t.pumpWidget(
          MaterialApp(
            home: Builder(
              builder: (context) =>
                  TextButton(onPressed: () {}, child: const Text('host')),
            ),
          ),
        );
        await c.load();
        await t.pumpAndSettle();
        final original = c.comments.single;
        await c.load();
        await t.pumpAndSettle();
        final context = t.element(find.text('host'));
        expect(
          await c.publish(context, body: '保留文字', replyTo: original),
          isNull,
        );
        expect(writes, 0);
        expect(c.operations.pending, isNull);
      } finally {
        await t.pumpWidget(const SizedBox());
        c.dispose();
        client.close();
      }
    },
  );
}
