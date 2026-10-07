import 'package:flutter/foundation.dart';
import 'active_social_intent_controller.dart' show ActiveIntentIdentity;
import 'intent_activity_conversion_api.dart';

class IntentActivityConversionController extends ChangeNotifier {
  IntentActivityConversionController({
    required this.api,
    required this.identity,
    required this.intentID,
    DateTime Function()? now,
  }) : now = now ?? DateTime.now,
       _bound = identity();
  final IntentActivityConversionAPI api;
  final ActiveIntentIdentity Function() identity;
  final String intentID;
  final DateTime Function() now;
  ActiveIntentIdentity _bound;
  bool _closed = false, _retired = false;
  int generation = 0, _serial = 0;
  bool busy = false, unknown = false;
  String? message, _agent;
  ConversionOptions? options;
  ConversionPreview? review;
  ConversionReceipt? receipt;
  ConversionChoice? completedChoice;
  bool get current =>
      !_closed && !_retired && _bound.personal && _bound.same(identity());
  bool get ready => current && !busy && !unknown;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void sync() {
    if (_bound.same(identity())) return;
    _bound = identity();
    _retired = true;
    generation++;
    _serial++;
    busy = false;
    unknown = false;
    options = null;
    review = null;
    receipt = null;
    completedChoice = null;
    message = '账号或工作区已变化，请重新打开。';
    _notify();
  }

  bool _valid(int serial, ActiveIntentIdentity b) =>
      current && serial == _serial && b.same(_bound) && b.same(identity());
  bool reviewCurrent(ConversionPreview p, int epoch) =>
      ready &&
      generation == epoch &&
      identical(review, p) &&
      p.expiresAt.isAfter(now()) &&
      p.agentID == _agent;
  void expire() {
    if (review != null && !review!.expiresAt.isAfter(now())) {
      review = null;
      message = '具体预览已到期，请重新检查当前来源。';
      _notify();
    }
  }

  void discard() {
    review = null;
    _notify();
  }

  Future<void> load() async {
    sync();
    if (!current || busy) return;
    final serial = ++_serial, b = _bound, recovering = unknown;
    busy = true;
    review = null;
    _notify();
    try {
      final v = await api.options(b.auth!, b.owner!, intentID);
      if (!_valid(serial, b)) return;
      if (_agent != null && _agent != v.agentID) {
        throw const FormatException('当前 Agent 已变化');
      }
      _agent = v.agentID;
      options = v;
      completedChoice = v.intent.linked
          ? v.choices
                .where(
                  (c) =>
                      c.activity.activityId ==
                          v.intent.value["convertedActivityId"] &&
                      c.participationID ==
                          v.intent.value["convertedParticipationId"],
                )
                .firstOrNull
          : null;
      unknown = false;
      message = recovering
          ? (v.intent.linked
                ? '已核对原意图当前关联；不会重新提交。'
                : '已核对原意图当前状态，不能证明上次未知提交成功。')
          : null;
    } catch (_) {
      if (_valid(serial, b)) {
        options = null;
        receipt = null;
        completedChoice = null;
        message = '暂时无法读取当前意图与报名，请重试。';
      }
    } finally {
      if (_valid(serial, b)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<ConversionPreview?> prepare(ConversionChoice selected) async {
    sync();
    if (!ready ||
        options == null ||
        options!.intent.linked ||
        !['ACTIVE', 'MATCHED'].contains(options!.intent.status) ||
        !options!.choices.contains(selected)) {
      return null;
    }
    final serial = ++_serial, b = _bound, v = options!;
    busy = true;
    review = null;
    _notify();
    try {
      final p = await api.preview(
        b.auth!,
        b.owner!,
        intentID,
        selected.activity.activityId,
        v.version,
      );
      if (!_valid(serial, b)) return null;
      if (p.agentID != _agent || !p.expiresAt.isAfter(now())) {
        throw const FormatException('来源或期限已变化');
      }
      review = p;
      message = null;
      return p;
    } catch (_) {
      if (_valid(serial, b)) {
        options = null;
        message = '来源或意图已变化，请刷新后重新选择。';
      }
      return null;
    } finally {
      if (_valid(serial, b)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> approve(ConversionPreview p, int epoch) async {
    sync();
    if (!reviewCurrent(p, epoch)) return;
    final serial = ++_serial, b = _bound;
    busy = true;
    review = null;
    _notify();
    try {
      final v = await api.approve(b.auth!, b.owner!, intentID, p);
      if (!_valid(serial, b)) return;
      receipt = v;
      completedChoice = p.choice;
      options = null;
      unknown = false;
      message = v.explanation;
    } catch (e) {
      if (_valid(serial, b)) {
        options = null;
        receipt = null;
        completedChoice = null;
        // Even a 403/409 may arrive from the post-commit encoding fence.
        // Only the original authority read can reconcile this unknown response.
        unknown = true;
        message = unknown
            ? '提交结果未知，请读取原意图核对；不会自动重新批准。'
            : '未获得可核验的关联结果，请刷新当前状态。';
      }
    } finally {
      if (_valid(serial, b)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    _serial++;
    review = null;
    options = null;
    receipt = null;
    completedChoice = null;
    api.close();
    super.dispose();
  }
}
