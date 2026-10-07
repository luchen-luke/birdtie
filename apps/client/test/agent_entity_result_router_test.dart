import 'package:birdtie_client/src/workspace/agent_entity_result_router.dart';
import 'package:birdtie_client/src/workspace/agent_result_projection.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('原七类域详情只能用真实稳定UUID，不从文案或网址路由', () {
    const id = '6c12dfe8-18ef-49eb-8b73-4db5bda920ec';
    for (final k in [
      'person',
      'activity',
      'place',
      'community',
      'organization',
      'business',
    ]) {
      expect(
        AgentEntityResultRouter.routable(AgentResultRef(type: k, id: id)),
        true,
      );
    }
    for (final r in [
      null,
      const AgentResultRef(type: 'opportunity', id: 'intent:activity'),
      const AgentResultRef(type: 'group', id: id),
      const AgentResultRef(type: 'activity', id: 'https://example.com'),
      const AgentResultRef(type: 'activity', id: '旧活动标题'),
    ]) {
      expect(AgentEntityResultRouter.routable(r), false);
    }
  });
}
