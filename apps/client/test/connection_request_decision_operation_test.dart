import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/connection_request_decision_operation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'connection_request_review_entry_test.dart' show reviewOwner,reviewID;

const serviceOperation = '44444444-4444-4444-8444-444444444444';
// Original registered handler unit wire, synthetic Store/session; not native PG.
Map<String,dynamic> operationWire(String name) => jsonDecode(File(
  '../../work/connection-decision-operation-recovery-2026-10-07/http-static04/wires/$name.json').readAsStringSync()) as Map<String,dynamic>;
Map<String,dynamic> operationResponse(String operation,{String wire='commit'}) {
  final root=operationWire(wire);
  (root['data'] as Map<String,dynamic>)['operationId']=operation;
  return root;
}
ConnectionRequestOperationReceipt decodeOperation(Map<String,dynamic> v) =>
  ConnectionRequestOperationReceipt.decode(v,owner:reviewOwner,request:reviewID,
    operation:serviceOperation,action:'accept',scope:'friend');
void main(){
  test('实际注册服务COMMITTED与NO_EFFECT wire绑定原操作且不含聊天权限',(){
    final committed=decodeOperation(operationWire('commit')['data']);
    expect(committed.committed,isTrue);expect(committed.state,'accepted');
    expect(committed.requestDigest,connectionDecisionDigest(reviewOwner,reviewID,'accept'));
    final refused=decodeOperation(operationWire('no_effect')['data']);
    expect(refused.committed,isFalse);expect(refused.reason,'EXPIRED');
    expect(operationWire('commit')['data'].containsKey('conversationId'),isFalse);
  });
  test('错主体操作摘要和伪批准无效回执严格拒绝',(){
    for(final field in ['ownerId','operationId','requestDigest','state','status']){
      final v=Map<String,dynamic>.from(operationWire('commit')['data']);v[field]='INVALID';
      expect(()=>decodeOperation(v),throwsFormatException,reason:field);
    }
    final v=Map<String,dynamic>.from(operationWire('commit')['data']);v['authority']='allowed';
    expect(()=>decodeOperation(v),throwsFormatException);
    v.remove('authority');v['recordedAt']='2026-10-07T01:00:00+00:00';
    expect(()=>decodeOperation(v),throwsFormatException);
  });
}
