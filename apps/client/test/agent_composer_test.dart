import 'package:birdtie_client/src/content/moment_image_picker.dart';
import 'package:birdtie_client/src/workspace/friend_image.dart';
import 'friend_image_test.dart' show imageFixtureBytes, imageMetadata, imageReceipt, imageWire;
import 'private_moment_media_controller_test.dart' show TestMomentImagePicker;
import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/chat_message_operation.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/friend_audio.dart';
import 'package:birdtie_client/src/workspace/friend_conversation_controller.dart';
import 'package:birdtie_client/src/workspace/voice_input.dart';
import 'package:birdtie_client/src/workspace/voice_input_consent.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'friend_audio_test.dart'
    show
        audioFixture,
        audioOwner,
        audioPeer,
        audioConversation,
        audioMessage,
        audioOperation;
import 'connection_chat_page_test.dart' show chatPageWire;

class _HumanAuth extends BirdtieAuthController {
  _HumanAuth()
    : super(client: MockClient((_) async => http.Response('{}', 404)));
  @override
  String get accountID => audioOwner;
  @override
  String get authorizationHeader => 'Bearer synthetic-a';
  @override
  bool get signedIn => true;
}

class _NoHumanAudio extends FriendAudioPort {
  @override
  bool get supported => false;
  @override
  Future<void> start(String id, ValueChanged<FriendAudioPhase> onPhase) async =>
      throw StateError('Native audio is outside this composer capability test');
  @override
  Future<FriendAudioDraft> stop(String id) async => audioFixture();
  @override
  Future<void> cancel(String id) async {}
  @override
  Future<void> play(
    String id,
    FriendAudioDraft draft,
    VoidCallback onFinished,
  ) async {}
  @override
  Future<void> stopPlayback(String id) async {}
  @override
  void dispose() {}
}

FriendConversationController _humanController(
  http.Client client, {
  MemoryHumanMessagePendingStore? pending,
  MemoryFriendImagePendingStore? imageStore,
  bool imageEnabled = false, MomentImagePicker? imagePicker,
}) {
  final auth = _HumanAuth();
  final c = FriendConversationController(
    auth: auth,
    conversation: const HumanConversation(
      id: audioConversation,
      otherAccountId: audioPeer,
      otherName: '测试联系人',
    ),
    apiBaseUrl: 'https://unit.example',
    client: client,
    audioPort: _NoHumanAudio(),
    audioPendingStore: MemoryFriendAudioPendingStore(),
    messagePendingStore: pending ?? MemoryHumanMessagePendingStore(),
    imageEnabled: imageEnabled, imagePicker: imagePicker,
    imagePendingStore: imageStore ?? MemoryFriendImagePendingStore(),
  );
  addTearDown(c.dispose);
  addTearDown(auth.dispose);
  addTearDown(client.close);
  return c;
}

Future<void> _paintHumanComposer(
  WidgetTester tester,
  FriendConversationController c,
  ValueChanged<String> onSubmit, {
  bool humanMode = true,
}) async {
  final workspace = AgentWorkspaceController();
  addTearDown(workspace.dispose);
  await tester.pumpWidget(
    MaterialApp(
      theme: birdtieTheme(Brightness.light),
      home: Scaffold(
        body: AgentComposer(
          workspace: workspace,
          humanMode: humanMode,
          friendConversation: c,
          recipientName: '测试联系人',
          draftIdentity: audioConversation,
          onSubmit: onSubmit,
          onSearchArea: () {},
          hasSearchArea: false,
          voiceInputPort: _FeedbackVoice(),
          voiceConsentStore: MemoryVoiceInputConsentStore(),
        ),
      ),
    ),
  );
  await tester.pump();
}

IconButton _sendButton(WidgetTester tester) => tester.widget<IconButton>(
  find.byKey(const Key('now-composer-primary-action')),
);

String _composerDraft(WidgetTester tester) =>
    tester.widget<TextField>(find.byType(TextField)).controller!.text;

class _PendingSource extends AgentTaskSource {
  final requests = <String, Completer<AgentResult>>{};

  @override
  Future<AgentResult> resolve(
    String query,
    List<PublicActivity> activities,
    List<PublicPlace> places,
  ) => (requests[query] = Completer<AgentResult>()).future;
}

const _voiceFailureMessage = '当前语音服务不可用，请稍后重试或使用键盘。';

class _FeedbackVoice extends VoiceInputPort {
  int captures = 0;

  @override
  Future<VoiceAvailability> availability() async =>
      const VoiceAvailability(onDevice: true);
  @override
  Future<bool> requestPermission(String requestId) async => true;
  @override
  Future<bool> localLanguageReady(String requestId, String language) async =>
      true;
  @override
  Future<String> recognize({
    required String requestId,
    required String language,
    required bool onDevice,
    required VoiceSystemService? systemService,
    required bool systemApproved,
    required ValueChanged<VoiceInputPhase> onPhase,
  }) async {
    captures++;
    throw const VoiceInputFailure(_voiceFailureMessage);
  }

  @override
  Future<void> stop(String requestId) async {}
  @override
  Future<void> cancel(String requestId) async {}
  @override
  void dispose() {}
}

void imageRecoveryComposerTests() {
  for (final useIme in [false, true]) {
    testWidgets(
      'image operation404 cannot intercept an independent composer ${useIme ? "IME" : "arrow"} send',
      (tester) async {
        tester.binding.handleAppLifecycleStateChanged(
          AppLifecycleState.resumed,
        );
        final bytes = (await tester.runAsync(imageFixtureBytes))!;
        final pending = PendingFriendImage.review(
          operationID: audioOperation,
          conversationID: audioConversation,
          recipientID: audioPeer,
          draft: FriendImageDraft(bytes, width: 4, height: 4),
          caption: '原图片说明',
        );
        final store = MemoryFriendImagePendingStore();
        await store.write('https://unit.example', audioOwner, pending);
        final posts = <http.Request>[];
        var imageReads = 0, found = false;
        final client = MockClient((r) async {
          if (r.method == 'POST') {
            posts.add(r);
            expect(
              r.url.path,
              '/v1/me/conversations/$audioConversation/message-operations',
            );
            final body = jsonDecode(r.body) as Map<String, dynamic>;
            return imageWire({
              'schemaVersion': humanMessageOperationSchema,
              'ownerId': audioOwner,
              'conversationId': audioConversation,
              'operationId': body['operationId'],
              'effectKey': humanMessageEffectKey(
                audioOwner,
                body['operationId'],
              ),
              'payloadDigest': humanMessagePayloadDigest(
                audioConversation,
                body['body'],
              ),
              'toolVersion': humanMessageToolVersion,
              'messageId': audioMessage,
              'createdAt': '2026-10-12T01:02:03Z',
            }, status: 201);
          }
          if (r.url.path.contains('/image-message-operations/')) {
            imageReads++;
            return found
                ? imageWire(
                    imageReceipt(
                      pending,
                      imageMetadata(bytes),
                      owner: audioOwner,
                    ),
                  )
                : imageWire({}, status: 404);
          }
          return chatPageWire([]);
        });
        final c = _humanController(
          client,
          imageEnabled: true,
          imageStore: store,
        );
        await tester.runAsync(() async {
          await c.initialise();
          expect(await c.recoverImage(c.imagePending.single), isFalse);
        });
        Future<bool?>? submission;
        await _paintHumanComposer(
          tester,
          c,
          // Only transport work leaves fakeAsync; taps, IME and pumps stay here.
          (text) => submission = tester.runAsync(() => c.sendText(text)),
        );
        await tester.enterText(find.byType(TextField), '独立的新文字');
        await tester.pump();
        expect(find.byType(TextField), findsOneWidget);
        expect(
          find.byKey(const Key('now-composer-image-thumbnail')),
          findsNothing,
        );
        expect(_sendButton(tester).onPressed, isNotNull);
        expect(posts, isEmpty);
        if (useIme) {
          await tester.testTextInput.receiveAction(TextInputAction.send);
        } else {
          await tester.tap(
            find.byKey(const Key('now-composer-primary-action')),
          );
        }
        await tester.pump();
        expect(submission, isNotNull);
        expect(await submission!, isTrue);
        await tester.pumpAndSettle();
        expect(posts, hasLength(1));
        expect(jsonDecode(posts.single.body)['body'], '独立的新文字');
        expect(
          jsonDecode(posts.single.body)['operationId'],
          isNot(pending.operationID),
        );
        expect(
          imageReads,
          1,
          reason:
              'A new explicit text send cannot replay or recover the image.',
        );
        expect(c.imagePending, hasLength(1));
        expect(
          await store.read('https://unit.example', audioOwner),
          hasLength(1),
        );
        expect(c.canSelectImage, isFalse);
        await tester.enterText(find.byType(TextField), '接着编辑，先不发送');
        found = true;
        expect(
          await tester.runAsync(() => c.recoverImage(
            c.imagePending.single,
            caption: '接着编辑，先不发送',
            draftCurrent: () => true,
          )),
          isTrue,
        );
        await tester.pumpAndSettle();
        expect(_composerDraft(tester), '接着编辑，先不发送');
        expect(c.imagePending, isEmpty);
        expect(await store.read('https://unit.example', audioOwner), isEmpty);
        expect(posts, hasLength(1));
        expect(imageReads, 2);
        expect(find.byType(TextField), findsOneWidget);
        await tester.pumpWidget(const SizedBox.shrink());
      },
    );
  }
}

void main() {
  imageRecoveryComposerTests();

  testWidgets('human image-only arrow and IME share dispatch; composition retains exact caption', (tester) async {
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    final client = MockClient((_) async => chatPageWire([]));
    final c = _humanController(client, imageEnabled: true,
        imagePicker: TestMomentImagePicker((await tester.runAsync(imageFixtureBytes))!));
    await c.initialise(); await tester.runAsync(() => c.selectImage(gestureCurrent: () => true));
    final submissions = <String>[];
    await _paintHumanComposer(tester, c, submissions.add);
    expect(find.byType(TextField), findsOneWidget);
    expect(find.byKey(const Key('now-composer-image-thumbnail')), findsOneWidget);
    expect(_sendButton(tester).onPressed, isNotNull);
    await tester.tap(find.byKey(const Key('now-composer-primary-action')));
    await tester.pump();
    expect(submissions, ['']);
    final field = tester.widget<TextField>(find.byType(TextField));
    field.controller!.value = const TextEditingValue(text: '  caption  ',
        selection: TextSelection.collapsed(offset: 11), composing: TextRange(start: 2, end: 9));
    await tester.pump();
    field.onSubmitted!('ignored');
    expect(submissions, ['']);
    expect(field.controller!.text, '  caption  ');
    field.controller!.value = field.controller!.value.copyWith(composing: TextRange.empty);
    await tester.pump();
    field.onSubmitted!('ignored');
    expect(submissions, ['', '  caption  ']);
    expect(find.byType(TextField), findsOneWidget);
  });

  testWidgets('human mode never grants Agent image authorization or exposes creation menu', (tester) async {
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    final client = MockClient((_) async => chatPageWire([]));
    final c = _humanController(client, imageEnabled: true,
        imagePicker: TestMomentImagePicker((await tester.runAsync(imageFixtureBytes))!));
    await c.initialise();
    final key = GlobalKey<AgentComposerState>(), workspace = AgentWorkspaceController();
    addTearDown(workspace.dispose);
    await tester.pumpWidget(MaterialApp(home: Scaffold(body: AgentComposer(
      key: key, workspace: workspace, humanMode: true, friendConversation: c,
      draftIdentity: audioConversation, recipientName: '测试联系人',
      onSubmit: (_) {}, onSearchArea: () {}, hasSearchArea: false,
      onCreateMoment: () => throw StateError('Human image must not create an Agent task'),
      voiceInputPort: _FeedbackVoice(), voiceConsentStore: MemoryVoiceInputConsentStore(),
    ))));
    final menu = key.currentState!.showNowMaterialMenu();
    await tester.pumpAndSettle();
    expect(find.text('本机图片'), findsOneWidget);
    expect(find.text('创建'), findsNothing);
    expect(find.byType(TextField), findsOneWidget);
    Navigator.pop(tester.element(find.text('本机图片')));
    await tester.pumpAndSettle(); await menu;
  });

  for (final status in [403, 404]) {
    testWidgets(
      'human HTTP$status disables text send and IME send while retaining its draft; HTTP200 restores it',
      (tester) async {
        var denied = false, reads = 0, posts = 0;
        final c = _humanController(
          MockClient((r) async {
            if (r.method == 'POST') posts++;
            expect(r.method, 'GET');
            expect(
              r.url.path,
              '/v1/me/conversations/$audioConversation/messages/page',
            );
            reads++;
            if (denied) return http.Response('{}', status);
            return chatPageWire(
              reads == 1
                  ? [
                      {
                        'id': audioMessage,
                        'conversationId': audioConversation,
                        'senderAccountId': audioPeer,
                        'speakerKind': 'human',
                        'body': '先前可见的私密消息',
                        'createdAt': '2026-10-11T01:02:03Z',
                      },
                    ]
                  : [],
            );
          }),
        );
        await c.initialise();
        final submitted = <String>[];
        await _paintHumanComposer(tester, c, submitted.add);
        await tester.enterText(find.byType(TextField), '保留待发送草稿');
        await tester.pump();
        expect(_sendButton(tester).onPressed, isNotNull);
        final sendBeforeDenial = _sendButton(tester).onPressed!;
        final retainedDraft = tester
            .widget<TextField>(find.byType(TextField))
            .controller!
            .value;

        denied = true;
        await c.load();
        // The old rendered callback must recheck capability before the next
        // frame, just as an IME send rechecks it independently of the button.
        sendBeforeDenial();
        expect(submitted, isEmpty);
        await tester.pump();
        expect(c.current, isTrue);
        expect(c.messages, isEmpty);
        expect(c.error, contains(status == 403 ? '访问权限' : '不可访问'));
        expect(_sendButton(tester).onPressed, isNull);
        await tester.testTextInput.receiveAction(TextInputAction.send);
        await tester.pump();
        expect(submitted, isEmpty);
        expect(_composerDraft(tester), '保留待发送草稿');
        expect(
          tester.widget<TextField>(find.byType(TextField)).controller!.value,
          retainedDraft,
        );
        expect(await c.sendText('保留待发送草稿'), isFalse);
        expect(posts, 0);

        denied = false;
        await c.load();
        await tester.pump();
        expect(reads, 3);
        expect(c.error, isNull);
        expect(_sendButton(tester).onPressed, isNotNull);
        expect(submitted, isEmpty);
        expect(_composerDraft(tester), '保留待发送草稿');
        await tester.tap(find.byTooltip('发送消息'));
        await tester.pump();
        expect(submitted, ['保留待发送草稿']);
        expect(posts, 0);
      },
    );
  }

  testWidgets('human unresolved pending message disables button and IME send', (
    tester,
  ) async {
    final pending = MemoryHumanMessagePendingStore();
    await pending.write(
      'https://unit.example',
      audioOwner,
      PendingHumanMessage(
        operationID: audioOperation,
        conversationID: audioConversation,
        payloadDigest: humanMessagePayloadDigest(audioConversation, '原操作'),
      ),
    );
    var posts = 0;
    final c = _humanController(
      MockClient((r) async {
        if (r.method == 'POST') posts++;
        expect(r.method, 'GET');
        expect(
          r.url.path,
          '/v1/me/conversations/$audioConversation/messages/page',
        );
        return chatPageWire([]);
      }),
      pending: pending,
    );
    await c.initialise();
    expect(c.pending, isTrue);
    final submitted = <String>[];
    await _paintHumanComposer(tester, c, submitted.add);
    await tester.enterText(find.byType(TextField), '后来的未发送草稿');
    await tester.pump();
    expect(_sendButton(tester).onPressed, isNull);
    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.pump();
    await c.load();
    await tester.pump();
    expect(c.pending, isTrue);
    expect(_sendButton(tester).onPressed, isNull);
    expect(submitted, isEmpty);
    expect(_composerDraft(tester), '后来的未发送草稿');
    expect(await c.sendText('后来的未发送草稿'), isFalse);
    expect(posts, 0);
  });

  testWidgets('human audio capability cannot enable an unsendable text draft', (
    tester,
  ) async {
    final c = _humanController(MockClient((_) async => chatPageWire([])));
    await c.initialise();
    expect(
      c.restoreAudioDraft(
        audioFixture(),
        owner: audioOwner,
        conversationID: audioConversation,
      ),
      isTrue,
    );
    expect(c.canSendAudio, isTrue);
    final submitted = <String>[];
    await _paintHumanComposer(tester, c, submitted.add);
    await tester.enterText(find.byType(TextField), '文字草稿保留');
    await tester.pump();
    expect(_sendButton(tester).onPressed, isNull);
    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.pump();
    expect(submitted, isEmpty);
    expect(_composerDraft(tester), '文字草稿保留');
    expect(c.audioDraft, isNotNull);
    await tester.enterText(find.byType(TextField), '');
    await tester.pump();
    expect(_sendButton(tester).onPressed, isNotNull);
    expect(c.canSendAudio, isTrue);
    expect(submitted, isEmpty);
  });

  testWidgets('Agent text remains sendable when a human controller is denied', (
    tester,
  ) async {
    final c = _humanController(
      MockClient((_) async => http.Response('{}', 404)),
    );
    await c.initialise();
    expect(c.historyAvailable, isFalse);
    final submitted = <String>[];
    await _paintHumanComposer(tester, c, submitted.add, humanMode: false);
    await tester.enterText(find.byType(TextField), 'Agent 原来的需求');
    await tester.pump();
    expect(_sendButton(tester).onPressed, isNotNull);
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(submitted, ['Agent 原来的需求']);
  });

  for (final heightBudget in [300.0, 140.0]) {
    testWidgets('200% 字号失败反馈可读且保留草稿，高度预算 $heightBudget', (tester) async {
      tester.view.physicalSize = const Size(375, 817);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final workspace = AgentWorkspaceController();
      addTearDown(workspace.dispose);
      final voice = _FeedbackVoice(), sent = <String>[];
      final consent = MemoryVoiceInputConsentStore();
      await consent.acknowledge(null);
      double? preferredHeight;
      await tester.pumpWidget(
        MaterialApp(
          theme: birdtieTheme(Brightness.light),
          home: MediaQuery(
            data: const MediaQueryData(textScaler: TextScaler.linear(2)),
            child: Scaffold(
              body: Align(
                alignment: Alignment.bottomCenter,
                child: SizedBox(
                  width: 343,
                  height: heightBudget,
                  child: AgentComposer(
                    workspace: workspace,
                    onSubmit: sent.add,
                    onSearchArea: () {},
                    hasSearchArea: false,
                    voiceInputPort: voice,
                    voiceConsentStore: consent,
                    onPreferredHeightChanged: (value) =>
                        preferredHeight = value,
                  ),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pump();
      await tester.enterText(find.byType(TextField), '原来未发送的草稿');
      await tester.tap(find.byTooltip('语音输入'));
      await tester.pumpAndSettle();
      final message = find.text(_voiceFailureMessage);
      expect(message, findsOneWidget);
      final feedback = find.ancestor(
        of: message,
        matching: find.byWidgetPredicate(
          (widget) =>
              widget is BirdtieSurface &&
              widget.kind == BirdtieSurfaceKind.critical,
        ),
      );
      final scroll = find.ancestor(
        of: message,
        matching: find.byType(Scrollable),
      );
      final position = tester.state<ScrollableState>(scroll).position;
      if (heightBudget == 300) {
        expect(
          tester.getRect(message).bottom,
          lessThanOrEqualTo(tester.getRect(feedback).bottom),
          reason: '空间足够时，失败提示末行“键盘”必须完整显示。',
        );
        expect(position.maxScrollExtent, 0);
        expect(
          preferredHeight,
          greaterThanOrEqualTo(
            tester.getSize(message).height +
                16 +
                tester.getSize(find.byType(TextField)).height,
          ),
          reason: '宿主申请的高度应覆盖完整反馈和当前编辑器。',
        );
      } else {
        expect(position.maxScrollExtent, greaterThan(0));
        final scrollbar = find.ancestor(
          of: message,
          matching: find.byType(Scrollbar),
        );
        expect(scrollbar, findsOneWidget);
        expect(tester.widget<Scrollbar>(scrollbar).thumbVisibility, isTrue);
        await tester.drag(scroll, const Offset(0, -400));
        await tester.pumpAndSettle();
        expect(position.pixels, position.maxScrollExtent);
        expect(
          tester.getRect(message).bottom,
          lessThanOrEqualTo(tester.getRect(feedback).bottom),
        );
      }
      final close = find.byTooltip('关闭素材提示');
      expect(tester.getSize(close).width, greaterThanOrEqualTo(48));
      expect(tester.getSize(close).height, greaterThanOrEqualTo(48));
      expect(find.byType(TextField), findsOneWidget);
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        '原来未发送的草稿',
      );
      expect(sent, isEmpty);
      expect(voice.captures, 1);
      expect(tester.takeException(), isNull);
      await tester.tap(close);
      await tester.pumpAndSettle();
      expect(message, findsNothing);
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        '原来未发送的草稿',
      );
      expect(sent, isEmpty);
      await tester.pumpWidget(const SizedBox.shrink());
    });
  }
  testWidgets('空态真实不可用麦克风，有字原发送，提示常规字重', (tester) async {
    final workspace = AgentWorkspaceController();
    final sent = <String>[];
    await tester.pumpWidget(
      MaterialApp(
        theme: birdtieTheme(Brightness.light),
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: sent.add,
            onSearchArea: () {},
            hasSearchArea: false,
          ),
        ),
      ),
    );
    final field = find.byType(TextField);
    final hint = tester.widget<TextField>(field).decoration!.hintStyle!;
    expect(hint.fontSize, 15);
    expect(hint.fontWeight, FontWeight.w400);
    for (final empty in ['', '   ']) {
      await tester.enterText(field, empty);
      await tester.pump();
      expect(find.byIcon(Icons.mic_none_rounded), findsOneWidget);
      expect(find.byIcon(Icons.arrow_upward_rounded), findsOneWidget);
      final voice = find.byTooltip('语音输入');
      expect(
        tester
            .widget<IconButton>(
              find.byKey(const Key('now-composer-microphone')),
            )
            .onPressed,
        isNull,
      );
      expect(
        tester
            .widget<IconButton>(
              find.byKey(const Key('now-composer-primary-action')),
            )
            .onPressed,
        isNull,
      );
      expect(tester.getSize(voice).height, greaterThanOrEqualTo(48));
      await tester.tap(voice);
      expect(sent, isEmpty);
    }
    await tester.enterText(field, '  看看 Aberdeen  ');
    await tester.pump();
    expect(find.byIcon(Icons.mic_none_rounded), findsOneWidget);
    expect(find.byIcon(Icons.arrow_upward_rounded), findsOneWidget);
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(sent, ['看看 Aberdeen']);
    expect(find.byIcon(Icons.mic_none_rounded), findsOneWidget);
    expect(find.byType(TextField), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    workspace.dispose();
  });
  testWidgets(
    'Now has no attachment control without a current-session protocol',
    (tester) async {
      var clipboardReads = 0;
      var privateImageCalls = 0;
      final sent = <String>[];
      final workspace = AgentWorkspaceController();
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async {
          if (call.method == 'Clipboard.getData') {
            clipboardReads++;
            return <String, String>{'text': 'private clipboard'};
          }
          return null;
        },
      );
      addTearDown(() {
        tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AgentComposer(
              workspace: workspace,
              onSubmit: sent.add,
              onSearchArea: () {},
              hasSearchArea: false,
              onPrivateMomentImage: () => privateImageCalls++,
              privateMomentImageContext: () => 'private-moment',
            ),
          ),
        ),
      );
      await tester.enterText(find.byType(TextField), '当前会话草稿');
      await tester.pump();
      expect(find.byTooltip('打开快捷操作'), findsNothing);
      expect(find.byIcon(Icons.add), findsNothing);
      expect(find.byTooltip('添加材料'), findsNothing);
      expect(find.text('添加素材'), findsNothing);
      expect(find.text('为私人记录添加图片'), findsNothing);
      expect(clipboardReads, 0);
      expect(privateImageCalls, 0);
      expect(sent, isEmpty);
      expect(
        tester.widget<TextField>(find.byType(TextField)).controller!.text,
        '当前会话草稿',
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      workspace.dispose();
    },
  );

  for (final brightness in Brightness.values) {
    testWidgets(
      'composer $brightness narrow large text preserves draft and cancel',
      (tester) async {
        tester.view.physicalSize = const Size(320, 720);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final workspace = AgentWorkspaceController();
        final sent = <String>[];
        await tester.pumpWidget(
          MaterialApp(
            theme: birdtieTheme(brightness),
            home: MediaQuery(
              data: const MediaQueryData(
                textScaler: TextScaler.linear(3),
                viewInsets: EdgeInsets.only(bottom: 260),
              ),
              child: Scaffold(
                body: Align(
                  alignment: Alignment.bottomCenter,
                  child: AgentComposer(
                    workspace: workspace,
                    onSubmit: sent.add,
                    onSearchArea: () {},
                    hasSearchArea: false,
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.enterText(find.byType(TextField), '周末想找附近可用的地点 Aberdeen');
        await tester.pump();
        expect(tester.takeException(), isNull);
        final send = find.byTooltip('发送需求');
        expect(tester.getSize(send).height, greaterThanOrEqualTo(48));
        final gesture = await tester.startGesture(tester.getCenter(send));
        await gesture.moveBy(const Offset(-180, -110));
        await gesture.up();
        await tester.pump();
        expect(sent, isEmpty);
        expect(
          tester.widget<TextField>(find.byType(TextField)).controller!.text,
          '周末想找附近可用的地点 Aberdeen',
        );
        await tester.tap(send);
        await tester.pump();
        expect(sent, ['周末想找附近可用的地点 Aberdeen']);
        await tester.tap(find.byTooltip('语音输入'));
        expect(sent, hasLength(1));
        await tester.pumpWidget(const SizedBox.shrink());
        workspace.dispose();
      },
    );
  }
  for (final withImage in [false, true]) {
    testWidgets(
      'preferred height covers actual large-text editor with fixed controls, image=$withImage',
      (tester) async {
        final workspace = AgentWorkspaceController();
        addTearDown(workspace.dispose);
        double? preferred;
        await tester.pumpWidget(
          MaterialApp(
            theme: birdtieTheme(Brightness.light),
            home: MediaQuery(
              data: const MediaQueryData(textScaler: TextScaler.linear(2)),
              child: Scaffold(
                body: Align(
                  alignment: Alignment.bottomCenter,
                  child: SizedBox(
                    width: 320,
                    child: AgentComposer(
                      workspace: workspace,
                      onSubmit: (_) {},
                      onSearchArea: () {},
                      hasSearchArea: false,
                      onDraftImage: withImage ? () {} : null,
                      // A retained helper alone must neither show an empty menu nor
                      // consume the editor's available width.
                      onPasteText: () {},
                      onPreferredHeightChanged: (height) => preferred = height,
                    ),
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.enterText(
          find.byType(TextField),
          List.filled(withImage ? 18 : 15, '中').join(),
        );
        await tester.pumpAndSettle();
        expect(preferred, isNotNull);
        expect(
          preferred!,
          greaterThanOrEqualTo(
            tester.getSize(find.byType(TextField)).height - .5,
          ),
        );
        expect(
          find.byIcon(Icons.add),
          withImage ? findsOneWidget : findsNothing,
        );
        expect(
          find.byKey(const Key('now-composer-microphone')),
          findsOneWidget,
        );
        expect(
          find.byKey(const Key('now-composer-primary-action')),
          findsOneWidget,
        );
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox.shrink());
      },
    );
  }
  testWidgets('IME composition is retained until the text is committed', (
    tester,
  ) async {
    final workspace = AgentWorkspaceController();
    final sent = <String>[];
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: sent.add,
            onSearchArea: () {},
            hasSearchArea: false,
          ),
        ),
      ),
    );
    await tester.showKeyboard(find.byType(TextField));
    tester.testTextInput.updateEditingValue(
      const TextEditingValue(
        text: '地点',
        selection: TextSelection.collapsed(offset: 2),
        composing: TextRange(start: 0, end: 2),
      ),
    );
    await tester.pump();
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(sent, isEmpty);
    expect(
      tester.widget<TextField>(find.byType(TextField)).controller!.text,
      '地点',
    );
    tester.testTextInput.updateEditingValue(
      const TextEditingValue(
        text: '地点',
        selection: TextSelection.collapsed(offset: 2),
      ),
    );
    await tester.pump();
    await tester.testTextInput.receiveAction(TextInputAction.send);
    await tester.pump();
    expect(sent, ['地点']);
    await tester.pumpWidget(const SizedBox.shrink());
    workspace.dispose();
  });
  testWidgets('composer can send another query while a request is pending', (
    tester,
  ) async {
    final source = _PendingSource();
    final workspace = AgentWorkspaceController(source: source);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AgentComposer(
            workspace: workspace,
            onSubmit: (query) => unawaited(workspace.submit(query, [], [])),
            onSearchArea: () {},
            hasSearchArea: false,
          ),
        ),
      ),
    );
    await tester.enterText(find.byType(TextField), 'A');
    await tester.pump();
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(workspace.state, AgentViewState.searching);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    await tester.enterText(find.byType(TextField), 'B');
    await tester.pump();
    expect(find.byTooltip('发送需求'), findsOneWidget);
    await tester.tap(find.byTooltip('发送需求'));
    await tester.pump();
    expect(source.requests.keys, ['A', 'B']);
    source.requests['B']!.complete(
      const AgentResult(entities: [], activities: [], places: [], note: 'B'),
    );
    source.requests['A']!.complete(
      const AgentResult(entities: [], activities: [], places: [], note: 'A'),
    );
    await tester.pump();
    expect(workspace.result?.note, 'B');
    await tester.pumpWidget(const SizedBox.shrink());
    workspace.dispose();
  });
}
