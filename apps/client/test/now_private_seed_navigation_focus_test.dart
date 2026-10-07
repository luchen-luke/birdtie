import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:convert';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/content/agent_seed_sheet.dart';
import 'package:birdtie_client/src/workspace/notification_destination_router.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'now_scope_recovery_test.dart';

class _CountWire extends http.BaseClient {
  _CountWire(this.original);
  final http.Client original;
  final methods = <String>[];
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    methods.add(request.method);
    return original.send(request);
  }
}

void main() {
  testWidgets('本人闲置选择入口返回保留安全草稿且不自动恢复焦点或IME', (t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final f = NowFixture();
    // The original fixture uses generic GET data[], not a real signed-in session.
    f.auth.token = 'Bearer synthetic-return-focus-unit';
    f.auth.owner = '11111111-1111-4111-8111-111111111111';
    final wire = _CountWire(f.client);
    try {
      await t.pumpWidget(MaterialApp(home: MapWorkspace(
        city: f.city, auth: f.auth, moments: f.moments,
        agentTaskSource: f.source, seedClient: wire, seedApiBaseUrl: f.base)));
      await t.pumpAndSettle();
      expect(f.auth.signedIn, true);
      expect(f.workspace(t).task, isNull);
      await nowTap(t, nowField()); // Real touch admission, not FocusNode override.
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, true);
      expect(t.testTextInput.isVisible, true);
      const draft = '尚未发送的周末选择草稿';
      await t.enterText(nowField(), draft);
      await t.pumpAndSettle();
      final entry = find.widgetWithText(TextButton, '完善我的选择');
      expect(entry.hitTestable(), findsOneWidget);
      await t.tap(entry); // The actual visible original button, not its callback.
      await t.pumpAndSettle();
      expect(find.byType(AgentSeedSheet), findsOneWidget);
      expect(find.byType(NotificationDestinationBoundary), findsOneWidget);
      await t.pageBack(); // Standard AppBar back from the real route.
      await t.pumpAndSettle();
      final field = t.widget<TextField>(nowField());
      final returnedFocus = field.focusNode!.hasFocus;
      final returnedIME = t.testTextInput.isVisible;
      debugPrintSynchronously((jsonEncode({'actualSeedRouteReturn': true,
        'draftPreserved': field.controller!.text == draft,
        'focusAfterBack': returnedFocus, 'testTextInputVisibleAfterBack': returnedIME,
        'agentQueries': f.source.queries.length, 'httpMethods': wire.methods,
        'fixture': 'original NowFixture generic GET data[] / synthetic person'})).toString());
      expect(field.controller!.text, draft);
      expect(returnedFocus, false);
      expect(returnedIME, false);
      expect(f.source.queries, isEmpty);
      expect(wire.methods, everyElement('GET'));
      expect(f.workspace(t).task, isNull);
      await nowTap(t, nowField());
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, true);
      expect(t.testTextInput.isVisible, true);
      expect(t.widget<TextField>(nowField()).controller!.text, draft);
      expect(t.takeException(), isNull);
    } finally {
      await f.unmount(t);
      f.dispose();
    }
  });
}
