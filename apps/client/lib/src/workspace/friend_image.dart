import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import '../content/moment_image_header.dart';
import 'chat_message_operation.dart';
import 'entity_share_pending_store.dart' show chatEntityUUID;
import 'local_image_draft.dart' show rejectAnimatedLocalImage;

const friendImagePurpose = 'HUMAN_CONVERSATION_IMAGE_ATTACHMENT';
const friendImageReceiptSchema = 'human.image_message_operation.v1';
const friendImageMetadataSchema = 'human.conversation_image.v1';
const maxFriendImageInputBytes = 10485760;
const maxFriendImageFullBytes = 16777216;
const maxFriendImageThumbnailBytes = 1048576;
const maxFriendImageCaptionBytes = 2000;
const maxFriendImagePostBytes = 13981016 + 16384;

bool friendImageID(String value) =>
    chatEntityUUID.hasMatch(value) &&
    value == value.toLowerCase() &&
    value != '00000000-0000-0000-0000-000000000000';
final _imageHex = RegExp(r'^[0-9a-f]{64}$');
bool friendImageCaption(String value) {
  if (value.contains('\u0000')) return false;
  final units = value.codeUnits;
  for (var i = 0; i < units.length; i++) {
    final unit = units[i];
    if (unit >= 0xd800 && unit <= 0xdbff) {
      if (++i >= units.length || units[i] < 0xdc00 || units[i] > 0xdfff)
        return false;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      return false;
    }
  }
  return utf8.encode(value).length <= maxFriendImageCaptionBytes;
}

DateTime? _imageStamp(String value) {
  final match = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d{1,9})?Z$',
  ).firstMatch(value);
  if (match == null) return null;
  final fields = [for (var i = 1; i <= 6; i++) int.parse(match.group(i)!)];
  final at = DateTime.tryParse(value);
  if (at == null ||
      fields[0] < 2020 ||
      fields[0] > 2200 ||
      at.year != fields[0] ||
      at.month != fields[1] ||
      at.day != fields[2] ||
      fields[3] > 23 ||
      fields[4] > 59 ||
      fields[5] > 59)
    return null;
  return at.toUtc();
}

Map<String, dynamic> _closedImageObject(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      raw.keys.toSet().difference(keys).isNotEmpty) {
    throw const FormatException('图片来源字段无法核验');
  }
  return raw;
}

/// jsonDecode alone silently replaces duplicate object keys. Scan only keys,
/// then let the standard decoder validate the complete JSON value.
Object? decodeFriendImageJson(String text) {
  final scan = _JsonKeys(text);
  scan.value(0);
  scan.space();
  if (scan.index != text.length) throw const FormatException('图片 JSON 不完整');
  return jsonDecode(text);
}

class _JsonKeys {
  _JsonKeys(this.text);
  final String text;
  int index = 0;
  void space() {
    while (index < text.length &&
        const {9, 10, 13, 32}.contains(text.codeUnitAt(index))) {
      ++index;
    }
  }

  String string() {
    final start = index++;
    var escaped = false;
    while (index < text.length) {
      final c = text.codeUnitAt(index++);
      if (escaped) {
        escaped = false;
        continue;
      }
      if (c == 92) {
        escaped = true;
        continue;
      }
      if (c == 34) return jsonDecode(text.substring(start, index)) as String;
    }
    throw const FormatException('图片 JSON 字符串不完整');
  }

  void value(int depth) {
    if (depth > 12) throw const FormatException('图片 JSON 层级过深');
    space();
    if (index >= text.length) throw const FormatException('图片 JSON 不完整');
    final char = text[index];
    if (char == '"') {
      string();
      return;
    }
    if (char == '{') {
      ++index;
      final keys = <String>{};
      space();
      if (index < text.length && text[index] == '}') {
        ++index;
        return;
      }
      while (true) {
        space();
        if (index >= text.length || text[index] != '"') {
          throw const FormatException('图片 JSON 对象无效');
        }
        if (!keys.add(string())) throw const FormatException('图片 JSON 字段重复');
        space();
        if (index >= text.length || text[index++] != ':') {
          throw const FormatException('图片 JSON 对象无效');
        }
        value(depth + 1);
        space();
        if (index >= text.length) throw const FormatException('图片 JSON 不完整');
        final next = text[index++];
        if (next == '}') return;
        if (next != ',') throw const FormatException('图片 JSON 对象无效');
      }
    }
    if (char == '[') {
      ++index;
      space();
      if (index < text.length && text[index] == ']') {
        ++index;
        return;
      }
      while (true) {
        value(depth + 1);
        space();
        if (index >= text.length) throw const FormatException('图片 JSON 不完整');
        final next = text[index++];
        if (next == ']') return;
        if (next != ',') throw const FormatException('图片 JSON 数组无效');
      }
    }
    final start = index;
    while (index < text.length &&
        !const {',', '}', ']', ' ', '\n', '\r', '\t'}.contains(text[index])) {
      ++index;
    }
    if (index == start) throw const FormatException('图片 JSON 值无效');
  }
}

class FriendImageDerivative {
  const FriendImageDerivative({
    required this.mimeType,
    required this.byteSize,
    required this.sha256,
    required this.width,
    required this.height,
  });
  final String mimeType, sha256;
  final int byteSize, width, height;
  Map<String, Object> toJson() => {
    'mimeType': mimeType,
    'byteSize': byteSize,
    'sha256': sha256,
    'width': width,
    'height': height,
  };
  static FriendImageDerivative decode(Object? raw, {required bool thumbnail}) {
    final v = _closedImageObject(raw, const {
      'mimeType',
      'byteSize',
      'sha256',
      'width',
      'height',
    });
    final maxBytes = thumbnail
        ? maxFriendImageThumbnailBytes
        : maxFriendImageFullBytes;
    final maxSide = thumbnail ? 256 : 8192;
    if (!(thumbnail
            ? v['mimeType'] == 'image/png'
            : const {'image/png', 'image/jpeg'}.contains(v['mimeType'])) ||
        v['byteSize'] is! int ||
        v['byteSize'] < 1 ||
        v['byteSize'] > maxBytes ||
        v['sha256'] is! String ||
        !_imageHex.hasMatch(v['sha256']) ||
        v['width'] is! int ||
        v['width'] < 1 ||
        v['width'] > maxSide ||
        v['height'] is! int ||
        v['height'] < 1 ||
        v['height'] > maxSide ||
        v['width'] * v['height'] > 16777216) {
      throw const FormatException('图片派生元数据无法核验');
    }
    return FriendImageDerivative(
      mimeType: v['mimeType'],
      byteSize: v['byteSize'],
      sha256: v['sha256'],
      width: v['width'],
      height: v['height'],
    );
  }

  bool same(FriendImageDerivative other) =>
      mimeType == other.mimeType &&
      byteSize == other.byteSize &&
      sha256 == other.sha256 &&
      width == other.width &&
      height == other.height;
}

class FriendImageMetadata {
  const FriendImageMetadata({required this.full, required this.thumbnail});
  final FriendImageDerivative full, thumbnail;
  Map<String, Object> toJson() => {
    'schemaVersion': friendImageMetadataSchema,
    ...full.toJson(),
    'thumbnail': thumbnail.toJson(),
  };
  factory FriendImageMetadata.decode(Object? raw) {
    final v = _closedImageObject(raw, const {
      'schemaVersion',
      'mimeType',
      'byteSize',
      'sha256',
      'width',
      'height',
      'thumbnail',
    });
    if (v['schemaVersion'] != friendImageMetadataSchema) {
      throw const FormatException('图片元数据版本不符');
    }
    final full = FriendImageDerivative.decode({
      for (final key in const [
        'mimeType',
        'byteSize',
        'sha256',
        'width',
        'height',
      ])
        key: v[key],
    }, thumbnail: false);
    final thumb = FriendImageDerivative.decode(v['thumbnail'], thumbnail: true);
    final long = full.width > full.height ? full.width : full.height;
    final expectedWidth = long <= 256
        ? full.width
        : (full.width * 256 + long ~/ 2) ~/ long;
    final expectedHeight = long <= 256
        ? full.height
        : (full.height * 256 + long ~/ 2) ~/ long;
    if (thumb.width != (expectedWidth < 1 ? 1 : expectedWidth) ||
        thumb.height != (expectedHeight < 1 ? 1 : expectedHeight)) {
      throw const FormatException('缩略图比例与原图不符');
    }
    return FriendImageMetadata(full: full, thumbnail: thumb);
  }
  bool same(FriendImageMetadata other) =>
      full.same(other.full) && thumbnail.same(other.thumbnail);
}

class FriendImageDraft {
  FriendImageDraft(
    Uint8List value, {
    required this.width,
    required this.height,
    this.pixelRisk = 'UNKNOWN',
  }) : bytes = Uint8List.fromList(value).asUnmodifiableView(),
       inputSha256 = sha256.convert(value).toString() {
    if (value.isEmpty ||
        value.length > maxFriendImageInputBytes ||
        !const {'UNKNOWN', 'USER_MASKED'}.contains(pixelRisk)) {
      throw const FormatException('本机图片预览无法核验');
    }
    final h = MomentImageHeader.inspect(value);
    rejectAnimatedLocalImage(value);
    if (h.mime != 'image/png' || h.width != width || h.height != height) {
      throw const FormatException('发送内容不是已预览的本机 PNG');
    }
  }
  final Uint8List bytes;
  final int width, height;
  final String pixelRisk, inputSha256;
  String get mimeType => 'image/png';
  int get inputSize => bytes.length;
}

String friendImagePayloadDigest({
  required String conversation,
  required String recipient,
  required String mimeType,
  required int inputSize,
  required String inputSha256,
  required String pixelRisk,
  required String caption,
}) => sha256
    .convert(
      utf8.encode(
        [
              'birdtie.human-image-payload.v1',
              conversation,
              recipient,
              friendImagePurpose,
              mimeType,
              inputSize.toString(),
              inputSha256,
              pixelRisk,
              utf8.encode(caption).length.toString(),
            ].join('\n') +
            '\n' +
            caption,
      ),
    )
    .toString();

/// Metadata journal only: no pixels, raw caption, approval or auth credentials.
class PendingFriendImage {
  const PendingFriendImage({
    required this.operationID,
    required this.conversationID,
    required this.recipientID,
    required this.inputMimeType,
    required this.inputSize,
    required this.inputSha256,
    required this.pixelRisk,
    required this.captionSha256,
    required this.captionSize,
    required this.payloadDigest,
  });
  final String operationID, conversationID, recipientID, inputMimeType;
  final String inputSha256, pixelRisk, captionSha256, payloadDigest;
  final int inputSize, captionSize;
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'conversationId': conversationID,
    'recipientAccountId': recipientID,
    'inputMimeType': inputMimeType,
    'inputSize': inputSize,
    'inputSha256': inputSha256,
    'pixelRisk': pixelRisk,
    'captionSha256': captionSha256,
    'captionSize': captionSize,
    'payloadDigest': payloadDigest,
  };
  factory PendingFriendImage.decode(Object? raw) {
    final v = _closedImageObject(raw, const {
      'operationId',
      'conversationId',
      'recipientAccountId',
      'inputMimeType',
      'inputSize',
      'inputSha256',
      'pixelRisk',
      'captionSha256',
      'captionSize',
      'payloadDigest',
    });
    if (v['operationId'] is! String ||
        !friendImageID(v['operationId']) ||
        v['conversationId'] is! String ||
        !friendImageID(v['conversationId']) ||
        v['recipientAccountId'] is! String ||
        !friendImageID(v['recipientAccountId']) ||
        !const {'image/png', 'image/jpeg'}.contains(v['inputMimeType']) ||
        v['inputSize'] is! int ||
        v['inputSize'] < 1 ||
        v['inputSize'] > maxFriendImageInputBytes ||
        v['captionSize'] is! int ||
        v['captionSize'] < 0 ||
        v['captionSize'] > maxFriendImageCaptionBytes ||
        !const {'UNKNOWN', 'USER_MASKED'}.contains(v['pixelRisk']) ||
        [
          'inputSha256',
          'captionSha256',
          'payloadDigest',
        ].any((key) => v[key] is! String || !_imageHex.hasMatch(v[key]))) {
      throw const FormatException('图片发送记录无法核验');
    }
    return PendingFriendImage(
      operationID: v['operationId'],
      conversationID: v['conversationId'],
      recipientID: v['recipientAccountId'],
      inputMimeType: v['inputMimeType'],
      inputSize: v['inputSize'],
      inputSha256: v['inputSha256'],
      pixelRisk: v['pixelRisk'],
      captionSha256: v['captionSha256'],
      captionSize: v['captionSize'],
      payloadDigest: v['payloadDigest'],
    );
  }
  factory PendingFriendImage.review({
    required String operationID,
    required String conversationID,
    required String recipientID,
    required FriendImageDraft draft,
    required String caption,
  }) {
    if (!friendImageCaption(caption))
      throw const FormatException('图片文字超过 2000 字节');
    return PendingFriendImage.decode({
      'operationId': operationID,
      'conversationId': conversationID,
      'recipientAccountId': recipientID,
      'inputMimeType': draft.mimeType,
      'inputSize': draft.inputSize,
      'inputSha256': draft.inputSha256,
      'pixelRisk': draft.pixelRisk,
      'captionSha256': sha256.convert(utf8.encode(caption)).toString(),
      'captionSize': utf8.encode(caption).length,
      'payloadDigest': friendImagePayloadDigest(
        conversation: conversationID,
        recipient: recipientID,
        mimeType: draft.mimeType,
        inputSize: draft.inputSize,
        inputSha256: draft.inputSha256,
        pixelRisk: draft.pixelRisk,
        caption: caption,
      ),
    });
  }
  bool matches(FriendImageDraft draft, String caption) =>
      friendImageCaption(caption) &&
      inputMimeType == draft.mimeType &&
      inputSize == draft.inputSize &&
      inputSha256 == draft.inputSha256 &&
      pixelRisk == draft.pixelRisk &&
      captionSize == utf8.encode(caption).length &&
      captionSha256 == sha256.convert(utf8.encode(caption)).toString() &&
      payloadDigest ==
          friendImagePayloadDigest(
            conversation: conversationID,
            recipient: recipientID,
            mimeType: draft.mimeType,
            inputSize: draft.inputSize,
            inputSha256: draft.inputSha256,
            pixelRisk: draft.pixelRisk,
            caption: caption,
          );
}

class FriendImageReceipt {
  const FriendImageReceipt(this.messageID, this.createdAt, this.image);
  final String messageID;
  final DateTime createdAt;
  final FriendImageMetadata image;
  static FriendImageReceipt decode(
    Object? raw,
    String owner,
    PendingFriendImage p,
  ) {
    final v = _closedImageObject(raw, const {
      'schemaVersion',
      'ownerId',
      'conversationId',
      'recipientAccountId',
      'operationId',
      'effectKey',
      'payloadDigest',
      'messageId',
      'inputMimeType',
      'inputSize',
      'inputSha256',
      'pixelRisk',
      'captionSha256',
      'purpose',
      'createdAt',
      'image',
    });
    if (!friendImageID(owner) ||
        v['schemaVersion'] != friendImageReceiptSchema ||
        v['ownerId'] != owner ||
        v['conversationId'] != p.conversationID ||
        v['recipientAccountId'] != p.recipientID ||
        v['operationId'] != p.operationID ||
        v['effectKey'] != humanMessageEffectKey(owner, p.operationID) ||
        v['payloadDigest'] != p.payloadDigest ||
        v['purpose'] != friendImagePurpose ||
        v['inputMimeType'] != p.inputMimeType ||
        v['inputSize'] != p.inputSize ||
        v['inputSha256'] != p.inputSha256 ||
        v['pixelRisk'] != p.pixelRisk ||
        v['captionSha256'] != p.captionSha256 ||
        v['messageId'] is! String ||
        !friendImageID(v['messageId']) ||
        v['createdAt'] is! String ||
        !(v['createdAt'] as String).endsWith('Z')) {
      throw const FormatException('图片回执与已展示版本不符');
    }
    final at = _imageStamp(v['createdAt']);
    if (at == null) {
      throw const FormatException('图片回执时间无法核验');
    }
    // The server re-encodes pixels: final SHA is deliberately independent of input SHA.
    return FriendImageReceipt(
      v['messageId'],
      at.toUtc(),
      FriendImageMetadata.decode(v['image']),
    );
  }
}

abstract interface class FriendImagePendingStore {
  Future<List<PendingFriendImage>> read(String environment, String owner);
  Future<void> write(
    String environment,
    String owner,
    PendingFriendImage pending,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingFriendImage pending,
  );
}

class SecureFriendImagePendingStore implements FriendImagePendingStore {
  SecureFriendImagePendingStore({this.storage = const FlutterSecureStorage()});
  final FlutterSecureStorage storage;
  static Future<void> _tail = Future.value();
  Future<T> _exclusive<T>(Future<T> Function() work) {
    final next = _tail.then((_) => work());
    _tail = next.then<void>((_) {}, onError: (Object _, StackTrace _) {});
    return next;
  }

  String _prefix(String environment, String owner) {
    final uri = Uri.tryParse(environment);
    if (uri == null ||
        !const {'http', 'https'}.contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment ||
        !friendImageID(owner)) {
      throw const FormatException('图片恢复分区无效');
    }
    return 'birdtie.human-image.pending.v1.' +
        sha256.convert(utf8.encode(environment + '\n' + owner)).toString() +
        '.';
  }

  @override
  Future<List<PendingFriendImage>> read(String environment, String owner) =>
      _exclusive(() async {
        final prefix = _prefix(environment, owner);
        final values = await storage.readAll();
        final selected = values.entries.where((e) => e.key.startsWith(prefix));
        if (selected.length > 64) throw const FormatException('图片恢复记录过多');
        return selected
            .map((e) {
              if (e.value.length > 4096)
                throw const FormatException('图片恢复记录过大');
              final pending = PendingFriendImage.decode(
                decodeFriendImageJson(e.value),
              );
              if (e.key != prefix + pending.operationID) {
                throw const FormatException('图片恢复地址不符');
              }
              return pending;
            })
            .toList(growable: false);
      });
  @override
  Future<void> write(String environment, String owner, PendingFriendImage p) =>
      _exclusive(() async {
        PendingFriendImage.decode(p.toJson());
        final key = _prefix(environment, owner) + p.operationID;
        final value = jsonEncode(p.toJson()),
            previous = await storage.read(key: key);
        if (previous != null && previous != value) {
          throw const FormatException('原图片操作已绑定另一版本');
        }
        if (previous == null) {
          final prefix = _prefix(environment, owner),
              all = await storage.readAll();
          if (all.keys.where((key) => key.startsWith(prefix)).length >= 64) {
            throw const FormatException('未确认图片记录过多，请先核实原发送');
          }
        }
        await storage.write(key: key, value: value);
      });
  @override
  Future<void> delete(String environment, String owner, PendingFriendImage p) =>
      _exclusive(() async {
        PendingFriendImage.decode(p.toJson());
        final key = _prefix(environment, owner) + p.operationID;
        final previous = await storage.read(key: key);
        if (previous != null && previous != jsonEncode(p.toJson())) {
          throw const FormatException('图片恢复版本不符');
        }
        await storage.delete(key: key);
      });
}

class MemoryFriendImagePendingStore implements FriendImagePendingStore {
  final values = <(String, String, String), PendingFriendImage>{};
  @override
  Future<List<PendingFriendImage>> read(String e, String o) async => values
      .entries
      .where((v) => v.key.$1 == e && v.key.$2 == o)
      .map((v) => v.value)
      .toList(growable: false);
  @override
  Future<void> write(String e, String o, PendingFriendImage p) async {
    PendingFriendImage.decode(p.toJson());
    final key = (e, o, p.operationID), old = values[key];
    if (old != null && jsonEncode(old.toJson()) != jsonEncode(p.toJson())) {
      throw const FormatException('原图片操作已绑定另一版本');
    }
    if (old == null &&
        values.keys.where((key) => key.$1 == e && key.$2 == o).length >= 64) {
      throw const FormatException('未确认图片记录过多，请先核实原发送');
    }
    values[key] = p;
  }

  @override
  Future<void> delete(String e, String o, PendingFriendImage p) async {
    final key = (e, o, p.operationID), old = values[key];
    if (old != null && jsonEncode(old.toJson()) != jsonEncode(p.toJson())) {
      throw const FormatException('图片恢复版本不符');
    }
    values.remove(key);
  }
}

enum FriendImageVariant { thumbnail, full }

class FriendImageDeliveryFailure implements Exception {
  const FriendImageDeliveryFailure(this.status);
  final int status;
}

class _ImageRead {
  final abortSignal = Completer<void>();
  StreamSubscription<List<int>>? subscription;
  void Function()? onAbort;
  void abort() {
    if (!abortSignal.isCompleted) abortSignal.complete();
    final callback = onAbort;
    onAbort = null;
    callback?.call();
    unawaited(subscription?.cancel());
  }
}

/// Uses the caller's shared authenticated transport. Default disabled until the
/// matching image API/migration and client have actually been verified together.
class FriendImageSource {
  FriendImageSource({
    required this.client,
    required this.environment,
    required this.authorizationHeader,
    required this.current,
    required this.binding,
    this.enabled = false,
    this.timeout = const Duration(seconds: 12),
  });
  final http.Client client;
  final String environment;
  final String? Function() authorizationHeader;
  final bool Function() current;
  final Object? Function() binding;
  final bool enabled;
  final Duration timeout;
  final _reads = <_ImageRead>{};
  bool _closed = false;
  int _epoch = 0;
  void cancelRequests() {
    ++_epoch;
    for (final read in List.of(_reads)) {
      read.abort();
    }
  }

  Uri _url(String path) => Uri.parse(environment + path);
  void _checkAddress(String conversation, [String? message]) {
    if (!friendImageID(conversation) ||
        message != null && !friendImageID(message)) {
      throw const FormatException('图片地址无法核验');
    }
  }

  Future<Uint8List> _request(
    String method,
    Uri url,
    int status,
    int maximum, {
    Uint8List? body,
    void Function(http.StreamedResponse response)? inspect,
  }) async {
    final token = authorizationHeader(), scope = binding(), epoch = _epoch;
    if (!enabled || _closed || token == null || !current()) {
      throw StateError('当前版本的好友图片服务尚未启用');
    }
    final read = _ImageRead();
    _reads.add(read);
    bool valid() =>
        !_closed &&
        enabled &&
        !read.abortSignal.isCompleted &&
        epoch == _epoch &&
        scope == binding() &&
        current() &&
        authorizationHeader() == token;
    Future<Uint8List> run() async {
      final request =
          http.AbortableRequest(
              method,
              url,
              abortTrigger: read.abortSignal.future,
            )
            ..headers.addAll({
              'Authorization': token,
              'Content-Type': 'application/json',
            });
      if (body != null) request.bodyBytes = body;
      final response = await client.send(request);
      if (!valid() || response.statusCode != status) {
        read.abort();
        await response.stream.listen(null).cancel();
        if (!valid() && response.statusCode == status) {
          throw StateError('图片读取对象已变化');
        }
        throw FriendImageDeliveryFailure(response.statusCode);
      }
      try {
        inspect?.call(response);
      } catch (_) {
        read.abort();
        await response.stream.listen(null).cancel();
        rethrow;
      }
      final out = BytesBuilder(copy: false), done = Completer<Uint8List>();
      void fail(Object error, [StackTrace? stack]) {
        if (!done.isCompleted) done.completeError(error, stack);
        final discarded = out.takeBytes();
        discarded.fillRange(0, discarded.length, 0);
        read.abort();
      }

      read.onAbort = () {
        if (!done.isCompleted) fail(StateError('图片读取已取消'));
      };
      read.subscription = response.stream.listen(
        (chunk) {
          if (!valid()) {
            fail(StateError('图片读取对象已变化'));
            return;
          }
          if (chunk.length > maximum - out.length) {
            fail(const FormatException('图片响应超过处理范围'));
            return;
          }
          out.add(Uint8List.fromList(chunk));
        },
        onError: (Object error, StackTrace stack) {
          fail(error, stack);
        },
        onDone: () {
          if (!valid()) {
            fail(StateError('图片读取对象已变化'));
            return;
          }
          if (!done.isCompleted) done.complete(out.takeBytes());
        },
        cancelOnError: true,
      );
      return done.future;
    }

    try {
      return await run().timeout(
        timeout,
        onTimeout: () {
          read.abort();
          throw TimeoutException('图片请求尚未确认', timeout);
        },
      );
    } finally {
      read.abort();
      _reads.remove(read);
    }
  }

  Future<FriendImageReceipt> _receipt(
    String method,
    Uri url,
    int status,
    String owner,
    PendingFriendImage p, {
    Uint8List? body,
  }) async {
    final bytes = await _request(
      method,
      url,
      status,
      16384,
      body: body,
      inspect: (r) {
        if (!(r.headers['content-type'] ?? '').toLowerCase().startsWith(
              'application/json',
            ) ||
            r.headers['cache-control'] != 'no-store') {
          throw const FormatException('图片回执来源无法核验');
        }
      },
    );
    final root = _closedImageObject(
      decodeFriendImageJson(utf8.decode(bytes)),
      const {'data'},
    );
    return FriendImageReceipt.decode(root['data'], owner, p);
  }

  Future<FriendImageReceipt> send(
    String owner,
    PendingFriendImage p,
    FriendImageDraft draft,
    String caption,
  ) {
    _checkAddress(p.conversationID);
    PendingFriendImage.decode(p.toJson());
    if (!friendImageID(owner) || !p.matches(draft, caption)) {
      throw const FormatException('图片或文字与原批准不符');
    }
    final encoded = Uint8List.fromList(
      utf8.encode(
        jsonEncode({
          'operationId': p.operationID,
          'recipientAccountId': p.recipientID,
          'purpose': friendImagePurpose,
          'caption': caption,
          'pixelRisk': draft.pixelRisk,
          'mimeType': draft.mimeType,
          'inputSize': draft.inputSize,
          'inputSha256': draft.inputSha256,
          'contentBase64': base64Encode(draft.bytes),
        }),
      ),
    );
    if (encoded.length > maxFriendImagePostBytes) {
      throw const FormatException('图片消息超过处理范围');
    }
    return _receipt(
      'POST',
      _url(
        '/v1/me/conversations/' +
            p.conversationID +
            '/image-message-operations',
      ),
      201,
      owner,
      p,
      body: encoded,
    ).whenComplete(() => encoded.fillRange(0, encoded.length, 0));
  }

  Future<FriendImageReceipt> recover(String owner, PendingFriendImage p) {
    _checkAddress(p.conversationID);
    PendingFriendImage.decode(p.toJson());
    return _receipt(
      'GET',
      _url(
        '/v1/me/conversations/' +
            p.conversationID +
            '/image-message-operations/' +
            p.operationID,
      ),
      200,
      owner,
      p,
    );
  }

  Future<Uint8List> content({
    required String conversation,
    required String message,
    required FriendImageMetadata metadata,
    required FriendImageVariant variant,
  }) async {
    _checkAddress(conversation, message);
    FriendImageMetadata.decode(metadata.toJson());
    final selected = variant == FriendImageVariant.thumbnail
        ? metadata.thumbnail
        : metadata.full;
    final bytes = await _request(
      'GET',
      _url(
        '/v1/me/conversations/' +
            conversation +
            '/messages/' +
            message +
            '/image',
      ).replace(queryParameters: {'variant': variant.name}),
      200,
      selected.byteSize,
      inspect: (r) {
        if (r.headers['content-type'] != selected.mimeType ||
            r.headers['content-length'] != selected.byteSize.toString() ||
            r.headers['x-content-sha256'] != selected.sha256 ||
            r.headers['x-image-width'] != selected.width.toString() ||
            r.headers['x-image-height'] != selected.height.toString() ||
            r.headers['x-image-variant'] != variant.name ||
            r.headers['cache-control'] != 'no-store' ||
            r.headers['x-content-type-options'] != 'nosniff') {
          throw const FormatException('图片内容来源无法核验');
        }
      },
    );
    var verified = false;
    try {
      if (bytes.length != selected.byteSize ||
          sha256.convert(bytes).toString() != selected.sha256) {
        throw const FormatException('图片内容与消息元数据不符');
      }
      final header = MomentImageHeader.inspect(bytes, storedDerivative: true);
      rejectAnimatedLocalImage(bytes);
      if (header.mime != selected.mimeType ||
          header.width != selected.width ||
          header.height != selected.height) {
        throw const FormatException('图片像素尺寸与消息不符');
      }
      verified = true;
      return bytes;
    } finally {
      if (!verified) bytes.fillRange(0, bytes.length, 0);
    }
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    cancelRequests();
    // This class borrows the authenticated transport and never closes it.
  }
}
