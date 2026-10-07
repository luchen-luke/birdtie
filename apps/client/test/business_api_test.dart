import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const businessID = 'bb000000-0000-4000-8000-000000000001';
const personID = 'bb000000-0000-4000-8000-000000000002';
const placeID = 'bb000000-0000-4000-8000-000000000003';

Map<String, dynamic> summary() => {
  'id': businessID,
  'name': '本地合成商家',
  'claimStatus': 'pending',
  'role': 'owner',
};

Map<String, dynamic> console() => {
  'business': summary(),
  'canManage': true,
  'canManageMembers': true,
  'canReview': false,
  'reviewPermissions': <String>[],
  'membershipVersion': 0,
  'claim': {
    'version': 1,
    'state': 'pending',
    'submittedBy': personID,
    'name': '本地合成商家',
  },
  'profile': null,
  'venues': <dynamic>[],
  'members': <dynamic>[],
};

http.Response response(dynamic data, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);

BusinessApi api(
  FutureOr<http.Response> Function(http.Request) handler, {
  String? Function()? header,
  Duration timeout = const Duration(seconds: 2),
}) => BusinessApi(
  authorizationHeader: header ?? () => 'Bearer local-contract-session',
  client: MockClient((request) async => handler(request)),
  apiBaseUrl: 'http://127.0.0.1:1',
  timeout: timeout,
);

void main() {
  test(
    'Business read uses only current native bearer and ordinary route',
    () async {
      final service = api((request) {
        expect(request.method, 'GET');
        expect(request.url.path, '/v1/me/businesses/$businessID/console');
        expect(request.url.query, isEmpty);
        expect(
          request.headers['Authorization'],
          'Bearer local-contract-session',
        );
        expect(
          request.headers.keys.any(
            (k) => k.toLowerCase().contains('organization'),
          ),
          isFalse,
        );
        expect(
          request.headers.keys.any((k) => k.toLowerCase().contains('agent')),
          isFalse,
        );
        return response(console());
      });
      final snapshot = await service.read(businessID);
      expect(snapshot.canManage, isTrue);
      expect(snapshot.claim!['version'], 1);
      expect(() => snapshot.claim!['version'] = 9, throwsUnsupportedError);
      expect(() => snapshot.venues.add({}), throwsUnsupportedError);
    },
  );

  test('Missing native session prevents any HTTP call', () async {
    var calls = 0;
    final service = api((_) {
      calls++;
      return response([]);
    }, header: () => null);
    await expectLater(
      service.list(),
      throwsA(
        isA<BusinessApiException>().having((e) => e.status, 'status', 401),
      ),
    );
    expect(calls, 0);
  });

  test(
    'Canonical Business and Place IDs cannot inject a different route',
    () async {
      var calls = 0;
      final service = api((_) {
        calls++;
        return response(console());
      });
      await expectLater(
        service.read('../other'),
        throwsA(isA<BusinessApiException>()),
      );
      expect(
        () => service.putVenue(businessID, '$placeID/other', {}),
        throwsA(isA<BusinessApiException>()),
      );
      expect(calls, 0);
    },
  );

  test('Response for another Business is rejected', () async {
    final service = api(
      (_) => response(console()..['business'] = (summary()..['id'] = placeID)),
    );
    await expectLater(
      service.read(businessID),
      throwsA(isA<BusinessApiException>()),
    );
  });

  test('Account token change during HTTP rejects old visible data', () async {
    var token = 'Bearer account-a';
    final pending = Completer<http.Response>();
    final service = api((_) => pending.future, header: () => token);
    final requested = service.list();
    token = 'Bearer account-b';
    pending.complete(response([summary()]));
    await expectLater(
      requested,
      throwsA(
        isA<BusinessApiException>().having((e) => e.status, 'status', 409),
      ),
    );
  });

  test('Exact write body is captured before draft edits; no retry', () async {
    var calls = 0;
    final pending = Completer<http.Response>();
    final draft = {'expectedVersion': 0, 'name': '已预览版本'};
    final service = api((request) {
      calls++;
      expect(request.method, 'PUT');
      expect(jsonDecode(request.body), {'expectedVersion': 0, 'name': '已预览版本'});
      return pending.future;
    });
    final submitted = service.submitClaim(businessID, draft);
    draft['name'] = '后来的编辑';
    pending.complete(
      response({'version': 1, 'state': 'pending', 'submittedBy': personID}),
    );
    expect((await submitted)['version'], 1);
    expect(calls, 1);
  });

  for (final status in [401, 403, 500, 503]) {
    test(
      'Write HTTP $status may follow commit; result stays unknown',
      () async {
        var calls = 0;
        final service = api((_) {
          calls++;
          return response(null, status);
        });
        await expectLater(
          service.putProfile(businessID, {}),
          throwsA(
            isA<BusinessApiException>().having(
              (e) => e.outcomeUnknown,
              'outcomeUnknown',
              isTrue,
            ),
          ),
        );
        expect(calls, 1);
      },
    );
  }

  test(
    'Malformed successful member response does not claim rollback',
    () async {
      final service = api((_) => response({'business': summary()}));
      await expectLater(
        service.changeMember(businessID, {}),
        throwsA(
          isA<BusinessApiException>().having(
            (e) => e.outcomeUnknown,
            'unknown',
            isTrue,
          ),
        ),
      );
    },
  );

  test('Malformed fact acknowledgement remains unknown', () async {
    final service = api((_) => response({'version': 1, 'state': 'pending'}));
    await expectLater(
      service.submitClaim(businessID, {}),
      throwsA(
        isA<BusinessApiException>().having(
          (e) => e.outcomeUnknown,
          'unknown',
          isTrue,
        ),
      ),
    );
  });

  test('Write timeout makes exactly one attempt and remains unknown', () async {
    var calls = 0;
    final pending = Completer<http.Response>();
    final service = api((_) {
      calls++;
      return pending.future;
    }, timeout: const Duration(milliseconds: 5));
    await expectLater(
      service.submitClaim(businessID, {}),
      throwsA(
        isA<BusinessApiException>().having(
          (e) => e.outcomeUnknown,
          'unknown',
          isTrue,
        ),
      ),
    );
    pending.complete(response({}));
    expect(calls, 1);
  });

  test(
    'Bounded list and closed reviewer permissions reject malformed source',
    () async {
      final many = api((_) => response(List.generate(101, (_) => summary())));
      await expectLater(many.list(), throwsA(isA<BusinessApiException>()));
      final invalid = api(
        (_) => response(console()..['reviewPermissions'] = ['claim', 'claim']),
      );
      await expectLater(
        invalid.read(businessID),
        throwsA(isA<BusinessApiException>()),
      );
    },
  );
}
