import 'dart:convert';

import 'package:birdtie_client/src/content/collection_control_pending_store.dart';
import 'package:birdtie_client/src/content/collection_copy_pending_store.dart';
import 'package:birdtie_client/src/content/personal_collection_controller.dart';
import 'package:birdtie_client/src/content/personal_collection_page.dart';
import 'package:birdtie_client/src/workspace/legacy_place_reference_page.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _owner = '73000000-0000-4000-8000-000000000001';
const _record = '73000000-0000-4000-8000-000000000002';
const _place = '73000000-0000-4000-8000-000000000003';
const _saved = '73000000-0000-4000-8000-000000000004';

Map<String, Object> _reference({
  String kind = 'theme_map',
  String record = _record,
  bool expired = false,
}) {
  final at = DateTime.now().toUtc().subtract(
    Duration(seconds: expired ? 31 : 0),
  );
  return {
    'kind': kind,
    'recordId': record,
    'placeId': _place,
    'observedAt': at.toIso8601String(),
    'validUntil': at.add(const Duration(seconds: 30)).toIso8601String(),
    'sourceVersion': 'a' * 64,
  };
}

Map<String, dynamic> _theme({bool expired = false}) => {
  'id': _record,
  'ownerAccountId': _owner,
  'kind': 'theme_map',
  'cityId': 'city-b',
  'title': '跨城旧主题',
  'summary': '',
  'revision': 1,
  'status': 'draft',
  'visibility': 'private',
  'entries': [
    {
      'placeId': _place,
      'placeName': '旧私人地名',
      'note': '本人旧备注',
      'locationVisibility': 'hidden',
      'available': false,
      'legacyReference': _reference(expired: expired),
    },
  ],
};

Map<String, dynamic> _savedPlace({bool expired = false}) => {
  'id': _saved,
  'kind': 'place',
  'targetId': _place,
  'title': '旧私人地名',
  'summary': '旧地点资料',
  'cityId': 'city-b',
  'available': false,
  'note': '本人收藏备注',
  'legacyReference': _reference(
    kind: 'saved_place',
    record: _saved,
    expired: expired,
  ),
};

http.Response _response(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json'},
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  test(
    'private legacy references bind exact saved or theme records without public/map authority',
    () {
      final theme = PersonalCollection.fromJson(_theme());
      final entry = theme.entries.single;
      expect(entry.readable, isTrue);
      expect(entry.available, isFalse);
      expect(entry.sourceCurrent, isFalse);
      expect(entry.latitude, isNull);
      expect(entry.longitude, isNull);
      expect(theme.hasPrivateLegacyReferences, isTrue);
      expect(entry.copyWith(note: '本人修改').toInput(), {
        'placeId': _place,
        'note': '本人修改',
        'locationVisibility': 'hidden',
      });
      expect(
        PersonalCollection.fromJson(
          _theme(expired: true),
        ).entries.single.readable,
        isFalse,
      );
      expect(
        () => PersonalCollection.fromJson({..._theme(), 'id': _saved}),
        throwsFormatException,
      );
      expect(
        () => PersonalCollection.fromJson({
          ..._theme(),
          'visibility': 'public',
          'status': 'published',
        }),
        throwsFormatException,
      );
      final invalidEntry = Map<String, dynamic>.from(
        (_theme()['entries'] as List).single as Map,
      );
      expect(
        () => PersonalCollectionEntry.fromJson({
          ...invalidEntry,
          'latitude': 1,
          'longitude': 2,
        }),
        throwsFormatException,
      );

      final saved = SavedItem.fromJson(_savedPlace());
      expect(saved.readable, isTrue);
      expect(saved.available, isFalse);
      expect(saved.mapCurrent, isFalse);
      expect(
        saved.withNote('本人新备注', 1).legacyReference,
        same(saved.legacyReference),
      );
      expect(SavedItem.fromJson(_savedPlace(expired: true)).readable, isFalse);
      expect(
        () => SavedItem.fromJson({..._savedPlace(), 'id': _record}),
        throwsFormatException,
      );
      expect(
        () => SavedItem.fromJson({..._savedPlace(), 'targetId': _owner}),
        throwsFormatException,
      );
      expect(
        () => SavedItem.fromJson({..._savedPlace(), 'available': true}),
        throwsFormatException,
      );
      expect(
        () => SavedItem.fromJson({
          ..._savedPlace(),
          'latitude': 1,
          'longitude': 2,
        }),
        throwsFormatException,
      );
    },
  );

  testWidgets(
    'owner list opens another city independently and reads leased private details',
    (tester) async {
      final methods = <String>[];
      var privateReads = 0, publicPlaceOpens = 0;
      final client = MockClient((request) async {
        methods.add(request.method);
        if (request.url.path.startsWith('/v1/me/place-references/')) {
          privateReads++;
          expect(request.headers['Authorization'], 'Bearer synthetic');
          expect(request.url.queryParameters, {
            'kind': 'theme_map',
            'recordId': _record,
          });
          return _response({
            'schemaVersion': 'owned-legacy-place-reference-v1',
            'reference': _reference(),
            'name': '旧地点完整资料',
            'categoryCode': 'museum',
            'summary': '仅本人详情说明',
            'addressLabel': '',
            'cityId': 'city-b',
            'cityName': '乙城',
          });
        }
        return _response(
          request.url.path.endsWith('/$_record')
              ? _theme()
              : {
                  'items': [_theme()],
                },
        );
      });
      final controller = PersonalCollectionController(
        kind: 'theme_map',
        cityID: 'city-a',
        ownerID: () => _owner,
        authorizationHeader: () => 'Bearer synthetic',
        client: client,
        apiBaseUrl: 'https://legacy-place.fixture',
        controlPendingStore: MemoryCollectionControlPendingStore(),
      );
      controller.setText(title: '原城未保存的草稿');
      await controller.load();
      expect(controller.items.single.cityID, 'city-b');
      expect(await controller.open(controller.items.single), isFalse);
      expect(controller.title, '原城未保存的草稿');
      await tester.pumpWidget(
        MaterialApp(
          home: PersonalCollectionPage(
            controller: controller,
            cityName: '甲城',
            cityNames: const {'city-b': '乙城'},
            copyPendingStore: MemoryCollectionCopyPendingStore(),
            onOpenPlace: (_) => publicPlaceOpens++,
            onViewMap: (_) => fail('private references cannot enter the map'),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('已保存主题地图'));
      await tester.pumpAndSettle();
      expect(find.text('本人的草稿与已公开内容'), findsOneWidget);
      await tester.runAsync(() async {
        await tester.tap(find.text('跨城旧主题'));
        await Future<void>.delayed(const Duration(milliseconds: 20));
      });
      await tester.pumpAndSettle();
      expect(find.text('替换未保存草稿？'), findsNothing);
      expect(find.text('乙城 · 仅自己可见'), findsOneWidget);
      final editor = tester.widget<PersonalCollectionPage>(
        find.byType(PersonalCollectionPage),
      );
      final editorRoute = ModalRoute.of(
        tester.element(find.byType(PersonalCollectionPage)),
      )!;
      expect(editor.controller.cityID, 'city-b');
      expect(editor.controller.publicationVersionCurrent, isFalse);
      expect(controller.title, '原城未保存的草稿');
      expect(controller.editing, isNull);
      expect(find.text('旧私人地名'), findsOneWidget);
      expect(find.text('本人旧备注'), findsOneWidget);
      await tester.runAsync(() async {
        await tester.tap(find.text('旧私人地名'));
        await Future<void>.delayed(const Duration(milliseconds: 20));
      });
      await tester.pumpAndSettle();
      expect(find.byType(LegacyPlaceReferencePage), findsOneWidget);
      expect(find.text('旧地点完整资料'), findsOneWidget);
      expect(find.text('仅本人详情说明'), findsOneWidget);
      expect(privateReads, 1);
      expect(publicPlaceOpens, 0);
      await tester.pump(const Duration(seconds: 31));
      expect(find.text('旧地点完整资料'), findsNothing);
      expect(find.text('仅本人详情说明'), findsNothing);
      expect(find.text('请刷新地点资料。'), findsOneWidget);
      await tester.pageBack();
      await tester.pumpAndSettle();
      await tester.pageBack();
      await tester.pumpAndSettle();
      // The route was opened in runAsync. Await its real-zone completion so
      // the page's registered release callback disposes the borrowed editor.
      await tester.runAsync(() async {
        await editorRoute.completed;
      });
      expect(editor.controller.current, isFalse);
      expect(controller.title, '原城未保存的草稿');
      expect(controller.dirty, isTrue);
      expect(methods.every((method) => method == 'GET'), isTrue);
      await tester.pumpWidget(const SizedBox());
      controller.dispose();
      client.close();
    },
  );
}
