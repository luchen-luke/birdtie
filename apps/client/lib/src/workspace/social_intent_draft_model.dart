import 'dart:convert';

const socialIntentModes = {'IN_PERSON', 'ONLINE', 'HYBRID'};
bool socialIntentIDValid(Object? value) =>
    value is String &&
    RegExp(
      r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
    ).hasMatch(value);

/// One editable declaration before a server ID exists. No model inference,
/// location lookup, publication, anonymous persistence or authority lives here.
class SocialIntentDraftModel {
  SocialIntentDraftModel({
    String? title,
    String? category,
    String? area,
    String? modality,
    DateTime? now,
  }) : title = title ?? '',
       category = category ?? '',
       area = area ?? '',
       modality = socialIntentModes.contains(modality) ? modality : null,
       expiresAt = (now ?? DateTime.now()).toUtc().add(const Duration(days: 7));

  /// Restore only a captured, validated private pending request. This does not
  /// invent a new expiry or replay the earlier user's authorization.
  factory SocialIntentDraftModel.fromPayload(Map<String, dynamic> data) {
    final c = data['constraints'] as Map<String, dynamic>;
    final model = SocialIntentDraftModel(
      title: data['title'] as String,
      category: c['category'] as String?,
      area: c['areaLabel'] as String?,
      modality: data['modality'] as String,
    );
    model.platform = c['onlinePlatform'] as String? ?? '';
    model.expiresAt = DateTime.parse(data['expiresAt'] as String).toUtc();
    model.startsAt = c['startsAt'] == null
        ? null
        : DateTime.parse(c['startsAt'] as String).toUtc();
    model.endsAt = c['endsAt'] == null
        ? null
        : DateTime.parse(c['endsAt'] as String).toUtc();
    return model;
  }

  String title, category, area;
  String platform = '';
  String? modality;
  String? areaSource;
  String? timeNote;
  DateTime expiresAt;
  DateTime? startsAt, endsAt;

  void organize() {
    title = title.trim();
    // These are literal words in the supplied text, not invented facts.
    if (category.isEmpty && title.contains('羽毛球')) category = 'badminton';
    // Mentioning a mode can be a negation or a question. Keep unknown modes
    // unknown until the user chooses, or a caller supplies a confirmed mode.
    if (area.isEmpty) {
      final literal = RegExp(
        r'在([^，。！？,\n]{1,80}?)(?:打|参加|玩|见面|散步|一起)',
      ).firstMatch(title);
      if (literal != null) {
        area = literal.group(1)!.trim();
        areaSource = '当前输入中的大致区域，不代表定位';
      }
    }
    timeNote = RegExp(
      r'这周末|本周末|周末|今晚|明晚|今天|明天|下周|这周',
    ).firstMatch(title)?.group(0);
  }

  List<String> missing({required DateTime now}) => [
    if (title.trim().isEmpty) '请先写下想做什么。',
    if (title.trim().runes.length > 160) '想做的事不能超过160个字。',
    if (modality == null) '请选择参与方式，不会根据城市推断线上或线下。',
    if (modality != null && modality != 'ONLINE' && area.trim().isEmpty)
      '请填写大致区域；无需精确住址或定位。',
    if (modality == 'HYBRID' && platform.trim().isEmpty) '请填写线上参与方式。',
    if (category.trim().runes.length > 80 ||
        area.trim().runes.length > 160 ||
        platform.trim().runes.length > 80)
      '请缩短类别、区域或线上方式。',
    if ((startsAt == null) != (endsAt == null)) '请选择完整活动时间，或清除活动时间。',
    if (startsAt != null &&
        endsAt != null &&
        (!endsAt!.isAfter(startsAt!) ||
            endsAt!.difference(startsAt!) > const Duration(days: 90)))
      '活动结束时间应晚于开始时间，且相隔不超过90天。',
    if (!expiresAt.isAfter(now.toUtc().add(const Duration(minutes: 1))) ||
        expiresAt.isAfter(now.toUtc().add(const Duration(days: 90))))
      '寻找有效期应在一分钟后至90天内。',
  ];

  Map<String, dynamic> payload({required bool fromTask}) => {
    'type': fromTask ? 'FIND_ACTIVITY' : 'FIND_COMPANION',
    'title': title.trim(),
    'audience': 'PRIVATE',
    'modality': modality,
    'constraints': {
      if (category.trim().isNotEmpty) 'category': category.trim(),
      if (modality != 'ONLINE') 'areaLabel': area.trim(),
      if (modality != 'IN_PERSON' && platform.trim().isNotEmpty)
        'onlinePlatform': platform.trim(),
      if (startsAt != null && endsAt != null) ...{
        'startsAt': startsAt!.toUtc().toIso8601String(),
        'endsAt': endsAt!.toUtc().toIso8601String(),
      },
    },
    'expiresAt': expiresAt.toUtc().toIso8601String(),
  };
}

class SocialIntentDraftReceipt {
  SocialIntentDraftReceipt._(this.id, this.ownerID);
  final String id, ownerID;
  factory SocialIntentDraftReceipt.decode(
    String body,
    String expectedOwner,
    Map<String, dynamic> submitted,
  ) {
    final json = jsonDecode(body);
    if (json is! Map<String, dynamic> ||
        json['data'] is! Map<String, dynamic>) {
      throw const FormatException('无法核实保存回执');
    }
    final data = json['data'] as Map<String, dynamic>;
    if (!socialIntentIDValid(data['id']) ||
        !socialIntentIDValid(expectedOwner) ||
        data['creatorAccountId'] != expectedOwner ||
        data['audience'] != 'PRIVATE' ||
        data['status'] != 'DRAFT') {
      throw const FormatException('保存对象或身份无法核实');
    }
    for (final key in ['title', 'modality', 'type']) {
      if (data.containsKey(key) && data[key] != submitted[key]) {
        throw const FormatException('保存内容无法核实');
      }
    }
    return SocialIntentDraftReceipt._(data['id'] as String, expectedOwner);
  }
}
