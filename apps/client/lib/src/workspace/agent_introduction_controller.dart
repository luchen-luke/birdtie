import 'package:flutter/foundation.dart';
import 'model_egress_api.dart' show egressIDValid;
import 'agent_introduction_api.dart';

class IntroductionPolicyReview {
  IntroductionPolicyReview(
    this.original,
    Map<String, String> values,
    this.expiry,
  ) : rules = Map.unmodifiable(values);
  final IntroductionPolicy original;
  final Map<String, String> rules;
  final DateTime expiry;
}

class AgentIntroductionController extends ChangeNotifier {
  AgentIntroductionController({
    required this.api,
    required this.authorizationHeader,
    required this.accountID,
    required this.organizationWorkspaceID,
    this.initialSourceIntentID,
  });
  final AgentIntroductionAPI api;
  final String? Function() authorizationHeader,
      accountID,
      organizationWorkspaceID;
  final String? initialSourceIntentID;
  (String?, String?, String?)? _identity;
  int generation = 0, _serial = 0;
  bool _closed = false, busy = false, unknownSave = false;
  bool? consent;
  String? message, selectedID;
  List<IntroductionIntent> intents = const [];
  IntroductionPolicy? policy;
  IntroductionResult? result;
  IntroductionPolicyReview? review;
  bool _initialApplied = false;
  bool get personal =>
      authorizationHeader() != null &&
      egressIDValid(accountID()) &&
      organizationWorkspaceID() == null;
  bool get identityCurrent =>
      !_closed &&
      personal &&
      _identity ==
          (authorizationHeader(), accountID(), organizationWorkspaceID());
  String? get currentSourceID =>
      identityCurrent && !busy && !unknownSave ? selected?.id : null;
  DateTime get now => DateTime.now().toUtc();
  List<IntroductionIntent> get available =>
      intents.where((i) => i.current(now)).toList(growable: false);
  IntroductionIntent? get selected {
    for (final i in available) {
      if (i.id == selectedID) return i;
    }
    return null;
  }

  bool get canSearch =>
      identityCurrent &&
      !busy &&
      !unknownSave &&
      consent == true &&
      selected != null &&
      policy?.reviewEnabled == true &&
      policy!.expiry!.isAfter(now);
  void synchronizeIdentity() {
    final v = (authorizationHeader(), accountID(), organizationWorkspaceID());
    if (v == _identity) return;
    _identity = v;
    generation++;
    _serial++;
    busy = false;
    unknownSave = false;
    consent = null;
    message = null;
    selectedID = null;
    intents = const [];
    policy = null;
    result = null;
    review = null;
    _initialApplied = false;
  }

  bool _current(int serial, int g) =>
      !_closed &&
      personal &&
      serial == _serial &&
      g == generation &&
      _identity ==
          (authorizationHeader(), accountID(), organizationWorkspaceID());
  Future<void> load() async {
    synchronizeIdentity();
    if (!personal || busy || _closed) return;
    final serial = ++_serial,
        g = generation,
        token = authorizationHeader()!,
        owner = accountID()!;
    busy = true;
    review = null;
    result = null;
    message = null;
    notifyListeners();
    try {
      final rows = await Future.wait<dynamic>([
        api.consent(token),
        api.intents(token, owner),
        api.policy(token, owner),
      ]);
      if (!_current(serial, g)) return;
      consent = rows[0] as bool;
      intents = rows[1] as List<IntroductionIntent>;
      policy = rows[2] as IntroductionPolicy;
      if (selectedID != null && selected == null) selectedID = null;
      if (!_initialApplied) {
        _initialApplied = true;
        if (initialSourceIntentID != null) {
          if (available.any((i) => i.id == initialSourceIntentID)) {
            selectedID = initialSourceIntentID;
          } else {
            message = '原意图已变化，请重新选择本人当前公开的找搭子意图。';
          }
        }
      }
      if (unknownSave) {
        unknownSave = false;
        message = '已重新读取当前权威设置。请检查实际值；没有自动重复保存或续期。';
      }
    } catch (_) {
      if (_current(serial, g)) {
        policy = null;
        consent = null;
        intents = const [];
        selectedID = null;
        message = unknownSave ? '保存结果仍无法核实，请重读原设置；不会自动重发。' : '本人设置或意图暂不可用，请刷新。';
      }
    } finally {
      if (_current(serial, g)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  void choose(String id) {
    synchronizeIdentity();
    if (!personal ||
        busy ||
        unknownSave ||
        _closed ||
        !available.any((i) => i.id == id)) {
      return;
    }
    ++_serial;
    selectedID = id;
    result = null;
    review = null;
    message = null;
    notifyListeners();
  }

  Future<void> search() async {
    synchronizeIdentity();
    if (!canSearch || _closed) return;
    final serial = ++_serial,
        g = generation,
        token = authorizationHeader()!,
        owner = accountID()!,
        source = selectedID!;
    busy = true;
    result = null;
    review = null;
    message = null;
    notifyListeners();
    try {
      final r = await api.suggestions(token, owner, source);
      if (!_current(serial, g) || selectedID != source) return;
      if (!r.expiry.isAfter(now) || selected == null) {
        throw const FormatException('建议已过期');
      }
      result = r;
    } catch (e) {
      if (_current(serial, g)) {
        message = e is IntroductionFailure && [400, 403, 409].contains(e.status)
            ? '当前公开来源或设置已变化，请刷新后重选。不会显示其他人的私密设置。'
            : '建议暂不可用，请重试。没有发出申请或消息。';
      }
    } finally {
      if (_current(serial, g)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  bool candidateCurrent(IntroductionCandidate c) =>
      canSearch &&
      selected != null &&
      result?.sourceID == selectedID &&
      result!.expiry.isAfter(now) &&
      c.expiry.isAfter(now) &&
      result!.candidates.any((v) => identical(v, c)) &&
      !busy &&
      !unknownSave;
  void preparePolicy({
    required bool unknownPerson,
    required bool community,
    bool? sharedActivity,
    required int days,
  }) {
    synchronizeIdentity();
    final p = policy;
    if (!personal ||
        busy ||
        unknownSave ||
        _closed ||
        p == null ||
        ![1, 7, 30].contains(days)) {
      return;
    }
    final values = Map<String, String>.from(p.rules);
    values['UNKNOWN_PERSON'] = unknownPerson ? 'REVIEW_REQUIRED' : 'DISABLED';
    values['SHARED_COMMUNITY'] = community ? 'REVIEW_REQUIRED' : 'DISABLED';
    if (sharedActivity != null) {
      values['SHARED_ACTIVITY'] = sharedActivity
          ? 'REVIEW_REQUIRED'
          : 'DISABLED';
    }
    review = IntroductionPolicyReview(p, values, now.add(Duration(days: days)));
    message = null;
    notifyListeners();
  }

  void cancelPolicy() {
    review = null;
    if (!_closed) notifyListeners();
  }

  Future<void> savePolicy(IntroductionPolicyReview chosen) async {
    synchronizeIdentity();
    if (!personal ||
        busy ||
        unknownSave ||
        _closed ||
        !identical(review, chosen) ||
        !identical(policy, chosen.original) ||
        !chosen.expiry.isAfter(now)) {
      return;
    }
    final serial = ++_serial, g = generation, token = authorizationHeader()!;
    busy = true;
    result = null;
    message = null;
    notifyListeners();
    try {
      final p = await api.saveSocial(
        token,
        chosen.original,
        chosen.rules,
        chosen.expiry,
      );
      if (!_current(serial, g)) return;
      policy = p;
      review = null;
      message = '本人社交偏好已保存。它不会发送引荐、申请或模型请求。';
    } catch (e) {
      if (_current(serial, g)) {
        review = null;
        if (e is IntroductionFailure && e.status >= 400 && e.status < 500) {
          policy = null;
          message = '版本、身份或期限已变化。请重新读取并检查后再决定，不会自动重复保存。';
        } else {
          unknownSave = true;
          message = '保存结果待核实，请重新读取原策略；不会盲目重发。';
        }
      }
    } finally {
      if (_current(serial, g)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  void expire() {
    if (_closed || !personal) return;
    var changed = false;
    if (selectedID != null && selected == null) {
      selectedID = null;
      result = null;
      changed = true;
    }
    if (result != null &&
        (!result!.expiry.isAfter(now) ||
            result!.candidates.any((c) => !c.expiry.isAfter(now)))) {
      result = null;
      changed = true;
    }
    if (review != null && !review!.expiry.isAfter(now)) {
      review = null;
      changed = true;
    }
    if (policy?.expiry != null &&
        !policy!.expiry!.isAfter(now) &&
        result != null) {
      result = null;
      changed = true;
    }
    if (changed) {
      message = '当前来源或期限已变化，请刷新核实；没有发送申请。';
      notifyListeners();
    }
  }

  @override
  void dispose() {
    _closed = true;
    generation++;
    _serial++;
    api.dispose();
    super.dispose();
  }
}
