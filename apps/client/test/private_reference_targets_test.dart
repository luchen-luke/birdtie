import 'dart:convert';

import 'package:birdtie_client/src/content/legacy_archive_page.dart';
import 'package:birdtie_client/src/content/private_reference_media_view.dart';
import 'package:birdtie_client/src/content/private_reference_targets_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'saved_page_test.dart' show exerciseSavedPrivateReferenceExpiry;

Future<void> _finishRequests(WidgetTester tester) async {
  // Streamed mock responses need real IO turns. Do not advance the lease
  // repeatedly while waiting for the body stream's asynchronous completion.
  for (var phase = 0; phase < 4; phase++) {
    await tester.pump();
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 10)),
    );
  }
  await tester.pump(const Duration(milliseconds: 400));
  await tester.pump();
}

void main() {
  testWidgets('旧地点原图入口本人分页独立读取与返回刷新，换身份清空且收藏过期可重读', (tester) async {
    FlutterSecureStorage.setMockInitialValues({});
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const first = '76800000-0000-4000-8000-000000000001';
    var owner = '76800000-0000-4000-8000-000000000090';
    var token = 'Bearer original-local';
    final changes = ValueNotifier<int>(0);
    var firstReads = 0, nextReads = 0, imageReads = 0, contentReads = 0;
    final start = DateTime.now().toUtc();
    final until = start.add(const Duration(seconds: 30));
    String encoded(List<int> bytes) =>
        base64Url.encode(bytes).replaceAll('=', '');
    final cursorPayload = encoded(
      utf8.encode(
        jsonEncode({
          'Version': 1,
          'Owner': owner,
          'Reader': 'a' * 64,
          'Query': 'b' * 64,
          'Source': 'a' * 64,
          'Before': '76800000-0000-4000-8000-000000000020',
          'Until': until.toIso8601String(),
        }),
      ),
    );
    final cursorSignature = encoded(List.generate(32, (index) => index * 7));
    // Same closed base64url payload + dot + base64url HMAC shape as Go's
    // encodeJourneyHistoryCursor. The opaque signature is verified by Go.
    final nextCursor = '$cursorPayload.$cursorSignature';
    Map<String, Object?> target(int index) => {
      'targetId':
          '76800000-0000-4000-8000-${(index + 1).toString().padLeft(12, '0')}',
      'name': '原贡献地点 $index',
      'imageCount': 1,
    };
    Map<String, Object?> page(List<Object?> rows, String? next) => {
      'schemaVersion': 'owned-reference-image-targets-v1',
      'targetType': 'PLACE',
      'items': rows,
      'observedAt': DateTime.now().toUtc().toIso8601String(),
      'validUntil': until.toIso8601String(),
      'sourceVersion': 'a' * 64,
      'nextCursor': next,
    };
    http.Response json(Object data) => http.Response.bytes(
      utf8.encode(jsonEncode({'data': data})),
      200,
      headers: {'content-type': 'application/json'},
    );
    final client = MockClient((request) async {
      expect(request.method, 'GET');
      expect(request.headers['Authorization'], 'Bearer original-local');
      if (request.url.path == '/v1/me/migration-archive')
        return json({'items': []});
      if (request.url.path == '/v1/me/reference-image-targets') {
        expect(request.url.queryParameters['targetType'], 'PLACE');
        if (request.url.queryParameters.containsKey('cursor')) {
          nextReads++;
          expect(request.url.queryParameters, {
            'targetType': 'PLACE',
            'cursor': nextCursor,
          });
          return json(page([target(20)], null));
        }
        firstReads++;
        return json(
          page(
            firstReads == 1
                ? List.generate(20, target)
                : [
                    {...target(0), 'name': '返回后重新读取的地点'},
                  ],
            firstReads == 1 ? nextCursor : null,
          ),
        );
      }
      if (request.url.path == '/v1/me/reference-images') {
        imageReads++;
        expect(request.url.queryParameters, {
          'targetType': 'PLACE',
          'targetId': first,
        });
        final now = DateTime.now().toUtc();
        return json({
          'schemaVersion': 'owned-reference-images-v1',
          'targetType': 'PLACE',
          'targetId': first,
          'items': [],
          'observedAt': now.toIso8601String(),
          'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
          'sourceVersion': 'b' * 64,
          'nextCursor': null,
        });
      }
      contentReads++;
      return http.Response('', 404);
    });
    final valid = page([target(0)], null);
    final continued = page(List.generate(20, target), nextCursor);
    expect(
      PrivateReferenceTargetsPageData.decode(continued).nextCursor,
      nextCursor,
    );
    for (final malformed in [
      'targets_next',
      '.$cursorSignature',
      '$cursorPayload.',
      '$cursorPayload..$cursorSignature',
      '$cursorPayload.$cursorSignature.extra',
      '$cursorPayload.$cursorSignature=',
      '$cursorPayload/.$cursorSignature',
      'A.$cursorSignature',
      '$cursorPayload.${'a' * 4096}',
    ]) {
      expect(
        () => PrivateReferenceTargetsPageData.decode({
          ...continued,
          'nextCursor': malformed,
        }),
        throwsFormatException,
      );
    }
    expect(
      PrivateReferenceTargetsPageData.decode(valid).items.single.id,
      first,
    );
    expect(
      () => PrivateReferenceTargetsPageData.decode({
        ...valid,
        'targetType': 'PUBLIC',
      }),
      throwsFormatException,
    );
    expect(
      () => PrivateReferenceTargetsPageData.decode({
        ...valid,
        'validUntil': start
            .subtract(const Duration(seconds: 1))
            .toIso8601String(),
      }),
      throwsFormatException,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: LegacyArchivePage(
          authorizationHeader: () => token,
          ownerID: () => owner,
          identityChanges: changes,
          client: client,
          apiBaseUrl: 'https://reference.test',
        ),
      ),
    );
    await _finishRequests(tester);
    await tester.tap(find.text('我的旧地点图片'));
    await _finishRequests(tester);
    expect(firstReads, 1);
    expect(find.text('原贡献地点 0'), findsOneWidget);
    expect(imageReads, 0);
    final scrollable = find
        .descendant(
          of: find.byType(ListView).last,
          matching: find.byType(Scrollable),
        )
        .first;
    await tester.scrollUntilVisible(
      find.text('继续读取地点图片'),
      500,
      scrollable: scrollable,
    );
    await tester.tap(find.text('继续读取地点图片'));
    await _finishRequests(tester);
    expect(nextReads, 1);
    await tester.scrollUntilVisible(
      find.text('原贡献地点 0'),
      -500,
      scrollable: scrollable,
    );
    await tester.tap(find.text('原贡献地点 0'));
    await _finishRequests(tester);
    expect(find.byType(PrivateReferenceMediaView), findsOneWidget);
    expect(imageReads, 1);
    expect(contentReads, 0);
    final detail = find
        .ancestor(
          of: find.byType(PrivateReferenceMediaView),
          matching: find.byType(Scaffold),
        )
        .first;
    await tester.tap(
      find.descendant(of: detail, matching: find.byType(BackButton)),
    );
    await _finishRequests(tester);
    expect(firstReads, 2);
    expect(find.text('返回后重新读取的地点'), findsOneWidget);
    owner = '76800000-0000-4000-8000-000000000091';
    token = 'Bearer another-local';
    changes.value++;
    await _finishRequests(tester);
    expect(find.text('返回后重新读取的地点'), findsNothing);
    expect(find.text('当前个人身份已变化，请返回后重新打开。'), findsOneWidget);
    expect(firstReads, 2);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    changes.dispose();
    client.close();

    await exerciseSavedPrivateReferenceExpiry(tester);
    expect(tester.takeException(), isNull);
  });
}
