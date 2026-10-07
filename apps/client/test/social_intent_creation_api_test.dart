import 'dart:convert';
import 'package:birdtie_client/src/workspace/social_intent_creation_api.dart';
import 'package:birdtie_client/src/workspace/social_intent_creation_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const creationOwner = '11111111-1111-4111-8111-111111111111';
const creationID = '33333333-3333-4333-8333-333333333333';
Map<String, dynamic> creationDraft({String title = '本人的私人想法'}) => {
  'type': 'FIND_COMPANION',
  'title': title,
  'audience': 'PRIVATE',
  'modality': 'ONLINE',
  'constraints': <String, dynamic>{},
  'expiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(days: 7))
      .toIso8601String(),
};

/// Matches the real native DTO, with exclusively synthetic owners and fields.
http.Response creationWire(
  http.Request request, {
  Map<String, dynamic>? bad,
  String? reason,
}) {
  final wire = jsonDecode(request.body) as Map<String, dynamic>;
  final draft = Map<String, dynamic>.from((wire['draft'] ?? wire) as Map),
      operation = draft.remove('operationId') as String;
  final source = request.url.path.contains('/agent-tasks/')
      ? request.url.pathSegments[3]
      : '';
  final normalized = intentCreationDraft(draft, source);
  final now = DateTime.now().toUtc().toIso8601String();
  final item = {
    'id': creationID,
    'creatorAccountId': creationOwner,
    ...normalized,
    'status': 'DRAFT',
    'createdAt': now,
    'updatedAt': now,
    ...?bad,
  };
  return http.Response.bytes(
    utf8.encode(
      jsonEncode({
        'data': {
          'schemaVersion': socialIntentCreationSchema,
          'ownerAccountId': creationOwner,
          'operationId': operation,
          'requestDigest': intentCreationDigest(creationOwner, source, draft),
          if (source.isNotEmpty) 'sourceTaskId': source,
          'status': reason == null ? 'COMMITTED' : 'NO_EFFECT',
          if (reason == null) ...{'intentId': creationID, 'intent': item},
          'reason': ?reason,
          'recordedAt': now,
        },
      }),
    ),
    reason == null ? 201 : 200,
  );
}

// Verbatim actual registered HTTP/PostgreSQL wire snapshots. Synthetic only.
const nativeCreationWireSnapshots = r'''[
  {
    "label": "SOURCE_READ",
    "source": "work/intent-draft-creation-recovery-2026-10-06/native-wire05/wire/TestSocialIntentCreationHTTPNativeLegacySourceAndReadBoundaries-get-v1-me-agent-tasks-7002be99-a146-4871-b5bc-14dcd512c5b4-social-intent-draft-0-person-000.json",
    "sha256": "cd0e70acb7bbdb2ecbcaa8e92949bd9d2cfed1e360d0481f8c56ed6be2b92302",
    "wire": {
      "method": "GET",
      "path": "/v1/me/agent-tasks/7002be99-a146-4871-b5bc-14dcd512c5b4/social-intent-draft",
      "requestBody": "null",
      "responseBody": "{\"data\":{\"schemaVersion\":\"social-intent-creations-v1\",\"ownerAccountId\":\"ba8aab7e-dc60-4189-b24c-9bd7fe89afad\",\"sourceTaskId\":\"7002be99-a146-4871-b5bc-14dcd512c5b4\",\"intentId\":\"6dad6345-dab2-4480-8702-4ba2b9e87057\",\"intent\":{\"id\":\"6dad6345-dab2-4480-8702-4ba2b9e87057\",\"creatorAccountId\":\"ba8aab7e-dc60-4189-b24c-9bd7fe89afad\",\"type\":\"FIND_ACTIVITY\",\"title\":\"原来源私人草稿\",\"constraints\":{},\"audience\":\"PRIVATE\",\"modality\":\"ONLINE\",\"status\":\"DRAFT\",\"expiresAt\":\"2026-10-05T23:49:38.228057Z\",\"createdAt\":\"2026-10-05T22:49:38.231417Z\",\"updatedAt\":\"2026-10-05T22:49:38.231417Z\"}}}\n",
      "scope": "ACTUAL_REGISTERED_HTTP_POSTGRES_SYNTHETIC_NOT_IDP_OR_PILOT",
      "status": 200
    }
  },
  {
    "label": "SOURCE_NO_EFFECT",
    "source": "work/intent-draft-creation-recovery-2026-10-06/native-wire05/wire/TestSocialIntentCreationHTTPNativeLegacySourceAndReadBoundaries-post-v1-me-agent-tasks-7002be99-a146-4871-b5bc-14dcd512c5b4-social-intent-drafts-0-person-002.json",
    "sha256": "9e8f280c3df683b0b4c705f2f4a1d4e3c7eebb022edfaf19fb225124f1ee0179",
    "wire": {
      "method": "POST",
      "path": "/v1/me/agent-tasks/7002be99-a146-4871-b5bc-14dcd512c5b4/social-intent-drafts",
      "requestBody": "{\"confirmed\":true,\"draft\":{\"audience\":\"PRIVATE\",\"constraints\":{},\"expiresAt\":\"2026-10-05T23:49:38.2280571Z\",\"modality\":\"ONLINE\",\"operationId\":\"70891b1f-a11b-4d36-b594-04fc30ab6e7a\",\"title\":\"这次修改并未再次保存\",\"type\":\"FIND_ACTIVITY\"}}",
      "responseBody": "{\"data\":{\"schemaVersion\":\"social-intent-creations-v1\",\"ownerAccountId\":\"ba8aab7e-dc60-4189-b24c-9bd7fe89afad\",\"operationId\":\"70891b1f-a11b-4d36-b594-04fc30ab6e7a\",\"requestDigest\":\"c1544e2ac85cd0ee909f69fee29d86c1c5cf72c39cbb104ad9f606656309cbd9\",\"sourceTaskId\":\"7002be99-a146-4871-b5bc-14dcd512c5b4\",\"status\":\"NO_EFFECT\",\"priorIntentId\":\"6dad6345-dab2-4480-8702-4ba2b9e87057\",\"reason\":\"SOURCE_ALREADY_EXISTS\",\"recordedAt\":\"2026-10-06T06:49:38.288577+08:00\",\"intent\":{\"id\":\"6dad6345-dab2-4480-8702-4ba2b9e87057\",\"creatorAccountId\":\"ba8aab7e-dc60-4189-b24c-9bd7fe89afad\",\"type\":\"FIND_ACTIVITY\",\"title\":\"原来源私人草稿\",\"constraints\":{},\"audience\":\"PRIVATE\",\"modality\":\"ONLINE\",\"status\":\"DRAFT\",\"expiresAt\":\"2026-10-05T23:49:38.228057Z\",\"createdAt\":\"2026-10-05T22:49:38.231417Z\",\"updatedAt\":\"2026-10-05T22:49:38.231417Z\"}}}\n",
      "scope": "ACTUAL_REGISTERED_HTTP_POSTGRES_SYNTHETIC_NOT_IDP_OR_PILOT",
      "status": 409
    }
  },
  {
    "label": "CREATE",
    "source": "work/intent-draft-creation-recovery-2026-10-06/native-wire05/wire/TestSocialIntentCreationHTTPNativeOperationKey-create-000.json",
    "sha256": "7df5a671552c467edeaa6e59d8d8079a55d866af0f23500a4f440f4575428604",
    "wire": {
      "method": "POST",
      "path": "/v1/me/social-intents",
      "requestBody": "{\"audience\":\"PRIVATE\",\"constraints\":{},\"expiresAt\":\"2026-10-05T23:49:38.0266367Z\",\"modality\":\"ONLINE\",\"operationId\":\"e63c8803-e122-4b33-b4a8-114293e3afcc\",\"title\":\"合成私人意图保存回执\",\"type\":\"FIND_ACTIVITY\"}",
      "responseBody": "{\"data\":{\"schemaVersion\":\"social-intent-creations-v1\",\"ownerAccountId\":\"b1a705c4-d420-4076-9c05-24874cac73a7\",\"operationId\":\"e63c8803-e122-4b33-b4a8-114293e3afcc\",\"requestDigest\":\"d5f14d3c920da1d2656c49b0c3300c3be129dc35e1d5971446770135d0598f8d\",\"status\":\"COMMITTED\",\"intentId\":\"38c4c64a-4541-4f80-836f-82a397368f1e\",\"recordedAt\":\"2026-10-06T06:49:38.051745+08:00\",\"intent\":{\"id\":\"38c4c64a-4541-4f80-836f-82a397368f1e\",\"creatorAccountId\":\"b1a705c4-d420-4076-9c05-24874cac73a7\",\"type\":\"FIND_ACTIVITY\",\"title\":\"合成私人意图保存回执\",\"constraints\":{},\"audience\":\"PRIVATE\",\"modality\":\"ONLINE\",\"status\":\"DRAFT\",\"expiresAt\":\"2026-10-06T07:49:38.026636+08:00\",\"createdAt\":\"2026-10-06T06:49:38.033455+08:00\",\"updatedAt\":\"2026-10-06T06:49:38.033455+08:00\"}}}\n",
      "scope": "ACTUAL_REGISTERED_HTTP_POSTGRES_SYNTHETIC_NOT_IDP_OR_PILOT",
      "status": 201
    }
  },
  {
    "label": "CURRENT_CANCELLED",
    "source": "work/intent-draft-creation-recovery-2026-10-06/native-wire05/wire/TestSocialIntentCreationHTTPNativeOperationKey-current-cancelled-000.json",
    "sha256": "4ad54fdf0510c920dcba56890f88e084565da8619eb180d11d14d7f11af981ec",
    "wire": {
      "method": "POST",
      "path": "/v1/me/social-intents",
      "requestBody": "{\"audience\":\"PRIVATE\",\"constraints\":{},\"expiresAt\":\"2026-10-05T23:49:38.0266367Z\",\"modality\":\"ONLINE\",\"operationId\":\"e63c8803-e122-4b33-b4a8-114293e3afcc\",\"title\":\"合成私人意图保存回执\",\"type\":\"FIND_ACTIVITY\"}",
      "responseBody": "{\"data\":{\"schemaVersion\":\"social-intent-creations-v1\",\"ownerAccountId\":\"b1a705c4-d420-4076-9c05-24874cac73a7\",\"operationId\":\"e63c8803-e122-4b33-b4a8-114293e3afcc\",\"requestDigest\":\"d5f14d3c920da1d2656c49b0c3300c3be129dc35e1d5971446770135d0598f8d\",\"status\":\"COMMITTED\",\"intentId\":\"38c4c64a-4541-4f80-836f-82a397368f1e\",\"recordedAt\":\"2026-10-06T06:49:38.051745+08:00\",\"intent\":{\"id\":\"38c4c64a-4541-4f80-836f-82a397368f1e\",\"creatorAccountId\":\"b1a705c4-d420-4076-9c05-24874cac73a7\",\"type\":\"FIND_ACTIVITY\",\"title\":\"原实体已由本人后续修改\",\"constraints\":{},\"audience\":\"PRIVATE\",\"modality\":\"ONLINE\",\"status\":\"CANCELLED\",\"expiresAt\":\"2026-10-05T23:49:38.026636Z\",\"createdAt\":\"2026-10-05T22:49:38.033455Z\",\"updatedAt\":\"2026-10-05T22:49:38.084456Z\"}}}\n",
      "scope": "ACTUAL_REGISTERED_HTTP_POSTGRES_SYNTHETIC_NOT_IDP_OR_PILOT",
      "status": 200
    }
  },
  {
    "label": "CURRENT_EXPIRED",
    "source": "work/intent-draft-creation-recovery-2026-10-06/native-wire05/wire/TestSocialIntentCreationHTTPNativeOperationKey-current-expired-000.json",
    "sha256": "67b713e503e36be4cca31f50ebaa10ebd0e0beac2a46ee55b11b7f47229570ff",
    "wire": {
      "method": "POST",
      "path": "/v1/me/social-intents",
      "requestBody": "{\"audience\":\"PRIVATE\",\"constraints\":{},\"expiresAt\":\"2026-10-05T23:49:38.0266367Z\",\"modality\":\"ONLINE\",\"operationId\":\"e63c8803-e122-4b33-b4a8-114293e3afcc\",\"title\":\"合成私人意图保存回执\",\"type\":\"FIND_ACTIVITY\"}",
      "responseBody": "{\"data\":{\"schemaVersion\":\"social-intent-creations-v1\",\"ownerAccountId\":\"b1a705c4-d420-4076-9c05-24874cac73a7\",\"operationId\":\"e63c8803-e122-4b33-b4a8-114293e3afcc\",\"requestDigest\":\"d5f14d3c920da1d2656c49b0c3300c3be129dc35e1d5971446770135d0598f8d\",\"status\":\"COMMITTED\",\"intentId\":\"38c4c64a-4541-4f80-836f-82a397368f1e\",\"recordedAt\":\"2026-10-06T06:49:38.051745+08:00\",\"intent\":{\"id\":\"38c4c64a-4541-4f80-836f-82a397368f1e\",\"creatorAccountId\":\"b1a705c4-d420-4076-9c05-24874cac73a7\",\"type\":\"FIND_ACTIVITY\",\"title\":\"原实体已由本人后续修改\",\"constraints\":{},\"audience\":\"PRIVATE\",\"modality\":\"ONLINE\",\"status\":\"EXPIRED\",\"expiresAt\":\"2026-10-05T22:49:38.033456Z\",\"createdAt\":\"2026-10-05T22:49:38.033455Z\",\"updatedAt\":\"2026-10-05T22:49:38.098959Z\"}}}\n",
      "scope": "ACTUAL_REGISTERED_HTTP_POSTGRES_SYNTHETIC_NOT_IDP_OR_PILOT",
      "status": 200
    }
  },
  {
    "label": "REPLAY",
    "source": "work/intent-draft-creation-recovery-2026-10-06/native-wire05/wire/TestSocialIntentCreationHTTPNativeOperationKey-replay-000.json",
    "sha256": "65852ea197c992c656578de5da5c3dba7ad0311c7312e06fcf6d9b25082fa896",
    "wire": {
      "method": "POST",
      "path": "/v1/me/social-intents",
      "requestBody": "{\"audience\":\"PRIVATE\",\"constraints\":{},\"expiresAt\":\"2026-10-05T23:49:38.0266367Z\",\"modality\":\"ONLINE\",\"operationId\":\"e63c8803-e122-4b33-b4a8-114293e3afcc\",\"title\":\"合成私人意图保存回执\",\"type\":\"FIND_ACTIVITY\"}",
      "responseBody": "{\"data\":{\"schemaVersion\":\"social-intent-creations-v1\",\"ownerAccountId\":\"b1a705c4-d420-4076-9c05-24874cac73a7\",\"operationId\":\"e63c8803-e122-4b33-b4a8-114293e3afcc\",\"requestDigest\":\"d5f14d3c920da1d2656c49b0c3300c3be129dc35e1d5971446770135d0598f8d\",\"status\":\"COMMITTED\",\"intentId\":\"38c4c64a-4541-4f80-836f-82a397368f1e\",\"recordedAt\":\"2026-10-06T06:49:38.051745+08:00\",\"intent\":{\"id\":\"38c4c64a-4541-4f80-836f-82a397368f1e\",\"creatorAccountId\":\"b1a705c4-d420-4076-9c05-24874cac73a7\",\"type\":\"FIND_ACTIVITY\",\"title\":\"合成私人意图保存回执\",\"constraints\":{},\"audience\":\"PRIVATE\",\"modality\":\"ONLINE\",\"status\":\"DRAFT\",\"expiresAt\":\"2026-10-05T23:49:38.026636Z\",\"createdAt\":\"2026-10-05T22:49:38.033455Z\",\"updatedAt\":\"2026-10-05T22:49:38.033455Z\"}}}\n",
      "scope": "ACTUAL_REGISTERED_HTTP_POSTGRES_SYNTHETIC_NOT_IDP_OR_PILOT",
      "status": 200
    }
  }
]''';

void main() {
  test('原生Go与Dart规范化摘要一致，操作ID不代替内容摘要', () {
    final draft = {
      'type': 'FIND_ACTIVITY',
      'title': ' 周末羽毛球 ',
      'audience': 'PRIVATE',
      'modality': 'ONLINE',
      'constraints': {
        'onlinePlatform': ' Zoom ',
        'startsAt': '2030-01-01T18:00:00+08:00',
        'endsAt': '2030-01-01T19:00:00+08:00',
      },
      'expiresAt': '2030-01-02T01:02:03.123456Z',
    };
    expect(
      intentCreationDigest('33333333-3333-4333-8333-333333333333', '', draft),
      'f12ae4867a5f750203bd2f6b9245ae4808c642a65441b73544e3dd5e669530a2',
    );
    final other = {
      ...draft,
      'title': '周末羽毛球',
      'constraints': {
        'endsAt': '2030-01-01T11:00:00Z',
        'startsAt': '2030-01-01T10:00:00Z',
        'onlinePlatform': 'Zoom',
      },
    };
    expect(
      intentCreationDigest(creationOwner, '', draft),
      intentCreationDigest(creationOwner, '', other),
    );
    expect(
      intentCreationDigest(creationOwner, '', draft),
      isNot(intentCreationDigest(creationID, '', draft)),
    );
  });
  test('创建捕获身份、请求与操作ID，按原ID读取而不是按标题匹配', () async {
    final pending = PendingSocialIntentCreation.capture(
      environment: 'https://api.test/',
      owner: creationOwner,
      source: '',
      draft: creationDraft(),
    );
    final calls = <http.Request>[];
    late http.Response receipt;
    final client = MockClient((r) async {
      calls.add(r);
      if (r.method == 'POST') {
        receipt = creationWire(r);
        return receipt;
      }
      return http.Response.bytes(receipt.bodyBytes, 200);
    });
    final api = SocialIntentCreationAPI(
      client: client,
      base: 'https://api.test/',
      token: 'Bearer captured-A',
    );
    final created = await api.create(
      owner: pending.owner,
      operation: pending.operation,
      digest: pending.digest,
      source: pending.source,
      draft: pending.draft,
    );
    expect(created.id, creationID);
    expect(created.committed, isTrue);
    final got = await api.read(
      owner: pending.owner,
      operation: pending.operation,
      digest: pending.digest,
      source: pending.source,
    );
    expect(got!.id, created.id);
    expect(
      calls.map((r) => r.headers['Authorization']),
      everyElement('Bearer captured-A'),
    );
    expect(
      calls.last.url.path,
      '/v1/me/social-intent-creations/${pending.operation}',
    );
    client.close();
  });
  test('404只表示当前无回执，不证明原POST失败', () async {
    final p = PendingSocialIntentCreation.capture(
      environment: 'https://api.test',
      owner: creationOwner,
      source: '',
      draft: creationDraft(),
    );
    final client = MockClient(
      (_) async => http.Response('{"error":{"code":"not_found"}}', 404),
    );
    expect(
      await SocialIntentCreationAPI(
        client: client,
        base: 'https://api.test',
        token: 'Bearer A',
      ).read(
        owner: p.owner,
        operation: p.operation,
        digest: p.digest,
        source: p.source,
      ),
      isNull,
    );
    client.close();
  });
  for (final field in [
    'operationId',
    'requestDigest',
    'ownerAccountId',
    'sourceTaskId',
  ]) {
    test('核查拒绝错误$field', () async {
      final p = PendingSocialIntentCreation.capture(
        environment: 'https://api.test',
        owner: creationOwner,
        source: '',
        draft: creationDraft(),
      );
      final client = MockClient((r) async {
        final good = jsonDecode(creationWire(r).body) as Map<String, dynamic>;
        (good['data'] as Map<String, dynamic>)[field] = 'different';
        return http.Response(jsonEncode(good), 201);
      });
      expect(
        SocialIntentCreationAPI(
          client: client,
          base: 'https://api.test',
          token: 'Bearer A',
        ).create(
          owner: p.owner,
          operation: p.operation,
          digest: p.digest,
          source: p.source,
          draft: p.draft,
        ),
        throwsFormatException,
      );
      client.close();
    });
  }

  test('实际原生HTTP创建/重放/来源拒绝wire由Dart直接解析，摘要不靠手造成功回执', () {
    final snapshots = jsonDecode(nativeCreationWireSnapshots) as List;
    for (final raw in snapshots) {
      final snapshot = raw as Map<String, dynamic>,
          wire = snapshot['wire'] as Map<String, dynamic>;
      final envelope =
              jsonDecode(wire['responseBody'] as String)
                  as Map<String, dynamic>,
          data = envelope['data'] as Map<String, dynamic>;
      if (snapshot['label'] == 'SOURCE_READ') {
        final source = SocialIntentTaskDraftReceipt.decode(
          wire['responseBody'] as String,
          owner: data['ownerAccountId'] as String,
          source: data['sourceTaskId'] as String,
        );
        expect(source.id, data['intentId']);
        expect(data.containsKey('operationId'), isFalse);
        expect(
          () => SocialIntentTaskDraftReceipt.decode(
            wire['responseBody'] as String,
            owner: creationOwner,
            source: data['sourceTaskId'] as String,
          ),
          throwsFormatException,
        );
        continue;
      }
      final request =
              jsonDecode(wire['requestBody'] as String) as Map<String, dynamic>,
          draft = Map<String, dynamic>.from(
            (request['draft'] ?? request) as Map,
          ),
          operation = draft.remove('operationId') as String;
      final owner = data['ownerAccountId'] as String,
          source = data['sourceTaskId'] as String? ?? '',
          digest = intentCreationDigest(owner, source, draft);
      expect(
        digest,
        data['requestDigest'],
        reason: 'actual Go digest vs Dart request digest',
      );
      final receipt = SocialIntentCreationReceipt.decode(
        wire['responseBody'] as String,
        owner: owner,
        operation: operation,
        digest: digest,
        source: source,
      );
      expect(receipt.committed, snapshot['label'] != 'SOURCE_NO_EFFECT');
      if (snapshot['label'] == 'SOURCE_NO_EFFECT') {
        expect(receipt.reason, 'SOURCE_ALREADY_EXISTS');
        expect(receipt.intent!['title'], isNot(draft['title']));
      }
      expect(
        () => SocialIntentCreationReceipt.decode(
          wire['responseBody'] as String,
          owner: owner,
          operation: operation,
          digest: intentCreationDigest(owner, source, {
            ...draft,
            'title': '同key另一个内容',
          }),
          source: source,
        ),
        throwsFormatException,
      );
    }
  });
}
