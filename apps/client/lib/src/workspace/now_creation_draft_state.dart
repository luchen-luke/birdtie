import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';

import 'now_creation_draft.dart';
import 'typed_local_draft.dart';

/// Durable local material only. This store never restores a server operation,
/// receipt, authentication, media bytes or permission to publish.
class NowCreationDraftState extends ChangeNotifier {
  NowCreationDraftState({TypedLocalDraftStore? store})
    : _store = store ?? SecureTypedLocalDraftStore();

  final TypedLocalDraftStore _store;
  final _rows = <String, TypedLocalDraft>{};
  final _dirty = <String>{};
  String _origin = '';
  String? _owner;
  bool _suspended = false, _disposed = false;
  int _generation = 0;
  Timer? _saveTimer;
  String? storageError;

  String? get owner => _owner;
  String get origin => _origin;
  bool get suspended => _suspended;
  static String slot(String cityID) =>
      'now-creation:${sha256.convert(utf8.encode(cityID))}';
  static String _slot(String cityID, String kind) => slot(kind == 'moment' ? '' : cityID);
  static String _key(String cityID, String kind) => '$kind:${_slot(cityID, kind)}';

  NowCreationDraft? cached(String cityID, String kind) {
    if (_disposed || _suspended || _owner == null) return null;
    final row = _rows[_key(cityID, kind)];
    if (row == null) return null;
    try {
      return _decode(row, cityID, kind);
    } catch (_) {
      return null;
    }
  }

  void configure(String origin, String? owner, {String? rejectedOwner}) {
    if (_disposed) return;
    if (owner == null &&
        rejectedOwner != null &&
        rejectedOwner == _owner &&
        origin == _origin) {
      if (!_suspended) ++_generation;
      _suspended = true;
      _saveTimer?.cancel();
      return;
    }
    if (_origin == origin && _owner == owner && !_suspended) return;
    if (_owner != null && owner != _owner) {
      unawaited(_store.clear(_origin, _owner!).catchError((Object _) {}));
    }
    _saveTimer?.cancel();
    if (_origin != origin || _owner != owner) {
      _rows.clear();
      _dirty.clear();
    }
    _origin = origin;
    _owner = owner;
    _suspended = false;
    storageError = null;
    ++_generation;
  }

  bool _current(String origin, String owner, int generation) =>
      !_disposed &&
      !_suspended &&
      _origin == origin &&
      _owner == owner &&
      _generation == generation;

  Future<NowCreationDraft?> restore(String cityID, String kind) async {
    final owner = _owner, origin = _origin, generation = _generation;
    if (_disposed || _suspended || owner == null || origin.isEmpty) return null;
    final key = _key(cityID, kind);
    try {
      final cached = _rows[key];
      if (cached != null) return _decode(cached, cityID, kind);
      final rows = await _store.read(origin, owner);
      if (!_current(origin, owner, generation)) return null;
      // A late read cannot replace material edited after that read started.
      if (_rows.containsKey(key)) return _decode(_rows[key]!, cityID, kind);
      for (final row in rows) {
        if (row.origin != origin || row.owner != owner) {
          throw const FormatException('创建草稿主体不符');
        }
        if (row.kind != kind || row.slotID != _slot(cityID, kind)) continue;
        final draft = _decode(row, cityID, kind);
        _rows[key] = row;
        storageError = null;
        return draft;
      }
      storageError = null;
    } catch (_) {
      if (_current(origin, owner, generation)) {
        storageError = '本机创建草稿暂时无法恢复，可以继续编辑。';
      }
    }
    return null;
  }

  NowCreationDraft _decode(TypedLocalDraft row, String cityID, String kind) {
    if (row.workspaceID != null ||
        row.conversationID != null ||
        row.recipientID != null ||
        row.taskID != null ||
        row.audience != 'private' ||
        row.references.isNotEmpty ||
        row.attachments.isNotEmpty) {
      throw const FormatException('创建草稿不可携带授权或提交');
    }
    final draft = NowCreationDraft.fromJson(
      jsonDecode(row.text) as Map<String, dynamic>,
    );
    if ((kind != 'moment' && draft.cityID != cityID) ||
        draft.kind != kind ||
        row.kind != kind ||
        row.slotID != _slot(cityID, kind)) {
      throw const FormatException('创建草稿城市或类型不符');
    }
    return draft;
  }

  void remember(NowCreationDraft draft) {
    final owner = _owner;
    if (_disposed || _suspended || owner == null || _origin.isEmpty) return;
    final key = _key(draft.cityID, draft.kind), previous = _rows[key];
    final text = jsonEncode(draft.toJson());
    _rows[key] =
        previous?.edit(text: text) ??
        TypedLocalDraft(
          owner: owner,
          origin: _origin,
          kind: draft.kind,
          text: text,
          slotID: _slot(draft.cityID, draft.kind),
        );
    _dirty.add(key);
    _saveTimer?.cancel();
    _saveTimer = Timer(
      const Duration(milliseconds: 300),
      () => unawaited(flush()),
    );
  }

  Future<void> flush() async {
    _saveTimer?.cancel();
    final owner = _owner, origin = _origin, generation = _generation;
    if (_disposed || _suspended || owner == null || origin.isEmpty) return;
    final changes = {for (final key in _dirty) key: _rows[key]!};
    if (changes.isEmpty) return;
    try {
      for (final row in changes.values) {
        TypedLocalDraft.fromJson(row.toJson());
        _decode(
          row,
          NowCreationDraft.fromJson(
            jsonDecode(row.text) as Map<String, dynamic>,
          ).cityID,
          row.kind,
        );
      }
      await _store.save(origin, owner, changes.values.toList());
      if (!_current(origin, owner, generation)) return;
      for (final entry in changes.entries) {
        if (identical(_rows[entry.key], entry.value)) _dirty.remove(entry.key);
      }
      storageError = null;
    } catch (_) {
      if (!_current(origin, owner, generation)) return;
      storageError = '本机创建草稿尚未保存，当前内容仍在，请稍后重试。';
    }
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _saveTimer?.cancel();
    _disposed = true;
    ++_generation;
    super.dispose();
  }
}
