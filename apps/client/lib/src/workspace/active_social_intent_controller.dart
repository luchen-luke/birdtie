import 'package:flutter/foundation.dart';
import 'active_social_intent_api.dart';
import 'model_egress_api.dart' show egressIDValid;

class ActiveIntentIdentity {
  const ActiveIntentIdentity(this.auth, this.owner, this.workspace);
  final String? auth, owner, workspace;
  bool get personal =>
      auth != null && egressIDValid(owner) && workspace == null;
  bool same(ActiveIntentIdentity b) =>
      auth == b.auth && owner == b.owner && workspace == b.workspace;
}

class ActiveSocialIntentController extends ChangeNotifier {
  ActiveSocialIntentController({
    required this.api,
    required this.identity,
    DateTime Function()? now,
  }) : now = now ?? DateTime.now,
       _bound = identity();
  final ActiveSocialIntentAPI api;
  final ActiveIntentIdentity Function() identity;
  final DateTime Function() now;
  ActiveIntentIdentity _bound;
  bool _closed = false, _retired = false;
  int _serial = 0, generation = 0;
  bool busy = false, unknown = false;
  String? message, selectedID, _agentID;
  ActiveIntentList? list;
  ActiveIntentOptions? options;
  ActiveSocialIntent? selected;
  ActiveIntentPreview? review;
  bool get current =>
      !_closed && !_retired && _bound.personal && _bound.same(identity());
  bool get ready => current && !busy && !unknown;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void sync() {
    final v = identity();
    if (_bound.same(v)) return;
    _retired = true;
    _bound = v;
    _serial++;
    generation++;
    busy = false;
    unknown = false;
    list = null;
    options = null;
    selected = null;
    selectedID = null;
    review = null;
    message = '账号或工作区已变化，请重新打开意图管理。';
    _notify();
  }

  bool _valid(int serial, ActiveIntentIdentity b) =>
      current && serial == _serial && _bound.same(b) && b.same(identity());
  Future<void> load() async {
    sync();
    if (!current) return;
    final b = _bound, s = ++_serial;
    final recovering = unknown;
    busy = true;
    review = null;
    _notify();
    try {
      final v = await api.list(b.auth!, b.owner!);
      if (!_valid(s, b)) return;
      final o = await api.options(b.auth!, b.owner!);
      if (!_valid(s, b)) return;
      if (v.agentID != o.agentID) throw const FormatException('当前 Agent 已变化');
      list = v;
      options = o;
      _agentID = v.agentID;
      selected = v.items.where((i) => i.id == selectedID).firstOrNull;
      unknown = false;
      message = recovering ? '已核对原意图当前状态，不能据此证明先前未知提交成功。' : null;
    } catch (_) {
      if (_valid(s, b)) {
        list = null;
        options = null;
        selected = null;
        message = '暂时无法读取意图，请重试。';
      }
    } finally {
      if (_valid(s, b)) {
        busy = false;
        _notify();
      }
    }
  }

  void select(String? id) {
    if (!ready) return;
    selected = list?.items.where((i) => i.id == id).firstOrNull;
    selectedID = selected?.id;
    review = null;
    _notify();
  }

  void reset() {
    if (!ready) return;
    selected = null;
    selectedID = null;
    review = null;
    message = '已清除本地选择；服务器意图未取消，对话与任务未清除。';
    _notify();
  }

  Future<ActiveIntentPreview?> prepare(
    String op, {
    Map<String, dynamic>? edit,
  }) async {
    sync();
    final original = selected;
    if (!ready || original == null || !original.editable(now())) return null;
    final b = _bound, s = ++_serial;
    busy = true;
    review = null;
    message = null;
    _notify();
    try {
      final p = await api.preview(b.auth!, b.owner!, original, op, edit);
      if (!_valid(s, b)) return null;
      if (p.agentID != _agentID || !p.expiresAt.isAfter(now())) {
        throw const FormatException('具体版本已失效');
      }
      review = p;
      return p;
    } catch (_) {
      if (_valid(s, b)) message = '当前版本或来源已变化，请刷新后重新检查。';
      return null;
    } finally {
      if (_valid(s, b)) {
        busy = false;
        _notify();
      }
    }
  }

  bool reviewCurrent(ActiveIntentPreview p, int epoch) =>
      ready &&
      generation == epoch &&
      identical(review, p) &&
      p.expiresAt.isAfter(now()) &&
      selectedID == p.before.id;
  void discardReview() {
    review = null;
    _notify();
  }

  void expire() {
    if (review != null && !review!.expiresAt.isAfter(now())) {
      review = null;
      message = '具体预览已到期，请重新检查。';
      _notify();
    }
  }

  Future<void> approve(ActiveIntentPreview p, int epoch) async {
    sync();
    if (!reviewCurrent(p, epoch)) return;
    final b = _bound, s = ++_serial;
    busy = true;
    review = null;
    _notify();
    try {
      final v = await api.approve(b.auth!, p);
      if (!_valid(s, b)) return;
      selected = v.item;
      selectedID = v.item.id;
      options = null;
      list = null;
      unknown = false;
      message = v.explanation;
    } on ActiveIntentHTTPError catch (e) {
      if (!_valid(s, b)) return;
      if ([400, 401, 403, 404, 409].contains(e.status)) {
        selected = null;
        options = null;
        list = null;
        message = '批准已失效，请刷新原意图后重新检查。';
      } else {
        unknown = true;
        message = '提交结果未知；请核对原意图，不要重复批准。';
      }
    } catch (_) {
      if (_valid(s, b)) {
        unknown = true;
        message = '提交结果未知；请核对原意图，不要重复批准。';
      }
    } finally {
      if (_valid(s, b)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> reconcile() async {
    sync();
    if (!current || busy || selectedID == null) return;
    final id = selectedID!, b = _bound, s = ++_serial;
    busy = true;
    review = null;
    _notify();
    try {
      final value = await api.read(b.auth!, b.owner!, id);
      if (!_valid(s, b)) return;
      selected = value;
      unknown = false;
      message = '已读取原意图当前状态；不能据此证明先前未知提交成功。';
    } catch (_) {
      if (_valid(s, b)) message = '暂时无法核对原意图，请稍后重试。';
    } finally {
      if (_valid(s, b)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    _serial++;
    review = null;
    api.dispose();
    super.dispose();
  }
}
