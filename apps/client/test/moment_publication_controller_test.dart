import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/moment_publication_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const publicationTestMoment = '11111111-1111-4111-8111-111111111111';
const publicationTestPlace = '22222222-2222-4222-8222-222222222222';
Map<String, dynamic> publicationTestPreview() => {
  'momentId': publicationTestMoment,
  'placeId': publicationTestPlace,
  'cityId': 'test',
  'placeName': '测试图书馆',
  'title': '想公开的记录',
  'body': '由我确认的内容',
  'revision': 3,
  'snapshot': 'mp1.1.2.${'a' * 64}',
  'expiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(seconds: 90))
      .toIso8601String(),
};
http.Response publicationTestReply(Map<String, dynamic> d, [int code = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': d})), code);
void main() {
  test('同主体重选目标使旧权威结果显示失效', () async {
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer self',
      apiBaseUrl: 'https://test',
      client: MockClient(
        (_) async => publicationTestReply({
          'id': publicationTestMoment,
          'placeId': publicationTestPlace,
          'revision': 4,
          'status': 'published',
          'visibility': 'public',
        }),
      ),
    );
    addTearDown(c.dispose);
    await c.checkCurrentStatus(withdrawal: false);
    expect(c.resolvedStatus, 'published');
    c.invalidate();
    expect(c.resolvedStatus, isNull);
    expect(c.preview, isNull);
  });

  test('组织工作区即使沿用个人令牌也不读个人预览、状态或写入', () async {
    String? workspace = 'org-one';
    var calls = 0;
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer same-person',
      organizationWorkspaceID: () => workspace,
      apiBaseUrl: 'https://test',
      client: MockClient((_) async {
        calls++;
        return publicationTestReply(publicationTestPreview());
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    await c.load(withdrawal: true);
    await c.checkCurrentStatus(withdrawal: false);
    expect(c.preview, isNull);
    expect(c.error, contains('组织工作区'));
    expect(calls, 0);
    workspace = null;
    await c.load();
    final old = c.preview!;
    workspace = 'org-one';
    c.invalidate();
    workspace = null;
    expect(await c.confirm(old, checked: true), false);
    expect(calls, 1);
    await c.load();
    expect(c.preview, isNot(same(old)));
    expect(calls, 2);
  });
  test('组织往返后同令牌的迟到个人预览不回流', () async {
    String? workspace;
    final wait = Completer<http.Response>();
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer same-person',
      organizationWorkspaceID: () => workspace,
      apiBaseUrl: 'https://test',
      client: MockClient((_) => wait.future),
    );
    addTearDown(c.dispose);
    final pending = c.load();
    workspace = 'org-one';
    c.invalidate();
    workspace = null;
    c.invalidate();
    wait.complete(publicationTestReply(publicationTestPreview()));
    await pending;
    expect(c.preview, isNull);
    expect(c.loading, false);
  });
  test('迟到公开提交在组织切换后不报告个人成功或恢复批准', () async {
    String? workspace;
    final wait = Completer<http.Response>();
    var posts = 0;
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer same-person',
      organizationWorkspaceID: () => workspace,
      apiBaseUrl: 'https://test',
      client: MockClient((r) async {
        if (r.method == 'GET') {
          return publicationTestReply(publicationTestPreview());
        }
        posts++;
        return wait.future;
      }),
    );
    addTearDown(c.dispose);
    await c.load();
    final old = c.preview!;
    final pending = c.confirm(old, checked: true);
    workspace = 'org-one';
    c.invalidate();
    wait.complete(
      publicationTestReply({
        'momentId': publicationTestMoment,
        'placeId': publicationTestPlace,
        'revision': 4,
        'status': 'published',
        'publishedAt': DateTime.now().toUtc().toIso8601String(),
      }),
    );
    expect(await pending, false);
    expect(c.preview, isNull);
    expect(c.error, contains('组织工作区'));
    expect(posts, 1);
    workspace = null;
    c.invalidate();
    expect(await c.confirm(old, checked: true), false);
    expect(posts, 1);
  });

  test('提交响应丢失后读取权威状态，不把已公开说成私人草稿', () async {
    var posted = false, statusReads = 0;
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer self',
      client: MockClient((r) async {
        if (r.method == 'POST') {
          posted = true;
          return http.Response('{}', 503);
        }
        if (r.url.path.endsWith('/publication')) {
          return posted
              ? http.Response('{}', 409)
              : publicationTestReply(publicationTestPreview());
        }
        statusReads++;
        return publicationTestReply({
          'id': publicationTestMoment,
          'placeId': publicationTestPlace,
          'revision': 4,
          'status': 'published',
          'visibility': 'public',
        });
      }),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await c.load();
    expect(await c.confirm(c.preview!, checked: true), false);
    expect(c.uncertain, true);
    await c.load();
    expect(statusReads, 1);
    expect(c.error, contains('当前记录已公开'));
    expect(c.uncertain, false);
  });
  test('只有具体预览和勾选确认才提交原三个字段', () async {
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method == 'GET') {
        return publicationTestReply(publicationTestPreview());
      }
      writes++;
      expect(r.headers['Authorization'], 'Bearer self');
      expect(jsonDecode(r.body), {
        'revision': 3,
        'snapshot': 'mp1.1.2.${'a' * 64}',
        'confirmPublic': true,
      });
      return publicationTestReply({
        'momentId': publicationTestMoment,
        'placeId': publicationTestPlace,
        'revision': 4,
        'status': 'published',
        'publishedAt': DateTime.now().toUtc().toIso8601String(),
      });
    });
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer self',
      client: client,
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await c.load();
    final first = c.preview!;
    expect(await c.confirm(first, checked: false), false);
    expect(writes, 0);
    await c.load();
    expect(await c.confirm(c.preview!, checked: true), true);
    expect(writes, 1);
  });
  test('迟到预览和已观测账号ABA不能恢复旧批准', () async {
    var token = 'Bearer one';
    final wait = Completer<http.Response>();
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method == 'GET') return wait.future;
      writes++;
      return http.Response('', 500);
    });
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => token,
      client: client,
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    final pending = c.load();
    token = 'Bearer two';
    expect(c.preview, isNull);
    token = 'Bearer one';
    expect(c.preview, isNull);
    wait.complete(publicationTestReply(publicationTestPreview()));
    await pending;
    expect(c.preview, isNull);
    expect(writes, 0);
  });
  test('来源字段、错地点、失效预览、未知响应均拒绝', () async {
    for (final mode in ['unknown', 'place', 'expiry', 'revision', 'title']) {
      final p = publicationTestPreview();
      switch (mode) {
        case 'unknown':
          p['privateContexts'] = ['secret'];
        case 'place':
          p['placeId'] = publicationTestMoment;
        case 'expiry':
          p['expiresAt'] = '2000-01-01T00:00:00Z';
        case 'revision':
          p['revision'] = 1.5;
        case 'title':
          p['title'] = '';
      }
      final c = MomentPublicationController(
        momentID: publicationTestMoment,
        placeID: publicationTestPlace,
        authorizationHeader: () => 'Bearer self',
        client: MockClient((_) async => publicationTestReply(p)),
        apiBaseUrl: 'https://test',
      );
      await c.load();
      expect(c.preview, isNull, reason: mode);
      expect(c.error, isNotNull);
      c.dispose();
    }
  });
  test('提交不明不自动重试，旧版本不能再提交', () async {
    var writes = 0;
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer self',
      client: MockClient((r) async {
        if (r.method == 'GET') {
          return publicationTestReply(publicationTestPreview());
        }
        writes++;
        return publicationTestReply({'unexpected': 'no'}, 200);
      }),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await c.load();
    final p = c.preview!;
    expect(await c.confirm(p, checked: true), false);
    expect(c.uncertain, true);
    expect(await c.confirm(p, checked: true), false);
    expect(writes, 1);
  });
  test('撤回先读本人公开版本，再DELETE原revision', () async {
    var deletes = 0;
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => 'Bearer self',
      client: MockClient((r) async {
        if (r.method == 'GET') {
          return publicationTestReply({
            'id': publicationTestMoment,
            'placeId': publicationTestPlace,
            'title': '公开记录',
            'body': '内容',
            'revision': 4,
            'status': 'published',
            'visibility': 'public',
          });
        }
        deletes++;
        expect(r.method, 'DELETE');
        expect(r.url.queryParameters, {'revision': '4'});
        return http.Response('', 204);
      }),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await c.load(withdrawal: true);
    expect(await c.confirm(c.preview!, checked: true), true);
    expect(deletes, 1);
  });
  test('迟到提交在主体变化后不报告成功', () async {
    var token = 'Bearer one';
    final waiting = Completer<http.Response>();
    final c = MomentPublicationController(
      momentID: publicationTestMoment,
      placeID: publicationTestPlace,
      authorizationHeader: () => token,
      client: MockClient(
        (r) async => r.method == 'GET'
            ? publicationTestReply(publicationTestPreview())
            : waiting.future,
      ),
      apiBaseUrl: 'https://test',
    );
    addTearDown(c.dispose);
    await c.load();
    final send = c.confirm(c.preview!, checked: true);
    token = 'Bearer two';
    expect(c.preview, isNull);
    waiting.complete(
      publicationTestReply({
        'momentId': publicationTestMoment,
        'placeId': publicationTestPlace,
        'revision': 4,
        'status': 'published',
        'publishedAt': DateTime.now().toUtc().toIso8601String(),
      }),
    );
    expect(await send, false);
    expect(c.preview, isNull);
  });
}
