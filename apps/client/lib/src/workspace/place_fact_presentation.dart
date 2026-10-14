import '../city/public_city_controller.dart';
import 'agent_answer_sources.dart';
import 'agent_result_projection.dart';
import 'agent_workspace_controller.dart';

/// A conservative renderer adapter over an already validated native reply.
/// No prose parsing, address synthesis, new source read or lease renewal.
class PlaceFactPresentation {
  const PlaceFactPresentation._({
    required this.result,
    required this.item,
    required this.place,
    required this.sources,
  });

  final AgentResult result;
  final AgentResultItem item;
  final PublicPlace place;
  final AgentMessageSources sources;
  String get entityID => item.entity.mapID;
  String get name => place.name;
  String get formattedAddress => place.addressLabel;

  static PlaceFactPresentation? read(
    AgentWorkspaceController workspace,
    AgentResult result,
  ) {
    if (!workspace.canUseReplyProjection(result) ||
        result.resultSet?.status != 'ready' ||
        result.onlineContext != null ||
        result.activities.isNotEmpty ||
        result.people.isNotEmpty ||
        result.groups.isNotEmpty ||
        result.organizations.isNotEmpty ||
        result.onlineIntents.isNotEmpty ||
        result.actions.isNotEmpty ||
        result.sponsoredOpportunities.isNotEmpty) {
      return null;
    }
    final items = result.projectionItems;
    if (items == null || items.length != 1 || result.places.length != 1) {
      return null;
    }
    final item = items.single, place = result.places.single;
    final anchor = item.anchor, point = place.location;
    if (!item.valid ||
        item.entity.type != 'place' ||
        item.scope != 'AUTHORIZED_VIEW' ||
        item.entity.id != place.id ||
        item.title != place.name ||
        place.name.trim().isEmpty ||
        place.addressLabel.trim().isEmpty ||
        item.summary.isNotEmpty ||
        place.summary.isNotEmpty ||
        anchor == null ||
        anchor.precision != 'point' ||
        anchor.placeID != place.id ||
        !point.hasPublicPoint ||
        point.latitude != anchor.latitude ||
        point.longitude != anchor.longitude) {
      return null;
    }
    final sources = workspace.messageSourcesForReply(result);
    if (sources == null ||
        !workspace.retainsMessageSources(result, sources) ||
        !sources.current ||
        sources.remaining == null ||
        sources.remaining! <= Duration.zero ||
        sources.localReplyID == null ||
        sources.localMemoryUseCount != 0 ||
        sources.sources.isEmpty ||
        sources.localFacts.length != 3) {
      return null;
    }
    final fields = <String>{};
    for (final fact in sources.localFacts) {
      if (fact.placeID != place.id || !fields.add(fact.field)) return null;
    }
    // The native v1 producer emits exactly these fields for its single address
    // fact. Hours/recommendations add other fields. Unknown shapes retain prose.
    if (fields.length != 3 ||
        !fields.containsAll(const {'name', 'address', 'coordinates'})) {
      return null;
    }
    return PlaceFactPresentation._(
      result: result,
      item: item,
      place: place,
      sources: sources,
    );
  }
}
