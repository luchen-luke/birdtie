import 'package:flutter/foundation.dart';
import 'agent_memory_candidate_api.dart';
import 'agent_multi_candidate_api.dart';
import 'agent_multi_candidate_pending_store.dart';
import 'enrichment_privacy_api.dart';

/// A local, concrete human review. This object is never native consent.
class EnrichmentPrivacyApproval {
  EnrichmentPrivacyApproval._(this.generation, this.serial, this.grant);
  final int generation, serial;
  final CandidatePurposeGrant grant;
}

class EnrichmentPrivacyController extends ChangeNotifier {
  EnrichmentPrivacyController({
    required this.api,
    required this.authorizationHeader,
    required this.ownerID,
    this.organizationWorkspaceID,
    this.current,
    this.pendingStore = const SecureAgentMultiCandidatePendingStore(),
    DateTime Function()? now,
  }) : now = now ?? (() => DateTime.now().toUtc());
  final EnrichmentPrivacyAPI api;
  final String? Function() authorizationHeader, ownerID;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  final AgentMultiCandidatePendingStore pendingStore;
  final DateTime Function() now;
  (String?, String?, String?)? _identity;
  int _generation = 0, _serial = 0;
  bool _closed = false, _retired = false;
  final _elapsed = Stopwatch();
  Duration _remaining = Duration.zero;
  EnrichmentPrivacyInventory? inventory;
  EnrichmentPrivacyApproval? approval;
  List<PendingMultiCandidateOperation> pending = const [];
  bool busy = false;
  String? error, notice;
  bool get retired => _retired;
  bool get personal =>
      !_closed &&
      !_retired &&
      current?.call() != false &&
      authorizationHeader() != null &&
      candidateIDValid(ownerID()) &&
      organizationWorkspaceID?.call() == null;
  bool get fresh =>
      personal &&
      inventory != null &&
      now().isBefore(inventory!.validUntil) &&
      _elapsed.elapsed < _remaining;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void synchronizeIdentity() {
    final next = (
      authorizationHeader(),
      ownerID(),
      organizationWorkspaceID?.call(),
    );
    if (_identity == null) {
      _identity = next;
      return;
    }
    if (_identity != next || current?.call() == false) {
      _retired = true;
      _generation++;
      _serial++;
      inventory = null;
      approval = null;
      pending = const [];
      busy = false;
      error = '身份或入口来源已变化，请返回设置重新打开。';
      notice = null;
      _elapsed.stop();
      _notify();
    }
  }

  bool _valid(int generation, int serial) {
    synchronizeIdentity();
    return personal && generation == _generation && serial == _serial;
  }

  String _message(Object e) => e is CandidateAPIError
      ? switch (e.status) {
          401 => '登录已失效，请重新登录。',
          403 => '当前本人身份或许可不可用。',
          409 => '许可版本已变化，请重新读取并检查。',
          404 => '未找到原许可，不能据此断言先前撤回未发生。',
          _ => '许可服务暂不可用，请稍后重新读取。',
        }
      : e is FormatException
      ? e.message
      : '读取未完成，请检查连接后核实。';
  Future<void> load() async {
    synchronizeIdentity();
    if (!personal || busy) return;
    final auth = authorizationHeader()!,
        owner = ownerID()!,
        g = _generation,
        s = ++_serial;
    busy = true;
    approval = null;
    error = null;
    _notify();
    try {
      final records = await pendingStore.read(api.environment, owner);
      if (!_valid(g, s)) return;
      pending = List.unmodifiable(records);
      final value = await api.list(auth, owner);
      if (!_valid(g, s)) return;
      final time = now();
      if (value.observedAt.isAfter(time) || !value.validUntil.isAfter(time)) {
        throw const FormatException('这份许可清单已过读取期限，请重新读取。');
      }
      inventory = value;
      _remaining = value.validUntil.difference(time);
      _elapsed
        ..reset()
        ..start();
      if (pending.isNotEmpty) notice = '发现待核实的原操作。本页只读取当前状态，不恢复旧批准或自动重发。';
    } catch (e) {
      if (_valid(g, s)) {
        inventory = null;
        error = _message(e);
      }
    } finally {
      if (_valid(g, s)) {
        busy = false;
        _notify();
      }
    }
  }

  EnrichmentPrivacyApproval? preview(CandidatePurposeGrant grant) {
    synchronizeIdentity();
    if (!fresh ||
        busy ||
        pending.isNotEmpty ||
        grant.revoked ||
        grant.revision >= 9007199254740991 ||
        inventory!.grants.where((g) => identical(g, grant)).isEmpty) {
      return null;
    }
    approval = EnrichmentPrivacyApproval._(_generation, _serial, grant);
    _notify();
    return approval;
  }

  void cancelPreview() {
    approval = null;
    _notify();
  }

  PendingMultiCandidateOperation _entry(
    CandidatePurposeGrant grant,
    String auth,
    String owner,
  ) => PendingMultiCandidateOperation({
    'environment': candidateRecoveryFingerprint(api.environment),
    'ownerId': owner,
    'agentId': grant.agentID,
    'sessionFingerprint': candidateRecoveryFingerprint(auth),
    'phase': 'revocation',
    'multi': false,
    'previewId': grant.previewID,
    'grantId': grant.id,
    'grantRevision': grant.revision,
    'selectionDigest': multiSelectionDigest(grant.selection, false),
    'reviewDigest': multiReviewDigest(null),
    'taskId': grant.selection['taskId'],
    'anchorEventId': null,
    'logicalOperationId': null,
    'expiresAt': grant.expiresAt.toIso8601String(),
    'sourceVersions': [
      {
        'momentId': grant.selection['momentId'],
        'revision': grant.selection['momentRevision'],
      },
    ],
  });
  Future<void> withdraw(EnrichmentPrivacyApproval captured) async {
    synchronizeIdentity();
    if (!fresh ||
        busy ||
        pending.isNotEmpty ||
        !identical(approval, captured) ||
        captured.generation != _generation ||
        captured.serial != _serial) {
      return;
    }
    final auth = authorizationHeader()!,
        owner = ownerID()!,
        g = _generation,
        s = _serial;
    final grant = captured.grant, entry = _entry(captured.grant, auth, owner);
    busy = true;
    approval = null;
    error = null;
    notice = null;
    _notify();
    bool dispatched = false;
    try {
      // Preserve the original durable bodyless recovery protocol. A failed
      // storage read/write cannot be skipped to send an untracked mutation.
      await pendingStore.write(api.environment, owner, entry);
      if (!_valid(g, s) || !fresh) return;
      pending = [entry];
      final native = await api.readGrant(auth, owner, grant);
      if (!_valid(g, s) || !fresh) return;
      if (native.revoked || native.revision != grant.revision) {
        notice = '当前许可状态或版本已变化。仅显示当前状态，不证明先前操作由本次请求完成。';
        return;
      }
      // Recheck after both storage and current by-ID read, immediately before
      // invoking the original writer with captured credentials and revision.
      if (!_valid(g, s) || !fresh) return;
      dispatched = true;
      await api.revoke(auth, owner, native);
      if (!_valid(g, s)) return;
      await pendingStore.delete(api.environment, owner, entry);
      if (!_valid(g, s)) return;
      pending = const [];
      notice = '已收到原接口的撤回结果。已经消费的内容不会因此被召回，独立保存的记忆未被删除。';
      inventory = null;
    } catch (e) {
      if (_valid(g, s)) {
        pending = [entry];
        error = dispatched
            ? '撤回结果未知。本页仅可核实原许可，不会自动重发。'
            : '撤回未取得完整结果，请核实原许可。本机记录未安全读写时不会提交。';
      }
    } finally {
      if (_valid(g, s)) {
        busy = false;
        _notify();
      }
    }
    if (_valid(g, s) && pending.isEmpty) await load();
  }

  Future<void> verifyPending(PendingMultiCandidateOperation entry) async {
    synchronizeIdentity();
    if (!personal || busy || !pending.any((v) => v.same(entry))) return;
    final auth = authorizationHeader()!,
        owner = ownerID()!,
        g = _generation,
        s = ++_serial;
    busy = true;
    approval = null;
    error = null;
    _notify();
    try {
      final value = await api.original.reconcile(auth, owner, entry);
      if (!_valid(g, s)) return;
      // Current state is not a causal receipt. Never clear an active/missing
      // request as NO_EFFECT or make it eligible for another automatic write.
      notice = value.state == 'REVOKED'
          ? '已核实原许可当前已撤回；这不能证明先前那次请求造成了撤回。'
          : '原操作当前状态：${value.state == 'NOT_REVOKED' ? '尚未撤回' : '仍需在原入口核实'}。不能据此断言先前请求没有生效。';
      if (value.state == 'REVOKED' && entry.data['phase'] == 'revocation') {
        await pendingStore.delete(api.environment, owner, entry);
        if (!_valid(g, s)) return;
        pending = List.unmodifiable(pending.where((v) => !v.same(entry)));
      }
    } catch (e) {
      if (_valid(g, s)) error = _message(e);
    } finally {
      if (_valid(g, s)) {
        busy = false;
        _notify();
      }
    }
  }

  void expire() {
    if (!fresh) {
      approval = null;
      _notify();
    }
  }

  @override
  void dispose() {
    _closed = true;
    _generation++;
    _serial++;
    _elapsed.stop();
    api.dispose();
    super.dispose();
  }
}
