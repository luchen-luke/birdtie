import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';

const owner = '85000000-0000-4000-8000-000000000001';
const moment = '85000000-0000-4000-8000-000000000002';
const place = '85000000-0000-4000-8000-000000000003';

Future<Uint8List> pixels(int width, int height) async {
  final recorder = ui.PictureRecorder();
  final canvas = ui.Canvas(recorder);
  canvas.drawRect(ui.Rect.fromLTWH(0, 0, width.toDouble(), height.toDouble()),
      ui.Paint()..color = const ui.Color(0xff436750));
  final picture = recorder.endRecording();
  final image = await picture.toImage(width, height);
  picture.dispose();
  try {
    final data = (await image.toByteData(format: ui.ImageByteFormat.png))!;
    return Uint8List.fromList(data.buffer.asUint8List(data.offsetInBytes, data.lengthInBytes));
  } finally { image.dispose(); }
}

class ReaderController extends OwnedMapController {
  ReaderController(this.changes, this.pictures, this.dimensions, {this.withText = false})
      : super(client: MockClient((_) async => throw StateError('unexpected HTTP')),
          apiBaseUrl: () => 'https://reader.unit.test', ownerID: () => owner,
          authorizationHeader: () => 'Bearer synthetic',
          organizationWorkspaceID: () => null, identityChanges: changes,
          refreshAllowed: () => true);
  final ValueNotifier<int> changes;
  final List<Uint8List> pictures;
  final List<(int, int)> dimensions;
  final bool withText;
  bool allowed = true, fail = false;
  final readIDs = <String>[];
  late final pin = OwnedMapPin(placeID: '', name: '', categoryCode: 'moment',
    cityID: null, latitude: 31.2, longitude: 121.5,
    visibilitySources: const ['SELF_PRIVATE'], sourceVersion: 'a' * 64,
    momentCount: 1, moments: const [OwnedMapMoment(moment, 1, 'SELF_PRIVATE',
      '/v1/me/map-content/moments/$moment')], momentsTruncated: false, cover: null,
    kind: 'MOMENT_ANCHOR', markerID: 'anchor:$place', anchorID: place,
    momentID: moment, entryID: moment);
  late final detail = OwnedMapMomentDetail(viewerAccountID: owner, id: moment,
    revision: 1, visibilitySource: 'SELF_PRIVATE', canEdit: false,
    status: 'draft', visibility: 'private', title: '',
    body: withText ? '真实正文仍保留' : '',
    createdAt: DateTime.now().toUtc(), createdAtSource: 'UNKNOWN',
    locations: withText ? const [OwnedMapLocation(place, '实际关联地点', null, null)] : const [],
    media: [for (var i = 0; i < pictures.length; i++) OwnedMapMomentMedia(
      kind: 'IMAGE', id: '85000000-0000-4000-8000-00000000001$i',
      revision: 1, mimeType: 'image/png', width: dimensions[i].$1,
      height: dimensions[i].$2, byteCount: pictures[i].length,
      sha256: sha256.convert(pictures[i]).toString(), sourceVersion: 'b' * 64,
      contentPath: '/v1/me/map-content/moments/$moment/media/$i/content',
      durationMs: null, videoCodec: null, audioCodec: null, poster: null)],
    observedAt: DateTime.now().toUtc(),
    validUntil: DateTime.now().toUtc().add(const Duration(seconds: 30)),
    sourceVersion: 'c' * 64);
  @override bool get current => allowed;
  @override OwnedMapPin? find(String id) => id == pin.mapID ? pin : null;
  @override bool sourceCurrent(OwnedMapPin pin) => allowed;
  @override bool detailCurrent(OwnedMapMomentDetail d) => allowed &&
      d.validUntil.isAfter(DateTime.now().toUtc());
  @override Future<OwnedMapMomentDetail> readMoment(OwnedMapMoment value,
      {bool Function()? current, Future<void>? abortTrigger}) async => detail;
  @override Future<OwnedMapThread?> readThread(OwnedMapPin pin,
      OwnedMapMomentDetail seed) async => null;
  @override Future<Uint8List> momentContent(OwnedMapMomentDetail d,
      OwnedMapMomentMedia media, {bool Function()? current}) async {
    if (!detailCurrent(d) || current?.call() == false || fail)
      throw const FormatException('unavailable');
    readIDs.add(media.id);
    return Uint8List.fromList(pictures[d.media.indexOf(media)]);
  }
}

Widget reader(ReaderController c) => MaterialApp(home: Scaffold(body:
  NowMapContentPanel(controller: c, pin: c.pin, current: () => c.allowed,
    changes: c.changes, onClose: () {}, onExpand: () {}, onEdit: (_, __) {},
    initialExpanded: true)));

Future<void> settleReads(WidgetTester t) async {
  await t.pump();
  await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 25)));
  await t.pump(const Duration(milliseconds: 50));
}
void screen(WidgetTester t) {
  t.view.physicalSize = const Size(390, 760);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}
void main() {
  testWidgets('portrait uses content width and original aspect, no invented author or type head', (t) async {
    screen(t);
    final bytes = (await t.runAsync(() => pixels(16, 32)))!;
    final changes = ValueNotifier(0), c = ReaderController(ValueNotifier(0), [bytes], [(16, 32)]);
    await t.pumpWidget(reader(c)); await settleReads(t);
    final frame = find.byKey(const ValueKey('moment-media-frame:$moment:false'));
    expect(t.getSize(frame), const Size(358, 716));
    expect(t.getTopLeft(frame).dx, 16);
    expect(find.text('作者未提供'), findsNothing);
    expect(find.text('时间未提供'), findsNothing);
    expect(find.text('动态'), findsNothing);
    expect(find.text('1/1'), findsNothing);
    expect(find.byTooltip('查看完整媒体'), findsNothing);
    expect(find.byIcon(Icons.photo_outlined), findsNothing);
    expect(find.byIcon(Icons.person_outline), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox()); c.dispose(); c.changes.dispose(); changes.dispose();
  });

  testWidgets('gallery swipes at natural aspect and returns from viewer at same page', (t) async {
    screen(t);
    final images = (await t.runAsync(() async => [await pixels(32, 16), await pixels(16, 32)]))!;
    final c = ReaderController(ValueNotifier(0), images, [(32, 16), (16, 32)]);
    await t.pumpWidget(reader(c)); await settleReads(t);
    final frame = find.byKey(const ValueKey('moment-media-frame:$moment:false'));
    expect(t.getSize(frame), const Size(358, 179));
    expect(find.text('1/2'), findsOneWidget);
    expect(find.byTooltip('下一张'), findsNothing);
    expect(find.byTooltip('上一张'), findsNothing);
    final badge = t.getRect(find.text('1/2'));
    expect(t.getRect(frame).contains(badge.topLeft), isTrue);
    expect(t.getRect(frame).contains(badge.bottomRight), isTrue);
    await t.drag(find.byType(PageView), const Offset(-300, 0));
    await t.pumpAndSettle(); await settleReads(t);
    expect(find.text('2/2'), findsOneWidget);
    expect(t.getSize(frame), const Size(358, 716));
    await t.tap(find.bySemanticsLabel('查看第2张图片'));
    await settleReads(t);
    expect(find.byTooltip('返回动态'), findsOneWidget);
    expect(find.text('2/2'), findsOneWidget);
    await t.tap(find.byTooltip('返回动态')); await settleReads(t);
    expect(find.text('2/2'), findsOneWidget);
    expect(t.getSize(frame), const Size(358, 716));
    expect(find.byTooltip('查看完整媒体'), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox()); c.dispose(); c.changes.dispose();
  });

  testWidgets('failed image keeps actual body and lightweight place with retry', (t) async {
    screen(t);
    final bytes = (await t.runAsync(() => pixels(32, 16)))!;
    final c = ReaderController(ValueNotifier(0), [bytes], [(32, 16)], withText: true)..fail = true;
    await t.pumpWidget(reader(c)); await settleReads(t);
    expect(find.text('真实正文仍保留'), findsOneWidget);
    expect(find.text('实际关联地点'), findsOneWidget);
    expect(find.byType(ActionChip), findsNothing);
    expect(find.text('图片加载失败'), findsOneWidget);
    c.fail = false;
    await t.tap(find.text('重试')); await settleReads(t);
    expect(find.text('图片加载失败'), findsNothing);
    expect(find.byType(Image), findsOneWidget);
    c.allowed = false; c.notifyListeners(); await t.pump();
    expect(find.text('真实正文仍保留'), findsNothing);
    expect(find.byType(Image), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox()); c.dispose(); c.changes.dispose();
  });
}
