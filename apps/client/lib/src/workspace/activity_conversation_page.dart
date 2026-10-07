import 'entity_conversation_page.dart';

class ActivityConversationPage extends EntityConversationPage {
  const ActivityConversationPage({
    super.key,
    required String activityID,
    required String activityTitle,
    required super.authorizationHeader,
    super.apiBaseUrl,
    super.client,
  }) : super(
         entityID: activityID,
         entityTitle: activityTitle,
         kind: ConversationKind.activity,
       );
}
