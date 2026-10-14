import 'dart:convert';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:birdtie_client/src/content/public_intent_section.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/human_visibility.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'public_intent_identity_test.dart';
import 'close_connections_page_test.dart'
    show closeSnapshot, loadVisibilityFonts;
import 'close_connections_page_test.dart' show closeTap;
import 'close_connections_api_test.dart';
import 'package:http/http.dart' as http;

void main() {
  setUpAll(loadVisibilityFonts);
  for (final entry in ['contacts', 'profile']) {
    testWidgets('真实 $entry 父入口transport A→B→A关闭子route原确认不写', (t) async {
      final s = ProfileScenario();
      addTearDown(s.dispose);
      s.profiles[profileOwnerA]!['visibility'] = 'close';
      final methods = <String>[];
      ProfileTrackingClient source() => ProfileTrackingClient((r) async {
        if (r.url.path.startsWith('/v1/me/close-connections')) {
          methods.add(r.method);
          return closeJSON(closeList(now: DateTime.now().toUtc()));
        }
        if (r.url.path == '/v1/me' && entry == 'contacts') {
          return closeJSON({'id': closeOwner, 'accountType': 'person'});
        }
        return s.reply(r);
      });
      final a = source(), b = source();
      addTearDown(a.close);
      addTearDown(b.close);
      s.source.value = (a, 'http://a.test');
      if (entry == 'profile') {
        await s.mount(t);
      } else {
        await t.pumpWidget(
          MaterialApp(
            home: ValueListenableBuilder<(http.Client, String)>(
              valueListenable: s.source,
              builder: (context, config, _) => Scaffold(
                body: SingleChildScrollView(
                  child: SocialInboxSection(
                    key: const ValueKey('same-contact-parent'),
                    auth: s.auth,
                    client: config.$1,
                    apiBaseUrl: config.$2,
                  ),
                ),
              ),
            ),
          ),
        );
        await t.pumpAndSettle();
      }
      await profileTap(t, '管理亲密好友');
      await t.pumpAndSettle();
      await closeTap(t, '设为亲密好友');
      expect(find.text('确认设为亲密好友'), findsOneWidget);
      s.source.value = (b, 'http://b.test');
      await t.pumpAndSettle();
      s.source.value = (a, 'http://a.test');
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.textContaining('请返回并重新打开'), findsOneWidget);
      expect(methods, ['GET']);
      expect(a.closed, false);
      expect(b.closed, false);
      await s.unmount(t);
    });
  }
  for (final entry in humanVisibilityLabels.entries) {
    testWidgets('选择${entry.value}先显示真实后果，保存本人同档', (t) async {
      final s = ProfileScenario();
      addTearDown(s.dispose);
      await s.mount(t);
      final picker = find.byKey(const ValueKey('profile-visibility:private'));
      await t.ensureVisible(picker);
      await t.tap(picker);
      await t.pumpAndSettle();
      await t.tap(find.text(entry.value).last);
      await t.pumpAndSettle();
      expect(
        find.text(humanVisibilityDescriptions[entry.key]!),
        findsOneWidget,
      );
      expect(s.mutations, isEmpty);
      await profileTap(t, '保存个人资料');
      await t.pumpAndSettle();
      expect(jsonDecode(s.mutations.single.body)['visibility'], entry.key);
      if (entry.key != 'public') {
        expect(
          t
              .widget<FilledButton>(find.widgetWithText(FilledButton, '公开意图'))
              .onPressed,
          isNull,
        );
      }
      await s.unmount(t);
    });
  }
  for (final value in [
    'public',
    'followers',
    'connections',
    'close',
    'private',
  ]) {
    testWidgets('原资料真实读取 $value 后保存同档不退回private', (t) async {
      final s = ProfileScenario();
      addTearDown(s.dispose);
      s.profiles[profileOwnerA]!['visibility'] = value;
      await s.mount(t);
      await profileTap(t, '保存个人资料');
      await t.pumpAndSettle();
      expect(jsonDecode(s.mutations.single.body)['visibility'], value);
      await s.unmount(t);
    });
  }
  testWidgets('未知资料受众不能回退为private并保存', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    s.profiles[profileOwnerA]!['visibility'] = 'NEW_UNKNOWN';
    await s.mount(t);
    final button = t.widget<OutlinedButton>(
      find.widgetWithText(OutlinedButton, '保存个人资料'),
    );
    expect(button.onPressed, isNull);
    expect(find.text('读取失败，重试'), findsOneWidget);
    expect(s.mutations, isEmpty);
    await s.unmount(t);
  });
  testWidgets('原联系人组件直接提供亲密好友管理入口，无隐式写', (t) async {
    final s = ProfileScenario();
    addTearDown(s.dispose);
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: SocialInboxSection(
              auth: s.auth,
              client: s.client,
              apiBaseUrl: 'http://profile.test',
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('管理亲密好友'), findsOneWidget);
    expect(s.mutations, isEmpty);
    await s.unmount(t);
  });
  for (final setting in [
    (1.0, Brightness.light),
    (1.3, Brightness.light),
    (2.0, Brightness.light),
    (2.0, Brightness.dark),
  ]) {
    testWidgets('320dp个人资料${setting.$1} ${setting.$2}短后果与五档选择可达48', (t) async {
      t.view.physicalSize = const Size(320, 720);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final s = ProfileScenario();
      addTearDown(s.dispose);
      s.profiles[profileOwnerA]!['displayName'] = '长中文显示名称 Aberdeen 混排';
      s.profiles[profileOwnerA]!['visibility'] = 'close';
      await t.pumpWidget(
        MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: birdtieTheme(setting.$2),
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: TextScaler.linear(setting.$1)),
            child: RepaintBoundary(
              key: const Key('visibility-capture'),
              child: child!,
            ),
          ),
          home: Scaffold(
            body: SingleChildScrollView(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: PublicIntentSection(
                  auth: s.auth,
                  city: s.city,
                  client: s.client,
                  apiBaseUrl: 'http://profile.test',
                ),
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      final suffix = '${(setting.$1 * 100).round()}-${setting.$2.name}';
      await closeSnapshot(t, 'profile-top-$suffix');
      await t.ensureVisible(find.text(humanVisibilityDescriptions['close']!));
      await t.pumpAndSettle();
      await closeSnapshot(t, 'profile-audience-actions-$suffix');
      final save = find.widgetWithText(OutlinedButton, '保存个人资料');
      expect(t.getSize(save).height, greaterThanOrEqualTo(48));
      final picker = find.byKey(const ValueKey('profile-visibility:close'));
      await t.ensureVisible(picker);
      await t.tap(picker);
      await t.pumpAndSettle();
      await closeSnapshot(t, 'profile-audience-picker-$suffix');
      await t.tap(find.text('关注我的人').last);
      await t.pumpAndSettle();
      await profileTap(t, '保存个人资料');
      await t.pumpAndSettle();
      expect(jsonDecode(s.mutations.single.body)['visibility'], 'followers');
      expect(t.takeException(), isNull);
      await s.unmount(t);
    });
  }
}
