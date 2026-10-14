import 'dart:convert';

import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/task_image_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart' show NowFixtureCity, NowFixtureSource;
import 'private_moment_media_controller_test.dart'
    show syntheticPrivatePNG, TestMomentImagePicker;

const _owner = '22222222-2222-4222-8222-222222222222';
const _token = 'Bearer synthetic-media-recovery';
const _draftText = '这张图片先留在首页，我还没有发送。';

/// Models the actual launch ordering: a verified token/owner is observable
/// before the restoration flag changes, with no second identity change.
class _RestoringAuth extends SeedTestAuth {
  _RestoringAuth({required bool identityBeforeMount}) {
    token = identityBeforeMount ? _token : null;
    owner = _owner;
  }

  bool _restoring = true;

  @override
  bool get sessionRestoring => _restoring;

  void restoreIdentity() {
    token = _token;
    owner = _owner;
    notifyListeners();
  }

  void finishRestoring() {
    _restoring = false;
    notifyListeners();
  }
}

AgentComposer _composer(WidgetTester tester) =>
    tester.widget<AgentComposer>(find.byType(AgentComposer));

Finder _input() => find.descendant(
  of: find.byType(AgentComposer),
  matching: find.byType(TextField),
);

void main() {
  for (final identityBeforeMount in [false, true]) {
    testWidgets(
      '真实 Now 图片入口在${identityBeforeMount ? '挂载前身份已恢复' : '挂载后身份先恢复'}且同身份恢复结束后可选图并保留原文',
      (tester) async {
        tester.binding.handleAppLifecycleStateChanged(
          AppLifecycleState.resumed,
        );
        FlutterSecureStorage.setMockInitialValues({});
        tester.view.physicalSize = const Size(390, 844);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);

        final png = (await tester.runAsync(syntheticPrivatePNG))!;
        final picker = TestMomentImagePicker(png);
        final auth = _RestoringAuth(identityBeforeMount: identityBeforeMount);
        final city = NowFixtureCity();
        final source = NowFixtureSource(city);
        final requests = <http.Request>[];
        final client = MockClient((request) async {
          requests.add(request);
          return http.Response(
            jsonEncode({'data': []}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        });
        final moments = PrivateMomentController(
          client: client,
          apiBaseUrl: 'https://media-recovery.synthetic.example',
          authorizationHeader: () => auth.authorizationHeader,
        );
        addTearDown(() async {
          await tester.pumpWidget(const SizedBox());
          await tester.pumpAndSettle();
          moments.dispose();
          city.dispose();
          auth.dispose();
          client.close();
        });

        await tester.pumpWidget(
          MaterialApp(
            home: MapWorkspace(
              city: city,
              auth: auth,
              moments: moments,
              agentTaskSource: source,
              seedClient: client,
              seedApiBaseUrl: 'https://media-recovery.synthetic.example',
              taskImagePicker: picker,
              taskImagePendingStore: MemoryTaskImagePendingStore(),
            ),
          ),
        );
        await tester.pumpAndSettle();
        if (!identityBeforeMount) {
          auth.restoreIdentity();
          await tester.pumpAndSettle();
        }

        expect(auth.signedIn, isTrue);
        expect(auth.sessionRestoring, isTrue);
        expect(_composer(tester).onDraftImage, isNull);
        expect(_composer(tester).imageMaterialRecovering, isTrue);
        expect(picker.calls, 0);
        await tester.enterText(_input(), _draftText);
        await tester.pumpAndSettle();
        final identity = (auth.authorizationHeader, auth.accountID);

        auth.finishRestoring();
        await tester.pumpAndSettle();

        expect((auth.authorizationHeader, auth.accountID), identity);
        expect(auth.sessionRestoring, isFalse);
        expect(_composer(tester).onDraftImage, isNotNull);
        expect(_composer(tester).imageMaterialRecovering, isFalse);
        expect(tester.widget<TextField>(_input()).controller!.text, _draftText);
        final draft = _composer(tester).imageDraft!;
        await tester.runAsync(() async {
          // Use the host's real picker callback, including its source checks
          // and local-media synchronization, rather than calling a fake setter.
          _composer(tester).onDraftImage!();
          for (var n = 0; n < 100 && draft.busy; n++) {
            await Future<void>.delayed(const Duration(milliseconds: 10));
          }
        });
        await tester.pumpAndSettle();

        expect(picker.calls, 1);
        expect(draft.current, isTrue);
        expect(draft.busy, isFalse);
        expect(draft.error, isNull);
        expect(draft.localBytes, isNotNull);
        expect(draft.reviewed, isFalse);
        final thumbnail = find.byKey(const Key('now-composer-image-thumbnail'));
        expect(thumbnail, findsOneWidget);
        expect(tester.getRect(thumbnail).bottom, lessThanOrEqualTo(844));
        expect(tester.widget<TextField>(_input()).controller!.text, _draftText);
        expect(source.queries, isEmpty);
        expect(
          tester.widget<MapCanvas>(find.byType(MapCanvas)).workspace.task,
          isNull,
        );
        expect(requests.where((request) => request.method != 'GET'), isEmpty);
        expect(tester.takeException(), isNull);
      },
    );
  }
}
