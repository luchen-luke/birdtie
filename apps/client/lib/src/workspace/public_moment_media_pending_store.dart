import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'human_causal_control.dart';

/// Selectors and original payload digest only. Never bytes, rights approval,
/// credentials, request JSON, or an executable replay command.
class PublicMomentMediaPending {
  const PublicMomentMediaPending({
    required this.kind,
    required this.operationID,
    required this.targetID,
    required this.digest,
    required this.momentRevision,
    required this.setVersion,
    required this.action,
    required this.media,
    this.journalEntryID = '',
    this.phase = 'UNKNOWN',
    this.materialOrder,
  });
  final String kind,
      operationID,
      targetID,
      digest,
      action,
      journalEntryID,
      phase;
  final int momentRevision, setVersion;
  final int? materialOrder;
  final List<Map<String, Object>> media;
  bool get staged => kind == 'STAGE' && phase == 'STAGED';
  Map<String, Object> toJson() => {
    'schema': 'moment-media-journal-v1',
    'kind': kind,
    'operationId': operationID,
    'targetId': targetID,
    'digest': digest,
    'momentRevision': momentRevision,
    'setVersion': setVersion,
    'action': action,
    'media': media,
    if (journalEntryID.isNotEmpty) 'entryId': journalEntryID,
    if (phase != 'UNKNOWN') 'phase': phase,
    if (materialOrder != null) 'materialOrder': materialOrder!,
  };
  factory PublicMomentMediaPending.decode(Object? raw) {
    final hasEntry = raw is Map<String, dynamic> && raw.containsKey('entryId');
    final hasPhase = raw is Map<String, dynamic> && raw.containsKey('phase');
    final hasOrder =
        raw is Map<String, dynamic> && raw.containsKey('materialOrder');
    final v = humanControlObject(raw, {
      'schema',
      'kind',
      'operationId',
      'targetId',
      'digest',
      'momentRevision',
      'setVersion',
      'action',
      'media',
      if (hasEntry) 'entryId',
      if (hasPhase) 'phase',
      if (hasOrder) 'materialOrder',
    });
    if (v['schema'] != 'moment-media-journal-v1' ||
        !const {'STAGE', 'SET'}.contains(v['kind']) ||
        !humanControlID(v['operationId']) ||
        !humanControlID(v['targetId']) ||
        !humanControlHash(v['digest']) ||
        !humanControlVersion(v['momentRevision']) ||
        !humanControlVersion(v['setVersion']) ||
        v['media'] is! List ||
        (v['media'] as List).length > 9 ||
        !const {'PREPARE', 'REPLACE', 'DELETE'}.contains(v['action']) ||
        hasPhase && !const {'UNKNOWN', 'STAGED'}.contains(v['phase']) ||
        hasOrder &&
            (v['kind'] != 'STAGE' ||
                v['materialOrder'] is! int ||
                v['materialOrder'] < 0 ||
                v['materialOrder'] > 8) ||
        v['phase'] == 'STAGED' && (v['kind'] != 'STAGE' || !hasOrder) ||
        hasEntry && (v['kind'] != 'STAGE' || !humanControlID(v['entryId'])) ||
        v['kind'] == 'STAGE' &&
            (v['action'] != 'PREPARE' ||
                v['momentRevision'] != 0 ||
                v['setVersion'] != 0 ||
                (v['media'] as List).isNotEmpty) ||
        v['kind'] == 'SET' &&
            (v['momentRevision'] < 2 ||
                !const {'REPLACE', 'DELETE'}.contains(v['action'])))
      throw const FormatException('原图片操作记录不符');
    final selectors = <Map<String, Object>>[];
    for (final raw in v['media']) {
      final item = humanControlObject(raw, {'mediaId', 'mediaVersion'});
      if (!humanControlID(item['mediaId']) ||
          !humanControlVersion(item['mediaVersion'], zero: false))
        throw const FormatException('原图片操作选择器不符');
      selectors.add(Map.unmodifiable(item.cast<String, Object>()));
    }
    return PublicMomentMediaPending(
      kind: v['kind'],
      operationID: v['operationId'],
      targetID: v['targetId'],
      digest: v['digest'],
      momentRevision: v['momentRevision'],
      setVersion: v['setVersion'],
      action: v['action'],
      media: List.unmodifiable(selectors),
      journalEntryID: hasEntry ? v['entryId'] : '',
      phase: hasPhase ? v['phase'] : 'UNKNOWN',
      materialOrder: hasOrder ? v['materialOrder'] : null,
    );
  }
  PublicMomentMediaPending asStaged(int order) => PublicMomentMediaPending(
    kind: kind,
    operationID: operationID,
    targetID: targetID,
    digest: digest,
    momentRevision: momentRevision,
    setVersion: setVersion,
    action: action,
    media: media,
    journalEntryID: journalEntryID,
    phase: 'STAGED',
    materialOrder: order,
  );
  bool sameFact(PublicMomentMediaPending other) =>
      kind == other.kind &&
      operationID == other.operationID &&
      targetID == other.targetID &&
      digest == other.digest &&
      momentRevision == other.momentRevision &&
      setVersion == other.setVersion &&
      action == other.action &&
      journalEntryID == other.journalEntryID &&
      jsonEncode(media) == jsonEncode(other.media);
  bool matches(PublicMomentMediaPending other) =>
      jsonEncode(toJson()) == jsonEncode(other.toJson());
}

abstract interface class PublicMomentMediaPendingStore {
  Future<List<PublicMomentMediaPending>> read(String origin, String owner);
  Future<void> write(
    String origin,
    String owner,
    PublicMomentMediaPending pending,
  );
  Future<void> remove(
    String origin,
    String owner,
    PublicMomentMediaPending pending,
  );
  Future<void> saveStagedSelection(
    String origin,
    String owner,
    String localScope,
    String entryID,
    List<PublicMomentMediaPending> expected,
    List<PublicMomentMediaPending> selection,
  );
}

/// One local group transition. Original operation facts remain immutable;
/// order and known-stage classification carry no server permission or rights.
List<PublicMomentMediaPending> replaceStagedMediaSelection(
  List<PublicMomentMediaPending> rows,
  String localScope,
  String entryID,
  List<PublicMomentMediaPending> expected,
  List<PublicMomentMediaPending> selection,
) {
  if (!humanControlID(localScope) ||
      entryID.isNotEmpty && !humanControlID(entryID) ||
      selection.length > 9) {
    throw const FormatException('本机材料分组不符');
  }
  bool group(PublicMomentMediaPending p) =>
      p.staged && p.targetID == localScope && p.journalEntryID == entryID;
  final current = rows.where(group).toList()
    ..sort((a, b) => a.materialOrder!.compareTo(b.materialOrder!));
  if (current.length != expected.length ||
      List.generate(
        current.length,
        (i) => i,
      ).any((i) => !current[i].matches(expected[i]))) {
    throw const FormatException('本机原材料顺序已经变化');
  }
  final ids = <String>{};
  for (var i = 0; i < selection.length; i++) {
    final next = PublicMomentMediaPending.decode(selection[i].toJson());
    final original = rows
        .where((p) => p.operationID == next.operationID)
        .firstOrNull;
    if (!next.staged ||
        next.targetID != localScope ||
        next.journalEntryID != entryID ||
        next.materialOrder != i ||
        !ids.add(next.operationID) ||
        original == null ||
        !original.sameFact(next)) {
      throw const FormatException('本机材料不能更换原操作事实');
    }
  }
  return [
    ...rows.where((p) => !group(p) && !ids.contains(p.operationID)),
    ...selection,
  ];
}

class SecurePublicMomentMediaPendingStore
    implements PublicMomentMediaPendingStore {
  const SecurePublicMomentMediaPendingStore({
    FlutterSecureStorage storage = const FlutterSecureStorage(),
  }) : _storage = storage;
  final FlutterSecureStorage _storage;
  static final _tails = <String, Future<void>>{};
  String _key(String origin, String owner) =>
      'birdtie.moment-media-journal.v1.${sha256.convert(utf8.encode(jsonEncode([origin, owner])))}';
  Future<T> _serial<T>(String key, Future<T> Function() run) async {
    final previous = _tails[key] ?? Future<void>.value(),
        done = Completer<void>();
    _tails[key] = done.future;
    try {
      await previous;
      return await run();
    } finally {
      done.complete();
      if (identical(_tails[key], done.future)) _tails.remove(key);
    }
  }

  Future<List<PublicMomentMediaPending>> _read(
    String origin,
    String owner,
  ) async {
    final raw = await _storage.read(key: _key(origin, owner));
    if (raw == null) return const [];
    if (utf8.encode(raw).length > 256 * 1024)
      throw const FormatException('原图片操作记录过大');
    final v = humanControlObject(jsonDecode(raw), {
      'schema',
      'origin',
      'owner',
      'operations',
    });
    if (v['schema'] != 'moment-media-journal-v1' ||
        v['origin'] != origin ||
        v['owner'] != owner ||
        v['operations'] is! List ||
        (v['operations'] as List).length > 256)
      throw const FormatException('原图片操作身份不符');
    final values = [
      for (final row in v['operations']) PublicMomentMediaPending.decode(row),
    ];
    if (values.map((p) => p.operationID).toSet().length != values.length)
      throw const FormatException('原图片操作重复');
    if (values.where((p) => !p.staged).length > 40) {
      throw const FormatException('待核实原图片操作过多');
    }
    final orders = <String, List<int>>{};
    for (final p in values.where((p) => p.staged)) {
      orders
          .putIfAbsent(jsonEncode([p.targetID, p.journalEntryID]), () => [])
          .add(p.materialOrder!);
    }
    for (final group in orders.values) {
      group.sort();
      if (List.generate(group.length, (i) => i).any((i) => group[i] != i)) {
        throw const FormatException('原材料顺序重复或不完整');
      }
    }
    return values;
  }

  @override
  Future<List<PublicMomentMediaPending>> read(String origin, String owner) =>
      _serial(_key(origin, owner), () => _read(origin, owner));
  Future<void> _save(
    String origin,
    String owner,
    List<PublicMomentMediaPending> rows,
  ) async {
    if (rows.length > 256 || rows.where((p) => !p.staged).length > 40) {
      throw const FormatException('原图片操作或材料记录过多');
    }
    final raw = jsonEncode({
      'schema': 'moment-media-journal-v1',
      'origin': origin,
      'owner': owner,
      'operations': rows.map((p) => p.toJson()).toList(),
    });
    if (utf8.encode(raw).length > 256 * 1024) {
      throw const FormatException('原图片操作记录过大');
    }
    await _storage.write(key: _key(origin, owner), value: raw);
    if (await _storage.read(key: _key(origin, owner)) != raw)
      throw StateError('原图片操作未确认保存');
  }

  @override
  Future<void> write(
    String origin,
    String owner,
    PublicMomentMediaPending pending,
  ) => _serial(_key(origin, owner), () async {
    PublicMomentMediaPending.decode(pending.toJson());
    if (pending.staged) throw const FormatException('已暂存材料必须按原分组保存');
    final rows = await _read(origin, owner);
    final original = rows
        .where((r) => r.operationID == pending.operationID)
        .firstOrNull;
    if (original?.staged == true) throw const FormatException('已暂存材料必须按原分组移除');
    if (original != null && !original.matches(pending))
      throw const FormatException('原图片操作不可替换');
    await _save(origin, owner, [
      ...rows.where((r) => r.operationID != pending.operationID),
      pending,
    ]);
  });
  @override
  Future<void> remove(
    String origin,
    String owner,
    PublicMomentMediaPending pending,
  ) => _serial(_key(origin, owner), () async {
    final rows = await _read(origin, owner);
    if (pending.staged) throw const FormatException('已暂存材料必须按原分组移除');
    final original = rows
        .where((r) => r.operationID == pending.operationID)
        .firstOrNull;
    if (original != null && !original.matches(pending))
      throw const FormatException('原图片操作不可替换');
    await _save(
      origin,
      owner,
      rows.where((r) => r.operationID != pending.operationID).toList(),
    );
  });
  @override
  Future<void> saveStagedSelection(
    String origin,
    String owner,
    String localScope,
    String entryID,
    List<PublicMomentMediaPending> expected,
    List<PublicMomentMediaPending> selection,
  ) => _serial(_key(origin, owner), () async {
    final rows = await _read(origin, owner);
    await _save(
      origin,
      owner,
      replaceStagedMediaSelection(
        rows,
        localScope,
        entryID,
        expected,
        selection,
      ),
    );
  });
}
