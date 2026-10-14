import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'human_causal_control.dart';
import 'public_moment_photo.dart';

class PublicMomentPhotoPending {
  const PublicMomentPhotoPending({
    required this.momentID,
    required this.operationID,
    required this.action,
    required this.photoID,
    required this.expectedMomentRevision,
    required this.expectedPhotoVersion,
    required this.inputSHA,
    required this.rightsSHA,
    required this.payloadDigest,
    required this.sourceVersion,
    required this.stage,
  });
  final String momentID,
      operationID,
      action,
      photoID,
      inputSHA,
      rightsSHA,
      payloadDigest,
      sourceVersion,
      stage;
  final int expectedMomentRevision, expectedPhotoVersion;
  factory PublicMomentPhotoPending.preview(PublicMomentPhotoPreview p) =>
      PublicMomentPhotoPending(
        momentID: p.input.momentID,
        operationID: p.input.operationID,
        action: p.input.action,
        photoID: p.input.photoID,
        expectedMomentRevision: p.input.expectedMomentRevision,
        expectedPhotoVersion: p.input.expectedPhotoVersion,
        inputSHA: p.inputSHA,
        rightsSHA: p.input.rights?.digest ?? '',
        payloadDigest: p.payloadDigest,
        sourceVersion: p.sourceVersion,
        stage: 'PREPARED',
      );
  PublicMomentPhotoPending get unknown => PublicMomentPhotoPending(
    momentID: momentID,
    operationID: operationID,
    action: action,
    photoID: photoID,
    expectedMomentRevision: expectedMomentRevision,
    expectedPhotoVersion: expectedPhotoVersion,
    inputSHA: inputSHA,
    rightsSHA: rightsSHA,
    payloadDigest: payloadDigest,
    sourceVersion: sourceVersion,
    stage: 'UNKNOWN',
  );
  Map<String, Object> toJson() => {
    'schema': 'public-moment-photo-pending-v1',
    'momentId': momentID,
    'operationId': operationID,
    'action': action,
    'photoId': photoID,
    'expectedMomentRevision': expectedMomentRevision,
    'expectedPhotoVersion': expectedPhotoVersion,
    'inputSha256': inputSHA,
    'rightsSha256': rightsSHA,
    'payloadDigest': payloadDigest,
    'sourceVersion': sourceVersion,
    'stage': stage,
  };
  bool same(PublicMomentPhotoPending p) =>
      momentID == p.momentID &&
      operationID == p.operationID &&
      action == p.action &&
      photoID == p.photoID &&
      expectedMomentRevision == p.expectedMomentRevision &&
      expectedPhotoVersion == p.expectedPhotoVersion &&
      inputSHA == p.inputSHA &&
      rightsSHA == p.rightsSHA &&
      payloadDigest == p.payloadDigest &&
      sourceVersion == p.sourceVersion;
  factory PublicMomentPhotoPending.decode(Object? raw) {
    final m = humanControlObject(raw, {
      'schema',
      'momentId',
      'operationId',
      'action',
      'photoId',
      'expectedMomentRevision',
      'expectedPhotoVersion',
      'inputSha256',
      'rightsSha256',
      'payloadDigest',
      'sourceVersion',
      'stage',
    });
    final save = m['action'] == 'SAVE';
    if (m['schema'] != 'public-moment-photo-pending-v1' ||
        !humanControlID(m['momentId']) ||
        !humanControlID(m['operationId']) ||
        !humanControlID(m['photoId']) ||
        !humanControlVersion(m['expectedMomentRevision'], zero: false) ||
        !humanControlVersion(m['expectedPhotoVersion']) ||
        !const {'SAVE', 'DELETE'}.contains(m['action']) ||
        !const {'PREPARED', 'UNKNOWN'}.contains(m['stage']) ||
        !humanControlHash(m['sourceVersion']) ||
        !humanControlHash(m['payloadDigest']) ||
        (save
            ? m['expectedMomentRevision'] < 2 ||
                  !humanControlHash(m['inputSha256']) ||
                  !humanControlHash(m['rightsSha256']) ||
                  m['expectedPhotoVersion'] >= publicMomentPhotoMaxVersion
            : m['inputSha256'] != '' ||
                  m['rightsSha256'] != '' ||
                  m['expectedPhotoVersion'] < 1) ||
        m['payloadDigest'] !=
            humanControlTupleDigest([
              'public-moment-photo-v1',
              m['momentId'],
              m['operationId'],
              m['action'],
              m['expectedMomentRevision'],
              m['photoId'],
              m['expectedPhotoVersion'],
              m['inputSha256'],
              m['rightsSha256'],
            ]))
      throw const FormatException('原公开图片选择器不符');
    return PublicMomentPhotoPending(
      momentID: m['momentId'],
      operationID: m['operationId'],
      action: m['action'],
      photoID: m['photoId'],
      expectedMomentRevision: m['expectedMomentRevision'],
      expectedPhotoVersion: m['expectedPhotoVersion'],
      inputSHA: m['inputSha256'],
      rightsSHA: m['rightsSha256'],
      payloadDigest: m['payloadDigest'],
      sourceVersion: m['sourceVersion'],
      stage: m['stage'],
    );
  }
  PublicMomentPhotoReceipt receipt(Object? raw) =>
      PublicMomentPhotoReceipt.decode(
        raw,
        operationID: operationID,
        momentID: momentID,
        photoID: photoID,
        action: action,
        expectedMomentRevision: expectedMomentRevision,
        expectedPhotoVersion: expectedPhotoVersion,
        digest: payloadDigest,
      );
}

abstract interface class PublicMomentPhotoPendingStore {
  Future<List<PublicMomentPhotoPending>> read(String origin, String owner);
  Future<void> write(String origin, String owner, PublicMomentPhotoPending p);
  Future<void> remove(String origin, String owner, PublicMomentPhotoPending p);
}

void _transition(PublicMomentPhotoPending? old, PublicMomentPhotoPending next) {
  PublicMomentPhotoPending.decode(next.toJson());
  if (old != null && !old.same(next)) throw StateError('原照片操作不能替换');
  if (next.stage == 'UNKNOWN' && old == null) throw StateError('原准备记录已退休');
  if (old?.stage == 'UNKNOWN' && next.stage != 'UNKNOWN')
    throw StateError('未知操作不能重放');
}

class SecurePublicMomentPhotoPendingStore
    implements PublicMomentPhotoPendingStore {
  const SecurePublicMomentPhotoPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static final _tails = <String, Future<void>>{};
  String _prefix(String origin, String owner) {
    if (origin.isEmpty || !humanControlID(owner))
      throw const FormatException('公开图片归属不符');
    return 'birdtie.public-moment-photo.v1.${sha256.convert(utf8.encode('$origin\n$owner'))}.';
  }

  Future<T> _locked<T>(String key, Future<T> Function() run) async {
    final old = _tails[key], done = Completer<void>();
    _tails[key] = done.future;
    try {
      if (old != null) await old;
      return await run();
    } finally {
      if (identical(_tails[key], done.future)) _tails.remove(key);
      done.complete();
    }
  }

  @override
  Future<List<PublicMomentPhotoPending>> read(String origin, String owner) =>
      _locked(_prefix(origin, owner), () async {
        final prefix = _prefix(origin, owner),
            all = await storage.readAll(),
            rows = <PublicMomentPhotoPending>[];
        for (final e in all.entries.where((e) => e.key.startsWith(prefix))) {
          final p = PublicMomentPhotoPending.decode(jsonDecode(e.value));
          if (e.key != '$prefix${p.operationID}')
            throw const FormatException('图片原操作键不符');
          rows.add(p);
        }
        return rows;
      });
  @override
  Future<void> write(String origin, String owner, PublicMomentPhotoPending p) =>
      _locked(_prefix(origin, owner), () async {
        final key = '${_prefix(origin, owner)}${p.operationID}',
            raw = await storage.read(key: key);
        _transition(
          raw == null ? null : PublicMomentPhotoPending.decode(jsonDecode(raw)),
          p,
        );
        await storage.write(key: key, value: jsonEncode(p.toJson()));
      });
  @override
  Future<void> remove(
    String origin,
    String owner,
    PublicMomentPhotoPending p,
  ) => _locked(_prefix(origin, owner), () async {
    final key = '${_prefix(origin, owner)}${p.operationID}',
        raw = await storage.read(key: key);
    if (raw == null) return;
    final old = PublicMomentPhotoPending.decode(jsonDecode(raw));
    if (!old.same(p) || old.stage != p.stage) throw StateError('本机照片操作已变化');
    await storage.delete(key: key);
  });
}

class MemoryPublicMomentPhotoPendingStore
    implements PublicMomentPhotoPendingStore {
  final values = <(String, String, String), PublicMomentPhotoPending>{};
  @override
  Future<List<PublicMomentPhotoPending>> read(
    String origin,
    String owner,
  ) async => values.entries
      .where((e) => e.key.$1 == origin && e.key.$2 == owner)
      .map((e) => e.value)
      .toList();
  @override
  Future<void> write(
    String origin,
    String owner,
    PublicMomentPhotoPending p,
  ) async {
    final key = (origin, owner, p.operationID);
    _transition(values[key], p);
    values[key] = p;
  }

  @override
  Future<void> remove(
    String origin,
    String owner,
    PublicMomentPhotoPending p,
  ) async {
    final key = (origin, owner, p.operationID), old = values[key];
    if (old == null) return;
    if (!old.same(p) || old.stage != p.stage) throw StateError('本机照片操作已变化');
    values.remove(key);
  }
}
