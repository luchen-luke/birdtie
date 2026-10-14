import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class _MaterialFixture {
  final workspace = AgentWorkspaceController();
  final scope = ValueNotifier('owner-A');
  final capability = ValueNotifier(0); // unavailable, restoring, ready
  final loginNeeded = ValueNotifier(false);
  final key = GlobalKey<AgentComposerState>();
  final actions = <String>[];
  final sent = <String>[];
  Future<void> mount(
    WidgetTester t, {
    double scale = 1,
    bool login = false,
  }) async {
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: TextScaler.linear(scale)),
          child: child!,
        ),
        home: Scaffold(
          body: AnimatedBuilder(
            animation: Listenable.merge([capability, loginNeeded]),
            builder: (context, _) {
              final state = capability.value;
              return AgentComposer(
                key: key,
                workspace: workspace,
                onSubmit: sent.add,
                onSearchArea: () {},
                hasSearchArea: false,
                materialContext: () => scope.value,
                materialContextChanges: scope,
                draftIdentity: scope.value,
                imageMaterialStatus: state == 0 ? '当前来源暂时无法使用图片' : null,
                imageMaterialRecovering: state == 1,
                onDraftImage: state == 2 ? () => actions.add('image') : null,
                onCaptureImage: state == 2 ? () => actions.add('camera') : null,
                onImageFile: state == 2 ? () => actions.add('file') : null,
                onRefreshImageMaterial: () => actions.add('recover-image'),
                needsLogin: loginNeeded.value,
                onSignIn: () {
                  actions.add('login');
                  // This fixture restores the same owner's editor, never a new
                  // recipient, picker callback, upload or source approval.
                  loginNeeded.value = false;
                },
                onVideoMaterial: () => actions.add('video'),
                onCreateMoment: () => actions.add('moment'),
                onCreateActivity: () => actions.add('activity'),
                onCreateJourney: () => actions.add('journey'),
                onCreateThemeMap: () => actions.add('theme'),
                onSavePlace: () => actions.add('place'),
              );
            },
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField), '原未发送草稿');
    expect(
      t.widget<TextField>(find.byType(TextField)).controller!.text,
      '原未发送草稿',
    );
    if (login) {
      // Establish the original draft while editing is actually enabled. The
      // login gate is a later presentation state for this same owner.
      loginNeeded.value = true;
      await t.pumpAndSettle();
      expect(t.widget<TextField>(find.byType(TextField)).enabled, isFalse);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        '原未发送草稿',
      );
    }
  }

  Future<void> open(WidgetTester t) async {
    await t.tap(find.byTooltip('添加材料'));
    await t.pumpAndSettle();
  }

  ListTile tile(WidgetTester t, String text) => t.widget<ListTile>(
    find.ancestor(of: find.text(text), matching: find.byType(ListTile)).first,
  );
  Future<void> finish(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    workspace.dispose();
    scope.dispose();
    capability.dispose();
    loginNeeded.dispose();
    expect(t.takeException(), isNull);
  }
}

void main() {
  for (final (title, action) in [('拍照', 'camera'), ('文件', 'file')]) {
    testWidgets('$title仍在原材料菜单，只调用当前源动作且保留未发送文字', (t) async {
      final f = _MaterialFixture()..capability.value = 2;
      await f.mount(t);
      await f.open(t);
      await t.scrollUntilVisible(
        find.text(title),
        160,
        scrollable: find.byType(Scrollable).last,
      );
      await t.tap(find.text(title));
      await t.pumpAndSettle();
      expect(f.actions, [action]);
      expect(f.sent, isEmpty);
      expect(f.workspace.task, isNull);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        '原未发送草稿',
      );
      expect(t.takeException(), isNull);
      await f.finish(t);
    });
  }
  for (final width in [360.0, 390.0]) {
    testWidgets('recording materials one grouped sheet $width 大字每项可达且只进草稿', (
      t,
    ) async {
      t.view.physicalSize = Size(width, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final f = _MaterialFixture();
      f.capability.value = 2;
      await f.mount(t, scale: 2);
      await f.open(t);
      expect(find.text('选择材料'), findsOneWidget);
      expect(find.text('创建草稿'), findsOneWidget);
      expect(find.text('创建'), findsNothing);
      for (final label in ['图片', '视频', '动态', '活动', '旅程', '主题地图', '收藏地点']) {
        await t.ensureVisible(find.text(label));
        await t.pumpAndSettle();
        expect(find.text(label).hitTestable(), findsOneWidget);
        final tile = find
            .ancestor(of: find.text(label), matching: find.byType(ListTile))
            .first;
        expect(t.getSize(tile).height, greaterThanOrEqualTo(48));
      }
      await t.ensureVisible(find.text('旅程'));
      await t.pumpAndSettle();
      await t.tap(find.text('旅程'));
      await t.pumpAndSettle();
      expect(f.actions, ['journey']);
      expect(f.sent, isEmpty);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        '原未发送草稿',
      );
      await f.finish(t);
    });
  }
  testWidgets(
    'recording image unavailable/restoring/ready stays visible with real recovery',
    (t) async {
      final f = _MaterialFixture();
      await f.mount(t);
      await f.open(t);
      expect(find.text('图片'), findsOneWidget);
      expect(find.text('当前来源暂时无法使用图片'), findsOneWidget);
      expect(f.tile(t, '图片').enabled, isFalse);
      f.capability.value = 1;
      await t.pumpAndSettle();
      expect(find.text('图片'), findsOneWidget);
      expect(find.text('正在恢复图片入口…'), findsOneWidget);
      expect(f.tile(t, '图片').onTap, isNull);
      expect(f.tile(t, '重新读取图片能力').onTap, isNull);
      f.capability.value = 0;
      await t.pumpAndSettle();
      await t.tap(find.text('重新读取图片能力'));
      await t.pumpAndSettle();
      expect(f.actions, ['recover-image']);
      expect(f.sent, isEmpty);
      f.capability.value = 2;
      await t.pumpAndSettle();
      await f.open(t);
      expect(f.tile(t, '图片').enabled, isTrue);
      await t.tap(find.text('图片'));
      await t.pumpAndSettle();
      expect(f.actions, ['recover-image', 'image']);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        '原未发送草稿',
      );
      await f.finish(t);
    },
  );
  for (final action in ['重新读取图片能力', '旅程'])
    testWidgets('recording old material menu $action cannot cross owner ABA', (
      t,
    ) async {
      final f = _MaterialFixture();
      await f.mount(t);
      await f.open(t);
      final oldAction = f.tile(t, action).onTap!;
      f.scope.value = 'owner-B';
      f.scope.value = 'owner-A';
      await t.pumpAndSettle();
      expect(find.text('账号或情境已变化，请重新打开材料菜单'), findsOneWidget);
      oldAction();
      await t.pumpAndSettle();
      expect(f.actions, isEmpty);
      expect(f.sent, isEmpty);
      await f.finish(t);
    });
  testWidgets(
    'recording material action is consumed once without duplicate route pops',
    (t) async {
      final f = _MaterialFixture();
      await f.mount(t);
      await f.open(t);
      final first = f.tile(t, '旅程').onTap!;
      final duplicate = f.tile(t, '主题地图').onTap!;
      first();
      duplicate();
      await t.pumpAndSettle();
      expect(f.actions, ['journey']);
      expect(f.sent, isEmpty);
      expect(find.byType(TextField), findsOneWidget);
      await f.finish(t);
    },
  );
  testWidgets(
    'recording image login recovery does not imply picker or upload',
    (t) async {
      final f = _MaterialFixture();
      await f.mount(t, login: true);
      await f.open(t);
      expect(f.tile(t, '图片').enabled, isFalse);
      expect(find.text('重新读取图片能力'), findsNothing);
      await t.tap(find.text('登录后添加图片'));
      await t.pumpAndSettle();
      expect(f.actions, ['login']);
      expect(f.sent, isEmpty);
      expect(f.scope.value, 'owner-A');
      expect(f.loginNeeded.value, isFalse);
      expect(
        t.widget<AgentComposer>(find.byType(AgentComposer)).draftIdentity,
        'owner-A',
      );
      expect(t.widget<TextField>(find.byType(TextField)).enabled, isTrue);
      expect(f.actions.where((action) => action == 'image'), isEmpty);
      expect(f.workspace.task, isNull);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        '原未发送草稿',
      );
      await f.finish(t);
    },
  );
}
