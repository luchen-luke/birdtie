import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/close_connections_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'close_connections_api_test.dart';

const closeLongName = '林同学一起探索阿伯丁周末城市生活 Aberdeen 混排名字';
Future<void> closeTap(WidgetTester t, String label) async {
  final f = find.text(label);
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  expect(f.hitTestable(), findsOneWidget);
  await t.tap(f);
  await t.pumpAndSettle();
}

Future<void> closeSnapshot(WidgetTester t, String name) async {
  final target = Platform.environment['BIRDTIE_VISIBILITY_WIDGET_DIR'];
  if (target == null) return;
  await t.pump();
  await t.runAsync(() async {
    final render = t.renderObject<RenderRepaintBoundary>(
      find.byKey(const Key('visibility-capture')),
    );
    final picture = await render.toImage(pixelRatio: 1);
    final bytes = await picture.toByteData(format: ui.ImageByteFormat.png);
    picture.dispose();
    final file = File('$target/$name.png');
    await file.parent.create(recursive: true);
    await file.writeAsBytes(bytes!.buffer.asUint8List());
  });
}

Widget closeHarness(
  SeedTestAuth auth,
  http.Client client, {
  double scale = 1,
  Brightness brightness = Brightness.light,
  ValueNotifier<String?>? workspace,
  String base = 'http://synthetic.test',
  Listenable? changes,
}) => MaterialApp(
  debugShowCheckedModeBanner: false,
  theme: birdtieTheme(brightness),
  builder: (context, child) => MediaQuery(
    data: MediaQuery.of(context).copyWith(textScaler: TextScaler.linear(scale)),
    child: RepaintBoundary(key: const Key('visibility-capture'), child: child!),
  ),
  home: CloseConnectionsPage(
    key: const ValueKey('same-close-page'),
    auth: auth,
    client: client,
    apiBaseUrl: base,
    workspaceChanges: changes ?? workspace,
    organizationWorkspaceID: workspace == null ? null : () => workspace.value,
  ),
);
SeedTestAuth closeAuth() => SeedTestAuth()
  ..owner = closeOwner
  ..token = 'Bearer SYNTHETIC'
  ..label = '本人';

Future<void> loadVisibilityFonts() async {
  if (Platform.isWindows) {
    for (final family in ['Roboto', 'VisibilityTestChinese']) {
      final font = FontLoader(family)
        ..addFont(
          Future.value(
            ByteData.sublistView(
              File(r'C:\Windows\Fonts\msyh.ttc').readAsBytesSync(),
            ),
          ),
        );
      await font.load();
    }
    final icons = FontLoader('MaterialIcons')
      ..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'));
    await icons.load();
  }
}

void main() {
  setUpAll(loadVisibilityFonts);
  testWidgets('取消/返回零写，确认一次PUT+GET后可明确撤销DELETE+GET', (t) async {
    final auth = closeAuth();
    addTearDown(auth.dispose);
    var selected = false, revision = 0;
    final calls = <http.Request>[];
    final c = MockClient((r) async {
      calls.add(r);
      final now = DateTime.now().toUtc();
      if (r.method == 'PUT') {
        selected = true;
        revision = 1;
        return closeJSON(closeResult(now: now));
      }
      if (r.method == 'DELETE') {
        selected = false;
        revision = 2;
        return http.Response('', 204);
      }
      return closeJSON(
        closeList(
          now: now,
          entries: [closeEntry(selected: selected, revision: revision)],
        ),
      );
    });
    addTearDown(c.close);
    await t.pumpWidget(closeHarness(auth, c));
    await t.pumpAndSettle();
    await closeTap(t, '设为亲密好友');
    expect(find.text('设为亲密好友？'), findsOneWidget);
    await closeTap(t, '取消');
    expect(calls.every((v) => v.method == 'GET'), true);
    await closeTap(t, '设为亲密好友');
    await t.binding.handlePopRoute();
    await t.pumpAndSettle();
    expect(calls.every((v) => v.method == 'GET'), true);
    await closeTap(t, '设为亲密好友');
    await closeTap(t, '确认设为亲密好友');
    expect(calls.map((v) => v.method), ['GET', 'PUT', 'GET']);
    expect(find.text('已选为亲密好友'), findsOneWidget);
    await closeTap(t, '撤销亲密好友');
    await closeTap(t, '确认撤销亲密好友');
    expect(calls.map((v) => v.method), ['GET', 'PUT', 'GET', 'DELETE', 'GET']);
    expect(
      jsonDecode(
        calls.lastWhere((v) => v.method == 'DELETE').body,
      )['expectedRevision'],
      1,
    );
    expect(find.text('当前好友'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('UNKNOWN写只核对GET，加载/空/失败有可用直接恢复', (t) async {
    final auth = closeAuth();
    addTearDown(auth.dispose);
    var gets = 0;
    final methods = <String>[];
    final c = MockClient((r) async {
      methods.add(r.method);
      if (r.method == 'PUT') throw TimeoutException('SYNTHETIC');
      gets++;
      if (gets == 2) return http.Response('', 503);
      return closeJSON(
        closeList(now: DateTime.now().toUtc(), entries: gets > 2 ? [] : null),
      );
    });
    addTearDown(c.close);
    await t.pumpWidget(closeHarness(auth, c));
    await t.pumpAndSettle();
    await closeTap(t, '设为亲密好友');
    await closeTap(t, '确认设为亲密好友');
    expect(find.textContaining('不会自动重发'), findsOneWidget);
    final row = t.widget<OutlinedButton>(
      find.byKey(const ValueKey('close-person:$closePeer')),
    );
    expect(row.onPressed, isNull);
    await closeTap(t, '核对当前名单');
    expect(find.textContaining('暂时无法核对'), findsOneWidget);
    await closeTap(t, '核对当前名单');
    expect(find.textContaining('还没有当前好友'), findsOneWidget);
    expect(methods, ['GET', 'PUT', 'GET', 'GET']);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });
  for (final mode in ['account', 'token', 'workspace']) {
    testWidgets('打开具体确认后$mode ABA关闭旧确认无隐式写', (t) async {
      final auth = closeAuth();
      addTearDown(auth.dispose);
      final workspace = ValueNotifier<String?>(null);
      addTearDown(workspace.dispose);
      final methods = <String>[];
      final c = MockClient((r) async {
        methods.add(r.method);
        return closeJSON(closeList(now: DateTime.now().toUtc()));
      });
      addTearDown(c.close);
      await t.pumpWidget(closeHarness(auth, c, workspace: workspace));
      await t.pumpAndSettle();
      await closeTap(t, '设为亲密好友');
      if (mode == 'workspace') {
        workspace.value = closeTie;
        workspace.value = null;
      } else {
        auth.changeIdentity(
          'Bearer B',
          nextOwner: mode == 'token' ? closeOwner : closePeer,
        );
        auth.changeIdentity('Bearer SYNTHETIC', nextOwner: closeOwner);
      }
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.textContaining('请返回并重新打开'), findsOneWidget);
      expect(methods, ['GET']);
      await t.pumpWidget(const SizedBox());
    });
  }
  testWidgets('samekey transport A→B→A退役原确认，不关闭借用client', (t) async {
    final auth = closeAuth();
    addTearDown(auth.dispose);
    final a = CloseBorrowedClient(
          (_) async => closeJSON(closeList(now: DateTime.now().toUtc())),
        ),
        b = CloseBorrowedClient(
          (_) async => closeJSON(closeList(now: DateTime.now().toUtc())),
        );
    addTearDown(a.close);
    addTearDown(b.close);
    await t.pumpWidget(closeHarness(auth, a));
    await t.pumpAndSettle();
    await closeTap(t, '设为亲密好友');
    await t.pumpWidget(closeHarness(auth, b, base: 'http://b.test'));
    await t.pumpAndSettle();
    await t.pumpWidget(closeHarness(auth, a));
    await t.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(a.closes, 0);
    expect(b.closes, 0);
    await t.pumpWidget(const SizedBox());
    expect(a.closes, 0);
    expect(b.closes, 0);
  });
  testWidgets('到期来源只能刷新再检查，不能用旧范围提交', (t) async {
    final auth = closeAuth();
    addTearDown(auth.dispose);
    var gets = 0;
    final c = MockClient((_) async {
      gets++;
      return closeJSON(
        closeList(
          now: DateTime.now().toUtc().subtract(
            Duration(seconds: gets == 1 ? 31 : 0),
          ),
        ),
      );
    });
    addTearDown(c.close);
    await t.pumpWidget(closeHarness(auth, c));
    await t.pumpAndSettle();
    expect(find.textContaining('当前关系核验已到期'), findsOneWidget);
    expect(
      t
          .widget<OutlinedButton>(
            find.byKey(const ValueKey('close-person:$closePeer')),
          )
          .onPressed,
      isNull,
    );
    await closeTap(t, '刷新后继续');
    expect(
      t
          .widget<OutlinedButton>(
            find.byKey(const ValueKey('close-person:$closePeer')),
          )
          .onPressed,
      isNotNull,
    );
    expect(gets, 2);
    await t.pumpWidget(const SizedBox());
  });
  for (final setting in [
    (1.0, Brightness.light),
    (1.3, Brightness.light),
    (2.0, Brightness.light),
    (2.0, Brightness.dark),
  ]) {
    testWidgets('真实Close生产页320dp长中文混排${setting.$1} ${setting.$2}滚动确认48触控', (
      t,
    ) async {
      t.view.physicalSize = const Size(320, 720);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final auth = closeAuth();
      addTearDown(auth.dispose);
      var selected = false;
      final c = MockClient((r) async {
        final now = DateTime.now().toUtc();
        if (r.method == 'PUT') {
          selected = true;
          return closeJSON(
            closeResult(
              now: now,
              entry: closeEntry(selected: true, name: closeLongName),
            ),
          );
        }
        return closeJSON(
          closeList(
            now: now,
            entries: [closeEntry(selected: selected, name: closeLongName)],
          ),
        );
      });
      addTearDown(c.close);
      await t.pumpWidget(
        closeHarness(auth, c, scale: setting.$1, brightness: setting.$2),
      );
      await t.pumpAndSettle();
      final suffix = '${(setting.$1 * 100).round()}-${setting.$2.name}';
      await closeSnapshot(t, 'close-list-top-$suffix');
      await t.ensureVisible(find.text('设为亲密好友'));
      await t.pumpAndSettle();
      await closeSnapshot(t, 'close-list-action-$suffix');
      final row = find.byKey(const ValueKey('close-person:$closePeer'));
      expect(t.getSize(row).height, greaterThanOrEqualTo(48));
      await closeTap(t, '设为亲密好友');
      await closeSnapshot(t, 'close-confirm-$suffix');
      await t.scrollUntilVisible(
        find.textContaining('只管理你自己的可见范围'),
        100,
        scrollable: find
            .descendant(
              of: find.byType(AlertDialog),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      await t.pumpAndSettle();
      await closeSnapshot(t, 'close-confirm-consequence-$suffix');
      final confirm = find.widgetWithText(FilledButton, '确认设为亲密好友');
      expect(t.getSize(confirm).height, greaterThanOrEqualTo(48));
      await closeTap(t, '确认设为亲密好友');
      expect(find.textContaining('当前亲密好友名单已更新'), findsOneWidget);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    });
  }
}
