import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/connection_request_decision_operation.dart';
import 'package:birdtie_client/src/workspace/message_request_policy_controller.dart';
import 'package:birdtie_client/src/workspace/message_request_policy_page.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;

// Original native registered HTTP bytes; this test uses MockClient transport.
// Local seed identities and test fonts do not prove real IdP/device acceptance.
const _evidence =
    '../../docs/testing/evidence/unfinished-completion-2026-10-10/message-policy';
Map<String, dynamic> _entry(String name) =>
    (jsonDecode(File('$_evidence/client-wire-index.json').readAsStringSync())
            as Map<String, dynamic>)['packets'][name]
        as Map<String, dynamic>;
List<int> _bytes(String name) {
  final entry = _entry(name);
  final bytes = File('../../${entry['path']}').readAsBytesSync();
  expect(sha256.convert(bytes).toString(), entry['sha256']);
  return bytes;
}

Object? _data(String name) =>
    (jsonDecode(utf8.decode(_bytes(name))) as Map<String, dynamic>)['data'];
http.Response _response(String name) => http.Response.bytes(
  _bytes(name),
  _entry(name)['status'] as int,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    for (final name in ['NativeWireTestChinese', 'Roboto']) {
      final font = FontLoader(name)
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
      ..addFont(
        Future.value(
          ByteData.sublistView(
            File(
              r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
            ).readAsBytesSync(),
          ),
        ),
      );
    await icons.load();
  });
  test(
    'actual native own records retain precise owner/Agent and manual SCREEN',
    () {
      for (final name in ['policyUnconfigured', 'policyScreen']) {
        final meta = _entry(name);
        final policy = MessageRequestPolicy.read(_data(name), meta['ownerId']);
        expect(policy.ownerID, meta['ownerId']);
        expect(policy.agentID, meta['agentId']);
        expect(policy.version, meta['revision']);
        expect(policy.incoming, name == 'policyScreen' ? 'SCREEN' : 'REQUEST');
        expect(policy.configured, name == 'policyScreen');
        expect(
          policy.status,
          name == 'policyScreen' ? 'ACTIVE' : 'UNCONFIGURED',
        );
      }
    },
  );
  test(
    'actual native saved policy loads through original controller with no PUT',
    () async {
      final meta = _entry('policyScreen');
      final p = MessageRequestPolicy.read(
        _data('policyScreen'),
        meta['ownerId'],
      );
      final requests = <http.Request>[];
      final client = MockClient((request) async {
        requests.add(request);
        return _response('policyScreen');
      });
      final controller = MessageRequestPolicyController(
        accountID: () => meta['ownerId'],
        authorizationHeader: () => 'Bearer local-mock-only',
        apiBaseUrl: 'https://native-wire.test',
        now: () => p.observedAt,
        client: client,
      );
      addTearDown(controller.dispose);
      addTearDown(client.close);
      await controller.load();
      expect(controller.policy!.incoming, 'SCREEN');
      expect(controller.editable, isTrue);
      expect(controller.uncertain, isFalse);
      expect(requests.single.method, 'GET');
      expect(requests.single.url.path, '/v1/me/message-request-policy');
    },
  );
  test(
    'actual native original SCREEN list remains same pending Request without AI result',
    () {
      final meta = _entry('requestScreenList');
      final all = (_data('requestScreenList') as List)
          .map(
            (row) => ContactRequest.fromReviewJson(row as Map<String, dynamic>),
          )
          .toList();
      final request = all.singleWhere((r) => r.id == meta['requestId']);
      expect(request.direction, 'incoming');
      expect(request.scope, meta['scope']);
      expect(request.state, 'pending');
      expect(request.pendingReview, isTrue);
      expect(request.policyDisposition, 'SCREEN');
      expect(request.screeningStatus, 'PENDING_REVIEW');
      expect(request.conversationId, isEmpty);
    },
  );
  test(
    'actual native keyed accept receipt is original historical result only',
    () {
      final meta = _entry('requestAcceptReceipt');
      final receipt = ConnectionRequestOperationReceipt.decode(
        _data('requestAcceptReceipt') as Map<String, dynamic>,
        owner: meta['ownerId'],
        request: meta['requestId'],
        operation: meta['operationId'],
        action: meta['action'],
        scope: meta['scope'],
      );
      expect(receipt.committed, isTrue);
      expect(receipt.state, 'accepted');
      expect(receipt.requestID, _entry('requestScreenList')['requestId']);
      expect(
        receipt.requestDigest,
        connectionDecisionDigest(
          meta['ownerId'],
          meta['requestId'],
          meta['action'],
        ),
      );
    },
  );
  for (final config in [(1.0, Brightness.light), (2.0, Brightness.dark)]) {
    testWidgets(
      'native SCREEN settings readable at narrow320/${config.$1}/${config.$2}',
      (tester) async {
        tester.view.physicalSize = const Size(320, 720);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final meta = _entry('policyScreen');
        final p = MessageRequestPolicy.read(
          _data('policyScreen'),
          meta['ownerId'],
        );
        final auth = SeedTestAuth()..owner = meta['ownerId'];
        final requests = <http.Request>[];
        final client = MockClient((request) async {
          requests.add(request);
          return _response('policyScreen');
        });
        addTearDown(auth.dispose);
        addTearDown(client.close);
        final theme = birdtieTheme(config.$2);
        final boundary = GlobalKey();
        Future<void> capture(String state) async {
          final directory = Platform.environment['BIRDTIE_POLICY_WIDGET_DIR'];
          if (directory == null) return;
          await tester.runAsync(() async {
            final render =
                boundary.currentContext!.findRenderObject()!
                    as RenderRepaintBoundary;
            final image = await render.toImage(pixelRatio: 1);
            final png = await image.toByteData(format: ui.ImageByteFormat.png);
            image.dispose();
            await Directory(directory).create(recursive: true);
            await File(
              '$directory/policy-native-$state-${config.$2.name}-${(config.$1 * 100).round()}.png',
            ).writeAsBytes(png!.buffer.asUint8List());
          });
        }

        await tester.pumpWidget(
          RepaintBoundary(
            key: boundary,
            child: MaterialApp(
              debugShowCheckedModeBanner: false,
              theme: theme.copyWith(
                textTheme: theme.textTheme.apply(
                  fontFamily: 'NativeWireTestChinese',
                ),
              ),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(
                  context,
                ).copyWith(textScaler: TextScaler.linear(config.$1)),
                child: child!,
              ),
              home: MessageRequestPolicyPage(
                auth: auth,
                client: client,
                apiBaseUrl: 'https://native-wire.test',
                now: () => p.observedAt,
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        await tester.scrollUntilVisible(
          find.text('先留待人工审阅'),
          180,
          maxScrolls: 40,
          scrollable: find.byType(Scrollable).first,
        );
        expect(find.text('先留待人工审阅'), findsOneWidget);
        await capture('screen');
        await tester.scrollUntilVisible(
          find.text('重新读取当前设置'),
          180,
          maxScrolls: 40,
          scrollable: find.byType(Scrollable).first,
        );
        await tester.pumpAndSettle();
        await capture('refresh');
        expect(
          tester
              .getSize(find.widgetWithText(OutlinedButton, '重新读取当前设置'))
              .height,
          greaterThanOrEqualTo(48),
        );
        expect(requests.every((r) => r.method == 'GET'), isTrue);
        expect(tester.takeException(), isNull);
      },
    );
  }
}
