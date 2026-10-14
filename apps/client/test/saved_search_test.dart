import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';

const searchOwner = '11111111-1111-4111-8111-111111111111';
const searchPlace = '22222222-2222-4222-8222-222222222222';
const searchSaved = '33333333-3333-4333-8333-333333333333';
Map<String, dynamic> searchRow({
  String title = '图书馆',
  String note = '',
  bool point = false,
}) => {
  'id': searchSaved,
  'kind': 'place',
  'targetId': searchPlace,
  'title': title,
  'summary': '公开简介',
  'cityId': 'aberdeen-gb',
  'cityName': 'Aberdeen',
  'available': true,
  'note': note,
  'noteRevision': 0,
  if (point) ...{
    'latitude': 57.1,
    'longitude': -2.1,
    'sourceExpiresAt': DateTime.now()
        .toUtc()
        .add(const Duration(seconds: 60))
        .toIso8601String(),
  },
};
http.Response searchPage(List<Map<String, dynamic>> rows, {String? cursor}) =>
    http.Response.bytes(
      utf8.encode(
        jsonEncode({
          'data': {'items': rows, 'nextCursor': cursor},
        }),
      ),
      200,
    );

class SearchNoteStore implements SavedNotePendingStore {
  @override
  Future<SavedNotePending?> read(String o, String u, String i) async => null;
  @override
  Future<void> write(String o, String u, SavedNotePending p) async {}
  @override
  Future<void> remove(String o, String u, SavedNotePending p) async {}
}

SavedController searchController(
  http.Client client, {
  String? Function()? owner,
  String? Function()? token,
}) => SavedController(
  client: client,
  apiBaseUrl: 'https://saved.test',
  ownerID: owner ?? () => searchOwner,
  authorizationHeader: token ?? () => 'Bearer A',
  notePendingStore: SearchNoteStore(),
);
void main() {
  test('savedSearch 全分页查询不以首20项缓存代替且保留opaque cursor', () async {
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return searchPage(
        [
          searchRow(
            title: r.url.queryParameters['q'] == '咖啡' ? '第21项咖啡' : '图书馆',
          ),
        ],
        cursor: r.url.queryParameters.containsKey('cursor')
            ? null
            : 'opaque_search_cursor',
      );
    });
    final c = searchController(client);
    await c.load(paged: true);
    await c.search('咖啡', kind: 'place', cityID: 'aberdeen-gb');
    expect(c.items.single.title, '第21项咖啡');
    expect(c.resultsForCurrentQuery, true);
    await c.load(more: true);
    expect(calls.last.url.queryParameters, {
      'q': '咖啡',
      'kind': 'place',
      'cityId': 'aberdeen-gb',
      'cursor': 'opaque_search_cursor',
    });
    await c.search('');
    expect(calls.last.url.queryParameters, isEmpty);
    expect(c.hasMore, true);
    c.dispose();
    client.close();
  });
  test('savedSearch 旧查询迟到不覆盖新的清查询结果', () async {
    final old = Completer<http.Response>();
    final client = MockClient(
      (r) async => r.url.queryParameters['q'] == '旧词'
          ? old.future
          : searchPage([searchRow(title: '当前全部收藏')]),
    );
    final c = searchController(client);
    final first = c.search('旧词');
    await Future<void>.delayed(Duration.zero);
    await c.search('');
    old.complete(searchPage([searchRow(title: '旧私有结果')]));
    await first;
    expect(c.items.single.title, '当前全部收藏');
    expect(c.query, '');
    expect(c.loading, false);
    c.dispose();
    client.close();
  });
  test('savedSearch 主体ABA后旧分页响应永不复活', () async {
    final wait = Completer<http.Response>();
    String owner = searchOwner;
    final client = MockClient((_) => wait.future);
    final c = searchController(client, owner: () => owner);
    final request = c.search('旧词');
    await Future<void>.delayed(Duration.zero);
    owner = searchPlace;
    c.clear();
    owner = searchOwner;
    c.clear();
    wait.complete(searchPage([searchRow()]));
    await request;
    expect(c.items, isEmpty);
    expect(c.hasMore, false);
    c.dispose();
    client.close();
  });
  test('savedSearch 字节上限失败不发GET，字面百分号保留URI值', () async {
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return searchPage([]);
    });
    final c = searchController(client);
    await expectLater(
      c.search(List.filled(81, '中').join()),
      throwsFormatException,
    );
    expect(calls, isEmpty);
    await c.search('%_\\');
    expect(calls.single.url.queryParameters['q'], '%_\\');
    c.dispose();
    client.close();
  });
  test('savedSearch filtered409保留内容并要求第一页重新核实', () async {
    int n = 0;
    final client = MockClient(
      (r) async => n++ == 0
          ? searchPage([searchRow()], cursor: 'opaque')
          : http.Response('', 409),
    );
    final c = searchController(client);
    await c.search('图');
    await c.load(more: true);
    expect(c.items, hasLength(1));
    expect(c.pageChanged, true);
    expect(c.nextCursor, isNull);
    c.dispose();
    client.close();
  });
  test('savedSearch actual地图元数据无图不猜坐标，失配拒绝', () {
    expect(SavedItem.fromJson(searchRow()).mapCurrent, false);
    final item = SavedItem.fromJson(searchRow(point: true));
    expect(item.mapCurrent, true);
    expect(item.withNote('私密备注', 1).latitude, 57.1);
    for (final bad in [
      {...searchRow(point: true), 'kind': 'group'},
      {...searchRow(point: true), 'available': false},
      {...searchRow(point: true), 'longitude': null},
      {...searchRow(point: true), 'latitude': double.infinity},
    ]) {
      expect(() => SavedItem.fromJson(bad), throwsFormatException);
    }
  });
  testWidgets('savedSearch 已加载私人备注筛选明确范围且不发送搜索', (tester) async {
    final calls = <http.Request>[];
    final client = MockClient((r) async {
      calls.add(r);
      return searchPage([searchRow(note: '私人检查词')]);
    });
    final c = searchController(client);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: SavedPage(saved: c)),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byType(CheckboxListTile));
    await tester.pump();
    await tester.enterText(find.byKey(const Key('saved-search')), '私人检查词');
    await tester.pump(const Duration(milliseconds: 500));
    expect(calls, hasLength(1));
    expect(find.text('在已加载的 1 项中筛选'), findsOneWidget);
    expect(find.text('图书馆'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    c.dispose();
    client.close();
  });
}
