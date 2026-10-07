import 'package:flutter/foundation.dart' show debugPrintSynchronously;
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
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
  testWidgets('Now真实社交入口在父来源更换后丢弃旧刷新，当前新来源重新打开可读', (t) async {
    t.view.physicalSize = const Size(390, 844);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final f = NowFixture();
    f.auth.token = 'Bearer synthetic-social-parent-unit';
    f.auth.owner = nowOwner;
    final a = _SocialWire('甲来源'), b = _SocialWire('乙来源');
    final source = ValueNotifier<(_SocialWire, String)>((
      a,
      'http://social-a.test',
    ));
    const mapKey = ValueKey('original-now-parent');
    try {
      await t.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<(_SocialWire, String)>(
            valueListenable: source,
            builder: (_, value, _) => MapWorkspace(
              key: mapKey,
              city: f.city,
              auth: f.auth,
              moments: f.moments,
              agentTaskSource: f.source,
              seedClient: value.$1,
              seedApiBaseUrl: value.$2,
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      final entry = find.widgetWithText(TextButton, '我的社交近况');
      expect(entry.hitTestable(), findsOneWidget);
      expect(f.workspace(t).task, isNull);
      expect(f.workspace(t).selectedEntityId, isNull);
      await t.tap(entry);
      await t.pumpAndSettle();
      expect(find.byType(SocialNowPage), findsOneWidget);
      expect(find.text('甲来源当前好友意图'), findsOneWidget);
      expect(
        a.socialGets,
        3,
      ); // Actual valid ties + intents + rule opportunities.
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
      await t.pump(); // Real same-key parent update; no child callback invoked.
      final pagePresentBeforeLate = find
          .byType(SocialNowPage)
          .evaluate()
          .isNotEmpty;
      expect(
        t
            .widget<MapWorkspace>(find.byKey(mapKey, skipOffstage: false))
            .seedClient,
        same(b),
      );
      late.complete(nowResponse([nowTie()]));
      await t.pumpAndSettle();
      debugPrintSynchronously((
        jsonEncode({
          'scenario': 'original-now-social-parent-replacement',
          'sameSyntheticTokenOwnerAndNoOrganization': true,
          'pagePresentAtParentUpdateBeforeLate': pagePresentBeforeLate,
          'oldLateVisible': find.text('甲来源迟到刷新好友意图').evaluate().isNotEmpty,
          'oldGets': a.socialGets,
          'oldDispatch': a.dispatch,
          'newDispatchBeforeReopen': b.dispatch,
        })).toString());
      expect(
        find.text('甲来源迟到刷新好友意图'),
        findsNothing,
        reason: '父来源已退休，旧GET成功不得重新显示旧来源内容',
      );
      expect(find.byType(SocialNowPage), findsNothing);
      expect(find.textContaining('来源已变化'), findsOneWidget);
      expect(find.byTooltip('刷新社交近况'), findsNothing);
      final oldDispatchCount = a.dispatch.length;

      await t.pageBack(); // Standard outer back from the retired destination.
      await t.pumpAndSettle();
      expect(entry.hitTestable(), findsOneWidget);
      expect(t.widget<TextField>(nowField()).focusNode!.hasFocus, false);
      expect(t.testTextInput.isVisible, false);
      expect(f.workspace(t).task, isNull);
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
          'scenario': 'fresh-current-source-reopen',
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
