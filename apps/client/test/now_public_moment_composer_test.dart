import 'dart:async';
import 'package:birdtie_client/src/workspace/public_moment_page.dart';
import 'package:birdtie_client/src/workspace/moment_interaction.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'public_moment_api_test.dart' show publicMomentV3Data, momentReply;
import 'moment_locations_test.dart' show locatedPublicMapView;
import 'entity_share_pending_store_test.dart' show shareTarget;

const owner = '11111111-1111-4111-8111-111111111111';
Map<String, dynamic> page({int revision = 2}) {
  final at = DateTime.now().toUtc();
  return {
    'schemaVersion': 'moment-interaction-page-v2',
    'momentId': shareTarget,
    'momentRevision': revision,
    'canComment': true,
    'likeCount': 0,
    'commentCount': 0,
    'liked': false,
    'likeVersion': 0,
    'comments': <Object?>[],
    'nextCursor': null,
    'observedAt': at.toIso8601String(),
    'validUntil': at.add(const Duration(seconds: 30)).toIso8601String(),
    'sourceVersion': 'a' * 64,
  };
}

Future<void> settle(WidgetTester t) async {
  for (var i = 0; i < 12; i++) {
    await t.pump(const Duration(milliseconds: 20));
  }
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  testWidgets(
    'Now public reader binds original comment target without input and retires source',
    (t) async {
      final source = ValueNotifier(true);
      MomentInteractionController? target;
      String? title;
      final methods = <String>[];
      var reads = 0;
      final client = MockClient((r) async {
        methods.add(r.method);
        if (r.url.path == '/v1/moments/$shareTarget') {
          reads++;
          return momentReply(publicMomentV3Data(photo: false));
        }
        if (r.url.path.endsWith('/interactions')) return momentReply(page());
        if (r.url.path.endsWith('/locations'))
          return momentReply(
            locatedPublicMapView(shareTarget).cast<String, dynamic>(),
          );
        return http.Response('{}', 404);
      });
      Widget host() => MaterialApp(
        home: Scaffold(
          body: PublicMomentPage(
            momentID: shareTarget,
            authorizationHeader: () => 'Bearer synthetic',
            ownerID: () => owner,
            identityChanges: source,
            sourceCurrent: () => source.value,
            apiBaseUrl: 'https://now-reader.test',
            client: client,
            embedded: true,
            showManagement: false,
            useNowComposer: true,
            onCommentTarget: (c, momentID, text, reply) {
              target = c;
              title = text;
            },
          ),
        ),
      );
      await t.pumpWidget(host());
      await settle(t);
      expect(target?.momentID, shareTarget);
      expect(target?.canComment, true);
      expect(title, '本人明确公开的合成动态');
      expect(find.byType(TextField), findsNothing);
      expect(t.testTextInput.isVisible, false);
      final original = target;
      final rootList = t.element(find.byType(ListView).first);
      await t.pumpWidget(host());
      await settle(t);
      expect(target, same(original));
      expect(reads, 1);
      source.notifyListeners();
      await settle(t);
      expect(target, same(original));
      final place = find.widgetWithText(TextButton, '合成甲地点 · 合成地区 · GB');
      await t.scrollUntilVisible(
        place,
        160,
        scrollable: find.byType(Scrollable).first,
      );
      await t.tap(place);
      await settle(t);
      expect(find.byType(PlaceDetailSheet), findsOneWidget);
      expect(target, isNull);
      expect(title, isNull);
      await t.tap(find.text('返回原动态'));
      await settle(t);
      expect(find.byType(PlaceDetailSheet), findsNothing);
      expect(target?.momentID, shareTarget);
      expect(target?.canComment, true);
      expect(reads, 1);
      expect(t.element(find.byType(ListView).first), same(rootList));
      source.value = false;
      await settle(t);
      expect(target, isNull);
      expect(title, isNotNull);
      expect(original!.current(), false);
      expect(find.text('中文公开正文'), findsNothing);
      expect(methods.every((m) => m == 'GET'), true);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      source.dispose();
      client.close();
    },
  );
  testWidgets(
    'Now public comment revision mismatch disables original target without replacing body authority',
    (t) async {
      final interaction = Completer<http.Response>();
      MomentInteractionController? target;
      final client = MockClient((r) async {
        expect(r.method, 'GET');
        if (r.url.path == '/v1/moments/$shareTarget')
          return momentReply(publicMomentV3Data(photo: false));
        if (r.url.path.endsWith('/interactions')) return interaction.future;
        return http.Response('{}', 404);
      });
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PublicMomentPage(
              momentID: shareTarget,
              authorizationHeader: () => 'Bearer synthetic',
              ownerID: () => owner,
              apiBaseUrl: 'https://now-reader.test',
              client: client,
              embedded: true,
              showManagement: false,
              useNowComposer: true,
              onCommentTarget: (c, momentID, title, reply) => target = c,
            ),
          ),
        ),
      );
      await settle(t);
      final initial = target;
      expect(initial, isNotNull);
      expect(initial!.canComment, false);
      interaction.complete(momentReply(page(revision: 3)));
      await settle(t);
      expect(target, isNull);
      expect(initial.current(), false);
      expect(find.text('动态版本已变化，请重新核实后评论。'), findsOneWidget);
      expect(find.byType(TextField), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      client.close();
    },
  );
  testWidgets(
    'nested Moment reply keeps its exact target when ancestor source expires',
    (t) async {
      const childID = '33333333-3333-4333-8333-333333333333';
      const commentID = '44444444-4444-4444-8444-444444444444';
      final source = ValueNotifier(true);
      MomentInteractionController? target;
      String? targetID, title;
      MomentComment? reply;
      final client = MockClient((r) async {
        expect(r.method, 'GET');
        if (r.url.path == '/v1/moments/$shareTarget' ||
            r.url.path == '/v1/moments/$childID') {
          final id = r.url.path.split('/').last;
          return momentReply({
            ...publicMomentV3Data(photo: false),
            'id': id,
            'title': id == childID ? '子动态' : '父动态',
          });
        }
        if (r.url.path.endsWith('/interactions')) {
          final id = r.url.path.split('/')[3];
          return momentReply({
            ...page(),
            'momentId': id,
            'commentCount': id == childID ? 1 : 0,
            'comments': id != childID
                ? []
                : [
                    {
                      'id': commentID,
                      'body': '子动态原评论',
                      'createdAt': DateTime.now().toUtc().toIso8601String(),
                      'authorId': null,
                      'authorPublicProfile': null,
                      'canDelete': false,
                      'version': 1,
                      'canReply': true,
                      'replyTo': null,
                    },
                  ],
          });
        }
        if (r.url.path.endsWith('/locations'))
          return momentReply(
            locatedPublicMapView(shareTarget).cast<String, dynamic>(),
          );
        return http.Response('{}', 404);
      });
      Widget host() => MaterialApp(
        home: Scaffold(
          body: PublicMomentPage(
            momentID: shareTarget,
            authorizationHeader: () => 'Bearer synthetic',
            ownerID: () => owner,
            identityChanges: source,
            sourceCurrent: () => source.value,
            apiBaseUrl: 'https://now-reader.test',
            client: client,
            embedded: true,
            showManagement: false,
            useNowComposer: true,
            onCommentTarget: (c, id, text, parent) {
              target = c;
              targetID = id;
              title = text;
              reply = parent;
            },
          ),
        ),
      );
      await t.pumpWidget(host());
      await settle(t);
      final place = find.widgetWithText(TextButton, '合成甲地点 · 合成地区 · GB');
      await t.scrollUntilVisible(
        place,
        160,
        scrollable: find.byType(Scrollable).first,
      );
      await t.tap(place);
      await settle(t);
      t.widget<PlaceDetailSheet>(find.byType(PlaceDetailSheet)).onOpenMoment!(
        childID,
      );
      await settle(t);
      expect(targetID, childID);
      expect(title, '子动态');
      expect(target?.canComment, true);
      final panel = t.widget<MomentInteractionPanel>(
        find.byType(MomentInteractionPanel),
      );
      panel.onReply!(target!.page!.comments.single);
      await settle(t);
      expect(reply?.id, commentID);
      final original = target;
      await t.pumpWidget(host());
      await settle(t);
      expect(target, same(original));
      expect(reply?.id, commentID);
      source.value = false;
      await settle(t);
      expect(target, isNull);
      expect(targetID, childID);
      expect(title, '子动态');
      expect(reply?.id, commentID);
      expect(original!.current(), false);
      expect(find.byType(TextField), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      source.dispose();
      client.close();
    },
  );
}
