import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'entity_action_contract_test.dart' show actionWire;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/chat_entity_router.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/activity_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:birdtie_client/src/workspace/public_organization_page.dart';
import 'package:birdtie_client/src/workspace/public_business_page.dart';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/community_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_share_pending_store_test.dart';
import 'public_moment_api_test.dart';
import 'share_entity_to_chat_test.dart' show ShareFixture;
import 'supplier_profile_api_test.dart' show publicSupplier;

class ChatTestAuth extends BirdtieAuthController {
  String token = 'Bearer A', person = shareOwner;
  @override
  String? get authorizationHeader => token;
  @override
  String? get accountID => person;
  @override
  bool get signedIn => true;
  void use(String next) {
    token = next;
    person = next == 'Bearer A' ? shareOwner : sharePeer;
    notifyListeners();
  }
}

http.Response chatReply(Object? value, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': value})), status);
Map<String, dynamic> chatActivity() => {
  'id': shareTarget,
  'title': '当前活动',
  'hostLabel': '本地合成主办人',
  'summary': '当前公开内容',
  'startsAt': '2030-10-03T08:00:00Z',
  'endsAt': '2030-10-03T10:00:00Z',
  'timeZone': 'Europe/London',
  'status': 'upcoming',
  'source': {'label': '本地合成测试'},
  'modality': 'online',
  'physicalPlaceStatus': 'not_applicable',
};
http.Response chatDomainReply(http.Request r) {
  final p = r.url.path;
  if (p == '/v1/activities/$shareTarget') return chatReply(chatActivity());
  if (p.endsWith('/participations/me')) return chatReply(null);
  if (p == '/v1/places/$shareTarget') {
    return chatReply({
      'id': shareTarget,
      'name': '当前地点',
      'source': {'label': '本地合成测试'},
      'location': {'precision': 'none'},
    });
  }
  if (p == '/v1/accounts/$shareTarget/profile') {
    return chatReply({
      'accountId': shareTarget,
      'displayName': '当前个人',
      'visibility': 'public',
      'bio': '',
    });
  }
  if (p == '/v1/organizations/$shareTarget') {
    return chatReply({
      'id': shareTarget,
      'name': '当前组织',
      'verificationStatus': 'verified',
      'upcomingActivities': [],
    });
  }
  if (p == '/v1/businesses/$shareTarget') {
    return chatReply(publicSupplier(published: false)..['id'] = shareTarget);
  }
  if (p == '/v1/communities/$shareTarget') {
    return chatReply({
      'id': shareTarget,
      'name': '当前社群',
      'description': '公开介绍',
      'visibility': 'public',
      'joinPolicy': 'open',
      'status': 'active',
      'memberCount': 0,
    });
  }
  if (p == '/v1/moments/$shareTarget') return momentReply(publicMomentData());
  if (p.endsWith('/activities') || p == '/v1/me/moments') return chatReply([]);
  return http.Response('{}', 503);
}

void main() {
  for (final changed in [false, true]) {
    testWidgets('聊天原Person卡MESSAGE实际Tie开原会话 $changed，零自动消息身份更换拒旧操作', (t) async {
      FlutterSecureStorage.setMockInitialValues({});
      final auth = ChatTestAuth();
      var opens = 0, messages = 0;
      final now = DateTime.now().toUtc();
      final pending = Completer<http.Response>();
      final client = MockClient((r) async {
        if (r.url.path.contains('/entity-actions/')) {
          final d = actionWire(
            ref: const EntityActionRef('person', shareTarget),
            now: now,
          );
          for (final dynamic a in d['actions'] as List) {
            a['state'] = a['kind'] == 'MESSAGE' ? 'AVAILABLE' : 'UNAVAILABLE';
          }
          return changed ? pending.future : chatReply(d);
        }
        if (r.url.path == '/v1/me/ties') {
          return chatReply([
            {
              'id': sharePeer,
              'otherAccountId': shareTarget,
              'otherName': '当前个人',
            },
          ]);
        }
        if (r.url.path == '/v1/me/ties/$sharePeer/conversation') {
          opens++;
          expect(r.headers['Authorization'], 'Bearer A');
          expect(r.headers['X-Birdtie-Action-Version'], 'a' * 64);
          expect(
            r.headers['X-Birdtie-Action-Until'],
            now.add(const Duration(seconds: 25)).toIso8601String(),
          );
          expect(r.headers['X-Birdtie-Action-Operation'], 'OPEN_CHAT');
          return chatReply({
            'id': shareConversation,
            'otherAccountId': shareTarget,
            'otherName': '当前个人',
          });
        }
        if (r.url.path.contains('/conversations/')) {
          if (r.method == 'POST' && r.url.path.endsWith('/messages')) {
            messages++;
          }
          return chatReply(r.method == 'GET' ? [] : {});
        }
        return chatDomainReply(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: ChatEntityDetail(
            type: 'person',
            id: shareTarget,
            auth: auth,
            client: client,
            apiBaseUrl: 'https://api.example',
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.ensureVisible(find.text('打开私信'));
      await t.tap(find.text('打开私信'));
      await t.pump();
      if (changed) {
        auth.use('Bearer B');
        auth.use('Bearer A');
        final d = actionWire(
          ref: const EntityActionRef('person', shareTarget),
          now: now,
        );
        for (final dynamic a in d['actions'] as List) {
          a['state'] = a['kind'] == 'MESSAGE' ? 'AVAILABLE' : 'UNAVAILABLE';
        }
        pending.complete(chatReply(d));
        await t.pumpAndSettle();
      } else {
        await t.pumpAndSettle();
        expect(find.byType(HumanConversationRoute), findsOneWidget);
        expect(find.byType(HumanConversationPage), findsOneWidget);
        await t.pageBack();
        await t.pumpAndSettle();
      }
      expect(opens, changed ? 0 : 1);
      expect(messages, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    });
  }
  const types = <String, Type>{
    'activity': ActivityDetailSheet,
    'place': PlaceDetailSheet,
    'person': PublicPersonPage,
    'organization': PublicOrganizationPage,
    'business': PublicBusinessPage,
    'community': CommunityDetailPage,
    'moment': PublicMomentPage,
  };
  for (final item in types.entries) {
    testWidgets('聊天${item.key}稳定ID复用现有领域详情并能返回', (t) async {
      final auth = ChatTestAuth(), requests = <String>[];
      final client = MockClient((r) async {
        requests.add(r.url.path);
        return chatDomainReply(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (c) => FilledButton(
                onPressed: () => openChatEntity(
                  c,
                  ChatEntityCard(
                    type: item.key,
                    id: shareTarget,
                    title: '旧卡标题',
                    available: true,
                  ),
                  auth: auth,
                  apiBaseUrl: 'https://api.example',
                  client: client,
                ),
                child: const Text('打开卡片'),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('打开卡片'));
      await t.pumpAndSettle();
      expect(find.byType(item.value), findsOneWidget);
      expect(requests.any((p) => p.contains(shareTarget)), isTrue);
      expect(find.text('旧卡标题'), findsNothing);
      expect(find.byType(BackButton), findsWidgets);
      await t.tap(find.byType(BackButton).first);
      await t.pumpAndSettle();
      expect(find.text('打开卡片'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    });
  }
  for (final bad in [
    const ChatEntityCard(type: 'memory', id: shareTarget, available: true),
    const ChatEntityCard(
      type: 'moment',
      id: 'https://example.invalid',
      available: true,
    ),
    const ChatEntityCard(type: 'moment', id: shareTarget, available: false),
  ]) {
    testWidgets('未知类型、伪ID或不可用卡不发请求：${bad.type}/${bad.available}', (t) async {
      final auth = ChatTestAuth();
      var calls = 0;
      final client = MockClient((_) async {
        calls++;
        return http.Response('{}', 500);
      });
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (c) => TextButton(
                onPressed: () => openChatEntity(
                  c,
                  bad,
                  auth: auth,
                  apiBaseUrl: 'https://api.example',
                  client: client,
                ),
                child: const Text('打开'),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('打开'));
      await t.pumpAndSettle();
      expect(calls, 0);
      expect(find.byType(ChatEntityDetail), findsNothing);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    });
  }
  testWidgets('嵌套公开动态分享确认在身份ABA后整个撤除，无旧收件人与POST', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final auth = ChatTestAuth(), f = ShareFixture();
    await t.pumpWidget(
      MaterialApp(
        home: ChatEntityDetail(
          type: 'moment',
          id: shareTarget,
          auth: auth,
          apiBaseUrl: 'https://api.example',
          client: f.client,
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.ensureVisible(find.text('发给好友'));
    await t.tap(find.text('发给好友'));
    await t.pumpAndSettle();
    await t.ensureVisible(find.text('本地合成好友'));
    await t.tap(find.text('本地合成好友'));
    await t.pumpAndSettle();
    expect(find.text('确认发送卡片'), findsOneWidget);
    auth.use('Bearer B');
    auth.use('Bearer A');
    await t.pumpAndSettle();
    expect(find.text('确认发送卡片'), findsNothing);
    expect(find.textContaining('本地合成好友'), findsNothing);
    expect(find.textContaining('工作身份已变化'), findsOneWidget);
    expect(f.posted, isEmpty);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    f.dispose();
  });
  testWidgets('活动稳定读取迟到不进入新身份的详情', (t) async {
    final auth = ChatTestAuth(), pending = Completer<http.Response>();
    final client = MockClient((_) async => pending.future);
    await t.pumpWidget(
      MaterialApp(
        home: ChatEntityDetail(
          type: 'activity',
          id: shareTarget,
          auth: auth,
          client: client,
          apiBaseUrl: 'https://api.example',
        ),
      ),
    );
    auth.use('Bearer B');
    auth.use('Bearer A');
    pending.complete(chatReply(chatActivity()));
    await t.pumpAndSettle();
    expect(find.byType(ActivityDetailSheet), findsNothing);
    expect(find.text('当前活动'), findsNothing);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
}
