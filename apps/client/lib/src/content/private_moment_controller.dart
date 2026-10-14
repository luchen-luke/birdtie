import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'moment_time_choice.dart';
import 'moment_control.dart';
import 'moment_control_pending_store.dart';
import '../workspace/human_causal_control.dart';
import 'package:flutter/scheduler.dart';

class PrivateMoment {
  const PrivateMoment({
    required this.id,
    required this.cityID,
    required this.placeID,
    required this.title,
    required this.body,
    required this.timePrecision,
    required this.locationPrecision,
    required this.status,
    required this.revision,
    required this.activityIDs,
    required this.communityID,
    required this.organizationID,
    this.authorAccountID,
    this.visibility,
    this.occurredAt,
    this.createdAt,
    this.updatedAt,
  });
  final String id,
      cityID,
      placeID,
      title,
      body,
      timePrecision,
      locationPrecision,
      status;
  final int revision;
  final List<String> activityIDs;
  final String communityID, organizationID;
  final String? authorAccountID;
  final String? visibility;
  String get visibilityLabel => switch (visibility) {
    'private' => '仅自己可见',
    'public' => '公开可见',
    _ => '可见范围未确认',
  };
  String get statusLabel => switch (status) {
    'draft' => '草稿',
    'published' => '已发布',
    'withdrawn' => '已撤回',
    _ => '状态未确认',
  };
  final DateTime? occurredAt, createdAt, updatedAt;
  MomentTimeValue get time =>
      MomentTimeValue.fromWire(timePrecision, occurredAt);
  factory PrivateMoment.fromJson(Map<String, dynamic> json) {
    DateTime? date(String key) {
      if (json[key] == null) return null;
      final value = json[key] as String;
      if (!RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(value)) {
        throw const FormatException('timestamp needs an offset');
      }
      return DateTime.parse(value).toUtc();
    }

    final occurrence = date('occurredAt');
    final precision = json['timePrecision'] as String;
    MomentTimeValue.fromWire(precision, occurrence);
    return PrivateMoment(
      id: json['id'] as String,
      authorAccountID: json['authorAccountId'] as String?,
      visibility: json['visibility'] as String?,
      cityID: json['cityId'] as String,
      placeID: json['placeId'] as String? ?? '',
      title: json['title'] as String,
      body: json['body'] as String? ?? '',
      timePrecision: precision,
      occurredAt: occurrence,
      createdAt: date('createdAt'),
      updatedAt: date('updatedAt'),
      locationPrecision: json['locationPrecision'] as String,
      status: json['status'] as String,
      revision: json['revision'] as int,
      activityIDs: List.unmodifiable(
        (json['activityIds'] as List<dynamic>? ?? const []).cast<String>(),
      ),
      communityID: json['communityId'] as String? ?? '',
      organizationID: json['organizationId'] as String? ?? '',
    );
  }
  bool get isEditableDraft => status == 'draft' && visibility != 'public';
}

class PrivateMomentController extends ChangeNotifier {
  PrivateMomentController({
    required this.authorizationHeader,
    this.ownerID,
    this.identityChanges,
    MomentControlPendingStore? pendingStore,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _apiBase = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       operationStore =
           pendingStore ?? const SecureMomentControlPendingStore() {
    _syncActor();
    identityChanges?.addListener(_onIdentityChanged);
  }
  final String _apiBase;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final Listenable? identityChanges;
  final http.Client _client;
  final bool _ownsClient;
  final MomentControlPendingStore operationStore;
  // Read-only references for another human consumer of this same Moment source.
  // The borrower does not own or close this client.
  http.Client get borrowedClient => _client;
  String get apiBaseUrl => _apiBase;
  int get identityEpoch {
    _syncActor();
    return _epoch;
  }

  List<PrivateMoment> _moments = const [];
  String? _actor, _owner, _loadedToken, _loadedOwner, _error;
  bool _loading = false,
      _saving = false,
      _closed = false,
      _creationUncertain = false;
  int _epoch = 0, _request = 0, _logicalEpoch = 0;
  int get logicalIdentityEpoch {
    _syncActor();
    return _logicalEpoch;
  }

  late final _controlAPI = MomentControlAPI(
    client: _client,
    apiBaseUrl: _apiBase,
    authorizationHeader: authorizationHeader,
  );
  List<MomentControlPending> pendingOperations = [];
  MomentControlPreview? operationPreview;
  MomentControlReceipt? operationReceipt;
  String? operationError, operationNotice, _controlOwner, _lastControlOwner;
  bool operationBusy = false, _pendingReady = false;
  MomentControlPending? _notSubmitted;
  bool get canStartOperation {
    _syncActor();
    return !_closed &&
        _actor != null &&
        _ownerReady &&
        humanControlID(_controlOwner) &&
        _pendingReady &&
        pendingOperations.isEmpty &&
        _notSubmitted == null &&
        !operationBusy &&
        !_saving;
  }

  List<MomentControlPending> get recoveryOperations {
    _syncActor();
    final local = _notSubmitted;
    return [
      ...pendingOperations,
      if (local != null && !pendingOperations.any((p) => p.same(local))) local,
    ];
  }

  bool currentOwnedVersion(PrivateMoment moment) {
    _syncActor();
    return _ownsVersion(moment);
  }

  Timer? _previewTimer;
  int _previewEpoch = -1;
  bool Function()? _previewSourceCurrent;

  int? _readStatus;
  int? get readStatus {
    _syncActor();
    return _readStatus;
  }

  // Detect observed account changes before both reads and writes. A late response
  // from a previous actor cannot restore its data, errors or saving state.
  String? _syncActor() {
    final token = authorizationHeader();
    final owner = ownerID?.call();
    if (token != _actor || owner != _owner) {
      final changedOwner = ownerID != null
          ? owner != _owner
          : token == null || _actor == null;
      if (changedOwner) {
        _logicalEpoch++;
        pendingOperations = [];
        operationReceipt = null;
        _controlOwner = null;
        operationNotice = null;
        _notSubmitted = null;
      }
      _previewTimer?.cancel();
      operationPreview = null;
      _previewSourceCurrent = null;
      operationBusy = false;
      _pendingReady = false;
      operationError = null;
      _actor = token;
      _owner = owner;
      _epoch++;
      _request++;
      _loadedToken = null;
      _loadedOwner = null;
      _moments = const [];
      _loading = false;
      _saving = false;
      _error = null;
      _readStatus = null;
      _creationUncertain = false;
    }
    return token;
  }

  void _onIdentityChanged() {
    if (_closed) return;
    final epoch = _epoch;
    _syncActor();
    if (_actor == null || !_ownerReady) {
      _notify();
      return;
    }
    // Another listener may already have observed the renewed credential via
    // identityEpoch/current getters. The cleared authoritative read still
    // needs replacing even when this listener sees an unchanged epoch.
    if (epoch == _epoch && _loadedToken == _actor && _loadedOwner == _owner)
      return;
    if (_loading) return;
    // Retire on the actual event, not the next widget frame. A -> B -> A
    // receives new epochs and authoritative reads for each current identity.
    unawaited(refresh());
  }

  bool get _ownerReady => ownerID == null || _owner?.isNotEmpty == true;

  void _checkAuthor(PrivateMoment moment, String? owner) {
    if (ownerID != null && (owner == null || moment.authorAccountID != owner)) {
      throw const FormatException('moment author does not match current owner');
    }
  }

  List<PrivateMoment> get moments {
    _syncActor();
    return _ownerReady && _loadedToken == _actor && _loadedOwner == _owner
        ? _moments
        : const [];
  }

  bool get loading {
    _syncActor();
    return _loading;
  }

  bool get saving {
    _syncActor();
    return _saving;
  }

  String? get error {
    _syncActor();
    return _error;
  }

  bool get creationUncertain {
    _syncActor();
    return _creationUncertain;
  }

  void confirmCreationChecked() {
    _syncActor();
    // A list inspection cannot prove the outcome of an original operation.
    if (pendingOperations.isNotEmpty) {
      operationError = '请核实或停止原操作，不能根据列表文字判断上次保存结果。';
      _notify();
    }
  }

  bool _current(String token, int epoch) =>
      !_closed && _syncActor() == token && _epoch == epoch;
  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');
  void _notify() {
    if (_closed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      SchedulerBinding.instance.addPostFrameCallback((_) {
        if (!_closed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  bool _ownsVersion(PrivateMoment moment) =>
      _loadedToken == _actor &&
      _loadedOwner == _owner &&
      _ownerReady &&
      (ownerID == null || moment.authorAccountID == _owner) &&
      _moments.any(
        (item) => item.id == moment.id && item.revision == moment.revision,
      );

  Future<void> refresh({bool Function()? current}) async {
    if (_closed || current?.call() == false) return;
    final token = _syncActor(), epoch = _epoch, request = ++_request;
    final owner = _owner;
    if (token == null || _apiBase.isEmpty) {
      _loading = false;
      _notify();
      return;
    }
    if (!_ownerReady) {
      _loading = false;
      _error = '登录身份尚未确认，请重新登录后读取私人草稿。';
      _notify();
      return;
    }
    if (!_pendingReady) await loadOperations();
    if (!_current(token, epoch) ||
        request != _request ||
        current?.call() == false)
      return;
    _loading = true;
    _error = null;
    _readStatus = null;
    _notify();
    try {
      if (current?.call() == false ||
          !_current(token, epoch) ||
          request != _request) {
        return;
      }
      final response = await _client
          .get(_endpoint('/v1/me/moments'), headers: {'Authorization': token})
          .timeout(const Duration(seconds: 10));
      if (current?.call() == false ||
          !_current(token, epoch) ||
          request != _request) {
        return;
      }
      _readStatus = response.statusCode;
      if (response.statusCode != 200) {
        if (response.statusCode == 403 || response.statusCode == 404) {
          _moments = const [];
          _loadedToken = null;
          _loadedOwner = null;
        }
        throw const FormatException('unavailable');
      }
      final rows =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as List<dynamic>;
      final next = rows
          .map((row) => PrivateMoment.fromJson(row as Map<String, dynamic>))
          .toList();
      for (final moment in next) {
        _checkAuthor(moment, owner);
      }
      if (_current(token, epoch) &&
          request == _request &&
          current?.call() != false) {
        _moments = List.unmodifiable(next);
        _loadedToken = token;
        _loadedOwner = owner;
      }
    } catch (error) {
      if (_current(token, epoch) &&
          request == _request &&
          current?.call() != false) {
        _error = switch (_readStatus) {
          401 => '登录已失效，请重新登录后读取私人草稿。',
          403 => '当前账号无权读取私人草稿。',
          404 => '私人草稿暂不可访问，请返回后重新查看。',
          429 => '读取过于频繁，请稍后重试。',
          int status when status >= 500 => '私人草稿服务暂不可用，请稍后重试。',
          200 => '私人草稿数据暂不可用，请重新读取。',
          null =>
            error is TimeoutException
                ? '读取私人草稿超时，请检查网络后重试。'
                : '无法连接私人草稿服务，请检查网络后重试。',
          _ => '未能读取私人草稿，请稍后重试。',
        };
      }
    } finally {
      if (_current(token, epoch) && request == _request) {
        _loading = false;
        _notify();
      }
    }
  }

  Map<String, Object?> _input({
    required String cityID,
    required String title,
    required String body,
    required String placeID,
    String? activityID,
    String? communityID,
    String? organizationID,
    required MomentTimeValue time,
    String? locationPrecision,
  }) => {
    'cityId': cityID.trim(),
    'placeId': placeID.trim().toLowerCase(),
    'title': title.trim(),
    'body': body.trim(),
    'timePrecision': time.precision,
    if (time.occurredAt != null) 'occurredAt': momentUTC(time.occurredAt!),
    'locationPrecision':
        locationPrecision ?? (placeID.isEmpty ? 'city' : 'place'),
    if (activityID != null) 'activityId': activityID.trim().toLowerCase(),
    if (communityID != null) 'communityId': communityID.trim().toLowerCase(),
    if (organizationID != null)
      'organizationId': organizationID.trim().toLowerCase(),
  };
  Future<bool> _resolveControlOwner(bool Function() valid) async {
    final found = ownerID?.call() ?? await _controlAPI.api.owner(valid);
    if (!valid() || !humanControlID(found)) return false;
    if (_lastControlOwner != null && _lastControlOwner != found) {
      _logicalEpoch++;
      pendingOperations = [];
      operationPreview = null;
      operationReceipt = null;
    }
    _controlOwner = found;
    _lastControlOwner = found;
    return true;
  }

  Future<void> loadOperations() async {
    final token = _syncActor(), e = _epoch;
    if (token == null || _closed || !_ownerReady) return;
    bool valid() => _current(token, e);
    _pendingReady = false;
    try {
      if (!await _resolveControlOwner(valid)) return;
      final records = await operationStore.read(
        _controlAPI.api.origin,
        _controlOwner!,
      );
      if (!valid()) return;
      pendingOperations = records;
      _pendingReady = true;
      _creationUncertain = records.any(
        (p) => p.action == 'CREATE' && p.stage == 'UNKNOWN',
      );
      operationError = null;
    } catch (_) {
      if (valid()) operationError = '本机动态操作记录暂不可读取。请先检查原记录，不要再次提交。';
    } finally {
      if (valid()) _notify();
    }
  }

  void retirePreview() {
    _previewTimer?.cancel();
    operationPreview = null;
    _previewSourceCurrent = null;
    _previewEpoch = -1;
    _notify();
  }

  Future<MomentControlPreview?> previewCreate({
    required String cityID,
    required String title,
    required String body,
    String placeID = '',
    String activityID = '',
    String communityID = '',
    String organizationID = '',
    MomentTimeValue time = MomentTimeValue.unknown,
    bool Function()? current,
  }) => _prepare(
    MomentControlInput(
      operationID: newMomentOperationID(),
      action: 'CREATE',
      expectedRevision: 0,
      input: _input(
        cityID: cityID,
        title: title,
        body: body,
        placeID: placeID,
        activityID: activityID,
        communityID: communityID,
        organizationID: organizationID,
        time: time,
      ),
    ),
    current,
  );
  Future<MomentControlPreview?> previewUpdate({
    required PrivateMoment moment,
    required String title,
    required String body,
    required String placeID,
    String? activityID,
    String? communityID,
    String? organizationID,
    MomentTimeValue? time,
    String? locationPrecision,
    bool Function()? current,
  }) async {
    _syncActor();
    if (!moment.isEditableDraft || !_ownsVersion(moment)) return null;
    return _prepare(
      MomentControlInput(
        operationID: newMomentOperationID(),
        action: 'UPDATE',
        momentID: moment.id,
        expectedRevision: moment.revision,
        input: _input(
          cityID: moment.cityID,
          title: title,
          body: body,
          placeID: placeID,
          activityID: activityID,
          communityID: communityID,
          organizationID: organizationID,
          time: time ?? moment.time,
          locationPrecision: locationPrecision,
        ),
      ),
      current,
    );
  }

  Future<MomentControlPreview?> previewWithdraw(
    PrivateMoment moment, {
    bool Function()? current,
  }) async {
    _syncActor();
    if (!_ownsVersion(moment)) return null;
    return _prepare(
      MomentControlInput(
        operationID: newMomentOperationID(),
        action: 'WITHDRAW',
        momentID: moment.id,
        expectedRevision: moment.revision,
      ),
      current,
    );
  }

  Future<MomentControlPreview?> previewPublish(
    PrivateMoment moment, {
    bool Function()? current,
  }) async {
    _syncActor();
    if (!moment.isEditableDraft ||
        !_ownsVersion(moment) ||
        moment.placeID.isEmpty ||
        moment.locationPrecision != 'place')
      return null;
    return _prepare(
      MomentControlInput(
        operationID: newMomentOperationID(),
        action: 'PUBLISH',
        momentID: moment.id,
        expectedRevision: moment.revision,
        input: _input(
          cityID: moment.cityID,
          title: moment.title,
          body: moment.body,
          placeID: moment.placeID,
          time: moment.time,
          locationPrecision: moment.locationPrecision,
        ),
      ),
      current,
    );
  }

  Future<MomentControlPreview?> previewPublicUpdate({
    required PrivateMoment moment,
    required String title,
    required String body,
    bool Function()? current,
  }) async {
    _syncActor();
    if (moment.status != 'published' ||
        moment.visibility != 'public' ||
        !_ownsVersion(moment))
      return null;
    return _prepare(
      MomentControlInput(
        operationID: newMomentOperationID(),
        action: 'UPDATE_PUBLIC',
        momentID: moment.id,
        expectedRevision: moment.revision,
        input: _input(
          cityID: moment.cityID,
          title: title,
          body: body,
          placeID: moment.placeID,
          time: moment.time,
          locationPrecision: moment.locationPrecision,
        ),
      ),
      current,
    );
  }

  /// Read this exact owned record instead of relying on the bounded owner list.
  Future<PrivateMoment?> readOwned(
    String id, {
    bool Function()? current,
  }) async {
    final token = _syncActor(), epoch = _epoch, owner = _owner;
    if (token == null || !_ownerReady || _closed || current?.call() == false)
      return null;
    bool valid() => _current(token, epoch) && current?.call() != false;
    if (!_pendingReady) await loadOperations();
    if (!valid()) return null;
    final response = await _client
        .get(
          _endpoint('/v1/me/moments/${Uri.encodeComponent(id)}'),
          headers: {'Authorization': token},
        )
        .timeout(const Duration(seconds: 10));
    if (!valid() || response.statusCode != 200) return null;
    final raw = jsonDecode(utf8.decode(response.bodyBytes));
    final moment = PrivateMoment.fromJson(
      Map<String, dynamic>.from(raw['data'] as Map),
    );
    if (!valid() || moment.id != id) return null;
    _checkAuthor(moment, owner);
    _loadedToken = token;
    _loadedOwner = owner;
    _moments = [
      for (final item in _moments)
        if (item.id != id) item,
      moment,
    ];
    _notify();
    return moment;
  }

  Future<MomentControlPreview?> _prepare(
    MomentControlInput input,
    bool Function()? sourceCurrent,
  ) async {
    final token = _syncActor(), e = _epoch;
    if (token == null ||
        _closed ||
        !_ownerReady ||
        operationBusy ||
        sourceCurrent?.call() == false)
      return null;
    bool valid() => _current(token, e) && sourceCurrent?.call() != false;
    operationBusy = true;
    operationError = null;
    operationNotice = null;
    operationReceipt = null;
    retirePreview();
    try {
      if (!await _resolveControlOwner(valid)) return null;
      final records = await operationStore.read(
        _controlAPI.api.origin,
        _controlOwner!,
      );
      if (!valid()) return null;
      pendingOperations = records;
      _pendingReady = true;
      if (records.isNotEmpty) {
        operationError = '有待核实的原动态操作。请先核实或停止原操作。';
        return null;
      }
      final p = await _controlAPI.preview(input, valid);
      if (!valid()) return null;
      operationPreview = p;
      _previewSourceCurrent = sourceCurrent;
      _previewEpoch = e;
      _previewTimer = Timer(p.expiresAt.difference(DateTime.now().toUtc()), () {
        if (identical(operationPreview, p)) {
          operationPreview = null;
          operationError = '这次确认已到期，旧批准已移除。请重新检查。';
          _notify();
        }
      });
      return p;
    } catch (_) {
      if (valid()) operationError = '当前动态、版本或关联已变化，暂不可取得具体预览。';
      return null;
    } finally {
      if (_current(token, e)) {
        operationBusy = false;
        _notify();
      }
    }
  }

  bool previewCurrent(MomentControlPreview p, {bool Function()? current}) =>
      !_closed &&
      _syncActor() != null &&
      _epoch == _previewEpoch &&
      identical(operationPreview, p) &&
      p.live &&
      _previewSourceCurrent?.call() != false &&
      current?.call() != false;
  Future<bool> commit(
    MomentControlPreview p, {
    bool Function()? current,
  }) async {
    if (operationBusy ||
        !_pendingReady ||
        _controlOwner == null ||
        !previewCurrent(p, current: current))
      return false;
    final token = _actor!, e = _epoch, owner = _controlOwner!;
    bool valid() => _current(token, e) && previewCurrent(p, current: current);
    var record = MomentControlPending.preview(p), sent = false;
    operationBusy = true;
    _saving = true;
    operationError = null;
    operationNotice = null;
    _notSubmitted = record;
    _notify();
    try {
      await operationStore.write(_controlAPI.api.origin, owner, record);
      if (!valid()) return false;
      var records = await operationStore.read(_controlAPI.api.origin, owner);
      if (!valid()) return false;
      if (!records.any((v) => v.same(record) && v.stage == 'PREPARED'))
        throw StateError('原准备未读回');
      record = record.unknown;
      await operationStore.write(_controlAPI.api.origin, owner, record);
      if (!valid()) return false;
      records = await operationStore.read(_controlAPI.api.origin, owner);
      if (!valid()) return false;
      if (!records.any((v) => v.same(record) && v.stage == 'UNKNOWN'))
        throw StateError('原提交记录未读回');
      pendingOperations = records;
      sent = true;
      _notSubmitted = null;
      final receipt = await _controlAPI.put(p, valid);
      if (!valid()) return false;
      return await _finishOperation(receipt, record, () => _current(token, e));
    } catch (_) {
      if (_current(token, e)) {
        try {
          final records = await operationStore.read(
            _controlAPI.api.origin,
            owner,
          );
          if (_current(token, e)) pendingOperations = records;
        } catch (_) {
          _pendingReady = false;
        }
        if (_current(token, e)) {
          _creationUncertain = sent && p.input.action == 'CREATE';
          operationError = sent
              ? '保存结果待核实，可能已经完成。只能核实或停止原操作，不会重新提交。'
              : '尚未提交。本机准备记录需要检查，不会直接重新发送。';
        }
      }
      return false;
    } finally {
      if (_current(token, e)) {
        // Expiry/cancellation during durable preparation still leaves a
        // findable original selector, even though no PUT was dispatched.
        try {
          final records = await operationStore.read(
            _controlAPI.api.origin,
            owner,
          );
          if (_current(token, e)) pendingOperations = records;
        } catch (_) {
          if (_current(token, e)) _pendingReady = false;
        }
        if (!_current(token, e)) return false;
        operationBusy = false;
        _saving = false;
        retirePreview();
        _notify();
      }
    }
  }

  bool locallyPrepared(MomentControlPending p) =>
      p.stage == 'PREPARED' || _notSubmitted?.same(p) == true;
  Future<void> repairPrepared(MomentControlPending p) async {
    final token = _syncActor(), e = _epoch;
    if (token == null || operationBusy || !locallyPrepared(p)) return;
    bool valid() => _current(token, e);
    operationBusy = true;
    _notify();
    try {
      if (!await _resolveControlOwner(valid)) return;
      final list = await operationStore.read(
        _controlAPI.api.origin,
        _controlOwner!,
      );
      if (!valid()) return;
      final selected = list.where((v) => v.same(p)).firstOrNull;
      if (selected != null &&
          (selected.stage == 'PREPARED' ||
              _notSubmitted?.same(selected) == true))
        await operationStore.remove(
          _controlAPI.api.origin,
          _controlOwner!,
          selected,
        );
      if (!valid()) return;
      pendingOperations = await operationStore.read(
        _controlAPI.api.origin,
        _controlOwner!,
      );
      if (!valid()) return;
      _notSubmitted = null;
      _pendingReady = true;
      operationError = null;
      operationNotice = '尚未提交的本机准备记录已检查，可重新取得具体预览。';
    } catch (_) {
      if (valid()) operationError = '本机准备记录仍不可核实，没有再次提交。';
    } finally {
      if (valid()) {
        operationBusy = false;
        _notify();
      }
    }
  }

  Future<void> recoverOperation(
    MomentControlPending p, {
    bool stop = false,
  }) async {
    if (locallyPrepared(p)) {
      await repairPrepared(p);
      return;
    }
    final token = _syncActor(), e = _epoch;
    if (token == null || operationBusy) return;
    bool valid() => _current(token, e);
    operationBusy = true;
    operationError = null;
    operationNotice = null;
    retirePreview();
    try {
      if (!await _resolveControlOwner(valid)) return;
      final raw = await _controlAPI.original(p.operationID, valid, stop: stop);
      if (!valid()) return;
      final r = MomentControlReceipt.fromJson(
        raw,
        operationID: p.operationID,
        action: p.action,
        momentID: p.momentID,
        expectedRevision: p.expectedRevision,
        digest: p.payloadDigest,
      );
      await _finishOperation(r, p, valid);
    } catch (_) {
      if (valid()) operationError = '原结果仍待核实；未找到回执不等于没有保存。只可核实或停止原操作。';
    } finally {
      if (valid()) {
        operationBusy = false;
        _saving = false;
        _notify();
      }
    }
  }

  Future<bool> _finishOperation(
    MomentControlReceipt r,
    MomentControlPending p,
    bool Function() valid,
  ) async {
    operationReceipt = r;
    operationNotice = r.succeeded
        ? '原操作已完成。当前动态需重新读取，不能由回执推断现在仍可见。'
        : '原操作已停止，不会执行迟到提交；已完成的保存不会被撤销。';
    operationError = null;
    _creationUncertain = false;
    try {
      await operationStore.remove(_controlAPI.api.origin, _controlOwner!, p);
      if (!valid()) return false;
      pendingOperations = await operationStore.read(
        _controlAPI.api.origin,
        _controlOwner!,
      );
      if (!valid()) return false;
      _pendingReady = true;
    } catch (_) {
      if (valid()) operationError = '原结果已核实，本机记录尚未清理。可再次核实同一原操作。';
    }
    if (valid()) await refresh(current: valid);
    // Current list failure does not erase an immutable original fact.
    return valid() && r.succeeded;
  }

  /// Retained SDK surface requires the exact explicit approval. Raw writes are retired.
  Future<bool> create({
    required String cityID,
    required String title,
    required String body,
    String placeID = '',
    String activityID = '',
    String communityID = '',
    String organizationID = '',
    MomentTimeValue time = MomentTimeValue.unknown,
    MomentControlPreview? approved,
  }) async {
    if (approved == null) {
      _syncActor();
      if (_actor != null) operationError = '请先检查具体预览并确认保存。';
      return false;
    }
    final expected = MomentControlInput(
      operationID: approved.input.operationID,
      action: 'CREATE',
      expectedRevision: 0,
      input: _input(
        cityID: cityID,
        title: title,
        body: body,
        placeID: placeID,
        activityID: activityID,
        communityID: communityID,
        organizationID: organizationID,
        time: time,
      ),
    );
    return expected.digest == approved.input.digest ? commit(approved) : false;
  }

  Future<bool> update({
    required PrivateMoment moment,
    required String title,
    required String body,
    required String placeID,
    String? activityID,
    String? communityID,
    String? organizationID,
    MomentTimeValue? time,
    MomentControlPreview? approved,
  }) async {
    if (approved == null) return false;
    final expected = MomentControlInput(
      operationID: approved.input.operationID,
      action: 'UPDATE',
      momentID: moment.id,
      expectedRevision: moment.revision,
      input: _input(
        cityID: moment.cityID,
        title: title,
        body: body,
        placeID: placeID,
        activityID: activityID,
        communityID: communityID,
        organizationID: organizationID,
        time: time ?? moment.time,
      ),
    );
    return expected.digest == approved.input.digest ? commit(approved) : false;
  }

  Future<bool> withdraw(
    PrivateMoment moment, {
    MomentControlPreview? approved,
  }) async =>
      approved != null &&
          approved.input.action == 'WITHDRAW' &&
          approved.input.momentID == moment.id &&
          approved.input.expectedRevision == moment.revision
      ? commit(approved)
      : false;

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    _previewTimer?.cancel();
    operationPreview = null;
    identityChanges?.removeListener(_onIdentityChanged);
    _epoch++;
    _request++;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
