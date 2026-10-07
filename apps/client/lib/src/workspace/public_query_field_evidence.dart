import 'dart:convert';

import 'agent_result_projection.dart';

const publicQueryFieldEvidenceSchema = 'public-query-field-evidence-v1';
const _reasonKeys = {
  'EXPIRED_SOURCE',
  'OMITTED_BUDGET',
  'SOURCE_METADATA_UNAVAILABLE',
  'UNKNOWN_SOURCE_TIME',
};
void _require(bool value) {
  if (!value) throw const FormatException('公开来源说明无法读取');
}

Map<String, dynamic> _map(
  Object? raw,
  Set<String> required, [
  Set<String> optional = const {},
]) {
  _require(raw is Map<String, dynamic>);
  final m = raw as Map<String, dynamic>;
  _require(
    required.every(m.containsKey) &&
        m.keys.every((k) => required.contains(k) || optional.contains(k)),
  );
  return m;
}

DateTime _time(Object? raw) {
  _require(
    raw is String &&
        RegExp(
          r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$',
        ).hasMatch(raw),
  );
  final value = DateTime.tryParse(raw as String);
  _require(value != null && value.year > 1);
  _require(value!.toIso8601String().substring(0, 19) == raw.substring(0, 19));
  return value;
}

BigInt _nanos(Object? raw) {
  _time(raw);
  final match = RegExp(r'^(.*?)(?:\.(\d{1,9}))?Z$').firstMatch(raw as String)!;
  return BigInt.from(DateTime.parse('${match[1]}Z').microsecondsSinceEpoch) *
          BigInt.from(1000) +
      BigInt.parse((match[2] ?? '').padRight(9, '0'));
}

bool _id(Object? raw) =>
    raw is String &&
    RegExp(
      r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
    ).hasMatch(raw) &&
    raw != '00000000-0000-0000-0000-000000000000';

class PublicQueryFieldEvidenceTimes {
  const PublicQueryFieldEvidenceTimes(this.updatedAt, this.sourceExpiresAt);
  final DateTime updatedAt;
  final DateTime? sourceExpiresAt;
}

/// Read-only descriptions from one original response. Not an action descriptor,
/// approval, CAS version, source-reading grant, model context or truth score.
class PublicQueryFieldEvidence {
  PublicQueryFieldEvidence._({
    required this.taskID,
    required this.resultSetID,
    required this.queryKind,
    required this.observedAt,
    required this.readExpiresAt,
    required this.status,
    required this.omitted,
    required this.times,
    required this.selectedRefs,
    required this._remaining,
    required this._now,
    required this._current,
    this.invalid = false,
  });
  final String taskID, resultSetID, queryKind, status;
  final DateTime? observedAt, readExpiresAt;
  final Map<String, int> omitted;
  final Map<String, PublicQueryFieldEvidenceTimes> times;
  final Set<String> selectedRefs;
  final bool invalid;
  final Duration _remaining;
  final DateTime Function() _now;
  final bool Function() _current;
  final Stopwatch _watch = Stopwatch()..start();
  bool _retired = false;
  void retire() => _retired = true;
  bool get current {
    if (_retired ||
        !_current() ||
        !invalid &&
            (_watch.elapsed >= _remaining ||
                readExpiresAt?.isAfter(_now().toUtc()) != true)) {
      _retired = true;
      return false;
    }
    return true;
  }

  Duration get remaining {
    if (!current || invalid) return Duration.zero;
    final utc = readExpiresAt!.difference(_now().toUtc());
    final monotonic = _remaining - _watch.elapsed;
    return utc < monotonic ? utc : monotonic;
  }

  bool hasItem(AgentResultItem item) =>
      selectedRefs.contains(item.entity.mapID) && item.entity.type == queryKind;

  factory PublicQueryFieldEvidence.unavailable({
    required String taskID,
    required String resultSetID,
    required String queryKind,
    required Set<String> selectedRefs,
    required bool Function() current,
  }) => PublicQueryFieldEvidence._(
    taskID: taskID,
    resultSetID: resultSetID,
    queryKind: queryKind,
    status: 'UNAVAILABLE',
    observedAt: null,
    readExpiresAt: null,
    omitted: const {},
    times: const {},
    selectedRefs: Set.unmodifiable(selectedRefs),
    remaining: Duration.zero,
    now: () => DateTime.now().toUtc(),
    current: current,
    invalid: true,
  );

  factory PublicQueryFieldEvidence.read(
    Map<String, dynamic> data, {
    required String expectedOwner,
    required DateTime Function() now,
    required bool Function() current,
    Duration waited = Duration.zero,
  }) {
    _require(_id(expectedOwner) && current() && waited >= Duration.zero);
    final rawSet = data['resultSet'];
    _require(rawSet is Map<String, dynamic>);
    final set = rawSet as Map<String, dynamic>;
    final task = data['task'];
    _require(task is Map<String, dynamic>);
    final t = task as Map<String, dynamic>;
    final taskID = t['id'];
    _require(
      _id(taskID) &&
          data['principalType'] == 'PERSON' &&
          data['principalId'] == expectedOwner &&
          data['workspace'] == 'PERSONAL' &&
          (t['principalType'] as String?)?.toUpperCase() == 'PERSON' &&
          t['principalId'] == expectedOwner &&
          t['actingUserId'] == expectedOwner &&
          t['contextType'] != 'ONLINE' &&
          {'ACTIVE', 'COMPLETED'}.contains(t['status']) &&
          data['taskId'] == taskID &&
          set['taskId'] == taskID &&
          set['schema'] == typedAgentResultSchema &&
          {'ready', 'empty'}.contains(set['status']) &&
          _nanos(t['updatedAt']) == _nanos(set['generatedAt']) &&
          set['id'] == '$taskID:${_nanos(t['updatedAt'])}',
    );
    final kind = switch (t['intent']) {
      'FIND_ACTIVITY' ||
      'AREA_DISCOVERY' ||
      'REFINE_RESULTS' ||
      'COMPARE_RESULTS' => 'activity',
      'FIND_PLACE' => 'place',
      _ => '',
    };
    _require(kind.isNotEmpty);
    final items = decodeAgentResultItems(set);
    final refs = {for (final i in items) i.entity.mapID};
    final p = _map(set['publicFieldEvidence'], {
      'schemaVersion',
      'taskId',
      'queryKind',
      'owner',
      'observedAt',
      'validUntil',
      'status',
      'fieldEvidenceSet',
      'budget',
      'modelAccess',
      'mediaAccess',
      'grantsAuthority',
    });
    final owner = _map(p['owner'], {'type', 'id'});
    _require(
      p['schemaVersion'] == publicQueryFieldEvidenceSchema &&
          p['taskId'] == taskID &&
          p['queryKind'] == kind &&
          owner['type'] == 'PERSON' &&
          owner['id'] == expectedOwner &&
          p['modelAccess'] == 'UNAVAILABLE' &&
          p['mediaAccess'] == 'UNAVAILABLE' &&
          p['grantsAuthority'] == false,
    );
    final observed = _time(p['observedAt']),
        expires = _time(p['validUntil']),
        at = now().toUtc();
    final nativeLease = expires.difference(observed);
    final utcRemaining = expires.difference(at);
    final nativeRemaining = nativeLease - waited;
    _require(
      !observed.isAfter(at) &&
          nativeLease > Duration.zero &&
          nativeLease <= const Duration(minutes: 2) &&
          utcRemaining > Duration.zero &&
          nativeRemaining > Duration.zero,
    );
    final budget = _map(p['budget'], {'unit', 'limit', 'used', 'omitted'});
    final omitted = _map(budget['omitted'], _reasonKeys);
    _require(
      budget['unit'] == 'UTF8_JSON_BYTES_V1' &&
          budget['limit'] == 16384 &&
          budget['used'] is int &&
          utf8.encode(jsonEncode(p)).length == budget['used'] &&
          (budget['used'] as int) <= 16384 &&
          omitted.values.every((n) => n is int && n >= 0),
    );
    final counts = {for (final k in _reasonKeys) k: omitted[k] as int};
    final omittedTotal = counts.values.fold(0, (a, b) => a + b);
    _require(omittedTotal <= items.length);
    final fields = _map(p['fieldEvidenceSet'], {
      'schemaVersion',
      'owner',
      'observedAt',
      'expiresAt',
      'scope',
      'claims',
      'conflicts',
      'modelAccess',
      'mediaAccess',
      'grantsAuthority',
    });
    final fieldOwner = _map(fields['owner'], {'type', 'id'});
    _require(
      fields['schemaVersion'] == 'agent-field-evidence-v1' &&
          fieldOwner['type'] == 'PERSON' &&
          fieldOwner['id'] == expectedOwner &&
          _nanos(fields['observedAt']) == _nanos(p['observedAt']) &&
          _nanos(fields['expiresAt']) == _nanos(p['validUntil']) &&
          fields['scope'] ==
              (counts['OMITTED_BUDGET']! > 0
                  ? 'BUDGETED_CONTEXT'
                  : 'FILTERED_CONTEXT') &&
          fields['modelAccess'] == 'UNAVAILABLE' &&
          fields['mediaAccess'] == 'UNAVAILABLE' &&
          fields['grantsAuthority'] == false &&
          fields['claims'] is List &&
          (fields['claims'] as List).length <= 100 &&
          fields['conflicts'] is List &&
          (fields['conflicts'] as List).isEmpty,
    );
    final grouped = <String, Set<String>>{},
        times = <String, PublicQueryFieldEvidenceTimes>{};
    final versions = <String, String>{};
    final closedFields = kind == 'activity'
        ? {
            'activity.title',
            'activity.category',
            'activity.startsAt',
            'activity.endsAt',
            'activity.organizerType',
            'activity.organizerId',
          }
        : {'place.name', 'place.category'};
    final sourceKind = kind == 'activity' ? 'PUBLIC_ACTIVITY' : 'PUBLIC_PLACE';
    final rows = data[kind == 'activity' ? 'activities' : 'places'];
    _require(rows == null || rows is List);
    for (final raw in fields['claims'] as List) {
      final c = _map(
        raw,
        {
          'id',
          'itemKind',
          'itemId',
          'subjectKind',
          'subjectId',
          'field',
          'claimantKind',
          'source',
          'nature',
          'use',
          'observedAt',
          'collectedAt',
          'collectionEvent',
          'sourceUpdatedAt',
          'captureTimeStatus',
          'validityStatus',
        },
        {'validUntil'},
      );
      final id = c['itemId'], field = c['field'];
      _require(
        _id(id) &&
            refs.contains('$kind:$id') &&
            closedFields.contains(field) &&
            c['id'] == '$sourceKind:$id:$field' &&
            c['itemKind'] == (kind == 'activity' ? 'activities' : 'places') &&
            c['subjectKind'] == kind.toUpperCase() &&
            c['subjectId'] == id &&
            c['claimantKind'] == 'NATIVE_DOMAIN_RECORD' &&
            c['nature'] == 'NATIVE_RECORD' &&
            c['use'] ==
                (kind == 'activity'
                    ? 'SCHEDULE_OR_RECORD_NOT_ATTENDANCE'
                    : 'CURRENT_DOMAIN_RECORD_ONLY') &&
            c['collectionEvent'] == 'CURRENT_NATIVE_READ' &&
            c['captureTimeStatus'] == 'UNKNOWN_NOT_COLLECTED' &&
            _nanos(c['observedAt']) == _nanos(p['observedAt']) &&
            _nanos(c['collectedAt']) == _nanos(p['observedAt']),
      );
      final source = _map(c['source'], {'kind', 'id', 'version', 'nativeTime'});
      final version = _map(source['version'], {'kind', 'token'});
      _require(
        source['kind'] == sourceKind &&
            source['id'] == id &&
            version['kind'] == 'UPDATED_AT_DIGEST' &&
            version['token'] is String &&
            RegExp(r'^[0-9a-f]{64}$').hasMatch(version['token'] as String) &&
            _nanos(source['nativeTime']) == _nanos(c['sourceUpdatedAt']) &&
            !_time(c['sourceUpdatedAt']).isAfter(observed),
      );
      final matching = (rows as List? ?? const [])
          .where((r) => r is Map<String, dynamic> && r['id'] == id)
          .toList();
      _require(matching.length == 1);
      final row = matching.single as Map<String, dynamic>;
      _require(kind != 'activity' || row['visibility'] == 'public');
      final nativeSource = row['source'];
      _require(
        nativeSource is Map<String, dynamic> &&
            _nanos(nativeSource['updatedAt']) == _nanos(c['sourceUpdatedAt']),
      );
      DateTime? sourceExpires;
      if (c.containsKey('validUntil')) {
        sourceExpires = _time(c['validUntil']);
        _require(
          sourceExpires.isAfter(observed) &&
              c['validityStatus'] == 'VALID_UNTIL_KNOWN' &&
              _nanos(c['validUntil']) ==
                  _nanos((nativeSource as Map)['expiresAt']),
        );
      } else {
        _require(
          c['validityStatus'] == 'SOURCE_INTERVAL_UNKNOWN' &&
              (nativeSource as Map)['expiresAt'] == null,
        );
      }
      final group = grouped.putIfAbsent(id as String, () => <String>{});
      _require(group.add(field as String));
      _require(versions[id] == null || versions[id] == version['token']);
      versions[id] = version['token'] as String;
      final old = times[id];
      _require(
        old == null ||
            old.updatedAt == _time(c['sourceUpdatedAt']) &&
                old.sourceExpiresAt == sourceExpires,
      );
      times[id] = PublicQueryFieldEvidenceTimes(
        _time(c['sourceUpdatedAt']),
        sourceExpires,
      );
    }
    _require(
      grouped.values.every(
        (g) => g.length == closedFields.length && g.containsAll(closedFields),
      ),
    );
    final claims = (fields['claims'] as List).length;
    _require(switch (p['status']) {
      'AVAILABLE' => claims > 0 && omittedTotal == 0,
      'PARTIAL' => claims > 0 && omittedTotal > 0,
      'UNAVAILABLE' => claims == 0 && omittedTotal > 0,
      'NO_PUBLIC_FIELDS' => claims == 0 && omittedTotal == 0,
      _ => false,
    });
    return PublicQueryFieldEvidence._(
      taskID: taskID as String,
      resultSetID: set['id'] as String,
      queryKind: kind,
      observedAt: observed,
      readExpiresAt: expires,
      status: p['status'] as String,
      omitted: Map.unmodifiable(counts),
      times: Map.unmodifiable(times),
      selectedRefs: Set.unmodifiable(refs),
      remaining: nativeRemaining < utcRemaining
          ? nativeRemaining
          : utcRemaining,
      now: now,
      current: current,
    );
  }
}
