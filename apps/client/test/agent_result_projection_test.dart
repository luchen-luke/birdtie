import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter_test/flutter_test.dart';

Map<String, dynamic> typedItem(
  String kind,
  String id, {
  String scope = 'AUTHORIZED_VIEW',
  Map<String, dynamic>? anchor,
}) => {
  'entityRef': {'type': kind, 'id': id},
  'title': '原实体中文标题',
  'summary': '原始摘要\n可完整读取',
  'scope': scope,
  if (kind != 'group')
    'detailRef': {
      'type': kind == 'opportunity' ? 'activity' : kind,
      'id': kind == 'opportunity' ? id.split(':').last : id,
    },
  if (kind != 'group')
    'shareRef': {
      'type': kind == 'opportunity' ? 'activity' : kind,
      'id': kind == 'opportunity' ? id.split(':').last : id,
    },
  'anchor': ?anchor,
};
Map<String, dynamic> typedSet(List<Map<String, dynamic>> items) => {
  'schema': typedAgentResultSchema,
  'items': items,
  'entities': [for (final i in items) i['entityRef']],
};

void main() {
  test('真实七类协议接收闭集动作合同并保持原Ref', () {
    const id = '22e9cd18-babb-4359-9d1e-53593bedff47';
    final raw = typedItem('activity', id)
      ..['actionsSourceVersion'] = 'a' * 64
      ..['actionsValidUntil'] = '2026-10-04T12:00:30+08:00'
      ..['actions'] = [
        for (final kind in [
          'CONNECT',
          'MESSAGE',
          'SHARE',
          'JOIN',
          'SAVE',
          'NAVIGATE',
        ])
          {
            'kind': kind,
            'operation': {
              'CONNECT': 'REQUEST_FRIEND',
              'MESSAGE': 'OPEN_CHAT',
              'SHARE': 'CHOOSE_RECIPIENT',
              'JOIN': 'JOIN',
              'SAVE': 'SAVE',
              'NAVIGATE': 'NAVIGATE',
            }[kind],
            'targetRef': {'type': 'activity', 'id': id},
            'state': kind == 'SHARE' ? 'AVAILABLE' : 'UNAVAILABLE',
            'label': '具体操作',
            'reason': '原领域仍需核验',
            'requiresConfirmation': kind != 'MESSAGE',
          },
      ];
    expect(AgentResultItem.decode(raw).valid, true);
  });
  test('仅收藏动作兼容旧Community外键名，不改变原实体类型', () {
    final community = AgentResultItem.decode(
      typedItem('community', 'original-community'),
    );
    expect(community.entity.type, 'community');
    expect(community.bookmarkKind, 'group');
    expect(community.detail!.type, 'community');
    expect(community.share!.type, 'community');
    expect(
      AgentResultItem.decode(typedItem('group', 'legacy')).bookmarkKind,
      isNull,
    );
    expect(
      AgentResultItem.decode(
        typedItem('business', 'original-business'),
      ).bookmarkKind,
      isNull,
    );
  });
  test('七类稳定原引用同源，私密机会详情分享仅原活动，无坐标不造Pin', () {
    final raw = [
      for (final k in [
        'person',
        'activity',
        'place',
        'community',
        'organization',
        'business',
      ])
        typedItem(k, 'original-$k'),
      typedItem(
        'opportunity',
        'own-intent:original-activity',
        scope: 'SELF_PRIVATE',
      ),
    ];
    final items = decodeAgentResultItems(typedSet(raw));
    expect(items.length, 7);
    expect(items.every((i) => i.valid && i.mapEntity == null), true);
    expect(
      items.last.share,
      const AgentResultRef(type: 'activity', id: 'original-activity'),
    );
    expect(() => items.add(items.first), throwsUnsupportedError);
    raw.first['title'] = '修改输入';
    expect(items.first.title, '原实体中文标题');
  });
  test('原生Community和旧Group区分，只有实际公开锚点创建同一实体Pin', () {
    final community = AgentResultItem.decode(
      typedItem(
        'community',
        'native-community',
        anchor: {
          'coordinateSystem': 'wgs84',
          'precision': 'point',
          'latitude': 57.14,
          'longitude': -2.1,
        },
      ),
    );
    expect(community.mapEntity!.id, community.entity.mapID);
    expect(community.mapEntity!.kind, MapEntityKind.community);
    final group = AgentResultItem.decode(typedItem('group', 'legacy-group'));
    expect(group.detail, isNull);
    expect(group.share, isNull);
    expect(group.mapEntity, isNull);
    final renamed = typedItem('group', 'legacy-group')
      ..['detailRef'] = {'type': 'community', 'id': 'legacy-group'};
    expect(() => AgentResultItem.decode(renamed), throwsFormatException);
  });
  test('未知、重复、错序、冲突和私人分享目标均拒绝', () {
    final good = typedItem('activity', 'original-activity');
    for (final broken in [
      {
        ...good,
        'entityRef': {'type': 'unknown', 'id': 'original-activity'},
      },
      {
        ...good,
        'shareRef': {'type': 'activity', 'id': 'other-activity'},
      },
      {
        ...typedItem(
          'opportunity',
          'own-intent:original-activity',
          scope: 'SELF_PRIVATE',
        ),
        'shareRef': {
          'type': 'opportunity',
          'id': 'own-intent:original-activity',
        },
      },
      {...good, 'scope': 'SELF_PRIVATE'},
      {
        ...good,
        'anchor': {
          'coordinateSystem': 'wgs84',
          'precision': 'point',
          'latitude': double.nan,
          'longitude': 0,
        },
      },
    ]) {
      expect(() => AgentResultItem.decode(broken), throwsFormatException);
    }
    expect(
      () => decodeAgentResultItems(typedSet([good, good])),
      throwsFormatException,
    );
    final wrong = typedSet([good])
      ..['entities'] = [
        {'type': 'place', 'id': 'original-activity'},
      ];
    expect(() => decodeAgentResultItems(wrong), throwsFormatException);
  });
  test('Person仅明确公开粗略区域，不允许精确个人点位', () {
    final coarse = typedItem(
      'person',
      'person-a',
      anchor: {
        'coordinateSystem': 'wgs84',
        'precision': 'area',
        'publicZone': 'north',
        'latitude': 57.14,
        'longitude': -2.1,
      },
    );
    expect(AgentResultItem.decode(coarse).mapEntity!.subtitle, '公开的大致区域');
    expect(
      () => AgentResultItem.decode({
        ...coarse,
        'anchor': {
          'coordinateSystem': 'wgs84',
          'precision': 'point',
          'publicZone': 'north',
          'latitude': 57.14,
          'longitude': -2.1,
        },
      }),
      throwsFormatException,
    );
  });
}
