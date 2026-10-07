import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:birdtie_client/src/workspace/business_console_page.dart';
import 'package:birdtie_client/src/workspace/business_console_controller.dart';
import 'package:birdtie_client/src/workspace/business_public_permission_page.dart';
import 'package:birdtie_client/src/workspace/supplier_profile_api.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'business_console_controller_test.dart' show person, view, reply;
import 'business_knowledge_api_test.dart' show knowledgeAnswer;
import 'dart:async';
import 'package:http/http.dart' as http;

Future<BirdtieAuthController> signedInAuth() async {
  const owner = 'be000000-0000-4000-8000-000000000099';
  final auth = BirdtieAuthController(
    client: MockClient((r) async {
      if (r.url.path == '/v1/session/logout') return reply(null, 204);
      if (r.url.path == '/v1/me') return reply({'id': owner});
      if (r.url.path == '/v1/accounts/$owner/profile') {
        return reply({'displayName': '本地测试所有者'});
      }
      if (r.url.path == '/v1/auth/dev-phone/status') {
        return reply({'enabled': true});
      }
      return reply(null, 404);
    }),
    apiBaseUrl: 'http://127.0.0.1:1',
    sessionVault: MemorySessionVault()
      ..session = const StoredSession(
        token: 'local-business-contract',
        method: 'dev_phone',
      ),
  );
  await auth.initialize();
  return auth;
}

void main() {
  testWidgets(
    'same key external controller rebind permanently retires knowledge page without owning controllers',
    (tester) async {
      final auth = await signedInAuth();
      final city = PublicCityController();
      final org = OrganizationWorkspaceController(
        authorizationHeader: () => auth.authorizationHeader,
      );
      var posts = 0, reads = 0;
      final api = BusinessApi(
        authorizationHeader: () => auth.authorizationHeader,
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            return reply(knowledgeAnswer());
          }
          reads++;
          return reply(
            r.url.path.endsWith('/console') ? view() : [view()['business']],
          );
        }),
      );
      BusinessConsoleController controller() => BusinessConsoleController(
        api: api,
        accountID: () => auth.accountID,
        authorizationHeader: () => auth.authorizationHeader,
        organizationWorkspaceID: () => org.active?.id,
      );
      final old = controller(), next = controller();
      final selected = ValueNotifier<BusinessConsoleController>(old);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ValueListenableBuilder<BusinessConsoleController>(
              valueListenable: selected,
              builder: (_, c, _) => BusinessConsolePage(
                key: const ValueKey('same-controller'),
                auth: auth,
                city: city,
                organizations: org,
                api: api,
                controller: c,
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('本地合成商家').first);
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('查看商家资料回答'), 220);
      await tester.pumpAndSettle();
      await tester.tap(find.text('查看商家资料回答'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(OutlinedButton, '商家介绍'));
      await tester.pumpAndSettle();
      final ask = find.widgetWithText(FilledButton, '查看资料回答');
      await tester.scrollUntilVisible(ask, 220);
      await tester.pumpAndSettle();
      await tester.tap(ask);
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('本地合成资料回答'), 220);
      await tester.pumpAndSettle();
      selected.value = next;
      await tester.pumpAndSettle();
      selected.value = old;
      await tester.pumpAndSettle();
      expect(find.text('本地合成资料回答'), findsNothing);
      expect(find.textContaining('重新选择商家'), findsOneWidget);
      expect(posts, 1);
      await tester.pumpWidget(const SizedBox());
      final prior = reads;
      await old.load();
      await next.load();
      expect(reads, prior + 2);
      selected.dispose();
      old.dispose();
      next.dispose();
      api.dispose();
      auth.dispose();
      city.dispose();
      org.dispose();
    },
  );
  for (final scenario in [
    'cancel',
    'rebind-query',
    'rebind-editor',
    'rebind-public',
  ]) {
    testWidgets(
      'Console ordinary knowledge $scenario keeps old frame retired and borrowed transport usable',
      (tester) async {
        final auth = await signedInAuth();
        final city = PublicCityController();
        final org = OrganizationWorkspaceController(
          authorizationHeader: () => auth.authorizationHeader,
        );
        final pending = Completer<http.Response>();
        var posts = 0, writes = 0;
        BusinessApi makeApi(bool old) => BusinessApi(
          authorizationHeader: () => auth.authorizationHeader,
          apiBaseUrl: old ? 'http://old-fixture' : 'http://new-fixture',
          client: MockClient((r) async {
            if (r.url.path.endsWith('/knowledge/ask')) {
              posts++;
              return pending.future;
            }
            if (r.method != 'GET') writes++;
            return reply(
              r.url.path.endsWith('/console') ? view() : [view()['business']],
            );
          }),
        );
        final old = makeApi(true), next = makeApi(false);
        final selected = ValueNotifier<BusinessApi>(old);
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: ValueListenableBuilder<BusinessApi>(
                valueListenable: selected,
                builder: (_, api, _) => BusinessConsolePage(
                  key: const ValueKey('console'),
                  auth: auth,
                  city: city,
                  organizations: org,
                  api: api,
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        await tester.tap(find.text('本地合成商家').first);
        await tester.pumpAndSettle();
        final entry = find.text(switch (scenario) {
          'rebind-editor' => '编辑介绍与营业时间',
          'rebind-public' => '管理资料公开范围',
          _ => '查看商家资料回答',
        });
        await tester.scrollUntilVisible(entry, 220);
        await tester.pumpAndSettle();
        await tester.tap(entry);
        await tester.pumpAndSettle();
        if (scenario == 'cancel') {
          expect(posts, 0);
          await tester.pageBack();
          await tester.pumpAndSettle();
          await tester.scrollUntilVisible(find.text('商家工作台'), -220);
          await tester.pumpAndSettle();
          expect(find.text('商家工作台'), findsOneWidget);
        } else if (scenario == 'rebind-query') {
          await tester.tap(find.widgetWithText(OutlinedButton, '商家介绍'));
          await tester.pumpAndSettle();
          final ask = find.widgetWithText(FilledButton, '查看资料回答');
          await tester.scrollUntilVisible(ask, 220);
          await tester.pumpAndSettle();
          await tester.tap(ask);
          await tester.pump();
          selected.value = next;
          await tester.pumpAndSettle();
          pending.complete(reply(knowledgeAnswer()));
          await tester.pumpAndSettle();
          expect(find.text('本地合成资料回答'), findsNothing);
          expect(find.textContaining('重新选择商家'), findsOneWidget);
          expect(posts, 1);
          await tester.pageBack();
          await tester.pumpAndSettle();
        } else if (scenario == 'rebind-public') {
          final page = tester.widget<BusinessPublicPermissionPage>(
            find.byType(BusinessPublicPermissionPage),
          );
          expect(page.authorizationHeader(), auth.authorizationHeader);
          selected.value = next;
          await tester.pumpAndSettle();
          expect(page.authorizationHeader(), isNull);
          selected.value = old;
          await tester.pumpAndSettle();
          expect(page.authorizationHeader(), isNull);
          var publicPuts = 0;
          final guarded = SupplierProfileApi(
            authorizationHeader: page.authorizationHeader,
            workspaceID: page.workspaceID,
            apiBaseUrl: 'http://guarded-fixture',
            client: MockClient((r) async {
              if (r.method == 'PUT') publicPuts++;
              return reply(null);
            }),
          );
          await expectLater(
            guarded.changePermission(page.businessID, {}),
            throwsA(
              isA<BusinessApiException>().having(
                (e) => e.status,
                'retired session',
                401,
              ),
            ),
          );
          expect(publicPuts, 0);
          guarded.dispose();
          expect(find.text('工作身份已变化，请重新读取后检查。'), findsOneWidget);
          // The unchanged permission API receives a null retired Session, so it
          // cannot send any PUT even if the old transport is restored.
          await tester.pageBack();
          await tester.pumpAndSettle();
        } else {
          await tester.enterText(
            find.widgetWithText(TextFormField, '商家介绍'),
            '旧transport私人草稿',
          );
          selected.value = next;
          await tester.pumpAndSettle();
          expect(find.text('旧transport私人草稿'), findsNothing);
          expect(find.text('检查并预览'), findsNothing);
          expect(find.textContaining('工作身份已变化'), findsOneWidget);
          await tester.pageBack();
          await tester.pumpAndSettle();
        }
        expect(writes, 0);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        expect((await old.list()).isNotEmpty, true);
        expect((await next.list()).isNotEmpty, true);
        selected.dispose();
        old.dispose();
        next.dispose();
        auth.dispose();
        city.dispose();
        org.dispose();
      },
    );
  }
  for (final task in ['review-material', 'existing-venue']) {
    testWidgets(
      'Concrete $task preview preserves source and current version without writes',
      (tester) async {
        final auth = await signedInAuth();
        final city = PublicCityController();
        final organizations = OrganizationWorkspaceController(
          authorizationHeader: () => auth.authorizationHeader,
        );
        const boundPlace = 'be000000-0000-4000-8000-000000000088';
        final source = view();
        if (task == 'review-material') {
          source['canManage'] = false;
          source['canManageMembers'] = false;
          source['canReview'] = true;
          source['reviewPermissions'] = ['claim'];
          source['claim'] = {
            'version': 3,
            'state': 'pending',
            'name': '待核验具体商家名',
            'description': '这个版本的完整介绍',
            'sourceUrl': 'https://example.invalid/exact-version',
            'rightsNote': '具体经营权声明材料',
          };
        } else {
          source['venues'] = [
            {
              'placeId': boundPlace,
              'placeName': '另一城市已绑定场地',
              'state': 'pending',
              'version': 4,
              'operationStatus': 'verified',
              'validUntil': DateTime.now()
                  .toUtc()
                  .add(const Duration(days: 1))
                  .toIso8601String(),
              'sourceUrl': 'https://example.invalid/venue-source',
              'rightsNote': '本地合法声明',
              'facts': {
                'suitability': ['羽毛球'],
                'bookingUrl': 'https://example.invalid/booking',
                'note': '合成既有资料',
              },
            },
          ];
        }
        var writes = 0;
        final api = BusinessApi(
          authorizationHeader: () => auth.authorizationHeader,
          apiBaseUrl: 'http://fixture',
          client: MockClient((r) async {
            if (r.method != 'GET') writes++;
            return reply(
              r.url.path.endsWith('/console') ? source : [source['business']],
            );
          }),
        );
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: BusinessConsolePage(
                auth: auth,
                city: city,
                organizations: organizations,
                api: api,
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        await tester.tap(find.text('本地合成商家').first);
        await tester.pumpAndSettle();
        final entry = find.text(
          task == 'review-material' ? '审核经营权声明' : '编辑这项场地资料',
        );
        await tester.scrollUntilVisible(
          entry,
          250,
          scrollable: find.byType(Scrollable).first,
        );
        await tester.tap(entry);
        await tester.pumpAndSettle();
        if (task == 'review-material') {
          await tester.enterText(
            find.widgetWithText(TextFormField, '说明'),
            '只核验当前合成版本',
          );
        } else {
          expect(city.places, isEmpty);
          expect(find.text('已绑定场地'), findsOneWidget);
          expect(find.text('另一城市已绑定场地'), findsOneWidget);
          expect(find.byType(DropdownButtonFormField<String>), findsNothing);
        }
        await tester.scrollUntilVisible(
          find.text('检查并预览'),
          250,
          scrollable: find.byType(Scrollable).first,
        );
        await tester.pumpAndSettle();
        await tester.tap(find.text('检查并预览'));
        await tester.pumpAndSettle();
        expect(find.text('核对后再提交'), findsOneWidget);
        if (task == 'review-material') {
          final preview = find.byType(AlertDialog);
          expect(find.textContaining('当前资料版本：3'), findsOneWidget);
          expect(find.text('本次核验的具体材料'), findsOneWidget);
          expect(
            find.descendant(
              of: preview,
              matching: find.textContaining('这个版本的完整介绍'),
            ),
            findsOneWidget,
          );
          expect(
            find.descendant(
              of: preview,
              matching: find.textContaining('具体经营权声明材料'),
            ),
            findsOneWidget,
          );
          expect(
            find.descendant(
              of: preview,
              matching: find.textContaining(
                'https://example.invalid/exact-version',
              ),
            ),
            findsOneWidget,
          );
        } else {
          expect(find.textContaining('当前资料版本：4'), findsOneWidget);
          expect(find.textContaining(boundPlace), findsOneWidget);
        }
        await tester.tap(find.text('返回修改'));
        await tester.pumpAndSettle();
        expect(writes, 0);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox.shrink());
        api.dispose();
        organizations.dispose();
        city.dispose();
        auth.dispose();
      },
    );
  }
  for (final changed in ['denied', 'role', 'version']) {
    testWidgets(
      'Same identity $changed refresh hides approved materials without writes',
      (tester) async {
        final auth = await signedInAuth();
        final city = PublicCityController();
        final organizations = OrganizationWorkspaceController(
          authorizationHeader: () => auth.authorizationHeader,
        );
        var changedSource = false, writes = 0;
        final api = BusinessApi(
          authorizationHeader: () => auth.authorizationHeader,
          apiBaseUrl: 'http://fixture',
          client: MockClient((r) async {
            if (r.method != 'GET') writes++;
            if (r.url.path.endsWith('/console')) {
              if (changedSource && changed == 'denied') return reply(null, 403);
              return reply(
                view(
                  owner: !(changedSource && changed == 'role'),
                  version: changedSource && changed == 'version' ? 1 : 0,
                ),
              );
            }
            return reply([view()['business']]);
          }),
        );
        final controller = BusinessConsoleController(
          api: api,
          accountID: () => auth.accountID,
          authorizationHeader: () => auth.authorizationHeader,
          organizationWorkspaceID: () => organizations.active?.id,
        );
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: BusinessConsolePage(
                auth: auth,
                city: city,
                organizations: organizations,
                api: api,
                controller: controller,
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        await tester.tap(find.text('本地合成商家').first);
        await tester.pumpAndSettle();
        await tester.scrollUntilVisible(find.text('管理成员权限'), 250);
        await tester.pumpAndSettle();
        expect(find.text('管理成员权限').hitTestable(), findsOneWidget);
        await tester.tap(find.text('管理成员权限'));
        await tester.pumpAndSettle();
        await tester.enterText(
          find.widgetWithText(TextFormField, '对方账号标识'),
          person,
        );
        await tester.ensureVisible(find.text('检查并预览'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('检查并预览'));
        await tester.pumpAndSettle();
        expect(find.text('确认提交'), findsOneWidget);
        changedSource = true;
        await controller.refresh();
        await tester.pumpAndSettle();
        expect(find.text('资料或权限已变化'), findsOneWidget);
        expect(find.text('确认提交'), findsNothing);
        expect(find.textContaining(person), findsNothing);
        expect(writes, 0);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox.shrink());
        controller.dispose();
        api.dispose();
        organizations.dispose();
        city.dispose();
        auth.dispose();
      },
    );
  }
  for (final scale in [1.0, 1.6]) {
    testWidgets(
      'Chinese merchant workspace claim preview cancel on mobile scale $scale',
      (tester) async {
        tester.view.physicalSize = const Size(360, 640);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final auth = await signedInAuth();
        final city = PublicCityController();
        final organizations = OrganizationWorkspaceController(
          authorizationHeader: () => auth.authorizationHeader,
        );
        var writes = 0;
        final api = BusinessApi(
          authorizationHeader: () => auth.authorizationHeader,
          client: MockClient((r) async {
            if (r.method != 'GET') writes++;
            return reply([]);
          }),
          apiBaseUrl: 'http://fixture',
        );
        await tester.pumpWidget(
          MaterialApp(
            builder: (context, child) => MediaQuery(
              data: MediaQuery.of(
                context,
              ).copyWith(textScaler: TextScaler.linear(scale)),
              child: child!,
            ),
            home: Scaffold(
              body: BusinessConsolePage(
                auth: auth,
                city: city,
                organizations: organizations,
                api: api,
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        expect(find.text('暂无可管理或审核的商家。已有经营权时可提交申领材料。'), findsOneWidget);
        await tester.tap(find.text('申领商家'));
        await tester.pumpAndSettle();
        Future<void> enter(String label, String text) async {
          final field = find.widgetWithText(TextFormField, label);
          await tester.ensureVisible(field);
          await tester.pumpAndSettle();
          await tester.enterText(field, text);
        }

        await enter('商家名称', '本地合成商家');
        await enter('商家介绍', '仅用于契约测试');
        await enter('证明来源', 'https://example.invalid/claim');
        await enter('资料及经营权声明', '明确的本地合成经营权声明');
        await tester.ensureVisible(find.text('检查并预览'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('检查并预览'));
        await tester.pumpAndSettle();
        expect(find.text('核对后再提交'), findsOneWidget);
        expect(writes, 0);
        expect(
          tester
              .widget<FilledButton>(find.widgetWithText(FilledButton, '确认提交'))
              .onPressed,
          isNull,
        );
        await tester.tap(find.text('返回修改'));
        await tester.pumpAndSettle();
        expect(find.widgetWithText(TextFormField, '商家名称'), findsOneWidget);
        expect(
          tester
              .widget<TextFormField>(find.widgetWithText(TextFormField, '商家名称'))
              .controller!
              .text,
          '本地合成商家',
        );
        await tester.ensureVisible(find.widgetWithText(TextFormField, '证明来源'));
        await tester.pumpAndSettle();
        expect(
          tester
              .widget<TextFormField>(find.widgetWithText(TextFormField, '证明来源'))
              .controller!
              .text,
          'https://example.invalid/claim',
        );
        await tester.ensureVisible(find.text('检查并预览'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('检查并预览'));
        await tester.pumpAndSettle();
        expect(find.text('核对后再提交'), findsOneWidget);
        expect(
          tester
              .widget<FilledButton>(find.widgetWithText(FilledButton, '确认提交'))
              .onPressed,
          isNull,
        );
        expect(writes, 0);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox.shrink());
        auth.dispose();
        city.dispose();
        organizations.dispose();
        api.dispose();
      },
    );
  }
  testWidgets(
    'Actual owner member path submits only after checkbox and rereads',
    (tester) async {
      final auth = await signedInAuth();
      final city = PublicCityController();
      final organizations = OrganizationWorkspaceController(
        authorizationHeader: () => auth.authorizationHeader,
      );
      var writes = 0, reads = 0;
      final api = BusinessApi(
        authorizationHeader: () => auth.authorizationHeader,
        client: MockClient((r) async {
          if (r.method == 'PUT') {
            writes++;
            return reply(view(version: 1));
          }
          reads++;
          return reply(
            r.url.path.endsWith('/console')
                ? view(version: writes)
                : [view()['business']],
          );
        }),
        apiBaseUrl: 'http://fixture',
      );
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: BusinessConsolePage(
              auth: auth,
              city: city,
              organizations: organizations,
              api: api,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('本地合成商家').first);
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('管理成员权限'), 250);
      await tester.pumpAndSettle();
      expect(find.text('管理成员权限').hitTestable(), findsOneWidget);
      await tester.tap(find.text('管理成员权限'));
      await tester.pumpAndSettle();
      await tester.enterText(
        find.widgetWithText(TextFormField, '对方账号标识'),
        person,
      );
      await tester.ensureVisible(find.text('检查并预览'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('检查并预览'));
      await tester.pumpAndSettle();
      expect(writes, 0);
      await tester.ensureVisible(find.text('我已核对当前主体、资料和后果'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('我已核对当前主体、资料和后果'));
      await tester.pump();
      await tester.tap(find.text('确认提交'));
      await tester.pumpAndSettle();
      expect(writes, 1);
      expect(reads, 3);
      await tester.scrollUntilVisible(find.text('服务器已接受操作，当前资料已重新读取。'), -200);
      await tester.pumpAndSettle();
      expect(find.text('服务器已接受操作，当前资料已重新读取。'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      auth.dispose();
      city.dispose();
      organizations.dispose();
      api.dispose();
    },
  );
  testWidgets('Signing out during preview hides old draft and cannot submit', (
    tester,
  ) async {
    final auth = await signedInAuth();
    final city = PublicCityController();
    final organizations = OrganizationWorkspaceController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    var writes = 0;
    final api = BusinessApi(
      authorizationHeader: () => auth.authorizationHeader,
      client: MockClient((r) async {
        if (r.method != 'GET') writes++;
        return reply(
          r.url.path.endsWith('/console') ? view() : [view()['business']],
        );
      }),
      apiBaseUrl: 'http://fixture',
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: BusinessConsolePage(
            auth: auth,
            city: city,
            organizations: organizations,
            api: api,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('本地合成商家').first);
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(find.text('管理成员权限'), 250);
    await tester.pumpAndSettle();
    expect(find.text('管理成员权限').hitTestable(), findsOneWidget);
    await tester.tap(find.text('管理成员权限'));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.widgetWithText(TextFormField, '对方账号标识'),
      person,
    );
    await tester.ensureVisible(find.text('检查并预览'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('检查并预览'));
    await tester.pumpAndSettle();
    await auth.signOut();
    await tester.pumpAndSettle();
    expect(find.text('工作身份已变化'), findsOneWidget);
    expect(find.text('确认提交'), findsNothing);
    expect(find.textContaining(person), findsNothing);
    expect(writes, 0);
    await tester.pumpWidget(const SizedBox.shrink());
    auth.dispose();
    city.dispose();
    organizations.dispose();
    api.dispose();
  });
}
