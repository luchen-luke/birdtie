import 'dart:convert';
import 'package:birdtie_client/src/workspace/notification_policy_controller.dart';
import 'package:birdtie_client/src/workspace/notification_schedule_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'notification_policy_controller_test.dart' as policy;
import 'notification_schedule_controller_test.dart' as schedule;

// Synthetic transport responses. Existing wire fixtures are reused without
// calling their main(); these units neither simulate nor prove a real commit.
class _SaveTimeout {
  _SaveTimeout(this.kind, this.outcome) {
    client = MockClient((r) async {
      requests.add(r);
      if (r.method == 'PUT') {
        submitted = true;
        return http.Response('{}', outcome == 'known400' ? 400 : 408);
      }
      if (submitted && outcome == 'unreadable') {
        return http.Response('{}', 503);
      }
      final version = submitted && outcome == 'matching' ? 1 : 0;
      final data = kind == 'schedule'
          ? schedule.scheduleData(version: version)
          : policy.currentPolicy(version: version, expires: approved?.draft.expiresAt);
      return http.Response.bytes(utf8.encode(jsonEncode({'data': data})), 200,
          headers: {'content-type': 'application/json; charset=utf-8'});
    });
    c = kind == 'schedule'
        ? NotificationScheduleController(
            authorizationHeader: () => token,
            accountID: () => owner,
            client: client,
            apiBaseUrl: 'http://unit.fixture',
            now: schedule.scheduleClock,
          )
        : NotificationPolicyController(
            authorizationHeader: () => token,
            accountID: () => owner,
            client: client,
            apiBaseUrl: 'http://unit.fixture',
          );
  }
  final String kind, outcome;
  String? token = 'Bearer original', owner = 'original';
  late final dynamic c;
  late final http.Client client;
  dynamic approved;
  bool submitted = false;
  final requests = <http.Request>[];
  Future<void> prepare() async {
    await c.load();
    if (kind == 'schedule') {
      c.startDraft();
      c.edit(schedule.scheduleDraft());
    }
    approved = c.preview();
    expect(approved, isNotNull);
  }
  void dispose() { c.dispose(); client.close(); }
}

void main() {
  for (final kind in ['policy', 'schedule']) {
    for (final outcome in ['matching', 'unchanged', 'unreadable', 'retired', 'known400']) {
      test('AIR019保存超时$kind/$outcome只读核实不重复提交', () async {
        final h = _SaveTimeout(kind, outcome);
        addTearDown(h.dispose);
        await h.prepare();
        var retired = false;
        if (outcome == 'retired') {
          h.c.addListener(() {
            if (!retired && h.c.uncertain == true) {
              retired = true;
              h.owner = 'replacement';
              h.token = 'Bearer replacement';
              h.c.synchronizeIdentity();
            }
          });
        }
        await h.c.save(h.approved);
        expect(h.requests.map((r) => r.method).toList(),
            outcome == 'known400' || outcome == 'retired'
                ? ['GET', 'PUT'] : ['GET', 'PUT', 'GET']);
        expect(h.requests.every((r) => r.headers['Authorization'] == 'Bearer original'), isTrue);
        if (outcome == 'matching') {
          expect(h.c.uncertain, isFalse);
          expect(h.c.policy.version, 1);
          expect(h.c.message, contains('已核实当前'));
          expect(h.c.message, isNot(contains('已保存')));
        } else if (outcome == 'retired') {
          expect(retired, isTrue);
          expect(h.c.policy, isNull);
          expect(h.c.draft, isNull);
          expect(h.c.message, isNull);
        } else if (outcome == 'known400') {
          expect(h.c.uncertain, isFalse);
          expect(h.c.error, isNotNull);
        } else {
          expect(h.c.uncertain, isTrue);
          expect(h.c.error, isNotNull);
          expect(h.c.message, isNull);
        }
        final count = h.requests.length;
        await h.c.save(h.approved);
        if (outcome != 'known400') {
          expect(h.requests.length, count);
          expect(h.requests.where((r) => r.method == 'PUT').length, 1);
        }
      });
    }
  }
}
