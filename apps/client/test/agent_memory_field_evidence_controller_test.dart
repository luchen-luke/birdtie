import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_memory_correction_api.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_controller.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_memory_correction_api_test.dart';
import 'agent_memory_field_evidence_test.dart' show memoryDetailWire;

// Count the actual synchronous send boundary, not MockClient's async handler.
class FieldSendClient extends http.BaseClient {
  FieldSendClient(this.detail);
  Future<http.Response> Function(http.BaseRequest) detail;
  final dispatch = <String>[];
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    dispatch.add('${r.method} ${r.url.path}');
    final response = r.url.path.endsWith(correctionMemory) || r.method == 'POST'
        ? detail(r)
        : Future.value(
            correctionResponse({
              'data': r.url.path.endsWith('agent-memories')
                  ? [correctionMemoryRaw()]
                  : [],
            }),
          );
    return response.then(
      (v) => http.StreamedResponse(Stream.value(v.bodyBytes), v.statusCode),
    );
  }

  int get details => dispatch
      .where((v) => v.endsWith('/agent-memories/$correctionMemory'))
      .length;
}

AgentMemoryCorrectionController fieldController(
  FieldSendClient client, {
  String? Function()? token,
  String? Function()? owner,
  String? Function()? workspace,
  DateTime Function()? now,
  Duration Function()? elapsed,
  AgentMemoryCorrectionPendingStore? store,
}) => AgentMemoryCorrectionController(
  authorizationHeader: token ?? () => 'Bearer synthetic',
  accountID: owner ?? () => correctionOwner,
  organizationWorkspaceID: workspace,
  now: now ?? () => correctionAt,
  elapsed: elapsed,
  pendingStore: store ?? MemoryAgentMemoryCorrectionPendingStore(),
  api: AgentMemoryCorrectionAPI(client: client, apiBaseUrl: 'http://local'),
);

void main() {
  test('来源读取：正常本人仅一次GET，不创建批准或待核实操作', () async {
    final cl = FieldSendClient(
      (r) async => correctionResponse({'data': memoryDetailWire()}),
    );
    final c = fieldController(cl);
    addTearDown(c.dispose);
    await c.load();
    final m = c.memories.single;
    await c.readFieldEvidence(m);
    expect(c.fieldDetail!.memory!.summary, m.summary);
    expect(cl.details, 1);
    expect(c.canReview, isTrue);
    expect(c.hasPending, isFalse);
    expect(c.canConfirm, isFalse);
    expect(cl.dispatch.where((v) => v.startsWith('POST')), isEmpty);
  });
  for (final change in ['token', 'owner', 'workspace']) {
    test('来源读取：busy同步通知$change退休在真实send前，零旧GET', () async {
      var token = 'Bearer synthetic', owner = correctionOwner;
      String? workspace;
      final cl = FieldSendClient(
        (r) async => correctionResponse({'data': memoryDetailWire()}),
      );
      final c = fieldController(
        cl,
        token: () => token,
        owner: () => owner,
        workspace: () => workspace,
      );
      addTearDown(c.dispose);
      await c.load();
      final m = c.memories.single;
      c.addListener(() {
        if (!c.fieldLoading) return;
        switch (change) {
          case 'token':
            token = 'Bearer other';
            break;
          case 'owner':
            owner = correctionID;
            break;
          default:
            workspace = correctionID;
        }
        c.identityChanged();
      });
      await c.readFieldEvidence(m);
      expect(c.retired, isTrue);
      expect(cl.details, 0);
      expect(c.fieldDetail, isNull);
      expect(c.fieldLoading, isFalse);
    });
  }
  for (final late in ['success', 'failure']) {
    for (final reason in ['close', 'refresh', 'ABA']) {
      test('来源读取：$reason后迟到$late不能复活只读详情', () async {
        final pending = Completer<http.Response>();
        var token = 'Bearer synthetic';
        final cl = FieldSendClient((r) => pending.future);
        final c = fieldController(cl, token: () => token);
        await c.load();
        final m = c.memories.single;
        final reading = c.readFieldEvidence(m);
        expect(cl.details, 1);
        if (reason == 'close') {
          c.dispose();
        } else if (reason == 'refresh') {
          await c.load();
        } else {
          token = 'Bearer B';
          c.identityChanged();
          token = 'Bearer synthetic';
          c.identityChanged();
        }
        pending.complete(
          late == 'success'
              ? correctionResponse({'data': memoryDetailWire()})
              : http.Response('', 503),
        );
        await reading;
        expect(c.fieldDetail, isNull);
        expect(c.fieldLoading, isFalse);
        if (reason == 'refresh') expect(c.canReview, isTrue);
        if (reason != 'close') c.dispose();
      });
    }
  }
  test('来源读取：连续选择同记录，迟到旧响应不覆盖当前错误', () async {
    final old = Completer<http.Response>();
    var calls = 0;
    final cl = FieldSendClient(
      (r) => ++calls == 1 ? old.future : Future.value(http.Response('', 403)),
    );
    final c = fieldController(cl);
    addTearDown(c.dispose);
    await c.load();
    final m = c.memories.single, first = c.readFieldEvidence(c.memories.single);
    await c.readFieldEvidence(m);
    final error = c.fieldError;
    old.complete(correctionResponse({'data': memoryDetailWire()}));
    await first;
    expect(c.fieldDetail, isNull);
    expect(c.fieldError, error);
    expect(error, contains('没有查看权限'));
  });
  for (final wrong in ['new-version', 'same-version-new-body']) {
    test('来源读取：$wrong不能混入旧列表正文或批准', () async {
      final m = correctionMemoryRaw();
      if (wrong == 'new-version') {
        m['version'] = 2;
      } else {
        m['summary'] = '另一版正文';
      }
      final cl = FieldSendClient(
        (r) async => correctionResponse({'data': memoryDetailWire(memory: m)}),
      );
      final c = fieldController(cl);
      addTearDown(c.dispose);
      await c.load();
      final selected = c.memories.single;
      await c.readFieldEvidence(selected);
      expect(c.fieldDetail, isNull);
      expect(c.fieldError, contains('重新读取当前记忆'));
      expect(c.memories.single, same(selected));
      expect(c.memories.single.summary, '我偏好徒步活动');
      expect(c.hasPending, isFalse);
    });
  }
  for (final status in [401, 403, 404, 409, 503]) {
    test('来源读取：HTTP$status只改变详情错误，不清原记忆或制造空成功', () async {
      final cl = FieldSendClient((r) async => http.Response('', status));
      final c = fieldController(cl);
      addTearDown(c.dispose);
      await c.load();
      final selected = c.memories.single;
      await c.readFieldEvidence(selected);
      expect(c.fieldDetail, isNull);
      expect(c.fieldError, isNotNull);
      expect(c.fieldLoading, isFalse);
      expect(c.memories.single, same(selected));
      expect(c.canConfirm, isFalse);
      expect(c.hasPending, isFalse);
    });
  }
  test('来源读取：网络失败后本人明确重新GET，仍无POST', () async {
    var fail = true;
    final cl = FieldSendClient((r) async {
      if (fail) throw http.ClientException('synthetic');
      return correctionResponse({'data': memoryDetailWire()});
    });
    final c = fieldController(cl);
    addTearDown(c.dispose);
    await c.load();
    final m = c.memories.single;
    await c.readFieldEvidence(m);
    expect(c.fieldError, contains('再次查看'));
    fail = false;
    await c.readFieldEvidence(m);
    expect(c.fieldDetail, isNotNull);
    expect(cl.details, 2);
    expect(cl.dispatch.where((v) => v.startsWith('POST')), isEmpty);
  });
  test('来源读取：详情到期及时钟回拨仅丢弃详情，不清更正材料', () async {
    var ticks = Duration.zero, now = correctionAt;
    final cl = FieldSendClient(
      (r) async => correctionResponse({'data': memoryDetailWire()}),
    );
    final c = fieldController(cl, now: () => now, elapsed: () => ticks);
    addTearDown(c.dispose);
    await c.load();
    final m = c.memories.single;
    await c.readFieldEvidence(m);
    ticks = const Duration(minutes: 2);
    now = correctionAt.subtract(const Duration(days: 1));
    c.expireFieldEvidence();
    expect(c.fieldDetail, isNull);
    expect(c.fieldError, contains('已到期'));
    expect(c.memories.single, same(m));
    expect(c.hasPending, isFalse);
    expect(c.canReview, isTrue);
  });
  test('来源读取：响应等待耗尽原租期，未来UTC不能延长单调deadline', () async {
    var ticks = Duration.zero;
    final cl = FieldSendClient((r) async {
      ticks = const Duration(minutes: 3);
      return correctionResponse({'data': memoryDetailWire()});
    });
    final c = fieldController(cl, elapsed: () => ticks);
    addTearDown(c.dispose);
    await c.load();
    await c.readFieldEvidence(c.memories.single);
    expect(c.fieldDetail, isNull);
    expect(c.fieldError, isNotNull);
  });
  test('来源读取：已有具体preview与journal不因只读成功失败到期改变', () async {
    var detailStatus = 200, ticks = Duration.zero;
    final store = MemoryAgentMemoryCorrectionPendingStore();
    final cl = FieldSendClient((r) async {
      if (r.method == 'POST') {
        return correctionResponse(
          correctionPreviewRaw(input: jsonDecode((r as http.Request).body)),
        );
      }
      return detailStatus == 200
          ? correctionResponse({'data': memoryDetailWire()})
          : http.Response('', detailStatus);
    });
    final c = fieldController(cl, store: store, elapsed: () => ticks);
    addTearDown(c.dispose);
    await c.load();
    final m = c.memories.single;
    await c.reviewMemory(m, 'REJECT');
    final preview = c.preview;
    final p = await store.read(c.api.environment, correctionOwner);
    expect(c.canConfirm, isTrue);
    await c.readFieldEvidence(m);
    expect(c.fieldDetail, isNotNull);
    ticks = const Duration(minutes: 2);
    c.expireFieldEvidence();
    expect(c.preview, same(preview));
    expect(c.canConfirm, isTrue);
    detailStatus = 503;
    await c.readFieldEvidence(m);
    expect(c.preview, same(preview));
    expect(c.hasPending, isTrue);
    expect(c.unknown, isFalse);
    expect(
      (await store.read(c.api.environment, correctionOwner))!.same(p!),
      isTrue,
    );
    expect(cl.dispatch.where((v) => v.startsWith('POST')).length, 1);
  });
}
