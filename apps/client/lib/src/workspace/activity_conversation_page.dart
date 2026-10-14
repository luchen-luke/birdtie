import 'entity_conversation_page.dart';

class ActivityConversationPage extends EntityConversationPage {
  const ActivityConversationPage({
    super.key,
    required String activityID,
    required String activityTitle,
    required super.authorizationHeader,
    super.apiBaseUrl,
    super.client,
    super.ownerID,
    super.identityChanges,
    super.sourceCurrent,
  }) : super(
         entityID: activityID,
         entityTitle: activityTitle,
         kind: ConversationKind.activity,
       );
}
