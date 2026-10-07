import 'package:flutter/foundation.dart';
import 'agent_memory_candidate_api.dart';
import 'agent_multi_candidate_api.dart';
import 'agent_multi_candidate_pending_store.dart';

enum MultiCandidateUnknown { approval, revocation, stage }

class AgentMultiCandidateController extends ChangeNotifier {
  AgentMultiCandidateController({
    required this.api,
    required this.authorizationHeader,
    required this.accountID,
    required this.organizationWorkspaceID,
    this.pendingStore = const SecureAgentMultiCandidatePendingStore(),
  });
  final AgentMultiCandidateAPI api;
  final AgentMultiCandidatePendingStore pendingStore;
  PendingMultiCandidateOperation? _pending;
  bool _pendingChecked = false, _storageBlocked = false, _reopened = false;
  String? _retryApprovalID;
  bool get recoveryBlocked => _pending != null || _storageBlocked;
  String get _environment => api.recoveryEnvironment;
  String get _session =>
      candidateRecoveryFingerprint(authorizationHeader() ?? '');
  final String? Function() authorizationHeader,
      accountID,
      organizationWorkspaceID;
  List<AnalysisMomentChoice> moments = [];
  List<AnalysisTaskChoice> tasks = [];
  String? taskID;
  final Map<String, bool> selected =
      {}; // Value explicitly selects body as well as title.
  final Map<String, CandidatePurposeGrant> analysisGrants = {};
  CandidatePurposePreview? preview;
  CandidatePurposeGrant? retentionGrant;
  Map<String, dynamic>? _approvedMultiReview;
  CandidatePurposeGrant? _unknownGrant;
  MultiCandidateReceipt? result;
  MultiCandidateUnknown? unknown;
  String? error, notice;
  bool busy = false, _closed = false, loaded = false;
  int generation = 0, _serial = 0;
  (String?, String?, String?)? _identity;
  bool get personal =>
      authorizationHeader() != null &&
      candidateIDValid(accountID()) &&
      organizationWorkspaceID() == null;
  bool get editable =>
      personal && !busy && unknown == null && !recoveryBlocked && !_closed;
  bool get canApprove =>
      personal &&
      !busy &&
      !_closed &&
      !_storageBlocked &&
      preview?.current == true &&
      (editable ||
          unknown == MultiCandidateUnknown.approval &&
              !_reopened &&
              _retryApprovalID == preview?.id &&
              _pending?.data['previewId'] == preview?.id &&
              _pending?.data['sessionFingerprint'] == _session);
  bool get canPrepareMulti =>
      editable &&
      selected.length >= 2 &&
      selected.length <= 5 &&
      selected.keys.every((id) => analysisGrants[id]?.usable == true);
  bool get canStage =>
      editable && retentionGrant?.usable == true && result?.staged != true;

  void synchronizeIdentity() {
    if (_closed) return;
    final next = (
      authorizationHeader(),
      accountID(),
      organizationWorkspaceID(),
    );
    if (next == _identity) return;
    _identity = next;
    generation++;
    _serial++;
    moments = [];
    tasks = [];
    taskID = null;
    selected.clear();
    analysisGrants.clear();
    _approvedMultiReview = null;
    preview = null;
    retentionGrant = null;
    _unknownGrant = null;
    result = null;
    unknown = null;
    error = null;
    notice = null;
    busy = false;
    loaded = false;
    _pending = null;
    _pendingChecked = false;
    _storageBlocked = false;
    _reopened = false;
    _retryApprovalID = null;
    notifyListeners();
  }

  bool _current(int g, int s) {
    if (_closed) return false;
    synchronizeIdentity();
    return personal && generation == g && _serial == s;
  }

  Future<void> _run(
    Future<void> Function(String, String, int, int) action, {
    bool allowPending = false,
  }) async {
    if (_closed) return;
    synchronizeIdentity();
    if (!personal ||
        busy ||
        (!allowPending && (recoveryBlocked || unknown != null))) {
      return;
    }
    final g = generation,
        s = ++_serial,
        auth = authorizationHeader()!,
        owner = accountID()!;
    busy = true;
    error = null;
    notifyListeners();
    try {
      if (!_pendingChecked) {
        try {
          final saved = await pendingStore.read(_environment, owner);
          if (!_current(g, s)) return;
          _pendingChecked = true;
          _storageBlocked = false;
          if (saved.isNotEmpty) {
            if (saved.length != 1) throw StateError('存在多项待核实记录');
            _pending = saved.single;
            _reopened = true;
            unknown = MultiCandidateUnknown.values.singleWhere(
              (v) => v.name == _pending!.data['phase'],
            );
            notice = '发现原操作的本机核实记录。仅可读取原结果；不会恢复旧许可或自动提交。';
          }
        } catch (_) {
          if (_current(g, s)) {
            _storageBlocked = true;
            _pendingChecked = false;
            error = '本机核实记录无法安全读取，请重试读取；未提交。';
          }
          return;
        }
      }
      if (recoveryBlocked && !allowPending) return;
      await action(auth, owner, g, s);
    } catch (e) {
      if (_current(g, s)) {
        error = e is CandidateAPIError
            ? e.message
            : e is FormatException
            ? e.message
            : '连接未完成，请检查网络后重试；未自动提交。';
      }
    } finally {
      if (_current(g, s)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  Future<void> load() => _run((a, o, g, s) async {
    if (unknown != null) return;
    final choices = await api.choices(a, o);
    if (!_current(g, s)) return;
    tasks = choices.$1;
    moments = choices.$2;
    loaded = true;
    // Reading new versions retires all concrete approvals. Existing remote
    // grants are not revoked implicitly; their original expiry remains in force.
    taskID = null;
    selected.clear();
    analysisGrants.clear();
    preview = null;
    retentionGrant = null;
    result = null;
    notice = null;
    _approvedMultiReview = null;
  }, allowPending: true);
  void edit({String? nextTask, String? toggleMoment, String? toggleBody}) {
    synchronizeIdentity();
    if (!editable) return;
    if (nextTask != null &&
        tasks.any((t) => t.id == nextTask) &&
        nextTask != taskID) {
      taskID = nextTask;
      analysisGrants.clear();
    }
    if (toggleMoment != null && moments.any((m) => m.id == toggleMoment)) {
      if (selected.containsKey(toggleMoment)) {
        selected.remove(toggleMoment);
      } else if (selected.length < 5) {
        selected[toggleMoment] = false;
      }
      analysisGrants.remove(toggleMoment);
    }
    if (toggleBody != null && selected.containsKey(toggleBody)) {
      selected[toggleBody] = !selected[toggleBody]!;
      analysisGrants.remove(toggleBody);
    }
    preview = null;
    retentionGrant = null;
    result = null;
    error = null;
    _approvedMultiReview = null;
    notice = null;
    notifyListeners();
  }

  void cancelPreview() {
    synchronizeIdentity();
    if (!editable) return;
    preview = null;
    error = null;
    notifyListeners();
  }

  Future<void> prepareAnalysis(String momentID) => _run((a, o, g, s) async {
    if (unknown != null || !selected.containsKey(momentID) || taskID == null) {
      return;
    }
    final moment = moments.singleWhere((m) => m.id == momentID);
    final now = DateTime.now().toUtc();
    var deadline = now.add(const Duration(minutes: 10));
    final sourceDeadline = moment.updatedAt.add(const Duration(minutes: 15));
    if (sourceDeadline.isBefore(deadline)) deadline = sourceDeadline;
    if (!deadline.isAfter(now)) {
      throw const FormatException('这条动态已超出分析期限，请先检查原动态。');
    }
    final includeBody = selected[momentID]!;
    final task = tasks.singleWhere((t) => t.id == taskID);
    final fields = includeBody ? ['body', 'title'] : ['title'];
    final sel = {
      'taskId': taskID,
      'momentId': momentID,
      'momentRevision': moment.revision,
      'fields': fields,
      'deadlineAt': deadline.toIso8601String(),
    };
    final p = await api.preview(a, o, sel, multi: false);
    if (!_current(g, s)) return;
    final content = p.review!['content'] as Map;
    if (p.state != 'CURRENT_REVIEW' ||
        content['title'] != moment.title ||
        (includeBody && content['body'] != moment.body) ||
        p.review!['taskQuery'] != task.query ||
        candidateTime(p.review!['taskUpdatedAt']) != task.updatedAt) {
      throw const FormatException('任务或来源内容已变化，请重新读取；旧批准不可使用。');
    }
    if (_current(g, s)) {
      preview = p;
      retentionGrant = null;
      _approvedMultiReview = null;
      result = null;
      notice = null;
    }
  });
  Future<void> prepareMulti() => _run((a, o, g, s) async {
    if (unknown != null ||
        selected.length < 2 ||
        selected.length > 5 ||
        !selected.keys.every((id) => analysisGrants[id]?.usable == true)) {
      return;
    }
    final grants = selected.keys.map((id) => analysisGrants[id]!).toList();
    if (grants.map((g) => g.agentID).toSet().length != 1) {
      throw const FormatException('来源的个人身份不一致，请重新检查。');
    }
    final earliest = grants
        .map((g) => g.expiresAt)
        .reduce((a, b) => a.isBefore(b) ? a : b);
    final p = await api.preview(a, o, {
      'analysisGrantIds': grants.map((g) => g.id).toList()..sort(),
      'retainUntil': earliest.toIso8601String(),
    }, multi: true);
    if (!_current(g, s)) return;
    if (p.state != 'CURRENT_REVIEW' ||
        p.agentID != grants.first.agentID ||
        p.review!['taskId'] != taskID) {
      throw const FormatException('组合候选的任务或身份不匹配。');
    }
    for (final member in p.review!['sourceSelections'] as List) {
      final source = member['source'] as Map;
      final id = source['selector']['id'] as String;
      final original = analysisGrants[id];
      if (original == null ||
          member['analysisGrantId'] != original.id ||
          member['analysisPreviewId'] != original.previewID ||
          source['version']['revision'] !=
              original.selection['momentRevision'] ||
          !listEquals(
            (member['selectedFields'] as List).cast<String>(),
            (original.selection['fields'] as List).cast<String>(),
          )) {
        throw const FormatException('组合来源的具体版本不匹配。');
      }
    }
    if (_current(g, s)) {
      preview = p;
      retentionGrant = null;
      _approvedMultiReview = null;
      result = null;
      notice = null;
    }
  });
  PendingMultiCandidateOperation _entry(
    String phase,
    String owner, {
    CandidatePurposePreview? p,
    CandidatePurposeGrant? grant,
  }) {
    final multi = p?.multi ?? grant!.multi;
    final selection = p?.selection ?? grant!.selection;
    final review = p?.review ?? (multi ? _approvedMultiReview : null);
    final versions = <Map<String, dynamic>>[];
    if (multi && review != null) {
      for (final member in review['sourceSelections'] as List) {
        versions.add({
          'momentId': member['source']['selector']['id'],
          'revision': member['source']['version']['revision'],
        });
      }
    } else if (!multi) {
      versions.add({
        'momentId': selection['momentId'],
        'revision': selection['momentRevision'],
      });
    }
    return PendingMultiCandidateOperation({
      'environment': candidateRecoveryFingerprint(_environment),
      'ownerId': owner,
      'agentId': p?.agentID ?? grant!.agentID,
      'sessionFingerprint': _session,
      'phase': phase,
      'multi': multi,
      'previewId': p?.id ?? grant!.previewID,
      'grantId': grant?.id,
      'grantRevision': grant?.revision,
      'selectionDigest': multiSelectionDigest(selection, multi),
      'reviewDigest': multiReviewDigest(review),
      'taskId': multi ? (review?['taskId']) : selection['taskId'],
      'anchorEventId': multi ? (review?['anchorEventId']) : null,
      'logicalOperationId': multi ? (review?['logicalOperationId']) : null,
      'expiresAt': (p?.expiresAt ?? grant!.expiresAt).toIso8601String(),
      'sourceVersions': versions,
    });
  }

  Future<bool> _reserve(
    PendingMultiCandidateOperation entry,
    String owner,
    int g,
    int s,
  ) async {
    try {
      await pendingStore.write(_environment, owner, entry);
      final saved = await pendingStore.read(_environment, owner);
      if (!_current(g, s)) return false;
      if (saved.length != 1 || !saved.single.same(entry)) {
        throw StateError('本机记录未完整保存');
      }
      _pending = entry;
      _reopened = false;
      if (!entry.expiresAt.isAfter(DateTime.now().toUtc())) {
        unknown = MultiCandidateUnknown.values.singleWhere(
          (v) => v.name == entry.data['phase'],
        );
        error = '保存核实记录期间原期限已过，请读取原结果；未发送。';
        return false;
      }
      return true;
    } catch (_) {
      if (_current(g, s)) {
        _storageBlocked = true;
        _pendingChecked = false;
        error = '本机记录未安全保存，请先重新读取核实；未发送。';
      }
      return false;
    }
  }

  Future<bool> _clear(
    PendingMultiCandidateOperation entry,
    String owner,
    int g,
    int s,
  ) async {
    if (!_current(g, s)) return false;
    await pendingStore.delete(_environment, owner, entry);
    if (!_current(g, s)) return false;
    _pending = null;
    _reopened = false;
    return true;
  }

  void _approved(CandidatePurposeGrant grant) {
    if (!grant.usable) throw const FormatException('许可已经撤回或到期，请重新检查。');
    if (grant.multi) {
      _approvedMultiReview = preview!.review;
      retentionGrant = grant;
      notice = '已批准保留候选，尚未提交；不会自动写入记忆。';
    } else {
      analysisGrants[grant.selection['momentId'] as String] = grant;
      notice = '已允许本次所选内容的本地分析，尚未保留候选。';
    }
    preview = null;
  }

  Future<void> approve() => _run((a, o, g, s) async {
    final retry =
        unknown == MultiCandidateUnknown.approval &&
        !_reopened &&
        !_storageBlocked &&
        _retryApprovalID == preview?.id &&
        _pending?.data['previewId'] == preview?.id &&
        _pending?.data['sessionFingerprint'] == _session;
    if (_storageBlocked || (unknown != null || recoveryBlocked) && !retry) {
      return;
    }
    _retryApprovalID = null;
    final p = preview;
    if (p == null || !p.current) {
      preview = null;
      return;
    }
    final entry = _entry('approval', o, p: p);
    if (!await _reserve(entry, o, g, s) || !_current(g, s)) return;
    if (!p.current) {
      unknown = MultiCandidateUnknown.approval;
      error = '原预览期限已过，请核实原结果；未发送。';
      return;
    }
    try {
      final grant = await api.approve(a, o, p);
      if (_current(g, s) && await _clear(entry, o, g, s)) {
        if (grant.usable) {
          _approved(grant);
        } else {
          preview = null;
          notice = '已收到原批准结果，但当前许可已到期或撤回；不能继续提交。';
        }
      }
    } catch (_) {
      if (_current(g, s)) {
        unknown = MultiCandidateUnknown.approval;
        error = '授权结果未知，请先核实原预览；不会重新建立许可或自动提交。';
      }
    }
  }, allowPending: true);
  Future<void> stage() => _run((a, o, g, s) async {
    final grant = retentionGrant;
    if (unknown != null ||
        grant == null ||
        !grant.usable ||
        result?.staged == true) {
      return;
    }
    final entry = _entry('stage', o, grant: grant);
    if (!await _reserve(entry, o, g, s) || !_current(g, s)) return;
    if (!grant.usable) {
      unknown = MultiCandidateUnknown.stage;
      error = '原许可期限已过，请核实原结果；未发送。';
      return;
    }
    try {
      final receipt = await api.stage(a, o, grant);
      if (_current(g, s)) {
        receipt.requireApprovedReview(_approvedMultiReview!);
        if (!await _clear(entry, o, g, s)) return;
        result = receipt;
        notice = '候选已提交，尚未写入记忆。请到“我的候选记录”检查并自行决定。';
      }
    } catch (_) {
      if (_current(g, s)) {
        unknown = MultiCandidateUnknown.stage;
        error = '提交结果未知，请核实原许可的回执；不会自动重发或另选来源。';
      }
    }
  });
  Future<void> revoke(CandidatePurposeGrant grant) => _run((a, o, g, s) async {
    if (unknown != null || grant.revoked) return;
    final entry = _entry('revocation', o, grant: grant);
    if (!await _reserve(entry, o, g, s) || !_current(g, s)) return;
    try {
      final revoked = await api.revoke(a, o, grant);
      if (_current(g, s) && await _clear(entry, o, g, s)) _revoked(revoked);
    } catch (_) {
      if (_current(g, s)) {
        unknown = MultiCandidateUnknown.revocation;
        _unknownGrant = grant;
        error = '撤回结果未知，请核实原许可；不会自动重发。';
      }
    }
  });
  void _revoked(CandidatePurposeGrant grant) {
    if (grant.multi) {
      retentionGrant = grant;
    } else {
      analysisGrants[grant.selection['momentId'] as String] = grant;
      retentionGrant = null;
    }
    preview = null;
    result = null;
    _approvedMultiReview = null;
    notice = '已核实许可撤回。待确认候选支持失效；已由你明确保存的独立记忆仍需在记忆管理中处理。';
  }

  Future<void> verifyUnknown() => _run((a, o, g, s) async {
    final kind = unknown;
    if (kind == null) return;
    final entry = _pending;
    if (entry == null) return;
    _retryApprovalID = null;
    if (kind == MultiCandidateUnknown.approval) {
      final status = await api.reconcile(a, o, entry);
      if (!_current(g, s)) return;
      if (status.resolved) {
        if (!await _clear(entry, o, g, s)) return;
        preview = null;
        unknown = null;
        notice = status.state == 'APPROVAL_RECORDED'
            ? '已核实原批准有记录。旧许可未恢复；如需继续，请重新选择并具体检查。'
            : '原预览期限已结束，当前没有批准记录。不能据此断言先前提交从未发生；未恢复许可。';
        return;
      }
      notice = '原批准窗口尚未结束，请保留待核记录；不会新建目标或自动提交。';
      final p = preview;
      if (!_reopened &&
          entry.data['sessionFingerprint'] == _session &&
          p != null &&
          p.current) {
        final current = await api.readPreview(a, o, p);
        if (!_current(g, s)) return;
        if (current.state == 'CURRENT_REVIEW' && current.current) {
          preview = current;
          _retryApprovalID = current.id;
          notice = '原具体预览已重新检查。仅可明确重试这一次批准；待核记录仍保留，不能改选来源。';
        }
      }
      return;
    }
    if (_reopened || entry.data['sessionFingerprint'] != _session) {
      final status = await api.reconcile(a, o, entry);
      if (!_current(g, s)) return;
      if (status.resolved) {
        if (!await _clear(entry, o, g, s)) return;
        unknown = null;
      }
      notice = switch (status.state) {
        'CANDIDATE_STAGED' => '已核实原候选已提交。请在候选记录检查；旧许可和正文未恢复。',
        'REVOKED' => '已核实原许可撤回。已独立保存的本人声明未被删除。',
        _ => '已读取原操作当前状态；不能证明先前提交从未发生，未恢复执行许可。',
      };
      return;
    }
    void Function()? apply;
    if (kind == MultiCandidateUnknown.stage) {
      final receipt = await api.receipt(a, o, retentionGrant!);
      if (!_current(g, s)) return;
      receipt.requireApprovedReview(_approvedMultiReview!);
      apply = () {
        result = receipt;
        notice = receipt.staged
            ? '已核实：原候选已提交，尚未写入记忆。'
            : '当前没有提交回执；不能据此断言先前请求未发生，可在原期限内重新检查决定。';
      };
    } else {
      final grant = await api.readGrant(a, o, _unknownGrant!);
      if (!_current(g, s)) return;
      apply = () {
        if (grant.revoked) {
          _revoked(grant);
        } else {
          notice = '原许可当前尚未撤回；如仍需要，可再次明确撤回同一许可。';
        }
        _unknownGrant = null;
      };
    }
    if (!await _clear(entry, o, g, s)) return;
    apply();
    unknown = null;
  }, allowPending: true);
  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    generation++;
    _serial++;
    tasks = [];
    moments = [];
    selected.clear();
    analysisGrants.clear();
    preview = null;
    retentionGrant = null;
    result = null;
    _approvedMultiReview = null;
    _unknownGrant = null;
    _retryApprovalID = null;
    unknown = null;
    error = null;
    notice = null;
    busy = false;
    api.dispose();
    super.dispose();
  }
}
