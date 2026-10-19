import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

String id(int n) => '83000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
const caption = '隔离测试：不同动态也可以有相同正文';

OwnedMapPin anchor(
  int projection,
  int moment, {
  String thread = '',
  int position = 0,
}) => OwnedMapPin(
  placeID: id(100 + projection),
  name: caption,
  categoryCode: '',
  cityID: null,
  latitude: 31.2,
  longitude: 121.5,
  visibilitySources: const ['SELF_PRIVATE'],
  sourceVersion: 'a' * 64,
  momentCount: 1,
  moments: [
    OwnedMapMoment(
      id(moment),
      1,
      'SELF_PRIVATE',
      '/v1/me/map-content/moments/${id(moment)}',
    ),
  ],
  momentsTruncated: false,
  cover: null,
  kind: 'MOMENT_ANCHOR',
  markerID: 'anchor:${id(projection)}',
  anchorID: id(projection),
  momentID: id(moment),
  entryID: id(moment),
  threadID: thread,
  threadPosition: position,
);

class ChoicesController extends OwnedMapController {
  ChoicesController(ValueNotifier<int> changes, List<OwnedMapPin> pins)
    : this._(changes, pins, ValueNotifier<int>(0));
  ChoicesController._(this.changes, this.pins, this.identity)
    : super(
        client: MockClient((_) async => throw StateError('unexpected HTTP')),
        apiBaseUrl: () => 'https://choices.unit.test',
        ownerID: () => id(999),
        authorizationHeader: () => 'Bearer synthetic',
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => true,
      );
  final ValueNotifier<int> changes, identity;
  @override
  void dispose() {
    super.dispose();
    identity.dispose();
  }

  final List<OwnedMapPin> pins;
  final retired = <String>{};
  final reads = <String>[];
  bool permitted = true;
  @override
  bool get current => permitted;
  @override
  OwnedMapPin? find(String value) =>
      pins.where((p) => p.mapID == value && sourceCurrent(p)).firstOrNull;
  @override
  bool sourceCurrent(OwnedMapPin pin) =>
      permitted && !retired.contains(pin.mapID) && pins.contains(pin);
  @override
  bool detailCurrent(OwnedMapMomentDetail detail) =>
      permitted && detail.validUntil.isAfter(DateTime.now().toUtc());
  @override
  Future<OwnedMapMomentDetail> readMoment(
    OwnedMapMoment moment, {
    bool Function()? current,
    Future<void>? abortTrigger,
  }) async {
    if (!permitted || current?.call() == false)
      throw const FormatException('retired');
    reads.add(moment.id);
    final now = DateTime.now().toUtc();
    return OwnedMapMomentDetail.decode(
      {
        'schemaVersion': 'map-content-moment-v1',
        'viewerAccountId': id(999),
        'id': moment.id,
        'revision': 1,
        'visibilitySource': 'SELF_PRIVATE',
        'canEdit': true,
        'status': 'draft',
        'visibility': 'private',
        'title': '',
        'body': '正文 ${moment.id}',
        'createdAt': now.subtract(const Duration(days: 1)).toIso8601String(),
        'locations': <Object>[],
        'media': <Object>[],
        'sourceVersion': 'b' * 64,
        'observedAt': now.toIso8601String(),
        'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
      },
      ownerID: id(999),
      now: now,
    );
  }

  @override
  Future<OwnedMapThread?> readThread(
    OwnedMapPin pin,
    OwnedMapMomentDetail seed,
  ) async => null;
}

Widget panel(ChoicesController c) => MaterialApp(
  home: Scaffold(
    body: NowMapContentGroupPanel(
      controller: c,
      pins: c.pins,
      current: () => c.permitted,
      changes: c.changes,
      onClose: () {},
      onEdit: (_, __) {},
    ),
  ),
);

void main() {
  testWidgets(
    'same Moment projections form one choice; different Moments with identical text remain distinct',
    (t) async {
      final c = ChoicesController(ValueNotifier(0), [
        anchor(1, 21),
        anchor(2, 21),
        anchor(3, 21),
        anchor(4, 22),
      ]);
      await t.pumpWidget(panel(c));
      await t.pump();
      expect(find.text(caption), findsNWidgets(2));
      final list = t.widget<ListView>(find.byType(ListView));
      expect(list.padding, EdgeInsets.zero);
      expect(t.getTopLeft(find.byKey(ValueKey(c.pins[0].mapID))).dy,
          lessThanOrEqualTo(t.getBottomLeft(find.byType(ListTile).first).dy + 1));
      expect(find.byKey(ValueKey(c.pins[0].mapID)), findsOneWidget);
      expect(find.byKey(ValueKey(c.pins[1].mapID)), findsNothing);
      expect(find.byKey(ValueKey(c.pins[2].mapID)), findsNothing);
      await t.tap(find.byKey(ValueKey(c.pins[3].mapID)));
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(c.reads, [id(22)]);
      expect(find.text('正文 ${id(22)}'), findsOneWidget);
      expect(find.textContaining('串文'), findsNothing);
      await t.tap(find.byTooltip('返回内容列表'));
      await t.pump();
      expect(find.text(caption), findsNWidgets(2));
      await t.pumpWidget(const SizedBox());
      c.dispose();
      c.changes.dispose();
    },
  );

  testWidgets(
    'retired representative cannot hide a current projection or retain a selected reader',
    (t) async {
      final c = ChoicesController(ValueNotifier(0), [
        anchor(1, 21),
        anchor(2, 21),
        anchor(3, 22),
      ]);
      await t.pumpWidget(panel(c));
      await t.pump();
      await t.tap(find.byKey(ValueKey(c.pins[0].mapID)));
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(find.text('正文 ${id(21)}'), findsOneWidget);
      c.retired.add(c.pins[0].mapID);
      c.changes.value++;
      await t.pump();
      await t.pump(const Duration(milliseconds: 100));
      expect(find.text('正文 ${id(21)}'), findsNothing);
      expect(find.text(caption), findsNWidgets(2));
      expect(find.byKey(ValueKey(c.pins[1].mapID)), findsOneWidget);
      expect(c.reads, [
        id(21),
      ]); // No automatic read/action retarget to a new grant.
      c.permitted = false;
      c.changes.value++;
      await t.pump();
      expect(find.text(caption), findsNothing);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      c.changes.dispose();
    },
  );

  testWidgets(
    'genuine thread choices keep each original segment and its authoritative position',
    (t) async {
      final c = ChoicesController(ValueNotifier(0), [
        anchor(1, 21, thread: id(80)),
        anchor(2, 21, thread: id(80)),
        anchor(3, 22, thread: id(80), position: 2),
      ]);
      await t.pumpWidget(panel(c));
      await t.pump();
      expect(find.text(caption), findsNWidgets(2));
      expect(find.text('串文 · 第1段'), findsOneWidget);
      expect(find.text('串文 · 第3段'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      c.changes.dispose();
    },
  );
}
