import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;

import '../content/moment_image_header.dart';
import 'entity_cover.dart';

/// Place editorial authority is separate from activity/organization covers.
class PlacePhotoRef extends EntityCoverRef {
  const PlacePhotoRef(String id) : super('place', id);
  @override
  bool get valid =>
      entityCoverID(id) && id != '00000000-0000-0000-0000-000000000000';
  @override
  String get path => '/v1/places/$id/photos';
}

Map<String, dynamic> _object(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      !raw.keys.every(keys.contains)) {
    throw const FormatException('地点照片资料结构不符');
  }
  return raw;
}

bool _version(Object? n, {bool zero = true}) =>
    n is int && n >= (zero ? 0 : 1) && n <= entityCoverMaxVersion;
bool _hash(Object? s) => s is String && RegExp(r'^[0-9a-f]{64}$').hasMatch(s);
bool _text(Object? s, int limit) =>
    s is String &&
    utf8.encode(s).length <= limit &&
    s == s.trim() &&
    !RegExp(r'[\x00-\x1f\x7f-\x9f]').hasMatch(s);
DateTime _time(Object? raw) {
  if (raw is! String || !RegExp(r'Z$|[+-]\d\d:\d\d$').hasMatch(raw)) {
    throw const FormatException('地点照片期限不符');
  }
  final t = DateTime.tryParse(raw)?.toUtc();
  if (t == null) throw const FormatException('地点照片期限不符');
  return t;
}

EntityCoverRights _rights(Object? raw) {
  final m = _object(raw, const {
    'basis',
    'attribution',
    'licenseReference',
    'retentionUntil',
    'canRetain',
    'canPublish',
  });
  if (!const {'OWNED', 'LICENSED'}.contains(m['basis']) ||
      !_text(m['attribution'], 320) ||
      !_text(m['licenseReference'], 1024) ||
      (m['basis'] == 'LICENSED' && m['licenseReference'] == '') ||
      m['canRetain'] != true ||
      m['canPublish'] != true) {
    throw const FormatException('地点照片权利资料不符');
  }
  return EntityCoverRights(
    basis: m['basis'],
    attribution: m['attribution'],
    licenseReference: m['licenseReference'],
    retentionUntil: _time(m['retentionUntil']),
    canRetain: true,
    canPublish: true,
  );
}

class PlacePhotoState {
  const PlacePhotoState(this.version, this.placePhoto);
  final int version;
  final EntityCoverMetadata? placePhoto;
  static PlacePhotoState fromJson(Object? raw, PlacePhotoRef ref) {
    final m = _object(raw, const {'version', 'placePhoto'});
    if (!_version(m['version'])) throw const FormatException('地点照片版本不符');
    final photo = EntityCoverMetadata.optional(m['placePhoto'], ref);
    if (photo != null && photo.version != m['version']) {
      throw const FormatException('地点照片版本不符');
    }
    return PlacePhotoState(m['version'], photo);
  }
}

class PlacePhotoThumbnail {
  const PlacePhotoThumbnail(this.byteCount, this.digest);
  final int byteCount;
  final String digest;
  static PlacePhotoThumbnail fromJson(Object? raw) {
    final m = _object(raw, const {
      'contentType',
      'width',
      'height',
      'byteCount',
      'sha256',
    });
    if (m['contentType'] != 'image/jpeg' ||
        m['width'] != 320 ||
        m['height'] != 180 ||
        m['byteCount'] is! int ||
        m['byteCount'] < 1 ||
        m['byteCount'] > entityCoverMaxBytes ||
        !_hash(m['sha256'])) {
      throw const FormatException('地点照片缩略图不符');
    }
    return PlacePhotoThumbnail(m['byteCount'], m['sha256']);
  }
}

class PlacePhotoCandidate {
  const PlacePhotoCandidate({
    required this.id,
    required this.version,
    required this.status,
    required this.expectedVersion,
    required this.rights,
    required this.thumbnail,
    required this.canApprove,
    required this.canReject,
    required this.canWithdraw,
  });
  final String id, status;
  final int version, expectedVersion;
  final EntityCoverRights rights;
  final PlacePhotoThumbnail? thumbnail;
  final bool canApprove, canReject, canWithdraw;
  static PlacePhotoCandidate fromJson(Object? raw) {
    final m = _object(raw, const {
      'id',
      'version',
      'status',
      'expectedVersion',
      'rights',
      'thumbnail',
      'canApprove',
      'canReject',
      'canWithdraw',
    });
    if (!entityCoverID(m['id'] is String ? m['id'] : '') ||
        !_version(m['version'], zero: false) ||
        !_version(m['expectedVersion']) ||
        !const {
          'PENDING',
          'APPROVED',
          'REJECTED',
          'WITHDRAWN',
          'EXPIRED',
        }.contains(m['status']) ||
        m['canApprove'] is! bool ||
        m['canReject'] is! bool ||
        m['canWithdraw'] is! bool) {
      throw const FormatException('地点照片候选不符');
    }
    final thumbnail = m['thumbnail'] == null
        ? null
        : PlacePhotoThumbnail.fromJson(m['thumbnail']);
    if ((m['canApprove'] || m['canReject']) &&
        (m['status'] != 'PENDING' || thumbnail == null)) {
      throw const FormatException('地点照片审核状态不符');
    }
    return PlacePhotoCandidate(
      id: m['id'],
      version: m['version'],
      status: m['status'],
      expectedVersion: m['expectedVersion'],
      rights: _rights(m['rights']),
      thumbnail: thumbnail,
      canApprove: m['canApprove'],
      canReject: m['canReject'],
      canWithdraw: m['canWithdraw'],
    );
  }

  String get statusLabel => switch (status) {
    'PENDING' => '等待独立审核',
    'APPROVED' => '已批准',
    'REJECTED' => '已拒绝',
    'WITHDRAWN' => '已撤回',
    _ => '已到期',
  };
}

class PlacePhotoEditorial {
  const PlacePhotoEditorial({
    required this.ref,
    required this.cityID,
    required this.version,
    required this.canContribute,
    required this.canReview,
    required this.observedAt,
    required this.expiresAt,
    required this.candidates,
  });
  final PlacePhotoRef ref;
  final String cityID;
  final int version;
  final bool canContribute, canReview;
  final DateTime observedAt, expiresAt;
  final List<PlacePhotoCandidate> candidates;
  bool get current => expiresAt.isAfter(DateTime.now().toUtc());
  static PlacePhotoEditorial fromJson(Object? raw, PlacePhotoRef ref) {
    final m = _object(raw, const {
      'placeId',
      'cityId',
      'version',
      'canContribute',
      'canReview',
      'observedAt',
      'expiresAt',
      'candidates',
    });
    final now = DateTime.now().toUtc(),
        observed = _time(m['observedAt']),
        expires = _time(m['expiresAt']);
    if (!ref.valid ||
        m['placeId'] != ref.id ||
        !_text(m['cityId'], 80) ||
        m['cityId'] == '' ||
        !_version(m['version']) ||
        m['canContribute'] is! bool ||
        m['canReview'] is! bool ||
        m['candidates'] is! List ||
        (m['candidates'] as List).length > 20 ||
        observed.isAfter(now.add(const Duration(seconds: 2))) ||
        !expires.isAfter(now) ||
        !expires.isAfter(observed) ||
        expires.isAfter(now.add(const Duration(seconds: 31)))) {
      throw const FormatException('地点照片当前权限或来源不符');
    }
    final candidates = (m['candidates'] as List)
        .map(PlacePhotoCandidate.fromJson)
        .toList(growable: false);
    if (candidates.map((c) => c.id).toSet().length != candidates.length ||
        (!m['canReview'] &&
            candidates.any((c) => c.canApprove || c.canReject))) {
      throw const FormatException('地点照片审核权限不符');
    }
    return PlacePhotoEditorial(
      ref: ref,
      cityID: m['cityId'],
      version: m['version'],
      canContribute: m['canContribute'],
      canReview: m['canReview'],
      observedAt: observed,
      expiresAt: expires,
      candidates: List.unmodifiable(candidates),
    );
  }
}

/// Immutable original operation selector; no photo body or credentials.
class PlacePhotoSelector {
  const PlacePhotoSelector({
    required this.operationID,
    required this.expectedVersion,
    required this.action,
    this.candidateID = '',
    this.candidateVersion = 0,
  });
  final String operationID, action, candidateID;
  final int expectedVersion, candidateVersion;
  bool get valid =>
      entityCoverID(operationID) &&
      operationID != '00000000-0000-0000-0000-000000000000' &&
      expectedVersion >= 0 &&
      expectedVersion <= entityCoverMaxVersion &&
      (action == 'SUBMIT'
          ? candidateID.isEmpty &&
                candidateVersion == 0 &&
                expectedVersion < entityCoverMaxVersion
          : const {'APPROVE', 'REJECT', 'WITHDRAW'}.contains(action) &&
                entityCoverID(candidateID) &&
                candidateID != '00000000-0000-0000-0000-000000000000' &&
                candidateVersion > 0 &&
                candidateVersion <= entityCoverMaxVersion &&
                (action != 'APPROVE' ||
                    (expectedVersion < entityCoverMaxVersion &&
                        candidateVersion < entityCoverMaxVersion)));
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'expectedVersion': expectedVersion,
    'action': action,
    if (action != 'SUBMIT') ...{
      'candidateId': candidateID,
      'candidateVersion': candidateVersion,
    },
  };
  bool same(PlacePhotoSelector o) =>
      operationID == o.operationID &&
      expectedVersion == o.expectedVersion &&
      action == o.action &&
      candidateID == o.candidateID &&
      candidateVersion == o.candidateVersion;
}

class PlacePhotoPreview {
  const PlacePhotoPreview(
    this.selector,
    this.ref,
    this.token,
    this.expiresAt,
    this.inputDigest,
    this.rightsDigest,
    this.bytes,
  );
  final PlacePhotoSelector selector;
  final PlacePhotoRef ref;
  final String token, inputDigest, rightsDigest;
  final DateTime expiresAt;
  final Uint8List? bytes;
  static PlacePhotoPreview fromJson(
    Object? raw,
    PlacePhotoRef ref,
    PlacePhotoSelector selector,
    EntityCoverRights rights,
    PlacePhotoThumbnail? thumbnail,
  ) {
    final picture = selector.action != 'WITHDRAW';
    final m = _object(raw, {
      'placeId',
      'operationId',
      'expectedVersion',
      'action',
      'candidateId',
      'candidateVersion',
      'inputSha256',
      'rightsSha256',
      'expiresAt',
      'token',
      if (picture) 'previewDerivative',
    });
    final end = _time(m['expiresAt']), now = DateTime.now().toUtc();
    if (!ref.valid ||
        !selector.valid ||
        m['placeId'] != ref.id ||
        m['operationId'] != selector.operationID ||
        m['expectedVersion'] != selector.expectedVersion ||
        m['action'] != selector.action ||
        m['candidateId'] !=
            (selector.action == 'SUBMIT'
                ? selector.operationID
                : selector.candidateID) ||
        m['candidateVersion'] != selector.candidateVersion ||
        m['rightsSha256'] != rights.digest ||
        !_hash(m['inputSha256']) ||
        (thumbnail != null && m['inputSha256'] != thumbnail.digest) ||
        m['token'] is! String ||
        !RegExp(r'^pp1\.\d+\.\d+\.[0-9a-f]{64}$').hasMatch(m['token']) ||
        (const {'SUBMIT', 'APPROVE'}.contains(selector.action) &&
            (!rights.valid || end.isAfter(rights.retentionUntil.toUtc()))) ||
        !end.isAfter(now) ||
        end.isAfter(now.add(const Duration(seconds: 31)))) {
      throw const FormatException('地点照片确认与原候选或权利不符');
    }
    Uint8List? bytes;
    if (picture) {
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
        throw const FormatException('地点照片预览过大');
      }
      bytes = base64Decode(d['contentBase64']);
      if (base64Encode(bytes) != d['contentBase64'] ||
          d['contentType'] != 'image/jpeg' ||
          d['width'] != 320 ||
          d['height'] != 180 ||
          d['byteCount'] != bytes.length ||
          bytes.isEmpty ||
          bytes.length > entityCoverMaxBytes ||
          d['sha256'] != m['inputSha256'] ||
          sha256.convert(bytes).toString() != m['inputSha256']) {
        throw const FormatException('地点照片预览内容不符');
      }
      final h = MomentImageHeader.inspect(bytes);
      if (h.mime != 'image/jpeg' || h.width != 320 || h.height != 180) {
        throw const FormatException('地点照片预览尺寸不符');
      }
    }
    return PlacePhotoPreview(
      selector,
      ref,
      m['token'],
      end,
      m['inputSha256'],
      m['rightsSha256'],
      bytes,
    );
  }
}

class PlacePhotoReceipt {
  const PlacePhotoReceipt(
    this.status,
    this.appliedVersion,
    this.candidateID,
    this.candidateVersion,
  );
  final String status, candidateID;
  final int appliedVersion, candidateVersion;
  static PlacePhotoReceipt fromJson(
    Object? raw,
    PlacePhotoRef ref,
    PlacePhotoSelector selector,
  ) {
    if (raw is! Map<String, dynamic>) {
      throw const FormatException('地点照片原结果不符');
    }
    final stopped = raw['status'] == 'STOPPED';
    final m = _object(raw, {
      'placeId',
      'operationId',
      'action',
      'candidateVersion',
      'appliedVersion',
      'status',
      if (!stopped) 'candidateId',
    });
    if (!ref.valid ||
        !selector.valid ||
        m['placeId'] != ref.id ||
        m['operationId'] != selector.operationID ||
        !_version(m['appliedVersion']) ||
        !_version(m['candidateVersion']) ||
        (stopped
            ? m['action'] != 'STOP' ||
                  m['appliedVersion'] != 0 ||
                  m['candidateVersion'] != 0
            : m['status'] != 'SUCCEEDED' ||
                  m['action'] != selector.action ||
                  m['candidateId'] !=
                      (selector.action == 'SUBMIT'
                          ? selector.operationID
                          : selector.candidateID) ||
                  m['candidateVersion'] !=
                      (selector.candidateVersion < entityCoverMaxVersion
                          ? selector.candidateVersion + 1
                          : entityCoverMaxVersion) ||
                  (selector.action == 'APPROVE'
                      ? m['appliedVersion'] != selector.expectedVersion + 1
                      : selector.action == 'WITHDRAW'
                      ? m['appliedVersion'] != 0 &&
                            m['appliedVersion'] !=
                                (selector.expectedVersion <
                                        entityCoverMaxVersion
                                    ? selector.expectedVersion + 1
                                    : entityCoverMaxVersion)
                      : m['appliedVersion'] != 0))) {
      throw const FormatException('地点照片原操作回执不符');
    }
    return PlacePhotoReceipt(
      m['status'],
      m['appliedVersion'],
      stopped ? '' : m['candidateId'],
      m['candidateVersion'],
    );
  }
}

class PlacePhotoFailure implements Exception {
  const PlacePhotoFailure(this.status);
  final int status;
  String get message => switch (status) {
    401 => '登录已失效，请重新登录。',
    403 => '当前没有此照片的贡献、审核或撤回权限。',
    404 => '地点、候选或原操作暂不可见。',
    409 => '照片、权限或版本已变化，请重新读取。',
    400 => '图片或权利资料有误，请检查。',
    _ => '照片服务暂不可用，请重试读取。',
  };
}

/// Separate editorial commands, borrowing transport without closing it.
class PlacePhotoAPI {
  const PlacePhotoAPI({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
    this.current,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  final bool Function()? current;
  EntityCoverAPI get pixels => EntityCoverAPI(
    client: client,
    baseUrl: baseUrl,
    authorizationHeader: authorizationHeader,
    current: current,
  );
  Future<Object?> _json(
    String method,
    String path, {
    Map<String, Object>? body,
  }) async {
    final base = Uri.tryParse(baseUrl), token = authorizationHeader();
    if (base == null ||
        !const {'https', 'http'}.contains(base.scheme) ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty ||
        base.hasQuery ||
        base.hasFragment ||
        current?.call() == false ||
        ((method != 'GET' ||
                path.endsWith('/editorial') ||
                path.contains('/operations/')) &&
            token == null)) {
      throw const PlacePhotoFailure(401);
    }
    final request = http.Request(
      method,
      Uri.parse('${baseUrl.replaceFirst(RegExp(r'/$'), '')}$path'),
    );
    request.followRedirects = false;
    if (request.url.origin != base.origin) {
      throw const FormatException('地点照片来源不符');
    }
    if (token != null) request.headers['Authorization'] = token;
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }
    final response = await client
        .send(request)
        .timeout(const Duration(seconds: 12));
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (chunk.length > 400000 - bytes.length) {
        throw const FormatException('地点照片响应过大');
      }
      bytes.add(chunk);
    }
    if (authorizationHeader() != token || current?.call() == false) {
      throw const PlacePhotoFailure(401);
    }
    if (response.statusCode != 200)
      throw PlacePhotoFailure(response.statusCode);
    return _object(jsonDecode(utf8.decode(bytes.takeBytes())), const {
      'data',
    })['data'];
  }

  void _ref(PlacePhotoRef ref) {
    if (!ref.valid) throw const FormatException('地点照片对象不符');
  }

  Future<PlacePhotoState> state(PlacePhotoRef ref) async {
    _ref(ref);
    return PlacePhotoState.fromJson(await _json('GET', ref.path), ref);
  }

  Future<PlacePhotoEditorial> editorial(PlacePhotoRef ref) async {
    _ref(ref);
    return PlacePhotoEditorial.fromJson(
      await _json('GET', '${ref.path}/editorial'),
      ref,
    );
  }

  Map<String, Object> input(
    PlacePhotoSelector selector, {
    Uint8List? bytes,
    EntityCoverRights? rights,
    String? approval,
  }) {
    if (!selector.valid ||
        (selector.action == 'SUBMIT'
            ? bytes == null || rights?.valid != true
            : bytes != null || rights != null)) {
      throw const FormatException('地点照片输入不符');
    }
    if (bytes != null) {
      final h = MomentImageHeader.inspect(bytes);
      if (bytes.isEmpty ||
          bytes.length > entityCoverMaxInputBytes ||
          h.width > 4096 ||
          h.height > 4096 ||
          h.width * h.height > 16 * 1024 * 1024) {
        throw const FormatException('请选择5 MiB以内、4096像素以内的静态图片。');
      }
    }
    return {
      ...selector.toJson(),
      if (bytes != null) ...{
        'contentType': MomentImageHeader.inspect(bytes).mime,
        'contentBase64': base64Encode(bytes),
        'rights': rights!.toJson(),
      },
      if (selector.action == 'APPROVE') ...{
        'rightsConfirmed': true,
        'photoConfirmed': true,
      },
      if (approval != null) 'approval': approval,
    };
  }

  Future<PlacePhotoPreview> preview(
    PlacePhotoRef ref,
    PlacePhotoSelector selector, {
    Uint8List? bytes,
    required EntityCoverRights rights,
    PlacePhotoThumbnail? thumbnail,
  }) async {
    _ref(ref);
    return PlacePhotoPreview.fromJson(
      await _json(
        'POST',
        '${ref.path}/preview',
        body: input(
          selector,
          bytes: bytes,
          rights: selector.action == 'SUBMIT' ? rights : null,
        ),
      ),
      ref,
      selector,
      rights,
      thumbnail,
    );
  }

  Future<PlacePhotoReceipt> change(
    PlacePhotoRef ref,
    PlacePhotoSelector selector,
    PlacePhotoPreview approval, {
    Uint8List? bytes,
    EntityCoverRights? rights,
  }) async {
    _ref(ref);
    if (approval.ref != ref ||
        !approval.selector.same(selector) ||
        !approval.expiresAt.isAfter(DateTime.now().toUtc()) ||
        (selector.action == 'SUBMIT' &&
            approval.rightsDigest != rights?.digest)) {
      throw const FormatException('地点照片批准已变化或过期');
    }
    return PlacePhotoReceipt.fromJson(
      await _json(
        'PUT',
        ref.path,
        body: input(
          selector,
          bytes: bytes,
          rights: rights,
          approval: approval.token,
        ),
      ),
      ref,
      selector,
    );
  }

  Future<PlacePhotoReceipt> operation(
    PlacePhotoRef ref,
    PlacePhotoSelector selector,
  ) async {
    _ref(ref);
    if (!selector.valid) throw const FormatException('原地点照片操作不符');
    return PlacePhotoReceipt.fromJson(
      await _json('GET', '${ref.path}/operations/${selector.operationID}'),
      ref,
      selector,
    );
  }

  Future<PlacePhotoReceipt> stop(
    PlacePhotoRef ref,
    PlacePhotoSelector selector,
  ) async {
    _ref(ref);
    if (!selector.valid) throw const FormatException('原地点照片操作不符');
    return PlacePhotoReceipt.fromJson(
      await _json(
        'POST',
        '${ref.path}/operations/${selector.operationID}/stop',
        body: {'expectedVersion': selector.expectedVersion},
      ),
      ref,
      selector,
    );
  }
}
