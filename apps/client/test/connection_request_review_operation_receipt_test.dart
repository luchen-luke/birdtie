import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_controller.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'connection_request_review_entry_test.dart' show reviewOwner,reviewID,reviewRequest,reviewJSON;
import 'connection_request_decision_operation_test.dart' show operationResponse;
import 'connection_request_review_operation_pending_store_test.dart' show recoveryReference;
ConnectionRequestReviewController reviewController(ConnectionSource s,MemoryConnectionReviewPendingStore p,{bool Function()? current})=>ConnectionRequestReviewController(
  requestID:reviewID,source:s,accountID:()=>reviewOwner,authorizationHeader:()=> 'Bearer synthetic',
  current:current,pendingStore:p,operationReceipts:true);
http.Response operationJSON(String op,{String wire='commit'})=>http.Response.bytes(utf8.encode(jsonEncode(operationResponse(op,wire:wire))),200,headers:{'content-type':'application/json; charset=utf-8'});
void main(){
  test('未知POST关闭重开只GET原回执COMMITTED不会重复写',()async{
    var posts=0,reads=0;String? operation;
    final c=MockClient((r)async{
      if(r.method=='POST'){posts++;operation=jsonDecode(r.body)['operationId'];return reviewJSON({},503);}
      if(r.url.path.contains('/decision-operations/')){reads++;return operationJSON(operation!);}
      return reviewJSON([reviewRequest()]);
    }),p=MemoryConnectionReviewPendingStore();
    final source=ConnectionSource(client:c,apiBaseUrl:'https://original.fixture',authorizationHeader:()=> 'Bearer synthetic');
    final first=reviewController(source,p);await first.load();await first.submit(first.preview('accept')!);
    expect(first.uncertain,isTrue);expect(posts,1);expect(p.values.length,1);first.dispose();
    final reopened=reviewController(source,p);await reopened.load();
    expect(reads,1);expect(posts,1);expect(reopened.operationReceipt!.committed,isTrue);
    expect(p.values,isEmpty);expect(reopened.canAct('accept'),isFalse);
    expect(reopened.message,contains('历史'));reopened.dispose();source.dispose();c.close();
  });
  test('NO_EFFECT原回执才清服务key缺失409坏DTO仍未知禁止降级',()async{
    for(final mode in ['no_effect','missing','binding_changed','bad_dto']){
      final p=MemoryConnectionReviewPendingStore(),ref=recoveryReference();await p.write('https://original.fixture',ref);var posts=0;
      final c=MockClient((r)async{
        if(r.method=='POST'){posts++;return reviewJSON({},500);}
        if(r.url.path.contains('/decision-operations/')){
          if(mode=='no_effect')return operationJSON(ref.operationID!,wire:'no_effect');
          if(mode=='bad_dto')return reviewJSON({'state':'accepted'});
          return http.Response(jsonEncode({'error':{'code':mode=='missing'?'not_found':'connection_operation_changed'}}),mode=='missing'?404:409);
        }return reviewJSON([reviewRequest()]);
      });
      final source=ConnectionSource(client:c,apiBaseUrl:'https://original.fixture',authorizationHeader:()=> 'Bearer synthetic'),data=reviewController(ConnectionSource(client:c,apiBaseUrl:'https://original.fixture',authorizationHeader:()=> 'Bearer synthetic'),p);
      await data.load();expect(posts,0);
      if(mode=='no_effect'){expect(data.uncertain,isFalse);expect(p.values,isEmpty);expect(data.canAct('accept'),isTrue);expect(data.message,contains('未生效'));}
      else{expect(data.uncertain,isTrue);expect(p.values.length,1);expect(data.preview('accept'),isNull);}
      data.dispose();source.dispose();c.close();
    }
  });
  test('旧v1未知引用不查服务key迟到回执来源退役不能清新记录',()async{
    final p=MemoryConnectionReviewPendingStore();await p.write('https://original.fixture',recoveryReference(keyed:false));var reads=0,posts=0;
    final c=MockClient((r)async{if(r.method=='POST')posts++;if(r.url.path.contains('/decision-operations/'))reads++;return reviewJSON([reviewRequest()]);});
    final s=ConnectionSource(client:c,apiBaseUrl:'https://original.fixture',authorizationHeader:()=> 'Bearer synthetic'),d=reviewController(ConnectionSource(client:c,apiBaseUrl:'https://original.fixture',authorizationHeader:()=> 'Bearer synthetic'),p);
    await d.load();expect(d.uncertain,isTrue);expect(reads,0);expect(posts,0);d.dispose();s.dispose();c.close();
    final newer=MemoryConnectionReviewPendingStore(),ref=recoveryReference();await newer.write('https://original.fixture',ref);
    final gate=Completer<http.Response>();var active=true;
    final late=MockClient((r)async=>r.url.path.contains('/decision-operations/')?gate.future:reviewJSON([reviewRequest()]));
    final ls=ConnectionSource(client:late,apiBaseUrl:'https://original.fixture',authorizationHeader:()=> 'Bearer synthetic'),ld=reviewController(ConnectionSource(client:late,apiBaseUrl:'https://original.fixture',authorizationHeader:()=> 'Bearer synthetic'),newer,current:()=>active);
    final f=ld.load();await Future<void>.delayed(Duration.zero);active=false;ld.synchronizeIdentity();active=true;gate.complete(operationJSON(ref.operationID!));await f;
    expect(ld.retired,isTrue);expect(ld.operationReceipt,isNull);expect(newer.values.length,1);ld.dispose();ls.dispose();late.close();
  });
}
