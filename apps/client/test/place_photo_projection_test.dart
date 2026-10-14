import 'dart:async';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_entity_result_card.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:birdtie_client/src/workspace/place_photo.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_result_projection_test.dart' show typedItem;
import 'entity_cover_test.dart'
    show coverJPEG, coverWire, coverSettle, decodeCover;
import 'person_avatar_fixture.dart' show avatarPeer;
import 'place_photo_test.dart';

Map<String, dynamic> photoPlace({bool visiblePhoto = true, int version = 1}) =>
    {
      'id': photoRef.id,
      'name': '隔离合成地点',
      'categoryCode': 'park',
      'summary': '公开地点原文',
      'addressLabel': '合成 QA 地址',
      'source': {'label': '隔离样例', 'freshness': 'verified'},
      'location': {'precision': 'none'},
      if (visiblePhoto) 'placePhoto': photoMetadata(version: version),
    };
Map<String, dynamic> photoSaved({bool available = true, int version = 1}) => {
  'id': avatarPeer,
  'kind': 'place',
  'targetId': photoRef.id,
  'title': available ? '隔离合成地点' : '',
  'summary': '',
  'cityId': 'aberdeen-gb',
  'cityName': '阿伯丁',
  'available': available,
  'note': '真实本人草稿不进入照片协议',
  'noteRevision': 2,
  if (available) ...{
    'latitude': 57.15,
    'longitude': -2.09,
    'sourceExpiresAt': DateTime.now()
        .toUtc()
        .add(const Duration(seconds: 50))
        .toIso8601String(),
  },
  if (available) 'placePhoto': photoMetadata(version: version),
};
void main() {
  test(
    'Place photo actual DTO pairs public Place typed Result and visible Saved without cover alias',
    () {
      final public = PublicPlace.fromJson(photoPlace());
      expect(public.placePhoto!.ref, photoRef);
      final raw = {
        ...typedItem('place', photoRef.id),
        'placePhoto': photoMetadata(),
      };
      expect(AgentResultItem.decode(raw).placePhoto!.ref, photoRef);
      for (final kind in [
        'person',
        'activity',
        'organization',
        'community',
        'group',
      ]) {
        expect(
          () => AgentResultItem.decode({
            ...typedItem(kind, photoRef.id),
            'placePhoto': photoMetadata(),
          }),
          throwsFormatException,
        );
      }
      expect(
        () => AgentResultItem.decode({...raw, 'cover': photoMetadata()}),
        throwsFormatException,
      );
      expect(
        AgentResultItem(
          entity: AgentResultRef(type: 'place', id: photoRef.id),
          title: '真实地点',
          summary: '',
          scope: 'AUTHORIZED_VIEW',
          cover: EntityCoverMetadata.fromJson(photoMetadata(), photoRef),
        ).valid,
        isFalse,
      );
      final saved = SavedItem.fromJson(photoSaved());
      expect(saved.withNote('修改的本人备注', 3).placePhoto, same(saved.placePhoto));
      for (final diff in [
        {'available': false},
        {'kind': 'activity'},
        {'targetId': avatarPeer},
      ]) {
        expect(
          () => SavedItem.fromJson({...photoSaved(), ...diff}),
          throwsFormatException,
        );
      }
      expect(
        PublicPlace.fromJson({
          ...photoPlace(visiblePhoto: false),
          'photoUrl': 'https://legacy.invalid/place.jpg',
        }).placePhoto,
        isNull,
      );
      expect(
        SavedItem.fromJson({
          ...photoSaved(available: false),
          'photoUrl': 'https://legacy.invalid/place.jpg',
        }).placePhoto,
        isNull,
      );
    },
  );
  testWidgets(
    'Place Result actual JPEG version delete source ABA retires original receipt pixels',
    (t) async {
      final changes = ValueNotifier(true);
      addTearDown(changes.dispose);
      final paths = <String>[];
      final client = MockClient((r) async {
        paths.add(r.url.path);
        return http.Response.bytes(coverJPEG(), 200);
      });
      addTearDown(client.close);
      AgentResultItem item(int v, {bool photo = true}) =>
          AgentResultItem.decode({
            ...typedItem('place', photoRef.id),
            if (photo) 'placePhoto': photoMetadata(version: v),
          });
      Widget app(AgentResultItem i) => MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: i,
            mediaClient: client,
            mediaBaseUrl: photoBase,
            authorizationHeader: () => null,
            mediaIdentityChanges: changes,
            sourceCurrent: () => changes.value,
            mediaCurrent: () => changes.value,
          ),
        ),
      );
      await t.pumpWidget(app(item(1)));
      await decodeCover(t);
      await t.pumpWidget(app(item(2)));
      await decodeCover(t);
      expect(paths, [
        '${photoRef.path}/1/content',
        '${photoRef.path}/2/content',
      ]);
      await t.pumpWidget(app(item(2, photo: false)));
      await t.pump();
      expect(find.byType(Image), findsNothing);
      await t.pumpWidget(app(item(3)));
      await decodeCover(t);
      changes.value = false;
      changes.value = true;
      await t.pump();
      expect(find.byType(Image), findsNothing);
      expect(paths, hasLength(3));
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    'Place photo late content actor ABA and original lease renewal cannot restore expired Image',
    (t) async {
      final identity = ValueNotifier('Bearer A');
      addTearDown(identity.dispose);
      final pending = Completer<http.Response>();
      var delayed = true, reads = 0;
      final client = MockClient((r) async {
        reads++;
        return delayed ? pending.future : http.Response.bytes(coverJPEG(), 200);
      });
      addTearDown(client.close);
      Widget app(
        EntityCoverMetadata? metadata, {
        Key key = const Key('original-photo'),
      }) => MaterialApp(
        home: Scaffold(
          body: EntityCover(
            key: key,
            metadata: metadata,
            api: PlacePhotoAPI(
              client: client,
              baseUrl: photoBase,
              authorizationHeader: () => identity.value,
            ).pixels,
            current: () => true,
            identityChanges: identity,
            width: 320,
            height: 180,
          ),
        ),
      );
      await t.pumpWidget(
        app(EntityCoverMetadata.fromJson(photoMetadata(), photoRef)),
      );
      await coverSettle(t);
      identity.value = 'Bearer B';
      identity.value = 'Bearer A';
      pending.complete(http.Response.bytes(coverJPEG(), 200));
      await coverSettle(t);
      expect(find.byType(Image), findsNothing);
      expect(reads, 1);
      delayed = false;
      final original = EntityCoverMetadata.fromJson(
        photoMetadata(lease: const Duration(seconds: 4)),
        photoRef,
      );
      await t.pumpWidget(app(original, key: const Key('fresh-photo')));
      await decodeCover(t);
      await t.pumpWidget(
        app(
          EntityCoverMetadata.fromJson(photoMetadata(), photoRef),
          key: const Key('fresh-photo'),
        ),
      );
      await t.pump();
      expect(reads, 2);
      await t.pump(const Duration(seconds: 5));
      await t.pump();
      expect(find.byType(Image), findsNothing);
      expect(reads, 2);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    'Place Saved public pixels hidden row retirement retains note and failed query disables map collection',
    (t) async {
      FlutterSecureStorage.setMockInitialValues({});
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      var visible = true, failing = false, version = 1, maps = 0, adds = 0;
      final methods = <String>[], paths = <String>[];
      final client = MockClient((r) async {
        methods.add(r.method);
        paths.add(r.url.path);
        if (r.url.path.endsWith('/content'))
          return http.Response.bytes(coverJPEG(), 200);
        return coverWire({
          'items': [photoSaved(available: visible, version: version)],
          'nextCursor': null,
        }, status: failing ? 503 : 200);
      });
      addTearDown(client.close);
      final saved = SavedController(
        client: client,
        apiBaseUrl: photoBase,
        authorizationHeader: () => 'Bearer A',
        ownerID: () => avatarPeer,
      );
      addTearDown(saved.dispose);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            appBar: AppBar(title: const Text('收藏')),
            body: SavedPage(
              saved: saved,
              onViewMap: (_) => maps++,
              onAddToCollection: (_) => adds++,
            ),
          ),
        ),
      );
      await decodeCover(t);
      expect(find.text('收藏'), findsOneWidget);
      version = 2;
      await saved.load(paged: true);
      await decodeCover(t);
      expect(paths.where((p) => p.endsWith('/content')), [
        '${photoRef.path}/1/content',
        '${photoRef.path}/2/content',
      ]);
      final checkbox = find.byWidgetPredicate(
        (w) => w is Checkbox && w.semanticLabel == '选择隔离合成地点加入集合',
      );
      await t.ensureVisible(checkbox);
      await t.tap(checkbox);
      await t.pump();
      expect(
        t
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, '已加载地点上图与比较'),
            )
            .onPressed,
        isNotNull,
      );
      expect(
        t
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, '加入已有旅程或主题地图'),
            )
            .onPressed,
        isNotNull,
      );
      failing = true;
      await saved.load(paged: true);
      await t.pump();
      expect(saved.failed, isTrue);
      expect(
        t
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, '已加载地点上图与比较'),
            )
            .onPressed,
        isNull,
      );
      expect(
        t
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, '加入已有旅程或主题地图'),
            )
            .onPressed,
        isNull,
      );
      expect(maps, 0);
      expect(adds, 0);
      failing = false;
      visible = false;
      await saved.load(paged: true);
      await t.pump();
      expect(find.byType(Image), findsNothing);
      expect(saved.items.single.note, '真实本人草稿不进入照片协议');
      expect(saved.items.single.noteRevision, 2);
      expect(saved.failed, isFalse);
      expect(methods.every((m) => m == 'GET'), isTrue);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
}
