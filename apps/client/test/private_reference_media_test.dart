import 'dart:convert';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/content/private_reference_media_view.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _owner = '75000000-0000-4000-8000-000000000001';
const _other = '75000000-0000-4000-8000-000000000002';
const _target = '75000000-0000-4000-8000-000000000003';
const _image = '75000000-0000-4000-8000-000000000004';
const _operation = '75000000-0000-4000-8000-000000000005';
const _origin = 'https://birdtie.invalid';

Map<String, dynamic> _item(Uint8List bytes, {String review = 'rejected'}) => {
  'id': _image,
  'kind': 'PLACE_IMAGE',
  'revision': 3,
  'position': 7,
  'mimeType': 'image/png',
  'width': 2,
  'height': 2,
  'byteCount': bytes.length,
  'sha256': sha256.convert(bytes).toString(),
  'contentPath':
      '/v1/me/reference-images/$_image/content?revision=3&sourceVersion=${'b' * 64}',
  'sourceVersion': 'b' * 64,
  'sourceType': 'owner_upload',
  'sourceReviewStatus': review,
  'sourceIsCover': true,
  'sourceRightsBasis': 'owner',
  'sourceAttribution': '',
  'sourcePublishable': false,
  'canPublish': false,
  'canCopy': false,
};

Map<String, dynamic> _page(Uint8List bytes, {bool empty = false}) {
  final now = DateTime.now().toUtc();
  return {
    'schemaVersion': 'owned-reference-images-v1',
    'targetType': 'PLACE',
    'targetId': _target,
    'items': empty ? [] : [_item(bytes)],
    'observedAt': now.toIso8601String(),
    'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
    'sourceVersion': 'a' * 64,
    'nextCursor': null,
  };
}

http.Response _response(Object value) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': value})),
  200,
  headers: {'content-type': 'application/json'},
);

Future<Uint8List> _pixels() async {
  final recorder = ui.PictureRecorder();
  Canvas(recorder).drawColor(Colors.teal, BlendMode.src);
  final picture = recorder.endRecording();
  final image = await picture.toImage(2, 2);
  final data = await image.toByteData(format: ui.ImageByteFormat.png);
  image.dispose();
  picture.dispose();
  return data!.buffer.asUint8List();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  test(
    'original roles retain review facts and delete selectors are owner/origin isolated',
    () async {
      final bytes = Uint8List.fromList([1, 2, 3]);
      final item = PrivateReferenceImage.decode(_item(bytes), 'PLACE');
      expect(item.sourceReviewStatus, 'rejected');
      expect(item.readable, isTrue);
      expect(item.sourcePublishable, isFalse);
      expect(
        PrivateReferenceImage.decode(
          _item(bytes, review: 'removed'),
          'PLACE',
        ).readable,
        isFalse,
      );
      expect(
        () => PrivateReferenceImage.decode({
          ..._item(bytes),
          'canPublish': true,
        }, 'PLACE'),
        throwsFormatException,
      );
      expect(
        () => PrivateReferenceImage.decode({
          ..._item(bytes),
          'contentPath': 'https://foreign.invalid/image',
        }, 'PLACE'),
        throwsFormatException,
      );
      expect(
        () => PrivateReferenceImagePage.decode(
          {..._page(bytes), 'targetId': _other},
          'PLACE',
          _target,
        ),
        throwsFormatException,
      );
      final store = SecurePrivateReferenceImagePendingStore();
      final pending = PrivateReferenceImagePending(
        operationID: _operation,
        imageID: _image,
        expectedRevision: 3,
        sourceVersion: 'b' * 64,
      );
      await store.write(_origin, _owner, 'PLACE', _target, pending);
      expect(
        (await store.read(_origin, _owner, 'PLACE', _target))!.sourceVersion,
        'b' * 64,
      );
      expect(await store.read(_origin, _other, 'PLACE', _target), isNull);
      expect(
        await store.read('https://other.invalid', _owner, 'PLACE', _target),
        isNull,
      );
      expect(await store.read(_origin, _owner, 'JOURNEY', _target), isNull);
      await expectLater(
        store.write(
          _origin,
          _owner,
          'PLACE',
          _target,
          PrivateReferenceImagePending(
            operationID: _operation,
            imageID: _image,
            expectedRevision: 3,
            sourceVersion: 'c' * 64,
          ),
        ),
        throwsFormatException,
      );
      final stored = await const FlutterSecureStorage().readAll();
      expect(jsonDecode(stored.values.single), pending.toJson());
      expect(stored.values.single, isNot(contains('Bearer')));
      await store.remove(_origin, _owner, 'PLACE', _target, _operation);
      expect(await store.read(_origin, _owner, 'PLACE', _target), isNull);
    },
  );

  testWidgets(
    'Session image RAM and UNKNOWN delete resume only the exact original operation',
    (tester) async {
      final bytes = await tester.runAsync(_pixels);
      final changes = ValueNotifier<int>(0);
      var token = 'Bearer current-session';
      var deleteCount = 0, receiptCount = 0, contentCount = 0;
      var committed = false;
      String? operation;
      final client = MockClient((request) async {
        expect(request.headers['Authorization'], token);
        expect(request.url.origin, _origin);
        if (request.url.path == '/v1/me/reference-images' &&
            request.method == 'GET') {
          expect(request.url.queryParameters, {
            'targetType': 'PLACE',
            'targetId': _target,
          });
          return _response(_page(bytes!, empty: committed));
        }
        if (request.url.path.endsWith('/content')) {
          contentCount++;
          expect(request.url.queryParameters['revision'], '3');
          expect(request.url.queryParameters['sourceVersion'], 'b' * 64);
          return http.Response.bytes(
            bytes!,
            200,
            headers: {'content-type': 'image/png', 'cache-control': 'no-store'},
          );
        }
        if (request.method == 'DELETE') {
          deleteCount++;
          expect(request.url.path, '/v1/me/reference-images/$_image');
          final body = jsonDecode(request.body) as Map;
          operation = body['operationId'];
          expect(body, {
            'operationId': operation,
            'expectedRevision': 3,
            'expectedSourceVersion': 'b' * 64,
          });
          throw http.ClientException('synthetic lost response');
        }
        if (request.url.path ==
            '/v1/me/reference-image-operations/$operation') {
          receiptCount++;
          if (!committed) return http.Response('', 404);
          return _response({
            'schemaVersion': 'owned-reference-image-operation-v1',
            'operationId': operation,
            'imageId': _image,
            'revision': 4,
            'status': 'COMMITTED',
            'effect': 'DELETED',
            'committedAt': DateTime.now().toUtc().toIso8601String(),
          });
        }
        throw StateError('Unexpected reference request');
      });
      Widget view() => MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: PrivateReferenceMediaView(
              targetType: 'PLACE',
              targetID: _target,
              authorizationHeader: () => token,
              ownerID: () => _owner,
              sourceCurrent: () => true,
              apiBaseUrl: _origin,
              client: client,
              identityChanges: changes,
            ),
          ),
        ),
      );
      Future<void> finishImageIO() async {
        // Metadata, content and engine decode are distinct asynchronous phases.
        // Yield to their real IO without advancing the 30-second fake lease.
        for (var phase = 0; phase < 4; phase++) {
          await tester.pump();
          await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)),
          );
        }
        await tester.pump();
      }

      await tester.pumpWidget(view());
      await finishImageIO();
      expect(
        find.byKey(const ValueKey('private-reference-image-$_image')),
        findsOneWidget,
        reason:
            'content GETs=$contentCount; visible=${tester.widgetList<Text>(find.byType(Text)).map((w) => w.data).join(';')}',
      );
      expect(find.textContaining('审核未通过'), findsOneWidget);
      expect(contentCount, 1);
      await tester.tap(find.text('移除原图片引用'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认移除'));
      await finishImageIO();
      await tester.pumpAndSettle();
      expect(deleteCount, 1);
      expect(find.text('有一项图片移除结果待核实。'), findsOneWidget);
      final saved = await const FlutterSecureStorage().readAll();
      expect(saved.values.single, contains(operation));
      expect(saved.values.single, isNot(contains('current-session')));
      expect(saved.values.single, isNot(contains(base64Encode(bytes!))));
      await tester.pumpWidget(const SizedBox());
      await tester.pumpWidget(view());
      await finishImageIO();
      expect(deleteCount, 1);
      expect(find.text('有一项图片移除结果待核实。'), findsOneWidget);
      await tester.tap(find.text('核实原移除结果'));
      await finishImageIO();
      await tester.pumpAndSettle();
      expect(receiptCount, 1);
      expect(deleteCount, 1);
      expect(find.text('有一项图片移除结果待核实。'), findsOneWidget);
      await tester.pump(const Duration(seconds: 31));
      await tester.pump();
      expect(find.byType(Image), findsNothing);
      expect(find.text('有一项图片移除结果待核实。'), findsOneWidget);
      committed = true;
      await tester.tap(find.text('核实原移除结果'));
      await finishImageIO();
      await tester.pumpAndSettle();
      expect(receiptCount, 2);
      expect(deleteCount, 1);
      expect(find.text('有一项图片移除结果待核实。'), findsNothing);
      expect(await const FlutterSecureStorage().readAll(), isEmpty);
      token = 'Bearer replacement';
      changes.value++;
      await tester.pump();
      expect(find.byType(Image), findsNothing);
      await tester.pumpWidget(const SizedBox());
      client.close();
      changes.dispose();
    },
  );
}
