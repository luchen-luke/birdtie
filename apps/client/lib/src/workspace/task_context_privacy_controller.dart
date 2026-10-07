import 'package:flutter/foundation.dart';
import 'agent_memory_candidate_api.dart' show candidateIDValid;
import 'task_context_privacy_api.dart';

class TaskContextPrivacyReview {
  TaskContextPrivacyReview._(this.grant, this.generation, this.serial);
  final TaskContextPrivacyGrant grant;
  final int generation, serial;
}

class TaskContextPrivacyController extends ChangeNotifier {
  TaskContextPrivacyController({
    required this.api,
    required this.authorizationHeader,
    required this.ownerID,
    this.organizationWorkspaceID,
    this.current,
    DateTime Function()? now,
  }) : now = now ?? (() => DateTime.now().toUtc()) {
    _identity = (
      authorizationHeader(),
      ownerID(),
      organizationWorkspaceID?.call(),
    );
  }
  final TaskContextPrivacyAPI api;
  final String? Function() authorizationHeader, ownerID;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  final DateTime Function() now;
  late final (String?, String?, String?) _identity;
  int _generation = 0, _serial = 0;
  bool _closed = false, _retired = false;
  bool busy = false;
  final _elapsed = Stopwatch();
  Duration _remaining = Duration.zero;
  TaskContextPrivacyInventory? inventory;
  TaskContextPrivacyReview? review;
  TaskContextPrivacyGrant? pending;
  String? pendingAgent;
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
    if (_closed || _retired) return;
    if (_identity !=
            (
              authorizationHeader(),
              ownerID(),
              organizationWorkspaceID?.call(),
            ) ||
        current?.call() == false) {
      _retired = true;
      _generation++;
      _serial++;
      inventory = null;
      review = null;
      pending = null;
      pendingAgent = null;
      busy = false;
      _elapsed.stop();
      notice = null;
      error = '身份或入口来源已变化，请返回设置重新打开。';
      _notify();
    }
  }

  bool _valid(int g, int s) {
    synchronizeIdentity();
    return personal && g == _generation && s == _serial;
  }

  String _message(Object e) => e is TaskContextPrivacyHTTPError
      ? switch (e.status) {
          401 => '本次登录已失效，请重新登录。',
          403 => '当前会话或许可不可用；请核对身份与权限。',
          404 => '原许可当前不可读取；这不证明先前撤回没有生效。',
          409 => '许可版本或期限已变化，请重新读取并检查。',
          _ => '任务资料许可暂不可用，请稍后重新读取。',
        }
      : '读取未取得有效结果，请核实当前状态。';
  Future<void> load() async {
    synchronizeIdentity();
    if (!personal || busy) return;
    final auth = authorizationHeader()!,
        owner = ownerID()!,
        g = _generation,
        s = ++_serial;
    busy = true;
    review = null;
    error = null;
    _notify();
    try {
      if (!_valid(g, s)) return;
      final v = await api.list(auth, owner);
      if (!_valid(g, s)) return;
      final at = now();
      if (v.observedAt.isAfter(at) || !v.validUntil.isAfter(at)) {
        throw const FormatException('许可清单已过读取期限。');
      }
      inventory = v;
      _remaining = v.validUntil.difference(at);
      _elapsed
        ..reset()
        ..start();
      final original = pending;
      if (original != null && pendingAgent == v.agentID) {
        for (final item in v.grants) {
          if (item.id == original.id &&
              item.sameScope(original) &&
              item.revoked &&
              item.revision > original.revision) {
            pending = null;
            pendingAgent = null;
            notice = '清单显示原许可当前已撤回；这不是先前请求的因果回执。';
            break;
          }
        }
      }
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

  TaskContextPrivacyReview? prepare(TaskContextPrivacyGrant grant) {
    synchronizeIdentity();
    if (!fresh ||
        busy ||
        pending != null ||
        grant.revoked ||
        grant.revision >= 9007199254740991 ||
        !inventory!.grants.any((g) => identical(g, grant))) {
      return null;
    }
    review = TaskContextPrivacyReview._(grant, _generation, _serial);
    _notify();
    return review;
  }

  void cancel() {
    review = null;
    _notify();
  }

  Future<void> withdraw(TaskContextPrivacyReview checked) async {
    synchronizeIdentity();
    if (!fresh ||
        busy ||
        pending != null ||
        !identical(review, checked) ||
        checked.generation != _generation ||
        checked.serial != _serial) {
      return;
    }
    final auth = authorizationHeader()!,
        g = _generation,
        s = _serial,
        agent = inventory!.agentID,
        grant = checked.grant;
    busy = true;
    review = null;
    error = null;
    notice = null;
    _notify();
    bool dispatched = false;
    try {
      if (!_valid(g, s) || !fresh) return;
      // The fresh current-session inventory is the human metadata review. The
      // legacy source-reading GET can deny a changed/expired Task; it must not be
      // a prerequisite for the original owner/session-only CAS revocation.
      pending = grant;
      pendingAgent = agent;
      dispatched = true;
      await api.revoke(auth, agent, grant, now);
      if (!_valid(g, s)) return;
      pending = null;
      pendingAgent = null;
      inventory = null;
      notice = '已收到撤回结果。已经读取的内容不会被召回，也未删除独立保存的记忆。';
    } catch (e) {
      if (_valid(g, s)) {
        error = dispatched ? '撤回结果暂未确认，请仅核实原许可；不会自动重发。' : _message(e);
      }
    } finally {
      if (_valid(g, s)) {
        busy = false;
        _notify();
      }
    }
    if (_valid(g, s) && pending == null && inventory == null) await load();
  }

  Future<void> verifyPending() async {
    synchronizeIdentity();
    final old = pending, agent = pendingAgent;
    if (!personal || busy || old == null || agent == null) return;
    final auth = authorizationHeader()!, g = _generation, s = ++_serial;
    busy = true;
    review = null;
    error = null;
    _notify();
    try {
      if (!_valid(g, s)) return;
      final v = await api.readGrant(auth, agent, old, now);
      if (!_valid(g, s)) return;
      if (v.revoked) {
        pending = null;
        pendingAgent = null;
        notice = '已核实原许可当前已撤回；不能据此证明先前那次请求造成了撤回。';
      } else {
        notice = '原许可当前尚未撤回；先前提交结果仍未确认，不会重复提交。';
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
      review = null;
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
