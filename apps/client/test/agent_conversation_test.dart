import 'package:birdtie_client/src/workspace/agent_conversation.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/now_context_query_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'map_layers_api_test.dart' show mapTestBounds;
import 'now_recording_content_hierarchy_test.dart' show recordingMapItem;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'now_context_query_api_test.dart' show onlineWire;

void main() {
  testWidgets(
    'recording conversation map rows share scope once and keep stable entity actions',
    (t) async {
      final rows = [for (var n = 1; n <= 3; n++) recordingMapItem('MOMENT', n)];
      final now = DateTime.now().toUtc();
      final controller = AgentWorkspaceController()
        ..areaOperation = AgentAreaOperation(
          id: 'original-area',
          fingerprint: 'original-source',
          cityID: 'synthetic-city',
          bounds: mapTestBounds,
          kinds: const {'MOMENT'},
          status: AgentAreaStatus.success,
          read: AgentAreaRead(
            publicView: TypedMapView(
              cityID: 'synthetic-city',
              scope: 'PUBLIC',
              observedAt: now,
              validUntil: now.add(const Duration(minutes: 1)),
              truncated: false,
              items: rows,
            ),
          ),
        );
      final opened = <TypedMapItem>[];
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentConversation(
              workspace: controller,
              onSuggestion: (_) {},
              onRetry: () {},
              onOpenMapAreaItem: opened.add,
            ),
          ),
        ),
      );
      expect(find.text('公开记录来自本人明确公开的地点情境，不表示作者当前位置。'), findsOneWidget);
      for (final row in rows)
        expect(find.byKey(ValueKey('area-${row.mapID}')), findsOneWidget);
      expect(find.text('公开记录 · 公开地点'), findsNWidgets(3));
      final tile = find.byKey(ValueKey('area-${rows.last.mapID}'));
      await t.ensureVisible(tile);
      await t.pumpAndSettle();
      await t.tap(tile);
      await t.pumpAndSettle();
      expect(opened.single, same(rows.last));
      final old = t.widget<ListTile>(tile).onTap!;
      controller.retireMapArea();
      old();
      expect(opened, hasLength(1));
      await t.pumpWidget(const SizedBox());
      controller.dispose();
      expect(t.takeException(), isNull);
    },
  );
  for (final brightness in Brightness.values) {
    testWidgets(
      'conversation $brightness uses one user bubble and readable plain assistant',
      (t) async {
        final controller = AgentWorkspaceController()
          ..conversation.addAll(const [
            AgentMessage(role: 'user', text: '找附近的地点'),
            AgentMessage(role: 'assistant', text: '先查看当前公开地点资料'),
          ]);
        final theme = birdtieTheme(brightness);
        await t.pumpWidget(
          MaterialApp(
            theme: theme,
            home: Scaffold(
              body: AgentConversation(
                workspace: controller,
                onSuggestion: (_) {},
                onRetry: () {},
              ),
            ),
          ),
        );
        for (final text in ['找附近的地点', '先查看当前公开地点资料']) {
          final align = t.widget<Align>(
            find
                .ancestor(of: find.text(text), matching: find.byType(Align))
                .first,
          );
          final background = text == '找附近的地点'
              ? ((align.child! as Container).decoration! as BoxDecoration)
                    .color!
              : theme.colorScheme.surface;
          if (text == '先查看当前公开地点资料') {
            expect(align.child, isNot(isA<Container>()));
          }
          final foreground = t.widget<Text>(find.text(text)).style!.color!;
          final a = foreground.computeLuminance(),
              b = background.computeLuminance();
          expect(
            ((a > b ? a : b) + .05) / ((a < b ? a : b) + .05),
            greaterThanOrEqualTo(4.5),
          );
          expect(foreground, NowUiTokens.reviewedTypography(theme).body.color);
        }
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        controller.dispose();
      },
    );
  }
  testWidgets(
    'native seven-type counts use typed truth at narrow large text with IME',
    (tester) async {
      tester.view.physicalSize = const Size(320, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      const kinds = [
        'person',
        'activity',
        'place',
        'community',
        'organization',
        'business',
        'opportunity',
      ];
      final items = [
        for (final kind in kinds)
          AgentResultItem(
            entity: AgentResultRef(
              type: kind,
              id: kind == 'opportunity' ? 'intent:activity' : kind,
            ),
            title: '原生中文标题',
            summary: '',
            scope: kind == 'opportunity' ? 'SELF_PRIVATE' : 'AUTHORIZED_VIEW',
          ),
      ];
      final controller = AgentWorkspaceController()
        ..task = const AgentTask(id: 'native', query: '查找', status: 'COMPLETED')
        ..result = AgentResult(
          entities: const [],
          activities: const [],
          places: const [],
          note: '',
          resultSet: AgentResultSet(
            id: 'native',
            status: 'ready',
            entities: const [],
            items: items,
            generatedAt: DateTime.utc(2026),
          ),
        );
      await tester.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(
              size: Size(320, 800),
              viewInsets: EdgeInsets.only(bottom: 260),
              textScaler: TextScaler.linear(3),
            ),
            child: Scaffold(
              body: AgentConversation(
                workspace: controller,
                onSuggestion: (_) {},
                onRetry: () {},
              ),
            ),
          ),
        ),
      );
      expect(
        find.text('1 个成员 · 1 个活动 · 1 个地点 · 1 个社区 · 1 个组织 · 1 个商家 · 1 个社交机会'),
        findsOneWidget,
      );
      expect(find.textContaining('0 个活动'), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      controller.dispose();
    },
  );
  testWidgets(
    'ONLINE conversation names actual public intents without fake geographic results',
    (tester) async {
      final data = NowOnlineResponse.decode(onlineWire());
      final controller = AgentWorkspaceController()
        ..task = data.task
        ..result = data.result();
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentConversation(
              workspace: controller,
              onSuggestion: (_) {},
              onRetry: () {},
            ),
          ),
        ),
      );
      expect(find.text('1 个公开线上意图 · 无地图点位'), findsOneWidget);
      expect(find.textContaining('0 个活动'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      controller.dispose();
    },
  );
  testWidgets(
    'CITY empty conversation does not append a second zero-count terminal',
    (tester) async {
      final controller = AgentWorkspaceController()
        ..task = const AgentTask(id: 'city', query: '附近', status: 'COMPLETED')
        ..result = const AgentResult(
          entities: [],
          activities: [],
          places: [],
          note: '尚无结果',
        );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentConversation(
              workspace: controller,
              onSuggestion: (_) {},
              onRetry: () {},
            ),
          ),
        ),
      );
      expect(find.text('0 个活动 · 0 个组织 · 0 个地点'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      controller.dispose();
    },
  );
}
