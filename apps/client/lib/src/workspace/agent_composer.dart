import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/services.dart';

import '../app/birdtie_surfaces.dart';
import 'agent_workspace_controller.dart';

const _composerFocusTraceEnabled = bool.fromEnvironment(
  'BIRDTIE_COMPOSER_FOCUS_TRACE',
  defaultValue: false,
);
const _composerFocusEventsEnabled = bool.fromEnvironment(
  'BIRDTIE_COMPOSER_FOCUS_EVENTS',
  defaultValue: false,
);
const _composerFocusObservationEnabled =
    _composerFocusTraceEnabled || _composerFocusEventsEnabled;

class AgentComposer extends StatefulWidget {
  const AgentComposer({
    super.key,
    required this.workspace,
    required this.onSubmit,
    required this.onSearchArea,
    required this.hasSearchArea,
    this.suggestions = const [],
    this.onlineMode = false,
    this.onHeightChanged,
    this.materialContext,
    this.materialContextChanges,
    this.onPrivateMomentImage,
    this.privateMomentImageContext,
    this.reserveKeyboardRestoreSpace = false,
  });
  final AgentWorkspaceController workspace;
  final ValueChanged<String> onSubmit;
  final VoidCallback onSearchArea;
  final bool hasSearchArea;
  final List<String> suggestions;
  final bool onlineMode;
  final ValueChanged<double>? onHeightChanged;
  final Object? Function()? materialContext;
  final Listenable? materialContextChanges;
  final VoidCallback? onPrivateMomentImage;
  final Object? Function()? privateMomentImageContext;
  final bool reserveKeyboardRestoreSpace;

  @override
  State<AgentComposer> createState() => AgentComposerState();
}

class AgentComposerState extends State<AgentComposer> {
  final _text = TextEditingController();
  final _focus = FocusNode();
  // Opt-in diagnostics observe the existing editor only. The normal build
  // creates no key/clock/subscription and never reads geometry for tracing.
  final GlobalKey? _focusTraceSurface = _composerFocusTraceEnabled
      ? GlobalKey()
      : null;
  final Stopwatch? _focusTraceClock = _composerFocusObservationEnabled
      ? (Stopwatch()..start())
      : null;
  int _focusTraceSequence = 0;
  double? _focusTraceDPR;
  String? _selectedQuickAction;
  // The current native query APIs accept at most 240 UTF-8 bytes. Pasting
  // material must not silently truncate it or create an unsendable query.
  static const _maxDraftBytes = 240;
  static const _minimumInputHeight = 56.0;
  int _pasteSerial = 0;
  int _boundarySerial = 0;
  int _draftRevision = 0;
  String _observedDraft = '';
  Object? _observedContext;
  int _observedTaskEpoch = 0;
  bool _readingClipboard = false;
  bool _hasMaterial = false;
  String? _feedbackMessage;

  Object? get _materialContext => widget.materialContext?.call();

  void _traceFocusEvents(
    String event, {
    String? keyboardPath,
    bool? editableFound,
    bool? editableMounted,
    bool? editableSameNode,
  }) {
    if (!_composerFocusEventsEnabled || !mounted) return;
    final primary = FocusManager.instance.primaryFocus;
    final ancestors = _focus.ancestors.toList(growable: false);
    final focusContext = _focus.context;
    final focusContextType = focusContext?.mounted == true
        ? focusContext!.widget.runtimeType.toString()
        : null;
    debugPrint(
      'BIRDTIE_COMPOSER_FOCUS_EVENTS ${jsonEncode({'event': event, 'sequence': ++_focusTraceSequence, 'elapsedMicros': _focusTraceClock!.elapsedMicroseconds, 'mounted': mounted, 'hasFocus': _focus.hasFocus, 'hasPrimaryFocus': _focus.hasPrimaryFocus, 'canRequestFocus': _focus.canRequestFocus, 'ownNodeHash': identityHashCode(_focus), 'primaryType': primary?.runtimeType.toString(), 'primaryHash': primary == null ? null : identityHashCode(primary), 'parentAttached': _focus.parent != null, 'ancestorCount': ancestors.length, 'blockedAncestorCount': ancestors.where((node) => !node.descendantsAreFocusable).length, 'focusContextType': focusContextType, 'keyboardPath': ?keyboardPath, 'editableFound': ?editableFound, 'editableMounted': ?editableMounted, 'editableSameNode': ?editableSameNode})}',
    );
  }

  void _traceFocus(String event, {Offset? logicalTap}) {
    _traceFocusEvents(event);
    if (!_composerFocusTraceEnabled || !mounted) return;
    final primary = FocusManager.instance.primaryFocus;
    final surface = _focusTraceSurface?.currentContext?.findRenderObject();
    Rect? rect;
    final attached = surface is RenderBox && surface.attached;
    if (attached && surface.hasSize) {
      try {
        final offset = surface.localToGlobal(Offset.zero);
        final candidate = offset & surface.size;
        if ([
          candidate.left,
          candidate.top,
          candidate.right,
          candidate.bottom,
        ].every((value) => value.isFinite)) {
          rect = candidate;
        }
      } catch (_) {
        // Detached/incomplete transforms are unknown, never inferred.
      }
    }
    final dpr = _focusTraceDPR;
    List<double>? coordinates(Rect? value, double scale) => value == null
        ? null
        : [
            value.left * scale,
            value.top * scale,
            value.right * scale,
            value.bottom * scale,
          ];
    debugPrint(
      'BIRDTIE_COMPOSER_FOCUS_TRACE ${jsonEncode({
        'event': event,
        'sequence': ++_focusTraceSequence,
        'elapsedMicros': _focusTraceClock!.elapsedMicroseconds,
        'mounted': mounted,
        'hasFocus': _focus.hasFocus,
        'canRequestFocus': _focus.canRequestFocus,
        'ownNodeHash': identityHashCode(_focus),
        'primaryType': primary?.runtimeType.toString(),
        'primaryHash': primary == null ? null : identityHashCode(primary),
        'surfaceAttached': attached,
        'logicalRect': coordinates(rect, 1),
        'physicalRect': dpr == null ? null : coordinates(rect, dpr),
        'dpr': dpr,
        if (logicalTap != null) 'logicalTap': [logicalTap.dx, logicalTap.dy],
        if (logicalTap != null && dpr != null) 'physicalTap': [logicalTap.dx * dpr, logicalTap.dy * dpr],
      })}',
    );
  }

  void _onManagerFocusTrace() => _traceFocus('manager_focus_change');

  void _refresh() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _onDraftEdit() {
    if (_text.text == _observedDraft) return;
    _observedDraft = _text.text;
    ++_draftRevision;
  }

  void _retireMaterial() {
    ++_boundarySerial;
    ++_pasteSerial;
    _readingClipboard = false;
    _feedbackMessage = null;
    if (_hasMaterial) {
      _hasMaterial = false;
      _selectedQuickAction = null;
      _text.clear();
      pauseEditing();
    }
    _refresh();
  }

  void _onMaterialBoundaryChange() {
    final context = _materialContext;
    final epoch = widget.workspace.taskEpoch;
    if (context == _observedContext && epoch == _observedTaskEpoch) return;
    _observedContext = context;
    _observedTaskEpoch = epoch;
    _retireMaterial();
  }

  void fillDraft(String value, {bool requestFocus = true}) {
    _text.text = value;
    _text.selection = TextSelection.collapsed(offset: value.length);
    setState(() {});
    if (requestFocus) resumeEditing();
  }

  void pauseEditing() {
    _traceFocus('pause_before');
    _focus.unfocus();
    _focus.canRequestFocus = false;
    _traceFocus('pause_after');
  }

  void resumeEditing() {
    if (!mounted) return;
    _traceFocus('resume_before');
    _focus.canRequestFocus = true;
    final editable = _focus.context
        ?.findAncestorStateOfType<EditableTextState>();
    if (_composerFocusEventsEnabled) {
      _traceFocusEvents(
        'resume_keyboard_path',
        keyboardPath:
            editable != null &&
                editable.mounted &&
                identical(editable.widget.focusNode, _focus)
            ? 'requestKeyboard'
            : 'requestFocusFallback',
        editableFound: editable != null,
        editableMounted: editable?.mounted,
        editableSameNode: editable == null
            ? null
            : identical(editable.widget.focusNode, _focus),
      );
    }
    if (editable != null &&
        editable.mounted &&
        identical(editable.widget.focusNode, _focus)) {
      // Native Back can hide the IME while this field retains focus. Request
      // this editor's keyboard, preserving its current composing/selection;
      // requesting the same FocusNode alone does not reopen an attached IME.
      editable.requestKeyboard();
    } else {
      _focus.requestFocus();
    }
    _traceFocus('resume_after');
  }

  void clearDraft() {
    ++_boundarySerial;
    ++_pasteSerial;
    _readingClipboard = false;
    _hasMaterial = false;
    _feedbackMessage = null;
    _text.clear();
    _selectedQuickAction = null;
    pauseEditing();
    _refresh();
  }

  void _chooseQuickAction(
    BuildContext sheetContext,
    String draft,
    String kind,
  ) {
    Navigator.pop(sheetContext);
    final previous = _text.text;
    final next = previous.isEmpty || previous == draft
        ? draft
        : '$previous\n$draft';
    if (utf8.encode(next).length > _maxDraftBytes) {
      _feedback('草稿较长，未添加快捷任务。请先编辑已有内容。');
      return;
    }
    _selectedQuickAction = kind;
    fillDraft(next);
  }

  @override
  void initState() {
    super.initState();
    if (_composerFocusObservationEnabled) {
      FocusManager.instance.addListener(_onManagerFocusTrace);
      _traceFocus('init');
    }
    _focus.addListener(_onFocus);
    _text.addListener(_onDraftEdit);
    _observedContext = _materialContext;
    _observedTaskEpoch = widget.workspace.taskEpoch;
    widget.workspace.addListener(_onMaterialBoundaryChange);
    widget.materialContextChanges?.addListener(_onMaterialBoundaryChange);
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_composerFocusTraceEnabled) {
      _focusTraceDPR = MediaQuery.maybeOf(context)?.devicePixelRatio;
      _traceFocus('dependencies');
    }
  }

  @override
  void didUpdateWidget(AgentComposer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.workspace != widget.workspace) {
      oldWidget.workspace.removeListener(_onMaterialBoundaryChange);
      widget.workspace.addListener(_onMaterialBoundaryChange);
      _retireMaterial();
    }
    if (oldWidget.materialContextChanges != widget.materialContextChanges) {
      oldWidget.materialContextChanges?.removeListener(
        _onMaterialBoundaryChange,
      );
      widget.materialContextChanges?.addListener(_onMaterialBoundaryChange);
    }
    _onMaterialBoundaryChange();
  }

  void _onFocus() {
    _traceFocus('own_focus_change');
    if (_focus.hasFocus) {
      widget.workspace.beginTyping();
    } else {
      widget.workspace.stopTyping();
    }
  }

  void _submit() {
    if (_text.value.composing.isValid && !_text.value.composing.isCollapsed) {
      return;
    }
    final query = _text.text.trim();
    if (query.isEmpty) return;
    _text.clear();
    _hasMaterial = false;
    _feedbackMessage = null;
    ++_pasteSerial;
    _selectedQuickAction = null;
    _focus.unfocus();
    widget.onSubmit(query);
  }

  Future<void> showMaterialTools(BuildContext context) async {
    final contextKey = _materialContext;
    final privateImageSource = widget.privateMomentImageContext?.call();
    final taskEpoch = widget.workspace.taskEpoch;
    final boundary = _boundarySerial;
    bool current() =>
        mounted &&
        boundary == _boundarySerial &&
        contextKey == _materialContext &&
        taskEpoch == widget.workspace.taskEpoch;
    pauseEditing();
    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (sheetContext) => SafeArea(
        child: ConstrainedBox(
          constraints: BoxConstraints(
            maxHeight: MediaQuery.sizeOf(sheetContext).height * .85,
          ),
          child: SingleChildScrollView(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 20),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (_feedbackMessage != null)
                    Padding(
                      padding: const EdgeInsets.all(8),
                      child: Semantics(
                        liveRegion: true,
                        child: Text(_feedbackMessage!),
                      ),
                    ),
                  const Padding(
                    padding: EdgeInsets.fromLTRB(8, 4, 8, 8),
                    child: Text(
                      '添加素材',
                      style: TextStyle(
                        fontSize: 20,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  _QuickActionTile(
                    icon: Icons.content_paste_outlined,
                    title: '粘贴文字/链接',
                    subtitle: '仅在点击后读取，加入草稿供你检查；不会自动发送',
                    onTap: _readingClipboard
                        ? null
                        : () {
                            Navigator.pop(sheetContext);
                            if (current()) _pasteText();
                          },
                  ),
                  if (widget.onPrivateMomentImage != null)
                    _QuickActionTile(
                      icon: Icons.add_photo_alternate_outlined,
                      title: '为私人记录添加图片',
                      subtitle: '先选择本人已保存的私人草稿；不加入当前对话，也不发送给 AI',
                      onTap: () {
                        Navigator.pop(sheetContext);
                        if (!current()) return;
                        if (privateImageSource !=
                            widget.privateMomentImageContext?.call()) {
                          _feedback('私人记录来源已变化，请重新打开添加素材。');
                          return;
                        }
                        widget.onPrivateMomentImage?.call();
                      },
                    ),
                  Padding(
                    padding: EdgeInsets.fromLTRB(8, 0, 8, 12),
                    child: Text(
                      widget.onPrivateMomentImage == null
                          ? '仅支持短文字或链接，内容未经核验。图片、语音、文件及长文暂未接入。'
                          : '文字或链接可加入草稿，内容未经核验。图片仅添加到所选私人记录；语音、文件及长文暂未接入。',
                    ),
                  ),
                  const Padding(
                    padding: EdgeInsets.fromLTRB(8, 4, 8, 12),
                    child: Text(
                      '快捷任务',
                      style: TextStyle(
                        fontSize: 20,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  if (widget.onlineMode)
                    _QuickActionTile(
                      icon: Icons.public_outlined,
                      title: '查找公开线上意图',
                      subtitle: '添加到现有草稿，检查后发送',
                      onTap: () {
                        if (!current()) {
                          Navigator.pop(sheetContext);
                          return;
                        }
                        _chooseQuickAction(
                          sheetContext,
                          '查找公开线上意图',
                          'online_intents',
                        );
                      },
                    ),
                  _QuickActionTile(
                    icon: Icons.event_outlined,
                    title: '找活动',
                    subtitle: '描述你想参加的活动',
                    onTap: () {
                      if (!current()) {
                        Navigator.pop(sheetContext);
                        return;
                      }
                      _chooseScopedQuickAction(
                        current,
                        sheetContext,
                        _text.text,
                        'find_activities',
                      );
                    },
                  ),
                  _QuickActionTile(
                    icon: Icons.groups_outlined,
                    title: '找组织',
                    subtitle: '搜索当前城市公开组织',
                    onTap: () => _chooseScopedQuickAction(
                      current,
                      sheetContext,
                      '找组织',
                      'find_organizations',
                    ),
                  ),
                  _QuickActionTile(
                    icon: Icons.place_outlined,
                    title: '找地点',
                    subtitle: '搜索当前城市已发布地点',
                    onTap: () => _chooseScopedQuickAction(
                      current,
                      sheetContext,
                      '找地点',
                      'find_places',
                    ),
                  ),
                  const _QuickActionTile(
                    icon: Icons.question_answer_outlined,
                    title: '询问附近信息',
                    subtitle: '暂不支持区域问答',
                  ),
                  _QuickActionTile(
                    icon: Icons.add_location_alt_outlined,
                    title: '发布活动',
                    subtitle: '从组织工作台创建草稿',
                    onTap: () => _chooseScopedQuickAction(
                      current,
                      sheetContext,
                      '创建活动',
                      'create_activity',
                    ),
                  ),
                  _QuickActionTile(
                    icon: Icons.my_location_outlined,
                    title: '搜索当前地图区域',
                    subtitle: !widget.hasSearchArea ? '先拖动地图选择区域' : '搜索当前可见范围',
                    onTap: !widget.hasSearchArea || widget.onlineMode
                        ? null
                        : () {
                            Navigator.pop(sheetContext);
                            if (current()) widget.onSearchArea();
                          },
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  void _chooseScopedQuickAction(
    bool Function() current,
    BuildContext context,
    String draft,
    String kind,
  ) {
    if (!current()) {
      Navigator.pop(context);
      return;
    }
    if (widget.onlineMode) {
      Navigator.pop(context);
      _feedback('当前为线上范围。请切换公开城市范围后使用这项快捷任务。');
      return;
    }
    _chooseQuickAction(context, draft, kind);
  }

  void _feedback(String message) {
    if (!mounted) return;
    _feedbackMessage = message;
    _refresh();
  }

  Future<void> _pasteText() async {
    if (_readingClipboard) return;
    final serial = ++_pasteSerial;
    final contextKey = _materialContext;
    final epoch = widget.workspace.taskEpoch;
    final revision = _draftRevision;
    bool current() =>
        mounted &&
        serial == _pasteSerial &&
        contextKey == _materialContext &&
        epoch == widget.workspace.taskEpoch;
    _readingClipboard = true;
    _feedbackMessage = null;
    _refresh();
    try {
      final text = (await Clipboard.getData(
        Clipboard.kTextPlain,
      ))?.text?.trim();
      if (!current()) return;
      if (revision != _draftRevision) {
        _feedback('输入已变化，未加入迟到的剪贴板内容。请重新点击粘贴。');
        return;
      }
      if (text == null || text.isEmpty) {
        _feedback('剪贴板没有可粘贴的文字或链接。');
        return;
      }
      final previous = _text.text;
      final next = previous.isEmpty ? text : '$previous\n$text';
      if (utf8.encode(next).length > _maxDraftBytes) {
        _feedback('文字较长，未粘贴且保留原草稿。请缩短后再试（当前输入最多240字节）。');
        return;
      }
      _hasMaterial = true;
      fillDraft(next);
      _feedback('已加入可编辑草稿。内容未经核验，检查后再发送。');
    } catch (_) {
      if (current()) _feedback('暂时无法读取剪贴板。请允许系统读取或手动输入。');
    } finally {
      if (current()) {
        _readingClipboard = false;
        _refresh();
      }
    }
  }

  @override
  void dispose() {
    if (_composerFocusObservationEnabled) {
      FocusManager.instance.removeListener(_onManagerFocusTrace);
      _traceFocus('dispose');
      _focusTraceClock?.stop();
    }
    ++_pasteSerial;
    widget.workspace.removeListener(_onMaterialBoundaryChange);
    widget.materialContextChanges?.removeListener(_onMaterialBoundaryChange);
    _text.removeListener(_onDraftEdit);
    _focus.removeListener(_onFocus);
    _focus.dispose();
    _text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => _ComposerSizeReporter(
    onSize: (size) => widget.onHeightChanged?.call(size.height),
    child: LayoutBuilder(
      builder: (context, constraints) => AnimatedBuilder(
        animation: widget.workspace,
        builder: (context, _) {
          final searching = widget.workspace.state == AgentViewState.searching;
          final showSuggestions =
              !widget.workspace.inputFocused &&
              widget.workspace.task == null &&
              widget.suggestions.isNotEmpty &&
              (!constraints.hasBoundedHeight ||
                  constraints.maxHeight >= _minimumInputHeight + 42);
          final feedbackHeight = constraints.hasBoundedHeight
              ? (constraints.maxHeight -
                        _minimumInputHeight -
                        8 -
                        (showSuggestions ? 42 : 0))
                    .clamp(0.0, 120.0)
                    .toDouble()
              : 120.0;
          final showFeedback = _feedbackMessage != null && feedbackHeight >= 48;
          final inputMaxHeight = constraints.hasBoundedHeight
              ? (constraints.maxHeight -
                        (showFeedback ? feedbackHeight + 8 : 0) -
                        (showSuggestions ? 42 : 0))
                    .clamp(0.0, constraints.maxHeight)
                    .toDouble()
              : double.infinity;
          final inputMinHeight = inputMaxHeight
              .clamp(0.0, _minimumInputHeight)
              .toDouble();
          return Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (showFeedback) ...[
                ConstrainedBox(
                  constraints: BoxConstraints(maxHeight: feedbackHeight),
                  child: BirdtieSurface(
                    kind: BirdtieSurfaceKind.critical,
                    child: Row(
                      children: [
                        Expanded(
                          child: SingleChildScrollView(
                            child: Padding(
                              padding: const EdgeInsets.all(8),
                              child: Semantics(
                                liveRegion: true,
                                child: Text(_feedbackMessage!),
                              ),
                            ),
                          ),
                        ),
                        IconButton(
                          tooltip: '关闭素材提示',
                          onPressed: () {
                            _feedbackMessage = null;
                            _refresh();
                          },
                          icon: const Icon(Icons.close),
                        ),
                      ],
                    ),
                  ),
                ),
                const SizedBox(height: 8),
              ],
              if (showSuggestions)
                SizedBox(
                  height: 42,
                  child: ListView.separated(
                    scrollDirection: Axis.horizontal,
                    itemCount: widget.suggestions.length,
                    separatorBuilder: (_, _) => const SizedBox(width: 8),
                    itemBuilder: (context, index) => ActionChip(
                      label: Text(widget.suggestions[index]),
                      onPressed: () {
                        _text.text = widget.suggestions[index];
                        _text.selection = TextSelection.collapsed(
                          offset: _text.text.length,
                        );
                        setState(() {});
                        _focus.requestFocus();
                      },
                    ),
                  ),
                ),
              BirdtieSurface(
                kind: BirdtieSurfaceKind.floating,
                child: Container(
                  constraints: BoxConstraints(
                    minHeight: inputMinHeight,
                    maxHeight: inputMaxHeight,
                  ),
                  padding: const EdgeInsets.symmetric(horizontal: 5),
                  child: Row(
                    children: [
                      Expanded(
                        // The visible central surface must be editable even in
                        // its vertical whitespace. Keep the adjacent actions
                        // independent and let multiline input grow naturally.
                        child: GestureDetector(
                          key: _focusTraceSurface,
                          behavior: HitTestBehavior.opaque,
                          onTapDown: _composerFocusObservationEnabled
                              ? (details) => _traceFocus(
                                  'central_tap_down',
                                  logicalTap: _composerFocusTraceEnabled
                                      ? details.globalPosition
                                      : null,
                                )
                              : null,
                          onTapCancel: _composerFocusObservationEnabled
                              ? () => _traceFocus('central_tap_cancel')
                              : null,
                          onTap: _composerFocusObservationEnabled
                              ? () {
                                  _traceFocus('central_tap');
                                  resumeEditing();
                                }
                              : resumeEditing,
                          child: ConstrainedBox(
                            constraints: BoxConstraints(
                              minHeight: inputMinHeight,
                              maxHeight: inputMaxHeight,
                            ),
                            child: Align(
                              alignment: Alignment.center,
                              heightFactor: 1,
                              child: TextField(
                                controller: _text,
                                focusNode: _focus,
                                minLines: 1,
                                maxLines: 4,
                                onTap: resumeEditing,
                                textInputAction: TextInputAction.send,
                                onSubmitted: (_) => _submit(),
                                onChanged: (_) => setState(() {}),
                                decoration: InputDecoration(
                                  hintText: widget.onlineMode
                                      ? '想查找什么公开线上意图？'
                                      : switch (_selectedQuickAction) {
                                          'find_activities' => '想参加什么活动？什么时候？',
                                          'find_organizations' => '输入组织名称或直接发送',
                                          'find_places' => '输入地点名称或类别',
                                          'create_activity' => '发送后检查组织发布权限',
                                          _ =>
                                            _focus.hasFocus
                                                ? '描述你想做什么'
                                                : '你想做什么？',
                                        },
                                  hintStyle: TextStyle(
                                    color: Theme.of(
                                      context,
                                    ).colorScheme.onSurfaceVariant,
                                    fontSize: 14,
                                  ),
                                  border: InputBorder.none,
                                  isDense: true,
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                      if (searching)
                        const Padding(
                          padding: EdgeInsets.symmetric(horizontal: 4),
                          child: SizedBox(
                            width: 17,
                            height: 17,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          ),
                        ),
                      IconButton(
                        style: birdtieFloatingButtonStyle,
                        tooltip: _text.text.trim().isEmpty ? '请输入需求' : '发送需求',
                        onPressed: _text.text.trim().isEmpty ? null : _submit,
                        icon: const Icon(Icons.arrow_upward_rounded),
                      ),
                      if (widget.reserveKeyboardRestoreSpace)
                        const SizedBox(width: 48),
                    ],
                  ),
                ),
              ),
            ],
          );
        },
      ),
    ),
  );
}

/// Reports the actual laid-out input including suggestions and large text.
class _ComposerSizeReporter extends SingleChildRenderObjectWidget {
  const _ComposerSizeReporter({required this.onSize, required super.child});
  final ValueChanged<Size> onSize;
  @override
  RenderObject createRenderObject(BuildContext context) =>
      _ComposerSizeRender(onSize);
  @override
  void updateRenderObject(
    BuildContext context,
    covariant _ComposerSizeRender renderObject,
  ) => renderObject.onSize = onSize;
}

class _ComposerSizeRender extends RenderProxyBox {
  _ComposerSizeRender(this.onSize);
  ValueChanged<Size> onSize;
  Size? _reported;
  @override
  void performLayout() {
    super.performLayout();
    if (_reported == size) return;
    _reported = size;
    final measured = size;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (attached) onSize(measured);
    });
  }
}

class _QuickActionTile extends StatelessWidget {
  const _QuickActionTile({
    required this.icon,
    required this.title,
    required this.subtitle,
    this.onTap,
  });
  final IconData icon;
  final String title;
  final String subtitle;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) => ListTile(
    enabled: onTap != null,
    leading: Icon(icon),
    title: Text(title),
    subtitle: Text(subtitle),
    onTap: onTap,
  );
}
