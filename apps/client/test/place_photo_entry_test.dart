import 'dart:async';

import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/place_photo_editor.dart';
import 'package:birdtie_client/src/workspace/now_place_preview.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'entity_cover_test.dart'
    show coverJPEG, coverWire, coverSettle, decodeCover;
import 'person_avatar_fixture.dart' show avatarPeer;
import 'place_photo_test.dart';
import 'place_photo_projection_test.dart' show photoPlace;
import 'place_photo_editor_test.dart' show normalPhotoView, photoTap;
import 'place_history_controller_test.dart' show historyTestData;

class _PhotoTransport extends MockClient {
  _PhotoTransport(super.fn);
  bool closed = false;
  @override
  void close() {
    closed = true;
    super.close();
  }
}

http.Response photoAncillary(http.Request r) {
  if (r.url.path.endsWith('/activities') || r.url.path == '/v1/me/moments') {
    return coverWire([]);
  }
  if (r.url.path.endsWith('/social-history')) {
    return coverWire({...historyTestData(), 'placeId': photoRef.id});
  }
  return coverWire(null, status: 404);
}

void main() {
  testWidgets(
    'Place canonical anonymous detail renders actual JPEG with no editorial authority or borrowed close',
    (t) async {
      normalPhotoView(t);
      final calls = <http.Request>[];
      final changes = ValueNotifier<String?>(null);
      addTearDown(changes.dispose);
      final client = _PhotoTransport((r) async {
        calls.add(r);
        if (r.url.path.endsWith('/content'))
          return http.Response.bytes(coverJPEG(), 200);
        if (r.url.path == '/v1/places/${photoRef.id}')
          return coverWire(photoPlace());
        return photoAncillary(r);
      });
      addTearDown(client.close);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PlaceDetailSheet(
              placeID: photoRef.id,
              authorizationHeader: () => changes.value,
              ownerID: () => null,
              workspaceChanges: changes,
              onOpenActivity: (_) {},
              apiBaseUrl: photoBase,
              client: client,
            ),
          ),
        ),
      );
      await decodeCover(t);
      expect(calls.any((r) => r.url.path.endsWith('/editorial')), isFalse);
      expect(find.byKey(const Key('place-photo-entry')), findsNothing);
      expect(
        calls
            .where((r) => r.url.path.endsWith('/content'))
            .single
            .headers['Authorization'],
        isNull,
      );
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(client.closed, isFalse);
      expect(calls.every((r) => r.method == 'GET'), isTrue);
    },
  );
  testWidgets(
    'Place human detail opens original editorial only granted permissions not organization owner alias',
    (t) async {
      normalPhotoView(t);
      FlutterSecureStorage.setMockInitialValues({});
      final workspace = ValueNotifier<String?>(null);
      addTearDown(workspace.dispose);
      final calls = <http.Request>[];
      final client = _PhotoTransport((r) async {
        calls.add(r);
        if (r.url.path == '/v1/places/${photoRef.id}')
          return coverWire(photoPlace(visiblePhoto: false));
        if (r.url.path.endsWith('/editorial'))
          return coverWire(photoEditorial());
        if (r.url.path == photoRef.path)
          return coverWire({'version': 0, 'placePhoto': null});
        return photoAncillary(r);
      });
      addTearDown(client.close);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PlaceDetailSheet(
              placeID: photoRef.id,
              authorizationHeader: () => 'Bearer human',
              ownerID: () => avatarPeer,
              organizationWorkspaceID: () => workspace.value,
              workspaceChanges: workspace,
              onOpenActivity: (_) {},
              apiBaseUrl: photoBase,
              client: client,
            ),
          ),
        ),
      );
      await coverSettle(t);
      expect(find.byKey(const Key('place-photo-entry')), findsOneWidget);
      await photoTap(t, find.byKey(const Key('place-photo-entry')));
      expect(find.byType(PlacePhotoEditor), findsOneWidget);
      expect(find.text('选择地点照片'), findsOneWidget);
      expect(
        calls.where((r) => r.url.path.endsWith('/editorial')),
        hasLength(2),
      );
      workspace.value = 'organization-workspace';
      workspace.value = null;
      await t.pump();
      expect(find.text('选择地点照片'), findsNothing);
      expect(find.text('原地点编辑已退出'), findsOneWidget);
      expect(calls.any((r) => r.method != 'GET'), isFalse);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(client.closed, isFalse);
    },
  );
  testWidgets(
    'Place detail original owner ABA drops late granted editorial projection',
    (t) async {
      normalPhotoView(t);
      FlutterSecureStorage.setMockInitialValues({});
      final owner = ValueNotifier(avatarPeer);
      addTearDown(owner.dispose);
      final pending = Completer<http.Response>();
      var editorialReads = 0;
      final client = _PhotoTransport((r) async {
        if (r.url.path == '/v1/places/${photoRef.id}')
          return coverWire(photoPlace(visiblePhoto: false));
        if (r.url.path.endsWith('/editorial')) {
          editorialReads++;
          return editorialReads == 1
              ? pending.future
              : coverWire(null, status: 403);
        }
        return photoAncillary(r);
      });
      addTearDown(client.close);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PlaceDetailSheet(
              placeID: photoRef.id,
              authorizationHeader: () => 'Bearer unchanged',
              ownerID: () => owner.value,
              workspaceChanges: owner,
              onOpenActivity: (_) {},
              apiBaseUrl: photoBase,
              client: client,
            ),
          ),
        ),
      );
      await coverSettle(t);
      expect(editorialReads, 1);
      owner.value = '44444444-4444-4444-8444-444444444444';
      owner.value = avatarPeer;
      await coverSettle(t);
      pending.complete(coverWire(photoEditorial()));
      await coverSettle(t);
      expect(find.byKey(const Key('place-photo-entry')), findsNothing);
      expect(find.byType(Image), findsNothing);
      expect(editorialReads, 2);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
  testWidgets(
    'Place Now preview actual original public photo is bounded by map lease without map or body writes',
    (t) async {
      normalPhotoView(t);
      final current = ValueNotifier(true);
      addTearDown(current.dispose);
      final calls = <http.Request>[];
      var opens = 0;
      final client = _PhotoTransport((r) async {
        calls.add(r);
        if (r.url.path == photoRef.path)
          return coverWire({'version': 1, 'placePhoto': photoMetadata()});
        if (r.url.path.endsWith('/content'))
          return http.Response.bytes(coverJPEG(), 200);
        return coverWire(null, status: 404);
      });
      addTearDown(client.close);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: NowPlacePreview(
              placeID: photoRef.id,
              placeTitle: '隔离合成地点',
              validUntil: DateTime.now().toUtc().add(
                const Duration(seconds: 4),
              ),
              authorizationHeader: () => null,
              workspaceID: () => null,
              sourceChanges: current,
              current: () => current.value,
              revalidate: () async => true,
              onClose: () {},
              onOpenPlace: () => opens++,
              onOpenMoment: (_) {},
              apiBaseUrl: photoBase,
              client: client,
            ),
          ),
        ),
      );
      await decodeCover(t);
      expect(calls.any((r) => r.url.path == photoRef.path), isTrue);
      await t.pump(const Duration(seconds: 5));
      await t.pump();
      expect(find.byType(Image), findsNothing);
      expect(find.text('请重新打开地点'), findsOneWidget);
      expect(opens, 0);
      expect(calls.every((r) => r.method == 'GET' && r.body.isEmpty), isTrue);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      expect(client.closed, isFalse);
    },
  );
}
