import 'map_entities.dart';
import 'entity_action_contract.dart';
import 'now_context_query_api.dart' show onlineStamp;

const typedAgentResultSchema = 'typed-agent-results-v1';
const _productKinds = {
  'person',
  'activity',
  'place',
  'community',
  'organization',
  'business',
  'opportunity',
};

bool _text(String s, int min, int max, {bool multiline = false}) =>
    s.trim() == s &&
    s.runes.length >= min &&
    s.runes.length <= max &&
    !s.runes.any(
      (r) =>
          (r < 32 || r == 127) &&
          !(multiline && (r == 9 || r == 10 || r == 13)),
    );

class AgentResultRef {
  const AgentResultRef({required this.type, required this.id});
  final String type, id;
  String get mapID => '$type:$id';
  bool get valid =>
      (_productKinds.contains(type) || type == 'group') && _text(id, 1, 180);
  factory AgentResultRef.decode(Object? value) {
    if (value is! Map<String, dynamic> ||
        value.length != 2 ||
        value['type'] is! String ||
        value['id'] is! String) {
      throw const FormatException('实体引用无法读取');
    }
    final r = AgentResultRef(
      type: value['type'] as String,
      id: value['id'] as String,
    );
    if (!r.valid) throw const FormatException('实体引用类型未知');
    return r;
  }
  @override
  bool operator ==(Object other) =>
      other is AgentResultRef && other.type == type && other.id == id;
  @override
  int get hashCode => Object.hash(type, id);
}

class AgentResultAnchor {
  const AgentResultAnchor({
    required this.precision,
    required this.latitude,
    required this.longitude,
    this.publicZone = '',
    this.placeID = '',
  });
  final String precision, publicZone, placeID;
  final double latitude, longitude;
  bool validFor(String kind) =>
      latitude.isFinite &&
      longitude.isFinite &&
      latitude >= -90 &&
      latitude <= 90 &&
      longitude >= -180 &&
      longitude <= 180 &&
      (kind == 'person'
          ? precision == 'area' &&
                const {
                  'city_centre',
                  'north',
                  'south',
                  'east',
                  'west',
                }.contains(publicZone)
          : _productKinds.contains(kind) &&
                precision == 'point' &&
                publicZone.isEmpty);
  factory AgentResultAnchor.decode(Object? value) {
    if (value is! Map<String, dynamic> ||
        value.keys.any(
          (k) => !const {
            'coordinateSystem',
            'precision',
            'latitude',
            'longitude',
            'publicZone',
            'placeId',
          }.contains(k),
        ) ||
        value['coordinateSystem'] != 'wgs84' ||
        value['precision'] is! String ||
        value['latitude'] is! num ||
        value['longitude'] is! num ||
        (value['publicZone'] != null && value['publicZone'] is! String) ||
        (value['placeId'] != null && value['placeId'] is! String)) {
      throw const FormatException('公开点位无法读取');
    }
    return AgentResultAnchor(
      precision: value['precision'] as String,
      latitude: (value['latitude'] as num).toDouble(),
      longitude: (value['longitude'] as num).toDouble(),
      publicZone: value['publicZone'] as String? ?? '',
      placeID: value['placeId'] as String? ?? '',
    );
  }
}

/// The card, optional Pin, detail and share targets all use this one item.
/// Presence is not an authorization: original domain endpoints recheck access.
class AgentResultItem {
  const AgentResultItem({
    required this.entity,
    required this.title,
    required this.summary,
    required this.scope,
    this.detail,
    this.share,
    this.anchor,
    this.sourceVersion = '',
    this.actions,
    this.actionsValidUntil,
    this.actionsSourceVersion = '',
  });
  final AgentResultRef entity;
  final String title, summary, scope, sourceVersion;
  final AgentResultRef? detail, share;
  final AgentResultAnchor? anchor;
  final List<EntityActionDescriptor>? actions;
  final DateTime? actionsValidUntil;
  final String actionsSourceVersion;
  EntityActionDescriptor? action(EntityActionKind kind) {
    if (actionsValidUntil?.isAfter(DateTime.now().toUtc()) != true) return null;
    for (final a in actions ?? const <EntityActionDescriptor>[]) {
      if (a.kind == kind) return a;
    }
    return null;
  }

  // The existing bookmark API calls its Community FK "group". Only this
  // action uses that compatibility name; the entity remains a Community.
  String? get bookmarkKind => switch (entity.type) {
    'community' => 'group',
    'activity' || 'place' => entity.type,
    _ => null,
  };
  bool get valid {
    if (actions != null &&
        (actions!.length != 6 ||
            actions!.map((a) => a.kind).toSet().length != 6 ||
            actionsValidUntil == null ||
            !validEntityActionVersion(actionsSourceVersion) ||
            actions!.any(
              (a) =>
                  a.target !=
                  EntityActionRef(
                    (entity.type == 'opportunity' ? detail : entity)?.type ??
                        '',
                    (entity.type == 'opportunity' ? detail : entity)?.id ?? '',
                  ),
            ))) {
      return false;
    }
    if (actions == null &&
        (actionsValidUntil != null || actionsSourceVersion.isNotEmpty)) {
      return false;
    }
    if (!entity.valid ||
        !_text(title, 1, 300) ||
        !_text(summary, 0, 1000, multiline: true) ||
        !_text(sourceVersion, 0, 180) ||
        !const {'AUTHORIZED_VIEW', 'SELF_PRIVATE'}.contains(scope) ||
        (anchor != null && !anchor!.validFor(entity.type))) {
      return false;
    }
    if (entity.type == 'group') {
      return scope == 'AUTHORIZED_VIEW' &&
          detail == null &&
          share == null &&
          anchor == null;
    }
    if (entity.type == 'opportunity') {
      final ids = entity.id.split(':');
      return scope == 'SELF_PRIVATE' &&
          ids.length == 2 &&
          _text(ids[0], 1, 80) &&
          _text(ids[1], 1, 80) &&
          detail == AgentResultRef(type: 'activity', id: ids[1]) &&
          (share == null || share == detail);
    }
    return scope == 'AUTHORIZED_VIEW' &&
        (detail == null || detail == entity) &&
        (share == null || share == entity);
  }

  factory AgentResultItem.decode(Object? value) {
    if (value is! Map<String, dynamic> ||
        value.keys.any(
          (k) => !const {
            'entityRef',
            'title',
            'summary',
            'scope',
            'detailRef',
            'shareRef',
            'anchor',
            'sourceVersion',
            'actions',
            'actionsValidUntil',
            'actionsSourceVersion',
          }.contains(k),
        ) ||
        value['title'] is! String ||
        value['summary'] is! String ||
        value['scope'] is! String ||
        (value['sourceVersion'] != null && value['sourceVersion'] is! String)) {
      throw const FormatException('实体卡片无法读取');
    }
    final entity = AgentResultRef.decode(value['entityRef']);
    final detail = value['detailRef'] == null
        ? null
        : AgentResultRef.decode(value['detailRef']);
    final target = entity.type == 'opportunity' ? detail : entity;
    final item = AgentResultItem(
      entity: entity,
      title: value['title'] as String,
      summary: value['summary'] as String,
      scope: value['scope'] as String,
      detail: detail,
      share: value['shareRef'] == null
          ? null
          : AgentResultRef.decode(value['shareRef']),
      anchor: value['anchor'] == null
          ? null
          : AgentResultAnchor.decode(value['anchor']),
      sourceVersion: value['sourceVersion'] as String? ?? '',
      actions: !value.containsKey('actions')
          ? null
          : decodeEntityActions(
              value['actions'],
              EntityActionRef(target?.type ?? '', target?.id ?? ''),
            ),
      actionsValidUntil: !value.containsKey('actionsValidUntil')
          ? null
          : onlineStamp(value['actionsValidUntil']),
      actionsSourceVersion: !value.containsKey('actionsSourceVersion')
          ? ''
          : value['actionsSourceVersion'] is String
          ? value['actionsSourceVersion'] as String
          : throw const FormatException('动作版本无法读取'),
    );
    if (!item.valid) throw const FormatException('实体引用或权限范围不一致');
    return item;
  }
  MapEntity? get mapEntity {
    if (!valid || anchor == null) return null;
    final kind = switch (entity.type) {
      'person' => MapEntityKind.person,
      'activity' => MapEntityKind.activity,
      'place' => MapEntityKind.place,
      'community' => MapEntityKind.community,
      'organization' => MapEntityKind.organization,
      'business' => MapEntityKind.business,
      'opportunity' => MapEntityKind.opportunity,
      _ => null,
    };
    if (kind == null) return null;
    return MapEntity(
      id: entity.mapID,
      kind: kind,
      title: title,
      subtitle: entity.type == 'person' ? '公开的大致区域' : summary,
      latitude: anchor!.latitude,
      longitude: anchor!.longitude,
    );
  }
}

List<AgentResultItem> decodeAgentResultItems(Map<String, dynamic> resultSet) {
  if (resultSet['schema'] != typedAgentResultSchema ||
      resultSet['items'] is! List ||
      resultSet['entities'] is! List ||
      (resultSet['items'] as List).length > 200) {
    throw const FormatException('结果卡片协议不匹配');
  }
  final items = [
    for (final raw in resultSet['items'] as List) AgentResultItem.decode(raw),
  ];
  final refs = [
    for (final raw in resultSet['entities'] as List) AgentResultRef.decode(raw),
  ];
  if (refs.length != items.length) throw const FormatException('结果引用数量不一致');
  final seen = <String>{};
  for (var n = 0; n < items.length; n++) {
    if (!seen.add(items[n].entity.mapID) || refs[n] != items[n].entity) {
      throw const FormatException('结果引用有重复或来源冲突');
    }
  }
  return List.unmodifiable(items);
}
