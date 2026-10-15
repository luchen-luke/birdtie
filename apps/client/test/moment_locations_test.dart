import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/moment_locations.dart';
import 'package:birdtie_client/src/workspace/moment_locations_api.dart';
import 'package:birdtie_client/src/workspace/moment_thread_api.dart';
import 'package:birdtie_client/src/workspace/moment_thread_controller.dart';
import 'package:birdtie_client/src/workspace/moment_thread_pending_store.dart';
import 'package:birdtie_client/src/workspace/now_creation_draft.dart';
import 'package:birdtie_client/src/workspace/now_creation_panel.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'moment_control_test.dart'
    show mcOp, mcOwner, mcTarget, mcInput, mcPreview;
import 'map_layers_api_test.dart' show mapTestBounds, mapTestItem, mapTestView;

const relationB = '44444444-4444-4444-8444-444444444444';
const placeB = '55555555-5555-4555-8555-555555555555';
final source = List.filled(64, 'a').join();
const first = MomentLocationInput(
  id: mcOp,
  kind: 'PLACE',
  placeID: mcOwner,
  precision: 'place',
);
const second = MomentLocationInput(
  id: relationB,
  kind: 'PLACE',
  placeID: placeB,
  precision: 'city',
);
Map<String, Object?> body({List<MomentLocationInput>? locations}) => {
  'cityId': '',
  'placeId': '',
  'title': '',
  'body': '正文保持一条动态',
  'timePrecision': 'unknown',
  'locationPrecision': 'none',
  if (locations != null) 'locations': locations.map((v) => v.toJson()).toList(),
};
Map<String, Object?> resolved(
  MomentLocationInput input,
  int position,
  DateTime at, {
  bool own = true,
}) => {
  'id': input.id,
  own ? 'position' : 'displayOrder': position,
  'kind': !own && input.precision == 'city' ? 'CITY' : input.kind,
  'precision': input.precision,
  'cityId': !own && input.precision == 'city' ? 'synthetic-second-city' : '',
  'cityName': position == 0 ? '合成甲城' : '合成乙城',
  'placeId': !own && input.precision == 'city' ? '' : input.placeID,
  'placeName': !own && input.precision == 'city'
      ? ''
      : position == 0
      ? '合成甲地点'
      : '合成乙地点',
  'region': '合成地区',
  'countryCode': 'GB',
  'sourceVersion': source,
  if (own) ...{
    'available': true,
    'observedAt': at.toIso8601String(),
    'validUntil': at.add(const Duration(seconds: 30)).toIso8601String(),
  },
};
Map<String, Object?> view(DateTime at, {bool own = false}) => {
  'schemaVersion': 'moment-locations-v1',
  'momentId': mcTarget,
  'momentRevision': 2,
  'items': [
    resolved(first, 0, at, own: own),
    resolved(second, 1, at, own: own),
  ],
  'sourceVersion': source,
  'observedAt': at.toIso8601String(),
  'validUntil': at.add(const Duration(seconds: 30)).toIso8601String(),
};
http.Response reply(Object value) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': value})),
  200,
  headers: {'content-type': 'application/json'},
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'locations preserve old hashes and seal ordered selectors without restoring names or approval',
    () async {
      final legacy = MomentControlInput(
        operationID: mcOp,
        action: 'CREATE',
        expectedRevision: 0,
        input: mcInput(),
      );
      expect(
        legacy.digest,
        '653411493f706ea41ea76b54362a954d601dcec6dbe4cd86a2f088660dded516',
      );
      MomentControlInput make(Map<String, Object?> input) => MomentControlInput(
        operationID: mcOp,
        action: 'UPDATE_PUBLIC',
        momentID: mcTarget,
        expectedRevision: 2,
        input: input,
      );
      final omitted = make(body()), cleared = make(body(locations: const []));
      expect(omitted.digest, isNot(cleared.digest));
      expect(() => make({...body(), 'locations': null}), throwsFormatException);
      final mutable = body(locations: [first, second]),
          input = make(mutable),
          digest = input.digest;
      (mutable['locations'] as List).clear();
      expect(input.digest, digest);
      expect(make(body(locations: [second, first])).digest, isNot(digest));
      expect(
        () => make(body(locations: [first, first])),
        throwsFormatException,
      );
      final now = DateTime.now().toUtc(), preview = mcPreview(input);
      preview['locations'] = [
        resolved(first, 0, now),
        resolved(second, 1, now),
      ];
      preview['locationsSourceVersion'] = source;
      final approved = MomentControlPreview.fromJson(preview, input);
      expect(approved.locations.map((v) => v.input.toJson()).toList(), [
        first.toJson(),
        second.toJson(),
      ]);
      expect(approved.locations.map((v) => v.label).join(), contains('合成乙地点'));
      final retained = mcPreview(input)..['locationsSourceVersion'] = source;
      final unavailable = resolved(first, 0, now)
        ..addAll({
          'available': false,
          'cityName': '',
          'placeName': '',
          'region': '',
          'countryCode': '',
        });
      retained['locations'] = [unavailable, resolved(second, 1, now)];
      retained['preservedPlaceUnavailable'] = true;
      final kept = MomentControlPreview.fromJson(retained, input);
      expect(kept.preservedPlaceUnavailable, isTrue);
      expect(kept.locations.first.label, '位置暂不可用');
      expect(kept.locations.first.input.toJson(), first.toJson());
      final draft = NowCreationDraft(
        kind: 'moment',
        cityID: '',
        cityName: '',
        body: '原正文',
        locations: [first, second],
      );
      final restored = NowCreationDraft.fromJson(draft.toJson());
      expect(restored.locations!.map((v) => v.id), [mcOp, relationB]);
      expect(jsonEncode(draft.toJson()), isNot(contains('合成甲地点')));
      expect(jsonEncode(draft.toJson()), isNot(contains('approval')));
      final thread = MomentThreadInput(
        operationID: mcOp,
        action: 'CREATE_PUBLISH',
        localScope: relationB,
        entries: [
          MomentThreadDraftEntry(
            localID: mcOwner,
            cityID: '',
            text: '第一条',
            locations: restored.locations,
          ),
          MomentThreadDraftEntry(
            localID: mcTarget,
            cityID: '',
            text: '第二条',
            locations: const [],
          ),
        ],
      );
      final items = thread.toJson()['items'] as List;
      expect(items.first['input']['locations'], [
        first.toJson(),
        second.toJson(),
      ]);
      expect(items.last['input']['locations'], isEmpty);
      expect(
        MomentThreadDraftEntry.decode(thread.entries.first.json).input,
        thread.entries.first.input,
      );
      final saved = <String, String>{};
      final controller = MomentThreadController(
        ownerID: mcOwner,
        api: MomentThreadAPI(
          baseUrl: 'https://locations.test',
          authorizationHeader: () => 'Bearer synthetic',
          current: () => true,
          client: MockClient((_) async => throw StateError('no HTTP expected')),
        ),
        journal: MomentThreadJournal(
          readValue: (key) async => saved[key],
          writeValue: (key, value) async {
            saved[key] = value;
          },
        ),
      );
      await controller.begin(cityID: 'legacy-city', initialText: '原有正文');
      controller.addActiveLocation(first);
      expect(controller.entries.first.locations!.map((v) => v.kind), [
        'CITY',
        'PLACE',
      ]);
      expect(controller.entries.first.locations!.first.cityID, 'legacy-city');
      final originalID = controller.entries.first.locations!.first.id;
      controller.setActiveLocations(controller.entries.first.locations!);
      expect(controller.entries.first.locations!.first.id, originalID);
      await Future<void>.delayed(Duration.zero);
      controller.dispose();
    },
  );

  testWidgets(
    'same composer panel changes selected precision and public sidecar retires leased names on expiry and identity change',
    (tester) async {
      var locations = <MomentLocationInput>[first, second];
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: StatefulBuilder(
              builder: (context, setState) => NowCreationPanel(
                draft: NowCreationDraft(
                  kind: 'moment',
                  cityID: '',
                  cityName: '',
                  body: '本人正文',
                  locations: locations,
                ),
                onToggle: () {},
                onClose: () {},
                onEditTitle: () {},
                onEditBody: () {},
                onChoosePlace: () {},
                onChooseTime: () {},
                onSave: () {},
                locationLabels: const {mcOp: '合成甲地点', relationB: '合成乙地点'},
                onRemoveLocation: (id) => setState(
                  () => locations = locations.where((v) => v.id != id).toList(),
                ),
                onSetLocationPrecision: (id, precision) => setState(
                  () => locations = locations
                      .map((v) => v.id == id ? v.withPrecision(precision) : v)
                      .toList(),
                ),
              ),
            ),
          ),
        ),
      );
      expect(find.byType(TextField), findsNothing);
      await tester.tap(find.text('合成甲地点 · 公开地点'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('仅公开城市').last);
      await tester.pumpAndSettle();
      expect(locations.first.id, mcOp);
      expect(locations.first.precision, 'city');
      await tester.tap(find.text('合成乙地点 · 仅公开城市'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('移除这处位置'));
      await tester.pumpAndSettle();
      expect(locations.map((v) => v.id), [mcOp]);

      var current = true, token = 'Bearer synthetic-a', reads = 0;
      final identity = ValueNotifier(0);
      final client = MockClient((request) async {
        reads++;
        return reply(
          view(DateTime.now().toUtc(), own: request.url.path.contains('/me/')),
        );
      });
      final api = MomentLocationsAPI(
        authorizationHeader: () => token,
        current: () => current,
        apiBaseUrl: 'https://locations.test',
        client: client,
      );
      final owner = await api.read(mcTarget, own: true, expectedRevision: 2);
      expect(owner.inputs.map((v) => v.toJson()).toList(), [
        first.toJson(),
        second.toJson(),
      ]);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MomentLocationsSidecar(
              momentID: mcTarget,
              expectedRevision: 2,
              authorizationHeader: () => token,
              sourceCurrent: () => current,
              identityChanges: identity,
              client: client,
              apiBaseUrl: 'https://locations.test',
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('合成甲地点'), findsOneWidget);
      expect(find.textContaining('合成乙地点'), findsNothing);
      expect(find.textContaining('合成乙城'), findsOneWidget);
      await tester.pump(const Duration(seconds: 31));
      expect(find.textContaining('合成甲地点'), findsNothing);
      await tester.tap(find.textContaining('重新读取'));
      await tester.pumpAndSettle();
      expect(find.textContaining('合成甲地点'), findsOneWidget);
      current = false;
      token = 'Bearer synthetic-b';
      identity.value++;
      await tester.pump();
      expect(find.textContaining('合成甲地点'), findsNothing);
      expect(reads, 3);
      await tester.pumpWidget(const SizedBox.shrink());
      api.dispose();
      identity.dispose();
      client.close();
    },
  );

  test(
    'map v5 preserves two stable pins and one original Moment detail identity',
    () async {
      final requests = <http.Request>[];
      final one = mapTestItem('MOMENT')..['pinIdentity'] = mcOp,
          two = mapTestItem('MOMENT')..['pinIdentity'] = relationB;
      two['anchor'] = {
        ...(two['anchor'] as Map<String, dynamic>),
        'longitude': -2.05,
      };
      final raw = mapTestView()
        ..['schemaVersion'] = 'typed-map-layers-v5'
        ..['items'] = [one, two];
      final api = MapLayersApi(
        apiBaseUrl: 'https://locations.test',
        client: MockClient((request) async {
          requests.add(request);
          return reply(raw);
        }),
      );
      final parsed = await api.read('owned-city', mapTestBounds);
      expect(
        requests.single.headers['X-Birdtie-Map-Locations'],
        'locations-v2',
      );
      expect(parsed.items.map((v) => v.mapID).toSet().length, 2);
      expect(parsed.items.map((v) => v.detailID).toSet(), {one['id']});
      expect(parsed.items.map((v) => v.entity.id).toSet().length, 2);
      expect(
        () => TypedMapView.decode(
          {
            ...raw,
            'items': [one, one],
          },
          'owned-city',
          mapTestBounds,
          false,
        ),
        throwsFormatException,
      );
      expect(
        () => TypedMapView.decode(
          {
            ...raw,
            'items': [
              {...one, 'pinIdentity': ''},
            ],
          },
          'owned-city',
          mapTestBounds,
          false,
        ),
        throwsFormatException,
      );
      api.dispose();
    },
  );
}
