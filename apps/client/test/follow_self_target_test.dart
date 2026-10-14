import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/follow_button.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _self = '11111111-1111-4111-8111-111111111111';
const _other = '22222222-2222-4222-8222-222222222222';

class _Identity extends ChangeNotifier {
  String? owner = _self, workspace;
  void changeOwner(String? next) {
    owner = next;
    notifyListeners();
  }
}

Widget _screen(
  _Identity identity,
  http.Client client, {
  String kind = 'PERSON',
  String target = _self,
}) => MaterialApp(
  home: Scaffold(
    body: FollowButton(
      targetType: kind,
      targetID: target,
      authorizationHeader: () => 'Bearer isolated-widget',
      accountID: () => identity.owner,
      workspaceID: () => identity.workspace,
      identityChanges: identity,
      apiBaseUrl: 'http://widget.test',
      client: client,
    ),
  ),
);

http.Response _empty() => http.Response('{"data":[]}', 200);

void main() {
  testWidgets('已知本人 PERSON 不读取关注或呈现关系动作', (t) async {
    final identity = _Identity();
    var requests = 0;
    final client = MockClient((r) async {
      requests++;
      return _empty();
    });
    await t.pumpWidget(_screen(identity, client));
    await t.pumpAndSettle();
    expect(requests, 0);
    expect(find.text('关注'), findsNothing);
    expect(find.text('已关注'), findsNothing);
    expect(find.text('重新读取关注状态'), findsNothing);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('相同ID的 ORGANIZATION 不是本人 PERSON，保留领域权限路径', (t) async {
    final identity = _Identity();
    var reads = 0;
    final client = MockClient((r) async {
      expect(r.method, 'GET');
      reads++;
      return _empty();
    });
    await t.pumpWidget(_screen(identity, client, kind: 'ORGANIZATION'));
    await t.pumpAndSettle();
    expect(reads, 1);
    expect(find.text('关注'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('旧调用未提供owner仍使用原服务器领域权限，不虚构本人', (t) async {
    final identity = _Identity()..owner = null;
    var reads = 0;
    final client = MockClient((r) async {
      expect(r.method, 'GET');
      reads++;
      return _empty();
    });
    await t.pumpWidget(_screen(identity, client));
    await t.pumpAndSettle();
    expect(reads, 1);
    expect(find.text('关注'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('组织身份即使目标非本人也零个人关系请求', (t) async {
    final identity = _Identity()..workspace = 'organization-isolated';
    var requests = 0;
    final client = MockClient((r) async {
      requests++;
      return _empty();
    });
    await t.pumpWidget(_screen(identity, client, target: _other));
    await t.pumpAndSettle();
    expect(requests, 0);
    expect(find.byType(TextButton), findsNothing);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('同token从他人切成本人不再读取，旧按钮回调不能提交', (t) async {
    final identity = _Identity()..owner = _other;
    var reads = 0, writes = 0;
    final client = MockClient((r) async {
      if (r.method == 'GET') {
        reads++;
        return _empty();
      }
      writes++;
      return http.Response('', 201);
    });
    await t.pumpWidget(_screen(identity, client));
    await t.pumpAndSettle();
    final oldTap = t.widget<TextButton>(find.byType(TextButton)).onPressed!;
    identity.changeOwner(_self);
    await t.pumpAndSettle();
    expect(reads, 1);
    expect(find.text('关注'), findsNothing);
    oldTap();
    await t.pumpAndSettle();
    expect(writes, 0);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('他人旧GET迟到不能在本人页面复活关注', (t) async {
    final identity = _Identity()..owner = _other;
    final old = Completer<http.Response>();
    var reads = 0;
    final client = MockClient((r) async {
      reads++;
      return reads == 1 ? old.future : _empty();
    });
    await t.pumpWidget(_screen(identity, client));
    await t.pump();
    identity.changeOwner(_self);
    await t.pump();
    old.complete(
      http.Response(
        jsonEncode({
          'data': [
            {
              'target': {'targetType': 'PERSON', 'targetId': _self},
            },
          ],
        }),
        200,
      ),
    );
    await t.pumpAndSettle();
    expect(reads, 1);
    expect(find.text('关注'), findsNothing);
    expect(find.text('已关注'), findsNothing);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('他人→本人→他人 ABA 旧写回执不恢复旧资格', (t) async {
    final identity = _Identity()..owner = _other;
    final old = Completer<http.Response>();
    var reads = 0, writes = 0;
    final client = MockClient((r) async {
      if (r.method == 'GET') {
        reads++;
        return _empty();
      }
      writes++;
      return old.future;
    });
    await t.pumpWidget(_screen(identity, client));
    await t.pumpAndSettle();
    await t.tap(find.text('关注'));
    await t.pump();
    identity.changeOwner(_self);
    await t.pump();
    expect(reads, 1);
    identity.changeOwner(_other);
    await t.pumpAndSettle();
    old.complete(http.Response('', 201));
    await t.pumpAndSettle();
    expect(reads, 2);
    expect(writes, 1);
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    expect(find.text('操作已提交，正在读取当前关注状态。'), findsNothing);
    await t.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });
}
