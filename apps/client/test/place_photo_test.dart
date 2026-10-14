import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:birdtie_client/src/workspace/place_photo.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'entity_cover_test.dart' show coverJPEG, coverRights, coverWire;
import 'person_avatar_fixture.dart'
    show avatarOwner, avatarPeer, avatarOperation;

const photoRef = PlacePhotoRef(avatarOwner);
const photoBase = 'https://place-photo-fixture.test';
const photoCandidateID = avatarPeer;
Map<String, dynamic> photoMetadata({
  int version = 1,
  Duration lease = const Duration(seconds: 50),
}) => {
  'entityType': 'place',
  'entityId': photoRef.id,
  'version': version,
  'thumbnailPath': '${photoRef.path}/$version/content',
  'width': 320,
  'height': 180,
  'byteCount': coverJPEG().length,
  'sha256': sha256.convert(coverJPEG()).toString(),
  'expiresAt': DateTime.now().toUtc().add(lease).toIso8601String(),
  'attribution': '隔离合成照片 fixture',
};
Map<String, dynamic> photoCandidate({
  String id = photoCandidateID,
  int version = 1,
  String status = 'PENDING',
  bool approve = false,
  bool reject = false,
  bool withdraw = true,
  bool pixels = true,
}) => {
  'id': id,
  'version': version,
  'status': status,
  'expectedVersion': 0,
  'rights': coverRights().toJson(),
  'thumbnail': pixels
      ? {
          'contentType': 'image/jpeg',
          'width': 320,
          'height': 180,
          'byteCount': coverJPEG().length,
          'sha256': sha256.convert(coverJPEG()).toString(),
        }
      : null,
  'canApprove': approve,
  'canReject': reject,
  'canWithdraw': withdraw,
};
Map<String, dynamic> photoEditorial({
  int version = 0,
  bool contribute = true,
  bool review = false,
  List<Map<String, dynamic>> candidates = const [],
}) => {
  'placeId': photoRef.id,
  'cityId': 'aberdeen-gb',
  'version': version,
  'canContribute': contribute,
  'canReview': review,
  'candidates': candidates,
  'observedAt': DateTime.now().toUtc().toIso8601String(),
  'expiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(seconds: 25))
      .toIso8601String(),
};
Map<String, dynamic> photoApproval(
  Map<String, dynamic> body, {
  EntityCoverRights? rights,
  Uint8List? bytes,
}) {
  final image = bytes ?? coverJPEG(), hash = sha256.convert(image).toString();
  final supplied = body['rights'] as Map<String, dynamic>?;
  rights ??= supplied == null
      ? coverRights()
      : EntityCoverRights(
          basis: supplied['basis'],
          attribution: supplied['attribution'],
          licenseReference: supplied['licenseReference'],
          retentionUntil: DateTime.parse(supplied['retentionUntil']),
          canRetain: supplied['canRetain'],
          canPublish: supplied['canPublish'],
        );
  return {
    'placeId': photoRef.id,
    'operationId': body['operationId'],
    'expectedVersion': body['expectedVersion'],
    'action': body['action'],
    'candidateId': body['action'] == 'SUBMIT'
        ? body['operationId']
        : body['candidateId'],
    'candidateVersion': body['candidateVersion'] ?? 0,
    'inputSha256': hash,
    'rightsSha256': rights.digest,
    'expiresAt': DateTime.now()
        .toUtc()
        .add(const Duration(seconds: 25))
        .toIso8601String(),
    'token': 'pp1.1.1.${'a' * 64}',
    if (body['action'] != 'WITHDRAW')
      'previewDerivative': {
        'contentType': 'image/jpeg',
        'width': 320,
        'height': 180,
        'byteCount': image.length,
        'sha256': hash,
        'contentBase64': base64Encode(image),
      },
  };
}

Map<String, dynamic> photoReceipt(
  Map<String, dynamic> body, {
  bool stopped = false,
  int? appliedVersion,
}) => {
  'placeId': photoRef.id,
  'operationId': body['operationId'],
  'action': stopped ? 'STOP' : body['action'],
  if (!stopped)
    'candidateId': body['action'] == 'SUBMIT'
        ? body['operationId']
        : body['candidateId'],
  'candidateVersion': stopped ? 0 : (body['candidateVersion'] as int? ?? 0) + 1,
  'appliedVersion': stopped
      ? 0
      : appliedVersion ??
            (body['action'] == 'APPROVE'
                ? (body['expectedVersion'] as int) + 1
                : 0),
  'status': stopped ? 'STOPPED' : 'SUCCEEDED',
};
PlacePhotoSelector photoSelector(String action, {int expected = 0}) =>
    PlacePhotoSelector(
      operationID: avatarOperation,
      expectedVersion: expected,
      action: action,
      candidateID: action == 'SUBMIT' ? '' : photoCandidateID,
      candidateVersion: action == 'SUBMIT' ? 0 : 1,
    );

void main() {
  test(
    'Place photo strict metadata has independent path and never widens cover refs',
    () {
      expect(photoRef.valid, isTrue);
      expect(const EntityCoverRef('place', avatarOwner).valid, isFalse);
      expect(
        EntityCoverMetadata.fromJson(photoMetadata(), photoRef).ref,
        photoRef,
      );
      for (final change in <Map<String, dynamic>>[
        {'entityType': 'activity'},
        {'entityId': avatarPeer},
        {'thumbnailPath': 'https://external.test/photo.jpg'},
        {'thumbnailPath': '/v1/places/${photoRef.id}/photos/2/content'},
        {'width': 128},
        {'sha256': 'A' * 64},
        {'byteCount': 262145},
        {
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 80))
              .toIso8601String(),
        },
        {'extra': true},
      ]) {
        expect(
          () => EntityCoverMetadata.fromJson({
            ...photoMetadata(),
            ...change,
          }, photoRef),
          throwsFormatException,
        );
      }
      expect(
        () => EntityCoverMetadata.fromJson(
          photoMetadata(),
          const EntityCoverRef('activity', avatarOwner),
        ),
        throwsFormatException,
      );
    },
  );
  test(
    'Place editorial uses canonical text city and different reviewer flags with no list pixels',
    () {
      final data = photoEditorial(
        review: true,
        contribute: false,
        candidates: [photoCandidate(approve: true, reject: true)],
      );
      final editorial = PlacePhotoEditorial.fromJson(data, photoRef);
      expect(editorial.cityID, 'aberdeen-gb');
      expect(editorial.candidates.single.canApprove, isTrue);
      expect(jsonEncode(data), isNot(contains('contentBase64')));
      expect(
        () => PlacePhotoEditorial.fromJson({
          ...data,
          'canReview': false,
        }, photoRef),
        throwsFormatException,
      );
      expect(
        () => PlacePhotoEditorial.fromJson({...data, 'cityId': ''}, photoRef),
        throwsFormatException,
      );
      expect(
        () => PlacePhotoEditorial.fromJson({
          ...data,
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 45))
              .toIso8601String(),
        }, photoRef),
        throwsFormatException,
      );
      expect(
        () => PlacePhotoEditorial.fromJson({
          ...data,
          'candidates': List.generate(21, (_) => photoCandidate()),
        }, photoRef),
        throwsFormatException,
      );
      final expired = photoCandidate(
        status: 'EXPIRED',
        pixels: false,
        withdraw: false,
      );
      (expired['rights'] as Map)['retentionUntil'] = '2020-01-01T00:00:00Z';
      expect(PlacePhotoCandidate.fromJson(expired).rights.valid, isFalse);
    },
  );
  test(
    'Place photo original actions use exact bodies approval derivative and receipts',
    () async {
      final calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        if (r.url.path.endsWith('/preview'))
          return coverWire(photoApproval(jsonDecode(r.body)));
        return coverWire(photoReceipt(jsonDecode(r.body)));
      });
      addTearDown(client.close);
      final api = PlacePhotoAPI(
        client: client,
        baseUrl: photoBase,
        authorizationHeader: () => 'Bearer original',
      );
      for (final action in ['SUBMIT', 'APPROVE', 'REJECT', 'WITHDRAW']) {
        final s = photoSelector(action), rights = coverRights();
        final bytes = action == 'SUBMIT' ? coverJPEG() : null;
        final approval = await api.preview(
          photoRef,
          s,
          bytes: bytes,
          rights: rights,
          thumbnail: action == 'SUBMIT'
              ? null
              : PlacePhotoThumbnail(
                  coverJPEG().length,
                  sha256.convert(coverJPEG()).toString(),
                ),
        );
        expect(approval.bytes == null, action == 'WITHDRAW');
        expect(
          (await api.change(
            photoRef,
            s,
            approval,
            bytes: bytes,
            rights: action == 'SUBMIT' ? rights : null,
          )).status,
          'SUCCEEDED',
        );
        final body = jsonDecode(calls[calls.length - 2].body) as Map;
        expect(
          body.keys,
          unorderedEquals(
            action == 'SUBMIT'
                ? [
                    'operationId',
                    'expectedVersion',
                    'action',
                    'contentType',
                    'contentBase64',
                    'rights',
                  ]
                : [
                    'operationId',
                    'expectedVersion',
                    'action',
                    'candidateId',
                    'candidateVersion',
                    if (action == 'APPROVE') ...[
                      'rightsConfirmed',
                      'photoConfirmed',
                    ],
                  ],
          ),
        );
        expect(calls.last.headers['Authorization'], 'Bearer original');
        expect(calls.last.followRedirects, isFalse);
        expect(calls.last.url.origin, Uri.parse(photoBase).origin);
      }
      expect(calls, hasLength(8));
      expect(
        () => api.input(
          photoSelector('APPROVE'),
          bytes: coverJPEG(),
          rights: coverRights(),
        ),
        throwsFormatException,
      );
      expect(
        () => PlacePhotoReceipt.fromJson(
          photoReceipt(photoSelector('APPROVE').toJson(), appliedVersion: 0),
          photoRef,
          photoSelector('APPROVE'),
        ),
        throwsFormatException,
      );
      expect(
        () => PlacePhotoReceipt.fromJson(
          {
            ...photoReceipt(photoSelector('REJECT').toJson()),
            'candidateId': avatarOwner,
          },
          photoRef,
          photoSelector('REJECT'),
        ),
        throwsFormatException,
      );
    },
  );
  test(
    'Place preview rejects wrong normalized SHA candidate rights token and derivative',
    () {
      final selector = photoSelector('APPROVE'), rights = coverRights();
      final raw = photoApproval(selector.toJson());
      final thumbnail = PlacePhotoThumbnail(
        coverJPEG().length,
        sha256.convert(coverJPEG()).toString(),
      );
      for (final diff in <Map<String, dynamic>>[
        {'candidateId': avatarOwner},
        {'candidateVersion': 2},
        {'rightsSha256': 'b' * 64},
        {'inputSha256': 'c' * 64},
        {'token': 'ec1.1.1.${'a' * 64}'},
        {
          'previewDerivative': {
            ...raw['previewDerivative'] as Map,
            'width': 128,
          },
        },
      ]) {
        expect(
          () => PlacePhotoPreview.fromJson(
            {...raw, ...diff},
            photoRef,
            selector,
            rights,
            thumbnail,
          ),
          throwsFormatException,
        );
      }
    },
  );
  test(
    'Place photo terminal cleanup at maximum version remains possible without new publication',
    () {
      for (final action in ['REJECT', 'WITHDRAW']) {
        final s = PlacePhotoSelector(
          operationID: avatarOperation,
          expectedVersion: entityCoverMaxVersion,
          action: action,
          candidateID: photoCandidateID,
          candidateVersion: entityCoverMaxVersion,
        );
        expect(s.valid, isTrue);
        final r = PlacePhotoReceipt.fromJson(
          {
            'placeId': photoRef.id,
            'operationId': avatarOperation,
            'action': action,
            'candidateId': photoCandidateID,
            'candidateVersion': entityCoverMaxVersion,
            'appliedVersion': action == 'WITHDRAW' ? entityCoverMaxVersion : 0,
            'status': 'SUCCEEDED',
          },
          photoRef,
          s,
        );
        expect(r.candidateVersion, entityCoverMaxVersion);
      }
      expect(
        PlacePhotoSelector(
          operationID: avatarOperation,
          expectedVersion: entityCoverMaxVersion,
          action: 'SUBMIT',
        ).valid,
        isFalse,
      );
      expect(
        PlacePhotoSelector(
          operationID: avatarOperation,
          expectedVersion: 0,
          action: 'APPROVE',
          candidateID: photoCandidateID,
          candidateVersion: entityCoverMaxVersion,
        ).valid,
        isFalse,
      );
    },
  );
  test(
    'Place API read late actor and unavailable original operation never invent result',
    () async {
      final response = Completer<http.Response>();
      var token = 'Bearer A', current = true;
      final methods = <String>[];
      final client = MockClient((r) async {
        methods.add(r.method);
        return r.url.path.endsWith('/editorial')
            ? response.future
            : coverWire({}, status: 404);
      });
      addTearDown(client.close);
      final api = PlacePhotoAPI(
        client: client,
        baseUrl: photoBase,
        authorizationHeader: () => token,
        current: () => current,
      );
      final future = api.editorial(photoRef);
      await Future<void>.delayed(Duration.zero);
      token = 'Bearer B';
      current = false;
      response.complete(coverWire(photoEditorial()));
      await expectLater(future, throwsA(isA<PlacePhotoFailure>()));
      token = 'Bearer A';
      current = true;
      await expectLater(
        api.operation(photoRef, photoSelector('SUBMIT')),
        throwsA(
          isA<PlacePhotoFailure>().having((e) => e.status, 'status', 404),
        ),
      );
      expect(methods, ['GET', 'GET']);
    },
  );
  test(
    'Place photo content uses original metadata same-origin digest dimensions and lease',
    () async {
      final paths = <String>[];
      final client = MockClient((r) async {
        paths.add(r.url.path);
        return http.Response.bytes(coverJPEG(), 200);
      });
      addTearDown(client.close);
      final api = PlacePhotoAPI(
        client: client,
        baseUrl: photoBase,
        authorizationHeader: () => null,
      );
      final metadata = EntityCoverMetadata.fromJson(photoMetadata(), photoRef);
      expect(await api.pixels.content(metadata), orderedEquals(coverJPEG()));
      expect(paths, ['${photoRef.path}/1/content']);
      final bad = EntityCoverMetadata.fromJson({
        ...photoMetadata(),
        'sha256': 'd' * 64,
      }, photoRef);
      await expectLater(api.pixels.content(bad), throwsFormatException);
      expect(const EntityCoverRef('place', avatarOwner).valid, isFalse);
    },
  );
}
