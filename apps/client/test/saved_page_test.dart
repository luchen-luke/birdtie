import 'dart:convert';

import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'saved_items_test.dart'
    show
        MemorySavedNoteStore,
        savedNoteID,
        savedTargetID,
        savedOwnerID,
        savedTestRow;

http.Response _json(Object? data, [int status = 200]) => http.Response(
  jsonEncode({'data': data}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

Future<void> _tap(WidgetTester t, Finder finder) async {
  await t.ensureVisible(finder);
  await t.tap(finder);
  await t.pumpAndSettle();
}

void _phone(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

Future<void> _mount(
  WidgetTester t,
  SavedController c, {
  ValueChanged<SavedItem>? open,
  ValueChanged<String>? activity,
}) async {
  await t.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: SavedPage(
          saved: c,
          onOpenItem: open,
          onOpenActivity: activity,
          cityNames: const {'alpha': '阿伯丁', 'beta': '伦敦'},
        ),
      ),
    ),
  );
  await t.pumpAndSettle();
}

void main() {
  for (final round in [1, 2]) {
    testWidgets('顺序widget zone $round 默认SecureStore真实收藏route可读写，不保留已完成串行tail', (
      t,
    ) async {
      _phone(t);
      FlutterSecureStorage.setMockInitialValues({});
      var note = '', revision = 0;
      final calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        if (r.method == 'PUT') {
          final body = jsonDecode(r.body) as Map;
          note = body['note'];
          revision++;
          return _json({
            'savedId': savedNoteID,
            'operationId': body['operationId'],
            'revision': revision,
            'status': 'SUCCEEDED',
          });
        }
        if (r.url.path.endsWith('/note'))
          return _json({
            'savedId': savedNoteID,
            'note': note,
            'revision': revision,
          });
        return _json({
          'items': [
            savedTestRow(
              savedNoteID,
              'group',
              savedTargetID,
              title: '默认恢复存储社群收藏',
            ),
          ],
          'nextCursor': null,
        });
      });
      final c = SavedController(
        authorizationHeader: () => 'Bearer A',
        ownerID: () => savedOwnerID,
        client: client,
        apiBaseUrl: 'https://shared-zone.test',
      );
      await t.pumpWidget(
        MaterialApp(
          home: AnimatedBuilder(
            animation: c,
            builder: (context, _) => Scaffold(
              body: TextButton(
                onPressed: () => Navigator.of(context).push<void>(
                  MaterialPageRoute(
                    builder: (_) => Scaffold(body: SavedPage(saved: c)),
                  ),
                ),
                child: const Text('打开默认存储收藏'),
              ),
            ),
          ),
        ),
      );
      await _tap(t, find.text('打开默认存储收藏'));
      expect(find.text('默认恢复存储社群收藏'), findsOneWidget);
      await _tap(t, find.byTooltip('本人备注'));
      await t.enterText(find.byKey(const Key('saved-note-draft')), '本轮本人备注');
      await _tap(t, find.text('保存备注'));
      expect(c.items.single.note, '本轮本人备注');
      expect(calls.where((r) => r.method == 'PUT'), hasLength(1));
      expect(await const FlutterSecureStorage().readAll(), isEmpty);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      c.dispose();
      client.close();
    });
  }
  testWidgets('父Now同监听SavedController时打开收藏与更换controller加载在build后且不重复', (
    t,
  ) async {
    _phone(t);
    final reads = <String>[];
    final client = MockClient((r) async {
      reads.add(r.url.origin);
      return _json({
        'items': [
          savedTestRow(savedNoteID, 'group', savedTargetID, title: '本轮社群'),
        ],
        'nextCursor': null,
      });
    });
    final a = SavedController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://a.test',
    );
    final b = SavedController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://b.test',
    );
    final current = ValueNotifier<SavedController>(a);
    await t.pumpWidget(
      MaterialApp(
        home: AnimatedBuilder(
          animation: Listenable.merge([a, b]),
          builder: (context, _) => Scaffold(
            body: TextButton(
              onPressed: () {
                Navigator.of(context).push<void>(
                  MaterialPageRoute(
                    builder: (_) => Scaffold(
                      body: ValueListenableBuilder<SavedController>(
                        valueListenable: current,
                        builder: (_, c, _) => SavedPage(
                          key: const ValueKey('shared-listener-saved'),
                          saved: c,
                        ),
                      ),
                    ),
                  ),
                );
              },
              child: const Text('打开真实收藏入口'),
            ),
          ),
        ),
      ),
    );
    await _tap(t, find.text('打开真实收藏入口'));
    expect(find.text('本轮社群'), findsOneWidget);
    expect(reads, ['https://a.test']);
    expect(t.takeException(), isNull);
    current.value = b;
    await t.pumpAndSettle();
    expect(reads, ['https://a.test', 'https://b.test']);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    current.dispose();
    a.dispose();
    b.dispose();
    client.close();
  });
  testWidgets('首帧结束前账号generation退休，已排首轮加载不读取旧本人', (t) async {
    _phone(t);
    var reads = 0;
    final client = MockClient((r) async {
      reads++;
      return _json({'items': [], 'nextCursor': null});
    });
    final c = SavedController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://a.test',
    );
    var registered = false;
    await t.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (_) {
              if (!registered) {
                registered = true;
                WidgetsBinding.instance.addPostFrameCallback((_) => c.clear());
              }
              return SavedPage(saved: c);
            },
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(reads, 0);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    c.dispose();
    client.close();
  });
  testWidgets('收藏来源改变后备注sheet隐藏旧标题；409列表变化可重读首批', (t) async {
    _phone(t);
    var available = true, pageChanged = false;
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.url.path.endsWith('/note'))
        return _json({
          'savedId': savedNoteID,
          'note': '仅本人备注仍可见',
          'revision': 0,
        });
      if (pageChanged && r.url.queryParameters['cursor'] != null)
        return _json(null, 409);
      return _json({
        'items': [
          savedTestRow(
            savedNoteID,
            'place',
            savedTargetID,
            available: available,
            title: available ? '来源撤销前专属地点标题' : '',
          ),
        ],
        'nextCursor': savedNoteID,
      });
    });
    final c = SavedController(
      authorizationHeader: () => 'Bearer A',
      ownerID: () => savedOwnerID,
      client: client,
      apiBaseUrl: 'https://saved.test',
      notePendingStore: MemorySavedNoteStore(),
    );
    await _mount(t, c);
    await _tap(t, find.byTooltip('本人备注'));
    expect(find.text('来源撤销前专属地点标题'), findsNWidgets(2));
    available = false;
    await c.load(paged: true);
    await t.pumpAndSettle();
    expect(find.text('来源撤销前专属地点标题'), findsNothing);
    expect(find.text('内容暂不可用'), findsNWidgets(2));
    expect(
      t
          .widget<TextField>(find.byKey(const Key('saved-note-draft')))
          .controller!
          .text,
      '仅本人备注仍可见',
    );
    await _tap(t, find.text('取消'));
    pageChanged = true;
    await _tap(t, find.text('继续读取收藏'));
    expect(find.text('重新读取收藏'), findsOneWidget);
    expect(find.text('继续读取收藏'), findsNothing);
    await _tap(t, find.text('重新读取收藏'));
    expect(requests.last.url.queryParameters, isEmpty);
    expect(find.text('继续读取收藏'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    c.dispose();
    client.close();
  });
  testWidgets('390窄屏按城市类型和已读取关键词整理，空筛选可清除且继续读取20+内容', (t) async {
    _phone(t);
    final seen = <http.Request>[];
    final client = MockClient((r) async {
      seen.add(r);
      expect(r.url.path, '/v1/me/saved/page');
      return _json({
        'items': r.url.queryParameters['cursor'] == null
            ? List.generate(
                20,
                (i) => savedTestRow(
                  'bookmark-$i',
                  'place',
                  'place-$i',
                  city: i.isEven ? 'alpha' : 'beta',
                  title: '地点 $i',
                ),
              )
            : [
                savedTestRow(
                  savedNoteID,
                  'group',
                  savedTargetID,
                  title: '后续页中文 English 社群',
                ),
              ],
        'nextCursor': r.url.queryParameters['cursor'] == null
            ? savedNoteID
            : null,
      });
    });
    final c = SavedController(
      authorizationHeader: () => 'Bearer A',
      client: client,
      apiBaseUrl: 'https://saved.test',
    );
    await _mount(t, c);
    expect(find.text('已加载 20 项'), findsOneWidget);
    // This regression explicitly exercises the original loaded-only scope.
    // The new server search is independently covered by saved_search_test.
    await _tap(t, find.text('仅筛选已加载内容'));
    await _tap(t, find.text('社群'));
    expect(find.text('没有匹配的收藏'), findsOneWidget);
    await _tap(t, find.text('继续读取收藏'));
    expect(find.text('后续页中文 English 社群'), findsOneWidget);
    expect(c.items, hasLength(21));
    await _tap(t, find.text('清除筛选'));
    await _tap(t, find.text('全部城市'));
    await _tap(t, find.text('伦敦').last);
    expect(find.text('地点 1'), findsOneWidget);
    expect(find.text('地点 0'), findsNothing);
    await t.drag(find.byType(ListView), const Offset(0, 1500));
    await t.pumpAndSettle();
    await t.enterText(find.byKey(const Key('saved-search')), '没有这个关键词');
    await t.pumpAndSettle();
    expect(find.text('没有匹配的收藏'), findsOneWidget);
    await _tap(t, find.text('清除筛选'));
    expect(find.text('地点 0'), findsOneWidget);
    expect(seen, hasLength(2));
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    c.dispose();
    client.close();
  });
  testWidgets('三个可见收藏使用同SavedItem打开，不可见零导航仍可编辑私人备注和取消收藏', (t) async {
    _phone(t);
    final rows = [
      savedTestRow(
        '10000000-0000-4000-8000-000000000011',
        'place',
        savedTargetID,
        title: '地点原ID',
      ),
      savedTestRow(
        '10000000-0000-4000-8000-000000000012',
        'activity',
        savedTargetID,
        title: '活动原ID',
      ),
      savedTestRow(
        '10000000-0000-4000-8000-000000000013',
        'group',
        savedTargetID,
        title: '社群原ID',
      ),
      savedTestRow(
        savedNoteID,
        'place',
        '10000000-0000-4000-8000-000000000014',
        available: false,
      ),
    ];
    final opened = <SavedItem>[], writes = <http.Request>[];
    final client = MockClient((r) async {
      if (r.method == 'DELETE') {
        writes.add(r);
        rows.removeWhere((i) => i['id'] == savedNoteID);
        return http.Response('', 204);
      }
      if (r.url.path.endsWith('/note'))
        return _json({
          'savedId': savedNoteID,
          'note': '本人可保留备注',
          'revision': 2,
        });
      return _json({'items': rows, 'nextCursor': null});
    });
    final c = SavedController(
      authorizationHeader: () => 'Bearer A',
      ownerID: () => savedOwnerID,
      client: client,
      apiBaseUrl: 'https://saved.test',
      notePendingStore: MemorySavedNoteStore(),
    );
    await _mount(t, c, open: opened.add);
    for (final name in ['地点原ID', '活动原ID', '社群原ID'])
      await _tap(t, find.text(name));
    expect(opened.map((i) => i.kind).toList(), ['place', 'activity', 'group']);
    for (final i in opened) expect(c.items.any((v) => identical(i, v)), true);
    await _tap(t, find.text('内容暂不可用'));
    expect(opened, hasLength(3));
    final unavailable = find.ancestor(
      of: find.text('内容暂不可用'),
      matching: find.byType(ListTile),
    );
    await _tap(
      t,
      find.descendant(of: unavailable, matching: find.byTooltip('本人备注')),
    );
    expect(find.text('仅自己可见，不会随内容公开或分享。'), findsOneWidget);
    expect(
      t
          .widget<TextField>(find.byKey(const Key('saved-note-draft')))
          .controller!
          .text,
      '本人可保留备注',
    );
    await _tap(t, find.text('取消'));
    await _tap(
      t,
      find.descendant(of: unavailable, matching: find.byTooltip('取消收藏')),
    );
    expect(writes.single.url.path, '/v1/me/saved/$savedNoteID');
    expect(find.text('内容暂不可用'), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    c.dispose();
    client.close();
  });
  testWidgets('本人备注键盘可滚动并取消零写，UNKNOWN重开保留草稿只读核实', (t) async {
    _phone(t);
    addTearDown(t.view.resetViewInsets);
    final requests = <http.Request>[];
    final client = MockClient((r) async {
      requests.add(r);
      if (r.method == 'PUT') return http.Response('', 503);
      if (r.url.path.contains('/operations/')) return http.Response('', 404);
      if (r.url.path.endsWith('/note'))
        return _json({
          'savedId': savedNoteID,
          'note': '已保存本人备注',
          'revision': 2,
        });
      return _json({
        'items': [
          savedTestRow(
            savedNoteID,
            'place',
            savedTargetID,
            title: '收藏长中文 English 地点',
          ),
        ],
        'nextCursor': null,
      });
    });
    final c = SavedController(
      authorizationHeader: () => 'Bearer A',
      ownerID: () => savedOwnerID,
      client: client,
      apiBaseUrl: 'https://saved.test',
      notePendingStore: MemorySavedNoteStore(),
    );
    await _mount(t, c);
    await _tap(t, find.byTooltip('本人备注'));
    await t.enterText(find.byKey(const Key('saved-note-draft')), '取消不保存的草稿');
    t.view.viewInsets = const FakeViewPadding(bottom: 300);
    await t.pumpAndSettle();
    await _tap(t, find.text('取消'));
    expect(requests.where((r) => r.method == 'PUT'), isEmpty);
    t.view.viewInsets = const FakeViewPadding();
    await t.pumpAndSettle();
    await _tap(t, find.byTooltip('本人备注'));
    expect(
      t
          .widget<TextField>(find.byKey(const Key('saved-note-draft')))
          .controller!
          .text,
      '已保存本人备注',
    );
    await t.enterText(
      find.byKey(const Key('saved-note-draft')),
      '未知保存仍保留的本人草稿',
    );
    await _tap(t, find.text('保存备注'));
    expect(find.text('核实上次保存'), findsOneWidget);
    expect(find.text('保存备注'), findsNothing);
    await _tap(t, find.text('返回收藏'));
    await _tap(t, find.byTooltip('本人备注'));
    expect(
      t
          .widget<TextField>(find.byKey(const Key('saved-note-draft')))
          .controller!
          .text,
      '未知保存仍保留的本人草稿',
    );
    await _tap(t, find.text('核实上次保存'));
    expect(requests.where((r) => r.method == 'PUT'), hasLength(1));
    expect(
      requests.where((r) => r.url.path.contains('/operations/')),
      hasLength(1),
    );
    expect(find.text('尚未取得上次保存回执，请稍后再核实。'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    c.dispose();
    client.close();
  });
  testWidgets('旧兼容activity回调保持且备注账号ABA立即隐藏', (t) async {
    _phone(t);
    var token = 'Bearer A', owner = savedOwnerID;
    final opened = <String>[];
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/note'))
        return _json({
          'savedId': savedNoteID,
          'note': '旧本人私人正文',
          'revision': 1,
        });
      return _json({
        'items': [
          savedTestRow(savedNoteID, 'activity', savedTargetID, title: '原活动兼容'),
        ],
        'nextCursor': null,
      });
    });
    final c = SavedController(
      authorizationHeader: () => token,
      ownerID: () => owner,
      client: client,
      apiBaseUrl: 'https://saved.test',
      notePendingStore: MemorySavedNoteStore(),
    );
    await _mount(t, c, activity: opened.add);
    await _tap(t, find.text('原活动兼容'));
    expect(opened, [savedTargetID]);
    await _tap(t, find.byTooltip('本人备注'));
    token = 'Bearer B';
    owner = savedTargetID;
    c.clear();
    token = 'Bearer A';
    owner = savedOwnerID;
    c.clear();
    await t.pumpAndSettle();
    expect(find.text('当前身份已变化'), findsOneWidget);
    expect(find.byKey(const Key('saved-note-draft')), findsNothing);
    expect(find.text('旧本人私人正文'), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    c.dispose();
    client.close();
  });
}
