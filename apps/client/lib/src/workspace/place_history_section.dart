import 'package:flutter/material.dart';
import 'place_history_controller.dart';

class PlaceHistorySection extends StatelessWidget {
  const PlaceHistorySection({
    super.key,
    required this.controller,
    this.onOpenMoment,
  });
  final PlaceHistoryController controller;
  final ValueChanged<String>? onOpenMoment;
  String _code(String value) => switch (value) {
    'badminton' => '羽毛球',
    'sports' => '运动',
    'social' => '社交',
    'culture' => '文化',
    'meeting' => '会议',
    'quiet' => '安静',
    'study' => '学习',
    'reading' => '阅读',
    _ => '其他公开类别',
  };
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) {
      final s = controller.summary, f = s?.facts;
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('这里最近的公开分享', style: Theme.of(context).textTheme.titleMedium),
          const Text('最近 30 天，由本人明确公开的记录。不是到访证明。'),
          if (controller.loading) const LinearProgressIndicator(),
          if (controller.error != null) ...[
            Semantics(liveRegion: true, child: Text(controller.error!)),
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton(
                onPressed: controller.refresh,
                child: const Text('重试公开摘要'),
              ),
            ),
          ],
          if (s != null) ...[
            if (s.count == 0)
              const Text('暂无仍公开的近期分享。')
            else
              Text('近期公开分享：${s.count} 条'),
            for (final m in s.moments)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      m.title,
                      style: Theme.of(context).textTheme.titleSmall,
                    ),
                    if (m.excerpt.isNotEmpty) Text(m.excerpt),
                    Text(
                      '公开于 ${m.publishedAt.toLocal().month}月${m.publishedAt.toLocal().day}日',
                    ),
                    if (m.id != null && onOpenMoment != null)
                      TextButton(
                        onPressed: () => onOpenMoment!(m.id!),
                        child: const Text('查看公开动态'),
                      ),
                  ],
                ),
              ),
            const SizedBox(height: 8),
            Text('近期公开活动安排', style: Theme.of(context).textTheme.titleSmall),
            if (s.patterns.isEmpty) const Text('暂无可查看的已结束公开安排。'),
            for (final p in s.patterns)
              Text(
                '${p.weekend ? '周末' : '工作日'} · ${_code(p.category)}：${p.arrangements} 项安排',
              ),
            const Text('仅表示已公布的安排，不代表实际举办、到场或人数。'),
            const SizedBox(height: 12),
            Text('场所适合度资料', style: Theme.of(context).textTheme.titleSmall),
            if (f == null)
              const Text('尚无仍有效的审核声明，各项适合度尚未核验。')
            else ...[
              for (final pair in {
                'vibe': '氛围',
                'good_for': '适合用途',
                'suitability': '活动类型',
              }.entries)
                Text(
                  '${pair.value}：${f[pair.key] is List && (f[pair.key] as List).isNotEmpty ? (f[pair.key] as List).cast<String>().map(_code).join('、') : '尚未核验'}',
                ),
              Text(
                '价格：${f['price'] is Map ? '${f['price']['currency']} ${(f['price']['minMinor'] / 100).toStringAsFixed(2)}–${(f['price']['maxMinor'] / 100).toStringAsFixed(2)}' : '尚未核验'}',
              ),
              Text(
                '无障碍：${f['accessibility'] is Map ? '无台阶通行：${switch (f['accessibility']['stepFree']) {
                        'yes' => '资料称支持',
                        'no' => '资料称不支持',
                        _ => '尚未核验',
                      }}；无障碍卫生间：${switch (f['accessibility']['accessibleToilet']) {
                        'yes' => '资料称支持',
                        'no' => '资料称不支持',
                        _ => '尚未核验',
                      }}' : '尚未核验'}',
              ),
              Text(
                '小组规模：${f['group_size'] is Map ? '${f['group_size']['min']}–${f['group_size']['max']} 人（资料建议）' : '尚未核验'}',
              ),
              Text(
                '预约：${f['reservation'] is Map ? switch (f['reservation']['support']) {
                        'contact' => '资料建议联系咨询',
                        'external_url' => '有外部预约说明',
                        _ => '资料未提供预约',
                      } : '尚未核验'}',
              ),
              Text('审核来源：${s.sourceLabel}'),
              Text(
                '编辑评估：${switch (s.confidence) {
                  'LOW' => '低',
                  'MEDIUM' => '中',
                  'HIGH' => '高',
                  _ => '未知',
                }}（不是概率或实际体验保证）',
              ),
            ],
          ],
        ],
      );
    },
  );
}
