import 'dart:async';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'public_moment_api_test.dart';
import 'public_moment_photo_test.dart'
    show photoMetadata, photoDecodeVisible, photoMoment, photoOwner, photoState;
import 'entity_cover_test.dart' show coverJPEG, coverSettle;
import 'package:birdtie_client/src/workspace/public_moment_photo.dart';
import 'entity_share_pending_store_test.dart' show shareTarget;

void main() {
  testWidgets(
    'public hidden author remains independent from actual owner photo control and captured actor ABA',
    (t) async {
      FlutterSecureStorage.setMockInitialValues({});
      final owner = ValueNotifier(photoOwner);
      var stateReads = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/public-photo')) {
          stateReads++;
          return momentReply(photoState());
        }
        return momentReply({
          ...publicMomentV3Data(photo: false),
          'id': photoMoment,
        });
      });
      await t.pumpWidget(
        MaterialApp(
          home: PublicMomentPage(
            momentID: photoMoment,
            authorizationHeader: () => 'Bearer isolated-A',
            ownerID: () => owner.value,
            identityChanges: owner,
            onOpenAuthor: (_) {},
            client: client,
            apiBaseUrl: 'https://api.example',
          ),
        ),
      );
      await coverSettle(t);
      expect(find.text('查看作者主页'), findsNothing);
      expect(find.text('管理本人公开图片'), findsOneWidget);
      final old = t
          .widget<TextButton>(find.widgetWithText(TextButton, '管理本人公开图片'))
          .onPressed!;
      owner.value = photoMoment;
      owner.value = photoOwner;
      await coverSettle(t);
      final after = stateReads;
      old();
      await coverSettle(t);
      expect(stateReads, after);
      expect(find.text('公开动态图片'), findsNothing);
      await t.ensureVisible(find.text('管理本人公开图片'));
      await t.tap(find.text('管理本人公开图片'));
      await coverSettle(t);
      expect(find.text('公开动态图片'), findsOneWidget);
      expect(find.text('重新选择本机图片'), findsOneWidget);
      expect(find.byType(Image), findsNothing);
      await t.pumpWidget(const SizedBox());
      owner.dispose();
    },
  );
  testWidgets(
    'public v3 detail actual current JPEG full frame version replacement and parent lease retirement',
    (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final identity = ValueNotifier(0);
      String? token;
      var revision = 2;
      final contentPaths = <String>[];
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/content')) {
          contentPaths.add(r.url.path);
          return http.Response.bytes(coverJPEG(), 200);
        }
        final at = DateTime.now().toUtc();
        return momentReply({
          ...publicMomentV3Data(
            revision: revision,
            lease: const Duration(seconds: 5),
          ),
          'photo': photoMetadata(
            id: shareTarget,
            revision: revision,
            version: revision - 1,
            at: at,
            lease: const Duration(seconds: 4),
          ),
        });
      });
      await t.pumpWidget(
        MaterialApp(
          home: PublicMomentPage(
            momentID: shareTarget,
            authorizationHeader: () => token,
            identityChanges: identity,
            client: client,
            apiBaseUrl: 'https://api.example',
          ),
        ),
      );
      await photoDecodeVisible(t);
      expect(
        contentPaths.last,
        publicMomentPhotoPath(
          shareTarget,
          2,
          '74100000-0000-4000-8000-000000000003',
          1,
        ),
      );
      revision = 3;
      token = 'Bearer new-isolated';
      identity.value++;
      await t.pump();
      expect(find.byType(RawImage), findsNothing);
      await photoDecodeVisible(t);
      expect(
        contentPaths.last,
        publicMomentPhotoPath(
          shareTarget,
          3,
          '74100000-0000-4000-8000-000000000003',
          2,
        ),
      );
      await t.pump(const Duration(seconds: 6));
      await t.pump();
      expect(find.byType(RawImage), findsNothing);
      expect(find.text('本人明确公开的合成动态'), findsNothing);
      expect(find.textContaining('当前动态资料已到期'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      identity.dispose();
    },
  );
  testWidgets(
    'public v3 detail late old pixels cannot appear after actor workspace ABA',
    (t) async {
      final identity = ValueNotifier('Bearer A');
      final old = Completer<http.Response>();
      var pixelReads = 0, reads = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/content')) {
          if (++pixelReads == 1) return old.future;
          return http.Response.bytes(coverJPEG(), 200);
        }
        reads++;
        return momentReply(
          reads == 1 ? publicMomentV3Data() : publicMomentV3Data(photo: false),
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: PublicMomentPage(
            momentID: shareTarget,
            authorizationHeader: () => identity.value,
            identityChanges: identity,
            client: client,
            apiBaseUrl: 'https://api.example',
          ),
        ),
      );
      await coverSettle(t);
      expect(pixelReads, 1);
      identity.value = 'Bearer B';
      identity.value = 'Bearer A';
      await coverSettle(t);
      old.complete(http.Response.bytes(coverJPEG(), 200));
      await coverSettle(t);
      expect(find.byType(RawImage), findsNothing);
      expect(find.text('合成测试图片 · 非真实发布'), findsNothing);
      await t.pumpWidget(const SizedBox());
      identity.dispose();
    },
  );
  for (final mode in ['account', 'workspace']) {
    testWidgets('公开动态$mode ABA丢弃旧响应', (t) async {
      final identity = ValueNotifier<String?>('Bearer A'),
          workspace = ValueNotifier<String?>(null);
      final old = Completer<http.Response>();
      var reads = 0;
      final client = MockClient(
        (r) async => ++reads == 1
            ? old.future
            : momentReply(publicMomentData(title: '当前动态')),
      );
      await t.pumpWidget(
        MaterialApp(
          home: PublicMomentPage(
            momentID: shareTarget,
            authorizationHeader: () => identity.value,
            workspaceID: () => workspace.value,
            identityChanges: Listenable.merge([identity, workspace]),
            client: client,
            apiBaseUrl: 'https://api.example',
          ),
        ),
      );
      if (mode == 'account') {
        identity.value = 'Bearer B';
        identity.value = 'Bearer A';
      } else {
        workspace.value = 'organization';
        workspace.value = null;
      }
      await t.pumpAndSettle();
      old.complete(momentReply(publicMomentData(title: '旧身份动态')));
      await t.pumpAndSettle();
      expect(find.text('旧身份动态'), findsNothing);
      expect(find.text('当前动态'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      identity.dispose();
      workspace.dispose();
    });
  }
  testWidgets('公开动态中文、小屏大字、稳定地点入口及撤回重试', (t) async {
    var reads = 0;
    String? opened;
    final client = MockClient(
      (_) async => ++reads == 1
          ? http.Response('{}', 404)
          : momentReply(publicMomentData()),
    );
    await t.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(360, 800),
            textScaler: TextScaler.linear(2),
          ),
          child: PublicMomentPage(
            momentID: shareTarget,
            authorizationHeader: () => null,
            apiBaseUrl: 'https://api.example',
            client: client,
            onOpenPlace: (id) => opened = id,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('重新核实'));
    await t.pumpAndSettle();
    expect(find.textContaining('不是到访'), findsNothing);
    expect(find.textContaining('不代表已核验'), findsOneWidget);
    await t.ensureVisible(find.text('查看地点'));
    await t.tap(find.text('查看地点'));
    expect(opened, isNotNull);
    expect(t.takeException(), isNull);
  });
}
