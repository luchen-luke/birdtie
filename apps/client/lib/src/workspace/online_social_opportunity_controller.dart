import 'dart:async';
import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'online_social_opportunity_api.dart';

typedef OnlineDiscoveryIdentity = (String?, String?, String?);

class OnlineSocialOpportunityController extends ChangeNotifier {
  OnlineSocialOpportunityController({
    required this.api,
    required this.identity,
  }) {
    captured = identity();
  }
  final OnlineSocialOpportunityAPI api;
  final OnlineDiscoveryIdentity Function() identity;
  late final OnlineDiscoveryIdentity captured;
  OnlineOpportunityView? options, view;
  String? selectedID, error;
  bool busy = false, _retired = false, _disposed = false, _queued = false;
  int _epoch = 0, _serial = 0;
  Timer? _timer;
  bool get current =>
      !_disposed &&
      !_retired &&
      captured == identity() &&
      captured.$1 != null &&
      captured.$2 != null &&
      captured.$3 == null;
  int get epoch => _epoch;
  void _notify() {
    if (_disposed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_queued) return;
      _queued = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _queued = false;
        if (!_disposed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  void sync() {
    if (captured == identity() || _disposed || _retired) return;
    _retired = true;
    _epoch++;
    _serial++;
    _timer?.cancel();
    view = null;
    options = null;
    selectedID = null;
    busy = false;
    error = '工作身份已变化，请重新打开线上发现。';
    _notify();
  }

  bool _isCurrent(int serial, int epoch) =>
      current && serial == _serial && epoch == _epoch;
  void expire() {
    if (_disposed) return;
    if (view?.live(DateTime.now()) == false ||
        options?.live(DateTime.now()) == false) {
      view = null;
      options = null;
      busy = false;
      _serial++;
      error = '来源已到期，请重新读取当前线上意图。';
      _timer?.cancel();
      _notify();
    }
  }

  void _schedule() {
    _timer?.cancel();
    final ends = [
      if (options != null) options!.validUntil,
      if (view != null) view!.validUntil,
    ];
    if (ends.isEmpty) return;
    ends.sort();
    final delay = ends.first.difference(DateTime.now());
    _timer = Timer(delay.isNegative ? Duration.zero : delay, expire);
  }

  Future<void> load({String? initialIntentID}) async {
    sync();
    if (!current) return;
    final s = ++_serial, e = _epoch;
    busy = true;
    error = null;
    _notify();
    try {
      final result = await api.read(captured.$1!, captured.$2!);
      if (!_isCurrent(s, e)) return;
      if (!result.live(DateTime.now())) throw const FormatException('来源已到期');
      options = result;
      view = null;
      selectedID = null;
      _schedule();
    } catch (_) {
      if (_isCurrent(s, e)) {
        options = null;
        view = null;
        error = '无法读取当前线上意图，请重试。';
      }
    } finally {
      if (_isCurrent(s, e)) {
        busy = false;
        _notify();
      }
    }
    if (current &&
        initialIntentID != null &&
        options?.intents.any((i) => i.id == initialIntentID) == true) {
      await select(initialIntentID);
    }
  }

  Future<void> select(String id) async {
    sync();
    expire();
    if (!current || options?.intents.any((i) => i.id == id) != true) return;
    final s = ++_serial, e = _epoch;
    selectedID = id;
    view = null;
    busy = true;
    error = null;
    _notify();
    try {
      final result = await api.read(captured.$1!, captured.$2!, intentID: id);
      if (!_isCurrent(s, e)) return;
      if (!result.live(DateTime.now())) throw const FormatException('来源已到期');
      view = result;
      _schedule();
    } catch (_) {
      if (_isCurrent(s, e)) {
        view = null;
        error = '意图或来源当前不可用，请重新读取。';
      }
    } finally {
      if (_isCurrent(s, e)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<OnlineSocialOpportunity?> resolve(OnlineSocialOpportunity old) async {
    sync();
    expire();
    if (!current ||
        view == null ||
        busy ||
        selectedID == null ||
        !view!.items.contains(old)) {
      return null;
    }
    final s = ++_serial, e = _epoch, id = selectedID!;
    busy = true;
    error = null;
    _notify();
    try {
      final fresh = await api.read(captured.$1!, captured.$2!, intentID: id);
      if (!_isCurrent(s, e)) return null;
      if (!fresh.live(DateTime.now())) throw const FormatException('来源已到期');
      view = fresh;
      _schedule();
      for (final item in fresh.items) {
        if (item.id == old.id &&
            item.sourceVersion == old.sourceVersion &&
            item.tieID == old.tieID &&
            item.communityID == old.communityID) {
          return item;
        }
      }
      error = '该来源已变化，请查看刷新后的结果。';
      return null;
    } catch (_) {
      if (_isCurrent(s, e)) {
        view = null;
        error = '暂时无法核实来源，请重新读取。';
      }
      return null;
    } finally {
      if (_isCurrent(s, e)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _epoch++;
    _serial++;
    _timer?.cancel();
    api.dispose();
    super.dispose();
  }
}
