import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'human_causal_control.dart';
import 'human_causal_pending_store.dart';

/// Caller-local logical namespace generation, excluding credential renewal.
class HumanCausalSourceSerial extends ChangeNotifier {
  Object? _seen;
  int generation = 0;
  bool _closed = false;
  void observe(Object? key) {
    if (_closed || key == _seen) return;
    _seen = key;
    generation++;
    notifyListeners();
  }

  /// Forward the current reader binding without changing logical authority.
  /// Header renewal can retire approval while keeping the same owner's journal.
  void notifyReadBindingChanged() {
    if (!_closed) notifyListeners();
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    generation++;
    notifyListeners();
    super.dispose();
  }
}

/// Keeps receiver lifetime, current source and the original write distinct.
/// A historical receipt never manufactures a current business effect.
class HumanCausalOperationController extends ChangeNotifier {
  HumanCausalOperationController({
    required this.api,
    required this.family,
    this.organizationID = '',
    this.targetID = '',
    this.reportTargetType = 'general',
    this.ownerID,
    this.identityChanges,
    this.sourceCurrent,
    this.targetKey,
    HumanCausalPendingStore? pendingStore,
  }) : store = pendingStore ?? SecureHumanCausalPendingStore() {
    _owner = ownerID?.call();
    _header = api.authorizationHeader();
    _target = targetKey?.call();
    identityChanges?.addListener(_observe);
  }
  final HumanCausalApi api;
  final HumanCausalFamily family;
  final String organizationID, targetID, reportTargetType;
  final String? Function()? ownerID;
  final Listenable? identityChanges;
  final bool Function()? sourceCurrent;
  final Object? Function()? targetKey;
  final HumanCausalPendingStore store;
  String? _owner, _header;
  Object? _target;
  int _epoch = 0, _draft = 0;
  bool _retired = false,
      _disposed = false,
      _notifierDisposed = false,
      _storageReady = false;
  int _dialogs = 0;
  final _dialogTimers = <Timer>{};
  Timer? _stateTimer, _previewTimer;
  bool busy = false;
  String? error, notice;
  HumanCausalSnapshot? snapshot;
  HumanCausalPreview? preview;
  HumanCausalSnapshot? _previewSource;
  HumanCausalPending? pending;
  HumanCausalReceipt? receipt;
  HumanCausalSelector? _notSubmitted;
  String? get owner => _owner;
  int get epoch => _epoch;
  String get key =>
      '${family.wire}:${family == HumanCausalFamily.faq ? organizationID : ''}';
  bool get current {
    if (!_disposed &&
        !_retired &&
        (sourceCurrent?.call() == false ||
            targetKey?.call() != _target ||
            ownerID != null && (_owner == null || ownerID!() != _owner) ||
            api.authorizationHeader() == null)) {
      _retired = true;
      ++_epoch;
      ++_draft;
      _clearSource();
      pending = null;
      receipt = null;
      busy = false;
      _storageReady = false;
      error = '个人身份或来源已变化。旧资料和批准已移除，请返回当前入口重新打开。';
    }
    return !_disposed && !_retired;
  }

  bool get storageReady => _storageReady;
  bool get locallyPrepared =>
      pending?.stage == 'PREPARED' ||
      pending != null && _notSubmitted?.same(pending!.selector) == true;
  bool get canStart =>
      current &&
      humanControlID(_owner) &&
      _storageReady &&
      !busy &&
      pending == null &&
      snapshot?.current == true;
  void _clearSource() {
    _stateTimer?.cancel();
    _previewTimer?.cancel();
    snapshot = null;
    preview = null;
    _previewSource = null;
  }

  void _notify() {
    if (_notifierDisposed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!_notifierDisposed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  void _observe() {
    final before = _epoch;
    if (!current) {
      _notify();
      return;
    }
    final next = api.authorizationHeader();
    if (_header != next) {
      _header = next;
      ++_epoch;
      ++_draft;
      _clearSource();
      busy = false;
      _storageReady = false;
      // With an explicit owner getter, ordinary renewal keeps owned drafts.
      // Legacy callers re-read /me before any private source is shown again.
      if (ownerID == null) _owner = null;
      error = '登录已更新。本人尚未提交的文字保留，旧批准已失效；请重读本机记录和当前资料。';
    }
    if (before != _epoch) _notify();
  }

  bool _valid(int epoch, String? header) =>
      current &&
      _epoch == epoch &&
      api.authorizationHeader() == header &&
      (_owner == null || ownerID == null || ownerID!() == _owner);
  bool Function() capture() {
    _observe();
    final e = _epoch, h = _header;
    return () => _valid(e, h);
  }

  Future<bool> _resolveOwner(int e, String? h) async {
    if (ownerID != null) return humanControlID(_owner);
    final found = await api.owner(() => _valid(e, h));
    if (!_valid(e, h)) return false;
    if (_lastResolvedOwner != null && _lastResolvedOwner != found) {
      _retired = true;
      ++_epoch;
      _clearSource();
      pending = null;
      receipt = null;
      busy = false;
      error = '账号已变化，旧私人资料已移除。';
      _notify();
      return false;
    }
    _lastResolvedOwner = found;
    _owner = found;
    return true;
  }

  String? _lastResolvedOwner;
  Future<void> initialize() async {
    _observe();
    if (!current || busy) return;
    final e = _epoch, h = _header;
    busy = true;
    error = null;
    _storageReady = false;
    _clearSource();
    _notify();
    try {
      if (!await _resolveOwner(e, h) || !_valid(e, h)) return;
      final rows = await store.read(api.origin, _owner!);
      if (!_valid(e, h)) return;
      final matches = rows.where((p) => p.key == key);
      pending = matches.isEmpty ? null : matches.single;
      _storageReady = true;
      if (pending != null)
        notice = locallyPrepared
            ? '原操作只准备了本机记录。请检查后重新预览；当前不提交。'
            : '本人原操作结果尚未核实，只能核实原回执或明确停止。';
      await _readState(e, h);
    } catch (e2) {
      if (_valid(e, h)) {
        error = _message(e2);
        if (!humanControlID(_owner)) _storageReady = false;
      }
    } finally {
      if (_valid(e, h)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> loadState() async {
    _observe();
    if (!current || busy) return;
    if (!_storageReady || !humanControlID(_owner)) {
      await initialize();
      return;
    }
    final e = _epoch, h = _header;
    busy = true;
    error = null;
    _clearSource();
    _notify();
    try {
      await _readState(e, h);
    } catch (e2) {
      if (_valid(e, h)) {
        snapshot = null;
        error = _message(e2);
      }
    } finally {
      if (_valid(e, h)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _readState(int e, String? h) async {
    final s = await api.state(
      family,
      organizationID: organizationID,
      targetID: targetID,
      current: () => _valid(e, h),
    );
    if (!_valid(e, h)) return;
    snapshot = s;
    _stateTimer?.cancel();
    _stateTimer = Timer(s.expiresAt.difference(DateTime.now().toUtc()), () {
      if (!_disposed && identical(snapshot, s)) {
        _clearSource();
        ++_draft;
        _notify();
      }
    });
  }

  void invalidatePreview() {
    if (!current) return;
    ++_draft;
    preview = null;
    _previewSource = null;
    _previewTimer?.cancel();
  }

  bool _scope(HumanCausalInput input) =>
      input.family == family &&
      input.selector.organizationID == organizationID &&
      switch (family) {
        HumanCausalFamily.grantRevoke ||
        HumanCausalFamily.block => input.selector.targetID == targetID,
        HumanCausalFamily.report =>
          input.selector.targetID == targetID &&
              input.selector.targetType == reportTargetType,
        _ => true,
      };
  Future<HumanCausalPreview?> prepare(HumanCausalInput input) async {
    _observe();
    final s = snapshot;
    if (!canStart ||
        s == null ||
        !input.valid ||
        !_scope(input) ||
        !s.permits(input)) {
      if (current) {
        error = '请检查内容并重新读取当前目标、权限和具体版本。';
        _notify();
      }
      return null;
    }
    final e = _epoch, h = _header, d = _draft;
    busy = true;
    error = null;
    preview = null;
    _notify();
    bool frame() =>
        _valid(e, h) && d == _draft && identical(snapshot, s) && s.current;
    try {
      final p = await api.preview(input, s.expiresAt, frame);
      if (!frame()) return null;
      preview = p;
      _previewSource = s;
      _previewTimer?.cancel();
      _previewTimer = Timer(p.expiresAt.difference(DateTime.now().toUtc()), () {
        if (!_disposed && identical(preview, p)) {
          preview = null;
          _previewSource = null;
          _notify();
        }
      });
      return p;
    } catch (e2) {
      if (_valid(e, h)) error = _message(e2);
      return null;
    } finally {
      if (_valid(e, h)) {
        busy = false;
        _notify();
      }
    }
  }

  bool previewCurrent(HumanCausalPreview p) =>
      current &&
      !busy &&
      _storageReady &&
      pending == null &&
      api.authorizationHeader() == _header &&
      identical(preview, p) &&
      identical(snapshot, _previewSource) &&
      snapshot?.current == true &&
      p.expiresAt.isAfter(DateTime.now().toUtc()) &&
      _scope(p.input);
  Future<HumanCausalReceipt?> submit(HumanCausalPreview p) async {
    _observe();
    if (!previewCurrent(p)) return null;
    final e = _epoch,
        h = _header,
        owner = _owner!,
        s = snapshot,
        d = _draft,
        selector = p.input.selector;
    bool frame() => _valid(e, h);
    bool source() =>
        frame() && d == _draft && identical(snapshot, s) && s?.current == true;
    var sent = false;
    busy = true;
    error = null;
    _notify();
    try {
      final prepared = HumanCausalPending(selector, stage: 'PREPARED');
      await store.write(api.origin, owner, prepared);
      if (!source()) throw const FormatException('来源已变化，操作尚未发送');
      final read = (await store.read(
        api.origin,
        owner,
      )).where((v) => v.key == key);
      if (!source() || read.isEmpty || !read.single.same(prepared))
        throw const FormatException('本机准备记录未核对完成');
      pending = prepared;
      final unknown = HumanCausalPending(selector);
      await store.write(api.origin, owner, unknown);
      if (!source()) throw const FormatException('来源已变化，操作尚未发送');
      final back = (await store.read(
        api.origin,
        owner,
      )).where((v) => v.key == key);
      if (!source() ||
          back.isEmpty ||
          !back.single.same(unknown) ||
          !p.expiresAt.isAfter(DateTime.now().toUtc()))
        throw const FormatException('原批准或本机提交记录已失效，尚未发送');
      pending = unknown;
      sent = true;
      final result = await api.apply(p, frame);
      if (!frame()) return null;
      await _accept(result, unknown, e, h, owner);
      return frame() ? result : null;
    } catch (e2) {
      if (frame()) {
        preview = null;
        _previewSource = null;
        if (!sent) {
          _notSubmitted = selector;
          try {
            final rows = await store.read(api.origin, owner);
            if (frame()) {
              final matches = rows.where((v) => v.key == key);
              pending = matches.isEmpty ? null : matches.single;
            }
          } catch (_) {
            if (frame()) _storageReady = false;
          }
          if (frame()) error = '本次尚未发送，草稿保留。请检查本机准备记录后重新预览。';
        } else {
          pending = HumanCausalPending(selector);
          _notSubmitted = null;
          error = '结果尚未核实，只读取原操作或明确停止；不会重发。';
        }
      }
      return null;
    } finally {
      if (frame()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> repairPrepared() async {
    _observe();
    if (!current || busy || !humanControlID(_owner)) return;
    final e = _epoch, h = _header, owner = _owner!;
    busy = true;
    _notify();
    try {
      final rows = await store.read(api.origin, owner);
      if (!_valid(e, h)) return;
      final matches = rows.where((v) => v.key == key),
          p = matches.isEmpty ? null : matches.single;
      if (p != null &&
          (p.stage == 'PREPARED' || _notSubmitted?.same(p.selector) == true)) {
        await store.remove(api.origin, owner, p);
        if (!_valid(e, h)) return;
        pending = null;
        _notSubmitted = null;
        _storageReady = true;
        error = null;
        notice = '本机准备记录已核对；尚未发送，请重新读取并预览。';
      } else {
        pending = p;
        _notSubmitted = null;
        _storageReady = true;
        error = null;
      }
    } catch (_) {
      if (_valid(e, h)) error = '本机记录仍不可读，当前不会提交。';
    } finally {
      if (_valid(e, h)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<HumanCausalReceipt?> recover({bool stop = false}) async {
    _observe();
    if (!current ||
        busy ||
        pending == null ||
        locallyPrepared ||
        !humanControlID(_owner))
      return null;
    final p = pending!, e = _epoch, h = _header, owner = _owner!;
    busy = true;
    error = null;
    _notify();
    try {
      final r = await api.operation(p.selector, () => _valid(e, h), stop: stop);
      if (!_valid(e, h)) return null;
      await _accept(r, p, e, h, owner);
      return _valid(e, h) ? r : null;
    } catch (_) {
      if (_valid(e, h)) error = '原操作仍未核实。当前不可读不代表未执行，不会重发。';
      return null;
    } finally {
      if (_valid(e, h)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _accept(
    HumanCausalReceipt r,
    HumanCausalPending original,
    int e,
    String? h,
    String owner,
  ) async {
    receipt = r;
    preview = null;
    _previewSource = null;
    _notSubmitted = null;
    notice = r.status == 'STOPPED'
        ? '原操作已停止，迟到请求不会产生效果。'
        : '原操作已确证完成，正在读取当前资料。';
    _clearSource();
    try {
      await store.remove(api.origin, owner, original);
      if (!_valid(e, h)) return;
      pending = null;
      _storageReady = true;
    } catch (_) {
      if (_valid(e, h)) error = '原结果已确证，本机记录清理尚未完成；可再次核实同一原操作。';
    }
    if (!_valid(e, h)) return;
    try {
      await _readState(e, h);
      if (_valid(e, h))
        notice = r.status == 'STOPPED' ? '原操作已停止，当前资料已读取。' : '原操作已完成，当前资料已读取。';
    } catch (_) {
      if (_valid(e, h)) {
        _clearSource();
        notice = r.status == 'STOPPED'
            ? '原操作已停止，当前资料暂不可读；不会再次提交。'
            : '原操作已完成，当前资料暂不可读；不会再次提交原操作。';
      }
    }
  }

  String _message(Object e) => e is HumanCausalFailure
      ? e.message
      : e is FormatException
      ? e.message
      : '读取失败，请稍后核实。';

  Future<HumanCausalReceipt?> checkAndConfirm(
    BuildContext context,
    HumanCausalInput input,
  ) async {
    final e = _epoch;
    final p = await prepare(input);
    if (p == null || !context.mounted || e != _epoch || !previewCurrent(p))
      return null;
    final yes = await _confirm(context, p);
    return yes && context.mounted && e == _epoch && previewCurrent(p)
        ? submit(p)
        : null;
  }

  Future<bool> _confirm(BuildContext context, HumanCausalPreview p) async {
    final e = _epoch, live = ValueNotifier(true);
    bool valid() => live.value && _epoch == e && previewCurrent(p);
    late final DialogRoute<bool> route;
    route = DialogRoute<bool>(
      context: context,
      builder: (dialogContext) => ListenableBuilder(
        listenable: Listenable.merge([this, live]),
        builder: (context, _) {
          final enabled = valid();
          return AlertDialog(
            title: Text(enabled ? '确认${family.label}操作' : '这次确认已失效'),
            content: SingleChildScrollView(
              child: enabled
                  ? Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        ...humanCausalConfirmation(p.input),
                        const SizedBox(height: 12),
                        const Text('只执行本次已检查的内容和具体版本。取消不会提交；确认到期后需重新读取。'),
                      ],
                    )
                  : const Text('旧确认中的资料已隐藏。请关闭后重新读取，不会执行旧批准。'),
            ),
            actions: [
              TextButton(
                onPressed: () {
                  if (live.value) Navigator.of(dialogContext).pop(false);
                },
                child: const Text('取消'),
              ),
              FilledButton(
                key: const Key('human-control-confirm'),
                onPressed: enabled
                    ? () {
                        if (valid()) Navigator.of(dialogContext).pop(true);
                      }
                    : null,
                child: const Text('确认提交'),
              ),
            ],
          );
        },
      ),
    );
    ++_dialogs;
    final timer = Timer(p.expiresAt.difference(DateTime.now().toUtc()), () {
      if (live.value) live.value = false;
    });
    _dialogTimers.add(timer);
    unawaited(
      route.completed.then((_) {
        timer.cancel();
        _dialogTimers.remove(timer);
        live.dispose();
        --_dialogs;
        _finishDispose();
      }),
    );
    try {
      final yes = await Navigator.of(context).push(route);
      final accepted = yes == true && valid();
      live.value = false;
      return accepted;
    } finally {
      timer.cancel();
      _dialogTimers.remove(timer);
    }
  }

  Future<void> confirmStop(BuildContext context) async {
    final p = pending, e = _epoch, live = ValueNotifier(true);
    if (!current || busy || p == null || locallyPrepared) {
      live.dispose();
      return;
    }
    bool valid() =>
        live.value && current && e == _epoch && identical(pending, p) && !busy;
    final route = DialogRoute<bool>(
      context: context,
      builder: (dialogContext) => ListenableBuilder(
        listenable: Listenable.merge([this, live]),
        builder: (context, _) => AlertDialog(
          title: const Text('停止并核实原操作'),
          content: Text(
            valid()
                ? '若原操作已成功，读取其实际回执；否则永久停止此操作。不会撤销已发生的效果，也不会重新提交。'
                : '原身份或操作已变化，请关闭后重新核实。',
          ),
          actions: [
            TextButton(
              onPressed: () {
                if (live.value) Navigator.of(dialogContext).pop(false);
              },
              child: const Text('保留待核实状态'),
            ),
            FilledButton(
              key: const Key('human-control-confirm-stop'),
              onPressed: valid()
                  ? () {
                      if (valid()) Navigator.of(dialogContext).pop(true);
                    }
                  : null,
              child: const Text('停止并核实'),
            ),
          ],
        ),
      ),
    );
    ++_dialogs;
    unawaited(
      route.completed.then((_) {
        live.dispose();
        --_dialogs;
        _finishDispose();
      }),
    );
    final yes = await Navigator.of(context).push(route);
    final confirmed = yes == true && valid();
    live.value = false;
    if (confirmed) await recover(stop: true);
  }

  void _finishDispose() {
    if (_disposed && _dialogs == 0 && !_notifierDisposed) {
      _notifierDisposed = true;
      super.dispose();
    }
  }

  // _finishDispose invokes super.dispose exactly once after the last retained
  // dialog unmounts; its listener must remain usable until route.completed.
  @override
  // ignore: must_call_super
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    ++_epoch;
    _clearSource();
    for (final t in _dialogTimers) {
      t.cancel();
    }
    _dialogTimers.clear();
    identityChanges?.removeListener(_observe);
    _notify();
    _finishDispose();
  }
}

List<Widget> humanCausalConfirmation(HumanCausalInput input) => switch (input) {
  HumanFAQInput i => [
    Text(
      i.action == 'DELETE'
          ? '删除原问答'
          : '${i.published == true ? '发布' : '保存草稿'}问答',
    ),
    if (i.action != 'DELETE') ...[
      Text(i.question),
      const SizedBox(height: 8),
      Text(i.answer),
    ],
    Text(
      i.action == 'DELETE'
          ? '删除后该问答不再用于组织回答，原组织保留。'
          : i.published == true
          ? '按原组织核验与公开规则用于组织问答；不会替你核验回答事实。'
          : '保存组织管理员可见草稿，不用于公开回答。',
    ),
    Text('原问答版本 ${i.expectedVersion}'),
  ],
  HumanContextInput i => [
    Text(
      i.action == 'REMOVE'
          ? '移除本人这条生活资料声明'
          : '${i.contextType == 'CITY'
                ? '城市'
                : i.contextType == 'INSTITUTION'
                ? '学校'
                : '线上兴趣'}：${i.localLabel ?? (i.contextType == 'CITY' ? '所选城市' : i.sourceKey)}',
    ),
    Text(
      i.relation == 'current' && i.action == 'DECLARE'
          ? '将替换本人原所在城市声明；不会证明居住、加入社群或授予访问权限。'
          : '只修改本人声明，不是身份、就读或到访证明，也不授予社群访问权限。',
    ),
  ],
  HumanReportInput i => [
    Text(i.details),
    const SizedBox(height: 8),
    const Text('提交问题供处理，并消耗一次当前举报额度。不会自动屏蔽、撤回内容或向对方发消息。'),
  ],
  HumanGrantInput _ => [
    const Text('撤销这项本人资料访问授权。撤销后不能继续按此授权读取；已发生的读取不会被删除，取消屏蔽也不会恢复授权。'),
  ],
  HumanBlockInput i => [
    Text(
      i.action == 'BLOCK'
          ? '屏蔽此账号，并按原规则终止双方联系和资料访问；取消屏蔽不会自动恢复好友或授权。'
          : '取消此账号的屏蔽。不会自动恢复好友、之前撤销的授权或旧消息。',
    ),
    Text('原账号标识：${i.accountID.substring(i.accountID.length - 6)}'),
  ],
};

class HumanCausalOperationPanel extends StatelessWidget {
  const HumanCausalOperationPanel({super.key, required this.controller});
  final HumanCausalOperationController controller;
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) {
      final c = controller, epoch = c.epoch, p = c.pending;
      bool frame() => c.current && c.epoch == epoch;
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (c.busy) const LinearProgressIndicator(),
          if (c.notice != null)
            Text(c.notice!, key: const Key('human-control-notice')),
          if (c.error != null)
            Text(c.error!, key: const Key('human-control-error')),
          if (p != null) ...[
            Text('本人原${p.selector.family.label}操作'),
            if (p.selector.targetID.isNotEmpty)
              Text(
                '原目标标识末六位：${p.selector.targetID.substring(p.selector.targetID.length - 6)}',
              ),
            if (c.locallyPrepared)
              TextButton(
                key: const Key('human-control-repair'),
                onPressed: c.busy
                    ? null
                    : () {
                        if (frame() && identical(c.pending, p))
                          unawaited(c.repairPrepared());
                      },
                child: const Text('检查本机准备记录'),
              )
            else ...[
              FilledButton(
                key: const Key('human-control-recover'),
                onPressed: c.busy
                    ? null
                    : () {
                        if (frame() && identical(c.pending, p))
                          unawaited(c.recover());
                      },
                child: const Text('核实原操作结果'),
              ),
              OutlinedButton(
                key: const Key('human-control-stop'),
                onPressed: c.busy
                    ? null
                    : () {
                        if (frame() && identical(c.pending, p))
                          unawaited(c.confirmStop(context));
                      },
                child: const Text('停止并核实原操作'),
              ),
            ],
          ],
          if (c.current && !c.busy)
            TextButton(
              key: const Key('human-control-read'),
              onPressed: () {
                if (frame()) unawaited(c.loadState());
              },
              child: const Text('重新读取当前资料'),
            ),
        ],
      );
    },
  );
}

/// A caller's logical personal source; credential renewal is observed by the
/// operation controller separately and cannot revive an old concrete approval.
class HumanCausalEntryBoundary extends StatefulWidget {
  const HumanCausalEntryBoundary({
    super.key,
    required this.ownerID,
    required this.authorizationHeader,
    required this.sourceKey,
    required this.builder,
    this.identityChanges,
    this.sourceCurrent,
  });
  final String? Function() ownerID, authorizationHeader;
  final Object? Function() sourceKey;
  final Listenable? identityChanges;
  final bool Function()? sourceCurrent;
  final Widget Function(BuildContext, bool Function()) builder;
  @override
  State<HumanCausalEntryBoundary> createState() =>
      _HumanCausalEntryBoundaryState();
}

class _HumanCausalEntryBoundaryState extends State<HumanCausalEntryBoundary> {
  late final String? _owner = widget.ownerID(),
      _initialHeader = widget.authorizationHeader();
  late final Object? _source = widget.sourceKey();
  bool _retired = false;
  bool get _current {
    if (!mounted || _retired) return false;
    if (widget.sourceCurrent?.call() == false ||
        (_owner != null && !humanControlID(_owner)) ||
        widget.ownerID() != _owner ||
        widget.authorizationHeader() == null ||
        (_owner == null && widget.authorizationHeader() != _initialHeader) ||
        widget.sourceKey() != _source)
      _retired = true;
    return !_retired;
  }

  void _changed() {
    if (!_current) {
      if (SchedulerBinding.instance.schedulerPhase ==
          SchedulerPhase.persistentCallbacks) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) setState(() {});
        });
      } else if (mounted)
        setState(() {});
    }
  }

  @override
  void initState() {
    super.initState();
    widget.identityChanges?.addListener(_changed);
  }

  @override
  void didUpdateWidget(HumanCausalEntryBoundary old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_changed);
      widget.identityChanges?.addListener(_changed);
    }
    _current;
  }

  @override
  void dispose() {
    widget.identityChanges?.removeListener(_changed);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => _current
      ? widget.builder(context, () => _current)
      : const Scaffold(body: Center(child: Text('工作身份或来源已变化，请返回当前入口重新核实。')));
}
