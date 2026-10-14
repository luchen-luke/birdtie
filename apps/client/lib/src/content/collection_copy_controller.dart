import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import 'collection_copy_pending_store.dart';
import 'personal_collection_controller.dart';

DateTime _copyTime(Object? raw) {
  if (raw is! String || !RegExp(r'Z$|[+-]\d\d:\d\d$').hasMatch(raw))
    throw const FormatException('副本来源期限不符');
  final d = DateTime.tryParse(raw)?.toUtc();
  if (d == null) throw const FormatException('副本来源期限不符');
  return d;
}

String collectionCopyPlaceDigest(List<String> ids) => sha256
    .convert(utf8.encode(jsonEncode(['collection-copy-places-v1', ids])))
    .toString();
String collectionCopyInputDigest(
  CollectionCopyReference ref,
  String operation,
  int revision,
  int grant,
  String placeDigest,
  String title,
) => sha256
    .convert(
      utf8.encode(
        jsonEncode([
          'collection-private-copy-v1',
          ref.kind,
          ref.id,
          operation,
          revision,
          grant,
          placeDigest,
          sha256.convert(utf8.encode(title)).toString(),
        ]),
      ),
    )
    .toString();
bool collectionCopySubsequence(List<String> selected, List<String> eligible) {
  var position = 0;
  for (final id in selected) {
    while (position < eligible.length && eligible[position] != id) {
      position++;
    }
    if (position == eligible.length) return false;
    position++;
  }
  return true;
}

class CollectionCopyGrant {
  CollectionCopyGrant._(
    this.sourceRef,
    this.publicationRevision,
    this.grantVersion,
    this.eligiblePlaceIDs,
    this.orderedPlaceIDsHash,
    this.expiresAt,
  );
  final CollectionCopyReference sourceRef;
  final int publicationRevision, grantVersion;
  final List<String> eligiblePlaceIDs;
  final String orderedPlaceIDsHash;
  final DateTime expiresAt;
  bool get current => expiresAt.isAfter(DateTime.now().toUtc());
  bool sameChoiceSource(CollectionCopyGrant other) =>
      sourceRef.same(other.sourceRef) &&
      publicationRevision == other.publicationRevision &&
      grantVersion == other.grantVersion &&
      orderedPlaceIDsHash == other.orderedPlaceIDsHash;
  factory CollectionCopyGrant.fromJson(
    Object? raw, {
    required CollectionCopyReference source,
    required int publicationRevision,
    required DateTime sourceExpiresAt,
    required List<String> publicPlaceIDs,
  }) {
    final m = collectionCopyObject(raw, {
      'sourceRef',
      'publicationRevision',
      'grantVersion',
      'eligiblePlaceIds',
      'orderedPlaceIdsSha256',
      'expiresAt',
    });
    final ref = CollectionCopyReference.fromJson(m['sourceRef']),
        expires = _copyTime(m['expiresAt']),
        now = DateTime.now().toUtc();
    if (!ref.same(source) ||
        !collectionCopyVersion(m['publicationRevision']) ||
        m['publicationRevision'] != publicationRevision ||
        publicationRevision < 2 ||
        !collectionCopyVersion(m['grantVersion']) ||
        m['eligiblePlaceIds'] is! List ||
        (m['eligiblePlaceIds'] as List).isEmpty ||
        (m['eligiblePlaceIds'] as List).length > 50 ||
        !collectionCopyHash(m['orderedPlaceIdsSha256']) ||
        !expires.isAfter(now) ||
        expires.isAfter(sourceExpiresAt) ||
        expires.isAfter(now.add(const Duration(seconds: 61))))
      throw const FormatException('公开副本许可与来源不符');
    final ids = (m['eligiblePlaceIds'] as List)
        .map((v) {
          if (!collectionCopyID(v)) throw const FormatException('获准复制地点不符');
          return v as String;
        })
        .toList(growable: false);
    if (ids.toSet().length != ids.length ||
        !listEquals(ids, publicPlaceIDs) ||
        m['orderedPlaceIdsSha256'] != collectionCopyPlaceDigest(ids))
      throw const FormatException('获准复制地点顺序或摘要不符');
    return CollectionCopyGrant._(
      ref,
      publicationRevision,
      m['grantVersion'],
      List.unmodifiable(ids),
      m['orderedPlaceIdsSha256'],
      expires,
    );
  }
}

class CollectionCopyInput {
  CollectionCopyInput({
    required this.sourceRef,
    required this.operationID,
    required this.publicationRevision,
    required this.grantVersion,
    required List<String> orderedPlaceIDs,
    required this.title,
  }) : orderedPlaceIDs = List.unmodifiable(orderedPlaceIDs);
  final CollectionCopyReference sourceRef;
  final String operationID, title;
  final int publicationRevision, grantVersion;
  final List<String> orderedPlaceIDs;
  String get placeDigest => collectionCopyPlaceDigest(orderedPlaceIDs);
  String get inputDigest => collectionCopyInputDigest(
    sourceRef,
    operationID,
    publicationRevision,
    grantVersion,
    placeDigest,
    title,
  );
  bool get valid =>
      sourceRef.valid &&
      collectionCopyID(operationID) &&
      collectionCopyVersion(publicationRevision) &&
      publicationRevision >= 2 &&
      collectionCopyVersion(grantVersion) &&
      orderedPlaceIDs.isNotEmpty &&
      orderedPlaceIDs.length <= 50 &&
      orderedPlaceIDs.every(collectionCopyID) &&
      orderedPlaceIDs.toSet().length == orderedPlaceIDs.length &&
      title == title.trim() &&
      !title.contains('\u0000') &&
      utf8.encode(title).isNotEmpty &&
      utf8.encode(title).length <= 160;
  Map<String, Object> toJson({String? approval}) {
    if (!valid) throw const FormatException('请填写本人标题并选择当前获准的地点。');
    return {
      'operationId': operationID,
      'publicationRevision': publicationRevision,
      'grantVersion': grantVersion,
      'orderedPlaceIds': orderedPlaceIDs,
      'orderedPlaceIdsSha256': placeDigest,
      'title': title,
      if (approval != null) 'approval': approval,
    };
  }

  CollectionCopySelector get selector => CollectionCopySelector(
    sourceRef: sourceRef,
    operationID: operationID,
    publicationRevision: publicationRevision,
    grantVersion: grantVersion,
    orderedPlaceIDsHash: placeDigest,
    inputHash: inputDigest,
  );
}

class CollectionCopyPreview {
  const CollectionCopyPreview(this.input, this.approval, this.expiresAt);
  final CollectionCopyInput input;
  final String approval;
  final DateTime expiresAt;
  factory CollectionCopyPreview.fromJson(
    Object? raw,
    CollectionCopyInput input,
    DateTime grantExpiry,
  ) {
    final m = collectionCopyObject(raw, {
      'sourceRef',
      'operationId',
      'publicationRevision',
      'grantVersion',
      'orderedPlaceIdsSha256',
      'entryCount',
      'title',
      'inputSha256',
      'approval',
      'expiresAt',
    });
    final expires = _copyTime(m['expiresAt']), now = DateTime.now().toUtc();
    if (!input.valid ||
        !CollectionCopyReference.fromJson(
          m['sourceRef'],
        ).same(input.sourceRef) ||
        m['operationId'] != input.operationID ||
        !collectionCopyVersion(m['publicationRevision']) ||
        m['publicationRevision'] != input.publicationRevision ||
        !collectionCopyVersion(m['grantVersion']) ||
        m['grantVersion'] != input.grantVersion ||
        m['orderedPlaceIdsSha256'] != input.placeDigest ||
        m['inputSha256'] != input.inputDigest ||
        m['entryCount'] is! int ||
        m['entryCount'] != input.orderedPlaceIDs.length ||
        m['title'] != input.title ||
        m['approval'] is! String ||
        (m['approval'] as String).isEmpty ||
        utf8.encode(m['approval'] as String).length > 8192 ||
        !expires.isAfter(now) ||
        expires.isAfter(grantExpiry) ||
        expires.isAfter(now.add(const Duration(seconds: 31))))
      throw const FormatException('这次副本确认与原输入、许可或期限不符');
    return CollectionCopyPreview(input, m['approval'], expires);
  }
}

class CollectionCopyReceipt {
  const CollectionCopyReceipt(this.selector, this.status, this.destinationRef);
  final CollectionCopySelector selector;
  final String status;
  final CollectionCopyReference? destinationRef;
  factory CollectionCopyReceipt.fromJson(
    Object? raw,
    CollectionCopySelector selector,
  ) {
    if (raw is! Map<String, dynamic> || !selector.valid)
      throw const FormatException('原副本回执不符');
    final stopped = raw['status'] == 'STOPPED';
    final m = collectionCopyObject(
      raw,
      stopped
          ? {'operationId', 'status', 'resultRevision', 'destinationRef'}
          : {
              'operationId',
              'status',
              'resultRevision',
              'destinationRef',
              'sourceRef',
              'publicationRevision',
              'grantVersion',
              'orderedPlaceIdsSha256',
            },
    );
    if (m['operationId'] != selector.operationID || m['resultRevision'] is! int)
      throw const FormatException('原副本操作标识不符');
    if (stopped) {
      if (m['resultRevision'] != 0 || m['destinationRef'] != null)
        throw const FormatException('原停止回执不符');
      return CollectionCopyReceipt(selector, 'STOPPED', null);
    }
    final ref = CollectionCopyReference.fromJson(m['destinationRef']);
    if (m['status'] != 'SUCCEEDED' ||
        m['resultRevision'] != 1 ||
        ref.kind != selector.sourceRef.kind ||
        ref.id == selector.operationID ||
        ref.id == selector.sourceRef.id ||
        !CollectionCopyReference.fromJson(
          m['sourceRef'],
        ).same(selector.sourceRef) ||
        !collectionCopyVersion(m['publicationRevision']) ||
        m['publicationRevision'] != selector.publicationRevision ||
        !collectionCopyVersion(m['grantVersion']) ||
        m['grantVersion'] != selector.grantVersion ||
        m['orderedPlaceIdsSha256'] != selector.orderedPlaceIDsHash)
      throw const FormatException('原副本结果、来源或目标不符');
    return CollectionCopyReceipt(selector, 'SUCCEEDED', ref);
  }
}

class CollectionCopyFailure implements Exception {
  const CollectionCopyFailure(this.status);
  final int status;
  String get message => switch (status) {
    401 => '当前个人登录已失效。',
    403 => '当前没有复制此来源的许可。',
    404 => '当前来源或原操作不可读。',
    409 => '公开版本、复制许可或地点已变化，请重新读取并选择。',
    _ => '副本服务暂不可用，请稍后读取。',
  };
}

class CollectionCopyApi {
  const CollectionCopyApi({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  String publicPath(CollectionCopyReference ref) {
    if (!ref.valid) throw const FormatException('副本来源不符');
    return '/v1/public-collections/${ref.kind}/${ref.id}/copy';
  }

  String operationPath(String op) {
    if (!collectionCopyID(op)) throw const FormatException('副本操作标识不符');
    return '/v1/me/personal-collections/copy-operations/$op';
  }

  Future<Object?> json(
    String method,
    String path, {
    Map<String, Object>? body,
    required bool Function() current,
  }) async {
    final base = Uri.tryParse(baseUrl), header = authorizationHeader();
    if (base == null ||
        !const {'http', 'https'}.contains(base.scheme) ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty ||
        base.hasFragment ||
        base.hasQuery ||
        header == null ||
        !current())
      throw const CollectionCopyFailure(401);
    final uri = Uri.parse('${baseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
    if (uri.origin != base.origin) throw const FormatException('副本读取来源不符');
    final request = http.Request(method, uri)
      ..followRedirects = false
      ..headers['Authorization'] = header;
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }
    final response = await client
            .send(request)
            .timeout(const Duration(seconds: 12)),
        bytes = BytesBuilder(copy: false);
    await for (final b in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (b.length > 1024 * 1024 - bytes.length)
        throw const FormatException('副本响应过大');
      bytes.add(b);
    }
    if (!current() || header != authorizationHeader())
      throw const CollectionCopyFailure(401);
    if (response.statusCode != 200)
      throw CollectionCopyFailure(response.statusCode);
    return collectionCopyObject(jsonDecode(utf8.decode(bytes.takeBytes())), {
      'data',
    })['data'];
  }

  Future<CollectionCopyPreview> preview(
    CollectionCopyInput input,
    DateTime grantExpiry,
    bool Function() current,
  ) async => CollectionCopyPreview.fromJson(
    await json(
      'POST',
      '${publicPath(input.sourceRef)}/preview',
      body: input.toJson(),
      current: current,
    ),
    input,
    grantExpiry,
  );
  Future<CollectionCopyReceipt> apply(
    CollectionCopyPreview preview,
    bool Function() current,
  ) async {
    if (!preview.expiresAt.isAfter(DateTime.now().toUtc()))
      throw const FormatException('这次副本确认已到期');
    return CollectionCopyReceipt.fromJson(
      await json(
        'PUT',
        publicPath(preview.input.sourceRef),
        body: preview.input.toJson(approval: preview.approval),
        current: current,
      ),
      preview.input.selector,
    );
  }

  Future<CollectionCopyReceipt> operation(
    CollectionCopySelector selector,
    bool Function() current, {
    bool stop = false,
  }) async => CollectionCopyReceipt.fromJson(
    await json(
      stop ? 'POST' : 'GET',
      '${operationPath(selector.operationID)}/${stop ? 'stop' : 'receipt'}',
      current: current,
    ),
    selector,
  );
  Future<PersonalCollection> destination(
    CollectionCopyReceipt receipt,
    String owner,
    bool Function() current,
  ) async {
    final ref = receipt.destinationRef;
    if (ref == null) throw const FormatException('停止的操作没有副本');
    final raw = await json(
      'GET',
      '/v1/me/personal-collections/${ref.kind}/${ref.id}',
      current: current,
    );
    if (raw is! Map<String, dynamic>) throw const FormatException('本人的副本资料不符');
    final v = PersonalCollection.fromJson(raw);
    if (v.id != ref.id ||
        v.kind != ref.kind ||
        v.ownerID != owner ||
        v.revision < 1 ||
        v.status == 'withdrawn' ||
        v.revision == 1 &&
            (!v.privatePlaceCopyRestricted ||
                v.visibility != 'private' ||
                v.status != 'draft' ||
                v.summary.isNotEmpty ||
                v.entries.any(
                  (e) => e.note.isNotEmpty || e.locationVisibility != 'hidden',
                )))
      throw const FormatException('本人的副本当前来源不符');
    return v;
  }
}

/// New private copy has its own writer and causal journal. Ordinary collection
/// create/update is never used to bypass the author's current copy grant.
class CollectionCopyController extends ChangeNotifier {
  CollectionCopyController({
    required this.api,
    required this.ownerID,
    required this.sourceCurrent,
    this.sourceRef,
    this.grant,
    this.identityChanges,
    CollectionCopyPendingStore? pendingStore,
    PersonalCollectionCopyLocalDraft? localDraft,
    this.onRetainLocalDraft,
  }) : pendingStore = pendingStore ?? SecureCollectionCopyPendingStore() {
    _owner = ownerID();
    _header = api.authorizationHeader();
    if (localDraft != null &&
        localDraft.valid &&
        sourceRef?.kind == localDraft.sourceKind &&
        sourceRef?.id == localDraft.sourceID) {
      title = localDraft.title;
      _selected = localDraft.selectedPlaceIDs == null
          ? null
          : List.unmodifiable(localDraft.selectedPlaceIDs!);
    }
    identityChanges?.addListener(_observe);
    _observeGrant();
  }
  final CollectionCopyApi api;
  final String? Function() ownerID;
  final bool Function() sourceCurrent;
  final CollectionCopyReference? sourceRef;
  final CollectionCopyGrant? Function()? grant;
  final Listenable? identityChanges;
  final CollectionCopyPendingStore pendingStore;
  final ValueChanged<PersonalCollectionCopyLocalDraft>? onRetainLocalDraft;
  String? _owner, _header;
  bool _disposed = false, _retired = false, _storageReady = false;
  int _epoch = 0, _draftEpoch = 0;
  int get epoch => _epoch;
  String get origin => api.baseUrl.replaceFirst(RegExp(r'/$'), '');
  bool busy = false;
  String title = '';
  String? error, notice;
  List<String>? _selected;
  List<String> get selected => _selected ?? const [];
  CollectionCopyGrant? _lastGrant, _visibleGrant, _previewGrant;
  CollectionCopyPreview? preview;
  CollectionCopyPending? pending;
  CollectionCopyReceipt? receipt;
  PersonalCollection? destination;
  CollectionCopySelector? _notSubmitted;
  Timer? _expiry;
  Timer? _previewExpiry;
  bool get current {
    if (!_disposed &&
        !_retired &&
        (_owner == null ||
            !collectionCopyID(_owner) ||
            ownerID() != _owner ||
            !sourceCurrent() ||
            api.authorizationHeader() == null)) {
      _retain();
      _retired = true;
      ++_epoch;
      ++_draftEpoch;
      _expiry?.cancel();
      _previewExpiry?.cancel();
      title = '';
      _selected = null;
      preview = null;
      _previewGrant = null;
      _visibleGrant = null;
      _lastGrant = null;
      pending = null;
      receipt = null;
      destination = null;
      busy = false;
      _storageReady = false;
      error = '个人身份或来源已变化；旧许可与资料已移除。本人标题草稿保留在原账号范围。';
    }
    return !_disposed && !_retired;
  }

  CollectionCopyGrant? get currentGrant {
    if (!current) return null;
    final g = grant?.call();
    return g != null && g.current && sourceRef?.same(g.sourceRef) == true
        ? g
        : null;
  }

  bool get storageReady => _storageReady;
  bool get locallyPrepared =>
      pending?.stage == 'PREPARED' ||
      pending != null && _notSubmitted?.same(pending!.selector) == true;
  bool get canPreview =>
      current &&
      _storageReady &&
      !busy &&
      pending == null &&
      receipt == null &&
      currentGrant != null &&
      selected.isNotEmpty &&
      collectionCopySubsequence(selected, currentGrant!.eligiblePlaceIDs);
  bool _valid(int epoch, String? header) =>
      current &&
      _epoch == epoch &&
      api.authorizationHeader() == header &&
      ownerID() == _owner;
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _observe() {
    final previous = _epoch;
    if (!current) {
      _notify();
      return;
    }
    if (_header != api.authorizationHeader()) {
      _retain();
      _header = api.authorizationHeader();
      ++_epoch;
      _previewExpiry?.cancel();
      _storageReady = false;
      preview = null;
      _previewGrant = null;
      destination = null;
      busy = false;
      error = '登录已更新，本人标题与地点选择已保留。请检查本机原记录并重新预览，旧批准已失效。';
    }
    final changed = _observeGrant();
    if (previous != _epoch || changed) _notify();
  }

  bool _observeGrant() {
    final g = currentGrant;
    if (identical(g, _visibleGrant)) return false;
    _visibleGrant = g;
    preview = null;
    _previewGrant = null;
    _expiry?.cancel();
    _previewExpiry?.cancel();
    ++_draftEpoch;
    if (g != null) {
      if (_lastGrant != null && !g.sameChoiceSource(_lastGrant!)) {
        _selected = const [];
        error = '公开版本或复制许可已变化，请重新选择当前获准地点。';
      } else if (_selected == null) {
        _selected = g.eligiblePlaceIDs;
      } else if (!collectionCopySubsequence(selected, g.eligiblePlaceIDs)) {
        _selected = const [];
        error = '原地点选择不再获准，请重新选择。';
      }
      _lastGrant = g;
      _expiry = Timer(g.expiresAt.difference(DateTime.now().toUtc()), () {
        if (!_disposed && identical(_visibleGrant, g)) {
          _visibleGrant = null;
          preview = null;
          _previewGrant = null;
          ++_draftEpoch;
          _notify();
        }
      });
    }
    return true;
  }

  void _retain() {
    final ref = sourceRef;
    if (_owner == null ||
        ref == null ||
        !ref.valid ||
        utf8.encode(title).length > 16384)
      return;
    try {
      onRetainLocalDraft?.call(
        PersonalCollectionCopyLocalDraft(
          sourceKind: ref.kind,
          sourceID: ref.id,
          title: title,
          selectedPlaceIDs: _selected == null
              ? null
              : List.unmodifiable(selected),
        ),
      );
    } catch (_) {}
  }

  void setTitle(String value) {
    if (!current || busy) return;
    title = value;
    ++_draftEpoch;
    preview = null;
    _previewGrant = null;
    _previewExpiry?.cancel();
    _retain();
    _notify();
  }

  void selectPlace(String id, bool include) {
    final g = currentGrant;
    if (g == null ||
        busy ||
        pending != null ||
        !g.eligiblePlaceIDs.contains(id))
      return;
    final wanted = selected.toSet();
    if (include)
      wanted.add(id);
    else
      wanted.remove(id);
    _selected = List.unmodifiable(g.eligiblePlaceIDs.where(wanted.contains));
    ++_draftEpoch;
    preview = null;
    _previewGrant = null;
    _previewExpiry?.cancel();
    _retain();
    _notify();
  }

  void chooseNeutralTitle() {
    if (current && !busy)
      setTitle(sourceRef?.kind == 'theme_map' ? '我的主题地图副本' : '我的旅程副本');
  }

  Future<void> restore() async {
    _observe();
    if (!current || busy) return;
    final epoch = _epoch, header = _header, owner = _owner!;
    busy = true;
    error = null;
    _notify();
    try {
      final p = await pendingStore.read(origin, owner);
      if (!_valid(epoch, header)) return;
      pending = p;
      _storageReady = true;
      if (p != null)
        notice = p.stage == 'PREPARED'
            ? '上次只准备了本机记录，尚未发送。请检查后重新预览。'
            : '原副本操作结果尚未核实。只能读取原回执或明确停止，不会重发。';
    } catch (_) {
      if (_valid(epoch, header)) {
        _storageReady = false;
        error = '本机原操作记录暂不可读；当前不会创建副本。';
      }
    } finally {
      if (_valid(epoch, header)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<CollectionCopyPreview?> prepare() async {
    _observe();
    if (!canPreview) return null;
    final g = currentGrant!,
        epoch = _epoch,
        header = _header,
        draft = _draftEpoch;
    final input = CollectionCopyInput(
      sourceRef: g.sourceRef,
      operationID: const Uuid().v4(),
      publicationRevision: g.publicationRevision,
      grantVersion: g.grantVersion,
      orderedPlaceIDs: selected,
      title: title.trim(),
    );
    if (!input.valid) {
      error = '请填写1–160字节的本人标题，并选择获准的公开地点。';
      _notify();
      return null;
    }
    busy = true;
    error = null;
    preview = null;
    _notify();
    try {
      final p = await api.preview(
        input,
        g.expiresAt,
        () =>
            _valid(epoch, header) &&
            draft == _draftEpoch &&
            identical(currentGrant, g),
      );
      if (!_valid(epoch, header) ||
          draft != _draftEpoch ||
          !identical(currentGrant, g))
        return null;
      preview = p;
      _previewGrant = g;
      _previewExpiry?.cancel();
      _previewExpiry = Timer(
        p.expiresAt.difference(DateTime.now().toUtc()),
        () {
          if (!_disposed && identical(preview, p)) {
            preview = null;
            _previewGrant = null;
            _notify();
          }
        },
      );
      return p;
    } catch (e) {
      if (_valid(epoch, header)) error = _message(e);
      return null;
    } finally {
      if (_valid(epoch, header)) {
        busy = false;
        _notify();
      }
    }
  }

  bool previewCurrent(CollectionCopyPreview p) =>
      current &&
      !busy &&
      pending == null &&
      _storageReady &&
      api.authorizationHeader() == _header &&
      identical(preview, p) &&
      identical(currentGrant, _previewGrant) &&
      _previewGrant != null &&
      p.expiresAt.isAfter(DateTime.now().toUtc()) &&
      p.input.title == title.trim() &&
      listEquals(p.input.orderedPlaceIDs, selected);
  Future<CollectionCopyReceipt?> submit(CollectionCopyPreview p) async {
    _observe();
    if (!previewCurrent(p)) return null;
    final epoch = _epoch,
        header = _header,
        owner = _owner!,
        draft = _draftEpoch,
        g = _previewGrant;
    bool frame() => _valid(epoch, header);
    bool source() =>
        frame() &&
        draft == _draftEpoch &&
        identical(currentGrant, g) &&
        g != null &&
        g.current;
    final selector = p.input.selector;
    bool sent = false;
    busy = true;
    error = null;
    _notify();
    try {
      final prepared = CollectionCopyPending(selector, stage: 'PREPARED');
      await pendingStore.write(origin, owner, prepared);
      if (!source()) throw const FormatException('来源已变化，尚未发送副本操作');
      final read = await pendingStore.read(origin, owner);
      if (!source()) throw const FormatException('来源已变化，尚未发送副本操作');
      if (read == null || !read.same(prepared))
        throw const FormatException('本机准备记录未核对完成');
      pending = prepared;
      final unknown = CollectionCopyPending(selector);
      await pendingStore.write(origin, owner, unknown);
      if (!source()) throw const FormatException('来源已变化，尚未发送副本操作');
      final back = await pendingStore.read(origin, owner);
      if (!source()) throw const FormatException('来源已变化，尚未发送副本操作');
      if (back == null || !back.same(unknown))
        throw const FormatException('本机提交记录未核对完成');
      pending = unknown;
      if (!p.expiresAt.isAfter(DateTime.now().toUtc()))
        throw const FormatException('确认已到期，尚未发送副本操作');
      sent = true;
      // Original outcome is factual; the source lease gates the invocation,
      // while the same concrete receiver Session gates accepting its receipt.
      final result = await api.apply(p, frame);
      if (!frame()) return null;
      await _accept(result, unknown, epoch, header, owner);
      return frame() ? result : null;
    } catch (e) {
      if (frame()) {
        preview = null;
        _previewGrant = null;
        if (!sent) {
          _notSubmitted = selector;
          try {
            final saved = await pendingStore.read(origin, owner);
            if (frame()) {
              pending = saved;
              if (saved == null) _storageReady = false;
            }
          } catch (_) {
            if (frame()) _storageReady = false;
          }
          if (frame()) error = '副本操作尚未发送；本人标题草稿已保留。请检查本机准备记录后重新预览。';
        } else {
          pending = CollectionCopyPending(selector);
          _notSubmitted = null;
          error = '副本结果尚未核实；请核实原操作或明确停止，不会重新发送。';
        }
      }
      return null;
    } finally {
      if (frame()) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> repairPrepared() async {
    _observe();
    if (!current || busy) return;
    final epoch = _epoch, header = _header, owner = _owner!;
    busy = true;
    _notify();
    try {
      final p = await pendingStore.read(origin, owner);
      if (!_valid(epoch, header)) return;
      if (p != null &&
          (p.stage == 'PREPARED' || _notSubmitted?.same(p.selector) == true)) {
        await pendingStore.remove(origin, owner, p);
        if (!_valid(epoch, header)) return;
        pending = null;
        _notSubmitted = null;
        _storageReady = true;
        notice = '本机准备记录已清理，操作尚未发送。请重新预览。';
        error = null;
      } else {
        pending = p;
        _notSubmitted = null;
        _storageReady = true;
        error = null;
        notice = p == null ? null : '发现本人原副本操作尚未核实，请读取原回执。';
      }
    } catch (_) {
      if (_valid(epoch, header)) error = '本机记录尚未核对完成；不会发送副本操作。';
    } finally {
      if (_valid(epoch, header)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<CollectionCopyReceipt?> recover({bool stop = false}) async {
    _observe();
    if (!current || busy || pending == null || locallyPrepared) return null;
    final p = pending!, epoch = _epoch, header = _header, owner = _owner!;
    busy = true;
    error = null;
    _notify();
    try {
      final result = await api.operation(
        p.selector,
        () => _valid(epoch, header),
        stop: stop,
      );
      if (!_valid(epoch, header)) return null;
      await _accept(result, p, epoch, header, owner);
      return _valid(epoch, header) ? result : null;
    } catch (_) {
      if (_valid(epoch, header)) error = '原副本操作仍未核实。当前不可读不代表未执行；不会重发。';
      return null;
    } finally {
      if (_valid(epoch, header)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _accept(
    CollectionCopyReceipt result,
    CollectionCopyPending original,
    int epoch,
    String? header,
    String owner,
  ) async {
    receipt = result;
    preview = null;
    _previewGrant = null;
    _notSubmitted = null;
    destination = null;
    notice = result.status == 'STOPPED'
        ? '原副本操作已停止，未创建新副本。'
        : '原私人副本已创建；正在读取本人当前记录。';
    try {
      await pendingStore.remove(origin, owner, original);
      if (!_valid(epoch, header)) return;
      pending = null;
      _storageReady = true;
    } catch (_) {
      if (_valid(epoch, header)) {
        error = '原副本结果已确证，本机记录清理尚未完成；可再次读取同一原回执。';
      }
    }
    if (!_valid(epoch, header) || result.destinationRef == null) return;
    try {
      final saved = await api.destination(
        result,
        owner,
        () => _valid(epoch, header),
      );
      if (!_valid(epoch, header)) return;
      destination = saved;
      notice = '原私人副本已创建，当前记录已读取。';
    } catch (_) {
      if (_valid(epoch, header)) {
        destination = null;
        notice = '原私人副本已创建，但当前记录暂不可读；不会再次创建。';
      }
    }
  }

  Future<void> loadDestination() async {
    _observe();
    if (!current || busy || receipt?.destinationRef == null) return;
    final r = receipt!, epoch = _epoch, header = _header, owner = _owner!;
    busy = true;
    _notify();
    try {
      final v = await api.destination(r, owner, () => _valid(epoch, header));
      if (_valid(epoch, header)) {
        destination = v;
        notice = '当前本人副本已读取。';
      }
    } catch (_) {
      if (_valid(epoch, header)) {
        destination = null;
        notice = '原副本已确证创建，当前记录暂不可读；不会再次创建。';
      }
    } finally {
      if (_valid(epoch, header)) {
        busy = false;
        _notify();
      }
    }
  }

  String _message(Object e) => e is CollectionCopyFailure
      ? e.message
      : e is FormatException
      ? e.message
      : '无法读取副本预览，请稍后重试。';
  @override
  void dispose() {
    if (_disposed) return;
    _retain();
    _disposed = true;
    ++_epoch;
    _expiry?.cancel();
    _previewExpiry?.cancel();
    identityChanges?.removeListener(_observe);
    super.dispose();
  }
}
