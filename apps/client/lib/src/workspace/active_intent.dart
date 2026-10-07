import 'agent_workspace_controller.dart';

/// A short UI projection of the task's structured intent and filters.
/// The original query stays in the conversation.
class ActiveIntentSummary {
  const ActiveIntentSummary({required this.action, this.category, this.time});

  final String action;
  final String? category;
  final String? time;

  String get label => [action, if (time != null) '· $time'].join(' ');

  factory ActiveIntentSummary.fromTask(AgentTask task) {
    final intent = task.intent?.toUpperCase();
    final query = task.query.toLowerCase();
    final category =
        task.filters['category'] == 'badminton' ||
            (intent == null &&
                (query.contains('羽毛球') || query.contains('badminton')))
        ? '羽毛球'
        : null;
    final time =
        task.filters['timePreference'] == 'weekend' ||
            (intent == null &&
                (query.contains('周末') || query.contains('weekend')))
        ? '周末'
        : null;
    final action = switch (intent) {
      'FIND_NEW_PEOPLE' => '找新朋友',
      'PERSONAL_RELATIONSHIP_CONTEXT' => '我的关系信号',
      'FIND_ORGANIZATION' => '找组织',
      'FIND_PLACE' => '找地点',
      'AREA_DISCOVERY' => '搜索此区域',
      'COMPARE_RESULTS' => '比较活动',
      'CREATE_ACTIVITY' => '创建活动',
      'FIND_ACTIVITY' ||
      'REFINE_RESULTS' => category == null ? '找活动' : '$category活动',
      _ when query.contains('组织') || query.contains('社团') => '找组织',
      _ when query.contains('地点') || query.contains('场馆') => '找地点',
      _ when category != null => '$category活动',
      _ when query.contains('活动') || query.contains('activity') => '找活动',
      _ => '当前任务',
    };
    return ActiveIntentSummary(action: action, category: category, time: time);
  }
}
