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
  bool get canPublish =>
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
      record?.status == 'published' &&
      source.editing?.id == record?.id &&
      source.editing?.revision == record?.revision;
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
      if (action == 'PUBLISH') 'allowPrivatePlaceCopy': allowPrivatePlaceCopy,
      if (action == 'PUBLISH')
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
  );
  final CollectionShareReference reference;
  final String cityID, title, summary;
  final int revision;
  final DateTime expiresAt;
  final List<Map<String, dynamic>> entries;
  final CollectionCopyGrant? copyGrant;
  factory PublicCollectionRecord.fromJson(
    Map<String, dynamic> j,
    CollectionShareReference expected,
  ) {
    final expires = DateTime.parse(j['sourceExpiresAt'] as String).toUtc();
    final rows = (j['entries'] as List).cast<Map<String, dynamic>>();
    final now = DateTime.now().toUtc();
    if (j['id'] != expected.id ||
        j['kind'] != expected.kind ||
        (j['revision'] as int) < 1 ||
        (j['cityId'] as String).isEmpty ||
        (j['title'] as String).trim().isEmpty ||
        !now.isBefore(expires) ||
        expires.difference(now) > const Duration(seconds: 61) ||
        rows.isEmpty ||
        rows.length > 50 ||
        j.containsKey('ownerAccountId'))
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
