import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:flutter_test/flutter_test.dart';

TypedMapItem recordingMapItem(
  String kind,
  int n, {
  bool repeatedName = false,
}) => TypedMapItem(
  kind: kind,
  id: '${kind == 'OPPORTUNITY' ? '33000000-0000-4000-8000-000000000001:' : ''}22000000-0000-4000-8000-${n.toString().padLeft(12, '0')}',
  title: repeatedName ? '公开地点' : '公开记录 $n',
  detailType: kind == 'OPPORTUNITY' ? 'ACTIVITY' : kind,
  detailID: '22000000-0000-4000-8000-${n.toString().padLeft(12, '0')}',
  placeID: '33000000-0000-4000-8000-000000000001',
  anchorLabel: '公开地点',
  latitude: 57.2,
  longitude: -2.1,
  sourceVersion: 'native-original-$n',
);

void main() {
  test(
    'recording group scope repeats once while each original object and location remains',
    () {
      final items = [
        for (var n = 1; n <= 9; n++) recordingMapItem('MOMENT', n),
      ];
      expect(mapLayerScopeNotices(items), ['公开记录来自本人明确公开的地点情境，不表示作者当前位置。']);
      for (final item in items) {
        expect(item.rowSubtitle, '公开记录 · 公开地点');
        expect(item.rowSubtitle, isNot(contains('当前位置')));
        expect(item.entity.id, item.mapID);
        expect(item.detailID, item.id);
        expect(item.latitude, 57.2);
        expect(item.longitude, -2.1);
        expect(item.explanation, contains('不表示当前位置'));
      }
    },
  );
  test(
    'recording row keeps private audience and removes repeated entity name only',
    () {
      final place = recordingMapItem('PLACE', 1, repeatedName: true);
      final opportunity = recordingMapItem('OPPORTUNITY', 2);
      expect(place.rowSubtitle, '地点');
      expect(opportunity.rowSubtitle, '仅本人可见的活动机会 · 公开地点');
      expect(mapLayerScopeNotices([place, opportunity]), [
        '活动机会仅本人可见，点位来自当前公开活动，不公开意图或关系。',
      ]);
      expect(opportunity.explanation, contains('仅你可见'));
    },
  );
  test(
    'recording mixed group does not broaden organization or business claims',
    () {
      final items = [
        recordingMapItem('MOMENT', 1),
        recordingMapItem('BUSINESS', 2),
        recordingMapItem('ORGANIZATION', 3),
        recordingMapItem('BUSINESS', 4),
      ];
      final notices = mapLayerScopeNotices(items);
      expect(notices, hasLength(3));
      expect(notices.where((v) => v.contains('不代表交易背书')), hasLength(1));
      expect(notices.where((v) => v.contains('明确提交并审核公开')), hasLength(1));
      expect(notices.every((v) => !v.contains('仅本人可见')), isTrue);
      expect(mapLayerScopeNotices(const []), isEmpty);
    },
  );
}
