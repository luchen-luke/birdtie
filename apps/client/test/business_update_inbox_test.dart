
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:birdtie_client/src/workspace/public_business_page.dart';
import 'package:birdtie_client/src/workspace/business_claim_notification_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'inbox_panel_test.dart' show signedInAuth;
import 'remote_business_update_inbox_test.dart' show businessInboxWire,businessReadWire,businessPublicWire,businessNotice,businessResponse;
void main(){
 testWidgets('真实公开更新从原Inbox当前Read打开同一商家公开页不进经营工作台',(t)async{
  await t.binding.setSurfaceSize(const Size(400,1100));addTearDown(()=>t.binding.setSurfaceSize(null));
  final auth=await signedInAuth();final row=businessNotice();final calls=<String>[];
  final source=RemoteInboxSource(authorizationHeader:()=>auth.authorizationHeader,apiBaseUrl:'https://local.fixture',client:MockClient((r)async{
   calls.add('${r.method} ${r.url.path}');
   if(r.url.path=='/v1/me/inbox'){return http.Response.bytes(utf8.encode(businessInboxWire),200);}
   if(r.url.path.endsWith('/read')){expect(r.url.path,'/v1/me/inbox/${row['id']}/read');return http.Response.bytes(utf8.encode(businessReadWire),200);}
   expect(r.method,'GET');
   if(r.url.path=='/v1/businesses/${row['targetBusinessId']}'){return http.Response.bytes(utf8.encode(businessPublicWire),200);}
   return businessResponse([]);
  }));
  await t.pumpWidget(MaterialApp(home:Scaffold(body:InboxPanel(auth:auth,source:source))));await t.pumpAndSettle();
  final title=find.text(row['title'] as String);expect(title,findsOneWidget);expect(calls.where((v)=>v.startsWith('POST')),isEmpty);
  await t.ensureVisible(title);await t.pumpAndSettle();await t.tap(title.hitTestable());await t.pumpAndSettle();
  expect(find.byType(PublicBusinessPage),findsOneWidget);expect(find.byType(BusinessClaimNotificationPage),findsNothing);
  expect(find.text('本地合成公开商家'),findsOneWidget);expect(find.text('明确公开批准的资料'),findsOneWidget);
  expect(calls.where((v)=>v.startsWith('POST')).toList(),['POST /v1/me/inbox/${row['id']}/read']);expect(calls.any((v)=>v.contains('/business-console')),isFalse);expect(t.takeException(),isNull);
  await t.pumpWidget(const SizedBox());source.dispose();auth.dispose();
 });
 testWidgets('公开来源Read拒绝保留通知与恢复反馈320大字号入口可点',(t)async{
  await t.binding.setSurfaceSize(const Size(320,1100));addTearDown(()=>t.binding.setSurfaceSize(null));
  final auth=await signedInAuth();final row=businessNotice();var reads=0,publicReads=0;
  final source=RemoteInboxSource(authorizationHeader:()=>auth.authorizationHeader,apiBaseUrl:'https://local.fixture',client:MockClient((r)async{
   if(r.url.path=='/v1/me/inbox'){return http.Response.bytes(utf8.encode(businessInboxWire),200);}
   if(r.url.path.endsWith('/read')){reads++;return http.Response('{}',404);}
   if(r.url.path.startsWith('/v1/businesses/')){publicReads++;}
   expect(r.method,'GET');return businessResponse([]);
  }));
  await t.pumpWidget(MaterialApp(builder:(c,child)=>MediaQuery(data:MediaQuery.of(c).copyWith(textScaler:const TextScaler.linear(2)),child:child!),home:Scaffold(body:InboxPanel(auth:auth,source:source))));await t.pumpAndSettle();
  final title=find.text(row['title'] as String);await t.ensureVisible(title);await t.pumpAndSettle();
  final tile=find.ancestor(of:title,matching:find.byType(ListTile));expect(t.getSize(tile).height,greaterThanOrEqualTo(48));expect(title.hitTestable(),findsOneWidget);
  await t.tap(title.hitTestable());await t.pumpAndSettle();expect(reads,1);expect(publicReads,0);expect(find.byType(PublicBusinessPage),findsNothing);
  expect(find.text('当前通知暂不可读取，请刷新后重试。'),findsOneWidget);expect(title,findsOneWidget);expect(t.takeException(),isNull);
  await t.pumpWidget(const SizedBox());source.dispose();auth.dispose();
 });
 testWidgets('公开详情在组织ABA后的旧响应不回到原通知来源',(t)async{
  await t.binding.setSurfaceSize(const Size(400,1100));addTearDown(()=>t.binding.setSurfaceSize(null));
  final auth=await signedInAuth(),workspace=ValueNotifier<String?>(null);final row=businessNotice();final pending=Completer<http.Response>();var publicReads=0;
  final source=RemoteInboxSource(authorizationHeader:()=>auth.authorizationHeader,apiBaseUrl:'https://local.fixture',client:MockClient((r)async{
   if(r.url.path=='/v1/me/inbox'){return http.Response.bytes(utf8.encode(businessInboxWire),200);}
   if(r.url.path.endsWith('/read')){return http.Response.bytes(utf8.encode(businessReadWire),200);}
   if(r.url.path=='/v1/businesses/${row['targetBusinessId']}'){publicReads++;return pending.future;}
   expect(r.method,'GET');return businessResponse([]);
  }));
  await t.pumpWidget(MaterialApp(home:Scaffold(body:InboxPanel(auth:auth,source:source,workspaceChanges:workspace,organizationWorkspaceID:()=>workspace.value))));await t.pumpAndSettle();
  final title=find.text(row['title'] as String);await t.ensureVisible(title);await t.pumpAndSettle();await t.tap(title.hitTestable());await t.pump();await t.pump(const Duration(milliseconds:350));expect(publicReads,1);
  workspace.value='organization';workspace.value=null;await t.pump();pending.complete(http.Response.bytes(utf8.encode(businessPublicWire),200));await t.pumpAndSettle();
  expect(find.text('明确公开批准的资料'),findsNothing);expect(find.byType(PublicBusinessPage),findsNothing);expect(t.takeException(),isNull);
  await t.pumpWidget(const SizedBox());source.dispose();auth.dispose();workspace.dispose();
 });
}
