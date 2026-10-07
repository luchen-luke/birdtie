import 'package:birdtie_client/src/content/agent_seed_controller.dart';
import 'package:birdtie_client/src/content/profile_completion_choice.dart';
import 'package:flutter_test/flutter_test.dart';
import 'agent_seed_controller_test.dart' show seedJson;

void main() {
  test('只用当前本人源计算缺项，兴趣可选，已完整只问所选组', () {
    final source = AgentSeedRecord.fromJson(
      seedJson(
        city: 'aberdeen-gb',
        languages: ['zh-CN'],
        intent: 'JUST_EXPLORE',
      ),
    );
    expect(requiredCompletionGroups(source), isEmpty);
    for (final group in ProfileCompletionGroup.values) {
      expect(completionSteps(source, group), [group]);
    }
    expect(
      () => completionSteps(source, ProfileCompletionGroup.interests).clear(),
      throwsUnsupportedError,
    );
  });
  test('目录缺失的旧城市不能复用；未知意图与语言不得猜值', () {
    final raw = seedJson(city: 'old-city');
    final source = AgentSeedRecord.fromJson(raw);
    expect(requiredCompletionGroups(source), {
      ProfileCompletionGroup.nameAndCity,
      ProfileCompletionGroup.languageAndIntent,
    });
    expect(completionSteps(source, ProfileCompletionGroup.interests), [
      ProfileCompletionGroup.interests,
      ProfileCompletionGroup.nameAndCity,
      ProfileCompletionGroup.languageAndIntent,
    ]);
    expect(source.basicIntent, '');
    expect(source.languages, isEmpty);
  });
}
