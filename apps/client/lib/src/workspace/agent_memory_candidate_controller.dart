import 'package:flutter/foundation.dart';
import 'agent_memory_candidate_api.dart';
import 'agent_candidate_pending_store.dart';

class AgentMemoryCandidateController extends ChangeNotifier {
  AgentMemoryCandidateController({
    required this.api,
    required this.authorizationHeader,
    required this.accountID,
    required this.organizationWorkspaceID,
    AgentCandidatePendingStore? pendingStore,
  }) : pendingStore = pendingStore ?? const SecureAgentCandidatePendingStore();
  final AgentCandidatePendingStore pendingStore;
  PendingHumanAcceptance? _pending;
  bool _pendingChecked = false, _storageBlocked = false;
  int _pendingCount = 0;
  final AgentMemoryCandidateAPI api;
  final String? Function() authorizationHeader,
      accountID,
      organizationWorkspaceID;
  List<HumanMemoryCandidate> records = [];
  List<CandidateSourceChoice> choices = [];
  final Set<String> selected = {};
  String category = 'badminton';
  HumanCandidatePreview? preview;
  HumanMemoryCandidate? current;
  String? error, notice;
  bool busy = false, unknownAccept = false, _closed = false;
  int generation = 0, _serial = 0;
  (String?, String?, String?)? _identity;
  bool get hasPendingAcceptance => _pending != null || _storageBlocked;
  bool get personal =>
      authorizationHeader() != null &&
      candidateIDValid(accountID()) &&
      organizationWorkspaceID() == null;
  void synchronizeIdentity() {
    if (_closed) {
      return;
    }
    final i = (authorizationHeader(), accountID(), organizationWorkspaceID());
    if (i == _identity) {
      return;
    }
    _identity = i;
    generation++;
    _serial++;
    records = [];
    choices = [];
    selected.clear();
    category = 'badminton';
    preview = null;
    current = null;
    error = null;
    notice = null;
    unknownAccept = false;
    _pending = null;
    _pendingChecked = false;
    _storageBlocked = false;
    busy = false;
    if (!_closed) {
      notifyListeners();
    }
  }

  bool _current(int g, int s) {
    if (_closed) {
      return false;
    }
    synchronizeIdentity();
    return personal && generation == g && _serial == s;
  }

  String _message(Object e) =>
      e is CandidateAPIError ? e.message : '请求结果未确认，请核实或重试。';
  Future<void> _run(
    Future<void> Function(String, String, int, int) action, {
    bool recoverStorage = false,
  }) async {
    if (_closed) {
      return;
    }
    synchronizeIdentity();
    if (!personal || busy) {
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
      if (!_pendingChecked || (_storageBlocked && recoverStorage)) {
        await _restore(auth, owner, g, s);
        if (!_current(g, s) || _storageBlocked) {
          return;
        }
      }
      if (_storageBlocked) {
        error = '本机核实记录无法安全处理，已阻止提交。请重试读取。';
        return;
      }
      await action(auth, owner, g, s);
    } catch (e) {
      if (_current(g, s)) {
        error = _message(e);
      }
    } finally {
      if (_current(g, s)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  Future<void> _restore(String a, String o, int g, int s) async {
    try {
      final entries = await pendingStore.read(api.recoveryEnvironment, o);
      if (!_current(g, s)) {
        return;
      }
      _pendingChecked = true;
      _storageBlocked = false;
      _pendingCount = entries.length;
      _pending = entries.isEmpty ? null : entries.first;
      unknownAccept = _pending != null;
      if (_pending != null) {
        notice = '发现尚未核实的保存记录；只读取原候选，不会自动提交。';
      }
    } catch (_) {
      if (_current(g, s)) {
        _storageBlocked = true;
        unknownAccept = true;
        error = '本机核实记录无法安全读取，已阻止提交。请重试读取。';
      }
    }
  }

  Future<void> _forget(
    PendingHumanAcceptance entry,
    String o,
    int g,
    int s,
  ) async {
    if (!_current(g, s)) {
      return;
    }
    await pendingStore.delete(api.recoveryEnvironment, o, entry);
    if (!_current(g, s)) {
      return;
    }
    _pending = null;
    unknownAccept = false;
    // Other unresolved operations must also block writes.
    await _restore(authorizationHeader()!, o, g, s);
  }

  Future<void> load() => _run((a, o, g, s) async {
    if (_pending != null) {
      await _verify(a, o, g, s);
      if (!_current(g, s)) {
        return;
      }
    }
    final r = await api.list(a, o);
    if (_current(g, s)) {
      records = r;
    }
  }, recoverStorage: true);
  Future<void> loadSources() => _run((a, o, g, s) async {
    if (unknownAccept || hasPendingAcceptance) {
      return;
    }
    final c = await api.sources(a, o);
    if (_current(g, s)) {
      choices = c;
      selected.removeWhere((k) => !c.any((v) => v.key == k));
      preview = null;
    }
  });
  void edit({String? nextCategory, String? toggle}) {
    synchronizeIdentity();
    if (!personal || busy || unknownAccept || hasPendingAcceptance || _closed) {
      return;
    }
    if (nextCategory != null && candidateCategories.containsKey(nextCategory)) {
      category = nextCategory;
    }
    if (toggle != null && choices.any((e) => e.key == toggle)) {
      if (selected.contains(toggle)) {
        selected.remove(toggle);
      } else if (selected.length < 20) {
        selected.add(toggle);
      }
    }
    preview = null;
    notice = null;
    notifyListeners();
  }

  void cancelPreview() {
    if (_closed || busy || unknownAccept || hasPendingAcceptance) {
      return;
    }
    preview = null;
    notifyListeners();
  }

  Future<void> save() => _run((a, o, g, s) async {
    if (unknownAccept || hasPendingAcceptance) {
      return;
    }
    final selectors = choices
        .where((e) => selected.contains(e.key))
        .map((e) => e.selector)
        .toList();
    if (selectors.isEmpty) {
      error = '请选择自己的真实来源。';
      return;
    }
    final r = HumanMemoryCandidate.read(
      await api.request('POST', AgentMemoryCandidateAPI.path, a, {
        'category': category,
        'sources': selectors,
        'validUntil': DateTime.now()
            .toUtc()
            .add(const Duration(hours: 1))
            .toIso8601String(),
      }),
      o,
    );
    if (_current(g, s)) {
      current = r;
      preview = null;
      notice = r.status == 'CANDIDATE'
          ? '候选已保存，尚未写入记忆。'
          : '同一候选已有处理结果：${candidateStatusLabels[r.status]}。不会重复写入或恢复旧建议。';
      records = [r, ...records.where((e) => e.id != r.id)];
    }
  });
  Future<void> prepare(HumanMemoryCandidate c) => _run((a, o, g, s) async {
    if (unknownAccept || hasPendingAcceptance || c.status != 'CANDIDATE') {
      return;
    }
    final key = AgentMemoryCandidateAPI.previewKey();
    final r = HumanCandidatePreview.read(
      await api.request(
        'POST',
        '${AgentMemoryCandidateAPI.path}/${c.id}/preview',
        a,
        {
          'previewId': key,
          'expectedVersion': c.version,
          'memoryValidUntil': DateTime.now()
              .toUtc()
              .add(const Duration(days: 1))
              .toIso8601String(),
        },
      ),
      o,
      c,
      key,
    );
    if (_current(g, s)) {
      preview = r;
      current = c;
      notice = null;
    }
  });
  Future<void> accept() => _run((a, o, g, s) async {
    final p = preview;
    if (p == null || unknownAccept || !p.expiresAt.isAfter(DateTime.now())) {
      if (!unknownAccept) {
        preview = null;
      }
      error = '预览已失效，请重新检查。';
      return;
    }
    final entry = PendingHumanAcceptance({
      'environment': candidateRecoveryFingerprint(api.recoveryEnvironment),
      'ownerId': o,
      'agentId': p.candidate.raw['agentId'],
      'sessionFingerprint': candidateRecoveryFingerprint(a),
      'previewId': p.id,
      'candidateId': p.candidate.id,
      'candidateVersion': p.candidate.version,
      'targetMemoryId': p.review['targetMemoryId'],
      'expectedMemoryVersion': p.review['expectedMemoryVersion'],
      'planDigest': p.review['planDigest'],
      'expiresAt': p.expiresAt.toUtc().toIso8601String(),
    });
    try {
      await pendingStore.write(api.recoveryEnvironment, o, entry);
      if (!_current(g, s)) {
        return;
      }
      final saved = await pendingStore.read(api.recoveryEnvironment, o);
      if (!_current(g, s)) {
        return;
      }
      if (!saved.any((v) => v.same(entry))) {
        throw StateError('本机核实记录未安全保存');
      }
      _pendingCount = saved.length;
      _pending = entry;
      if (!p.expiresAt.isAfter(DateTime.now())) {
        unknownAccept = true;
        error = '记录保存期间预览已过期；未发送提交，请核实原候选。';
        return;
      }
    } catch (_) {
      if (_current(g, s)) {
        _storageBlocked = true;
        unknownAccept = true;
        error = '本机核实记录未安全保存，已阻止提交。';
      }
      return;
    }
    try {
      final r = HumanMemoryCandidate.read(
        await api.request(
          'POST',
          '${AgentMemoryCandidateAPI.path}/${p.candidate.id}/accept',
          a,
          {'previewId': p.id},
        ),
        o,
      );
      if (!entry.matches(r)) {
        throw const FormatException('权威结果不匹配');
      }
      if (!_current(g, s)) {
        return;
      }
      await _forget(entry, o, g, s);
      if (!_current(g, s)) {
        return;
      }
      current = r;
      preview = null;
      notice = '已保存为本人明确声明的私密记忆。';
      records = [r, ...records.where((e) => e.id != r.id)];
    } catch (_) {
      if (_current(g, s)) {
        unknownAccept = true;
        current = p.candidate;
        error = '提交结果未知，请先核实原候选；不会自动重发。';
      }
    }
  });
  Future<void> _verify(String a, String o, int g, int s) async {
    final entry = _pending;
    if (entry == null) {
      return;
    }
    final p = preview;
    HumanMemoryCandidate r;
    try {
      r = await api.read(a, o, entry.candidateID);
    } catch (e) {
      if (_current(g, s)) {
        notice = '当前候选不可用，旧保存结果仍未确认；不会自动重发。';
      }
      rethrow;
    }
    if (!_current(g, s)) {
      return;
    }
    current = r;
    records = [r, ...records.where((e) => e.id != r.id)];
    if (entry.matches(r)) {
      await _forget(entry, o, g, s);
      if (!_current(g, s)) {
        return;
      }
      preview = null;
      notice = '已核实：原声明已经保存，无需重复提交。';
    } else if (r.status == 'CANDIDATE' &&
        r.version == entry.data['candidateVersion'] &&
        r.raw['agentId'] == entry.data['agentId']) {
      final sameReview =
          _pendingCount == 1 &&
          p != null &&
          p.id == entry.previewID &&
          p.candidate.id == entry.candidateID &&
          entry.data['sessionFingerprint'] == candidateRecoveryFingerprint(a);
      if (sameReview && p.expiresAt.isAfter(DateTime.now())) {
        unknownAccept = false;
        notice = '当前仍待确认。可在原期限内明确重试同一预览，不会另建目标。';
      } else if (!entry.expiresAt.isAfter(DateTime.now())) {
        await _forget(entry, o, g, s);
        if (!_current(g, s)) {
          return;
        }
        preview = null;
        notice = '当前仍待确认，旧预览已过期；可重新检查具体预览后决定。';
      } else {
        unknownAccept = true;
        preview = null;
        notice = '原保存仍待核实；本页没有旧具体预览。原期限前仅可核实，不会恢复批准。';
      }
    } else {
      unknownAccept = true;
      preview = null;
      notice = '当前状态：${candidateStatusLabels[r.status]}，旧保存结果未匹配；不会自动重发。';
    }
  }

  Future<void> verifyUnknown() => _run((a, o, g, s) async {
    if (!unknownAccept) {
      return;
    }
    await _verify(a, o, g, s);
  }, recoverStorage: true);
  Future<void> reject(HumanMemoryCandidate c) => _run((a, o, g, s) async {
    if (unknownAccept || hasPendingAcceptance) {
      return;
    }
    final r = HumanMemoryCandidate.read(
      await api.request(
        'POST',
        '${AgentMemoryCandidateAPI.path}/${c.id}/reject',
        a,
        {'expectedVersion': c.version},
      ),
      o,
    );
    if (r.id != c.id || r.status != 'REJECTED') {
      throw const FormatException('拒绝结果无效');
    }
    if (_current(g, s)) {
      preview = null;
      current = r;
      records = [r, ...records.where((e) => e.id != r.id)];
      notice = '已拒绝，不会自动恢复同一候选。';
    }
  });
  @override
  void dispose() {
    if (_closed) {
      return;
    }
    _closed = true;
    generation++;
    _serial++;
    records = [];
    choices = [];
    selected.clear();
    preview = null;
    current = null;
    error = null;
    notice = null;
    unknownAccept = false;
    busy = false;
    _identity = null;
    api.dispose();
    super.dispose();
  }
}
