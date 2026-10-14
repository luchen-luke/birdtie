import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'activity_participation_operation.dart';
import 'activity_participation_pending_store.dart';
import 'entity_action_contract.dart';
import 'entity_share_pending_store.dart' show newEntityShareOperationID;

/// Human RSVP recovery. An unknown operation is read-only until separately closed.
class ParticipationOperationController extends ChangeNotifier {
  ParticipationOperationController({
    required this.activityID,
    required this.base,
    required this.client,
    required this.ownerID,
    required this.authorizationHeader,
    this.workspaceID,
    this.identityChanges,
    this.domainCurrent,
    ParticipationPendingStore? store,
    this.timeout = const Duration(seconds: 12),
    this.newOperationID = newEntityShareOperationID,
  }) : store = store ?? SecureParticipationPendingStore() {
    _identity = _now;
    identityChanges?.addListener(_identityChanged);
  }
  final String activityID, base;
  final http.Client client;
  final String? Function() ownerID, authorizationHeader;
  final String? Function()? workspaceID;
  final Listenable? identityChanges;
  final bool Function()? domainCurrent;
  final ParticipationPendingStore store;
  final Duration timeout;
  final String Function() newOperationID;
  String get _environment => participationEnvironment(base);
  late final (String?, String?, String?) _identity;
  (String?, String?, String?) get _now =>
      (ownerID(), authorizationHeader(), workspaceID?.call());
  bool _disposed = false, _retired = false;
  bool busy = false, initialized = false, storageBlocked = false;
  PendingParticipationOperation? pending;
  String? message;
  bool get current {
    if (_disposed || _retired) return false;
    if (_now != _identity || domainCurrent?.call() == false) {
      _retired = true;
      return false;
    }
    return true;
  }

  bool get _personal =>
      current &&
      _identity.$1 != null &&
      participationID(_identity.$1!) &&
      _identity.$2 != null &&
      _identity.$3 == null;
  bool get blocked => !initialized || storageBlocked || pending != null || busy;
  void _notify() {
    if (current) notifyListeners();
  }

  void _identityChanged() {
    if (_now != _identity) {
      _retired = true;
    }
  }

  Uri _url(
    PendingParticipationOperation p, [
    String suffix = '',
    bool execute = false,
  ]) => Uri.parse(
    '$_environment/v1/activities/${Uri.encodeComponent(activityID)}/participation-operations${execute ? '' : '/${p.operationID}'}$suffix',
  );
  Map<String, String> get _headers => {
    'Authorization': _identity.$2!,
    'Content-Type': 'application/json',
  };
  Future<void> initialize() async {
    if (!_personal || initialized || busy) return;
    busy = true;
    _notify();
    try {
      final all = await store.read(_environment, _identity.$1!);
      if (!current) return;
      final rows = all.where((x) => x.activityID == activityID).toList();
      if (rows.length > 1) throw const FormatException('原报名记录不唯一。');
      pending = rows.isEmpty ? null : rows.single;
      initialized = true;
    } catch (_) {
      if (current) {
        storageBlocked = true;
        message = '无法读取报名恢复记录，请保留原记录，暂不提交新操作。';
      }
    } finally {
      if (current) {
        busy = false;
        _notify();
      }
    }
    if (current && pending != null) await verify();
  }

  Future<void> submit(EntityActionDescriptor approved) async {
    if (!_personal || blocked) return;
    busy = true;
    message = null;
    _notify();
    final owner = _identity.$1!;
    try {
      final id = newOperationID();
      final p = PendingParticipationOperation(
        operationID: id,
        activityID: activityID,
        operation: approved.operation,
        requestDigest: participationRequestDigest(
          owner,
          activityID,
          id,
          approved,
        ),
      );
      // Persistence must finish before the only execute POST. Approval is never persisted.
      await store.write(_environment, owner, p);
      if (!current) return;
      pending = p;
      _notify();
      final response = await client
          .post(
            _url(p, '', true),
            headers: {
              ..._headers,
              ...approved.conditionHeaders,
              'X-Birdtie-Action-Until': participationDeadline(
                approved.reviewedUntil!,
              ),
            },
            body: jsonEncode({
              'operationId': p.operationID,
              'operation': p.operation,
            }),
          )
          .timeout(timeout);
      if (!current) return;
      if (response.statusCode != 200 && response.statusCode != 201) {
        throw StateError('unknown');
      }
      final receipt = ParticipationOperationReceipt.decode(
        participationEnvelope(utf8.decode(response.bodyBytes)),
        owner,
        p,
      );
      await _finish(
        p,
        receipt.committed ? '原操作已核实，请查看当前报名状态。' : '已停止原操作，请重新检查当前报名状态。',
      );
    } catch (_) {
      if (current) {
        if (pending == null) {
          storageBlocked = true;
          message = '未能保存报名恢复记录，未提交新操作。请重新打开后核对。';
        } else {
          message = '这次报名结果尚未核实。请核对原操作，暂不重复提交。';
        }
      }
    } finally {
      if (current) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _finish(PendingParticipationOperation p, String text) async {
    if (!current || pending != p) return;
    await store.delete(_environment, _identity.$1!, p);
    if (!current || pending != p) return;
    pending = null;
    message = text;
  }

  Future<void> verify() async {
    final p = pending;
    if (!_personal || busy || p == null) return;
    busy = true;
    _notify();
    try {
      final response = await client
          .get(_url(p, p.stopRequested ? '/closure' : ''), headers: _headers)
          .timeout(timeout);
      if (!current || pending != p) return;
      if (response.statusCode != 200) throw StateError('unknown');
      final raw = participationEnvelope(utf8.decode(response.bodyBytes));
      if (p.stopRequested) {
        ParticipationOperationClosure.decode(raw, _identity.$1!, p);
        await _finish(p, '已停止提交，原报名结果未核实。请查看当前报名状态。');
      } else {
        final receipt = ParticipationOperationReceipt.decode(
          raw,
          _identity.$1!,
          p,
        );
        await _finish(
          p,
          receipt.committed ? '原操作已核实，请查看当前报名状态。' : '原操作已停止，请查看当前报名状态。',
        );
      }
    } catch (_) {
      if (current) {
        message = p.stopRequested
            ? '停止提交尚未核实，请核对原停止记录；不会自动再次提交。'
            : '这次报名结果尚未核实。查不到记录也不能确定没有发生，请继续核对。';
      }
    } finally {
      if (current) {
        busy = false;
        _notify();
      }
    }
  }

  /// Called only after a fresh explicit human stop confirmation in the UI.
  Future<void> stopAfterConfirmation({
    required PendingParticipationOperation confirmedPending,
  }) async {
    final previous = pending;
    if (!_personal ||
        busy ||
        previous == null ||
        !identical(previous, confirmedPending)) {
      return;
    }
    busy = true;
    _notify();
    try {
      // A fresh human confirmation may retry the same close only. Restoring a
      // journal still performs GET only; this never retries the execute POST.
      final p = previous.stopRequested ? previous : previous.stopping();
      await store.write(_environment, _identity.$1!, p);
      if (!current || pending != previous) return;
      pending = p;
      final response = await client
          .post(
            _url(p, '/close'),
            headers: _headers,
            body: jsonEncode({
              'operation': p.operation,
              'requestDigest': p.requestDigest,
            }),
          )
          .timeout(timeout);
      if (!current || pending != p) return;
      if (response.statusCode != 200) throw StateError('unknown');
      ParticipationOperationClosure.decode(
        participationEnvelope(utf8.decode(response.bodyBytes)),
        _identity.$1!,
        p,
      );
      await _finish(p, '已停止提交，原报名结果未核实。请查看当前报名状态。');
    } catch (_) {
      if (current) message = '停止提交尚未核实，请核对原停止记录；不会自动再次提交。';
    } finally {
      if (current) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _disposed = true;
    identityChanges?.removeListener(_identityChanged);
    super.dispose();
  }
}
