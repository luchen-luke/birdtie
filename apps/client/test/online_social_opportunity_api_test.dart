import 'dart:convert';
import 'package:birdtie_client/src/workspace/online_social_opportunity_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const onlineOppOwner = '11111111-1111-4111-8111-111111111111',
    onlineOppIntent = '22222222-2222-4222-8222-222222222222',
    onlineOppSource = '33333333-3333-4333-8333-333333333333',
    onlineOppTie = '44444444-4444-4444-8444-444444444444',
    onlineOppCommunity = '55555555-5555-4555-8555-555555555555';
Map<String, dynamic> onlineOppWire({
  bool options = false,
  String relation = 'PUBLIC',
  String type = 'ACTIVITY',
  DateTime? now,
  String? title,
}) {
  final stamp = now ?? DateTime.now().toUtc();
  return {
    'schemaVersion': 'online-social-opportunities-v1',
    'ownerId': onlineOppOwner,
    'intentId': options ? '' : onlineOppIntent,
    'observedAt': stamp.toIso8601String(),
    'validUntil': stamp.add(const Duration(seconds: 80)).toIso8601String(),
    'truncated': false,
    'intents': [
      {
        'id': onlineOppIntent,
        'title': '线上羽毛球交流',
        'updatedAt': '2026-01-01T12:00:00+08:00',
        'expiresAt': stamp.add(const Duration(hours: 1)).toIso8601String(),
      },
    ],
    'items': options
        ? []
        : <Map<String, dynamic>>[
            {
              'id': '$onlineOppIntent:$type:$onlineOppSource',
              'title': title ?? '异地线上羽毛球活动',
              'sourceRef': {'type': type, 'id': onlineOppSource},
              'relation': relation,
              'sourceVersion': '2026-01-01T12:00:00+08:00',
              'expiresAt': stamp
                  .add(const Duration(hours: 1))
                  .toIso8601String(),
              'tieId': relation == 'FRIEND' ? onlineOppTie : '',
              'communityId': relation == 'COMMUNITY' ? onlineOppCommunity : '',
            },
          ],
  };
}

http.Response onlineOppResponse(Map<String, dynamic> wire) => http.Response(
  jsonEncode({'data': wire}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
void main() {
  test('原ID闭合DTO及合法Go偏移，GET不带城市坐标或私密正文', () async {
    final requests = <http.Request>[];
    final api = OnlineSocialOpportunityAPI(
      client: MockClient((r) async {
        requests.add(r);
        return onlineOppResponse(
          onlineOppWire(options: r.url.path.endsWith('/options')),
        );
      }),
      apiBaseUrl: 'https://fixture.test',
    );
    expect(
      (await api.read('Bearer A', onlineOppOwner)).intents.single.id,
      onlineOppIntent,
    );
    final v = await api.read(
      'Bearer A',
      onlineOppOwner,
      intentID: onlineOppIntent,
    );
    expect(v.items.single.sourceID, onlineOppSource);
    expect(v.items.single.id, '$onlineOppIntent:ACTIVITY:$onlineOppSource');
    expect(
      requests.every(
        (r) => r.method == 'GET' && r.body.isEmpty && r.url.query.isEmpty,
      ),
      true,
    );
    expect(requests.last.headers['Authorization'], 'Bearer A');
    api.dispose();
  });
  for (final kind in [
    'extra',
    'owner',
    'fakePin',
    'private',
    'duplicate',
    'overflow',
    'missingZone',
    'expired',
    'badRelation',
    'fakeTie',
  ]) {
    test('严格拒绝 $kind', () {
      final w = onlineOppWire();
      final item = (w['items'] as List).single as Map<String, dynamic>;
      switch (kind) {
        case 'extra':
          w['privateBody'] = 'CANARY';
        case 'owner':
          w['ownerId'] = onlineOppSource;
        case 'fakePin':
          item['sourceRef'] = {'type': 'PLACE', 'id': onlineOppSource};
        case 'private':
          item['relation'] = 'PRIVATE';
        case 'duplicate':
          (w['items'] as List).add(jsonDecode(jsonEncode(item)));
        case 'overflow':
          w['observedAt'] = '2026-13-32T25:00:00Z';
        case 'missingZone':
          w['validUntil'] = '2026-10-04T10:00:00';
        case 'expired':
          item['expiresAt'] = w['observedAt'];
        case 'badRelation':
          item['relation'] = 'COMMUNITY';
        case 'fakeTie':
          item['tieId'] = onlineOppTie;
      }
      expect(
        () => OnlineOpportunityView.decode(w, onlineOppOwner, onlineOppIntent),
        throwsA(anything),
      );
    });
  }
}
