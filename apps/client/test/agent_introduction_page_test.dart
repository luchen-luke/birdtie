import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_introduction_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:http/http.dart' as http;
import 'agent_introduction_api_test.dart';
import 'agent_introduction_controller_test.dart' show introReads;
import 'model_egress_page_test.dart' show egressAuth;

Future<void> introTap(WidgetTester t, String label) async {
  final f = find.text(label);
  if (find.byType(AlertDialog).evaluate().isEmpty) {
    await t.scrollUntilVisible(
      f,
      160,
      scrollable: find.byType(Scrollable).first,
    );
  }
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f);
  await t.pumpAndSettle();
}

class IntroBorrowClient extends MockClient {
  IntroBorrowClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

void main() {
  testWidgets(
    'ordinary user can review shared-public-registration preference without publishing or sending',
    (t) async {
      final auth = await egressAuth();
      var writes = 0;
      final client = MockClient((r) async {
        if (r.method != 'GET') {
          writes++;
        }
        return introReads(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: AgentIntroductionPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      await introTap(t, '检查本人社交偏好');
      await introTap(t, '共同公开报名：允许本人审阅');
      await introTap(t, '检查具体变更');
      expect(find.textContaining('共同公开报名：需本人审阅'), findsOneWidget);
      expect(find.textContaining('不代表到场'), findsOneWidget);
      await introTap(t, '返回');
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );

  testWidgets(
    'Chinese empty and disabled consent path is real navigation with zero writes',
    (t) async {
      final auth = await egressAuth();
      var writes = 0, manage = 0;
      final client = MockClient((r) async {
        if (r.method != 'GET') writes++;
        return introReads(r, consent: false, intents: []);
      });
      await t.pumpWidget(
        MaterialApp(
          home: AgentIntroductionPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
            onManageFindPeople: () => manage++,
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('看看共同意图'), findsOneWidget);
      expect(find.byType(TextField), findsNothing);
      await introTap(t, '前往找新朋友');
      expect(manage, 1);
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'stable current Person and original source callbacks cause no invitation or chat writes',
    (t) async {
      final auth = await egressAuth();
      final calls = <http.Request>[];
      final persons = <String>[], sources = <String>[];
      final client = MockClient((r) async {
        calls.add(r);
        return introReads(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: AgentIntroductionPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
            initialSourceIntentID: introSource,
            onOpenPerson: persons.add,
            onFindPeople: sources.add,
          ),
        ),
      );
      await t.pumpAndSettle();
      await introTap(t, '查看共同依据');
      await introTap(t, '查看此人资料');
      await introTap(t, '回找新朋友检查申请');
      expect(persons, [introPeer]);
      expect(sources, [introSource]);
      expect(calls.every((r) => r.method == 'GET'), true);
      expect(
        calls
            .where((r) => r.url.path.endsWith('/agent-introductions'))
            .single
            .url
            .queryParameters,
        {'sourceIntentId': introSource},
      );
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'mobile large text concrete social review cancellation writes zero PUT',
    (t) async {
      final auth = await egressAuth();
      var puts = 0;
      t.view.physicalSize = const Size(360, 720);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final client = MockClient((r) async {
        if (r.method == 'PUT') puts++;
        return introReads(r);
      });
      await t.pumpWidget(
        MaterialApp(
          builder: (c, w) => MediaQuery(
            data: MediaQuery.of(
              c,
            ).copyWith(textScaler: const TextScaler.linear(1.6)),
            child: w!,
          ),
          home: AgentIntroductionPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      await introTap(t, '检查本人社交偏好');
      expect(find.byType(TextField), findsNothing);
      expect(find.text('编辑本人社交偏好'), findsOneWidget);
      await introTap(t, '检查具体变更');
      expect(find.text('确认此版本的本人偏好'), findsOneWidget);
      final content = find.textContaining('原策略版本：1');
      expect(content, findsOneWidget);
      expect((t.widget<Text>(content)).data, contains('有效至：'));
      expect((t.widget<Text>(content)).data, contains('商家：关闭'));
      await introTap(t, '返回');
      expect(puts, 0);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'explicit save uses original current social version after preview and preserves other settings',
    (t) async {
      final auth = await egressAuth();
      final writes = <http.Request>[];
      final client = MockClient((r) async {
        if (r.method == 'PUT') {
          writes.add(r);
          final b = jsonDecode(r.body);
          return introResponse(
            introPolicy(
              revision: 2,
              values: {
                for (final v in b['settings']['rules'])
                  v['category'] as String: v['preference'] as String,
              },
              expiry: b['expiresAt'],
            ),
          );
        }
        return introReads(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: AgentIntroductionPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      await introTap(t, '检查本人社交偏好');
      await introTap(t, '检查具体变更');
      expect(writes, isEmpty);
      await introTap(t, '保存本人偏好');
      expect(writes.length, 1);
      expect(writes.single.url.path, '/v1/me/agent-policies/social');
      expect(jsonDecode(writes.single.body)['settings']['rules'].length, 7);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'same key transport replacement retires concrete dialog and never sends A with B',
    (t) async {
      final auth = await egressAuth();
      final calls = <http.Request>[];
      final client = IntroBorrowClient((r) async {
        calls.add(r);
        return introReads(r);
      });
      Widget page(String base) => MaterialApp(
        home: AgentIntroductionPage(
          key: const ValueKey('same'),
          auth: auth,
          client: client,
          apiBaseUrl: base,
        ),
      );
      await t.pumpWidget(page('http://A'));
      await t.pumpAndSettle();
      await introTap(t, '检查本人社交偏好');
      await introTap(t, '检查具体变更');
      expect(find.byType(AlertDialog), findsOneWidget);
      await t.pumpWidget(page('http://B'));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(calls.where((r) => r.url.host == 'b').length, 3);
      expect(calls.every((r) => r.method == 'GET'), true);
      expect(client.closes, 0);
      await t.pumpWidget(const SizedBox());
      expect(client.closes, 0);
      auth.dispose();
    },
  );
  testWidgets(
    'workspace switch retires editor and exact source callbacks before restoring personal identity',
    (t) async {
      final auth = await egressAuth();
      final changes = ValueNotifier<String?>(null);
      var writes = 0;
      final client = MockClient((r) async {
        if (r.method != 'GET') writes++;
        return introReads(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: AgentIntroductionPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
            workspaceChanges: changes,
            organizationWorkspaceID: () => changes.value,
          ),
        ),
      );
      await t.pumpAndSettle();
      await introTap(t, '检查本人社交偏好');
      expect(find.byType(AlertDialog), findsOneWidget);
      changes.value = introPeer;
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.textContaining('组织工作区'), findsOneWidget);
      changes.value = null;
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      changes.dispose();
      auth.dispose();
    },
  );
}
