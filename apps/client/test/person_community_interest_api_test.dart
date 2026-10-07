import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/person_community_interest_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const interestOwner = '11111111-1111-4111-8111-111111111111';
const interestAgent = '22222222-2222-4222-8222-222222222222';
const interestCommunity = '33333333-3333-4333-8333-333333333333';
const interestContext = '44444444-4444-4444-8444-444444444444';
Map<String, dynamic> interestRecord([
  String state = 'PRIVATE',
  bool available = true,
]) => {
  'communityId': interestCommunity,
  'name': available ? '合成公开社群' : '该社群当前不可公开展示',
  'sourceAvailable': available,
  'relation': 'interest',
  'state': state,
  if (state != 'ABSENT') 'contextId': interestContext,
};
Map<String, dynamic> interestView({
  bool options = false,
  String? state,
  bool available = true,
}) => {
  'schemaVersion': interestSchema,
  'ownerId': interestOwner,
  'agentId': interestAgent,
  'observedAt': DateTime.now().toUtc().toIso8601String(),
  'records': state == null ? [] : [interestRecord(state, available)],
  'options': options
      ? [
          {'communityId': interestCommunity, 'name': '合成公开社群'},
        ]
      : [],
  'limit': 100,
  'truncated': false,
  'modelAccess': false,
  'sendAllowed': false,
  'membershipGranted': false,
};
Map<String, dynamic> interestPreview([String op = 'PRIVATE', DateTime? now]) {
  final v = now ?? DateTime.now().toUtc();
  return {
    ...interestRecord(op == 'DELETE' ? 'PUBLIC' : 'ABSENT'),
    'schemaVersion': interestSchema,
    'ownerId': interestOwner,
    'agentId': interestAgent,
    'operation': op,
    'targetState': op == 'DELETE' ? 'ABSENT' : op,
    'preview': 'opaqueServerVersion0123456789',
    'observedAt': v.toIso8601String(),
    'expiresAt': v.add(const Duration(seconds: 90)).toIso8601String(),
    'consequence': '只有明确批准此版本后才更改你的兴趣声明。公开兴趣不代表成员资格。',
    'modelAccess': false,
    'sendAllowed': false,
    'membershipGranted': false,
  };
}

http.Response interestResponse(dynamic data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json'},
);
void main() {
  test(
    'actual fresh077 registered native HTTP wire accepted without replacing fields',
    () async {
      final file = File(
        '../../docs/testing/evidence/community-interest-declaration-2026-10-04/native-wire.json',
      );
      final receipt =
          jsonDecode(await file.readAsString()) as Map<String, dynamic>;
      expect(
        receipt['origin'],
        'ACTUAL_REGISTERED_HTTP_FRESH077_SYNTHETIC_OWNED_DATABASE',
      );
      expect(receipt['containsBearer'], false);
      expect(receipt['notProduction'], true);
      var previews = 0, views = 0, approvals = 0;
      CommunityInterestPreview? latest;
      for (final entry in receipt['entries'] as List) {
        final data = Map<String, dynamic>.from(
          entry['response']['data'] as Map,
        );
        final owner = data['ownerId'] as String;
        if (entry['path'].endsWith('/preview')) {
          latest = CommunityInterestPreview(
            data,
            owner,
            data['communityId'] as String,
            data['operation'] as String,
          );
          previews++;
        } else {
          final view = CommunityInterestView(data, owner);
          views++;
          expect(view.records.every((r) => r.name.isNotEmpty), true);
          if (entry['path'].endsWith('/approve')) {
            expect(latest, isNotNull);
            final api = PersonCommunityInterestAPI(
              client: MockClient((_) async => interestResponse(data)),
              apiBaseUrl: 'http://fixture',
            );
            await api.approve('synthetic parser replay only', latest!);
            api.dispose();
            approvals++;
          }
        }
      }
      expect(previews, greaterThanOrEqualTo(4));
      expect(views, greaterThanOrEqualTo(4));
      expect(approvals, 2);
    },
  );
  test(
    'approval receipt native observed window exact context and single-result scope',
    () async {
      final raw = interestPreview('DELETE', DateTime.utc(2026, 10, 4));
      final p = CommunityInterestPreview(
        raw,
        interestOwner,
        interestCommunity,
        'DELETE',
      );
      final good = interestView(state: 'ABSENT')
        ..['observedAt'] = '2026-10-04T00:00:20Z';
      (good['records'] as List).single['contextId'] = interestContext;
      for (final bad in [
        {...good, 'observedAt': '2026-10-03T23:59:59Z'},
        {...good, 'observedAt': '2026-10-04T00:01:30Z'},
        {
          ...good,
          'records': [
            {...interestRecord('ABSENT'), 'contextId': interestCommunity},
          ],
        },
        {...good, 'truncated': true},
        {
          ...good,
          'options': [
            {'communityId': interestCommunity, 'name': '不属于单次结果'},
          ],
        },
      ]) {
        final api = PersonCommunityInterestAPI(
          client: MockClient((_) async => interestResponse(bad)),
          apiBaseUrl: 'http://fixture',
        );
        await expectLater(api.approve('synthetic', p), throwsFormatException);
        api.dispose();
      }
      final api = PersonCommunityInterestAPI(
        client: MockClient((_) async => interestResponse(good)),
        apiBaseUrl: 'http://fixture',
      );
      expect(
        (await api.approve('synthetic', p)).records.single.contextID,
        interestContext,
      );
      api.dispose();
    },
  );
  test(
    'strict UTF8 actual selectors and approval uses only opaque preview',
    () async {
      final requests = <http.Request>[];
      final api = PersonCommunityInterestAPI(
        client: MockClient((r) async {
          requests.add(r);
          if (r.url.path.endsWith('/preview')) {
            return interestResponse(interestPreview());
          }
          if (r.url.path.endsWith('/approve')) {
            return interestResponse(interestView(state: 'PRIVATE'));
          }
          return interestResponse(
            interestView(options: r.url.path.endsWith('/options')),
          );
        }),
        apiBaseUrl: 'http://fixture',
      );
      final options = await api.read(
        'Bearer synthetic',
        interestOwner,
        options: true,
      );
      expect(options.options.single.name, '合成公开社群');
      final p = await api.preview(
        'Bearer synthetic',
        interestOwner,
        interestCommunity,
        'PRIVATE',
      );
      expect(
        (await api.approve('Bearer synthetic', p)).records.single.state,
        'PRIVATE',
      );
      expect(jsonDecode(requests[1].body), {
        'communityId': interestCommunity,
        'operation': 'PRIVATE',
      });
      expect(jsonDecode(requests[2].body), {'preview': p.token});
      expect(
        requests.every((r) => r.headers['Authorization'] == 'Bearer synthetic'),
        isTrue,
      );
      api.dispose();
    },
  );
  test(
    'RFC3339 Z and positive negative offsets accepted illegal dates rejected',
    () {
      for (final stamp in [
        '2026-10-04T01:02:03Z',
        '2026-10-04T09:02:03+08:00',
        '2026-10-03T22:02:03-03:00',
      ]) {
        final v = interestView()..['observedAt'] = stamp;
        expect(
          CommunityInterestView(v, interestOwner).observedAt.toUtc(),
          DateTime.utc(2026, 10, 4, 1, 2, 3),
        );
      }
      for (final stamp in [
        '2026-02-30T00:00:00Z',
        '2026-10-04T01:02:03+24:00',
        '2026-10-04 01:02:03',
      ]) {
        expect(
          () => CommunityInterestView(
            interestView()..['observedAt'] = stamp,
            interestOwner,
          ),
          throwsFormatException,
        );
      }
    },
  );
  test(
    'closed DTO owner agent states limits duplicate IDs and effects fail closed',
    () {
      for (final v in [
        interestView()..['ownerId'] = interestAgent,
        interestView()..['agentId'] = '',
        interestView()..['modelAccess'] = true,
        interestView()..['membershipGranted'] = true,
        interestView()..['sendAllowed'] = true,
        interestView()..['limit'] = 101,
        interestView()..['truncated'] = 1,
        interestView()..['privateRights'] = 'canary',
        interestView(state: 'PRIVATE')
          ..['records'] = [interestRecord(), interestRecord()],
        interestView()..['records'] = [interestRecord('VERIFIED')],
      ]) {
        expect(
          () => CommunityInterestView(v, interestOwner),
          throwsFormatException,
        );
      }
    },
  );
  test(
    'preview exact operation ID version time scope and result shape fail closed',
    () async {
      for (final p in [
        interestPreview()..['operation'] = 'PUBLIC',
        interestPreview()..['communityId'] = interestAgent,
        interestPreview()..['preview'] = 'confirmed',
        interestPreview()..['targetState'] = 'PUBLIC',
        interestPreview()..['membershipGranted'] = true,
        interestPreview()
          ..['expiresAt'] = DateTime.now()
              .add(const Duration(minutes: 5))
              .toUtc()
              .toIso8601String(),
      ]) {
        expect(
          () => CommunityInterestPreview(
            p,
            interestOwner,
            interestCommunity,
            'PRIVATE',
          ),
          throwsFormatException,
        );
      }
      final p = CommunityInterestPreview(
        interestPreview(),
        interestOwner,
        interestCommunity,
        'PRIVATE',
      );
      for (final v in [
        interestView(state: 'PUBLIC'),
        interestView(state: 'PRIVATE')..['agentId'] = interestOwner,
        interestView(),
      ]) {
        final api = PersonCommunityInterestAPI(
          client: MockClient((_) async => interestResponse(v)),
          apiBaseUrl: 'http://fixture',
        );
        await expectLater(api.approve('synthetic', p), throwsFormatException);
        api.dispose();
      }
    },
  );
  test('HTTP errors never become successful declaration', () async {
    for (final status in [401, 403, 409, 503]) {
      final api = PersonCommunityInterestAPI(
        client: MockClient((_) async => http.Response('{}', status)),
        apiBaseUrl: 'http://fixture',
      );
      await expectLater(
        api.read('synthetic', interestOwner),
        throwsA(isA<CommunityInterestHTTPError>()),
      );
      api.dispose();
    }
  });
}
