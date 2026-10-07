import 'dart:async';
import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:birdtie_client/src/workspace/agent_memory_correction_api.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_controller.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'agent_memory_self_review_api_test.dart';

AgentMemoryCorrectionController reviewController(
  ReviewSendClient client, {
  String? Function()? token,
  String? Function()? owner,
  String? Function()? workspace,
  DateTime Function()? now,
  Duration Function()? elapsed,
  AgentMemoryCorrectionPendingStore? store,
}) => AgentMemoryCorrectionController(
  authorizationHeader: token ?? () => 'Bearer synthetic',
  accountID: owner ?? () => reviewOwner,
  organizationWorkspaceID: workspace,
  now: now ?? reviewClock,
  elapsed: elapsed,
  pendingStore: store ?? MemoryAgentMemoryCorrectionPendingStore(),
  api: AgentMemoryCorrectionAPI(client: client, apiBaseUrl: 'http://local'),
);
ReviewSendClient reviewListClient(
  Future<http.Response> Function(http.BaseRequest) read,
) => ReviewSendClient((r) {
  if (r.url.path.endsWith('/self-review') || r.method == 'POST') return read(r);
  return Future.value(
    reviewResponse(
      r.url.path.endsWith('/agent-memories') ? [reviewMemoryRow()] : [],
    ),
  );
});
int reviewReadCount(ReviewSendClient c) =>
    c.requests.where((r) => r.url.path.endsWith('/self-review')).length;

void main() {
  test('同体读取：原wire单请求、当前列表对象与原更正状态独立', () async {
    final store = MemoryAgentMemoryCorrectionPendingStore();
    final client = reviewListClient((r) async => reviewWireResponse());
    final c = reviewController(client, store: store);
    addTearDown(c.dispose);
    await c.load();
    final m = c.memories.single;
    await c.readSelfReview(m);
    expect(reviewReadCount(client), 1);
    expect(c.selfReview!.conflictCategories, ['badminton']);
    expect(c.memories.single, same(m));
    expect(c.preview, isNull);
    expect(c.hasPending, isFalse);
    expect(await store.read(c.api.environment, reviewOwner), isNull);
    expect(c.canReview, isTrue);
  });
  for (final change in ['token', 'owner', 'org', 'ABA', 'close', 'source']) {
    test('同体读取：loading同步通知$change在真实send前关闭只读状态', () async {
      String? token = 'Bearer synthetic', owner = reviewOwner, workspace;
      final client = reviewListClient((r) async => reviewWireResponse());
      final c = reviewController(
        client,
        token: () => token,
        owner: () => owner,
        workspace: () => workspace,
      );
      addTearDown(c.dispose);
      await c.load();
      final m = c.memories.single;
      var armed = true;
      c.addListener(() {
        if (!armed || !c.selfReviewLoading) return;
        armed = false;
        switch (change) {
          case 'token':
            token = 'Bearer changed';
            break;
          case 'owner':
            owner = '40000000-0000-4000-8000-000000000099';
            break;
          case 'org':
            workspace = 'org';
            break;
          case 'ABA':
            owner = '40000000-0000-4000-8000-000000000099';
            c.identityChanged();
            owner = reviewOwner;
            break;
          case 'close':
            c.closeSelfReview();
            break;
          case 'source':
            c.memories = [
              CorrectableMemory.read(reviewMemoryRow(), reviewOwner),
            ];
            break;
        }
      });
      await c.readSelfReview(m);
      expect(reviewReadCount(client), 0);
      expect(c.selfReview, isNull);
      expect(c.selfReviewLoading, isFalse);
      if (!{'close', 'source'}.contains(change)) expect(c.retired, isTrue);
    });
  }
  for (final change in ['identityABA', 'close', 'reload', 'source']) {
    test('同体读取：$change后迟到原wire不能复活旧声明', () async {
      final gate = Completer<http.Response>();
      String? owner = reviewOwner;
      final client = reviewListClient((r) => gate.future);
      final c = reviewController(client, owner: () => owner);
      addTearDown(c.dispose);
      await c.load();
      final read = c.readSelfReview(c.memories.single);
      expect(reviewReadCount(client), 1);
      switch (change) {
        case 'identityABA':
          owner = '40000000-0000-4000-8000-000000000099';
          c.identityChanged();
          owner = reviewOwner;
          break;
        case 'close':
          c.closeSelfReview();
          break;
        case 'reload':
          await c.load();
          break;
        case 'source':
          c.memories = [CorrectableMemory.read(reviewMemoryRow(), reviewOwner)];
          break;
      }
      gate.complete(reviewWireResponse());
      await read;
      expect(c.selfReview, isNull);
      expect(c.selfReviewLoading, isFalse);
      expect(c.hasPending, isFalse);
    });
  }
  test('同体读取：迟到旧响应不覆盖最新错误和新serial', () async {
    final first = Completer<http.Response>();
    var calls = 0;
    final client = reviewListClient(
      (r) => ++calls == 1 ? first.future : Future.value(http.Response('', 503)),
    );
    final c = reviewController(client);
    addTearDown(c.dispose);
    await c.load();
    final m = c.memories.single, old = c.readSelfReview(c.memories.single);
    await c.readSelfReview(m);
    final message = c.selfReviewError;
    first.complete(reviewWireResponse());
    await old;
    expect(c.selfReviewError, message);
    expect(c.selfReviewError, contains('暂时无法'));
    expect(c.selfReview, isNull);
    expect(reviewReadCount(client), 2);
  });
  for (final reason in ['elapsed', 'utc', 'wait', 'rollback']) {
    test('同体读取：$reason不延长原shortlease或影响当前记忆', () async {
      var ticks = Duration.zero, now = reviewClock();
      final client = reviewListClient((r) async {
        if (reason == 'wait') ticks = const Duration(minutes: 3);
        return reviewWireResponse();
      });
      final c = reviewController(client, now: () => now, elapsed: () => ticks);
      addTearDown(c.dispose);
      await c.load();
      final m = c.memories.single;
      await c.readSelfReview(m);
      if (reason != 'wait') {
        expect(c.selfReview, isNotNull);
        ticks = const Duration(minutes: 2);
        if (reason == 'utc') now = now.add(const Duration(minutes: 3));
        if (reason == 'rollback') now = now.subtract(const Duration(hours: 1));
        c.expireSelfReview();
      }
      expect(c.selfReview, isNull);
      expect(c.selfReviewError, isNotNull);
      expect(c.memories.single, same(m));
      expect(c.hasPending, isFalse);
      expect(reviewReadCount(client), 1);
    });
  }
  for (final status in [401, 403, 409, 503]) {
    test('同体读取：HTTP$status是读取失败不是空结果或已执行', () async {
      final client = reviewListClient((r) async => http.Response('', status));
      final c = reviewController(client);
      addTearDown(c.dispose);
      await c.load();
      final m = c.memories.single;
      await c.readSelfReview(m);
      expect(c.selfReview, isNull);
      expect(c.selfReviewError, isNotNull);
      expect(c.memories.single, same(m));
      expect(c.hasPending, isFalse);
      expect(c.preview, isNull);
    });
  }
  test('同体读取：已有具体preview与journal成功失败到期均不改变批准', () async {
    var ticks = Duration.zero, status = 200;
    final store = MemoryAgentMemoryCorrectionPendingStore();
    final client = reviewListClient((r) async {
      if (r.url.path.endsWith('/self-review')) {
        return reviewWireResponse(status: status);
      }
      final input = jsonDecode((r as http.Request).body), m = reviewMemoryRow();
      return http.Response(
        jsonEncode({
          'schemaVersion': correctionSchema,
          'id': input['id'],
          'owner': {'type': 'PERSON', 'id': reviewOwner},
          'agentId': m['agentId'],
          'input': input,
          'memories': [m],
          'affected': [
            {'kind': 'MEMORY', 'id': m['id'], 'version': m['version']},
          ],
          'planDigest': 'b' * 64,
          'observedAt': reviewClock().toIso8601String(),
          'expiresAt': reviewClock()
              .add(const Duration(minutes: 5))
              .toIso8601String(),
          'explanation': correctionExplanation,
          'modelAccess': false,
        }),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    final c = reviewController(client, store: store, elapsed: () => ticks);
    addTearDown(c.dispose);
    await c.load();
    final m = c.memories.single;
    await c.reviewMemory(m, 'REJECT');
    final preview = c.preview,
        pending = await store.read(c.api.environment, reviewOwner);
    expect(c.canConfirm, isTrue);
    await c.readSelfReview(m);
    expect(c.selfReview, isNotNull);
    ticks = const Duration(minutes: 2);
    c.expireSelfReview();
    expect(c.preview, same(preview));
    expect(c.canConfirm, isTrue);
    status = 503;
    await c.readSelfReview(m);
    expect(c.preview, same(preview));
    expect(c.unknown, isFalse);
    expect(
      (await store.read(c.api.environment, reviewOwner))!.same(pending!),
      isTrue,
    );
    expect(
      client.requests.where(
        (r) => r.method == 'POST' && !r.url.path.endsWith('/self-review'),
      ),
      hasLength(1),
    );
  });
}
