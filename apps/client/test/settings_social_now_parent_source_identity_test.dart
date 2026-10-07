import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:birdtie_client/src/workspace/social_now_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'now_scope_recovery_test.dart';
import 'social_now_controller_test.dart'
    show nowTie, nowIntent, nowResponse, nowOwner;

class _SocialWire extends http.BaseClient {
  _SocialWire(this.label);
  final String label;
  final dispatch = <Map<String, String>>[];
  Completer<http.Response>? pendingTies;
  bool refreshed = false;
  int closes = 0;

  bool _social(String path) => const [
    '/v1/me/ties',
    '/v1/social-intents',
    '/v1/me/opportunities',
  ].contains(path);
  int get socialGets => dispatch.where((r) => _social(r['path']!)).length;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    // This is the actual synchronous Client.send boundary, not an async mock
    // handler. Headers are synthetic unit credentials only.
    dispatch.add({
      'method': request.method,
      'host': request.url.host,
      'path': request.url.path,
      'authorization': request.headers['Authorization'] ?? '',
    });
    final path = request.url.path;
    final Future<http.Response> response;
    if (path == '/v1/me/ties') {
      response = pendingTies?.future ?? Future.value(nowResponse([nowTie()]));
    } else if (path == '/v1/social-intents') {
      response = Future.value(
        nowResponse([
          {...nowIntent(30), 'title': '$label${refreshed ? '迟到刷新' : '当前'}好友意图'},
        ]),
      );
    } else if (path == '/v1/me/opportunities') {
      response = Future.value(nowResponse([], opportunities: true));
    } else {
      response = Future.value(nowResponse([]));
    }
    return response.then(
      (r) => http.StreamedResponse(
        Stream.value(r.bodyBytes),
        r.statusCode,
        headers: r.headers,
        request: request,
      ),
    );
  }

  @override
  void close() {
    closes++;
  }
}

void main() {
  testWidgets('设置真实社交入口在父来源更换后丢弃旧刷新，当前新来源重新打开可读', (t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final f = NowFixture();
    f.auth.token = 'Bearer synthetic-settings-social-parent-unit';
    f.auth.owner = nowOwner;
    final a = _SocialWire('甲来源'), b = _SocialWire('乙来源');
    final source = ValueNotifier<(_SocialWire, String)>((
      a,
      'http://social-a.test',
    ));
    const settingsKey = ValueKey('original-settings-parent');
    final entry = find.widgetWithText(ListTile, '我的社交近况');
    Future<void> showEntry() async {
      await t.scrollUntilVisible(
        entry,
        120,
        maxScrolls: 60,
        scrollable: find.byType(Scrollable).last,
      );
      await t.ensureVisible(entry);
      await t.pumpAndSettle();
      expect(entry.hitTestable(), findsOneWidget);
    }

    try {
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<(_SocialWire, String)>(
            valueListenable: source,
            builder: (_, value, _) => Scaffold(
              body: SettingsPage(
                key: settingsKey,
                city: f.city,
                auth: f.auth,
                moments: f.moments,
                client: value.$1,
                apiBaseUrl: value.$2,
              ),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      await showEntry();
      await t.tap(entry);
      await t.pumpAndSettle();
      expect(find.byType(SocialNowPage), findsOneWidget);
      expect(find.text('甲来源当前好友意图'), findsOneWidget);
      expect(a.socialGets, 3);
      expect(a.dispatch.every((r) => r['method'] == 'GET'), true);

      a.refreshed = true;
      final late = Completer<http.Response>();
      a.pendingTies = late;
      final refresh = find.byTooltip('刷新社交近况');
      expect(refresh.hitTestable(), findsOneWidget);
      await t.tap(refresh);
      await t.pump();
      expect(a.socialGets, 6);
      source.value = (b, 'http://social-b.test');
      await t.pump();
      final presentBeforeLate = find
          .byType(SocialNowPage)
          .evaluate()
          .isNotEmpty;
      final currentSettings = t.widget<SettingsPage>(
        find.byKey(settingsKey, skipOffstage: false),
      );
      expect(currentSettings.client, same(b));
      expect(currentSettings.apiBaseUrl, 'http://social-b.test');
      late.complete(nowResponse([nowTie()]));
      await t.pumpAndSettle();
      debugPrintSynchronously((
        jsonEncode({
          'scenario': 'original-settings-social-parent-replacement',
          'sameSyntheticTokenOwnerAndNoOrganization': true,
          'pagePresentAtParentUpdateBeforeLate': presentBeforeLate,
          'oldLateVisible': find.text('甲来源迟到刷新好友意图').evaluate().isNotEmpty,
          'oldGets': a.socialGets,
          'oldDispatch': a.dispatch,
          'newDispatchBeforeReopen': b.dispatch,
        })).toString());
      expect(
        find.text('甲来源迟到刷新好友意图'),
        findsNothing,
        reason: '设置父来源已退休，旧GET成功不能显示为当前近况',
      );
      expect(find.byType(SocialNowPage), findsNothing);
      expect(find.textContaining('来源已变化'), findsOneWidget);
      expect(find.byTooltip('刷新社交近况'), findsNothing);
      final oldDispatchCount = a.dispatch.length;

      await t.pageBack();
      await t.pumpAndSettle();
      await showEntry();
      await t.tap(entry);
      await t.pumpAndSettle();
      expect(find.text('乙来源当前好友意图'), findsOneWidget);
      expect(b.socialGets, 3);
      expect(a.dispatch.length, oldDispatchCount);
      expect(b.dispatch.every((r) => r['method'] == 'GET'), true);
      expect(
        b.dispatch
            .where(
              (r) => const [
                '/v1/me/ties',
                '/v1/social-intents',
                '/v1/me/opportunities',
              ].contains(r['path']),
            )
            .every(
              (r) =>
                  r['host'] == 'social-b.test' &&
                  r['authorization'] == f.auth.token,
            ),
        true,
      );
      expect(f.source.queries, isEmpty);
      expect(a.closes, 0);
      expect(b.closes, 0);
      expect(t.takeException(), isNull);
      debugPrintSynchronously((
        jsonEncode({
          'scenario': 'fresh-settings-current-source-reopen',
          'newSocialGets': b.socialGets,
          'newDispatch': b.dispatch,
          'oldSourceNoFurtherDispatch': a.dispatch.length == oldDispatchCount,
          'agentQueries': f.source.queries.length,
          'borrowedClientsNotClosed': true,
        })).toString());
    } finally {
      await f.unmount(t);
      f.dispose();
      source.dispose();
      a.close();
      b.close();
    }
  });
}
