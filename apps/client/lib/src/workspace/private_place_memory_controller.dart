import 'package:flutter/foundation.dart';
import 'private_place_memory_api.dart';

class PlaceMemoryApproval {
  PlaceMemoryApproval._(
    this.generation,
    this.view,
    this.controls,
    this.pending,
    this.draft,
  );
  final int generation;
  final PrivatePlaceMemory? view;
  final PrivatePlaceDeclarationControls controls;
  final PendingPlaceDeclaration pending;
  final PlaceDeclarationDraft? draft;
}

class PrivatePlaceMemoryController extends ChangeNotifier {
  PrivatePlaceMemoryController({
    required this.placeID,
    required this.authorizationHeader,
    required this.accountID,
    this.organizationWorkspaceID,
    required this.api,
    PlaceDeclarationPendingStore? pendingStore,
    DateTime Function()? now,
  }) : pendingStore =
           pendingStore ?? const SecurePlaceDeclarationPendingStore(),
       now = now ?? DateTime.now;
  final String placeID;
  final String? Function() authorizationHeader, accountID;
  final String? Function()? organizationWorkspaceID;
  final PrivatePlaceMemoryApi api;
  final PlaceDeclarationPendingStore pendingStore;
  final DateTime Function() now;
  (String?, String?, String?)? _identity;
  int generation = 0, _serial = 0;
  bool _closed = false, loading = false, saving = false;
  PrivatePlaceMemory? view;
  PrivatePlaceDeclarationControls? controls;
  PlaceDeclarationDraft? draft;
  PendingPlaceDeclaration? pending;
  String? error, message;
  bool get uncertain => pending != null;
  bool get personal =>
      !_closed &&
      authorizationHeader() != null &&
      validPlaceMemoryID(accountID()) &&
      organizationWorkspaceID?.call() == null;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void synchronizeIdentity() {
    if (_closed) return;
    final current = (
      authorizationHeader(),
      accountID(),
      organizationWorkspaceID?.call(),
    );
    if (_identity == current) return;
    _identity = current;
    generation++;
    _serial++;
    view = null;
    controls = null;
    draft = null;
    pending = null;
    loading = false;
    saving = false;
    error = null;
    message = null;
    _notify();
  }

  bool _current(int g, int serial) {
    if (_closed) return false;
    synchronizeIdentity();
    return !_closed && g == generation && serial == _serial && personal;
  }

  String _failure(Object e) => e is PlaceMemoryHTTP
      ? switch (e.status) {
          401 => '登录已失效，请重新登录。',
          403 => '当前无权管理，请切回本人账号并重新检查权限。',
          404 => '地点或记录当前不可用；不能据此判断到访或撤回结果。',
          409 => '记录或来源已变化，请重新读取后确认。',
          _ => '地点记录暂不可用，请重试。',
        }
      : '地点记录暂不可用，请重试。';
  bool _belongs(PrivatePlaceMemory v) =>
      v.ownerID == accountID() &&
      v.placeID == placeID &&
      v.expiresAt.isAfter(now().toUtc());
  bool _controlBelongs(PrivatePlaceDeclarationControls c) =>
      c.ownerID == accountID() &&
      c.placeID == placeID &&
      c.expiresAt.isAfter(now().toUtc());
  Future<void> load() async {
    if (_closed) return;
    synchronizeIdentity();
    if (!personal) {
      error = '请使用已登录的本人账号查看地点记录。';
      _notify();
      return;
    }
    if (saving) return;
    final g = generation,
        s = ++_serial,
        token = authorizationHeader()!,
        owner = accountID()!;
    loading = true;
    error = null;
    message = null;
    draft = null;
    view = null;
    controls = null;
    _notify();
    try {
      final saved = await pendingStore.read(api.base, owner, placeID);
      if (!_current(g, s)) return;
      pending = saved;
      final c = await api.readControls(placeID, token);
      if (!_current(g, s)) return;
      if (!_controlBelongs(c)) {
        throw const FormatException(
          'Current control owner/target/lease mismatch',
        );
      }
      controls = c;
      try {
        final v = await api.read(placeID, token);
        if (!_current(g, s)) return;
        if (!_belongs(v) || v.agentID != c.agentID) {
          throw const FormatException('Current owner/target/lease mismatch');
        }
        view = v;
      } on PlaceMemoryHTTP catch (e) {
        if (!_current(g, s)) return;
        if (e.status != 404) rethrow;
        error = '地点目前不公开或已过期，公开来源不可读。仍可检查和撤回本人已有声明；不会显示旧公开详情。';
      }
      if (saved != null) {
        // A current value is not proof that this exact operation caused it.
        // Only replaying the original native CAS may produce its own receipt.
        error = '有一项提交结果待核实。请核实原操作，不能仅凭记录消失判断成功。';
        if (saved.agentID != c.agentID ||
            saved.sessionFingerprint != placeSessionFingerprint(token)) {
          error = '登录或个人 Agent 已变化。旧确认不可复用；可查看当前记录并停止旧操作核实，但不会显示它已成功。';
        }
      }
    } catch (e) {
      if (_current(g, s)) {
        view = null;
        controls = null;
        error = _failure(e);
      }
    } finally {
      if (_current(g, s)) {
        loading = false;
        _notify();
      }
    }
  }

  void edit(PlaceDeclarationDraft d) {
    synchronizeIdentity();
    if (!personal ||
        view == null ||
        controls == null ||
        loading ||
        saving ||
        uncertain) {
      return;
    }
    if (!{'LIKED', 'VISITED'}.contains(d.kind) ||
        !placeMemoryVisibilityLabels.containsKey(d.visibility) ||
        !d.validUntil.isAfter(now().toUtc()) ||
        d.validUntil.difference(now().toUtc()) > const Duration(days: 365)) {
      error = '请选择声明、可见范围和有效期。';
      _notify();
      return;
    }
    draft = d;
    message = null;
    error = null;
    _notify();
  }

  void cancelDraft() {
    if (_closed) return;
    if (saving) return;
    draft = null;
    message = null;
    _notify();
  }

  PlaceMemoryApproval? preview({PrivatePlaceDeclaration? deleting}) {
    synchronizeIdentity();
    final v = view, c = controls, d = draft;
    if (!personal ||
        c == null ||
        loading ||
        saving ||
        uncertain ||
        !c.expiresAt.isAfter(now().toUtc())) {
      return null;
    }
    if (deleting != null) {
      if (!c.declarations.contains(deleting)) return null;
      return PlaceMemoryApproval._(
        generation,
        v,
        c,
        PendingPlaceDeclaration(
          memoryID: deleting.memoryID,
          agentID: c.agentID,
          expectedVersion: deleting.version,
          sessionFingerprint: placeSessionFingerprint(authorizationHeader()!),
          deleting: true,
        ),
        null,
      );
    }
    if (v == null ||
        !v.expiresAt.isAfter(now().toUtc()) ||
        d == null ||
        !d.validUntil.isAfter(now().toUtc())) {
      return null;
    }
    PrivatePlaceDeclaration? current;
    try {
      current = c.declaration(d.kind);
    } catch (_) {
      error = '有多条同类声明，请先检查并撤回不需保留的旧记录。';
      _notify();
      return null;
    }
    return PlaceMemoryApproval._(
      generation,
      v,
      c,
      PendingPlaceDeclaration(
        memoryID: current?.memoryID ?? newPlaceMemoryID(),
        agentID: c.agentID,
        expectedVersion: current?.version ?? 0,
        sessionFingerprint: placeSessionFingerprint(authorizationHeader()!),
        deleting: false,
        draft: d,
      ),
      d,
    );
  }

  Future<void> submit(PlaceMemoryApproval approved) async {
    synchronizeIdentity();
    if (!personal ||
        saving ||
        loading ||
        uncertain ||
        approved.generation != generation ||
        !identical(approved.controls, controls) ||
        (!approved.pending.deleting && !identical(approved.view, view)) ||
        (!approved.pending.deleting && !identical(approved.draft, draft)) ||
        !approved.controls.expiresAt.isAfter(now().toUtc()) ||
        (!approved.pending.deleting &&
            !(approved.view?.expiresAt.isAfter(now().toUtc()) ?? false))) {
      return;
    }
    await _execute(approved.pending, savePending: true);
  }

  bool get canRetryOriginal =>
      personal &&
      !loading &&
      !saving &&
      pending != null &&
      pending!.sessionFingerprint ==
          placeSessionFingerprint(authorizationHeader()!) &&
      controls != null &&
      controls!.expiresAt.isAfter(now().toUtc()) &&
      controls!.agentID == pending!.agentID;
  Future<void> retryOriginal() async {
    synchronizeIdentity();
    if (!canRetryOriginal) return;
    await _execute(pending!, savePending: false);
  }

  Future<void> _execute(
    PendingPlaceDeclaration operation, {
    required bool savePending,
  }) async {
    final g = generation,
        s = ++_serial,
        owner = accountID()!,
        token = authorizationHeader()!;
    saving = true;
    error = null;
    message = null;
    _notify();
    bool durable = false, preflightPassed = !savePending;
    try {
      // A new concrete confirmation uses one more current owner/Agent/CAS
      // read before dispatch. This is a human preview check, not a server
      // cognitive grant or proof that separate requests are one snapshot.
      if (savePending) {
        final current = await api.readControls(placeID, token);
        if (!_current(g, s)) return;
        final selected = current.declarations
            .where((d) => d.memoryID == operation.memoryID)
            .toList();
        final matches =
            _controlBelongs(current) &&
            current.agentID == operation.agentID &&
            (operation.expectedVersion == 0
                ? selected.isEmpty &&
                      current.declarations.every(
                        (d) => d.kind != operation.draft!.kind,
                      )
                : selected.length == 1 &&
                      selected.single.version == operation.expectedVersion);
        if (!matches) {
          view = null;
          controls = null;
          draft = null;
          error = '当前 Agent 或声明版本已变化，尚未发送。请重新读取并检查后确认。';
          return;
        }
        preflightPassed = true;
        final existing = await pendingStore.read(api.base, owner, placeID);
        if (!_current(g, s)) return;
        if (existing != null) {
          pending = existing;
          error = '已有原操作待核实，请先处理它。';
          return;
        }
        await pendingStore.write(api.base, owner, placeID, operation);
        durable = true;
        if (!_current(g, s)) return;
      } else {
        durable = true;
      }
      pending = operation;
      _notify();
      final receipt = operation.deleting
          ? await api.delete(
              operation.memoryID,
              operation.expectedVersion,
              token,
            )
          : await api.put(
              operation.memoryID,
              placeID,
              operation.agentID,
              operation.expectedVersion,
              operation.draft!,
              token,
            );
      if (!_current(g, s)) return;
      if (receipt.ownerID != owner ||
          receipt.agentID != operation.agentID ||
          receipt.memoryID != operation.memoryID ||
          (receipt.version != operation.expectedVersion + 1 &&
              (operation.deleting ||
                  receipt.version != operation.expectedVersion)) ||
          receipt.status != (operation.deleting ? 'DELETED' : 'ACTIVE') ||
          (!operation.deleting &&
              (receipt.placeID != placeID ||
                  receipt.kind != operation.draft!.kind ||
                  receipt.visibility != operation.draft!.visibility ||
                  receipt.validUntil != operation.draft!.validUntil.toUtc()))) {
        throw const FormatException('Receipt mismatches original operation');
      }
      await pendingStore.delete(api.base, owner, placeID);
      if (!_current(g, s)) return;
      pending = null;
      draft = null;
      message = operation.deleting ? '已收到权威撤回回执。' : '已收到本人声明保存回执（不是到访核验）。';
      try {
        final control = await api.readControls(placeID, token);
        if (!_current(g, s)) return;
        if (!_controlBelongs(control) || control.agentID != operation.agentID) {
          throw const FormatException('Current control mismatch');
        }
        controls = control;
        final v = await api.read(placeID, token);
        if (!_current(g, s)) return;
        if (!_belongs(v) || v.agentID != control.agentID) {
          throw const FormatException('Owner mismatch');
        }
        view = v;
      } catch (_) {
        if (_current(g, s)) {
          view = null;
          controls = null;
          error = '操作已收到回执，但地点当前记录暂不可读。请重新读取。';
        }
      }
    } catch (e) {
      if (!_current(g, s)) return;
      if (!durable) {
        error = preflightPassed
            ? '本地恢复记录未保存，尚未发送。请检查设备存储后重试。'
            : '当前声明复核失败，尚未发送。${_failure(e)}';
        return;
      }
      // A mutation may commit before a later source/session response gate
      // rejects it. No error status alone proves that nothing happened.
      pending = operation;
      draft = null;
      view = null;
      controls = null;
      error = '提交结果未知，可能已经保存或撤回。请核实原操作；不要重新创建记录。';
      if (e is PlaceMemoryHTTP) error = '$error ${_failure(e)}';
    } finally {
      if (_current(g, s)) {
        saving = false;
        _notify();
      }
    }
  }

  /// Stop local recovery explicitly, never call it a rollback or success. A
  /// new operation still needs a fresh native read and a new concrete preview.
  Future<void> stopRecovery() async {
    synchronizeIdentity();
    if (!personal || saving || loading || pending == null) return;
    final g = generation, s = ++_serial, owner = accountID()!;
    saving = true;
    _notify();
    try {
      await pendingStore.delete(api.base, owner, placeID);
      if (!_current(g, s)) return;
      pending = null;
      view = null;
      controls = null;
      draft = null;
      message = '已停止本机核实；原操作结果仍未知，不代表撤销或成功。请重新读取。';
      error = null;
    } catch (_) {
      if (_current(g, s)) error = '无法移除本机恢复记录，请稍后重试。';
    } finally {
      if (_current(g, s)) {
        saving = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    generation++;
    _serial++;
    _identity = null;
    view = null;
    controls = null;
    draft = null;
    pending = null;
    error = null;
    message = null;
    loading = false;
    saving = false;
    api.dispose();
    super.dispose();
  }
}
