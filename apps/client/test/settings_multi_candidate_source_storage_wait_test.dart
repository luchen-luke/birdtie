import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_page.dart';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_api.dart';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_section.dart';
import 'package:birdtie_client/src/workspace/notification_destination_router.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_memory_candidate_api_test.dart'
    show candidateOwner, candidateMoment, candidateSaved, candidateResponse;
import 'agent_multi_candidate_api_test.dart'
    show MultiWireFixture, multiTask, multiPreviewID, multiGrantID;

const _secureChannel = MethodChannel(
  'plugins.it_nomads.com/flutter_secure_storage',
);

class _Auth extends BirdtieAuthController {
  @override
  String? get authorizationHeader => 'Bearer synthetic-multi-candidate';
  @override
  String? get accountID => candidateOwner;
  @override
  bool get signedIn => true;
  @override
  String? get displayName => '本人合成单元身份';
}

class _Storage {
  _Storage(this.phase);
  final String phase;
  final values = <String, String>{};
  Completer<void>? gate;
  int targetWrites = 0, targetDeletes = 0;
  Map<String, dynamic>? captured;
  bool _target(Map<String, dynamic> value) =>
      value['phase'] == phase &&
      value['multi'] == true &&
      value['previewId'] == multiPreviewID &&
      (phase == 'approval' || value['grantId'] == multiGrantID);
  Future<Object?> handle(MethodCall call) async {
    final args = (call.arguments as Map).cast<String, dynamic>();
    final key = args['key'] as String?;
    switch (call.method) {
      case 'readAll':
        return Map<String, String>.from(values);
      case 'read':
        return values[key];
      case 'containsKey':
        return values.containsKey(key);
      case 'write':
        final encoded = args['value'] as String;
        final value = (jsonDecode(encoded) as Map).cast<String, dynamic>();
        if (_target(value)) {
          targetWrites++;
          captured = value;
          await gate?.future;
        }
        values[key!] = encoded;
        return null;
      case 'delete':
        final encoded = values[key];
        if (encoded != null &&
            _target((jsonDecode(encoded) as Map).cast<String, dynamic>())) {
          targetDeletes++;
        }
        values.remove(key);
        return null;
      default:
        throw MissingPluginException(call.method);
    }
  }
}

class _Wire extends http.BaseClient {
  final fixture = MultiWireFixture();
  final sent =
      <
        ({String method, String path, String host, String? token, String body})
      >[];
  bool closed = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    // This trace is the synchronous send boundary, not MockClient's later handler.
    final typed = request as http.Request;
    sent.add((
      method: typed.method,
      path: typed.url.path,
      host: typed.url.host,
      token: typed.headers['Authorization'],
      body: typed.body,
    ));
    final path = typed.url.path;
    final Future<http.Response> response;
    if (path == '/v1/me/agent-memory-candidates' ||
        path == '/v1/me/blocks' ||
        path == '/v1/me/consents') {
      response = Future.value(candidateResponse(<Object>[]));
    } else {
      response = fixture.handle(typed);
    }
    return response.then(
      (value) => http.StreamedResponse(
        Stream.value(value.bodyBytes),
        value.statusCode,
        headers: value.headers,
      ),
    );
  }

  Iterable<
    ({String method, String path, String host, String? token, String body})
  >
  target(String phase) => sent.where(
    (r) =>
        r.method == 'POST' &&
        r.path ==
            (phase == 'approval'
                ? '$multiCandidatePath/previews/$multiPreviewID/approve'
                : '$multiCandidatePath/stage'),
  );
  Iterable<
    ({String method, String path, String host, String? token, String body})
  >
  get accepts => sent.where(
    (r) =>
        r.method == 'POST' &&
        r.path.startsWith('/v1/me/agent-memory-candidates/') &&
        r.path.endsWith('/accept'),
  );
  @override
  void close() {
    closed = true;
    super.close();
  }
}

class _Scenario {
  _Scenario(String phase) : storage = _Storage(phase);
  final auth = _Auth(), city = PublicCityController();
  final _Storage storage;
  final wire = _Wire(), otherWire = _Wire();
  late final source = ValueNotifier<http.Client>(wire);
  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: wire,
    apiBaseUrl: 'https://multi-candidate.test',
  );
  void dispose() {
    moments.dispose();
    city.dispose();
    auth.dispose();
    source.dispose();
    wire.close();
    otherWire.close();
  }
}

Widget _home(_Scenario s) => MaterialApp(
  home: ValueListenableBuilder<http.Client>(
    valueListenable: s.source,
    builder: (_, client, _) => Scaffold(
      body: SettingsPage(
        key: const ValueKey('same-settings'),
        auth: s.auth,
        city: s.city,
        moments: s.moments,
        client: client,
        apiBaseUrl: 'https://multi-candidate.test',
      ),
    ),
  ),
);

Future<void> _readyToTap(WidgetTester t, Finder target) async {
  await t.scrollUntilVisible(
    target,
    180,
    maxScrolls: 50,
    scrollable: find.byType(Scrollable).last,
  );
  await t.ensureVisible(target);
  await t.pumpAndSettle();
  expect(target.hitTestable(), findsOneWidget);
}

Future<void> _tap(WidgetTester t, Finder target) async {
  await _readyToTap(t, target);
  await t.tap(target);
  await t.pumpAndSettle();
}

Future<void> _prepare(WidgetTester t, _Scenario s, String phase) async {
  await _tap(t, find.widgetWithText(ListTile, '待确认的记忆'));
  expect(find.byType(AgentMemoryCandidatePage), findsOneWidget);
  await _tap(t, find.text('选择来源和已有任务'));
  await _tap(t, find.byKey(const ValueKey('multi-task-$multiTask')));
  for (final id in [candidateMoment, candidateSaved]) {
    await _tap(t, find.byKey(ValueKey('multi-source-$id')));
    await _tap(t, find.byKey(ValueKey('multi-review-$id')));
    expect(find.text('本次来源分析预览'), findsOneWidget);
    await _tap(t, find.text('允许这次本地分析'));
  }
  expect(
    s.wire.sent
        .where(
          (r) =>
              r.method == 'POST' &&
              r.path.startsWith(analysisPurposePath) &&
              r.path.endsWith('/approve'),
        )
        .length,
    2,
  );
  await _tap(t, find.text('检查组合候选'));
  expect(find.text('组合候选预览'), findsOneWidget);
  if (phase == 'stage') {
    await _tap(t, find.text('批准这项候选的保留'));
    expect(s.wire.target('approval').length, 1);
  }
  expect(s.storage.values, isEmpty);
}

Future<void> _exercise(WidgetTester t, String phase) async {
  // Both controls use the real Settings route and default production store.
  // Their scenarios stay in one FakeAsync zone; phases are separate selectors.
  for (final retired in [false, true]) {
    final s = _Scenario(phase);
    t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      _secureChannel,
      s.storage.handle,
    );
    try {
      await t.pumpWidget(_home(s));
      await t.pumpAndSettle();
      await _prepare(t, s, phase);
      final action = find.widgetWithText(
        FilledButton,
        phase == 'approval' ? '批准这项候选的保留' : '提交为待确认候选',
      );
      await _readyToTap(t, action);
      expect(t.widget<FilledButton>(action).onPressed, isNotNull);
      s.storage.gate = Completer<void>();
      await t.tap(action);
      await t.idle();
      expect(s.storage.targetWrites, 1);
      expect(s.wire.target(phase), isEmpty);
      if (retired) s.source.value = s.otherWire;
      await t.pump(); // Only the parent replacement frame, not disposal frame.
      final page = find.byType(AgentMemoryCandidatePage);
      final section = find.byType(AgentMultiCandidateSection);
      expect(page, findsOneWidget);
      expect(section, findsOneWidget);
      final pageElement = t.element(page), sectionElement = t.element(section);
      expect(pageElement.mounted, true);
      expect(sectionElement.mounted, true);
      final boundary = t.widget<NotificationDestinationBoundary>(
        find.byType(NotificationDestinationBoundary),
      );
      expect(boundary.current(), !retired);
      s.storage.gate!.complete();
      await t.idle(); // Observe guard before any child disposal frame.
      debugPrintSynchronously((
        jsonEncode({
          'phase': phase,
          'scenario': retired ? 'retired-parent' : 'normal-current',
          'pageStillMounted': pageElement.mounted,
          'sectionStillMounted': sectionElement.mounted,
          'boundaryCurrent': boundary.current(),
          'targetWrites': s.storage.targetWrites,
          'targetHTTP': s.wire
              .target(phase)
              .map(
                (r) => {
                  'method': r.method,
                  'host': r.host,
                  'path': r.path,
                  'body': r.body,
                },
              )
              .toList(),
          'newSourceTargetHTTP': s.otherWire.target(phase).length,
          'pendingReferences': s.storage.values.length,
          'memoryAccepts': s.wire.accepts.length + s.otherWire.accepts.length,
        })).toString());
      expect(pageElement.mounted, true);
      expect(sectionElement.mounted, true);
      expect(s.wire.target(phase).length, retired ? 0 : 1);
      expect(s.otherWire.target(phase), isEmpty);
      expect(s.wire.accepts, isEmpty);
      expect(s.otherWire.accepts, isEmpty);
      expect(s.wire.closed, false);
      expect(s.otherWire.closed, false);
      if (retired) {
        expect(s.storage.values.length, 1);
        expect(s.storage.targetDeletes, 0);
        final journal = (jsonDecode(s.storage.values.values.single) as Map)
            .cast<String, dynamic>();
        expect(journal['phase'], phase);
        expect(journal['multi'], true);
        expect(journal['previewId'], multiPreviewID);
        expect(journal['ownerId'], candidateOwner);
        expect((journal['sourceVersions'] as List).length, 2);
        if (phase == 'stage') expect(journal['grantId'], multiGrantID);
      } else {
        expect(s.storage.values, isEmpty);
        expect(s.storage.targetDeletes, 1);
        expect(s.wire.target(phase).single.host, 'multi-candidate.test');
        expect(
          s.wire.target(phase).single.token,
          'Bearer synthetic-multi-candidate',
        );
      }
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    } finally {
      if (s.storage.gate != null && !s.storage.gate!.isCompleted) {
        s.storage.gate!.complete();
      }
      await t.idle();
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      s.dispose();
      t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        _secureChannel,
        null,
      );
    }
  }
}

void main() {
  testWidgets('设置多候选批准与提交：默认存储等待中父来源退休保留原核实引用', (t) async {
    // Both original phases use the production store's static serial tail.
    // Keep their four real Settings scenarios in one FakeAsync zone; the
    // previous file selected the phases separately and never ran them together.
    for (final phase in ['approval', 'stage']) {
      debugPrintSynchronously(('ORIGINAL_STORAGE_WAIT_PHASE: $phase').toString());
      await _exercise(t, phase);
    }
  });
}
