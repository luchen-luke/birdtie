import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import '../content/moment_image_header.dart';
import '../content/video_material_picker.dart' show validateLocalVideoMP4;
import 'public_moment_video.dart';
import 'entity_cover.dart';
import 'human_causal_control.dart';

const publicMomentMediaMaxImages = 9;
const publicMomentMediaValidation = 'STATIC_IMAGE_DECODE_REENCODE_V1';
const publicMomentVideoPurpose = 'PUBLIC_MOMENT_VIDEO_STAGE_V2';
const publicMomentVideoValidation = 'FFMPEG_TRANSCODE_REPROBE_FULL_DECODE_V2';

class PublicMomentVideoSelection {
  PublicMomentVideoSelection({
    required Uint8List bytes,
    required this.durationMS,
    required this.width,
    required this.height,
  }) : bytes = Uint8List.fromList(bytes) {
    validateLocalVideoMP4(this.bytes);
    if (durationMS < 1 ||
        durationMS > 30000 ||
        width < 2 ||
        height < 2 ||
        width * height > 1920 * 1080 ||
        width > 1920 ||
        height > 1920 ||
        width.clamp(0, height) > 1080) {
      this.bytes.fillRange(0, this.bytes.length, 0);
      throw const FormatException('请选择30秒以内、1080p以内的MP4视频');
    }
  }
  final Uint8List bytes;
  final int durationMS, width, height;
  Map<String, Object> toPreparationJSON(String operation, String scope) => {
    'operationId': operation,
    'localScope': scope,
    'purpose': publicMomentVideoPurpose,
    'contentType': 'video/mp4',
    'contentBase64': base64Encode(bytes),
  };
}

Map<String, dynamic> _videoContract(Object? raw, String path) {
  final value = mediaObject(raw, {
    'mimeType',
    'durationMs',
    'codecVideo',
    'codecAudio',
    'pixelFormat',
    'videoSha256',
    'videoBytes',
    'videoPath',
  });
  if (value['mimeType'] != 'video/mp4' ||
      value['durationMs'] is! int ||
      value['durationMs'] < 1 ||
      value['durationMs'] > 30000 ||
      value['codecVideo'] != 'h264' ||
      !const {'aac', 'none'}.contains(value['codecAudio']) ||
      value['pixelFormat'] != 'yuv420p' ||
      value['videoPath'] != path) {
    throw const FormatException('公开视频规范合同不符');
  }
  _hash(value['videoSha256']);
  _count(value['videoBytes'], 20 * 1024 * 1024);
  return Map.unmodifiable(value);
}

class PublicMomentMediaLocalChange {
  const PublicMomentMediaLocalChange({
    required this.ownerID,
    required this.origin,
    required this.momentID,
    required this.momentRevision,
    required this.setVersion,
    required this.action,
  });
  final String ownerID, origin, momentID, action;
  final int momentRevision, setVersion;
}

final publicMomentMediaChanges = ValueNotifier<PublicMomentMediaLocalChange?>(
  null,
);
String momentMediaPath(
  String moment,
  int revision,
  int set,
  String media,
  int version,
  String variant,
) => '/v1/moments/$moment/public-media/$revision/$set/$media/$version/$variant';
Map<String, dynamic> mediaObject(Object? value, Set<String> keys) =>
    humanControlObject(value, keys);
void _id(Object? value) {
  if (!humanControlID(value)) throw const FormatException('图片标识不符');
}

void _hash(Object? value) {
  if (!humanControlHash(value)) throw const FormatException('图片摘要不符');
}

void _version(Object? value, {bool zero = false}) {
  if (!humanControlVersion(value, zero: zero))
    throw const FormatException('图片版本不符');
}

void _count(Object? value, int max) {
  if (value is! int || value < 1 || value > max)
    throw const FormatException('图片规格不符');
}

void _validation(Map<String, dynamic> value) {
  if (value['validationKind'] !=
          (value['kind'] == 'VIDEO'
              ? publicMomentVideoValidation
              : publicMomentMediaValidation) ||
      value['validationStatus'] != 'VALIDATED' ||
      value['antivirusScan'] != 'NOT_RUN' ||
      value['privacyRisk'] != 'UNKNOWN')
    throw const FormatException('图片检查范围不符');
}

class PublicMomentMediaStage {
  PublicMomentMediaStage._(this.wire);
  final Map<String, dynamic> wire;
  String get mediaID => wire['mediaId'];
  int get version => wire['mediaVersion'];
  String get localScope => wire['localScope'];
  DateTime get retainUntil => humanControlTime(wire['retainUntil']);
  bool get current => retainUntil.isAfter(DateTime.now().toUtc());
  int get thumbnailBytes => wire['thumbnailBytes'];
  String get thumbnailSHA => wire['thumbnailSha256'];
  bool get isVideo => wire['kind'] == 'VIDEO';
  int get width => wire['width'];
  int get height => wire['height'];
  Map<String, dynamic>? get video =>
      isVideo ? Map<String, dynamic>.from(wire['video']) : null;
  factory PublicMomentMediaStage.decode(Object? raw) {
    final typed =
        raw is Map<String, dynamic> &&
        raw['schemaVersion'] == 'public-moment-media-stage-v2';
    final value = mediaObject(raw, {
      if (typed) ...{'schemaVersion', 'kind', 'purpose', 'video'},
      'mediaId',
      'mediaVersion',
      'localScope',
      'displaySha256',
      'displayBytes',
      'width',
      'height',
      'thumbnailSha256',
      'thumbnailBytes',
      'validationKind',
      'validationStatus',
      'antivirusScan',
      'privacyRisk',
      'preparedAt',
      'retainUntil',
    });
    _id(value['mediaId']);
    _id(value['localScope']);
    _version(value['mediaVersion']);
    _hash(value['displaySha256']);
    _hash(value['thumbnailSha256']);
    _count(value['displayBytes'], 1024 * 1024);
    _count(value['thumbnailBytes'], 256 * 1024);
    _count(value['width'], 1920);
    _count(value['height'], 1920);
    _validation(value);
    if (typed) {
      if (value['kind'] != 'VIDEO' ||
          value['purpose'] != publicMomentVideoPurpose) {
        throw const FormatException('视频材料用途不符');
      }
      _videoContract(
        value['video'],
        '/v1/me/moment-media-materials/${value['mediaId']}/${value['mediaVersion']}/video',
      );
      if ((value['width'] as int) > (value['height'] as int)
          ? value['width'] > 1280 || value['height'] > 720
          : value['height'] > 1280 || value['width'] > 720) {
        throw const FormatException('规范视频尺寸不符');
      }
    }
    final start = humanControlTime(value['preparedAt']),
        end = humanControlTime(value['retainUntil']);
    if (!end.isAfter(start) ||
        end.difference(start) > const Duration(minutes: 10))
      throw const FormatException('图片准备期限不符');
    return PublicMomentMediaStage._(Map.unmodifiable(value));
  }
}

class PublicMomentMediaSelection {
  const PublicMomentMediaSelection(this.mediaID, this.version, this.rights);
  final String mediaID;
  final int version;
  final EntityCoverRights rights;
  Map<String, Object> toJson() => {
    'mediaId': mediaID,
    'mediaVersion': version,
    'rights': rights.toJson(),
  };
}

class PublicMomentMediaInput {
  PublicMomentMediaInput({
    required this.momentID,
    required this.operationID,
    required this.action,
    required this.momentRevision,
    required this.setVersion,
    List<PublicMomentMediaSelection> images = const [],
  }) : images = List.unmodifiable(images);
  final String momentID, operationID, action;
  final int momentRevision, setVersion;
  final List<PublicMomentMediaSelection> images;
  String get digest => humanControlTupleDigest([
    'public-moment-media-v1',
    momentID,
    operationID,
    action,
    momentRevision,
    setVersion,
    for (final image in images)
      [image.mediaID, image.version, image.rights.digest],
  ]);
  Map<String, Object> toJson({String? source, String? approval}) => {
    'operationId': operationID,
    'action': action,
    'expectedMomentRevision': momentRevision,
    'expectedSetVersion': setVersion,
    'attachments': images.map((image) => image.toJson()).toList(),
    if (source != null) 'sourceVersion': source,
    if (approval != null) 'approval': approval,
  };
}

class PublicMomentMediaAttachment {
  PublicMomentMediaAttachment._(this.wire);
  final Map<String, dynamic> wire;
  String get mediaID => wire['mediaId'];
  int get mediaVersion => wire['mediaVersion'];
  int get position => wire['position'];
  String get thumbnailPath => wire['thumbnailPath'];
  String get displayPath => wire['displayPath'];
  String get thumbnailSHA => wire['thumbnailSha256'];
  String get displaySHA => wire['displaySha256'];
  int get thumbnailBytes => wire['thumbnailBytes'];
  int get displayBytes => wire['displayBytes'];
  int get width => wire['width'];
  int get height => wire['height'];
  DateTime get expiresAt => humanControlTime(wire['expiresAt']);
  String get attribution => wire['attribution'];
  bool get isVideo => wire['kind'] == 'VIDEO';
  Map<String, dynamic>? get video =>
      isVideo ? Map<String, dynamic>.from(wire['video']) : null;
  factory PublicMomentMediaAttachment.decode(
    Object? raw,
    String moment,
    int revision,
    int set,
    int position,
    DateTime until,
  ) {
    final typed =
        raw is Map<String, dynamic> &&
        raw['schemaVersion'] == 'public-moment-media-attachment-v2';
    final value = mediaObject(raw, {
      if (typed) ...{'schemaVersion', 'kind', 'purpose', 'video'},
      'mediaId',
      'mediaVersion',
      'position',
      'width',
      'height',
      'displayBytes',
      'displaySha256',
      'thumbnailBytes',
      'thumbnailSha256',
      'displayPath',
      'thumbnailPath',
      'rightsDigest',
      'attribution',
      'validationKind',
      'validationStatus',
      'antivirusScan',
      'privacyRisk',
      'expiresAt',
    });
    _id(value['mediaId']);
    _version(value['mediaVersion']);
    _hash(value['displaySha256']);
    _hash(value['thumbnailSha256']);
    _hash(value['rightsDigest']);
    _count(value['displayBytes'], 1024 * 1024);
    _count(value['thumbnailBytes'], 256 * 1024);
    _count(value['width'], 1920);
    _count(value['height'], 1920);
    _validation(value);
    if (typed) {
      if (value['kind'] != 'VIDEO' ||
          value['purpose'] != publicMomentVideoPurpose) {
        throw const FormatException('公开视频材料用途不符');
      }
      _videoContract(
        value['video'],
        momentMediaPath(
          moment,
          revision,
          set,
          value['mediaId'],
          value['mediaVersion'],
          'video',
        ),
      );
    }
    if (value['position'] != position ||
        value['thumbnailPath'] !=
            momentMediaPath(
              moment,
              revision,
              set,
              value['mediaId'],
              value['mediaVersion'],
              'thumbnail',
            ) ||
        value['displayPath'] !=
            momentMediaPath(
              moment,
              revision,
              set,
              value['mediaId'],
              value['mediaVersion'],
              'display',
            ) ||
        humanControlTime(value['expiresAt']) != until ||
        value['attribution'] is! String ||
        utf8.encode(value['attribution']).length > 320)
      throw const FormatException('公开图片顺序或来源不符');
    return PublicMomentMediaAttachment._(Map.unmodifiable(value));
  }
}

class PublicMomentMediaSet {
  PublicMomentMediaSet._(this.wire, this.attachments);
  final Map<String, dynamic> wire;
  final List<PublicMomentMediaAttachment> attachments;
  String get momentID => wire['momentId'];
  int get momentRevision => wire['momentRevision'];
  int get setVersion => wire['setVersion'];
  String get sourceVersion => wire['sourceVersion'];
  String get status => wire['status'];
  bool get canReplace => wire['canReplace'];
  bool get canDelete => wire['canDelete'];
  DateTime get validUntil => humanControlTime(wire['validUntil']);
  bool get current => validUntil.isAfter(DateTime.now().toUtc());
  factory PublicMomentMediaSet.decode(
    Object? raw,
    String moment, {
    int? expectedRevision,
    bool own = false,
  }) {
    final value = mediaObject(raw, {
      'schemaVersion',
      'momentId',
      'momentRevision',
      'setVersion',
      'status',
      'attachments',
      'sourceVersion',
      'observedAt',
      'validUntil',
      'canReplace',
      'canDelete',
    });
    _id(moment);
    _version(value['momentRevision']);
    _version(value['setVersion'], zero: true);
    _hash(value['sourceVersion']);
    final at = humanControlTime(value['observedAt']),
        end = humanControlTime(value['validUntil']);
    if (!const {
          'public-moment-media-set-v1',
          'public-moment-media-set-v2',
        }.contains(value['schemaVersion']) ||
        value['momentId'] != moment ||
        value['momentRevision'] < 2 ||
        expectedRevision != null &&
            value['momentRevision'] != expectedRevision ||
        value['canReplace'] is! bool ||
        value['canDelete'] is! bool ||
        !own && (value['canReplace'] == true || value['canDelete'] == true) ||
        !end.isAfter(at) ||
        end.difference(at) > const Duration(minutes: 1) ||
        !end.isAfter(DateTime.now().toUtc()) ||
        value['attachments'] is! List)
      throw const FormatException('公开图片集合来源或期限不符');
    final rows = value['attachments'] as List;
    if (rows.length > 9 ||
        value['status'] == 'READY' &&
            (rows.isEmpty || value['setVersion'] < 1) ||
        value['status'] != 'READY' &&
            (!const {'EMPTY', 'DELETED'}.contains(value['status']) ||
                rows.isNotEmpty))
      throw const FormatException('公开图片集合不完整');
    final attachments = [
      for (var i = 0; i < rows.length; i++)
        PublicMomentMediaAttachment.decode(
          rows[i],
          moment,
          value['momentRevision'],
          value['setVersion'],
          i,
          end,
        ),
    ];
    if (attachments.map((a) => a.mediaID).toSet().length != attachments.length)
      throw const FormatException('公开图片重复');
    if (attachments.any((a) => a.isVideo)
        ? value['schemaVersion'] != 'public-moment-media-set-v2' ||
              attachments.length != 1
        : value['schemaVersion'] != 'public-moment-media-set-v1') {
      throw const FormatException('一条动态只能是图片集合或一段视频');
    }
    return PublicMomentMediaSet._(
      Map.unmodifiable(value),
      List.unmodifiable(attachments),
    );
  }
}

class PublicMomentMediaPreview {
  PublicMomentMediaPreview(
    this.input,
    this.sourceVersion,
    this.approval,
    this.expiresAt,
    this.thumbnails,
    this.stages,
  );
  final PublicMomentMediaInput input;
  final String sourceVersion, approval;
  final DateTime expiresAt;
  final List<Uint8List> thumbnails;
  final List<PublicMomentMediaStage> stages;
  bool get current => expiresAt.isAfter(DateTime.now().toUtc());
  factory PublicMomentMediaPreview.decode(
    Object? raw,
    PublicMomentMediaInput input,
  ) {
    final value = mediaObject(raw, {
      'operationId',
      'action',
      'payloadDigest',
      'observedAt',
      'expiresAt',
      'approval',
      'momentId',
      'expectedMomentRevision',
      'expectedSetVersion',
      'sourceVersion',
      'images',
    });
    final expires = humanControlTime(value['expiresAt']),
        at = humanControlTime(value['observedAt']);
    _hash(value['sourceVersion']);
    if (value['operationId'] != input.operationID ||
        value['action'] != input.action ||
        value['payloadDigest'] != input.digest ||
        value['momentId'] != input.momentID ||
        value['expectedMomentRevision'] != input.momentRevision ||
        value['expectedSetVersion'] != input.setVersion ||
        value['approval'] is! String ||
        (value['approval'] as String).isEmpty ||
        !expires.isAfter(DateTime.now().toUtc()) ||
        expires.difference(at) > const Duration(seconds: 30) ||
        value['images'] is! List ||
        (value['images'] as List).length != input.images.length)
      throw const FormatException('图片集合批准不匹配');
    final thumbnails = <Uint8List>[], stages = <PublicMomentMediaStage>[];
    for (var i = 0; i < input.images.length; i++) {
      final image = mediaObject(value['images'][i], {
            'mediaId',
            'mediaVersion',
            'rights',
            'material',
            'thumbnailBase64',
          }),
          stage = PublicMomentMediaStage.decode(image['material']),
          selection = input.images[i];
      if (image['mediaId'] != selection.mediaID ||
          image['mediaVersion'] != selection.version ||
          stage.mediaID != selection.mediaID ||
          stage.version != selection.version ||
          jsonEncode(image['rights']) !=
              jsonEncode(selection.rights.toJson()) ||
          !stage.current)
        throw const FormatException('批准中的图片或权利已变化');
      final pixels = base64Decode(image['thumbnailBase64']);
      momentMediaJPEG(
        pixels,
        stage.thumbnailBytes,
        stage.thumbnailSHA,
        320,
        180,
      );
      thumbnails.add(pixels);
      stages.add(stage);
    }
    return PublicMomentMediaPreview(
      input,
      value['sourceVersion'],
      value['approval'],
      expires,
      List.unmodifiable(thumbnails),
      List.unmodifiable(stages),
    );
  }
}

void momentMediaJPEG(
  Uint8List bytes,
  int count,
  String digest,
  int width,
  int height,
) {
  if (bytes.length != count ||
      bytes.isEmpty ||
      bytes.length > 1024 * 1024 ||
      sha256.convert(bytes).toString() != digest)
    throw const FormatException('图片字节校验失败');
  final header = MomentImageHeader.inspect(bytes);
  if (header.mime != 'image/jpeg' ||
      header.width != width ||
      header.height != height)
    throw const FormatException('图片规格校验失败');
}

class PublicMomentMediaSetAPI {
  PublicMomentMediaSetAPI({
    http.Client? client,
    String? apiBaseUrl,
    String? Function()? authorizationHeader,
    bool Function()? current,
  }) : _client = client ?? http.Client(),
       _owns = client == null,
       origin = (apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).replaceFirst(
         RegExp(r'/$'),
         '',
       ),
       authorizationHeader = authorizationHeader ?? (() => null),
       current = current ?? (() => true);
  final http.Client _client;
  final bool _owns;
  final String origin;
  final String? Function() authorizationHeader;
  final bool Function() current;
  Future<Uint8List> _read(
    http.StreamedResponse response,
    int maximum,
    bool Function() valid,
  ) async {
    if (response.contentLength != null && response.contentLength! > maximum) {
      await response.stream.listen(null).cancel();
      throw const FormatException('图片响应过大');
    }
    final iterator = StreamIterator<List<int>>(response.stream),
        bytes = BytesBuilder(copy: true),
        deadline = DateTime.now().add(const Duration(seconds: 25));
    try {
      while (true) {
        final left = deadline.difference(DateTime.now());
        if (left <= Duration.zero) throw TimeoutException('图片读取超时');
        if (!await iterator.moveNext().timeout(left)) break;
        if (!valid() || bytes.length + iterator.current.length > maximum)
          throw const FormatException('图片来源变化或响应过大');
        bytes.add(iterator.current);
      }
      if (!valid()) throw const FormatException('图片来源已变化');
      return bytes.takeBytes();
    } catch (_) {
      final discarded = bytes.takeBytes();
      discarded.fillRange(0, discarded.length, 0);
      rethrow;
    } finally {
      await iterator.cancel();
    }
  }

  Future<Object?> request(
    String method,
    String path, {
    Map<String, Object>? body,
  }) async {
    if (!current()) throw const FormatException('图片来源已退休');
    final token = authorizationHeader(),
        headers = <String, String>{
          'X-Birdtie-Public-Media-Context': 'typed-v2',
          if (token != null) 'Authorization': token,
          if (body != null) 'Content-Type': 'application/json',
        };
    final request = http.Request(method, Uri.parse('$origin$path'))
      ..headers.addAll(headers)
      ..followRedirects = false;
    if (body != null) request.body = jsonEncode(body);
    final response = await _client
        .send(request)
        .timeout(
          Duration(
            seconds: path.contains('/moment-media-video-materials/') ? 80 : 25,
          ),
        );
    final maximum = path.endsWith('/previews')
        ? ((9 * 256 * 1024 + 2) ~/ 3 * 4 + 65536)
        : 65536;
    final bytes = await _read(
      response,
      maximum,
      () => current() && token == authorizationHeader(),
    );
    if (response.statusCode < 200 || response.statusCode >= 300)
      throw MomentMediaHTTPFailure(response.statusCode);
    if (!(response.headers['content-type'] ?? '').startsWith(
      'application/json',
    ))
      throw const FormatException('图片响应类型不符');
    final value = jsonDecode(utf8.decode(bytes));
    return mediaObject(value, {'data'})['data'];
  }

  Future<PublicMomentMediaSet> ownSet(String id, int revision) async =>
      PublicMomentMediaSet.decode(
        await request('GET', '/v1/me/moments/$id/public-media'),
        id,
        expectedRevision: revision,
        own: true,
      );
  Future<PublicMomentMediaSet> publicSet(
    String id, {
    int? expectedRevision,
  }) async => PublicMomentMediaSet.decode(
    await request('GET', '/v1/moments/$id/public-media'),
    id,
    expectedRevision: expectedRevision,
  );
  Future<Uint8List> content(
    PublicMomentMediaAttachment image, {
    bool thumbnail = false,
  }) async {
    if (!current() || !image.expiresAt.isAfter(DateTime.now().toUtc()))
      throw const FormatException('图片已到期');
    final token = authorizationHeader(),
        request = http.Request(
          'GET',
          Uri.parse(
            '$origin${thumbnail ? image.thumbnailPath : image.displayPath}',
          ),
        )..followRedirects = false;
    if (token != null) request.headers['Authorization'] = token;
    final response = await _client
            .send(request)
            .timeout(const Duration(seconds: 25)),
        maximum = thumbnail ? image.thumbnailBytes : image.displayBytes;
    final bytes = await _read(
      response,
      maximum,
      () =>
          current() &&
          token == authorizationHeader() &&
          image.expiresAt.isAfter(DateTime.now().toUtc()),
    );
    try {
      if (response.statusCode != 200 ||
          response.headers['content-type'] != 'image/jpeg')
        throw const FormatException('公开图片暂不可读取');
      momentMediaJPEG(
        bytes,
        maximum,
        thumbnail ? image.thumbnailSHA : image.displaySHA,
        thumbnail ? 320 : image.width,
        thumbnail ? 180 : image.height,
      );
      return bytes;
    } catch (_) {
      bytes.fillRange(0, bytes.length, 0);
      rethrow;
    }
  }

  Future<Uint8List> _videoRead(
    String path,
    int count,
    String digest,
    String mime,
    DateTime until,
  ) async {
    if (!current() || !until.isAfter(DateTime.now().toUtc())) {
      throw const FormatException('视频资料已到期');
    }
    final token = authorizationHeader(),
        request = http.Request('GET', Uri.parse('$origin$path'))
          ..followRedirects = false;
    if (token != null) request.headers['Authorization'] = token;
    final response = await _client
        .send(request)
        .timeout(const Duration(seconds: 25));
    final bytes = await _read(
      response,
      count,
      () =>
          current() &&
          authorizationHeader() == token &&
          until.isAfter(DateTime.now().toUtc()),
    );
    if (response.statusCode != 200 ||
        response.headers['content-type'] != mime ||
        bytes.length != count ||
        sha256.convert(bytes).toString() != digest) {
      bytes.fillRange(0, bytes.length, 0);
      throw const FormatException('当前视频字节校验失败');
    }
    try {
      if (mime == 'video/mp4') {
        validateLocalVideoMP4(bytes);
      }
      return bytes;
    } catch (_) {
      bytes.fillRange(0, bytes.length, 0);
      rethrow;
    }
  }

  Future<Uint8List> videoContent(PublicMomentMediaAttachment attachment) async {
    final v = attachment.video;
    if (v == null) {
      throw const FormatException('当前材料不是视频');
    }
    return _videoRead(
      v['videoPath'],
      v['videoBytes'],
      v['videoSha256'],
      'video/mp4',
      attachment.expiresAt,
    );
  }

  Future<Uint8List> stageVideoContent(
    PublicMomentMediaStage stage, {
    bool thumbnail = false,
  }) async {
    final v = stage.video;
    if (v == null) {
      throw const FormatException('当前材料不是视频');
    }
    final variant = thumbnail ? 'thumbnail' : 'video',
        path =
            '/v1/me/moment-media-materials/${stage.mediaID}/${stage.version}/$variant';
    final bytes = await _videoRead(
      path,
      thumbnail ? stage.thumbnailBytes : v['videoBytes'],
      thumbnail ? stage.thumbnailSHA : v['videoSha256'],
      thumbnail ? 'image/jpeg' : 'video/mp4',
      stage.retainUntil,
    );
    try {
      if (thumbnail) {
        momentMediaJPEG(
          bytes,
          stage.thumbnailBytes,
          stage.thumbnailSHA,
          320,
          180,
        );
      }
      return bytes;
    } catch (_) {
      bytes.fillRange(0, bytes.length, 0);
      rethrow;
    }
  }

  void dispose() {
    if (_owns) _client.close();
  }
}

class MomentMediaHTTPFailure implements Exception {
  const MomentMediaHTTPFailure(this.status);
  final int status;
  bool get uncertain => status == 408 || status >= 500;
}

class PublicMomentMediaGallery extends StatefulWidget {
  const PublicMomentMediaGallery({
    super.key,
    required this.set,
    required this.api,
    this.current,
  });
  final PublicMomentMediaSet set;
  final PublicMomentMediaSetAPI api;
  final bool Function()? current;
  @override
  State<PublicMomentMediaGallery> createState() =>
      _PublicMomentMediaGalleryState();
}

class _PublicMomentMediaGalleryState extends State<PublicMomentMediaGallery> {
  Timer? _expiry;
  bool _expired = false;
  int _epoch = 0;
  final _reads = <String, Future<Uint8List>>{};
  final _pixels = <String, Uint8List>{};
  void _retirePixels() {
    ++_epoch;
    for (final bytes in _pixels.values) {
      bytes.fillRange(0, bytes.length, 0);
    }
    _pixels.clear();
    _reads.clear();
  }

  void _arm() {
    _expiry?.cancel();
    _retirePixels();
    _expired = !widget.set.current;
    if (!_expired)
      _expiry = Timer(
        widget.set.validUntil.difference(DateTime.now().toUtc()),
        () {
          if (mounted)
            setState(() {
              _expired = true;
              _retirePixels();
            });
        },
      );
  }

  Future<Uint8List> _content(PublicMomentMediaAttachment image) async {
    final epoch = _epoch;
    final bytes = await widget.api.content(image);
    if (!mounted ||
        epoch != _epoch ||
        _expired ||
        widget.current?.call() == false ||
        !widget.api.current()) {
      bytes.fillRange(0, bytes.length, 0);
      throw const FormatException('图片来源已退休');
    }
    _pixels[image.displayPath] = bytes;
    return bytes;
  }

  @override
  void initState() {
    super.initState();
    _arm();
  }

  @override
  void didUpdateWidget(PublicMomentMediaGallery old) {
    super.didUpdateWidget(old);
    if (old.set.sourceVersion != widget.set.sourceVersion ||
        old.set.validUntil != widget.set.validUntil ||
        old.api != widget.api)
      _arm();
  }

  @override
  Widget build(BuildContext context) {
    if (_expired || widget.current?.call() == false || !widget.api.current()) {
      _retirePixels();
      return const Text('图片资料已到期，请重新读取。');
    }
    return Column(
      children: [
        for (final image in widget.set.attachments)
          if (image.isVideo)
            PublicMomentVideoPlayer(
              key: ValueKey('${widget.set.sourceVersion}:${image.mediaID}'),
              attachment: image,
              api: widget.api,
              sourceVersion: widget.set.sourceVersion,
              current: () =>
                  !_expired &&
                  widget.current?.call() != false &&
                  widget.set.current,
            )
          else
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: FutureBuilder<Uint8List>(
                future: _reads.putIfAbsent(
                  image.displayPath,
                  () => _content(image),
                ),
                builder: (context, snapshot) => snapshot.hasData
                    ? Semantics(
                        label:
                            '动态图片 ${image.position + 1}，共 ${widget.set.attachments.length} 张',
                        image: true,
                        child: Image.memory(
                          snapshot.data!,
                          fit: BoxFit.contain,
                          gaplessPlayback: false,
                        ),
                      )
                    : AspectRatio(
                        aspectRatio: image.width / image.height,
                        child: Center(
                          child: Text(
                            snapshot.hasError ? '这张图片暂时无法读取。' : '正在读取图片…',
                          ),
                        ),
                      ),
              ),
            ),
      ],
    );
  }

  @override
  void dispose() {
    _expiry?.cancel();
    _retirePixels();
    super.dispose();
  }
}
