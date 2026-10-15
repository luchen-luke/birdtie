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
  });
  final String kind, operationID, targetID, digest, action, journalEntryID;
  final int momentRevision, setVersion;
  final List<Map<String, Object>> media;
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
  };
  factory PublicMomentMediaPending.decode(Object? raw) {
    final hasEntry = raw is Map<String, dynamic> && raw.containsKey('entryId');
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
    );
  }
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
    if (utf8.encode(raw).length > 65536)
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
        (v['operations'] as List).length > 40)
      throw const FormatException('原图片操作身份不符');
    final values = [
      for (final row in v['operations']) PublicMomentMediaPending.decode(row),
    ];
    if (values.map((p) => p.operationID).toSet().length != values.length)
      throw const FormatException('原图片操作重复');
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
    if (rows.length > 40) throw const FormatException('原图片操作记录过多');
    final raw = jsonEncode({
      'schema': 'moment-media-journal-v1',
      'origin': origin,
      'owner': owner,
      'operations': rows.map((p) => p.toJson()).toList(),
    });
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
    final rows = await _read(origin, owner);
    final original = rows
        .where((r) => r.operationID == pending.operationID)
        .firstOrNull;
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
}
