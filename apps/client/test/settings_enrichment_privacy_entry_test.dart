import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

class _Auth extends BirdtieAuthController {
  @override
  String? get authorizationHeader => 'Bearer synthetic-privacy';
  @override
  String? get accountID => '80000000-0000-4000-8000-000000000001';
  @override
  bool get signedIn => true;
  @override
  String? get displayName => '本人合成测试';
}

class _Wire extends http.BaseClient {
  final sent = <http.BaseRequest>[];
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    sent.add(r);
    final raw = r.url.path == '/v1/me/agent-enrichment-purpose/grants'
        ? File(
            '../../work/enrichment-privacy-control-2026-10-07/wires/inventory.json',
          ).readAsBytesSync()
        : utf8.encode('{"data":[]}');
    return Future.value(
      http.StreamedResponse(
        Stream.value(raw),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
  }
}

void main() {
  testWidgets('设置实际本人入口可发现本地分析许可，不自动写或恢复许可', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final auth = _Auth(), wire = _Wire(), city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: wire,
      apiBaseUrl: 'https://privacy.test',
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: wire,
            apiBaseUrl: 'https://privacy.test',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    for (var i = 0; i < 12 && find.text('本地分析许可').evaluate().isEmpty; i++) {
      await t.drag(find.byType(ListView).first, const Offset(0, -360));
      await t.pumpAndSettle();
    }
    expect(
      find.text('本地分析许可'),
      findsOneWidget,
      reason: '真实设置页面没有可发现、跨重开的本地分析许可管理入口',
    );
    await t.ensureVisible(find.text('本地分析许可'));
    await t.pumpAndSettle();
    expect(find.text('本地分析许可').hitTestable(), findsOneWidget);
    await t.tap(find.text('本地分析许可'));
    await t.pumpAndSettle();
    expect(find.text('本地分析许可').hitTestable(), findsOneWidget);
    expect(
      wire.sent
          .where((r) => r.url.path == '/v1/me/agent-enrichment-purpose/grants')
          .length,
      1,
    );
    expect(wire.sent.every((r) => r.method == 'GET'), true);
    await t.pumpWidget(const SizedBox());
    moments.dispose();
    city.dispose();
    auth.dispose();
    wire.close();
  });
}
