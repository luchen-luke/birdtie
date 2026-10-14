import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../city/public_city_controller.dart' show PublicSource;
import 'agent_source_labels.dart';
import 'now_ui_tokens.dart';

bool _bounded(Object? value, int max, {bool empty = false}) =>
    value is String &&
    (empty || value.isNotEmpty) &&
    value.trim() == value &&
    utf8.encode(value).length <= max &&
    !value.runes.any((r) => r < 32 || r == 127);

bool _digest(Object? value) =>
    value is String &&
    RegExp(r'^[0-9a-f]{64}$').hasMatch(value) &&
    value != List.filled(64, '0').join();

Uri? _sourceUri(Object? value) {
  if (!_bounded(value, 2048) || (value as String).contains(RegExp(r'[\s\\]'))) {
    return null;
  }
  final uri = Uri.tryParse(value);
  if (uri == null ||
      !const {'http', 'https'}.contains(uri.scheme) ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      !uri.hasAuthority) {
    return null;
  }
  return uri;
}

String agentAnswerQueryDigest(String query) => sha256
    .convert(
      utf8.encode('birdtie.sourced-answer.query.v1\u0000${query.trim()}'),
    )
    .toString();

// This digest identifies immutable presentation only. It is never a permission,
// deadline or grant, and includes the original URL rather than a normalized URI.
String _sourcePresentationDigest(
  List<Object?> binding,
  String text,
  List raw,
) => sha256
    .convert(
      utf8.encode(
        jsonEncode([
          'birdtie.source-presentation.v1',
          binding,
          text,
          for (final source in raw)
            [
              (source as Map)['id'],
              source['title'],
              source['url'],
              source['site'] ?? '',
              source['publishedAt'] ?? '',
            ],
        ]),
      ),
    )
    .toString();

String? _messagePresentationIdentity(
  Map<String, dynamic> message,
  String? taskID,
  int? messageIndex,
) {
  if (taskID == null ||
      messageIndex == null ||
      messageIndex < 0 ||
      message['text'] is! String ||
      message['sources'] is! List)
    return null;
  final licensed = message['licensedPlaceModelEvidence'] as Map?;
  final local = message['localReplyEvidence'] as Map?;
  return _sourcePresentationDigest(
    [
      'persisted',
      taskID,
      messageIndex,
      licensed != null
          ? 'licensed'
          : local != null
          ? 'local'
          : 'run',
      message['sourceRunId'],
      message['sourceEvidenceDigest'],
      licensed?['operationId'],
      licensed?['bindingId'],
      licensed?['textDigest'],
      licensed?['validUntil'],
      local?['id'],
      local?['validUntil'],
    ],
    message['text'] as String,
    message['sources'] as List,
  );
}

class AgentAnswerSource {
  const AgentAnswerSource({
    required this.id,
    required this.title,
    required this.uri,
    this.site = '',
    this.publishedAt = '',
  });
  final String id, title, site, publishedAt;
  final Uri uri;
}

/// Validated native fact references describe this reply only. They are not
/// values, grants, model context or permission to read a new source.
class AgentPlaceFactReference {
  const AgentPlaceFactReference({
    required this.placeID,
    required this.field,
    required this.evidenceID,
    required this.permissionID,
    required this.version,
  });
  final String placeID, field, evidenceID, permissionID;
  final int version;
}

List<AgentAnswerSource> _readSourceReferences(Object? raw) {
  void require(bool valid) {
    if (!valid) throw const FormatException('回答来源无法核验');
  }

  require(raw is List && raw.isNotEmpty && raw.length <= 10);
  final parsed = <AgentAnswerSource>[];
  final ids = <String>{}, urls = <String>{};
  for (final item in raw as List) {
    require(item is Map<String, dynamic>);
    final s = item as Map<String, dynamic>;
    require(
      s.keys.toSet().difference(const {
        'id',
        'title',
        'url',
        'site',
        'publishedAt',
      }).isEmpty,
    );
    require(_bounded(s['id'], 64) && _bounded(s['title'], 1024));
    require(s['site'] == null || _bounded(s['site'], 512, empty: true));
    require(
      s['publishedAt'] == null || _bounded(s['publishedAt'], 128, empty: true),
    );
    final uri = _sourceUri(s['url']);
    require(
      uri != null && ids.add(s['id'] as String) && urls.add(s['url'] as String),
    );
    parsed.add(
      AgentAnswerSource(
        id: s['id'] as String,
        title: s['title'] as String,
        uri: uri!,
        site: s['site'] as String? ?? '',
        publishedAt: s['publishedAt'] as String? ?? '',
      ),
    );
  }
  return List.unmodifiable(parsed);
}

/// An immutable citation on one stored assistant message. A current authorized
/// history read can display its public links; this does not renew an expired
/// live answer binding, query context or model egress permission.
class AgentMessageSources {
  AgentMessageSources._(
    this.runID,
    this.sourceEvidenceDigest,
    this.sources,
    this._current, {
    this.localReplyID,
    this.localObservedValidUntil,
    this.licensedModelOperationID,
    this.licensedModelObservedValidUntil,
    this.localFacts = const [],
    this.localMemoryUseCount = 0,
    Duration Function()? currentRoundRemaining,
    String? presentationIdentity,
  }) : _roundRemaining = currentRoundRemaining,
       _presentationIdentity = presentationIdentity;
  final String runID, sourceEvidenceDigest;
  // A native local reply is not a model Run. Its saved deadline describes the
  // original observation; only a fresh same-round history read grants a lease.
  final String? localReplyID;
  final DateTime? localObservedValidUntil;
  final String? licensedModelOperationID;
  final DateTime? licensedModelObservedValidUntil;
  final List<AgentAnswerSource> sources;
  final List<AgentPlaceFactReference> localFacts;
  final int localMemoryUseCount;
  final bool Function() _current;
  final Duration Function()? _roundRemaining;
  final String? _presentationIdentity;
  Duration? get remaining => _roundRemaining?.call();
  bool _retired = false;
  bool get current {
    if (_retired || !_current()) _retired = true;
    return !_retired;
  }

  static AgentMessageSources? read(
    Map<String, dynamic> message, {
    bool Function()? current,
    String? expectedTaskID,
    int? expectedMessageIndex,
    bool Function()? localRoundCurrent,
    Duration Function()? localRoundRemaining,
  }) {
    final licensed = message['licensedPlaceModelEvidence'];
    if (licensed != null) {
      void require(bool valid) {
        if (!valid) throw const FormatException('地点模型回答来源无法核验');
      }

      require(
        message['role'] == 'assistant' && message['localReplyEvidence'] == null,
      );
      require(licensed is Map<String, dynamic>);
      final proof = licensed as Map<String, dynamic>;
      const keys = {
        'schema',
        'operationId',
        'previewId',
        'bindingId',
        'providerRequestId',
        'provider',
        'model',
        'requestDigest',
        'payloadDigest',
        'currentQueryDigest',
        'textDigest',
        'sourceEvidenceDigest',
        'facts',
        'validUntil',
        'searchCalls',
        'modelCalls',
      };
      require(proof.length == keys.length && proof.keys.every(keys.contains));
      bool id(Object? value) =>
          value is String &&
          RegExp(
            r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
          ).hasMatch(value) &&
          value != '00000000-0000-0000-0000-000000000000';
      require(
        proof['schema'] == 'birdtie.licensed-place-model-reply.v1' &&
            proof['provider'] == 'tencent_tokenhub' &&
            proof['model'] == 'deepseek-v4-pro-0813' &&
            proof['searchCalls'] is int &&
            proof['searchCalls'] == 0 &&
            proof['modelCalls'] is int &&
            proof['modelCalls'] == 1 &&
            (message['sourceRunId'] == null || message['sourceRunId'] == '') &&
            id(proof['operationId']) &&
            id(proof['previewId']) &&
            id(proof['bindingId']) &&
            _bounded(proof['providerRequestId'], 512),
      );
      for (final key in const [
        'requestDigest',
        'payloadDigest',
        'currentQueryDigest',
        'textDigest',
        'sourceEvidenceDigest',
      ]) {
        require(_digest(proof[key]));
      }
      require(
        proof['sourceEvidenceDigest'] == message['sourceEvidenceDigest'] &&
            message['text'] is String &&
            proof['textDigest'] ==
                sha256
                    .convert(
                      utf8.encode(
                        'birdtie.licensed-model-text.v1\u0000${message['text']}',
                      ),
                    )
                    .toString(),
      );
      final rawUntil = proof['validUntil'];
      require(
        _bounded(rawUntil, 64) &&
            RegExp(
              r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$',
            ).hasMatch(rawUntil as String),
      );
      final civil = DateTime.tryParse(
        '${(rawUntil as String).substring(0, 19)}Z',
      );
      final zone = RegExp(r'(Z|([+-])(\d\d):(\d\d))$').firstMatch(rawUntil)!;
      require(
        zone.group(1) == 'Z' ||
            (int.parse(zone.group(3)!) < 24 &&
                int.parse(zone.group(4)!) < 60 &&
                zone.group(1) != '-00:00'),
      );
      final until = DateTime.tryParse(rawUntil)?.toUtc();
      require(
        civil != null &&
            civil.year > 1 &&
            until != null &&
            until.year > 1 &&
            civil.toIso8601String().substring(0, 19) ==
                rawUntil.substring(0, 19),
      );
      final facts = proof['facts'];
      require(facts is List && facts.isNotEmpty && facts.length <= 100);
      final evidenceIDs = <String>{}, placeFields = <String>{};
      for (final raw in facts as List) {
        require(raw is Map<String, dynamic>);
        final fact = raw as Map<String, dynamic>;
        require(
          fact.length == 5 &&
              fact.keys.every(
                const {
                  'evidenceId',
                  'placeId',
                  'field',
                  'version',
                  'permissionId',
                }.contains,
              ),
        );
        require(
          id(fact['evidenceId']) &&
              id(fact['placeId']) &&
              id(fact['permissionId']) &&
              fact['version'] is int &&
              (fact['version'] as int) > 0 &&
              const {
                'name',
                'address',
                'coordinates',
                'food_seafood',
                'food_spicy',
                'opening_hours',
              }.contains(fact['field']),
        );
        require(
          evidenceIDs.add(fact['evidenceId'] as String) &&
              placeFields.add(
                '${fact['placeId']}:${fact['field']}:${fact['permissionId']}',
              ),
        );
      }
      // Stored operation IDs and the old dispatch deadline cannot grant a
      // new provider call or source access. Only this authorized round read
      // supplies the current capability; withdrawal permanently retires it.
      return AgentMessageSources._(
        proof['bindingId'] as String,
        proof['sourceEvidenceDigest'] as String,
        _readSourceReferences(message['sources']),
        () =>
            expectedTaskID != null &&
            expectedMessageIndex != null &&
            current?.call() == true &&
            localRoundCurrent?.call() == true,
        licensedModelOperationID: proof['operationId'] as String,
        licensedModelObservedValidUntil: until,
        currentRoundRemaining: localRoundRemaining,
        presentationIdentity: _messagePresentationIdentity(
          message,
          expectedTaskID,
          expectedMessageIndex,
        ),
      );
    }
    final local = message['localReplyEvidence'];
    if (local != null) {
      void require(bool valid) {
        if (!valid) throw const FormatException('本地回答来源无法核验');
      }

      require(message['role'] == 'assistant');
      require(local is Map<String, dynamic>);
      final proof = local as Map<String, dynamic>;
      const keys = {
        'schema',
        'id',
        'sourceEvidenceDigest',
        'validUntil',
        'facts',
        'memoryUses',
        'reason',
        'searchCalls',
        'modelCalls',
      };
      require(proof.length == keys.length && proof.keys.every(keys.contains));
      require(
        proof['schema'] == 'birdtie.local-place-reply.v1' &&
            proof['reason'] == 'LOCAL_FIELDS_SUFFICIENT' &&
            proof['searchCalls'] is int &&
            proof['searchCalls'] == 0 &&
            proof['modelCalls'] is int &&
            proof['modelCalls'] == 0 &&
            (message['sourceRunId'] == null || message['sourceRunId'] == '') &&
            _digest(proof['sourceEvidenceDigest']) &&
            proof['sourceEvidenceDigest'] == message['sourceEvidenceDigest'],
      );
      require(_bounded(proof['id'], 180));
      final correlation = RegExp(
        r'^local-native:([^:]+):(0|[1-9]\d*)$',
      ).firstMatch(proof['id'] as String);
      require(correlation != null && _bounded(correlation.group(1), 80));
      require(
        (expectedTaskID == null && expectedMessageIndex == null) ||
            (expectedTaskID != null &&
                expectedMessageIndex != null &&
                expectedMessageIndex >= 0 &&
                proof['id'] ==
                    'local-native:$expectedTaskID:$expectedMessageIndex'),
      );
      final rawUntil = proof['validUntil'];
      require(
        _bounded(rawUntil, 64) &&
            RegExp(
              r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$',
            ).hasMatch(rawUntil as String),
      );
      // PostgreSQL returns the original Go observation with its explicit
      // RFC3339 offset. Validate its civil date before normalizing to UTC;
      // DateTime.tryParse alone accepts overflowing dates and zone minutes.
      final civil = DateTime.tryParse(
        '${(rawUntil as String).substring(0, 19)}Z',
      );
      final zone = RegExp(r'(Z|([+-])(\d\d):(\d\d))$').firstMatch(rawUntil)!;
      require(
        zone.group(1) == 'Z' ||
            (int.parse(zone.group(3)!) < 24 &&
                int.parse(zone.group(4)!) < 60 &&
                zone.group(1) != '-00:00'),
      );
      final until = DateTime.tryParse(rawUntil)?.toUtc();
      require(
        civil != null &&
            civil.year > 1 &&
            until != null &&
            until.year > 1 &&
            civil.toIso8601String().substring(0, 19) ==
                rawUntil.substring(0, 19),
      );
      bool id(Object? value) =>
          value is String &&
          RegExp(
            r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
          ).hasMatch(value) &&
          value != '00000000-0000-0000-0000-000000000000';
      bool version(Object? value) => value is int && value > 0;
      final facts = proof['facts'];
      require(facts is List && facts.isNotEmpty && facts.length <= 100);
      final evidenceIDs = <String>{}, placeFields = <String>{};
      for (final raw in facts as List) {
        require(raw is Map<String, dynamic>);
        final fact = raw as Map<String, dynamic>;
        require(
          fact.length == 5 &&
              fact.keys.every(
                const {
                  'evidenceId',
                  'placeId',
                  'field',
                  'version',
                  'permissionId',
                }.contains,
              ),
        );
        require(
          id(fact['evidenceId']) &&
              id(fact['placeId']) &&
              id(fact['permissionId']) &&
              version(fact['version']) &&
              const {
                'name',
                'address',
                'coordinates',
                'food_seafood',
                'food_spicy',
                'opening_hours',
              }.contains(fact['field']),
        );
        require(
          evidenceIDs.add(fact['evidenceId'] as String) &&
              placeFields.add(
                '${fact['placeId']}:${fact['field']}:${fact['permissionId']}',
              ),
        );
      }
      final memory = proof['memoryUses'];
      require(memory is List && memory.length <= 2);
      final memoryIDs = <String>{};
      for (final raw in memory as List) {
        require(raw is Map<String, dynamic>);
        final use = raw as Map<String, dynamic>;
        require(
          use.length == 4 &&
              use.keys.every(
                const {'memoryId', 'version', 'topic', 'value'}.contains,
              ),
        );
        require(
          id(use['memoryId']) &&
              version(use['version']) &&
              const {'seafood', 'spicy'}.contains(use['topic']) &&
              const {'LIKE', 'DISLIKE'}.contains(use['value']) &&
              memoryIDs.add(use['memoryId'] as String),
        );
      }
      return AgentMessageSources._(
        '',
        proof['sourceEvidenceDigest'] as String,
        _readSourceReferences(message['sources']),
        () =>
            expectedTaskID != null &&
            expectedMessageIndex != null &&
            current?.call() == true &&
            localRoundCurrent?.call() == true,
        localReplyID: proof['id'] as String,
        localObservedValidUntil: until,
        localFacts: List.unmodifiable([
          for (final fact in facts)
            AgentPlaceFactReference(
              placeID: fact['placeId'] as String,
              field: fact['field'] as String,
              evidenceID: fact['evidenceId'] as String,
              permissionID: fact['permissionId'] as String,
              version: fact['version'] as int,
            ),
        ]),
        localMemoryUseCount: memory.length,
        currentRoundRemaining: localRoundRemaining,
        presentationIdentity: _messagePresentationIdentity(
          message,
          expectedTaskID,
          expectedMessageIndex,
        ),
      );
    }
    if (message['role'] != 'assistant') return null;
    final raw = message['sources'];
    if (raw == null || raw is List && raw.isEmpty) return null;
    final run = message['sourceRunId'],
        evidence = message['sourceEvidenceDigest'];
    // Old unbound source assertions remain non-clickable. Actual history
    // metadata is read data, not a client-constructed authorization handle.
    if (run == null || evidence == null) return null;
    if (!_bounded(run, 180) || !_digest(evidence)) {
      throw const FormatException('回答来源无法核验');
    }
    return AgentMessageSources._(
      run as String,
      evidence as String,
      _readSourceReferences(raw),
      current ?? () => false,
      presentationIdentity: _messagePresentationIdentity(
        message,
        expectedTaskID,
        expectedMessageIndex,
      ),
    );
  }
}

/// Correlation and display data from one original response, never an entity,
/// action, source-reading authority, model context or egress permission.
class AgentAnswerSources {
  AgentAnswerSources._(
    this._current, {
    required this.taskID,
    required this.requestID,
    required this.queryDigest,
    required this.taskSnapshotDigest,
    required this.sourceEvidenceDigest,
    required this.runID,
    required this.generatedAt,
    required this.validUntil,
    required List<AgentAnswerSource> sources,
    required DateTime Function() now,
    required String? presentationIdentity,
  }) : sources = List.unmodifiable(sources),
       _now = now,
       _presentationIdentity = presentationIdentity,
       _remaining = validUntil.difference(now().toUtc());

  final String taskID,
      requestID,
      queryDigest,
      taskSnapshotDigest,
      sourceEvidenceDigest,
      runID;
  final DateTime generatedAt, validUntil;
  final List<AgentAnswerSource> sources;
  final bool Function() _current;
  final DateTime Function() _now;
  final Duration _remaining;
  final String? _presentationIdentity;
  final Stopwatch _watch = Stopwatch()..start();
  bool _retired = false;

  Duration get remaining => _remaining - _watch.elapsed;

  bool get current {
    final now = _now().toUtc();
    if (_retired ||
        !_current() ||
        now.isBefore(generatedAt) ||
        !now.isBefore(validUntil) ||
        remaining <= Duration.zero) {
      _retired = true;
      return false;
    }
    return true;
  }

  void retire() => _retired = true;

  static AgentAnswerSources? read(
    Map<String, dynamic> data, {
    required bool Function() current,
    required DateTime Function() now,
    String? expectedRequestID,
  }) {
    final set = data['resultSet'];
    if (set is! Map<String, dynamic>) return null;
    final rawSources = set['sources'];
    // Old APIs and unbound source assertions cannot create a clickable reply.
    if (rawSources == null || rawSources is List && rawSources.isEmpty) {
      return null;
    }
    final binding = set['answerBinding'];
    if (binding == null) return null;
    void require(bool valid) {
      if (!valid) throw const FormatException('回答来源无法核验');
    }

    require(rawSources is List && rawSources.length <= 10);
    require(binding is Map<String, dynamic>);
    final b = binding as Map<String, dynamic>;
    require(
      b.keys.toSet().difference(const {
        'taskId',
        'requestId',
        'currentQueryDigest',
        'taskSnapshotDigest',
        'sourceEvidenceDigest',
        'runId',
        'generatedAt',
        'validUntil',
      }).isEmpty,
    );
    require(_bounded(b['taskId'], 180) && _bounded(b['requestId'], 180));
    require(_bounded(b['runId'], 180));
    require(
      _digest(b['currentQueryDigest']) &&
          _digest(b['taskSnapshotDigest']) &&
          _digest(b['sourceEvidenceDigest']),
    );
    final task = data['task'];
    require(task is Map<String, dynamic>);
    final t = task as Map<String, dynamic>;
    require(
      t['id'] == b['taskId'] &&
          set['taskId'] == b['taskId'] &&
          data['taskId'] == b['taskId'] &&
          data['conversationId'] == b['taskId'],
    );
    require(
      data['requestId'] == b['requestId'] &&
          (expectedRequestID == null || expectedRequestID == b['requestId']),
    );
    final filters = t['filters'];
    require(filters == null || filters is Map<String, dynamic>);
    final query =
        filters is Map<String, dynamic> && filters.containsKey('currentQuery')
        ? filters['currentQuery']
        : t['query'];
    require(
      query is String &&
          query.trim().isNotEmpty &&
          utf8.encode(query).length <= 4096,
    );
    final messages = t['conversation'];
    require(messages is List);
    String? lastUser;
    for (final m in messages as List) {
      require(m is Map<String, dynamic>);
      if ((m as Map<String, dynamic>)['role'] == 'user') {
        require(m['text'] is String);
        lastUser = m['text'] as String;
      }
    }
    require(lastUser != null && lastUser.trim() == (query as String).trim());
    require(agentAnswerQueryDigest(query) == b['currentQueryDigest']);
    DateTime time(Object? value) {
      require(
        value is String &&
            RegExp(
              r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$',
            ).hasMatch(value),
      );
      final parsed = DateTime.tryParse(value as String);
      require(
        parsed != null &&
            parsed.year > 1 &&
            parsed.toIso8601String().substring(0, 19) == value.substring(0, 19),
      );
      return parsed!;
    }

    final generated = time(b['generatedAt']), expires = time(b['validUntil']);
    require(
      expires.isAfter(generated) &&
          expires.difference(generated) <= const Duration(minutes: 15),
    );
    final parsed = _readSourceReferences(rawSources);
    final text =
        data['message'] ??
        messages
            .cast<Map<String, dynamic>>()
            .where((m) => m['role'] == 'assistant')
            .lastOrNull?['text'];
    return AgentAnswerSources._(
      current,
      taskID: b['taskId'] as String,
      requestID: b['requestId'] as String,
      queryDigest: b['currentQueryDigest'] as String,
      taskSnapshotDigest: b['taskSnapshotDigest'] as String,
      sourceEvidenceDigest: b['sourceEvidenceDigest'] as String,
      runID: b['runId'] as String,
      generatedAt: generated,
      validUntil: expires,
      sources: parsed,
      now: now,
      presentationIdentity: text is String
          ? _sourcePresentationDigest(
              [
                'live',
                b['taskId'],
                b['runId'],
                b['currentQueryDigest'],
                b['taskSnapshotDigest'],
                b['sourceEvidenceDigest'],
              ],
              text,
              rawSources as List,
            )
          : null,
    );
  }
}

class AgentAnswerSourcesPanel extends StatefulWidget {
  const AgentAnswerSourcesPanel({
    super.key,
    required this.answer,
    required this.replyCurrent,
    this.openSource,
    this.changes,
  }) : persisted = null;
  const AgentAnswerSourcesPanel.persisted({
    super.key,
    required AgentMessageSources sources,
    required this.replyCurrent,
    this.openSource,
    this.changes,
  }) : persisted = sources,
       answer = null;
  final AgentAnswerSources? answer;
  final AgentMessageSources? persisted;
  final bool Function() replyCurrent;
  final Future<bool> Function(Uri)? openSource;
  final Listenable? changes;

  @override
  State<AgentAnswerSourcesPanel> createState() =>
      _AgentAnswerSourcesPanelState();
}

class _AgentAnswerSourcesPanelState extends State<AgentAnswerSourcesPanel> {
  Timer? _expiry;
  bool _expanded = false, _retired = false;
  bool _sourceCurrent(AgentAnswerSourcesPanel panel) =>
      panel.answer?.current ?? panel.persisted?.current ?? false;

  String? _presentationIdentity(AgentAnswerSourcesPanel panel) =>
      panel.answer?._presentationIdentity ??
      panel.persisted?._presentationIdentity;

  void _changed() {
    // A refresh notification changes reply ownership before didUpdateWidget
    // receives the fresh object. Its old replyCurrent closure is now false.
    // Observe actual authority immediately (including ABA withdrawal), then
    // let build evaluate reply ownership against the updated widget.
    if (!_sourceCurrent(widget)) {
      _retired = true;
      _expanded = false;
    }
    if (mounted) setState(() {});
  }

  void _schedule() {
    _expiry?.cancel();
    final remaining = widget.answer?.remaining ?? widget.persisted?.remaining;
    if (remaining != null && remaining > Duration.zero) {
      _expiry = Timer(remaining, () {
        if (mounted) setState(() {});
      });
    }
  }

  @override
  void initState() {
    super.initState();
    widget.changes?.addListener(_changed);
    _schedule();
  }

  @override
  void didUpdateWidget(AgentAnswerSourcesPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.changes != widget.changes) {
      oldWidget.changes?.removeListener(_changed);
      widget.changes?.addListener(_changed);
    }
    if (!identical(oldWidget.answer, widget.answer) ||
        !identical(oldWidget.persisted, widget.persisted)) {
      final original = _presentationIdentity(oldWidget);
      _expanded =
          _expanded &&
          !_retired &&
          original != null &&
          original == _presentationIdentity(widget) &&
          _sourceCurrent(oldWidget) &&
          _sourceCurrent(widget) &&
          widget.replyCurrent();
      _retired = false;
      // Always schedule the fresh capability's own lease. No previous closure,
      // source object or deadline survives this presentation-only handoff.
      _schedule();
    }
  }

  @override
  void dispose() {
    _expiry?.cancel();
    widget.changes?.removeListener(_changed);
    super.dispose();
  }

  bool get _current {
    if (!widget.replyCurrent() ||
        !(widget.answer?.current ?? widget.persisted!.current)) {
      _retired = true;
      _expanded = false;
    }
    return !_retired;
  }

  List<AgentAnswerSource> get _sources =>
      widget.answer?.sources ?? widget.persisted!.sources;

  Future<void> _open(AgentAnswerSource source) async {
    if (!_current) {
      if (mounted) setState(() {});
      return;
    }
    bool opened;
    try {
      opened =
          await (widget.openSource?.call(source.uri) ??
              launchUrl(source.uri, mode: LaunchMode.externalApplication));
    } catch (_) {
      opened = false;
    }
    if (mounted && _current && !opened) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('暂时无法打开来源链接。')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final current = _current;
    final tokens = NowUiTokens.reviewedOf(context);
    final content = Padding(
      key: const Key('agent-answer-sources'),
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (current && !_expanded)
            Wrap(
              spacing: 6,
              runSpacing: 0,
              children: [
                for (final source in _sources.take(3))
                  Tooltip(
                    message: source.title,
                    child: TextButton.icon(
                      key: ValueKey('agent-source-pill-${source.id}'),
                      onPressed: () => _open(source),
                      style: TextButton.styleFrom(
                        minimumSize: const Size(48, 48),
                        tapTargetSize: MaterialTapTargetSize.padded,
                        padding: const EdgeInsets.symmetric(horizontal: 9),
                        foregroundColor: tokens.metadata.color,
                        backgroundColor: Theme.of(
                          context,
                        ).colorScheme.surfaceContainerHigh,
                        textStyle: tokens.action,
                        shape: const StadiumBorder(),
                      ),
                      icon: const Icon(Icons.public, size: 14),
                      label: ConstrainedBox(
                        constraints: const BoxConstraints(maxWidth: 120),
                        child: Text(
                          source.site.isEmpty ? source.uri.host : source.site,
                          softWrap: true,
                        ),
                      ),
                    ),
                  ),
                _sourceToggle(current),
              ],
            ),
          if (_expanded || !current)
            Align(
              alignment: Alignment.centerLeft,
              child: _sourceToggle(current),
            ),
          if (!current) Text('来源已失效，请重新查询。', style: tokens.body),
          if (_expanded)
            for (final (index, source) in _sources.indexed)
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text('来源 ${index + 1}', style: tokens.metadata),
                    DefaultTextStyle(
                      style: tokens.metadata,
                      child: AgentSourceMetadata(
                        source: source,
                        showMetadata: current,
                        onOpen: current ? () => _open(source) : null,
                      ),
                    ),
                  ],
                ),
              ),
        ],
      ),
    );
    return LayoutBuilder(
      builder: (context, constraints) => constraints.hasBoundedHeight
          ? SingleChildScrollView(child: content)
          : content,
    );
  }

  Widget _sourceToggle(bool current) => TextButton.icon(
    key: const Key('agent-answer-sources-toggle'),
    style: TextButton.styleFrom(
      minimumSize: const Size(48, 48),
      textStyle: NowUiTokens.reviewedOf(context).action,
    ),
    onPressed: current ? () => setState(() => _expanded = !_expanded) : null,
    icon: Icon(_expanded ? Icons.expand_less : Icons.expand_more, size: 16),
    label: Text(_expanded ? '收起来源' : '全部来源 ${_sources.length}'),
  );
}

/// The sole provenance entry on a structured place answer. PublicSource only
/// supplements a matching document's metadata; it never revives a dead lease.
class AgentPlaceSourceBinding {
  const AgentPlaceSourceBinding({
    required this.taskID,
    required this.messageIndex,
    required this.entityID,
    required this.contentIdentity,
    required this.sources,
    required this.current,
    this.placeSource,
  });
  final String taskID, entityID, contentIdentity;
  final int messageIndex;
  final AgentMessageSources sources;
  final bool Function() current;
  final PublicSource? placeSource;

  bool _samePresentation(AgentPlaceSourceBinding other) {
    if (taskID != other.taskID ||
        messageIndex != other.messageIndex ||
        entityID != other.entityID ||
        contentIdentity != other.contentIdentity ||
        sources._presentationIdentity == null ||
        sources._presentationIdentity != other.sources._presentationIdentity ||
        sources.localReplyID != other.sources.localReplyID ||
        sources.sourceEvidenceDigest != other.sources.sourceEvidenceDigest ||
        sources.localMemoryUseCount != other.sources.localMemoryUseCount ||
        sources.localFacts.length != other.sources.localFacts.length ||
        sources.sources.length != other.sources.sources.length) {
      return false;
    }
    for (var i = 0; i < sources.localFacts.length; i++) {
      final a = sources.localFacts[i], b = other.sources.localFacts[i];
      if (a.placeID != b.placeID ||
          a.field != b.field ||
          a.evidenceID != b.evidenceID ||
          a.permissionID != b.permissionID ||
          a.version != b.version)
        return false;
    }
    for (var i = 0; i < sources.sources.length; i++) {
      final a = sources.sources[i], b = other.sources.sources[i];
      if (a.id != b.id ||
          a.title != b.title ||
          a.uri != b.uri ||
          a.site != b.site ||
          a.publishedAt != b.publishedAt)
        return false;
    }
    return true;
  }
}

class AgentPlaceSourceEntry extends StatelessWidget {
  const AgentPlaceSourceEntry({
    super.key,
    required this.sources,
    required this.current,
    required this.changes,
    this.placeSource,
    this.openSource,
    this.resolveCurrent,
  });
  final AgentMessageSources sources;
  final bool Function() current;
  final Listenable changes;
  final PublicSource? placeSource;
  final Future<bool> Function(Uri)? openSource;
  final AgentPlaceSourceBinding? Function()? resolveCurrent;

  @override
  Widget build(BuildContext context) => IconButton(
    key: const Key('place-answer-sources'),
    tooltip: '查看此地点的信息来源',
    constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
    onPressed: !current() || !sources.current
        ? null
        : () {
            if (!current() || !sources.current) return;
            final binding = resolveCurrent?.call();
            if (resolveCurrent != null &&
                (binding == null ||
                    !identical(binding.sources, sources) ||
                    !binding.current()))
              return;
            FocusManager.instance.primaryFocus?.unfocus();
            showModalBottomSheet<void>(
              context: context,
              isScrollControlled: true,
              useSafeArea: true,
              builder: (_) => _PlaceProvenanceSheet(
                sources: sources,
                current: current,
                changes: changes,
                placeSource: placeSource,
                openSource: openSource,
                binding: binding,
                resolveCurrent: resolveCurrent,
              ),
            );
          },
    icon: const Icon(Icons.info_outline, size: 20),
  );
}

class _PlaceProvenanceSheet extends StatefulWidget {
  const _PlaceProvenanceSheet({
    required this.sources,
    required this.current,
    required this.changes,
    this.placeSource,
    this.openSource,
    this.binding,
    this.resolveCurrent,
  });
  final AgentMessageSources sources;
  final bool Function() current;
  final Listenable changes;
  final PublicSource? placeSource;
  final Future<bool> Function(Uri)? openSource;
  final AgentPlaceSourceBinding? binding;
  final AgentPlaceSourceBinding? Function()? resolveCurrent;

  @override
  State<_PlaceProvenanceSheet> createState() => _PlaceProvenanceSheetState();
}

class _PlaceProvenanceSheetState extends State<_PlaceProvenanceSheet> {
  Timer? _expiry;
  bool _retired = false;
  AgentPlaceSourceBinding? _binding;
  AgentMessageSources get _sources => _binding?.sources ?? widget.sources;
  PublicSource? get _placeSource => _binding?.placeSource ?? widget.placeSource;

  void _resolveBinding() {
    if (_retired) return;
    final original = _binding;
    // The old capability must still be live at the handoff. A later fresh
    // response cannot revive an observed withdrawal or expired modal.
    if (!_sources.current ||
        (original != null &&
            (_sources.remaining == null ||
                _sources.remaining! <= Duration.zero))) {
      _retired = true;
      return;
    }
    if (original == null) {
      if (!widget.current()) _retired = true;
      return;
    }
    final fresh = widget.resolveCurrent?.call();
    if (fresh == null ||
        !original._samePresentation(fresh) ||
        !fresh.sources.current ||
        fresh.sources.remaining == null ||
        fresh.sources.remaining! <= Duration.zero ||
        !fresh.current()) {
      _retired = true;
      return;
    }
    if (!identical(original.sources, fresh.sources)) {
      _binding = fresh;
      _schedule();
    }
  }

  bool get _current {
    _resolveBinding();
    return !_retired;
  }

  void _changed() {
    if (!_current) _retired = true;
    if (mounted) setState(() {});
  }

  @override
  void initState() {
    super.initState();
    _binding = widget.binding;
    widget.changes.addListener(_changed);
    _schedule();
  }

  void _schedule() {
    _expiry?.cancel();
    final remaining = _sources.remaining;
    if (remaining != null && remaining > Duration.zero) {
      _expiry = Timer(remaining, _changed);
    }
  }

  @override
  void dispose() {
    widget.changes.removeListener(_changed);
    _expiry?.cancel();
    super.dispose();
  }

  Future<void> _open(AgentAnswerSource source) async {
    if (!_current) return;
    var opened = false;
    try {
      opened =
          await (widget.openSource?.call(source.uri) ??
              launchUrl(source.uri, mode: LaunchMode.externalApplication));
    } catch (_) {
      opened = false;
    }
    if (mounted && _current && !opened) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('暂时无法打开来源链接。')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.reviewedOf(context);
    final current = _current;
    // Exact documents only; never merge by domain or remove query arguments.
    final documents = <String, AgentAnswerSource>{};
    for (final source in _sources.sources) {
      documents.putIfAbsent(source.uri.toString(), () => source);
    }
    return SafeArea(
      key: const Key('place-provenance-modal'),
      top: false,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * .8,
        ),
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  Expanded(child: Text('地点信息来源', style: tokens.sheetTitle)),
                  IconButton(
                    tooltip: '关闭来源',
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close),
                  ),
                ],
              ),
              if (!current)
                Text('来源已失效，请重新查询。', style: tokens.body)
              else ...[
                Text('本回合证据字段：名称、地址、坐标', style: tokens.metadata),
                Text('网页与字段的逐项对应关系未提供。', style: tokens.metadata),
                const SizedBox(height: 16),
                for (final source in documents.values) ...[
                  Text(source.title, style: tokens.body),
                  if (source.site.isNotEmpty)
                    Text('来源原名：${source.site}', style: tokens.metadata),
                  SelectableText(source.uri.toString(), style: tokens.metadata),
                  Text(
                    '发布时间：${source.publishedAt.isEmpty ? '未提供' : source.publishedAt}',
                    style: tokens.metadata,
                  ),
                  if (_placeSource?.reference == source.uri.toString()) ...[
                    if (_placeSource!.maintainer.isNotEmpty)
                      Text(
                        '资料维护：${_placeSource!.maintainer}',
                        style: tokens.metadata,
                      ),
                    Text(
                      '更新时间：${_placeSource!.updatedAt?.toIso8601String() ?? '未提供'}',
                      style: tokens.metadata,
                    ),
                  ],
                  Text('获取时间：未提供', style: tokens.metadata),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton.icon(
                      key: ValueKey('place-answer-source-${source.id}'),
                      style: TextButton.styleFrom(
                        minimumSize: const Size(48, 48),
                        textStyle: tokens.action,
                      ),
                      onPressed: () => _open(source),
                      icon: const Icon(Icons.open_in_new, size: 18),
                      label: const Text('打开原文'),
                    ),
                  ),
                  const SizedBox(height: 16),
                ],
              ],
            ],
          ),
        ),
      ),
    );
  }
}
