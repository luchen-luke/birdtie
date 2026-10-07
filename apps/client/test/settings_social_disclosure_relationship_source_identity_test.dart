import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_relationship_page.dart';
import 'package:birdtie_client/src/workspace/social_disclosure_page.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'social_preference_seed_sheet_test.dart' show SocialTestAuth;

class _Wire extends http.BaseClient {
  final sent =
      <
        ({String method, String path, String host, String? token, String body})
      >[];
  Map<String, bool> disclosure = {
    'mutualTies': false,
    'sharedCommunities': false,
    'sharedActivities': false,
  };
  bool enabled = false, closed = false, loseDisclosureResponse = false;
  Completer<http.StreamedResponse>? pending;
  http.StreamedResponse response(Object value, [int status = 200]) =>
      http.StreamedResponse(
        Stream.value(utf8.encode(jsonEncode({'data': value}))),
        status,
      );
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    final body = request is http.Request ? request.body : '';
    final path = request.url.path;
    if (path == '/v1/me/social-disclosure' ||
        path == '/v1/me/agent-relationship-consent' ||
        path == '/v1/me/agent-relationship-context') {
      // Capture dispatch synchronously, not when an async MockClient handler runs.
      sent.add((
        method: request.method,
        path: path,
        host: request.url.host,
        token: request.headers['Authorization'],
        body: body,
      ));
      if (request.method == 'PUT') {
        if (pending != null) return pending!.future;
        if (path.endsWith('social-disclosure')) {
          disclosure = (jsonDecode(body) as Map<String, dynamic>)
              .cast<String, bool>();
          if (loseDisclosureResponse) {
            loseDisclosureResponse = false;
            return Future.error(
              http.ClientException('synthetic response loss'),
            );
          }
        } else {
          enabled =
              (jsonDecode(body) as Map<String, dynamic>)['enabled'] as bool;
        }
      }
      return Future.value(
        response(
          path.endsWith('social-disclosure')
              ? disclosure
              : path.endsWith('context')
              ? {'enabled': enabled, 'peers': <Object>[], 'truncated': false}
              : {'enabled': enabled},
        ),
      );
    }
    return Future.value(response(<Object>[]));
  }

  @override
  void close() {
    closed = true;
    super.close();
  }
}

class _Case {
  _Case() {
    addTearDown(dispose);
  }
  bool _closed = false;
  final auth = SocialTestAuth(), otherAuth = SocialTestAuth();
  final city = PublicCityController(), workspace = ValueNotifier<String?>(null);
  final wire = _Wire(), otherWire = _Wire();
  late final source = ValueNotifier<(SocialTestAuth, http.Client, String)>((
    auth,
    wire,
    'https://visibility-a.test',
  ));
  late final moments = PrivateMomentController(
    authorizationHeader: () => auth.authorizationHeader,
    client: wire,
    apiBaseUrl: 'https://visibility-a.test',
  );
  String? workspaceID() => workspace.value;
  Iterable<
    ({String method, String path, String host, String? token, String body})
  >
  get writes =>
      [...wire.sent, ...otherWire.sent].where((r) => r.method == 'PUT');
  void dispose() {
    if (_closed) return;
    _closed = true;
    moments.dispose();
    city.dispose();
    workspace.dispose();
    source.dispose();
    auth.dispose();
    otherAuth.dispose();
    wire.close();
    otherWire.close();
  }
}

Widget _home(_Case c) => MaterialApp(
  home: ValueListenableBuilder<(SocialTestAuth, http.Client, String)>(
    valueListenable: c.source,
    builder: (_, s, _) => Scaffold(
      body: SettingsPage(
        key: const ValueKey('same-settings'),
        auth: s.$1,
        client: s.$2,
        apiBaseUrl: s.$3,
        city: c.city,
        moments: c.moments,
        workspaceChanges: c.workspace,
        organizationWorkspaceID: c.workspaceID,
      ),
    ),
  ),
);
Finder _page(bool relationship) =>
    find.byType(relationship ? AgentRelationshipPage : SocialDisclosurePage);
Finder _switch(bool relationship) => find.widgetWithText(
  SwitchListTile,
  relationship ? '允许个人 Agent 使用关系信号' : '共同好友数量',
);
Future<void> _tap(WidgetTester t, Finder f) async {
  await t.ensureVisible(f);
  await t.pump();
  expect(f.hitTestable(), findsOneWidget);
  await t.tap(f);
  await t.pumpAndSettle();
}

Future<void> _open(WidgetTester t, bool relationship) async {
  final entry = find.widgetWithText(
    ListTile,
    relationship ? 'Agent 关系信号' : '共同信息展示',
  );
  await t.scrollUntilVisible(
    entry,
    180,
    maxScrolls: 50,
    scrollable: find.byType(Scrollable).last,
  );
  await _tap(t, entry);
  expect(_page(relationship), findsOneWidget);
  expect(_switch(relationship).hitTestable(), findsOneWidget);
  expect(t.widget<SwitchListTile>(_switch(relationship)).value, false);
  expect(t.widget<SwitchListTile>(_switch(relationship)).onChanged, isNotNull);
}

Future<void> _close(WidgetTester t, _Case c) async {
  await t.pumpWidget(const SizedBox());
  c.dispose();
}

void main() {
  for (final relationship in [false, true]) {
    final label = relationship ? '关系信号' : '共同信息';
    testWidgets('设置展示来源边界：$label 当前本人正常保存，新来源重新打开可操作', (t) async {
      final c = _Case();
      await t.pumpWidget(_home(c));
      await t.pumpAndSettle();
      await _open(t, relationship);
      await _tap(t, _switch(relationship));
      expect(c.writes.length, 1);
      expect(c.writes.single.host, 'visibility-a.test');
      expect(c.writes.single.token, 'Bearer owner');
      expect(
        jsonDecode(c.writes.single.body),
        relationship
            ? {'enabled': true}
            : {
                'mutualTies': true,
                'sharedCommunities': false,
                'sharedActivities': false,
              },
      );
      expect(t.widget<SwitchListTile>(_switch(relationship)).value, true);
      await t.pageBack();
      await t.pumpAndSettle();
      c.source.value = (c.auth, c.otherWire, 'https://visibility-b.test');
      await t.pumpAndSettle();
      await _open(t, relationship);
      await _tap(t, _switch(relationship));
      expect(c.writes.length, 2);
      expect(c.writes.last.host, 'visibility-b.test');
      expect(c.wire.closed, false);
      expect(c.otherWire.closed, false);
      expect(t.takeException(), isNull);
      await _close(t, c);
    });

    for (final change in ['client', 'base', 'source-aba', 'auth-instance']) {
      testWidgets('设置展示来源边界：$label $change 旧开关不能写旧来源', (t) async {
        final c = _Case();
        await t.pumpWidget(_home(c));
        await t.pumpAndSettle();
        await _open(t, relationship);
        final original = c.source.value;
        c.source.value = switch (change) {
          'client' => (c.auth, c.otherWire, original.$3),
          'base' => (c.auth, c.wire, 'https://visibility-b.test'),
          'auth-instance' => (c.otherAuth, c.wire, original.$3),
          _ => (c.auth, c.otherWire, 'https://visibility-b.test'),
        };
        await t.pumpAndSettle();
        if (change == 'source-aba') {
          c.source.value = original;
          await t.pumpAndSettle();
        }
        // A retired boundary may remove the control. If still rendered, use its real hit target.
        if (_switch(relationship).evaluate().isNotEmpty) {
          await _tap(t, _switch(relationship));
        }
        expect(
          c.writes,
          isEmpty,
          reason: 'Original Settings route must not dispatch an obsolete PUT.',
        );
        expect(_page(relationship), findsNothing);
        expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
        expect(t.takeException(), isNull);
        await _close(t, c);
      });
    }

    testWidgets('设置展示来源边界：$label 组织ABA下一帧前旧可见开关零PUT', (t) async {
      final c = _Case();
      await t.pumpWidget(_home(c));
      await t.pumpAndSettle();
      await _open(t, relationship);
      final target = _switch(relationship);
      expect(target.hitTestable(), findsOneWidget);
      c.workspace.value = 'synthetic-org';
      c.workspace.value = null;
      // No pump: old render is still visible, but entry identity was synchronously retired.
      expect(target.hitTestable(), findsOneWidget);
      await t.tap(target);
      expect(c.writes, isEmpty);
      await t.pumpAndSettle();
      expect(_page(relationship), findsNothing);
      expect(t.takeException(), isNull);
      await _close(t, c);
    });

    for (final status in [200, 503]) {
      testWidgets('设置展示来源边界：$label 已发PUT晚$status不恢复旧入口或读取旧信号', (t) async {
        final c = _Case();
        await t.pumpWidget(_home(c));
        await t.pumpAndSettle();
        await _open(t, relationship);
        c.wire.pending = Completer<http.StreamedResponse>();
        await t.tap(_switch(relationship));
        await t.pump();
        expect(c.writes.length, 1);
        expect(c.writes.single.host, 'visibility-a.test');
        c.source.value = (c.auth, c.otherWire, 'https://visibility-b.test');
        await t.pump();
        await t.pump(const Duration(milliseconds: 350));
        c.wire.pending!.complete(
          c.wire.response(
            relationship
                ? {'enabled': true}
                : {
                    'mutualTies': true,
                    'sharedCommunities': false,
                    'sharedActivities': false,
                  },
            status,
          ),
        );
        await t.pumpAndSettle();
        expect(
          c.writes.length,
          1,
          reason: 'Already dispatched A write is not claimed to be cancelled.',
        );
        expect(_page(relationship), findsNothing);
        expect(
          c.wire.sent.where(
            (r) => r.path.endsWith('agent-relationship-context'),
          ),
          isEmpty,
        );
        expect(find.textContaining('保存结果暂未确认'), findsNothing);
        expect(t.takeException(), isNull);
        await _close(t, c);
      });
    }
  }

  testWidgets('设置共同信息响应丢失：不声称未更改，人工刷新仅GET核当前值', (t) async {
    final c = _Case();
    await t.pumpWidget(_home(c));
    await t.pumpAndSettle();
    await _open(t, false);
    c.wire.loseDisclosureResponse = true;
    await _tap(t, _switch(false));
    expect(c.writes.length, 1);
    expect(c.wire.disclosure['mutualTies'], true);
    expect(find.text('保存结果暂未确认，请刷新核对当前设置。'), findsOneWidget);
    expect(find.textContaining('设置未更改'), findsNothing);
    expect(t.widget<SwitchListTile>(_switch(false)).value, false);
    final before = c.wire.sent.length;
    await _tap(t, find.text('保存结果暂未确认，请刷新核对当前设置。'));
    expect(c.wire.sent.length, before + 1);
    expect(c.wire.sent.last.method, 'GET');
    expect(c.writes.length, 1);
    expect(t.widget<SwitchListTile>(_switch(false)).value, true);
    expect(find.text('保存结果暂未确认，请刷新核对当前设置。'), findsNothing);
    expect(t.takeException(), isNull);
    await _close(t, c);
  });
}
