import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/public_person_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _self = '11111111-1111-4111-8111-111111111111';
const _other = '22222222-2222-4222-8222-222222222222';

class _Auth extends BirdtieAuthController {
  String? token = 'Bearer isolated-widget';
  String owner = _self;
  @override
  String? get authorizationHeader => token;
  @override
  String? get accountID => token == null ? null : owner;
  @override
  bool get signedIn => token != null;
  void changeOwner(String value) {
    owner = value;
    notifyListeners();
  }
}

http.Response _profile(
  String id, {
  String visibility = 'public',
  String bio = '已允许的简介',
}) => http.Response.bytes(
  utf8.encode(
    jsonEncode({
      'data': {
        'accountId': id,
        'displayName': '隔离组件资料',
        'bio': bio,
        'visibility': visibility,
      },
    }),
  ),
  200,
);

Widget _screen(
  _Auth auth,
  http.Client client, {
  String target = _self,
  String? workspace,
  String base = 'http://widget.test',
  Future<void> Function()? openOwn,
}) => MaterialApp(
  home: PublicPersonPage(
    key: const ValueKey('same-person-page'),
    accountID: target,
    auth: auth,
    authorizationHeader: () => auth.authorizationHeader,
    workspaceID: () => workspace,
    apiBaseUrl: base,
    client: client,
    onOpenOwnProfile: openOwn,
  ),
);

void main() {
  testWidgets('本人真实编辑入口达到48dp，返回后原GET读取新的私密资料', (t) async {
    final auth = _Auth();
    var reads = 0, opens = 0;
    final client = MockClient((r) async {
      expect(r.method, 'GET');
      expect(r.url.path, '/v1/accounts/$_self/profile');
      reads++;
      return _profile(
        _self,
        visibility: opens == 0 ? 'public' : 'private',
        bio: opens == 0 ? '编辑前公开简介' : '本人修改后的私密简介',
      );
    });
    Future<void> open() async {
      opens++;
    }

    await t.pumpWidget(_screen(auth, client, openOwn: open));
    await t.pumpAndSettle();
    final button = find.widgetWithText(TextButton, '编辑我的资料与隐私');
    expect(t.getSize(button).height, greaterThanOrEqualTo(48));
    await t.tap(button);
    await t.pumpAndSettle();
    expect(opens, 1);
    expect(reads, 2);
    expect(find.text('本人修改后的私密简介'), findsOneWidget);
    expect(find.text('编辑前公开简介'), findsNothing);
    expect(find.text('发给好友'), findsNothing, reason: '私密资料不继承之前的公开分享入口');
    expect(find.text('编辑我的资料与隐私'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('本人编辑回调未返回时两次显式tap只开一次，不自动提交任何HTTP写', (t) async {
    final auth = _Auth();
    final done = Completer<void>();
    var reads = 0, opens = 0;
    final client = MockClient((r) async {
      expect(r.method, 'GET');
      reads++;
      return _profile(_self);
    });
    Future<void> open() {
      opens++;
      return done.future;
    }

    await t.pumpWidget(_screen(auth, client, openOwn: open));
    await t.pumpAndSettle();
    final tap = t
        .widget<TextButton>(find.widgetWithText(TextButton, '编辑我的资料与隐私'))
        .onPressed!;
    final firstAction = (tap as Future<void> Function())();
    tap();
    await t.pump();
    expect(opens, 1);
    expect(
      t
          .widget<TextButton>(find.widgetWithText(TextButton, '编辑我的资料与隐私'))
          .onPressed,
      isNull,
    );
    done.complete();
    await firstAction;
    await t.pumpAndSettle();
    expect(opens, 1);
    expect(reads, 2);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('本人编辑返回后公开资料404真实显示不可访问，不复用旧简介', (t) async {
    final auth = _Auth();
    var reads = 0, opens = 0;
    final client = MockClient((r) async {
      expect(r.method, 'GET');
      reads++;
      return reads == 1
          ? _profile(_self, bio: '不能复活的旧简介')
          : http.Response('{"error":{"code":"not_found"}}', 404);
    });
    Future<void> open() async {
      opens++;
    }

    await t.pumpWidget(_screen(auth, client, openOwn: open));
    await t.pumpAndSettle();
    await t.tap(find.text('编辑我的资料与隐私'));
    await t.pumpAndSettle();
    expect(opens, 1);
    expect(reads, 2);
    expect(find.text('个人资料暂不可访问。 点击重试'), findsOneWidget);
    expect(find.text('不能复活的旧简介'), findsNothing);
    expect(find.text('编辑我的资料与隐私'), findsNothing);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('本人编辑回调失败不重开、不自动GET或写入', (t) async {
    final auth = _Auth();
    var reads = 0, opens = 0;
    final client = MockClient((r) async {
      expect(r.method, 'GET');
      reads++;
      return _profile(_self);
    });
    Future<void> open() async {
      opens++;
      throw StateError('isolated callback unavailable');
    }

    await t.pumpWidget(_screen(auth, client, openOwn: open));
    await t.pumpAndSettle();
    await t.tap(find.text('编辑我的资料与隐私'));
    await t.pumpAndSettle();
    expect(opens, 1);
    expect(reads, 1);
    expect(find.text('个人资料暂不可访问。 点击重试'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('本人旧编辑入口在来源ABA后永久退休，新入口仍能打开', (t) async {
    final auth = _Auth();
    var opens = 0;
    final client = MockClient(
      (r) async => r.url.path.endsWith('/profile')
          ? _profile(_self)
          : http.Response('{}', 404),
    );
    Future<void> open() async {
      opens++;
    }

    await t.pumpWidget(_screen(auth, client, openOwn: open));
    await t.pumpAndSettle();
    final old = t
        .widget<TextButton>(find.widgetWithText(TextButton, '编辑我的资料与隐私'))
        .onPressed!;
    await t.pumpWidget(
      _screen(auth, client, base: 'http://changed.widget', openOwn: open),
    );
    await t.pumpAndSettle();
    await t.pumpWidget(_screen(auth, client, openOwn: open));
    await t.pumpAndSettle();
    old();
    await t.pumpAndSettle();
    expect(opens, 0);
    await t.tap(find.text('编辑我的资料与隐私'));
    await t.pumpAndSettle();
    expect(opens, 1);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('本人编辑期间同token owner ABA，迟到返回不借新scope读取或恢复旧批准', (t) async {
    final auth = _Auth();
    final done = Completer<void>();
    var profileReads = 0, opens = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/profile')) {
        profileReads++;
        return _profile(_self);
      }
      return http.Response('{}', 404);
    });
    Future<void> open() {
      opens++;
      return done.future;
    }

    await t.pumpWidget(_screen(auth, client, openOwn: open));
    await t.pumpAndSettle();
    await t.tap(find.text('编辑我的资料与隐私'));
    await t.pump();
    auth.changeOwner(_other);
    await t.pumpAndSettle();
    auth.changeOwner(_self);
    await t.pumpAndSettle();
    final beforeReturn = profileReads;
    done.complete();
    await t.pumpAndSettle();
    expect(opens, 1);
    expect(beforeReturn, 3);
    expect(profileReads, beforeReturn);
    expect(find.text('关注'), findsNothing);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('本人公开主页仅读资料，不读取对自己的关系或显示连接私信', (t) async {
    final auth = _Auth();
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.url.path.endsWith('/profile')) return _profile(_self);
      if (r.url.path == '/v1/me/follows')
        return http.Response('{"data":[]}', 200);
      return http.Response('{}', 404);
    });
    await t.pumpWidget(_screen(auth, client));
    await t.pumpAndSettle();
    expect(requests.map((r) => r.url.path), ['/v1/accounts/$_self/profile']);
    expect(find.text('隔离组件资料'), findsOneWidget);
    expect(find.text('已允许的简介'), findsOneWidget);
    expect(find.text('关注'), findsNothing);
    expect(find.text('添加好友'), findsNothing);
    expect(find.text('打开私信'), findsNothing);
    expect(
      find.text('编辑我的资料与隐私'),
      findsNothing,
      reason: '没有真实编辑callback时不新增按钮',
    );
    expect(find.text('个人主页'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('他人公开主页保留原关注、连接、私信和分享且不自动写入', (t) async {
    final auth = _Auth();
    var writes = 0;
    final paths = <String>[];
    final client = MockClient((r) async {
      paths.add(r.url.path);
      if (r.method != 'GET') {
        writes++;
        return http.Response('{}', 400);
      }
      if (r.url.path.endsWith('/profile')) return _profile(_other);
      if (r.url.path == '/v1/me/follows')
        return http.Response('{"data":[]}', 200);
      return http.Response('{}', 404);
    });
    await t.pumpWidget(_screen(auth, client, target: _other));
    await t.pumpAndSettle();
    expect(find.text('关注'), findsOneWidget);
    expect(find.text('添加好友'), findsOneWidget);
    expect(find.text('打开私信'), findsOneWidget);
    expect(find.text('发给好友'), findsOneWidget);
    expect(find.text('编辑我的资料与隐私'), findsNothing);
    expect(paths, contains('/v1/me/follows'));
    expect(writes, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('组织范围保留公开资料，个人关系和好友分享控件及请求为零', (t) async {
    final auth = _Auth();
    final paths = <String>[];
    final client = MockClient((r) async {
      paths.add(r.url.path);
      return _profile(_other);
    });
    await t.pumpWidget(
      _screen(auth, client, target: _other, workspace: 'isolated-organization'),
    );
    await t.pumpAndSettle();
    expect(paths, ['/v1/accounts/$_other/profile']);
    for (final label in ['关注', '添加好友', '打开私信', '发给好友', '编辑我的资料与隐私']) {
      expect(find.text(label), findsNothing);
    }
    expect(find.text('隔离组件资料'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('同token owner变成本人，直接auth通知移除旧关系动作且不读self关系', (t) async {
    final auth = _Auth()..owner = _other;
    final paths = <String>[];
    var writes = 0;
    final client = MockClient((r) async {
      paths.add(r.url.path);
      if (r.method != 'GET') {
        writes++;
        return http.Response('{}', 400);
      }
      if (r.url.path.endsWith('/profile')) return _profile(_self);
      if (r.url.path == '/v1/me/follows')
        return http.Response('{"data":[]}', 200);
      return http.Response('{}', 404);
    });
    await t.pumpWidget(_screen(auth, client));
    await t.pumpAndSettle();
    final oldConnect = t
        .widget<OutlinedButton>(find.widgetWithText(OutlinedButton, '添加好友'))
        .onPressed!;
    final before = paths.length;
    auth.changeOwner(_self);
    await t.pumpAndSettle();
    expect(find.text('添加好友'), findsNothing);
    expect(find.text('打开私信'), findsNothing);
    expect(find.text('关注'), findsNothing);
    expect(paths.skip(before).toList(), ['/v1/accounts/$_self/profile']);
    oldConnect();
    await t.pumpAndSettle();
    expect(find.text('好友申请'), findsNothing);
    expect(writes, 0);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });

  testWidgets('同token owner ABA 后迟到资料不能覆盖新的授权读取', (t) async {
    final auth = _Auth();
    final first = Completer<http.Response>();
    var reads = 0;
    final client = MockClient((r) async {
      if (!r.url.path.endsWith('/profile')) return http.Response('{}', 404);
      reads++;
      return reads == 1 ? first.future : _profile(_self, bio: '当前合法简介');
    });
    await t.pumpWidget(_screen(auth, client));
    await t.pump();
    auth.changeOwner(_other);
    await t.pumpAndSettle();
    auth.changeOwner(_self);
    await t.pumpAndSettle();
    first.complete(_profile(_self, bio: '退休旧简介'));
    await t.pumpAndSettle();
    expect(reads, 3);
    expect(find.text('当前合法简介'), findsOneWidget);
    expect(find.text('退休旧简介'), findsNothing);
    expect(find.text('关注'), findsNothing);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
}
