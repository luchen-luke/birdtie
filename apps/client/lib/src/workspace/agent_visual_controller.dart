import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'agent_visual_pending_store.dart';
import 'local_visual_draft.dart';

const agentVisualPurpose = 'HUMAN_AGENT_VISUAL_ANALYSIS';

String _id() {
  final r = Random.secure(), b = List<int>.generate(16, (_) => r.nextInt(256));
  b[6] = (b[6] & 15) | 64;
  b[8] = (b[8] & 63) | 128;
  final s = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
  return '${s.substring(0, 8)}-${s.substring(8, 12)}-${s.substring(12, 16)}-${s.substring(16, 20)}-${s.substring(20)}';
}

DateTime _date(Object? value) {
  if (value is! String || !RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(value))
    throw const FormatException('图片分析期限无效');
  return DateTime.parse(value).toUtc();
}

void _require(bool value) {
  if (!value) throw const FormatException('图片分析回执无法核验');
}

class AgentVisualPreview {
  AgentVisualPreview(
    Map<String, dynamic> json,
    this.userText,
    String draftID,
    int draftRevision,
  ) : data = Map.unmodifiable(json) {
    final pixel = json['pixel'],
        destination = json['destination'],
        budget = json['budget'];
    _require(
      json['schemaVersion'] == 1 &&
          json['status'] == 'PREVIEW' &&
          json['purpose'] == agentVisualPurpose &&
          json['egressPurpose'] == 'MODEL_VISUAL_PIXEL_EGRESS' &&
          json['userText'] == userText &&
          json['clientDraftId'] == draftID &&
          json['clientDraftRevision'] == draftRevision &&
          json['memoryWriteAllowed'] == false &&
          json['placeFactWriteAllowed'] == false &&
          json['previewId'] is String &&
          visualUUID.hasMatch(json['previewId']) &&
          json['version'] is int &&
          json['version'] > 0 &&
          json['payloadDigest'] is String &&
          visualDigest.hasMatch(json['payloadDigest']) &&
          pixel is Map<String, dynamic> &&
          destination is Map<String, dynamic> &&
          budget is Map<String, dynamic>,
    );
    _require(
      pixel['sha256'] is String &&
          visualDigest.hasMatch(pixel['sha256']) &&
          pixel['id'] is String &&
          visualUUID.hasMatch(pixel['id']) &&
          pixel['revision'] is int &&
          pixel['revision'] > 0 &&
          ['image/png', 'image/jpeg'].contains(pixel['mimeType']) &&
          pixel['byteSize'] is int &&
          pixel['byteSize'] > 0 &&
          pixel['byteSize'] <= 10 * 1024 * 1024 &&
          pixel['width'] is int &&
          pixel['width'] > 0 &&
          pixel['width'] <= 4096 &&
          pixel['height'] is int &&
          pixel['height'] > 0 &&
          pixel['height'] <= 4096 &&
          pixel['previewContentPath'] ==
              '/v1/me/agent-visual/previews/${json['previewId']}/pixels' &&
          destination['provider'] is String &&
          destination['model'] is String &&
          destination['region'] is String &&
          destination['retention'] == 'UNKNOWN' &&
          budget['currency'] is String &&
          budget['priceVersion'] is String &&
          budget['inputTokenUpper'] is int &&
          budget['inputTokenUpper'] > 0 &&
          budget['outputTokenUpper'] is int &&
          budget['outputTokenUpper'] > 0 &&
          budget['costMicrosUpper'] is int &&
          budget['costMicrosUpper'] > 0,
    );
    _date(json['createdAt']);
    _date(json['expiresAt']);
    _date(budget['priceExpiresAt']);
  }
  final Map<String, dynamic> data;
  final String userText;
  String get id => data['previewId'];
  int get version => data['version'];
  String get digest => data['payloadDigest'];
  Map<String, dynamic> get pixel => data['pixel'];
  Map<String, dynamic> get destination => data['destination'];
  Map<String, dynamic> get budget => data['budget'];
  bool validAt(DateTime now) =>
      _date(data['expiresAt']).isAfter(now) &&
      _date(budget['priceExpiresAt']).isAfter(now);
}

/// Only the preview API sees selected pixels. A separate reviewed-pixel action
/// approves dispatch; any uncertain dispatch is recovered exclusively by GET.
class AgentVisualController extends ChangeNotifier with WidgetsBindingObserver {
  AgentVisualController({
    required this.authorizationHeader,
    required this.ownerID,
    required this.sourceContextID,
    required this.sourceContextType,
    required this.sourceCurrent,
    required this.draftCurrent,
    required this.identityChanges,
    required Uint8List bytes,
    required this.userText,
    required this.clientDraftRevision,
    required this.masked,
    http.Client? client,
    String? apiBaseUrl,
    AgentVisualPendingStore? pendingStore,
    DateTime Function()? clock,
  }) : _bytes = Uint8List.fromList(bytes),
       _client = client ?? http.Client(),
       _ownedClient = client == null,
       _base = Uri.parse(apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl),
       _pending = pendingStore ?? SecureAgentVisualPendingStore(),
       _clock = clock ?? (() => DateTime.now().toUtc()) {
    _owner = ownerID();
    _token = authorizationHeader();
    _selectedSHA256 = sha256.convert(bytes).toString();
    identityChanges.addListener(_observe);
    WidgetsBinding.instance.addObserver(this);
  }
  final String? Function() authorizationHeader, ownerID;
  final String sourceContextID, sourceContextType, userText;
  final int clientDraftRevision;
  final bool masked;
  final bool Function() sourceCurrent, draftCurrent;
  final Listenable identityChanges;
  final Uint8List _bytes;
  final http.Client _client;
  final bool _ownedClient;
  final Uri _base;
  final AgentVisualPendingStore _pending;
  final DateTime Function() _clock;
  String get _pendingSource => '$sourceContextType:$sourceContextID';
  late final String? _owner;
  String? _token;
  late final String _selectedSHA256;
  final String clientDraftID = _id();
  bool _disposed = false,
      _notifierDisposed = false,
      _retired = false,
      busy = false,
      pixelVisible = false,
      uncertain = false;
  int _serial = 0;
  int _pageReaders = 0;
  void retainPage() {
    if (_notifierDisposed) throw StateError('图片页已退休');
    _pageReaders++;
  }

  void releasePage() {
    _pageReaders--;
    if (_disposed && _pageReaders == 0 && !_notifierDisposed) {
      _notifierDisposed = true;
      super.dispose();
    }
  }

  String? error, operationID;
  AgentVisualPreview? preview;
  Uint8List? previewPixels;
  Map<String, dynamic>? report;
  LocalVisualReportProvenance? _reportProvenance;
  AgentVisualPreview? _knownPreviewBinding;
  Timer? _expiry;
  bool get _scopeCurrent =>
      !_disposed &&
      !_retired &&
      sourceCurrent() &&
      draftCurrent() &&
      ownerID() == _owner &&
      authorizationHeader() != null &&
      visualUUID.hasMatch(_owner ?? '') &&
      WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed;
  bool get current {
    _observe();
    return _scopeCurrent && authorizationHeader() == _token;
  }

  int get sourceVersion {
    _observe();
    return _serial;
  }

  LocalVisualSourceSelection? get localSourceSelection {
    if (!current) return null;
    return LocalVisualSourceSelection(
      ownerID: _owner!,
      origin: _base.origin,
      sourceContextType: sourceContextType,
      sourceContextID: sourceContextID,
      clientDraftID: clientDraftID,
      clientDraftRevision: clientDraftRevision,
      selectedImageSHA256: _selectedSHA256,
    );
  }

  LocalVisualReportProvenance? get localReportProvenance {
    if (!current || report == null || _reportProvenance == null) return null;
    try {
      return sha256.convert(utf8.encode(jsonEncode(report))).toString() ==
              _reportProvenance!.reportSHA256
          ? _reportProvenance
          : null;
    } catch (_) {
      return null;
    }
  }

  LocalVisualDraft? localQuestionDraft(String text) {
    final source = localSourceSelection;
    return source == null
        ? null
        : LocalVisualDraft.ownQuestion(text: text, sourceSelection: source);
  }

  LocalVisualDraft? localReportDraft(String text) {
    final source = localSourceSelection, p = localReportProvenance;
    return source == null || p == null
        ? null
        : LocalVisualDraft.reportNote(
            text: text,
            sourceSelection: source,
            reportProvenance: p,
          );
  }

  bool get canAnalyze =>
      current &&
      !busy &&
      !uncertain &&
      pixelVisible &&
      previewPixels != null &&
      preview?.validAt(_clock()) == true;
  Uint8List? get localPixels => current ? _bytes.asUnmodifiableView() : null;
  void _notify() {
    if (_notifierDisposed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      SchedulerBinding.instance.addPostFrameCallback((_) {
        if (!_notifierDisposed) notifyListeners();
      });
    } else {
      notifyListeners();
    }
  }

  void _observe() {
    if (!_scopeCurrent && !_retired && !_disposed) {
      _retired = true;
      ++_serial;
      busy = false;
      pixelVisible = false;
      previewPixels?.fillRange(0, previewPixels!.length, 0);
      previewPixels = null;
      preview = null;
      report = null;
      _reportProvenance = null;
      _knownPreviewBinding = null;
      _bytes.fillRange(0, _bytes.length, 0);
      error = '账号、范围或图片文字版本已变化，请重新打开当前图片。';
      _expiry?.cancel();
      _notify();
    } else if (_scopeCurrent && authorizationHeader() != _token) {
      _token = authorizationHeader();
      ++_serial;
      busy = false;
      pixelVisible = false;
      previewPixels?.fillRange(0, previewPixels!.length, 0);
      previewPixels = null;
      preview = null;
      if (report != null && operationID != null) uncertain = true;
      report = null;
      _reportProvenance = null;
      _expiry?.cancel();
      error = '登录已更新。本人图片问题与本机材料保留，旧批准和报告显示已失效；需要重新准备或核实原操作。';
      _notify();
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) _observe();
  }

  bool _valid(int serial) {
    _observe();
    return current && serial == _serial;
  }

  Uri _uri(String path) => _base.resolve(path);
  Future<http.Response> _request(
    String method,
    String path, {
    Object? input,
    int maxBytes = 256 * 1024,
  }) async {
    if (!current) throw const FormatException('当前图片来源已失效');
    final r = http.Request(method, _uri(path))
      ..followRedirects = false
      ..headers['Authorization'] = _token!;
    if (input != null) {
      r.headers['Content-Type'] = 'application/json';
      r.body = jsonEncode(input);
    }
    final response = await _client.send(r).timeout(const Duration(seconds: 30));
    final builder = BytesBuilder(copy: false);
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 30),
    )) {
      if (chunk.length > maxBytes - builder.length)
        throw const FormatException('图片分析响应超限');
      builder.add(chunk);
    }
    return http.Response.bytes(
      builder.takeBytes(),
      response.statusCode,
      headers: response.headers,
    );
  }

  Map<String, dynamic> _data(http.Response r) {
    if (r.statusCode == 503)
      throw const FormatException('图片分析服务尚未配置，图片和文字已保留。');
    if (r.statusCode == 401 || r.statusCode == 403)
      throw const FormatException('当前会话无权读取或分析这张图片。');
    if (r.statusCode < 200 || r.statusCode >= 300)
      throw const FormatException('图片分析未完成，请核实当前操作。');
    final value = jsonDecode(utf8.decode(r.bodyBytes));
    _require(
      value is Map<String, dynamic> && value['data'] is Map<String, dynamic>,
    );
    return value['data'];
  }

  Future<void> prepare() async {
    if (!current || busy || uncertain || report != null) return;
    final serial = ++_serial;
    busy = true;
    error = null;
    pixelVisible = false;
    _notify();
    try {
      final pending = await _pending.read(
        _base.origin,
        _owner!,
        _pendingSource,
      );
      if (!_valid(serial)) return;
      if (pending != null) {
        operationID = pending;
        uncertain = true;
        error = '上次图片分析尚未核实，请先核实原操作。';
        return;
      }
      final result = _data(
        await _request(
          'POST',
          '/v1/me/agent-visual/previews',
          input: {
            'operationId': _id(),
            'clientDraftId': clientDraftID,
            'clientDraftRevision': clientDraftRevision,
            'sourceContextType': sourceContextType,
            'sourceContextId': sourceContextID,
            'userText': userText,
            'purpose': agentVisualPurpose,
            'image': {
              'mimeType': 'image/png',
              'contentBase64': base64Encode(_bytes),
              'inputSha256': sha256.convert(_bytes).toString(),
              'pixelRisk': masked ? 'USER_MASKED' : 'UNKNOWN',
            },
          },
        ),
      );
      if (!_valid(serial)) return;
      final p = AgentVisualPreview(
        result,
        userText,
        clientDraftID,
        clientDraftRevision,
      );
      _require(p.validAt(_clock()));
      final pixels = await _request(
        'GET',
        p.pixel['previewContentPath'],
        maxBytes: 10 * 1024 * 1024,
      );
      if (!_valid(serial)) return;
      _require(
        pixels.statusCode == 200 &&
            pixels.bodyBytes.length == p.pixel['byteSize'] &&
            sha256.convert(pixels.bodyBytes).toString() == p.pixel['sha256'] &&
            pixels.headers['content-type']?.split(';').first ==
                p.pixel['mimeType'] &&
            p.validAt(_clock()),
      );
      preview = p;
      _knownPreviewBinding = p;
      previewPixels = Uint8List.fromList(pixels.bodyBytes);
      _expiry?.cancel();
      final previewExpiry = _date(p.data['expiresAt']);
      final priceExpiry = _date(p.budget['priceExpiresAt']);
      final expiresAt = previewExpiry.isBefore(priceExpiry)
          ? previewExpiry
          : priceExpiry;
      _expiry = Timer(expiresAt.difference(_clock()), () {
        pixelVisible = false;
        error = '此次批准预览已到期，请重新准备。';
        _notify();
      });
    } catch (e) {
      if (_valid(serial))
        error = e is FormatException ? e.message : '图片分析预览未能读取，图片和文字已保留。';
    } finally {
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
      }
    }
  }

  void setPixelVisible(String id, int version, bool value) {
    if (current &&
        preview?.id == id &&
        preview?.version == version &&
        preview?.validAt(_clock()) == true) {
      pixelVisible = value;
    }
  }

  Future<void> analyze() async {
    if (!canAnalyze) return;
    final serial = ++_serial, p = preview!, op = _id();
    busy = true;
    error = null;
    pixelVisible = false;
    _notify();
    try {
      await _pending.write(_base.origin, _owner!, _pendingSource, op);
      if (!_valid(serial) || !p.validAt(_clock())) {
        // No request has started. This exact local selector can be removed;
        // a dispatch with an uncertain outcome is never cleared this way.
        await _pending.delete(_base.origin, _owner, _pendingSource, op);
        return;
      }
      operationID = op;
      uncertain = true;
      final result = _data(
        await _request(
          'POST',
          '/v1/me/agent-visual/previews/${p.id}/analyze',
          input: {
            'operationId': op,
            'expectedVersion': p.version,
            'payloadDigest': p.digest,
            'pixelSha256': p.pixel['sha256'],
            'purpose': agentVisualPurpose,
            'confirmed': true,
          },
        ),
      );
      if (!_valid(serial)) return;
      await _accept(result, op, serial);
    } catch (e) {
      if (_valid(serial))
        error = operationID == op
            ? '分析结果尚未核实，请核实原操作；图片和文字已保留。'
            : '操作恢复记录未能保存，尚未发送分析。';
    } finally {
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> recover() async {
    if (!current || busy || !uncertain || operationID == null) return;
    final serial = ++_serial, op = operationID!;
    busy = true;
    error = null;
    _notify();
    try {
      final result = _data(
        await _request('GET', '/v1/me/agent-visual/operations/$op'),
      );
      if (_valid(serial)) await _accept(result, op, serial);
    } catch (_) {
      if (_valid(serial)) error = '原操作仍无法核实，请稍后再次核实。';
    } finally {
      if (!_disposed && serial == _serial) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _accept(
    Map<String, dynamic> result,
    String op,
    int serial,
  ) async {
    _require(
      result['schemaVersion'] == 1 &&
          result['operationId'] == op &&
          result['previewId'] is String &&
          visualUUID.hasMatch(result['previewId']) &&
          result['version'] is int,
    );
    final binding = preview ?? _knownPreviewBinding;
    if (binding != null)
      _require(
        result['previewId'] == binding.id &&
            result['version'] == binding.version,
      );
    final status = result['status'];
    if (status == 'UNKNOWN') {
      error = '结果尚未确认，请核实原操作。';
      return;
    }
    _require(['COMPLETED', 'REFUSED', 'UNAVAILABLE'].contains(status));
    if (status == 'COMPLETED') {
      final value = result['report'];
      _require(
        value is Map<String, dynamic> &&
            value['summary'] is String &&
            value['observations'] is List &&
            value['unknowns'] is List,
      );
      _require((value['unknowns'] as List).every((v) => v is String));
      for (final item in value['observations']) {
        _require(
          item is Map<String, dynamic> &&
              [
                'VISIBLE_OBJECT',
                'VISIBLE_TEXT',
                'VISIBLE_SCENE',
              ].contains(item['kind']) &&
              item['value'] is String &&
              item['confidence'] is Map &&
              item['confidence']['kind'] == 'QUALITATIVE' &&
              [
                'LOW',
                'MEDIUM',
                'HIGH',
                'UNKNOWN',
              ].contains(item['confidence']['level']) &&
              item['source'] is Map &&
              item['source']['type'] == 'APPROVED_PIXELS',
        );
        final source = item['source'];
        _require(
          source['id'] is String &&
              visualUUID.hasMatch(source['id']) &&
              source['revision'] is int &&
              source['sha256'] is String &&
              visualDigest.hasMatch(source['sha256']),
        );
        if (binding != null)
          _require(
            source['id'] == binding.pixel['id'] &&
                source['revision'] == binding.pixel['revision'] &&
                source['sha256'] == binding.pixel['sha256'],
          );
        _date(item['observedAt']);
      }
      if (!_valid(serial)) return;
      final source = localSourceSelection;
      _reportProvenance = null;
      // Local export is optional. A report that cannot fit its local text
      // envelope must still retain the original operation's verified result.
      if (source != null && binding != null) {
        try {
          _reportProvenance = LocalVisualReportProvenance(
            selectionBindingKey: source.bindingKey,
            operationID: op,
            previewID: binding.id,
            previewVersion: binding.version,
            pixelID: binding.pixel['id'],
            pixelRevision: binding.pixel['revision'],
            pixelSHA256: binding.pixel['sha256'],
            reportSHA256: sha256
                .convert(utf8.encode(jsonEncode(value)))
                .toString(),
            unknowns: (value['unknowns'] as List).cast<String>(),
          );
        } on FormatException {
          _reportProvenance = null;
        }
      }
      report = Map.unmodifiable(
        jsonDecode(jsonEncode(value)) as Map<String, dynamic>,
      );
    } else {
      _require(result['report'] == null);
      _reportProvenance = null;
      error = status == 'REFUSED' ? '此次图片分析已拒绝，草稿已保留。' : '此次图片分析不可用，草稿已保留。';
    }
    await _pending.delete(_base.origin, _owner!, _pendingSource, op);
    if (_valid(serial)) uncertain = false;
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    ++_serial;
    _expiry?.cancel();
    identityChanges.removeListener(_observe);
    WidgetsBinding.instance.removeObserver(this);
    _bytes.fillRange(0, _bytes.length, 0);
    previewPixels?.fillRange(0, previewPixels!.length, 0);
    previewPixels = null;
    preview = null;
    report = null;
    _reportProvenance = null;
    _knownPreviewBinding = null;
    if (_ownedClient) _client.close();
    _notify();
    if (_pageReaders == 0) {
      _notifierDisposed = true;
      super.dispose();
    }
  }
}
