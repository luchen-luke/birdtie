import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:birdtie_client/src/workspace/person_avatar.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'content_map_controller_test.dart' as f;

class ReaderFixture extends f.Fixture {
  bool video = false, unknown = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    if (request.url.path.endsWith('/thread')) {
      return http.StreamedResponse(
        Stream.value('404 page not found\n'.codeUnits),
        404,
        headers: {'content-type': 'text/plain'},
      );
    }
    return super.send(request);
  }

  Map<String, Object?> media(int n, {bool isVideo = false}) => {
    'kind': isVideo ? 'VIDEO' : 'IMAGE',
    'id': f.id(6000 + n),
    'revision': 1,
    'mimeType': isVideo ? 'video/mp4' : 'image/jpeg',
    'width': 160,
    'height': 90,
    'byteCount': isVideo ? 20 : f.jpg.length,
    'sha256': isVideo ? f.hash('video') : sha256.convert(f.jpg).toString(),
    'sourceVersion': f.hash('reader-$n'),
    'contentPath':
        '/v1/me/map-content/moments/${f.id(1)}/media/${f.id(6000 + n)}/content?sourceVersion=${f.hash('reader-$n')}',
    'durationMs': isVideo ? 65000 : null,
    'videoCodec': isVideo ? 'h264' : null,
    'audioCodec': isVideo ? 'none' : null,
    'poster': isVideo
        ? {
            ...cover(1),
            'kind': 'MOMENT_VIDEO_POSTER',
            'id': f.id(6000 + n),
            'sourceVersion': f.hash('poster-$n'),
            'contentPath':
                '/v1/me/map-content/moments/${f.id(1)}/media/${f.id(6000 + n)}/content?sourceVersion=${f.hash('poster-$n')}&variant=poster',
          }
        : null,
  };
  @override
  Map<String, Object?> detail(int n) => {
    ...super.detail(n),
    'title': '真实内容只出现一次',
    'body': '真实内容只出现一次',
    'author': unknown
        ? null
        : {'accountId': owner, 'displayName': '林小满', 'avatar': null},
    'createdAtSource': unknown ? 'UNKNOWN' : 'CIVU_ORIGINAL',
    'publishedAt': null,
    'media': [media(1, isVideo: video), if (!video) media(2)],
  };
}

class _CancellableVideoFixture extends ReaderFixture {
  _CancellableVideoFixture() {
    video = true;
  }
  bool bodyRequested = false, cancelled = false;
  late final body = StreamController<List<int>>(
    onCancel: () {
      cancelled = true;
    },
  );
  final bytes = Uint8List.fromList([
    0,
    0,
    0,
    24,
    102,
    116,
    121,
    112,
    105,
    115,
    111,
    109,
    0,
    0,
    0,
    0,
    105,
    115,
    111,
    109,
    109,
    112,
    52,
    50,
  ]);
  @override
  Map<String, Object?> media(int n, {bool isVideo = false}) => {
    ...super.media(n, isVideo: isVideo),
    'byteCount': bytes.length,
    'sha256': sha256.convert(bytes).toString(),
  };
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    if (request.url.path.endsWith('/content') &&
        request.url.queryParameters['variant'] == null) {
      bodyRequested = true;
      return http.StreamedResponse(
        body.stream,
        200,
        contentLength: bytes.length,
        headers: {
          'cache-control': 'private, no-store',
          'content-type': 'video/mp4',
          'x-content-type-options': 'nosniff',
        },
      );
    }
    return super.send(request);
  }
}

Future<Set<String>> _temporaryVideoReads() async => {
  await for (final e in Directory.systemTemp.list())
    if (e is Directory &&
        e.path
            .split(Platform.pathSeparator)
            .last
            .startsWith('birdtie-video-read-'))
      e.path,
};

Future<(OwnedMapController, ValueNotifier<int>)> mountReader(
  WidgetTester t,
  ReaderFixture fixture, {
  bool expanded = true,
  VoidCallback? onExpand,
  VoidCallback? onEdit,
}) async {
  final identity = ValueNotifier(0);
  final c = OwnedMapController(
    client: fixture,
    apiBaseUrl: () => f.base,
    ownerID: () => fixture.owner,
    authorizationHeader: () => 'Bearer fixture',
    organizationWorkspaceID: () => fixture.organization,
    identityChanges: identity,
    refreshAllowed: () => true,
    now: () => fixture.now,
  );
  c.updateQuery('', f.bounds);
  await f.drain(t, () => c.current);
  final pin = c.items.single, epoch = c.permissionEpoch;
  await t.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Align(
          alignment: Alignment.topCenter,
          child: SizedBox(
            width: 360,
            height: 640,
            child: NowMapContentPanel(
              controller: c,
              pin: pin,
              initialExpanded: expanded,
              current: () =>
                  c.permissionEpoch == epoch &&
                  c.find(pin.mapID)?.sourceVersion == pin.sourceVersion,
              changes: c,
              onClose: () {},
              onExpand: onExpand ?? () {},
              onEdit: (_, current) {
                if (current()) onEdit?.call();
              },
            ),
          ),
        ),
      ),
    ),
  );
  await f.drain(t, () => find.text('真实内容只出现一次').evaluate().isNotEmpty);
  return (c, identity);
}

Future<void> disposeReader(
  WidgetTester t,
  OwnedMapController c,
  ValueNotifier<int> i,
) async {
  await t.pumpWidget(const SizedBox());
  c.dispose();
  i.dispose();
  await t.pump();
}

void main() {
  testWidgets(
    'WAN reader disposal cancels the loading video socket and deletes unpublished temp',
    (t) async {
      final fixture = _CancellableVideoFixture()..now = DateTime.now().toUtc();
      final before = await t.runAsync(_temporaryVideoReads);
      final (c, identity) = await mountReader(t, fixture);
      await t.tap(find.byTooltip('播放视频'));
      await f.drain(t, () => fixture.bodyRequested);
      expect(fixture.cancelled, false);
      await disposeReader(t, c, identity);
      await f.drain(t, () => fixture.cancelled);
      Set<String>? after;
      for (var i = 0; i < 100; i++) {
        after = await t.runAsync(_temporaryVideoReads);
        if (setEquals(after, before)) break;
        await t.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 5)),
        );
        await t.pump();
      }
      expect(after, before);
      unawaited(fixture.body.close());
    },
  );
  test('moment author avatar accepts only its exact controlled source', () {
    final fixture = ReaderFixture();
    final sourceID = f.id(8000);
    final sourceVersion = f.hash('original-author-avatar');
    final legacyPath =
        '/v1/me/map-content/moments/${f.id(1)}/author-avatar/$sourceID/content?sourceVersion=$sourceVersion';
    final avatar = <String, Object?>{
      'accountId': fixture.owner,
      'version': 3,
      'thumbnailPath': legacyPath,
      'width': 128,
      'height': 128,
      'byteCount': 200,
      'sha256': f.hash('avatar-jpeg'),
    };
    OwnedMapMomentDetail decode(Map<String, Object?> candidate) =>
        OwnedMapMomentDetail.decode(
          {
            ...fixture.detail(1),
            'author': {
              'accountId': fixture.owner,
              'displayName': '林小满',
              'avatar': candidate,
            },
          },
          ownerID: fixture.owner,
          now: fixture.now,
        );
    final legacy = decode(avatar).author!.avatar!;
    expect(legacy.thumbnailPath, legacyPath);
    expect(legacy.accountID, fixture.owner);
    expect(legacy.version, 3);
    final nativePath = '/v1/accounts/${fixture.owner}/avatar/3/content';
    expect(
      decode({
        ...avatar,
        'thumbnailPath': nativePath,
      }).author!.avatar!.thumbnailPath,
      nativePath,
    );
    // The profile parser remains restricted to native profile-avatar routes.
    expect(
      () => PersonAvatarMetadata.fromJson(avatar, fixture.owner),
      throwsFormatException,
    );
    for (final path in <String>[
      legacyPath.replaceFirst('/moments/${f.id(1)}/', '/moments/${f.id(2)}/'),
      'https://example.com$legacyPath',
      '//example.com$legacyPath',
      '$legacyPath&extra=true',
      '$legacyPath&sourceVersion=$sourceVersion',
      '$legacyPath#avatar',
      '$legacyPath\n',
      legacyPath.replaceFirst(sourceID, '00000000-0000-0000-0000-000000000000'),
      legacyPath.replaceFirst(sourceID, 'invalid-source-media-id'),
      legacyPath.replaceFirst(sourceID, '%38${sourceID.substring(1)}'),
      legacyPath.replaceFirst(sourceVersion, sourceVersion.toUpperCase()),
      legacyPath.replaceFirst(sourceVersion, 'short'),
    ]) {
      expect(
        () => decode({...avatar, 'thumbnailPath': path}),
        throwsFormatException,
        reason: 'Unverified author-avatar route must fail: $path',
      );
    }
    for (final invalid in <Map<String, Object?>>[
      {...avatar, 'accountId': f.id(2)},
      {...avatar, 'version': 0},
      {...avatar, 'width': 256},
      {...avatar, 'height': 256},
      {...avatar, 'byteCount': 0},
      {...avatar, 'byteCount': personAvatarMaxThumbnailBytes + 1},
      {...avatar, 'sha256': 'short'},
      {...avatar, 'sha256': '${avatar['sha256']}\n'},
      {...avatar, 'handle': 'unsupported'},
    ]) {
      expect(() => decode(invalid), throwsFormatException);
    }
  });

  test(
    'author and original time decode without inventing a publication timestamp',
    () {
      final fixture = ReaderFixture();
      final detail = OwnedMapMomentDetail.decode(
        fixture.detail(1),
        ownerID: fixture.owner,
        now: fixture.now,
      );
      expect(detail.author!.displayName, '林小满');
      expect(detail.publishedAt, isNull);
      expect(detail.createdAtSource, 'CIVU_ORIGINAL');
      expect(detail.displayPublishedAt, DateTime.utc(2026, 1, 1));
      fixture.unknown = true;
      final unknown = OwnedMapMomentDetail.decode(
        fixture.detail(1),
        ownerID: fixture.owner,
        now: fixture.now,
      );
      expect(unknown.author, isNull);
      expect(unknown.displayPublishedAt, isNull);
    },
  );

  testWidgets(
    'author time, stable media frame and same-panel gallery preserve page and permission',
    (t) async {
      final fixture = ReaderFixture()..now = DateTime.now().toUtc();
      var edits = 0;
      final (c, identity) = await mountReader(
        t,
        fixture,
        onEdit: () => edits++,
      );
      expect(find.text('林小满'), findsOneWidget);
      expect(find.text('真实内容只出现一次'), findsOneWidget);
      expect(find.byTooltip('原创建时间'), findsOneWidget);
      expect(find.textContaining('原公开记录'), findsNothing);
      final frame = find.byKey(ValueKey('moment-media-frame:${f.id(1)}:false'));
      expect(t.getSize(frame).width, 360);
      expect(t.getSize(frame).height, closeTo(360 * .75, .1));
      await t.tap(find.byTooltip('编辑动态'));
      expect(edits, 1);
      await t.tap(find.byTooltip('下一张'));
      await t.pump();
      await t.pump(const Duration(milliseconds: 250));
      await t.pump();
      expect(find.text('媒体 2 / 2'), findsOneWidget);
      await t.tap(find.byTooltip('查看完整媒体'));
      await t.pump();
      expect(find.text('照片与视频'), findsOneWidget);
      expect(find.byType(InteractiveViewer), findsOneWidget);
      expect(find.text('媒体 2 / 2'), findsOneWidget);
      await t.tap(find.byTooltip('返回动态'));
      await t.pump();
      expect(find.text('媒体 2 / 2'), findsOneWidget);
      identity.value++;
      await t.pump();
      expect(find.text('林小满'), findsNothing);
      expect(find.text('真实内容只出现一次'), findsNothing);
      expect(find.byType(Image), findsNothing);
      await disposeReader(t, c, identity);
    },
  );

  testWidgets(
    'video only reads poster until explicit play; missing author and date stay honest',
    (t) async {
      final fixture = ReaderFixture()
        ..now = DateTime.now().toUtc()
        ..video = true
        ..unknown = true;
      final (c, identity) = await mountReader(t, fixture);
      await f.drain(
        t,
        () => fixture.requests.any(
          (r) => r.url.queryParameters['variant'] == 'poster',
        ),
      );
      expect(find.byTooltip('播放视频'), findsOneWidget);
      expect(find.text('1:05'), findsOneWidget);
      expect(find.text('作者未提供'), findsOneWidget);
      expect(find.text('时间未提供'), findsOneWidget);
      expect(
        fixture.requests.where(
          (r) =>
              r.url.path.endsWith('/content') &&
              r.url.queryParameters['variant'] != 'poster',
        ),
        isEmpty,
      );
      await disposeReader(t, c, identity);
    },
  );
}
