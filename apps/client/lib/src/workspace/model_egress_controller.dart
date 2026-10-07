import 'package:flutter/foundation.dart';
import 'model_egress_api.dart';

class ModelEgressController extends ChangeNotifier {
  ModelEgressController({
    required this.api,
    required this.authorizationHeader,
    required this.accountID,
    required this.organizationWorkspaceID,
  });
  final ModelEgressAPI api;
  final String? Function() authorizationHeader,
      accountID,
      organizationWorkspaceID;
  (String?, String?, String?)? _identity;
  int generation = 0, _serial = 0;
  bool busy = false, _closed = false, unknown = false, unknownPreview = false;
  String? message;
  List<EgressOption> options = const [];
  List<EgressReceipt> receipts = const [];
  List<EgressBudget> budgets = const [];
  EgressOption? selected;
  EgressPreview? preview;
  EgressReceipt? current;
  String? pendingID;
  DateTime? _pendingPreviewDeadline;
  bool get personal =>
      authorizationHeader() != null &&
      egressIDValid(accountID()) &&
      organizationWorkspaceID() == null;
  void synchronizeIdentity() {
    final value = (
      authorizationHeader(),
      accountID(),
      organizationWorkspaceID(),
    );
    if (value == _identity) return;
    _identity = value;
    ++generation;
    ++_serial;
    busy = false;
    unknown = false;
    unknownPreview = false;
    message = null;
    options = const [];
    receipts = const [];
    budgets = const [];
    selected = null;
    preview = null;
    current = null;
    pendingID = null;
    _pendingPreviewDeadline = null;
  }

  bool _valid(int serial, int g) =>
      !_closed &&
      personal &&
      serial == _serial &&
      g == generation &&
      _identity ==
          (authorizationHeader(), accountID(), organizationWorkspaceID());
  Future<void> load() async {
    synchronizeIdentity();
    if (!personal || busy || _closed) {
      if (!_closed) notifyListeners();
      return;
    }
    final serial = ++_serial,
        g = generation,
        token = authorizationHeader()!,
        owner = accountID()!;
    busy = true;
    message = null;
    notifyListeners();
    try {
      final values = await Future.wait([
        api.options(token, owner),
        api.receipts(token, owner),
      ]);
      if (!_valid(serial, g)) return;
      options = values[0] as List<EgressOption>;
      receipts = values[1] as List<EgressReceipt>;
      if (selected != null && !options.any((v) => v.key == selected!.key)) {
        selected = null;
        preview = null;
        budgets = const [];
      }
    } catch (_) {
      if (_valid(serial, g)) message = '暂时无法核实配置和记录，请刷新。没有执行模型请求。';
    } finally {
      if (_valid(serial, g)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  Future<void> choose(EgressOption o) async {
    synchronizeIdentity();
    if (!personal ||
        busy ||
        unknown ||
        unknownPreview ||
        _closed ||
        !options.any((v) => v.key == o.key)) {
      return;
    }
    selected = o;
    preview = null;
    current = null;
    pendingID = null;
    budgets = const [];
    message = null;
    final serial = ++_serial, g = generation, token = authorizationHeader()!;
    busy = true;
    notifyListeners();
    try {
      final v = await api.budgets(token, o.rootID, o.taskID);
      if (_valid(serial, g)) budgets = v;
    } catch (_) {
      if (_valid(serial, g)) message = '预算无法核实，请刷新后重选；不会创建或增加额度。';
    } finally {
      if (_valid(serial, g)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  Future<void> prepare() async {
    synchronizeIdentity();
    final o = selected;
    if (!personal ||
        o == null ||
        budgets.length != 4 ||
        busy ||
        unknown ||
        unknownPreview ||
        _closed) {
      return;
    }
    final serial = ++_serial,
        g = generation,
        token = authorizationHeader()!,
        owner = accountID()!;
    var deadline = DateTime.now().toUtc().add(const Duration(seconds: 90));
    if (o.deadline.isBefore(deadline)) deadline = o.deadline;
    // Round to PostgreSQL microseconds to keep exact replay selectors stable.
    if (!deadline.isAfter(DateTime.now().toUtc())) {
      message = '选择已过期，请刷新后重新核实。';
      notifyListeners();
      return;
    }
    busy = true;
    preview = null;
    current = null;
    message = null;
    _pendingPreviewDeadline = deadline;
    notifyListeners();
    try {
      final p = await api.preview(
        token,
        owner,
        o,
        o.maxOutput < 64 ? o.maxOutput : 64,
        deadline,
      );
      if (!_valid(serial, g)) return;
      preview = p;
      pendingID = p.id;
    } catch (_) {
      if (_valid(serial, g)) {
        unknownPreview = true;
        message = '预览创建结果尚未核实。请查看原记录；不要重复创建或猜测已批准。';
      }
    } finally {
      if (_valid(serial, g)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  void cancelReview() {
    synchronizeIdentity();
    if (busy || unknown || _closed) return;
    preview = null;
    message = '已退出检查，没有批准或执行模型请求。';
    notifyListeners();
  }

  Future<void> inspect(String id) async {
    synchronizeIdentity();
    if (unknown && pendingID != id) {
      message = '请先核实本次提交的同一原记录。';
      notifyListeners();
      return;
    }
    if (!personal || busy || _closed) return;
    final serial = ++_serial,
        g = generation,
        token = authorizationHeader()!,
        owner = accountID()!;
    busy = true;
    preview = null;
    message = null;
    notifyListeners();
    try {
      final r = await api.receipt(token, owner, id);
      if (!_valid(serial, g)) return;
      current = r;
      pendingID = id;
      unknown = false;
      if (unknownPreview) {
        bool matches(EgressReceipt v) =>
            selected != null &&
            v.data['rootTraceId'] == selected!.rootID &&
            v.data['taskId'] == selected!.taskID &&
            v.data['priceVersion'] == selected!.data['priceVersion'] &&
            egressTime(v.data['expiresAt']) == _pendingPreviewDeadline;
        if (matches(r) && receipts.where(matches).length == 1) {
          unknownPreview = false;
        }
      }
      preview = unknownPreview ? null : r.review;
      message = unknownPreview
          ? '已读取这项记录；原预览创建仍无法唯一核实，不重新创建。'
          : r.status == 'APPROVED'
          ? '已核实本地批准；模型请求没有执行。'
          : r.status == 'REVOKED'
          ? '已核实撤回；旧批准不可恢复。'
          : r.approvable
          ? '原预览仍可检查；是否批准由你决定。'
          : '原记录已失效或原会话不同；可撤回，不能沿用批准。';
    } catch (_) {
      if (_valid(serial, g)) message = '原记录暂时无法核实。保持未知，不重新创建或提交。';
    } finally {
      if (_valid(serial, g)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  Future<void> approve() async {
    synchronizeIdentity();
    final p = preview;
    if (!personal ||
        p == null ||
        busy ||
        unknown ||
        unknownPreview ||
        _closed ||
        p.data['status'] != 'DRAFT' ||
        (current != null && !current!.approvable)) {
      return;
    }
    if (!p.expiry.isAfter(DateTime.now().toUtc())) {
      preview = null;
      message = '具体预览已过期，请重新核实原记录。';
      notifyListeners();
      return;
    }
    final serial = ++_serial, g = generation, token = authorizationHeader()!;
    busy = true;
    pendingID = p.id;
    message = null;
    notifyListeners();
    try {
      await api.approve(token, p);
      if (!_valid(serial, g)) return;
      preview = null;
      current = null;
      message = '此版本的本地许可已确认。没有预留费用或执行模型请求。';
    } catch (_) {
      if (_valid(serial, g)) {
        unknown = true;
        preview = null;
        message = '批准结果未知，请核实原记录；不要重新批准或创建。';
      }
    } finally {
      if (_valid(serial, g)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  Future<void> revoke(String id) async {
    synchronizeIdentity();
    if (unknown && pendingID != id) {
      message = '请先核实本次提交的同一原记录。';
      notifyListeners();
      return;
    }
    if (!personal || busy || _closed) return;
    final serial = ++_serial, g = generation, token = authorizationHeader()!;
    busy = true;
    pendingID = id;
    preview = null;
    message = null;
    notifyListeners();
    try {
      await api.revoke(token, id);
      if (!_valid(serial, g)) return;
      unknown = false;
      current = null;
      message = '原记录已撤回。没有执行或取消外部网络请求。';
    } catch (_) {
      if (_valid(serial, g)) {
        unknown = true;
        message = '撤回结果未知，请核实同一原记录。';
      }
    } finally {
      if (_valid(serial, g)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    ++generation;
    ++_serial;
    api.dispose();
    super.dispose();
  }
}
