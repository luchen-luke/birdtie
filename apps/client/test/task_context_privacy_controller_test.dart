import 'dart:async';
import 'package:birdtie_client/src/workspace/task_context_privacy_api.dart';
import 'package:birdtie_client/src/workspace/task_context_privacy_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'task_context_privacy_api_test.dart';

void main() {
  test('具体许可检查后明确撤回一次并刷新，原来源GET失败不阻止合法撤销', () async {
    final at = DateTime.now().toUtc();
    bool revoked = false;
    final wire = TaskPrivacyWire((r) async {
      if (r.url.path == taskContextPrivacyPath) {
        return taskResponse(
          taskInventoryWire(
            at,
            grants: [
              taskGrantWire(
                at,
                revision: revoked ? 2 : 1,
                revoked: revoked,
                expired: true,
              ),
            ],
          ),
        );
      }
      if (r.method == 'GET') return taskResponse(null, status: 403);
      revoked = true;
      return taskResponse(
        taskOriginalWire(
          taskGrantWire(at, revision: 2, revoked: true, expired: true),
        ),
      );
    });
    final c = TaskContextPrivacyController(
      api: TaskContextPrivacyAPI(
        client: wire,
        apiBaseUrl: 'https://context.test',
      ),
      authorizationHeader: () => 'Bearer unit',
      ownerID: () => taskOwner,
      now: () => at,
    );
    await c.load();
    final check = c.prepare(c.inventory!.grants.single)!;
    expect(wire.sent.where((r) => r.method == 'DELETE'), isEmpty);
    await c.withdraw(check);
    expect(wire.sent.where((r) => r.method == 'DELETE').length, 1);
    expect(
      wire.sent.where(
        (r) => r.method == 'GET' && r.url.path != taskContextPrivacyPath,
      ),
      isEmpty,
    );
    expect(c.inventory!.grants.single.revoked, true);
    await c.withdraw(check);
    expect(wire.sent.where((r) => r.method == 'DELETE').length, 1);
    c.dispose();
  });
  test('响应丢失只核实原许可，不恢复旧检查或盲重发', () async {
    final at = DateTime.now().toUtc();
    bool serverRevoked = false;
    final wire = TaskPrivacyWire((r) async {
      if (r.url.path == taskContextPrivacyPath) {
        return taskResponse(taskInventoryWire(at));
      }
      if (r.method == 'DELETE') {
        serverRevoked = true;
        throw http.ClientException('unit lost response');
      }
      return taskResponse(
        taskOriginalWire(
          taskGrantWire(
            at,
            revision: serverRevoked ? 2 : 1,
            revoked: serverRevoked,
          ),
        ),
      );
    });
    final c = TaskContextPrivacyController(
      api: TaskContextPrivacyAPI(
        client: wire,
        apiBaseUrl: 'https://context.test',
      ),
      authorizationHeader: () => 'Bearer unit',
      ownerID: () => taskOwner,
      now: () => at,
    );
    await c.load();
    final old = c.prepare(c.inventory!.grants.single)!;
    await c.withdraw(old);
    expect(c.pending, isNotNull);
    await c.withdraw(old);
    expect(wire.sent.where((r) => r.method == 'DELETE').length, 1);
    await c.verifyPending();
    expect(c.pending, isNull);
    expect(c.notice, contains('不能据此证明'));
    expect(wire.sent.where((r) => r.method == 'DELETE').length, 1);
    expect(wire.sent.last.method, 'GET');
    c.dispose();
  });
  test('409后保留未知状态，原GET拒绝不能说已撤销；清单可核当前撤回', () async {
    final at = DateTime.now().toUtc();
    bool updated = false;
    final wire = TaskPrivacyWire((r) async {
      if (r.url.path == taskContextPrivacyPath) {
        return taskResponse(
          taskInventoryWire(
            at,
            grants: [
              taskGrantWire(at, revision: updated ? 2 : 1, revoked: updated),
            ],
          ),
        );
      }
      return taskResponse(null, status: r.method == 'DELETE' ? 409 : 403);
    });
    final c = TaskContextPrivacyController(
      api: TaskContextPrivacyAPI(
        client: wire,
        apiBaseUrl: 'https://context.test',
      ),
      authorizationHeader: () => 'Bearer unit',
      ownerID: () => taskOwner,
      now: () => at,
    );
    await c.load();
    final old = c.prepare(c.inventory!.grants.single)!;
    await c.withdraw(old);
    await c.verifyPending();
    expect(c.pending, isNotNull);
    expect(c.error, contains('不可用'));
    expect(c.prepare(c.inventory!.grants.single), isNull);
    updated = true;
    await c.load();
    expect(c.pending, isNull);
    expect(c.notice, contains('不是先前请求的因果回执'));
    expect(c.review, isNull);
    expect(wire.sent.where((r) => r.method == 'DELETE').length, 1);
    c.dispose();
  });
  test('同步busy通知退休在真正send前阻止旧读取及撤回', () async {
    final at = DateTime.now().toUtc();
    bool current = true;
    final wire = TaskPrivacyWire(
      (r) async => taskResponse(taskInventoryWire(at)),
    );
    final c = TaskContextPrivacyController(
      api: TaskContextPrivacyAPI(
        client: wire,
        apiBaseUrl: 'https://context.test',
      ),
      authorizationHeader: () => 'Bearer unit',
      ownerID: () => taskOwner,
      current: () => current,
      now: () => at,
    );
    void retire() {
      if (c.busy) current = false;
    }

    c.addListener(retire);
    await c.load();
    expect(wire.sent, isEmpty);
    expect(c.retired, true);
    c.dispose();
    current = true;
    final second = TaskContextPrivacyController(
      api: TaskContextPrivacyAPI(
        client: wire,
        apiBaseUrl: 'https://context.test',
      ),
      authorizationHeader: () => 'Bearer unit',
      ownerID: () => taskOwner,
      current: () => current,
      now: () => at,
    );
    await second.load();
    final check = second.prepare(second.inventory!.grants.single)!;
    second.addListener(() {
      if (second.busy) current = false;
    });
    await second.withdraw(check);
    expect(wire.sent.where((r) => r.method == 'DELETE'), isEmpty);
    second.dispose();
  });
  test('身份ABA和迟到请求不能重新带入旧清单', () async {
    final at = DateTime.now().toUtc(),
        gate = Completer<http.StreamedResponse>();
    String auth = 'Bearer A';
    final wire = TaskPrivacyWire((r) => gate.future);
    final c = TaskContextPrivacyController(
      api: TaskContextPrivacyAPI(
        client: wire,
        apiBaseUrl: 'https://context.test',
      ),
      authorizationHeader: () => auth,
      ownerID: () => taskOwner,
      now: () => at,
    );
    final running = c.load();
    auth = 'Bearer B';
    c.synchronizeIdentity();
    auth = 'Bearer A';
    c.synchronizeIdentity();
    gate.complete(taskResponse(taskInventoryWire(at)));
    await running;
    expect(c.retired, true);
    expect(c.inventory, isNull);
    await c.load();
    expect(wire.sent.length, 1);
    c.dispose();
  });
}
