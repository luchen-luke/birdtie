import 'dart:async';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:birdtie_client/src/workspace/business_agent_identity_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'business_agent_identity_api_test.dart'
    show
        identityWire,
        identityReply,
        identityBusiness,
        identityPerson,
        identityAgent;

class IdentitySendSpy extends http.BaseClient {
  IdentitySendSpy(this.next);
  final Future<http.Response> Function(http.BaseRequest) next;
  final calls = <http.BaseRequest>[];
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    calls.add(r);
    return next(r).then(
      (v) => http.StreamedResponse(Stream.value(v.bodyBytes), v.statusCode),
    );
  }
}

class IdentityFixture {
  IdentityFixture(
    Future<http.Response> Function(http.BaseRequest) reply, {
    DateTime? at,
  }) {
    now = at ?? DateTime.now().toUtc();
    client = IdentitySendSpy(reply);
    api = BusinessApi(
      authorizationHeader: () => token,
      apiBaseUrl: 'https://business.test',
      client: client,
    );
    c = BusinessAgentIdentityController(
      api: api,
      businessID: identityBusiness,
      accountID: () => owner,
      authorizationHeader: () => token,
      workspaceID: () => workspace,
      currentBusinessID: () => identityBusiness,
      bindingCurrent: () => current,
      sourceFrame: () => source,
      identityChanges: changes,
      now: () => now,
    );
  }
  String? token = 'Bearer contract-person', owner = identityPerson, workspace;
  bool current = true;
  Object source = Object();
  late DateTime now;
  final changes = ChangeNotifier();
  late final BusinessApi api;
  late final IdentitySendSpy client;
  late final BusinessAgentIdentityController c;
  void dispose() {
    c.dispose();
    changes.dispose();
    api.dispose();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('明确版本检查后只建立暂停身份，旧批准不能再次提交', () async {
    final at = DateTime.now().toUtc();
    final f = IdentityFixture(
      (r) async =>
          identityReply(identityWire(exists: r.method == 'POST', now: at)),
      at: at,
    );
    await f.c.load();
    expect(f.c.prepareReview(), true);
    final approved = f.c.review!;
    await f.c.establish(approved);
    expect(f.c.value!.agentID, identityAgent);
    expect(f.c.message, contains('保持暂停'));
    await f.c.establish(approved);
    expect(f.client.calls.where((r) => r.method == 'POST').length, 1);
    f.dispose();
  });
  test('409须重新GET和新版本检查，未知只能核实不盲重POST', () async {
    final at = DateTime.now().toUtc();
    var status = 409;
    var version = 2;
    var exists = false;
    final f = IdentityFixture(
      (r) async => r.method == 'POST'
          ? http.Response('{}', status)
          : identityReply(
              identityWire(exists: exists, version: version, now: at),
            ),
      at: at,
    );
    await f.c.load();
    f.c.prepareReview();
    final old = f.c.review!;
    await f.c.establish(old);
    expect(f.c.uncertain, false);
    expect(f.c.value, isNull);
    version = 3;
    await f.c.load();
    await f.c.establish(old);
    expect(f.client.calls.where((r) => r.method == 'POST').length, 1);
    expect(f.c.prepareReview(), true);
    status = 503;
    await f.c.establish(f.c.review!);
    expect(f.c.uncertain, true);
    exists = true;
    await f.c.load();
    expect(f.c.value!.agentID, identityAgent);
    expect(f.c.message, contains('不是此前操作'));
    expect(f.c.prepareReview(), false);
    expect(f.client.calls.where((r) => r.method == 'POST').length, 2);
    f.dispose();
  });
  test('同步busy通知来源退休不发wire，同帧ABA不能复活', () async {
    final f = IdentityFixture((r) async => identityReply(identityWire()));
    f.c.addListener(() {
      if (f.c.busy) {
        f.source = Object();
        f.changes.notifyListeners();
      }
    });
    await f.c.load();
    expect(f.client.calls, isEmpty);
    expect(f.c.permitted, false);
    expect(f.c.value, isNull);
    f.dispose();
  });
  test('已发写的迟到成功与dispose均不能污染新来源', () async {
    final at = DateTime.now().toUtc();
    final gate = Completer<http.Response>();
    final f = IdentityFixture(
      (r) async => r.method == 'POST'
          ? gate.future
          : identityReply(identityWire(now: at)),
      at: at,
    );
    await f.c.load();
    f.c.prepareReview();
    final write = f.c.establish(f.c.review!);
    expect(f.client.calls.last.method, 'POST');
    f.workspace = 'org-unit';
    f.changes.notifyListeners();
    f.workspace = null;
    f.changes.notifyListeners();
    gate.complete(identityReply(identityWire(exists: true, now: at)));
    await write;
    expect(f.c.value, isNull);
    expect(f.c.message, isNull);
    expect(f.c.permitted, false);
    expect(f.client.calls.where((r) => r.method == 'POST').length, 1);
    f.dispose();
  });
}
