import 'dart:convert';
import 'package:birdtie_client/src/workspace/notification_destination_router.dart';
import 'package:birdtie_client/src/workspace/community_conversation_page.dart';
import 'package:birdtie_client/src/workspace/place_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/private_place_memory_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_task_detail_page_test.dart'
    show taskDestinationID, taskDestinationOwner;
import 'private_place_memory_api_test.dart' show MemoryPlacePendingStore;

void main() {
  testWidgets(
    'system back returns to the nested source before leaving its entry',
    (t) async {
      final changes = ValueNotifier<int>(0);
      await t.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (outer) => Scaffold(
              body: TextButton(
                onPressed: () => Navigator.of(outer).push(
                  MaterialPageRoute<void>(
                    builder: (_) => NotificationDestinationBoundary(
                      identityChanges: changes,
                      current: () => true,
                      builder: (inner) => Scaffold(
                        appBar: AppBar(title: const Text('原引荐入口')),
                        body: TextButton(
                          onPressed: () => Navigator.of(inner).push(
                            MaterialPageRoute<void>(
                              builder: (_) => Scaffold(
                                appBar: AppBar(title: const Text('原找朋友流程')),
                                body: const Text('子流程'),
                              ),
                            ),
                          ),
                          child: const Text('打开原流程'),
                        ),
                      ),
                    ),
                  ),
                ),
                child: const Text('设置入口'),
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.tap(find.text('设置入口'));
      await t.pumpAndSettle();
      await t.tap(find.text('打开原流程'));
      await t.pumpAndSettle();
      expect(find.text('原找朋友流程'), findsOneWidget);
      await t.binding.handlePopRoute();
      await t.pumpAndSettle();
      expect(find.text('原引荐入口'), findsOneWidget);
      expect(find.text('原找朋友流程'), findsNothing);
      await t.binding.handlePopRoute();
      await t.pumpAndSettle();
      expect(find.text('设置入口'), findsOneWidget);
      expect(find.text('原引荐入口'), findsNothing);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      changes.dispose();
    },
  );
  testWidgets(
    'original Community approval dialog is removed on workspace ABA',
    (t) async {
      final workspace = ValueNotifier<String?>(null);
      final client = MockClient((r) async {
        expect(r.method, 'GET');
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': r.url.path.endsWith('/messages')
                  ? {'messages': [], 'hasMore': false}
                  : {
                      'viewerAccountId': taskDestinationOwner,
                      'joined': true,
                      'canJoin': true,
                      'canSend': true,
                      'moderator': false,
                    },
            }),
          ),
          200,
        );
      });
      await t.pumpWidget(
        MaterialApp(
          home: NotificationDestinationBoundary(
            identityChanges: workspace,
            current: () => workspace.value == null,
            builder: (_) => CommunityConversationPage(
              communityID: taskDestinationID,
              communityTitle: '原社群',
              authorizationHeader: () =>
                  workspace.value == null ? 'Bearer original' : null,
              client: client,
              apiBaseUrl: 'https://fixture',
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await t.tap(find.text('退出交流'));
      await t.pumpAndSettle();
      expect(find.text('确认'), findsOneWidget);
      workspace.value = 'org';
      workspace.value = null;
      await t.pumpAndSettle();
      expect(find.text('确认'), findsNothing);
      expect(find.text('退出交流'), findsNothing);
      expect(find.text('请重新打开内容'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      client.close();
      workspace.dispose();
    },
  );
  for (final mode in ['valid', 'wrong-owner-kind', 'missing-listener']) {
    testWidgets(
      'Place entry resolves actual Me identity and binds old frame: $mode',
      (t) async {
        final workspace = ValueNotifier<String?>(null);
        var meReads = 0;
        final client = MockClient((r) async {
          if (r.url.path == '/v1/me') {
            meReads++;
            return http.Response(
              jsonEncode({
                'data': {
                  'id': taskDestinationOwner,
                  'accountType': mode == 'wrong-owner-kind'
                      ? 'organization'
                      : 'person',
                },
              }),
              200,
            );
          }
          if (r.url.path == '/v1/places/$taskDestinationID') {
            return http.Response.bytes(
              utf8.encode(
                jsonEncode({
                  'data': {
                    'id': taskDestinationID,
                    'name': '本地地点',
                    'source': {'label': '隔离合成'},
                    'location': {'precision': 'none'},
                  },
                }),
              ),
              200,
            );
          }
          if (r.url.path.endsWith('/activities') ||
              r.url.path == '/v1/me/moments') {
            return http.Response('{"data":[]}', 200);
          }
          return http.Response('{}', 503);
        });
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: PlaceDetailSheet(
                placeID: taskDestinationID,
                authorizationHeader: () => 'Bearer opaque',
                onOpenActivity: (_) {},
                client: client,
                apiBaseUrl: 'https://fixture',
                organizationWorkspaceID: () => workspace.value,
                workspaceChanges: mode == 'missing-listener' ? null : workspace,
                privatePlacePendingStore: MemoryPlacePendingStore(),
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
        if (mode == 'missing-listener') {
          expect(find.text('我的地点记录'), findsNothing);
          expect(meReads, 0);
        } else {
          await t.scrollUntilVisible(find.text('我的地点记录'), 150);
          await t.tap(find.text('我的地点记录'));
          await t.pumpAndSettle();
          expect(meReads, 1);
          if (mode == 'valid') {
            final page = t.widget<PrivatePlaceMemoryPage>(
              find.byType(PrivatePlaceMemoryPage),
            );
            expect(page.accountID(), taskDestinationOwner);
            workspace.value = 'org';
            workspace.value = null;
            await t.pumpAndSettle();
            expect(find.byType(PrivatePlaceMemoryPage), findsNothing);
            expect(page.accountID(), isNull);
            expect(page.authorizationHeader(), isNull);
            expect(find.text('请重新打开内容'), findsOneWidget);
          } else {
            expect(find.byType(PrivatePlaceMemoryPage), findsNothing);
          }
        }
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        client.close();
        workspace.dispose();
      },
    );
  }
}
