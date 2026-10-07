import 'dart:async';
import 'package:flutter/foundation.dart';
import 'agent_profile_api.dart';
import 'agent_memory_correction_api.dart';
import 'person_community_interest_api.dart';
import 'activity_participation_disclosure_api.dart';

const agentProfileSources = [
  'profile',
  'memories',
  'policies',
  'interests',
  'participations',
];

/// A short lived human display snapshot, never a model/source grant.
class AgentProfileController extends ChangeNotifier {
  AgentProfileController({
    required this.api,
    required String? Function() authorizationHeader,
    required String? Function() accountID,
    String? Function()? organizationWorkspaceID,
  }) : _authorization = authorizationHeader,
       _account = accountID,
       _workspace = organizationWorkspaceID ?? (() => null) {
    _identity = (_authorization(), _account(), _workspace());
  }
  final AgentProfileAPI api;
  final String? Function() _authorization, _account, _workspace;
  late final (String?, String?, String?) _identity;
  bool retired = false, _disposed = false;
  int _generation = 0;
  String? _agent;
  final values = <String, Object>{};
  final errors = <String, String>{};
  final loading = <String>{};
  final _timers = <String, Timer>{};
  bool get current =>
      !_disposed &&
      !retired &&
      _identity == (_authorization(), _account(), _workspace()) &&
      _identity.$1 != null &&
      _identity.$1!.isNotEmpty &&
      _identity.$2 != null &&
      _identity.$3 == null;
  bool get busy => loading.isNotEmpty;
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void identityChanged() {
    if (_identity != (_authorization(), _account(), _workspace())) retire();
  }

  void retire() {
    if (retired) return;
    retired = true;
    ++_generation;
    _clear();
    _notify();
  }

  void _clear() {
    for (final t in _timers.values) {
      t.cancel();
    }
    _timers.clear();
    values.clear();
    errors.clear();
    loading.clear();
    _agent = null;
  }

  bool _valid(int generation) {
    identityChanged();
    return current && generation == _generation;
  }

  Duration _remaining(Stopwatch request, {DateTime? observed, DateTime? end}) {
    var result = const Duration(seconds: 30) - request.elapsed;
    if (observed != null && end != null) {
      final relative = end.difference(observed) - request.elapsed;
      final absolute = end.difference(DateTime.now().toUtc());
      if (relative < result) result = relative;
      if (absolute < result) result = absolute;
    }
    return result;
  }

  Future<void> _read<T extends Object>(
    String key,
    int generation,
    Future<T> Function(String, String) call,
    String Function(T) agent, {
    DateTime? Function(T)? observed,
    DateTime? Function(T)? end,
  }) async {
    final request = Stopwatch()..start();
    loading.add(key);
    _notify();
    try {
      final value = await call(_identity.$1!, _identity.$2!);
      if (!_valid(generation)) return;
      final id = agent(value);
      if (id.isNotEmpty) {
        if (_agent != null && _agent != id) {
          retire();
          return;
        }
        _agent = id;
      }
      final remaining = _remaining(
        request,
        observed: observed?.call(value),
        end: end?.call(value),
      );
      if (remaining <= Duration.zero) {
        errors[key] = '读取已到期，请刷新当前资料。';
        return;
      }
      values[key] = value;
      errors.remove(key);
      _timers.remove(key)?.cancel();
      _timers[key] = Timer(remaining, () {
        if (_valid(generation)) {
          values.remove(key);
          errors[key] = '读取已到期，请刷新当前资料。';
          _notify();
        }
      });
    } catch (e) {
      if (!_valid(generation)) return;
      if (e is AgentProfileHTTPError && {401, 403}.contains(e.status)) {
        retire();
        return;
      }
      values.remove(key);
      errors[key] = e is AgentProfileHTTPError && e.status == 404
          ? '当前记录不可用。'
          : '暂时无法读取，请刷新重试。';
    } finally {
      if (_valid(generation)) {
        loading.remove(key);
        _notify();
      }
    }
  }

  Future<void> load() async {
    identityChanged();
    if (!current || busy) return;
    ++_generation;
    _clear();
    final g = _generation;
    await Future.wait([
      _read('profile', g, api.profile, (v) => v.agentID),
      _read(
        'memories',
        g,
        api.memories,
        (v) => v.isEmpty ? '' : v.first.agentID,
      ),
      _read(
        'policies',
        g,
        api.policies,
        (v) => v.agentID,
        observed: (v) => v.observedAt,
        end: (v) {
          final dates =
              v.records.values
                  .where((r) => r.status == 'ACTIVE')
                  .map((r) => r.expiresAt!)
                  .toList()
                ..sort();
          return dates.isEmpty ? null : dates.first;
        },
      ),
      _read('interests', g, api.interests, (v) => v.agentID),
      _read(
        'participations',
        g,
        api.participations,
        (v) => v.agentID,
        observed: (v) => v.observedAt,
        end: (v) {
          final dates = <DateTime>[
            for (final r in v.records) ...[
              if (r.sourceAvailable) r.sourceExpiresAt!,
              if (r.effectivePublic) r.disclosureExpiresAt!,
            ],
          ]..sort();
          return dates.isEmpty ? null : dates.first;
        },
      ),
    ]);
  }

  Future<void> detail(String id) async {
    identityChanged();
    if (!current || loading.contains('detail')) return;
    final rows = values['memories'] as List<CorrectableMemory>?;
    if (rows == null || !rows.any((m) => m.id == id)) return;
    values.remove('detail');
    errors.remove('detail');
    _timers.remove('detail')?.cancel();
    await _read(
      'detail',
      _generation,
      (token, owner) => api.detail(token, owner, id),
      (v) => v.agentID,
      observed: (v) => v.observedAt,
      end: (v) => v.expiresAt,
    );
  }

  AgentPrivateProfileView? get profile =>
      values['profile'] as AgentPrivateProfileView?;
  List<CorrectableMemory>? get memories =>
      values['memories'] as List<CorrectableMemory>?;
  AgentPoliciesView? get policies => values['policies'] as AgentPoliciesView?;
  CommunityInterestView? get interests =>
      values['interests'] as CommunityInterestView?;
  ParticipationDisclosureView? get participations =>
      values['participations'] as ParticipationDisclosureView?;
  AgentMemoryDetailView? get selectedDetail =>
      values['detail'] as AgentMemoryDetailView?;
  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    ++_generation;
    _clear();
    api.close();
    super.dispose();
  }
}
