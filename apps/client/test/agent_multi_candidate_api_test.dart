import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_api.dart';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_api.dart';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_pending_store.dart';
import 'agent_multi_candidate_pending_store_test.dart' show pendingMulti;
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_memory_candidate_api_test.dart';

const multiTask = '77777777-7777-4777-8777-777777777777';
const multiContext = '88888888-8888-4888-8888-888888888888';
const multiPreviewID = '99999999-9999-4999-8999-999999999999';
const multiGrantID = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';
const multiEvent = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';
const multiLogical = 'cccccccc-cccc-4ccc-8ccc-cccccccccccc';
String sourcePreviewID(int index) =>
    '00000001-0000-4000-8000-${index.toString().padLeft(12, '0')}';
String sourceGrantID(int index) =>
    '00000002-0000-4000-8000-${index.toString().padLeft(12, '0')}';
dynamic multiClone(dynamic v) => jsonDecode(jsonEncode(v));

/// Synthetic wire fixtures only; actual native SQL acceptance has separate logs.
class MultiWireFixture {
  final now = DateTime.now().toUtc();
  final List<Map<String, dynamic>> requests = [];
  final Map<String, Map<String, dynamic>> previews = {}, grants = {};
  bool loseApproval = false,
      loseStage = false,
      loseRevoke = false,
      staged = false;
  bool rejectPreview = false;
  String title = '周末打羽毛球';
  String category = 'badminton';
  String algorithm = 'moment-lexical-category-v1';
  String? currentQuery;
  Map<String, dynamic> task() => {
    'id': multiTask,
    'principalType': 'person',
    'principalId': candidateOwner,
    'actingUserId': candidateOwner,
    'contextType': 'CITY',
    'contextId': multiContext,
    'cityContext': multiContext,
    'status': 'ACTIVE',
    'query': '整理我的羽毛球活动偏好',
    if (currentQuery != null) 'filters': {'currentQuery': currentQuery},
    'updatedAt': now.toIso8601String(),
  };
  Map<String, dynamic> moment(int index) => {
    'id': index == 1 ? candidateMoment : candidateSaved,
    'authorAccountId': candidateOwner,
    'status': 'draft',
    'visibility': 'private',
    'revision': index,
    'title': title,
    'body': '想找附近的羽毛球活动',
    'activityIds': <String>[],
    'updatedAt': now.toIso8601String(),
  };
  Map<String, dynamic> analysisSelection(int index) => {
    'taskId': multiTask,
    'momentId': moment(index)['id'],
    'momentRevision': index,
    'fields': ['title'],
    'deadlineAt': now.add(const Duration(minutes: 10)).toIso8601String(),
  };
  Map<String, dynamic> analysisPreview(
    int index, [
    Map<String, dynamic>? selection,
  ]) => {
    'schemaVersion': 'agent-enrichment-purpose-v1',
    'state': 'CURRENT_REVIEW',
    'id': sourcePreviewID(index),
    'purpose': 'MOMENT_LOCAL_ANALYSIS',
    'owner': {'type': 'person', 'id': candidateOwner},
    'agentId': candidateAgent,
    'selection': selection ?? analysisSelection(index),
    'review': {
      'taskQuery': currentQuery == null || currentQuery == ''
          ? task()['query']
          : currentQuery,
      'taskUpdatedAt': task()['updatedAt'],
      'content': {
        for (final field
            in (selection ?? analysisSelection(index))['fields'] as List)
          field: moment(index)[field],
      },
    },
    'observedAt': now.toIso8601String(),
    'expiresAt': now.add(const Duration(minutes: 5)).toIso8601String(),
    'explanation': '只分析本次选中的内容',
    'modelAccess': false,
    'candidateRetentionAllowed': false,
  };
  Map<String, dynamic> grant(Map<String, dynamic> preview, String id) => {
    'schemaVersion': preview['schemaVersion'],
    'id': id,
    'previewId': preview['id'],
    'purpose': preview['purpose'],
    'owner': preview['owner'],
    'agentId': preview['agentId'],
    'selection': preview['selection'],
    'revision': 1,
    'createdAt': now.toIso8601String(),
    'observedAt': now.toIso8601String(),
    'expiresAt': preview['expiresAt'],
    'modelAccess': false,
    if (preview['purpose'] == 'MOMENT_LOCAL_ANALYSIS')
      'candidateRetentionAllowed': false,
    if (preview['purpose'] == 'STAGE_MEMORY_CANDIDATE_MULTI') ...{
      'memoryPromotionAllowed': false,
      'candidateWriteAvailable': false,
    },
  };
  Map<String, dynamic> source(int index) => {
    'selector': {'type': 'MOMENT', 'id': moment(index)['id']},
    'version': {'kind': 'REVISION', 'revision': index},
    'eventTime': now.toIso8601String(),
    'fingerprint': 'a' * 64,
    'anchors': ['MOMENT:${moment(index)['id']}'],
  };
  Map<String, dynamic> multiSelection() => {
    'analysisGrantIds': [sourceGrantID(1), sourceGrantID(2)],
    'retainUntil': now.add(const Duration(minutes: 5)).toIso8601String(),
  };
  Map<String, dynamic> multiPreview([Map<String, dynamic>? selection]) => {
    'schemaVersion': 'agent-multi-candidate-v1',
    'state': 'CURRENT_REVIEW',
    'id': multiPreviewID,
    'purpose': 'STAGE_MEMORY_CANDIDATE_MULTI',
    'owner': {'type': 'person', 'id': candidateOwner},
    'agentId': candidateAgent,
    'selection': selection ?? multiSelection(),
    'review': {
      'proposal': {
        'algorithmVersion': algorithm,
        'predicate': 'ACTIVITY_CATEGORY',
        'category': category,
        'assessment': {'semantics': 'ORDINAL', 'level': 'LOW'},
        'explanation': '关键词仅是较弱建议',
      },
      'sources': [source(1), source(2)],
      'sourceSelections': [
        for (final index in [1, 2])
          {
            'analysisGrantId': sourceGrantID(index),
            'analysisPreviewId': sourcePreviewID(index),
            'source': source(index),
            'selectedFields':
                (grants[sourceGrantID(index)]?['selection'] ??
                analysisSelection(index))['fields'],
          },
      ],
      'anchorSource': source(1),
      'anchorEventId': multiEvent,
      'logicalOperationId': multiLogical,
      'taskId': multiTask,
      'clusters': 2,
      'retainUntil': now.add(const Duration(seconds: 90)).toIso8601String(),
    },
    'observedAt': now.toIso8601String(),
    'expiresAt': now.add(const Duration(seconds: 90)).toIso8601String(),
    'explanation': '两条明确允许的本地来源',
    'modelAccess': false,
    'memoryPromotionAllowed': false,
    'candidateWriteAvailable': false,
  };
  Map<String, dynamic> receipt({bool withCandidate = true}) => {
    'schemaVersion': 'agent-multi-candidate-v1',
    'state': staged ? 'CANDIDATE_STAGED' : 'NOT_STAGED',
    'retentionGrantId': multiGrantID,
    'owner': {'type': 'person', 'id': candidateOwner},
    'agentId': candidateAgent,
    'committed': staged,
    'observedAt': now.toIso8601String(),
    'explanation': staged ? '候选已提交' : '尚未提交',
    'modelAccess': false,
    'memoryPromotionAllowed': false,
    if (staged) ...{
      'eventId': multiEvent,
      'logicalOperationId': multiLogical,
      'effectKey': 'a' * 64,
      'handlerVersion': 'mom-candidate-multi-v1',
      'candidateId': candidateTarget,
      if (withCandidate)
        'candidate': {
          ...candidateWire(),
          'category': category,
          'sources': [source(1), source(2)],
          'createdAt': now.toIso8601String(),
          'updatedAt': now.toIso8601String(),
          'validUntil': now.add(const Duration(seconds: 90)).toIso8601String(),
        },
    },
  };
  Map<String, dynamic> previewReceipt(Map<String, dynamic> p, {String? state}) {
    final at = DateTime.now().toUtc();
    final end = DateTime.parse(p['expiresAt'] as String);
    final recorded = p['state'] == 'RECEIPT_ONLY';
    return {
      'schemaVersion': p['purpose'] == 'MOMENT_LOCAL_ANALYSIS'
          ? 'agent-enrichment-purpose-preview-receipt-v1'
          : 'agent-multi-candidate-preview-receipt-v1',
      'previewId': p['id'],
      'purpose': p['purpose'],
      'owner': p['owner'],
      'agentId': p['agentId'],
      'state':
          state ??
          (recorded
              ? 'APPROVAL_RECORDED'
              : end.isAfter(at)
              ? 'OPEN_UNCONSUMED'
              : 'CLOSED_UNCONSUMED'),
      if (recorded) 'consumedGrantId': p['consumedGrantId'],
      'previewObservedAt': p['observedAt'],
      'previewExpiresAt': p['expiresAt'],
      'observedAt': at.toIso8601String(),
      'validUntil': at.add(const Duration(seconds: 30)).toIso8601String(),
      'modelAccess': false,
      'candidateWriteAvailable': false,
      'memoryPromotionAllowed': false,
    };
  }

  Future<http.Response> handle(http.Request request) async {
    requests.add({
      'method': request.method,
      'path': request.url.path,
      'body': request.body,
    });
    final path = request.url.path;
    if (path == '/v1/me/agent-tasks') return candidateResponse([task()]);
    if (path == '/v1/me/moments') {
      return candidateResponse([moment(1), moment(2)]);
    }
    if (path == '$analysisPurposePath/previews' ||
        path == '$multiCandidatePath/previews') {
      if (rejectPreview) return http.Response('{}', 503);
      final selection = Map<String, dynamic>.from(
        jsonDecode(request.body) as Map,
      );
      final index = selection['momentId'] == candidateMoment ? 1 : 2;
      final p = path.startsWith(analysisPurposePath)
          ? analysisPreview(index, selection)
          : multiPreview(selection);
      previews[p['id'] as String] = multiClone(p);
      return candidateResponse(p);
    }
    if (path.endsWith('/approve')) {
      final id = path.split('/')[5], p = previews[id]!;
      final grantID = id == multiPreviewID
          ? multiGrantID
          : id == sourcePreviewID(1)
          ? sourceGrantID(1)
          : sourceGrantID(2);
      final g = grant(p, grantID);
      grants[grantID] = g;
      previews[id] = {
        ...p,
        'state': 'RECEIPT_ONLY',
        'consumedGrantId': grantID,
        'review': id == multiPreviewID
            ? null
            : {
                'taskQuery': '',
                'taskUpdatedAt': '0001-01-01T00:00:00Z',
                'content': <String, String>{},
              },
      };
      if (loseApproval) {
        loseApproval = false;
        throw http.ClientException('synthetic response lost');
      }
      return candidateResponse(g);
    }
    if (path.contains('/previews/') && path.endsWith('/receipt')) {
      return candidateResponse(previewReceipt(previews[path.split('/')[5]]!));
    }
    if (path.endsWith('/receipt')) return candidateResponse(receipt());
    if (path == '$multiCandidatePath/stage') {
      staged = true;
      if (loseStage) {
        loseStage = false;
        throw http.ClientException('synthetic response lost');
      }
      return candidateResponse(receipt());
    }
    if (path.contains('/previews/')) {
      return candidateResponse(previews[path.split('/').last]);
    }
    if (path.contains('/grants/')) {
      final id = path.split('/').last;
      if (request.method == 'DELETE') {
        grants[id] = {
          ...grants[id]!,
          'revision': (grants[id]!['revision'] as int) + 1,
          'revokedAt': now.toIso8601String(),
        };
        if (loseRevoke) {
          loseRevoke = false;
          throw http.ClientException('synthetic response lost');
        }
      }
      return candidateResponse(grants[id]);
    }
    throw StateError('Unexpected synthetic route $path');
  }

  AgentMultiCandidateAPI api([http.Client? client]) => AgentMultiCandidateAPI(
    AgentMemoryCandidateAPI(
      client: client ?? MockClient(handle),
      apiBaseUrl: 'http://fixture',
    ),
  );
}

// Actual native1 registered HTTP body, retained as historical parsing fixture.
// It does not refresh a grant, authorize execution or restore private answers.
const historicalHikingPreviewWire = r'''
{"schemaVersion":"agent-multi-candidate-v1","state":"CURRENT_REVIEW","id":"fe3f59e9-6434-4e00-8552-880ad38dbee7","purpose":"STAGE_MEMORY_CANDIDATE_MULTI","owner":{"type":"PERSON","id":"533fcfb3-0f17-4dc4-a4dc-eebf942852ff"},"agentId":"e4fb42bb-21b4-431d-a4ef-d1910948208f","selection":{"analysisGrantIds":["5095fb85-984f-4e71-b3a2-6a6593f84c36","723428fe-0c0a-46e8-8281-9425fa4ffc9e"],"retainUntil":"2026-10-05T11:02:23.469529Z"},"review":{"proposal":{"algorithmVersion":"moment-lexical-category-v2","predicate":"ACTIVITY_CATEGORY","category":"hiking","assessment":{"semantics":"ORDINAL","level":"LOW"},"matchedTerms":["徒步"],"explanation":"只检测到所选文字中的类别关键词，形成待核验建议；不证明偏好、参加或到场，LOW不是概率，不会写入正式记忆。"},"sources":[{"selector":{"type":"MOMENT","id":"450a4682-f0e2-4bda-b0be-cf568c01f2dc"},"version":{"kind":"REVISION","revision":3},"eventTime":"2026-10-05T10:02:23.470708Z","fingerprint":"26342b4b40e035fef1203c4ea0e5152dbe2c54d5004905be7cf7c139d974211b","anchors":["MOMENT:450a4682-f0e2-4bda-b0be-cf568c01f2dc"]},{"selector":{"type":"MOMENT","id":"90bbd776-e3d0-4831-80a5-e06d3b621e5b"},"version":{"kind":"REVISION","revision":1},"eventTime":"2026-10-05T10:02:23.506418Z","fingerprint":"4ff1706c2507b32d40521ec94b3c7befdefe90c4b083c9c4de75c3d10f38d4e2","anchors":["MOMENT:90bbd776-e3d0-4831-80a5-e06d3b621e5b"]}],"sourceSelections":[{"analysisGrantId":"723428fe-0c0a-46e8-8281-9425fa4ffc9e","analysisPreviewId":"58e10673-6f76-44e6-a0a3-ec9fbaee9981","source":{"selector":{"type":"MOMENT","id":"450a4682-f0e2-4bda-b0be-cf568c01f2dc"},"version":{"kind":"REVISION","revision":3},"eventTime":"2026-10-05T10:02:23.470708Z","fingerprint":"26342b4b40e035fef1203c4ea0e5152dbe2c54d5004905be7cf7c139d974211b","anchors":["MOMENT:450a4682-f0e2-4bda-b0be-cf568c01f2dc"]},"selectedFields":["body"]},{"analysisGrantId":"5095fb85-984f-4e71-b3a2-6a6593f84c36","analysisPreviewId":"fe5ad954-a8be-4628-a53d-86dcaab16814","source":{"selector":{"type":"MOMENT","id":"90bbd776-e3d0-4831-80a5-e06d3b621e5b"},"version":{"kind":"REVISION","revision":1},"eventTime":"2026-10-05T10:02:23.506418Z","fingerprint":"4ff1706c2507b32d40521ec94b3c7befdefe90c4b083c9c4de75c3d10f38d4e2","anchors":["MOMENT:90bbd776-e3d0-4831-80a5-e06d3b621e5b"]},"selectedFields":["body"]}],"anchorEventId":"71b21904-86e5-8cbe-a0ff-0ba9f84079ce","logicalOperationId":"518a971e-707f-8180-ba9b-a4bcc7979aff","anchorSource":{"selector":{"type":"MOMENT","id":"450a4682-f0e2-4bda-b0be-cf568c01f2dc"},"version":{"kind":"REVISION","revision":3},"eventTime":"2026-10-05T10:02:23.470708Z","fingerprint":"26342b4b40e035fef1203c4ea0e5152dbe2c54d5004905be7cf7c139d974211b","anchors":["MOMENT:450a4682-f0e2-4bda-b0be-cf568c01f2dc"]},"taskId":"e0d2724d-4112-4f49-adc7-4100ea362b8f","retainUntil":"2026-10-05T10:03:53.548459Z","clusters":2},"observedAt":"2026-10-05T10:02:23.632262Z","expiresAt":"2026-10-05T10:03:53.548459Z","explanation":"仅批准上面这项关键词假设的有限私密候选保留；不证明偏好或到场。批准不会自动提交候选；原生提交还需当前服务开关与原许可。不会发送模型或写正式记忆。","modelAccess":false,"memoryPromotionAllowed":false,"candidateWriteAvailable":false}
''';

void main() {
  test(
    'actual native4 bare analysis approval receipt is bodyless metadata, never a grant',
    () async {
      // Exact registered HTTP body from native4. Historical timestamps are not a
      // current lease or usable grant. Journal selectors below are test-only IDs.
      const wire =
          r'''{"schemaVersion":"agent-enrichment-purpose-preview-receipt-v1","previewId":"988e60ae-7db6-4305-83e6-9979c2722088","purpose":"MOMENT_LOCAL_ANALYSIS","owner":{"type":"PERSON","id":"49986a51-3b80-4270-a636-81e3f8facafd"},"agentId":"314a6f62-1b17-4f28-9601-b5992b9dc751","state":"APPROVAL_RECORDED","consumedGrantId":"9d886d05-94f1-4e7d-8ef1-95ad27891d0f","previewObservedAt":"2026-10-05T13:51:23.004998Z","previewExpiresAt":"2026-10-05T13:56:23.004998Z","observedAt":"2026-10-05T13:51:23.079376Z","validUntil":"2026-10-05T13:51:53.079376Z","modelAccess":false,"candidateWriteAvailable":false,"memoryPromotionAllowed":false}''';
      final m = jsonDecode(wire) as Map<String, dynamic>;
      final entry = PendingMultiCandidateOperation({
        ...pendingMulti(phase: 'approval').data,
        'ownerId': m['owner']['id'],
        'agentId': m['agentId'],
        'multi': false,
        'anchorEventId': null,
        'logicalOperationId': null,
        'previewId': m['previewId'],
        'expiresAt': m['previewExpiresAt'],
        'sourceVersions': [
          for (var i = 0; i < 1; i++)
            {
              'momentId':
                  '00000000-0000-4000-8000-${i.toString().padLeft(12, '0')}',
              'revision': 1,
            },
        ],
      });
      var calls = 0;
      final client = MockClient((r) async {
        calls++;
        expect(r.method, 'GET');
        expect(r.url.path.endsWith('/${m['previewId']}/receipt'), true);
        return http.Response(
          wire,
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      final transport = AgentMemoryCandidateAPI(
        client: client,
        apiBaseUrl: 'http://synthetic.invalid',
      );
      final status = await AgentMultiCandidateAPI(
        transport,
      ).reconcile('Bearer synthetic', m['owner']['id'] as String, entry);
      expect(status.state, 'APPROVAL_RECORDED');
      expect(status.resolved, true);
      expect(calls, 1);
      transport.dispose();
      client.close();
    },
  );

  test(
    'actual native4 bare multi approval receipt is bodyless metadata, never a grant',
    () async {
      // Exact registered HTTP body from native4. Historical timestamps are not a
      // current lease or usable grant. Journal selectors below are test-only IDs.
      const wire =
          r'''{"schemaVersion":"agent-multi-candidate-preview-receipt-v1","previewId":"3ee11747-d60f-48a9-b7e2-ae6d35089cd0","purpose":"STAGE_MEMORY_CANDIDATE_MULTI","owner":{"type":"PERSON","id":"3645547c-aaff-4803-a47e-17fd6db4aa53"},"agentId":"6d849ead-0b5b-4793-900a-63e353f1ade3","state":"APPROVAL_RECORDED","consumedGrantId":"c3f44cc0-fbf8-4a1d-9382-39c9ec36e3e0","previewObservedAt":"2026-10-05T13:51:24.973488Z","previewExpiresAt":"2026-10-05T13:52:54.941942Z","observedAt":"2026-10-05T13:51:25.286165Z","validUntil":"2026-10-05T13:51:55.286165Z","modelAccess":false,"candidateWriteAvailable":false,"memoryPromotionAllowed":false}''';
      final m = jsonDecode(wire) as Map<String, dynamic>;
      final entry = PendingMultiCandidateOperation({
        ...pendingMulti(phase: 'approval').data,
        'ownerId': m['owner']['id'],
        'agentId': m['agentId'],
        'multi': true,
        'previewId': m['previewId'],
        'expiresAt': m['previewExpiresAt'],
        'sourceVersions': [
          for (var i = 0; i < 2; i++)
            {
              'momentId':
                  '00000000-0000-4000-8000-${i.toString().padLeft(12, '0')}',
              'revision': 1,
            },
        ],
      });
      var calls = 0;
      final client = MockClient((r) async {
        calls++;
        expect(r.method, 'GET');
        expect(r.url.path.endsWith('/${m['previewId']}/receipt'), true);
        return http.Response(
          wire,
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      final transport = AgentMemoryCandidateAPI(
        client: client,
        apiBaseUrl: 'http://synthetic.invalid',
      );
      final status = await AgentMultiCandidateAPI(
        transport,
      ).reconcile('Bearer synthetic', m['owner']['id'] as String, entry);
      expect(status.state, 'APPROVAL_RECORDED');
      expect(status.resolved, true);
      expect(calls, 1);
      transport.dispose();
      client.close();
    },
  );

  for (final multi in [false, true]) {
    for (final change in [
      'owner',
      'agent',
      'preview',
      'purpose',
      'schema',
      'extra',
      'expiry',
      'clock',
      'state',
      'grant',
      'permission',
      'lease',
    ]) {
      test(
        'bodyless receipt $multi rejects malformed $change without grant authority',
        () async {
          final f = MultiWireFixture(),
              p = multi ? f.multiPreview() : f.analysisPreview(1);
          final entry = PendingMultiCandidateOperation({
            ...pendingMulti(phase: 'approval').data,
            'multi': multi,
            'previewId': p['id'],
            'expiresAt': p['expiresAt'],
            if (!multi) 'anchorEventId': null,
            if (!multi) 'logicalOperationId': null,
            if (!multi)
              'sourceVersions': [
                {'momentId': candidateMoment, 'revision': 1},
              ],
          });
          final body = f.previewReceipt(p);
          switch (change) {
            case 'owner':
              body['owner'] = {'type': 'PERSON', 'id': candidateAgent};
            case 'agent':
              body['agentId'] = candidateOwner;
            case 'preview':
              body['previewId'] = candidateSaved;
            case 'purpose':
              body['purpose'] = 'TASK_CONTEXT_READ';
            case 'schema':
              body['schemaVersion'] = 'unknown';
            case 'extra':
              body['review'] = {'content': 'PRIVATE_CANARY'};
            case 'expiry':
              body['previewExpiresAt'] = f.now
                  .add(const Duration(minutes: 9))
                  .toIso8601String();
            case 'clock':
              body['observedAt'] = f.now
                  .subtract(const Duration(days: 1))
                  .toIso8601String();
            case 'state':
              body['state'] = 'CLOSED_UNCONSUMED';
            case 'grant':
              body['consumedGrantId'] = multiGrantID;
            case 'permission':
              body['modelAccess'] = true;
            case 'lease':
              body['validUntil'] = f.now
                  .add(const Duration(minutes: 1))
                  .toIso8601String();
          }
          final client = MockClient((r) async => candidateResponse(body)),
              api = f.api(client);
          await expectLater(
            api.reconcile('Bearer synthetic', candidateOwner, entry),
            throwsFormatException,
          );
          api.dispose();
          client.close();
        },
      );
    }
  }

  for (final bad in [
    'schema',
    'owner',
    'agent',
    'grant',
    'event',
    'operation',
    'state',
    'committed',
    'effect',
    'permission',
  ]) {
    test(
      'metadata-only original stage receipt rejects $bad without releasing journal',
      () async {
        final f = MultiWireFixture()..staged = true;
        final body = f.receipt();
        switch (bad) {
          case 'schema':
            body['schemaVersion'] = 'unknown';
          case 'owner':
            body['owner'] = {'type': 'ORGANIZATION', 'id': candidateOwner};
          case 'agent':
            body['agentId'] = candidateSaved;
          case 'grant':
            body['retentionGrantId'] = candidateSaved;
          case 'event':
            body['eventId'] = candidateSaved;
          case 'operation':
            body['logicalOperationId'] = candidateSaved;
          case 'state':
            body['state'] = 'ACTIVE';
          case 'committed':
            body['committed'] = false;
          case 'effect':
            body['effectKey'] = 'unknown';
          case 'permission':
            body['modelAccess'] = true;
        }
        final client = MockClient((r) async => candidateResponse(body));
        final api = f.api(client);
        await expectLater(
          api.reconcile('Bearer synthetic', candidateOwner, pendingMulti()),
          throwsFormatException,
        );
        api.dispose();
        client.close();
      },
    );
  }
  test(
    'approval bodyless metadata checks original preview identity and deadline without granting authority',
    () async {
      final f = MultiWireFixture(), p = f.multiPreview();
      final entry = PendingMultiCandidateOperation({
        ...pendingMulti(phase: 'approval').data,
        'selectionDigest': multiSelectionDigest(p['selection'], true),
        'reviewDigest': multiReviewDigest(p['review']),
        'expiresAt': p['expiresAt'],
      });
      final body = f.previewReceipt(p);
      final client = MockClient((r) async => candidateResponse(body));
      final api = f.api(client);
      final status = await api.reconcile(
        'Bearer synthetic',
        candidateOwner,
        entry,
      );
      expect(status.state, 'OPEN_UNCONSUMED');
      expect(status.resolved, false);
      body['previewId'] = candidateSaved;
      await expectLater(
        api.reconcile('Bearer synthetic', candidateOwner, entry),
        throwsFormatException,
      );
      api.dispose();
      client.close();
    },
  );
  test(
    'actual canonical PERSON receipt validates its original approved review',
    () {
      final p = jsonDecode(historicalHikingPreviewWire) as Map<String, dynamic>;
      final r = jsonDecode(historicalHikingReceiptWire) as Map<String, dynamic>;
      final owner = p['owner']['id'] as String;
      // Synthetic decoder grant context only: no API request, current authority,
      // TTL renewal, Ticket or grant is created from these historic bytes.
      final g = CandidatePurposeGrant.read(
        {
          'schemaVersion': p['schemaVersion'],
          'purpose': p['purpose'],
          'owner': p['owner'],
          'agentId': p['agentId'],
          'id': r['retentionGrantId'],
          'previewId': p['id'],
          'selection': p['selection'],
          'revision': 1,
          'createdAt': p['observedAt'],
          'observedAt': p['observedAt'],
          'expiresAt': p['expiresAt'],
          'explanation': '仅用于历史DTO解析的合成上下文',
          'modelAccess': false,
          'memoryPromotionAllowed': false,
          'candidateWriteAvailable': false,
        },
        owner,
        multi: true,
        selection: Map<String, dynamic>.from(p['selection']),
        previewID: p['id'],
      );
      final receipt = MultiCandidateReceipt.read(r, owner, g);
      receipt.requireApprovedReview(Map<String, dynamic>.from(p['review']));
      expect(receipt.candidate!.category, 'hiking');
      for (final wrong in [
        'ORGANIZATION',
        'organization',
        'BUSINESS',
        'Person',
        'pErSoN',
        ' PERSON',
        'PERSON ',
        null,
        1,
      ]) {
        final bad = multiClone(r);
        bad['owner']['type'] = wrong;
        expect(
          () => MultiCandidateReceipt.read(bad, owner, g),
          throwsFormatException,
        );
      }
    },
  );
  test(
    'actual canonical PERSON hiking preview is compatible without normalizing identity',
    () {
      final raw =
          jsonDecode(historicalHikingPreviewWire) as Map<String, dynamic>;
      final owner = raw['owner']['id'] as String;
      final selection = Map<String, dynamic>.from(raw['selection']);
      final p = CandidatePurposePreview.read(
        raw,
        owner,
        multi: true,
        selection: selection,
      );
      expect(p.review!['proposal']['category'], 'hiking');
      for (final wrong in [
        'ORGANIZATION',
        'organization',
        'BUSINESS',
        'Person',
        'pErSoN',
        ' PERSON',
        'PERSON ',
        null,
        1,
      ]) {
        final bad = multiClone(raw);
        bad['owner']['type'] = wrong;
        expect(
          () => CandidatePurposePreview.read(
            bad,
            owner,
            multi: true,
            selection: selection,
          ),
          throwsFormatException,
        );
      }
    },
  );

  test('v2 hiking stays ordinal and legacy v1 never admits hiking', () {
    final f = MultiWireFixture()
      ..category = 'hiking'
      ..algorithm = 'moment-lexical-category-v2';
    final parsed = CandidatePurposePreview.read(
      f.multiPreview(),
      candidateOwner,
      multi: true,
      selection: f.multiSelection(),
    );
    expect(parsed.review!['proposal']['category'], 'hiking');
    expect(parsed.review!['proposal']['assessment']['value'], isNull);
    for (final algorithm in [
      'moment-lexical-category-v1',
      'moment-lexical-category-v3',
      '',
    ]) {
      f.algorithm = algorithm;
      expect(
        () => CandidatePurposePreview.read(
          f.multiPreview(),
          candidateOwner,
          multi: true,
          selection: f.multiSelection(),
        ),
        throwsFormatException,
      );
    }
    f.algorithm = 'moment-lexical-category-v1';
    f.category = 'culture';
    expect(
      CandidatePurposePreview.read(
        f.multiPreview(),
        candidateOwner,
        multi: true,
        selection: f.multiSelection(),
      ).review!['proposal']['category'],
      'culture',
    );
  });
  test('task choices use the exact native follow-up query', () async {
    final f = MultiWireFixture()..currentQuery = '近一点的周末羽毛球呢？';
    final api = f.api();
    final (tasks, moments) = await api.choices(
      'Bearer synthetic',
      candidateOwner,
    );
    expect(tasks.single.query, f.currentQuery);
    expect(tasks.single.raw.keys.toSet(), {'id', 'query', 'updatedAt'});
    expect(moments.length, 2);
    api.dispose();
  });
  for (final filters in [
    null,
    <String, dynamic>{},
    {'currentQuery': null},
    {'currentQuery': ''},
  ]) {
    test(
      'empty current query uses the original native fallback $filters',
      () async {
        final f = MultiWireFixture();
        final client = MockClient((r) async {
          if (r.url.path == '/v1/me/agent-tasks') {
            return candidateResponse([
              {...f.task(), 'filters': filters},
            ]);
          }
          return f.handle(r);
        });
        final api = f.api(client);
        final (tasks, _) = await api.choices(
          'Bearer synthetic',
          candidateOwner,
        );
        expect(tasks.single.query, f.task()['query']);
        api.dispose();
        client.close();
      },
    );
  }
  for (final filters in [
    [],
    'invalid filters',
    {'currentQuery': 123},
    {'currentQuery': true},
    {'currentQuery': '   '},
    {'currentQuery': '中' * 81},
  ]) {
    test(
      'invalid current query fails closed rather than old query fallback $filters',
      () async {
        final f = MultiWireFixture();
        final client = MockClient((r) async {
          if (r.url.path == '/v1/me/agent-tasks') {
            return candidateResponse([
              {...f.task(), 'filters': filters},
            ]);
          }
          return f.handle(r);
        });
        final api = f.api(client);
        await expectLater(
          api.choices('Bearer synthetic', candidateOwner),
          throwsFormatException,
        );
        api.dispose();
        client.close();
      },
    );
  }
  test('native query byte boundary is preserved exactly', () async {
    final f = MultiWireFixture()..currentQuery = '中' * 80;
    final api = f.api();
    final (tasks, _) = await api.choices('Bearer synthetic', candidateOwner);
    expect(tasks.single.query, f.currentQuery);
    api.dispose();
  });
  for (final mutation in ['anchor', 'logical', 'category', 'version']) {
    test('staged receipt cannot substitute the approved $mutation', () {
      final f = MultiWireFixture()..staged = true;
      final p = CandidatePurposePreview.read(
        f.multiPreview(),
        candidateOwner,
        multi: true,
        selection: f.multiSelection(),
      );
      final g = CandidatePurposeGrant.read(
        f.grant(f.multiPreview(), multiGrantID),
        candidateOwner,
        multi: true,
        selection: f.multiSelection(),
        previewID: multiPreviewID,
      );
      final r = multiClone(f.receipt()) as Map<String, dynamic>;
      switch (mutation) {
        case 'anchor':
          r['eventId'] = candidateAgent;
        case 'logical':
          r['logicalOperationId'] = candidateOwner;
        case 'category':
          r['candidate']['category'] = 'sports';
        case 'version':
          r['candidate']['sources'][1]['version']['revision'] = 3;
      }
      final receipt = MultiCandidateReceipt.read(r, candidateOwner, g);
      expect(
        () => receipt.requireApprovedReview(p.review!),
        throwsFormatException,
      );
    });
  }
  test(
    'approved permit cannot extend the displayed concrete preview expiry',
    () async {
      final f = MultiWireFixture(),
          p = CandidatePurposePreview.read(
            f.multiPreview(),
            candidateOwner,
            multi: true,
            selection: f.multiSelection(),
          );
      final g = f.grant(f.multiPreview(), multiGrantID);
      g['createdAt'] = f.now.add(const Duration(seconds: 10)).toIso8601String();
      g['observedAt'] = g['createdAt'];
      g['expiresAt'] = f.now
          .add(const Duration(seconds: 100))
          .toIso8601String();
      final client = MockClient((r) async => candidateResponse(g)),
          api = f.api(client);
      await expectLater(
        api.approve('Bearer synthetic', candidateOwner, p),
        throwsFormatException,
      );
      api.dispose();
      client.close();
    },
  );
  test('native schemas are typed, ordinal only and immutable', () {
    final f = MultiWireFixture();
    final p = CandidatePurposePreview.read(
      f.multiPreview(),
      candidateOwner,
      multi: true,
      selection: f.multiSelection(),
    );
    expect(p.current, true);
    expect(p.review!['proposal']['assessment']['value'], isNull);
    expect(
      () => p.selection['analysisGrantIds'].add(sourceGrantID(3)),
      throwsUnsupportedError,
    );
  });
  test(
    'selection dates compare normalized UTC instants rather than spelling',
    () {
      final f = MultiWireFixture(),
          selection = MultiWireFixture().analysisSelection(1);
      selection['deadlineAt'] = '2030-01-01T00:00:00.000000Z';
      final p = f.analysisPreview(1, selection);
      p['selection'] = multiClone(selection);
      p['selection']['deadlineAt'] = '2030-01-01T00:00:00Z';
      expect(
        CandidatePurposePreview.read(
          p,
          candidateOwner,
          multi: false,
          selection: selection,
        ).id,
        sourcePreviewID(1),
      );
    },
  );
  for (final mutation in [
    'owner',
    'schema',
    'purpose',
    'agent',
    'model',
    'promotion',
    'write',
    'emptyExplanation',
    'expired',
    'selection',
    'nullAnchor',
    'nullTask',
    'nullMember',
    'duplicateSource',
    'duplicateGrant',
    'version',
    'fields',
    'anchors',
    'category',
    'probability',
    'clusters',
    'sourceOrder',
  ]) {
    test('closed multi review rejects $mutation', () {
      final f = MultiWireFixture(),
          p = multiClone(f.multiPreview()) as Map<String, dynamic>,
          r = p['review'] as Map;
      switch (mutation) {
        case 'owner':
          p['owner']['id'] = candidateAgent;
        case 'schema':
          p['schemaVersion'] = 'unknown';
        case 'purpose':
          p['purpose'] = 'MEMORY';
        case 'agent':
          p['agentId'] = 'bad';
        case 'model':
          p['modelAccess'] = true;
        case 'promotion':
          p['memoryPromotionAllowed'] = true;
        case 'write':
          p['candidateWriteAvailable'] = true;
        case 'emptyExplanation':
          p['explanation'] = '';
        case 'expired':
          p['expiresAt'] = f.now.toIso8601String();
        case 'selection':
          p['selection'] = {...f.multiSelection(), 'score': 0.82};
        case 'nullAnchor':
          r['anchorEventId'] = null;
        case 'nullTask':
          r['taskId'] = null;
        case 'nullMember':
          r['sourceSelections'][0]['analysisPreviewId'] = null;
        case 'duplicateSource':
          r['sources'][1] = r['sources'][0];
        case 'duplicateGrant':
          r['sourceSelections'][1]['analysisGrantId'] = sourceGrantID(1);
        case 'version':
          r['sources'][0]['version']['revision'] = 0;
        case 'fields':
          r['sourceSelections'][0]['selectedFields'] = ['location'];
        case 'anchors':
          r['sources'][0]['anchors'] = [];
        case 'category':
          r['proposal']['category'] = 'religion';
        case 'probability':
          r['proposal']['assessment']['value'] = 0.82;
        case 'clusters':
          r['clusters'] = 1;
        case 'sourceOrder':
          r['sources'] = (r['sources'] as List).reversed.toList();
      }
      expect(
        () => CandidatePurposePreview.read(
          p,
          candidateOwner,
          multi: true,
          selection: f.multiSelection(),
        ),
        throwsA(anything),
      );
    });
  }
  for (final mutation in [
    'owner',
    'id',
    'agent',
    'purpose',
    'selection',
    'revision',
    'model',
    'revokeClock',
  ]) {
    test('grant exact binding rejects $mutation', () {
      final f = MultiWireFixture(),
          p = f.multiPreview(),
          g = f.grant(p, multiGrantID);
      switch (mutation) {
        case 'owner':
          g['owner']['type'] = 'organization';
        case 'id':
          g['id'] = candidateAgent;
        case 'agent':
          g['agentId'] = candidateOwner;
        case 'purpose':
          g['purpose'] = 'MOMENT_LOCAL_ANALYSIS';
        case 'selection':
          g['selection'] = {
            ...f.multiSelection(),
            'retainUntil': f.now.toIso8601String(),
          };
        case 'revision':
          g['revision'] = 0;
        case 'model':
          g['modelAccess'] = true;
        case 'revokeClock':
          g['revokedAt'] = f.now
              .subtract(const Duration(seconds: 1))
              .toIso8601String();
      }
      expect(
        () => CandidatePurposeGrant.read(
          g,
          candidateOwner,
          multi: true,
          selection: f.multiSelection(),
          previewID: multiPreviewID,
          expectedID: multiGrantID,
          agentID: candidateAgent,
        ),
        throwsA(anything),
      );
    });
  }
  test('metadata staged receipt has no authority to promote Memory', () {
    final f = MultiWireFixture()..staged = true;
    final g = CandidatePurposeGrant.read(
      f.grant(f.multiPreview(), multiGrantID),
      candidateOwner,
      multi: true,
      selection: f.multiSelection(),
      previewID: multiPreviewID,
    );
    expect(
      MultiCandidateReceipt.read(
        f.receipt(withCandidate: false),
        candidateOwner,
        g,
      ).staged,
      true,
    );
    final r = f.receipt();
    r['candidate']['status'] = 'ACTIVE';
    expect(
      () => MultiCandidateReceipt.read(r, candidateOwner, g),
      throwsA(anything),
    );
  });
  test(
    'analysis approve has no body and revoke is actual DELETE with revision',
    () async {
      final f = MultiWireFixture(), api = f.api();
      final p = await api.preview(
        'Bearer synthetic',
        candidateOwner,
        f.analysisSelection(1),
        multi: false,
      );
      final g = await api.approve('Bearer synthetic', candidateOwner, p);
      final revoked = await api.revoke('Bearer synthetic', candidateOwner, g);
      expect(revoked.revoked, true);
      expect(f.requests[1]['body'], '');
      expect(f.requests[2]['method'], 'DELETE');
      expect(jsonDecode(f.requests[2]['body'] as String), {
        'expectedRevision': 1,
      });
      api.dispose();
    },
  );
  test(
    'only current private drafts and real ACTIVE personal CITY tasks are offered',
    () async {
      final f = MultiWireFixture();
      final client = MockClient(
        (r) async => candidateResponse(
          r.url.path.endsWith('agent-tasks')
              ? [
                  f.task(),
                  {...f.task(), 'id': candidateAgent, 'status': 'COMPLETED'},
                  {...f.task(), 'id': candidateMemory, 'contextType': 'ONLINE'},
                ]
              : [
                  f.moment(1),
                  {...f.moment(2), 'visibility': 'public'},
                  {...f.moment(2), 'status': 'published'},
                  {
                    ...f.moment(2),
                    'activityIds': [candidateMemory],
                  },
                  {
                    ...f.moment(2),
                    'updatedAt': f.now
                        .subtract(const Duration(minutes: 16))
                        .toIso8601String(),
                  },
                ],
        ),
      );
      final api = f.api(client),
          choices = await api.choices('Bearer synthetic', candidateOwner);
      expect(choices.$1.single.id, multiTask);
      expect(choices.$2.single.id, candidateMoment);
      expect(
        choices.$1.single.raw.keys,
        unorderedEquals(['id', 'query', 'updatedAt']),
      );
      api.dispose();
      client.close();
    },
  );
  test('foreign own-source response cannot be displayed', () async {
    final f = MultiWireFixture();
    final client = MockClient(
      (r) async => candidateResponse(
        r.url.path.endsWith('agent-tasks')
            ? [f.task()]
            : [
                {...f.moment(1), 'authorAccountId': candidateAgent},
              ],
      ),
    );
    final api = f.api(client);
    await expectLater(
      api.choices('Bearer synthetic', candidateOwner),
      throwsFormatException,
    );
    api.dispose();
    client.close();
  });
}
