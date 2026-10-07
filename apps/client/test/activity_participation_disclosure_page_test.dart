import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/activity_participation_disclosure_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'model_egress_page_test.dart' show egressAuth;
import 'activity_participation_disclosure_api_test.dart';

class DisclosureBorrowedClient extends MockClient {
  DisclosureBorrowedClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

Future<void> disclosureTap(WidgetTester t, String label) async {
  final f = find.text(label);
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
  testWidgets(
    'duration selection retires on source deadline with zero preview request',
    (t) async {
      final auth = await egressAuth();
      var previews = 0;
      final deadline = DateTime.now().toUtc().add(const Duration(seconds: 2));
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          previews++;
        }
        final v = disclosureView();
        v['records'][0]['sourceExpiresAt'] = ds(deadline);
        return disclosureResponse(v);
      });
      await t.pumpWidget(
        MaterialApp(
          home: ActivityParticipationDisclosurePage(auth: auth, client: client),
        ),
      );
      await t.pumpAndSettle();
      await disclosureTap(t, '选择期限并检查公开报名');
      expect(find.byType(AlertDialog), findsOneWidget);
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 2100)),
      );
      await t.pump(const Duration(seconds: 1));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(previews, 0);
      expect(find.text('选择期限并检查公开报名'), findsNothing);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'workspace during parent build retires duration selection before any native preview',
    (t) async {
      final auth = await egressAuth();
      final workspace = ValueNotifier<String?>(null),
          rebuild = ValueNotifier<int>(0);
      var previews = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          previews++;
        }
        return disclosureResponse(disclosureView());
      });
      String? getter() => workspace.value;
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: rebuild,
            builder: (ctx, v, child) {
              if (v == 1 && workspace.value == null) {
                workspace.value = disclosureA;
              }
              return ActivityParticipationDisclosurePage(
                auth: auth,
                client: client,
                workspaceChanges: workspace,
                organizationWorkspaceID: getter,
              );
            },
          ),
        ),
      );
      await t.pumpAndSettle();
      await disclosureTap(t, '选择期限并检查公开报名');
      expect(find.byType(AlertDialog), findsOneWidget);
      rebuild.value = 1;
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
      expect(find.byType(AlertDialog), findsNothing);
      expect(previews, 0);
      await t.pumpWidget(const SizedBox());
      workspace.dispose();
      rebuild.dispose();
      auth.dispose();
    },
  );

  testWidgets(
    'workspace notification during parent build retires live preview without ancestor Navigator mutation',
    (t) async {
      final auth = await egressAuth();
      final workspace = ValueNotifier<String?>(null),
          rebuild = ValueNotifier<int>(0);
      var approvals = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          return disclosureResponse(disclosurePreview('PRIVATE'));
        }
        if (r.url.path.endsWith('/approve')) {
          approvals++;
        }
        return disclosureResponse(disclosureView(state: 'PUBLIC'));
      });
      String? getter() => workspace.value;
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<int>(
            valueListenable: rebuild,
            builder: (ctx, v, child) {
              if (v == 1 && workspace.value == null) {
                workspace.value = disclosureA;
              }
              return ActivityParticipationDisclosurePage(
                auth: auth,
                client: client,
                workspaceChanges: workspace,
                organizationWorkspaceID: getter,
              );
            },
          ),
        ),
      );
      await t.pumpAndSettle();
      await disclosureTap(t, '检查设为仅自己可见');
      expect(find.byType(AlertDialog), findsOneWidget);
      rebuild.value = 1;
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
      expect(find.byType(AlertDialog), findsNothing);
      expect(approvals, 0);
      await t.pumpWidget(const SizedBox());
      workspace.dispose();
      rebuild.dispose();
      auth.dispose();
    },
  );

  testWidgets(
    'native concrete preview expires closes dialog and never approves old version',
    (t) async {
      final auth = await egressAuth();
      var approvals = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          final p = disclosurePreview('PRIVATE');
          p['expiresAt'] = ds(
            DateTime.now().add(const Duration(milliseconds: 500)),
          );
          return disclosureResponse(p);
        }
        if (r.url.path.endsWith('/approve')) {
          approvals++;
        }
        return disclosureResponse(disclosureView(state: 'PUBLIC'));
      });
      await t.pumpWidget(
        MaterialApp(
          home: ActivityParticipationDisclosurePage(auth: auth, client: client),
        ),
      );
      await t.pumpAndSettle();
      await disclosureTap(t, '检查设为仅自己可见');
      expect(find.byType(AlertDialog), findsOneWidget);
      expect(
        t.getSize(find.byType(FilledButton)).height,
        greaterThanOrEqualTo(48),
      );
      await t.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 650)),
      );
      await t.pump(const Duration(seconds: 1));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('具体预览已到期，请重新检查。'), findsOneWidget);
      expect(approvals, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'unknown submit reconciles with read only no automatic approve repeat',
    (t) async {
      final auth = await egressAuth();
      var approvals = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          return disclosureResponse(disclosurePreview('PRIVATE'));
        }
        if (r.url.path.endsWith('/approve')) {
          approvals++;
          throw StateError('unknown network');
        }
        return disclosureResponse(disclosureView(state: 'PUBLIC'));
      });
      await t.pumpWidget(
        MaterialApp(
          home: ActivityParticipationDisclosurePage(auth: auth, client: client),
        ),
      );
      await t.pumpAndSettle();
      await disclosureTap(t, '检查设为仅自己可见');
      await disclosureTap(t, '批准这个版本');
      expect(approvals, 1);
      expect(find.text('只读核查当前报名声明'), findsOneWidget);
      await disclosureTap(t, '只读核查当前报名声明');
      expect(find.textContaining('不能证明先前未知提交'), findsOneWidget);
      expect(approvals, 1);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );

  testWidgets(
    'PUBLIC approve then PRIVATE withdrawal without refresh uses new concrete preview',
    (t) async {
      final auth = await egressAuth();
      var current = 'PRIVATE';
      DateTime? target;
      final ops = <String>[];
      var approvals = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          final b = jsonDecode(r.body);
          ops.add(b['operation']);
          target = b['disclosureExpiresAt'] == null
              ? null
              : DateTime.parse(b['disclosureExpiresAt']);
          final p = disclosurePreview(b['operation'], expiry: target);
          p.addAll(disclosureRecord(state: current, expiry: target));
          return disclosureResponse(p);
        }
        if (r.url.path.endsWith('/approve')) {
          approvals++;
          current = ops.last;
          return disclosureResponse(
            disclosureView(state: current, expiry: target),
          );
        }
        return disclosureResponse(disclosureView());
      });
      await t.pumpWidget(
        MaterialApp(
          home: ActivityParticipationDisclosurePage(auth: auth, client: client),
        ),
      );
      await t.pumpAndSettle();
      await disclosureTap(t, '选择期限并检查公开报名');
      await disclosureTap(t, '1小时（不超过活动有效期）');
      await disclosureTap(t, '批准这个版本');
      expect(find.text('当前公开'), findsOneWidget);
      expect(approvals, 1);
      await disclosureTap(t, '检查设为仅自己可见');
      await disclosureTap(t, '批准这个版本');
      expect(ops, ['PUBLIC', 'PRIVATE']);
      expect(approvals, 2);
      expect(find.text('仅自己可见'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'dispose while own dialog active removes owned overlay and closes borrowed client zero times',
    (t) async {
      final auth = await egressAuth();
      final visible = ValueNotifier<bool>(true);
      var approvals = 0;
      final client = DisclosureBorrowedClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          return disclosureResponse(disclosurePreview('PRIVATE'));
        }
        if (r.url.path.endsWith('/approve')) {
          approvals++;
        }
        return disclosureResponse(disclosureView(state: 'PUBLIC'));
      });
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<bool>(
            valueListenable: visible,
            builder: (c, v, child) => v
                ? ActivityParticipationDisclosurePage(
                    auth: auth,
                    client: client,
                  )
                : const SizedBox(),
          ),
        ),
      );
      await t.pumpAndSettle();
      await disclosureTap(t, '检查设为仅自己可见');
      expect(find.byType(AlertDialog), findsOneWidget);
      visible.value = false;
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(t.takeException(), isNull);
      expect(approvals, 0);
      expect(client.closes, 0);
      await t.pumpWidget(const SizedBox());
      visible.dispose();
      auth.dispose();
    },
  );

  testWidgets(
    'explicit duration then concrete preview cancel and back zero approval; narrow large keyboard no overflow',
    (t) async {
      final auth = await egressAuth();
      var previews = 0, approves = 0;
      final client = DisclosureBorrowedClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          previews++;
          final b = jsonDecode(r.body);
          return disclosureResponse(
            disclosurePreview(
              b['operation'],
              expiry: b['disclosureExpiresAt'] == null
                  ? null
                  : DateTime.parse(b['disclosureExpiresAt']),
            ),
          );
        }
        if (r.url.path.endsWith('/approve')) {
          approves++;
          return disclosureResponse(disclosureView());
        }
        return disclosureResponse(disclosureView());
      });
      t.view.physicalSize = const Size(360, 640);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      await t.pumpWidget(
        MaterialApp(
          builder: (c, child) => MediaQuery(
            data: MediaQuery.of(c).copyWith(
              textScaler: const TextScaler.linear(1.8),
              viewInsets: const EdgeInsets.only(bottom: 240),
            ),
            child: child!,
          ),
          home: ActivityParticipationDisclosurePage(
            auth: auth,
            client: client,
            apiBaseUrl: 'https://A.example',
          ),
        ),
      );
      await t.pumpAndSettle();
      await disclosureTap(t, '选择期限并检查公开报名');
      expect(previews, 0);
      await disclosureTap(t, '1小时（不超过活动有效期）');
      expect(previews, 1);
      expect(find.textContaining('公开报名截止'), findsOneWidget);
      expect(find.text('本人报名：已报名'), findsOneWidget);
      expect(find.textContaining('活动开始：'), findsOneWidget);
      expect(find.textContaining('活动结束：'), findsOneWidget);
      expect(find.textContaining('受众：允许查看当前公开来源的用户'), findsOneWidget);
      expect(find.textContaining('不代表实际到场'), findsWidgets);
      expect(t.takeException(), isNull);
      await disclosureTap(t, '取消');
      expect(approves, 0);
      await disclosureTap(t, '选择期限并检查公开报名');
      await t.binding.handlePopRoute();
      await t.pumpAndSettle();
      expect(previews, 1);
      expect(approves, 0);
      await t.pumpWidget(const SizedBox());
      await t.pump();
      expect(client.closes, 0);
      auth.dispose();
    },
  );
  testWidgets(
    'same key transport A B A retires preview and never sends captured approval',
    (t) async {
      final auth = await egressAuth();
      var approves = 0;
      final a = DisclosureBorrowedClient((r) async {
        if (r.url.path.endsWith('/preview')) {
          return disclosureResponse(disclosurePreview('PRIVATE'));
        }
        if (r.url.path.endsWith('/approve')) {
          approves++;
          return disclosureResponse(disclosureView());
        }
        return disclosureResponse(disclosureView(state: 'PUBLIC'));
      });
      final b = DisclosureBorrowedClient(
        (r) async => disclosureResponse(disclosureView(empty: true)),
      );
      Widget page(http.Client client, String base) => MaterialApp(
        home: ActivityParticipationDisclosurePage(
          key: const ValueKey('same'),
          auth: auth,
          client: client,
          apiBaseUrl: base,
        ),
      );
      await t.pumpWidget(page(a, 'https://a.example'));
      await t.pumpAndSettle();
      await disclosureTap(t, '检查设为仅自己可见');
      expect(find.byType(AlertDialog), findsOneWidget);
      await t.pumpWidget(page(b, 'https://b.example'));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(page(a, 'https://a.example'));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(approves, 0);
      expect(a.closes, 0);
      expect(b.closes, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
  testWidgets(
    'same-valued getter/listener replacement retires duration dialog; workspace ABA fails closed',
    (t) async {
      final auth = await egressAuth();
      final first = ValueNotifier<String?>(null),
          second = ValueNotifier<String?>(null);
      var previews = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/preview')) previews++;
        return disclosureResponse(disclosureView());
      });
      String? getFirst() => first.value;
      String? getSecond() => second.value;
      Widget page(ValueNotifier<String?> changes, String? Function() getter) =>
          MaterialApp(
            home: ActivityParticipationDisclosurePage(
              key: const ValueKey('same'),
              auth: auth,
              client: client,
              workspaceChanges: changes,
              organizationWorkspaceID: getter,
            ),
          );
      await t.pumpWidget(page(first, getFirst));
      await t.pumpAndSettle();
      await disclosureTap(t, '选择期限并检查公开报名');
      await t.pumpWidget(page(second, getSecond));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(t.takeException(), isNull);
      await disclosureTap(t, '选择期限并检查公开报名');
      second.value = disclosureA;
      second.value = null;
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(previews, 0);
      expect(find.textContaining('旧预览已失效'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      first.dispose();
      second.dispose();
      auth.dispose();
    },
  );
  testWidgets(
    'late preview after transport replacement cannot open stale dialog',
    (t) async {
      final auth = await egressAuth();
      final delayed = Completer<http.Response>();
      final a = MockClient(
        (r) => r.url.path.endsWith('/preview')
            ? delayed.future
            : Future.value(disclosureResponse(disclosureView(state: 'PUBLIC'))),
      );
      final b = MockClient(
        (r) async => disclosureResponse(disclosureView(empty: true)),
      );
      Widget page(http.Client client) => MaterialApp(
        home: ActivityParticipationDisclosurePage(
          key: const ValueKey('same'),
          auth: auth,
          client: client,
        ),
      );
      await t.pumpWidget(page(a));
      await t.pumpAndSettle();
      await t.tap(find.text('检查设为仅自己可见'));
      await t.pump();
      await t.pumpWidget(page(b));
      await t.pumpAndSettle();
      delayed.complete(disclosureResponse(disclosurePreview('PRIVATE')));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );
}
