import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/community_api.dart';
import 'package:birdtie_client/src/workspace/community_page.dart';
import 'package:birdtie_client/src/workspace/community_avatar_editor.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'chat_entity_router_test.dart' show ChatTestAuth;
import 'person_avatar_fixture.dart';
import 'community_avatar_test.dart';

void main() {
  testWidgets('社区整改后正常字号同数据截图并移除空组', (t) async {
    final loader = FontLoader('AuditCJK')
      ..addFont(
        Future.value(
          ByteData.sublistView(
            File('C:/Windows/Fonts/msyh.ttc').readAsBytesSync(),
          ),
        ),
      );
    await t.runAsync(loader.load);
    final icons = FontLoader('MaterialIcons')
      ..addFont(
        Future.value(
          ByteData.sublistView(
            File(
              'D:/DevTools/flutter/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf',
            ).readAsBytesSync(),
          ),
        ),
      );
    await t.runAsync(icons.load);
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final auth = ChatTestAuth(), city = PublicCityController();
    final client = MockClient(
      (r) async => http.Response.bytes(
        utf8.encode(
          jsonEncode({
            'data': r.url.path.contains('/me/')
                ? []
                : [
                    {
                      'id': '11111111-1111-4111-8111-111111111111',
                      'name': '合成视觉社群',
                      'description': '仅用于隔离视觉验收',
                      'visibility': 'public',
                      'joinPolicy': 'request',
                      'status': 'active',
                      'memberCount': 3,
                    },
                  ],
          }),
        ),
        200,
      ),
    );
    final api = CommunityApi(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'https://fixture.test',
    );
    final boundary = GlobalKey();
    final text = const TextStyle(fontFamily: 'AuditCJK', color: Colors.black87);
    final theme = ThemeData(
      fontFamily: 'AuditCJK',
      listTileTheme: ListTileThemeData(
        titleTextStyle: text.copyWith(fontSize: 16),
        subtitleTextStyle: text.copyWith(fontSize: 14),
      ),
      appBarTheme: AppBarTheme(titleTextStyle: text.copyWith(fontSize: 20)),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(textStyle: text),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(textStyle: text),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(textStyle: text),
      ),
      chipTheme: ChipThemeData(labelStyle: text),
    );
    await t.pumpWidget(
      MaterialApp(
        theme: theme,
        home: RepaintBoundary(
          key: boundary,
          child: Scaffold(
            body: CommunityPage(auth: auth, city: city, api: api),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(t.takeException(), isNull);
    expect(find.text('我管理的'), findsNothing);
    expect(find.text('我加入的'), findsNothing);
    expect(find.text('待处理'), findsNothing);
    expect(find.text('发现社群'), findsOneWidget);
    expect(find.text('合成视觉社群'), findsOneWidget);
    await t.runAsync(() async {
      final image =
          await (boundary.currentContext!.findRenderObject()
                  as RenderRepaintBoundary)
              .toImage(pixelRatio: 1);
      final png = await image.toByteData(format: ui.ImageByteFormat.png);
      await File(
        'D:/Project/birdtie/docs/testing/evidence/now-v4-v41-integration-20261010/continuation20/copy29/community-media/after/widget-normal.png',
      ).writeAsBytes(png!.buffer.asUint8List());
      image.dispose();
    });
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    city.dispose();
    api.dispose();
    client.close();
  });
  testWidgets('创建社群使用本地头像选择且未知头像结果不会重复创建', (t) async {
    var creates = 0, writes = 0;
    final auth = ChatTestAuth(), city = PublicCityController();
    final store = MemoryCommunityAvatarPendingStore();
    final item = {
      'id': avatarOwner,
      'name': '合成视觉社群',
      'description': '仅用于隔离视觉验收',
      'visibility': 'public',
      'joinPolicy': 'request',
      'status': 'active',
      'memberCount': 1,
      'myRole': 'owner',
      'myStatus': 'active',
    };
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/avatar/preview'))
        return avatarWire(
          communityAvatarApproval(jsonDecode(r.body) as Map<String, dynamic>),
        );
      if (r.url.path.endsWith('/avatar')) {
        if (r.method == 'PUT') {
          writes++;
          return avatarWire(null, status: 503);
        }
        return avatarWire({'version': 0, 'avatar': null});
      }
      if (r.method == 'POST') {
        creates++;
        final body = jsonDecode(r.body) as Map;
        expect(body['avatarUrl'], anyOf(isNull, ''));
        return avatarWire(item, status: 201);
      }
      return avatarWire(
        r.url.path.contains('/me/') ? (creates == 0 ? [] : [item]) : [item],
      );
    });
    addTearDown(client.close);
    addTearDown(auth.dispose);
    addTearDown(city.dispose);
    final api = CommunityApi(
      authorizationHeader: () => auth.authorizationHeader,
      client: client,
      apiBaseUrl: 'https://fixture.test',
    );
    addTearDown(api.dispose);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CommunityPage(
            auth: auth,
            city: city,
            api: api,
            avatarPicker: () async => avatarJPEG(),
            avatarPendingStore: store,
          ),
        ),
      ),
    );
    await settleCommunityAvatar(t);
    await t.tap(find.text('创建社群'));
    await t.pumpAndSettle();
    expect(find.text('头像 URL'), findsNothing);
    expect(find.text('选择头像（选填）'), findsOneWidget);
    await t.enterText(find.byType(TextField).first, '合成视觉社群');
    await t.runAsync(() async {
      await t.tap(find.byKey(const Key('community-create-avatar-select')));
      await Future<void>.delayed(const Duration(milliseconds: 120));
    });
    await t.pumpAndSettle();
    expect(creates, 0);
    expect(writes, 0);
    await t.tap(find.widgetWithText(FilledButton, '创建'));
    await settleCommunityAvatar(t);
    expect(creates, 1);
    expect(find.byType(CommunityAvatarEditor), findsOneWidget);
    await t.tap(find.byKey(const Key('community-avatar-save')));
    await settleCommunityAvatar(t);
    await t.tap(find.text('确认保存'));
    await settleCommunityAvatar(t);
    expect(writes, 1);
    expect(creates, 1);
    expect(find.text('保存结果待核实，请先核实原操作。'), findsOneWidget);
    expect(
      await store.read('https://fixture.test', auth.accountID!, avatarOwner),
      isNotNull,
    );
    expect(t.takeException(), isNull);
  });
}
