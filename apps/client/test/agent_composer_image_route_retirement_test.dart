import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/workspace/friend_audio.dart';
import 'package:birdtie_client/src/workspace/friend_conversation_controller.dart';
import 'package:birdtie_client/src/workspace/friend_image.dart';
import 'package:birdtie_client/src/workspace/voice_input.dart';
import 'package:birdtie_client/src/workspace/voice_input_consent.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'connection_chat_page_test.dart' show chatPageWire;
import 'friend_audio_test.dart' show audioFixture;
import 'friend_image_test.dart'
    show imageFixtureBytes, imageOwner, imagePeer, imageConversation;
import 'private_moment_media_controller_test.dart' show TestMomentImagePicker;

const _recipient = '旧账号的私密联系人';
const _caption = '保留的图片说明，尚未发送';

class _ChangingOwner extends BirdtieAuthController {
  _ChangingOwner()
    : super(client: MockClient((_) async => http.Response('{}', 404)));
  String? owner = imageOwner;
  @override
  String? get accountID => owner;
  @override
  String? get authorizationHeader => owner == null ? null : 'Bearer $owner';
  @override
  bool get signedIn => owner != null;
  void changeOwner(String? next) {
    owner = next;
    notifyListeners();
  }
}

class _NoAudio extends FriendAudioPort {
  @override
  bool get supported => false;
  @override
  Future<void> start(String id, ValueChanged<FriendAudioPhase> onPhase) async =>
      throw StateError('No recording is expected');
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

class _NoVoice extends VoiceInputPort {
  @override
  Future<VoiceAvailability> availability() async =>
      const VoiceAvailability(onDevice: true);
  @override
  Future<bool> requestPermission(String id) async => false;
  @override
  Future<bool> localLanguageReady(String id, String language) async => false;
  @override
  Future<String> recognize({
    required String requestId,
    required String language,
    required bool onDevice,
    required VoiceSystemService? systemService,
    required bool systemApproved,
    required ValueChanged<VoiceInputPhase> onPhase,
  }) async => throw StateError('No recognition is expected');
  @override
  Future<void> stop(String id) async {}
  @override
  Future<void> cancel(String id) async {}
  @override
  void dispose() {}
}

void main() {
  for (final preview in [false, true]) {
    for (final retirement in [
      'logout',
      'account replacement',
      'source replacement',
      'controller disposal',
      'host build disposal',
      'before modal mount',
      'view retirement',
      'composer identity ABA',
    ]) {
      testWidgets(
        '${preview ? "image preview" : "image menu"} hides captured recipient after $retirement',
        (tester) async {
          tester.binding.handleAppLifecycleStateChanged(
            AppLifecycleState.resumed,
          );
          final bytes = (await tester.runAsync(imageFixtureBytes))!;
          final picker = TestMomentImagePicker(bytes);
          final auth = _ChangingOwner();
          final source = ValueNotifier<int>(0);
          final brightness = ValueNotifier<Brightness>(Brightness.light);
          final workspace = AgentWorkspaceController();
          final key = GlobalKey<AgentComposerState>();
          final voice = _NoVoice();
          final consent = MemoryVoiceInputConsentStore();
          var requests = 0, sends = 0;
          final client = MockClient((request) async {
            requests++;
            expect(request.method, 'GET');
            return chatPageWire([]);
          });
          final friend = FriendConversationController(
            auth: auth,
            conversation: const HumanConversation(
              id: imageConversation,
              otherAccountId: imagePeer,
              otherName: _recipient,
            ),
            apiBaseUrl: 'https://unit.example',
            client: client,
            audioPort: _NoAudio(),
            audioPendingStore: MemoryFriendAudioPendingStore(),
            messagePendingStore: MemoryHumanMessagePendingStore(),
            imageEnabled: true,
            imagePicker: picker,
            imagePendingStore: MemoryFriendImagePendingStore(),
            workspaceChanges: source,
            imageSourceBinding: () => source.value,
          );
          addTearDown(() {
            friend.dispose();
            workspace.dispose();
            source.dispose();
            brightness.dispose();
            auth.dispose();
            client.close();
          });
          await tester.runAsync(() async {
            await friend.initialise();
            await friend.selectImage(gestureCurrent: () => true);
          });
          expect(friend.imageDraft.localBytes, isNotNull);
          final originalPixels = List<int>.of(friend.imageDraft.localBytes!);
          final requestsBeforeRoute = requests;
          var identity = 0, human = true;
          var retireDuringBuild = false;
          var draft = const TextEditingValue(text: _caption);
          late StateSetter updateHost;
          await tester.pumpWidget(
            ValueListenableBuilder<Brightness>(
              valueListenable: brightness,
              builder: (context, value, child) =>
                  MaterialApp(theme: birdtieTheme(value), home: child),
              child: StatefulBuilder(
                builder: (context, update) {
                  updateHost = update;
                  if (retireDuringBuild) {
                    retireDuringBuild = false;
                    // As in MapWorkspace.didUpdateWidget, the source owner
                    // disposes its friend during its own branch's rebuild.
                    // The modal route is a different navigator branch.
                    friend.dispose();
                    human = false;
                    identity++;
                    draft = TextEditingValue.empty;
                  }
                  return Scaffold(
                    body: AgentComposer(
                      key: key,
                      workspace: workspace,
                      humanMode: human,
                      friendConversation: human ? friend : null,
                      recipientName: human ? _recipient : null,
                      draftIdentity: identity,
                      draftValue: draft,
                      onDraftChanged: (value) => draft = value,
                      onSubmit: (_) => sends++,
                      onSearchArea: () {},
                      hasSearchArea: false,
                      voiceInputPort: voice,
                      voiceConsentStore: consent,
                    ),
                  );
                },
              ),
            ),
          );
          await tester.pumpAndSettle();
          Future<void>? menu;
          if (preview) {
            await tester.tap(find.text('图片 · 发给 $_recipient'));
          } else {
            menu = key.currentState!.showNowMaterialMenu();
          }
          if (retirement == 'before modal mount') {
            // push has returned, but neither owned route has mounted yet.
            friend.dispose();
            updateHost(() {
              human = false;
              identity++;
              draft = TextEditingValue.empty;
            });
          }
          await tester.pumpAndSettle();
          final route = find.byType(preview ? Dialog : BottomSheet);
          Finder within(Finder child) =>
              find.descendant(of: route, matching: child);
          expect(route, findsOneWidget);
          if (retirement != 'before modal mount') {
            expect(within(find.text('发给 $_recipient')), findsOneWidget);
            if (preview) {
              expect(within(find.byType(Image)), findsOneWidget);
            }
          }

          switch (retirement) {
            case 'logout':
            case 'account replacement':
              auth.changeOwner(retirement == 'logout' ? null : imagePeer);
              updateHost(() {
                human = false;
                identity++;
                draft = TextEditingValue.empty;
              });
            case 'source replacement':
              source.value++;
            case 'controller disposal':
              // MapWorkspace disposes the old controller before rebuilding
              // its single composer on auth change. Its own notifier is
              // already closed when the draft emits retirement.
              friend.dispose();
              updateHost(() {
                human = false;
                identity++;
                draft = TextEditingValue.empty;
              });
            case 'view retirement':
              friend.retireImageView();
            case 'host build disposal':
              updateHost(() => retireDuringBuild = true);
            case 'before modal mount':
              break;
            case 'composer identity ABA':
              updateHost(() => identity = 1);
              await tester.pump();
              updateHost(() => identity = 0);
          }
          await tester.pumpAndSettle();
          // The route stays cancellable after retirement. An inherited theme
          // rebuild must not subscribe anew to a disposed friend or draft.
          brightness.value = Brightness.dark;
          await tester.pumpAndSettle();
          expect(tester.takeException(), isNull);
          expect(route, findsOneWidget);
          expect(within(find.textContaining(_recipient)), findsNothing);
          expect(within(find.byType(Image)), findsNothing);
          if (preview) {
            expect(within(find.text('查看图片')), findsOneWidget);
            expect(within(find.text('图片草稿已失效')), findsOneWidget);
            final mask = tester.widget<TextButton>(
              within(find.widgetWithText(TextButton, '拖动遮盖区域')),
            );
            expect(mask.onPressed, isNull);
          } else {
            expect(within(find.text('图片操作已失效')), findsOneWidget);
            expect(within(find.text('本机图片')), findsNothing);
            expect(within(find.text('查看与遮盖图片')), findsNothing);
            expect(within(find.text('移除本机图片')), findsNothing);
          }

          final preserve =
              retirement == 'view retirement' ||
              retirement == 'composer identity ABA';
          if (preserve) {
            expect(friend.imageDraft.localBytes, orderedEquals(originalPixels));
            expect(key.currentState!.draftText, _caption);
          } else if (retirement != 'source replacement') {
            expect(find.textContaining(_recipient), findsNothing);
            expect(key.currentState!.draftText, isEmpty);
          }
          expect(picker.calls, 1);
          expect(requests, requestsBeforeRoute);
          expect(sends, 0);
          await tester.tap(within(find.text(preview ? '完成查看' : '关闭')));
          await tester.pumpAndSettle();
          if (menu != null) await menu;
          expect(route, findsNothing);
          expect(find.byType(TextField), findsOneWidget);
          if (preserve) {
            expect(key.currentState!.draftText, _caption);
            expect(friend.imageDraft.localBytes, orderedEquals(originalPixels));
          }
          // A removed route must release its listeners. New source signals
          // and host rebuilds cannot call setState on the retired modal.
          source.value++;
          updateHost(() => identity++);
          await tester.pumpAndSettle();
          expect(tester.takeException(), isNull);
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox.shrink());
        },
      );
    }
  }
}
