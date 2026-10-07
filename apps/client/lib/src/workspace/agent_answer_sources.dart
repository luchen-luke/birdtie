import 'dart:async';
import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

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
    this._current,
  );
  final String runID, sourceEvidenceDigest;
  final List<AgentAnswerSource> sources;
  final bool Function() _current;
  bool _retired = false;
  bool get current {
    if (_retired || !_current()) _retired = true;
    return !_retired;
  }

  static AgentMessageSources? read(
    Map<String, dynamic> message, {
    bool Function()? current,
  }) {
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
  }) : sources = List.unmodifiable(sources),
       _now = now,
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
    );
  }
}

class AgentAnswerSourcesPanel extends StatefulWidget {
  const AgentAnswerSourcesPanel({
    super.key,
    required this.answer,
    required this.replyCurrent,
    this.openSource,
  }) : persisted = null;
  const AgentAnswerSourcesPanel.persisted({
    super.key,
    required AgentMessageSources sources,
    required this.replyCurrent,
    this.openSource,
  }) : persisted = sources,
       answer = null;
  final AgentAnswerSources? answer;
  final AgentMessageSources? persisted;
  final bool Function() replyCurrent;
  final Future<bool> Function(Uri)? openSource;

  @override
  State<AgentAnswerSourcesPanel> createState() =>
      _AgentAnswerSourcesPanelState();
}

class _AgentAnswerSourcesPanelState extends State<AgentAnswerSourcesPanel> {
  Timer? _expiry;
  void _schedule() {
    _expiry?.cancel();
    final remaining = widget.answer?.remaining;
    if (remaining != null && remaining > Duration.zero) {
      _expiry = Timer(remaining, () {
        if (mounted) setState(() {});
      });
    }
  }

  @override
  void initState() {
    super.initState();
    _schedule();
  }

  @override
  void didUpdateWidget(AgentAnswerSourcesPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.answer, widget.answer)) _schedule();
  }

  @override
  void dispose() {
    _expiry?.cancel();
    super.dispose();
  }

  bool get _current =>
      widget.replyCurrent() &&
      (widget.answer?.current ?? widget.persisted!.current);
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
    return Padding(
      key: const Key('agent-answer-sources'),
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            current
                ? (widget.persisted == null ? '本轮回答来源' : '这条回答的来源')
                : '来源已失效，请重新查询。',
            style: Theme.of(context).textTheme.labelLarge,
          ),
          for (final (index, source) in _sources.indexed)
            TextButton.icon(
              key: ValueKey('agent-answer-source-${source.id}'),
              style: TextButton.styleFrom(
                minimumSize: const Size(48, 48),
                alignment: Alignment.centerLeft,
              ),
              onPressed: current ? () => _open(source) : null,
              icon: const Icon(Icons.open_in_new, size: 18),
              label: Text(
                '[${index + 1}] ${source.title}${source.site.isEmpty ? '' : ' · ${source.site}'}',
              ),
            ),
        ],
      ),
    );
  }
}
