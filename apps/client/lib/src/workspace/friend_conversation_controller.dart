import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter/painting.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import 'chat_message_operation.dart';
import 'chat_message_pending_store.dart';
import 'connections.dart';
import 'entity_share_pending_store.dart';
import 'friend_audio.dart';
import 'friend_image.dart';
import 'local_image_draft.dart';
import '../content/moment_image_picker.dart';

/// One immutable human recipient. A reusable thread has no composer: Now owns
/// its sole input and invokes this controller only for its visible recipient.
class FriendConversationController extends ChangeNotifier
    with WidgetsBindingObserver {
  FriendConversationController({
    required this.auth,
    required this.conversation,
    this.workspaceChanges,
    this.workspaceID,
    String? apiBaseUrl,
    http.Client? client,
    FriendAudioPort? audioPort,
    FriendAudioPendingStore? audioPendingStore,
    HumanMessagePendingStore? messagePendingStore,
    this.onUnauthorized,
    this.isVisible,
    this.imageEnabled = false,
    this.imageSourceBinding,
    MomentImagePicker? imagePicker,
    FriendImagePendingStore? imagePendingStore,
  }) : _audio = audioPort ?? NativeFriendAudioPort(),
       _ownsAudio = audioPort == null,
       _audioStore = audioPendingStore ?? SecureFriendAudioPendingStore(),
       _textStore = messagePendingStore ?? SecureHumanMessagePendingStore() {
    _identity = _actual;
    _changes = Listenable.merge([auth, workspaceChanges]);
    _changes.addListener(_changed);
    _source = ConnectionSource(
      authorizationHeader: () => current ? _identity.$1 : null,
      apiBaseUrl: apiBaseUrl,
      client: client,
      identityChanges: _changes,
    );
    _wire = FriendAudioSource(
      authorizationHeader: () => current ? _identity.$1 : null,
      environment: _source.environment,
      client: client,
    );
    _imageBinding = imageSourceBinding?.call();
    _imageStore = imagePendingStore ?? SecureFriendImagePendingStore();
    imageDraft = LocalImageDraftController(
      isCurrent: () => imageEnabled && current,
      scope: () => (_identity, _imageEpoch, imageSourceBinding?.call()),
      picker: imagePicker,
    );
    imageDraft.addListener(_notify);
    _imageWire = FriendImageSource(
      client: _source.followClient,
      environment: _source.environment,
      authorizationHeader: () => current ? _identity.$1 : null,
      current: () =>
          imageViewCurrent(_imageEpoch) && (isVisible?.call() ?? true),
      binding: () => (_identity, _imageEpoch, imageSourceBinding?.call()),
      enabled: imageEnabled,
    );
    WidgetsBinding.instance.addObserver(this);
  }
  final BirdtieAuthController auth;
  final HumanConversation conversation;
  final Listenable? workspaceChanges;
  final String? Function()? workspaceID;
  final void Function(String expectedAuthorization)? onUnauthorized;

  /// The host verifies the exact recipient still occupies the visible thread.
  /// A cached or background controller must never consume unread messages.
  final bool Function()? isVisible;
  // Enabled only with the matched image API/migration rollout; API03 is not capable.
  final bool imageEnabled;
  final Object? Function()? imageSourceBinding;
  late final LocalImageDraftController imageDraft;
  late final FriendImageSource _imageWire;
  late final FriendImagePendingStore _imageStore;
  Object? _imageBinding;
  (int, FriendImageDraft)? _imageSnapshot;
  int _imageEpoch = 0;
  int get imageEpoch => _imageEpoch;
  final ValueNotifier<int> imageViewport = ValueNotifier(0);
  final Map<Object, FriendReceivedImage> _images = {};
  final Map<Object, Future<FriendReceivedImage?>> _imageReads = {};
  List<PendingFriendImage> _imagePending = [];
  List<PendingFriendImage> get imagePending =>
      current ? List.unmodifiable(_imagePending) : const [];
  final FriendAudioPort _audio;
  final bool _ownsAudio;
  final FriendAudioPendingStore _audioStore;
  final HumanMessagePendingStore _textStore;
  late final ConnectionSource _source;
  late final FriendAudioSource _wire;
  late final Listenable _changes;
  late (String?, String?, String?) _identity;
  (String?, String?, String?) get _actual =>
      (auth.authorizationHeader, auth.accountID, workspaceID?.call());
  bool _closed = false, _retired = false, _ready = false;
  int _serial = 0, _sequence = 0;
  int _historyEpoch = 0;
  String? _nextCursor;
  bool loadingEarlier = false;
  bool get hasEarlierMessages => historyAvailable && _nextCursor != null;
  String? _acknowledgedThrough;
  (int, int, String)? _readInFlight;
  String? _recordID, _playID;
  Timer? _ticker;
  DateTime? _started;
  bool loading = false, sending = false;
  String? error;
  List<HumanMessage> _messages = [];
  List<HumanMessage> get messages => List.unmodifiable(_messages);

  /// A current authorised history read, independent of local audio actions.
  bool get historyAvailable => current && _ready;
  List<PendingFriendAudio> _audioPending = [];
  List<PendingFriendAudio> get audioPending =>
      current ? List.unmodifiable(_audioPending) : const [];
  List<PendingHumanMessage> _textPending = [];
  List<PendingHumanMessage> get textPending =>
      current ? List.unmodifiable(_textPending) : const [];
  FriendAudioDraft? _audioDraft;
  FriendAudioDraft? get audioDraft => current ? _audioDraft : null;
  FriendAudioPhase audioPhase = FriendAudioPhase.idle;
  Duration elapsed = Duration.zero;
  String? playingMessageID;
  bool get audioSupported => _audio.supported;
  String get elapsedLabel =>
      '${elapsed.inMinutes}:${(elapsed.inSeconds % 60).toString().padLeft(2, '0')}';
  bool get canRecord =>
      current &&
      _ready &&
      audioSupported &&
      !sending &&
      !audioActive &&
      audioDraft == null &&
      imageDraft.localBytes == null &&
      !imageDraft.busy &&
      !hasPendingTextOrAudio;
  bool get recording => audioPhase == FriendAudioPhase.recording;
  bool get audioActive => const {
    FriendAudioPhase.permission,
    FriendAudioPhase.recording,
    FriendAudioPhase.processing,
  }.contains(audioPhase);
  bool get current =>
      !_closed &&
      !_retired &&
      _identity == _actual &&
      _identity.$1 != null &&
      _identity.$2 != null &&
      _identity.$3 == null &&
      chatEntityUUID.hasMatch(_identity.$2!) &&
      chatEntityUUID.hasMatch(conversation.id) &&
      chatEntityUUID.hasMatch(conversation.otherAccountId);
  bool get canSendText =>
      current &&
      _ready &&
      !sending &&
      !hasPendingTextOrAudio &&
      !audioActive &&
      audioDraft == null &&
      imageDraft.localBytes == null &&
      !imageDraft.busy;
  bool get canSendAudio =>
      current &&
      _ready &&
      !sending &&
      !audioActive &&
      audioDraft != null &&
      !hasPendingTextOrAudio &&
      imageDraft.localBytes == null &&
      !imageDraft.busy;
  // An unknown image keeps its own journal and blocks another image. With no
  // local image, a new explicit text/voice action is independent of that effect.
  bool get hasPendingTextOrAudio =>
      _textPending.isNotEmpty || _audioPending.isNotEmpty;
  bool get pending => hasPendingTextOrAudio || _imagePending.isNotEmpty;
  FriendAudioDraft? retainAudioDraft() =>
      current ||
          (!_closed &&
              !_retired &&
              _actual.$3 == null &&
              auth.rejectedSessionOwnerID == _identity.$2)
      ? _audioDraft
      : null;
  bool restoreAudioDraft(
    FriendAudioDraft draft, {
    required String owner,
    required String conversationID,
  }) {
    if (!current ||
        sending ||
        audioActive ||
        hasPendingTextOrAudio ||
        imageDraft.localBytes != null ||
        imageDraft.busy ||
        owner != _identity.$2 ||
        conversationID != conversation.id)
      return false;
    _audioDraft = FriendAudioDraft(draft.bytes, draft.metadata);
    audioPhase = FriendAudioPhase.preview;
    elapsed = Duration(milliseconds: draft.durationMillis);
    _notify();
    return true;
  }

  bool _valid(int serial) => current && serial == _serial;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void _changed() {
    final next = _actual;
    if (_closed || _retired) return;
    final binding = imageSourceBinding?.call();
    if (binding != _imageBinding) {
      retireImageView();
      _retired = true;
      _ready = false;
      _messages = [];
      imageDraft.discard();
      error = '图片来源已变化，请重新打开这段对话。';
      _notify();
      return;
    }
    if (next == _identity) return;
    retireImageView();
    final previousOwner = _identity.$2;
    ++_serial;
    ++_historyEpoch;
    _acknowledgedThrough = null;
    _cancelRecording();
    _stopPlayback();
    loading = false;
    loadingEarlier = false;
    _nextCursor = null;
    sending = false;
    _ready = false;
    _messages = [];
    // Preserve local same-owner drafts through reauthentication. New identity
    // never receives the old account's audio, history or recovery journal.
    final sameOwnerSuspended =
        next.$1 == null &&
        next.$2 == null &&
        auth.rejectedSessionOwnerID == previousOwner;
    if (next.$2 != null && next.$2 != previousOwner ||
        next.$3 != null ||
        next.$2 == null && !sameOwnerSuspended) {
      _retired = true;
      _audioDraft = null;
      _audioPending = [];
      _textPending = [];
      _imagePending = [];
      imageDraft.discard();
    } else if (next.$2 == previousOwner && next.$1 != null) {
      _identity = next;
      audioPhase = _audioDraft == null
          ? FriendAudioPhase.idle
          : FriendAudioPhase.preview;
      unawaited(initialise());
    }
    error = current ? null : '请重新登录后继续，草稿未发送。';
    _notify();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      // The explicit native gallery owns a temporary lifecycle lease. Its result
      // still checks the host gesture, recipient, epoch and resumed state.
      if (!imageDraft.busy || state == AppLifecycleState.detached) {
        retireImageView();
      }
      // A permission dialog may move Flutter to inactive/paused. No microphone is
      // running yet; native onStop/current-owner checks retire real backgrounding.
      if (audioPhase == FriendAudioPhase.permission &&
          state != AppLifecycleState.detached)
        return;
      if (audioActive) {
        ++_sequence;
        _cancelRecording();
        error = '录音已取消，未发送。';
      }
      _stopPlayback();
      _notify();
    }
  }

  void _failed(Object e, int serial) {
    if (!_valid(serial)) return;
    if (e is FriendImageDeliveryFailure && e.status == 401 ||
        e is FriendAudioDeliveryFailure && e.status == 401 ||
        e is ConnectionTransportFailure && e.status == 401) {
      final token = _identity.$1;
      if (token != null) onUnauthorized?.call(token);
    }
    if (!_valid(serial)) return;
    final status = _failureStatus(e);
    if (status == 401 || status == 403 || status == 404) {
      retireImageView();
      _messages = [];
      _ready = false;
      _nextCursor = null;
      loadingEarlier = false;
      ++_historyEpoch;
      _acknowledgedThrough = null;
    }
    error = switch (status) {
      401 => '登录已失效，请重新登录，草稿未发送。',
      403 => '当前身份没有对话访问权限。',
      404 => '这段对话已不可访问，请返回联系人核对。',
      409 => '消息条件已变化，请核实原操作后继续。',
      429 => '操作过于频繁，请稍后重试。',
      503 => '消息服务暂不可用，请稍后重试。',
      _ => e is FormatException ? '消息来源无法核实，请重新读取。' : '连接中断，请检查网络后重试。',
    };
  }

  int? _failureStatus(Object e) => e is FriendImageDeliveryFailure
      ? e.status
      : e is FriendAudioDeliveryFailure
      ? e.status
      : e is ConnectionTransportFailure
      ? e.status
      : null;

  bool _definiteDenial(Object e) =>
      const {400, 401, 403, 404, 409, 429}.contains(_failureStatus(e));

  Future<void> initialise() async {
    if (!current) return;
    final serial = ++_serial;
    final historyEpoch = ++_historyEpoch;
    loading = true;
    loadingEarlier = false;
    _ready = false;
    _nextCursor = null;
    error = null;
    _notify();
    final owner = _identity.$2!;
    try {
      final ap = await _audioStore.read(_wire.environment, owner);
      if (!_valid(serial)) return;
      final tp = await _textStore.read(_wire.environment, owner);
      if (!_valid(serial)) return;
      _audioPending = ap
          .where((p) => p.conversationID == conversation.id)
          .toList();
      _textPending = tp
          .where((p) => p.conversationID == conversation.id)
          .toList();
      if (imageEnabled) {
        final ip = await _imageStore.read(_source.environment, owner);
        if (!_valid(serial)) return;
        _imagePending = ip
            .where(
              (p) =>
                  p.conversationID == conversation.id &&
                  p.recipientID == conversation.otherAccountId,
            )
            .toList();
      }
      final page = await _source.messagesPage(conversation.id);
      if (!_valid(serial) || historyEpoch != _historyEpoch) return;
      _messages = page.items;
      _nextCursor = page.nextCursor;
      _ready = true;
    } catch (e) {
      _failed(e, serial);
    } finally {
      if (_valid(serial)) {
        loading = false;
        _notify();
        _scheduleReadAcknowledgement(serial, historyEpoch);
      }
    }
  }

  Future<void> load() async {
    if (!current || loading || sending) return;
    if (!_ready) {
      await initialise();
      return;
    }
    final serial = _serial;
    final historyEpoch = ++_historyEpoch;
    loading = true;
    loadingEarlier = false;
    error = null;
    _notify();
    try {
      final page = await _source.messagesPage(conversation.id);
      if (!_valid(serial) || historyEpoch != _historyEpoch) return;
      _messages = page.items;
      _nextCursor = page.nextCursor;
    } catch (e) {
      _failed(e, serial);
    } finally {
      if (_valid(serial)) {
        loading = false;
        _notify();
        _scheduleReadAcknowledgement(serial, historyEpoch);
      }
    }
  }

  /// A user requests earlier history; it is never eagerly fetched or marked read.
  Future<void> loadEarlier() async {
    final cursor = _nextCursor;
    if (!current ||
        !_ready ||
        cursor == null ||
        loading ||
        loadingEarlier ||
        sending)
      return;
    final serial = _serial, historyEpoch = _historyEpoch;
    loadingEarlier = true;
    error = null;
    _notify();
    bool stillCurrent() =>
        _valid(serial) &&
        historyEpoch == _historyEpoch &&
        _ready &&
        _nextCursor == cursor;
    try {
      final page = await _source.messagesPage(conversation.id, cursor: cursor);
      if (!stillCurrent()) return;
      // The server page must be strictly before the oldest retained item. Do
      // not merge an overlapping or out-of-order response into private history.
      if (page.items.isNotEmpty && _messages.isNotEmpty) {
        final last = page.items.last, first = _messages.first;
        if (last.createdAt.isAfter(first.createdAt) ||
            last.createdAt.isAtSameMomentAs(first.createdAt) &&
                last.id.compareTo(first.id) >= 0 ||
            page.items.any((m) => _messages.any((old) => old.id == m.id))) {
          throw const FormatException('更早消息顺序无法核实');
        }
      }
      _messages = [...page.items, ..._messages];
      _nextCursor = page.nextCursor;
    } catch (e) {
      if (stillCurrent()) _failed(e, serial);
    } finally {
      if (_valid(serial) && historyEpoch == _historyEpoch) {
        loadingEarlier = false;
        _notify();
      } else if (_valid(serial) && !loadingEarlier) {
        // A denial has already retired the pending page and all visible rows.
        _notify();
      }
    }
  }

  void _scheduleReadAcknowledgement(int serial, int historyEpoch) {
    if (!_ready || isVisible?.call() != true) return;
    // Notify first and wait for the thread's frame, so a successful fetch alone
    // cannot mark messages read before the user can see the authorised history.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_valid(serial) && historyEpoch == _historyEpoch) {
        unawaited(acknowledgeVisible());
      }
    });
  }

  bool _readCurrent(int serial, int historyEpoch, String through) =>
      _valid(serial) &&
      historyEpoch == _historyEpoch &&
      _ready &&
      !loading &&
      isVisible?.call() == true &&
      _messages.isNotEmpty &&
      _messages.last.id == through;

  /// Acknowledges only this exact visible conversation, using the server's last
  /// authorised history item. Notification read state is independent.
  Future<void> acknowledgeVisible() async {
    if (!current ||
        !_ready ||
        loading ||
        isVisible?.call() != true ||
        _messages.isEmpty)
      return;
    final serial = _serial,
        historyEpoch = _historyEpoch,
        through = _messages.last.id,
        attempt = (serial, historyEpoch, _messages.last.id);
    if (!chatEntityUUID.hasMatch(through) ||
        _acknowledgedThrough == through ||
        _readInFlight == attempt)
      return;
    _readInFlight = attempt;
    try {
      if (!_readCurrent(serial, historyEpoch, through)) return;
      await _source.markRead(conversation.id, through);
      if (_readCurrent(serial, historyEpoch, through)) {
        _acknowledgedThrough = through;
      }
    } catch (e) {
      if (_readCurrent(serial, historyEpoch, through)) {
        _failed(e, serial);
        _notify();
      }
    } finally {
      if (_readInFlight == attempt) _readInFlight = null;
    }
  }

  bool get canSelectImage =>
      imageViewCurrent(_imageEpoch) &&
      (isVisible?.call() ?? true) &&
      !sending &&
      !pending &&
      !audioActive &&
      audioDraft == null &&
      imageDraft.canSelect;

  bool canSendImage(String caption) =>
      imageViewCurrent(_imageEpoch) &&
      (isVisible?.call() ?? true) &&
      !sending &&
      !pending &&
      !audioActive &&
      audioDraft == null &&
      imageDraft.hasPixels &&
      friendImageCaption(caption);

  FriendImageDraft? _frozenImage() {
    final bytes = imageDraft.localBytes;
    if (bytes == null || !imageDraft.hasPixels) {
      _imageSnapshot = null;
      return null;
    }
    if (_imageSnapshot?.$1 == imageDraft.revision) return _imageSnapshot!.$2;
    final draft = FriendImageDraft(
      bytes,
      width: imageDraft.width!,
      height: imageDraft.height!,
      pixelRisk: imageDraft.masked ? 'USER_MASKED' : 'UNKNOWN',
    );
    _imageSnapshot = (imageDraft.revision, draft);
    return draft;
  }

  bool canSelectImageSource(MomentImageSource source) =>
      canSelectImage && imageDraft.supportsSource(source);
  Future<void> selectImage({
    required bool Function() gestureCurrent,
    MomentImageSource source = MomentImageSource.gallery,
  }) async {
    if (!canSelectImage || !gestureCurrent()) return;
    await imageDraft.select(
      source: source,
      gestureCurrent: () =>
          current &&
          (isVisible?.call() ?? true) &&
          !audioActive &&
          audioDraft == null &&
          WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed &&
          gestureCurrent(),
    );
  }

  LocalImageRetainedDraft? retainImageDraft() =>
      current ||
          (!_closed &&
              !_retired &&
              _actual.$3 == null &&
              auth.rejectedSessionOwnerID == _identity.$2)
      ? imageDraft.retain()
      : null;

  bool restoreImageDraft(
    LocalImageRetainedDraft retained, {
    required String owner,
    required String conversationID,
    required String recipient,
  }) =>
      imageEnabled &&
      current &&
      _ready &&
      !sending &&
      !audioActive &&
      audioDraft == null &&
      _audioPending.isEmpty &&
      _textPending.isEmpty &&
      owner == _identity.$2 &&
      conversationID == conversation.id &&
      recipient == conversation.otherAccountId &&
      imageDraft.restore(retained);

  /// Invalidates A-B-A, provider and route callbacks; original local pixels stay
  /// only in this immutable recipient's private controller until owner retirement.
  void retireImageView() {
    ++_imageEpoch;
    _imageWire.cancelRequests();
    imageDraft.retireSelection();
    for (final value in _images.values) {
      value.dispose();
    }
    _images.clear();
    _imageReads.clear();
  }

  void imageViewportChanged() {
    if (!_closed) ++imageViewport.value;
  }

  bool imageViewCurrent(int epoch) =>
      imageEnabled &&
      current &&
      _ready &&
      epoch == _imageEpoch &&
      imageSourceBinding?.call() == _imageBinding;

  Future<FriendReceivedImage?> receiveImage(
    HumanMessage message,
    FriendImageVariant variant,
  ) async {
    final metadata = message.image, epoch = _imageEpoch, serial = _serial;
    bool valid() =>
        imageViewCurrent(epoch) &&
        (isVisible?.call() ?? true) &&
        _valid(serial) &&
        metadata != null &&
        _messages.any(
          (m) => m.id == message.id && m.image?.same(metadata) == true,
        );
    if (!valid()) return null;
    final derivative = variant == FriendImageVariant.thumbnail
        ? metadata!.thumbnail
        : metadata!.full;
    final key = (
      _source.environment,
      _identity,
      epoch,
      conversation.id,
      message.id,
      variant,
      derivative.sha256,
    );
    final cached = _images.remove(key);
    if (cached != null) {
      _images[key] = cached;
      return cached;
    }
    final inFlight = _imageReads[key];
    if (inFlight != null) return inFlight;
    Future<FriendReceivedImage?> fetch() async {
      Uint8List? bytes;
      try {
        bytes = await _imageWire.content(
          conversation: conversation.id,
          message: message.id,
          metadata: metadata,
          variant: variant,
        );
        if (!valid()) return null;
        if (variant == FriendImageVariant.full) {
          final oldFull = _images.keys
              .where((k) => _images[k]!.variant == FriendImageVariant.full)
              .toList();
          for (final k in oldFull) {
            _images.remove(k)?.dispose();
          }
        } else {
          int count() => _images.values
              .where((v) => v.variant == FriendImageVariant.thumbnail)
              .length;
          int size() => _images.values
              .where((v) => v.variant == FriendImageVariant.thumbnail)
              .fold(0, (sum, v) => sum + v.byteSize);
          while (count() >= 8 || size() + bytes.length > 8 * 1024 * 1024) {
            final oldest = _images.keys.firstWhere(
              (k) => _images[k]!.variant == FriendImageVariant.thumbnail,
            );
            _images.remove(oldest)?.dispose();
          }
        }
        final result = FriendReceivedImage(bytes, variant, epoch);
        bytes = null;
        _images[key] = result;
        return result;
      } catch (e) {
        if (valid()) {
          _failed(e, serial);
          _notify();
        }
        return null;
      } finally {
        if (bytes != null) bytes.fillRange(0, bytes.length, 0);
        _imageReads.remove(key);
      }
    }

    final future = fetch();
    _imageReads[key] = future;
    return future;
  }

  bool canRetryImage(PendingFriendImage p, String caption) {
    final draft = _frozenImage();
    return imageEnabled &&
        current &&
        _ready &&
        !sending &&
        !audioActive &&
        audioDraft == null &&
        (isVisible?.call() ?? true) &&
        _imagePending.contains(p) &&
        draft != null &&
        p.matches(draft, caption);
  }

  Future<bool> sendImage(
    String caption, {
    required bool Function() draftCurrent,
  }) async {
    if (!canSendImage(caption) || !draftCurrent()) return false;
    final draft = _frozenImage()!, revision = imageDraft.revision;
    final pending = PendingFriendImage.review(
      operationID: newEntityShareOperationID(),
      conversationID: conversation.id,
      recipientID: conversation.otherAccountId,
      draft: draft,
      caption: caption,
    );
    return _imageOperation(
      pending,
      draft: draft,
      caption: caption,
      revision: revision,
      draftCurrent: draftCurrent,
    );
  }

  Future<bool> recoverImage(
    PendingFriendImage pending, {
    String? caption,
    bool Function()? draftCurrent,
  }) async {
    if (!imageEnabled ||
        !current ||
        !_ready ||
        sending ||
        audioActive ||
        !(isVisible?.call() ?? true) ||
        !_imagePending.contains(pending))
      return false;
    final draft = _frozenImage();
    final matches =
        draft != null && caption != null && pending.matches(draft, caption);
    return _imageOperation(
      pending,
      draft: matches ? draft : null,
      caption: matches ? caption : null,
      revision: imageDraft.revision,
      draftCurrent: draftCurrent,
      recoverOnly: true,
    );
  }

  /// The retry button is a new approval gesture for the same original input.
  /// A fresh conversation read precedes the original-operation GET; 404 alone
  /// never clears the journal and never schedules an automatic replay.
  Future<bool> retryImage(
    PendingFriendImage pending,
    String caption, {
    required bool Function() draftCurrent,
  }) async {
    if (!canRetryImage(pending, caption) || !draftCurrent()) return false;
    return _imageOperation(
      pending,
      draft: _frozenImage()!,
      caption: caption,
      revision: imageDraft.revision,
      draftCurrent: draftCurrent,
      retry: true,
    );
  }

  Future<bool> _imageOperation(
    PendingFriendImage p, {
    FriendImageDraft? draft,
    String? caption,
    int? revision,
    bool Function()? draftCurrent,
    bool retry = false,
    bool recoverOnly = false,
  }) async {
    final serial = _serial, epoch = _imageEpoch, owner = _identity.$2!;
    bool valid() =>
        _valid(serial) &&
        imageViewCurrent(epoch) &&
        (isVisible?.call() ?? true);
    bool approved() =>
        valid() &&
        draft != null &&
        caption != null &&
        imageDraft.revision == revision &&
        draftCurrent?.call() == true &&
        p.matches(draft, caption);
    var verified = false;
    sending = true;
    error = null;
    _notify();
    try {
      if (draft != null && !retry && !recoverOnly) {
        await _imageStore.write(_source.environment, owner, p);
        if (!valid()) return false;
        _imagePending = [..._imagePending, p];
        if (!approved()) return false;
        await _imageWire.send(owner, p, draft, caption!);
      } else {
        if (retry) {
          // Recheck immutable participants using the current session, rather
          // than treating an operation404 as conversation authorization.
          final fresh = await _source.conversation(conversation.id);
          if (!approved() ||
              fresh.id != conversation.id ||
              fresh.otherAccountId != conversation.otherAccountId)
            return false;
        }
        try {
          await _imageWire.recover(owner, p);
        } on FriendImageDeliveryFailure catch (e) {
          if (!retry || e.status != 404) rethrow;
          if (!approved()) return false;
          await _imageWire.send(owner, p, draft!, caption!);
        }
      }
      if (!valid()) return false;
      await _imageStore.delete(_source.environment, owner, p);
      if (!valid()) return false;
      _imagePending.removeWhere((v) => v.operationID == p.operationID);
      if (approved()) imageDraft.discard();
      verified = true;
      return true;
    } catch (e) {
      if (valid()) {
        if (e is FriendImageDeliveryFailure && e.status == 404) {
          error = '原图片操作尚未查到，结果仍未确认；未自动重发。';
        } else {
          _failed(e, serial);
          if (valid() && !_definiteDenial(e)) {
            error = '图片发送结果未确认，请保留记录并核实原操作。';
          }
        }
      }
      return false;
    } finally {
      if (_valid(serial)) {
        sending = false;
        _notify();
        if (verified) unawaited(load());
      }
    }
  }

  Future<bool> sendText(String value) async {
    final body = value.trim(), serial = _serial;
    if (!canSendText || body.isEmpty) return false;
    if (utf8.encode(body).length > 2000 || body.contains('\u0000')) {
      error = '消息过长，请缩短后发送。';
      _notify();
      return false;
    }
    final owner = _identity.$2!,
        p = PendingHumanMessage(
          operationID: newEntityShareOperationID(),
          conversationID: conversation.id,
          payloadDigest: humanMessagePayloadDigest(conversation.id, body),
        );
    sending = true;
    error = null;
    _notify();
    try {
      await _textStore.write(_wire.environment, owner, p);
      if (!_valid(serial)) return false;
      _textPending = [..._textPending, p];
      await _source.sendMessageOperation(owner, p, body);
      if (!_valid(serial)) return false;
      await _textStore.delete(_wire.environment, owner, p);
      if (!_valid(serial)) return false;
      _textPending.removeWhere((v) => v.operationID == p.operationID);
      return true;
    } catch (e) {
      _failed(e, serial);
      if (_valid(serial)) {
        if (_textPending.isEmpty) _ready = false;
        if (!_definiteDenial(e)) error = '发送结果未确认，请核实原操作。';
      }
      return false;
    } finally {
      if (_valid(serial)) {
        sending = false;
        _notify();
        unawaited(load());
      }
    }
  }

  Future<bool> recoverText(PendingHumanMessage p) async {
    final serial = _serial;
    if (!current || sending || !_textPending.contains(p)) return false;
    final owner = _identity.$2!;
    sending = true;
    _notify();
    try {
      await _source.readMessageOperation(owner, p);
      if (!_valid(serial)) return false;
      await _textStore.delete(_wire.environment, owner, p);
      if (!_valid(serial)) return false;
      _textPending.removeWhere((v) => v.operationID == p.operationID);
      error = null;
      return true;
    } catch (e) {
      _failed(e, serial);
      return false;
    } finally {
      if (_valid(serial)) {
        sending = false;
        _notify();
        unawaited(load());
      }
    }
  }

  Future<void> startRecording() async {
    if (!current ||
        !_ready ||
        sending ||
        hasPendingTextOrAudio ||
        audioActive ||
        audioDraft != null ||
        imageDraft.localBytes != null ||
        imageDraft.busy)
      return;
    if (!audioSupported) {
      error = '这台设备暂不支持语音消息，请输入文字。';
      _notify();
      return;
    }
    _stopPlayback();
    final serial = _serial,
        seq = ++_sequence,
        id = 'friend-record-${DateTime.now().microsecondsSinceEpoch}-$seq';
    _recordID = id;
    audioPhase = FriendAudioPhase.permission;
    error = null;
    elapsed = Duration.zero;
    _notify();
    try {
      await _audio.start(id, (phase) {
        if (!_valid(serial) || _recordID != id || seq != _sequence) return;
        audioPhase = phase;
        if (phase == FriendAudioPhase.idle) {
          _cancelRecording();
          error = '录音已中断，未发送。';
        }
        _notify();
      });
      if (!_valid(serial) || _recordID != id || seq != _sequence) {
        await _audio.cancel(id);
        return;
      }
      _audioDraft = null;
      audioPhase = FriendAudioPhase.recording;
      _started = DateTime.now();
      _ticker = Timer.periodic(const Duration(milliseconds: 200), (_) {
        if (!_valid(serial) || _recordID != id || !recording) {
          _ticker?.cancel();
          return;
        }
        elapsed = DateTime.now().difference(_started!);
        _notify();
        if (elapsed >= const Duration(seconds: 89)) unawaited(stopRecording());
      });
      _notify();
    } catch (_) {
      if (_valid(serial) && _recordID == id) {
        await _audio.cancel(id);
        if (!_valid(serial)) return;
        _recordID = null;
        audioPhase = audioDraft == null
            ? FriendAudioPhase.idle
            : FriendAudioPhase.preview;
        error = '无法开始录音，请检查麦克风权限后重试。';
        _notify();
      }
    }
  }

  Future<void> stopRecording() async {
    final id = _recordID, serial = _serial, seq = _sequence;
    if (id == null || !current || !recording) return;
    _ticker?.cancel();
    _ticker = null;
    audioPhase = FriendAudioPhase.processing;
    _notify();
    try {
      final draft = await _audio.stop(id);
      if (!_valid(serial) || seq != _sequence || _recordID != id) return;
      _audioDraft = draft;
      _recordID = null;
      elapsed = Duration(milliseconds: draft.durationMillis);
      audioPhase = FriendAudioPhase.preview;
      error = null;
    } catch (_) {
      if (_valid(serial) && _recordID == id) {
        await _audio.cancel(id);
        if (!_valid(serial)) return;
        _recordID = null;
        audioPhase = FriendAudioPhase.idle;
        error = '录音没有完成，请重新录制。';
      }
    } finally {
      if (_valid(serial)) _notify();
    }
  }

  void _cancelRecording() {
    final id = _recordID;
    _recordID = null;
    _ticker?.cancel();
    _ticker = null;
    _started = null;
    audioPhase = audioDraft == null
        ? FriendAudioPhase.idle
        : FriendAudioPhase.preview;
    if (id != null) unawaited(_audio.cancel(id));
  }

  void cancelAudio() {
    if (_closed) return;
    ++_sequence;
    _cancelRecording();
    _stopPlayback();
    _audioDraft = null;
    elapsed = Duration.zero;
    audioPhase = FriendAudioPhase.idle;
    // Cancelling a local draft does not recover a failed history read.
    _notify();
  }

  void _stopPlayback() {
    final id = _playID;
    _playID = null;
    playingMessageID = null;
    if (id != null) unawaited(_audio.stopPlayback(id));
    if (audioPhase == FriendAudioPhase.playing)
      audioPhase = audioDraft == null
          ? FriendAudioPhase.idle
          : FriendAudioPhase.preview;
  }

  Future<void> _play(FriendAudioDraft d, {String? message}) async {
    final serial = _serial;
    if (!current || audioActive) return;
    _stopPlayback();
    final id =
        'friend-play-${DateTime.now().microsecondsSinceEpoch}-${++_sequence}';
    _playID = id;
    playingMessageID = message;
    audioPhase = FriendAudioPhase.playing;
    _notify();
    try {
      await _audio.play(id, d, () {
        if (_valid(serial) && _playID == id) {
          _playID = null;
          playingMessageID = null;
          audioPhase = audioDraft == null
              ? FriendAudioPhase.idle
              : FriendAudioPhase.preview;
          _notify();
        }
      });
    } catch (_) {
      if (_valid(serial) && _playID == id) {
        _stopPlayback();
        error = '语音暂时无法播放，请重试。';
        _notify();
      }
    }
  }

  Future<void> playDraft() async {
    final d = audioDraft;
    if (d != null) await _play(d);
  }

  Future<void> playMessage(HumanMessage message) async {
    final serial = _serial, meta = message.audio;
    if (!current || meta == null || !_messages.any((m) => m.id == message.id))
      return;
    try {
      final d = await _wire.content(conversation.id, message.id, meta);
      if (!_valid(serial)) return;
      await _play(d, message: message.id);
    } catch (e) {
      _failed(e, serial);
      _notify();
    }
  }

  Future<bool> sendAudio() async {
    if (!canSendAudio) return false;
    final d = audioDraft!,
        serial = _serial,
        owner = _identity.$2!,
        p = PendingFriendAudio(
          operationID: newEntityShareOperationID(),
          conversationID: conversation.id,
          metadata: d.metadata,
        );
    _stopPlayback();
    sending = true;
    error = null;
    _notify();
    try {
      await _audioStore.write(_wire.environment, owner, p);
      if (!_valid(serial)) return false;
      _audioPending = [..._audioPending, p];
      await _wire.send(owner, p, d);
      if (!_valid(serial)) return false;
      await _audioStore.delete(_wire.environment, owner, p);
      if (!_valid(serial)) return false;
      _audioPending.removeWhere((v) => v.operationID == p.operationID);
      if (identical(audioDraft, d)) {
        _audioDraft = null;
        audioPhase = FriendAudioPhase.idle;
        elapsed = Duration.zero;
      }
      return true;
    } catch (e) {
      _failed(e, serial);
      if (_valid(serial)) {
        if (_audioPending.isEmpty) _ready = false;
        if (!_definiteDenial(e)) error = '语音发送结果未确认，请先核实这次发送。';
      }
      return false;
    } finally {
      if (_valid(serial)) {
        sending = false;
        _notify();
        unawaited(load());
      }
    }
  }

  Future<bool> recoverAudio([PendingFriendAudio? original]) async {
    final p =
            original ??
            (_audioPending.length == 1 ? _audioPending.single : null),
        serial = _serial;
    if (!current || sending || p == null || !_audioPending.contains(p))
      return false;
    final owner = _identity.$2!;
    sending = true;
    error = null;
    _notify();
    try {
      await _wire.recover(owner, p);
      if (!_valid(serial)) return false;
      await _audioStore.delete(_wire.environment, owner, p);
      if (!_valid(serial)) return false;
      _audioPending.removeWhere((v) => v.operationID == p.operationID);
      if (audioDraft?.metadata.same(p.metadata) == true) {
        _audioDraft = null;
        audioPhase = FriendAudioPhase.idle;
      }
      return true;
    } catch (e) {
      _failed(e, serial);
      if (_valid(serial)) {
        if (e is FriendAudioDeliveryFailure && e.status == 404) {
          error = '原操作尚未查到。可以核实后重试同一次语音，未自动重发。';
        } else if (!_definiteDenial(e)) {
          error = '尚无法核实原语音，请保留这次记录。';
        }
      }
      return false;
    } finally {
      if (_valid(serial)) {
        sending = false;
        _notify();
        unawaited(load());
      }
    }
  }

  /// Explicit retry only after a current GET says no receipt; always the original
  /// operation/address/content. Restart without bytes permits recovery only.
  Future<bool> retryAudio(PendingFriendAudio p) async {
    final d = audioDraft, serial = _serial;
    if (!current ||
        sending ||
        d == null ||
        !_audioPending.contains(p) ||
        !p.metadata.same(d.metadata))
      return false;
    final owner = _identity.$2!;
    sending = true;
    _notify();
    try {
      try {
        await _wire.recover(owner, p);
      } on FriendAudioDeliveryFailure catch (e) {
        if (e.status != 404) rethrow;
        if (!_valid(serial)) return false;
        await _wire.send(owner, p, d);
      }
      if (!_valid(serial)) return false;
      await _audioStore.delete(_wire.environment, owner, p);
      if (!_valid(serial)) return false;
      _audioPending.removeWhere((v) => v.operationID == p.operationID);
      if (identical(audioDraft, d)) {
        _audioDraft = null;
        audioPhase = FriendAudioPhase.idle;
      }
      error = null;
      return true;
    } catch (e) {
      _failed(e, serial);
      return false;
    } finally {
      if (_valid(serial)) {
        sending = false;
        _notify();
        unawaited(load());
      }
    }
  }

  @override
  void dispose() {
    if (_closed) return;
    _closed = true;
    ++_serial;
    ++_sequence;
    _cancelRecording();
    _stopPlayback();
    _audioDraft = null;
    _messages = [];
    _changes.removeListener(_changed);
    WidgetsBinding.instance.removeObserver(this);
    retireImageView();
    imageDraft.removeListener(_notify);
    imageDraft.dispose();
    _imageSnapshot = null;
    imageViewport.dispose();
    _imageWire.dispose();
    _source.dispose();
    _wire.dispose();
    if (_ownsAudio) _audio.dispose();
    super.dispose();
  }
}

/// One bounded object cache entry. Owner retirement wipes this exact byte buffer
/// and evicts this provider only, including the resized providers used by its UI.
class FriendReceivedImage {
  FriendReceivedImage(this._bytes, this.variant, this.epoch)
    : _view = _bytes.asUnmodifiableView();
  final Uint8List _bytes;
  final Uint8List _view;
  final FriendImageVariant variant;
  final int epoch;
  bool _disposed = false;
  final Map<(int, int), ImageProvider> _providers = {};
  int get byteSize => _bytes.length;
  bool get current => !_disposed;
  ImageProvider provider({required int width, required int height}) {
    if (_disposed) throw StateError('图片视图已失效');
    final cached = _providers[(width, height)];
    if (cached != null) return cached;
    for (final old in _providers.values) {
      unawaited(old.evict());
    }
    _providers.clear();
    final provider = ResizeImage(
      MemoryImage(_view),
      width: width,
      height: height,
      policy: ResizeImagePolicy.fit,
      allowUpscaling: false,
    );
    _providers[(width, height)] = provider;
    return provider;
  }

  void dispose() {
    if (_disposed) return;
    _disposed = true;
    _bytes.fillRange(0, _bytes.length, 0);
    for (final provider in _providers.values) {
      unawaited(provider.evict());
    }
    _providers.clear();
  }
}
