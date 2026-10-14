import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:birdtie_client/src/workspace/public_business_page.dart';
import 'package:birdtie_client/src/workspace/business_claim_notification_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'inbox_panel_test.dart' show signedInAuth;
import 'remote_business_update_inbox_test.dart'
    show
        businessInboxWire,
        businessReadWire,
        businessPublicWire,
        businessNotice,
        businessResponse;

void main() {
  final capturePath = Platform.environment['BIRDTIE_WIDGET_EVIDENCE_DIR'];
  setUpAll(() async {
    if (capturePath == null) return;
    // Existing Windows font, only for Chinese-readable widget evidence. It is
    // not an APK asset or evidence of the phone's actual platform glyph font.
    final font = await File('C:/Windows/Fonts/msyh.ttc').readAsBytes();
    await (FontLoader(
      'Roboto',
    )..addFont(Future.value(ByteData.sublistView(font)))).load();
    final icons = await File(
      'D:/DevTools/flutter/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf',
    ).readAsBytes();
    await (FontLoader(
      'MaterialIcons',
    )..addFont(Future.value(ByteData.sublistView(icons)))).load();
  });
  for (final brightness in [Brightness.light, Brightness.dark]) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets('原生DIGEST整包在320dp ${brightness.name} $scale读取并进入同一公开商家', (
        t,
      ) async {
        t.view.devicePixelRatio = 1;
        t.view.physicalSize = const Size(320, 800);
        addTearDown(t.view.resetDevicePixelRatio);
        addTearDown(t.view.resetPhysicalSize);
        const fixturePath =
            '../../docs/testing/evidence/unfinished-completion-2026-10-10/digest-backend/native-digest-wire';
        final inboxBytes = File(
          '$fixturePath/inbox-response.json',
        ).readAsBytesSync();
        final readBytes = File(
          '$fixturePath/read-response.json',
        ).readAsBytesSync();
        final publicBytes = File(
          '$fixturePath/public-business-response.json',
        ).readAsBytesSync();
        final notice =
            (jsonDecode(utf8.decode(inboxBytes))['data'] as List).single
                as Map<String, dynamic>;
        final business =
            jsonDecode(utf8.decode(publicBytes))['data']
                as Map<String, dynamic>;
        final auth = await signedInAuth();
        final calls = <String>[];
        final source = RemoteInboxSource(
          authorizationHeader: () => auth.authorizationHeader,
          apiBaseUrl: 'https://native.fixture',
          client: MockClient((request) async {
            calls.add('${request.method} ${request.url.path}');
            if (request.url.path == '/v1/me/inbox') {
              return http.Response.bytes(inboxBytes, 200);
            }
            if (request.url.path == '/v1/me/inbox/${notice['id']}/read') {
              expect(request.method, 'POST');
              return http.Response.bytes(readBytes, 200);
            }
            if (request.url.path ==
                '/v1/businesses/${notice['targetBusinessId']}') {
              expect(request.method, 'GET');
              return http.Response.bytes(publicBytes, 200);
            }
            expect(request.method, 'GET');
            expect(request.url.path.contains('business-console'), isFalse);
            return businessResponse([]);
          }),
        );
        final boundaryKey = GlobalKey();
        await t.pumpWidget(
          RepaintBoundary(
            key: boundaryKey,
            child: MaterialApp(
              debugShowCheckedModeBanner: false,
              theme: birdtieTheme(brightness),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(
                  context,
                ).copyWith(textScaler: TextScaler.linear(scale)),
                child: child!,
              ),
              home: Scaffold(
                body: InboxPanel(auth: auth, source: source),
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(find.text('定时汇总'), findsOneWidget);
        expect(calls.where((call) => call.startsWith('POST')), isEmpty);
        final title = find.text(notice['title'] as String);
        await t.ensureVisible(title);
        await t.pumpAndSettle();
        final tile = find.ancestor(of: title, matching: find.byType(ListTile));
        expect(t.getSize(tile).height, greaterThanOrEqualTo(48));
        expect(title.hitTestable(), findsOneWidget);
        expect(t.takeException(), isNull);
        Future<void> capture(String surface) async {
          if (capturePath == null ||
              !((brightness == Brightness.light && scale == 1) ||
                  (brightness == Brightness.dark && scale == 2))) {
            return;
          }
          final boundary =
              boundaryKey.currentContext!.findRenderObject()!
                  as RenderRepaintBoundary;
          final image = await boundary.toImage(pixelRatio: 2);
          final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
          final output = File(
            '$capturePath/$surface-${brightness.name}-${(scale * 100).round()}.png',
          );
          await output.parent.create(recursive: true);
          await output.writeAsBytes(bytes!.buffer.asUint8List());
          image.dispose();
        }

        await t.runAsync(() => capture('inbox-native-digest'));
        await t.tap(title.hitTestable());
        await t.pumpAndSettle();
        expect(find.byType(PublicBusinessPage), findsOneWidget);
        expect(find.byType(BusinessClaimNotificationPage), findsNothing);
        expect(find.text(business['description'] as String), findsOneWidget);
        expect(calls.where((call) => call.startsWith('POST')).toList(), [
          'POST /v1/me/inbox/${notice['id']}/read',
        ]);
        expect(
          calls,
          contains('GET /v1/businesses/${notice['targetBusinessId']}'),
        );
        expect(
          calls,
          isNot(contains('GET /v1/businesses/${notice['resourceId']}')),
        );
        expect(t.takeException(), isNull);
        await t.runAsync(() => capture('business-native-digest'));
        await t.pumpWidget(const SizedBox());
        source.dispose();
        auth.dispose();
      });
    }
  }
  testWidgets('真实公开更新从原Inbox当前Read打开同一商家公开页不进经营工作台', (t) async {
    await t.binding.setSurfaceSize(const Size(400, 1100));
    addTearDown(() => t.binding.setSurfaceSize(null));
    final auth = await signedInAuth();
    final row = businessNotice();
    final calls = <String>[];
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://local.fixture',
      client: MockClient((r) async {
        calls.add('${r.method} ${r.url.path}');
        if (r.url.path == '/v1/me/inbox') {
          return http.Response.bytes(utf8.encode(businessInboxWire), 200);
        }
        if (r.url.path.endsWith('/read')) {
          expect(r.url.path, '/v1/me/inbox/${row['id']}/read');
          return http.Response.bytes(utf8.encode(businessReadWire), 200);
        }
        expect(r.method, 'GET');
        if (r.url.path == '/v1/businesses/${row['targetBusinessId']}') {
          return http.Response.bytes(utf8.encode(businessPublicWire), 200);
        }
        return businessResponse([]);
      }),
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(auth: auth, source: source),
        ),
      ),
    );
    await t.pumpAndSettle();
    final title = find.text(row['title'] as String);
    expect(title, findsOneWidget);
    expect(calls.where((v) => v.startsWith('POST')), isEmpty);
    await t.ensureVisible(title);
    await t.pumpAndSettle();
    await t.tap(title.hitTestable());
    await t.pumpAndSettle();
    expect(find.byType(PublicBusinessPage), findsOneWidget);
    expect(find.byType(BusinessClaimNotificationPage), findsNothing);
    expect(find.text('本地合成公开商家'), findsOneWidget);
    expect(find.text('明确公开批准的资料'), findsOneWidget);
    expect(calls.where((v) => v.startsWith('POST')).toList(), [
      'POST /v1/me/inbox/${row['id']}/read',
    ]);
    expect(calls.any((v) => v.contains('/business-console')), isFalse);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
  });
  testWidgets('公开来源Read拒绝保留通知与恢复反馈320大字号入口可点', (t) async {
    await t.binding.setSurfaceSize(const Size(320, 1100));
    addTearDown(() => t.binding.setSurfaceSize(null));
    final auth = await signedInAuth();
    final row = businessNotice();
    var reads = 0, publicReads = 0;
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://local.fixture',
      client: MockClient((r) async {
        if (r.url.path == '/v1/me/inbox') {
          return http.Response.bytes(utf8.encode(businessInboxWire), 200);
        }
        if (r.url.path.endsWith('/read')) {
          reads++;
          return http.Response('{}', 404);
        }
        if (r.url.path.startsWith('/v1/businesses/')) {
          publicReads++;
        }
        expect(r.method, 'GET');
        return businessResponse([]);
      }),
    );
    await t.pumpWidget(
      MaterialApp(
        builder: (c, child) => MediaQuery(
          data: MediaQuery.of(
            c,
          ).copyWith(textScaler: const TextScaler.linear(2)),
          child: child!,
        ),
        home: Scaffold(
          body: InboxPanel(auth: auth, source: source),
        ),
      ),
    );
    await t.pumpAndSettle();
    final title = find.text(row['title'] as String);
    await t.ensureVisible(title);
    await t.pumpAndSettle();
    final tile = find.ancestor(of: title, matching: find.byType(ListTile));
    expect(t.getSize(tile).height, greaterThanOrEqualTo(48));
    expect(title.hitTestable(), findsOneWidget);
    await t.tap(title.hitTestable());
    await t.pumpAndSettle();
    expect(reads, 1);
    expect(publicReads, 0);
    expect(find.byType(PublicBusinessPage), findsNothing);
    expect(find.text('当前通知暂不可读取，请刷新后重试。'), findsOneWidget);
    expect(title, findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
  });
  testWidgets('公开详情在组织ABA后的旧响应不回到原通知来源', (t) async {
    await t.binding.setSurfaceSize(const Size(400, 1100));
    addTearDown(() => t.binding.setSurfaceSize(null));
    final auth = await signedInAuth(), workspace = ValueNotifier<String?>(null);
    final row = businessNotice();
    final pending = Completer<http.Response>();
    var publicReads = 0;
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://local.fixture',
      client: MockClient((r) async {
        if (r.url.path == '/v1/me/inbox') {
          return http.Response.bytes(utf8.encode(businessInboxWire), 200);
        }
        if (r.url.path.endsWith('/read')) {
          return http.Response.bytes(utf8.encode(businessReadWire), 200);
        }
        if (r.url.path == '/v1/businesses/${row['targetBusinessId']}') {
          publicReads++;
          return pending.future;
        }
        expect(r.method, 'GET');
        return businessResponse([]);
      }),
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(
            auth: auth,
            source: source,
            workspaceChanges: workspace,
            organizationWorkspaceID: () => workspace.value,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    final title = find.text(row['title'] as String);
    await t.ensureVisible(title);
    await t.pumpAndSettle();
    await t.tap(title.hitTestable());
    await t.pump();
    await t.pump(const Duration(milliseconds: 350));
    expect(publicReads, 1);
    workspace.value = 'organization';
    workspace.value = null;
    await t.pump();
    pending.complete(http.Response.bytes(utf8.encode(businessPublicWire), 200));
    await t.pumpAndSettle();
    expect(find.text('明确公开批准的资料'), findsNothing);
    expect(find.byType(PublicBusinessPage), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
    workspace.dispose();
  });
  testWidgets('公开更新摘要分组保留系统大字并在Read之后打开公开资料', (t) async {
    await t.binding.setSurfaceSize(const Size(320, 800));
    addTearDown(() => t.binding.setSurfaceSize(null));
    final auth = await signedInAuth();
    final row = {
      ...businessNotice(),
      'notificationRoute': 'DIGEST',
      'notificationPriority': 10,
    };
    final paths = <String>[];
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://local.fixture',
      client: MockClient((request) async {
        paths.add('${request.method} ${request.url.path}');
        if (request.url.path == '/v1/me/inbox') return businessResponse([row]);
        if (request.url.path.endsWith('/read')) {
          return businessResponse({
            ...row,
            'readAt': '2026-10-10T14:00:00+08:00',
          });
        }
        if (request.url.path == '/v1/businesses/${row['targetBusinessId']}') {
          return http.Response.bytes(utf8.encode(businessPublicWire), 200);
        }
        expect(request.method, 'GET');
        return businessResponse([]);
      }),
    );
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(2)),
          child: child!,
        ),
        home: Scaffold(
          body: InboxPanel(auth: auth, source: source),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('定时汇总'), findsOneWidget);
    expect(paths.where((path) => path.startsWith('POST')), isEmpty);
    final title = find.text(row['title'] as String);
    await t.ensureVisible(title);
    await t.pumpAndSettle();
    expect(
      t
          .getSize(find.ancestor(of: title, matching: find.byType(ListTile)))
          .height,
      greaterThanOrEqualTo(48),
    );
    await t.tap(title.hitTestable());
    await t.pumpAndSettle();
    expect(find.byType(PublicBusinessPage), findsOneWidget);
    expect(find.byType(BusinessClaimNotificationPage), findsNothing);
    expect(
      paths.indexOf('POST /v1/me/inbox/${row['id']}/read'),
      lessThan(paths.indexOf('GET /v1/businesses/${row['targetBusinessId']}')),
    );
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
  });
  testWidgets('Read等待中更换Inbox来源不打开旧商家并保留新收件箱', (t) async {
    final auth = await signedInAuth();
    final row = businessNotice();
    final delayedRead = Completer<http.Response>();
    var publicReads = 0;
    final oldSource = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://old.fixture',
      client: MockClient((request) async {
        if (request.url.path == '/v1/me/inbox') {
          return http.Response.bytes(utf8.encode(businessInboxWire), 200);
        }
        if (request.url.path.endsWith('/read')) return delayedRead.future;
        if (request.url.path.startsWith('/v1/businesses/')) publicReads++;
        return businessResponse([]);
      }),
    );
    final newSource = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://new.fixture',
      client: MockClient((request) async {
        expect(request.method, 'GET');
        if (request.url.path.startsWith('/v1/businesses/')) publicReads++;
        return businessResponse([]);
      }),
    );
    Widget view(RemoteInboxSource source) => MaterialApp(
      home: Scaffold(
        body: InboxPanel(auth: auth, source: source),
      ),
    );
    await t.pumpWidget(view(oldSource));
    await t.pumpAndSettle();
    final title = find.text(row['title'] as String);
    await t.ensureVisible(title);
    await t.tap(title.hitTestable());
    await t.pump();
    await t.pumpWidget(view(newSource));
    await t.pump();
    delayedRead.complete(
      http.Response.bytes(utf8.encode(businessReadWire), 200),
    );
    await t.pumpAndSettle();
    expect(publicReads, 0);
    expect(find.byType(PublicBusinessPage), findsNothing);
    expect(find.text('暂时没有新动态。活动变更和重要消息会显示在这里。'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    oldSource.dispose();
    newSource.dispose();
    auth.dispose();
  });
}
