import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/legacy/legacy_shell.dart';
import 'package:birdtie_client/src/workspace/now_moment_editor_host.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/public_moment_photo_editor.dart';
import 'package:birdtie_client/src/workspace/public_moment_photo_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'entity_cover_test.dart' show coverSettle;
import 'public_moment_api_test.dart' show publicMomentV3Data;
import 'public_moment_photo_test.dart'
    show photoMoment, photoOwner, photoState, photoApproval, photoInput;
import 'public_moment_photo_controller_test.dart' show PhotoFixture;
import 'public_moment_photo_pending_store_test.dart' show photoPending;

const _draftMoment = '74100000-0000-4000-8000-000000000004';
const _placeID = '22222222-2222-4222-8222-222222222222';

class _ProfileAuth extends BirdtieAuthController {
  _ProfileAuth(this.owner, this.token);
  final String Function() owner, token;
  @override
  String get accountID => owner();
  @override
  String get authorizationHeader => token();
  @override
  bool get signedIn => true;
  @override
  String get displayName => '隔离本人列表';
}

class _ProfileMoments extends PrivateMomentController {
  _ProfileMoments({
    required super.authorizationHeader,
    required super.ownerID,
    required super.client,
    required super.apiBaseUrl,
  });
  @override
  Future<void> enableHistory({bool Function()? current}) async {}
  @override
  List<PrivateMoment> get moments => const [
    PrivateMoment(
      id: _draftMoment,
      authorAccountID: photoOwner,
      cityID: '',
      placeID: '',
      title: '',
      body: '原私人草稿',
      timePrecision: 'unknown',
      locationPrecision: 'none',
      status: 'draft',
      visibility: 'private',
      revision: 2,
      activityIDs: [],
      communityID: '',
      organizationID: '',
    ),
    PrivateMoment(
      id: photoMoment,
      authorAccountID: photoOwner,
      cityID: '',
      placeID: '',
      title: '',
      body: '原公开动态',
      timePrecision: 'unknown',
      locationPrecision: 'none',
      status: 'published',
      visibility: 'public',
      revision: 2,
      activityIDs: [],
      communityID: '',
      organizationID: '',
    ),
  ];
}

void main() {
  testWidgets('所有详情与本人列表编辑回原Now且旧图片仅核实原操作', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final navigator = GlobalKey<NavigatorState>();
    final changes = ValueNotifier(0);
    final composer = TextEditingController(text: '保留原输入和地图选择');
    var owner = photoOwner, token = 'Bearer synthetic-now';
    var origin = 'https://now-editor.example';
    String? workspace;
    var writes = 0, opened = 0, created = 0;
    var legacyPublicationCalls = 0;
    var recoveryMode = false;
    final recovery = PhotoFixture()
      ..header = token
      ..version = 1
      ..missingReceipt = true
      ..approved = photoApproval(photoInput().body());
    var canOpen = true;
    String? edited;
    Completer<void>? opening;
    late NowMomentEditorController capability;
    late StateSetter updateNow;
    final client = MockClient((request) async {
      if (request.method != 'GET') writes++;
      if (request.url.path.endsWith('/publication')) legacyPublicationCalls++;
      if (recoveryMode && request.url.path.contains('/public-photo')) {
        return recovery.request(request);
      }
      Object? data;
      if (request.url.path == '/v1/moments/$photoMoment') {
        data = {...publicMomentV3Data(photo: false), 'id': photoMoment};
      } else if (request.url.path.endsWith('/public-photo')) {
        data = photoState();
      } else if (request.url.path == '/v1/places/$_placeID') {
        data = {
          'id': _placeID,
          'name': '入口地点',
          'source': {'label': '本地合成来源'},
          'location': {'precision': 'none'},
        };
      } else if (request.url.path == '/v1/me/moments') {
        data = [
          for (final item in [
            (_draftMoment, 'draft', 'private'),
            (photoMoment, 'published', 'public'),
          ])
            {
              'id': item.$1,
              'authorAccountId': photoOwner,
              'placeId': _placeID,
              'title': '',
              'body': '地点原记录 ${item.$2}',
              'revision': 2,
              'status': item.$2,
              'visibility': item.$3,
              'locationPrecision': 'place',
            },
        ];
      } else if (request.url.path.endsWith('/activities')) {
        data = <Object>[];
      }
      return data == null
          ? http.Response('{}', 404)
          : http.Response.bytes(utf8.encode(jsonEncode({'data': data})), 200);
    });
    final profileAuth = _ProfileAuth(() => owner, () => token);
    final profileCity = PublicCityController();
    final profileMoments = _ProfileMoments(
      authorizationHeader: () => token,
      ownerID: () => owner,
      client: client,
      apiBaseUrl: origin,
    );
    Widget profile() => Scaffold(
      body: LegacyProfilePage(
        auth: profileAuth,
        city: profileCity,
        moments: profileMoments,
        client: client,
        apiBaseUrl: origin,
        workspaceChanges: changes,
        organizationWorkspaceID: () => workspace,
      ),
    );
    await t.pumpWidget(
      NowMomentEditorScope(
        child: MaterialApp(
          navigatorKey: navigator,
          home: StatefulBuilder(
            builder: (context, setState) {
              updateNow = setState;
              capability = NowMomentEditorScope.maybeOf(context)!;
              return NowMomentEditorHost(
                ownerID: () => owner,
                authorizationHeader: () => token,
                apiBaseUrl: () => origin,
                workspaceID: () => workspace,
                identityChanges: changes,
                canOpen: () => workspace == null && canOpen,
                onCreate: (current) async {
                  if (!current()) return;
                  updateNow(() => created++);
                },
                onOpen: (id, current) async {
                  if (opening case final pending?) await pending.future;
                  if (!current()) return;
                  updateNow(() {
                    edited = id;
                    opened++;
                  });
                },
                child: Scaffold(
                  body: Column(
                    key: const ValueKey('original-now-workspace'),
                    children: [
                      const Text('原地图与面板'),
                      TextField(controller: composer),
                      if (edited != null) Text('Now编辑 $edited'),
                    ],
                  ),
                ),
              );
            },
          ),
        ),
      ),
    );
    final original = t.element(
      find.byKey(const ValueKey('original-now-workspace')),
    );
    for (final label in ['修改或删除这条动态', '管理本人图片或视频', '管理本人公开图片']) {
      unawaited(
        navigator.currentState!.push<void>(
          MaterialPageRoute(
            builder: (_) => const Scaffold(body: Text('历史与聊天入口')),
          ),
        ),
      );
      await t.pumpAndSettle();
      unawaited(
        navigator.currentState!.push<void>(
          MaterialPageRoute(
            builder: (_) => PublicMomentPage(
              momentID: photoMoment,
              ownerID: () => owner,
              authorizationHeader: () => token,
              workspaceID: () => workspace,
              identityChanges: changes,
              apiBaseUrl: origin,
              client: client,
            ),
          ),
        ),
      );
      await coverSettle(t);
      await t.ensureVisible(find.text(label));
      await t.tap(find.text(label));
      await coverSettle(t);
      expect(navigator.currentState!.canPop(), isFalse);
      expect(find.byType(PublicMomentPage), findsNothing);
      expect(edited, photoMoment);
      expect(
        identical(
          t.element(find.byKey(const ValueKey('original-now-workspace'))),
          original,
        ),
        isTrue,
      );
      expect(composer.text, '保留原输入和地图选择');
      expect(writes, 0);
    }
    expect(opened, 3);
    for (final label in ['新建草稿', '管理本人公开图片', '编辑或撤回', '修改或删除']) {
      unawaited(
        navigator.currentState!.push<void>(
          MaterialPageRoute(builder: (_) => profile()),
        ),
      );
      await coverSettle(t);
      await t.ensureVisible(find.text(label));
      await t.tap(find.text(label));
      await coverSettle(t);
      expect(navigator.currentState!.canPop(), isFalse);
      expect(find.byType(LegacyProfilePage), findsNothing);
      expect(
        identical(
          t.element(find.byKey(const ValueKey('original-now-workspace'))),
          original,
        ),
        isTrue,
      );
      expect(composer.text, '保留原输入和地图选择');
      if (label != '新建草稿') {
        expect(edited, label == '编辑或撤回' ? _draftMoment : photoMoment);
      }
      expect(writes, 0);
    }
    expect(created, 1);
    expect(opened, 6);

    Widget place() => Scaffold(
      body: PlaceDetailSheet(
        placeID: _placeID,
        ownerID: () => owner,
        authorizationHeader: () => token,
        organizationWorkspaceID: () => workspace,
        workspaceChanges: changes,
        onOpenActivity: (_) {},
        apiBaseUrl: origin,
        client: client,
      ),
    );
    for (final item in [('编辑并检查公开', _draftMoment), ('修改或撤回', photoMoment)]) {
      unawaited(
        navigator.currentState!.push<void>(
          MaterialPageRoute(builder: (_) => place()),
        ),
      );
      await coverSettle(t);
      final button = find.widgetWithText(TextButton, item.$1);
      await t.ensureVisible(button);
      await t.pumpAndSettle();
      await t.tap(button.hitTestable());
      await coverSettle(t);
      expect(navigator.currentState!.canPop(), isFalse);
      expect(find.byType(PlaceDetailSheet), findsNothing);
      expect(edited, item.$2);
      expect(
        identical(
          t.element(find.byKey(const ValueKey('original-now-workspace'))),
          original,
        ),
        isTrue,
      );
      expect(composer.text, '保留原输入和地图选择');
      expect(writes, 0);
      expect(legacyPublicationCalls, 0);
    }
    expect(opened, 8);
    final completedOpens = opened;

    unawaited(
      navigator.currentState!.push<void>(
        MaterialPageRoute(
          builder: (_) => const Scaffold(body: Text('仍在原详情入口')),
        ),
      ),
    );
    await t.pumpAndSettle();
    var sourceCurrent = true;
    final request = NowMomentEditorRequest(
      momentID: photoMoment,
      ownerID: owner,
      authorizationHeader: token,
      apiBaseUrl: origin,
      sourceCurrent: () => sourceCurrent,
    );
    Future<void> rejected() async {
      changes.value++;
      expect(await capability.open(request), isFalse);
      expect(navigator.currentState!.canPop(), isTrue);
      expect(opened, completedOpens);
    }

    owner = '74100000-0000-4000-8000-000000000099';
    await rejected();
    owner = photoOwner;
    origin = 'https://other-origin.example';
    await rejected();
    origin = request.apiBaseUrl;
    token = 'Bearer changed-session';
    await rejected();
    token = request.authorizationHeader;
    workspace = photoMoment;
    await rejected();
    workspace = null;
    canOpen = false;
    await rejected();
    canOpen = true;
    sourceCurrent = false;
    await rejected();

    // Source handoff is read-only. An owner ABA while the original Now loader
    // awaits may not populate its editor with a stale identity's response.
    sourceCurrent = true;
    opening = Completer<void>();
    final pending = capability.open(request);
    owner = '74100000-0000-4000-8000-000000000099';
    changes.value++;
    owner = photoOwner;
    changes.value++;
    opening.complete();
    expect(await pending, isFalse);
    await t.pumpAndSettle();
    expect(opened, completedOpens);
    expect(writes, 0);

    // A stand-alone detail without a Now host offers no old writer fallback.
    await t.pumpWidget(
      MaterialApp(
        home: PublicMomentPage(
          momentID: photoMoment,
          ownerID: () => owner,
          authorizationHeader: () => token,
          apiBaseUrl: origin,
          client: client,
        ),
      ),
    );
    await coverSettle(t);
    final editButton = t.widget<TextButton>(
      find.widgetWithText(TextButton, '修改或删除这条动态'),
    );
    expect(editButton.onPressed, isNull);
    expect(find.text('请回到 Now 首页编辑这条动态。'), findsOneWidget);
    expect(writes, 0);
    await t.pumpWidget(MaterialApp(home: profile()));
    await coverSettle(t);
    expect(
      t.widget<TextButton>(find.widgetWithText(TextButton, '新建草稿')).onPressed,
      isNull,
    );
    expect(
      t
          .widget<TextButton>(find.widgetWithText(TextButton, '管理本人公开图片'))
          .onPressed,
      isNull,
    );
    expect(find.text('请回到 Now 首页新建或编辑动态。'), findsOneWidget);
    for (final label in ['编辑或撤回', '修改或删除']) {
      expect(
        t.widget<TextButton>(find.widgetWithText(TextButton, label)).onPressed,
        isNull,
      );
    }
    expect(writes, 0);
    await t.pumpWidget(MaterialApp(home: place()));
    await coverSettle(t);
    for (final label in ['编辑并检查公开', '修改或撤回']) {
      expect(
        t.widget<TextButton>(find.widgetWithText(TextButton, label)).onPressed,
        isNull,
      );
    }
    expect(find.text('请回到 Now 首页编辑或撤回动态。'), findsOneWidget);

    // The only remaining production photo-editor route recovers its original
    // journal, even without a Now host. It must never become a new writer.
    await t.pumpWidget(const SizedBox.shrink());
    recoveryMode = true;
    const store = SecurePublicMomentPhotoPendingStore();
    final oldPhoto = photoPending();
    await t.runAsync(() async {
      await store.write(origin, owner, oldPhoto);
      await store.write(origin, owner, oldPhoto.unknown);
    });
    await t.pumpWidget(MaterialApp(home: profile()));
    await coverSettle(t);
    await t.ensureVisible(find.text('核实原图片操作 1'));
    await t.tap(find.text('核实原图片操作 1'));
    await coverSettle(t);
    final recoveryEditor = t.widget<PublicMomentPhotoEditor>(
      find.byType(PublicMomentPhotoEditor),
    );
    expect(recoveryEditor.recoveryOnly, isTrue);
    expect(recoveryEditor.controller.state?.canSave, isTrue);
    expect(recoveryEditor.controller.state?.canDelete, isTrue);
    void noNewPhotoWriter() {
      expect(find.byKey(const Key('public-photo-select')), findsNothing);
      expect(find.byKey(const Key('public-photo-preview')), findsNothing);
      expect(find.text('检查移除当前公开图片'), findsNothing);
      expect(recovery.previews, 0);
      expect(recovery.puts, 0);
    }

    noNewPhotoWriter();
    await t.ensureVisible(find.text('核实原结果'));
    await t.tap(find.text('核实原结果'));
    await coverSettle(t);
    expect(recovery.originalReads, 1);
    await t.ensureVisible(find.widgetWithText(TextButton, '停止原操作'));
    await t.tap(find.widgetWithText(TextButton, '停止原操作'));
    await coverSettle(t);
    await t.tap(find.widgetWithText(FilledButton, '停止原操作'));
    await coverSettle(t);
    expect(recovery.stops, 1);
    expect(recoveryEditor.controller.receipt?.status, 'STOPPED');
    expect(await store.read(origin, owner), isEmpty);
    noNewPhotoWriter();
    expect(writes, 1); // Explicit bodyless STOP of the original operation only.
    expect(legacyPublicationCalls, 0);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
    profileMoments.dispose();
    profileAuth.dispose();
    profileCity.dispose();
    changes.dispose();
    composer.dispose();
    client.close();
    recovery.dispose();
  });
}
