import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/person_community_interest_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'model_egress_page_test.dart' show egressAuth;
import 'person_community_interest_api_test.dart';
import 'person_community_interest_controller_test.dart' show interestReads;

class InterestBorrowClient extends MockClient {
  InterestBorrowClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

Future<void> interestTap(WidgetTester t, String label) async {
  final f = find.text(label);
  if (find.byType(AlertDialog).evaluate().isEmpty) {
    await t.scrollUntilVisible(
      f,
      140,
      scrollable: find.byType(Scrollable).first,
    );
  }
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f);
  await t.pumpAndSettle();
}

void main() {
  testWidgets(
    'PRIVATE success then enabled own public action actually requests new concrete PUBLIC preview',
    (t) async {
      final auth = await egressAuth();
      final operations = <String>[];
      var approves = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          final op = jsonDecode(r.body)['operation'] as String;
          operations.add(op);
          final p = interestPreview(op);
          if (op == 'PUBLIC') {
            p['state'] = 'PRIVATE';
            p['contextId'] = interestContext;
          }
          return interestResponse(p);
        }
        if (r.url.path.endsWith('/approve')) {
          approves++;
          return interestResponse(interestView(state: 'PRIVATE'));
        }
        return interestReads(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: PersonCommunityInterestPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('已读取当前兴趣声明。'), findsOneWidget);
      await interestTap(t, '合成公开社群');
      await interestTap(t, '检查兴趣声明');
      await interestTap(t, '批准这个版本');
      expect(approves, 1);
      await interestTap(t, '检查公开兴趣');
      expect(operations, ['PRIVATE', 'PUBLIC']);
      expect(find.text('仅自己可见 → 公开兴趣'), findsOneWidget);
      await interestTap(t, '取消');
      expect(approves, 1);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'own public selector stale source shows refresh feedback not approval',
    (t) async {
      final auth = await egressAuth();
      var publicPreviews = 0, approves = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          final op = jsonDecode(r.body)['operation'];
          if (op == 'PUBLIC') {
            publicPreviews++;
            return http.Response('{}', 409);
          }
          return interestResponse(interestPreview());
        }
        if (r.url.path.endsWith('/approve')) {
          approves++;
          return interestResponse(interestView(state: 'PRIVATE'));
        }
        return interestReads(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: PersonCommunityInterestPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      await interestTap(t, '合成公开社群');
      await interestTap(t, '检查兴趣声明');
      await interestTap(t, '批准这个版本');
      await interestTap(t, '检查公开兴趣');
      expect(publicPreviews, 1);
      expect(approves, 1);
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('当前来源或具体版本已变化，请刷新后检查。'), findsOneWidget);
      expect(find.text('检查公开兴趣'), findsNothing);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'Chinese empty PRIVATE default real read path no UUID inputs no writes',
    (t) async {
      final auth = await egressAuth();
      var writes = 0;
      final client = MockClient((r) async {
        if (r.method != 'GET') writes++;
        return interestResponse(interestView());
      });
      await t.pumpWidget(
        MaterialApp(
          home: PersonCommunityInterestPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('我的社群兴趣'), findsOneWidget);
      expect(find.byType(TextField), findsNothing);
      expect(find.textContaining('暂时没有可选'), findsOneWidget);
      expect(find.text('仅自己可见'), findsOneWidget);
      await interestTap(t, '刷新当前声明');
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'specific Chinese review shows audience source current target expiry cancel zero approve and valid approve once',
    (t) async {
      final auth = await egressAuth();
      var approved = 0;
      final operations = <String>[];
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          final op = jsonDecode(r.body)['operation'] as String;
          operations.add(op);
          return interestResponse(interestPreview(op));
        }
        if (r.url.path.endsWith('/approve')) {
          approved++;
          return interestResponse(interestView(state: 'PRIVATE'));
        }
        return interestReads(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: PersonCommunityInterestPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      await interestTap(t, '合成公开社群');
      await interestTap(t, '检查兴趣声明');
      expect(find.text('检查这次具体声明'), findsOneWidget);
      expect(find.text('没有声明 → 仅自己可见'), findsOneWidget);
      expect(find.textContaining('预览有效至'), findsOneWidget);
      expect(find.textContaining('不代表成员资格'), findsWidgets);
      await interestTap(t, '取消');
      expect(approved, 0);
      expect(operations, ['PRIVATE']);
      await interestTap(t, '检查兴趣声明');
      await interestTap(t, '批准这个版本');
      expect(approved, 1);
      expect(find.text('已收到该具体声明操作的权威结果。'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'same-key base/client replacement closes approval and retires borrowed transport A no A-to-B writes',
    (t) async {
      final auth = await egressAuth();
      var writesA = 0, writesB = 0;
      final hosts = <String>[];
      Future<http.Response> respond(http.Request r) async {
        hosts.add(r.url.host);
        if (r.url.path.endsWith('/preview')) {
          return interestResponse(interestPreview());
        }
        return interestReads(r);
      }

      final a = InterestBorrowClient((r) async {
        if (r.url.path.endsWith('/approve')) writesA++;
        return respond(r);
      });
      final b = InterestBorrowClient((r) async {
        if (r.url.path.endsWith('/approve')) writesB++;
        return respond(r);
      });
      Widget page(http.Client c, String base) => MaterialApp(
        home: PersonCommunityInterestPage(
          key: const ValueKey('same'),
          auth: auth,
          client: c,
          apiBaseUrl: base,
        ),
      );
      await t.pumpWidget(page(a, 'http://transport-a'));
      await t.pumpAndSettle();
      await interestTap(t, '合成公开社群');
      await interestTap(t, '检查兴趣声明');
      expect(find.byType(AlertDialog), findsOneWidget);
      await t.pumpWidget(page(b, 'http://transport-b'));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      await interestTap(t, '刷新当前声明');
      expect(hosts.last, 'transport-b');
      expect(writesA + writesB, 0);
      expect(a.closes, 0);
      await t.pumpWidget(const SizedBox());
      expect(b.closes, 0);
      auth.dispose();
    },
  );
  testWidgets(
    'workspace ABA removes dialog and late preview never opens under replaced identity',
    (t) async {
      final auth = await egressAuth();
      final changes = ValueNotifier<int>(0);
      String? workspace;
      var approved = 0;
      final late = Completer<http.Response>();
      var wait = false;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          return wait ? late.future : interestResponse(interestPreview());
        }
        if (r.url.path.endsWith('/approve')) approved++;
        return interestReads(r);
      });
      await t.pumpWidget(
        MaterialApp(
          home: PersonCommunityInterestPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
            workspaceChanges: changes,
            organizationWorkspaceID: () => workspace,
          ),
        ),
      );
      await t.pumpAndSettle();
      await interestTap(t, '合成公开社群');
      await interestTap(t, '检查兴趣声明');
      workspace = interestCommunity;
      changes.value++;
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      workspace = null;
      changes.value++;
      await t.pumpAndSettle();
      expect(approved, 0);
      wait = true;
      await interestTap(t, '合成公开社群');
      await t.ensureVisible(find.text('检查兴趣声明'));
      await t.tap(find.text('检查兴趣声明'));
      await t.pump();
      workspace = interestCommunity;
      changes.value++;
      await t.pump();
      workspace = null;
      changes.value++;
      await t.pump();
      late.complete(interestResponse(interestPreview()));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(approved, 0);
      await t.pumpWidget(const SizedBox());
      changes.dispose();
      auth.dispose();
    },
  );
  testWidgets(
    'unknown approve permits only read recovery never same token resend',
    (t) async {
      final auth = await egressAuth();
      var approves = 0, reads = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          return interestResponse(interestPreview());
        }
        if (r.url.path.endsWith('/approve')) {
          approves++;
          return http.Response('{}', 503);
        }
        reads++;
        return interestReads(r, state: approves > 0 ? 'PRIVATE' : null);
      });
      await t.pumpWidget(
        MaterialApp(
          home: PersonCommunityInterestPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      await interestTap(t, '合成公开社群');
      await interestTap(t, '检查兴趣声明');
      await interestTap(t, '批准这个版本');
      expect(find.textContaining('提交结果未知'), findsOneWidget);
      expect(approves, 1);
      final oldReads = reads;
      await interestTap(t, '只读核查当前声明');
      expect(reads, oldReads + 2);
      expect(find.textContaining('不能证明'), findsOneWidget);
      expect(approves, 1);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'hidden existing record only neutral source and real DELETE review no public button',
    (t) async {
      final auth = await egressAuth();
      var approves = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          return interestResponse(
            interestPreview('DELETE')
              ..['name'] = '该社群当前不可公开展示'
              ..['sourceAvailable'] = false,
          );
        }
        if (r.url.path.endsWith('/approve')) approves++;
        return interestResponse(
          interestView(
            state: r.url.path.endsWith('/options') ? null : 'PUBLIC',
            available: false,
          ),
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: PersonCommunityInterestPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('检查公开兴趣'), findsNothing);
      await interestTap(t, '检查撤回声明');
      expect(find.text('公开兴趣 → 没有声明'), findsOneWidget);
      await interestTap(t, '取消');
      expect(approves, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'large text narrow safe area keyboard reachable concrete approval and cancellation no clipping',
    (t) async {
      final auth = await egressAuth();
      var writes = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          return interestResponse(
            interestPreview()..['name'] = '具有很长中文名称的留学生社群兴趣自声明公开来源用于移动端检查',
          );
        }
        if (r.url.path.endsWith('/approve')) writes++;
        return interestReads(r);
      });
      t.view.physicalSize = const Size(360, 780);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      await t.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context).copyWith(
              textScaler: const TextScaler.linear(2),
              viewInsets: const EdgeInsets.only(bottom: 220),
            ),
            child: child!,
          ),
          home: PersonCommunityInterestPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://fixture',
          ),
        ),
      );
      await t.pumpAndSettle();
      await interestTap(t, '合成公开社群');
      await interestTap(t, '检查兴趣声明');
      expect(t.takeException(), isNull);
      await interestTap(t, '取消');
      expect(t.takeException(), isNull);
      expect(writes, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets('real wall expiry removes open dialog with zero approve', (
    t,
  ) async {
    final auth = await egressAuth();
    var writes = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/preview')) {
        final now = DateTime.now().toUtc();
        return interestResponse(
          interestPreview('PRIVATE', now)
            ..['expiresAt'] = now
                .add(const Duration(milliseconds: 200))
                .toIso8601String(),
        );
      }
      if (r.url.path.endsWith('/approve')) writes++;
      return interestReads(r);
    });
    await t.pumpWidget(
      MaterialApp(
        home: PersonCommunityInterestPage(
          auth: auth,
          client: client,
          apiBaseUrl: 'http://fixture',
        ),
      ),
    );
    await t.pumpAndSettle();
    await interestTap(t, '合成公开社群');
    await interestTap(t, '检查兴趣声明');
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 250)),
    );
    await t.pump(const Duration(seconds: 1));
    await t.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(writes, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
  });
}
