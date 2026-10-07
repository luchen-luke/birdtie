import 'dart:convert';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'inbox_panel_test.dart' show signedInAuth;

void main() {
  testWidgets('inbox mismatched source cannot send new identity to old transport', (t) async {
    final authA=await signedInAuth(accountID:'11111111-1111-4111-8111-111111111111',token:'synthetic-a');
    final authB=await signedInAuth(accountID:'22222222-2222-4222-8222-222222222222',token:'synthetic-b');
    final oldRequests=<http.Request>[];
    final client=MockClient((r) async {
      oldRequests.add(r);
      return http.Response(jsonEncode({'data':[]}),200,
          headers:{'content-type':'application/json; charset=utf-8'});
    });
    final source=RemoteInboxSource(authorizationHeader:()=>authA.authorizationHeader,
        client:client,apiBaseUrl:'https://old-synthetic.fixture');
    Widget page(bool second)=>MaterialApp(home:Scaffold(body:InboxPanel(
      key:const ValueKey('same-inbox-source'),auth:second?authB:authA,source:source)));
    await t.pumpWidget(page(false));
    await t.pumpAndSettle();
    expect(oldRequests,isNotEmpty);
    expect(oldRequests.every((r)=>r.headers['Authorization']=='Bearer synthetic-a'),isTrue);
    final before=oldRequests.length;
    await t.pumpWidget(page(true));
    await t.pumpAndSettle();
    expect(oldRequests.length,before,reason:'failed Inbox must not create a social child with the new token and old transport');
    expect(find.text('消息连接与当前账号不符，请返回当前入口重新打开。'),findsOneWidget);
    expect(t.takeException(),isNull);
    await t.pumpWidget(const SizedBox.shrink());
    source.dispose();authA.dispose();authB.dispose();
  });
}
