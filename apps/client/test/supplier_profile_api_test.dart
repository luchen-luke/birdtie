import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/supplier_profile_api.dart';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const supplierID = 'b1700000-0000-4000-8000-000000000001';
Map<String, dynamic> publicSupplier({bool published = true}) => {
  'id': supplierID,
  'name': '合成商家',
  'verificationStatus': 'verified',
  'profileStatus': published ? 'verified' : 'unpublished',
  'description': published ? '获准公开的介绍' : '',
  'officialLinks': published
      ? ['https://example.invalid/official']
      : <String>[],
  'profileVersion': published ? 2 : 0,
  'reviewedAt': published ? '2026-10-03T10:00:00Z' : null,
  'validUntil': published ? '2027-01-01T10:00:00Z' : null,
  'upcomingActivities': <Object>[],
};
http.Response supplierResponse(Map<String, dynamic> data) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), 200);

void main() {
  test('response business ID must match the requested entity', () async {
    final api = SupplierProfileApi(
      authorizationHeader: () => null,
      apiBaseUrl: 'http://127.0.0.1:3697',
      client: MockClient(
        (_) async => supplierResponse(
          publicSupplier()..['id'] = 'b1700000-0000-4000-8000-000000000002',
        ),
      ),
    );
    await expectLater(
      api.readPublic(supplierID),
      throwsA(isA<BusinessApiException>()),
    );
  });
  test(
    'public DTO is closed and management review does not imply publication',
    () {
      final p = PublicBusinessProfile.fromJson(
        publicSupplier(published: false),
      );
      expect(p.description, isEmpty);
      expect(p.links, isEmpty);
      for (final j in [
        publicSupplier()..['rightsNote'] = 'private',
        publicSupplier(published: false)
          ..['officialLinks'] = ['https://example.invalid/private'],
        publicSupplier()..['id'] = 'unknown',
        publicSupplier()
          ..['officialLinks'] = ['https://user:secret@example.invalid'],
      ]) {
        expect(
          () => PublicBusinessProfile.fromJson(j),
          throwsA(isA<BusinessApiException>()),
        );
      }
    },
  );
  test(
    'timestamps reject normalization and links reject non HTTPS or private credentials',
    () {
      expect(supplierStamp('2026-10-03T10:00:00.123456789Z').isUtc, isTrue);
      for (final s in [
        '2026-02-30T10:00:00Z',
        '2026-10-03T25:00:00Z',
        '2026-10-03T10:00:00+00:00',
        '2026-10-03T10:00:60Z',
      ]) {
        expect(() => supplierStamp(s), throwsA(isA<BusinessApiException>()));
      }
      for (final s in [
        'http://example.invalid',
        'https://a:b@example.invalid',
        'https://example.invalid/#x',
        'https://example.invalid/a b',
      ]) {
        expect(supplierHTTPS(s), isNull);
      }
    },
  );
  test(
    'owner read requires ordinary signed in identity before network',
    () async {
      var calls = 0;
      String? token;
      String? workspace;
      final api = SupplierProfileApi(
        authorizationHeader: () => token,
        workspaceID: () => workspace,
        apiBaseUrl: 'http://127.0.0.1:3697',
        client: MockClient((_) async {
          calls++;
          return supplierResponse({});
        }),
      );
      await expectLater(
        api.readPermission(supplierID),
        throwsA(
          isA<BusinessApiException>().having((e) => e.status, 'status', 401),
        ),
      );
      token = 'Bearer local';
      workspace = 'organization';
      await expectLater(
        api.readPermission(supplierID),
        throwsA(
          isA<BusinessApiException>().having((e) => e.status, 'status', 403),
        ),
      );
      expect(calls, 0);
    },
  );
  test('identity change rejects delayed public response', () async {
    var token = 'Bearer A';
    final gate = Completer<http.Response>();
    final api = SupplierProfileApi(
      authorizationHeader: () => token,
      apiBaseUrl: 'http://127.0.0.1:3697',
      client: MockClient((_) => gate.future),
    );
    final result = api.readPublic(supplierID);
    token = 'Bearer B';
    gate.complete(supplierResponse(publicSupplier()));
    await expectLater(result, throwsA(isA<BusinessApiException>()));
  });
  test(
    'unknown write outcome is explicit and never automatically retried',
    () async {
      var calls = 0;
      final api = SupplierProfileApi(
        authorizationHeader: () => 'Bearer A',
        apiBaseUrl: 'http://127.0.0.1:3697',
        client: MockClient((_) async {
          calls++;
          throw StateError('connection lost');
        }),
      );
      await expectLater(
        api.changePermission(supplierID, {}),
        throwsA(
          isA<BusinessApiException>().having(
            (e) => e.outcomeUnknown,
            'unknown',
            true,
          ),
        ),
      );
      expect(calls, 1);
    },
  );
}
