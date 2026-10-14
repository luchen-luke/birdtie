import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../auth/session_vault.dart';
import '../config/birdtie_environment.dart';

const legacyArchiveSourceSystem = 'hangzhou-civu-20261014';
const legacyArchiveMaxVersion = 8999999999999999;
const legacyArchiveResponseMaxBytes = 8 * 1024 * 1024;
final _id = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
final _hash = RegExp(r'^[0-9a-f]{64}$');
bool legacyArchiveID(Object? value) => value is String && _id.hasMatch(value);
Never _invalid() => throw const FormatException('旧档案资料无法完整核对，请重新读取。');
Map<String, dynamic> _object(
  Object? raw,
  Set<String> required, {
  Set<String> optional = const {},
}) {
  if (raw is! Map<String, dynamic> ||
      !required.every(raw.containsKey) ||
      raw.keys.any((k) => !required.contains(k) && !optional.contains(k)))
    _invalid();
  return raw;
}

String _text(Object? raw) {
  if (raw is! String ||
      raw.contains('\u0000') ||
      utf8.decode(utf8.encode(raw)) != raw)
    _invalid();
  return raw;
}

String? _optionalText(Object? raw) => raw == null ? null : _text(raw);
int _integer(Object? raw, {int min = 0}) {
  if (raw is! int || raw < min || raw > legacyArchiveMaxVersion) _invalid();
  return raw;
}

bool _boolean(Object? raw) {
  if (raw is! bool) _invalid();
  return raw;
}

String _uuid(Object? raw) {
  if (!legacyArchiveID(raw)) _invalid();
  return raw as String;
}

String? _optionalUUID(Object? raw) => raw == null ? null : _uuid(raw);
num? _historicalNumber(Object? raw) {
  if (raw == null) return null;
  if (raw is! num || !raw.isFinite) _invalid();
  return raw;
}

List<T> _array<T>(Object? raw, T Function(Object?) decode) {
  if (raw is! List) _invalid();
  return List<T>.unmodifiable(raw.map(decode));
}

DateTime _importTime(Object? raw) {
  final text = _text(raw);
  final parts = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$',
  ).firstMatch(text);
  if (parts == null) _invalid();
  final year = int.parse(parts[1]!),
      month = int.parse(parts[2]!),
      day = int.parse(parts[3]!);
  final date = DateTime.utc(year, month, day);
  if (year < 1 ||
      date.year != year ||
      date.month != month ||
      date.day != day ||
      int.parse(parts[4]!) > 23 ||
      int.parse(parts[5]!) > 59 ||
      int.parse(parts[6]!) > 59)
    _invalid();
  final result = DateTime.tryParse(text);
  if (result == null) _invalid();
  final zone = parts[7]!;
  if (zone != 'Z' &&
      (int.parse(zone.substring(1, 3)) > 23 ||
          int.parse(zone.substring(4, 6)) > 59))
    _invalid();
  if (result.toUtc() == DateTime.utc(1)) _invalid();
  return result.toUtc();
}

const _summaryKeys = {
  'id',
  'source',
  'sourceVersion',
  'sourceSha256',
  'deleted',
  'title',
  'recordedStartDate',
  'recordedEndDate',
  'sourceVisibility',
  'sourceModerationStatus',
  'importedAt',
};

class LegacyArchiveSummary {
  LegacyArchiveSummary._(Map<String, dynamic> m)
    : id = _uuid(m['id']),
      sourceID = _uuid(_source(m)['id']),
      sourceVersion = _integer(m['sourceVersion'], min: 1),
      sourceSHA256 = _text(m['sourceSha256']),
      deleted = _boolean(m['deleted']),
      title = _text(m['title']),
      recordedStartDate = _optionalText(m['recordedStartDate']),
      recordedEndDate = _optionalText(m['recordedEndDate']),
      sourceVisibility = _text(m['sourceVisibility']),
      sourceModerationStatus = _text(m['sourceModerationStatus']),
      importedAtRaw = _text(m['importedAt']),
      importedAt = _importTime(m['importedAt']) {
    if (!_hash.hasMatch(sourceSHA256) ||
        deleted &&
            (title.isNotEmpty ||
                recordedStartDate != null ||
                recordedEndDate != null ||
                sourceVisibility.isNotEmpty ||
                sourceModerationStatus.isNotEmpty))
      _invalid();
  }
  static Map<String, dynamic> _source(Map<String, dynamic> m) {
    final s = _object(m['source'], {'system', 'kind', 'id'});
    if (s['system'] != legacyArchiveSourceSystem || s['kind'] != 'note')
      _invalid();
    return s;
  }

  factory LegacyArchiveSummary.decode(Object? raw) =>
      LegacyArchiveSummary._(_object(raw, _summaryKeys));
  final String id,
      sourceID,
      sourceSHA256,
      title,
      sourceVisibility,
      sourceModerationStatus,
      importedAtRaw;
  final int sourceVersion;
  final bool deleted;
  final String? recordedStartDate, recordedEndDate;
  final DateTime importedAt;
  String get displayTitle => deleted
      ? '原记录已删除'
      : title.isEmpty
      ? '未命名的旧记录'
      : title;
  bool sameSource(LegacyArchiveSummary other) =>
      id == other.id &&
      sourceID == other.sourceID &&
      sourceVersion == other.sourceVersion &&
      sourceSHA256 == other.sourceSHA256 &&
      deleted == other.deleted &&
      title == other.title &&
      recordedStartDate == other.recordedStartDate &&
      recordedEndDate == other.recordedEndDate &&
      sourceVisibility == other.sourceVisibility &&
      sourceModerationStatus == other.sourceModerationStatus &&
      importedAtRaw == other.importedAtRaw;
}

class LegacyArchivePlace {
  LegacyArchivePlace._(Map<String, dynamic> m)
    : sourcePlaceID = _optionalUUID(m['sourcePlaceId']),
      rawName = _text(m['rawName']),
      position = _integer(m['position']);
  factory LegacyArchivePlace.decode(Object? raw) => LegacyArchivePlace._(
    _object(raw, {'sourcePlaceId', 'rawName', 'position'}),
  );
  final String? sourcePlaceID;
  final String rawName;
  final int position;
}

class LegacyArchiveMedia {
  LegacyArchiveMedia._(Map<String, dynamic> m)
    : sourceMediaID = _uuid(m['sourceMediaId']),
      position = _integer(m['position']) {
    if (m['state'] != 'UNAVAILABLE') _invalid();
  }
  factory LegacyArchiveMedia.decode(Object? raw) => LegacyArchiveMedia._(
    _object(raw, {'sourceMediaId', 'position', 'state'}),
  );
  final String sourceMediaID;
  final int position;
}

class LegacyArchiveMediaLocation {
  LegacyArchiveMediaLocation._(Map<String, dynamic> m)
    : sourceMediaID = _uuid(m['sourceMediaId']),
      latitude = _historicalNumber(m['latitude']),
      longitude = _historicalNumber(m['longitude']),
      mapEnabled = _boolean(m['mapEnabled']),
      sourcePlaceIDs = _array(m['sourcePlaceIds'], _uuid),
      capturedDate = _optionalText(m['capturedDate']);
  factory LegacyArchiveMediaLocation.decode(Object? raw) =>
      LegacyArchiveMediaLocation._(
        _object(raw, {
          'sourceMediaId',
          'latitude',
          'longitude',
          'mapEnabled',
          'sourcePlaceIds',
          'capturedDate',
        }),
      );
  final String sourceMediaID;
  final num? latitude, longitude;
  final bool mapEnabled;
  final List<String> sourcePlaceIDs;
  final String? capturedDate;
}

class LegacyArchiveThread {
  LegacyArchiveThread._(Map<String, dynamic> m)
    : sourceThreadID = _uuid(m['sourceThreadId']),
      position = m['position'] == null ? null : _integer(m['position']),
      entryCount = _integer(m['entryCount']),
      routeRevision = _integer(m['routeRevision']),
      createdAt = _text(m['createdAt']);
  factory LegacyArchiveThread.decode(Object? raw) => LegacyArchiveThread._(
    _object(raw, {
      'sourceThreadId',
      'position',
      'entryCount',
      'routeRevision',
      'createdAt',
    }),
  );
  final String sourceThreadID, createdAt;
  final int? position;
  final int entryCount, routeRevision;
}

class LegacyArchiveThreadLeg {
  LegacyArchiveThreadLeg._(Map<String, dynamic> m)
    : sourceFromNoteID = _uuid(m['sourceFromNoteId']),
      sourceToNoteID = _uuid(m['sourceToNoteId']),
      fromPosition = _integer(m['fromPosition']),
      toPosition = _integer(m['toPosition']),
      mode = _text(m['mode']),
      sourceFromAnchorPlaceID = _optionalUUID(m['sourceFromAnchorPlaceId']),
      sourceToAnchorPlaceID = _optionalUUID(m['sourceToAnchorPlaceId']),
      createdAt = _text(m['createdAt']),
      updatedAt = _text(m['updatedAt']);
  factory LegacyArchiveThreadLeg.decode(Object? raw) =>
      LegacyArchiveThreadLeg._(
        _object(raw, {
          'sourceFromNoteId',
          'sourceToNoteId',
          'fromPosition',
          'toPosition',
          'mode',
          'sourceFromAnchorPlaceId',
          'sourceToAnchorPlaceId',
          'createdAt',
          'updatedAt',
        }),
      );
  final String sourceFromNoteID, sourceToNoteID, mode, createdAt, updatedAt;
  final String? sourceFromAnchorPlaceID, sourceToAnchorPlaceID;
  final int fromPosition, toPosition;
}

class LegacyArchiveRelationships {
  LegacyArchiveRelationships._(Map<String, dynamic> m)
    : places = _array(m['places'], LegacyArchivePlace.decode),
      media = _array(m['media'], LegacyArchiveMedia.decode),
      mediaLocations = _array(
        m['mediaLocations'],
        LegacyArchiveMediaLocation.decode,
      ),
      thread = m['thread'] == null
          ? null
          : LegacyArchiveThread.decode(m['thread']),
      threadLegs = _array(m['threadLegs'], LegacyArchiveThreadLeg.decode) {
    if (places.map((p) => p.position).toSet().length != places.length ||
        media.map((p) => p.position).toSet().length != media.length ||
        mediaLocations.map((p) => p.sourceMediaID).toSet().length !=
            mediaLocations.length ||
        threadLegs.map((p) => p.fromPosition).toSet().length !=
            threadLegs.length ||
        thread == null && threadLegs.isNotEmpty)
      _invalid();
  }
  factory LegacyArchiveRelationships.decode(Object? raw) =>
      LegacyArchiveRelationships._(
        _object(raw, {
          'places',
          'media',
          'mediaLocations',
          'thread',
          'threadLegs',
        }),
      );
  final List<LegacyArchivePlace> places;
  final List<LegacyArchiveMedia> media;
  final List<LegacyArchiveMediaLocation> mediaLocations;
  final LegacyArchiveThread? thread;
  final List<LegacyArchiveThreadLeg> threadLegs;
}

class LegacyArchiveDetail {
  LegacyArchiveDetail._(this.summary, this.body, this.relationships);
  factory LegacyArchiveDetail.decode(Object? raw) {
    final m = _object(
      raw,
      {..._summaryKeys, 'body'},
      optional: {'relationships'},
    );
    final summary = LegacyArchiveSummary._(m), body = _text(m['body']);
    if (summary.deleted) {
      if (body.isNotEmpty || m.containsKey('relationships')) _invalid();
      return LegacyArchiveDetail._(summary, body, null);
    }
    return LegacyArchiveDetail._(
      summary,
      body,
      LegacyArchiveRelationships.decode(m['relationships']),
    );
  }
  final LegacyArchiveSummary summary;
  final String body;
  final LegacyArchiveRelationships? relationships;
}

class LegacyArchivePageData {
  LegacyArchivePageData._(this.items, this.nextCursor);
  factory LegacyArchivePageData.decode(Object? raw) {
    final m = _object(raw, {'items'}, optional: {'nextCursor'});
    final items = _array(m['items'], LegacyArchiveSummary.decode);
    final next = m.containsKey('nextCursor') ? _text(m['nextCursor']) : null;
    if (items.length > 20 ||
        items.map((p) => p.id).toSet().length != items.length ||
        next != null && (!legacyArchiveCursor(next) || items.isEmpty))
      _invalid();
    return LegacyArchivePageData._(items, next);
  }
  final List<LegacyArchiveSummary> items;
  final String? nextCursor;
}

bool legacyArchiveCursor(String cursor) =>
    RegExp(r'^[A-Za-z0-9_-]{1,1024}$').hasMatch(cursor);

/// Duplicate keys are not silently discarded before closed typed validation.
/// Size is bounded; text itself is never truncated.
Object? legacyArchiveJSON(String text) {
  if (utf8.encode(text).length > legacyArchiveResponseMaxBytes) _invalid();
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
    _invalid();
  }

  void Function(int) value = (_) {};
  value = (int depth) {
    if (depth > 64) _invalid();
    space();
    if (i >= text.length) _invalid();
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
        if (i >= text.length || text[i] != '"' || !keys.add(string()))
          _invalid();
        space();
        if (i >= text.length || text[i++] != ':') _invalid();
        value(depth + 1);
        space();
        if (i >= text.length) _invalid();
        final end = text[i++];
        if (end == '}') return;
        if (end != ',') _invalid();
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
        if (i >= text.length) _invalid();
        final end = text[i++];
        if (end == ']') return;
        if (end != ',') _invalid();
      }
    }
    if (text[i] == '"') {
      string();
      return;
    }
    final start = i;
    while (i < text.length && !' \r\n\t,}]'.contains(text[i])) i++;
    if (start == i) _invalid();
    jsonDecode(text.substring(start, i));
  };
  value(0);
  space();
  if (i != text.length) _invalid();
  return jsonDecode(text);
}

class LegacyArchiveFailure implements Exception {
  const LegacyArchiveFailure(this.status);
  final int status;
  bool get permission => status == 401 || status == 403;
  @override
  String toString() => permission
      ? '请在有效的个人身份下查看本人的旧内容档案。'
      : status == 404
      ? '这条旧档案当前不可读取。'
      : status == 400 || status == 409
      ? '档案来源已有变化，请重新读取列表。'
      : '旧档案暂时无法读取，请稍后重试。';
}

class LegacyArchiveSourceChanged implements Exception {}

class LegacyArchiveAPI {
  LegacyArchiveAPI({
    required this.authorizationHeader,
    required this.ownerID,
    this.organizationWorkspaceID,
    this.current,
    this.sourceKey,
    this.identityChanges,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       origin = canonicalAuthOrigin(
         apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       ) {
    _scope = _namespace;
    _header = authorizationHeader();
    identityChanges?.addListener(_observe);
  }
  final String? Function() authorizationHeader, ownerID;
  final String? Function()? organizationWorkspaceID;
  final bool Function()? current;
  final Object? Function()? sourceKey;
  final Listenable? identityChanges;
  final http.Client _client;
  final bool _ownsClient;
  final String origin;
  Object? _scope;
  String? _header;
  int _epoch = 0;
  bool _closed = false, _retired = false;
  Object get _namespace =>
      (ownerID(), organizationWorkspaceID?.call(), sourceKey?.call());
  void _observe() {
    if (_closed || _retired) return;
    if (current?.call() == false || _namespace != _scope) {
      _retired = true;
      ++_epoch;
    } else if (_header != authorizationHeader()) {
      _header = authorizationHeader();
      ++_epoch;
    }
  }

  Future<Object?> _get(String path, {Map<String, String>? query}) async {
    _observe();
    if (_closed || _retired || current?.call() == false)
      throw LegacyArchiveSourceChanged();
    if (origin.isEmpty ||
        !legacyArchiveID(ownerID()) ||
        authorizationHeader() == null ||
        organizationWorkspaceID?.call() != null)
      throw const LegacyArchiveFailure(403);
    final epoch = _epoch, owner = ownerID(), header = authorizationHeader()!;
    bool live() {
      _observe();
      return !_closed &&
          !_retired &&
          _epoch == epoch &&
          ownerID() == owner &&
          authorizationHeader() == header &&
          organizationWorkspaceID?.call() == null &&
          current?.call() != false;
    }

    var uri = Uri.parse(origin).replace(path: path);
    if (query != null && query.isNotEmpty)
      uri = uri.replace(queryParameters: query);
    final request = http.Request('GET', uri)
      ..followRedirects = false
      ..headers.addAll({'Authorization': header, 'Accept': 'application/json'});
    final response = await _client
        .send(request)
        .timeout(const Duration(seconds: 12));
    if (!live()) throw LegacyArchiveSourceChanged();
    if (response.statusCode != 200)
      throw LegacyArchiveFailure(response.statusCode);
    if (response.headers['content-type']?.split(';').first !=
        'application/json')
      _invalid();
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (!live()) throw LegacyArchiveSourceChanged();
      if (chunk.length > legacyArchiveResponseMaxBytes - bytes.length)
        _invalid();
      bytes.add(chunk);
    }
    if (!live()) throw LegacyArchiveSourceChanged();
    return _object(legacyArchiveJSON(utf8.decode(bytes.takeBytes())), {
      'data',
    })['data'];
  }

  Future<LegacyArchivePageData> page({String? cursor, String? kind}) async {
    if (cursor != null && !legacyArchiveCursor(cursor) ||
        kind != null && kind != 'note')
      _invalid();
    final result = LegacyArchivePageData.decode(
      await _get(
        '/v1/me/migration-archive',
        query: {
          if (cursor != null) 'cursor': cursor,
          if (kind != null) 'kind': kind,
        },
      ),
    );
    if (cursor != null && result.nextCursor == cursor) _invalid();
    return result;
  }

  Future<LegacyArchiveDetail> detail(
    String id, {
    LegacyArchiveSummary? expected,
  }) async {
    if (!legacyArchiveID(id) || expected != null && expected.id != id)
      _invalid();
    final result = LegacyArchiveDetail.decode(
      await _get('/v1/me/migration-archive/' + id),
    );
    if (result.summary.id != id ||
        expected != null && !result.summary.sameSource(expected))
      _invalid();
    return result;
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    ++_epoch;
    identityChanges?.removeListener(_observe);
    if (_ownsClient) _client.close();
  }
}
