import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/agent_seed_sheet.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_controller_test.dart' show seedJson, seedResponse;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;

void main() {
  for (final entry in [
    'settings-initial',
    'settings-progressive',
    'now-auto',
    'now-progressive',
  ]) {
    testWidgets('$entry 来源重绑永久移除旧设置正文且不向新源自动保存', (tester) async {
      final auth = SeedTestAuth();
      final city = PublicCityController();
      final rebound = ValueNotifier<bool>(false);
      final requests = <http.Request>[];
      final client = MockClient((r) async {
        requests.add(r);
        if (r.url.path == '/v1/me/agent-seed') {
          return seedResponse(
            seedJson(
              progress: entry == 'now-auto' ? 'UNSET' : 'COMPLETED',
              intent: 'JUST_EXPLORE',
              city: 'aberdeen-gb',
              languages: ['zh-CN'],
              displayName: '旧来源私密昵称',
            ),
          );
        }
        return http.Response('{"data":[]}', 200);
      });
      final nextRequests = <http.Request>[];
      final nextClient = MockClient((r) async {
        nextRequests.add(r);
        return http.Response('{"data":[]}', 200);
      });
      final moments = PrivateMomentController(
        client: client,
        apiBaseUrl: 'http://source-a',
        authorizationHeader: () => auth.authorizationHeader,
      );
      await tester.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<bool>(
            valueListenable: rebound,
            builder: (_, next, _) {
              final base = next ? 'http://source-b' : 'http://source-a';
              final source = next ? nextClient : client;
              return entry.startsWith('settings')
                  ? Scaffold(
                      body: SettingsPage(
                        key: const ValueKey('same-settings'),
                        auth: auth,
                        city: city,
                        moments: moments,
                        client: source,
                        apiBaseUrl: base,
                      ),
                    )
                  : MapWorkspace(
                      key: const ValueKey('same-now'),
                      auth: auth,
                      city: city,
                      moments: moments,
                      seedClient: source,
                      seedApiBaseUrl: base,
                    );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();
      if (entry.startsWith('settings')) {
        final label = entry == 'settings-initial' ? '我的初始设置' : '完善我的选择';
        await tester.ensureVisible(find.text(label));
        await tester.tap(find.text(label));
        await tester.pumpAndSettle();
      } else if (entry == 'now-progressive') {
        await tester.tap(find.text('完善我的选择'));
        await tester.pumpAndSettle();
      }
      expect(find.byType(AgentSeedSheet), findsOneWidget);
      final originalReads = requests
          .where((r) => r.url.path == '/v1/me/agent-seed')
          .length;
      expect(originalReads, greaterThan(0));
      rebound.value = true;
      await tester.pumpAndSettle();
      expect(find.byType(AgentSeedSheet), findsNothing);
      expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
      rebound.value = false;
      await tester.pumpAndSettle();
      expect(find.byType(AgentSeedSheet), findsNothing);
      expect(find.text('旧来源私密昵称'), findsNothing);
      expect(requests.where((r) => r.method != 'GET'), isEmpty);
      expect(
        nextRequests.where((r) => r.url.path == '/v1/me/agent-seed'),
        isEmpty,
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      moments.dispose();
      city.dispose();
      auth.dispose();
      rebound.dispose();
      client.close();
      nextClient.close();
    });
  }
  for (final restored in [false, true]) {
    testWidgets(restored ? '恢复登录读取真实缺项并打开渐进入口' : '完成登录后读取本人缺项，不使用浏览城市默认值', (
      tester,
    ) async {
      final auth = SeedTestAuth();
      if (!restored) auth.token = null;
      final city = PublicCityController();
      final client = MockClient((_) async => seedResponse(seedJson()));
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'http://local',
      );
      addTearDown(auth.dispose);
      addTearDown(city.dispose);
      addTearDown(moments.dispose);
      await tester.pumpWidget(
        MaterialApp(
          home: MapWorkspace(
            city: city,
            auth: auth,
            moments: moments,
            seedClient: client,
            seedApiBaseUrl: 'http://local',
          ),
        ),
      );
      await tester.pumpAndSettle();
      if (!restored) {
        expect(find.text('初始设置'), findsNothing);
        auth.changeIdentity('Bearer owner', nextOwner: 'owner');
        await tester.pumpAndSettle();
      }
      expect(find.text('初始设置'), findsOneWidget);
      expect(find.text('本人声明的当前城市'), findsOneWidget);
      expect(find.text('阿伯丁'), findsNothing);
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.text('初始设置'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      await tester.pump();
    });
  }
  testWidgets('恢复已完成或稍后进度不再弹出，设置直接入口继续保留', (tester) async {
    for (final progress in ['COMPLETED', 'DEFERRED']) {
      final auth = SeedTestAuth();
      final city = PublicCityController();
      final client = MockClient(
        (_) async => seedResponse(
          seedJson(
            progress: progress,
            intent: progress == 'COMPLETED' ? 'JUST_EXPLORE' : '',
            city: progress == 'COMPLETED' ? 'aberdeen-gb' : null,
            languages: progress == 'COMPLETED' ? ['zh-CN'] : [],
          ),
        ),
      );
      final moments = PrivateMomentController(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: 'http://local',
      );
      await tester.pumpWidget(
        MaterialApp(
          home: MapWorkspace(
            city: city,
            auth: auth,
            moments: moments,
            seedClient: client,
            seedApiBaseUrl: 'http://local',
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('初始设置'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      await tester.pump();
      moments.dispose();
      city.dispose();
      auth.dispose();
    }
  });
  testWidgets('真实设置保留本人seed直接入口，已存私密内容可重新读取', (tester) async {
    final auth = SeedTestAuth();
    addTearDown(auth.dispose);
    final client = MockClient((r) async {
      if (r.url.path == '/v1/me/agent-seed') {
        return seedResponse(
          seedJson(
            progress: 'COMPLETED',
            intent: 'DISCOVER_PLACES',
            city: 'aberdeen-gb',
            languages: ['zh-CN'],
            interests: ['原有兴趣'],
          ),
        );
      }
      return http.Response(
        '{"data":[]}',
        200,
        headers: {'content-type': 'application/json'},
      );
    });
    final city = PublicCityController();
    final moments = PrivateMomentController(
      client: client,
      apiBaseUrl: 'http://local',
      authorizationHeader: () => auth.authorizationHeader,
    );
    addTearDown(city.dispose);
    addTearDown(moments.dispose);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: 'http://local',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('我的初始设置'));
    await tester.tap(find.text('我的初始设置'));
    await tester.pumpAndSettle();
    expect(find.text('初始设置'), findsOneWidget);
    expect(find.text('阿伯丁'), findsOneWidget);
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(find.text('个人资料与可见范围'), findsOneWidget);
  });
}
