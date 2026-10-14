import 'package:flutter/foundation.dart';

import 'agent_activity_join_api.dart';
import 'agent_activity_join_pending_store.dart';

/// Runtime review and explicit human action. A restored journal can only
/// reconcile or revoke its original approval; it never reconstructs this view.
class AgentActivityJoinController extends ChangeNotifier {
  AgentActivityJoinController({
    required String? Function() authorizationHeader,
    required String? Function() ownerID,
    String? Function()? organizationWorkspaceID,
    bool Function()? sourceCurrent,
    required this.api,
    this.selection,
    AgentActivityJoinPendingStore? pendingStore,
    DateTime Function()? now,
    Duration Function()? elapsed,
  }) : _authorization = authorizationHeader,
       _owner = ownerID,
       _organization = organizationWorkspaceID,
       _source = sourceCurrent,
       store = pendingStore ?? const SecureAgentActivityJoinPendingStore(),
       _now = now ?? (() => DateTime.now().toUtc()),
       _elapsedOverride = elapsed {
    _clock.start();
    _capturedToken = _authorization();
    _capturedOwner = _owner();
    _capturedOrganization = _organization?.call();
  }
  final AgentActivityJoinAPI api;
  final AgentActivityJoinPendingStore store;
  final AgentActivityJoinSelection? selection;
  final String? Function() _authorization, _owner;
  final String? Function()? _organization;
  final bool Function()? _source;
  final DateTime Function() _now;
  final Duration Function()? _elapsedOverride;
  final Stopwatch _clock = Stopwatch();
  late final String? _capturedToken, _capturedOwner, _capturedOrganization;
  Duration get _elapsed => _elapsedOverride?.call() ?? _clock.elapsed;
  AgentActivityJoinPreview? _preview;
  AgentActivityJoinReceipt? _receipt;
  PendingAgentActivityJoin? _pending;
  Duration? _reviewStarted;
  String? message;
  bool busy = false, initialized = false, unknown = false;
  bool _disposed = false, _identityRetired = false, _writeRetired = false;
  int _epoch = 0;
  AgentActivityJoinPreview? get preview => _preview;
  AgentActivityJoinReceipt? get receipt => _receipt;
  PendingAgentActivityJoin? get pending => _pending;
  bool get hasPending => _pending != null;
  bool get identityRetired => _identityRetired;
  bool get writeRetired => _writeRetired;
  bool get _identityCurrent =>
      !_disposed &&
      !_identityRetired &&
      _capturedToken != null &&
      _capturedToken.isNotEmpty &&
      activityJoinID(_capturedOwner) &&
      _capturedOrganization == null &&
      _authorization() == _capturedToken &&
      _owner() == _capturedOwner &&
      _organization?.call() == _capturedOrganization;
  bool get canRecover => initialized && _identityCurrent && !busy && hasPending;
  bool get canPrepare =>
      initialized &&
      _identityCurrent &&
      !busy &&
      !_writeRetired &&
      !hasPending &&
      _preview == null &&
      selection?.valid == true &&
      (_source?.call() ?? true);
  bool get _reviewCurrent {
    final p = _preview, start = _reviewStarted;
    if (!_identityCurrent ||
        _writeRetired ||
        !(_source?.call() ?? true) ||
        p == null ||
        start == null ||
        !p.freshState ||
        p.review == null ||
        !p.expiresAt.isAfter(_now().toUtc()))
      return false;
    final spent = _elapsed - start;
    return !spent.isNegative && spent < p.expiresAt.difference(p.observedAt);
  }

  bool get canConfirm =>
      !busy &&
      !hasPending &&
      _preview?.state == agentActivityJoinPending &&
      _reviewCurrent;
  bool get canStop =>
      initialized &&
      _identityCurrent &&
      !busy &&
      (hasPending || _preview != null);
  Duration get reviewRemaining {
    final p = _preview, start = _reviewStarted;
    if (p == null || start == null || !_reviewCurrent) return Duration.zero;
    final wall = p.expiresAt.difference(_now().toUtc());
    final monotonic = p.expiresAt.difference(p.observedAt) - (_elapsed - start);
    return wall < monotonic ? wall : monotonic;
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void identityChanged() {
    if (_disposed || _identityRetired) return;
    if (_authorization() != _capturedToken ||
        _owner() != _capturedOwner ||
        _organization?.call() != _capturedOrganization) {
      retireIdentity();
    } else if (!(_source?.call() ?? true)) {
      retireApproval('原查询来源已变化，本次批准已停用；原操作仍可核实。');
    } else {
      expireReview();
    }
  }

  void retireIdentity() {
    if (_disposed || _identityRetired) return;
    _identityRetired = true;
    _writeRetired = true;
    _epoch++;
    _preview = null;
    _receipt = null;
    _pending = null;
    _reviewStarted = null;
    unknown = false;
    message = '当前身份或服务来源已变化，请返回后以本人身份核实原报名。';
    _notify();
  }

  void retireApproval([String reason = '本次批准已停用；原操作仍可核实。']) {
    if (_disposed || _writeRetired) return;
    _writeRetired = true;
    _epoch++;
    _preview = null;
    _receipt = null;
    _reviewStarted = null;
    message = reason;
    _notify();
  }

  void expireReview() {
    if (_preview != null &&
        !_reviewCurrent &&
        !_writeRetired &&
        !_identityRetired) {
      retireApproval('报名预览已到期，本次批准已停用；请核实或结束原操作。');
    }
  }

  bool _same(int epoch) {
    identityChanged();
    return _identityCurrent && epoch == _epoch;
  }

  String get _environment => api.environment;
  String get _token => _capturedToken!;
  String get _ownerID => _capturedOwner!;
  void _failure(Object e, {bool pending = false}) {
    if (_identityRetired || _disposed) return;
    if (pending || hasPending) {
      unknown = true;
      _preview = null;
      _reviewStarted = null;
      _writeRetired = true;
      message = '原智能体报名结果尚未核实。请核实原操作，或结束并核实原批准；不会再次提交。';
    } else if (e is AgentActivityJoinFailure && e.status == 401) {
      message = '请以本人账号登录后重新打开本次活动。';
    } else if (e is AgentActivityJoinFailure && e.status == 403) {
      message = '当前本人身份或授权不能准备智能体报名。可返回活动查看普通报名。';
    } else if (e is AgentActivityJoinFailure && e.status == 409) {
      _writeRetired = true;
      message = e.code == 'activity_full'
          ? '活动名额已变化，请返回活动重新查看。'
          : '原查询、活动或授权已变化，请返回Now重新查看。';
    } else {
      message = '当前无法准备智能体报名。请稍后重试，或返回活动查看普通报名。';
    }
  }

  Future<void> initialize() async {
    if (busy || initialized || _disposed) return;
    identityChanged();
    if (!_identityCurrent) {
      message = _capturedOrganization != null
          ? '请切换到本人身份查看智能体报名。'
          : '请先以本人账号登录查看智能体报名。';
      initialized = true;
      _notify();
      return;
    }
    busy = true;
    _notify();
    final epoch = _epoch;
    try {
      final p = await store.read(_environment, _ownerID);
      if (!_same(epoch)) return;
      _pending = p;
      unknown = p != null;
      if (p != null) {
        _writeRetired = true;
        message = '有一项原智能体报名待核实。恢复记录不提供新的报名批准。';
      } else if (selection == null) {
        message = '暂无待核实的智能体报名。请在Now当前活动结果中准备报名。';
      }
    } catch (e) {
      if (_same(epoch)) {
        _writeRetired = true;
        unknown = true;
        message = '无法读取本机报名核实记录。请保留记录并重试读取。';
      }
    } finally {
      if (!_disposed) {
        initialized = true;
        busy = false;
        _notify();
      }
    }
  }

  Future<void> reloadJournal() async {
    if (busy || !_identityCurrent || hasPending) return;
    initialized = false;
    await initialize();
  }

  Future<void> prepare() async {
    identityChanged();
    if (!canPrepare) return;
    busy = true;
    message = null;
    _preview = null;
    _receipt = null;
    final epoch = _epoch, started = _elapsed;
    _notify();
    try {
      // A competing controller may have reserved after this page initialized.
      final retained = await store.read(_environment, _ownerID);
      if (!_same(epoch)) return;
      if (retained != null) {
        _pending = retained;
        unknown = true;
        _writeRetired = true;
        message = '原智能体报名需要先核实；不会准备另一项报名。';
        return;
      }
      final p = await api.preview(
        _token,
        _ownerID,
        selection!,
        activityJoinUUID(),
        activityJoinUUID(),
      );
      if (!_same(epoch)) return;
      if (p.observedAt.isAfter(
        _now().toUtc().add(const Duration(seconds: 1)),
      )) {
        throw const FormatException('报名预览时间不符。');
      }
      _preview = p;
      _reviewStarted = started;
      if (!_reviewCurrent) {
        retireApproval('报名预览已到期，请返回Now重新查看活动。');
        return;
      }
      message = '请检查以下原活动内容。只有你明确确认后，智能体才会提交这一次报名。';
    } catch (e) {
      if (_same(epoch)) _failure(e);
    } finally {
      if (!_disposed) {
        busy = false;
        _notify();
      }
    }
  }

  PendingAgentActivityJoin _selector(AgentActivityJoinPreview p, String phase) {
    final s = selection!;
    return PendingAgentActivityJoin(
      environmentDigest: activityJoinFingerprint(_environment),
      ownerID: _ownerID,
      agentID: p.agentID,
      taskID: p.taskID,
      activityID: p.activityID,
      approvalID: p.approvalID,
      logicalOperationID: p.logicalOperationID,
      actionID: p.actionID,
      bindingDigest: p.bindingDigest,
      expectedTaskUpdatedAt: s.expectedTaskUpdatedAt.toUtc().toIso8601String(),
      replyTurnDigest: s.replyTurnDigest,
      phase: phase,
    );
  }

  Future<PendingAgentActivityJoin> _readExact(
    PendingAgentActivityJoin expected,
  ) async {
    final p = await store.read(_environment, _ownerID);
    if (p == null || !p.same(expected)) throw StateError('原智能体报名核实记录已变化。');
    return p;
  }

  Future<void> confirmAndExecute() async {
    identityChanged();
    if (!canConfirm) return;
    final original = _preview!, epoch = _epoch;
    final proposed = _selector(original, 'CONFIRM');
    _pending = proposed;
    busy = true;
    message = '正在保留原操作并核对本次批准…';
    _notify();
    try {
      final reserved = await store.reserve(_environment, _ownerID, proposed);
      if (!_same(epoch)) return;
      if (!reserved) {
        _pending = await store.read(_environment, _ownerID);
        if (!_same(epoch)) return;
        _writeRetired = true;
        _preview = null;
        _reviewStarted = null;
        unknown = true;
        message = '已有原智能体报名操作；请核实原记录，不会再次确认或提交。';
        return;
      }
      _pending = proposed;
      await _readExact(proposed);
      if (!_same(epoch) || !_reviewCurrent) {
        _failure(StateError('expired'), pending: true);
        return;
      }
      final approved = await api.confirm(_token, _ownerID, original);
      if (!_same(epoch) || !_reviewCurrent) return;
      _preview = approved;
      final executing = proposed.withPhase('EXECUTE');
      await store.replace(_environment, _ownerID, proposed, executing);
      if (!_same(epoch)) return;
      _pending = executing;
      await _readExact(executing);
      if (!_same(epoch) || !_reviewCurrent) {
        _failure(StateError('expired'), pending: true);
        return;
      }
      message = '正在提交这一次智能体报名…';
      _notify();
      final r = await api.execute(
        _token,
        _ownerID,
        executing.approvalID,
        executing.bindingDigest,
        executing.agentID,
      );
      if (!_same(epoch)) return;
      await _acceptReceipt(r, executing, epoch);
    } catch (e) {
      if (_same(epoch)) {
        // Even a failed storage acknowledgement may have retained the selector.
        _pending ??= proposed;
        _failure(e, pending: true);
      }
    } finally {
      if (!_disposed) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _acceptReceipt(
    AgentActivityJoinReceipt r,
    PendingAgentActivityJoin p,
    int epoch,
  ) async {
    if (r.approvalID != p.approvalID ||
        r.agentID != p.agentID ||
        _receipt != null && _receipt!.dispatchID != r.dispatchID) {
      throw const FormatException('原智能体报名因果回执不符。');
    }
    _receipt = r;
    _preview = null;
    _reviewStarted = null;
    _writeRetired = true;
    if (!r.terminal) {
      unknown = true;
      message = '原操作已提交，报名结果仍待核实；不会再次提交。';
      return;
    }
    unknown = false;
    message = r.status == 'SUCCEEDED'
        ? '原报名操作已生效，请在原活动记录查看当前状态。'
        : '这一次原操作已核实，没有产生智能体报名效果；不会取消其他已有报名。';
    try {
      await store.delete(_environment, _ownerID, p);
      if (!_same(epoch)) return;
      _pending = null;
    } catch (_) {
      if (_same(epoch)) {
        _pending = p;
        message = '${message!} 本机核实记录尚未清除，请再次核实原记录。';
      }
    }
  }

  Future<void> reconcile() async {
    identityChanged();
    if (!canRecover) return;
    busy = true;
    final epoch = _epoch;
    _notify();
    try {
      final local = _pending!;
      final stored = await store.read(_environment, _ownerID);
      if (!_same(epoch)) return;
      if (stored != null && !stored.sameBinding(local))
        throw StateError('原记录已变化。');
      final p = stored ?? local;
      _pending = p;
      final r = await api.reconcile(_token, _ownerID, p.approvalID, p.agentID);
      if (!_same(epoch)) return;
      await _acceptReceipt(r, p, epoch);
    } catch (e) {
      if (_same(epoch)) _failure(e, pending: true);
    } finally {
      if (!_disposed) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> stop() async {
    identityChanged();
    if (!canStop) return;
    final epoch = _epoch, p = _pending ?? _selector(_preview!, 'STOP');
    busy = true;
    _notify();
    try {
      final stored = await store.read(_environment, _ownerID);
      if (!_same(epoch)) return;
      if (stored == null) {
        if (!await store.reserve(_environment, _ownerID, p))
          throw StateError('原记录已有归属。');
      } else if (!stored.sameBinding(p)) {
        _pending = stored;
        throw StateError('请先核实原记录。');
      }
      if (!_same(epoch)) return;
      final actual = await store.read(_environment, _ownerID);
      if (!_same(epoch)) return;
      if (actual == null || !actual.sameBinding(p)) throw StateError('原记录未保存。');
      final stopping = actual.withPhase('STOP');
      if (!actual.same(stopping))
        await store.replace(_environment, _ownerID, actual, stopping);
      _pending = stopping;
      await _readExact(stopping);
      if (!_same(epoch)) return;
      // Revoke is a fenced stop, never cancellation of a participation.
      final cancelled = await api.revoke(
        _token,
        _ownerID,
        stopping.approvalID,
        stopping.agentID,
      );
      if (!_same(epoch)) return;
      if (!stopping.matches(cancelled))
        throw const FormatException('原批准停止回执不符。');
      _preview = null;
      _reviewStarted = null;
      _writeRetired = true;
      if (cancelled.dispatchCommitted) {
        final r = await api.reconcile(
          _token,
          _ownerID,
          stopping.approvalID,
          stopping.agentID,
        );
        if (!_same(epoch)) return;
        await _acceptReceipt(r, stopping, epoch);
      } else {
        await store.delete(_environment, _ownerID, stopping);
        if (!_same(epoch)) return;
        _pending = null;
        _receipt = null;
        unknown = false;
        message = '原批准已结束，未由这次批准提交智能体报名。';
      }
    } catch (e) {
      if (_same(epoch)) {
        _pending ??= p;
        _failure(e, pending: true);
      }
    } finally {
      if (!_disposed) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _disposed = true;
    _epoch++;
    _clock.stop();
    _preview = null;
    _receipt = null;
    _pending = null;
    api.dispose();
    super.dispose();
  }
}
