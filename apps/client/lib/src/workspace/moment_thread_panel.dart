import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'moment_thread_api.dart';
import 'moment_thread_controller.dart';
import 'moment_thread_pending_store.dart';

/// Now owns the only text input. This panel changes its active entry and shows
/// exact ordered previews, while all publication uses the thread contract.
class MomentThreadPanel extends StatefulWidget {
  const MomentThreadPanel({
    super.key,
    required this.controller,
    required this.onActiveText,
    this.onClose,
    this.onPublished,
    this.entryMaterials,
    this.materialsReady = true,
  });
  final MomentThreadController controller;
  final ValueChanged<String> onActiveText;
  final VoidCallback? onClose;
  final ValueChanged<MomentThreadReceipt>? onPublished;
  final Widget? entryMaterials;
  final bool materialsReady;
  @override
  State<MomentThreadPanel> createState() => _MomentThreadPanelState();
}

class _MomentThreadPanelState extends State<MomentThreadPanel> {
  MomentThreadReceipt? _delivered;
  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_changed);
  }

  void _changed() {
    if (!mounted) {
      return;
    }
    final result = widget.controller.receipt;
    if (result != null &&
        result.status == 'SUCCEEDED' &&
        !identical(_delivered, result)) {
      _delivered = result;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.controller.current) {
          widget.onPublished?.call(result);
        }
      });
    }
    setState(() {});
  }

  @override
  void didUpdateWidget(covariant MomentThreadPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.controller, widget.controller)) {
      oldWidget.controller.removeListener(_changed);
      _delivered = null;
      widget.controller.addListener(_changed);
    }
  }

  @override
  void dispose() {
    widget.controller.removeListener(_changed);
    super.dispose();
  }

  void _edit(VoidCallback action) {
    action();
    widget.onActiveText(widget.controller.activeText);
  }

  @override
  Widget build(BuildContext context) {
    final c = widget.controller, preview = widget.controller.previewValue;
    if (!c.current) {
      return const SizedBox.shrink();
    }
    return SingleChildScrollView(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Expanded(
                  child: Text(
                    '串文',
                    style: TextStyle(fontWeight: FontWeight.w600),
                  ),
                ),
                if (widget.onClose != null)
                  IconButton(
                    tooltip: '收起串文草稿',
                    onPressed: c.busy || c.unsavedDraft && c.pending == null
                        ? null
                        : widget.onClose,
                    icon: const Icon(Icons.close),
                  ),
              ],
            ),
            const Text('所有条目共用公开范围。选择一条后，在首页原输入框继续修改。'),
            if (c.busy) const LinearProgressIndicator(),
            if (c.notice != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Text(c.notice!, key: const ValueKey('thread-notice')),
              ),
            if (c.uncertain)
              Wrap(
                spacing: 8,
                children: [
                  TextButton(
                    onPressed: c.busy ? null : () => c.recover(),
                    child: const Text('核实原操作'),
                  ),
                  TextButton(
                    onPressed: c.busy ? null : () => c.recover(stop: true),
                    child: const Text('停止原操作'),
                  ),
                ],
              ),
            if (preview == null && !c.uncertain) ...[
              if (widget.entryMaterials != null) widget.entryMaterials!,
              for (var index = 0; index < c.entries.length; index++)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 4),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        child: TextButton(
                          style: TextButton.styleFrom(
                            alignment: Alignment.centerLeft,
                            backgroundColor: index == c.activeIndex
                                ? Theme.of(context).colorScheme.primaryContainer
                                      .withValues(alpha: .55)
                                : null,
                          ),
                          onPressed: c.editable
                              ? () => _edit(() => c.selectEntry(index))
                              : null,
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                '第${index + 1}条${index == c.activeIndex ? ' · 正在编辑' : ''}',
                              ),
                              Text(
                                c.entries[index].text.isEmpty
                                    ? '待输入内容'
                                    : c.entries[index].text,
                                maxLines: 2,
                                overflow: TextOverflow.ellipsis,
                              ),
                              if (c.entries[index].media.isNotEmpty)
                                Text('${c.entries[index].media.length}张已准备图片'),
                            ],
                          ),
                        ),
                      ),
                      IconButton(
                        tooltip: '向前移动第${index + 1}条',
                        onPressed: c.editable && index > 0
                            ? () => _edit(() => c.moveEntry(index, index - 1))
                            : null,
                        icon: const Icon(Icons.arrow_upward, size: 18),
                      ),
                      IconButton(
                        tooltip: '向后移动第${index + 1}条',
                        onPressed: c.editable && index + 1 < c.entries.length
                            ? () => _edit(() => c.moveEntry(index, index + 1))
                            : null,
                        icon: const Icon(Icons.arrow_downward, size: 18),
                      ),
                      IconButton(
                        tooltip: '删除草稿第${index + 1}条',
                        onPressed: c.editable && c.entries.length > 2
                            ? () => _edit(() => c.removeEntry(index))
                            : null,
                        icon: const Icon(Icons.remove_circle_outline, size: 18),
                      ),
                    ],
                  ),
                ),
              Wrap(
                spacing: 8,
                children: [
                  TextButton(
                    onPressed: c.editable && c.entries.length < 20
                        ? () => _edit(c.addEntry)
                        : null,
                    child: const Text('添加一条'),
                  ),
                  FilledButton(
                    onPressed:
                        c.editable &&
                            widget.materialsReady &&
                            c.entries.length >= 2
                        ? () => c.preview()
                        : null,
                    child: const Text('预览整串'),
                  ),
                ],
              ),
            ],
            if (!widget.materialsReady && preview == null && !c.uncertain)
              const Text('请先准备各条材料，或移除尚未准备的材料后预览整串。'),
            if (preview != null) ...[
              Text(
                preview.input.action == 'WITHDRAW'
                    ? '将一次撤回整串当前内容'
                    : '将一次公开以下${preview.input.entries.length}条内容',
                style: const TextStyle(fontWeight: FontWeight.w600),
              ),
              if (preview.input.action == 'WITHDRAW') ...[
                const Text('已单独撤回的条目保持原状态和位置，其余条目将全部撤回。单条后续修改会使这次批准失效。'),
                for (final member in preview.members)
                  Text(
                    '第${member.position + 1}条 · 版本${member.revision} · ${member.status == 'withdrawn' ? '已撤回' : '当前公开'}',
                  ),
              ] else
                for (
                  var index = 0;
                  index < preview.input.entries.length;
                  index++
                )
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '第${index + 1}条 · ${preview.contexts[index]['cityName']}${preview.contexts[index]['placeName'] == '' ? '' : ' · ${preview.contexts[index]['placeName']}'} · 公开',
                          style: const TextStyle(fontWeight: FontWeight.w600),
                        ),
                        Text(preview.input.entries[index].title),
                        Text(preview.input.entries[index].text.trim()),
                        if (preview.materials[index].isNotEmpty)
                          Wrap(
                            spacing: 8,
                            runSpacing: 8,
                            children: [
                              for (final image in preview.materials[index])
                                Image.memory(
                                  base64Decode(image['thumbnailBase64']),
                                  width: 128,
                                  height: 72,
                                  fit: BoxFit.contain,
                                  semanticLabel: '整串批准中的规范图片',
                                ),
                            ],
                          ),
                      ],
                    ),
                  ),
              Wrap(
                spacing: 8,
                children: [
                  TextButton(
                    onPressed: c.busy ? null : c.cancelPreview,
                    child: const Text('返回修改'),
                  ),
                  FilledButton(
                    onPressed: c.busy || !preview.current ? null : c.confirm,
                    child: Text(
                      preview.input.action == 'WITHDRAW' ? '确认撤回整串' : '确认公开整串',
                    ),
                  ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class MomentThreadSidecar extends StatefulWidget {
  const MomentThreadSidecar({
    super.key,
    required this.api,
    required this.momentID,
    required this.onOpenMoment,
    this.ownerID,
    this.journal,
  });
  final MomentThreadAPI api;
  final String momentID;
  final ValueChanged<String> onOpenMoment;
  final String? ownerID;
  final MomentThreadJournal? journal;
  @override
  State<MomentThreadSidecar> createState() => _MomentThreadSidecarState();
}

class _MomentThreadSidecarState extends State<MomentThreadSidecar> {
  MomentThreadView? _view;
  MomentThreadController? _control;
  String? _notice;
  Timer? _lease;
  int _epoch = 0;
  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(covariant MomentThreadSidecar oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.api, widget.api) ||
        oldWidget.momentID != widget.momentID ||
        oldWidget.ownerID != widget.ownerID) {
      _epoch++;
      _control?.dispose();
      _control = null;
      _view = null;
      _lease?.cancel();
      _load();
    }
  }

  Future<void> _load() async {
    final epoch = ++_epoch, api = widget.api;
    try {
      final view = await api.view(widget.momentID);
      if (!mounted || epoch != _epoch || !api.current()) {
        return;
      }
      _view = view;
      _notice = null;
      _lease?.cancel();
      _lease = Timer(view.validUntil.difference(DateTime.now().toUtc()), () {
        if (mounted && epoch == _epoch) {
          setState(() {
            _view = null;
            _notice = '串文位置已到期，请重新读取。';
          });
        }
      });
      if (view.canWithdraw && widget.ownerID != null && _control == null) {
        _control = MomentThreadController(
          api: api,
          ownerID: widget.ownerID!,
          journal: widget.journal,
        );
        await _control!.restore();
      }
      if (mounted && epoch == _epoch) {
        setState(() {});
      }
    } catch (error) {
      if (!mounted || epoch != _epoch) {
        return;
      }
      setState(() {
        _view = null;
        _notice = error is MomentThreadHTTPFailure && error.status == 404
            ? null
            : '串文位置暂不可读取。';
      });
    }
  }

  @override
  void dispose() {
    _epoch++;
    _lease?.cancel();
    _control?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final view = _view;
    if (!widget.api.current()) {
      return const SizedBox.shrink();
    }
    if (view == null || !view.current) {
      if (_control?.uncertain == true) {
        return MomentThreadPanel(controller: _control!, onActiveText: (_) {});
      }
      return _notice == null
          ? const SizedBox.shrink()
          : Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(_notice!),
                TextButton(onPressed: _load, child: const Text('重新读取串文')),
              ],
            );
    }
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text('同一串文', style: TextStyle(fontWeight: FontWeight.w600)),
        Wrap(
          spacing: 8,
          children: [
            for (final member in view.members)
              TextButton(
                onPressed:
                    member['momentId'] == null ||
                        member['momentId'] == widget.momentID
                    ? null
                    : () {
                        if (identical(_view, view) &&
                            view.current &&
                            widget.api.current()) {
                          widget.onOpenMoment(member['momentId']);
                        }
                      },
                child: Text(
                  member['momentId'] == null
                      ? '第${member['position'] + 1}条 · 暂不可见'
                      : '第${member['position'] + 1}条${member['momentId'] == widget.momentID ? ' · 当前' : ''}',
                ),
              ),
          ],
        ),
        if (view.canWithdraw && _control != null) ...[
          TextButton(
            onPressed: _control!.busy || _control!.pending != null
                ? null
                : () => _control!.previewWithdraw(view.threadID),
            child: const Text('预览撤回整串'),
          ),
          ListenableBuilder(
            listenable: _control!,
            builder: (context, _) =>
                _control!.previewValue != null ||
                    _control!.uncertain ||
                    _control!.notice != null
                ? MomentThreadPanel(
                    controller: _control!,
                    onActiveText: (_) {},
                    onPublished: (_) => _load(),
                  )
                : const SizedBox.shrink(),
          ),
        ],
      ],
    );
  }
}
