import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'agent_memory_candidate_api.dart'
    show candidateIDValid, HumanMemoryCandidate;
import 'agent_memory_correction_api.dart';
import 'agent_memory_correction_pending_store.dart';
import 'agent_memory_field_evidence.dart';
import 'agent_memory_self_review.dart';

String _canonical(dynamic v) {
  dynamic sorted(dynamic x) {
    if (x is Map) {
      final keys = x.keys.cast<String>().toList()..sort();
      return {for (final k in keys) k: sorted(x[k])};
    }
    if (x is List) return x.map(sorted).toList();
    return x;
  }

  return jsonEncode(sorted(v));
}

class AgentMemoryCorrectionController extends ChangeNotifier {
  AgentMemoryCorrectionController({
    required String? Function() authorizationHeader,
    required String? Function() accountID,
    String? Function()? organizationWorkspaceID,
    AgentMemoryCorrectionAPI? api,
    AgentMemoryCorrectionPendingStore? pendingStore,
    DateTime Function()? now,
    this.elapsed,
    this.current,
  }) : _token = authorizationHeader,
       _owner = accountID,
       _workspace = organizationWorkspaceID ?? (() => null),
       api = api ?? AgentMemoryCorrectionAPI(),
       store = pendingStore ?? const SecureAgentMemoryCorrectionPendingStore(),
       _now = now ?? (() => DateTime.now().toUtc());
  final String? Function() _token, _owner, _workspace;
  final AgentMemoryCorrectionAPI api;
  final AgentMemoryCorrectionPendingStore store;
  final DateTime Function() _now;
  final Duration Function()? elapsed;
  final bool Function()? current;
  final Stopwatch _watch = Stopwatch()..start();
  Duration? _reviewDeadline;
  Duration _ticks() => elapsed?.call() ?? _watch.elapsed;
  bool _reviewTimeCurrent(CorrectionPreview p) =>
      _reviewDeadline != null &&
      _ticks() < _reviewDeadline! &&
      p.expiresAt.isAfter(_now());
  String? _boundToken, _boundOwner, _boundWorkspace;
  bool _bound = false, _closed = false, _retired = false;
  bool busy = false,
      unknown = false,
      storageBlocked = false,
      denied = false,
      candidatesUnavailable = false;
  String? error, message;
  List<CorrectableMemory> memories = const [];
  List<HumanMemoryCandidate> candidates = const [];
  CorrectionPreview? preview;
  PendingMemoryCorrection? _pending;
  bool get retired => _retired;
  bool get hasPending => _pending != null;
  bool get _current =>
      (current?.call() ?? true) &&
      !_closed &&
      !_retired &&
      !denied &&
      _bound &&
      _boundToken != null &&
      _boundToken!.isNotEmpty &&
      candidateIDValid(_boundOwner) &&
      _boundWorkspace == null &&
      _token() == _boundToken &&
      _owner() == _boundOwner &&
      _workspace() == _boundWorkspace;
  bool get canReview =>
      _current &&
      !busy &&
      !unknown &&
      !hasPending &&
      !storageBlocked &&
      !denied;
  bool get canConfirm =>
      _current &&
      !busy &&
      !unknown &&
      !storageBlocked &&
      preview != null &&
      _pending != null &&
      _reviewTimeCurrent(preview!);
  // This selected same-snapshot read has no preview/journal/approval state.
  MemorySelfReview? selfReview;
  String? selfReviewMemoryID, selfReviewError;
  bool selfReviewLoading = false;
  int _selfReviewSerial = 0;
  Duration? _selfReviewDeadline;
  CorrectableMemory? _selfReviewSelected;
  bool canReadSelfReview(CorrectableMemory m) =>
      _current && !busy && !unknown && !storageBlocked && memories.contains(m) &&
      m.sourceType == 'EXPLICIT' && m.status == 'ACTIVE' && m.validUntil.isAfter(_now());

  void _invalidateSelfReview() {
    _selfReviewSerial++;
    selfReview = null;
    selfReviewMemoryID = selfReviewError = null;
    selfReviewLoading = false;
    _selfReviewDeadline = null;
    _selfReviewSelected = null;
  }

  bool _selfReviewCurrent(int serial, CorrectableMemory m) {
    identityChanged();
    if (serial != _selfReviewSerial) return false;
    if (!_current || !memories.contains(m) || m.sourceType != 'EXPLICIT' ||
        m.status != 'ACTIVE' || !m.validUntil.isAfter(_now())) {
      _invalidateSelfReview();
      _changed();
      return false;
    }
    return true;
  }

  Future<void> readSelfReview(CorrectableMemory m) async {
    identityChanged();
    if (!canReadSelfReview(m)) return;
    _invalidateSelfReview();
    final serial = _selfReviewSerial, started = _ticks();
    final token = _boundToken!, owner = _boundOwner!;
    final body = <String, dynamic>{
      'profileFields': ['preferredActivityTypes'], 'memoryIds': [m.id], 'policyFamilies': [],
    };
    selfReviewMemoryID = m.id;
    _selfReviewSelected = m;
    selfReviewLoading = true;
    _changed();
    // A synchronous identity notice can retire this read before real send.
    if (!_selfReviewCurrent(serial, m)) return;
    try {
      final raw = await api.request('POST', 'self-review', token, body: body);
      if (!_selfReviewCurrent(serial, m)) return;
      final view = MemorySelfReview.read(raw, owner: owner, selected: m, now: _now());
      final received = _ticks(), wait = received - started;
      final native = view.expiresAt.difference(view.observedAt) - wait;
      final utc = view.expiresAt.difference(_now());
      final remaining = native < utc ? native : utc;
      if (wait < Duration.zero || remaining <= Duration.zero) {
        throw const FormatException('同体审阅已到期');
      }
      _selfReviewDeadline = received + remaining;
      selfReview = view;
    } catch (e) {
      if (!_selfReviewCurrent(serial, m)) return;
      selfReview = null;
      selfReviewError = e is CorrectionHTTPError
          ? switch (e.status) {
              401 => '请恢复本人登录后重新打开，才能核对当前声明。',
              403 => '当前主体或来源权限已变化，请以本人个人身份重新打开。',
              409 => '本次审阅或来源已到期，请重新读取当前记忆后再次核对。',
              _ => '暂时无法核对这些来源；当前记忆和更正批准未改变。',
            }
          : e is FormatException
              ? '同体审阅或当前记忆版本不符，请重新读取当前记忆后核对。'
              : '暂时无法核对这些来源；当前记忆和更正批准未改变。';
    } finally {
      if (_selfReviewCurrent(serial, m)) {
        selfReviewLoading = false;
        _changed();
      }
    }
  }

  void expireSelfReview() {
    identityChanged();
    final view = selfReview;
    if (!_current || view == null) return;
    final selected = _selfReviewSelected;
    if (selected == null || !_selfReviewCurrent(_selfReviewSerial, selected)) return;
    if (_selfReviewDeadline != null && _ticks() < _selfReviewDeadline! &&
        view.expiresAt.isAfter(_now())) {
      return;
    }
    final id = selfReviewMemoryID;
    _invalidateSelfReview();
    selfReviewMemoryID = id;
    selfReviewError = '同体来源审阅已到期，请再次核对；更正操作与批准状态未因此改变。';
    _changed();
  }

  void closeSelfReview() {
    _invalidateSelfReview();
    _changed();
  }
  // Detail is an independent, expiring human read, never a correction approval.
  MemoryFieldEvidenceDetail? fieldDetail;
  String? fieldMemoryID, fieldError;
  bool fieldLoading = false;
  int _fieldSerial = 0;
  Duration? _fieldDeadline;
  bool canReadFieldEvidence(CorrectableMemory m) =>
      _current && !busy && !unknown && !storageBlocked && memories.contains(m);

  void _invalidateField() {
    _fieldSerial++;
    fieldDetail = null;
    fieldMemoryID = null;
    fieldError = null;
    fieldLoading = false;
    _fieldDeadline = null;
  }

  bool _fieldCurrent(int serial, CorrectableMemory m) {
    identityChanged();
    return _current && serial == _fieldSerial && memories.contains(m);
  }

  Future<void> readFieldEvidence(CorrectableMemory m) async {
    identityChanged();
    if (!canReadFieldEvidence(m)) return;
    _invalidateField();
    final serial = _fieldSerial, started = _ticks();
    fieldMemoryID = m.id;
    fieldLoading = true;
    _changed();
    // A synchronous observer may retire the source during the loading notice.
    if (!_fieldCurrent(serial, m)) return;
    try {
      final raw = await api.request('GET', 'memories/${m.id}', _boundToken!);
      if (!_fieldCurrent(serial, m)) return;
      final detail = MemoryFieldEvidenceDetail.read(
        raw, owner: _boundOwner!, memoryID: m.id, agentID: m.agentID,
        version: m.version, now: _now(),
      );
      if (detail.status != m.status ||
          detail.memory != null && _canonical(detail.memory!.raw) != _canonical(m.raw)) {
        throw const FormatException('当前记忆版本已改变');
      }
      final received = _ticks(), wait = received - started;
      final native = detail.expiresAt.difference(detail.observedAt) - wait;
      final utc = detail.expiresAt.difference(_now());
      final remaining = native < utc ? native : utc;
      if (wait < Duration.zero || remaining <= Duration.zero) {
        throw const FormatException('来源详情已到期');
      }
      _fieldDeadline = received + remaining;
      fieldDetail = detail;
    } catch (e) {
      if (!_fieldCurrent(serial, m)) return;
      fieldDetail = null;
      fieldError = e is CorrectionHTTPError
          ? switch (e.status) {
              401 => '请恢复本人登录后重新打开，才能查看记忆来源。',
              403 => '当前身份没有查看权限，请恢复本人个人身份后重新打开。',
              404 => '这条记忆目前不可查看，请重新读取当前记忆。',
              409 => '这条记忆已变化，请重新读取当前记忆后查看来源。',
              _ => '暂时无法读取来源与时间，请稍后再次查看。',
            }
          : e is FormatException
              ? '来源详情或记忆版本已变化，请重新读取当前记忆后查看来源。'
              : '暂时无法读取来源与时间，请稍后再次查看。';
    } finally {
      if (_fieldCurrent(serial, m)) {
        fieldLoading = false;
        _changed();
      }
    }
  }

  void expireFieldEvidence() {
    identityChanged();
    final detail = fieldDetail;
    if (!_current || detail == null) return;
    if (_fieldDeadline != null && _ticks() < _fieldDeadline! &&
        detail.expiresAt.isAfter(_now())) {
      return;
    }
    final id = fieldMemoryID;
    _invalidateField();
    fieldMemoryID = id;
    fieldError = '来源详情已到期，请再次查看；更正操作与批准状态未因此改变。';
    _changed();
  }

  void identityChanged() {
    if (_bound && !_current) retire();
  }

  void retire() {
    if (_closed || _retired) return;
    _retired = true;
    _invalidateSelfReview();
    _invalidateField();
    busy = false;
    preview = null;
    memories = const [];
    candidates = const [];
    message = null;
    error = '身份或连接已变化，请关闭后以当前身份重新打开。';
    _changed();
  }

  bool _bind() {
    if (denied || _closed || _retired || !(current?.call() ?? true)) return false;
    if (_bound) {
      identityChanged();
      return _current;
    }
    _bound = true;
    _boundToken = _token();
    _boundOwner = _owner();
    _boundWorkspace = _workspace();
    if (_boundToken == null ||
        _boundToken!.isEmpty ||
        !candidateIDValid(_boundOwner) ||
        _boundWorkspace != null) {
      denied = true;
      error = '请以本人有效个人身份管理记忆。';
      _changed();
      return false;
    }
    return true;
  }

  void _changed() {
    if (!_closed) notifyListeners();
  }

  Future<void> load() async {
    if (busy || !_bind()) return;
    _invalidateSelfReview();
    _invalidateField();
    busy = true;
    error = null;
    _changed();
    var readingStorage = true;
    try {
      final p = await store.read(api.environment, _boundOwner!);
      if (!_current) return;
      readingStorage = false;
      _pending = p;
      storageBlocked = false;
      if (p != null) {
        unknown = true;
        busy = false;
        _changed();
        await verify();
        return;
      }
      final ms = await api.memories(_boundToken!, _boundOwner!);
      if (!_current) return;
      memories = ms;
      candidates = const [];
      candidatesUnavailable = false;
      try {
        final cs = await api.candidates(_boundToken!, _boundOwner!);
        if (!_current) return;
        final agents = {
          ...ms.map((m) => m.agentID),
          ...cs.map((c) => c.raw['agentId'] as String),
        };
        if (agents.length > 1) throw const FormatException('当前Agent绑定不符');
        candidates = cs;
      } on CorrectionHTTPError catch (e) {
        if (!_current) return;
        if (e.status != 503) rethrow;
        candidatesUnavailable = true;
      }
      unknown = false;
      message = memories.isEmpty && candidates.isEmpty
          ? '当前没有可管理的记忆。'
          : '选择本人当前记忆，先检查具体版本再确认。';
    } catch (e) {
      if (_current) {
        if (readingStorage) {
          storageBlocked = true;
          error = '本机核实记录暂时无法安全读取，未提交新操作。请重新核实。';
        } else if (e is FormatException) {
          storageBlocked = true;
          error = '回执或本机核实记录不符，未提交新操作。';
        } else {
          memories = const [];
          candidates = const [];
          error = '暂时无法读取，请稍后重试。';
        }
      }
    } finally {
      if (_current) {
        busy = false;
        _changed();
      }
    }
  }

  Future<void> reviewMemory(
    CorrectableMemory m,
    String action, {
    String? summary,
    String? category,
  }) async {
    if (!memories.contains(m) ||
        m.status == 'DELETED' ||
        !m.validUntil.isAfter(_now()) ||
        action == 'EDIT' && m.sourceType != 'EXPLICIT' ||
        action == 'NEGATE' && category != m.category) {
      return;
    }
    await _review(
      {
        'id': api.newID(),
        'targetKind': 'MEMORY',
        'targetId': m.id,
        'expectedVersion': m.version,
        'action': action,
        if (action == 'EDIT')
          'replacement': m.replacement(
            (summary ?? '').replaceAll('\r\n', '\n'),
          ),
        if (action == 'NEGATE') 'category': category,
      },
      m.agentID,
      m.validUntil,
      selected: m,
    );
  }

  Future<void> reviewCandidate(
    HumanMemoryCandidate c,
    String action, {
    String? category,
  }) async {
    if (!candidates.contains(c) ||
        c.status != 'CANDIDATE' ||
        !c.validUntil.isAfter(_now()) ||
        !{'REJECT', 'NEGATE'}.contains(action) ||
        action == 'NEGATE' && category != c.category) {
      return;
    }
    await _review(
      {
        'id': api.newID(),
        'targetKind': 'CANDIDATE',
        'targetId': c.id,
        'expectedVersion': c.version,
        'action': action,
        if (action == 'NEGATE') 'category': category,
      },
      c.raw['agentId'],
      c.validUntil,
    );
  }

  PendingMemoryCorrection _journal(
    Map<String, dynamic> input,
    String agent,
    DateTime end, {
    CorrectionPreview? reviewed,
  }) => PendingMemoryCorrection({
    'environment': completionFingerprint(api.environment),
    'ownerId': _boundOwner,
    'agentId': agent,
    'sessionFingerprint': completionFingerprint(_boundToken!),
    'operationId': input['id'],
    'targetId': input['targetId'],
    'expectedVersion': input['expectedVersion'],
    'targetKind': input['targetKind'],
    'action': input['action'],
    'phase': reviewed == null ? 'PREVIEW' : 'CONFIRM',
    'planDigest': reviewed?.digest,
    'expiresAt': (reviewed?.expiresAt ?? end).toIso8601String(),
  });
  Future<void> _review(
    Map<String, dynamic> input,
    String agent,
    DateTime end, {
    CorrectableMemory? selected,
  }) async {
    identityChanged();
    if (!canReview || !_bind()) return;
    Map<String, dynamic> normalized;
    try {
      normalized = correctionInput(input);
    } catch (_) {
      error = '请检查填写内容和明确的活动类别。';
      _changed();
      return;
    }
    _invalidateField();
    _invalidateSelfReview();
    busy = true;
    error = null;
    message = null;
    final p = _journal(normalized, agent, end);
    final started = _ticks();
    _changed();
    try {
      await store.write(api.environment, _boundOwner!, p);
      if (!_current) return;
      _pending = p;
      if (!end.isAfter(_now())) {
        unknown = true;
        message = '原记忆已到期，未发送提交。请核实原操作。';
        return;
      }
      final raw = await api.request(
        'POST',
        'previews',
        _boundToken!,
        body: normalized,
      );
      if (!_current) return;
      final v = CorrectionPreview.read(raw, _boundOwner!);
      if (v.id != p.id ||
          v.agentID != agent ||
          _canonical(v.input) != _canonical(normalized) ||
          v.expiresAt.isAfter(end) ||
          !v.expiresAt.isAfter(_now()) ||
          selected != null &&
              _canonical(
                    v.memories.firstWhere((m) => m.id == selected.id).raw,
                  ) !=
                  _canonical(selected.raw)) {
        throw const FormatException('具体审阅版本已改变');
      }
      final received = _ticks();
      final wait = received - started;
      final remainingNative =
          v.expiresAt.difference(correctionTime(v.raw['observedAt'])) - wait;
      final remainingUTC = v.expiresAt.difference(_now());
      final remaining = remainingNative < remainingUTC
          ? remainingNative
          : remainingUTC;
      if (wait < Duration.zero || remaining <= Duration.zero) {
        throw const FormatException('原具体预览租期已过');
      }
      _reviewDeadline = received + remaining;
      preview = v;
      message = '这是具体版本的纠正草稿；尚未执行。请检查全部影响后确认。';
    } catch (_) {
      if (_current) {
        unknown = _pending != null;
        storageBlocked = _pending == null;
        if (unknown) {
          preview = null;
          memories = const [];
          candidates = const [];
        }
        error = storageBlocked ? '本机核实记录未安全保存，未发送提交。' : '结果尚未核实，请读取原操作；不会自动重发。';
      }
    } finally {
      if (_current) {
        busy = false;
        _changed();
      }
    }
  }

  void expireReview() {
    identityChanged();
    if (!_current || preview == null || _reviewTimeCurrent(preview!)) {
      return;
    }
    _invalidateField();
    _invalidateSelfReview();
    preview = null;
    memories = const [];
    candidates = const [];
    unknown = true;
    message = '具体预览已到期，已清除本机审阅材料。仅保留原操作引用，请读取原结果；不会自动提交。';
    _changed();
  }

  Future<void> confirm() async {
    expireReview();
    identityChanged();
    if (!canConfirm || !_current) return;
    _invalidateField();
    _invalidateSelfReview();
    final v = preview!,
        p = _journal(v.input, v.agentID, v.expiresAt, reviewed: v);
    busy = true;
    error = null;
    _changed();
    try {
      await store.write(api.environment, _boundOwner!, p);
      if (!_current) return;
      _pending = p;
      if (!_reviewTimeCurrent(v)) {
        unknown = true;
        preview = null;
        message = '具体预览已到期，未发送纠正。请核实原操作。';
        return;
      }
      final raw = await api.request(
        'POST',
        '${v.id}/confirm',
        _boundToken!,
        body: {'planDigest': v.digest},
      );
      if (!_current) return;
      final receipt = CorrectionReceipt.read(raw, _boundOwner!);
      if (!p.matches(receipt) || receipt.state != 'COMMITTED') {
        throw const FormatException('纠正回执不符');
      }
      await _publish(receipt, p);
    } catch (_) {
      if (_current) {
        unknown = true;
        preview = null;
        error = '纠正结果尚未核实，请读取原操作；不会自动重发。';
      }
    } finally {
      if (_current) {
        busy = false;
        _changed();
      }
    }
  }

  Future<void> _publish(CorrectionReceipt r, PendingMemoryCorrection p) async {
    if (!_current || _pending == null || !_pending!.same(p)) return;
    await store.delete(api.environment, _boundOwner!, p);
    if (!_current) return;
    _invalidateField();
    _invalidateSelfReview();
    _pending = null;
    unknown = false;
    preview = null;
    memories = const [];
    candidates = const [];
    storageBlocked = false;
    message = r.state == 'COMMITTED'
        ? (r.currentMatches
              ? '原纠正已完成，当前对应记忆仍与该回执一致。请重新读取当前记忆。'
              : '原纠正曾完成；此回执不代表当前记忆仍相同，请重新读取当前记忆。')
        : '原预览已失效，未执行纠正。可以重新读取当前记忆。';
  }

  Future<void> verify() async {
    identityChanged();
    if (busy || !_current || _pending == null) return;
    _invalidateField();
    _invalidateSelfReview();
    busy = true;
    error = null;
    preview = null;
    unknown = true;
    final p = _pending!;
    _changed();
    try {
      final raw = await api.request('GET', p.id, _boundToken!);
      if (!_current) return;
      final r = CorrectionReceipt.read(raw, _boundOwner!);
      if (!p.matches(r)) throw const FormatException('原操作引用不符');
      if (r.state == 'PENDING') {
        message = '原预览仍有效，尚未执行。旧审阅内容无法恢复；请在原截止时间过后核实，再重新检查。';
      } else {
        await _publish(r, p);
      }
    } catch (e) {
      if (_current) {
        error = e is CorrectionHTTPError && e.status == 404
            ? '尚未找到原操作；原请求可能仍在处理中。保留核实引用，请稍后再次读取，不会提交新操作。'
            : '仍无法核实原操作。不会恢复旧批准或自动提交。';
      }
    } finally {
      if (_current) {
        busy = false;
        _changed();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    _invalidateSelfReview();
    _invalidateField();
    api.dispose();
    super.dispose();
  }
}
