import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'organization public pin parses explicit reviewed endpoint coordinates',
    () {
      final pin = PublicOrganizationPin.fromJson({
        'id': 'org-1',
        'name': '测试组织',
        'latitude': 57.1497,
        'longitude': -2.0943,
        'coordinateSystem': 'wgs84',
        'precision': 'point',
      });
      expect(pin.id, 'org-1');
      expect(pin.latitude, 57.1497);
      expect(pin.longitude, -2.0943);
    },
  );

  testWidgets('organization marker selects its stable organization ID', (
    tester,
  ) async {
    const entity = MapEntity(
      id: 'organization:00000000-0000-0000-0000-000000000001',
      kind: MapEntityKind.organization,
      title: '测试组织',
      subtitle: '已审核的公开组织地点',
      latitude: 57.1497,
      longitude: -2.0943,
    );
    String? selected;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: GroupMarker(
            entity: entity,
            selected: false,
            onTap: () => selected = entity.id,
          ),
        ),
      ),
    );
    expect(find.text('测试组织'), findsOneWidget);
    await tester.tap(find.text('测试组织'));
    expect(selected, entity.id);
  });
}
