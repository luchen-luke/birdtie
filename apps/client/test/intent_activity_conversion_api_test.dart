import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/intent_activity_conversion_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'active_social_intent_api_test.dart' show nowOwner, nowIntent, nowAgent;

const conversionActivity = 'a1111111-1111-4111-8111-111111111111';
const conversionParticipation = 'b1111111-1111-4111-8111-111111111111';
Map<String, dynamic> conversionIntent({String status = 'ACTIVE'}) => {
  'id': nowIntent,
  'creatorAccountId': nowOwner,
  'type': 'FIND_ACTIVITY',
  'title': '本人明确寻找线上活动',
  'constraints': <String, dynamic>{},
  'audience': 'PRIVATE',
  'modality': 'ONLINE',
  'status': status,
  'expiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(hours: 1))
      .toIso8601String(),
  'createdAt': '2026-01-01T00:00:00Z',
  'updatedAt': DateTime.now().toUtc().toIso8601String(),
  if (status == 'CONVERTED') ...{
    'convertedActivityId': conversionActivity,
    'convertedParticipationId': conversionParticipation,
    'convertedAt': DateTime.now().toUtc().toIso8601String(),
  },
};
Map<String, dynamic> conversionEnvelope() => {
  'schemaVersion': conversionSchema,
  'ownerId': nowOwner,
  'agentId': nowAgent,
  'observedAt': DateTime.now().toUtc().toIso8601String(),
  'modelAccess': false,
  'sendAllowed': false,
};
Map<String, dynamic> conversionChoice() => {
  'participationId': conversionParticipation,
  'activity': {
    'id': conversionActivity,
    'activityId': conversionActivity,
    'title': '原已报名线上活动',
    'cityId': 'aberdeen-gb',
    'startsAt': '2026-10-05T23:30:00+08:00',
    'endsAt': '2026-10-06T00:30:00+08:00',
    'available': true,
    'status': 'upcoming',
    'createdAt': '2026-01-01T00:00:00Z',
    'modality': 'online',
    'physicalPlaceStatus': 'not_applicable',
    'timeZone': 'Europe/London',
  },
};
Map<String, dynamic> conversionList() => {
  ...conversionEnvelope(),
  'intent': conversionIntent(),
  'version': 'a' * 64,
  'choices': [conversionChoice()],
  'limit': 100,
  'truncated': false,
  'explanation': '只关联当前原已报名活动，不重新报名。',
};
Map<String, dynamic> conversionPreview() => {
  ...conversionEnvelope(),
  'intent': conversionIntent(),
  'version': 'a' * 64,
  'choice': conversionChoice(),
  'previewId': 'opaque-current-native-selection',
  'expiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(seconds: 30))
      .toIso8601String(),
  'explanation': '结束寻找，关联原报名。不新增报名、邀请、公开或模型授权。',
};
Map<String, dynamic> conversionReceipt(Map<String, dynamic> p) {
  final now = DateTime.now().toUtc().toIso8601String();
  return {
    ...conversionEnvelope(),
    'intent': {
      ...p['intent'],
      'status': 'CONVERTED',
      'updatedAt': now,
      'convertedAt': now,
      'convertedActivityId': conversionActivity,
      'convertedParticipationId': conversionParticipation,
    },
    'activityId': conversionActivity,
    'participationId': conversionParticipation,
    'committed': true,
    'explanation': '已关联原报名，未再次报名。',
  };
}

http.Response conversionResponse(Map<String, dynamic> m) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': m})),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

class ConversionBorrowedClient extends MockClient {
  ConversionBorrowedClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

void main() {
  test(
    'actual native8 registered raw HTTP bodies parse with original IDs and times',
    () {
      // Exact httptest Body.String from conversion-native8, local synthetic data.
      // Historical opaque previews are parsed only; never sent as authority.
      final list =
          jsonDecode(
                r'''{"data":{"schemaVersion":"intent-activity-conversion-v1","ownerId":"bc8da064-36f3-4a8b-903d-d25ebc6dc5dd","agentId":"16c6f870-5519-4a5d-9039-99e927ccec66","observedAt":"2026-10-05T03:35:42.137405+08:00","modelAccess":false,"sendAllowed":false,"intent":{"id":"10817b54-9348-4a6d-8c58-995b699f9fc6","creatorAccountId":"bc8da064-36f3-4a8b-903d-d25ebc6dc5dd","type":"FIND_ACTIVITY","title":"本人线上活动寻找","constraints":{},"audience":"PRIVATE","modality":"ONLINE","status":"ACTIVE","expiresAt":"2026-10-04T20:35:41.935263Z","createdAt":"2026-10-04T19:35:41.93571Z","updatedAt":"2026-10-04T19:35:41.939065Z"},"version":"46805e50e23364aa2c921f3be2f0127165fb89c1ad3651f0ed6e6091f28abe44","choices":[{"activity":{"id":"f05db899-9474-453b-ab8d-f95610b7bc2d","activityId":"f05db899-9474-453b-ab8d-f95610b7bc2d","title":"异地线上羽毛球协调（合成）","cityId":"e2e004-city-0-74fa49bc-9d71-4364-a35b-952170428ebd","startsAt":"2026-10-05T04:35:41+08:00","endsAt":"2026-10-05T05:35:41+08:00","status":"upcoming","available":true,"createdAt":"2026-10-05T03:35:41.867382+08:00","modality":"online","physicalPlaceStatus":"not_applicable","placeId":"","placeName":"","venuePlaceId":"","timeZone":"UTC"},"participationId":"9f46f300-6f42-42b3-add9-5a8268a2a8ff"}],"limit":100,"truncated":false,"explanation":"仅选择当前已报名且仍可见的活动；不会重新报名、邀请或公开意图。"}}''',
              )
              as Map<String, dynamic>;
      final preview =
          jsonDecode(
                r'''{"data":{"schemaVersion":"intent-activity-conversion-v1","ownerId":"bc8da064-36f3-4a8b-903d-d25ebc6dc5dd","agentId":"16c6f870-5519-4a5d-9039-99e927ccec66","observedAt":"2026-10-05T03:35:42.171394+08:00","modelAccess":false,"sendAllowed":false,"previewId":"R0l2V9ngVS50I85p6HodJidECCXfSx_7mMioKNx-FzxXkVTWNi9-RG_PMcStRuCrv1NpY2z8RZFE2drglC0cfFqSnSLrtAa9iGZGz6bKfvPM0Sn2yidbXOZUx6RpaxY_qyN8igaL5hadkalkktVKIB4B6AhBSfBufS1fiybj6FySISkarcW1v8mure0WFbzKnAJnRHckUS9Y1na_5JlrfpTFgQvF08K4p6bajJiDNP6sFDP3zoBtLILi-U1FPDd-Ez56TH8EE1ejIvBaAp3o7Z1aLE6iEvjztealylcHUdbiBtLYNsEid-64evJIPa2_pXtr1gSYXrMh7uUM8CFtYW3M04AUbyb7tk0600qzv_H8lGM2q_nJTsuJrhNd19seS5QeoWbH2tFk0O2sWgRD4c2p4tpD9hI_zDO0pm4zKnpZkbKMDOHnADb5dUISOot5biMRqwEwbH0dFWxtD73mSOMaPlGGCkqr6EN2qcVAqhUNhHhurvFbsuWa3EFgZj7D-EXZKWR3IDp2Sx9YVs7onIQPeGiNs3Fh1D7PrMp78WuJjPP1S-a_DXFzUyKlAldrAZ6lR1MkuenO_TLaeTQDc9b67_HEk2BqSRC0osPWQjgarZIuLppgCzg-lMvrcxIBEJ7WjFGsQJUbyBLOdWBwKxKqshvfOJvfAIK58hsXQ2VQ4Y-CSbPchtA--zra1M7A7-XHAXfQLynzUWaGmtJEr2AZD2EK_rbQ5AlT7pw-KKP5cNNC9YOJbDrRfriaCZUz0PLgb_pDrFwIGH6NWhuU2hxxPHwBm1X5xhyUtQ42ex7BRpOqoiNBFxy9NwEDuu4rZkv9x7tvXpgjOcQtCCOmEV1KiUCxT5pbnur-HMPrIvl4P-GETDsY72QKnZG8zNKbwC2Htfz06slbdqWbxKHftUogoV9pkUCr4pSshlhqEVJxbMbFZeylNyC8R-2Ca3fJGjeglOnBdEOmBg","intent":{"id":"10817b54-9348-4a6d-8c58-995b699f9fc6","creatorAccountId":"bc8da064-36f3-4a8b-903d-d25ebc6dc5dd","type":"FIND_ACTIVITY","title":"本人线上活动寻找","constraints":{},"audience":"PRIVATE","modality":"ONLINE","status":"ACTIVE","expiresAt":"2026-10-04T20:35:41.935263Z","createdAt":"2026-10-04T19:35:41.93571Z","updatedAt":"2026-10-04T19:35:41.939065Z"},"version":"46805e50e23364aa2c921f3be2f0127165fb89c1ad3651f0ed6e6091f28abe44","choice":{"activity":{"id":"f05db899-9474-453b-ab8d-f95610b7bc2d","activityId":"f05db899-9474-453b-ab8d-f95610b7bc2d","title":"异地线上羽毛球协调（合成）","cityId":"e2e004-city-0-74fa49bc-9d71-4364-a35b-952170428ebd","startsAt":"2026-10-05T04:35:41+08:00","endsAt":"2026-10-05T05:35:41+08:00","status":"upcoming","available":true,"createdAt":"2026-10-05T03:35:41.867382+08:00","modality":"online","physicalPlaceStatus":"not_applicable","placeId":"","placeName":"","venuePlaceId":"","timeZone":"UTC"},"participationId":"9f46f300-6f42-42b3-add9-5a8268a2a8ff"},"expiresAt":"2026-10-05T03:36:12.163735+08:00","explanation":"确认将这条寻找活动意图关联到原已报名活动并结束寻找；报名ID保持，不再次报名，不邀请、不公开、不通知其他人。"}}''',
              )
              as Map<String, dynamic>;
      final receipt =
          jsonDecode(
                r'''{"data":{"schemaVersion":"intent-activity-conversion-v1","ownerId":"bc8da064-36f3-4a8b-903d-d25ebc6dc5dd","agentId":"16c6f870-5519-4a5d-9039-99e927ccec66","observedAt":"2026-10-05T03:35:42.267502+08:00","modelAccess":false,"sendAllowed":false,"intent":{"convertedActivityId":"f05db899-9474-453b-ab8d-f95610b7bc2d","convertedParticipationId":"9f46f300-6f42-42b3-add9-5a8268a2a8ff","convertedAt":"2026-10-05T03:35:42.253654+08:00","id":"10817b54-9348-4a6d-8c58-995b699f9fc6","creatorAccountId":"bc8da064-36f3-4a8b-903d-d25ebc6dc5dd","type":"FIND_ACTIVITY","title":"本人线上活动寻找","constraints":{},"audience":"PRIVATE","modality":"ONLINE","status":"CONVERTED","expiresAt":"2026-10-04T20:35:41.935263Z","createdAt":"2026-10-04T19:35:41.93571Z","updatedAt":"2026-10-04T19:35:42.253654Z"},"activityId":"f05db899-9474-453b-ab8d-f95610b7bc2d","participationId":"9f46f300-6f42-42b3-add9-5a8268a2a8ff","committed":true,"explanation":"意图已关联原报名活动；报名、提醒和受众均未新增或扩大。"}}''',
              )
              as Map<String, dynamic>;
      final owner = list['data']['ownerId'] as String,
          id = list['data']['intent']['id'] as String;
      final a = ConversionOptions(list['data'], owner, id),
          p = ConversionPreview(preview['data'], owner, id),
          r = ConversionReceipt(receipt['data'], owner, id);
      expect(
        a.choices.single.activity.activityId,
        p.choice.activity.activityId,
      );
      expect(r.participationID, p.choice.participationID);
      expect(r.intent.linked, true);
    },
  );

  test(
    'original intent constraints audience and modality combinations fail closed',
    () {
      final mutations = <void Function(Map<String, dynamic>)>[
        (m) => m['constraints'] = {'minParticipants': 3, 'maxParticipants': 2},
        (m) => m['constraints'] = {
          'startsAt': '2026-01-01T00:00:00Z',
          'endsAt': '2026-04-02T00:00:00Z',
        },
        (m) => m['audience'] = 'LOCAL',
        (m) => m['cityId'] = 'unexpected-city',
        (m) => m['audience'] = 'COMMUNITY',
        (m) => m['communityId'] = conversionActivity,
        (m) => m['audience'] = 'INVITE_ONLY',
        (m) => m['inviteeAccountIds'] = [conversionActivity],
        (m) => m['constraints'] = {'placeId': conversionActivity},
        (m) => m['modality'] = 'IN_PERSON',
        (m) {
          m['modality'] = 'IN_PERSON';
          m['constraints'] = {
            'placeId': conversionActivity,
            'onlinePlatform': 'online',
          };
        },
        (m) {
          m['modality'] = 'HYBRID';
          m['constraints'] = {'placeId': conversionActivity};
        },
      ];
      for (final change in mutations) {
        final m =
            jsonDecode(jsonEncode(conversionIntent())) as Map<String, dynamic>;
        change(m);
        expect(() => ConversionIntent(m, nowOwner), throwsFormatException);
      }
    },
  );
  test(
    'native activity createdAt is required and zero placeholders reject',
    () {
      for (final value in [
        null,
        '0001-01-01T00:00:00Z',
        '2026-02-30T00:00:00Z',
      ]) {
        final m =
            jsonDecode(jsonEncode(conversionChoice())) as Map<String, dynamic>;
        (m['activity'] as Map)['createdAt'] = value;
        expect(() => ConversionChoice(m), throwsFormatException);
      }
    },
  );

  test(
    'strict real protocol parses original IDs and offset without inventing local time',
    () {
      final v = ConversionOptions(conversionList(), nowOwner, nowIntent);
      expect(
        v.choices.single.activity.startsAt!.toUtc(),
        DateTime.utc(2026, 10, 5, 15, 30),
      );
      expect(v.choices.single.participationID, conversionParticipation);
      expect(v.intent.linked, false);
    },
  );
  test('closed owner model flags fields and actual activity dimensions', () {
    for (final change in <void Function(Map<String, dynamic>)>[
      (m) => m['ownerId'] = conversionActivity,
      (m) => m['modelAccess'] = true,
      (m) => m['permission'] = true,
      (m) => m['choices'][0]['activity']['latitude'] = 57,
      (m) => m['choices'][0]['activity']['startsAt'] = '2026-02-30T00:00:00Z',
      (m) => m['choices'][0]['activity']['startsAt'] = '2026-10-05T00:00:00',
      (m) => m['choices'][0]['activity']['placeName'] = '猜出的地址',
      (m) => m['choices'].add(m['choices'][0]),
    ]) {
      final m =
          jsonDecode(jsonEncode(conversionList())) as Map<String, dynamic>;
      change(m);
      expect(
        () => ConversionOptions(m, nowOwner, nowIntent),
        throwsFormatException,
      );
    }
  });
  test(
    'legacy converted null remains unknown and partial association rejects',
    () {
      final m = conversionList();
      m['intent'] = {...conversionIntent(), 'status': 'CONVERTED'};
      expect(ConversionOptions(m, nowOwner, nowIntent).intent.linked, false);
      m['intent']['convertedActivityId'] = conversionActivity;
      expect(
        () => ConversionOptions(m, nowOwner, nowIntent),
        throwsFormatException,
      );
    },
  );
  test(
    'exact opaque approval and bounded timeout never retry nor close borrowed client',
    () async {
      final requests = <http.Request>[];
      final p = conversionPreview();
      final c = ConversionBorrowedClient((r) async {
        requests.add(r);
        return conversionResponse(
          r.url.path.endsWith('/approve')
              ? conversionReceipt(p)
              : r.url.path.endsWith('/preview')
              ? p
              : conversionList(),
        );
      });
      final api = IntentActivityConversionAPI(
        client: c,
        apiBaseUrl: 'http://127.0.0.1:9999',
      );
      await api.options('Bearer A', nowOwner, nowIntent);
      final review = await api.preview(
        'Bearer A',
        nowOwner,
        nowIntent,
        conversionActivity,
        'a' * 64,
      );
      await api.approve('Bearer A', nowOwner, nowIntent, review);
      expect(jsonDecode(requests.last.body), {'previewId': review.previewID});
      expect(
        requests.every((r) => r.headers['Authorization'] == 'Bearer A'),
        true,
      );
      api.close();
      api.close();
      expect(c.closes, 0);
      final hung = Completer<http.Response>();
      final api2 = IntentActivityConversionAPI(
        client: MockClient((_) => hung.future),
        timeout: const Duration(milliseconds: 20),
      );
      await expectLater(
        api2.options('Bearer A', nowOwner, nowIntent),
        throwsA(isA<TimeoutException>()),
      );
      api2.close();
      hung.complete(conversionResponse(conversionList()));
    },
  );
  test(
    'specific response validates owner chosen original source and preview expiry',
    () {
      final p = conversionPreview();
      p['expiresAt'] = DateTime.now()
          .toUtc()
          .add(const Duration(minutes: 2))
          .toIso8601String();
      expect(
        () => ConversionPreview(p, nowOwner, nowIntent),
        throwsFormatException,
      );
      final r = conversionReceipt(conversionPreview());
      r['intent']['convertedParticipationId'] = conversionActivity;
      expect(
        () => ConversionReceipt(r, nowOwner, nowIntent),
        throwsFormatException,
      );
    },
  );
}
