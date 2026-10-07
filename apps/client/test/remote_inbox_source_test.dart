import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';

void main() {
  test(
    'Business claim decision uses distinct current private management target',
    () {
      final row = <String, dynamic>{
        'id': '11111111-1111-4111-8111-111111111111',
        'category': 'updates',
        'title': '商家经营权审核状态已更新',
        'detail': '请查看当前审核状态。',
        'resourceType': 'business_claim_review',
        'resourceId': '22222222-2222-4222-8222-222222222222',
        'targetBusinessId': '33333333-3333-4333-8333-333333333333',
        'semanticCategory': 'BUSINESS',
        'notificationRoute': 'NORMAL',
        'createdAt': '2026-10-04T04:00:00Z',
      };
      final item = InboxItem.fromJson(row);
      expect(item.targetBusinessID, row['targetBusinessId']);
      expect(item.targetBusinessID, isNot(item.resourceID));
      for (final bad in [
        {...row, 'targetBusinessId': null},
        {...row, 'targetBusinessId': 'invalid'},
        {...row, 'semanticCategory': 'ACTIVITY'},
        {...row, 'targetActivityId': row['targetBusinessId']},
        {...row, 'targetTaskId': row['targetBusinessId']},
        {...row, 'targetCommunityId': row['targetBusinessId']},
        {...row, 'targetConversationId': row['targetBusinessId']},
        {...row, 'resourceType': 'agent_task'},
        {...row, 'notificationRoute': 'SILENT'},
      ]) {
        expect(() => InboxItem.fromJson(bad), throwsFormatException);
      }
    },
  );
  test(
    'native Community and Task target IDs are distinct from decision IDs',
    () {
      final data = <String, dynamic>{
        'id': '11111111-1111-4111-8111-111111111111',
        'category': 'messages',
        'title': '新动态',
        'detail': '查看当前内容',
        'resourceType': 'community_message',
        'resourceId': '22222222-2222-4222-8222-222222222222',
        'createdAt': '2026-10-01T04:00:00Z',
        'targetCommunityId': '33333333-3333-4333-8333-333333333333',
      };
      final community = InboxItem.fromJson(data);
      expect(
        community.targetCommunityID,
        '33333333-3333-4333-8333-333333333333',
      );
      expect(community.targetCommunityID, isNot(community.resourceID));
      final task = InboxItem.fromJson({
        ...data,
        'resourceType': 'agent_task',
        'targetCommunityId': null,
        'targetTaskId': '44444444-4444-4444-8444-444444444444',
      });
      expect(task.targetTaskID, '44444444-4444-4444-8444-444444444444');
      for (final change in [
        {'resourceType': 'agent_task'},
        {'targetCommunityId': 'not-an-id'},
        {'targetTaskId': '44444444-4444-4444-8444-444444444444'},
        {'targetActivityId': '44444444-4444-4444-8444-444444444444'},
      ]) {
        expect(
          () => InboxItem.fromJson({...data, ...change}),
          throwsFormatException,
        );
      }
      final legacy = InboxItem.fromJson({...data, 'targetCommunityId': null});
      expect(legacy.targetCommunityID, isNull);
    },
  );
  test('activity notifications preserve the target activity and read time', () {
    final item = InboxItem.fromJson({
      'id': '11111111-1111-4111-8111-111111111111',
      'category': 'needs_attention',
      'title': '活动已取消',
      'detail': '请查看活动详情。',
      'resourceType': 'activity_cancelled',
      'resourceId': '22222222-2222-4222-8222-222222222222',
      'targetActivityId': '33333333-3333-4333-8333-333333333333',
      'targetConversationId': '44444444-4444-4444-8444-444444444444',
      'createdAt': '2026-10-01T04:00:00Z',
      'readAt': '2026-10-01T04:01:00Z',
    });

    expect(item.targetActivityID, '33333333-3333-4333-8333-333333333333');
    expect(item.targetConversationID, '44444444-4444-4444-8444-444444444444');
    expect(item.resourceType, 'activity_cancelled');
    expect(item.readAt, DateTime.utc(2026, 10, 1, 4, 1));
  });
}
