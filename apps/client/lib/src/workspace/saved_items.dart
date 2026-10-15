import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'entity_action_contract.dart';
import 'entity_share_pending_store.dart'
    show chatEntityUUID, newEntityShareOperationID;
import 'now_ui_tokens.dart';
import 'community_avatar.dart';
import 'entity_cover.dart';
import 'place_photo.dart';
import 'legacy_place_reference_page.dart';

class SavedItem {
  const SavedItem({
    required this.id,
    required this.kind,
    required this.targetId,
    required this.title,
    required this.summary,
    required this.cityId,
    required this.available,
    this.cityName = '',
    this.note = '',
    this.noteRevision = 0,
    this.communityAvatar,
    this.cover,
    this.placePhoto,
    this.latitude,
    this.longitude,
    this.sourceExpiresAt,
    this.legacyReference,
  });

  final String id;
  final String kind;
  final String targetId;
  final String title;
  final String summary;
  final String cityId;
  final bool available;
  final String cityName;
  final String note;
  final int noteRevision;
  final CommunityAvatarMetadata? communityAvatar;
  final EntityCoverMetadata? cover;
  final EntityCoverMetadata? placePhoto;
  final double? latitude, longitude;
  final DateTime? sourceExpiresAt;
  final LegacyPlaceReference? legacyReference;
  bool get readable => available || legacyReference?.current == true;
  bool get mapCurrent =>
      kind == 'place' &&
      available &&
      latitude != null &&
      longitude != null &&
      sourceExpiresAt != null &&
      DateTime.now().toUtc().isBefore(sourceExpiresAt!);

  SavedItem withNote(String value, int revision) => SavedItem(
    id: id,
    kind: kind,
    targetId: targetId,
    title: title,
    summary: summary,
    cityId: cityId,
    cityName: cityName,
    available: available,
    note: value,
    noteRevision: revision,
    communityAvatar: communityAvatar,
    cover: cover,
    placePhoto: placePhoto,
    latitude: latitude,
    longitude: longitude,
    sourceExpiresAt: sourceExpiresAt,
    legacyReference: legacyReference,
  );

  factory SavedItem.fromJson(Map<String, dynamic> json) {
    final legacy = json['legacyReference'] == null
        ? null
        : LegacyPlaceReference.decode(json['legacyReference']);
    if (legacy != null &&
        (json['kind'] != 'place' ||
            json['available'] != false ||
            legacy.kind != 'saved_place' ||
            legacy.recordID != json['id'] ||
            legacy.placeID != json['targetId'] ||
            json['latitude'] != null ||
            json['longitude'] != null ||
            json['sourceExpiresAt'] != null ||
            json['placePhoto'] != null ||
            json['cover'] != null ||
            json['communityAvatar'] != null)) {
      throw const FormatException('本人地点引用不符');
    }
    final lat = (json['latitude'] as num?)?.toDouble();
    final lng = (json['longitude'] as num?)?.toDouble();
    final expires = json['sourceExpiresAt'] == null
        ? null
        : DateTime.parse(json['sourceExpiresAt'] as String).toUtc();
    if ((lat == null) != (lng == null) ||
        (lat == null) != (expires == null) ||
        (lat != null &&
            (json['kind'] != 'place' ||
                json['available'] != true ||
                !lat.isFinite ||
                !lng!.isFinite ||
                lat.abs() > 90 ||
                lng.abs() > 180))) {
      throw const FormatException('收藏地图来源无效');
    }
    return SavedItem(
      id: json['id'] as String,
      kind: json['kind'] as String,
      targetId: json['targetId'] as String,
      title: json['title'] as String? ?? '',
      summary: json['summary'] as String? ?? '',
      cityId: json['cityId'] as String? ?? '',
      cityName: json['cityName'] as String? ?? '',
      available: json['available'] as bool? ?? false,
      note: json['note'] as String? ?? '',
      noteRevision: json['noteRevision'] as int? ?? 0,
      communityAvatar: _communityMedia(json),
      cover: _coverMedia(json),
      placePhoto: _placePhotoMedia(json),
      latitude: lat,
      longitude: lng,
      sourceExpiresAt: expires,
      legacyReference: legacy,
    );
  }

  static CommunityAvatarMetadata? _communityMedia(Map<String, dynamic> json) {
    if (json['communityAvatar'] == null) return null;
    if (json['kind'] != 'group' || json['available'] != true) {
      throw const FormatException('收藏头像与可见社群来源不符');
    }
    return CommunityAvatarMetadata.fromJson(
      json['communityAvatar'],
      json['targetId'] as String,
    );
  }

  static EntityCoverMetadata? _coverMedia(Map<String, dynamic> json) {
    if (json['cover'] == null) return null;
    if (json['kind'] != 'activity' || json['available'] != true)
      throw const FormatException('收藏封面与当前活动不符');
    return EntityCoverMetadata.fromJson(
      json['cover'],
      EntityCoverRef('activity', json['targetId'] as String),
    );
  }

  static EntityCoverMetadata? _placePhotoMedia(Map<String, dynamic> json) {
    if (json['placePhoto'] == null) return null;
    if (json['kind'] != 'place' || json['available'] != true) {
      throw const FormatException('收藏照片与当前可见地点不符');
    }
    return EntityCoverMetadata.fromJson(
      json['placePhoto'],
      PlacePhotoRef(json['targetId'] as String),
    );
  }
}

/// Recovery selectors contain no credentials or private note body.
class SavedNotePending {
  const SavedNotePending({
    required this.savedID,
    required this.operationID,
    required this.expectedRevision,
    this.stage = 'UNKNOWN',
  });
  final String savedID, operationID;
  final int expectedRevision;
  final String stage;
  Map<String, Object> toJson() => {
    'savedId': savedID,
    'operationId': operationID,
    'expectedRevision': expectedRevision,
    'stage': stage,
  };
  bool same(SavedNotePending other) =>
      savedID == other.savedID &&
      operationID == other.operationID &&
      expectedRevision == other.expectedRevision &&
      stage == other.stage;
  bool sameOperation(SavedNotePending other) =>
      savedID == other.savedID &&
      operationID == other.operationID &&
      expectedRevision == other.expectedRevision;
  SavedNotePending submitting() => SavedNotePending(
    savedID: savedID,
    operationID: operationID,
    expectedRevision: expectedRevision,
  );
  factory SavedNotePending.decode(Object? raw) {
    if (raw is! Map<String, dynamic> ||
        (raw.length != 3 && raw.length != 4) ||
        raw['savedId'] is! String ||
        !chatEntityUUID.hasMatch(raw['savedId']) ||
        raw['operationId'] is! String ||
        !chatEntityUUID.hasMatch(raw['operationId']) ||
        raw['expectedRevision'] is! int ||
        raw['expectedRevision'] < 0 ||
        (raw.length == 4 &&
            !const {'PREPARED', 'UNKNOWN'}.contains(raw['stage']))) {
      throw const FormatException('收藏备注恢复记录无效');
    }
    return SavedNotePending(
      savedID: raw['savedId'],
      operationID: raw['operationId'],
      expectedRevision: raw['expectedRevision'],
      stage: raw['stage'] as String? ?? 'UNKNOWN',
    );
  }
}

abstract class SavedNotePendingStore {
  Future<SavedNotePending?> read(String origin, String owner, String savedID);
  Future<void> write(String origin, String owner, SavedNotePending pending);
  Future<void> remove(String origin, String owner, SavedNotePending pending);
}

class SecureSavedNotePendingStore implements SavedNotePendingStore {
  SecureSavedNotePendingStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  static final Map<String, Future<void>> _tails = {};
  Future<T> _serial<T>(String key, Future<T> Function() work) {
    final previous = _tails[key], released = Completer<void>();
    final tail = released.future;
    _tails[key] = tail;
    final result = Completer<T>();
    void release() {
      if (identical(_tails[key], tail)) _tails.remove(key);
      released.complete();
    }

    Future<void> run() async {
      try {
        if (previous != null) await previous;
        final value = await work();
        release();
        result.complete(value);
      } catch (error, stack) {
        release();
        result.completeError(error, stack);
      }
    }

    unawaited(run());
    return result.future;
  }

  String _key(String origin, String owner, String saved) =>
      'birdtie.saved-note.pending.v1.${sha256.convert(utf8.encode('$origin\n$owner\n$saved'))}';
  @override
  Future<SavedNotePending?> read(String origin, String owner, String savedID) =>
      _serial(
        _key(origin, owner, savedID),
        () => _read(origin, owner, savedID),
      );
  Future<SavedNotePending?> _read(
    String origin,
    String owner,
    String savedID,
  ) async {
    final raw = await _storage.read(key: _key(origin, owner, savedID));
    if (raw == null) return null;
    final p = SavedNotePending.decode(jsonDecode(raw));
    if (p.savedID != savedID) throw const FormatException('收藏备注恢复对象不符');
    return p;
  }

  @override
  Future<void> write(String origin, String owner, SavedNotePending pending) =>
      _serial(_key(origin, owner, pending.savedID), () async {
        final existing = await _read(origin, owner, pending.savedID);
        if (existing != null &&
            (!existing.sameOperation(pending) ||
                existing.stage == 'UNKNOWN' && pending.stage == 'PREPARED'))
          throw const FormatException('上次保存记录尚未核实');
        await _storage.write(
          key: _key(origin, owner, pending.savedID),
          value: jsonEncode(pending.toJson()),
        );
      });
  @override
  Future<void> remove(String origin, String owner, SavedNotePending pending) =>
      _serial(_key(origin, owner, pending.savedID), () async {
        final current = await _read(origin, owner, pending.savedID);
        if (current != null && !current.same(pending)) {
          throw const FormatException('收藏备注恢复版本已变化');
        }
        await _storage.delete(key: _key(origin, owner, pending.savedID));
      });
}

class SavedNoteState {
  SavedNoteState(this.savedID);
  final String savedID;
  String draft = '', currentNote = '';
  int revision = 0;
  bool loaded = false, loading = false, saving = false, journalFailed = false;
  bool notSubmitted = false;
  SavedNotePending? notSubmittedSelector;
  String? error;
  SavedNotePending? pending;
  bool get uncertain => pending?.stage == 'UNKNOWN' && !notSubmitted;
  bool get editable =>
      loaded && !loading && !saving && pending == null && !journalFailed;
}

class SavedController extends ChangeNotifier {
  SavedController({
    required this.authorizationHeader,
    this.ownerID,
    SavedNotePendingStore? notePendingStore,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _notePendingStore = notePendingStore ?? SecureSavedNotePendingStore(),
       _apiBaseUrl = apiBaseUrl ?? apiBase;

  static const apiBase = BirdtieEnvironment.apiBaseUrl;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final SavedNotePendingStore _notePendingStore;
  final http.Client _client;
  final bool _ownsClient;
  bool _disposed = false;
  int _generation = 0;
  http.Client get followClient => _client;
  String get followApiBaseUrl => _apiBaseUrl;
  final String _apiBaseUrl;
  bool get configured => _apiBaseUrl.isNotEmpty;
  List<SavedItem> items = const [];
  bool loading = false;
  bool failed = false;
  bool pageChanged = false;
  bool _paged = false;
  String? nextCursor;
  String query = '';
  String? queryKind, queryCity;
  (String, String?, String?)? _resultQuery;
  bool get resultsForCurrentQuery =>
      _resultQuery == (query, queryKind, queryCity);
  bool get hasMore => nextCursor != null;
  int get identityGeneration => _generation;
  bool get noteCapable =>
      configured && authorizationHeader() != null && ownerID?.call() != null;
  final Map<String, SavedNoteState> _notes = {};
  final Map<String, SavedItem> _membership = {};
  SavedNoteState? noteState(String savedID) => _notes[savedID];
  final Set<String> busy = {};
  int _serial = 0;

  String _key(String kind, String targetId) => '$kind:$targetId';
  bool contains(String kind, String targetId) =>
      _membership.containsKey(_key(kind, targetId));
  bool isBusy(String kind, String targetId) =>
      busy.contains(_key(kind, targetId));

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers({bool json = false}) {
    final token = authorizationHeader();
    final headers = <String, String>{};
    if (json) headers['Content-Type'] = 'application/json';
    if (token != null) headers['Authorization'] = token;
    return headers;
  }

  void clear({bool notify = true}) {
    if (_disposed) return;
    ++_generation;
    ++_serial;
    items = const [];
    loading = false;
    failed = false;
    pageChanged = false;
    nextCursor = null;
    query = '';
    queryKind = queryCity = null;
    _resultQuery = null;
    _notes.clear();
    _membership.clear();
    busy.clear();
    if (notify) notifyListeners();
  }

  Future<void> load({bool more = false, bool? paged}) async {
    if (_disposed) return;
    if (paged != null) _paged = paged;
    if (more && (loading || nextCursor == null)) return;
    final capturedToken = authorizationHeader();
    final capturedOwner = ownerID?.call(), generation = _generation;
    final pageMode = _paged;
    final cursor = more ? nextCursor : null;
    final requestQuery = (query, queryKind, queryCity);
    if (!configured || authorizationHeader() == null) {
      clear();
      return;
    }
    final serial = ++_serial;
    loading = true;
    failed = false;
    pageChanged = false;
    notifyListeners();
    try {
      final endpoint = _endpoint(
        pageMode ? '/v1/me/saved/page' : '/v1/me/saved',
      );
      final parameters = <String, String>{
        if (cursor != null) 'cursor': cursor,
        if (pageMode && query.isNotEmpty) 'q': query,
        if (pageMode && queryKind != null) 'kind': queryKind!,
        if (pageMode && queryCity != null) 'cityId': queryCity!,
      };
      final response = await _client
          .get(
            parameters.isEmpty
                ? endpoint
                : endpoint.replace(queryParameters: parameters),
            headers: _headers(),
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode == 409 &&
          pageMode &&
          cursor != null &&
          _binding(generation, capturedToken, capturedOwner) &&
          serial == _serial) {
        nextCursor = null;
        pageChanged = true;
      }
      if (response.statusCode != 200) throw StateError('Saved unavailable');
      final data =
          (jsonDecode(utf8.decode(response.bodyBytes))
              as Map<String, dynamic>)['data'];
      final rows = pageMode
          ? (data as Map<String, dynamic>)['items'] as List<dynamic>
          : data as List<dynamic>;
      final following = pageMode
          ? (data as Map<String, dynamic>)['nextCursor'] as String?
          : null;
      if (following != null &&
          (!RegExp(r'^[A-Za-z0-9_-]{1,1024}$').hasMatch(following) ||
              following == cursor))
        throw const FormatException('收藏分页游标无效');
      if (_disposed ||
          generation != _generation ||
          ownerID?.call() != capturedOwner ||
          serial != _serial ||
          authorizationHeader() != capturedToken) {
        return;
      }
      final incoming = [
        for (final row in rows) SavedItem.fromJson(row as Map<String, dynamic>),
      ];
      for (final item in incoming) {
        if (!pageMode || _notes.containsKey(item.id) || capturedOwner == null)
          continue;
        final state = SavedNoteState(item.id);
        try {
          state.pending = await _notePendingStore.read(
            _apiBaseUrl,
            capturedOwner,
            item.id,
          );
          if (state.pending?.stage == 'PREPARED') {
            state.notSubmitted = true;
            state.error = '上次仅保留了本机准备记录，尚未提交。请检查本机记录。';
          }
        } catch (_) {
          state.journalFailed = true;
          state.error = '无法读取上次保存记录，请重试；暂不提交新的备注。';
        }
        if (!_binding(generation, capturedToken, capturedOwner) ||
            serial != _serial)
          return;
        _notes[item.id] = state;
      }
      final byID = <String, SavedItem>{
        if (more)
          for (final i in items) i.id: i,
      };
      for (final i in incoming) {
        byID[i.id] = i;
        _membership[_key(i.kind, i.targetId)] = i;
      }
      items = List.unmodifiable(byID.values);
      nextCursor = following;
      _resultQuery = requestQuery;
    } catch (_) {
      if (_disposed ||
          serial != _serial ||
          generation != _generation ||
          ownerID?.call() != capturedOwner ||
          authorizationHeader() != capturedToken) {
        return;
      }
      failed = true;
    } finally {
      if (!_disposed &&
          serial == _serial &&
          generation == _generation &&
          ownerID?.call() == capturedOwner &&
          authorizationHeader() == capturedToken) {
        loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> search(String value, {String? kind, String? cityID}) async {
    if (_disposed) return;
    final normalized = value.trim();
    if (utf8.encode(normalized).length > 240 ||
        (kind != null &&
            !const ['place', 'activity', 'group'].contains(kind)) ||
        (cityID != null &&
            (cityID.isEmpty ||
                utf8.encode(cityID).length > 80 ||
                cityID.contains(RegExp(r'[\x00-\x1f]'))))) {
      throw const FormatException('搜索条件无效');
    }
    query = normalized;
    queryKind = kind;
    queryCity = cityID;
    nextCursor = null;
    await load(paged: true);
  }

  Future<void> toggle(
    String kind,
    String targetId, {
    String entrySource = 'direct',
    EntityActionDescriptor? approved,
  }) async {
    if (_disposed) return;
    if (!configured || authorizationHeader() == null) {
      throw StateError('Sign in to save Birdtie items');
    }
    final key = _key(kind, targetId);
    final currentToken = authorizationHeader();
    final generation = _generation;
    if (approved != null) {
      final expectedType = kind == 'group' ? 'community' : kind;
      if (approved.target != EntityActionRef(expectedType, targetId) ||
          !const {'SAVE', 'UNSAVE'}.contains(approved.operation)) {
        throw StateError('收藏状态已变化，请重新检查。');
      }
    }
    if (busy.contains(key)) return;
    busy.add(key);
    notifyListeners();
    try {
      final remove =
          approved?.operation == 'UNSAVE' ||
          approved == null && contains(kind, targetId);
      if (!remove) {
        final response = await _client
            .post(
              _endpoint('/v1/me/saved'),
              headers: {
                ..._headers(json: true),
                'X-Birdtie-Entry-Source': entrySource,
                if (approved != null) ...approved.conditionHeaders,
              },
              body: jsonEncode({'kind': kind, 'targetId': targetId}),
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 201) throw StateError('Could not save item');
      } else {
        var existing = _membership[key];
        if (existing == null) {
          final lookup = await _client
              .get(
                _endpoint(
                  '/v1/me/saved/target/${Uri.encodeComponent(kind)}/${Uri.encodeComponent(targetId)}',
                ),
                headers: _headers(),
              )
              .timeout(const Duration(seconds: 12));
          if (_disposed ||
              generation != _generation ||
              authorizationHeader() != currentToken)
            return;
          if (lookup.statusCode != 200) throw StateError('无法核实当前收藏，请重试。');
          final raw =
              (jsonDecode(utf8.decode(lookup.bodyBytes))
                  as Map<String, dynamic>)['data'];
          if (raw == null) throw StateError('收藏状态已变化，请重新读取。');
          existing = SavedItem.fromJson(raw as Map<String, dynamic>);
          if (existing.kind != kind || existing.targetId != targetId)
            throw const FormatException('收藏对象不符');
        }
        final response = await _client
            .delete(
              _endpoint('/v1/me/saved/${Uri.encodeComponent(existing.id)}'),
              headers: {
                ..._headers(),
                if (approved != null) ...approved.conditionHeaders,
              },
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 204) {
          throw StateError('Could not remove saved item');
        }
        if (!_disposed &&
            generation == _generation &&
            authorizationHeader() == currentToken)
          _membership.remove(key);
      }
      if (_disposed ||
          generation != _generation ||
          authorizationHeader() != currentToken) {
        return;
      }
      await load();
    } finally {
      if (!_disposed && generation == _generation) {
        busy.remove(key);
        notifyListeners();
      }
    }
  }

  bool _binding(int generation, String? token, String? owner) =>
      !_disposed &&
      generation == _generation &&
      token == authorizationHeader() &&
      owner == ownerID?.call();
  bool noteBinding(int generation, String? token, String? owner) =>
      _binding(generation, token, owner);
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  bool _hasItem(SavedItem item) => items.any(
    (i) =>
        i.id == item.id && i.kind == item.kind && i.targetId == item.targetId,
  );
  Map<String, dynamic> _noteData(http.Response response) =>
      (jsonDecode(utf8.decode(response.bodyBytes))
              as Map<String, dynamic>)['data']
          as Map<String, dynamic>;
  void _adoptNote(SavedNoteState state, Map<String, dynamic> data) {
    if (data['savedId'] != state.savedID ||
        data['note'] is! String ||
        data['revision'] is! int ||
        data['revision'] < 0 ||
        utf8.encode(data['note']).length > 2000)
      throw const FormatException('收藏备注内容无效');
    state.currentNote = data['note'];
    state.revision = data['revision'];
    state.loaded = true;
    items = List.unmodifiable(
      items.map(
        (i) => i.id == state.savedID
            ? i.withNote(state.currentNote, state.revision)
            : i,
      ),
    );
    for (final i in items.where((i) => i.id == state.savedID)) {
      _membership[_key(i.kind, i.targetId)] = i;
    }
  }

  void _receipt(Map<String, dynamic> data, SavedNotePending pending) {
    if (data['savedId'] != pending.savedID ||
        data['operationId'] != pending.operationID ||
        data['status'] != 'SUCCEEDED' ||
        data['revision'] != pending.expectedRevision + 1)
      throw const FormatException('上次备注保存回执不符');
  }

  Future<void> readNote(SavedItem item, {bool refresh = false}) async {
    if (!noteCapable || !_hasItem(item)) return;
    final state = _notes.putIfAbsent(item.id, () => SavedNoteState(item.id));
    if (state.loading ||
        state.saving ||
        state.uncertain ||
        (state.loaded && !refresh))
      return;
    final generation = _generation,
        token = authorizationHeader(),
        owner = ownerID!.call()!;
    final retainedDraft = state.loaded ? state.draft : null;
    state.loading = true;
    state.error = null;
    _notify();
    try {
      final pending = await _notePendingStore.read(_apiBaseUrl, owner, item.id);
      if (!_binding(generation, token, owner)) return;
      state.journalFailed = false;
      final canClean =
          pending != null &&
          (pending.stage == 'PREPARED' ||
              state.notSubmitted &&
                  state.notSubmittedSelector?.same(pending) == true);
      state.pending = pending;
      if (pending == null) {
        state.notSubmitted = false;
        state.notSubmittedSelector = null;
      }
      if (canClean) {
        await _notePendingStore.remove(_apiBaseUrl, owner, pending);
        if (!_binding(generation, token, owner)) return;
        state.pending = null;
        state.notSubmitted = false;
        state.notSubmittedSelector = null;
      } else if (pending != null) {
        state.notSubmitted = false;
        state.notSubmittedSelector = null;
        state.error = '上次保存结果待核实，请先核实后再编辑。';
        return;
      }
      final response = await _client
          .get(
            _endpoint('/v1/me/saved/${Uri.encodeComponent(item.id)}/note'),
            headers: _headers(),
          )
          .timeout(const Duration(seconds: 12));
      if (!_binding(generation, token, owner)) return;
      if (response.statusCode != 200) throw const FormatException();
      _adoptNote(state, _noteData(response));
      state.draft = retainedDraft ?? state.currentNote;
    } catch (_) {
      if (_binding(generation, token, owner)) {
        state.error = '无法读取本人备注或上次保存记录，请重试。';
      }
    } finally {
      if (_binding(generation, token, owner)) {
        state.loading = false;
        _notify();
      }
    }
  }

  void setNoteDraft(String savedID, String value) {
    final state = _notes[savedID];
    if (state == null || !state.editable) return;
    state.draft = value;
    state.error = utf8.encode(value).length > 2000
        ? '备注超过 2000 字节，请缩短后保存。'
        : null;
    _notify();
  }

  void discardNoteDraft(String savedID) {
    final state = _notes[savedID];
    if (state == null || !state.editable) return;
    state.draft = state.currentNote;
    state.error = null;
    _notify();
  }

  Future<bool> saveNote(SavedItem item) async {
    final state = _notes[item.id];
    if (!noteCapable || !_hasItem(item) || state == null || !state.editable)
      return false;
    if (utf8.encode(state.draft).length > 2000) {
      state.error = '备注超过 2000 字节，请缩短后保存。';
      _notify();
      return false;
    }
    final generation = _generation,
        token = authorizationHeader(),
        owner = ownerID!.call()!;
    var pending = SavedNotePending(
      savedID: item.id,
      operationID: newEntityShareOperationID(),
      expectedRevision: state.revision,
      stage: 'PREPARED',
    );
    final draft = state.draft;
    state.saving = true;
    state.error = null;
    _notify();
    bool submitted = false;
    try {
      final existing = await _notePendingStore.read(
        _apiBaseUrl,
        owner,
        item.id,
      );
      if (!_binding(generation, token, owner)) return false;
      if (existing != null) {
        state.pending = existing;
        state.notSubmitted = existing.stage == 'PREPARED';
        state.notSubmittedSelector = state.notSubmitted ? existing : null;
        state.error = state.notSubmitted
            ? '本机准备记录尚未核对，请检查本机记录。'
            : '上次保存结果待核实，请先核实后再编辑。';
        return false;
      }
      await _notePendingStore.write(_apiBaseUrl, owner, pending);
      if (!_binding(generation, token, owner)) return false;
      state.pending = pending;
      final stored = await _notePendingStore.read(_apiBaseUrl, owner, item.id);
      if (!_binding(generation, token, owner)) return false;
      if (stored == null || !stored.same(pending))
        throw const FormatException('备注保存记录未持久化');
      pending = pending.submitting();
      await _notePendingStore.write(_apiBaseUrl, owner, pending);
      if (!_binding(generation, token, owner)) return false;
      state.pending = pending;
      final ready = await _notePendingStore.read(_apiBaseUrl, owner, item.id);
      if (!_binding(generation, token, owner)) return false;
      if (ready == null || !ready.same(pending))
        throw const FormatException('备注提交标记未持久化');
      state.notSubmitted = false;
      state.notSubmittedSelector = null;
      submitted = true;
      final response = await _client
          .put(
            _endpoint('/v1/me/saved/${Uri.encodeComponent(item.id)}/note'),
            headers: _headers(json: true),
            body: jsonEncode({
              'note': draft,
              'expectedRevision': pending.expectedRevision,
              'operationId': pending.operationID,
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (!_binding(generation, token, owner)) return false;
      if (response.statusCode >= 400 && response.statusCode < 500) {
        await _notePendingStore.remove(_apiBaseUrl, owner, pending);
        if (!_binding(generation, token, owner)) return false;
        state.pending = null;
        state.error = response.statusCode == 409
            ? '备注版本已变化，草稿已保留。请读取当前备注后检查修改。'
            : '保存未被接受，草稿已保留，请检查后重试。';
        return false;
      }
      if (response.statusCode != 200) throw const FormatException();
      _receipt(_noteData(response), pending);
      return await _finishNote(state, pending, generation, token, owner);
    } catch (_) {
      if (_binding(generation, token, owner)) {
        if (!submitted) {
          state.notSubmitted = true;
          state.notSubmittedSelector = pending;
          state.journalFailed = true;
        }
        state.error = submitted
            ? '保存结果尚未确认，草稿已保留。请核实上次保存。'
            : '本次尚未提交，草稿已保留。请检查本机记录后重新准备保存。';
      }
      return false;
    } finally {
      if (_binding(generation, token, owner)) {
        state.saving = false;
        _notify();
      }
    }
  }

  Future<bool> _finishNote(
    SavedNoteState state,
    SavedNotePending pending,
    int generation,
    String? token,
    String owner,
  ) async {
    final response = await _client
        .get(
          _endpoint('/v1/me/saved/${Uri.encodeComponent(state.savedID)}/note'),
          headers: _headers(),
        )
        .timeout(const Duration(seconds: 12));
    if (!_binding(generation, token, owner)) return false;
    if (response.statusCode != 200) throw const FormatException();
    final data = _noteData(response);
    if (data['revision'] is! int ||
        data['revision'] < pending.expectedRevision + 1)
      throw const FormatException('备注版本早于原保存回执');
    _adoptNote(state, data);
    await _notePendingStore.remove(_apiBaseUrl, owner, pending);
    if (!_binding(generation, token, owner)) return false;
    state.pending = null;
    state.notSubmitted = false;
    state.notSubmittedSelector = null;
    state.journalFailed = false;
    state.draft = state.currentNote;
    state.error = null;
    return true;
  }

  Future<bool> recoverNote(SavedItem item) async {
    final state = _notes[item.id], pending = _notes[item.id]?.pending;
    if (!noteCapable ||
        !_hasItem(item) ||
        state == null ||
        pending == null ||
        !state.uncertain ||
        state.loading ||
        state.saving)
      return false;
    final generation = _generation,
        token = authorizationHeader(),
        owner = ownerID!.call()!;
    state.saving = true;
    state.error = null;
    _notify();
    try {
      final response = await _client
          .get(
            _endpoint(
              '/v1/me/saved/${Uri.encodeComponent(item.id)}/note/operations/${pending.operationID}',
            ),
            headers: _headers(),
          )
          .timeout(const Duration(seconds: 12));
      if (!_binding(generation, token, owner)) return false;
      if (response.statusCode != 200) {
        state.error = '尚未取得上次保存回执，请稍后再核实。';
        return false;
      }
      _receipt(_noteData(response), pending);
      return await _finishNote(state, pending, generation, token, owner);
    } catch (_) {
      if (_binding(generation, token, owner))
        state.error = '无法核实上次保存，原操作编号仍保留，请重试。';
      return false;
    } finally {
      if (_binding(generation, token, owner)) {
        state.saving = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    ++_generation;
    ++_serial;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}

class SavedPage extends StatefulWidget {
  const SavedPage({
    super.key,
    required this.saved,
    this.onOpenActivity,
    this.onOpenItem,
    this.onAddToCollection,
    this.onViewMap,
    this.cityNames = const {},
  });
  final SavedController saved;
  final ValueChanged<String>? onOpenActivity;
  final ValueChanged<SavedItem>? onOpenItem;
  final ValueChanged<List<SavedItem>>? onAddToCollection, onViewMap;
  final Map<String, String> cityNames;

  @override
  State<SavedPage> createState() => _SavedPageState();
}

class _SavedPageState extends State<SavedPage> {
  final _search = TextEditingController();
  String? _kind, _city;
  bool _localOnly = false;
  Timer? _searchTimer;
  Timer? _privateExpiry;
  final _selected = <String>{};
  int _loadEpoch = 0;
  void _scheduleInitialLoad() {
    final saved = widget.saved, epoch = ++_loadEpoch;
    final generation = saved.identityGeneration;
    final token = saved.authorizationHeader(), owner = saved.ownerID?.call();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted &&
          epoch == _loadEpoch &&
          identical(saved, widget.saved) &&
          saved.noteBinding(generation, token, owner)) {
        unawaited(saved.load(paged: true));
      }
    });
  }

  @override
  void initState() {
    super.initState();
    widget.saved.addListener(_schedulePrivateExpiry);
    _schedulePrivateExpiry();
    _scheduleInitialLoad();
  }

  void _schedulePrivateExpiry() {
    _privateExpiry?.cancel();
    final deadlines = [
      for (final item in widget.saved.items)
        if (item.legacyReference?.current == true)
          item.legacyReference!.validUntil,
    ]..sort();
    if (deadlines.isNotEmpty) {
      _privateExpiry = Timer(
        deadlines.first.difference(DateTime.now().toUtc()) +
            const Duration(milliseconds: 1),
        () {
          if (mounted) {
            setState(() {});
            _schedulePrivateExpiry();
          }
        },
      );
    }
  }

  @override
  void didUpdateWidget(SavedPage old) {
    super.didUpdateWidget(old);
    if (old.saved != widget.saved) {
      old.saved.removeListener(_schedulePrivateExpiry);
      widget.saved.addListener(_schedulePrivateExpiry);
      _schedulePrivateExpiry();
      _search.clear();
      _kind = _city = null;
      _localOnly = false;
      _selected.clear();
      _searchTimer?.cancel();
      _scheduleInitialLoad();
    }
  }

  @override
  void dispose() {
    ++_loadEpoch;
    widget.saved.removeListener(_schedulePrivateExpiry);
    _privateExpiry?.cancel();
    _searchTimer?.cancel();
    _search.dispose();
    super.dispose();
  }

  Future<void> _remove(SavedItem item) async {
    final saved = widget.saved, generation = widget.saved.identityGeneration;
    final token = saved.authorizationHeader(), owner = saved.ownerID?.call();
    try {
      await saved.toggle(item.kind, item.targetId);
    } catch (_) {
      if (mounted &&
          saved == widget.saved &&
          saved.noteBinding(generation, token, owner)) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('取消收藏失败，请重试。')));
      }
    }
  }

  void _open(SavedItem item) {
    if (!item.readable) return;
    if (widget.onOpenItem != null)
      widget.onOpenItem!(item);
    else if (item.kind == 'activity')
      widget.onOpenActivity?.call(item.targetId);
  }

  Future<void> _note(SavedItem item) async {
    final saved = widget.saved;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (_) => SavedNoteSheet(saved: saved, item: item),
    );
  }

  Widget _leading(SavedItem item) {
    final saved = widget.saved, metadata = item.communityAvatar;
    if (item.kind == 'place' &&
        item.available &&
        item.placePhoto != null &&
        item.placePhoto!.ref == PlacePhotoRef(item.targetId)) {
      final generation = saved.identityGeneration,
          token = saved.authorizationHeader(),
          owner = saved.ownerID?.call();
      return EntityCover(
        key: ObjectKey(item),
        metadata: item.placePhoto,
        api: EntityCoverAPI(
          client: saved.followClient,
          baseUrl: saved.followApiBaseUrl,
          authorizationHeader: saved.authorizationHeader,
        ),
        identityChanges: saved,
        width: 80,
        height: 45,
        showAttribution: false,
        label: '${item.title}的地点照片',
        current: () =>
            mounted &&
            identical(saved, widget.saved) &&
            saved.noteBinding(generation, token, owner) &&
            saved.items.any(
              (row) =>
                  identical(row, item) && row.available && row.kind == 'place',
            ),
      );
    }
    if (item.kind == 'activity' &&
        item.available &&
        item.cover != null &&
        item.cover!.ref == EntityCoverRef('activity', item.targetId)) {
      final generation = saved.identityGeneration,
          token = saved.authorizationHeader(),
          owner = saved.ownerID?.call();
      return EntityCover(
        key: ObjectKey(item),
        metadata: item.cover,
        api: EntityCoverAPI(
          client: saved.followClient,
          baseUrl: saved.followApiBaseUrl,
          authorizationHeader: saved.authorizationHeader,
        ),
        identityChanges: saved,
        width: 80,
        height: 45,
        showAttribution: false,
        label: '${item.title}的活动封面',
        current: () =>
            mounted &&
            identical(saved, widget.saved) &&
            saved.noteBinding(generation, token, owner) &&
            saved.items.any(
              (row) =>
                  identical(row, item) &&
                  row.available &&
                  row.kind == 'activity',
            ),
      );
    }
    if (item.kind == 'group' && item.available && metadata != null) {
      final generation = saved.identityGeneration;
      final token = saved.authorizationHeader(), owner = saved.ownerID?.call();
      return CommunityAvatar(
        key: ValueKey((
          'saved-community-avatar',
          saved,
          generation,
          item.id,
          item.targetId,
        )),
        metadata: metadata,
        fallbackLabel: item.title,
        api: CommunityAvatarAPI(
          client: saved.followClient,
          baseUrl: saved.followApiBaseUrl,
          authorizationHeader: saved.authorizationHeader,
        ),
        identityChanges: saved,
        current: () =>
            mounted &&
            identical(saved, widget.saved) &&
            saved.noteBinding(generation, token, owner) &&
            saved.items.any(
              (current) =>
                  current.id == item.id &&
                  current.kind == 'group' &&
                  current.targetId == item.targetId &&
                  current.available &&
                  current.communityAvatar?.same(metadata) == true,
            ),
      );
    }
    return Icon(switch (item.kind) {
      'place' => Icons.place_outlined,
      'activity' => Icons.event_outlined,
      _ => Icons.group_outlined,
    });
  }

  void _searchChanged() {
    _searchTimer?.cancel();
    _selected.clear();
    setState(() {});
    if (!_localOnly)
      _searchTimer = Timer(const Duration(milliseconds: 300), _sendSearch);
  }

  Future<void> _sendSearch() async {
    _searchTimer?.cancel();
    if (!mounted || _localOnly) return;
    try {
      await widget.saved.search(_search.text, kind: _kind, cityID: _city);
    } on FormatException {
      if (mounted)
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('搜索文字过长，请缩短后重试。')));
    }
  }

  void _clearFilters() {
    setState(() {
      _search.clear();
      _kind = _city = null;
      _selected.clear();
    });
    if (!_localOnly) unawaited(_sendSearch());
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.saved,
    builder: (context, _) {
      final saved = widget.saved;
      if (!saved.configured) {
        return const Center(child: Text('收藏暂不可用，请稍后重试。'));
      }
      if (saved.authorizationHeader() == null) {
        return const Center(child: Text('登录后即可查看收藏内容。'));
      }
      final tokens = NowUiTokens.of(context);
      final query = _search.text.trim().toLowerCase();
      final matches = saved.items
          .where(
            (i) =>
                (_kind == null || _kind == i.kind) &&
                (_city == null || _city == i.cityId) &&
                (!_localOnly ||
                    query.isEmpty ||
                    '${i.title} ${i.summary} ${i.note}'.toLowerCase().contains(
                      query,
                    )),
          )
          .toList();
      final cities = saved.items
          .map((i) => i.cityId)
          .where((i) => i.isNotEmpty)
          .toSet();
      if (_city != null) cities.add(_city!);
      final expiredReferences = saved.items.any(
        (item) =>
            item.legacyReference != null && !item.legacyReference!.current,
      );
      String cityLabel(String id) {
        for (final item in saved.items) {
          if (item.cityId == id && item.cityName.isNotEmpty)
            return item.cityName;
        }
        return widget.cityNames[id] ?? '其他城市';
      }

      return RefreshIndicator(
        onRefresh: () => saved.load(paged: true),
        child: ListView(
          padding: const EdgeInsets.all(16),
          physics: const AlwaysScrollableScrollPhysics(),
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          children: [
            Text(
              _localOnly
                  ? '在已加载的 ${saved.items.length} 项中筛选'
                  : saved.loading
                  ? (saved.items.isEmpty ? '正在读取收藏' : '保留上次结果，正在读取新的查询')
                  : saved.failed
                  ? (saved.items.isEmpty
                        ? '尚未取得收藏结果'
                        : '保留上次已加载的 ${saved.items.length} 项')
                  : saved.resultsForCurrentQuery
                  ? (saved.hasMore
                        ? '${saved.query.isEmpty && saved.queryKind == null && saved.queryCity == null ? '已加载' : '搜索结果已加载'} ${saved.items.length} 项'
                        : '${saved.items.length} 项')
                  : '查询尚未完成',
              style: tokens.metadata,
            ),
            const SizedBox(height: 16),
            TextField(
              key: const Key('saved-search'),
              controller: _search,
              decoration: InputDecoration(
                labelText: _localOnly ? '筛选已加载收藏（含本人备注）' : '搜索全部收藏（标题、简介、城市）',
                prefixIcon: Icon(Icons.search),
              ),
              onChanged: (_) => _searchChanged(),
              onSubmitted: (_) => unawaited(_sendSearch()),
            ),
            CheckboxListTile(
              contentPadding: EdgeInsets.zero,
              value: _localOnly,
              title: const Text('仅筛选已加载内容'),
              onChanged: (v) {
                _searchTimer?.cancel();
                setState(() {
                  _localOnly = v == true;
                  _selected.clear();
                });
                if (!_localOnly) unawaited(_sendSearch());
              },
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final kind in [null, 'place', 'activity', 'group'])
                  ChoiceChip(
                    label: Text(kind == null ? '全部类型' : _savedKind(kind)),
                    selected: _kind == kind,
                    onSelected: (_) {
                      _kind = kind;
                      _searchChanged();
                    },
                  ),
              ],
            ),
            if (cities.isNotEmpty) ...[
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                key: ValueKey('saved-city-${_city ?? 'all'}'),
                initialValue: _city ?? '',
                isExpanded: true,
                decoration: const InputDecoration(labelText: '城市'),
                items: [
                  const DropdownMenuItem(value: '', child: Text('全部城市')),
                  for (final id in cities)
                    DropdownMenuItem(value: id, child: Text(cityLabel(id))),
                ],
                onChanged: (v) {
                  _city = v == '' ? null : v;
                  _searchChanged();
                },
              ),
            ],
            if (_kind != null || _city != null || query.isNotEmpty)
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton(
                  onPressed: _clearFilters,
                  child: const Text('清除筛选'),
                ),
              ),
            const SizedBox(height: 16),
            if (widget.onViewMap != null || widget.onAddToCollection != null)
              Wrap(
                spacing: 8,
                children: [
                  if (widget.onViewMap != null)
                    OutlinedButton.icon(
                      onPressed:
                          saved.loading ||
                              saved.failed ||
                              (!_localOnly && !saved.resultsForCurrentQuery) ||
                              !matches.any((i) => i.mapCurrent)
                          ? null
                          : () => widget.onViewMap!(
                              List.unmodifiable(
                                matches.where((i) => i.mapCurrent),
                              ),
                            ),
                      icon: const Icon(Icons.map_outlined),
                      label: const Text('已加载地点上图与比较'),
                    ),
                  if (widget.onAddToCollection != null)
                    OutlinedButton.icon(
                      onPressed:
                          saved.loading ||
                              saved.failed ||
                              (!_localOnly && !saved.resultsForCurrentQuery) ||
                              !matches.any(
                                (i) =>
                                    _selected.contains(i.id) &&
                                    i.available &&
                                    i.kind == 'place',
                              )
                          ? null
                          : () => widget.onAddToCollection!(
                              List.unmodifiable(
                                matches.where(
                                  (i) =>
                                      _selected.contains(i.id) &&
                                      i.available &&
                                      i.kind == 'place',
                                ),
                              ),
                            ),
                      icon: const Icon(Icons.add_location_alt_outlined),
                      label: const Text('加入已有旅程或主题地图'),
                    ),
                ],
              ),
            if (saved.loading) const LinearProgressIndicator(),
            if (expiredReferences) ...[
              const Text('这批地点资料已到期，重新读取后可查看当前资料。'),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: saved.loading
                      ? null
                      : () => saved.load(paged: true),
                  icon: const Icon(Icons.refresh),
                  label: const Text('重新读取收藏'),
                ),
              ),
            ],
            if (saved.failed) ...[
              Text(saved.pageChanged ? '收藏已更新，请刷新。' : '加载失败，请重试。'),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton(
                  onPressed: saved.loading
                      ? null
                      : () => saved.load(paged: true),
                  child: Text(saved.pageChanged ? '重新读取收藏' : '重试读取'),
                ),
              ),
            ],
            if (matches.isEmpty && !saved.loading && !saved.failed)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 24),
                child: Text(saved.items.isEmpty ? '暂无收藏' : '没有匹配的收藏'),
              ),
            if (saved.items.isEmpty &&
                !saved.loading &&
                !saved.failed &&
                Navigator.canPop(context))
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton(
                  onPressed: () => Navigator.maybePop(context),
                  child: const Text('去探索'),
                ),
              ),
            for (final item in matches)
              ListTile(
                contentPadding: EdgeInsets.zero,
                onTap:
                    item.readable &&
                        (widget.onOpenItem != null ||
                            item.kind == 'activity' &&
                                widget.onOpenActivity != null)
                    ? () => _open(item)
                    : null,
                leading: _leading(item),
                title: Text(
                  item.readable
                      ? item.title
                      : item.legacyReference != null
                      ? '地点资料已到期'
                      : '内容暂不可用',
                  style: tokens.placeTitle,
                ),
                subtitle: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (widget.onAddToCollection != null &&
                        item.kind == 'place')
                      Checkbox(
                        semanticLabel: item.readable
                            ? '选择${item.title}加入集合'
                            : '当前地点资料不可选择',
                        value: _selected.contains(item.id),
                        onChanged: item.available && !saved.loading
                            ? (v) => setState(() {
                                if (v == true)
                                  _selected.add(item.id);
                                else
                                  _selected.remove(item.id);
                              })
                            : null,
                      ),
                    Text(
                      item.readable
                          ? [
                              if (item.cityId.isNotEmpty &&
                                  (item.cityName.isNotEmpty ||
                                      widget.cityNames.containsKey(
                                        item.cityId,
                                      )))
                                item.cityName.isNotEmpty
                                    ? item.cityName
                                    : widget.cityNames[item.cityId]!,
                              _savedKind(item.kind),
                              if (item.legacyReference != null) '仅本人可见',
                              if (item.summary.isNotEmpty) item.summary,
                            ].join(' · ')
                          : item.legacyReference != null
                          ? '重新读取收藏后可查看当前资料。'
                          : '该内容已不可见，你可以保留备注或取消收藏。',
                      style: tokens.listSubtitle,
                    ),
                    if (item.note.isNotEmpty)
                      Text('本人备注：${item.note}', style: tokens.body),
                    if (saved.noteState(item.id)?.uncertain == true)
                      const Text('备注保存待核实'),
                  ],
                ),
                trailing: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    IconButton(
                      tooltip: '本人备注',
                      onPressed:
                          saved.noteCapable &&
                              !saved.isBusy(item.kind, item.targetId)
                          ? () => _note(item)
                          : null,
                      icon: const Icon(Icons.edit_note_outlined),
                    ),
                    IconButton(
                      tooltip: '取消收藏',
                      onPressed: saved.isBusy(item.kind, item.targetId)
                          ? null
                          : () => _remove(item),
                      icon: const Icon(Icons.bookmark_remove_outlined),
                    ),
                  ],
                ),
              ),
            if (saved.hasMore) ...[
              const SizedBox(height: 12),
              OutlinedButton(
                onPressed: saved.loading
                    ? null
                    : () => saved.load(more: true, paged: true),
                child: const Text('继续读取收藏'),
              ),
            ],
          ],
        ),
      );
    },
  );
}

class SavedNoteSheet extends StatefulWidget {
  const SavedNoteSheet({super.key, required this.saved, required this.item});
  final SavedController saved;
  final SavedItem item;
  @override
  State<SavedNoteSheet> createState() => _SavedNoteSheetState();
}

class _SavedNoteSheetState extends State<SavedNoteSheet> {
  final _text = TextEditingController();
  late final int _generation;
  late final String? _token, _owner;
  bool get _current => widget.saved.noteBinding(_generation, _token, _owner);
  SavedNoteState? get _state => widget.saved.noteState(widget.item.id);
  SavedItem? get _currentItem {
    if (!_current) return null;
    for (final item in widget.saved.items) {
      if (item.id == widget.item.id &&
          item.kind == widget.item.kind &&
          item.targetId == widget.item.targetId)
        return item;
    }
    return null;
  }

  @override
  void initState() {
    super.initState();
    _generation = widget.saved.identityGeneration;
    _token = widget.saved.authorizationHeader();
    _owner = widget.saved.ownerID?.call();
    _text.text = _state?.draft ?? '';
    widget.saved.addListener(_changed);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && _current) unawaited(widget.saved.readNote(widget.item));
    });
  }

  void _changed() {
    if (!mounted) return;
    final value = _current ? _state?.draft ?? '' : '';
    if (_text.text != value)
      _text.value = TextEditingValue(
        text: value,
        selection: TextSelection.collapsed(offset: value.length),
      );
    setState(() {});
  }

  @override
  void dispose() {
    widget.saved.removeListener(_changed);
    _text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.of(context), state = _current ? _state : null;
    return Padding(
      padding: EdgeInsets.fromLTRB(
        16,
        0,
        16,
        MediaQuery.viewInsetsOf(context).bottom + 16,
      ),
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text('本人备注', style: tokens.sheetTitle),
            const SizedBox(height: 8),
            Text(
              _current
                  ? (_currentItem?.available == true
                        ? _currentItem!.title
                        : '内容暂不可用')
                  : '当前身份已变化',
              style: tokens.placeTitle,
            ),
            const SizedBox(height: 8),
            Text('仅自己可见，不会随内容公开或分享。', style: tokens.metadata),
            const SizedBox(height: 16),
            if (!_current)
              const Text('请返回当前收藏重新打开；原身份的内容已隐藏。')
            else ...[
              if (state?.loading == true || state?.saving == true)
                const LinearProgressIndicator(),
              if (state?.loaded == true && state!.draft != state.currentNote)
                Text(
                  '当前已保存备注：${state.currentNote.isEmpty ? '尚无备注' : state.currentNote}',
                  style: tokens.listSubtitle,
                ),
              TextField(
                key: const Key('saved-note-draft'),
                controller: _text,
                enabled: state?.editable == true,
                minLines: 3,
                maxLines: 8,
                decoration: const InputDecoration(
                  labelText: '备注',
                  helperText: '最多 2000 字节，可清空备注',
                ),
                onChanged: (v) => widget.saved.setNoteDraft(widget.item.id, v),
              ),
              if (state?.error != null)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Semantics(
                    liveRegion: true,
                    child: Text(state!.error!, style: tokens.body),
                  ),
                ),
              const SizedBox(height: 16),
              Wrap(
                spacing: 12,
                runSpacing: 8,
                children: [
                  if (state?.uncertain == true)
                    FilledButton(
                      onPressed: state?.saving == true
                          ? null
                          : () => unawaited(
                              widget.saved.recoverNote(widget.item),
                            ),
                      child: const Text('核实上次保存'),
                    )
                  else ...[
                    FilledButton(
                      onPressed:
                          state?.editable == true &&
                              utf8.encode(_text.text).length <= 2000
                          ? () async {
                              if (await widget.saved.saveNote(widget.item) &&
                                  mounted &&
                                  _current)
                                Navigator.pop(context);
                            }
                          : null,
                      child: const Text('保存备注'),
                    ),
                    if (state?.error != null || state?.loaded != true)
                      TextButton(
                        onPressed:
                            state?.loading == true || state?.saving == true
                            ? null
                            : () => unawaited(
                                widget.saved.readNote(
                                  widget.item,
                                  refresh: true,
                                ),
                              ),
                        child: Text(
                          state?.notSubmitted == true ||
                                  state?.journalFailed == true
                              ? '检查本机记录'
                              : '读取当前备注',
                        ),
                      ),
                  ],
                  TextButton(
                    onPressed: state?.saving == true
                        ? null
                        : () {
                            widget.saved.discardNoteDraft(widget.item.id);
                            Navigator.pop(context);
                          },
                    child: Text(state?.uncertain == true ? '返回收藏' : '取消'),
                  ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}

String _savedKind(String kind) => switch (kind) {
  'place' => '地点',
  'activity' => '活动',
  'group' => '社群',
  _ => kind,
};
