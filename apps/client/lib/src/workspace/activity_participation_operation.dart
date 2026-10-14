import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'entity_action_contract.dart';
import 'entity_share_pending_store.dart' show chatEntityUUID;

const participationOperationSchema = 'human.participation_operation.v1';
const participationClosureSchema = 'human.participation_operation_closure.v1';
const participationToolVersion = 'human.participation.write.v1';
final _hex = RegExp(r'^[0-9a-f]{64}$');
bool participationID(String value) =>
    chatEntityUUID.hasMatch(value) &&
    value != '00000000-0000-0000-0000-000000000000';
String participationEffectKey(String owner, String operation) => sha256
    .convert(
      utf8.encode(
        'birdtie.human-participation-effect.v1\n$owner\n$operation\n$operation\nACTIVITY_PARTICIPATION',
      ),
    )
    .toString();

String participationDeadline(DateTime value) {
  final iso = value.toUtc().toIso8601String();
  final parts = iso.substring(0, iso.length - 1).split('.');
  return '${parts.first}.${parts.length == 1 ? '000000' : parts.last.padRight(6, '0')}Z';
}

String participationRequestDigest(
  String owner,
  String activity,
  String id,
  EntityActionDescriptor approved,
) {
  final version = approved.reviewedVersion, until = approved.reviewedUntil;
  if (!participationID(owner) ||
      !participationID(activity) ||
      !participationID(id) ||
      approved.target != EntityActionRef('activity', activity) ||
      approved.kind != EntityActionKind.join ||
      !const {'JOIN', 'CANCEL_RSVP'}.contains(approved.operation) ||
      version == null ||
      !_hex.hasMatch(version) ||
      until == null) {
    throw const FormatException('请重新核对这次报名操作。');
  }
  return sha256
      .convert(
        utf8.encode(
          'birdtie.human-participation-request.v1\n$owner\n$activity\n$id\n${approved.operation}\n$participationToolVersion\n$version\n${participationDeadline(until)}',
        ),
      )
      .toString();
}

/// Recovery metadata only: no token, activity content or reusable approval.
class PendingParticipationOperation {
  const PendingParticipationOperation({
    required this.operationID,
    required this.activityID,
    required this.operation,
    required this.requestDigest,
    this.stopRequested = false,
  });
  final String operationID, activityID, operation, requestDigest;
  final bool stopRequested;
  Map<String, dynamic> toJson() => {
    'operationId': operationID,
    'activityId': activityID,
    'operation': operation,
    'requestDigest': requestDigest,
    'toolVersion': participationToolVersion,
    'stopRequested': stopRequested,
  };
  PendingParticipationOperation stopping() => PendingParticipationOperation(
    operationID: operationID,
    activityID: activityID,
    operation: operation,
    requestDigest: requestDigest,
    stopRequested: true,
  );
  bool sameBinding(PendingParticipationOperation other) =>
      operationID == other.operationID &&
      activityID == other.activityID &&
      operation == other.operation &&
      requestDigest == other.requestDigest;
  factory PendingParticipationOperation.decode(Object? raw) {
    final j = _closed(raw, {
      'operationId',
      'activityId',
      'operation',
      'requestDigest',
      'toolVersion',
      'stopRequested',
    });
    if (j['operationId'] is! String ||
        !participationID(j['operationId']) ||
        j['activityId'] is! String ||
        !participationID(j['activityId']) ||
        !const {'JOIN', 'CANCEL_RSVP'}.contains(j['operation']) ||
        j['requestDigest'] is! String ||
        !_hex.hasMatch(j['requestDigest']) ||
        j['toolVersion'] != participationToolVersion ||
        j['stopRequested'] is! bool) {
      throw const FormatException('原报名记录无法读取，请保留并核实。');
    }
    return PendingParticipationOperation(
      operationID: j['operationId'],
      activityID: j['activityId'],
      operation: j['operation'],
      requestDigest: j['requestDigest'],
      stopRequested: j['stopRequested'],
    );
  }
}

Map<String, dynamic> _closed(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      raw.keys.any((k) => !keys.contains(k))) {
    throw const FormatException('报名恢复记录不完整。');
  }
  return raw;
}

DateTime _stamp(Object? raw) {
  if (raw is! String ||
      !RegExp(
        r'^20[0-9]{2}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?Z$|^21[0-9]{2}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?Z$|^2200-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?Z$',
      ).hasMatch(raw)) {
    throw const FormatException('报名记录时间无效。');
  }
  final at = DateTime.tryParse(raw);
  // DateTime.parse normalizes dates such as February 30; reject that wire.
  if (at == null ||
      at.year < 2000 ||
      at.year > 2200 ||
      participationDeadline(at).substring(0, 19) != raw.substring(0, 19)) {
    throw const FormatException('报名记录时间无效。');
  }
  return at.toUtc();
}

const _baseKeys = {
  'schemaVersion',
  'ownerId',
  'activityId',
  'operationId',
  'operation',
  'effectKey',
  'requestDigest',
  'toolVersion',
  'recordedAt',
};
void _binding(
  Map<String, dynamic> j,
  String owner,
  PendingParticipationOperation p,
) {
  if (!participationID(owner) ||
      j['ownerId'] != owner ||
      j['activityId'] != p.activityID ||
      j['operationId'] != p.operationID ||
      j['operation'] != p.operation ||
      j['requestDigest'] != p.requestDigest ||
      j['toolVersion'] != participationToolVersion ||
      j['effectKey'] != participationEffectKey(owner, p.operationID)) {
    throw const FormatException('恢复记录与原报名不符，请继续核实。');
  }
}

class ParticipationOperationReceipt {
  const ParticipationOperationReceipt._(this.committed, this.recordedAt);
  final bool committed;
  final DateTime recordedAt;
  static ParticipationOperationReceipt decode(
    Object? raw,
    String owner,
    PendingParticipationOperation p,
  ) {
    final committed = raw is Map && raw['status'] == 'COMMITTED';
    final j = _closed(raw, {
      ..._baseKeys,
      'status',
      if (committed) ...{'effectId', 'state', 'effectAt'} else 'reason',
    });
    _binding(j, owner, p);
    if (j['schemaVersion'] != participationOperationSchema ||
        (!committed &&
            (j['status'] != 'NO_EFFECT' || j['reason'] != 'USER_CLOSED'))) {
      throw const FormatException('报名结果尚未核实。');
    }
    final at = _stamp(j['recordedAt']);
    if (committed) {
      if (j['effectId'] is! String ||
          !participationID(j['effectId']) ||
          j['state'] != (p.operation == 'JOIN' ? 'going' : 'cancelled') ||
          _stamp(j['effectAt']).isAfter(at)) {
        throw const FormatException('原报名效果无法核实。');
      }
    }
    return ParticipationOperationReceipt._(committed, at);
  }
}

class ParticipationOperationClosure {
  const ParticipationOperationClosure._(this.recordedAt);
  final DateTime recordedAt;
  static ParticipationOperationClosure decode(
    Object? raw,
    String owner,
    PendingParticipationOperation p,
  ) {
    final j = _closed(raw, {..._baseKeys, 'closed'});
    _binding(j, owner, p);
    if (j['schemaVersion'] != participationClosureSchema ||
        j['closed'] != true) {
      throw const FormatException('停止提交尚未核实。');
    }
    return ParticipationOperationClosure._(_stamp(j['recordedAt']));
  }
}

/// A strict JSON parser rejecting duplicate object keys, including escaped keys.
/// jsonDecode by itself would discard a duplicate binding silently.
Object? participationWireJSON(String text) {
  if (text.length > 8192) throw const FormatException('恢复记录过大。');
  var i = 0;
  void space() {
    while (i < text.length && ' \r\n\t'.contains(text[i])) {
      i++;
    }
  }

  String string() {
    final start = i++;
    while (i < text.length) {
      if (text[i] == '\\') {
        i += 2;
        continue;
      }
      if (text[i++] == '"') {
        return jsonDecode(text.substring(start, i)) as String;
      }
    }
    throw const FormatException('JSON string');
  }

  void Function(int) value = (_) {};
  value = (int depth) {
    if (depth > 12) throw const FormatException('JSON depth');
    space();
    if (i >= text.length) throw const FormatException('JSON value');
    if (text[i] == '{') {
      i++;
      space();
      final keys = <String>{};
      if (i < text.length && text[i] == '}') {
        i++;
        return;
      }
      while (true) {
        space();
        if (i >= text.length || text[i] != '"') {
          throw const FormatException('JSON key');
        }
        if (!keys.add(string())) {
          throw const FormatException('Duplicate JSON key');
        }
        space();
        if (i >= text.length || text[i++] != ':') {
          throw const FormatException('JSON colon');
        }
        value(depth + 1);
        space();
        if (i >= text.length) throw const FormatException('JSON object');
        final end = text[i++];
        if (end == '}') return;
        if (end != ',') throw const FormatException('JSON comma');
      }
    }
    if (text[i] == '[') {
      i++;
      space();
      if (i < text.length && text[i] == ']') {
        i++;
        return;
      }
      while (true) {
        value(depth + 1);
        space();
        if (i >= text.length) throw const FormatException('JSON array');
        final end = text[i++];
        if (end == ']') return;
        if (end != ',') throw const FormatException('JSON comma');
      }
    }
    if (text[i] == '"') {
      string();
      return;
    }
    final start = i;
    while (i < text.length && !' \r\n\t,}]'.contains(text[i])) {
      i++;
    }
    if (start == i) throw const FormatException('JSON primitive');
    jsonDecode(text.substring(start, i));
  };
  value(0);
  space();
  if (i != text.length) throw const FormatException('JSON tail');
  return jsonDecode(text);
}

Object? participationEnvelope(String text) {
  final j = _closed(participationWireJSON(text), {'data'});
  return j['data'];
}
