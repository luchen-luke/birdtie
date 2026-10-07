import 'connection_request_decision_operation.dart';
import 'package:flutter/foundation.dart';
import 'connections.dart';
import 'entity_share_pending_store.dart'
    show chatEntityUUID, newEntityShareOperationID;
import 'connection_request_review_pending_store.dart';

class ConnectionRequestApproval {
  ConnectionRequestApproval._(this.request, this.action, this.generation);
  final ContactRequest request;
  final String action;
  final int generation;
}

/// UI preview only. Original human Request/Session/ACL remains authoritative.
class ConnectionRequestReviewController extends ChangeNotifier {
  ConnectionRequestReviewController({
    required this.requestID,
    required this.source,
    required this.accountID,
    required this.authorizationHeader,
    this.organizationWorkspaceID,
    this.current,
    this.operationReceipts = false,
    DateTime Function()? now,
    ConnectionReviewPendingStore? pendingStore,
  }) : _now = now ?? DateTime.now,
       _pendingStore =
           pendingStore ?? const SecureConnectionReviewPendingStore() {
    _binding = _capture();
    _retired = !_usable(_binding);
  }
  final bool operationReceipts;
  final String requestID;
  final ConnectionSource source;
  final String? Function() accountID, authorizationHeader;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  final DateTime Function() _now;
  final ConnectionReviewPendingStore _pendingStore;
  late final (String?, String?, String?, String?, Object, String) _binding;
  bool _closed = false, _retired = false, _busy = false, _uncertain = false;
  int _generation = 0;
  ContactRequest? _request;
  ConnectionRequestApproval? _approval;
  ConnectionDecisionReceipt? _receipt;
  ConnectionRequestOperationReceipt? _operationReceipt;
  ConnectionRequestOperationReceipt? get operationReceipt => _operationReceipt;
  String? _message;
  bool _journalLoaded = false, _journalFailed = false;
  PendingConnectionReview? _pending;
  bool get retired => _retired;
  bool get busy => _busy;
  bool get uncertain => _uncertain;
  int get generation => _generation;
  ContactRequest? get request => _request;
  ConnectionDecisionReceipt? get receipt => _receipt;
  String? get message => _message;
  bool get recoveryBlocked => _journalFailed;
  PendingConnectionReview? get pendingReference => _pending;

  (String?, String?, String?, String?, Object, String) _capture() => (
    accountID(),
    authorizationHeader(),
    organizationWorkspaceID?.call(),
    source.authorizationHeader(),
    source.followClient,
    source.environment,
  );
  bool _usable((String?, String?, String?, String?, Object, String) b) =>
      b.$1 != null &&
      chatEntityUUID.hasMatch(b.$1!) &&
      chatEntityUUID.hasMatch(requestID) &&
      !source.isClosed &&
      b.$2 != null &&
      b.$3 == null &&
      b.$2 == b.$4 &&
      b.$6.isNotEmpty;
  bool _valid() {
    if (_closed || _retired) return false;
    final observed = _capture();
    final boundaryCurrent = current?.call() ?? true;
    // Getter/listener reentrancy must not revive a synchronously retired page.
    return !_closed &&
        !_retired &&
        boundaryCurrent &&
        observed == _binding &&
        _usable(_binding);
  }

  void synchronizeIdentity() {
    if (_closed || _retired) return;
    if (_valid()) return;
    _retired = true;
    ++_generation;
    _approval = null;
    _request = null;
    _receipt = null;
    _operationReceipt = null;
    _pending = null;
    _busy = false;
    _message = '身份或来源已变化，请返回当前收件箱重新打开。';
    notifyListeners();
  }

  ConnectionSource _wire() => ConnectionSource(
    authorizationHeader: () => _binding.$2,
    client: source.followClient,
    apiBaseUrl: _binding.$6,
  );
  void _changed() {
    if (!_closed) notifyListeners();
  }

  Future<bool> _restorePending(int generation) async {
    try {
      final value = await _pendingStore.read(
        _binding.$6,
        _binding.$1!,
        requestID,
      );
      if (value != null) {
        PendingConnectionReview.fromJson(value.toJson());
        if (value.ownerID != _binding.$1 || value.requestID != requestID) {
          throw const FormatException('申请恢复引用归属不符');
        }
      }
      if (!_valid() || generation != _generation) {
        synchronizeIdentity();
        return false;
      }
      _journalLoaded = true;
      _journalFailed = false;
      _pending = value ?? _pending;
      // Absence after a read is not a receipt for a previously unknown POST.
      _uncertain = _uncertain || value != null;
      return true;
    } catch (_) {
      if (_valid() && generation == _generation) {
        _journalFailed = true;
        _request = null;
        _message = '本机操作恢复记录无法读取，暂不能提交申请决定。请重试读取，不要重复提交。';
      } else {
        synchronizeIdentity();
      }
      return false;
    }
  }

  Future<bool> _clearPending(
    PendingConnectionReview value,
    int generation,
  ) async {
    if (!_valid() || generation != _generation) return false;
    try {
      final removed = await _pendingStore.compareDelete(_binding.$6, value);
      if (!_valid() || generation != _generation) {
        synchronizeIdentity();
        return false;
      }
      if (!removed) throw const FormatException('原恢复引用已变化');
      _pending = null;
      _uncertain = false;
      _journalFailed = false;
      return true;
    } catch (_) {
      if (_valid() && generation == _generation) {
        _uncertain = true;
        _journalFailed = true;
        _message = _receipt != null || _operationReceipt != null
            ? '已收到本次操作回执，但本机恢复记录尚未清理。暂不重复提交，请核实当前申请。'
            : '本机操作恢复记录尚未清理。暂不能再次提交，请核实当前申请。';
      } else {
        synchronizeIdentity();
      }
      return false;
    }
  }

  Future<void> load() async {
    synchronizeIdentity();
    if (!_valid() || _busy) return;
    _busy = true;
    _approval = null;
    final generation = ++_generation;
    _changed();
    final wire = _wire();
    try {
      if (!_valid()) {
        synchronizeIdentity();
        return;
      }
      if (!await _restorePending(generation)) return;
      // Explicitly recheck this caller's current generation after the await.
      if (!_valid() || generation != _generation) {
        synchronizeIdentity();
        return;
      }
      var receiptUnavailable=false;
      final pending = _pending;
      if (pending?.hasServiceOperation == true) {
        try {
          final receipt=await wire.readDecisionOperation(_binding.$1!,requestID,
            pending!.operationID!,pending.action,pending.scope);
          if (!_valid() || generation != _generation) {synchronizeIdentity();return;}
          _operationReceipt=receipt;
          if (!await _clearPending(pending,generation)) return;
          if (!_valid() || generation != _generation) return;
        } catch (_) {
          if (!_valid() || generation != _generation) {synchronizeIdentity();return;}
          receiptUnavailable=true;
        }
      }
      if (!_valid() || generation != _generation) {synchronizeIdentity();return;}
      final items = await wire.reviewRequests();
      if (!_valid() || generation != _generation) {
        synchronizeIdentity();
        return;
      }
      final matches = items.where((r) => r.id == requestID).toList();
      if (matches.length > 1) throw const FormatException('申请标识重复');
      _request = matches.isEmpty ? null : matches.single;
      _message = receiptUnavailable
          ? '原服务操作仍未核实；当前申请状态不是操作回执，不会重复提交。'
          : _operationReceipt != null
          ? _operationMessage(_operationReceipt!)
          : _uncertain
          ? '本次操作结果仍未确认。这里只显示当前申请状态，不是本次操作回执；不会重复提交。'
          : _receipt != null
          ? '已收到本次操作回执。当前列表已重新核实。'
          : _request == null
          ? '当前最近 100 条申请中未找到此项，不能据此认定已撤回或未生效。'
          : null;
    } catch (_) {
      if (_valid() && generation == _generation) {
        _request = null;
        _message = _receipt != null || _operationReceipt != null
            ? '已收到本次操作回执，但申请列表刷新失败，请稍后核实当前状态。'
            : _uncertain
            ? '暂时无法核实本次操作结果；不会重复提交，请稍后再次核实。'
            : '申请暂时无法读取，请重新核实。';
      } else {
        synchronizeIdentity();
      }
    } finally {
      wire.dispose();
      if (_valid() && generation == _generation) {
        _busy = false;
        _changed();
      }
    }
  }

  bool canAct(String action) {
    final r = _request;
    return _valid() &&
        !_busy &&
        _journalLoaded &&
        !_journalFailed &&
        !_uncertain &&
        _receipt == null &&
        _operationReceipt?.committed != true &&
        r != null &&
        r.state == 'pending' &&
        r.expiresAt?.isAfter(_now().toUtc()) == true &&
        (r.direction == 'incoming' &&
                const {'accept', 'decline'}.contains(action) ||
            r.direction == 'outgoing' && action == 'withdraw');
  }

  ConnectionRequestApproval? preview(String action) {
    synchronizeIdentity();
    if (!canAct(action)) return null;
    return _approval = ConnectionRequestApproval._(
      _request!,
      action,
      _generation,
    );
  }

  bool _same(ContactRequest a, ContactRequest b) =>
      a.id == b.id &&
      a.direction == b.direction &&
      a.otherAccountId == b.otherAccountId &&
      a.otherName == b.otherName &&
      a.note == b.note &&
      a.scope == b.scope &&
      a.state == b.state &&
      a.conversationId == b.conversationId &&
      a.policyDisposition == b.policyDisposition &&
      a.screeningStatus == b.screeningStatus &&
      a.expiresAt == b.expiresAt &&
      a.createdAt == b.createdAt;

  Future<void> submit(ConnectionRequestApproval approval) async {
    synchronizeIdentity();
    if (!_valid() ||
        !identical(_approval, approval) ||
        approval.generation != _generation ||
        !canAct(approval.action)) {
      return;
    }
    _approval = null;
    _operationReceipt = null;
    _busy = true;
    final generation = _generation;
    _changed();
    final wire = _wire();
    var posted = false;
    PendingConnectionReview? reference;
    var journalWritten = false;
    try {
      if (!_valid()) {
        synchronizeIdentity();
        return;
      }
      reference = PendingConnectionReview(
        ownerID: _binding.$1!,
        requestID: requestID,
        referenceID: newEntityShareOperationID(),
        operationID: operationReceipts ? newEntityShareOperationID() : null,
        requestDigest: operationReceipts ? connectionDecisionDigest(_binding.$1!,requestID,approval.action) : null,
        action: approval.action,
        scope: approval.request.scope,
        direction: approval.request.direction,
        observedAt: _now().toUtc(),
        requestCreatedAt: approval.request.createdAt!,
        requestExpiresAt: approval.request.expiresAt!,
      );
      _pending = reference;
      await _pendingStore.write(_binding.$6, reference);
      journalWritten = true;
      if (!_valid() || generation != _generation) {
        synchronizeIdentity();
        return;
      }
      // The original current Request is read after persistence, immediately
      // before the original decision writer. No old preview survives this wait.
      final rows = await wire.reviewRequests();
      if (!_valid() || generation != _generation) {
        synchronizeIdentity();
        return;
      }
      final rowsForID = rows.where((r) => r.id == requestID).toList();
      if (rowsForID.length != 1 ||
          !_same(approval.request, rowsForID.single) ||
          !rowsForID.single.expiresAt!.isAfter(_now().toUtc())) {
        _request = rowsForID.length == 1 ? rowsForID.single : null;
        if (!await _clearPending(reference, generation)) return;
        if (!_valid() || generation != _generation) return;
        ++_generation;
        _message = '申请已变化或过期，请重新核实并审阅，不沿用原确认。';
        return;
      }
      // Validate again after all synchronous getters/listeners, before wire.
      if (!_valid() || generation != _generation) {
        synchronizeIdentity();
        return;
      }
      posted = true;
      final ConnectionDecisionReceipt? receipt;
      final ConnectionRequestOperationReceipt? operation;
      if (operationReceipts) {
        operation=await wire.decideReviewedOperation(_binding.$1!,approval.request,
          approval.action,reference.operationID!);receipt=null;
      } else {
        receipt=await wire.decideReviewed(approval.request,approval.action);operation=null;
      }
      if (!_valid() || generation != _generation) {
        synchronizeIdentity();
        return;
      }
      _receipt = receipt;
      _operationReceipt = operation;
      if (!await _clearPending(reference, generation)) return;
      if (!_valid() || generation != _generation) return;
      _message = operation != null ? _operationMessage(operation) : switch (receipt!.state) {
        'accepted' =>
          approval.request.scope == 'friend'
              ? '已收到接受回执：申请已接受。好友关系由原服务处理；没有自动打开聊天或发送消息。'
              : '已收到接受回执：原联系申请已接受。没有自动打开对话或发送消息。',
        'declined' => '已收到拒绝回执：此申请已拒绝。',
        _ => '已收到撤回回执：此申请已撤回。',
      };
    } catch (e) {
      if (_valid() && generation == _generation) {
        // Status alone is not a rejection receipt. These two closed codes
        // originate before the original native transaction commits.
        final refused =
            !operationReceipts && e is ConnectionReviewException &&
            (e.status == 409 && e.errorCode == 'connection_conflict' ||
                e.status == 400 && e.errorCode == 'invalid_decision');
        if (journalWritten && reference != null && (!posted || refused)) {
          if (!await _clearPending(reference, generation)) return;
          if (!_valid() || generation != _generation) return;
        }
        if (posted && !refused) {
          _uncertain = true;
          _message = '本次操作结果未确认，请核实当前申请；不会重复提交。';
        } else if (!journalWritten && reference != null) {
          _uncertain = true;
          _journalFailed = true;
          _message = '本机恢复记录未能确认，请重试读取。申请决定尚未发出，暂不重复提交。';
        } else {
          _request = null;
          _message = e is ConnectionReviewException && e.status == 409
              ? '申请已变化，请重新读取并审阅。'
              : '此次操作未提交或已被拒绝，请重新核实申请。';
        }
      } else {
        synchronizeIdentity();
      }
    } finally {
      wire.dispose();
      if (_valid()) {
        _busy = false;
        _changed();
      }
    }
  }

  String _operationMessage(ConnectionRequestOperationReceipt receipt) => receipt.committed
      ? '已核实原服务操作回执：${switch(receipt.state){
          'accepted'=>'申请已接受','declined'=>'申请已拒绝',_=>'申请已撤回'}}。这是原操作历史，不代表当前好友或聊天权限；没有自动打开聊天或发送消息。'
      : '原服务确认本次操作未生效：${receipt.reason=='EXPIRED'?'发出时申请已过期':'发出时申请已被决定'}。如需操作，请先重新读取并审阅当前申请。';

  @override
  void dispose() {
    _closed = true;
    _approval = null;
    ++_generation;
    super.dispose();
  }
}
