import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_pending_store.dart';
import 'package:birdtie_client/src/workspace/enrichment_privacy_api.dart';
import 'package:birdtie_client/src/workspace/enrichment_privacy_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

// Immutable actual registered HTTP wires from Go02 with a synthetic Store.
// Not PostgreSQL/session/production evidence. Original timestamps unchanged.
const privacyOwner = '80000000-0000-4000-8000-000000000001';
const privacyPath = '/v1/me/agent-enrichment-purpose/grants';
List<int> privacyWire(String name) => File(
  '../../work/enrichment-privacy-control-2026-10-07/wires/$name.json',
).readAsBytesSync();
Map<String, dynamic> privacyRaw() =>
    jsonDecode(utf8.decode(privacyWire('inventory'))) as Map<String, dynamic>;
DateTime privacyNow() => DateTime.parse(
  privacyRaw()['data']['observedAt'],
).toUtc().add(const Duration(milliseconds: 1));

class PrivacyWire extends http.BaseClient {
  final sent =
      <
        ({String method, String path, String host, String? auth, String body})
      >[];
  Completer<http.StreamedResponse>? listGate, readGate, deleteGate;
  Object? listValue;
  bool withdrawn = false, closed = false;
  int deleteStatus = 200;
  void Function(http.BaseRequest)? entering;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    sent.add((
      method: r.method,
      path: r.url.path,
      host: r.url.host,
      auth: r.headers['Authorization'],
      body: r is http.Request ? r.body : '',
    ));
    entering?.call(r);
    if (r.method == 'DELETE') {
      if (deleteGate != null) return deleteGate!.future;
      if (deleteStatus != 200) {
        return Future.value(
          reply({
            'error': {'code': 'enrichment_purpose_changed'},
          }, status: deleteStatus),
        );
      }
      withdrawn = true;
      return Future.value(rawReply(privacyWire('revoke')));
    }
    if (r.url.path == privacyPath) {
      if (listGate != null) return listGate!.future;
      if (listValue != null) return Future.value(reply(listValue!));
      if (!withdrawn) return Future.value(rawReply(privacyWire('inventory')));
      // Explicit synthetic transition: original registered revoke DTO in a
      // list envelope. It is not another claimed native/registered response.
      final v = privacyRaw();
      v['data']['grants'] = [jsonDecode(utf8.decode(privacyWire('revoke')))];
      return Future.value(reply(v));
    }
    if (readGate != null) return readGate!.future;
    final v = withdrawn
        ? jsonDecode(utf8.decode(privacyWire('revoke')))
        : privacyRaw()['data']['grants'][0];
    return Future.value(reply(v));
  }

  static http.StreamedResponse rawReply(List<int> raw) => http.StreamedResponse(
    Stream.value(raw),
    200,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );
  static http.StreamedResponse reply(Object value, {int status = 200}) =>
      http.StreamedResponse(
        Stream.value(utf8.encode(jsonEncode(value))),
        status,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
  @override
  void close() {
    closed = true;
    super.close();
  }
}

class _WriteGate extends MemoryAgentMultiCandidatePendingStore {
  final gate = Completer<void>();
  bool entered = false;
  @override
  Future<void> write(
    String env,
    String owner,
    PendingMultiCandidateOperation value,
  ) async {
    entered = true;
    await gate.future;
    await super.write(env, owner, value);
  }
}

EnrichmentPrivacyController privacyController(
  PrivacyWire wire, {
  AgentMultiCandidatePendingStore? store,
  String? Function()? auth,
  String? Function()? owner,
  String? Function()? org,
  bool Function()? current,
  DateTime Function()? now,
}) => EnrichmentPrivacyController(
  api: EnrichmentPrivacyAPI(client: wire, apiBaseUrl: 'https://privacy.test'),
  authorizationHeader: auth ?? (() => 'Bearer privacy-fixture'),
  ownerID: owner ?? (() => privacyOwner),
  organizationWorkspaceID: org,
  current: current,
  pendingStore: store ?? MemoryAgentMultiCandidatePendingStore(),
  now: now ?? privacyNow,
);
void main() {
  test('原注册清单wire严格解析本人同快照，不授权模型或保留', () async {
    final w = PrivacyWire(), c = privacyController(PrivacyWire());
    final api = EnrichmentPrivacyAPI(
      client: w,
      apiBaseUrl: 'https://privacy.test',
    );
    final v = await api.list('Bearer privacy-fixture', privacyOwner);
    expect(v.grants.length, 1);
    expect(v.grants.single.multi, false);
    expect(v.observedAt, DateTime.parse(privacyRaw()['data']['observedAt']));
    expect(w.sent.single.method, 'GET');
    expect(w.sent.single.body, isEmpty);
    for (final change in <void Function(Map<String, dynamic>)>[
      (m) => m['owner']['id'] = v.agentID,
      (m) => m['agentId'] = privacyOwner,
      (m) => m['grants'][0]['purpose'] = 'MODEL_EGRESS',
      (m) => m['grants'][0]['content'] = 'HIDDEN_BODY_CANARY',
      (m) => m['grants'][0]['modelAccess'] = true,
      (m) => m['grants'][0]['candidateRetentionAllowed'] = true,
      (m) => m['grants'][0]['observedAt'] = '2026-10-07T00:00:00Z',
      (m) => m['truncated'] = true,
      (m) => m['grants'].add(m['grants'][0]),
    ]) {
      final m = privacyRaw()['data'] as Map<String, dynamic>;
      change(m);
      expect(
        () => EnrichmentPrivacyInventory.read(m, privacyOwner),
        throwsA(anything),
      );
    }
    c.dispose();
    api.dispose();
    expect(w.closed, false);
    w.close();
  });
  test('具体版本撤回先落原journal再原GET和DELETE，重新读取不恢复批准', () async {
    final w = PrivacyWire(), store = MemoryAgentMultiCandidatePendingStore();
    final c = privacyController(w, store: store);
    await c.load();
    final a = c.preview(c.inventory!.grants.single)!;
    w.entering = (r) {
      if (r.method == 'DELETE') {
        expect(store.values.length, 1);
        expect(jsonDecode((r as http.Request).body), {'expectedRevision': 1});
      }
    };
    await c.withdraw(a);
    expect(w.sent.map((r) => r.method).toList(), [
      'GET',
      'GET',
      'DELETE',
      'GET',
    ]);
    expect(c.inventory!.grants.single.revoked, true);
    expect(c.approval, isNull);
    expect(store.values, isEmpty);
    expect(c.notice, contains('已经消费'));
    expect(w.sent.every((r) => r.auth == 'Bearer privacy-fixture'), true);
    c.dispose();
    w.close();
  });
  test('撤回409未知跨重开仅核实原ID，不按缺项或相同列表自动重发', () async {
    final w = PrivacyWire()..deleteStatus = 409,
        store = MemoryAgentMultiCandidatePendingStore();
    var c = privacyController(w, store: store);
    await c.load();
    await c.withdraw(c.preview(c.inventory!.grants.single)!);
    expect(c.error, contains('未知'));
    expect(store.values.length, 1);
    expect(w.sent.where((r) => r.method == 'DELETE').length, 1);
    c.dispose();
    c = privacyController(w, store: store);
    await c.load();
    expect(c.pending.length, 1);
    expect(c.preview(c.inventory!.grants.single), isNull);
    await c.verifyPending(c.pending.single);
    expect(store.values.length, 1);
    expect(w.sent.where((r) => r.method == 'DELETE').length, 1);
    expect(c.notice, contains('不能据此断言'));
    expect(c.notice, contains('原操作当前状态：尚未撤回'));
    c.dispose();
    w.close();
  });
  test('存储await时本人身份ABA永久退休，旧批准零DELETE', () async {
    final w = PrivacyWire(), store = _WriteGate();
    var auth = 'Bearer privacy-fixture';
    final c = privacyController(w, store: store, auth: () => auth);
    await c.load();
    final a = c.preview(c.inventory!.grants.single)!;
    final f = c.withdraw(a);
    await Future<void>.delayed(Duration.zero);
    expect(store.entered, true);
    auth = 'Bearer other';
    c.synchronizeIdentity();
    auth = 'Bearer privacy-fixture';
    c.synchronizeIdentity();
    store.gate.complete();
    await f;
    expect(c.retired, true);
    expect(c.inventory, isNull);
    expect(w.sent.where((r) => r.method == 'DELETE'), isEmpty);
    c.dispose();
    w.close();
  });
  test('入口source退役和晚读取不产生新请求或污染界面', () async {
    final w = PrivacyWire()..listGate = Completer<http.StreamedResponse>();
    var active = true;
    final c = privacyController(w, current: () => active);
    final f = c.load();
    await Future<void>.delayed(Duration.zero);
    active = false;
    c.synchronizeIdentity();
    active = true;
    c.synchronizeIdentity();
    w.listGate!.complete(PrivacyWire.rawReply(privacyWire('inventory')));
    await f;
    expect(c.inventory, isNull);
    expect(c.retired, true);
    await c.load();
    expect(w.sent.length, 1);
    c.dispose();
    w.close();
  });
  test('清单短读取期限不被许可到期替代，许可历史到期不隐匿', () async {
    final w = PrivacyWire();
    var now = privacyNow();
    final c = privacyController(w, now: () => now);
    await c.load();
    final original = c.inventory!.grants.single;
    now = now.add(const Duration(seconds: 30));
    expect(c.fresh, false);
    expect(c.preview(original), isNull);
    expect(w.sent.length, 1);
    final v = privacyRaw()['data'] as Map<String, dynamic>;
    v['grants'][0]['expiresAt'] = DateTime.parse(
      v['observedAt'],
    ).subtract(const Duration(seconds: 1)).toIso8601String();
    v['grants'][0]['selection']['deadlineAt'] = v['grants'][0]['expiresAt'];
    expect(EnrichmentPrivacyInventory.read(v, privacyOwner).grants.length, 1);
    c.dispose();
    w.close();
  });
  test('原GET后入口退休阻止DELETE；已发DELETE晚回不撤销或假成功', () async {
    final w = PrivacyWire(), store = MemoryAgentMultiCandidatePendingStore();
    var active = true;
    final c = privacyController(w, store: store, current: () => active);
    await c.load();
    w.readGate = Completer<http.StreamedResponse>();
    final f = c.withdraw(c.preview(c.inventory!.grants.single)!);
    await Future<void>.delayed(Duration.zero);
    active = false;
    c.synchronizeIdentity();
    w.readGate!.complete(PrivacyWire.reply(privacyRaw()['data']['grants'][0]));
    await f;
    expect(w.sent.where((r) => r.method == 'DELETE'), isEmpty);
    expect(store.values.length, 1);
    c.dispose();
    w.close();
    final late = PrivacyWire()..deleteGate = Completer<http.StreamedResponse>();
    active = true;
    final d = privacyController(late, current: () => active);
    await d.load();
    final submit = d.withdraw(d.preview(d.inventory!.grants.single)!);
    await Future<void>.delayed(Duration.zero);
    expect(late.sent.where((r) => r.method == 'DELETE').length, 1);
    active = false;
    d.synchronizeIdentity();
    late.deleteGate!.complete(PrivacyWire.rawReply(privacyWire('revoke')));
    await submit;
    expect(d.notice, isNull);
    expect(d.inventory, isNull);
    expect(late.sent.where((r) => r.method == 'DELETE').length, 1);
    d.dispose();
    late.close();
  });
}
