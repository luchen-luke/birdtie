// A short human review page. Its cursor never grants analysis or Memory writes.
class AgentMemoryReviewPage<T> {
  const AgentMemoryReviewPage({
    required this.agentID,
    required this.items,
    required this.nextCursor,
    required this.observedAt,
    required this.expiresAt,
  });
  final String agentID;
  final List<T> items;
  final String? nextCursor;
  final DateTime observedAt, expiresAt;
  static bool validCursor(Object? value) =>
      value is String && RegExp(r'^[A-Za-z0-9_-]{1,1024}$').hasMatch(value);
  static bool _uuid(Object? value) =>
      value is String &&
      RegExp(r'^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$').hasMatch(value) &&
      value != '00000000-0000-0000-0000-000000000000';
  static AgentMemoryReviewPage<T> read<T>(
    dynamic value, {
    required String owner,
    required String collection,
    required T Function(dynamic) parse,
    required String Function(T) id,
    required String Function(T) agent,
    DateTime? now,
  }) {
    void check(bool ok) {
      if (!ok) throw const FormatException('本人审阅分页回执不符');
    }

    const fields = {
      'schema',
      'collection',
      'owner',
      'agentId',
      'items',
      'nextCursor',
      'observedAt',
      'expiresAt',
      'modelAccess',
    };
    check(
      value is Map &&
          value.length == fields.length &&
          value.keys.every(fields.contains),
    );
    final m = Map<String, dynamic>.from(value as Map), o = m['owner'];
    check(
      m['schema'] == 'agent-memory-review-page-v1' &&
          m['collection'] == collection &&
          o is Map &&
          o.length == 2 &&
          o['type'] == 'PERSON' &&
          o['id'] == owner &&
          _uuid(owner) &&
          _uuid(m['agentId']) &&
          m['items'] is List &&
          (m['items'] as List).length <= 20 &&
          m['modelAccess'] == false &&
          (m['nextCursor'] == null || validCursor(m['nextCursor'])) &&
          (m['nextCursor'] == null || (m['items'] as List).length == 20) &&
          m['observedAt'] is String &&
          m['expiresAt'] is String &&
          RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(m['observedAt']) &&
          RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(m['expiresAt']),
    );
    final observed = DateTime.parse(m['observedAt']).toUtc(),
        end = DateTime.parse(m['expiresAt']).toUtc(),
        clock = (now ?? DateTime.now()).toUtc();
    check(
      !observed.isAfter(clock) &&
          end.isAfter(clock) &&
          end.isAfter(observed) &&
          end.difference(observed) <= const Duration(minutes: 2),
    );
    final items = List<T>.unmodifiable((m['items'] as List).map(parse));
    check(
      items.map(id).toSet().length == items.length &&
          items.every((v) => agent(v) == m['agentId']),
    );
    return AgentMemoryReviewPage<T>(
      agentID: m['agentId'],
      items: items,
      nextCursor: m['nextCursor'],
      observedAt: observed,
      expiresAt: end,
    );
  }
}
