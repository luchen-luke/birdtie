import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'public_moment_api.dart';
import 'public_moment_history_api.dart';

class PublicMomentHistoryController extends ChangeNotifier {
  PublicMomentHistoryController({
    required this.api,
    required PublicMomentHistoryQuery query,
    this.ownerID,
    this.workspaceID,
    this.identityChanges,
    this.sourceCurrent,
  }) : _query = query {
    _identity = _currentIdentity;
    identityChanges?.addListener(_identityChanged);
  }
  final PublicMomentHistoryApi api;
  final String? Function()? ownerID, workspaceID;
  final Listenable? identityChanges;
  final bool Function()? sourceCurrent;
  PublicMomentHistoryQuery _query;
  PublicMomentHistoryQuery get query => _query;
  List<PublicMoment> _items = const [];
  List<PublicMoment> get items {
    _sync();
    if (_pages.any((p) => !p.page.validUntil.isAfter(DateTime.now().toUtc()))) {
      return const [];
    }
    return _items;
  }

  String? nextCursor, error;
  bool loading = false;
  int _epoch = 0, _request = 0;
  bool _disposed = false, _retired = false;
  Object? _identity;
  DateTime? _snapshot;
  final List<({String? cursor, PublicMomentHistoryPageData page})> _pages = [];
  Timer? _leaseTimer;
  Object get _currentIdentity =>
      (api.authorizationHeader(), ownerID?.call(), workspaceID?.call());
  bool get current =>
      !_disposed &&
      !_retired &&
      workspaceID?.call() == null &&
      sourceCurrent?.call() != false;
  void _notify() {
    if (_disposed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      SchedulerBinding.instance.addPostFrameCallback((_) {
        if (!_disposed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  void _clear() {
    _epoch++;
    _request++;
    _items = const [];
    _pages.clear();
    _snapshot = null;
    nextCursor = null;
    _leaseTimer?.cancel();
    loading = false;
  }

  void _sync() {
    if (_disposed) return;
    if (_identity != _currentIdentity) {
      _identity = _currentIdentity;
      _clear();
      error = null;
    }
    if (workspaceID?.call() != null || sourceCurrent?.call() == false) {
      if (!_retired) _clear();
      _retired = true;
      error = '当前身份或浏览来源已变化，请返回重新打开。';
    }
  }

  void _identityChanged() {
    final epoch = _epoch;
    _sync();
    // Any actual source notification re-reads visible rows. A same-token
    // notification cannot preserve a removed relationship's old cached rows.
    if (current) {
      if (epoch == _epoch) _clear();
      unawaited(refresh());
    }
    _notify();
  }

  Future<void> select(PublicMomentHistoryQuery query) async {
    _sync();
    if (!current || query.binding == _query.binding) return;
    _query = query;
    _clear();
    error = null;
    _notify();
    await refresh();
  }

  Future<void> refresh() => _load(append: false);
  Future<void> loadMore() => _load(append: true);
  Future<void> _load({required bool append}) async {
    _sync();
    if (!current || loading || append && nextCursor == null) return;
    final epoch = _epoch,
        identity = _identity,
        query = _query,
        cursor = append ? nextCursor : null,
        request = ++_request;
    bool valid() {
      _sync();
      return current &&
          epoch == _epoch &&
          request == _request &&
          identity == _identity &&
          query.binding == _query.binding;
    }

    loading = true;
    error = null;
    if (!append &&
        _pages.any((p) => !p.page.validUntil.isAfter(DateTime.now().toUtc()))) {
      _items = const [];
      _pages.clear();
      _snapshot = null;
      nextCursor = null;
    }
    _notify();
    try {
      final page = await api.read(query, cursor: cursor);
      if (!valid()) return;
      if (!append) {
        _items = const [];
        _pages.clear();
      }
      if (append &&
          (page.snapshotAt != _snapshot ||
              page.items.any((m) => _items.any((old) => old.id == m.id))))
        throw const FormatException('动态分页来源已变化');
      if (append &&
          _items.isNotEmpty &&
          page.items.isNotEmpty &&
          (page.items.first.publishedAt.isAfter(_items.last.publishedAt) ||
              page.items.first.publishedAt == _items.last.publishedAt &&
                  page.items.first.id.compareTo(_items.last.id) >= 0))
        throw const FormatException('动态分页顺序已变化');
      _snapshot = page.snapshotAt;
      nextCursor = page.nextCursor;
      _pages.add((cursor: page.refreshCursor, page: page));
      _items = List.unmodifiable([..._items, ...page.items]);
      _scheduleLease();
    } catch (e) {
      if (!valid()) return;
      if (e is MomentHistoryFailure && [401, 404, 409].contains(e.status) ||
          e is FormatException) {
        _items = const [];
        _pages.clear();
        nextCursor = null;
        _leaseTimer?.cancel();
      }
      error = e is MomentHistoryFailure
          ? e.message
          : e is TimeoutException
          ? '读取动态超时，请重试。'
          : '动态暂不可读取，请重试。';
    } finally {
      if (valid()) {
        loading = false;
        _notify();
      }
    }
  }

  void _scheduleLease() {
    _leaseTimer?.cancel();
    if (_pages.isEmpty) return;
    final until = _pages
        .map((p) => p.page.validUntil)
        .reduce((a, b) => a.isBefore(b) ? a : b);
    final wait = until.difference(DateTime.now().toUtc());
    _leaseTimer = Timer(wait.isNegative ? Duration.zero : wait, () {
      // Retire text before reading fresh page leases. IDs/scroll alone survive.
      _items = const [];
      _notify();
      unawaited(_revalidateLoadedPages());
    });
  }

  Future<void> _revalidateLoadedPages() async {
    _sync();
    if (!current || loading) return;
    final starts = _pages.map((p) => p.cursor).toList();
    final epoch = _epoch,
        identity = _identity,
        query = _query,
        request = ++_request;
    bool valid() {
      _sync();
      return current &&
          epoch == _epoch &&
          identity == _identity &&
          request == _request &&
          query.binding == _query.binding;
    }

    loading = true;
    _notify();
    try {
      final fresh = <({String? cursor, PublicMomentHistoryPageData page})>[];
      for (final cursor in starts) {
        final page = await api.read(query, cursor: cursor);
        if (!valid()) return;
        if (_snapshot != null && page.snapshotAt != _snapshot)
          throw const FormatException('动态快照已变化');
        fresh.add((cursor: cursor, page: page));
      }
      if (!valid()) return;
      _pages
        ..clear()
        ..addAll(fresh);
      _items = List.unmodifiable(fresh.expand((p) => p.page.items));
      error = null;
      _scheduleLease();
    } catch (_) {
      if (valid()) {
        _pages.clear();
        nextCursor = null;
        error = '动态或关系已更新，请刷新列表。';
      }
    } finally {
      if (valid()) {
        loading = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _epoch++;
    _request++;
    _leaseTimer?.cancel();
    identityChanges?.removeListener(_identityChanged);
    api.dispose();
    super.dispose();
  }
}
