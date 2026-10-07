import 'dart:async';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:birdtie_client/src/workspace/business_knowledge_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'business_console_controller_test.dart'
    show merchant, person, view, reply;
import 'business_knowledge_api_test.dart' show knowledgeAnswer, knowledgePlace;

void main() {
  testWidgets(
    'background clears reply; resume rereads current role without automatic query',
    (tester) async {
      final changes = ChangeNotifier();
      final source = Object();
      var denied = false, posts = 0;
      final api = BusinessApi(
        authorizationHeader: () => 'Bearer local',
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          if (r.method == 'POST') posts++;
          return reply(
            r.method == 'GET' ? view(manage: !denied) : knowledgeAnswer(),
          );
        }),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: BusinessKnowledgePage(
            api: api,
            businessID: merchant,
            accountID: () => person,
            authorizationHeader: () => 'Bearer local',
            workspaceID: () => null,
            currentBusinessID: () => merchant,
            bindingCurrent: () => true,
            sourceFrame: () => source,
            identityChanges: changes,
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(OutlinedButton, '商家介绍'));
      await tester.pumpAndSettle();
      final ask = find.widgetWithText(FilledButton, '查看资料回答');
      await tester.scrollUntilVisible(ask, 220);
      await tester.pumpAndSettle();
      await tester.tap(ask);
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('本地合成资料回答'), 220);
      await tester.pumpAndSettle();
      expect(posts, 1);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      await tester.pumpAndSettle();
      expect(find.text('本地合成资料回答'), findsNothing);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
      // Paused disables Flutter frames; the prior inactive frame is clear.
      denied = true;
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await tester.pumpAndSettle();
      expect(posts, 1);
      expect(find.text('本地合成资料回答'), findsNothing);
      expect(find.textContaining('权限'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      changes.dispose();
      api.dispose();
    },
  );
  testWidgets(
    'Chinese six choices, concrete venue, 360 width font3 scrolling and semantics',
    (tester) async {
      tester.view.physicalSize = const Size(360, 780);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final changes = ChangeNotifier();
      final s = view();
      s['venues'] = [
        {
          'placeId': knowledgePlace,
          'placeName': '本地合成很长很长的明确场地名称',
          'operationStatus': 'verified',
          'sourceUrl': '不应输出的私密核验来源',
          'rightsNote': '不应输出的经营权声明',
          'facts': {'note': '不应输出的私密备注'},
        },
      ];
      final calls = <http.Request>[];
      final api = BusinessApi(
        authorizationHeader: () => 'Bearer local',
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          calls.add(r);
          return reply(r.method == 'GET' ? s : knowledgeAnswer(venue: true));
        }),
      );
      final semantics = tester.ensureSemantics();
      await tester.pumpWidget(
        MaterialApp(
          home: BusinessKnowledgePage(
            api: api,
            businessID: merchant,
            accountID: () => person,
            authorizationHeader: () => 'Bearer local',
            workspaceID: () => null,
            currentBusinessID: () => merchant,
            bindingCurrent: () => true,
            sourceFrame: () => s,
            identityChanges: changes,
          ),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: TextScaler.linear(3)),
            child: child!,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(calls.where((r) => r.method == 'POST'), isEmpty);
      final question = find.widgetWithText(OutlinedButton, '场地适用场景');
      await tester.scrollUntilVisible(question, 300);
      await tester.pumpAndSettle();
      await tester.tap(question);
      await tester.pumpAndSettle();
      final venue = find.text('本地合成很长很长的明确场地名称');
      await tester.scrollUntilVisible(venue, 300);
      await tester.pumpAndSettle();
      await tester.tap(venue);
      await tester.pumpAndSettle();
      final ask = find.widgetWithText(FilledButton, '查看资料回答');
      await tester.scrollUntilVisible(ask, 300);
      await tester.pumpAndSettle();
      expect(tester.getSize(ask).height, greaterThanOrEqualTo(48));
      expect(
        tester.getSemantics(ask),
        matchesSemantics(
          isButton: true,
          hasEnabledState: true,
          isEnabled: true,
          isFocusable: true,
          label: '查看资料回答',
          hasTapAction: true,
          hasFocusAction: true,
        ),
      );
      await tester.tap(ask);
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('本地合成资料回答'), 250);
      await tester.pumpAndSettle();
      expect(find.text('本地合成资料回答'), findsOneWidget);
      expect(find.textContaining('不应输出'), findsNothing);
      expect(calls.where((r) => r.method == 'POST').length, 1);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      semantics.dispose();
      changes.dispose();
      api.dispose();
    },
  );
  testWidgets(
    'unknown and query denial clear current answer; leave without mutation',
    (tester) async {
      var status = 200, posts = 0;
      final changes = ChangeNotifier();
      final source = Object();
      final api = BusinessApi(
        authorizationHeader: () => 'Bearer local',
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          if (r.method == 'POST') posts++;
          return reply(
            r.method == 'GET' ? view() : knowledgeAnswer(status: 'unknown'),
            r.method == 'POST' ? status : 200,
          );
        }),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: BusinessKnowledgePage(
            api: api,
            businessID: merchant,
            accountID: () => person,
            authorizationHeader: () => 'Bearer local',
            workspaceID: () => null,
            currentBusinessID: () => merchant,
            bindingCurrent: () => true,
            sourceFrame: () => source,
            identityChanges: changes,
          ),
        ),
      );
      await tester.pumpAndSettle();
      final q = find.widgetWithText(OutlinedButton, '商家介绍');
      await tester.tap(q);
      await tester.pumpAndSettle();
      final ask = find.widgetWithText(FilledButton, '查看资料回答');
      await tester.scrollUntilVisible(ask, 200);
      await tester.pumpAndSettle();
      await tester.tap(ask);
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('当前资料未知'), 200);
      await tester.pumpAndSettle();
      expect(find.text('当前资料未知'), findsOneWidget);
      status = 403;
      await tester.ensureVisible(ask);
      await tester.tap(ask);
      await tester.pumpAndSettle();
      expect(find.text('当前资料未知'), findsNothing);
      expect(find.textContaining('权限'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      expect(posts, 2);
      changes.dispose();
      api.dispose();
    },
  );
  testWidgets(
    'same key new API detaches old response and reads replacement source',
    (tester) async {
      final oldResponse = Completer<http.Response>();
      final changes = ChangeNotifier();
      final source = Object();
      final old = BusinessApi(
        authorizationHeader: () => 'Bearer local',
        apiBaseUrl: 'http://old',
        client: MockClient((r) => oldResponse.future),
      );
      final next = BusinessApi(
        authorizationHeader: () => 'Bearer new',
        apiBaseUrl: 'http://new',
        client: MockClient((r) async {
          final s = view();
          (s['business'] as Map)['name'] = '替换后当前商家';
          return reply(s);
        }),
      );
      Widget page(BusinessApi api, String token) => MaterialApp(
        home: BusinessKnowledgePage(
          key: const ValueKey('same'),
          api: api,
          businessID: merchant,
          accountID: () => person,
          authorizationHeader: () => token,
          workspaceID: () => null,
          currentBusinessID: () => merchant,
          bindingCurrent: () => true,
          sourceFrame: () => source,
          identityChanges: changes,
        ),
      );
      await tester.pumpWidget(page(old, 'Bearer local'));
      await tester.pump();
      await tester.pumpWidget(page(next, 'Bearer new'));
      await tester.pumpAndSettle();
      oldResponse.complete(reply(view()));
      await tester.pumpAndSettle();
      expect(find.text('当前商家：替换后当前商家'), findsOneWidget);
      expect(find.text('当前商家：本地合成商家'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      expect((await next.read(merchant)).business.name, '替换后当前商家');
      changes.dispose();
      old.dispose();
      next.dispose();
    },
  );
  testWidgets('organization then same identity cannot resurrect old answer', (
    tester,
  ) async {
    final changes = ChangeNotifier();
    String? org;
    final source = Object();
    final api = BusinessApi(
      authorizationHeader: () => 'Bearer local',
      apiBaseUrl: 'http://fixture',
      client: MockClient((r) async => reply(view())),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: BusinessKnowledgePage(
          api: api,
          businessID: merchant,
          accountID: () => person,
          authorizationHeader: () => 'Bearer local',
          workspaceID: () => org,
          currentBusinessID: () => merchant,
          bindingCurrent: () => true,
          sourceFrame: () => source,
          identityChanges: changes,
        ),
      ),
    );
    await tester.pumpAndSettle();
    org = knowledgePlace;
    changes.notifyListeners();
    await tester.pumpAndSettle();
    org = null;
    changes.notifyListeners();
    await tester.pumpAndSettle();
    expect(find.text('商家介绍'), findsNothing);
    expect(find.textContaining('重新选择商家'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    changes.dispose();
    api.dispose();
  });
}
