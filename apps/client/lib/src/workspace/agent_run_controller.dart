import 'dart:async';
import 'dart:math';
import 'package:flutter/foundation.dart';
import 'agent_run_api.dart';
import 'agent_run_pending_store.dart';
import 'agent_memory_candidate_api.dart' show candidateIDValid;

class AgentRunCancelReview {
  AgentRunCancelReview(this.record) : watch = Stopwatch()..start();
  final AgentRunRecord record;
  final Stopwatch watch;
  bool get fresh => watch.elapsed < const Duration(seconds: 30);
}

class AgentRunController extends ChangeNotifier {
  AgentRunController({
    required this.api,
    required this.authorizationHeader,
    required this.ownerID,
    this.organizationWorkspaceID,
    this.sourceCurrent,
    this.identityChanges,
    this.pendingStore = const SecureAgentRunPendingStore(),
  }) {
    _token = authorizationHeader();
    _owner = ownerID();
    identityChanges?.addListener(identityChanged);
  }
  final AgentRunAPI api;
  final String? Function() authorizationHeader, ownerID;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? sourceCurrent;
  final Listenable? identityChanges;
  final AgentRunPendingStore pendingStore;
  String? _token, _owner, _agent, nextCursor, error;
  String? notice;
  List<AgentRunRecord> items = const [];
  AgentRunRecord? selected;
  AgentRunCancelReview? review;
  PendingAgentRunCancel? pending;
  bool busy = false, _closed = false, _retired = false, _storeReady = false;
  int _epoch = 0;
  Timer? _expiry;
  bool get current {
    if (!_closed &&
        !_retired &&
        (_token == null ||
            !candidateIDValid(_owner) ||
            authorizationHeader() != _token ||
            ownerID() != _owner ||
            organizationWorkspaceID?.call() != null ||
            sourceCurrent?.call() == false)) {
      _retired = true;
      _epoch++;
      items = const [];
      selected = null;
      review = null;
      nextCursor = null;
      busy = false;
      _expiry?.cancel();
      error = '账号或来源已变化，请返回后重新打开。';
    }
    return !_closed && !_retired;
  }

  void identityChanged() {
    current;
    if (!_closed) notifyListeners();
  }

  bool _valid(int e) => current && e == _epoch;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void retire() {
    if (_closed || _retired) return;
    _retired = true;
    _epoch++;
    items = const [];
    selected = null;
    review = null;
    nextCursor = null;
    busy = false;
    _expiry?.cancel();
    error = '账号、智能体或来源已变化，请返回后重新打开。';
    _notify();
  }

  void _bindAgent(String a) {
    if (_agent != null && _agent != a) {
      retire();
      throw const FormatException('智能体身份已变化');
    }
    _agent = a;
  }

  Future<void> initialize() async {
    if (!current || busy) return;
    final e = _epoch;
    busy = true;
    _notify();
    try {
      pending = await pendingStore.read(api.base, _owner!);
      if (!_valid(e)) return;
      if (pending != null) {
        _agent = pending!.agentID;
        notice = '原停止操作结果待核实，当前不会重发。';
      }
      _storeReady = true;
    } catch (_) {
      if (_valid(e)) error = '无法读取核实记录，当前不会提交停止操作。';
    } finally {
      if (_valid(e)) {
        busy = false;
        _notify();
      }
    }
    if (_valid(e)) await load();
  }

  void _expireAt(DateTime at) {
    _expiry?.cancel();
    _expiry = Timer(at.difference(DateTime.now().toUtc()), () {
      if (current) {
        items = const [];
        selected = null;
        review = null;
        nextCursor = null;
        error = '状态读取已到期，请重新读取。';
        _notify();
      }
    });
  }

  void _failure(Object e) {
    if (e is AgentRunFailure && (e.status == 401 || e.status == 403)) {
      retire();
    } else {
      error = e is AgentRunFailure && e.status == 409
          ? '读取条件已变化，请重新读取第一页。'
          : '暂时无法读取处理状态，请重试。';
    }
  }

  Future<void> load({bool more = false}) async {
    if (!current || busy || (more && nextCursor == null)) return;
    final e = _epoch, after = more ? nextCursor : null;
    busy = true;
    review = null;
    error = null;
    _notify();
    if (!_valid(e)) return;
    try {
      final p = await api.page(_token!, _owner!, cursor: after);
      if (!_valid(e)) return;
      _bindAgent(p.agentID);
      final rows = <String, AgentRunRecord>{
        if (more)
          for (final r in items) r.id: r,
        for (final r in p.items) r.id: r,
      };
      items = List.unmodifiable(rows.values);
      nextCursor = p.nextCursor;
      _expireAt(p.expiresAt);
    } catch (x) {
      if (_valid(e)) {
        if (x is AgentRunFailure && x.status == 409) {
          nextCursor = null;
        }
        _failure(x);
      }
    } finally {
      if (_valid(e)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> select(String id) async {
    if (!current || busy) return;
    final e = _epoch;
    busy = true;
    review = null;
    error = null;
    _notify();
    if (!_valid(e)) return;
    try {
      final r = await api.read(_token!, _owner!, id, agent: _agent);
      if (!_valid(e)) return;
      _bindAgent(r.agentID);
      selected = r;
      _expireAt(DateTime.now().toUtc().add(const Duration(seconds: 30)));
    } catch (x) {
      if (_valid(e)) {
        selected = null;
        _failure(x);
      }
    } finally {
      if (_valid(e)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<AgentRunCancelReview?> prepareCancel() async {
    if (!current ||
        busy ||
        pending != null ||
        !_storeReady ||
        selected == null ||
        selected!.terminal)
      return null;
    final id = selected!.id;
    await select(id);
    if (!current ||
        busy ||
        selected == null ||
        selected!.id != id ||
        selected!.terminal)
      return null;
    review = AgentRunCancelReview(selected!);
    return review;
  }

  String _nonce() {
    final b = List.generate(16, (_) => Random.secure().nextInt(256));
    b[6] = (b[6] & 15) | 64;
    b[8] = (b[8] & 63) | 128;
    final s = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    return '${s.substring(0, 8)}-${s.substring(8, 12)}-${s.substring(12, 16)}-${s.substring(16, 20)}-${s.substring(20)}';
  }

  Future<bool> confirmCancel(AgentRunCancelReview checked) async {
    if (!current ||
        busy ||
        pending != null ||
        !_storeReady ||
        !identical(review, checked) ||
        !identical(selected, checked.record))
      return false;
    if (!checked.fresh) {
      review = null;
      error = '检查已到期，请重新读取后确认停止。';
      _notify();
      return false;
    }
    final e = _epoch,
        owner = _owner!,
        p = PendingAgentRunCancel(
          agentRunFingerprint(api.base),
          owner,
          checked.record.agentID,
          checked.record.id,
          checked.record.version,
          _nonce(),
        );
    busy = true;
    review = null;
    error = null;
    _notify();
    var sent = false, reserved = false;
    try {
      await pendingStore.reserve(api.base, owner, p);
      reserved = true;
      if (_valid(e)) pending = p;
      if (!_valid(e) || !checked.fresh) return false;
      sent = true;
      final r = await api.cancel(_token!, owner, checked.record);
      if (!_valid(e)) return false;
      if (r.state != 'CANCELLED' || r.version != p.version + 1)
        throw const FormatException('停止回执不符');
      await pendingStore.delete(api.base, owner, p);
      if (!_valid(e)) return false;
      pending = null;
      selected = r;
      items = List.unmodifiable(items.map((old) => old.id == r.id ? r : old));
      notice = '当前状态：已取消。原文字与已经形成的候选保留。';
      return true;
    } catch (x) {
      if (_valid(e)) {
        if (!reserved) {
          // Another controller may own this journal slot. Recover its actual
          // selector, never the unsent nonce of this losing confirmation.
          try {
            final original = await pendingStore.read(api.base, owner);
            if (_valid(e)) {
              pending = original;
              _storeReady = original != null;
              error = '请先核实原停止操作，当前没有提交新的停止请求。';
            }
          } catch (_) {
            if (_valid(e)) {
              _storeReady = false;
              error = '无法读取原操作记录，当前不会提交。';
            }
          }
        } else if (x is AgentRunFailure &&
            (x.status == 400 || x.status == 409)) {
          try {
            await pendingStore.delete(api.base, owner, p);
            if (_valid(e)) {
              pending = null;
              selected = null;
              error = '状态版本已变化，请重新读取并检查后决定。';
            }
          } catch (_) {
            error = '原操作记录仍待核实，当前不会重发。';
          }
        } else {
          error = '停止结果待核实，请读取原对象当前状态；不会自动重发。';
        }
      }
      return false;
    } finally {
      if (reserved && !sent) {
        try {
          await pendingStore.delete(api.base, owner, p);
          if (_valid(e)) {
            pending = null;
            error = '本次尚未提交，请重新读取后检查。';
          }
        } catch (_) {
          if (_valid(e)) {
            _storeReady = false;
            error = '无法核实本机记录，当前不会再次提交。';
          }
        }
      }
      if (_valid(e)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> recover() async {
    if (!current || busy || pending == null) return;
    final e = _epoch, p = pending!;
    busy = true;
    review = null;
    error = null;
    _notify();
    if (!_valid(e)) return;
    try {
      final r = await api.read(_token!, _owner!, p.runID, agent: p.agentID);
      if (!_valid(e)) return;
      selected = r;
      _expireAt(DateTime.now().toUtc().add(const Duration(seconds: 30)));
      notice = '当前状态：${agentRunStates[r.state]}。此读取不证明此前请求是该状态的原因。';
      if (r.version > p.version || r.terminal) {
        await pendingStore.delete(api.base, _owner!, p);
        if (!_valid(e)) return;
        pending = null;
        items = List.unmodifiable(items.map((old) => old.id == r.id ? r : old));
      } else {
        error = '原版本仍未确定，保留核实记录；当前不会再次提交。';
      }
    } catch (x) {
      if (_valid(e)) {
        selected = null;
        error = '尚未取得原对象状态，保留核实记录；不会重发。';
        if (x is AgentRunFailure && (x.status == 401 || x.status == 403))
          _failure(x);
      }
    } finally {
      if (_valid(e)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    identityChanges?.removeListener(identityChanged);
    _closed = true;
    _epoch++;
    _expiry?.cancel();
    super.dispose();
  }
}
