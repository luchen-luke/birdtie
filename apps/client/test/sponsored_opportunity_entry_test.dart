import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/opportunity_page.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'sponsored_opportunities_test.dart'
    show sponsorEnvelope, sponsorActivityID, sponsorPlaceID, sponsorTitle;

http.Response _json(Object v) =>
    http.Response.bytes(utf8.encode(jsonEncode(v)), 200,
      headers: {'content-type': 'application/json; charset=utf-8'});
const _person = '00000000-0000-4000-8000-000000000001';
const _intent = '11111111-1111-4111-8111-111111111111';
Future<BirdtieAuthController> _auth() async {
  final a = BirdtieAuthController(
    apiBaseUrl: 'https://qa.example',
    sessionVault: MemorySessionVault(),
    client: MockClient(
      (r) async => r.url.path == '/v1/session/logout'
          ? http.Response('', 204)
          : _json({
              'data': switch (r.url.path) {
                '/v1/auth/dev-phone/status' => {'enabled': true},
                '/v1/auth/dev-phone/code' => {'expiresInSeconds': 300},
                '/v1/auth/dev-phone/verify' => {'accessToken': 'qa-token'},
                '/v1/me' => {'id': _person},
                _ => {'displayName': '本地合成验收'},
              },
            }),
    ),
  );
  await a.initialize();
  await a.requestDevPhoneCode('13800138000');
  await a.verifyDevPhoneCode('13800138000', '123456');
  return a;
}

Map<String, dynamic> _opportunity() => {
  'data': [
    {
      'id': '$_intent:$sponsorActivityID',
      'intentId': _intent,
      'entity': {'type': 'ACTIVITY', 'id': sponsorActivityID},
      'place': {'type': 'PLACE', 'id': sponsorPlaceID},
      'title': sponsorTitle,
      'placeName': '合成公开球馆',
      'startsAt': '2026-10-10T12:00:00Z',
      'reasonCodes': ['INTENT_CATEGORY'],
      'reason': '不可呈现的原始理由',
      'ruleVersion': 'activity-place-v2',
      'routeTier': 'PUBLIC',
      'action': {
        'type': 'OPEN_ACTIVITY',
        'target': {'type': 'ACTIVITY', 'id': sponsorActivityID},
      },
    },
  ],
  'source': 'RULE_BASED',
  'ruleVersion': 'activity-place-v2',
  ...sponsorEnvelope(),
};
Map<String, dynamic> _agent({String type = 'ACTIVITY'}) => {
  'activities': [
    {
      'id': sponsorActivityID,
      'title': sponsorTitle,
      'placeName': '合成公开球馆',
      'status': 'upcoming',
      'timeZone': 'Europe/London',
      'source': {},
      'startsAt': '2026-10-10T12:00:00Z',
      'endsAt': '2026-10-10T14:00:00Z',
    },
  ],
  'places': [
    {
      'id': sponsorPlaceID,
      'name': '合成公开球馆',
      'categoryCode': 'sports_venue',
      'summary': '',
      'source': {},
      'location': {
        'coordinateSystem': 'wgs84',
        'precision': 'point',
        'latitude': 57.14,
        'longitude': -2.1,
      },
    },
  ],
  'people': [],
  'groups': [],
  'organizations': [],
  'message': '公开匹配结果',
  'resultSet': {
    'id': 'current-set',
    'status': 'ready',
    'generatedAt': '2026-10-03T01:06:00Z',
    'entities': [
      {'type': 'activity', 'id': sponsorActivityID},
      {'type': 'place', 'id': sponsorPlaceID},
    ],
  },
  'mapEffects': {
    'camera': 'preserve',
    'pinEntityIds': ['place:$sponsorPlaceID'],
  },
  ...sponsorEnvelope(type: type),
};
void main() {
  testWidgets(
    'real opportunity entry discloses sponsor after natural content and opens same activity',
    (t) async {
      final auth = await _auth();
      final requests = <String>[];
      String? opened;
      final client = MockClient((r) async {
        requests.add('${r.method} ${r.url.path}');
        expect(r.headers['Authorization'], 'Bearer qa-token');
        return _json(
          r.url.path.endsWith('opportunities') ? _opportunity() : {'data': []},
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: OpportunityPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'https://qa.example',
            onOpenActivity: (id) => opened = id,
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text(sponsorTitle), findsNWidgets(2));
      expect(find.text('赞助'), findsOneWidget);
      expect(find.textContaining('不可呈现'), findsNothing);
      final list = find.byType(ListView);
      await t.drag(list, const Offset(0, -500));
      await t.pumpAndSettle();
      await t.ensureVisible(find.text('查看赞助活动详情'));
      await t.tap(find.text('查看赞助活动详情'));
      expect(opened, sponsorActivityID);
      expect(requests.every((r) => r.startsWith('GET ')), true);
      await auth.signOut();
      await t.pumpAndSettle();
      expect(find.text('赞助'), findsNothing);
      expect(find.text(sponsorTitle), findsNothing);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    },
  );
  testWidgets('late sponsor response cannot return after real logout', (
    t,
  ) async {
    final auth = await _auth();
    final delayed = Completer<http.Response>();
    final client = MockClient(
      (r) async => r.url.path.endsWith('opportunities')
          ? delayed.future
          : _json({'data': []}),
    );
    await t.pumpWidget(
      MaterialApp(
        home: OpportunityPage(
          auth: auth,
          client: client,
          apiBaseUrl: 'https://qa.example',
        ),
      ),
    );
    await t.pump();
    await auth.signOut();
    await t.pump();
    delayed.complete(_json(_opportunity()));
    await t.pumpAndSettle();
    expect(find.text('赞助'), findsNothing);
    expect(find.text(sponsorTitle), findsNothing);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  for (final type in ['ACTIVITY', 'PLACE']) {
    testWidgets(
      'actual remote $type parsing and result sheet keep natural set/pins and stable detail',
      (t) async {
        t.view.physicalSize = const Size(800, 1200);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final payload = _agent(type: type);
        final natural = jsonDecode(jsonEncode(payload)) as Map<String, dynamic>;
        natural.remove('commercialTrustVersion');
        natural.remove('sponsoredStatus');
        natural.remove('sponsoredOpportunities');
        Future<AgentResult> parse(Map<String, dynamic> data) async {
          final source = RemoteAgentTaskSource(
            cityID: () => 'aberdeen-gb',
            authorizationHeader: () => null,
            apiBaseUrl: 'https://qa.example',
            client: MockClient((_) async => _json({'data': data})),
          );
          final result = await source.resolve('找羽毛球', [], []);
          source.dispose();
          return result;
        }

        final base = await parse(natural);
        final sponsored = await parse(payload);
        expect(
          sponsored.entities.map((e) => e.id),
          base.entities.map((e) => e.id),
        );
        expect(
          sponsored.resultSet?.entities.map((e) => e.mapID),
          base.resultSet?.entities.map((e) => e.mapID),
        );
        expect(
          sponsored.mapEffects?.pinEntityIDs,
          base.mapEffects?.pinEntityIDs,
        );
        expect(
          sponsored.activities.map((a) => a.id),
          base.activities.map((a) => a.id),
        );
        expect(sponsored.places.map((p) => p.id), base.places.map((p) => p.id));
        final w = AgentWorkspaceController()
          ..task = const AgentTask(
            id: 'synthetic-task',
            query: '找羽毛球',
            status: 'COMPLETED',
          )
          ..result = sponsored
          ..setSheetExtent(AgentSheetExtent.medium);
        String? opened;
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: AgentResultsSheet(
                workspace: w,
                availableHeight: 1100,
                onSuggestion: (_) {},
                onRetry: () {},
                onOpenActivity: (a) => opened = a.id,
                onOpenPlace: (id) => opened = id,
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        await t.scrollUntilVisible(
          find.text(type == 'ACTIVITY' ? '查看赞助活动详情' : '查看赞助地点详情'),
          300,
          scrollable: find.byType(Scrollable).last,
        );
        await t.pumpAndSettle();
        await t.tap(find.text(type == 'ACTIVITY' ? '查看赞助活动详情' : '查看赞助地点详情'));
        expect(opened, type == 'ACTIVITY' ? sponsorActivityID : sponsorPlaceID);
        expect(find.text('赞助'), findsOneWidget);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        w.dispose();
      },
    );
  }
  test(
    'actual remote malformed sponsor does not silently present paid metadata as organic',
    () async {
      final payload = _agent();
      payload['sponsoredOpportunities'][0]['label'] = '最推荐';
      final s = RemoteAgentTaskSource(
        cityID: () => 'aberdeen-gb',
        authorizationHeader: () => null,
        apiBaseUrl: 'https://qa.example',
        client: MockClient((_) async => _json({'data': payload})),
      );
      await expectLater(
        s.resolve('找活动', [], []),
        throwsA(isA<AgentRequestFailure>()),
      );
      s.dispose();
    },
  );
}
