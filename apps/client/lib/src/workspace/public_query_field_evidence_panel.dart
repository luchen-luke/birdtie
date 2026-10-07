import 'dart:async';
import 'package:flutter/material.dart';
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
    return '${local.year}年${local.month}月${local.day}日 ${two(local.hour)}:${two(local.minute)}:${two(local.second)}（本机时间）';
  }

  @override
  Widget build(BuildContext context) {
    final live = _live;
    final evidence = widget.evidence,
        detail = evidence.times[widget.item.entity.id];
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
                  const Text('来源说明已失效，请重新查询后查看。原内容与操作保留。')
                else if (evidence.invalid)
                  const Text('来源说明暂不可用。可继续查看原内容及详情。')
                else ...[
                  const Text('本次公开来源记录；说明只读，不提供身份、到访或操作权限。'),
                  if (detail != null) ...[
                    Text('来源更新时间：${_stamp(detail.updatedAt)}'),
                    Text(
                      detail.sourceExpiresAt == null
                          ? '来源到期时间：未提供'
                          : '来源到期时间：${_stamp(detail.sourceExpiresAt!)}',
                    ),
                  ] else
                    const Text('当前内容未提供可展开的公开字段来源。非公开来源不会自动标为公开。'),
                  if ((evidence.omitted['EXPIRED_SOURCE'] ?? 0) > 0)
                    const Text('部分来源已过期，其附加字段说明不可用。'),
                  if ((evidence.omitted['UNKNOWN_SOURCE_TIME'] ?? 0) > 0)
                    const Text('部分来源更新时间未提供，保持未知。'),
                  if ((evidence.omitted['SOURCE_METADATA_UNAVAILABLE'] ?? 0) >
                      0)
                    const Text('部分公开来源说明暂不可用。'),
                  if ((evidence.omitted['OMITTED_BUDGET'] ?? 0) > 0)
                    Text(
                      '因说明大小限制省略了 ${evidence.omitted['OMITTED_BUDGET']} 个对象的字段说明；原结果保留。',
                    ),
                  Text('本次读取时间：${_stamp(evidence.observedAt!)}'),
                  Text('本次说明读取期限：${_stamp(evidence.readExpiresAt!)}'),
                  const Text('拍摄时间：未采集。更新时间和读取时间不表示发生、出席、到访或当前位置。'),
                  const Text('本次未提供跨来源冲突核对，不代表所有声明都没有冲突。'),
                ],
              ],
            ),
          ),
      ],
    );
  }
}
