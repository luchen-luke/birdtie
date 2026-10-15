import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/moment_context_choice.dart';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'moment_control_controller_test.dart';
import 'moment_control_test.dart';

Map<String, Object?> unplacedRecord() => {
  ...mcRecord(),
  'title': '',
  'cityId': '',
  'locationPrecision': 'none',
  'createdAt': '2021-02-03T04:05:06Z',
  'updatedAt': '2021-02-03T04:05:06Z',
};

class _Auth extends BirdtieAuthController {
  @override
  bool get signedIn => true;
  @override
  String get authorizationHeader => 'Bearer synthetic';
  @override
  String get accountID => mcOwner;
  @override
  String get displayName => '隔离合成本人';
}

class _City extends PublicCityController {
  static const source = PublicSource(
    label: '隔离合成城市',
    maintainer: '本机单元验证',
    freshness: 'unverified',
    updatedAt: null,
  );
  static const city = PublicCity(
    id: 'aberdeen-gb',
    name: '当前浏览城市',
    region: '合成地区',
    contentStatus: 'published',
    source: source,
    map: null,
  );
  @override
  PublicCity get selectedCity => city;
  @override
  List<PublicCity> get cities => const [city];
  @override
  List<PublicPlace> get places => const [
    PublicPlace(
      id: mcTarget,
      name: '当前浏览城市的地点',
      categoryCode: 'museum',
      summary: '',
      source: source,
      location: PublicPlaceLocation(
        coordinateSystem: 'wgs84',
        precision: 'none',
        latitude: null,
        longitude: null,
      ),
    ),
  ];
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'title and location are optional while false context claims are rejected',
    () {
      final input = {
        ...mcInput(),
        'title': '',
        'cityId': '',
        'locationPrecision': 'none',
      };
      final update = MomentControlInput(
        operationID: mcOp,
        action: 'UPDATE',
        momentID: mcTarget,
        expectedRevision: 1,
        input: input,
      );
      expect(update.valid, isTrue);
      expect(
        MomentControlPreview.fromJson(mcPreview(update), update).input.input,
        input,
      );
      expect(PrivateMoment.fromJson(unplacedRecord()).cityID, isEmpty);
      expect(
        MomentControlInput(
          operationID: mcOp,
          action: 'PUBLISH',
          momentID: mcTarget,
          expectedRevision: 1,
          input: input,
        ).valid,
        isTrue,
      );
      expect(
        () => MomentControlInput(
          operationID: mcOp,
          action: 'UPDATE',
          momentID: mcTarget,
          expectedRevision: 1,
          input: {...input, 'activityId': mcOwner},
        ),
        throwsFormatException,
      );
      expect(
        PrivateMoment.fromJson({
          ...unplacedRecord(),
          'visibility': 'public',
          'status': 'published',
        }).cityID,
        isEmpty,
      );
    },
  );

  test(
    'editing an imported private draft preserves the unknown city',
    () async {
      final fixture = MomentFixture()..record = unplacedRecord();
      addTearDown(fixture.dispose);
      await fixture.controller.refresh();
      final original = fixture.controller.moments.single;
      final preview = await fixture.controller.previewUpdate(
        moment: original,
        title: original.title,
        body: '本人修改旧动态',
        placeID: '',
      );
      expect(preview, isNotNull);
      expect(preview!.input.input!['cityId'], '');
      expect(preview.input.input!['placeId'], '');
      expect(preview.input.input!['locationPrecision'], 'none');
      expect(await fixture.controller.commit(preview), isTrue);
      final saved = fixture.controller.moments.single;
      expect(saved.body, '本人修改旧动态');
      expect(saved.cityID, isEmpty);
      expect(saved.visibility, 'private');
      expect(await fixture.controller.previewPublish(saved), isNotNull);
      expect(fixture.previews, 2);
      expect(fixture.puts, 1);
    },
  );

  testWidgets(
    'real imported editor keeps no title or location and saves no browse anchor',
    (tester) async {
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1;
      FlutterSecureStorage.setMockInitialValues({});
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final fixture = MomentFixture()..record = unplacedRecord();
      final auth = _Auth(), city = _City();
      final client = MockClient((request) async {
        if (request.url.path == '/v1/me/moments/history') {
          final now = DateTime.now().toUtc();
          return mcJSON({
            'schemaVersion': 'owner-moment-history-v1',
            'status': 'active',
            'items': [fixture.record],
            'observedAt': now.toIso8601String(),
            'snapshotAt': now.toIso8601String(),
            'validUntil': now
                .add(const Duration(seconds: 50))
                .toIso8601String(),
            'nextCursor': null,
            'refreshCursor': 'same-private-history',
          });
        }
        if (request.url.path == '/v1/me') {
          return mcJSON({'id': mcOwner, 'accountType': 'person'});
        }
        if (request.url.path == '/v1/me/intents') return mcJSON([]);
        if (request.url.path == '/v1/me/avatar') return mcJSON(null, 404);
        if (request.url.path == '/v1/accounts/$mcOwner/profile') {
          return mcJSON({
            'accountId': mcOwner,
            'displayName': '隔离合成本人',
            'bio': '',
            'visibility': 'private',
          });
        }
        if (request.url.path == '/v1/me/moments') {
          return mcJSON([fixture.record]);
        }
        if (request.url.path == '/v1/me/moment-controls/previews') {
          final body = jsonDecode(request.body) as Map<String, dynamic>;
          final input = MomentControlInput(
            operationID: body['operationId'] as String,
            action: body['action'] as String,
            momentID: body['momentId'] as String? ?? '',
            expectedRevision: body['expectedRevision'] as int,
            input: Map<String, Object?>.from(body['input'] as Map),
          );
          fixture.inputs[input.operationID] = input;
          fixture.previews++;
          return mcJSON(mcPreview(input));
        }
        if (request.method == 'PUT' &&
            request.url.path.startsWith('/v1/me/moment-controls/operations/')) {
          final operationID = request.url.pathSegments.last;
          final input = fixture.inputs[operationID]!;
          expect(
            jsonDecode(request.body)['approval'],
            'opaque-actual-synthetic-token',
          );
          fixture.puts++;
          fixture.record = {
            ...fixture.record,
            ...input.input!,
            'revision': input.expectedRevision + 1,
          };
          return mcJSON(mcReceipt(input));
        }
        fixture.unexpectedRequests.add('${request.method} ${request.url.path}');
        return mcJSON(null, 404);
      });
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
        ownerID: () => auth.accountID,
        identityChanges: auth,
        client: client,
        apiBaseUrl: 'https://isolated.invalid',
        pendingStore: fixture.store,
      );
      addTearDown(() {
        moments.dispose();
        fixture.dispose();
        client.close();
        city.dispose();
        auth.dispose();
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: LegacyProfilePage(
              auth: auth,
              city: city,
              moments: moments,
              client: client,
              apiBaseUrl: 'https://isolated.invalid',
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('城市未记录'), findsNothing);
      await tester.ensureVisible(find.text('编辑或撤回'));
      await tester.tap(find.text('编辑或撤回'));
      await tester.pumpAndSettle();
      expect(find.textContaining('仅自己可见 · 本页不会发布'), findsOneWidget);
      expect(find.widgetWithText(TextFormField, '标题'), findsNothing);
      expect(find.byType(MomentContextChoice), findsNothing);
      expect(find.text('当前浏览城市的地点'), findsNothing);
      final body = find.widgetWithText(TextFormField, '记录');
      await tester.ensureVisible(body);
      await tester.enterText(body, '本人在真实编辑组件修改');
      await tester.ensureVisible(find.text('保存私人草稿'));
      // The causal HTTP reader drains a bounded response stream using real
      // async events. Deliver that response before settling UI animations.
      await tester.runAsync(() async {
        await tester.tap(find.text('保存私人草稿'));
        await Future<void>.delayed(const Duration(milliseconds: 20));
      });
      await tester.pumpAndSettle();
      expect(
        fixture.previews,
        1,
        reason:
            'private preview: ${moments.operationError}; ready=${moments.canStartOperation}',
      );
      expect(moments.operationError, isNull);
      expect(
        find.byType(AlertDialog),
        findsOneWidget,
        reason:
            'preview=${moments.operationPreview != null}, live=${moments.operationPreview?.live}',
      );
      expect(find.text('保存为本人私人草稿，不会公开发布，也不会上传私人图片。'), findsOneWidget);
      await tester.ensureVisible(find.text('确认保存'));
      await tester.runAsync(() async {
        await tester.tap(find.text('确认保存'));
        await Future<void>.delayed(const Duration(milliseconds: 20));
      });
      await tester.pumpAndSettle();
      expect(fixture.puts, 1);
      final sent = fixture.inputs.values.single;
      expect(sent.input!['cityId'], '');
      expect(sent.input!['locationPrecision'], 'none');
      expect(sent.input!['body'], '本人在真实编辑组件修改');
      expect(fixture.record['cityId'], '');
      expect(fixture.record['visibility'], 'private');
      expect(fixture.unexpectedRequests, isEmpty);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    },
  );
}
