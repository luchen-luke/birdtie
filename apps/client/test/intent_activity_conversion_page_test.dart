import 'dart:async';
import 'chat_entity_router_test.dart' show chatActivity, chatReply;
import 'package:birdtie_client/src/workspace/intent_activity_conversion_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'active_social_intent_api_test.dart' show nowIntent;
import 'intent_activity_conversion_api_test.dart';
import 'model_egress_page_test.dart' show egressAuth;

Future<void> conversionTap(WidgetTester t, String text) async {
  final f = find.text(text);
  if (find.byType(AlertDialog).evaluate().isEmpty) {
    await t.scrollUntilVisible(
      f,
      120,
      scrollable: find.byType(Scrollable).first,
    );
  }
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  await t.tap(f);
  await t.pumpAndSettle();
}

void main() {
  for (final changed in [false, true]) {
    testWidgets(
      'successful original association opens original activity detail with borrowed transport and retires late detail $changed',
      (t) async {
        final auth = await egressAuth();
        final workspace = ValueNotifier<String?>(null);
        String? getter() => workspace.value;
        final pending = Completer<http.Response>();
        var approvals = 0, domainWrites = 0;
        final detailPaths = <String>[];
        final preview = conversionPreview();
        final client = ConversionBorrowedClient((r) async {
          if (r.url.path.contains('/activity-conversion')) {
            if (r.url.path.endsWith('/approve')) {
              approvals++;
              return conversionResponse(conversionReceipt(preview));
            }
            return conversionResponse(
              r.url.path.endsWith('/preview') ? preview : conversionList(),
            );
          }
          if (r.method != 'GET') domainWrites++;
          if (r.url.path == '/v1/activities/$conversionActivity') {
            detailPaths.add(r.url.path);
            return changed
                ? pending.future
                : conversionResponse({
                    ...chatActivity(),
                    'id': conversionActivity,
                    'title': '原活动详情实际页面',
                  });
          }
          if (r.url.path.endsWith('/participations/me')) return chatReply(null);
          if (r.url.path == '/v1/me/saved') return chatReply([]);
          return http.Response('{}', 503);
        });
        await t.pumpWidget(
          MaterialApp(
            home: IntentActivityConversionPage(
              auth: auth,
              intentID: nowIntent,
              client: client,
              apiBaseUrl: 'http://127.0.0.1:9999',
              workspaceChanges: workspace,
              organizationWorkspaceID: getter,
            ),
          ),
        );
        await t.pumpAndSettle();
        await conversionTap(t, '检查关联此活动');
        await conversionTap(t, '确认关联');
        expect(find.textContaining('已关联之前报名的活动'), findsOneWidget);
        expect(find.text('原已报名线上活动'), findsOneWidget);
        await conversionTap(t, '查看原活动详情');
        expect(
          detailPaths,
          List.filled(changed ? 1 : 2, '/v1/activities/$conversionActivity'),
        );
        if (changed) {
          workspace.value = '33333333-3333-4333-8333-333333333333';
          workspace.value = null;
          await t.pump();
          pending.complete(
            conversionResponse({
              ...chatActivity(),
              'id': conversionActivity,
              'title': '不可恢复旧私密标题',
            }),
          );
          await t.pumpAndSettle();
          expect(find.text('不可恢复旧私密标题'), findsNothing);
        } else {
          expect(find.text('原活动详情实际页面'), findsWidgets);
          await t.binding.handlePopRoute();
          await t.pumpAndSettle();
        }
        expect(approvals, 1);
        expect(domainWrites, 0);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        expect(client.closes, 0);
        workspace.dispose();
        auth.dispose();
      },
    );
  }
  testWidgets('real selected Chinese review cancellation/back zero approve', (
    t,
  ) async {
    final auth = await egressAuth();
    var approvals = 0;
    final c = MockClient((r) async {
      if (r.url.path.endsWith('/approve')) approvals++;
      return conversionResponse(
        r.url.path.endsWith('/preview')
            ? conversionPreview()
            : conversionList(),
      );
    });
    await t.pumpWidget(
      MaterialApp(
        home: IntentActivityConversionPage(
          auth: auth,
          intentID: nowIntent,
          client: c,
        ),
      ),
    );
    await t.pumpAndSettle();
    await conversionTap(t, '检查关联此活动');
    expect(find.text('检查这次意图关联'), findsOneWidget);
    expect(find.textContaining('原受众：仅自己'), findsOneWidget);
    expect(find.textContaining('你的设备时间'), findsWidgets);
    expect(find.textContaining('不代表实际到场'), findsOneWidget);
    await conversionTap(t, '取消');
    expect(approvals, 0);
    await conversionTap(t, '检查关联此活动');
    await t.binding.handlePopRoute();
    await t.pumpAndSettle();
    expect(approvals, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
  });
  testWidgets(
    'same-key client/base/getter/listener replacement closes old review and no A through B',
    (t) async {
      final auth = await egressAuth();
      var posts = 0;
      final hosts = <String>[];
      final c = ConversionBorrowedClient((r) async {
        hosts.add(r.url.host);
        if (r.url.path.endsWith('/approve')) posts++;
        return conversionResponse(
          r.url.path.endsWith('/preview')
              ? conversionPreview()
              : conversionList(),
        );
      });
      final workspace = ValueNotifier<String?>(null);
      String? getter() => workspace.value;
      Widget page(
        String base, {
        http.Client? client,
        String? Function()? read,
      }) => MaterialApp(
        home: IntentActivityConversionPage(
          key: const ValueKey('same'),
          auth: auth,
          intentID: nowIntent,
          client: client ?? c,
          apiBaseUrl: base,
          workspaceChanges: workspace,
          organizationWorkspaceID: read ?? getter,
        ),
      );
      await t.pumpWidget(page('http://A'));
      await t.pumpAndSettle();
      await conversionTap(t, '检查关联此活动');
      await t.pumpWidget(page('http://B'));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      await t.pumpWidget(page('http://A'));
      await t.pumpAndSettle();
      expect(posts, 0);
      expect(hosts, containsAll(['a', 'b']));
      await conversionTap(t, '检查关联此活动');
      await t.pumpWidget(page('http://A', read: () => workspace.value));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(posts, 0);
      await t.pumpWidget(const SizedBox());
      expect(c.closes, 0);
      workspace.dispose();
      auth.dispose();
    },
  );
  testWidgets(
    'workspace ABA during ancestor build removes concrete dialog without Navigator build error',
    (t) async {
      final auth = await egressAuth();
      final workspace = ValueNotifier<String?>(null);
      var approvals = 0, change = false;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/approve')) approvals++;
        return conversionResponse(
          r.url.path.endsWith('/preview')
              ? conversionPreview()
              : conversionList(),
        );
      });
      late StateSetter rebuild;
      String? getter() => workspace.value;
      await t.pumpWidget(
        MaterialApp(
          home: StatefulBuilder(
            builder: (context, setState) {
              rebuild = setState;
              if (change) {
                change = false;
                workspace.value = '33333333-3333-4333-8333-333333333333';
                workspace.value = null;
              }
              return IntentActivityConversionPage(
                auth: auth,
                intentID: nowIntent,
                client: c,
                workspaceChanges: workspace,
                organizationWorkspaceID: getter,
              );
            },
          ),
        ),
      );
      await t.pumpAndSettle();
      await conversionTap(t, '检查关联此活动');
      rebuild(() => change = true);
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.textContaining('重新打开'), findsWidgets);
      expect(approvals, 0);
      await t.pumpWidget(const SizedBox());
      workspace.dispose();
      auth.dispose();
    },
  );
  for (final keyboard in [200.0, 260.0]) {
    testWidgets(
      '320 font3 IME$keyboard long time and preview has reachable 48dp actions',
      (t) async {
        t.view.physicalSize = const Size(320, 640);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final auth = await egressAuth();
        final c = MockClient((r) async {
          final m = r.url.path.endsWith('/preview')
              ? conversionPreview()
              : conversionList();
          m['intent']['title'] = '本人明确寻找线上活动且保留原已报名的长标题' * 3;
          return conversionResponse(m);
        });
        await t.pumpWidget(
          MaterialApp(
            builder: (context, child) => MediaQuery(
              data: MediaQuery.of(context).copyWith(
                textScaler: const TextScaler.linear(3),
                viewInsets: EdgeInsets.only(bottom: keyboard),
              ),
              child: child!,
            ),
            home: IntentActivityConversionPage(
              auth: auth,
              intentID: nowIntent,
              client: c,
            ),
          ),
        );
        await t.pumpAndSettle();
        await conversionTap(t, '检查关联此活动');
        expect(t.takeException(), isNull);
        final confirm = find.widgetWithText(FilledButton, '确认关联');
        await t.ensureVisible(confirm);
        await t.pumpAndSettle();
        expect(t.getSize(confirm).height, greaterThanOrEqualTo(48));
        expect(t.getRect(confirm).bottom, lessThanOrEqualTo(640 - keyboard));
        final cancel = find.widgetWithText(TextButton, '取消');
        expect(t.getSize(cancel).height, greaterThanOrEqualTo(48));
        await conversionTap(t, '取消');
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        auth.dispose();
      },
    );
  }
  testWidgets(
    'late preview after dispose does not restore private payload or approval',
    (t) async {
      final auth = await egressAuth();
      final pending = Completer<http.Response>();
      var approvals = 0;
      final c = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) return pending.future;
        if (r.url.path.endsWith('/approve')) approvals++;
        return conversionResponse(conversionList());
      });
      await t.pumpWidget(
        MaterialApp(
          home: IntentActivityConversionPage(
            auth: auth,
            intentID: nowIntent,
            client: c,
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.tap(find.text('检查关联此活动'));
      await t.pump();
      await t.pumpWidget(const SizedBox());
      pending.complete(conversionResponse(conversionPreview()));
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
      expect(find.byType(AlertDialog), findsNothing);
      expect(approvals, 0);
      auth.dispose();
    },
  );
}
