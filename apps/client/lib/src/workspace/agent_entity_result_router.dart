import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import 'agent_result_projection.dart';
import 'chat_entity_router.dart';
import 'connections.dart';
import 'entity_share_pending_store.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';
import 'saved_items.dart';

/// The exact original domain route is shared by rows and Pins. Actual reads
/// and concrete recipient preview still use the existing domain services.
class AgentEntityResultRouter {
  const AgentEntityResultRouter({
    required this.auth,
    required this.current,
    required this.workspaceChanges,
    required this.workspaceID,
    this.apiBaseUrl,
    this.client,
  });
  final BirdtieAuthController auth;
  final bool Function(AgentResultItem) current;
  final Listenable workspaceChanges;
  final String? Function() workspaceID;
  final String? apiBaseUrl;
  final http.Client? client;

  static bool routable(AgentResultRef? ref) =>
      ref != null &&
      chatEntityTypes.contains(ref.type) &&
      chatEntityUUID.hasMatch(ref.id);

  bool _can(AgentResultItem item, AgentResultRef? ref) =>
      item.valid && current(item) && routable(ref);

  Future<void> open(BuildContext context, AgentResultItem item) async {
    if (!_can(item, item.detail)) return;
    await openChatEntity(
      context,
      ChatEntityCard(
        type: item.detail!.type,
        id: item.detail!.id,
        available: true,
      ),
      auth: auth,
      workspaceChanges: workspaceChanges,
      workspaceID: workspaceID,
      apiBaseUrl: apiBaseUrl,
      client: client,
    );
  }

  Future<void> share(BuildContext context, AgentResultItem item) async {
    if (!_can(item, item.share)) return;
    final token = auth.authorizationHeader,
        owner = auth.accountID,
        org = workspaceID();
    final ref = item.share!;
    bool live() =>
        _can(item, ref) &&
        auth.authorizationHeader == token &&
        auth.accountID == owner &&
        workspaceID() == org;
    await runEntityAction(
      context,
      ref: EntityActionRef(ref.type, ref.id),
      kind: EntityActionKind.share,
      authorizationHeader: () => live() ? token : null,
      accountID: () => live() ? owner : null,
      workspaceID: workspaceID,
      identityChanges: Listenable.merge([auth, workspaceChanges]),
      apiBaseUrl: apiBaseUrl,
      client: client,
      domainCurrent: live,
      handler: (_) async {
        if (!live()) return;
        await shareEntityToChat(
          context,
          authorizationHeader: () => live() ? token : null,
          type: ref.type,
          id: ref.id,
          identityChanges: Listenable.merge([auth, workspaceChanges]),
          workspaceID: workspaceID,
          apiBaseUrl: apiBaseUrl,
          client: client,
        );
      },
    );
  }

  /// Saves only the original native domain reference. Opportunity IDs are never
  /// bookmark or write targets. The original writer checks its current ACL.
  Future<void> save(
    BuildContext context,
    AgentResultItem item,
    SavedController saved,
  ) async {
    if (!_can(item, item.detail) ||
        item.bookmarkKind == null ||
        item.entity.type == 'opportunity') {
      return;
    }
    final token = auth.authorizationHeader,
        owner = auth.accountID,
        org = workspaceID();
    final ref = item.detail!;
    bool live() =>
        _can(item, ref) &&
        auth.authorizationHeader == token &&
        auth.accountID == owner &&
        workspaceID() == org &&
        saved.authorizationHeader() == token;
    await runEntityAction(
      context,
      ref: EntityActionRef(ref.type, ref.id),
      kind: EntityActionKind.save,
      authorizationHeader: () => live() ? token : null,
      accountID: () => live() ? owner : null,
      workspaceID: workspaceID,
      identityChanges: Listenable.merge([auth, workspaceChanges]),
      client: client,
      apiBaseUrl: apiBaseUrl,
      domainCurrent: live,
      handler: (action) async {
        if (!live() ||
            action.operation !=
                (saved.contains(item.bookmarkKind!, ref.id)
                    ? 'UNSAVE'
                    : 'SAVE')) {
          throw StateError('收藏状态已变化，请重新检查。');
        }
        await saved.toggle(
          item.bookmarkKind!,
          ref.id,
          entrySource: 'agent',
          approved: action,
        );
      },
    );
  }
}
