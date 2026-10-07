import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/notification_schedule_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

final scheduleNow = DateTime.utc(2026, 10, 6, 12);
DateTime scheduleClock() => scheduleNow;
const scheduleAgent = '82000000-0000-4000-8000-000000000001';
Map<String, dynamic> scheduleData({
  int version = 0,
  String agent = scheduleAgent,
  String? status,
  bool enabled = true,
}) => {
  'schemaVersion': notificationScheduleSchema,
  'version': version,
  'agentId': agent,
  'configured': version != 0,
  'status':
      status ??
      (version == 0
          ? 'UNCONFIGURED'
          : enabled
          ? 'ACTIVE'
          : 'DISABLED'),
  'budgetWindowHours': 24,
  'enabled': version == 0 ? false : enabled,
  'timeZone': version == 0 ? '' : 'Europe/London',
  'localMinute': version == 0 ? 0 : 1080,
  'gapPolicy': version == 0 ? '' : 'SKIP',
  'foldPolicy': version == 0 ? '' : 'EARLIER_ONCE',
  'quiet': version == 0 ? null : {'startMinute': 1320, 'endMinute': 420},
  'maxContactsPerDay': version == 0 ? 0 : 3,
  'categories': version == 0 ? <String>[] : ['ACTIVITY', 'SOCIAL'],
  if (version != 0) ...{
    'validFrom': scheduleNow.toIso8601String(),
    'updatedAt': scheduleNow.toIso8601String(),
    'expiresAt': scheduleNow.add(const Duration(days: 7)).toIso8601String(),
  },
};
http.Response scheduleResponse(Object data, [int status = 200]) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
NotificationScheduleDraft scheduleDraft() => NotificationScheduleDraft(
  enabled: true,
  timeZone: 'Europe/London',
  localMinute: 1080,
  quiet: const NotificationQuietWindow(1320, 420),
  maxContactsPerDay: 3,
  categories: ['ACTIVITY', 'SOCIAL'],
  expiresAt: scheduleNow.add(const Duration(days: 7)),
);
NotificationScheduleController _scheduleController(
  http.Client client, {
  _ScheduleIdentity? identity,
  DateTime Function()? now,
}) => NotificationScheduleController(
  authorizationHeader: () => identity == null ? 'Bearer owner' : identity.token,
  accountID: () => identity == null ? 'owner' : identity.owner,
  organizationWorkspaceID: () => identity?.workspace,
  identityChanges: identity,
  client: client,
  apiBaseUrl: 'http://schedule-fixture.test',
  now: now ?? scheduleClock,
);

class _ScheduleIdentity extends ChangeNotifier {
  String? token = 'Bearer owner', owner = 'owner', workspace;
  void change(String? t, String? o, {String? w}) {
    token = t;
    owner = o;
    workspace = w;
    notifyListeners();
  }

  void label() {
    notifyListeners();
  }
}

void main() {
  // The original HTTP handler can reject the response after Store.Put commits.
  // These are transport units, not native commit/revocation concurrency tests.
  for (final status in [401, 403, 400]) {
    test('AIR019晚鉴权$status仅重读当前计划不重复提交', () async {
      final requests = <http.Request>[];
      var attempted = false;
      final c = _scheduleController(MockClient((r) async {
        requests.add(r);
        if (r.method == 'PUT') {
          attempted = true;
          return scheduleResponse(<String, dynamic>{}, status);
        }
        return scheduleResponse(scheduleData(version: attempted ? 1 : 0));
      }));
      await c.load();
      c.startDraft();
      c.edit(scheduleDraft());
      final approval = c.preview()!;
      await c.save(approval);
      if (status == 400) {
        expect(c.uncertain, isFalse);
        expect(c.error, contains('请检查'));
      } else {
        expect(c.uncertain, isTrue);
        expect(c.editable, isFalse);
        expect(c.policy, isNull);
        expect(c.draft, isNull);
        expect(c.error, contains('结果尚未确认'));
        await c.save(approval);
        expect(requests.where((r) => r.method == 'PUT').length, 1);
        await c.load();
        expect(c.policy!.version, 1);
        expect(c.uncertain, isFalse);
        expect(c.message, contains('当前计划与确认内容一致'));
        expect(c.message, isNot(contains('已保存')));
        expect(requests.where((r) => r.method == 'PUT').length, 1);
        expect(requests.map((r) => r.method), ['GET', 'PUT', 'GET']);
      }
      c.dispose();
    });
  }
  test('AIR019缺API配置明确不可读且不造未配置计划', () async {
    final requests = <http.Request>[];
    final c = NotificationScheduleController(
      authorizationHeader: () => 'Bearer owner',
      accountID: () => 'owner',
      client: MockClient((r) async {
        requests.add(r);
        return scheduleResponse(scheduleData());
      }),
      apiBaseUrl: '',
      now: scheduleClock,
    );
    await c.load();
    expect(c.error, '当前运行环境未配置API，无法读取或保存定时汇总计划。');
    expect(c.policy, isNull);
    expect(c.editable, isFalse);
    expect(requests, isEmpty);
    c.dispose();
  });
  test('AIR019 GET未配置保留真实0状态，不猜草稿或自动PUT', () async {
    final requests = <http.Request>[];
    final c = _scheduleController(
      MockClient((r) async {
        requests.add(r);
        return scheduleResponse(scheduleData());
      }),
    );
    await c.load();
    expect(c.policy!.configured, isFalse);
    expect(c.draft, isNull);
    expect(c.preview(), isNull);
    expect(requests.single.url.path, '/v1/me/notification-schedule');
    expect(requests.single.url.query, '');
    expect(requests.single.method, 'GET');
    c.startDraft();
    expect(c.draft!.timeZone, isNull);
    expect(c.draft!.localMinute, isNull);
    expect(c.draft!.maxContactsPerDay, isNull);
    expect(c.draft!.expiresAt, isNull);
    expect(c.draft!.categories, isEmpty);
    expect(c.preview(), isNull);
    expect(requests.length, 1);
    c.dispose();
  });
  for (final status in ['ACTIVE', 'DISABLED', 'EXPIRED', 'PAUSED_SESSION']) {
    test('AIR019 真实$status完整读取，不推断已投递', () async {
      final c = _scheduleController(
        MockClient(
          (r) async => scheduleResponse(
            scheduleData(
              version: 1,
              status: status,
              enabled: status != 'DISABLED',
            ),
          ),
        ),
      );
      await c.load();
      expect(c.policy!.status, status);
      expect(c.draft!.timeZone, 'Europe/London');
      expect(c.draft!.quiet!.endMinute, 420);
      expect(c.message, isNull);
      c.dispose();
    });
  }
  final invalid = <String, void Function(Map<String, dynamic>)>{
    'schema': (p) => p['schemaVersion'] = 'model-approved',
    'version': (p) => p['version'] = 9007199254740992,
    'agent': (p) => p['agentId'] = '00000000-0000-0000-0000-000000000000',
    'fractionalminute': (p) => p['localMinute'] = 1.5,
    'minute': (p) => p['localMinute'] = 1440,
    'zone': (p) => p['timeZone'] = 'Local',
    'gap': (p) => p['gapPolicy'] = 'NORMALIZE',
    'fold': (p) => p['foldPolicy'] = 'TWICE',
    'quiet': (p) => p['quiet'] = {'startMinute': 1, 'endMinute': 1},
    'quietmissing': (p) => p.remove('quiet'),
    'budget': (p) => p['maxContactsPerDay'] = 21,
    'window': (p) => p['budgetWindowHours'] = 0,
    'categories': (p) => p['categories'] = ['SOCIAL', 'SOCIAL'],
    'emptycategories': (p) => p['categories'] = [],
    'timestamp': (p) => p['expiresAt'] = '2026-02-31T12:00:00Z',
    'clock': (p) => p['updatedAt'] = '2026-10-06T12:00:00.000001Z',
    'duration': (p) => p['expiresAt'] = '2027-01-01T00:00:00Z',
    'unconfigured': (p) {
      p.addAll(scheduleData());
      p['timeZone'] = 'Asia/Shanghai';
    },
  };
  for (final e in invalid.entries) {
    test('AIR019拒绝坏${e.key}响应而非未配置默认', () async {
      final p = scheduleData(version: 1);
      e.value(p);
      final c = _scheduleController(
        MockClient((r) async => scheduleResponse(p)),
      );
      await c.load();
      expect(c.policy, isNull);
      expect(c.draft, isNull);
      expect(c.error, isNotNull);
      expect(c.preview(), isNull);
      c.dispose();
    });
  }
  test('AIR019 explicit CAS分钟DST跨午夜类别额度期限只有真实PUT', () async {
    final requests = <http.Request>[];
    final c = _scheduleController(
      MockClient((r) async {
        requests.add(r);
        if (r.method == 'GET') return scheduleResponse(scheduleData());
        return scheduleResponse(scheduleData(version: 1));
      }),
    );
    await c.load();
    c.startDraft();
    c.edit(scheduleDraft());
    final a = c.preview()!;
    await c.save(a);
    expect(c.uncertain, isFalse);
    expect(c.policy!.version, 1);
    expect(requests.length, 2);
    final body = jsonDecode(requests.last.body) as Map<String, dynamic>;
    expect(body.keys.toSet(), {
      'expectedVersion',
      'enabled',
      'timeZone',
      'localMinute',
      'gapPolicy',
      'foldPolicy',
      'quiet',
      'maxContactsPerDay',
      'categories',
      'expiresAt',
    });
    expect(body['expectedVersion'], 0);
    expect(body['localMinute'], 1080);
    expect(body['gapPolicy'], 'SKIP');
    expect(body['foldPolicy'], 'EARLIER_ONCE');
    expect(body['quiet'], {'startMinute': 1320, 'endMinute': 420});
    expect(body['categories'], ['ACTIVITY', 'SOCIAL']);
    expect(c.message, '计划设置已保存，不代表消息已投递；请在收件箱查看实际汇总。');
    c.dispose();
  });
  test('AIR019关停与0额度保留null静默，不默认为普通提醒', () async {
    Map<String, dynamic>? sent;
    final c = _scheduleController(
      MockClient((r) async {
        if (r.method == 'GET') {
          return scheduleResponse(scheduleData(version: 1));
        }
        sent = jsonDecode(r.body);
        final p = scheduleData(version: 2, enabled: false)
          ..['quiet'] = null
          ..['maxContactsPerDay'] = 0;
        return scheduleResponse(p);
      }),
    );
    await c.load();
    c.edit(
      c.draft!.copyWith(enabled: false, clearQuiet: true, maxContactsPerDay: 0),
    );
    await c.save(c.preview()!);
    expect(sent!['enabled'], isFalse);
    expect(sent!['quiet'], isNull);
    expect(sent!['maxContactsPerDay'], 0);
    expect(c.policy!.status, 'DISABLED');
    c.dispose();
  });
  test('AIR019同版连点与过期批准不重复PUT', () async {
    var puts = 0;
    var now = scheduleNow;
    final gate = Completer<http.Response>();
    final started = Completer<void>();
    final c = _scheduleController(
      MockClient((r) async {
        if (r.method == 'GET') return scheduleResponse(scheduleData());
        puts++;
        started.complete();
        return gate.future;
      }),
      now: () => now,
    );
    await c.load();
    c.startDraft();
    c.edit(scheduleDraft());
    final a = c.preview()!;
    final first = c.save(a);
    await started.future;
    await c.save(a);
    expect(puts, 1);
    gate.complete(scheduleResponse(scheduleData(version: 1)));
    await first;
    final second = c.preview()!;
    now = scheduleNow.add(const Duration(days: 8));
    await c.save(second);
    expect(puts, 1);
    c.dispose();
  });
  test('AIR019修改草稿退休旧批准', () async {
    var puts = 0;
    final c = _scheduleController(
      MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return scheduleResponse(scheduleData(version: 1));
      }),
    );
    await c.load();
    final a = c.preview()!;
    c.edit(c.draft!.copyWith(localMinute: 600));
    await c.save(a);
    expect(puts, 0);
    c.dispose();
  });
  for (final readable in [true, false]) {
    test('AIR019 409仅GET重审 readable$readable 不虚构UNKNOWN重发', () async {
      var puts = 0, gets = 0;
      final c = _scheduleController(
        MockClient((r) async {
          if (r.method == 'PUT') {
            puts++;
            return http.Response('{}', 409);
          }
          gets++;
          if (gets > 1 && !readable) return http.Response('{}', 503);
          return scheduleResponse(scheduleData(version: gets == 1 ? 1 : 2));
        }),
      );
      await c.load();
      await c.save(c.preview()!);
      expect(puts, 1);
      expect(gets, 2);
      expect(c.uncertain, isFalse);
      if (readable) {
        expect(c.policy!.version, 2);
        expect(c.error, contains('重新确认'));
      } else {
        expect(c.policy, isNull);
        expect(c.preview(), isNull);
      }
      c.dispose();
    });
  }
  for (final outcome in ['network', '503', 'malformed']) {
    test('AIR019未知$outcome只GET核实完整原批准，不重PUT', () async {
      var puts = 0, gets = 0;
      final c = _scheduleController(
        MockClient((r) async {
          if (r.method == 'GET') {
            gets++;
            return scheduleResponse(scheduleData(version: gets == 1 ? 0 : 1));
          }
          puts++;
          if (outcome == 'network') throw http.ClientException('unknown');
          return outcome == '503'
              ? http.Response('{}', 503)
              : scheduleResponse({'approved': true});
        }),
      );
      await c.load();
      c.startDraft();
      c.edit(scheduleDraft());
      await c.save(c.preview()!);
      expect(puts, 1);
      expect(gets, 2);
      expect(c.uncertain, isFalse);
      expect(c.message, contains('当前计划与确认内容一致'));
      expect(c.message, contains('不代表已投递'));
      c.dispose();
    });
  }
  test('AIR019未知不一致锁旧PUT，人工采用读回版本才重新编辑', () async {
    var puts = 0, gets = 0;
    final c = _scheduleController(
      MockClient((r) async {
        if (r.method == 'GET') {
          gets++;
          return scheduleResponse(scheduleData(version: 1));
        }
        puts++;
        return http.Response('{}', 503);
      }),
    );
    await c.load();
    final a = c.preview()!;
    await c.save(a);
    expect(c.uncertain, isTrue);
    expect(c.preview(), isNull);
    await c.save(a);
    expect(puts, 1);
    await c.load();
    expect(gets, 3);
    expect(c.uncertain, isTrue);
    c.adoptReadback();
    expect(c.uncertain, isFalse);
    expect(c.preview(), isNotNull);
    expect(puts, 1);
    c.dispose();
  });
  for (final phase in ['GET', 'PUT']) {
    for (final fail in [false, true]) {
      test('AIR019 late $phase failure$fail 及实际同帧身份ABA退休', () async {
        final identity = _ScheduleIdentity();
        final gate = Completer<http.Response>();
        var block = false, puts = 0;
        final c = _scheduleController(
          MockClient((r) async {
            if (r.method == 'PUT') puts++;
            if (block && r.method == phase) {
              block = false;
              return gate.future;
            }
            return scheduleResponse(scheduleData(version: 1));
          }),
          identity: identity,
        );
        await c.load();
        block = true;
        final old = phase == 'GET' ? c.load() : c.save(c.preview()!);
        await Future<void>.delayed(Duration.zero);
        identity.change('Bearer peer', 'peer');
        identity.change('Bearer owner', 'owner');
        await Future<void>.delayed(Duration.zero);
        final current = c.policy;
        expect(current, isNotNull);
        if (fail) {
          gate.completeError(http.ClientException('old private late'));
        } else {
          gate.complete(scheduleResponse(scheduleData(version: 2)));
        }
        await old;
        expect(c.policy, same(current));
        expect(c.policy!.version, 1);
        expect(c.error, isNull);
        expect(c.uncertain, isFalse);
        expect(puts, phase == 'PUT' ? 1 : 0);
        c.dispose();
        identity.dispose();
      });
    }
  }
  test('AIR019标签不读、同token新owner/组织切换立即清旧批准', () async {
    final id = _ScheduleIdentity();
    var gets = 0;
    final c = _scheduleController(
      MockClient((r) async {
        gets++;
        return scheduleResponse(scheduleData(version: 1));
      }),
      identity: id,
    );
    await c.load();
    final a = c.preview()!;
    id.label();
    expect(gets, 1);
    id.change('Bearer owner', 'peer');
    await Future<void>.delayed(Duration.zero);
    await c.save(a);
    expect(gets, 2);
    id.change('Bearer owner', 'peer', w: 'org');
    expect(c.personal, isFalse);
    expect(c.policy, isNull);
    expect(c.preview(), isNull);
    expect(gets, 2);
    c.dispose();
    id.dispose();
  });
  test('AIR019 disposed pending GET不notify且借用client不关闭', () async {
    final gate = Completer<http.Response>();
    final client = _BorrowedScheduleClient((r) => gate.future);
    final c = _scheduleController(client);
    final pending = c.load();
    c.dispose();
    gate.complete(scheduleResponse(scheduleData(version: 1)));
    await pending;
    expect(client.closes, 0);
    client.close();
    expect(client.closes, 1);
  });
}

class _BorrowedScheduleClient extends MockClient {
  _BorrowedScheduleClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}
