import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../content/moment_image_header.dart';
import '../content/moment_image_picker.dart';
import 'agent_reply_membership.dart';
import 'agent_workspace_controller.dart';
import 'task_image_pending_store.dart';
import 'local_image_draft.dart';

const taskImagePurpose = 'HUMAN_TASK_MESSAGE_ATTACHMENT';
final _taskImageID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
final _taskImageSHA = RegExp(r'^[0-9a-f]{64}$');

DateTime _taskImageDate(Object? value) {
  if (value is! String || !RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(value)) {
    throw const FormatException('图片期限无法核验');
  }
  return DateTime.parse(value).toUtc();
}

String? taskImagePrefix(AgentTask task, int index) {
  if (index < 0 ||
      index > 255 ||
      index >= task.messages.length ||
      task.messages[index].role != 'user')
    return null;
  return agentReplyTurnDigest(
    task.messages.take(index + 1).map((m) => (role: m.role, text: m.text)),
  );
}

bool taskImageDisplayMatches(
  AgentTask task,
  List<AgentMessage> shown,
  int index,
) {
  if (index < 0 ||
      index >= task.messages.length ||
      index >= shown.length ||
      task.messages[index].role != 'user')
    return false;
  for (var n = 0; n <= index; n++) {
    final native = task.messages[n], display = shown[n];
    if (native.role != display.role ||
        (native.role == 'user' && native.text != display.text))
      return false;
  }
  return true;
}

Future<ui.Image> _decodeTaskImage(MomentImageSelection selected) =>
    decodeLocalImagePixels(selected);

Future<Uint8List> _renderTaskImage(ui.Image pixels, List<ui.Rect> masks) =>
    renderLocalImagePixels(pixels, masks);

String? _taskImageSessionDigest(String? value) => value == null || value.isEmpty
    ? null
    : sha256.convert(utf8.encode(value)).toString();

/// Local editing state of the existing human-only Task image attachment. It
/// makes no HTTP request, creates no Task, and persists no pixels or approval.
class TaskImageDraftController extends ChangeNotifier
    implements ComposerImageDraftView {
  TaskImageDraftController({
    required this.authorizationHeader,
    required this.ownerID,
    required this.cityID,
    required this.sourceCurrent,
    required this.sourceChanges,
    this.preserveLocalOnSessionRenewal = false,
    MomentImagePicker? picker,
  }) : _picker = picker ?? MomentImagePicker() {
    _owner = ownerID();
    _city = cityID();
    _session = _taskImageSessionDigest(authorizationHeader());
    sourceChanges.addListener(_observe);
  }
  final String? Function() authorizationHeader, ownerID, cityID;
  final bool Function() sourceCurrent;
  final Listenable sourceChanges;
  // Opt-in only for a caller whose source lease excludes credentials and still
  // fences the exact owner, city, workspace and transport. No upload approval
  // or pending picker result survives credential renewal.
  final bool preserveLocalOnSessionRenewal;
  final MomentImagePicker _picker;
  bool supportsSource(MomentImageSource source) =>
      _picker.supportsSource(source);
  bool canSelectSource(MomentImageSource source) =>
      canSelect && supportsSource(source);
  late final String? _owner, _city;
  late String? _session;
  bool _disposed = false, _retired = false, busy = false, reviewed = false;
  int _serial = 0, _revision = 0;
  ui.Image? _pixels;
  Uint8List? localBytes;
  final _masks = <ui.Rect>[];
  String? error;

  int get localRevision => _revision;
  bool get locallyMasked => _masks.isNotEmpty;

  bool get current {
    final session = _taskImageSessionDigest(authorizationHeader());
    if (!_disposed &&
        !_retired &&
        preserveLocalOnSessionRenewal &&
        !busy &&
        sourceCurrent() &&
        _taskImageID.hasMatch(_owner ?? '') &&
        _owner == ownerID() &&
        _city != null &&
        _city.isNotEmpty &&
        _city == cityID() &&
        _session != null &&
        session != null &&
        _session != session) {
      _session = session;
      ++_serial;
      ++_revision;
      reviewed = false;
      error = null;
    }
    if (!_disposed &&
        !_retired &&
        (!sourceCurrent() ||
            !_taskImageID.hasMatch(_owner ?? '') ||
            _city == null ||
            _city.isEmpty ||
            _session == null ||
            _owner != ownerID() ||
            _city != cityID() ||
            _session != session)) {
      _retired = true;
      ++_serial;
      _clearLocal();
      busy = false;
    }
    return !_disposed && !_retired;
  }

  bool get canSelect => current && !busy;
  double get localAspectRatio =>
      _pixels == null ? 1 : _pixels!.width / _pixels!.height;
  bool get canUse =>
      current && !busy && reviewed && localBytes != null && _pixels != null;
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _observe() {
    final revision = _revision;
    if (!current) {
      error = '账户或探索范围已变化，请在当前会话重新选择图片。';
      _notify();
    } else if (_revision != revision) {
      _notify();
    }
  }

  void _clearLocal() {
    ++_revision;
    reviewed = false;
    localBytes = null;
    _pixels?.dispose();
    _pixels = null;
    _masks.clear();
  }

  bool _current(int serial) => current && serial == _serial;

  Future<void> select({
    MomentImageSource source = MomentImageSource.gallery,
  }) async {
    if (!canSelect) return;
    final serial = ++_serial;
    busy = true;
    error = null;
    _notify();
    ui.Image? decoded;
    try {
      if (await _picker.discardLostSelection() && _current(serial)) {
        error = '上次图片选择已失效，请在当前会话重新选择。';
      }
      if (!_current(serial)) return;
      final selected = await _picker.selectSource(source);
      if (!_current(serial) || selected == null) return;
      decoded = await _decodeTaskImage(selected);
      if (!_current(serial)) return;
      final bytes = await _renderTaskImage(decoded, const []);
      if (!_current(serial)) return;
      _clearLocal();
      _pixels = decoded;
      decoded = null;
      localBytes = bytes;
    } catch (e) {
      if (_current(serial))
        error = e is FormatException ? e.message : '图片未能处理，请重新选择。';
    } finally {
      decoded?.dispose();
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> mask(ui.Rect rectangle) async {
    if (!canSelect ||
        _pixels == null ||
        ![
          rectangle.left,
          rectangle.top,
          rectangle.right,
          rectangle.bottom,
        ].every((v) => v.isFinite))
      return;
    final rect = rectangle.intersect(const ui.Rect.fromLTRB(0, 0, 1, 1));
    if (rect.isEmpty) return;
    final serial = ++_serial;
    busy = true;
    reviewed = false;
    ++_revision;
    _masks.add(rect);
    _notify();
    try {
      final bytes = await _renderTaskImage(_pixels!, _masks);
      if (_current(serial)) localBytes = bytes;
    } catch (_) {
      if (_current(serial)) {
        _clearLocal();
        error = '遮盖未完成，请重新选择图片。';
      }
    } finally {
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  void setReviewed(bool value) {
    if (!current || busy || localBytes == null || reviewed == value) return;
    reviewed = value;
    ++_revision;
    _notify();
  }

  void removeLocal() {
    if (!canSelect) return;
    ++_serial;
    _clearLocal();
    error = null;
    _notify();
  }

  TaskImageDraftSelection? snapshot() =>
      canUse ? TaskImageDraftSelection._(this) : null;

  /// Keep local editing material across rejected-session recovery. This is a
  /// detached copy, never an upload approval; a different owner cannot restore
  /// it and the new authenticated session must review the visible material.
  TaskImageRetainedDraft? retainLocal({String? rejectedOwner}) {
    if (_disposed ||
        _retired ||
        busy ||
        localBytes == null ||
        _pixels == null) {
      return null;
    }
    // Shared identity listeners receive the rejection after credentials have
    // already been removed. Calling current first would erase the local draft.
    // Only the identity controller's rejected original owner permits a copy in
    // that signed-out transition; ordinary logout and account replacement do not.
    final rejectedOriginalOwner =
        rejectedOwner != null &&
        rejectedOwner == _owner &&
        _taskImageID.hasMatch(_owner ?? '') &&
        _session != null &&
        authorizationHeader() == null &&
        ownerID() == null &&
        _city != null &&
        _city.isNotEmpty &&
        _city == cityID();
    if (rejectedOwner != null) {
      if (!rejectedOriginalOwner) return null;
    } else if (!current) {
      return null;
    }
    final retained = TaskImageRetainedDraft._(
      _owner!,
      _city!,
      Uint8List.fromList(localBytes!),
      _pixels!.clone(),
      List.of(_masks),
    );
    reviewed = false;
    ++_revision;
    return retained;
  }

  bool restoreLocal(TaskImageRetainedDraft retained) {
    if (!canSelect ||
        localBytes != null ||
        retained._disposed ||
        retained.owner != _owner ||
        retained.city != _city)
      return false;
    _pixels = retained._pixels.clone();
    localBytes = Uint8List.fromList(retained._bytes);
    _masks.addAll(retained._masks);
    reviewed = false;
    ++_revision;
    _notify();
    return true;
  }

  @override
  void dispose() {
    _disposed = true;
    ++_serial;
    sourceChanges.removeListener(_observe);
    _clearLocal();
    super.dispose();
  }
}

class TaskImageRetainedDraft {
  TaskImageRetainedDraft._(
    this.owner,
    this.city,
    this._bytes,
    this._pixels,
    this._masks,
  );
  final String owner, city;
  final Uint8List _bytes;
  final ui.Image _pixels;
  final List<ui.Rect> _masks;
  bool _disposed = false;
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _pixels.dispose();
    _bytes.fillRange(0, _bytes.length, 0);
    _masks.clear();
  }
}

/// A revision-bound local material, never permission to upload or invoke a
/// visual model. Bytes are private and transferred only through the native port.
class TaskImageDraftSelection {
  TaskImageDraftSelection._(this._draft)
    : _revision = _draft._revision,
      _hash = sha256.convert(_draft.localBytes!).toString();
  final TaskImageDraftController _draft;
  final int _revision;
  final String _hash;
  bool _consumed = false;
  bool get isCurrent =>
      !_consumed &&
      _draft.canUse &&
      _draft._revision == _revision &&
      sha256.convert(_draft.localBytes!).toString() == _hash;
}

class TaskImageReceipt {
  TaskImageReceipt.fromJson(Map<String, dynamic> value)
    : id = value['id'] as String,
      operationID = value['operationId'] as String,
      taskID = value['taskId'] as String,
      ownerID = value['ownerAccountId'] as String,
      index = value['messageIndex'] as int,
      prefix = value['prefixDigest'] as String,
      taskUpdatedAt = _taskImageDate(value['expectedTaskUpdatedAt']),
      mime = value['mimeType'] as String,
      inputSize = value['inputSize'] as int,
      byteSize = value['byteSize'] as int,
      inputHash = value['inputSha256'] as String,
      hash = value['sha256'] as String,
      risk = value['pixelRisk'] as String,
      purpose = value['purpose'] as String,
      status = value['status'] as String,
      revision = value['revision'] as int,
      previewExpiresAt = _taskImageDate(value['previewExpiresAt']),
      retainUntil = _taskImageDate(value['retainUntil']) {
    if (value.length != 18 ||
        !_taskImageID.hasMatch(id) ||
        !_taskImageID.hasMatch(operationID) ||
        !_taskImageID.hasMatch(taskID) ||
        !_taskImageID.hasMatch(ownerID) ||
        index < 0 ||
        index > 255 ||
        !_taskImageSHA.hasMatch(prefix) ||
        !_taskImageSHA.hasMatch(inputHash) ||
        inputSize < 1 ||
        inputSize > momentImageMaxBytes ||
        !const ['image/png', 'image/jpeg'].contains(mime) ||
        !const ['UNKNOWN', 'USER_MASKED'].contains(risk) ||
        purpose != taskImagePurpose ||
        !const [
          'preview',
          'invalidated_preview',
          'ready_private',
          'deleted',
        ].contains(status) ||
        revision < 1 ||
        !retainUntil.isAfter(previewExpiresAt) ||
        (status == 'ready_private' &&
            (!_taskImageSHA.hasMatch(hash) ||
                byteSize < 1 ||
                byteSize > 16 * 1024 * 1024)) ||
        (status != 'ready_private' && (hash.isNotEmpty || byteSize != 0))) {
      throw const FormatException('会话图片回执无法核验');
    }
  }
  final String id,
      operationID,
      taskID,
      ownerID,
      prefix,
      mime,
      inputHash,
      hash,
      risk,
      purpose,
      status;
  final int index, inputSize, byteSize, revision;
  final DateTime taskUpdatedAt, previewExpiresAt, retainUntil;
  bool matches(AgentTask task, String owner) =>
      taskID == task.id &&
      ownerID == owner &&
      task.principalID == owner &&
      task.actingUserID == owner &&
      taskImagePrefix(task, index) == prefix;
}

// Images stay in a separate human-only port bound to the original native Task
// messages. Nothing here can submit a query, invoke a model, change map results
// or turn human image access into permission to export pixels.
class TaskImageController extends ChangeNotifier {
  TaskImageController({
    required this.task,
    required this.authorizationHeader,
    required this.ownerID,
    required this.sourceCurrent,
    required this.sourceChanges,
    required String apiBaseUrl,
    http.Client? client,
    MomentImagePicker? picker,
    DateTime Function()? clock,
    TaskImagePendingStore? pendingStore,
  }) : _base = apiBaseUrl.replaceFirst(RegExp(r'/$'), ''),
       _client = client ?? http.Client(),
       _ownsClient = client == null,
       _picker = picker ?? MomentImagePicker(),
       _clock = clock ?? DateTime.now,
       _pendingStore = pendingStore ?? const SecureTaskImagePendingStore() {
    _token = authorizationHeader();
    _owner = ownerID();
    sourceChanges.addListener(_observe);
  }
  final AgentTask task;
  final String? Function() authorizationHeader, ownerID;
  final bool Function() sourceCurrent;
  final Listenable sourceChanges;
  final String _base;
  final http.Client _client;
  final bool _ownsClient;
  final MomentImagePicker _picker;
  bool supportsSource(MomentImageSource source) =>
      _picker.supportsSource(source);
  bool canSelectSource(MomentImageSource source) =>
      canSelect && supportsSource(source);
  final DateTime Function() _clock;
  final TaskImagePendingStore _pendingStore;
  late final String? _token, _owner;
  bool _disposed = false,
      _retired = false,
      busy = false,
      reviewed = false,
      uncertain = false,
      connected = false;
  int _serial = 0;
  String? error;
  List<TaskImageReceipt> images = const [];
  TaskImageReceipt? preview;
  String? _operation;
  Map<String, Object>? _operationInput;
  TaskImagePendingOperation? _pending;
  List<TaskImagePendingOperation> pendingOperations = const [];
  ui.Image? _pixels;
  final _masks = <ui.Rect>[];
  Uint8List? localBytes;
  int? _draftMessageIndex;

  static bool eligible(AgentTask? task, String? owner) =>
      task != null &&
      owner != null &&
      _taskImageID.hasMatch(owner) &&
      _taskImageID.hasMatch(task.id) &&
      task.principalType.toLowerCase() == 'person' &&
      task.principalID == owner &&
      task.actingUserID == owner &&
      task.contextType == 'CITY' &&
      _taskImageID.hasMatch(task.contextID ?? '') &&
      (task.cityID?.isNotEmpty ?? false) &&
      task.updatedAt != null &&
      task.messages.any((m) => m.role == 'user');
  bool get current {
    if (!_disposed &&
        !_retired &&
        (!sourceCurrent() ||
            _token == null ||
            _owner == null ||
            _token != authorizationHeader() ||
            _owner != ownerID() ||
            !eligible(task, _owner)))
      _retired = true;
    return !_disposed && !_retired;
  }

  int get lastUserIndex =>
      task.messages.lastIndexWhere((m) => m.role == 'user');
  int get targetUserIndex => _draftMessageIndex ?? lastUserIndex;
  bool get canSelect =>
      current &&
      connected &&
      !busy &&
      !uncertain &&
      pendingOperations.isEmpty &&
      preview == null &&
      task.status == 'COMPLETED';
  bool get canPreview => canSelect && localBytes != null && reviewed;
  bool get canOpen =>
      current &&
      connected &&
      !busy &&
      (task.status == 'COMPLETED' ||
          uncertain ||
          preview != null ||
          images.isNotEmpty ||
          pendingOperations.isNotEmpty);
  DateTime get nowUtc => _clock().toUtc();
  bool isReadable(TaskImageReceipt r) =>
      current &&
      r.matches(task, _owner!) &&
      r.status == 'ready_private' &&
      r.retainUntil.isAfter(nowUtc) &&
      images.any(
        (v) => v.id == r.id && v.revision == r.revision && v.hash == r.hash,
      );
  double get localAspectRatio =>
      _pixels == null ? 1 : _pixels!.width / _pixels!.height;
  bool get canSave =>
      current &&
      !busy &&
      !uncertain &&
      reviewed &&
      localBytes != null &&
      preview?.status == 'preview' &&
      preview!.previewExpiresAt.isAfter(_clock().toUtc()) &&
      preview!.taskUpdatedAt == task.updatedAt?.toUtc() &&
      preview!.inputHash == sha256.convert(localBytes!).toString();
  bool _current(int serial) => current && serial == _serial;
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _observe() {
    if (!current) {
      ++_serial;
      busy = false;
      reviewed = false;
      connected = false;
      localBytes = null;
      _pixels?.dispose();
      _pixels = null;
      _masks.clear();
      images = const [];
      pendingOperations = const [];
      error = '会话或账户已变化，请返回后重新打开。';
      _notify();
    }
  }

  Uri _url(String suffix) =>
      Uri.parse('$_base/v1/me/agent-tasks/${task.id}/$suffix');
  Map<String, String> get _headers => {'Authorization': _token!};
  Future<http.Response> _jsonRequest(
    int serial,
    String method,
    String suffix, {
    Map<String, String>? headers,
    Object? body,
  }) async {
    if (!_current(serial)) throw const FormatException('会话已变化');
    final request = http.Request(method, _url(suffix))
      ..headers.addAll({..._headers, ...?headers});
    if (body is Uint8List) {
      request.bodyBytes = body;
    } else if (body is String) {
      request.body = body;
    }
    final response = await _client
        .send(request)
        .timeout(const Duration(seconds: 15));
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (!_current(serial) || chunk.length > 65536 - bytes.length)
        throw const FormatException('回执无法读取');
      bytes.add(chunk);
    }
    if (_current(serial) &&
        (response.statusCode == 401 || response.statusCode == 403)) {
      _retired = true;
      _observe();
    }
    return http.Response.bytes(
      bytes.takeBytes(),
      response.statusCode,
      headers: response.headers,
    );
  }

  TaskImageReceipt _receipt(http.Response response, {required int status}) {
    if (response.statusCode != status) throw const FormatException('请求未完成');
    final value =
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    final r = TaskImageReceipt.fromJson(value);
    if (!r.matches(task, _owner!)) throw const FormatException('会话图片来源已变化');
    return r;
  }

  Future<void> load() async {
    if (!current || busy || _base.isEmpty) return;
    final serial = ++_serial;
    busy = true;
    error = null;
    _notify();
    try {
      if (!_current(serial)) return;
      final pending = await _pendingStore.read(_base, _owner!, task.id);
      if (!_current(serial)) return;
      if (pending.any(
        (v) =>
            v.ownerID != _owner ||
            v.taskID != task.id ||
            v.contextID != task.contextID ||
            taskImagePrefix(task, v.input['messageIndex'] as int) !=
                v.input['prefixDigest'],
      )) {
        throw const FormatException('原图片消息已变化，请核实已有操作');
      }
      pendingOperations = List.unmodifiable(pending);
      final response = await _jsonRequest(serial, 'GET', 'images');
      if (!_current(serial)) return;
      if (response.statusCode != 200) throw const FormatException('会话图片加载失败');
      final raw =
          (jsonDecode(response.body) as Map<String, dynamic>)['data'] as List;
      if (raw.length > 12) throw const FormatException('会话图片数量无法核验');
      final rs = raw
          .map((v) => TaskImageReceipt.fromJson(v as Map<String, dynamic>))
          .toList();
      if (rs.map((v) => v.id).toSet().length != rs.length ||
          rs.any(
            (v) =>
                !v.matches(task, _owner) ||
                v.status != 'ready_private' ||
                !v.retainUntil.isAfter(_clock().toUtc()),
          ))
        throw const FormatException('会话图片来源无法核验');
      images = List.unmodifiable(rs);
      connected = true;
    } catch (_) {
      if (_current(serial)) {
        connected = false;
        error = '会话图片暂不可用，请重试读取。';
      }
    } finally {
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> select({
    MomentImageSource source = MomentImageSource.gallery,
  }) async {
    if (!canSelect) return;
    final serial = ++_serial;
    busy = true;
    error = null;
    reviewed = false;
    _notify();
    ui.Image? decoded;
    try {
      if (!_current(serial)) return;
      if (await _picker.discardLostSelection()) {
        if (_current(serial)) error = '上次图片选择已失效，请在当前会话重新选择。';
      }
      if (!_current(serial)) return;
      final selected = await _picker.selectSource(source);
      if (!_current(serial) || selected == null) return;
      decoded = await _decodeTaskImage(selected);
      if (!_current(serial)) return;
      _pixels?.dispose();
      _pixels = decoded;
      decoded = null;
      _masks.clear();
      await _render(serial);
    } catch (e) {
      if (_current(serial))
        error = e is FormatException ? e.message : '图片未能处理，请重新选择。';
    } finally {
      decoded?.dispose();
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _render(int serial) async {
    final pixels = _pixels;
    if (pixels == null) return;
    final bytes = await _renderTaskImage(pixels, _masks);
    if (_current(serial)) localBytes = bytes;
  }

  Future<void> mask(ui.Rect rectangle) async {
    if (!canSelect ||
        _pixels == null ||
        ![
          rectangle.left,
          rectangle.top,
          rectangle.right,
          rectangle.bottom,
        ].every((v) => v.isFinite))
      return;
    final rect = rectangle.intersect(const ui.Rect.fromLTRB(0, 0, 1, 1));
    if (rect.isEmpty) return;
    final serial = ++_serial;
    busy = true;
    reviewed = false;
    _masks.add(rect);
    _notify();
    try {
      await _render(serial);
    } catch (_) {
      if (_current(serial)) {
        // Failed masking must not leave old pixels under USER_MASKED.
        localBytes = null;
        reviewed = false;
        _pixels?.dispose();
        _pixels = null;
        _masks.clear();
        _draftMessageIndex = null;
        error = '遮盖未完成，请重新选择图片。';
      }
    } finally {
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  void setReviewed(bool value) {
    if (!current || busy) return;
    reviewed = value;
    _notify();
  }

  void removeLocal() {
    if (!current || busy || uncertain || preview != null) return;
    localBytes = null;
    reviewed = false;
    _pixels?.dispose();
    _pixels = null;
    _masks.clear();
    _draftMessageIndex = null;
    _notify();
  }

  /// Stages a checked local material against the exact newly submitted human
  /// message. No preview/upload occurs until the existing page confirms it.
  bool stageReviewedDraft(
    TaskImageDraftSelection selection, {
    required int messageIndex,
    required String submittedText,
  }) {
    if (!canSelect ||
        localBytes != null ||
        !selection.isCurrent ||
        messageIndex < 0 ||
        messageIndex >= task.messages.length ||
        messageIndex > 255 ||
        task.messages[messageIndex].role != 'user' ||
        task.messages[messageIndex].text != submittedText ||
        submittedText.trim().isEmpty ||
        taskImagePrefix(task, messageIndex) == null)
      return false;
    final draft = selection._draft;
    if (draft._owner != _owner ||
        draft._city != task.cityID ||
        draft._session != _taskImageSessionDigest(_token))
      return false;
    final bytes = Uint8List.fromList(draft.localBytes!);
    if (MomentImageHeader.inspect(bytes).mime != 'image/png' ||
        sha256.convert(bytes).toString() != selection._hash)
      return false;
    _pixels?.dispose();
    _pixels = draft._pixels!.clone();
    _masks
      ..clear()
      ..addAll(draft._masks);
    localBytes = bytes;
    _draftMessageIndex = messageIndex;
    reviewed = false;
    error = null;
    selection._consumed = true;
    draft.removeLocal();
    _notify();
    return true;
  }

  /// The current send click approves only saving this exact checked material
  /// into the newly confirmed human turn. It grants no model/publishing rights.
  Future<void> sendReviewedDraft(
    TaskImageDraftSelection selection, {
    required int messageIndex,
    required String submittedText,
  }) async {
    if (!stageReviewedDraft(
      selection,
      messageIndex: messageIndex,
      submittedText: submittedText,
    ))
      return;
    reviewed = true;
    await prepare();
    if (preview == null || uncertain || !current) return;
    reviewed = true;
    await save();
  }

  String _newOperation() {
    final random = Random.secure(),
        bytes = List<int>.generate(16, (_) => random.nextInt(256));
    bytes[6] = (bytes[6] & 15) | 64;
    bytes[8] = (bytes[8] & 63) | 128;
    final h = bytes.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-${h.substring(16, 20)}-${h.substring(20)}';
  }

  bool _sameOperation(TaskImageReceipt r) =>
      r.operationID == _operation &&
      r.index == _operationInput?['messageIndex'] &&
      r.prefix == _operationInput?['prefixDigest'] &&
      r.inputHash == _operationInput?['sha256'] &&
      r.inputSize == _operationInput?['byteSize'] &&
      r.mime == _operationInput?['mimeType'] &&
      r.risk == _operationInput?['pixelRisk'] &&
      r.taskUpdatedAt ==
          _taskImageDate(_operationInput?['expectedTaskUpdatedAt']);

  void choosePending(TaskImagePendingOperation pending) {
    if (!current ||
        busy ||
        uncertain ||
        preview != null ||
        localBytes != null ||
        !pendingOperations.contains(pending))
      return;
    _pending = pending;
    _operation = pending.operationID;
    _operationInput = Map<String, Object>.from(pending.input);
    uncertain = true;
    reviewed = false;
    error = '仅核实原操作，不会自动上传或重新提交。';
    _notify();
  }

  Future<bool> _journal(int serial) async {
    if (!_current(serial) || _operationInput == null) return false;
    final pending = _pending ??= TaskImagePendingOperation(
      ownerID: _owner!,
      taskID: task.id,
      contextID: task.contextID!,
      input: Map<String, dynamic>.from(_operationInput!),
    );
    await _pendingStore.write(_base, pending);
    if (!_current(serial)) return false;
    pendingOperations = List.unmodifiable([
      ...pendingOperations.where((v) => v.operationID != pending.operationID),
      pending,
    ]);
    return true;
  }

  Future<bool> _clearJournal(int serial) async {
    final pending = _pending;
    if (!_current(serial)) return false;
    if (pending != null) {
      await _pendingStore.delete(_base, pending);
      if (!_current(serial)) return false;
      pendingOperations = List.unmodifiable(
        pendingOperations.where((v) => v.operationID != pending.operationID),
      );
    }
    return true;
  }

  Future<void> prepare() async {
    if (!canPreview || _operation != null) return;
    final serial = ++_serial;
    _operation = _newOperation();
    _operationInput = {
      'operationId': _operation!,
      'messageIndex': targetUserIndex,
      'prefixDigest': taskImagePrefix(task, targetUserIndex)!,
      'expectedTaskUpdatedAt': task.updatedAt!.toUtc().toIso8601String(),
      'mimeType': 'image/png',
      'byteSize': localBytes!.length,
      'sha256': sha256.convert(localBytes!).toString(),
      'pixelRisk': _masks.isEmpty ? 'UNKNOWN' : 'USER_MASKED',
      'purpose': taskImagePurpose,
    };
    busy = true;
    uncertain = true;
    error = null;
    _notify();
    try {
      if (!_current(serial)) return;
      if (!await _journal(serial)) return;
      final response = await _jsonRequest(
        serial,
        'POST',
        'image-previews',
        headers: {'Content-Type': 'application/json'},
        body: jsonEncode(_operationInput),
      );
      if (!_current(serial)) return;
      final r = _receipt(response, status: 201);
      if (!_sameOperation(r) || r.status != 'preview')
        throw const FormatException('图片预览无法核验');
      preview = r;
      uncertain = false;
      reviewed = false;
    } catch (_) {
      if (_current(serial)) error = '预览结果尚未确认。保留了本机图片，请读取操作状态；不会自动重发。';
    } finally {
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> save() async {
    if (!canSave) return;
    final serial = ++_serial, r = preview!, bytes = localBytes!;
    busy = true;
    uncertain = true;
    error = null;
    _notify();
    try {
      if (!_current(serial)) return;
      if (!await _journal(serial)) return;
      final response = await _jsonRequest(
        serial,
        'PUT',
        'image-previews/${r.id}/content',
        headers: {
          'Content-Type': 'image/png',
          'X-Birdtie-Task-Image-Confirmation': r.id,
        },
        body: bytes,
      );
      if (!_current(serial)) return;
      final saved = _receipt(response, status: 200);
      if (!_sameOperation(saved) ||
          saved.id != r.id ||
          saved.status != 'ready_private')
        throw const FormatException('图片保存无法核验');
      await _acceptOperation(serial, saved);
    } catch (_) {
      if (_current(serial)) error = '保存结果尚未确认。图片仍保留在本机；请读取操作状态，不要重复发送。';
    } finally {
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _acceptOperation(int serial, TaskImageReceipt r) async {
    if (!await _clearJournal(serial)) return;
    uncertain = false;
    reviewed = false;
    images = List.unmodifiable([
      ...images.where((v) => v.id != r.id),
      if (r.status == 'ready_private') r,
    ]);
    _clearOperation();
  }

  void _clearOperation() {
    uncertain = false;
    reviewed = false;
    preview = null;
    _operation = null;
    _operationInput = null;
    _pending = null;
    localBytes = null;
    _pixels?.dispose();
    _pixels = null;
    _masks.clear();
    _draftMessageIndex = null;
  }

  Future<void> readOperation() async {
    if (!current || busy || _operation == null) return;
    final serial = ++_serial;
    busy = true;
    error = null;
    _notify();
    try {
      if (!_current(serial)) return;
      final response = await _jsonRequest(
        serial,
        'GET',
        'image-operations/$_operation',
      );
      if (!_current(serial)) return;
      final r = _receipt(response, status: 200);
      if (!_sameOperation(r)) throw const FormatException('操作来源无法核验');
      if (r.status == 'ready_private' || r.status == 'deleted') {
        await _acceptOperation(serial, r);
      } else {
        preview = r;
        uncertain = false;
        reviewed = false;
        if (r.status == 'invalidated_preview') {
          error = '待保存图片已失效，可以移除后重新选择；未重新上传图片。';
        } else if (localBytes == null) {
          error = '本机预览已关闭。可移除这条待保存记录后重新选择图片；不会续传。';
        }
      }
    } catch (_) {
      if (_current(serial)) {
        uncertain = true;
        error = '操作状态仍未知，请稍后重试读取；未重发上传。';
      }
    } finally {
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<Uint8List?> readBytes(TaskImageReceipt r) async {
    if (!isReadable(r) || busy) return null;
    final serial = ++_serial;
    busy = true;
    error = null;
    _notify();
    try {
      if (!_current(serial)) return null;
      final request = http.Request('GET', _url('images/${r.id}/content'))
        ..headers.addAll(_headers);
      final response = await _client
          .send(request)
          .timeout(const Duration(seconds: 12));
      if (_current(serial) &&
          (response.statusCode == 401 || response.statusCode == 403)) {
        _retired = true;
        _observe();
      }
      if (!_current(serial) ||
          response.statusCode != 200 ||
          response.headers['content-type'] != r.mime)
        throw const FormatException('图片读取失败');
      final data = BytesBuilder(copy: false);
      await for (final chunk in response.stream.timeout(
        const Duration(seconds: 12),
      )) {
        if (!_current(serial) || chunk.length > 16 * 1024 * 1024 - data.length)
          throw const FormatException('图片读取已失效');
        data.add(chunk);
      }
      final bytes = data.takeBytes();
      if (!_current(serial) ||
          !isReadable(r) ||
          bytes.length != r.byteSize ||
          sha256.convert(bytes).toString() != r.hash)
        throw const FormatException('图片完整性无法核验');
      if (MomentImageHeader.inspect(bytes, storedDerivative: true).mime !=
          r.mime) {
        throw const FormatException('图片格式无法核验');
      }
      return bytes;
    } catch (_) {
      if (_current(serial)) error = '图片暂不可用，请重试读取。';
      return null;
    } finally {
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> remove(TaskImageReceipt r) async {
    if (!current || busy || !r.matches(task, _owner!)) return;
    final serial = ++_serial;
    busy = true;
    error = null;
    _notify();
    try {
      if (!_current(serial)) return;
      final response = await _jsonRequest(
        serial,
        'DELETE',
        'images/${r.id}?revision=${r.revision}',
      );
      if (!_current(serial)) return;
      if (response.statusCode != 204) throw const FormatException('移除未确认');
      images = List.unmodifiable(images.where((v) => v.id != r.id));
      if (_operation == r.operationID && await _clearJournal(serial))
        _clearOperation();
    } catch (_) {
      if (_current(serial)) {
        error = '移除尚未确认，请重新读取图片状态。';
        if (_operation == r.operationID) uncertain = true;
      }
    } finally {
      if (_current(serial)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _disposed = true;
    ++_serial;
    sourceChanges.removeListener(_observe);
    localBytes = null;
    _pixels?.dispose();
    _pixels = null;
    images = const [];
    pendingOperations = const [];
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
