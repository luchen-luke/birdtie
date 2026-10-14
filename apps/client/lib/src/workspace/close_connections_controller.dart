import 'package:flutter/foundation.dart';
import 'close_connections_api.dart';

class CloseConnectionsIdentity {
  const CloseConnectionsIdentity(
    this.auth,
    this.owner,
    this.workspace, {
    this.sourceCurrent = true,
  });
  final String? auth, owner, workspace;
  final bool sourceCurrent;
  bool get personal =>
      auth != null &&
      auth!.isNotEmpty &&
      closePersonIDValid(owner) &&
      workspace == null &&
      sourceCurrent;
  bool same(CloseConnectionsIdentity v) =>
      auth == v.auth &&
      owner == v.owner &&
      workspace == v.workspace &&
      sourceCurrent == v.sourceCurrent;
}

/// Human-only audience management. Unknown writes are reconciled with GET;
/// neither a retry timer nor an identity change replays a write.
class CloseConnectionsController extends ChangeNotifier {
  CloseConnectionsController({
    required this.api,
    required this.identity,
    DateTime Function()? now,
  }) : now = now ?? DateTime.now,
       _bound = identity();
  final CloseConnectionsAPI api;
  final CloseConnectionsIdentity Function() identity;
  final DateTime Function() now;
  CloseConnectionsIdentity _bound;
  bool _closed = false, _retired = false;
  int _serial = 0, generation = 0;
  bool busy = false, unknown = false, readFailed = false;
  String? message;
  CloseConnectionsList? list;
  bool get current =>
      !_closed && !_retired && _bound.personal && _bound.same(identity());
  bool get stale => list?.validUntil.isAfter(now().toUtc()) != true;
  bool get ready =>
      current && !busy && !unknown && !readFailed && list != null && !stale;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void sync() {
    final next = identity();
    if (_bound.same(next)) return;
    _bound = next;
    _retired = true;
    generation++;
    _serial++;
    busy = false;
    unknown = false;
    readFailed = false;
    list = null;
    message = '账号或工作区已变化，请重新打开亲密好友管理。';
    _notify();
  }

  bool _valid(int s, CloseConnectionsIdentity who) =>
      current && s == _serial && _bound.same(who) && who.same(identity());
  Future<void> load() async {
    sync();
    if (!current || busy) return;
    final who = _bound, s = ++_serial, recovering = unknown;
    busy = true;
    message = null;
    _notify();
    try {
      final fresh = await api.list(who.auth!, who.owner!);
      if (!_valid(s, who)) return;
      list = fresh;
      readFailed = false;
      unknown = false;
      message = recovering ? '已核对当前名单；这不能证明先前未确认的提交成功。' : null;
    } catch (_) {
      if (_valid(s, who)) {
        readFailed = true;
        message = '暂时无法核对亲密好友，请重试。';
      }
    } finally {
      if (_valid(s, who)) {
        busy = false;
        _notify();
      }
    }
  }

  bool reviewCurrent(CloseConnection entry, int epoch, DateTime deadline) =>
      ready &&
      generation == epoch &&
      list!.validUntil == deadline &&
      list!.connections.any((v) => identical(v, entry));
  Future<void> confirm(
    CloseConnection entry, {
    required bool add,
    required int epoch,
    required DateTime deadline,
  }) async {
    sync();
    if (!reviewCurrent(entry, epoch, deadline) || add == entry.isClose) return;
    final who = _bound, s = ++_serial;
    busy = true;
    message = null;
    _notify();
    var responded = false;
    try {
      if (add) {
        await api.add(who.auth!, who.owner!, entry, deadline);
      } else {
        await api.remove(who.auth!, who.owner!, entry);
      }
      if (!_valid(s, who)) return;
      responded = true;
      final fresh = await api.list(who.auth!, who.owner!);
      if (!_valid(s, who)) return;
      list = fresh;
      unknown = false;
      readFailed = false;
      final target = fresh.connections
          .where((v) => v.personID == entry.personID)
          .firstOrNull;
      if (add &&
          (target?.isClose != true ||
              target?.tieID != entry.tieID ||
              target?.requestID != entry.requestID)) {
        message = '已收到操作回应，但当前关系已变化。请按最新名单重新检查。';
      } else if (!add && target?.isClose == true) {
        message = '已收到撤销回应，但这位好友当前仍在亲密好友名单中。请按最新状态重新检查。';
      } else {
        message = add ? '已核对，当前亲密好友名单已更新。' : '已核对，已撤销这位好友的亲密好友范围。';
      }
    } catch (error) {
      if (!_valid(s, who)) return;
      unknown = true;
      message = responded
          ? '操作已回应，当前名单尚未核实。请核对当前名单。'
          : error is CloseConnectionsException && error.status == 409
          ? '当前关系或资料版本已变化，请核对名单后重新检查。'
          : '提交结果尚未核实，请核对当前名单；不会自动重发。';
    } finally {
      if (_valid(s, who)) {
        busy = false;
        _notify();
      }
    }
  }

  void expire() {
    if (current && !busy && stale) _notify();
  }

  @override
  void dispose() {
    _closed = true;
    _serial++;
    list = null;
    api.dispose();
    super.dispose();
  }
}
