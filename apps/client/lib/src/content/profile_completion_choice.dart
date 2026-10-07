import 'dart:convert';

import 'agent_seed_controller.dart';

enum ProfileCompletionGroup {
  nameAndCity('昵称与当前城市'),
  languageAndIntent('交流语言与私密意图'),
  interests('私密兴趣');

  const ProfileCompletionGroup(this.label);
  final String label;
}

// Only current human-editing sources. Missing values remain missing; this does
// not infer facts from map, search, RSVP, behavior or model output.
Set<ProfileCompletionGroup> requiredCompletionGroups(AgentSeedRecord source) {
  final nameLength = utf8.encode(source.displayName.trim()).length;
  return Set.unmodifiable({
    if (nameLength < 2 ||
        nameLength > 80 ||
        source.currentCityID == null ||
        !source.cities.any((city) => city.id == source.currentCityID))
      ProfileCompletionGroup.nameAndCity,
    if (source.languages.isEmpty ||
        !seedIntentLabels.containsKey(source.basicIntent))
      ProfileCompletionGroup.languageAndIntent,
  });
}

List<ProfileCompletionGroup> completionSteps(
  AgentSeedRecord source,
  ProfileCompletionGroup chosen,
) => List.unmodifiable([
  chosen,
  for (final group in requiredCompletionGroups(source))
    if (group != chosen) group,
]);
