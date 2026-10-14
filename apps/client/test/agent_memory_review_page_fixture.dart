import 'package:http/http.dart' as http;

// Synthetic transport fixture only. Actual source/pagination proof is the
// owned PostgreSQL test; these receipts never authorize model access or writes.
Map<String, dynamic> memoryReviewPageFixture({
  required String owner,
  required String agent,
  required String collection,
  required List<dynamic> items,
  String? nextCursor,
  DateTime? at,
}) {
  final t = at ?? DateTime.now().toUtc();
  return {
    'schema': 'agent-memory-review-page-v1',
    'collection': collection,
    'owner': {'type': 'PERSON', 'id': owner},
    'agentId': agent,
    'items': items,
    'nextCursor': nextCursor,
    'observedAt': t.toIso8601String(),
    'expiresAt': t.add(const Duration(minutes: 1)).toIso8601String(),
    'modelAccess': false,
  };
}

dynamic memoryReviewReadFixture(
  http.BaseRequest request,
  dynamic value, {
  required String owner,
  required String agent,
}) {
  final collection = switch (request.url.path) {
    '/v1/me/agent-memories/page' => 'MEMORIES',
    '/v1/me/agent-memory-candidates/page' => 'CANDIDATES',
    _ => null,
  };
  if (collection == null) return value;
  if (request.method != 'GET' || value is! List) {
    throw StateError('review page fixture must supply a bodyless GET list');
  }
  return memoryReviewPageFixture(
    owner: owner,
    agent: agent,
    collection: collection,
    items: value,
  );
}
