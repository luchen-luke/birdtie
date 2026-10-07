import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:flutter_test/flutter_test.dart';

const actionRef = EntityActionRef(
  'activity',
  '22e9cd18-babb-4359-9d1e-53593bedff47',
);
Map<String, dynamic> actionWire({
  EntityActionRef ref = actionRef,
  String? version,
  DateTime? now,
  Duration ttl = const Duration(seconds: 25),
}) {
  final n = (now ?? DateTime.now()).toUtc();
  return {
    'schema': entityActionSchema,
    'entityRef': {'type': ref.type, 'id': ref.id},
    'title': '真实原活动名称',
    'sourceVersion': version ?? 'a' * 64,
    'observedAt': n.toIso8601String(),
    'validUntil': n.add(ttl).toIso8601String(),
    'actions': [
      for (final k in EntityActionKind.values)
        {
          'kind': entityActionWireKind(k),
          'operation': switch (k) {
            EntityActionKind.connect => 'REQUEST_FRIEND',
            EntityActionKind.message => 'OPEN_CHAT',
            EntityActionKind.share => 'CHOOSE_RECIPIENT',
            EntityActionKind.join => 'JOIN',
            EntityActionKind.save => 'SAVE',
            EntityActionKind.navigate => 'NAVIGATE',
          },
          'targetRef': {'type': ref.type, 'id': ref.id},
          'state':
              const {
                EntityActionKind.share,
                EntityActionKind.join,
                EntityActionKind.save,
              }.contains(k)
              ? 'AVAILABLE'
              : 'UNAVAILABLE',
          'label': k == EntityActionKind.save ? '收藏' : '具体操作',
          'reason': '原领域提交前仍核验',
          'requiresConfirmation': k != EntityActionKind.message,
        },
    ],
  };
}

void main() {
  test('连接两种真实意图闭集，未知或跨动作操作不得默认执行', () {
    const ref = EntityActionRef(
      'person',
      '22e9cd18-babb-4359-9d1e-53593bedff47',
    );
    Map<String, dynamic> wire() {
      final j = actionWire(ref: ref);
      for (final dynamic a in j['actions'] as List) {
        a['state'] = a['kind'] == 'CONNECT' ? 'AVAILABLE' : 'UNAVAILABLE';
      }
      j['actions'][0]['allowedOperations'] = [
        'REQUEST_FRIEND',
        'REQUEST_CONVERSATION',
      ];
      return j;
    }

    final view = EntityActionView.decode(wire(), ref);
    final conversation = view
        .action(EntityActionKind.connect)
        .selectOperation('REQUEST_CONVERSATION')
        .reviewed(view);
    expect(conversation.operation, 'REQUEST_CONVERSATION');
    expect(conversation.conditionHeaders['X-Birdtie-Action-Version'], 'a' * 64);
    expect(
      conversation.conditionHeaders['X-Birdtie-Action-Until'],
      view.validUntil.toIso8601String(),
    );
    for (final operations in <List<String>>[
      [],
      ['REQUEST_FRIEND', 'REQUEST_FRIEND'],
      ['REQUEST_CONVERSATION', 'REQUEST_FRIEND'],
      ['REQUEST_FRIEND', 'EXECUTE'],
      ['REQUEST_FRIEND', 'OPEN_CHAT'],
    ]) {
      final j = wire();
      j['actions'][0]['allowedOperations'] = operations;
      expect(() => EntityActionView.decode(j, ref), throwsFormatException);
    }
    expect(
      () => conversation.selectOperation('ACCEPT_INVITATION'),
      throwsFormatException,
    );
  });
  test('邀请明确接受拒绝闭集，不从默认操作猜拒绝权限', () {
    const ref = EntityActionRef(
      'community',
      '22e9cd18-babb-4359-9d1e-53593bedff47',
    );
    Map<String, dynamic> wire() {
      final j = actionWire(ref: ref);
      final a = (j['actions'] as List)[3] as Map<String, dynamic>;
      a['operation'] = 'ACCEPT_INVITATION';
      a['allowedOperations'] = ['ACCEPT_INVITATION', 'DECLINE_INVITATION'];
      return j;
    }

    final v = EntityActionView.decode(wire(), ref);
    expect(
      v
          .action(EntityActionKind.join)
          .selectOperation('DECLINE_INVITATION')
          .label,
      '拒绝邀请',
    );
    expect(
      () => v.action(EntityActionKind.join).selectOperation('LEAVE'),
      throwsFormatException,
    );
    for (final ops in <List<String>>[
      [],
      ['ACCEPT_INVITATION', 'ACCEPT_INVITATION'],
      ['DECLINE_INVITATION', 'ACCEPT_INVITATION'],
      ['ACCEPT_INVITATION', 'EXECUTE'],
      ['ACCEPT_INVITATION', 'LEAVE'],
    ]) {
      final j = wire();
      j['actions'][3]['allowedOperations'] = ops;
      expect(() => EntityActionView.decode(j, ref), throwsFormatException);
    }
  });
  test('六闭集动作目标与原源版本严格，明确偏移日期兼容Go wire', () {
    final j = actionWire()
      ..['observedAt'] = '2026-10-04T20:00:00.442838+08:00'
      ..['validUntil'] = '2026-10-04T20:00:25.442838+08:00';
    final v = EntityActionView.decode(j, actionRef);
    expect(v.observedAt, DateTime.utc(2026, 10, 4, 12, 0, 0, 442, 838));
    expect(v.actions.length, 6);
    expect(() => v.actions.add(v.actions.first), throwsUnsupportedError);
    for (final bad in [
      '2026-13-04T20:00:00Z',
      '2026-10-32T20:00:00Z',
      '2026-10-04T25:00:00Z',
      '2026-10-04T20:00:00+24:00',
      '2026-10-04T20:00:00',
    ]) {
      expect(
        () => EntityActionView.decode({...j, 'observedAt': bad}, actionRef),
        throwsFormatException,
      );
    }
  });
  test('未知类型/状态/多余权限/缺确认/重复/伪目标/无期限拒绝', () {
    for (final mutate in <void Function(Map<String, dynamic>)>[
      (j) => j['permission'] = true,
      (j) => j.remove('validUntil'),
      (j) => j['sourceVersion'] = 'nativeauthority',
      (j) => j['actions'][0]['kind'] = 'EXECUTE',
      (j) => j['actions'][0]['state'] = 'APPROVED',
      (j) => j['actions'][2]['requiresConfirmation'] = false,
      (j) => j['actions'][1] = j['actions'][0],
      (j) => j['actions'][2]['targetRef'] = {
        'type': 'activity',
        'id': '0d66ef33-9cec-41e5-9a57-2acea3c2eee2',
      },
      (j) => j['actions'][0]['state'] = 'AVAILABLE',
    ]) {
      final j = actionWire();
      mutate(j);
      expect(
        () => EntityActionView.decode(j, actionRef),
        throwsFormatException,
      );
    }
    expect(
      () => EntityActionView.decode(
        actionWire(ttl: const Duration(minutes: 2)),
        actionRef,
      ),
      throwsFormatException,
    );
    expect(
      () => EntityActionView.decode(actionWire(ttl: Duration.zero), actionRef),
      throwsFormatException,
    );
  });
}
