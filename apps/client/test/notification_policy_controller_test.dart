import 'dart:async';
import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/notification_policy_controller.dart';

const agent = '11111111-1111-4111-8111-111111111111';
Map<String, dynamic> currentPolicy({
  int version = 0,
  bool enabled = false,
  String route = 'NORMAL',
  Map<String, String> rules = const {},
  DateTime? expires,
}) => {
  'schemaVersion': 'agent-notification-policy-v1',
  'version': version,
  'agentId': agent,
  'enabled': enabled,
  'defaultRoute': route,
  'rules': [
    for (final c in notificationCategories)
      if (rules.containsKey(c)) {'category': c, 'route': rules[c]},
  ],
  if (version > 0)
    'expiresAt':
        (expires ?? DateTime.now().toUtc().add(const Duration(days: 7)))
            .toIso8601String(),
  if (version > 0)
    'updatedAt': DateTime.now()
        .toUtc()
        .subtract(const Duration(seconds: 1))
        .toIso8601String(),
};
http.Response wire(Map<String, dynamic> p) =>
    http.Response(jsonEncode({'data': p}), 200);
void main() {
  test('first read is no-write and unknown wire fails closed', () async {
    var puts = 0;
    var bad = false;
    final c = NotificationPolicyController(
      authorizationHeader: () => 'Bearer test',
      accountID: () => 'person',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          puts++;
        }
        return wire({...currentPolicy(), if (bad) 'defaultRoute': 'VERIFIED'});
      }),
      apiBaseUrl: 'http://fixture',
    );
    await c.load();
    expect(c.policy!.version, 0);
    expect(puts, 0);
    bad = true;
    await c.load();
    expect(c.policy, isNull);
    expect(c.preview(), isNull);
    c.dispose();
  });
  test('exact version, double click and authority result', () async {
    var puts = 0;
    final pending = Completer<http.Response>();
    final started = Completer<void>();
    Map<String, dynamic>? sent;
    final c = NotificationPolicyController(
      authorizationHeader: () => 'Bearer test',
      accountID: () => 'person',
      client: MockClient((r) async {
        if (r.method == 'GET') {
          return wire(currentPolicy());
        }
        puts++;
        sent = jsonDecode(r.body) as Map<String, dynamic>;
        started.complete();
        return pending.future;
      }),
      apiBaseUrl: 'http://fixture',
    );
    await c.load();
    final approval = c.preview()!;
    final first = c.save(approval);
    await started.future;
    await c.save(approval);
    expect(puts, 1);
    expect(sent!['expectedVersion'], 0);
    pending.complete(
      wire(currentPolicy(version: 1, expires: approval.draft.expiresAt)),
    );
    await first;
    expect(c.policy!.version, 1);
    expect(c.message, '通知设置已保存。');
    c.dispose();
  });
  test('conflict reads current version without automatic retry', () async {
    var reads = 0, puts = 0;
    final c = NotificationPolicyController(
      authorizationHeader: () => 'Bearer test',
      accountID: () => 'person',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          puts++;
          return http.Response('{}', 409);
        }
        return wire(
          currentPolicy(
            version: reads++ == 0 ? 0 : 2,
            enabled: reads > 1,
            route: reads > 1 ? 'SILENT' : 'NORMAL',
          ),
        );
      }),
      apiBaseUrl: 'http://fixture',
    );
    await c.load();
    await c.save(c.preview()!);
    expect(puts, 1);
    expect(c.policy!.version, 2);
    expect(c.error, contains('旧草稿未覆盖'));
    c.dispose();
  });
  test('unknown PUT reconciles exact approved current state', () async {
    var reads = 0;
    NotificationPolicyApproval? approval;
    final c = NotificationPolicyController(
      authorizationHeader: () => 'Bearer test',
      accountID: () => 'person',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          return http.Response('{}', 503);
        }
        return wire(
          reads++ == 0
              ? currentPolicy()
              : currentPolicy(version: 1, expires: approval!.draft.expiresAt),
        );
      }),
      apiBaseUrl: 'http://fixture',
    );
    await c.load();
    approval = c.preview()!;
    await c.save(approval);
    expect(c.uncertain, false);
    expect(c.message, contains('已核实当前设置'));
    c.dispose();
  });
  test('unknown unchanged GET remains uncertain and cannot resend', () async {
    var puts = 0;
    final c = NotificationPolicyController(
      authorizationHeader: () => 'Bearer test',
      accountID: () => 'person',
      client: MockClient((r) async {
        if (r.method == 'PUT') {
          puts++;
          return http.Response('{}', 503);
        }
        return wire(currentPolicy());
      }),
      apiBaseUrl: 'http://fixture',
    );
    await c.load();
    final a = c.preview()!;
    await c.save(a);
    await c.save(a);
    expect(c.uncertain, true);
    expect(c.preview(), isNull);
    expect(puts, 1);
    c.dispose();
  });
  test('identity ABA, workspace and late read retire all approval', () async {
    var token = 'Bearer A';
    String? workspace;
    final pending = Completer<http.Response>();
    var calls = 0;
    final c = NotificationPolicyController(
      authorizationHeader: () => token,
      accountID: () => 'person',
      organizationWorkspaceID: () => workspace,
      client: MockClient((r) async {
        if (calls++ == 0) {
          return wire(currentPolicy());
        }
        return pending.future;
      }),
      apiBaseUrl: 'http://fixture',
    );
    await c.load();
    final old = c.preview()!;
    final late = c.load();
    token = 'Bearer B';
    c.synchronizeIdentity();
    token = 'Bearer A';
    c.synchronizeIdentity();
    pending.complete(wire(currentPolicy(version: 1)));
    await late;
    expect(c.policy, isNull);
    await c.save(old);
    expect(calls, 2);
    workspace = 'organization';
    c.synchronizeIdentity();
    expect(c.personal, false);
    expect(c.preview(), isNull);
    c.dispose();
  });
}
