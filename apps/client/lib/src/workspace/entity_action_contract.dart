import 'now_context_query_api.dart' show onlineStamp, validOnlineID;

const entityActionSchema = 'entity-actions-v1';

enum EntityActionKind { connect, message, share, join, save, navigate }

String entityActionWireKind(EntityActionKind kind) => kind.name.toUpperCase();
Map<String, dynamic> _object(Object? value, Set<String> keys) {
  if (value is! Map<String, dynamic> ||
      value.length != keys.length ||
      value.keys.any((k) => !keys.contains(k))) {
    throw const FormatException('动作合同无法读取');
  }
  return value;
}

String _text(Object? value, int max) {
  if (value is! String ||
      value.trim() != value ||
      value.isEmpty ||
      value.runes.length > max ||
      value.runes.any((r) => r < 32 || r == 127)) {
    throw const FormatException('动作文字无法读取');
  }
  return value;
}

class EntityActionRef {
  const EntityActionRef(this.type, this.id);
  final String type, id;
  bool get valid =>
      const {
        'person',
        'activity',
        'place',
        'community',
        'organization',
        'business',
      }.contains(type) &&
      validOnlineID(id);
  factory EntityActionRef.decode(Object? raw) {
    final j = _object(raw, {'type', 'id'});
    final ref = EntityActionRef(_text(j['type'], 20), _text(j['id'], 36));
    if (!ref.valid) throw const FormatException('动作目标无法读取');
    return ref;
  }
  @override
  bool operator ==(Object other) =>
      other is EntityActionRef && other.type == type && other.id == id;
  @override
  int get hashCode => Object.hash(type, id);
}

/// A current human proposal, never a capability or a stored approval.
class EntityActionDescriptor {
  const EntityActionDescriptor({
    required this.kind,
    required this.target,
    required this.available,
    required this.label,
    required this.reason,
    required this.requiresConfirmation,
    required this.operation,
    this.allowedOperations = const [],
    this.reviewedVersion,
    this.reviewedUntil,
  });
  final EntityActionKind kind;
  final EntityActionRef target;
  final bool available, requiresConfirmation;
  final String label, reason;
  final String operation;
  final List<String> allowedOperations;
  // Only carried by the local checked callback. Not a permission or a wire
  // descriptor field; original writers treat it as an optimistic condition.
  final String? reviewedVersion;
  final DateTime? reviewedUntil;
  Map<String, String> get conditionHeaders {
    if (reviewedVersion == null || reviewedUntil == null) {
      throw StateError('请重新检查当前操作。');
    }
    return {
      'X-Birdtie-Action-Version': reviewedVersion!,
      'X-Birdtie-Action-Until': reviewedUntil!.toUtc().toIso8601String(),
      'X-Birdtie-Action-Operation': operation,
    };
  }

  EntityActionDescriptor reviewed(EntityActionView view) =>
      EntityActionDescriptor(
        kind: kind,
        target: target,
        available: available,
        label: label,
        reason: reason,
        requiresConfirmation: requiresConfirmation,
        operation: operation,
        allowedOperations: operations,
        reviewedVersion: view.sourceVersion,
        reviewedUntil: view.validUntil,
      );
  List<String> get operations =>
      allowedOperations.isEmpty ? [operation] : allowedOperations;
  EntityActionDescriptor selectOperation(String selected) {
    if (!operations.contains(selected)) {
      throw const FormatException('具体操作不在当前合同中');
    }
    return EntityActionDescriptor(
      kind: kind,
      target: target,
      available: available,
      label: selected == 'DECLINE_INVITATION' ? '拒绝邀请' : label,
      reason: reason,
      requiresConfirmation: requiresConfirmation,
      operation: selected,
      allowedOperations: operations,
      reviewedVersion: reviewedVersion,
      reviewedUntil: reviewedUntil,
    );
  }

  factory EntityActionDescriptor.decode(Object? raw, EntityActionRef expected) {
    final j = _object(raw, {
      'kind',
      'targetRef',
      'state',
      'label',
      'reason',
      'requiresConfirmation',
      'operation',
      if (raw is Map && raw.containsKey('allowedOperations'))
        'allowedOperations',
    });
    final wire = j['kind'];
    final kinds = EntityActionKind.values.where(
      (k) => entityActionWireKind(k) == wire,
    );
    if (kinds.length != 1 ||
        !const {'AVAILABLE', 'UNAVAILABLE'}.contains(j['state']) ||
        j['requiresConfirmation'] is! bool) {
      throw const FormatException('动作类型或状态无法读取');
    }
    final target = EntityActionRef.decode(j['targetRef']);
    if (target != expected) throw const FormatException('动作目标不一致');
    final d = EntityActionDescriptor(
      kind: kinds.single,
      target: target,
      available: j['state'] == 'AVAILABLE',
      label: _text(j['label'], 80),
      reason: _text(j['reason'], 300),
      requiresConfirmation: j['requiresConfirmation'] as bool,
      operation: _text(j['operation'], 32),
      allowedOperations: j.containsKey('allowedOperations')
          ? List.unmodifiable(
              (j['allowedOperations'] is List
                      ? j['allowedOperations'] as List
                      : throw const FormatException('操作列表无法读取'))
                  .map((v) => _text(v, 32)),
            )
          : const [],
    );
    if (!validEntityActionOperation(d.kind, d.operation, target.type)) {
      throw const FormatException('动作具体操作无法读取');
    }
    final ops = d.operations;
    if ((j.containsKey('allowedOperations') && d.allowedOperations.isEmpty) ||
        ops.length > 2 ||
        ops.toSet().length != ops.length ||
        ops.first != d.operation ||
        ops.any((op) => !validEntityActionOperation(d.kind, op, target.type)) ||
        (ops.length > 1 &&
            !(target.type == 'person' &&
                d.kind == EntityActionKind.connect &&
                d.operation == 'REQUEST_FRIEND' &&
                ops.contains('REQUEST_CONVERSATION')) &&
            !(target.type == 'community' &&
                d.kind == EntityActionKind.join &&
                d.operation == 'ACCEPT_INVITATION' &&
                ops.contains('DECLINE_INVITATION')) &&
            !(const {'activity', 'place'}.contains(target.type) &&
                d.kind == EntityActionKind.share &&
                d.operation == 'CHOOSE_RECIPIENT' &&
                ops.contains('EXPORT_PUBLIC')))) {
      throw const FormatException('具体操作列表不合法');
    }
    if (d.available &&
        !switch (d.kind) {
          EntityActionKind.connect ||
          EntityActionKind.message => target.type == 'person',
          EntityActionKind.join =>
            target.type == 'activity' || target.type == 'community',
          EntityActionKind.save => const {
            'activity',
            'place',
            'community',
          }.contains(target.type),
          EntityActionKind.navigate =>
            target.type == 'activity' || target.type == 'place',
          EntityActionKind.share => true,
        }) {
      throw const FormatException('动作与实体类型不一致');
    }
    if (d.kind != EntityActionKind.message && !d.requiresConfirmation) {
      throw const FormatException('动作缺少确认边界');
    }
    return d;
  }
}

bool validEntityActionOperation(
  EntityActionKind kind,
  String op,
  String entity,
) => switch (kind) {
  EntityActionKind.connect =>
    op == 'REQUEST_FRIEND' ||
        (entity == 'person' && op == 'REQUEST_CONVERSATION'),
  EntityActionKind.message => op == 'OPEN_CHAT',
  EntityActionKind.share =>
    op == 'CHOOSE_RECIPIENT' ||
        (const {'activity', 'place'}.contains(entity) && op == 'EXPORT_PUBLIC'),
  EntityActionKind.save => op == 'SAVE' || op == 'UNSAVE',
  EntityActionKind.navigate => op == 'NAVIGATE',
  EntityActionKind.join =>
    op == 'JOIN' ||
        (entity == 'activity' && op == 'CANCEL_RSVP') ||
        (entity == 'community' &&
            const {
              'LEAVE',
              'REQUEST_JOIN',
              'ACCEPT_INVITATION',
              'DECLINE_INVITATION',
            }.contains(op)),
};

List<EntityActionDescriptor> decodeEntityActions(
  Object? raw,
  EntityActionRef expected,
) {
  if (raw is! List || raw.length != EntityActionKind.values.length) {
    throw const FormatException('动作集合无法读取');
  }
  final result = [
    for (final a in raw) EntityActionDescriptor.decode(a, expected),
  ];
  if (result.map((a) => a.kind).toSet().length != 6) {
    throw const FormatException('动作重复');
  }
  return List.unmodifiable(result);
}

bool validEntityActionVersion(Object? value) =>
    value is String && RegExp(r'^[0-9a-f]{64}$').hasMatch(value);

class EntityActionView {
  EntityActionView._(
    this.entity,
    this.title,
    this.sourceVersion,
    this.actions,
    this.observedAt,
    this.validUntil,
  );
  final EntityActionRef entity;
  final String title, sourceVersion;
  final List<EntityActionDescriptor> actions;
  final DateTime observedAt, validUntil;
  bool live(DateTime now) => validUntil.isAfter(now.toUtc());
  EntityActionDescriptor action(EntityActionKind kind) =>
      actions.singleWhere((a) => a.kind == kind);
  factory EntityActionView.decode(Object? raw, EntityActionRef expected) {
    final j = _object(raw, {
      'schema',
      'entityRef',
      'title',
      'sourceVersion',
      'actions',
      'observedAt',
      'validUntil',
    });
    final ref = EntityActionRef.decode(j['entityRef']);
    final observed = onlineStamp(j['observedAt']),
        end = onlineStamp(j['validUntil']);
    if (j['schema'] != entityActionSchema ||
        ref != expected ||
        !validEntityActionVersion(j['sourceVersion']) ||
        !end.isAfter(observed) ||
        end.difference(observed) > const Duration(seconds: 30)) {
      throw const FormatException('动作身份、版本或期限不一致');
    }
    return EntityActionView._(
      ref,
      _text(j['title'], 300),
      j['sourceVersion'] as String,
      decodeEntityActions(j['actions'], ref),
      observed,
      end,
    );
  }
}
