import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'business_api.dart';
import 'business_knowledge_api.dart';

class BusinessKnowledgeVenue {
  const BusinessKnowledgeVenue(this.id, this.name);
  final String id, name;
}

/// Borrows the Console transport. Native permission is checked on every query.
class BusinessKnowledgeController extends ChangeNotifier {
  BusinessKnowledgeController({
    required this.api,
    required this.businessID,
    required this.accountID,
    required this.authorizationHeader,
    required this.workspaceID,
    required this.currentBusinessID,
    required this.bindingCurrent,
    required this.sourceFrame,
    required this.identityChanges,
    DateTime Function()? now,
  }) : now = now ?? (() => DateTime.now().toUtc()) {
    _identity = _capture();
    identityChanges.addListener(synchronize);
  }
  final BusinessApi api;
  final String businessID;
  final String? Function() accountID,
      authorizationHeader,
      workspaceID,
      currentBusinessID;
  final bool Function() bindingCurrent;
  final Object? Function() sourceFrame;
  final Listenable identityChanges;
  final DateTime Function() now;
  late Object _identity;
  bool _closed = false;
  bool _invalidBinding = false;
  bool _notificationPending = false;
  int _serial = 0;
  Timer? _expiry;
  bool loading = false, ready = false;
  String? error, query, placeID;
  String businessName = '';
  List<BusinessKnowledgeVenue> venues = const [];
  BusinessKnowledgeAnswer? answer;
  Object _capture() => (
    accountID(),
    authorizationHeader(),
    api.authorizationHeader(),
    workspaceID(),
    currentBusinessID(),
    bindingCurrent(),
    sourceFrame(),
  );
  bool get permitted =>
      !_closed &&
      !_invalidBinding &&
      bindingCurrent() &&
      BusinessApi.validID(accountID() ?? '') &&
      (authorizationHeader()?.startsWith('Bearer ') ?? false) &&
      api.authorizationHeader() == authorizationHeader() &&
      workspaceID() == null &&
      currentBusinessID() == businessID;
  void synchronize() {
    if (_closed) return;
    final next = _capture();
    if (next != _identity) {
      _identity = next;
      _invalidBinding = true;
      invalidate('工作身份、商家或资料已变化，请返回工作台重新进入。');
    }
  }

  void invalidate([String? message]) {
    if (_closed) return;
    _serial++;
    _expiry?.cancel();
    answer = null;
    ready = false;
    venues = const [];
    businessName = '';
    query = null;
    placeID = null;
    loading = false;
    error = message;
    notifyListeners();
  }

  bool _current(int serial, Object identity) {
    synchronize();
    return !_closed && permitted && serial == _serial && identity == _identity;
  }

  Future<BusinessConsoleSnapshot?> _read(int serial, Object identity) async {
    final snapshot = await api.read(businessID);
    if (!_current(serial, identity)) return null;
    if (!snapshot.canManage ||
        !const {'owner', 'admin'}.contains(snapshot.business.role)) {
      throw const BusinessApiException(403);
    }
    return snapshot;
  }

  List<BusinessKnowledgeVenue> _venues(BusinessConsoleSnapshot snapshot) {
    final seen = <String>{};
    return List.unmodifiable([
      for (final v in snapshot.venues)
        if (v['placeId'] is String &&
            BusinessApi.validID(v['placeId']) &&
            v['placeName'] is String &&
            (v['placeName'] as String).trim().isNotEmpty &&
            v['operationStatus'] == 'verified' &&
            seen.add(v['placeId']))
          BusinessKnowledgeVenue(v['placeId'], v['placeName']),
    ]);
  }

  Future<void> load() async {
    synchronize();
    if (!permitted) {
      invalidate('当前身份不能查看商家管理资料。');
      return;
    }
    final serial = ++_serial;
    final identity = _identity;
    answer = null;
    ready = false;
    loading = true;
    error = null;
    notifyListeners();
    try {
      final s = await _read(serial, identity);
      if (s == null) return;
      businessName = s.business.name;
      venues = _venues(s);
      ready = true;
    } on BusinessApiException catch (e) {
      if (_current(serial, identity)) {
        error = e.message;
        venues = const [];
        businessName = '';
      }
    } finally {
      if (_current(serial, identity)) {
        loading = false;
        notifyListeners();
      }
    }
  }

  void selectQuestion(String value) {
    synchronize();
    if (!ready ||
        loading ||
        !permitted ||
        !businessKnowledgeQuestions.contains(value)) {
      return;
    }
    _serial++;
    _expiry?.cancel();
    query = value;
    placeID = null;
    answer = null;
    error = null;
    notifyListeners();
  }

  void selectPlace(String value) {
    synchronize();
    if (!ready || loading || !permitted || !venues.any((v) => v.id == value)) {
      return;
    }
    _serial++;
    _expiry?.cancel();
    placeID = value;
    answer = null;
    error = null;
    notifyListeners();
  }

  bool get canAsk =>
      permitted &&
      ready &&
      !loading &&
      query != null &&
      (!businessKnowledgeNeedsPlace(query!) ||
          venues.any((v) => v.id == placeID));
  Future<void> ask() async {
    synchronize();
    if (!canAsk) return;
    final q = query!, p = businessKnowledgeNeedsPlace(q) ? placeID! : '';
    final serial = ++_serial;
    final identity = _identity;
    _expiry?.cancel();
    answer = null;
    loading = true;
    error = null;
    notifyListeners();
    try {
      // Re-read membership and concrete target instead of trusting an old Console.
      final s = await _read(serial, identity);
      if (s == null) return;
      if (p.isNotEmpty && !_venues(s).any((v) => v.id == p)) {
        throw const BusinessApiException(409);
      }
      final result = await api.askKnowledge(
        businessID,
        q,
        placeID: p,
        now: now(),
      );
      if (!_current(serial, identity)) return;
      if (!result.currentAt(now())) throw const BusinessApiException(409);
      answer = result;
      if (result.sources.isNotEmpty) {
        final until = result.sources.first.validUntil;
        _expiry = Timer(until.difference(now()), () {
          if (_current(serial, identity)) invalidate('这次回答的资料已到期，请重新读取当前资料。');
        });
      }
    } on BusinessApiException catch (e) {
      if (_current(serial, identity)) {
        answer = null;
        error = e.message;
        if (const {401, 403, 404, 409}.contains(e.status)) {
          ready = false;
          venues = const [];
          businessName = '';
          placeID = null;
        }
      }
    } finally {
      if (_current(serial, identity)) {
        loading = false;
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    _serial++;
    _expiry?.cancel();
    identityChanges.removeListener(synchronize);
    // The parent owns the borrowed transport.
    super.dispose();
  }

  @override
  void notifyListeners() {
    // Retire authority synchronously; repaint sibling routes after parent build.
    if (_closed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_notificationPending) return;
      _notificationPending = true;
      SchedulerBinding.instance.addPostFrameCallback((_) {
        _notificationPending = false;
        if (!_closed) super.notifyListeners();
      });
    } else {
      super.notifyListeners();
    }
  }
}
