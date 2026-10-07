import 'package:flutter_test/flutter_test.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_marker_bitmap.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'all six native glyphs distinct cached and selection distinct',
    () async {
      final signatures = <String>{};
      for (final kind in [
        MapEntityKind.place,
        MapEntityKind.activity,
        MapEntityKind.moment,
        MapEntityKind.organization,
        MapEntityKind.business,
        MapEntityKind.opportunity,
      ]) {
        final b = await MapMarkerBitmap.render(kind: kind, selected: false);
        expect(b.length, greaterThan(100));
        signatures.add(b.toString());
        expect(
          identical(
            b,
            await MapMarkerBitmap.render(kind: kind, selected: false),
          ),
          true,
        );
        expect(
          b.toString(),
          isNot(
            (await MapMarkerBitmap.render(
              kind: kind,
              selected: true,
            )).toString(),
          ),
        );
      }
      expect(signatures.length, 6);
    },
  );
}
