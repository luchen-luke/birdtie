import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'person_avatar_fixture.dart'
    show avatarJPEG, avatarOwner, avatarPeer, avatarOperation;

const coverRef = EntityCoverRef('activity', avatarOwner);
const coverBase = 'https://cover-fixture.test';
// Synthetic 320x180 JPEG, isolated fixture; never published to a real entity.
Uint8List coverJPEG() => base64Decode(
  '/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAUDBAQEAwUEBAQFBQUGBwwIBwcHBw8LCwkMEQ8SEhEPERETFhwXExQaFRERGCEYGh0dHx8fExciJCIeJBweHx7/2wBDAQUFBQcGBw4ICA4eFBEUHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh4eHh7/wAARCAC0AUADASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwDzeiiiv0U+NCiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooAKKKKACiiigAooooA/9k=',
);
Map<String, Object?> coverMetadata({
  EntityCoverRef ref = coverRef,
  int version = 1,
  Duration lease = const Duration(seconds: 50),
}) => {
  'entityType': ref.type,
  'entityId': ref.id,
  'version': version,
  'thumbnailPath': '${ref.path}/$version/content',
  'width': 320,
  'height': 180,
  'byteCount': coverJPEG().length,
  'sha256': sha256.convert(coverJPEG()).toString(),
  'expiresAt': DateTime.now().toUtc().add(lease).toIso8601String(),
  'attribution': '合成测试素材',
};
http.Response coverWire(Object? value, {int status = 200}) =>
    http.Response.bytes(
      utf8.encode(jsonEncode({'data': value})),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
EntityCoverRights coverRights() => EntityCoverRights(
  basis: 'OWNED',
  attribution: '合成测试素材',
  licenseReference: '',
  retentionUntil: DateTime.utc(2030, 1, 2),
  canRetain: true,
  canPublish: true,
);
Map<String, Object?> coverApproval(
  Map<String, dynamic> body, {
  EntityCoverRef ref = coverRef,
}) {
  final rights = body['rights'] as Map<String, dynamic>?;
  final r = rights == null
      ? null
      : EntityCoverRights(
          basis: rights['basis'],
          attribution: rights['attribution'],
          licenseReference: rights['licenseReference'],
          retentionUntil: DateTime.parse(rights['retentionUntil']),
          canRetain: rights['canRetain'],
          canPublish: rights['canPublish'],
        );
  return {
    'entityType': ref.type,
    'entityId': ref.id,
    'operationId': body['operationId'],
    'expectedVersion': body['expectedVersion'],
    'action': body['action'],
    'inputSha256': body['action'] == 'SAVE'
        ? sha256.convert(coverJPEG()).toString()
        : '',
    'rightsSha256': r?.digest ?? '',
    'expiresAt': DateTime.now()
        .toUtc()
        .add(const Duration(seconds: 25))
        .toIso8601String(),
    'token': 'ec1.1.2.${'a' * 64}',
    if (body['action'] == 'SAVE')
      'previewDerivative': {
        'contentType': 'image/jpeg',
        'width': 320,
        'height': 180,
        'byteCount': coverJPEG().length,
        'sha256': sha256.convert(coverJPEG()).toString(),
        'contentBase64': base64Encode(coverJPEG()),
      },
  };
}

Map<String, Object> coverReceipt(
  String op, {
  EntityCoverRef ref = coverRef,
  int expected = 0,
  String action = 'SAVE',
  String status = 'SUCCEEDED',
}) => {
  'entityType': ref.type,
  'entityId': ref.id,
  'operationId': op,
  'action': status == 'STOPPED' ? 'STOP' : action,
  'appliedVersion': status == 'STOPPED' ? 0 : expected + 1,
  'status': status,
};
Future<void> coverSettle(WidgetTester t) async {
  // Durable preparation, receipt, and current-state GET are separate stages.
  // Fake-time pumping cannot drain streamed MockClient work between them.
  for (var stage = 0; stage < 3; stage++) {
    await t.pump();
    await t.runAsync(() async {
      await Future<void>.delayed(const Duration(milliseconds: 80));
    });
  }
  await t.pumpAndSettle();
}

Future<void> decodeCover(WidgetTester t) async {
  await coverSettle(t);
  final f = find.byType(Image);
  expect(f, findsOneWidget);
  await t.runAsync(
    () => precacheImage(
      t.widget<Image>(f).image,
      t.element(f),
    ).timeout(const Duration(seconds: 2)),
  );
  await t.pump();
  final raw = t.widget<RawImage>(
    find.descendant(of: f, matching: find.byType(RawImage)),
  );
  expect(raw.image, isNotNull);
  expect(raw.image!.width, 320);
  expect(raw.image!.height, 180);
}

void main() {
  test(
    'cover strict metadata pairs type id version lease and same-origin path',
    () {
      final raw = coverMetadata();
      expect(EntityCoverMetadata.fromJson(raw, coverRef).ref, coverRef);
      for (final bad in [
        {'entityId': avatarPeer},
        {'entityType': 'person'},
        {'width': 128},
        {'version': entityCoverMaxVersion + 1},
        {'thumbnailPath': 'https://unapproved.test/photo.jpg'},
        {'thumbnailPath': '/v1/accounts/${coverRef.id}/avatar/1/content'},
        {
          'expiresAt': DateTime.now()
              .add(const Duration(minutes: 10))
              .toIso8601String(),
        },
        {'sourceProof': 'secret'},
      ]) {
        expect(
          () => EntityCoverMetadata.fromJson({...raw, ...bad}, coverRef),
          throwsFormatException,
        );
      }
      expect(
        EntityCoverState.fromJson({
          'version': 0,
          'cover': null,
        }, coverRef).cover,
        isNull,
      );
      expect(
        () => EntityCoverState.fromJson({'version': 2, 'cover': raw}, coverRef),
        throwsFormatException,
      );
    },
  );
  test(
    'cover preview checks actual derivative bytes rights and original receipt',
    () async {
      final rights = coverRights();
      Map<String, dynamic>? sent;
      final client = MockClient((req) async {
        sent = jsonDecode(req.body) as Map<String, dynamic>;
        return coverWire(coverApproval(sent!));
      });
      addTearDown(client.close);
      final api = EntityCoverAPI(
        client: client,
        baseUrl: coverBase,
        authorizationHeader: () => 'Bearer A',
      );
      final preview = await api.preview(
        coverRef,
        avatarOperation,
        0,
        'SAVE',
        avatarJPEG(),
        rights,
      );
      expect(preview.bytes, orderedEquals(coverJPEG()));
      expect(base64Decode(sent!['contentBase64']), orderedEquals(avatarJPEG()));
      final bad = coverApproval(sent!);
      (bad['previewDerivative'] as Map)['contentBase64'] = base64Encode(
        avatarJPEG(),
      );
      expect(
        () => EntityCoverPreview.fromJson(
          bad,
          coverRef,
          avatarOperation,
          0,
          'SAVE',
          rights,
        ),
        throwsFormatException,
      );
      expect(
        () => EntityCoverReceipt.fromJson(
          coverReceipt(avatarOperation, expected: 2),
          coverRef,
          avatarOperation,
          0,
          'SAVE',
        ),
        throwsFormatException,
      );
      expect(
        EntityCoverReceipt.fromJson(
          coverReceipt(avatarOperation, status: 'STOPPED'),
          coverRef,
          avatarOperation,
          0,
          'SAVE',
        ).appliedVersion,
        0,
      );
    },
  );
  testWidgets(
    'cover actual decoded versions delete TTL and actor ABA retire pixels',
    (t) async {
      final changes = ValueNotifier('Bearer A');
      addTearDown(changes.dispose);
      final paths = <String>[];
      final client = MockClient((r) async {
        paths.add(r.url.path);
        return http.Response.bytes(coverJPEG(), 200);
      });
      addTearDown(client.close);
      EntityCoverMetadata? m = EntityCoverMetadata.fromJson(
        coverMetadata(),
        coverRef,
      );
      Widget app() => MaterialApp(
        home: Scaffold(
          body: EntityCover(
            key: const ValueKey('cover'),
            metadata: m,
            api: EntityCoverAPI(
              client: client,
              baseUrl: coverBase,
              authorizationHeader: () => changes.value,
            ),
            identityChanges: changes,
            current: () => true,
            width: 320,
            height: 180,
          ),
        ),
      );
      await t.pumpWidget(app());
      await decodeCover(t);
      m = EntityCoverMetadata.fromJson(coverMetadata(version: 2), coverRef);
      await t.pumpWidget(app());
      await decodeCover(t);
      expect(paths, [
        '${coverRef.path}/1/content',
        '${coverRef.path}/2/content',
      ]);
      m = null;
      await t.pumpWidget(app());
      await t.pump();
      expect(find.byType(Image), findsNothing);
      m = EntityCoverMetadata.fromJson(
        coverMetadata(version: 3, lease: const Duration(seconds: 1)),
        coverRef,
      );
      await t.pumpWidget(app());
      await decodeCover(t);
      await t.pump(const Duration(seconds: 2));
      expect(find.byType(Image), findsNothing);
      m = EntityCoverMetadata.fromJson(coverMetadata(version: 4), coverRef);
      await t.pumpWidget(app());
      await decodeCover(t);
      changes.value = 'Bearer B';
      changes.value = 'Bearer A';
      await t.pumpWidget(app());
      await t.pump();
      expect(find.byType(Image), findsNothing);
      expect(paths.where((p) => p.endsWith('/4/content')), hasLength(1));
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'cover late original bytes cannot restore after current-source ABA',
    (t) async {
      final pending = Completer<http.Response>(), changes = ValueNotifier(true);
      addTearDown(changes.dispose);
      final client = MockClient((r) => pending.future);
      addTearDown(client.close);
      await t.pumpWidget(
        MaterialApp(
          home: EntityCover(
            metadata: EntityCoverMetadata.fromJson(coverMetadata(), coverRef),
            api: EntityCoverAPI(
              client: client,
              baseUrl: coverBase,
              authorizationHeader: () => null,
            ),
            identityChanges: changes,
            current: () => changes.value,
          ),
        ),
      );
      await t.pump();
      changes.value = false;
      changes.value = true;
      pending.complete(http.Response.bytes(coverJPEG(), 200));
      await coverSettle(t);
      expect(find.byType(Image), findsNothing);
      await t.pumpWidget(const SizedBox());
    },
  );
}
