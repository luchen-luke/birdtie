import 'dart:convert';
import 'package:birdtie_client/src/workspace/social_intent_draft_model.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final now = DateTime.utc(2026, 10, 6, 12);
  const owner = '11111111-1111-4111-8111-111111111111';
  const id = '22222222-2222-4222-8222-222222222222';

  test('恢复待核实内容保持原寻找有效期和活动时间，不重设七天', () {
    final original =
        SocialIntentDraftModel(
            title: '原待核实输入',
            modality: 'HYBRID',
            area: '本人选定区域',
            now: now,
          )
          ..platform = '线上会议'
          ..startsAt = now.add(const Duration(hours: 1))
          ..endsAt = now.add(const Duration(hours: 2))
          ..expiresAt = now.add(const Duration(days: 2));
    final restored = SocialIntentDraftModel.fromPayload(
      original.payload(fromTask: false),
    );
    expect(
      restored.payload(fromTask: false),
      original.payload(fromTask: false),
    );
    expect(restored.expiresAt, now.add(const Duration(days: 2)));
    expect(restored.areaSource, isNull);
    expect(restored.timeNote, isNull);
  });

  test('未知模式保持未知，默认七天只作为可编辑寻找有效期', () {
    final d = SocialIntentDraftModel(now: now);
    expect(d.modality, isNull);
    expect(d.expiresAt, now.add(const Duration(days: 7)));
    expect(d.startsAt, isNull);
    expect(d.endsAt, isNull);
    expect(d.missing(now: now), hasLength(2));
  });
  test('整理仅复用原句事实，不换算周末或猜参与方式和坐标', () {
    const text = '这周末想在阿伯丁打羽毛球，先帮我记下来';
    final d = SocialIntentDraftModel(title: text, now: now)..organize();
    expect(d.title, text);
    expect(d.category, 'badminton');
    expect(d.area, '阿伯丁');
    expect(d.areaSource, contains('当前输入'));
    expect(d.modality, isNull);
    expect(d.timeNote, '这周末');
    expect(d.startsAt, isNull);
    expect(d.endsAt, isNull);
  });
  for (final text in [
    '线上讨论',
    '线下见面',
    '线上和线下都可以',
    '不想线上，也不确定线下地点',
    '不要把线上默认成我的选择',
  ]) {
    test('关键词不冒充已确认参与方式：$text', () {
      final d = SocialIntentDraftModel(title: text, now: now)..organize();
      expect(d.modality, isNull);
    });
  }
  test('已有明确选择整理后保留，不由关键词覆盖', () {
    final d = SocialIntentDraftModel(
      title: '线上和线下讨论',
      modality: 'ONLINE',
      now: now,
    )..organize();
    expect(d.modality, 'ONLINE');
  });
  test('完整编辑往返和模式切换保留同一草稿，只提交当前适用字段', () {
    final d = SocialIntentDraftModel(
      title: '讨论',
      area: '用户选定区域',
      category: '球类',
      modality: 'HYBRID',
      now: now,
    )..platform = '视频会议';
    d.modality = 'ONLINE';
    expect(d.payload(fromTask: false)['constraints'], {
      'category': '球类',
      'onlinePlatform': '视频会议',
    });
    expect(d.area, '用户选定区域');
    d.modality = 'IN_PERSON';
    expect(d.payload(fromTask: false)['constraints'], {
      'category': '球类',
      'areaLabel': '用户选定区域',
    });
    expect(d.platform, '视频会议');
  });
  test('线上不索取区域，混合只补当前必要区域和线上方式', () {
    final d = SocialIntentDraftModel(title: '讨论', modality: 'ONLINE', now: now);
    expect(d.missing(now: now), isEmpty);
    d.modality = 'HYBRID';
    expect(d.missing(now: now), hasLength(2));
    d.area = '市中心';
    d.platform = '视频通话';
    expect(d.missing(now: now), isEmpty);
  });
  test('寻找有效期和真实活动时间分别校验，不捏造结束时间', () {
    final d = SocialIntentDraftModel(title: '讨论', modality: 'ONLINE', now: now);
    d.expiresAt = now;
    expect(d.missing(now: now), contains('寻找有效期应在一分钟后至90天内。'));
    d.expiresAt = now.add(const Duration(days: 91));
    expect(d.missing(now: now), isNotEmpty);
    d.expiresAt = now.add(const Duration(days: 8));
    d.startsAt = now.add(const Duration(days: 3));
    expect(d.missing(now: now), contains('请选择完整活动时间，或清除活动时间。'));
    d.endsAt = d.startsAt!.add(const Duration(hours: 2));
    expect(d.missing(now: now), isEmpty);
    final body = d.payload(fromTask: true);
    expect(body['audience'], 'PRIVATE');
    expect(body['type'], 'FIND_ACTIVITY');
    expect(body.containsKey('cityId'), isFalse);
    expect(
      (body['constraints'] as Map)['startsAt'],
      d.startsAt!.toIso8601String(),
    );
  });
  test('合法201绑定真实稳定ID、本人、PRIVATE及DRAFT', () {
    final receipt = SocialIntentDraftReceipt.decode(
      jsonEncode({
        'data': {
          'id': id,
          'creatorAccountId': owner,
          'audience': 'PRIVATE',
          'status': 'DRAFT',
          'title': '讨论',
        },
      }),
      owner,
      {'title': '讨论'},
    );
    expect(receipt.id, id);
    expect(receipt.ownerID, owner);
  });
  for (final bad in [
    {'id': 'draft'},
    {'creatorAccountId': id},
    {'audience': 'PUBLIC'},
    {'status': 'ACTIVE'},
    {'title': '不同内容'},
  ]) {
    test('拒绝假201回执：${bad.keys.single}', () {
      final data = {
        'id': id,
        'creatorAccountId': owner,
        'audience': 'PRIVATE',
        'status': 'DRAFT',
        'title': '讨论',
        ...bad,
      };
      expect(
        () => SocialIntentDraftReceipt.decode(
          jsonEncode({'data': data}),
          owner,
          {'title': '讨论'},
        ),
        throwsFormatException,
      );
    });
  }
  test('缺少本人或状态不能把任意201称为真实保存', () {
    expect(
      () => SocialIntentDraftReceipt.decode('{"data":{"id":"$id"}}', owner, {}),
      throwsFormatException,
    );
  });
}
