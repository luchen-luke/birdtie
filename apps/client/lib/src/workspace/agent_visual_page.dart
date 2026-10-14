import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import '../app/birdtie_app_bar.dart';
import 'agent_visual_controller.dart';
import 'local_visual_draft.dart';

class AgentVisualPage extends StatefulWidget {
  const AgentVisualPage({
    super.key,
    required this.controller,
    this.onSaveLocalDraft,
  });
  final AgentVisualController controller;
  final Future<bool> Function(LocalVisualDraft)? onSaveLocalDraft;
  @override
  State<AgentVisualPage> createState() => _AgentVisualPageState();
}

class _AgentVisualPageState extends State<AgentVisualPage> {
  final _question = TextEditingController(), _note = TextEditingController();
  final _localChanges = ValueNotifier<int>(0);
  final _dialogTimers = <Timer>{};
  bool _confirmed = false,
      _saving = false,
      _suppress = false,
      _closed = false,
      _notifierClosed = false;
  int _editVersion = 0, _dialogs = 0;
  String? _visibleVersion, _localError;
  Map<String, dynamic>? _seenReport;
  @override
  void initState() {
    super.initState();
    _attach();
    _question.addListener(_edited);
    _note.addListener(_edited);
  }

  void _attach() {
    widget.controller.retainPage();
    widget.controller.addListener(_observe);
    _suppress = true;
    _question.text = widget.controller.userText;
    _suppress = false;
    _observe();
  }

  void _notifyLocal() {
    if (_notifierClosed) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!_notifierClosed) _localChanges.value++;
      });
    } else {
      _localChanges.value++;
    }
  }

  void _edited() {
    if (_suppress || _closed) return;
    ++_editVersion;
    _localError = null;
    _notifyLocal();
    if (mounted) setState(() {});
  }

  void _observe() {
    if (_closed) return;
    final c = widget.controller;
    _suppress = true;
    if (!c.current) {
      _question.clear();
      _note.clear();
      _seenReport = null;
      _confirmed = false;
    } else if (!identical(_seenReport, c.report)) {
      _seenReport = c.report;
      _note.text = c.report == null ? '' : _reportText(c.report!);
    }
    _suppress = false;
    _notifyLocal();
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && !_closed) setState(() {});
      });
    } else if (mounted) {
      setState(() {});
    }
  }

  String _reportText(Map<String, dynamic> r) => [
    r['summary'] as String,
    for (final item in r['observations'] as List)
      '${item['value']}\n确定程度：${switch (item['confidence']['level']) {
        'HIGH' => '高',
        'MEDIUM' => '中',
        'LOW' => '低',
        _ => '未知',
      }}',
  ].join('\n\n');
  @override
  void didUpdateWidget(AgentVisualPage old) {
    super.didUpdateWidget(old);
    if (old.controller != widget.controller) {
      old.controller.removeListener(_observe);
      old.controller.dispose();
      old.controller.releasePage();
      ++_editVersion;
      _confirmed = false;
      _visibleVersion = null;
      _seenReport = null;
      _suppress = true;
      _note.clear();
      _suppress = false;
      _attach();
    } else if (old.onSaveLocalDraft != widget.onSaveLocalDraft) {
      ++_editVersion;
      _notifyLocal();
    }
  }

  Future<void> _saveLocal({required bool reportNote}) async {
    if (!mounted || _closed || _saving) return;
    final c = widget.controller, save = widget.onSaveLocalDraft;
    if (save == null || !c.current || c.busy) return;
    LocalVisualDraft? draft;
    try {
      draft = reportNote
          ? c.localReportDraft(_note.text)
          : c.localQuestionDraft(_question.text);
    } on FormatException catch (e) {
      setState(() => _localError = e.message);
      return;
    }
    if (draft == null) return;
    final candidate = draft,
        editVersion = _editVersion,
        version = c.sourceVersion,
        report = c.report;
    var open = true;
    bool current() =>
        open &&
        mounted &&
        !_closed &&
        widget.controller == c &&
        widget.onSaveLocalDraft == save &&
        c.current &&
        !c.busy &&
        editVersion == _editVersion &&
        version == c.sourceVersion &&
        c.localSourceSelection?.bindingKey ==
            candidate.sourceSelection.bindingKey &&
        (!reportNote ||
            identical(c.report, report) &&
                identical(c.localReportProvenance, candidate.reportProvenance));
    final timer = Timer.periodic(const Duration(milliseconds: 100), (timer) {
      if (!current()) {
        timer.cancel();
        _notifyLocal();
      }
    });
    _dialogTimers.add(timer);
    _dialogs++;
    final route = DialogRoute<bool>(
      context: context,
      builder: (d) => ListenableBuilder(
        listenable: _localChanges,
        builder: (d, _) {
          final live = current();
          return AlertDialog(
            title: Text(reportNote ? '检查未核验观察摘录' : '检查本机文字草稿'),
            content: SingleChildScrollView(
              child: live
                  ? Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          candidate.text,
                          key: const Key('local-visual-preview'),
                        ),
                        const SizedBox(height: 12),
                        const Text(
                          '仅保存到本人当前本机草稿。不会分析图片、发送、发布，或写入记忆和地点事实；图片像素与批准不会一起保存。',
                        ),
                      ],
                    )
                  : const Text('当前来源或草稿版本已变化，确认中的旧文字已隐藏。请返回当前资料重新检查。'),
            ),
            actions: [
              TextButton(
                onPressed: () {
                  if (!open || !d.mounted) return;
                  open = false;
                  Navigator.pop(d, false);
                },
                child: const Text('取消'),
              ),
              FilledButton(
                key: const Key('local-visual-confirm'),
                onPressed: live
                    ? () {
                        if (!current()) return;
                        open = false;
                        Navigator.pop(d, true);
                      }
                    : null,
                child: const Text('仅保存到本机'),
              ),
            ],
          );
        },
      ),
    );
    route.completed.whenComplete(() {
      timer.cancel();
      _dialogTimers.remove(timer);
      _dialogs--;
      if (_closed && _dialogs == 0 && !_notifierClosed) {
        _notifierClosed = true;
        _localChanges.dispose();
      }
    });
    final yes = await Navigator.of(context, rootNavigator: true).push(route);
    open = false;
    bool stillCurrent() =>
        mounted &&
        !_closed &&
        widget.controller == c &&
        widget.onSaveLocalDraft == save &&
        c.current &&
        !c.busy &&
        editVersion == _editVersion &&
        version == c.sourceVersion &&
        c.localSourceSelection?.bindingKey ==
            candidate.sourceSelection.bindingKey &&
        (!reportNote ||
            identical(c.report, report) &&
                identical(c.localReportProvenance, candidate.reportProvenance));
    if (yes != true || !stillCurrent()) return;
    setState(() => _saving = true);
    try {
      final saved = await save(candidate);
      if (!mounted || _closed) return;
      if (stillCurrent())
        setState(
          () => _localError = saved
              ? '已保存到本机草稿；可回到原编辑器继续编辑。'
              : '本机保存未完成，文字仍保留在这里，请检查后重试。',
        );
    } catch (_) {
      if (stillCurrent())
        setState(() => _localError = '本机保存未完成，文字仍保留在这里，请检查后重试。');
    } finally {
      if (mounted && !_closed) setState(() => _saving = false);
    }
  }

  @override
  void dispose() {
    _closed = true;
    ++_editVersion;
    _notifyLocal();
    for (final t in _dialogTimers) t.cancel();
    widget.controller.removeListener(_observe);
    widget.controller.dispose();
    widget.controller.releasePage();
    _question.removeListener(_edited);
    _note.removeListener(_edited);
    _question.dispose();
    _note.dispose();
    if (_dialogs == 0 && !_notifierClosed) {
      _notifierClosed = true;
      _localChanges.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.controller,
    builder: (context, _) {
      final c = widget.controller,
          current = c.current,
          p = current ? c.preview : null;
      final bindingVersion = c.sourceVersion,
          operation = c.operationID,
          report = current ? c.report : null;
      final historyOnly = report != null && c.localReportProvenance == null;
      final version = p == null ? null : '${p.id}:${p.version}';
      if (_visibleVersion != version) {
        _visibleVersion = version;
        _confirmed = false;
      }
      final pixels = current && !historyOnly
          ? (c.previewPixels ?? c.localPixels)
          : null;
      return Scaffold(
        appBar: birdtieAppBar(context, title: const Text('图片提问')),
        body: SafeArea(
          child: ListView(
            padding: EdgeInsets.fromLTRB(
              20,
              20,
              20,
              20 + MediaQuery.viewInsetsOf(context).bottom,
            ),
            children: [
              if (pixels != null)
                Image.memory(
                  pixels,
                  key: ValueKey(version ?? 'local'),
                  height: 240,
                  fit: BoxFit.contain,
                  frameBuilder: (context, child, frame, synchronous) {
                    if (p != null && (frame != null || synchronous))
                      WidgetsBinding.instance.addPostFrameCallback((_) {
                        if (mounted &&
                            !_closed &&
                            widget.controller == c &&
                            c.current &&
                            identical(c.preview, p) &&
                            !c.pixelVisible)
                          setState(
                            () => c.setPixelVisible(p.id, p.version, true),
                          );
                      });
                    return child;
                  },
                  errorBuilder: (_, __, ___) => const Text('图片预览无法显示，请重新选择。'),
                ),
              if (current) ...[
                const SizedBox(height: 16),
                Text(c.userText.isEmpty ? '本次尚未填写分析问题' : c.userText),
              ],
              if (c.error != null)
                Semantics(liveRegion: true, child: Text(c.error!)),
              if (!current) const Text('当前来源已变化，原编辑器保留仍有效的草稿。'),
              if (_localError != null)
                Text(_localError!, key: const Key('local-visual-save-status')),
              if (c.busy)
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: Center(child: CircularProgressIndicator()),
                ),
              if (current && widget.onSaveLocalDraft != null) ...[
                const SizedBox(height: 16),
                TextField(
                  key: const Key('local-visual-question'),
                  controller: _question,
                  maxLines: 3,
                  maxLength: 2000,
                  enabled: !_saving,
                  decoration: const InputDecoration(
                    labelText: '本机文字草稿',
                    helperText: '只编辑本机草稿，不改变上方本次分析的问题。',
                  ),
                ),
                OutlinedButton(
                  key: const Key('local-visual-save-question'),
                  onPressed:
                      !c.busy && !_saving && _question.text.trim().isNotEmpty
                      ? () {
                          if (widget.controller == c &&
                              c.current &&
                              c.sourceVersion == bindingVersion)
                            _saveLocal(reportNote: false);
                        }
                      : null,
                  child: const Text('保存本人文字到本机'),
                ),
              ],
              if (current && p == null && !c.uncertain && report == null) ...[
                const SizedBox(height: 16),
                const Text('准备分析预览会将当前选中的像素交给 Birdtie 私有服务。请先遮盖不想提供的内容。'),
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: !c.busy
                      ? () {
                          if (widget.controller == c &&
                              c.current &&
                              c.sourceVersion == bindingVersion)
                            c.prepare();
                        }
                      : null,
                  child: const Text('准备分析预览'),
                ),
              ],
              if (current && p != null && report == null && !c.uncertain) ...[
                const SizedBox(height: 16),
                const Text('批准后，这次预览中的像素和文字将发给以下模型，用于图片分析。'),
                Text(
                  '${p.destination['provider']} · ${p.destination['model']}',
                ),
                Text('处理区域：${p.destination['region']}；服务商保留政策未知'),
                Text(
                  '费用上限：${p.budget['currency']} ${(p.budget['costMicrosUpper'] / 1000000).toStringAsFixed(6)}',
                ),
                const Text('观察结果可包含不确定项；拍摄时间和地点须另行核实。'),
                const Text('本次不会自动保存记忆或地点事实。'),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('我已查看这次图片，批准将其用于上述分析'),
                  value: _confirmed,
                  onChanged:
                      c.current &&
                          !c.busy &&
                          c.pixelVisible &&
                          p.validAt(DateTime.now().toUtc())
                      ? (v) {
                          if (c.current &&
                              identical(c.preview, p) &&
                              c.sourceVersion == bindingVersion)
                            setState(() => _confirmed = v ?? false);
                        }
                      : null,
                ),
                FilledButton(
                  onPressed: _confirmed && c.canAnalyze
                      ? () {
                          if (mounted &&
                              !_closed &&
                              widget.controller == c &&
                              identical(c.preview, p) &&
                              _confirmed &&
                              c.canAnalyze &&
                              c.sourceVersion == bindingVersion)
                            c.analyze();
                        }
                      : null,
                  child: const Text('批准并分析'),
                ),
              ],
              if (current && c.uncertain)
                Padding(
                  padding: const EdgeInsets.only(top: 16),
                  child: OutlinedButton(
                    onPressed: !c.busy
                        ? () {
                            if (widget.controller == c &&
                                c.current &&
                                c.sourceVersion == bindingVersion &&
                                c.operationID == operation)
                              c.recover();
                          }
                        : null,
                    child: const Text('核实原操作'),
                  ),
                ),
              if (report != null) ...[
                const SizedBox(height: 20),
                if (historyOnly) ...[
                  const Text('原操作历史结果（未绑定当前选图）'),
                  const Text('已核实原操作状态。这份历史报告未与当前重新选择的图片配对，不能把它当作当前图片的分析结果。'),
                ],
                Text(report['summary']),
                for (final item in report['observations'])
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(
                      '${item['value']}\n确定程度：${switch (item['confidence']['level']) {
                        'HIGH' => '高',
                        'MEDIUM' => '中',
                        'LOW' => '低',
                        _ => '未知',
                      }}',
                    ),
                  ),
                if ((report['unknowns'] as List).isNotEmpty) ...[
                  const SizedBox(height: 16),
                  const Text('尚不能确定'),
                  for (final value in report['unknowns'])
                    Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Text(value),
                    ),
                ],
                if (widget.onSaveLocalDraft != null) ...[
                  if (c.localReportProvenance != null) ...[
                    const SizedBox(height: 16),
                    TextField(
                      key: const Key('local-visual-report-note'),
                      controller: _note,
                      maxLines: 5,
                      maxLength: 2000,
                      enabled: !_saving,
                      decoration: const InputDecoration(
                        labelText: '未核验观察摘录（可编辑）',
                        helperText: '保存时保留来源说明和报告原有的不确定项。',
                      ),
                    ),
                    OutlinedButton(
                      key: const Key('local-visual-save-report'),
                      onPressed:
                          !c.busy && !_saving && _note.text.trim().isNotEmpty
                          ? () {
                              if (widget.controller == c &&
                                  c.current &&
                                  c.sourceVersion == bindingVersion &&
                                  identical(c.report, report))
                                _saveLocal(reportNote: true);
                            }
                          : null,
                      child: const Text('另存为未核验本机备注'),
                    ),
                  ] else
                    const Text('原报告尚未与当前选图预览配对，不能另存为当前图片摘录。核实原操作不会重新分析。'),
                ],
              ],
              const SizedBox(height: 20),
            ],
          ),
        ),
      );
    },
  );
}
