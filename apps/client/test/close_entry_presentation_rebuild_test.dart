import 'dart:convert';

import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/content/public_intent_section.dart';
import 'package:birdtie_client/src/workspace/close_connections_page.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'close_connections_api_test.dart';
import 'close_connections_page_test.dart' show closeTap;
import 'public_intent_identity_test.dart';

/// Synthetic transport, real production parent entry, Navigator route,
/// Close page, controller and API. No live account or network is used.
class CloseEntryScenario {
  CloseEntryScenario(this.entry, {this.expired = false}) {
    profile.profiles[profileOwnerA]!['visibility'] = 'close';
    a = source();
    b = source();
    profile.source.value = (a, 'http://a.test');
    String? workspaceID() => profile.workspace.value;
    home = ValueListenableBuilder<(http.Client, String)>(
      valueListenable: profile.source,
      builder: (context, config, _) => Scaffold(
        body: SingleChildScrollView(
          child: entry == 'profile'
              ? PublicIntentSection(
                  key: const ValueKey('same-profile-entry'),
                  auth: profile.auth,
                  city: profile.city,
                  client: config.$1,
                  apiBaseUrl: config.$2,
                  workspaceChanges: profile.workspace,
                  organizationWorkspaceID: workspaceID,
                )
              : SocialInboxSection(
                  key: const ValueKey('same-contacts-entry'),
                  auth: profile.auth,
                  client: config.$1,
                  apiBaseUrl: config.$2,
                  workspaceChanges: profile.workspace,
                  workspaceID: workspaceID,
                ),
        ),
      ),
    );
  }

  final String entry;
  final bool expired;
  final profile = ProfileScenario();
  final presentation = ValueNotifier<(double, Brightness)>((
    1,
    Brightness.light,
  ));
  late final ProfileTrackingClient a, b;
  late final Widget home;
  final calls = <http.Request>[];
  final receivedDeadlines = <String>[];
  var selected = false;

  ProfileTrackingClient source() => ProfileTrackingClient((r) async {
    if (r.url.path.startsWith('/v1/me/close-connections')) {
      calls.add(r);
      final now = DateTime.now().toUtc();
      if (r.method == 'PUT') {
        selected = true;
        return closeJSON(closeResult(now: now));
      }
      final packet = closeList(
        now: expired && calls.length == 1
            ? now.subtract(const Duration(seconds: 31))
            : now,
        entries: [closeEntry(selected: selected)],
      );
      receivedDeadlines.add(packet['validUntil'] as String);
      return closeJSON(packet);
    }
    if (entry == 'contacts' && r.url.path == '/v1/me') {
      return closeJSON({'id': profileOwnerA, 'accountType': 'person'});
    }
    return profile.reply(r);
  });

  Future<void> mount(WidgetTester t) async {
    await t.pumpWidget(
      ValueListenableBuilder<(double, Brightness)>(
        valueListenable: presentation,
        builder: (context, config, _) => MaterialApp(
          theme: birdtieTheme(config.$2),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(context).copyWith(
              textScaler: TextScaler.linear(config.$1),
            ),
            child: child!,
          ),
          home: home,
        ),
      ),
    );
    await t.pumpAndSettle();
    await profileTap(t, '管理亲密好友');
    await t.pumpAndSettle();
    expect(calls.map((r) => r.method), ['GET']);
  }

  Future<void> changePresentation(WidgetTester t) async {
    for (final next in [
      (1.3, Brightness.light),
      (2.0, Brightness.dark),
      (1.0, Brightness.light),
    ]) {
      presentation.value = next;
      await t.pumpAndSettle();
    }
  }

  Future<void> unmount(WidgetTester t) => profile.unmount(t);
  void dispose() {
    presentation.dispose();
    a.close();
    b.close();
    profile.dispose();
  }
}

void main() {
  for (final entry in ['profile', 'contacts']) {
    testWidgets('$entry真实入口主题和系统字号重建保留名单与具体确认，不重复GET', (
      t,
    ) async {
      final s = CloseEntryScenario(entry);
      addTearDown(s.dispose);
      await s.mount(t);
      final before = t.widget<CloseConnectionsPage>(
        find.byType(CloseConnectionsPage),
      );
      await closeTap(t, '设为亲密好友');
      await s.changePresentation(t);
      final after = t.widget<CloseConnectionsPage>(
        find.byType(CloseConnectionsPage),
      );
      expect(s.calls.map((r) => r.method), ['GET']);
      expect(after.workspaceChanges, same(before.workspaceChanges));
      expect(after.bindingCurrent, same(before.bindingCurrent));
      expect(after.organizationWorkspaceID, same(before.organizationWorkspaceID));
      expect(find.text('设为亲密好友？'), findsOneWidget);
      expect(find.text('确认设为亲密好友'), findsOneWidget);
      expect(s.receivedDeadlines, hasLength(1));
      await closeTap(t, '确认设为亲密好友');
      expect(s.calls.map((r) => r.method), ['GET', 'PUT', 'GET']);
      final put = jsonDecode(s.calls[1].body) as Map<String, dynamic>;
      expect(put['validUntil'], s.receivedDeadlines.first);
      expect(put['confirmed'], true);
      expect(find.text('已选为亲密好友'), findsOneWidget);
      expect(s.a.closed, false);
      expect(s.b.closed, false);
      expect(t.takeException(), isNull);
      await s.unmount(t);
    });

    testWidgets('$entry来源已到期时主题重建不续期，只有明确刷新才能读取新名单', (t) async {
      final s = CloseEntryScenario(entry, expired: true);
      addTearDown(s.dispose);
      await s.mount(t);
      final deadline = s.receivedDeadlines.single;
      await s.changePresentation(t);
      expect(s.calls.map((r) => r.method), ['GET']);
      expect(s.receivedDeadlines, [deadline]);
      expect(find.textContaining('当前关系核验已到期'), findsOneWidget);
      expect(
        t.widget<OutlinedButton>(
          find.byKey(const ValueKey('close-person:$closePeer')),
        ).onPressed,
        isNull,
      );
      await closeTap(t, '刷新后继续');
      expect(s.calls.map((r) => r.method), ['GET', 'GET']);
      expect(
        t.widget<OutlinedButton>(
          find.byKey(const ValueKey('close-person:$closePeer')),
        ).onPressed,
        isNotNull,
      );
      expect(t.takeException(), isNull);
      await s.unmount(t);
    });

    for (final mode in ['account', 'token', 'workspace', 'base', 'source']) {
      testWidgets('$entry稳定入口保留$mode A→B→A退役，旧确认不可写也不隐式读取', (
        t,
      ) async {
        final s = CloseEntryScenario(entry);
        addTearDown(s.dispose);
        await s.mount(t);
        await closeTap(t, '设为亲密好友');
        if (mode == 'workspace') {
          s.profile.workspace.value = profileOrg;
          s.profile.workspace.value = null;
        } else if (mode == 'account' || mode == 'token') {
          s.profile.auth.changeIdentity(
            profileTokenB,
            nextOwner: mode == 'account' ? profileOwnerB : profileOwnerA,
          );
          s.profile.auth.changeIdentity(profileTokenA, nextOwner: profileOwnerA);
        } else {
          s.profile.source.value = (
            mode == 'source' ? s.b : s.a,
            mode == 'base' ? 'http://b.test' : 'http://a.test',
          );
          await t.pumpAndSettle();
          s.profile.source.value = (s.a, 'http://a.test');
        }
        await t.pumpAndSettle();
        await s.changePresentation(t);
        expect(find.byType(AlertDialog), findsNothing);
        expect(find.textContaining('请返回并重新打开'), findsOneWidget);
        expect(s.calls.map((r) => r.method), ['GET']);
        expect(s.a.closed, false);
        expect(s.b.closed, false);
        expect(t.takeException(), isNull);
        await s.unmount(t);
      });
    }
  }
}
