import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/services.dart';

import '../app/birdtie_surfaces.dart';
import 'agent_workspace_controller.dart';
import 'now_ui_tokens.dart';
import 'now_glass_surface.dart';
import 'task_image_controller.dart';
import 'voice_input.dart';
import 'voice_input_consent.dart';
import 'friend_conversation_controller.dart';
import 'local_image_draft.dart';
import '../content/moment_image_picker.dart';

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
    this.onPreferredHeightChanged,
    this.materialContext,
    this.materialContextChanges,
    this.onPrivateMomentImage,
    this.privateMomentImageContext,
    this.reserveKeyboardRestoreSpace = false,
    this.onTaskImage,
    this.onVideoMaterial,
    this.onAnalyzeImage,
    this.imageDraft,
    this.onDraftImage,
    this.onCaptureImage,
    this.onImageFile,
    this.imageMaterialStatus,
    this.imageMaterialRecovering = false,
    this.onRefreshImageMaterial,
    this.onPasteText,
    this.embedded = false,
    this.voiceInputPort,
    this.voiceConsentStore,
    this.recipientName,
    this.draftIdentity,
    this.draftValue,
    this.onDraftChanged,
    this.submitBusy = false,
    this.humanMode = false,
    this.friendConversation,
    this.imageUpload,
    this.needsLogin = false,
    this.onSignIn,
    this.onCreateMoment,
    this.onCreateActivity,
    this.onCreateJourney,
    this.onCreateThemeMap,
    this.onSavePlace,
    this.onCreateRoute,
    this.onImportPlan,
    this.onDraftContext,
    this.creationLabel,
    this.creationInputHint,
    this.onExitCreation,
    this.maxDraftBytes = 240,
    this.videoPreview,
    this.videoStatus,
  }) : assert(maxDraftBytes > 0);
  final AgentWorkspaceController workspace;
  final ValueChanged<String> onSubmit;
  final VoidCallback onSearchArea;
  final bool hasSearchArea;
  final List<String> suggestions;
  final bool onlineMode;
  final ValueChanged<double>? onHeightChanged;

  /// Natural 1–4-line editor plus material rows, measured with the allocated
  /// width before the current panel height clamps the editor. The single root
  /// layout owner clamps this preference to its available IME budget.
  final ValueChanged<double>? onPreferredHeightChanged;
  final Object? Function()? materialContext;
  final Listenable? materialContextChanges;
  final VoidCallback? onPrivateMomentImage;
  final Object? Function()? privateMomentImageContext;
  final bool reserveKeyboardRestoreSpace;
  final VoidCallback? onTaskImage;
  final VoidCallback? onVideoMaterial;
  final ValueChanged<String>? onAnalyzeImage;
  final TaskImageDraftController? imageDraft;
  final VoidCallback? onDraftImage, onCaptureImage, onImageFile, onPasteText;

  /// Read-only capability presentation. A missing picker callback never grants
  /// access; the host supplies its real current recovery action separately.
  final String? imageMaterialStatus;
  final bool imageMaterialRecovering;
  final VoidCallback? onRefreshImageMaterial;
  final bool embedded;
  final VoiceInputPort? voiceInputPort;
  final VoiceInputConsentStore? voiceConsentStore;
  final String? recipientName;
  final Object? draftIdentity;
  final TextEditingValue? draftValue;
  final ValueChanged<TextEditingValue>? onDraftChanged;
  final bool submitBusy, humanMode;
  final FriendConversationController? friendConversation;
  final TaskImageController? imageUpload;
  final bool needsLogin;
  final VoidCallback? onSignIn;
  final String? creationLabel, creationInputHint;
  final VoidCallback? onExitCreation;
  final int maxDraftBytes;
  final Widget? videoPreview;
  final String? videoStatus;
  final VoidCallback? onCreateMoment,
      onCreateActivity,
      onCreateJourney,
      onCreateThemeMap,
      onSavePlace,
      onCreateRoute,
      onImportPlan,
      onDraftContext;

  static const inputContentPadding = EdgeInsets.symmetric(
    vertical: NowUiTokens.space8,
  );

  static TextStyle _inputStyleOf(BuildContext context) {
    return NowUiTokens.reviewedOf(context).composer;
  }

  static double _scaledLineHeightOf(BuildContext context, TextStyle style) {
    final line = TextPainter(
      text: TextSpan(text: '国Hg', style: style),
      textScaler: MediaQuery.textScalerOf(context),
      textDirection: Directionality.of(context),
      maxLines: 1,
    )..layout();
    final height = line.height;
    line.dispose();
    return height;
  }

  /// The parent reserves the same scaled line and padding that the editor uses.
  /// A 56dp visual target alone is insufficient for a large-text landscape IME.
  static double minimumInputHeightOf(BuildContext context) {
    final height = math.max(
      NowUiTokens.composerMinHeight,
      _scaledLineHeightOf(context, _inputStyleOf(context)) +
          inputContentPadding.vertical,
    );
    return height.ceilToDouble();
  }

  static double _minimumSuggestionHeightOf(BuildContext context) => math
      .max(
        NowUiTokens.actionHitSize,
        _scaledLineHeightOf(context, NowUiTokens.reviewedOf(context).action) +
            NowUiTokens.space16,
      )
      .ceilToDouble();

  @override
  State<AgentComposer> createState() => AgentComposerState();
}

class AgentComposerState extends State<AgentComposer> {
  final _text = TextEditingController();
  final _focus = FocusNode();
  final _feedbackScroll = ScrollController(keepScrollOffset: false);
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
  // Native queries default to 240 UTF-8 bytes. Creation hosts supply their
  // domain limit so the shared editor can retain a complete creation draft.
  static const _minimumInputHeight = NowUiTokens.composerMinHeight;
  int _pasteSerial = 0;
  int _boundarySerial = 0;
  late VoiceInputController _voice;
  late VoiceInputConsentStore _voiceConsent;
  bool _applyingExternalDraft = false;
  int _voiceSerial = 0;
  DialogRoute<VoiceInputChoice>? _voiceConsentRoute;
  TextEditingValue? _voiceInitialValue;
  int _draftRevision = 0;
  String _observedDraft = '';
  Object? _observedContext;
  int _observedTaskIdentityEpoch = 0;
  bool _readingClipboard = false;
  bool _materialMenuOpen = false;
  bool _friendImageVisible = false;
  int _humanImageViewSerial = 0;
  ComposerImageDraftView? get _visibleImage => widget.humanMode
      ? widget.friendConversation?.imageDraft
      : widget.imageDraft;
  final _materialMenuUpdates = ValueNotifier<int>(0);
  bool _hasMaterial = false;
  String? _feedbackMessage;
  double? _reportedPreferredHeight;
  int _preferredHeightSerial = 0;
  static const _compositionFeedback = '请先确认输入法候选字，再点击发送。';
  TextEditingValue? _blockedComposition;
  bool _feedbackFits = true;
  ScaffoldFeatureController<SnackBar, SnackBarClosedReason>?
  _compositionSnackBar;

  Object? get _materialContext => widget.materialContext?.call();
  String get draftText => _text.text;
  int get draftRevision => _draftRevision;

  double _naturalTextHeight(
    String text,
    TextStyle style,
    double width, {
    int? maxLines,
  }) {
    final painter = TextPainter(
      text: TextSpan(text: text, style: style),
      textScaler: MediaQuery.textScalerOf(context),
      textDirection: Directionality.of(context),
      maxLines: maxLines,
    )..layout(maxWidth: math.max(0.0, width));
    final height = painter.height;
    painter.dispose();
    return height;
  }

  void _reportPreferredHeight(double height) {
    if (widget.onPreferredHeightChanged == null ||
        _reportedPreferredHeight == height) {
      return;
    }
    _reportedPreferredHeight = height;
    final serial = ++_preferredHeightSerial;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && serial == _preferredHeightSerial) {
        widget.onPreferredHeightChanged?.call(height);
      }
    });
  }

  /// Reuses the existing explicit clipboard read and revision/source fences.
  Future<void> pasteTextFromClipboard() => _pasteText();

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
    void rebuild() {
      if (!mounted) return;
      setState(() {});
      _materialMenuUpdates.value++;
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => rebuild());
    } else {
      rebuild();
    }
  }

  void _onDraftEdit() {
    if (!_applyingExternalDraft) widget.onDraftChanged?.call(_text.value);
    final voiceValue = _voiceInitialValue;
    if (_voice.active &&
        voiceValue != null &&
        (_text.text != voiceValue.text ||
            _text.value.composing != voiceValue.composing)) {
      _cancelVoice();
    }
    final blocked = _blockedComposition;
    if (blocked != null &&
        (_text.text != blocked.text ||
            _text.value.composing != blocked.composing)) {
      _clearCompositionFeedback();
      _refresh();
    }
    if (_text.text == _observedDraft) return;
    _observedDraft = _text.text;
    ++_draftRevision;
  }

  void _retireMaterial() {
    _cancelVoice();
    _clearCompositionFeedback();
    ++_boundarySerial;
    ++_pasteSerial;
    _readingClipboard = false;
    _feedbackMessage = null;
    if (_hasMaterial && widget.draftIdentity == null) {
      _hasMaterial = false;
      _text.clear();
      pauseEditing();
    }
    _refresh();
  }

  void _onMaterialBoundaryChange() {
    final context = _materialContext;
    final epoch = widget.workspace.taskIdentityEpoch;
    if (context == _observedContext && epoch == _observedTaskIdentityEpoch) {
      return;
    }
    _observedContext = context;
    _observedTaskIdentityEpoch = epoch;
    _retireMaterial();
  }

  void fillDraft(String value, {bool requestFocus = true}) {
    _text.text = value;
    _text.selection = TextSelection.collapsed(offset: value.length);
    setState(() {});
    if (requestFocus) resumeEditing();
  }

  void _attachVoice() {
    _voice = VoiceInputController(port: widget.voiceInputPort);
    _voice.addListener(_refresh);
    unawaited(_voice.probe());
  }

  void _cancelVoice() {
    ++_voiceSerial;
    _voiceInitialValue = null;
    _voice.cancel();
  }

  Future<VoiceInputChoice?> _chooseVoice(
    VoiceAvailability availability,
    bool Function() current, {
    bool systemOnly = false,
  }) async {
    if (!mounted || !current()) return null;
    final local = availability.onDevice && !systemOnly;
    final service = local
        ? availability.bundledEngine
        : availability.preferredSystem;
    if (!local && service == null) return null;
    if (await _voiceConsent.acknowledged(service)) {
      return mounted && current()
          ? VoiceInputChoice(systemApproved: !local)
          : null;
    }
    if (!mounted || !current()) return null;
    final serial = _voiceSerial;
    var completed = false;
    late final DialogRoute<VoiceInputChoice> route;
    void retireCoveredConsent() {
      if (!completed &&
          mounted &&
          serial == _voiceSerial &&
          identical(_voiceConsentRoute, route)) {
        _cancelVoice();
      }
    }

    route = DialogRoute<VoiceInputChoice>(
      context: context,
      themes: InheritedTheme.capture(
        from: context,
        to: Navigator.of(context, rootNavigator: true).context,
      ),
      barrierLabel: MaterialLocalizations.of(context).modalBarrierDismissLabel,
      builder: (dialogContext) => _VoiceConsentRouteBoundary(
        onCovered: retireCoveredConsent,
        child: ValueListenableBuilder<int>(
          valueListenable: _materialMenuUpdates,
          builder: (dialogContext, _, _) => AlertDialog(
            scrollable: true,
            title: Text(
              '语音输入',
              style: NowUiTokens.reviewedOf(dialogContext).sheetTitle,
            ),
            content: Text(
              local
                  ? service is VoiceBundledEngine
                        ? '声音只在本机处理，不上传。录音仅存内存，处理完成或取消后清除。最多录音30秒，可随时停止并转成文字，由你编辑和发送。'
                        : '在设备上识别语音，转成文字后由你编辑和发送。'
                  : service is VoiceSystemActivity
                  ? '将打开设备的语音界面（${service.name}）。该服务可能联网处理声音，返回文字后由你编辑和发送。详情可在设置中查看。'
                  : '设备的语音服务可能联网处理声音。转成文字后由你编辑和发送。详情可在设置中查看。',
              style: NowUiTokens.reviewedOf(dialogContext).body,
            ),
            actions: [
              TextButton(
                onPressed: () {
                  completed = true;
                  Navigator.pop(dialogContext);
                },
                child: const Text('取消'),
              ),
              TextButton(
                key: const Key('now-voice-consent-confirm'),
                onPressed: current()
                    ? () {
                        if (!current()) return;
                        completed = true;
                        Navigator.pop(
                          dialogContext,
                          VoiceInputChoice(systemApproved: !local),
                        );
                      }
                    : null,
                child: const Text('开始'),
              ),
            ],
          ),
        ),
      ),
    );
    _voiceConsentRoute = route;
    try {
      final choice = await Navigator.of(
        context,
        rootNavigator: true,
      ).push(route);
      if (choice != null && mounted && current()) {
        await _voiceConsent.acknowledge(service);
      }
      return mounted && current() ? choice : null;
    } finally {
      if (identical(_voiceConsentRoute, route)) _voiceConsentRoute = null;
    }
  }

  Future<void> _startVoice() async {
    if (!_voice.canUse ||
        _voice.active ||
        widget.humanMode ||
        widget.submitBusy ||
        ModalRoute.of(context)?.isCurrent == false) {
      return;
    }
    final serial = ++_voiceSerial, boundary = _boundarySerial;
    final workspace = widget.workspace, epoch = workspace.taskIdentityEpoch;
    final material = _materialContext, revision = _draftRevision;
    final value = _text.value;
    _voiceInitialValue = value;
    bool current() =>
        mounted &&
        serial == _voiceSerial &&
        boundary == _boundarySerial &&
        workspace == widget.workspace &&
        epoch == widget.workspace.taskIdentityEpoch &&
        material == _materialContext &&
        revision == _draftRevision &&
        value.text == _text.text &&
        value.composing == _text.value.composing &&
        (_voiceConsentRoute?.isCurrent == true ||
            ModalRoute.of(context)?.isCurrent != false);
    final text = await _voice.capture(
      current: current,
      choose: (availability) => _chooseVoice(availability, current),
      confirmSystem: (service) async =>
          (await _chooseVoice(
            service is VoiceSystemActivity
                ? VoiceAvailability(systemActivity: service)
                : VoiceAvailability(systemService: service),
            current,
            systemOnly: true,
          ))?.systemApproved ==
          true,
    );
    if (!current() || !_voice.resumed) return;
    _voiceInitialValue = null;
    if (text == null) {
      if (_voice.message != null) _feedback(_voice.message!);
      return;
    }
    final recognized = text.trim();
    final selection = value.selection;
    final start = selection.isValid ? selection.start : value.text.length;
    final end = selection.isValid ? selection.end : value.text.length;
    final draft = value.text.replaceRange(start, end, recognized);
    if (recognized.isEmpty) {
      _feedback('没有识别到语音，请重试或使用键盘。');
    } else {
      _hasMaterial = true;
      fillDraft(draft, requestFocus: false);
      _feedback(
        utf8.encode(draft.trim()).length > widget.maxDraftBytes
            ? '文字较长，已保留，请缩短后发送。'
            : '已转为可编辑文字，请检查后发送。',
      );
    }
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
    pauseEditing();
    _refresh();
  }

  @override
  void initState() {
    super.initState();
    _voiceConsent = widget.voiceConsentStore ?? SecureVoiceInputConsentStore();
    _attachVoice();
    if (_composerFocusObservationEnabled) {
      FocusManager.instance.addListener(_onManagerFocusTrace);
      _traceFocus('init');
    }
    _focus.addListener(_onFocus);
    _text.addListener(_onDraftEdit);
    _observedContext = _materialContext;
    _observedTaskIdentityEpoch = widget.workspace.taskIdentityEpoch;
    widget.workspace.addListener(_onMaterialBoundaryChange);
    widget.materialContextChanges?.addListener(_onMaterialBoundaryChange);
    _applyExternalDraft();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_voice.active &&
        _voiceConsentRoute?.isCurrent != true &&
        ModalRoute.of(context)?.isCurrent == false) {
      _cancelVoice();
    }
    if (_composerFocusTraceEnabled) {
      _focusTraceDPR = MediaQuery.maybeOf(context)?.devicePixelRatio;
      _traceFocus('dependencies');
    }
  }

  @override
  void didUpdateWidget(AgentComposer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.voiceInputPort != widget.voiceInputPort) {
      _cancelVoice();
      _voice.removeListener(_refresh);
      _voice.dispose();
      _attachVoice();
    }
    if (oldWidget.onPreferredHeightChanged == null &&
        widget.onPreferredHeightChanged != null) {
      _reportedPreferredHeight = null;
    }
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
    if (oldWidget.imageMaterialStatus != widget.imageMaterialStatus ||
        oldWidget.imageMaterialRecovering != widget.imageMaterialRecovering ||
        (oldWidget.onDraftImage == null) != (widget.onDraftImage == null) ||
        (oldWidget.onRefreshImageMaterial == null) !=
            (widget.onRefreshImageMaterial == null)) {
      _refresh();
    }
    if (oldWidget.draftIdentity != widget.draftIdentity) {
      _cancelVoice();
      _clearCompositionFeedback();
      _hasMaterial = false;
      _feedbackMessage = null;
    }
    if (oldWidget.draftIdentity != widget.draftIdentity ||
        oldWidget.humanMode != widget.humanMode ||
        oldWidget.friendConversation != widget.friendConversation) {
      // Owned image routes must retire even if the composer returns to the
      // original recipient before the next frame.
      ++_humanImageViewSerial;
      _refresh();
    }
    _applyExternalDraft();
  }

  void _applyExternalDraft() {
    final value = widget.draftValue;
    if (value == null || value == _text.value) return;
    _applyingExternalDraft = true;
    _text.value = value;
    _applyingExternalDraft = false;
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
    if (widget.needsLogin) {
      widget.onSignIn?.call();
      return;
    }
    if (widget.submitBusy) return;
    if (_voice.active) {
      _cancelVoice();
      return;
    }
    if (_text.value.composing.isValid && !_text.value.composing.isCollapsed) {
      _blockedComposition = _text.value;
      _feedback(_compositionFeedback);
      if (!_feedbackFits && _compositionSnackBar == null) {
        final notice = ScaffoldMessenger.maybeOf(context)?.showSnackBar(
          SnackBar(
            behavior: SnackBarBehavior.floating,
            margin: EdgeInsets.fromLTRB(16, 0, 16, _compositionNoticeBottom()),
            content: const Text(_compositionFeedback),
          ),
        );
        _compositionSnackBar = notice;
        notice?.closed.then((_) {
          if (mounted && identical(_compositionSnackBar, notice)) {
            _compositionSnackBar = null;
          }
        });
      }
      return;
    }
    if (!widget.humanMode && widget.creationLabel != null) {
      final text = _text.text;
      if (text.trim().isEmpty &&
          widget.imageDraft?.localBytes == null &&
          widget.videoPreview == null) {
        return;
      }
      if (utf8.encode(text).length > widget.maxDraftBytes) {
        _feedback('文字较长，已保留，请缩短后更新草稿。');
        return;
      }
      _feedbackMessage = null;
      ++_pasteSerial;
      _readingClipboard = false;
      _focus.unfocus();
      // Updating an inline creation draft neither approves its media nor
      // dispatches an Agent analysis or an external publish action.
      widget.onSubmit(text);
      return;
    }
    final friend = widget.humanMode ? widget.friendConversation : null;
    final imageOnly = friend?.imageDraft.localBytes != null;
    final query = imageOnly ? _text.text : _text.text.trim();
    if (widget.humanMode) {
      if (imageOnly
          ? !_friendImageVisible || friend?.canSendImage(query) != true
          : query.isNotEmpty
          ? friend?.canSendText != true
          : friend?.canSendAudio != true)
        return;
    } else if (query.isEmpty &&
        (widget.onAnalyzeImage == null ||
            widget.imageDraft?.localBytes == null))
      return;
    if (!widget.humanMode && utf8.encode(query).length > widget.maxDraftBytes) {
      _feedback('文字较长，已保留，请缩短后发送。');
      return;
    }
    final image = widget.imageDraft;
    if (!widget.humanMode &&
        image?.localBytes != null &&
        widget.onAnalyzeImage != null) {
      _focus.unfocus();
      widget.onAnalyzeImage!(query);
      return;
    }
    if (!widget.humanMode && image?.localBytes != null) {
      // The current thumbnail plus this explicit send gesture approves this
      // exact local version. Selecting an image alone never uploads it.
      image!.setReviewed(true);
      if (!image.canUse) return;
    }
    if (widget.draftIdentity == null && !widget.humanMode) _text.clear();
    _hasMaterial = false;
    _feedbackMessage = null;
    ++_pasteSerial;
    // Sending the current text retires its in-flight paste immediately. A late
    // response cannot keep the next message's paste action locked.
    _readingClipboard = false;
    _focus.unfocus();
    widget.onSubmit(query);
  }

  void _clearCompositionFeedback() {
    _blockedComposition = null;
    if (_feedbackMessage == _compositionFeedback) _feedbackMessage = null;
    _compositionSnackBar?.close();
    _compositionSnackBar = null;
  }

  double _compositionNoticeBottom() {
    final scaffold = Scaffold.maybeOf(context);
    final surface = context.findRenderObject();
    final scaffoldSurface = scaffold?.context.findRenderObject();
    if (surface is! RenderBox ||
        scaffoldSurface is! RenderBox ||
        !surface.attached ||
        !scaffoldSurface.attached ||
        !surface.hasSize ||
        !scaffoldSurface.hasSize) {
      return _minimumInputHeight + 24;
    }
    final top = surface
        .localToGlobal(Offset.zero, ancestor: scaffoldSurface)
        .dy;
    final inset = (scaffold?.widget.resizeToAvoidBottomInset ?? true)
        ? MediaQuery.viewInsetsOf(context).bottom
        : 0.0;
    // The real input position includes the map's keyboard/safe-area offset.
    // Keep the notice above it under either Scaffold resize policy.
    return (scaffoldSurface.size.height - inset - top + 8)
        .clamp(8.0, scaffoldSurface.size.height)
        .toDouble();
  }

  /// The gallery result belongs to this exact recipient and text version.
  /// It never invokes Agent review, uploads bytes, or creates a conversation.
  Future<void> selectFriendImage({
    MomentImageSource source = MomentImageSource.gallery,
  }) async {
    final friend = widget.friendConversation;
    if (!mounted ||
        !widget.humanMode ||
        friend?.canSelectImageSource(source) != true)
      return;
    final identity = widget.draftIdentity, revision = _draftRevision;
    final epoch = friend!.imageEpoch;
    bool current() =>
        mounted &&
        widget.humanMode &&
        identical(widget.friendConversation, friend) &&
        widget.draftIdentity == identity &&
        _draftRevision == revision &&
        friend.imageViewCurrent(epoch);
    pauseEditing();
    await friend.selectImage(gestureCurrent: current, source: source);
    if (current() && friend.imageDraft.error != null)
      _feedback(friend.imageDraft.error!);
  }

  Future<void> _showHumanImageMenu() async {
    final friend = widget.friendConversation;
    if (friend == null || !friend.imageEnabled || !friend.current) return;
    final identity = widget.draftIdentity, epoch = friend.imageEpoch;
    final viewSerial = _humanImageViewSerial;
    bool current() =>
        mounted &&
        viewSerial == _humanImageViewSerial &&
        widget.humanMode &&
        identical(widget.friendConversation, friend) &&
        widget.draftIdentity == identity &&
        friend.imageViewCurrent(epoch);
    // Keep this subscription identity stable through inherited-widget rebuilds,
    // including after the captured controller has been disposed.
    final changes = Listenable.merge([
      friend,
      friend.imageDraft,
      _materialMenuUpdates,
    ]);
    _materialMenuOpen = true;
    pauseEditing();
    String? action;
    try {
      action = await showModalBottomSheet<String>(
        context: context,
        useSafeArea: true,
        showDragHandle: true,
        isScrollControlled: true,
        builder: (sheet) => _FriendImageRouteBuilder(
          // Controller disposal notifies its draft after closing its own
          // notifier. Listen to both so old recipient labels disappear too.
          changes: changes,
          current: current,
          builder: (sheet) => SafeArea(
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Padding(
                    padding: const EdgeInsets.all(16),
                    child: Text(
                      current()
                          ? '发给 ${friend.conversation.otherName}'
                          : '图片操作已失效',
                      style: NowUiTokens.of(sheet).sheetTitle,
                    ),
                  ),
                  if (!current())
                    TextButton(
                      onPressed: () => Navigator.pop(sheet),
                      child: const Text('关闭'),
                    ),
                  if (current())
                    _QuickActionTile(
                      icon: Icons.photo_library_outlined,
                      title: '本机图片',
                      subtitle: '选择只形成草稿；发送前请核对收件人与图片',
                      onTap:
                          current() &&
                              !friend.sending &&
                              !friend.pending &&
                              !friend.audioActive &&
                              friend.audioDraft == null
                          ? () => Navigator.pop(sheet, 'select')
                          : null,
                    ),
                  for (final (source, icon, title) in [
                    (
                      MomentImageSource.camera,
                      Icons.photo_camera_outlined,
                      '拍照',
                    ),
                    (
                      MomentImageSource.file,
                      Icons.insert_drive_file_outlined,
                      '图片文件',
                    ),
                  ])
                    if (current() && friend.imageDraft.supportsSource(source))
                      _QuickActionTile(
                        icon: icon,
                        title: title,
                        subtitle: '先形成本机草稿；发送前核对收件人与图片',
                        onTap: current() && friend.canSelectImageSource(source)
                            ? () => Navigator.pop(sheet, source.name)
                            : null,
                      ),
                  if (current() && friend.imageDraft.localBytes != null)
                    _QuickActionTile(
                      icon: Icons.image_outlined,
                      title: '查看与遮盖图片',
                      onTap:
                          current() &&
                              !friend.sending &&
                              !friend.imageDraft.busy
                          ? () => Navigator.pop(sheet, 'preview')
                          : null,
                    ),
                  if (current() && friend.imageDraft.localBytes != null)
                    _QuickActionTile(
                      icon: Icons.close,
                      title: '移除本机图片',
                      subtitle: '保留这段对话中的文字',
                      onTap:
                          current() &&
                              !friend.sending &&
                              !friend.imageDraft.busy
                          ? () => Navigator.pop(sheet, 'remove')
                          : null,
                    ),
                ],
              ),
            ),
          ),
        ),
      );
    } finally {
      _materialMenuOpen = false;
    }
    if (!current()) return;
    // Let the owned sheet finish retiring so the native-picker gesture starts
    // only while the original Now route is visible again.
    await WidgetsBinding.instance.endOfFrame;
    if (!current()) return;
    if (action == 'select') await selectFriendImage();
    if (action == 'camera') {
      await selectFriendImage(source: MomentImageSource.camera);
    }
    if (action == 'file') {
      await selectFriendImage(source: MomentImageSource.file);
    }
    if (action == 'preview') await _showHumanImagePreview();
    if (action == 'remove') friend.imageDraft.removeLocal();
  }

  Future<void> _showHumanImagePreview() async {
    final friend = widget.friendConversation;
    if (!mounted ||
        !widget.humanMode ||
        friend == null ||
        friend.imageDraft.localBytes == null ||
        friend.sending)
      return;
    final identity = widget.draftIdentity, epoch = friend.imageEpoch;
    final viewSerial = _humanImageViewSerial;
    bool current() =>
        mounted &&
        viewSerial == _humanImageViewSerial &&
        widget.humanMode &&
        identical(widget.friendConversation, friend) &&
        widget.draftIdentity == identity &&
        friend.imageViewCurrent(epoch);
    // Keep this subscription identity stable through inherited-widget rebuilds,
    // including after the captured controller has been disposed.
    final changes = Listenable.merge([
      friend,
      friend.imageDraft,
      _materialMenuUpdates,
    ]);
    pauseEditing();
    await showDialog<void>(
      context: context,
      builder: (dialog) => _FriendLocalPreview(
        draft: friend.imageDraft,
        changes: changes,
        current: current,
        recipient: friend.conversation.otherName,
      ),
    );
  }

  Future<void> showNowMaterialMenu() async {
    if (!mounted || _materialMenuOpen) return;
    if (widget.humanMode) {
      await _showHumanImageMenu();
      return;
    }
    _materialMenuOpen = true;
    final contextKey = _materialContext, boundary = _boundarySerial;
    final epoch = widget.workspace.taskEpoch;
    var chosen = false;
    bool current() =>
        mounted &&
        boundary == _boundarySerial &&
        contextKey == _materialContext &&
        epoch == widget.workspace.taskEpoch;
    void choose(BuildContext sheetContext, VoidCallback? action) {
      if (chosen || !sheetContext.mounted) return;
      chosen = true;
      Navigator.pop(sheetContext);
      if (current()) action?.call();
    }

    pauseEditing();
    try {
      await showModalBottomSheet<void>(
        context: context,
        showDragHandle: true,
        isScrollControlled: true,
        useSafeArea: true,
        builder: (sheetContext) => AnimatedBuilder(
          animation: _materialMenuUpdates,
          builder: (sheetContext, _) => SafeArea(
            child: SingleChildScrollView(
              child: Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 20),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(
                      padding: const EdgeInsets.all(8),
                      child: Text(
                        '添加材料',
                        style: NowUiTokens.of(sheetContext).sheetTitle,
                      ),
                    ),
                    if (_feedbackMessage != null)
                      Padding(
                        padding: const EdgeInsets.all(8),
                        child: Semantics(
                          liveRegion: true,
                          child: Text(
                            _feedbackMessage!,
                            style: NowUiTokens.of(sheetContext).metadata,
                          ),
                        ),
                      ),
                    Padding(
                      padding: const EdgeInsets.fromLTRB(8, 12, 8, 4),
                      child: Text(
                        '选择材料',
                        style: NowUiTokens.of(sheetContext).placeTitle,
                      ),
                    ),
                    _QuickActionTile(
                      icon: Icons.photo_library_outlined,
                      title: '图片',
                      subtitle: !current()
                          ? '账号或情境已变化，请重新打开材料菜单'
                          : widget.imageMaterialStatus ??
                                (widget.imageMaterialRecovering
                                    ? '正在恢复图片入口…'
                                    : widget.onDraftImage != null
                                    ? '选择本机图片，检查后再使用'
                                    : widget.needsLogin
                                    ? '登录后可选择本机图片'
                                    : '图片暂不可用，文字草稿已保留'),
                      onTap:
                          !current() ||
                              widget.imageMaterialRecovering ||
                              widget.onDraftImage == null
                          ? null
                          : () => choose(sheetContext, widget.onDraftImage),
                    ),
                    if (widget.onDraftImage == null &&
                        widget.needsLogin &&
                        widget.onSignIn != null)
                      _QuickActionTile(
                        icon: Icons.login,
                        title: '登录后添加图片',
                        onTap: () => choose(sheetContext, widget.onSignIn),
                      )
                    else if (widget.onDraftImage == null &&
                        widget.onRefreshImageMaterial != null)
                      _QuickActionTile(
                        icon: Icons.refresh,
                        title: '重新读取图片能力',
                        onTap: widget.imageMaterialRecovering
                            ? null
                            : () => choose(
                                sheetContext,
                                widget.onRefreshImageMaterial,
                              ),
                      ),
                    if (widget.onVideoMaterial != null)
                      _QuickActionTile(
                        icon: Icons.video_library_outlined,
                        title: '视频',
                        onTap: () =>
                            choose(sheetContext, widget.onVideoMaterial),
                      ),
                    if (widget.onCaptureImage != null)
                      _QuickActionTile(
                        icon: Icons.photo_camera_outlined,
                        title: '拍照',
                        subtitle: '拍摄后先在本机检查，不会自动上传',
                        onTap: !current() || widget.imageMaterialRecovering
                            ? null
                            : () => choose(sheetContext, widget.onCaptureImage),
                      ),
                    if (widget.onImageFile != null)
                      _QuickActionTile(
                        icon: Icons.insert_drive_file_outlined,
                        title: '文件',
                        subtitle: '选择 JPEG 或 PNG 图片文件，检查后再使用',
                        onTap: !current() || widget.imageMaterialRecovering
                            ? null
                            : () => choose(sheetContext, widget.onImageFile),
                      ),
                    if (widget.imageDraft?.localBytes != null)
                      _QuickActionTile(
                        icon: Icons.close,
                        title: '移除本机图片',
                        subtitle: '保留原输入框中的文字',
                        onTap: widget.imageDraft?.canSelect == true
                            ? () => choose(
                                sheetContext,
                                widget.imageDraft?.removeLocal,
                              )
                            : null,
                      ),
                    if (widget.onTaskImage != null)
                      _QuickActionTile(
                        icon: Icons.image_outlined,
                        title: '查看已保存会话图片',
                        subtitle: '读取当前会话中本人可见的图片',
                        onTap: () => choose(sheetContext, widget.onTaskImage),
                      ),
                    if (widget.onCreateMoment != null ||
                        widget.onDraftContext != null ||
                        widget.onCreateActivity != null ||
                        widget.onCreateJourney != null ||
                        widget.onCreateThemeMap != null ||
                        widget.onCreateRoute != null ||
                        widget.onImportPlan != null ||
                        widget.onSavePlace != null) ...[
                      const Divider(height: 24),
                      Padding(
                        padding: const EdgeInsets.fromLTRB(8, 0, 8, 4),
                        child: Text(
                          '创建草稿',
                          style: NowUiTokens.of(sheetContext).placeTitle,
                        ),
                      ),
                      for (final (icon, label, action) in [
                        (
                          Icons.alternate_email,
                          '草稿引用与材料',
                          widget.onDraftContext,
                        ),
                        (Icons.edit_outlined, '动态', widget.onCreateMoment),
                        (Icons.event_outlined, '活动', widget.onCreateActivity),
                        (Icons.route_outlined, '旅程', widget.onCreateJourney),
                        (Icons.map_outlined, '主题地图', widget.onCreateThemeMap),
                        (Icons.alt_route, '多站路线草稿', widget.onCreateRoute),
                        (
                          Icons.file_open_outlined,
                          '导入外部计划',
                          widget.onImportPlan,
                        ),
                        (Icons.bookmark_outline, '收藏地点', widget.onSavePlace),
                      ])
                        if (action != null)
                          _QuickActionTile(
                            icon: icon,
                            title: label,
                            onTap: () => choose(sheetContext, action),
                          ),
                    ],
                  ],
                ),
              ),
            ),
          ),
        ),
      );
    } finally {
      _materialMenuOpen = false;
    }
  }

  // Kept for callers compiled against the older helper. Its UI is the same
  // current-draft material menu; clipboard and private-record actions live in
  // their native editor/record flows.
  Future<void> showMaterialTools(BuildContext context) => showNowMaterialMenu();

  void _feedback(String message) {
    if (!mounted) return;
    _feedbackMessage = message;
    _refresh();
  }

  Future<void> _pasteText() async {
    if (_readingClipboard) return;
    final serial = ++_pasteSerial;
    final contextKey = _materialContext;
    // A pending request can retire without replacing this conversation.
    // City/ONLINE changes are tracked separately by the material context.
    final epoch = widget.workspace.taskIdentityEpoch;
    final revision = _draftRevision;
    bool current() =>
        mounted &&
        serial == _pasteSerial &&
        contextKey == _materialContext &&
        epoch == widget.workspace.taskIdentityEpoch;
    _readingClipboard = true;
    _feedbackMessage = null;
    _refresh();
    try {
      final text = (await Clipboard.getData(
        Clipboard.kTextPlain,
      ))?.text?.trim();
      if (!mounted || !current()) return;
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
      if (utf8.encode(next).length > widget.maxDraftBytes) {
        _feedback('文字较长，未粘贴且保留原草稿。请缩短后重试。');
        return;
      }
      _hasMaterial = true;
      // An async read may finish while a menu or detail route is in front.
      // Preserve the draft without bringing the background editor's IME back.
      fillDraft(next, requestFocus: ModalRoute.of(context)?.isCurrent == true);
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
    ++_voiceSerial;
    _voice.removeListener(_refresh);
    _voice.dispose();
    // The owning Scaffold retires its presentation; do not start its animation
    // while this subtree is already deactivated.
    _blockedComposition = null;
    _compositionSnackBar = null;
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
    _feedbackScroll.dispose();
    _materialMenuUpdates.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => _ComposerSizeReporter(
    onSize: (size) => widget.onHeightChanged?.call(size.height),
    child: LayoutBuilder(
      builder: (context, constraints) => AnimatedBuilder(
        animation: Listenable.merge([
          widget.workspace,
          if (widget.imageDraft != null) widget.imageDraft!,
          if (widget.imageDraft != null) widget.imageDraft!.sourceChanges,
          if (widget.imageUpload != null) widget.imageUpload!,
          if (widget.friendConversation != null) widget.friendConversation!,
        ]),
        builder: (context, _) {
          final minimumInputHeight = AgentComposer.minimumInputHeightOf(
            context,
          );
          final inputStyle = AgentComposer._inputStyleOf(context);
          final tokens = NowUiTokens.reviewedOf(context);
          final actionRowHeight = AgentComposer._minimumSuggestionHeightOf(
            context,
          );
          final suggestionHeight = widget.suggestions.isEmpty
              ? 0.0
              : actionRowHeight;
          final searching = widget.workspace.state == AgentViewState.searching;
          final friend = widget.humanMode ? widget.friendConversation : null;
          final voiceActive = widget.humanMode
              ? (friend?.audioActive ?? false)
              : _voice.active;
          final recording = widget.humanMode
              ? (friend?.recording ?? false)
              : _voice.recording;
          final creating = !widget.humanMode && widget.creationLabel != null;
          final hasText = _text.text.trim().isNotEmpty;
          final elapsed = widget.humanMode
              ? (friend?.elapsedLabel ?? '0:00')
              : '${_voice.elapsed.inMinutes}:${(_voice.elapsed.inSeconds % 60).toString().padLeft(2, '0')}';
          final hintText = widget.needsLogin
              ? '登录后继续，草稿已保留'
              : _voice.active
              ? switch (_voice.phase) {
                  VoiceInputPhase.checking => '检查语音能力',
                  VoiceInputPhase.listening => '录音 $elapsed',
                  VoiceInputPhase.consent => '等待语音授权…',
                  VoiceInputPhase.permission => '等待麦克风授权…',
                  VoiceInputPhase.processing => '正在转成文字…',
                  VoiceInputPhase.awaitingSystemUi => '等待系统语音界面…',
                  _ => '正在准备语音…',
                }
              : widget.humanMode
              ? (recording
                    ? '录音 $elapsed'
                    : '发给 ${widget.recipientName ?? '当前联系人'}')
              : widget.creationInputHint != null
              ? widget.creationInputHint!
              : widget.onlineMode
              ? '想查找什么公开线上意图？'
              : _focus.hasFocus
              ? '描述你想做什么'
              : '你想做什么？';
          final image = _visibleImage;
          final hasImage = image?.current == true && image?.localBytes != null;
          final hasVideo = !widget.humanMode && widget.videoPreview != null;
          final videoStatus = widget.humanMode
              ? null
              : widget.videoStatus?.trim();
          final hasVideoStatus = videoStatus?.isNotEmpty == true;
          final creationHeight = creating ? actionRowHeight + 8 : 0.0;
          final preferredImageHeight =
              math.max(72.0, actionRowHeight) + NowUiTokens.space8;
          final preferredVideoHeight = hasVideo ? 80.0 : 0.0;
          final videoStatusHeight = hasVideoStatus
              ? math.max(
                      NowUiTokens.actionHitSize,
                      _naturalTextHeight(
                        videoStatus!,
                        tokens.metadata,
                        constraints.maxWidth - NowUiTokens.space16,
                      ),
                    ) +
                    8
              : 0.0;
          final naturalMaterialHeight =
              creationHeight +
              (hasImage ? preferredImageHeight : 0) +
              preferredVideoHeight +
              videoStatusHeight;
          final materialHeight = widget.humanMode
              ? hasImage &&
                        (!constraints.hasBoundedHeight ||
                            constraints.maxHeight >=
                                minimumInputHeight + preferredImageHeight)
                    ? preferredImageHeight
                    : 0.0
              : constraints.hasBoundedHeight
              ? math.min(
                  naturalMaterialHeight,
                  math.max(0.0, constraints.maxHeight - minimumInputHeight),
                )
              : naturalMaterialHeight;
          final showImage = hasImage && materialHeight > 0;
          _friendImageVisible = widget.humanMode && showImage;
          final canSend =
              !widget.submitBusy &&
              !widget.needsLogin &&
              !voiceActive &&
              (widget.humanMode
                  ? friend?.imageDraft.localBytes != null
                        ? showImage && friend?.canSendImage(_text.text) == true
                        : hasText
                        ? friend?.canSendText == true
                        : friend?.canSendAudio == true
                  : (hasText ||
                            (creating
                                ? hasImage || hasVideo
                                : hasImage &&
                                      showImage &&
                                      widget.onAnalyzeImage != null)) &&
                        utf8
                                .encode(
                                  creating ? _text.text : _text.text.trim(),
                                )
                                .length <=
                            widget.maxDraftBytes);
          final eligibleSuggestions =
              !widget.workspace.inputFocused &&
              widget.workspace.task == null &&
              widget.suggestions.isNotEmpty;
          final showSuggestions =
              eligibleSuggestions &&
              (!constraints.hasBoundedHeight ||
                  constraints.maxHeight >=
                      minimumInputHeight +
                          suggestionHeight +
                          (widget.humanMode ? 0 : materialHeight));
          final naturalFeedbackHeight = _feedbackMessage == null
              ? 0.0
              : math
                    .max(
                      NowUiTokens.actionHitSize,
                      _naturalTextHeight(
                            _feedbackMessage!,
                            tokens.body,
                            constraints.maxWidth -
                                NowUiTokens.actionHitSize -
                                NowUiTokens.space16,
                          ) +
                          NowUiTokens.space16,
                    )
                    .ceilToDouble();
          final feedbackHeight = constraints.hasBoundedHeight
              ? (constraints.maxHeight -
                        minimumInputHeight -
                        materialHeight -
                        8 -
                        (showSuggestions ? suggestionHeight : 0))
                    .clamp(0.0, naturalFeedbackHeight)
                    .toDouble()
              : naturalFeedbackHeight;
          final showFeedback = _feedbackMessage != null && feedbackHeight >= 48;
          _feedbackFits = feedbackHeight >= 48;
          final inputMaxHeight = constraints.hasBoundedHeight
              ? (constraints.maxHeight -
                        (showFeedback ? feedbackHeight + 8 : 0) -
                        materialHeight -
                        (showSuggestions ? suggestionHeight : 0))
                    .clamp(0.0, constraints.maxHeight)
                    .toDouble()
              : double.infinity;
          final inputMinHeight = inputMaxHeight
              .clamp(0.0, minimumInputHeight)
              .toDouble();
          if (widget.onPreferredHeightChanged != null &&
              constraints.hasBoundedWidth) {
            final hasMaterialAction =
                widget.onDraftImage != null ||
                widget.imageMaterialStatus != null ||
                widget.imageMaterialRecovering ||
                widget.onRefreshImageMaterial != null ||
                widget.onTaskImage != null ||
                widget.onVideoMaterial != null ||
                widget.onCreateMoment != null ||
                widget.onDraftContext != null ||
                widget.onCreateActivity != null ||
                widget.onCreateJourney != null ||
                widget.onCreateThemeMap != null ||
                widget.onCreateRoute != null ||
                widget.onImportPlan != null ||
                widget.onSavePlace != null;
            final editorWidth =
                constraints.maxWidth -
                2 * NowUiTokens.space4 -
                2 * NowUiTokens.actionHitSize -
                (hasMaterialAction ? NowUiTokens.actionHitSize : 0) -
                (searching ? 25 : 0) -
                (voiceActive ? NowUiTokens.actionHitSize : 0) -
                (widget.reserveKeyboardRestoreSpace
                    ? NowUiTokens.actionHitSize
                    : 0);
            final editorHeight = math.max(
              minimumInputHeight,
              _naturalTextHeight(
                    _text.text.isEmpty ? hintText : _text.text,
                    inputStyle,
                    editorWidth,
                    maxLines: 4,
                  ) +
                  AgentComposer.inputContentPadding.vertical,
            );
            _reportPreferredHeight(
              editorHeight +
                  naturalMaterialHeight +
                  (eligibleSuggestions ? suggestionHeight : 0) +
                  (naturalFeedbackHeight > 0 ? naturalFeedbackHeight + 8 : 0),
            );
          }
          return Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (materialHeight > 0)
                SizedBox(
                  height: materialHeight,
                  child: SingleChildScrollView(
                    key: const Key('now-composer-materials'),
                    primary: false,
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (creating) ...[
                          SizedBox(
                            height: actionRowHeight,
                            child: Align(
                              alignment: Alignment.centerLeft,
                              child: InputChip(
                                key: const Key('now-composer-creation-chip'),
                                label: Text(
                                  '创建${widget.creationLabel!}',
                                  style: tokens.action,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                ),
                                onDeleted: widget.submitBusy
                                    ? null
                                    : widget.onExitCreation,
                                deleteButtonTooltipMessage: '退出创建',
                              ),
                            ),
                          ),
                          const SizedBox(height: 8),
                        ],
                        if (showImage) ...[
                          SizedBox(
                            height: math.max(72.0, actionRowHeight),
                            child: Row(
                              children: [
                                Padding(
                                  padding: const EdgeInsets.only(right: 8),
                                  child: ClipRRect(
                                    borderRadius: BorderRadius.circular(8),
                                    child: widget.humanMode
                                        ? _OwnedLocalPixels(
                                            bytes: image!.localBytes!,
                                            key: const Key(
                                              'now-composer-image-thumbnail',
                                            ),
                                            width: 56,
                                            height: 56,
                                            fit: BoxFit.cover,
                                            semanticLabel: '待发送图片',
                                          )
                                        : Image.memory(
                                            image!.localBytes!,
                                            key: const Key(
                                              'now-composer-image-thumbnail',
                                            ),
                                            width: 56,
                                            height: 56,
                                            fit: BoxFit.cover,
                                            semanticLabel: '待发送图片',
                                          ),
                                  ),
                                ),
                                Expanded(
                                  child: TextButton.icon(
                                    onPressed: image.busy || widget.submitBusy
                                        ? null
                                        : widget.humanMode
                                        ? _showHumanImagePreview
                                        : widget.onDraftImage,
                                    icon: const Icon(Icons.image_outlined),
                                    label: Text(
                                      widget.humanMode
                                          ? '图片 · 发给 ${widget.recipientName ?? '当前联系人'}'
                                          : creating
                                          ? '图片 · 待加入${widget.creationLabel!}'
                                          : '图片 · 助手暂不读图',
                                      style: tokens.action,
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                    ),
                                  ),
                                ),
                                IconButton(
                                  tooltip: '移除本机图片',
                                  onPressed: image.busy || widget.submitBusy
                                      ? null
                                      : image.removeLocal,
                                  icon: const Icon(Icons.close),
                                ),
                              ],
                            ),
                          ),
                          const SizedBox(height: 8),
                        ],
                        if (hasVideo) ...[
                          SizedBox(
                            key: const Key('now-composer-video-preview'),
                            height: 72,
                            child: widget.videoPreview!,
                          ),
                          const SizedBox(height: 8),
                        ],
                        if (hasVideoStatus)
                          SizedBox(
                            height: videoStatusHeight,
                            child: Padding(
                              padding: const EdgeInsets.fromLTRB(8, 0, 8, 8),
                              child: Semantics(
                                liveRegion: true,
                                child: Text(
                                  videoStatus!,
                                  style: tokens.metadata,
                                ),
                              ),
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
              if (showFeedback) ...[
                SizedBox(
                  height: feedbackHeight,
                  child: BirdtieSurface(
                    kind: BirdtieSurfaceKind.critical,
                    child: Row(
                      children: [
                        Expanded(
                          child: Scrollbar(
                            controller: _feedbackScroll,
                            thumbVisibility: true,
                            trackVisibility: true,
                            interactive: true,
                            child: SingleChildScrollView(
                              key: ValueKey((
                                'composer-feedback',
                                _feedbackMessage,
                              )),
                              controller: _feedbackScroll,
                              primary: false,
                              padding: const EdgeInsets.all(8),
                              child: Semantics(
                                liveRegion: true,
                                child: Text(
                                  _feedbackMessage!,
                                  style: tokens.body,
                                ),
                              ),
                            ),
                          ),
                        ),
                        IconButton(
                          constraints: const BoxConstraints(
                            minWidth: NowUiTokens.actionHitSize,
                            minHeight: NowUiTokens.actionHitSize,
                          ),
                          visualDensity: VisualDensity.standard,
                          tooltip: _feedbackMessage == _compositionFeedback
                              ? '关闭提示'
                              : '关闭素材提示',
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
                  height: suggestionHeight,
                  child: ListView.separated(
                    scrollDirection: Axis.horizontal,
                    itemCount: widget.suggestions.length,
                    separatorBuilder: (_, _) => const SizedBox(width: 8),
                    itemBuilder: (context, index) => ActionChip(
                      materialTapTargetSize: MaterialTapTargetSize.padded,
                      label: Text(
                        widget.suggestions[index],
                        style: tokens.action,
                      ),
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
              _ComposerInputSurface(
                embedded: widget.embedded,
                child: Container(
                  constraints: BoxConstraints(
                    minHeight: inputMinHeight,
                    maxHeight: inputMaxHeight,
                  ),
                  padding: const EdgeInsets.symmetric(
                    horizontal: NowUiTokens.space4,
                  ),
                  child: Row(
                    children: [
                      if (widget.onDraftImage != null ||
                          widget.imageMaterialStatus != null ||
                          widget.imageMaterialRecovering ||
                          widget.onRefreshImageMaterial != null ||
                          widget.onTaskImage != null ||
                          widget.onVideoMaterial != null ||
                          widget.onCreateMoment != null ||
                          widget.onDraftContext != null ||
                          widget.onCreateActivity != null ||
                          widget.onCreateJourney != null ||
                          widget.onCreateThemeMap != null ||
                          widget.onCreateRoute != null ||
                          widget.onImportPlan != null ||
                          widget.onSavePlace != null)
                        IconButton(
                          style: birdtieFloatingButtonStyle,
                          tooltip: '添加材料',
                          onPressed: () => unawaited(showNowMaterialMenu()),
                          icon: const Icon(Icons.add),
                        ),
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
                                style: inputStyle,
                                minLines: 1,
                                maxLines: 4,
                                enabled: !widget.needsLogin,
                                onTap: resumeEditing,
                                textInputAction: TextInputAction.send,
                                // The default completion clears composing
                                // before onSubmitted. Keep the native draft
                                // intact so both send paths use _submit's guard.
                                onEditingComplete: () {},
                                onSubmitted: (_) => _submit(),
                                onChanged: (_) => setState(() {}),
                                decoration: InputDecoration(
                                  hintText: hintText,
                                  hintStyle: tokens.composerHint,
                                  border: InputBorder.none,
                                  isDense: true,
                                  contentPadding:
                                      AgentComposer.inputContentPadding,
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
                      if (voiceActive)
                        IconButton(
                          key: const Key('now-voice-cancel'),
                          style: birdtieFloatingButtonStyle,
                          tooltip: '取消语音输入',
                          onPressed: widget.humanMode
                              ? friend?.cancelAudio
                              : _cancelVoice,
                          icon: const Icon(Icons.close_rounded, size: 20),
                        ),
                      IconButton(
                        key: const Key('now-composer-microphone'),
                        style: birdtieFloatingButtonStyle,
                        tooltip: voiceActive
                            ? (recording
                                  ? (widget.humanMode ? '停止录音' : '停止录音并转写')
                                  : '正在处理语音')
                            : '语音输入',
                        onPressed: widget.needsLogin
                            ? widget.onSignIn
                            : widget.submitBusy
                            ? null
                            : voiceActive
                            ? (recording
                                  ? () {
                                      if (widget.humanMode) {
                                        unawaited(friend!.stopRecording());
                                      } else {
                                        unawaited(_voice.stop());
                                      }
                                    }
                                  : null)
                            : widget.humanMode
                            ? (friend?.canRecord == true
                                  ? () => unawaited(friend!.startRecording())
                                  : null)
                            : (_voice.canUse
                                  ? () => unawaited(_startVoice())
                                  : null),
                        icon: Icon(
                          recording
                              ? Icons.stop_rounded
                              : Icons.mic_none_rounded,
                          size: 20,
                        ),
                      ),
                      IconButton(
                        key: const Key('now-composer-primary-action'),
                        style: canSend
                            ? birdtieFloatingButtonStyle.copyWith(
                                backgroundColor: WidgetStatePropertyAll(
                                  Theme.of(context).colorScheme.onSurface,
                                ),
                                foregroundColor: WidgetStatePropertyAll(
                                  Theme.of(context).colorScheme.surface,
                                ),
                                shape: const WidgetStatePropertyAll(
                                  CircleBorder(),
                                ),
                              )
                            : birdtieFloatingButtonStyle,
                        tooltip: widget.needsLogin
                            ? '登录后继续'
                            : widget.humanMode
                            ? '发送消息'
                            : creating
                            ? '更新草稿'
                            : '发送需求',
                        onPressed: widget.needsLogin
                            ? widget.onSignIn
                            : canSend
                            ? _submit
                            : null,
                        icon: Icon(
                          widget.needsLogin
                              ? Icons.login_rounded
                              : Icons.arrow_upward_rounded,
                          size: 20,
                        ),
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

/// The Now panel owns the outer surface; the editor has only a fine boundary.
/// Observe the owned dialog itself: Now is already non-current beneath it,
/// so another route covering this dialog does not notify Now's route again.
class _VoiceConsentRouteBoundary extends StatefulWidget {
  const _VoiceConsentRouteBoundary({
    required this.onCovered,
    required this.child,
  });
  final VoidCallback onCovered;
  final Widget child;

  @override
  State<_VoiceConsentRouteBoundary> createState() =>
      _VoiceConsentRouteBoundaryState();
}

class _VoiceConsentRouteBoundaryState
    extends State<_VoiceConsentRouteBoundary> {
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (ModalRoute.isCurrentOf(context) == false) widget.onCovered();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

class _ComposerInputSurface extends StatelessWidget {
  const _ComposerInputSurface({required this.embedded, required this.child});
  final bool embedded;
  final Widget child;

  @override
  Widget build(BuildContext context) => embedded
      ? NowGlassSurface(radius: 28, role: NowGlassRole.composer, child: child)
      : BirdtieSurface(kind: BirdtieSurfaceKind.floating, child: child);
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
    this.subtitle,
    this.onTap,
  });
  final IconData icon;
  final String title;
  final String? subtitle;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) => ListTile(
    minTileHeight: NowUiTokens.actionHitSize,
    enabled: onTap != null,
    leading: Icon(icon),
    title: Text(
      title,
      style: NowUiTokens.reviewedOf(
        context,
      ).action.copyWith(color: NowUiTokens.reviewedOf(context).body.color),
    ),
    subtitle: subtitle == null
        ? null
        : Text(subtitle!, style: NowUiTokens.reviewedOf(context).listSubtitle),
    onTap: onTap,
  );
}

/// A view owns only its image provider; retirement evicts this exact decoded
/// key, without clearing any global image cache or unrelated image.
class _OwnedLocalPixels extends StatefulWidget {
  const _OwnedLocalPixels({
    super.key,
    required this.bytes,
    this.width,
    this.height,
    this.fit = BoxFit.contain,
    this.semanticLabel,
  });
  final Uint8List bytes;
  final double? width, height;
  final BoxFit fit;
  final String? semanticLabel;
  @override
  State<_OwnedLocalPixels> createState() => _OwnedLocalPixelsState();
}

class _OwnedLocalPixelsState extends State<_OwnedLocalPixels> {
  late ImageProvider _provider;
  @override
  void initState() {
    super.initState();
    _replace();
  }

  void _replace() {
    _provider = ResizeImage(
      MemoryImage(widget.bytes),
      width: 1024,
      height: 1024,
      policy: ResizeImagePolicy.fit,
      allowUpscaling: false,
    );
  }

  @override
  void didUpdateWidget(covariant _OwnedLocalPixels oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.bytes, widget.bytes)) {
      unawaited(_provider.evict());
      _replace();
    }
  }

  @override
  Widget build(BuildContext context) => Image(
    image: _provider,
    width: widget.width,
    height: widget.height,
    fit: widget.fit,
    semanticLabel: widget.semanticLabel,
    errorBuilder: (_, __, ___) => const Text('图片未能显示，请重新选择'),
  );
  @override
  void dispose() {
    unawaited(_provider.evict());
    super.dispose();
  }
}

/// A host can retire a friend while rebuilding a different navigator branch.
/// Keep owned modal updates outside that build, and release subscriptions when
/// this route is removed even if the captured controller is already disposed.
class _FriendImageRouteBuilder extends StatefulWidget {
  const _FriendImageRouteBuilder({
    required this.changes,
    required this.current,
    required this.builder,
  });
  final Listenable changes;
  final bool Function() current;
  final WidgetBuilder builder;

  @override
  State<_FriendImageRouteBuilder> createState() =>
      _FriendImageRouteBuilderState();
}

class _FriendImageRouteBuilderState extends State<_FriendImageRouteBuilder> {
  bool _updateScheduled = false;
  Listenable? _subscribedChanges;

  @override
  void initState() {
    super.initState();
    _subscribe();
  }

  void _subscribe() {
    // Retirement can happen after push but before this route's first frame.
    if (!widget.current()) return;
    _subscribedChanges = widget.changes;
    _subscribedChanges!.addListener(_changed);
  }

  @override
  void didUpdateWidget(_FriendImageRouteBuilder oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.changes != widget.changes) {
      _subscribedChanges?.removeListener(_changed);
      _subscribedChanges = null;
      _subscribe();
    }
  }

  void _changed() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_updateScheduled) return;
      _updateScheduled = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _updateScheduled = false;
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  @override
  void dispose() {
    _subscribedChanges?.removeListener(_changed);
    _subscribedChanges = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(context);
}

class _FriendLocalPreview extends StatefulWidget {
  const _FriendLocalPreview({
    required this.draft,
    required this.changes,
    required this.current,
    required this.recipient,
  });
  final LocalImageDraftController draft;
  final Listenable changes;
  final bool Function() current;
  final String recipient;
  @override
  State<_FriendLocalPreview> createState() => _FriendLocalPreviewState();
}

class _FriendLocalPreviewState extends State<_FriendLocalPreview> {
  bool _masking = false;
  Offset? _start, _end;
  @override
  Widget build(BuildContext context) => _FriendImageRouteBuilder(
    changes: widget.changes,
    current: widget.current,
    builder: (context) {
      final draft = widget.draft, bytes = draft.localBytes;
      final valid = widget.current() && bytes != null;
      return Dialog(
        child: SingleChildScrollView(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(valid ? '发给 ${widget.recipient}' : '查看图片'),
                const SizedBox(height: 12),
                if (valid)
                  ConstrainedBox(
                    constraints: const BoxConstraints(maxHeight: 420),
                    child: AspectRatio(
                      aspectRatio: draft.localAspectRatio,
                      child: LayoutBuilder(
                        builder: (context, bounds) => GestureDetector(
                          onPanStart: _masking && !draft.busy
                              ? (event) => setState(() {
                                  _start = event.localPosition;
                                  _end = _start;
                                })
                              : null,
                          onPanUpdate: _masking && !draft.busy
                              ? (event) =>
                                    setState(() => _end = event.localPosition)
                              : null,
                          onPanEnd: _masking && !draft.busy
                              ? (_) {
                                  final start = _start, end = _end;
                                  setState(() {
                                    _start = null;
                                    _end = null;
                                  });
                                  if (start != null &&
                                      end != null &&
                                      widget.current() &&
                                      bounds.maxWidth > 0 &&
                                      bounds.maxHeight > 0) {
                                    unawaited(
                                      draft.mask(
                                        Rect.fromPoints(
                                          Offset(
                                            start.dx / bounds.maxWidth,
                                            start.dy / bounds.maxHeight,
                                          ),
                                          Offset(
                                            end.dx / bounds.maxWidth,
                                            end.dy / bounds.maxHeight,
                                          ),
                                        ),
                                      ),
                                    );
                                  }
                                }
                              : null,
                          child: Stack(
                            fit: StackFit.expand,
                            children: [
                              _OwnedLocalPixels(
                                bytes: bytes,
                                semanticLabel: '本机待发送图片',
                              ),
                              if (_start != null && _end != null)
                                Positioned.fromRect(
                                  rect: Rect.fromPoints(_start!, _end!),
                                  child: ColoredBox(
                                    color: Colors.black.withValues(alpha: .7),
                                  ),
                                ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  )
                else
                  const Text('图片草稿已失效'),
                if (valid && draft.error != null) Text(draft.error!),
                Wrap(
                  children: [
                    TextButton(
                      onPressed: valid && !draft.busy
                          ? () => setState(() => _masking = !_masking)
                          : null,
                      child: Text(_masking ? '完成遮盖' : '拖动遮盖区域'),
                    ),
                    TextButton(
                      onPressed: () => Navigator.pop(context),
                      child: const Text('完成查看'),
                    ),
                  ],
                ),
                const Text('遮盖后仍请核对图片；这不会确认内容安全，也不会自动发送。'),
              ],
            ),
          ),
        ),
      );
    },
  );
}
