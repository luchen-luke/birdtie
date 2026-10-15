import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import 'collection_control.dart';
import 'collection_control_pending_store.dart';
import '../workspace/legacy_place_reference_page.dart';

final _collectionUUID = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);

/// Only the receiver's own local title and stable public selector survive a
/// Session change. No author text, source lease, grant or approval is retained.
class PersonalCollectionCopyLocalDraft {
  const PersonalCollectionCopyLocalDraft({
    required this.sourceKind,
    required this.sourceID,
    required this.title,
    this.selectedPlaceIDs,
  });
  final String sourceKind, sourceID, title;
  final List<String>? selectedPlaceIDs;
  String get key => 'birdtie:collection/$sourceKind/$sourceID';
  bool get valid =>
      const {'journey', 'theme_map'}.contains(sourceKind) &&
      _collectionUUID.hasMatch(sourceID) &&
      utf8.encode(title).length <= 16384 &&
      (selectedPlaceIDs == null ||
          selectedPlaceIDs!.length <= 50 &&
              selectedPlaceIDs!.every(_collectionUUID.hasMatch) &&
              selectedPlaceIDs!.toSet().length == selectedPlaceIDs!.length);
}

class CollectionPlace {
  const CollectionPlace({required this.id, required this.name});
  final String id, name;
}

class PersonalCollectionEntry {
  const PersonalCollectionEntry({
    required this.placeID,
    this.note = '',
    this.locationVisibility = 'hidden',
    this.placeName = '',
    this.available = true,
    this.latitude,
    this.longitude,
    this.sourceExpiresAt,
    this.selectorOnly = false,
    this.legacyReference,
  });
  final String placeID, note, locationVisibility, placeName;
  final bool available;
  final double? latitude, longitude;
  final DateTime? sourceExpiresAt;
  final bool selectorOnly;
  final LegacyPlaceReference? legacyReference;
  bool get readable =>
      sourceCurrent ||
      !selectorOnly &&
          !available &&
          locationVisibility == 'hidden' &&
          latitude == null &&
          longitude == null &&
          legacyReference?.current == true;
  bool get sourceCurrent =>
      legacyReference == null &&
      available &&
      !selectorOnly &&
      (sourceExpiresAt == null ||
          DateTime.now().toUtc().isBefore(sourceExpiresAt!));
  Map<String, dynamic> toInput() => {
    'placeId': placeID,
    'note': note,
    'locationVisibility': locationVisibility,
  };
  PersonalCollectionEntry copyWith({
    String? note,
    String? locationVisibility,
  }) => PersonalCollectionEntry(
    placeID: placeID,
    note: note ?? this.note,
    locationVisibility: locationVisibility ?? this.locationVisibility,
    placeName: placeName,
    available: available,
    latitude: latitude,
    longitude: longitude,
    sourceExpiresAt: sourceExpiresAt,
    selectorOnly: selectorOnly,
    legacyReference: legacyReference,
  );
  factory PersonalCollectionEntry.fromJson(Map<String, dynamic> p) {
    final id = p['placeId'] as String;
    final precision = p['locationVisibility'] as String;
    final lat = (p['latitude'] as num?)?.toDouble(),
        lng = (p['longitude'] as num?)?.toDouble();
    final expires = p['sourceExpiresAt'] == null
        ? null
        : DateTime.parse(p['sourceExpiresAt'] as String).toUtc();
    final legacy = p['legacyReference'] == null
        ? null
        : LegacyPlaceReference.decode(p['legacyReference']);
    if (!_collectionUUID.hasMatch(id) ||
        !const ['hidden', 'city', 'place'].contains(precision) ||
        (lat == null) != (lng == null) ||
        (lat != null &&
            (!lat.isFinite ||
                lat < -90 ||
                lat > 90 ||
                !lng!.isFinite ||
                lng < -180 ||
                lng > 180)) ||
        (p['available'] == true && expires == null) ||
        legacy != null &&
            (legacy.kind != 'theme_map' ||
                legacy.placeID != id ||
                p['available'] != false ||
                precision != 'hidden' ||
                lat != null ||
                lng != null ||
                expires != null))
      throw const FormatException('invalid collection entry');
    return PersonalCollectionEntry(
      placeID: id,
      note: p['note'] as String,
      locationVisibility: precision,
      placeName: p['placeName'] as String? ?? '',
      available: p['available'] as bool,
      latitude: lat,
      longitude: lng,
      sourceExpiresAt: expires,
      legacyReference: legacy,
    );
  }
}

/// Local user text and stable selectors. This carries no Session, source
/// receipt, coordinate, display label, lifetime or approval for a new context.
class PersonalCollectionRetainedLocalDraft {
  PersonalCollectionRetainedLocalDraft._({
    required this.ownerID,
    required this.kind,
    required this.cityID,
    required this.apiBaseUrl,
    required this.title,
    required this.summary,
    required List<PersonalCollectionEntry> entries,
    this.editingID,
    this.editingRevision,
    Map<String, dynamic>? unknownPayload,
    this.pendingID,
    this.pendingRevision,
    required this.pendingDelete,
    required this.unknown,
    this.publicationPending,
    Map<String, PersonalCollectionCopyLocalDraft> copyLocalDrafts = const {},
  }) : entries = List.unmodifiable(
         entries.map(
           (e) => PersonalCollectionEntry(
             placeID: e.placeID,
             note: e.note,
             locationVisibility: e.locationVisibility,
             selectorOnly: true,
           ),
         ),
       ),
       copyLocalDrafts = Map.unmodifiable(
         copyLocalDrafts.map(
           (key, value) => MapEntry(
             key,
             PersonalCollectionCopyLocalDraft(
               sourceKind: value.sourceKind,
               sourceID: value.sourceID,
               title: value.title,
               selectedPlaceIDs: value.selectedPlaceIDs == null
                   ? null
                   : List.unmodifiable(value.selectedPlaceIDs!),
             ),
           ),
         ),
       ),
       unknownPayload = unknownPayload == null
           ? null
           : _freezeCollectionInput(unknownPayload);
  final String ownerID, kind, cityID, apiBaseUrl, title, summary;
  final List<PersonalCollectionEntry> entries;
  final String? editingID, pendingID;
  final int? editingRevision, pendingRevision;
  final Map<String, dynamic>? unknownPayload;
  final bool pendingDelete, unknown;
  final Map<String, dynamic>? publicationPending;
  final Map<String, PersonalCollectionCopyLocalDraft> copyLocalDrafts;
}

Map<String, dynamic> _freezeCollectionInput(Map<String, dynamic> value) {
  final j = jsonDecode(jsonEncode(value)) as Map<String, dynamic>;
  if (j['entries'] is List)
    j['entries'] = List.unmodifiable(
      (j['entries'] as List).map(
        (e) => Map<String, dynamic>.unmodifiable(e as Map<String, dynamic>),
      ),
    );
  return Map.unmodifiable(j);
}

class PersonalCollection {
  const PersonalCollection({
    required this.id,
    required this.ownerID,
    required this.kind,
    required this.cityID,
    required this.title,
    required this.summary,
    required this.entries,
    required this.revision,
    required this.status,
    this.visibility = 'private',
    this.privatePlaceCopyRestricted = false,
  });
  final String id, ownerID, kind, cityID, title, summary, status, visibility;
  final int revision;
  final bool privatePlaceCopyRestricted;
  final List<PersonalCollectionEntry> entries;
  bool get hasPrivateLegacyReferences =>
      entries.any((e) => e.legacyReference != null);
  factory PersonalCollection.fromJson(Map<String, dynamic> p) {
    final entries = (p['entries'] as List)
        .map((e) => PersonalCollectionEntry.fromJson(e as Map<String, dynamic>))
        .toList(growable: false);
    final id = p['id'] as String, owner = p['ownerAccountId'] as String;
    final kind = p['kind'] as String, status = p['status'] as String;
    final revision = p['revision'] as int;
    if (!_collectionUUID.hasMatch(id) ||
        !_collectionUUID.hasMatch(owner) ||
        !const ['journey', 'theme_map'].contains(kind) ||
        !const ['private', 'public'].contains(p['visibility']) ||
        !const ['draft', 'published', 'withdrawn'].contains(status) ||
        (status == 'published' && p['visibility'] != 'public') ||
        (status == 'draft' && p['visibility'] != 'private') ||
        revision < 1 ||
        entries.length > 50 ||
        entries.map((e) => e.placeID).toSet().length != entries.length)
      throw const FormatException('invalid collection receipt');
    if (entries.any(
      (e) =>
          e.legacyReference != null &&
          (kind != 'theme_map' ||
              status != 'draft' ||
              p['visibility'] != 'private' ||
              e.legacyReference!.recordID != id),
    )) {
      throw const FormatException(
        'private collection reference source changed',
      );
    }
    if (p.containsKey('privatePlaceCopyRestricted') &&
        p['privatePlaceCopyRestricted'] is! bool)
      throw const FormatException('invalid private copy capability');
    return PersonalCollection(
      id: id,
      ownerID: owner,
      kind: kind,
      cityID: p['cityId'] as String,
      title: p['title'] as String,
      summary: p['summary'] as String,
      entries: List.unmodifiable(entries),
      revision: revision,
      status: status,
      visibility: p['visibility'] as String,
      privatePlaceCopyRestricted:
          p['privatePlaceCopyRestricted'] as bool? ?? false,
    );
  }
}

/// A personal creation controller. Media, publication and model permission are
/// never inherited from a saved private collection. Source retirement is final.
class PersonalCollectionController extends ChangeNotifier {
  PersonalCollectionController({
    required this.kind,
    required this.cityID,
    required this.authorizationHeader,
    required this.ownerID,
    this.sourceCurrent,
    this.identityChanges,
    http.Client? client,
    String? apiBaseUrl,
    this.initialText = '',
    PersonalCollectionRetainedLocalDraft? retainedLocalDraft,
    this.onRetainLocalDraft,
    this.publicationRecoveryReader,
    CollectionControlPendingStore? controlPendingStore,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       controlPendingStore =
           controlPendingStore ?? const SecureCollectionControlPendingStore(),
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl {
    if (!const ['journey', 'theme_map'].contains(kind))
      throw ArgumentError.value(kind);
    controlAPI = CollectionControlAPI(
      client: _client,
      apiBaseUrl: _base,
      authorizationHeader: authorizationHeader,
    );
    _token = authorizationHeader();
    _owner = ownerID();
    title = initialText.length <= 80 ? initialText : '';
    summary = initialText.length > 80 ? initialText : '';
    if (retainedLocalDraft != null) _restoreLocalDraft(retainedLocalDraft);
    identityChanges?.addListener(_onBoundary);
  }
  final String kind, cityID, initialText, _base;
  final String? Function() authorizationHeader, ownerID;
  final bool Function()? sourceCurrent;
  final Listenable? identityChanges;
  final ValueChanged<PersonalCollectionRetainedLocalDraft>? onRetainLocalDraft;
  final Future<Map<String, dynamic>?> Function(String id)?
  publicationRecoveryReader;
  final http.Client _client;
  final bool _ownsClient;
  final CollectionControlPendingStore controlPendingStore;
  late final CollectionControlAPI controlAPI;
  List<CollectionControlPending> controlPending = const [];
  CollectionControlPreview? controlPreview;
  CollectionControlReceipt? lastControlReceipt;
  bool controlReady = false, controlReadFailed = false, _controlReading = false;
  bool controlCurrentUnread = false;
  Future<void>? _controlReadFuture;
  int _controlReads = 0,
      _draftVersion = 0,
      _previewEpoch = -1,
      _previewDraft = -1;
  CollectionControlPending? _notSubmittedControl;
  CollectionControlPending? get controlNotSubmitted => _notSubmittedControl;
  bool get controlBlocked =>
      !controlReady ||
      controlReadFailed ||
      controlPending.isNotEmpty ||
      controlCurrentUnread ||
      _notSubmittedControl != null;
  bool get controlBusy => saving || _controlReading;
  bool controlPreviewCurrent(CollectionControlPreview p) =>
      current &&
      identical(controlPreview, p) &&
      p.live &&
      _previewEpoch == _epoch &&
      _previewDraft == _draftVersion &&
      editing?.id == p.input.collectionID &&
      editing?.revision == p.input.expectedRevision &&
      !controlBlocked &&
      !saving;
  void _controlEdited() {
    ++_draftVersion;
    controlPreview = null;
  }

  String get apiBaseUrl => _base;
  http.Client get transport => _client;
  String? _token, _owner;
  bool _closed = false, _retired = false;
  int _epoch = 0, _reads = 0, _searches = 0;
  Timer? _sourceExpiry;
  bool loading = false, saving = false, searching = false, uncertain = false;
  String? error, searchError, nextCursor;
  List<PersonalCollection> items = const [];
  List<CollectionPlace> places = const [];
  PersonalCollection? editing;
  bool get hasPrivateLegacyReferences =>
      editing?.hasPrivateLegacyReferences == true ||
      draftEntries.any((e) => e.legacyReference != null);
  String title = '', summary = '';
  List<PersonalCollectionEntry> draftEntries = const [];
  Map<String, dynamic>? _pending;
  String? _pendingID;
  int? _pendingRevision;
  bool _pendingDelete = false;
  bool _restoredUnknown = false, _editingNeedsRevalidation = false;
  bool _publicationRecoveryFailed = false;
  Map<String, dynamic>? _publicationPending;
  PersonalCollectionRetainedLocalDraft? _lastRetainedDraft;
  Map<String, PersonalCollectionCopyLocalDraft> _copyLocalDrafts = const {};
  Map<String, PersonalCollectionCopyLocalDraft> get copyLocalDrafts =>
      _copyLocalDrafts;
  void retainCopyLocalDraft(PersonalCollectionCopyLocalDraft draft) {
    if (!current || !draft.valid) return;
    _copyLocalDrafts = Map.unmodifiable({
      ..._copyLocalDrafts,
      draft.key: PersonalCollectionCopyLocalDraft(
        sourceKind: draft.sourceKind,
        sourceID: draft.sourceID,
        title: draft.title,
        selectedPlaceIDs: draft.selectedPlaceIDs == null
            ? null
            : List.unmodifiable(draft.selectedPlaceIDs!),
      ),
    });
    _retainLocalDraft();
  }

  void clearCopyLocalDraft(String key) {
    if (!current || !_copyLocalDrafts.containsKey(key)) return;
    final original = retainedLocalDraft;
    _copyLocalDrafts = Map.unmodifiable({..._copyLocalDrafts}..remove(key));
    if (retainedLocalDraft != null) {
      _retainLocalDraft();
      return;
    }
    // An empty retained snapshot explicitly replaces a host's old copy title.
    // Otherwise clearing the last local copy draft would leave it in the cache.
    if (original != null) {
      final cleared = PersonalCollectionRetainedLocalDraft._(
        ownerID: original.ownerID,
        kind: original.kind,
        cityID: original.cityID,
        apiBaseUrl: original.apiBaseUrl,
        title: original.title,
        summary: original.summary,
        entries: original.entries,
        editingID: original.editingID,
        editingRevision: original.editingRevision,
        unknownPayload: original.unknownPayload,
        pendingID: original.pendingID,
        pendingRevision: original.pendingRevision,
        pendingDelete: original.pendingDelete,
        unknown: original.unknown,
        publicationPending: original.publicationPending,
        copyLocalDrafts: _copyLocalDrafts,
      );
      _lastRetainedDraft = cleared;
      try {
        onRetainLocalDraft?.call(cleared);
      } catch (_) {}
    }
  }

  Map<String, dynamic>? get publicationPending => _publicationPending;
  void holdPublication(Map<String, dynamic>? selectors) {
    if (!current) return;
    _publicationPending = selectors == null
        ? null
        : Map.unmodifiable(selectors);
    _retainLocalDraft();
    _notify();
  }

  void clearUnsentPublication(String operationID) {
    if (_publicationPending?['operationId'] == operationID) {
      _publicationPending = null;
      _retainLocalDraft();
      _notify();
    } else if (_lastRetainedDraft?.publicationPending?['operationId'] ==
        operationID) {
      final d = _lastRetainedDraft!;
      final corrected = PersonalCollectionRetainedLocalDraft._(
        ownerID: d.ownerID,
        kind: d.kind,
        cityID: d.cityID,
        apiBaseUrl: d.apiBaseUrl,
        title: d.title,
        summary: d.summary,
        entries: d.entries,
        editingID: d.editingID,
        editingRevision: d.editingRevision,
        unknownPayload: d.unknownPayload,
        pendingID: d.pendingID,
        pendingRevision: d.pendingRevision,
        pendingDelete: d.pendingDelete,
        unknown: d.unknown,
        copyLocalDrafts: d.copyLocalDrafts,
      );
      _lastRetainedDraft = corrected;
      try {
        onRetainLocalDraft?.call(corrected);
      } catch (_) {}
    }
  }

  void acceptPublicationReceipt(PersonalCollection value) {
    if (!current ||
        value.ownerID != _owner ||
        value.kind != kind ||
        value.cityID != cityID ||
        value.id != _publicationPending?['id'])
      return;
    _publicationPending = null;
    _adopt(value);
    _publicationRecoveryFailed = false;
    items = List.unmodifiable([value, ...items.where((e) => e.id != value.id)]);
    _notify();
  }

  void acceptPublicationNoEffect(PersonalCollection value, String operationID) {
    if (!current ||
        value.ownerID != _owner ||
        value.kind != kind ||
        value.cityID != cityID ||
        value.id != _publicationPending?['id'] ||
        operationID != _publicationPending?['operationId'])
      return;
    _publicationPending = null;
    // A no-effect receipt reads the server's current object; it does not
    // approve rebasing an older local draft onto that newer revision.
    editing ??= value;
    if (editing!.revision != value.revision) {
      error = '已保存版本已变化，保留本机草稿；请重新打开当前版本后处理修改。';
    }
    final currentEntries = {for (final e in value.entries) e.placeID: e};
    draftEntries = List.unmodifiable(
      draftEntries.map((draft) {
        final e = currentEntries[draft.placeID];
        return e == null
            ? draft
            : e.copyWith(
                note: draft.note,
                locationVisibility: draft.locationVisibility,
              );
      }),
    );
    items = List.unmodifiable([value, ...items.where((e) => e.id != value.id)]);
    _editingNeedsRevalidation = true;
    _publicationRecoveryFailed = false;
    // Keep the original local text, notes and selectors. A no-effect receipt
    // cannot silently replace them or grant permission for a new write.
    _retainLocalDraft();
    _scheduleSourceExpiry();
    _notify();
  }

  PersonalCollectionRetainedLocalDraft? get retainedLocalDraft {
    if (_owner == null ||
        !_collectionUUID.hasMatch(_owner!) ||
        (!hasDraft &&
            editing == null &&
            _pending == null &&
            !_pendingDelete &&
            _copyLocalDrafts.isEmpty))
      return null;
    return PersonalCollectionRetainedLocalDraft._(
      ownerID: _owner!,
      kind: kind,
      cityID: cityID,
      apiBaseUrl: _base,
      title: title,
      summary: summary,
      entries: draftEntries,
      editingID: editing?.id,
      editingRevision: editing?.revision,
      unknownPayload: (_pending != null && (uncertain || saving))
          ? _pending
          : null,
      pendingID: _pendingID,
      pendingRevision: _pendingRevision,
      pendingDelete: _pendingDelete,
      unknown:
          (uncertain && controlPending.isEmpty) ||
          saving && (_pending != null || _pendingDelete),
      publicationPending: _publicationPending,
      copyLocalDrafts: _copyLocalDrafts,
    );
  }

  void _retainLocalDraft() {
    final draft = retainedLocalDraft;
    if (draft == null) return;
    _lastRetainedDraft = draft;
    try {
      onRetainLocalDraft?.call(draft);
    } catch (_) {
      /* Retirement still fences every old approval even if host caching fails. */
    }
  }

  void _restoreLocalDraft(PersonalCollectionRetainedLocalDraft draft) {
    if (draft.ownerID != _owner ||
        draft.kind != kind ||
        draft.cityID != cityID ||
        draft.apiBaseUrl != _base) {
      error = '原草稿属于其他账号、城市或来源，当前不能恢复。';
      return;
    }
    title = draft.title;
    summary = draft.summary;
    draftEntries = draft.entries;
    _publicationPending = draft.publicationPending;
    if (draft.copyLocalDrafts.values.any((d) => !d.valid) ||
        draft.copyLocalDrafts.entries.any((e) => e.key != e.value.key)) {
      error = '本人的副本标题草稿来源不符，当前不能恢复。';
    } else {
      _copyLocalDrafts = draft.copyLocalDrafts;
    }
    if (draft.editingID != null && draft.editingRevision != null) {
      editing = PersonalCollection(
        id: draft.editingID!,
        ownerID: _owner!,
        kind: kind,
        cityID: cityID,
        title: '',
        summary: '',
        entries: const [],
        revision: draft.editingRevision!,
        status: 'draft',
      );
      _editingNeedsRevalidation = true;
    }
    if (draft.unknown) {
      _pending = draft.unknownPayload;
      _pendingID = draft.pendingID;
      _pendingRevision = draft.pendingRevision;
      _pendingDelete = draft.pendingDelete;
      uncertain = true;
      _restoredUnknown = true;
      error = '原操作结果尚未确认；请先读取原回执，当前不会重发。';
    }
  }

  bool get current {
    _sync();
    return !_closed && !_retired;
  }

  bool get editable =>
      current &&
      !saving &&
      !uncertain &&
      !controlReadFailed &&
      !controlCurrentUnread &&
      controlPending.isEmpty &&
      _publicationPending == null &&
      !_publicationRecoveryFailed &&
      editing?.status != 'published';
  bool get publicationVersionCurrent =>
      current &&
      !_editingNeedsRevalidation &&
      !controlBlocked &&
      !hasPrivateLegacyReferences;
  bool get canSwitchDraft =>
      current &&
      !saving &&
      !uncertain &&
      !controlBlocked &&
      _publicationPending == null &&
      !_publicationRecoveryFailed;
  String get label => kind == 'journey' ? '旅程' : '主题地图';
  bool get hasDraft =>
      title.isNotEmpty || summary.isNotEmpty || draftEntries.isNotEmpty;
  bool get dirty => editing == null
      ? hasDraft
      : title != editing!.title ||
            summary != editing!.summary ||
            jsonEncode(draftEntries.map((e) => e.toInput()).toList()) !=
                jsonEncode(editing!.entries.map((e) => e.toInput()).toList());
  bool get pendingWithdrawal =>
      _pendingDelete || controlPending.any((p) => p.action == 'WITHDRAW');

  void _sync() {
    if (_closed || _retired) return;
    if (_token == null ||
        authorizationHeader() == null ||
        _owner == null ||
        !_collectionUUID.hasMatch(_owner!) ||
        ownerID() != _owner ||
        sourceCurrent?.call() == false) {
      _retainLocalDraft();
      _retired = true;
      _epoch++;
      _reads++;
      _searches++;
      loading = saving = searching = uncertain = false;
      items = const [];
      places = const [];
      draftEntries = const [];
      title = summary = '';
      editing = null;
      _pending = null;
      _pendingID = null;
      _pendingRevision = null;
      _pendingDelete = false;
      _restoredUnknown = false;
      _editingNeedsRevalidation = false;
      _publicationPending = null;
      _copyLocalDrafts = const {};
      _sourceExpiry?.cancel();
      controlPreview = null;
      controlPending = const [];
      _notSubmittedControl = null;
      lastControlReceipt = null;
      controlReady = false;
      _controlReads++;
      error = '账号、城市或工作身份已变化，请返回后重新打开。';
    } else if (authorizationHeader() != _token) {
      _token = authorizationHeader();
      _epoch++;
      _reads++;
      _searches++;
      _controlReads++;
      _controlReading = false;
      _controlReadFuture = null;
      loading = saving = searching = false;
      controlPreview = null;
      controlReady = false;
      places = const [];
      items = const [];
      draftEntries = List.unmodifiable(
        draftEntries.map(
          (e) => PersonalCollectionEntry(
            placeID: e.placeID,
            note: e.note,
            locationVisibility: e.locationVisibility,
            selectorOnly: true,
          ),
        ),
      );
      if (editing != null) _editingNeedsRevalidation = true;
      _sourceExpiry?.cancel();
      unawaited(loadControlPending());
    }
  }

  void _onBoundary() {
    final before = _epoch;
    _sync();
    if (_epoch != before) _notify();
  }

  void _notify() {
    if (!_closed) notifyListeners();
  }

  void _scheduleSourceExpiry() {
    _sourceExpiry?.cancel();
    final deadlines = [
      for (final e in draftEntries) ...[
        if (e.sourceExpiresAt != null && e.sourceCurrent) e.sourceExpiresAt!,
        if (e.legacyReference?.current == true) e.legacyReference!.validUntil,
      ],
      for (final v in items)
        for (final e in v.entries) ...[
          if (e.sourceExpiresAt != null && e.sourceCurrent) e.sourceExpiresAt!,
          if (e.legacyReference?.current == true) e.legacyReference!.validUntil,
        ],
    ];
    if (deadlines.isEmpty || !current) return;
    deadlines.sort();
    _sourceExpiry = Timer(
      deadlines.first.difference(DateTime.now().toUtc()) +
          const Duration(milliseconds: 1),
      () {
        if (current) {
          _notify();
          _scheduleSourceExpiry();
        }
      },
    );
  }

  bool _valid(int epoch) => current && epoch == _epoch;
  Uri _uri(String suffix, [Map<String, String>? query]) => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/personal-collections$suffix',
  ).replace(queryParameters: query);
  Map<String, String> get _headers => {
    'Authorization': _token!,
    'Content-Type': 'application/json',
  };
  dynamic _data(http.Response r) =>
      (jsonDecode(utf8.decode(r.bodyBytes)) as Map<String, dynamic>)['data'];
  PersonalCollection _record(http.Response r) {
    final v = PersonalCollection.fromJson(_data(r) as Map<String, dynamic>);
    if (v.ownerID != _owner || v.kind != kind || v.cityID != cityID)
      throw const FormatException('collection source changed');
    return v;
  }

  Set<String> _retainedPrivatePlaceIDs(PersonalCollection? original) {
    if (original == null ||
        original.ownerID != _owner ||
        original.kind != kind ||
        original.cityID != cityID ||
        original.status != 'draft' ||
        original.visibility != 'private')
      return const {};
    return original.entries
        .where(
          (entry) =>
              original.privatePlaceCopyRestricted ||
              entry.legacyReference != null,
        )
        .map((entry) => entry.placeID)
        .toSet();
  }

  void setText({String? title, String? summary}) {
    if (!editable) return;
    _controlEdited();
    this.title = title ?? this.title;
    this.summary = summary ?? this.summary;
    _notify();
  }

  void addPlace(CollectionPlace p) {
    if (!editable ||
        !_collectionUUID.hasMatch(p.id) ||
        draftEntries.length >= 50 ||
        draftEntries.any((e) => e.placeID == p.id))
      return;
    _controlEdited();
    draftEntries = List.unmodifiable([
      ...draftEntries,
      PersonalCollectionEntry(placeID: p.id, placeName: p.name),
    ]);
    _notify();
  }

  void removeEntry(int index) {
    if (!editable || index < 0 || index >= draftEntries.length) return;
    _controlEdited();
    final rows = [...draftEntries]..removeAt(index);
    draftEntries = List.unmodifiable(rows);
    _notify();
  }

  void moveEntry(int index, int delta) {
    final to = index + delta;
    if (!editable ||
        index < 0 ||
        to < 0 ||
        index >= draftEntries.length ||
        to >= draftEntries.length)
      return;
    _controlEdited();
    final rows = [...draftEntries];
    final entry = rows.removeAt(index);
    rows.insert(to, entry);
    draftEntries = List.unmodifiable(rows);
    _notify();
  }

  void editEntry(int index, String note) {
    if (!editable || index < 0 || index >= draftEntries.length) return;
    _controlEdited();
    final rows = [...draftEntries];
    rows[index] = rows[index].copyWith(note: note);
    draftEntries = List.unmodifiable(rows);
    _notify();
  }

  void newDraft() {
    if (!canSwitchDraft) return;
    _controlEdited();
    editing = null;
    title = summary = '';
    draftEntries = const [];
    _pending = null;
    _pendingID = null;
    _editingNeedsRevalidation = false;
    _restoredUnknown = false;
    error = null;
    _notify();
  }

  void _adopt(PersonalCollection v) {
    _controlEdited();
    controlCurrentUnread = false;
    editing = v;
    title = v.title;
    summary = v.summary;
    draftEntries = v.entries;
    _pending = null;
    _pendingID = null;
    _pendingDelete = false;
    uncertain = false;
    _editingNeedsRevalidation = false;
    _restoredUnknown = false;
    _scheduleSourceExpiry();
  }

  Future<void> load({bool more = false}) async {
    await loadControlPending();
    if (!current || loading || (more && nextCursor == null)) return;
    final epoch = _epoch, serial = ++_reads;
    final after = more ? nextCursor : null;
    loading = true;
    error = null;
    _notify();
    try {
      final r = await _client
          .get(
            _uri('', {'kind': kind, if (after != null) 'cursor': after}),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch) || serial != _reads) return;
      if (r.statusCode != 200) throw const FormatException();
      final p = _data(r) as Map<String, dynamic>,
          records = <PersonalCollection>[];
      for (final row in p['items'] as List) {
        final v = PersonalCollection.fromJson(row as Map<String, dynamic>);
        if (v.ownerID != _owner ||
            v.kind != kind ||
            !const ['draft', 'published'].contains(v.status)) {
          if (v.ownerID != _owner || v.kind != kind)
            throw const FormatException();
          continue;
        }
        records.add(v);
      }
      final cursor = p['nextCursor'] as String?;
      if (records.length > 20 ||
          (cursor != null &&
              (!_collectionUUID.hasMatch(cursor) || cursor == after)))
        throw const FormatException();
      final combined = <String, PersonalCollection>{
        if (more)
          for (final v in items) v.id: v,
        for (final v in records) v.id: v,
      };
      items = List.unmodifiable(combined.values);
      nextCursor = cursor;
      _scheduleSourceExpiry();
    } catch (_) {
      if (_valid(epoch) && serial == _reads)
        error = '无法读取已保存$label，保留已有内容，请重试。';
    } finally {
      if (_valid(epoch) && serial == _reads) {
        loading = false;
        _notify();
      }
    }
  }

  Future<void> searchPlaces(String query) async {
    if (!current) return;
    final epoch = _epoch, serial = ++_searches;
    searching = true;
    searchError = null;
    _notify();
    try {
      final q = query.trim();
      if (utf8.encode(q).length > 240) throw const FormatException();
      final uri = Uri.parse(
        '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/cities/${Uri.encodeComponent(cityID)}/places',
      ).replace(queryParameters: {if (q.isNotEmpty) 'q': q});
      final r = await _client.get(uri).timeout(const Duration(seconds: 12));
      if (!_valid(epoch) || serial != _searches) return;
      if (r.statusCode != 200) throw const FormatException();
      final rows = _data(r) as List, found = <String, CollectionPlace>{};
      for (final row in rows) {
        final p = row as Map<String, dynamic>;
        final id = p['id'] as String, name = p['name'] as String;
        if (!_collectionUUID.hasMatch(id) || name.trim().isEmpty)
          throw const FormatException();
        found[id] = CollectionPlace(id: id, name: name);
      }
      places = List.unmodifiable(found.values);
    } catch (_) {
      if (_valid(epoch) && serial == _searches) {
        places = const [];
        searchError = '无法读取地点，请重试。';
      }
    } finally {
      if (_valid(epoch) && serial == _searches) {
        searching = false;
        _notify();
      }
    }
  }

  Future<bool> open(PersonalCollection v) async {
    return openID(v.id);
  }

  Future<bool> openID(String id) async {
    await loadControlPending();
    if (controlBlocked) return false;
    if (!current || saving || uncertain || _publicationPending != null)
      return false;
    if (!_collectionUUID.hasMatch(id)) return false;
    final epoch = _epoch;
    saving = true;
    error = null;
    _notify();
    try {
      final r = await _client
          .get(_uri('/$kind/$id'), headers: _headers)
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch)) return false;
      if (r.statusCode != 200) throw const FormatException();
      final latest = _record(r);
      if (latest.id != id)
        throw const FormatException('collection target changed');
      if (!const ['draft', 'published'].contains(latest.status))
        throw const FormatException();
      _publicationRecoveryFailed = true;
      final pending = await publicationRecoveryReader?.call(latest.id);
      if (!_valid(epoch)) return false;
      _adopt(latest);
      _publicationRecoveryFailed = false;
      _publicationPending = pending;
      return true;
    } catch (_) {
      if (_valid(epoch)) error = '无法打开当前$label，保留本机草稿，请重试。';
      return false;
    } finally {
      if (_valid(epoch)) {
        saving = false;
        _notify();
      }
    }
  }

  String _uuid() {
    final r = Random.secure(),
        bytes = List<int>.generate(16, (_) => r.nextInt(256));
    bytes[6] = (bytes[6] & 15) | 64;
    bytes[8] = (bytes[8] & 63) | 128;
    final s = bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    return '${s.substring(0, 8)}-${s.substring(8, 12)}-${s.substring(12, 16)}-${s.substring(16, 20)}-${s.substring(20)}';
  }

  Future<bool> save() async {
    await loadControlPending();
    if (controlBlocked) return false;
    if (editing != null) {
      error = '请先检查具体修改，再确认保存。';
      _notify();
      return false;
    }
    if (!current ||
        saving ||
        _pendingDelete ||
        _pendingID != null ||
        _publicationPending != null ||
        _publicationRecoveryFailed ||
        editing?.status == 'published')
      return false;
    if (_restoredUnknown) {
      error = '原操作尚未核实，当前只读取原回执，不会重发。';
      _notify();
      return false;
    }
    if (_editingNeedsRevalidation && !await revalidateRetainedEditing())
      return false;
    if (!current || saving) return false;
    if (_pending == null) {
      final original = editing;
      final retainedUnavailableIDs = _retainedPrivatePlaceIDs(original);
      if (utf8.encode(title.trim()).length > 160 ||
          title.trim().isEmpty ||
          utf8.encode(summary.trim()).length > 5000 ||
          draftEntries.isEmpty ||
          draftEntries.length > 50 ||
          draftEntries.any(
            (e) =>
                utf8.encode(e.note).length > 2000 ||
                e.legacyReference != null && e.locationVisibility != 'hidden' ||
                !e.available && !retainedUnavailableIDs.contains(e.placeID),
          )) {
        error = '填写名称并添加 1–50 个当前可见地点；移除已不可用地点后保存。';
        _notify();
        return false;
      }
      _pendingID = editing?.id;
      _pendingRevision = editing?.revision;
      _pending = {
        'kind': kind,
        'cityId': cityID,
        'title': title.trim(),
        'summary': summary.trim(),
        'entries': draftEntries.map((e) => e.toInput()).toList(),
        if (editing == null) 'operationId': _uuid(),
        if (editing != null) 'revision': editing!.revision,
      };
    }
    final epoch = _epoch;
    saving = true;
    error = null;
    _notify();
    try {
      final r = await _client
          .post(_uri(''), headers: _headers, body: jsonEncode(_pending))
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch)) return false;
      if (r.statusCode != 201) {
        if (r.statusCode >= 400 && r.statusCode < 500) {
          _pending = null;
          uncertain = false;
          error = r.statusCode == 409
              ? '地点或草稿版本已变化，保留本机草稿，请重新查看后保存。'
              : '保存未被接受，保留本机草稿，请检查后重试。';
          return false;
        }
        throw const FormatException();
      }
      final v = _record(r);
      if (v.status != 'draft' || (_pendingID != null && v.id != _pendingID))
        throw const FormatException();
      _adopt(v);
      items = List.unmodifiable([v, ...items.where((e) => e.id != v.id)]);
      return true;
    } catch (_) {
      if (_valid(epoch)) {
        uncertain = true;
        error = '保存结果尚未确认，原稿已保留。请核实结果或按原稿重试。';
      }
      return false;
    } finally {
      if (_valid(epoch)) {
        saving = false;
        _notify();
      }
    }
  }

  Future<bool> revalidateRetainedEditing() async {
    if (!current || saving || uncertain || editing == null) return false;
    if (!_editingNeedsRevalidation) return true;
    final epoch = _epoch, id = editing!.id, revision = editing!.revision;
    saving = true;
    error = null;
    _notify();
    try {
      final r = await _client
          .get(_uri('/$kind/$id'), headers: _headers)
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch)) return false;
      if (r.statusCode != 200) throw const FormatException();
      final latest = _record(r);
      if (latest.id != id ||
          latest.status != 'draft' ||
          latest.revision != revision) {
        error = '原编辑对象版本已变化，保留修改稿；请重新查看当前版本。';
        return false;
      }
      _publicationRecoveryFailed = true;
      final pending = await publicationRecoveryReader?.call(latest.id);
      if (!_valid(epoch)) return false;
      _publicationRecoveryFailed = false;
      if (pending != null) {
        holdPublication(pending);
        error = '原公开操作结果待核实，请打开公开状态读取原回执。';
        return false;
      }
      editing = latest;
      final currentEntries = {for (final e in latest.entries) e.placeID: e};
      draftEntries = List.unmodifiable(
        draftEntries.map((draft) {
          final e = currentEntries[draft.placeID];
          return e == null
              ? draft
              : e.copyWith(
                  note: draft.note,
                  locationVisibility: draft.locationVisibility,
                );
        }),
      );
      _editingNeedsRevalidation = false;
      _scheduleSourceExpiry();
      return true;
    } catch (_) {
      if (_valid(epoch)) error = '无法读取原编辑对象，保留本机修改稿，请重试。';
      return false;
    } finally {
      if (_valid(epoch)) {
        saving = false;
        _notify();
      }
    }
  }

  Future<bool> recover() async {
    if (controlPending.isNotEmpty) return recoverControl(controlPending.first);
    if (_pendingID != null && (_pending?['operationId'] == null)) {
      error = '这份旧记录没有可核对的原操作回执，当前保留原稿；读取当前对象不能证明那次修改或移除成功。';
      _notify();
      return false;
    }
    if (!current || saving || !uncertain) return false;
    final epoch = _epoch;
    saving = true;
    error = null;
    _notify();
    try {
      final op = _pending?['operationId'] as String?;
      final r = await _client
          .get(
            _uri(op != null ? '/operations/$op' : '/$kind/$_pendingID'),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch)) return false;
      if (r.statusCode != 200) {
        error = '尚未取得保存回执，保留原稿。可以按原稿重试。';
        return false;
      }
      final v = _record(r);
      if (_pendingDelete) {
        if (v.status == 'withdrawn') {
          items = List.unmodifiable(items.where((e) => e.id != v.id));
          uncertain = false;
          _pendingDelete = false;
          _pending = null;
          editing = null;
          title = summary = '';
          draftEntries = const [];
          _restoredUnknown = false;
          _editingNeedsRevalidation = false;
          return true;
        }
        error = '尚未确认移除，请按原对象重试。';
        return false;
      }
      final expected = _pending;
      final actual = {
        'kind': v.kind,
        'cityId': v.cityID,
        'title': v.title,
        'summary': v.summary,
        'entries': v.entries.map((e) => e.toInput()).toList(),
      };
      final target = expected == null
          ? null
          : ({...expected}
              ..remove('operationId')
              ..remove('revision'));
      if (v.status != 'draft' ||
          jsonEncode(actual) != jsonEncode(target) ||
          (_pendingID != null && v.revision <= (_pendingRevision ?? 0))) {
        error = '服务端内容与原稿不同，请保留原稿并重新查看。';
        return false;
      }
      _adopt(v);
      items = List.unmodifiable([v, ...items.where((e) => e.id != v.id)]);
      return true;
    } catch (_) {
      if (_valid(epoch)) error = '无法核实结果，保留原稿，请重试。';
      return false;
    } finally {
      if (_valid(epoch)) {
        saving = false;
        _notify();
      }
    }
  }

  Future<void> loadControlPending() {
    if (!current) return Future.value();
    final old = _controlReadFuture;
    if (old != null) return old;
    late Future<void> future;
    future = _readControlPending().whenComplete(() {
      if (identical(_controlReadFuture, future)) _controlReadFuture = null;
    });
    _controlReadFuture = future;
    return future;
  }

  Future<void> _readControlPending() async {
    if (!current || _controlReading) return;
    final epoch = _epoch, serial = ++_controlReads, owner = _owner!;
    _controlReading = true;
    try {
      final values = await controlPendingStore.read(controlAPI.origin, owner);
      if (!_valid(epoch) || serial != _controlReads) return;
      controlPending = List.unmodifiable(values);
      controlReady = true;
      controlReadFailed = false;
      if (controlPending.isNotEmpty) {
        uncertain = true;
        controlPreview = null;
        error = '原集合操作尚未核实，当前只可核实、停止或检查未提交的本机准备记录。';
      }
    } catch (_) {
      if (_valid(epoch) && serial == _controlReads) {
        controlReady = false;
        controlReadFailed = true;
        error = '本机原集合操作列表暂不可读，请重新读取；当前不提交新操作。';
      }
    } finally {
      if (_valid(epoch) && serial == _controlReads) {
        _controlReading = false;
        _notify();
      }
    }
  }

  Future<CollectionControlPreview?> prepareControl(String action) async {
    await loadControlPending();
    if (!current ||
        saving ||
        uncertain ||
        controlBlocked ||
        editing == null ||
        editing!.status != 'draft' ||
        _publicationPending != null ||
        _publicationRecoveryFailed)
      return null;
    if (_editingNeedsRevalidation && !await revalidateRetainedEditing())
      return null;
    final original = editing!;
    CollectionControlInput input;
    try {
      final retained = _retainedPrivatePlaceIDs(original);
      if (action == 'UPDATE' &&
          draftEntries.any(
            (e) =>
                e.legacyReference != null && e.locationVisibility != 'hidden' ||
                !e.available && !retained.contains(e.placeID),
          )) {
        throw const FormatException();
      }
      input = CollectionControlInput(
        operationID: newCollectionControlOperationID(),
        action: action,
        kind: kind,
        collectionID: original.id.toLowerCase(),
        expectedRevision: original.revision,
        input: action == 'UPDATE'
            ? CollectionControlBody.normalize({
                'kind': kind,
                'cityId': cityID,
                'title': title,
                'summary': summary,
                'entries': draftEntries.map((e) => e.toInput()).toList(),
              })
            : null,
      );
    } catch (_) {
      error = '请检查名称、备注、地点和当前草稿版本后再预览。';
      _notify();
      return null;
    }
    final epoch = _epoch, draft = _draftVersion;
    bool live() =>
        _valid(epoch) &&
        draft == _draftVersion &&
        identical(editing, original) &&
        controlPending.isEmpty;
    saving = true;
    error = null;
    controlPreview = null;
    _notify();
    try {
      final preview = await controlAPI.preview(input, live);
      if (!live()) return null;
      controlPreview = preview;
      _previewEpoch = epoch;
      _previewDraft = draft;
      return preview;
    } catch (_) {
      if (live()) error = '未取得可确认的具体修改，请检查当前版本后重新预览。';
      return null;
    } finally {
      if (_valid(epoch)) {
        saving = false;
        _notify();
      }
    }
  }

  void cancelControlPreview(CollectionControlPreview preview) {
    if (identical(controlPreview, preview)) {
      controlPreview = null;
      _notify();
    }
  }

  Future<bool> commitControl(CollectionControlPreview preview) async {
    if (!controlPreviewCurrent(preview)) return false;
    final epoch = _epoch, draft = _draftVersion, owner = _owner!;
    final p = CollectionControlPending.preview(preview);
    var dispatched = false;
    bool live() =>
        _valid(epoch) &&
        draft == _draftVersion &&
        identical(controlPreview, preview) &&
        preview.live &&
        editing?.id == preview.input.collectionID &&
        editing?.revision == preview.input.expectedRevision;
    saving = true;
    error = null;
    _notify();
    try {
      if ((await controlPendingStore.read(controlAPI.origin, owner)).isNotEmpty)
        throw StateError('原操作待核实');
      if (!live()) return false;
      _notSubmittedControl = p;
      await controlPendingStore.write(controlAPI.origin, owner, p);
      if (!live()) return false;
      final prepared = await controlPendingStore.read(controlAPI.origin, owner);
      if (!live()) return false;
      if (!prepared.any((v) => v.same(p) && v.stage == 'PREPARED'))
        throw StateError('准备记录未读回');
      await controlPendingStore.write(controlAPI.origin, owner, p.unknown);
      if (!live()) return false;
      final unknown = await controlPendingStore.read(controlAPI.origin, owner);
      if (!live()) return false;
      if (!unknown.any((v) => v.same(p) && v.stage == 'UNKNOWN'))
        throw StateError('提交记录未读回');
      controlPending = List.unmodifiable(unknown);
      uncertain = true;
      _notSubmittedControl = null;
      dispatched = true;
      final receipt = await controlAPI.put(preview, live);
      if (!live()) return false;
      return await _finishControl(p.unknown, receipt, epoch);
    } catch (_) {
      if (_valid(epoch)) {
        if (dispatched) {
          controlPending = List.unmodifiable([
            p.unknown,
            ...controlPending.where((v) => v.operationID != p.operationID),
          ]);
          uncertain = true;
          error = '原提交结果待核实。不会再次提交，请核实原结果或明确停止原操作。';
        } else {
          error = '尚未发送修改。请检查本机准备记录后重新准备；不会核实不存在的提交。';
          controlReady = false;
          controlReadFailed = true;
        }
      }
      return false;
    } finally {
      if (_valid(epoch)) {
        saving = false;
        _notify();
      }
      if (current) unawaited(loadControlPending());
    }
  }

  Future<bool> repairControlPrepared(CollectionControlPending p) async {
    if (!current ||
        saving ||
        !(p.stage == 'PREPARED' || _notSubmittedControl?.same(p) == true))
      return false;
    final epoch = _epoch, owner = _owner!;
    saving = true;
    try {
      final values = await controlPendingStore.read(controlAPI.origin, owner);
      if (!_valid(epoch)) return false;
      final matches = values.where((v) => v.same(p));
      if (matches.isNotEmpty) {
        final exact = matches.single;
        if (exact.stage != 'PREPARED' &&
            _notSubmittedControl?.same(exact) != true)
          return false;
        await controlPendingStore.remove(controlAPI.origin, owner, exact);
        if (!_valid(epoch)) return false;
      }
      _notSubmittedControl = null;
      controlPending = List.unmodifiable(values.where((v) => !v.same(p)));
      controlPreview = null;
      controlReadFailed = false;
      controlReady = true;
      uncertain = controlPending.isNotEmpty;
      error = '本机准备记录已清理，修改尚未发送。请重新预览后确认。';
      return true;
    } catch (_) {
      if (_valid(epoch)) error = '本机准备记录暂不可核对，保留原稿，请重试。';
      return false;
    } finally {
      if (_valid(epoch)) {
        saving = false;
        _notify();
      }
    }
  }

  Future<bool> recoverControl(
    CollectionControlPending p, {
    bool stop = false,
  }) async {
    if (!current || saving || !controlPending.any((v) => v.same(p)))
      return false;
    if (p.stage == 'PREPARED' || _notSubmittedControl?.same(p) == true)
      return repairControlPrepared(p);
    final epoch = _epoch;
    saving = true;
    error = null;
    _notify();
    try {
      final raw = await controlAPI.original(
        p.operationID,
        () => _valid(epoch),
        stop: stop,
      );
      if (!_valid(epoch)) return false;
      final receipt = CollectionControlReceipt.decode(
        raw,
        operationID: p.operationID,
        action: p.action,
        kind: p.kind,
        collectionID: p.collectionID,
        expectedRevision: p.expectedRevision,
        payloadDigest: p.payloadDigest,
      );
      return await _finishControl(p, receipt, epoch);
    } catch (_) {
      if (_valid(epoch)) error = '尚未核实原操作结果，继续保留原记录；不会创建新操作或再次提交。';
      return false;
    } finally {
      if (_valid(epoch)) {
        saving = false;
        _notify();
      }
    }
  }

  Future<bool> _finishControl(
    CollectionControlPending p,
    CollectionControlReceipt receipt,
    int epoch,
  ) async {
    if (!_valid(epoch)) return false;
    lastControlReceipt = receipt;
    controlPreview = null;
    try {
      await controlPendingStore.remove(controlAPI.origin, _owner!, p);
    } catch (_) {
      if (_valid(epoch)) error = '原结果已经核实，本机记录尚未清理，请再次核实原结果完成清理。';
      return false;
    }
    if (!_valid(epoch)) return false;
    controlPending = List.unmodifiable(controlPending.where((v) => !v.same(p)));
    _notSubmittedControl = null;
    uncertain = controlPending.isNotEmpty;
    controlReady = true;
    controlReadFailed = false;
    if (!receipt.succeeded) {
      _editingNeedsRevalidation = editing != null;
      error = '原操作已停止，迟到提交不会生效；本人修改稿仍保留。';
      _retainLocalDraft();
      return true;
    }
    if (p.kind != kind) {
      error = '已核实本人原集合操作，当前编辑稿未被替换。';
      _retainLocalDraft();
      return true;
    }
    if (p.action == 'WITHDRAW') {
      items = List.unmodifiable(items.where((v) => v.id != p.collectionID));
      if (editing?.id == p.collectionID) {
        editing = null;
        title = summary = '';
        draftEntries = const [];
        _editingNeedsRevalidation = false;
      }
      error = '已核实移除完成。';
      _retainLocalDraft();
      return true;
    }
    try {
      final response = await _client
          .get(_uri('/' + p.kind + '/' + p.collectionID), headers: _headers)
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch)) return true;
      if (response.statusCode != 200) throw const FormatException();
      final latest = _record(response);
      if (latest.id != p.collectionID ||
          latest.revision < receipt.appliedRevision!)
        throw const FormatException();
      _adopt(latest);
      items = List.unmodifiable([
        latest,
        ...items.where((v) => v.id != latest.id),
      ]);
      error = null;
    } catch (_) {
      if (_valid(epoch)) {
        items = List.unmodifiable(items.where((v) => v.id != p.collectionID));
        draftEntries = List.unmodifiable(
          draftEntries.map(
            (e) => PersonalCollectionEntry(
              placeID: e.placeID,
              note: e.note,
              locationVisibility: e.locationVisibility,
              selectorOnly: true,
            ),
          ),
        );
        editing = PersonalCollection(
          id: p.collectionID,
          ownerID: _owner!,
          kind: kind,
          cityID: cityID,
          title: '',
          summary: '',
          entries: const [],
          revision: receipt.appliedRevision!,
          status: 'draft',
        );
        _editingNeedsRevalidation = true;
        controlCurrentUnread = true;
        error = '已核实修改完成，但当前内容暂不可读取。本人文字保留，请重新读取当前记录。';
      }
    }
    _retainLocalDraft();
    return true;
  }

  Future<bool> refreshControlTarget() async {
    final receipt = lastControlReceipt;
    if (receipt == null ||
        !current ||
        saving ||
        !receipt.succeeded ||
        receipt.action != 'UPDATE' ||
        controlPending.isNotEmpty)
      return false;
    final epoch = _epoch;
    saving = true;
    try {
      final response = await _client
          .get(
            _uri('/' + receipt.kind! + '/' + receipt.collectionID!),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch) || response.statusCode != 200) return false;
      final latest = _record(response);
      if (latest.id != receipt.collectionID ||
          latest.revision < receipt.appliedRevision!)
        return false;
      _adopt(latest);
      items = List.unmodifiable([
        latest,
        ...items.where((v) => v.id != latest.id),
      ]);
      error = null;
      return true;
    } catch (_) {
      return false;
    } finally {
      if (_valid(epoch)) {
        saving = false;
        _notify();
      }
    }
  }

  Future<bool> withdraw() async {
    error = '请先检查具体移除后果，再确认。';
    _notify();
    return false;
  }

  @override
  void dispose() {
    if (_closed) return;
    if (!_retired) _retainLocalDraft();
    _closed = true;
    _epoch++;
    _sourceExpiry?.cancel();
    identityChanges?.removeListener(_onBoundary);
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
