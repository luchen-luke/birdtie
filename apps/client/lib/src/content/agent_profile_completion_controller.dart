import 'package:flutter/foundation.dart';
import '../workspace/agent_memory_candidate_api.dart' show candidateIDValid;
import 'agent_profile_completion_api.dart';
import 'agent_profile_completion_pending_store.dart';

class AgentProfileCompletionController extends ChangeNotifier {
  AgentProfileCompletionController({
    required String? Function() authorizationHeader,
    required String? Function() accountID,
    String? Function()? organizationWorkspaceID,
    AgentProfileCompletionAPI? api,
    AgentProfileCompletionPendingStore? pendingStore,
    DateTime Function()? now,
  }) : _token = authorizationHeader,
       _owner = accountID,
       _workspace = organizationWorkspaceID ?? (() => null),
       api = api ?? AgentProfileCompletionAPI(),
       store = pendingStore ?? const SecureAgentProfileCompletionPendingStore(),
       _now = now ?? (() => DateTime.now().toUtc());
  final String? Function() _token, _owner, _workspace;
  final AgentProfileCompletionAPI api;
  final AgentProfileCompletionPendingStore store;
  final DateTime Function() _now;
  String? _boundToken, _boundOwner, _boundWorkspace;
  bool _bound = false, _closed = false, _retired = false;
  bool busy = false, unknown = false, storageBlocked = false, denied = false;
  String? error, message;
  ProfileCompletionSuggestions? suggestions;
  ProfileCompletionPreview? preview;
  PendingProfileCompletion? _pending;
  bool get retired => _retired;
  bool get hasPending => _pending != null;
  bool get canReview =>
      _current &&
      !busy &&
      !unknown &&
      !hasPending &&
      !storageBlocked &&
      !denied &&
      !retired;
  bool get canAccept =>
      _current &&
      !busy &&
      !unknown &&
      !storageBlocked &&
      !denied &&
      !retired &&
      preview != null &&
      preview!.expiresAt.isAfter(_now());
  bool get _current =>
      !_closed &&
      !_retired &&
      !denied &&
      _bound &&
      _boundToken != null &&
      candidateIDValid(_boundOwner) &&
      _boundWorkspace == null &&
      _token() == _boundToken &&
      _owner() == _boundOwner &&
      _workspace() == _boundWorkspace;
  void identityChanged() {
    if (_bound && !_current) retire();
  }

  void retire() {
    if (_closed || _retired) return;
    _retired = true;
    busy = false;
    preview = null;
    suggestions = null;
    message = null;
    error = '身份或连接已变化，请关闭后以当前身份重新打开。';
    notifyListeners();
  }

  bool _bind() {
    if (denied || _closed || _retired) return false;
    if (_bound) {
      identityChanged();
      return _current;
    }
    _bound = true;
    _boundToken = _token();
    _boundOwner = _owner();
    _boundWorkspace = _workspace();
    if (_boundToken == null ||
        !candidateIDValid(_boundOwner) ||
        _boundWorkspace != null) {
      denied = true;
      error = '请以本人有效个人身份打开。';
      notifyListeners();
      return false;
    }
    return true;
  }

  void _changed() {
    if (!_closed) notifyListeners();
  }

  Future<void> load() async {
    if (busy || !_bind()) return;
    busy = true;
    error = null;
    _changed();
    try {
      final p = await store.read(api.environment, _boundOwner!);
      if (!_current) return;
      _pending = p;
      if (p != null) {
        unknown = true;
        busy = false;
        _changed();
        await verify();
        return;
      }
      final raw = await api.request('GET', 'suggestions', _boundToken!);
      if (!_current) return;
      suggestions = ProfileCompletionSuggestions.read(raw, _boundOwner!);
      unknown = false;
      storageBlocked = false;
      message = suggestions!.alreadySet
          ? '活动偏好已有内容，本次不会覆盖。'
          : suggestions!.sources.isEmpty
          ? '当前没有可复用的有效活动声明。可以保留现有资料，以后再补。'
          : '选择一条本人已确认的活动声明，先检查再保存。';
    } catch (e) {
      if (_current) {
        if (e is FormatException) {
          storageBlocked = true;
          error = '回执或本机核实记录不符，未提交新操作。';
        } else {
          error = '暂时无法读取，请稍后重试。';
        }
      }
    } finally {
      if (_current) {
        busy = false;
        _changed();
      }
    }
  }

  PendingProfileCompletion _journal(
    ProfileCompletionSource s,
    String id, {
    ProfileCompletionPreview? reviewed,
  }) => PendingProfileCompletion({
    'environment': completionFingerprint(api.environment),
    'ownerId': _boundOwner,
    'agentId': suggestions!.agentID,
    'sessionFingerprint': completionFingerprint(_boundToken!),
    'previewId': id,
    'memoryId': s.id,
    'memoryVersion': s.version,
    'expectedProfileVersion': suggestions!.profileVersion,
    'phase': reviewed == null ? 'PREVIEW' : 'ACCEPT',
    'planDigest': reviewed?.digest,
    'expiresAt': (reviewed?.expiresAt ?? s.validUntil).toIso8601String(),
  });
  Future<void> review(ProfileCompletionSource source) async {
    if (!canReview ||
        !_bind() ||
        suggestions == null ||
        suggestions!.alreadySet ||
        !suggestions!.sources.contains(source) ||
        !source.validUntil.isAfter(_now())) {
      return;
    }
    busy = true;
    error = null;
    message = null;
    final p = _journal(source, api.newID());
    _changed();
    try {
      await store.write(api.environment, _boundOwner!, p);
      if (!_current) return;
      _pending = p;
      if (!source.validUntil.isAfter(_now())) {
        unknown = true;
        message = '来源已到期；未发送新提交，请核实原操作。';
        return;
      }
      final raw = await api.request(
        'POST',
        'previews',
        _boundToken!,
        body: {
          'previewId': p.id,
          'memoryId': source.id,
          'memoryVersion': source.version,
          'expectedProfileVersion': suggestions!.profileVersion,
        },
      );
      if (!_current) return;
      final v = ProfileCompletionPreview.read(raw, _boundOwner!);
      if (v.id != p.id ||
          v.agentID != p.data['agentId'] ||
          v.source.id != source.id ||
          v.source.version != source.version ||
          v.source.category != source.category ||
          !v.source.validUntil.isAtSameMomentAs(source.validUntil) ||
          v.profileVersion != p.data['expectedProfileVersion'] ||
          !v.expiresAt.isAfter(_now())) {
        throw const FormatException('审阅版本已改变');
      }
      preview = v;
      message = '请检查具体内容和保存后果；尚未保存。';
    } catch (e) {
      if (_current) {
        unknown = _pending != null;
        storageBlocked = _pending == null;
        error = storageBlocked ? '本机核实记录未安全保存，未发送提交。' : '结果尚未核实，请读取原操作；不会自动重发。';
      }
    } finally {
      if (_current) {
        busy = false;
        _changed();
      }
    }
  }

  Future<void> accept() async {
    identityChanged();
    if (!canAccept || !_current || suggestions == null || _pending == null) {
      return;
    }
    final v = preview!, p = _journal(v.source, v.id, reviewed: v);
    busy = true;
    error = null;
    _changed();
    try {
      await store.write(api.environment, _boundOwner!, p);
      if (!_current) return;
      _pending = p;
      if (!v.expiresAt.isAfter(_now())) {
        unknown = true;
        preview = null;
        message = '审阅期限已过，未发送保存。请核实原操作后重新检查。';
        return;
      }
      final raw = await api.request(
        'POST',
        'previews/${v.id}/accept',
        _boundToken!,
        body: {'planDigest': v.digest},
      );
      if (!_current) return;
      final r = ProfileCompletionReceipt.read(raw, _boundOwner!);
      if (!p.matches(r) || r.state != 'COMMITTED') {
        throw const FormatException('保存回执不符');
      }
      await _publish(r, p);
    } catch (e) {
      if (_current) {
        unknown = true;
        preview = null;
        error = '保存结果尚未核实，请读取原操作；不会自动重发。';
      }
    } finally {
      if (_current) {
        busy = false;
        _changed();
      }
    }
  }

  Future<void> _publish(
    ProfileCompletionReceipt r,
    PendingProfileCompletion p,
  ) async {
    if (!_current || _pending == null || !_pending!.same(p)) return;
    await store.delete(api.environment, _boundOwner!, p);
    if (!_current) return;
    _pending = null;
    unknown = false;
    preview = null;
    suggestions = null;
    storageBlocked = false;
    message = r.state == 'COMMITTED'
        ? (r.currentMatches
              ? '已核实保存：本人私密活动偏好已补齐。'
              : '原操作曾保存，但当前资料已变化。请重新读取当前资料。')
        : '原预览已失效，未保存。可以重新读取当前来源。';
  }

  Future<void> verify() async {
    identityChanged();
    if (busy || !_current || _pending == null) return;
    busy = true;
    error = null;
    preview = null;
    unknown = true;
    final p = _pending!;
    _changed();
    try {
      final raw = await api.request('GET', 'previews/${p.id}', _boundToken!);
      if (!_current) return;
      final r = ProfileCompletionReceipt.read(raw, _boundOwner!);
      if (!p.matches(r)) throw const FormatException('原操作回执不符');
      if (r.state == 'PENDING') {
        message = '原预览仍有效，尚未保存。旧审阅内容无法恢复；请在原期限过后核实，再重新检查。';
      } else {
        await _publish(r, p);
      }
    } catch (e) {
      if (_current) {
        if (e is ProfileCompletionHTTPError && e.status == 404) {
          error = '尚未找到原操作；原请求可能仍在处理中。保留核实引用，请稍后再次读取，不会提交新操作。';
        } else {
          error = '仍无法核实原操作，请稍后再次读取。不会恢复旧批准或自动提交。';
        }
      }
    } finally {
      if (_current) {
        busy = false;
        _changed();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    api.dispose();
    super.dispose();
  }
}
