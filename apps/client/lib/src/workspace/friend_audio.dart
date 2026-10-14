import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import 'chat_message_operation.dart';
import 'entity_share_pending_store.dart' show chatEntityUUID;

const maxFriendAudioBytes = 1048576;
const maxFriendAudioMillis = 90000;
const friendAudioReceiptSchema = 'human.audio_message_operation.v1';

enum FriendAudioPhase {
  idle,
  permission,
  recording,
  processing,
  preview,
  playing,
}

class FriendAudioMetadata {
  const FriendAudioMetadata({
    required this.byteSize,
    required this.sha256,
    required this.durationMillis,
    this.mimeType = 'audio/mp4',
  });
  final String mimeType, sha256;
  final int byteSize, durationMillis;
  Map<String, Object> toJson() => {
    'mimeType': mimeType,
    'byteSize': byteSize,
    'sha256': sha256,
    'durationMillis': durationMillis,
  };
  factory FriendAudioMetadata.decode(Object? raw) {
    if (raw is! Map ||
        raw.length != 4 ||
        raw.keys.toSet().difference({
          'mimeType',
          'byteSize',
          'sha256',
          'durationMillis',
        }).isNotEmpty ||
        raw['mimeType'] != 'audio/mp4' ||
        raw['byteSize'] is! int ||
        raw['byteSize'] < 1 ||
        raw['byteSize'] > maxFriendAudioBytes ||
        raw['sha256'] is! String ||
        !humanMessageHex.hasMatch(raw['sha256']) ||
        raw['durationMillis'] is! int ||
        raw['durationMillis'] < 1 ||
        raw['durationMillis'] > maxFriendAudioMillis) {
      throw const FormatException('语音附件格式无法核验');
    }
    return FriendAudioMetadata(
      byteSize: raw['byteSize'],
      sha256: raw['sha256'],
      durationMillis: raw['durationMillis'],
    );
  }
  bool same(FriendAudioMetadata other) =>
      mimeType == other.mimeType &&
      byteSize == other.byteSize &&
      sha256 == other.sha256 &&
      durationMillis == other.durationMillis;
}

class FriendAudioDraft {
  FriendAudioDraft(Uint8List bytes, this.metadata)
    : bytes = Uint8List.fromList(bytes).asUnmodifiableView() {
    FriendAudioMetadata.decode(metadata.toJson());
    if (bytes.length != metadata.byteSize ||
        sha256.convert(bytes).toString() != metadata.sha256)
      throw const FormatException('语音文件与预览不符');
  }
  final Uint8List bytes;
  final FriendAudioMetadata metadata;
  int get durationMillis => metadata.durationMillis;
  String get sha => metadata.sha256;
}

String friendAudioPayloadDigest(
  String conversation,
  FriendAudioMetadata m,
) => sha256
    .convert(
      utf8.encode(
        'birdtie.human-audio-payload.v1\n$conversation\n${m.mimeType}\n${m.byteSize}\n${m.sha256}\n${m.durationMillis}',
      ),
    )
    .toString();

class PendingFriendAudio {
  const PendingFriendAudio({
    required this.operationID,
    required this.conversationID,
    required this.metadata,
  });
  final String operationID, conversationID;
  final FriendAudioMetadata metadata;
  String get payloadDigest =>
      friendAudioPayloadDigest(conversationID, metadata);
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'conversationId': conversationID,
    ...metadata.toJson(),
  };
  factory PendingFriendAudio.decode(Object? raw) {
    if (raw is! Map<String, dynamic> ||
        raw.length != 6 ||
        raw.keys.toSet().difference({
          'operationId',
          'conversationId',
          'mimeType',
          'byteSize',
          'sha256',
          'durationMillis',
        }).isNotEmpty ||
        raw['operationId'] is! String ||
        !chatEntityUUID.hasMatch(raw['operationId']) ||
        raw['conversationId'] is! String ||
        !chatEntityUUID.hasMatch(raw['conversationId']))
      throw const FormatException('语音发送记录无法核验');
    return PendingFriendAudio(
      operationID: raw['operationId'],
      conversationID: raw['conversationId'],
      metadata: FriendAudioMetadata.decode({
        for (final k in const [
          'mimeType',
          'byteSize',
          'sha256',
          'durationMillis',
        ])
          k: raw[k],
      }),
    );
  }
}

class FriendAudioReceipt {
  const FriendAudioReceipt(this.messageID, this.createdAt);
  final String messageID;
  final DateTime createdAt;
  static FriendAudioReceipt decode(
    Object? raw,
    String owner,
    PendingFriendAudio pending,
  ) {
    const keys = {
      'schemaVersion',
      'ownerId',
      'conversationId',
      'operationId',
      'effectKey',
      'payloadDigest',
      'messageId',
      'mimeType',
      'byteSize',
      'sha256',
      'durationMillis',
      'createdAt',
    };
    if (raw is! Map<String, dynamic> ||
        raw.length != keys.length ||
        raw.keys.toSet().difference(keys).isNotEmpty ||
        raw['schemaVersion'] != friendAudioReceiptSchema ||
        raw['ownerId'] != owner ||
        raw['conversationId'] != pending.conversationID ||
        raw['operationId'] != pending.operationID ||
        raw['effectKey'] != humanMessageEffectKey(owner, pending.operationID) ||
        raw['payloadDigest'] != pending.payloadDigest ||
        raw['messageId'] is! String ||
        !chatEntityUUID.hasMatch(raw['messageId']) ||
        raw['createdAt'] is! String ||
        !(raw['createdAt'] as String).endsWith('Z') ||
        !chatEntityUUID.hasMatch(owner))
      throw const FormatException('语音发送回执与这次操作不符');
    final meta = FriendAudioMetadata.decode({
      for (final k in const [
        'mimeType',
        'byteSize',
        'sha256',
        'durationMillis',
      ])
        k: raw[k],
    });
    final at = DateTime.tryParse(raw['createdAt']);
    if (!meta.same(pending.metadata) ||
        at == null ||
        at.year < 2020 ||
        at.year > 2200)
      throw const FormatException('语音发送回执无法核验');
    return FriendAudioReceipt(raw['messageId'], at.toUtc());
  }
}

class FriendAudioDeliveryFailure implements Exception {
  const FriendAudioDeliveryFailure(this.status);
  final int status;
}

abstract interface class FriendAudioPendingStore {
  Future<List<PendingFriendAudio>> read(String environment, String owner);
  Future<void> write(
    String environment,
    String owner,
    PendingFriendAudio pending,
  );
  Future<void> delete(
    String environment,
    String owner,
    PendingFriendAudio pending,
  );
}

class SecureFriendAudioPendingStore implements FriendAudioPendingStore {
  SecureFriendAudioPendingStore({this.storage = const FlutterSecureStorage()});
  final FlutterSecureStorage storage;
  static Future<void> _tail = Future.value();
  Future<T> _exclusive<T>(Future<T> Function() f) {
    final next = _tail.then((_) => f());
    _tail = next.then<void>((_) {}, onError: (Object _, StackTrace _) {});
    return next;
  }

  String _prefix(String e, String o) {
    final u = Uri.tryParse(e);
    if (u == null ||
        !u.hasAuthority ||
        !const {'http', 'https'}.contains(u.scheme) ||
        u.userInfo.isNotEmpty ||
        u.hasQuery ||
        u.hasFragment ||
        !chatEntityUUID.hasMatch(o))
      throw const FormatException('语音记录归属不符');
    return 'birdtie.human-audio.pending.v1.${sha256.convert(utf8.encode('$e\n$o'))}.';
  }

  Future<List<PendingFriendAudio>> _read(String e, String o) async {
    final prefix = _prefix(e, o),
        all = await storage.readAll(),
        rows = all.entries.where((r) => r.key.startsWith(prefix)).toList();
    if (rows.length > 16) throw const FormatException('请先核实已有语音发送');
    return [for (final r in rows) _decode(r, prefix)];
  }

  PendingFriendAudio _decode(MapEntry<String, String> row, String prefix) {
    if (row.value.length > 1024) throw const FormatException('语音记录过大');
    final p = PendingFriendAudio.decode(jsonDecode(row.value));
    if (row.key != '$prefix${p.operationID}')
      throw const FormatException('语音记录地址不符');
    return p;
  }

  @override
  Future<List<PendingFriendAudio>> read(String e, String o) =>
      _exclusive(() => _read(e, o));
  @override
  Future<void> write(String e, String o, PendingFriendAudio p) =>
      _exclusive(() async {
        PendingFriendAudio.decode(p.toJson());
        final all = await _read(e, o),
            old = all.where((v) => v.operationID == p.operationID).toList();
        if (old.isNotEmpty &&
            jsonEncode(old.single.toJson()) != jsonEncode(p.toJson()))
          throw StateError('同次发送不能更换语音或收件人');
        if (old.isEmpty && all.length >= 16) throw StateError('请先核实已有语音发送');
        await storage.write(
          key: '${_prefix(e, o)}${p.operationID}',
          value: jsonEncode(p.toJson()),
        );
      });
  @override
  Future<void> delete(String e, String o, PendingFriendAudio p) =>
      _exclusive(() async {
        final rows = await _read(e, o),
            old = rows.where((v) => v.operationID == p.operationID).toList();
        if (old.isNotEmpty &&
            jsonEncode(old.single.toJson()) != jsonEncode(p.toJson()))
          throw StateError('原发送记录已变化');
        await storage.delete(key: '${_prefix(e, o)}${p.operationID}');
      });
}

class MemoryFriendAudioPendingStore implements FriendAudioPendingStore {
  final values = <(String, String, String), PendingFriendAudio>{};
  @override
  Future<List<PendingFriendAudio>> read(String e, String o) async => [
    for (final r in values.entries)
      if (r.key.$1 == e && r.key.$2 == o) r.value,
  ];
  @override
  Future<void> write(String e, String o, PendingFriendAudio p) async {
    PendingFriendAudio.decode(p.toJson());
    final k = (e, o, p.operationID), old = values[k];
    if (old != null && jsonEncode(old.toJson()) != jsonEncode(p.toJson()))
      throw StateError('原语音发送已变化');
    values[k] = p;
  }

  @override
  Future<void> delete(String e, String o, PendingFriendAudio p) async {
    final k = (e, o, p.operationID), old = values[k];
    if (old != null && jsonEncode(old.toJson()) != jsonEncode(p.toJson()))
      throw StateError('原语音发送已变化');
    values.remove(k);
  }
}

class FriendAudioSource {
  FriendAudioSource({
    required this.authorizationHeader,
    required this.environment,
    http.Client? client,
  }) : _client = client ?? http.Client(),
       _owns = client == null;
  final String? Function() authorizationHeader;
  final String environment;
  final http.Client _client;
  final bool _owns;
  bool _closed = false;
  Map<String, String> _headers() {
    final auth = authorizationHeader();
    if (_closed || auth == null) throw StateError('请登录后发送');
    return {'Authorization': auth, 'Content-Type': 'application/json'};
  }

  Uri _uri(String path) => Uri.parse('$environment$path');
  FriendAudioReceipt _receipt(
    http.Response r,
    String owner,
    PendingFriendAudio p,
    int expected,
  ) {
    if (r.statusCode != expected)
      throw FriendAudioDeliveryFailure(r.statusCode);
    if (r.bodyBytes.length > 4096) throw const FormatException('语音回执过大');
    final root = jsonDecode(utf8.decode(r.bodyBytes));
    if (root is! Map<String, dynamic> || root.length != 1)
      throw const FormatException('语音回执格式不符');
    return FriendAudioReceipt.decode(root['data'], owner, p);
  }

  Future<FriendAudioReceipt> send(
    String owner,
    PendingFriendAudio p,
    FriendAudioDraft d,
  ) async {
    if (!p.metadata.same(d.metadata) ||
        d.bytes.length != d.metadata.byteSize ||
        sha256.convert(d.bytes).toString() != d.metadata.sha256)
      throw const FormatException('原语音预览已变化');
    final r = await _client
        .post(
          _uri(
            '/v1/me/conversations/${p.conversationID}/audio-message-operations',
          ),
          headers: _headers(),
          body: jsonEncode({
            'operationId': p.operationID,
            ...d.metadata.toJson(),
            'contentBase64': base64Encode(d.bytes),
          }),
        )
        .timeout(const Duration(seconds: 12));
    return _receipt(r, owner, p, 201);
  }

  Future<FriendAudioReceipt> recover(
    String owner,
    PendingFriendAudio p,
  ) async => _receipt(
    await _client
        .get(
          _uri(
            '/v1/me/conversations/${p.conversationID}/audio-message-operations/${p.operationID}',
          ),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12)),
    owner,
    p,
    200,
  );
  Future<FriendAudioDraft> content(
    String conversation,
    String message,
    FriendAudioMetadata meta,
  ) async {
    if (!chatEntityUUID.hasMatch(conversation) ||
        !chatEntityUUID.hasMatch(message))
      throw const FormatException('语音消息编号不符');
    Future<FriendAudioDraft> read() async {
      final req = http.Request(
        'GET',
        _uri('/v1/me/conversations/$conversation/messages/$message/audio'),
      )..headers.addAll(_headers());
      final r = await _client.send(req);
      if (r.statusCode != 200) {
        await r.stream.drain<void>();
        throw FriendAudioDeliveryFailure(r.statusCode);
      }
      if (r.headers['content-type'] != 'audio/mp4' ||
          r.headers['x-content-sha256'] != meta.sha256 ||
          int.tryParse(r.headers['x-audio-duration-millis'] ?? '') !=
              meta.durationMillis ||
          r.contentLength != meta.byteSize)
        throw const FormatException('语音内容来源无法核验');
      final out = BytesBuilder(copy: false);
      await for (final b in r.stream) {
        out.add(b);
        if (out.length > maxFriendAudioBytes)
          throw const FormatException('语音文件过大');
      }
      return FriendAudioDraft(out.takeBytes(), meta);
    }

    return read().timeout(const Duration(seconds: 12));
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_owns) _client.close();
  }
}

abstract class FriendAudioPort {
  bool get supported;
  Future<void> start(String id, ValueChanged<FriendAudioPhase> onPhase);
  Future<FriendAudioDraft> stop(String id);
  Future<void> cancel(String id);
  Future<void> play(String id, FriendAudioDraft draft, VoidCallback onFinished);
  Future<void> stopPlayback(String id);
  void dispose();
}

class NativeFriendAudioPort extends FriendAudioPort {
  NativeFriendAudioPort({MethodChannel? channel, bool Function()? supported})
    : _channel = channel ?? const MethodChannel('birdtie/friend_audio'),
      _supported =
          supported ??
          (() => !kIsWeb && defaultTargetPlatform == TargetPlatform.android);
  final MethodChannel _channel;
  final bool Function() _supported;
  static NativeFriendAudioPort? _owner;
  String? _recordID, _playID;
  ValueChanged<FriendAudioPhase>? _phase;
  VoidCallback? _finished;
  bool _closed = false;
  @override
  bool get supported => !_closed && _supported();
  void _listen() {
    if (_owner != this) {
      _owner?._retire();
      _owner = this;
      _channel.setMethodCallHandler((call) async {
        if (_closed ||
            _owner != this ||
            call.method != 'state' ||
            call.arguments is! Map)
          return;
        final a = call.arguments as Map;
        final id = a['requestId'];
        if (id == _recordID) {
          switch (a['phase']) {
            case 'permission':
              _phase?.call(FriendAudioPhase.permission);
            case 'recording':
              _phase?.call(FriendAudioPhase.recording);
            case 'interrupted':
              _phase?.call(FriendAudioPhase.idle);
          }
        }
        if (id == _playID && a['phase'] == 'finished') {
          _playID = null;
          _finished?.call();
          _finished = null;
        }
      });
    }
  }

  void _retire() {
    final r = _recordID, p = _playID;
    _recordID = null;
    _playID = null;
    _phase = null;
    _finished = null;
    if (r != null)
      unawaited(
        _channel
            .invokeMethod<void>('cancel', {'requestId': r})
            .catchError((Object _) {}),
      );
    if (p != null)
      unawaited(
        _channel
            .invokeMethod<void>('stopPlayback', {'requestId': p})
            .catchError((Object _) {}),
      );
  }

  @override
  Future<void> start(String id, ValueChanged<FriendAudioPhase> phase) async {
    if (!supported)
      throw PlatformException(code: 'UNAVAILABLE', message: '这台设备暂不支持录制语音消息');
    _listen();
    _recordID = id;
    _phase = phase;
    await _channel
        .invokeMethod<void>('start', {'requestId': id})
        .timeout(const Duration(seconds: 45));
    if (_closed || _recordID != id) throw PlatformException(code: 'CANCELED');
  }

  @override
  Future<FriendAudioDraft> stop(String id) async {
    if (_closed || _recordID != id) throw PlatformException(code: 'CANCELED');
    final raw = await _channel
        .invokeMethod<Object?>('stop', {'requestId': id})
        .timeout(const Duration(seconds: 8));
    if (_closed ||
        _recordID != id ||
        raw is! Map ||
        raw['requestId'] != id ||
        raw['contentBase64'] is! String)
      throw const FormatException('录音结果已失效');
    final meta = FriendAudioMetadata.decode({
      for (final k in const [
        'mimeType',
        'byteSize',
        'sha256',
        'durationMillis',
      ])
        k: raw[k],
    });
    final bytes = base64Decode(raw['contentBase64']);
    _recordID = null;
    _phase = null;
    return FriendAudioDraft(bytes, meta);
  }

  @override
  Future<void> cancel(String id) async {
    if (_recordID == id) {
      _recordID = null;
      _phase = null;
    }
    try {
      await _channel
          .invokeMethod<void>('cancel', {'requestId': id})
          .timeout(const Duration(seconds: 3));
    } catch (_) {}
  }

  @override
  Future<void> play(
    String id,
    FriendAudioDraft draft,
    VoidCallback finished,
  ) async {
    if (!supported) throw PlatformException(code: 'UNAVAILABLE');
    _listen();
    _playID = id;
    _finished = finished;
    await _channel
        .invokeMethod<void>('play', {
          'requestId': id,
          'contentBase64': base64Encode(draft.bytes),
          'sha256': draft.metadata.sha256,
        })
        .timeout(const Duration(seconds: 8));
  }

  @override
  Future<void> stopPlayback(String id) async {
    if (_playID == id) {
      _playID = null;
      _finished = null;
    }
    try {
      await _channel
          .invokeMethod<void>('stopPlayback', {'requestId': id})
          .timeout(const Duration(seconds: 3));
    } catch (_) {}
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    _retire();
    if (_owner == this) {
      _owner = null;
      _channel.setMethodCallHandler(null);
    }
  }
}
