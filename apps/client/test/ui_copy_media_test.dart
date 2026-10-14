import 'dart:async';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/agent_entity_result_card.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/person_avatar.dart';
import 'package:birdtie_client/src/workspace/community_avatar.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_result_projection_test.dart' show typedItem;
import 'inbox_panel_test.dart' show signedInAuth, noticePage, pageNotice;
import 'person_avatar_fixture.dart';
import 'community_avatar_test.dart' show communityAvatarMetadata;
import 'saved_items_test.dart' show MemorySavedNoteStore, savedTestRow;

Future<void> _settleMedia(WidgetTester t) async {
  await t.runAsync(() async {
    await Future<void>.delayed(const Duration(milliseconds: 50));
  });
  await t.pumpAndSettle();
}

void main() {
  testWidgets('communityProjection 结果受控社群缩略图实际Image并随原epoch退休', (t) async {
    var epoch = 0;
    var token = 'Bearer A';
    final changes = ValueNotifier<int>(0);
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      return http.Response.bytes(avatarJPEG(), 200);
    });
    addTearDown(changes.dispose);
    addTearDown(client.close);
    final item = AgentResultItem.decode({
      ...typedItem('community', avatarOwner),
      'title': '合成社群 · 隔离测试非真人',
      'communityAvatar': communityAvatarMetadata(),
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: item,
            authorizationHeader: () => token,
            mediaBaseUrl: 'https://native.test',
            mediaClient: client,
            sourceChanges: changes,
            sourceCurrent: () => epoch == 0,
            mediaCurrent: () => epoch == 0,
          ),
        ),
      ),
    );
    await _settleMedia(t);
    expect(find.byType(CommunityAvatar), findsOneWidget);
    expect(find.byType(PersonAvatar), findsNothing);
    expect(find.byType(Image), findsOneWidget);
    expect(
      requests.single.url.path,
      '/v1/communities/$avatarOwner/avatar/1/content',
    );
    expect(requests.single.headers['Authorization'], 'Bearer A');
    epoch++;
    token = 'Bearer B';
    changes.value++;
    await t.pump();
    expect(find.byType(Image), findsNothing);
    epoch++;
    token = 'Bearer A';
    changes.value++;
    await _settleMedia(t);
    expect(find.byType(Image), findsNothing);
    expect(requests, hasLength(1));
    expect(find.text('合成社群 · 隔离测试非真人'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('communityProjection 迟到社群像素不附到退休结果', (t) async {
    final response = Completer<http.Response>();
    final changes = ValueNotifier<int>(0);
    var current = true;
    final client = MockClient((_) => response.future);
    addTearDown(changes.dispose);
    addTearDown(client.close);
    final item = AgentResultItem.decode({
      ...typedItem('community', avatarOwner),
      'communityAvatar': communityAvatarMetadata(),
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: item,
            authorizationHeader: () => current ? 'Bearer A' : null,
            mediaBaseUrl: 'https://native.test',
            mediaClient: client,
            sourceChanges: changes,
            sourceCurrent: () => current,
          ),
        ),
      ),
    );
    await t.pump();
    current = false;
    changes.value++;
    await t.pump();
    response.complete(http.Response.bytes(avatarJPEG(), 200));
    await _settleMedia(t);
    expect(find.byType(Image), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('communityProjection 收藏group真实Image/新版本重读，来源隐藏零图仍保留本人备注', (
    t,
  ) async {
    var version = 1;
    var visible = true;
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.url.path.endsWith('/content'))
        return http.Response.bytes(avatarJPEG(), 200);
      return avatarWire({
        'items': [
          {
            ...savedTestRow(
              avatarOperation,
              'group',
              avatarOwner,
              title: visible ? '合成社群 · 非真人测试' : '',
              city: visible ? 'aberdeen-gb' : '',
            ),
            'summary': visible ? '真实原社群摘要' : '',
            'available': visible,
            'note': '本人原备注',
            if (visible)
              'communityAvatar': communityAvatarMetadata(version: version),
          },
        ],
        'nextCursor': null,
      });
    });
    final saved = SavedController(
      authorizationHeader: () => 'Bearer A',
      ownerID: () => avatarPeer,
      notePendingStore: MemorySavedNoteStore(),
      client: client,
      apiBaseUrl: 'https://native.test',
    );
    addTearDown(saved.dispose);
    addTearDown(client.close);
    String? opened;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SavedPage(saved: saved, onOpenItem: (i) => opened = i.targetId),
        ),
      ),
    );
    await t.pumpAndSettle();
    await _settleMedia(t);
    expect(find.byType(Image), findsOneWidget);
    expect(find.byType(PersonAvatar), findsNothing);
    await t.tap(find.text('合成社群 · 非真人测试'));
    expect(opened, avatarOwner);
    version = 2;
    await saved.load(paged: true);
    await t.pumpAndSettle();
    await _settleMedia(t);
    expect(find.byType(Image), findsOneWidget);
    expect(
      requests.where((r) => r.url.path.endsWith('/avatar/2/content')),
      hasLength(1),
    );
    visible = false;
    await saved.load(paged: true);
    await t.pumpAndSettle();
    expect(find.byType(Image), findsNothing);
    expect(find.text('内容暂不可用'), findsOneWidget);
    expect(find.text('本人备注：本人原备注'), findsOneWidget);
    expect(requests.where((r) => r.method != 'GET'), isEmpty);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('communityProjection 收藏挂起头像跨owner ABA退休，借用client不关闭', (t) async {
    final response = Completer<http.Response>();
    var owner = avatarPeer;
    var contentReads = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/content')) {
        contentReads++;
        return response.future;
      }
      return avatarWire({
        'items': [
          {
            ...savedTestRow(avatarOperation, 'group', avatarOwner),
            'communityAvatar': communityAvatarMetadata(),
          },
        ],
        'nextCursor': null,
      });
    });
    final saved = SavedController(
      authorizationHeader: () => 'Bearer A',
      ownerID: () => owner,
      notePendingStore: MemorySavedNoteStore(),
      client: client,
      apiBaseUrl: 'https://native.test',
    );
    addTearDown(saved.dispose);
    addTearDown(client.close);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(body: SavedPage(saved: saved)),
      ),
    );
    await t.pumpAndSettle();
    expect(contentReads, 1);
    owner = avatarOwner;
    saved.clear();
    await t.pump();
    owner = avatarPeer;
    saved.clear();
    await t.pump();
    response.complete(http.Response.bytes(avatarJPEG(), 200));
    await _settleMedia(t);
    expect(find.byType(Image), findsNothing);
    expect(saved.items, isEmpty);
    await t.pumpWidget(const SizedBox());
    saved.dispose();
    // A borrowed transport is still usable after controller retirement.
    expect(
      (await client.get(
        Uri.parse('https://native.test/still-open'),
      )).statusCode,
      200,
    );
    expect(t.takeException(), isNull);
  });
  test('媒体协议只接受原人物头像及原通知actor，旧无媒体字段兼容', () {
    final person = typedItem('person', avatarOwner)
      ..['avatar'] = avatarMetadata();
    expect(AgentResultItem.decode(person).avatar?.accountID, avatarOwner);
    expect(
      () => AgentResultItem.decode({
        ...person,
        'avatar': avatarMetadata(owner: avatarPeer),
      }),
      throwsFormatException,
    );
    expect(
      () => AgentResultItem.decode({
        ...typedItem('place', avatarOwner),
        'avatar': avatarMetadata(),
      }),
      throwsFormatException,
    );
    expect(
      AgentResultItem.decode(typedItem('place', avatarPeer)).avatar,
      isNull,
    );
    final notice = {
      ...pageNotice(1),
      'resourceType': 'connection_request',
      'actorId': avatarOwner,
      'actorAvatar': avatarMetadata(),
    };
    expect(InboxItem.fromJson(notice).actorAvatar?.accountID, avatarOwner);
    for (final invalid in [
      {...notice, 'actorId': avatarPeer},
      {...notice, 'actorAvatar': null},
      {...notice, 'resourceType': 'community'},
      {
        ...notice,
        'actorAvatar': {
          ...avatarMetadata(),
          'thumbnailPath': 'https://other.invalid/photo',
        },
      },
    ]) {
      expect(() => InboxItem.fromJson(invalid), throwsFormatException);
    }
    expect(InboxItem.fromJson(pageNotice(1)).actorAvatar, isNull);
  });

  testWidgets('人物结果加载获准缩略图，原来源退休立刻恢复无图布局', (t) async {
    var current = true;
    final changes = ValueNotifier<int>(0);
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      return requests.length == 1
          ? http.Response.bytes(avatarJPEG(), 200)
          : http.Response('', 404);
    });
    addTearDown(changes.dispose);
    addTearDown(client.close);
    final item = AgentResultItem.decode({
      ...typedItem('person', avatarOwner),
      'avatar': avatarMetadata(),
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: item,
            authorizationHeader: () => 'Bearer same-person',
            mediaBaseUrl: 'https://native.test',
            mediaClient: client,
            sourceChanges: changes,
            sourceCurrent: () => current,
            mediaCurrent: () => current,
          ),
        ),
      ),
    );
    await _settleMedia(t);
    expect(find.byType(PersonAvatar), findsOneWidget);
    expect(find.byType(Image), findsOneWidget);
    expect(
      requests.single.url.path,
      '/v1/accounts/$avatarOwner/avatar/1/content',
    );
    expect(requests.single.headers['Authorization'], 'Bearer same-person');
    expect(find.textContaining('未显示地图标记'), findsNothing);
    current = false;
    changes.value++;
    await t.pump();
    expect(find.byType(Image), findsNothing);
    expect(find.text('原实体中文标题'), findsOneWidget);
    current = true;
    changes.value++;
    await _settleMedia(t);
    expect(requests, hasLength(2));
    expect(find.byType(Image), findsNothing);
    expect(find.text('原实体中文标题'), findsOneWidget);
  });

  testWidgets('迟到的结果头像响应不能附到退休身份，失败不造假照片', (t) async {
    final response = Completer<http.Response>();
    final changes = ValueNotifier<int>(0);
    var current = true;
    final client = MockClient((_) => response.future);
    addTearDown(changes.dispose);
    addTearDown(client.close);
    final item = AgentResultItem.decode({
      ...typedItem('person', avatarOwner),
      'avatar': avatarMetadata(),
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: item,
            authorizationHeader: () => current ? 'Bearer A' : null,
            mediaBaseUrl: 'https://native.test',
            mediaClient: client,
            sourceChanges: changes,
            mediaCurrent: () => current,
          ),
        ),
      ),
    );
    await t.pump();
    current = false;
    changes.value++;
    await t.pump();
    response.complete(http.Response.bytes(avatarJPEG(), 200));
    await _settleMedia(t);
    expect(find.byType(Image), findsNothing);
    expect(find.text('原实体中文标题'), findsOneWidget);
    expect(t.takeException(), isNull);
  });

  testWidgets('机会卡按原活动引用呈现真实时间地点，保留私密和分享边界', (t) async {
    const activityID = avatarPeer;
    const item = AgentResultItem(
      entity: AgentResultRef(type: 'opportunity', id: 'own-intent:$activityID'),
      title: '明确原活动',
      summary: '本人活动机会',
      scope: 'SELF_PRIVATE',
      detail: AgentResultRef(type: 'activity', id: activityID),
      share: AgentResultRef(type: 'activity', id: activityID),
    );
    final activity = PublicActivity(
      id: activityID,
      hostLabel: '合成组织者',
      placeName: '合成原活动场地',
      title: '明确原活动',
      summary: '',
      startsAt: DateTime.utc(2026, 10, 13, 10),
      endsAt: DateTime.utc(2026, 10, 13, 12),
      timeZone: 'Asia/Shanghai',
      schedule: '10月13日 18:00',
      status: 'upcoming',
      source: const PublicSource(
        label: '合成来源',
        maintainer: '合成',
        freshness: 'current',
        updatedAt: null,
      ),
      location: null,
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: item,
            activity: activity,
            onShare: (_) {},
          ),
        ),
      ),
    );
    expect(find.textContaining('10月13日 18:00'), findsOneWidget);
    expect(find.textContaining('合成原活动场地'), findsOneWidget);
    expect(find.textContaining('仅自己可见'), findsOneWidget);
    expect(find.text('分享活动'), findsOneWidget);
    expect(find.textContaining('分享只包含原活动'), findsNothing);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentEntityResultCard(
            item: item,
            activity: PublicActivity(
              id: avatarOwner,
              hostLabel: '',
              placeName: '其他活动不可串入',
              title: 'other',
              summary: '',
              startsAt: activity.startsAt,
              endsAt: activity.endsAt,
              timeZone: 'UTC',
              schedule: 'wrong',
              status: 'upcoming',
              source: activity.source,
              location: null,
            ),
          ),
        ),
      ),
    );
    expect(find.textContaining('其他活动不可串入'), findsNothing);
  });

  testWidgets('收藏用权威城市名，备注更新保留名称，无图仍可到原详情', (t) async {
    final client = MockClient(
      (_) async => avatarWire({
        'items': [
          {
            ...savedTestRow(
              avatarOperation,
              'place',
              avatarPeer,
              city: 'aberdeen-gb',
              title: '合成原地点',
            ),
            'cityName': '阿伯丁',
          },
        ],
        'nextCursor': null,
      }),
    );
    final c = SavedController(
      authorizationHeader: () => 'Bearer A',
      ownerID: () => avatarOwner,
      notePendingStore: MemorySavedNoteStore(),
      client: client,
      apiBaseUrl: 'https://native.test',
    );
    addTearDown(c.dispose);
    addTearDown(client.close);
    SavedItem? opened;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SavedPage(saved: c, onOpenItem: (i) => opened = i),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.textContaining('阿伯丁'), findsWidgets);
    expect(find.textContaining('aberdeen-gb'), findsNothing);
    expect(c.items.single.withNote('仅本人备注', 1).cityName, '阿伯丁');
    await t.tap(find.text('合成原地点'));
    expect(opened?.targetId, avatarPeer);
    expect(find.byType(Image), findsNothing);
    expect(t.takeException(), isNull);
  });

  testWidgets('通知真实发起人头像跟随当前个人来源，切组织后退休', (t) async {
    final auth = await signedInAuth(accountID: avatarPeer);
    final workspace = ValueNotifier<String?>(null);
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.url.path.endsWith('/content'))
        return http.Response.bytes(avatarJPEG(), 200);
      return noticePage([
        {
          ...pageNotice(1),
          'title': '明确原申请',
          'resourceType': 'connection_request',
          'actorId': avatarOwner,
          'actorAvatar': avatarMetadata(),
        },
      ]);
    });
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://native.test',
      client: client,
    );
    addTearDown(auth.dispose);
    addTearDown(workspace.dispose);
    addTearDown(source.dispose);
    addTearDown(client.close);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(
            auth: auth,
            source: source,
            embedded: true,
            notificationsOnly: true,
            workspaceChanges: workspace,
            organizationWorkspaceID: () => workspace.value,
          ),
        ),
      ),
    );
    // Inbox's first read creates the nested Avatar on the next paint. Give its
    // bounded content stream and codec a real event-loop turn after that paint.
    await t.pumpAndSettle();
    await _settleMedia(t);
    expect(find.text('明确原申请'), findsOneWidget);
    expect(find.byType(PersonAvatar), findsOneWidget);
    expect(find.byType(Image), findsOneWidget);
    expect(
      requests.where((r) => r.url.path.endsWith('/content')),
      hasLength(1),
    );
    expect(find.textContaining('已投递条目'), findsNothing);
    workspace.value = avatarOperation;
    await t.pumpAndSettle();
    expect(find.byType(Image), findsNothing);
    expect(find.text('明确原申请'), findsNothing);
    expect(t.takeException(), isNull);
  });
}
