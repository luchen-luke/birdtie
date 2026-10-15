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
  Timer? _previewTimer, _setTimer, _stageTimer;
  final images = <PublicMomentMediaDraftImage>[];
  List<PublicMomentMediaPending> pending = [];
  List<PublicMomentMediaPending> _staged = [];
  bool _stagedReadFailed = false, _journalWriteFailed = false;
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
      _stageTimer?.cancel();
    }
    return !_disposed && !_retired;
  }

  bool get canEdit =>
      current && !busy && !uncertain && pending.isEmpty && !_journalWriteFailed;
  bool get hasUnresolvedStagedMaterials =>
      _stagedReadFailed ||
      _journalWriteFailed ||
      _staged.any((reference) {
        final image = images
            .where((p) => p.operationID == reference.operationID)
            .firstOrNull;
        return image?.stage?.current != true;
      });
  List<String> get stagedOperationIDs =>
      List.unmodifiable(_staged.map((p) => p.operationID));
  bool get canPreview =>
      canEdit &&
      journalReady &&
      !hasUnresolvedStagedMaterials &&
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
      final scoped = operations
          .where(
            (p) => p.kind == 'STAGE'
                ? p.targetID == localScope && p.journalEntryID == journalEntryID
                : !materialOnly && p.targetID == momentID,
          )
          .toList();
      pending = scoped.where((p) => !p.staged).toList();
      _staged = scoped.where((p) => p.staged).toList()
        ..sort((a, b) => a.materialOrder!.compareTo(b.materialOrder!));
      uncertain = pending.isNotEmpty;
      journalReady = true;
      _journalWriteFailed = false;
      await _restoreStagedMaterials(valid);
      if (!valid()) return;
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
        journalReady = false;
        _stagedReadFailed = true;
        state = null;
      }
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _restoreStagedMaterials(bool Function() valid) async {
    _stagedReadFailed = false;
    if (_staged.isEmpty) return;
    final restored = <PublicMomentMediaDraftImage>[];
    final stagedIDs = _staged.map((p) => p.operationID).toSet();
    final local = images
        .where((p) => !stagedIDs.contains(p.operationID))
        .toList();
    for (final reference in _staged) {
      final existing = images
          .where((p) => p.operationID == reference.operationID)
          .firstOrNull;
      final image =
          existing ??
          PublicMomentMediaDraftImage(operationID: reference.operationID);
      image.rights = null;
      try {
        final result = await api.request(
          'GET',
          '/v1/me/moment-media-materials/operations/${reference.operationID}',
        );
        if (!valid()) return;
        image.stage = _stageReceipt(result, reference);
        if (image.stage?.current != true) {
          _stagedReadFailed = true;
        } else if (!await _videoPoster(image, valid, _selection)) {
          return;
        }
      } catch (_) {
        if (!valid()) return;
        image.stage = null;
        image.rights = null;
        _stagedReadFailed = true;
      }
      restored.add(image);
    }
    if (!valid()) return;
    images
      ..clear()
      ..addAll(restored)
      ..addAll(local);
    _armStageLease();
    if (images.length > 9 ||
        images.length > 1 && images.any((p) => p.stage?.isVideo == true)) {
      _stagedReadFailed = true;
    }
    if (_stagedReadFailed) {
      error = '部分暂存材料已过期或暂不可核实。请重试读取，或明确移除后重新选择；不会直接按纯文字发布。';
    } else {
      notice = '已找回原暂存材料，请重新确认公开使用权。原暂存期限没有延长。';
    }
  }

  Future<bool> _saveStagedSelection(
    List<PublicMomentMediaPending> selection,
    bool Function() valid,
  ) async {
    try {
      await _store.saveStagedSelection(
        api.origin,
        _owner!,
        localScope,
        journalEntryID,
        _staged,
        selection,
      );
      if (!valid()) return false;
      _staged = List.of(selection);
      _armStageLease();
      _journalWriteFailed = false;
      pending.removeWhere(
        (p) => selection.any((s) => s.operationID == p.operationID),
      );
      uncertain = pending.isNotEmpty;
      return true;
    } catch (_) {
      if (valid()) {
        _journalWriteFailed = true;
        journalReady = false;
        error = '本机材料记录尚未保存，当前材料仍保留。请重试读取原记录后继续。';
      }
      rethrow;
    }
  }

  void _armStageLease() {
    _stageTimer?.cancel();
    final selected = _staged.map((p) => p.operationID).toSet();
    final deadlines =
        images
            .where(
              (p) =>
                  selected.contains(p.operationID) && p.stage?.current == true,
            )
            .map((p) => p.stage!.retainUntil)
            .toList()
          ..sort();
    if (deadlines.isEmpty) return;
    _stageTimer = Timer(deadlines.first.difference(DateTime.now().toUtc()), () {
      if (!current) return;
      for (final image in images.where(
        (p) => p.stage != null && !p.stage!.current,
      )) {
        image.poster?.fillRange(0, image.poster!.length, 0);
        image.poster = null;
        image.rights = null;
      }
      _stagedReadFailed = true;
      _clearApproval();
      error = '暂存材料已过期，请移除后重新选择。';
      _notify();
      _armStageLease();
    });
  }

  List<PublicMomentMediaPending> _orderedReferences(
    List<PublicMomentMediaDraftImage> ordered, {
    PublicMomentMediaPending? promoted,
  }) {
    final references = <PublicMomentMediaPending>[];
    for (final image in ordered) {
      final reference = promoted?.operationID == image.operationID
          ? promoted
          : _staged
                .where((p) => p.operationID == image.operationID)
                .firstOrNull;
      if (reference != null)
        references.add(reference.asStaged(references.length));
    }
    return references;
  }

  /// Call only after reading the original successful publication fact.
  /// This removes local known-stage selectors only. It remains safe after the
  /// host disposes its controller; unknown operations are never discarded.
  Future<bool> releaseStagedAfterPublication({
    Iterable<String>? operationIDs,
  }) async {
    final owner = _owner;
    if (owner == null || _staged.isEmpty) return true;
    final original = List<PublicMomentMediaPending>.of(_staged);
    final selected =
        operationIDs?.toSet() ?? original.map((p) => p.operationID).toSet();
    final remaining = <PublicMomentMediaPending>[];
    for (final reference in original) {
      if (!selected.contains(reference.operationID)) {
        remaining.add(reference.asStaged(remaining.length));
      }
    }
    try {
      await _store.saveStagedSelection(
        api.origin,
        owner,
        localScope,
        journalEntryID,
        original,
        remaining,
      );
      _staged = remaining;
      _armStageLease();
      return true;
    } catch (_) {
      if (!_disposed) {
        error = '发布结果已确认，本机材料记录尚未清理。请重试核实原结果。';
        _notify();
      }
      return false;
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

  Future<void> remove(int index) async {
    if (!canEdit || index < 0 || index >= images.length) return;
    ++_selection;
    _clearApproval();
    _notify();
    final ordered = List<PublicMomentMediaDraftImage>.of(images)
      ..removeAt(index);
    if (!await _persistReorder(ordered)) return;
    final image = images.removeAt(index);
    image.bytes?.fillRange(0, image.bytes!.length, 0);
    image.bytes = null;
    image.videoSelection?.bytes.fillRange(
      0,
      image.videoSelection!.bytes.length,
      0,
    );
    image.poster?.fillRange(0, image.poster!.length, 0);
    _stagedReadFailed = _staged.any(
      (p) =>
          images
              .where((i) => i.operationID == p.operationID)
              .firstOrNull
              ?.stage
              ?.current !=
          true,
    );
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

  Future<void> reorder(int from, int to) async {
    if (!canEdit ||
        from < 0 ||
        to < 0 ||
        from >= images.length ||
        to >= images.length)
      return;
    if (from == to) return;
    ++_selection;
    _clearApproval();
    _notify();
    final ordered = List<PublicMomentMediaDraftImage>.of(images);
    ordered.insert(to, ordered.removeAt(from));
    if (!await _persistReorder(ordered)) return;
    final image = images.removeAt(from);
    images.insert(to, image);
    _clearApproval();
    _notify();
  }

  Future<bool> _persistReorder(
    List<PublicMomentMediaDraftImage> ordered,
  ) async {
    if (_staged.isEmpty) return true;
    final valid = _capture();
    busy = true;
    _notify();
    try {
      return await _saveStagedSelection(_orderedReferences(ordered), valid);
    } catch (_) {
      return false;
    } finally {
      if (valid()) {
        busy = false;
        _notify();
      }
    }
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
        materialOrder: images.indexOf(image),
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
      if (image.stage == null) {
        await _retirePending(operation, valid);
      } else if (!await _saveStagedSelection(
        _orderedReferences(images, promoted: operation),
        valid,
      )) {
        return false;
      }
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
        hasUnresolvedStagedMaterials ||
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
      notice = '媒体已准备，待随动态检查和确认。';
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
        if (!await releaseStagedAfterPublication())
          throw StateError('本机材料尚未清理');
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
        if (stage != null) {
          final image = images
              .where((i) => i.operationID == operation.operationID)
              .firstOrNull;
          if (image != null) {
            image.stage = stage;
          } else if (images.length < 9) {
            images.insert(
              (operation.materialOrder ?? images.length).clamp(
                0,
                images.length,
              ),
              PublicMomentMediaDraftImage(
                operationID: operation.operationID,
                stage: stage,
              ),
            );
          }
          if (!await _saveStagedSelection(
            _orderedReferences(images, promoted: operation),
            valid,
          ))
            return;
          _stagedReadFailed = !stage.current;
        }
      } else {
        if (_setReceipt(result, operation)) {
          if (!await releaseStagedAfterPublication())
            throw StateError('本机材料尚未清理');
          _emitFact(operation);
          _clearImages();
        }
        _clearApproval();
      }
      if (pending.any((p) => p.operationID == operation.operationID)) {
        await _retirePending(operation, valid);
      }
      if (!valid()) return;
      notice = hasUnresolvedStagedMaterials
          ? '原操作已核实，材料已经过期。请移除后重新选择，原期限不会延长。'
          : '原材料操作已核实，请检查当前材料并重新确认使用权。';
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
    _stageTimer?.cancel();
    _clearImages();
    _clearApproval();
    api.dispose();
    super.dispose();
  }
}
