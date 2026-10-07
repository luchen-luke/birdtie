import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';

import 'business_api.dart';

dynamic _freezeBusiness(dynamic value) {
  if (value is Map<String, dynamic>) {
    return Map<String, dynamic>.unmodifiable(
      value.map((k, v) => MapEntry(k, _freezeBusiness(v))),
    );
  }
  if (value is List) {
    return List<dynamic>.unmodifiable(value.map(_freezeBusiness));
  }
  return value;
}

class BusinessDraft {
  BusinessDraft({
    required this.kind,
    required Map<String, dynamic> body,
    this.resource,
    this.placeID,
  }) : body =
           _freezeBusiness(jsonDecode(jsonEncode(body)))
               as Map<String, dynamic>;
  final String kind;
  final String? resource, placeID;
  final Map<String, dynamic> body;
}

class BusinessApproval {
  BusinessApproval._(this.generation, this.businessID, this.draft);
  final int generation;
  final String businessID;
  final BusinessDraft draft;
}

/// Ordinary Person management: business membership and review permissions
/// remain server facts. Every identity transition invalidates pending approval.
class BusinessConsoleController extends ChangeNotifier {
  BusinessConsoleController({
    required this.api,
    required this.accountID,
    required this.authorizationHeader,
    required this.organizationWorkspaceID,
  });
  final BusinessApi api;
  final String? Function() accountID,
      authorizationHeader,
      organizationWorkspaceID;
  (String?, String?, String?)? _identity;
  int _generation = 0, _request = 0;
  bool _closed = false;
  bool _newClaim = false;
  List<BusinessSummary> businesses = const [];
  BusinessConsoleSnapshot? snapshot;
  String? selectedID, error, message;
  BusinessDraft? draft;
  BusinessApproval? _approval;
  bool loading = false, saving = false, uncertain = false;
  bool get personal =>
      accountID() != null &&
      authorizationHeader() != null &&
      organizationWorkspaceID() == null;
  int get generation => _generation;

  void _notify() {
    if (!_closed) notifyListeners();
  }

  void synchronizeIdentity() {
    final next = (
      accountID(),
      authorizationHeader(),
      organizationWorkspaceID(),
    );
    if (_identity == next) return;
    _identity = next;
    _generation++;
    _request++;
    businesses = const [];
    snapshot = null;
    selectedID = null;
    _newClaim = false;
    draft = null;
    _approval = null;
    loading = saving = uncertain = false;
    error = message = null;
    _notify();
  }

  bool _current(int generation, int request) {
    synchronizeIdentity();
    return !_closed &&
        personal &&
        generation == _generation &&
        request == _request;
  }

  void _fail(Object e) {
    error = e is BusinessApiException ? e.message : '商家资料暂不可用，请重新读取。';
    if (e is BusinessApiException && const {401, 403, 404}.contains(e.status)) {
      snapshot = null;
      draft = null;
      _approval = null;
    }
  }

  Future<void> load() async {
    synchronizeIdentity();
    if (!personal || saving) return;
    final g = _generation, r = ++_request;
    loading = true;
    error = null;
    _notify();
    try {
      final items = await api.list();
      if (!_current(g, r)) return;
      businesses = items;
    } on Object catch (e) {
      if (_current(g, r)) _fail(e);
    } finally {
      if (_current(g, r)) {
        loading = false;
        _notify();
      }
    }
  }

  Future<void> select(String id) async {
    synchronizeIdentity();
    if (!personal || saving || !BusinessApi.validID(id)) return;
    selectedID = id;
    _newClaim = false;
    snapshot = null;
    draft = null;
    _approval = null;
    error = message = null;
    uncertain = false;
    await refresh();
  }

  static String newBusinessID() {
    final random = Random.secure();
    final bytes = List<int>.generate(16, (_) => random.nextInt(256));
    bytes[6] = (bytes[6] & 15) | 64;
    bytes[8] = (bytes[8] & 63) | 128;
    final h = bytes.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-${h.substring(16, 20)}-${h.substring(20)}';
  }

  void newClaim() {
    synchronizeIdentity();
    if (!personal || loading || saving || uncertain) return;
    _request++;
    selectedID = newBusinessID();
    _newClaim = true;
    snapshot = null;
    draft = null;
    _approval = null;
    error = message = null;
    _notify();
  }

  Future<void> refresh() async {
    synchronizeIdentity();
    final id = selectedID;
    if (!personal || saving || id == null) return;
    final g = _generation, r = ++_request;
    final wasUncertain = uncertain;
    loading = true;
    error = null;
    _approval = null;
    _notify();
    try {
      final value = await api.read(id);
      if (!_current(g, r) || selectedID != id) return;
      snapshot = value;
      _newClaim = false;
      draft = null;
      uncertain = false;
      if (wasUncertain) message = '已读取当前状态。它不证明上次操作是否唯一成功，请核对资料后再决定。';
    } on Object catch (e) {
      if (_current(g, r)) _fail(e);
    } finally {
      if (_current(g, r)) {
        loading = false;
        _notify();
      }
    }
  }

  bool _permitted(BusinessDraft d) => switch (d.kind) {
    'claim' =>
      (_newClaim && snapshot == null && selectedID != null && !uncertain) ||
          (snapshot?.canManage == true &&
              snapshot?.business.claimStatus != 'verified'),
    'profile' || 'venue' => snapshot?.canManage == true,
    'member' => snapshot?.canManageMembers == true,
    'review' => snapshot?.reviewPermissions.contains(d.resource) == true,
    _ => false,
  };
  bool _versionCurrent(BusinessDraft d) {
    if (d.kind == 'claim') {
      return (d.body['expectedVersion'] ?? 0) ==
          (snapshot?.claim?['version'] ?? 0);
    }
    final expected = d.body['expectedVersion'];
    if (expected is! int) return false;
    if (d.kind == 'member') return expected == snapshot?.membershipVersion;
    final resource = d.kind == 'review' ? d.resource : d.kind;
    final facts = switch (resource) {
      'claim' => snapshot?.claim,
      'profile' => snapshot?.profile,
      'venue' =>
        snapshot?.venues.where((v) => v['placeId'] == d.placeID).firstOrNull,
      _ => null,
    };
    if (d.kind == 'review' && facts == null) return false;
    return expected == (facts?['version'] ?? 0);
  }

  void edit(BusinessDraft value) {
    synchronizeIdentity();
    if (!personal || loading || saving || uncertain || !_permitted(value)) {
      return;
    }
    draft = value;
    _approval = null;
    error = message = null;
    _notify();
  }

  void cancelPreview() {
    _approval = null;
  }

  BusinessApproval? preview() {
    synchronizeIdentity();
    final d = draft, id = selectedID;
    if (!personal ||
        loading ||
        saving ||
        uncertain ||
        d == null ||
        id == null ||
        !_permitted(d) ||
        !_versionCurrent(d)) {
      return null;
    }
    return _approval = BusinessApproval._(_generation, id, d);
  }

  bool isApprovalCurrent(BusinessApproval approved) =>
      !_closed &&
      personal &&
      _identity ==
          (accountID(), authorizationHeader(), organizationWorkspaceID()) &&
      !loading &&
      !saving &&
      !uncertain &&
      identical(approved, _approval) &&
      approved.generation == _generation &&
      approved.businessID == selectedID &&
      identical(approved.draft, draft) &&
      _permitted(approved.draft) &&
      _versionCurrent(approved.draft);

  Future<void> submit(BusinessApproval approved) async {
    synchronizeIdentity();
    if (!isApprovalCurrent(approved)) {
      return;
    }
    final g = _generation,
        r = ++_request,
        id = approved.businessID,
        d = approved.draft;
    saving = true;
    _approval = null;
    error = message = null;
    _notify();
    try {
      switch (d.kind) {
        case 'claim':
          await api.submitClaim(id, d.body);
        case 'profile':
          await api.putProfile(id, d.body);
        case 'venue':
          await api.putVenue(id, d.placeID!, d.body);
        case 'member':
          await api.changeMember(id, d.body);
        case 'review':
          await api.review(id, d.resource!, d.body, placeID: d.placeID);
      }
      if (!_current(g, r)) return;
      // A successful acknowledgement is distinct from this subsequent read.
      uncertain = true;
      final current = await api.read(id);
      if (!_current(g, r)) return;
      snapshot = current;
      _newClaim = false;
      draft = null;
      uncertain = false;
      message = '服务器已接受操作，当前资料已重新读取。';
    } on Object catch (e) {
      if (!_current(g, r)) return;
      _fail(e);
      if (uncertain || e is BusinessApiException && e.outcomeUnknown) {
        uncertain = true;
        snapshot = null;
        draft = null;
        // Resolve by a real read, never resend a mutation after an unknown result.
        try {
          final current = await api.read(id);
          if (!_current(g, r)) return;
          snapshot = current;
          _newClaim = false;
          uncertain = false;
          message = '已读取当前状态。上次操作结果仍须按资料核对，未自动重发。';
        } on Object catch (_) {
          /* Preserve unknown status for explicit recovery. */
        }
      }
    } finally {
      if (_current(g, r)) {
        saving = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    _generation++;
    _request++;
    super.dispose();
  }
}
