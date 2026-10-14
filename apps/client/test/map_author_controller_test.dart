import 'dart:async';
import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:birdtie_client/src/workspace/map_layers_controller.dart';
import 'map_layers_api_test.dart' as f;

const authorA = '33333333-3333-4333-8333-333333333333';
const authorB = '44444444-4444-4444-8444-444444444444';
Map<String, dynamic> authorWire(String author, {int ms = 120000}) =>
    f.mapTestView(milliseconds: ms)
      ..['schemaVersion'] = 'typed-map-layers-v2'
      ..['scope'] = 'PUBLIC_AUTHOR'
      ..['authorId'] = author
      ..['items'] = [f.mapTestItem('MOMENT')];
http.Response response(Map<String, dynamic> wire) => http.Response(
  jsonEncode({'data': wire}),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

MapLayersController controller(
  Future<http.Response> Function(http.Request) get, {
  String? Function()? token,
}) => MapLayersController(
  api: MapLayersApi(client: MockClient(get), apiBaseUrl: 'http://owned.test'),
  authorizationHeader: token ?? () => null,
  organizationWorkspaceID: () => null,
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('同一typed作者集合供列表和地图，原城市集合和epoch不变', () async {
    final c = controller((r) async {
      final a = r.url.queryParameters['authorId'];
      return response(a == null ? f.mapTestView() : authorWire(a));
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    final base = c.publicView;
    final epoch = c.epoch;
    expect(await c.loadAuthor('owned-city', f.mapTestBounds, authorA), isTrue);
    expect(c.authorID, authorA);
    expect(c.items, same(c.authorView!.items));
    expect(c.items.single.kind, 'MOMENT');
    expect(c.items.single.entity.id, c.items.single.mapID);
    expect(c.findBase('place:${f.mapTestID}')?.kind, 'PLACE');
    expect(c.find('place:${f.mapTestID}'), isNull);
    expect(c.publicView, same(base));
    expect(c.epoch, epoch);
    c.leaveAuthor();
    expect(c.authorID, isNull);
    expect(c.items.length, 5);
    expect(c.publicView, same(base));
    expect(c.epoch, epoch);
  });

  test('作者404不回退城市，显式离开才返回原仍有效城市集合', () async {
    final c = controller(
      (r) async => r.url.queryParameters.containsKey('authorId')
          ? http.Response('{"error":{"code":"map_context_unavailable"}}', 404)
          : response(f.mapTestView()),
    );
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    final base = c.publicView;
    expect(await c.loadAuthor('owned-city', f.mapTestBounds, authorA), isFalse);
    expect(c.authorError, isNotNull);
    expect(c.items, isEmpty);
    expect(c.baseItems.length, 5);
    c.leaveAuthor();
    expect(c.items.length, 5);
    expect(c.publicView, same(base));
  });

  test('有效空作者集合保持作者范围，不能补入其他实体', () async {
    final c = controller((r) async {
      final a = r.url.queryParameters['authorId'];
      return response(
        a == null ? f.mapTestView() : (authorWire(a)..['items'] = []),
      );
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    expect(await c.loadAuthor('owned-city', f.mapTestBounds, authorA), isTrue);
    expect(c.authorView, isNotNull);
    expect(c.authorError, isNull);
    expect(c.authorID, authorA);
    expect(c.items, isEmpty);
    expect(c.baseItems, isNotEmpty);
  });

  test('作者来源到期后卡片点位同时为空，不延长原城市来源', () async {
    final c = controller((r) async {
      final a = r.url.queryParameters['authorId'];
      return response(a == null ? f.mapTestView() : authorWire(a, ms: 60));
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    final base = c.publicView;
    expect(await c.loadAuthor('owned-city', f.mapTestBounds, authorA), isTrue);
    await Future<void>.delayed(const Duration(milliseconds: 100));
    expect(c.items, isEmpty);
    expect(c.authorID, authorA);
    expect(c.authorError, contains('到期'));
    expect(c.publicView, same(base));
  });

  test('同作者刷新请求挂起不延长旧lease', () async {
    final pending = Completer<http.Response>();
    var n = 0;
    final c = controller((r) async {
      final a = r.url.queryParameters['authorId'];
      if (a == null) return response(f.mapTestView());
      if (++n > 1) return pending.future;
      return response(authorWire(a, ms: 60));
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    await c.loadAuthor('owned-city', f.mapTestBounds, authorA);
    final refresh = c.loadAuthor('owned-city', f.mapTestBounds, authorA);
    expect(c.items, isNotEmpty);
    await Future<void>.delayed(const Duration(milliseconds: 100));
    expect(c.items, isEmpty);
    pending.complete(response(authorWire(authorA)));
    expect(await refresh, isTrue);
    expect(c.items, isNotEmpty);
  });

  test('切换目标后旧作者迟到响应不能覆盖新目标', () async {
    final pending = Completer<http.Response>();
    final c = controller((r) async {
      final a = r.url.queryParameters['authorId'];
      if (a == authorA) return pending.future;
      return response(a == null ? f.mapTestView() : authorWire(a));
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    final old = c.loadAuthor('owned-city', f.mapTestBounds, authorA);
    expect(await c.loadAuthor('owned-city', f.mapTestBounds, authorB), isTrue);
    pending.complete(response(authorWire(authorA)));
    expect(await old, isFalse);
    expect(c.authorView?.authorID, authorB);
    expect(c.authorLoading, isFalse);
  });

  test('身份A-B-A原retire永久拒绝旧作者范围', () async {
    final pending = Completer<http.Response>();
    var token = 'Bearer A';
    final c = controller(
      (r) async => r.url.queryParameters.containsKey('authorId')
          ? pending.future
          : response(f.mapTestView()),
      token: () => token,
    );
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    final old = c.loadAuthor('owned-city', f.mapTestBounds, authorA);
    token = 'Bearer B';
    c.retire();
    token = 'Bearer A';
    c.retire();
    pending.complete(response(authorWire(authorA)));
    expect(await old, isFalse);
    expect(c.authorView, isNull);
    expect(c.authorID, isNull);
    expect(c.items, isEmpty);
  });

  test('离开时尚未完成的作者请求不恢复旧范围', () async {
    final pending = Completer<http.Response>();
    final c = controller(
      (r) async => r.url.queryParameters.containsKey('authorId')
          ? pending.future
          : response(f.mapTestView()),
    );
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    final old = c.loadAuthor('owned-city', f.mapTestBounds, authorA);
    c.leaveAuthor();
    pending.complete(response(authorWire(authorA)));
    expect(await old, isFalse);
    expect(c.items.length, 5);
    expect(c.authorID, isNull);
  });

  test('读取耗时计入作者来源期限', () async {
    final c = controller((r) async {
      final a = r.url.queryParameters['authorId'];
      if (a == null) return response(f.mapTestView());
      await Future<void>.delayed(const Duration(milliseconds: 100));
      return response(authorWire(a, ms: 60));
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    expect(await c.loadAuthor('owned-city', f.mapTestBounds, authorA), isFalse);
    expect(c.items, isEmpty);
    expect(c.authorError, contains('到期'));
  });

  test('其他城市不得沿当前上下文请求', () async {
    var gets = 0;
    final c = controller((r) async {
      gets++;
      return response(f.mapTestView());
    });
    addTearDown(c.dispose);
    await c.load('owned-city', f.mapTestBounds);
    expect(
      await c.loadAuthor('another-city', f.mapTestBounds, authorA),
      isFalse,
    );
    expect(gets, 1);
    expect(c.authorID, isNull);
  });
}
