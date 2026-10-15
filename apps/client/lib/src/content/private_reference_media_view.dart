import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

import '../auth/session_vault.dart';
import '../workspace/human_causal_control.dart';
import '../workspace/now_ui_tokens.dart';
import 'moment_image_header.dart';

const _referenceTypes = {'PLACE', 'JOURNEY', 'MOMENT'};

/// The original role stays separate from an ordinary Moment attachment.
class PrivateReferenceImage {
  const PrivateReferenceImage({
    required this.id,
    required this.kind,
    required this.revision,
    required this.position,
    required this.mimeType,
    required this.width,
    required this.height,
    required this.byteCount,
    required this.sha256,
    required this.contentPath,
    required this.sourceVersion,
    required this.sourceType,
    required this.sourceReviewStatus,
    required this.sourceIsCover,
    required this.sourceRightsBasis,
    required this.sourceAttribution,
    required this.sourcePublishable,
  });
  final String id, kind, mimeType, sha256, contentPath, sourceVersion;
  final String sourceType, sourceReviewStatus, sourceRightsBasis;
  final String sourceAttribution;
  final int revision, position, width, height, byteCount;
  final bool sourceIsCover;
  final bool? sourcePublishable;
  bool get readable => sourceReviewStatus != 'removed';
  String get roleLabel => switch (kind) {
    'PLACE_IMAGE' => sourceIsCover ? '原地点封面' : '原地点图片',
    'JOURNEY_MEDIA' => '原旅程图片',
    _ => '原动态封面',
  };

  factory PrivateReferenceImage.decode(Object? raw, String targetType) {
    final v = humanControlObject(raw, {
      'id',
      'kind',
      'revision',
      'position',
      'mimeType',
      'width',
      'height',
      'byteCount',
      'sha256',
      'contentPath',
      'sourceVersion',
      'sourceType',
      'sourceReviewStatus',
      'sourceIsCover',
      'sourceRightsBasis',
      'sourceAttribution',
      'sourcePublishable',
      'canPublish',
      'canCopy',
    });
    final expectedKind = switch (targetType) {
      'PLACE' => 'PLACE_IMAGE',
      'JOURNEY' => 'JOURNEY_MEDIA',
      'MOMENT' => 'MOMENT_COVER',
      _ => '',
    };
    if (!humanControlID(v['id']) ||
        v['kind'] != expectedKind ||
        !humanControlVersion(v['revision'], zero: false) ||
        v['position'] is! int ||
        v['position'] < 0 ||
        !const {'image/jpeg', 'image/png'}.contains(v['mimeType']) ||
        v['width'] is! int ||
        v['height'] is! int ||
        v['width'] < 1 ||
        v['height'] < 1 ||
        v['width'] > 8192 ||
        v['height'] > 8192 ||
        v['width'] * v['height'] > 48 * 1024 * 1024 ||
        v['byteCount'] is! int ||
        v['byteCount'] < 1 ||
        v['byteCount'] > 16 * 1024 * 1024 ||
        !humanControlHash(v['sha256']) ||
        !humanControlHash(v['sourceVersion']) ||
        v['sourceIsCover'] is! bool ||
        !const {
          'approved',
          'rejected',
          'removed',
          'none',
        }.contains(v['sourceReviewStatus']) ||
        v['sourcePublishable'] != null && v['sourcePublishable'] is! bool ||
        v['canPublish'] != false ||
        v['canCopy'] != false ||
        ['sourceType', 'sourceRightsBasis', 'sourceAttribution'].any(
          (key) => v[key] is! String || utf8.encode(v[key]).length > 16384,
        ) ||
        (v['sourceType'] is String &&
            (v['sourceType'] as String).runes.length > 80) ||
        (v['sourceRightsBasis'] is String &&
            (v['sourceRightsBasis'] as String).runes.length > 160) ||
        (v['sourceAttribution'] is String &&
            utf8.encode(v['sourceAttribution']).length > 16000) ||
        v['contentPath'] is! String) {
      throw const FormatException('原图片资料不符');
    }
    final path = Uri.tryParse(v['contentPath']);
    if (path == null ||
        path.hasScheme ||
        path.hasAuthority ||
        path.hasFragment ||
        path.path != '/v1/me/reference-images/${v['id']}/content' ||
        path.queryParametersAll.length != 2 ||
        path.queryParametersAll['revision']?.length != 1 ||
        path.queryParametersAll['sourceVersion']?.length != 1 ||
        path.queryParameters['revision'] != '${v['revision']}' ||
        path.queryParameters['sourceVersion'] != v['sourceVersion']) {
      throw const FormatException('原图片读取来源不符');
    }
    return PrivateReferenceImage(
      id: v['id'],
      kind: v['kind'],
      revision: v['revision'],
      position: v['position'],
      mimeType: v['mimeType'],
      width: v['width'],
      height: v['height'],
      byteCount: v['byteCount'],
      sha256: v['sha256'],
      contentPath: v['contentPath'],
      sourceVersion: v['sourceVersion'],
      sourceType: v['sourceType'],
      sourceReviewStatus: v['sourceReviewStatus'],
      sourceIsCover: v['sourceIsCover'],
      sourceRightsBasis: v['sourceRightsBasis'],
      sourceAttribution: v['sourceAttribution'],
      sourcePublishable: v['sourcePublishable'],
    );
  }
}

class PrivateReferenceImagePage {
  const PrivateReferenceImagePage(
    this.items,
    this.observedAt,
    this.validUntil,
    this.sourceVersion,
    this.nextCursor,
  );
  final List<PrivateReferenceImage> items;
  final DateTime observedAt, validUntil;
  final String sourceVersion;
  final String? nextCursor;
  bool get current => DateTime.now().toUtc().isBefore(validUntil);

  factory PrivateReferenceImagePage.decode(
    Object? raw,
    String type,
    String id,
  ) {
    final v = humanControlObject(raw, {
      'schemaVersion',
      'targetType',
      'targetId',
      'items',
      'observedAt',
      'validUntil',
      'sourceVersion',
      'nextCursor',
    });
    if (v['schemaVersion'] != 'owned-reference-images-v1' ||
        !_referenceTypes.contains(type) ||
        !humanControlID(id) ||
        v['targetType'] != type ||
        v['targetId'] != id ||
        !humanControlHash(v['sourceVersion']) ||
        v['items'] is! List ||
        (v['items'] as List).length > 50 ||
        v['nextCursor'] != null &&
            (v['nextCursor'] is! String ||
                (v['nextCursor'] as String).isEmpty ||
                (v['nextCursor'] as String).length > 4096)) {
      throw const FormatException('原图片归属不符');
    }
    final at = humanControlTime(v['observedAt']);
    final until = humanControlTime(v['validUntil']);
    humanControlLease(at, until);
    if (until.difference(at) > const Duration(seconds: 30)) {
      throw const FormatException('原图片读取期限不符');
    }
    final items = (v['items'] as List)
        .map((r) => PrivateReferenceImage.decode(r, type))
        .toList();
    if (items.map((r) => r.id).toSet().length != items.length) {
      throw const FormatException('原图片身份重复');
    }
    return PrivateReferenceImagePage(
      List.unmodifiable(items),
      at,
      until,
      v['sourceVersion'],
      v['nextCursor'],
    );
  }
}

/// Only a selector survives a lost response. It is never a read permission.
class PrivateReferenceImagePending {
  const PrivateReferenceImagePending({
    required this.operationID,
    required this.imageID,
    required this.expectedRevision,
    required this.sourceVersion,
  });
  final String operationID, imageID, sourceVersion;
  final int expectedRevision;
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'imageId': imageID,
    'expectedRevision': expectedRevision,
    'sourceVersion': sourceVersion,
  };
  factory PrivateReferenceImagePending.decode(Object? raw) {
    final v = humanControlObject(raw, {
      'operationId',
      'imageId',
      'expectedRevision',
      'sourceVersion',
    });
    if (!humanControlID(v['operationId']) ||
        !humanControlID(v['imageId']) ||
        !humanControlHash(v['sourceVersion']) ||
        !humanControlVersion(v['expectedRevision'], zero: false) ||
        v['expectedRevision'] == humanControlMaxVersion) {
      throw const FormatException('图片移除恢复记录不符');
    }
    return PrivateReferenceImagePending(
      operationID: v['operationId'],
      imageID: v['imageId'],
      expectedRevision: v['expectedRevision'],
      sourceVersion: v['sourceVersion'],
    );
  }
}

abstract interface class PrivateReferenceImagePendingStore {
  Future<PrivateReferenceImagePending?> read(
    String origin,
    String owner,
    String type,
    String target,
  );
  Future<void> write(
    String origin,
    String owner,
    String type,
    String target,
    PrivateReferenceImagePending value,
  );
  Future<void> remove(
    String origin,
    String owner,
    String type,
    String target,
    String operationID,
  );
}

class SecurePrivateReferenceImagePendingStore
    implements PrivateReferenceImagePendingStore {
  const SecurePrivateReferenceImagePendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;
  String _key(String origin, String owner, String type, String target) {
    final canonical = canonicalAuthOrigin(origin);
    if (canonical.isEmpty ||
        !humanControlID(owner) ||
        !_referenceTypes.contains(type) ||
        !humanControlID(target)) {
      throw const FormatException('图片恢复归属不符');
    }
    return 'birdtie.private-reference.pending.v1.' +
        sha256
            .convert(utf8.encode(jsonEncode([canonical, owner, type, target])))
            .toString();
  }

  @override
  Future<PrivateReferenceImagePending?> read(
    String origin,
    String owner,
    String type,
    String target,
  ) async {
    final key = _key(origin, owner, type, target);
    return withAuthStorageKey(key, () async {
      final raw = await storage.read(key: key);
      if (raw == null) return null;
      if (utf8.encode(raw).length > 1024) {
        throw const FormatException('图片恢复记录需要检查');
      }
      return PrivateReferenceImagePending.decode(jsonDecode(raw));
    });
  }

  @override
  Future<void> write(
    String origin,
    String owner,
    String type,
    String target,
    PrivateReferenceImagePending value,
  ) async {
    final key = _key(origin, owner, type, target);
    PrivateReferenceImagePending.decode(value.toJson());
    await withAuthStorageKey(key, () async {
      final previous = await storage.read(key: key);
      if (previous != null) {
        final original = PrivateReferenceImagePending.decode(
          jsonDecode(previous),
        );
        if (jsonEncode(original.toJson()) != jsonEncode(value.toJson())) {
          throw const FormatException('请先核实原图片移除结果');
        }
      }
      await storage.write(key: key, value: jsonEncode(value.toJson()));
    });
  }

  @override
  Future<void> remove(
    String origin,
    String owner,
    String type,
    String target,
    String operationID,
  ) async {
    final key = _key(origin, owner, type, target);
    await withAuthStorageKey(key, () async {
      final raw = await storage.read(key: key);
      if (raw != null &&
          PrivateReferenceImagePending.decode(jsonDecode(raw)).operationID ==
              operationID) {
        await storage.delete(key: key);
      }
    });
  }
}

/// Authenticated, short-lived bytes only. One full image is held at a time.
class PrivateReferenceMediaView extends StatefulWidget {
  const PrivateReferenceMediaView({
    super.key,
    required this.targetType,
    required this.targetID,
    required this.authorizationHeader,
    required this.sourceCurrent,
    required this.apiBaseUrl,
    required this.client,
    this.ownerID,
    this.identityChanges,
    this.pendingStore = const SecurePrivateReferenceImagePendingStore(),
  });
  final String targetType, targetID, apiBaseUrl;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final bool Function() sourceCurrent;
  final http.Client client;
  final Listenable? identityChanges;
  final PrivateReferenceImagePendingStore pendingStore;
  @override
  State<PrivateReferenceMediaView> createState() =>
      _PrivateReferenceMediaState();
}

class _PrivateReferenceMediaState extends State<PrivateReferenceMediaView>
    with WidgetsBindingObserver {
  late final String _origin;
  String? _token, _owner, _error;
  PrivateReferenceImagePage? _page;
  PrivateReferenceImagePending? _pending;
  Uint8List? _bytes;
  ImageProvider? _provider;
  StreamIterator<List<int>>? _reader;
  Timer? _lease;
  int _serial = 0, _index = 0;
  bool _retired = false, _foreground = true, _loading = false;
  bool _fetching = false, _deleting = false, _journalFailed = false;

  bool get _current =>
      mounted &&
      !_retired &&
      _foreground &&
      widget.sourceCurrent() &&
      _token != null &&
      _token == widget.authorizationHeader() &&
      (widget.ownerID == null || _owner == widget.ownerID!());
  PrivateReferenceImage? get _selected =>
      _page != null && _index < _page!.items.length
      ? _page!.items[_index]
      : null;
  bool get _canDelete =>
      _current &&
      _owner != null &&
      humanControlID(_owner) &&
      _page?.current == true &&
      _bytes != null &&
      _selected?.readable == true &&
      !_loading &&
      !_fetching &&
      !_deleting &&
      _pending == null &&
      !_journalFailed;

  @override
  void initState() {
    super.initState();
    _origin = canonicalAuthOrigin(widget.apiBaseUrl);
    _token = widget.authorizationHeader();
    _owner = widget.ownerID?.call();
    widget.identityChanges?.addListener(_boundary);
    WidgetsBinding.instance.addObserver(this);
    unawaited(_initialize());
  }

  Future<void> _initialize() async {
    if (_owner != null && humanControlID(_owner)) {
      try {
        final pending = await widget.pendingStore.read(
          _origin,
          _owner!,
          widget.targetType,
          widget.targetID,
        );
        if (!_current) return;
        setState(() => _pending = pending);
      } catch (_) {
        if (!_current) return;
        setState(() => _journalFailed = true);
      }
    }
    if (_current) await _load();
  }

  void _clearBytes() {
    final reader = _reader;
    _reader = null;
    if (reader != null) unawaited(reader.cancel());
    final provider = _provider;
    _provider = null;
    if (provider != null) unawaited(provider.evict().then<void>((_) {}));
    _bytes?.fillRange(0, _bytes!.length, 0);
    _bytes = null;
    _fetching = false;
  }

  void _clearRead() {
    _serial++;
    _lease?.cancel();
    _clearBytes();
    _page = null;
    _loading = false;
  }

  void _boundary() {
    if (_current || !mounted) return;
    _retired = true;
    _clearRead();
    setState(() => _error = '登录或资料已变化，请返回后重新打开。');
  }

  @override
  void didUpdateWidget(covariant PrivateReferenceMediaView old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_boundary);
      widget.identityChanges?.addListener(_boundary);
    }
    if (old.targetType != widget.targetType ||
        old.targetID != widget.targetID ||
        old.client != widget.client ||
        canonicalAuthOrigin(widget.apiBaseUrl) != _origin) {
      _retired = true;
    }
    _boundary();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    if (!_foreground) {
      _clearRead();
      if (mounted) setState(() {});
    } else {
      _boundary();
      if (_current) unawaited(_load());
    }
  }

  Uri _uri(String path, [Map<String, String>? query]) =>
      Uri.parse(_origin).replace(path: path, queryParameters: query);
  Future<Object?> _json(http.Request request) async {
    request.followRedirects = false;
    request.headers['Authorization'] = _token!;
    final response = await widget.client
        .send(request)
        .timeout(const Duration(seconds: 15));
    if (response.statusCode != 200 ||
        response.contentLength != null &&
            response.contentLength! > 1024 * 1024) {
      await response.stream.listen((_) {}).cancel();
      throw const FormatException('图片资料暂不可读');
    }
    final reader = StreamIterator(response.stream);
    final bytes = BytesBuilder(copy: true);
    final deadline = DateTime.now().toUtc().add(const Duration(seconds: 20));
    try {
      while (await reader.moveNext().timeout(const Duration(seconds: 10))) {
        if (!_current ||
            DateTime.now().toUtc().isAfter(deadline) ||
            bytes.length + reader.current.length > 1024 * 1024) {
          throw const FormatException('图片资料读取已失效');
        }
        bytes.add(reader.current);
      }
    } finally {
      await reader.cancel();
    }
    final data = bytes.takeBytes();
    return humanControlObject(jsonDecode(utf8.decode(data)), {'data'})['data'];
  }

  Future<void> _load({bool more = false}) async {
    if (!_current || _loading || _deleting) return;
    final previous = more ? _page : null;
    final cursor = previous?.nextCursor;
    if (more && (previous == null || !previous.current || cursor == null))
      return;
    final serial = ++_serial;
    _clearBytes();
    _lease?.cancel();
    setState(() {
      _loading = true;
      _error = null;
      if (!more) {
        _page = null;
        _index = 0;
      }
    });
    try {
      final value = PrivateReferenceImagePage.decode(
        await _json(
          http.Request(
            'GET',
            _uri('/v1/me/reference-images', {
              'targetType': widget.targetType,
              'targetId': widget.targetID,
              if (cursor != null) 'cursor': cursor,
            }),
          ),
        ),
        widget.targetType,
        widget.targetID,
      );
      if (!_current || serial != _serial || !value.current) return;
      var page = value;
      if (previous != null) {
        if (previous.sourceVersion != value.sourceVersion ||
            previous.validUntil != value.validUntil ||
            value.nextCursor == cursor ||
            previous.items.any((a) => value.items.any((b) => a.id == b.id)) ||
            previous.items.length + value.items.length > 200) {
          throw const FormatException('原图片列表已变化，请刷新');
        }
        page = PrivateReferenceImagePage(
          List.unmodifiable([...previous.items, ...value.items]),
          previous.observedAt,
          previous.validUntil,
          previous.sourceVersion,
          value.nextCursor,
        );
      }
      setState(() {
        _page = page;
        _loading = false;
      });
      _lease = Timer(page.validUntil.difference(DateTime.now().toUtc()), () {
        if (!mounted || serial != _serial) return;
        setState(() {
          _clearRead();
          _error = '图片读取已到期，请重新读取。';
        });
      });
      await _readSelected();
    } catch (_) {
      if (_current && serial == _serial) {
        setState(() {
          _page = null;
          _error = '原图片暂不可读，请重新读取。';
        });
      }
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _readSelected() async {
    final item = _selected, page = _page;
    if (!_current || page?.current != true || item == null || !item.readable)
      return;
    final serial = _serial;
    _clearBytes();
    setState(() {
      _fetching = true;
      _error = null;
    });
    final chunks = <Uint8List>[];
    try {
      final request = http.Request(
        'GET',
        _uri('/v1/me/reference-images/${item.id}/content', {
          'revision': '${item.revision}',
          'sourceVersion': item.sourceVersion,
        }),
      )..followRedirects = false;
      request.headers['Authorization'] = _token!;
      final response = await widget.client
          .send(request)
          .timeout(const Duration(seconds: 15));
      if (!_current ||
          serial != _serial ||
          !identical(_selected, item) ||
          !page!.current) {
        await response.stream.listen((_) {}).cancel();
        return;
      }
      if (response.statusCode != 200 ||
          response.headers['content-type']?.split(';').first != item.mimeType ||
          !response.headers['cache-control'].toString().toLowerCase().contains(
            'no-store',
          ) ||
          response.contentLength != null &&
              response.contentLength != item.byteCount) {
        await response.stream.listen((_) {}).cancel();
        throw const FormatException('原图片读取已变化');
      }
      final reader = StreamIterator(response.stream);
      _reader = reader;
      var count = 0;
      final deadline = DateTime.now().toUtc().add(const Duration(seconds: 25));
      while (await reader.moveNext().timeout(const Duration(seconds: 10))) {
        if (!_current ||
            serial != _serial ||
            !identical(_selected, item) ||
            !page.current ||
            DateTime.now().toUtc().isAfter(deadline)) {
          throw const FormatException('图片读取已失效');
        }
        count += reader.current.length;
        if (count > item.byteCount) throw const FormatException('原图片大小不符');
        chunks.add(Uint8List.fromList(reader.current));
      }
      await reader.cancel();
      if (identical(_reader, reader)) _reader = null;
      final bytes = Uint8List(count);
      var offset = 0;
      for (final chunk in chunks) {
        bytes.setRange(offset, offset + chunk.length, chunk);
        offset += chunk.length;
      }
      var accepted = false;
      try {
        final header = MomentImageHeader.inspect(
          bytes,
          storedDerivative: true,
          importedLegacyPrivate: true,
        );
        if (count != item.byteCount ||
            sha256.convert(bytes).toString() != item.sha256 ||
            header.mime != item.mimeType ||
            header.width != item.width ||
            header.height != item.height) {
          throw const FormatException('原图片内容不符');
        }
        if (_current &&
            serial == _serial &&
            identical(_selected, item) &&
            page.current) {
          setState(() {
            _bytes = bytes;
            _provider = ResizeImage(
              MemoryImage(bytes),
              width: 1536,
              height: 1536,
              policy: ResizeImagePolicy.fit,
              allowUpscaling: false,
            );
          });
          accepted = true;
        }
      } finally {
        if (!accepted) bytes.fillRange(0, bytes.length, 0);
      }
    } catch (_) {
      if (_current && serial == _serial)
        setState(() => _error = '这张图片暂不可读，请重新读取。');
    } finally {
      for (final chunk in chunks) {
        chunk.fillRange(0, chunk.length, 0);
      }
      final reader = _reader;
      if (serial == _serial && reader != null) {
        _reader = null;
        await reader.cancel();
      }
      if (mounted && serial == _serial) setState(() => _fetching = false);
    }
  }

  Future<void> _select(int index) async {
    if (!_current ||
        _deleting ||
        _loading ||
        index < 0 ||
        index >= (_page?.items.length ?? 0))
      return;
    _serial++;
    _clearBytes();
    setState(() => _index = index);
    // The existing lease still applies when changing the selected image.
    final deadline = _page!.validUntil;
    _lease?.cancel();
    final serial = _serial;
    _lease = Timer(deadline.difference(DateTime.now().toUtc()), () {
      if (mounted && serial == _serial)
        setState(() {
          _clearRead();
          _error = '图片读取已到期，请重新读取。';
        });
    });
    await _readSelected();
  }

  String _operationID() {
    final r = Random.secure();
    final b = List<int>.generate(16, (_) => r.nextInt(256));
    b[6] = (b[6] & 15) | 64;
    b[8] = (b[8] & 63) | 128;
    final h = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    return '${h.substring(0, 8)}-${h.substring(8, 12)}-${h.substring(12, 16)}-${h.substring(16, 20)}-${h.substring(20)}';
  }

  Future<void> _delete() async {
    if (!_canDelete) return;
    final item = _selected!, page = _page!, image = _provider!;
    final yes = await showDialog<bool>(
      context: context,
      builder: (_) => _ReferenceDeleteDialog(
        item: item,
        image: image,
        validUntil: page.validUntil,
        changes: widget.identityChanges,
        current: () =>
            _canDelete && identical(_selected, item) && identical(_page, page),
      ),
    );
    if (yes != true ||
        !_canDelete ||
        !identical(_selected, item) ||
        !identical(_page, page))
      return;
    final pending = PrivateReferenceImagePending(
      operationID: _operationID(),
      imageID: item.id,
      expectedRevision: item.revision,
      sourceVersion: item.sourceVersion,
    );
    setState(() {
      _deleting = true;
      _error = null;
    });
    try {
      await widget.pendingStore.write(
        _origin,
        _owner!,
        widget.targetType,
        widget.targetID,
        pending,
      );
      if (!_current || !page.current || !identical(_selected, item)) {
        // No request has been sent. Retire only this local, unsent selector.
        await widget.pendingStore.remove(
          _origin,
          _owner!,
          widget.targetType,
          widget.targetID,
          pending.operationID,
        );
        return;
      }
      setState(() => _pending = pending);
      final request = http.Request(
        'DELETE',
        _uri('/v1/me/reference-images/${item.id}'),
      );
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode({
        'operationId': pending.operationID,
        'expectedRevision': pending.expectedRevision,
        'expectedSourceVersion': pending.sourceVersion,
      });
      final receipt = await _json(request);
      if (_current) await _acceptReceipt(receipt, pending);
    } catch (_) {
      if (_current)
        setState(
          () => _error = _pending == null
              ? '未能保存移除操作记录，未提交移除。请重试。'
              : '移除结果尚未核实，请核实原操作。',
        );
    } finally {
      if (mounted) setState(() => _deleting = false);
    }
  }

  Future<void> _acceptReceipt(
    Object? raw,
    PrivateReferenceImagePending pending,
  ) async {
    if (!_current || _pending?.operationID != pending.operationID) return;
    final v = humanControlObject(raw, {
      'schemaVersion',
      'operationId',
      'imageId',
      'revision',
      'status',
      'effect',
      'committedAt',
    });
    if (v['schemaVersion'] != 'owned-reference-image-operation-v1' ||
        v['operationId'] != pending.operationID ||
        v['imageId'] != pending.imageID ||
        v['revision'] != pending.expectedRevision + 1 ||
        v['status'] != 'COMMITTED' ||
        v['effect'] != 'DELETED')
      throw const FormatException('原移除操作事实不符');
    humanControlTime(v['committedAt']);
    await widget.pendingStore.remove(
      _origin,
      _owner!,
      widget.targetType,
      widget.targetID,
      pending.operationID,
    );
    if (!_current || _pending?.operationID != pending.operationID) return;
    setState(() {
      _pending = null;
      _clearRead();
      _error = null;
    });
    _deleting = false;
    await _load();
  }

  Future<void> _reconcile() async {
    final pending = _pending;
    if (!_current || pending == null || _deleting) return;
    setState(() {
      _deleting = true;
      _error = null;
    });
    try {
      final raw = await _json(
        http.Request(
          'GET',
          _uri('/v1/me/reference-image-operations/${pending.operationID}'),
        ),
      );
      if (_current && _pending?.operationID == pending.operationID)
        await _acceptReceipt(raw, pending);
    } catch (_) {
      if (_current) setState(() => _error = '尚未取得原移除事实，请稍后再次核实；不会重复移除。');
    } finally {
      if (mounted) setState(() => _deleting = false);
    }
  }

  @override
  void dispose() {
    _retired = true;
    _clearRead();
    widget.identityChanges?.removeListener(_boundary);
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final text = NowUiTokens.of(context);
    if (!_foreground) return const SizedBox.shrink();
    if (!_current) {
      if (!_retired) {
        _retired = true;
        _clearRead();
      }
      return const SizedBox.shrink();
    }
    final item = _selected, page = _page;
    if (page?.items.isEmpty == true && _pending == null && _error == null)
      return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(item?.roleLabel ?? '原图片', style: text.placeTitle),
          const SizedBox(height: 8),
          if (_loading || _fetching || _deleting)
            const LinearProgressIndicator(),
          if (_provider != null && item?.readable == true)
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 320),
              child: Image(
                key: ValueKey('private-reference-image-${item!.id}'),
                image: _provider!,
                fit: BoxFit.contain,
                semanticLabel: item.roleLabel,
                errorBuilder: (_, _, _) => const Text('图片无法显示，请重新读取。'),
              ),
            ),
          if (item != null) ...[
            if (item.sourceReviewStatus == 'rejected')
              const Padding(
                padding: EdgeInsets.only(top: 8),
                child: Text('这张原图片审核未通过，仅本人可查看原资料。'),
              ),
            if (!item.readable) const Text('这张原图片已移除。'),
            if (item.sourcePublishable == false)
              const Padding(
                padding: EdgeInsets.only(top: 8),
                child: Text('原图片保留为私人资料。'),
              ),
            if (item.sourceAttribution.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(item.sourceAttribution, style: text.metadata),
              ),
          ],
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(_error!, style: text.body),
            ),
          if (_journalFailed) const Text('图片移除恢复记录暂不可读，请返回后重新打开。'),
          if (_pending != null) ...[
            const Text('有一项图片移除结果待核实。'),
            TextButton(
              onPressed: _deleting ? null : _reconcile,
              child: const Text('核实原移除结果'),
            ),
          ],
          Wrap(
            spacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              if ((page?.items.length ?? 0) > 1) ...[
                IconButton(
                  tooltip: '上一张原图片',
                  onPressed: _index > 0 && !_deleting
                      ? () => _select(_index - 1)
                      : null,
                  icon: const Icon(Icons.chevron_left),
                ),
                Text(
                  '${_index + 1} / ${page!.items.length}',
                  style: text.metadata,
                ),
                IconButton(
                  tooltip: '下一张原图片',
                  onPressed: _index + 1 < page.items.length && !_deleting
                      ? () => _select(_index + 1)
                      : null,
                  icon: const Icon(Icons.chevron_right),
                ),
              ],
              if (page?.nextCursor != null)
                TextButton(
                  onPressed: _loading || _deleting
                      ? null
                      : () => _load(more: true),
                  child: const Text('读取更多原图片'),
                ),
              TextButton.icon(
                onPressed: _loading || _deleting ? null : _load,
                icon: const Icon(Icons.refresh),
                label: const Text('重新读取图片'),
              ),
              if (_owner != null)
                TextButton.icon(
                  onPressed: _canDelete ? _delete : null,
                  icon: const Icon(Icons.delete_outline),
                  label: const Text('移除原图片引用'),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

class _ReferenceDeleteDialog extends StatefulWidget {
  const _ReferenceDeleteDialog({
    required this.item,
    required this.image,
    required this.validUntil,
    required this.current,
    this.changes,
  });
  final PrivateReferenceImage item;
  final ImageProvider image;
  final DateTime validUntil;
  final bool Function() current;
  final Listenable? changes;
  @override
  State<_ReferenceDeleteDialog> createState() => _ReferenceDeleteDialogState();
}

class _ReferenceDeleteDialogState extends State<_ReferenceDeleteDialog>
    with WidgetsBindingObserver {
  Timer? _timer;
  bool _expired = false, _foreground = true;
  bool get _current => !_expired && _foreground && widget.current();
  @override
  void initState() {
    super.initState();
    widget.changes?.addListener(_changed);
    WidgetsBinding.instance.addObserver(this);
    _timer = Timer(widget.validUntil.difference(DateTime.now().toUtc()), () {
      if (mounted) setState(() => _expired = true);
    });
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    if (!_foreground) _expired = true;
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    _timer?.cancel();
    widget.changes?.removeListener(_changed);
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    scrollable: true,
    title: Text('移除这张${widget.item.roleLabel}？'),
    content: _current
        ? Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Image(image: widget.image, height: 160, fit: BoxFit.contain),
              const SizedBox(height: 12),
              const Text('移除这条原图片引用后将无法继续在这里读取。文字记录和其他用途的图片保留。'),
            ],
          )
        : const Text('图片资料已变化，请返回后重新读取。'),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context, false),
        child: const Text('保留图片'),
      ),
      FilledButton(
        onPressed: _current ? () => Navigator.pop(context, true) : null,
        child: const Text('确认移除'),
      ),
    ],
  );
}
