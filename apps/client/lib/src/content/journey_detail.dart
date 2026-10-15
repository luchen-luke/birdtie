import 'dart:convert';
import 'package:crypto/crypto.dart';

const journeyDetailsSchema = 'birdtie.journey-details.v1';
const journeyTextFields = [
  'title',
  'note',
  'cityId',
  'placeId',
  'kind',
  'startDate',
  'endDate',
  'startTime',
  'endTime',
  'timeLabel',
  'stopId',
  'fromStopId',
  'toStopId',
  'transportMode',
  'serviceNumber',
  'departureName',
  'arrivalName',
  'customAddress',
  'sourceLabel',
  'sourceText',
];
const journeyDayFields = ['dayIndex', 'startDay', 'endDay'];
final _id = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);
String _b64(String v) => base64UrlEncode(utf8.encode(v)).replaceAll('=', '');
bool _date(String s) {
  if (s.isEmpty) return true;
  final t = DateTime.tryParse(s);
  return RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(s) &&
      t != null &&
      t.year > 0 &&
      t.toIso8601String().substring(0, 10) == s;
}

bool _clock(String s) =>
    s.isEmpty ||
    RegExp(r'^(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d)?$').hasMatch(s);
bool _text(Object? s, [int max = 16000]) =>
    s is String &&
    !s.contains('\u0000') &&
    utf8.decode(utf8.encode(s)) == s &&
    utf8.encode(s).length <= max;

class JourneyEntry {
  JourneyEntry._(this.values, this.origin);
  final Map<String, dynamic> values;
  final Map<String, dynamic>? origin;
  String get id => values['id'];
  String get type => values['entryType'];
  String text(String key) => values[key] as String;
  int? day(String key) => values[key] as int?;
  factory JourneyEntry.decode(
    Map<String, dynamic> v,
    int position, {
    bool input = false,
  }) {
    final keys = {
      'id',
      'entryType',
      'position',
      ...journeyTextFields,
      ...journeyDayFields,
      if (!input) 'origin',
    };
    if (v.keys.any((k) => !keys.contains(k)) ||
        !_id.hasMatch(v['id'] as String? ?? '') ||
        !const {'STOP', 'ARRANGEMENT', 'NOTE'}.contains(v['entryType']) ||
        v['position'] != position ||
        journeyTextFields.any((k) => !_text(v[k])) ||
        journeyDayFields.any(
          (k) => v[k] != null && (v[k] is! int || v[k] < 0 || v[k] > 36600),
        ))
      throw const FormatException('旅程安排格式不完整');
    if (!_text(v['title'], 1000) ||
        !_text(v['cityId'], 80) ||
        !_text(v['kind'], 80) ||
        !['startDate', 'endDate'].every((k) => _date(v[k])) ||
        !['startTime', 'endTime'].every((k) => _clock(v[k])) ||
        v['startDate'] != '' &&
            v['endDate'] != '' &&
            (v['endDate'] as String).compareTo(v['startDate']) < 0 ||
        v['startDay'] != null &&
            v['endDay'] != null &&
            v['endDay'] < v['startDay'] ||
        [
          'placeId',
          'stopId',
          'fromStopId',
          'toStopId',
        ].any((k) => v[k] != '' && !_id.hasMatch(v[k])))
      throw const FormatException('旅程日期或位置无效');
    final value = Map<String, dynamic>.from(v)..remove('origin');
    return JourneyEntry._(
      Map.unmodifiable(value),
      v['origin'] == null
          ? null
          : Map.unmodifiable(v['origin'] as Map<String, dynamic>),
    );
  }
  Map<String, dynamic> toInput() => {
    'id': id,
    'entryType': type,
    'position': values['position'],
    for (final key in journeyTextFields) key: values[key],
    for (final key in journeyDayFields) key: values[key],
  };
  JourneyEntry changed(Map<String, dynamic> changes) => JourneyEntry.decode({
    ...values,
    ...changes,
    if (origin != null) 'origin': origin,
  }, changes['position'] as int? ?? values['position']);
  List<Object?> get tuple => [
    id,
    type,
    values['position'],
    _b64(text('title')),
    _b64(text('note')),
    _b64(text('cityId')),
    text('placeId'),
    _b64(text('kind')),
    day('dayIndex'),
    day('startDay'),
    day('endDay'),
    for (final k in [
      'startDate',
      'endDate',
      'startTime',
      'endTime',
      'timeLabel',
    ])
      _b64(text(k)),
    text('stopId'),
    text('fromStopId'),
    text('toStopId'),
    for (final k in [
      'transportMode',
      'serviceNumber',
      'departureName',
      'arrivalName',
      'customAddress',
      'sourceLabel',
      'sourceText',
    ])
      _b64(text(k)),
  ];
}

class JourneyDetails {
  JourneyDetails._(
    this.startDate,
    this.endDate,
    this.dateLabel,
    this.days,
    this.itinerary,
  );
  factory JourneyDetails.empty() => JourneyDetails._('', '', '', 0, const []);
  final String startDate, endDate, dateLabel;
  final int days;
  final List<JourneyEntry> itinerary;
  factory JourneyDetails.decode(Object? raw, {bool input = false}) {
    if (raw is! Map<String, dynamic> ||
        raw.keys.toSet().difference({
          'schemaVersion',
          'startDate',
          'endDate',
          'dateLabel',
          'days',
          'itinerary',
          if (!input) 'sourceVersion',
        }).isNotEmpty ||
        raw['schemaVersion'] != journeyDetailsSchema ||
        raw['startDate'] is! String ||
        raw['endDate'] is! String ||
        !_text(raw['dateLabel'], 1000) ||
        !_date(raw['startDate']) ||
        !_date(raw['endDate']) ||
        raw['days'] is! int ||
        raw['days'] < 0 ||
        raw['days'] > 36600 ||
        raw['itinerary'] is! List ||
        (raw['itinerary'] as List).length > 500 ||
        raw['startDate'] != '' &&
            raw['endDate'] != '' &&
            (raw['endDate'] as String).compareTo(raw['startDate']) < 0)
      throw const FormatException('旅程明细格式不完整');
    if (raw.containsKey('sourceVersion') &&
        (raw['sourceVersion'] is! String ||
            !RegExp(r'^[0-9a-f]{64}$').hasMatch(raw['sourceVersion']))) {
      throw const FormatException('旅程读取版本无法核对');
    }
    final rows = raw['itinerary'] as List;
    final entries = [
      for (var i = 0; i < rows.length; i++)
        JourneyEntry.decode(
          rows[i] as Map<String, dynamic>,
          i + 1,
          input: input,
        ),
    ];
    final ids = {for (final e in entries) e.id: e.type};
    if (ids.length != entries.length ||
        entries.any(
          (e) => ['stopId', 'fromStopId', 'toStopId'].any(
            (k) =>
                e.text(k).isNotEmpty &&
                (e.type != 'ARRANGEMENT' ||
                    e.text(k) == e.id ||
                    ids[e.text(k)] != 'STOP'),
          ),
        ))
      throw const FormatException('旅程停留点关联无效');
    return JourneyDetails._(
      raw['startDate'],
      raw['endDate'],
      raw['dateLabel'],
      raw['days'],
      List.unmodifiable(entries),
    );
  }
  Map<String, dynamic> toInput() => {
    'schemaVersion': journeyDetailsSchema,
    'startDate': startDate,
    'endDate': endDate,
    'dateLabel': dateLabel,
    'days': days,
    'itinerary': itinerary.map((e) => e.toInput()).toList(),
  };
  String get digest => sha256
      .convert(
        utf8.encode(
          jsonEncode([
            journeyDetailsSchema,
            _b64(startDate),
            _b64(endDate),
            _b64(dateLabel),
            days,
            itinerary.map((e) => e.tuple).toList(),
          ]),
        ),
      )
      .toString();
  JourneyDetails changed({
    String? startDate,
    String? endDate,
    String? dateLabel,
    int? days,
    List<JourneyEntry>? itinerary,
  }) {
    final rows = itinerary ?? this.itinerary;
    return JourneyDetails.decode({
      ...toInput(),
      'startDate': startDate ?? this.startDate,
      'endDate': endDate ?? this.endDate,
      'dateLabel': dateLabel ?? this.dateLabel,
      'days': days ?? this.days,
      'itinerary': [
        for (var i = 0; i < rows.length; i++)
          {
            ...rows[i].toInput(),
            'position': i + 1,
            if (rows[i].origin != null) 'origin': rows[i].origin,
          },
      ],
    });
  }
}
