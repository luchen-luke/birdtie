import 'dart:convert';

const externalPlanMaxBytes = 6144;
const externalPlanMaxDays = 14;
const externalPlanMaxStops = 50;

String _text(Object? raw, String field, int limit) {
  if (raw is! String ||
      raw.contains('\u0000') ||
      utf8.encode(raw).length > limit) {
    throw FormatException('$field须为长度合适的文字');
  }
  return raw;
}

Map<String, dynamic> _object(Object? raw, String field) {
  if (raw is! Map<String, dynamic>) throw FormatException('$field须为对象');
  return raw;
}

void _closed(Map<String, dynamic> json, Set<String> keys) {
  if (json.length != keys.length || json.keys.any((k) => !keys.contains(k))) {
    throw const FormatException('导入材料含未知持久字段');
  }
}

/// Supplied text only. An input place ID is never a canonical place selection.
class ExternalPlanStopInput {
  ExternalPlanStopInput({
    this.name = '',
    this.time = '',
    this.placeID = '',
    this.note = '',
  }) {
    _text(name, '输入地点名称', 320);
    _text(time, '输入时间', 80);
    _text(placeID, '输入地点标识', 160);
    _text(note, '输入备注', 2000);
  }
  final String name, time, placeID, note;
  ExternalPlanStopInput edit({
    String? name,
    String? time,
    String? placeID,
    String? note,
  }) => ExternalPlanStopInput(
    name: name ?? this.name,
    time: time ?? this.time,
    placeID: placeID ?? this.placeID,
    note: note ?? this.note,
  );
  Map<String, dynamic> toJson() => {
    'name': name,
    'time': time,
    'placeId': placeID,
    'note': note,
  };
  factory ExternalPlanStopInput.fromJson(Object? raw) {
    final j = _object(raw, '输入地点');
    _closed(j, {'name', 'time', 'placeId', 'note'});
    return ExternalPlanStopInput(
      name: _text(j['name'], '输入地点名称', 320),
      time: _text(j['time'], '输入时间', 80),
      placeID: _text(j['placeId'], '输入地点标识', 160),
      note: _text(j['note'], '输入备注', 2000),
    );
  }
}

class ExternalPlanDayInput {
  ExternalPlanDayInput({
    this.date = '',
    List<ExternalPlanStopInput> stops = const [],
  }) : stops = List.unmodifiable(stops) {
    _text(date, '输入日期', 80);
    if (stops.length > externalPlanMaxStops)
      throw const FormatException('导入地点条目过多');
  }
  final String date;
  final List<ExternalPlanStopInput> stops;
  ExternalPlanDayInput edit({
    String? date,
    List<ExternalPlanStopInput>? stops,
  }) =>
      ExternalPlanDayInput(date: date ?? this.date, stops: stops ?? this.stops);
  Map<String, dynamic> toJson() => {
    'date': date,
    'stops': stops.map((s) => s.toJson()).toList(),
  };
  factory ExternalPlanDayInput.fromJson(Object? raw) {
    final j = _object(raw, '输入日信息');
    _closed(j, {'date', 'stops'});
    if (j['stops'] is! List ||
        (j['stops'] as List).length > externalPlanMaxStops) {
      throw const FormatException('输入地点列表无效');
    }
    return ExternalPlanDayInput(
      date: _text(j['date'], '输入日期', 80),
      stops: (j['stops'] as List).map(ExternalPlanStopInput.fromJson).toList(),
    );
  }
}

class ExternalPlanImportedDay {
  const ExternalPlanImportedDay({
    required this.title,
    required this.text,
    required this.input,
  });
  final String title, text;
  final ExternalPlanDayInput input;
}

/// This parser cannot access a network, reader, map, writer or model. All
/// dates/times remain input text without a timezone or evidence inference.
class ExternalPlanImport {
  ExternalPlanImport._(
    this.raw,
    this.title,
    List<ExternalPlanImportedDay> days,
    List<String> unknownFields,
  ) : days = List.unmodifiable(days),
      unknownFields = List.unmodifiable(unknownFields);
  final String raw, title;
  final List<ExternalPlanImportedDay> days;
  final List<String> unknownFields;

  static ExternalPlanImport parse(String raw) {
    _text(raw, '计划原文', externalPlanMaxBytes);
    final j = _object(_StrictPlanJSON(raw).decode(), '计划JSON');
    _rejectCredentialFields(j);
    final unknown = <String>[];
    void fields(Map<String, dynamic> value, Set<String> known, String path) {
      for (final key in value.keys.where((key) => !known.contains(key))) {
        final label = '$path.$key';
        if (unknown.length >= 32 ||
            utf8.encode(label).length > 200 ||
            key.contains(RegExp(r'[\x00-\x1f\x7f]'))) {
          throw const FormatException('未识别字段过多或名称过长，请简化计划');
        }
        unknown.add('未识别字段：$label（值保留在原文中）');
      }
    }

    fields(j, {'title', 'days'}, '计划');
    if (j['days'] is! List ||
        (j['days'] as List).isEmpty ||
        (j['days'] as List).length > externalPlanMaxDays) {
      throw const FormatException('计划须包含1–14天，不会截断多余天数');
    }
    final title = j.containsKey('title')
        ? _text(j['title'], '计划名称', 320)
        : '导入计划草稿';
    var total = 0;
    final days = <ExternalPlanImportedDay>[];
    for (var n = 0; n < (j['days'] as List).length; n++) {
      final day = _object((j['days'] as List)[n], '第${n + 1}天');
      fields(day, {'title', 'date', 'text', 'stops'}, '第${n + 1}天');
      final label = day.containsKey('title')
          ? _text(day['title'], '日名称', 160)
          : '第${n + 1}天';
      if (label.trim().isEmpty) throw const FormatException('日名称不能为空');
      final date = day.containsKey('date')
          ? _text(day['date'], '输入日期', 80)
          : '';
      final text = day.containsKey('text')
          ? _text(day['text'], '当天文字', 6144)
          : '';
      final list = day.containsKey('stops') ? day['stops'] : const [];
      if (list is! List ||
          list.length > externalPlanMaxStops ||
          (total += list.length) > externalPlanMaxStops) {
        throw const FormatException('计划最多50个输入地点，不会截断多余条目');
      }
      final stops = <ExternalPlanStopInput>[];
      for (var s = 0; s < list.length; s++) {
        final stop = _object(list[s], '输入地点');
        fields(stop, {
          'name',
          'time',
          'placeId',
          'note',
        }, '第${n + 1}天.地点${s + 1}');
        String optional(String key, String label, int max) =>
            stop.containsKey(key) ? _text(stop[key], label, max) : '';
        stops.add(
          ExternalPlanStopInput(
            name: optional('name', '输入地点名称', 320),
            time: optional('time', '输入时间', 80),
            placeID: optional('placeId', '输入地点标识', 160),
            note: optional('note', '输入备注', 2000),
          ),
        );
      }
      days.add(
        ExternalPlanImportedDay(
          title: label,
          text: text,
          input: ExternalPlanDayInput(date: date, stops: stops),
        ),
      );
    }
    return ExternalPlanImport._(raw, title, days, unknown);
  }
}

void _rejectCredentialFields(Object? value) {
  if (value is Map<String, dynamic>) {
    for (final entry in value.entries) {
      if (const {
        'token',
        'password',
        'authorization',
        'authorizationheader',
        'accesstoken',
        'refreshtoken',
        'sessiontoken',
        'clientsecret',
        'access_token',
        'refresh_token',
        'session_token',
        'client_secret',
        'authorization_header',
      }.contains(entry.key.toLowerCase())) {
        throw const FormatException('计划含明确凭据字段，请删除令牌或密码后再导入');
      }
      _rejectCredentialFields(entry.value);
    }
  } else if (value is List) {
    for (final item in value) _rejectCredentialFields(item);
  }
}

/// Standard JSON values with duplicate object keys and excessive nesting
/// rejected. jsonDecode is used only for bounded scalar lexical validation.
class _StrictPlanJSON {
  _StrictPlanJSON(this.raw);
  final String raw;
  int at = 0;
  Never _invalid() => throw const FormatException('计划JSON格式无效、字段重复或层级过深');
  void _space() {
    while (at < raw.length && ' \t\r\n'.contains(raw[at])) at++;
  }

  Object? decode() {
    final value = _value(0);
    _space();
    if (at != raw.length) _invalid();
    return value;
  }

  String _string() {
    final start = at++;
    while (at < raw.length) {
      final ch = raw[at++];
      if (ch == '"') {
        try {
          return jsonDecode(raw.substring(start, at)) as String;
        } on FormatException {
          _invalid();
        }
      }
      if (ch == '\\') at++;
    }
    _invalid();
  }

  Object? _value(int depth) {
    _space();
    if (depth > 16 || at >= raw.length) _invalid();
    final first = raw[at];
    if (first == '"') return _string();
    if (first == '{') {
      at++;
      final object = <String, dynamic>{};
      _space();
      if (at < raw.length && raw[at] == '}') {
        at++;
        return object;
      }
      while (true) {
        _space();
        if (at >= raw.length || raw[at] != '"') _invalid();
        final key = _string();
        if (object.containsKey(key)) _invalid();
        _space();
        if (at >= raw.length || raw[at++] != ':') _invalid();
        object[key] = _value(depth + 1);
        _space();
        if (at >= raw.length) _invalid();
        final delimiter = raw[at++];
        if (delimiter == '}') return object;
        if (delimiter != ',') _invalid();
      }
    }
    if (first == '[') {
      at++;
      final array = <dynamic>[];
      _space();
      if (at < raw.length && raw[at] == ']') {
        at++;
        return array;
      }
      while (true) {
        array.add(_value(depth + 1));
        _space();
        if (at >= raw.length) _invalid();
        final delimiter = raw[at++];
        if (delimiter == ']') return array;
        if (delimiter != ',') _invalid();
      }
    }
    final start = at;
    while (at < raw.length && !' \t\r\n,]}'.contains(raw[at])) at++;
    if (at == start) _invalid();
    try {
      return jsonDecode(raw.substring(start, at));
    } on FormatException {
      _invalid();
    }
  }
}
