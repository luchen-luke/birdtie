import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../content/moment_image_header.dart';

const personAvatarMaxInputBytes = 5 * 1024 * 1024;
const personAvatarMaxThumbnailBytes = 128 * 1024;
final _avatarID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
bool personAvatarID(String id) =>
    _avatarID.hasMatch(id) && id != '00000000-0000-0000-0000-000000000000';

class PersonAvatarMetadata {
  const PersonAvatarMetadata({
    required this.accountID,
    required this.version,
    required this.thumbnailPath,
    required this.byteCount,
    required this.digest,
  });
  final String accountID, thumbnailPath, digest;
  final int version, byteCount;
  static PersonAvatarMetadata? optional(Object? raw, String accountID) =>
      raw == null ? null : fromJson(raw, accountID);
  static PersonAvatarMetadata fromJson(Object? raw, String accountID) {
    const keys = {
      'accountId',
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
        !personAvatarID(accountID) ||
        raw['accountId'] != accountID ||
        raw['version'] is! int ||
        (raw['version'] as int) < 1 ||
        raw['width'] != 128 ||
        raw['height'] != 128 ||
        raw['byteCount'] is! int ||
        (raw['byteCount'] as int) < 1 ||
        (raw['byteCount'] as int) > personAvatarMaxThumbnailBytes ||
        raw['sha256'] is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(raw['sha256'] as String) ||
        raw['thumbnailPath'] !=
            '/v1/accounts/$accountID/avatar/${raw['version']}/content') {
      throw const FormatException('头像来源无法核实');
    }
    return PersonAvatarMetadata(
      accountID: accountID,
      version: raw['version'] as int,
      thumbnailPath: raw['thumbnailPath'] as String,
      byteCount: raw['byteCount'] as int,
      digest: raw['sha256'] as String,
    );
  }
}

class PersonAvatarState {
  const PersonAvatarState(this.version, this.avatar);
  final int version;
  final PersonAvatarMetadata? avatar;
  static PersonAvatarState fromJson(Object? raw, String owner) {
    if (raw is! Map<String, dynamic> ||
        raw.length != 2 ||
        !raw.containsKey('avatar') ||
        raw['version'] is! int ||
        (raw['version'] as int) < 0) {
      throw const FormatException('当前头像版本无法核实');
    }
    final avatar = PersonAvatarMetadata.optional(raw['avatar'], owner);
    if (avatar != null && avatar.version != raw['version'])
      throw const FormatException('头像版本不一致');
    return PersonAvatarState(raw['version'] as int, avatar);
  }
}

class PersonAvatarResult {
  const PersonAvatarResult(
    this.operationID,
    this.action,
    this.appliedVersion,
    this.current,
  );
  final String operationID, action;
  final int appliedVersion;
  final PersonAvatarState current;
  static PersonAvatarResult fromJson(
    Object? raw,
    String owner,
    String op, {
    String? action,
    int? expectedVersion,
  }) {
    if (raw is! Map<String, dynamic> ||
        raw.length != 4 ||
        raw['operationId'] != op ||
        !personAvatarID(op) ||
        !const {'SAVE', 'DELETE'}.contains(raw['action']) ||
        raw['appliedVersion'] is! int ||
        (raw['appliedVersion'] as int) < 1 ||
        action != null && raw['action'] != action ||
        expectedVersion != null &&
            raw['appliedVersion'] != expectedVersion + 1) {
      throw const FormatException('头像保存回执无法核实');
    }
    final current = PersonAvatarState.fromJson(raw['current'], owner);
    if (current.version < (raw['appliedVersion'] as int))
      throw const FormatException('头像保存版本无法核实');
    return PersonAvatarResult(
      op,
      raw['action'] as String,
      raw['appliedVersion'] as int,
      current,
    );
  }
}

class PersonAvatarFailure implements Exception {
  const PersonAvatarFailure(this.status);
  final int status;
  String get message => switch (status) {
    401 => '登录已失效，请重新登录。',
    403 => '请切换到个人身份后修改头像。',
    409 => '头像已在其他操作中变化，请刷新后重新选择。',
    400 => '仅支持 5 MiB 以内的静态 JPEG 或 PNG 图片。',
    404 => '当前头像或操作暂不可读取。',
    _ => '头像服务暂不可用，请稍后重试。',
  };
}

class PersonAvatarAPI {
  const PersonAvatarAPI({
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
    Object? body,
    int limit = 64 * 1024,
  }) async {
    final token = authorizationHeader();
    final request = http.Request(
      method,
      Uri.parse('${baseUrl.replaceFirst(RegExp(r'/$'), '')}$path'),
    );
    if (token != null) request.headers['Authorization'] = token;
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }
    final response = await client
        .send(request)
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      await response.stream.drain<void>().timeout(const Duration(seconds: 12));
      throw PersonAvatarFailure(response.statusCode);
    }
    if (response.contentLength != null && response.contentLength! > limit)
      throw const FormatException('头像响应超过大小限制');
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (chunk.length > limit - bytes.length)
        throw const FormatException('头像响应超过大小限制');
      bytes.add(chunk);
    }
    if (authorizationHeader() != token) throw StateError('头像身份已变化');
    return bytes.takeBytes();
  }

  Future<Object?> _json(String method, String path, {Object? body}) async {
    final root = jsonDecode(
      utf8.decode(await _request(method, path, body: body)),
    );
    if (root is! Map<String, dynamic> ||
        root.length != 1 ||
        !root.containsKey('data'))
      throw const FormatException('头像响应来源不符');
    return root['data'];
  }

  Future<PersonAvatarState> own(String owner) async =>
      PersonAvatarState.fromJson(await _json('GET', '/v1/me/avatar'), owner);
  Future<PersonAvatarResult> change(
    String owner,
    String op,
    int version, {
    Uint8List? bytes,
  }) async {
    final deleting = bytes == null;
    if (!personAvatarID(owner) ||
        !personAvatarID(op) ||
        version < 0 ||
        bytes != null &&
            (bytes.isEmpty || bytes.length > personAvatarMaxInputBytes))
      throw const FormatException('头像变更来源不符');
    final data = await _json(
      deleting ? 'DELETE' : 'PUT',
      '/v1/me/avatar',
      body: {
        'operationId': op,
        'expectedVersion': version,
        if (bytes != null) 'contentType': MomentImageHeader.inspect(bytes).mime,
        if (bytes != null) 'contentBase64': base64Encode(bytes),
      },
    );
    return PersonAvatarResult.fromJson(
      data,
      owner,
      op,
      action: deleting ? 'DELETE' : 'SAVE',
      expectedVersion: version,
    );
  }

  Future<PersonAvatarResult> operation(
    String owner,
    String op, {
    String? action,
    int? expectedVersion,
  }) async => PersonAvatarResult.fromJson(
    await _json('GET', '/v1/me/avatar/operations/$op'),
    owner,
    op,
    action: action,
    expectedVersion: expectedVersion,
  );
  Future<Uint8List> content(PersonAvatarMetadata metadata) async {
    final bytes = await _request(
      'GET',
      metadata.thumbnailPath,
      limit: personAvatarMaxThumbnailBytes,
    );
    final header = MomentImageHeader.inspect(bytes);
    if (bytes.length != metadata.byteCount ||
        sha256.convert(bytes).toString() != metadata.digest ||
        header.mime != 'image/jpeg' ||
        header.width != 128 ||
        header.height != 128)
      throw const FormatException('头像缩略图无法核实');
    return bytes;
  }
}

/// No shared disk/network image cache. A retired identity drops and evicts its
/// image immediately, and late reads can never attach to the next identity.
class PersonAvatar extends StatefulWidget {
  const PersonAvatar({
    super.key,
    required this.metadata,
    required this.authorizationHeader,
    required this.baseUrl,
    this.client,
    this.identityChanges,
    this.current,
    this.radius = 18,
  });
  final PersonAvatarMetadata? metadata;
  final String? Function() authorizationHeader;
  final String baseUrl;
  final http.Client? client;
  final Listenable? identityChanges;
  final bool Function()? current;
  final double radius;
  @override
  State<PersonAvatar> createState() => _PersonAvatarState();
}

class _PersonAvatarState extends State<PersonAvatar> {
  late http.Client _client;
  late bool _ownsClient;
  MemoryImage? _image;
  int _serial = 0;
  Object? _binding;
  Object get _currentBinding => (
    widget.authorizationHeader(),
    widget.baseUrl,
    widget.metadata?.thumbnailPath,
    widget.client,
    widget.current?.call(),
  );
  @override
  void initState() {
    super.initState();
    _client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    widget.identityChanges?.addListener(_changed);
    _load();
  }

  void _drop() {
    final image = _image;
    _image = null;
    if (image != null) unawaited(image.evict());
  }

  void _changed() {
    if (_binding == _currentBinding) return;
    _load();
  }

  @override
  void didUpdateWidget(PersonAvatar old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_changed);
      widget.identityChanges?.addListener(_changed);
    }
    if (old.client != widget.client) {
      if (_ownsClient) _client.close();
      _client = widget.client ?? http.Client();
      _ownsClient = widget.client == null;
    }
    if (_binding != _currentBinding ||
        old.metadata?.digest != widget.metadata?.digest)
      _load();
  }

  void _load() {
    final serial = ++_serial, binding = _currentBinding;
    _binding = binding;
    _drop();
    if (mounted) setState(() {});
    final metadata = widget.metadata;
    if (metadata == null ||
        widget.baseUrl.isEmpty ||
        widget.current?.call() == false)
      return;
    unawaited(() async {
      try {
        final bytes = await PersonAvatarAPI(
          client: _client,
          baseUrl: widget.baseUrl,
          authorizationHeader: widget.authorizationHeader,
        ).content(metadata);
        if (!mounted ||
            serial != _serial ||
            binding != _currentBinding ||
            widget.current?.call() == false)
          return;
        setState(() => _image = MemoryImage(bytes));
      } catch (_) {
        /* Unavailable/deleted/restricted picture keeps the placeholder. */
      }
    }());
  }

  @override
  void dispose() {
    ++_serial;
    widget.identityChanges?.removeListener(_changed);
    _drop();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => ExcludeSemantics(
    child: CircleAvatar(
      radius: widget.radius,
      backgroundColor: Theme.of(context).colorScheme.surfaceContainerLow,
      child: _image == null
          ? const Icon(Icons.person_outline, size: 20)
          : ClipOval(
              child: Image(
                image: _image!,
                width: widget.radius * 2,
                height: widget.radius * 2,
                fit: BoxFit.cover,
                errorBuilder: (_, __, ___) =>
                    const Icon(Icons.person_outline, size: 20),
              ),
            ),
    ),
  );
}
