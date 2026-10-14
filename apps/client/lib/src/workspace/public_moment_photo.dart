import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../content/moment_image_header.dart';
import 'entity_cover.dart' show EntityCoverRights;
import 'human_causal_control.dart';

const publicMomentPhotoMaxVersion = humanControlMaxVersion;
const publicMomentPhotoMaxBytes = 256 * 1024;
const publicMomentPhotoMaxInputBytes = 5 * 1024 * 1024;

/// RAM-only invalidation hint. It carries no public-media authority or pixels.
class PublicMomentPhotoLocalChange {
  const PublicMomentPhotoLocalChange({
    required this.ownerID,
    required this.origin,
    required this.momentID,
  });
  final String ownerID, origin, momentID;
}

final publicMomentPhotoChanges = ValueNotifier<PublicMomentPhotoLocalChange?>(
  null,
);
String publicMomentPhotoPath(
  String momentID,
  int revision,
  String photoID,
  int version,
) => '/v1/moments/$momentID/public-photo/$revision/$photoID/$version/content';

class PublicMomentPhotoMetadata {
  const PublicMomentPhotoMetadata({
    required this.momentID,
    required this.momentRevision,
    required this.photoID,
    required this.version,
    required this.path,
    required this.byteCount,
    required this.digest,
    required this.expiresAt,
    required this.attribution,
  });
  final String momentID, photoID, path, digest, attribution;
  final int momentRevision, version, byteCount;
  final DateTime expiresAt;
  bool get current => DateTime.now().toUtc().isBefore(expiresAt);
  Map<String, Object> toJson() => {
    'momentId': momentID,
    'momentRevision': momentRevision,
    'photoId': photoID,
    'version': version,
    'thumbnailPath': path,
    'width': 320,
    'height': 180,
    'byteCount': byteCount,
    'sha256': digest,
    'expiresAt': expiresAt.toUtc().toIso8601String(),
    'attribution': attribution,
  };
  static PublicMomentPhotoMetadata decode(
    Object? raw,
    String expectedMomentID,
    DateTime observedAt, {
    DateTime? now,
    int? expectedMomentRevision,
    DateTime? validUntil,
  }) {
    final m = humanControlObject(raw, {
      'momentId',
      'momentRevision',
      'photoId',
      'version',
      'thumbnailPath',
      'width',
      'height',
      'byteCount',
      'sha256',
      'expiresAt',
      'attribution',
    });
    final until = humanControlTime(m['expiresAt']),
        clock = (now ?? DateTime.now()).toUtc();
    if (!humanControlID(expectedMomentID) ||
        m['momentId'] != expectedMomentID ||
        !humanControlID(m['photoId']) ||
        !humanControlVersion(m['momentRevision'], zero: false) ||
        m['momentRevision'] < 2 ||
        expectedMomentRevision != null &&
            m['momentRevision'] != expectedMomentRevision ||
        !humanControlVersion(m['version'], zero: false) ||
        m['thumbnailPath'] !=
            publicMomentPhotoPath(
              expectedMomentID,
              m['momentRevision'],
              m['photoId'],
              m['version'],
            ) ||
        m['width'] != 320 ||
        m['height'] != 180 ||
        m['byteCount'] is! int ||
        m['byteCount'] < 1 ||
        m['byteCount'] > publicMomentPhotoMaxBytes ||
        !humanControlHash(m['sha256']) ||
        !until.isAfter(observedAt.toUtc()) ||
        until.isAfter(observedAt.toUtc().add(const Duration(seconds: 60))) ||
        observedAt.toUtc().isAfter(clock.add(const Duration(seconds: 2))) ||
        validUntil != null && until.isAfter(validUntil.toUtc()) ||
        m['attribution'] is! String ||
        utf8.encode(m['attribution']).length > 320 ||
        (m['attribution'] as String).trim() != m['attribution'] ||
        (m['attribution'] as String).contains(RegExp(r'[\x00-\x1f\x7f-\x9f]')))
      throw const FormatException('公开动态照片来源、版本或期限不符');
    return PublicMomentPhotoMetadata(
      momentID: expectedMomentID,
      momentRevision: m['momentRevision'],
      photoID: m['photoId'],
      version: m['version'],
      path: m['thumbnailPath'],
      byteCount: m['byteCount'],
      digest: m['sha256'],
      expiresAt: until,
      attribution: m['attribution'],
    );
  }

  bool sameSource(PublicMomentPhotoMetadata other) =>
      momentID == other.momentID &&
      momentRevision == other.momentRevision &&
      photoID == other.photoID &&
      version == other.version &&
      path == other.path &&
      byteCount == other.byteCount &&
      digest == other.digest &&
      attribution == other.attribution;
}

void publicMomentPhotoJPEG(Uint8List bytes, int count, String digest) {
  if (bytes.isEmpty ||
      bytes.length > publicMomentPhotoMaxBytes ||
      bytes.length != count ||
      sha256.convert(bytes).toString() != digest)
    throw const FormatException('公开照片内容校验失败');
  final h = MomentImageHeader.inspect(bytes);
  if (h.mime != 'image/jpeg' || h.width != 320 || h.height != 180)
    throw const FormatException('公开照片规格不符');
}

MomentImageHeader publicMomentPhotoSelectedImage(Uint8List bytes) {
  if (bytes.isEmpty || bytes.length > publicMomentPhotoMaxInputBytes)
    throw const FormatException('请选择5MiB以内的静态JPEG或PNG。');
  final h = MomentImageHeader.inspect(bytes);
  if (h.width > 4096 ||
      h.height > 4096 ||
      h.width * h.height > 16 * 1024 * 1024)
    throw const FormatException('图片尺寸超过允许范围。');
  return h;
}

class PublicMomentPhotoState {
  const PublicMomentPhotoState({
    required this.momentID,
    required this.momentRevision,
    required this.photoID,
    required this.photoVersion,
    required this.photo,
    required this.canSave,
    required this.canDelete,
    required this.observedAt,
    required this.expiresAt,
    required this.sourceVersion,
  });
  final String momentID, sourceVersion;
  final String? photoID;
  final int momentRevision, photoVersion;
  final PublicMomentPhotoMetadata? photo;
  final bool canSave, canDelete;
  final DateTime observedAt, expiresAt;
  bool get current => DateTime.now().toUtc().isBefore(expiresAt);
  factory PublicMomentPhotoState.decode(Object? raw, String id) {
    final m = humanControlObject(raw, {
      'momentId',
      'momentRevision',
      'photoId',
      'photoVersion',
      'photo',
      'canSave',
      'canDelete',
      'observedAt',
      'expiresAt',
      'sourceVersion',
    });
    final at = humanControlTime(m['observedAt']),
        until = humanControlTime(m['expiresAt']);
    humanControlLease(at, until);
    if (m['momentId'] != id ||
        !humanControlID(id) ||
        !humanControlVersion(m['momentRevision'], zero: false) ||
        !humanControlVersion(m['photoVersion']) ||
        m['photoId'] != null && !humanControlID(m['photoId']) ||
        (m['photoId'] == null) != (m['photoVersion'] == 0) ||
        m['canSave'] is! bool ||
        m['canDelete'] is! bool ||
        m['canSave'] == true &&
            (m['momentRevision'] < 2 ||
                m['photoVersion'] >= publicMomentPhotoMaxVersion) ||
        m['canDelete'] == true &&
            (m['photoId'] == null || m['photoVersion'] < 1) ||
        !humanControlHash(m['sourceVersion']) ||
        until.isAfter(at.add(const Duration(seconds: 30))))
      throw const FormatException('本人公开图片状态不符');
    final photo = m['photo'] == null
        ? null
        : PublicMomentPhotoMetadata.decode(
            m['photo'],
            id,
            at,
            expectedMomentRevision: m['momentRevision'],
          );
    if (photo != null &&
        (photo.photoID != m['photoId'] ||
            photo.version != m['photoVersion'] ||
            photo.expiresAt.isAfter(until)))
      throw const FormatException('本人图片版本不符');
    return PublicMomentPhotoState(
      momentID: id,
      momentRevision: m['momentRevision'],
      photoID: m['photoId'],
      photoVersion: m['photoVersion'],
      photo: photo,
      canSave: m['canSave'],
      canDelete: m['canDelete'],
      observedAt: at,
      expiresAt: until,
      sourceVersion: m['sourceVersion'],
    );
  }
}

class PublicMomentPhotoInput {
  PublicMomentPhotoInput({
    required this.momentID,
    required this.operationID,
    required this.action,
    required this.expectedMomentRevision,
    required this.photoID,
    required this.expectedPhotoVersion,
    Uint8List? bytes,
    this.rights,
  }) : bytes = bytes == null
           ? null
           : Uint8List.fromList(bytes).asUnmodifiableView() {
    if (!humanControlID(momentID) ||
        !humanControlID(operationID) ||
        !humanControlID(photoID) ||
        !humanControlVersion(expectedMomentRevision, zero: false) ||
        !humanControlVersion(expectedPhotoVersion) ||
        !const {'SAVE', 'DELETE'}.contains(action) ||
        (action == 'SAVE'
            ? expectedMomentRevision < 2 ||
                  bytes == null ||
                  rights?.valid != true ||
                  expectedPhotoVersion >= publicMomentPhotoMaxVersion
            : bytes != null || rights != null || expectedPhotoVersion < 1))
      throw const FormatException('公开动态照片输入不符');
    if (bytes != null) {
      publicMomentPhotoSelectedImage(bytes);
    }
  }
  final String momentID, operationID, action, photoID;
  final int expectedMomentRevision, expectedPhotoVersion;
  final Uint8List? bytes;
  final EntityCoverRights? rights;
  Map<String, Object> body() => {
    'operationId': operationID,
    'action': action,
    'expectedMomentRevision': expectedMomentRevision,
    'photoId': photoID,
    'expectedPhotoVersion': expectedPhotoVersion,
    if (bytes != null) ...{
      'contentType': MomentImageHeader.inspect(bytes!).mime,
      'contentBase64': base64Encode(bytes!),
      'rights': rights!.toJson(),
    },
  };
  String digest(String normalizedSHA) => humanControlTupleDigest([
    'public-moment-photo-v1',
    momentID,
    operationID,
    action,
    expectedMomentRevision,
    photoID,
    expectedPhotoVersion,
    normalizedSHA,
    rights?.digest ?? '',
  ]);
}

class PublicMomentPhotoPreview {
  const PublicMomentPhotoPreview({
    required this.input,
    required this.inputSHA,
    required this.payloadDigest,
    required this.sourceVersion,
    required this.approval,
    required this.observedAt,
    required this.expiresAt,
    required this.derivative,
  });
  final PublicMomentPhotoInput input;
  final String inputSHA, payloadDigest, sourceVersion, approval;
  final DateTime observedAt, expiresAt;
  final Uint8List? derivative;
  bool get current => DateTime.now().toUtc().isBefore(expiresAt);
  factory PublicMomentPhotoPreview.decode(
    Object? raw,
    PublicMomentPhotoInput input,
    DateTime originalExpiry,
  ) {
    final m = humanControlObject(raw, {
      'operationId',
      'action',
      'payloadDigest',
      'observedAt',
      'expiresAt',
      'approval',
      'momentId',
      'photoId',
      'expectedMomentRevision',
      'expectedPhotoVersion',
      'inputSha256',
      'rightsSha256',
      'sourceVersion',
      if (input.action == 'SAVE') 'previewDerivative',
    });
    final at = humanControlTime(m['observedAt']),
        until = humanControlTime(m['expiresAt']);
    humanControlLease(at, until);
    if (m['operationId'] != input.operationID ||
        m['momentId'] != input.momentID ||
        m['action'] != input.action ||
        m['photoId'] != input.photoID ||
        m['expectedMomentRevision'] != input.expectedMomentRevision ||
        m['expectedPhotoVersion'] != input.expectedPhotoVersion ||
        !humanControlHash(m['sourceVersion']) ||
        m['approval'] is! String ||
        (m['approval'] as String).isEmpty ||
        utf8.encode(m['approval']).length > 8192 ||
        (m['approval'] as String).contains('\u0000') ||
        m['rightsSha256'] != (input.rights?.digest ?? '') ||
        (input.action == 'SAVE'
            ? !humanControlHash(m['inputSha256'])
            : m['inputSha256'] != '') ||
        m['payloadDigest'] != input.digest(m['inputSha256']) ||
        until.isAfter(at.add(const Duration(seconds: 30))))
      throw const FormatException('公开照片具体批准不符');
    Uint8List? bytes;
    if (input.action == 'SAVE') {
      final d = humanControlObject(m['previewDerivative'], {
        'contentType',
        'width',
        'height',
        'byteCount',
        'sha256',
        'contentBase64',
      });
      if (d['contentBase64'] is! String ||
          (d['contentBase64'] as String).length >
              4 * ((publicMomentPhotoMaxBytes + 2) ~/ 3))
        throw const FormatException('图片预览过大');
      bytes = base64Decode(d['contentBase64']);
      if (base64Encode(bytes) != d['contentBase64'] ||
          d['contentType'] != 'image/jpeg' ||
          d['width'] != 320 ||
          d['height'] != 180 ||
          d['byteCount'] != bytes.length ||
          d['sha256'] != m['inputSha256'])
        throw const FormatException('图片预览不符');
      publicMomentPhotoJPEG(bytes, bytes.length, m['inputSha256']);
    }
    return PublicMomentPhotoPreview(
      input: input,
      inputSHA: m['inputSha256'],
      payloadDigest: m['payloadDigest'],
      sourceVersion: m['sourceVersion'],
      approval: m['approval'],
      observedAt: at,
      expiresAt: until.isBefore(originalExpiry) ? until : originalExpiry,
      derivative: bytes?.asUnmodifiableView(),
    );
  }
}

class PublicMomentPhotoReceipt {
  const PublicMomentPhotoReceipt({
    required this.operationID,
    required this.status,
    required this.appliedPhotoVersion,
  });
  final String operationID, status;
  final int appliedPhotoVersion;
  bool get succeeded => status == 'SUCCEEDED';
  factory PublicMomentPhotoReceipt.decode(
    Object? raw, {
    required String operationID,
    required String momentID,
    required String photoID,
    required String action,
    required int expectedMomentRevision,
    required int expectedPhotoVersion,
    required String digest,
  }) {
    if (raw is! Map<String, dynamic>) throw const FormatException('原图片回执不符');
    final stop = raw['status'] == 'STOPPED';
    final m = humanControlObject(
      raw,
      stop
          ? {'operationId', 'status', 'appliedPhotoVersion'}
          : {
              'operationId',
              'status',
              'action',
              'momentId',
              'photoId',
              'expectedMomentRevision',
              'expectedPhotoVersion',
              'appliedPhotoVersion',
              'payloadDigest',
              'committedAt',
            },
    );
    if (m['operationId'] != operationID ||
        !humanControlID(operationID) ||
        !humanControlVersion(m['appliedPhotoVersion']))
      throw const FormatException('原图片回执不符');
    if (stop) {
      if (m['appliedPhotoVersion'] != 0)
        throw const FormatException('停止图片结果不符');
    } else {
      final want =
          action == 'DELETE' &&
              expectedPhotoVersion == publicMomentPhotoMaxVersion
          ? expectedPhotoVersion
          : expectedPhotoVersion + 1;
      if (m['status'] != 'SUCCEEDED' ||
          m['action'] != action ||
          m['momentId'] != momentID ||
          m['photoId'] != photoID ||
          m['expectedMomentRevision'] != expectedMomentRevision ||
          m['expectedPhotoVersion'] != expectedPhotoVersion ||
          m['appliedPhotoVersion'] != want ||
          m['payloadDigest'] != digest)
        throw const FormatException('原图片事实不符');
      humanControlTime(m['committedAt']);
    }
    return PublicMomentPhotoReceipt(
      operationID: operationID,
      status: m['status'],
      appliedPhotoVersion: m['appliedPhotoVersion'],
    );
  }
}

class PublicMomentPhotoAPI {
  const PublicMomentPhotoAPI({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
    this.current,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  final bool Function()? current;
  String get origin => baseUrl.replaceFirst(RegExp(r'/$'), '');
  HumanCausalApi get _json => HumanCausalApi(
    client: client,
    baseUrl: baseUrl,
    authorizationHeader: authorizationHeader,
  );
  String _base(String id) {
    if (!humanControlID(id)) throw const FormatException('动态ID不符');
    return '/v1/me/moments/$id/public-photo';
  }

  Future<PublicMomentPhotoState> state(String id) async =>
      PublicMomentPhotoState.decode(
        await _json.json(
          'GET',
          _base(id),
          current: () => current?.call() != false,
        ),
        id,
      );
  Future<PublicMomentPhotoPreview> preview(
    PublicMomentPhotoInput input,
    DateTime expiry,
  ) async => PublicMomentPhotoPreview.decode(
    await _json.json(
      'POST',
      '${_base(input.momentID)}/previews',
      body: input.body(),
      current: () => current?.call() != false,
    ),
    input,
    expiry,
  );
  Future<Object?> put(PublicMomentPhotoPreview p) => _json.json(
    'PUT',
    '${_base(p.input.momentID)}/operations/${p.input.operationID}',
    body: {
      ...p.input.body(),
      'sourceVersion': p.sourceVersion,
      'approval': p.approval,
    },
    current: () => current?.call() != false,
  );
  Future<Object?> original(String id, String op, {bool stop = false}) {
    if (!humanControlID(op)) throw const FormatException('原图片操作ID不符');
    return _json.json(
      stop ? 'POST' : 'GET',
      '${_base(id)}/operations/$op${stop ? '/stop' : ''}',
      current: () => current?.call() != false,
    );
  }

  Future<Uint8List> content(PublicMomentPhotoMetadata m) async {
    PublicMomentPhotoMetadata.decode(
      m.toJson(),
      m.momentID,
      m.expiresAt.subtract(const Duration(seconds: 60)),
    );
    if (!m.current || current?.call() == false)
      throw const HumanCausalFailure(404);
    final base = Uri.tryParse(baseUrl),
        token = authorizationHeader(),
        url = Uri.tryParse('$origin${m.path}');
    if (base == null ||
        url == null ||
        !const {'http', 'https'}.contains(base.scheme) ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty ||
        base.hasQuery ||
        base.hasFragment ||
        url.origin != base.origin ||
        m.path !=
            publicMomentPhotoPath(
              m.momentID,
              m.momentRevision,
              m.photoID,
              m.version,
            ))
      throw const FormatException('公开照片读取来源不符');
    final req = http.Request('GET', url)..followRedirects = false;
    if (token != null) req.headers['Authorization'] = token;
    final response = await client
            .send(req)
            .timeout(const Duration(seconds: 12)),
        bytes = BytesBuilder(copy: false);
    await for (final block in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (block.length > publicMomentPhotoMaxBytes - bytes.length)
        throw const FormatException('图片响应过大');
      bytes.add(block);
    }
    if (current?.call() == false ||
        authorizationHeader() != token ||
        !m.current)
      throw const HumanCausalFailure(404);
    if (response.statusCode != 200)
      throw HumanCausalFailure(response.statusCode);
    final raw = bytes.takeBytes();
    publicMomentPhotoJPEG(raw, m.byteCount, m.digest);
    return raw;
  }
}

/// Current controlled public pixels only. Absent/failed media occupies no area.
class PublicMomentPhoto extends StatefulWidget {
  const PublicMomentPhoto({
    super.key,
    required this.metadata,
    required this.api,
    required this.current,
    this.identityChanges,
    this.width,
    this.height,
  });
  final PublicMomentPhotoMetadata? metadata;
  final PublicMomentPhotoAPI api;
  final bool Function() current;
  final Listenable? identityChanges;
  final double? width, height;
  @override
  State<PublicMomentPhoto> createState() => _PublicMomentPhotoState();
}

class _PublicMomentPhotoState extends State<PublicMomentPhoto> {
  Uint8List? _bytes;
  int _epoch = 0;
  Timer? _expiry;
  String? _header;
  @override
  void initState() {
    super.initState();
    _header = widget.api.authorizationHeader();
    widget.identityChanges?.addListener(_observe);
    unawaited(_load());
  }

  bool _valid(int e, PublicMomentPhotoMetadata m) =>
      mounted &&
      e == _epoch &&
      widget.current() &&
      widget.metadata?.sameSource(m) == true &&
      widget.metadata?.expiresAt == m.expiresAt &&
      m.current &&
      _header == widget.api.authorizationHeader();
  void _observe() {
    ++_epoch;
    _bytes = null;
    _expiry?.cancel();
    _header = widget.api.authorizationHeader();
    if (mounted) {
      if (SchedulerBinding.instance.schedulerPhase ==
          SchedulerPhase.persistentCallbacks) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) setState(() {});
        });
      } else {
        setState(() {});
      }
    }
    if (widget.current()) unawaited(_load());
  }

  @override
  void didUpdateWidget(PublicMomentPhoto old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_observe);
      widget.identityChanges?.addListener(_observe);
    }
    if (old.metadata != widget.metadata ||
        old.api.client != widget.api.client ||
        old.api.baseUrl != widget.api.baseUrl ||
        _header != widget.api.authorizationHeader())
      _observe();
  }

  Future<void> _load() async {
    final m = widget.metadata, e = ++_epoch;
    _bytes = null;
    _expiry?.cancel();
    if (m == null || !m.current || !widget.current()) return;
    _expiry = Timer(m.expiresAt.difference(DateTime.now().toUtc()), () {
      if (mounted && e == _epoch) setState(() => _bytes = null);
    });
    try {
      final b = await widget.api.content(m);
      if (!_valid(e, m)) return;
      final codec = await ui.instantiateImageCodec(b);
      try {
        if (!_valid(e, m)) return;
        final frame = await codec.getNextFrame();
        try {
          if (codec.frameCount != 1 ||
              frame.image.width != 320 ||
              frame.image.height != 180)
            throw const FormatException('公开图片像素规格不符');
        } finally {
          frame.image.dispose();
        }
      } finally {
        codec.dispose();
      }
      if (_valid(e, m)) setState(() => _bytes = b);
    } catch (_) {
      if (_valid(e, m)) setState(() => _bytes = null);
    }
  }

  @override
  void dispose() {
    ++_epoch;
    _expiry?.cancel();
    widget.identityChanges?.removeListener(_observe);
    _bytes = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext c) {
    final m = widget.metadata, b = _bytes;
    if (m == null ||
        b == null ||
        !m.current ||
        !widget.current() ||
        _header != widget.api.authorizationHeader())
      return const SizedBox.shrink();
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Image.memory(
          b,
          width: widget.width,
          height: widget.height,
          fit: BoxFit.cover,
          gaplessPlayback: false,
          semanticLabel: '本人明确公开的动态图片',
          errorBuilder: (_, __, ___) => const SizedBox.shrink(),
        ),
        if (m.attribution.isNotEmpty)
          Text(m.attribution, style: Theme.of(c).textTheme.bodySmall),
      ],
    );
  }
}
