import 'dart:convert';

import 'package:flutter/foundation.dart';

/// Local creation material. This object carries no approval or server receipt.
@immutable
class NowCreationDraft {
  const NowCreationDraft({
    required this.kind,
    required this.cityID,
    required this.cityName,
    this.title = '',
    this.body = '',
    this.placeID = '',
    this.placeName = '',
    this.orderedPlaceIDs = const [],
    this.startsAt,
    this.endsAt,
  });

  final String kind, cityID, cityName, title, body, placeID, placeName;
  final DateTime? startsAt, endsAt;
  final List<String> orderedPlaceIDs;

  String get label => switch (kind) {
    'moment' => '动态',
    'activity' => '活动',
    'journey' => '旅程',
    'theme_map' || 'theme-map' || 'collection' => '主题地图',
    _ => '内容',
  };

  /// Only existing user material is returned to the shared composer.
  String get initialText => body.isNotEmpty ? body : title;

  String get inputHint => switch (kind) {
    'moment' => '写下这条动态的内容…',
    'activity' => '说说你想组织的活动…',
    'journey' => '写下你的旅程和沿途内容…',
    'theme_map' || 'theme-map' || 'collection' => '说说这张主题地图要收集什么…',
    _ => '写下你想创建的内容…',
  };

  NowCreationDraft copyWith({
    String? title,
    String? body,
    String? placeID,
    String? placeName,
    DateTime? startsAt,
    DateTime? endsAt,
    List<String>? orderedPlaceIDs,
  }) => NowCreationDraft(
    kind: kind,
    cityID: cityID,
    cityName: cityName,
    title: title ?? this.title,
    body: body ?? this.body,
    placeID: placeID ?? this.placeID,
    placeName: placeName ?? this.placeName,
    startsAt: startsAt ?? this.startsAt,
    endsAt: endsAt ?? this.endsAt,
    orderedPlaceIDs: orderedPlaceIDs == null
        ? this.orderedPlaceIDs
        : List.unmodifiable(orderedPlaceIDs),
  );

  Map<String, dynamic> toJson() => {
    'schemaVersion': 'birdtie-now-creation-material-v1',
    'kind': kind,
    'cityId': cityID,
    'cityName': cityName,
    'title': title,
    'body': body,
    'placeId': placeID,
    'placeName': placeName,
    'startsAt': startsAt?.toUtc().toIso8601String(),
    'endsAt': endsAt?.toUtc().toIso8601String(),
    'orderedPlaceIds': orderedPlaceIDs,
  };

  factory NowCreationDraft.fromJson(Map<String, dynamic> json) {
    const keys = {
      'schemaVersion',
      'kind',
      'cityId',
      'cityName',
      'title',
      'body',
      'placeId',
      'placeName',
      'startsAt',
      'endsAt',
      'orderedPlaceIds',
    };
    if (json.length != keys.length ||
        json.keys.toSet().difference(keys).isNotEmpty ||
        json['schemaVersion'] != 'birdtie-now-creation-material-v1' ||
        !const {
          'moment',
          'activity',
          'journey',
          'theme_map',
        }.contains(json['kind'])) {
      throw const FormatException('创建草稿格式无效');
    }
    String text(String key, int maximum, {bool empty = true}) {
      final value = json[key];
      if (value is! String ||
          !empty && value.isEmpty ||
          value.contains('\u0000') ||
          utf8.encode(value).length > maximum) {
        throw const FormatException('创建草稿内容无效');
      }
      return value;
    }

    DateTime? time(String key) {
      if (json[key] == null) return null;
      final value = text(key, 64, empty: false);
      final parsed = DateTime.tryParse(value);
      if (parsed == null ||
          !parsed.isUtc ||
          parsed.toIso8601String() != value) {
        throw const FormatException('创建草稿日期无效');
      }
      return parsed;
    }

    final places = json['orderedPlaceIds'];
    if (places is! List ||
        places.length > 50 ||
        places.any(
          (id) =>
              id is! String ||
              id.isEmpty ||
              id.contains('\u0000') ||
              utf8.encode(id).length > 160,
        ) ||
        places.toSet().length != places.length) {
      throw const FormatException('创建草稿地点顺序无效');
    }
    return NowCreationDraft(
      kind: json['kind'] as String,
      cityID: text('cityId', 160, empty: false),
      cityName: text('cityName', 512),
      title: text('title', 1024),
      body: text('body', 12000),
      placeID: text('placeId', 160),
      placeName: text('placeName', 512),
      startsAt: time('startsAt'),
      endsAt: time('endsAt'),
      orderedPlaceIDs: List<String>.unmodifiable(places.cast<String>()),
    );
  }

  /// Preserve the body verbatim; derive a title only when one is still absent.
  NowCreationDraft withText(String text, {bool titleOnly = false}) {
    if (titleOnly) return copyWith(title: _firstLineTitle(text));
    return copyWith(
      title: title.trim().isEmpty ? _firstLineTitle(text) : title,
      body: text,
    );
  }

  static String _firstLineTitle(String text) {
    final firstLine = text.split(RegExp(r'\r\n|\n|\r')).first.trim();
    final result = StringBuffer();
    var characterCount = 0, byteCount = 0;
    for (final rune in firstLine.runes) {
      final character = String.fromCharCode(rune);
      final bytes = utf8.encode(character).length;
      if (characterCount == 40 || byteCount + bytes > 80) break;
      result.write(character);
      characterCount++;
      byteCount += bytes;
    }
    return result.toString();
  }
}
