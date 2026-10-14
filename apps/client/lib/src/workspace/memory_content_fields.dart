import 'package:flutter/material.dart';
import 'agent_memory_candidate_api.dart' show candidateCategories;
import 'now_ui_tokens.dart';

/// Human-readable supplemental content. Unknown fields stay in the domain DTO;
/// they are neither guessed nor exposed as identifiers/diagnostic JSON.
class MemoryContentFields extends StatelessWidget {
  const MemoryContentFields({super.key, required this.values});
  final Map values;
  static const _labels = {
    'activityCategory': '活动类型',
    'activityCategories': '活动类型',
    'preferredActivityTypes': '偏好的活动',
    'cityName': '城市',
    'placeName': '地点',
    'communityName': '社群',
    'organizationName': '组织',
    'description': '补充说明',
    'statement': '本人声明',
    'summary': '补充内容',
    'text': '补充内容',
    'content': '补充内容',
    'body': '补充内容',
    'note': '备注',
    'notes': '备注',
    'availability': '空闲时间',
    'languages': '交流语言',
  };
  String? _value(String field, Object? value) {
    if (value is List) {
      final parts = value
          .map((v) => _value(field, v))
          .whereType<String>()
          .toList();
      return parts.isEmpty ? null : parts.join('、');
    }
    if (value is! String || value.trim().isEmpty) return null;
    if (const {
      'activityCategory',
      'activityCategories',
      'preferredActivityTypes',
    }.contains(field))
      return candidateCategories[value];
    if (field == 'languages') return const {'zh-CN': '简体中文', 'en': '英语'}[value];
    return value;
  }

  @override
  Widget build(BuildContext context) {
    final rows = <Widget>[];
    for (final entry in _labels.entries) {
      final value = _value(entry.key, values[entry.key]);
      if (value != null)
        rows.add(
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(
              '${entry.value}：$value',
              style: NowUiTokens.of(context).body,
            ),
          ),
        );
    }
    final unrepresented = values.entries.any(
      (entry) =>
          entry.value != null &&
          entry.value != '' &&
          !(entry.value is Iterable && (entry.value as Iterable).isEmpty) &&
          (!_labels.containsKey(entry.key) ||
              _value(entry.key as String, entry.value) == null),
    );
    if (unrepresented)
      rows.add(
        Padding(
          padding: const EdgeInsets.only(top: 8),
          child: Text(
            '部分补充信息暂无法展示，修改时会保留。',
            style: NowUiTokens.of(context).metadata,
          ),
        ),
      );
    return rows.isEmpty
        ? const SizedBox.shrink()
        : Column(crossAxisAlignment: CrossAxisAlignment.start, children: rows);
  }
}
