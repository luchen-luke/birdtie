import 'entity_conversation_page.dart';

class CommunityConversationPage extends EntityConversationPage {
  const CommunityConversationPage({
    super.key,
    required String communityID,
    required String communityTitle,
    required super.authorizationHeader,
    super.apiBaseUrl,
    super.client,
  }) : super(
         entityID: communityID,
         entityTitle: communityTitle,
         kind: ConversationKind.community,
       );
}
