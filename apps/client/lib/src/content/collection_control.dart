import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;
import '../auth/session_vault.dart';
import 'journey_detail.dart';

const collectionControlSchema = 'human-personal-collection-control-v1';
const collectionControlMaxRevision = 9000000000000000;
bool collectionControlID(Object? v) =>
    v is String &&
    RegExp(
      r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
    ).hasMatch(v);
bool collectionControlKind(Object? v) => v == 'journey' || v == 'theme_map';
bool collectionControlVersion(Object? v) =>
    v is int && v >= 1 && v <= collectionControlMaxRevision;
bool collectionControlHash(Object? v) =>
    v is String && RegExp(r'^[0-9a-f]{64}$').hasMatch(v);
Never _bad() => throw const FormatException('集合操作资料无法完整核对。');
Map<String, dynamic> collectionControlObject(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      !keys.every(raw.containsKey))
    _bad();
  return raw;
}

String _text(Object? v, int min, int max, {bool trim = true}) {
  if (v is! String || v.contains('\u0000') || utf8.decode(utf8.encode(v)) != v)
    _bad();
  final value = trim ? v.trim() : v;
  if (utf8.encode(value).length < min || utf8.encode(value).length > max)
    _bad();
  return value;
}

String _digest(Object value) =>
    sha256.convert(utf8.encode(jsonEncode(value))).toString();
String _b64(String value) =>
    base64UrlEncode(utf8.encode(value)).replaceAll('=', '');
DateTime _time(Object? value) {
  if (value is! String ||
      !RegExp(
        r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$',
      ).hasMatch(value))
    _bad();
  final time = DateTime.tryParse(value);
  if (time == null) _bad();
  if (int.parse(value.substring(11, 13)) > 23 ||
      int.parse(value.substring(14, 16)) > 59 ||
      int.parse(value.substring(17, 19)) > 59)
    _bad();
  final date = value.substring(0, 10);
  if (time.year < 1 ||
      time.year > 9999 ||
      time.toIso8601String().substring(0, 10) != date ||
      time == DateTime.utc(1))
    _bad();
  return time.toUtc();
}

String newCollectionControlOperationID() {
  final random = Random.secure(),
      b = List<int>.generate(16, (_) => random.nextInt(256));
  b[6] = b[6] & 15 | 64;
  b[8] = b[8] & 63 | 128;
  final s = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
  return s.substring(0, 8) +
      '-' +
      s.substring(8, 12) +
      '-' +
      s.substring(12, 16) +
      '-' +
      s.substring(16, 20) +
      '-' +
      s.substring(20);
}

class CollectionControlEntry {
  const CollectionControlEntry(
    this.placeID,
    this.note,
    this.locationVisibility,
  );
  final String placeID, note, locationVisibility;
  factory CollectionControlEntry.normalize(Object? raw, {bool strict = false}) {
    final m = collectionControlObject(raw, {
      'placeId',
      'note',
      'locationVisibility',
    });
    final id = _text(m['placeId'], 36, 36).toLowerCase(),
        note = _text(m['note'], 0, 2000);
    if (!collectionControlID(id) ||
        !const {'hidden', 'city', 'place'}.contains(m['locationVisibility']))
      _bad();
    if (strict && (id != m['placeId'] || note != m['note'])) _bad();
    return CollectionControlEntry(id, note, m['locationVisibility']);
  }
  Map<String, Object> toJson() => {
    'placeId': placeID,
    'note': note,
    'locationVisibility': locationVisibility,
  };
  List<String> get tuple => [placeID, _b64(note), locationVisibility];
}

class CollectionControlBody {
  CollectionControlBody._(
    this.kind,
    this.cityID,
    this.title,
    this.summary,
    this.entries,
    this.journeyDetails,
  );
  final String kind, cityID, title, summary;
  final List<CollectionControlEntry> entries;
  final JourneyDetails? journeyDetails;
  factory CollectionControlBody.normalize(Object? raw, {bool strict = false}) {
    final m = collectionControlObject(raw, {
      'kind',
      'cityId',
      'title',
      'summary',
      'entries',
      if (raw is Map && raw.containsKey('journeyDetails')) 'journeyDetails',
    });
    final details = m.containsKey('journeyDetails')
        ? JourneyDetails.decode(m['journeyDetails'], input: true)
        : null;
    if (details != null && m['kind'] != 'journey') _bad();
    if (!collectionControlKind(m['kind']) || m['entries'] is! List) _bad();
    final city = _text(m['cityId'], details == null ? 1 : 0, 80),
        title = _text(m['title'], details == null ? 1 : 0, 160),
        summary = _text(m['summary'], 0, 5000);
    if (city.contains(RegExp(r'[\r\n\t]'))) _bad();
    if (strict &&
        (city != m['cityId'] || title != m['title'] || summary != m['summary']))
      _bad();
    final entries = (m['entries'] as List)
        .map((v) => CollectionControlEntry.normalize(v, strict: strict))
        .toList();
    if ((details == null ? entries.isEmpty : entries.isNotEmpty) ||
        entries.length > 50 ||
        entries.map((e) => e.placeID).toSet().length != entries.length)
      _bad();
    return CollectionControlBody._(
      m['kind'],
      city,
      title,
      summary,
      List.unmodifiable(entries),
      details,
    );
  }
  Map<String, Object> toJson() => {
    'kind': kind,
    'cityId': cityID,
    'title': title,
    'summary': summary,
    'entries': entries.map((e) => e.toJson()).toList(growable: false),
    if (journeyDetails != null) 'journeyDetails': journeyDetails!.toInput(),
  };
  List<Object> get tuple => [
    kind,
    _b64(cityID),
    _b64(title),
    _b64(summary),
    entries.map((e) => e.tuple).toList(growable: false),
    if (journeyDetails != null) [journeyDetailsSchema, journeyDetails!.digest],
  ];
}

class CollectionControlInput {
  CollectionControlInput({
    required this.operationID,
    required this.action,
    required this.kind,
    required this.collectionID,
    required this.expectedRevision,
    this.input,
  }) {
    if (!valid) _bad();
    if (utf8.encode(jsonEncode(toJson())).length > 128 * 1024) _bad();
  }
  final String operationID, action, kind, collectionID;
  final int expectedRevision;
  final CollectionControlBody? input;
  bool get valid =>
      collectionControlID(operationID) &&
      collectionControlID(collectionID) &&
      collectionControlKind(kind) &&
      collectionControlVersion(expectedRevision) &&
      (action == 'UPDATE'
          ? input != null &&
                input!.kind == kind &&
                expectedRevision < collectionControlMaxRevision
          : action == 'WITHDRAW' && input == null);
  String get payloadDigest => _digest([
    collectionControlSchema,
    action,
    kind,
    collectionID,
    expectedRevision,
    input?.tuple,
  ]);
  Map<String, Object> toJson() => {
    'schemaVersion': collectionControlSchema,
    'operationId': operationID,
    'action': action,
    'kind': kind,
    'collectionId': collectionID,
    'expectedRevision': expectedRevision,
    if (input != null) 'input': input!.toJson(),
  };
}

class CollectionControlPreview {
  CollectionControlPreview._(
    this.input,
    this.observedAt,
    this.expiresAt,
    this.approval,
  );
  final CollectionControlInput input;
  final DateTime observedAt, expiresAt;
  final String approval;
  bool get live => DateTime.now().toUtc().isBefore(expiresAt);
  Map<String, Object> get body => {...input.toJson(), 'approval': approval};
  factory CollectionControlPreview.decode(
    Object? raw,
    CollectionControlInput expected,
  ) {
    final m = collectionControlObject(raw, {
      'schemaVersion',
      'operationId',
      'action',
      'kind',
      'collectionId',
      'expectedRevision',
      'payloadDigest',
      'observedAt',
      'expiresAt',
      'approval',
      if (expected.action == 'UPDATE') 'normalizedInput',
    });
    if (m['schemaVersion'] != collectionControlSchema ||
        m['operationId'] != expected.operationID ||
        m['action'] != expected.action ||
        m['kind'] != expected.kind ||
        m['collectionId'] != expected.collectionID ||
        m['expectedRevision'] != expected.expectedRevision ||
        m['payloadDigest'] != expected.payloadDigest)
      _bad();
    final echo = expected.action == 'UPDATE'
        ? CollectionControlBody.normalize(m['normalizedInput'], strict: true)
        : null;
    final input = CollectionControlInput(
      operationID: expected.operationID,
      action: expected.action,
      kind: expected.kind,
      collectionID: expected.collectionID,
      expectedRevision: expected.expectedRevision,
      input: echo,
    );
    if (input.payloadDigest != expected.payloadDigest) _bad();
    final approval = _text(m['approval'], 1, 8192, trim: false),
        at = _time(m['observedAt']),
        until = _time(m['expiresAt']);
    if (approval != approval.trim() || approval.contains(RegExp(r'[\r\n\t]')))
      _bad();
    final now = DateTime.now().toUtc();
    if (utf8
            .encode(jsonEncode({...input.toJson(), 'approval': approval}))
            .length >
        128 * 1024)
      _bad();
    if (at.isAfter(now.add(const Duration(seconds: 2))) ||
        !until.isAfter(now) ||
        !until.isAfter(at) ||
        until.isAfter(at.add(const Duration(seconds: 30))) ||
        until.isAfter(now.add(const Duration(seconds: 32))))
      _bad();
    return CollectionControlPreview._(input, at, until, approval);
  }
}

class CollectionControlReceipt {
  const CollectionControlReceipt._(
    this.operationID,
    this.status, {
    this.action,
    this.kind,
    this.collectionID,
    this.expectedRevision,
    this.appliedRevision,
    this.payloadDigest,
  });
  final String operationID, status;
  final String? action, kind, collectionID, payloadDigest;
  final int? expectedRevision, appliedRevision;
  bool get succeeded => status == 'SUCCEEDED';
  factory CollectionControlReceipt.decode(
    Object? raw, {
    required String operationID,
    String? action,
    String? kind,
    String? collectionID,
    int? expectedRevision,
    String? payloadDigest,
  }) {
    if (raw is! Map<String, dynamic>) _bad();
    final stopped = raw['status'] == 'STOPPED';
    final m = collectionControlObject(
      raw,
      stopped
          ? {'schemaVersion', 'operationId', 'status'}
          : {
              'schemaVersion',
              'operationId',
              'status',
              'action',
              'kind',
              'collectionId',
              'expectedRevision',
              'appliedRevision',
              'payloadDigest',
              'committedAt',
            },
    );
    if (m['schemaVersion'] != collectionControlSchema ||
        m['operationId'] != operationID ||
        !collectionControlID(operationID))
      _bad();
    if (stopped) return CollectionControlReceipt._(operationID, 'STOPPED');
    final a = m['action'], revision = m['expectedRevision'];
    if (m['status'] != 'SUCCEEDED' ||
        !const {'UPDATE', 'WITHDRAW'}.contains(a) ||
        !collectionControlKind(m['kind']) ||
        !collectionControlID(m['collectionId']) ||
        !collectionControlVersion(revision) ||
        !collectionControlVersion(m['appliedRevision']) ||
        a == 'UPDATE' && revision == collectionControlMaxRevision ||
        !collectionControlHash(m['payloadDigest']) ||
        action != null && action != a ||
        kind != null && kind != m['kind'] ||
        collectionID != null && collectionID != m['collectionId'] ||
        expectedRevision != null && expectedRevision != revision ||
        payloadDigest != null && payloadDigest != m['payloadDigest'])
      _bad();
    final want = a == 'WITHDRAW' && revision == collectionControlMaxRevision
        ? revision
        : revision + 1;
    if (m['appliedRevision'] != want) _bad();
    _time(m['committedAt']);
    return CollectionControlReceipt._(
      operationID,
      'SUCCEEDED',
      action: a,
      kind: m['kind'],
      collectionID: m['collectionId'],
      expectedRevision: revision,
      appliedRevision: want,
      payloadDigest: m['payloadDigest'],
    );
  }
}

/// Repeated JSON keys must not silently become a different confirmed input.
Object? collectionControlJSON(String text) {
  var i = 0;
  void space() {
    while (i < text.length && ' \r\n\t'.contains(text[i])) i++;
  }

  String string() {
    final start = i++;
    while (i < text.length) {
      if (text[i] == '\\') {
        i += 2;
        continue;
      }
      if (text[i++] == '"')
        return jsonDecode(text.substring(start, i)) as String;
    }
    _bad();
  }

  void Function(int) value = (_) {};
  value = (depth) {
    if (depth > 64) _bad();
    space();
    if (i >= text.length) _bad();
    if (text[i] == '{') {
      i++;
      space();
      final keys = <String>{};
      if (i < text.length && text[i] == '}') {
        i++;
        return;
      }
      while (true) {
        space();
        if (i >= text.length || text[i] != '"' || !keys.add(string())) _bad();
        space();
        if (i >= text.length || text[i++] != ':') _bad();
        value(depth + 1);
        space();
        if (i >= text.length) _bad();
        final end = text[i++];
        if (end == '}') return;
        if (end != ',') _bad();
      }
    }
    if (text[i] == '[') {
      i++;
      space();
      if (i < text.length && text[i] == ']') {
        i++;
        return;
      }
      while (true) {
        value(depth + 1);
        space();
        if (i >= text.length) _bad();
        final end = text[i++];
        if (end == ']') return;
        if (end != ',') _bad();
      }
    }
    if (text[i] == '"') {
      string();
      return;
    }
    final start = i;
    while (i < text.length && !' \r\n\t,}]'.contains(text[i])) i++;
    if (start == i) _bad();
    jsonDecode(text.substring(start, i));
  };
  value(0);
  space();
  if (i != text.length) _bad();
  return jsonDecode(text);
}

class CollectionControlFailure implements Exception {
  const CollectionControlFailure(this.status);
  final int status;
}

class CollectionControlAPI {
  CollectionControlAPI({
    required this.client,
    required String apiBaseUrl,
    required this.authorizationHeader,
  }) : origin = canonicalAuthOrigin(apiBaseUrl);
  final http.Client client;
  final String origin;
  final String? Function() authorizationHeader;
  Future<Object?> _json(
    String method,
    String path,
    bool Function() current, {
    Map<String, Object>? body,
  }) async {
    final header = authorizationHeader();
    if (header == null || origin.isEmpty || !current())
      throw const CollectionControlFailure(401);
    final request = http.Request(method, Uri.parse(origin).replace(path: path))
      ..followRedirects = false
      ..headers.addAll({'Authorization': header, 'Accept': 'application/json'});
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }
    final response = await client
        .send(request)
        .timeout(const Duration(seconds: 12));
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (!current() || header != authorizationHeader())
        throw const CollectionControlFailure(401);
      if (chunk.length > 1024 * 1024 - bytes.length) _bad();
      bytes.add(chunk);
    }
    if (!current() || header != authorizationHeader())
      throw const CollectionControlFailure(401);
    if (response.statusCode != 200)
      throw CollectionControlFailure(response.statusCode);
    if (response.headers['content-type']?.split(';').first.toLowerCase() !=
        'application/json')
      _bad();
    return collectionControlObject(
      collectionControlJSON(utf8.decode(bytes.takeBytes())),
      {'data'},
    )['data'];
  }

  Future<CollectionControlPreview> preview(
    CollectionControlInput input,
    bool Function() current,
  ) async => CollectionControlPreview.decode(
    await _json(
      'POST',
      '/v1/me/personal-collection-controls/preview',
      current,
      body: input.toJson(),
    ),
    input,
  );
  Future<CollectionControlReceipt> put(
    CollectionControlPreview preview,
    bool Function() current,
  ) async => CollectionControlReceipt.decode(
    await _json(
      'PUT',
      '/v1/me/personal-collection-controls/operations/' +
          preview.input.operationID,
      current,
      body: preview.body,
    ),
    operationID: preview.input.operationID,
    action: preview.input.action,
    kind: preview.input.kind,
    collectionID: preview.input.collectionID,
    expectedRevision: preview.input.expectedRevision,
    payloadDigest: preview.input.payloadDigest,
  );
  Future<Object?> original(
    String operationID,
    bool Function() current, {
    bool stop = false,
  }) {
    if (!collectionControlID(operationID)) _bad();
    return _json(
      stop ? 'POST' : 'GET',
      '/v1/me/personal-collection-controls/operations/' +
          operationID +
          (stop ? '/stop' : ''),
      current,
    );
  }
}
