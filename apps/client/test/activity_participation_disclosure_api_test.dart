import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/activity_participation_disclosure_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const disclosureOwner = '11111111-1111-4111-8111-111111111111';
const disclosureAgent = '22222222-2222-4222-8222-222222222222';
const disclosureP = '33333333-3333-4333-8333-333333333333';
const disclosureA = '44444444-4444-4444-8444-444444444444';
String ds(DateTime t) => t.toUtc().toIso8601String();
Map<String, dynamic> disclosureRecord({
  String state = 'PRIVATE',
  bool available = true,
  DateTime? expiry,
}) {
  final now = DateTime.now().toUtc();
  return {
    'participationId': disclosureP,
    'activityId': disclosureA,
    'title': available ? '合成报名活动' : '已不可公开展示的活动报名',
    'status': 'going',
    'sourceAvailable': available,
    'visibility': state,
    'effectivePublic':
        state == 'PUBLIC' &&
        available &&
        (expiry ?? now.add(const Duration(hours: 1))).isAfter(now),
    'attendance': 'UNKNOWN',
    if (available) ...{
      'startsAt': ds(now.subtract(const Duration(hours: 1))),
      'endsAt': ds(now.add(const Duration(days: 2))),
      'sourceExpiresAt': ds(now.add(const Duration(days: 1))),
    },
    if (state == 'PUBLIC')
      'disclosureExpiresAt': ds(expiry ?? now.add(const Duration(hours: 1))),
  };
}

Map<String, dynamic> disclosureView({
  String state = 'PRIVATE',
  bool available = true,
  DateTime? expiry,
  bool empty = false,
}) => {
  'schemaVersion': 'human-activity-participation-disclosure-v1',
  'ownerId': disclosureOwner,
  'agentId': disclosureAgent,
  'observedAt': ds(DateTime.now()),
  'records': empty
      ? []
      : [disclosureRecord(state: state, available: available, expiry: expiry)],
  'limit': 100,
  'truncated': false,
  'modelAccess': false,
  'sendAllowed': false,
  'membershipGranted': false,
};
Map<String, dynamic> disclosurePreview(
  String op, {
  DateTime? expiry,
  bool available = true,
}) {
  final now = DateTime.now().toUtc();
  return {
    ...disclosureRecord(available: available),
    'schemaVersion': 'human-activity-participation-disclosure-v1',
    'ownerId': disclosureOwner,
    'agentId': disclosureAgent,
    'operation': op,
    'targetVisibility': op,
    if (expiry != null) 'targetExpiresAt': ds(expiry),
    'preview': 'opaqueServerSpecificVersion0123456789',
    'observedAt': ds(now),
    'expiresAt': ds(now.add(const Duration(seconds: 90))),
    'consequence': '只公开当前报名；不代表实际到场。',
    'modelAccess': false,
    'sendAllowed': false,
    'membershipGranted': false,
  };
}

http.Response disclosureResponse(dynamic v) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': v})),
  200,
  headers: {'content-type': 'application/json'},
);

void main() {
  test(
    'actual fresh078 registered native wire parsed without fixture substitution',
    () async {
      final wire = jsonDecode(
        await File(
          '../../docs/testing/evidence/activity-participation-disclosure-2026-10-04/native-wire.json',
        ).readAsString(),
      );
      expect(
        wire['origin'],
        'ACTUAL_REGISTERED_HTTP_FRESH078_SYNTHETIC_OWNED_DATABASE',
      );
      expect(wire['containsBearer'], false);
      var views = 0, previews = 0, approvals = 0;
      ParticipationDisclosurePreview? latest;
      for (final e in wire['entries']) {
        final m = Map<String, dynamic>.from(e['response']['data']);
        if (e['path'].endsWith('/preview')) {
          latest = ParticipationDisclosurePreview(
            m,
            m['ownerId'],
            m['participationId'],
            m['operation'],
            m['targetExpiresAt'] == null
                ? null
                : DateTime.parse(m['targetExpiresAt']).toUtc(),
          );
          previews++;
        } else {
          ParticipationDisclosureView(m, m['ownerId']);
          if (e['path'].endsWith('/approve')) {
            final api = ActivityParticipationDisclosureAPI(
              client: MockClient((r) async => disclosureResponse(m)),
            );
            await api.approve('Bearer synthetic-not-forwarded', latest!);
            approvals++;
            api.dispose();
          }
          views++;
        }
      }
      expect(previews, 2);
      expect(views, 5);
      expect(approvals, 2);
    },
  );
  test(
    'legitimate +08 time equals Z; malformed timestamps owner flags hidden fields rejected',
    () {
      final m = disclosureView();
      final t = DateTime.parse(m['observedAt']);
      m['observedAt'] =
          '${t.add(const Duration(hours: 8)).toIso8601String().replaceFirst('Z', '')}+08:00';
      expect(ParticipationDisclosureView(m, disclosureOwner).observedAt, t);
      for (final patch in [
        {'ownerId': disclosureA},
        {'modelAccess': true},
        {'observedAt': '2026-02-30T12:00:00Z'},
        {'observedAt': 'not-time'},
        {'unexpected': true},
      ]) {
        expect(
          () => ParticipationDisclosureView({
            ...disclosureView(),
            ...patch,
          }, disclosureOwner),
          throwsFormatException,
        );
      }
      final hidden = disclosureView(available: false);
      hidden['records'][0]['startsAt'] = ds(DateTime.now());
      expect(
        () => ParticipationDisclosureView(hidden, disclosureOwner),
        throwsFormatException,
      );
      final duplicate = disclosureView();
      duplicate['records'].add(duplicate['records'][0]);
      expect(
        () => ParticipationDisclosureView(duplicate, disclosureOwner),
        throwsFormatException,
      );
    },
  );
  test(
    'PUBLIC preview requires selected concrete expiry; PRIVATE cannot carry expiry',
    () async {
      var requests = 0;
      final api = ActivityParticipationDisclosureAPI(
        client: MockClient((r) async {
          requests++;
          return disclosureResponse(disclosurePreview('PRIVATE'));
        }),
      );
      await expectLater(
        api.preview('Bearer A', disclosureOwner, disclosureP, 'PUBLIC', null),
        throwsFormatException,
      );
      await expectLater(
        api.preview(
          'Bearer A',
          disclosureOwner,
          disclosureP,
          'PRIVATE',
          DateTime.now(),
        ),
        throwsFormatException,
      );
      expect(requests, 0);
    },
  );
  test(
    'approval sends only opaque preview and verifies exact P/A/owner/expiry/window',
    () async {
      final expiry = DateTime.now().toUtc().add(const Duration(hours: 1));
      final p = ParticipationDisclosurePreview(
        disclosurePreview('PUBLIC', expiry: expiry),
        disclosureOwner,
        disclosureP,
        'PUBLIC',
        expiry,
      );
      final api = ActivityParticipationDisclosureAPI(
        client: MockClient((r) async {
          expect(jsonDecode(r.body), {'preview': p.token});
          return disclosureResponse(
            disclosureView(state: 'PUBLIC', expiry: expiry),
          );
        }),
      );
      expect(
        (await api.approve('Bearer A', p)).records.single.effectivePublic,
        true,
      );
      for (final mode in [
        'wrong_activity',
        'stale',
        'late',
        'wrong_expiry',
        'wrong_agent',
        'truncated',
      ]) {
        final bad = disclosureView(state: 'PUBLIC', expiry: expiry);
        switch (mode) {
          case 'wrong_activity':
            bad['records'][0]['activityId'] = disclosureAgent;
          case 'stale':
            bad['observedAt'] = ds(
              p.observedAt.subtract(const Duration(seconds: 1)),
            );
          case 'late':
            bad['observedAt'] = ds(p.expiresAt);
          case 'wrong_expiry':
            bad['records'][0]['disclosureExpiresAt'] = ds(
              expiry.add(const Duration(seconds: 1)),
            );
          case 'wrong_agent':
            bad['agentId'] = disclosureA;
          case 'truncated':
            bad['truncated'] = true;
        }
        final a = ActivityParticipationDisclosureAPI(
          client: MockClient((r) async => disclosureResponse(bad)),
        );
        await expectLater(
          a.approve('Bearer A', p),
          throwsFormatException,
          reason: mode,
        );
      }
    },
  );
}
