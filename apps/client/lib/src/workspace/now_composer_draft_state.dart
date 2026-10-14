import 'package:flutter/material.dart';
import 'dart:async';

import 'connections.dart';
import 'typed_local_draft.dart';

/// A recipient is presentation plus an immutable routing selector. Selecting
/// it never creates/sends a message or grants access to a conversation.
class NowComposerTarget {
  const NowComposerTarget.agent()
    : conversationID = null,
      recipientAccountID = null,
      displayName = null;
  NowComposerTarget.human(HumanConversation conversation)
    : conversationID = conversation.id,
      recipientAccountID = conversation.otherAccountId,
      displayName = conversation.otherName;
  final String? conversationID, recipientAccountID, displayName;
  bool get human => conversationID != null;
  Object get key => (conversationID, recipientAccountID);
}

/// In-memory, owner/recipient-separated editable material. Tokens, approvals,
/// audio and server submission receipts are deliberately never stored here.
class NowComposerDraftState extends ChangeNotifier {
  NowComposerDraftState({TypedLocalDraftStore? store}) : _store = store;
  String? _owner, _suspendedOwner;
  String _origin = '';
  String? _messageOrigin;
  String? _workspaceID;
  TypedLocalDraftStore? _store;
  Timer? _saveTimer;
  bool _disposed = false;
  String? storageError;
  final _drafts = <Object, TextEditingValue>{};
  final _envelopes = <Object, TypedLocalDraft>{};
  final _dirty = <Object>{};
  NowComposerTarget target = const NowComposerTarget.agent();
  int _generation = 0;
  String get _activeOrigin =>
      target.human ? (_messageOrigin ?? _origin) : _origin;
  Object get _key => (_activeOrigin, _workspaceID, target.key);
  Object get identity =>
      (_owner, _activeOrigin, _workspaceID, target.key, _generation);
  String? get owner => _owner;
  String? get workspaceID => _workspaceID;
  bool get suspended => _suspendedOwner != null;
  TextEditingValue get value => _drafts[_key] ?? TextEditingValue.empty;
  TypedLocalDraft? get envelope {
    final owner = _owner;
    if (owner == null) return null;
    return _envelopes.putIfAbsent(
      _key,
      () => TypedLocalDraft(
        owner: owner,
        origin: _activeOrigin,
        workspaceID: _workspaceID,
        kind: target.human ? 'message' : 'intent',
        text: value.text,
        conversationID: target.conversationID,
        recipientID: target.recipientAccountID,
        audience: target.human ? 'recipient' : 'private',
      ),
    );
  }

  String? get contentVersion => envelope?.contentVersion;

  void configurePersistence(
    String origin, {
    String? messageOrigin,
    TypedLocalDraftStore? store,
  }) {
    if (_origin == origin && _messageOrigin == messageOrigin && _store != null)
      return;
    _saveTimer?.cancel();
    if (_origin.isNotEmpty &&
        (_origin != origin || _messageOrigin != messageOrigin)) {
      _drafts.clear();
      _envelopes.clear();
      _dirty.clear();
      ++_generation;
      target = const NowComposerTarget.agent();
    }
    _origin = origin;
    _messageOrigin = messageOrigin;
    _store = store ?? _store ?? SecureTypedLocalDraftStore();
    if (_owner != null && origin.isNotEmpty) unawaited(restore());
  }

  void observeWorkspace(String? workspaceID) {
    if (workspaceID == _workspaceID) return;
    unawaited(flush());
    _workspaceID = workspaceID;
    if (target.human) target = const NowComposerTarget.agent();
    ++_generation;
    _notify();
    if (_activeOrigin.isNotEmpty && _store != null) unawaited(restore());
  }

  Future<void> restore() async {
    final owner = _owner,
        origin = _activeOrigin,
        store = _store,
        generation = _generation;
    if (owner == null ||
        origin.isEmpty ||
        store == null ||
        suspended ||
        _disposed)
      return;
    try {
      final rows = await store.read(origin, owner);
      if (_disposed ||
          owner != _owner ||
          origin != _activeOrigin ||
          generation != _generation ||
          suspended)
        return;
      for (final row in rows) {
        if (row.owner != owner || row.origin != origin)
          throw const FormatException();
        // Route/import views share this private store, not the send target.
        if (!const {'intent', 'message'}.contains(row.kind)) continue;
        final key = (
          row.origin,
          row.workspaceID,
          (row.conversationID, row.recipientID),
        );
        // A slow disk read cannot replace an edit already made in this session.
        if (_drafts.containsKey(key)) continue;
        _envelopes[key] = row;
        _drafts[key] = TextEditingValue(
          text: row.text,
          selection: TextSelection.collapsed(offset: row.text.length),
        );
      }
      storageError = null;
      _notify();
    } catch (_) {
      if (!_disposed && owner == _owner && origin == _activeOrigin) {
        storageError = '本机草稿暂时无法恢复，未发送任何内容。';
        _notify();
      }
    }
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _scheduleSave() {
    _saveTimer?.cancel();
    if (_store != null &&
        _owner != null &&
        _activeOrigin.isNotEmpty &&
        !suspended) {
      _saveTimer = Timer(
        const Duration(milliseconds: 300),
        () => unawaited(flush()),
      );
    }
  }

  Future<void> flush() async {
    _saveTimer?.cancel();
    final owner = _owner, store = _store;
    if (_disposed ||
        owner == null ||
        _activeOrigin.isEmpty ||
        store == null ||
        suspended)
      return;
    final changes = {
      for (final key in _dirty)
        if (_envelopes[key]?.owner == owner &&
            _envelopes[key]!.origin.isNotEmpty)
          key: _envelopes[key]!,
    };
    if (changes.isEmpty) return;
    final groups = <String, List<TypedLocalDraft>>{};
    for (final row in changes.values)
      groups.putIfAbsent(row.origin, () => []).add(row);
    try {
      await Future.wait(
        groups.entries.map((g) => store.save(g.key, owner, g.value)),
      );
      for (final item in changes.entries)
        if (identical(_envelopes[item.key], item.value))
          _dirty.remove(item.key);
      if (!_disposed && owner == _owner) storageError = null;
    } catch (_) {
      if (!_disposed && owner == _owner) {
        storageError = '本机草稿尚未保存，当前内容仍在，请稍后重试。';
        _notify();
      }
    }
  }

  void setContext({
    String? taskID,
    List<LocalDraftReference>? references,
    List<LocalDraftAttachment>? attachments,
  }) {
    if (suspended || _disposed) return;
    final current = envelope;
    if (current == null) return;
    _envelopes[_key] = current.edit(
      taskID: taskID,
      references: references,
      attachments: attachments,
    );
    _dirty.add(_key);
    _scheduleSave();
    _notify();
  }

  /// An explicit local edit only. Captured analysis pages cannot replace a
  /// newer draft or move material into another account/recipient namespace.
  Future<bool> saveEditedText(
    String text, {
    required Object expectedIdentity,
    required String expectedVersion,
  }) async {
    if (_disposed ||
        suspended ||
        _store == null ||
        _activeOrigin.isEmpty ||
        identity != expectedIdentity ||
        contentVersion != expectedVersion) {
      return false;
    }
    final original = envelope;
    if (original == null || original.kind != 'intent' || target.human)
      return false;
    try {
      TypedLocalDraft.fromJson(original.edit(text: text).toJson());
    } on FormatException {
      return false;
    }
    final edited = original.edit(text: text), store = _store!;
    _saveTimer?.cancel();
    bool current() =>
        !_disposed &&
        !suspended &&
        identity == expectedIdentity &&
        contentVersion == expectedVersion;
    try {
      await store.save(original.origin, original.owner, [edited]);
      final rows = await store.read(original.origin, original.owner);
      if (!current()) return false;
      if (!rows.any(
        (row) =>
            row.id == edited.id &&
            row.scopeKey == edited.scopeKey &&
            row.contentVersion == edited.contentVersion,
      )) {
        throw StateError('本机草稿保存未确认');
      }
      _envelopes[_key] = edited;
      _drafts[_key] = TextEditingValue(
        text: text,
        selection: TextSelection.collapsed(offset: text.length),
      );
      _dirty.remove(_key);
      storageError = null;
      _notify();
      return true;
    } catch (_) {
      if (current()) {
        storageError = '本机草稿尚未保存，当前编辑文字仍在，请稍后重试。';
        _notify();
      }
      return false;
    }
  }

  TypedLocalDraft? derive(String kind) =>
      suspended ? null : envelope?.converted(kind);
  void _retireStoredOwner(String? owner) {
    _saveTimer?.cancel();
    final store = _store;
    if (owner != null && store != null) {
      for (final origin in {
        _origin,
        _messageOrigin,
      }.whereType<String>().where((v) => v.isNotEmpty))
        unawaited(store.clear(origin, owner).catchError((Object _) {}));
    }
  }

  void observeOwner(String? owner, {String? rejectedOwner}) {
    if (_owner == owner && _suspendedOwner == null) return;
    if (owner == null && rejectedOwner != null && rejectedOwner == _owner) {
      if (_suspendedOwner == rejectedOwner) return;
      _suspendedOwner = rejectedOwner;
      _saveTimer?.cancel();
      ++_generation;
      _notify();
      return;
    }
    if (_owner == null && _suspendedOwner == null && owner != null) {
      _owner = owner;
      ++_generation;
      if (value.text.isNotEmpty && envelope != null) {
        _dirty.add(_key);
        _scheduleSave();
      }
      if (_origin.isNotEmpty) unawaited(restore());
      _notify();
      return;
    }
    if (_suspendedOwner != null && owner == _suspendedOwner) {
      _owner = owner;
      _suspendedOwner = null;
      ++_generation;
      _scheduleSave();
      if (_activeOrigin.isNotEmpty && _store != null) unawaited(restore());
      _notify();
      return;
    }
    if (_owner != owner || _suspendedOwner != null) {
      _retireStoredOwner(_owner);
      _drafts.clear();
      _envelopes.clear();
      _dirty.clear();
      storageError = null;
      target = const NowComposerTarget.agent();
      _workspaceID = null;
      _owner = owner;
      _suspendedOwner = null;
      ++_generation;
      if (owner != null && _origin.isNotEmpty) unawaited(restore());
      _notify();
    }
  }

  void update(TextEditingValue value) {
    if (suspended) return;
    final previous = value.text == this.value.text;
    _drafts[_key] = value;
    if (!previous && envelope != null) {
      _envelopes[_key] = envelope!.edit(text: value.text);
      _dirty.add(_key);
    }
    _scheduleSave();
    if (!previous) _notify();
  }

  void select(NowComposerTarget next) {
    if (next.key == target.key) return;
    if (_workspaceID != null && next.human) return;
    unawaited(flush());
    target = next;
    ++_generation;
    if (_activeOrigin.isNotEmpty && _store != null) unawaited(restore());
    _notify();
  }

  void clearSent(
    Object exactIdentity, {
    String? expectedText,
    String? expectedVersion,
    bool preserveMaterials = false,
    String? taskID,
  }) {
    if (identity != exactIdentity ||
        suspended ||
        expectedText != null && value.text != expectedText ||
        expectedVersion != null && contentVersion != expectedVersion)
      return;
    _drafts[_key] = TextEditingValue.empty;
    if (envelope != null)
      _envelopes[_key] = envelope!.edit(
        text: '',
        taskID: taskID,
        references: preserveMaterials ? envelope!.references : const [],
        attachments: preserveMaterials ? envelope!.attachments : const [],
      );
    _dirty.add(_key);
    _scheduleSave();
    _notify();
  }

  void clear() {
    _drafts.clear();
    _envelopes.clear();
    _dirty.clear();
    _retireStoredOwner(_owner);
    _suspendedOwner = null;
    ++_generation;
    _notify();
  }

  @override
  void dispose() {
    _saveTimer?.cancel();
    _disposed = true;
    super.dispose();
  }
}
