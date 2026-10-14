import 'dart:io';
import 'dart:ui' as ui;

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'package:birdtie_client/src/workspace/agent_result_sheet.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/external_place_candidates.dart';
import 'package:birdtie_client/src/workspace/now_ui_tokens.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'consumer_agent_public_followup_test.dart' show FollowupFixture, query;

const _message = 'AI 服务目前不可用，本轮没有生成新回答。原对话仍保留。';
const _recovery = '可从最近对话查看之前的内容，服务恢复后再发起查询。';
const _captureKey = ValueKey('ai-service-widget-capture');

Future<void> _loadCaptureFonts(WidgetTester tester) async {
  if (Platform.environment['BIRDTIE_AI_SERVICE_CAPTURE_DIR'] == null) return;
  // Test-only Windows system font, as in the existing typography harnesses.
  // This does not change production fonts or claim phone/build evidence.
  await tester.runAsync(() async {
    await (FontLoader('BirdtieAIServiceQA')..addFont(
          File(
            'C:/Windows/Fonts/msyh.ttc',
          ).readAsBytes().then(ByteData.sublistView),
        ))
        .load();
    await (FontLoader('MaterialIcons')..addFont(
          File(
            'D:/DevTools/flutter/bin/cache/artifacts/material_fonts/materialicons-regular.otf',
          ).readAsBytes().then(ByteData.sublistView),
        ))
        .load();
  });
}

ThemeData _theme(Brightness brightness) {
  final theme = birdtieTheme(brightness);
  if (Platform.environment['BIRDTIE_AI_SERVICE_CAPTURE_DIR'] == null) {
    return theme;
  }
  final withFont = theme.copyWith(
    textTheme: theme.textTheme.apply(fontFamily: 'BirdtieAIServiceQA'),
  );
  return withFont.copyWith(
    extensions: [
      ...theme.extensions.values.where((e) => e is! NowUiTokens),
      NowUiTokens.fromTheme(withFont),
    ],
  );
}

Future<void> _capture(WidgetTester tester, String name) async {
  final directory = Platform.environment['BIRDTIE_AI_SERVICE_CAPTURE_DIR'];
  if (directory == null || directory.isEmpty) return;
  final boundary = tester.renderObject<RenderRepaintBoundary>(
    find.byKey(_captureKey),
  );
  await tester.runAsync(() async {
    final image = await boundary.toImage(pixelRatio: 2);
    try {
      final data = await image.toByteData(format: ui.ImageByteFormat.png);
      await Directory(directory).create(recursive: true);
      await File(
        '$directory/$name.png',
      ).writeAsBytes(data!.buffer.asUint8List());
    } finally {
      image.dispose();
    }
  });
}

void main() {
  test(
    'AI service typed 503 blocks unchanged retry and offers history recovery',
    () {
      const failure = AgentRequestFailure(
        _message,
        statusCode: 503,
        code: 'ai_service_unavailable',
      );
      expect(failure.permitsUnchangedRetry, false);
      expect(failure.recoveryMessage, _recovery);
    },
  );
  for (final status in [401, 403]) {
    test('AI service code cannot replace identity recovery $status', () {
      final failure = AgentRequestFailure(
        'original',
        statusCode: status,
        code: 'ai_service_unavailable',
      );
      expect(failure.permitsUnchangedRetry, false);
      expect(
        failure.recoveryMessage,
        status == 401 ? contains('重新登录') : contains('访问权限'),
      );
      expect(failure.recoveryMessage, isNot(_recovery));
    });
  }
  for (final status in [null, 400, 500]) {
    test('AI service code requires the exact 503 envelope $status', () {
      final failure = AgentRequestFailure(
        'original',
        statusCode: status,
        code: 'ai_service_unavailable',
      );
      expect(failure.permitsUnchangedRetry, true);
      expect(failure.recoveryMessage, isNull);
    });
  }
  test('generic 503 preserves its existing retry behavior', () {
    const failure = AgentRequestFailure(
      'original',
      statusCode: 503,
      code: 'internal_error',
    );
    expect(failure.permitsUnchangedRetry, true);
    expect(failure.recoveryMessage, isNull);
  });

  for (final brightness in Brightness.values) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets(
        'AI service recovery retains cards at 320dp $brightness/$scale',
        (tester) async {
          tester.view.devicePixelRatio = 1;
          tester.view.physicalSize = const Size(320, 800);
          addTearDown(tester.view.resetDevicePixelRatio);
          addTearDown(tester.view.resetPhysicalSize);
          await _loadCaptureFonts(tester);
          final f = FollowupFixture();
          addTearDown(f.dispose);
          await f.send(query);
          final previous = f.workspace.result!;
          final first = previous.onlinePlaceSearch!.places.first;
          f.failure = 'ai_service_unavailable';
          f.failureStatus = 503;
          await f.send('请继续比较 Aberdeen 阿伯丁餐厅的位置，保留上一轮查询内容与地点卡片。');
          f.workspace.setSheetExtent(AgentSheetExtent.expanded);
          await tester.pumpWidget(
            MaterialApp(
              theme: _theme(brightness),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(
                  context,
                ).copyWith(textScaler: TextScaler.linear(scale)),
                child: child!,
              ),
              home: RepaintBoundary(
                key: _captureKey,
                child: Scaffold(
                  body: AgentResultsSheet(
                    workspace: f.workspace,
                    onSuggestion: (_) {},
                    onRetry: () => f.workspace.retry(),
                  ),
                ),
              ),
            ),
          );
          await tester.pumpAndSettle();
          expect(find.byKey(const Key('agent-sheet-retry')), findsNothing);
          // Both real error/recovery text and retained cards are reachable by
          // the existing coordinated scroll surfaces, including 200% text.
          await tester.ensureVisible(find.text(_message));
          await tester.pumpAndSettle();
          expect(find.text(_message), findsOneWidget);
          expect(find.text(_recovery), findsOneWidget);
          final toggle = find.byKey(const Key('agent-sheet-map-toggle'));
          expect(tester.getSize(toggle).height, greaterThanOrEqualTo(48));
          await _capture(
            tester,
            'widget-${brightness.name}-${(scale * 100).round()}-recovery',
          );
          final card = find.byKey(ValueKey('external-place-${first.id}'));
          await tester.ensureVisible(card);
          await tester.pumpAndSettle();
          expect(find.byType(ExternalPlaceCandidateCard), findsWidgets);
          expect(f.workspace.result, same(previous));
          expect(previous.onlinePlaceSearch!.places, hasLength(15));
          expect(f.requests.where((r) => r.method == 'POST'), hasLength(2));
          expect(tester.takeException(), isNull);
          await _capture(
            tester,
            'widget-${brightness.name}-${(scale * 100).round()}-retained-card',
          );
          await tester.pumpWidget(const SizedBox.shrink());
        },
      );
    }
  }
}
