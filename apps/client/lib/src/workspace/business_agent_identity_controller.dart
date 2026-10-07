import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'business_api.dart';
import 'business_agent_identity_model.dart';

/// Borrows Console transport and lifetime. Human confirmation cannot enable
/// Business runtime or turn the existence of an identity into a write receipt.
class BusinessAgentIdentityController extends ChangeNotifier {
  BusinessAgentIdentityController({
    required this.api,
    required this.businessID,
    required this.accountID,
    required this.authorizationHeader,
    required this.workspaceID,
    required this.currentBusinessID,
    required this.bindingCurrent,
    required this.sourceFrame,
    required this.identityChanges,
    DateTime Function()? now,
  }) : now = now ?? (() => DateTime.now().toUtc()) {
    _identity = _capture();
    identityChanges.addListener(synchronize);
  }
  final BusinessApi api;
  final String businessID;
  final String? Function() accountID,
      authorizationHeader,
      workspaceID,
      currentBusinessID;
  final bool Function() bindingCurrent;
  final Object? Function() sourceFrame;
  final Listenable identityChanges;
  final DateTime Function() now;
  late final Object _identity;
  bool _closed = false, _retired = false, _queued = false;
  int _serial = 0;
  bool busy = false, uncertain = false;
  BusinessAgentIdentity? value, review;
  String? error, message;
  Object _capture() => (
    accountID(),
    authorizationHeader(),
    api.authorizationHeader(),
    workspaceID(),
    currentBusinessID(),
    sourceFrame(),
    bindingCurrent(),
  );
  bool get permitted =>
      !_closed &&
      !_retired &&
      bindingCurrent() &&
      BusinessApi.validID(accountID() ?? '') &&
      (authorizationHeader()?.startsWith('Bearer ') ?? false) &&
      authorizationHeader() == api.authorizationHeader() &&
      workspaceID() == null &&
      currentBusinessID() == businessID;
  void _notify() {
    if (_closed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_queued) return;
      _queued = true;
      SchedulerBinding.instance.addPostFrameCallback((_) {
        _queued = false;
        if (!_closed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  void synchronize() {
    if (_closed || _retired) return;
    if (_capture() != _identity || !permitted) {
      _retired = true;
      ++_serial;
      busy = false;
      value = null;
      review = null;
      message = null;
      error = '工作身份、商家或资料来源已变化，请返回工作台重新进入。';
      _notify();
    }
  }

  bool _current(int n) {
    synchronize();
    return permitted && n == _serial;
  }

  void clearReview() {
    review = null;
    _notify();
  }

  Future<void> load() async {
    synchronize();
    if (!permitted || busy) return;
    final n = ++_serial;
    busy = true;
    review = null;
    error = null;
    message = null;
    _notify();
    if (!_current(n)) return;
    try {
      final fresh = await api.readAgentIdentity(businessID, now: now());
      if (!_current(n)) return;
      value = fresh;
      if (uncertain) {
        message = fresh.agentID == null
            ? '当前尚未读到身份，不能据此确认此前操作未生效，请继续核对。'
            : '已核实当前身份存在；这不是此前操作的结果回执。';
      }
    } on BusinessApiException catch (e) {
      if (_current(n)) {
        value = null;
        error = e.message;
      }
    } finally {
      if (_current(n)) {
        busy = false;
        _notify();
      }
    }
  }

  bool prepareReview() {
    synchronize();
    final v = value;
    if (!permitted || busy || uncertain || v == null || !v.eligible(now())) {
      return false;
    }
    review = v;
    _notify();
    return _current(_serial) && identical(review, v);
  }

  Future<void> establish(BusinessAgentIdentity approved) async {
    synchronize();
    if (!permitted ||
        busy ||
        uncertain ||
        !identical(review, approved) ||
        !identical(value, approved) ||
        !approved.eligible(now())) {
      // Expiry feedback belongs only to this still-current review. Other
      // invalid/retired states keep their original recovery and write guards.
      if (permitted &&
          !busy &&
          !uncertain &&
          identical(review, approved) &&
          identical(value, approved) &&
          approved.eligible(approved.observedAt) &&
          !approved.validUntil.isAfter(now())) {
        error = '本次未提交，读取快照已到期，请重新读取后检查。';
        message = null;
      }
      review = null;
      _notify();
      return;
    }
    final n = ++_serial;
    busy = true;
    review = null;
    error = null;
    message = null;
    _notify();
    if (!_current(n)) return;
    try {
      final fresh = await api.establishAgentIdentity(
        businessID,
        approved.claimVersion,
        now: now(),
      );
      if (!_current(n)) return;
      if (fresh.agentID == null) {
        throw const BusinessApiException(503, outcomeUnknown: true);
      }
      value = fresh;
      message = fresh.agentStatus == 'retired'
          ? '当前身份已退役，未重新启用。'
          : '身份已建立并保持暂停；未启用模型、资料读取或工具。';
    } on BusinessApiException catch (e) {
      if (_current(n)) {
        value = null;
        uncertain = e.outcomeUnknown;
        error = e.message;
      }
    } finally {
      if (_current(n)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    ++_serial;
    identityChanges.removeListener(synchronize);
    super.dispose();
  }
}
