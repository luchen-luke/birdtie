/// Each source has its own approved facts. Raw server explanations are never
/// used as display text, and codes cannot transfer authority between sources.
enum OpportunityReasonScheme { activityPlaceV2, newPeopleV1 }

const opportunityReasonFallback = '依据当前有权查看的信息进行基础规则匹配';

const _activityReasons = <String, String>{
  'TIE_ORGANIZER': '主办者与你已有好友关系',
  'JOINED_COMMUNITY': '活动来自你已加入的社群',
  'FOLLOWED_ORGANIZER': '活动来自你关注的主办者',
  'ORGANIZATION_ACTIVITY': '这是组织主办的活动，不代表组织身份已经核验',
  'INTENT_CATEGORY': '活动类别与你保存的意图一致',
  'INTENT_PLACE': '地点与你明确指定的地点一致',
  'PLACE_TEXT_AREA': '审核地点名称或地址包含你填写的区域文字，不代表距离',
  'INTENT_CITY_CONTEXT': '活动在你为意图明确选择的城市范围内',
  'DECLARED_CITY_CONTEXT': '活动在你主动选择的城市情境内，不代表居住地',
};

const _newPeopleReasons = <String, String>{
  'CATEGORY_EQUAL': '双方填写的类别相同',
  'MODALITY_EQUAL': '双方选择的参与方式相同',
  'PLACE_EQUAL': '双方明确选择了同一公开地点',
  'CITY_AREA_EQUAL': '双方选择的城市与粗区域相同，不代表距离或所在地',
  'ONLINE_PLATFORM_EQUAL': '双方填写的线上平台相同',
  'PARTICIPANT_RANGE_COMPATIBLE': '已填写的人数范围没有冲突',
};

/// Shows at most three approved facts, with actual route relationships first.
/// Unknown codes are omitted; an entirely unknown result stays neutral.
List<String> projectOpportunityReasons(
  Iterable<String> codes, {
  required OpportunityReasonScheme scheme,
}) {
  final allowed = switch (scheme) {
    OpportunityReasonScheme.activityPlaceV2 => _activityReasons,
    OpportunityReasonScheme.newPeopleV1 => _newPeopleReasons,
  };
  final present = codes.toSet();
  final result = <String>[
    for (final entry in allowed.entries)
      if (present.contains(entry.key)) entry.value,
  ];
  return List.unmodifiable(
    result.isEmpty ? [opportunityReasonFallback] : result.take(3),
  );
}
