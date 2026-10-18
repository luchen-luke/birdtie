import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import 'personal_collection_controller.dart';
import 'collection_publication_pending_store.dart';
import 'collection_copy_controller.dart';
import 'collection_copy_pending_store.dart';
import 'journey_detail.dart';
import 'private_moment_controller.dart';

final _id = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);

class CollectionShareReference {
  const CollectionShareReference(this.kind, this.id);
  final String kind, id;
  String get code => 'birdtie:collection/$kind/$id';
  static CollectionShareReference? parse(String value) {
    final m = RegExp(
      r'^birdtie:collection/(journey|theme_map)/([0-9a-fA-F-]{36})$',
    ).firstMatch(value.trim());
    return m == null || !_id.hasMatch(m[2]!)
        ? null
        : CollectionShareReference(m[1]!, m[2]!.toLowerCase());
  }
}

/// One explicit publication of an already saved native collection revision.
/// Unknown effects retain only their causal selectors in the owner's draft.
class CollectionPublicationController extends ChangeNotifier {
  CollectionPublicationController({
    required this.source,
    required this.authorizationHeader,
    required this.ownerID,
    http.Client? client,
    String? apiBaseUrl,
    this.pendingStore,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl {
    _token = authorizationHeader();
    _owner = ownerID();
    record = source.editing;
    precision = [
      for (final e in record?.entries ?? <PersonalCollectionEntry>[])
        e.locationVisibility,
    ];
    source.addListener(_sourceChanged);
  }
  final PersonalCollectionController source;
  final String? Function() authorizationHeader, ownerID;
  final http.Client _client;
  final bool _ownsClient;
  final String _base;
  final CollectionPublicationPendingStore? pendingStore;
  bool _storeReady = false;
  Future<void> restorePending() async {
    if (!current || busy) return;
    final epoch = _epoch;
    busy = true;
    try {
      final pending = await pendingStore?.load();
      if (!current || epoch != _epoch) return;
      if (pending != null) source.holdPublication(pending);
      _storeReady = true;
    } catch (_) {
      if (current) error = '无法读取原操作记录，请重新打开；当前不会提交。';
    } finally {
      if (current && epoch == _epoch) {
        busy = false;
        notifyListeners();
      }
    }
  }

  String? _token, _owner;
  PersonalCollection? record;
  List<String> precision = [];
  bool allowPrivatePlaceCopy = false;
  bool busy = false, _closed = false, _retired = false;
  int _epoch = 0;
  String? error;
  String? notice;
  bool get current {
    if (!_closed &&
        !_retired &&
        (!source.current ||
            _token == null ||
            authorizationHeader() != _token ||
            ownerID() != _owner)) {
      _retired = true;
      _epoch++;
      record = null;
      precision = [];
      allowPrivatePlaceCopy = false;
      busy = false;
      error = '账号或来源已变化，请返回后重新打开。';
    }
    return !_closed && !_retired;
  }

  bool get uncertain => source.publicationPending != null;
  bool get canConfigure =>
      current &&
      (pendingStore == null || _storeReady) &&
      !busy &&
      !uncertain &&
      source.editable &&
      source.publicationVersionCurrent &&
      !source.dirty &&
      source.editing?.id == record?.id &&
      source.editing?.revision == record?.revision &&
      record?.status == 'draft' &&
      record?.privatePlaceCopyRestricted != true &&
      !source.uncertain &&
      !source.saving;
  bool get canUnpublish =>
      current &&
      (pendingStore == null || _storeReady) &&
      !busy &&
      !uncertain &&
      source.publicationVersionCurrent &&
      (record?.status == 'published' || record?.publishedSnapshot != null) &&
      source.editing?.id == record?.id &&
      source.editing?.revision == record?.revision;
  bool get richJourney =>
      record?.kind == 'journey' && record?.journeyDetails != null;
  List<JourneyEntry> get journeyStops => [
    for (final e in record?.journeyDetails?.itinerary ?? <JourneyEntry>[])
      if (e.type == 'STOP') e,
  ];
  final selectedStops = <String>{};
  final stopPrecision = <String, String>{};
  final stopMoments = <String, PrivateMoment>{};
  final commentStops = <String>{};
  List<PrivateMoment> momentChoices = const [];
  bool loadingMoments = false;
  String? momentsError;
  bool get canPublish =>
      canConfigure &&
      (!richJourney ||
          selectedStops.isNotEmpty &&
              selectedStops.length <= 50 &&
              record!.title.trim().isNotEmpty &&
              utf8.encode(record!.title).length <= 160 &&
              utf8.encode(record!.summary).length <= 5000 &&
              journeyStops
                  .where((e) => selectedStops.contains(e.id))
                  .every(
                    (e) =>
                        utf8.encode(e.text('title')).length <= 160 &&
                        utf8.encode(e.text('note')).length <= 2000,
                  ));
  void selectStop(String id, bool selected) {
    if (!canConfigure || !journeyStops.any((e) => e.id == id)) return;
    if (selected) {
      selectedStops.add(id);
    } else {
      selectedStops.remove(id);
    }
    notifyListeners();
  }

  void setStopPrecision(String id, String value) {
    if (!canConfigure ||
        !selectedStops.contains(id) ||
        !const {'hidden', 'city', 'place'}.contains(value))
      return;
    stopPrecision[id] = value;
    notifyListeners();
  }

  void setStopMoment(String id, PrivateMoment? moment) {
    if (!canConfigure ||
        !selectedStops.contains(id) ||
        moment != null && !momentChoices.contains(moment))
      return;
    if (moment == null) {
      stopMoments.remove(id);
    } else {
      stopMoments[id] = moment;
    }
    commentStops.remove(id);
    notifyListeners();
  }

  void setStopComment(String id, bool enabled) {
    if (!canConfigure || !selectedStops.contains(id) || stopMoments[id] == null)
      return;
    if (enabled) {
      commentStops.add(id);
    } else {
      commentStops.remove(id);
    }
    notifyListeners();
  }

  Map<String, dynamic> get approvedJourneySnapshot => {
    'title': record!.title,
    'summary': record!.summary,
    'stops': [
      for (final e in journeyStops)
        if (selectedStops.contains(e.id))
          {
            'entryId': e.id,
            'position': e.values['position'],
            'title': e.text('title'),
            'note': e.text('note'),
            'locationVisibility': stopPrecision[e.id] ?? 'hidden',
            if (stopMoments[e.id] case final moment?) ...{
              'momentId': moment.id,
              'momentRevision': moment.revision,
              if (commentStops.contains(e.id)) 'commentMomentId': moment.id,
            },
          },
    ],
  };
  Future<void> loadMomentChoices() async {
    if (!canConfigure || loadingMoments) return;
    final epoch = _epoch;
    loadingMoments = true;
    momentsError = null;
    notifyListeners();
    try {
      final response = await _client
          .get(
            Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/moments'),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!current || epoch != _epoch) return;
      if (response.statusCode != 200) throw const FormatException();
      final rows =
          (jsonDecode(utf8.decode(response.bodyBytes))
                  as Map<String, dynamic>)['data']
              as List;
      final values = rows
          .map((v) => PrivateMoment.fromJson(v as Map<String, dynamic>))
          .toList();
      if (values.any(
        (v) =>
            v.authorAccountID != _owner ||
            !_id.hasMatch(v.id) ||
            v.revision < 1,
      ))
        throw const FormatException();
      momentChoices = List.unmodifiable(
        values.where(
          (v) => v.status == 'published' && v.visibility == 'public',
        ),
      );
    } catch (_) {
      if (current && epoch == _epoch) momentsError = '无法读取本人已发布动态，请重试。';
    } finally {
      if (current && epoch == _epoch) {
        loadingMoments = false;
        notifyListeners();
      }
    }
  }

  void _sourceChanged() {
    current;
    if (!_closed) notifyListeners();
  }

  void setPrecision(int i, String value) {
    if (!canPublish ||
        i < 0 ||
        i >= precision.length ||
        !const ['hidden', 'city', 'place'].contains(value))
      return;
    precision = [...precision]..[i] = value;
    notifyListeners();
  }

  void setAllowPrivatePlaceCopy(bool value) {
    if (!canPublish) return;
    allowPrivatePlaceCopy = value;
    notifyListeners();
  }

  Map<String, String> get _headers => {
    'Authorization': _token!,
    'Content-Type': 'application/json',
  };
  Uri _uri(String suffix) => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/personal-collections$suffix',
  );
  String _uuid() {
    final random = Random.secure(),
        b = List.generate(16, (_) => random.nextInt(256));
    b[6] = (b[6] & 15) | 64;
    b[8] = (b[8] & 63) | 128;
    final s = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
    return '${s.substring(0, 8)}-${s.substring(8, 12)}-${s.substring(12, 16)}-${s.substring(16, 20)}-${s.substring(20)}';
  }

  Future<bool> publish() => _write('PUBLISH');
  Future<bool> unpublish() => _write('UNPUBLISH');
  Future<bool> _write(String action) async {
    if (action == 'PUBLISH' ? !canPublish : !canUnpublish) return false;
    final v = record!, epoch = _epoch;
    final pending = {
      'id': v.id,
      'kind': v.kind,
      'operationId': _uuid(),
      'revision': v.revision,
      'action': action,
    };
    final payload = {
      'operationId': pending['operationId'],
      'revision': v.revision,
      if (action == 'PUBLISH' && !richJourney)
        'allowPrivatePlaceCopy': allowPrivatePlaceCopy,
      if (action == 'PUBLISH' && richJourney)
        'journeySnapshot': approvedJourneySnapshot,
      if (action == 'PUBLISH' && !richJourney)
        'entries': [
          for (var i = 0; i < v.entries.length; i++)
            {
              'placeId': v.entries[i].placeID,
              'locationVisibility': precision[i],
            },
        ],
    };
    source.holdPublication(pending);
    busy = true;
    error = null;
    notifyListeners();
    try {
      await pendingStore?.save(pending);
    } catch (_) {
      Map<String, dynamic>? retained = pending;
      try {
        retained = await pendingStore?.load();
      } catch (_) {}
      if (current && epoch == _epoch) {
        source.holdPublication(retained);
        _storeReady = false;
        busy = false;
        error = '无法保留操作回执，本次尚未提交，请重新打开。';
        notifyListeners();
      }
      return false;
    }
    if (!current || epoch != _epoch) {
      // No HTTP invocation has happened. Retirement during persistence cannot
      // leave a missing native receipt blocking the original owner's draft.
      try {
        await pendingStore?.clearIfOperation(pending['operationId'] as String);
      } catch (_) {}
      source.clearUnsentPublication(pending['operationId'] as String);
      return false;
    }
    try {
      final r = await _client
          .post(
            _uri(
              '/${v.kind}/${v.id}${action == 'UNPUBLISH' ? '/unpublish' : '/publication'}',
            ),
            headers: _headers,
            body: jsonEncode(payload),
          )
          .timeout(const Duration(seconds: 12));
      if (!current || epoch != _epoch) return false;
      if (r.statusCode != 200) {
        // Authentication can be rejected by the final response guard after a
        // native commit. Preserve its causal selector for the original owner.
        if (r.statusCode == 400 || r.statusCode == 409) {
          await pendingStore?.clearIfOperation(
            pending['operationId'] as String,
          );
          if (!current || epoch != _epoch) return false;
          source.holdPublication(null);
          error = r.statusCode == 409
              ? '已保存内容或地点版本已变化，请返回重新读取后预览。'
              : '本次操作未被接受，请重新读取当前状态。';
          return false;
        }
        throw const FormatException();
      }
      return await _accept(r, pending);
    } catch (_) {
      if (current && epoch == _epoch) error = '结果待核实。请读取原操作回执，当前不会重发。';
      return false;
    } finally {
      if (current && epoch == _epoch) {
        busy = false;
        notifyListeners();
      }
    }
  }

  Future<bool> _accept(http.Response r, Map<String, dynamic> pending) async {
    final data =
        (jsonDecode(utf8.decode(r.bodyBytes)) as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    final causal = data['publicationOperation'] as Map<String, dynamic>;
    final outcome = causal['outcome'] ?? 'COMMITTED';
    if (!const ['COMMITTED', 'NO_EFFECT'].contains(outcome) ||
        causal.keys.any(
          (key) => !const {
            'operationId',
            'action',
            'expectedRevision',
            'resultRevision',
            'outcome',
          }.contains(key),
        ))
      throw const FormatException('invalid operation outcome');
    if (causal['operationId'] != pending['operationId'] ||
        causal['action'] != pending['action'] ||
        causal['expectedRevision'] != pending['revision'] ||
        (outcome == 'COMMITTED'
            ? causal['resultRevision'] != (pending['revision'] as int) + 1
            : causal.containsKey('resultRevision'))) {
      throw const FormatException('invalid causal receipt');
    }
    final v = PersonalCollection.fromJson(data);
    if (v.id != pending['id'] ||
        v.kind != pending['kind'] ||
        v.ownerID != _owner ||
        v.cityID != source.cityID ||
        (outcome == 'COMMITTED' &&
            v.revision < (causal['resultRevision'] as int)))
      throw const FormatException();
    // Finish cleanup while the old operation still fences every new write.
    // A delayed deletion must never erase the next operation's recovery key.
    await pendingStore?.clearIfOperation(pending['operationId'] as String);
    if (!current) return false;
    if (outcome == 'NO_EFFECT') {
      source.acceptPublicationNoEffect(v, pending['operationId'] as String);
      notice = '这次操作已结束，未产生发布或撤回效果。原本机草稿已保留，请返回重新读取当前版本。';
    } else {
      source.acceptPublicationReceipt(v);
      notice = null;
    }
    record = v;
    selectedStops.clear();
    stopPrecision.clear();
    stopMoments.clear();
    commentStops.clear();
    precision = v.entries.map((e) => e.locationVisibility).toList();
    error = null;
    return true;
  }

  Future<bool> recover() async {
    if (!current || busy || source.publicationPending == null) return false;
    final pending = source.publicationPending!, epoch = _epoch;
    busy = true;
    error = null;
    notifyListeners();
    try {
      final r = await _client
          .get(
            _uri('/publication-operations/${pending['operationId']}/receipt'),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!current || epoch != _epoch) return false;
      if (r.statusCode != 200) throw const FormatException();
      return await _accept(r, pending);
    } catch (_) {
      if (current && epoch == _epoch) error = '尚未取得原操作回执，请稍后再次核实；不会重发。';
      return false;
    } finally {
      if (current && epoch == _epoch) {
        busy = false;
        notifyListeners();
      }
    }
  }

  /// Explicitly closes the original operation. A missing GET receipt alone
  /// never establishes that a publication did not commit.
  Future<bool> closeOriginal() async {
    if (!current || busy || source.publicationPending == null) return false;
    final pending = Map<String, dynamic>.from(source.publicationPending!),
        epoch = _epoch;
    busy = true;
    error = null;
    notifyListeners();
    try {
      final r = await _client
          .post(
            _uri('/publication-operations/${pending['operationId']}/close'),
            headers: _headers,
            body: jsonEncode({
              'kind': pending['kind'],
              'id': pending['id'],
              'action': pending['action'],
              'revision': pending['revision'],
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (!current || epoch != _epoch) return false;
      if (r.statusCode != 200) throw const FormatException();
      return await _accept(r, pending);
    } catch (_) {
      if (current && epoch == _epoch) {
        error = '尚未取得原操作的最终回执，请再次核实；不会重新发布或撤回。';
      }
      return false;
    } finally {
      if (current && epoch == _epoch) {
        busy = false;
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    source.removeListener(_sourceChanged);
    _closed = true;
    _epoch++;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}

class PublicJourneySnapshot {
  PublicJourneySnapshot(
    this.publicationRevision,
    this.sourceRevision,
    this.publishedAt,
    this.stops,
  );
  final int publicationRevision, sourceRevision;
  final DateTime publishedAt;
  final List<Map<String, dynamic>> stops;
  factory PublicJourneySnapshot.decode(Object? raw, int revision) {
    if (raw is! Map<String, dynamic> ||
        raw['schemaVersion'] != 'journey-publication-v1' ||
        raw['publicationRevision'] != revision ||
        raw['sourceRevision'] is! int ||
        raw['sourceRevision'] < 1 ||
        raw['sourceRevision'] >= revision ||
        raw['stops'] is! List)
      throw const FormatException('旅程公开快照不符');
    final at = DateTime.parse(raw['publishedAt'] as String).toUtc(),
        rows = (raw['stops'] as List)
            .map((v) => Map<String, dynamic>.from(v as Map<String, dynamic>))
            .toList();
    if (at.isAfter(DateTime.now().toUtc().add(const Duration(seconds: 1))) ||
        rows.isEmpty ||
        rows.length > 50)
      throw const FormatException('旅程发布顺序不符');
    var previous = 0;
    final ids = <String>{};
    for (final row in rows) {
      if (!_id.hasMatch(row['entryId'] as String? ?? '') ||
          !ids.add(row['entryId']) ||
          row['position'] is! int ||
          row['position'] <= previous ||
          row['position'] > 500 ||
          row['available'] is! bool)
        throw const FormatException('原旅程站点不符');
      previous = row['position'];
      final allowed = row['available'] == true
          ? {
              'entryId',
              'position',
              'available',
              'title',
              'note',
              'locationVisibility',
              'placeId',
              'placeName',
              'latitude',
              'longitude',
              'momentId',
              'momentRevision',
              'commentMomentId',
            }
          : {'entryId', 'position', 'available'};
      if (row.keys.any((k) => !allowed.contains(k)))
        throw const FormatException('公开旅程越过批准范围');
      if (row['available'] != true) continue;
      row.putIfAbsent('title', () => '');
      row.putIfAbsent('note', () => '');
      if (row['title'] is! String ||
          row['note'] is! String ||
          !const {
            'hidden',
            'city',
            'place',
          }.contains(row['locationVisibility']))
        throw const FormatException('公开旅程站点不完整');
      if (row['locationVisibility'] != 'place' &&
          [
            'placeId',
            'placeName',
            'latitude',
            'longitude',
          ].any(row.containsKey))
        throw const FormatException('旅程位置越过批准范围');
      final lat = row['latitude'], lon = row['longitude'];
      if ((lat == null) != (lon == null) ||
          lat != null &&
              (lat is! num ||
                  lon is! num ||
                  !lat.isFinite ||
                  !lon.isFinite ||
                  lat.abs() > 90 ||
                  lon.abs() > 180))
        throw const FormatException('旅程公开位置不符');
      if (row['placeId'] != null && !_id.hasMatch(row['placeId'] as String))
        throw const FormatException();
      final mid = row['momentId'] as String? ?? '',
          comment = row['commentMomentId'] as String? ?? '';
      if (mid.isNotEmpty &&
              (!_id.hasMatch(mid) ||
                  row['momentRevision'] is! int ||
                  row['momentRevision'] < 1) ||
          comment.isNotEmpty && comment != mid ||
          mid.isEmpty && row.containsKey('momentRevision'))
        throw const FormatException('旅程动态引用不符');
    }
    return PublicJourneySnapshot(
      revision,
      raw['sourceRevision'],
      at,
      List.unmodifiable(rows.map((r) => Map<String, dynamic>.unmodifiable(r))),
    );
  }
}

class PublicCollectionRecord {
  PublicCollectionRecord._(
    this.reference,
    this.cityID,
    this.title,
    this.summary,
    this.revision,
    this.expiresAt,
    this.entries,
    this.copyGrant,
    this.journeySnapshot,
  );
  final CollectionShareReference reference;
  final String cityID, title, summary;
  final int revision;
  final DateTime expiresAt;
  final List<Map<String, dynamic>> entries;
  final CollectionCopyGrant? copyGrant;
  final PublicJourneySnapshot? journeySnapshot;
  factory PublicCollectionRecord.fromJson(
    Map<String, dynamic> j,
    CollectionShareReference expected,
  ) {
    final expires = DateTime.parse(j['sourceExpiresAt'] as String).toUtc();
    final rows = (j['entries'] as List).cast<Map<String, dynamic>>();
    final snapshot = j['journeySnapshot'] == null
        ? null
        : PublicJourneySnapshot.decode(
            j['journeySnapshot'],
            j['revision'] as int,
          );
    final now = DateTime.now().toUtc();
    if (j['id'] != expected.id ||
        j['kind'] != expected.kind ||
        (j['revision'] as int) < 1 ||
        snapshot == null && (j['cityId'] as String).isEmpty ||
        (j['title'] as String).trim().isEmpty ||
        !now.isBefore(expires) ||
        expires.difference(now) > const Duration(seconds: 61) ||
        (snapshot == null
            ? rows.isEmpty
            : rows.isNotEmpty || expected.kind != 'journey') ||
        rows.length > 50 ||
        [
          'ownerAccountId',
          'journeyDetails',
          'budget',
          'members',
          'lodgings',
          'origin',
        ].any(j.containsKey))
      throw const FormatException();
    for (final row in rows) {
      final p = row['locationVisibility'];
      if (!const ['hidden', 'city', 'place'].contains(p) ||
          row['note'] is! String)
        throw const FormatException();
      if (p != 'place' &&
          [
            'placeId',
            'placeName',
            'latitude',
            'longitude',
          ].any(row.containsKey))
        throw const FormatException('location leaked');
      if (row['placeId'] != null &&
          (!_id.hasMatch(row['placeId'] as String) ||
              (row['placeName'] as String).isEmpty))
        throw const FormatException();
      final lat = (row['latitude'] as num?)?.toDouble(),
          lng = (row['longitude'] as num?)?.toDouble();
      if ((lat == null) != (lng == null) ||
          (lat != null &&
              (!lat.isFinite ||
                  lat.abs() > 90 ||
                  !lng!.isFinite ||
                  lng.abs() > 180)))
        throw const FormatException();
      if (row['placeId'] == null &&
          ['placeName', 'latitude', 'longitude'].any(row.containsKey))
        throw const FormatException();
    }
    return PublicCollectionRecord._(
      expected,
      j['cityId'] as String,
      j['title'] as String,
      j['summary'] as String,
      j['revision'] as int,
      expires,
      List.unmodifiable(rows.map((r) => Map<String, dynamic>.unmodifiable(r))),
      j.containsKey('copyGrant')
          ? CollectionCopyGrant.fromJson(
              j['copyGrant'],
              source: CollectionCopyReference(expected.kind, expected.id),
              publicationRevision: j['revision'] as int,
              sourceExpiresAt: expires,
              publicPlaceIDs: [
                for (final row in rows)
                  if (row['locationVisibility'] == 'place' &&
                      row['placeId'] is String)
                    row['placeId'] as String,
              ],
            )
          : null,
      snapshot,
    );
  }
}

class PublicCollectionController extends ChangeNotifier {
  PublicCollectionController({
    required this.reference,
    required this.authorizationHeader,
    this.identityChanges,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl {
    _token = authorizationHeader();
    identityChanges?.addListener(_changed);
  }
  final CollectionShareReference reference;
  final String? Function() authorizationHeader;
  final Listenable? identityChanges;
  final http.Client _client;
  final bool _ownsClient;
  final String _base;
  http.Client get transport => _client;
  String get apiBaseUrl => _base;
  String? _token, error;
  PublicCollectionRecord? record;
  bool loading = false, _closed = false, _retired = false;
  int _epoch = 0;
  Timer? _expiry;
  bool get current {
    if (!_closed && !_retired && authorizationHeader() != _token) {
      _retired = true;
      _epoch++;
      record = null;
      loading = false;
      _expiry?.cancel();
      error = '账号已变化，请返回后重新打开分享。';
    }
    if (record != null && !DateTime.now().toUtc().isBefore(record!.expiresAt))
      record = null;
    return !_closed && !_retired;
  }

  void _changed() {
    if (!_closed) {
      current;
      notifyListeners();
    }
  }

  Future<void> load() async {
    if (!current || loading) return;
    final epoch = _epoch;
    loading = true;
    error = null;
    notifyListeners();
    try {
      final uri = Uri.parse(
        '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/public-collections/${reference.kind}/${reference.id}',
      );
      final r = await _client
          .get(uri, headers: {if (_token != null) 'Authorization': _token!})
          .timeout(const Duration(seconds: 12));
      if (!current || epoch != _epoch) return;
      if (r.statusCode != 200) throw const FormatException();
      final j =
          (jsonDecode(utf8.decode(r.bodyBytes)) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      record = PublicCollectionRecord.fromJson(j, reference);
      _expiry?.cancel();
      _expiry = Timer(record!.expiresAt.difference(DateTime.now().toUtc()), () {
        if (!_closed) {
          record = null;
          error = '公开来源已到期，请重新读取。';
          notifyListeners();
        }
      });
    } catch (_) {
      if (current && epoch == _epoch) {
        record = null;
        error = '当前分享不可查看，可能已撤回或来源失效。';
      }
    } finally {
      if (current && epoch == _epoch) {
        loading = false;
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    _epoch++;
    _expiry?.cancel();
    identityChanges?.removeListener(_changed);
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
