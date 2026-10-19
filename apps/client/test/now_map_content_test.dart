import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:birdtie_client/src/content/moment_image_header.dart';
import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';

import 'entity_cover_test.dart' show coverJPEG;

const _owner = '11111111-1111-4111-8111-111111111111';
const _place = '22222222-2222-4222-8222-222222222222';
const _ownMoment = '33333333-3333-4333-8333-333333333333';
const _publicMoment = '44444444-4444-4444-8444-444444444444';
final _source = 'a' * 64;
final _privateBody = [
  '本人私人正文，保留正文。',
  for (var i = 1; i <= 20; i++) '较长正文第$i行，仍是同一条动态正文。',
].join('\n');

class _PanelController extends OwnedMapController {
  _PanelController(this.identity)
    : super(
        client: MockClient((_) async => throw StateError('Unexpected route')),
        apiBaseUrl: () => 'https://example.invalid',
        ownerID: () => _owner,
        authorizationHeader: () => 'Bearer synthetic',
        organizationWorkspaceID: () => null,
        identityChanges: identity,
        refreshAllowed: () => true,
      );

  final ValueNotifier<int> identity;
  bool allowed = true;
  int generation = 0;
  final reads = <String?>[];
  final imageBuffers = <Uint8List>[];
  final jpeg = coverJPEG();

  @override
  bool get current => allowed;
  @override
  int get epoch => generation;
  OwnedMapPin get pin => OwnedMapPin(
    placeID: _place,
    name: '同地图里的地点 North Harbour',
    categoryCode: 'museum',
    cityID: null,
    latitude: 57.1,
    longitude: -2.1,
    visibilitySources: const ['SELF_PRIVATE', 'CIVU_PUBLIC_MOMENT'],
    sourceVersion: _source,
    momentCount: 2,
    moments: const [
      OwnedMapMoment(
        _ownMoment,
        1,
        'SELF_PRIVATE',
        '/v1/me/map-content/moments/$_ownMoment',
      ),
    ],
    momentsTruncated: true,
    cover: null,
    observedAt: DateTime.now().toUtc(),
    validUntil: DateTime.now().toUtc().add(const Duration(seconds: 30)),
  );
  @override
  OwnedMapPin? find(String mapID) => allowed && mapID == pin.mapID ? pin : null;
  @override
  bool sourceCurrent(OwnedMapPin item) =>
      allowed && item.sourceVersion == _source && item.placeID == _place;
  @override
  bool detailCurrent(OwnedMapMomentDetail detail) =>
      allowed && detail.validUntil.isAfter(DateTime.now().toUtc());
  @override
  Future<void> refresh() async {
    notifyListeners();
  }

  OwnedMapMomentDetail detail(bool own) {
    final now = DateTime.now().toUtc(),
        header = MomentImageHeader.inspect(jpeg, storedDerivative: true);
    return OwnedMapMomentDetail(
      viewerAccountID: _owner,
      id: own ? _ownMoment : _publicMoment,
      revision: 1,
      visibilitySource: own ? 'SELF_PRIVATE' : 'CIVU_PUBLIC_MOMENT',
      canEdit: own,
      status: own ? 'draft' : 'published',
      visibility: own ? 'private' : 'public',
      title: '',
      body: own ? _privateBody : '原公开的另一条记录，只读内容。',
      createdAt: now.subtract(const Duration(days: 1)),
      locations: const [OwnedMapLocation(_place, '另一城市的真实关联地点', null, null)],
      media: !own
          ? const []
          : [
              OwnedMapMomentMedia(
                kind: 'IMAGE',
                id: '55555555-5555-4555-8555-555555555555',
                revision: 1,
                mimeType: 'image/jpeg',
                width: header.width,
                height: header.height,
                byteCount: jpeg.length,
                sha256: sha256.convert(jpeg).toString(),
                sourceVersion: 'b' * 64,
                contentPath:
                    '/v1/me/map-content/moments/$_ownMoment/media/55555555-5555-4555-8555-555555555555/content?sourceVersion=${'b' * 64}',
                durationMs: null,
                videoCodec: null,
                audioCodec: null,
                poster: null,
              ),
            ],
      observedAt: now,
      validUntil: now.add(const Duration(seconds: 30)),
      sourceVersion: own ? 'c' * 64 : 'd' * 64,
    );
  }

  @override
  Future<OwnedMapPlacePage> readPlaceMoments(
    OwnedMapPin pin, {
    String? cursor,
    String? placeID,
    OwnedMapMomentDetail? relationSource,
  }) async {
    if (!allowed) throw const FormatException();
    reads.add(cursor);
    final now = DateTime.now().toUtc();
    return OwnedMapPlacePage(
      placeID: _place,
      items: [detail(cursor == null)],
      nextCursor: cursor == null ? 'next.page' : null,
      observedAt: now,
      validUntil: now.add(const Duration(seconds: 30)),
      sourceVersion: _source,
    );
  }

  @override
  Future<OwnedMapThread?> readThread(OwnedMapPin pin,
      OwnedMapMomentDetail seed) async => null;

  @override
  Future<Uint8List> momentContent(
    OwnedMapMomentDetail detail,
    OwnedMapMomentMedia media, {
    bool Function()? current,
  }) async {
    if (!detailCurrent(detail) ||
        current?.call() == false ||
        !detail.media.any((item) => identical(item, media))) {
      throw const FormatException();
    }
    final data = Uint8List.fromList(jpeg);
    imageBuffers.add(data);
    return data;
  }

  void revoke() {
    allowed = false;
    generation++;
    notifyListeners();
  }
}

void main() {
  testWidgets(
    'Now compact expands in place pages renew without scroll reset and retires private content',
    (tester) async {
      tester.view.physicalSize = const Size(390, 760);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final identity = ValueNotifier(0), c = _PanelController(ValueNotifier(0));
      var expansions = 0, closes = 0;
      final edited = <String>[];
      bool Function()? editCurrent;
      final pin = c.pin;
      try {
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: Align(
                alignment: Alignment.topCenter,
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxHeight: 650),
                  child: NowMapContentPanel(
                    controller: c,
                    pin: pin,
                    current: () => c.allowed,
                    changes: identity,
                    onClose: () => closes++,
                    onExpand: () => expansions++,
                    onEdit: (id, current) {
                      edited.add(id);
                      editCurrent = current;
                    },
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.pump();
        await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 30)),
        );
        await tester.pump();
        expect(find.text(pin.name), findsOneWidget);
        expect(find.text('2条动态'), findsOneWidget);
        expect(find.text(_privateBody), findsOneWidget);
        expect(find.text('原公开的另一条记录，只读内容。'), findsNothing);
        expect(find.byType(Image), findsOneWidget);
        final expand = find.text('查看此地点的全部2条动态');
        await tester.ensureVisible(expand);
        await tester.pump();
        expect(expand.hitTestable(), findsOneWidget);
        await tester.tap(expand);
        await tester.pump();
        expect(expansions, 1);
        expect(find.byType(NowMapContentPanel), findsOneWidget);
        await tester.tap(find.byKey(const ValueKey('read-thread:$_ownMoment')));
        await tester.pump();
        await tester.pump();
        await tester.tap(find.byTooltip('编辑动态'));
        expect(edited, [_ownMoment]);
        expect(editCurrent!(), isTrue);
        await tester.tap(find.byTooltip('返回地点动态'));
        await tester.pump();
        await tester.pump();
        final mainScroll = find
            .descendant(
              of: find.byType(ListView),
              matching: find.byType(Scrollable),
            )
            .first;
        await tester.scrollUntilVisible(
          find.text('加载更多动态'),
          200,
          scrollable: mainScroll,
        );
        await tester.pump();
        await tester.tap(find.text('加载更多动态'));
        await tester.pump();
        await tester.pump();
        await tester.scrollUntilVisible(
          find.text('原公开的另一条记录，只读内容。'),
          150,
          scrollable: mainScroll,
        );
        await tester.pump();
        expect(find.byTooltip('编辑动态'), findsNothing);
        expect(find.text('CIVU_PUBLIC_MOMENT'), findsNothing);
        await tester.tap(find.byKey(const ValueKey('read-thread:$_ownMoment')));
        await tester.pump();
        await tester.pump();
        await tester.drag(find.byType(ListView), const Offset(0, -350));
        await tester.pumpAndSettle();
        final scroll = tester
            .widget<ListView>(find.byType(ListView))
            .controller!;
        final offset = scroll.offset;
        expect(offset, greaterThan(0));
        final element = tester.element(find.byType(NowMapContentPanel));
        final readsBeforeRenewal = c.reads.length;
        await c.refresh();
        await tester.pump(const Duration(seconds: 25));
        await tester.pump();
        expect(tester.element(find.byType(NowMapContentPanel)), same(element));
        expect(scroll.offset, closeTo(offset, 1));
        expect(c.reads.skip(readsBeforeRenewal), [null, 'next.page']);
        expect(closes, 0);
        c.revoke();
        await tester.pump();
        expect(find.text(_privateBody), findsNothing);
        expect(find.text('原公开的另一条记录，只读内容。'), findsNothing);
        expect(find.byType(Image), findsNothing);
        expect(find.text('这条动态暂时无法查看'), findsOneWidget);
        expect(c.imageBuffers, isNotEmpty);
        expect(
          c.imageBuffers.every((bytes) => bytes.every((byte) => byte == 0)),
          isTrue,
        );
        expect(editCurrent!(), isFalse);
        expect(closes, 0);
        expect(tester.takeException(), isNull);
      } finally {
        await tester.pumpWidget(const SizedBox.shrink());
        c.dispose();
        identity.dispose();
        c.identity.dispose();
      }
    },
  );
}
