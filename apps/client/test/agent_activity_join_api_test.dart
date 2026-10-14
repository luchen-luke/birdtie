import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_activity_join_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const joinOwner = '20000000-0000-4000-8000-000000000001';
const joinAgent = '20000000-0000-4000-8000-000000000002';
const joinTask = '20000000-0000-4000-8000-000000000003';
const joinActivity = '20000000-0000-4000-8000-000000000004';
const joinApproval = '20000000-0000-4000-8000-000000000005';
const joinLogical = '20000000-0000-4000-8000-000000000006';
const joinAction = '20000000-0000-4000-8000-000000000007';
const joinDispatch = '20000000-0000-4000-8000-000000000008';
const joinEffect = '20000000-0000-4000-8000-000000000009';
const joinParticipation = '20000000-0000-4000-8000-000000000010';
const joinOther = '20000000-0000-4000-8000-000000000099';
final joinAt = DateTime.utc(2026, 10, 13, 8);
final joinDigest = 'a' * 64;
final joinTurn = 'b' * 64;
AgentActivityJoinSelection joinSelection() => AgentActivityJoinSelection(
  taskID: joinTask,
  activityID: joinActivity,
  expectedTaskUpdatedAt: joinAt.subtract(const Duration(seconds: 1)),
  replyTurnDigest: joinTurn,
);
Map<String, dynamic> joinReviewWire() => {
  'schemaVersion': 'activity-join-review-v1',
  'activityId': joinActivity,
  'targetVersion': 'c' * 64,
  'title': '周末原生徒步活动',
  'summary': '原活动简介，须逐字展示。',
  'description': '请自备饮水。天气变更可能影响活动，出发前由本人查看通知。',
  'hostLabel': null,
  'organizer': {'type': 'ORGANIZATION', 'id': joinOther, 'name': '原主办组织'},
  'startsAt': joinAt.add(const Duration(days: 1)).toIso8601String(),
  'endsAt': joinAt.add(const Duration(days: 1, hours: 3)).toIso8601String(),
  'timeZone': 'Asia/Shanghai',
  'modality': 'in_person',
  'locationLabel': '原生公园东门',
  'physicalPlaceStatus': 'confirmed',
  'priceMinor': null,
  'currency': null,
  'eligibility': '需自行确认体力适合。',
  'visibility': 'public',
  'capacity': 20,
  'participantCount': 7,
};
Map<String, dynamic> joinPreviewWire({
  String state = agentActivityJoinPending,
  bool committed = false,
  bool includeReview = true,
  String logical = joinLogical,
  String action = joinAction,
}) => {
  'schemaVersion': agentActivityJoinSchema,
  'owner': {'type': 'PERSON', 'id': joinOwner},
  'agentId': joinAgent,
  'taskId': joinTask,
  'approvalId': joinApproval,
  'logicalOperationId': logical,
  'actionId': action,
  'activityId': joinActivity,
  'bindingDigest': joinDigest,
  'state': state,
  'observedAt': joinAt.toIso8601String(),
  'expiresAt': joinAt.add(const Duration(seconds: 30)).toIso8601String(),
  'dispatchCommitted': committed,
  'review': includeReview ? joinReviewWire() : null,
};
Map<String, dynamic> joinReceiptWire({String status = 'SUCCEEDED'}) => {
  'schemaVersion': agentActivityJoinSchema,
  'owner': {'type': 'PERSON', 'id': joinOwner},
  'agentId': joinAgent,
  'approvalId': joinApproval,
  'dispatchId': joinDispatch,
  'status': status,
  'committedAt': joinAt.toIso8601String(),
  if (status == 'SUCCEEDED') ...{
    'effectId': joinEffect,
    'participationId': joinParticipation,
    'appliedAt': joinAt.add(const Duration(seconds: 1)).toIso8601String(),
  },
};
http.Response joinResponse(Object? value, {int status = 200}) => http.Response(
  jsonEncode({'data': value}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
Map<String, dynamic> joinPreviewForRequest(http.Request r) {
  final body = jsonDecode(r.body) as Map<String, dynamic>;
  return joinPreviewWire(
    logical: body['logicalOperationId'],
    action: body['actionId'],
  );
}

void main() {
  test(
    'native preview sends only six displayed-source selectors and preserves all exact review content',
    () async {
      final client = MockClient((r) async {
        expect(r.method, 'POST');
        expect(r.url.path, '/v1/me/agent-activity-join/previews');
        expect(r.headers['Authorization'], 'Bearer fixture');
        expect(jsonDecode(r.body), {
          'taskId': joinTask,
          'logicalOperationId': joinLogical,
          'actionId': joinAction,
          'activityId': joinActivity,
          'expectedTaskUpdatedAt': joinSelection().expectedTaskUpdatedAt
              .toIso8601String(),
          'replyTurnDigest': joinTurn,
        });
        return joinResponse(joinPreviewWire(), status: 201);
      });
      final api = AgentActivityJoinAPI(
        client: client,
        apiBaseUrl: 'http://localhost:18112/',
      );
      final p = await api.preview(
        'Bearer fixture',
        joinOwner,
        joinSelection(),
        joinLogical,
        joinAction,
      );
      expect(p.review!.raw, joinReviewWire());
      expect(p.review!.summary, contains('逐字展示'));
      expect(p.review!.priceMinor, null);
      expect(p.review!.currency, null);
      expect(p.dispatchCommitted, false);
      expect(() => p.review!.raw['title'] = '篡改', throwsUnsupportedError);
      expect(
        () => (p.review!.raw['organizer'] as Map)['name'] = '篡改',
        throwsUnsupportedError,
      );
      expect(api.environment, 'http://localhost:18112');
      client.close();
    },
  );
  test(
    'confirm binds same native review and original approval; recovery never sends digest or body',
    () async {
      final seen = <String>[];
      final p = AgentActivityJoinPreview.read(joinPreviewWire(), joinOwner);
      final client = MockClient((r) async {
        seen.add(r.url.path);
        if (r.url.path.endsWith('/confirm')) {
          expect(jsonDecode(r.body), {'bindingDigest': joinDigest});
          return joinResponse(
            joinPreviewWire(state: agentActivityJoinApproved),
          );
        }
        expect(r.body, isEmpty);
        expect(r.headers.containsKey('Content-Type'), false);
        if (r.url.path.endsWith('/revoke'))
          return joinResponse(
            joinPreviewWire(state: 'CANCELLED', includeReview: false),
          );
        return joinResponse(joinReceiptWire());
      });
      final api = AgentActivityJoinAPI(
        client: client,
        apiBaseUrl: 'http://local',
      );
      expect(
        (await api.confirm('Bearer fixture', joinOwner, p)).state,
        agentActivityJoinApproved,
      );
      expect(
        (await api.reconcile(
          'Bearer fixture',
          joinOwner,
          joinApproval,
          joinAgent,
        )).participationID,
        joinParticipation,
      );
      expect(
        (await api.revoke(
          'Bearer fixture',
          joinOwner,
          joinApproval,
          joinAgent,
        )).review,
        null,
      );
      expect(seen, everyElement(contains('/approvals/$joinApproval/')));
      client.close();
    },
  );
  test(
    'closed preview and source rejection do not silently rebase or manufacture authority',
    () async {
      final malformed = <String, Map<String, dynamic>>{
        'different owner': joinPreviewWire()
          ..['owner'] = {'type': 'PERSON', 'id': joinOther},
        'extra permission': joinPreviewWire()..['approved'] = true,
        'missing nullable review': joinPreviewWire()..remove('review'),
        'fresh without review': joinPreviewWire(includeReview: false),
        'fresh already dispatched': joinPreviewWire(committed: true),
        'consumed without dispatch': joinPreviewWire(
          state: 'DISPATCH_COMMITTED',
          committed: false,
        ),
        'unbounded approval': joinPreviewWire()
          ..['expiresAt'] = joinAt
              .add(const Duration(seconds: 31))
              .toIso8601String(),
        'wrong calendar': joinPreviewWire()
          ..['observedAt'] = '2026-02-30T08:00:00Z',
      };
      for (final item in malformed.entries) {
        expect(
          () => AgentActivityJoinPreview.read(item.value, joinOwner),
          throwsFormatException,
          reason: item.key,
        );
      }
      final wrongSource = MockClient(
        (_) async =>
            http.Response('{"error":"agent_activity_join_changed"}', 409),
      );
      final api = AgentActivityJoinAPI(
        client: wrongSource,
        apiBaseUrl: 'http://local',
      );
      expect(
        api.preview(
          'Bearer fixture',
          joinOwner,
          joinSelection(),
          joinLogical,
          joinAction,
        ),
        throwsA(
          isA<AgentActivityJoinFailure>().having(
            (e) => e.status,
            'status',
            409,
          ),
        ),
      );
      wrongSource.close();
    },
  );
  test(
    'native review rejects hidden conditions, wrong currency, negative counts and raw permission fields',
    () {
      final mutations = <void Function(Map<String, dynamic>)>[
        (r) => r['description'] = '中' * 6000,
        (r) => r['title'] = '',
        (r) => r['currency'] = 'USD',
        (r) => r['priceMinor'] = -1,
        (r) => r['capacity'] = 0,
        (r) => r['participantCount'] = -1,
        (r) => r['visibility'] = 'private',
        (r) => r['eligibility'] = ['错误类型'],
        (r) => r['grantId'] = joinOther,
        (r) => r.remove('hostLabel'),
        (r) => r['organizer'] = {
          'type': 'ORGANIZATION',
          'id': joinOther,
          'name': '组织',
          'grant': true,
        },
      ];
      for (final mutate in mutations) {
        final review = joinReviewWire();
        mutate(review);
        expect(
          () => AgentActivityJoinReview.read(review, joinActivity),
          throwsFormatException,
        );
      }
      final long = joinReviewWire()..['description'] = '完整条款' * 1000;
      expect(
        AgentActivityJoinReview.read(long, joinActivity).description,
        long['description'],
      );
    },
  );
  test(
    'receipt requires independent causal effect and stable approval/agent, not current going',
    () {
      final success = AgentActivityJoinReceipt.read(
        joinReceiptWire(),
        joinOwner,
        joinApproval,
        agentID: joinAgent,
      );
      expect(success.participationID, isNot(joinActivity));
      expect(success.terminal, true);
      for (final value in [
        joinReceiptWire()..remove('effectId'),
        joinReceiptWire()..['activityId'] = joinActivity,
        joinReceiptWire()..['approvalId'] = joinOther,
        joinReceiptWire()..['agentId'] = joinOther,
        joinReceiptWire()
          ..['appliedAt'] = joinAt
              .subtract(const Duration(seconds: 1))
              .toIso8601String(),
        joinReceiptWire(status: 'NO_EFFECT')
          ..['participationId'] = joinParticipation,
        joinReceiptWire(status: 'going'),
      ]) {
        expect(
          () => AgentActivityJoinReceipt.read(
            value,
            joinOwner,
            joinApproval,
            agentID: joinAgent,
          ),
          throwsFormatException,
        );
      }
      expect(
        AgentActivityJoinReceipt.read(
          joinReceiptWire(status: 'NO_EFFECT'),
          joinOwner,
          joinApproval,
        ).terminal,
        true,
      );
    },
  );
  test(
    'duplicate escaped keys, oversized streaming reply, bad mime and changed confirmed review fail closed',
    () async {
      final p = AgentActivityJoinPreview.read(joinPreviewWire(), joinOwner);
      final wires = [
        jsonEncode({'data': joinPreviewWire()}).replaceFirst(
          '"bindingDigest":',
          '"bindingDigest":"${'d' * 64}","binding\\u0044igest":',
        ),
        jsonEncode({
          'data': joinPreviewWire(state: agentActivityJoinApproved)
            ..['review'] = (joinReviewWire()..['description'] = '悄悄换了条件'),
        }),
        ' ' * 32769,
      ];
      for (final wire in wires) {
        final client = MockClient(
          (_) async => http.Response(
            wire,
            200,
            headers: {'content-type': 'application/json'},
          ),
        );
        final api = AgentActivityJoinAPI(
          client: client,
          apiBaseUrl: 'http://local',
        );
        expect(
          api.confirm('Bearer fixture', joinOwner, p),
          throwsFormatException,
        );
        client.close();
      }
      final client = MockClient(
        (_) async =>
            http.Response(jsonEncode({'data': joinReceiptWire()}), 200),
      );
      final api = AgentActivityJoinAPI(
        client: client,
        apiBaseUrl: 'http://local',
      );
      expect(
        api.reconcile('Bearer fixture', joinOwner, joinApproval, joinAgent),
        throwsFormatException,
      );
      client.close();
    },
  );
}
