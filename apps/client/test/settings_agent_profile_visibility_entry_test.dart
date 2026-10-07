import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'agent_profile_visibility_api_test.dart'
    show
        audienceCommunity,
        audienceOtherCommunity,
        audienceAgent,
        audienceWire,
        audienceResponse,
        audienceCommunityRow;

class _Auth extends BirdtieAuthController {
  @override
  String? get authorizationHeader => 'Bearer synthetic-audience';
  @override
  String? get accountID => '80000000-0000-4000-8000-000000000001';
  @override
  bool get signedIn => true;
}

class _Wire extends http.BaseClient {
  final sent = <http.BaseRequest>[];
  bool closed = false;
  @override
  void close() {
    closed = true;
    super.close();
  }

  Map<String, Object>? saved;
  int version = 1;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    sent.add(r);
    if (r.url.path == '/v1/me/agent-profile-visibility') {
      if (r.method == 'PUT') {
        final m = jsonDecode((r as http.Request).body) as Map;
        expect(m['expectedVersion'], version);
        expect(m.keys.toSet(), {'expectedVersion', 'rules'});
        saved = Map<String, Object>.from(m['rules']);
        version++;
      }
      return Future.value(
        audienceResponse(
          audienceWire(
            version: version,
            rules: saved,
            configured: saved != null,
          ),
        ),
      );
    }
    if (r.url.path == '/v1/me/social-communities') {
      return Future.value(
        audienceResponse([
          audienceCommunityRow(audienceCommunity, '合成本人已加入社群', 'active'),
          audienceCommunityRow(audienceOtherCommunity, '合成邀请非成员', 'invited'),
          audienceCommunityRow(audienceAgent, '合成申请非成员', 'pending'),
        ]),
      );
    }
    return Future.value(
      http.StreamedResponse(Stream.value(utf8.encode('{"data":[]}')), 200),
    );
  }
}

Future<void> _visible(WidgetTester t, Finder f, {double step = 200}) async {
  await t.scrollUntilVisible(
    f,
    step,
    scrollable: find.byType(Scrollable).last,
    maxScrolls: 45,
  );
  await t.pumpAndSettle();
  expect(f.hitTestable(), findsOneWidget);
}

Future<void> _tap(WidgetTester t, Finder f) async {
  await t.ensureVisible(f);
  await t.pumpAndSettle();
  expect(f.hitTestable(), findsOneWidget);
  await t.tap(f);
  await t.pumpAndSettle();
}

Future<void> _entry(WidgetTester t) async {
  await _visible(t, find.widgetWithText(ListTile, '各项资料的可见范围'));
  await _tap(t, find.widgetWithText(ListTile, '各项资料的可见范围'));
}

Future<void> _choose(WidgetTester t, String field, String audience) async {
  await t.drag(find.byType(ListView).first, const Offset(0, 5000));
  await t.pumpAndSettle();
  await _visible(t, find.byKey(ValueKey('audience-field-$field')));
  await _tap(t, find.byKey(ValueKey('audience-field-$field')));
  final radio = find.byWidgetPredicate(
    (w) => w is RadioListTile<String> && w.value == audience,
  );
  await _visible(t, radio, step: 140);
  await _tap(t, radio);
  if (audience == 'COMMUNITY') {
    await _visible(
      t,
      find.widgetWithText(CheckboxListTile, '合成本人已加入社群'),
      step: 140,
    );
    expect(find.text('合成邀请非成员'), findsNothing);
    expect(find.text('合成申请非成员'), findsNothing);
    await _tap(t, find.widgetWithText(CheckboxListTile, '合成本人已加入社群'));
  }
  await _tap(t, find.widgetWithText(FilledButton, '保留为待检查的修改'));
}

void main() {
  testWidgets('实际设置入口可以找到各项资料的可见范围，初始不写', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final auth = _Auth(), wire = _Wire(), city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: wire,
      apiBaseUrl: 'https://audience.test',
    );
    addTearDown(() {
      moments.dispose();
      city.dispose();
      auth.dispose();
      wire.close();
    });
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: wire,
            apiBaseUrl: 'https://audience.test',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    for (var i = 0; i < 12 && find.text('各项资料的可见范围').evaluate().isEmpty; i++) {
      await t.drag(find.byType(ListView).first, const Offset(0, -320));
      await t.pumpAndSettle();
    }
    expect(
      find.text('各项资料的可见范围'),
      findsOneWidget,
      reason: '实际Settings没有十一字段受众管理的消费入口',
    );
    await t.ensureVisible(find.text('各项资料的可见范围'));
    await t.pumpAndSettle();
    expect(find.text('各项资料的可见范围').hitTestable(), findsOneWidget);
    await t.tap(find.text('各项资料的可见范围'));
    await t.pumpAndSettle();
    expect(
      wire.sent
          .where((r) => r.url.path == '/v1/me/agent-profile-visibility')
          .length,
      1,
    );
    expect(find.text('尚未自定义：名称与简介沿用公开范围，其余九项仅本人。'), findsOneWidget);
    expect(wire.sent.every((r) => r.method == 'GET'), true);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('真实入口大字号逐项选择好友智能体与有效社群，检查后仅完整PUT一次', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    t.view.physicalSize = const Size(320, 760);
    t.view.devicePixelRatio = 1;
    addTearDown(() {
      t.view.resetPhysicalSize();
      t.view.resetDevicePixelRatio();
    });
    final auth = _Auth(), wire = _Wire(), city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: wire,
      apiBaseUrl: 'https://audience.test',
    );
    addTearDown(() {
      moments.dispose();
      city.dispose();
      auth.dispose();
      wire.close();
    });
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(2)),
          child: child!,
        ),
        home: Scaffold(
          body: SettingsPage(
            auth: auth,
            city: city,
            moments: moments,
            client: wire,
            apiBaseUrl: 'https://audience.test',
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await _entry(t);
    await _choose(t, 'bio', 'CONNECTIONS');
    await _choose(t, 'agentNotes', 'AGENT_ONLY');
    await _choose(t, 'socialPreferences', 'COMMUNITY');
    expect(wire.sent.where((r) => r.method == 'PUT'), isEmpty);
    await _visible(t, find.widgetWithText(FilledButton, '检查受众改动'));
    await _tap(t, find.widgetWithText(FilledButton, '检查受众改动'));
    await _visible(t, find.widgetWithText(FilledButton, '确认保存以上受众设置'));
    expect(
      t.getRect(find.widgetWithText(FilledButton, '确认保存以上受众设置')).height,
      greaterThanOrEqualTo(48),
    );
    expect(wire.sent.where((r) => r.method == 'PUT'), isEmpty);
    await _tap(t, find.widgetWithText(FilledButton, '确认保存以上受众设置'));
    expect(wire.sent.where((r) => r.method == 'PUT').length, 1);
    expect(wire.saved!.length, 11);
    expect((wire.saved!['bio'] as Map)['visibility'], 'CONNECTIONS');
    expect((wire.saved!['agentNotes'] as Map)['visibility'], 'AGENT_ONLY');
    expect((wire.saved!['socialPreferences'] as Map)['communityIds'], [
      audienceCommunity,
    ]);
    expect(
      wire.sent.every(
        (r) =>
            r.method == 'GET' ||
            (r.method == 'PUT' &&
                r.url.path == '/v1/me/agent-profile-visibility'),
      ),
      true,
    );
    expect(t.takeException(), null);
    await t.pumpWidget(const SizedBox());
  });
  testWidgets('Settings父连接替换使旧具体检查退休，新的来源重开可正常读取', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final auth = _Auth(),
        first = _Wire(),
        second = _Wire(),
        city = PublicCityController();
    final moments = PrivateMomentController(
      authorizationHeader: () => auth.authorizationHeader,
      client: first,
      apiBaseUrl: 'https://audience.test',
    );
    final source = ValueNotifier<http.Client>(first);
    addTearDown(() {
      source.dispose();
      moments.dispose();
      city.dispose();
      auth.dispose();
      first.close();
      second.close();
    });
    await t.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<http.Client>(
          valueListenable: source,
          builder: (_, client, _) => Scaffold(
            body: SettingsPage(
              key: const ValueKey('settings'),
              auth: auth,
              city: city,
              moments: moments,
              client: client,
              apiBaseUrl: identical(client, first)
                  ? 'https://audience.test'
                  : 'https://new-audience.test',
            ),
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await _entry(t);
    await _choose(t, 'bio', 'PRIVATE');
    await _visible(t, find.widgetWithText(FilledButton, '检查受众改动'));
    await _tap(t, find.widgetWithText(FilledButton, '检查受众改动'));
    await _visible(t, find.widgetWithText(FilledButton, '确认保存以上受众设置'));
    source.value = second;
    await t.pump();
    await t.pumpAndSettle();
    expect(find.widgetWithText(FilledButton, '确认保存以上受众设置'), findsNothing);
    expect(first.sent.where((r) => r.method == 'PUT'), isEmpty);
    expect(second.sent.where((r) => r.method == 'PUT'), isEmpty);
    await t.pageBack();
    await t.pumpAndSettle();
    await t.drag(find.byType(ListView).first, const Offset(0, 5000));
    await t.pumpAndSettle();
    await _entry(t);
    expect(
      second.sent
          .where((r) => r.url.path == '/v1/me/agent-profile-visibility')
          .single
          .url
          .host,
      'new-audience.test',
    );
    expect(first.closed, false);
    expect(second.closed, false);
    expect(t.takeException(), null);
    await t.pumpWidget(const SizedBox());
  });
}
