import 'dart:convert';

import 'entity_share_pending_store.dart' show newEntityShareOperationID;
import 'typed_local_draft.dart';

/// A local editing namespace. Session tokens are deliberately not an axis.
class LocalDraftMediaNamespace {
  LocalDraftMediaNamespace({
    required this.owner,
    required this.origin,
    required this.targetKey,
    this.workspaceID,
    this.taskID,
  }) {
    _text(owner, 160);
    _text(origin, 2048);
    _text(targetKey, 4096);
    if (workspaceID != null) _text(workspaceID!, 160);
    if (taskID != null) _text(taskID!, 160);
  }

  factory LocalDraftMediaNamespace.fromDraft(TypedLocalDraft draft) =>
      LocalDraftMediaNamespace(
        owner: draft.owner,
        origin: draft.origin,
        workspaceID: draft.workspaceID,
        taskID: draft.taskID,
        targetKey: draft.scopeKey,
      );

  final String owner, origin, targetKey;
  final String? workspaceID, taskID;

  bool matches(LocalDraftMediaNamespace other) =>
      owner == other.owner &&
      origin == other.origin &&
      targetKey == other.targetKey &&
      workspaceID == other.workspaceID &&
      taskID == other.taskID;

  static void _text(String value, int limit) {
    if (value.isEmpty ||
        value.contains('\u0000') ||
        utf8.encode(value).length > limit) {
      throw const FormatException('本机附件范围无效');
    }
  }
}

/// A RAM frame returned by bind, not a grant or a durable attachment receipt.
class LocalDraftMediaBinding {
  LocalDraftMediaBinding._(this.namespace);
  final LocalDraftMediaNamespace namespace;
}

/// The caller supplies the actual current selection, never a restored ID.
/// identity is a stable RAM key (for example controller + local revision).
/// source and its liveness callback are retained only in this bridge's RAM.
class LocalDraftMediaSelection {
  const LocalDraftMediaSelection({
    required this.identity,
    required this.source,
    required this.current,
    this.state = 'local',
    this.sourceCurrent,
  });
  final Object identity, source;
  final bool current;
  final String state;
  final bool Function()? sourceCurrent;
}

/// A revocable RAM view. Even an earlier captured view loses its source when
/// a namespace, selection or source object changes. It cannot execute uploads.
class LocalDraftMediaCapture {
  LocalDraftMediaCapture._(this._bridge, this._binding, _MediaEntry entry)
    : _entry = entry,
      _generation = entry.generation;
  final LocalDraftMediaBridge _bridge;
  final LocalDraftMediaBinding _binding;
  final _MediaEntry _entry;
  final int _generation;
  Object? get source => _bridge._source(this);
  bool get current => source != null;
}

class _MediaEntry {
  _MediaEntry(this.identity, this.id, this.state);
  final Object identity;
  final String id;
  String state;
  Object? source;
  bool Function()? sourceCurrent;
  int generation = 0;

  void retire() {
    ++generation;
    source = null;
    sourceCurrent = null;
  }

  bool checkLive() {
    if (source == null) return false;
    try {
      if (sourceCurrent?.call() ?? true) return true;
    } catch (_) {
      // A failed current-source check never revives a captured selection.
    }
    retire();
    return false;
  }
}

/// Projects only {opaque id, kind, historical state} into TypedLocalDraft.
/// No HTTP, bytes, paths, source titles, token, approval or receipt persistence.
class LocalDraftMediaBridge {
  LocalDraftMediaBridge({String Function()? newSelector})
    : _newSelector = newSelector ?? newEntityShareOperationID;
  final String Function() _newSelector;
  final _slots = <String, List<_MediaEntry>>{};
  LocalDraftMediaBinding? _binding;
  bool _closed = false;
  static const _kinds = {'image', 'video', 'audio', 'file'};
  static const _states = {
    'local',
    'uploaded',
    'failed',
    'unknown',
    'unavailable',
  };

  LocalDraftMediaBinding bind(LocalDraftMediaNamespace namespace) {
    if (_closed) throw StateError('本机附件视图已关闭');
    final current = _binding;
    if (current != null && current.namespace.matches(namespace)) return current;
    retire();
    return _binding = LocalDraftMediaBinding._(namespace);
  }

  bool isCurrent(LocalDraftMediaBinding binding) =>
      !_closed && identical(binding, _binding);

  /// Merge one fixed kind slot. Only this binding's owned selectors may be
  /// removed. Equal semantic lists return the exact original draft so host
  /// listeners do not create revision/save loops.
  TypedLocalDraft sync(
    LocalDraftMediaBinding binding,
    TypedLocalDraft draft, {
    required String kind,
    required Iterable<LocalDraftMediaSelection> selections,
  }) {
    if (!isCurrent(binding) ||
        !binding.namespace.matches(LocalDraftMediaNamespace.fromDraft(draft))) {
      throw StateError('本机附件范围已退休');
    }
    if (!_kinds.contains(kind)) throw const FormatException('附件类型无效');
    final selected = selections.toList(growable: false);
    if (selected.length > 10 ||
        selected.any((s) => !_states.contains(s.state)) ||
        selected.map((s) => s.identity).toSet().length != selected.length) {
      throw const FormatException('本机附件选择无效');
    }
    final old = _slots[kind] ?? const <_MediaEntry>[];
    final owned = old.map((e) => e.id).toSet();
    if (draft.attachments.any((a) => owned.contains(a.id) && a.kind != kind)) {
      throw const FormatException('本机附件种类已变化，请重新选择');
    }
    final retained = draft.attachments
        .where((a) => !owned.contains(a.id))
        .toList();
    if (retained.length + selected.length > 10 ||
        draft.attachments.map((a) => a.id).toSet().length !=
            draft.attachments.length) {
      throw const FormatException('附件过多或标识重复，请先整理已有附件');
    }
    final taken = {
      ...draft.attachments.map((a) => a.id),
      ..._slots.values.expand((s) => s.map((e) => e.id)),
    };
    final next = <_MediaEntry>[];
    for (final selection in selected) {
      final matches = old.where((e) => e.identity == selection.identity);
      final entry = matches.isEmpty
          ? _MediaEntry(selection.identity, _allocate(taken), selection.state)
          : matches.first;
      next.add(entry);
    }
    // Validation/allocation completed before changing the old RAM slot.
    for (final entry in old.where((e) => !next.contains(e))) entry.retire();
    for (var i = 0; i < next.length; i++) {
      final entry = next[i], selection = selected[i];
      if (!selection.current || selection.state == 'unavailable') {
        entry.retire();
      } else {
        if (!identical(entry.source, selection.source)) ++entry.generation;
        entry.source = selection.source;
        entry.sourceCurrent = selection.sourceCurrent;
        entry.checkLive();
      }
      entry.state = selection.state;
    }
    if (!isCurrent(binding)) {
      for (final entry in next) entry.retire();
      throw StateError('本机附件范围已退休');
    }
    _slots[kind] = next;
    final replacement = [
      for (final entry in next)
        LocalDraftAttachment(id: entry.id, kind: kind, state: entry.state),
    ];
    final attachments = <LocalDraftAttachment>[];
    var inserted = false;
    for (final attachment in draft.attachments) {
      if (owned.contains(attachment.id)) {
        if (!inserted) attachments.addAll(replacement);
        inserted = true;
      } else {
        attachments.add(attachment);
      }
    }
    if (!inserted) attachments.addAll(replacement);
    final equal =
        draft.attachments.length == attachments.length &&
        List.generate(attachments.length, (i) => i).every(
          (i) =>
              draft.attachments[i].id == attachments[i].id &&
              draft.attachments[i].kind == attachments[i].kind &&
              draft.attachments[i].state == attachments[i].state,
        );
    return equal ? draft : draft.edit(attachments: attachments);
  }

  String _allocate(Set<String> taken) {
    for (var attempt = 0; attempt < 16; attempt++) {
      final id = _newSelector();
      if (!RegExp(
        r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
      ).hasMatch(id)) {
        throw const FormatException('本机附件标识无法创建');
      }
      if (taken.add(id)) return id;
    }
    throw StateError('本机附件标识重复');
  }

  LocalDraftMediaCapture? capture(
    LocalDraftMediaBinding binding,
    LocalDraftAttachment attachment,
  ) {
    if (!isCurrent(binding)) return null;
    for (final entry in _slots[attachment.kind] ?? const <_MediaEntry>[]) {
      if (entry.id == attachment.id &&
          entry.checkLive() &&
          isCurrent(binding)) {
        return LocalDraftMediaCapture._(this, binding, entry);
      }
    }
    return null;
  }

  bool displayNeedsReselect(
    LocalDraftMediaBinding binding,
    LocalDraftAttachment attachment,
  ) => capture(binding, attachment) == null;

  /// Loss of the actual controller retires RAM without interpreting an empty
  /// current selection as user removal or erasing its historical unknown slot.
  void retireSources(LocalDraftMediaBinding binding, {String? kind}) {
    if (!isCurrent(binding)) return;
    if (kind != null && !_kinds.contains(kind)) {
      throw const FormatException('附件类型无效');
    }
    for (final slot in _slots.entries) {
      if (kind == null || slot.key == kind) {
        for (final entry in slot.value) entry.retire();
      }
    }
  }

  Object? _source(LocalDraftMediaCapture captured) {
    final entry = captured._entry;
    if (!isCurrent(captured._binding) ||
        entry.generation != captured._generation) {
      return null;
    }
    final live = entry.checkLive();
    return live &&
            isCurrent(captured._binding) &&
            entry.generation == captured._generation &&
            entry.source != null
        ? entry.source
        : null;
  }

  void retire() {
    for (final slot in _slots.values) {
      for (final entry in slot) entry.retire();
    }
    _slots.clear();
    _binding = null;
  }

  void dispose() {
    retire();
    _closed = true;
  }
}
