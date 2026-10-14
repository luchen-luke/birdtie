import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/activity_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/entity_cover.dart';
import 'package:birdtie_client/src/workspace/entity_cover_editor.dart';
import 'package:birdtie_client/src/workspace/organization_activity_api.dart';
import 'package:birdtie_client/src/workspace/organization_console.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/public_organization_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_cover_test.dart';
import 'entity_cover_editor_test.dart' show coverTap;
import 'chat_entity_router_test.dart' show ChatTestAuth;
import 'now_scope_recovery_test.dart' show NowFixtureCity;

void _normalCoverEntry(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Map<String, dynamic> coveredActivity({Object? cover}) => {
  'id': coverRef.id,
  'title': '隔离合成活动',
  'summary': '实际公开正文',
  'description': '保留原活动正文',
  'startsAt': DateTime.now()
      .toUtc()
      .add(const Duration(days: 1))
      .toIso8601String(),
  'endsAt': DateTime.now()
      .toUtc()
      .add(const Duration(days: 1, hours: 1))
      .toIso8601String(),
  'timeZone': 'Europe/London',
  'schedule': '明天18:00',
  'status': 'upcoming',
  'source': {'label': '隔离夹具'},
  if (cover != null) 'cover': cover,
};
Map<String, dynamic> managedCoveredActivity(String owner, {Object? cover}) => {
  ...coveredActivity(cover: cover),
  'cityId': 'alpha',
  'publicationStatus': 'draft',
  'visibility': 'private',
  'organizer': {'type': 'ORGANIZATION', 'id': owner, 'name': '隔离合成组织'},
};
void main() {
  for (final kind in ['activity', 'organization']) {
    testWidgets(
      'cover canonical $kind detail actually renders authoritative anonymous JPEG and retires current source',
      (t) async {
        _normalCoverEntry(t);
        final changes = ValueNotifier(true);
        addTearDown(changes.dispose);
        final calls = <http.Request>[];
        final ref = EntityCoverRef(kind, coverRef.id);
        final client = MockClient((r) async {
          calls.add(r);
          if (r.url.path.endsWith('/content'))
            return http.Response.bytes(coverJPEG(), 200);
          return coverWire(
            kind == 'activity'
                ? coveredActivity(cover: coverMetadata(ref: ref))
                : {
                    'id': ref.id,
                    'name': '隔离合成组织',
                    'verificationStatus': 'verified',
                    'upcomingActivities': [],
                    'cover': coverMetadata(ref: ref),
                  },
          );
        });
        addTearDown(client.close);
        final content = kind == 'activity'
            ? ActivityDetailSheet(
                activity: PublicActivity.fromJson(coveredActivity()),
                apiBaseUrl: coverBase,
                client: client,
                authorizationHeader: () => null,
                identityChanges: changes,
                domainCurrent: () => changes.value,
              )
            : PublicOrganizationPage(
                organizationID: ref.id,
                apiBaseUrl: coverBase,
                client: client,
                authorizationHeader: () => null,
                workspaceID: () => changes.value ? null : 'different-context',
                identityChanges: changes,
                onOpenActivity: (_) {},
              );
        await t.pumpWidget(MaterialApp(home: Scaffold(body: content)));
        await decodeCover(t);
        expect(
          calls.where((r) => r.url.path.endsWith('/content')),
          hasLength(1),
        );
        expect(
          calls.every(
            (r) => r.method == 'GET' && r.headers['Authorization'] == null,
          ),
          isTrue,
        );
        if (kind == 'activity') {
          changes.value = false;
          await t.pump();
          expect(find.byType(Image), findsNothing);
        }
        await t.pumpWidget(const SizedBox());
        await t.pumpAndSettle();
        expect(t.takeException(), isNull);
      },
    );
  }
  testWidgets(
    'cover original organization owner entry retires role ABA without publishing',
    (t) async {
      _normalCoverEntry(t);
      FlutterSecureStorage.setMockInitialValues({});
      final auth = ChatTestAuth(), city = NowFixtureCity();
      final calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        return coverWire(
          r.url.path.endsWith('/cover') ? {'version': 0, 'cover': null} : [],
        );
      });
      final api = OrganizationActivityApi(
        client: client,
        apiBaseUrl: coverBase,
        authorizationHeader: () => auth.authorizationHeader,
      );
      final orgs = OrganizationWorkspaceController(
        client: client,
        apiBaseUrl: coverBase,
        authorizationHeader: () => auth.authorizationHeader,
      );
      OrganizationWorkspace org(String role) => OrganizationWorkspace(
        id: coverRef.id,
        name: '隔离合成组织',
        role: role,
        organizationType: 'society',
      );
      orgs.select(org('owner'));
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: OrganizationConsolePage(
              auth: auth,
              city: city,
              organizations: orgs,
              api: api,
            ),
          ),
        ),
      );
      await coverSettle(t);
      await coverTap(t, find.byKey(const Key('organization_cover_editor')));
      final editor = t.widget<EntityCoverEditor>(
        find.byType(EntityCoverEditor),
      );
      expect(editor.ref, EntityCoverRef('organization', coverRef.id));
      expect(editor.ownerID, auth.accountID);
      expect(editor.api.client, same(client));
      orgs.select(org('member'));
      orgs.select(org('owner'));
      await t.pump();
      expect(editor.current(), isFalse);
      expect(find.textContaining('当前身份、管理权限或对象已变化'), findsOneWidget);
      expect(calls.every((r) => r.method == 'GET'), isTrue);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      orgs.dispose();
      auth.dispose();
      city.dispose();
      api.dispose();
      client.close();
    },
  );
  testWidgets(
    'cover existing managed draft activity uses original writer transport and never creates activity',
    (t) async {
      _normalCoverEntry(t);
      FlutterSecureStorage.setMockInitialValues({});
      final auth = ChatTestAuth(), city = NowFixtureCity();
      final calls = <http.Request>[];
      final owner = coverRef.id;
      final client = MockClient((r) async {
        calls.add(r);
        if (r.url.path.endsWith('/cover'))
          return coverWire({'version': 0, 'cover': null});
        if (r.url.path == '/v1/me/organizations')
          return coverWire([
            {
              'id': owner,
              'name': '隔离合成组织',
              'role': 'owner',
              'organizationType': 'society',
            },
          ]);
        return coverWire([]);
      });
      final api = OrganizationActivityApi(
        client: client,
        apiBaseUrl: coverBase,
        authorizationHeader: () => auth.authorizationHeader,
      );
      final orgs = OrganizationWorkspaceController(
        client: client,
        apiBaseUrl: coverBase,
        authorizationHeader: () => auth.authorizationHeader,
      );
      final activity = OrganizationActivity.fromJson(
        managedCoveredActivity(owner),
      );
      await t.pumpWidget(
        MaterialApp(
          home: ActivityEditorPage(
            api: api,
            auth: auth,
            organizations: orgs,
            city: city,
            existing: activity,
          ),
        ),
      );
      await coverSettle(t);
      expect(activity.publicationStatus, 'draft');
      await coverTap(t, find.byKey(const Key('activity_cover_editor')));
      final editor = t.widget<EntityCoverEditor>(
        find.byType(EntityCoverEditor),
      );
      expect(editor.ref, coverRef);
      expect(editor.api.client, same(client));
      expect(editor.api.baseUrl, coverBase);
      expect(calls.where((r) => r.url.path == coverRef.path), hasLength(1));
      expect(calls.every((r) => r.method == 'GET'), isTrue);
      auth.use('Bearer B');
      auth.use('Bearer A');
      await t.pump();
      expect(editor.current(), isFalse);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      orgs.dispose();
      auth.dispose();
      city.dispose();
      api.dispose();
      client.close();
    },
  );
}
