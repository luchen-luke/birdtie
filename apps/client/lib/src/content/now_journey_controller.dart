import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'personal_collection_controller.dart';
import 'collection_publication_controller.dart';

/// The original collection controllers retain private edits and operation IDs.
/// Discovery carries an independent, finite public read; it never grants edits.
class NowJourneyController extends ChangeNotifier {
  NowJourneyController({
    required this.authorizationHeader,
    required this.ownerID,
    required this.currentScope,
    required this.client,
    required this.apiBaseUrl,
    required this.cityID,
    this.identityChanges,
  }) {
    _owner = ownerID();
    _token = authorizationHeader();
    mine = _collection(cityID);
    mine.addListener(_changed);
    identityChanges?.addListener(_boundary);
  }
  final String? Function() authorizationHeader, ownerID;
  final bool Function() currentScope;
  final http.Client client;
  final String apiBaseUrl, cityID;
  final Listenable? identityChanges;
  late final String? _owner, _token;
  late final PersonalCollectionController mine;
  final _editors = <String, PersonalCollectionController>{};
  PersonalCollectionController? selectedMine;
  PublicCollectionRecord? selectedPublic;
  List<PublicCollectionRecord> visible = const [];
  String? nextCursor, error;
  bool loading = false, opening = false, _closed = false, _retired = false;
  int _reads = 0, _opens = 0;
  Timer? _expiry;
  DateTime? _validUntil;
  bool get current {
    if (!_closed &&
        !_retired &&
        (!currentScope() ||
            _owner == null ||
            _token == null ||
            ownerID() != _owner ||
            authorizationHeader() != _token)) {
      _retired = true;
      ++_reads;
      ++_opens;
      _expiry?.cancel();
      visible = const [];
      selectedPublic = null;
      selectedMine = null;
      nextCursor = null;
    }
    return !_closed && !_retired;
  }

  PersonalCollectionController _collection(String city) =>
      PersonalCollectionController(
        kind: 'journey',
        cityID: city,
        ownerID: ownerID,
        authorizationHeader: authorizationHeader,
        sourceCurrent: () => current,
        identityChanges: identityChanges,
        client: client,
        apiBaseUrl: apiBaseUrl,
      );
  void _changed() {
    if (!_closed) notifyListeners();
  }

  void _boundary() {
    if (!current) {
      _retired = true;
      ++_reads;
      ++_opens;
      _expiry?.cancel();
      visible = const [];
      selectedPublic = null;
      selectedMine = null;
      nextCursor = null;
      error = '账号或工作身份已变化，请重新打开旅程。';
    }
    _changed();
  }

  Future<void> load() => mine.load();
  void back() {
    ++_opens;
    opening = false;
    selectedPublic = null;
    selectedMine = null;
    error = null;
    _changed();
  }

  Future<void> openMine(PersonalCollection record) async {
    if (!current ||
        opening ||
        record.ownerID != _owner ||
        record.kind != 'journey')
      return;
    final serial = ++_opens;
    opening = true;
    error = null;
    _changed();
    final c = _editors.putIfAbsent(record.id, () {
      final v = _collection(record.cityID);
      v.addListener(_changed);
      return v;
    });
    final ok = c.editing?.id == record.id && c.current || await c.open(record);
    if (!current || serial != _opens) return;
    opening = false;
    if (ok) {
      selectedPublic = null;
      selectedMine = c;
    } else {
      error = c.error ?? '旅程暂时无法读取，请重试。';
    }
    _changed();
  }

  Uri _uri(String path, [Map<String, String>? query]) => Uri.parse(
    '${apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path',
  ).replace(queryParameters: query);
  Map<String, String> get _headers => {'Authorization': _token!};
  Future<void> loadVisible({bool more = false}) async {
    if (!current || loading || more && nextCursor == null) return;
    final serial = ++_reads, cursor = more ? nextCursor : null;
    loading = true;
    error = null;
    _changed();
    try {
      final response = await client
          .get(
            _uri('/v1/me/journeys/discovery', {
              if (cursor != null) 'cursor': cursor,
            }),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!current || serial != _reads) return;
      if (response.statusCode != 200) throw const FormatException();
      final data =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final now = DateTime.now().toUtc(),
          observed = DateTime.parse(data['observedAt'] as String).toUtc(),
          until = DateTime.parse(data['validUntil'] as String).toUtc();
      final rows = data['items'] as List, after = data['nextCursor'] as String?;
      if (data['schemaVersion'] != 'journey-discovery-v1' ||
          data['viewerAccountId'] != _owner ||
          observed.isAfter(now.add(const Duration(seconds: 1))) ||
          !until.isAfter(now) ||
          until.isAfter(observed.add(const Duration(seconds: 61))) ||
          !RegExp(
            r'^[0-9a-f]{64}$',
          ).hasMatch(data['sourceVersion'] as String? ?? '') ||
          rows.length > 20 ||
          after != null &&
              (!RegExp(
                    r'^jd1\.[A-Za-z0-9_-]{1,208}\.[A-Za-z0-9_-]{43}$',
                  ).hasMatch(after) ||
                  after == cursor))
        throw const FormatException();
      final page = [
        for (final raw in rows)
          PublicCollectionRecord.fromJson(
            raw as Map<String, dynamic>,
            CollectionShareReference('journey', raw['id'] as String),
          ),
      ];
      if (page.map((v) => v.reference.id).toSet().length != page.length ||
          page.any((v) => v.expiresAt.isAfter(until)))
        throw const FormatException();
      visible = List.unmodifiable(
        {
          if (more)
            for (final v in visible)
              if (now.isBefore(v.expiresAt)) v.reference.id: v,
          for (final v in page) v.reference.id: v,
        }.values,
      );
      nextCursor = after;
      _validUntil = until;
      _scheduleExpiry();
    } catch (_) {
      if (current && serial == _reads) error = '无法读取可见旅程，仍有效的内容已保留，请重试。';
    } finally {
      if (current && serial == _reads) {
        loading = false;
        _changed();
      }
    }
  }

  Future<void> openPublic(PublicCollectionRecord record) async {
    if (!current ||
        opening ||
        !DateTime.now().toUtc().isBefore(record.expiresAt))
      return;
    final serial = ++_opens;
    opening = true;
    error = null;
    _changed();
    try {
      final response = await client
          .get(
            _uri('/v1/public-collections/journey/${record.reference.id}'),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!current || serial != _opens) return;
      if (response.statusCode != 200) throw const FormatException();
      final value = PublicCollectionRecord.fromJson(
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
        record.reference,
      );
      selectedMine = null;
      selectedPublic = value;
      _scheduleExpiry();
    } catch (_) {
      if (current && serial == _opens) error = '这份公开旅程已变化或暂不可读，请重新加载。';
    } finally {
      if (current && serial == _opens) {
        opening = false;
        _changed();
      }
    }
  }

  bool publicCurrent(PublicCollectionRecord record) =>
      current &&
      identical(selectedPublic, record) &&
      DateTime.now().toUtc().isBefore(record.expiresAt);
  void _scheduleExpiry() {
    _expiry?.cancel();
    final times = [
      if (_validUntil != null) _validUntil!,
      if (selectedPublic != null) selectedPublic!.expiresAt,
      for (final v in visible) v.expiresAt,
    ];
    if (times.isEmpty) return;
    times.sort();
    _expiry = Timer(times.first.difference(DateTime.now().toUtc()), () {
      if (!current) return;
      final now = DateTime.now().toUtc();
      visible = List.unmodifiable(
        visible.where((v) => now.isBefore(v.expiresAt)),
      );
      if (selectedPublic != null && !now.isBefore(selectedPublic!.expiresAt)) {
        selectedPublic = null;
        error = '公开旅程来源已到期，请重新读取。';
      }
      if (_validUntil != null && !now.isBefore(_validUntil!)) {
        _validUntil = null;
        nextCursor = null;
      }
      _scheduleExpiry();
      _changed();
    });
  }

  @override
  void dispose() {
    _closed = true;
    ++_reads;
    ++_opens;
    _expiry?.cancel();
    identityChanges?.removeListener(_boundary);
    mine.removeListener(_changed);
    mine.dispose();
    for (final c in _editors.values) {
      c.removeListener(_changed);
      c.dispose();
    }
    super.dispose();
  }
}

/// Edits fill the sole Now composer; applying changes only the original local draft.
class NowJourneyFieldEdit {
  const NowJourneyFieldEdit({
    required this.journeyID,
    required this.fieldID,
    required this.label,
    required this.value,
    required this.current,
    required this.apply,
  });
  final String journeyID, fieldID, label, value;
  final bool Function() current;
  final bool Function(String) apply;
}
