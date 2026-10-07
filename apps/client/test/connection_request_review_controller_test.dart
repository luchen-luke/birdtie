import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_controller.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'connection_request_review_entry_test.dart';

Map<String, dynamic> decisionWire(Map<String, dynamic> list, String action) => {
  // Synthetic fixture mirrors original Go Request zero fields after Decide.
  'direction': '', 'otherAccountId': '', 'otherName': '',
  'cityId': list['cityId'] ?? '',
  for (final k in ['id', 'cityId', 'note', 'scope', 'createdAt', 'expiresAt'])
    if (list.containsKey(k)) k: list[k],
  'state': {
    'accept': 'accepted',
    'decline': 'declined',
    'withdraw': 'withdrawn',
  }[action],
  'conversationId': list['scope'] == 'conversation' && action == 'accept'
      ? '44444444-4444-4444-8444-444444444444'
      : '',
};

class ReviewHarness {
  ReviewHarness({
    this.postStatus = 200,
    this.postError,
    this.responseChange,
    this.beforeResponse,
    this.listErrorAfterPost = false,
    this.postBody,
    ConnectionReviewPendingStore? pendingStore,
  }) : pendingStore = pendingStore ?? MemoryConnectionReviewPendingStore() {
    client = TraceReviewMockClient((r) async {
      headers.add(r.headers['Authorization']);
      if (r.method == 'POST') {
        posts++;
        expect(r.url.path, '/v1/me/connection-requests/$reviewID/decision');
        final body = jsonDecode(r.body) as Map;
        expect(body.keys, ['action']);
        action = body['action'] as String;
        if (postError != null) throw postError!;
        if (beforeResponse != null) await beforeResponse!();
        final result = decisionWire(row, action!);
        responseChange?.call(result);
        final failure = postBody ?? (postStatus == 409 ? {'error': {'code': 'connection_conflict'}} : null);
        return failure == null ? reviewJSON(result, postStatus) : http.Response.bytes(
          utf8.encode(jsonEncode(failure)), postStatus,
          headers: {'Content-Type': 'application/json; charset=utf-8'},
        );
      }
      gets++;
      if (listErrorAfterPost && posts > 0) return reviewJSON({}, 503);
      return reviewJSON(missing ? [] : [row]);
    }, httpTrace);
    source = ConnectionSource(
      authorizationHeader: () {
        final hook = onSourceRead;
        onSourceRead = null;
        hook?.call();
        return sourceToken ?? token;
      },
      apiBaseUrl: 'https://original.fixture',
      client: client,
    );
    data = ConnectionRequestReviewController(
      requestID: reviewID,
      source: source,
      authorizationHeader: () => token,
      accountID: () => owner,
      organizationWorkspaceID: () => workspace,
      current: () {
        final hook = onBoundaryRead;
        onBoundaryRead = null;
        hook?.call();
        return valid;
      },
      now: () => DateTime.utc(2026, 10, 6, 9),
      pendingStore: this.pendingStore,
    );
  }
  String? owner = reviewOwner,
      token = 'Bearer synthetic-review-session',
      workspace,
      sourceToken;
  bool valid = true, missing = false;
  final int postStatus;
  final Object? postError;
  final void Function(Map<String, dynamic>)? responseChange;
  final Future<void> Function()? beforeResponse;
  final bool listErrorAfterPost;
  final Map<String, dynamic>? postBody;
  final ConnectionReviewPendingStore pendingStore;
  Map<String, dynamic> row = reviewRequest();
  int posts = 0, gets = 0;
  String? action;
  void Function()? onSourceRead;
  void Function()? onBoundaryRead;
  final List<String?> headers = [];
  final List<String> httpTrace = [];
  late final MockClient client;
  late final ConnectionSource source;
  late final ConnectionRequestReviewController data;
  void dispose() {
    data.dispose();
    source.dispose();
    client.close();
  }
}

class TraceReviewMockClient extends MockClient {
  TraceReviewMockClient(super.handler, this.trace);
  final List<String> trace;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    trace.add('send:${request.method}');
    return super.send(request);
  }
}

class ReviewFaultStore implements ConnectionReviewPendingStore {
  final inner = MemoryConnectionReviewPendingStore();
  bool failRead = false, failWrite = false, failDelete = false;
  Future<void> Function()? beforeRead, afterWrite, beforeDelete;
  @override
  Future<PendingConnectionReview?> read(String environment, String owner, String request) async {
    await beforeRead?.call();
    if (failRead) throw StateError('synthetic private storage failure');
    return inner.read(environment, owner, request);
  }
  @override
  Future<void> write(String environment, PendingConnectionReview value) async {
    if (failWrite) throw StateError('synthetic private storage failure');
    await inner.write(environment, value);
    await afterWrite?.call();
  }
  @override
  Future<bool> compareDelete(String environment, PendingConnectionReview value) async {
    await beforeDelete?.call();
    if (failDelete) throw StateError('synthetic private storage failure');
    return inner.compareDelete(environment, value);
  }
}

ConnectionRequestReviewController reopenReview(ReviewHarness h) => ConnectionRequestReviewController(
  requestID: reviewID, source: h.source, accountID: () => h.owner,
  authorizationHeader: () => h.token, organizationWorkspaceID: () => h.workspace,
  current: () => h.valid, now: () => DateTime.utc(2026, 10, 6, 9),
  pendingStore: h.pendingStore,
);

void main() {
  test('申请决定恢复：关闭重开同pending只能GET且不能复活旧批准', () async {
    final h = ReviewHarness(postStatus: 503);
    await h.data.load();
    await h.data.submit(h.data.preview('accept')!);
    expect(h.data.uncertain, true);
    expect(h.posts, 1);
    h.data.dispose();
    final reopened = ConnectionRequestReviewController(
      requestID: reviewID,
      source: h.source,
      authorizationHeader: () => h.token,
      accountID: () => h.owner,
      organizationWorkspaceID: () => h.workspace,
      current: () => h.valid,
      now: () => DateTime.utc(2026, 10, 6, 9),
      pendingStore: h.pendingStore,
    );
    addTearDown(() {
      reopened.dispose();
      h.source.dispose();
      h.client.close();
    });
    await reopened.load();
    expect(reopened.uncertain, true);
    expect(reopened.preview('accept'), null);
    expect(reopened.preview('decline'), null);
    expect(reopened.receipt, null);
    expect(reopened.message, contains('不是本次操作回执'));
    expect(h.posts, 1);
  });
  for (final status in [400, 401, 403, 404, 409, 422, 500, 503]) {
    test('申请决定恢复：HTTP$status无原拒绝code重开仍未知而非未生效', () async {
      final h = ReviewHarness(postStatus: status, postBody: {'error': {'code': 'message_policy_changed'}});
      await h.data.load();
      await h.data.submit(h.data.preview('accept')!);
      expect(h.data.uncertain, true);
      h.data.dispose();
      final reopened = reopenReview(h);
      addTearDown(() {reopened.dispose();h.source.dispose();h.client.close();});
      await reopened.load();
      expect(reopened.uncertain, true);
      expect(reopened.canAct('accept'), false);
      expect(reopened.receipt, null);
      expect(h.posts, 1);
    });
  }
  for (final state in ['pending', 'accepted', 'declined', 'withdrawn', 'expired', 'missing']) {
    test('申请决定恢复：当前$state不构成原操作回执或清除引用', () async {
      final h = ReviewHarness(postStatus: 503);
      await h.data.load();
      await h.data.submit(h.data.preview('accept')!);
      h.data.dispose();
      h.row = reviewRequest(state: state == 'missing' ? 'pending' : state);
      h.missing = state == 'missing';
      final reopened = reopenReview(h);
      addTearDown(() {reopened.dispose();h.source.dispose();h.client.close();});
      await reopened.load();
      await reopened.load();
      expect(reopened.uncertain, true);
      expect(reopened.pendingReference, isNotNull);
      expect(reopened.receipt, null);
      expect(reopened.preview('accept'), null);
      expect(reopened.message, contains('不是本次操作回执'));
      expect(h.posts, 1);
    });
  }
  for (final failure in ['read', 'write', 'delete']) {
    test('申请决定恢复：本机$failure失败阻止决定且不泄露原错误', () async {
      final store = ReviewFaultStore();
      final h = ReviewHarness(pendingStore: store);
      addTearDown(h.dispose);
      store.failRead = failure == 'read';
      await h.data.load();
      if (failure == 'read') {
        expect(h.data.preview('accept'), null);
      } else {
        store.failWrite = failure == 'write';
        store.failDelete = failure == 'delete';
        await h.data.submit(h.data.preview('accept')!);
      }
      expect(h.data.recoveryBlocked, true);
      expect(h.data.canAct('accept'), false);
      expect(h.data.message, isNot(contains('synthetic private')));
      expect(h.posts, failure == 'delete' ? 1 : 0);
      if (failure == 'delete') expect(h.data.receipt!.state, 'accepted');
    });
  }
  test('申请决定恢复：损坏本机引用failclosed而非空记录', () async {
    final store = MemoryConnectionReviewPendingStore(), h = ReviewHarness(pendingStore: store);
    addTearDown(h.dispose);
    await h.data.load();
    await h.data.submit(h.data.preview('accept')!);
    // A valid successful receipt cleared the first reference; generate UNKNOWN.
    final store2 = ReviewFaultStore(), second = ReviewHarness(postStatus: 503, pendingStore: store2);
    addTearDown(second.dispose);
    await second.data.load();
    await second.data.submit(second.data.preview('accept')!);
    store2.inner.values.updateAll((_, value) => '{corrupt');
    await second.data.load();
    expect(second.data.recoveryBlocked, true);
    expect(second.data.canAct('accept'), false);
    expect(second.posts, 1);
  });
  for (final boundary in ['token', 'owner', 'organization', 'source', 'boundary']) {
    test('申请决定恢复：落盘等待期间$boundary通知ABA永退且零POST', () async {
      final store = ReviewFaultStore(), h = ReviewHarness(pendingStore: store);
      addTearDown(h.dispose);
      await h.data.load();
      store.afterWrite = () async {
        switch (boundary) {
          case 'token': h.token = 'Bearer other';
          case 'owner': h.owner = reviewPeer;
          case 'organization': h.workspace = reviewPeer;
          case 'source': h.sourceToken = 'Bearer other';
          case 'boundary': h.valid = false;
        }
        h.data.synchronizeIdentity();
        h.token = 'Bearer synthetic-review-session'; h.owner = reviewOwner;
        h.workspace = null; h.sourceToken = null; h.valid = true;
      };
      await h.data.submit(h.data.preview('accept')!);
      expect(h.data.retired, true);
      expect(h.data.receipt, null);
      expect(h.posts, 0);
      expect(store.inner.values.length, 1);
    });
  }
  test('申请决定恢复：持久写后重新GET变化取消原批准但已确认未dispatch可清准确引用', () async {
    final store = ReviewFaultStore(), h = ReviewHarness(pendingStore: store);
    addTearDown(h.dispose);
    await h.data.load();
    store.afterWrite = () async {h.row['note'] = '服务中的说明已更新';};
    await h.data.submit(h.data.preview('accept')!);
    expect(h.posts, 0);
    expect(h.data.receipt, null);
    expect(h.data.uncertain, false);
    expect(store.inner.values, isEmpty);
    expect(h.data.message, contains('不沿用原确认'));
  });
  test('申请决定恢复：收到有效回执清理遇到新ref不清新记录并阻止重写', () async {
    final store = ReviewFaultStore(), h = ReviewHarness(pendingStore: store);
    addTearDown(h.dispose);
    await h.data.load();
    store.beforeDelete = () async {
      store.inner.values.updateAll((key, raw) => jsonEncode((jsonDecode(raw) as Map<String,dynamic>)
        ..['referenceId'] = reviewPeer..['action'] = 'decline'));
    };
    await h.data.submit(h.data.preview('accept')!);
    expect(h.data.receipt!.state, 'accepted');
    expect(h.data.uncertain, true);
    expect(h.data.recoveryBlocked, true);
    expect(h.posts, 1);
    expect(store.inner.values.values.single, contains(reviewPeer));
  });
  test('申请决定恢复：本机读取等待notify ABA退休不读取旧申请', () async {
    final store = ReviewFaultStore(), h = ReviewHarness(pendingStore: store);
    addTearDown(h.dispose);
    store.beforeRead = () async {
      h.workspace = reviewPeer;
      h.data.synchronizeIdentity();
      h.workspace = null;
    };
    await h.data.load();
    expect(h.data.retired, true);
    expect(h.data.pendingReference, null);
    expect(h.data.request, null);
    expect(h.gets, 0);
    expect(h.posts, 0);
  });
  test('申请决定恢复：同步send先于notify ABA时保已发GET但不接受迟到正文或批准', () async {
    final store = ReviewFaultStore(), h = ReviewHarness(pendingStore: store);
    addTearDown(h.dispose);
    store.beforeRead = () async {
      h.onBoundaryRead = () {
        scheduleMicrotask(() {
          h.httpTrace.add('retirement_notification');
          h.workspace = reviewPeer;
          h.data.synchronizeIdentity();
          h.workspace = null;
        });
      };
    };
    await h.data.load();
    debugPrintSynchronously(('Unit synchronous client send ordering: ${h.httpTrace}').toString());
    expect(h.data.retired, true);
    expect(h.httpTrace, ['send:GET', 'retirement_notification']);
    expect(h.gets, 1);
    expect(h.posts, 0);
    expect(h.data.request, null);
    expect(h.data.receipt, null);
    expect(h.data.preview('accept'), null);
  });
  test('申请决定恢复：新合法session可核实本人原引用而不恢复批准', () async {
    final h = ReviewHarness(postStatus: 503);
    await h.data.load();
    await h.data.submit(h.data.preview('accept')!);
    h.data.dispose();
    h.token = 'Bearer new-valid-fixture-session';
    final reopened = reopenReview(h);
    addTearDown(() {reopened.dispose();h.source.dispose();h.client.close();});
    await reopened.load();
    expect(reopened.retired, false);
    expect(reopened.uncertain, true);
    expect(reopened.preview('accept'), null);
    expect(reopened.receipt, null);
    expect(h.headers.last, h.token);
    expect(h.posts, 1);
  });
  test('申请决定恢复：原invalid_decision400拒绝code才能清引用，状态400本身不够', () async {
    final store = MemoryConnectionReviewPendingStore(), h = ReviewHarness(
      pendingStore: store, postStatus: 400,
      postBody: {'error': {'code': 'invalid_decision'}},
    );
    addTearDown(h.dispose);
    await h.data.load();
    await h.data.submit(h.data.preview('accept')!);
    expect(h.data.uncertain, false);
    expect(h.data.receipt, null);
    expect(store.values, isEmpty);
    expect(h.posts, 1);
  });
  test('同步来源getter notify ABA后旧批准零POST，字段先捕获', () async {
    final h = ReviewHarness();
    addTearDown(h.dispose);
    await h.data.load();
    final approval = h.data.preview('accept')!;
    h.onSourceRead = () {
      h.workspace = reviewPeer;
      h.data.synchronizeIdentity();
      h.workspace = null;
    };
    await h.data.submit(approval);
    expect(h.data.retired, true);
    expect(h.posts, 0);
    expect(h.data.receipt, null);
  });
  for (final action in ['accept', 'decline', 'withdraw']) {
    for (final scope in ['friend', 'conversation']) {
      test('原申请具体人工操作 $scope/$action，真实小DTO，无自动聊天', () async {
        final h = ReviewHarness();
        addTearDown(h.dispose);
        h.row['scope'] = scope;
        h.row['direction'] = action == 'withdraw' ? 'outgoing' : 'incoming';
        await h.data.load();
        expect(h.posts, 0);
        expect(h.data.request!.id, reviewID);
        final approval = h.data.preview(action)!;
        await h.data.submit(approval);
        expect(h.posts, 1);
        expect(h.gets, 2);
        expect(h.data.receipt!.id, reviewID);
        expect(h.data.receipt!.state, decisionWire(h.row, action)['state']);
        expect(h.data.uncertain, false);
        await h.data.submit(approval);
        expect(h.posts, 1);
        expect(
          h.headers.every((v) => v == 'Bearer synthetic-review-session'),
          true,
        );
        expect(h.data.canAct(action), false);
      });
    }
  }
  for (final status in [401, 403, 404, 500, 503]) {
    test('POST $status结果未知；当前同state不是回执，GET-only', () async {
      final h = ReviewHarness(postStatus: status);
      addTearDown(h.dispose);
      await h.data.load();
      await h.data.submit(h.data.preview('accept')!);
      expect(h.data.uncertain, true);
      expect(h.data.receipt, null);
      h.row = reviewRequest(state: 'accepted');
      await h.data.load();
      expect(h.data.request!.state, 'accepted');
      expect(h.data.receipt, null);
      expect(h.data.message, contains('不是本次操作回执'));
      expect(h.data.preview('accept'), null);
      expect(h.posts, 1);
    });
  }
  for (final error in [
    TimeoutException('synthetic timeout'),
    const FormatException('raw private canary'),
  ]) {
    test('传输/未解码结果未知且不输出原错误：${error.runtimeType}', () async {
      final h = ReviewHarness(postError: error);
      addTearDown(h.dispose);
      await h.data.load();
      await h.data.submit(h.data.preview('accept')!);
      expect(h.data.uncertain, true);
      expect(h.data.message, isNot(contains('canary')));
      h.missing = true;
      await h.data.load();
      expect(h.data.receipt, null);
      expect(h.posts, 1);
      expect(h.data.preview('accept'), null);
    });
  }
  for (final field in [
    'id',
    'state',
    'scope',
    'note',
    'createdAt',
    'expiresAt',
    'conversationId',
    'otherAccountId',
    'confirmed',
  ]) {
    test('伪小DTO拒绝错绑 $field，UNKNOWN不重发', () async {
      final h = ReviewHarness(
        responseChange: (r) => r[field] = 'invalid-canary',
      );
      addTearDown(h.dispose);
      await h.data.load();
      await h.data.submit(h.data.preview('accept')!);
      expect(h.data.receipt, null);
      expect(h.data.uncertain, true);
      expect(h.posts, 1);
    });
  }
  for (final field in ['token', 'owner', 'workspace', 'source', 'boundary']) {
    test('真实notify后 $field ABA旧批准永久退役，零POST', () async {
      final h = ReviewHarness();
      addTearDown(h.dispose);
      await h.data.load();
      final approval = h.data.preview('accept')!;
      switch (field) {
        case 'token':
          h.token = 'Bearer another';
        case 'owner':
          h.owner = reviewPeer;
        case 'workspace':
          h.workspace = reviewPeer;
        case 'source':
          h.sourceToken = 'Bearer another';
        case 'boundary':
          h.valid = false;
      }
      h.data.synchronizeIdentity();
      h.token = 'Bearer synthetic-review-session';
      h.owner = reviewOwner;
      h.workspace = null;
      h.sourceToken = null;
      h.valid = true;
      await h.data.submit(approval);
      await h.data.load();
      expect(h.data.retired, true);
      expect(h.data.request, null);
      expect(h.posts, 0);
    });
  }
  test('审批前重新GET来源变化/缺项不提交', () async {
    for (final change in ['note', 'state', 'expiry', 'missing']) {
      final h = ReviewHarness();
      await h.data.load();
      final approval = h.data.preview('accept')!;
      switch (change) {
        case 'note':
          h.row['note'] = '修改后的说明';
        case 'state':
          h.row = reviewRequest(state: 'withdrawn');
        case 'expiry':
          h.row['expiresAt'] = '2026-10-06T08:30:00Z';
        case 'missing':
          h.missing = true;
      }
      await h.data.submit(approval);
      expect(h.posts, 0);
      expect(h.data.receipt, null);
      h.dispose();
    }
  });
  test('已收200回执与刷新503区分，不重POST', () async {
    final h = ReviewHarness(listErrorAfterPost: true);
    addTearDown(h.dispose);
    await h.data.load();
    await h.data.submit(h.data.preview('accept')!);
    await h.data.load();
    expect(h.data.receipt!.state, 'accepted');
    expect(h.data.uncertain, false);
    expect(h.data.message, contains('列表刷新失败'));
    expect(h.posts, 1);
  });
  test('迟到200不能回到原主体/批准', () async {
    final waiting = Completer<void>();
    final h = ReviewHarness(beforeResponse: () => waiting.future);
    addTearDown(h.dispose);
    await h.data.load();
    final future = h.data.submit(h.data.preview('accept')!);
    await Future<void>.delayed(Duration.zero);
    h.workspace = reviewPeer;
    h.data.synchronizeIdentity();
    h.workspace = null;
    waiting.complete();
    await future;
    expect(h.data.retired, true);
    expect(h.data.receipt, null);
    expect(h.posts, 1);
  });
  test('错误方向/过期/终态不可批准', () async {
    final h = ReviewHarness();
    addTearDown(h.dispose);
    await h.data.load();
    expect(h.data.preview('withdraw'), null);
    h.row = reviewRequest(state: 'expired');
    await h.data.load();
    expect(h.data.preview('accept'), null);
    expect(h.posts, 0);
    h.row = reviewRequest();
    h.row['expiresAt'] = '2026-10-06T08:30:00Z';
    await h.data.load();
    expect(h.data.preview('accept'), null);
  });
  test('409重读重审，原批准无效而非假成功', () async {
    final h = ReviewHarness(postStatus: 409);
    addTearDown(h.dispose);
    await h.data.load();
    final old = h.data.preview('accept')!;
    await h.data.submit(old);
    expect(h.data.uncertain, false);
    expect(h.data.receipt, null);
    await h.data.submit(old);
    expect(h.posts, 1);
    await h.data.load();
    expect(h.data.preview('accept'), isNotNull);
  });
}
