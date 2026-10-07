import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:birdtie_client/src/workspace/business_console_page.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'business_console_page_test.dart' show signedInAuth;
import 'business_console_controller_test.dart' show view, reply;
import 'business_agent_identity_api_test.dart' show identityWire, identityAgent;
import 'package:birdtie_client/src/workspace/business_agent_identity_page.dart';

void main() {
  testWidgets('商家工作台有可命中的智能体身份直接入口', (t) async {
    final auth = await signedInAuth();
    final city = PublicCityController();
    final org = OrganizationWorkspaceController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    var posts = 0;
    var established = false;
    final api = BusinessApi(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://business.test',
      client: MockClient((r) async {
        if (r.url.path.endsWith('/agent-identity')) {
          if (r.method == 'POST') {
            posts++;
            established = true;
          }
          return reply(identityWire(exists: established));
        }
        return reply(
          r.url.path.endsWith('/console') ? view() : [view()['business']],
        );
      }),
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: BusinessConsolePage(
            auth: auth,
            city: city,
            organizations: org,
            api: api,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.tap(find.text('本地合成商家'));
    await t.pumpAndSettle();
    final entry = find.widgetWithText(OutlinedButton, '商家智能体身份');
    expect(entry, findsOneWidget);
    await t.ensureVisible(entry);
    await t.pumpAndSettle();
    expect(entry.hitTestable(), findsOneWidget);
    expect(t.getSize(entry).height, greaterThanOrEqualTo(48));
    await t.tap(entry);
    await t.pumpAndSettle();
    expect(find.byType(BusinessAgentIdentityPage), findsOneWidget);
    expect(find.text('本地合成商家'), findsWidgets);
    final check = find.widgetWithText(FilledButton, '检查并建立身份');
    await t.ensureVisible(check);
    await t.tap(check);
    await t.pumpAndSettle();
    expect(find.text('确认建立商家智能体身份'), findsOneWidget);
    expect(posts, 0);
    await t.tap(find.widgetWithText(TextButton, '取消'));
    await t.pumpAndSettle();
    expect(posts, 0);
    await t.tap(check);
    await t.pumpAndSettle();
    await t.tap(find.widgetWithText(FilledButton, '确认建立'));
    await t.pumpAndSettle();
    expect(posts, 1);
    expect(find.text('身份已建立 · 暂停中'), findsOneWidget);
    expect(find.textContaining(identityAgent), findsNothing);
    await t.pumpWidget(const SizedBox());
    api.dispose();
    org.dispose();
    city.dispose();
    auth.dispose();
  });
}
