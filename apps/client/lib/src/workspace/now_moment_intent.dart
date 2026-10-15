/// Only a direct request to compose opens a private, editable Moment draft.
/// The remainder is author text; conversation history is never consulted.
String? explicitMomentDraftBody(String text) {
  final match = RegExp(
    r'^\s*(?:请)?(?:帮我)?(?:发(?:一)?条动态|发布(?:一条)?动态)(?:\s*[:：，,]\s*|\s+|$)([\s\S]*)$',
  ).firstMatch(text);
  return match?.group(1);
}
