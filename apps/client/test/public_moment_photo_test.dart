import 'dart:convert';
import 'dart:typed_data';
import 'package:birdtie_client/src/workspace/public_moment_photo.dart';
import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_cover_test.dart' show coverJPEG, coverRights, coverSettle;

const photoMoment = '74100000-0000-4000-8000-000000000001';
const photoOwner = '74100000-0000-4000-8000-000000000002';
const photoSlot = '74100000-0000-4000-8000-000000000003';
const photoOperation = '74100000-0000-4000-8000-000000000004';
const photoBase = 'https://public-photo-isolated.test';
Map<String, dynamic> photoMetadata({
  String id = photoMoment,
  int revision = 2,
  int version = 1,
  DateTime? at,
  Duration lease = const Duration(seconds: 20),
  Uint8List? pixels,
}) {
  final bytes = pixels ?? coverJPEG();
  return {
    'momentId': id,
    'momentRevision': revision,
    'photoId': photoSlot,
    'version': version,
    'thumbnailPath': publicMomentPhotoPath(id, revision, photoSlot, version),
    'width': 320,
    'height': 180,
    'byteCount': bytes.length,
    'sha256': sha256.convert(bytes).toString(),
    'expiresAt': (at ?? DateTime.now().toUtc()).add(lease).toIso8601String(),
    'attribution': '合成测试图片 · 非真实发布',
  };
}

Map<String, dynamic> photoState({
  bool retired = false,
  int revision = 2,
  int version = 0,
  DateTime? at,
  bool withPhoto = false,
}) {
  final now = at ?? DateTime.now().toUtc();
  return {
    'momentId': photoMoment,
    'momentRevision': revision,
    'photoId': version == 0 ? null : photoSlot,
    'photoVersion': version,
    'photo': withPhoto
        ? photoMetadata(at: now, revision: revision, version: version)
        : null,
    'canSave':
        !retired && revision >= 2 && version < publicMomentPhotoMaxVersion,
    'canDelete': version > 0,
    'observedAt': now.toIso8601String(),
    'expiresAt': now.add(const Duration(seconds: 29)).toIso8601String(),
    'sourceVersion': 'a' * 64,
  };
}

Map<String, dynamic> photoApproval(
  Map<String, dynamic> body, {
  DateTime? at,
  Duration lease = const Duration(seconds: 24),
  Uint8List? derivative,
}) {
  final now = at ?? DateTime.now().toUtc(), save = body['action'] == 'SAVE';
  final bytes = derivative ?? coverJPEG();
  final r = body['rights'] as Map<String, dynamic>?;
  final rights = r == null
      ? null
      : EntityCoverRights(
          basis: r['basis'],
          attribution: r['attribution'],
          licenseReference: r['licenseReference'],
          retentionUntil: DateTime.parse(r['retentionUntil']),
          canRetain: r['canRetain'],
          canPublish: r['canPublish'],
        );
  final inputSHA = save ? sha256.convert(bytes).toString() : '',
      rightsSHA = rights?.digest ?? '';
  return {
    'operationId': body['operationId'],
    'action': body['action'],
    'payloadDigest': humanControlTupleDigest([
      'public-moment-photo-v1',
      photoMoment,
      body['operationId'],
      body['action'],
      body['expectedMomentRevision'],
      body['photoId'],
      body['expectedPhotoVersion'],
      inputSHA,
      rightsSHA,
    ]),
    'observedAt': now.toIso8601String(),
    'expiresAt': now.add(lease).toIso8601String(),
    'approval': 'hw1.synthetic-only-approval',
    'momentId': photoMoment,
    'photoId': body['photoId'],
    'expectedMomentRevision': body['expectedMomentRevision'],
    'expectedPhotoVersion': body['expectedPhotoVersion'],
    'inputSha256': inputSHA,
    'rightsSha256': rightsSHA,
    'sourceVersion': 'a' * 64,
    if (save)
      'previewDerivative': {
        'contentType': 'image/jpeg',
        'width': 320,
        'height': 180,
        'byteCount': bytes.length,
        'sha256': inputSHA,
        'contentBase64': base64Encode(bytes),
      },
  };
}

Map<String, dynamic> photoReceipt(
  Map<String, dynamic> p, {
  bool stop = false,
}) => stop
    ? {
        'operationId': p['operationId'],
        'status': 'STOPPED',
        'appliedPhotoVersion': 0,
      }
    : {
        'operationId': p['operationId'],
        'status': 'SUCCEEDED',
        'action': p['action'],
        'momentId': photoMoment,
        'photoId': p['photoId'],
        'expectedMomentRevision': p['expectedMomentRevision'],
        'expectedPhotoVersion': p['expectedPhotoVersion'],
        'appliedPhotoVersion':
            p['expectedPhotoVersion'] == publicMomentPhotoMaxVersion
            ? publicMomentPhotoMaxVersion
            : p['expectedPhotoVersion'] + 1,
        'payloadDigest': p['payloadDigest'],
        'committedAt': DateTime.now().toUtc().toIso8601String(),
      };
http.Response photoWire(Object? data, {int status = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
PublicMomentPhotoInput photoInput({
  String action = 'SAVE',
  int parent = 2,
  int version = 0,
}) => PublicMomentPhotoInput(
  momentID: photoMoment,
  operationID: photoOperation,
  action: action,
  expectedMomentRevision: parent,
  photoID: photoSlot,
  expectedPhotoVersion: version,
  bytes: action == 'SAVE' ? coverJPEG() : null,
  rights: action == 'SAVE' ? coverRights() : null,
);

Future<void> photoDecodeVisible(WidgetTester t, {Finder? image}) async {
  await coverSettle(t);
  final f = image ?? find.byType(Image);
  expect(f, findsOneWidget);
  await t.runAsync(
    () => precacheImage(
      t.widget<Image>(f).image,
      t.element(f),
    ).timeout(const Duration(seconds: 2)),
  );
  await t.pump();
  await t.pumpAndSettle();
  final raw = t.widget<RawImage>(
    find.descendant(of: f, matching: find.byType(RawImage)),
  );
  expect(raw.image, isNotNull);
  expect(raw.image!.width, 320);
  expect(raw.image!.height, 180);
}

void main() {
  test(
    'public photo metadata exact parent revision ID same origin path and bounded lease',
    () {
      final at = DateTime.now().toUtc(),
          m = photoMetadata(at: DateTime.now().toUtc());
      expect(
        PublicMomentPhotoMetadata.decode(
          m,
          photoMoment,
          at,
          expectedMomentRevision: 2,
        ).momentRevision,
        2,
      );
      for (final delta in <Map<String, Object?>>[
        {'momentId': photoOwner},
        {'momentRevision': 1},
        {'momentRevision': 3},
        {'photoId': photoOperation},
        {'version': publicMomentPhotoMaxVersion + 1},
        {'thumbnailPath': 'https://foreign.test/image.jpg'},
        {'thumbnailPath': '/v1/me/moments/$photoMoment/images/private/content'},
        {'width': 128},
        {'attribution': 'bad\nlabel'},
        {'expiresAt': at.add(const Duration(seconds: 61)).toIso8601String()},
        {'privateAttachmentId': photoSlot},
      ]) {
        expect(
          () => PublicMomentPhotoMetadata.decode(
            {...m, ...delta},
            photoMoment,
            at,
            expectedMomentRevision: 2,
          ),
          throwsFormatException,
        );
      }
      final old = photoMetadata(at: at.subtract(const Duration(seconds: 40)));
      expect(
        PublicMomentPhotoMetadata.decode(
          old,
          photoMoment,
          at.subtract(const Duration(seconds: 40)),
        ).current,
        isFalse,
      );
      expect(
        () => PublicMomentPhotoMetadata.decode(
          m,
          photoMoment,
          at,
          validUntil: at.add(const Duration(seconds: 10)),
        ),
        throwsFormatException,
      );
    },
  );
  test(
    'owner cleanup accepts hidden nullable photo and old parent but SAVE public revision minimum',
    () {
      final s = PublicMomentPhotoState.decode(
        photoState(
          retired: true,
          revision: 1,
          version: publicMomentPhotoMaxVersion,
        ),
        photoMoment,
      );
      expect(s.canDelete, isTrue);
      expect(s.photo, isNull);
      expect(s.canSave, isFalse);
      expect(
        photoInput(
          action: 'DELETE',
          parent: 1,
          version: publicMomentPhotoMaxVersion,
        ).body()['expectedPhotoVersion'],
        publicMomentPhotoMaxVersion,
      );
      expect(() => photoInput(parent: 1), throwsFormatException);
      expect(
        () => PublicMomentPhotoState.decode({
          ...photoState(),
          'canDelete': true,
        }, photoMoment),
        throwsFormatException,
      );
      final tooLong = photoState(withPhoto: true, version: 1);
      (tooLong['photo'] as Map)['expiresAt'] = DateTime.now()
          .toUtc()
          .add(const Duration(seconds: 40))
          .toIso8601String();
      expect(
        () => PublicMomentPhotoState.decode(tooLong, photoMoment),
        throwsFormatException,
      );
    },
  );
  test(
    'normalized JPEG echo hash rights and standard Base64 exact approval; prior state clips deadline',
    () {
      final input = photoInput(),
          raw = photoApproval(photoInput().body()),
          expiry = DateTime.now().toUtc().add(const Duration(seconds: 5));
      final p = PublicMomentPhotoPreview.decode(raw, input, expiry);
      expect(p.expiresAt, expiry);
      expect(p.derivative, coverJPEG());
      for (final key in [
        'photoId',
        'sourceVersion',
        'rightsSha256',
        'payloadDigest',
      ]) {
        expect(
          () => PublicMomentPhotoPreview.decode(
            {...raw, key: 'bad'},
            input,
            expiry,
          ),
          throwsFormatException,
        );
      }
      final d = {
        ...(raw['previewDerivative'] as Map<String, dynamic>),
        'contentBase64': base64Encode(coverJPEG()).replaceAll('=', ''),
      };
      expect(
        () => PublicMomentPhotoPreview.decode(
          {...raw, 'previewDerivative': d},
          input,
          expiry,
        ),
        throwsFormatException,
      );
    },
  );
  test(
    'tiny original receipt exact source digest and capped delete; STOP cannot contain source or title',
    () {
      final input = photoInput(
            action: 'DELETE',
            parent: 1,
            version: publicMomentPhotoMaxVersion,
          ),
          p = photoApproval(
            photoInput(
              action: 'DELETE',
              parent: 1,
              version: publicMomentPhotoMaxVersion,
            ).body(),
          );
      PublicMomentPhotoReceipt decode(Object? r) =>
          PublicMomentPhotoReceipt.decode(
            r,
            operationID: input.operationID,
            momentID: input.momentID,
            photoID: input.photoID,
            action: input.action,
            expectedMomentRevision: input.expectedMomentRevision,
            expectedPhotoVersion: input.expectedPhotoVersion,
            digest: p['payloadDigest'],
          );
      expect(
        decode(photoReceipt(p)).appliedPhotoVersion,
        publicMomentPhotoMaxVersion,
      );
      expect(decode(photoReceipt(p, stop: true)).status, 'STOPPED');
      expect(
        () => decode({...photoReceipt(p), 'payloadDigest': 'b' * 64}),
        throwsFormatException,
      );
      expect(
        () => decode({...photoReceipt(p, stop: true), 'momentId': photoMoment}),
        throwsFormatException,
      );
    },
  );
  test(
    'public controlled content verifies actual bytes SHA no foreign redirect or token fallback',
    () async {
      final at = DateTime.now().toUtc(),
          m = PublicMomentPhotoMetadata.decode(
            photoMetadata(),
            photoMoment,
            DateTime.now().toUtc(),
          );
      var reads = 0;
      final api = PublicMomentPhotoAPI(
        client: MockClient((r) async {
          reads++;
          expectSync(r.url.origin, photoBase);
          expectSync(r.url.path, m.path);
          expectSync(
            r.headers.containsKey('X-Birdtie-Organization-Workspace'),
            isFalse,
          );
          return http.Response.bytes(coverJPEG(), 200);
        }),
        baseUrl: photoBase,
        authorizationHeader: () => null,
      );
      expect(await api.content(m), coverJPEG());
      expect(reads, 1);
      final wrong = PublicMomentPhotoMetadata.decode(
        {...photoMetadata(at: at), 'sha256': 'b' * 64},
        photoMoment,
        at,
      );
      await expectLater(api.content(wrong), throwsFormatException);
    },
  );
  testWidgets(
    'public actual JPEG visible then replacement late read and source expiry retire pixels',
    (t) async {
      final identity = ValueNotifier(0);
      var live = true;
      final at = DateTime.now().toUtc(),
          m = PublicMomentPhotoMetadata.decode(
            photoMetadata(at: DateTime.now().toUtc()),
            photoMoment,
            at,
          );
      final api = PublicMomentPhotoAPI(
        client: MockClient((r) async => http.Response.bytes(coverJPEG(), 200)),
        baseUrl: photoBase,
        authorizationHeader: () => null,
      );
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PublicMomentPhoto(
              metadata: m,
              api: api,
              current: () => live,
              identityChanges: identity,
            ),
          ),
        ),
      );
      await photoDecodeVisible(t);
      live = false;
      identity.value++;
      await t.pump();
      expect(find.byType(RawImage), findsNothing);
      expect(find.text(m.attribution), findsNothing);
      await t.pumpWidget(const SizedBox());
      identity.dispose();
    },
  );
  testWidgets(
    'public header valid but corrupt JPEG decoder hides entire media and attribution',
    (t) async {
      final bytes = coverJPEG();
      for (var i = 0; i < bytes.length - 4; i++) {
        if (bytes[i] == 255 && bytes[i + 1] == 192) {
          bytes[i + 4] = 255;
          break;
        }
      }
      final at = DateTime.now().toUtc(),
          m = PublicMomentPhotoMetadata.decode(
            photoMetadata(at: DateTime.now().toUtc(), pixels: bytes),
            photoMoment,
            at,
          );
      final api = PublicMomentPhotoAPI(
        client: MockClient((r) async => http.Response.bytes(bytes, 200)),
        baseUrl: photoBase,
        authorizationHeader: () => null,
      );
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PublicMomentPhoto(metadata: m, api: api, current: () => true),
          ),
        ),
      );
      await coverSettle(t);
      expect(find.byType(Image), findsNothing);
      expect(find.text(m.attribution), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
  );
}
