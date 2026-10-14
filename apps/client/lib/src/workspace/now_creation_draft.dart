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
    this.startsAt,
    this.endsAt,
  });

  final String kind, cityID, cityName, title, body, placeID, placeName;
  final DateTime? startsAt, endsAt;

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
  );

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
