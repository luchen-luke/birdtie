import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../content/moment_image_header.dart';

const entityCoverMaxVersion = 9007199254740991;
const entityCoverMaxInputBytes = 5 * 1024 * 1024;
const entityCoverMaxBytes = 256 * 1024;
bool entityCoverID(String id) => RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
).hasMatch(id);
bool _text(String s, int max, {bool empty = true}) =>
    (empty || s.isNotEmpty) &&
    s == s.trim() &&
    utf8.encode(s).length <= max &&
    !s.contains(RegExp(r'[\x00-\x1f\x7f-\x9f]'));
bool _hash(Object? v) => v is String && RegExp(r'^[0-9a-f]{64}$').hasMatch(v);
Map<String, dynamic> _object(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      raw.keys.any((k) => !keys.contains(k))) {
    throw const FormatException('封面来源格式不符');
  }
  return raw;
}

class EntityCoverRef {
  const EntityCoverRef(this.type, this.id);
  final String type, id;
  bool get valid =>
      const {'activity', 'organization'}.contains(type) && entityCoverID(id);
  String get path {
    if (!valid) throw const FormatException('封面对象不符');
    return '/v1/${type == 'activity' ? 'activities' : 'organizations'}/$id/cover';
  }

  @override
  bool operator ==(Object o) =>
      o is EntityCoverRef && type == o.type && id == o.id;
  @override
  int get hashCode => Object.hash(type, id);
}

class EntityCoverMetadata {
  const EntityCoverMetadata({
    required this.ref,
    required this.version,
    required this.path,
    required this.byteCount,
    required this.digest,
    required this.expiresAt,
    required this.attribution,
  });
  final EntityCoverRef ref;
  final int version, byteCount;
  final String path, digest, attribution;
  final DateTime expiresAt;
  bool get current => expiresAt.isAfter(DateTime.now().toUtc());
  Map<String, Object> toJson() => {
    'entityType': ref.type,
    'entityId': ref.id,
    'version': version,
    'thumbnailPath': path,
    'width': 320,
    'height': 180,
    'byteCount': byteCount,
    'sha256': digest,
    'expiresAt': expiresAt.toUtc().toIso8601String(),
    'attribution': attribution,
  };
  static EntityCoverMetadata? optional(Object? raw, EntityCoverRef ref) =>
      raw == null ? null : fromJson(raw, ref);
  static EntityCoverMetadata fromJson(Object? raw, EntityCoverRef ref) {
    final m = _object(raw, const {
      'entityType',
      'entityId',
      'version',
      'thumbnailPath',
      'width',
      'height',
      'byteCount',
      'sha256',
      'expiresAt',
      'attribution',
    });
    final now = DateTime.now().toUtc();
    final end = m['expiresAt'] is String
        ? DateTime.tryParse(m['expiresAt'])?.toUtc()
        : null;
    if (!ref.valid ||
        m['entityType'] != ref.type ||
        m['entityId'] != ref.id ||
        m['version'] is! int ||
        m['version'] < 1 ||
        m['version'] > entityCoverMaxVersion ||
        m['thumbnailPath'] != '${ref.path}/${m['version']}/content' ||
        m['width'] != 320 ||
        m['height'] != 180 ||
        m['byteCount'] is! int ||
        m['byteCount'] < 1 ||
        m['byteCount'] > entityCoverMaxBytes ||
        !_hash(m['sha256']) ||
        end == null ||
        !RegExp(r'Z$|[+-]\d\d:\d\d$').hasMatch(m['expiresAt']) ||
        !end.isAfter(now) ||
        end.isAfter(now.add(const Duration(seconds: 61))) ||
        m['attribution'] is! String ||
        !_text(m['attribution'], 320)) {
      throw const FormatException('封面对象、版本或可见期限不符');
    }
    return EntityCoverMetadata(
      ref: ref,
      version: m['version'],
      path: m['thumbnailPath'],
      byteCount: m['byteCount'],
      digest: m['sha256'],
      expiresAt: end,
      attribution: m['attribution'],
    );
  }

  bool sameSource(EntityCoverMetadata o) =>
      ref == o.ref &&
      version == o.version &&
      path == o.path &&
      byteCount == o.byteCount &&
      digest == o.digest &&
      attribution == o.attribution;
}

class EntityCoverState {
  const EntityCoverState(this.version, this.cover);
  final int version;
  final EntityCoverMetadata? cover;
  static EntityCoverState fromJson(Object? raw, EntityCoverRef ref) {
    final m = _object(raw, const {'version', 'cover'});
    if (m['version'] is! int ||
        m['version'] < 0 ||
        m['version'] > entityCoverMaxVersion) {
      throw const FormatException('封面版本不符');
    }
    final cover = EntityCoverMetadata.optional(m['cover'], ref);
    if (cover != null && cover.version != m['version'])
      throw const FormatException('封面版本不符');
    return EntityCoverState(m['version'], cover);
  }
}

class EntityCoverRights {
  const EntityCoverRights({
    required this.basis,
    required this.attribution,
    required this.licenseReference,
    required this.retentionUntil,
    required this.canRetain,
    required this.canPublish,
  });
  final String basis, attribution, licenseReference;
  final DateTime retentionUntil;
  final bool canRetain, canPublish;
  bool get valid =>
      const {'OWNED', 'LICENSED'}.contains(basis) &&
      _text(attribution, 320) &&
      _text(licenseReference, 1024, empty: basis == 'OWNED') &&
      canRetain &&
      canPublish &&
      retentionUntil.isAfter(DateTime.now().toUtc()) &&
      retentionUntil.year <= 9999;
  Map<String, Object> toJson() => {
    'basis': basis,
    'attribution': attribution,
    'licenseReference': licenseReference,
    'retentionUntil': _wireTime(retentionUntil),
    'canRetain': canRetain,
    'canPublish': canPublish,
  };
  // Match the server's canonical Rights struct order and RFC3339Nano UTC.
  String get digest => sha256
      .convert(
        utf8.encode(
          jsonEncode(toJson())
              .replaceAll('<', r'\u003c')
              .replaceAll('>', r'\u003e')
              .replaceAll('&', r'\u0026')
              .replaceAll('\u2028', r'\u2028')
              .replaceAll('\u2029', r'\u2029'),
        ),
      )
      .toString();
}

String _wireTime(DateTime t) => t
    .toUtc()
    .toIso8601String()
    .replaceFirst(RegExp(r'\.0+Z$'), 'Z')
    .replaceFirstMapped(RegExp(r'(\.\d*[1-9])0+Z$'), (m) => '${m[1]}Z');

class EntityCoverPreview {
  const EntityCoverPreview(
    this.token,
    this.expiresAt,
    this.inputDigest,
    this.rightsDigest,
    this.bytes,
    this.ref,
    this.operationID,
    this.expectedVersion,
    this.action,
  );
  final String token, inputDigest, rightsDigest;
  final DateTime expiresAt;
  final Uint8List? bytes;
  final EntityCoverRef ref;
  final String operationID, action;
  final int expectedVersion;
  static EntityCoverPreview fromJson(
    Object? raw,
    EntityCoverRef ref,
    String op,
    int expected,
    String action,
    EntityCoverRights? rights,
  ) {
    final keys = {
      'entityType',
      'entityId',
      'operationId',
      'expectedVersion',
      'action',
      'inputSha256',
      'rightsSha256',
      'expiresAt',
      'token',
      if (action == 'SAVE') 'previewDerivative',
    };
    final m = _object(raw, keys);
    final end = m['expiresAt'] is String
        ? DateTime.tryParse(m['expiresAt'])?.toUtc()
        : null;
    final now = DateTime.now().toUtc();
    if (m['entityType'] != ref.type ||
        m['entityId'] != ref.id ||
        m['operationId'] != op ||
        m['expectedVersion'] != expected ||
        m['action'] != action ||
        m['token'] is! String ||
        !RegExp(r'^ec1\.\d+\.\d+\.[0-9a-f]{64}$').hasMatch(m['token']) ||
        end == null ||
        !end.isAfter(now) ||
        end.isAfter(now.add(const Duration(seconds: 31))) ||
        m['rightsSha256'] != (rights?.digest ?? '') ||
        (action == 'SAVE'
            ? !_hash(m['inputSha256'])
            : m['inputSha256'] != '')) {
      throw const FormatException('封面确认来源或授权不符');
    }
    Uint8List? bytes;
    if (action == 'SAVE') {
      final d = _object(m['previewDerivative'], const {
        'contentType',
        'width',
        'height',
        'byteCount',
        'sha256',
        'contentBase64',
      });
      if (d['contentBase64'] is! String ||
          (d['contentBase64'] as String).length >
              4 * ((entityCoverMaxBytes + 2) ~/ 3)) {
        throw const FormatException('封面预览过大');
      }
      bytes = base64Decode(d['contentBase64']);
      if (base64Encode(bytes) != d['contentBase64'] ||
          d['contentType'] != 'image/jpeg' ||
          d['width'] != 320 ||
          d['height'] != 180 ||
          d['byteCount'] != bytes.length ||
          d['sha256'] != m['inputSha256'])
        throw const FormatException('封面预览不符');
      _validateJPEG(bytes, bytes.length, m['inputSha256']);
    }
    return EntityCoverPreview(
      m['token'],
      end,
      m['inputSha256'],
      m['rightsSha256'],
      bytes,
      ref,
      op,
      expected,
      action,
    );
  }
}

class EntityCoverReceipt {
  const EntityCoverReceipt(this.status, this.appliedVersion);
  final String status;
  final int appliedVersion;
  static EntityCoverReceipt fromJson(
    Object? raw,
    EntityCoverRef ref,
    String op,
    int expected,
    String action,
  ) {
    final m = _object(raw, const {
      'entityType',
      'entityId',
      'operationId',
      'action',
      'appliedVersion',
      'status',
    });
    if (m['entityType'] != ref.type ||
        m['entityId'] != ref.id ||
        m['operationId'] != op ||
        !((m['status'] == 'SUCCEEDED' &&
                m['action'] == action &&
                m['appliedVersion'] == expected + 1) ||
            (m['status'] == 'STOPPED' &&
                m['action'] == 'STOP' &&
                m['appliedVersion'] == 0))) {
      throw const FormatException('原封面操作结果不符');
    }
    return EntityCoverReceipt(m['status'], m['appliedVersion']);
  }
}

void _validateJPEG(Uint8List bytes, int count, String digest) {
  if (bytes.isEmpty ||
      bytes.length > entityCoverMaxBytes ||
      bytes.length != count ||
      sha256.convert(bytes).toString() != digest)
    throw const FormatException('封面内容校验失败');
  final header = MomentImageHeader.inspect(bytes);
  if (header.mime != 'image/jpeg' ||
      header.width != 320 ||
      header.height != 180) {
    throw const FormatException('封面尺寸或格式不符');
  }
}

class EntityCoverFailure implements Exception {
  const EntityCoverFailure(this.status);
  final int status;
  String get message => switch (status) {
    401 => '登录已失效，请重新登录。',
    403 => '你当前没有管理此封面的权限。',
    404 => '封面或原对象暂不可见。',
    409 => '对象或封面已变化，请重新读取。',
    400 => '图片或授权资料有误，请检查。',
    _ => '封面服务暂不可用，请重试读取。',
  };
}

/// Borrowed transport. Cover pixels never become Agent visual material.
class EntityCoverAPI {
  const EntityCoverAPI({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
    this.current,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  final bool Function()? current;
  Future<Uint8List> _request(
    String method,
    String path, {
    Map<String, Object>? body,
    int limit = 400000,
  }) async {
    final base = Uri.tryParse(baseUrl), token = authorizationHeader();
    if (base == null ||
        !const {'https', 'http'}.contains(base.scheme) ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty ||
        base.hasQuery ||
        base.hasFragment ||
        current?.call() == false ||
        (method != 'GET' && token == null))
      throw const EntityCoverFailure(401);
    final req = http.Request(
      method,
      Uri.parse('${baseUrl.replaceFirst(RegExp(r'/$'), '')}$path'),
    );
    req.followRedirects = false;
    if (req.url.origin != base.origin) throw const FormatException('封面来源不符');
    if (token != null) req.headers['Authorization'] = token;
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final response = await client
        .send(req)
        .timeout(const Duration(seconds: 12));
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (chunk.length > limit - bytes.length)
        throw const FormatException('封面响应过大');
      bytes.add(chunk);
    }
    if (authorizationHeader() != token || current?.call() == false)
      throw const EntityCoverFailure(401);
    if (response.statusCode != 200)
      throw EntityCoverFailure(response.statusCode);
    return bytes.takeBytes();
  }

  Future<Object?> _json(
    String method,
    String path, {
    Map<String, Object>? body,
  }) async {
    final raw = jsonDecode(
      utf8.decode(await _request(method, path, body: body)),
    );
    return _object(raw, const {'data'})['data'];
  }

  Future<EntityCoverState> state(EntityCoverRef ref) async =>
      EntityCoverState.fromJson(await _json('GET', ref.path), ref);
  Map<String, Object> input(
    String op,
    int expected,
    String action,
    Uint8List? bytes,
    EntityCoverRights? rights, {
    String? approval,
  }) {
    if (!entityCoverID(op) ||
        expected < 0 ||
        expected >= entityCoverMaxVersion ||
        !const {'SAVE', 'DELETE'}.contains(action) ||
        (action == 'SAVE'
            ? bytes == null || rights?.valid != true
            : bytes != null || rights != null))
      throw const FormatException('封面输入不符');
    if (bytes != null) {
      final h = MomentImageHeader.inspect(bytes);
      if (bytes.isEmpty ||
          bytes.length > entityCoverMaxInputBytes ||
          h.width > 4096 ||
          h.height > 4096 ||
          h.width * h.height > 16 * 1024 * 1024)
        throw const FormatException('请选择5 MiB以内、4096像素以内的静态图片。');
    }
    return {
      'operationId': op,
      'expectedVersion': expected,
      'action': action,
      if (bytes != null) ...{
        'contentType': MomentImageHeader.inspect(bytes).mime,
        'contentBase64': base64Encode(bytes),
        'rights': rights!.toJson(),
      },
      if (approval != null) 'approval': approval,
    };
  }

  Future<EntityCoverPreview> preview(
    EntityCoverRef ref,
    String op,
    int expected,
    String action,
    Uint8List? bytes,
    EntityCoverRights? rights,
  ) async => EntityCoverPreview.fromJson(
    await _json(
      'POST',
      '${ref.path}/preview',
      body: input(op, expected, action, bytes, rights),
    ),
    ref,
    op,
    expected,
    action,
    rights,
  );
  Future<EntityCoverReceipt> change(
    EntityCoverRef ref,
    String op,
    int expected,
    String action,
    Uint8List? bytes,
    EntityCoverRights? rights,
    EntityCoverPreview approval,
  ) async {
    if (approval.ref != ref ||
        approval.operationID != op ||
        approval.expectedVersion != expected ||
        approval.action != action ||
        approval.rightsDigest != (rights?.digest ?? '')) {
      throw const FormatException('封面确认与本次操作不符');
    }
    if (!approval.expiresAt.isAfter(DateTime.now().toUtc()))
      throw const FormatException('封面确认已过期');
    return EntityCoverReceipt.fromJson(
      await _json(
        'PUT',
        ref.path,
        body: input(
          op,
          expected,
          action,
          bytes,
          rights,
          approval: approval.token,
        ),
      ),
      ref,
      op,
      expected,
      action,
    );
  }

  void _selector(String op, int expected, String action) {
    if (!entityCoverID(op) ||
        expected < 0 ||
        expected >= entityCoverMaxVersion ||
        !const {'SAVE', 'DELETE'}.contains(action))
      throw const FormatException('原封面操作标识不符');
  }

  Future<EntityCoverReceipt> operation(
    EntityCoverRef ref,
    String op,
    int expected,
    String action,
  ) async {
    _selector(op, expected, action);
    return EntityCoverReceipt.fromJson(
      await _json('GET', '${ref.path}/operations/$op'),
      ref,
      op,
      expected,
      action,
    );
  }

  Future<EntityCoverReceipt> stop(
    EntityCoverRef ref,
    String op,
    int expected,
    String action,
  ) async {
    _selector(op, expected, action);
    return EntityCoverReceipt.fromJson(
      await _json(
        'POST',
        '${ref.path}/operations/$op/stop',
        body: {'expectedVersion': expected},
      ),
      ref,
      op,
      expected,
      action,
    );
  }

  Future<Uint8List> content(EntityCoverMetadata m) async {
    EntityCoverMetadata.fromJson(m.toJson(), m.ref);
    if (!m.current) throw const EntityCoverFailure(404);
    final bytes = await _request('GET', m.path, limit: entityCoverMaxBytes);
    if (!m.current) throw const EntityCoverFailure(404);
    _validateJPEG(bytes, m.byteCount, m.digest);
    return bytes;
  }
}

/// A rectangular cover with a bounded source lease; absent pixels occupy no area.
class EntityCover extends StatefulWidget {
  const EntityCover({
    super.key,
    required this.metadata,
    required this.api,
    required this.current,
    this.identityChanges,
    this.width,
    this.height,
    this.label = '封面',
    this.showAttribution = true,
  });
  final EntityCoverMetadata? metadata;
  final EntityCoverAPI api;
  final bool Function() current;
  final Listenable? identityChanges;
  final double? width, height;
  final String label;
  final bool showAttribution;
  @override
  State<EntityCover> createState() => _EntityCoverState();
}

class _EntityCoverState extends State<EntityCover> {
  MemoryImage? _image;
  int _epoch = 0;
  Timer? _expiry;
  Object? _identity;
  EntityCoverMetadata? _seen;
  bool _retired = false, _identityRetired = false;
  Object get _binding => (
    widget.api.client,
    widget.api.baseUrl,
    widget.api.authorizationHeader(),
    widget.current(),
  );
  void _drop() {
    final old = _image;
    _image = null;
    if (old != null) unawaited(old.evict());
  }

  void _observe() {
    if (_identity != _binding || !widget.current()) {
      _identityRetired = true;
      _retired = true;
      ++_epoch;
      _expiry?.cancel();
      _drop();
      if (mounted) setState(() {});
    }
  }

  @override
  void initState() {
    super.initState();
    widget.identityChanges?.addListener(_observe);
    _load();
  }

  void _load() {
    final epoch = ++_epoch, binding = _binding, metadata = widget.metadata;
    _identity = binding;
    _seen = metadata;
    _expiry?.cancel();
    _drop();
    if (_identityRetired ||
        _retired ||
        metadata == null ||
        !metadata.current ||
        !widget.current())
      return;
    _expiry = Timer(metadata.expiresAt.difference(DateTime.now().toUtc()), () {
      if (mounted && epoch == _epoch) {
        ++_epoch;
        _retired = true;
        _drop();
        setState(() {});
      }
    });
    unawaited(() async {
      try {
        final bytes = await widget.api.content(metadata);
        if (mounted &&
            epoch == _epoch &&
            binding == _binding &&
            !_retired &&
            metadata.current &&
            widget.current())
          setState(() => _image = MemoryImage(bytes));
      } catch (_) {
        /* No placeholder masquerades as an available cover. */
      }
    }());
  }

  @override
  void didUpdateWidget(EntityCover old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_observe);
      widget.identityChanges?.addListener(_observe);
    }
    if (_identity != _binding) {
      _observe();
      return;
    }
    if (_identityRetired) return;
    final next = widget.metadata, prior = _seen;
    if (prior == null || next == null || !prior.sameSource(next)) {
      _retired = false;
      _load();
    } else if (next.expiresAt.isBefore(prior.expiresAt)) {
      _seen = next;
      _load();
    }
  }

  @override
  void dispose() {
    ++_epoch;
    _expiry?.cancel();
    widget.identityChanges?.removeListener(_observe);
    _drop();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_image == null || _retired || !widget.current())
      return const SizedBox.shrink();
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        ClipRRect(
          borderRadius: BorderRadius.circular(8),
          child: Image(
            image: _image!,
            width: widget.width,
            height: widget.height,
            fit: BoxFit.cover,
            semanticLabel: widget.label,
            errorBuilder: (_, __, ___) => const SizedBox.shrink(),
          ),
        ),
        if (widget.showAttribution && _seen?.attribution.isNotEmpty == true)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(
              _seen!.attribution,
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ),
      ],
    );
  }
}
