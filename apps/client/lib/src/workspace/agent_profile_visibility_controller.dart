import 'package:flutter/foundation.dart';
import 'agent_memory_candidate_api.dart' show candidateIDValid;
import 'agent_profile_visibility_api.dart';
import 'community_api.dart';

class ProfileVisibilityReview {
  ProfileVisibilityReview._(
    this.generation,
    this.serial,
    this.edit,
    this.original,
    this.rules,
  );
  final int generation, serial, edit;
  final ProfileVisibilityRecord original;
  final Map<String, ProfileFieldAudience> rules;
}

class AgentProfileVisibilityController extends ChangeNotifier {
  AgentProfileVisibilityController({
    required this.api,
    required this.authorizationHeader,
    required this.ownerID,
    this.organizationWorkspaceID,
    this.current,
  }) {
    _identity = (
      authorizationHeader(),
      ownerID(),
      organizationWorkspaceID?.call(),
    );
  }
  final AgentProfileVisibilityAPI api;
  final String? Function() authorizationHeader, ownerID;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  late final (String?, String?, String?) _identity;
  int _generation = 0, _serial = 0, _edit = 0;
  bool _closed = false, retired = false, busy = false, resultUnknown = false;
  bool communitiesLoaded = false;
  String? error, notice, _agentID;
  ProfileVisibilityRecord? record;
  Map<String, ProfileFieldAudience> draft = const {};
  List<CommunityItem> communities = const [];
  ProfileVisibilityReview? review;
  bool get personal =>
      !_closed &&
      !retired &&
      current?.call() != false &&
      _identity ==
          (authorizationHeader(), ownerID(), organizationWorkspaceID?.call()) &&
      _identity.$1 != null &&
      _identity.$1!.isNotEmpty &&
      candidateIDValid(_identity.$2) &&
      _identity.$3 == null;
  bool get canEdit => personal && !busy && !resultUnknown && record != null;
  bool get changed => record != null && !profileRulesSame(record!.rules, draft);
  bool get needsCommunities =>
      draft.values.any((v) => v.visibility == 'COMMUNITY');
  List<String> get unavailableIDs => communitiesLoaded
      ? (draft.values
            .expand((r) => r.communityIDs)
            .toSet()
            .difference(communities.map((r) => r.id).toSet())
            .toList()
          ..sort())
      : const [];
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void synchronizeIdentity() {
    if (_closed || retired) return;
    if (_identity !=
            (
              authorizationHeader(),
              ownerID(),
              organizationWorkspaceID?.call(),
            ) ||
        current?.call() == false) {
      retired = true;
      ++_generation;
      ++_serial;
      ++_edit;
      record = null;
      draft = const {};
      communities = const [];
      communitiesLoaded = false;
      review = null;
      busy = false;
      resultUnknown = false;
      notice = null;
      error = '身份或入口来源已变化，请返回设置重新打开。';
      _notify();
    }
  }

  bool _valid(int g, int s) {
    synchronizeIdentity();
    return personal && g == _generation && s == _serial;
  }

  int? _begin() {
    synchronizeIdentity();
    if (!personal || busy) return null;
    final s = ++_serial;
    busy = true;
    review = null;
    error = null;
    notice = null;
    _notify();
    return _valid(_generation, s) ? s : null;
  }

  String _failure(Object e) => e is ProfileVisibilityHTTPError
      ? switch (e.status) {
          401 => '登录已失效，请重新登录后从设置打开。',
          403 => '当前身份或社群资格不允许保存，请重新读取并检查。',
          409 => '资料版本已变化，请重新读取，再检查新的受众设置。',
          404 => '当前本人资料不可读取，请返回设置核对。',
          400 => '受众设置未被接受，请重新检查字段和社群选择。',
          _ => '资料服务暂不可用，请重新读取核对。',
        }
      : e is CommunityApiException
      ? e.message
      : e is FormatException
      ? e.message
      : '读取未完成，请检查连接后重新核对。';

  Future<void> load() async {
    final unknown = resultUnknown, s = _begin(), g = _generation;
    if (s == null) return;
    try {
      if (!_valid(g, s)) return;
      final value = await api.read(_identity.$1!, _identity.$2!);
      if (!_valid(g, s)) return;
      if (_agentID != null && _agentID != value.agentID) {
        retire();
        return;
      }
      _agentID = value.agentID;
      record = value;
      draft = Map.unmodifiable(value.rules);
      ++_edit;
      communities = const [];
      communitiesLoaded = false;
      resultUnknown = false;
      if (unknown) notice = '已读取当前受众设置。这不能证明先前那次请求造成了当前状态；再次修改需重新检查。';
    } catch (e) {
      if (_valid(g, s)) {
        record = null;
        review = null;
        error = _failure(e);
      }
    } finally {
      if (_valid(g, s)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> loadCommunities() async {
    if (resultUnknown || record == null) return;
    final s = _begin(), g = _generation;
    if (s == null) return;
    try {
      if (!_valid(g, s)) return;
      final values = await api.communities(_identity.$1!, () => _valid(g, s));
      if (!_valid(g, s)) return;
      communities = values;
      communitiesLoaded = true;
    } catch (e) {
      if (_valid(g, s)) {
        communitiesLoaded = false;
        communities = const [];
        error = _failure(e);
      }
    } finally {
      if (_valid(g, s)) {
        busy = false;
        _notify();
      }
    }
  }

  void change(String field, ProfileFieldAudience value) {
    synchronizeIdentity();
    if (!canEdit || !profileVisibilityLabels.containsKey(field)) return;
    final originalIDs = record!.rules[field]!.communityIDs;
    if (value.visibility == 'COMMUNITY' &&
        value.communityIDs.any(
          (id) =>
              !originalIDs.contains(id) && !communities.any((r) => r.id == id),
        )) {
      error = '请从当前有效成员社群中明确选择。';
      _notify();
      return;
    }
    if (draft[field]!.same(value)) return;
    draft = Map.unmodifiable({...draft, field: value});
    ++_edit;
    review = null;
    error = null;
    notice = null;
    _notify();
  }

  ProfileVisibilityReview? prepareReview() {
    synchronizeIdentity();
    if (!canEdit || !changed) return null;
    review = ProfileVisibilityReview._(
      _generation,
      _serial,
      _edit,
      record!,
      Map.unmodifiable(draft),
    );
    _notify();
    return review;
  }

  void cancelReview() {
    review = null;
    _notify();
  }

  Future<void> save(ProfileVisibilityReview captured) async {
    synchronizeIdentity();
    if (!canEdit ||
        !identical(review, captured) ||
        captured.generation != _generation ||
        captured.serial != _serial ||
        captured.edit != _edit ||
        !identical(captured.original, record)) {
      return;
    }
    final g = _generation, s = ++_serial;
    busy = true;
    review = null;
    error = notice = null;
    _notify();
    try {
      if (!_valid(g, s)) return;
      final value = await api.replace(
        _identity.$1!,
        captured.original,
        captured.rules,
      );
      if (!_valid(g, s)) return;
      record = value;
      draft = Map.unmodifiable(value.rules);
      ++_edit;
      notice = '已收到受众设置的保存结果；不会开启模型、学习或改写资料内容。';
    } catch (e) {
      if (_valid(g, s)) {
        if (e is ProfileVisibilityHTTPError &&
            const {400, 401, 403, 404, 409, 413, 415}.contains(e.status)) {
          record = null;
          error = _failure(e);
        } else {
          resultUnknown = true;
          error = '保存结果暂未确认，请重新读取当前设置；不会自动重发。';
        }
      }
    } finally {
      if (_valid(g, s)) {
        busy = false;
        _notify();
      }
    }
  }

  void retire() {
    if (retired || _closed) return;
    retired = true;
    ++_generation;
    ++_serial;
    record = null;
    draft = const {};
    review = null;
    communities = const [];
    communitiesLoaded = false;
    busy = false;
    notice = null;
    error = '当前智能体来源已变化，请返回设置重新打开。';
    _notify();
  }

  @override
  void dispose() {
    _closed = true;
    ++_generation;
    ++_serial;
    api.dispose();
    super.dispose();
  }
}
