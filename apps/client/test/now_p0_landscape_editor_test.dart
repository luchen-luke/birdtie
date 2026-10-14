import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'chat_entity_router_test.dart' show ChatTestAuth;

class _EditorCity extends PublicCityController {
  @override
  PublicCity get selectedCity => const PublicCity(
    id: 'aberdeen-gb',
    name: 'Aberdeen',
    region: '测试',
    contentStatus: 'building',
    source: PublicSource(
      label: 'fixture',
      maintainer: 'test',
      freshness: 'test',
      updatedAt: null,
    ),
    map: null,
  );
}

void main() {
  for (final scene in [
    (landscape: true, scale: 2.0, history: false),
    (landscape: true, scale: 2.0, history: true),
    (landscape: false, scale: 2.0, history: true),
    (landscape: false, scale: 1.3, history: true),
  ]) {
    testWidgets('full scaled line survives IME in $scene', (tester) async {
      // Injected widget geometry, not a real native IME result. The landscape
      // physical aspect and IME top reproduce the actual 2656x1220 screenshot.
      const dpr = 3.25;
      final physical = scene.landscape
          ? const Size(2656, 1220)
          : const Size(1220, 2656);
      final keyboardPhysical = scene.landscape ? 1220.0 - 379 : 1000.0;
      tester.view.physicalSize = physical;
      tester.view.devicePixelRatio = dpr;
      tester.view.viewPadding = const FakeViewPadding(top: 150, bottom: 78);
      tester.view.padding = const FakeViewPadding(top: 150, bottom: 78);
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.view.resetViewInsets);
      addTearDown(tester.view.resetViewPadding);
      addTearDown(tester.view.resetPadding);
      final auth = ChatTestAuth(), city = _EditorCity();
      final client = MockClient((_) async => http.Response('{"data":[]}', 200));
      final moments = PrivateMomentController(
        client: client,
        apiBaseUrl: '',
        authorizationHeader: () => null,
      );
      final connections = ConnectionSource(
        client: client,
        apiBaseUrl: '',
        authorizationHeader: () => null,
      );
      await tester.pumpWidget(
        MaterialApp(
          theme: birdtieTheme(Brightness.light),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: TextScaler.linear(scene.scale)),
            child: child!,
          ),
          home: MapWorkspace(
            city: city,
            auth: auth,
            moments: moments,
            connectionSource: connections,
            seedClient: client,
            seedApiBaseUrl: '',
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull, reason: '进入时的布局');
      final map = find.byType(MapCanvas).evaluate().single;
      final workspace = tester
          .widget<MapCanvas>(find.byType(MapCanvas))
          .workspace;
      final composer = tester.state<AgentComposerState>(
        find.byType(AgentComposer),
      );
      const task = AgentTask(id: 'retained', query: '历史', status: 'COMPLETED');
      if (scene.history) {
        workspace.task = task;
        workspace.result = const AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: '保留的历史回答',
        );
        workspace.setSheetExtent(AgentSheetExtent.peek);
        workspace.selectEntity('place:retained', preserveViewport: true);
      }
      composer.fillDraft('保留中文草稿第一行\n第二行继续编辑\n第三行\n第四行');
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull, reason: '有多行草稿、尚未注入IME时');
      final draft = composer.draftText;
      for (var cycle = 0; cycle < 2; cycle++) {
        tester.view.viewInsets = FakeViewPadding(bottom: keyboardPhysical);
        await tester.pumpAndSettle();
        final field = tester.widget<TextField>(find.byType(TextField));
        final editable = tester
            .state<EditableTextState>(find.byType(EditableText))
            .renderEditable;
        final padding = field.decoration!.contentPadding!.resolve(
          TextDirection.ltr,
        );
        expect(
          editable.size.height,
          greaterThanOrEqualTo(editable.preferredLineHeight - .01),
          reason: '实际编辑视口必须容纳当前字体的一整行，而非只断言没有overflow',
        );
        expect(
          tester.getSize(find.byType(TextField)).height,
          greaterThanOrEqualTo(
            editable.preferredLineHeight + padding.vertical - .01,
          ),
        );
        if (scene.landscape) {
          expect(
            (editable.offset as ScrollPosition).maxScrollExtent,
            greaterThan(0),
            reason: '保留4行原编辑器；受限高度时通过内部滚动读取草稿',
          );
        }
        final send = find.byTooltip('发送需求');
        expect(send.hitTestable(), findsOneWidget);
        expect(tester.getSize(send).height, greaterThanOrEqualTo(48));
        expect(
          tester.getBottomRight(send).dy,
          lessThanOrEqualTo((physical.height - keyboardPhysical) / dpr),
        );
        expect(composer.draftText, draft);
        expect(
          tester.state<AgentComposerState>(find.byType(AgentComposer)),
          same(composer),
        );
        expect(find.byType(MapCanvas).evaluate().single, same(map));
        expect(find.byType(TextField), findsOneWidget);
        if (scene.history) {
          expect(workspace.task, same(task));
          expect(workspace.selectedEntityId, 'place:retained');
        }
        expect(tester.takeException(), isNull);
        tester.view.viewInsets = const FakeViewPadding();
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull, reason: '收起IME后');
      }
      await tester.pumpWidget(const SizedBox());
      moments.dispose();
      connections.dispose();
      city.dispose();
      auth.dispose();
      client.close();
    });
  }
}
