import 'package:birdtie_client/src/workspace/agent_entity_result_card.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_cover_test.dart';
import 'agent_result_projection_test.dart' show typedItem;
import 'person_avatar_fixture.dart' show avatarPeer;

Map<String, dynamic> savedCoverRow({int version = 1, bool visible = true}) => {
  'id': avatarPeer,
  'kind': 'activity',
  'targetId': coverRef.id,
  'title': '隔离真实收藏活动',
  'summary': '公开摘要',
  'cityId': 'alpha',
  'available': visible,
  'note': '只本人可见，不进入图片协议',
  'noteRevision': 0,
  if (visible) 'cover': coverMetadata(version: version),
};
void main() {
  test(
    'cover projection permits activity organization and original opportunity detail only',
    () {
      for (final kind in ['activity', 'organization']) {
        final ref = EntityCoverRef(kind, coverRef.id);
        final raw = {
          ...typedItem(kind, ref.id),
          'cover': coverMetadata(ref: ref),
        };
        expect(AgentResultItem.decode(raw).cover!.ref, ref);
      }
      final opportunity = {
        ...typedItem(
          'opportunity',
          'original-opportunity:${coverRef.id}',
          scope: 'SELF_PRIVATE',
        ),
        'cover': coverMetadata(),
      };
      expect(AgentResultItem.decode(opportunity).cover!.ref, coverRef);
      for (final kind in ['person', 'place', 'community', 'group']) {
        expect(
          () => AgentResultItem.decode({
            ...typedItem(kind, coverRef.id),
            'cover': coverMetadata(),
          }),
          throwsFormatException,
        );
      }
      expect(
        () => AgentResultItem.decode({
          ...opportunity,
          'cover': coverMetadata(
            ref: const EntityCoverRef('activity', avatarPeer),
          ),
        }),
        throwsFormatException,
      );
      final saved = SavedItem.fromJson(savedCoverRow());
      expect(saved.withNote('修改私人备注', 1).cover, same(saved.cover));
      expect(
        () => SavedItem.fromJson({...savedCoverRow(), 'available': false}),
        throwsFormatException,
      );
      expect(
        () => SavedItem.fromJson({...savedCoverRow(), 'kind': 'place'}),
        throwsFormatException,
      );
    },
  );
  testWidgets(
    'cover typed result actual images version changes and current receipt retirement',
    (t) async {
      final changes = ValueNotifier(true);
      addTearDown(changes.dispose);
      final paths = <String>[];
      final client = MockClient((r) async {
        paths.add(r.url.path);
        return http.Response.bytes(coverJPEG(), 200);
      });
      addTearDown(client.close);
      AgentResultItem item(int v) => AgentResultItem.decode({
        ...typedItem('activity', coverRef.id),
        'cover': coverMetadata(version: v),
      });
      Widget app(AgentResultItem i) => MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: i,
            authorizationHeader: () => 'Bearer A',
            mediaClient: client,
            mediaBaseUrl: coverBase,
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
        '${coverRef.path}/1/content',
        '${coverRef.path}/2/content',
      ]);
      changes.value = false;
      await t.pump();
      expect(find.byType(Image), findsNothing);
      changes.value = true;
      await t.pump();
      expect(find.byType(Image), findsNothing);
      expect(paths, hasLength(2));
      await t.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'cover Saved authoritative activity row renders JPEG then hidden source retires without losing note paging',
    (t) async {
      FlutterSecureStorage.setMockInitialValues({});
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      var visible = true, version = 1;
      final paths = <String>[], bodies = <String>[];
      final client = MockClient((r) async {
        paths.add(r.url.path);
        bodies.add(r.body);
        expectSync(r.method, 'GET');
        if (r.url.path.endsWith('/content'))
          return http.Response.bytes(coverJPEG(), 200);
        return coverWire({
          'items': [savedCoverRow(version: version, visible: visible)],
          'nextCursor': null,
        });
      });
      addTearDown(client.close);
      final saved = SavedController(
        client: client,
        apiBaseUrl: coverBase,
        ownerID: () => avatarPeer,
        authorizationHeader: () => 'Bearer A',
      );
      addTearDown(saved.dispose);
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            appBar: AppBar(title: const Text('收藏')),
            body: SavedPage(saved: saved),
          ),
        ),
      );
      await decodeCover(t);
      expect(find.text('收藏'), findsOneWidget);
      expect(find.textContaining('只本人可见'), findsOneWidget);
      version = 2;
      await saved.load(paged: true);
      await decodeCover(t);
      expect(paths.where((p) => p.endsWith('/content')), [
        '${coverRef.path}/1/content',
        '${coverRef.path}/2/content',
      ]);
      visible = false;
      await saved.load(paged: true);
      await t.pump();
      expect(find.byType(Image), findsNothing);
      expect(saved.items.single.note, '只本人可见，不进入图片协议');
      expect(saved.items.single.available, isFalse);
      expect(bodies.every((b) => b.isEmpty), isTrue);
      expect(saved.failed, isFalse);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
    },
  );
}
