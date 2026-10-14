import 'dart:async';
import 'package:flutter/widgets.dart';
import 'package:http/http.dart' as http;
import 'system_push_api.dart';
import 'system_push_gateway.dart';
import 'system_push_pending_store.dart';
import 'remote_inbox_source.dart';

class SystemPushController extends ChangeNotifier with WidgetsBindingObserver {
  SystemPushController({
    required this.authorizationHeader,
    required this.ownerID,
    required this.sourceCurrent,
    required this.identityChanges,
    required String apiBaseUrl,
    http.Client? client,
    SystemPushGateway? gateway,
    SystemPushPendingStore? pendingStore,
    this.onOpen,
    this.onForeground,
  }) : _port = gateway ?? PlatformSystemPushGateway(),
       _pending = pendingStore ?? SecureSystemPushPendingStore() {
    _owner = ownerID();
    _token = authorizationHeader();
    _api = SystemPushAPI(
      authorizationHeader: authorizationHeader,
      current: () => current,
      baseUrl: apiBaseUrl,
      client: client,
      onDenied: _retire,
    );
    identityChanges.addListener(_observe);
    WidgetsBinding.instance.addObserver(this);
    _events = _port.events.listen(_event);
    _rotations = _port.addressChanges.listen(_rotation);
  }
  final String? Function() authorizationHeader, ownerID;
  final bool Function() sourceCurrent;
  final Listenable identityChanges;
  final Future<void> Function(InboxItem, PushEvent)? onOpen;
  final VoidCallback? onForeground;
  final SystemPushGateway _port;
  final SystemPushPendingStore _pending;
  late final SystemPushAPI _api;
  late final String? _owner, _token;
  StreamSubscription<PushDelivery>? _events;
  StreamSubscription<String>? _rotations;
  PushInstallation? installation;
  PushView? view;
  PushPending? pending;
  List<String> kinds = [];
  String? error;
  bool _disposed = false,
      _retired = false,
      busy = false,
      initialized = false,
      uncertain = false;
  int _serial = 0;
  Timer? _expiry;
  int _openSerial = 0;
  PushDelivery? _deferredOpen;
  final _openedEvents = <(String, String, int)>[];
  String? _address;
  bool? _devicePermission;
  bool get foreground =>
      WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed;
  bool get current {
    if (_disposed || _retired) return false;
    if (!sourceCurrent() ||
        ownerID() != _owner ||
        authorizationHeader() != _token ||
        _token == null ||
        !pushUUID.hasMatch(_owner ?? '')) {
      _retire(notify: false);
      return false;
    }
    return true;
  }

  bool get available =>
      current && _port.supported && kinds.contains(_port.kind);
  bool get canEnable =>
      available &&
      foreground &&
      !busy &&
      !uncertain &&
      installation != null &&
      view != null;
  bool get enabled =>
      current &&
      _devicePermission != false &&
      view?.enabled == true &&
      view?.validUntil?.isAfter(DateTime.now().toUtc()) == true;
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  bool _valid(int serial) => current && serial == _serial;
  void _retire({bool notify = true}) {
    if (_disposed || _retired) return;
    _retired = true;
    ++_serial;
    ++_openSerial;
    _deferredOpen = null;
    _address = null;
    busy = false;
    view = null;
    installation = null;
    kinds = [];
    pending = null;
    uncertain = false;
    error = '账号或工作身份已变化，请从当前设置重新打开。';
    _expiry?.cancel();
    unawaited(_port.stop(deleteAddress: false).catchError((_) {}));
    if (notify) _notify();
  }

  void _observe() {
    final was = _retired;
    if (!current && !was) _notify();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed && current && initialized && !busy)
      unawaited(refresh());
  }

  Future<void> initialize() async {
    if (!current || busy || initialized) return;
    final serial = ++_serial;
    busy = true;
    error = null;
    _notify();
    try {
      if (!_port.supported) {
        initialized = true;
        error = '当前版本的系统通知尚未配置。';
        return;
      }
      final options = await _api.options();
      if (!_valid(serial)) return;
      kinds = options;
      if (!kinds.contains(_port.kind)) {
        initialized = true;
        error = '系统通知服务尚未配置。';
        return;
      }
      final local = await _pending.installation();
      if (!_valid(serial)) return;
      installation = local;
      final original = await _pending.read(_api.origin, _owner!);
      if (!_valid(serial)) return;
      pending = original;
      uncertain = original != null;
      final snapshot = await _api.view(local);
      if (!_valid(serial)) return;
      _apply(snapshot);
      initialized = true;
      if (enabled && !uncertain) {
        await _port.start();
        if (!_valid(serial)) return;
        await _resumeBinding(serial);
      }
    } catch (e) {
      if (_valid(serial)) {
        view = null;
        _expiry?.cancel();
        _deferredOpen = null;
        await _port.stop(deleteAddress: false);
        error = e is FormatException ? e.message : '系统通知状态暂不可读取，请重试。';
      }
    } finally {
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
        _drainOpen();
      }
    }
  }

  void _apply(PushView value) {
    view = value;
    _devicePermission = null;
    _expiry?.cancel();
    if (value.enabled && value.validUntil != null) {
      final time = value.validUntil!.difference(DateTime.now().toUtc());
      if (time > Duration.zero)
        _expiry = Timer(time, () {
          unawaited(_port.stop(deleteAddress: false).catchError((_) {}));
          _notify();
        });
    }
  }

  Future<void> refresh() async {
    if (!current || busy) return;
    if (!initialized) {
      await initialize();
      return;
    }
    if (installation == null) return;
    final serial = ++_serial;
    busy = true;
    error = null;
    _notify();
    try {
      final snapshot = await _api.view(installation!);
      if (!_valid(serial)) return;
      _apply(snapshot);
      if (!enabled) {
        await _port.stop(deleteAddress: false);
      } else if (!uncertain) {
        await _port.start();
        if (_valid(serial)) await _resumeBinding(serial);
      }
    } catch (e) {
      if (_valid(serial)) {
        view = null;
        error = '系统通知状态暂不可核实，请稍后刷新。';
        await _port.stop(deleteAddress: false);
      }
    } finally {
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
        _drainOpen();
      }
    }
  }

  Future<void> enable() async {
    if (!canEnable) return;
    final serial = ++_serial;
    busy = true;
    error = null;
    _notify();
    try {
      await _port.start();
      if (!_valid(serial) || !foreground) return;
      final address = await _port.register(requestPermission: true);
      if (!_valid(serial) || !foreground) {
        await _port.stop(deleteAddress: false);
        return;
      }
      if (address == null) {
        _devicePermission = false;
        error = '系统通知权限未开启，可在设备设置中修改。';
        await _port.stop(deleteAddress: false);
        return;
      }
      await _register(serial, address);
    } catch (e) {
      if (_valid(serial))
        error = uncertain
            ? '设备绑定结果尚未确认，请核实原操作。'
            : view == null
            ? '设备操作已受理，当前状态尚未核实，请刷新。'
            : '系统通知未能开启，请检查权限后重试。';
    } finally {
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
        _drainOpen();
      }
    }
  }

  Future<void> _resumeBinding(int serial) async {
    if (!_valid(serial) ||
        !foreground ||
        uncertain ||
        installation == null ||
        view?.enabled != true)
      return;
    final address = await _port.register(requestPermission: false);
    if (!_valid(serial) || !foreground) return;
    if (address == null) {
      _devicePermission = false;
      _address = null;
      _deferredOpen = null;
      error = '设备系统通知权限未开启，请在设备设置检查。';
      await _port.stop(deleteAddress: false);
      return;
    }
    final binding = view!;
    final matches = await _api.addressMatches(
      installation!,
      binding,
      kind: _port.kind,
      address: address.address,
    );
    if (!_valid(serial) || !foreground || !identical(view, binding)) return;
    _devicePermission = true;
    if (matches) {
      _address = address.address;
      return;
    }
    await _register(serial, address);
  }

  Future<void> _register(int serial, PushDeviceAddress address) async {
    if (!_valid(serial) ||
        !foreground ||
        view == null ||
        installation == null ||
        uncertain)
      return;
    final op = PushPending(
      owner: _owner!,
      id: newPushID(),
      operation: 'REGISTER',
      expectedVersion: view!.version,
    );
    await _pending.write(_api.origin, op);
    if (!_valid(serial) || !foreground) {
      await _pending.delete(_api.origin, op);
      return;
    }
    pending = op;
    uncertain = true;
    _deferredOpen = null;
    ++_openSerial;
    await _api.register(
      installation!,
      op,
      kind: _port.kind,
      platform: _port.platform,
      address: address.address,
      permission: address.permission,
    );
    if (!_valid(serial)) return;
    await _pending.delete(_api.origin, op);
    if (!_valid(serial)) return;
    pending = null;
    uncertain = false;
    view = null;
    _expiry?.cancel();
    _address = address.address;
    final result = await _api.view(installation!);
    if (_valid(serial)) _apply(result);
  }

  Future<void> disable() async {
    if (!current ||
        !foreground ||
        busy ||
        uncertain ||
        installation == null ||
        view == null)
      return;
    final serial = ++_serial;
    busy = true;
    error = null;
    _notify();
    try {
      await _port.stop(deleteAddress: true);
      if (!_valid(serial) || !foreground) return;
      final op = PushPending(
        owner: _owner!,
        id: newPushID(),
        operation: 'REVOKE',
        expectedVersion: view!.version,
      );
      await _pending.write(_api.origin, op);
      if (!_valid(serial)) {
        await _pending.delete(_api.origin, op);
        return;
      }
      pending = op;
      uncertain = true;
      _deferredOpen = null;
      ++_openSerial;
      _address = null;
      await _api.revoke(installation!, op);
      if (!_valid(serial)) return;
      await _pending.delete(_api.origin, op);
      if (!_valid(serial)) return;
      pending = null;
      uncertain = false;
      view = null;
      _expiry?.cancel();
      final result = await _api.view(installation!);
      if (_valid(serial)) _apply(result);
    } catch (_) {
      if (_valid(serial))
        error = uncertain ? '停用结果尚未确认，请核实原操作。' : '系统通知尚未停用，请稍后重试。';
    } finally {
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
        _drainOpen();
      }
    }
  }

  Future<void> recover() async {
    if (!current ||
        !foreground ||
        busy ||
        !uncertain ||
        pending == null ||
        installation == null)
      return;
    final serial = ++_serial, op = pending!;
    busy = true;
    error = null;
    _notify();
    try {
      await _api.recover(installation!, op);
      if (!_valid(serial)) return;
      await _pending.delete(_api.origin, op);
      if (!_valid(serial)) return;
      pending = null;
      uncertain = false;
      view = null;
      _expiry?.cancel();
      final result = await _api.view(installation!);
      if (!_valid(serial)) return;
      _apply(result);
      if (result.enabled) {
        await _port.start();
        if (_valid(serial)) await _resumeBinding(serial);
      } else
        await _port.stop(deleteAddress: false);
    } catch (_) {
      if (_valid(serial)) error = '原设备通知操作仍无法核实，请稍后再次核实。';
    } finally {
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
        _drainOpen();
      }
    }
  }

  Future<void> _rotation(String token) async {
    if (!canEnable || !enabled || token.isEmpty || token == _address) return;
    final serial = ++_serial;
    busy = true;
    _notify();
    try {
      final permission = await _port.register(requestPermission: false);
      if (permission == null || permission.address != token || !_valid(serial))
        return;
      await _register(serial, permission);
    } catch (_) {
      if (_valid(serial)) error = '系统通知地址更新尚未核实。';
    } finally {
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
        _drainOpen();
      }
    }
  }

  Future<void> _event(PushDelivery delivery) async {
    if (!current ||
        !enabled ||
        uncertain ||
        installation == null ||
        view?.matches(delivery.event) != true)
      return;
    if (!foreground) {
      if (delivery.opened) _deferredOpen = delivery;
      return;
    }
    if (busy) {
      if (delivery.opened) _deferredOpen = delivery;
      return;
    }
    if (!delivery.opened) {
      onForeground?.call();
      return;
    }
    final eventKey = (
      delivery.event.id,
      delivery.event.bindingID,
      delivery.event.version,
    );
    if (_openedEvents.contains(eventKey)) return;
    _openedEvents.add(eventKey);
    if (_openedEvents.length > 32) _openedEvents.removeAt(0);
    final serial = _serial, binding = view, open = ++_openSerial;
    try {
      final item = await _api.open(installation!, delivery.event);
      if (_valid(serial) &&
          open == _openSerial &&
          foreground &&
          identical(view, binding) &&
          view?.matches(delivery.event) == true)
        await onOpen?.call(item, delivery.event);
    } catch (_) {
      if (_valid(serial)) {
        error = '这条系统通知当前不可打开，请从通知列表重新查看。';
        _notify();
      }
    }
  }

  void _drainOpen() {
    final delivery = _deferredOpen;
    _deferredOpen = null;
    if (delivery != null && current && !busy && !uncertain)
      unawaited(_event(delivery));
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    ++_serial;
    ++_openSerial;
    _deferredOpen = null;
    identityChanges.removeListener(_observe);
    WidgetsBinding.instance.removeObserver(this);
    _expiry?.cancel();
    unawaited(_events?.cancel());
    unawaited(_rotations?.cancel());
    _port.dispose();
    _api.dispose();
    super.dispose();
  }
}
