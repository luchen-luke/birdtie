import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:birdtie_client/src/content/collection_publication_page.dart';
import 'package:birdtie_client/src/content/collection_publication_controller.dart';
import 'package:birdtie_client/src/content/now_journey_controller.dart';
import 'package:birdtie_client/src/content/now_journey_panel.dart';
import 'package:birdtie_client/src/content/personal_collection_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'personal_collection_controller_test.dart'
    show
        collectionOwner,
        collectionID,
        collectionReceipt,
        collectionResponse,
        collectionController;
import 'collection_publication_controller_test.dart' show publication;
import 'journey_detail_test.dart' show fixture;

Map<String, dynamic> publicJourney() => {
  'id': collectionID,
  'kind': 'journey',
  'cityId': '',
  'title': '公开站点',
  'summary': '',
  'revision': 2,
  'sourceExpiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(seconds: 50))
      .toIso8601String(),
  'entries': [],
  'journeySnapshot': {
    'schemaVersion': 'journey-publication-v1',
    'publicationRevision': 2,
    'sourceRevision': 1,
    'publishedAt': DateTime.now()
        .toUtc()
        .subtract(const Duration(minutes: 1))
        .toIso8601String(),
    'stops': [
      {
        'entryId': '15800000-0000-4000-8000-000000000001',
        'position': 1,
        'available': true,
        'locationVisibility': 'hidden',
      },
      {
        'entryId': '15800000-0000-4000-8000-000000000003',
        'position': 3,
        'available': false,
      },
    ],
  },
};
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
    'private place reorder retires the old field callback and uses no second input',
    (t) async {
      final wire = collectionReceipt();
      wire['entries'] = [...(wire['entries'] as List), <String, dynamic>{
        ...((wire['entries'] as List).first as Map),
        'placeId': '33333333-3333-4333-8333-333333333334',
        'placeName': '第二个地点',
      }];
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/collections'))
          return collectionResponse({'items': []});
        return collectionResponse(wire);
      });
      final source = collectionController(client);
      expect(await source.open(PersonalCollection.fromJson(wire)), isTrue);
      final c = NowJourneyController(
        authorizationHeader: () => 'Bearer owner',
        ownerID: () => collectionOwner,
        currentScope: () => true,
        client: client,
        apiBaseUrl: 'https://synthetic.invalid',
        cityID: 'aberdeen-gb',
      );
      c.selectedMine = source;
      NowJourneyFieldEdit? field;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NowJourneyPanel(
              controller: c,
              onClose: () {},
              onEditField: (v) => field = v,
              onOpenMoment: (_, __) {},
              onViewPlan: (_) {},
              onPublish: (_) {},
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.byType(TextField), findsNothing);
      await t.tap(find.text('1. 海边公园'));
      await t.pump();
      expect(field?.current(), isTrue);
      source.moveEntry(0, 1);
      await t.pump();
      expect(field?.current(), isFalse);
      expect(field?.apply('迟到字段'), isFalse);
      expect(source.draftEntries.first.placeName, '第二个地点');
      expect(source.draftEntries.every((v) => v.note.isEmpty), isTrue);
      await t.pumpWidget(const SizedBox.shrink());
      c.dispose();
      source.dispose();
      client.close();
    },
  );

  test(
    'Public Journey retains original gaps and rejects private fields or unreadable source details',
    () {
      final ref = CollectionShareReference('journey', collectionID),
          wire = publicJourney();
      final value = PublicCollectionRecord.fromJson(wire, ref);
      expect(value.journeySnapshot!.stops.map((v) => v['position']), [1, 3]);
      expect(value.journeySnapshot!.stops.first['title'], '');
      expect(
        (wire['journeySnapshot']['stops'] as List).first.containsKey('title'),
        false,
      );
      final leak = publicJourney();
      leak['journeySnapshot']['stops'][1]['title'] = '无权内容';
      expect(
        () => PublicCollectionRecord.fromJson(leak, ref),
        throwsFormatException,
      );
      expect(
        () => PublicCollectionRecord.fromJson({
          ...publicJourney(),
          'journeyDetails': fixture().toInput(),
        }, ref),
        throwsFormatException,
      );
    },
  );
  test(
    'Journey discovery retires late identity reads instead of reviving public snapshots',
    () async {
      final pending = Completer<http.Response>();
      var current = true;
      final client = MockClient((_) => pending.future);
      final c = NowJourneyController(
        authorizationHeader: () => 'Bearer owner',
        ownerID: () => collectionOwner,
        currentScope: () => current,
        client: client,
        apiBaseUrl: 'https://synthetic.invalid',
        cityID: '',
      );
      final future = c.loadVisible();
      current = false;
      pending.complete(
        collectionResponse({
          'schemaVersion': 'journey-discovery-v1',
          'viewerAccountId': collectionOwner,
          'items': [publicJourney()],
          'sourceVersion': 'a' * 64,
          'observedAt': DateTime.now().toUtc().toIso8601String(),
          'validUntil': DateTime.now()
              .toUtc()
              .add(const Duration(seconds: 55))
              .toIso8601String(),
        }),
      );
      await future;
      expect(c.visible, isEmpty);
      c.dispose();
      client.close();
    },
  );
  test(
    'Rich Journey publication requires explicit STOP selection and sends only approved snapshot',
    () async {
      final writes = <Map<String, dynamic>>[];
      final detail = fixture();
      final wire = {
        ...collectionReceipt(),
        'entries': <dynamic>[],
        'journeyDetails': {...detail.toInput(), 'sourceVersion': detail.digest},
      };
      final client = MockClient((r) async {
        if (r.method == 'POST') {
          throw StateError(
            'This test inspects approval before any network write',
          );
        }
        return collectionResponse(wire);
      });
      final source = collectionController(client);
      expect(await source.open(PersonalCollection.fromJson(wire)), true);
      final c = publication(source);
      expect(c.canPublish, false);
      c.selectStop(detail.itinerary.first.id, true);
      expect(c.canPublish, true);
      final snapshot = c.approvedJourneySnapshot;
      writes.add(snapshot);
      expect(snapshot.keys.toSet(), {'title', 'summary', 'stops'});
      final stops = snapshot['stops'] as List;
      expect(stops.length, 1);
      expect(stops.single['entryId'], detail.itinerary.first.id);
      expect(stops.single['position'], 1);
      expect(stops.single['locationVisibility'], 'hidden');
      expect(stops.single.containsKey('momentId'), false);
      expect(stops.single.containsKey('origin'), false);
      expect(stops.single.containsKey('sourceLabel'), false);
      source.setText(summary: '新的私人修改');
      expect(c.canPublish, false);
      c.dispose();
      source.dispose();
      client.close();
    },
  );
  testWidgets(
    'normal-size rich Journey preview publishes only the explicitly selected STOP',
    (tester) async {
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final detail = fixture();
      final wire = {
        ...collectionReceipt(),
        'entries': <dynamic>[],
        'journeyDetails': {...detail.toInput(), 'sourceVersion': detail.digest},
      };
      final writes = <Map<String, dynamic>>[];
      final client = MockClient((r) async {
        if (r.method == 'GET') return collectionResponse(wire);
        final body = jsonDecode(r.body) as Map<String, dynamic>;
        writes.add(body);
        return collectionResponse({
          ...wire,
          'revision': 2,
          'publishedSnapshot': {
            'publicationRevision': 2,
            'sourceRevision': 1,
            'publishedAt': DateTime.now().toUtc().toIso8601String(),
            'title': wire['title'],
            'summary': wire['summary'],
            'stopCount': 1,
          },
          'publicationOperation': {
            'operationId': body['operationId'],
            'action': 'PUBLISH',
            'expectedRevision': 1,
            'resultRevision': 2,
          },
        });
      });
      final source = collectionController(client);
      await source.open(PersonalCollection.fromJson(wire));
      final c = publication(source);
      await tester.pumpWidget(
        MaterialApp(
          home: CollectionPublicationPage(
            controller: c,
            cityName: '合成城市',
            embedded: true,
            onClose: () {},
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(writes, isEmpty);
      expect(c.canPublish, false);
      await tester.tap(find.byType(CheckboxListTile).first);
      await tester.pumpAndSettle();
      expect(c.canPublish, true);
      final publish = find.text('将以上内容公开发布');
      await tester.scrollUntilVisible(
        publish,
        240,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.tap(publish);
      await tester.pumpAndSettle();
      expect(writes.length, 1);
      expect(writes.single.keys.toSet(), {
        'operationId',
        'revision',
        'journeySnapshot',
      });
      final selected = writes.single['journeySnapshot']['stops'] as List;
      expect(selected.length, 1);
      expect(selected.single['entryId'], detail.itinerary.first.id);
      expect(source.editing!.status, 'draft');
      expect(source.editing!.visibility, 'private');
      expect(source.editing!.publishedSnapshot!.stopCount, 1);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      c.dispose();
      source.dispose();
      client.close();
    },
  );

  test(
    'cross-city Journey pages forward signed opaque cursors and retain earlier rows',
    () async {
      final discoveryCursor = 'jd1.Zml4dHVyZQ.${'s' * 43}',
          ownerCursor = 'jl1.Zml4dHVyZQ.${'s' * 43}';
      final seen = <String?>[];
      final other = '22222222-2222-4222-8222-222222222223';
      final client = MockClient((r) async {
        final cursor = r.url.queryParameters['cursor'];
        seen.add(cursor);
        final more = cursor != null;
        if (r.url.path.endsWith('/discovery'))
          return collectionResponse({
            'schemaVersion': 'journey-discovery-v1',
            'viewerAccountId': collectionOwner,
            'items': [
              {...publicJourney(), 'id': more ? other : collectionID},
            ],
            if (!more) 'nextCursor': discoveryCursor,
            'sourceVersion': 'a' * 64,
            'observedAt': DateTime.now().toUtc().toIso8601String(),
            'validUntil': DateTime.now()
                .toUtc()
                .add(const Duration(seconds: 55))
                .toIso8601String(),
          });
        return collectionResponse({
          'items': [collectionReceipt(id: more ? other : collectionID)],
          if (!more) 'nextCursor': ownerCursor,
        });
      });
      final c = NowJourneyController(
        authorizationHeader: () => 'Bearer owner',
        ownerID: () => collectionOwner,
        currentScope: () => true,
        client: client,
        apiBaseUrl: 'https://synthetic.invalid',
        cityID: '',
      );
      await c.loadVisible();
      expect(c.nextCursor, discoveryCursor);
      await c.loadVisible(more: true);
      expect(c.visible.length, 2);
      expect(c.nextCursor, isNull);
      final mine = collectionController(client);
      await mine.load();
      expect(mine.nextCursor, ownerCursor);
      await mine.load(more: true);
      expect(mine.items.length, 2);
      expect(mine.nextCursor, isNull);
      expect(seen, [null, discoveryCursor, null, ownerCursor]);
      c.dispose();
      mine.dispose();
      client.close();
    },
  );
}
