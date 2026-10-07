import 'package:birdtie_client/src/workspace/connection_request_decision_operation.dart';
import 'package:birdtie_client/src/workspace/connection_request_review_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'connection_request_review_entry_test.dart' show reviewOwner,reviewID;
import 'connection_request_decision_operation_test.dart' show serviceOperation;
PendingConnectionReview recoveryReference({bool keyed=true,String action='accept'})=>PendingConnectionReview(
  ownerID:reviewOwner,requestID:reviewID,referenceID:'55555555-5555-4555-8555-555555555555',
  operationID:keyed?serviceOperation:null,requestDigest:keyed?connectionDecisionDigest(reviewOwner,reviewID,action):null,
  action:action,scope:'friend',direction:'incoming',observedAt:DateTime.utc(2026,10,7),
  requestCreatedAt:DateTime.utc(2026,10,6),requestExpiresAt:DateTime.utc(2099,10,6));
void main(){
  test('旧本机v1引用永不升级成服务key新v2严格恢复且比较清理',()async{
    final store=MemoryConnectionReviewPendingStore();
    final old=recoveryReference(keyed:false);expect(old.toJson().length,9);
    expect(PendingConnectionReview.fromJson(old.toJson()).hasServiceOperation,isFalse);
    final v=recoveryReference();expect(v.toJson().length,12);expect(v.referenceID,isNot(v.operationID));
    await store.write('https://original.fixture',v);
    expect((await store.read('https://original.fixture',reviewOwner,reviewID))!.operationID,serviceOperation);
    expect(await store.compareDelete('https://original.fixture',recoveryReference(action:'decline')),isFalse);
    expect(store.values.length,1);expect(await store.compareDelete('https://original.fixture',v),isTrue);
  });
  test('服务key摘要错配和额外正文拒绝不存token',(){
    final v=recoveryReference().toJson();v['requestDigest']='changed';
    expect(()=>PendingConnectionReview.fromJson(v),throwsFormatException);
    final extra=recoveryReference().toJson();extra['note']='private';
    expect(()=>PendingConnectionReview.fromJson(extra),throwsFormatException);
    expect(recoveryReference().toJson().keys, isNot(contains('token')));
  });
}
