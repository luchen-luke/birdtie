import 'dart:async';
import 'dart:convert';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import '../content/moment_control.dart';
import 'human_causal_control.dart';
import 'moment_thread_api.dart';

const momentThreadJournalMaxBytes = 16 * 1024;

class MomentThreadPending {
  const MomentThreadPending(
    this.operationID,
    this.action,
    this.digest,
    this.sourceVersion,
    this.stage,
  );
  final String operationID, action, digest, sourceVersion, stage;
  MomentThreadPending get unknown => MomentThreadPending(
    operationID,
    action,
    digest,
    sourceVersion,
    'UNKNOWN',
  );
  bool same(MomentThreadPending other) =>
      operationID == other.operationID &&
      action == other.action &&
      digest == other.digest &&
      sourceVersion == other.sourceVersion;
  Map<String, Object> get json => {
    'operationId': operationID,
    'action': action,
    'payloadDigest': digest,
    'sourceVersion': sourceVersion,
    'stage': stage,
  };
  factory MomentThreadPending.decode(Object? raw) {
    final value = humanControlObject(raw, {
      'operationId',
      'action',
      'payloadDigest',
      'sourceVersion',
      'stage',
    });
    if (!humanControlID(value['operationId']) ||
        !const {
          'CREATE_PUBLISH',
          'PUBLISH',
          'WITHDRAW',
        }.contains(value['action']) ||
        !humanControlHash(value['payloadDigest']) ||
        !humanControlHash(value['sourceVersion']) ||
        !const {'PREPARED', 'UNKNOWN', 'RETIRED'}.contains(value['stage'])) {
      throw const FormatException('原串文待核实记录不符');
    }
    return MomentThreadPending(
      value['operationId'],
      value['action'],
      value['payloadDigest'],
      value['sourceVersion'],
      value['stage'],
    );
  }
}

class MomentThreadJournalState {
  const MomentThreadJournalState(
    this.localScope,
    this.entries,
    this.activeIndex,
    this.pending, {
    this.threadID = '',
    this.threadVersion = 0,
  });
  final String localScope;
  final List<MomentThreadDraftEntry> entries;
  final int activeIndex;
  final MomentThreadPending? pending;
  final String threadID;
  final int threadVersion;
  MomentThreadJournalState withPending(
    MomentThreadPending? pending, {
    bool clearDraft = false,
  }) => MomentThreadJournalState(
    localScope,
    clearDraft ? const [] : entries,
    clearDraft ? 0 : activeIndex,
    pending,
    threadID: clearDraft ? '' : threadID,
    threadVersion: clearDraft ? 0 : threadVersion,
  );
  Map<String, Object?> get json => {
    'schema': 'moment-thread-private-journal-v1',
    'localScope': localScope,
    'entries': entries.map((entry) => entry.json).toList(),
    'activeIndex': activeIndex,
    'pending': pending?.json,
    if (threadID.isNotEmpty) ...{
      'threadId': threadID,
      'threadVersion': threadVersion,
    },
  };
  factory MomentThreadJournalState.decode(Object? raw) {
    final value = humanControlObject(
      raw,
      {'schema', 'localScope', 'entries', 'activeIndex', 'pending'},
      optional: {'threadId', 'threadVersion'},
    );
    if (value['schema'] != 'moment-thread-private-journal-v1' ||
        !humanControlID(value['localScope']) ||
        value['entries'] is! List ||
        (value['entries'] as List).length > 20 ||
        value['activeIndex'] is! int ||
        value['activeIndex'] < 0 ||
        (value['entries'] as List).isNotEmpty &&
            value['activeIndex'] >= (value['entries'] as List).length) {
      throw const FormatException('私人串文草稿结构不符');
    }
    final entries = (value['entries'] as List)
        .map(MomentThreadDraftEntry.decode)
        .toList();
    if (value.containsKey('threadId') &&
        (!humanControlID(value['threadId']) ||
            !humanControlVersion(value['threadVersion'], zero: false) ||
            entries.any(
              (e) =>
                  !humanControlID(e.momentID) ||
                  !humanControlVersion(e.expectedRevision, zero: false),
            )))
      throw const FormatException('原串文私人草稿归属不符');
    if (entries.map((entry) => entry.localID).toSet().length !=
        entries.length) {
      throw const FormatException('私人串文草稿条目重复');
    }
    return MomentThreadJournalState(
      value['localScope'],
      List.unmodifiable(entries),
      value['activeIndex'],
      value['pending'] == null
          ? null
          : MomentThreadPending.decode(value['pending']),
      threadID: value['threadId'] as String? ?? '',
      threadVersion: value['threadVersion'] as int? ?? 0,
    );
  }
}

/// Encrypted, owner-and-origin-scoped private text and material selectors.
/// Approval, pixels, credentials and server content snapshots are never saved.
class MomentThreadJournal {
  MomentThreadJournal({
    FlutterSecureStorage? storage,
    Future<String?> Function(String)? readValue,
    Future<void> Function(String, String)? writeValue,
  }) : _storage = storage ?? const FlutterSecureStorage(),
       _readValue = readValue,
       _writeValue = writeValue;
  final FlutterSecureStorage _storage;
  final Future<String?> Function(String)? _readValue;
  final Future<void> Function(String, String)? _writeValue;
  static final Map<String, Future<void>> _tails = {};
  String _key(String owner, String origin) {
    if (!humanControlID(owner) || Uri.parse(origin).origin != origin) {
      throw const FormatException('私人串文存储主体不符');
    }
    return 'birdtie_moment_thread_${momentHash([owner, origin])}';
  }

  Future<T> _serial<T>(String key, Future<T> Function() action) async {
    final previous = _tails[key] ?? Future<void>.value();
    final released = Completer<void>();
    _tails[key] = released.future;
    await previous;
    try {
      return await action();
    } finally {
      released.complete();
      if (identical(_tails[key], released.future)) {
        _tails.remove(key);
      }
    }
  }

  Future<MomentThreadJournalState?> _load(String key) async {
    final raw = await (_readValue?.call(key) ?? _storage.read(key: key));
    if (raw == null) {
      return null;
    }
    if (utf8.encode(raw).length > momentThreadJournalMaxBytes) {
      throw const FormatException('私人串文存储超限');
    }
    return MomentThreadJournalState.decode(jsonDecode(raw));
  }

  Future<void> _save(String key, MomentThreadJournalState state) async {
    final raw = jsonEncode(state.json);
    if (utf8.encode(raw).length > momentThreadJournalMaxBytes) {
      throw const FormatException('草稿超过本机16KiB保存限额，当前输入仍保留');
    }
    await (_writeValue?.call(key, raw) ?? _storage.write(key: key, value: raw));
    final checked = await _load(key);
    if (checked == null || jsonEncode(checked.json) != raw) {
      throw StateError('本机私人串文记录未确认写入');
    }
  }

  Future<MomentThreadJournalState?> load(String owner, String origin) {
    final key = _key(owner, origin);
    return _serial(key, () => _load(key));
  }

  Future<void> saveDraft(
    String owner,
    String origin,
    MomentThreadJournalState state,
  ) {
    final key = _key(owner, origin);
    return _serial(key, () async {
      final current = await _load(key);
      if (current?.pending != null) {
        throw StateError('请先核实原串文操作');
      }
      await _save(key, state.withPending(null));
    });
  }

  Future<void> prepare(
    String owner,
    String origin,
    MomentThreadJournalState state,
    MomentThreadPending pending,
  ) {
    final key = _key(owner, origin);
    return _serial(key, () async {
      final current = await _load(key);
      if (current?.pending != null) {
        throw StateError('另一原串文操作尚未核实');
      }
      await _save(key, state.withPending(pending));
    });
  }

  Future<void> markUnknown(
    String owner,
    String origin,
    MomentThreadPending expected,
  ) {
    final key = _key(owner, origin);
    return _serial(key, () async {
      final current = await _load(key);
      if (current == null ||
          current.pending == null ||
          !current.pending!.same(expected) ||
          current.pending!.stage != 'PREPARED') {
        throw StateError('原串文待提交记录已变化');
      }
      await _save(key, current.withPending(expected.unknown));
    });
  }

  Future<void> finish(
    String owner,
    String origin,
    MomentThreadPending expected, {
    bool clearDraft = false,
  }) {
    final key = _key(owner, origin);
    return _serial(key, () async {
      final current = await _load(key);
      if (current == null ||
          current.pending == null ||
          !current.pending!.same(expected)) {
        throw StateError('原串文存储记录已变化');
      }
      await _save(key, current.withPending(null, clearDraft: clearDraft));
    });
  }

  Future<void> retire(String owner, String origin) {
    final key = _key(owner, origin);
    return _serial(key, () async {
      final current = await _load(key);
      final pending = current?.pending;
      if (current != null && pending != null) {
        await _save(
          key,
          current.withPending(
            MomentThreadPending(
              pending.operationID,
              pending.action,
              pending.digest,
              pending.sourceVersion,
              'RETIRED',
            ),
          ),
        );
      }
    });
  }
}
