import 'package:birdtie_client/src/city/map_cluster_members_sheet.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

MapEntity _member(int n, {String? title}) => MapEntity(
  id: 'moment:$n',
  kind: MapEntityKind.moment,
  title: title ?? '公开记录 $n',
  subtitle: '当前可见来源',
  latitude: 57.2,
  longitude: -2.1,
);

const _cluster = MapEntity(
  id: 'cluster:test',
  kind: MapEntityKind.cluster,
  title: '原聚合',
  subtitle: '',
  latitude: 57.2,
  longitude: -2.1,
  memberIDs: ['moment:1', 'moment:2'],
);

class _ReaderFixture {
  final members = ValueNotifier([_member(1), _member(2)]);
  int epoch = 0;
  final visibility = <({Object token, bool visible})>[];
  MapEntity? selected;
  Future<void> mount(WidgetTester t) => t.pumpWidget(
    MaterialApp(
      home: Builder(
        builder: (context) => Scaffold(
          body: TextButton(
            onPressed: () async {
              selected = await showCurrentMapClusterMembers(
                context: context,
                cluster: _cluster,
                changes: members,
                currentMembers: () => members.value,
                currentScope: () => epoch,
                onVisibilityChanged: visibility.add,
              );
            },
            child: const Text('打开聚合'),
          ),
        ),
      ),
    ),
  );
  Future<void> open(WidgetTester t) async {
    await mount(t);
    await t.tap(find.text('打开聚合'));
    await t.pumpAndSettle();
    expect(
      find.byKey(const Key('current-map-cluster-members')),
      findsOneWidget,
    );
  }

  Future<void> dispose(WidgetTester t) async {
    await t.pumpWidget(const SizedBox.shrink());
    await t.pump();
    members.dispose();
  }
}

void main() {
  testWidgets(
    'owned modal reports route visibility changes while still mounted beneath foreign route',
    (t) async {
      final f = _ReaderFixture();
      try {
        await f.open(t);
        final nav = Navigator.of(
          t.element(find.byKey(const Key('current-map-cluster-members'))),
        );
        nav.push(
          MaterialPageRoute<void>(
            builder: (_) => const Scaffold(body: Text('其他页面')),
          ),
        );
        await t.pumpAndSettle();
        expect(f.visibility.map((e) => e.visible).toList(), [true, false]);
        expect(
          find.byKey(
            const Key('current-map-cluster-members'),
            skipOffstage: false,
          ),
          findsOneWidget,
        );
        nav.pop();
        await t.pumpAndSettle();
        expect(f.visibility.map((e) => e.visible).toList(), [
          true,
          false,
          true,
        ]);
        await t.tap(find.text('公开记录 1'));
        await t.pumpAndSettle();
        expect(f.visibility.map((e) => e.visible).toList(), [
          true,
          false,
          true,
          false,
        ]);
        expect(
          f.visibility.every(
            (e) => identical(e.token, f.visibility.first.token),
          ),
          true,
        );
      } finally {
        await f.dispose(t);
      }
    },
  );

  testWidgets(
    'open reader replaces current title and count, withdrawn member cannot remain selectable',
    (t) async {
      final f = _ReaderFixture();
      try {
        await f.open(t);
        expect(find.text('2条公开记录'), findsOneWidget);
        f.members.value = [_member(1, title: '最新合法标题 café 中文')];
        await t.pumpAndSettle();
        expect(find.text('公开记录 1'), findsNothing);
        expect(find.text('公开记录 2'), findsNothing);
        expect(find.text('1条公开记录'), findsOneWidget);
        await t.tap(find.text('最新合法标题 café 中文'));
        await t.pumpAndSettle();
        expect(f.selected?.title, '最新合法标题 café 中文');
        expect(f.visibility.map((e) => e.visible).toList(), [true, false]);
        expect(identical(f.visibility[0].token, f.visibility[1].token), true);
      } finally {
        await f.dispose(t);
      }
    },
  );

  testWidgets(
    'retired source closes original reader with no cached selection',
    (t) async {
      final f = _ReaderFixture();
      try {
        await f.open(t);
        f.members.value = [];
        await t.pumpAndSettle();
        expect(
          find.byKey(const Key('current-map-cluster-members')),
          findsNothing,
        );
        expect(f.selected, isNull);
        expect(f.visibility.map((e) => e.visible).toList(), [true, false]);
      } finally {
        await f.dispose(t);
      }
    },
  );

  testWidgets(
    'monotonic identity or city ABA closes old reader even after equal display returns',
    (t) async {
      final f = _ReaderFixture();
      try {
        await f.open(t);
        f.epoch++;
        f.members.value = [_member(2)];
        f.epoch++;
        f.members.value = [_member(1), _member(2)];
        await t.pumpAndSettle();
        expect(
          find.byKey(const Key('current-map-cluster-members')),
          findsNothing,
        );
        expect(f.selected, isNull);
      } finally {
        await f.dispose(t);
      }
    },
  );

  testWidgets(
    'current source replacement retains reading scroll rather than reopening modal',
    (t) async {
      final f = _ReaderFixture();
      try {
        f.members.value = [for (var n = 1; n <= 30; n++) _member(n)];
        await f.open(t);
        final list = find.descendant(
          of: find.byKey(const Key('current-map-cluster-members')),
          matching: find.byType(ListView),
        );
        final controller = t.widget<ListView>(list).controller!;
        controller.jumpTo(500);
        await t.pump();
        final element = list.evaluate().single;
        f.members.value = [
          for (var n = 1; n <= 30; n++) _member(n, title: '更新公开记录 $n'),
        ];
        await t.pump();
        expect(
          identical(t.widget<ListView>(list).controller, controller),
          true,
        );
        expect(identical(list.evaluate().single, element), true);
        expect(controller.offset, 500);
        expect(f.visibility, hasLength(1));
      } finally {
        await f.dispose(t);
      }
    },
  );
}
