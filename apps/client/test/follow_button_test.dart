import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/follow_button.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('关注与取消关注只写入明确的主体目标', (tester) async {
    const targetID = '11111111-1111-4111-8111-111111111111';
    var followed = false;
    final client = MockClient((request) async {
      expect(request.headers['Authorization'], 'Bearer person');
      if (request.method == 'GET') {
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': followed
                  ? [
                      {
                        'target': {
                          'targetType': 'ORGANIZATION',
                          'targetId': targetID,
                        },
                      },
                    ]
                  : [],
            }),
          ),
          200,
        );
      }
      if (request.method == 'POST') {
        expect(jsonDecode(request.body), {
          'targetType': 'ORGANIZATION',
          'targetId': targetID,
        });
        followed = true;
        return http.Response('{"data":{"id":"follow-1"}}', 201);
      }
      expect(request.method, 'DELETE');
      expect(request.url.path, '/v1/me/follows/ORGANIZATION/$targetID');
      followed = false;
      return http.Response('', 204);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: FollowButton(
            targetType: 'ORGANIZATION',
            targetID: targetID,
            authorizationHeader: () => 'Bearer person',
            apiBaseUrl: 'https://api.test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsOneWidget);
    await tester.tap(find.text('关注'));
    await tester.pumpAndSettle();
    expect(find.text('已关注'), findsOneWidget);
    await tester.tap(find.text('已关注'));
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsOneWidget);
    expect(followed, isFalse);
    client.close();
  });

  testWidgets('未登录时不显示关注操作', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: FollowButton(
            targetType: 'PERSON',
            targetID: '11111111-1111-4111-8111-111111111111',
            authorizationHeader: _noSession,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsNothing);
  });

  testWidgets('切换账号时不会展示上一账号的关注状态', (tester) async {
    var token = 'Bearer first';
    final client = MockClient((request) async {
      final followed = request.headers['Authorization'] == 'Bearer first';
      return http.Response(
        jsonEncode({
          'data': followed
              ? [
                  {
                    'target': {
                      'targetType': 'PERSON',
                      'targetId': '11111111-1111-4111-8111-111111111111',
                    },
                  },
                ]
              : [],
        }),
        200,
      );
    });
    Widget screen() => MaterialApp(
      home: Scaffold(
        body: FollowButton(
          targetType: 'PERSON',
          targetID: '11111111-1111-4111-8111-111111111111',
          authorizationHeader: () => token,
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpWidget(screen());
    await tester.pumpAndSettle();
    expect(find.text('已关注'), findsOneWidget);
    token = 'Bearer second';
    await tester.pumpWidget(screen());
    await tester.pumpAndSettle();
    expect(find.text('已关注'), findsNothing);
    expect(find.text('关注'), findsOneWidget);
    client.close();
  });

  testWidgets('迟到关注回执不能翻转另一个目标的状态', (tester) async {
    const first = '11111111-1111-4111-8111-111111111111';
    const second = '22222222-2222-4222-8222-222222222222';
    final write = Completer<http.Response>();
    final client = MockClient((request) async {
      if (request.method == 'POST') return write.future;
      return http.Response('{"data":[]}', 200);
    });
    Widget screen(String target) => MaterialApp(
      home: Scaffold(
        body: FollowButton(
          targetType: 'PERSON',
          targetID: target,
          authorizationHeader: () => 'Bearer same',
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpWidget(screen(first));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pump();
    await tester.pumpWidget(screen(second));
    await tester.pumpAndSettle();
    expect(
      tester.widget<TextButton>(find.byType(TextButton)).onPressed,
      isNotNull,
    );
    write.complete(http.Response('{"data":{"id":"follow-first"}}', 201));
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    expect(tester.takeException(), isNull);
    client.close();
  });

  testWidgets('换 transport 后读取新来源而非初始 client', (tester) async {
    const target = '11111111-1111-4111-8111-111111111111';
    var secondReads = 0;
    final firstClient = MockClient(
      (request) async => http.Response(
        jsonEncode({
          'data': [
            {
              'target': {'targetType': 'PERSON', 'targetId': target},
            },
          ],
        }),
        200,
      ),
    );
    final secondClient = MockClient((request) async {
      secondReads++;
      return http.Response('{"data":[]}', 200);
    });
    Widget screen(http.Client client, String base) => MaterialApp(
      home: Scaffold(
        body: FollowButton(
          targetType: 'PERSON',
          targetID: target,
          authorizationHeader: () => 'Bearer same',
          apiBaseUrl: base,
          client: client,
        ),
      ),
    );
    await tester.pumpWidget(screen(firstClient, 'https://first.test'));
    await tester.pumpAndSettle();
    expect(find.text('已关注'), findsOneWidget);
    await tester.pumpWidget(screen(secondClient, 'https://second.test'));
    await tester.pumpAndSettle();
    expect(secondReads, 1);
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    firstClient.close();
    secondClient.close();
  });

  testWidgets('成功回执后显示重新读取的关系而非盲翻', (tester) async {
    var reads = 0, writes = 0;
    final client = MockClient((request) async {
      if (request.method == 'GET') {
        reads++;
        return http.Response('{"data":[]}', 200);
      }
      writes++;
      return http.Response('{"data":{"id":"follow"}}', 201);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: FollowButton(
            targetType: 'PERSON',
            targetID: '11111111-1111-4111-8111-111111111111',
            authorizationHeader: () => 'Bearer same',
            apiBaseUrl: 'https://api.test',
            client: client,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(reads, 2);
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    client.close();
  });

  testWidgets('退休目标的迟到写错误不在新目标显示提示', (tester) async {
    final write = Completer<http.Response>();
    final client = MockClient(
      (request) async => request.method == 'POST'
          ? write.future
          : http.Response('{"data":[]}', 200),
    );
    Widget screen(String id) => MaterialApp(
      home: Scaffold(
        body: FollowButton(
          targetType: 'PERSON',
          targetID: id,
          authorizationHeader: () => 'Bearer same',
          apiBaseUrl: 'https://api.test',
          client: client,
        ),
      ),
    );
    await tester.pumpWidget(screen('11111111-1111-4111-8111-111111111111'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pump();
    await tester.pumpWidget(screen('22222222-2222-4222-8222-222222222222'));
    await tester.pumpAndSettle();
    write.complete(http.Response('', 500));
    await tester.pumpAndSettle();
    expect(find.byType(SnackBar), findsNothing);
    expect(find.text('关注'), findsOneWidget);
    client.close();
  });

  testWidgets('真实身份通知拒绝 A B A 的旧关注回执', (tester) async {
    final identity = _FollowIdentity();
    final write = Completer<http.Response>();
    var writes = 0;
    final client = MockClient((request) async {
      if (request.method == 'POST') {
        writes++;
        return write.future;
      }
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pump();
    identity.token = 'Bearer B';
    identity.owner = 'owner-B';
    identity.changed();
    await tester.pumpAndSettle();
    identity.token = 'Bearer A';
    identity.owner = 'owner-A';
    identity.changed();
    await tester.pumpAndSettle();
    write.complete(http.Response('{"data":{"id":"old-A"}}', 201));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('同 token 的 owner ABA 通知也退休旧回执', (tester) async {
    final identity = _FollowIdentity();
    final write = Completer<http.Response>();
    final client = MockClient(
      (request) async => request.method == 'POST'
          ? write.future
          : http.Response('{"data":[]}', 200),
    );
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pump();
    identity.owner = 'owner-B';
    identity.changed();
    await tester.pumpAndSettle();
    identity.owner = 'owner-A';
    identity.changed();
    await tester.pumpAndSettle();
    write.complete(http.Response('', 201));
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('同 token 工作区 ABA 组织期间零个人请求', (tester) async {
    final identity = _FollowIdentity();
    final write = Completer<http.Response>();
    var gets = 0, writes = 0;
    final client = MockClient((request) async {
      if (request.method == 'POST') {
        writes++;
        return write.future;
      }
      gets++;
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pump();
    final before = gets;
    identity.workspace = 'organization-1';
    identity.changed();
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsNothing);
    expect(gets, before);
    expect(writes, 1);
    identity.workspace = null;
    identity.changed();
    await tester.pumpAndSettle();
    write.complete(http.Response('', 201));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('初始组织工作区不读取或写入个人关注', (tester) async {
    final identity = _FollowIdentity()..workspace = 'organization-1';
    var calls = 0;
    final client = MockClient((request) async {
      calls++;
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    expect(calls, 0);
    expect(find.text('关注'), findsNothing);
    expect(find.text('重新读取关注状态'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('旧写 finally 不解锁另一个目标正在提交的写入', (tester) async {
    final identity = _FollowIdentity();
    final first = Completer<http.Response>(),
        second = Completer<http.Response>();
    var writes = 0;
    final client = MockClient((request) async {
      if (request.method == 'POST') {
        writes++;
        return writes == 1 ? first.future : second.future;
      }
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pump();
    await tester.pumpWidget(
      _followScreen(
        identity: identity,
        client: client,
        target: '22222222-2222-4222-8222-222222222222',
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pump();
    expect(writes, 2);
    expect(
      tester.widget<TextButton>(find.byType(TextButton)).onPressed,
      isNull,
    );
    first.complete(http.Response('', 201));
    await tester.pumpAndSettle();
    expect(
      tester.widget<TextButton>(find.byType(TextButton)).onPressed,
      isNull,
    );
    second.complete(http.Response('', 201));
    await tester.pumpAndSettle();
    expect(
      tester.widget<TextButton>(find.byType(TextButton)).onPressed,
      isNotNull,
    );
    expect(writes, 2);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('迟到取消关注不会翻转新目标的未关注状态', (tester) async {
    final identity = _FollowIdentity();
    final remove = Completer<http.Response>();
    var deletes = 0;
    const first = '11111111-1111-4111-8111-111111111111';
    final client = MockClient((request) async {
      if (request.method == 'DELETE') {
        deletes++;
        expect(request.url.path, '/v1/me/follows/PERSON/$first');
        return remove.future;
      }
      return http.Response(
        jsonEncode({
          'data': [
            {
              'target': {'targetType': 'PERSON', 'targetId': first},
            },
          ],
        }),
        200,
      );
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('已关注'));
    await tester.pump();
    await tester.pumpWidget(
      _followScreen(
        identity: identity,
        client: client,
        target: '22222222-2222-4222-8222-222222222222',
      ),
    );
    await tester.pumpAndSettle();
    remove.complete(http.Response('', 204));
    await tester.pumpAndSettle();
    expect(deletes, 1);
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('UNKNOWN 仅 GET 当前关系且不自动重发或假称原操作成功', (tester) async {
    final identity = _FollowIdentity();
    var reads = 0, writes = 0;
    final client = MockClient((request) async {
      expect(request.url.path, '/v1/me/follows');
      if (request.method == 'POST') {
        writes++;
        throw TimeoutException('simulated lost receipt');
      }
      expect(request.method, 'GET');
      reads++;
      return http.Response(
        jsonEncode({
          'data': reads == 1
              ? []
              : [
                  {
                    'target': {
                      'targetType': 'PERSON',
                      'targetId': '11111111-1111-4111-8111-111111111111',
                    },
                  },
                ],
        }),
        200,
      );
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(reads, 2);
    expect(find.text('关注'), findsNothing);
    expect(find.text('关注操作结果未核实。仅重新读取当前关系，不会自动重发。'), findsOneWidget);
    expect(find.text('当前关系：已关注（重新读取）'), findsOneWidget);
    await tester.tap(find.text('重新读取关注状态'));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(reads, 3);
    expect(find.text('关注操作结果未核实。仅重新读取当前关系，不会自动重发。'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('已知成功回执读回失败后手动 GET 可恢复已核实关系', (tester) async {
    final identity = _FollowIdentity();
    var reads = 0, writes = 0;
    final client = MockClient((request) async {
      if (request.method == 'POST') {
        writes++;
        return http.Response('', 201);
      }
      reads++;
      if (reads == 2) return http.Response('', 503);
      return http.Response(
        jsonEncode({
          'data': reads == 1
              ? []
              : [
                  {
                    'target': {
                      'targetType': 'PERSON',
                      'targetId': '11111111-1111-4111-8111-111111111111',
                    },
                  },
                ],
        }),
        200,
      );
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pumpAndSettle();
    expect(find.text('已收到操作回执，但当前关系未核实。请重新读取，不会自动重发。'), findsOneWidget);
    await tester.tap(find.text('重新读取关注状态'));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(reads, 3);
    expect(find.text('已关注'), findsOneWidget);
    expect(find.text('重新读取关注状态'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('来源退休后的旧 GET 不恢复身份 ABA 前的状态', (tester) async {
    final identity = _FollowIdentity();
    final old = Completer<http.Response>();
    var reads = 0;
    final client = MockClient((request) async {
      reads++;
      return reads == 1 ? old.future : http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pump();
    identity.token = 'Bearer B';
    identity.changed();
    await tester.pumpAndSettle();
    identity.token = 'Bearer A';
    identity.changed();
    await tester.pumpAndSettle();
    old.complete(
      http.Response(
        jsonEncode({
          'data': [
            {
              'target': {
                'targetType': 'PERSON',
                'targetId': '11111111-1111-4111-8111-111111111111',
              },
            },
          ],
        }),
        200,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('已关注'), findsNothing);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('更换身份通知源移除旧 listener 并保留借用 client', (tester) async {
    final first = _FollowIdentity(), second = _FollowIdentity();
    var reads = 0;
    final client = _TrackedFollowClient((request) async {
      reads++;
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(_followScreen(identity: first, client: client));
    await tester.pumpAndSettle();
    await tester.pumpWidget(_followScreen(identity: second, client: client));
    await tester.pumpAndSettle();
    final after = reads;
    first.changed();
    await tester.pumpAndSettle();
    expect(reads, after);
    expect(client.closed, isFalse);
    await tester.pumpWidget(const SizedBox());
    second.changed();
    await tester.pump();
    expect(reads, after);
    expect(client.closed, isFalse);
    first.dispose();
    second.dispose();
    client.close();
  });

  testWidgets('借用 client 换成自有 client 后不会关闭原借用对象', (tester) async {
    final identity = _FollowIdentity()..token = null;
    final client = _TrackedFollowClient(
      (request) async => http.Response('{"data":[]}', 200),
    );
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.pumpWidget(_followScreen(identity: identity, client: null));
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox());
    expect(client.closed, isFalse);
    identity.dispose();
    client.close();
  });

  testWidgets('来源不通知但重建时 owner 改变也退休旧写', (tester) async {
    final identity = _FollowIdentity();
    final write = Completer<http.Response>();
    final client = MockClient(
      (request) async => request.method == 'POST'
          ? write.future
          : http.Response('{"data":[]}', 200),
    );
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pump();
    identity.owner = 'owner-B';
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    write.complete(http.Response('', 201));
    await tester.pumpAndSettle();
    expect(find.text('关注'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('实际十二秒超时后迟到回执不消除 UNKNOWN 或再提交', (tester) async {
    final identity = _FollowIdentity();
    final delayed = Completer<http.Response>();
    var reads = 0, writes = 0;
    final client = MockClient((request) async {
      if (request.method == 'POST') {
        writes++;
        return delayed.future;
      }
      reads++;
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pump();
    await tester.pump(const Duration(seconds: 13));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(reads, 2);
    expect(find.text('关注操作结果未核实。仅重新读取当前关系，不会自动重发。'), findsOneWidget);
    delayed.complete(http.Response('', 201));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(reads, 2);
    expect(find.text('关注'), findsNothing);
    expect(find.text('关注操作结果未核实。仅重新读取当前关系，不会自动重发。'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });

  testWidgets('未知结果中文在二倍字体窄屏可读且核实按钮达到四十八像素', (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = 2;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
    final identity = _FollowIdentity();
    var writes = 0;
    final client = MockClient((request) async {
      if (request.method == 'POST') {
        writes++;
        throw TimeoutException('lost receipt');
      }
      return http.Response('{"data":[]}', 200);
    });
    await tester.pumpWidget(_followScreen(identity: identity, client: client));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关注'));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(find.text('关注操作结果未核实。仅重新读取当前关系，不会自动重发。'), findsOneWidget);
    expect(
      tester.getSize(find.widgetWithText(TextButton, '重新读取关注状态')).height,
      greaterThanOrEqualTo(48),
    );
    await tester.tap(find.text('重新读取关注状态'));
    await tester.pumpAndSettle();
    expect(writes, 1);
    await tester.pumpWidget(const SizedBox());
    identity.dispose();
    client.close();
  });
}

String? _noSession() => null;

class _FollowIdentity extends ChangeNotifier {
  String? token = 'Bearer A', owner = 'owner-A', workspace;
  void changed() => notifyListeners();
}

class _TrackedFollowClient extends MockClient {
  _TrackedFollowClient(super.fn);
  bool closed = false;
  @override
  void close() {
    closed = true;
    super.close();
  }
}

Widget _followScreen({
  required _FollowIdentity identity,
  required http.Client? client,
  String target = '11111111-1111-4111-8111-111111111111',
  String base = 'https://api.test',
}) => MaterialApp(
  home: Scaffold(
    body: SingleChildScrollView(
      child: FollowButton(
        targetType: 'PERSON',
        targetID: target,
        authorizationHeader: () => identity.token,
        accountID: () => identity.owner,
        workspaceID: () => identity.workspace,
        identityChanges: identity,
        apiBaseUrl: base,
        client: client,
      ),
    ),
  ),
);
