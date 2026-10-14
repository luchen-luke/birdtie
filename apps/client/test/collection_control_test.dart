import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/collection_control.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const ccOperation = '10000000-0000-4000-8000-000000000001';
const ccTarget = '20000000-0000-4000-8000-000000000002';
const ccFirst = '30000000-0000-4000-8000-000000000003';
const ccSecond = '40000000-0000-4000-8000-000000000004';
CollectionControlInput ccInput({String action = 'UPDATE', int revision = 7}) =>
    CollectionControlInput(
      operationID: ccOperation,
      action: action,
      kind: 'journey',
      collectionID: ccTarget,
      expectedRevision: revision,
      input: action == 'UPDATE'
          ? CollectionControlBody.normalize({
              'kind': 'journey',
              'cityId': 'aberdeen-gb',
              'title': '我的行程',
              'summary': '文字 <>& 原样',
              'entries': [
                {
                  'placeId': ccFirst,
                  'note': '私人备注',
                  'locationVisibility': 'hidden',
                },
                {
                  'placeId': ccSecond,
                  'note': '第二站',
                  'locationVisibility': 'city',
                },
              ],
            })
          : null,
    );
CollectionControlInput ccRequest(http.Request request) {
  final raw = jsonDecode(request.body) as Map<String, dynamic>;
  expectSync(raw['schemaVersion'], collectionControlSchema);
  expectSync(raw.keys.toSet(), {
    'schemaVersion',
    'operationId',
    'action',
    'kind',
    'collectionId',
    'expectedRevision',
    if (raw['action'] == 'UPDATE') 'input',
    if (request.method == 'PUT') 'approval',
  });
  return CollectionControlInput(
    operationID: raw['operationId'],
    action: raw['action'],
    kind: raw['kind'],
    collectionID: raw['collectionId'],
    expectedRevision: raw['expectedRevision'],
    input: raw['input'] == null
        ? null
        : CollectionControlBody.normalize(raw['input']),
  );
}

Map<String, Object> ccPreview(
  CollectionControlInput input, {
  DateTime? observedAt,
  DateTime? expiresAt,
}) {
  final at = observedAt ?? DateTime.now().toUtc();
  return {
    ...input.toJson()..remove('input'),
    if (input.input != null) 'normalizedInput': input.input!.toJson(),
    'payloadDigest': input.payloadDigest,
    'observedAt': at.toIso8601String(),
    'expiresAt': (expiresAt ?? at.add(const Duration(seconds: 25)))
        .toIso8601String(),
    'approval': 'synthetic-specific-approval',
  };
}

Map<String, Object> ccReceipt(
  CollectionControlInput input, {
  bool stopped = false,
}) => stopped
    ? {
        'schemaVersion': collectionControlSchema,
        'operationId': input.operationID,
        'status': 'STOPPED',
      }
    : {
        'schemaVersion': collectionControlSchema,
        'operationId': input.operationID,
        'status': 'SUCCEEDED',
        'action': input.action,
        'kind': input.kind,
        'collectionId': input.collectionID,
        'expectedRevision': input.expectedRevision,
        'appliedRevision':
            input.expectedRevision == collectionControlMaxRevision
            ? input.expectedRevision
            : input.expectedRevision + 1,
        'payloadDigest': input.payloadDigest,
        'committedAt': DateTime.now().toUtc().toIso8601String(),
      };
http.Response ccResponse(Object? data, {int status = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    '147 digest matches Go normalized Chinese ASCII golden without op in payload hash',
    () {
      final input = ccInput();
      expect(
        input.payloadDigest,
        'cbeea2b46b2093e9e0e91f8dd7d51a65c7ffa9489d8d4d21bfddd2e957d36ee2',
      );
      expect(
        ccInput(action: 'WITHDRAW').payloadDigest,
        '2729fe1072757aa3a9e06b0a0dcc91ee943ced2b5c774ffae3823a1e3c6587ab',
      );
      final same = CollectionControlInput(
        operationID: ccSecond,
        action: input.action,
        kind: input.kind,
        collectionID: input.collectionID,
        expectedRevision: input.expectedRevision,
        input: input.input,
      );
      expect(same.payloadDigest, input.payloadDigest);
      expect(same.operationID, isNot(input.operationID));
    },
  );
  test(
    '147 minimum normalized material excludes every authoritative Place field and preserves order',
    () {
      final body = CollectionControlBody.normalize({
        'kind': 'journey',
        'cityId': ' aberdeen-gb ',
        'title': ' 本人名字 ',
        'summary': ' <>& 本人说明 ',
        'entries': [
          {
            'placeId': ccSecond.toUpperCase(),
            'note': ' 私人备注 ',
            'locationVisibility': 'hidden',
          },
          {'placeId': ccFirst, 'note': '', 'locationVisibility': 'city'},
        ],
      });
      expect(body.title, '本人名字');
      expect(body.summary, '<>& 本人说明');
      expect(body.entries.map((e) => e.placeID), [ccSecond, ccFirst]);
      expect(body.entries.first.toJson(), {
        'placeId': ccSecond,
        'note': '私人备注',
        'locationVisibility': 'hidden',
      });
      for (final field in [
        'available',
        'placeName',
        'latitude',
        'sourceExpiresAt',
      ]) {
        expect(
          () => CollectionControlBody.normalize({
            ...body.toJson(),
            'entries': [
              {...body.entries.first.toJson(), field: true},
            ],
          }),
          throwsFormatException,
        );
      }
    },
  );
  test(
    '147 closed normalized echo source selector and strict deadline cannot approve different material',
    () {
      final input = ccInput(), wire = ccPreview(input);
      expect(
        CollectionControlPreview.decode(wire, input).input.payloadDigest,
        input.payloadDigest,
      );
      for (final patch in <Map<String, Object>>[
        {'input': input.input!.toJson()},
        {'collectionId': ccFirst},
        {'expectedRevision': 8},
        {'payloadDigest': 'a' * 64},
        {'schemaVersion': 'foreign'},
        {
          'normalizedInput': {...input.input!.toJson(), 'title': '其他文字'},
        },
        {'approval': ' synthetic '},
        {
          'expiresAt': DateTime.now()
              .toUtc()
              .subtract(const Duration(seconds: 1))
              .toIso8601String(),
        },
        {
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 90))
              .toIso8601String(),
        },
      ]) {
        expect(
          () => CollectionControlPreview.decode({...wire, ...patch}, input),
          throwsFormatException,
        );
      }
    },
  );
  test(
    '147 MAX update forbidden while withdraw saturated terminal receipt remains valid',
    () {
      expect(
        () => ccInput(revision: collectionControlMaxRevision),
        throwsFormatException,
      );
      final input = ccInput(
        action: 'WITHDRAW',
        revision: collectionControlMaxRevision,
      );
      final receipt = CollectionControlReceipt.decode(
        ccReceipt(input),
        operationID: input.operationID,
        action: input.action,
        collectionID: input.collectionID,
        expectedRevision: input.expectedRevision,
        payloadDigest: input.payloadDigest,
      );
      expect(receipt.appliedRevision, collectionControlMaxRevision);
      expect(
        () => CollectionControlReceipt.decode({
          ...ccReceipt(input),
          'appliedRevision': 1,
        }, operationID: input.operationID),
        throwsFormatException,
      );
    },
  );
  test(
    '147 tiny original facts enforce op kind ID CAS digest and STOP cannot borrow current body',
    () {
      final input = ccInput();
      for (final patch in <Map<String, Object>>[
        {'operationId': ccFirst},
        {'kind': 'theme_map'},
        {'collectionId': ccFirst},
        {'expectedRevision': 8},
        {'payloadDigest': 'f' * 64},
        {'appliedRevision': 9},
        {'body': 'not a fact'},
      ]) {
        expect(
          () => CollectionControlReceipt.decode(
            {...ccReceipt(input), ...patch},
            operationID: input.operationID,
            action: input.action,
            kind: input.kind,
            collectionID: input.collectionID,
            expectedRevision: input.expectedRevision,
            payloadDigest: input.payloadDigest,
          ),
          throwsFormatException,
        );
      }
      expect(
        CollectionControlReceipt.decode(
          ccReceipt(input, stopped: true),
          operationID: input.operationID,
        ).succeeded,
        false,
      );
      expect(
        () => CollectionControlReceipt.decode({
          ...ccReceipt(input, stopped: true),
          'appliedRevision': 0,
        }, operationID: input.operationID),
        throwsFormatException,
      );
      expect(
        () => collectionControlJSON('{"data":1,"d\\u0061ta":2}'),
        throwsFormatException,
      );
    },
  );
  test(
    '147 original GET and STOP bodyless noquery isolated routes do not replay UPDATE or raw delete',
    () async {
      final input = ccInput(), calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        expectSync(r.url.hasQuery, false);
        expectSync(
          r.headers.keys.any((k) => k.toLowerCase().contains('workspace')),
          false,
        );
        if (r.url.path.endsWith('/preview'))
          return ccResponse(ccPreview(ccRequest(r)));
        if (r.method == 'PUT') return ccResponse(ccReceipt(ccRequest(r)));
        expectSync(r.bodyBytes, isEmpty);
        return ccResponse(ccReceipt(input, stopped: true));
      });
      final api = CollectionControlAPI(
        client: client,
        apiBaseUrl: 'https://control-synthetic.test',
        authorizationHeader: () => 'Bearer synthetic',
      );
      final p = await api.preview(input, () => true);
      expect((await api.put(p, () => true)).succeeded, true);
      await api.original(input.operationID, () => true);
      await api.original(input.operationID, () => true, stop: true);
      expect(calls.map((r) => r.method), ['POST', 'PUT', 'GET', 'POST']);
      expect(
        calls.first.url.path,
        '/v1/me/personal-collection-controls/preview',
      );
      expect(
        calls.last.url.path,
        '/v1/me/personal-collection-controls/operations/' +
            input.operationID +
            '/stop',
      );
      client.close();
    },
  );
  test(
    '147 late preview after fresh same-owner header is never a valid specific approval',
    () async {
      var header = 'Bearer first';
      final pending = Completer<http.Response>();
      final client = MockClient((_) => pending.future);
      final api = CollectionControlAPI(
        client: client,
        apiBaseUrl: 'https://control-synthetic.test',
        authorizationHeader: () => header,
      );
      final future = api.preview(ccInput(), () => true);
      final expectation = expectLater(
        future,
        throwsA(isA<CollectionControlFailure>()),
      );
      await Future<void>.delayed(Duration.zero);
      header = 'Bearer renewed';
      pending.complete(ccResponse(ccPreview(ccInput())));
      await expectation;
      client.close();
    },
  );
}
