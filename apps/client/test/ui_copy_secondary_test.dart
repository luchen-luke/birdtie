import 'dart:convert';
import 'package:birdtie_client/src/workspace/memory_content_fields.dart';
import 'package:birdtie_client/src/workspace/agent_profile_page.dart';
import 'package:birdtie_client/src/workspace/social_now_page.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_profile_api_test.dart';
import 'agent_memory_correction_api_test.dart';
import 'agent_memory_correction_page_test.dart'
    show correctionPageHarness, correctionSettle, correctionTap;
import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'social_now_controller_test.dart' show nowEmpty, nowOwner;

void main() {
  testWidgets('记忆补充字段用中文，未知键值不猜测或暴露，完整用户备注保留', (t) async {
    final values = {
      'activityCategory': 'hiking',
      'languages': ['en', 'zh-CN'],
      'notes': '用户填写的完整中文 English 备注',
      'objectId': correctionMemory,
      'unknownField': {'action': 'SECRET'},
      'preferredActivityTypes': ['unknown-category'],
    };
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(body: MemoryContentFields(values: values)),
      ),
    );
    expect(find.text('活动类型：徒步'), findsOneWidget);
    expect(find.text('交流语言：英语、简体中文'), findsOneWidget);
    expect(find.text('备注：用户填写的完整中文 English 备注'), findsOneWidget);
    expect(find.textContaining(correctionMemory), findsNothing);
    expect(find.textContaining('SECRET'), findsNothing);
    expect(find.textContaining('unknown-category'), findsNothing);
    expect(find.text('部分补充信息暂无法展示，修改时会保留。'), findsOneWidget);
    expect(values['objectId'], correctionMemory);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('资料原摘要可打开中文补充，不呈现JSON/开发ID且查看零写', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      final data = profileData(r.url.path);
      if (r.url.path.endsWith('/$correctionMemory')) {
        final raw = detailRaw();
        (raw['memory'] as Map)['structuredValue'] = {
          'activityCategory': 'hiking',
          'objectId': 'hidden-diagnostic-id',
        };
        return profileResponse(raw);
      }
      return profileResponse(data);
    });
    await t.pumpWidget(
      MaterialApp(
        home: AgentProfilePage(
          auth: auth,
          client: client,
          apiBaseUrl: 'http://local',
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.ensureVisible(find.text('我偏好徒步活动'));
    await t.tap(find.text('我偏好徒步活动'));
    await t.pumpAndSettle();
    expect(find.text('活动类型：徒步'), findsOneWidget);
    expect(find.textContaining('activityCategory'), findsNothing);
    expect(find.textContaining('hidden-diagnostic-id'), findsNothing);
    expect(find.text('检查原补充内容'), findsNothing);
    expect(requests.every((r) => r.method == 'GET'), true);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('编辑记忆展示前后内容，具体版本/digest仍内部绑定，预览不能代替确认', (t) async {
    final auth = SeedTestAuth()..owner = correctionOwner;
    final at = DateTime.now().toUtc();
    final writes = <http.Request>[];
    Map<String, dynamic>? input;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        writes.add(r);
        if (r.url.path.endsWith('/previews')) {
          input = Map<String, dynamic>.from(jsonDecode(r.body));
          return correctionResponse(correctionPreviewRaw(input: input, at: at));
        }
        expect(jsonDecode(r.body), {'planDigest': correctionPlan});
        return correctionResponse(correctionReceiptRaw(input: input, at: at));
      }
      return correctionReadResponse(r, {
        'data': r.url.path.endsWith('agent-memories/page')
            ? [correctionMemoryRaw(at: at)]
            : [],
      });
    });
    await t.pumpWidget(
      correctionPageHarness(
        auth,
        client,
        MemoryAgentMemoryCorrectionPendingStore(),
      ),
    );
    await correctionSettle(t);
    await correctionTap(t, '修改这条记忆');
    await t.enterText(find.byType(TextField), '本人修改后的完整内容');
    await correctionTap(t, '检查修改');
    expect(writes, hasLength(1));
    expect(input!['expectedVersion'], 1);
    expect(input!['targetId'], correctionMemory);
    expect(input!['replacement']['structuredValue'], {
      'activityCategory': 'hiking',
    });
    expect(find.text('当前记忆'), findsOneWidget);
    expect(find.text('修改后'), findsOneWidget);
    expect(find.text('本人修改后的完整内容'), findsOneWidget);
    expect(find.textContaining('对象标识'), findsNothing);
    expect(find.textContaining('版本 1'), findsNothing);
    expect(find.textContaining('DELETED'), findsNothing);
    expect(find.textContaining(correctionMemory), findsNothing);
    await correctionTap(t, '确认修改');
    expect(writes, hasLength(2));
    expect(writes.last.headers['Authorization'], 'Bearer owner');
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('社交近况空组不占位，调整需求入口保留且零隐式写', (t) async {
    final auth = SeedTestAuth()..owner = nowOwner;
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      return nowEmpty(r);
    });
    await t.pumpWidget(
      MaterialApp(
        home: SocialNowPage(
          auth: auth,
          client: client,
          apiBaseUrl: 'http://local',
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('暂无社交近况'), findsOneWidget);
    for (final s in ['好友的想法', '好友主办的活动', '已加入社群的活动', '其他活动机会'])
      expect(find.text(s), findsNothing);
    expect(find.text('调整找活动需求'), findsOneWidget);
    expect(find.textContaining('最多'), findsNothing);
    expect(requests.every((r) => r.method == 'GET'), true);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('地点无场地资料隐藏整个举办空块，地址与来源仍可读', (t) async {
    final client = MockClient(
      (r) async => r.url.path.endsWith('/venue')
          ? http.Response('{}', 404)
          : http.Response.bytes(
              utf8.encode(
                jsonEncode({
                  'data': r.url.path.endsWith('/activities')
                      ? []
                      : {
                          'id': correctionMemory,
                          'name': '样例地点',
                          'addressLabel': '实际来源地址',
                          'source': {'label': '组件样例来源'},
                          'location': {'precision': 'none'},
                        },
                }),
              ),
              200,
            ),
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PlaceDetailSheet(
            placeID: correctionMemory,
            authorizationHeader: () => null,
            onOpenActivity: (_) {},
            client: client,
            apiBaseUrl: 'http://local',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('样例地点'), findsOneWidget);
    expect(find.text('地址：实际来源地址'), findsOneWidget);
    expect(find.text('举办条件'), findsNothing);
    expect(find.text('场地资料'), findsNothing);
    expect(find.textContaining('暂无已审核'), findsNothing);
    await t.pumpWidget(const SizedBox());
    client.close();
  });
}
