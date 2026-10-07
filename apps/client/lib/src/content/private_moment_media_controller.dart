import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'moment_image_text_recognizer.dart';
import 'moment_image_text_rules.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'moment_image_header.dart';
import 'moment_image_picker.dart';
import 'private_moment_media_pending_store.dart';

const privateMomentImagePurpose = 'PRIVATE_MOMENT_ATTACHMENT';
final _imageID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
final _imageHash = RegExp(r'^[0-9a-f]{64}$');

class PrivateMomentImageReceipt {
  PrivateMomentImageReceipt.fromJson(Map<String, dynamic> j)
    : id = j['id'] as String,
      operationID = j['operationId'] as String,
      momentID = j['momentId'] as String,
      ownerID = j['ownerAccountId'] as String,
      momentRevision = j['momentRevision'] as int,
      mime = j['mimeType'] as String,
      byteSize = j['byteSize'] as int,
      inputHash = j['inputSha256'] as String,
      hash = j['sha256'] as String,
      pixelRisk = j['pixelRisk'] as String,
      purpose = j['purpose'] as String,
      status = j['status'] as String,
      revision = j['revision'] as int,
      previewExpiresAt = _date(j['previewExpiresAt']),
      retainUntil = _date(j['retainUntil']) {
    if (!_imageID.hasMatch(id) ||
        !_imageID.hasMatch(operationID) ||
        !_imageID.hasMatch(momentID) ||
        !_imageID.hasMatch(ownerID) ||
        momentRevision < 1 ||
        revision < 1 ||
        byteSize < 1 ||
        byteSize > momentImageMaxBytes ||
        !_imageHash.hasMatch(inputHash) ||
        !['image/png', 'image/jpeg'].contains(mime) ||
        !['UNKNOWN', 'USER_MASKED'].contains(pixelRisk) ||
        purpose != privateMomentImagePurpose ||
        !['preview', 'ready_private', 'deleted'].contains(status) ||
        (status == 'ready_private' && !_imageHash.hasMatch(hash)) ||
        !retainUntil.isAfter(previewExpiresAt)) {
      throw const FormatException('图片回执无效');
    }
  }
  static DateTime _date(dynamic value) {
    if (value is! String || !RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(value)) {
      throw const FormatException('图片期限无效');
    }
    return DateTime.parse(value).toUtc();
  }

  final String id,
      operationID,
      momentID,
      ownerID,
      mime,
      inputHash,
      hash,
      pixelRisk,
      purpose,
      status;
  final int momentRevision, byteSize, revision;
  final DateTime previewExpiresAt, retainUntil;
}

class PrivateMomentMediaController extends ChangeNotifier {
  PrivateMomentMediaController({
    required this.momentID,
    required this.momentRevision,
    required this.authorizationHeader,
    required this.ownerID,
    required this.identityChanges,
    required this.sourceCurrent,
    required String apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    http.Client? client,
    MomentImagePicker? picker,
    DateTime Function()? clock,
    PrivateImagePendingStore? pendingStore,
    MomentImageTextRecognizer? textRecognizer,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl.replaceFirst(RegExp(r'/$'), ''),
       _picker = picker ?? MomentImagePicker(),
       _pendingStore = pendingStore ?? const SecurePrivateImagePendingStore(),
       _textRecognizer = textRecognizer ?? MomentImageTextRecognizer(),
       _clock = clock ?? DateTime.now {
    _token = authorizationHeader();
    _owner = ownerID();
    identityChanges.addListener(_observe);
    workspaceChanges?.addListener(_observe);
    _observe();
  }
  final String momentID;
  final int momentRevision;
  final String? Function() authorizationHeader, ownerID;
  final String? Function()? organizationWorkspaceID;
  final bool Function() sourceCurrent;
  final Listenable identityChanges;
  final Listenable? workspaceChanges;
  final http.Client _client;
  final bool _ownsClient;
  final String _base;
  final MomentImagePicker _picker;
  final DateTime Function() _clock;
  final PrivateImagePendingStore _pendingStore;
  final MomentImageTextRecognizer _textRecognizer;
  int _textSerial = 0;
  bool textChecking = false;
  String? textMessage;
  List<MomentImageTextHint> textHints = const [];
  final Set<int> _selectedTextHints = {};
  bool textHintSelected(int index) => _selectedTextHints.contains(index);
  bool get hasSelectedTextHints => _selectedTextHints.isNotEmpty;
  List<int> get selectedTextHintIndices =>
      List.unmodifiable(_selectedTextHints.toList()..sort());
  void _invalidateTextCheck() {
    _textSerial++;
    textChecking = false;
    textMessage = null;
    textHints = const [];
    _selectedTextHints.clear();
    unawaited(_textRecognizer.cancel());
  }

  bool _textCurrent(int serial, int check, Uint8List bytes, String hash) =>
      _current(serial) &&
      check == _textSerial &&
      identical(localBytes, bytes) &&
      sha256.convert(bytes).toString() == hash;

  Future<void> checkLocalText() async {
    _observe();
    if (!canChangeLocal || textChecking || !hasLocalImage) return;
    final bytes = localBytes!, hash = sha256.convert(localBytes!).toString();
    final serial = _serial, check = ++_textSerial;
    textHints = const [];
    _selectedTextHints.clear();
    textChecking = true;
    textMessage = null;
    localReviewed = false;
    _notify();
    _observe();
    if (!_textCurrent(serial, check, bytes, hash)) return;
    try {
      final result = await _textRecognizer.recognize(bytes);
      _observe();
      if (!_textCurrent(serial, check, bytes, hash)) return;
      textHints = momentImageTextHints(result.lines);
      textMessage = textHints.isEmpty
          ? '未找到可提示的邮箱或电话；这不表示图片安全，仍请检查其他文字和敏感内容。'
          : '仅提示可能的邮箱或电话所在行，请检查框选范围后选择要遮挡的区域；其他文字、人脸和敏感内容仍未检测。';
      if (result.limited || textHints.length == 24) {
        textMessage = '只检查了部分文字，可能遗漏。$textMessage';
      }
    } catch (e) {
      if (_textCurrent(serial, check, bytes, hash)) {
        textMessage = e is MomentImageTextFailure
            ? e.message
            : '本机文字结果未能核实；请重试或手动遮挡。';
      }
    } finally {
      _observe();
      if (_textCurrent(serial, check, bytes, hash)) {
        textChecking = false;
        _notify();
      }
    }
  }

  void selectTextHint(int index, bool selected) {
    _observe();
    if (!canChangeLocal ||
        textChecking ||
        index < 0 ||
        index >= textHints.length) {
      return;
    }
    if (selected) {
      _selectedTextHints.add(index);
    } else {
      _selectedTextHints.remove(index);
    }
    _notify();
  }

  Future<void> maskSelectedTextHints({required bool confirmed}) async {
    _observe();
    if (!confirmed || !canChangeLocal || textChecking || !hasSelectedTextHints) {
      return;
    }
    // Capture the actual regions before mask() invalidates all stale OCR state.
    final regions = _selectedTextHints.toList()..sort();
    final masks = regions.map((i) => textHints[i].region).toList();
    final image = _pixels;
    var expected = _serial;
    for (final rect in masks) {
      _observe();
      if (!_current(expected) || !canChangeLocal || !identical(_pixels, image)) {
        return;
      }
      await mask(rect);
      expected++;
    }
  }

  void cancelLocalText() {
    _invalidateTextCheck();
    textMessage = '文字检查已停止，未上传图片；仍请检查敏感内容。';
    _notify();
  }

  bool _journalInitialized = false, _journalFailed = false;
  List<PendingPrivateImageOperation> pendingOperations = const [];
  PendingPrivateImageOperation? _pending;
  bool get recoveryBlocked => _journalFailed;
  late final String? _token, _owner;
  bool _disposed = false,
      retired = false,
      busy = false,
      loaded = false,
      localReviewed = false,
      requiresReopen = false;
  String? error, message;
  Uint8List? localBytes, displayedBytes;
  ui.Image? _pixels;
  final _masks = <ui.Rect>[];
  String get pixelRisk => _masks.isEmpty ? 'UNKNOWN' : 'USER_MASKED';
  List<PrivateMomentImageReceipt> images = const [];
  PrivateMomentImageReceipt? preview;
  String? _operation;
  String? _unknown; // preview, save, delete. Unknown writes are GET-only.
  PrivateMomentImageReceipt? _deleting;
  int _serial = 0;
  bool get unknown => _unknown != null || pendingOperations.isNotEmpty;
  bool get hasLocalImage => localBytes != null;
  bool get canRead => !retired && !requiresReopen && !busy;
  bool get canChangeLocal => canRead && !unknown && !_journalFailed;
  bool get canPreview =>
      canChangeLocal &&
      !textChecking &&
      hasLocalImage &&
      localReviewed &&
      preview == null;
  bool get canSave =>
      !retired &&
      !requiresReopen &&
      !busy &&
      !textChecking &&
      !unknown &&
      !_journalFailed &&
      preview?.status == 'preview' &&
      preview!.previewExpiresAt.isAfter(_clock().toUtc()) &&
      localReviewed &&
      hasLocalImage;
  bool get _binding =>
      _token != null &&
      _owner != null &&
      _imageID.hasMatch(_owner) &&
      authorizationHeader() == _token &&
      ownerID() == _owner &&
      (organizationWorkspaceID?.call() == null ||
          organizationWorkspaceID?.call() == '') &&
      sourceCurrent();
  bool _current(int serial) =>
      !_disposed && !retired && serial == _serial && _binding;
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _observe() {
    if (_disposed || retired || _binding) return;
    retired = true;
    _invalidateTextCheck();
    _serial++;
    busy = false;
    localBytes = null;
    displayedBytes = null;
    images = const [];
    preview = null;
    _operation = null;
    _unknown = null;
    pendingOperations = const [];
    _pending = null;
    _pixels?.dispose();
    _pixels = null;
    _masks.clear();
    error = '身份或记录已变化，请关闭并从当前私人记录重新打开。';
    _notify();
  }

  void retire() {
    if (_disposed || retired) return;
    retired = true;
    _invalidateTextCheck();
    _serial++;
    busy = false;
    localBytes = null;
    displayedBytes = null;
    images = const [];
    preview = null;
    _operation = null;
    _unknown = null;
    pendingOperations = const [];
    _pending = null;
    _pixels?.dispose();
    _pixels = null;
    _masks.clear();
    error = '当前页面已失效，请重新打开。';
    _notify();
  }

  int? _begin() {
    _observe();
    if (retired || requiresReopen || busy || _disposed) return null;
    _invalidateTextCheck();
    final u = Uri.tryParse(_base);
    if (u == null || !u.hasAuthority || !['http', 'https'].contains(u.scheme)) {
      error = '图片服务尚未连接，请稍后重试。';
      _notify();
      return null;
    }
    busy = true;
    error = null;
    message = null;
    final n = ++_serial;
    _notify();
    _observe();
    return _current(n) ? n : null;
  }

  void _end(int n) {
    if (_current(n)) {
      busy = false;
      _notify();
    }
  }

  Map<String, String> get _headers => {'Authorization': _token!};

  Future<bool> _restoreJournal(int n) async {
    if (_journalInitialized && !_journalFailed) return _current(n);
    try {
      final rows = await _pendingStore.read(_base, _owner!, momentID);
      _observe();
      if (!_current(n)) return false;
      if (rows.length > 16 ||
          rows.any((v) => v.ownerID != _owner || v.momentID != momentID)) {
        throw const FormatException('图片恢复归属不符');
      }
      pendingOperations = List.unmodifiable(rows);
      _pending = rows.firstOrNull;
      _journalInitialized = true;
      _journalFailed = false;
      if (_pending != null) {
        _unknown = _pending!.phase;
        _operation = _pending!.operationID;
        message = '发现待核实的图片操作。请核实原操作结果；不会恢复图片或旧确认，也不会自动提交。';
      }
      return true;
    } catch (_) {
      _observe();
      if (_current(n)) {
        _journalFailed = true;
        error = '未能读取本机恢复记录，暂不能添加或移除图片。请重试读取，不要重复提交。';
      }
      return false;
    }
  }

  void choosePending(PendingPrivateImageOperation v) {
    _observe();
    if (retired || busy || !pendingOperations.any((x) => identical(x, v))) {
      return;
    }
    _pending = v;
    _unknown = v.phase;
    _operation = v.operationID;
    _notify();
  }

  bool isSelectedPending(PendingPrivateImageOperation v) =>
      identical(_pending, v);

  PendingPrivateImageOperation _reference(
    String phase, {
    PrivateMomentImageReceipt? asset,
  }) {
    final at = _clock().toUtc();
    return PendingPrivateImageOperation(
      ownerID: _owner!,
      momentID: momentID,
      operationID: asset?.operationID ?? _operation!,
      phase: phase,
      momentRevision: asset?.momentRevision ?? momentRevision,
      mime: asset?.mime ?? 'image/png',
      byteSize: asset?.byteSize ?? localBytes!.length,
      inputHash: asset?.inputHash ?? sha256.convert(localBytes!).toString(),
      pixelRisk: asset?.pixelRisk ?? pixelRisk,
      assetID: asset?.id,
      assetRevision: asset?.revision,
      derivativeHash: phase == 'delete' ? asset!.hash : null,
      observedAt: at,
      deadlineAt: phase == 'preview'
          ? at.add(const Duration(minutes: 5))
          : phase == 'save'
          ? asset!.previewExpiresAt
          : asset!.retainUntil,
    );
  }

  Future<bool> _journalBeforeWire(int n, PendingPrivateImageOperation v) async {
    try {
      await _pendingStore.write(_base, v);
      _observe();
      if (!_current(n)) return false;
      _pending = v;
      pendingOperations = List.unmodifiable([...pendingOperations, v]);
      return true;
    } catch (_) {
      _observe();
      if (_current(n)) {
        _journalFailed = true;
        error = '未能保存本机恢复记录，本次请求未发出。请重试读取恢复记录。';
      }
      return false;
    }
  }

  Future<bool> _clearJournal(int n, PendingPrivateImageOperation v) async {
    if (!_current(n)) return false;
    try {
      await _pendingStore.delete(_base, v);
      _observe();
      if (!_current(n)) return false;
      pendingOperations = List.unmodifiable(
        pendingOperations.where((x) => x.key != v.key),
      );
      _pending = pendingOperations.firstOrNull;
      _unknown = _pending?.phase;
      _operation = _pending?.operationID ?? _operation;
      _journalFailed = false;
      return true;
    } catch (_) {
      _observe();
      if (_current(n)) {
        _journalFailed = true;
        _unknown = v.phase;
        error = '已收到操作结果，但本机恢复记录尚未清理。请继续核实，暂不重复提交。';
      }
      return false;
    }
  }

  bool _matchesReference(
    PrivateMomentImageReceipt r,
    PendingPrivateImageOperation p,
  ) =>
      r.ownerID == p.ownerID &&
      r.momentID == p.momentID &&
      r.operationID == p.operationID &&
      r.momentRevision == p.momentRevision &&
      r.mime == p.mime &&
      r.byteSize == p.byteSize &&
      r.inputHash == p.inputHash &&
      r.pixelRisk == p.pixelRisk &&
      (p.assetID == null || r.id == p.assetID);
  Uri _uri(String path) => Uri.parse('$_base/v1/me/moments/$momentID/$path');
  String _newOperation() {
    final r = Random.secure();
    final b = List<int>.generate(16, (_) => r.nextInt(256));
    b[6] = (b[6] & 15) | 64;
    b[8] = (b[8] & 63) | 128;
    final s = b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();
    return '${s.substring(0, 8)}-${s.substring(8, 12)}-${s.substring(12, 16)}-${s.substring(16, 20)}-${s.substring(20)}';
  }

  String _failure(int code) {
    if ([401, 403, 409].contains(code)) {
      requiresReopen = true;
      localReviewed = false;
    }
    return switch (code) {
      401 => '登录已失效，请关闭页面，重新登录后打开当前私人记录。',
      403 => '当前身份没有权限，请关闭页面并返回本人的私人记录。',
      409 => '记录、图片版本或确认期限已变化，请关闭并重新打开当前私人记录。',
      404 => '未找到当前图片。',
      413 => '图片超过处理大小限制。',
      400 => '图片内容或格式无效。',
      _ => '图片服务暂不可用，请稍后核实。',
    };
  }

  PrivateMomentImageReceipt _receipt(http.Response r) {
    final j = jsonDecode(utf8.decode(r.bodyBytes)) as Map<String, dynamic>;
    final v = PrivateMomentImageReceipt.fromJson(
      j['data'] as Map<String, dynamic>,
    );
    if (v.ownerID != _owner || v.momentID != momentID) {
      throw const FormatException('图片不属于当前记录');
    }
    return v;
  }

  bool _exact(PrivateMomentImageReceipt r) =>
      r.operationID == _operation &&
      r.momentRevision == momentRevision &&
      r.inputHash == sha256.convert(localBytes!).toString() &&
      r.byteSize == localBytes!.length &&
      r.mime == 'image/png' &&
      r.pixelRisk == pixelRisk;

  Future<void> checkLostSelection() async {
    try {
      final lost = await _picker.discardLostSelection();
      if (!_disposed && !retired && _binding && message == null && lost) {
        message = '上次图片选择已中断。为保护当前身份，请重新选择图片。';
        _notify();
      }
    } catch (_) {
      if (!_disposed && !retired && _binding && message == null) {
        message = '无法恢复上次选择，可手动重新选择图片。';
        _notify();
      }
    }
  }

  Future<void> select() async {
    if (!canChangeLocal) return;
    _observe();
    if (retired) return;
    _invalidateTextCheck();
    busy = true;
    error = null;
    message = null;
    final n = ++_serial;
    _notify();
    _observe();
    if (!_current(n)) return;
    ui.Image? decoded;
    try {
      if (!await _restoreJournal(n) || unknown) return;
      final chosen = await _picker.select();
      if (!_current(n) || chosen == null) return;
      final codec = await ui.instantiateImageCodec(chosen.bytes);
      try {
        decoded = (await codec.getNextFrame()).image;
      } finally {
        codec.dispose();
      }
      if (!_current(n)) {
        decoded.dispose();
        decoded = null;
        return;
      }
      if (decoded.width > momentImageMaxDimension ||
          decoded.height > momentImageMaxDimension ||
          decoded.width * decoded.height > momentImageMaxPixels) {
        throw const FormatException('图片尺寸超过处理范围');
      }
      _pixels?.dispose();
      _pixels = decoded;
      decoded = null;
      _masks.clear();
      preview = null;
      _operation = null;
      localReviewed = false;
      displayedBytes = null;
      await _render(n);
    } catch (e) {
      decoded?.dispose();
      if (_current(n)) {
        error = e is FormatException ? e.message : '图片未能读取或处理，请重新选择。';
      }
    } finally {
      _end(n);
    }
  }

  Future<void> _render(int n) async {
    final image = _pixels;
    if (image == null) return;
    final rec = ui.PictureRecorder();
    final canvas = ui.Canvas(rec);
    canvas.drawImage(image, ui.Offset.zero, ui.Paint());
    for (final r in _masks) {
      canvas.drawRect(
        ui.Rect.fromLTRB(
          r.left * image.width,
          r.top * image.height,
          r.right * image.width,
          r.bottom * image.height,
        ),
        ui.Paint()..color = const ui.Color(0xff000000),
      );
    }
    final picture = rec.endRecording();
    final out = await picture.toImage(image.width, image.height);
    picture.dispose();
    try {
      final bytes = await out.toByteData(format: ui.ImageByteFormat.png);
      if (!_current(n)) return;
      if (bytes == null || bytes.lengthInBytes > momentImageMaxBytes) {
        throw const FormatException('处理后图片超过 10 MiB');
      }
      localBytes = Uint8List.fromList(
        bytes.buffer.asUint8List(bytes.offsetInBytes, bytes.lengthInBytes),
      );
      MomentImageHeader.inspect(localBytes!);
    } finally {
      out.dispose();
    }
  }

  Future<void> mask(ui.Rect rect) async {
    if (!canChangeLocal || _pixels == null) return;
    final r = rect.intersect(const ui.Rect.fromLTRB(0, 0, 1, 1));
    if (r.width <= 0 || r.height <= 0) return;
    _invalidateTextCheck();
    busy = true;
    error = null;
    final n = ++_serial;
    _masks.add(r);
    preview = null;
    _operation = null;
    localReviewed = false;
    _notify();
    try {
      await _render(n);
    } catch (_) {
      if (_current(n)) {
        localBytes = null;
        error = '遮挡处理失败，请重新选择图片。';
      }
    } finally {
      _end(n);
    }
  }

  void acknowledgeLocal(bool value) {
    if (!canChangeLocal) return;
    localReviewed = value;
    _notify();
  }

  void discard() {
    if (!canChangeLocal) return;
    _invalidateTextCheck();
    _serial++;
    localBytes = null;
    preview = null;
    _operation = null;
    _masks.clear();
    _pixels?.dispose();
    _pixels = null;
    localReviewed = false;
    error = null;
    message = '已清除本次图片；未上传。';
    _notify();
  }

  Future<void> load() async {
    if (unknown) return;
    final n = _begin();
    if (n == null) return;
    try {
      if (!await _restoreJournal(n) || unknown) return;
      final r = await _client
          .get(_uri('private-images'), headers: _headers)
          .timeout(const Duration(seconds: 12));
      if (!_current(n)) return;
      if (r.statusCode != 200) {
        error = _failure(r.statusCode);
        return;
      }
      final data =
          (jsonDecode(utf8.decode(r.bodyBytes)) as Map<String, dynamic>)['data']
              as List<dynamic>;
      final rows = data
          .map(
            (x) =>
                PrivateMomentImageReceipt.fromJson(x as Map<String, dynamic>),
          )
          .toList();
      if (rows.length > 12 ||
          rows.any(
            (v) =>
                v.ownerID != _owner ||
                v.momentID != momentID ||
                v.status != 'ready_private' ||
                !v.retainUntil.isAfter(_clock().toUtc()),
          )) {
        throw const FormatException('图片列表无效');
      }
      images = List.unmodifiable(rows);
      loaded = true;
    } catch (_) {
      if (_current(n)) error = '未能读取私人图片，请重试；这不表示列表为空。';
    } finally {
      _end(n);
    }
  }

  Future<void> prepare() async {
    if (!canPreview) return;
    final n = _begin();
    if (n == null) return;
    try {
      if (!await _restoreJournal(n) || unknown) return;
      _operation = _newOperation();
      final pending = _reference('preview');
      if (!await _journalBeforeWire(n, pending)) return;
      final r = await _client
          .post(
            _uri('private-image-previews'),
            headers: {..._headers, 'Content-Type': 'application/json'},
            body: jsonEncode({
              'operationId': _operation,
              'momentRevision': momentRevision,
              'mimeType': 'image/png',
              'byteSize': localBytes!.length,
              'sha256': sha256.convert(localBytes!).toString(),
              'pixelRisk': pixelRisk,
              'purpose': privateMomentImagePurpose,
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (!_current(n)) return;
      if (r.statusCode != 201) {
        error = _failure(r.statusCode);
        // A failed write response is not proof of no effect. Keep its reference.
        _unknown = 'preview';
        return;
      }
      final v = _receipt(r);
      if (!_exact(v) ||
          v.status != 'preview' ||
          !v.previewExpiresAt.isAfter(_clock().toUtc())) {
        throw const FormatException('预览无效');
      }
      if (!await _clearJournal(n, pending)) return;
      preview = v;
    } catch (_) {
      if (_current(n)) {
        _unknown = 'preview';
        error = '预览创建结果未知，请核实；图片尚未提交。';
      }
    } finally {
      _end(n);
    }
  }

  Future<void> saveReviewed({required bool confirmed}) async {
    if (!confirmed || !canSave) return;
    final v = preview!;
    if (!_exact(v)) return;
    final n = _begin();
    if (n == null) return;
    try {
      if (!await _restoreJournal(n) || unknown) return;
      final pending = _reference('save', asset: v);
      if (!await _journalBeforeWire(n, pending)) return;
      final r = await _client
          .put(
            _uri('private-image-previews/${v.id}/content'),
            headers: {
              ..._headers,
              'Content-Type': 'image/png',
              'X-Birdtie-Private-Image-Confirmation': v.id,
            },
            body: localBytes!,
          )
          .timeout(const Duration(seconds: 12));
      if (!_current(n)) return;
      if (r.statusCode != 200) {
        error = _failure(r.statusCode);
        _unknown = 'save';
        return;
      }
      final receipt = _receipt(r);
      if (!_exact(receipt) ||
          receipt.id != v.id ||
          receipt.status != 'ready_private') {
        throw const FormatException('保存回执无效');
      }
      if (!await _clearJournal(n, pending)) return;
      _saved(receipt);
    } catch (_) {
      if (_current(n)) {
        _unknown = 'save';
        error = '保存结果未知，请核实；不会重复提交图片。';
      }
    } finally {
      _end(n);
    }
  }

  void _saved(PrivateMomentImageReceipt v) {
    preview = v;
    _unknown = null;
    images = List.unmodifiable([...images.where((x) => x.id != v.id), v]);
    loaded = true;
    message = '私人图片已保存，仅自己可读，最长保留 30 天；未公开，也未发送给 AI。';
  }

  Future<void> reconcile() async {
    if (!unknown) return;
    final kind = _unknown;
    final n = _begin();
    if (n == null) return;
    final pending = _pending;
    if (pending != null) {
      await _reconcileReference(n, pending);
      _end(n);
      return;
    }
    final operation = kind == 'delete' ? _deleting!.operationID : _operation;
    try {
      final r = await _client
          .get(_uri('private-image-operations/$operation'), headers: _headers)
          .timeout(const Duration(seconds: 12));
      if (!_current(n)) return;
      if (r.statusCode != 200) {
        error = r.statusCode == 404
            ? '暂未查到回执，请稍后继续核实；不会重新提交。'
            : _failure(r.statusCode);
        return;
      }
      final v = _receipt(r);
      if (v.operationID != operation) throw const FormatException('操作回执不匹配');
      if (kind == 'delete') {
        if (v.id != _deleting?.id) throw const FormatException('删除回执不匹配');
        if (v.status == 'deleted') {
          images = List.unmodifiable(images.where((x) => x.id != v.id));
          displayedBytes = null;
          _unknown = null;
          _deleting = null;
          message = '已移除私人图片。';
        } else {
          error = '尚未确认移除，请稍后继续核实。';
        }
      } else {
        if (!_exact(v)) throw const FormatException('图片版本不匹配');
        if (kind == 'save' && v.id != preview?.id) {
          throw const FormatException('保存回执与确认的图片不匹配');
        }
        if (v.status == 'ready_private') {
          _saved(v);
        } else if (kind == 'preview' &&
            v.status == 'preview' &&
            v.previewExpiresAt.isAfter(_clock().toUtc())) {
          preview = v;
          _unknown = null;
          message = '已核实预览，请检查后明确保存。';
        } else {
          error = '尚未确认保存或期限已变化，请继续核实；不会重复提交。';
        }
      }
    } catch (_) {
      if (_current(n)) error = '未能核实结果，请稍后重试核实。';
    } finally {
      _end(n);
    }
  }

  Future<void> _reconcileReference(
    int n,
    PendingPrivateImageOperation p,
  ) async {
    try {
      final r = await _client
          .get(
            _uri('private-image-operations/${p.operationID}'),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      _observe();
      if (!_current(n)) return;
      if (r.statusCode != 200) {
        error = r.statusCode == 404
            ? '暂未查到原操作回执，请继续核实；不会重新提交。'
            : _failure(r.statusCode);
        return;
      }
      final v = _receipt(r);
      if (!_matchesReference(v, p)) throw const FormatException('原图片操作回执不匹配');
      final now = _clock().toUtc();
      if (p.phase == 'delete') {
        if (v.status != 'deleted' || v.revision <= p.assetRevision!) {
          error = '尚未确认这张图片已移除，请继续核实；不会重复删除。';
          return;
        }
      } else if (v.status == 'ready_private') {
        if (!v.retainUntil.isAfter(now) ||
            p.phase == 'save' && v.revision <= p.assetRevision!) {
          error = '原保存状态或期限已变化，请继续核实；不会重复提交。';
          return;
        }
      } else if (p.phase == 'preview' && v.status == 'preview') {
        if (!v.previewExpiresAt.isAfter(now)) {
          error = '原预览期限已变化，结果仍需核实；不会恢复旧确认。';
          return;
        }
      } else {
        error = '原操作结果尚未核实，请继续核实；不会重复提交。';
        return;
      }
      if (!await _clearJournal(n, p)) return;
      if (v.status == 'ready_private') {
        _saved(v);
      } else if (p.phase == 'delete') {
        images = List.unmodifiable(images.where((x) => x.id != v.id));
        displayedBytes = null;
        _deleting = null;
        message = '已核实这张私人图片当前已移除。';
      } else {
        // Only an unchanged image still reviewed in this very controller can
        // retain its live preview. A reopened controller has neither bytes nor
        // review; the persisted reference cannot recreate either one.
        if (hasLocalImage && localReviewed && _exact(v)) {
          preview = v;
          message = '已核实预览，请检查后明确保存。';
        } else {
          preview = null;
          localReviewed = false;
          message = '已核实原预览，图片尚未保存。请重新选择图片并检查；旧确认不会恢复。';
        }
      }
      _unknown = _pending?.phase;
    } catch (_) {
      _observe();
      if (_current(n)) error = '未能核实原操作结果，请稍后继续核实；不会重新提交。';
    }
  }

  Future<void> read(PrivateMomentImageReceipt v) async {
    if (unknown ||
        v.ownerID != _owner ||
        v.momentID != momentID ||
        v.status != 'ready_private') {
      return;
    }
    final n = _begin();
    if (n == null) return;
    try {
      final r = await _readContent(v, n).timeout(const Duration(seconds: 12));
      if (!_current(n)) return;
      if (r.statusCode != 200) {
        error = _failure(r.statusCode);
        return;
      }
      if (r.headers['content-type'] != v.mime ||
          sha256.convert(r.bodyBytes).toString() != v.hash) {
        throw const FormatException('图片内容不匹配');
      }
      if (MomentImageHeader.inspect(r.bodyBytes, storedDerivative: true).mime !=
          v.mime) {
        throw const FormatException('图片格式不匹配');
      }
      displayedBytes = Uint8List.fromList(r.bodyBytes);
    } catch (_) {
      if (_current(n)) error = '图片未能读取或内容不匹配，请重新核实。';
    } finally {
      _end(n);
    }
  }

  Future<http.Response> _readContent(PrivateMomentImageReceipt v, int n) async {
    final request = http.Request('GET', _uri('private-images/${v.id}/content'));
    request.headers.addAll(_headers);
    final response = await _client.send(request);
    if (!_current(n) ||
        response.statusCode != 200 ||
        response.headers['content-type'] != v.mime ||
        (response.contentLength ?? 0) > momentImageMaxDerivativeBytes) {
      await response.stream.listen((_) {}).cancel();
      if (!_current(n)) throw const FormatException('图片页面已失效');
      if (response.statusCode != 200) {
        return http.Response(
          '',
          response.statusCode,
          headers: response.headers,
        );
      }
      throw const FormatException('图片内容或大小不匹配');
    }
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (!_current(n) ||
          bytes.length + chunk.length > momentImageMaxDerivativeBytes) {
        throw const FormatException('图片页面已失效或内容超限');
      }
      bytes.add(chunk);
    }
    return http.Response.bytes(
      bytes.takeBytes(),
      response.statusCode,
      headers: response.headers,
    );
  }

  Future<void> remove(
    PrivateMomentImageReceipt v, {
    required bool confirmed,
  }) async {
    if (!confirmed ||
        unknown ||
        v.ownerID != _owner ||
        v.momentID != momentID ||
        !images.any(
          (current) => identical(current, v) && current.revision == v.revision,
        )) {
      return;
    }
    final n = _begin();
    if (n == null) return;
    try {
      if (!await _restoreJournal(n) || unknown) return;
      final pending = _reference('delete', asset: v);
      if (!await _journalBeforeWire(n, pending)) return;
      _deleting = v;
      final r = await _client
          .delete(
            _uri('private-images/${v.id}?revision=${v.revision}'),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!_current(n)) return;
      if (r.statusCode != 204) {
        error = _failure(r.statusCode);
        _unknown = 'delete';
        return;
      }
      if (!await _clearJournal(n, pending)) return;
      images = List.unmodifiable(images.where((x) => x.id != v.id));
      displayedBytes = null;
      message = '已移除私人图片。';
      _deleting = null;
    } catch (_) {
      if (_current(n)) {
        _unknown = 'delete';
        error = '移除结果未知，请核实；不会重复删除。';
      }
    } finally {
      _end(n);
    }
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _invalidateTextCheck();
    _serial++;
    identityChanges.removeListener(_observe);
    workspaceChanges?.removeListener(_observe);
    _pixels?.dispose();
    _pixels = null;
    localBytes = null;
    displayedBytes = null;
    images = const [];
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
