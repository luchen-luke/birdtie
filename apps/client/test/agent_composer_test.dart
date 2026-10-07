import 'dart:async';

import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

class _PendingSource extends AgentTaskSource {
  final requests = <String, Completer<AgentResult>>{};

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => (requests[query] = Completer<AgentResult>()).future;
}

void main() {
  testWidgets(
    'Now has no attachment control without a current-session protocol',
    (tester) async {
      var clipboardReads = 0;
      var privateImageCalls = 0;
      final sent = <String>[];
      final workspace = AgentWorkspaceController();
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async {
          if (call.method == 'Clipboard.getData') {
            clipboardReads++;
            return <String, String>{'text': 'private clipboard'};
          }
          return null;
        },
      );
      addTearDown(() {
        tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onSearchArea: () {},
              hasSearchArea: false,
              onPrivateMomentImage: () => privateImageCalls++,
              privateMomentImageContext: () => 'private-moment',
            ),
          ),
        ),
      );
      await tester.enterText(find.byType(TextField), '当前会话草稿');
      await tester.pump();
      expect(find.byTooltip('打开快捷操作'), findsNothing);
      expect(find.byIcon(Icons.add_rounded), findsNothing);
      expect(find.text('添加素材'), findsNothing);
      expect(find.text('为私人记录添加图片'), findsNothing);
      expect(clipboardReads, 0);
      expect(privateImageCalls, 0);
      expect(sent, isEmpty);
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        '当前会话草稿',
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      workspace.dispose();
    },
  );

  for (final brightness in Brightness.values) {
    testWidgets(
      'composer $brightness narrow large text preserves draft and cancel',
      (tester) async {
        tester.view.physicalSize = const Size(320, 720);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final workspace = AgentWorkspaceController();
        final sent = <String>[];
        await tester.pumpWidget(
          MaterialApp(
            theme: birdtieTheme(brightness),
            home: MediaQuery(
              data: const MediaQueryData(
                textScaler: TextScaler.linear(3),
                viewInsets: EdgeInsets.only(bottom: 260),
              ),
              child: Scaffold(
                body: Align(
                  alignment: Alignment.bottomCenter,
                  child: AgentComposer(
                    workspace: workspace,
                    onSubmit: sent.add,
                    onSearchArea: () {},
                    hasSearchArea: false,
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.enterText(find.byType(TextField), '周末想找附近可用的地点 Aberdeen');
        await tester.pump();
        expect(tester.takeException(), isNull);
        final send = find.byTooltip('发送需求');
        expect(tester.getSize(send).height, greaterThanOrEqualTo(48));
        final gesture = await tester.startGesture(tester.getCenter(send));
        await gesture.moveBy(const Offset(-180, -110));
        await gesture.up();
        await tester.pump();
        expect(sent, isEmpty);
        expect(
          tester.widget<TextField>(find.byType(TextField)).controller!.text,
          '周末想找附近可用的地点 Aberdeen',
        );
        await tester.tap(send);
        await tester.pump();
        expect(sent, ['周末想找附近可用的地点 Aberdeen']);
        await tester.tap(find.byTooltip('请输入需求'));
        expect(sent, hasLength(1));
        await tester.pumpWidget(const SizedBox.shrink());
        workspace.dispose();
      },
    );
  }
  testWidgets('IME composition is retained until the text is committed', (
    tester,
  ) async {
    final workspace = AgentWorkspaceController();
    final sent = <String>[];
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: sent.add,
            onSearchArea: () {},
            hasSearchArea: false,
          ),
        ),
      ),
    );
    await tester.showKeyboard(find.byType(TextField));
    tester.testTextInput.updateEditingValue(
      const TextEditingValue(
        text: '地点',
        selection: TextSelection.collapsed(offset: 2),
        composing: TextRange(start: 0, end: 2),
      ),
    );
    await tester.pump();
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(sent, isEmpty);
    expect(
      tester.widget<TextField>(find.byType(TextField)).controller!.text,
      '地点',
    );
    tester.testTextInput.updateEditingValue(
      const TextEditingValue(
        text: '地点',
        selection: TextSelection.collapsed(offset: 2),
      ),
    );
    await tester.pump();
    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.pump();
    expect(sent, ['地点']);
    await tester.pumpWidget(const SizedBox.shrink());
    workspace.dispose();
  });
  testWidgets('composer can send another query while a request is pending', (
    tester,
  ) async {
    final source = _PendingSource();
    final workspace = AgentWorkspaceController(source: source);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: (query) => unawaited(workspace.submit(query, [], [])),
            onSearchArea: () {},
            hasSearchArea: false,
          ),
        ),
      ),
    );
    await tester.enterText(find.byType(TextField), 'A');
    await tester.pump();
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(workspace.state, AgentViewState.searching);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    await tester.enterText(find.byType(TextField), 'B');
    await tester.pump();
    expect(find.byTooltip('发送需求'), findsOneWidget);
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(source.requests.keys, ['A', 'B']);
    source.requests['B']!.complete(
      const AgentResult(entities: [], activities: [], places: [], note: 'B'),
    );
    source.requests['A']!.complete(
      const AgentResult(entities: [], activities: [], places: [], note: 'A'),
    );
    await tester.pump();
    expect(workspace.result?.note, 'B');
    await tester.pumpWidget(const SizedBox.shrink());
    workspace.dispose();
  });
}
