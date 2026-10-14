import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'legacy_archive_api_test.dart';
import 'legacy_archive_page_test.dart' show archivePump, archiveSize;

class ArchiveSettingsFixture {
  ArchiveSettingsFixture() {
    auth.owner = archiveOwner;
    moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
    );
    client = ArchiveBorrowedClient((r) async {
      calls.add(r);
      expectSync(r.method, 'GET');
      expectSync(r.bodyBytes, isEmpty);
      expectSync(r.url.origin, base.value);
      expectSync(
        r.headers.keys.any((k) => k.toLowerCase().contains('workspace')),
        false,
      );
      if (r.url.path == '/v1/me/migration-archive')
        return archiveResponse({
          'items': [archiveSummary(1)],
        });
      if (r.url.path == '/v1/me/migration-archive/' + archiveID(1))
        return archiveResponse(archiveDetail(1));
      return archiveResponse([]);
    });
  }
  final auth = SeedTestAuth();
  final city = PublicCityController();
  late final PrivateMomentController moments;
  final workspace = ValueNotifier<String?>(null);
  final base = ValueNotifier(archiveOrigin);
  final listener = ValueNotifier<Listenable?>(null);
  final calls = <http.Request>[];
  late final ArchiveBorrowedClient client;
  String? currentWorkspace() => workspace.value;
  Widget harness() => MaterialApp(
    debugShowCheckedModeBanner: false,
    home: ValueListenableBuilder<String>(
      valueListenable: base,
      builder: (_, origin, _) => ValueListenableBuilder<Listenable?>(
        valueListenable: listener,
        builder: (_, notifier, _) => Scaffold(
          body: SettingsPage(
            key: const ValueKey('archive-settings'),
            auth: auth,
            city: city,
            moments: moments,
            client: client,
            apiBaseUrl: origin,
            workspaceChanges: notifier ?? workspace,
            organizationWorkspaceID: currentWorkspace,
          ),
        ),
      ),
    ),
  );
  void dispose() {
    auth.dispose();
    city.dispose();
    moments.dispose();
    workspace.dispose();
    base.dispose();
    listener.dispose();
    client.close();
  }
}

Future<void> openSettingsArchive(
  WidgetTester t,
  ArchiveSettingsFixture f,
) async {
  FlutterSecureStorage.setMockInitialValues({});
  await archiveSize(t);
  await t.pumpWidget(f.harness());
  await archivePump(t);
  await t.ensureVisible(find.text('旧内容档案'));
  await t.pump();
  await t.tap(find.text('旧内容档案'));
  await archivePump(t);
}

Future<void> closeSettingsArchive(
  WidgetTester t,
  ArchiveSettingsFixture f,
) async {
  await t.pumpWidget(const SizedBox());
  f.dispose();
}

void main() {
  testWidgets(
    'settings real personal old archive route uses injected origin and same client without native writes',
    (t) async {
      final f = ArchiveSettingsFixture();
      await openSettingsArchive(t, f);
      expect(
        find.descendant(of: find.byType(AppBar), matching: find.text('旧内容档案')),
        findsOneWidget,
      );
      expect(find.text('隔离旧记录 1'), findsOneWidget);
      await t.tap(find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))));
      await archivePump(t);
      expect(
        t.widget<Text>(find.byKey(const Key('legacy-archive-body'))).data,
        archiveDetail(1)['body'],
      );
      expect(
        f.calls.where((r) => r.url.path.contains('migration-archive')),
        hasLength(2),
      );
      expect(f.calls.every((r) => r.method == 'GET'), true);
      expect(f.client.wasClosed, false);
      await closeSettingsArchive(t, f);
    },
  );
  testWidgets(
    'settings current same-owner credential renewal preserves archive route and current reader',
    (t) async {
      final f = ArchiveSettingsFixture();
      await openSettingsArchive(t, f);
      final old = t
          .widget<ListTile>(
            find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))),
          )
          .onTap!;
      f.auth.changeIdentity(
        'Bearer renewed-synthetic',
        nextOwner: archiveOwner,
      );
      await archivePump(t);
      expect(find.text('隔离旧记录 1'), findsOneWidget);
      final n = f.calls.length;
      old();
      await archivePump(t);
      expect(f.calls.length, n);
      await t.tap(find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))));
      await archivePump(t);
      expect(find.byKey(const Key('legacy-archive-body')), findsOneWidget);
      expect(f.calls.last.headers['authorization'], 'Bearer renewed-synthetic');
      await closeSettingsArchive(t, f);
    },
  );
  for (final axis in ['owner', 'workspace', 'origin', 'listener']) {
    testWidgets(
      'settings archive real $axis ABA retires borrowed detail and old row GET',
      (t) async {
        final f = ArchiveSettingsFixture();
        await openSettingsArchive(t, f);
        final old = t
            .widget<ListTile>(
              find.byKey(ValueKey('legacy-archive-row-' + archiveID(1))),
            )
            .onTap!;
        old();
        await archivePump(t);
        expect(find.byKey(const Key('legacy-archive-body')), findsOneWidget);
        final otherListener = ValueNotifier(0);
        if (axis == 'owner') {
          f.auth.changeIdentity(
            'Bearer other-synthetic',
            nextOwner: archiveOtherOwner,
          );
          f.auth.changeIdentity(
            'Bearer returned-synthetic',
            nextOwner: archiveOwner,
          );
        } else if (axis == 'workspace') {
          f.workspace.value = archiveID(900);
          f.workspace.value = null;
        } else if (axis == 'origin') {
          f.base.value = 'https://other-archive-isolated.test';
          await t.pump();
          f.base.value = archiveOrigin;
          await t.pump();
        } else {
          f.listener.value = otherListener;
          await t.pump();
          f.listener.value = null;
          await t.pump();
        }
        await archivePump(t);
        expect(find.byKey(const Key('legacy-archive-body')), findsNothing);
        expect(find.textContaining('查看来源已变化'), findsOneWidget);
        final n = f.calls.length;
        old();
        await archivePump(t);
        expect(f.calls.length, n);
        expect(f.client.wasClosed, false);
        expect(t.takeException(), isNull);
        await closeSettingsArchive(t, f);
        otherListener.dispose();
      },
    );
  }
  testWidgets(
    'settings organization workspace has no personal archive entry and zero archive reads',
    (t) async {
      final f = ArchiveSettingsFixture()..workspace.value = archiveID(900);
      FlutterSecureStorage.setMockInitialValues({});
      await t.pumpWidget(f.harness());
      await archivePump(t);
      expect(find.text('旧内容档案'), findsNothing);
      expect(
        f.calls.where((r) => r.url.path.contains('migration-archive')),
        isEmpty,
      );
      await closeSettingsArchive(t, f);
    },
  );
  testWidgets(
    'settings signed out cannot open old-owner archive without a real personal session',
    (t) async {
      final f = ArchiveSettingsFixture();
      f.auth.changeIdentity(null, nextOwner: archiveOwner);
      await t.pumpWidget(f.harness());
      await archivePump(t);
      expect(find.text('旧内容档案'), findsNothing);
      expect(f.calls, isEmpty);
      await closeSettingsArchive(t, f);
    },
  );
}
