import 'package:flutter/material.dart';
import 'moment_publication_controller.dart';

class MomentPublicationSheet extends StatefulWidget {
  const MomentPublicationSheet({
    super.key,
    required this.controller,
    this.withdrawal = false,
  });
  final MomentPublicationController controller;
  final bool withdrawal;
  @override
  State<MomentPublicationSheet> createState() => _MomentPublicationSheetState();
}

class _MomentPublicationSheetState extends State<MomentPublicationSheet> {
  bool _checked = false;
  MomentPublicPreview? _shown;
  @override
  void initState() {
    super.initState();
    widget.controller.load(withdrawal: widget.withdrawal);
  }

  Future<void> _confirm(MomentPublicPreview p) async {
    final ok = await widget.controller.confirm(p, checked: _checked);
    if (mounted && ok) Navigator.pop(context, true);
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.controller,
    builder: (context, _) {
      final c = widget.controller, p = c.preview;
      if (!identical(p, _shown)) {
        _shown = p;
        _checked = false;
      }
      return SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 24),
          child: SingleChildScrollView(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  widget.withdrawal ? '撤回这条公开记录' : '确认公开这条记录',
                  style: Theme.of(context).textTheme.titleLarge,
                ),
                const SizedBox(height: 12),
                Text(
                  widget.withdrawal
                      ? '撤回后将不再出现在地点公开摘要中。已被他人看到或转发的内容无法收回。'
                      : '下面的标题和正文会公开出现在关联地点，任何人都可能看到。私人关联、时间自述和坐标不会随摘要展示。',
                ),
                if (c.loading)
                  const Padding(
                    padding: EdgeInsets.all(16),
                    child: LinearProgressIndicator(),
                  ),
                if (p != null) ...[
                  const SizedBox(height: 16),
                  Text(p.title, style: Theme.of(context).textTheme.titleMedium),
                  if (p.body.isNotEmpty) Text(p.body),
                  Text('关联地点：${p.placeName}'),
                  Text('记录版本：${p.revision}'),
                  CheckboxListTile(
                    contentPadding: EdgeInsets.zero,
                    value: _checked,
                    onChanged: c.saving
                        ? null
                        : (v) => setState(() => _checked = v ?? false),
                    title: Text(
                      widget.withdrawal ? '我确认撤回此版本' : '我已检查，确认公开此版本',
                    ),
                  ),
                  FilledButton(
                    onPressed: _checked && !c.saving ? () => _confirm(p) : null,
                    child: Text(
                      c.saving
                          ? '正在提交…'
                          : widget.withdrawal
                          ? '确认撤回'
                          : '确认公开',
                    ),
                  ),
                ],
                if (c.error != null)
                  Semantics(liveRegion: true, child: Text(c.error!)),
                if (c.resolvedStatus != null)
                  FilledButton(
                    onPressed: () => Navigator.pop(context, true),
                    child: const Text('完成核对'),
                  ),
                if (p == null &&
                    !c.loading &&
                    !c.saving &&
                    c.resolvedStatus == null)
                  TextButton(
                    onPressed: () => c.load(withdrawal: widget.withdrawal),
                    child: Text(c.uncertain ? '重新读取当前状态' : '重新查看'),
                  ),
                TextButton(
                  onPressed: c.saving
                      ? null
                      : () => Navigator.pop(context, false),
                  child: const Text('取消'),
                ),
              ],
            ),
          ),
        ),
      );
    },
  );
}
