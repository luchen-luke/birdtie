import 'dart:convert';

import 'package:crypto/crypto.dart';

import 'agent_result_projection.dart';

const agentReplyMembershipSchema = 'agent-reply-membership-v1';
const agentReplyHistoryKinds = {'activity', 'place', 'organization'};

String agentReplyTurnDigest(Iterable<({String role, String text})> messages) {
  // Match the native Go role/text prefix encoding, including HTML escaping.
  final encoded =
      jsonEncode([
            for (final message in messages)
              {'role': message.role, 'text': message.text},
          ])
          .replaceAll('<', r'\u003c')
          .replaceAll('>', r'\u003e')
          .replaceAll('&', r'\u0026')
          .replaceAll('\u2028', r'\u2028')
          .replaceAll('\u2029', r'\u2029');
  return sha256.convert(utf8.encode(encoded)).toString();
}

/// Saved membership identifies a past reply. It grants no domain permission.
/// Cards and map coordinates come only from a fresh authenticated history read.
class AgentReplyMembership {
  AgentReplyMembership._({
    required this.taskID,
    required this.cityID,
    required this.kind,
    required this.turnDigest,
    required this.resultSetID,
    required List<AgentResultRef> refs,
  }) : refs = List.unmodifiable(refs);

  final String taskID, cityID, kind, turnDigest, resultSetID;
  final List<AgentResultRef> refs;

  static AgentReplyMembership? read(Object? value) {
    if (value == null) return null;
    void require(bool condition) {
      if (!condition) throw const FormatException('这条回答的结果引用无法读取');
    }

    require(value is Map<String, dynamic>);
    final raw = value as Map<String, dynamic>;
    require(
      raw.length == 7 &&
          raw.keys.every(
            (key) => const {
              'schema',
              'taskId',
              'cityId',
              'kind',
              'turnDigest',
              'resultSetId',
              'refs',
            }.contains(key),
          ) &&
          raw['schema'] == agentReplyMembershipSchema,
    );
    String text(String key, int max) {
      final item = raw[key];
      require(
        item is String &&
            item.isNotEmpty &&
            item.trim() == item &&
            item.runes.length <= max &&
            !item.runes.any((r) => r < 32 || r == 127),
      );
      return item as String;
    }

    final taskID = text('taskId', 80);
    final cityID = text('cityId', 120);
    final kind = text('kind', 30);
    final digest = text('turnDigest', 64);
    final resultSetID = text('resultSetId', 200);
    require(
      agentReplyHistoryKinds.contains(kind) &&
          RegExp(r'^[0-9a-f]{64}$').hasMatch(digest) &&
          resultSetID == 'reply:$taskID:${digest.substring(0, 24)}' &&
          raw['refs'] is List &&
          (raw['refs'] as List).length <= 30,
    );
    final refs = [
      for (final ref in raw['refs'] as List) AgentResultRef.decode(ref),
    ];
    require(
      refs.every((ref) => ref.type == kind) &&
          refs.toSet().length == refs.length,
    );
    return AgentReplyMembership._(
      taskID: taskID,
      cityID: cityID,
      kind: kind,
      turnDigest: digest,
      resultSetID: resultSetID,
      refs: refs,
    );
  }
}

/// A read lifetime retires permanently on expiry or a context change. Saved
/// membership and message citations never extend or recreate this lifetime.
class AgentReplyProjectionLifetime {
  AgentReplyProjectionLifetime({
    required this.validUntil,
    required bool Function() current,
    DateTime Function()? now,
  }) : _sourceCurrent = current,
       _now = now ?? (() => DateTime.now().toUtc());

  final DateTime validUntil;
  final bool Function() _sourceCurrent;
  final DateTime Function() _now;
  bool _retired = false;

  bool get current {
    if (!_sourceCurrent() || !validUntil.isAfter(_now().toUtc())) {
      _retired = true;
    }
    return !_retired;
  }

  Duration get remaining =>
      current ? validUntil.difference(_now().toUtc()) : Duration.zero;
}
