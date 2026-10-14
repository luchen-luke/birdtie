import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/saved_items.dart';
import 'package:birdtie_client/src/workspace/saved_map_projection.dart';
import 'saved_search_test.dart';

void main() {
  test('savedMap 同一地点投影不含私人备注并可取消比较', () {
    final client = MockClient((_) async => searchPage([])),
        c = searchController(client),
        changes = ValueNotifier(0);
    final item = SavedItem.fromJson(searchRow(point: true, note: '绝不能流入地图或分享'));
    c.items = [item];
    var retired = 0;
    final p = SavedMapProjectionSource(
      saved: c,
      items: [item],
      cityID: 'aberdeen-gb',
      boundaryChanges: changes,
      sourceCurrent: () => true,
      onRetired: () => retired++,
    );
    expect(p.projection.entities.single.id, 'place:$searchPlace');
    expect(p.projection.entities.single.subtitle, '公开简介');
    p.toggle('place:$searchPlace');
    expect(p.comparison.single.targetId, searchPlace);
    p.toggle('place:$searchPlace');
    expect(p.comparison, isEmpty);
    expect(retired, 0);
    p.dispose();
    c.dispose();
    client.close();
    changes.dispose();
  });
  test('savedMap 备注改变不破坏点，来源隐藏与ABA永久退休', () {
    final client = MockClient((_) async => searchPage([])),
        c = searchController(client),
        changes = ValueNotifier(0);
    final item = SavedItem.fromJson(searchRow(point: true));
    c.items = [item];
    bool current = true;
    var retire = 0;
    final p = SavedMapProjectionSource(
      saved: c,
      items: [item],
      cityID: 'aberdeen-gb',
      boundaryChanges: changes,
      sourceCurrent: () => current,
      onRetired: () => retire++,
    );
    c.items = [item.withNote('新的私人备注', 2)];
    changes.value++;
    expect(p.current, true);
    current = false;
    changes.value++;
    current = true;
    changes.value++;
    expect(p.current, false);
    expect(retire, 1);
    p.dispose();
    c.dispose();
    client.close();
    changes.dispose();
  });
  test('savedMap 坐标来源到期和城市错配不产生假点', () {
    final client = MockClient((_) async => searchPage([])),
        c = searchController(client),
        changes = ValueNotifier(0);
    final expired = SavedItem.fromJson({
      ...searchRow(point: true),
      'sourceExpiresAt': '2020-01-01T00:00:00Z',
    });
    expect(
      () => SavedMapProjectionSource(
        saved: c,
        items: [expired],
        cityID: 'aberdeen-gb',
        boundaryChanges: changes,
        sourceCurrent: () => true,
        onRetired: () {},
      ),
      throwsArgumentError,
    );
    expect(
      () => SavedMapProjectionSource(
        saved: c,
        items: [SavedItem.fromJson(searchRow(point: true))],
        cityID: 'other',
        boundaryChanges: changes,
        sourceCurrent: () => true,
        onRetired: () {},
      ),
      throwsArgumentError,
    );
    c.dispose();
    client.close();
    changes.dispose();
  });
}
