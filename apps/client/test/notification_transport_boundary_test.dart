import 'dart:convert';
import 'package:birdtie_client/src/workspace/notification_policy_controller.dart';
import 'package:birdtie_client/src/workspace/notification_schedule_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'notification_policy_controller_test.dart' as policy;
import 'notification_schedule_controller_test.dart' as schedule;

// Reuse the original wire fixtures without invoking either original main().
class _Harness {
  _Harness(this.kind, {this.status = 200}) {
    client = MockClient((r) async {
      requests.add(r);
      if (r.method == 'PUT') {
        saved = true;
        if (status != 200) return http.Response('{}', status);
      }
      return response(version: saved ? 1 : 0);
    });
    c = kind == 'schedule'
        ? NotificationScheduleController(
            authorizationHeader: () => token,
            accountID: () => owner,
            organizationWorkspaceID: () => workspace,
            client: client,
            apiBaseUrl: 'http://unit.fixture',
            now: schedule.scheduleClock,
          )
        : NotificationPolicyController(
            authorizationHeader: () => token,
            accountID: () => owner,
            organizationWorkspaceID: () => workspace,
            client: client,
            apiBaseUrl: 'http://unit.fixture',
          );
  }
  final String kind;
  final int status;
  String? token = 'Bearer original', owner = 'original', workspace;
  late final dynamic c;
  late final http.Client client;
  final requests = <http.Request>[];
  bool saved = false;
  dynamic approval;
  http.Response response({int version = 0}) {
    final data = kind == 'schedule'
        ? schedule.scheduleData(version: version)
        : policy.currentPolicy(
            version: version,
            expires: approval?.draft.expiresAt,
          );
    return http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})), 200,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
  }
  Future<void> prepare() async {
    await c.load();
    if (kind == 'schedule') {
      c.startDraft();
      c.edit(schedule.scheduleDraft());
    }
    approval = c.preview();
    expect(approval, isNotNull);
  }
  void change(String what) {
    switch (what) {
      case 'owner': owner = 'replacement';
      case 'token': token = 'Bearer replacement';
      case 'organization': workspace = 'organization';
      case 'logout': token = null; owner = null;
    }
    c.synchronizeIdentity();
  }
  void dispose() { c.dispose(); client.close(); }
}

void main() {
  for (final kind in ['policy', 'schedule']) {
    for (final phase in ['load', 'save', 'uncertain']) {
      for (final change in ['owner', 'token', 'organization', 'logout']) {
        test('AIR019传输边界$kind/$phase/$change通知后旧请求不得发送', () async {
          final h = _Harness(kind, status: phase == 'uncertain' ? 503 : 200);
          addTearDown(h.dispose);
          if (phase != 'load') await h.prepare();
          var switched = false;
          h.c.addListener(() {
            final ready = phase == 'load' ? h.c.loading as bool
                : phase == 'save' ? h.c.saving as bool : h.c.uncertain as bool;
            if (!switched && ready) {
              switched = true;
              h.change(change);
            }
          });
          if (phase == 'load') {
            await h.c.load();
            expect(h.requests, isEmpty);
          } else {
            await h.c.save(h.approval);
            expect(h.requests.map((r) => r.method).toList(),
                phase == 'save' ? ['GET'] : ['GET', 'PUT']);
          }
          expect(switched, isTrue);
          expect(h.c.policy, isNull);
          expect(h.c.draft, isNull);
          expect(h.c.message, isNull);
          expect(h.requests.every((r) => r.headers['Authorization'] == 'Bearer original'), isTrue);
        });
      }
    }
    test('AIR019传输边界$kind普通状态通知不阻止原批准保存', () async {
      final h = _Harness(kind);
      addTearDown(h.dispose);
      var notices = 0;
      h.c.addListener(() { notices++; h.c.synchronizeIdentity(); });
      await h.prepare();
      await h.c.save(h.approval);
      expect(h.requests.map((r) => r.method).toList(), ['GET', 'PUT']);
      expect(h.requests.every((r) => r.headers['Authorization'] == 'Bearer original'), isTrue);
      expect(h.c.policy.version, 1);
      expect(h.c.message, contains('已保存'));
      expect(notices, greaterThan(0));
    });
  }
  for (final status in [401, 403, 400]) {
    test('AIR019传输边界policy迟到$status只读取不复用批准', () async {
      final h = _Harness('policy', status: status);
      addTearDown(h.dispose);
      await h.prepare();
      await h.c.save(h.approval);
      if (status == 400) {
        expect(h.c.uncertain, isFalse);
      } else {
        expect(h.c.uncertain, isTrue);
        expect(h.c.policy, isNull);
        expect(h.c.draft, isNull);
        expect(h.c.error, contains('结果尚未确认'));
        await h.c.save(h.approval);
        expect(h.requests.where((r) => r.method == 'PUT').length, 1);
        await h.c.load();
        expect(h.c.policy.version, 1);
        expect(h.c.message, isNot(contains('已保存')));
        expect(h.requests.map((r) => r.method).toList(), ['GET', 'PUT', 'GET']);
      }
    });
  }
}
