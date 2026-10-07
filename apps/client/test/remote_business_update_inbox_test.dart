import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
// Actual registered HTTP in the exclusively owned local106 DB. No live data,
// authorization header or private source is embedded. See work provenance.
const businessInboxWire = r'''{"data":[{"id":"03723e98-83d4-4a36-a91a-e9e75bc2d42e","category":"updates","title":"关注的商家公开资料已更新","detail":"请查看当前公开资料。资料更新不代表可预订或已开启智能体。","resourceType":"business_update","resourceId":"17c44896-7aff-49ec-b956-5e4177156733","targetBusinessId":"15329dc4-10db-4545-a105-7c521ca00c50","createdAt":"2026-10-07T21:34:32.981162+08:00","semanticCategory":"BUSINESS","notificationRoute":"NORMAL","notificationPriority":50}]}''';
const businessReadWire = r'''{"data":{"id":"03723e98-83d4-4a36-a91a-e9e75bc2d42e","category":"updates","title":"关注的商家公开资料已更新","detail":"请查看当前公开资料。资料更新不代表可预订或已开启智能体。","resourceType":"business_update","resourceId":"17c44896-7aff-49ec-b956-5e4177156733","targetBusinessId":"15329dc4-10db-4545-a105-7c521ca00c50","createdAt":"2026-10-07T21:34:32.981162+08:00","readAt":"2026-10-07T21:34:33.141386+08:00","semanticCategory":"BUSINESS","notificationRoute":"NORMAL","notificationPriority":50}}''';
const businessPublicWire = r'''{"data":{"id":"15329dc4-10db-4545-a105-7c521ca00c50","name":"本地合成公开商家","verificationStatus":"verified","profileStatus":"verified","description":"明确公开批准的资料","officialLinks":["https://example.invalid/public"],"profileVersion":2,"reviewedAt":"2026-10-07T13:34:32.90379Z","validUntil":"2026-10-07T14:34:32.851536Z","upcomingActivities":[]}}''';

Map<String,dynamic> businessNotice()=>Map<String,dynamic>.from((jsonDecode(businessInboxWire)['data'] as List).single);
http.Response businessResponse(Object data)=>http.Response.bytes(utf8.encode(jsonEncode({'data':data})),200);
void main(){
 test('实际原生公开更新通知及同ID读取回执可被原Remote读取',()async{
  final row=businessNotice();
  final client=MockClient((r)async{
   expect(r.headers['Authorization'],'Bearer local-unit');
   if(r.method=='GET'){expect(r.url.path,'/v1/me/inbox');return http.Response.bytes(utf8.encode(businessInboxWire),200);}
   expect(r.method,'POST');expect(r.url.path,'/v1/me/inbox/${row['id']}/read');return http.Response.bytes(utf8.encode(businessReadWire),200);
  });
  final source=RemoteInboxSource(authorizationHeader:()=>'Bearer local-unit',client:client,apiBaseUrl:'https://local.fixture');
  addTearDown(source.dispose);
  final item=(await source.load()).single;
  expect(item.resourceType,'business_update');expect(item.targetBusinessID,row['targetBusinessId']);expect(item.resourceID,isNot(item.targetBusinessID));
  final read=await source.markRead(item.id);expect(read.id,item.id);expect(read.readAt,isNotNull);expect(read.targetBusinessID,item.targetBusinessID);
  // Derived schema control; actual099 native delivery is proved separately.
  final digest=InboxItem.fromJson({...row,'notificationRoute':'DIGEST','notificationPriority':10});expect(digest.notificationRoute,'DIGEST');
 });
 test('公开更新严格区别私有审核目标且拒绝未投递及冲突目标',(){
  final row=businessNotice();
  for(final change in <Map<String,dynamic>>[{'targetBusinessId':null},{'semanticCategory':'ACTIVITY'},{'targetTaskId':row['targetBusinessId']},{'targetConversationId':row['targetBusinessId']},{'notificationRoute':'SILENT'},{'notificationRoute':'BLOCK'}]){
   expect(()=>InboxItem.fromJson({...row,...change}),throwsFormatException);
  }
  final original=InboxItem.fromJson({...row,'resourceType':'business_claim_review'});expect(original.resourceType,'business_claim_review');
 });
 test('新公开通知仍拒绝身份迟到的原Remote读取',()async{
  var token='Bearer A';final waiting=Completer<http.Response>();
  final source=RemoteInboxSource(authorizationHeader:()=>token,client:MockClient((_)=>waiting.future),apiBaseUrl:'https://local.fixture');addTearDown(source.dispose);
  final rejected=expectLater(source.load(),throwsStateError);token='Bearer B';waiting.complete(http.Response.bytes(utf8.encode(businessInboxWire),200));await rejected;
 });
}
