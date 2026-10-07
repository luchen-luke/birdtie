import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_page.dart';
import 'package:birdtie_client/src/workspace/notification_destination_router.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_memory_candidate_api_test.dart'
    show candidateOwner, candidateWire, previewWire;

const _secureChannel = MethodChannel(
  'plugins.it_nomads.com/flutter_secure_storage',
);

class _Auth extends BirdtieAuthController {
  @override
  String? get authorizationHeader => 'Bearer synthetic-candidate';
  @override
  String? get accountID => candidateOwner;
  @override
  bool get signedIn => true;
  @override
  String? get displayName => '本人合成测试';
}

class _Storage {
  final values = <String, String>{};
  Completer<void>? writeGate;
  bool writeEntered = false;
  Future<Object?> handle(MethodCall call) async {
    final args = (call.arguments as Map).cast<String, dynamic>();
    final key = args['key'] as String?;
    switch (call.method) {
      case 'readAll':
        return Map<String, String>.from(values);
      case 'read':
        return values[key];
      case 'write':
        writeEntered = true;
        await writeGate?.future;
        values[key!] = args['value'] as String;
        return null;
      case 'delete':
        values.remove(key);
        return null;
      case 'containsKey':
        return values.containsKey(key);
      default:
        throw MissingPluginException(call.method);
    }
  }
}

class _Wire extends http.BaseClient {
  final candidate = candidateWire();
  final sent =
      <
        ({String method, String path, String host, String? token, String body})
      >[];
  bool closed = false;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    final path = request.url.path;
    final body = request is http.Request ? request.body : '';
    if (path.startsWith('/v1/me/agent-memory-candidates')) {
      sent.add((
        method: request.method,
        path: path,
        host: request.url.host,
        token: request.headers['Authorization'],
        body: body,
      ));
    }
    final Object value = request.method == 'POST' && path.endsWith('/preview')
        ? previewWire(
            (jsonDecode(body) as Map)['previewId'] as String,
            candidate,
          )
        : request.method == 'POST' && path.endsWith('/accept')
        ? candidateWire(status: 'ACTIVE')
        : path == '/v1/me/agent-memory-candidates'
        ? [candidate]
        : <Object>[];
    return Future.value(
      http.StreamedResponse(
        Stream.value(utf8.encode(jsonEncode({'data': value}))),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
  }

  Iterable<
    ({String method, String path, String host, String? token, String body})
  >
  get accepts =>
      sent.where((r) => r.method == 'POST' && r.path.endsWith('/accept'));
  @override
  void close() {
    closed = true;
    super.close();
  }
}

class _Scenario {
  final auth = _Auth(), city = PublicCityController();
  final storage = _Storage(), wire = _Wire(), otherWire = _Wire();
  late final source = ValueNotifier<http.Client>(wire);
  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: wire,
    apiBaseUrl: 'https://candidate-a.test',
  );
  void dispose() {
    if (storage.writeGate != null && !storage.writeGate!.isCompleted) {
      storage.writeGate!.complete();
    }
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
        apiBaseUrl: 'https://candidate-a.test',
      ),
    ),
  ),
);

Future<void> _tap(WidgetTester t, Finder target) async {
  await t.scrollUntilVisible(
    target,
    160,
    maxScrolls: 50,
    scrollable: find.byType(Scrollable).last,
  );
  await t.ensureVisible(target);
  await t.pumpAndSettle();
  expect(target.hitTestable(), findsOneWidget);
  await t.tap(target);
  await t.pumpAndSettle();
}

Future<void> _preview(WidgetTester t) async {
  await _tap(t, find.widgetWithText(ListTile, '待确认的记忆'));
  expect(find.byType(AgentMemoryCandidatePage), findsOneWidget);
  await _tap(t, find.widgetWithText(OutlinedButton, '检查这项候选'));
  final review = find.text('具体版本预览');
  await t.scrollUntilVisible(
    review,
    -180,
    maxScrolls: 50,
    scrollable: find.byType(Scrollable).last,
  );
  expect(review, findsOneWidget);
  final confirm = find.widgetWithText(FilledButton, '确认保存这项声明');
  await t.ensureVisible(confirm);
  await t.pumpAndSettle();
  expect(confirm.hitTestable(), findsOneWidget);
  expect(t.widget<FilledButton>(confirm).onPressed, isNotNull);
}

void main() {
  testWidgets('设置候选来源：同来源确认可提交，父连接单帧退休后存储完成不得发送旧accept', (t) async {
    // Sequential scenarios share one real FakeAsync zone, preserving production SecureStore._tail.
    for (final replaced in [false, true]) {
      final s = _Scenario();
      t.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        _secureChannel,
        s.storage.handle,
      );
      try {
        await t.pumpWidget(_home(s));
        await t.pumpAndSettle();
        await _preview(t);
        s.storage.writeGate = Completer<void>();
        await t.tap(find.widgetWithText(FilledButton, '确认保存这项声明'));
        await t.idle();
        expect(s.storage.writeEntered, true);
        expect(s.wire.accepts, isEmpty);
        if (replaced) s.source.value = s.otherWire;
        await t
            .pump(); // Exactly one parent frame, not a second disposal frame.
        final page = find.byType(AgentMemoryCandidatePage);
        expect(
          page,
          findsOneWidget,
          reason:
              'If already disposed this candidate is disproved; do not force the window.',
        );
        expect(t.element(page).mounted, true);
        final boundary = t.widget<NotificationDestinationBoundary>(
          find.byType(NotificationDestinationBoundary),
        );
        expect(boundary.current(), !replaced);
        s.storage.writeGate!.complete();
        await t
            .idle(); // Flush journal completion; never draw the next retirement frame.
        debugPrintSynchronously((
          jsonEncode({
            'scenario': replaced ? 'retired-parent' : 'normal-current',
            'pageStillMounted': t.element(page).mounted,
            'parentCurrent': boundary.current(),
            'accepts': s.wire.accepts
                .map(
                  (v) => {
                    'host': v.host,
                    'method': v.method,
                    'path': v.path,
                    'body': v.body,
                  },
                )
                .toList(),
            'pendingReferences': s.storage.values.length,
          })).toString());
        expect(s.wire.accepts.length, replaced ? 0 : 1);
        expect(s.otherWire.accepts, isEmpty);
        if (!replaced) {
          expect(s.wire.accepts.single.token, 'Bearer synthetic-candidate');
          expect(s.wire.accepts.single.host, 'candidate-a.test');
          expect((jsonDecode(s.wire.accepts.single.body) as Map).keys, [
            'previewId',
          ]);
          expect(s.storage.values, isEmpty);
        } else {
          expect(
            s.storage.values.length,
            1,
            reason:
                'Unknown-operation journal is not an approval and must not be removed.',
          );
        }
        await t.pumpAndSettle();
        expect(t.takeException(), isNull);
      } finally {
        if (s.storage.writeGate != null && !s.storage.writeGate!.isCompleted) {
          s.storage.writeGate!.complete();
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
  });
}
