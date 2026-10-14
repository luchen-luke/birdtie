import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/contact_add_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qr_flutter/qr_flutter.dart';

const owner = '11111111-1111-4111-8111-111111111111';
const code = '22222222-2222-4222-8222-222222222222';

class Auth extends BirdtieAuthController {
  String? who = owner;
  @override
  String? get accountID => who;
  @override
  String? get authorizationHeader => who == null ? null : 'Bearer $who';
  @override
  bool get signedIn => who != null;
  @override
  String? get displayName => '二维码测试';
  void leave() {
    who = null;
    notifyListeners();
  }
}

http.Response view({int version = 1, String? id = code}) => http.Response(
  jsonEncode({
    'data': {
      'version': version,
      'codeId': id,
      'expiresAt': id == null ? null : DateTime.utc(2099).toIso8601String(),
    },
  }),
  200,
);
void main() {
  testWidgets('uncertain write stays uncertain when verification fails', (
    t,
  ) async {
    final auth = Auth();
    var writes = 0, gets = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') {
        writes++;
        throw http.ClientException('synthetic dropped response');
      }
      gets++;
      return gets == 2
          ? http.Response('{}', 503)
          : view(version: gets > 2 ? 2 : 1, id: gets > 2 ? null : code);
    });
    await t.pumpWidget(
      MaterialApp(
        home: MyContactCodePage(
          auth: auth,
          client: client,
          apiBaseUrl: 'http://code.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.byType(QrImageView), findsOneWidget);
    await t.tap(find.text('更新二维码'));
    await t.pumpAndSettle();
    expect(writes, 1);
    expect(find.byType(QrImageView), findsNothing);
    await t.tap(find.text('核实最新状态'));
    await t.pumpAndSettle();
    expect(find.text('核实最新状态'), findsOneWidget);
    expect(find.text('更新二维码'), findsNothing);
    expect(find.byType(QrImageView), findsNothing);
    await t.tap(find.text('核实最新状态'));
    await t.pumpAndSettle();
    expect(find.text('生成二维码'), findsOneWidget);
    expect(writes, 1);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
  testWidgets('a late own-code response cannot revive signed-out QR', (
    t,
  ) async {
    final auth = Auth();
    final pending = Completer<http.Response>();
    final client = MockClient((r) => pending.future);
    await t.pumpWidget(
      MaterialApp(
        home: MyContactCodePage(
          auth: auth,
          client: client,
          apiBaseUrl: 'http://code.test',
        ),
      ),
    );
    await t.pump();
    auth.leave();
    await t.pump();
    pending.complete(view());
    await t.pumpAndSettle();
    expect(find.byType(QrImageView), findsNothing);
    expect(find.text('生成二维码'), findsNothing);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
}
