import 'package:flutter/foundation.dart';

import 'community_api.dart';
import 'connections.dart';

class CommunityProposal {
  const CommunityProposal(
    this.approval,
    this.target,
    this.consequence,
    this._generation,
  );
  final CommunityApproval approval;
  final String target, consequence;
  final int _generation;
}

/// Current human management over existing Community actions. A proposal is
/// ephemeral and expires on account/session changes, reload or another action.
class CommunityMembersController extends ChangeNotifier {
  CommunityMembersController({
    required this.api,
    required this.communityId,
    required this.actorId,
    this.authority,
  }) {
    _seenIdentity = _identity;
    authority?.addListener(_changed);
  }
  final CommunityApi api;
  final String communityId;
  final String? Function() actorId;
  final Listenable? authority;
  int _generation = 0, _loadSerial = 0;
  bool _closed = false, loading = false, busy = false, unknownResult = false;
  String? message;
  String? friendMessage;
  CommunityItem? item;
  List<CommunityMember> members = const [], requests = const [];
  List<FriendTie> friends = const [];
  CommunityProposal? proposal;
  String? _seenIdentity;
  String? get _identity {
    final token = api.authorizationHeader(), who = actorId();
    return token == null || who == null ? null : '$who:$token';
  }

  void _notify() {
    if (!_closed) notifyListeners();
  }

  bool _current(int generation, String? identity) =>
      !_closed &&
      generation == _generation &&
      identity != null &&
      identity == _identity;
  void _changed() {
    if (_seenIdentity == _identity) return;
    _seenIdentity = _identity;
    invalidate();
  }

  void invalidate() {
    ++_generation;
    ++_loadSerial;
    item = null;
    members = const [];
    requests = const [];
    friends = const [];
    friendMessage = null;
    proposal = null;
    loading = false;
    busy = false;
    unknownResult = false;
    message = '账号已变化，请重新读取社群状态。';
    _notify();
  }

  void cancelProposal() {
    ++_generation;
    proposal = null;
    busy = false;
    _notify();
  }

  Future<void> reload({bool preserveMessage = false}) async {
    final gen = ++_generation, serial = ++_loadSerial, identity = _identity;
    proposal = null;
    loading = true;
    if (!preserveMessage) {
      message = null;
      unknownResult = false;
    }
    _notify();
    if (identity == null) {
      item = null;
      members = const [];
      requests = const [];
      friends = const [];
      loading = false;
      message = '请先登录个人账号。';
      _notify();
      return;
    }
    try {
      final group = await api.detail(communityId);
      if (!_current(gen, identity) || serial != _loadSerial) return;
      final people = group.joined
          ? await api.members(communityId)
          : <CommunityMember>[];
      final pending = group.managed
          ? await api.members(communityId, requests: true)
          : <CommunityMember>[];
      var ties = <FriendTie>[];
      friendMessage = null;
      if (group.managed) {
        try {
          ties = await ConnectionSource(
            client: api.followClient,
            apiBaseUrl: api.followApiBaseUrl,
            authorizationHeader: api.authorizationHeader,
          ).ties();
        } catch (_) {
          if (_current(gen, identity)) {
            friendMessage = '好友列表暂不可用，其他成员管理仍可使用。请刷新重试。';
          }
        }
      }
      if (!_current(gen, identity) || serial != _loadSerial) return;
      item = group;
      members = List.unmodifiable(people);
      requests = List.unmodifiable(pending);
      friends = List.unmodifiable(ties);
    } catch (e) {
      if (!_current(gen, identity)) return;
      item = null;
      members = const [];
      requests = const [];
      friends = const [];
      message = _error(e);
    } finally {
      if (_current(gen, identity) && serial == _loadSerial) {
        loading = false;
        _notify();
      }
    }
  }

  Future<void> prepare(
    CommunityAction action, {
    required String target,
    required String consequence,
  }) async {
    if (busy || loading) {
      return;
    }
    final identity = _identity, who = actorId();
    if (identity == null || who == null) {
      invalidate();
      return;
    }
    final gen = ++_generation;
    proposal = null;
    busy = true;
    message = null;
    unknownResult = false;
    _notify();
    try {
      final approval = await api.preview(action, actorId: who);
      if (!_current(gen, identity)) return;
      proposal = CommunityProposal(approval, target, consequence, gen);
    } catch (e) {
      if (_current(gen, identity)) message = _error(e);
    } finally {
      if (_current(gen, identity)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<bool> confirm() async {
    final p = proposal, identity = _identity;
    if (p == null ||
        busy ||
        !_current(p._generation, identity) ||
        p.approval.actorId != actorId()) {
      return false;
    }
    busy = true;
    message = null;
    _notify();
    try {
      await api.submit(p.approval);
      if (!_current(p._generation, identity)) return false;
      proposal = null;
      busy = false;
      message = '操作已完成，正在读取最新状态。';
      await reload(preserveMessage: true);
      return true;
    } catch (e) {
      if (!_current(p._generation, identity)) return false;
      unknownResult = e is! CommunityApiException || e.status >= 500;
      message = unknownResult ? '结果待核实，请先刷新当前状态，勿重复提交。' : _error(e);
      proposal = null;
      busy = false;
      await reload(preserveMessage: true);
      return false;
    } finally {
      if (_current(p._generation, identity)) {
        busy = false;
        _notify();
      }
    }
  }

  String _error(Object e) =>
      e is CommunityApiException ? e.message : '网络暂不可用，请稍后刷新。';
  @override
  void dispose() {
    _closed = true;
    authority?.removeListener(_changed);
    super.dispose();
  }
}
