import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../content/moment_image_header.dart';
import 'person_avatar.dart'
    show
        personAvatarID,
        personAvatarMaxInputBytes,
        personAvatarMaxThumbnailBytes;

class CommunityAvatarMetadata {
  const CommunityAvatarMetadata({
    required this.communityID,
    required this.version,
    required this.path,
    required this.byteCount,
    required this.digest,
  });
  final String communityID, path, digest;
  final int version, byteCount;
  static CommunityAvatarMetadata? optional(Object? raw, String id) =>
      raw == null ? null : fromJson(raw, id);
  static CommunityAvatarMetadata fromJson(Object? raw, String id) {
    const keys = {
      'communityId',
      'version',
      'thumbnailPath',
      'width',
      'height',
      'byteCount',
      'sha256',
    };
    if (raw is! Map<String, dynamic> ||
        raw.length != keys.length ||
        raw.keys.any((k) => !keys.contains(k)) ||
        !personAvatarID(id) ||
        raw['communityId'] != id ||
        raw['version'] is! int ||
        raw['version'] < 1 ||
        raw['version'] > 9007199254740991 ||
        raw['thumbnailPath'] !=
            '/v1/communities/$id/avatar/${raw['version']}/content' ||
        raw['width'] != 128 ||
        raw['height'] != 128 ||
        raw['byteCount'] is! int ||
        raw['byteCount'] < 1 ||
        raw['byteCount'] > personAvatarMaxThumbnailBytes ||
        raw['sha256'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(raw['sha256']))
      throw const FormatException('社群头像来源不符');
    return CommunityAvatarMetadata(
      communityID: id,
      version: raw['version'],
      path: raw['thumbnailPath'],
      byteCount: raw['byteCount'],
      digest: raw['sha256'],
    );
  }

  bool same(CommunityAvatarMetadata other) =>
      communityID == other.communityID &&
      version == other.version &&
      path == other.path &&
      byteCount == other.byteCount &&
      digest == other.digest;
}

class CommunityAvatarState {
  const CommunityAvatarState(this.version, this.avatar);
  final int version;
  final CommunityAvatarMetadata? avatar;
  static CommunityAvatarState fromJson(Object? raw, String id) {
    if (raw is! Map<String, dynamic> ||
        raw.length != 2 ||
        raw['version'] is! int ||
        raw['version'] < 0 ||
        raw['version'] > 9007199254740991 ||
        !raw.containsKey('avatar'))
      throw const FormatException('社群头像状态不符');
    final a = CommunityAvatarMetadata.optional(raw['avatar'], id);
    if (a != null && a.version != raw['version'])
      throw const FormatException('社群头像版本不符');
    return CommunityAvatarState(raw['version'], a);
  }
}

class CommunityAvatarPending {
  const CommunityAvatarPending(
    this.operationID,
    this.version,
    this.action, {
    this.stage = 'UNKNOWN',
  });
  final String operationID, action, stage;
  final int version;
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'version': version,
    'action': action,
    'stage': stage,
  };
  static CommunityAvatarPending fromJson(Object? raw) {
    if (raw is! Map<String, dynamic> ||
        raw.length != 4 ||
        !personAvatarID(
          raw['operationId'] is String ? raw['operationId'] : '',
        ) ||
        raw['version'] is! int ||
        raw['version'] < 0 ||
        raw['version'] >= 9007199254740991 ||
        !const {'SAVE', 'DELETE'}.contains(raw['action']) ||
        !const {'PREPARED', 'UNKNOWN'}.contains(raw['stage']))
      throw const FormatException('社群头像恢复记录不符');
    return CommunityAvatarPending(
      raw['operationId'],
      raw['version'],
      raw['action'],
      stage: raw['stage'],
    );
  }

  bool same(CommunityAvatarPending o) =>
      operationID == o.operationID &&
      version == o.version &&
      action == o.action;
}

class CommunityAvatarApproval {
  const CommunityAvatarApproval(this.token, this.expiresAt);
  final String token;
  final DateTime expiresAt;
  static CommunityAvatarApproval fromJson(
    Object? raw,
    String id,
    CommunityAvatarPending p,
  ) {
    if (raw is! Map<String, dynamic> ||
        raw['communityId'] != id ||
        raw['operationId'] != p.operationID ||
        raw['expectedVersion'] != p.version ||
        raw['action'] != p.action ||
        raw['token'] is! String ||
        !RegExp(r'^ca1\.\d+\.\d+\.[0-9a-f]{64}$').hasMatch(raw['token']) ||
        raw['expiresAt'] is! String ||
        raw['inputSha256'] is! String ||
        (p.action == 'SAVE' &&
            !RegExp(r'^[0-9a-f]{64}$').hasMatch(raw['inputSha256'])) ||
        (p.action == 'DELETE' && raw['inputSha256'] != ''))
      throw const FormatException('社群头像确认不符');
    final end = DateTime.parse(raw['expiresAt']).toUtc();
    if (!end.isAfter(DateTime.now().toUtc()) ||
        end.isAfter(DateTime.now().toUtc().add(const Duration(seconds: 31))))
      throw const FormatException('社群头像确认已过期');
    return CommunityAvatarApproval(raw['token'], end);
  }
}

class CommunityAvatarResult {
  const CommunityAvatarResult(this.appliedVersion, this.current);
  final int appliedVersion;
  final CommunityAvatarState current;
  static CommunityAvatarResult fromJson(
    Object? raw,
    String id,
    CommunityAvatarPending p,
  ) {
    if (raw is! Map<String, dynamic> ||
        raw['communityId'] != id ||
        raw['operationId'] != p.operationID ||
        raw['action'] != p.action ||
        raw['appliedVersion'] != p.version + 1)
      throw const FormatException('社群头像操作结果不符');
    final state = CommunityAvatarState.fromJson(raw['current'], id);
    if (state.version < p.version + 1)
      throw const FormatException('社群头像结果版本不符');
    return CommunityAvatarResult(p.version + 1, state);
  }
}

class CommunityAvatarFailure implements Exception {
  const CommunityAvatarFailure(this.status);
  final int status;
  String get message => switch (status) {
    401 => '登录已失效，请重新登录。',
    403 => '你当前没有管理此社群头像的权限。',
    404 => '社群或头像暂不可见。',
    409 => '社群状态已变化，请重新检查。',
    400 => '请选择有效的静态图片。',
    _ => '头像暂不可用，请稍后重试。',
  };
}

class CommunityAvatarAPI {
  const CommunityAvatarAPI({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  Future<Uint8List> _request(
    String method,
    String path, {
    Map<String, Object>? body,
    int limit = 200000,
  }) async {
    final token = authorizationHeader();
    if (baseUrl.isEmpty || token == null)
      throw const CommunityAvatarFailure(401);
    final req = http.Request(
      method,
      Uri.parse('${baseUrl.replaceFirst(RegExp(r'/$'), '')}$path'),
    )..headers['Authorization'] = token;
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
        throw const FormatException('头像响应超出限制');
      bytes.add(chunk);
    }
    if (token != authorizationHeader()) throw const CommunityAvatarFailure(401);
    if (response.statusCode != 200)
      throw CommunityAvatarFailure(response.statusCode);
    return bytes.takeBytes();
  }

  Future<Object?> _json(
    String method,
    String path, {
    Map<String, Object>? body,
  }) async =>
      (jsonDecode(utf8.decode(await _request(method, path, body: body)))
          as Map<String, dynamic>)['data'];
  String _path(String id) {
    if (!personAvatarID(id)) throw const FormatException('社群标识不符');
    return '/v1/communities/$id/avatar';
  }

  Future<CommunityAvatarState> state(String id) async =>
      CommunityAvatarState.fromJson(await _json('GET', _path(id)), id);
  Map<String, Object> _input(
    CommunityAvatarPending p,
    Uint8List? bytes, {
    String? approval,
  }) {
    CommunityAvatarPending.fromJson(p.toJson());
    if ((p.action == 'DELETE' && bytes != null) ||
        (p.action == 'SAVE' && bytes == null))
      throw const FormatException('头像输入与操作不符');
    if (bytes != null &&
        (bytes.isEmpty || bytes.length > personAvatarMaxInputBytes))
      throw const FormatException('头像超过大小限制');
    return {
      'operationId': p.operationID,
      'expectedVersion': p.version,
      'action': p.action,
      if (p.action == 'SAVE') ...{
        'contentType': MomentImageHeader.inspect(bytes!).mime,
        'contentBase64': base64Encode(bytes),
      },
      if (approval != null) 'approval': approval,
    };
  }

  Future<CommunityAvatarApproval> preview(
    String id,
    CommunityAvatarPending p,
    Uint8List? bytes,
  ) async => CommunityAvatarApproval.fromJson(
    await _json('POST', '${_path(id)}/preview', body: _input(p, bytes)),
    id,
    p,
  );
  Future<CommunityAvatarResult> change(
    String id,
    CommunityAvatarPending p,
    Uint8List? bytes,
    CommunityAvatarApproval approval,
  ) async => CommunityAvatarResult.fromJson(
    await _json(
      'PUT',
      _path(id),
      body: _input(p, bytes, approval: approval.token),
    ),
    id,
    p,
  );
  Future<CommunityAvatarResult> operation(
    String id,
    CommunityAvatarPending p,
  ) async => CommunityAvatarResult.fromJson(
    await _json('GET', '${_path(id)}/operations/${p.operationID}'),
    id,
    p,
  );
  Future<Uint8List> content(CommunityAvatarMetadata m) async {
    CommunityAvatarMetadata.fromJson({
      'communityId': m.communityID,
      'version': m.version,
      'thumbnailPath': m.path,
      'width': 128,
      'height': 128,
      'byteCount': m.byteCount,
      'sha256': m.digest,
    }, m.communityID);
    final data = await _request(
      'GET',
      m.path,
      limit: personAvatarMaxThumbnailBytes,
    );
    final header = MomentImageHeader.inspect(data);
    if (data.length != m.byteCount ||
        sha256.convert(data).toString() != m.digest ||
        header.mime != 'image/jpeg' ||
        header.width != 128 ||
        header.height != 128)
      throw const FormatException('社群头像缩略图不符');
    return data;
  }
}

class CommunityAvatar extends StatefulWidget {
  const CommunityAvatar({
    super.key,
    required this.metadata,
    required this.fallbackLabel,
    required this.api,
    required this.identityChanges,
    required this.current,
    this.radius = 22,
  });
  final CommunityAvatarMetadata? metadata;
  final String fallbackLabel;
  final CommunityAvatarAPI api;
  final Listenable identityChanges;
  final bool Function() current;
  final double radius;
  @override
  State<CommunityAvatar> createState() => _CommunityAvatarState();
}

class _CommunityAvatarState extends State<CommunityAvatar> {
  MemoryImage? _image;
  int _serial = 0;
  Object? _seen;
  Object get _binding => (
    widget.api.authorizationHeader(),
    widget.api.baseUrl,
    widget.api.client,
    widget.metadata?.communityID,
    widget.metadata?.version,
    widget.metadata?.path,
    widget.metadata?.byteCount,
    widget.metadata?.digest,
    widget.current(),
  );
  @override
  void initState() {
    super.initState();
    widget.identityChanges.addListener(_changed);
    _load();
  }

  void _drop() {
    final old = _image;
    _image = null;
    if (old != null) unawaited(old.evict());
  }

  void _changed() {
    if (_seen != _binding) _load();
  }

  void _load() {
    final serial = ++_serial, binding = _binding;
    _seen = binding;
    _drop();
    if (mounted) setState(() {});
    final meta = widget.metadata;
    if (meta == null || !widget.current()) return;
    unawaited(() async {
      try {
        final bytes = await widget.api.content(meta);
        if (mounted &&
            serial == _serial &&
            binding == _binding &&
            widget.current())
          setState(() => _image = MemoryImage(bytes));
      } catch (_) {
        /* Honest fixed-size fallback, no external image fetch. */
      }
    }());
  }

  @override
  void didUpdateWidget(CommunityAvatar old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges.removeListener(_changed);
      widget.identityChanges.addListener(_changed);
    }
    if (_seen != _binding) _load();
  }

  @override
  void dispose() {
    ++_serial;
    widget.identityChanges.removeListener(_changed);
    _drop();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ExcludeSemantics(
    child: CircleAvatar(
      radius: widget.radius,
      backgroundColor: Theme.of(context).colorScheme.surfaceContainerLow,
      child: _image == null
          ? Text(
              widget.fallbackLabel.isEmpty
                  ? '社'
                  : widget.fallbackLabel.characters.first,
            )
          : ClipOval(
              child: Image(
                image: _image!,
                width: widget.radius * 2,
                height: widget.radius * 2,
                fit: BoxFit.cover,
                errorBuilder: (_, __, ___) => const Icon(Icons.groups_outlined),
              ),
            ),
    ),
  );
}
