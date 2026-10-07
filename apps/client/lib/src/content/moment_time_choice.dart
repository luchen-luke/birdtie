import 'package:flutter/material.dart';

/// The calendar units for coarse precision are the server's UTC units. An
/// anchor such as January 1 is serialization, not a claim about the exact day.
class MomentTimeValue {
  const MomentTimeValue._(this.precision, this.occurredAt);
  static const unknown = MomentTimeValue._('unknown', null);
  static const precisions = ['unknown', 'year', 'month', 'day', 'instant'];
  final String precision;
  final DateTime? occurredAt;

  factory MomentTimeValue.fromWire(String precision, DateTime? occurredAt) {
    if (!precisions.contains(precision) ||
        (precision == 'unknown') != (occurredAt == null)) {
      throw const FormatException('invalid moment time');
    }
    final utc = occurredAt?.toUtc();
    if (utc != null && (utc.year < 1 || utc.year > 9999)) {
      throw const FormatException('invalid moment year');
    }
    return MomentTimeValue._(precision, utc);
  }

  static MomentTimeValue parse(String precision, String text) {
    if (precision == 'unknown') return unknown;
    final pattern = switch (precision) {
      'year' => r'^(\d{4})$',
      'month' => r'^(\d{4})-(\d{2})$',
      'day' => r'^(\d{4})-(\d{2})-(\d{2})$',
      'instant' =>
        r'^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,6}))?)?$',
      _ => throw const FormatException('invalid precision'),
    };
    final match = RegExp(pattern).firstMatch(text.trim());
    if (match == null) throw const FormatException('invalid time format');
    int part(int index, int fallback) =>
        index <= match.groupCount && match.group(index) != null
        ? int.parse(match.group(index)!)
        : fallback;
    final year = part(1, 0), month = part(2, 1), day = part(3, 1);
    final hour = part(4, 0), minute = part(5, 0), second = part(6, 0);
    final micro = match.groupCount >= 7 && match.group(7) != null
        ? int.parse(match.group(7)!.padRight(6, '0'))
        : 0;
    if (year < 1 ||
        year > 9999 ||
        month < 1 ||
        month > 12 ||
        day < 1 ||
        day > 31 ||
        hour > 23 ||
        minute > 59 ||
        second > 59) {
      throw const FormatException('invalid calendar time');
    }
    final value = DateTime.utc(
      year,
      month,
      day,
      hour,
      minute,
      second,
      micro ~/ 1000,
      micro % 1000,
    );
    if (value.year != year || value.month != month || value.day != day) {
      throw const FormatException('invalid calendar day');
    }
    return MomentTimeValue._(precision, value);
  }

  Map<String, dynamic> get payload => {
    'timePrecision': precision,
    if (occurredAt != null) 'occurredAt': occurredAt!.toIso8601String(),
  };
  String get formText {
    final date = occurredAt;
    if (date == null) return '';
    final iso = date.toIso8601String();
    return switch (precision) {
      'year' => iso.substring(0, 4),
      'month' => iso.substring(0, 7),
      'day' => iso.substring(0, 10),
      _ =>
        iso
            .replaceFirst('T', ' ')
            .replaceFirst(RegExp(r'0+Z$'), 'Z')
            .replaceFirst('.Z', 'Z')
            .replaceFirst('Z', ''),
    };
  }

  String get label {
    final date = occurredAt;
    if (date == null) return '发生时间未知';
    return switch (precision) {
      'year' => '${date.year}年',
      'month' => '${date.year}年${date.month}月',
      'day' => '${date.year}年${date.month}月${date.day}日',
      _ => '$formText（UTC）',
    };
  }
}

class MomentTimeChoice extends StatefulWidget {
  const MomentTimeChoice({
    super.key,
    required this.initialValue,
    required this.onChanged,
    this.enabled = true,
  });
  final MomentTimeValue initialValue;
  final ValueChanged<MomentTimeValue?> onChanged;
  final bool enabled;
  @override
  State<MomentTimeChoice> createState() => _MomentTimeChoiceState();
}

class _MomentTimeChoiceState extends State<MomentTimeChoice> {
  late String _precision;
  late final TextEditingController _text;
  @override
  void initState() {
    super.initState();
    _precision = widget.initialValue.precision;
    _text = TextEditingController(text: widget.initialValue.formText);
  }

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  String get _hint => switch (_precision) {
    'year' => '2025',
    'month' => '2025-09',
    'day' => '2025-09-12',
    _ => '2025-09-12 09:30',
  };
  String? _validate(String? value) {
    try {
      MomentTimeValue.parse(_precision, value ?? '');
      return null;
    } on FormatException {
      return '请填写有效时间，格式如 $_hint';
    }
  }

  void _change() {
    try {
      widget.onChanged(MomentTimeValue.parse(_precision, _text.text));
    } on FormatException {
      widget.onChanged(null);
    }
  }

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      const Text('发生时间', style: TextStyle(fontWeight: FontWeight.w600)),
      const SizedBox(height: 8),
      DropdownButtonFormField<String>(
        initialValue: _precision,
        isExpanded: true,
        decoration: const InputDecoration(labelText: '记得多具体'),
        items: const [
          DropdownMenuItem(value: 'unknown', child: Text('不确定')),
          DropdownMenuItem(value: 'year', child: Text('只记得年份')),
          DropdownMenuItem(value: 'month', child: Text('记得年月')),
          DropdownMenuItem(value: 'day', child: Text('记得日期')),
          DropdownMenuItem(value: 'instant', child: Text('准确时刻（UTC）')),
        ],
        onChanged: !widget.enabled
            ? null
            : (value) {
                if (value == null || value == _precision) return;
                setState(() {
                  _precision = value;
                  _text.clear();
                });
                _change();
              },
      ),
      if (_precision != 'unknown') ...[
        const SizedBox(height: 12),
        TextFormField(
          key: ValueKey('moment-time-$_precision'),
          controller: _text,
          enabled: widget.enabled,
          decoration: InputDecoration(
            labelText: _precision == 'instant' ? '时刻（UTC）' : '$_hint 格式',
            hintText: _hint,
          ),
          keyboardType: TextInputType.datetime,
          textInputAction: TextInputAction.next,
          validator: _validate,
          onChanged: (_) => _change(),
          autovalidateMode: AutovalidateMode.onUserInteraction,
        ),
      ],
      const SizedBox(height: 8),
      Text(
        _precision == 'instant'
            ? '按 UTC 填写准确时刻；不会按设备时区换算。'
            : '只记录你记得的范围；不确定时保留未知，不会用创建时间代替。',
        style: Theme.of(context).textTheme.bodySmall,
      ),
    ],
  );
}
