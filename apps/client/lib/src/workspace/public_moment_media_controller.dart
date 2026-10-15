import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../content/moment_control.dart' show newMomentOperationID;
import '../content/moment_image_header.dart';
import 'entity_cover.dart';
import 'human_causal_control.dart';
import 'public_moment_media.dart';
import 'public_moment_media_pending_store.dart';

class PublicMomentMediaDraftImage {
  PublicMomentMediaDraftImage({
    required this.operationID,
    this.bytes,
    this.mime,
    this.stage,
    this.videoSelection,
  });
  String operationID;
  Uint8List? bytes;
  final String? mime;
  PublicMomentMediaStage? stage;
  EntityCoverRights? rights;
  PublicMomentVideoSelection? videoSelection;
  Uint8List? poster;
}

class PublicMomentMediaController extends ChangeNotifier {
  PublicMomentMediaController({
    required this.momentID,
    required this.expectedMomentRevision,
    required this.ownerID,
    required String? Function() authorizationHeader,
    required this.sourceCurrent,
    this.boundaryChanges,
    this.materialOnly = false,
    this.journalEntryID = '',
    String? localScopeOverride,
    http.Client? client,
    String? apiBaseUrl,
    PublicMomentMediaPendingStore? pendingStore,
  }) : _header = authorizationHeader,
       _store = pendingStore ?? const SecurePublicMomentMediaPendingStore(),
       localScope = localScopeOverride ?? newMomentOperationID() {
    if (!humanControlID(localScope)) throw const FormatException('图片材料入口标识不符');
    if (journalEntryID.isNotEmpty && !humanControlID(journalEntryID)) {
      throw const FormatException('串文材料条目标识不符');
    }
    _owner = ownerID();
    _initialHeader = _header();
    api = PublicMomentMediaSetAPI(
      client: client,
      apiBaseUrl: apiBaseUrl,
      authorizationHeader: _header,
      current: () => current,
    );
    boundaryChanges?.addListener(observe);
  }
  final String momentID, localScope, journalEntryID;
  final bool materialOnly;
  final int expectedMomentRevision;
  final String? Function() ownerID, _header;
  final bool Function() sourceCurrent;
  final Listenable? boundaryChanges;
  final PublicMomentMediaPendingStore _store;
  late final PublicMomentMediaSetAPI api;
  String? _owner, _initialHeader;
  bool _disposed = false, _retired = false;
  int _epoch = 0, _selection = 0;
  int get selectionRevision => _selection;
  Timer? _previewTimer, _setTimer;
  final images = <PublicMomentMediaDraftImage>[];
  List<PublicMomentMediaPending> pending = [];
  PublicMomentMediaSet? state;
  PublicMomentMediaPreview? preview;
  bool busy = false, journalReady = false, uncertain = false;
  String? error, notice;
  bool get current {
    if (!_disposed &&
        !_retired &&
        _owner != null &&
        ownerID() == _owner &&
        _header() != null &&
        _header() != _initialHeader &&
        sourceCurrent()) {
      _initialHeader = _header();
      ++_epoch;
      _clearApproval();
      state = null;
      busy = false;
      uncertain = pending.isNotEmpty;
      notice = '登录状态已更新，请重新检查当前图片。';
    }
    if (!_disposed &&
        !_retired &&
        (_owner == null ||
            ownerID() != _owner ||
            _header() == null ||
            !sourceCurrent())) {
      _retired = true;
      ++_epoch;
      _clearImages();
      _clearApproval();
      state = null;
      pending = [];
      busy = false;
      journalReady = false;
      _previewTimer?.cancel();
      _setTimer?.cancel();
    }
    return !_disposed && !_retired;
  }

  bool get canEdit => current && !busy && !uncertain && pending.isEmpty;
  bool get canPreview =>
      canEdit &&
      journalReady &&
      (materialOnly || state?.current == true && state!.canReplace) &&
      images.isNotEmpty &&
      images.every((p) => p.rights?.valid == true);
  void observe() {
    final epoch = _epoch, oldPreview = preview, oldState = state;
    final active = current;
    if (!_disposed &&
        (!active ||
            epoch != _epoch ||
            oldPreview != preview ||
            oldState != state)) {
      notifyListeners();
    }
  }

  bool Function() _capture() {
    final epoch = _epoch;
    return () => current && epoch == _epoch;
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _clearApproval() {
    for (final bytes in preview?.thumbnails ?? <Uint8List>[]) {
      bytes.fillRange(0, bytes.length, 0);
    }
    preview = null;
    _previewTimer?.cancel();
  }

  void _clearImages() {
    for (final image in images) {
      image.bytes?.fillRange(0, image.bytes!.length, 0);
      image.bytes = null;
      image.videoSelection?.bytes.fillRange(
        0,
        image.videoSelection!.bytes.length,
        0,
      );
      image.poster?.fillRange(0, image.poster!.length, 0);
    }
    images.clear();
  }

  void _emitFact(PublicMomentMediaPending operation) {
    publicMomentMediaChanges.value = PublicMomentMediaLocalChange(
      ownerID: _owner!,
      origin: api.origin,
      momentID: operation.targetID,
      momentRevision: operation.momentRevision,
      setVersion: operation.setVersion + 1,
      action: operation.action,
    );
  }

  Future<void> load() async {
    if (!current || busy) return;
    final valid = _capture();
    busy = true;
    error = null;
    _notify();
    try {
      final operations = await _store.read(api.origin, _owner!);
      if (!valid()) return;
      pending = operations
          .where(
            (p) => p.kind == 'STAGE'
                ? p.targetID == localScope && p.journalEntryID == journalEntryID
                : !materialOnly && p.targetID == momentID,
          )
          .toList();
      uncertain = pending.isNotEmpty;
      journalReady = true;
      if (materialOnly) return;
      final next = await api.ownSet(momentID, expectedMomentRevision);
      if (!valid()) return;
      state = next;
      _setTimer?.cancel();
      _setTimer = Timer(next.validUntil.difference(DateTime.now().toUtc()), () {
        if (current) {
          state = null;
          _clearApproval();
          notice = '图片资料已到期，请重新读取。';
          _notify();
        }
      });
    } catch (_) {
      if (valid()) {
        error = '当前图片资料暂不可读取，请重试。';
        state = null;
      }
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  void addSelectedBytes(List<Uint8List> selected) {
    if (!canEdit) return;
    if (images.any(
      (item) => item.mime == 'video/mp4' || item.stage?.isVideo == true,
    )) {
      error = '一条动态只能添加图片，或者一段视频。请先删除视频材料。';
      _notify();
      return;
    }
    if (images.length + selected.length > 9) {
      error = '一条动态最多添加 9 张图片。';
      _notify();
      return;
    }
    final additions = <PublicMomentMediaDraftImage>[];
    try {
      for (final bytes in selected) {
        if (bytes.isEmpty || bytes.length > 5 * 1024 * 1024)
          throw const FormatException('图片大小超限');
        final header = MomentImageHeader.inspect(bytes);
        if (!const {'image/jpeg', 'image/png'}.contains(header.mime) ||
            header.width > 8192 ||
            header.height > 8192 ||
            header.width * header.height > 16 * 1024 * 1024)
          throw const FormatException('图片规格超限');
        additions.add(
          PublicMomentMediaDraftImage(
            operationID: newMomentOperationID(),
            bytes: Uint8List.fromList(bytes),
            mime: header.mime,
          ),
        );
      }
      images.addAll(additions);
      ++_selection;
      _clearApproval();
      error = notice = null;
    } catch (_) {
      for (final image in additions) {
        image.bytes?.fillRange(0, image.bytes!.length, 0);
      }
      error = '请选择 5MiB 以内的静态 JPEG 或 PNG 图片。';
    }
    _notify();
  }

  void remove(int index) {
    if (!canEdit || index < 0 || index >= images.length) return;
    final image = images.removeAt(index);
    image.bytes?.fillRange(0, image.bytes!.length, 0);
    image.bytes = null;
    image.videoSelection?.bytes.fillRange(
      0,
      image.videoSelection!.bytes.length,
      0,
    );
    image.poster?.fillRange(0, image.poster!.length, 0);
    ++_selection;
    _clearApproval();
    _notify();
  }

  void addSelectedVideo(PublicMomentVideoSelection selected) {
    if (!canEdit) {
      return;
    }
    if (images.isNotEmpty) {
      error = '一条动态只能添加最多九张图片，或者一段视频。请先删除已有材料。';
      _notify();
      return;
    }
    final copy = PublicMomentVideoSelection(
      bytes: selected.bytes,
      durationMS: selected.durationMS,
      width: selected.width,
      height: selected.height,
    );
    images.add(
      PublicMomentMediaDraftImage(
        operationID: newMomentOperationID(),
        bytes: copy.bytes,
        mime: 'video/mp4',
        videoSelection: copy,
      ),
    );
    ++_selection;
    _clearApproval();
    error = notice = null;
    _notify();
  }

  void copyDraftTo(PublicMomentMediaController next) {
    if (_disposed ||
        _retired ||
        pending.isNotEmpty ||
        !next.current ||
        _owner == null ||
        _owner != next._owner ||
        api.origin != next.api.origin ||
        localScope != next.localScope ||
        journalEntryID != next.journalEntryID ||
        next.images.isNotEmpty) {
      throw const FormatException('只能保留同一本人材料草稿');
    }
    for (final old in images) {
      final video = old.videoSelection;
      final copyVideo = video == null
          ? null
          : PublicMomentVideoSelection(
              bytes: video.bytes,
              durationMS: video.durationMS,
              width: video.width,
              height: video.height,
            );
      final copy = PublicMomentMediaDraftImage(
        operationID: old.operationID,
        bytes:
            copyVideo?.bytes ??
            (old.bytes == null ? null : Uint8List.fromList(old.bytes!)),
        mime: old.mime,
        stage: old.stage,
        videoSelection: copyVideo,
      )..rights = old.rights;
      copy.poster = old.poster == null ? null : Uint8List.fromList(old.poster!);
      next.images.add(copy);
    }
    next._selection++;
    next._notify();
  }

  void reorder(int from, int to) {
    if (!canEdit ||
        from < 0 ||
        to < 0 ||
        from >= images.length ||
        to >= images.length)
      return;
    final image = images.removeAt(from);
    images.insert(to, image);
    ++_selection;
    _clearApproval();
    _notify();
  }

  void setRights(int index, EntityCoverRights? rights) {
    if (!canEdit || index < 0 || index >= images.length) return;
    images[index].rights = rights;
    ++_selection;
    _clearApproval();
    _notify();
  }

  Future<bool> _persist(
    PublicMomentMediaPending operation,
    bool Function() valid,
  ) async {
    await _store.write(api.origin, _owner!, operation);
    if (!valid()) return false;
    final rows = await _store.read(api.origin, _owner!);
    if (!valid()) return false;
    if (!rows.any((p) => p.matches(operation))) throw StateError('原操作未确认保存');
    pending = [
      ...pending.where((p) => p.operationID != operation.operationID),
      operation,
    ];
    return true;
  }

  Future<void> _retirePending(
    PublicMomentMediaPending operation,
    bool Function() valid,
  ) async {
    await _store.remove(api.origin, _owner!, operation);
    if (!valid()) return;
    pending.removeWhere((p) => p.operationID == operation.operationID);
    uncertain = pending.isNotEmpty;
  }

  PublicMomentMediaStage? _stageReceipt(
    Object? raw,
    PublicMomentMediaPending p,
  ) {
    final v = mediaObject(raw, {
      'operationId',
      'status',
      'payloadDigest',
      'material',
      'committedAt',
    });
    humanControlTime(v['committedAt']);
    if (v['operationId'] != p.operationID)
      throw const FormatException('原图片准备回执不符');
    if (v['status'] == 'STOPPED') {
      if (v['material'] != null || v['payloadDigest'] != '')
        throw const FormatException('原图片准备停止回执不符');
      return null;
    }
    if (v['status'] != 'SUCCEEDED' || v['payloadDigest'] != p.digest)
      throw const FormatException('原图片准备事实不符');
    final stage = PublicMomentMediaStage.decode(v['material']);
    if (stage.localScope != p.targetID)
      throw const FormatException('原图片准备归属不符');
    return stage;
  }

  bool _setReceipt(Object? raw, PublicMomentMediaPending p) {
    final v = mediaObject(raw, {
      'operationId',
      'status',
      'momentId',
      'action',
      'expectedMomentRevision',
      'expectedSetVersion',
      'appliedSetVersion',
      'payloadDigest',
      'media',
      'committedAt',
    });
    humanControlTime(v['committedAt']);
    if (v['operationId'] != p.operationID || v['momentId'] != p.targetID)
      throw const FormatException('原图片集合回执不符');
    if (v['status'] == 'STOPPED') {
      if (v['action'] != '' ||
          v['payloadDigest'] != '' ||
          v['appliedSetVersion'] != 0 ||
          (v['media'] as List).isNotEmpty)
        throw const FormatException('原图片集合停止回执不符');
      return false;
    }
    if (v['status'] != 'SUCCEEDED' ||
        v['payloadDigest'] != p.digest ||
        v['action'] != p.action ||
        v['expectedMomentRevision'] != p.momentRevision ||
        v['expectedSetVersion'] != p.setVersion ||
        v['appliedSetVersion'] != p.setVersion + 1 ||
        jsonEncode(v['media']) != jsonEncode(p.media))
      throw const FormatException('原图片集合发布事实不符');
    return true;
  }

  Future<bool> _prepareStages(bool Function() valid, int generation) async {
    for (final image in images) {
      if (image.stage?.current == true) {
        if (!await _videoPoster(image, valid, generation)) {
          return false;
        }
        continue;
      }
      if (image.stage != null) image.operationID = newMomentOperationID();
      if (image.bytes == null || image.mime == null)
        throw const FormatException('请重新选择原图片');
      final digest = humanControlTupleDigest([
        image.mime == 'video/mp4'
            ? 'moment-video-stage-v2'
            : 'moment-media-stage-v1',
        image.operationID,
        localScope,
        if (image.mime == 'video/mp4') publicMomentVideoPurpose,
        image.mime!,
        sha256.convert(image.bytes!).toString(),
      ]);
      final operation = PublicMomentMediaPending(
        kind: 'STAGE',
        operationID: image.operationID,
        targetID: localScope,
        digest: digest,
        momentRevision: 0,
        setVersion: 0,
        action: 'PREPARE',
        media: const [],
        journalEntryID: journalEntryID,
      );
      if (!await _persist(operation, valid)) return false;
      Object? response;
      try {
        response = await api.request(
          'PUT',
          image.mime == 'video/mp4'
              ? '/v1/me/moment-media-video-materials/operations/${image.operationID}'
              : '/v1/me/moment-media-materials/operations/${image.operationID}',
          body: {
            'operationId': image.operationID,
            'localScope': localScope,
            if (image.mime == 'video/mp4') 'purpose': publicMomentVideoPurpose,
            'contentType': image.mime!,
            'contentBase64': base64Encode(image.bytes!),
          },
        );
      } catch (_) {
        if (valid()) uncertain = true;
        rethrow;
      }
      if (!valid() || generation != _selection) return false;
      image.stage = _stageReceipt(response, operation);
      await _retirePending(operation, valid);
      if (image.stage?.current != true) {
        image.operationID = newMomentOperationID();
        throw const FormatException('图片准备已停止或到期');
      }
      if (!valid()) return false;
      image.bytes?.fillRange(0, image.bytes!.length, 0);
      image.bytes = null;
      image.videoSelection = null;
      if (!await _videoPoster(image, valid, generation)) {
        return false;
      }
    }

    return true;
  }

  Future<bool> _videoPoster(
    PublicMomentMediaDraftImage image,
    bool Function() valid,
    int generation,
  ) async {
    if (image.stage?.isVideo != true || image.poster != null) {
      return true;
    }
    final pixels = await api.stageVideoContent(image.stage!, thumbnail: true);
    if (!valid() || generation != _selection) {
      pixels.fillRange(0, pixels.length, 0);
      return false;
    }
    image.poster = pixels;
    return true;
  }

  Future<List<PublicMomentMediaSelection>?> prepareMaterials() async {
    if (!canEdit ||
        !journalReady ||
        images.isEmpty ||
        images.any((p) => p.rights?.valid != true))
      return null;
    final valid = _capture(), generation = _selection;
    busy = true;
    error = notice = null;
    _clearApproval();
    _notify();
    try {
      if (!await _prepareStages(valid, generation) || !valid()) return null;
      notice = '图片已准备，待随串文检查和确认。';
      return List.unmodifiable([
        for (final image in images)
          PublicMomentMediaSelection(
            image.stage!.mediaID,
            image.stage!.version,
            image.rights!,
          ),
      ]);
    } catch (_) {
      if (valid()) {
        uncertain = pending.isNotEmpty;
        error = uncertain ? '原图片准备结果待核实，请先读取原操作。' : '图片尚未准备成功，请检查后重试。';
      }
      return null;
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> prepare({bool delete = false}) async {
    if (materialOnly) {
      if (!delete) await prepareMaterials();
      return;
    }
    if (delete
        ? !(canEdit && journalReady && state?.canDelete == true)
        : !canPreview)
      return;
    final valid = _capture(), generation = _selection;
    busy = true;
    error = notice = null;
    _clearApproval();
    _notify();
    try {
      if (!delete && !await _prepareStages(valid, generation)) return;
      final input = PublicMomentMediaInput(
        momentID: momentID,
        operationID: newMomentOperationID(),
        action: delete ? 'DELETE' : 'REPLACE',
        momentRevision: expectedMomentRevision,
        setVersion: state!.setVersion,
        images: delete
            ? const []
            : [
                for (final image in images)
                  PublicMomentMediaSelection(
                    image.stage!.mediaID,
                    image.stage!.version,
                    image.rights!,
                  ),
              ],
      );
      final operation = PublicMomentMediaPending(
        kind: 'SET',
        operationID: input.operationID,
        targetID: momentID,
        digest: input.digest,
        momentRevision: input.momentRevision,
        setVersion: input.setVersion,
        action: input.action,
        media: [
          for (final image in input.images)
            {'mediaId': image.mediaID, 'mediaVersion': image.version},
        ],
      );
      if (!await _persist(operation, valid)) return;
      final response = await api.request(
        'POST',
        '/v1/me/moments/$momentID/public-media/previews',
        body: input.toJson(),
      );
      if (!valid() || generation != _selection) return;
      preview = PublicMomentMediaPreview.decode(response, input);
      _previewTimer = Timer(
        preview!.expiresAt.difference(DateTime.now().toUtc()),
        () {
          if (current) {
            _clearApproval();
            notice = '这次图片批准已到期，请先停止原操作后重新检查。';
            _notify();
          }
        },
      );
    } catch (_) {
      if (valid()) {
        uncertain = pending.isNotEmpty;
        error = uncertain ? '图片操作结果待核实，材料仍在；请先读取原操作结果。' : '图片尚未准备成功，材料仍在，请重试。';
      }
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<bool> commit() async {
    final approved = preview;
    if (!current || busy || approved == null || !approved.current) return false;
    final valid = _capture();
    final operation = pending
        .where((p) => p.operationID == approved.input.operationID)
        .firstOrNull;
    if (operation == null) return false;
    busy = true;
    error = null;
    bool confirmed = false;
    _notify();
    try {
      final result = await api.request(
        'PUT',
        '/v1/me/moments/$momentID/public-media/operations/${operation.operationID}',
        body: approved.input.toJson(
          source: approved.sourceVersion,
          approval: approved.approval,
        ),
      );
      if (!valid()) return false;
      confirmed = _setReceipt(result, operation);
      if (confirmed) {
        _emitFact(operation);
        _clearApproval();
        _clearImages();
      }
      await _retirePending(operation, valid);
      if (!valid()) return false;
      notice = confirmed
          ? (operation.action == 'DELETE' ? '这一版图片集合已撤回。' : '全部图片已公开，顺序与批准一致。')
          : '原图片操作已停止，材料仍在。';
      if (!materialOnly)
        state = await api.ownSet(momentID, expectedMomentRevision);
    } catch (_) {
      if (valid()) {
        uncertain = true;
        _clearApproval();
        error = confirmed
            ? '图片操作已成功；当前资料或本机记录尚待核实。'
            : '图片集合提交结果待核实；请读取原操作结果，避免重复提交。';
      }
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
    return confirmed;
  }

  Future<void> recover(
    PublicMomentMediaPending operation, {
    bool stop = false,
  }) async {
    if (!current ||
        busy ||
        operation.kind == 'STAGE' &&
            (operation.targetID != localScope ||
                operation.journalEntryID != journalEntryID) ||
        materialOnly && operation.kind != 'STAGE')
      return;
    final valid = _capture();
    busy = true;
    error = null;
    _notify();
    try {
      final path = operation.kind == 'STAGE'
          ? '/v1/me/moment-media-materials/operations/${operation.operationID}'
          : '/v1/me/moments/${operation.targetID}/public-media/operations/${operation.operationID}';
      final result = await api.request(
        stop ? 'POST' : 'GET',
        stop ? '$path/stop' : path,
      );
      if (!valid()) return;
      if (operation.kind == 'STAGE') {
        final stage = _stageReceipt(result, operation);
        if (stage != null && stage.current) {
          final image = images
              .where((i) => i.operationID == operation.operationID)
              .firstOrNull;
          if (image != null) {
            image.stage = stage;
          } else if (images.length < 9) {
            images.add(
              PublicMomentMediaDraftImage(
                operationID: operation.operationID,
                stage: stage,
              ),
            );
          }
        }
      } else {
        if (_setReceipt(result, operation)) {
          _emitFact(operation);
          _clearImages();
        }
        _clearApproval();
      }
      await _retirePending(operation, valid);
      if (!valid()) return;
      notice = '原图片操作已核实，请检查当前图片集合。';
      if (!materialOnly)
        state = await api.ownSet(momentID, expectedMomentRevision);
    } catch (_) {
      if (valid()) error = '原操作尚未核实，可以重读或明确停止；不会自动重发。';
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    boundaryChanges?.removeListener(observe);
    _disposed = true;
    ++_epoch;
    _previewTimer?.cancel();
    _setTimer?.cancel();
    _clearImages();
    _clearApproval();
    api.dispose();
    super.dispose();
  }
}
