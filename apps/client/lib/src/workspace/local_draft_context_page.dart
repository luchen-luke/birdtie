import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'now_composer_draft_state.dart';
import 'typed_local_draft.dart';

/// A lease from an existing canonical reader. Titles are presentation only;
/// neither this object nor its approval is serialized into a local reference.
class LocalDraftReferenceCandidate {
  const LocalDraftReferenceCandidate({
    required this.kind,
    required this.id,
    required this.title,
    required this.version,
    required this.expiresAt,
    required this.sourceCurrent,
  });
  final String kind, id, title, version;
  final DateTime expiresAt;
  final bool Function() sourceCurrent;
  bool get current =>
      LocalDraftReference.kinds.contains(kind) &&
      id.isNotEmpty &&
      version.isNotEmpty &&
      sourceCurrent() &&
      DateTime.now().toUtc().isBefore(expiresAt);
}

String localDraftReferenceLabel(String kind) => switch (kind) {
  'person' => '人物',
  'place' => '地点',
  'activity' => '活动',
  'organization' => '组织',
  'business' => '商家',
  'community' => '社群',
  'journey' => '旅程',
  'theme_map' => '主题地图',
  'moment' => 'Moment',
  _ => '引用',
};

/// Edits only local selectors and the author's own annotation. Existing domain
/// writers still re-read the resource; an @ reference is never a send target.
class LocalDraftContextPage extends StatefulWidget {
  const LocalDraftContextPage({
    super.key,
    required this.drafts,
    required this.candidates,
    this.identityChanges,
    this.attachmentCurrent,
    this.canReselectAttachment,
    this.onReselectAttachment,
  });
  final NowComposerDraftState drafts;
  final List<LocalDraftReferenceCandidate> Function() candidates;
  final Listenable? identityChanges;
  final bool Function(LocalDraftAttachment)? attachmentCurrent;
  final bool Function(LocalDraftAttachment)? canReselectAttachment;
  final Future<void> Function(LocalDraftAttachment)? onReselectAttachment;
  @override
  State<LocalDraftContextPage> createState() => _LocalDraftContextPageState();
}

class _LocalDraftContextPageState extends State<LocalDraftContextPage> {
  late final Object _identity;
  late final NowComposerDraftState _drafts;
  bool _retired = false;
  late final Listenable _changes;
  final _ticks = ValueNotifier(0);
  Timer? _timer;
  String? _error;
  bool get current =>
      mounted &&
      !_retired &&
      identical(widget.drafts, _drafts) &&
      !widget.drafts.suspended &&
      widget.drafts.identity == _identity &&
      widget.drafts.envelope != null;
  @override
  void initState() {
    super.initState();
    _identity = widget.drafts.identity;
    _drafts = widget.drafts;
    _changes = Listenable.merge([
      widget.drafts,
      widget.identityChanges,
      _ticks,
    ]);
    _changes.addListener(_changed);
    _timer = Timer.periodic(const Duration(seconds: 1), (_) => _ticks.value++);
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  void didUpdateWidget(LocalDraftContextPage old) {
    super.didUpdateWidget(old);
    if (!identical(old.drafts, widget.drafts) ||
        !identical(old.candidates, widget.candidates) ||
        !identical(old.identityChanges, widget.identityChanges))
      _retired = true;
  }

  @override
  void dispose() {
    _changes.removeListener(_changed);
    _timer?.cancel();
    _ticks.dispose();
    super.dispose();
  }

  LocalDraftReferenceCandidate? _read(LocalDraftReference ref) {
    if (!current) return null;
    for (final c in widget.candidates()) {
      if (c.kind == ref.kind &&
          c.id == ref.id &&
          c.current &&
          (ref.version == null || c.version == ref.version))
        return c;
    }
    return null;
  }

  void _set(List<LocalDraftReference> rows) {
    if (!current) return;
    try {
      final draft = widget.drafts.envelope!;
      TypedLocalDraft.fromJson(draft.edit(references: rows).toJson());
      widget.drafts.setContext(references: List.unmodifiable(rows));
      _error = null;
    } catch (_) {
      setState(() => _error = '引用未保留，请检查数量或注释长度。');
    }
  }

  Future<void> _choose() async {
    if (!current) return;
    await showModalBottomSheet<void>(
      context: context,
      useSafeArea: true,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (sheet) => AnimatedBuilder(
        animation: _changes,
        builder: (context, _) {
          final rows = current
              ? widget.candidates().where((c) => c.current).toList()
              : <LocalDraftReferenceCandidate>[];
          final maximumHeight = MediaQuery.sizeOf(context).height * .72;
          final contentHeight = rows.isEmpty ? 152.0 : 80.0 + rows.length * 80;
          return SizedBox(
            height: contentHeight < maximumHeight
                ? contentHeight
                : maximumHeight,
            child: Column(
              children: [
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: Text('添加当前引用'),
                ),
                Expanded(
                  child: !current
                      ? const Center(child: Text('当前草稿已退休'))
                      : rows.isEmpty
                      ? const Center(
                          child: Padding(
                            padding: EdgeInsets.symmetric(horizontal: 16),
                            child: Text('当前没有可引用的结果。请返回 Now 读取结果。'),
                          ),
                        )
                      : ListView.builder(
                          itemCount: rows.length,
                          itemBuilder: (context, i) {
                            final c = rows[i],
                                refs = widget.drafts.envelope!.references;
                            final selected = refs.any(
                              (r) => r.kind == c.kind && r.id == c.id,
                            );
                            return ListTile(
                              title: Text(c.title),
                              subtitle: Text(localDraftReferenceLabel(c.kind)),
                              enabled:
                                  current &&
                                  c.current &&
                                  !selected &&
                                  refs.length < 50,
                              trailing: Icon(
                                selected ? Icons.check : Icons.alternate_email,
                              ),
                              onTap: () {
                                // Recheck the exact original reader lease, not a newer
                                // candidate with the same ID, when a callback is captured.
                                if (!current ||
                                    !c.current ||
                                    selected ||
                                    widget.drafts.envelope!.references.length >=
                                        50 ||
                                    widget.drafts.envelope!.references.any(
                                      (r) => r.kind == c.kind && r.id == c.id,
                                    ))
                                  return;
                                _set([
                                  ...widget.drafts.envelope!.references,
                                  LocalDraftReference(
                                    kind: c.kind,
                                    id: c.id,
                                    version: c.version,
                                  ),
                                ]);
                                if (current) Navigator.pop(sheet);
                              },
                            );
                          },
                        ),
                ),
              ],
            ),
          );
        },
      ),
    );
  }

  Future<void> _annotation(LocalDraftReference ref) async {
    if (!current) return;
    final draftVersion = widget.drafts.contentVersion;
    final field = TextEditingController(text: ref.snapshotText);
    final changes = _changes;
    final route = DialogRoute<String>(
      context: context,
      builder: (context) => AnimatedBuilder(
        animation: changes,
        builder: (context, _) {
          final valid = current && widget.drafts.contentVersion == draftVersion;
          return AlertDialog(
            title: const Text('本人注释快照'),
            content: valid
                ? TextField(
                    controller: field,
                    minLines: 2,
                    maxLines: 5,
                    decoration: const InputDecoration(
                      labelText: '自己的注释',
                      helperText: '只保存本人文字，不复制来源正文。',
                    ),
                  )
                : const Text('草稿已变化，请关闭后重新编辑。'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: valid
                    ? () {
                        if (current &&
                            widget.drafts.contentVersion == draftVersion &&
                            utf8.encode(field.text).length <= 8000)
                          Navigator.pop(context, field.text);
                      }
                    : null,
                child: const Text('保留注释'),
              ),
            ],
          );
        },
      ),
    );
    var released = false;
    void release() {
      if (!released) {
        released = true;
        field.dispose();
      }
    }

    unawaited(route.completed.then((_) => release()));
    try {
      final note = await Navigator.of(context).push(route);
      if (note != null &&
          current &&
          widget.drafts.contentVersion == draftVersion) {
        _set(
          widget.drafts.envelope!.references
              .map(
                (r) => identical(r, ref)
                    ? LocalDraftReference(
                        kind: r.kind,
                        id: r.id,
                        version: r.version,
                        mode: 'snapshot',
                        snapshotText: note,
                        purpose: r.purpose,
                      )
                    : r,
              )
              .toList(),
        );
      }
      await route.completed;
    } finally {
      release();
    }
  }

  @override
  Widget build(BuildContext context) {
    final draft = current ? widget.drafts.envelope : null;
    return Scaffold(
      appBar: AppBar(title: const Text('草稿引用与材料')),
      body: draft == null
          ? const Center(child: Text('当前草稿已退休，请返回 Now 重新打开。'))
          : ListView(
              padding: const EdgeInsets.all(16),
              children: [
                Text(
                  draft.recipientID == null
                      ? '实际发送目标：本人 Agent'
                      : '实际发送目标：${widget.drafts.target.displayName ?? '所选会话'}',
                ),
                const SizedBox(height: 12),
                const Text('@ 引用保留在本机，不会改变收件人，也不会自动随文字发送。'),
                const SizedBox(height: 16),
                OutlinedButton.icon(
                  onPressed: draft.references.length < 50 ? _choose : null,
                  icon: const Icon(Icons.alternate_email),
                  label: const Text('添加当前引用'),
                ),
                if (_error != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(_error!),
                  ),
                for (final ref in draft.references) ...[
                  const Divider(height: 24),
                  Text(
                    _read(ref)?.title ??
                        '${localDraftReferenceLabel(ref.kind)} · 当前来源未核验',
                  ),
                  Text(ref.id, style: Theme.of(context).textTheme.bodySmall),
                  Text(ref.mode == 'snapshot' ? '本人注释快照' : '动态引用 · 使用前重验来源'),
                  if (ref.mode == 'snapshot' && ref.snapshotText.isNotEmpty)
                    Text(ref.snapshotText),
                  Wrap(
                    spacing: 8,
                    runSpacing: 4,
                    children: [
                      TextButton(
                        onPressed: () => _annotation(ref),
                        child: const Text('编辑本人注释'),
                      ),
                      if (ref.mode == 'snapshot')
                        TextButton(
                          onPressed: () => current
                              ? _set(
                                  widget.drafts.envelope!.references
                                      .map(
                                        (r) => identical(r, ref)
                                            ? LocalDraftReference(
                                                kind: r.kind,
                                                id: r.id,
                                                version: r.version,
                                                purpose: r.purpose,
                                              )
                                            : r,
                                      )
                                      .toList(),
                                )
                              : null,
                          child: const Text('改为动态引用'),
                        ),
                      TextButton(
                        onPressed: () => current
                            ? _set(
                                widget.drafts.envelope!.references
                                    .where((r) => !identical(r, ref))
                                    .toList(),
                              )
                            : null,
                        child: const Text('移除引用'),
                      ),
                    ],
                  ),
                ],
                if (draft.attachments.isNotEmpty) ...[
                  const Divider(height: 24),
                  const Text('附件记录 · 使用前重新选择或核实'),
                  for (final a in draft.attachments) ...[
                    ListTile(
                      contentPadding: EdgeInsets.zero,
                      title: Text(switch (a.kind) {
                        'image' => '图片',
                        'video' => '视频',
                        'audio' => '语音',
                        _ => '文件',
                      }),
                      subtitle: Text(
                        '${switch (a.state) {
                          'uploaded' => '曾上传，当前未核验',
                          'unknown' => '结果待核实',
                          'failed' => '未完成',
                          'unavailable' => '当前不可用',
                          _ => widget.attachmentCurrent?.call(a) == true ? '本机已选择，发送须另行确认' : '仅有选择记录，请重新选择',
                        }}',
                      ),
                    ),
                    if (widget.onReselectAttachment != null &&
                        widget.canReselectAttachment?.call(a) == true)
                      Align(
                        alignment: Alignment.centerLeft,
                        child: TextButton(
                          onPressed: () {
                            if (current &&
                                widget.canReselectAttachment?.call(a) == true) {
                              unawaited(widget.onReselectAttachment!(a));
                            }
                          },
                          child: const Text('重新选择图片'),
                        ),
                      ),
                  ],
                ],
              ],
            ),
    );
  }
}
