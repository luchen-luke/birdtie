import 'dart:convert';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_page.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'connection_request_review_entry_test.dart';
import 'connection_request_decision_operation_test.dart' show operationResponse;

void main(){
 testWidgets('真实申请页确认记录服务操作key而非只本机引用',(t)async{
  Map<String,dynamic>? posted;var posts=0,receiptReads=0;
  final client=MockClient((r)async{
   if(r.method=='POST'&&r.url.path.endsWith('/decision')){posts++;posted=jsonDecode(r.body) as Map<String,dynamic>;return reviewJSON({},503);}
   if(r.url.path.contains('/decision-operations/')){receiptReads++;return http.Response.bytes(utf8.encode(jsonEncode(operationResponse(posted!['operationId']))),200,headers:{'content-type':'application/json; charset=utf-8'});}
   if(r.url.path=='/v1/me/connection-requests')return reviewJSON([reviewRequest()]);
   return reviewAuthResponse(r);
  });
  final auth=await reviewAuth(client),source=ConnectionSource(authorizationHeader:()=>auth.authorizationHeader,client:client,apiBaseUrl:'https://original.fixture');
  final store=MemoryConnectionReviewPendingStore();
  await t.pumpWidget(MaterialApp(home:ConnectionRequestReviewPage(auth:auth,source:source,requestID:reviewID,pendingStore:store,now:()=>DateTime.utc(2026,10,7))));
  await t.pumpAndSettle();await t.tap(find.text('审阅接受'));await t.pumpAndSettle();await t.tap(find.text('确认接受'));await t.pumpAndSettle();
  expect(posted?['operationId'],isA<String>(),reason:'真实原页面POST只有action，无法查询本次决定因果结果');
  expect(posted?['action'],'accept');expect(store.values.length,1);
  expect(find.textContaining('不会重复提交'),findsWidgets);
  await t.pumpWidget(const SizedBox());await t.pumpAndSettle();
  await t.pumpWidget(MaterialApp(home:ConnectionRequestReviewPage(auth:auth,source:source,requestID:reviewID,pendingStore:store,now:()=>DateTime.utc(2026,10,7))));
  await t.pumpAndSettle();expect(posts,1);expect(receiptReads,1);expect(store.values,isEmpty);
  expect(find.textContaining('原操作历史'),findsOneWidget);
  expect(t.widget<FilledButton>(find.widgetWithText(FilledButton,'审阅接受')).onPressed,isNull);
  await t.pumpWidget(const SizedBox());source.dispose();auth.dispose();client.close();
 });
}
