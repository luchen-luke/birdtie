import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'entity_share_pending_store.dart' show newEntityShareOperationID;

/// Editable local material, never an authorization or an executable command.
/// Restored references and attachment selectors always need current readers.
class LocalDraftReference {
  const LocalDraftReference({
    required this.kind,
    required this.id,
    this.mode = 'live',
    this.version,
    this.snapshotText = '',
    this.purpose = 'context',
  });
  final String kind, id, mode, snapshotText, purpose;
  final String? version;
  static const kinds = {
    'person',
    'place',
    'activity',
    'organization',
    'business',
    'community',
    'journey',
    'theme_map',
    'moment',
  };
  Map<String, dynamic> toJson() => {
    'kind': kind,
    'id': id,
    'mode': mode,
    'version': version,
    'snapshotText': snapshotText,
    if (purpose != 'context') 'purpose': purpose,
  };
  factory LocalDraftReference.fromJson(Map<String, dynamic> j) {
    _keys(j, {
      'kind',
      'id',
      'mode',
      'version',
      'snapshotText',
      if (j.containsKey('purpose')) 'purpose',
    });
    if (!kinds.contains(j['kind']) ||
        !const {'live', 'snapshot'}.contains(j['mode']) ||
        !const {'context', 'route_stop'}.contains(j['purpose'] ?? 'context') ||
        j['purpose'] == 'route_stop' && j['kind'] != 'place')
      throw const FormatException('引用类型无效');
    final id = _text(j['id'], 160, empty: false),
        snapshot = _text(j['snapshotText'], 8000);
    final version = j['version'] == null
        ? null
        : _text(j['version'], 160, empty: false);
    if (j['mode'] == 'live' && snapshot.isNotEmpty)
      throw const FormatException('动态引用不能持久化旧正文');
    return LocalDraftReference(
      kind: j['kind'] as String,
      id: id,
      mode: j['mode'] as String,
      version: version,
      snapshotText: snapshot,
      purpose: j['purpose'] as String? ?? 'context',
    );
  }
}

class LocalDraftAttachment {
  const LocalDraftAttachment({
    required this.id,
    required this.kind,
    required this.state,
  });
  final String id, kind, state;
  Map<String, dynamic> toJson() => {'id': id, 'kind': kind, 'state': state};
  factory LocalDraftAttachment.fromJson(Map<String, dynamic> j) {
    _keys(j, {'id', 'kind', 'state'});
    if (!const {'image', 'video', 'audio', 'file'}.contains(j['kind']) ||
        !const {
          'local',
          'uploaded',
          'failed',
          'unknown',
          'unavailable',
        }.contains(j['state']))
      throw const FormatException('附件状态无效');
    // Uploaded is historical status, not a reusable server receipt.
    return LocalDraftAttachment(
      id: _text(j['id'], 160, empty: false),
      kind: j['kind'] as String,
      state: j['state'] as String,
    );
  }
}

class TypedLocalDraft {
  TypedLocalDraft({
    required this.owner,
    required this.origin,
    required this.kind,
    required this.text,
    this.workspaceID,
    this.conversationID,
    this.recipientID,
    this.taskID,
    this.slotID = 'primary',
    String? id,
    this.revision = 1,
    this.audience = 'private',
    List<LocalDraftReference> references = const [],
    List<LocalDraftAttachment> attachments = const [],
  }) : id = id ?? newEntityShareOperationID(),
       references = List.unmodifiable(references),
       attachments = List.unmodifiable(attachments);
  final String id, owner, origin, kind, text, audience;
  final String? workspaceID, conversationID, recipientID, taskID;
  final String slotID;
  final int revision;
  final List<LocalDraftReference> references;
  final List<LocalDraftAttachment> attachments;
  static const kinds = {
    'intent',
    'comment',
    'reply',
    'message',
    'moment',
    'activity',
    'journey',
    'theme_map',
    'route',
    'external_plan',
  };
  String get scopeKey => jsonEncode([
    workspaceID,
    conversationID,
    recipientID,
    const {'route', 'external_plan'}.contains(kind) ? 'route-plan' : kind,
    slotID,
  ]);
  String get contentVersion =>
      sha256.convert(utf8.encode(jsonEncode(toJson()))).toString();
  bool matches(String currentOrigin, String currentOwner, String? workspace) =>
      origin == currentOrigin &&
      owner == currentOwner &&
      workspaceID == workspace;
  TypedLocalDraft edit({
    String? text,
    String? kind,
    String? audience,
    String? taskID,
    List<LocalDraftReference>? references,
    List<LocalDraftAttachment>? attachments,
  }) => TypedLocalDraft(
    id: id,
    owner: owner,
    origin: origin,
    workspaceID: workspaceID,
    conversationID: conversationID,
    recipientID: recipientID,
    kind: kind ?? this.kind,
    text: text ?? this.text,
    audience: audience ?? this.audience,
    taskID: taskID ?? this.taskID,
    slotID: slotID,
    revision: revision + 1,
    references: references ?? this.references,
    attachments: attachments ?? this.attachments,
  );

  /// Conversion produces another editable view. It performs no domain write.
  /// A sending target is never inherited by a private Moment/collection.
  TypedLocalDraft converted(String next) {
    if (!kinds.contains(next) ||
        const {'message', 'comment', 'reply'}.contains(next))
      throw const FormatException('请先明确实际收件人');
    return TypedLocalDraft(
      id: id,
      owner: owner,
      origin: origin,
      workspaceID: workspaceID,
      kind: next,
      text: text,
      taskID: taskID,
      slotID: const {'route', 'external_plan'}.contains(next)
          ? slotID
          : 'primary',
      revision: revision + 1,
      references: references,
      attachments: attachments,
    );
  }

  Map<String, dynamic> toJson() => {
    'schemaVersion': 'birdtie-local-draft-v1',
    'id': id,
    'owner': owner,
    'origin': origin,
    'workspaceId': workspaceID,
    'kind': kind,
    'text': text,
    'revision': revision,
    'conversationId': conversationID,
    'recipientId': recipientID,
    'audience': audience,
    'taskId': taskID,
    'slotId': slotID,
    'references': references.map((r) => r.toJson()).toList(),
    'attachments': attachments.map((a) => a.toJson()).toList(),
  };
  factory TypedLocalDraft.fromJson(Map<String, dynamic> j) {
    _keys(j, {
      'schemaVersion',
      'id',
      'owner',
      'origin',
      'workspaceId',
      'kind',
      'text',
      'revision',
      'conversationId',
      'recipientId',
      'audience',
      'taskId',
      if (j.containsKey('slotId')) 'slotId',
      'references',
      'attachments',
    });
    if (j['schemaVersion'] != 'birdtie-local-draft-v1' ||
        !kinds.contains(j['kind']) ||
        !const {'private', 'recipient'}.contains(j['audience']) ||
        j['revision'] is! int ||
        j['revision'] < 1 ||
        j['revision'] > 8999999999999999 ||
        j['references'] is! List ||
        j['attachments'] is! List ||
        (j['references'] as List).length > 50 ||
        (j['attachments'] as List).length > 10)
      throw const FormatException('草稿结构无效');
    String? selector(String key) =>
        j[key] == null ? null : _text(j[key], 160, empty: false);
    final conversation = selector('conversationId'),
        recipient = selector('recipientId');
    final message = j['kind'] == 'message';
    if (message != (conversation != null && recipient != null) ||
        !message && (conversation != null || recipient != null) ||
        message != (j['audience'] == 'recipient'))
      throw const FormatException('收件人与引用不可混用');
    final value = TypedLocalDraft(
      id: _text(j['id'], 160, empty: false),
      owner: _text(j['owner'], 160, empty: false),
      origin: _text(j['origin'], 2048, empty: false),
      workspaceID: selector('workspaceId'),
      kind: j['kind'] as String,
      text: _text(j['text'], 16384),
      revision: j['revision'] as int,
      conversationID: conversation,
      recipientID: recipient,
      audience: j['audience'] as String,
      taskID: selector('taskId'),
      slotID: j.containsKey('slotId')
          ? _text(j['slotId'], 160, empty: false)
          : 'primary',
      references: (j['references'] as List)
          .map((r) => LocalDraftReference.fromJson(r as Map<String, dynamic>))
          .toList(),
      attachments: (j['attachments'] as List)
          .map((a) => LocalDraftAttachment.fromJson(a as Map<String, dynamic>))
          .toList(),
    );
    if (message && value.workspaceID != null)
      throw const FormatException('本人聊天草稿不能借组织身份');
    if (value.attachments.map((a) => a.id).toSet().length !=
        value.attachments.length)
      throw const FormatException('附件记录不能重复');
    if (!const {'route', 'external_plan'}.contains(value.kind) &&
        value.slotID != 'primary' &&
        !(const {
              'comment',
              'reply',
              'moment',
              'journey',
            }.contains(value.kind) &&
            value.references.isEmpty &&
            value.attachments.isEmpty &&
            RegExp(
              r'^now-content:(comment|edit-moment|edit-journey):[a-f0-9-]{36}:([a-f0-9-]{36})?$',
            ).hasMatch(value.slotID)) &&
        !(const {
              'moment',
              'activity',
              'journey',
              'theme_map',
            }.contains(value.kind) &&
            value.workspaceID == null &&
            value.taskID == null &&
            value.references.isEmpty &&
            value.attachments.isEmpty &&
            RegExp(r'^now-creation:[a-f0-9]{64}$').hasMatch(value.slotID)))
      throw const FormatException('草稿入口标识不符');
    return value;
  }
}

void _keys(Map<String, dynamic> j, Set<String> keys) {
  if (j.length != keys.length || j.keys.toSet().difference(keys).isNotEmpty)
    throw const FormatException('草稿含未知字段');
}

String _text(Object? value, int max, {bool empty = true}) {
  if (value is! String ||
      !empty && value.isEmpty ||
      utf8.encode(value).length > max ||
      value.contains('\u0000'))
    throw const FormatException('草稿字段无效');
  return value;
}

abstract class TypedLocalDraftStore {
  Future<List<TypedLocalDraft>> read(String origin, String owner);
  Future<void> save(String origin, String owner, List<TypedLocalDraft> drafts);
  Future<void> clear(String origin, String owner);
}

class SecureTypedLocalDraftStore implements TypedLocalDraftStore {
  SecureTypedLocalDraftStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  static final _tails = <String, Future<void>>{};
  String _key(String origin, String owner) =>
      'birdtie.typed-local-drafts.v1.${sha256.convert(utf8.encode(jsonEncode([origin, owner])))}';
  Future<T> _serial<T>(String key, Future<T> Function() action) async {
    final previous = _tails[key] ?? Future<void>.value(),
        done = Completer<void>();
    _tails[key] = done.future;
    try {
      await previous;
      return await action();
    } finally {
      done.complete();
      if (identical(_tails[key], done.future)) _tails.remove(key);
    }
  }

  @override
  Future<List<TypedLocalDraft>> read(String origin, String owner) =>
      _serial(_key(origin, owner), () async {
        final raw = await _storage.read(key: _key(origin, owner));
        if (raw == null) return const [];
        if (utf8.encode(raw).length > 524288)
          throw const FormatException('本机草稿过大');
        final j = jsonDecode(raw) as Map<String, dynamic>;
        _keys(j, {'schemaVersion', 'origin', 'owner', 'drafts'});
        if (j['schemaVersion'] != 'birdtie-local-drafts-v1' ||
            j['origin'] != origin ||
            j['owner'] != owner ||
            j['drafts'] is! List ||
            (j['drafts'] as List).length > 40)
          throw const FormatException('草稿来源已变化');
        final rows = (j['drafts'] as List)
            .map((r) => TypedLocalDraft.fromJson(r as Map<String, dynamic>))
            .toList();
        if (rows.any((r) => r.origin != origin || r.owner != owner) ||
            rows.map((r) => r.scopeKey).toSet().length != rows.length)
          throw const FormatException('草稿主体无效');
        return List.unmodifiable(rows);
      });
  @override
  Future<void> save(String origin, String owner, List<TypedLocalDraft> drafts) {
    if (drafts.length > 40 ||
        drafts.any((r) => r.origin != origin || r.owner != owner))
      return Future.error(const FormatException('草稿数量或主体无效'));
    // Validate before touching storage; a body cannot contain a restored token.
    for (final d in drafts) TypedLocalDraft.fromJson(d.toJson());
    return _serial(_key(origin, owner), () async {
      final previous = await _storage.read(key: _key(origin, owner));
      final merged = <String, TypedLocalDraft>{};
      if (previous != null) {
        if (utf8.encode(previous).length > 524288)
          throw const FormatException('本机草稿过大');
        final j = jsonDecode(previous) as Map<String, dynamic>;
        _keys(j, {'schemaVersion', 'origin', 'owner', 'drafts'});
        if (j['schemaVersion'] != 'birdtie-local-drafts-v1' ||
            j['origin'] != origin ||
            j['owner'] != owner ||
            j['drafts'] is! List)
          throw const FormatException('草稿来源已变化');
        for (final raw in j['drafts'] as List) {
          final row = TypedLocalDraft.fromJson(raw as Map<String, dynamic>);
          if (row.origin != origin ||
              row.owner != owner ||
              merged.containsKey(row.scopeKey))
            throw const FormatException('草稿主体无效');
          merged[row.scopeKey] = row;
        }
      }
      // Save edited slots only. Slow restoration or another page must not
      // erase unrelated recipient/workspace drafts from the same namespace.
      for (final row in drafts) merged[row.scopeKey] = row;
      if (merged.length > 40) throw const FormatException('本机草稿数量过多');
      final raw = jsonEncode({
        'schemaVersion': 'birdtie-local-drafts-v1',
        'origin': origin,
        'owner': owner,
        'drafts': merged.values.map((r) => r.toJson()).toList(),
      });
      if (utf8.encode(raw).length > 524288)
        throw const FormatException('本机草稿过大');
      await _storage.write(key: _key(origin, owner), value: raw);
      if (await _storage.read(key: _key(origin, owner)) != raw)
        throw StateError('本机草稿保存未确认');
    });
  }

  @override
  Future<void> clear(String origin, String owner) => _serial(
    _key(origin, owner),
    () => _storage.delete(key: _key(origin, owner)),
  );
}
