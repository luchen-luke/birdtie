import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'entity_share_pending_store.dart' show chatEntityUUID;

const connectionDecisionOperationSchema = 'birdtie.connection-decision.v1';
String connectionDecisionDigest(String owner, String request, String action) {
  if (!chatEntityUUID.hasMatch(owner) || !chatEntityUUID.hasMatch(request) ||
      !const {'accept', 'decline', 'withdraw'}.contains(action)) {
    throw const FormatException('申请操作归属不符');
  }
  return sha256.convert(utf8.encode('$connectionDecisionOperationSchema\u0000${jsonEncode({'ownerId': owner, 'requestId': request, 'action': action})}')).toString();
}

/// Historical service outcome only, never current consent or chat authority.
class ConnectionRequestOperationReceipt {
  const ConnectionRequestOperationReceipt._(this.ownerID, this.requestID,
      this.operationID, this.requestDigest, this.action, this.scope,
      this.status, this.state, this.reason, this.recordedAt);
  final String ownerID, requestID, operationID, requestDigest, action, scope,
      status, state;
  final String? reason;
  final DateTime recordedAt;
  bool get committed => status == 'COMMITTED';
  static ConnectionRequestOperationReceipt decode(Map<String, dynamic> value, {
    required String owner, required String request, required String operation,
    required String action, required String scope,
  }) {
    const fields = {'schemaVersion', 'ownerId', 'requestId', 'operationId',
      'requestDigest', 'action', 'scope', 'status', 'state', 'recordedAt', 'reason'};
    final time = value['recordedAt'];
    final at = time is String && time.length <= 40 && time.endsWith('Z')
        ? DateTime.tryParse(time) : null;
    final state = const {'accept':'accepted', 'decline':'declined', 'withdraw':'withdrawn'}[action];
    final committed = value['status'] == 'COMMITTED';
    if (!chatEntityUUID.hasMatch(operation) || state == null ||
        !const {'friend', 'conversation'}.contains(scope) ||
        value.keys.any((k) => !fields.contains(k)) ||
        value.length != (value.containsKey('reason') ? 11 : 10) ||
        value['schemaVersion'] != connectionDecisionOperationSchema ||
        value['ownerId'] != owner || value['requestId'] != request ||
        value['operationId'] != operation || value['action'] != action ||
        value['scope'] != scope ||
        value['requestDigest'] != connectionDecisionDigest(owner,request,action) ||
        at == null || at.year < 2000 || at.year > 2200 ||
        (committed ? value['state'] != state || value.containsKey('reason') :
          value['status'] != 'NO_EFFECT' || value['state'] != '' ||
          !const {'EXPIRED','ALREADY_DECIDED'}.contains(value['reason']))) {
      throw const FormatException('服务操作回执与原申请不符');
    }
    return ConnectionRequestOperationReceipt._(owner,request,operation,
      value['requestDigest'],action,scope,value['status'],value['state'],
      value['reason'],at.toUtc());
  }
}
