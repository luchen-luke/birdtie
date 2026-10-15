import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'video_material_picker.dart';
import 'video_material_pending_store.dart';

class VideoMaterialCapabilities {
  const VideoMaterialCapabilities({
    required this.available,
    required this.providerLabel,
    required this.region,
    required this.uploadOrigin,
    required this.storagePolicyID,
  });
  final bool available;
  final String providerLabel, region, uploadOrigin, storagePolicyID;
  factory VideoMaterialCapabilities.fromJson(Map<String, dynamic> p) {
    final a = p['available'] as bool;
    final label = p['providerLabel'] as String,
        region = p['region'] as String,
        origin = p['uploadOrigin'] as String,
        policy = p['storagePolicyId'] as String;
    final u = Uri.tryParse(origin);
    if (p['maxBytes'] != videoMaterialMaxBytes ||
        p['maxDurationMs'] != videoMaterialMaxDurationMS ||
        p['mimeType'] != 'video/mp4' ||
        p['purpose'] != videoMaterialPurpose ||
        (a &&
            (label.isEmpty ||
                region.isEmpty ||
                !videoMaterialDigest.hasMatch(policy) ||
                u == null ||
                !u.hasAuthority ||
                u.userInfo.isNotEmpty ||
                u.hasQuery ||
                u.hasFragment ||
                !const ['https', 'http'].contains(u.scheme) ||
                (u.scheme == 'http' &&
                    !const [
                      'localhost',
                      '127.0.0.1',
                      '::1',
                    ].contains(u.host)) ||
                (u.path.isNotEmpty && u.path != '/'))))
      throw const FormatException('视频存储能力无法核验');
    return VideoMaterialCapabilities(
      available: a,
      providerLabel: label,
      region: region,
      uploadOrigin: origin,
      storagePolicyID: policy,
    );
  }
}

class VideoMaterialReceipt {
  const VideoMaterialReceipt({
    required this.id,
    required this.ownerID,
    required this.operationID,
    required this.status,
    required this.revision,
    required this.byteSize,
    required this.durationMS,
    required this.sha256Hex,
    required this.storagePolicyID,
    required this.uploadExpiresAt,
    required this.retainUntil,
    required this.width,
    required this.height,
    this.failureCode = '',
  });
  final String id,
      ownerID,
      operationID,
      status,
      sha256Hex,
      storagePolicyID,
      failureCode;
  final int revision, byteSize, durationMS, width, height;
  final DateTime uploadExpiresAt, retainUntil;
  String get statusLabel => switch (status) {
    'quarantine' => '等待上传',
    'queued' => '等待安全检查',
    'scanning' => '正在安全检查',
    'ready_private' => '已保存为本人私有材料',
    'rejected' => '安全检查未通过',
    'deleting' => '正在清理私人存储',
    'deleted' => '已删除',
    _ => '状态未确认',
  };
  factory VideoMaterialReceipt.fromJson(Map<String, dynamic> p) {
    DateTime date(String key) {
      final raw = p[key] as String;
      if (!RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(raw))
        throw const FormatException('视频期限无法核验');
      return DateTime.parse(raw).toUtc();
    }

    final id = p['id'] as String,
        owner = p['ownerAccountId'] as String,
        op = p['operationId'] as String,
        state = p['status'] as String,
        hash = p['sha256'] as String,
        policy = p['storagePolicyId'] as String;
    final rev = p['revision'] as int,
        size = p['byteSize'] as int,
        ms = p['durationMs'] as int,
        w = p['width'] as int,
        h = p['height'] as int;
    if (![id, owner, op].every(videoMaterialUUID.hasMatch) ||
        !const [
          'quarantine',
          'queued',
          'scanning',
          'ready_private',
          'rejected',
          'deleting',
          'deleted',
        ].contains(state) ||
        !videoMaterialDigest.hasMatch(hash) ||
        !videoMaterialDigest.hasMatch(policy) ||
        rev < 1 ||
        size < 1 ||
        size > videoMaterialMaxBytes ||
        ms < 1 ||
        ms > videoMaterialMaxDurationMS ||
        p['mimeType'] != 'video/mp4' ||
        p['purpose'] != videoMaterialPurpose ||
        p['confirmedPrivateSave'] != true ||
        (state == 'ready_private' &&
            (w < 1 || h < 1 || w > 1920 || h > 1920 || w * h > 1920 * 1080)))
      throw const FormatException('视频回执无法核验');
    return VideoMaterialReceipt(
      id: id,
      ownerID: owner,
      operationID: op,
      status: state,
      revision: rev,
      byteSize: size,
      durationMS: ms,
      sha256Hex: hash,
      storagePolicyID: policy,
      uploadExpiresAt: date('uploadExpiresAt'),
      retainUntil: date('retainUntil'),
      width: w,
      height: h,
      failureCode: p['failureCode'] as String? ?? '',
    );
  }
}

class VideoMaterialController extends ChangeNotifier {
  VideoMaterialController({
    required this.authorizationHeader,
    required this.ownerID,
    required this.sourceCurrent,
    required this.identityChanges,
    this.localSelectionOnly = false,
    http.Client? client,
    String? apiBaseUrl,
    VideoMaterialPicker? picker,
    VideoMaterialPendingStore? pendingStore,
    http.Client? uploadClient,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       _picker = picker ?? VideoMaterialPicker(),
       _pendingStore = pendingStore ?? const SecureVideoMaterialPendingStore(),
       _uploadClient = uploadClient ?? http.Client(),
       _ownsUpload = uploadClient == null {
    _token = authorizationHeader();
    _owner = ownerID();
    identityChanges.addListener(_observe);
  }
  final String? Function() authorizationHeader, ownerID;
  final bool Function() sourceCurrent;
  final bool localSelectionOnly;
  final Listenable identityChanges;
  final String _base;
  final http.Client _client;
  final bool _ownsClient;
  http.Client _uploadClient;
  final bool _ownsUpload;
  final VideoMaterialPicker _picker;
  final VideoMaterialPendingStore _pendingStore;
  String? _token, _owner;
  bool _retired = false, _closed = false;
  int _epoch = 0, _selectSerial = 0, _reads = 0;
  VideoMaterialSelection? selection;
  VideoMaterialCapabilities? capabilities;
  VideoMaterialReceipt? active;
  List<VideoMaterialReceipt> items = const [];
  List<VideoMaterialPending> pendingOperations = const [];
  String? nextCursor, error;
  bool selecting = false, loading = false, uploading = false, uncertain = false;
  int uploadedBytes = 0;
  VideoMaterialPending? _pending;
  String? _storedPreviewID;
  Timer? _previewExpiry;
  bool get current {
    _sync();
    return !_closed && !_retired;
  }

  bool get canSelect =>
      current && !selecting && !uploading && !uncertain && _picker.supported;
  bool get canSave =>
      !localSelectionOnly &&
      current &&
      selection != null &&
      !selecting &&
      !uploading &&
      capabilities?.available == true &&
      active?.status != 'ready_private' &&
      active?.status != 'queued' &&
      active?.status != 'scanning' &&
      active?.status != 'deleting' &&
      active?.status != 'deleted';
  bool get busy => selecting || uploading;
  double? get progress =>
      selection == null ? null : uploadedBytes / selection!.bytes.length;
  void _sync() {
    if (_closed || _retired) return;
    // A Now selection has no private upload approval or executable journal.
    // Renewal retires in-flight reads/pickers while retaining this owner's
    // selected bytes and local preview until the material realm changes.
    final nextToken = authorizationHeader();
    if (localSelectionOnly &&
        _owner != null &&
        ownerID() == _owner &&
        sourceCurrent() &&
        _token != null &&
        nextToken != null &&
        nextToken != _token &&
        _pending == null &&
        active == null &&
        !uploading) {
      _token = nextToken;
      ++_epoch;
      ++_selectSerial;
      ++_reads;
      _previewExpiry?.cancel();
      selecting = loading = uncertain = false;
      capabilities = null;
      items = const [];
      pendingOperations = const [];
      error = null;
    }
    if (_token == null ||
        _owner == null ||
        !videoMaterialUUID.hasMatch(_owner!) ||
        authorizationHeader() != _token ||
        ownerID() != _owner ||
        !sourceCurrent()) {
      _retired = true;
      _epoch++;
      _selectSerial++;
      _reads++;
      _previewExpiry?.cancel();
      selecting = loading = uploading = uncertain = false;
      final old = selection;
      selection = null;
      if (old != null)
        unawaited(_disposeSelection(old).catchError((Object _) {}));
      items = const [];
      pendingOperations = const [];
      active = null;
      _pending = null;
      capabilities = null;
      if (_ownsUpload) _uploadClient.close();
      error = '账号或来源已变化，请返回后重新选择视频。';
    }
  }

  Future<void> _disposeSelection(VideoMaterialSelection selected) async {
    if (localSelectionOnly) {
      selected.bytes.fillRange(0, selected.bytes.length, 0);
    }
    await selected.dispose();
  }

  void _observe() {
    final e = _epoch;
    _sync();
    if (e != _epoch) _notify();
  }

  bool _valid(int epoch) => current && epoch == _epoch;
  void _authorizationStatus(int status) {
    if (status != 401 && status != 403) return;
    _token = null;
    _sync();
    _notify();
  }

  void _notify() {
    if (!_closed) notifyListeners();
  }

  Uri _uri(
    String suffix, {
    bool capability = false,
    Map<String, String>? query,
  }) => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}${capability ? '/v1/video-material-capabilities' : '/v1/me/video-materials'}$suffix',
  ).replace(queryParameters: query);
  Map<String, String> get _headers => {
    'Authorization': _token!,
    'Content-Type': 'application/json',
  };
  dynamic _data(http.Response r) =>
      (jsonDecode(utf8.decode(r.bodyBytes)) as Map<String, dynamic>)['data'];
  VideoMaterialReceipt _receipt(dynamic p) {
    final v = VideoMaterialReceipt.fromJson(p as Map<String, dynamic>);
    if (v.ownerID != _owner) throw const FormatException('视频所属账号已变化');
    return v;
  }

  Future<void> initialize() async {
    if (!current || localSelectionOnly) return;
    final e = _epoch;
    try {
      final lost = _picker.discardLostSelection();
      final r = await _client
          .get(_uri('', capability: true))
          .timeout(const Duration(seconds: 12));
      await lost;
      if (!_valid(e)) return;
      if (r.statusCode != 200) throw const FormatException();
      capabilities = VideoMaterialCapabilities.fromJson(
        _data(r) as Map<String, dynamic>,
      );
      pendingOperations = await _pendingStore.read(_base, _owner!);
      if (!_valid(e)) return;
    } catch (_) {
      if (_valid(e)) error = '无法核实视频存储能力，本机选取与预览仍可使用。';
    } finally {
      if (_valid(e)) _notify();
    }
  }

  bool supportsSource(VideoMaterialSource source) =>
      _picker.supportsSource(source);
  bool canSelectSource(VideoMaterialSource source) =>
      canSelect && supportsSource(source);
  Future<void> select({
    VideoMaterialSource source = VideoMaterialSource.gallery,
  }) async {
    if (!canSelect) return;
    final e = _epoch, serial = ++_selectSerial;
    selecting = true;
    error = null;
    _notify();
    VideoMaterialSelection? next;
    try {
      next = await _picker.selectSource(source);
      if (!_valid(e) || serial != _selectSerial) {
        if (next != null) await _disposeSelection(next);
        return;
      }
      if (next == null) return;
      validateLocalVideoMP4(next.bytes);
      if (next.durationMS < 1 ||
          next.durationMS > videoMaterialMaxDurationMS ||
          next.preview.width < 1 ||
          next.preview.height < 1 ||
          next.preview.width > 1920 ||
          next.preview.height > 1920 ||
          next.preview.width * next.preview.height > 1920 * 1080)
        throw const FormatException('视频超过本机处理范围。');
      if (_pending != null &&
          (next.sha256Hex != _pending!.input['sha256'] ||
              next.bytes.length != _pending!.input['byteSize']))
        throw const FormatException('请选择原来的视频；当前恢复记录仍保留。');
      final old = selection;
      _previewExpiry?.cancel();
      _storedPreviewID = null;
      selection = next;
      next = null;
      if (old != null) await _disposeSelection(old);
      if (_pending == null) active = null;
      uploadedBytes = 0;
    } catch (failure) {
      if (next != null) await _disposeSelection(next);
      if (_valid(e) && serial == _selectSerial)
        error = failure is FormatException
            ? failure.message.toString()
            : '无法读取视频，请检查图库权限后重新选择。';
    } finally {
      if (_valid(e) && serial == _selectSerial) {
        selecting = false;
        _notify();
      }
    }
  }

  Future<void> removeLocal() async {
    if (!current || uploading || uncertain) return;
    _selectSerial++;
    final old = selection;
    _previewExpiry?.cancel();
    _storedPreviewID = null;
    selection = null;
    selecting = false;
    uploadedBytes = 0;
    if (old != null) await _disposeSelection(old);
    _notify();
  }

  Future<void> pausePreview() async {
    if (selection != null) await selection!.preview.pause();
  }

  Future<void> togglePreview() async {
    if (!current || selection == null) return;
    try {
      await selection!.preview.toggle();
      if (current) _notify();
    } catch (_) {
      if (current) {
        error = '无法播放本机视频，请重新选择。';
        _notify();
      }
    }
  }

  String _uuid() {
    final r = Random.secure(),
        b = List<int>.generate(16, (_) => r.nextInt(256));
    b[6] = (b[6] & 15) | 64;
    b[8] = (b[8] & 63) | 128;
    final s = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    return '${s.substring(0, 8)}-${s.substring(8, 12)}-${s.substring(12, 16)}-${s.substring(16, 20)}-${s.substring(20)}';
  }

  Future<bool> save() async {
    if (!canSave) return false;
    final selected = selection!, caps = capabilities!, e = _epoch;
    uploading = true;
    uncertain = false;
    error = null;
    uploadedBytes = 0;
    _notify();
    try {
      if (sha256.convert(selected.bytes).toString() != selected.sha256Hex)
        throw const FormatException('本机视频版本已变化，请重新选择。');
      _pending ??= VideoMaterialPending(_owner!, {
        'operationId': _uuid(),
        'mimeType': 'video/mp4',
        'byteSize': selected.bytes.length,
        'sha256': selected.sha256Hex,
        'durationMs': selected.durationMS,
        'purpose': videoMaterialPurpose,
        'confirmedPrivateSave': true,
        'storagePolicyId': caps.storagePolicyID,
      });
      if (_pending!.input['storagePolicyId'] != caps.storagePolicyID)
        throw const FormatException('存储服务或区域已变化，请核实原保存结果。');
      await _pendingStore.write(_base, _pending!);
      if (!_valid(e)) return false;
      final r = await _client
          .post(_uri(''), headers: _headers, body: jsonEncode(_pending!.input))
          .timeout(const Duration(seconds: 15));
      _authorizationStatus(r.statusCode);
      if (!_valid(e)) return false;
      if (r.statusCode != 201)
        throw const FormatException('无法建立私有视频上传，请核实结果后重试。');
      final plan = _data(r) as Map<String, dynamic>;
      final v = _receipt(plan['receipt']);
      if (v.operationID != _pending!.operationID ||
          v.sha256Hex != selected.sha256Hex ||
          v.byteSize != selected.bytes.length ||
          v.storagePolicyID != caps.storagePolicyID)
        throw const FormatException('视频上传版本无法核验');
      active = v;
      if (v.status != 'quarantine') {
        await _accept(v, e);
        return v.status == 'ready_private';
      }
      final url = Uri.parse(plan['uploadUrl'] as String),
          origin = Uri.parse(caps.uploadOrigin),
          expires = DateTime.parse(plan['expiresAt'] as String).toUtc();
      if (url.origin != origin.origin ||
          url.userInfo.isNotEmpty ||
          url.hasFragment ||
          !expires.isAfter(DateTime.now().toUtc()) ||
          expires.isAfter(v.uploadExpiresAt))
        throw const FormatException('视频上传目标已过期或不符合当前存储区域。');
      final h = (plan['uploadHeaders'] as Map<String, dynamic>).map(
        (k, v) => MapEntry(k, v as String),
      );
      if (h.length > 12 ||
          h.entries.any(
            (entry) =>
                entry.key.toLowerCase() == 'cookie' ||
                entry.key.toLowerCase() == 'host' ||
                (entry.key + entry.value).contains('\n') ||
                (entry.key + entry.value).contains('\r') ||
                entry.value.contains(_token!),
          ))
        throw const FormatException('视频上传目标无法核验');
      // The storage client receives only the bounded upload proof, never the
      // Birdtie session. Its actual PUT is separate from the API confirmation.
      final request = http.StreamedRequest('PUT', url)
        ..followRedirects = false
        ..contentLength = selected.bytes.length;
      request.headers.addAll(h);
      request.headers.putIfAbsent('Content-Type', () => 'video/mp4');
      final sent = _uploadClient.send(request);
      for (
        var offset = 0;
        offset < selected.bytes.length;
        offset += 64 * 1024
      ) {
        if (!_valid(e)) {
          await request.sink.close();
          unawaited(
            sent.catchError(
              (Object _) => http.StreamedResponse(const Stream.empty(), 499),
            ),
          );
          return false;
        }
        final end = min(offset + 64 * 1024, selected.bytes.length);
        request.sink.add(selected.bytes.sublist(offset, end));
        uploadedBytes = end;
        _notify();
      }
      await request.sink.close();
      final upload = await sent.timeout(const Duration(seconds: 30));
      var responseBytes = 0;
      await for (final chunk in upload.stream.timeout(
        const Duration(seconds: 5),
      )) {
        responseBytes += chunk.length;
        if (responseBytes > 16384) throw const FormatException('视频上传回执过大。');
      }
      if (!_valid(e)) return false;
      if (upload.statusCode != 200 &&
          upload.statusCode != 201 &&
          upload.statusCode != 204)
        throw const FormatException('视频未确认上传，保留原视频与恢复记录。');
      final completion = await _client
          .post(
            _uri('/${v.id}/complete'),
            headers: _headers,
            body: jsonEncode({'revision': v.revision}),
          )
          .timeout(const Duration(seconds: 15));
      _authorizationStatus(completion.statusCode);
      if (!_valid(e)) return false;
      if (completion.statusCode != 202)
        throw const FormatException('视频安全检查受理结果尚未确认。');
      await _accept(_receipt(_data(completion)), e);
      return active?.status == 'ready_private';
    } catch (failure) {
      if (_valid(e)) {
        uncertain = _pending != null;
        error = failure is FormatException
            ? failure.message.toString()
            : '视频保存结果尚未确认，请核实后重试。';
      }
      return false;
    } finally {
      if (_valid(e)) {
        uploading = false;
        _notify();
      }
    }
  }

  Future<void> _accept(VideoMaterialReceipt v, int epoch) async {
    if (!_valid(epoch)) return;
    active = v;
    if (_storedPreviewID == v.id && v.status != 'ready_private') {
      _previewExpiry?.cancel();
      _storedPreviewID = null;
      final old = selection;
      selection = null;
      if (old != null) await _disposeSelection(old);
      if (!_valid(epoch)) return;
    }
    items = List.unmodifiable([v, ...items.where((r) => r.id != v.id)]);
    uncertain = false;
    if (const ['ready_private', 'rejected', 'deleted'].contains(v.status) &&
        _pending?.operationID == v.operationID) {
      await _pendingStore.remove(_base, _owner!, v.operationID);
      if (!_valid(epoch)) return;
      _pending = null;
      pendingOperations = List.unmodifiable(
        pendingOperations.where((p) => p.operationID != v.operationID),
      );
    }
  }

  Future<void> recover({
    VideoMaterialPending? pending,
    VideoMaterialReceipt? receipt,
  }) async {
    if (!current || uploading) return;
    final e = _epoch;
    uploading = true;
    error = null;
    _notify();
    try {
      if (receipt != null) {
        if (receipt.ownerID != _owner ||
            (_pending != null && _pending!.operationID != receipt.operationID))
          throw const FormatException('请先核实当前未确认的视频。');
        active = receipt;
      }
      if (pending != null) {
        if (pending.ownerID != _owner) throw const FormatException();
        if (_pending?.operationID != pending.operationID) {
          final old = selection;
          selection = null;
          _previewExpiry?.cancel();
          _storedPreviewID = null;
          active = null;
          if (old != null) await _disposeSelection(old);
          if (!_valid(e)) return;
        }
        _pending = pending;
      }
      final op = _pending?.operationID;
      final target = op != null
          ? '/operations/$op/receipt'
          : active == null
          ? null
          : '/${active!.id}';
      if (target == null) return;
      final r = await _client
          .get(_uri(target), headers: _headers)
          .timeout(const Duration(seconds: 12));
      _authorizationStatus(r.statusCode);
      if (!_valid(e)) return;
      if (r.statusCode != 200) throw const FormatException('尚未取得视频回执，恢复记录仍保留。');
      final v = _receipt(_data(r));
      if (op != null &&
          (v.operationID != op ||
              v.sha256Hex != _pending!.input['sha256'] ||
              v.storagePolicyID != _pending!.input['storagePolicyId']))
        throw const FormatException('恢复回执与原视频不同。');
      await _accept(v, e);
    } catch (failure) {
      if (_valid(e)) {
        uncertain = _pending != null;
        error = failure is FormatException
            ? failure.message.toString()
            : '无法核实视频状态，请重试。';
      }
    } finally {
      if (_valid(e)) {
        uploading = false;
        _notify();
      }
    }
  }

  Future<void> load({bool more = false}) async {
    if (localSelectionOnly) return;
    if (!current || loading || (more && nextCursor == null)) return;
    final e = _epoch, serial = ++_reads;
    loading = true;
    error = null;
    _notify();
    try {
      final r = await _client
          .get(
            _uri('', query: {if (more) 'cursor': nextCursor!}),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      _authorizationStatus(r.statusCode);
      if (!_valid(e) || serial != _reads) return;
      if (r.statusCode != 200) throw const FormatException();
      final data = _data(r) as Map<String, dynamic>,
          rows = (data['items'] as List).map(_receipt).toList();
      if (rows.length > 20) throw const FormatException();
      final cursor = data['nextCursor'] as String?;
      if (cursor != null &&
          (!videoMaterialUUID.hasMatch(cursor) || cursor == nextCursor && more))
        throw const FormatException();
      items = List.unmodifiable(
        <String, VideoMaterialReceipt>{
          if (more)
            for (final v in items) v.id: v,
          for (final v in rows) v.id: v,
        }.values,
      );
      nextCursor = cursor;
    } catch (_) {
      if (_valid(e) && serial == _reads) error = '无法读取本人视频，保留已有内容，请重试。';
    } finally {
      if (_valid(e) && serial == _reads) {
        loading = false;
        _notify();
      }
    }
  }

  Future<void> delete(VideoMaterialReceipt v) async {
    if (!current || uploading || v.ownerID != _owner) return;
    if (_pending != null && _pending!.operationID != v.operationID) {
      error = '请先核实当前未确认的视频。';
      _notify();
      return;
    }
    final e = _epoch;
    uploading = true;
    error = null;
    _notify();
    try {
      final r = await _client
          .delete(
            _uri('/${v.id}', query: {'revision': '${v.revision}'}),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      _authorizationStatus(r.statusCode);
      if (!_valid(e)) return;
      if (r.statusCode != 202) throw const FormatException('删除结果尚未确认，请核实当前状态。');
      await _accept(_receipt(_data(r)), e);
    } catch (_) {
      if (_valid(e)) {
        active = v;
        error = '删除结果尚未确认，请核实当前状态。';
      }
    } finally {
      if (_valid(e)) {
        uploading = false;
        _notify();
      }
    }
  }

  Future<Uint8List?> readPrivate(VideoMaterialReceipt v) async {
    if (!current ||
        uploading ||
        v.ownerID != _owner ||
        v.status != 'ready_private')
      return null;
    final e = _epoch;
    uploading = true;
    error = null;
    _notify();
    try {
      final r = await _client
          .send(
            http.Request('GET', _uri('/${v.id}/content'))
              ..headers.addAll(_headers),
          )
          .timeout(const Duration(seconds: 30));
      _authorizationStatus(r.statusCode);
      if (!_valid(e)) return null;
      if (r.statusCode != 200 || r.contentLength != v.byteSize)
        throw const FormatException();
      final builder = BytesBuilder(copy: false);
      await for (final chunk in r.stream.timeout(const Duration(seconds: 15))) {
        if (!_valid(e)) return null;
        if (chunk.length > v.byteSize - builder.length)
          throw const FormatException();
        builder.add(chunk);
      }
      final bytes = builder.takeBytes();
      if (!_valid(e)) return null;
      if (bytes.length != v.byteSize ||
          bytes.length > videoMaterialMaxBytes ||
          sha256.convert(bytes).toString() != v.sha256Hex)
        throw const FormatException();
      return bytes;
    } catch (_) {
      if (_valid(e)) error = '无法读取当前本人视频，请重新核实。';
      return null;
    } finally {
      if (_valid(e)) {
        uploading = false;
        _notify();
      }
    }
  }

  Future<bool> openStoredPreview(VideoMaterialReceipt v) async {
    if (!current ||
        busy ||
        uncertain ||
        (_pending != null && _pending!.operationID != v.operationID))
      return false;
    final e = _epoch;
    final bytes = await readPrivate(v);
    if (bytes == null || !_valid(e)) return false;
    selecting = true;
    _notify();
    VideoMaterialSelection? next;
    try {
      next = await _picker.openPrivateBytes(bytes);
      if (!_valid(e)) {
        await _disposeSelection(next);
        return false;
      }
      final old = selection;
      if (!v.retainUntil.isAfter(DateTime.now().toUtc())) {
        await _disposeSelection(next);
        next = null;
        return false;
      }
      selection = next;
      next = null;
      active = v;
      _storedPreviewID = v.id;
      _previewExpiry?.cancel();
      _previewExpiry = Timer(
        v.retainUntil.difference(DateTime.now().toUtc()),
        () {
          if (!_valid(e) || _storedPreviewID != v.id) return;
          final expired = selection;
          selection = null;
          _storedPreviewID = null;
          if (expired != null)
            unawaited(expired.dispose().catchError((Object _) {}));
          error = '视频保留期限已到，请重新核实。';
          _notify();
        },
      );
      if (old != null) await _disposeSelection(old);
      return true;
    } catch (_) {
      if (next != null) await _disposeSelection(next);
      if (_valid(e)) error = '无法播放当前本人视频，请重新核实。';
      return false;
    } finally {
      if (_valid(e)) {
        selecting = false;
        _notify();
      }
    }
  }

  Future<void> cancelUpload() async {
    if (!current) return;
    final v = active;
    if (_ownsUpload) {
      _uploadClient.close();
      _uploadClient = http.Client();
    }
    _epoch++;
    uploading = false;
    uncertain = _pending != null;
    _notify();
    if (v != null)
      await delete(v);
    else if (_pending != null)
      await recover();
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    _epoch++;
    _selectSerial++;
    _previewExpiry?.cancel();
    identityChanges.removeListener(_observe);
    final old = selection;
    selection = null;
    if (old != null)
      unawaited(_disposeSelection(old).catchError((Object _) {}));
    if (_ownsClient) _client.close();
    if (_ownsUpload) _uploadClient.close();
    super.dispose();
  }
}
