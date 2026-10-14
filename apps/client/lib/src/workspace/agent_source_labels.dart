import 'dart:async';

import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import 'agent_answer_sources.dart';

/// Presentation only: a citation never supplies a new source or authority.
class AgentCitationText extends StatefulWidget {
  const AgentCitationText({
    super.key,
    required this.text,
    required this.sources,
    required this.current,
    this.style,
    this.changes,
    this.remaining,
    this.openSource,
  });
  final String text;
  final List<AgentAnswerSource> sources;
  final bool Function() current;
  final TextStyle? style;
  final Listenable? changes;
  final Duration? Function()? remaining;
  final Future<bool> Function(Uri)? openSource;

  @override
  State<AgentCitationText> createState() => _AgentCitationTextState();
}

class _AgentCitationTextState extends State<AgentCitationText> {
  Timer? _expiry;
  void _changed() {
    if (mounted) setState(() {});
  }

  void _schedule() {
    _expiry?.cancel();
    if (widget.remaining?.call() case final duration?
        when duration > Duration.zero) {
      _expiry = Timer(duration, _changed);
    }
  }

  @override
  void initState() {
    super.initState();
    widget.changes?.addListener(_changed);
    _schedule();
  }

  @override
  void didUpdateWidget(AgentCitationText old) {
    super.didUpdateWidget(old);
    if (old.changes != widget.changes) {
      old.changes?.removeListener(_changed);
      widget.changes?.addListener(_changed);
    }
    if (!identical(old.sources, widget.sources)) _schedule();
  }

  @override
  void dispose() {
    widget.changes?.removeListener(_changed);
    _expiry?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    // Full-width/space-spoofed markers are not promoted to verified links.
    final marker = RegExp(r'[\[［](?:source[^\]］\r\n]*|[0-9]+)[\]］]');
    final matches = marker.allMatches(widget.text).toList();
    if (matches.isEmpty) return Text(widget.text, style: widget.style);
    final spans = <InlineSpan>[];
    var offset = 0;
    final current = widget.current();
    for (final match in matches) {
      if (match.start > offset) {
        spans.add(TextSpan(text: widget.text.substring(offset, match.start)));
      }
      final token = match.group(0)!;
      var index = RegExp(r'^\[source-[1-9][0-9]*\]$').hasMatch(token)
          ? widget.sources.indexWhere((s) => token == '[${s.id}]')
          : -1;
      if (index < 0 && RegExp(r'^\[[1-9][0-9]*\]$').hasMatch(token)) {
        final ordinal = int.tryParse(token.substring(1, token.length - 1));
        if (ordinal != null && ordinal <= widget.sources.length) {
          index = ordinal - 1;
        }
      }
      if (index < 0) {
        spans.add(const TextSpan(text: '（引用未核验）'));
      } else {
        final source = widget.sources[index];
        spans.add(
          WidgetSpan(
            alignment: PlaceholderAlignment.middle,
            child: TextButton(
              key: ValueKey('agent-inline-source-${source.id}-${match.start}'),
              style: TextButton.styleFrom(
                minimumSize: const Size(48, 48),
                padding: const EdgeInsets.symmetric(horizontal: 8),
              ),
              onPressed: !current
                  ? null
                  : () => showAgentSourceDetails(
                      context,
                      source: source,
                      current: widget.current,
                      changes: widget.changes,
                      remaining: widget.remaining?.call(),
                      openSource: widget.openSource,
                    ),
              child: Text('来源 ${index + 1}'),
            ),
          ),
        );
      }
      offset = match.end;
    }
    if (offset < widget.text.length) {
      spans.add(TextSpan(text: widget.text.substring(offset)));
    }
    return Text.rich(TextSpan(children: spans), style: widget.style);
  }
}

/// Complete metadata remains reachable without putting long URLs in cards.
class AgentSourceMetadata extends StatelessWidget {
  const AgentSourceMetadata({
    super.key,
    required this.source,
    required this.onOpen,
    this.updatedAt,
    this.showMetadata = true,
    this.maintainer,
  });
  final AgentAnswerSource source;
  final VoidCallback? onOpen;
  final String? updatedAt;
  final String? maintainer;
  final bool showMetadata;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      if (showMetadata) ...[
        Text(source.title, style: Theme.of(context).textTheme.titleSmall),
        if (source.site.isNotEmpty) Text('来源原名：${source.site}'),
        if (maintainer?.isNotEmpty == true) Text('资料维护：$maintainer'),
        Text('域名：${source.uri.host}'),
        Text('发布时间：${source.publishedAt.isEmpty ? '未提供' : source.publishedAt}'),
        if (updatedAt != null) Text('更新时间：$updatedAt'),
        SelectableText(source.uri.toString()),
      ],
      Align(
        alignment: Alignment.centerLeft,
        child: TextButton.icon(
          key: ValueKey('agent-answer-source-${source.id}'),
          style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
          onPressed: onOpen,
          icon: const Icon(Icons.open_in_new, size: 18),
          label: const Text('打开原文'),
        ),
      ),
    ],
  );
}

Future<void> showAgentSourceDetails(
  BuildContext context, {
  required AgentAnswerSource source,
  required bool Function() current,
  Listenable? changes,
  Duration? remaining,
  String? updatedAt,
  String? maintainer,
  Future<bool> Function(Uri)? openSource,
}) async {
  if (!current()) return;
  await showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (context) => _AgentSourceDetails(
      source: source,
      current: current,
      changes: changes,
      remaining: remaining,
      updatedAt: updatedAt,
      maintainer: maintainer,
      openSource: openSource,
    ),
  );
}

class _AgentSourceDetails extends StatefulWidget {
  const _AgentSourceDetails({
    required this.source,
    required this.current,
    this.changes,
    this.remaining,
    this.updatedAt,
    this.maintainer,
    this.openSource,
  });
  final AgentAnswerSource source;
  final bool Function() current;
  final Listenable? changes;
  final Duration? remaining;
  final String? updatedAt;
  final String? maintainer;
  final Future<bool> Function(Uri)? openSource;
  @override
  State<_AgentSourceDetails> createState() => _AgentSourceDetailsState();
}

class _AgentSourceDetailsState extends State<_AgentSourceDetails> {
  Timer? _expiry;
  bool _retired = false, _opening = false;
  bool get _current {
    if (!widget.current()) _retired = true;
    return !_retired;
  }

  void _changed() {
    _current;
    if (mounted) setState(() {});
  }

  @override
  void initState() {
    super.initState();
    widget.changes?.addListener(_changed);
    if (widget.remaining case final duration? when duration > Duration.zero) {
      _expiry = Timer(duration, () {
        _retired = true;
        if (mounted) setState(() {});
      });
    }
  }

  @override
  void dispose() {
    widget.changes?.removeListener(_changed);
    _expiry?.cancel();
    super.dispose();
  }

  Future<void> _open() async {
    if (!_current || _opening) return;
    setState(() => _opening = true);
    bool opened;
    try {
      opened =
          await (widget.openSource?.call(widget.source.uri) ??
              launchUrl(
                widget.source.uri,
                mode: LaunchMode.externalApplication,
              ));
    } catch (_) {
      opened = false;
    }
    if (!mounted) return;
    setState(() => _opening = false);
    if (_current && !opened) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('暂时无法打开来源链接。')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final current = _current;
    return ConstrainedBox(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.sizeOf(context).height * .8,
      ),
      child: SingleChildScrollView(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  const Expanded(child: Text('来源详情')),
                  IconButton(
                    tooltip: '关闭来源详情',
                    onPressed: () => Navigator.of(context).pop(),
                    icon: const Icon(Icons.close),
                  ),
                ],
              ),
              if (!current) const Text('来源已失效，请重新查询。'),
              AgentSourceMetadata(
                source: widget.source,
                showMetadata: current,
                updatedAt: widget.updatedAt,
                maintainer: widget.maintainer,
                onOpen: current && !_opening ? _open : null,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
