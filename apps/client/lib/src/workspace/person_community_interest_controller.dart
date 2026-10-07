import 'package:flutter/foundation.dart';
import 'person_community_interest_api.dart';

class CommunityInterestIdentity {
  const CommunityInterestIdentity(this.auth, this.owner, this.workspace);
  final String? auth, owner, workspace;
  bool get personal => auth != null && owner != null && workspace == null;
  bool same(CommunityInterestIdentity v) =>
      auth == v.auth && owner == v.owner && workspace == v.workspace;
}

class PersonCommunityInterestController extends ChangeNotifier {
  PersonCommunityInterestController({
    required this.api,
    required this.identity,
    DateTime Function()? now,
  }) : now = now ?? DateTime.now,
       _bound = identity();
  final PersonCommunityInterestAPI api;
  final CommunityInterestIdentity Function() identity;
  final DateTime Function() now;
  CommunityInterestIdentity _bound;
  bool _closed = false;
  int _serial = 0;
  int generation = 0;
  bool busy = false, unknown = false, resultOnly = false;
  String? message, selectedID;
  String visibility = 'PRIVATE';
  CommunityInterestView? own, available;
  CommunityInterestPreview? review;
  bool get current => !_closed && _bound.personal && _bound.same(identity());
  bool get canPrepare => current && !busy && !unknown;
  void sync() {
    final v = identity();
    if (_bound.same(v)) return;
    _bound = v;
    ++_serial;
    ++generation;
    busy = false;
    unknown = false;
    resultOnly = false;
    own = null;
    available = null;
    review = null;
    selectedID = null;
    visibility = 'PRIVATE';
    message = null;
    if (!_closed) notifyListeners();
  }

  bool _valid(int serial, CommunityInterestIdentity b) =>
      !_closed && serial == _serial && b.same(identity()) && b.same(_bound);
  void _notify() {
    if (!_closed) notifyListeners();
  }

  Future<void> load() async {
    sync();
    if (!current) return;
    final b = _bound, serial = ++_serial;
    final recoveringUnknown = unknown;
    busy = true;
    review = null;
    _notify();
    try {
      final list = await api.read(b.auth!, b.owner!);
      if (!_valid(serial, b)) return;
      final options = await api.read(b.auth!, b.owner!, options: true);
      if (!_valid(serial, b)) return;
      if (list.agentID != options.agentID) {
        throw const FormatException('当前 Agent 已变化');
      }
      own = list;
      resultOnly = false;
      available = options;
      unknown = false;
      if (!options.options.any((o) => o.id == selectedID)) selectedID = null;
      message = recoveringUnknown ? '已读取当前权威声明；这不能证明先前未知提交是否成功。' : '已读取当前兴趣声明。';
    } catch (_) {
      if (_valid(serial, b)) {
        own = null;
        available = null;
        message = '暂时无法读取，请稍后刷新。';
      }
    } finally {
      if (_valid(serial, b)) {
        busy = false;
        _notify();
      }
    }
  }

  void select(String? id) {
    if (!canPrepare) return;
    if (id != null && available?.options.any((v) => v.id == id) != true) return;
    selectedID = id;
    review = null;
    _notify();
  }

  void chooseVisibility(String value) {
    if (!canPrepare || !['PRIVATE', 'PUBLIC'].contains(value)) return;
    visibility = value;
    review = null;
    _notify();
  }

  Future<CommunityInterestPreview?> prepare(String id, String operation) async {
    sync();
    if (!canPrepare) return null;
    final validOption = available?.options.any((v) => v.id == id) == true;
    final ownRecord = own?.records
        .where((v) => v.communityID == id)
        .firstOrNull;
    // An own result is only a selector for a new native preview, never approval.
    if (operation == 'PUBLIC' &&
        !validOption &&
        ownRecord?.sourceAvailable != true) {
      message = '当前来源不可公开，请刷新后检查。';
      _notify();
      return null;
    }
    if (!validOption && ownRecord == null) return null;
    final b = _bound, serial = ++_serial;
    busy = true;
    review = null;
    message = null;
    _notify();
    try {
      final p = await api.preview(b.auth!, b.owner!, id, operation);
      if (!_valid(serial, b)) return null;
      if (!p.expiresAt.isAfter(now())) {
        message = '具体预览已到期，请重新检查。';
        return null;
      }
      if (own == null || p.agentID != own!.agentID) {
        throw const FormatException('当前 Agent 已变化');
      }
      review = p;
      return p;
    } on CommunityInterestHTTPError catch (e) {
      if (_valid(serial, b)) {
        if ([400, 401, 403, 409].contains(e.status)) {
          own = null;
          available = null;
          selectedID = null;
          resultOnly = false;
        }
        message = '当前来源或具体版本已变化，请刷新后检查。';
      }
      return null;
    } catch (_) {
      if (_valid(serial, b)) message = '当前来源或具体版本已变化，请刷新后检查。';
      return null;
    } finally {
      if (_valid(serial, b)) {
        busy = false;
        _notify();
      }
    }
  }

  bool reviewCurrent(CommunityInterestPreview p, int capturedGeneration) =>
      current &&
      !busy &&
      !unknown &&
      capturedGeneration == generation &&
      identical(review, p) &&
      p.expiresAt.isAfter(now());
  Future<void> approve(
    CommunityInterestPreview p,
    int capturedGeneration,
  ) async {
    sync();
    if (!reviewCurrent(p, capturedGeneration)) return;
    final b = _bound, serial = ++_serial;
    busy = true;
    review = null;
    _notify();
    try {
      final v = await api.approve(b.auth!, p);
      if (!_valid(serial, b)) return;
      own = v;
      resultOnly = true;
      available = null;
      selectedID = null;
      message = '已收到该具体声明操作的权威结果。';
    } on CommunityInterestHTTPError catch (e) {
      if (!_valid(serial, b)) return;
      if (e.status == 400 ||
          e.status == 401 ||
          e.status == 403 ||
          e.status == 409) {
        own = null;
        available = null;
        message = '操作未获当前批准，请刷新并重新检查具体版本。';
      } else {
        unknown = true;
        message = '提交结果未知。请只读刷新核查，勿重复批准旧预览。';
      }
    } catch (_) {
      if (_valid(serial, b)) {
        unknown = true;
        message = '提交结果未知。请只读刷新核查，勿重复批准旧预览。';
      }
    } finally {
      if (_valid(serial, b)) {
        busy = false;
        _notify();
      }
    }
  }

  void cancel() {
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

  @override
  void dispose() {
    _closed = true;
    ++_serial;
    api.dispose();
    super.dispose();
  }
}
