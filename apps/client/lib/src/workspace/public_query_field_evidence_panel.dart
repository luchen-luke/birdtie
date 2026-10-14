import 'dart:async';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import 'agent_result_projection.dart';
import 'public_query_field_evidence.dart';

/// Only expands a local description; original domain buttons stay independent.
class PublicQueryFieldEvidencePanel extends StatefulWidget {
  const PublicQueryFieldEvidencePanel({
    super.key,
    required this.evidence,
    required this.item,
    required this.current,
    required this.changes,
  });
  final PublicQueryFieldEvidence evidence;
  final AgentResultItem item;
  final bool Function(AgentResultItem) current;
  final Listenable changes;
  @override
  State<PublicQueryFieldEvidencePanel> createState() =>
      _PublicQueryFieldEvidencePanelState();
}

class _PublicQueryFieldEvidencePanelState
    extends State<PublicQueryFieldEvidencePanel> {
  bool _expanded = false, _retired = false;
  Timer? _timer;
  @override
  void initState() {
    super.initState();
    widget.changes.addListener(_changed);
    _schedule();
  }

  bool get _live {
    if (_retired || !widget.current(widget.item) || !widget.evidence.current) {
      _retired = true;
      widget.evidence.retire();
      return false;
    }
    return true;
  }

  void _schedule() {
    _timer?.cancel();
    if (!_live || widget.evidence.invalid) return;
    _timer = Timer(widget.evidence.remaining, () {
      if (!mounted) return;
      widget.evidence.retire();
      _retired = true;
      setState(() {});
    });
  }

  void _changed() {
    if (!mounted) return;
    _live;
    setState(() {});
  }

  @override
  void didUpdateWidget(PublicQueryFieldEvidencePanel old) {
    super.didUpdateWidget(old);
    if (old.changes != widget.changes) {
      old.changes.removeListener(_changed);
      widget.changes.addListener(_changed);
    }
    if (!identical(old.evidence, widget.evidence)) {
      old.evidence.retire();
      _retired = false;
      _expanded = false;
    } else if (!identical(old.item, widget.item)) {
      widget.evidence.retire();
      _retired = true;
    } else if (old.current != widget.current || old.changes != widget.changes) {
      if (!_live) _retired = true;
    }
    _schedule();
  }

  @override
  void dispose() {
    widget.changes.removeListener(_changed);
    _timer?.cancel();
    super.dispose();
  }

  String _stamp(DateTime t) {
    final local = t.toLocal();
    String two(int n) => n.toString().padLeft(2, '0');
    return '${local.year}年${local.month}月${local.day}日 ${two(local.hour)}:${two(local.minute)}';
  }

  @override
  Widget build(BuildContext context) {
    final live = _live;
    final evidence = widget.evidence,
        detail = evidence.times[widget.item.entity.id];
    final reference = Uri.tryParse(detail?.sourceReference ?? '');
    final link =
        reference != null &&
            reference.scheme == 'https' &&
            reference.host.isNotEmpty &&
            reference.userInfo.isEmpty
        ? reference
        : null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        TextButton.icon(
          key: Key('public-evidence-${widget.item.entity.mapID}'),
          style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
          onPressed: () => setState(() => _expanded = !_expanded),
          icon: Icon(_expanded ? Icons.expand_less : Icons.info_outline),
          label: Text(_expanded ? '收起来源说明' : '查看来源与时间'),
        ),
        if (_expanded)
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (!live)
                  const Text('来源说明已失效，请重新查询。')
                else if (evidence.invalid)
                  const Text('来源说明暂不可用。')
                else ...[
                  if (detail != null) ...[
                    Text(
                      detail.sourceLabel.isNotEmpty
                          ? detail.sourceLabel
                          : evidence.queryKind == 'activity'
                          ? '公开活动资料'
                          : '公开地点资料',
                    ),
                    Text('来源更新时间：${_stamp(detail.updatedAt)}'),
                    if (detail.sourceExpiresAt != null)
                      Text('来源到期时间：${_stamp(detail.sourceExpiresAt!)}'),
                    if (detail.freshness == 'unverified' ||
                        detail.freshness == 'review_needed')
                      const Text('此来源尚未核实。'),
                    if (link != null)
                      TextButton.icon(
                        onPressed: () {
                          if (_live)
                            unawaited(
                              launchUrl(
                                link,
                                mode: LaunchMode.externalApplication,
                              ),
                            );
                        },
                        icon: const Icon(Icons.open_in_new),
                        label: const Text('打开来源'),
                      ),
                    if (evidence.queryKind == 'activity')
                      const Text('活动资料不代表实际出席。'),
                  ] else
                    const Text('此结果暂无可核对的来源。'),
                  if ((evidence.omitted['EXPIRED_SOURCE'] ?? 0) > 0)
                    const Text('部分结果的来源已过期。'),
                  if ((evidence.omitted['UNKNOWN_SOURCE_TIME'] ?? 0) > 0)
                    const Text('部分来源未提供更新时间。'),
                  if ((evidence.omitted['SOURCE_METADATA_UNAVAILABLE'] ?? 0) >
                      0)
                    const Text('部分来源暂不可用。'),
                ],
              ],
            ),
          ),
      ],
    );
  }
}
