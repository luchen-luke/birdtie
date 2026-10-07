import 'package:birdtie_client/src/workspace/opportunity_reasons.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const activity = OpportunityReasonScheme.activityPlaceV2;
  const people = OpportunityReasonScheme.newPeopleV1;
  const activityCodes = {
    'INTENT_CATEGORY': '活动类别与你保存的意图一致',
    'INTENT_PLACE': '地点与你明确指定的地点一致',
    'PLACE_TEXT_AREA': '审核地点名称或地址包含你填写的区域文字，不代表距离',
    'INTENT_CITY_CONTEXT': '活动在你为意图明确选择的城市范围内',
    'DECLARED_CITY_CONTEXT': '活动在你主动选择的城市情境内，不代表居住地',
    'TIE_ORGANIZER': '主办者与你已有好友关系',
    'JOINED_COMMUNITY': '活动来自你已加入的社群',
    'FOLLOWED_ORGANIZER': '活动来自你关注的主办者',
    'ORGANIZATION_ACTIVITY': '这是组织主办的活动，不代表组织身份已经核验',
  };
  for (final entry in activityCodes.entries) {
    test('活动理由 ${entry.key} 只投影允许的事实', () {
      expect(projectOpportunityReasons([entry.key], scheme: activity), [
        entry.value,
      ]);
    });
  }
  const peopleCodes = {
    'CATEGORY_EQUAL': '双方填写的类别相同',
    'MODALITY_EQUAL': '双方选择的参与方式相同',
    'PLACE_EQUAL': '双方明确选择了同一公开地点',
    'CITY_AREA_EQUAL': '双方选择的城市与粗区域相同，不代表距离或所在地',
    'ONLINE_PLATFORM_EQUAL': '双方填写的线上平台相同',
    'PARTICIPANT_RANGE_COMPATIBLE': '已填写的人数范围没有冲突',
  };
  for (final entry in peopleCodes.entries) {
    test('新朋友理由 ${entry.key} 只投影允许的事实', () {
      expect(projectOpportunityReasons([entry.key], scheme: people), [
        entry.value,
      ]);
    });
  }
  test('理由去重并限制三项，具体路由关系优先于泛类别', () {
    expect(
      projectOpportunityReasons([
        'INTENT_CATEGORY',
        'INTENT_PLACE',
        'INTENT_CITY_CONTEXT',
        'INTENT_PLACE',
        'TIE_ORGANIZER',
        'SECRET_CHAT',
      ], scheme: activity),
      ['主办者与你已有好友关系', '活动类别与你保存的意图一致', '地点与你明确指定的地点一致'],
    );
  });
  test('空、未知和跨来源代码保持中性，不推断好友兴趣或身份', () {
    expect(projectOpportunityReasons([], scheme: activity), [
      opportunityReasonFallback,
    ]);
    expect(
      projectOpportunityReasons([
        'CATEGORY_EQUAL',
        'SECRET_CHAT',
        'FRIEND_INTEREST',
      ], scheme: activity),
      [opportunityReasonFallback],
    );
    expect(
      projectOpportunityReasons([
        'TIE_ORGANIZER',
        'JOINED_COMMUNITY',
        'ORGANIZATION_ACTIVITY',
      ], scheme: people),
      [opportunityReasonFallback],
    );
    expect(
      projectOpportunityReasons([
        'CATEGORY_EQUAL',
        'TIE_ORGANIZER',
      ], scheme: people),
      ['双方填写的类别相同'],
    );
  });
}
