import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/inbox.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'inbox_panel_test.dart' show signedInAuth;

const noticeID = '11111111-1111-4111-8111-111111111111';
const oldActivityID = '22222222-2222-4222-8222-222222222222';
const currentActivityID = '33333333-3333-4333-8333-333333333333';
Map<String, dynamic> pushNotice({String target = oldActivityID}) => {
  'id': noticeID,
  'category': 'updates',
  'title': '活动更新',
  'detail': '打开查看',
  'resourceType': 'activity_change',
  'resourceId': target,
  'targetActivityId': target,
  'createdAt': '2026-10-13T00:00:00Z',
};
http.Response inboxResponse(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
http.Response inboxPageResponse(List<Object> items) => inboxResponse({
  'items': items,
  'nextCursor': null,
  'unreadCount': items.length,
});

void main() {
  testWidgets('系统打开用原Inbox ID标记已读并采用服务端当前对象', (t) async {
    final auth = await t.runAsync(signedInAuth);
    expect(auth!.signedIn, isTrue);
    final opened = Completer<String>();
    final paths = <String>[];
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://fixture',
      client: MockClient((r) async {
        paths.add('${r.method} ${r.url.path}');
        if (r.method == 'POST')
          return inboxResponse({
            ...pushNotice(target: currentActivityID),
            'readAt': '2026-10-13T00:00:01Z',
          });
        return inboxPageResponse([pushNotice()]);
      }),
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(
            auth: auth,
            source: source,
            notificationsOnly: true,
            initialItem: InboxItem.fromJson(pushNotice()),
            onOpenActivity: opened.complete,
          ),
        ),
      ),
    );
    await t.runAsync(() async {
      expect(
        await opened.future.timeout(const Duration(seconds: 5)),
        currentActivityID,
      );
    });
    await t.pumpAndSettle();
    expect(paths.where((p) => p.startsWith('POST ')), [
      'POST /v1/me/inbox/$noticeID/read',
    ]);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
  });

  testWidgets('初始通知加载期间组织ABA永久退休，迟到结果不能标记或打开', (t) async {
    final auth = await t.runAsync(signedInAuth);
    expect(auth!.signedIn, isTrue);
    final workspace = ValueNotifier<String?>(null);
    final firstRead = Completer<http.Response>();
    final requested = Completer<void>();
    var reads = 0, writes = 0, opens = 0;
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://fixture',
      client: MockClient((r) async {
        if (r.method == 'POST') {
          writes++;
          return inboxResponse(pushNotice());
        }
        if (++reads == 1) {
          requested.complete();
          return firstRead.future;
        }
        return inboxPageResponse(<Object>[]);
      }),
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(
            auth: auth,
            source: source,
            notificationsOnly: true,
            initialItem: InboxItem.fromJson(pushNotice()),
            workspaceChanges: workspace,
            organizationWorkspaceID: () => workspace.value,
            onOpenActivity: (_) => opens++,
          ),
        ),
      ),
    );
    await t.runAsync(() async {
      await requested.future.timeout(const Duration(seconds: 5));
      workspace.value = 'organization';
      workspace.value = null;
      firstRead.complete(inboxPageResponse([pushNotice()]));
      await Future<void>.delayed(const Duration(milliseconds: 30));
    });
    await t.pumpAndSettle();
    expect(writes, 0);
    expect(opens, 0);
    expect(reads, 2);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
    workspace.dispose();
  });

  testWidgets('前台系统事件只刷新当前Inbox，不隐式标记或导航', (t) async {
    final auth = await t.runAsync(signedInAuth);
    expect(auth!.signedIn, isTrue);
    final refresh = ValueNotifier<int>(0);
    var reads = 0, writes = 0;
    final second = Completer<void>();
    final first = Completer<void>();
    final source = RemoteInboxSource(
      authorizationHeader: () => auth.authorizationHeader,
      apiBaseUrl: 'https://fixture',
      client: MockClient((r) async {
        if (r.method == 'POST') writes++;
        if (++reads == 1) first.complete();
        if (reads == 2) second.complete();
        return inboxPageResponse(<Object>[]);
      }),
    );
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InboxPanel(
            auth: auth,
            source: source,
            notificationsOnly: true,
            refreshChanges: refresh,
          ),
        ),
      ),
    );
    await t.runAsync(() async {
      await first.future.timeout(const Duration(seconds: 5));
    });
    await t.pumpAndSettle();
    expect(find.byType(CircularProgressIndicator), findsNothing);
    refresh.value++;
    await t.pump();
    await t.runAsync(() async {
      await second.future.timeout(const Duration(seconds: 5));
    });
    await t.pumpAndSettle();
    expect(reads, 2);
    expect(writes, 0);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    auth.dispose();
    refresh.dispose();
  });
}
