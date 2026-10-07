import 'dart:convert';
import 'dart:math';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'social_intent_creation_api.dart';
import 'social_intent_draft_model.dart';

String newIntentCreationOperation() {
  final random = Random.secure(),
      bytes = List.generate(16, (_) => random.nextInt(256));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  final h = bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-${h.substring(16, 20)}-${h.substring(20)}';
}

Object? _immutable(Object? value) => value is Map
    ? Map<String, dynamic>.unmodifiable({
        for (final entry in value.entries)
          entry.key as String: _immutable(entry.value),
      })
    : value is List
    ? List.unmodifiable(value.map(_immutable))
    : value;

class PendingSocialIntentCreation {
  PendingSocialIntentCreation(Map<String, dynamic> data)
    : data = _immutable(jsonDecode(jsonEncode(data))) as Map<String, dynamic> {
    final d = this.data;
    if (d.keys.toSet().difference({
          'schemaVersion',
          'environmentHash',
          'ownerAccountId',
          'operationId',
          'sourceTaskId',
          'requestDigest',
          'draft',
          'createdAt',
        }).isNotEmpty ||
        d['schemaVersion'] != 'social-intent-creation-pending-v1' ||
        !socialIntentIDValid(d['ownerAccountId']) ||
        !socialIntentIDValid(d['operationId']) ||
        d['sourceTaskId'] is! String ||
        (d['sourceTaskId'] != '' && !socialIntentIDValid(d['sourceTaskId'])) ||
        d['environmentHash'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(d['environmentHash'] as String) ||
        d['draft'] is! Map<String, dynamic> ||
        DateTime.tryParse(d['createdAt'] as String? ?? '') == null) {
      throw const FormatException('本机原保存记录不符');
    }
    if (intentCreationDigest(owner, source, draft) != digest) {
      throw const FormatException('本机原保存内容已变化');
    }
  }
  factory PendingSocialIntentCreation.capture({
    required String environment,
    required String owner,
    required String source,
    required Map<String, dynamic> draft,
    String? operation,
  }) => PendingSocialIntentCreation({
    'schemaVersion': 'social-intent-creation-pending-v1',
    'environmentHash': intentCreationHash(
      intentCreationEnvironment(environment),
    ),
    'ownerAccountId': owner,
    'operationId': operation ?? newIntentCreationOperation(),
    'sourceTaskId': source,
    'requestDigest': intentCreationDigest(owner, source, draft),
    'draft': intentCreationDraft(draft, source),
    'createdAt': DateTime.now().toUtc().toIso8601String(),
  });
  final Map<String, dynamic> data;
  String get owner => data['ownerAccountId'] as String;
  String get operation => data['operationId'] as String;
  String get source => data['sourceTaskId'] as String;
  String get digest => data['requestDigest'] as String;
  Map<String, dynamic> get draft =>
      Map<String, dynamic>.from(jsonDecode(jsonEncode(data['draft'])) as Map);
  String get encoded => jsonEncode(data);
  bool same(PendingSocialIntentCreation other) => encoded == other.encoded;
  void bound(String environment, String owner) {
    if (data['environmentHash'] !=
            intentCreationHash(intentCreationEnvironment(environment)) ||
        this.owner != owner) {
      throw const FormatException('本机原保存归属不符');
    }
  }
}

abstract interface class SocialIntentCreationPendingStore {
  Future<PendingSocialIntentCreation?> read(String environment, String owner);
  Future<void> write(
    String environment,
    String owner,
    PendingSocialIntentCreation value,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingSocialIntentCreation captured,
  );
}

String _key(String environment, String owner) {
  if (!socialIntentIDValid(owner)) throw const FormatException('本人账号无法核实');
  return 'birdtie.social.intent.creation.v1.${intentCreationHash('${intentCreationEnvironment(environment)}\n$owner')}';
}

class SecureSocialIntentCreationPendingStore
    implements SocialIntentCreationPendingStore {
  const SecureSocialIntentCreationPendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  static Future<void> _tail = Future.value();
  Future<T> _serial<T>(Future<T> Function() action) {
    final r = _tail.then((_) => action());
    _tail = r.then<void>((_) {}, onError: (Object _, StackTrace _) {});
    return r;
  }

  @override
  Future<PendingSocialIntentCreation?> read(String environment, String owner) =>
      _serial(() async {
        final raw = await storage.read(key: _key(environment, owner));
        if (raw == null) return null;
        final p = PendingSocialIntentCreation(
          Map<String, dynamic>.from(jsonDecode(raw) as Map),
        );
        p.bound(environment, owner);
        return p;
      });
  @override
  Future<void> write(
    String environment,
    String owner,
    PendingSocialIntentCreation value,
  ) => _serial(() async {
    value.bound(environment, owner);
    final key = _key(environment, owner), raw = await storage.read(key: key);
    if (raw != null) {
      final old = PendingSocialIntentCreation(
        Map<String, dynamic>.from(jsonDecode(raw) as Map),
      );
      old.bound(environment, owner);
      if (!old.same(value)) throw StateError('请先核实原保存操作');
    }
    await storage.write(key: key, value: value.encoded);
    if (await storage.read(key: key) != value.encoded) {
      throw StateError('本机核实记录未安全保存');
    }
  });
  @override
  Future<void> delete(
    String environment,
    String owner,
    PendingSocialIntentCreation captured,
  ) => _serial(() async {
    captured.bound(environment, owner);
    final key = _key(environment, owner), raw = await storage.read(key: key);
    if (raw == null) return;
    if (raw != captured.encoded) throw StateError('原核实记录已改变');
    await storage.delete(key: key);
    if (await storage.read(key: key) != null) throw StateError('本机核实记录未安全删除');
  });
}

// Test injection only. The product default remains secure platform storage;
// there is no silent plaintext, process-only or anonymous persistence fallback.
class MemorySocialIntentCreationPendingStore
    implements SocialIntentCreationPendingStore {
  final Map<String, String> values = {};
  @override
  Future<PendingSocialIntentCreation?> read(String e, String o) async {
    final raw = values[_key(e, o)];
    if (raw == null) return null;
    final p = PendingSocialIntentCreation(
      Map<String, dynamic>.from(jsonDecode(raw) as Map),
    );
    p.bound(e, o);
    return p;
  }

  @override
  Future<void> write(String e, String o, PendingSocialIntentCreation p) async {
    p.bound(e, o);
    final key = _key(e, o), old = values[key];
    if (old != null && old != p.encoded) throw StateError('请先核实原保存操作');
    values[key] = p.encoded;
  }

  @override
  Future<void> delete(String e, String o, PendingSocialIntentCreation p) async {
    p.bound(e, o);
    final key = _key(e, o), old = values[key];
    if (old != null && old != p.encoded) throw StateError('原核实记录已改变');
    values.remove(key);
  }
}
